package eth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
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
const builderRegistryABI = `[{"type":"function","name":"getBuilders","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"tuple[]","components":[{"name":"addr","type":"address"},{"name":"url","type":"string"},{"name":"name","type":"string"}]}]}]`

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
	registryLastSuccess  = metrics.NewRegisteredGauge("miner/registry/lastSuccessTimestamp", nil)
)

type registryChain interface {
	CurrentFinalBlock() *types.Header
}

type registryCallFunc func(ctx context.Context, block rpc.BlockNumberOrHash, to common.Address, data []byte, gas uint64) ([]byte, error)

// builderRegistrySnapshot is immutable after publication.
type builderRegistrySnapshot struct {
	blockNumber uint64
	blockHash   common.Hash
	builders    map[common.Address]string
}

type builderRegistry struct {
	config       minerconfig.BuilderRegistryConfig
	chain        registryChain
	readRegistry registryCallFunc
	apply        func(map[common.Address]string)
	last         *builderRegistrySnapshot // accessed only by the polling goroutine
	cancel       context.CancelFunc
	done         chan struct{}
}

func (r *builderRegistry) start() {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.done = make(chan struct{})
	go func() {
		defer close(r.done)
		r.run(ctx)
	}()
}

func (r *builderRegistry) stop() {
	if r.cancel != nil {
		r.cancel()
		<-r.done
	}
}

func (r *builderRegistry) run(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	interval := time.Duration(r.config.PollInterval)
	if interval == 0 {
		interval = minerconfig.DefaultBuilderRegistryPollInterval
	}
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
	header := r.chain.CurrentFinalBlock()
	if header == nil || header.Number == nil || !header.Number.IsUint64() {
		return "", errors.New("finalized block unavailable")
	}
	// During catch-up, a successful read of old registry state would
	// otherwise delay the next refresh by a full normal polling interval.
	if age := time.Since(time.Unix(int64(header.Time), 0)); age > builderRegistryMaxBlockAge {
		return "", fmt.Errorf("builder registry finalized block is too old: age %s", age)
	}
	number, hash := header.Number.Uint64(), header.Hash()
	previous := r.last
	if previous != nil && (number < previous.blockNumber || (number == previous.blockNumber && hash != previous.blockHash)) {
		return "", errors.New("builder registry finalized snapshot regressed or conflicted")
	}
	data, err := registryABI.Pack("getBuilders")
	if err != nil {
		return "", err
	}
	// Pin the call to the finalized hash and require canonical local state. Bound
	// gas as well as wall time: the registry is an upgradeable external contract.
	output, err := r.readRegistry(ctx, rpc.BlockNumberOrHashWithHash(hash, true), r.config.ContractAddress, data, 10_000_000)
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
	// words). Require canonical output before publishing the snapshot.
	canonical, err := registryABI.Methods["getBuilders"].Outputs.Pack(builders)
	if err != nil || !bytes.Equal(canonical, output) {
		return "", errors.New("noncanonical getBuilders return data")
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
	if previous != nil && maps.Equal(previous.builders, snap.builders) {
		result = "unchanged"
	} else {
		r.apply(snap.builders)
		log.Info("Builder registry allowlist applied", "block", number, "hash", hash, "builders", len(builders))
	}
	r.last = snap
	registrySyncedBlock.Update(int64(number))
	registryBuilderCount.Update(int64(len(builders)))
	registryLastSuccess.Update(time.Now().Unix())
	return result, nil
}
