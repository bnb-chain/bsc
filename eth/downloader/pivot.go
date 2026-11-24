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
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/protocols/snap"
	"github.com/ethereum/go-ethereum/params"
)

var errFrozenPivotUnavailable = errors.New("cannot establish frozen pivot ancestry")

// ErrPivotBehind signals that the selected target is below the snap checkpoint.
var ErrPivotBehind = errors.New("snap sync target is behind the persisted pivot")

const (
	checkpointRetryTargets = 3
	checkpointRetryDelay   = 10 * time.Minute
)

type checkpointRetry struct {
	hash    common.Hash
	since   time.Time
	targets map[common.Hash]struct{}
}

// selectSnapPivot runs before starting a state sync cycle. Only the downloader
// decides when a peer-driven target may replace a higher snap checkpoint.
func (d *Downloader) selectSnapPivot(head, candidate *types.Header, now time.Time) (*types.Header, error) {
	pivot, err := d.resumeFrozenPivot(head, candidate)
	if err != nil {
		return nil, err
	}
	syncer, ok := d.snapSyncer.(interface{ CheckpointPivot() *types.Header })
	if !ok {
		return pivot, nil // snap/1 has no checkpoint pivot.
	}
	checkpoint := syncer.CheckpointPivot()
	if checkpoint == nil || pivot.Number.Cmp(checkpoint.Number) >= 0 {
		d.pivotRetry = checkpointRetry{}
		return pivot, nil
	}
	retry := &d.pivotRetry
	if hash := checkpoint.Hash(); retry.targets == nil || retry.hash != hash {
		*retry = checkpointRetry{hash: hash, since: now, targets: make(map[common.Hash]struct{})}
	}
	if len(retry.targets) < checkpointRetryTargets {
		retry.targets[pivot.Hash()] = struct{}{}
	}
	if len(retry.targets) < checkpointRetryTargets || now.Sub(retry.since) < checkpointRetryDelay {
		return nil, fmt.Errorf("%w: target %d, checkpoint %d", ErrPivotBehind, pivot.Number, checkpoint.Number)
	}
	return pivot, nil
}

// movePivot is shared by both staleness paths. A delayed header response must
// not overwrite a newer choice made by the content processor, or a frozen pivot.
func (d *Downloader) movePivot(from, to *types.Header) bool {
	d.pivotLock.Lock()
	defer d.pivotLock.Unlock()
	if d.committed.Load() || d.pivotHeader.Hash() != from.Hash() || d.snapSyncer.FrozenPivot() != nil {
		return false
	}
	d.pivotHeader = to
	rawdb.WriteLastPivotNumber(d.stateDB, to.Number.Uint64())
	return true
}

// resumeFrozenPivot checks the frozen header against the selected peer's hash
// chain. Local canonical indices can trail the pivot and cannot decide this.
// Missing ancestry aborts the attempt without discarding the sync checkpoint.
func (d *Downloader) resumeFrozenPivot(head, pivot *types.Header) (*types.Header, error) {
	frozen := d.snapSyncer.FrozenPivot()
	if frozen == nil {
		return pivot, nil
	}
	if head.Number.Cmp(frozen.Number) < 0 {
		return pivot, nil
	}
	if snap.CatchUpExceedsRetention(frozen, pivot) {
		// The state syncer will discard this expired checkpoint under the same
		// policy. Selecting the fresh pivot avoids retrying an unbounded ancestry
		// probe forever after a long shutdown; this is not a reorg conclusion.
		return pivot, nil
	}
	// Bound the ancestry probe independently of BAL catch-up. Exceeding this
	// bound is not proof of a reorg and must not discard a frozen checkpoint.
	if head.Number.Uint64()-frozen.Number.Uint64() > params.FullImmutabilityThreshold+uint64(fsMinFullBlocks) {
		return nil, fmt.Errorf("%w: head %d too far above pivot %d", errFrozenPivotUnavailable, head.Number, frozen.Number)
	}
	header := head
	for header.Number.Cmp(frozen.Number) > 0 {
		var err error
		header, err = d.queryHeader(header.ParentHash, header.Number.Uint64()-1, nil)
		if err != nil {
			return nil, err
		}
	}
	if header.Hash() != frozen.Hash() {
		return pivot, nil // A complete ancestry walk confirmed a different fork.
	}
	return frozen, nil
}

// queryHeader resolves a header by hash, including headers not yet persisted by
// InsertReceiptChain. Reverse batches amortize ancestry walks; the bounded cache
// is content-addressed and never changes canonical indices or on-disk progress.
func (d *Downloader) queryHeader(hash common.Hash, number uint64, cancel <-chan struct{}) (*types.Header, error) {
	select {
	case <-cancel:
		return nil, snap.ErrCancelled
	case <-d.cancelCh:
		return nil, errCanceled
	default:
	}
	if header := rawdb.ReadHeader(d.stateDB, hash, number); header != nil {
		return header, nil
	}
	if header, ok := d.headerCache.Get(hash); ok && header.Number.Uint64() == number {
		return header, nil
	}
	peer := d.peers.Peer(d.cancelPeer)
	if peer == nil {
		return nil, errUnknownPeer
	}
	count := MaxHeaderFetch
	if number < uint64(count-1) {
		count = int(number) + 1
	}
	headers, _, err := d.fetchHeadersByHash(peer, hash, count, 0, true, cancel)
	if err != nil {
		return nil, err
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("missing sync header %d (%s)", number, hash)
	}
	if len(headers) > count {
		return nil, fmt.Errorf("%w: too many sync headers", errBadPeer)
	}
	want := hash
	for i, header := range headers {
		if header == nil || header.Number == nil || !header.Number.IsUint64() || header.Number.Uint64() != number-uint64(i) || header.Hash() != want {
			return nil, fmt.Errorf("%w: invalid sync ancestry at header %d", errBadPeer, i)
		}
		if err := header.SanityCheck(); err != nil {
			return nil, fmt.Errorf("%w: invalid sync header: %v", errBadPeer, err)
		}
		want = header.ParentHash
	}
	for _, header := range headers {
		d.headerCache.Add(header.Hash(), header)
	}
	return headers[0], nil
}
