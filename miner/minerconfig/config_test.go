package minerconfig

import (
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/naoina/toml"
)

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

func TestBuilderRegistryConfig(t *testing.T) {
	var cfg Config
	if err := toml.NewDecoder(strings.NewReader(`[Mev.Registry]
Enabled = true
ContractAddress = "0x0000000000000000000000000000000000000001"
PollInterval = "30s"
`)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	ApplyDefaultMinerConfig(&cfg)
	if err := cfg.Mev.Registry.Validate(); err != nil {
		t.Fatal(err)
	}
	if time.Duration(cfg.Mev.Registry.PollInterval) != 30*time.Second {
		t.Fatalf("explicit interval overwritten: %v", cfg.Mev.Registry.PollInterval)
	}
	encoded, err := toml.Marshal(cfg.Mev.Registry)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip BuilderRegistryConfig
	if err := toml.Unmarshal(encoded, &roundtrip); err != nil || roundtrip != cfg.Mev.Registry {
		t.Fatalf("registry config round trip failed: %v", err)
	}
	var defaults Config
	ApplyDefaultMinerConfig(&defaults)
	if defaults.Mev.Registry.Enabled || time.Duration(defaults.Mev.Registry.PollInterval) != 10*time.Minute {
		t.Fatalf("unexpected registry defaults: %+v", defaults.Mev.Registry)
	}
	for _, cfg := range []BuilderRegistryConfig{
		{Enabled: true},
		{Enabled: true, ContractAddress: common.HexToAddress("0x1"), PollInterval: RegistryDuration(-time.Second)},
		{Enabled: true, ContractAddress: common.HexToAddress("0x1"), PollInterval: RegistryDuration(time.Nanosecond)},
		{Enabled: true, ContractAddress: common.HexToAddress("0x1"), PollInterval: RegistryDuration(time.Second - time.Nanosecond)},
	} {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("invalid config accepted: %+v", cfg)
		}
	}
	for _, interval := range []time.Duration{0, time.Second, 10 * time.Minute} {
		cfg := BuilderRegistryConfig{Enabled: true, ContractAddress: common.HexToAddress("0x1"), PollInterval: RegistryDuration(interval)}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("valid interval %s rejected: %v", interval, err)
		}
	}
}
