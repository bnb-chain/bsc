// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package downloader

import (
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

// makeChain creates a chain of n blocks starting at and including parent.
// The returned hash chain is ordered head->parent.
// If empty is false, every second block (i%2==0) contains one transaction.
// No uncles are added.
func makeChain(n int, seed byte, parent *types.Block, empty bool) ([]*types.Block, []types.Receipts) {
	blocks, receipts := core.GenerateChain(params.TestChainConfig, parent, ethash.NewFaker(), testDB, n, func(i int, block *core.BlockGen) {
		block.SetCoinbase(common.Address{seed})
		// Add one tx to every second block
		if !empty && i%2 == 0 {
			signer := types.MakeSigner(params.TestChainConfig, block.Number(), block.Timestamp())
			tx, err := types.SignTx(types.NewTransaction(block.TxNonce(testAddress), common.Address{seed}, big.NewInt(1000), params.TxGas, block.BaseFee(), nil), signer, testKey)
			if err != nil {
				panic(err)
			}
			block.AddTx(tx)
		}
	})
	return blocks, receipts
}

type chainData struct {
	blocks []*types.Block
	offset int
}

var chain *chainData
var emptyChain *chainData

func init() {
	// Create a chain of blocks to import
	targetBlocks := 128
	blocks, _ := makeChain(targetBlocks, 0, testGenesis, false)
	chain = &chainData{blocks, 0}

	blocks, _ = makeChain(targetBlocks, 0, testGenesis, true)
	emptyChain = &chainData{blocks, 0}
}

func (chain *chainData) headers() []*types.Header {
	hdrs := make([]*types.Header, len(chain.blocks))
	for i, b := range chain.blocks {
		hdrs[i] = b.Header()
	}
	return hdrs
}

func (chain *chainData) Len() int {
	return len(chain.blocks)
}

func dummyPeer(id string) *peerConnection {
	p := &peerConnection{
		id:      id,
		lacking: make(map[common.Hash]struct{}),
	}
	return p
}

func TestBasics(t *testing.T) {
	numOfBlocks := len(emptyChain.blocks)
	numOfReceipts := len(emptyChain.blocks) / 2

	q := newQueue(10, 10)
	if !q.Idle() {
		t.Errorf("new queue should be idle")
	}
	q.Prepare(1, SnapSync)
	if res := q.Results(false); len(res) != 0 {
		t.Fatal("new queue should have 0 results")
	}

	// Schedule a batch of headers
	headers := chain.headers()
	hashes := make([]common.Hash, len(headers))
	for i, header := range headers {
		hashes[i] = header.Hash()
	}
	q.Schedule(headers, hashes, 1)
	if q.Idle() {
		t.Errorf("queue should not be idle")
	}
	if got, exp := q.PendingBodies(), chain.Len(); got != exp {
		t.Errorf("wrong pending block count, got %d, exp %d", got, exp)
	}
	// Only non-empty receipts get added to task-queue
	if got, exp := q.PendingReceipts(), 64; got != exp {
		t.Errorf("wrong pending receipt count, got %d, exp %d", got, exp)
	}
	// Items are now queued for downloading, next step is that we tell the
	// queue that a certain peer will deliver them for us
	{
		peer := dummyPeer("peer-1")
		fetchReq, _, throttle := q.ReserveBodies(peer, 50)
		if !throttle {
			// queue size is only 10, so throttling should occur
			t.Fatal("should throttle")
		}
		// But we should still get the first things to fetch
		if got, exp := len(fetchReq.Headers), 5; got != exp {
			t.Fatalf("expected %d requests, got %d", exp, got)
		}
		if got, exp := fetchReq.Headers[0].Number.Uint64(), uint64(1); got != exp {
			t.Fatalf("expected header %d, got %d", exp, got)
		}
	}
	if exp, got := q.blockTaskQueue.Size(), numOfBlocks-10; exp != got {
		t.Errorf("expected block task queue to be %d, got %d", exp, got)
	}
	if exp, got := q.receiptTaskQueue.Size(), numOfReceipts; exp != got {
		t.Errorf("expected receipt task queue to be %d, got %d", exp, got)
	}
	{
		peer := dummyPeer("peer-2")
		fetchReq, _, throttle := q.ReserveBodies(peer, 50)

		// The second peer should hit throttling
		if !throttle {
			t.Fatalf("should throttle")
		}
		// And not get any fetches at all, since it was throttled to begin with
		if fetchReq != nil {
			t.Fatalf("should have no fetches, got %d", len(fetchReq.Headers))
		}
	}
	if exp, got := q.blockTaskQueue.Size(), numOfBlocks-10; exp != got {
		t.Errorf("expected block task queue to be %d, got %d", exp, got)
	}
	if exp, got := q.receiptTaskQueue.Size(), numOfReceipts; exp != got {
		t.Errorf("expected receipt task queue to be %d, got %d", exp, got)
	}
	{
		// The receipt delivering peer should not be affected
		// by the throttling of body deliveries
		peer := dummyPeer("peer-3")
		fetchReq, _, throttle := q.ReserveReceipts(peer, 50)
		if !throttle {
			// queue size is only 10, so throttling should occur
			t.Fatal("should throttle")
		}
		// But we should still get the first things to fetch
		if got, exp := len(fetchReq.Headers), 5; got != exp {
			t.Fatalf("expected %d requests, got %d", exp, got)
		}
		if got, exp := fetchReq.Headers[0].Number.Uint64(), uint64(1); got != exp {
			t.Fatalf("expected header %d, got %d", exp, got)
		}
	}
	if exp, got := q.blockTaskQueue.Size(), numOfBlocks-10; exp != got {
		t.Errorf("expected block task queue to be %d, got %d", exp, got)
	}
	if exp, got := q.receiptTaskQueue.Size(), numOfReceipts-5; exp != got {
		t.Errorf("expected receipt task queue to be %d, got %d", exp, got)
	}
	if got, exp := q.resultCache.countCompleted(), 0; got != exp {
		t.Errorf("wrong processable count, got %d, exp %d", got, exp)
	}
}

func TestEmptyBlocks(t *testing.T) {
	numOfBlocks := len(emptyChain.blocks)

	q := newQueue(10, 10)

	q.Prepare(1, SnapSync)

	// Schedule a batch of headers
	headers := emptyChain.headers()
	hashes := make([]common.Hash, len(headers))
	for i, header := range headers {
		hashes[i] = header.Hash()
	}
	q.Schedule(headers, hashes, 1)
	if q.Idle() {
		t.Errorf("queue should not be idle")
	}
	if got, exp := q.PendingBodies(), len(emptyChain.blocks); got != exp {
		t.Errorf("wrong pending block count, got %d, exp %d", got, exp)
	}
	if got, exp := q.PendingReceipts(), 0; got != exp {
		t.Errorf("wrong pending receipt count, got %d, exp %d", got, exp)
	}
	// They won't be processable, because the fetchresults haven't been
	// created yet
	if got, exp := q.resultCache.countCompleted(), 0; got != exp {
		t.Errorf("wrong processable count, got %d, exp %d", got, exp)
	}

	// Items are now queued for downloading, next step is that we tell the
	// queue that a certain peer will deliver them for us
	// That should trigger all of them to suddenly become 'done'
	{
		// Reserve blocks
		peer := dummyPeer("peer-1")
		fetchReq, _, _ := q.ReserveBodies(peer, 50)

		// there should be nothing to fetch, blocks are empty
		if fetchReq != nil {
			t.Fatal("there should be no body fetch tasks remaining")
		}
	}
	if q.blockTaskQueue.Size() != numOfBlocks-10 {
		t.Errorf("expected block task queue to be %d, got %d", numOfBlocks-10, q.blockTaskQueue.Size())
	}
	if q.receiptTaskQueue.Size() != 0 {
		t.Errorf("expected receipt task queue to be %d, got %d", 0, q.receiptTaskQueue.Size())
	}
	{
		peer := dummyPeer("peer-3")
		fetchReq, _, _ := q.ReserveReceipts(peer, 50)

		// there should be nothing to fetch, blocks are empty
		if fetchReq != nil {
			t.Fatal("there should be no receipt fetch tasks remaining")
		}
	}
	if q.blockTaskQueue.Size() != numOfBlocks-10 {
		t.Errorf("expected block task queue to be %d, got %d", numOfBlocks-10, q.blockTaskQueue.Size())
	}
	if q.receiptTaskQueue.Size() != 0 {
		t.Errorf("expected receipt task queue to be %d, got %d", 0, q.receiptTaskQueue.Size())
	}
	if got, exp := q.resultCache.countCompleted(), 10; got != exp {
		t.Errorf("wrong processable count, got %d, exp %d", got, exp)
	}
}

func TestBodyMemoryLimitWithHeadGap(t *testing.T) {
	for _, mode := range []SyncMode{FullSync, SnapSync} {
		t.Run(mode.String(), func(t *testing.T) { testBodyMemoryLimitWithHeadGap(t, mode) })
	}
}

func testBodyMemoryLimitWithHeadGap(t *testing.T, mode SyncMode) {
	txs := make([]*types.Transaction, 4)
	for i := range txs {
		txs[i] = types.NewTransaction(uint64(i), common.Address{}, big.NewInt(0), params.TxGas, big.NewInt(1), nil)
	}
	txList, err := rlp.EncodeToRawList(txs)
	if err != nil {
		t.Fatal(err)
	}
	body := eth.BlockBody{Transactions: txList}
	bodySize := blockBodyMemory(&body)

	oldMemoryLimit := blockCacheMemory
	blockCacheMemory = int(bodySize*4) + 1 // Rejection can precede an exactly full cache.
	defer func() { blockCacheMemory = oldMemoryLimit }()

	headers := make([]*types.Header, 5)
	headerHashes := make([]common.Hash, len(headers))
	parentHash := common.Hash{}
	receipts := types.Receipts{&types.Receipt{Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: params.TxGas}}
	receiptRoot := types.DeriveSha(receipts, trie.NewStackTrie(nil))
	receiptRLP, err := rlp.EncodeToBytes(receipts)
	if err != nil {
		t.Fatal(err)
	}
	txRoot := types.DeriveSha(types.Transactions(txs), trie.NewStackTrie(nil))
	for i := range headers {
		headers[i] = &types.Header{
			ParentHash:  parentHash,
			Difficulty:  big.NewInt(1),
			Number:      big.NewInt(int64(i + 1)),
			TxHash:      txRoot,
			ReceiptHash: receiptRoot,
			UncleHash:   types.EmptyUncleHash,
		}
		headerHashes[i] = headers[i].Hash()
		parentHash = headerHashes[i]
	}

	q := newQueue(len(headers), len(headers))
	q.Prepare(1, mode)
	if inserted := q.Schedule(headers, headerHashes, 1); inserted != len(headers) {
		t.Fatalf("scheduled %d headers, want %d", inserted, len(headers))
	}

	headPeer := dummyPeer("head")
	if request, _, _ := q.ReserveBodies(headPeer, 1); request == nil || len(request.Headers) != 1 || request.Headers[0] != headers[0] {
		t.Fatal("failed to reserve the head body")
	}
	tailPeer := dummyPeer("tail")
	request, _, _ := q.ReserveBodies(tailPeer, 4)
	if request == nil || len(request.Headers) != 4 {
		if request == nil {
			t.Fatal("failed to reserve tail bodies")
		}
		t.Fatalf("reserved %d tail bodies, want 4", len(request.Headers))
	}
	hashes := eth.BlockBodyHashes{
		TransactionRoots: []common.Hash{txRoot, txRoot, txRoot, txRoot},
		UncleHashes:      []common.Hash{types.EmptyUncleHash, types.EmptyUncleHash, types.EmptyUncleHash, types.EmptyUncleHash},
		WithdrawalRoots:  make([]common.Hash, 4),
	}
	accepted, err := q.DeliverBodies(tailPeer.id, hashes, []eth.BlockBody{body, body, body, body})
	if !errors.Is(err, errBodyCacheFull) {
		t.Fatalf("delivery error %v, want %v", err, errBodyCacheFull)
	}
	if accepted != 3 {
		t.Fatalf("accepted %d tail bodies, want 3", accepted)
	}
	if q.resultMemory != 3*bodySize {
		t.Fatalf("cached body memory %v, want %v", q.resultMemory, 3*bodySize)
	}

	// Nothing can be freed before the head arrives. Repeated reservations must
	// stop here, including when a little unused budget remains.
	for i := 0; i < 2; i++ {
		if req, _, throttle := q.ReserveBodies(tailPeer, 1); req != nil || !throttle {
			t.Fatal("body cache pressure did not throttle reservations")
		}
	}
	// A timed-out head must remain fetchable while the tail is throttled.
	q.ExpireBodies(headPeer.id)
	if req, _, _ := q.ReserveBodies(headPeer, 1); req == nil || req.Headers[0] != headers[0] {
		t.Fatal("body cache pressure blocked the missing head")
	}
	if mode == SnapSync {
		receiptPeer := dummyPeer("receipts")
		if req, _, _ := q.ReserveReceipts(receiptPeer, len(headers)); req == nil || len(req.Headers) != len(headers) {
			t.Fatal("body cache pressure blocked receipts")
		}
		list := make([]rlp.RawValue, len(headers))
		roots := make([]common.Hash, len(headers))
		for i := range list {
			list[i], roots[i] = receiptRLP, receiptRoot
		}
		if accepted, err := q.DeliverReceipts(receiptPeer.id, list, roots); err != nil || accepted != len(headers) {
			t.Fatalf("receipts accepted %d, error %v", accepted, err)
		}
	}

	headHashes := eth.BlockBodyHashes{
		TransactionRoots: []common.Hash{txRoot},
		UncleHashes:      []common.Hash{types.EmptyUncleHash},
		WithdrawalRoots:  make([]common.Hash, 1),
	}
	if accepted, err := q.DeliverBodies(headPeer.id, headHashes, []eth.BlockBody{body}); err != nil || accepted != 1 {
		t.Fatalf("head delivery accepted %d, error %v", accepted, err)
	}
	if results := q.Results(false); len(results) != 4 {
		t.Fatalf("returned %d results after filling head gap, want 4", len(results))
	}
	if q.resultMemory != 0 {
		t.Fatalf("cached body memory after release %v, want 0", q.resultMemory)
	}

	retryPeer := dummyPeer("retry")
	request, _, _ = q.ReserveBodies(retryPeer, 1)
	if request == nil || len(request.Headers) != 1 || request.Headers[0] != headers[4] {
		t.Fatal("failed to reserve body rejected by the memory limit")
	}
	if accepted, err := q.DeliverBodies(retryPeer.id, headHashes, []eth.BlockBody{body}); err != nil || accepted != 1 {
		t.Fatalf("retry delivery accepted %d, error %v", accepted, err)
	}
}

func TestBodyMemoryLimitRejectsOversizedBody(t *testing.T) {
	txs := make([]*types.Transaction, 4)
	for i := range txs {
		txs[i] = types.NewTransaction(uint64(i), common.Address{}, big.NewInt(0), params.TxGas, big.NewInt(1), nil)
	}
	txList, err := rlp.EncodeToRawList(txs)
	if err != nil {
		t.Fatal(err)
	}
	body := eth.BlockBody{Transactions: txList}

	oldMemoryLimit := blockCacheMemory
	blockCacheMemory = int(blockBodyMemory(&body) * 2)
	defer func() { blockCacheMemory = oldMemoryLimit }()

	txRoot := types.DeriveSha(types.Transactions(txs), trie.NewStackTrie(nil))
	header := &types.Header{
		Difficulty: big.NewInt(1),
		Number:     big.NewInt(1),
		TxHash:     txRoot,
		UncleHash:  types.EmptyUncleHash,
	}
	q := newQueue(1, 1)
	q.Prepare(1, FullSync)
	q.Schedule([]*types.Header{header}, []common.Hash{header.Hash()}, 1)
	peer := dummyPeer("peer")
	if request, _, _ := q.ReserveBodies(peer, 1); request == nil {
		t.Fatal("failed to reserve body")
	}
	hashes := eth.BlockBodyHashes{
		TransactionRoots: []common.Hash{txRoot},
		UncleHashes:      []common.Hash{types.EmptyUncleHash},
		WithdrawalRoots:  make([]common.Hash, 1),
	}
	accepted, err := q.DeliverBodies(peer.id, hashes, []eth.BlockBody{body})
	if !errors.Is(err, errInvalidBody) {
		t.Fatalf("delivery error %v, want %v", err, errInvalidBody)
	}
	if accepted != 0 || q.resultMemory != 0 {
		t.Fatalf("accepted %d bodies and cached %v, want no decoded body", accepted, q.resultMemory)
	}
}

func TestBodyMemoryTypedTransactions(t *testing.T) {
	for _, count := range []int{1, 4, 5, 64} {
		accesses := make(types.AccessList, count)
		for i := range accesses {
			accesses[i].StorageKeys = make([]common.Hash, count)
		}
		for _, data := range []types.TxData{
			&types.AccessListTx{AccessList: accesses},
			&types.DynamicFeeTx{AccessList: accesses},
			&types.BlobTx{AccessList: accesses, BlobHashes: make([]common.Hash, count)},
			&types.SetCodeTx{AccessList: accesses, AuthList: make([]types.SetCodeAuthorization, count)},
		} {
			tx := types.NewTx(data)
			t.Run(fmt.Sprintf("type%d/items%d", tx.Type(), count), func(t *testing.T) {
				list, err := rlp.EncodeToRawList([]*types.Transaction{tx})
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := list.Items()
				if err != nil {
					t.Fatal(err)
				}
				// Measure retained nested backing arrays, including decoder growth
				// capacity, instead of deriving the assertion from the estimator.
				accesses := decoded[0].AccessList()
				nested := uint64(cap(accesses)) * uint64(unsafe.Sizeof(types.AccessTuple{}))
				for _, tuple := range accesses {
					nested += uint64(cap(tuple.StorageKeys)) * uint64(unsafe.Sizeof(common.Hash{}))
				}
				nested += uint64(cap(decoded[0].BlobHashes())) * uint64(unsafe.Sizeof(common.Hash{}))
				nested += uint64(cap(decoded[0].SetCodeAuthorizations())) * uint64(unsafe.Sizeof(types.SetCodeAuthorization{}))
				if extra := transactionListMemory(&list) - rawListMemory(&list); extra < nested {
					t.Fatalf("nested estimate %d below retained backing arrays %d", extra, nested)
				}
			})
		}
	}
}

// XTestDelivery does some more extensive testing of events that happen,
// blocks that become known and peers that make reservations and deliveries.
// disabled since it's not really a unit-test, but can be executed to test
// some more advanced scenarios
func XTestDelivery(t *testing.T) {
	// the outside network, holding blocks
	blo, rec := makeChain(128, 0, testGenesis, false)
	world := newNetwork()
	world.receipts = rec
	world.chain = blo
	world.progress(10)
	if false {
		log.SetDefault(log.NewLogger(slog.NewTextHandler(os.Stdout, nil)))
	}
	q := newQueue(10, 10)
	var wg sync.WaitGroup
	q.Prepare(1, SnapSync)
	wg.Add(1)
	go func() {
		// deliver headers
		defer wg.Done()
		c := 1
		for {
			//fmt.Printf("getting headers from %d\n", c)
			headers := world.headers(c)
			hashes := make([]common.Hash, len(headers))
			for i, header := range headers {
				hashes[i] = header.Hash()
			}
			l := len(headers)
			//fmt.Printf("scheduling %d headers, first %d last %d\n",
			//	l, headers[0].Number.Uint64(), headers[len(headers)-1].Number.Uint64())
			q.Schedule(headers, hashes, uint64(c))
			c += l
		}
	}()
	wg.Add(1)
	go func() {
		// collect results
		defer wg.Done()
		tot := 0
		for {
			res := q.Results(true)
			tot += len(res)
			fmt.Printf("got %d results, %d tot\n", len(res), tot)
			// Now we can forget about these
			world.forget(res[len(res)-1].Header.Number.Uint64())
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		// reserve body fetch
		i := 4
		for {
			peer := dummyPeer(fmt.Sprintf("peer-%d", i))
			f, _, _ := q.ReserveBodies(peer, rand.Intn(30))
			if f != nil {
				var (
					emptyList []*types.Header
					txset     [][]*types.Transaction
					uncleset  [][]*types.Header
					bodies    []eth.BlockBody
				)
				numToSkip := rand.Intn(len(f.Headers))
				for _, hdr := range f.Headers[0 : len(f.Headers)-numToSkip] {
					txs := world.getTransactions(hdr.Number.Uint64())
					txset = append(txset, txs)
					uncleset = append(uncleset, emptyList)
					txsList, _ := rlp.EncodeToRawList(txs)
					bodies = append(bodies, eth.BlockBody{Transactions: txsList})
				}
				hashes := eth.BlockBodyHashes{
					TransactionRoots: make([]common.Hash, len(txset)),
					UncleHashes:      make([]common.Hash, len(uncleset)),
					WithdrawalRoots:  make([]common.Hash, len(txset)),
				}
				hasher := trie.NewStackTrie(nil)
				for i, txs := range txset {
					hashes.TransactionRoots[i] = types.DeriveSha(types.Transactions(txs), hasher)
				}
				for i, uncles := range uncleset {
					hashes.UncleHashes[i] = types.CalcUncleHash(uncles)
				}

				time.Sleep(100 * time.Millisecond)
				if _, err := q.DeliverBodies(peer.id, hashes, bodies); err != nil {
					fmt.Printf("delivered %d bodies %v\n", len(txset), err)
				}
			} else {
				i++
				time.Sleep(200 * time.Millisecond)
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		// reserve receiptfetch
		peer := dummyPeer("peer-3")
		for {
			f, _, _ := q.ReserveReceipts(peer, rand.Intn(50))
			if f != nil {
				var rcs []types.Receipts
				for _, hdr := range f.Headers {
					rcs = append(rcs, world.getReceipts(hdr.Number.Uint64()))
				}
				hasher := trie.NewStackTrie(nil)
				hashes := make([]common.Hash, len(rcs))
				for i, receipt := range rcs {
					hashes[i] = types.DeriveSha(receipt, hasher)
				}
				_, err := q.DeliverReceipts(peer.id, types.EncodeBlockReceiptLists(rcs), hashes)
				if err != nil {
					fmt.Printf("delivered %d receipts %v\n", len(rcs), err)
				}
				time.Sleep(100 * time.Millisecond)
			} else {
				time.Sleep(200 * time.Millisecond)
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			time.Sleep(300 * time.Millisecond)
			//world.tick()
			//fmt.Printf("trying to progress\n")
			world.progress(rand.Intn(100))
		}
		for i := 0; i < 50; i++ {
			time.Sleep(2990 * time.Millisecond)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			time.Sleep(990 * time.Millisecond)
			fmt.Printf("world block tip is %d\n",
				world.chain[len(world.chain)-1].Header().Number.Uint64())
			fmt.Println(q.Stats())
		}
	}()
	wg.Wait()
}

func newNetwork() *network {
	var l sync.RWMutex
	return &network{
		cond:   sync.NewCond(&l),
		offset: 1, // block 1 is at blocks[0]
	}
}

// represents the network
type network struct {
	offset   int
	chain    []*types.Block
	receipts []types.Receipts
	lock     sync.RWMutex
	cond     *sync.Cond
}

func (n *network) getTransactions(blocknum uint64) types.Transactions {
	index := blocknum - uint64(n.offset)
	return n.chain[index].Transactions()
}
func (n *network) getReceipts(blocknum uint64) types.Receipts {
	index := blocknum - uint64(n.offset)
	if got := n.chain[index].Header().Number.Uint64(); got != blocknum {
		fmt.Printf("Err, got %d exp %d\n", got, blocknum)
		panic("sd")
	}
	return n.receipts[index]
}

func (n *network) forget(blocknum uint64) {
	index := blocknum - uint64(n.offset)
	n.chain = n.chain[index:]
	n.receipts = n.receipts[index:]
	n.offset = int(blocknum)
}
func (n *network) progress(numBlocks int) {
	n.lock.Lock()
	defer n.lock.Unlock()
	//fmt.Printf("progressing...\n")
	newBlocks, newR := makeChain(numBlocks, 0, n.chain[len(n.chain)-1], false)
	n.chain = append(n.chain, newBlocks...)
	n.receipts = append(n.receipts, newR...)
	n.cond.Broadcast()
}

func (n *network) headers(from int) []*types.Header {
	numHeaders := 128
	var hdrs []*types.Header
	index := from - n.offset

	for index >= len(n.chain) {
		// wait for progress
		n.cond.L.Lock()
		//fmt.Printf("header going into wait\n")
		n.cond.Wait()
		index = from - n.offset
		n.cond.L.Unlock()
	}
	n.lock.RLock()
	defer n.lock.RUnlock()
	for i, b := range n.chain[index:] {
		hdrs = append(hdrs, b.Header())
		if i >= numHeaders {
			break
		}
	}
	return hdrs
}
