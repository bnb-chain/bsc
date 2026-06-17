package vm

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// 0x64/0x65 are deprecated from Pasteur; 0x67 switches to a per-byte-priced variant.
func TestPasteurSuspendsLegacyTendermintPrecompiles(t *testing.T) {
	addr64 := common.BytesToAddress([]byte{0x64})
	addr65 := common.BytesToAddress([]byte{0x65})

	// Under Pasteur, 0x64/0x65 return "deprecated" for any input.
	for _, addr := range []common.Address{addr64, addr65} {
		p := PrecompiledContractsPasteur[addr]
		if p == nil {
			t.Fatalf("%s missing from Pasteur precompile set", addr.Hex())
		}
		if _, err := p.Run([]byte("x")); err == nil || err.Error() != "deprecated" {
			t.Fatalf("%s under Pasteur: expected deprecated error, got %v", addr.Hex(), err)
		}
	}

	// Pre-Pasteur (Osaka) they are still the live, active implementations.
	if _, ok := PrecompiledContractsOsaka[addr64].(*tmHeaderValidate); !ok {
		t.Fatalf("0x64 must remain active (tmHeaderValidate) pre-Pasteur")
	}
	if _, ok := PrecompiledContractsOsaka[addr65].(*iavlMerkleProofValidatePlato); !ok {
		t.Fatalf("0x65 must remain active (iavlMerkleProofValidatePlato) pre-Pasteur")
	}

	// 0x67 stays active but switches to the per-byte-priced Pasteur variant
	// (Hertz/flat-gas pre-Pasteur, for replay of earlier blocks).
	addr67 := common.BytesToAddress([]byte{0x67})
	if _, ok := PrecompiledContractsPasteur[addr67].(*cometBFTLightBlockValidatePasteur); !ok {
		t.Fatalf("0x67 must be the Pasteur variant under Pasteur")
	}
	if _, ok := PrecompiledContractsOsaka[addr67].(*cometBFTLightBlockValidateHertz); !ok {
		t.Fatalf("0x67 must remain the Hertz variant pre-Pasteur")
	}

	// The still-needed precompiles (0x66, 0x68, 0x69) remain present under Pasteur.
	for _, b := range []byte{0x66, 0x68, 0x69} {
		addr := common.BytesToAddress([]byte{b})
		if PrecompiledContractsPasteur[addr] == nil {
			t.Fatalf("%s must remain present under Pasteur", addr.Hex())
		}
	}

	// 0x67 gas scales with input size under Pasteur, but is flat pre-Pasteur.
	small := make([]byte, 1024)
	large := make([]byte, 16*1024)
	pasteur67 := PrecompiledContractsPasteur[addr67]
	if pasteur67.RequiredGas(large) <= pasteur67.RequiredGas(small) {
		t.Fatalf("Pasteur 0x67 gas must scale with input size")
	}
	osaka67 := PrecompiledContractsOsaka[addr67]
	if osaka67.RequiredGas(large) != osaka67.RequiredGas(small) {
		t.Fatalf("pre-Pasteur 0x67 gas must stay flat")
	}
}
