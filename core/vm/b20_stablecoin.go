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

// B20 Stablecoin variant: adds an immutable currency() and fixes decimals at 6
// (BEP-702 3.13).

const b20StablecoinNamespace = "bsc.b20.stablecoin"

const b20StablecoinSlotCurrency = 0 // string

var (
	b20StablecoinRoot = erc7201Root(b20StablecoinNamespace)

	selCurrency = selector("currency()")
)

// stablecoinExt is a gas-metered view over the Stablecoin extension storage.
type stablecoinExt struct{ s b20Storage }

func newStablecoinExt(ctx *PrecompileContext) stablecoinExt {
	return stablecoinExt{s: newMeteredB20Storage(ctx)}
}

func stablecoinSlot(offset uint64) common.Hash {
	return offsetSlot(b20StablecoinRoot, offset)
}

func (e stablecoinExt) currency() string {
	return e.s.getStringAt(stablecoinSlot(b20StablecoinSlotCurrency))
}

func (e stablecoinExt) setCurrency(v string) bool {
	return e.s.setStringAt(stablecoinSlot(b20StablecoinSlotCurrency), v)
}

// stablecoinDispatch routes a Stablecoin call: the one extension selector
// first, then the shared IB20 surface.
func stablecoinDispatch(tok b20Token, ext stablecoinExt, input []byte) ([]byte, error) {
	if len(input) >= 4 {
		var sel [4]byte
		copy(sel[:], input[:4])
		if sel == selCurrency {
			return encString(ext.currency()), nil
		}
	}
	return tok.dispatch(input)
}
