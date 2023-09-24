package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"

	"github.com/google/pprof/profile"
)

func TestPrefetchLeaking(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var (
		gendb   = rawdb.NewMemoryDatabase()
		key, _  = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		address = crypto.PubkeyToAddress(key.PublicKey)
		funds   = big.NewInt(100000000000000000)
		gspec   = &Genesis{
			Config:  params.TestChainConfig,
			Alloc:   GenesisAlloc{address: {Balance: funds}},
			BaseFee: big.NewInt(params.InitialBaseFee),
		}
		triedb  = trie.NewDatabase(gendb, nil)
		genesis = gspec.MustCommit(gendb, triedb)
		signer  = types.LatestSigner(gspec.Config)
	)
	blocks, _ := GenerateChain(gspec.Config, genesis, ethash.NewFaker(), gendb, 1, func(i int, block *BlockGen) {
		block.SetCoinbase(common.Address{0x00})
		for j := 0; j < 100; j++ {
			tx, err := types.SignTx(types.NewTransaction(block.TxNonce(address), common.Address{0x00}, big.NewInt(1000), params.TxGas, block.header.BaseFee, nil), signer, key)
			if err != nil {
				panic(err)
			}
			block.AddTx(tx)
		}
	})
	archiveDb := rawdb.NewMemoryDatabase()
	gspec.MustCommit(archiveDb, triedb)
	archive, _ := NewBlockChain(archiveDb, nil, gspec, nil, ethash.NewFaker(), vm.Config{}, nil, nil)
	defer archive.Stop()

	block := blocks[0]
	parent := archive.GetHeader(block.ParentHash(), block.NumberU64()-1)
	statedb, _ := state.NewWithSharedPool(parent.Root, archive.stateCache, archive.snaps)
	inter := make(chan struct{})

	Track(ctx, t, func(ctx context.Context) {
		close(inter)
		go archive.prefetcher.Prefetch(block, statedb, &archive.vmConfig, inter)
		time.Sleep(1 * time.Second)
	})
}

func Track(ctx context.Context, t *testing.T, fn func(context.Context)) {
	label := t.Name()
	pprof.Do(ctx, pprof.Labels("test", label), fn)
	if err := CheckNoGoroutines("test", label); err != nil {
		t.Fatal("Leaked goroutines\n", err)
	}
}

func CheckNoGoroutines(key, value string) error {
	var pb bytes.Buffer
	profiler := pprof.Lookup("goroutine")
	if profiler == nil {
		return fmt.Errorf("unable to find profile")
	}
	err := profiler.WriteTo(&pb, 0)
	if err != nil {
		return fmt.Errorf("unable to read profile: %w", err)
	}

	p, err := profile.ParseData(pb.Bytes())
	if err != nil {
		return fmt.Errorf("unable to parse profile: %w", err)
	}

	return summarizeGoroutines(p, key, value)
}

func summarizeGoroutines(p *profile.Profile, key, expectedValue string) error {
	var b strings.Builder

	for _, sample := range p.Sample {
		if !matchesLabel(sample, key, expectedValue) {
			continue
		}

		fmt.Fprintf(&b, "count %d @", sample.Value[0])
		// format the stack trace for each goroutine
		for _, loc := range sample.Location {
			for i, ln := range loc.Line {
				if i == 0 {
					fmt.Fprintf(&b, "#   %#8x", loc.Address)
					if loc.IsFolded {
						fmt.Fprint(&b, " [F]")
					}
				} else {
					fmt.Fprint(&b, "#           ")
				}
				if fn := ln.Function; fn != nil {
					fmt.Fprintf(&b, " %-50s %s:%d", fn.Name, fn.Filename, ln.Line)
				} else {
					fmt.Fprintf(&b, " ???")
				}
				fmt.Fprintf(&b, "\n")
			}
		}
		fmt.Fprintf(&b, "\n")
	}

	if b.Len() == 0 {
		return nil
	}

	return errors.New(b.String())
}

func matchesLabel(sample *profile.Sample, key, expectedValue string) bool {
	values, hasLabel := sample.Label[key]
	if !hasLabel {
		return false
	}

	for _, value := range values {
		if value == expectedValue {
			return true
		}
	}

	return false
}
