package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ethereum/go-ethereum/cmd/bind/ownable"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/accounts/usbwallet"
	"github.com/ethereum/go-ethereum/cmd/bind/bep20"
	tokenmanager "github.com/ethereum/go-ethereum/cmd/bind/tokenmanger"
	"github.com/ethereum/go-ethereum/cmd/bind/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

var (
	tokenManager = common.HexToAddress("0x0000000000000000000000000000000000001008")
)

type Contract struct {
	ContractData string `json:"contract_data"`
	Symbol       string `json:"symbol"`
	BEP2Symbol   string `json:"bep2_symbol"`
}

func printUsage() {
	fmt.Print("usage: ./bind --contract-data {path to contract json} --operation {transferBNB or approveBind}\n")
}

func initFlags() {
	flag.String(utils.ContractData, "", "contract data file path")
	flag.String(utils.Operation, "", "operation to perform")
	flag.String(utils.BEP20ContractAddr, "", "bep20 contract address")
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	err := viper.BindPFlags(pflag.CommandLine)
	if err != nil {
		panic(err)
	}
}

func readContractData(contractPath string) (Contract, error) {
	file, err := os.Open(contractPath)
	if err != nil {
		return Contract{}, err
	}
	fileData, err := ioutil.ReadAll(file)
	if err != nil {
		return Contract{}, err
	}
	var contract Contract
	err = json.Unmarshal(fileData, &contract)
	if err != nil {
		return Contract{}, err
	}
	return contract, nil
}

func generateOrGetTempAccount() (*keystore.KeyStore, accounts.Account, error) {
	path, err := os.Getwd()
	if err != nil {
		return nil, accounts.Account{}, err
	}
	if _, err := os.Stat(filepath.Join(path, utils.BindKeystore)); os.IsNotExist(err) {
		err = os.Mkdir(filepath.Join(path, utils.BindKeystore), os.ModePerm)
		if err != nil {
			panic(err)
		}
	}
	keyStore := keystore.NewKeyStore(filepath.Join(path, utils.BindKeystore), keystore.StandardScryptN, keystore.StandardScryptP)
	var files []string
	err = filepath.Walk(filepath.Join(path, utils.BindKeystore), func(path string, info os.FileInfo, err error) error {
		files = append(files, path)
		return nil
	})
	files = files[1:]
	if len(files) == 0  {
		newAccount, err := keyStore.NewAccount(utils.Passwd)
		if err != nil {
			return nil, accounts.Account{}, err
		}
		err = keyStore.Unlock(newAccount, utils.Passwd)
		if err != nil {
			return nil, accounts.Account{}, err
		}
		fmt.Println(fmt.Sprintf("Create new account %s for deploying contract", newAccount.Address.String()))
		return keyStore, newAccount, nil
	} else if len(files) == 1 {
		accountList := keyStore.Accounts()
		if len(accountList) != 1 {
			return nil, accounts.Account{}, err
		}
		account := accountList[0]
		fmt.Println(fmt.Sprintf("Load account %s for deploying contract", account.Address.String()))
		err = keyStore.Unlock(account, utils.Passwd)
		if err != nil {
			return nil, accounts.Account{}, err
		}
		return keyStore, account, nil
	} else {
		return  nil, accounts.Account{}, fmt.Errorf("expect only one or zero keystore file in %s", filepath.Join(path, utils.BindKeystore))
	}
}

func openLedger(ethClient *ethclient.Client) (accounts.Wallet, accounts.Account, error) {
	ledgerHub, err := usbwallet.NewLedgerHub()
	if err != nil {
		return nil, accounts.Account{}, fmt.Errorf("failed to start Ledger hub, disabling: %v", err)
	}
	wallets := ledgerHub.Wallets()
	if len(wallets) == 0 {
		return nil, accounts.Account{}, fmt.Errorf("empty ledger wallet")
	}
	wallet := wallets[0]
	err = wallet.Close()
	if err != nil {
		fmt.Println(err.Error())
	}

	err = wallet.Open("")
	if err != nil {
		return nil, accounts.Account{}, fmt.Errorf("failed to start Ledger hub, disabling: %v", err)
	}

	walletStatus, err := wallet.Status()
	if err != nil {
		return nil, accounts.Account{}, fmt.Errorf("failed to start Ledger hub, disabling: %v", err)
	}
	fmt.Println(walletStatus)
	//fmt.Println(wallet.URL())

	wallet.SelfDerive([]accounts.DerivationPath{accounts.LegacyLedgerBaseDerivationPath, accounts.DefaultBaseDerivationPath}, ethClient)
	utils.Sleep(3)
	if len(wallet.Accounts()) == 0 {
		return nil,  accounts.Account{}, fmt.Errorf("empty ledger account")
	}
	ledgerAccount := wallet.Accounts()[0]

	fmt.Println(fmt.Sprintf("Ledger account %s", ledgerAccount.Address.String()))

	return wallet, ledgerAccount, nil
}

func main() {
	initFlags()

	contractPath := viper.GetString(utils.ContractData)
	operation := viper.GetString(utils.Operation)
	if contractPath == "" || operation != utils.TransferBNB && operation != utils.ApproveBind{
		printUsage()
		return
	}
	rpcClient, err := rpc.DialContext(context.Background(), utils.TestnetRPC)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	ethClient := ethclient.NewClient(rpcClient)
	contract, err := readContractData(contractPath)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	keyStore, tempAccount, err := generateOrGetTempAccount()
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	ledgerWallet, ledgerAccount, err := openLedger(ethClient)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	defer ledgerWallet.Close()

	if operation == utils.TransferBNB {
		contractAddr, err := TransferBNBAndDeployContractFromKeystoreAccount(ethClient, ledgerWallet, ledgerAccount, keyStore, tempAccount, contract)
		if err != nil {
			fmt.Println(err.Error())
			return
		}
		fmt.Println(fmt.Sprintf("For BEP2 token %s, the deployed BEP20 contract address is %s", contract.BEP2Symbol, contractAddr.String()))
	} else {
		bep20ContractAddr := viper.GetString(utils.BEP20ContractAddr)
		if bep20ContractAddr == "" {
			fmt.Println("bep20 contract address is empty")
			return
		}
		ApproveBindAndTransferOwnershipAndRestBalanceBackToLedgerAccount(ethClient, ledgerWallet, ledgerAccount, keyStore, tempAccount, contract, common.HexToAddress(bep20ContractAddr))
	}

}

func TransferBNBAndDeployContractFromKeystoreAccount(ethClient *ethclient.Client, ledgerWallet accounts.Wallet, ledgerAccount accounts.Account, keyStore *keystore.KeyStore, tempAccount accounts.Account, contract Contract) (common.Address, error) {
	amount := big.NewInt(utils.OneBNB)
	fmt.Println(fmt.Sprintf("Send %s BNB from to %s", amount.String(), tempAccount.Address.String()))
	err := utils.SendBNBToTempAccount(ethClient, ledgerWallet, ledgerAccount, tempAccount.Address, amount)
	if err != nil {
		return common.Address{}, err
	}
	utils.Sleep(10)

	fmt.Println(fmt.Sprintf("Deploy BEP20 contract %s from account %s", contract.Symbol, tempAccount.Address.String()))
	contractByteCode, err := hex.DecodeString(contract.ContractData)
	if err != nil {
		return common.Address{}, err
	}
	txHash, err := utils.DeployContract(ethClient, keyStore, tempAccount, contractByteCode)
	if err != nil {
		return common.Address{}, err
	}
	utils.Sleep(10)

	txRecipient, err := ethClient.TransactionReceipt(context.Background(), txHash)
	if err != nil {
		return common.Address{}, err
	}
	contractAddr := txRecipient.ContractAddress
	fmt.Println(fmt.Sprintf("BEP20 contract addrss: %s", contractAddr.String()))
	return contractAddr, nil
}

func ApproveBindAndTransferOwnershipAndRestBalanceBackToLedgerAccount(ethClient *ethclient.Client, ledgerWallet accounts.Wallet, ledgerAccount accounts.Account, keyStore *keystore.KeyStore, tempAccount accounts.Account, contract Contract, bep20ContractAddr common.Address)  {
	bep20Instance, err := bep20.NewBep20(bep20ContractAddr, ethClient)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	totalSupply, err := bep20Instance.TotalSupply(utils.GetCallOpts())
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	fmt.Println(fmt.Sprintf("Total Supply %s", totalSupply.String()))

	fmt.Println(fmt.Sprintf("Approve %s:%s to TokenManager from %s", totalSupply.String(), contract.Symbol, tempAccount.Address.String()))
	approveTxHash, err := bep20Instance.Approve(utils.GetTransactor(ethClient, keyStore, tempAccount, big.NewInt(0)), tokenManager, totalSupply)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	fmt.Println(fmt.Sprintf("Approve token to tokenManager txHash %s", approveTxHash.Hash().String()))

	utils.Sleep(20)

	tokenManagerInstance, _ := tokenmanager.NewTokenmanager(tokenManager, ethClient)
	approveBindTx, err := tokenManagerInstance.ApproveBind(utils.GetTransactor(ethClient, keyStore, tempAccount, big.NewInt(1e16)), bep20ContractAddr, contract.BEP2Symbol)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	fmt.Println(fmt.Sprintf("ApproveBind txHash %s", approveBindTx.Hash().String()))

	utils.Sleep(10)

	approveBindTxRecipient, err := ethClient.TransactionReceipt(context.Background(), approveBindTx.Hash())
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	fmt.Println("Track approveBind Tx status")
	if approveBindTxRecipient.Status != 1 {
		fmt.Println("Approve Bind Failed")
		rejectBindTx, err := tokenManagerInstance.RejectBind(utils.GetTransactor(ethClient, keyStore, tempAccount, big.NewInt(1e16)), bep20ContractAddr, contract.BEP2Symbol)
		if err != nil {
			fmt.Println(err.Error())
			return
		}
		fmt.Println(fmt.Sprintf("rejectBind txHash %s", rejectBindTx.Hash().String()))
		utils.Sleep(10)
		fmt.Println("Track rejectBind Tx status")
		rejectBindTxRecipient, err := ethClient.TransactionReceipt(context.Background(), rejectBindTx.Hash())
		if err != nil {
			fmt.Println(err.Error())
			return
		}
		fmt.Println(fmt.Sprintf("reject bind tx recipient status %d", rejectBindTxRecipient.Status))
		return
	}

	utils.Sleep(10)
	ownershipInstance, err := ownable.NewOwnable(bep20ContractAddr, ethClient)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	fmt.Println(fmt.Sprintf("Transfer %s %s to ledger account %s", totalSupply.String(), contract.Symbol, tempAccount.Address.String()))
	transferOwnerShipTxHash, err := ownershipInstance.TransferOwnership(utils.GetTransactor(ethClient, keyStore, tempAccount, big.NewInt(0)), ledgerAccount.Address)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	fmt.Println(fmt.Sprintf("transfer ownership txHash %s", transferOwnerShipTxHash.Hash().String()))
	utils.Sleep(20)
	fmt.Println(fmt.Sprintf("Send BNB back to ledger account, txhash %s", transferOwnerShipTxHash.Hash().String()))
	err = utils.SendBNBBackToLegerAccount(ethClient, keyStore, tempAccount, ledgerAccount.Address)
	if err != nil {
		fmt.Println(err.Error())
	}
}
