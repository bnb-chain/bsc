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

package vm

import "github.com/ethereum/go-ethereum/common"

// CAS20 Stablecoin variant: adds an immutable currency() and fixes decimals at 6
// (BEP-702 3.13).

const cas20StablecoinNamespace = "bsc.cas20.stablecoin"

const cas20StablecoinSlotCurrency = 0 // string

var (
	cas20StablecoinRoot = erc7201Root(cas20StablecoinNamespace)

	selCurrency = selector("currency()")
)

// stablecoinExt is a gas-metered view over the Stablecoin extension storage.
type stablecoinExt struct{ s cas20Storage }

func newStablecoinExt(ctx *PrecompileContext) stablecoinExt {
	return stablecoinExt{s: newMeteredCAS20Storage(ctx)}
}

func stablecoinSlot(offset uint64) common.Hash {
	return offsetSlot(cas20StablecoinRoot, offset)
}

func (e stablecoinExt) currency() (string, bool) {
	return e.s.getStringAt(stablecoinSlot(cas20StablecoinSlotCurrency))
}

func (e stablecoinExt) setCurrency(v string) bool {
	return e.s.setStringAt(stablecoinSlot(cas20StablecoinSlotCurrency), v)
}

// stablecoinDispatch routes a Stablecoin call: the one extension selector
// first, then the shared IB20 surface.
func stablecoinDispatch(tok cas20Token, ext stablecoinExt, input []byte) ([]byte, error) {
	if len(input) >= 4 {
		var sel [4]byte
		copy(sel[:], input[:4])
		if sel == selCurrency {
			v, ok := ext.currency()
			if !ok {
				return nil, ErrOutOfGas
			}
			return encString(v), nil
		}
	}
	return tok.dispatch(input)
}
