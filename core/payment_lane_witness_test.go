package core

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/systemcontracts/gauss"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

// TestPaymentLaneReadsReachTheWitness proves the loader stays on the witness-visible StateDB
// path. A raw Reader read would miss 0x2007's account node and, once governance has written
// anything, its storage nodes too. The two sub-cases cover both defaults-only and governed
// storage.
func TestPaymentLaneReadsReachTheWitness(t *testing.T) {
	for _, tc := range []struct {
		name    string
		storage map[common.Hash]common.Hash
		minGas  uint64
	}{
		{"0x2007 at its shipped defaults", nil, 2_000_000},
		{"0x2007 with governed parameters", map[common.Hash]common.Hash{
			{31: 6}: common.BigToHash(big.NewInt(3_000_000)), // slot 6 is MinGas
		}, 3_000_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, err := hex.DecodeString(strings.TrimSpace(gauss.RialtoPaymentLaneContract))
			require.NoError(t, err)

			config := *params.AllEthashProtocolChanges
			gaussTime := uint64(15)
			config.GaussTime = &gaussTime

			key := newKey(t)
			gspec := &Genesis{
				Config:   &config,
				GasLimit: laneTestGasLimit,
				Alloc: types.GenesisAlloc{
					paymentlane.ContractAddress: {Code: code, Balance: common.Big0, Storage: tc.storage},
					key.addr:                    {Balance: new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1e6))},
				},
			}

			signer := types.LatestSigner(&config)
			var nonce uint64
			_, blocks, _ := GenerateChainWithGenesis(gspec, ethash.NewFaker(), 4, func(i int, b *BlockGen) {
				b.AddTx(key.sign(t, signer, nonce, common.Address{0xaa}, big.NewInt(1), params.TxGas, nil))
				nonce++
			})

			// Premise: the parameters really did come out of 0x2007's storage, so the witness has
			// something to be missing.
			c, err := paymentlane.Decode(blocks[3].UncleHash())
			require.NoError(t, err)
			require.EqualValues(t, tc.minGas, c.LaneSize, "the lane floor must reflect the storage under test")

			cfg := DefaultConfig()
			cfg.StatelessSelfValidation = true
			chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), gspec, ethash.NewFaker(), cfg)
			require.NoError(t, err)
			defer chain.Stop()

			n, err := chain.InsertChain(blocks)
			require.NoError(t, err, "witness replay must serve the lane's 0x2007 reads; failed after %d blocks", n)
		})
	}
}
