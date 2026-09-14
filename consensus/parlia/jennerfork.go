package parlia

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
)

// checkMaintenance issues the BEP-714 system transaction on the last block of each epoch.
// It runs after slashing and daily validator settlement, so the next epoch header selects
// validators from the resulting maintenance state.
func (p *Parlia) checkMaintenance(chain consensus.ChainHeaderReader, state vm.StateDB, header *types.Header,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mode systemTxMode, tracer *tracing.Hooks,
) error {
	if !p.chainConfig.IsJenner(header.Number, header.Time) {
		return nil
	}
	epochLength, err := p.epochLength(chain, header, nil)
	if err != nil {
		return err
	}
	if header.Number.Uint64()%epochLength != epochLength-1 {
		return nil
	}
	data, err := p.validatorSetABI.Pack("checkMaintenance")
	if err != nil {
		return err
	}
	msg := p.getSystemMessage(header.Coinbase, common.HexToAddress(systemcontracts.ValidatorContract), data, common.Big0)
	return p.applyTransaction(msg, state, header, chainContext{ChainHeaderReader: chain, parlia: p}, txs, receipts, receivedTxs, usedGas, mode, tracer)
}
