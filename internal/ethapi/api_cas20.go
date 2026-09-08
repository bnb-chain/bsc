package ethapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/rpc"
)

// cas20BatchLimit bounds one cas20_getTokenInfoBatch request.
const cas20BatchLimit = 20

// CAS20API reads CAS20 token configuration straight from state, so a wallet gets
// in one call, at one block, what would otherwise take a dozen eth_calls. It is
// not in the default module list; enable it with --http.api or --ws.api.
type CAS20API struct {
	b Backend
}

func NewCAS20API(b Backend) *CAS20API {
	return &CAS20API{b: b}
}

// GetTokenInfo returns a CAS20 token's configuration as of the given block
// (latest when omitted). It errors for an address that holds no token there.
func (api *CAS20API) GetTokenInfo(ctx context.Context, address common.Address, blockNrOrHash *rpc.BlockNumberOrHash) (*vm.CAS20TokenInfo, error) {
	infos, err := api.tokenInfos(ctx, []common.Address{address}, blockNrOrHash)
	if err != nil {
		return nil, err
	}
	if infos[0] == nil {
		return nil, vm.ErrNotCAS20Token
	}
	return infos[0], nil
}

// GetTokenInfoBatch is GetTokenInfo over a list, answering null for an address
// that holds no token so one stranger does not fail the whole portfolio.
func (api *CAS20API) GetTokenInfoBatch(ctx context.Context, addresses []common.Address, blockNrOrHash *rpc.BlockNumberOrHash) ([]*vm.CAS20TokenInfo, error) {
	if len(addresses) > cas20BatchLimit {
		return nil, fmt.Errorf("batch of %d exceeds the limit of %d", len(addresses), cas20BatchLimit)
	}
	return api.tokenInfos(ctx, addresses, blockNrOrHash)
}

func (api *CAS20API) tokenInfos(ctx context.Context, addresses []common.Address, blockNrOrHash *rpc.BlockNumberOrHash) ([]*vm.CAS20TokenInfo, error) {
	at := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	if blockNrOrHash != nil {
		at = *blockNrOrHash
	}
	state, header, err := api.b.StateAndHeaderByNumberOrHash(ctx, at)
	if err != nil {
		return nil, err
	}
	if state == nil || header == nil {
		return nil, errors.New("block state unavailable")
	}
	config := api.b.ChainConfig()
	if !config.IsJenner(header.Number, header.Time) {
		return nil, errors.New("CAS20 is not active at this block")
	}
	out := make([]*vm.CAS20TokenInfo, len(addresses))
	for i, addr := range addresses {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := vm.CAS20TokenInfoAt(state, config.ChainID, addr, header.Time)
		if errors.Is(err, vm.ErrNotCAS20Token) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[i] = info
	}
	return out, state.Error()
}
