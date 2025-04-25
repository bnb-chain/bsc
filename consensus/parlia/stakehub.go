package parlia

import (
	"context"
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rpc"
)

// GetValidators retrieves validators from the StakeHubContract
// It returns operator addresses, credit addresses, and total length of validators
func (p *Parlia) GetValidators(blockNumber uint64, offset, limit *big.Int) ([]common.Address, []common.Address, *big.Int, error) {
	// Create the call data for getValidators
	data, err := p.stakeHubABI.Pack("getValidators", offset, limit)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to pack getValidators: %v", err)
	}

	// Make the call
	blockNr := rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(blockNumber))
	msgData := (hexutil.Bytes)(data)
	toAddress := common.HexToAddress(systemcontracts.StakeHubContract)
	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))

	result, err := p.ethAPI.Call(context.Background(), ethapi.TransactionArgs{
		Gas:  &gas,
		To:   &toAddress,
		Data: &msgData,
	}, &blockNr, nil, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to call getValidators: %v", err)
	}

	// Unpack the result
	var operatorAddrs []common.Address
	var creditAddrs []common.Address
	var totalLength *big.Int
	if err := p.stakeHubABI.UnpackIntoInterface(&[]interface{}{&operatorAddrs, &creditAddrs, &totalLength}, "getValidators", result); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to unpack getValidators result: %v", err)
	}

	return operatorAddrs, creditAddrs, totalLength, nil
}

// ListNodeIDsFor retrieves node IDs for the given validators
// It returns an array of node ID arrays, one for each validator
func (p *Parlia) ListNodeIDsFor(blockNumber uint64, validatorsToQuery []common.Address) ([][]enode.ID, error) {
	// Create the call data for listNodeIDsFor
	data, err := p.stakeHubABI.Pack("listNodeIDsFor", validatorsToQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to pack listNodeIDsFor: %v", err)
	}

	// Make the call
	blockNr := rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(blockNumber))
	msgData := (hexutil.Bytes)(data)
	toAddress := common.HexToAddress(systemcontracts.StakeHubContract)
	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))

	result, err := p.ethAPI.Call(context.Background(), ethapi.TransactionArgs{
		Gas:  &gas,
		To:   &toAddress,
		Data: &msgData,
	}, &blockNr, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call listNodeIDsFor: %v", err)
	}

	// Unpack the result
	var nodeIDs [][]enode.ID
	if err := p.stakeHubABI.UnpackIntoInterface(&nodeIDs, "listNodeIDsFor", result); err != nil {
		return nil, fmt.Errorf("failed to unpack listNodeIDsFor result: %v", err)
	}

	return nodeIDs, nil
}

// GetNodeIDs returns a flattened array of all node IDs for current validators
func (p *Parlia) GetNodeIDs() ([]enode.ID, error) {
	// Get latest block number
	block := p.ethAPI.BlockNumber()

	// Call GetValidators with latest block number
	operatorAddrs, _, _, err := p.GetValidators(uint64(block), big.NewInt(0), big.NewInt(1000))
	if err != nil {
		return nil, fmt.Errorf("failed to get validators: %v", err)
	}

	// Get node IDs for validators
	nodeIDs, err := p.ListNodeIDsFor(uint64(block), operatorAddrs)
	if err != nil {
		return nil, fmt.Errorf("failed to get node IDs: %v", err)
	}

	// Flatten the array of arrays into a single array
	flatNodeIDs := make([]enode.ID, 0)
	for _, nodeIDArray := range nodeIDs {
		flatNodeIDs = append(flatNodeIDs, nodeIDArray...)
	}
	return flatNodeIDs, nil
}

// AddNodeIDs creates a signed transaction to add node IDs to the StakeHub contract
func (p *Parlia) AddNodeIDs(nodeIDs []enode.ID, nonce uint64) (*types.Transaction, error) {
	p.lock.RLock()
	signTxFn := p.signTxFn
	val := p.val
	p.lock.RUnlock()

	if signTxFn == nil {
		return nil, fmt.Errorf("signing function not set, call Authorize first")
	}

	// Create the call data for addNodeIDs
	data, err := p.stakeHubABI.Pack("addNodeIDs", nodeIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to pack addNodeIDs: %v", err)
	}

	// Create the transaction
	tx := types.NewTransaction(
		nonce,
		common.HexToAddress(systemcontracts.StakeHubContract),
		common.Big0,
		math.MaxUint64/2,
		common.Big0,
		data,
	)

	// Sign the transaction with the node's private key
	signedTx, err := signTxFn(accounts.Account{Address: val}, tx, p.chainConfig.ChainID)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %v", err)
	}

	return signedTx, nil
}

// GetNodeIDsForConsensus returns node IDs for validators identified by their consensus addresses
func (p *Parlia) GetNodeIDsForConsensus(consensusAddresses []common.Address) ([][]enode.ID, error) {
	// Get latest block number
	block := p.ethAPI.BlockNumber()
	log.Debug("Getting node IDs for consensus addresses", "block", block, "addresses", consensusAddresses)

	// Create the call data for listNodeIDsForConsensus
	data, err := p.stakeHubABI.Pack("listNodeIDsForConsensus", consensusAddresses)
	if err != nil {
		log.Error("Failed to pack listNodeIDsForConsensus", "error", err)
		return nil, fmt.Errorf("failed to pack listNodeIDsForConsensus: %v", err)
	}

	// Make the call
	blockNr := rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(block))
	msgData := (hexutil.Bytes)(data)
	toAddress := common.HexToAddress(systemcontracts.StakeHubContract)
	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))

	log.Debug("Calling listNodeIDsForConsensus", "block", block, "to", toAddress)
	result, err := p.ethAPI.Call(context.Background(), ethapi.TransactionArgs{
		Gas:  &gas,
		To:   &toAddress,
		Data: &msgData,
	}, &blockNr, nil, nil)
	if err != nil {
		log.Error("Failed to call listNodeIDsForConsensus", "error", err)
		return nil, fmt.Errorf("failed to call listNodeIDsForConsensus: %v", err)
	}

	// Unpack the result
	var nodeIDs [][]enode.ID
	if err := p.stakeHubABI.UnpackIntoInterface(&nodeIDs, "listNodeIDsForConsensus", result); err != nil {
		log.Error("Failed to unpack listNodeIDsForConsensus result", "error", err)
		return nil, fmt.Errorf("failed to unpack listNodeIDsForConsensus result: %v", err)
	}

	log.Debug("Successfully retrieved node IDs", "count", len(nodeIDs))
	return nodeIDs, nil
}
