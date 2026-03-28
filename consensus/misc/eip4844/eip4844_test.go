// Copyright 2023 The go-ethereum Authors
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

package eip4844

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

func TestCalcExcessBlobGas(t *testing.T) {
	var (
		config        = params.MainnetChainConfig
		targetBlobs   = config.BlobScheduleConfig.Cancun.Target
		targetBlobGas = uint64(targetBlobs) * params.BlobTxBlobGasPerBlob
	)

	var tests = []struct {
		excess uint64
		blobs  int
		want   uint64
	}{
		// The excess blob gas should not increase from zero if the used blob
		// slots are below - or equal - to the target.
		{0, 0, 0},
		{0, 1, 0},
		{0, targetBlobs, 0},

		// If the target blob gas is exceeded, the excessBlobGas should increase
		// by however much it was overshot
		{0, targetBlobs + 1, params.BlobTxBlobGasPerBlob},
		{1, targetBlobs + 1, params.BlobTxBlobGasPerBlob + 1},
		{1, targetBlobs + 2, 2*params.BlobTxBlobGasPerBlob + 1},

		// The excess blob gas should decrease by however much the target was
		// under-shot, capped at zero.
		{targetBlobGas, targetBlobs, targetBlobGas},
		{targetBlobGas, targetBlobs - 1, targetBlobGas - params.BlobTxBlobGasPerBlob},
		{targetBlobGas, targetBlobs - 2, targetBlobGas - (2 * params.BlobTxBlobGasPerBlob)},
		{params.BlobTxBlobGasPerBlob - 1, targetBlobs - 1, 0},
	}
	for i, tt := range tests {
		blobGasUsed := uint64(tt.blobs) * params.BlobTxBlobGasPerBlob
		header := &types.Header{
			ExcessBlobGas: &tt.excess,
			BlobGasUsed:   &blobGasUsed,
		}
		result := CalcExcessBlobGas(config, header, *config.CancunTime)
		if result != tt.want {
			t.Errorf("test %d: excess blob gas mismatch: have %v, want %v", i, result, tt.want)
		}
	}
}

func TestCalcBlobFee(t *testing.T) {
	zero := uint64(0)

	tests := []struct {
		excessBlobGas uint64
		blobfee       int64
	}{
		{0, 1},
		{2314057, 1},
		{2314058, 2},
		{10 * 1024 * 1024, 23},
	}
	for i, tt := range tests {
		config := &params.ChainConfig{LondonBlock: big.NewInt(0), CancunTime: &zero, BlobScheduleConfig: params.DefaultBlobSchedule}
		header := &types.Header{ExcessBlobGas: &tt.excessBlobGas}
		have := CalcBlobFee(config, header)
		if have.Int64() != tt.blobfee {
			t.Errorf("test %d: blobfee mismatch: have %v want %v", i, have, tt.blobfee)
		}
	}
}

func TestCalcBlobFeePostOsaka(t *testing.T) {
	zero := uint64(0)
	bpo1 := uint64(1754836608)
	bpo2 := uint64(1754934912)
	bpo3 := uint64(1755033216)

	tests := []struct {
		excessBlobGas uint64
		blobGasUsed   uint64
		blobfee       uint64
		basefee       uint64
		parenttime    uint64
		headertime    uint64
	}{
		{5149252, 1310720, 5617366, 30, 1754904516, 1754904528},
		{19251039, 2490368, 20107103, 50, 1755033204, 1755033216},
	}
	for i, tt := range tests {
		config := &params.ChainConfig{
			LondonBlock: big.NewInt(0),
			CancunTime:  &zero,
			PragueTime:  &zero,
			OsakaTime:   &zero,
			BPO1Time:    &bpo1,
			BPO2Time:    &bpo2,
			BPO3Time:    &bpo3,
			BlobScheduleConfig: &params.BlobScheduleConfig{
				Cancun: params.DefaultCancunBlobConfig,
				Prague: params.DefaultPragueBlobConfig,
				Osaka:  params.DefaultOsakaBlobConfig,
				BPO1: &params.BlobConfig{
					Target:         9,
					Max:            14,
					UpdateFraction: 8832827,
				},
				BPO2: &params.BlobConfig{
					Target:         14,
					Max:            21,
					UpdateFraction: 13739630,
				},
				BPO3: &params.BlobConfig{
					Target:         21,
					Max:            32,
					UpdateFraction: 20609697,
				},
			}}
		parent := &types.Header{
			ExcessBlobGas: &tt.excessBlobGas,
			BlobGasUsed:   &tt.blobGasUsed,
			BaseFee:       big.NewInt(int64(tt.basefee)),
			Time:          tt.parenttime,
		}
		have := CalcExcessBlobGas(config, parent, tt.headertime)
		if have != tt.blobfee {
			t.Errorf("test %d: blobfee mismatch: have %v want %v", i, have, tt.blobfee)
		}
	}
}

func TestFakeExponential(t *testing.T) {
	tests := []struct {
		factor      int64
		numerator   int64
		denominator int64
		want        int64
	}{
		// When numerator == 0 the return value should always equal the value of factor
		{1, 0, 1, 1},
		{38493, 0, 1000, 38493},
		{0, 1234, 2345, 0}, // should be 0
		{1, 2, 1, 6},       // approximate 7.389
		{1, 4, 2, 6},
		{1, 3, 1, 16}, // approximate 20.09
		{1, 6, 2, 18},
		{1, 4, 1, 49}, // approximate 54.60
		{1, 8, 2, 50},
		{10, 8, 2, 542}, // approximate 540.598
		{11, 8, 2, 596}, // approximate 600.58
		{1, 5, 1, 136},  // approximate 148.4
		{1, 5, 2, 11},   // approximate 12.18
		{2, 5, 2, 23},   // approximate 24.36
		{1, 50000000, 2225652, 5709098764},
	}
	for i, tt := range tests {
		f, n, d := big.NewInt(tt.factor), big.NewInt(tt.numerator), big.NewInt(tt.denominator)
		original := fmt.Sprintf("%d %d %d", f, n, d)
		have := fakeExponential(f, n, d)
		if have.Int64() != tt.want {
			t.Errorf("test %d: fake exponential mismatch: have %v want %v", i, have, tt.want)
		}
		later := fmt.Sprintf("%d %d %d", f, n, d)
		if original != later {
			t.Errorf("test %d: fake exponential modified arguments: have\n%v\nwant\n%v", i, later, original)
		}
	}
}

func TestCalcExcessBlobGasEIP7918(t *testing.T) {
	var (
		cfg           = params.MergedTestChainConfig
		targetBlobs   = cfg.BlobScheduleConfig.Osaka.Target
		blobGasTarget = uint64(targetBlobs) * params.BlobTxBlobGasPerBlob
	)

	makeHeader := func(parentExcess, parentBaseFee uint64, blobsUsed int) *types.Header {
		blobGasUsed := uint64(blobsUsed) * params.BlobTxBlobGasPerBlob
		return &types.Header{
			BaseFee:       big.NewInt(int64(parentBaseFee)),
			ExcessBlobGas: &parentExcess,
			BlobGasUsed:   &blobGasUsed,
		}
	}

	tests := []struct {
		name          string
		header        *types.Header
		wantExcessGas uint64
	}{
		{
			name:          "BelowReservePrice",
			header:        makeHeader(0, 1_000_000_000, targetBlobs),
			wantExcessGas: blobGasTarget * 3 / 9,
		},
		{
			name:          "AboveReservePrice",
			header:        makeHeader(0, 1, targetBlobs),
			wantExcessGas: 0,
		},
	}
	for _, tc := range tests {
		got := CalcExcessBlobGas(cfg, tc.header, *cfg.CancunTime)
		if got != tc.wantExcessGas {
			t.Fatalf("%s: excess-blob-gas mismatch – have %d, want %d",
				tc.name, got, tc.wantExcessGas)
		}
	}
}

// TestBEP657 tests BEP-657: Limit Blob Transaction Inclusion by Block Number.
// After Mendel fork, only blocks where N % BlobEligibleBlockInterval == 0 can include blob transactions.
func TestBEP657(t *testing.T) {
	mendelTime := uint64(1000)
	cfg := *params.RialtoChainConfig
	cfg.MendelTime = &mendelTime
	config := &cfg
	targetBlobGas := uint64(config.BlobScheduleConfig.Cancun.Target) * params.BlobTxBlobGasPerBlob

	// Test IsBlobEligibleBlock
	for _, tt := range []struct {
		blockNum uint64
		time     uint64
		want     bool
	}{
		{1, 999, true},   // before fork: all eligible
		{5, 1000, true},  // after fork: N%5==0 eligible
		{1, 1000, false}, // after fork: N%5!=0 not eligible
	} {
		if got := IsBlobEligibleBlock(config, tt.blockNum, tt.time); got != tt.want {
			t.Errorf("IsBlobEligibleBlock(%d, %d) = %v, want %v", tt.blockNum, tt.time, got, tt.want)
		}
	}

	// Test CalcExcessBlobGas: inherit vs recalculate
	for i, tt := range []struct {
		parentNum   uint64
		excess      uint64
		used        uint64
		time        uint64
		wantInherit bool
	}{
		{5, 600000, params.BlobTxBlobGasPerBlob * 4, 1000, false}, // parent N%5==0: recalc
		{3, 700000, 0, 1000, true},                                // parent N%5!=0: inherit
		{3, 500000, params.BlobTxBlobGasPerBlob * 2, 999, false},  // before fork: recalc
	} {
		parent := &types.Header{Number: big.NewInt(int64(tt.parentNum)), Time: 1000, ExcessBlobGas: &tt.excess, BlobGasUsed: &tt.used}
		got := CalcExcessBlobGas(config, parent, tt.time)
		if tt.wantInherit && got != tt.excess {
			t.Errorf("CalcExcessBlobGas test %d: want inherit %d, got %d", i, tt.excess, got)
		}
		if !tt.wantInherit {
			want := tt.excess + tt.used
			if want >= targetBlobGas {
				want -= targetBlobGas
			} else {
				want = 0
			}
			if got != want {
				t.Errorf("CalcExcessBlobGas test %d: want recalc %d, got %d", i, want, got)
			}
		}
	}

	// Test VerifyEIP4844Header: BlobGasUsed validation
	makeHeader := func(num, blobGas, time uint64) *types.Header {
		zero := uint64(0)
		return &types.Header{Number: big.NewInt(int64(num)), Time: time, ExcessBlobGas: &zero, BlobGasUsed: &blobGas}
	}
	for i, tt := range []struct {
		parentNum, headerNum, blobGas uint64
		time                          uint64
		wantErr                       bool
	}{
		{4, 5, params.BlobTxBlobGasPerBlob, 1000, false}, // eligible: can have blobs
		{5, 6, params.BlobTxBlobGasPerBlob, 1000, true},  // non-eligible: must fail
		{5, 6, 0, 1000, false},                           // non-eligible: no blobs OK
		{5, 6, params.BlobTxBlobGasPerBlob, 999, false},  // before fork: all OK
	} {
		err := VerifyEIP4844Header(config, makeHeader(tt.parentNum, 0, 1000), makeHeader(tt.headerNum, tt.blobGas, tt.time))
		if tt.wantErr && err == nil {
			t.Errorf("VerifyEIP4844Header test %d: expected error", i)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("VerifyEIP4844Header test %d: unexpected error: %v", i, err)
		}
	}
}
