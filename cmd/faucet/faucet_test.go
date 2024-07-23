// Copyright 2021 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"net/http"
	"testing"

	"net/http/httptest"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/time/rate"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestFacebook(t *testing.T) {
	// TODO: Remove facebook auth or implement facebook api, which seems to require an API key
	t.Skipf("The facebook access is flaky, needs to be reimplemented or removed")
	for _, tt := range []struct {
		url  string
		want common.Address
	}{
		{
			"https://www.facebook.com/fooz.gazonk/posts/2837228539847129",
			common.HexToAddress("0xDeadDeaDDeaDbEefbEeFbEEfBeeFBeefBeeFbEEF"),
		},
	} {
		_, _, gotAddress, err := authFacebook(tt.url)
		if err != nil {
			t.Fatal(err)
		}
		if gotAddress != tt.want {
			t.Fatalf("address wrong, have %v want %v", gotAddress, tt.want)
		}
	}
}

func TestFaucetRateLimiting(t *testing.T) {
	// Create a minimal faucet instance for testing
	privateKey, _ := crypto.GenerateKey()
	faucetAddr := crypto.PubkeyToAddress(privateKey.PublicKey)

	config := &core.Genesis{
		Alloc: core.GenesisAlloc{
			faucetAddr: {Balance: common.Big1},
		},
	}

	// Create a faucet with rate limiting (1 request per second, burst of 2)
	ks := keystore.NewKeyStore(t.TempDir(), keystore.LightScryptN, keystore.LightScryptP)
	_, err := ks.NewAccount("password")
	if err != nil {
		t.Fatal(err)
	}
	if len(ks.Accounts()) == 0 {
		t.Fatalf("No accounts %v", ks)
	}
	f, err := newFaucet(config, "http://localhost:8545", ks, []byte{}, nil)
	if err != nil {
		t.Fatalf("Failed to create faucet: %v", err)
	}
	f.limiter, err = NewIPRateLimiter(rate.Limit(1.0), 1, 2)
	if err != nil {
		t.Fatalf("Failed to create NewIPRateLimiter: %v", err)
	}

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(f.apiHandler))
	defer server.Close()

	// Helper function to make a request
	makeRequest := func() int {
		resp, err := http.Get(server.URL)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	// Test rapid requests
	var wg sync.WaitGroup
	results := make([]int, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index] = makeRequest()
		}(i)
	}

	wg.Wait()

	// Check results
	successCount := 0
	rateLimitCount := 0
	for _, status := range results {
		if status == http.StatusOK {
			successCount++
		} else if status == http.StatusTooManyRequests {
			rateLimitCount++
		}
	}

	// We expect 2 successful requests (due to burst) and 3 rate-limited requests
	if successCount != 2 || rateLimitCount != 3 {
		t.Errorf("Expected 2 successful and 3 rate-limited requests, got %d successful and %d rate-limited", successCount, rateLimitCount)
	}

	// Wait for rate limit to reset
	time.Sleep(2 * time.Second)

	// Make another request, it should succeed
	status := makeRequest()
	if status != http.StatusOK {
		t.Errorf("Expected success after rate limit reset, got status %d", status)
	}
}
