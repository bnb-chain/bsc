package parlia_tests

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

func TestEIP2718PostHertz(t *testing.T) {
	bytecode := []byte{
		byte(vm.PC),
		byte(vm.PC),
		byte(vm.SLOAD),
		byte(vm.SLOAD),
	}
	aa := common.HexToAddress("0x000000000000000000000000000000000000aaaa")
	txData := &types.AccessListTx{
		ChainID: postHertzConfig().ChainID,
		Nonce:   0,
		To:      &aa,
		Gas:     30000,
		AccessList: types.AccessList{{
			Address:     aa,
			StorageKeys: []common.Hash{{0}},
		}},
	}

	// A sender who makes transactions, has some funds
	key, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	address := crypto.PubkeyToAddress(key.PublicKey)
	balanceBefore := big.NewInt(1000000000000000)
	gAlloc := core.GenesisAlloc{
		address: {Balance: balanceBefore},
		aa: {
			Code:    bytecode,
			Storage: nil,
			Nonce:   0,
			Balance: big.NewInt(0),
		},
	}

	// Expected gas is intrinsic + 2 * pc + hot load + cold load, since only one load is in the access list
	expectedGasUsage := params.TxGas + params.TxAccessListAddressGas + params.TxAccessListStorageKeyGas +
		vm.GasQuickStep*2 + params.WarmStorageReadCostEIP2929 + params.ColdSloadCostEIP2929

	TestGasUsage(t, postHertzConfig(), ethash.NewFaker(), key, gAlloc, txData, expectedGasUsage)
}
