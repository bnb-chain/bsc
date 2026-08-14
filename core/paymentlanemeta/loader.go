package paymentlanemeta

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

var loadMetaCache metaCache

// LoadMeta returns parent-pinned lane metadata. Cache hits reuse the shared Meta directly;
// misses repopulate it through the PaymentLane getters on the StateDB witness-visible path.
// The cache key comes from 0x2007's account in the supplied StateDB, so callers must pass a
// block state that is still opened on the parent root and not yet advanced by execution.
func LoadMeta(config *params.ChainConfig, header *types.Header, statedb *state.StateDB) (*Meta, error) {
	if err := statedb.Error(); err != nil {
		return nil, fmt.Errorf("%w: payment lane state read: %w", paymentlane.ErrStateUnavailable, err)
	}
	if !canCacheMeta(statedb) {
		return loadMetaFromStateDB(config, header, statedb)
	}
	key := metaCacheKeyFromStateDB(statedb)
	return loadMetaCache.loadOrStore(key, func() (*Meta, error) {
		return loadMetaFromStateDB(config, header, statedb)
	})
}

func loadMetaFromStateDB(config *params.ChainConfig, header *types.Header, statedb *state.StateDB) (*Meta, error) {
	params, err := loadParamsFromStateDB(config, header, statedb)
	if err != nil {
		return nil, err
	}
	listed, err := loadListedFromStateDB(config, header, statedb)
	if err != nil {
		return nil, err
	}
	return &Meta{params: params, listed: listed}, nil
}

// LoadParamsForQuota reads only the params needed for lane-size verification from a StateDB
// that is already opened on the parent post-state root.
func LoadParamsForQuota(config *params.ChainConfig, parent, header *types.Header, statedb *state.StateDB) (paymentlane.Params, error) {
	return loadParamsFromParentState(config, parent, header, statedb)
}

func loadParamsFromStateDB(config *params.ChainConfig, header *types.Header, statedb *state.StateDB) (paymentlane.Params, error) {
	ret, err := callFromStateDB(config, header, statedb, packGetPaymentLaneParams())
	if err != nil {
		return paymentlane.Params{}, err
	}
	return unpackGetPaymentLaneParams(ret)
}

func loadParamsFromParentState(config *params.ChainConfig, parent, header *types.Header, statedb *state.StateDB) (paymentlane.Params, error) {
	ret, err := callFromParentState(config, parent, header, statedb, packGetPaymentLaneParams())
	if err != nil {
		return paymentlane.Params{}, err
	}
	return unpackGetPaymentLaneParams(ret)
}

func loadListedFromStateDB(config *params.ChainConfig, header *types.Header, statedb *state.StateDB) (map[common.Address]struct{}, error) {
	ret, err := callFromStateDB(config, header, statedb, packGetPaymentContracts(0, pageSize))
	if err != nil {
		return nil, err
	}
	page, total, err := unpackGetPaymentContracts(ret)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return nil, nil
	}
	if len(page) == 0 {
		return nil, fmt.Errorf("%w: getPaymentContracts returned an empty first page for totalLength %d", paymentlane.ErrCorruptConfig, total)
	}
	listed := make(map[common.Address]struct{})
	if err := appendPage(listed, 0, page, total); err != nil {
		return nil, err
	}
	for offset := uint64(len(page)); offset < total; {
		ret, err := callFromStateDB(config, header, statedb, packGetPaymentContracts(offset, pageSize))
		if err != nil {
			return nil, err
		}
		page, nextTotal, err := unpackGetPaymentContracts(ret)
		if err != nil {
			return nil, err
		}
		if nextTotal != total {
			return nil, fmt.Errorf("%w: getPaymentContracts totalLength changed from %d to %d", paymentlane.ErrCorruptConfig, total, nextTotal)
		}
		if len(page) == 0 {
			return nil, fmt.Errorf("%w: getPaymentContracts returned an empty page at offset %d of %d", paymentlane.ErrCorruptConfig, offset, total)
		}
		if err := appendPage(listed, offset, page, total); err != nil {
			return nil, err
		}
		offset += uint64(len(page))
	}
	if uint64(len(listed)) != total {
		return nil, fmt.Errorf("%w: listed set size %d, want %d", paymentlane.ErrCorruptConfig, len(listed), total)
	}
	return listed, nil
}

func appendPage(listed map[common.Address]struct{}, offset uint64, page []common.Address, total uint64) error {
	if offset > total {
		return fmt.Errorf("%w: page offset %d exceeds totalLength %d", paymentlane.ErrCorruptConfig, offset, total)
	}
	if offset+uint64(len(page)) > total {
		return fmt.Errorf("%w: page offset %d length %d exceeds totalLength %d", paymentlane.ErrCorruptConfig, offset, len(page), total)
	}
	for i, addr := range page {
		if _, dup := listed[addr]; dup {
			return fmt.Errorf("%w: getPaymentContracts duplicate %x at %d", paymentlane.ErrCorruptConfig, addr, offset+uint64(i))
		}
		listed[addr] = struct{}{}
	}
	return nil
}
