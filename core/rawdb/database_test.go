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
			// chain dir is existent, so it should be returned.
			name: "1",
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
			// chain dir is nonexistent and state dir is existent; so chain
			// dir should be returned.
			name: "2",
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
			// both chain dir and state dir are nonexistent, if ancient dir is
			// existent, so ancient dir should be returned.
			name: "3",
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
			// ancient dir is nonexistent, so chain dir should be returned.
			name: "4",
			fn: func(dir string) string {
				return fmt.Sprintf("%s%s", dir, mockAncientFreezerPath)
			},
			wantedResult: mockChainFreezerPath,
		},
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
