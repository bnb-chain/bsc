package vm

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestB20NamespaceRoots pins every ERC-7201 storage root as a literal. A
// one-character change to a namespace relocates every slot under it, and the
// expectations must be literals: deriving them with erc7201Root passes whatever
// the namespace says.
//
// The roots have no published counterpart — base-std uses its own
// "base."-prefixed namespaces, so no root can match — and are regression anchors
// here, changeable only by editing this list. What does have a counterpart is
// the naming, checked in TestB20NamespacesMirrorBaseStd.
func TestB20NamespaceRoots(t *testing.T) {
	for _, tc := range []struct {
		namespace string
		got       common.Hash
		want      string
	}{
		{b20Namespace, b20CoreRoot, "0xd7d17b10507583ccbb27e6049e378ddb3a23890fde1bf3d25a473c9817975c00"},
		{b20AssetNamespace, b20AssetRoot, "0xbd7e2d89a2fdca3bc6bdfbef0d8ffdce85a7c64477784d85b13a60a9ff03b200"},
		{b20StablecoinNamespace, b20StablecoinRoot, "0xa255740e5778c5219db7c2fed675aee5760630ebae5750602b7ee4b5767b3100"},
		{b20ActivationNamespace, b20ActivationRoot, "0xa8970030726ea4c1e5fe64bf3ba11683da0b59f818265a7369e5570e9fc0bd00"},
		{b20PolicyNamespace, b20PolicyRoot, "0x2e7731329603a38e578303ba37c039549397ad42853922c474dfc3cb33d7b000"},
	} {
		if got := tc.got.Hex(); got != tc.want {
			t.Errorf("root for %q = %s, want %s", tc.namespace, got, tc.want)
		}
	}

	// The namespace strings themselves, so a rename has to be deliberate. The
	// two conventions here are worth noticing: storage namespaces separate the
	// variant with a dot, while the activation feature ids use an underscore
	// ("bsc.b20_asset"). Both are load-bearing and neither is interchangeable —
	// and both follow base-std, which draws the same distinction.
	for _, tc := range []struct{ got, want string }{
		{b20Namespace, "bsc.b20"},
		{b20AssetNamespace, "bsc.b20.asset"},
		{b20StablecoinNamespace, "bsc.b20.stablecoin"},
		{b20ActivationNamespace, "bsc.activation_registry"},
		{b20PolicyNamespace, "bsc.policy_registry"},
	} {
		if tc.got != tc.want {
			t.Errorf("namespace = %q, want %q", tc.got, tc.want)
		}
	}
}

// TestB20NamespacesMirrorBaseStd checks the two things about the namespaces that
// a counterpart exists for.
//
// The roots cannot match: a namespace string carries the chain's own prefix, so
// "bsc.b20" and "base.b20" hash apart by construction. What can match is the
// naming — one namespace per variant rather than the extensions sharing the core
// root at higher offsets — and it does, name for name with only the prefix
// swapped. base-std's storage mocks declare each one in an
// @custom:storage-location tag and publish its location as a constant
// (test/lib/mocks/MockB20Storage.sol, MockActivationRegistryStorage.sol,
// MockPolicyRegistryStorage.sol).
//
// Those five constants also serve as production vectors for erc7201Root: the
// formula test checks it against the one vector ERC-7201 publishes, which a
// misreading of the standard would satisfy alongside a wrong implementation.
// These come from a shipped chain that reads its own storage through them.
func TestB20NamespacesMirrorBaseStd(t *testing.T) {
	for _, tc := range []struct {
		ours, theirs, location string
	}{
		{b20Namespace, "base.b20", "0xc78b71fee795ddd74aff64ea9b2474194c938c3196430e10bb5f01ed48434000"},
		{b20AssetNamespace, "base.b20.asset", "0xfdc6d4552d1286ade4d9facdbf0fb50d2ec9b89a90e104f26fd277585e374b00"},
		{b20StablecoinNamespace, "base.b20.stablecoin", "0x35827975a06ca0e9367ea3129b19441d45d0ca58e30b7693f09e73d0943d6200"},
		{b20ActivationNamespace, "base.activation_registry", "0x43ee1bbe25e988521cccd8b2c8fbd38c8287ebff8e074e825a70dfd3885cce00"},
		{b20PolicyNamespace, "base.policy_registry", "0x00503aeb06982fa1fe3151dc68f90b3946c55c449dfd447e49dcaece71ba4a00"},
	} {
		if got := strings.Replace(tc.ours, "bsc.", "base.", 1); got != tc.theirs {
			t.Errorf("%q renames to %q, but base-std calls it %q", tc.ours, got, tc.theirs)
		}
		if got := erc7201Root(tc.theirs).Hex(); got != tc.location {
			t.Errorf("erc7201Root(%q) = %s, want %s (base-std's published location)",
				tc.theirs, got, tc.location)
		}
	}
}

// TestB20FeatureIDs pins the three activation feature ids as literals. A feature
// id is what governance names in an activate() proposal, so a typo opens an id no
// gate ever reads: the feature stays shut and the vote looks like it succeeded.
// BEP-702 3.15 publishes them, making this list the contract with the spec.
//
// The spec was renamed to N20 ahead of the code and now publishes
// keccak256("bsc.n20_asset") and keccak256("bsc.n20_stablecoin"), different
// values from the two below; the code rename has to carry them.
// "bsc.policy_registry" is unaffected.
func TestB20FeatureIDs(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  common.Hash
		want string
	}{
		{"bsc.b20_asset", featureB20Asset, "0x31878586514f9d016ce7c189a3d4e9a41924e23063e9711eebb579ec61cb15d5"},
		{"bsc.b20_stablecoin", featureB20Stablecoin, "0x8c527c688a2852724aabfe69efdd2dbaf4f2ca3782f91595570a95eab45f900d"},
		{"bsc.policy_registry", featurePolicyRegistry, "0xcc84b168b7eedbce699f3234b0a635f610f4aa826d72cc6a19f5b9a264557edf"},
	} {
		if got := tc.got.Hex(); got != tc.want {
			t.Errorf("feature id for %q = %s, want %s", tc.name, got, tc.want)
		}
		// The preimage too, so a rename cannot quietly keep the old hash.
		if got := crypto.Keccak256Hash([]byte(tc.name)); got != tc.got {
			t.Errorf("%q does not hash to the registered id: %s vs %s", tc.name, got.Hex(), tc.got.Hex())
		}
	}

	// Distinctness is what makes one switch per feature meaningful.
	seen := map[common.Hash]bool{}
	for _, f := range []common.Hash{featureB20Asset, featureB20Stablecoin, featurePolicyRegistry} {
		if seen[f] {
			t.Errorf("duplicate feature id %s — two features would share one switch", f.Hex())
		}
		seen[f] = true
	}

	// And the variant-to-feature mapping, which decides which switch createB20
	// consults. Swapping the two arms would gate each variant on the other's
	// feature.
	if f, ok := variantFeature(b20VariantAsset); !ok || f != featureB20Asset {
		t.Errorf("variantFeature(asset) = %s ok=%v, want the asset feature", f.Hex(), ok)
	}
	if f, ok := variantFeature(b20VariantStablecoin); !ok || f != featureB20Stablecoin {
		t.Errorf("variantFeature(stablecoin) = %s ok=%v, want the stablecoin feature", f.Hex(), ok)
	}
	if _, ok := variantFeature(0x02); ok {
		t.Error("an unrecognized variant must map to no feature")
	}
}

// TestB20ERC7201Formula checks erc7201Root against the vector published with
// ERC-7201 itself. Re-deriving the formula a second way catches a coding slip but
// not a misreading of the standard, since both copies would be wrong together.
func TestB20ERC7201Formula(t *testing.T) {
	const (
		example = "example.main"
		want    = "0x183a6125c38840424c4a85fa12bab2ab606c4b6d0e7cc73c0c06ba5300eab500"
	)
	if got := erc7201Root(example).Hex(); got != want {
		t.Errorf("erc7201Root(%q) = %s, want %s (ERC-7201's published vector)", example, got, want)
	}

	// And spell the formula out once, so the shape is documented where it is
	// asserted: keccak256(abi.encode(uint256(keccak256(id)) - 1)) with the low
	// byte cleared.
	inner := new(big.Int).SetBytes(crypto.Keccak256([]byte(example)))
	inner.Sub(inner, big.NewInt(1))
	var buf [32]byte
	inner.FillBytes(buf[:])
	exp := crypto.Keccak256Hash(buf[:])
	exp[31] = 0
	if exp.Hex() != want {
		t.Fatalf("the spelled-out formula gives %s, want %s", exp.Hex(), want)
	}
}
