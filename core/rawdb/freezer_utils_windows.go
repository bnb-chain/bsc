<<<<<<<< HEAD:eth/protocols/bsc/metrics.go
// Copyright 2023 The go-ethereum Authors
========
// Copyright 2022 The go-ethereum Authors
>>>>>>>> geth-v1.17.3:core/rawdb/freezer_utils_windows.go
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

<<<<<<<< HEAD:eth/protocols/bsc/metrics.go
package bsc

import (
	metrics "github.com/ethereum/go-ethereum/metrics"
)

var (
	ingressRegistrationErrorName = "eth/protocols/bsc/ingress/registration/error"
	egressRegistrationErrorName  = "eth/protocols/bsc/egress/registration/error"

	IngressRegistrationErrorMeter = metrics.NewRegisteredMeter(ingressRegistrationErrorName, nil)
	EgressRegistrationErrorMeter  = metrics.NewRegisteredMeter(egressRegistrationErrorName, nil)
)
========
//go:build windows
// +build windows

package rawdb

// syncDir is a no-op on Windows. Fsyncing a directory handle is not
// supported and returns "Access is denied".
func syncDir(name string) error {
	return nil
}
>>>>>>>> geth-v1.17.3:core/rawdb/freezer_utils_windows.go
