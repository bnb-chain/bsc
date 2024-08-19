package server_for_op_stack

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/ethclient"
	"math/big"
	"math/rand"
	"time"
)

const (
	blobCommitmentVersionKZG uint8 = 0x01
)

func kZGToVersionedHash(kzg kzg4844.Commitment) common.Hash {
	h := sha256.Sum256(kzg[:])
	h[0] = blobCommitmentVersionKZG

	return h
}

func fetchBlockNumberByTime(ctx context.Context, ts int64, client *ethclient.Client) (uint64, error) {
	// calc the block number of the ts.
	now := time.Now().Unix()
	if ts > now {
		return 0, errors.New("time too large")
	}
	blockNum, err := client.BlockNumber(ctx)
	if err != nil {
		return 0, err
	}
	estimateEndNumber := int64(blockNum) - (now-ts)/3
	// find the end number
	for {
		header, err := client.HeaderByNumber(ctx, big.NewInt(estimateEndNumber))
		if err != nil {
			time.Sleep(time.Duration(rand.Int()%180) * time.Millisecond)
			continue
		}
		if header == nil {
			estimateEndNumber -= 1
			time.Sleep(time.Duration(rand.Int()%180) * time.Millisecond)
			continue
		}
		headerTime := int64(header.Time)
		if headerTime == ts {
			return header.Number.Uint64(), nil
		}

		// let the estimateEndNumber a little bigger than real value
		if headerTime > ts && headerTime-ts > 8 {
			estimateEndNumber -= (headerTime - ts) / 3
		} else if headerTime < ts {
			estimateEndNumber += (ts-headerTime)/3 + 1
		} else {
			// search one by one
			for headerTime >= ts {
				header, err = client.HeaderByNumber(ctx, big.NewInt(estimateEndNumber-1))
				if err != nil {
					time.Sleep(time.Duration(rand.Int()%180) * time.Millisecond)
					continue
				}
				headerTime = int64(header.Time)
				if headerTime == ts {
					return header.Number.Uint64(), nil
				}
				estimateEndNumber -= 1
				if headerTime < ts { //found the real endNumber
					return 0, errors.New(fmt.Sprintf("block not found by time %d", ts))
				}
			}
		}
	}
}
