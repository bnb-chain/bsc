package miner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/miner/minerconfig"
)

func TestBuilderAdmissionPolicy(t *testing.T) {
	oldKey, newKey := common.HexToAddress("0x1"), common.HexToAddress("0x2")
	cfg := &minerconfig.MevConfig{}
	b := &bidSimulator{config: cfg, builders: map[common.Address]builderEndpoint{oldKey: {}}}
	m := &Miner{bidSimulator: b}

	require.True(t, m.HasBuilder(oldKey))
	require.False(t, m.HasBuilder(newKey))
	cfg.SentryURL = "http://sentry"
	require.True(t, m.HasBuilder(newKey))

	cfg.SentryURL = ""
	cfg.BuilderRegistry.Enabled = true
	m.ReplaceRegistryBuilders(map[common.Address]string{newKey: "invalid-url"})
	require.False(t, m.HasBuilder(oldKey))
	require.True(t, m.HasBuilder(newKey))
	_, _, err := b.issueClient(context.Background(), newKey)
	require.Error(t, err)
	require.Error(t, m.AddBuilder(oldKey, ""))
	require.Error(t, m.RemoveBuilder(newKey))

	m.ReplaceRegistryBuilders(nil)
	require.False(t, m.HasBuilder(newKey))
	cfg.SentryURL = "http://sentry"
	require.True(t, m.HasBuilder(newKey))
}
