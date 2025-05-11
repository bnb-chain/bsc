package parlia

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestValidatorHeap(t *testing.T) {
	testCases := []struct {
		description string
		k           int64
		validators  []ValidatorItem
		expected    []common.Address
	}{
		{
			description: "normal case",
			k:           2,
			validators: []ValidatorItem{
				{
					address:     common.HexToAddress("0x1"),
					votingPower: new(big.Int).Mul(big.NewInt(300), big.NewInt(1e10)),
					voteAddress: []byte("0x1"),
				},
				{
					address:     common.HexToAddress("0x2"),
					votingPower: new(big.Int).Mul(big.NewInt(200), big.NewInt(1e10)),
					voteAddress: []byte("0x2"),
				},
				{
					address:     common.HexToAddress("0x3"),
					votingPower: new(big.Int).Mul(big.NewInt(100), big.NewInt(1e10)),
					voteAddress: []byte("0x3"),
				},
			},
			expected: []common.Address{
				common.HexToAddress("0x1"),
				common.HexToAddress("0x2"),
			},
		},
		{
			description: "same voting power",
			k:           2,
			validators: []ValidatorItem{
				{
					address:     common.HexToAddress("0x1"),
					votingPower: new(big.Int).Mul(big.NewInt(300), big.NewInt(1e10)),
					voteAddress: []byte("0x1"),
				},
				{
					address:     common.HexToAddress("0x2"),
					votingPower: new(big.Int).Mul(big.NewInt(100), big.NewInt(1e10)),
					voteAddress: []byte("0x2"),
				},
				{
					address:     common.HexToAddress("0x3"),
					votingPower: new(big.Int).Mul(big.NewInt(100), big.NewInt(1e10)),
					voteAddress: []byte("0x3"),
				},
			},
			expected: []common.Address{
				common.HexToAddress("0x1"),
				common.HexToAddress("0x2"),
			},
		},
		{
			description: "zero voting power and k > len(validators)",
			k:           5,
			validators: []ValidatorItem{
				{
					address:     common.HexToAddress("0x1"),
					votingPower: new(big.Int).Mul(big.NewInt(300), big.NewInt(1e10)),
					voteAddress: []byte("0x1"),
				},
				{
					address:     common.HexToAddress("0x2"),
					votingPower: big.NewInt(0),
					voteAddress: []byte("0x2"),
				},
				{
					address:     common.HexToAddress("0x3"),
					votingPower: big.NewInt(0),
					voteAddress: []byte("0x3"),
				},
				{
					address:     common.HexToAddress("0x4"),
					votingPower: big.NewInt(0),
					voteAddress: []byte("0x4"),
				},
			},
			expected: []common.Address{
				common.HexToAddress("0x1"),
			},
		},
		{
			description: "zero voting power and k < len(validators)",
			k:           2,
			validators: []ValidatorItem{
				{
					address:     common.HexToAddress("0x1"),
					votingPower: new(big.Int).Mul(big.NewInt(300), big.NewInt(1e10)),
					voteAddress: []byte("0x1"),
				},
				{
					address:     common.HexToAddress("0x2"),
					votingPower: big.NewInt(0),
					voteAddress: []byte("0x2"),
				},
				{
					address:     common.HexToAddress("0x3"),
					votingPower: big.NewInt(0),
					voteAddress: []byte("0x3"),
				},
				{
					address:     common.HexToAddress("0x4"),
					votingPower: big.NewInt(0),
					voteAddress: []byte("0x4"),
				},
			},
			expected: []common.Address{
				common.HexToAddress("0x1"),
			},
		},
		{
			description: "all zero voting power",
			k:           2,
			validators: []ValidatorItem{
				{
					address:     common.HexToAddress("0x1"),
					votingPower: big.NewInt(0),
					voteAddress: []byte("0x1"),
				},
				{
					address:     common.HexToAddress("0x2"),
					votingPower: big.NewInt(0),
					voteAddress: []byte("0x2"),
				},
				{
					address:     common.HexToAddress("0x3"),
					votingPower: big.NewInt(0),
					voteAddress: []byte("0x3"),
				},
				{
					address:     common.HexToAddress("0x4"),
					votingPower: big.NewInt(0),
					voteAddress: []byte("0x4"),
				},
			},
			expected: []common.Address{},
		},
	}
	for _, tc := range testCases {
		eligibleValidators, _, _ := getTopValidatorsByVotingPower(tc.validators, big.NewInt(tc.k))

		// check
		if len(eligibleValidators) != len(tc.expected) {
			t.Errorf("expected %d, got %d", len(tc.expected), len(eligibleValidators))
		}
		for i := 0; i < len(tc.expected); i++ {
			if eligibleValidators[i] != tc.expected[i] {
				t.Errorf("expected %s, got %s", tc.expected[i].Hex(), eligibleValidators[i].Hex())
			}
		}
	}
}
