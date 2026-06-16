package parlia

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"fmt"
	"math/big"
	mrand "math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	cmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
	"golang.org/x/crypto/sha3"
)

const (
	upperLimitOfVoteBlockNumber = 11
)

func TestImpactOfValidatorOutOfService(t *testing.T) {
	testCases := []struct {
		totalValidators int
		downValidators  int
		turnLength      int
	}{
		{3, 1, 1},
		{5, 2, 1},
		{10, 1, 2},
		{10, 4, 2},
		{21, 1, 3},
		{21, 3, 3},
		{21, 5, 4},
		{21, 10, 5},
	}
	for _, tc := range testCases {
		simulateValidatorOutOfService(tc.totalValidators, tc.downValidators, tc.turnLength)
	}
}

// refer Snapshot.SignRecently
func signRecently(idx int, recents map[uint64]int, turnLength int) bool {
	recentSignTimes := 0
	for _, signIdx := range recents {
		if signIdx == idx {
			recentSignTimes += 1
		}
	}
	return recentSignTimes >= turnLength
}

// refer Snapshot.minerHistoryCheckLen
func minerHistoryCheckLen(totalValidators int, turnLength int) uint64 {
	return uint64(totalValidators/2+1)*uint64(turnLength) - 1
}

// refer Snapshot.inturnValidator
func inturnValidator(totalValidators int, turnLength int, height int) int {
	return height / turnLength % totalValidators
}

func simulateValidatorOutOfService(totalValidators int, downValidators int, turnLength int) {
	downBlocks := 10000
	recoverBlocks := 10000
	recents := make(map[uint64]int)

	validators := make(map[int]bool, totalValidators)
	down := make([]int, totalValidators)
	for i := 0; i < totalValidators; i++ {
		validators[i] = true
		down[i] = i
	}
	mrand.Shuffle(totalValidators, func(i, j int) {
		down[i], down[j] = down[j], down[i]
	})
	for i := 0; i < downValidators; i++ {
		delete(validators, down[i])
	}
	isRecentSign := func(idx int) bool {
		return signRecently(idx, recents, turnLength)
	}
	isInService := func(idx int) bool {
		return validators[idx]
	}

	downDelay := uint64(0)
	for h := 1; h <= downBlocks; h++ {
		if limit := minerHistoryCheckLen(totalValidators, turnLength) + 1; uint64(h) >= limit {
			delete(recents, uint64(h)-limit)
		}
		proposer := inturnValidator(totalValidators, turnLength, h)
		if !isInService(proposer) || isRecentSign(proposer) {
			candidates := make(map[int]bool, totalValidators/2)
			for v := range validators {
				if !isRecentSign(v) {
					candidates[v] = true
				}
			}
			if len(candidates) == 0 {
				panic("can not test such case")
			}
			idx, delay := producerBlockDelay(candidates, h, totalValidators)
			downDelay = downDelay + delay
			recents[uint64(h)] = idx
		} else {
			recents[uint64(h)] = proposer
		}
	}
	fmt.Printf("average delay is %v  when there is %d validators and %d is down \n",
		downDelay/uint64(downBlocks), totalValidators, downValidators)

	for i := 0; i < downValidators; i++ {
		validators[down[i]] = true
	}

	recoverDelay := uint64(0)
	lastseen := downBlocks
	for h := downBlocks + 1; h <= downBlocks+recoverBlocks; h++ {
		if limit := minerHistoryCheckLen(totalValidators, turnLength) + 1; uint64(h) >= limit {
			delete(recents, uint64(h)-limit)
		}
		proposer := inturnValidator(totalValidators, turnLength, h)
		if !isInService(proposer) || isRecentSign(proposer) {
			lastseen = h
			candidates := make(map[int]bool, totalValidators/2)
			for v := range validators {
				if !isRecentSign(v) {
					candidates[v] = true
				}
			}
			if len(candidates) == 0 {
				panic("can not test such case")
			}
			idx, delay := producerBlockDelay(candidates, h, totalValidators)
			recoverDelay = recoverDelay + delay
			recents[uint64(h)] = idx
		} else {
			recents[uint64(h)] = proposer
		}
	}
	fmt.Printf("total delay is %v after recover when there is %d validators down ever, last seen not proposer at height %d\n",
		recoverDelay, downValidators, lastseen)
}

func producerBlockDelay(candidates map[int]bool, height, numOfValidators int) (int, uint64) {
	s := mrand.NewSource(int64(height))
	r := mrand.New(s)
	n := numOfValidators
	backOffSteps := make([]int, 0, n)
	for idx := 0; idx < n; idx++ {
		backOffSteps = append(backOffSteps, idx)
	}
	r.Shuffle(n, func(i, j int) {
		backOffSteps[i], backOffSteps[j] = backOffSteps[j], backOffSteps[i]
	})
	minDelay := numOfValidators
	minCandidate := 0
	for c := range candidates {
		if minDelay > backOffSteps[c] {
			minDelay = backOffSteps[c]
			minCandidate = c
		}
	}
	delay := defaultInitialBackOffTime + uint64(minDelay)*wiggleTime
	return minCandidate, delay
}

func randomAddress() common.Address {
	addrBytes := make([]byte, 20)
	rand.Read(addrBytes)
	return common.BytesToAddress(addrBytes)
}

// =========================================================================
// =======     Simulator P2P network to verify fast finality    ============
// =========================================================================

type MockBlock struct {
	parent *MockBlock

	blockNumber uint64
	blockHash   common.Hash
	coinbase    *MockValidator
	td          uint64 // Total difficulty from genesis block to current block
	attestation uint64 // Vote attestation for parent block, zero means no attestation
}

var GenesisBlock = &MockBlock{
	parent:      nil,
	blockNumber: 0,
	blockHash:   common.Hash{},
	coinbase:    nil,
	td:          diffInTurn.Uint64(),
	attestation: 0,
}

func (b *MockBlock) Hash() (hash common.Hash) {
	hasher := sha3.NewLegacyKeccak256()
	rlp.Encode(hasher, []interface{}{
		b.parent,
		b.blockNumber,
		b.coinbase,
		b.td,
		b.attestation,
	})
	hasher.Sum(hash[:0])
	return hash
}

func (b *MockBlock) IsConflicted(a *MockBlock) bool {
	if a.blockNumber > b.blockNumber {
		p := a.parent
		for ; p.blockNumber > b.blockNumber; p = p.parent {
		}

		return p.blockHash != b.blockHash
	}

	if a.blockNumber < b.blockNumber {
		p := b.parent
		for ; p.blockNumber > a.blockNumber; p = p.parent {
		}

		return p.blockHash != a.blockHash
	}

	return a.blockHash != b.blockHash
}

// GetJustifiedNumberAndHash returns number and hash of the highest justified block,
// keep same func signature with consensus even if `error` will be nil definitely
func (b *MockBlock) GetJustifiedNumberAndHash() (uint64, common.Hash, error) {
	justifiedBlock := GenesisBlock
	for curBlock := b; curBlock.blockNumber > 1; curBlock = curBlock.parent {
		// justified
		if curBlock.attestation != 0 {
			justifiedBlock = curBlock.parent
			break
		}
	}

	return justifiedBlock.blockNumber, justifiedBlock.blockHash, nil
}

func (b *MockBlock) GetJustifiedNumber() uint64 {
	justifiedBlockNumber, _, _ := b.GetJustifiedNumberAndHash()
	return justifiedBlockNumber
}

// GetFinalizedBlock returns highest finalized block,
// include current block's attestation.
func (b *MockBlock) GetFinalizedBlock() *MockBlock {
	if b.blockNumber < 3 {
		return GenesisBlock
	}

	if b.attestation != 0 && b.parent.attestation != 0 {
		return b.parent.parent
	}

	return b.parent.GetFinalizedBlock()
}

type MockValidator struct {
	index        int
	validatorSet int // validators number
	head         *MockBlock
	voteRecords  map[uint64]*types.VoteData
}

func NewMockValidator(index int, validatorSet int) *MockValidator {
	v := &MockValidator{
		index:        index,
		validatorSet: validatorSet,
		head:         GenesisBlock,
		voteRecords:  make(map[uint64]*types.VoteData),
	}
	return v
}

func (v *MockValidator) SignRecently() bool {
	parent := v.head
	for i := 0; i < v.validatorSet*1/2; i++ {
		if parent.blockNumber == 0 {
			return false
		}

		if parent.coinbase == v {
			return true
		}

		parent = parent.parent
	}

	return false
}

func (v *MockValidator) Produce(attestation uint64) (*MockBlock, error) {
	if v.SignRecently() {
		return nil, fmt.Errorf("v %d sign recently", v.index)
	}

	block := &MockBlock{
		parent:      v.head,
		blockNumber: v.head.blockNumber + 1,
		coinbase:    v,
		td:          v.head.td + 1,
		attestation: attestation,
	}

	if (block.blockNumber-1)%uint64(v.validatorSet) == uint64(v.index) {
		block.td = v.head.td + 2
	}

	block.blockHash = block.Hash()
	return block, nil
}

func (v *MockValidator) Vote(block *MockBlock) bool {
	// Rule 3: The block should be the latest block of canonical chain
	if block != v.head {
		return false
	}

	// Rule 1: No double vote
	if _, ok := v.voteRecords[block.blockNumber]; ok {
		return false
	}

	// Rule 2: No surround vote
	justifiedBlockNumber, justifiedBlockHash, _ := block.GetJustifiedNumberAndHash()
	for targetNumber := justifiedBlockNumber + 1; targetNumber < block.blockNumber; targetNumber++ {
		if vote, ok := v.voteRecords[targetNumber]; ok {
			if vote.SourceNumber > justifiedBlockNumber {
				return false
			}
		}
	}
	for targetNumber := block.blockNumber; targetNumber <= block.blockNumber+upperLimitOfVoteBlockNumber; targetNumber++ {
		if vote, ok := v.voteRecords[targetNumber]; ok {
			if vote.SourceNumber < justifiedBlockNumber {
				return false
			}
		}
	}

	v.voteRecords[block.blockNumber] = &types.VoteData{
		SourceNumber: justifiedBlockNumber,
		SourceHash:   justifiedBlockHash,
		TargetNumber: block.blockNumber,
		TargetHash:   block.blockHash,
	}
	return true
}

func (v *MockValidator) InsertBlock(block *MockBlock) {
	// Reject block too old.
	if block.blockNumber+13 < v.head.blockNumber {
		return
	}

	// The higher justified block is the longest chain.
	if block.GetJustifiedNumber() < v.head.GetJustifiedNumber() {
		return
	}
	if block.GetJustifiedNumber() > v.head.GetJustifiedNumber() {
		v.head = block
		return
	}

	// The same finalized number, the larger difficulty is the longest chain.
	if block.td > v.head.td {
		v.head = block
	}
}

type BlockSimulator struct {
	blockNumber   uint64
	coinbaseIndex int
	voteMap       uint64
	insertMap     uint64
}

type ChainSimulator []*BlockSimulator

func (s ChainSimulator) Valid() bool {
	var pre *BlockSimulator
	for index, bs := range s {
		if index == 0 {
			if bs.blockNumber != 1 {
				return false
			}
		} else {
			if bs.blockNumber != pre.blockNumber+1 {
				return false
			}
		}

		pre = bs
	}
	return true
}

type Coordinator struct {
	validators   []*MockValidator
	attestations map[common.Hash]uint64
}

func NewCoordinator(validatorsNumber int) *Coordinator {
	validators := make([]*MockValidator, validatorsNumber)
	for i := 0; i < validatorsNumber; i++ {
		validators[i] = NewMockValidator(i, validatorsNumber)
	}

	return &Coordinator{
		validators:   validators,
		attestations: make(map[common.Hash]uint64),
	}
}

// SimulateP2P simulate a P2P network
func (c *Coordinator) SimulateP2P(cs ChainSimulator) error {
	for _, bs := range cs {
		parent := c.validators[bs.coinbaseIndex].head
		if bs.blockNumber != parent.blockNumber+1 {
			return fmt.Errorf("can't produce discontinuous block, head block: %d, expect produce: %d", parent.blockNumber, bs.blockNumber)
		}
		attestation := c.attestations[parent.blockHash]
		block, err := c.validators[bs.coinbaseIndex].Produce(attestation)
		if err != nil {
			return fmt.Errorf("produce block %v error %v", bs, err)
		}

		c.PropagateBlock(bs, block)
		err = c.AggregateVotes(bs, block)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Coordinator) AggregateVotes(bs *BlockSimulator, block *MockBlock) error {
	var attestation uint64
	count := 0
	for index, voteMap := 0, bs.voteMap; voteMap > 0; index, voteMap = index+1, voteMap>>1 {
		if voteMap&0x1 == 0 {
			continue
		}

		if !c.validators[index].Vote(block) {
			return fmt.Errorf("validator(%d) couldn't vote for block %d produced by validator(%d)", index, block.blockNumber, block.coinbase.index)
		}
		attestation |= 1 << index
		count++
	}

	if count >= cmath.CeilDiv(len(c.validators)*2, 3) {
		c.attestations[block.blockHash] = attestation
	}

	return nil
}

func (c *Coordinator) PropagateBlock(bs *BlockSimulator, block *MockBlock) {
	for index, insertMap := 0, bs.insertMap; insertMap > 0; index, insertMap = index+1, insertMap>>1 {
		if insertMap&0x1 == 0 {
			continue
		}

		c.validators[index].InsertBlock(block)
	}
}

func (c *Coordinator) CheckChain() bool {
	// All validators highest finalized block should not be conflicted
	finalizedBlocks := make([]*MockBlock, len(c.validators))
	for index, val := range c.validators {
		finalizedBlocks[index] = val.head.GetFinalizedBlock()
	}

	for i := 0; i < len(finalizedBlocks)-1; i++ {
		for j := i + 1; j < len(finalizedBlocks); j++ {
			if finalizedBlocks[i].IsConflicted(finalizedBlocks[j]) {
				return false
			}
		}
	}

	return true
}

type TestSimulatorParam struct {
	validatorsNumber int
	cs               ChainSimulator
}

var simulatorTestcases = []*TestSimulatorParam{
	{
		// 3 validators, all active
		validatorsNumber: 3,
		cs: []*BlockSimulator{
			{1, 0, 0x7, 0x7},
			{2, 1, 0x7, 0x7},
			{3, 2, 0x7, 0x7},
			{4, 0, 0x7, 0x7},
			{5, 1, 0x7, 0x7},
		},
	},
	{
		// 5 validators, 4 active, 1 down
		validatorsNumber: 5,
		cs: []*BlockSimulator{
			{1, 0, 0x1f, 0x1f},
			{2, 1, 0x1f, 0x1f},
			{3, 2, 0x1f, 0x1f},
			{4, 3, 0x1f, 0x1f},
			{5, 0, 0x1f, 0x1f},
			{6, 1, 0x1f, 0x1f},
			{7, 2, 0x1f, 0x1f},
		},
	},
	{
		// 21 validators, all active
		validatorsNumber: 21,
		cs: []*BlockSimulator{
			{1, 0, 0x1fffff, 0x1fffff},
			{2, 1, 0x1fffff, 0x1fffff},
			{3, 2, 0x1fffff, 0x1fffff},
			{4, 3, 0x1fffff, 0x1fffff},
			{5, 4, 0x1fffff, 0x1fffff},
			{6, 5, 0x1fffff, 0x1fffff},
			{7, 6, 0x1fffff, 0x1fffff},
			{8, 7, 0x1fffff, 0x1fffff},
			{9, 8, 0x1fffff, 0x1fffff},
			{10, 9, 0x1fffff, 0x1fffff},
			{11, 10, 0x1fffff, 0x1fffff},
			{12, 11, 0x1fffff, 0x1fffff},
			{13, 12, 0x1fffff, 0x1fffff},
			{14, 13, 0x1fffff, 0x1fffff},
			{15, 14, 0x1fffff, 0x1fffff},
			{16, 0, 0x1fffff, 0x1fffff},
			{17, 1, 0x1fffff, 0x1fffff},
			{18, 2, 0x1fffff, 0x1fffff},
		},
	},
	{
		// 21 validators, all active, the finalized fork can keep grow
		validatorsNumber: 21,
		cs: []*BlockSimulator{
			{1, 1, 0x00fffe, 0x00fffe},
			{2, 2, 0x00fffe, 0x00fffe},
			{1, 0, 0x1f0001, 0x1fffff},
			{2, 16, 0x1f0001, 0x1ffff1},
			{3, 17, 0x1f0001, 0x1ffff1},
			{4, 18, 0x1f0001, 0x1ffff1},
			{5, 19, 0x1f0001, 0x1ffff1},
			{3, 3, 0x00fffe, 0x00fffe}, // justify block 2 and finalize block 1
			{6, 20, 0x1f0001, 0x1fffff},
			{4, 4, 0x00fffe, 0x1fffff},
			{5, 5, 0x00fffe, 0x1fffff},
			{6, 6, 0x00fffe, 0x1fffff},
			{7, 7, 0x1fffff, 0x1fffff},
			{8, 8, 0x1fffff, 0x1fffff},
		},
	},
	{
		// 21 validators, all active, the finalized fork can keep grow
		validatorsNumber: 21,
		cs: []*BlockSimulator{
			{1, 14, 0x00fffe, 0x00fffe},
			{2, 15, 0x00fffe, 0x00fffe}, // The block 3 will never produce
			{1, 0, 0x1f0001, 0x1fffff},
			{2, 16, 0x1f0001, 0x1fffff},
			{3, 1, 0x1f0001, 0x1fffff}, // based block produced by 15
			{4, 2, 0x1f0001, 0x1fffff},
			{5, 3, 0x1f0001, 0x1fffff},
			{6, 4, 0x1f0001, 0x1fffff},
			{7, 5, 0x1f0001, 0x1fffff},
			{8, 6, 0x1f0001, 0x1fffff},
			{9, 7, 0x1f0001, 0x1fffff},
			{10, 8, 0x1f0001, 0x1fffff},
			{11, 9, 0x1f0001, 0x1fffff},
			{12, 10, 0x1f0001, 0x1fffff},
			{13, 11, 0x1f0001, 0x1fffff},
			{14, 12, 0x1f0001, 0x1fffff},
			{15, 13, 0x1f0001, 0x1fffff},
			{16, 14, 0x1f0001, 0x1fffff},
			{17, 15, 0x1fffff, 0x1fffff}, // begin new round vote
			{18, 16, 0x1fffff, 0x1fffff}, // attestation for block 17
			{19, 17, 0x1fffff, 0x1fffff}, // attestation for block 18
		},
	},
}

func TestSimulateP2P(t *testing.T) {
	for index, testcase := range simulatorTestcases {
		c := NewCoordinator(testcase.validatorsNumber)
		err := c.SimulateP2P(testcase.cs)
		if err != nil {
			t.Fatalf("[Testcase %d] simulate P2P error: %v", index, err)
		}
		for _, val := range c.validators {
			t.Logf("[Testcase %d] validator(%d) head block: %d",
				index, val.index, val.head.blockNumber)
			t.Logf("[Testcase %d] validator(%d) highest justified block: %d",
				index, val.index, val.head.GetJustifiedNumber())
			t.Logf("[Testcase %d] validator(%d) highest finalized block: %d",
				index, val.index, val.head.GetFinalizedBlock().blockNumber)
		}

		if c.CheckChain() == false {
			t.Fatalf("[Testcase %d] chain not works as expected", index)
		}
	}
}

var (
	// testKey is a private key to use for funding a tester account.
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	testAddr   = crypto.PubkeyToAddress(testKey.PublicKey)
)

func TestParlia_applyTransactionTracing(t *testing.T) {
	frdir := t.TempDir()
	db, err := rawdb.NewDatabaseWithFreezer(rawdb.NewMemoryDatabase(), frdir, "", false)
	if err != nil {
		t.Fatalf("failed to create database with ancient backend")
	}

	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	config := params.ParliaTestChainConfig
	gspec := &core.Genesis{
		Config: params.ParliaTestChainConfig,
		Alloc:  types.GenesisAlloc{testAddr: {Balance: new(big.Int).SetUint64(10 * params.Ether)}},
	}

	mockEngine := &mockParlia{}
	genesisBlock := gspec.MustCommit(db, trieDB)

	chain, _ := core.NewBlockChain(db, gspec, mockEngine, nil)
	signer := types.LatestSigner(config)

	bs, _ := core.GenerateChain(config, genesisBlock, mockEngine, db, 1, func(i int, gen *core.BlockGen) {
		if !config.IsCancun(gen.Number(), gen.Timestamp()) {
			tx, _ := makeMockTx(config, signer, testKey, gen.TxNonce(testAddr), gen.BaseFee().Uint64(), eip4844.CalcBlobFee(config, gen.HeadBlock()).Uint64(), false)
			gen.AddTxWithChain(chain, tx)
			return
		}
		tx, sidecar := makeMockTx(config, signer, testKey, gen.TxNonce(testAddr), gen.BaseFee().Uint64(), eip4844.CalcBlobFee(config, gen.HeadBlock()).Uint64(), true)
		gen.AddTxWithChain(chain, tx)
		gen.AddBlobSidecar(&types.BlobSidecar{
			BlobTxSidecar: *sidecar,
			TxIndex:       0,
			TxHash:        tx.Hash(),
		})
	})

	engine := New(params.ParliaTestChainConfig, db, nil, genesisBlock.Hash())

	stateDatabase := state.NewDatabase(trieDB, nil)
	stateDB, err := state.New(genesisBlock.Root(), stateDatabase)
	if err != nil {
		t.Fatalf("failed to create stateDB: %v", err)
	}

	method := "distributeFinalityReward"
	data, err := engine.validatorSetABI.Pack(method, make([]common.Address, 0), make([]*big.Int, 0))
	if err != nil {
		t.Fatalf("failed to pack system contract method %s: %v", method, err)
	}

	msg := engine.getSystemMessage(genesisBlock.Coinbase(), common.HexToAddress(systemcontracts.ValidatorContract), data, common.Big0)
	nonce := stateDB.GetNonce(msg.From)
	expectedTx := types.NewTransaction(nonce, *msg.To, msg.Value, msg.GasLimit, msg.GasPrice, msg.Data)

	receivedTxs := []*types.Transaction{expectedTx}
	txs := make([]*types.Transaction, 0, 1)
	receipts := make([]*types.Receipt, 0, 1)
	usedGas := uint64(0)

	recording := &recordingTracer{}
	hooks := recording.hooks()

	cx := chainContext{ChainHeaderReader: chain, parlia: engine}
	applyErr := engine.applyTransaction(msg, state.NewHookedState(stateDB, hooks), bs[0].Header(), cx, &txs, &receipts, &receivedTxs, &usedGas, systemTxImporting, hooks)
	if applyErr != nil {
		t.Fatalf("failed to apply system contract transaction: %v", applyErr)
	}

	expectedRecords := []string{
		"system tx start",
		"tx [0xe9a5597c7f5a6a10a18959d262319fbf19cecb4d9d1ce8f2c990089bd88016fc] from [0x0000000000000000000000000000000000000000] start",
		"call enter [0x0000000000000000000000000000000000000000] -> [0x0000000000000000000000000000000000001000] (type 241, gas 9223372036854775807, value 0)",
		"call exit (depth 0, gas used 0, reverted false, err: <none>)",
		"nonce change [0x0000000000000000000000000000000000000000]: 0 -> 1",
		"tx [0xe9a5597c7f5a6a10a18959d262319fbf19cecb4d9d1ce8f2c990089bd88016fc] end (log count 0, cumulative gas used 0, err: <none>)",
		"system tx end",
	}

	if !slices.Equal(recording.records, expectedRecords) {
		t.Errorf("expected \n%s\n\ngot\n\n%s", formatRecords(expectedRecords), formatRecords(recording.records))
	}
}

func TestParlia_applyTransactionModes(t *testing.T) {
	frdir := t.TempDir()
	db, err := rawdb.NewDatabaseWithFreezer(rawdb.NewMemoryDatabase(), frdir, "", false)
	if err != nil {
		t.Fatalf("failed to create database with ancient backend: %v", err)
	}

	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	config := params.ParliaTestChainConfig
	gspec := &core.Genesis{
		Config: config,
		Alloc:  types.GenesisAlloc{testAddr: {Balance: new(big.Int).SetUint64(10 * params.Ether)}},
	}
	mockEngine := &mockParlia{}
	genesisBlock := gspec.MustCommit(db, trieDB)
	chain, _ := core.NewBlockChain(db, gspec, mockEngine, nil)
	defer chain.Stop()
	parents, _ := core.GenerateChain(config, genesisBlock, mockEngine, db, 1, nil)
	header := parents[0].Header()

	engine := New(config, db, nil, genesisBlock.Hash())
	validatorKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate validator key: %v", err)
	}
	validator := crypto.PubkeyToAddress(validatorKey.PublicKey)
	engine.Authorize(validator, nil, func(account accounts.Account, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
		if account.Address != validator {
			return nil, fmt.Errorf("unexpected signing account %s", account.Address)
		}
		return types.SignTx(tx, types.LatestSigner(config), validatorKey)
	})

	data, err := engine.validatorSetABI.Pack("distributeFinalityReward", make([]common.Address, 0), make([]*big.Int, 0))
	if err != nil {
		t.Fatalf("failed to pack system contract method: %v", err)
	}
	msg := engine.getSystemMessage(validator, common.HexToAddress(systemcontracts.ValidatorContract), data, common.Big0)
	cx := chainContext{ChainHeaderReader: chain, parlia: engine}

	newState := func(t *testing.T) *state.StateDB {
		t.Helper()
		stateDB, err := state.New(genesisBlock.Root(), state.NewDatabase(trieDB, nil))
		if err != nil {
			t.Fatalf("failed to create stateDB: %v", err)
		}
		return stateDB
	}
	expectedTx := func(stateDB *state.StateDB) *types.Transaction {
		nonce := stateDB.GetNonce(msg.From)
		return types.NewTransaction(nonce, *msg.To, msg.Value, msg.GasLimit, msg.GasPrice, msg.Data)
	}
	apply := func(t *testing.T, stateDB *state.StateDB, receivedTxs *[]*types.Transaction, mode systemTxMode) ([]*types.Transaction, error) {
		t.Helper()
		txs := make([]*types.Transaction, 0, 1)
		receipts := make([]*types.Receipt, 0, 1)
		usedGas := uint64(0)
		err := engine.applyTransaction(msg, stateDB, header, cx, &txs, &receipts, receivedTxs, &usedGas, mode, nil)
		return txs, err
	}

	t.Run("mining signs system tx from validator", func(t *testing.T) {
		txs, err := apply(t, newState(t), nil, systemTxMining)
		if err != nil {
			t.Fatalf("applyTransaction mining failed: %v", err)
		}
		if len(txs) != 1 {
			t.Fatalf("expected one tx, got %d", len(txs))
		}
		if isUnsignedTx(txs[0]) {
			t.Fatalf("mining mode must sign system tx")
		}
	})

	t.Run("mining rejects non-validator sender", func(t *testing.T) {
		original := msg.From
		msg.From = common.HexToAddress("0x3000000000000000000000000000000000000003")
		defer func() { msg.From = original }()

		_, err := apply(t, newState(t), nil, systemTxMining)
		if err == nil || !strings.Contains(err.Error(), "cannot sign system tx") {
			t.Fatalf("expected cannot sign error, got %v", err)
		}
	})

	t.Run("packing keeps system tx unsigned", func(t *testing.T) {
		txs, err := apply(t, newState(t), nil, systemTxPacking)
		if err != nil {
			t.Fatalf("applyTransaction packing failed: %v", err)
		}
		if len(txs) != 1 {
			t.Fatalf("expected one tx, got %d", len(txs))
		}
		if !isUnsignedTx(txs[0]) {
			t.Fatalf("packing mode must keep system tx unsigned")
		}
	})

	t.Run("importing consumes matching received tx", func(t *testing.T) {
		stateDB := newState(t)
		receivedTxs := []*types.Transaction{expectedTx(stateDB)}
		txs, err := apply(t, stateDB, &receivedTxs, systemTxImporting)
		if err != nil {
			t.Fatalf("applyTransaction importing failed: %v", err)
		}
		if len(receivedTxs) != 0 {
			t.Fatalf("expected received tx to be consumed, got %d left", len(receivedTxs))
		}
		if len(txs) != 1 || txs[0] == nil {
			t.Fatalf("expected imported tx to be appended")
		}
	})

	t.Run("importing rejects missing received tx", func(t *testing.T) {
		receivedTxs := []*types.Transaction{}
		_, err := apply(t, newState(t), &receivedTxs, systemTxImporting)
		if err == nil || !strings.Contains(err.Error(), "supposed to get a actual transaction") {
			t.Fatalf("expected missing received tx error, got %v", err)
		}
	})

	t.Run("importing rejects mismatched received tx", func(t *testing.T) {
		stateDB := newState(t)
		expected := expectedTx(stateDB)
		wrongTx := types.NewTransaction(expected.Nonce()+1, *expected.To(), expected.Value(), expected.Gas(), expected.GasPrice(), expected.Data())
		receivedTxs := []*types.Transaction{wrongTx}
		_, err := apply(t, stateDB, &receivedTxs, systemTxImporting)
		if err == nil || !strings.Contains(err.Error(), "expected tx hash") {
			t.Fatalf("expected tx hash mismatch error, got %v", err)
		}
	})
}

func isUnsignedTx(tx *types.Transaction) bool {
	v, r, s := tx.RawSignatureValues()
	return v.Sign() == 0 && r.Sign() == 0 && s.Sign() == 0
}

// TestParliaFinalizeAndAssembleBidBlock verifies BidBlock assembly emits unsigned system txs.
func TestParliaFinalizeAndAssembleBidBlock(t *testing.T) {
	frdir := t.TempDir()
	db, err := rawdb.NewDatabaseWithFreezer(rawdb.NewMemoryDatabase(), frdir, "", false)
	if err != nil {
		t.Fatalf("failed to create database with ancient backend: %v", err)
	}

	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	config := params.ParliaTestChainConfig
	gspec := &core.Genesis{
		Config: config,
		Alloc:  types.GenesisAlloc{testAddr: {Balance: new(big.Int).SetUint64(10 * params.Ether)}},
	}
	mockEngine := &mockParlia{}
	genesisBlock := gspec.MustCommit(db, trieDB)
	chain, _ := core.NewBlockChain(db, gspec, mockEngine, nil)
	defer chain.Stop()
	parents, _ := core.GenerateChain(config, genesisBlock, mockEngine, db, 1, nil)
	parent := parents[0]
	rawdb.WriteBlock(db, parent)

	engine := New(config, db, nil, genesisBlock.Hash())
	validatorKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	validator := crypto.PubkeyToAddress(validatorKey.PublicKey)
	engine.Authorize(validator, nil, func(account accounts.Account, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
		if account.Address != validator {
			return nil, fmt.Errorf("unexpected signing account %s", account.Address)
		}
		return types.SignTx(tx, types.LatestSigner(config), validatorKey)
	})

	gasFee := uint256.NewInt(12345)
	newHeader := func() *types.Header {
		return &types.Header{
			ParentHash: parent.Hash(),
			Number:     new(big.Int).Add(parent.Number(), common.Big1),
			Coinbase:   validator,
			Difficulty: new(big.Int).Set(diffInTurn),
			GasLimit:   params.SystemTxsGasHardLimit,
			Time:       parent.Time() + 1,
		}
	}
	newState := func() *state.StateDB {
		stateDB, err := state.New(parent.Root(), state.NewDatabase(trieDB, nil))
		if err != nil {
			t.Fatalf("failed to create stateDB: %v", err)
		}
		stateDB.SetBalance(consensus.SystemAddress, new(uint256.Int).Set(gasFee), tracing.BalanceChangeUnspecified)
		return stateDB
	}

	signedBlock, signedReceipts, err := engine.FinalizeAndAssemble(chain, newHeader(), newState(), &types.Body{}, nil, nil)
	if err != nil {
		t.Fatalf("failed to finalize signed block: %v", err)
	}
	unsignedBlock, unsignedReceipts, err := engine.FinalizeAndAssembleBidBlock(chain, newHeader(), newState(), &types.Body{}, nil, nil)
	if err != nil {
		t.Fatalf("failed to finalize BidBlock: %v", err)
	}

	if signedBlock.Root() != unsignedBlock.Root() {
		t.Fatalf("state root mismatch: signed=%s unsigned=%s", signedBlock.Root(), unsignedBlock.Root())
	}
	if signedBlock.ReceiptHash() != unsignedBlock.ReceiptHash() {
		t.Fatalf("receipt hash mismatch: signed=%s unsigned=%s", signedBlock.ReceiptHash(), unsignedBlock.ReceiptHash())
	}
	if signedBlock.Bloom() != unsignedBlock.Bloom() {
		t.Fatalf("receipt bloom mismatch")
	}
	if signedBlock.GasUsed() != unsignedBlock.GasUsed() {
		t.Fatalf("gas used mismatch: signed=%d unsigned=%d", signedBlock.GasUsed(), unsignedBlock.GasUsed())
	}
	if len(signedBlock.Transactions()) == 0 || len(unsignedBlock.Transactions()) == 0 {
		t.Fatalf("expected system transactions in both finalized blocks")
	}
	if isUnsignedTx(signedBlock.Transactions()[0]) {
		t.Fatalf("expected default finalize path to sign system txs")
	}
	if !isUnsignedTx(unsignedBlock.Transactions()[0]) {
		t.Fatalf("expected BidBlock assembly to keep system txs unsigned")
	}
	if len(signedReceipts) != len(unsignedReceipts) {
		t.Fatalf("receipt count mismatch: signed=%d unsigned=%d", len(signedReceipts), len(unsignedReceipts))
	}
}

func TestParliaFinalizeAndAssembleBidBlockRewardsHeaderCoinbase(t *testing.T) {
	frdir := t.TempDir()
	db, err := rawdb.NewDatabaseWithFreezer(rawdb.NewMemoryDatabase(), frdir, "", false)
	if err != nil {
		t.Fatalf("failed to create database with ancient backend: %v", err)
	}

	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	config := params.ParliaTestChainConfig
	gspec := &core.Genesis{
		Config: config,
		Alloc:  types.GenesisAlloc{testAddr: {Balance: new(big.Int).SetUint64(10 * params.Ether)}},
	}
	mockEngine := &mockParlia{}
	genesisBlock := gspec.MustCommit(db, trieDB)
	chain, _ := core.NewBlockChain(db, gspec, mockEngine, nil)
	defer chain.Stop()
	parents, _ := core.GenerateChain(config, genesisBlock, mockEngine, db, 1, nil)
	parent := parents[0]
	rawdb.WriteBlock(db, parent)

	engine := New(config, db, nil, genesisBlock.Hash())
	localValidator := common.HexToAddress("0x1000000000000000000000000000000000000001")
	blockCoinbase := common.HexToAddress("0x2000000000000000000000000000000000000002")
	engine.Authorize(localValidator, nil, nil)

	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number(), common.Big1),
		Coinbase:   blockCoinbase,
		Difficulty: new(big.Int).Set(diffInTurn),
		GasLimit:   params.SystemTxsGasHardLimit,
		Time:       parent.Time() + 1,
	}
	stateDB, err := state.New(parent.Root(), state.NewDatabase(trieDB, nil))
	if err != nil {
		t.Fatalf("failed to create stateDB: %v", err)
	}
	stateDB.SetBalance(consensus.SystemAddress, uint256.NewInt(12345), tracing.BalanceChangeUnspecified)

	block, _, err := engine.FinalizeAndAssembleBidBlock(chain, header, stateDB, &types.Body{}, nil, nil)
	if err != nil {
		t.Fatalf("failed to finalize BidBlock: %v", err)
	}

	wantDeposit, err := engine.validatorSetABI.Pack("deposit", blockCoinbase)
	if err != nil {
		t.Fatalf("failed to pack expected deposit: %v", err)
	}
	wrongDeposit, err := engine.validatorSetABI.Pack("deposit", localValidator)
	if err != nil {
		t.Fatalf("failed to pack wrong deposit: %v", err)
	}
	var found bool
	for _, tx := range block.Transactions() {
		if tx.To() == nil || *tx.To() != common.HexToAddress(systemcontracts.ValidatorContract) {
			continue
		}
		if bytes.Equal(tx.Data(), wrongDeposit) {
			t.Fatalf("deposit reward routed to local p.val %s, want header coinbase %s", localValidator, blockCoinbase)
		}
		if bytes.Equal(tx.Data(), wantDeposit) {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing deposit reward for header coinbase %s", blockCoinbase)
	}
}

func TestParliaPrepareForBidBlock(t *testing.T) {
	frdir := t.TempDir()
	db, err := rawdb.NewDatabaseWithFreezer(rawdb.NewMemoryDatabase(), frdir, "", false)
	if err != nil {
		t.Fatalf("failed to create database with ancient backend: %v", err)
	}

	trieDB := triedb.NewDatabase(db, nil)
	defer trieDB.Close()

	config := params.ParliaTestChainConfig
	otherValidator := common.HexToAddress("0x2000000000000000000000000000000000000000")
	extra := make([]byte, extraVanity+validatorNumberSize+2*validatorBytesLength+extraSeal)
	extra[extraVanity] = 2
	copy(extra[extraVanity+validatorNumberSize:], testAddr[:])
	copy(extra[extraVanity+validatorNumberSize+validatorBytesLength:], otherValidator[:])
	gspec := &core.Genesis{
		Config:    config,
		ExtraData: extra,
		Timestamp: uint64(time.Now().Add(-10 * time.Second).Unix()),
		Alloc:     types.GenesisAlloc{testAddr: {Balance: new(big.Int).SetUint64(10 * params.Ether)}},
	}
	mockEngine := &mockParlia{}
	genesisBlock := gspec.MustCommit(db, trieDB)
	chain, _ := core.NewBlockChain(db, gspec, mockEngine, nil)
	defer chain.Stop()

	validatorEngine := New(config, db, nil, genesisBlock.Hash())
	inturnValidator, err := validatorEngine.NextInTurnValidator(chain, genesisBlock.Header())
	if err != nil {
		t.Fatalf("failed to get in-turn validator: %v", err)
	}
	validatorEngine.Authorize(inturnValidator, nil, nil)
	builderEngine := New(config, db, nil, genesisBlock.Hash())
	if inturnValidator == testAddr {
		builderEngine.Authorize(otherValidator, nil, nil)
	} else {
		builderEngine.Authorize(testAddr, nil, nil)
	}

	newHeader := func() *types.Header {
		return &types.Header{
			ParentHash: genesisBlock.Hash(),
			Number:     common.Big1,
			GasLimit:   params.SystemTxsGasHardLimit,
		}
	}
	validatorHeader := newHeader()
	builderHeader := newHeader()

	if err := validatorEngine.Prepare(chain, validatorHeader); err != nil {
		t.Fatalf("failed to prepare validator header: %v", err)
	}
	if err := builderEngine.PrepareForBidBlock(chain, builderHeader); err != nil {
		t.Fatalf("failed to prepare BidBlock header: %v", err)
	}

	if builderHeader.Coinbase != validatorHeader.Coinbase {
		t.Fatalf("coinbase mismatch: builder=%s validator=%s", builderHeader.Coinbase, validatorHeader.Coinbase)
	}
	if builderHeader.Coinbase != inturnValidator {
		t.Fatalf("builder coinbase mismatch: have %s want %s", builderHeader.Coinbase, inturnValidator)
	}
	// Prepare and PrepareForBidBlock share the prepare() core, so headers
	// prepared back-to-back land on the same blockTimeForRamanujanFork output.
	// Allow one 50ms quantum of tolerance for the rare case where the two
	// calls straddle a wall-clock alignment tick.
	diff := int64(builderHeader.MilliTimestamp()) - int64(validatorHeader.MilliTimestamp())
	if diff < 0 {
		diff = -diff
	}
	if diff > 50 {
		t.Fatalf("builder/validator time diverge by %dms: builder=%d validator=%d",
			diff, builderHeader.MilliTimestamp(), validatorHeader.MilliTimestamp())
	}
	if builderHeader.Difficulty.Cmp(validatorHeader.Difficulty) != 0 {
		t.Fatalf("difficulty mismatch: builder=%s validator=%s", builderHeader.Difficulty, validatorHeader.Difficulty)
	}
	// BidBlock prepare must produce a byte-identical Extra to the normal validator
	// prepare (length-equal is too weak: a wrong forkhash/vanity byte would slip through).
	if !bytes.Equal(builderHeader.Extra, validatorHeader.Extra) {
		t.Fatalf("extra mismatch:\n builder   =%x\n validator =%x", builderHeader.Extra, validatorHeader.Extra)
	}

	// SetExtraData branch checks on a non-epoch header (no validator-set contract call):
	// forkhash bytes, zeroed seal space, and vanity truncation/padding. The epoch
	// validators / turnLength branches require a populated validator contract and are
	// covered by e2e.
	const vanityLen = extraVanity - nextForkHashSize // 28
	genesisTime := chain.GenesisHeader().Time
	wantForkHash := forkid.NextForkHash(config, genesisBlock.Hash(), genesisTime, validatorHeader.Number.Uint64(), validatorHeader.Time)
	if got := validatorHeader.Extra[vanityLen:extraVanity]; !bytes.Equal(got, wantForkHash[:]) {
		t.Fatalf("forkhash mismatch: got %x want %x", got, wantForkHash[:])
	}
	if seal := validatorHeader.Extra[len(validatorHeader.Extra)-extraSeal:]; !bytes.Equal(seal, make([]byte, extraSeal)) {
		t.Fatalf("seal space not zeroed: %x", seal)
	}

	setExtra := func(vanity []byte) []byte {
		h := newHeader()
		h.Time = validatorHeader.Time
		h.Extra = append([]byte(nil), vanity...)
		if err := validatorEngine.SetExtraData(chain, h); err != nil {
			t.Fatalf("SetExtraData: %v", err)
		}
		return h.Extra
	}
	// over-long vanity is truncated to vanityLen bytes.
	long := bytes.Repeat([]byte{0xAB}, extraVanity*2)
	if got := setExtra(long)[:vanityLen]; !bytes.Equal(got, long[:vanityLen]) {
		t.Fatalf("vanity not truncated: got %x", got)
	}
	// short vanity is zero-padded to vanityLen bytes.
	short := []byte{0x01, 0x02}
	ext := setExtra(short)
	if !bytes.Equal(ext[:len(short)], short) || !bytes.Equal(ext[len(short):vanityLen], make([]byte, vanityLen-len(short))) {
		t.Fatalf("vanity not zero-padded: got %x", ext[:vanityLen])
	}
}

func formatRecords(records []string) string {
	indented := make([]string, 0, len(records))
	for _, record := range records {
		indented = append(indented, fmt.Sprintf("  %q,", record))
	}

	return "[\n" + strings.Join(indented, "\n") + "\n]"
}

type errorView struct {
	err error
}

func (e errorView) String() string {
	if e.err == nil {
		return "<none>"
	}

	return e.err.Error()
}

type recordingTracer struct {
	records []string
}

func (t *recordingTracer) record(format string, args ...any) {
	t.records = append(t.records, fmt.Sprintf(format, args...))
}

func (t *recordingTracer) hooks() *tracing.Hooks {
	return &tracing.Hooks{
		OnSystemTxStart: func() { t.record("system tx start") },
		OnTxStart: func(vm *tracing.VMContext, tx *types.Transaction, from common.Address) {
			t.record("tx [%s] from [%s] start", tx.Hash(), from)
		},
		OnTxEnd: func(receipt *types.Receipt, err error) {
			t.record("tx [%s] end (log count %d, cumulative gas used %d, err: %s)", receipt.TxHash, len(receipt.Logs), receipt.CumulativeGasUsed, errorView{err})
		},
		OnSystemTxEnd: func() { t.record("system tx end") },
		OnEnter: func(depth int, typ byte, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
			t.record("call enter [%s] -> [%s] (type %d, gas %d, value %s)", from, to, typ, gas, value)
		},
		OnExit: func(depth int, output []byte, gasUsed uint64, err error, reverted bool) {
			t.record("call exit (depth %d, gas used %d, reverted %v, err: %s)", depth, gasUsed, reverted, errorView{err})
		},
		OnNonceChange: func(addr common.Address, prev, new uint64) {
			t.record("nonce change [%s]: %d -> %d", addr, prev, new)
		},
	}
}

var (
	emptyBlob          = kzg4844.Blob{}
	emptyBlobCommit, _ = kzg4844.BlobToCommitment(&emptyBlob)
	emptyBlobProof, _  = kzg4844.ComputeBlobProof(&emptyBlob, emptyBlobCommit)
)

func makeMockTx(config *params.ChainConfig, signer types.Signer, key *ecdsa.PrivateKey, nonce uint64, baseFee uint64, blobBaseFee uint64, isBlobTx bool) (*types.Transaction, *types.BlobTxSidecar) {
	if !isBlobTx {
		raw := &types.DynamicFeeTx{
			ChainID:   config.ChainID,
			Nonce:     nonce,
			GasTipCap: big.NewInt(10),
			GasFeeCap: new(big.Int).SetUint64(baseFee + 10),
			Gas:       params.TxGas,
			To:        &common.Address{0x00},
			Value:     big.NewInt(0),
		}
		tx, _ := types.SignTx(types.NewTx(raw), signer, key)
		return tx, nil
	}
	sidecar := &types.BlobTxSidecar{
		Blobs:       []kzg4844.Blob{emptyBlob, emptyBlob},
		Commitments: []kzg4844.Commitment{emptyBlobCommit, emptyBlobCommit},
		Proofs:      []kzg4844.Proof{emptyBlobProof, emptyBlobProof},
	}
	raw := &types.BlobTx{
		ChainID:    uint256.MustFromBig(config.ChainID),
		Nonce:      nonce,
		GasTipCap:  uint256.NewInt(10),
		GasFeeCap:  uint256.NewInt(baseFee + 10),
		Gas:        params.TxGas,
		To:         common.Address{0x00},
		Value:      uint256.NewInt(0),
		BlobFeeCap: uint256.NewInt(blobBaseFee),
		BlobHashes: sidecar.BlobHashes(),
	}
	tx, _ := types.SignTx(types.NewTx(raw), signer, key)
	return tx, sidecar
}

type mockParlia struct {
	consensus.Engine
}

func (c *mockParlia) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

func (c *mockParlia) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	return nil
}

func (c *mockParlia) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header) error {
	return nil
}

func (c *mockParlia) VerifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header) (chan<- struct{}, <-chan error) {
	abort := make(chan<- struct{})
	results := make(chan error, len(headers))
	for i := 0; i < len(headers); i++ {
		results <- nil
	}
	return abort, results
}

func (c *mockParlia) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state vm.StateDB, _ *[]*types.Transaction, uncles []*types.Header, withdrawals []*types.Withdrawal,
	_ *[]*types.Receipt, _ *[]*types.Transaction, _ *uint64, tracer *tracing.Hooks) (err error) {
	return
}

func (c *mockParlia) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, body *types.Body, receipts []*types.Receipt, tracer *tracing.Hooks) (*types.Block, []*types.Receipt, error) {
	// Finalize block
	c.Finalize(chain, header, state, &body.Transactions, body.Uncles, body.Withdrawals, nil, nil, nil, tracer)

	// Assign the final state root to header.
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))

	// Header seems complete, assemble into a block and return
	return types.NewBlock(header, body, receipts, trie.NewStackTrie(nil)), receipts, nil
}

func (c *mockParlia) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	return big.NewInt(1)
}
