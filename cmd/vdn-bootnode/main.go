// Copyright 2025 BNB chain
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/vdn"
	"github.com/prysmaticlabs/prysm/v5/network"

	"github.com/sirupsen/logrus"
)

var (
	debug          = flag.Bool("debug", false, "Enable debug logging")
	privateKeyPath = flag.String("private-path", "./key", "Path to private key to use for peer ID")
	port           = flag.Int("port", 3000, "udp port to listen for discovery connections")
	externalIP     = flag.String("external-ip", "", "External IP for the bootnode")
)

func main() {
	flag.Parse()
	fmt.Printf("Starting bootnode...\n")

	if *debug {
		logrus.SetLevel(logrus.DebugLevel)

		// Geth specific logging.
		log.SetDefault(log.NewLogger(log.NewTerminalHandlerWithLevel(os.Stderr, log.LvlTrace, true)))

		log.Debug("Debug logging enabled.")
	}

	// Setting up the bootnode IP address.
	ipAddr, err := network.ExternalIP()
	if err != nil {
		utils.Fatalf("Failed to get external IP: %v", err)
	}
	if *externalIP != "" {
		ipAddr = *externalIP
	}

	cfg := vdn.Config{
		HostAddress:     ipAddr,
		NodeKeyPath:     *privateKeyPath,
		TCPPort:         *port,
		MaxPeers:        50,
		EnableQuic:      false,
		EnableDiscovery: true,
	}

	server, err := vdn.CreateBootstrapServer(&cfg)
	if err != nil {
		utils.Fatalf("Failed to create server: %v", err)
	}
	server.Start()
	defer server.Stop()
	select {}
}
