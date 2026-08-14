package core

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/params"
)

// TestB20ActivationAdminNeedsTheOverride pins why --override.b20activationadmin
// exists, which is not obvious and was nearly deleted as redundant.
//
// A QA cluster registers its own genesis hash as the Rialto hash
// (--rialtohash), so GetBuiltInChainConfig matches it and LoadChainConfig
// returns the built-in RialtoChainConfig, discarding the genesis file's own
// config. The b20ActivationAdmin written into that file therefore has no effect,
// and the built-in value is the governance timelock — a contract that cannot
// sign, so nothing on such a network could ever activate a feature.
func TestB20ActivationAdminNeedsTheOverride(t *testing.T) {
	qaAdmin := common.HexToAddress("0x04d63aBCd2b9b1baa327f2Dda0f873F197ccd186")

	genesis := &Genesis{Config: &params.ChainConfig{
		ChainID:            params.RialtoChainConfig.ChainID,
		B20ActivationAdmin: &qaAdmin,
	}}

	db := rawdb.NewMemoryDatabase()
	stored := genesis.ToBlock().Hash()
	rawdb.WriteCanonicalHash(db, stored, 0)
	rawdb.WriteChainConfig(db, stored, genesis.Config)

	// Without the hash aliasing, the genesis file's own config is used.
	cfg, _, err := LoadChainConfig(db, genesis)
	if err != nil {
		t.Fatalf("LoadChainConfig: %v", err)
	}
	if cfg.B20ActivationAdmin == nil || *cfg.B20ActivationAdmin != qaAdmin {
		t.Fatalf("without aliasing, admin = %v, want the genesis value", cfg.B20ActivationAdmin)
	}

	// With it — which is what --rialtohash does — the built-in config wins and
	// the genesis value is gone.
	defer func(orig common.Hash) { params.RialtoGenesisHash = orig }(params.RialtoGenesisHash)
	params.RialtoGenesisHash = stored

	cfg, _, err = LoadChainConfig(db, genesis)
	if err != nil {
		t.Fatalf("LoadChainConfig with the aliased hash: %v", err)
	}
	if cfg.B20ActivationAdmin != nil && *cfg.B20ActivationAdmin == qaAdmin {
		t.Fatal("the genesis admin survived; --override.b20activationadmin would be redundant")
	}
	if cfg.B20ActivationAdmin == nil || *cfg.B20ActivationAdmin != params.BSCTimelockAddress {
		t.Fatalf("built-in admin = %v, want the timelock", cfg.B20ActivationAdmin)
	}

	// And the override is what puts the QA value back.
	overrides := ChainOverrides{OverrideB20ActivationAdmin: &qaAdmin}
	applied := *cfg
	if err := overrides.apply(&applied); err != nil {
		t.Fatalf("apply overrides: %v", err)
	}
	if applied.B20ActivationAdmin == nil || *applied.B20ActivationAdmin != qaAdmin {
		t.Fatalf("after the override, admin = %v, want the QA account", applied.B20ActivationAdmin)
	}
}
