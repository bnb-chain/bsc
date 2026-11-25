package miner

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/miner/minerconfig"
)

func TestBuilderAdmissionPolicy(t *testing.T) {
	listed, unknown := common.HexToAddress("0x1"), common.HexToAddress("0x2")
	for _, sentry := range []string{"", "http://sentry"} {
		for _, enabled := range []bool{false, true} {
			cfg := &minerconfig.MevConfig{SentryURL: sentry, BuilderRegistry: minerconfig.BuilderRegistryConfig{Enabled: enabled}}
			b := &bidSimulator{config: cfg, builders: map[common.Address]builderEndpoint{listed: {}}}
			m := &Miner{bidSimulator: b}
			if !m.HasBuilder(listed) || m.HasBuilder(unknown) != (sentry != "") {
				t.Fatalf("wrong startup policy: registry=%v sentry=%q", enabled, sentry)
			}
			if enabled {
				m.ReplaceRegistryBuilders(map[common.Address]string{unknown: "file:///invalid-report-url"})
				if m.HasBuilder(listed) != (sentry != "") || !m.HasBuilder(unknown) {
					t.Fatalf("wrong policy after registry sync: sentry=%q", sentry)
				}
				if sentry == "" {
					if _, _, err := b.issueClient(context.Background(), unknown); err == nil {
						t.Fatal("invalid registry report URL accepted")
					}
				}
				m.ReplaceRegistryBuilders(nil)
				if m.HasBuilder(listed) != (sentry != "") || m.HasBuilder(unknown) != (sentry != "") {
					t.Fatalf("wrong policy after empty registry sync: sentry=%q", sentry)
				}
				if m.AddBuilder(listed, "") == nil || m.RemoveBuilder(unknown) == nil {
					t.Fatal("admin mutation must not silently override registry ownership")
				}
			} else {
				if sentry == "" {
					if err := m.AddBuilder(unknown, ""); err != nil {
						t.Fatal(err)
					}
					if !m.HasBuilder(unknown) {
						t.Fatal("manually added builder must be allowed")
					}
					if err := m.RemoveBuilder(listed); err != nil {
						t.Fatal(err)
					}
					if m.HasBuilder(listed) {
						t.Fatal("manually removed builder must be rejected")
					}
				}
				b.builders = nil
				if m.HasBuilder(listed) != (sentry != "") || m.HasBuilder(unknown) != (sentry != "") {
					t.Fatalf("empty static list must only allow builders when sentry is configured: sentry=%q", sentry)
				}
			}
		}
	}
}
