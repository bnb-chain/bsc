// Copyright 2014 The go-ethereum Authors
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

// Package miner implements Ethereum block creation and mining.
package minerconfig

import (
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

// Default timing configurations
var (
	defaultRecommit              = 10 * time.Second
	defaultMaxWaitProposalInSecs = uint64(45)

	// Extra time for finalizing and committing blocks (excludes writing to disk).
	defaultDelayLeftOver         = 25 * time.Millisecond
	defaultBidSimulationLeftOver = 30 * time.Millisecond
	// For estimation, assume 500 Mgas/s:
	//	(100M gas / 500 Mgas/s) * 1000 ms + 10 ms buffer + defaultDelayLeftOver ≈ 235 ms.
	defaultNoInterruptLeftOver = 235 * time.Millisecond
)

// Other default MEV-related configurations
var (
	defaultMevEnabled          = false
	defaultGreedyMergeTx       = true
	defaultBuilderFeeCeil      = "0"
	defaultValidatorCommission = uint64(100)
	defaultMaxBidsPerBuilder   = uint32(2) // Simple strategy: send one bid early, another near deadline
)

// Config is the configuration parameters of mining.
type Config struct {
	Etherbase              common.Address `toml:",omitempty"` // Public address for block mining rewards
	ExtraData              hexutil.Bytes  `toml:",omitempty"` // Block extra data set by the miner
	DelayLeftOver          *time.Duration `toml:",omitempty"` // Time reserved to finalize a block(calculate root, distribute income...)
	GasFloor               uint64         // Target gas floor for mined blocks.
	GasCeil                uint64         // Target gas ceiling for mined blocks.
	GasPrice               *big.Int       // Minimum gas price for mining a transaction
	Recommit               *time.Duration `toml:",omitempty"` // The time interval for miner to re-create mining work.
	VoteEnable             bool           // Whether to vote when mining
	MaxWaitProposalInSecs  *uint64        `toml:",omitempty"` // The maximum time to wait for the proposal to be done, it's aimed to prevent validator being slashed when restarting
	DisableVoteAttestation bool           // Whether to skip assembling vote attestation
	TxGasLimit             uint64         // Maximum gas for per transaction

	Mev MevConfig // Mev configuration
}

// DefaultConfig contains default settings for miner.
var DefaultConfig = Config{
	GasCeil:  100000000,
	GasPrice: big.NewInt(params.GWei),
	// The default recommit time is chosen as two seconds since
	// consensus-layer usually will wait a half slot of time(6s)
	// for payload generation. It should be enough for Geth to
	// run 3 rounds.
	Recommit:      &defaultRecommit,
	DelayLeftOver: &defaultDelayLeftOver,

	// The default value is set to 45 seconds.
	// Because the avg restart time in mainnet could be 30+ seconds, so the node try to wait for the next multi-proposals to be done.
	MaxWaitProposalInSecs: &defaultMaxWaitProposalInSecs,

	Mev: DefaultMevConfig,
}

type BuilderConfig struct {
	Address common.Address
	URL     string
	Name    string `toml:",omitempty"` // Builder name, if empty will use domain suffix from URL
}

// GetBuilderName returns the builder name for this config
// If Name is set, use it; otherwise extract from URL domain
func (bc *BuilderConfig) GetBuilderName() string {
	if bc.Name != "" {
		return bc.Name
	}
	if bc.URL != "" {
		return extractDomainSuffix(bc.URL)
	}
	// If no URL, return address as string
	return bc.Address.Hex()
}

// extractDomainSuffix extracts the domain suffix from a URL
// e.g., "https://tokyo.builder.blockrazor.io" -> "blockrazor"
func extractDomainSuffix(url string) string {
	// Remove protocol
	if idx := strings.Index(url, "://"); idx != -1 {
		url = url[idx+3:]
	}

	// Remove port if present
	if idx := strings.Index(url, ":"); idx != -1 {
		url = url[:idx]
	}

	// Remove path
	if idx := strings.Index(url, "/"); idx != -1 {
		url = url[:idx]
	}

	// Split by dots
	parts := strings.Split(url, ".")

	// If length >= 2, return the second to last part
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}

	// For invalid formats, return the original URL
	return url
}

type MevConfig struct {
	Enabled               *bool           `toml:",omitempty"` // Whether to enable Mev or not
	GreedyMergeTx         *bool           `toml:",omitempty"` // Whether to merge local transactions to the bid
	BuilderFeeCeil        *string         `toml:",omitempty"` // The maximum builder fee of a bid
	SentryURL             string          // The url of Mev sentry
	Builders              []BuilderConfig // The list of builders
	ValidatorCommission   *uint64         `toml:",omitempty"` // 100 means the validator claims 1% from block reward
	BidSimulationLeftOver *time.Duration  `toml:",omitempty"`
	NoInterruptLeftOver   *time.Duration  `toml:",omitempty"`
	MaxBidsPerBuilder     *uint32         `toml:",omitempty"` // Maximum number of bids allowed per builder per block
}

// GetBuilderName returns the builder name for the given address
func (mc *MevConfig) GetBuilderName(builder common.Address) string {
	for _, builderConfig := range mc.Builders {
		if builderConfig.Address == builder {
			return builderConfig.GetBuilderName()
		}
	}
	// If no mapping found, return the address as string
	return builder.Hex()
}

var DefaultMevConfig = MevConfig{
	Enabled:               &defaultMevEnabled,
	GreedyMergeTx:         &defaultGreedyMergeTx,
	BuilderFeeCeil:        &defaultBuilderFeeCeil,
	SentryURL:             "",
	Builders:              nil,
	ValidatorCommission:   &defaultValidatorCommission,
	BidSimulationLeftOver: &defaultBidSimulationLeftOver,
	NoInterruptLeftOver:   &defaultNoInterruptLeftOver,
	MaxBidsPerBuilder:     &defaultMaxBidsPerBuilder,
}

func ApplyDefaultMinerConfig(cfg *Config) {
	if cfg == nil {
		log.Warn("ApplyDefaultMinerConfig cfg == nil")
		return
	}

	// check [Eth.Miner]
	if cfg.DelayLeftOver == nil {
		cfg.DelayLeftOver = &defaultDelayLeftOver
		log.Info("ApplyDefaultMinerConfig", "DelayLeftOver", *cfg.DelayLeftOver)
	}
	if cfg.MaxWaitProposalInSecs == nil {
		cfg.MaxWaitProposalInSecs = &defaultMaxWaitProposalInSecs
		log.Info("ApplyDefaultMinerConfig", "MaxWaitProposalInSecs", *cfg.MaxWaitProposalInSecs)
	}
	if cfg.Recommit == nil {
		cfg.Recommit = &defaultRecommit
		log.Info("ApplyDefaultMinerConfig", "Recommit", *cfg.Recommit)
	}

	// check [Eth.Miner.Mev]
	if cfg.Mev.Enabled == nil {
		cfg.Mev.Enabled = &defaultMevEnabled
		log.Info("ApplyDefaultMinerConfig", "Mev.Enabled", *cfg.Mev.Enabled)
	}
	if cfg.Mev.BuilderFeeCeil == nil {
		cfg.Mev.BuilderFeeCeil = &defaultBuilderFeeCeil
		log.Info("ApplyDefaultMinerConfig", "Mev.BuilderFeeCeil", *cfg.Mev.BuilderFeeCeil)
	}
	if cfg.Mev.GreedyMergeTx == nil {
		cfg.Mev.GreedyMergeTx = &defaultGreedyMergeTx
		log.Info("ApplyDefaultMinerConfig", "Mev.GreedyMergeTx", *cfg.Mev.GreedyMergeTx)
	}
	if cfg.Mev.ValidatorCommission == nil {
		cfg.Mev.ValidatorCommission = &defaultValidatorCommission
		log.Info("ApplyDefaultMinerConfig", "Mev.ValidatorCommission", *cfg.Mev.ValidatorCommission)
	}
	if cfg.Mev.BidSimulationLeftOver == nil {
		cfg.Mev.BidSimulationLeftOver = &defaultBidSimulationLeftOver
		log.Info("ApplyDefaultMinerConfig", "Mev.BidSimulationLeftOver", *cfg.Mev.BidSimulationLeftOver)
	}
	if cfg.Mev.NoInterruptLeftOver == nil {
		cfg.Mev.NoInterruptLeftOver = &defaultNoInterruptLeftOver
		log.Info("ApplyDefaultMinerConfig", "Mev.NoInterruptLeftOver", *cfg.Mev.NoInterruptLeftOver)
	}
	if cfg.Mev.MaxBidsPerBuilder == nil {
		cfg.Mev.MaxBidsPerBuilder = &defaultMaxBidsPerBuilder
		log.Info("ApplyDefaultMinerConfig", "Mev.MaxBidsPerBuilder", *cfg.Mev.MaxBidsPerBuilder)
	}
}
