package parlia

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/systemcontracts"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
)

// IsUnsignedSystemTxCandidate reports whether tx looks like an unsigned
// BidBlock system tx. It does not recover the sender.
func (p *Parlia) IsUnsignedSystemTxCandidate(tx *types.Transaction) bool {
	if tx == nil || tx.To() == nil || !isToSystemContract(*tx.To()) {
		return false
	}
	if tx.GasPrice() == nil || tx.GasPrice().Sign() != 0 {
		return false
	}
	v, r, s := tx.RawSignatureValues()
	return isZeroSig(v, r, s)
}

func isZeroSig(v, r, s *big.Int) bool {
	isZero := func(x *big.Int) bool { return x == nil || x.Sign() == 0 }
	return isZero(v) && isZero(r) && isZero(s)
}

var signableSystemTxMethods = []string{
	"deposit",
	"distributeFinalityReward",
	"updateValidatorSetV2",
}

// IsSignableSystemTx reports whether tx can be bind-signed for BidBlock.
func (p *Parlia) IsSignableSystemTx(tx *types.Transaction) bool {
	if !p.IsUnsignedSystemTxCandidate(tx) {
		return false
	}
	if *tx.To() != common.HexToAddress(systemcontracts.ValidatorContract) {
		return false
	}
	return p.hasSignableSelector(tx.Data())
}

func (p *Parlia) hasSignableSelector(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	selector := data[:4]
	for _, name := range signableSystemTxMethods {
		method, ok := p.validatorSetABI.Methods[name]
		if !ok {
			continue
		}
		if bytes.Equal(selector, method.ID) {
			return true
		}
	}
	return false
}

type expectedSystemTxEntry struct {
	method   string
	selector []byte
}

// ExpectedSystemTxShape returns the expected trailing system-tx order for accepted BidBlocks:
//
//	deposit (if GasFee > 0) -> distributeFinalityReward (cond.) -> updateValidatorSetV2 (cond.)
func (p *Parlia) ExpectedSystemTxShape(header, parent *types.Header, gasFee *big.Int) []expectedSystemTxEntry {
	shape := make([]expectedSystemTxEntry, 0, 3)

	if gasFee != nil && gasFee.Sign() > 0 {
		shape = append(shape, expectedSystemTxEntry{
			method:   "deposit",
			selector: p.selectorFor("deposit"),
		})
	}

	if header.Number.Uint64()%finalityRewardInterval == 0 {
		shape = append(shape, expectedSystemTxEntry{
			method:   "distributeFinalityReward",
			selector: p.selectorFor("distributeFinalityReward"),
		})
	}

	if isBreatheBlock(parent.Time, header.Time) {
		shape = append(shape, expectedSystemTxEntry{
			method:   "updateValidatorSetV2",
			selector: p.selectorFor("updateValidatorSetV2"),
		})
	}

	return shape
}

func (p *Parlia) VerifySystemTxShape(txs []*types.Transaction, shape []expectedSystemTxEntry) error {
	if len(txs) < len(shape) {
		return fmt.Errorf("missing required system tx %q", shape[len(txs)].method)
	}
	if len(txs) > len(shape) {
		return fmt.Errorf("unexpected extra system tx at position %d (selector 0x%x)",
			len(shape), txSelector(txs[len(shape)]))
	}
	for i, exp := range shape {
		if !bytes.HasPrefix(txs[i].Data(), exp.selector) {
			return fmt.Errorf("expected system tx %q at position %d, got selector 0x%x",
				exp.method, i, txSelector(txs[i]))
		}
	}
	return nil
}

func txSelector(tx *types.Transaction) []byte {
	data := tx.Data()
	if len(data) < 4 {
		return data
	}
	return data[:4]
}

func (p *Parlia) selectorFor(methodName string) []byte {
	method, ok := p.validatorSetABI.Methods[methodName]
	if !ok {
		return nil
	}
	return method.ID
}

// ExtractDistributedGasFee reads the GasFee value from the trailing
// ValidatorContract.deposit system tx. Call only after InsertChain succeeds.
func (p *Parlia) ExtractDistributedGasFee(block *types.Block) *big.Int {
	txs := block.Transactions()
	actual := new(big.Int)

	depositSel := p.selectorFor("deposit")
	valContract := common.HexToAddress(systemcontracts.ValidatorContract)

	for i := len(txs) - 1; i >= 0; i-- {
		tx := txs[i]
		isSystem, err := p.IsSystemTransaction(tx, block.Header())
		if err != nil || !isSystem {
			return actual
		}

		to := tx.To()
		if to != nil &&
			*to == valContract &&
			len(depositSel) == 4 &&
			len(tx.Data()) >= 4 &&
			bytes.Equal(tx.Data()[:4], depositSel) {
			return tx.Value()
		}
	}
	return actual
}

// PrepareForBidBlock prepares consensus header fields for BidBlock construction.
// It mirrors Prepare, but uses the in-turn validator as Coinbase instead of p.val.
func (p *Parlia) PrepareForBidBlock(chain consensus.ChainHeaderReader, header *types.Header) error {
	header.Nonce = types.BlockNonce{}

	number := header.Number.Uint64()
	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		return err
	}
	validator := snap.inturnValidator()
	header.Coinbase = validator
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	blockTime := p.blockTimeForBidBlock(snap, header, parent)

	return p.prepareHeader(chain, header, snap, validator, number, blockTime)
}

func (p *Parlia) blockTimeForBidBlock(snap *Snapshot, header, parent *types.Header) uint64 {
	return parent.MilliTimestamp() + snap.BlockInterval +
		p.backOffTime(snap, parent, header, header.Coinbase)
}

// FinalizeAndAssembleBidBlock assembles a BidBlock with unsigned system txs
// and returns actualGasFee.
func (p *Parlia) FinalizeAndAssembleBidBlock(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB,
	body *types.Body, receipts []*types.Receipt, tracer *tracing.Hooks) (*types.Block, *big.Int, []*types.Receipt, error) {
	gasFee := state.GetBalance(consensus.SystemAddress).ToBig()
	block, receipts, err := p.FinalizeAndAssembleWithOpts(chain, header, state, body, receipts, tracer, FinalizeOpts{SignSystemTx: false})
	if err != nil {
		return nil, nil, nil, err
	}
	return block, gasFee, receipts, nil
}

// SignSystemTx signs a BidBlock system tx with the validator key.
func (p *Parlia) SignSystemTx(tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
	p.lock.RLock()
	defer p.lock.RUnlock()
	if p.signTxFn == nil {
		return nil, errors.New("signTxFn not set")
	}
	return p.signTxFn(accounts.Account{Address: p.val}, tx, chainID)
}

func (p *Parlia) ExpectedBidBlockTime(chain consensus.ChainHeaderReader, header, parent *types.Header) (uint64, error) {
	snap, err := p.snapshot(chain, parent.Number.Uint64(), parent.Hash(), nil)
	if err != nil {
		return 0, err
	}
	return p.blockTimeForBidBlock(snap, header, parent), nil
}

// VerifyBlockTime validates the deterministic BidBlock timestamp.
func (p *Parlia) VerifyBlockTime(chain consensus.ChainHeaderReader, header, parent *types.Header) error {
	expected, err := p.ExpectedBidBlockTime(chain, header, parent)
	if err != nil {
		return err
	}
	if got := header.MilliTimestamp(); got != expected {
		return fmt.Errorf("invalid BidBlock timestamp: got %d, want %d", got, expected)
	}
	return nil
}
