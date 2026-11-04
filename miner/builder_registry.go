package miner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/miner/minerconfig"
	"github.com/ethereum/go-ethereum/rpc"
)

// Matches BuilderKeyRegistry.Builder (addr, url, name), including the name field
// even though admission and issue reporting only need the first two fields.
const builderRegistryABI = `[{"type":"function","name":"getBuilders","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"tuple[]","components":[{"name":"addr","type":"address"},{"name":"url","type":"string"},{"name":"name","type":"string"}]}]},
{"type":"function","name":"defaultMaxKeys","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]}]`

const (
	builderRegistryTimeout     = 10 * time.Second
	builderRegistryMaxBlockAge = time.Minute
)

var (
	registryABI = func() abi.ABI {
		parsed, err := abi.JSON(strings.NewReader(builderRegistryABI))
		if err != nil {
			panic(err)
		}
		return parsed
	}()
	registrySyncedBlock  = metrics.NewRegisteredGauge("miner/registry/syncedBlock", nil)
	registryBuilderCount = metrics.NewRegisteredGauge("miner/registry/builderCount", nil)
	registryAllowAll     = metrics.NewRegisteredGauge("miner/registry/allowAll", nil)
	registryLastSuccess  = metrics.NewRegisteredGauge("miner/registry/lastSuccessTimestamp", nil)
)

type registryChain interface {
	CurrentFinalBlock() *types.Header
}

type registryCaller interface {
	CallContract(context.Context, rpc.BlockNumberOrHash, common.Address, []byte, uint64) ([]byte, error)
}

// builderRegistrySnapshot is immutable after publication. Admission mode and
// addresses must change together so readers cannot observe a partial update.
type builderRegistrySnapshot struct {
	blockNumber uint64
	blockHash   common.Hash
	allowAll    bool
	builders    map[common.Address]string // Last non-empty set also supplies report URLs in allow-all mode.
}

type builderRegistry struct {
	config   minerconfig.BuilderRegistryConfig
	chain    registryChain
	caller   registryCaller
	fallback map[common.Address]string
	snapshot atomic.Pointer[builderRegistrySnapshot]
}

func newBuilderRegistry(config minerconfig.BuilderRegistryConfig, builders []minerconfig.BuilderConfig, chain registryChain, caller registryCaller) *builderRegistry {
	if config.PollInterval == 0 {
		config.PollInterval = minerconfig.RegistryDuration(minerconfig.DefaultBuilderRegistryPollInterval)
	}
	r := &builderRegistry{config: config, chain: chain, caller: caller, fallback: make(map[common.Address]string)}
	for _, builder := range builders {
		r.fallback[builder.Address] = builder.URL
	}
	return r
}

func (r *builderRegistry) allowed(builder common.Address) bool {
	if snap := r.snapshot.Load(); snap != nil {
		_, listed := snap.builders[builder]
		return snap.allowAll || listed
	}
	_, listed := r.fallback[builder]
	return listed
}

func (r *builderRegistry) reportURL(builder common.Address) string {
	if snap := r.snapshot.Load(); snap != nil {
		return snap.builders[builder]
	}
	return r.fallback[builder]
}

func (r *builderRegistry) run(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	interval := time.Duration(r.config.PollInterval)
	retry := min(2*time.Second, interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			result := r.sync(ctx)
			timer.Reset(registryNextDelay(result, interval, &retry))
		}
	}
}

func registryNextDelay(result string, interval time.Duration, retry *time.Duration) time.Duration {
	if result == "error" {
		delay := *retry
		*retry = min(2*delay, time.Minute, interval)
		return delay
	}
	*retry = min(2*time.Second, interval)
	if result == "empty" {
		return min(3*time.Second, interval)
	}
	return interval
}

// sync is called by a single polling goroutine. A failure, including a backward
// finalized height or conflicting finalized hash, never changes admission.
func (r *builderRegistry) sync(ctx context.Context) string {
	result, err := r.refresh(ctx)
	if err != nil {
		result = "error"
		if ctx.Err() == nil {
			log.Warn("Builder registry sync failed; keeping current policy", "err", err)
		}
	}
	metrics.GetOrRegisterCounter("miner/registry/sync/"+result, nil).Inc(1)
	return result
}

func (r *builderRegistry) refresh(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, builderRegistryTimeout)
	defer cancel()
	if err := r.config.Validate(); err != nil {
		return "", err
	}
	if r.caller == nil {
		return "", errors.New("consensus engine does not support registry calls")
	}
	header := r.chain.CurrentFinalBlock()
	if header == nil || header.Number == nil || !header.Number.IsUint64() {
		return "", errors.New("finalized block unavailable")
	}
	// During catch-up, a successful read of an old non-empty registry would
	// otherwise delay the next refresh by a full normal polling interval.
	if age := time.Since(time.Unix(int64(header.Time), 0)); age > builderRegistryMaxBlockAge {
		return "", fmt.Errorf("builder registry finalized block is too old: age %s", age)
	}
	number, hash := header.Number.Uint64(), header.Hash()
	previous := r.snapshot.Load()
	if previous != nil && (number < previous.blockNumber || (number == previous.blockNumber && hash != previous.blockHash)) {
		return "", errors.New("builder registry finalized snapshot regressed or conflicted")
	}
	data, err := registryABI.Pack("getBuilders")
	if err != nil {
		return "", err
	}
	// Pin the call to the finalized hash and require canonical local state. Bound
	// gas as well as wall time: the registry is an upgradeable external contract.
	output, err := r.caller.CallContract(ctx, rpc.BlockNumberOrHashWithHash(hash, true), r.config.ContractAddress, data, 10_000_000)
	if err != nil {
		return "", fmt.Errorf("getBuilders: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(output) == 0 {
		return "", errors.New("empty getBuilders return data; check registry contract address")
	}
	var builders []struct {
		Addr common.Address
		Url  string
		Name string
	}
	if err := registryABI.UnpackIntoInterface(&builders, "getBuilders", output); err != nil {
		return "", fmt.Errorf("decode getBuilders: %w", err)
	}
	// The ABI decoder permits some noncanonical encodings (including all-zero
	// words). Require canonical output before treating an empty array as allow-all.
	canonical, err := registryABI.Methods["getBuilders"].Outputs.Pack(builders)
	if err != nil || !bytes.Equal(canonical, output) {
		return "", errors.New("noncanonical getBuilders return data")
	}
	// The implementation and an uninitialized proxy also return an empty list.
	// An initialized registry always has a positive defaultMaxKeys. Check it at
	// the same finalized hash before publishing either an empty or non-empty set.
	probe, err := registryABI.Pack("defaultMaxKeys")
	if err != nil {
		return "", err
	}
	capOutput, err := r.caller.CallContract(ctx, rpc.BlockNumberOrHashWithHash(hash, true), r.config.ContractAddress, probe, 10_000_000)
	if err != nil {
		return "", fmt.Errorf("defaultMaxKeys: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(capOutput) != 32 {
		return "", errors.New("invalid defaultMaxKeys return data; check registry contract address")
	}
	values, err := registryABI.Unpack("defaultMaxKeys", capOutput)
	if err != nil {
		return "", fmt.Errorf("decode defaultMaxKeys: %w", err)
	}
	if values[0].(*big.Int).Sign() == 0 {
		return "", errors.New("registry is not initialized; use an initialized BuilderKeyRegistry proxy")
	}
	snap := &builderRegistrySnapshot{blockNumber: number, blockHash: hash, builders: make(map[common.Address]string, len(builders))}
	for _, builder := range builders {
		if builder.Addr == (common.Address{}) {
			return "", errors.New("registry contains zero builder address")
		}
		if _, duplicate := snap.builders[builder.Addr]; duplicate {
			return "", errors.New("registry contains duplicate builder address")
		}
		snap.builders[builder.Addr] = builder.Url
	}
	result := "applied"
	if len(builders) == 0 {
		snap.allowAll = true
		snap.builders = r.fallback
		if previous != nil {
			snap.builders = previous.builders
		}
		result = "empty"
		log.Error("Builder registry is empty; all builder addresses are allowed", "block", number, "contract", r.config.ContractAddress)
	} else if previous != nil && !previous.allowAll && maps.Equal(previous.builders, snap.builders) {
		result = "unchanged"
	} else {
		log.Info("Builder registry allowlist applied", "block", number, "hash", hash, "builders", len(builders))
		if previous != nil && previous.allowAll {
			log.Warn("Builder registry leaving allow-all mode")
		}
	}
	r.snapshot.Store(snap)
	registrySyncedBlock.Update(int64(number))
	registryBuilderCount.Update(int64(len(builders)))
	var allowAll int64
	if snap.allowAll {
		allowAll = 1
	}
	registryAllowAll.Update(allowAll)
	registryLastSuccess.Update(time.Now().Unix())
	return result, nil
}
