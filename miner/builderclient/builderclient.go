package builderclient

import (
	"context"

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

// ReportIssue reports an issue
func (ec *Client) ReportIssue(ctx context.Context, args *types.BidIssue) error {
	return ec.c.CallContext(ctx, nil, "mev_reportIssue", args)
}
