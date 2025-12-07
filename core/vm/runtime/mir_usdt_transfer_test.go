package runtime

import (
	"encoding/hex"
	"io/ioutil"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/opcodeCompiler/compiler"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

// Function selectors for USDT contract
var (
	mintSelector      = []byte{0x40, 0xc1, 0x0f, 0x19} // mint(address,uint256)
	balanceOfSelector = []byte{0x70, 0xa0, 0x82, 0x31} // balanceOf(address)
	transferSelector  = []byte{0xa9, 0x05, 0x9c, 0xbb} // transfer(address,uint256)
)

// ContractRef implementation
type AddressRef struct {
	addr common.Address
}

func (a AddressRef) Address() common.Address {
	return a.addr
}

// Addresses for USDT contract
var (
	aliceAddr    = common.HexToAddress("0x1000000000000000000000000000000000000001")
	usdtContract = common.HexToAddress("0x2000000000000000000000000000000000000001")
	// 全局变量存储实际部署的合约地址
	globalUsdtContract common.Address
	// ContractRef for Alice
	aliceRef = AddressRef{addr: aliceAddr}
)

// 设置BSC详细日志
func setupBSCLogging(t *testing.T) {
	// 设置环境变量启用BSC的详细日志
	os.Setenv("BSC_LOG_LEVEL", "debug")
	os.Setenv("ETH_LOG_LEVEL", "debug")
	os.Setenv("EVM_DEBUG", "true")
	os.Setenv("BSC_DEBUG", "true")

	// 设置BSC特定的日志环境变量
	os.Setenv("GETH_LOG_LEVEL", "debug")
	os.Setenv("GETH_DEBUG", "true")
	os.Setenv("VM_DEBUG", "true")
	os.Setenv("CORE_DEBUG", "true")
	os.Setenv("TRIE_DEBUG", "true")
	os.Setenv("STATE_DEBUG", "true")

	// 设置日志输出到控制台
	os.Setenv("GETH_LOG_OUTPUT", "console")
	os.Setenv("BSC_LOG_OUTPUT", "console")

	t.Log("🔧 BSC detailed logging enabled")
	t.Log("📊 Log levels: BSC=debug, ETH=debug, EVM=debug")
}

// 配置50万次转账测试参数（保守版本）
func get500KScaleConfigConservative() (int64, uint64, uint64) {
	// 50万次转账测试配置（保守版本）
	numTransfers := int64(500000)          // 50万次转账
	batchGasLimit := uint64(100000000000)  // 100B gas for batch transfer
	blockGasLimit := uint64(1000000000000) // 1T gas limit for block

	return numTransfers, batchGasLimit, blockGasLimit
}

// 配置50万次转账测试参数
func get500KScaleConfig() (int64, uint64, uint64) {
	// 50万次转账测试配置
	numTransfers := int64(500000)          // 50万次转账
	batchGasLimit := uint64(100000000000)  // 100B gas for individual transfers (每次转账约200K gas)
	blockGasLimit := uint64(1000000000000) // 1T gas limit for block

	return numTransfers, batchGasLimit, blockGasLimit
}

// 配置大规模测试参数
func getLargeScaleConfig() (int64, uint64, uint64) {
	// 大规模测试配置
	numTransfers := int64(50000000)         // 5000万次转账
	batchGasLimit := uint64(1000000000000)  // 1T gas for batch transfer (从100B增加到1T)
	blockGasLimit := uint64(10000000000000) // 10T gas limit for block (从1T增加到10T)

	return numTransfers, batchGasLimit, blockGasLimit
}

// 配置中等规模测试参数
func getMediumScaleConfig() (int64, uint64, uint64) {
	// 中等规模测试配置
	numTransfers := int64(5000000)        // 500万次转账
	batchGasLimit := uint64(10000000000)  // 10B gas for batch transfer
	blockGasLimit := uint64(100000000000) // 100B gas limit for block

	return numTransfers, batchGasLimit, blockGasLimit
}

// 配置小规模测试参数
func getSmallScaleConfig() (int64, uint64, uint64) {
	// 小规模测试配置
	numTransfers := int64(100)           // 减少到100次转账以避免测试过慢
	batchGasLimit := uint64(50000000)    // 50M gas total预算（不再平均分配，单次至少200k）
	blockGasLimit := uint64(10000000000) // 10B gas limit for block

	return numTransfers, batchGasLimit, blockGasLimit
}

func TestMIRUSDTTransfer(t *testing.T) {
	// 启用BSC详细日志
	setupBSCLogging(t)

	// 选择测试规模 - 使用小规模测试避免超时
	numTransfers, batchGasLimit, blockGasLimit := getSmallScaleConfig() // 5万次转账

	t.Logf("🚀 Pure BSC-EVM Benchmark - USDT Token Individual Transfers (Scale: %d transfers)", numTransfers)
	t.Logf("📊 Gas Configuration - Total: %d, Block: %d", batchGasLimit, blockGasLimit)

	// Load USDT contract bytecode
	t.Log("📦 Loading USDT contract bytecode...")
	usdtBytecode := loadBytecode(t, "usdt.bin")
	t.Logf("✅ Bytecode loaded, size: %d bytes", len(usdtBytecode))

	// Initialize EVM with BSC configuration
	t.Log("🔧 Initializing EVM with BSC configuration...")
	db := rawdb.NewMemoryDatabase()
	t.Log("✅ Memory database created")

	trieDB := triedb.NewDatabase(db, nil)
	t.Log("✅ Trie database created")

	statedb, _ := state.New(common.Hash{}, state.NewDatabase(trieDB, nil))
	t.Log("✅ State database created")

	// Create Alice account with some BNB for gas
	t.Logf("👤 Creating Alice account: %s", aliceAddr.Hex())
	statedb.CreateAccount(aliceAddr)
	aliceBalance := uint256.NewInt(1000000000000000000) // 1 BNB
	statedb.SetBalance(aliceAddr, aliceBalance, tracing.BalanceChangeUnspecified)
	t.Logf("💰 Set Alice balance: %s wei", aliceBalance.String())

	// Create EVM context with BSC parameters
	t.Log("🔧 Creating BSC chain configuration...")
	chainConfig := &params.ChainConfig{
		ChainID:             big.NewInt(56), // BSC Mainnet
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		RamanujanBlock:      big.NewInt(0),          // BSC特有
		NielsBlock:          big.NewInt(0),          // BSC特有
		Parlia:              &params.ParliaConfig{}, // BSC的共识机制
	}
	t.Logf("✅ Chain config created - Chain ID: %d", chainConfig.ChainID)

	vmConfig := vm.Config{
		EnableOpcodeOptimizations: false,
		EnableMIR:                 true,
	}
	t.Log("✅ EVM configuration created (MIR enabled for both runtime and constructor)")

	compiler.EnableOpcodeParse()

	// 🔍 启用 MIR 调试日志
	compiler.EnableDebugLogs(true)
	compiler.EnableMIRDebugLogs(true)
	compiler.EnableParserDebugLogs(true)
	t.Log("🔍 MIR debug logs enabled")

	blockContext := vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		Coinbase:    common.Address{},
		BlockNumber: big.NewInt(1),
		Time:        uint64(1681338455),
		Difficulty:  big.NewInt(1),
		GasLimit:    blockGasLimit,
		BaseFee:     big.NewInt(0),
	}
	t.Logf("✅ Block context created - Block #%d, Gas Limit: %d", blockContext.BlockNumber, blockContext.GasLimit)

	// Create EVM
	t.Log("🚀 Creating EVM instance...")
	evm := vm.NewEVM(blockContext, statedb, chainConfig, vmConfig)
	t.Log("✅ EVM instance created successfully")

	// Deploy USDT contract
	t.Log("📦 Deploying USDT contract...")
	deployContract(t, evm, usdtBytecode)

	t.Log("💰 USDT contract constructor already gave tokens to Alice")

	// Verify Alice's balance
	t.Log("🔍 Verifying Alice's balance...")
	aliceTokenBalance := getTokenBalance(t, evm, aliceAddr)
	t.Logf("✅ Alice's balance: %s tokens", new(big.Int).Div(aliceTokenBalance, big.NewInt(1000000000000000000)).String())

	// Optional: ensure Alice has spendable balance by minting additional tokens if supported
	// t.Log("🪙 Minting 1 token to Alice (if contract supports mint)...")
	// mintTokens(t, evm, big.NewInt(1000000000000000000))

	// Perform individual transfers
	t.Log("🔄 Performing individual transfers...")
	duration := performIndividualTransfersWithConfig(t, evm, numTransfers, batchGasLimit)
	t.Logf("✅ Individual transfers completed in %v", duration)

	// Calculate performance metrics
	transfersPerSecond := float64(numTransfers) / duration.Seconds()
	t.Logf("⚡ Benchmark Results - Transfers: %d, Duration: %.2fms, TPS: %.2f",
		numTransfers, float64(duration.Nanoseconds())/1000000, transfersPerSecond)

	// Verify some recipient balances
	t.Log("🔍 Verifying transfers...")
	startRecipient := common.HexToAddress("0x1111111111111111111111111111111111111234")
	for i := 0; i < 3; i++ {
		recipient := common.BigToAddress(new(big.Int).Add(startRecipient.Big(), big.NewInt(int64(i))))
		balance := getTokenBalance(t, evm, recipient)
		t.Logf("✅ Recipient %d (%s): %s tokens", i+1, recipient.Hex(), new(big.Int).Div(balance, big.NewInt(1000000000000000000)).String())
	}

	// Verify Alice's final balance
	t.Log("🔍 Verifying Alice's final balance...")
	aliceFinalBalance := getTokenBalance(t, evm, aliceAddr)
	t.Logf("✅ Alice's final balance: %s tokens", new(big.Int).Div(aliceFinalBalance, big.NewInt(1000000000000000000)).String())

	t.Log("✨ BSC-EVM Benchmark completed successfully!")
}

func loadBytecode(t *testing.T, path string) []byte {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read bytecode file: %v", err)
	}

	bytecodeStr := strings.TrimSpace(string(data))
	if strings.HasPrefix(bytecodeStr, "0x") {
		bytecodeStr = bytecodeStr[2:]
	}

	bytecode, err := hex.DecodeString(bytecodeStr)
	if err != nil {
		t.Fatalf("Invalid hex in bytecode: %v", err)
	}

	return bytecode
}

func deployContract(t *testing.T, evm *vm.EVM, bytecode []byte) {
	// Deploy contract with increased gas limit
	value := uint256.NewInt(0)
	deployGasLimit := uint64(2000000000) // 2B gas
	t.Logf("🔧 Deploying contract with %d gas...", deployGasLimit)

	ret, contractAddr, leftOverGas, err := evm.Create(aliceRef, bytecode, deployGasLimit, value)
	gasUsed := deployGasLimit - leftOverGas
	t.Logf("📝 evm.Create returned: err=%v, gasUsed=%d", err, gasUsed)

	if err != nil {
		t.Fatalf("❌ Contract deployment failed: %v (Gas used: %d/%d)", err, gasUsed, deployGasLimit)
	}

	t.Logf("✅ Contract deployed at: %s, gas used: %d/%d (%.2f%%)",
		contractAddr.Hex(), gasUsed, deployGasLimit, float64(gasUsed)/float64(deployGasLimit)*100)

	// 更新全局变量存储实际部署的合约地址
	globalUsdtContract = contractAddr
	_ = ret
}

func mintTokens(t *testing.T, evm *vm.EVM, amount *big.Int) {
	// USDT合约的mint函数签名是 mint(uint256 amount)
	// 不需要to参数，因为USDT的mint函数会将代币铸造给msg.sender

	// Prepare calldata for USDT mint function
	calldata := make([]byte, 0, 36)
	calldata = append(calldata, mintSelector...)
	calldata = append(calldata, common.LeftPadBytes(amount.Bytes(), 32)...)

	// Execute transaction with increased gas limit
	executeTransaction(t, evm, globalUsdtContract, calldata, 100000000)
}

func getTokenBalance(t *testing.T, evm *vm.EVM, account common.Address) *big.Int {
	// Prepare calldata
	calldata := make([]byte, 0, 36)
	calldata = append(calldata, balanceOfSelector...)
	calldata = append(calldata, make([]byte, 12)...) // padding for address
	calldata = append(calldata, account.Bytes()...)

	// Execute transaction
	ret := executeTransaction(t, evm, globalUsdtContract, calldata, 100000000)

	if len(ret) >= 32 {
		balance := new(big.Int).SetBytes(ret[:32])
		return balance
	}
	return big.NewInt(0)
}

func performIndividualTransfersWithConfig(t *testing.T, evm *vm.EVM, numTransfers int64, gasLimit uint64) time.Duration {
	startRecipient := common.HexToAddress("0x1111111111111111111111111111111111111234")
	amountPerTransfer := big.NewInt(1000000000000000000) // 1 token

	// 计算每次转账的gas上限，至少200k，避免因gas不足导致revert
	candidate := gasLimit / uint64(numTransfers)
	if candidate < 200000 {
		candidate = 200000
	}
	gasPerTransfer := candidate

	t.Logf("🔄 Starting individual transfers with %d transfers, gas limit per transfer: %d", numTransfers, gasPerTransfer)

	// Measure execution time
	startTime := time.Now()

	for i := 0; i < int(numTransfers); i++ {
		// 计算接收地址
		recipient := common.BigToAddress(new(big.Int).Add(startRecipient.Big(), big.NewInt(int64(i))))
		if i == 0 {
			t.Logf("➡️ First recipient: %s", recipient.Hex())
		}

		// 准备transfer函数的calldata
		calldata := make([]byte, 0, 68)
		calldata = append(calldata, transferSelector...)
		calldata = append(calldata, make([]byte, 12)...) // padding for address
		calldata = append(calldata, recipient.Bytes()...)
		calldata = append(calldata, common.LeftPadBytes(amountPerTransfer.Bytes(), 32)...)

		// 执行transfer调用
		executeTransaction(t, evm, globalUsdtContract, calldata, gasPerTransfer)

		// 每10000次转账打印一次进度
		if (i+1)%10000 == 0 {
			t.Logf("📊 Progress: %d/%d transfers completed", i+1, numTransfers)
		}
	}

	duration := time.Since(startTime)
	t.Logf("✅ Individual transfers completed in %v", duration)

	return duration
}

func executeTransaction(t *testing.T, evm *vm.EVM, to common.Address, data []byte, gasLimit uint64) []byte {
	// Execute call
	value := uint256.NewInt(0)
	ret, leftOverGas, err := evm.Call(aliceRef, to, data, gasLimit, value)

	if err != nil {
		gasUsed := gasLimit - leftOverGas
		// 打印revert返回数据，帮助诊断失败原因
		if len(ret) > 0 {
			t.Logf("↩️ Revert data (hex): %s", hex.EncodeToString(ret))
		}
		t.Fatalf("❌ Transaction failed: %v (Gas used: %d/%d)", err, gasUsed, gasLimit)
	}

	return ret
}
