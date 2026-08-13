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
// production path (sealPayload).
func TestPayloadVersionJenner(t *testing.T) {
	jennerTime := uint64(1_000)
	config := *params.ParliaTestChainConfig // Parlia: the IsJenner BSC gate passes
	config.JennerTime = &jennerTime

	// Sanity: Jenner is the latest fork once activated.
	if got := config.LatestFork(jennerTime); got != forks.Jenner {
		t.Fatalf("LatestFork = %v, want Jenner", got)
	}
	// Before activation: unchanged (Cancun on this config => V3).
	if v := payloadVersion(&config, jennerTime-1); v != engine.PayloadV3 {
		t.Fatalf("pre-Jenner payloadVersion = %v, want V3", v)
	}
	// Jenner on top of a pre-Amsterdam fork set keeps V3 (it does not change
	// the Engine payload version) — and must not panic.
	if v := payloadVersion(&config, jennerTime); v != engine.PayloadV3 {
		t.Fatalf("Jenner payloadVersion = %v, want V3", v)
	}
	// Jenner on top of an active Amsterdam keeps Amsterdam's V4.
	amsterdamTime := jennerTime - 100
	config.AmsterdamTime = &amsterdamTime
	if v := payloadVersion(&config, jennerTime); v != engine.PayloadV4 {
		t.Fatalf("Jenner-over-Amsterdam payloadVersion = %v, want V4", v)
	}
}
