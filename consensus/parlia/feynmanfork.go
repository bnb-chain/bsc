package parlia

import (
	"container/heap"
	"context"
	"errors"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// the params should be two blocks' time(timestamp)
func sameDayInUTC(first, second uint64) bool {
	return first/params.BreatheBlockInterval == second/params.BreatheBlockInterval
}

func isBreatheBlock(lastBlockTime, blockTime uint64) bool {
	return lastBlockTime != 0 && !sameDayInUTC(lastBlockTime, blockTime)
}

// initializeFeynmanContract initialize new contracts of Feynman fork
func (p *Parlia) initializeFeynmanContract(state vm.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool, tracer *tracing.Hooks,
) error {
	// method
	method := "initialize"

	// initialize contracts
	contracts := []string{
		systemcontracts.StakeHubContract,
		systemcontracts.GovernorContract,
		systemcontracts.GovTokenContract,
		systemcontracts.TimelockContract,
		systemcontracts.TokenRecoverPortalContract,
	}
	// get packed data
	data, err := p.stakeHubABI.Pack(method)
	if err != nil {
		log.Error("Unable to pack tx for initialize feynman contracts", "error", err)
		return err
	}
	for _, c := range contracts {
		msg := p.getSystemMessage(header.Coinbase, common.HexToAddress(c), data, common.Big0)
		// apply message
		log.Info("initialize feynman contract", "block number", header.Number.Uint64(), "contract", c)
		err = p.applyTransaction(msg, state, header, chain, txs, receipts, receivedTxs, usedGas, mining, tracer)
		if err != nil {
			return err
		}
	}
	return nil
}

type ValidatorItem struct {
	address     common.Address
	votingPower *big.Int
	voteAddress []byte
}

// An ValidatorHeap is a max-heap of validator's votingPower.
type ValidatorHeap []ValidatorItem

func (h *ValidatorHeap) Len() int { return len(*h) }

func (h *ValidatorHeap) Less(i, j int) bool {
	// We want topK validators with max voting power, so we need a max-heap
	if (*h)[i].votingPower.Cmp((*h)[j].votingPower) == 0 {
		return (*h)[i].address.Hex() < (*h)[j].address.Hex()
	} else {
		return (*h)[i].votingPower.Cmp((*h)[j].votingPower) == 1
	}
}

func (h *ValidatorHeap) Swap(i, j int) { (*h)[i], (*h)[j] = (*h)[j], (*h)[i] }

func (h *ValidatorHeap) Push(x interface{}) {
	*h = append(*h, x.(ValidatorItem))
}

func (h *ValidatorHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func (p *Parlia) updateValidatorSetV2(state vm.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool, tracer *tracing.Hooks,
) error {
	// 1. get all validators and its voting power
	blockNr := rpc.BlockNumberOrHashWithHash(header.ParentHash, false)
	validatorItems, err := p.getValidatorElectionInfo(blockNr)
	if err != nil {
		return err
	}
	maxElectedValidators, err := p.getMaxElectedValidators(blockNr)
	if err != nil {
		return err
	}

	// 2. sort by voting power
	eValidators, eVotingPowers, eVoteAddrs := getTopValidatorsByVotingPower(validatorItems, maxElectedValidators)

	// 3. update validator set to system contract
	method := "updateValidatorSetV2"
	data, err := p.validatorSetABI.Pack(method, eValidators, eVotingPowers, eVoteAddrs)
	if err != nil {
		log.Error("Unable to pack tx for updateValidatorSetV2", "error", err)
		return err
	}

	// get system message
	msg := p.getSystemMessage(header.Coinbase, common.HexToAddress(systemcontracts.ValidatorContract), data, common.Big0)
	// apply message
	return p.applyTransaction(msg, state, header, chain, txs, receipts, receivedTxs, usedGas, mining, tracer)
}

func (p *Parlia) getValidatorElectionInfo(blockNr rpc.BlockNumberOrHash) ([]ValidatorItem, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	method := "getValidatorElectionInfo"
	toAddress := common.HexToAddress(systemcontracts.StakeHubContract)
	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))

	data, err := p.stakeHubABI.Pack(method, big.NewInt(0), big.NewInt(0))
	if err != nil {
		log.Error("Unable to pack tx for getValidatorElectionInfo", "error", err)
		return nil, err
	}
	msgData := (hexutil.Bytes)(data)

	result, err := p.ethAPI.Call(ctx, ethapi.TransactionArgs{
		Gas:  &gas,
		To:   &toAddress,
		Data: &msgData,
	}, &blockNr, nil, nil)
	if err != nil {
		return nil, err
	}

	var validators []common.Address
	var votingPowers []*big.Int
	var voteAddrs [][]byte
	var totalLength *big.Int
	if err := p.stakeHubABI.UnpackIntoInterface(&[]interface{}{&validators, &votingPowers, &voteAddrs, &totalLength}, method, result); err != nil {
		return nil, err
	}
	if totalLength.Int64() != int64(len(validators)) || totalLength.Int64() != int64(len(votingPowers)) || totalLength.Int64() != int64(len(voteAddrs)) {
		return nil, errors.New("validator length not match")
	}

	validatorItems := make([]ValidatorItem, len(validators))
	for i := 0; i < len(validators); i++ {
		validatorItems[i] = ValidatorItem{
			address:     validators[i],
			votingPower: votingPowers[i],
			voteAddress: voteAddrs[i],
		}
	}

	return validatorItems, nil
}

func (p *Parlia) getMaxElectedValidators(blockNr rpc.BlockNumberOrHash) (maxElectedValidators *big.Int, err error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	method := "maxElectedValidators"
	toAddress := common.HexToAddress(systemcontracts.StakeHubContract)
	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))

	data, err := p.stakeHubABI.Pack(method)
	if err != nil {
		log.Error("Unable to pack tx for maxElectedValidators", "error", err)
		return nil, err
	}
	msgData := (hexutil.Bytes)(data)

	result, err := p.ethAPI.Call(ctx, ethapi.TransactionArgs{
		Gas:  &gas,
		To:   &toAddress,
		Data: &msgData,
	}, &blockNr, nil, nil)
	if err != nil {
		return nil, err
	}

	if err := p.stakeHubABI.UnpackIntoInterface(&maxElectedValidators, method, result); err != nil {
		return nil, err
	}

	return maxElectedValidators, nil
}

func getTopValidatorsByVotingPower(validatorItems []ValidatorItem, maxElectedValidators *big.Int) ([]common.Address, []uint64, [][]byte) {
	var validatorHeap ValidatorHeap
	for i := 0; i < len(validatorItems); i++ {
		// only keep validators with voting power > 0
		if validatorItems[i].votingPower.Cmp(big.NewInt(0)) == 1 {
			validatorHeap = append(validatorHeap, validatorItems[i])
		}
	}
	hp := &validatorHeap
	heap.Init(hp)

	topN := int(maxElectedValidators.Int64())
	if topN > len(validatorHeap) {
		topN = len(validatorHeap)
	}
	eValidators := make([]common.Address, topN)
	eVotingPowers := make([]uint64, topN)
	eVoteAddrs := make([][]byte, topN)
	for i := 0; i < topN; i++ {
		item := heap.Pop(hp).(ValidatorItem)
		eValidators[i] = item.address
		// as the decimal in BNB Beacon Chain is 1e8 and in BNB Smart Chain is 1e18, we need to divide it by 1e10
		eVotingPowers[i] = new(big.Int).Div(item.votingPower, big.NewInt(1e10)).Uint64()
		eVoteAddrs[i] = item.voteAddress
	}

	return eValidators, eVotingPowers, eVoteAddrs
}
