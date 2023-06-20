// submit the evidence of malicious voting
package main

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/internal/flags"
	"github.com/ethereum/go-ethereum/log"
	"gopkg.in/urfave/cli.v1"
)

var (
	// Git SHA1 commit hash of the release (set via linker flags)
	gitCommit = ""
	gitDate   = ""

	app *cli.App

	senderFlag = cli.StringFlag{
		Name:  "sender",
		Usage: "raw private key in hex format without 0x prefix; check permission on your own",
	}
	nodeFlag = cli.StringFlag{
		Name:  "node",
		Usage: "rpc endpoint, http,https,ws,wss,ipc are supported",
	}
	chainIdFlag = cli.UintFlag{
		Name:  "chainId",
		Usage: "chainId, can get by eth_chainId",
	}
	evidenceFlag = cli.StringFlag{
		Name:  "evidence",
		Usage: "params for submitFinalityViolationEvidence in json format; string",
	}
)

func init() {
	app = flags.NewApp(gitCommit, gitDate, "a tool for submitting the evidence of malicious voting")
	app.Flags = []cli.Flag{
		senderFlag,
		nodeFlag,
		chainIdFlag,
		evidenceFlag,
	}
	app.Action = submitMaliciousVotes
	cli.CommandHelpTemplate = flags.AppHelpTemplate
}

func submitMaliciousVotes(c *cli.Context) {
	// get sender
	senderRawKey := c.GlobalString(senderFlag.Name)
	if senderRawKey == "" {
		log.Crit("no sender specified (--sender)")
	}
	sender, err := crypto.HexToECDSA(senderRawKey)
	if err != nil {
		log.Crit("get sender failed", "error", err)
	} else {
		log.Info("get sender success")
	}

	// connect to the given URL
	nodeURL := c.GlobalString(nodeFlag.Name)
	if nodeURL == "" {
		log.Crit("no node specified (--node)")
	}
	client, err := ethclient.Dial(nodeURL)
	if err != nil {
		log.Crit("Error connecting to client", "nodeURL", nodeURL, "error", err)
	} else {
		// when nodeURL is type of http or https, err==nil not mean successfully connected
		if !strings.HasPrefix(nodeURL, "http") {
			log.Info("Successfully connected to client", "nodeURL", nodeURL)
		}
	}

	// get chainId
	chainId := c.GlobalUint(chainIdFlag.Name)
	if chainId == 0 {
		log.Crit("no chainId specified (--chainId)")
	} else {
		log.Info("get chainId success", "chainId", chainId)
	}

	// get evidence
	evidenceJson := c.GlobalString(evidenceFlag.Name)
	if evidenceJson == "" {
		log.Crit("no evidence specified (--evidence)")
	}
	var evidence SlashIndicatorFinalityEvidence
	if err = evidence.UnmarshalJSON([]byte(evidenceJson)); err != nil {
		log.Crit("Error parsing evidence", "error", err)
	} else {
		log.Info("get evidence success")
	}

	ops, _ := bind.NewKeyedTransactorWithChainID(sender, big.NewInt(int64(chainId)))
	//ops.GasLimit = 800000
	slashIndicator, _ := NewSlashIndicator(common.HexToAddress(systemcontracts.SlashContract), client)
	tx, err := slashIndicator.SubmitFinalityViolationEvidence(ops, evidence)
	if err != nil {
		log.Crit("submitMaliciousVotes:", "error", err)
	}
	var rc *types.Receipt
	for i := 0; i < 180; i++ {
		rc, err = client.TransactionReceipt(context.Background(), tx.Hash())
		if err == nil && rc.Status != 0 {
			log.Info("submitMaliciousVotes: submit evidence success", "receipt", rc)
			break
		}
		if rc != nil && rc.Status == 0 {
			log.Crit("submitMaliciousVotes: tx failed: ", "error", err, "receipt", rc)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if rc == nil {
		log.Crit("submitMaliciousVotes: submit evidence failed")
	}
}

func main() {
	log.Root().SetHandler(log.LvlFilterHandler(log.LvlInfo, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

}
