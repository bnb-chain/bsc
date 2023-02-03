package tracer

import (
	"github.com/ethereum/go-ethereum/common"
	"math/big"
	"strconv"
	"strings"
)

// Example:
//	input = 0x249e5a5c000000000000000000000000d3c7eec9daa9b061303cfe54f880978d7d03398700000000000000000000000000000000000000000000021e19e0c9bab240000000000000000000000000000000000000000000000000152d02c7e14af6800000
//	return:
//	MethodID: 0x249e5a5c
//	[0]:  000000000000000000000000d3c7eec9daa9b061303cfe54f880978d7d033987
//	[1]:  00000000000000000000000000000000000000000000021e19e0c9bab2400000
//	[2]:  00000000000000000000000000000000000000000000152d02c7e14af6800000

func GetMethodIdAndArgsFromInput(input string) (string, []string) {
	var methodId string
	var methodArgs []string
	if len(input) >= 10 {
		args := input[10:]
		if len(args)%64 == 0 {
			methodId = input[0:10]
			for i := 0; i < len(args); i += 64 {
				methodArgs = append(methodArgs, args[i:i+64])
			}
		}
	}
	return methodId, methodArgs
}

func bytesToHex(s []byte) string {
	return "0x" + common.Bytes2Hex(s)
}

func bigToHex(n *big.Int) string {
	if n == nil {
		return ""
	}
	return "0x" + n.Text(16)
}

func uintToHex(n uint64) string {
	return "0x" + strconv.FormatUint(n, 16)
}

func addrToHex(a common.Address) string {
	return strings.ToLower(a.Hex())
}
