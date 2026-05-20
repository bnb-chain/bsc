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

var signableSystemTxSelectors = map[string][4]byte{
	"deposit":                  {0xf3, 0x40, 0xfa, 0x01},
	"distributeFinalityReward": {0x30, 0x0c, 0x35, 0x67},
	"updateValidatorSetV2":     {0x1e, 0x4c, 0x15, 0x24},
}

type expectedSystemTxEntry struct {
	method   string
	selector [4]byte
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
	blockTime := parent.MilliTimestamp() + snap.BlockInterval

	return p.prepareHeader(chain, header, snap, validator, number, blockTime)
}

// FinalizeAndAssembleBidBlock assembles a BidBlock with unsigned system txs.
func (p *Parlia) FinalizeAndAssembleBidBlock(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB,
	body *types.Body, receipts []*types.Receipt, tracer *tracing.Hooks) (*types.Block, []*types.Receipt, error) {
	block, receipts, err := p.finalizeAndAssemble(chain, header, state, body, receipts, tracer, systemTxPacking)
	if err != nil {
		return nil, nil, err
	}
	return block, receipts, nil
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

// VerifyBlockTime validates the deterministic BidBlock timestamp.
func (p *Parlia) VerifyBlockTime(chain consensus.ChainHeaderReader, header, parent *types.Header) error {
	snap, err := p.snapshot(chain, parent.Number.Uint64(), parent.Hash(), nil)
	if err != nil {
		return err
	}
	expected := parent.MilliTimestamp() + snap.BlockInterval
	if got := header.MilliTimestamp(); got != expected {
		return fmt.Errorf("invalid BidBlock timestamp: got %d, want %d", got, expected)
	}
	return nil
}

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

// ExpectedSystemTxShape returns the expected trailing system-tx order for accepted BidBlocks:
//
//	deposit (if GasFee > 0) -> distributeFinalityReward (cond.) -> updateValidatorSetV2 (cond.)
//
// Precondition: gasFee is validated by BidBlock admission.
// Deposit is expected only when GasFee > 0.
func (p *Parlia) ExpectedSystemTxShape(header, parent *types.Header, gasFee *big.Int) []expectedSystemTxEntry {
	shape := make([]expectedSystemTxEntry, 0, 3)

	if gasFee.Sign() > 0 {
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
		if !bytes.HasPrefix(txs[i].Data(), exp.selector[:]) {
			return fmt.Errorf("expected system tx %q at position %d, got selector 0x%x",
				exp.method, i, txSelector(txs[i]))
		}
	}
	return nil
}

// ExtractBidBlockDepositValue returns the deposit value from unsigned BidBlock system txs.
func (p *Parlia) ExtractBidBlockDepositValue(txs []*types.Transaction) *big.Int {
	depositSel := p.selectorFor("deposit")
	valContract := common.HexToAddress(systemcontracts.ValidatorContract)

	for i := len(txs) - 1; i >= 0; i-- {
		tx := txs[i]
		if !p.IsUnsignedSystemTxCandidate(tx) {
			break
		}
		if tx.To() != nil && *tx.To() == valContract && bytes.HasPrefix(tx.Data(), depositSel[:]) {
			return new(big.Int).Set(tx.Value())
		}
	}
	return new(big.Int)
}

// ExtractDistributedGasFee returns the validator-contract deposit from a sealed block.
func (p *Parlia) ExtractDistributedGasFee(block *types.Block) *big.Int {
	txs := block.Transactions()
	depositSel := p.selectorFor("deposit")
	valContract := common.HexToAddress(systemcontracts.ValidatorContract)

	for i := len(txs) - 1; i >= 0; i-- {
		tx := txs[i]
		isSystem, err := p.IsSystemTransaction(tx, block.Header())
		if err != nil || !isSystem {
			break
		}
		if tx.To() == nil || *tx.To() != valContract {
			continue
		}
		if bytes.HasPrefix(tx.Data(), depositSel[:]) {
			return new(big.Int).Set(tx.Value())
		}
	}
	return new(big.Int)
}

func (p *Parlia) hasSignableSelector(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	selector := data[:4]
	for _, methodSelector := range signableSystemTxSelectors {
		if bytes.Equal(selector, methodSelector[:]) {
			return true
		}
	}
	return false
}

func (p *Parlia) selectorFor(methodName string) [4]byte {
	selector, ok := signableSystemTxSelectors[methodName]
	if !ok {
		panic(fmt.Sprintf("missing fixed system tx selector %s", methodName))
	}
	return selector
}

func txSelector(tx *types.Transaction) []byte {
	data := tx.Data()
	if len(data) < 4 {
		return data
	}
	return data[:4]
}

func isZeroSig(v, r, s *big.Int) bool {
	isZero := func(x *big.Int) bool { return x == nil || x.Sign() == 0 }
	return isZero(v) && isZero(r) && isZero(s)
}
