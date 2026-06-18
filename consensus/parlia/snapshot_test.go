package parlia

import (
	"bytes"
	"math/big"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

func TestValidatorSetSort(t *testing.T) {
	size := 100
	validators := make([]common.Address, size)
	for i := 0; i < size; i++ {
		validators[i] = randomAddress()
	}
	sort.Sort(validatorsAscending(validators))
	for i := 0; i < size-1; i++ {
		assert.True(t, bytes.Compare(validators[i][:], validators[i+1][:]) < 0)
	}
}

// bohrChainConfig returns a ParliaTestChainConfig copy with the Bohr fork active
// from genesis, so that parseTurnLength runs its turn-length parsing branch.
func bohrChainConfig() *params.ChainConfig {
	cfg := *params.ParliaTestChainConfig
	bohr := uint64(0)
	cfg.BohrTime = &bohr
	return &cfg
}

// buildTurnLengthHeader builds a Bohr-active epoch checkpoint header whose
// extra-data encodes numValidators validators followed by the given turn-length
// byte (mirroring the on-wire layout parseTurnLength expects).
func buildTurnLengthHeader(number uint64, numValidators int, turnLength byte) *types.Header {
	pos := extraVanity + validatorNumberSize + numValidators*validatorBytesLength
	extra := make([]byte, pos+1+extraSeal)
	extra[extraVanity] = byte(numValidators) // validator count
	extra[pos] = turnLength                  // attacker-controlled turn-length byte
	return &types.Header{
		Number: new(big.Int).SetUint64(number),
		Time:   0,
		Extra:  extra,
	}
}

// TestParseTurnLengthRejectsZero is the H1 regression test: a malicious epoch
// checkpoint header that carries a zero turn-length byte must be rejected by
// parseTurnLength. Before the fix this returns (ptr->0, nil); the zero then flows
// into Snapshot.TurnLength and later triggers an integer divide-by-zero panic in
// inturnValidator/backOffTime on the header-verification path (chain-halt DoS),
// because the only sanity check (verifyTurnLength) lives in Finalize and is
// bypassed by header-only sync.
func TestParseTurnLengthRejectsZero(t *testing.T) {
	cfg := bohrChainConfig()
	header := buildTurnLengthHeader(defaultEpochLength, 1, 0)

	_, err := parseTurnLength(header, cfg, defaultEpochLength)
	assert.ErrorIs(t, err, errInvalidTurnLength,
		"parseTurnLength must reject a zero turn-length from attacker-controlled extra-data")
}
