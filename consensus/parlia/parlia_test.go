package parlia

import (
	"crypto/ecdsa"
	"crypto/rand"
	"fmt"
	"math/big"
	mrand "math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	cmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/internal/vmtest"
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
	for _, vmCfg := range vmtest.Configs() {
		t.Run(vmtest.Name(vmCfg), func(t *testing.T) {
			testImpactOfValidatorOutOfService(t, vmCfg)
		})
	}
}

func testImpactOfValidatorOutOfService(t *testing.T, vmCfg vm.Config) {
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
		/*
			for _, val := range c.validators {
				t.Logf("[Testcase %d] validator(%d) head block: %d",
					index, val.index, val.head.blockNumber)
				t.Logf("[Testcase %d] validator(%d) highest justified block: %d",
					index, val.index, val.head.GetJustifiedNumber())
				t.Logf("[Testcase %d] validator(%d) highest finalized block: %d",
					index, val.index, val.head.GetFinalizedBlock().blockNumber)
			}
		*/
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
	for _, vmCfg := range vmtest.Configs() {
		t.Run(vmtest.Name(vmCfg), func(t *testing.T) {
			testParlia_applyTransactionTracing(t, vmCfg)
		})
	}
}

func testParlia_applyTransactionTracing(t *testing.T, vmCfg vm.Config) {
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

	chain, _ := core.NewBlockChain(db, gspec, mockEngine, core.DefaultConfig().WithVMConfig(vmCfg))
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

	cx := chainContext{Chain: chain, parlia: engine}
	applyErr := engine.applyTransaction(msg, state.NewHookedState(stateDB, hooks), bs[0].Header(), cx, &txs, &receipts, &receivedTxs, &usedGas, false, hooks)
	if applyErr != nil {
		t.Fatalf("failed to apply system contract transaction: %v", applyErr)
	}

	expectedRecords := []string{
		"system tx start",
		"tx [0xe9a5597c7f5a6a10a18959d262319fbf19cecb4d9d1ce8f2c990089bd88016fc] from [0x0000000000000000000000000000000000000000] start",
		"nonce change [0x0000000000000000000000000000000000000000]: 0 -> 1",
		"call enter [0x0000000000000000000000000000000000000000] -> [0x0000000000000000000000000000000000001000] (type 241, gas 9223372036854775807, value 0)",
		"call exit (depth 0, gas used 0, reverted false, err: <none>)",
		"tx [0xe9a5597c7f5a6a10a18959d262319fbf19cecb4d9d1ce8f2c990089bd88016fc] end (log count 0, cumulative gas used 0, err: <none>)",
		"system tx end",
	}

	if !slices.Equal(recording.records, expectedRecords) {
		t.Errorf("expected \n%s\n\ngot\n\n%s", formatRecords(recording.records), formatRecords(expectedRecords))
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

func TestSignBAL(t *testing.T) {
	// Setup test environment
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)

	// Create mock signing function that succeeds
	mockSignFn := func(account accounts.Account, mimeType string, data []byte) ([]byte, error) {
		if account.Address != addr {
			return nil, fmt.Errorf("wrong address")
		}
		if mimeType != accounts.MimetypeParlia {
			return nil, fmt.Errorf("wrong mime type")
		}
		// Return a dummy 65-byte signature
		sig := make([]byte, 65)
		copy(sig, []byte("test_signature_data_for_testing_purposes_123456789012345678901234"))
		return sig, nil
	}

	// Create Parlia instance
	parlia := &Parlia{
		val:    addr,
		signFn: mockSignFn,
	}

	tests := []struct {
		name          string
		bal           *types.BlockAccessListEncode
		expectedError bool
		signFn        SignerFn
		description   string
	}{
		{
			name: "successful signing",
			bal: &types.BlockAccessListEncode{
				Version:  0,
				SignData: make([]byte, 65),
				Accounts: []types.AccountAccessListEncode{
					{
						Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
						StorageItems: []types.StorageAccessItem{
							{Key: common.HexToHash("0x01"), TxIndex: 0, Dirty: false},
						},
					},
				},
			},
			expectedError: false,
			signFn:        mockSignFn,
			description:   "Should successfully sign a valid BlockAccessListEncode",
		},
		{
			name: "signing function error",
			bal: &types.BlockAccessListEncode{
				Version:  0,
				SignData: make([]byte, 65),
				Accounts: []types.AccountAccessListEncode{},
			},
			expectedError: true,
			signFn: func(account accounts.Account, mimeType string, data []byte) ([]byte, error) {
				return nil, fmt.Errorf("signing failed")
			},
			description: "Should return error when signing function fails",
		},
		{
			name: "empty accounts list",
			bal: &types.BlockAccessListEncode{
				Version:  0,
				SignData: make([]byte, 65),
				Accounts: []types.AccountAccessListEncode{},
			},
			expectedError: false,
			signFn:        mockSignFn,
			description:   "Should successfully sign even with empty accounts list",
		},
		{
			name: "multiple accounts",
			bal: &types.BlockAccessListEncode{
				Version:  2,
				SignData: make([]byte, 65),
				Accounts: []types.AccountAccessListEncode{
					{
						Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
						StorageItems: []types.StorageAccessItem{
							{Key: common.HexToHash("0x01"), TxIndex: 0, Dirty: false},
							{Key: common.HexToHash("0x02"), TxIndex: 1, Dirty: true},
						},
					},
					{
						Address: common.HexToAddress("0x2222222222222222222222222222222222222222"),
						StorageItems: []types.StorageAccessItem{
							{Key: common.HexToHash("0x03"), TxIndex: 2, Dirty: false},
						},
					},
				},
			},
			expectedError: false,
			signFn:        mockSignFn,
			description:   "Should successfully sign with multiple accounts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up Parlia with the test signing function
			parlia.signFn = tt.signFn

			// Call SignBAL
			err := parlia.SignBAL(tt.bal)

			// Check results
			if tt.expectedError {
				if err == nil {
					t.Errorf("Expected error but got none. %s", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v. %s", err, tt.description)
				}
				// Verify signature was copied to SignData
				if tt.bal != nil && len(tt.bal.SignData) != 65 {
					t.Errorf("Expected SignData to be 65 bytes, got %d", len(tt.bal.SignData))
				}
				// Verify signature content (for successful cases)
				if tt.bal != nil && !tt.expectedError {
					expectedSig := "test_signature_data_for_testing_purposes_123456789012345678901234"
					if string(tt.bal.SignData[:len(expectedSig)]) != expectedSig {
						t.Errorf("SignData was not properly set")
					}
				}
			}
		})
	}
}

func TestVerifyBAL(t *testing.T) {
	// Setup test environment
	signerKey, _ := crypto.GenerateKey()
	signerAddr := crypto.PubkeyToAddress(signerKey.PublicKey)

	// Helper function to create a properly signed BAL
	createBlockWithBAL := func(addr common.Address, version uint32, signLength int, accounts []types.AccountAccessListEncode) *types.Block {
		header := &types.Header{
			ParentHash: types.EmptyRootHash,
			Number:     big.NewInt(10),
			Coinbase:   addr,
		}
		block := types.NewBlock(header, nil, nil, nil)
		bal := &types.BlockAccessListEncode{
			Version:  version,
			Number:   block.Number().Uint64(),
			Hash:     block.Hash(),
			SignData: make([]byte, signLength),
			Accounts: accounts,
		}

		// RLP encode the data
		data, _ := rlp.EncodeToBytes([]interface{}{bal.Version, bal.Number, bal.Hash, bal.Accounts})

		// Create signature using the test key
		hash := crypto.Keccak256(data)
		sig, _ := crypto.Sign(hash, signerKey)
		copy(bal.SignData, sig)
		block = block.WithBAL(bal)
		return block
	}

	// Create a Parlia instance
	parlia := &Parlia{}

	tests := []struct {
		name          string
		block         *types.Block
		expectedError bool
		description   string
	}{
		{
			name: "valid signature verification",
			block: createBlockWithBAL(signerAddr, 0, 65, []types.AccountAccessListEncode{
				{
					Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
					StorageItems: []types.StorageAccessItem{
						{Key: common.HexToHash("0x01"), TxIndex: 0, Dirty: false},
					},
				},
			}),
			expectedError: false,
			description:   "Should successfully verify a properly signed BAL",
		},
		{
			name: "invalid version",
			block: createBlockWithBAL(signerAddr, 1, 65, []types.AccountAccessListEncode{
				{
					Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
					StorageItems: []types.StorageAccessItem{
						{Key: common.HexToHash("0x01"), TxIndex: 0, Dirty: false},
					},
				},
			}),
			expectedError: true,
			description:   "Should fail when version is invalid",
		},
		{
			name:          "invalid signature length - too short",
			block:         createBlockWithBAL(signerAddr, 0, 64, []types.AccountAccessListEncode{}),
			expectedError: true,
			description:   "Should fail when signature is too short",
		},
		{
			name:          "invalid signature length - too long",
			block:         createBlockWithBAL(signerAddr, 0, 66, []types.AccountAccessListEncode{}),
			expectedError: true,
			description:   "Should fail when signature is too long",
		},
		{
			name:          "empty signature",
			block:         createBlockWithBAL(signerAddr, 0, 0, []types.AccountAccessListEncode{}),
			expectedError: true,
			description:   "Should fail with empty signature",
		},
		{
			name: "signer mismatch",
			block: createBlockWithBAL(common.HexToAddress("0x1234567890123456789012345678901234567890"), 0, 65, []types.AccountAccessListEncode{
				{
					Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
					StorageItems: []types.StorageAccessItem{
						{Key: common.HexToHash("0x01"), TxIndex: 0, Dirty: false},
					},
				},
			}),
			expectedError: true,
			description:   "Should fail when signer address doesn't match recovered address",
		},
		{
			name:          "empty accounts list",
			block:         createBlockWithBAL(signerAddr, 0, 65, []types.AccountAccessListEncode{}),
			expectedError: false,
			description:   "Should successfully verify BAL with empty accounts",
		},
		{
			name: "multiple accounts",
			block: createBlockWithBAL(signerAddr, 0, 65, []types.AccountAccessListEncode{
				{
					Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
					StorageItems: []types.StorageAccessItem{
						{Key: common.HexToHash("0x01"), TxIndex: 0, Dirty: false},
						{Key: common.HexToHash("0x02"), TxIndex: 1, Dirty: true},
					},
				},
				{
					Address: common.HexToAddress("0x2222222222222222222222222222222222222222"),
					StorageItems: []types.StorageAccessItem{
						{Key: common.HexToHash("0x03"), TxIndex: 2, Dirty: false},
					},
				},
			}),
			expectedError: false,
			description:   "Should successfully verify BAL with multiple accounts",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parlia.VerifyBAL(tt.block, tt.block.BAL())
			if tt.expectedError {
				if err == nil {
					t.Errorf("Expected error but got none. %s", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v. %s", err, tt.description)
				}
			}
		})
	}
}

func TestVerifyBAL_EdgeCases(t *testing.T) {
	// Test with different key to ensure proper signature verification
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()
	addr1 := crypto.PubkeyToAddress(key1.PublicKey)
	addr2 := crypto.PubkeyToAddress(key2.PublicKey)

	parlia := &Parlia{}

	header1 := &types.Header{
		ParentHash: types.EmptyRootHash,
		Number:     big.NewInt(10),
		Coinbase:   addr1,
	}
	block1 := types.NewBlock(header1, nil, nil, nil)
	// Create BAL signed with key1
	bal := &types.BlockAccessListEncode{
		Version:  0,
		Number:   block1.Number().Uint64(),
		Hash:     block1.Hash(),
		SignData: make([]byte, 65),
		Accounts: []types.AccountAccessListEncode{
			{
				Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
				StorageItems: []types.StorageAccessItem{
					{Key: common.HexToHash("0x01"), TxIndex: 0, Dirty: false},
				},
			},
		},
	}

	// Sign with key1
	data, _ := rlp.EncodeToBytes([]interface{}{bal.Version, bal.Number, bal.Hash, bal.Accounts})
	hash := crypto.Keccak256(data)
	sig, _ := crypto.Sign(hash, key1)
	copy(bal.SignData, sig)

	// Should succeed with addr1
	err := parlia.VerifyBAL(block1, bal)
	if err != nil {
		t.Errorf("Verification with correct signer failed: %v", err)
	}

	// Should fail with addr2 (different key)
	header2 := &types.Header{
		ParentHash: types.EmptyRootHash,
		Number:     big.NewInt(10),
		Coinbase:   addr2,
	}
	block2 := types.NewBlock(header2, nil, nil, nil)
	err = parlia.VerifyBAL(block2, bal)
	if err == nil {
		t.Error("Expected verification to fail with different signer address")
	}
}

func TestVerifyBAL_TooLargeData(t *testing.T) {
	// Test with large amount of data to ensure RLP encoding works correctly
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)
	parlia := &Parlia{}

	// Create BAL with many accounts
	accounts := make([]types.AccountAccessListEncode, 20000)
	for i := 0; i < 20000; i++ {
		accounts[i] = types.AccountAccessListEncode{
			Address: common.BigToAddress(big.NewInt(int64(i))),
			StorageItems: []types.StorageAccessItem{
				{Key: common.BigToHash(big.NewInt(int64(i))), TxIndex: uint32(i), Dirty: i%2 == 0},
				{Key: common.BigToHash(big.NewInt(int64(i + 1000))), TxIndex: uint32(i + 1), Dirty: i%3 == 0},
			},
		}
	}

	header := &types.Header{
		ParentHash: types.EmptyRootHash,
		Number:     big.NewInt(10),
		Coinbase:   addr,
	}
	block := types.NewBlock(header, nil, nil, nil)
	bal := &types.BlockAccessListEncode{
		Version:  0,
		Number:   block.Number().Uint64(),
		Hash:     block.Hash(),
		SignData: make([]byte, 65),
		Accounts: accounts,
	}

	// Sign the large data
	data, err := rlp.EncodeToBytes([]interface{}{bal.Version, bal.Number, bal.Hash, bal.Accounts})
	if err != nil {
		t.Fatalf("Failed to RLP encode large data: %v", err)
	}

	hash := crypto.Keccak256(data)
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatalf("Failed to sign large data: %v", err)
	}
	copy(bal.SignData, sig)

	// Verify the signature
	err = parlia.VerifyBAL(block, bal)
	if err.Error() != "data is too large" {
		t.Errorf("Failed to verify BAL with large data: %v", err)
	}
}

func TestSignBAL_VerifyBAL_Integration(t *testing.T) {
	// Test complete sign-verify cycle
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)

	// Create mock signing function
	mockSignFn := func(account accounts.Account, mimeType string, data []byte) ([]byte, error) {
		if account.Address != addr {
			return nil, fmt.Errorf("wrong address")
		}
		if mimeType != accounts.MimetypeParlia {
			return nil, fmt.Errorf("wrong mime type")
		}
		// Use the actual private key to sign
		hash := crypto.Keccak256(data)
		return crypto.Sign(hash, key)
	}

	parlia := &Parlia{
		val:    addr,
		signFn: mockSignFn,
	}

	testCases := []struct {
		name     string
		version  uint32
		accounts []types.AccountAccessListEncode
	}{
		{
			name:     "empty accounts",
			version:  0,
			accounts: []types.AccountAccessListEncode{},
		},
		{
			name:    "single account",
			version: 0,
			accounts: []types.AccountAccessListEncode{
				{
					Address: common.HexToAddress("0x1234567890123456789012345678901234567890"),
					StorageItems: []types.StorageAccessItem{
						{Key: common.HexToHash("0x01"), TxIndex: 0, Dirty: false},
						{Key: common.HexToHash("0x02"), TxIndex: 1, Dirty: true},
					},
				},
			},
		},
		{
			name:    "multiple accounts",
			version: 0,
			accounts: []types.AccountAccessListEncode{
				{
					Address: common.HexToAddress("0x1111111111111111111111111111111111111111"),
					StorageItems: []types.StorageAccessItem{
						{Key: common.HexToHash("0x01"), TxIndex: 0, Dirty: false},
					},
				},
				{
					Address: common.HexToAddress("0x2222222222222222222222222222222222222222"),
					StorageItems: []types.StorageAccessItem{
						{Key: common.HexToHash("0x02"), TxIndex: 1, Dirty: true},
						{Key: common.HexToHash("0x03"), TxIndex: 2, Dirty: false},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			header := &types.Header{
				ParentHash: types.EmptyRootHash,
				Number:     big.NewInt(10),
				Coinbase:   addr,
			}
			block := types.NewBlock(header, nil, nil, nil)
			// Create BAL
			bal := &types.BlockAccessListEncode{
				Version:  tc.version,
				Number:   block.Number().Uint64(),
				Hash:     block.Hash(),
				SignData: make([]byte, 65),
				Accounts: tc.accounts,
			}

			// Sign the BAL
			err := parlia.SignBAL(bal)
			if err != nil {
				t.Fatalf("SignBAL failed: %v", err)
			}

			// Verify signature length
			if len(bal.SignData) != 65 {
				t.Errorf("Expected SignData to be 65 bytes, got %d", len(bal.SignData))
			}

			// Verify the BAL with correct signer
			err = parlia.VerifyBAL(block, bal)
			if err != nil {
				t.Errorf("VerifyBAL failed with correct signer: %v", err)
			}
		})
	}
}
