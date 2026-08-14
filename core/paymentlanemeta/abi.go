package paymentlanemeta

import (
	_ "embed"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/paymentlane"
)

const (
	getPaymentLaneParamsMethod = "getPaymentLaneParams"
	getPaymentContractsMethod  = "getPaymentContracts"
)

// Refresh payment_lane_meta.abi.json from the contract repo with:
// forge inspect contracts/interface/0.8.x/IPaymentLaneMeta.sol:IPaymentLaneMeta abi --json
//
//go:embed payment_lane_meta.abi.json
var paymentLaneMetaABIJSON string

var paymentLaneMetaABI = mustParsePaymentLaneMetaABI()

type paymentLaneMetaParams struct {
	PaymentLaneMinRatio *big.Int
	PaymentLaneMaxRatio *big.Int
	ExpandTriggerRatio  *big.Int
	ShrinkTriggerRatio  *big.Int
	ExpandStepRatio     *big.Int
	ShrinkStepRatio     *big.Int
	PaymentLaneMin      *big.Int
	PaymentLaneMax      *big.Int
}

func mustParsePaymentLaneMetaABI() abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(paymentLaneMetaABIJSON))
	if err != nil {
		panic(err)
	}
	return parsed
}

func mustPack(name string, args ...interface{}) []byte {
	input, err := paymentLaneMetaABI.Pack(name, args...)
	if err != nil {
		panic(err)
	}
	return input
}

func packGetPaymentLaneParams() []byte {
	return mustPack(getPaymentLaneParamsMethod)
}

func packGetPaymentContracts(offset, limit uint64) []byte {
	return mustPack(getPaymentContractsMethod, new(big.Int).SetUint64(offset), new(big.Int).SetUint64(limit))
}

func unpackGetPaymentLaneParams(ret []byte) (paymentlane.Params, error) {
	values, err := paymentLaneMetaABI.Unpack(getPaymentLaneParamsMethod, ret)
	if err != nil {
		return paymentlane.Params{}, fmt.Errorf("%w: %s: %v", paymentlane.ErrCorruptConfig, getPaymentLaneParamsMethod, err)
	}
	if len(values) != 1 {
		return paymentlane.Params{}, unexpectedOutputCount(getPaymentLaneParamsMethod, len(values), 1)
	}
	params := *abi.ConvertType(values[0], new(paymentLaneMetaParams)).(*paymentLaneMetaParams)
	minRatio, err := parseUint64("getPaymentLaneParams.paymentLaneMinRatio", params.PaymentLaneMinRatio)
	if err != nil {
		return paymentlane.Params{}, err
	}
	maxRatio, err := parseUint64("getPaymentLaneParams.paymentLaneMaxRatio", params.PaymentLaneMaxRatio)
	if err != nil {
		return paymentlane.Params{}, err
	}
	expandTrigger, err := parseUint64("getPaymentLaneParams.expandTriggerRatio", params.ExpandTriggerRatio)
	if err != nil {
		return paymentlane.Params{}, err
	}
	shrinkTrigger, err := parseUint64("getPaymentLaneParams.shrinkTriggerRatio", params.ShrinkTriggerRatio)
	if err != nil {
		return paymentlane.Params{}, err
	}
	expandStep, err := parseUint64("getPaymentLaneParams.expandStepRatio", params.ExpandStepRatio)
	if err != nil {
		return paymentlane.Params{}, err
	}
	shrinkStep, err := parseUint64("getPaymentLaneParams.shrinkStepRatio", params.ShrinkStepRatio)
	if err != nil {
		return paymentlane.Params{}, err
	}
	minGas, err := parseUint64("getPaymentLaneParams.paymentLaneMin", params.PaymentLaneMin)
	if err != nil {
		return paymentlane.Params{}, err
	}
	maxGas, err := parseUint64("getPaymentLaneParams.paymentLaneMax", params.PaymentLaneMax)
	if err != nil {
		return paymentlane.Params{}, err
	}
	return paymentlane.Params{
		MinRatio:      minRatio,
		MaxRatio:      maxRatio,
		ExpandTrigger: expandTrigger,
		ShrinkTrigger: shrinkTrigger,
		ExpandStep:    expandStep,
		ShrinkStep:    shrinkStep,
		MinGas:        minGas,
		MaxGas:        maxGas,
	}, nil
}

func unpackGetPaymentContracts(ret []byte) ([]common.Address, uint64, error) {
	values, err := paymentLaneMetaABI.Unpack(getPaymentContractsMethod, ret)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %s: %v", paymentlane.ErrCorruptConfig, getPaymentContractsMethod, err)
	}
	if len(values) != 2 {
		return nil, 0, unexpectedOutputCount(getPaymentContractsMethod, len(values), 2)
	}
	paymentContracts := *abi.ConvertType(values[0], new([]common.Address)).(*[]common.Address)
	totalLength := *abi.ConvertType(values[1], new(*big.Int)).(**big.Int)
	total, err := parseUint64("getPaymentContracts.totalLength", totalLength)
	if err != nil {
		return nil, 0, err
	}
	return paymentContracts, total, nil
}

func unexpectedOutputCount(method string, got, want int) error {
	return fmt.Errorf("%w: %s returned %d values, want %d", paymentlane.ErrCorruptConfig, method, got, want)
}

func parseUint64(name string, v *big.Int) (uint64, error) {
	if v == nil || !v.IsUint64() {
		return 0, fmt.Errorf("%w: %s does not fit uint64: %v", paymentlane.ErrCorruptConfig, name, v)
	}
	return v.Uint64(), nil
}
