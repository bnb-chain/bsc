package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/logrusorgru/aurora"
	"github.com/prysmaticlabs/prysm/v3/crypto/bls"
	"github.com/prysmaticlabs/prysm/v3/encoding/bytesutil"
	"github.com/prysmaticlabs/prysm/v3/io/prompt"
	"github.com/prysmaticlabs/prysm/v3/proto/eth/service"
	"github.com/prysmaticlabs/prysm/v3/validator/accounts"
	"github.com/prysmaticlabs/prysm/v3/validator/accounts/iface"
	"github.com/prysmaticlabs/prysm/v3/validator/accounts/petnames"
	"github.com/prysmaticlabs/prysm/v3/validator/accounts/wallet"
	"github.com/prysmaticlabs/prysm/v3/validator/keymanager"
	"github.com/prysmaticlabs/prysm/v3/validator/keymanager/local"
	keystorev4 "github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4"
	"gopkg.in/urfave/cli.v1"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/signer/core"
)

const (
	BLSWalletPath   = "bls/wallet"
	BLSKeystorePath = "bls/keystore"
)

var (
	au = aurora.NewAurora(true)
)

var (
	blsCommand = cli.Command{
		Name:      "bls",
		Usage:     "Manage BLS wallet and accounts",
		ArgsUsage: "",
		Category:  "BLS ACCOUNT COMMANDS",
		Description: `

Manage BLS wallet and accounts, before creating or importing BLS accounts, create
BLS wallet first. One BLS wallet is enough for all BLS accounts, the first BLS
account in the wallet will be used to vote for fast finality now.

It only supports interactive mode now, when you are prompted for password, please
input your password, and take care the wallet password which is different from accounts'
password.

There are generally two steps to manage a BLS account:
1.Create a BLS wallet: geth bls wallet create
2.Create a BLS account: geth bls account new
  or import a BLS account: geth bls account import <keystore file>`,
		Subcommands: []cli.Command{
			{
				Name:      "wallet",
				Usage:     "Manage BLS wallet",
				ArgsUsage: "",
				Category:  "BLS ACCOUNT COMMANDS",
				Description: `

Create a BLS wallet to manage BLS accounts, this should before creating
or import a BLS account. The BLS wallet dir should be "<DATADIR>/bls/wallet".`,
				Subcommands: []cli.Command{
					{
						Name:      "create",
						Usage:     "Create BLS wallet",
						Action:    utils.MigrateFlags(blsWalletCreate),
						ArgsUsage: "",
						Category:  "BLS ACCOUNT COMMANDS",
						Flags: []cli.Flag{
							utils.DataDirFlag,
						},
						Description: `
	geth bls wallet create

will prompt for your password then create a BLS wallet in "<DATADIR>/bls/wallet",
don't create BLS wallet repeatedly'.`,
					},
				},
			},
			{
				Name:      "account",
				Usage:     "Manage BLS accounts",
				ArgsUsage: "",
				Category:  "BLS ACCOUNT COMMANDS",
				Description: `

Manage BLS accounts,list all existing accounts, import a BLS private key into
a new account, create a new account or delete an existing account.

Make sure you remember the password you gave when creating a new account.
Without it you are not able to unlock your account. And this password is
different from the wallet password, please take care.

Note that exporting your key in unencrypted format is NOT supported.

Keys are stored under <DATADIR>/bls/keystore.
It is safe to transfer the entire directory or the individual keys therein
between ethereum nodes by simply copying.

Make sure you backup your BLS keys regularly.`,
				Subcommands: []cli.Command{
					{
						Name:      "new",
						Usage:     "Create a BLS account",
						Action:    utils.MigrateFlags(blsAccountCreate),
						ArgsUsage: "",
						Category:  "BLS ACCOUNT COMMANDS",
						Flags: []cli.Flag{
							utils.DataDirFlag,
						},
						Description: `
	geth bls account new

Creates a new BLS account and imports the account into the BLS wallet.

If the BLS wallet not created yet, it will try to create BLS wallet first.

The account is saved in encrypted format, you are prompted for a password.
You must remember this password to unlock your account in the future.`,
					},
					{
						Name:      "import",
						Usage:     "Import a BLS account",
						Action:    utils.MigrateFlags(blsAccountImport),
						ArgsUsage: "<keystore file>",
						Category:  "BLS ACCOUNT COMMANDS",
						Flags: []cli.Flag{
							utils.DataDirFlag,
						},
						Description: `
	geth bls account import <keyFile>

Import a encrypted BLS account from keystore file <keyFile> into the BLS wallet.

If the BLS wallet not created yet, it will try to create BLS wallet first.`,
					},
					{
						Name:      "list",
						Usage:     "Print summary of existing BLS accounts",
						Action:    utils.MigrateFlags(blsAccountList),
						ArgsUsage: "",
						Category:  "BLS ACCOUNT COMMANDS",
						Flags: []cli.Flag{
							utils.DataDirFlag,
						},
						Description: `
	geth bls account list

Print summary of existing BLS accounts in the current BLS wallet.`,
					},
					{
						Name:      "delete",
						Usage:     "Delete the selected BLS account from the BLS wallet",
						Action:    utils.MigrateFlags(blsAccountDelete),
						ArgsUsage: "<BLS pubkey>",
						Category:  "BLS ACCOUNT COMMANDS",
						Flags: []cli.Flag{
							utils.DataDirFlag,
						},
						Description: `
	geth bls account delete

Delete the selected BLS account from the BLS wallet.`,
					},
				},
			},
		},
	}
)

// blsWalletCreate creates a BLS wallet in <DATADIR>/bls/wallet.
func blsWalletCreate(ctx *cli.Context) error {
	cfg := gethConfig{Node: defaultNodeConfig()}
	// Load config file.
	if file := ctx.GlobalString(configFileFlag.Name); file != "" {
		if err := loadConfig(file, &cfg); err != nil {
			utils.Fatalf("%v", err)
		}
	}
	utils.SetNodeConfig(ctx, &cfg.Node)

	dir := filepath.Join(cfg.Node.DataDir, BLSWalletPath)
	dirExists, err := wallet.Exists(dir)
	if err != nil {
		utils.Fatalf("Check BLS wallet exists error: %v.", err)
	}
	if dirExists {
		utils.Fatalf("BLS wallet already exists in <DATADIR>/bls/wallet.")
	}

	password := utils.GetPassPhrase("Your new BLS wallet will be locked with a password. Please give a password. Do not forget this password.", true)

	opts := []accounts.Option{}
	opts = append(opts, accounts.WithWalletDir(dir))
	opts = append(opts, accounts.WithWalletPassword(password))
	opts = append(opts, accounts.WithKeymanagerType(keymanager.Local))
	opts = append(opts, accounts.WithSkipMnemonicConfirm(true))
	acc, err := accounts.NewCLIManager(opts...)
	if err != nil {
		utils.Fatalf("New Accounts CLI Manager failed: %v.", err)
	}
	if _, err := acc.WalletCreate(context.Background()); err != nil {
		utils.Fatalf("Create BLS wallet failed: %v.", err)
	}

	fmt.Println("Create BLS wallet successfully!")
	return nil
}

// openOrCreateBLSWallet opens BLS wallet in <DATADIR>/bls/wallet, if wallet
// not exists, then creates BLS wallet in <DATADIR>/bls/wallet.
func openOrCreateBLSWallet(ctx *cli.Context, cfg *gethConfig) (*wallet.Wallet, error) {
	var w *wallet.Wallet
	walletDir := filepath.Join(cfg.Node.DataDir, BLSWalletPath)
	dirExists, err := wallet.Exists(walletDir)
	if err != nil {
		utils.Fatalf("Check dir %s failed: %v.", walletDir, err)
	}
	if !dirExists {
		fmt.Println("BLS wallet not exists, creating BLS wallet...")
		password := utils.GetPassPhrase("Your new BLS wallet will be locked with a password. Please give a password. Do not forget this password.", true)

		opts := []accounts.Option{}
		opts = append(opts, accounts.WithWalletDir(walletDir))
		opts = append(opts, accounts.WithWalletPassword(password))
		opts = append(opts, accounts.WithKeymanagerType(keymanager.Local))
		opts = append(opts, accounts.WithSkipMnemonicConfirm(true))
		acc, err := accounts.NewCLIManager(opts...)
		if err != nil {
			utils.Fatalf("New Accounts CLI Manager failed: %v.", err)
		}
		w, err := acc.WalletCreate(context.Background())
		if err != nil {
			utils.Fatalf("Create BLS wallet failed: %v.", err)
		}

		fmt.Println("Create BLS wallet successfully!")
		return w, nil
	}

	walletPassword := utils.GetPassPhrase("Enter the password for your BLS wallet.", false)
	w, err = wallet.OpenWallet(context.Background(), &wallet.Config{
		WalletDir:      walletDir,
		WalletPassword: walletPassword,
	})
	if err != nil {
		utils.Fatalf("Open BLS wallet failed: %v.", err)
	}
	return w, nil
}

// blsAccountCreate creates a BLS account in <DATADIR>/bls/keystore,
// and import the created account into the BLS wallet.
func blsAccountCreate(ctx *cli.Context) error {
	cfg := gethConfig{Node: defaultNodeConfig()}
	// Load config file.
	if file := ctx.GlobalString(configFileFlag.Name); file != "" {
		if err := loadConfig(file, &cfg); err != nil {
			utils.Fatalf("%v", err)
		}
	}
	utils.SetNodeConfig(ctx, &cfg.Node)

	w, _ := openOrCreateBLSWallet(ctx, &cfg)
	if w.KeymanagerKind() != keymanager.Local {
		utils.Fatalf("BLS wallet has wrong key manager kind.")
	}
	km, err := w.InitializeKeymanager(context.Background(), iface.InitKeymanagerConfig{ListenForChanges: false})
	if err != nil {
		utils.Fatalf("Initialize key manager failed: %v.", err)
	}
	k, ok := km.(keymanager.Importer)
	if !ok {
		utils.Fatalf("The BLS keymanager cannot import keystores")
	}

	keystoreDir := filepath.Join(cfg.Node.DataDir, BLSKeystorePath)
	if err := os.MkdirAll(keystoreDir, 0755); err != nil {
		utils.Fatalf("Could not access keystore dir: %v.", err)
	}
	accountPassword := utils.GetPassPhrase("Your new BLS account will be encrypted with a password. Please give a password. Do not forget this password.", true)
	if err := core.ValidatePasswordFormat(accountPassword); err != nil {
		utils.Fatalf("Password invalid: %v.", err)
	}

	encryptor := keystorev4.New()
	secretKey, err := bls.RandKey()
	if err != nil {
		utils.Fatalf("Could not generate BLS secret key: %v.", err)
	}
	pubKeyBytes := secretKey.PublicKey().Marshal()
	cryptoFields, err := encryptor.Encrypt(secretKey.Marshal(), accountPassword)
	if err != nil {
		utils.Fatalf("Could not encrypt secret key: %v.", err)
	}
	id, err := uuid.NewRandom()
	if err != nil {
		utils.Fatalf("Could not generate uuid: %v.", err)
	}
	keystore := &keymanager.Keystore{
		Crypto:  cryptoFields,
		ID:      id.String(),
		Pubkey:  fmt.Sprintf("%x", pubKeyBytes),
		Version: encryptor.Version(),
		Name:    encryptor.Name(),
	}

	encodedFile, err := json.MarshalIndent(keystore, "", "\t")
	if err != nil {
		utils.Fatalf("Could not marshal keystore to JSON file: %v.", err)
	}
	keystoreFile, err := os.Create(fmt.Sprintf("%s/keystore-%s.json", keystoreDir, petnames.DeterministicName(pubKeyBytes, "-")))
	if err != nil {
		utils.Fatalf("Could not create keystore file: %v.", err)
	}
	if _, err := keystoreFile.Write(encodedFile); err != nil {
		utils.Fatalf("Could not write keystore file contents: %v.", err)
	}
	fmt.Println("Successfully create a BLS account.")

	fmt.Println("Importing BLS account, this may take a while...")
	_, err = accounts.ImportAccounts(context.Background(), &accounts.ImportAccountsConfig{
		Importer:        k,
		Keystores:       []*keymanager.Keystore{keystore},
		AccountPassword: accountPassword,
	})
	if err != nil {
		utils.Fatalf("Import BLS account failed: %v.", err)
	}
	fmt.Printf("Successfully import created BLS account.\n")
	return nil
}

// blsAccountImport imports a BLS account into the BLS wallet.
func blsAccountImport(ctx *cli.Context) error {
	keyfile := ctx.Args().First()
	if len(keyfile) == 0 {
		utils.Fatalf("The keystore file must be given as argument.")
	}
	keyJSON, err := ioutil.ReadFile(keyfile)
	if err != nil {
		utils.Fatalf("Could not read keystore file: %v", err)
	}
	keystore := &keymanager.Keystore{}
	if err := json.Unmarshal(keyJSON, keystore); err != nil {
		utils.Fatalf("Could not decode keystore file: %v.", err)
	}
	if keystore.Pubkey == "" {
		utils.Fatalf(" Missing public key, wrong keystore file.")
	}

	cfg := gethConfig{Node: defaultNodeConfig()}
	// Load config file.
	if file := ctx.GlobalString(configFileFlag.Name); file != "" {
		if err := loadConfig(file, &cfg); err != nil {
			utils.Fatalf("%v", err)
		}
	}
	utils.SetNodeConfig(ctx, &cfg.Node)

	w, _ := openOrCreateBLSWallet(ctx, &cfg)
	if w.KeymanagerKind() != keymanager.Local {
		utils.Fatalf("BLS wallet has wrong key manager kind.")
	}
	km, err := w.InitializeKeymanager(context.Background(), iface.InitKeymanagerConfig{ListenForChanges: false})
	if err != nil {
		utils.Fatalf("Initialize key manager failed: %v.", err)
	}
	k, ok := km.(keymanager.Importer)
	if !ok {
		utils.Fatalf("The BLS keymanager cannot import keystores")
	}

	password := utils.GetPassPhrase("Enter the password for your imported account.", false)
	fmt.Println("Importing BLS account, this may take a while...")
	statuses, err := accounts.ImportAccounts(context.Background(), &accounts.ImportAccountsConfig{
		Importer:        k,
		Keystores:       []*keymanager.Keystore{keystore},
		AccountPassword: password,
	})
	if err != nil {
		utils.Fatalf("Import BLS account failed: %v.", err)
	}
	// len(statuses)==len(Keystores) when err==nil
	if statuses[0].Status == service.ImportedKeystoreStatus_ERROR {
		fmt.Printf("Could not import keystore: %v.", statuses[0].Message)
	} else {
		fmt.Println("Successfully import BLS account.")
	}
	return nil
}

// blsAccountList prints existing BLS accounts in the BLS wallet.
func blsAccountList(ctx *cli.Context) error {
	cfg := gethConfig{Node: defaultNodeConfig()}
	// Load config file.
	if file := ctx.GlobalString(configFileFlag.Name); file != "" {
		if err := loadConfig(file, &cfg); err != nil {
			utils.Fatalf("%v", err)
		}
	}
	utils.SetNodeConfig(ctx, &cfg.Node)

	walletDir := filepath.Join(cfg.Node.DataDir, BLSWalletPath)
	dirExists, err := wallet.Exists(walletDir)
	if err != nil || !dirExists {
		utils.Fatalf("BLS wallet not exists.")
	}

	walletPassword := utils.GetPassPhrase("Enter the password for your BLS wallet.", false)
	w, err := wallet.OpenWallet(context.Background(), &wallet.Config{
		WalletDir:      walletDir,
		WalletPassword: walletPassword,
	})
	if err != nil {
		utils.Fatalf("Open BLS wallet failed: %v.", err)
	}
	km, err := w.InitializeKeymanager(context.Background(), iface.InitKeymanagerConfig{ListenForChanges: false})
	if err != nil {
		utils.Fatalf("Initialize key manager failed: %v.", err)
	}

	ikm, ok := km.(*local.Keymanager)
	if !ok {
		utils.Fatalf("Could not assert keymanager interface to concrete type.")
	}
	accountNames, err := ikm.ValidatingAccountNames()
	if err != nil {
		utils.Fatalf("Could not fetch account names: %v.", err)
	}
	numAccounts := au.BrightYellow(len(accountNames))
	fmt.Printf("(keymanager kind) %s\n", au.BrightGreen("imported wallet").Bold())
	fmt.Println("")
	if len(accountNames) == 1 {
		fmt.Printf("Showing %d BLS account\n", numAccounts)
	} else {
		fmt.Printf("Showing %d BLS accounts\n", numAccounts)
	}
	pubKeys, err := km.FetchValidatingPublicKeys(context.Background())
	if err != nil {
		utils.Fatalf("Could not fetch BLS public keys: %v.", err)
	}
	for i := 0; i < len(accountNames); i++ {
		fmt.Println("")
		fmt.Printf("%s | %s\n", au.BrightBlue(fmt.Sprintf("Account %d", i)).Bold(), au.BrightGreen(accountNames[i]).Bold())
		fmt.Printf("%s %#x\n", au.BrightMagenta("[BLS public key]").Bold(), pubKeys[i])
	}
	fmt.Println("")
	return nil
}

// blsAccountDelete deletes a selected BLS account from the BLS wallet.
func blsAccountDelete(ctx *cli.Context) error {
	if len(ctx.Args()) == 0 {
		utils.Fatalf("No BLS account specified to delete.")
	}
	var filteredPubKeys []bls.PublicKey
	for _, str := range ctx.Args() {
		pkString := str
		if strings.Contains(pkString, "0x") {
			pkString = pkString[2:]
		}
		pubKeyBytes, err := hex.DecodeString(pkString)
		if err != nil {
			utils.Fatalf("Could not decode string %s as hex.", pkString)
		}
		blsPublicKey, err := bls.PublicKeyFromBytes(pubKeyBytes)
		if err != nil {
			utils.Fatalf("%#x is not a valid BLS public key.", pubKeyBytes)
		}
		filteredPubKeys = append(filteredPubKeys, blsPublicKey)
	}

	cfg := gethConfig{Node: defaultNodeConfig()}
	// Load config file.
	if file := ctx.GlobalString(configFileFlag.Name); file != "" {
		if err := loadConfig(file, &cfg); err != nil {
			utils.Fatalf("%v", err)
		}
	}
	utils.SetNodeConfig(ctx, &cfg.Node)

	walletDir := filepath.Join(cfg.Node.DataDir, BLSWalletPath)
	dirExists, err := wallet.Exists(walletDir)
	if err != nil || !dirExists {
		utils.Fatalf("BLS wallet not exists.")
	}

	walletPassword := utils.GetPassPhrase("Enter the password for your BLS wallet.", false)
	w, err := wallet.OpenWallet(context.Background(), &wallet.Config{
		WalletDir:      walletDir,
		WalletPassword: walletPassword,
	})
	if err != nil {
		utils.Fatalf("Open BLS wallet failed: %v.", err)
	}
	km, err := w.InitializeKeymanager(context.Background(), iface.InitKeymanagerConfig{ListenForChanges: false})
	if err != nil {
		utils.Fatalf("Initialize key manager failed: %v.", err)
	}
	pubkeys, err := km.FetchValidatingPublicKeys(context.Background())
	if err != nil {
		utils.Fatalf("Could not fetch BLS public keys: %v.", err)
	}

	rawPublicKeys := make([][]byte, len(filteredPubKeys))
	formattedPubKeys := make([]string, len(filteredPubKeys))
	for i, pk := range filteredPubKeys {
		pubKeyBytes := pk.Marshal()
		rawPublicKeys[i] = pubKeyBytes
		formattedPubKeys[i] = fmt.Sprintf("%#x", bytesutil.Trunc(pubKeyBytes))
	}
	allAccountStr := strings.Join(formattedPubKeys, ", ")
	if len(filteredPubKeys) == 1 {
		promptText := "Are you sure you want to delete 1 account? (%s) Y/N"
		resp, err := prompt.ValidatePrompt(
			os.Stdin, fmt.Sprintf(promptText, au.BrightGreen(formattedPubKeys[0])), prompt.ValidateYesOrNo,
		)
		if err != nil {
			return err
		}
		if strings.EqualFold(resp, "n") {
			return nil
		}
	} else {
		promptText := "Are you sure you want to delete %d accounts? (%s) Y/N"
		if len(filteredPubKeys) == len(pubkeys) {
			promptText = fmt.Sprintf("Are you sure you want to delete all accounts? Y/N (%s)", au.BrightGreen(allAccountStr))
		} else {
			promptText = fmt.Sprintf(promptText, len(filteredPubKeys), au.BrightGreen(allAccountStr))
		}
		resp, err := prompt.ValidatePrompt(os.Stdin, promptText, prompt.ValidateYesOrNo)
		if err != nil {
			return err
		}
		if strings.EqualFold(resp, "n") {
			return nil
		}
	}

	if err := accounts.DeleteAccount(context.Background(), &accounts.DeleteConfig{
		Keymanager:       km,
		DeletePublicKeys: rawPublicKeys,
	}); err != nil {
		utils.Fatalf("Delete account failed: %v.", err)
	}

	return nil
}
