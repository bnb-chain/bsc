// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package main

import (
	"errors"
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
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
)

// SlashIndicatorFinalityEvidence is an auto generated low-level Go binding around an user-defined struct.
type SlashIndicatorFinalityEvidence struct {
	VoteA    SlashIndicatorVoteData
	VoteB    SlashIndicatorVoteData
	VoteAddr []byte
}

// SlashIndicatorVoteData is an auto generated low-level Go binding around an user-defined struct.
type SlashIndicatorVoteData struct {
	SrcNum  *big.Int
	SrcHash [32]byte
	TarNum  *big.Int
	TarHash [32]byte
	Sig     []byte
}

// SlashIndicatorMetaData contains all meta data concerning the SlashIndicator contract.
var SlashIndicatorMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"srcNum\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"srcHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"tarNum\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"tarHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"sig\",\"type\":\"bytes\"}],\"internalType\":\"structSlashIndicator.VoteData\",\"name\":\"voteA\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"srcNum\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"srcHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"tarNum\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"tarHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"sig\",\"type\":\"bytes\"}],\"internalType\":\"structSlashIndicator.VoteData\",\"name\":\"voteB\",\"type\":\"tuple\"},{\"internalType\":\"bytes\",\"name\":\"voteAddr\",\"type\":\"bytes\"}],\"internalType\":\"structSlashIndicator.FinalityEvidence\",\"name\":\"_evidence\",\"type\":\"tuple\"}],\"name\":\"submitFinalityViolationEvidence\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// SlashIndicatorABI is the input ABI used to generate the binding from.
// Deprecated: Use SlashIndicatorMetaData.ABI instead.
var SlashIndicatorABI = SlashIndicatorMetaData.ABI

// SlashIndicator is an auto generated Go binding around an Ethereum contract.
type SlashIndicator struct {
	SlashIndicatorCaller     // Read-only binding to the contract
	SlashIndicatorTransactor // Write-only binding to the contract
	SlashIndicatorFilterer   // Log filterer for contract events
}

// SlashIndicatorCaller is an auto generated read-only Go binding around an Ethereum contract.
type SlashIndicatorCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SlashIndicatorTransactor is an auto generated write-only Go binding around an Ethereum contract.
type SlashIndicatorTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SlashIndicatorFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type SlashIndicatorFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SlashIndicatorSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type SlashIndicatorSession struct {
	Contract     *SlashIndicator   // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// SlashIndicatorCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type SlashIndicatorCallerSession struct {
	Contract *SlashIndicatorCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts         // Call options to use throughout this session
}

// SlashIndicatorTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type SlashIndicatorTransactorSession struct {
	Contract     *SlashIndicatorTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts         // Transaction auth options to use throughout this session
}

// SlashIndicatorRaw is an auto generated low-level Go binding around an Ethereum contract.
type SlashIndicatorRaw struct {
	Contract *SlashIndicator // Generic contract binding to access the raw methods on
}

// SlashIndicatorCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type SlashIndicatorCallerRaw struct {
	Contract *SlashIndicatorCaller // Generic read-only contract binding to access the raw methods on
}

// SlashIndicatorTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type SlashIndicatorTransactorRaw struct {
	Contract *SlashIndicatorTransactor // Generic write-only contract binding to access the raw methods on
}

// NewSlashIndicator creates a new instance of SlashIndicator, bound to a specific deployed contract.
func NewSlashIndicator(address common.Address, backend bind.ContractBackend) (*SlashIndicator, error) {
	contract, err := bindSlashIndicator(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &SlashIndicator{SlashIndicatorCaller: SlashIndicatorCaller{contract: contract}, SlashIndicatorTransactor: SlashIndicatorTransactor{contract: contract}, SlashIndicatorFilterer: SlashIndicatorFilterer{contract: contract}}, nil
}

// NewSlashIndicatorCaller creates a new read-only instance of SlashIndicator, bound to a specific deployed contract.
func NewSlashIndicatorCaller(address common.Address, caller bind.ContractCaller) (*SlashIndicatorCaller, error) {
	contract, err := bindSlashIndicator(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &SlashIndicatorCaller{contract: contract}, nil
}

// NewSlashIndicatorTransactor creates a new write-only instance of SlashIndicator, bound to a specific deployed contract.
func NewSlashIndicatorTransactor(address common.Address, transactor bind.ContractTransactor) (*SlashIndicatorTransactor, error) {
	contract, err := bindSlashIndicator(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &SlashIndicatorTransactor{contract: contract}, nil
}

// NewSlashIndicatorFilterer creates a new log filterer instance of SlashIndicator, bound to a specific deployed contract.
func NewSlashIndicatorFilterer(address common.Address, filterer bind.ContractFilterer) (*SlashIndicatorFilterer, error) {
	contract, err := bindSlashIndicator(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &SlashIndicatorFilterer{contract: contract}, nil
}

// bindSlashIndicator binds a generic wrapper to an already deployed contract.
func bindSlashIndicator(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := abi.JSON(strings.NewReader(SlashIndicatorABI))
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_SlashIndicator *SlashIndicatorRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _SlashIndicator.Contract.SlashIndicatorCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_SlashIndicator *SlashIndicatorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _SlashIndicator.Contract.SlashIndicatorTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_SlashIndicator *SlashIndicatorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _SlashIndicator.Contract.SlashIndicatorTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_SlashIndicator *SlashIndicatorCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _SlashIndicator.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_SlashIndicator *SlashIndicatorTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _SlashIndicator.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_SlashIndicator *SlashIndicatorTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _SlashIndicator.Contract.contract.Transact(opts, method, params...)
}

// SubmitFinalityViolationEvidence is a paid mutator transaction binding the contract method 0xcc844b73.
//
// Solidity: function submitFinalityViolationEvidence(((uint256,bytes32,uint256,bytes32,bytes),(uint256,bytes32,uint256,bytes32,bytes),bytes) _evidence) returns()
func (_SlashIndicator *SlashIndicatorTransactor) SubmitFinalityViolationEvidence(opts *bind.TransactOpts, _evidence SlashIndicatorFinalityEvidence) (*types.Transaction, error) {
	return _SlashIndicator.contract.Transact(opts, "submitFinalityViolationEvidence", _evidence)
}

// SubmitFinalityViolationEvidence is a paid mutator transaction binding the contract method 0xcc844b73.
//
// Solidity: function submitFinalityViolationEvidence(((uint256,bytes32,uint256,bytes32,bytes),(uint256,bytes32,uint256,bytes32,bytes),bytes) _evidence) returns()
func (_SlashIndicator *SlashIndicatorSession) SubmitFinalityViolationEvidence(_evidence SlashIndicatorFinalityEvidence) (*types.Transaction, error) {
	return _SlashIndicator.Contract.SubmitFinalityViolationEvidence(&_SlashIndicator.TransactOpts, _evidence)
}

// SubmitFinalityViolationEvidence is a paid mutator transaction binding the contract method 0xcc844b73.
//
// Solidity: function submitFinalityViolationEvidence(((uint256,bytes32,uint256,bytes32,bytes),(uint256,bytes32,uint256,bytes32,bytes),bytes) _evidence) returns()
func (_SlashIndicator *SlashIndicatorTransactorSession) SubmitFinalityViolationEvidence(_evidence SlashIndicatorFinalityEvidence) (*types.Transaction, error) {
	return _SlashIndicator.Contract.SubmitFinalityViolationEvidence(&_SlashIndicator.TransactOpts, _evidence)
}
