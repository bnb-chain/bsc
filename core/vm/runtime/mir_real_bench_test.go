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

// USDT contract runtime bytecode (implementation behind the proxy at 0x524bc9...90bc)
const usdtHex = "0x608060405234801561001057600080fd5b50600436106101425760003560e01c806384b0196e116100b8578063a18cd7c61161007c578063a18cd7c6146102a8578063a457c2d7146102bb578063a9059cbb146102ce578063c71f4615146102e1578063d505accf146102f4578063dd62ed3e1461030757600080fd5b806384b0196e146102365780638da5cb5b1461025157806395d89b411461026c5780639a8a0592146102745780639dc29fac1461029557600080fd5b80633644e5151161010a5780633644e515146101c257806339509351146101ca5780633d6c043b146101dd57806340c10f19146101e557806370a08231146101fa5780637ecebe001461022357600080fd5b806306fdde0314610147578063095ea7b31461016557806318160ddd1461018857806323b872dd1461019a578063313ce567146101ad575b600080fd5b61014f610340565b60405161015c91906118d8565b60405180910390f35b6101786101733660046116aa565b6103d4565b604051901515815260200161015c565b6003545b60405190815260200161015c565b6101786101a8366004611606565b6103ea565b60045460405160ff909116815260200161015c565b61018c6104a0565b6101786101d83660046116aa565b6104af565b60085461018c565b6101f86101f33660046116aa565b6104e6565b005b61018c6102083660046115b3565b6001600160a01b031660009081526005602052604090205490565b61018c6102313660046115b3565b61051e565b61023e61053e565b60405161015c9796959493929190611843565b6007546040516001600160a01b03909116815260200161015c565b61014f61059c565b600754600160a81b900461ffff1660405161ffff909116815260200161015c565b6101f86102a33660046116aa565b6105ae565b6101f86102b63660046116d3565b6105e2565b6101786102c93660046116aa565b6106bc565b6101786102dc3660046116aa565b610757565b6101f86102ef366004611744565b610764565b6101f8610302366004611641565b6107e7565b61018c6103153660046115d4565b6001600160a01b03918216600090815260066020908152604080832093909416825291909152205490565b606060008001805461035190611981565b80601f016020809104026020016040519081016040528092919081815260200182805461037d90611981565b80156103ca5780601f1061039f576101008083540402835291602001916103ca565b820191906000526020600020905b8154815290600101906020018083116103ad57829003601f168201915b5050505050905090565b60006103e1338484610953565b50600192915050565b60006103f7848484610a78565b6001600160a01b0384166000908152600660209081526040808320338452909152902054828110156104815760405162461bcd60e51b815260206004820152602860248201527f45524332303a207472616e7366657220616d6f756e74206578636565647320616044820152676c6c6f77616e636560c01b60648201526084015b60405180910390fd5b6104958533610490868561193a565b610953565b506001949350505050565b60006104aa610c50565b905090565b3360008181526006602090815260408083206001600160a01b038716845290915281205490916103e1918590610490908690611922565b6007546001600160a01b031633146105105760405162461bcd60e51b8152600401610478906118eb565b61051a8282610c92565b5050565b6001600160a01b0381166000908152600e60205260408120545b92915050565b600060608060008060006060610552610340565b6040805180820190915260018152603160f81b60208201524630610574610d74565b604080516000815260208101909152601f60f81b9d959c50939a509198509650945092509050565b60606000600101805461035190611981565b6007546001600160a01b031633146105d85760405162461bcd60e51b8152600401610478906118eb565b61051a8282610dbe565b6007546001600160a01b0316331461060c5760405162461bcd60e51b8152600401610478906118eb565b60025467ffffffffffffffff80831691161061066a5760405162461bcd60e51b815260206004820152601e60248201527f63757272656e74206d6574616461746120697320757020746f206461746500006044820152606401610478565b825161067d90600090602086019061144e565b50815161069190600190602085019061144e565b506002805467ffffffffffffffff191667ffffffffffffffff83161790556106b7610f0d565b505050565b3360009081526006602090815260408083206001600160a01b03861684529091528120548281101561073e5760405162461bcd60e51b815260206004820152602560248201527f45524332303a2064656372656173656420616c6c6f77616e63652062656c6f77604482015264207a65726f60d81b6064820152608401610478565b61074d3385610490868561193a565b5060019392505050565b60006103e1338484610a78565b600754600160a01b900460ff16156107b45760405162461bcd60e51b8152602060048201526013602482015272105b1c9958591e481a5b9a5d1a585b1a5e9959606a1b6044820152606401610478565b6007805460ff60a01b1916600160a01b1790556107d687878787878787610f69565b6107de610f0d565b50505050505050565b6107ef610f0d565b8342111561083f5760405162461bcd60e51b815260206004820152601d60248201527f45524332305065726d69743a206578706972656420646561646c696e650000006044820152606401610478565b60007f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c988888861086e8c611005565b6040805160208101969096526001600160a01b0394851690860152929091166060840152608083015260a082015260c0810186905260e00160405160208183030381529060405280519060200120905060006108c98261102d565b905060006108d982878787611040565b9050896001600160a01b0316816001600160a01b03161461093c5760405162461bcd60e51b815260206004820152601e60248201527f45524332305065726d69743a20696e76616c6964207369676e617475726500006044820152606401610478565b6109478a8a8a610953565b50505050505050505050565b6001600160a01b0383166109b55760405162461bcd60e51b8152602060048201526024808201527f45524332303a20617070726f76652066726f6d20746865207a65726f206164646044820152637265737360e01b6064820152608401610478565b6001600160a01b038216610a165760405162461bcd60e51b815260206004820152602260248201527f45524332303a20617070726f766520746f20746865207a65726f206164647265604482015261737360f01b6064820152608401610478565b6001600160a01b0383811660008181526006602090815260408083209487168084529482529182902085905590518481527f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b92591015b60405180910390a3505050565b6001600160a01b038316610adc5760405162461bcd60e51b815260206004820152602560248201527f45524332303a207472616e736665722066726f6d20746865207a65726f206164604482015264647265737360d81b6064820152608401610478565b6001600160a01b038216610b3e5760405162461bcd60e51b815260206004820152602360248201527f45524332303a207472616e7366657220746f20746865207a65726f206164647260448201526265737360e81b6064820152608401610478565b6001600160a01b03831660009081526005602052604090205481811015610bb65760405162461bcd60e51b815260206004820152602660248201527f45524332303a207472616e7366657220616d6f756e7420657863656564732062604482015265616c616e636560d01b6064820152608401610478565b610bc0828261193a565b6001600160a01b038086166000908152600560205260408082209390935590851681529081208054849290610bf6908490611922565b92505081905550826001600160a01b0316846001600160a01b03167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef84604051610c4291815260200190565b60405180910390a350505050565b600b546000906001600160a01b031630148015610c6e5750600a5446145b15610c7a575060095490565b6104aa610c85611068565b610c8d610d74565b611082565b6001600160a01b038216610ce85760405162461bcd60e51b815260206004820152601f60248201527f45524332303a206d696e7420746f20746865207a65726f2061646472657373006044820152606401610478565b8060006003016000828254610cfd9190611922565b90915550506001600160a01b03821660009081526005602052604081208054839290610d2a908490611922565b90915550506040518181526001600160a01b038316906000907fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9060200160405180910390a35050565b600754600854604051600160a81b90920460f01b6001600160f01b031916602083015260228201526000906042015b60405160208183030381529060405280519060200120905090565b6001600160a01b038216610e1e5760405162461bcd60e51b815260206004820152602160248201527f45524332303a206275726e2066726f6d20746865207a65726f206164647265736044820152607360f81b6064820152608401610478565b6001600160a01b03821660009081526005602052604090205481811015610e925760405162461bcd60e51b815260206004820152602260248201527f45524332303a206275726e20616d6f756e7420657863656564732062616c616e604482015261636560f01b6064820152608401610478565b610e9c828261193a565b6001600160a01b03841660009081526005602052604081209190915560038054849290610eca90849061193a565b90915550506040518281526000906001600160a01b038516907fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef90602001610a6b565b6000610f17611068565b90506000610f23610d74565b600d5490915082141580610f395750600c548114155b1561051a5746600a55600b80546001600160a01b03191630179055610f5e8282611082565b600955600c55600d55565b8651610f7c9060009060208a019061144e565b508551610f9090600190602089019061144e565b506004805460ff90961660ff19909616959095179094556002805467ffffffffffffffff90941667ffffffffffffffff19909416939093179092556007805461ffff909316600160a81b02600162ffff0160a01b03199093166001600160a01b03909216919091179190911790556008555050565b6001600160a01b0381166000908152600e602052604090208054600181018255905b50919050565b600061053861103a610c50565b83611136565b60008060006110518787878761115d565b9150915061105e8161124a565b5095945050505050565b6000611072610340565b604051602001610da39190611827565b60007fd87cd6ef79d4e2b95e15ce8abf732db51ec771f1ca2edccf22a46c729ac56472836110c46040805180820190915260018152603160f81b602082015290565b6040516020016110d49190611827565b60408051601f1981840301815282825280516020918201209083019490945281019190915260608101919091524660808201523060a082015260c0810183905260e0015b60405160208183030381529060405280519060200120905092915050565b60405161190160f01b60208201526022810183905260428101829052600090606201611118565b6000807f7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a08311156111945750600090506003611241565b8460ff16601b141580156111ac57508460ff16601c14155b156111bd5750600090506004611241565b6040805160008082526020820180845289905260ff881692820192909252606081018690526080810185905260019060a0016020604051602081039080840390855afa158015611211573d6000803e3d6000fd5b5050604051601f1901519150506001600160a01b03811661123a57600060019250925050611241565b9150600090505b94509492505050565b600081600481111561126c57634e487b7160e01b600052602160045260246000fd5b14156112755750565b600181600481111561129757634e487b7160e01b600052602160045260246000fd5b14156112e55760405162461bcd60e51b815260206004820152601860248201527f45434453413a20696e76616c6964207369676e617475726500000000000000006044820152606401610478565b600281600481111561130757634e487b7160e01b600052602160045260246000fd5b14156113555760405162461bcd60e51b815260206004820152601f60248201527f45434453413a20696e76616c6964207369676e6174757265206c656e677468006044820152606401610478565b600381600481111561137757634e487b7160e01b600052602160045260246000fd5b14156113d05760405162461bcd60e51b815260206004820152602260248201527f45434453413a20696e76616c6964207369676e6174757265202773272076616c604482015261756560f01b6064820152608401610478565b60048160048111156113f257634e487b7160e01b600052602160045260246000fd5b141561144b5760405162461bcd60e51b815260206004820152602260248201527f45434453413a20696e76616c6964207369676e6174757265202776272076616c604482015261756560f01b6064820152608401610478565b50565b82805461145a90611981565b90600052602060002090601f01602090048101928261147c57600085556114c2565b82601f1061149557805160ff19168380011785556114c2565b828001600101855582156114c2579182015b828111156114c25782518255916020019190600101906114a7565b506114ce9291506114d2565b5090565b5b808211156114ce57600081556001016114d3565b80356001600160a01b03811681146114fe57600080fd5b919050565b600082601f830112611513578081fd5b813567ffffffffffffffff8082111561152e5761152e6119cc565b604051601f8301601f19908116603f01168101908282118183101715611556576115566119cc565b8160405283815286602085880101111561156e578485fd5b8360208701602083013792830160200193909352509392505050565b803567ffffffffffffffff811681146114fe57600080fd5b803560ff811681146114fe57600080fd5b6000602082840312156115c4578081fd5b6115cd826114e7565b9392505050565b600080604083850312156115e6578081fd5b6115ef836114e7565b91506115fd602084016114e7565b90509250929050565b60008060006060848603121561161a578283fd5b611623846114e7565b9250611631602085016114e7565b9150604084013590509250925092565b600080600080600080600060e0888a03121561165b578283fd5b611664886114e7565b9650611672602089016114e7565b9550604088013594506060880135935061168e608089016115a2565b925060a0880135915060c0880135905092959891949750929550565b600080604083850312156116bc578182fd5b6116c5836114e7565b946020939093013593505050565b6000806000606084860312156116e7578283fd5b833567ffffffffffffffff808211156116fe578485fd5b61170a87838801611503565b9450602086013591508082111561171f578384fd5b5061172c86828701611503565b92505061173b6040850161158a565b90509250925092565b600080600080600080600060e0888a03121561175e578283fd5b611767886114e7565b9650611775602089016114e7565b9550604088013594506060880135935061168e6080890161158a565b92506117ce608089016114e7565b925060a088013561ffff811681146117e4578283fd5b8092505060c0880135905092959891949750929550565b60008151808452611813816020860160208601611951565b601f01601f19169290920160200192915050565b60008251611839818460208701611951565b9190910192915050565b60ff60f81b881681526000602060e08184015261186360e084018a6117fb565b8381036040850152611875818a6117fb565b606085018990526001600160a01b038816608086015260a0850187905284810360c08601528551808252838701925090830190845b818110156118c6578351835292840192918401916001016118aa565b50909c9b505050505050505050505050565b6020815260006115cd60208301846117fb565b60208082526017908201527f63616c6c6572206973206e6f7420746865206f776e6572000000000000000000604082015260600190565b60008219821115611935576119356119b6565b500190565b60008282101561194c5761194c6119b6565b500390565b60005b8381101561196c578181015183820152602001611954565b8381111561197b576000848401525b50505050565b600181811c9082168061199557607f821691505b6020821081141561102757634e487b7160e01b600052602260045260246000fd5b634e487b7160e01b600052601160045260246000fd5b634e487b7160e01b600052604160045260246000fdfea26469706673582212203de9d8f3af673eec4b7def57fea7c44ddaacb566240ed7be1ee0e924bc2e586264736f6c63430008040033"

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
const wbnbHex = "0x6060604052600436106100af576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff16806306fdde03146100b9578063095ea7b31461014757806318160ddd146101a157806323b872dd146101ca5780632e1a7d4d14610243578063313ce5671461026657806370a082311461029557806395d89b41146102e2578063a9059cbb14610370578063d0e30db0146103ca578063dd62ed3e146103d4575b6100b7610440565b005b34156100c457600080fd5b6100cc6104dd565b6040518080602001828103825283818151815260200191508051906020019080838360005b8381101561010c5780820151818401526020810190506100f1565b50505050905090810190601f1680156101395780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b341561015257600080fd5b610187600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803590602001909190505061057b565b604051808215151515815260200191505060405180910390f35b34156101ac57600080fd5b6101b461066d565b6040518082815260200191505060405180910390f35b34156101d557600080fd5b610229600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803573ffffffffffffffffffffffffffffffffffffffff1690602001909190505061068c565b604051808215151515815260200191505060405180910390f35b341561024e57600080fd5b61026460048080359060200190919050506109d9565b005b341561027157600080fd5b610279610b05565b604051808260ff1660ff16815260200191505060405180910390f35b34156102a057600080fd5b6102cc600480803573ffffffffffffffffffffffffffffffffffffffff16906020019091905050610b18565b6040518082815260200191505060405180910390f35b34156102ed57600080fd5b6102f5610b30565b6040518080602001828103825283818151815260200191508051906020019080838360005b8381101561033557808201518184015260208101905061031a565b50505050905090810190601f1680156103625780820380516001836020036101000a031916815260200191505b509250505060405180910390f35b341561037b57600080fd5b6103b0600480803573ffffffffffffffffffffffffffffffffffffffff16906020019091908035906020019091905050610bce565b604051808215151515815260200191505060405180910390f35b6103d2610440565b005b34156103df57600080fd5b61042a600480803573ffffffffffffffffffffffffffffffffffffffff1690602001909190803573ffffffffffffffffffffffffffffffffffffffff16906020019091905050610be3565b6040518082815260200191505060405180910390f35b34600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055503373ffffffffffffffffffffffffffffffffffffffff167fe1fffcc4923d04b559f4d29a8bfc6cda04eb5b0d3c460751c2402c5c5cc9109c346040518082815260200191505060405180910390a2565b60008054600181600116156101000203166002900480601f0160208091040260200160405190810160405280929190818152602001828054600181600116156101000203166002900480156105735780601f1061054857610100808354040283529160200191610573565b820191906000526020600020905b81548152906001019060200180831161055657829003601f168201915b505050505081565b600081600460003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055508273ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff167f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925846040518082815260200191505060405180910390a36001905092915050565b60003073ffffffffffffffffffffffffffffffffffffffff1631905090565b600081600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002054101515156106dc57600080fd5b3373ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff16141580156107b457507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205414155b156108cf5781600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020541015151561084457600080fd5b81600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055505b81600360008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000206000828254039250508190555081600360008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055508273ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef846040518082815260200191505060405180910390a3600190509392505050565b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205410151515610a2757600080fd5b80600360003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825403925050819055503373ffffffffffffffffffffffffffffffffffffffff166108fc829081150290604051600060405180830381858888f193505050501515610ab457600080fd5b3373ffffffffffffffffffffffffffffffffffffffff167f7fcf532c15f0a6db0bd6d0e038bea71d30d808c7d98cb3bf7268a95bf5081b65826040518082815260200191505060405180910390a250565b600260009054906101000a900460ff1681565b60036020528060005260406000206000915090505481565b60018054600181600116156101000203166002900480601f016020809104026020016040519081016040528092919081815260200182805460018160011615610100020316600290048015610bc65780601f10610b9b57610100808354040283529160200191610bc6565b820191906000526020600020905b815481529060010190602001808311610ba957829003601f168201915b505050505081565b6000610bdb33848461068c565b905092915050565b60046020528160005260406000206020528060005260406000206000915091505054815600a165627a7a72305820bcf3db16903185450bc04cb54da92f216e96710cce101fd2b4b47d5b70dc11e00029"

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
			op := vm.OpCode(opcode)
			if op >= vm.PUSH1 && op <= vm.PUSH32 {
				sz := int(op - vm.PUSH1 + 1)
				start := int(pc) + 1
				end := start + sz
				if start >= 0 && end <= len(code) {
					imm := code[start:end]
					before := len(scope.StackData())
					after := vm.NextStackSize(op, before)
					t.Logf("EVM-tracer opcode pc=%d op=%s imm=0x%x stack_after=%d scope.StackData=%v", pc, op, imm, after, scope.StackData())
					return
				}
			}
			before := len(scope.StackData())
			after := vm.NextStackSize(op, before)
			t.Logf("EVM-tracer opcode pc=%d op=%s stack_after=%d scope.StackData=%v", pc, op, after, scope.StackData())
		},
	}
	mirTracer := func(op compiler.MirOperation) {
		t.Logf("MIR exec: %s (0x%02x)", op.String(), byte(op))
	}

	// Also install extended MIR tracer to print mapping to EVM opcode and pc
	compiler.SetGlobalMIRTracerExtended(func(m *compiler.MIR) {
		if m != nil && m.Op().String() == "MirPHI" {
			// Include phi stack index for PHI nodes
			t.Logf("MIR exec: %s phiSlot=%d evm_pc=%d evm_op=0x%02x ops=%v", m.Op().String(), m.PhiStackIndex(), m.EvmPC(), m.EvmOp(), m.OperandDebugStrings())
			return
		}
		t.Logf("MIR exec: %s evm_pc=%d evm_op=0x%02x ops=%v", m.Op().String(), m.EvmPC(), m.EvmOp(), m.OperandDebugStrings())
	})
	defer compiler.SetGlobalMIRTracerExtended(nil)

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
	/*
	   // Print full EVM opcodes (including PUSH data) of the USDT contract
	   fullCode, derr := hex.DecodeString(usdtHex[2:])

	   	if derr != nil {
	   		t.Fatalf("decode USDT hex: %v", derr)
	   	}

	   	for pc := 0; pc < len(fullCode); {
	   		op := vm.OpCode(fullCode[pc])
	   		if op >= vm.PUSH1 && op <= vm.PUSH32 {
	   			n := int(op - vm.PUSH1 + 1)
	   			if pc+1+n <= len(fullCode) {
	   				data := fullCode[pc+1 : pc+1+n]
	   				t.Logf("pc=%d op=%-8s (0x%02x) data=0x%x", pc, op.String(), byte(op), data)
	   			} else {
	   				t.Logf("pc=%d op=%-8s (0x%02x) data=<truncated>", pc, op.String(), byte(op))
	   			}
	   			pc += 1 + n
	   			continue
	   		}
	   		t.Logf("pc=%d op=%-8s (0x%02x)", pc, op.String(), byte(op))
	   		pc++
	   	}
	*/
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
