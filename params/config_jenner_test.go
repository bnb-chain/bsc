package params

import (
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/params/forks"
)

// testJennerTime is an arbitrary activation time used by the Jenner tests. It
// is intentionally later than every fork time configured on the BSC networks
// so that appending it keeps the fork ordering monotonic.
const testJennerTime = uint64(4102444800) // 2100-01-01 00:00:00 UTC

// newJennerTestConfig returns a minimal Parlia (BSC-like) config with the
// Jenner fork scheduled at testJennerTime.
func newJennerTestConfig() *ChainConfig {
	return &ChainConfig{
		LondonBlock: big.NewInt(0),
		JennerTime:  newUint64(testJennerTime),
		Parlia:      &ParliaConfig{},
	}
}

func TestIsJenner(t *testing.T) {
	c := newJennerTestConfig()
	num := big.NewInt(1)

	if c.IsJenner(num, testJennerTime-1) {
		t.Errorf("IsJenner must be false one second before the fork time")
	}
	if !c.IsJenner(num, testJennerTime) {
		t.Errorf("IsJenner must be true exactly at the fork time")
	}
	if !c.IsJenner(num, testJennerTime+1) {
		t.Errorf("IsJenner must be true one second after the fork time")
	}

	// nil JennerTime never activates, no matter how late the timestamp.
	cNil := &ChainConfig{LondonBlock: big.NewInt(0), Parlia: &ParliaConfig{}}
	if cNil.IsJenner(num, math.MaxUint64) {
		t.Errorf("IsJenner must be false when JennerTime is nil")
	}
}

// TestIsJenner_BSCOnlyGate verifies that Jenner (BEP-706) can never activate
// on a non-Parlia config, even if something sets JennerTime on it. The gate
// is embedded in IsJenner itself, so Rules() inherits it automatically.
func TestIsJenner_BSCOnlyGate(t *testing.T) {
	num := big.NewInt(1)
	activeTime := testJennerTime + 1

	nonBSC := &ChainConfig{
		LondonBlock: big.NewInt(0),
		JennerTime:  newUint64(testJennerTime),
		// Parlia is nil: not a BSC chain.
	}
	if nonBSC.IsJenner(num, activeTime) {
		t.Fatalf("IsJenner must be false on a non-Parlia config even with JennerTime set and passed")
	}
	if r := nonBSC.Rules(num, false, activeTime); r.IsJenner {
		t.Fatalf("Rules().IsJenner must be false on a non-Parlia config")
	}

	bsc := newJennerTestConfig()
	if !bsc.IsJenner(num, activeTime) {
		t.Fatalf("IsJenner must be true on a Parlia config past the fork time")
	}
	if r := bsc.Rules(num, false, activeTime); !r.IsJenner {
		t.Fatalf("Rules().IsJenner must be true on a Parlia config past the fork time")
	}
}

func TestLatestFork_Jenner(t *testing.T) {
	c := *ChapelChainConfig
	c.JennerTime = newUint64(testJennerTime)

	if got := c.LatestFork(testJennerTime - 1); got == forks.Jenner {
		t.Fatalf("LatestFork must not report Jenner before the fork time, got %v", got)
	}
	if got := c.LatestFork(testJennerTime); got != forks.Jenner {
		t.Fatalf("LatestFork at the fork time: got %v, want Jenner", got)
	}
	if got := c.LatestFork(testJennerTime + 1); got != forks.Jenner {
		t.Fatalf("LatestFork after the fork time: got %v, want Jenner", got)
	}
	// Right before Jenner, the latest fork must be Pasteur (the latest
	// scheduled fork on Chapel).
	if got := c.LatestFork(testJennerTime - 1); got != forks.Pasteur {
		t.Fatalf("LatestFork right before Jenner: got %v, want Pasteur", got)
	}
	// forks.Fork arithmetic used by logForkReadiness: the enum value right
	// after the pre-Jenner latest fork chain must eventually reach Jenner,
	// and Timestamp must resolve for it (see TestTimestampFunc_Jenner).
	if forks.Jenner <= forks.Amsterdam {
		t.Fatalf("Jenner must be the newest fork enum value")
	}
}

// TestVerifyForkOrdering_JennerNotEarlier pins down the "equal or later than
// every other fork, never earlier" invariant enforced via CheckConfigForkOrder.
func TestVerifyForkOrdering_JennerNotEarlier(t *testing.T) {
	// Chapel has PasteurTime scheduled; use it as the reference fork.
	pasteur := *ChapelChainConfig.PasteurTime

	// Earlier than another defined fork: must be rejected.
	c := *ChapelChainConfig
	c.JennerTime = newUint64(pasteur - 1)
	if err := c.CheckConfigForkOrder(); err == nil {
		t.Fatalf("CheckConfigForkOrder must reject JennerTime earlier than PasteurTime")
	}

	// Equal is explicitly allowed.
	c = *ChapelChainConfig
	c.JennerTime = newUint64(pasteur)
	if err := c.CheckConfigForkOrder(); err != nil {
		t.Fatalf("CheckConfigForkOrder must accept JennerTime equal to PasteurTime: %v", err)
	}

	// Later is allowed.
	c = *ChapelChainConfig
	c.JennerTime = newUint64(testJennerTime)
	if err := c.CheckConfigForkOrder(); err != nil {
		t.Fatalf("CheckConfigForkOrder must accept JennerTime later than PasteurTime: %v", err)
	}

	// Unset (nil) is allowed: the fork is optional until scheduled.
	c = *ChapelChainConfig
	c.JennerTime = nil
	if err := c.CheckConfigForkOrder(); err != nil {
		t.Fatalf("CheckConfigForkOrder must accept a nil JennerTime: %v", err)
	}
}

func TestCheckCompatible_Jenner(t *testing.T) {
	stored := newJennerTestConfig()
	// Moving the Jenner time after a block past the fork point has been
	// imported must be rejected.
	newcfg := newJennerTestConfig()
	newcfg.JennerTime = newUint64(testJennerTime + 100)
	err := stored.CheckCompatible(newcfg, 10, testJennerTime+1)
	if err == nil {
		t.Fatalf("CheckCompatible must reject rescheduling Jenner after activation")
	}
	if err.What != "Jenner fork timestamp" {
		t.Fatalf("unexpected compat error: %v", err)
	}
	// Before activation the same change is fine.
	if err := stored.CheckCompatible(newcfg, 10, testJennerTime-1); err != nil {
		t.Fatalf("CheckCompatible must accept rescheduling Jenner before activation: %v", err)
	}
}

func TestTimestampFunc_Jenner(t *testing.T) {
	c := newJennerTestConfig()
	got := c.Timestamp(forks.Jenner)
	if got == nil || *got != testJennerTime {
		t.Fatalf("Timestamp(forks.Jenner) = %v, want %d", got, testJennerTime)
	}
	cNil := &ChainConfig{Parlia: &ParliaConfig{}}
	if cNil.Timestamp(forks.Jenner) != nil {
		t.Fatalf("Timestamp(forks.Jenner) must be nil when JennerTime is unset")
	}
}

// TestRulesJennerRegression verifies that introducing JennerTime does not
// change the value of any pre-existing fork predicate: two configs that only
// differ in JennerTime must produce Rules that only differ in IsJenner.
func TestRulesJennerRegression(t *testing.T) {
	num := big.NewInt(100_000_000)
	timestamp := testJennerTime + 1

	for _, base := range []*ChainConfig{BSCChainConfig, ChapelChainConfig} {
		withoutJenner := *base
		withoutJenner.JennerTime = nil
		withJenner := *base
		withJenner.JennerTime = newUint64(testJennerTime)

		rA := withoutJenner.Rules(num, false, timestamp)
		rB := withJenner.Rules(num, false, timestamp)
		if rA.IsJenner {
			t.Fatalf("chain %v: IsJenner must be false without JennerTime", base.ChainID)
		}
		if !rB.IsJenner {
			t.Fatalf("chain %v: IsJenner must be true past JennerTime", base.ChainID)
		}
		// Every other rule flag must be identical.
		rB.IsJenner = rA.IsJenner
		if rA != rB {
			t.Fatalf("chain %v: setting JennerTime changed unrelated rules:\nwithout: %+v\nwith:    %+v", base.ChainID, rA, rB)
		}
	}
}
