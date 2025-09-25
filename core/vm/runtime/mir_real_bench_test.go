package runtime_test

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	ethlog "github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// Configure logger to emit warnings/errors to stdout during tests so MIR fallback logs are visible
func init() {
	h := ethlog.NewTerminalHandlerWithLevel(os.Stdout, ethlog.LevelWarn, false)
	ethlog.SetDefault(ethlog.NewLogger(h))
}

// USDT contract bytecode from BSCScan (truncated benches ignore method reverts)
const usdtHex = "0x608060405234801561001057600080fd5b506004361061012c5760003560e01c8063893d20e8116100ad578063a9059cbb11610071578063a9059cbb1461035a578063b09f126614610386578063d28d88521461038e578063dd62ed3e14610396578063f2fde38b146103c45761012c565b8063893d20e8146102dd5780638da5cb5b1461030157806395d89b4114610309578063a0712d6814610311578063a457c2d71461032e5761012c565b806332424aa3116100f457806332424aa31461025c578063395093511461026457806342966c681461029057806370a08231146102ad578063715018a6146102d35761012c565b806306fdde0314610131578063095ea7b3146101ae57806318160ddd146101ee57806323b872dd14610208578063313ce5671461023e575b600080fd5b6101396103ea565b6040805160208082528351818301528351919283929083019185019080838360005b8381101561017357818101518382015260200161015b565b50505050905090810190601f1680156101a05780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b6101da600480360360408110156101c457600080fd5b506001600160a01b038135169060200135610480565b604080519115158252519081900360200190f35b6101f661049d565b60408051918252519081900360200190f35b6101da6004803603606081101561021e57600080fd5b506001600160a01b038135811691602081013590911690604001356104a3565b610246610530565b6040805160ff9092168252519081900360200190f35b610246610539565b6101da6004803603604081101561027a57600080fd5b506001600160a01b038135169060200135610542565b6101da600480360360208110156102a657600080fd5b5035610596565b6101f6600480360360208110156102c357600080fd5b50356001600160a01b03166105b1565b6102db6105cc565b005b6102e5610680565b604080516001600160a01b039092168252519081900360200190f35b6102e561068f565b61013961069e565b6101da6004803603602081101561032757600080fd5b50356106ff565b6101da6004803603604081101561034457600080fd5b506001600160a01b03813516906020013561077c565b6101da6004803603604081101561037057600080fd5b506001600160a01b0381351690602001356107ea565b6101396107fe565b61013961088c565b6101f6600480360360408110156103ac57600080fd5b506001600160a01b03813581169160200135166108e7565b6102db600480360360208110156103da57600080fd5b50356001600160a01b0316610912565b60068054604080516020601f60026000196101006001881615020190951694909404938401819004810282018101909252828152606093909290918301828280156104765780601f1061044b57610100808354040283529160200191610476565b820191906000526020600020905b81548152906001019060200180831161045957829003601f168201915b5050505050905090565b600061049461048d610988565b848461098c565b50600192915050565b60035490565b60006104b0848484610a78565b610526846104bc610988565b6105218560405180606001604052806028815260200161100e602891396001600160a01b038a166000908152600260205260408120906104fa610988565b6001600160a01b03168152602081019190915260400160002054919063ffffffff610bd616565b61098c565b5060019392505050565b60045460ff1690565b60045460ff1681565b600061049461054f610988565b846105218560026000610560610988565b6001600160a01b03908116825260208083019390935260409182016000908120918c16815292529020549063ffffffff610c6d16565b60006105a96105a3610988565b83610cce565b506001919050565b6001600160a01b031660009081526001602052604090205490565b6105d4610988565b6000546001600160a01b03908116911614610636576040805162461bcd60e51b815260206004820181905260248201527f4f776e61626c653a2063616c6c6572206973206e6f7420746865206f776e6572604482015290519081900360640190fd5b600080546040516001600160a01b03909116907f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0908390a3600080546001600160a01b0319169055565b600061068a61068f565b905090565b6000546001600160a01b031690565b60058054604080516020601f60026000196101006001881615020190951694909404938401819004810282018101909252828152606093909290918301828280156104765780601f1061085957610100808354040283529160200191610884565b820191906000526020600020905b81548152906001019060200180831161045957829003601f168201915b505050505081565b6006805460408051602060026001851615610100026000190190941693909304601f810184900484028201840190925281815292918301828280156108845780601f1061085957610100808354040283529160200191610884565b6001600160a01b03918216600090815260026020908152604080832093909416825291909152205490565b61091a610988565b6000546001600160a01b0390811691161461097c576040805162461bcd60e51b815260206004820181905260248201527f4f776e61626c653a2063616c6c6572206973206e6f7420746865206f776e6572604482015290519081900360640190fd5b61098581610ebc565b50565b3390565b6001600160a01b0383166109d15760405162461bcd60e51b8152600401808060200182810382526024815260200180610fc46024913960400191505060405180910390fd5b6001600160a01b038216610a165760405162461bcd60e51b81526004018080602001828103825260228152602001806110e76022913960400191505060405180910390fd5b6001600160a01b03808416600081815260026020908152604080832094871680845294825291829020859055815185815291517f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259281900390910190a3505050565b6001600160a01b038316610abd5760405162461bcd60e51b8152600401808060200182810382526025815260200180610f9f6025913960400191505060405180910390fd5b6001600160a01b038216610b025760405162461bcd60e51b815260040180806020018281038252602381526020018061105c6023913960400191505060405180910390fd5b610b4581604051806060016040528060268152602001611036602691396001600160a01b038616600090815260016020526040902054919063ffffffff610bd616565b6001600160a01b038085166000908152600160205260408082209390935590841681522054610b7a908263ffffffff610c6d16565b6001600160a01b0380841660008181526001602090815260409182902094909455805185815290519193928716927fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9281900390910190a3505050565b60008184841115610c655760405162461bcd60e51b81526004018080602001828103825283818151815260200191508051906020019080838360005b83811015610c2a578181015183820152602001610c12565b50505050905090810190601f168015610c575780820380516001836020036101000a031916815260200191505b509250505060405180910390fd5b505050900390565b600082820183811015610cc7576040805162461bcd60e51b815260206004820152601b60248201527f536166654d6174683a206164646974696f6e206f766572666c6f770000000000604482015290519081900360640190fd5b9392505050565b6001600160a01b038216610d135760405162461bcd60e51b81526004018080602001828103825260218152602001806110a46021913960400191505060405180910390fd5b610d56816040518060600160405280602281526020016110c5602291396001600160a01b038516600090815260016020526040902054919063ffffffff610bd616565b6001600160a01b038316600090815260016020526040902055600354610d82908263ffffffff610f5c16565b6003556040805182815290516000916001600160a01b038516917fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181900360200190a35050565b6001600160a01b038216610e255760405162461bcd60e51b815260040180806020018281038252601f60248201527f42455032303a206d696e7420746f20746865207a65726f206164647265737300604482015290519081900360640190fd5b600354610e38908263ffffffff610c6d16565b6003556001600160a01b038316600090815260016020526040902054610e64908263ffffffff610c6d16565b6001600160a01b03831660008181526001602090815260408083209490945583518581529351929391927fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9281900390910190a35050565b6001600160a01b038116610f015760405162461bcd60e51b8152600401808060200182810382526026815260200180610fe86026913960400191505060405180910390fd5b600080546040516001600160a01b03808516939216917f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e091a3600080546001600160a01b0319166001600160a01b0392909216919091179055565b6000610cc783836040518060400160405280601e81526020017f536166654d6174683a207375627472616374696f6e206f766572666c6f770000815250610bd656fe42455032303a207472616e736665722066726f6d20746865207a65726f206164647265737342455032303a20617070726f76652066726f6d20746865207a65726f20616464726573734f776e61626c653a206e6577206f776e657220697320746865207a65726f206164647265737342455032303a207472616e7366657220616d6f756e74206578636565647320616c6c6f77616e636542455032303a207472616e7366657220616d6f756e7420657863656564732062616c616e636542455032303a207472616e7366657220746f20746865207a65726f206164647265737342455032303a2064656372656173656420616c6c6f77616e63652062656c6f77207a65726f42455032303a206275726e2066726f6d20746865207a65726f206164647265737342455032303a206275726e20616d6f756e7420657863656564732062616c616e636542455032303a20617070726f766520746f20746865207a65726f2061646472657373a265627a7a72315820cbbd570ae478f6b7abf9c9a5c8c6884cf3f64dded74f7ec3e9b6d0b41122eaff64736f6c63430005100032"

func BenchmarkMIRVsEVM_USDT(b *testing.B) {
	// Base and MIR configs
	cfgBase := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(1), Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: false}}
	cfgMIR := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(1), Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: true}}
	compiler.EnableOpcodeParse()

	// decode bytecode
	realCode, err := hex.DecodeString(usdtHex[2:])
	if err != nil {
		b.Fatalf("decode USDT hex: %v", err)
	}

	zeroAddress := make([]byte, 32)
	oneUint := make([]byte, 32)
	oneUint[31] = 1

	methods := []struct {
		name     string
		selector []byte
		args     [][]byte
	}{
		{"name", []byte{0x06, 0xfd, 0xde, 0x03}, nil},
		{"symbol", []byte{0x95, 0xd8, 0x9b, 0x41}, nil},
		{"decimals", []byte{0x31, 0x3c, 0xe5, 0x67}, nil},
		{"totalSupply", []byte{0x18, 0x16, 0x0d, 0xdd}, nil},
		{"balanceOf", []byte{0x70, 0xa0, 0x82, 0x31}, [][]byte{zeroAddress}},
		{"allowance", []byte{0x39, 0x50, 0x93, 0x51}, [][]byte{zeroAddress, zeroAddress}},
		{"approve", []byte{0x09, 0x5e, 0xa7, 0xb3}, [][]byte{zeroAddress, oneUint}},
		{"transfer", []byte{0xa9, 0x05, 0x9c, 0xbb}, [][]byte{zeroAddress, oneUint}},
	}

	for _, m := range methods {
		input := append([]byte{}, m.selector...)
		for _, arg := range m.args {
			input = append(input, arg...)
		}

		b.Run("EVM_Base_"+m.name, func(b *testing.B) {
			if cfgBase.State == nil {
				cfgBase.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			}
			evm := runtime.NewEnv(cfgBase)
			address := common.BytesToAddress([]byte("contract_usdt"))
			sender := vm.AccountRef(cfgBase.Origin)
			evm.StateDB.CreateAccount(address)
			evm.StateDB.SetCode(address, realCode)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _, _ = evm.Call(sender, address, input, cfgBase.GasLimit, uint256.MustFromBig(cfgBase.Value))
			}
		})

		b.Run("MIR_Interpreter_"+m.name, func(b *testing.B) {
			if cfgMIR.State == nil {
				cfgMIR.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			}
			evm := runtime.NewEnv(cfgMIR)
			address := common.BytesToAddress([]byte("contract_usdt"))
			sender := vm.AccountRef(cfgMIR.Origin)
			evm.StateDB.CreateAccount(address)
			evm.StateDB.SetCode(address, realCode)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _, _ = evm.Call(sender, address, input, cfgMIR.GasLimit, uint256.MustFromBig(cfgMIR.Value))
			}
		})
	}
}

// WBNB contract bytecode from BSCScan
const wbnbHex = "0x6060604052600436106100af576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff16806306fdde03146100b9578063095ea7b31461014757806318160ddd146101a157806323b872dd146101ca5780632e1a7d4d14610243578063313ce5671461026657806370a082311461029557806395d89b41146102e2578063a9059cbb14610370578063d0e30db0146103ca578063dd62ed3e146103d4575b6100b7610440565b005b34156100c457600080fd5b6100cc6104dd565b6040518080602001828103825283818151815260200191508051906020019080838360005b8381101561010c5780820151818401526020810190506100f1565b50505050905090810190601f1680156101395780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b341561015257600080fd5b610187600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803590602001909190505061057b565b604051808215151515815260200191505060405180910390f35b34156101ac57600080fd5b6101b461066d565b6040518082815260200191505060405180910390f35b34156101d557600080fd5b610229600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803590602001909190505061068c565b604051808215151515815260200191505060405180910390f35b341561024e57600080fd5b61026460048080359060200190919050506109d9565b005b341561027157600080fd5b610279610b05565b604051808260ff1660ff16815260200191505060405180910390f35b34156102a057600080fd5b6102cc600480803573ffffffffffffffffffffffffffffffffffffffff16906020019091905050610b18565b6040518082815260200191505060405180910390f35b34156102ed57600080fd5b6102f5610b30565b6040518080602001828103825283818151815260200191508051906020019080838360005b8381101561033557808201518184015260208101905061031a565b50505050905090810190601f1680156103625780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b341561037b57600080fd5b6103b0600480803573ffffffffffffffffffffffffffffffffffffffff16906020019091908035906020019091905050610bce565b604051808215151515815260200191505060405180910390f35b6103d2610440565b005b34156103df57600080fd5b61042a600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803573ffffffffffffffffffffffffffffffffffffffff16906020019091905050610be3565b6040518082815260200191505060405180910390f35b34600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055503373ffffffffffffffffffffffffffffffffffffffff167fe1fffcc4923d04b559f4d29a8bfc6cda04eb5b0d3c460751c2402c5c5cc9109c346040518082815260200191505060405180910390a2565b60008054600181600116156101000203166002900480601f0160208091040260200160405190810160405280929190818152602001828054600181600116156101000203166002900480156105735780601f1061054857610100808354040283529160200191610573565b820191906000526020600020905b81548152906001019060200180831161055657829003601f168201915b505050505081565b600081600460003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055508273ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff167f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925846040518082815260200191505060405180910390a36001905092915050565b60003073ffffffffffffffffffffffffffffffffffffffff1631905090565b600081600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002054101515156106dc57600080fd5b3373ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff16141580156107b457507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205414155b156108cf5781600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020541015151561084457600080fd5b81600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055505b81600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000206000828254039250508190555081600360008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055508273ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef846040518082815260200191505060405180910390a3600190509392505050565b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205410151515610a2757600080fd5b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055503373ffffffffffffffffffffffffffffffffffffffff166108fc829081150290604051600060405180830381858888f193505050501515610ab457600080fd5b3373ffffffffffffffffffffffffffffffffffffffff167f7fcf532c15f0a6db0bd6d0e038bea71d30d808c7d98cb3bf7268a95bf5081b65826040518082815260200191505060405180910390a250565b600260009054906101000a900460ff1681565b60036020528060005260406000206000915090505481565b60018054600181600116156101000203166002900480601f016020809104026020016040519081016040528092919081815260200182805460018160011615610100020316600290048015610bc65780601f10610b9b57610100808354040283529160200191610bc6565b820191906000526020600020905b815481529060010190602001808311610ba957829003601f168201915b505050505081565b6000610bdb33848461068c565b905092915050565b60046020528160005260406000206020528060005260406000206000915091505054815600a165627a7a72305820bcf3db16903185450bc04cb54da92f216e96710cce101fd2b4b47d5b70dc11e00029"

func BenchmarkMIRVsEVM_WBNB(b *testing.B) {
	only := strings.ToUpper(os.Getenv("ONLY"))
	cfgBase := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(1), Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: false}}
	cfgMIR := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(1), Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: true}}
	compiler.EnableOpcodeParse()

	code, err := hex.DecodeString(wbnbHex[2:])
	if err != nil {
		b.Fatalf("decode WBNB hex: %v", err)
	}
	zeroAddress := make([]byte, 32)
	oneUint := make([]byte, 32)
	oneUint[31] = 1

	methods := []struct {
		name     string
		selector []byte
		args     [][]byte
	}{
		{"name", []byte{0x06, 0xfd, 0xde, 0x03}, nil},
		{"symbol", []byte{0x95, 0xd8, 0x9b, 0x41}, nil},
		{"decimals", []byte{0x31, 0x3c, 0xe5, 0x67}, nil},
		{"totalSupply", []byte{0x18, 0x16, 0x0d, 0xdd}, nil},
		{"balanceOf", []byte{0x70, 0xa0, 0x82, 0x31}, [][]byte{zeroAddress}},
		{"deposit", []byte{0xd0, 0xe3, 0x0d, 0xb0}, nil},
		{"withdraw", []byte{0x2e, 0x1a, 0x7d, 0x4d}, [][]byte{oneUint}},
		{"transfer", []byte{0xa9, 0x05, 0x9c, 0xbb}, [][]byte{zeroAddress, oneUint}},
	}

	for _, m := range methods {
		input := append([]byte{}, m.selector...)
		for _, arg := range m.args {
			input = append(input, arg...)
		}

		if only == "MIR" {
			// skip EVM
		} else {
			b.Run("EVM_Base_"+m.name, func(b *testing.B) {
				if cfgBase.State == nil {
					cfgBase.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
				}
				evm := runtime.NewEnv(cfgBase)
				address := common.BytesToAddress([]byte("contract_wbnb"))
				sender := vm.AccountRef(cfgBase.Origin)
				evm.StateDB.CreateAccount(address)
				evm.StateDB.SetCode(address, code)

				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, _, _ = evm.Call(sender, address, input, cfgBase.GasLimit, uint256.MustFromBig(cfgBase.Value))
				}
			})
		}

		if only == "EVM" {
			// skip MIR
		} else {
			b.Run("MIR_Interpreter_"+m.name, func(b *testing.B) {
				if cfgMIR.State == nil {
					cfgMIR.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
				}
				evm := runtime.NewEnv(cfgMIR)
				address := common.BytesToAddress([]byte("contract_wbnb"))
				sender := vm.AccountRef(cfgMIR.Origin)
				evm.StateDB.CreateAccount(address)
				evm.StateDB.SetCode(address, code)

				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, _, _ = evm.Call(sender, address, input, cfgMIR.GasLimit, uint256.MustFromBig(cfgMIR.Value))
				}
			})
		}
	}
}

// TestCountUSDTMIR generates MIR CFG for the USDT bytecode and reports total MIR instructions
func TestCountUSDTMIR(t *testing.T) {
	// Decode USDT bytecode
	realCode, err := hex.DecodeString(usdtHex[2:])
	if err != nil {
		t.Fatalf("decode USDT hex: %v", err)
	}
	// Generate CFG
	cfg, err := compiler.GenerateMIRCFG(common.Hash{}, realCode)
	if err != nil {
		t.Logf("GenerateMIRCFG: %v (skipping count)", err)
		return
	}
	// Count MIR instructions across all basic blocks
	total := 0
	blocks := cfg.GetBasicBlocks()
	for _, bb := range blocks {
		if bb == nil {
			continue
		}
		total += len(bb.Instructions())
	}
	t.Logf("USDT MIR: %d basic blocks, %d total MIR instructions", len(blocks), total)
}

// TestDumpUSDTMIRAndEVM lists MIR operations (by symbolic names) and all EVM opcodes for USDT
func TestDumpUSDTMIRAndEVM(t *testing.T) {
	// Decode USDT bytecode
	realCode, err := hex.DecodeString(usdtHex[2:])
	if err != nil {
		t.Fatalf("decode USDT hex: %v", err)
	}
	// Generate CFG and collect MIR ops
	cfg, err := compiler.GenerateMIRCFG(common.Hash{}, realCode)
	if err != nil {
		t.Logf("GenerateMIRCFG: %v (skipping MIR dump)", err)
		cfg = nil
	}
	// Map MIR opcode to name using fmt.Sprintf fallback
	mirName := func(op compiler.MirOperation) string {
		switch op {
		case compiler.MirSTOP:
			return "MirSTOP"
		case compiler.MirADD:
			return "MirADD"
		case compiler.MirMUL:
			return "MirMUL"
		case compiler.MirSUB:
			return "MirSUB"
		case compiler.MirDIV:
			return "MirDIV"
		case compiler.MirSDIV:
			return "MirSDIV"
		case compiler.MirMOD:
			return "MirMOD"
		case compiler.MirSMOD:
			return "MirSMOD"
		case compiler.MirADDMOD:
			return "MirADDMOD"
		case compiler.MirMULMOD:
			return "MirMULMOD"
		case compiler.MirEXP:
			return "MirEXP"
		case compiler.MirSIGNEXT:
			return "MirSIGNEXT"
		case compiler.MirLT:
			return "MirLT"
		case compiler.MirGT:
			return "MirGT"
		case compiler.MirSLT:
			return "MirSLT"
		case compiler.MirSGT:
			return "MirSGT"
		case compiler.MirEQ:
			return "MirEQ"
		case compiler.MirISZERO:
			return "MirISZERO"
		case compiler.MirAND:
			return "MirAND"
		case compiler.MirOR:
			return "MirOR"
		case compiler.MirXOR:
			return "MirXOR"
		case compiler.MirNOT:
			return "MirNOT"
		case compiler.MirBYTE:
			return "MirBYTE"
		case compiler.MirSHL:
			return "MirSHL"
		case compiler.MirSHR:
			return "MirSHR"
		case compiler.MirSAR:
			return "MirSAR"
		case compiler.MirKECCAK256:
			return "MirKECCAK256"
		case compiler.MirADDRESS:
			return "MirADDRESS"
		case compiler.MirBALANCE:
			return "MirBALANCE"
		case compiler.MirORIGIN:
			return "MirORIGIN"
		case compiler.MirCALLER:
			return "MirCALLER"
		case compiler.MirCALLVALUE:
			return "MirCALLVALUE"
		case compiler.MirCALLDATALOAD:
			return "MirCALLDATALOAD"
		case compiler.MirCALLDATASIZE:
			return "MirCALLDATASIZE"
		case compiler.MirCALLDATACOPY:
			return "MirCALLDATACOPY"
		case compiler.MirCODESIZE:
			return "MirCODESIZE"
		case compiler.MirCODECOPY:
			return "MirCODECOPY"
		case compiler.MirGASPRICE:
			return "MirGASPRICE"
		case compiler.MirEXTCODESIZE:
			return "MirEXTCODESIZE"
		case compiler.MirEXTCODECOPY:
			return "MirEXTCODECOPY"
		case compiler.MirRETURNDATASIZE:
			return "MirRETURNDATASIZE"
		case compiler.MirRETURNDATACOPY:
			return "MirRETURNDATACOPY"
		case compiler.MirEXTCODEHASH:
			return "MirEXTCODEHASH"
		case compiler.MirBLOCKHASH:
			return "MirBLOCKHASH"
		case compiler.MirCOINBASE:
			return "MirCOINBASE"
		case compiler.MirTIMESTAMP:
			return "MirTIMESTAMP"
		case compiler.MirNUMBER:
			return "MirNUMBER"
		case compiler.MirDIFFICULTY:
			return "MirDIFFICULTY"
		case compiler.MirGASLIMIT:
			return "MirGASLIMIT"
		case compiler.MirCHAINID:
			return "MirCHAINID"
		case compiler.MirSELFBALANCE:
			return "MirSELFBALANCE"
		case compiler.MirBASEFEE:
			return "MirBASEFEE"
		case compiler.MirBLOBHASH:
			return "MirBLOBHASH"
		case compiler.MirBLOBBASEFEE:
			return "MirBLOBBASEFEE"
		case compiler.MirMLOAD:
			return "MirMLOAD"
		case compiler.MirMSTORE:
			return "MirMSTORE"
		case compiler.MirMSTORE8:
			return "MirMSTORE8"
		case compiler.MirSLOAD:
			return "MirSLOAD"
		case compiler.MirSSTORE:
			return "MirSSTORE"
		case compiler.MirJUMP:
			return "MirJUMP"
		case compiler.MirJUMPI:
			return "MirJUMPI"
		case compiler.MirPC:
			return "MirPC"
		case compiler.MirMSIZE:
			return "MirMSIZE"
		case compiler.MirGAS:
			return "MirGAS"
		case compiler.MirJUMPDEST:
			return "MirJUMPDEST"
		case compiler.MirTLOAD:
			return "MirTLOAD"
		case compiler.MirTSTORE:
			return "MirTSTORE"
		case compiler.MirMCOPY:
			return "MirMCOPY"
		case compiler.MirLOG0:
			return "MirLOG0"
		case compiler.MirLOG1:
			return "MirLOG1"
		case compiler.MirLOG2:
			return "MirLOG2"
		case compiler.MirLOG3:
			return "MirLOG3"
		case compiler.MirLOG4:
			return "MirLOG4"
		case compiler.MirCREATE:
			return "MirCREATE"
		case compiler.MirCALL:
			return "MirCALL"
		case compiler.MirCALLCODE:
			return "MirCALLCODE"
		case compiler.MirRETURN:
			return "MirRETURN"
		case compiler.MirDELEGATECALL:
			return "MirDELEGATECALL"
		case compiler.MirCREATE2:
			return "MirCREATE2"
		case compiler.MirSTATICCALL:
			return "MirSTATICCALL"
		case compiler.MirREVERT:
			return "MirREVERT"
		case compiler.MirRETURNDATALOAD:
			return "MirRETURNDATALOAD"
		case compiler.MirINVALID:
			return "MirINVALID"
		case compiler.MirSELFDESTRUCT:
			return "MirSELFDESTRUCT"
		case compiler.MirNOP:
			return "MirNOP"
		default:
			return fmt.Sprintf("Mir(0x%02x)", byte(op))
		}
	}

	if cfg != nil {
		// Log MIR ops
		t.Log("MIR operations:")
		mirTotal := 0
		for _, bb := range cfg.GetBasicBlocks() {
			if bb == nil {
				continue
			}
			for _, m := range bb.Instructions() {
				if m == nil {
					continue
				}
				t.Logf("  %s", mirName(m.Op()))
				mirTotal++
			}
		}
		t.Logf("Total MIR ops: %d", mirTotal)

		// Dump MIR CFG with per-block MIR instructions and corresponding EVM bytecode window
		t.Log("MIR CFG:")
		// (disAssemble helper removed)
		findIndex := func(list []*compiler.MIRBasicBlock, target *compiler.MIRBasicBlock) int {
			for i, b := range list {
				if b == target {
					return i
				}
			}
			return -1
		}
		blocks := cfg.GetBasicBlocks()
		for i, bb := range blocks {
			if bb == nil {
				continue
			}
			parents := []int{}
			for _, p := range bb.Parents() {
				parents = append(parents, findIndex(blocks, p))
			}
			children := []int{}
			for _, c := range bb.Children() {
				children = append(children, findIndex(blocks, c))
			}
			// EVM byte window from block first PC
			first := int(bb.FirstPC())
			end := first + 32
			if end > len(realCode) {
				end = len(realCode)
			}
			byteWindow := realCode[first:end]
			t.Logf("Block %d: PC[%d,%d) size=%d parents=%v children=%v", i, bb.FirstPC(), bb.LastPC(), len(bb.Instructions()), parents, children)
			t.Logf("  EVM bytes[%d:%d]: %x", first, end, byteWindow)
			for mIdx, m := range bb.Instructions() {
				if m == nil {
					continue
				}
				opStr := m.Op().String()
				depth := m.GenStackDepth()
				// Use recorded pc if available to show corresponding EVM opcode
				evmStr := ""
				if m != nil {
					// best-effort: compute pc as block firstPC + instruction index when available
					p := int(bb.FirstPC()) + mIdx // fallback heuristic
					if p >= 0 && p < len(realCode) {
						evmOp := vm.OpCode(realCode[p])
						evmStr = fmt.Sprintf("%s (0x%02x) @0x%04x", evmOp.String(), byte(evmOp), p)
					}
				}
				if evmStr == "" {
					t.Logf("    %s stack=%d", opStr, depth)
				} else {
					t.Logf("    %s stack=%d  evm=%s", opStr, depth, evmStr)
				}
			}
		}
	}

	// Disassemble and list all EVM opcodes by name
	t.Log("EVM opcodes:")
	for pc := 0; pc < len(realCode); {
		op := vm.OpCode(realCode[pc])
		t.Logf("0x%04x: %s (0x%02x)", pc, op.String(), realCode[pc])
		pc++
		if op >= vm.PUSH1 && op <= vm.PUSH32 {
			n := int(op - vm.PUSH1 + 1)
			pc += n
		}
	}
}

// TestUSDT_MIRVsEVM_Parity compares outputs of MIR vs base EVM for key USDT selectors
func TestUSDT_MIRVsEVM_Parity(t *testing.T) {
	// Decode USDT runtime bytecode
	code, err := hex.DecodeString(usdtHex[2:])
	if err != nil {
		t.Fatalf("decode USDT hex: %v", err)
	}

	// Base and MIR configs
	// Use BSC chain config at/after London so SHR/SHL/SAR and others are enabled
	compatBlock := new(big.Int).Set(params.BSCChainConfig.LondonBlock)
	base := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: false}}
	mir := &runtime.Config{ChainConfig: params.BSCChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: compatBlock, Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: true}}
	compiler.EnableOpcodeParse()

	// Prepare simple args (zero address, 1)
	zeroAddress := make([]byte, 32)
	oneUint := make([]byte, 32)
	oneUint[31] = 1

	methods := []struct {
		name     string
		selector []byte
		args     [][]byte
	}{
		{"name", []byte{0x06, 0xfd, 0xde, 0x03}, nil},
		{"symbol", []byte{0x95, 0xd8, 0x9b, 0x41}, nil},
		{"decimals", []byte{0x31, 0x3c, 0xe5, 0x67}, nil},
		{"totalSupply", []byte{0x18, 0x16, 0x0d, 0xdd}, nil},
		{"balanceOf", []byte{0x70, 0xa0, 0x82, 0x31}, [][]byte{zeroAddress}},
		{"allowance", []byte{0x39, 0x50, 0x93, 0x51}, [][]byte{zeroAddress, zeroAddress}},
		{"approve", []byte{0x09, 0x5e, 0xa7, 0xb3}, [][]byte{zeroAddress, oneUint}},
		{"transfer", []byte{0xa9, 0x05, 0x9c, 0xbb}, [][]byte{zeroAddress, oneUint}},
	}

	// Helper: create env and run
	run := func(cfg *runtime.Config, label string, input []byte) ([]byte, error) {
		if cfg.State == nil {
			cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfg)
		address := common.BytesToAddress([]byte(label))
		sender := vm.AccountRef(cfg.Origin)
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, code)
		ret, _, err := evm.Call(sender, address, input, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		return ret, err
	}

	for _, m := range methods {
		input := append([]byte{}, m.selector...)
		for _, arg := range m.args {
			input = append(input, arg...)
		}

		rb, errB := run(base, "usdt_parity_b", input)
		rm, errM := run(mir, "usdt_parity_m", input)

		// Compare success/failure and return data
		if (errB != nil) != (errM != nil) {
			t.Fatalf("%s: error mismatch base=%v mir=%v", m.name, errB, errM)
		}
		if string(rb) != string(rm) {
			t.Fatalf("%s: return mismatch base=%x mir=%x", m.name, rb, rm)
		}
	}
}

// TestUSDT_Transfer_EVMvsMIR runs only the ERC20 transfer selector against
// the USDT runtime bytecode and compares EVM vs MIR outputs and error parity.
func TestUSDT_Transfer_EVMvsMIR(t *testing.T) {
	// Decode USDT runtime bytecode
	code, err := hex.DecodeString(usdtHex[2:])
	if err != nil {
		t.Fatalf("decode USDT hex: %v", err)
	}

	// Base and MIR configs
	// Use a post-Constantinople/London block so SHR and friends are enabled and MIR can execute
	base := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(15000000), Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: false}}
	mir := &runtime.Config{ChainConfig: params.MainnetChainConfig, GasLimit: 10_000_000, Origin: common.Address{}, BlockNumber: big.NewInt(15000000), Value: big.NewInt(0), EVMConfig: vm.Config{EnableOpcodeOptimizations: true}}
	compiler.EnableOpcodeParse()

	// Prepare transfer(to, amount)
	zeroAddress := make([]byte, 32)
	oneUint := make([]byte, 32)
	oneUint[31] = 1
	selector := []byte{0xa9, 0x05, 0x9c, 0xbb}
	input := append([]byte{}, selector...)
	input = append(input, zeroAddress...)
	input = append(input, oneUint...)

	// Helper: create env and run with optional tracers
	run := func(cfg *runtime.Config, label string, evmTracer *tracing.Hooks, mirTracer func(compiler.MirOperation)) ([]byte, error) {
		// Attach EVM tracer if provided
		if evmTracer != nil {
			cfg.EVMConfig.Tracer = evmTracer
		}
		// Install global MIR tracer so newly created MIR interpreters inherit it
		if mirTracer != nil {
			compiler.SetGlobalMIRTracer(mirTracer)
			defer compiler.SetGlobalMIRTracer(nil)
		}
		if cfg.State == nil {
			cfg.State, _ = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
		}
		evm := runtime.NewEnv(cfg)
		address := common.BytesToAddress([]byte(label))
		sender := vm.AccountRef(cfg.Origin)
		evm.StateDB.CreateAccount(address)
		evm.StateDB.SetCode(address, code)
		ret, _, err := evm.Call(sender, address, input, cfg.GasLimit, uint256.MustFromBig(cfg.Value))
		return ret, err
	}

	// Define EVM and MIR tracers (execution-time)
	evmTracer := &tracing.Hooks{
		OnOpcode: func(pc uint64, opcode byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			t.Logf("EVM opcode pc=%d op=%s", pc, vm.OpCode(opcode))
		},
	}
	mirTracer := func(op compiler.MirOperation) {
		switch op {
		case compiler.MirADD:
			t.Log("MIR exec: MirADD")
		case compiler.MirMUL:
			t.Log("MIR exec: MirMUL")
		case compiler.MirSUB:
			t.Log("MIR exec: MirSUB")
		case compiler.MirDIV:
			t.Log("MIR exec: MirDIV")
		case compiler.MirSDIV:
			t.Log("MIR exec: MirSDIV")
		case compiler.MirMOD:
			t.Log("MIR exec: MirMOD")
		case compiler.MirSMOD:
			t.Log("MIR exec: MirSMOD")
		case compiler.MirADDMOD:
			t.Log("MIR exec: MirADDMOD")
		case compiler.MirMULMOD:
			t.Log("MIR exec: MirMULMOD")
		case compiler.MirEXP:
			t.Log("MIR exec: MirEXP")
		case compiler.MirISZERO:
			t.Log("MIR exec: MirISZERO")
		case compiler.MirAND:
			t.Log("MIR exec: MirAND")
		case compiler.MirOR:
			t.Log("MIR exec: MirOR")
		case compiler.MirXOR:
			t.Log("MIR exec: MirXOR")
		case compiler.MirBYTE:
			t.Log("MIR exec: MirBYTE")
		case compiler.MirSHL:
			t.Log("MIR exec: MirSHL")
		case compiler.MirSHR:
			t.Log("MIR exec: MirSHR")
		case compiler.MirSAR:
			t.Log("MIR exec: MirSAR")
		case compiler.MirKECCAK256:
			t.Log("MIR exec: MirKECCAK256")
		case compiler.MirCALLDATALOAD:
			t.Log("MIR exec: MirCALLDATALOAD")
		case compiler.MirCALLDATASIZE:
			t.Log("MIR exec: MirCALLDATASIZE")
		case compiler.MirCALLDATACOPY:
			t.Log("MIR exec: MirCALLDATACOPY")
		case compiler.MirMLOAD:
			t.Log("MIR exec: MirMLOAD")
		case compiler.MirMSTORE:
			t.Log("MIR exec: MirMSTORE")
		case compiler.MirMSTORE8:
			t.Log("MIR exec: MirMSTORE8")
		case compiler.MirSLOAD:
			t.Log("MIR exec: MirSLOAD")
		case compiler.MirSSTORE:
			t.Log("MIR exec: MirSSTORE")
		case compiler.MirRETURN:
			t.Log("MIR exec: MirRETURN")
		case compiler.MirREVERT:
			t.Log("MIR exec: MirREVERT")
		default:
			t.Logf("MIR exec: 0x%x", byte(op))
		}
	}

	// Execute on both interpreters ONCE, with tracers attached
	rb, errB := run(base, "usdt_transfer_b", evmTracer, nil)
	t.Logf("rb: %x", rb)
	rm, errM := run(mir, "usdt_transfer_m", evmTracer, mirTracer)
	t.Logf("rm: %x", rm)
	// Compare success/failure and return data
	if (errB != nil) != (errM != nil) {
		t.Fatalf("transfer: error mismatch base=%v mir=%v", errB, errM)
	}
	if string(rb) != string(rm) {
		t.Fatalf("transfer: return mismatch base=%x mir=%x", rb, rm)
	}

}

// TestUSDT_StackAroundDup6 prints a window of opcodes and an approximate
// stack depth trace around the failing pc to diagnose insufficient depth.
func TestUSDT_StackAroundDup6(t *testing.T) {
	// target pc where DUP6 was reported
	target := 2283 // 0x08EB
	start := target - 32
	end := target + 32

	code, err := hex.DecodeString(usdtHex[2:])
	if err != nil {
		t.Fatalf("decode USDT hex: %v", err)
	}

	depth := 0
	for pc := 0; pc < len(code); {
		op := vm.OpCode(code[pc])
		// Only print the window
		if pc >= start && pc <= end {
			t.Logf("pc=0x%04x op=%-12s (0x%02x) depth=%d", pc, op.String(), byte(op), depth)
		}
		// Update depth approximately
		switch {
		case op >= vm.PUSH1 && op <= vm.PUSH32:
			depth++
			n := int(op - vm.PUSH1 + 1)
			pc += 1 + n
			continue
		case op >= vm.DUP1 && op <= vm.DUP16:
			// Requires depth >= N; adds one
			n := int(op - vm.DUP1 + 1)
			if depth < n {
				if pc >= start-8 && pc <= end {
					t.Logf("  !! insufficient depth for %s need=%d have=%d", op.String(), n, depth)
				}
			}
			depth++
		case op >= vm.SWAP1 && op <= vm.SWAP16:
			// requires depth >= n+1; no depth change
			n := int(op - vm.SWAP1 + 1)
			if depth <= n {
				if pc >= start-8 && pc <= end {
					t.Logf("  !! insufficient depth for %s need=%d have=%d", op.String(), n+1, depth)
				}
			}
		default:
			switch op {
			case vm.POP:
				if depth > 0 {
					depth--
				}
			case vm.ADD, vm.MUL, vm.SUB, vm.DIV, vm.SDIV, vm.MOD, vm.SMOD,
				vm.LT, vm.GT, vm.SLT, vm.SGT, vm.EQ, vm.AND, vm.OR, vm.XOR,
				vm.BYTE, vm.SHL, vm.SHR, vm.SAR:
				if depth >= 2 {
					depth -= 1
				} // pop2 push1
				if depth == 1 {
					depth = 0
				}
			case vm.ISZERO, vm.NOT, vm.BALANCE, vm.CALLDATALOAD, vm.MLOAD,
				vm.CALLDATASIZE, vm.CODESIZE, vm.GASPRICE, vm.EXTCODESIZE,
				vm.RETURNDATASIZE, vm.EXTCODEHASH, vm.PC, vm.MSIZE, vm.GAS,
				vm.ADDRESS, vm.ORIGIN, vm.CALLER, vm.CALLVALUE, vm.CHAINID,
				vm.SELFBALANCE, vm.BASEFEE:
				// pop1 push1 or pure push1; treat as +1 or 0 conservatively
				if op == vm.CALLVALUE || op == vm.CALLDATASIZE || op == vm.CODESIZE || op == vm.GASPRICE || op == vm.EXTCODESIZE || op == vm.RETURNDATASIZE || op == vm.EXTCODEHASH || op == vm.PC || op == vm.MSIZE || op == vm.GAS || op == vm.ADDRESS || op == vm.ORIGIN || op == vm.CALLER || op == vm.CHAINID || op == vm.SELFBALANCE || op == vm.BASEFEE {
					depth++
				}
			case vm.MSTORE, vm.MSTORE8, vm.SSTORE, vm.RETURN, vm.REVERT:
				// pop2 (RETURN/REVERT pop2: offset,size)
				if depth >= 2 {
					depth -= 2
				} else {
					depth = 0
				}
			case vm.SLOAD:
				if depth >= 1 { /* pop1 push1 -> no change */
				}
			case vm.JUMP:
				if depth >= 1 {
					depth -= 1
				} else {
					depth = 0
				}
			case vm.JUMPI:
				if depth >= 2 {
					depth -= 2
				} else {
					depth = 0
				}
			case vm.JUMPDEST:
				// no change
			case vm.LOG0:
				if depth >= 2 {
					depth -= 2
				} else {
					depth = 0
				}
			case vm.LOG1:
				if depth >= 3 {
					depth -= 3
				} else {
					depth = 0
				}
			case vm.LOG2:
				if depth >= 4 {
					depth -= 4
				} else {
					depth = 0
				}
			case vm.LOG3:
				if depth >= 5 {
					depth -= 5
				} else {
					depth = 0
				}
			case vm.LOG4:
				if depth >= 6 {
					depth -= 6
				} else {
					depth = 0
				}
			}
		}
		pc++
	}
}
