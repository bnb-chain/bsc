package ethapi

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/internal/ethapi/override"
	"github.com/ethereum/go-ethereum/rpc"
)

func TestEstimateGasPrevRandaoOnParlia(t *testing.T) {
	for _, fork := range []string{"beforeJenner", "afterJenner"} {
		t.Run(fork, func(t *testing.T) {
			var backend *testBackend
			if fork == "beforeJenner" {
				config := bscJennerTestConfig()
				config.JennerTime = nil
				backend = newTestBackend(t, 0, &core.Genesis{
					Config:     config,
					Difficulty: big.NewInt(2),
					Alloc:      types.GenesisAlloc{},
				}, ethash.NewFullFaker(), nil)
			} else {
				backend = newJennerBSCBackend(t)
			}
			defer backend.chain.Stop()
			api := NewBlockChainAPI(backend)
			latest := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
			head := backend.CurrentHeader()
			if head.Difficulty.Sign() == 0 {
				t.Fatal("test requires a non-zero-difficulty Parlia header")
			}
			// Revert unless opcode 0x44 equals the first calldata word.
			code := hexutil.Bytes(common.FromHex("4460003514600d5760006000fd5b00"))
			addr := common.HexToAddress("0x3333")
			state := override.StateOverride{addr: {Code: &code}}
			zero := common.Hash{}
			randao := common.BigToHash(big.NewInt(500))
			timestamp := hexutil.Uint64(head.Time + 1)
			difficulty := (*hexutil.Big)(big.NewInt(2))
			for _, tc := range []struct {
				name      string
				overrides *override.BlockOverrides
				want      common.Hash
			}{
				{"noOverrides", nil, common.BigToHash(head.Difficulty)},
				{"timeOnly", &override.BlockOverrides{Time: &timestamp}, common.BigToHash(head.Difficulty)},
				{"difficultyOnly", &override.BlockOverrides{Difficulty: difficulty}, common.BigToHash(big.NewInt(2))},
				{"zeroRandao", &override.BlockOverrides{PrevRandao: &zero}, zero},
				{"randao", &override.BlockOverrides{PrevRandao: &randao}, randao},
				{"timeAndRandao", &override.BlockOverrides{Time: &timestamp, PrevRandao: &randao}, randao},
				{"difficultyAndRandao", &override.BlockOverrides{Difficulty: difficulty, PrevRandao: &randao}, randao},
			} {
				t.Run(tc.name, func(t *testing.T) {
					input := hexutil.Bytes(tc.want.Bytes())
					args := TransactionArgs{To: &addr, Input: &input}
					if _, err := api.Call(context.Background(), args, &latest, &state, tc.overrides); err != nil {
						t.Fatalf("eth_call: %v", err)
					}
					if _, err := api.EstimateGas(context.Background(), args, &latest, &state, tc.overrides); err != nil {
						t.Fatalf("eth_estimateGas: %v", err)
					}
					wrong := new(big.Int).Add(tc.want.Big(), big.NewInt(1))
					input = common.LeftPadBytes(wrong.Bytes(), 32)
					if _, err := api.EstimateGas(context.Background(), args, &latest, &state, tc.overrides); err == nil {
						t.Fatal("estimate must revert when the opcode value does not match")
					}
				})
			}
		})
	}
}
