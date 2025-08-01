package eth

import (
	"reflect"
	"slices"
	"testing"

	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
)

// mockPeer is a simplified p2p.Peer for testing purposes
type mockPeer struct {
	id                    string
	enableDirectBroadcast atomic.Bool
	enableNoTxBroadcast   atomic.Bool
}

func (p *mockPeer) ID() string {
	return p.id
}

// mockEthPeer is a simplified eth.Peer for testing purposes
type mockEthPeer struct {
	peer *mockPeer
}

func (p *mockEthPeer) ID() string {
	return p.peer.ID()
}

// Create a test mockEthPeer
func newMockEthPeer(id string) *mockEthPeer {
	return &mockEthPeer{
		peer: &mockPeer{
			id: id,
		},
	}
}

// Test the functionality of enablePeerFeatures method
func TestEnablePeerFeatures(t *testing.T) {
	tests := []struct {
		name         string
		validatorMap map[common.Address][]string
		directList   []string
		noTxList     []string
		proxyedList  []string
		peers        map[string]*mockEthPeer
		expectations map[string]struct {
			directBroadcast bool
			noTxBroadcast   bool
		}
	}{
		{
			name:         "Empty Lists Test",
			validatorMap: map[common.Address][]string{},
			directList:   []string{},
			noTxList:     []string{},
			proxyedList:  []string{},
			peers: map[string]*mockEthPeer{
				"peer1": newMockEthPeer("peer1"),
				"peer2": newMockEthPeer("peer2"),
			},
			expectations: map[string]struct {
				directBroadcast bool
				noTxBroadcast   bool
			}{
				"peer1": {directBroadcast: false, noTxBroadcast: false},
				"peer2": {directBroadcast: false, noTxBroadcast: false},
			},
		},
		{
			name: "Validator Node Test",
			validatorMap: map[common.Address][]string{
				common.HexToAddress("0x1"): {"peer1"},
			},
			directList:  []string{},
			noTxList:    []string{},
			proxyedList: []string{},
			peers: map[string]*mockEthPeer{
				"peer1": newMockEthPeer("peer1"),
				"peer2": newMockEthPeer("peer2"),
			},
			expectations: map[string]struct {
				directBroadcast bool
				noTxBroadcast   bool
			}{
				"peer1": {directBroadcast: true, noTxBroadcast: false},
				"peer2": {directBroadcast: false, noTxBroadcast: false},
			},
		},
		{
			name:         "Direct Broadcast Node Test",
			validatorMap: map[common.Address][]string{},
			directList:   []string{"peer2"},
			noTxList:     []string{},
			proxyedList:  []string{},
			peers: map[string]*mockEthPeer{
				"peer1": newMockEthPeer("peer1"),
				"peer2": newMockEthPeer("peer2"),
			},
			expectations: map[string]struct {
				directBroadcast bool
				noTxBroadcast   bool
			}{
				"peer1": {directBroadcast: false, noTxBroadcast: false},
				"peer2": {directBroadcast: true, noTxBroadcast: false},
			},
		},
		{
			name:         "No Transaction Broadcast Node Test",
			validatorMap: map[common.Address][]string{},
			directList:   []string{},
			noTxList:     []string{"peer1"},
			proxyedList:  []string{},
			peers: map[string]*mockEthPeer{
				"peer1": newMockEthPeer("peer1"),
				"peer2": newMockEthPeer("peer2"),
			},
			expectations: map[string]struct {
				directBroadcast bool
				noTxBroadcast   bool
			}{
				"peer1": {directBroadcast: false, noTxBroadcast: true},
				"peer2": {directBroadcast: false, noTxBroadcast: false},
			},
		},
		{
			name:         "Proxy Node Test",
			validatorMap: map[common.Address][]string{},
			directList:   []string{},
			noTxList:     []string{"peer1", "peer2"},
			proxyedList:  []string{"peer2"},
			peers: map[string]*mockEthPeer{
				"peer1": newMockEthPeer("peer1"),
				"peer2": newMockEthPeer("peer2"),
			},
			expectations: map[string]struct {
				directBroadcast bool
				noTxBroadcast   bool
			}{
				"peer1": {directBroadcast: false, noTxBroadcast: true},
				"peer2": {directBroadcast: false, noTxBroadcast: false}, // Node in proxyedList should not enable noTxBroadcast
			},
		},
		{
			name: "Combined Test",
			validatorMap: map[common.Address][]string{
				common.HexToAddress("0x1"): {"peer1", "peer3"},
			},
			directList:  []string{"peer2"},
			noTxList:    []string{"peer1", "peer2", "peer4"},
			proxyedList: []string{"peer2"},
			peers: map[string]*mockEthPeer{
				"peer1": newMockEthPeer("peer1"),
				"peer2": newMockEthPeer("peer2"),
				"peer3": newMockEthPeer("peer3"),
				"peer4": newMockEthPeer("peer4"),
			},
			expectations: map[string]struct {
				directBroadcast bool
				noTxBroadcast   bool
			}{
				"peer1": {directBroadcast: true, noTxBroadcast: true},
				"peer2": {directBroadcast: true, noTxBroadcast: false}, // In proxyedList
				"peer3": {directBroadcast: true, noTxBroadcast: false},
				"peer4": {directBroadcast: false, noTxBroadcast: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a special peerSet to mock enablePeerFeatures
			mockPeerSet := &mockPeerSet{
				peers: tt.peers,
			}

			// Call the test method
			mockPeerSet.enablePeerFeatures(tt.validatorMap, tt.directList, tt.noTxList, tt.proxyedList)

			// Verify that consensusAddressMap is set correctly
			if !reflect.DeepEqual(mockPeerSet.consensusAddressMap, tt.validatorMap) {
				t.Errorf("consensusAddressMap = %v, want %v", mockPeerSet.consensusAddressMap, tt.validatorMap)
			}

			// Verify that each peer's settings match the expected values
			for id, expect := range tt.expectations {
				peer := mockPeerSet.peers[id]
				if peer.peer.enableDirectBroadcast.Load() != expect.directBroadcast {
					t.Errorf("peer %s, EnableDirectBroadcast = %v, want %v", id, peer.peer.enableDirectBroadcast.Load(), expect.directBroadcast)
				}
				if peer.peer.enableNoTxBroadcast.Load() != expect.noTxBroadcast {
					t.Errorf("peer %s, EnableNoTxBroadcast = %v, want %v", id, peer.peer.enableNoTxBroadcast.Load(), expect.noTxBroadcast)
				}
			}
		})
	}
}

// mockPeerSet is a simplified peerSet for testing purposes
type mockPeerSet struct {
	peers               map[string]*mockEthPeer
	consensusAddressMap map[common.Address][]string
}

// Mock the functionality of enablePeerFeatures method
func (ps *mockPeerSet) enablePeerFeatures(validatorMap map[common.Address][]string, directList []string, noTxList []string, proxyedList []string) {
	ps.consensusAddressMap = validatorMap
	var valNodeIDs []string
	for _, nodeIDs := range validatorMap {
		valNodeIDs = append(valNodeIDs, nodeIDs...)
	}

	// Similar to the implementation in peerset.go
	for _, peer := range ps.peers {
		nodeID := peer.ID()
		if contains(directList, nodeID) || contains(valNodeIDs, nodeID) {
			peer.peer.enableDirectBroadcast.Store(true)
		}
		// if the peer is in the noTxList and not in the proxyedList, enable the no tx broadcast feature
		if contains(noTxList, nodeID) && !contains(proxyedList, nodeID) {
			peer.peer.enableNoTxBroadcast.Store(true)
		}
	}
}

// Test the functionality of existProxyedValidator method
func TestExistProxyedValidator(t *testing.T) {
	tests := []struct {
		name           string
		address        common.Address
		proxyedList    []string
		validatorMap   map[common.Address][]string
		peers          map[string]*mockEthPeer
		expectExisting bool
	}{
		{
			name:           "Empty Validator Map",
			address:        common.HexToAddress("0x1"),
			proxyedList:    []string{"peer1"},
			validatorMap:   nil,
			peers:          map[string]*mockEthPeer{},
			expectExisting: false,
		},
		{
			name:        "Validator Not In Map",
			address:     common.HexToAddress("0x1"),
			proxyedList: []string{"peer1"},
			validatorMap: map[common.Address][]string{
				common.HexToAddress("0x2"): {"peer2"},
			},
			peers: map[string]*mockEthPeer{
				"peer1": newMockEthPeer("peer1"),
				"peer2": newMockEthPeer("peer2"),
			},
			expectExisting: false,
		},
		{
			name:        "Validator In Map But Node Not In Proxy List",
			address:     common.HexToAddress("0x1"),
			proxyedList: []string{"peer2"},
			validatorMap: map[common.Address][]string{
				common.HexToAddress("0x1"): {"peer1"},
			},
			peers: map[string]*mockEthPeer{
				"peer1": newMockEthPeer("peer1"),
				"peer2": newMockEthPeer("peer2"),
			},
			expectExisting: false,
		},
		{
			name:        "Validator In Map And Node In Proxy List",
			address:     common.HexToAddress("0x1"),
			proxyedList: []string{"peer1"},
			validatorMap: map[common.Address][]string{
				common.HexToAddress("0x1"): {"peer1"},
			},
			peers: map[string]*mockEthPeer{
				"peer1": newMockEthPeer("peer1"),
				"peer2": newMockEthPeer("peer2"),
			},
			expectExisting: true,
		},
		{
			name:        "Validator Has Multiple Nodes And Some In Proxy List",
			address:     common.HexToAddress("0x1"),
			proxyedList: []string{"peer2"},
			validatorMap: map[common.Address][]string{
				common.HexToAddress("0x1"): {"peer1", "peer2", "peer3"},
			},
			peers: map[string]*mockEthPeer{
				"peer1": newMockEthPeer("peer1"),
				"peer2": newMockEthPeer("peer2"),
				"peer3": newMockEthPeer("peer3"),
			},
			expectExisting: true,
		},
		{
			name:        "Validator Node Not Connected",
			address:     common.HexToAddress("0x1"),
			proxyedList: []string{"peer1"},
			validatorMap: map[common.Address][]string{
				common.HexToAddress("0x1"): {"peer1"},
			},
			peers:          map[string]*mockEthPeer{}, // Empty peers set, indicating validator node is not connected
			expectExisting: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a special peerSet to mock existProxyedValidator
			mockPeerSet := &mockPeerSet{
				peers:               tt.peers,
				consensusAddressMap: tt.validatorMap,
			}

			// Test method
			result := existProxyedValidator(mockPeerSet, tt.address, tt.proxyedList)

			// Verify result
			if result != tt.expectExisting {
				t.Errorf("existProxyedValidator() = %v, want %v", result, tt.expectExisting)
			}
		})
	}
}

// Mock the functionality of existProxyedValidator method
func existProxyedValidator(ps *mockPeerSet, address common.Address, proxyedList []string) bool {
	if ps.consensusAddressMap == nil {
		return false
	}

	peerIDs := ps.consensusAddressMap[address]
	for _, peerID := range peerIDs {
		if ps.peers[peerID] == nil {
			continue
		}
		if contains(proxyedList, peerID) {
			return true
		}
	}
	return false
}

// contains checks if a string slice contains a specific string
func contains(slice []string, str string) bool {
	return slices.Contains(slice, str)
}
