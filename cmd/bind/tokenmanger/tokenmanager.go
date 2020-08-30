// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package tokenmanager

import (
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = abi.U256
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
)

// TokenmanagerABI is the input ABI used to generate the binding from.
const TokenmanagerABI = "[{\"inputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"contractAddr\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"string\",\"name\":\"bep2Symbol\",\"type\":\"string\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"failedReason\",\"type\":\"uint32\"}],\"name\":\"bindFailure\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"contractAddr\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"string\",\"name\":\"bep2Symbol\",\"type\":\"string\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"totalSupply\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"peggyAmount\",\"type\":\"uint256\"}],\"name\":\"bindSuccess\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint8\",\"name\":\"channelId\",\"type\":\"uint8\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"msgBytes\",\"type\":\"bytes\"}],\"name\":\"unexpectedPackage\",\"type\":\"event\"},{\"constant\":true,\"inputs\":[],\"name\":\"BIND_CHANNELID\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"BIND_PACKAGE\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"BIND_STATUS_ALREADY_BOUND_TOKEN\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"BIND_STATUS_DECIMALS_MISMATCH\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"BIND_STATUS_REJECTED\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"BIND_STATUS_SUCCESS\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"BIND_STATUS_SYMBOL_MISMATCH\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"BIND_STATUS_TIMEOUT\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"BIND_STATUS_TOO_MUCH_TOKENHUB_BALANCE\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"BIND_STATUS_TOTAL_SUPPLY_MISMATCH\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"CODE_OK\",\"outputs\":[{\"internalType\":\"uint32\",\"name\":\"\",\"type\":\"uint32\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"CROSS_CHAIN_CONTRACT_ADDR\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"ERROR_FAIL_DECODE\",\"outputs\":[{\"internalType\":\"uint32\",\"name\":\"\",\"type\":\"uint32\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"GOV_CHANNELID\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"GOV_HUB_ADDR\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"INCENTIVIZE_ADDR\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"LIGHT_CLIENT_ADDR\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"MAXIMUM_BEP20_SYMBOL_LEN\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"MINIMUM_BEP20_SYMBOL_LEN\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"RELAYERHUB_CONTRACT_ADDR\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"SLASH_CHANNELID\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"SLASH_CONTRACT_ADDR\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"STAKING_CHANNELID\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"SYSTEM_REWARD_ADDR\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"TEN_DECIMALS\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"TOKEN_HUB_ADDR\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"TOKEN_MANAGER_ADDR\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"TRANSFER_IN_CHANNELID\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"TRANSFER_OUT_CHANNELID\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"UNBIND_PACKAGE\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"VALIDATOR_CONTRACT_ADDR\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"alreadyInit\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"bindPackageRecord\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"packageType\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"bep2TokenSymbol\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"contractAddr\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"totalSupply\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"peggyAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint8\",\"name\":\"bep20Decimals\",\"type\":\"uint8\"},{\"internalType\":\"uint64\",\"name\":\"expireTime\",\"type\":\"uint64\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"bscChainID\",\"outputs\":[{\"internalType\":\"uint16\",\"name\":\"\",\"type\":\"uint16\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"},{\"internalType\":\"bytes\",\"name\":\"msgBytes\",\"type\":\"bytes\"}],\"name\":\"handleSynPackage\",\"outputs\":[{\"internalType\":\"bytes\",\"name\":\"\",\"type\":\"bytes\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"uint8\",\"name\":\"channelId\",\"type\":\"uint8\"},{\"internalType\":\"bytes\",\"name\":\"msgBytes\",\"type\":\"bytes\"}],\"name\":\"handleAckPackage\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"uint8\",\"name\":\"channelId\",\"type\":\"uint8\"},{\"internalType\":\"bytes\",\"name\":\"msgBytes\",\"type\":\"bytes\"}],\"name\":\"handleFailAckPackage\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"contractAddr\",\"type\":\"address\"},{\"internalType\":\"string\",\"name\":\"bep2Symbol\",\"type\":\"string\"}],\"name\":\"approveBind\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":true,\"stateMutability\":\"payable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"contractAddr\",\"type\":\"address\"},{\"internalType\":\"string\",\"name\":\"bep2Symbol\",\"type\":\"string\"}],\"name\":\"rejectBind\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":true,\"stateMutability\":\"payable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"string\",\"name\":\"bep2Symbol\",\"type\":\"string\"}],\"name\":\"expireBind\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":true,\"stateMutability\":\"payable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"internalType\":\"string\",\"name\":\"symbol\",\"type\":\"string\"}],\"name\":\"queryRequiredLockAmountForBind\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"}]"

// Tokenmanager is an auto generated Go binding around an Ethereum contract.
type Tokenmanager struct {
	TokenmanagerCaller     // Read-only binding to the contract
	TokenmanagerTransactor // Write-only binding to the contract
	TokenmanagerFilterer   // Log filterer for contract events
}

// TokenmanagerCaller is an auto generated read-only Go binding around an Ethereum contract.
type TokenmanagerCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// TokenmanagerTransactor is an auto generated write-only Go binding around an Ethereum contract.
type TokenmanagerTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// TokenmanagerFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type TokenmanagerFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// TokenmanagerSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type TokenmanagerSession struct {
	Contract     *Tokenmanager     // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// TokenmanagerCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type TokenmanagerCallerSession struct {
	Contract *TokenmanagerCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts       // Call options to use throughout this session
}

// TokenmanagerTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type TokenmanagerTransactorSession struct {
	Contract     *TokenmanagerTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts       // Transaction auth options to use throughout this session
}

// TokenmanagerRaw is an auto generated low-level Go binding around an Ethereum contract.
type TokenmanagerRaw struct {
	Contract *Tokenmanager // Generic contract binding to access the raw methods on
}

// TokenmanagerCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type TokenmanagerCallerRaw struct {
	Contract *TokenmanagerCaller // Generic read-only contract binding to access the raw methods on
}

// TokenmanagerTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type TokenmanagerTransactorRaw struct {
	Contract *TokenmanagerTransactor // Generic write-only contract binding to access the raw methods on
}

// NewTokenmanager creates a new instance of Tokenmanager, bound to a specific deployed contract.
func NewTokenmanager(address common.Address, backend bind.ContractBackend) (*Tokenmanager, error) {
	contract, err := bindTokenmanager(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Tokenmanager{TokenmanagerCaller: TokenmanagerCaller{contract: contract}, TokenmanagerTransactor: TokenmanagerTransactor{contract: contract}, TokenmanagerFilterer: TokenmanagerFilterer{contract: contract}}, nil
}

// NewTokenmanagerCaller creates a new read-only instance of Tokenmanager, bound to a specific deployed contract.
func NewTokenmanagerCaller(address common.Address, caller bind.ContractCaller) (*TokenmanagerCaller, error) {
	contract, err := bindTokenmanager(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &TokenmanagerCaller{contract: contract}, nil
}

// NewTokenmanagerTransactor creates a new write-only instance of Tokenmanager, bound to a specific deployed contract.
func NewTokenmanagerTransactor(address common.Address, transactor bind.ContractTransactor) (*TokenmanagerTransactor, error) {
	contract, err := bindTokenmanager(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &TokenmanagerTransactor{contract: contract}, nil
}

// NewTokenmanagerFilterer creates a new log filterer instance of Tokenmanager, bound to a specific deployed contract.
func NewTokenmanagerFilterer(address common.Address, filterer bind.ContractFilterer) (*TokenmanagerFilterer, error) {
	contract, err := bindTokenmanager(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &TokenmanagerFilterer{contract: contract}, nil
}

// bindTokenmanager binds a generic wrapper to an already deployed contract.
func bindTokenmanager(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := abi.JSON(strings.NewReader(TokenmanagerABI))
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Tokenmanager *TokenmanagerRaw) Call(opts *bind.CallOpts, result interface{}, method string, params ...interface{}) error {
	return _Tokenmanager.Contract.TokenmanagerCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Tokenmanager *TokenmanagerRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Tokenmanager.Contract.TokenmanagerTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Tokenmanager *TokenmanagerRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Tokenmanager.Contract.TokenmanagerTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Tokenmanager *TokenmanagerCallerRaw) Call(opts *bind.CallOpts, result interface{}, method string, params ...interface{}) error {
	return _Tokenmanager.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Tokenmanager *TokenmanagerTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Tokenmanager.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Tokenmanager *TokenmanagerTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Tokenmanager.Contract.contract.Transact(opts, method, params...)
}

// BINDCHANNELID is a free data retrieval call binding the contract method 0x3dffc387.
//
// Solidity: function BIND_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) BINDCHANNELID(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "BIND_CHANNELID")
	return *ret0, err
}

// BINDCHANNELID is a free data retrieval call binding the contract method 0x3dffc387.
//
// Solidity: function BIND_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) BINDCHANNELID() (uint8, error) {
	return _Tokenmanager.Contract.BINDCHANNELID(&_Tokenmanager.CallOpts)
}

// BINDCHANNELID is a free data retrieval call binding the contract method 0x3dffc387.
//
// Solidity: function BIND_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) BINDCHANNELID() (uint8, error) {
	return _Tokenmanager.Contract.BINDCHANNELID(&_Tokenmanager.CallOpts)
}

// BINDPACKAGE is a free data retrieval call binding the contract method 0xfe3a2af5.
//
// Solidity: function BIND_PACKAGE() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) BINDPACKAGE(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "BIND_PACKAGE")
	return *ret0, err
}

// BINDPACKAGE is a free data retrieval call binding the contract method 0xfe3a2af5.
//
// Solidity: function BIND_PACKAGE() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) BINDPACKAGE() (uint8, error) {
	return _Tokenmanager.Contract.BINDPACKAGE(&_Tokenmanager.CallOpts)
}

// BINDPACKAGE is a free data retrieval call binding the contract method 0xfe3a2af5.
//
// Solidity: function BIND_PACKAGE() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) BINDPACKAGE() (uint8, error) {
	return _Tokenmanager.Contract.BINDPACKAGE(&_Tokenmanager.CallOpts)
}

// BINDSTATUSALREADYBOUNDTOKEN is a free data retrieval call binding the contract method 0x0f212b1b.
//
// Solidity: function BIND_STATUS_ALREADY_BOUND_TOKEN() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) BINDSTATUSALREADYBOUNDTOKEN(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "BIND_STATUS_ALREADY_BOUND_TOKEN")
	return *ret0, err
}

// BINDSTATUSALREADYBOUNDTOKEN is a free data retrieval call binding the contract method 0x0f212b1b.
//
// Solidity: function BIND_STATUS_ALREADY_BOUND_TOKEN() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) BINDSTATUSALREADYBOUNDTOKEN() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSALREADYBOUNDTOKEN(&_Tokenmanager.CallOpts)
}

// BINDSTATUSALREADYBOUNDTOKEN is a free data retrieval call binding the contract method 0x0f212b1b.
//
// Solidity: function BIND_STATUS_ALREADY_BOUND_TOKEN() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) BINDSTATUSALREADYBOUNDTOKEN() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSALREADYBOUNDTOKEN(&_Tokenmanager.CallOpts)
}

// BINDSTATUSDECIMALSMISMATCH is a free data retrieval call binding the contract method 0x4bc81c00.
//
// Solidity: function BIND_STATUS_DECIMALS_MISMATCH() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) BINDSTATUSDECIMALSMISMATCH(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "BIND_STATUS_DECIMALS_MISMATCH")
	return *ret0, err
}

// BINDSTATUSDECIMALSMISMATCH is a free data retrieval call binding the contract method 0x4bc81c00.
//
// Solidity: function BIND_STATUS_DECIMALS_MISMATCH() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) BINDSTATUSDECIMALSMISMATCH() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSDECIMALSMISMATCH(&_Tokenmanager.CallOpts)
}

// BINDSTATUSDECIMALSMISMATCH is a free data retrieval call binding the contract method 0x4bc81c00.
//
// Solidity: function BIND_STATUS_DECIMALS_MISMATCH() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) BINDSTATUSDECIMALSMISMATCH() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSDECIMALSMISMATCH(&_Tokenmanager.CallOpts)
}

// BINDSTATUSREJECTED is a free data retrieval call binding the contract method 0x95b9ad26.
//
// Solidity: function BIND_STATUS_REJECTED() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) BINDSTATUSREJECTED(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "BIND_STATUS_REJECTED")
	return *ret0, err
}

// BINDSTATUSREJECTED is a free data retrieval call binding the contract method 0x95b9ad26.
//
// Solidity: function BIND_STATUS_REJECTED() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) BINDSTATUSREJECTED() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSREJECTED(&_Tokenmanager.CallOpts)
}

// BINDSTATUSREJECTED is a free data retrieval call binding the contract method 0x95b9ad26.
//
// Solidity: function BIND_STATUS_REJECTED() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) BINDSTATUSREJECTED() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSREJECTED(&_Tokenmanager.CallOpts)
}

// BINDSTATUSSUCCESS is a free data retrieval call binding the contract method 0x4a688818.
//
// Solidity: function BIND_STATUS_SUCCESS() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) BINDSTATUSSUCCESS(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "BIND_STATUS_SUCCESS")
	return *ret0, err
}

// BINDSTATUSSUCCESS is a free data retrieval call binding the contract method 0x4a688818.
//
// Solidity: function BIND_STATUS_SUCCESS() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) BINDSTATUSSUCCESS() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSSUCCESS(&_Tokenmanager.CallOpts)
}

// BINDSTATUSSUCCESS is a free data retrieval call binding the contract method 0x4a688818.
//
// Solidity: function BIND_STATUS_SUCCESS() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) BINDSTATUSSUCCESS() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSSUCCESS(&_Tokenmanager.CallOpts)
}

// BINDSTATUSSYMBOLMISMATCH is a free data retrieval call binding the contract method 0x5f558f86.
//
// Solidity: function BIND_STATUS_SYMBOL_MISMATCH() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) BINDSTATUSSYMBOLMISMATCH(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "BIND_STATUS_SYMBOL_MISMATCH")
	return *ret0, err
}

// BINDSTATUSSYMBOLMISMATCH is a free data retrieval call binding the contract method 0x5f558f86.
//
// Solidity: function BIND_STATUS_SYMBOL_MISMATCH() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) BINDSTATUSSYMBOLMISMATCH() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSSYMBOLMISMATCH(&_Tokenmanager.CallOpts)
}

// BINDSTATUSSYMBOLMISMATCH is a free data retrieval call binding the contract method 0x5f558f86.
//
// Solidity: function BIND_STATUS_SYMBOL_MISMATCH() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) BINDSTATUSSYMBOLMISMATCH() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSSYMBOLMISMATCH(&_Tokenmanager.CallOpts)
}

// BINDSTATUSTIMEOUT is a free data retrieval call binding the contract method 0x7d078e13.
//
// Solidity: function BIND_STATUS_TIMEOUT() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) BINDSTATUSTIMEOUT(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "BIND_STATUS_TIMEOUT")
	return *ret0, err
}

// BINDSTATUSTIMEOUT is a free data retrieval call binding the contract method 0x7d078e13.
//
// Solidity: function BIND_STATUS_TIMEOUT() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) BINDSTATUSTIMEOUT() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSTIMEOUT(&_Tokenmanager.CallOpts)
}

// BINDSTATUSTIMEOUT is a free data retrieval call binding the contract method 0x7d078e13.
//
// Solidity: function BIND_STATUS_TIMEOUT() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) BINDSTATUSTIMEOUT() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSTIMEOUT(&_Tokenmanager.CallOpts)
}

// BINDSTATUSTOOMUCHTOKENHUBBALANCE is a free data retrieval call binding the contract method 0xc8e704a4.
//
// Solidity: function BIND_STATUS_TOO_MUCH_TOKENHUB_BALANCE() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) BINDSTATUSTOOMUCHTOKENHUBBALANCE(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "BIND_STATUS_TOO_MUCH_TOKENHUB_BALANCE")
	return *ret0, err
}

// BINDSTATUSTOOMUCHTOKENHUBBALANCE is a free data retrieval call binding the contract method 0xc8e704a4.
//
// Solidity: function BIND_STATUS_TOO_MUCH_TOKENHUB_BALANCE() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) BINDSTATUSTOOMUCHTOKENHUBBALANCE() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSTOOMUCHTOKENHUBBALANCE(&_Tokenmanager.CallOpts)
}

// BINDSTATUSTOOMUCHTOKENHUBBALANCE is a free data retrieval call binding the contract method 0xc8e704a4.
//
// Solidity: function BIND_STATUS_TOO_MUCH_TOKENHUB_BALANCE() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) BINDSTATUSTOOMUCHTOKENHUBBALANCE() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSTOOMUCHTOKENHUBBALANCE(&_Tokenmanager.CallOpts)
}

// BINDSTATUSTOTALSUPPLYMISMATCH is a free data retrieval call binding the contract method 0x1f91600b.
//
// Solidity: function BIND_STATUS_TOTAL_SUPPLY_MISMATCH() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) BINDSTATUSTOTALSUPPLYMISMATCH(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "BIND_STATUS_TOTAL_SUPPLY_MISMATCH")
	return *ret0, err
}

// BINDSTATUSTOTALSUPPLYMISMATCH is a free data retrieval call binding the contract method 0x1f91600b.
//
// Solidity: function BIND_STATUS_TOTAL_SUPPLY_MISMATCH() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) BINDSTATUSTOTALSUPPLYMISMATCH() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSTOTALSUPPLYMISMATCH(&_Tokenmanager.CallOpts)
}

// BINDSTATUSTOTALSUPPLYMISMATCH is a free data retrieval call binding the contract method 0x1f91600b.
//
// Solidity: function BIND_STATUS_TOTAL_SUPPLY_MISMATCH() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) BINDSTATUSTOTALSUPPLYMISMATCH() (uint8, error) {
	return _Tokenmanager.Contract.BINDSTATUSTOTALSUPPLYMISMATCH(&_Tokenmanager.CallOpts)
}

// CODEOK is a free data retrieval call binding the contract method 0xab51bb96.
//
// Solidity: function CODE_OK() view returns(uint32)
func (_Tokenmanager *TokenmanagerCaller) CODEOK(opts *bind.CallOpts) (uint32, error) {
	var (
		ret0 = new(uint32)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "CODE_OK")
	return *ret0, err
}

// CODEOK is a free data retrieval call binding the contract method 0xab51bb96.
//
// Solidity: function CODE_OK() view returns(uint32)
func (_Tokenmanager *TokenmanagerSession) CODEOK() (uint32, error) {
	return _Tokenmanager.Contract.CODEOK(&_Tokenmanager.CallOpts)
}

// CODEOK is a free data retrieval call binding the contract method 0xab51bb96.
//
// Solidity: function CODE_OK() view returns(uint32)
func (_Tokenmanager *TokenmanagerCallerSession) CODEOK() (uint32, error) {
	return _Tokenmanager.Contract.CODEOK(&_Tokenmanager.CallOpts)
}

// CROSSCHAINCONTRACTADDR is a free data retrieval call binding the contract method 0x51e80672.
//
// Solidity: function CROSS_CHAIN_CONTRACT_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCaller) CROSSCHAINCONTRACTADDR(opts *bind.CallOpts) (common.Address, error) {
	var (
		ret0 = new(common.Address)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "CROSS_CHAIN_CONTRACT_ADDR")
	return *ret0, err
}

// CROSSCHAINCONTRACTADDR is a free data retrieval call binding the contract method 0x51e80672.
//
// Solidity: function CROSS_CHAIN_CONTRACT_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerSession) CROSSCHAINCONTRACTADDR() (common.Address, error) {
	return _Tokenmanager.Contract.CROSSCHAINCONTRACTADDR(&_Tokenmanager.CallOpts)
}

// CROSSCHAINCONTRACTADDR is a free data retrieval call binding the contract method 0x51e80672.
//
// Solidity: function CROSS_CHAIN_CONTRACT_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCallerSession) CROSSCHAINCONTRACTADDR() (common.Address, error) {
	return _Tokenmanager.Contract.CROSSCHAINCONTRACTADDR(&_Tokenmanager.CallOpts)
}

// ERRORFAILDECODE is a free data retrieval call binding the contract method 0x0bee7a67.
//
// Solidity: function ERROR_FAIL_DECODE() view returns(uint32)
func (_Tokenmanager *TokenmanagerCaller) ERRORFAILDECODE(opts *bind.CallOpts) (uint32, error) {
	var (
		ret0 = new(uint32)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "ERROR_FAIL_DECODE")
	return *ret0, err
}

// ERRORFAILDECODE is a free data retrieval call binding the contract method 0x0bee7a67.
//
// Solidity: function ERROR_FAIL_DECODE() view returns(uint32)
func (_Tokenmanager *TokenmanagerSession) ERRORFAILDECODE() (uint32, error) {
	return _Tokenmanager.Contract.ERRORFAILDECODE(&_Tokenmanager.CallOpts)
}

// ERRORFAILDECODE is a free data retrieval call binding the contract method 0x0bee7a67.
//
// Solidity: function ERROR_FAIL_DECODE() view returns(uint32)
func (_Tokenmanager *TokenmanagerCallerSession) ERRORFAILDECODE() (uint32, error) {
	return _Tokenmanager.Contract.ERRORFAILDECODE(&_Tokenmanager.CallOpts)
}

// GOVCHANNELID is a free data retrieval call binding the contract method 0x96713da9.
//
// Solidity: function GOV_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) GOVCHANNELID(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "GOV_CHANNELID")
	return *ret0, err
}

// GOVCHANNELID is a free data retrieval call binding the contract method 0x96713da9.
//
// Solidity: function GOV_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) GOVCHANNELID() (uint8, error) {
	return _Tokenmanager.Contract.GOVCHANNELID(&_Tokenmanager.CallOpts)
}

// GOVCHANNELID is a free data retrieval call binding the contract method 0x96713da9.
//
// Solidity: function GOV_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) GOVCHANNELID() (uint8, error) {
	return _Tokenmanager.Contract.GOVCHANNELID(&_Tokenmanager.CallOpts)
}

// GOVHUBADDR is a free data retrieval call binding the contract method 0x9dc09262.
//
// Solidity: function GOV_HUB_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCaller) GOVHUBADDR(opts *bind.CallOpts) (common.Address, error) {
	var (
		ret0 = new(common.Address)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "GOV_HUB_ADDR")
	return *ret0, err
}

// GOVHUBADDR is a free data retrieval call binding the contract method 0x9dc09262.
//
// Solidity: function GOV_HUB_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerSession) GOVHUBADDR() (common.Address, error) {
	return _Tokenmanager.Contract.GOVHUBADDR(&_Tokenmanager.CallOpts)
}

// GOVHUBADDR is a free data retrieval call binding the contract method 0x9dc09262.
//
// Solidity: function GOV_HUB_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCallerSession) GOVHUBADDR() (common.Address, error) {
	return _Tokenmanager.Contract.GOVHUBADDR(&_Tokenmanager.CallOpts)
}

// INCENTIVIZEADDR is a free data retrieval call binding the contract method 0x6e47b482.
//
// Solidity: function INCENTIVIZE_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCaller) INCENTIVIZEADDR(opts *bind.CallOpts) (common.Address, error) {
	var (
		ret0 = new(common.Address)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "INCENTIVIZE_ADDR")
	return *ret0, err
}

// INCENTIVIZEADDR is a free data retrieval call binding the contract method 0x6e47b482.
//
// Solidity: function INCENTIVIZE_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerSession) INCENTIVIZEADDR() (common.Address, error) {
	return _Tokenmanager.Contract.INCENTIVIZEADDR(&_Tokenmanager.CallOpts)
}

// INCENTIVIZEADDR is a free data retrieval call binding the contract method 0x6e47b482.
//
// Solidity: function INCENTIVIZE_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCallerSession) INCENTIVIZEADDR() (common.Address, error) {
	return _Tokenmanager.Contract.INCENTIVIZEADDR(&_Tokenmanager.CallOpts)
}

// LIGHTCLIENTADDR is a free data retrieval call binding the contract method 0xdc927faf.
//
// Solidity: function LIGHT_CLIENT_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCaller) LIGHTCLIENTADDR(opts *bind.CallOpts) (common.Address, error) {
	var (
		ret0 = new(common.Address)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "LIGHT_CLIENT_ADDR")
	return *ret0, err
}

// LIGHTCLIENTADDR is a free data retrieval call binding the contract method 0xdc927faf.
//
// Solidity: function LIGHT_CLIENT_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerSession) LIGHTCLIENTADDR() (common.Address, error) {
	return _Tokenmanager.Contract.LIGHTCLIENTADDR(&_Tokenmanager.CallOpts)
}

// LIGHTCLIENTADDR is a free data retrieval call binding the contract method 0xdc927faf.
//
// Solidity: function LIGHT_CLIENT_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCallerSession) LIGHTCLIENTADDR() (common.Address, error) {
	return _Tokenmanager.Contract.LIGHTCLIENTADDR(&_Tokenmanager.CallOpts)
}

// MAXIMUMBEP20SYMBOLLEN is a free data retrieval call binding the contract method 0xd9e6dae9.
//
// Solidity: function MAXIMUM_BEP20_SYMBOL_LEN() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) MAXIMUMBEP20SYMBOLLEN(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "MAXIMUM_BEP20_SYMBOL_LEN")
	return *ret0, err
}

// MAXIMUMBEP20SYMBOLLEN is a free data retrieval call binding the contract method 0xd9e6dae9.
//
// Solidity: function MAXIMUM_BEP20_SYMBOL_LEN() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) MAXIMUMBEP20SYMBOLLEN() (uint8, error) {
	return _Tokenmanager.Contract.MAXIMUMBEP20SYMBOLLEN(&_Tokenmanager.CallOpts)
}

// MAXIMUMBEP20SYMBOLLEN is a free data retrieval call binding the contract method 0xd9e6dae9.
//
// Solidity: function MAXIMUM_BEP20_SYMBOL_LEN() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) MAXIMUMBEP20SYMBOLLEN() (uint8, error) {
	return _Tokenmanager.Contract.MAXIMUMBEP20SYMBOLLEN(&_Tokenmanager.CallOpts)
}

// MINIMUMBEP20SYMBOLLEN is a free data retrieval call binding the contract method 0x66dea52a.
//
// Solidity: function MINIMUM_BEP20_SYMBOL_LEN() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) MINIMUMBEP20SYMBOLLEN(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "MINIMUM_BEP20_SYMBOL_LEN")
	return *ret0, err
}

// MINIMUMBEP20SYMBOLLEN is a free data retrieval call binding the contract method 0x66dea52a.
//
// Solidity: function MINIMUM_BEP20_SYMBOL_LEN() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) MINIMUMBEP20SYMBOLLEN() (uint8, error) {
	return _Tokenmanager.Contract.MINIMUMBEP20SYMBOLLEN(&_Tokenmanager.CallOpts)
}

// MINIMUMBEP20SYMBOLLEN is a free data retrieval call binding the contract method 0x66dea52a.
//
// Solidity: function MINIMUM_BEP20_SYMBOL_LEN() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) MINIMUMBEP20SYMBOLLEN() (uint8, error) {
	return _Tokenmanager.Contract.MINIMUMBEP20SYMBOLLEN(&_Tokenmanager.CallOpts)
}

// RELAYERHUBCONTRACTADDR is a free data retrieval call binding the contract method 0xa1a11bf5.
//
// Solidity: function RELAYERHUB_CONTRACT_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCaller) RELAYERHUBCONTRACTADDR(opts *bind.CallOpts) (common.Address, error) {
	var (
		ret0 = new(common.Address)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "RELAYERHUB_CONTRACT_ADDR")
	return *ret0, err
}

// RELAYERHUBCONTRACTADDR is a free data retrieval call binding the contract method 0xa1a11bf5.
//
// Solidity: function RELAYERHUB_CONTRACT_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerSession) RELAYERHUBCONTRACTADDR() (common.Address, error) {
	return _Tokenmanager.Contract.RELAYERHUBCONTRACTADDR(&_Tokenmanager.CallOpts)
}

// RELAYERHUBCONTRACTADDR is a free data retrieval call binding the contract method 0xa1a11bf5.
//
// Solidity: function RELAYERHUB_CONTRACT_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCallerSession) RELAYERHUBCONTRACTADDR() (common.Address, error) {
	return _Tokenmanager.Contract.RELAYERHUBCONTRACTADDR(&_Tokenmanager.CallOpts)
}

// SLASHCHANNELID is a free data retrieval call binding the contract method 0x7942fd05.
//
// Solidity: function SLASH_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) SLASHCHANNELID(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "SLASH_CHANNELID")
	return *ret0, err
}

// SLASHCHANNELID is a free data retrieval call binding the contract method 0x7942fd05.
//
// Solidity: function SLASH_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) SLASHCHANNELID() (uint8, error) {
	return _Tokenmanager.Contract.SLASHCHANNELID(&_Tokenmanager.CallOpts)
}

// SLASHCHANNELID is a free data retrieval call binding the contract method 0x7942fd05.
//
// Solidity: function SLASH_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) SLASHCHANNELID() (uint8, error) {
	return _Tokenmanager.Contract.SLASHCHANNELID(&_Tokenmanager.CallOpts)
}

// SLASHCONTRACTADDR is a free data retrieval call binding the contract method 0x43756e5c.
//
// Solidity: function SLASH_CONTRACT_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCaller) SLASHCONTRACTADDR(opts *bind.CallOpts) (common.Address, error) {
	var (
		ret0 = new(common.Address)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "SLASH_CONTRACT_ADDR")
	return *ret0, err
}

// SLASHCONTRACTADDR is a free data retrieval call binding the contract method 0x43756e5c.
//
// Solidity: function SLASH_CONTRACT_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerSession) SLASHCONTRACTADDR() (common.Address, error) {
	return _Tokenmanager.Contract.SLASHCONTRACTADDR(&_Tokenmanager.CallOpts)
}

// SLASHCONTRACTADDR is a free data retrieval call binding the contract method 0x43756e5c.
//
// Solidity: function SLASH_CONTRACT_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCallerSession) SLASHCONTRACTADDR() (common.Address, error) {
	return _Tokenmanager.Contract.SLASHCONTRACTADDR(&_Tokenmanager.CallOpts)
}

// STAKINGCHANNELID is a free data retrieval call binding the contract method 0x4bf6c882.
//
// Solidity: function STAKING_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) STAKINGCHANNELID(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "STAKING_CHANNELID")
	return *ret0, err
}

// STAKINGCHANNELID is a free data retrieval call binding the contract method 0x4bf6c882.
//
// Solidity: function STAKING_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) STAKINGCHANNELID() (uint8, error) {
	return _Tokenmanager.Contract.STAKINGCHANNELID(&_Tokenmanager.CallOpts)
}

// STAKINGCHANNELID is a free data retrieval call binding the contract method 0x4bf6c882.
//
// Solidity: function STAKING_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) STAKINGCHANNELID() (uint8, error) {
	return _Tokenmanager.Contract.STAKINGCHANNELID(&_Tokenmanager.CallOpts)
}

// SYSTEMREWARDADDR is a free data retrieval call binding the contract method 0xc81b1662.
//
// Solidity: function SYSTEM_REWARD_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCaller) SYSTEMREWARDADDR(opts *bind.CallOpts) (common.Address, error) {
	var (
		ret0 = new(common.Address)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "SYSTEM_REWARD_ADDR")
	return *ret0, err
}

// SYSTEMREWARDADDR is a free data retrieval call binding the contract method 0xc81b1662.
//
// Solidity: function SYSTEM_REWARD_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerSession) SYSTEMREWARDADDR() (common.Address, error) {
	return _Tokenmanager.Contract.SYSTEMREWARDADDR(&_Tokenmanager.CallOpts)
}

// SYSTEMREWARDADDR is a free data retrieval call binding the contract method 0xc81b1662.
//
// Solidity: function SYSTEM_REWARD_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCallerSession) SYSTEMREWARDADDR() (common.Address, error) {
	return _Tokenmanager.Contract.SYSTEMREWARDADDR(&_Tokenmanager.CallOpts)
}

// TENDECIMALS is a free data retrieval call binding the contract method 0x5d499b1b.
//
// Solidity: function TEN_DECIMALS() view returns(uint256)
func (_Tokenmanager *TokenmanagerCaller) TENDECIMALS(opts *bind.CallOpts) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "TEN_DECIMALS")
	return *ret0, err
}

// TENDECIMALS is a free data retrieval call binding the contract method 0x5d499b1b.
//
// Solidity: function TEN_DECIMALS() view returns(uint256)
func (_Tokenmanager *TokenmanagerSession) TENDECIMALS() (*big.Int, error) {
	return _Tokenmanager.Contract.TENDECIMALS(&_Tokenmanager.CallOpts)
}

// TENDECIMALS is a free data retrieval call binding the contract method 0x5d499b1b.
//
// Solidity: function TEN_DECIMALS() view returns(uint256)
func (_Tokenmanager *TokenmanagerCallerSession) TENDECIMALS() (*big.Int, error) {
	return _Tokenmanager.Contract.TENDECIMALS(&_Tokenmanager.CallOpts)
}

// TOKENHUBADDR is a free data retrieval call binding the contract method 0xfd6a6879.
//
// Solidity: function TOKEN_HUB_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCaller) TOKENHUBADDR(opts *bind.CallOpts) (common.Address, error) {
	var (
		ret0 = new(common.Address)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "TOKEN_HUB_ADDR")
	return *ret0, err
}

// TOKENHUBADDR is a free data retrieval call binding the contract method 0xfd6a6879.
//
// Solidity: function TOKEN_HUB_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerSession) TOKENHUBADDR() (common.Address, error) {
	return _Tokenmanager.Contract.TOKENHUBADDR(&_Tokenmanager.CallOpts)
}

// TOKENHUBADDR is a free data retrieval call binding the contract method 0xfd6a6879.
//
// Solidity: function TOKEN_HUB_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCallerSession) TOKENHUBADDR() (common.Address, error) {
	return _Tokenmanager.Contract.TOKENHUBADDR(&_Tokenmanager.CallOpts)
}

// TOKENMANAGERADDR is a free data retrieval call binding the contract method 0x75d47a0a.
//
// Solidity: function TOKEN_MANAGER_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCaller) TOKENMANAGERADDR(opts *bind.CallOpts) (common.Address, error) {
	var (
		ret0 = new(common.Address)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "TOKEN_MANAGER_ADDR")
	return *ret0, err
}

// TOKENMANAGERADDR is a free data retrieval call binding the contract method 0x75d47a0a.
//
// Solidity: function TOKEN_MANAGER_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerSession) TOKENMANAGERADDR() (common.Address, error) {
	return _Tokenmanager.Contract.TOKENMANAGERADDR(&_Tokenmanager.CallOpts)
}

// TOKENMANAGERADDR is a free data retrieval call binding the contract method 0x75d47a0a.
//
// Solidity: function TOKEN_MANAGER_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCallerSession) TOKENMANAGERADDR() (common.Address, error) {
	return _Tokenmanager.Contract.TOKENMANAGERADDR(&_Tokenmanager.CallOpts)
}

// TRANSFERINCHANNELID is a free data retrieval call binding the contract method 0x70fd5bad.
//
// Solidity: function TRANSFER_IN_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) TRANSFERINCHANNELID(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "TRANSFER_IN_CHANNELID")
	return *ret0, err
}

// TRANSFERINCHANNELID is a free data retrieval call binding the contract method 0x70fd5bad.
//
// Solidity: function TRANSFER_IN_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) TRANSFERINCHANNELID() (uint8, error) {
	return _Tokenmanager.Contract.TRANSFERINCHANNELID(&_Tokenmanager.CallOpts)
}

// TRANSFERINCHANNELID is a free data retrieval call binding the contract method 0x70fd5bad.
//
// Solidity: function TRANSFER_IN_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) TRANSFERINCHANNELID() (uint8, error) {
	return _Tokenmanager.Contract.TRANSFERINCHANNELID(&_Tokenmanager.CallOpts)
}

// TRANSFEROUTCHANNELID is a free data retrieval call binding the contract method 0xfc3e5908.
//
// Solidity: function TRANSFER_OUT_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) TRANSFEROUTCHANNELID(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "TRANSFER_OUT_CHANNELID")
	return *ret0, err
}

// TRANSFEROUTCHANNELID is a free data retrieval call binding the contract method 0xfc3e5908.
//
// Solidity: function TRANSFER_OUT_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) TRANSFEROUTCHANNELID() (uint8, error) {
	return _Tokenmanager.Contract.TRANSFEROUTCHANNELID(&_Tokenmanager.CallOpts)
}

// TRANSFEROUTCHANNELID is a free data retrieval call binding the contract method 0xfc3e5908.
//
// Solidity: function TRANSFER_OUT_CHANNELID() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) TRANSFEROUTCHANNELID() (uint8, error) {
	return _Tokenmanager.Contract.TRANSFEROUTCHANNELID(&_Tokenmanager.CallOpts)
}

// UNBINDPACKAGE is a free data retrieval call binding the contract method 0x23996b53.
//
// Solidity: function UNBIND_PACKAGE() view returns(uint8)
func (_Tokenmanager *TokenmanagerCaller) UNBINDPACKAGE(opts *bind.CallOpts) (uint8, error) {
	var (
		ret0 = new(uint8)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "UNBIND_PACKAGE")
	return *ret0, err
}

// UNBINDPACKAGE is a free data retrieval call binding the contract method 0x23996b53.
//
// Solidity: function UNBIND_PACKAGE() view returns(uint8)
func (_Tokenmanager *TokenmanagerSession) UNBINDPACKAGE() (uint8, error) {
	return _Tokenmanager.Contract.UNBINDPACKAGE(&_Tokenmanager.CallOpts)
}

// UNBINDPACKAGE is a free data retrieval call binding the contract method 0x23996b53.
//
// Solidity: function UNBIND_PACKAGE() view returns(uint8)
func (_Tokenmanager *TokenmanagerCallerSession) UNBINDPACKAGE() (uint8, error) {
	return _Tokenmanager.Contract.UNBINDPACKAGE(&_Tokenmanager.CallOpts)
}

// VALIDATORCONTRACTADDR is a free data retrieval call binding the contract method 0xf9a2bbc7.
//
// Solidity: function VALIDATOR_CONTRACT_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCaller) VALIDATORCONTRACTADDR(opts *bind.CallOpts) (common.Address, error) {
	var (
		ret0 = new(common.Address)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "VALIDATOR_CONTRACT_ADDR")
	return *ret0, err
}

// VALIDATORCONTRACTADDR is a free data retrieval call binding the contract method 0xf9a2bbc7.
//
// Solidity: function VALIDATOR_CONTRACT_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerSession) VALIDATORCONTRACTADDR() (common.Address, error) {
	return _Tokenmanager.Contract.VALIDATORCONTRACTADDR(&_Tokenmanager.CallOpts)
}

// VALIDATORCONTRACTADDR is a free data retrieval call binding the contract method 0xf9a2bbc7.
//
// Solidity: function VALIDATOR_CONTRACT_ADDR() view returns(address)
func (_Tokenmanager *TokenmanagerCallerSession) VALIDATORCONTRACTADDR() (common.Address, error) {
	return _Tokenmanager.Contract.VALIDATORCONTRACTADDR(&_Tokenmanager.CallOpts)
}

// AlreadyInit is a free data retrieval call binding the contract method 0xa78abc16.
//
// Solidity: function alreadyInit() view returns(bool)
func (_Tokenmanager *TokenmanagerCaller) AlreadyInit(opts *bind.CallOpts) (bool, error) {
	var (
		ret0 = new(bool)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "alreadyInit")
	return *ret0, err
}

// AlreadyInit is a free data retrieval call binding the contract method 0xa78abc16.
//
// Solidity: function alreadyInit() view returns(bool)
func (_Tokenmanager *TokenmanagerSession) AlreadyInit() (bool, error) {
	return _Tokenmanager.Contract.AlreadyInit(&_Tokenmanager.CallOpts)
}

// AlreadyInit is a free data retrieval call binding the contract method 0xa78abc16.
//
// Solidity: function alreadyInit() view returns(bool)
func (_Tokenmanager *TokenmanagerCallerSession) AlreadyInit() (bool, error) {
	return _Tokenmanager.Contract.AlreadyInit(&_Tokenmanager.CallOpts)
}

// BindPackageRecord is a free data retrieval call binding the contract method 0xd117a110.
//
// Solidity: function bindPackageRecord(bytes32 ) view returns(uint8 packageType, bytes32 bep2TokenSymbol, address contractAddr, uint256 totalSupply, uint256 peggyAmount, uint8 bep20Decimals, uint64 expireTime)
func (_Tokenmanager *TokenmanagerCaller) BindPackageRecord(opts *bind.CallOpts, arg0 [32]byte) (struct {
	PackageType     uint8
	Bep2TokenSymbol [32]byte
	ContractAddr    common.Address
	TotalSupply     *big.Int
	PeggyAmount     *big.Int
	Bep20Decimals   uint8
	ExpireTime      uint64
}, error) {
	ret := new(struct {
		PackageType     uint8
		Bep2TokenSymbol [32]byte
		ContractAddr    common.Address
		TotalSupply     *big.Int
		PeggyAmount     *big.Int
		Bep20Decimals   uint8
		ExpireTime      uint64
	})
	out := ret
	err := _Tokenmanager.contract.Call(opts, out, "bindPackageRecord", arg0)
	return *ret, err
}

// BindPackageRecord is a free data retrieval call binding the contract method 0xd117a110.
//
// Solidity: function bindPackageRecord(bytes32 ) view returns(uint8 packageType, bytes32 bep2TokenSymbol, address contractAddr, uint256 totalSupply, uint256 peggyAmount, uint8 bep20Decimals, uint64 expireTime)
func (_Tokenmanager *TokenmanagerSession) BindPackageRecord(arg0 [32]byte) (struct {
	PackageType     uint8
	Bep2TokenSymbol [32]byte
	ContractAddr    common.Address
	TotalSupply     *big.Int
	PeggyAmount     *big.Int
	Bep20Decimals   uint8
	ExpireTime      uint64
}, error) {
	return _Tokenmanager.Contract.BindPackageRecord(&_Tokenmanager.CallOpts, arg0)
}

// BindPackageRecord is a free data retrieval call binding the contract method 0xd117a110.
//
// Solidity: function bindPackageRecord(bytes32 ) view returns(uint8 packageType, bytes32 bep2TokenSymbol, address contractAddr, uint256 totalSupply, uint256 peggyAmount, uint8 bep20Decimals, uint64 expireTime)
func (_Tokenmanager *TokenmanagerCallerSession) BindPackageRecord(arg0 [32]byte) (struct {
	PackageType     uint8
	Bep2TokenSymbol [32]byte
	ContractAddr    common.Address
	TotalSupply     *big.Int
	PeggyAmount     *big.Int
	Bep20Decimals   uint8
	ExpireTime      uint64
}, error) {
	return _Tokenmanager.Contract.BindPackageRecord(&_Tokenmanager.CallOpts, arg0)
}

// BscChainID is a free data retrieval call binding the contract method 0x493279b1.
//
// Solidity: function bscChainID() view returns(uint16)
func (_Tokenmanager *TokenmanagerCaller) BscChainID(opts *bind.CallOpts) (uint16, error) {
	var (
		ret0 = new(uint16)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "bscChainID")
	return *ret0, err
}

// BscChainID is a free data retrieval call binding the contract method 0x493279b1.
//
// Solidity: function bscChainID() view returns(uint16)
func (_Tokenmanager *TokenmanagerSession) BscChainID() (uint16, error) {
	return _Tokenmanager.Contract.BscChainID(&_Tokenmanager.CallOpts)
}

// BscChainID is a free data retrieval call binding the contract method 0x493279b1.
//
// Solidity: function bscChainID() view returns(uint16)
func (_Tokenmanager *TokenmanagerCallerSession) BscChainID() (uint16, error) {
	return _Tokenmanager.Contract.BscChainID(&_Tokenmanager.CallOpts)
}

// QueryRequiredLockAmountForBind is a free data retrieval call binding the contract method 0x445fcefe.
//
// Solidity: function queryRequiredLockAmountForBind(string symbol) view returns(uint256)
func (_Tokenmanager *TokenmanagerCaller) QueryRequiredLockAmountForBind(opts *bind.CallOpts, symbol string) (*big.Int, error) {
	var (
		ret0 = new(*big.Int)
	)
	out := ret0
	err := _Tokenmanager.contract.Call(opts, out, "queryRequiredLockAmountForBind", symbol)
	return *ret0, err
}

// QueryRequiredLockAmountForBind is a free data retrieval call binding the contract method 0x445fcefe.
//
// Solidity: function queryRequiredLockAmountForBind(string symbol) view returns(uint256)
func (_Tokenmanager *TokenmanagerSession) QueryRequiredLockAmountForBind(symbol string) (*big.Int, error) {
	return _Tokenmanager.Contract.QueryRequiredLockAmountForBind(&_Tokenmanager.CallOpts, symbol)
}

// QueryRequiredLockAmountForBind is a free data retrieval call binding the contract method 0x445fcefe.
//
// Solidity: function queryRequiredLockAmountForBind(string symbol) view returns(uint256)
func (_Tokenmanager *TokenmanagerCallerSession) QueryRequiredLockAmountForBind(symbol string) (*big.Int, error) {
	return _Tokenmanager.Contract.QueryRequiredLockAmountForBind(&_Tokenmanager.CallOpts, symbol)
}

// ApproveBind is a paid mutator transaction binding the contract method 0x6b3f1307.
//
// Solidity: function approveBind(address contractAddr, string bep2Symbol) payable returns(bool)
func (_Tokenmanager *TokenmanagerTransactor) ApproveBind(opts *bind.TransactOpts, contractAddr common.Address, bep2Symbol string) (*types.Transaction, error) {
	return _Tokenmanager.contract.Transact(opts, "approveBind", contractAddr, bep2Symbol)
}

// ApproveBind is a paid mutator transaction binding the contract method 0x6b3f1307.
//
// Solidity: function approveBind(address contractAddr, string bep2Symbol) payable returns(bool)
func (_Tokenmanager *TokenmanagerSession) ApproveBind(contractAddr common.Address, bep2Symbol string) (*types.Transaction, error) {
	return _Tokenmanager.Contract.ApproveBind(&_Tokenmanager.TransactOpts, contractAddr, bep2Symbol)
}

// ApproveBind is a paid mutator transaction binding the contract method 0x6b3f1307.
//
// Solidity: function approveBind(address contractAddr, string bep2Symbol) payable returns(bool)
func (_Tokenmanager *TokenmanagerTransactorSession) ApproveBind(contractAddr common.Address, bep2Symbol string) (*types.Transaction, error) {
	return _Tokenmanager.Contract.ApproveBind(&_Tokenmanager.TransactOpts, contractAddr, bep2Symbol)
}

// ExpireBind is a paid mutator transaction binding the contract method 0x72c4e086.
//
// Solidity: function expireBind(string bep2Symbol) payable returns(bool)
func (_Tokenmanager *TokenmanagerTransactor) ExpireBind(opts *bind.TransactOpts, bep2Symbol string) (*types.Transaction, error) {
	return _Tokenmanager.contract.Transact(opts, "expireBind", bep2Symbol)
}

// ExpireBind is a paid mutator transaction binding the contract method 0x72c4e086.
//
// Solidity: function expireBind(string bep2Symbol) payable returns(bool)
func (_Tokenmanager *TokenmanagerSession) ExpireBind(bep2Symbol string) (*types.Transaction, error) {
	return _Tokenmanager.Contract.ExpireBind(&_Tokenmanager.TransactOpts, bep2Symbol)
}

// ExpireBind is a paid mutator transaction binding the contract method 0x72c4e086.
//
// Solidity: function expireBind(string bep2Symbol) payable returns(bool)
func (_Tokenmanager *TokenmanagerTransactorSession) ExpireBind(bep2Symbol string) (*types.Transaction, error) {
	return _Tokenmanager.Contract.ExpireBind(&_Tokenmanager.TransactOpts, bep2Symbol)
}

// HandleAckPackage is a paid mutator transaction binding the contract method 0x831d65d1.
//
// Solidity: function handleAckPackage(uint8 channelId, bytes msgBytes) returns()
func (_Tokenmanager *TokenmanagerTransactor) HandleAckPackage(opts *bind.TransactOpts, channelId uint8, msgBytes []byte) (*types.Transaction, error) {
	return _Tokenmanager.contract.Transact(opts, "handleAckPackage", channelId, msgBytes)
}

// HandleAckPackage is a paid mutator transaction binding the contract method 0x831d65d1.
//
// Solidity: function handleAckPackage(uint8 channelId, bytes msgBytes) returns()
func (_Tokenmanager *TokenmanagerSession) HandleAckPackage(channelId uint8, msgBytes []byte) (*types.Transaction, error) {
	return _Tokenmanager.Contract.HandleAckPackage(&_Tokenmanager.TransactOpts, channelId, msgBytes)
}

// HandleAckPackage is a paid mutator transaction binding the contract method 0x831d65d1.
//
// Solidity: function handleAckPackage(uint8 channelId, bytes msgBytes) returns()
func (_Tokenmanager *TokenmanagerTransactorSession) HandleAckPackage(channelId uint8, msgBytes []byte) (*types.Transaction, error) {
	return _Tokenmanager.Contract.HandleAckPackage(&_Tokenmanager.TransactOpts, channelId, msgBytes)
}

// HandleFailAckPackage is a paid mutator transaction binding the contract method 0xc8509d81.
//
// Solidity: function handleFailAckPackage(uint8 channelId, bytes msgBytes) returns()
func (_Tokenmanager *TokenmanagerTransactor) HandleFailAckPackage(opts *bind.TransactOpts, channelId uint8, msgBytes []byte) (*types.Transaction, error) {
	return _Tokenmanager.contract.Transact(opts, "handleFailAckPackage", channelId, msgBytes)
}

// HandleFailAckPackage is a paid mutator transaction binding the contract method 0xc8509d81.
//
// Solidity: function handleFailAckPackage(uint8 channelId, bytes msgBytes) returns()
func (_Tokenmanager *TokenmanagerSession) HandleFailAckPackage(channelId uint8, msgBytes []byte) (*types.Transaction, error) {
	return _Tokenmanager.Contract.HandleFailAckPackage(&_Tokenmanager.TransactOpts, channelId, msgBytes)
}

// HandleFailAckPackage is a paid mutator transaction binding the contract method 0xc8509d81.
//
// Solidity: function handleFailAckPackage(uint8 channelId, bytes msgBytes) returns()
func (_Tokenmanager *TokenmanagerTransactorSession) HandleFailAckPackage(channelId uint8, msgBytes []byte) (*types.Transaction, error) {
	return _Tokenmanager.Contract.HandleFailAckPackage(&_Tokenmanager.TransactOpts, channelId, msgBytes)
}

// HandleSynPackage is a paid mutator transaction binding the contract method 0x1182b875.
//
// Solidity: function handleSynPackage(uint8 , bytes msgBytes) returns(bytes)
func (_Tokenmanager *TokenmanagerTransactor) HandleSynPackage(opts *bind.TransactOpts, arg0 uint8, msgBytes []byte) (*types.Transaction, error) {
	return _Tokenmanager.contract.Transact(opts, "handleSynPackage", arg0, msgBytes)
}

// HandleSynPackage is a paid mutator transaction binding the contract method 0x1182b875.
//
// Solidity: function handleSynPackage(uint8 , bytes msgBytes) returns(bytes)
func (_Tokenmanager *TokenmanagerSession) HandleSynPackage(arg0 uint8, msgBytes []byte) (*types.Transaction, error) {
	return _Tokenmanager.Contract.HandleSynPackage(&_Tokenmanager.TransactOpts, arg0, msgBytes)
}

// HandleSynPackage is a paid mutator transaction binding the contract method 0x1182b875.
//
// Solidity: function handleSynPackage(uint8 , bytes msgBytes) returns(bytes)
func (_Tokenmanager *TokenmanagerTransactorSession) HandleSynPackage(arg0 uint8, msgBytes []byte) (*types.Transaction, error) {
	return _Tokenmanager.Contract.HandleSynPackage(&_Tokenmanager.TransactOpts, arg0, msgBytes)
}

// RejectBind is a paid mutator transaction binding the contract method 0x77d9dae8.
//
// Solidity: function rejectBind(address contractAddr, string bep2Symbol) payable returns(bool)
func (_Tokenmanager *TokenmanagerTransactor) RejectBind(opts *bind.TransactOpts, contractAddr common.Address, bep2Symbol string) (*types.Transaction, error) {
	return _Tokenmanager.contract.Transact(opts, "rejectBind", contractAddr, bep2Symbol)
}

// RejectBind is a paid mutator transaction binding the contract method 0x77d9dae8.
//
// Solidity: function rejectBind(address contractAddr, string bep2Symbol) payable returns(bool)
func (_Tokenmanager *TokenmanagerSession) RejectBind(contractAddr common.Address, bep2Symbol string) (*types.Transaction, error) {
	return _Tokenmanager.Contract.RejectBind(&_Tokenmanager.TransactOpts, contractAddr, bep2Symbol)
}

// RejectBind is a paid mutator transaction binding the contract method 0x77d9dae8.
//
// Solidity: function rejectBind(address contractAddr, string bep2Symbol) payable returns(bool)
func (_Tokenmanager *TokenmanagerTransactorSession) RejectBind(contractAddr common.Address, bep2Symbol string) (*types.Transaction, error) {
	return _Tokenmanager.Contract.RejectBind(&_Tokenmanager.TransactOpts, contractAddr, bep2Symbol)
}

// TokenmanagerBindFailureIterator is returned from FilterBindFailure and is used to iterate over the raw logs and unpacked data for BindFailure events raised by the Tokenmanager contract.
type TokenmanagerBindFailureIterator struct {
	Event *TokenmanagerBindFailure // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *TokenmanagerBindFailureIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(TokenmanagerBindFailure)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(TokenmanagerBindFailure)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *TokenmanagerBindFailureIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *TokenmanagerBindFailureIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// TokenmanagerBindFailure represents a BindFailure event raised by the Tokenmanager contract.
type TokenmanagerBindFailure struct {
	ContractAddr common.Address
	Bep2Symbol   string
	FailedReason uint32
	Raw          types.Log // Blockchain specific contextual infos
}

// FilterBindFailure is a free log retrieval operation binding the contract event 0x831c0ef4d93bda3bce08b69ae3f29ef1a6e052b833200988554158494405a107.
//
// Solidity: event bindFailure(address indexed contractAddr, string bep2Symbol, uint32 failedReason)
func (_Tokenmanager *TokenmanagerFilterer) FilterBindFailure(opts *bind.FilterOpts, contractAddr []common.Address) (*TokenmanagerBindFailureIterator, error) {

	var contractAddrRule []interface{}
	for _, contractAddrItem := range contractAddr {
		contractAddrRule = append(contractAddrRule, contractAddrItem)
	}

	logs, sub, err := _Tokenmanager.contract.FilterLogs(opts, "bindFailure", contractAddrRule)
	if err != nil {
		return nil, err
	}
	return &TokenmanagerBindFailureIterator{contract: _Tokenmanager.contract, event: "bindFailure", logs: logs, sub: sub}, nil
}

// WatchBindFailure is a free log subscription operation binding the contract event 0x831c0ef4d93bda3bce08b69ae3f29ef1a6e052b833200988554158494405a107.
//
// Solidity: event bindFailure(address indexed contractAddr, string bep2Symbol, uint32 failedReason)
func (_Tokenmanager *TokenmanagerFilterer) WatchBindFailure(opts *bind.WatchOpts, sink chan<- *TokenmanagerBindFailure, contractAddr []common.Address) (event.Subscription, error) {

	var contractAddrRule []interface{}
	for _, contractAddrItem := range contractAddr {
		contractAddrRule = append(contractAddrRule, contractAddrItem)
	}

	logs, sub, err := _Tokenmanager.contract.WatchLogs(opts, "bindFailure", contractAddrRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(TokenmanagerBindFailure)
				if err := _Tokenmanager.contract.UnpackLog(event, "bindFailure", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseBindFailure is a log parse operation binding the contract event 0x831c0ef4d93bda3bce08b69ae3f29ef1a6e052b833200988554158494405a107.
//
// Solidity: event bindFailure(address indexed contractAddr, string bep2Symbol, uint32 failedReason)
func (_Tokenmanager *TokenmanagerFilterer) ParseBindFailure(log types.Log) (*TokenmanagerBindFailure, error) {
	event := new(TokenmanagerBindFailure)
	if err := _Tokenmanager.contract.UnpackLog(event, "bindFailure", log); err != nil {
		return nil, err
	}
	return event, nil
}

// TokenmanagerBindSuccessIterator is returned from FilterBindSuccess and is used to iterate over the raw logs and unpacked data for BindSuccess events raised by the Tokenmanager contract.
type TokenmanagerBindSuccessIterator struct {
	Event *TokenmanagerBindSuccess // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *TokenmanagerBindSuccessIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(TokenmanagerBindSuccess)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(TokenmanagerBindSuccess)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *TokenmanagerBindSuccessIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *TokenmanagerBindSuccessIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// TokenmanagerBindSuccess represents a BindSuccess event raised by the Tokenmanager contract.
type TokenmanagerBindSuccess struct {
	ContractAddr common.Address
	Bep2Symbol   string
	TotalSupply  *big.Int
	PeggyAmount  *big.Int
	Raw          types.Log // Blockchain specific contextual infos
}

// FilterBindSuccess is a free log retrieval operation binding the contract event 0x78e7dd9aefcdbf795c4936a66f7dc6d41bb56637b54f561a6bf7829dca3348a8.
//
// Solidity: event bindSuccess(address indexed contractAddr, string bep2Symbol, uint256 totalSupply, uint256 peggyAmount)
func (_Tokenmanager *TokenmanagerFilterer) FilterBindSuccess(opts *bind.FilterOpts, contractAddr []common.Address) (*TokenmanagerBindSuccessIterator, error) {

	var contractAddrRule []interface{}
	for _, contractAddrItem := range contractAddr {
		contractAddrRule = append(contractAddrRule, contractAddrItem)
	}

	logs, sub, err := _Tokenmanager.contract.FilterLogs(opts, "bindSuccess", contractAddrRule)
	if err != nil {
		return nil, err
	}
	return &TokenmanagerBindSuccessIterator{contract: _Tokenmanager.contract, event: "bindSuccess", logs: logs, sub: sub}, nil
}

// WatchBindSuccess is a free log subscription operation binding the contract event 0x78e7dd9aefcdbf795c4936a66f7dc6d41bb56637b54f561a6bf7829dca3348a8.
//
// Solidity: event bindSuccess(address indexed contractAddr, string bep2Symbol, uint256 totalSupply, uint256 peggyAmount)
func (_Tokenmanager *TokenmanagerFilterer) WatchBindSuccess(opts *bind.WatchOpts, sink chan<- *TokenmanagerBindSuccess, contractAddr []common.Address) (event.Subscription, error) {

	var contractAddrRule []interface{}
	for _, contractAddrItem := range contractAddr {
		contractAddrRule = append(contractAddrRule, contractAddrItem)
	}

	logs, sub, err := _Tokenmanager.contract.WatchLogs(opts, "bindSuccess", contractAddrRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(TokenmanagerBindSuccess)
				if err := _Tokenmanager.contract.UnpackLog(event, "bindSuccess", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseBindSuccess is a log parse operation binding the contract event 0x78e7dd9aefcdbf795c4936a66f7dc6d41bb56637b54f561a6bf7829dca3348a8.
//
// Solidity: event bindSuccess(address indexed contractAddr, string bep2Symbol, uint256 totalSupply, uint256 peggyAmount)
func (_Tokenmanager *TokenmanagerFilterer) ParseBindSuccess(log types.Log) (*TokenmanagerBindSuccess, error) {
	event := new(TokenmanagerBindSuccess)
	if err := _Tokenmanager.contract.UnpackLog(event, "bindSuccess", log); err != nil {
		return nil, err
	}
	return event, nil
}

// TokenmanagerUnexpectedPackageIterator is returned from FilterUnexpectedPackage and is used to iterate over the raw logs and unpacked data for UnexpectedPackage events raised by the Tokenmanager contract.
type TokenmanagerUnexpectedPackageIterator struct {
	Event *TokenmanagerUnexpectedPackage // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *TokenmanagerUnexpectedPackageIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(TokenmanagerUnexpectedPackage)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(TokenmanagerUnexpectedPackage)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *TokenmanagerUnexpectedPackageIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *TokenmanagerUnexpectedPackageIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// TokenmanagerUnexpectedPackage represents a UnexpectedPackage event raised by the Tokenmanager contract.
type TokenmanagerUnexpectedPackage struct {
	ChannelId uint8
	MsgBytes  []byte
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterUnexpectedPackage is a free log retrieval operation binding the contract event 0x41ce201247b6ceb957dcdb217d0b8acb50b9ea0e12af9af4f5e7f38902101605.
//
// Solidity: event unexpectedPackage(uint8 channelId, bytes msgBytes)
func (_Tokenmanager *TokenmanagerFilterer) FilterUnexpectedPackage(opts *bind.FilterOpts) (*TokenmanagerUnexpectedPackageIterator, error) {

	logs, sub, err := _Tokenmanager.contract.FilterLogs(opts, "unexpectedPackage")
	if err != nil {
		return nil, err
	}
	return &TokenmanagerUnexpectedPackageIterator{contract: _Tokenmanager.contract, event: "unexpectedPackage", logs: logs, sub: sub}, nil
}

// WatchUnexpectedPackage is a free log subscription operation binding the contract event 0x41ce201247b6ceb957dcdb217d0b8acb50b9ea0e12af9af4f5e7f38902101605.
//
// Solidity: event unexpectedPackage(uint8 channelId, bytes msgBytes)
func (_Tokenmanager *TokenmanagerFilterer) WatchUnexpectedPackage(opts *bind.WatchOpts, sink chan<- *TokenmanagerUnexpectedPackage) (event.Subscription, error) {

	logs, sub, err := _Tokenmanager.contract.WatchLogs(opts, "unexpectedPackage")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(TokenmanagerUnexpectedPackage)
				if err := _Tokenmanager.contract.UnpackLog(event, "unexpectedPackage", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseUnexpectedPackage is a log parse operation binding the contract event 0x41ce201247b6ceb957dcdb217d0b8acb50b9ea0e12af9af4f5e7f38902101605.
//
// Solidity: event unexpectedPackage(uint8 channelId, bytes msgBytes)
func (_Tokenmanager *TokenmanagerFilterer) ParseUnexpectedPackage(log types.Log) (*TokenmanagerUnexpectedPackage, error) {
	event := new(TokenmanagerUnexpectedPackage)
	if err := _Tokenmanager.contract.UnpackLog(event, "unexpectedPackage", log); err != nil {
		return nil, err
	}
	return event, nil
}
