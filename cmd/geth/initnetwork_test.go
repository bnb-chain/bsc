package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

var size int
var basePort int
var configPath string
var genesisPath string

func setup(t *testing.T) {
	size = 4
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("error getting current file path")
	}
	currentDirectory := filepath.Dir(filename)
	configPath = filepath.Join(currentDirectory, "testdata/config.toml")
	genesisPath = filepath.Join(currentDirectory, "testdata/parlia.json")
	basePort = 30311
}

func TestInitNetworkLocalhost(t *testing.T) {
	setup(t)
	ipStr := ""
	testInitNetwork(t, size, basePort, ipStr, configPath, genesisPath)
}

func TestInitNetworkRemoteHosts(t *testing.T) {
	setup(t)
	ipStr := "192.168.24.103,172.15.67.89,10.0.17.36,203.113.45.76"
	testInitNetwork(t, size, basePort, ipStr, configPath, genesisPath)
}

func testInitNetwork(t *testing.T, size, basePort int, ipStr, configPath, genesisPath string) {
	dir := t.TempDir()
	geth := runGeth(t, "init-network", "--init.dir", dir, "--init.size", strconv.Itoa(size),
		"--init.ips", ipStr, "--init.p2p-port", strconv.Itoa(basePort), "--config", configPath,
		genesisPath)
	// expect the command to complete first
	geth.WaitExit()

	// Read the output of the command
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != size {
		t.Fatalf("expected %d node folders but found %d instead", size, len(files))
	}

	for i, file := range files {
		if file.IsDir() {
			expectedNodeDirName := fmt.Sprintf("node%d", i)
			if file.Name() != expectedNodeDirName {
				t.Fatalf("node dir name is %s but %s was expected", file.Name(), expectedNodeDirName)
			}
			configFilePath := filepath.Join(dir, file.Name(), "config.toml")
			var config gethConfig
			err := loadConfig(configFilePath, &config)
			if err != nil {
				t.Fatalf("failed to load config.toml : %v", err)
			}
			if ipStr == "" {
				verifyConfigFileLocalhost(t, &config, i, basePort, size)
			} else {
				verifyConfigFileRemoteHosts(t, &config, ipStr, i, basePort, size)
			}
		}
	}
}

func verifyConfigFileRemoteHosts(t *testing.T, config *gethConfig, ipStr string, i, basePort, size int) {
	// 1. check ip string
	ips := strings.Split(ipStr, ",")
	if len(ips) != size {
		t.Fatalf("found %d ips in ipStr=%s instead of %d", len(ips), ipStr, size)
	}

	// 2. check listening port
	expectedListenAddr := fmt.Sprintf(":%d", basePort)
	if config.Node.P2P.ListenAddr != expectedListenAddr {
		t.Fatalf("expected ListenAddr to be %s but it is %s instead", expectedListenAddr, config.Node.P2P.ListenAddr)
	}

	bootnodes := config.Node.P2P.BootstrapNodes

	// 3. check correctness of peers' hosts
	for j := 0; j < i; j++ {
		ip := bootnodes[j].IP().String()
		if ip != ips[j] {
			t.Fatalf("expected IP of bootnode to be %s but found %s instead", ips[j], ip)
		}
	}

	for j := i + 1; j < size; j++ {
		ip := bootnodes[j-1].IP().String()
		if ip != ips[j] {
			t.Fatalf("expected IP of bootnode to be %s but found %s instead", ips[j-1], ip)
		}
	}

	// 4. check correctness of peer port numbers
	for j := 0; j < size-1; j++ {
		if bootnodes[j].UDP() != basePort {
			t.Fatalf("expected bootnode port at position %d to be %d but got %d instead", j, basePort, bootnodes[j].UDP())
		}
	}
}

func verifyConfigFileLocalhost(t *testing.T, config *gethConfig, i int, basePort int, size int) {
	// 1. check listening port
	expectedListenAddr := fmt.Sprintf(":%d", basePort+i)
	if config.Node.P2P.ListenAddr != expectedListenAddr {
		t.Fatalf("expected ListenAddr to be %s but it is %s instead", expectedListenAddr, config.Node.P2P.ListenAddr)
	}

	bootnodes := config.Node.P2P.BootstrapNodes
	// 2. check correctness of peers' hosts
	localhost := "127.0.0.1"
	for j := 0; j < size-1; j++ {
		ip := bootnodes[j].IP().String()
		if ip != localhost {
			t.Fatalf("expected IP of bootnode to be %s but found %s instead", localhost, ip)
		}
	}

	// 3. check correctness of peer port numbers
	for j := 0; j < i; j++ {
		if bootnodes[j].UDP() != basePort+j {
			t.Fatalf("expected bootnode port at position %d to be %d but got %d instead", j, basePort+j, bootnodes[j].UDP())
		}
	}
	for j := i + 1; j < size; j++ {
		if bootnodes[j-1].UDP() != basePort+j {
			t.Fatalf("expected bootnode port at position %d to be %d but got %d instead", j-1, basePort+j, bootnodes[j-1].UDP())
		}
	}
}
