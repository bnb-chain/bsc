// Copyright 2017 The go-ethereum Authors
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

package rawdb

import (
	"fmt"
	"os"
	"testing"
)

const (
	mockChainFreezerPath   = "/geth/chaindata/ancient/chain"
	mockStateFreezerPath   = "/geth/chaindata/ancient/state"
	mockAncientFreezerPath = "/geth/chaindata/ancient"
)

func Test_resolveChainFreezerDir(t *testing.T) {
	tests := []struct {
		name         string
		fn           func(dir string) string
		ancient      string
		wantedResult string
	}{
		{
			name: "run geth in pruned mode and chain dir is existent",
			fn: func(dir string) string {
				path := fmt.Sprintf("%s%s", dir, mockChainFreezerPath)
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatalf("Failed to mkdir all dirs, error: %v", err)
				}
				return fmt.Sprintf("%s%s", dir, mockAncientFreezerPath)
			},
			wantedResult: mockChainFreezerPath,
		},
		{
			name: "run geth in path and pruned mode; chain is nonexistent and state is existent",
			fn: func(dir string) string {
				path := fmt.Sprintf("%s%s", dir, mockStateFreezerPath)
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatalf("Failed to mkdir all dirs, error: %v", err)
				}
				return fmt.Sprintf("%s%s", dir, mockAncientFreezerPath)
			},
			wantedResult: mockChainFreezerPath,
		},
		{
			name: "run geth in hash and pruned mode; ancient block data locates in ancient dir",
			fn: func(dir string) string {
				path := fmt.Sprintf("%s%s", dir, mockAncientFreezerPath)
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatalf("Failed to mkdir all dirs, error: %v", err)
				}
				return path
			},
			wantedResult: mockAncientFreezerPath,
		},
		{
			name: "run geth in pruned mode and there is no ancient dir",
			fn: func(dir string) string {
				return fmt.Sprintf("%s%s", dir, mockAncientFreezerPath)
			},
			wantedResult: mockChainFreezerPath,
		},
		{
			name: "run geth in non-pruned mode; ancient is existent and state dir is nonexistent",
			fn: func(dir string) string {
				path := fmt.Sprintf("%s%s", dir, mockAncientFreezerPath)
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatalf("Failed to mkdir all dirs, error: %v", err)
				}
				return fmt.Sprintf("%s%s", dir, mockAncientFreezerPath)
			},
			wantedResult: mockAncientFreezerPath,
		},
		// {
		// 	name: "run geth in non-pruned mode and chain dir is existent",
		// 	fn: func(dir string) string {
		// 		path := fmt.Sprintf("%s%s", dir, mockChainFreezerPath)
		// 		if err := os.MkdirAll(path, 0700); err != nil {
		// 			t.Fatalf("Failed to mkdir all dirs, error: %v", err)
		// 		}
		// 		return fmt.Sprintf("%s%s", dir, mockAncientFreezerPath)
		// 	},
		// 	wantedResult: mockChainFreezerPath,
		// },
		// {
		// 	name: "run geth in non-pruned mode, ancient and chain dir is nonexistent",
		// 	fn: func(dir string) string {
		// 		return fmt.Sprintf("%s%s", dir, mockAncientFreezerPath)
		// 	},
		// 	wantedResult: mockChainFreezerPath,
		// },
		// {
		// 	name: "run geth in non-pruned mode; ancient dir is existent, chain and state dir is nonexistent",
		// 	fn: func(dir string) string {
		// 		path := fmt.Sprintf("%s%s", dir, mockStateFreezerPath)
		// 		if err := os.MkdirAll(path, 0700); err != nil {
		// 			t.Fatalf("Failed to mkdir all dirs, error: %v", err)
		// 		}
		// 		return fmt.Sprintf("%s%s", dir, mockAncientFreezerPath)
		// 	},
		// 	wantedResult: mockChainFreezerPath,
		// },
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			got := resolveChainFreezerDir(tt.fn(tempDir))
			if got != fmt.Sprintf("%s%s", tempDir, tt.wantedResult) {
				t.Fatalf("resolveChainFreezerDir() = %s, wanted = %s", got, tt.wantedResult)
			}
		})
	}
}
