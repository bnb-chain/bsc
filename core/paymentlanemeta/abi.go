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
	getPaymentLaneRatioMethod = "getPaymentLaneRatio"
	getPaymentContractsMethod = "getPaymentContracts"
)

// Refresh payment_lane_meta.abi.json from the contract repo with:
// forge inspect contracts/interface/0.8.x/IPaymentLaneMeta.sol:IPaymentLaneMeta abi --json
//
//go:embed payment_lane_meta.abi.json
var paymentLaneMetaABIJSON string

var paymentLaneMetaABI = mustParsePaymentLaneMetaABI()

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

func packGetPaymentLaneRatio() []byte {
	return mustPack(getPaymentLaneRatioMethod)
}

func packGetPaymentContracts(offset, limit uint64) []byte {
	return mustPack(getPaymentContractsMethod, new(big.Int).SetUint64(offset), new(big.Int).SetUint64(limit))
}

// unpackGetPaymentLaneRatio applies section 3.6.1's guard to the returned uint256 before it is
// ever narrowed, so a value that only fits the guard once truncated is still rejected.
func unpackGetPaymentLaneRatio(ret []byte) (uint64, error) {
	values, err := paymentLaneMetaABI.Unpack(getPaymentLaneRatioMethod, ret)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %v", paymentlane.ErrCorruptConfig, getPaymentLaneRatioMethod, err)
	}
	if len(values) != 1 {
		return 0, unexpectedOutputCount(getPaymentLaneRatioMethod, len(values), 1)
	}
	return paymentlane.CheckRatio(*abi.ConvertType(values[0], new(*big.Int)).(**big.Int))
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
	if totalLength == nil || !totalLength.IsUint64() {
		return nil, 0, fmt.Errorf("%w: getPaymentContracts.totalLength does not fit uint64: %v", paymentlane.ErrCorruptConfig, totalLength)
	}
	return paymentContracts, totalLength.Uint64(), nil
}

func unexpectedOutputCount(method string, got, want int) error {
	return fmt.Errorf("%w: %s returned %d values, want %d", paymentlane.ErrCorruptConfig, method, got, want)
}
