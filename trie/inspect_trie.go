package trie

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/olekukonko/tablewriter"
)

type Account struct {
	Nonce    uint64
	Balance  *big.Int
	Root     common.Hash // merkle root of the storage trie
	CodeHash []byte
}

type Inspector struct {
	trie            *Trie // traverse trie
	db              *Database
	stateRootHash   common.Hash
	blocknum        uint64
	root            node               // root of triedb
	num             uint64             // block number
	result          *TotalTrieTreeStat // inspector result
	totalNum        uint64
	concurrentQueue chan struct{}
	wg              sync.WaitGroup
}

type RWMap struct {
	sync.RWMutex
	m map[uint64]*TrieTreeStat
}

// 新建一个RWMap
func NewRWMap() *RWMap {
	return &RWMap{
		m: make(map[uint64]*TrieTreeStat, 1),
	}
}
func (m *RWMap) Get(k uint64) (*TrieTreeStat, bool) { //从map中读取一个值
	m.RLock()
	defer m.RUnlock()
	v, existed := m.m[k] // 在锁的保护下从map中读取
	return v, existed
}

func (m *RWMap) Set(k uint64, v *TrieTreeStat) { // 设置一个键值对
	m.Lock() // 锁保护
	defer m.Unlock()
	m.m[k] = v
}

func (m *RWMap) Delete(k uint64) { //删除一个键
	m.Lock() // 锁保护
	defer m.Unlock()
	delete(m.m, k)
}

func (m *RWMap) Len() int { // map的长度
	m.RLock() // 锁保护
	defer m.RUnlock()
	return len(m.m)
}

func (m *RWMap) Each(f func(k uint64, v *TrieTreeStat) bool) { // 遍历map
	m.RLock() //遍历期间一直持有读锁
	defer m.RUnlock()

	for k, v := range m.m {
		if !f(k, v) {
			return
		}
	}
}

type TotalTrieTreeStat struct {
	theTrieTreeStats RWMap
}

type TrieTreeStat struct {
	is_account_trie    bool
	theNodeStatByLevel [15]NodeStat
	totalNodeStat      NodeStat
}

type NodeStat struct {
	ShortNodeCnt uint64
	FullNodeCnt  uint64
	ValueNodeCnt uint64
}

func (trieStat *TrieTreeStat) AtomicAdd(theNode node, height uint32) {
	switch (theNode).(type) {
	case *shortNode:
		atomic.AddUint64(&trieStat.totalNodeStat.ShortNodeCnt, 1)
		atomic.AddUint64(&(trieStat.theNodeStatByLevel[height].ShortNodeCnt), 1)
	case *fullNode:
		atomic.AddUint64(&trieStat.totalNodeStat.FullNodeCnt, 1)
		atomic.AddUint64(&trieStat.theNodeStatByLevel[height].FullNodeCnt, 1)
	case valueNode:
		atomic.AddUint64(&trieStat.totalNodeStat.ValueNodeCnt, 1)
		atomic.AddUint64(&((trieStat.theNodeStatByLevel[height]).ValueNodeCnt), 1)
	default:
		panic(errors.New("Invalid node type to statistics"))
	}
}

func (trieStat *TrieTreeStat) Display(rootHash uint64, treeType string) {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"TrieType", "Level", "ShortNodeCnt", "FullNodeCnt", "ValueNodeCnt"})
	table.SetAlignment(1)
	for i := 0; i < len(trieStat.theNodeStatByLevel); i++ {
		nodeStat := trieStat.theNodeStatByLevel[i]
		if nodeStat.FullNodeCnt == 0 && nodeStat.ShortNodeCnt == 0 && nodeStat.ValueNodeCnt == 0 {
			break
		}
		table.AppendBulk([][]string{
			{"-", strconv.Itoa(i), nodeStat.ShortNodeCount(), nodeStat.FullNodeCount(), nodeStat.ValueNodeCount()},
		})
	}
	table.AppendBulk([][]string{
		{fmt.Sprintf("%v-%v", treeType, rootHash), "Total", trieStat.totalNodeStat.ShortNodeCount(), trieStat.totalNodeStat.FullNodeCount(), trieStat.totalNodeStat.ValueNodeCount()},
	})
	table.Render()
}

func Uint64ToString(cnt uint64) string {
	return fmt.Sprintf("%v", cnt)
}

func (nodeStat *NodeStat) ShortNodeCount() string {
	return Uint64ToString(nodeStat.ShortNodeCnt)
}

func (nodeStat *NodeStat) FullNodeCount() string {
	return Uint64ToString(nodeStat.FullNodeCnt)
}
func (nodeStat *NodeStat) ValueNodeCount() string {
	return Uint64ToString(nodeStat.ValueNodeCnt)
}

// NewInspector return a inspector obj
func NewInspector(tr *Trie, db *Database, stateRootHash common.Hash, blocknum uint64, jobnum uint64) (*Inspector, error) {
	if tr == nil {
		return nil, errors.New("trie is nil")
	}

	if tr.root == nil {
		return nil, errors.New("trie root is nil")
	}

	ins := &Inspector{
		trie:          tr,
		db:            db,
		stateRootHash: stateRootHash,
		blocknum:      blocknum,
		root:          tr.root,
		result: &TotalTrieTreeStat{
			theTrieTreeStats: *NewRWMap(),
		},
		totalNum:        (uint64)(0),
		concurrentQueue: make(chan struct{}, jobnum),
		wg:              sync.WaitGroup{},
	}

	return ins, nil
}

// Run statistics, external call
func (inspect *Inspector) Run() {
	accountTrieStat := new(TrieTreeStat)
	roothash := inspect.trie.Hash().Big().Uint64()
	path := make([]byte, 0)

	inspect.result.theTrieTreeStats.Set(roothash, accountTrieStat)
	log.Info("Find Account Trie Tree, rootHash: ", inspect.trie.Hash().String(), "BlockNum: ", inspect.blocknum)
	inspect.ConcurrentTraversal(inspect.trie, accountTrieStat, inspect.root, 0, path)
	inspect.wg.Wait()
}

func (inspect *Inspector) SubConcurrentTraversal(theTrie *Trie, theTrieTreeStat *TrieTreeStat, theNode node, height uint32, path []byte) {
	inspect.concurrentQueue <- struct{}{}
	inspect.ConcurrentTraversal(theTrie, theTrieTreeStat, theNode, height, path)
	<-inspect.concurrentQueue
	inspect.wg.Done()
	return
}

func (inspect *Inspector) ConcurrentTraversal(theTrie *Trie, theTrieTreeStat *TrieTreeStat, theNode node, height uint32, path []byte) {
	// print process progress
	total_num := atomic.AddUint64(&inspect.totalNum, 1)
	if total_num%100000 == 0 {
		fmt.Printf("Complete progress: %v, go routines Num: %v, inspect concurrentQueue: %v\n", total_num, runtime.NumGoroutine(), len(inspect.concurrentQueue))
	}

	// nil node
	if theNode == nil {
		return
	}

	switch current := (theNode).(type) {
	case *shortNode:
		path = append(path, current.Key...)
		inspect.ConcurrentTraversal(theTrie, theTrieTreeStat, current.Val, height+1, path)
		path = path[:len(path)-len(current.Key)]
	case *fullNode:
		for idx, child := range current.Children {
			if child == nil {
				continue
			}
			childPath := path
			childPath = append(childPath, byte(idx))
			if len(inspect.concurrentQueue)*2 < cap(inspect.concurrentQueue) {
				inspect.wg.Add(1)
				go inspect.SubConcurrentTraversal(theTrie, theTrieTreeStat, child, height+1, childPath)
			} else {
				inspect.ConcurrentTraversal(theTrie, theTrieTreeStat, child, height+1, childPath)
			}
		}
	case hashNode:
		n, err := theTrie.resloveWithoutTrack(current, nil)
		if err != nil {
			fmt.Printf("Resolve HashNode error: %v, TrieRoot: %v, Height: %v, Path: %v\n", err, theTrie.Hash().String(), height+1, path)
			return
		}
		inspect.ConcurrentTraversal(theTrie, theTrieTreeStat, n, height, path)
		return
	case valueNode:
		if !hasTerm(path) {
			break
		}
		var account Account
		if err := rlp.Decode(bytes.NewReader(current), &account); err != nil {
			break
		}
		if account.Root == (common.Hash{}) {
			break
		}
		ownerAddress := common.BytesToHash(hexToCompact(path))
		contractTrie, err := New(StorageTrieID(inspect.stateRootHash, ownerAddress, account.Root), inspect.db)
		if err != nil {
			// fmt.Printf("New contract trie node: %v, error: %v, Height: %v, Path: %v\n", theNode, err, height, path)
			break
		}
		trieStat := new(TrieTreeStat)
		trieStat.is_account_trie = false
		subRootHash := contractTrie.Hash().Big().Uint64()
		inspect.result.theTrieTreeStats.Set(subRootHash, trieStat)
		contractPath := make([]byte, 0)
		// log.Info("Find Contract Trie Tree, rootHash: ", contractTrie.Hash().String(), "")
		inspect.wg.Add(1)
		go inspect.SubConcurrentTraversal(contractTrie, trieStat, contractTrie.root, 0, contractPath)
	default:
		panic(errors.New("Invalid node type to traverse."))
	}
	theTrieTreeStat.AtomicAdd(theNode, height)
	return
}

func (inspect *Inspector) DisplayResult() {
	// display root hash
	roothash := inspect.trie.Hash().Big().Uint64()
	rootStat, _ := inspect.result.theTrieTreeStats.Get(roothash)
	rootStat.Display(roothash, "AccountTrie")

	// display contract trie
	trieNodeNums := make([][]uint64, 0, inspect.result.theTrieTreeStats.Len()-1)
	var totalContactsNodeStat NodeStat
	var contractTrieCnt uint64 = 0
	inspect.result.theTrieTreeStats.Each(func(rootHash uint64, stat *TrieTreeStat) bool {
		if rootHash == roothash {
			return true
		}
		contractTrieCnt++
		totalContactsNodeStat.ShortNodeCnt += stat.totalNodeStat.ShortNodeCnt
		totalContactsNodeStat.FullNodeCnt += stat.totalNodeStat.FullNodeCnt
		totalContactsNodeStat.ValueNodeCnt += stat.totalNodeStat.ValueNodeCnt
		totalNodeCnt := stat.totalNodeStat.ShortNodeCnt + stat.totalNodeStat.ValueNodeCnt + stat.totalNodeStat.FullNodeCnt
		trieNodeNums = append(trieNodeNums, []uint64{totalNodeCnt, rootHash})
		return true
	})

	fmt.Printf("Contract Trie, total trie num: %v, ShortNodeCnt: %v, FullNodeCnt: %v, ValueNodeCnt: %v\n",
		contractTrieCnt, totalContactsNodeStat.ShortNodeCnt, totalContactsNodeStat.FullNodeCnt, totalContactsNodeStat.ValueNodeCnt)
	sort.Slice(trieNodeNums, func(i, j int) bool {
		return trieNodeNums[i][0] > trieNodeNums[j][0]
	})
	// only display top 5
	for i, cntHash := range trieNodeNums {
		if i > 5 {
			break
		}
		stat, _ := inspect.result.theTrieTreeStats.Get(cntHash[1])
		stat.Display(cntHash[1], "ContractTrie")
		i++
	}
}
