package core

import (
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	gokzg4844 "github.com/crate-crypto/go-kzg-4844"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

var (
	emptyBlob          = kzg4844.Blob{}
	emptyBlobCommit, _ = kzg4844.BlobToCommitment(&emptyBlob)
	emptyBlobProof, _  = kzg4844.ComputeBlobProof(&emptyBlob, emptyBlobCommit)
)

func TestIsDataAvailable(t *testing.T) {
	hr := NewMockDAHeaderReader(params.ParliaTestChainConfig)
	tests := []struct {
		block             *types.Block
		chasingHeadNumber uint64
		chasingHeadTime   uint64
		withSidecar       bool
		err               bool
	}{
		{
			block: types.NewBlockWithHeader(&types.Header{
				Number: big.NewInt(1),
			}).WithBody(types.Body{Transactions: types.Transactions{
				createMockDATx(hr.Config(), nil),
				createMockDATx(hr.Config(), &types.BlobTxSidecar{
					Blobs:       []kzg4844.Blob{emptyBlob},
					Commitments: []kzg4844.Commitment{emptyBlobCommit},
					Proofs:      []kzg4844.Proof{emptyBlobProof},
				}),
			}}),
			chasingHeadNumber: 1,
			chasingHeadTime:   params.MinTimeDurationForBlobRequests - 1,
			withSidecar:       true,
			err:               false,
		},
		{
			block: types.NewBlockWithHeader(&types.Header{
				Number: big.NewInt(1),
			}).WithBody(types.Body{Transactions: types.Transactions{
				createMockDATx(hr.Config(), nil),
				createMockDATx(hr.Config(), nil),
			}}),
			chasingHeadNumber: 1,
			chasingHeadTime:   params.MinTimeDurationForBlobRequests - 1,
			withSidecar:       true,
			err:               false,
		},
		{
			block: types.NewBlockWithHeader(&types.Header{
				Number: big.NewInt(1),
			}).WithBody(types.Body{Transactions: types.Transactions{
				createMockDATx(hr.Config(), nil),
				createMockDATx(hr.Config(), &types.BlobTxSidecar{
					Blobs:       []kzg4844.Blob{emptyBlob, emptyBlob, emptyBlob},
					Commitments: []kzg4844.Commitment{emptyBlobCommit, emptyBlobCommit, emptyBlobCommit},
					Proofs:      []kzg4844.Proof{emptyBlobProof},
				}),
			}}),
			chasingHeadNumber: 1,
			chasingHeadTime:   params.MinTimeDurationForBlobRequests - 1,
			withSidecar:       false,
			err:               true,
		},
		{
			block: types.NewBlockWithHeader(&types.Header{
				Number: big.NewInt(1),
			}).WithBody(types.Body{Transactions: types.Transactions{
				createMockDATx(hr.Config(), nil),
				createMockDATx(hr.Config(), &types.BlobTxSidecar{
					Blobs:       []kzg4844.Blob{emptyBlob},
					Commitments: []kzg4844.Commitment{emptyBlobCommit},
					Proofs:      []kzg4844.Proof{emptyBlobProof},
				}),
				createMockDATx(hr.Config(), &types.BlobTxSidecar{
					Blobs:       []kzg4844.Blob{emptyBlob, emptyBlob},
					Commitments: []kzg4844.Commitment{emptyBlobCommit, emptyBlobCommit},
					Proofs:      []kzg4844.Proof{emptyBlobProof, emptyBlobProof},
				}),
			}}),
			chasingHeadNumber: 1,
			chasingHeadTime:   params.MinTimeDurationForBlobRequests - 1,
			withSidecar:       true,
			err:               false,
		},

		{
			block: types.NewBlockWithHeader(&types.Header{
				Number: big.NewInt(1),
			}).WithBody(types.Body{Transactions: types.Transactions{
				createMockDATx(hr.Config(), nil),
				createMockDATx(hr.Config(), &types.BlobTxSidecar{
					Blobs:       []kzg4844.Blob{emptyBlob, emptyBlob, emptyBlob, emptyBlob},
					Commitments: []kzg4844.Commitment{emptyBlobCommit, emptyBlobCommit, emptyBlobCommit, emptyBlobCommit},
					Proofs:      []kzg4844.Proof{emptyBlobProof, emptyBlobProof, emptyBlobProof, emptyBlobProof},
				}),
				createMockDATx(hr.Config(), &types.BlobTxSidecar{
					Blobs:       []kzg4844.Blob{emptyBlob, emptyBlob, emptyBlob, emptyBlob},
					Commitments: []kzg4844.Commitment{emptyBlobCommit, emptyBlobCommit, emptyBlobCommit, emptyBlobCommit},
					Proofs:      []kzg4844.Proof{emptyBlobProof, emptyBlobProof, emptyBlobProof, emptyBlobProof},
				}),
			}}),
			chasingHeadNumber: params.MinBlocksForBlobRequests + 1,
			chasingHeadTime:   params.MinTimeDurationForBlobRequests,
			withSidecar:       true,
			err:               true,
		},
		{
			block: types.NewBlockWithHeader(&types.Header{
				Number: big.NewInt(1),
			}).WithBody(types.Body{Transactions: types.Transactions{
				createMockDATx(hr.Config(), nil),
				createMockDATx(hr.Config(), &types.BlobTxSidecar{
					Blobs:       []kzg4844.Blob{emptyBlob},
					Commitments: []kzg4844.Commitment{emptyBlobCommit},
					Proofs:      []kzg4844.Proof{emptyBlobProof},
				}),
			}}),
			chasingHeadNumber: params.MinBlocksForBlobRequests + 1,
			chasingHeadTime:   params.MinTimeDurationForBlobRequests + 1,
			withSidecar:       false,
			err:               false,
		},
	}

	for i, item := range tests {
		if item.withSidecar {
			item.block = item.block.WithSidecars(collectBlobsFromTxs(item.block.Header(), item.block.Transactions()))
		}
		hr.setChasingHead(item.chasingHeadNumber, item.chasingHeadTime)
		err := IsDataAvailable(hr, item.block)
		if item.err {
			require.Error(t, err, i)
			t.Log(err)
			continue
		}
		require.NoError(t, err, i)
	}
}

func TestCheckDataAvailableInBatch(t *testing.T) {
	hr := NewMockDAHeaderReader(params.ParliaTestChainConfig)
	tests := []struct {
		chain types.Blocks
		err   bool
		index int
	}{
		{
			chain: types.Blocks{
				types.NewBlockWithHeader(&types.Header{
					Number: big.NewInt(1),
				}).WithBody(types.Body{Transactions: types.Transactions{
					createMockDATx(hr.Config(), nil),
					createMockDATx(hr.Config(), &types.BlobTxSidecar{
						Blobs:       []kzg4844.Blob{emptyBlob},
						Commitments: []kzg4844.Commitment{emptyBlobCommit},
						Proofs:      []kzg4844.Proof{emptyBlobProof},
					}),
					createMockDATx(hr.Config(), &types.BlobTxSidecar{
						Blobs:       []kzg4844.Blob{emptyBlob, emptyBlob},
						Commitments: []kzg4844.Commitment{emptyBlobCommit, emptyBlobCommit},
						Proofs:      []kzg4844.Proof{emptyBlobProof, emptyBlobProof},
					}),
				}}),
				types.NewBlockWithHeader(&types.Header{
					Number: big.NewInt(2),
				}).WithBody(types.Body{Transactions: types.Transactions{
					createMockDATx(hr.Config(), &types.BlobTxSidecar{
						Blobs:       []kzg4844.Blob{emptyBlob, emptyBlob},
						Commitments: []kzg4844.Commitment{emptyBlobCommit, emptyBlobCommit},
						Proofs:      []kzg4844.Proof{emptyBlobProof, emptyBlobProof},
					}),
				}}),
			},
			err: false,
		},
		{
			chain: types.Blocks{
				types.NewBlockWithHeader(&types.Header{
					Number: big.NewInt(1),
				}).WithBody(types.Body{Transactions: types.Transactions{
					createMockDATx(hr.Config(), &types.BlobTxSidecar{
						Blobs:       []kzg4844.Blob{emptyBlob},
						Commitments: []kzg4844.Commitment{emptyBlobCommit},
						Proofs:      []kzg4844.Proof{emptyBlobProof},
					}),
				}}),
				types.NewBlockWithHeader(&types.Header{
					Number: big.NewInt(2),
				}).WithBody(types.Body{Transactions: types.Transactions{
					createMockDATx(hr.Config(), &types.BlobTxSidecar{
						Blobs:       []kzg4844.Blob{emptyBlob, emptyBlob, emptyBlob},
						Commitments: []kzg4844.Commitment{emptyBlobCommit, emptyBlobCommit, emptyBlobCommit},
						Proofs:      []kzg4844.Proof{emptyBlobProof},
					}),
				}}),
				types.NewBlockWithHeader(&types.Header{
					Number: big.NewInt(3),
				}).WithBody(types.Body{Transactions: types.Transactions{
					createMockDATx(hr.Config(), &types.BlobTxSidecar{
						Blobs:       []kzg4844.Blob{emptyBlob},
						Commitments: []kzg4844.Commitment{emptyBlobCommit},
						Proofs:      []kzg4844.Proof{emptyBlobProof},
					}),
				}}),
			},
			err:   true,
			index: 1,
		},
		{
			chain: types.Blocks{
				types.NewBlockWithHeader(&types.Header{
					Number: big.NewInt(1),
				}).WithBody(types.Body{Transactions: types.Transactions{
					createMockDATx(hr.Config(), nil),
					createMockDATx(hr.Config(), &types.BlobTxSidecar{
						Blobs:       []kzg4844.Blob{emptyBlob, emptyBlob, emptyBlob, emptyBlob},
						Commitments: []kzg4844.Commitment{emptyBlobCommit, emptyBlobCommit, emptyBlobCommit, emptyBlobCommit},
						Proofs:      []kzg4844.Proof{emptyBlobProof, emptyBlobProof, emptyBlobProof, emptyBlobProof},
					}),
					createMockDATx(hr.Config(), &types.BlobTxSidecar{
						Blobs:       []kzg4844.Blob{emptyBlob, emptyBlob, emptyBlob, emptyBlob},
						Commitments: []kzg4844.Commitment{emptyBlobCommit, emptyBlobCommit, emptyBlobCommit, emptyBlobCommit},
						Proofs:      []kzg4844.Proof{emptyBlobProof, emptyBlobProof, emptyBlobProof, emptyBlobProof},
					}),
				}}),
			},
			err:   true,
			index: 0,
		},
	}

	for i, item := range tests {
		for j, block := range item.chain {
			item.chain[j] = block.WithSidecars(collectBlobsFromTxs(block.Header(), block.Transactions()))
		}
		index, err := CheckDataAvailableInBatch(hr, item.chain)
		if item.err {
			t.Log(index, err)
			require.Error(t, err, i)
			require.Equal(t, item.index, index, i)
			continue
		}
		require.NoError(t, err, i)
	}
}

func BenchmarkEmptySidecarDAChecking(b *testing.B) {
	hr := NewMockDAHeaderReader(params.ParliaTestChainConfig)
	block := types.NewBlockWithHeader(&types.Header{
		Number: big.NewInt(1),
	}).WithBody(types.Body{Transactions: types.Transactions{
		createMockDATx(hr.Config(), emptySidecar()),
		createMockDATx(hr.Config(), emptySidecar()),
		createMockDATx(hr.Config(), emptySidecar()),
		createMockDATx(hr.Config(), emptySidecar()),
		createMockDATx(hr.Config(), emptySidecar()),
		createMockDATx(hr.Config(), emptySidecar()),
	}})
	block = block.WithSidecars(collectBlobsFromTxs(block.Header(), block.Transactions()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsDataAvailable(hr, block)
	}
}

func BenchmarkRandomSidecarDAChecking(b *testing.B) {
	hr := NewMockDAHeaderReader(params.ParliaTestChainConfig)
	const count = 10
	blocks := make([]*types.Block, count)
	for i := 0; i < len(blocks); i++ {
		block := types.NewBlockWithHeader(&types.Header{
			Number: big.NewInt(1),
		}).WithBody(types.Body{Transactions: types.Transactions{
			createMockDATx(hr.Config(), randomSidecar()),
			createMockDATx(hr.Config(), randomSidecar()),
			createMockDATx(hr.Config(), randomSidecar()),
			createMockDATx(hr.Config(), randomSidecar()),
			createMockDATx(hr.Config(), randomSidecar()),
			createMockDATx(hr.Config(), randomSidecar()),
		}})
		block = block.WithSidecars(collectBlobsFromTxs(block.Header(), block.Transactions()))
		blocks[i] = block
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsDataAvailable(hr, blocks[i%count])
	}
}

func collectBlobsFromTxs(header *types.Header, txs types.Transactions) types.BlobSidecars {
	sidecars := make(types.BlobSidecars, 0, len(txs))
	for i, tx := range txs {
		sidecar := types.NewBlobSidecarFromTx(tx)
		if sidecar == nil {
			continue
		}
		sidecar.TxIndex = uint64(i)
		sidecar.TxHash = tx.Hash()
		sidecar.BlockNumber = header.Number
		sidecar.BlockHash = header.Hash()
		sidecars = append(sidecars, sidecar)
	}
	return sidecars
}

type mockDAHeaderReader struct {
	config            *params.ChainConfig
	chasingHeadNumber uint64
	chasingHeadTime   uint64
}

func NewMockDAHeaderReader(config *params.ChainConfig) *mockDAHeaderReader {
	return &mockDAHeaderReader{
		config: config,
	}
}

func (r *mockDAHeaderReader) setChasingHead(number, time uint64) {
	r.chasingHeadNumber = number
	r.chasingHeadTime = time
}

func (r *mockDAHeaderReader) Config() *params.ChainConfig {
	return r.config
}

func (r *mockDAHeaderReader) CurrentHeader() *types.Header {
	return &types.Header{
		Number: new(big.Int).SetUint64(r.chasingHeadNumber),
		Time:   r.chasingHeadTime,
	}
}

func (r *mockDAHeaderReader) ChasingHead() *types.Header {
	return &types.Header{
		Number: new(big.Int).SetUint64(r.chasingHeadNumber),
		Time:   r.chasingHeadTime,
	}
}

func (r *mockDAHeaderReader) GenesisHeader() *types.Header {
	panic("not supported")
}

func (r *mockDAHeaderReader) GetHeader(hash common.Hash, number uint64) *types.Header {
	panic("not supported")
}

func (r *mockDAHeaderReader) GetHeaderByNumber(number uint64) *types.Header {
	panic("not supported")
}

func (r *mockDAHeaderReader) GetHeaderByHash(hash common.Hash) *types.Header {
	panic("not supported")
}

func (r *mockDAHeaderReader) GetTd(hash common.Hash, number uint64) *big.Int {
	panic("not supported")
}

func (r *mockDAHeaderReader) GetHighestVerifiedHeader() *types.Header {
	panic("not supported")
}

func (r *mockDAHeaderReader) GetVerifiedBlockByHash(hash common.Hash) *types.Header {
	panic("not supported")
}

func createMockDATx(config *params.ChainConfig, sidecar *types.BlobTxSidecar) *types.Transaction {
	if sidecar == nil {
		tx := &types.DynamicFeeTx{
			ChainID:   config.ChainID,
			Nonce:     0,
			GasTipCap: big.NewInt(22),
			GasFeeCap: big.NewInt(5),
			Gas:       25000,
			To:        &common.Address{0x03, 0x04, 0x05},
			Value:     big.NewInt(99),
			Data:      make([]byte, 50),
		}
		return types.NewTx(tx)
	}
	tx := &types.BlobTx{
		ChainID:    uint256.MustFromBig(config.ChainID),
		Nonce:      5,
		GasTipCap:  uint256.NewInt(22),
		GasFeeCap:  uint256.NewInt(5),
		Gas:        25000,
		To:         common.Address{0x03, 0x04, 0x05},
		Value:      uint256.NewInt(99),
		Data:       make([]byte, 50),
		BlobFeeCap: uint256.NewInt(15),
		BlobHashes: sidecar.BlobHashes(),
		Sidecar:    sidecar,
	}
	return types.NewTx(tx)
}

func randFieldElement() [32]byte {
	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	if err != nil {
		panic("failed to get random field element")
	}
	var r fr.Element
	r.SetBytes(bytes)

	return gokzg4844.SerializeScalar(r)
}

func randBlob() kzg4844.Blob {
	var blob kzg4844.Blob
	for i := 0; i < len(blob); i += gokzg4844.SerializedScalarSize {
		fieldElementBytes := randFieldElement()
		copy(blob[i:i+gokzg4844.SerializedScalarSize], fieldElementBytes[:])
	}
	return blob
}

func randomSidecar() *types.BlobTxSidecar {
	blob := randBlob()
	commitment, _ := kzg4844.BlobToCommitment(&blob)
	proof, _ := kzg4844.ComputeBlobProof(&blob, commitment)
	return &types.BlobTxSidecar{
		Blobs:       []kzg4844.Blob{blob},
		Commitments: []kzg4844.Commitment{commitment},
		Proofs:      []kzg4844.Proof{proof},
	}
}

func emptySidecar() *types.BlobTxSidecar {
	return &types.BlobTxSidecar{
		Blobs:       []kzg4844.Blob{emptyBlob},
		Commitments: []kzg4844.Commitment{emptyBlobCommit},
		Proofs:      []kzg4844.Proof{emptyBlobProof},
	}
}
