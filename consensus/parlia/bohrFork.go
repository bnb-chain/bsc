package parlia

import (
	"context"
	"errors"
	"math"
	"math/big"
	mrand "math/rand"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

func (p *Parlia) getTurnLength(chain consensus.ChainHeaderReader, header *types.Header) (*uint8, error) {
	parent := chain.GetHeaderByHash(header.ParentHash)
	if parent == nil {
		return nil, errors.New("parent not found")
	}

	var turnLength uint8
	if p.chainConfig.IsBohr(parent.Number, parent.Time) {
		turnLengthFromContract, err := p.getTurnLengthFromContract(parent)
		if err != nil {
			return nil, err
		}
		if turnLengthFromContract == nil {
			return nil, errors.New("unexpected error when getTurnLengthFromContract")
		}
		turnLength = uint8(turnLengthFromContract.Int64())
	} else {
		turnLength = defaultTurnLength
	}
	log.Debug("getTurnLength", "turnLength", turnLength)

	return &turnLength, nil
}

func (p *Parlia) getTurnLengthFromContract(header *types.Header) (turnLength *big.Int, err error) {
	// mock to get turnLength from the contract
	if params.FixedTurnLength >= 1 && params.FixedTurnLength <= 9 {
		if params.FixedTurnLength == 2 {
			return p.getRandTurnLength(header)
		}
		return big.NewInt(int64(params.FixedTurnLength)), nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	method := "getTurnLength"
	toAddress := common.HexToAddress(systemcontracts.ValidatorContract)
	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))

	data, err := p.validatorSetABI.Pack(method)
	if err != nil {
		log.Error("Unable to pack tx for getTurnLength", "error", err)
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

	if err := p.validatorSetABI.UnpackIntoInterface(&turnLength, method, result); err != nil {
		return nil, err
	}

	return turnLength, nil
}

// getRandTurnLength returns a random valid value, used to test switching turn length
func (p *Parlia) getRandTurnLength(header *types.Header) (turnLength *big.Int, err error) {
	turnLengths := [8]uint8{1, 3, 4, 5, 6, 7, 8, 9}
	r := mrand.New(mrand.NewSource(int64(header.Time)))
	lengthIndex := int(r.Int31n(int32(len(turnLengths))))
	return big.NewInt(int64(turnLengths[lengthIndex])), nil
}
