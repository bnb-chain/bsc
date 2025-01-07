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
	"github.com/ethereum/go-ethereum/params"
)

// Config is the configuration parameters of mining.
type Config struct {
	Etherbase             common.Address `toml:",omitempty"` // Public address for block mining rewards
	ExtraData             hexutil.Bytes  `toml:",omitempty"` // Block extra data set by the miner
	DelayLeftOver         time.Duration  // Time reserved to finalize a block(calculate root, distribute income...)
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
	DelayLeftOver: 50 * time.Millisecond,

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
	GreedyMergeTx         bool            // Whether to merge local transactions to the bid
	BuilderFeeCeil        string          // The maximum builder fee of a bid
	SentryURL             string          // The url of Mev sentry
	Builders              []BuilderConfig // The list of builders
	ValidatorCommission   uint64          // 100 means the validator claims 1% from block reward
	BidSimulationLeftOver time.Duration
}

var DefaultMevConfig = MevConfig{
	Enabled:               false,
	SentryURL:             "",
	Builders:              nil,
	ValidatorCommission:   100,
	BidSimulationLeftOver: 50 * time.Millisecond,
}
