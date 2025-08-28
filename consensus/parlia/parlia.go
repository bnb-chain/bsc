package parlia

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/holiman/uint256"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
	"github.com/willf/bitset"
	"golang.org/x/crypto/sha3"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/gopool"
	"github.com/ethereum/go-ethereum/common/hexutil"
	cmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip1559"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/trie"
)

const (
	inMemorySnapshots  = 1280  // Number of recent snapshots to keep in memory; a buffer exceeding the EpochLength
	inMemorySignatures = 4096  // Number of recent block signatures to keep in memory
	inMemoryHeaders    = 86400 // Number of recent headers to keep in memory for double sign detection,

	checkpointInterval = 1024 // Number of blocks after which to save the snapshot to the database

	defaultEpochLength   uint64 = 200  // Default number of blocks of checkpoint to update validatorSet from contract
	lorentzEpochLength   uint64 = 500  // Epoch length starting from the Lorentz hard fork
	maxwellEpochLength   uint64 = 1000 // Epoch length starting from the Maxwell hard fork
	defaultBlockInterval uint64 = 3000 // Default block interval in milliseconds
	lorentzBlockInterval uint64 = 1500 // Block interval starting from the Lorentz hard fork
	maxwellBlockInterval uint64 = 750  // Block interval starting from the Maxwell hard fork
	defaultTurnLength    uint8  = 1    // Default consecutive number of blocks a validator receives priority for block production

	extraVanity      = 32 // Fixed number of extra-data prefix bytes reserved for signer vanity
	extraSeal        = 65 // Fixed number of extra-data suffix bytes reserved for signer seal
	nextForkHashSize = 4  // Fixed number of extra-data suffix bytes reserved for nextForkHash.
	turnLengthSize   = 1  // Fixed number of extra-data suffix bytes reserved for turnLength

	validatorBytesLengthBeforeLuban = common.AddressLength
	validatorBytesLength            = common.AddressLength + types.BLSPublicKeyLength
	validatorNumberSize             = 1 // Fixed number of extra prefix bytes reserved for validator number after Luban

	wiggleTime                uint64 = 1000 // milliseconds, Random delay (per signer) to allow concurrent signers
	defaultInitialBackOffTime uint64 = 1000 // milliseconds, Default backoff time for the second validator permitted to produce blocks
	lorentzInitialBackOffTime uint64 = 2000 // milliseconds, Backoff time for the second validator permitted to produce blocks from the Lorentz hard fork

	systemRewardPercent = 4 // it means 1/2^4 = 1/16 percentage of gas fee incoming will be distributed to system

	collectAdditionalVotesRewardRatio = 100 // ratio of additional reward for collecting more votes than needed, the denominator is 100

	gasLimitBoundDivisorBeforeLorentz uint64 = 256 // The bound divisor of the gas limit, used in update calculations before lorentz hard fork.

	// `finalityRewardInterval` should be smaller than `inMemorySnapshots`, otherwise, it will result in excessive computation.
	finalityRewardInterval = 200
)

var (
	diffInTurn = big.NewInt(2) // Block difficulty for in-turn signatures
	diffNoTurn = big.NewInt(1) // Block difficulty for out-of-turn signatures
	// 100 native token
	maxSystemBalance                  = new(uint256.Int).Mul(uint256.NewInt(100), uint256.NewInt(params.Ether))
	verifyVoteAttestationErrorCounter = metrics.NewRegisteredCounter("parlia/verifyVoteAttestation/error", nil)
	updateAttestationErrorCounter     = metrics.NewRegisteredCounter("parlia/updateAttestation/error", nil)
	validVotesfromSelfCounter         = metrics.NewRegisteredCounter("parlia/VerifyVote/self", nil)
	doubleSignCounter                 = metrics.NewRegisteredCounter("parlia/doublesign", nil)
	intentionalDelayMiningCounter     = metrics.NewRegisteredCounter("parlia/intentionalDelayMining", nil)

	systemContracts = map[common.Address]bool{
		common.HexToAddress(systemcontracts.ValidatorContract):          true,
		common.HexToAddress(systemcontracts.SlashContract):              true,
		common.HexToAddress(systemcontracts.SystemRewardContract):       true,
		common.HexToAddress(systemcontracts.LightClientContract):        true,
		common.HexToAddress(systemcontracts.RelayerHubContract):         true,
		common.HexToAddress(systemcontracts.GovHubContract):             true,
		common.HexToAddress(systemcontracts.TokenHubContract):           true,
		common.HexToAddress(systemcontracts.RelayerIncentivizeContract): true,
		common.HexToAddress(systemcontracts.CrossChainContract):         true,
		common.HexToAddress(systemcontracts.StakeHubContract):           true,
		common.HexToAddress(systemcontracts.GovernorContract):           true,
		common.HexToAddress(systemcontracts.GovTokenContract):           true,
		common.HexToAddress(systemcontracts.TimelockContract):           true,
		common.HexToAddress(systemcontracts.TokenRecoverPortalContract): true,
	}
)

// Various error messages to mark blocks invalid. These should be private to
// prevent engine specific errors from being referenced in the remainder of the
// codebase, inherently breaking if the engine is swapped out. Please put common
// error types into the consensus package.
var (
	// errUnknownBlock is returned when the list of validators is requested for a block
	// that is not part of the local blockchain.
	errUnknownBlock = errors.New("unknown block")

	// errMissingVanity is returned if a block's extra-data section is shorter than
	// 32 bytes, which is required to store the signer vanity.
	errMissingVanity = errors.New("extra-data 32 byte vanity prefix missing")

	// errMissingSignature is returned if a block's extra-data section doesn't seem
	// to contain a 65 byte secp256k1 signature.
	errMissingSignature = errors.New("extra-data 65 byte signature suffix missing")

	// errExtraValidators is returned if non-sprint-end block contain validator data in
	// their extra-data fields.
	errExtraValidators = errors.New("non-sprint-end block contains extra validator list")

	// errInvalidSpanValidators is returned if a block contains an
	// invalid list of validators (i.e. non divisible by 20 bytes).
	errInvalidSpanValidators = errors.New("invalid validator list on sprint end block")

	// errInvalidTurnLength is returned if a block contains an
	// invalid length of turn (i.e. no data left after parsing validators).
	errInvalidTurnLength = errors.New("invalid turnLength")

	// errInvalidMixDigest is returned if a block's mix digest is non-zero.
	errInvalidMixDigest = errors.New("non-zero mix digest")

	// errInvalidUncleHash is returned if a block contains an non-empty uncle list.
	errInvalidUncleHash = errors.New("non empty uncle hash")

	// errMismatchingEpochValidators is returned if a sprint block contains a
	// list of validators different than the one the local node calculated.
	errMismatchingEpochValidators = errors.New("mismatching validator list on epoch block")

	// errMismatchingEpochTurnLength is returned if a sprint block contains a
	// turn length different than the one the local node calculated.
	errMismatchingEpochTurnLength = errors.New("mismatching turn length on epoch block")

	// errInvalidDifficulty is returned if the difficulty of a block is missing.
	errInvalidDifficulty = errors.New("invalid difficulty")

	// errWrongDifficulty is returned if the difficulty of a block doesn't match the
	// turn of the signer.
	errWrongDifficulty = errors.New("wrong difficulty")

	// errOutOfRangeChain is returned if an authorization list is attempted to
	// be modified via out-of-range or non-contiguous headers.
	errOutOfRangeChain = errors.New("out of range or non-contiguous chain")

	// errBlockHashInconsistent is returned if an authorization list is attempted to
	// insert an inconsistent block.
	errBlockHashInconsistent = errors.New("the block hash is inconsistent")

	// errUnauthorizedValidator is returned if a header is signed by a non-authorized entity.
	errUnauthorizedValidator = func(val string) error {
		return errors.New("unauthorized validator: " + val)
	}

	// errCoinBaseMisMatch is returned if a header's coinbase do not match with signature
	errCoinBaseMisMatch = errors.New("coinbase do not match with signature")

	// errRecentlySigned is returned if a header is signed by an authorized entity
	// that already signed a header recently, thus is temporarily not allowed to.
	errRecentlySigned = errors.New("recently signed")
)

// SignerFn is a signer callback function to request a header to be signed by a
// backing account.
type SignerFn func(accounts.Account, string, []byte) ([]byte, error)
type SignerTxFn func(accounts.Account, *types.Transaction, *big.Int) (*types.Transaction, error)

func isToSystemContract(to common.Address) bool {
	return systemContracts[to]
}

// ecrecover extracts the Ethereum account address from a signed header.
func ecrecover(header *types.Header, sigCache *lru.Cache[common.Hash, common.Address], chainId *big.Int) (common.Address, error) {
	// If the signature's already cached, return that
	hash := header.Hash()
	if address, known := sigCache.Get(hash); known {
		return address, nil
	}
	// Retrieve the signature from the header extra-data
	if len(header.Extra) < extraSeal {
		return common.Address{}, errMissingSignature
	}
	signature := header.Extra[len(header.Extra)-extraSeal:]

	// Recover the public key and the Ethereum address
	pubkey, err := crypto.Ecrecover(types.SealHash(header, chainId).Bytes(), signature)
	if err != nil {
		return common.Address{}, err
	}
	var signer common.Address
	copy(signer[:], crypto.Keccak256(pubkey[1:])[12:])

	sigCache.Add(hash, signer)
	return signer, nil
}

// ParliaRLP returns the rlp bytes which needs to be signed for the parlia
// sealing. The RLP to sign consists of the entire header apart from the 65 byte signature
// contained at the end of the extra data.
//
// Note, the method requires the extra data to be at least 65 bytes, otherwise it
// panics. This is done to avoid accidentally using both forms (signature present
// or not), which could be abused to produce different hashes for the same header.
func ParliaRLP(header *types.Header, chainId *big.Int) []byte {
	b := new(bytes.Buffer)
	types.EncodeSigHeader(b, header, chainId)
	return b.Bytes()
}

// Parlia is the consensus engine of BSC
type Parlia struct {
	chainConfig *params.ChainConfig  // Chain config
	config      *params.ParliaConfig // Consensus engine configuration parameters for parlia consensus
	genesisHash common.Hash
	db          ethdb.Database // Database to store and retrieve snapshot checkpoints

	recentSnaps   *lru.Cache[common.Hash, *Snapshot]      // Snapshots for recent block to speed up
	signatures    *lru.Cache[common.Hash, common.Address] // Signatures of recent blocks to speed up mining
	recentHeaders *lru.Cache[string, common.Hash]
	// Recent headers to check for double signing: key includes block number and miner. value is the block header
	// If same key's value already exists for different block header roots then double sign is detected

	signer types.Signer

	val      common.Address // Ethereum address of the signing key
	signFn   SignerFn       // Signer function to authorize hashes with
	signTxFn SignerTxFn

	lock sync.RWMutex // Protects the signer fields

	ethAPI                     *ethapi.BlockChainAPI
	VotePool                   consensus.VotePool
	validatorSetABIBeforeLuban abi.ABI
	validatorSetABI            abi.ABI
	slashABI                   abi.ABI
	stakeHubABI                abi.ABI

	// The fields below are for testing only
	fakeDiff bool // Skip difficulty verifications
}

// New creates a Parlia consensus engine.
func New(
	chainConfig *params.ChainConfig,
	db ethdb.Database,
	ethAPI *ethapi.BlockChainAPI,
	genesisHash common.Hash,
) *Parlia {
	// get parlia config
	parliaConfig := chainConfig.Parlia
	log.Info("Parlia", "chainConfig", chainConfig)

	vABIBeforeLuban, err := abi.JSON(strings.NewReader(validatorSetABIBeforeLuban))
	if err != nil {
		panic(err)
	}
	vABI, err := abi.JSON(strings.NewReader(validatorSetABI))
	if err != nil {
		panic(err)
	}
	sABI, err := abi.JSON(strings.NewReader(slashABI))
	if err != nil {
		panic(err)
	}
	stABI, err := abi.JSON(strings.NewReader(stakeABI))
	if err != nil {
		panic(err)
	}
	c := &Parlia{
		chainConfig:                chainConfig,
		config:                     parliaConfig,
		genesisHash:                genesisHash,
		db:                         db,
		ethAPI:                     ethAPI,
		recentSnaps:                lru.NewCache[common.Hash, *Snapshot](inMemorySnapshots),
		recentHeaders:              lru.NewCache[string, common.Hash](inMemoryHeaders),
		signatures:                 lru.NewCache[common.Hash, common.Address](inMemorySignatures),
		validatorSetABIBeforeLuban: vABIBeforeLuban,
		validatorSetABI:            vABI,
		slashABI:                   sABI,
		stakeHubABI:                stABI,
		signer:                     types.LatestSigner(chainConfig),
	}

	return c
}

func (p *Parlia) IsSystemTransaction(tx *types.Transaction, header *types.Header) (bool, error) {
	if tx.To() == nil || !isToSystemContract(*tx.To()) {
		return false, nil
	}
	if tx.GasPrice().Sign() != 0 {
		return false, nil
	}
	sender, err := types.Sender(p.signer, tx)
	if err != nil {
		return false, errors.New("UnAuthorized transaction")
	}
	return sender == header.Coinbase, nil
}

func (p *Parlia) IsSystemContract(to *common.Address) bool {
	if to == nil {
		return false
	}
	return isToSystemContract(*to)
}

// Author implements consensus.Engine, returning the SystemAddress
func (p *Parlia) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

// ConsensusAddress returns the consensus address of the validator
func (p *Parlia) ConsensusAddress() common.Address {
	return p.val
}

// VerifyHeader checks whether a header conforms to the consensus rules.
func (p *Parlia) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header) error {
	return p.verifyHeader(chain, header, nil)
}

// VerifyHeaders is similar to VerifyHeader, but verifies a batch of headers. The
// method returns a quit channel to abort the operations and a results channel to
// retrieve the async verifications (the order is that of the input slice).
func (p *Parlia) VerifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header) (chan<- struct{}, <-chan error) {
	abort := make(chan struct{})
	results := make(chan error, len(headers))

	gopool.Submit(func() {
		for i, header := range headers {
			err := p.verifyHeader(chain, header, headers[:i])

			select {
			case <-abort:
				return
			case results <- err:
			}
		}
	})
	return abort, results
}

// getValidatorBytesFromHeader returns the validators bytes extracted from the header's extra field if exists.
// The validators bytes would be contained only in the epoch block's header, and its each validator bytes length is fixed.
// On luban fork, we introduce vote attestation into the header's extra field, so extra format is different from before.
// Before luban fork: |---Extra Vanity---|---Validators Bytes (or Empty)---|---Extra Seal---|
// After luban fork:  |---Extra Vanity---|---Validators Number and Validators Bytes (or Empty)---|---Vote Attestation (or Empty)---|---Extra Seal---|
// After bohr fork:   |---Extra Vanity---|---Validators Number, Validators Bytes and Turn Length (or Empty)---|---Vote Attestation (or Empty)---|---Extra Seal---|
func getValidatorBytesFromHeader(header *types.Header, chainConfig *params.ChainConfig, epochLength uint64) []byte {
	if len(header.Extra) <= extraVanity+extraSeal {
		return nil
	}

	if !chainConfig.IsLuban(header.Number) {
		if header.Number.Uint64()%epochLength == 0 && (len(header.Extra)-extraSeal-extraVanity)%validatorBytesLengthBeforeLuban != 0 {
			return nil
		}
		return header.Extra[extraVanity : len(header.Extra)-extraSeal]
	}

	if header.Number.Uint64()%epochLength != 0 {
		return nil
	}
	num := int(header.Extra[extraVanity])
	start := extraVanity + validatorNumberSize
	end := start + num*validatorBytesLength
	extraMinLen := end + extraSeal
	if chainConfig.IsBohr(header.Number, header.Time) {
		extraMinLen += turnLengthSize
	}
	if num == 0 || len(header.Extra) < extraMinLen {
		return nil
	}
	return header.Extra[start:end]
}

// getVoteAttestationFromHeader returns the vote attestation extracted from the header's extra field if exists.
func getVoteAttestationFromHeader(header *types.Header, chainConfig *params.ChainConfig, epochLength uint64) (*types.VoteAttestation, error) {
	if len(header.Extra) <= extraVanity+extraSeal {
		return nil, nil
	}

	if !chainConfig.IsLuban(header.Number) {
		return nil, nil
	}

	var attestationBytes []byte
	if header.Number.Uint64()%epochLength != 0 {
		attestationBytes = header.Extra[extraVanity : len(header.Extra)-extraSeal]
	} else {
		num := int(header.Extra[extraVanity])
		start := extraVanity + validatorNumberSize + num*validatorBytesLength
		if chainConfig.IsBohr(header.Number, header.Time) {
			start += turnLengthSize
		}
		end := len(header.Extra) - extraSeal
		if end <= start {
			return nil, nil
		}
		attestationBytes = header.Extra[start:end]
	}

	var attestation types.VoteAttestation
	if err := rlp.Decode(bytes.NewReader(attestationBytes), &attestation); err != nil {
		return nil, fmt.Errorf("block %d has vote attestation info, decode err: %s", header.Number.Uint64(), err)
	}
	return &attestation, nil
}

// getParent returns the parent of a given block.
func (p *Parlia) getParent(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header) (*types.Header, error) {
	var parent *types.Header
	number := header.Number.Uint64()
	if len(parents) > 0 {
		parent = parents[len(parents)-1]
	} else {
		parent = chain.GetHeader(header.ParentHash, number-1)
	}

	if parent == nil || parent.Number.Uint64() != number-1 || parent.Hash() != header.ParentHash {
		return nil, consensus.ErrUnknownAncestor
	}
	return parent, nil
}

// verifyVoteAttestation checks whether the vote attestation in the header is valid.
func (p *Parlia) verifyVoteAttestation(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header) error {
	epochLength, err := p.epochLength(chain, header, parents)
	if err != nil {
		return err
	}
	attestation, err := getVoteAttestationFromHeader(header, chain.Config(), epochLength)
	if err != nil {
		return err
	}
	if attestation == nil {
		return nil
	}
	if attestation.Data == nil {
		return errors.New("invalid attestation, vote data is nil")
	}
	if len(attestation.Extra) > types.MaxAttestationExtraLength {
		return fmt.Errorf("invalid attestation, too large extra length: %d", len(attestation.Extra))
	}

	// Get parent block
	parent, err := p.getParent(chain, header, parents)
	if err != nil {
		return err
	}

	// The target block should be direct parent.
	targetNumber := attestation.Data.TargetNumber
	targetHash := attestation.Data.TargetHash
	if targetNumber != parent.Number.Uint64() || targetHash != parent.Hash() {
		return fmt.Errorf("invalid attestation, target mismatch, expected block: %d, hash: %s; real block: %d, hash: %s",
			parent.Number.Uint64(), parent.Hash(), targetNumber, targetHash)
	}

	// The source block should be the highest justified block.
	sourceNumber := attestation.Data.SourceNumber
	sourceHash := attestation.Data.SourceHash
	headers := []*types.Header{parent}
	if len(parents) > 0 {
		headers = parents
	}
	justifiedBlockNumber, justifiedBlockHash, err := p.GetJustifiedNumberAndHash(chain, headers)
	if err != nil {
		return errors.New("unexpected error when getting the highest justified number and hash")
	}
	if sourceNumber != justifiedBlockNumber || sourceHash != justifiedBlockHash {
		return fmt.Errorf("invalid attestation, source mismatch, expected block: %d, hash: %s; real block: %d, hash: %s",
			justifiedBlockNumber, justifiedBlockHash, sourceNumber, sourceHash)
	}

	// The snapshot should be the targetNumber-1 block's snapshot.
	if len(parents) > 1 {
		parents = parents[:len(parents)-1]
	} else {
		parents = nil
	}
	snap, err := p.snapshot(chain, parent.Number.Uint64()-1, parent.ParentHash, parents)
	if err != nil {
		return err
	}

	// Filter out valid validator from attestation.
	validators := snap.validators()
	validatorsBitSet := bitset.From([]uint64{uint64(attestation.VoteAddressSet)})
	if validatorsBitSet.Count() > uint(len(validators)) {
		return errors.New("invalid attestation, vote number larger than validators number")
	}
	votedAddrs := make([]bls.PublicKey, 0, validatorsBitSet.Count())
	for index, val := range validators {
		if !validatorsBitSet.Test(uint(index)) {
			continue
		}

		voteAddr, err := bls.PublicKeyFromBytes(snap.Validators[val].VoteAddress[:])
		if err != nil {
			return fmt.Errorf("BLS public key converts failed: %v", err)
		}
		votedAddrs = append(votedAddrs, voteAddr)
	}

	// The valid voted validators should be no less than 2/3 validators.
	if len(votedAddrs) < cmath.CeilDiv(len(snap.Validators)*2, 3) {
		return errors.New("invalid attestation, not enough validators voted")
	}

	// Verify the aggregated signature.
	aggSig, err := bls.SignatureFromBytes(attestation.AggSignature[:])
	if err != nil {
		return fmt.Errorf("BLS signature converts failed: %v", err)
	}
	if !aggSig.FastAggregateVerify(votedAddrs, attestation.Data.Hash()) {
		return errors.New("invalid attestation, signature verify failed")
	}

	return nil
}

// verifyHeader checks whether a header conforms to the consensus rules.The
// caller may optionally pass in a batch of parents (ascending order) to avoid
// looking those up from the database. This is useful for concurrently verifying
// a batch of new headers.
func (p *Parlia) verifyHeader(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header) error {
	if header.Number == nil {
		return errUnknownBlock
	}

	// Don't waste time checking blocks from the future
	if header.Time > uint64(time.Now().Unix()) {
		return consensus.ErrFutureBlock
	}
	// Check that the extra-data contains the vanity, validators and signature.
	if len(header.Extra) < extraVanity {
		return errMissingVanity
	}
	if len(header.Extra) < extraVanity+extraSeal {
		return errMissingSignature
	}

	// check extra data
	number := header.Number.Uint64()
	epochLength, err := p.epochLength(chain, header, parents)
	if err != nil {
		return err
	}
	// Ensure that the extra-data contains a signer list on checkpoint, but none otherwise
	signersBytes := getValidatorBytesFromHeader(header, p.chainConfig, epochLength)
	isEpoch := number%epochLength == 0
	if !isEpoch && len(signersBytes) != 0 {
		return errExtraValidators
	}
	if isEpoch && len(signersBytes) == 0 {
		return errInvalidSpanValidators
	}

	lorentz := chain.Config().IsLorentz(header.Number, header.Time)
	if !lorentz {
		if header.MixDigest != (common.Hash{}) {
			return errInvalidMixDigest
		}
	} else {
		if header.MilliTimestamp()/1000 != header.Time {
			return fmt.Errorf("invalid MixDigest, have %#x, expected the last two bytes to represent milliseconds", header.MixDigest)
		}
	}
	// Ensure that the block doesn't contain any uncles which are meaningless in PoA
	if header.UncleHash != types.EmptyUncleHash {
		return errInvalidUncleHash
	}
	// Ensure that the block's difficulty is meaningful (may not be correct at this point)
	if number > 0 {
		if header.Difficulty == nil {
			return errInvalidDifficulty
		}
	}

	parent, err := p.getParent(chain, header, parents)
	if err != nil {
		return err
	}

	// Verify the block's gas usage and (if applicable) verify the base fee.
	if !chain.Config().IsLondon(header.Number) {
		// Verify BaseFee not present before EIP-1559 fork.
		if header.BaseFee != nil {
			return fmt.Errorf("invalid baseFee before fork: have %d, expected 'nil'", header.BaseFee)
		}
	} else if err := eip1559.VerifyEIP1559Header(chain.Config(), parent, header); err != nil {
		// Verify the header's EIP-1559 attributes.
		return err
	}

	cancun := chain.Config().IsCancun(header.Number, header.Time)
	if !cancun {
		switch {
		case header.ExcessBlobGas != nil:
			return fmt.Errorf("invalid excessBlobGas: have %d, expected nil", header.ExcessBlobGas)
		case header.BlobGasUsed != nil:
			return fmt.Errorf("invalid blobGasUsed: have %d, expected nil", header.BlobGasUsed)
		case header.WithdrawalsHash != nil:
			return fmt.Errorf("invalid WithdrawalsHash, have %#x, expected nil", header.WithdrawalsHash)
		}
	} else {
		switch {
		case !header.EmptyWithdrawalsHash():
			return errors.New("header has wrong WithdrawalsHash")
		}
		if err := eip4844.VerifyEIP4844Header(chain.Config(), parent, header); err != nil {
			return err
		}
	}

	bohr := chain.Config().IsBohr(header.Number, header.Time)
	if !bohr {
		if header.ParentBeaconRoot != nil {
			return fmt.Errorf("invalid parentBeaconRoot, have %#x, expected nil", header.ParentBeaconRoot)
		}
	} else {
		if header.ParentBeaconRoot == nil || *header.ParentBeaconRoot != (common.Hash{}) {
			return fmt.Errorf("invalid parentBeaconRoot, have %#x, expected zero hash", header.ParentBeaconRoot)
		}
	}

	prague := chain.Config().IsPrague(header.Number, header.Time)
	if !prague {
		if header.RequestsHash != nil {
			return fmt.Errorf("invalid RequestsHash, have %#x, expected nil", header.RequestsHash)
		}
	} else {
		if header.RequestsHash == nil {
			return errors.New("header has nil RequestsHash after Prague")
		}
	}

	// All basic checks passed, verify cascading fields
	return p.verifyCascadingFields(chain, header, parents)
}

// verifyCascadingFields verifies all the header fields that are not standalone,
// rather depend on a batch of previous headers. The caller may optionally pass
// in a batch of parents (ascending order) to avoid looking those up from the
// database. This is useful for concurrently verifying a batch of new headers.
func (p *Parlia) verifyCascadingFields(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header) error {
	// The genesis block is the always valid dead-end
	number := header.Number.Uint64()
	if number == 0 {
		return nil
	}

	parent, err := p.getParent(chain, header, parents)
	if err != nil {
		return err
	}

	snap, err := p.snapshot(chain, number-1, header.ParentHash, parents)
	if err != nil {
		return err
	}

	err = p.blockTimeVerifyForRamanujanFork(snap, header, parent)
	if err != nil {
		return err
	}

	// Verify that the gas limit is <= 2^63-1
	capacity := uint64(0x7fffffffffffffff)
	if header.GasLimit > capacity {
		return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, capacity)
	}
	// Verify that the gasUsed is <= gasLimit
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
	}

	// Verify that the gas limit remains within allowed bounds
	diff := int64(parent.GasLimit) - int64(header.GasLimit)
	if diff < 0 {
		diff *= -1
	}
	gasLimitBoundDivisor := gasLimitBoundDivisorBeforeLorentz
	if p.chainConfig.IsLorentz(header.Number, header.Time) {
		gasLimitBoundDivisor = params.GasLimitBoundDivisor
	}
	limit := parent.GasLimit / gasLimitBoundDivisor

	if uint64(diff) >= limit || header.GasLimit < params.MinGasLimit {
		return fmt.Errorf("invalid gas limit: have %d, want %d += %d", header.GasLimit, parent.GasLimit, limit-1)
	}

	// Verify vote attestation for fast finality.
	if err := p.verifyVoteAttestation(chain, header, parents); err != nil {
		log.Warn("Verify vote attestation failed", "error", err, "hash", header.Hash(), "number", header.Number,
			"parent", header.ParentHash, "coinbase", header.Coinbase, "extra", common.Bytes2Hex(header.Extra))
		verifyVoteAttestationErrorCounter.Inc(1)
		if chain.Config().IsPlato(header.Number) {
			return err
		}
	}

	// All basic checks passed, verify the seal and return
	return p.verifySeal(chain, header, parents)
}

// snapshot retrieves the authorization snapshot at a given point in time.
// !!! be careful
// the block with `number` and `hash` is just the last element of `parents`,
// unlike other interfaces such as verifyCascadingFields, `parents` are real parents
func (p *Parlia) snapshot(chain consensus.ChainHeaderReader, number uint64, hash common.Hash, parents []*types.Header) (*Snapshot, error) {
	// Search for a snapshot in memory or on disk for checkpoints
	var (
		headers []*types.Header
		snap    *Snapshot
	)

	for snap == nil {
		// If an in-memory snapshot was found, use that
		if s, ok := p.recentSnaps.Get(hash); ok {
			snap = s
			break
		}

		// If an on-disk checkpoint snapshot can be found, use that
		if number%checkpointInterval == 0 {
			if s, err := loadSnapshot(p.config, p.signatures, p.db, hash, p.ethAPI); err == nil {
				log.Trace("Loaded snapshot from disk", "number", number, "hash", hash)
				snap = s
				break
			}
		}

		// If we're at the genesis, snapshot the initial state. Alternatively if we have
		// piled up more headers than allowed to be reorged (chain reinit from a freezer),
		// consider the checkpoint trusted and snapshot it.

		// Unable to retrieve the exact EpochLength here.
		// As known
		// 		defaultEpochLength = 200 && turnLength = 1 or 4
		// 		lorentzEpochLength = 500 && turnLength = 8
		// 		maxwellEpochLength = 1000 && turnLength = 16
		// So just select block number like 1200, 2200, 3200, we can always get the right validators from `number - 200`
		offset := uint64(200)
		if number == 0 || (number%maxwellEpochLength == offset && (len(headers) > int(params.FullImmutabilityThreshold))) {
			var (
				checkpoint    *types.Header
				blockHash     common.Hash
				blockInterval = defaultBlockInterval
				epochLength   = defaultEpochLength
			)
			if number == 0 {
				checkpoint = chain.GetHeaderByNumber(0)
				if checkpoint != nil {
					blockHash = checkpoint.Hash()
				}
			} else {
				checkpoint = chain.GetHeaderByNumber(number - offset)
				blockHeader := chain.GetHeaderByNumber(number)
				if blockHeader != nil {
					blockHash = blockHeader.Hash()
					if p.chainConfig.IsMaxwell(blockHeader.Number, blockHeader.Time) {
						blockInterval = maxwellBlockInterval
					} else if p.chainConfig.IsLorentz(blockHeader.Number, blockHeader.Time) {
						blockInterval = lorentzBlockInterval
					}
				}
				if number > offset { // exclude `number == 200`
					blockBeforeCheckpoint := chain.GetHeaderByNumber(number - offset - 1)
					if blockBeforeCheckpoint != nil {
						if p.chainConfig.IsMaxwell(blockBeforeCheckpoint.Number, blockBeforeCheckpoint.Time) {
							epochLength = maxwellEpochLength
						} else if p.chainConfig.IsLorentz(blockBeforeCheckpoint.Number, blockBeforeCheckpoint.Time) {
							epochLength = lorentzEpochLength
						}
					}
				}
			}
			if checkpoint != nil && blockHash != (common.Hash{}) {
				// get validators from headers
				validators, voteAddrs, err := parseValidators(checkpoint, p.chainConfig, epochLength)
				if err != nil {
					return nil, err
				}

				// new snapshot
				snap = newSnapshot(p.config, p.signatures, number, blockHash, validators, voteAddrs, p.ethAPI)

				// get turnLength from headers and use that for new turnLength
				turnLength, err := parseTurnLength(checkpoint, p.chainConfig, epochLength)
				if err != nil {
					return nil, err
				}
				if turnLength != nil {
					snap.TurnLength = *turnLength
				}
				snap.BlockInterval = blockInterval
				snap.EpochLength = epochLength

				// snap.Recents is currently empty, which affects the following:
				// a. The function SignRecently - This is acceptable since an empty snap.Recents results in a more lenient check.
				// b. The function blockTimeVerifyForRamanujanFork - This is also acceptable as it won't be invoked during `snap.apply`.
				// c. This may cause a mismatch in the slash systemtx, but the transaction list is not verified during `snap.apply`.

				// snap.Attestation is nil, but Snapshot.updateAttestation will handle it correctly.
				if err := snap.store(p.db); err != nil {
					return nil, err
				}
				log.Info("Stored checkpoint snapshot to disk", "number", number, "hash", blockHash)
				break
			}
		}

		// No snapshot for this header, gather the header and move backward
		var header *types.Header
		if len(parents) > 0 {
			// If we have explicit parents, pick from there (enforced)
			header = parents[len(parents)-1]
			if header.Hash() != hash || header.Number.Uint64() != number {
				return nil, consensus.ErrUnknownAncestor
			}
			parents = parents[:len(parents)-1]
		} else {
			// No explicit parents (or no more left), reach out to the database
			header = chain.GetHeader(hash, number)
			if header == nil {
				return nil, consensus.ErrUnknownAncestor
			}
		}
		headers = append(headers, header)
		number, hash = number-1, header.ParentHash
	}

	// check if snapshot is nil
	if snap == nil {
		return nil, fmt.Errorf("unknown error while retrieving snapshot at block number %v", number)
	}

	// Previous snapshot found, apply any pending headers on top of it
	for i := 0; i < len(headers)/2; i++ {
		headers[i], headers[len(headers)-1-i] = headers[len(headers)-1-i], headers[i]
	}

	snap, err := snap.apply(headers, chain, parents, p.chainConfig)
	if err != nil {
		return nil, err
	}
	p.recentSnaps.Add(snap.Hash, snap)

	// If we've generated a new checkpoint snapshot, save to disk
	if snap.Number%checkpointInterval == 0 && len(headers) > 0 {
		if err = snap.store(p.db); err != nil {
			return nil, err
		}
		log.Trace("Stored snapshot to disk", "number", snap.Number, "hash", snap.Hash)
	}
	return snap, err
}

// VerifyUncles implements consensus.Engine, always returning an error for any
// uncles as this consensus mechanism doesn't permit uncles.
func (p *Parlia) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	if len(block.Uncles()) > 0 {
		return errors.New("uncles not allowed")
	}
	return nil
}

func (p *Parlia) VerifyRequests(header *types.Header, Requests [][]byte) error {
	return nil
}

// VerifySeal implements consensus.Engine, checking whether the signature contained
// in the header satisfies the consensus protocol requirements.
func (p *Parlia) VerifySeal(chain consensus.ChainReader, header *types.Header) error {
	return p.verifySeal(chain, header, nil)
}

// verifySeal checks whether the signature contained in the header satisfies the
// consensus protocol requirements. The method accepts an optional list of parent
// headers that aren't yet part of the local blockchain to generate the snapshots
// from.
func (p *Parlia) verifySeal(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header) error {
	// Verifying the genesis block is not supported
	number := header.Number.Uint64()
	if number == 0 {
		return errUnknownBlock
	}
	// Retrieve the snapshot needed to verify this header and cache it
	snap, err := p.snapshot(chain, number-1, header.ParentHash, parents)
	if err != nil {
		return err
	}

	// Resolve the authorization key and check against validators
	signer, err := ecrecover(header, p.signatures, p.chainConfig.ChainID)
	if err != nil {
		return err
	}

	if signer != header.Coinbase {
		return errCoinBaseMisMatch
	}

	// check for double sign & add to cache
	key := proposalKey(*header)
	preHash, ok := p.recentHeaders.Get(key)
	if ok && preHash != header.Hash() {
		doubleSignCounter.Inc(1)
		log.Warn("DoubleSign detected", " block", header.Number, " miner", header.Coinbase,
			"hash1", preHash, "hash2", header.Hash())
	} else {
		p.recentHeaders.Add(key, header.Hash())
	}

	if _, ok := snap.Validators[signer]; !ok {
		return errUnauthorizedValidator(signer.String())
	}

	if snap.SignRecently(signer) {
		return errRecentlySigned
	}

	// Ensure that the difficulty corresponds to the turn-ness of the signer
	if !p.fakeDiff {
		inturn := snap.inturn(signer)
		if inturn && header.Difficulty.Cmp(diffInTurn) != 0 {
			return errWrongDifficulty
		}
		if !inturn && header.Difficulty.Cmp(diffNoTurn) != 0 {
			return errWrongDifficulty
		}
	}

	return nil
}

func (p *Parlia) prepareValidators(chain consensus.ChainHeaderReader, header *types.Header) error {
	epochLength, err := p.epochLength(chain, header, nil)
	if err != nil {
		return err
	}
	if header.Number.Uint64()%epochLength != 0 {
		return nil
	}

	newValidators, voteAddressMap, err := p.getCurrentValidators(header.ParentHash, new(big.Int).Sub(header.Number, big.NewInt(1)))
	if err != nil {
		return err
	}
	// sort validator by address
	sort.Sort(validatorsAscending(newValidators))
	if !p.chainConfig.IsLuban(header.Number) {
		for _, validator := range newValidators {
			header.Extra = append(header.Extra, validator.Bytes()...)
		}
	} else {
		header.Extra = append(header.Extra, byte(len(newValidators)))
		if p.chainConfig.IsOnLuban(header.Number) {
			voteAddressMap = make(map[common.Address]*types.BLSPublicKey, len(newValidators))
			var zeroBlsKey types.BLSPublicKey
			for _, validator := range newValidators {
				voteAddressMap[validator] = &zeroBlsKey
			}
		}
		for _, validator := range newValidators {
			header.Extra = append(header.Extra, validator.Bytes()...)
			header.Extra = append(header.Extra, voteAddressMap[validator].Bytes()...)
		}
	}
	return nil
}

func (p *Parlia) prepareTurnLength(chain consensus.ChainHeaderReader, header *types.Header) error {
	epochLength, err := p.epochLength(chain, header, nil)
	if err != nil {
		return err
	}
	if header.Number.Uint64()%epochLength != 0 ||
		!p.chainConfig.IsBohr(header.Number, header.Time) {
		return nil
	}

	turnLength, err := p.getTurnLength(chain, header)
	if err != nil {
		return err
	}

	if turnLength != nil {
		header.Extra = append(header.Extra, *turnLength)
	}

	return nil
}

func (p *Parlia) assembleVoteAttestation(chain consensus.ChainHeaderReader, header *types.Header) error {
	if !p.chainConfig.IsLuban(header.Number) || header.Number.Uint64() < 2 {
		return nil
	}

	if p.VotePool == nil {
		return nil
	}

	// Fetch direct parent's votes
	parent := chain.GetHeaderByHash(header.ParentHash)
	if parent == nil {
		return errors.New("parent not found")
	}
	snap, err := p.snapshot(chain, parent.Number.Uint64()-1, parent.ParentHash, nil)
	if err != nil {
		return err
	}
	votes := p.VotePool.FetchVoteByBlockHash(parent.Hash())
	if len(votes) < cmath.CeilDiv(len(snap.Validators)*2, 3) {
		return nil
	}

	// Prepare vote attestation
	// Prepare vote data
	justifiedBlockNumber, justifiedBlockHash, err := p.GetJustifiedNumberAndHash(chain, []*types.Header{parent})
	if err != nil {
		return errors.New("unexpected error when getting the highest justified number and hash")
	}
	attestation := &types.VoteAttestation{
		Data: &types.VoteData{
			SourceNumber: justifiedBlockNumber,
			SourceHash:   justifiedBlockHash,
			TargetNumber: parent.Number.Uint64(),
			TargetHash:   parent.Hash(),
		},
	}
	// Check vote data from votes
	for _, vote := range votes {
		if vote.Data.Hash() != attestation.Data.Hash() {
			return fmt.Errorf("vote check error, expected: %v, real: %v", attestation.Data, vote)
		}
	}
	// Prepare aggregated vote signature
	voteAddrSet := make(map[types.BLSPublicKey]struct{}, len(votes))
	signatures := make([][]byte, 0, len(votes))
	for _, vote := range votes {
		voteAddrSet[vote.VoteAddress] = struct{}{}
		signatures = append(signatures, vote.Signature[:])
	}
	sigs, err := bls.MultipleSignaturesFromBytes(signatures)
	if err != nil {
		return err
	}
	copy(attestation.AggSignature[:], bls.AggregateSignatures(sigs).Marshal())
	// Prepare vote address bitset.
	for _, valInfo := range snap.Validators {
		if _, ok := voteAddrSet[valInfo.VoteAddress]; ok {
			attestation.VoteAddressSet |= 1 << (valInfo.Index - 1) // Index is offset by 1
		}
	}
	validatorsBitSet := bitset.From([]uint64{uint64(attestation.VoteAddressSet)})
	if validatorsBitSet.Count() < uint(len(signatures)) {
		log.Warn(fmt.Sprintf("assembleVoteAttestation, check VoteAddress Set failed, expected:%d, real:%d", len(signatures), validatorsBitSet.Count()))
		return errors.New("invalid attestation, check VoteAddress Set failed")
	}

	// Append attestation to header extra field.
	buf := new(bytes.Buffer)
	err = rlp.Encode(buf, attestation)
	if err != nil {
		return err
	}

	// Insert vote attestation into header extra ahead extra seal.
	extraSealStart := len(header.Extra) - extraSeal
	extraSealBytes := header.Extra[extraSealStart:]
	header.Extra = append(header.Extra[0:extraSealStart], buf.Bytes()...)
	header.Extra = append(header.Extra, extraSealBytes...)

	return nil
}

// NextInTurnValidator return the next in-turn validator for header
func (p *Parlia) NextInTurnValidator(chain consensus.ChainHeaderReader, header *types.Header) (common.Address, error) {
	snap, err := p.snapshot(chain, header.Number.Uint64(), header.Hash(), nil)
	if err != nil {
		return common.Address{}, err
	}

	return snap.inturnValidator(), nil
}

// Prepare implements consensus.Engine, preparing all the consensus fields of the
// header for running the transactions on top.
func (p *Parlia) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	header.Coinbase = p.val
	header.Nonce = types.BlockNonce{}

	number := header.Number.Uint64()
	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		return err
	}

	// Set the correct difficulty
	header.Difficulty = CalcDifficulty(snap, p.val)

	// Ensure the extra data has all it's components
	if len(header.Extra) < extraVanity-nextForkHashSize {
		header.Extra = append(header.Extra, bytes.Repeat([]byte{0x00}, extraVanity-nextForkHashSize-len(header.Extra))...)
	}

	// Ensure the timestamp has the correct delay
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	blockTime := p.blockTimeForRamanujanFork(snap, header, parent)
	header.Time = blockTime / 1000 // get seconds
	if p.chainConfig.IsLorentz(header.Number, header.Time) {
		header.SetMilliseconds(blockTime % 1000)
	} else {
		header.MixDigest = common.Hash{}
	}

	header.Extra = header.Extra[:extraVanity-nextForkHashSize]
	nextForkHash := forkid.NextForkHash(p.chainConfig, p.genesisHash, chain.GenesisHeader().Time, number, header.Time)
	header.Extra = append(header.Extra, nextForkHash[:]...)

	if err := p.prepareValidators(chain, header); err != nil {
		return err
	}

	if err := p.prepareTurnLength(chain, header); err != nil {
		return err
	}
	// add extra seal space
	header.Extra = append(header.Extra, make([]byte, extraSeal)...)

	return nil
}

func (p *Parlia) verifyValidators(chain consensus.ChainHeaderReader, header *types.Header) error {
	epochLength, err := p.epochLength(chain, header, nil)
	if err != nil {
		return err
	}
	if header.Number.Uint64()%epochLength != 0 {
		return nil
	}

	newValidators, voteAddressMap, err := p.getCurrentValidators(header.ParentHash, new(big.Int).Sub(header.Number, big.NewInt(1)))
	if err != nil {
		return err
	}
	// sort validator by address
	sort.Sort(validatorsAscending(newValidators))
	var validatorsBytes []byte
	validatorsNumber := len(newValidators)
	if !p.chainConfig.IsLuban(header.Number) {
		validatorsBytes = make([]byte, validatorsNumber*validatorBytesLengthBeforeLuban)
		for i, validator := range newValidators {
			copy(validatorsBytes[i*validatorBytesLengthBeforeLuban:], validator.Bytes())
		}
	} else {
		if uint8(validatorsNumber) != header.Extra[extraVanity] {
			return errMismatchingEpochValidators
		}
		validatorsBytes = make([]byte, validatorsNumber*validatorBytesLength)
		if p.chainConfig.IsOnLuban(header.Number) {
			voteAddressMap = make(map[common.Address]*types.BLSPublicKey, len(newValidators))
			var zeroBlsKey types.BLSPublicKey
			for _, validator := range newValidators {
				voteAddressMap[validator] = &zeroBlsKey
			}
		}
		for i, validator := range newValidators {
			copy(validatorsBytes[i*validatorBytesLength:], validator.Bytes())
			copy(validatorsBytes[i*validatorBytesLength+common.AddressLength:], voteAddressMap[validator].Bytes())
		}
	}
	if !bytes.Equal(getValidatorBytesFromHeader(header, p.chainConfig, epochLength), validatorsBytes) {
		return errMismatchingEpochValidators
	}
	return nil
}

func (p *Parlia) verifyTurnLength(chain consensus.ChainHeaderReader, header *types.Header) error {
	epochLength, err := p.epochLength(chain, header, nil)
	if err != nil {
		return err
	}
	if header.Number.Uint64()%epochLength != 0 ||
		!p.chainConfig.IsBohr(header.Number, header.Time) {
		return nil
	}

	turnLengthFromHeader, err := parseTurnLength(header, p.chainConfig, epochLength)
	if err != nil {
		return err
	}
	if turnLengthFromHeader != nil {
		turnLength, err := p.getTurnLength(chain, header)
		if err != nil {
			return err
		}
		if turnLength != nil && *turnLength == *turnLengthFromHeader {
			log.Debug("verifyTurnLength", "turnLength", *turnLength)
			return nil
		}
	}

	return errMismatchingEpochTurnLength
}

func (p *Parlia) distributeFinalityReward(chain consensus.ChainHeaderReader, state vm.StateDB, header *types.Header,
	cx core.ChainContext, txs *[]*types.Transaction, receipts *[]*types.Receipt, systemTxs *[]*types.Transaction,
	usedGas *uint64, mining bool, tracer *tracing.Hooks) error {
	currentHeight := header.Number.Uint64()
	if currentHeight%finalityRewardInterval != 0 {
		return nil
	}

	head := header
	accumulatedWeights := make(map[common.Address]uint64)
	for height := currentHeight - 1; height+finalityRewardInterval >= currentHeight && height >= 1; height-- {
		head = chain.GetHeaderByHash(head.ParentHash)
		if head == nil {
			return fmt.Errorf("header is nil at height %d", height)
		}
		epochLength, err := p.epochLength(chain, head, nil)
		if err != nil {
			return err
		}
		voteAttestation, err := getVoteAttestationFromHeader(head, chain.Config(), epochLength)
		if err != nil {
			return err
		}
		if voteAttestation == nil {
			continue
		}
		justifiedBlock := chain.GetHeaderByHash(voteAttestation.Data.TargetHash)
		if justifiedBlock == nil {
			log.Warn("justifiedBlock is nil at height %d", voteAttestation.Data.TargetNumber)
			continue
		}

		snap, err := p.snapshot(chain, justifiedBlock.Number.Uint64()-1, justifiedBlock.ParentHash, nil)
		if err != nil {
			return err
		}
		validators := snap.validators()
		validatorsBitSet := bitset.From([]uint64{uint64(voteAttestation.VoteAddressSet)})
		if validatorsBitSet.Count() > uint(len(validators)) {
			log.Error("invalid attestation, vote number larger than validators number")
			continue
		}
		validVoteCount := 0
		for index, val := range validators {
			if validatorsBitSet.Test(uint(index)) {
				accumulatedWeights[val] += 1
				validVoteCount += 1
			}
		}
		quorum := cmath.CeilDiv(len(snap.Validators)*2, 3)
		if validVoteCount > quorum {
			accumulatedWeights[head.Coinbase] += uint64((validVoteCount - quorum) * collectAdditionalVotesRewardRatio / 100)
		}
	}

	validators := make([]common.Address, 0, len(accumulatedWeights))
	weights := make([]*big.Int, 0, len(accumulatedWeights))
	for val := range accumulatedWeights {
		validators = append(validators, val)
	}
	sort.Sort(validatorsAscending(validators))
	for _, val := range validators {
		weights = append(weights, big.NewInt(int64(accumulatedWeights[val])))
	}

	// generate system transaction
	method := "distributeFinalityReward"
	data, err := p.validatorSetABI.Pack(method, validators, weights)
	if err != nil {
		log.Error("Unable to pack tx for distributeFinalityReward", "error", err)
		return err
	}
	msg := p.getSystemMessage(header.Coinbase, common.HexToAddress(systemcontracts.ValidatorContract), data, common.Big0)
	return p.applyTransaction(msg, state, header, cx, txs, receipts, systemTxs, usedGas, mining, tracer)
}

func (p *Parlia) EstimateGasReservedForSystemTxs(chain consensus.ChainHeaderReader, header *types.Header) uint64 {
	parent := chain.GetHeaderByHash(header.ParentHash)
	if parent != nil {
		// Mainnet and Chapel have both passed Feynman. Now, simplify the logic before and during the Feynman hard fork.
		if p.chainConfig.IsFeynman(header.Number, header.Time) &&
			!p.chainConfig.IsOnFeynman(header.Number, parent.Time, header.Time) {
			// const (
			// 	the following values represent the maximum values found in the most recent blocks on the mainnet
			// 	depositTxGas         = uint64(60_000)
			// 	slashTxGas           = uint64(140_000)
			// 	finalityRewardTxGas  = uint64(350_000)
			// 	updateValidatorTxGas = uint64(12_160_000)
			// )
			// suggestReservedGas := depositTxGas
			// if header.Difficulty.Cmp(diffInTurn) != 0 {
			// 	snap, err := p.snapshot(chain, header.Number.Uint64()-1, header.ParentHash, nil)
			// 	if err != nil || !snap.SignRecently(snap.inturnValidator()) {
			// 		suggestReservedGas += slashTxGas
			// 	}
			// }
			// if header.Number.Uint64()%p.config.Epoch == 0 {
			// 	suggestReservedGas += finalityRewardTxGas
			// }
			// if isBreatheBlock(parent.Time, header.Time) {
			// 	suggestReservedGas += updateValidatorTxGas
			// }
			// return suggestReservedGas * 150 / 100
			if !isBreatheBlock(parent.Time, header.Time) {
				// params.SystemTxsGasSoftLimit > (depositTxGas+slashTxGas+finalityRewardTxGas)*150/100
				return params.SystemTxsGasSoftLimit
			}
		}
	}

	// params.SystemTxsGasHardLimit > (depositTxGas+slashTxGas+finalityRewardTxGas+updateValidatorTxGas)*150/100
	return params.SystemTxsGasHardLimit
}

// Finalize implements consensus.Engine, ensuring no uncles are set, nor block
// rewards given.
func (p *Parlia) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state vm.StateDB, txs *[]*types.Transaction,
	uncles []*types.Header, _ []*types.Withdrawal, receipts *[]*types.Receipt, systemTxs *[]*types.Transaction, usedGas *uint64, tracer *tracing.Hooks) error {
	// warn if not in majority fork
	p.detectNewVersionWithFork(chain, header, state)

	// If the block is an epoch end block, verify the validator list
	// The verification can only be done when the state is ready, it can't be done in VerifyHeader.
	if err := p.verifyValidators(chain, header); err != nil {
		return err
	}

	if err := p.verifyTurnLength(chain, header); err != nil {
		return err
	}

	cx := chainContext{Chain: chain, parlia: p}

	parent := chain.GetHeaderByHash(header.ParentHash)
	if parent == nil {
		return errors.New("parent not found")
	}

	systemcontracts.TryUpdateBuildInSystemContract(p.chainConfig, header.Number, parent.Time, header.Time, state, false)

	if err := p.checkNanoBlackList(state, header); err != nil {
		return err
	}
	if p.chainConfig.IsOnFeynman(header.Number, parent.Time, header.Time) {
		err := p.initializeFeynmanContract(state, header, cx, txs, receipts, systemTxs, usedGas, false, tracer)
		if err != nil {
			log.Error("init feynman contract failed", "error", err)
		}
	}

	// No block rewards in PoA, so the state remains as is and uncles are dropped
	if header.Number.Cmp(common.Big1) == 0 {
		err := p.initContract(state, header, cx, txs, receipts, systemTxs, usedGas, false, tracer)
		if err != nil {
			log.Error("init contract failed")
		}
	}
	if header.Difficulty.Cmp(diffInTurn) != 0 {
		snap, err := p.snapshot(chain, header.Number.Uint64()-1, header.ParentHash, nil)
		if err != nil {
			return err
		}
		spoiledVal := snap.inturnValidator()
		signedRecently := false
		if p.chainConfig.IsPlato(header.Number) {
			signedRecently = snap.SignRecently(spoiledVal)
		} else {
			for _, recent := range snap.Recents {
				if recent == spoiledVal {
					signedRecently = true
					break
				}
			}
		}

		if !signedRecently {
			log.Trace("slash validator", "block hash", header.Hash(), "address", spoiledVal)
			err = p.slash(spoiledVal, state, header, cx, txs, receipts, systemTxs, usedGas, false, tracer)
			if err != nil {
				// it is possible that slash validator failed because of the slash channel is disabled.
				log.Error("slash validator failed", "block hash", header.Hash(), "address", spoiledVal, "err", err)
			}
		}
	}

	val := header.Coinbase
	PenalizeForDelayMining, err := p.isIntentionalDelayMining(chain, header)
	if err != nil {
		log.Debug("unexpected error happened when detecting intentional delay mining", "err", err)
	}
	if PenalizeForDelayMining {
		intentionalDelayMiningCounter.Inc(1)
		log.Warn("intentional delay mining detected", "validator", val, "number", header.Number, "hash", header.Hash())
	}
	err = p.distributeIncoming(val, state, header, cx, txs, receipts, systemTxs, usedGas, false, tracer)
	if err != nil {
		return err
	}

	if p.chainConfig.IsPlato(header.Number) {
		if err := p.distributeFinalityReward(chain, state, header, cx, txs, receipts, systemTxs, usedGas, false, tracer); err != nil {
			return err
		}
	}

	// update validators every day
	if p.chainConfig.IsFeynman(header.Number, header.Time) && isBreatheBlock(parent.Time, header.Time) {
		// we should avoid update validators in the Feynman upgrade block
		if !p.chainConfig.IsOnFeynman(header.Number, parent.Time, header.Time) {
			if err := p.updateValidatorSetV2(state, header, cx, txs, receipts, systemTxs, usedGas, false, tracer); err != nil {
				return err
			}
		}
	}

	if len(*systemTxs) > 0 {
		return errors.New("the length of systemTxs do not match")
	}
	return nil
}

// FinalizeAndAssemble implements consensus.Engine, ensuring no uncles are set,
// nor block rewards given, and returns the final block.
func (p *Parlia) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB,
	body *types.Body, receipts []*types.Receipt, tracer *tracing.Hooks) (*types.Block, []*types.Receipt, error) {
	// No block rewards in PoA, so the state remains as is and uncles are dropped
	cx := chainContext{Chain: chain, parlia: p}

	if body.Transactions == nil {
		body.Transactions = make([]*types.Transaction, 0)
	}
	if receipts == nil {
		receipts = make([]*types.Receipt, 0)
	}

	parent := chain.GetHeaderByHash(header.ParentHash)
	if parent == nil {
		return nil, nil, errors.New("parent not found")
	}

	systemcontracts.TryUpdateBuildInSystemContract(p.chainConfig, header.Number, parent.Time, header.Time, state, false)

	if err := p.checkNanoBlackList(state, header); err != nil {
		return nil, nil, err
	}

	if p.chainConfig.IsOnFeynman(header.Number, parent.Time, header.Time) {
		err := p.initializeFeynmanContract(state, header, cx, &body.Transactions, &receipts, nil, &header.GasUsed, true, tracer)
		if err != nil {
			log.Error("init feynman contract failed", "error", err)
		}
	}

	if header.Number.Cmp(common.Big1) == 0 {
		err := p.initContract(state, header, cx, &body.Transactions, &receipts, nil, &header.GasUsed, true, tracer)
		if err != nil {
			log.Error("init contract failed")
		}
	}
	if header.Difficulty.Cmp(diffInTurn) != 0 {
		number := header.Number.Uint64()
		snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
		if err != nil {
			return nil, nil, err
		}
		spoiledVal := snap.inturnValidator()
		signedRecently := false
		if p.chainConfig.IsPlato(header.Number) {
			signedRecently = snap.SignRecently(spoiledVal)
		} else {
			for _, recent := range snap.Recents {
				if recent == spoiledVal {
					signedRecently = true
					break
				}
			}
		}
		if !signedRecently {
			err = p.slash(spoiledVal, state, header, cx, &body.Transactions, &receipts, nil, &header.GasUsed, true, tracer)
			if err != nil {
				// it is possible that slash validator failed because of the slash channel is disabled.
				log.Error("slash validator failed", "block hash", header.Hash(), "address", spoiledVal)
			}
		}
	}

	err := p.distributeIncoming(p.val, state, header, cx, &body.Transactions, &receipts, nil, &header.GasUsed, true, tracer)
	if err != nil {
		return nil, nil, err
	}

	if p.chainConfig.IsPlato(header.Number) {
		if err := p.distributeFinalityReward(chain, state, header, cx, &body.Transactions, &receipts, nil, &header.GasUsed, true, tracer); err != nil {
			return nil, nil, err
		}
	}

	// update validators every day
	if p.chainConfig.IsFeynman(header.Number, header.Time) && isBreatheBlock(parent.Time, header.Time) {
		// we should avoid update validators in the Feynman upgrade block
		if !p.chainConfig.IsOnFeynman(header.Number, parent.Time, header.Time) {
			if err := p.updateValidatorSetV2(state, header, cx, &body.Transactions, &receipts, nil, &header.GasUsed, true, tracer); err != nil {
				return nil, nil, err
			}
		}
	}

	// should not happen. Once happen, stop the node is better than broadcast the block
	if header.GasLimit < header.GasUsed {
		return nil, nil, errors.New("gas consumption of system txs exceed the gas limit")
	}
	header.UncleHash = types.EmptyUncleHash
	var blk *types.Block
	var rootHash common.Hash
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		rootHash = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
		wg.Done()
	}()
	go func() {
		blk = types.NewBlock(header, body, receipts, trie.NewStackTrie(nil))
		wg.Done()
	}()
	wg.Wait()
	blk.SetRoot(rootHash)
	// Assemble and return the final block for sealing
	return blk, receipts, nil
}

func (p *Parlia) IsActiveValidatorAt(chain consensus.ChainHeaderReader, header *types.Header, checkVoteKeyFn func(bLSPublicKey *types.BLSPublicKey) bool) bool {
	number := header.Number.Uint64()
	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		log.Error("failed to get the snapshot from consensus", "error", err)
		return false
	}
	validators := snap.Validators
	validatorInfo, ok := validators[p.val]

	return ok && (checkVoteKeyFn == nil || (validatorInfo != nil && checkVoteKeyFn(&validatorInfo.VoteAddress)))
}

// VerifyVote will verify: 1. If the vote comes from valid validators 2. If the vote's sourceNumber and sourceHash are correct
func (p *Parlia) VerifyVote(chain consensus.ChainHeaderReader, vote *types.VoteEnvelope) error {
	targetNumber := vote.Data.TargetNumber
	targetHash := vote.Data.TargetHash
	header := chain.GetVerifiedBlockByHash(targetHash)
	if header == nil {
		log.Warn("BlockHeader at current voteBlockNumber is nil", "targetNumber", targetNumber, "targetHash", targetHash)
		return errors.New("BlockHeader at current voteBlockNumber is nil")
	}
	if header.Number.Uint64() != targetNumber {
		log.Warn("unexpected target number", "expect", header.Number.Uint64(), "real", targetNumber)
		return errors.New("target number mismatch")
	}

	justifiedBlockNumber, justifiedBlockHash, err := p.GetJustifiedNumberAndHash(chain, []*types.Header{header})
	if err != nil {
		log.Error("failed to get the highest justified number and hash", "headerNumber", header.Number, "headerHash", header.Hash())
		return errors.New("unexpected error when getting the highest justified number and hash")
	}
	if vote.Data.SourceNumber != justifiedBlockNumber || vote.Data.SourceHash != justifiedBlockHash {
		return errors.New("vote source block mismatch")
	}

	number := header.Number.Uint64()
	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		log.Error("failed to get the snapshot from consensus", "error", err)
		return errors.New("failed to get the snapshot from consensus")
	}

	validators := snap.Validators
	voteAddress := vote.VoteAddress
	for addr, validator := range validators {
		if validator.VoteAddress == voteAddress {
			if addr == p.val {
				validVotesfromSelfCounter.Inc(1)
			}
			metrics.GetOrRegisterCounter(fmt.Sprintf("parlia/VerifyVote/%s", addr.String()), nil).Inc(1)
			return nil
		}
	}

	return errors.New("vote verification failed")
}

// Authorize injects a private key into the consensus engine to mint new blocks
// with.
func (p *Parlia) Authorize(val common.Address, signFn SignerFn, signTxFn SignerTxFn) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.val = val
	p.signFn = signFn
	p.signTxFn = signTxFn
}

// Argument leftOver is the time reserved for block finalize(calculate root, distribute income...)
func (p *Parlia) Delay(chain consensus.ChainReader, header *types.Header, leftOver *time.Duration) *time.Duration {
	number := header.Number.Uint64()
	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		return nil
	}
	delay := p.delayForRamanujanFork(snap, header)

	if *leftOver >= time.Duration(snap.BlockInterval)*time.Millisecond {
		// ignore invalid leftOver
		log.Error("Delay invalid argument", "leftOver", leftOver.String(), "Period", snap.BlockInterval)
	} else if *leftOver >= delay {
		delay = time.Duration(0)
		return &delay
	} else {
		delay = delay - *leftOver
	}

	// The blocking time should be no more than half of period when snap.TurnLength == 1
	timeForMining := time.Duration(snap.BlockInterval) * time.Millisecond / 2
	if !snap.lastBlockInOneTurn(header.Number.Uint64()) {
		timeForMining = time.Duration(snap.BlockInterval) * time.Millisecond * 4 / 5
	}
	if delay > timeForMining {
		delay = timeForMining
	}
	return &delay
}

// Seal implements consensus.Engine, attempting to create a sealed block using
// the local signing credentials.
func (p *Parlia) Seal(chain consensus.ChainHeaderReader, block *types.Block, results chan<- *types.Block, stop <-chan struct{}) error {
	header := block.Header()

	// Sealing the genesis block is not supported
	number := header.Number.Uint64()
	if number == 0 {
		return errUnknownBlock
	}
	// Don't hold the val fields for the entire sealing procedure
	p.lock.RLock()
	val, signFn := p.val, p.signFn
	p.lock.RUnlock()

	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		return err
	}

	// Bail out if we're unauthorized to sign a block
	if _, authorized := snap.Validators[val]; !authorized {
		return errUnauthorizedValidator(val.String())
	}

	// If we're amongst the recent signers, wait for the next block
	if snap.SignRecently(val) {
		log.Info("Signed recently, must wait for others")
		return nil
	}

	// Sweet, the protocol permits us to sign the block, wait for our time
	delay := p.delayForRamanujanFork(snap, header)

	log.Info("Sealing block with", "number", number, "delay", delay, "headerDifficulty", header.Difficulty, "val", val.Hex())

	// Wait until sealing is terminated or delay timeout.
	log.Trace("Waiting for slot to sign and propagate", "delay", common.PrettyDuration(delay))
	go func() {
		select {
		case <-stop:
			return
		case <-time.After(delay):
		}

		err := p.assembleVoteAttestation(chain, header)
		if err != nil {
			/* If the vote attestation can't be assembled successfully, the blockchain won't get
			   fast finalized, but it can be tolerated, so just report this error here. */
			log.Error("Assemble vote attestation failed when sealing", "err", err)
		}

		// Sign all the things!
		sig, err := signFn(accounts.Account{Address: val}, accounts.MimetypeParlia, ParliaRLP(header, p.chainConfig.ChainID))
		if err != nil {
			log.Error("Sign for the block header failed when sealing", "err", err)
			return
		}
		copy(header.Extra[len(header.Extra)-extraSeal:], sig)

		if p.shouldWaitForCurrentBlockProcess(chain, header, snap) {
			highestVerifiedHeader := chain.GetHighestVerifiedHeader()
			// including time for writing and committing blocks
			waitProcessEstimate := math.Ceil(float64(highestVerifiedHeader.GasUsed) / float64(100_000_000))
			log.Info("Waiting for received in turn block to process", "waitProcessEstimate(Seconds)", waitProcessEstimate)
			select {
			case <-stop:
				log.Info("Received block process finished, abort block seal")
				return
			case <-time.After(time.Duration(waitProcessEstimate) * time.Second):
				if chain.CurrentHeader().Number.Uint64() >= header.Number.Uint64() {
					log.Info("Process backoff time exhausted, and current header has updated to abort this seal")
					return
				}
				log.Info("Process backoff time exhausted, start to seal block")
			}
		}

		select {
		case results <- block.WithSeal(header):
		default:
			log.Warn("Sealing result is not read by miner", "sealhash", types.SealHash(header, p.chainConfig.ChainID))
		}
	}()

	return nil
}

func (p *Parlia) shouldWaitForCurrentBlockProcess(chain consensus.ChainHeaderReader, header *types.Header, snap *Snapshot) bool {
	if header.Difficulty.Cmp(diffInTurn) == 0 {
		return false
	}

	highestVerifiedHeader := chain.GetHighestVerifiedHeader()
	if highestVerifiedHeader == nil {
		return false
	}

	if header.ParentHash == highestVerifiedHeader.ParentHash {
		return true
	}
	return false
}

func (p *Parlia) EnoughDistance(chain consensus.ChainReader, header *types.Header) bool {
	snap, err := p.snapshot(chain, header.Number.Uint64()-1, header.ParentHash, nil)
	if err != nil {
		return true
	}
	return snap.enoughDistance(p.val, header)
}

func (p *Parlia) IsLocalBlock(header *types.Header) bool {
	return p.val == header.Coinbase
}

func (p *Parlia) SignRecently(chain consensus.ChainReader, parent *types.Header) (bool, error) {
	snap, err := p.snapshot(chain, parent.Number.Uint64(), parent.Hash(), nil)
	if err != nil {
		return true, err
	}

	// Bail out if we're unauthorized to sign a block
	if _, authorized := snap.Validators[p.val]; !authorized {
		return true, errUnauthorizedValidator(p.val.String())
	}

	return snap.SignRecently(p.val), nil
}

// CalcDifficulty is the difficulty adjustment algorithm. It returns the difficulty
// that a new block should have based on the previous blocks in the chain and the
// current signer.
func (p *Parlia) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	snap, err := p.snapshot(chain, parent.Number.Uint64(), parent.Hash(), nil)
	if err != nil {
		return nil
	}
	return CalcDifficulty(snap, p.val)
}

// CalcDifficulty is the difficulty adjustment algorithm. It returns the difficulty
// that a new block should have based on the previous blocks in the chain and the
// current signer.
func CalcDifficulty(snap *Snapshot, signer common.Address) *big.Int {
	if snap.inturn(signer) {
		return new(big.Int).Set(diffInTurn)
	}
	return new(big.Int).Set(diffNoTurn)
}

func encodeSigHeaderWithoutVoteAttestation(w io.Writer, header *types.Header, chainId *big.Int) {
	err := rlp.Encode(w, []interface{}{
		chainId,
		header.ParentHash,
		header.UncleHash,
		header.Coinbase,
		header.Root,
		header.TxHash,
		header.ReceiptHash,
		header.Bloom,
		header.Difficulty,
		header.Number,
		header.GasLimit,
		header.GasUsed,
		header.Time,
		header.Extra[:extraVanity], // this will panic if extra is too short, should check before calling encodeSigHeaderWithoutVoteAttestation
		header.MixDigest,
		header.Nonce,
	})
	if err != nil {
		panic("can't encode: " + err.Error())
	}
}

// SealHash returns the hash of a block without vote attestation prior to it being sealed.
// So it's not the real hash of a block, just used as unique id to distinguish task
func (p *Parlia) SealHash(header *types.Header) (hash common.Hash) {
	hasher := sha3.NewLegacyKeccak256()
	encodeSigHeaderWithoutVoteAttestation(hasher, header, p.chainConfig.ChainID)
	hasher.Sum(hash[:0])
	return hash
}

// APIs implements consensus.Engine, returning the user facing RPC API to query snapshot.
func (p *Parlia) APIs(chain consensus.ChainHeaderReader) []rpc.API {
	return []rpc.API{{
		Namespace: "parlia",
		Version:   "1.0",
		Service:   &API{chain: chain, parlia: p},
		Public:    false,
	}}
}

// Close implements consensus.Engine. It's a noop for parlia as there are no background threads.
func (p *Parlia) Close() error {
	return nil
}

// ==========================  interaction with contract/account =========

// getCurrentValidators get current validators
func (p *Parlia) getCurrentValidators(blockHash common.Hash, blockNum *big.Int) ([]common.Address, map[common.Address]*types.BLSPublicKey, error) {
	// block
	blockNr := rpc.BlockNumberOrHashWithHash(blockHash, false)

	if !p.chainConfig.IsLuban(blockNum) {
		validators, err := p.getCurrentValidatorsBeforeLuban(blockHash, blockNum)
		return validators, nil, err
	}

	// method
	method := "getMiningValidators"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // cancel when we are finished consuming integers

	data, err := p.validatorSetABI.Pack(method)
	if err != nil {
		log.Error("Unable to pack tx for getMiningValidators", "error", err)
		return nil, nil, err
	}
	// call
	msgData := (hexutil.Bytes)(data)
	toAddress := common.HexToAddress(systemcontracts.ValidatorContract)
	gas := (hexutil.Uint64)(uint64(math.MaxUint64 / 2))
	result, err := p.ethAPI.Call(ctx, ethapi.TransactionArgs{
		Gas:  &gas,
		To:   &toAddress,
		Data: &msgData,
	}, &blockNr, nil, nil)
	if err != nil {
		return nil, nil, err
	}

	var valSet []common.Address
	var voteAddrSet []types.BLSPublicKey
	if err := p.validatorSetABI.UnpackIntoInterface(&[]interface{}{&valSet, &voteAddrSet}, method, result); err != nil {
		return nil, nil, err
	}

	voteAddrMap := make(map[common.Address]*types.BLSPublicKey, len(valSet))
	for i := 0; i < len(valSet); i++ {
		voteAddrMap[valSet[i]] = &(voteAddrSet)[i]
	}
	return valSet, voteAddrMap, nil
}

func (p *Parlia) isIntentionalDelayMining(chain consensus.ChainHeaderReader, header *types.Header) (bool, error) {
	parent := chain.GetHeader(header.ParentHash, header.Number.Uint64()-1)
	if parent == nil {
		return false, errors.New("parent not found")
	}
	blockInterval, err := p.BlockInterval(chain, header)
	if err != nil {
		return false, err
	}
	isIntentional := header.Coinbase == parent.Coinbase &&
		header.Difficulty == diffInTurn && parent.Difficulty == diffInTurn &&
		parent.MilliTimestamp()+blockInterval < header.MilliTimestamp()
	return isIntentional, nil
}

// distributeIncoming distributes system incoming of the block
func (p *Parlia) distributeIncoming(val common.Address, state vm.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool, tracer *tracing.Hooks) error {
	coinbase := header.Coinbase

	doDistributeSysReward := !p.chainConfig.IsKepler(header.Number, header.Time) &&
		state.GetBalance(common.HexToAddress(systemcontracts.SystemRewardContract)).Cmp(maxSystemBalance) < 0
	if doDistributeSysReward {
		balance := state.GetBalance(consensus.SystemAddress)
		rewards := new(uint256.Int)
		rewards = rewards.Rsh(balance, systemRewardPercent)
		if rewards.Cmp(common.U2560) > 0 {
			state.SetBalance(consensus.SystemAddress, balance.Sub(balance, rewards), tracing.BalanceChangeUnspecified)
			state.AddBalance(coinbase, rewards, tracing.BalanceChangeUnspecified)
			err := p.distributeToSystem(rewards.ToBig(), state, header, chain, txs, receipts, receivedTxs, usedGas, mining, tracer)
			if err != nil {
				return err
			}
			log.Trace("distribute to system reward pool", "block hash", header.Hash(), "amount", rewards)
		}
	}

	balance := state.GetBalance(consensus.SystemAddress)
	if balance.Cmp(common.U2560) <= 0 {
		return nil
	}

	state.SetBalance(consensus.SystemAddress, common.U2560, tracing.BalanceDecreaseBSCDistributeReward)
	state.AddBalance(coinbase, balance, tracing.BalanceIncreaseBSCDistributeReward)
	log.Trace("distribute to validator contract", "block hash", header.Hash(), "amount", balance)
	return p.distributeToValidator(balance.ToBig(), val, state, header, chain, txs, receipts, receivedTxs, usedGas, mining, tracer)
}

// slash spoiled validators
func (p *Parlia) slash(spoiledVal common.Address, state vm.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool, tracer *tracing.Hooks) error {
	// method
	method := "slash"

	// get packed data
	data, err := p.slashABI.Pack(method,
		spoiledVal,
	)
	if err != nil {
		log.Error("Unable to pack tx for slash", "error", err)
		return err
	}
	// get system message
	msg := p.getSystemMessage(header.Coinbase, common.HexToAddress(systemcontracts.SlashContract), data, common.Big0)
	// apply message
	return p.applyTransaction(msg, state, header, chain, txs, receipts, receivedTxs, usedGas, mining, tracer)
}

// init contract
func (p *Parlia) initContract(state vm.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool, tracer *tracing.Hooks) error {
	// method
	method := "init"
	// contracts
	contracts := []string{
		systemcontracts.ValidatorContract,
		systemcontracts.SlashContract,
		systemcontracts.LightClientContract,
		systemcontracts.RelayerHubContract,
		systemcontracts.TokenHubContract,
		systemcontracts.RelayerIncentivizeContract,
		systemcontracts.CrossChainContract,
	}
	// get packed data
	data, err := p.validatorSetABI.Pack(method)
	if err != nil {
		log.Error("Unable to pack tx for init validator set", "error", err)
		return err
	}
	for _, c := range contracts {
		msg := p.getSystemMessage(header.Coinbase, common.HexToAddress(c), data, common.Big0)
		// apply message
		log.Trace("init contract", "block hash", header.Hash(), "contract", c)
		err = p.applyTransaction(msg, state, header, chain, txs, receipts, receivedTxs, usedGas, mining, tracer)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Parlia) distributeToSystem(amount *big.Int, state vm.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool, tracer *tracing.Hooks) error {
	// get system message
	msg := p.getSystemMessage(header.Coinbase, common.HexToAddress(systemcontracts.SystemRewardContract), nil, amount)
	// apply message
	return p.applyTransaction(msg, state, header, chain, txs, receipts, receivedTxs, usedGas, mining, tracer)
}

// distributeToValidator deposits validator reward to validator contract
func (p *Parlia) distributeToValidator(amount *big.Int, validator common.Address,
	state vm.StateDB, header *types.Header, chain core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt, receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool, tracer *tracing.Hooks) error {
	// method
	method := "deposit"

	// get packed data
	data, err := p.validatorSetABI.Pack(method,
		validator,
	)
	if err != nil {
		log.Error("Unable to pack tx for deposit", "error", err)
		return err
	}
	// get system message
	msg := p.getSystemMessage(header.Coinbase, common.HexToAddress(systemcontracts.ValidatorContract), data, amount)
	// apply message
	return p.applyTransaction(msg, state, header, chain, txs, receipts, receivedTxs, usedGas, mining, tracer)
}

// get system message
func (p *Parlia) getSystemMessage(from, toAddress common.Address, data []byte, value *big.Int) *core.Message {
	return &core.Message{
		From:     from,
		GasLimit: math.MaxUint64 / 2,
		GasPrice: big.NewInt(0),
		Value:    value,
		To:       &toAddress,
		Data:     data,
	}
}

func (p *Parlia) applyTransaction(
	msg *core.Message,
	state vm.StateDB,
	header *types.Header,
	chainContext core.ChainContext,
	txs *[]*types.Transaction, receipts *[]*types.Receipt,
	receivedTxs *[]*types.Transaction, usedGas *uint64, mining bool,
	tracer *tracing.Hooks,
) (applyErr error) {
	nonce := state.GetNonce(msg.From)
	expectedTx := types.NewTransaction(nonce, *msg.To, msg.Value, msg.GasLimit, msg.GasPrice, msg.Data)
	expectedHash := p.signer.Hash(expectedTx)

	if msg.From == p.val && mining {
		var err error
		expectedTx, err = p.signTxFn(accounts.Account{Address: msg.From}, expectedTx, p.chainConfig.ChainID)
		if err != nil {
			return err
		}
	} else {
		if receivedTxs == nil || len(*receivedTxs) == 0 || (*receivedTxs)[0] == nil {
			return errors.New("supposed to get a actual transaction, but get none")
		}
		actualTx := (*receivedTxs)[0]
		if !bytes.Equal(p.signer.Hash(actualTx).Bytes(), expectedHash.Bytes()) {
			return fmt.Errorf("expected tx hash %v, get %v, nonce %d, to %s, value %s, gas %d, gasPrice %s, data %s", expectedHash.String(), actualTx.Hash().String(),
				expectedTx.Nonce(),
				expectedTx.To().String(),
				expectedTx.Value().String(),
				expectedTx.Gas(),
				expectedTx.GasPrice().String(),
				hex.EncodeToString(expectedTx.Data()),
			)
		}
		expectedTx = actualTx
		// move to next
		*receivedTxs = (*receivedTxs)[1:]
	}
	state.SetTxContext(expectedTx.Hash(), len(*txs))

	// Create a new context to be used in the EVM environment
	context := core.NewEVMBlockContext(header, chainContext, nil)
	// Create a new environment which holds all relevant information
	// about the transaction and calling mechanisms.
	evm := vm.NewEVM(context, state, p.chainConfig, vm.Config{Tracer: tracer})
	evm.SetTxContext(core.NewEVMTxContext(msg))

	// Tracing receipt will be set if there is no error and will be used to trace the transaction
	var tracingReceipt *types.Receipt
	if tracer != nil {
		if tracer.OnSystemTxStart != nil {
			tracer.OnSystemTxStart()
		}
		if tracer.OnTxStart != nil {
			tracer.OnTxStart(evm.GetVMContext(), expectedTx, msg.From)
		}

		// Defers are last in first out, so OnTxEnd will run before OnSystemTxEnd in this transaction,
		// which is what we want.
		if tracer.OnSystemTxEnd != nil {
			defer func() {
				tracer.OnSystemTxEnd()
			}()
		}
		if tracer.OnTxEnd != nil {
			defer func() {
				tracer.OnTxEnd(tracingReceipt, applyErr)
			}()
		}
	}

	gasUsed, err := applyMessage(msg, evm, state, header, p.chainConfig, chainContext)
	if err != nil {
		return err
	}
	*txs = append(*txs, expectedTx)
	var root []byte
	if p.chainConfig.IsByzantium(header.Number) {
		state.Finalise(true)
	} else {
		root = state.IntermediateRoot(p.chainConfig.IsEIP158(header.Number)).Bytes()
	}
	*usedGas += gasUsed
	tracingReceipt = types.NewReceipt(root, false, *usedGas)
	tracingReceipt.TxHash = expectedTx.Hash()
	tracingReceipt.GasUsed = gasUsed

	// Set the receipt logs and create a bloom for filtering
	tracingReceipt.Logs = state.GetLogs(expectedTx.Hash(), header.Number.Uint64(), header.Hash(), header.Time)
	tracingReceipt.Bloom = types.CreateBloom(tracingReceipt)
	tracingReceipt.BlockHash = header.Hash()
	tracingReceipt.BlockNumber = header.Number
	tracingReceipt.TransactionIndex = uint(state.TxIndex())
	*receipts = append(*receipts, tracingReceipt)
	return nil
}

// GetJustifiedNumberAndHash retrieves the number and hash of the highest justified block
// within the branch including `headers` and utilizing the latest element as the head.
func (p *Parlia) GetJustifiedNumberAndHash(chain consensus.ChainHeaderReader, headers []*types.Header) (uint64, common.Hash, error) {
	if chain == nil || len(headers) == 0 || headers[len(headers)-1] == nil {
		return 0, common.Hash{}, errors.New("illegal chain or header")
	}
	head := headers[len(headers)-1]
	snap, err := p.snapshot(chain, head.Number.Uint64(), head.Hash(), headers)
	if err != nil {
		log.Error("Unexpected error when getting snapshot",
			"error", err, "blockNumber", head.Number.Uint64(), "blockHash", head.Hash())
		return 0, common.Hash{}, err
	}

	if snap.Attestation == nil {
		if p.chainConfig.IsLuban(head.Number) {
			log.Debug("once one attestation generated, attestation of snap would not be nil forever basically")
		}
		return 0, chain.GetHeaderByNumber(0).Hash(), nil
	}
	return snap.Attestation.TargetNumber, snap.Attestation.TargetHash, nil
}

// GetFinalizedHeader returns highest finalized block header.
func (p *Parlia) GetFinalizedHeader(chain consensus.ChainHeaderReader, header *types.Header) *types.Header {
	if chain == nil || header == nil {
		return nil
	}
	if !chain.Config().IsPlato(header.Number) {
		return chain.GetHeaderByNumber(0)
	}

	snap, err := p.snapshot(chain, header.Number.Uint64(), header.Hash(), nil)
	if err != nil {
		log.Error("Unexpected error when getting snapshot",
			"error", err, "blockNumber", header.Number.Uint64(), "blockHash", header.Hash())
		return nil
	}

	if snap.Attestation == nil {
		return chain.GetHeaderByNumber(0) // keep consistent with GetJustifiedNumberAndHash
	}

	return chain.GetHeader(snap.Attestation.SourceHash, snap.Attestation.SourceNumber)
}

// ===========================     utility function        ==========================
func (p *Parlia) backOffTime(snap *Snapshot, parent, header *types.Header, val common.Address) uint64 {
	if snap.inturn(val) {
		log.Debug("backOffTime", "blockNumber", header.Number, "in turn validator", val)
		return 0
	} else {
		delay := defaultInitialBackOffTime
		// When mining blocks, `header.Time` is temporarily set to time.Now() + 1.
		// Therefore, using `header.Time` to determine whether a hard fork has occurred is incorrect.
		// As a result, during the Bohr and Lorentz hard forks, the network may experience some instability,
		// So use `parent.Time` instead.
		isParerntLorentz := p.chainConfig.IsLorentz(parent.Number, parent.Time)
		if isParerntLorentz {
			// If the in-turn validator has not signed recently, the expected backoff times are [2, 3, 4, ...].
			delay = lorentzInitialBackOffTime
		}
		validators := snap.validators()
		if p.chainConfig.IsPlanck(header.Number) {
			counts := snap.countRecents()
			for addr, seenTimes := range counts {
				log.Trace("backOffTime", "blockNumber", header.Number, "validator", addr, "seenTimes", seenTimes)
			}

			// The backOffTime does not matter when a validator has signed recently.
			if snap.signRecentlyByCounts(val, counts) {
				return 0
			}

			inTurnAddr := snap.inturnValidator()
			if snap.signRecentlyByCounts(inTurnAddr, counts) {
				log.Debug("in turn validator has recently signed, skip initialBackOffTime",
					"inTurnAddr", inTurnAddr)
				delay = 0
			}

			// Exclude the recently signed validators and the in turn validator
			temp := make([]common.Address, 0, len(validators))
			for _, addr := range validators {
				if snap.signRecentlyByCounts(addr, counts) {
					continue
				}
				if p.chainConfig.IsBohr(header.Number, header.Time) {
					if addr == inTurnAddr {
						continue
					}
				}
				temp = append(temp, addr)
			}
			validators = temp
		}

		// get the index of current validator and its shuffled backoff time.
		idx := -1
		for index, itemAddr := range validators {
			if val == itemAddr {
				idx = index
			}
		}
		if idx < 0 {
			log.Debug("The validator is not authorized", "addr", val)
			return 0
		}

		randSeed := snap.Number
		if p.chainConfig.IsBohr(header.Number, header.Time) {
			randSeed = header.Number.Uint64() / uint64(snap.TurnLength)
		}
		s := rand.NewSource(int64(randSeed))
		r := rand.New(s)
		n := len(validators)
		backOffSteps := make([]uint64, 0, n)

		for i := uint64(0); i < uint64(n); i++ {
			backOffSteps = append(backOffSteps, i)
		}

		r.Shuffle(n, func(i, j int) {
			backOffSteps[i], backOffSteps[j] = backOffSteps[j], backOffSteps[i]
		})

		if delay == 0 && isParerntLorentz {
			// If the in-turn validator has signed recently, the expected backoff times are [0, 2, 3, ...].
			if backOffSteps[idx] == 0 {
				return 0
			}
			return lorentzInitialBackOffTime + (backOffSteps[idx]-1)*wiggleTime
		}
		delay += backOffSteps[idx] * wiggleTime
		return delay
	}
}

// BlockInterval returns number of blocks in one epoch for the given header
func (p *Parlia) epochLength(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header) (uint64, error) {
	if header == nil {
		return defaultEpochLength, errUnknownBlock
	}
	if header.Number.Uint64() == 0 {
		return defaultEpochLength, nil
	}
	snap, err := p.snapshot(chain, header.Number.Uint64()-1, header.ParentHash, parents)
	if err != nil {
		return defaultEpochLength, err
	}
	return snap.EpochLength, nil
}

// BlockInterval returns the block interval in milliseconds for the given header
func (p *Parlia) BlockInterval(chain consensus.ChainHeaderReader, header *types.Header) (uint64, error) {
	if header == nil {
		return defaultBlockInterval, errUnknownBlock
	}
	if header.Number.Uint64() == 0 {
		return defaultBlockInterval, nil
	}
	snap, err := p.snapshot(chain, header.Number.Uint64()-1, header.ParentHash, nil)
	if err != nil {
		return defaultBlockInterval, err
	}
	return snap.BlockInterval, nil
}

func (p *Parlia) NextProposalBlock(chain consensus.ChainHeaderReader, header *types.Header, proposer common.Address) (uint64, uint64, error) {
	snap, err := p.snapshot(chain, header.Number.Uint64(), header.Hash(), nil)
	if err != nil {
		return 0, 0, err
	}

	return snap.nextProposalBlock(proposer)
}

func (p *Parlia) checkNanoBlackList(state vm.StateDB, header *types.Header) error {
	if p.chainConfig.IsNano(header.Number) {
		for _, blackListAddr := range types.NanoBlackList {
			if state.IsAddressInMutations(blackListAddr) {
				log.Error("blacklisted address found", "address", blackListAddr)
				return fmt.Errorf("block contains blacklisted address: %s", blackListAddr.Hex())
			}
		}
	}
	return nil
}

func (p *Parlia) detectNewVersionWithFork(chain consensus.ChainHeaderReader, header *types.Header, state vm.StateDB) {
	// Ignore blocks that are considered too old
	const maxBlockReceiveDelay = 10 * time.Second
	blockTime := time.UnixMilli(int64(header.MilliTimestamp()))
	if time.Since(blockTime) > maxBlockReceiveDelay {
		return
	}

	// If the fork is not a majority, log a warning or debug message
	number := header.Number.Uint64()
	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		return
	}
	nextForkHash := forkid.NextForkHash(p.chainConfig, p.genesisHash, chain.GenesisHeader().Time, number, header.Time)
	forkHashHex := hex.EncodeToString(nextForkHash[:])
	if !snap.isMajorityFork(forkHashHex) {
		logFn := log.Debug
		if state.NoTrie() {
			logFn = log.Warn
		}
		logFn("possible fork detected: client is not in majority", "nextForkHash", forkHashHex)
	}
}

// chain context
type chainContext struct {
	Chain  consensus.ChainHeaderReader
	parlia consensus.Engine
}

func (c chainContext) Engine() consensus.Engine {
	return c.parlia
}

func (c chainContext) GetHeader(hash common.Hash, number uint64) *types.Header {
	return c.Chain.GetHeader(hash, number)
}

func (c chainContext) Config() *params.ChainConfig {
	return c.Chain.Config()
}

// apply message
func applyMessage(
	msg *core.Message,
	evm *vm.EVM,
	state vm.StateDB,
	header *types.Header,
	chainConfig *params.ChainConfig,
	chainContext core.ChainContext,
) (uint64, error) {
	// Apply the transaction to the current state (included in the env)
	if chainConfig.IsCancun(header.Number, header.Time) {
		rules := evm.ChainConfig().Rules(evm.Context.BlockNumber, evm.Context.Random != nil, evm.Context.Time)
		state.Prepare(rules, msg.From, evm.Context.Coinbase, msg.To, vm.ActivePrecompiles(rules), msg.AccessList)
	} else {
		state.ClearAccessList()
	}
	// Increment the nonce for the next transaction
	state.SetNonce(msg.From, state.GetNonce(msg.From)+1, tracing.NonceChangeEoACall)

	ret, returnGas, err := evm.Call(
		msg.From,
		*msg.To,
		msg.Data,
		msg.GasLimit,
		uint256.MustFromBig(msg.Value),
	)
	if err != nil {
		log.Error("apply message failed", "msg", string(ret), "err", err)
	}
	return msg.GasLimit - returnGas, err
}

// proposalKey build a key which is a combination of the block number and the proposer address.
func proposalKey(header types.Header) string {
	return header.ParentHash.String() + header.Coinbase.String()
}
