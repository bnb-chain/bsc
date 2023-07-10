package main

import (
	"context"
	"crypto/rand"
	"log"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/pkg/errors"
	"github.com/protolambda/ztyp/view"
)

const prefix = "SEND_BLOBS"

// send-blobs <url-without-auth> <transactions-send-formula 10x1,4x2,3x6> <secret-key> <receiver-address>
// send-blobs http://localhost:8545 5 0x0000000000000000000000000000000000000000000000000000000000000000 0x000000000000000000000000000000000000f1c1 100 100

func main() {
	logger := log.New(os.Stdout, prefix, log.LstdFlags|log.Lmicroseconds|log.Lshortfile)
	if err := run(logger); err != nil {
		log.Fatalf(err.Error())
	}
}

func run(logger *log.Logger) error {
	rpcURL := os.Args[1]
	blobTxCounts := parseBlobTxCounts(os.Args[2])
	privateKeyString := os.Args[3]
	receiver := common.HexToAddress(os.Args[4])

	maxFeePerDataGas := uint64(1000)

	if len(os.Args) > 4 {
		var err error
		maxFeePerDataGas, err = strconv.ParseUint(os.Args[4], 10, 64)
		if err != nil {
			return errors.Wrap(err, "parsing maxFeePerDataGas on argument pos 4")
		}
	}

	feeMultiplier := uint64(4)
	if len(os.Args) > 5 {
		var err error
		feeMultiplier, err = strconv.ParseUint(os.Args[5], 10, 64)
		if err != nil {
			return errors.Wrap(err, "parsing maxFeePerDataGas on argument pos 4")
		}
	}

	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return errors.Wrap(err, "connecting to eth client")
	}

	privateKeyECDSA, err := crypto.HexToECDSA(privateKeyString)
	if err != nil {
		return errors.Wrap(err, "parsing private key")
	}

	ctx := context.Background()

	nonce, err := client.PendingNonceAt(ctx, crypto.PubkeyToAddress(privateKeyECDSA.PublicKey))
	if err != nil {
		return errors.Wrap(err, "getting nonce")
	}

	chainID, err := client.ChainID(ctx)
	if err != nil {
		return errors.Wrap(err, "retreiving chain id")
	}

	for _, btxc := range blobTxCounts {
		txCount, blobCount := btxc.count, btxc.perTx

		for txCount > 0 {
			txCount--
			blobs := make([][]byte, 0, blobCount)

			kzgBlobs := make(types.Blobs, 0, blobCount)

			for blobIndex := 0; blobIndex < blobCount; blobIndex++ {
				blobs[blobIndex] = make([]byte, params.BytesPerBlob)
				_, _ = rand.Read(blobs[blobIndex])
				for i := 0; i < params.BytesPerBlob; i += 32 {
					blobs[blobIndex][i] = 0
				}

				blob := blobs[blobIndex]

				var blb [params.BytesPerBlob]byte

				copy(blb[:], blob)

				kzgBlobs[blobIndex] = blb
			}

			commitments, blobHashes, proofs, err := kzgBlobs.ComputeCommitmentsAndProofs()
			if err != nil {
				return errors.Wrap(err, "computing commitments and proofs")
			}

			gasPrice, err := client.SuggestGasPrice(ctx)
			if err != nil {
				return errors.Wrap(err, "retrieving gas price")
			}

			maxPriorityFeePerGas, err := client.SuggestGasTipCap(ctx)
			if err != nil {
				return errors.Wrap(err, "retrieving gas tip cap")
			}

			logger.Printf("Nonce: %d, GasPrice: %s, MaxPriorityFeePerGas: %s\n", nonce, gasPrice.String(), maxPriorityFeePerGas.String())

			msg := types.BlobTxMessage{
				Nonce:               view.Uint64View(nonce),
				Gas:                 view.Uint64View(gasPrice.Mul(gasPrice, new(big.Int).SetUint64(feeMultiplier)).Uint64()),
				To:                  types.AddressOptionalSSZ{Address: (*types.AddressSSZ)(&receiver)},
				GasTipCap:           view.Uint256View(*uint256.NewInt(maxPriorityFeePerGas.Uint64())),
				GasFeeCap:           view.Uint256View(*uint256.NewInt(gasPrice.Mul(gasPrice, new(big.Int).SetUint64(feeMultiplier)).Uint64())),
				MaxFeePerDataGas:    view.Uint256View(*uint256.NewInt(maxFeePerDataGas)),
				Value:               view.Uint256View(*uint256.NewInt(0)),
				BlobVersionedHashes: blobHashes,
			}

			data := types.BlobTxWrapData{
				BlobKzgs: commitments,
				Blobs:    kzgBlobs,
				Proofs:   proofs,
			}
			txdata := types.SignedBlobTx{Message: msg}
			tx := types.NewTx(&txdata, types.WithTxWrapData(&data))

			signedTx, err := types.SignTx(tx, types.NewDankSigner(chainID), privateKeyECDSA)
			if err != nil {
				return errors.Wrapf(err, "signing tx: %+v", tx)
			}

			err = client.SendTransaction(ctx, signedTx)
			if err != nil {
				return errors.Wrapf(err, "sending signed tx: %+v", signedTx)
			}

			nonce++
		}
	}

	return nil
}

func parseBlobTxCounts(blobTxCountsStr string) []blobTxCount {
	blobTxCountsStrArr := strings.Split(blobTxCountsStr, ",")
	blobTxCounts := make([]blobTxCount, len(blobTxCountsStrArr))

	for i, btxcStr := range blobTxCountsStrArr {
		if strings.Contains(btxcStr, "x") {
			parts := strings.Split(btxcStr, "x")
			count, _ := strconv.Atoi(parts[0])
			perTx, _ := strconv.Atoi(parts[1])
			blobTxCounts[i] = blobTxCount{count, perTx}
		} else {
			count, _ := strconv.Atoi(btxcStr)
			blobTxCounts[i] = blobTxCount{count, 1}
		}
	}

	return blobTxCounts
}

type blobTxCount struct {
	count int
	perTx int
}
