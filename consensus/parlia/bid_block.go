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
	buildertypes "github.com/ethereum/go-ethereum/core/types/builder"
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

// PrepareForBidBlock is Prepare with Coinbase set to the in-turn validator instead of p.val.
func (p *Parlia) PrepareForBidBlock(chain consensus.ChainHeaderReader, header *types.Header) error {
	// Coinbase must be set before prepare(): backOffTime and calcDifficulty depend on it.
	number := header.Number.Uint64()
	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		return err
	}
	header.Coinbase = snap.inturnValidator()
	return p.prepare(chain, header)
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

// isUnsignedSystemTxCandidate reports whether tx looks like an unsigned
// BidBlock system tx. It does not recover the sender.
func (p *Parlia) isUnsignedSystemTxCandidate(tx *types.Transaction) bool {
	if tx == nil || tx.To() == nil || !isToSystemContract(*tx.To()) {
		return false
	}
	if tx.GasPrice() == nil || tx.GasPrice().Sign() != 0 {
		return false
	}
	v, r, s := tx.RawSignatureValues()
	return isZeroSig(v, r, s)
}

// isSignableSystemTx reports whether tx can be bind-signed for BidBlock.
func (p *Parlia) isSignableSystemTx(tx *types.Transaction) bool {
	if !p.isUnsignedSystemTxCandidate(tx) {
		return false
	}
	if *tx.To() != common.HexToAddress(systemcontracts.ValidatorContract) {
		return false
	}
	return p.hasSignableSelector(tx.Data())
}

// expectedSystemTxShape returns the expected trailing system-tx order for accepted BidBlocks:
//
//	deposit -> distributeFinalityReward (cond.) -> updateValidatorSetV2 (cond.)
//
// Precondition: BidBlock admission has already enforced a non-zero deposit value.
func (p *Parlia) expectedSystemTxShape(header, parent *types.Header) []expectedSystemTxEntry {
	shape := make([]expectedSystemTxEntry, 0, 3)

	shape = append(shape, expectedSystemTxEntry{
		method:   "deposit",
		selector: p.selectorFor("deposit"),
	})

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

func (p *Parlia) verifySystemTxShape(txs []*types.Transaction, shape []expectedSystemTxEntry) error {
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

// ExtractBidBlockDepositValue locates the trailing unsigned system-tx region and
// returns its start index along with the value of the deposit tx (zero if absent).
func (p *Parlia) ExtractBidBlockDepositValue(txs []*types.Transaction) (int, *big.Int) {
	systemTxStart := len(txs)
	for i := len(txs) - 1; i >= 0; i-- {
		if !p.isUnsignedSystemTxCandidate(txs[i]) {
			break
		}
		systemTxStart = i
	}
	// Deposit is the first trailing unsigned system tx (see expectedSystemTxShape).
	// systemTxStart == 0 means there are no preceding user txs to collect fees from,
	// which is invalid by design — return zero GasFee so admission rejects it.
	if systemTxStart > 0 && systemTxStart < len(txs) {
		depositSel := p.selectorFor("deposit")
		if bytes.HasPrefix(txs[systemTxStart].Data(), depositSel[:]) {
			return systemTxStart, new(big.Int).Set(txs[systemTxStart].Value())
		}
	}
	return systemTxStart, new(big.Int)
}

// VerifyBidBlockSystemTxs validates the trailing unsigned system-tx region starting at systemTxStart.
//
//	Stage 1 — each trailing unsigned tx must be on the BEP-675 signable whitelist.
//	Stage 2 — selectors & order must match expectedSystemTxShape for this header.
func (p *Parlia) VerifyBidBlockSystemTxs(decoded *buildertypes.DecodedBidBlock, parent *types.Header, systemTxStart int) error {
	for i := systemTxStart; i < len(decoded.Txs); i++ {
		if !p.isSignableSystemTx(decoded.Txs[i]) {
			toAddr := "<nil>"
			if decoded.Txs[i].To() != nil {
				toAddr = decoded.Txs[i].To().Hex()
			}
			return fmt.Errorf("unsigned system tx at position %d (to=%s) is not on the signable whitelist", i, toAddr)
		}
	}
	shape := p.expectedSystemTxShape(decoded.Header, parent)
	return p.verifySystemTxShape(decoded.Txs[systemTxStart:], shape)
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

func (p *Parlia) BlockTimeUpperCheck(chain consensus.ChainHeaderReader, header *types.Header) error {
	number := header.Number.Uint64()
	snap, err := p.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		return err
	}

	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}

	maxAllowed := p.blockTimeForRamanujanFork(snap, header, parent)
	if header.MilliTimestamp() > maxAllowed {
		return fmt.Errorf("BidBlock time too far in future: headerTime=%d, maxAllowed=%d",
			header.MilliTimestamp(), maxAllowed)
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

func isZeroSig(v, r, s *big.Int) bool {
	isZero := func(x *big.Int) bool { return x == nil || x.Sign() == 0 }
	return isZero(v) && isZero(r) && isZero(s)
}
