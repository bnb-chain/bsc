package parlia

import (
	"context"
	"errors"
	"math/big"
	mrand "math/rand"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

func (p *Parlia) getTurnTerm(chain consensus.ChainHeaderReader, header *types.Header) (*uint8, error) {
	parent := chain.GetHeaderByHash(header.ParentHash)
	if parent == nil {
		return nil, errors.New("parent not found")
	}

	var turnTerm uint8
	if p.chainConfig.IsBohr(parent.Number, parent.Time) {
		turnTermFromContract, err := p.getTurnTermFromContract(parent)
		if err != nil {
			return nil, err
		}
		if turnTermFromContract == nil {
			return nil, errors.New("unexpected error when getTurnTermFromContract")
		}
		turnTerm = uint8(turnTermFromContract.Int64())
	} else {
		turnTerm = uint8(defaultTurnTerm)
	}
	log.Debug("getTurnTerm", "turnTerm", turnTerm)

	return &turnTerm, nil
}

func (p *Parlia) getTurnTermFromContract(header *types.Header) (turnTerm *big.Int, err error) {
	// mock to get turnTerm from the contract
	if params.FixedTurnTerm >= 1 && params.FixedTurnTerm <= 9 {
		if params.FixedTurnTerm == 2 {
			return p.getRandTurnTerm(header)
		}
		return big.NewInt(int64(params.FixedTurnTerm)), nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	method := "getTurnTerm"
	toAddress := common.HexToAddress(systemcontracts.ValidatorContract)
	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))

	data, err := p.validatorSetABI.Pack(method)
	if err != nil {
		log.Error("Unable to pack tx for getTurnTerm", "error", err)
		return nil, err
	}
	msgData := (hexutil.Bytes)(data)

	blockNr := rpc.BlockNumberOrHashWithHash(header.Hash(), false)
	result, err := p.ethAPI.Call(ctx, ethapi.TransactionArgs{
		Gas:  &gas,
		To:   &toAddress,
		Data: &msgData,
	}, &blockNr, nil, nil)
	if err != nil {
		return nil, err
	}

	if err := p.validatorSetABI.UnpackIntoInterface(&turnTerm, method, result); err != nil {
		return nil, err
	}

	return turnTerm, nil
}

// getRandTurnTerm returns a random valid value, used to test switching turn terms
func (p *Parlia) getRandTurnTerm(header *types.Header) (turnTerm *big.Int, err error) {
	turnTerms := [8]uint8{1, 3, 4, 5, 6, 7, 8, 9}
	r := mrand.New(mrand.NewSource(int64(header.Time)))
	termIndex := int(r.Int31n(int32(len(turnTerms))))
	return big.NewInt(int64(turnTerms[termIndex])), nil
}
