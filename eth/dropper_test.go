package eth

import (
	"testing"

	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

func TestDropperProtectsEVNAndProxyedPeers(t *testing.T) {
	tests := []struct {
		name    string
		evn     bool
		proxyed bool
		want    bool
	}{
		{name: "ordinary", want: false},
		{name: "evn", evn: true, want: true},
		{name: "proxyed", proxyed: true, want: true},
		{name: "evn and proxyed", evn: true, proxyed: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peer := p2p.NewPeer(enode.ID{1}, "", nil)
			peer.EVNPeerFlag.Store(tt.evn)
			peer.ProxyedPeerFlag.Store(tt.proxyed)

			if got := isProtectedPeer(peer); got != tt.want {
				t.Fatalf("isProtectedPeer() = %t, want %t", got, tt.want)
			}
		})
	}
}
