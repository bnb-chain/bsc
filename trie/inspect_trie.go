package trie

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/mclock"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/triedb/database"
	"github.com/olekukonko/tablewriter"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/sync/semaphore"
)

type Database interface {
	database.NodeDatabase
	Scheme() string
	Cap(limit common.StorageSize) error
	Disk() ethdb.Database
}

const TopN = 3
const DEFAULT_TRIEDBCACHE_SIZE = 1024 * 1024 * 1024

type Inspector struct {
	trie           *Trie // traverse trie
	db             Database
	stateRootHash  common.Hash
	blockNum       uint64
	root           node // root of triedb
	sem            *semaphore.Weighted
	eoaAccountNums uint64

	wg sync.WaitGroup

	results stat
	topN    int

	totalAccountNum atomic.Uint64
	totalStorageNum atomic.Uint64
	lastTime        mclock.AbsTime
}

type stat struct {
	lock             sync.RWMutex
	account          *trieStat
	storageTopN      []*trieStat
	storageTopNTotal []uint64
	storageTotal     nodeStat
	storageTrieNum   uint64
}

type trieStat struct {
	owner           common.Hash
	totalNodeStat   nodeStat
	nodeStatByLevel [16]nodeStat
}

type nodeStat struct {
	ShortNodeCnt atomic.Uint64
	FullNodeCnt  atomic.Uint64
	ValueNodeCnt atomic.Uint64
}

func (ns *nodeStat) IsEmpty() bool {
	if ns.FullNodeCnt.Load() == 0 && ns.ShortNodeCnt.Load() == 0 && ns.ValueNodeCnt.Load() == 0 {
		return true
	}
	return false
}

func (s *stat) add(ts *trieStat, topN int) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if ts.owner == (common.Hash{}) {
		s.account = ts
		return
	}

	total := ts.totalNodeStat.ValueNodeCnt.Load() + ts.totalNodeStat.FullNodeCnt.Load() + ts.totalNodeStat.ShortNodeCnt.Load()
	if len(s.storageTopNTotal) == 0 || total > s.storageTopNTotal[len(s.storageTopNTotal)-1] {
		var (
			i int
			t uint64
		)
		for i, t = range s.storageTopNTotal {
			if total < t {
				continue
			}
			break
		}
		s.storageTopNTotal = append(s.storageTopNTotal[:i], append([]uint64{total}, s.storageTopNTotal[i:]...)...)
		s.storageTopN = append(s.storageTopN[:i], append([]*trieStat{ts}, s.storageTopN[i:]...)...)
		if len(s.storageTopN) > topN {
			s.storageTopNTotal = s.storageTopNTotal[:topN]
			s.storageTopN = s.storageTopN[:topN]
		}
	}

	s.storageTotal.ShortNodeCnt.Add(ts.totalNodeStat.ShortNodeCnt.Load())
	s.storageTotal.ValueNodeCnt.Add(ts.totalNodeStat.ValueNodeCnt.Load())
	s.storageTotal.FullNodeCnt.Add(ts.totalNodeStat.FullNodeCnt.Load())
	s.storageTrieNum++
}

func (trieStat *trieStat) add(theNode node, height int) {
	switch (theNode).(type) {
	case *shortNode:
		trieStat.totalNodeStat.ShortNodeCnt.Add(1)
		trieStat.nodeStatByLevel[height].ShortNodeCnt.Add(1)
	case *fullNode:
		trieStat.totalNodeStat.FullNodeCnt.Add(1)
		trieStat.nodeStatByLevel[height].FullNodeCnt.Add(1)
	case valueNode:
		trieStat.totalNodeStat.ValueNodeCnt.Add(1)
		trieStat.nodeStatByLevel[height].ValueNodeCnt.Add(1)
	}
}

func (trieStat *trieStat) Display(ownerAddress string, treeType string) string {
	sw := new(strings.Builder)
	table := tablewriter.NewWriter(sw)
	table.SetHeader([]string{"-", "Level", "ShortNodeCnt", "FullNodeCnt", "ValueNodeCnt"})
	if ownerAddress == "" {
		table.SetCaption(true, fmt.Sprintf("%v", treeType))
	} else {
		table.SetCaption(true, fmt.Sprintf("%v-%v", treeType, ownerAddress))
	}
	table.SetAlignment(1)

	for i := range trieStat.nodeStatByLevel {
		if trieStat.nodeStatByLevel[i].IsEmpty() {
			continue
		}
		table.AppendBulk([][]string{
			{"-", fmt.Sprintf("%d", i),
				fmt.Sprintf("%d", trieStat.nodeStatByLevel[i].ShortNodeCnt.Load()),
				fmt.Sprintf("%d", trieStat.nodeStatByLevel[i].FullNodeCnt.Load()),
				fmt.Sprintf("%d", trieStat.nodeStatByLevel[i].ValueNodeCnt.Load())},
		})
	}
	table.AppendBulk([][]string{
		{"Total", "-", fmt.Sprintf("%d", trieStat.totalNodeStat.ShortNodeCnt.Load()), fmt.Sprintf("%d", trieStat.totalNodeStat.FullNodeCnt.Load()), fmt.Sprintf("%d", trieStat.totalNodeStat.ValueNodeCnt.Load())},
	})
	table.Render()
	return sw.String()
}

// NewInspector return a inspector obj
func NewInspector(tr *Trie, db Database, stateRootHash common.Hash, blockNum uint64, jobNum uint64, topN int) (*Inspector, error) {
	if tr == nil {
		return nil, errors.New("trie is nil")
	}

	if tr.root == nil {
		return nil, errors.New("trie root is nil")
	}

	ins := &Inspector{
		trie:            tr,
		db:              db,
		stateRootHash:   stateRootHash,
		blockNum:        blockNum,
		root:            tr.root,
		results:         stat{},
		topN:            topN,
		totalAccountNum: atomic.Uint64{},
		totalStorageNum: atomic.Uint64{},
		lastTime:        mclock.Now(),
		sem:             semaphore.NewWeighted(int64(jobNum)),

		wg: sync.WaitGroup{},

		eoaAccountNums: 0,
	}

	return ins, nil
}

// Run statistics, external call
func (s *Inspector) Run() {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			if s.db.Scheme() == rawdb.HashScheme {
				s.db.Cap(DEFAULT_TRIEDBCACHE_SIZE)
			}
			runtime.GC()
		}
	}()

	log.Info("Find Account Trie Tree", "rootHash: ", s.trie.Hash().String(), "BlockNum: ", s.blockNum)

	ts := &trieStat{
		owner: common.Hash{},
	}
	s.traversal(s.trie, ts, s.root, 0, []byte{})
	s.results.add(ts, s.topN)
	s.wg.Wait()
}

func (s *Inspector) traversal(trie *Trie, ts *trieStat, n node, height int, path []byte) {
	// nil node
	if n == nil {
		return
	}

	ts.add(n, height)

	switch current := (n).(type) {
	case *shortNode:
		s.traversal(trie, ts, current.Val, height, append(path, current.Key...))
	case *fullNode:
		for idx, child := range current.Children {
			if child == nil {
				continue
			}
			p := common.CopyBytes(append(path, byte(idx)))
			s.traversal(trie, ts, child, height+1, p)
		}
	case hashNode:
		tn, err := trie.resloveWithoutTrack(current, path)
		if err != nil {
			fmt.Printf("Resolve HashNode error: %v, TrieRoot: %v, Height: %v, Path: %v\n", err, trie.Hash().String(), height+1, path)
			return
		}
		s.PrintProgress(trie)
		s.traversal(trie, ts, tn, height, path)
	case valueNode:
		if !hasTerm(path) {
			break
		}
		var account types.StateAccount
		if err := rlp.Decode(bytes.NewReader(current), &account); err != nil {
			break
		}
		if common.BytesToHash(account.CodeHash) == types.EmptyCodeHash {
			s.eoaAccountNums++
		}
		if account.Root == (common.Hash{}) || account.Root == types.EmptyRootHash {
			break
		}
		ownerAddress := common.BytesToHash(hexToCompact(path))
		contractTrie, err := New(StorageTrieID(s.stateRootHash, ownerAddress, account.Root), s.db)
		if err != nil {
			panic(err)
		}
		contractTrie.opTracer.reset()

		if s.sem.TryAcquire(1) {
			s.wg.Add(1)
			go func() {
				t := &trieStat{
					owner: ownerAddress,
				}
				s.traversal(contractTrie, t, contractTrie.root, 0, []byte{})
				s.results.add(t, s.topN)
				s.sem.Release(1)
				s.wg.Done()
			}()
		} else {
			t := &trieStat{
				owner: ownerAddress,
			}
			s.traversal(contractTrie, t, contractTrie.root, 0, []byte{})
			s.results.add(t, s.topN)
		}
	default:
		panic(errors.New("invalid node type to traverse"))
	}
}

func (s *Inspector) PrintProgress(t *Trie) {
	var (
		elapsed = mclock.Now().Sub(s.lastTime)
	)
	if t.owner == (common.Hash{}) {
		s.totalAccountNum.Add(1)
	} else {
		s.totalStorageNum.Add(1)
	}
	if elapsed > 4*time.Second {
		log.Info("traversal progress", "TotalAccountNum", s.totalAccountNum.Load(), "TotalStorageNum", s.totalStorageNum.Load(), "Goroutine", runtime.NumGoroutine())
		s.lastTime = mclock.Now()
	}
}

func (s *Inspector) DisplayResult() {
	// display root hash
	fmt.Println(s.results.account.Display("", "AccountTrie"))
	fmt.Println("EOA accounts num: ", s.eoaAccountNums)

	// display contract trie
	for _, st := range s.results.storageTopN {
		fmt.Println(st.Display(st.owner.String(), "StorageTrie"))
	}
	fmt.Printf("Contract Trie, total trie num: %v, ShortNodeCnt: %v, FullNodeCnt: %v, ValueNodeCnt: %v\n",
		s.results.storageTrieNum, s.results.storageTotal.ShortNodeCnt.Load(), s.results.storageTotal.FullNodeCnt.Load(), s.results.storageTotal.ValueNodeCnt.Load())
}
