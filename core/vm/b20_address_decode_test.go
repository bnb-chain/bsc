package vm

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// TestB20AddressArrayDecodesStrictly pins that an address[] element is read as
// strictly as a scalar address argument.
//
// batchMint truncated its recipient words to the low twenty bytes. A word with
// dirty high bytes minted to the truncated address where mint with the same word
// reverts, so the two paths disagreed about what the calldata said. A client that
// decoded strictly would revert where this one mints — the state roots differ, and
// nothing in the surface's own tests would show it.
func TestB20AddressArrayDecodesStrictly(t *testing.T) {
	_, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	salt := common.HexToHash("0xd1r7")

	ret, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, salt, creator,
			[][]byte{b20Call(selGrantRole, roleMint, addrKey(creator))}),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(ret)

	// A well-formed envelope, with the recipient word dirtied in place: the
	// selector, two offsets and the length put it at [100:132]. Building the
	// calldata by hand instead risks reverting for a malformed layout, which would
	// look like the guard working.
	call := encodeBatchMint([]common.Address{b20Bob}, []uint64{1000})
	if len(call) < 132 {
		t.Fatalf("unexpected batchMint encoding, %d bytes", len(call))
	}
	clean := append([]byte{}, call...)
	call[100], call[105] = 0xde, 0xad

	if _, _, err := evm.Call(creator, token, call, NewGasBudget(5_000_000), uint256.NewInt(0)); !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("batchMint accepted a dirty recipient word: %v", err)
	}
	if bal := newB20Storage(evm.StateDB, token).balanceOf(b20Bob); bal.Sign() != 0 {
		t.Errorf("balanceOf(truncated address) = %s, want 0", bal)
	}

	// The control: the same envelope, clean, must succeed — otherwise the
	// assertion above holds for the wrong reason.
	if _, _, err := evm.Call(creator, token, clean, NewGasBudget(5_000_000), uint256.NewInt(0)); err != nil {
		t.Fatalf("batchMint with a clean recipient word: %v", err)
	}
	if bal := newB20Storage(evm.StateDB, token).balanceOf(b20Bob); bal.Uint64() != 1000 {
		t.Fatalf("balanceOf after the clean batch = %s, want 1000", bal)
	}

	// And the scalar path, which already behaved, so the two now agree.
	dirty := addrKey(b20Carol)
	dirty[0] = 0xde
	if _, _, err := evm.Call(creator, token, b20Call(selMint, dirty, u256hash(5)),
		NewGasBudget(5_000_000), uint256.NewInt(0)); !errors.Is(err, ErrExecutionReverted) {
		t.Errorf("mint accepted a dirty recipient word: %v", err)
	}
}
