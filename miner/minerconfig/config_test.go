package minerconfig

import "testing"

// A config.toml that omits the BEP-675 gRPC knobs must still come out with the
// defaults, otherwise eth.New rejects the node with "invalid MEV gRPC port 0".
func TestApplyDefaultMinerConfigBackfillsGRPC(t *testing.T) {
	cfg := Config{}
	ApplyDefaultMinerConfig(&cfg)
	if cfg.Mev.GRPCPort != defaultGRPCPort {
		t.Errorf("GRPCPort = %d, want %d", cfg.Mev.GRPCPort, defaultGRPCPort)
	}
	if cfg.Mev.GRPCConcurrency != defaultGRPCConcurrency {
		t.Errorf("GRPCConcurrency = %d, want %d", cfg.Mev.GRPCConcurrency, defaultGRPCConcurrency)
	}
	if cfg.Mev.GRPCRequestTimeout != defaultGRPCRequestTimeout {
		t.Errorf("GRPCRequestTimeout = %v, want %v", cfg.Mev.GRPCRequestTimeout, defaultGRPCRequestTimeout)
	}

	// An explicit value in the config file wins.
	cfg = Config{Mev: MevConfig{GRPCPort: 9999}}
	ApplyDefaultMinerConfig(&cfg)
	if cfg.Mev.GRPCPort != 9999 {
		t.Errorf("GRPCPort = %d, want 9999", cfg.Mev.GRPCPort)
	}
}
