package parlia

import (
	"context"
	"math/big"
	mrand "math/rand"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

func (p *Parlia) getTurnTerm(header *types.Header) (turnTerm *big.Int, err error) {
	if params.UseRandTurnTerm {
		return p.getRandTurnTerm(header)
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
