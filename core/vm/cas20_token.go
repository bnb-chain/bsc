package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

type cas20Token struct {
	ctx      *PrecompileContext
	s        cas20Storage
	decimals uint8

	// privileged is set on the factory's bootstrap path, where role and
	// transfer-side policy gates are skipped (anti-revival and MINT_RECEIVER
	// checks are still enforced). Always false for ordinary calls.
	privileged bool

	// inAnnounce marks the Asset disclosure window. announce sets it on the
	// token value it threads into the bundle's internal calls, so a nested
	// announce sees it and reverts; the enclosing frame's own value is a copy
	// and needs no reset.
	inAnnounce bool
}

func newCAS20Token(ctx *PrecompileContext, decimals uint8) cas20Token {
	return cas20Token{ctx: ctx, s: newMeteredCAS20Storage(ctx), decimals: decimals}
}

// newCAS20TokenBootstrap returns a token in the factory's privileged bootstrap
// mode, where role and transfer-side policy gates are skipped.
func newCAS20TokenBootstrap(ctx *PrecompileContext, decimals uint8) cas20Token {
	t := newCAS20Token(ctx, decimals)
	t.privileged = true
	return t
}

// pause feature bits in the paused bitmask (slot 11).
const (
	cas20PauseTransfer = 0
	cas20PauseMint     = 1
	cas20PauseBurn     = 2
	cas20PauseSeize    = 3
)

func selector(sig string) (s [4]byte) {
	copy(s[:], crypto.Keccak256([]byte(sig)))
	return s
}

var (
	selName         = selector("name()")
	selSymbol       = selector("symbol()")
	selDecimals     = selector("decimals()")
	selTotalSupply  = selector("totalSupply()")
	selBalanceOf    = selector("balanceOf(address)")
	selAllowance    = selector("allowance(address,address)")
	selApprove      = selector("approve(address,uint256)")
	selTransfer     = selector("transfer(address,uint256)")
	selTransferFrom = selector("transferFrom(address,address,uint256)")

	cas20TopicTransfer = eventTopic("Transfer(address,address,uint256)")
	cas20TopicApproval = eventTopic("Approval(address,address,uint256)")

	maxU256 = new(uint256.Int).Not(new(uint256.Int))
)

// dispatch routes a call by selector. It returns the ABI-encoded result on
// success. Business-rule failures and unknown selectors revert
// (ErrExecutionReverted); a write reached in a read-only frame throws
// (ErrWriteProtection), matching SSTORE-in-STATICCALL semantics.
func (t cas20Token) dispatch(input []byte) ([]byte, error) {
	if len(input) < 4 {
		return nil, ErrExecutionReverted
	}
	var sel [4]byte
	copy(sel[:], input[:4])
	args := input[4:]

	switch sel {
	case selName:
		v, ok := t.s.name()
		if !ok {
			return nil, ErrOutOfGas
		}
		return encString(v), nil
	case selSymbol:
		v, ok := t.s.symbol()
		if !ok {
			return nil, ErrOutOfGas
		}
		return encString(v), nil
	case selDecimals:
		return encU256(uint256.NewInt(uint64(t.decimals))), nil
	case selTotalSupply:
		return encU256(t.s.totalSupply()), nil
	case selBalanceOf:
		a, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		return encU256(t.s.balanceOf(a)), nil
	case selAllowance:
		owner, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		spender, err := readAddress(args, 1)
		if err != nil {
			return nil, err
		}
		return encU256(t.s.allowance(owner, spender)), nil
	case selApprove:
		spender, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		amount, err := readU256(args, 1)
		if err != nil {
			return nil, err
		}
		return t.approve(t.ctx.Caller, spender, amount)
	case selTransfer:
		to, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		amount, err := readU256(args, 1)
		if err != nil {
			return nil, err
		}
		return t.transfer(t.ctx.Caller, to, amount)
	case selTransferFrom:
		from, err := readAddress(args, 0)
		if err != nil {
			return nil, err
		}
		to, err := readAddress(args, 1)
		if err != nil {
			return nil, err
		}
		amount, err := readU256(args, 2)
		if err != nil {
			return nil, err
		}
		return t.transferFrom(t.ctx.Caller, from, to, amount)
	}
	// RBAC / pause / mint-burn / configurable selectors.
	if ret, err, ok := t.dispatchAdmin(sel, args); ok {
		return ret, err
	}
	// Mutable metadata and the configuration views.
	if ret, err, ok := t.dispatchMetadata(sel, args); ok {
		return ret, err
	}
	// permit (EIP-2612) and the *WithMemo family.
	if ret, err, ok := t.dispatchPermitMemo(sel, args); ok {
		return ret, err
	}
	return nil, ErrExecutionReverted
}

// --- ERC-20 core ------------------------------------------------------------

func (t cas20Token) approve(owner, spender common.Address, amount *uint256.Int) ([]byte, error) {
	if t.ctx.ReadOnly {
		return nil, ErrWriteProtection
	}
	// The approver is checked first. owner is msg.sender, so a zero one only
	// arises from a frame with no caller, but the check is declared and costs a
	// comparison.
	if owner == (common.Address{}) {
		return nil, revCAS20("InvalidApprover(address)", errSelInvalidApprover, addrKey(owner))
	}
	if spender == (common.Address{}) {
		return nil, revCAS20("InvalidSpender(address)", errSelInvalidSpender, addrKey(spender))
	}
	// Not gated by pause or policy, and that follows from the published lists
	// rather than from intent: PausableFeature is { TRANSFER, MINT, BURN, SEIZE }
	// (BEP-702 3.9) and the six policy scopes cover transfer sender/receiver/
	// executor, mint receiver and seize holder/receiver (3.8). Neither names
	// approve, so there is nothing to consult. A paused token still accepts
	// approvals; the transfer they authorize is what the pause stops.
	t.s.setAllowance(owner, spender, amount)
	if !t.emit(cas20TopicApproval, owner, spender, amount) {
		return nil, ErrOutOfGas
	}
	return encBool(true), nil
}

func (t cas20Token) transfer(from, to common.Address, amount *uint256.Int) ([]byte, error) {
	if t.ctx.ReadOnly {
		return nil, ErrWriteProtection
	}
	if t.isPaused(cas20PauseTransfer) {
		return nil, revCAS20("ContractPaused(uint8)", errSelContractPaused, wU8(cas20PauseTransfer))
	}
	if err := t.move(from, to, amount); err != nil {
		return nil, err
	}
	if !t.emit(cas20TopicTransfer, from, to, amount) {
		return nil, ErrOutOfGas
	}
	return encBool(true), nil
}

func (t cas20Token) transferFrom(spender, from, to common.Address, amount *uint256.Int) ([]byte, error) {
	if t.ctx.ReadOnly {
		return nil, ErrWriteProtection
	}
	if t.isPaused(cas20PauseTransfer) {
		return nil, revCAS20("ContractPaused(uint8)", errSelContractPaused, wU8(cas20PauseTransfer))
	}
	// The two malformed-argument checks come before the allowance and the
	// executor policy: a transfer to the zero address is reported as such whatever
	// the caller's allowance is. move() repeats them for
	// the direct transfer path; they are comparisons on already-decoded arguments,
	// so the duplicate costs nothing.
	if to == (common.Address{}) {
		return nil, revCAS20("InvalidReceiver(address)", errSelInvalidReceiver, addrKey(to))
	}
	if from == (common.Address{}) {
		return nil, revCAS20("InvalidSender(address)", errSelInvalidSender, addrKey(from))
	}
	// The allowance is spent unconditionally — the owner spending their own
	// balance through transferFrom needs a self-approval, and the factory
	// bootstrap window carves no exception either. U256::MAX stays infinite.
	//
	// Only the executor policy takes the self shortcut, and the privileged one:
	// the sender policy already covers `from` inside move().
	slot := t.s.allowanceSlot(from, spender)
	allowed := t.s.getU256At(slot)
	infinite := allowed.Eq(maxU256)
	if !infinite && allowed.Lt(amount) {
		return nil, revCAS20("InsufficientAllowance(address,uint256,uint256)", errSelInsufficientAllow,
			addrKey(spender), wU256(allowed), wU256(amount))
	}
	// Consulted after the allowance: an unauthorized executor with too little
	// allowance is told about the allowance.
	if !t.privileged && spender != from {
		if _, _, executor := t.s.transferPolicies(); !t.policyAllows(executor, spender) {
			return nil, revCAS20("PolicyForbids(bytes32,uint64)", errSelPolicyForbids,
				scopeTransferExecutor, wU64(executor))
		}
	}
	if !infinite {
		t.s.setU256At(slot, new(uint256.Int).Sub(allowed, amount))
	}
	if err := t.move(from, to, amount); err != nil {
		return nil, err
	}
	if !t.emit(cas20TopicTransfer, from, to, amount) {
		return nil, ErrOutOfGas
	}
	return encBool(true), nil
}

func (t cas20Token) policyAllows(id uint64, account common.Address) bool {
	if id == 0 {
		return true
	}
	return newPolicyReg(t.ctx).isAuthorized(id, account)
}

// move debits from and credits to, reverting on insufficient balance.
func (t cas20Token) move(from, to common.Address, amount *uint256.Int) error {
	if to == (common.Address{}) {
		return revCAS20("InvalidReceiver(address)", errSelInvalidReceiver, addrKey(to))
	}
	if from == (common.Address{}) {
		return revCAS20("InvalidSender(address)", errSelInvalidSender, addrKey(from))
	}
	// TRANSFER_SENDER / TRANSFER_RECEIVER compliance (skipped when privileged).
	// Both ids share one slot, and the revert payload reuses the id already read
	// rather than reading it again.
	if !t.privileged {
		sender, receiver, _ := t.s.transferPolicies()
		if !t.policyAllows(sender, from) {
			return revCAS20("PolicyForbids(bytes32,uint64)", errSelPolicyForbids,
				scopeTransferSender, wU64(sender))
		}
		if !t.policyAllows(receiver, to) {
			return revCAS20("PolicyForbids(bytes32,uint64)", errSelPolicyForbids,
				scopeTransferReceiver, wU64(receiver))
		}
	}
	// Each balance slot is derived once and reused for its read and its write, and
	// both writes happen unconditionally — including when from == to or amount is
	// zero. Bytecode performs them anyway, and skipping them would price a native
	// token below the contract it replaces (BEP-702 3.14).
	fromSlot := t.s.balanceSlot(from)
	bal := t.s.getU256At(fromSlot)
	if bal.Lt(amount) {
		return revCAS20("InsufficientBalance(address,uint256,uint256)", errSelInsufficientBalance,
			addrKey(from), wU256(bal), wU256(amount))
	}
	t.s.setU256At(fromSlot, new(uint256.Int).Sub(bal, amount))
	toSlot := t.s.balanceSlot(to)
	t.s.setU256At(toSlot, new(uint256.Int).Add(t.s.getU256At(toSlot), amount))
	return nil
}

func (t cas20Token) isPaused(bit uint) bool {
	return new(uint256.Int).Rsh(t.s.paused(), bit).Uint64()&1 == 1
}

func (t cas20Token) emit(topic0 common.Hash, a, b common.Address, value *uint256.Int) bool {
	v := value.Bytes32()
	return t.ctx.AddLog([]common.Hash{topic0, addrKey(a), addrKey(b)}, v[:])
}

// --- ABI helpers ------------------------------------------------------------

func readWord(args []byte, i int) (common.Hash, error) {
	off := i * 32
	if len(args) < off+32 {
		return common.Hash{}, ErrExecutionReverted
	}
	return common.BytesToHash(args[off : off+32]), nil
}

func readAddress(args []byte, i int) (common.Address, error) {
	w, err := readWord(args, i)
	if err != nil {
		return common.Address{}, err
	}
	a, ok := addressFromWord(w)
	if !ok {
		return common.Address{}, ErrExecutionReverted
	}
	return a, nil
}

// addressFromWord is the strict ABI reading of an address word: the twelve high
// bytes must be zero. Truncating them instead would accept encodings another
// client rejects, and address[] elements need the same rule as scalar arguments.
// u64FromWord decodes a uint64 argument, refusing anything above the low eight
// bytes — Solidity's external decoder rejects a dirty word rather than
// truncating it.
// wordFitsIn reports whether only the low n bytes of w are set, the check
// Solidity's external decoder makes for every type narrower than a word.
func wordFitsIn(w common.Hash, n int) bool {
	for _, b := range w[:32-n] {
		if b != 0 {
			return false
		}
	}
	return true
}

func u64FromWord(w common.Hash) (uint64, bool) {
	if !wordFitsIn(w, 8) {
		return 0, false
	}
	return new(uint256.Int).SetBytes(w.Bytes()).Uint64(), true
}

func addressFromWord(w common.Hash) (common.Address, bool) {
	if !wordFitsIn(w, 20) {
		return common.Address{}, false
	}
	return common.BytesToAddress(w.Bytes()), true
}

// readU64 strictly decodes a uint64 argument (upper 24 bytes must be zero).
func readU64(args []byte, i int) (uint64, error) {
	w, err := readWord(args, i)
	if err != nil {
		return 0, err
	}
	for _, b := range w[:24] {
		if b != 0 {
			return 0, ErrExecutionReverted
		}
	}
	return new(uint256.Int).SetBytes(w[24:]).Uint64(), nil
}

func readU256(args []byte, i int) (*uint256.Int, error) {
	w, err := readWord(args, i)
	if err != nil {
		return nil, err
	}
	return new(uint256.Int).SetBytes(w.Bytes()), nil
}

func encU256(v *uint256.Int) []byte {
	b := v.Bytes32()
	return b[:]
}

func encBool(b bool) []byte {
	out := make([]byte, 32)
	if b {
		out[31] = 1
	}
	return out
}

// encString ABI-encodes a string as a complete return value: one dynamic member
// in a tuple, so a head offset followed by the length-prefixed data.
func encString(s string) []byte { return encodeTuple(abiString(s)) }

// --- ABI encoding primitives ------------------------------------------------

// abiPart is one encoded tuple member: a static head word, or a dynamic value
// whose head is an offset and whose tail carries length-prefixed data.
type abiPart struct {
	word    common.Hash
	dynamic bool
	tail    []byte
}

func abiWord(w common.Hash) abiPart { return abiPart{word: w} }

// abiBytes encodes a dynamic byte string: length word then right-padded data.
func abiBytes(b []byte) abiPart {
	padded := (len(b) + 31) / 32 * 32
	tail := make([]byte, 32+padded)
	l := uint256.NewInt(uint64(len(b))).Bytes32()
	copy(tail[:32], l[:])
	copy(tail[32:], b)
	return abiPart{dynamic: true, tail: tail}
}

func abiString(s string) abiPart { return abiBytes([]byte(s)) }

// abiWordArray encodes a dynamic array whose members are static words
// (uint8[], uint256[], address[]): a length word followed by the members.
func abiWordArray(words []common.Hash) abiPart {
	tail := make([]byte, 0, 32*(len(words)+1))
	l := uint256.NewInt(uint64(len(words))).Bytes32()
	tail = append(tail, l[:]...)
	for _, w := range words {
		tail = append(tail, w[:]...)
	}
	return abiPart{dynamic: true, tail: tail}
}

// encodeTuple lays out parts as ABI head/tail: static members inline, dynamic
// members as an offset into the tail section.
func encodeTuple(parts ...abiPart) []byte {
	head := make([]byte, 0, 32*len(parts))
	tail := make([]byte, 0)
	tailStart := uint64(32 * len(parts))
	for _, p := range parts {
		if !p.dynamic {
			head = append(head, p.word[:]...)
			continue
		}
		off := uint256.NewInt(tailStart + uint64(len(tail))).Bytes32()
		head = append(head, off[:]...)
		tail = append(tail, p.tail...)
	}
	return append(head, tail...)
}

// abiEncodeStruct produces abi.encode(s) for one dynamic struct. Encoding a
// single value wraps it in a one-element tuple, so the result opens with an
// offset word (0x20) and only then carries the struct's own head/tail. Getting
// this wrapper wrong is invisible to a test that encodes and decodes with the
// same helper, so it is spelled out here once and used everywhere.
func abiEncodeStruct(members ...abiPart) []byte {
	return encodeTuple(abiPart{dynamic: true, tail: encodeTuple(members...)})
}

func readBytesArg(args []byte, argIndex int) ([]byte, error) {
	s, err := readStringArg(args, argIndex)
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}
