package eth

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rlp"
)

// TestPartialReceiptsCancelDoesNotDeadlock is the regression test for the
// deadlock fixed in ethereum/go-ethereum#35537: an eth/70 receipts response
// with LastBlockIncomplete set made the reader goroutine re-request the
// remainder while holding receiptBufferLock and blocking on the dispatcher;
// if the dispatcher was concurrently processing a cancellation for the same
// peer it needed the same lock, and both goroutines waited on each other
// forever.
//
// Every round sets the interleaving up explicitly instead of relying on
// timing:
//
//  1. the dispatcher is parked inside p2p.Send of an unrelated request: the
//     test reads that packet but does not consume its payload, so the send
//     cannot return until the test says so;
//  2. the partial response is handled, and the test waits until the handler
//     goroutine is parked handing the follow-up request to the dispatcher;
//  3. the original request is cancelled, and the test waits until Close is
//     parked handing the cancel to the dispatcher;
//  4. the dispatcher is released with both operations ready. Go's select picks
//     one of them at random: on the unfixed code the cancel-first order
//     deadlocks, so a round fails with probability 1/2 and 24 rounds miss the
//     bug with probability 2^-24. On the fixed code both orders must finish
//     and leave no receipt buffer entry behind.
func TestPartialReceiptsCancelDoesNotDeadlock(t *testing.T) {
	backend := newTestBackend(1)
	defer backend.close()

	for round := 0; round < 24; round++ {
		runPartialReceiptsCancelRound(t, backend, round)
	}
}

func runPartialReceiptsCancelRound(t *testing.T, backend Backend, round int) {
	peer, _ := newTestPeer("peer", ETH70, backend)
	defer peer.close()

	fail := func(format string, args ...interface{}) {
		t.Helper()
		t.Fatalf("round %d: %s", round, fmt.Sprintf(format, args...))
	}
	// 1. Issue the receipt request the round is about, then park the
	//    dispatcher inside the send of an unrelated request.
	sink := make(chan *Response, 1)
	req := requestReceipts(t, peer, []common.Hash{{0x01}}, sink)

	go func() {
		_, _ = peer.RequestHeadersByNumber(1, 1, 0, false, make(chan *Response, 1))
	}()
	parked, err := peer.app.ReadMsg()
	if err != nil {
		fail("read parked request: %v", err)
	}
	// 2. Handle a legal partial response. The handler must end up parked
	//    while handing the follow-up request to the dispatcher.
	page := []*ReceiptList{NewReceiptList([]*types.Receipt{{Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: 21000}})}
	enc := encodeReceiptsPacket(t, req.id, page, true)
	done := make(chan error, 2)
	go func() { done <- handleReceipts70(backend, decoder{msg: enc}, peer.Peer) }()
	waitParkedIn(t, "eth.(*Peer).requestPartialReceipts")

	// 3. Cancel the original request. Close must end up parked while handing
	//    the cancel to the dispatcher.
	go func() { done <- req.Close() }()
	waitParkedIn(t, "eth.(*Request).Close")

	// 4. Release the dispatcher and consume whatever it sends from now on.
	go drainMessages(peer.app)
	if err := parked.Discard(); err != nil {
		fail("release dispatcher: %v", err)
	}
	deadline := time.After(10 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				fail("unexpected error: %v", err)
			}
		case <-deadline:
			for _, g := range goroutineDump() {
				if strings.Contains(g, "eth.(*Peer).dispatch") || strings.Contains(g, "eth.(*Request).Close") {
					t.Log(g)
				}
			}
			fail("partial receipt re-request and request cancellation deadlocked")
		}
	}
	// Whichever order the dispatcher picked, the cancelled request must not
	// leave its receipt buffer behind.
	if n := receiptBufferSize(peer.Peer); n != 0 {
		fail("%d receipt buffer entries left after cancel", n)
	}
}

// TestPartialReceiptsContinuation checks the client side of a multi-page
// receipt exchange: the follow-up continues the original request id from the
// first missing receipt of the incomplete block only, and the completed
// response reaches the original requester with the pages merged.
func TestPartialReceiptsContinuation(t *testing.T) {
	backend := newTestBackend(1)
	defer backend.close()

	peer, _ := newTestPeer("peer", ETH70, backend)
	defer peer.close()

	hashes := []common.Hash{{0x01}, {0x02}}
	sink := make(chan *Response, 1)
	req := requestReceipts(t, peer, hashes, sink)

	receipt := func(gas uint64) *types.Receipt {
		return &types.Receipt{Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: gas}
	}
	// Page 1: the first block is complete, the second block is cut after its
	// first receipt.
	page1 := []*ReceiptList{
		NewReceiptList([]*types.Receipt{receipt(21000)}),
		NewReceiptList([]*types.Receipt{receipt(21000)}),
	}
	enc1 := encodeReceiptsPacket(t, req.id, page1, true)
	done := make(chan error, 1)
	go func() { done <- handleReceipts70(backend, decoder{msg: enc1}, peer.Peer) }()

	msg, err := peer.app.ReadMsg()
	if err != nil {
		t.Fatalf("read follow-up request: %v", err)
	}
	if msg.Code != GetReceiptsMsg {
		t.Fatalf("follow-up message code %d, want %d", msg.Code, GetReceiptsMsg)
	}
	var followUp GetReceiptsPacket70
	if err := msg.Decode(&followUp); err != nil {
		t.Fatalf("decode follow-up request: %v", err)
	}
	msg.Discard()
	if followUp.RequestId != req.id || followUp.FirstBlockReceiptIndex != 1 ||
		len(followUp.GetReceiptsRequest) != 1 || followUp.GetReceiptsRequest[0] != hashes[1] {
		t.Fatalf("follow-up request id %d index %d hashes %v, want id %d index 1 hashes %v",
			followUp.RequestId, followUp.FirstBlockReceiptIndex, followUp.GetReceiptsRequest, req.id, hashes[1:])
	}
	if err := <-done; err != nil {
		t.Fatalf("handle page 1: %v", err)
	}
	// Page 2 completes the second block; the merged result must reach the
	// original requester.
	page2 := []*ReceiptList{NewReceiptList([]*types.Receipt{receipt(42000)})}
	enc2 := encodeReceiptsPacket(t, req.id, page2, false)
	go func() { done <- handleReceipts70(backend, decoder{msg: enc2}, peer.Peer) }()

	var res *Response
	select {
	case res = <-sink:
	case <-time.After(10 * time.Second):
		t.Fatal("merged receipt response never reached the requester")
	}
	res.Done <- nil
	if err := <-done; err != nil {
		t.Fatalf("handle page 2: %v", err)
	}
	if res.Req != req {
		t.Fatal("response delivered for a different request")
	}
	lists := *res.Res.(*ReceiptsRLPResponse)
	if len(lists) != 2 {
		t.Fatalf("response has %d receipt lists, want 2", len(lists))
	}
	for i, want := range []int{1, 2} {
		var items []rlp.RawValue
		if err := rlp.DecodeBytes(lists[i], &items); err != nil {
			t.Fatalf("decode receipt list %d: %v", i, err)
		}
		if len(items) != want {
			t.Fatalf("block %d has %d receipts, want %d", i, len(items), want)
		}
	}
	if n := receiptBufferSize(peer.Peer); n != 0 {
		t.Fatalf("%d receipt buffer entries left after completion", n)
	}
}

// requestReceipts issues a receipt request for hashes and consumes the packet
// the dispatcher sends for it, so the dispatcher is idle again on return.
func requestReceipts(t *testing.T, peer *testPeer, hashes []common.Hash, sink chan *Response) *Request {
	t.Helper()
	sent := make(chan error, 1)
	go func() {
		msg, err := peer.app.ReadMsg()
		if err == nil {
			err = msg.Discard()
		}
		sent <- err
	}()
	gasUsed := make([]uint64, len(hashes))
	for i := range gasUsed {
		gasUsed[i] = 1_000_000 // room for plenty of receipts per block
	}
	req, err := peer.RequestReceipts(hashes, gasUsed, make([]uint64, len(hashes)), sink)
	if err != nil {
		t.Fatalf("request receipts: %v", err)
	}
	if err := <-sent; err != nil {
		t.Fatalf("read receipt request: %v", err)
	}
	return req
}

// encodeReceiptsPacket encodes an eth/70 Receipts response for the given request.
func encodeReceiptsPacket(t *testing.T, id uint64, lists []*ReceiptList, incomplete bool) []byte {
	t.Helper()
	raw, err := rlp.EncodeToRawList(lists)
	if err != nil {
		t.Fatalf("encode receipt lists: %v", err)
	}
	enc, err := rlp.EncodeToBytes(&ReceiptsPacket70{RequestId: id, LastBlockIncomplete: incomplete, List: raw})
	if err != nil {
		t.Fatalf("encode receipts packet: %v", err)
	}
	return enc
}

// drainMessages consumes everything the peer sends until the pipe is closed.
func drainMessages(rw p2p.MsgReader) {
	for {
		msg, err := rw.ReadMsg()
		if err != nil {
			return
		}
		msg.Discard()
	}
}

func receiptBufferSize(p *Peer) int {
	p.receiptBufferLock.Lock()
	defer p.receiptBufferLock.Unlock()
	return len(p.receiptBuffer)
}

// goroutineDump returns the stack of every goroutine, one entry each.
func goroutineDump() []string {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Split(string(buf[:n]), "\n\n")
		}
		buf = make([]byte, 2*len(buf))
	}
}

// waitParkedIn blocks until a goroutine with the named function on its stack
// is parked in a select, i.e. it has reached the channel operation the test
// wants to race, and fails the test if that does not happen in time. Checking
// the goroutine state instead of sleeping is what makes each round meaningful
// regardless of scheduling.
func waitParkedIn(t *testing.T, function string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		for _, g := range goroutineDump() {
			header, _, _ := strings.Cut(g, "\n")
			if strings.Contains(header, "[select") && strings.Contains(g, function) {
				return
			}
		}
	}
	t.Fatalf("no goroutine parked in a select inside %s", function)
}
