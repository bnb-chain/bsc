package monitor

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
)

func TestMaliciousVoteMonitor(t *testing.T) {
	//log.Root().SetHandler(log.StdoutHandler)
	// case 1, different voteAddress
	{
		maliciousVoteMonitor := NewMaliciousVoteMonitor()
		pendingBlockNumber := uint64(1000)
		voteAddrBytes := common.Hex2BytesFixed("000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001", types.BLSPublicKeyLength)
		voteAddress := types.BLSPublicKey{}
		copy(voteAddress[:], voteAddrBytes[:])
		vote1 := &types.VoteEnvelope{
			VoteAddress: voteAddress,
			Signature:   types.BLSSignature{},
			Data: &types.VoteData{
				SourceNumber: uint64(0),
				SourceHash:   common.BytesToHash(common.Hex2Bytes("00")),
				TargetNumber: pendingBlockNumber - maliciousVoteSlashScope - 1,
				TargetHash:   common.BytesToHash(common.Hex2Bytes(("01"))),
			},
		}
		assert.Equal(t, false, maliciousVoteMonitor.ConflictDetect(vote1, pendingBlockNumber))
		voteAddress[0] = 4
		vote2 := &types.VoteEnvelope{
			VoteAddress: voteAddress,
			Signature:   types.BLSSignature{},
			Data: &types.VoteData{
				SourceNumber: uint64(0),
				SourceHash:   common.BytesToHash(common.Hex2Bytes("00")),
				TargetNumber: pendingBlockNumber - maliciousVoteSlashScope - 1,
				TargetHash:   common.BytesToHash(common.Hex2Bytes("02")),
			},
		}
		assert.Equal(t, false, maliciousVoteMonitor.ConflictDetect(vote2, pendingBlockNumber))
	}

	// case 2, target number not in maliciousVoteSlashScope
	{
		maliciousVoteMonitor := NewMaliciousVoteMonitor()
		pendingBlockNumber := uint64(1000)
		voteAddrBytes := common.Hex2BytesFixed("000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001", types.BLSPublicKeyLength)
		voteAddress := types.BLSPublicKey{}
		copy(voteAddress[:], voteAddrBytes[:])
		vote1 := &types.VoteEnvelope{
			VoteAddress: voteAddress,
			Signature:   types.BLSSignature{},
			Data: &types.VoteData{
				SourceNumber: uint64(0),
				SourceHash:   common.BytesToHash(common.Hex2Bytes("00")),
				TargetNumber: pendingBlockNumber - maliciousVoteSlashScope - 1,
				TargetHash:   common.BytesToHash(common.Hex2Bytes("01")),
			},
		}
		assert.Equal(t, false, maliciousVoteMonitor.ConflictDetect(vote1, pendingBlockNumber))
		vote2 := &types.VoteEnvelope{
			VoteAddress: voteAddress,
			Signature:   types.BLSSignature{},
			Data: &types.VoteData{
				SourceNumber: uint64(0),
				SourceHash:   common.BytesToHash(common.Hex2Bytes("00")),
				TargetNumber: pendingBlockNumber - maliciousVoteSlashScope - 1,
				TargetHash:   common.BytesToHash(common.Hex2Bytes("02")),
			},
		}
		assert.Equal(t, false, maliciousVoteMonitor.ConflictDetect(vote2, pendingBlockNumber))
	}

	// case 3, violate rule1
	{
		maliciousVoteMonitor := NewMaliciousVoteMonitor()
		pendingBlockNumber := uint64(1000)
		voteAddrBytes := common.Hex2BytesFixed("000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001", types.BLSPublicKeyLength)
		voteAddress := types.BLSPublicKey{}
		copy(voteAddress[:], voteAddrBytes[:])
		vote1 := &types.VoteEnvelope{
			VoteAddress: voteAddress,
			Signature:   types.BLSSignature{},
			Data: &types.VoteData{
				SourceNumber: uint64(0),
				SourceHash:   common.BytesToHash(common.Hex2Bytes("00")),
				TargetNumber: pendingBlockNumber - 1,
				TargetHash:   common.BytesToHash(common.Hex2Bytes("01")),
			},
		}
		assert.Equal(t, false, maliciousVoteMonitor.ConflictDetect(vote1, pendingBlockNumber))
		vote2 := &types.VoteEnvelope{
			VoteAddress: voteAddress,
			Signature:   types.BLSSignature{},
			Data: &types.VoteData{
				SourceNumber: uint64(0),
				SourceHash:   common.BytesToHash(common.Hex2Bytes("00")),
				TargetNumber: pendingBlockNumber - 1,
				TargetHash:   common.BytesToHash(common.Hex2Bytes("02")),
			},
		}
		assert.Equal(t, true, maliciousVoteMonitor.ConflictDetect(vote2, pendingBlockNumber))
	}

	// case 4,  violate rule2, vote with smaller range first
	{
		maliciousVoteMonitor := NewMaliciousVoteMonitor()
		pendingBlockNumber := uint64(1000)
		voteAddrBytes := common.Hex2BytesFixed("000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001", types.BLSPublicKeyLength)
		voteAddress := types.BLSPublicKey{}
		copy(voteAddress[:], voteAddrBytes[:])
		vote1 := &types.VoteEnvelope{
			VoteAddress: voteAddress,
			Signature:   types.BLSSignature{},
			Data: &types.VoteData{
				SourceNumber: pendingBlockNumber - 4,
				SourceHash:   common.BytesToHash(common.Hex2Bytes("00")),
				TargetNumber: pendingBlockNumber - 1,
				TargetHash:   common.BytesToHash(common.Hex2Bytes("01")),
			},
		}
		assert.Equal(t, false, maliciousVoteMonitor.ConflictDetect(vote1, pendingBlockNumber))
		vote2 := &types.VoteEnvelope{
			VoteAddress: voteAddress,
			Signature:   types.BLSSignature{},
			Data: &types.VoteData{
				SourceNumber: pendingBlockNumber - 2,
				SourceHash:   common.BytesToHash(common.Hex2Bytes("00")),
				TargetNumber: pendingBlockNumber - 3,
				TargetHash:   common.BytesToHash(common.Hex2Bytes("02")),
			},
		}
		assert.Equal(t, true, maliciousVoteMonitor.ConflictDetect(vote2, pendingBlockNumber))
	}

	// case 5,  violate rule2, vote with larger range first
	{
		maliciousVoteMonitor := NewMaliciousVoteMonitor()
		pendingBlockNumber := uint64(1000)
		voteAddrBytes := common.Hex2BytesFixed("000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001", types.BLSPublicKeyLength)
		voteAddress := types.BLSPublicKey{}
		copy(voteAddress[:], voteAddrBytes[:])
		vote1 := &types.VoteEnvelope{
			VoteAddress: voteAddress,
			Signature:   types.BLSSignature{},
			Data: &types.VoteData{
				SourceNumber: pendingBlockNumber - 2,
				SourceHash:   common.BytesToHash(common.Hex2Bytes("00")),
				TargetNumber: pendingBlockNumber - 3,
				TargetHash:   common.BytesToHash(common.Hex2Bytes("01")),
			},
		}
		assert.Equal(t, false, maliciousVoteMonitor.ConflictDetect(vote1, pendingBlockNumber))
		vote2 := &types.VoteEnvelope{
			VoteAddress: voteAddress,
			Signature:   types.BLSSignature{},
			Data: &types.VoteData{
				SourceNumber: pendingBlockNumber - 4,
				SourceHash:   common.BytesToHash(common.Hex2Bytes("00")),
				TargetNumber: pendingBlockNumber - 1,
				TargetHash:   common.BytesToHash(common.Hex2Bytes("02")),
			},
		}
		assert.Equal(t, true, maliciousVoteMonitor.ConflictDetect(vote2, pendingBlockNumber))
	}

	// case 6, normal case
	{
		maliciousVoteMonitor := NewMaliciousVoteMonitor()
		pendingBlockNumber := uint64(1000)
		voteAddrBytes := common.Hex2BytesFixed("000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001", types.BLSPublicKeyLength)
		voteAddress := types.BLSPublicKey{}
		copy(voteAddress[:], voteAddrBytes[:])
		vote1 := &types.VoteEnvelope{
			VoteAddress: voteAddress,
			Signature:   types.BLSSignature{},
			Data: &types.VoteData{
				SourceNumber: pendingBlockNumber - 4,
				SourceHash:   common.BytesToHash(common.Hex2Bytes("00")),
				TargetNumber: pendingBlockNumber - 3,
				TargetHash:   common.BytesToHash(common.Hex2Bytes("01")),
			},
		}
		assert.Equal(t, false, maliciousVoteMonitor.ConflictDetect(vote1, pendingBlockNumber))
		vote2 := &types.VoteEnvelope{
			VoteAddress: voteAddress,
			Signature:   types.BLSSignature{},
			Data: &types.VoteData{
				SourceNumber: pendingBlockNumber - 3,
				SourceHash:   common.BytesToHash(common.Hex2Bytes("00")),
				TargetNumber: pendingBlockNumber - 2,
				TargetHash:   common.BytesToHash(common.Hex2Bytes("02")),
			},
		}
		assert.Equal(t, false, maliciousVoteMonitor.ConflictDetect(vote2, pendingBlockNumber))
		vote3 := &types.VoteEnvelope{
			VoteAddress: voteAddress,
			Signature:   types.BLSSignature{},
			Data: &types.VoteData{
				SourceNumber: pendingBlockNumber - 2,
				SourceHash:   common.BytesToHash(common.Hex2Bytes("00")),
				TargetNumber: pendingBlockNumber - 1,
				TargetHash:   common.BytesToHash(common.Hex2Bytes("02")),
			},
		}
		assert.Equal(t, false, maliciousVoteMonitor.ConflictDetect(vote3, pendingBlockNumber))
	}
}
