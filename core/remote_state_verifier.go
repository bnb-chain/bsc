package core

import (
	"fmt"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/metrics"
	"math/rand"
	"time"

	lru "github.com/hashicorp/golang-lru"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

const (
	verifiedCacheSize = 256
	maxForkHeight     = 11

	// defaultPeerNumber is default number of verify peers
	defaultPeerNumber = 3
	// pruneHeightDiff indicates that if the height difference between current block and task's
	// corresponding block is larger than it, the task should be pruned.
	pruneHeightDiff = 15
	pruneInterval   = 5 * time.Second
	resendInterval  = 2 * time.Second
	// tryAllPeersTime is the time that a block has not been verified and then try all the valid verify peers.
	tryAllPeersTime = 15 * time.Second
)

var (
	remoteVerifyTaskCounter = metrics.NewRegisteredCounter("remote/state/verify/task/total", nil)

	statusFullVerifiedMeter      = metrics.NewRegisteredMeter("status/full/verified/messages/total", nil)
	statusUntrustedVerifiedMeter = metrics.NewRegisteredMeter("status/untrusted/verified/messages/total", nil)

	statusDiffHashMismatchMeter = metrics.NewRegisteredMeter("status/diffhash/mismatch/messages/total", nil)
	statusImpossibleForkMeter   = metrics.NewRegisteredMeter("status/impossible/fork/messages/total", nil)

	statusBlockTooNewMeter     = metrics.NewRegisteredMeter("status/block/too/new/messages/total", nil)
	statusBlockNewerMeter      = metrics.NewRegisteredMeter("status/block/newer/messages/total", nil)
	statusPossibleForkMeter    = metrics.NewRegisteredMeter("status/possible/fork/messages/total", nil)
	statusUnexpectedErrorMeter = metrics.NewRegisteredMeter("status/unexpected/error/total", nil)

	codeMap = map[uint16]metrics.Meter{
		0x101: statusFullVerifiedMeter,
		0x102: statusUntrustedVerifiedMeter,
		0x201: statusDiffHashMismatchMeter,
		0x202: statusImpossibleForkMeter,
		0x301: statusBlockTooNewMeter,
		0x302: statusBlockNewerMeter,
		0x303: statusPossibleForkMeter,
		0x400: statusUnexpectedErrorMeter,
	}
)

type remoteVerifyManager struct {
	bc            *BlockChain
	tasks         map[common.Hash]*verifyTask
	peers         verifyPeers
	verifiedCache *lru.Cache
	allowInsecure bool

	// Subscription
	chainHeadCh  chan ChainHeadEvent
	chainHeadSub event.Subscription

	// Channels
	verifyCh  chan common.Hash
	messageCh chan verifyMessage
}

func NewVerifyManager(blockchain *BlockChain, peers verifyPeers, allowUntrusted bool) *remoteVerifyManager {
	verifiedCache, _ := lru.New(verifiedCacheSize)
	vm := &remoteVerifyManager{
		bc:            blockchain,
		tasks:         make(map[common.Hash]*verifyTask),
		peers:         peers,
		verifiedCache: verifiedCache,
		allowInsecure: allowUntrusted,

		chainHeadCh: make(chan ChainHeadEvent, chainHeadChanSize),
		verifyCh:    make(chan common.Hash, maxForkHeight),
		messageCh:   make(chan verifyMessage),
	}
	vm.chainHeadSub = blockchain.SubscribeChainHeadEvent(vm.chainHeadCh)
	return vm
}

func (vm *remoteVerifyManager) mainLoop() {
	defer vm.chainHeadSub.Unsubscribe()

	// load unverified blocks in a normalized chain and start a batch of verify task
	header := vm.bc.CurrentHeader()
	// Start verify task from H to H-11 if need.
	vm.NewBlockVerifyTask(header)
	pruneTicker := time.NewTicker(pruneInterval)
	defer pruneTicker.Stop()
	for {
		select {
		case h := <-vm.chainHeadCh:
			vm.NewBlockVerifyTask(h.Block.Header())
		case hash := <-vm.verifyCh:
			vm.cacheBlockVerified(hash)
			if task, ok := vm.tasks[hash]; ok {
				delete(vm.tasks, hash)
				remoteVerifyTaskCounter.Dec(1)
				close(task.terminalCh)
			}
		case <-pruneTicker.C:
			for hash, task := range vm.tasks {
				if vm.bc.CurrentHeader().Number.Cmp(task.blockHeader.Number) == 1 &&
					vm.bc.CurrentHeader().Number.Uint64()-task.blockHeader.Number.Uint64() > pruneHeightDiff {
					delete(vm.tasks, hash)
					remoteVerifyTaskCounter.Dec(1)
					close(task.terminalCh)
				}
			}
		case message := <-vm.messageCh:
			if vt, ok := vm.tasks[message.verifyResult.BlockHash]; ok {
				vt.messageCh <- message
			}

		// System stopped
		case <-vm.bc.quit:
			for _, task := range vm.tasks {
				close(task.terminalCh)
			}
			return
		case <-vm.chainHeadSub.Err():
			return
		}
	}
}

func (vm *remoteVerifyManager) NewBlockVerifyTask(header *types.Header) {
	for i := 0; header != nil && i <= maxForkHeight; i++ {
		func(hash common.Hash) {
			// if verified cache record that this block has been verified, skip.
			if _, ok := vm.verifiedCache.Get(hash); ok {
				return
			}
			// if there already has a verify task for this block, skip.
			if _, ok := vm.tasks[hash]; ok {
				return
			}
			diffLayer := vm.bc.GetTrustedDiffLayer(hash)
			// if this block has no diff, there is no need to verify it.
			var err error
			if diffLayer == nil {
				if diffLayer, err = vm.bc.GenerateDiffLayer(hash); err != nil {
					log.Error("failed to get diff layer", "block", hash, "number", header.Number, "error", err)
					return
				} else {
					log.Info("this is an empty block:", "block", hash, "number", header.Number)
					return
				}
			}
			diffHash, err := CalculateDiffHash(diffLayer)
			if err != nil {
				log.Error("failed to get diff hash", "block", hash, "number", header.Number, "error", err)
				return
			}
			verifyTask := NewVerifyTask(diffHash, header, vm.peers, vm.verifyCh, vm.allowInsecure)
			vm.tasks[hash] = verifyTask
			remoteVerifyTaskCounter.Inc(1)
		}(header.Hash())
		header = vm.bc.GetHeaderByHash(header.ParentHash)
	}
}

func (vm *remoteVerifyManager) cacheBlockVerified(hash common.Hash) {
	if vm.verifiedCache.Len() >= verifiedCacheSize {
		vm.verifiedCache.RemoveOldest()
	}
	vm.verifiedCache.Add(hash, true)
}

// AncestorVerified function check block has been verified or it's a empty block.
func (vm *remoteVerifyManager) AncestorVerified(header *types.Header) bool {
	// find header of H-11 block.
	header = vm.bc.GetHeaderByNumber(header.Number.Uint64() - maxForkHeight)
	// If start from genesis block, there has not a H-11 block.
	if header == nil {
		return true
	}
	// check whether H-11 block is a empty block.
	if header.TxHash == types.EmptyRootHash {
		parent := vm.bc.GetHeaderByHash(header.ParentHash)
		return header.Root == parent.Root
	}
	hash := header.Hash()
	_, exist := vm.verifiedCache.Get(hash)
	return exist
}

func (vm *remoteVerifyManager) HandleRootResponse(vr *VerifyResult, pid string) error {
	vm.messageCh <- verifyMessage{verifyResult: vr, peerId: pid}
	return nil
}

type VerifyResult struct {
	Status      types.VerifyStatus
	BlockNumber uint64
	BlockHash   common.Hash
	Root        common.Hash
}

type verifyMessage struct {
	verifyResult *VerifyResult
	peerId       string
}

type verifyTask struct {
	diffhash       common.Hash
	blockHeader    *types.Header
	candidatePeers verifyPeers
	BadPeers       map[string]struct{}
	startAt        time.Time
	allowUntrusted bool

	messageCh  chan verifyMessage
	terminalCh chan struct{}
}

func NewVerifyTask(diffhash common.Hash, header *types.Header, peers verifyPeers, verifyCh chan common.Hash, allowUntrusted bool) *verifyTask {
	vt := &verifyTask{
		diffhash:       diffhash,
		blockHeader:    header,
		candidatePeers: peers,
		BadPeers:       make(map[string]struct{}),
		allowUntrusted: allowUntrusted,
		messageCh:      make(chan verifyMessage),
		terminalCh:     make(chan struct{}),
	}
	go vt.Start(verifyCh)
	return vt
}

func (vt *verifyTask) Start(verifyCh chan common.Hash) {
	vt.startAt = time.Now()

	vt.sendVerifyRequest(defaultPeerNumber)
	resend := time.NewTicker(resendInterval)
	defer resend.Stop()
	for {
		select {
		case msg := <-vt.messageCh:
			if metric, exist := codeMap[msg.verifyResult.Status.Code]; exist {
				metric.Mark(1)
			}
			switch msg.verifyResult.Status {
			case types.StatusFullVerified:
				statusFullVerifiedMeter.Mark(1)
				vt.compareRootHashAndWrite(msg, verifyCh)
			case types.StatusPartiallyVerified:
				statusUntrustedVerifiedMeter.Mark(1)
				log.Warn("block %s , num= %s is untrusted verified", msg.verifyResult.BlockHash, msg.verifyResult.BlockNumber)
				if vt.allowUntrusted {
					vt.compareRootHashAndWrite(msg, verifyCh)
				}
			case types.StatusUnexpectedError, types.StatusImpossibleFork, types.StatusDiffHashMismatch:
				vt.BadPeers[msg.peerId] = struct{}{}
				log.Info("peer %s is not available: code %d, msg %s,", msg.peerId, msg.verifyResult.Status.Code, msg.verifyResult.Status.Msg)
			case types.StatusBlockTooNew, types.StatusBlockNewer, types.StatusPossibleFork:
				log.Info("return msg from peer %s for block %s is %s", msg.peerId, msg.verifyResult.BlockHash, msg.verifyResult.Status.Msg)
			}
		case <-resend.C:
			// if a task has run over 15s, try all the vaild peers to verify.
			if time.Since(vt.startAt) < tryAllPeersTime {
				vt.sendVerifyRequest(1)
			} else {
				vt.sendVerifyRequest(-1)
			}
		case <-vt.terminalCh:
			return
		}
	}
}

// sendVerifyRequest func select at most n peers from (candidatePeers-badPeers) randomly and send verify request.
// when n<0, send to all the peers exclude badPeers.
func (vt *verifyTask) sendVerifyRequest(n int) {
	var validPeers []VerifyPeer
	candidatePeers := vt.candidatePeers.GetVerifyPeers()
	for _, p := range candidatePeers {
		if _, ok := vt.BadPeers[p.ID()]; !ok {
			validPeers = append(validPeers, p)
		}
	}
	// if has not valid peer, log warning.
	if len(validPeers) == 0 {
		log.Warn("there is no valid peer for block", vt.blockHeader.Number)
	}
	if n < 0 || n >= len(validPeers) {
		for _, p := range validPeers {
			p.RequestRoot(vt.blockHeader.Number.Uint64(), vt.blockHeader.Hash(), vt.diffhash)
		}
		return
	}

	// if n < len(validPeers), select n peers from validPeers randomly.
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(validPeers), func(i, j int) { validPeers[i], validPeers[j] = validPeers[j], validPeers[i] })
	for i := 0; i < n; i++ {
		p := validPeers[i]
		p.RequestRoot(vt.blockHeader.Number.Uint64(), vt.blockHeader.Hash(), vt.diffhash)
	}
}

func (vt *verifyTask) compareRootHashAndWrite(msg verifyMessage, verifyCh chan common.Hash) {
	if msg.verifyResult.Root == vt.blockHeader.Root {
		blockhash := msg.verifyResult.BlockHash
		// write back to manager so that manager can cache the result and delete this task.
		verifyCh <- blockhash
	} else {
		vt.BadPeers[msg.peerId] = struct{}{}
	}
}

type VerifyPeer interface {
	RequestRoot(blockNumber uint64, blockHash common.Hash, diffHash common.Hash) error
	ID() string
}

type verifyPeers interface {
	GetVerifyPeers() []VerifyPeer
}

type VerifyMode uint32

const (
	LocalVerify VerifyMode = iota
	FullVerify
	InsecureVerify
	NoneVerify
)

func (mode VerifyMode) IsValid() bool {
	return mode >= LocalVerify && mode <= NoneVerify
}

func (mode VerifyMode) String() string {
	switch mode {
	case LocalVerify:
		return "local"
	case FullVerify:
		return "full"
	case InsecureVerify:
		return "insecure"
	case NoneVerify:
		return "none"
	default:
		return "unknown"
	}
}

func (mode VerifyMode) MarshalText() ([]byte, error) {
	switch mode {
	case LocalVerify:
		return []byte("local"), nil
	case FullVerify:
		return []byte("full"), nil
	case InsecureVerify:
		return []byte("insecure"), nil
	case NoneVerify:
		return []byte("none"), nil
	default:
		return nil, fmt.Errorf("unknown verify mode %d", mode)
	}
}

func (mode *VerifyMode) UnmarshalText(text []byte) error {
	switch string(text) {
	case "local":
		*mode = LocalVerify
	case "full":
		*mode = FullVerify
	case "insecure":
		*mode = InsecureVerify
	case "none":
		*mode = NoneVerify
	default:
		return fmt.Errorf(`unknown sync mode %q, want "full", "light" or "insecure"`, text)
	}
	return nil
}

func (mode VerifyMode) NeedRemoteVerify() bool {
	return mode == FullVerify || mode == InsecureVerify
}
