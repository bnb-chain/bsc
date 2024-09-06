package eth

import (
	"sync"
	"testing"
	"time"
)

// TestAddOrUpdatePeer tests adding new peers and updating existing ones.
func TestAddOrUpdatePeer(t *testing.T) {
	bl := NewBlackList(2, 10*time.Minute, 3)
	bl.AddOrUpdatePeer("peer1", 100)
	if len(bl.peers) != 1 {
		t.Errorf("Expected 1 peer, got %d", len(bl.peers))
	}

	// Test updating the same peer
	bl.AddOrUpdatePeer("peer1", 101)
	if bl.peers["peer1"].HeadBlock != 101 {
		t.Errorf("Expected head block 101, got %d", bl.peers["peer1"].HeadBlock)
	}

	// Test adding another peer and triggering the maxPeers limit
	bl.AddOrUpdatePeer("peer2", 102)
	bl.AddOrUpdatePeer("peer3", 103) // This should remove the oldest (peer1)
	if len(bl.peers) != 2 {
		t.Errorf("Expected 2 peers, got %d", len(bl.peers))
	}
	if _, exists := bl.peers["peer1"]; exists {
		t.Errorf("Expected peer1 to be removed")
	}
}

// TestBlacklistStalePeers tests the automatic blacklisting of stale peers.
func TestBlacklistStalePeers(t *testing.T) {
	bl := NewBlackList(2, 1*time.Minute, 1)
	bl.AddOrUpdatePeer("peer1", 100)
	time.Sleep(2 * time.Minute) // simulate time passing
	bl.BlacklistStalePeers()

	if bl.peers["peer1"].BlacklistCount != 1 {
		t.Errorf("Expected BlacklistCount of 1, got %d", bl.peers["peer1"].BlacklistCount)
	}
}

// TestIsBlacklisted tests checking if a peer is blacklisted.
func TestIsBlacklisted(t *testing.T) {
	bl := NewBlackList(2, 1*time.Minute, 1)
	bl.AddOrUpdatePeer("peer1", 100)
	bl.peers["peer1"].LastSeen = time.Now().Add(-2 * time.Minute) // make peer stale
	bl.BlacklistStalePeers()

	if !bl.IsBlacklisted("peer1") {
		t.Errorf("Expected peer1 to be blacklisted")
	}
}

// TestEdgeCases tests handling of edge cases such as invalid IDs.
func TestEdgeCases(t *testing.T) {
	bl := NewBlackList(2, 1*time.Minute, 1)
	bl.AddOrUpdatePeer("", 100) // testing with empty ID
	if len(bl.peers) != 0 {
		t.Errorf("Expected 0 peers, got %d", len(bl.peers))
	}
}

// TestAddOrUpdatePeer_MaxPeers tests behavior when adding peers up to and beyond the maximum limit.
func TestAddOrUpdatePeer_MaxPeers(t *testing.T) {
	bl := NewBlackList(3, 10*time.Minute, 3)

	bl.AddOrUpdatePeer("peer1", 100)
	bl.AddOrUpdatePeer("peer2", 101)
	bl.AddOrUpdatePeer("peer3", 102)
	bl.AddOrUpdatePeer("peer4", 103) // This should remove peer1

	if len(bl.peers) != 3 {
		t.Errorf("Expected 3 peers, got %d", len(bl.peers))
	}

	if _, exists := bl.peers["peer1"]; exists {
		t.Errorf("Expected peer1 to be removed")
	}
}

// TestBlacklistCountOverflow checks how the system handles when a peer's BlacklistCount exceeds the threshold.
func TestBlacklistCountOverflow(t *testing.T) {
	bl := NewBlackList(2, 1*time.Second, 2)
	bl.AddOrUpdatePeer("peer1", 100)

	// Simulate multiple blacklist increments
	time.Sleep(2 * time.Second)
	bl.BlacklistStalePeers()
	bl.BlacklistStalePeers()

	if !bl.IsBlacklisted("peer1") {
		t.Errorf("Expected peer1 to be blacklisted after exceeding blacklist count")
	}
}

// TestExpiryTimeBoundary tests behavior when a peer's LastSeen is exactly at the expiry boundary.
func TestExpiryTimeBoundary(t *testing.T) {
	bl := NewBlackList(2, 1*time.Second, 2)
	bl.AddOrUpdatePeer("peer1", 100)
	time.Sleep(1 * time.Second)

	bl.BlacklistStalePeers() // Should increment blacklist count but not blacklisted yet

	if bl.peers["peer1"].BlacklistCount != 1 {
		t.Errorf("Expected BlacklistCount of 1, got %d", bl.peers["peer1"].BlacklistCount)
	}

	if bl.IsBlacklisted("peer1") {
		t.Errorf("Peer1 should not be blacklisted yet")
	}
}

// TestConcurrentAccess tests concurrent access to the blackList.
func TestConcurrentAccess(t *testing.T) {
	bl := NewBlackList(100, 10*time.Minute, 3)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			bl.AddOrUpdatePeer(id, int64(i))
		}(string(rune('a' + i)))
	}

	wg.Wait()

	if len(bl.peers) != 50 {
		t.Errorf("Expected 50 peers, got %d", len(bl.peers))
	}
}

// TestReaddingBlacklistedPeer tests the behavior when a blacklisted peer is re-added.
func TestReaddingBlacklistedPeer(t *testing.T) {
	bl := NewBlackList(2, 1*time.Second, 1)
	bl.AddOrUpdatePeer("peer1", 100)

	// Simulate time passing to trigger blacklist
	time.Sleep(2 * time.Second)
	bl.BlacklistStalePeers()

	// Ensure peer1 is blacklisted
	if !bl.IsBlacklisted("peer1") {
		t.Errorf("Expected peer1 to be blacklisted")
	}

	// Re-add the same peer
	bl.AddOrUpdatePeer("peer1", 101)

	if bl.peers["peer1"].BlacklistCount != 0 {
		t.Errorf("Expected BlacklistCount to be reset, got %d", bl.peers["peer1"].BlacklistCount)
	}

	if bl.IsBlacklisted("peer1") {
		t.Errorf("Peer1 should not be blacklisted after re-adding with updated information")
	}
}
