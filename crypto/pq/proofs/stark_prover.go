package proofs

import (
	"crypto/sha256"
	"errors"
	"math/big"
	"sync"
)

// Errors
var (
	ErrNilTrace       = errors.New("stark_prover: nil execution trace")
	ErrEmptyTrace     = errors.New("stark_prover: empty execution trace")
	ErrProofGenFailed = errors.New("stark_prover: proof generation failed")
)

// FieldElement represents an element in the STARK finite field.
type FieldElement struct {
	Value *big.Int
}

// NewFieldElement creates a FieldElement from an int64.
func NewFieldElement(v int64) FieldElement {
	return FieldElement{Value: big.NewInt(v)}
}

// STARKConstraint represents a constraint in the STARK proof system.
type STARKConstraint struct {
	Degree       int
	Coefficients []FieldElement
}

// STARKProofData holds the generated STARK proof.
type STARKProofData struct {
	// CommitmentRoot is the Merkle commitment to the execution trace.
	CommitmentRoot [32]byte
	// FRILayers stores the FRI (Fast Reed-Solomon IOP) layer commitments.
	FRILayers [][32]byte
	// QueryResponses holds query/response pairs for the interactive portion.
	QueryResponses []QueryResponse
	// TraceLength is the number of rows in the execution trace.
	TraceLength int
	// NumColumns is the number of columns in the execution trace.
	NumColumns int
}

// QueryResponse is a single query-response in the STARK proof.
type QueryResponse struct {
	Index    int
	Value    [32]byte
	AuthPath [][32]byte
}

// ProofSize returns the approximate byte size of the proof.
func (p *STARKProofData) ProofSize() int {
	if p == nil {
		return 0
	}
	size := 32 // CommitmentRoot
	size += len(p.FRILayers) * 32
	for _, qr := range p.QueryResponses {
		size += 4 + 32 + len(qr.AuthPath)*32 // Index + Value + AuthPath
	}
	return size
}

// STARKProver generates and verifies STARK proofs.
type STARKProver struct {
	mu sync.RWMutex
}

// NewSTARKProver creates a new STARKProver.
func NewSTARKProver() *STARKProver {
	return &STARKProver{}
}

// GenerateSTARKProof generates a STARK proof from an execution trace and constraints.
// The trace is a 2D matrix where each row is one "step" and each column is a register.
// For signature aggregation, each row represents one attestation's data.
//
// TODO(pq-phase3): This is a placeholder implementation that builds Merkle commitments
// over the execution trace but does NOT perform real STARK proving (polynomial
// interpolation, FRI commitment, constraint evaluation). Replace with a production
// STARK/leanVM prover before enabling on any public network.
func (sp *STARKProver) GenerateSTARKProof(trace [][]FieldElement, constraints []STARKConstraint) (*STARKProofData, error) {
	if trace == nil {
		return nil, ErrNilTrace
	}
	if len(trace) == 0 {
		return nil, ErrEmptyTrace
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	numColumns := 0
	if len(trace) > 0 && len(trace[0]) > 0 {
		numColumns = len(trace[0])
	}

	// Step 1: Compute commitment root from the execution trace.
	// Hash each row, then build a Merkle tree.
	rowHashes := make([][32]byte, len(trace))
	for i, row := range trace {
		h := sha256.New()
		for _, elem := range row {
			if elem.Value != nil {
				h.Write(elem.Value.Bytes())
			} else {
				h.Write([]byte{0})
			}
		}
		copy(rowHashes[i][:], h.Sum(nil))
	}
	commitmentRoot := computeMerkleRoot(rowHashes)

	// Step 2: Generate FRI layer commitments.
	// Simulate FRI folding: log2(traceLength) layers.
	numLayers := 0
	n := len(trace)
	for n > 1 {
		n = (n + 1) / 2
		numLayers++
	}
	if numLayers == 0 {
		numLayers = 1
	}

	friLayers := make([][32]byte, numLayers)
	currentHashes := rowHashes
	for layer := 0; layer < numLayers; layer++ {
		nextLen := (len(currentHashes) + 1) / 2
		nextHashes := make([][32]byte, nextLen)
		for j := 0; j < nextLen; j++ {
			h := sha256.New()
			h.Write(currentHashes[j*2][:])
			if j*2+1 < len(currentHashes) {
				h.Write(currentHashes[j*2+1][:])
			} else {
				h.Write(currentHashes[j*2][:]) // duplicate last if odd
			}
			copy(nextHashes[j][:], h.Sum(nil))
		}
		friLayers[layer] = nextHashes[0]
		currentHashes = nextHashes
	}

	// Step 3: Generate query responses.
	// For each constraint, produce a query response at deterministic indices.
	numQueries := len(constraints)
	if numQueries == 0 {
		numQueries = 1
	}
	if numQueries > len(trace) {
		numQueries = len(trace)
	}

	queryResponses := make([]QueryResponse, numQueries)
	for q := 0; q < numQueries; q++ {
		idx := q % len(trace)
		queryResponses[q] = QueryResponse{
			Index:    idx,
			Value:    rowHashes[idx],
			AuthPath: computeAuthPath(rowHashes, idx),
		}
	}

	return &STARKProofData{
		CommitmentRoot: commitmentRoot,
		FRILayers:      friLayers,
		QueryResponses: queryResponses,
		TraceLength:    len(trace),
		NumColumns:     numColumns,
	}, nil
}

// VerifySTARKProof verifies a STARK proof.
// publicInputs can be nil for basic verification.
//
// TODO(pq-phase3): This is a placeholder that only checks Merkle auth paths.
// It does NOT verify actual STARK constraints, FRI consistency, or that the
// underlying PQ signatures are valid. Must be replaced with a real STARK
// verifier (or leanVM verifier) before any testnet deployment.
func (sp *STARKProver) VerifySTARKProof(proof *STARKProofData, publicInputs []FieldElement) (bool, error) {
	if proof == nil {
		return false, ErrProofGenFailed
	}

	sp.mu.RLock()
	defer sp.mu.RUnlock()

	// Verify FRI layer chain: each layer should be consistent.
	if len(proof.FRILayers) == 0 {
		return false, errors.New("stark_prover: no FRI layers")
	}

	// Verify query responses against the commitment root.
	for _, qr := range proof.QueryResponses {
		if !verifyAuthPath(proof.CommitmentRoot, qr.Value, qr.AuthPath, qr.Index, proof.TraceLength) {
			return false, errors.New("stark_prover: auth path verification failed")
		}
	}

	return true, nil
}

// computeMerkleRoot computes a Merkle root from a list of leaf hashes.
func computeMerkleRoot(leaves [][32]byte) [32]byte {
	if len(leaves) == 0 {
		return [32]byte{}
	}
	if len(leaves) == 1 {
		return leaves[0]
	}

	// Pad to next power of two.
	target := 1
	for target < len(leaves) {
		target <<= 1
	}
	padded := make([][32]byte, target)
	copy(padded, leaves)

	layer := padded
	for len(layer) > 1 {
		next := make([][32]byte, len(layer)/2)
		for i := range next {
			h := sha256.New()
			h.Write(layer[2*i][:])
			h.Write(layer[2*i+1][:])
			copy(next[i][:], h.Sum(nil))
		}
		layer = next
	}
	return layer[0]
}

// computeAuthPath computes the Merkle authentication path for a leaf at the given index.
func computeAuthPath(leaves [][32]byte, index int) [][32]byte {
	if len(leaves) <= 1 {
		return nil
	}

	target := 1
	for target < len(leaves) {
		target <<= 1
	}
	padded := make([][32]byte, target)
	copy(padded, leaves)

	var path [][32]byte
	layer := padded
	idx := index

	for len(layer) > 1 {
		sibling := idx ^ 1
		if sibling < len(layer) {
			path = append(path, layer[sibling])
		}
		next := make([][32]byte, len(layer)/2)
		for i := range next {
			h := sha256.New()
			h.Write(layer[2*i][:])
			h.Write(layer[2*i+1][:])
			copy(next[i][:], h.Sum(nil))
		}
		layer = next
		idx = idx / 2
	}
	return path
}

// verifyAuthPath verifies a Merkle authentication path.
func verifyAuthPath(root [32]byte, leaf [32]byte, authPath [][32]byte, index int, totalLeaves int) bool {
	current := leaf
	idx := index

	for _, sibling := range authPath {
		h := sha256.New()
		if idx%2 == 0 {
			h.Write(current[:])
			h.Write(sibling[:])
		} else {
			h.Write(sibling[:])
			h.Write(current[:])
		}
		copy(current[:], h.Sum(nil))
		idx = idx / 2
	}

	return current == root
}
