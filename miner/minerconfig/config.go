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
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

var (
	defaultDelayLeftOver = 50 * time.Millisecond
	// default confogurations for MEV
	defaultGreedyMergeTx         bool   = true
	defaultValidatorCommission   uint64 = 100
	defaultBidSimulationLeftOver        = 50 * time.Millisecond
	defaultNoInterruptLeftOver          = 400 * time.Millisecond
	defaultMaxBidsPerBuilder     uint32 = 3
)

// Config is the configuration parameters of mining.
type Config struct {
	Etherbase             common.Address `toml:",omitempty"` // Public address for block mining rewards
	ExtraData             hexutil.Bytes  `toml:",omitempty"` // Block extra data set by the miner
	DelayLeftOver         *time.Duration `toml:",omitempty"` // Time reserved to finalize a block(calculate root, distribute income...)
	GasFloor              uint64         // Target gas floor for mined blocks.
	GasCeil               uint64         // Target gas ceiling for mined blocks.
	GasPrice              *big.Int       // Minimum gas price for mining a transaction
	Recommit              time.Duration  // The time interval for miner to re-create mining work.
	VoteEnable            bool           // Whether to vote when mining
	MaxWaitProposalInSecs uint64         // The maximum time to wait for the proposal to be done, it's aimed to prevent validator being slashed when restarting

	DisableVoteAttestation bool // Whether to skip assembling vote attestation

	Mev MevConfig // Mev configuration
}

// DefaultConfig contains default settings for miner.
var DefaultConfig = Config{
	GasCeil:  0,
	GasPrice: big.NewInt(params.GWei),

	// The default recommit time is chosen as two seconds since
	// consensus-layer usually will wait a half slot of time(6s)
	// for payload generation. It should be enough for Geth to
	// run 3 rounds.
	Recommit:      3 * time.Second,
	DelayLeftOver: &defaultDelayLeftOver,

	// The default value is set to 30 seconds.
	// Because the avg restart time in mainnet is around 30s, so the node try to wait for the next multi-proposals to be done.
	MaxWaitProposalInSecs: 30,

	Mev: DefaultMevConfig,
}

type BuilderConfig struct {
	Address common.Address
	URL     string
}

type MevConfig struct {
	Enabled               bool            // Whether to enable Mev or not
	GreedyMergeTx         *bool           `toml:",omitempty"` // Whether to merge local transactions to the bid
	BuilderFeeCeil        string          // The maximum builder fee of a bid
	SentryURL             string          // The url of Mev sentry
	Builders              []BuilderConfig // The list of builders
	ValidatorCommission   *uint64         `toml:",omitempty"` // 100 means the validator claims 1% from block reward
	BidSimulationLeftOver *time.Duration  `toml:",omitempty"`
	NoInterruptLeftOver   *time.Duration  `toml:",omitempty"`
	MaxBidsPerBuilder     *uint32         `toml:",omitempty"` // Maximum number of bids allowed per builder per block
}

var DefaultMevConfig = MevConfig{
	Enabled:               false,
	GreedyMergeTx:         &defaultGreedyMergeTx,
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

	// check [Eth.Miner.Mev]
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
