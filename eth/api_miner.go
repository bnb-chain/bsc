// Copyright 2023 The go-ethereum Authors
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

package eth

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/params"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// MinerAPI provides an API to control the miner.
type MinerAPI struct {
	e *Ethereum
}

// NewMinerAPI creates a new MinerAPI instance.
func NewMinerAPI(e *Ethereum) *MinerAPI {
	return &MinerAPI{e}
}

// Start starts the miner with the given number of threads. If threads is nil,
// the number of workers started is equal to the number of logical CPUs that are
// usable by this process. If mining is already running, this method adjust the
// number of threads allowed to use and updates the minimum price required by the
// transaction pool.
func (api *MinerAPI) Start() error {
	return api.e.StartMining()
}

// Stop terminates the miner, both at the consensus engine level as well as at
// the block creation level.
func (api *MinerAPI) Stop() {
	api.e.StopMining()
}

// SetExtra sets the extra data string that is included when this miner mines a block.
func (api *MinerAPI) SetExtra(extra string) (bool, error) {
	if err := api.e.Miner().SetExtra([]byte(extra)); err != nil {
		return false, err
	}
	return true, nil
}

// SetGasPrice sets the minimum accepted gas price for the miner.
func (api *MinerAPI) SetGasPrice(gasPrice hexutil.Big) bool {
	api.e.lock.Lock()
	api.e.gasPrice = (*big.Int)(&gasPrice)
	api.e.lock.Unlock()

	api.e.txPool.SetGasTip((*big.Int)(&gasPrice))
	api.e.Miner().SetGasTip((*big.Int)(&gasPrice))
	return true
}

// SetGasLimit sets the gaslimit to target towards during mining.
func (api *MinerAPI) SetGasLimit(gasLimit hexutil.Uint64) bool {
	api.e.Miner().SetGasCeil(uint64(gasLimit))
	if uint64(gasLimit) > params.SystemTxsGas {
		api.e.TxPool().SetMaxGas(uint64(gasLimit) - params.SystemTxsGas)
	}
	return true
}

// SetEtherbase sets the etherbase of the miner.
func (api *MinerAPI) SetEtherbase(etherbase common.Address) bool {
	api.e.SetEtherbase(etherbase)
	return true
}

// SetRecommitInterval updates the interval for miner sealing work recommitting.
func (api *MinerAPI) SetRecommitInterval(interval int) {
	api.e.Miner().SetRecommitInterval(time.Duration(interval) * time.Millisecond)
}

// MevRunning returns true if the validator accept bids from builder
func (api *MinerAPI) MevRunning() bool {
	return api.e.APIBackend.MevRunning()
}

// StartMev starts mev. It notifies the miner to start to receive bids.
func (api *MinerAPI) StartMev() {
	api.e.APIBackend.StartMev()
}

// StopMev stops mev. It notifies the miner to stop receiving bids from this moment,
// but the bids before this moment would still been taken into consideration by mev.
func (api *MinerAPI) StopMev() {
	api.e.APIBackend.StopMev()
}

// AddBuilder adds a builder to the bid simulator.
// url is the endpoint of the builder, for example, "https://mev-builder.amazonaws.com",
// if validator is equipped with sentry, ignore the url.
func (api *MinerAPI) AddBuilder(builder common.Address, url string) error {
	return api.e.APIBackend.AddBuilder(builder, url)
}

// RemoveBuilder removes a builder from the bid simulator.
func (api *MinerAPI) RemoveBuilder(builder common.Address) error {
	return api.e.APIBackend.RemoveBuilder(builder)
}
