// Copyright 2023 The bsc Authors
// This file is part of bsc.
package main

import (
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/willf/bitset"
)

// follow define in parlia
const (
	AddressLength      = 20
	BLSPublicKeyLength = 48

	// follow order in extra field
	// |---Extra Vanity---|---Validators Number and Validators Bytes (or Empty)---|---Vote Attestation (or Empty)---|---Extra Seal---|
	extraVanityLength    = 32 // Fixed number of extra-data prefix bytes reserved for signer vanity
	validatorNumberSize  = 1  // Fixed number of extra prefix bytes reserved for validator number after Luban
	validatorBytesLength = common.AddressLength + types.BLSPublicKeyLength
	extraSealLength      = 65 // Fixed number of extra-data suffix bytes reserved for signer seal
)

type Extra struct {
	ExtraVanity   string
	ValidatorSize uint8
	Validators    validatorsAscending
	*types.VoteAttestation
	ExtraSeal []byte
}

type ValidatorInfo struct {
	common.Address
	types.BLSPublicKey
	VoteIncluded bool
}

// validatorsAscending implements the sort interface to allow sorting a list of ValidatorInfo
type validatorsAscending []ValidatorInfo

func (s validatorsAscending) Len() int { return len(s) }
func (s validatorsAscending) Less(i, j int) bool {
	return bytes.Compare(s[i].Address[:], s[j].Address[:]) < 0
}
func (s validatorsAscending) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func init() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage:", os.Args[0], "[extraHexData]")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, `
Dumps extra info from the given hex data, only support extra after luban upgrade.`)
	}
}

func main() {
	flag.Parse()
	extraHexData := os.Args[1]
	if extra, err := parseExtra(extraHexData); err == nil {
		fmt.Println("extra parsed successly")
		prettyExtra(*extra)
	} else {
		fmt.Println("extra parsed failed", "err", err)
	}
}

// parseExtra parse hex data into type Extra
func parseExtra(hexData string) (*Extra, error) {
	// decode hex into bytes
	data, err := hex.DecodeString(strings.TrimPrefix(hexData, "0x"))
	if err != nil {
		return nil, fmt.Errorf("invalid hex data")
	}

	// parse ExtraVanity and ExtraSeal
	dataLength := len(data)
	var extra Extra
	if dataLength < extraVanityLength+extraSealLength {
		fmt.Println("length less than min required")
	}
	extra.ExtraVanity = string(data[:extraVanityLength])
	extra.ExtraSeal = data[dataLength-extraSealLength:]
	data = data[extraVanityLength : dataLength-extraSealLength]
	dataLength = len(data)

	// parse Validators and Vote Attestation
	if dataLength > 0 {
		// parse Validators
		if data[0] != '\xf8' { // rlp format of attestation begin with 'f8'
			validatorNum := int(data[0])
			validatorBytesTotalLength := validatorNumberSize + validatorNum*validatorBytesLength
			if dataLength < validatorBytesTotalLength {
				return nil, fmt.Errorf("parse validators failed")
			}
			extra.ValidatorSize = uint8(validatorNum)
			data = data[validatorNumberSize:]
			for i := 0; i < validatorNum; i++ {
				var validatorInfo ValidatorInfo
				validatorInfo.Address = common.BytesToAddress(data[i*validatorBytesLength : i*validatorBytesLength+common.AddressLength])
				copy(validatorInfo.BLSPublicKey[:], data[i*validatorBytesLength+common.AddressLength:(i+1)*validatorBytesLength])
				extra.Validators = append(extra.Validators, validatorInfo)
			}
			sort.Sort(extra.Validators)
			data = data[validatorBytesTotalLength-validatorNumberSize:]
			dataLength = len(data)
		}

		// parse Vote Attestation
		if dataLength > 0 {
			if err := rlp.Decode(bytes.NewReader(data), &extra.VoteAttestation); err != nil {
				return nil, fmt.Errorf("parse voteAttestation failed")
			}
			if extra.ValidatorSize > 0 {
				validatorsBitSet := bitset.From([]uint64{uint64(extra.VoteAddressSet)})
				for i := 0; i < int(extra.ValidatorSize); i++ {
					if validatorsBitSet.Test(uint(i)) {
						extra.Validators[i].VoteIncluded = true
					}
				}
			}
		}
	}

	return &extra, nil
}

// prettyExtra print Extra with a pretty format
func prettyExtra(extra Extra) {
	fmt.Printf("ExtraVanity	:	%s\n", extra.ExtraVanity)

	if extra.ValidatorSize > 0 {
		fmt.Printf("ValidatorSize	:	%d\n", extra.ValidatorSize)
		for i := 0; i < int(extra.ValidatorSize); i++ {
			fmt.Printf("Validator	%d\n", i+1)
			fmt.Printf("\tAddress	:	%s\n", common.Bytes2Hex(extra.Validators[i].Address[:]))
			fmt.Printf("\tVoteKey	:	%s\n", common.Bytes2Hex(extra.Validators[i].BLSPublicKey[:]))
			fmt.Printf("\tVoteIncluded	:	%t\n", extra.Validators[i].VoteIncluded)
		}
	}

	if extra.VoteAttestation != nil {
		fmt.Printf("Attestation	:\n")
		fmt.Printf("\tVoteAddressSet	:	%b,	%d\n", extra.VoteAddressSet, bitset.From([]uint64{uint64(extra.VoteAddressSet)}).Count())
		fmt.Printf("\tAggSignature	:	%s\n", common.Bytes2Hex(extra.AggSignature[:]))
		fmt.Printf("\tVoteData	:\n")
		fmt.Printf("\t\tSourceNumber	:	%d\n", extra.Data.SourceNumber)
		fmt.Printf("\t\tSourceHash	:	%s\n", common.Bytes2Hex(extra.Data.SourceHash[:]))
		fmt.Printf("\t\tTargetNumber	:	%d\n", extra.Data.TargetNumber)
		fmt.Printf("\t\tTargetHash	:	%s\n", common.Bytes2Hex(extra.Data.TargetHash[:]))
	}

	fmt.Printf("ExtraSeal	:	%s\n", common.Bytes2Hex(extra.ExtraSeal))
}
