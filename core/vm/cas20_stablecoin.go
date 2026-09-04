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
