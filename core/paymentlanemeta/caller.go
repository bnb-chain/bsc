package paymentlanemeta

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

const getterGasLimit uint64 = 50_000_000

// query payment lane state from the parent post-state.
func callGetter(config *params.ChainConfig, header *types.Header, statedb *state.StateDB, input []byte) ([]byte, error) {
	snapshot := statedb.Snapshot()
	defer statedb.RevertToSnapshot(snapshot)

	evm := vm.NewEVM(blockContext(header), statedb, config, vm.Config{NoBaseFee: true})
	defer evm.Release()

	ret, _, callErr := evm.StaticCall(common.Address{}, paymentlane.ContractAddress, input, vm.NewGasBudget(getterGasLimit))
	if err := statedb.Error(); err != nil {
		return nil, fmt.Errorf("%w: payment lane state read: %w", paymentlane.ErrStateUnavailable, err)
	}
	if callErr != nil {
		return nil, fmt.Errorf("%w: payment lane getter %x: %w", paymentlane.ErrCorruptConfig, input[:4], callErr)
	}
	return ret, nil
}

func blockContext(header *types.Header) vm.BlockContext {
	difficulty := big.NewInt(0)
	if header.Difficulty != nil {
		difficulty = new(big.Int).Set(header.Difficulty)
	}
	baseFee := big.NewInt(0)
	if header.BaseFee != nil {
		baseFee = new(big.Int).Set(header.BaseFee)
	}
	blobBaseFee := big.NewInt(0)
	var random *common.Hash
	if difficulty.Sign() == 0 {
		random = &header.MixDigest
	}
	var slotNum uint64
	if header.SlotNumber != nil {
		slotNum = *header.SlotNumber
	}
	return vm.BlockContext{
		CanTransfer: func(vm.StateDB, common.Address, *uint256.Int) bool { return true },
		Transfer:    func(vm.StateDB, common.Address, common.Address, *uint256.Int, *params.Rules) {},
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		Coinbase:    header.Coinbase,
		BlockNumber: new(big.Int).Set(header.Number),
		Time:        header.Time,
		Difficulty:  difficulty,
		BaseFee:     baseFee,
		BlobBaseFee: blobBaseFee,
		GasLimit:    header.GasLimit,
		Random:      random,
		SlotNum:     slotNum,
	}
}
