package minerconfig

import (
	"strings"
	"testing"
	"time"

	"github.com/naoina/toml"
	"github.com/stretchr/testify/require"
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
	ApplyDefaultMinerConfig(&cfg)
	registry := &cfg.Mev.BuilderRegistry
	require.False(t, registry.Enabled)
	require.Equal(t, 10*time.Minute, time.Duration(registry.PollInterval))
	registry.Enabled = true
	require.Error(t, registry.Validate())

	require.NoError(t, toml.NewDecoder(strings.NewReader(`[Mev.BuilderRegistry]
Enabled = true
ContractAddress = "0x0000000000000000000000000000000000000001"
PollInterval = "250ms"
`)).Decode(&cfg))
	ApplyDefaultMinerConfig(&cfg)
	require.NoError(t, registry.Validate())
	require.Equal(t, 250*time.Millisecond, time.Duration(registry.PollInterval))

	registry.PollInterval = RegistryDuration(-time.Second)
	ApplyDefaultMinerConfig(&cfg)
	require.Equal(t, 10*time.Minute, time.Duration(registry.PollInterval))
}
