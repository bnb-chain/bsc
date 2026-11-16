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

package ethtest

import (
	crand "crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/catalyst"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	ethproto "github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/internal/utesting"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
)

func makeJWTSecret(t *testing.T) (string, [32]byte, error) {
	var secret [32]byte
	if _, err := crand.Read(secret[:]); err != nil {
		return "", secret, fmt.Errorf("failed to create jwt secret: %v", err)
	}
	jwtPath := filepath.Join(t.TempDir(), "jwt_secret")
	if err := os.WriteFile(jwtPath, []byte(hexutil.Encode(secret[:])), 0600); err != nil {
		return "", secret, fmt.Errorf("failed to prepare jwt secret file: %v", err)
	}
	return jwtPath, secret, nil
}

// Keep eth/70 coverage until the production protocol list drops it.
var suiteEthVersions = []uint{ethproto.ETH71, ethproto.ETH68, ethproto.ETH70}

func TestEthSuite(t *testing.T) {
	for _, version := range suiteEthVersions {
		t.Run(fmt.Sprintf("eth%d", version), func(t *testing.T) {
			testEthSuite(t, version)
		})
	}
}

func testEthSuite(t *testing.T, ethVersion uint) {
	if !slices.Contains(ethproto.ProtocolVersions, ethVersion) {
		t.Skipf("eth/%d is not enabled in the node", ethVersion)
	}
	jwtPath, secret, err := makeJWTSecret(t)
	if err != nil {
		t.Fatalf("could not make jwt secret: %v", err)
	}
	geth, err := runGeth("./testdata", jwtPath, ethVersion)
	if err != nil {
		t.Fatalf("could not run geth: %v", err)
	}
	defer geth.Close()

	suite, err := NewSuite(geth.Server().Self(), "./testdata", geth.HTTPAuthEndpoint(), common.Bytes2Hex(secret[:]))
	if err != nil {
		t.Fatalf("could not create new test suite: %v", err)
	}
	// The default 71+68 capabilities must negotiate either target version.
	if ethVersion == ethproto.ETH70 {
		suite.ethVersion = ethVersion
	}
	conn, err := suite.dialAndPeer(nil)
	if err != nil {
		t.Fatalf("could not peer with test node: %v", err)
	}
	conn.Close()
	if conn.negotiatedProtoVersion != ethVersion {
		t.Fatalf("negotiated eth/%d, want eth/%d", conn.negotiatedProtoVersion, ethVersion)
	}
	suite.ethVersion = ethVersion
	suite.requireAvailableBALs = true
	for _, test := range suite.EthTests() {
		t.Run(test.Name, func(t *testing.T) {
			switch test.Name {
			case "GetBlockAccessLists":
				if ethVersion < ethproto.ETH71 {
					t.Skip("BAL exchange requires eth/71")
				}
			case "GetLargeReceipts":
				if ethVersion < ethproto.ETH70 {
					t.Skip("receipt pagination requires eth/70")
				}
			case "BlobTxAvailabilityFailure", "GetCells", "BlobTxWithInvalidCells":
				if ethVersion < ethproto.ETH72 {
					t.Skip("cell exchange requires eth/72")
				}
			}
			if test.Slow && testing.Short() {
				t.Skipf("%s: skipping in -short mode", test.Name)
			}
			result := utesting.RunTests([]utesting.Test{{Name: test.Name, Fn: test.Fn}}, os.Stdout)
			if result[0].Failed {
				t.Fatal()
			}
		})
	}
}

func TestSnapSuite(t *testing.T) {
	for _, version := range suiteEthVersions {
		t.Run(fmt.Sprintf("eth%d", version), func(t *testing.T) {
			testSnapSuite(t, version, false)
		})
	}
}

func TestSnap2Suite(t *testing.T) {
	for _, version := range suiteEthVersions {
		t.Run(fmt.Sprintf("eth%d", version), func(t *testing.T) {
			testSnapSuite(t, version, true)
		})
	}
}

func testSnapSuite(t *testing.T, ethVersion uint, snap2 bool) {
	if !slices.Contains(ethproto.ProtocolVersions, ethVersion) {
		t.Skipf("eth/%d is not enabled in the node", ethVersion)
	}
	jwtPath, secret, err := makeJWTSecret(t)
	if err != nil {
		t.Fatalf("could not make jwt secret: %v", err)
	}
	geth, err := runGeth("./testdata", jwtPath, ethVersion)
	if err != nil {
		t.Fatalf("could not run geth: %v", err)
	}
	defer geth.Close()
	suite, err := NewSuite(geth.Server().Self(), "./testdata", geth.HTTPAuthEndpoint(), common.Bytes2Hex(secret[:]))
	if err != nil {
		t.Fatalf("could not create new test suite: %v", err)
	}
	suite.ethVersion = ethVersion
	tests := suite.SnapTests()
	if snap2 {
		suite.requireAvailableBALs = true
		tests = suite.Snap2Tests()
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			result := utesting.RunTests([]utesting.Test{{Name: test.Name, Fn: test.Fn}}, os.Stdout)
			if result[0].Failed {
				t.Fatal()
			}
		})
	}
}

// runGeth creates and starts a geth node
func runGeth(dir string, jwtPath string, ethVersion uint) (*node.Node, error) {
	stack, err := node.New(&node.Config{
		AuthAddr: "127.0.0.1",
		AuthPort: 0,
		P2P: p2p.Config{
			ListenAddr:    "127.0.0.1:0",
			NoDiscovery:   true,
			MaxPeers:      10, // in case a test requires multiple connections, can be changed in the future
			MaxPeersPerIP: 10,
			NoDial:        true,
		},
		JWTSecret: jwtPath,
	})
	if err != nil {
		return nil, err
	}

	err = setupGeth(stack, dir)
	if err != nil {
		stack.Close()
		return nil, err
	}
	stack.Server().Protocols = slices.DeleteFunc(stack.Server().Protocols, func(protocol p2p.Protocol) bool {
		return protocol.Name == "eth" && protocol.Version != ethVersion
	})
	if err = stack.Start(); err != nil {
		stack.Close()
		return nil, err
	}
	return stack, nil
}

func setupGeth(stack *node.Node, dir string) error {
	chain, err := NewChain(dir)
	if err != nil {
		return err
	}
	backend, err := eth.New(stack, &ethconfig.Config{
		Genesis:        &chain.genesis,
		NetworkId:      chain.genesis.Config.ChainID.Uint64(), // 19763
		DatabaseCache:  10,
		TrieCleanCache: 10,
		TrieDirtyCache: 16,
		TrieTimeout:    60 * time.Minute,
		SnapshotCache:  10,
		TriesInMemory:  128,
		SnapV2:         true, // advertise snap/2 (alongside snap/1) so the snap/2 suite can negotiate it
	})
	if err != nil {
		return err
	}
	if err := catalyst.Register(stack, backend); err != nil {
		return fmt.Errorf("failed to register catalyst service: %v", err)
	}
	_, err = backend.BlockChain().InsertChain(chain.blocks[1:])
	return err
}
