package paymentlanemeta

import (
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/paymentlane"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

var loadMetaCache metaCache

// LoadMeta returns parent-pinned lane metadata, reading it through the PaymentLane getters on a
// miss. The cache key is 0x2007's account in the supplied StateDB, so callers MUST pass a block
// state of the parent block post-execution.
func LoadMeta(config *params.ChainConfig, header *types.Header, statedb *state.StateDB) (*Meta, error) {
	cacheable := canCacheMeta(statedb)
	if err := statedb.Error(); err != nil {
		err = fmt.Errorf("%w: payment lane state read: %w", paymentlane.ErrStateUnavailable, err)
		logMetaLoadFailure(header, statedb, cacheable, nil, err)
		return nil, err
	}
	if !cacheable {
		meta, err := loadMetaFromStateDB(config, header, statedb)
		if err != nil {
			logMetaLoadFailure(header, statedb, false, nil, err)
		}
		return meta, err
	}
	key := metaCacheKeyFromStateDB(statedb)
	return loadMetaCache.loadOrStore(key, func() (*Meta, error) {
		meta, err := loadMetaFromStateDB(config, header, statedb)
		if err != nil {
			logMetaLoadFailure(header, statedb, true, &key, err)
		}
		return meta, err
	})
}

func loadMetaFromStateDB(config *params.ChainConfig, header *types.Header, statedb *state.StateDB) (*Meta, error) {
	start := time.Now()

	ratio, err := loadRatioFromStateDB(config, header, statedb)
	if err != nil {
		return nil, err
	}
	listed, err := loadListedFromStateDB(config, header, statedb)
	if err != nil {
		return nil, err
	}
	log.Info("Loaded payment lane metadata", "elapsed", time.Since(start), "ratio", ratio, "listed", len(listed))
	return &Meta{ratio: ratio, listed: listed}, nil
}

func loadRatioFromStateDB(config *params.ChainConfig, header *types.Header, statedb *state.StateDB) (uint64, error) {
	ret, err := callGetter(config, header, statedb, packGetPaymentLaneRatio())
	if err != nil {
		return 0, err
	}
	return unpackGetPaymentLaneRatio(ret)
}

func loadListedFromStateDB(config *params.ChainConfig, header *types.Header, statedb *state.StateDB) (map[common.Address]struct{}, error) {
	ret, err := callGetter(config, header, statedb, packGetPaymentContracts(0, pageSize))
	if err != nil {
		return nil, err
	}
	page, total, err := unpackGetPaymentContracts(ret)
	if err != nil {
		return nil, err
	}
	if total > paymentlane.MaxListedContracts {
		return nil, fmt.Errorf("%w: getPaymentContracts totalLength %d exceeds limit %d", paymentlane.ErrCorruptConfig, total, paymentlane.MaxListedContracts)
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
		ret, err := callGetter(config, header, statedb, packGetPaymentContracts(offset, pageSize))
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
	if uint64(len(page)) > pageSize {
		return fmt.Errorf("%w: getPaymentContracts page at offset %d returned %d entries, limit %d", paymentlane.ErrCorruptConfig, offset, len(page), pageSize)
	}
	if offset > total {
		return fmt.Errorf("%w: page offset %d exceeds totalLength %d", paymentlane.ErrCorruptConfig, offset, total)
	}
	if uint64(len(page)) > total-offset {
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

func logMetaLoadFailure(header *types.Header, statedb *state.StateDB, cacheable bool, key *metaCacheKey, err error) {
	scheme := "mpt"
	if statedb.Database().Type().Is(state.TypeUBT) {
		scheme = "ubt"
	}
	category := "unexpected"
	switch {
	case errors.Is(err, paymentlane.ErrStateUnavailable):
		category = "stateUnavailable"
	case errors.Is(err, paymentlane.ErrCorruptConfig):
		category = "corruptConfig"
	}
	ctx := []any{
		"number", header.Number,
		"time", header.Time,
		"gasLimit", header.GasLimit,
		"cacheable", cacheable,
		"stateScheme", scheme,
		"noTries", statedb.NoTries(),
		"hasWitness", statedb.Witness() != nil,
		"category", category,
		"err", err,
	}
	if key != nil {
		ctx = append(ctx, "codeHash", key.codeHash, "storageRoot", key.storageRoot)
	}
	log.Error("Payment lane metadata load failed", ctx...)
}
