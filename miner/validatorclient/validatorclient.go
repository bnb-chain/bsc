package validatorclient

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

// Client defines typed wrappers for the Ethereum RPC API.
type Client struct {
	c *rpc.Client
}

// DialOptions creates a new RPC client for the given URL. You can supply any of the
// pre-defined client options to configure the underlying transport.
func DialOptions(ctx context.Context, rawurl string, opts ...rpc.ClientOption) (*Client, error) {
	c, err := rpc.DialOptions(ctx, rawurl, opts...)
	if err != nil {
		return nil, err
	}
	return newClient(c), nil
}

// newClient creates a client that uses the given RPC client.
func newClient(c *rpc.Client) *Client {
	return &Client{c}
}

// MevRunning returns whether MEV is running
func (ec *Client) MevRunning(ctx context.Context) (bool, error) {
	var result bool
	err := ec.c.CallContext(ctx, &result, "mev_running")
	return result, err
}

// SendBid sends a bid
func (ec *Client) SendBid(ctx context.Context, args types.BidArgs) (common.Hash, error) {
	var hash common.Hash
	err := ec.c.CallContext(ctx, &hash, "mev_sendBid", args)
	if err != nil {
		return common.Hash{}, err
	}
	return hash, nil
}

// BestBidGasFee returns the gas fee of the best bid for the given parent hash.
func (ec *Client) BestBidGasFee(ctx context.Context, parentHash common.Hash) (*big.Int, error) {
	var fee *big.Int
	err := ec.c.CallContext(ctx, &fee, "mev_bestBidGasFee", parentHash)
	if err != nil {
		return nil, err
	}
	return fee, nil
}

// MevParams returns the static params of mev
func (ec *Client) MevParams(ctx context.Context) (*types.MevParams, error) {
	var params types.MevParams
	err := ec.c.CallContext(ctx, &params, "mev_params")
	if err != nil {
		return nil, err
	}
	return &params, err
}
