package vm

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// deployStopCode is initcode that deploys a single STOP byte. STOP is the useful
// choice for these tests: a call that reaches it succeeds with empty returndata,
// while a call the token handler intercepts reverts — so the two are told apart
// by the error alone, with no dependence on what a handler returns.
//
//	PUSH1 0x01 PUSH1 0x0f PUSH1 0x00 CODECOPY PUSH1 0x01 PUSH1 0x00 RETURN STOP
var deployStopCode = []byte{
	0x60, 0x01, 0x60, 0x0f, 0x60, 0x00, 0x39,
	0x60, 0x01, 0x60, 0x00, 0xf3,
	0x00,
}

// TestB20ReservedSpaceDeployment pins what ordinary contract creation does
// against the reserved space. BEP-702 3.3 deliberately places no restriction on
// CREATE/CREATE2 output addresses: a squatter that grinds its way in (2^80 for
// the ten-byte prefix) only bricks its own account, because dispatch resolves the
// token handler before any code runs. Nothing tested this, so the "deliberate"
// half was an assumption rather than a fact.
func TestB20ReservedSpaceDeployment(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	statedb.SetBalance(creator, uint256.NewInt(1e18), 0)

	// An unused reserved address accepts the deployment...
	squatted := b20Addr(b20VariantAsset, 0x42)
	_, _, _, err := evm.create(creator, deployStopCode, NewGasBudget(1_000_000), uint256.NewInt(0), squatted, CREATE2)
	if err != nil {
		t.Fatalf("deploy into an unused reserved address: %v, want success (BEP-702 3.3)", err)
	}
	if code := statedb.GetCode(squatted); len(code) != 1 || code[0] != 0x00 {
		t.Fatalf("deployed code = %x, want the STOP byte", code)
	}

	// ...and the code is then unreachable: dispatch hands the address to the
	// token handler, which refuses because no token was ever created there.
	if p, ok := evm.precompile(squatted); !ok {
		t.Fatal("a squatted reserved address must still resolve to a token handler")
	} else if _, isAsset := p.(*b20AssetPrecompile); !isAsset {
		t.Fatalf("resolved %T, want the Asset handler", p)
	}
	if _, _, err := evm.Call(creator, squatted, nil, NewGasBudget(200_000), uint256.NewInt(0)); err == nil {
		t.Fatal("the squatter's STOP ran — its code must be unreachable, not merely shadowed")
	}

	// A live token cannot be deployed over: the sentinel is non-empty code, so
	// the ordinary collision check refuses.
	tok, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, common.HexToHash("0x01"), creator, nil),
		NewGasBudget(5_000_000), uint256.NewInt(0))
	if err != nil {
		t.Fatalf("createB20: %v", err)
	}
	token := common.BytesToAddress(tok)
	_, _, _, err = evm.create(creator, deployStopCode, NewGasBudget(1_000_000), uint256.NewInt(0), token, CREATE2)
	if !errors.Is(err, ErrContractAddressCollision) {
		t.Fatalf("deploy over a live token = %v, want a collision", err)
	}

	// Same for the registries, and this is what their sentinel is for: without
	// the code the fork seeds, these addresses would be plain empty accounts and
	// a deployment would land on them.
	for _, addr := range []common.Address{B20ActivationRegistryAddress, B20PolicyRegistryAddress} {
		_, _, _, err := evm.create(creator, deployStopCode, NewGasBudget(1_000_000), uint256.NewInt(0), addr, CREATE2)
		if !errors.Is(err, ErrContractAddressCollision) {
			t.Errorf("deploy over registry %s = %v, want a collision", addr.Hex(), err)
		}
	}
}

// TestB20CreateRejectsForeignCode covers the MUST in BEP-702 3.4: createB20 has
// to refuse a derived address that already carries code, not just one carrying
// the sentinel. The distinction matters because the two checks answer different
// questions — b20Initialized compares against the sentinel hash exactly, which a
// squatter's code does not match — so a factory that reused the existence check
// would happily overwrite foreign code, destroying it and inheriting whatever
// storage it left behind.
func TestB20CreateRejectsForeignCode(t *testing.T) {
	statedb, evm := newB20EVM(t)
	creator := common.HexToAddress("0xc4ea70")
	statedb.SetBalance(creator, uint256.NewInt(1e18), 0)

	salt := common.HexToHash("0xdead")
	target := b20DeriveAddress(b20VariantAsset, creator, salt)

	// Put foreign code at the address the factory is about to derive. Reaching a
	// specific address this way costs 2^160 in practice; the point here is that
	// the guard does not depend on that being infeasible.
	if _, _, _, err := evm.create(creator, deployStopCode, NewGasBudget(1_000_000), uint256.NewInt(0), target, CREATE2); err != nil {
		t.Fatalf("planting foreign code: %v", err)
	}
	if b20Initialized(statedb, target) {
		t.Fatal("foreign code must not satisfy the existence check")
	}

	_, _, err := evm.Call(creator, B20FactoryAddress,
		encodeCreateB20(b20VariantAsset, salt, creator, nil), NewGasBudget(5_000_000), uint256.NewInt(0))
	if !errors.Is(err, ErrExecutionReverted) {
		t.Fatalf("createB20 over foreign code = %v, want a revert (BEP-702 3.4)", err)
	}
	// The foreign code must survive: a rejection that still wrote the sentinel
	// would have destroyed it.
	if code := statedb.GetCode(target); len(code) != 1 || code[0] != 0x00 {
		t.Fatalf("code at the derived address = %x, want the untouched STOP byte", code)
	}
}
