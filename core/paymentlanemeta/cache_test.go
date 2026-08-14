package paymentlanemeta

import (
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/systemcontracts/gauss"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/hashdb"
	"github.com/stretchr/testify/require"
)

func TestMetaCacheReusesLastSuccess(t *testing.T) {
	var cache metaCache
	key := metaCacheKey{codeHash: common.Hash{31: 1}, storageRoot: common.Hash{31: 2}}
	want := &Meta{params: paymentlane.Params{MinGas: 2_000_000}}

	var loads int32
	got1, err := cache.loadOrStore(key, func() (*Meta, error) {
		atomic.AddInt32(&loads, 1)
		return want, nil
	})
	require.NoError(t, err)

	got2, err := cache.loadOrStore(key, func() (*Meta, error) {
		atomic.AddInt32(&loads, 1)
		return &Meta{}, nil
	})
	require.NoError(t, err)
	require.Same(t, got1, got2)
	require.Same(t, want, got2)
	require.EqualValues(t, 1, atomic.LoadInt32(&loads))
}

func TestMetaCacheDeduplicatesConcurrentMisses(t *testing.T) {
	var cache metaCache
	key := metaCacheKey{codeHash: common.Hash{31: 3}, storageRoot: common.Hash{31: 4}}
	want := &Meta{params: paymentlane.Params{MinGas: 3_000_000}}

	start := make(chan struct{})
	release := make(chan struct{})
	var loads int32
	var wg sync.WaitGroup
	results := make([]*Meta, 16)
	errs := make([]error, len(results))

	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = cache.loadOrStore(key, func() (*Meta, error) {
				if atomic.AddInt32(&loads, 1) == 1 {
					close(release)
				}
				<-release
				return want, nil
			})
		}(i)
	}
	close(start)
	wg.Wait()

	require.EqualValues(t, 1, atomic.LoadInt32(&loads))
	for i := range results {
		require.NoError(t, errs[i])
		require.Same(t, want, results[i])
	}
}

func TestMetaCacheDoesNotStickErrors(t *testing.T) {
	var cache metaCache
	key := metaCacheKey{codeHash: common.Hash{31: 5}, storageRoot: common.Hash{31: 6}}
	wantErr := errors.New("boom")

	var loads int32
	got, err := cache.loadOrStore(key, func() (*Meta, error) {
		atomic.AddInt32(&loads, 1)
		return nil, wantErr
	})
	require.Nil(t, got)
	require.ErrorIs(t, err, wantErr)

	want := &Meta{params: paymentlane.Params{MinGas: 4_000_000}}
	got, err = cache.loadOrStore(key, func() (*Meta, error) {
		atomic.AddInt32(&loads, 1)
		return want, nil
	})
	require.NoError(t, err)
	require.Same(t, want, got)
	require.EqualValues(t, 2, atomic.LoadInt32(&loads))
}

func TestCanCacheMetaAcceptsCleanMPTState(t *testing.T) {
	statedb := deployedContractState(t)
	require.True(t, canCacheMeta(statedb))
}

func TestCanCacheMetaRejectsWitnessState(t *testing.T) {
	statedb := deployedContractState(t)
	witness, err := stateless.NewWitness(laneHeader(60_000_000), nil, false)
	require.NoError(t, err)
	statedb.StartPrefetcher("test", witness)
	defer statedb.StopPrefetcher()
	require.False(t, canCacheMeta(statedb))
}

func TestCanCacheMetaRejectsNoTriesState(t *testing.T) {
	disk := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(disk, &triedb.Config{NoTries: true, HashDB: hashdb.Defaults})
	statedb, err := state.NewWithReader(types.EmptyRootHash, state.NewDatabase(tdb, nil), stubReader{})
	require.NoError(t, err)
	require.False(t, canCacheMeta(statedb))
}

func TestCanCacheMetaRejectsUBTState(t *testing.T) {
	disk := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(disk, triedb.UBTDefaults)
	statedb, err := state.New(types.EmptyBinaryHash, state.NewDatabase(tdb, nil))
	require.NoError(t, err)
	require.False(t, canCacheMeta(statedb))
}

func TestLoadMetaCacheHitStillFailsOnStickyStateError(t *testing.T) {
	loadMetaCache = metaCache{}

	db := state.NewDatabaseForTesting()
	statedb, err := state.New(types.EmptyRootHash, db)
	require.NoError(t, err)

	code, err := hex.DecodeString(strings.TrimSpace(gauss.RialtoPaymentLaneContract))
	require.NoError(t, err)
	statedb.SetCode(paymentlane.ContractAddress, code, tracing.CodeChangeSystemContractUpgrade)

	root, err := statedb.Commit(1, false, false)
	require.NoError(t, err)

	good, err := state.New(root, db)
	require.NoError(t, err)
	_, err = LoadMeta(params.BSCChainConfig, laneHeader(60_000_000), good)
	require.NoError(t, err)

	reader, err := db.Reader(root)
	require.NoError(t, err)
	badAddr := common.Address{0xaa}
	live, err := state.NewWithReader(root, db, faultingReader{Reader: reader, badAddr: badAddr, err: errors.New("boom")})
	require.NoError(t, err)

	live.GetCodeHash(badAddr)
	_, err = LoadMeta(params.BSCChainConfig, laneHeader(60_000_001), live)
	require.ErrorIs(t, err, paymentlane.ErrStateUnavailable)
}

type faultingReader struct {
	state.Reader
	badAddr common.Address
	err     error
}

func (r faultingReader) Account(addr common.Address) (*types.StateAccount, error) {
	if addr == r.badAddr {
		return nil, r.err
	}
	return r.Reader.Account(addr)
}

type stubReader struct{}

func (stubReader) Has(common.Address, common.Hash) bool                { return false }
func (stubReader) Code(common.Address, common.Hash) []byte             { return nil }
func (stubReader) CodeSize(common.Address, common.Hash) int            { return 0 }
func (stubReader) Account(common.Address) (*types.StateAccount, error) { return nil, nil }
func (stubReader) Storage(common.Address, common.Hash) (common.Hash, error) {
	return common.Hash{}, nil
}
