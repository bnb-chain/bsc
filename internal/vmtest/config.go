// Copyright 2024 The go-ethereum Authors
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

// Package vmtest provides testing utilities for EVM and MIR interpreter testing.
package vmtest

import (
	"os"

	"github.com/ethereum/go-ethereum/core/vm"
)

// =============================================================================
// Dual-mode testing helpers (EVM + MIR)
// =============================================================================
//
// These helpers allow tests to run with both EVM and MIR interpreters.
//
// Usage:
//   - By default, tests only run with EVM interpreter
//   - Set TEST_WITH_MIR=true environment variable to also run with MIR
//
// Example:
//   import "github.com/ethereum/go-ethereum/internal/vmtest"
//
//   func TestSomething(t *testing.T) {
//       for _, vmCfg := range vmtest.Configs() {
//           t.Run(vmtest.Name(vmCfg), func(t *testing.T) {
//               // test code using vmCfg
//           })
//       }
//   }
//
// Running tests:
//   go test ./...                        # EVM only (default)
//   TEST_WITH_MIR=true go test ./...     # EVM + MIR

// Configs returns VM configs to test.
// By default, only returns EVM config. Set TEST_WITH_MIR=true to also include MIR.
func Configs() []vm.Config {
	configs := []vm.Config{
		{}, // Default EVM interpreter
	}
	if os.Getenv("TEST_WITH_MIR") == "true" {
		configs = append(configs, vm.Config{EnableMIR: true})
	}
	return configs
}

// Name returns a human-readable name for a vm.Config.
// Used for test sub-test naming.
func Name(cfg vm.Config) string {
	if cfg.EnableMIR {
		return "MIR"
	}
	if cfg.EnableOpcodeOptimizations {
		return "OptimizedEVM"
	}
	return "EVM"
}

// MIREnabled returns true if MIR testing is enabled via environment variable.
func MIREnabled() bool {
	return os.Getenv("TEST_WITH_MIR") == "true"
}
