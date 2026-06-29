// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package eth

import (
	"testing"

	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/params"
)

func TestApplyChainConfigOverridesAddsOsakaBlobSchedule(t *testing.T) {
	osakaTime := uint64(1782779200)
	mendelTime := uint64(1782779300)
	pasteurTime := uint64(1782779400)
	chainConfig := new(params.ChainConfig)
	*chainConfig = *params.RialtoChainConfig
	blobSchedule := new(params.BlobScheduleConfig)
	*blobSchedule = *params.RialtoChainConfig.BlobScheduleConfig
	blobSchedule.Osaka = nil
	chainConfig.BlobScheduleConfig = blobSchedule

	applyChainConfigOverrides(chainConfig, &ethconfig.Config{
		OverrideOsaka:   &osakaTime,
		OverrideMendel:  &mendelTime,
		OverridePasteur: &pasteurTime,
	})

	if err := chainConfig.CheckConfigForkOrder(); err != nil {
		t.Fatalf("unexpected fork order error: %v", err)
	}
	if chainConfig.OsakaTime == nil || *chainConfig.OsakaTime != osakaTime {
		t.Fatalf("unexpected OsakaTime: have %v, want %d", chainConfig.OsakaTime, osakaTime)
	}
	if chainConfig.BlobScheduleConfig == nil {
		t.Fatal("expected blob schedule config to be initialized")
	}
	if chainConfig.BlobScheduleConfig.Osaka != params.DefaultOsakaBlobConfigBSC {
		t.Fatalf("unexpected Osaka blob schedule: have %v, want %v", chainConfig.BlobScheduleConfig.Osaka, params.DefaultOsakaBlobConfigBSC)
	}
}
