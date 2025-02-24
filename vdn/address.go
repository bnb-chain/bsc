package vdn

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
)

func ParsePeersAddr(raws []string) ([]peer.AddrInfo, error) {
	addrs := make([]multiaddr.Multiaddr, len(raws))
	for i, addr := range raws {
		addrs[i] = multiaddr.StringCast(addr)
	}

	if len(addrs) == 0 {
		return nil, nil
	}
	ret, err := peer.AddrInfosFromP2pAddrs(addrs...)
	if err != nil {
		return nil, errors.Wrap(err, "AddrInfosFromP2pAddrs err")
	}
	return ret, nil
}
