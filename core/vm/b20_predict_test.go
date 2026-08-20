package vm

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// TestB20GetAddressDecodesVariantStrictly pins that the predictor and the creator
// read their shared first argument the same way. getB20Address answers, before
// creation, what address createB20 will use (BEP-702 3.3), which only means
// something if both accept the same encodings.
func TestB20GetAddressDecodesVariantStrictly(t *testing.T) {
	_, evm := newB20EVM(t)
	caller := common.HexToAddress("0xc4ea70")
	salt := common.HexToHash("0x01")

	predict := func(variantWord common.Hash) ([]byte, error) {
		input := b20Call(selGetB20Address, variantWord, addrKey(caller), salt)
		ret, _, err := evm.Call(caller, B20FactoryAddress, input, NewGasBudget(1_000_000), uint256.NewInt(0))
		return ret, err
	}

	// The two known variants answer, and differ only in the variant byte.
	var asset, stable common.Hash
	asset[31] = b20VariantAsset
	stable[31] = b20VariantStablecoin
	retA, err := predict(asset)
	if err != nil {
		t.Fatalf("getB20Address(asset): %v", err)
	}
	retS, err := predict(stable)
	if err != nil {
		t.Fatalf("getB20Address(stablecoin): %v", err)
	}
	a, s := common.BytesToAddress(retA), common.BytesToAddress(retS)
	if a[10] != b20VariantAsset || s[10] != b20VariantStablecoin {
		t.Errorf("variant bytes = %#x / %#x, want %#x / %#x", a[10], s[10], b20VariantAsset, b20VariantStablecoin)
	}
	if !bytes.Equal(a[11:20], s[11:20]) {
		t.Error("the fingerprint must not depend on the variant")
	}

	// A clean but unrecognized variant must be refused, not answered.
	var future common.Hash
	future[31] = 0x02
	if ret, err := predict(future); err == nil {
		t.Errorf("getB20Address(variant 2) returned %s, want a revert", common.BytesToAddress(ret).Hex())
	}

	// And a word carrying anything above uint8 is a malformed encoding, exactly
	// as createB20 already treats it — not a value to truncate.
	dirty := asset
	dirty[0] = 1
	if ret, err := predict(dirty); err == nil {
		t.Errorf("getB20Address(dirty word) returned %s, want a revert", common.BytesToAddress(ret).Hex())
	} else if !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("dirty word gave %v, want a revert", err)
	}

	// The predictor and the creator must refuse the same inputs, which is the
	// property that makes prediction meaningful.
	//
	// The variant word is spliced in raw rather than passed through
	// encodeCreateB20's byte argument, which would re-encode it clean and lose
	// exactly the dirty bits under test.
	for _, w := range []common.Hash{future, dirty} {
		_, predictErr := predict(w)

		create := encodeCreateB20(b20VariantAsset, salt, caller, nil)
		copy(create[4:36], w.Bytes())
		_, _, createErr := evm.Call(caller, B20FactoryAddress, create,
			NewGasBudget(5_000_000), uint256.NewInt(0))

		if (predictErr == nil) != (createErr == nil) {
			t.Errorf("predict and create disagree on %s: predict=%v create=%v", w.Hex(), predictErr, createErr)
		}
	}
}
