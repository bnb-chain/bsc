package catalyst

import (
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/params/forks"
)

// TestPayloadVersionJenner pins down that payloadVersion handles the Jenner
// fork: LatestFork returns Jenner once it activates, and without a case for
// it the switch used to fall through to the post-merge panic — on the block
// production path (sealPayload). Jenner is a V3 fork sitting right after
// Pasteur (not the newest fork), so it reports PayloadV3.
func TestPayloadVersionJenner(t *testing.T) {
	jennerTime := uint64(1_000)
	config := *params.ParliaTestChainConfig // Parlia: the IsJenner BSC gate passes
	config.JennerTime = &jennerTime

	// Sanity: with no later fork scheduled, Jenner is the active fork.
	if got := config.LatestFork(jennerTime); got != forks.Jenner {
		t.Fatalf("LatestFork = %v, want Jenner", got)
	}
	// Before activation: unchanged (Cancun on this config => V3).
	if v := payloadVersion(&config, jennerTime-1); v != engine.PayloadV3 {
		t.Fatalf("pre-Jenner payloadVersion = %v, want V3", v)
	}
	// At/after activation: Jenner is a V3 fork and must not panic.
	if v := payloadVersion(&config, jennerTime); v != engine.PayloadV3 {
		t.Fatalf("Jenner payloadVersion = %v, want V3", v)
	}
}
