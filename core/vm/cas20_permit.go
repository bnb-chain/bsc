package vm

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// CAS20 EIP-2612 permit (gasless approvals) and the memo transfer/mint/burn
// family. The EIP-712 domain is derived from the live token name, so any
// updateName automatically invalidates outstanding permit signatures — there
// is no cached separator to roll.

// cas20EIP712Version is the domain's version field. DOMAIN_SEPARATOR() hashes it
// and eip712Domain() reports it, so the two cannot drift apart.
const cas20EIP712Version = "1"

var (
	selDomainSeparator      = selector("DOMAIN_SEPARATOR()")
	selNonces               = selector("nonces(address)")
	selPermit               = selector("permit(address,address,uint256,uint256,uint8,bytes32,bytes32)")
	selTransferWithMemo     = selector("transferWithMemo(address,uint256,bytes32)")
	selTransferFromWithMemo = selector("transferFromWithMemo(address,address,uint256,bytes32)")
	selMintWithMemo         = selector("mintWithMemo(address,uint256,bytes32)")
	selBurnWithMemo         = selector("burnWithMemo(uint256,bytes32)")

	cas20DomainTypehash = crypto.Keccak256Hash([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	cas20PermitTypehash = crypto.Keccak256Hash([]byte("Permit(address owner,address spender,uint256 value,uint256 nonce,uint256 deadline)"))
	cas20TopicMemo      = eventTopic("Memo(address,bytes32)")
)

// dispatchPermitMemo handles the permit and *WithMemo selectors. ok is false
// when sel matches none of them.
func (t cas20Token) dispatchPermitMemo(sel [4]byte, args []byte) (ret []byte, err error, ok bool) {
	switch sel {
	case selDomainSeparator:
		d, ok := t.domainSeparator()
		if !ok {
			return nil, ErrOutOfGas, true
		}
		return d.Bytes(), nil, true
	case selNonces:
		owner, err := readAddress(args, 0)
		if err != nil {
			return nil, err, true
		}
		return encU256(t.s.nonce(owner)), nil, true
	case selPermit:
		ret, err := t.decodePermit(args)
		return ret, err, true

	case selTransferWithMemo:
		to, amount, memo, err := readToAmountMemo(args)
		if err != nil {
			return nil, err, true
		}
		ret, err := t.transfer(t.ctx.Caller, to, amount)
		if err == nil {
			if !t.emitMemo(memo) {
				return nil, ErrOutOfGas, true
			}
		}
		return ret, err, true
	case selTransferFromWithMemo:
		from, err := readAddress(args, 0)
		if err != nil {
			return nil, err, true
		}
		to, err := readAddress(args, 1)
		if err != nil {
			return nil, err, true
		}
		amount, err := readU256(args, 2)
		if err != nil {
			return nil, err, true
		}
		memo, err := readWord(args, 3)
		if err != nil {
			return nil, err, true
		}
		ret, err := t.transferFrom(t.ctx.Caller, from, to, amount)
		if err == nil {
			if !t.emitMemo(memo) {
				return nil, ErrOutOfGas, true
			}
		}
		return ret, err, true
	case selMintWithMemo:
		to, amount, memo, err := readToAmountMemo(args)
		if err != nil {
			return nil, err, true
		}
		if err := t.mint(to, amount); err != nil {
			return nil, err, true
		}
		if !t.emitMemo(memo) {
			return nil, ErrOutOfGas, true
		}
		return nil, nil, true
	case selBurnWithMemo:
		amount, err := readU256(args, 0)
		if err != nil {
			return nil, err, true
		}
		memo, err := readWord(args, 1)
		if err != nil {
			return nil, err, true
		}
		if err := t.burn(t.ctx.Caller, amount); err != nil {
			return nil, err, true
		}
		if !t.emitMemo(memo) {
			return nil, ErrOutOfGas, true
		}
		return nil, nil, true
	}
	return nil, nil, false
}

// emitMemo emits Memo(caller, memo) immediately after a primary op.
func (t cas20Token) emitMemo(memo common.Hash) bool {
	return t.ctx.AddLog([]common.Hash{cas20TopicMemo, addrKey(t.ctx.Caller), memo}, nil)
}

func readToAmountMemo(args []byte) (common.Address, *uint256.Int, common.Hash, error) {
	to, err := readAddress(args, 0)
	if err != nil {
		return common.Address{}, nil, common.Hash{}, err
	}
	amount, err := readU256(args, 1)
	if err != nil {
		return common.Address{}, nil, common.Hash{}, err
	}
	memo, err := readWord(args, 2)
	if err != nil {
		return common.Address{}, nil, common.Hash{}, err
	}
	return to, amount, memo, nil
}

// --- EIP-2612 permit --------------------------------------------------------

// domainSeparator computes the EIP-712 domain hash from the live token name,
// version "1", chain id and the token address, and reports false when a charge on
// the way could not be covered. A Solidity ERC-2612 pays for the same three
// hashes, so leaving them free would make the native path cheaper than bytecode
// (BEP-702 3.14).
func (t cas20Token) domainSeparator() (common.Hash, bool) {
	name, ok := t.s.name()
	if !ok {
		return common.Hash{}, false
	}
	if !t.ctx.chargeKeccak(len(name)) ||
		!t.ctx.chargeKeccak(len(cas20EIP712Version)) ||
		!t.ctx.chargeKeccak(160) {
		return common.Hash{}, false
	}
	nameHash := crypto.Keccak256Hash([]byte(name))
	versionHash := crypto.Keccak256Hash([]byte(cas20EIP712Version))
	chainID := t.ctx.ChainID().Bytes32()

	enc := make([]byte, 0, 160)
	enc = append(enc, cas20DomainTypehash.Bytes()...)
	enc = append(enc, nameHash.Bytes()...)
	enc = append(enc, versionHash.Bytes()...)
	enc = append(enc, chainID[:]...)
	enc = append(enc, addrKey(t.ctx.Self).Bytes()...)
	return crypto.Keccak256Hash(enc), true
}

func (t cas20Token) decodePermit(args []byte) ([]byte, error) {
	owner, err := readAddress(args, 0)
	if err != nil {
		return nil, err
	}
	spender, err := readAddress(args, 1)
	if err != nil {
		return nil, err
	}
	value, err := readU256(args, 2)
	if err != nil {
		return nil, err
	}
	deadline, err := readU256(args, 3)
	if err != nil {
		return nil, err
	}
	// v is a uint8: a word with dirty high bytes is a malformed encoding, not a
	// signature to be truncated and then blamed on the signer.
	v, err := readStrictUint8(args, 4)
	if err != nil {
		return nil, err
	}
	r, err := readWord(args, 5)
	if err != nil {
		return nil, err
	}
	s, err := readWord(args, 6)
	if err != nil {
		return nil, err
	}
	return t.permit(owner, spender, value, deadline, v, r, s)
}

func (t cas20Token) permit(owner, spender common.Address, value, deadline *uint256.Int, v byte, r, s common.Hash) ([]byte, error) {
	if t.ctx.ReadOnly {
		return nil, ErrWriteProtection
	}
	if owner == (common.Address{}) {
		return nil, revCAS20("InvalidApprover(address)", errSelInvalidApprover, addrKey(owner))
	}
	// Deadline is inclusive: now > deadline is expired.
	if deadline.LtUint64(t.ctx.BlockTime()) {
		return nil, revCAS20("ExpiredSignature(uint256)", errSelExpiredSignature, wU256(deadline))
	}
	nonce := t.s.nonce(owner)

	structHash := make([]byte, 0, 192)
	structHash = append(structHash, cas20PermitTypehash.Bytes()...)
	structHash = append(structHash, addrKey(owner).Bytes()...)
	structHash = append(structHash, addrKey(spender).Bytes()...)
	vb := value.Bytes32()
	structHash = append(structHash, vb[:]...)
	nb := nonce.Bytes32()
	structHash = append(structHash, nb[:]...)
	db := deadline.Bytes32()
	structHash = append(structHash, db[:]...)

	dom, paid := t.domainSeparator()
	if !paid {
		return nil, ErrOutOfGas
	}
	// The struct hash, the final digest, and the signature recovery. ECRECOVER is
	// its own precompile at a flat 3000 gas; a native permit doing the same
	// secp256k1 work owes the same, and owes it whether or not the signature turns
	// out to be valid.
	if !t.ctx.chargeKeccak(len(structHash)) ||
		!t.ctx.chargeKeccak(66) ||
		!t.ctx.chargeGas(params.EcrecoverGas) {
		return nil, ErrOutOfGas
	}
	digest := crypto.Keccak256([]byte{0x19, 0x01}, dom.Bytes(), crypto.Keccak256(structHash))

	signer, ok := ecrecoverAddress(digest, v, r, s)
	if !ok || signer != owner {
		return nil, revCAS20("InvalidSigner(address,address)", errSelInvalidSigner,
			addrKey(signer), addrKey(owner))
	}

	// A signature over the zero spender is a valid signature for an approval that
	// must still be refused; without this check permit would set an allowance
	// approve() rejects. Its position decides which error a request that is wrong
	// twice receives: a bad signature naming the zero spender is reported as
	// InvalidSigner, and the caller pays the two keccaks and the ecrecover before
	// the free comparison runs.
	if spender == (common.Address{}) {
		return nil, revCAS20("InvalidSpender(address)", errSelInvalidSpender, addrKey(spender))
	}
	t.s.setNonce(owner, new(uint256.Int).AddUint64(nonce, 1))
	t.s.setAllowance(owner, spender, value)
	if !t.emit(cas20TopicApproval, owner, spender, value) {
		return nil, ErrOutOfGas
	}
	// Empty returndata: EIP-2612 declares `permit(...) external` with no return
	// value, so returning an ABI true would make a caller that decodes a bool
	// succeed here and fail against every other implementation.
	return nil, nil
}

// ecrecoverAddress recovers the signer of an EIP-712 digest. It enforces
// EIP-2 low-s and v ∈ {27,28}; ERC-1271 contract signatures are not supported.
func ecrecoverAddress(hash []byte, v byte, r, s common.Hash) (common.Address, bool) {
	if v != 27 && v != 28 {
		return common.Address{}, false
	}
	rBig := new(big.Int).SetBytes(r[:])
	sBig := new(big.Int).SetBytes(s[:])
	if !crypto.ValidateSignatureValues(v-27, rBig, sBig, true) {
		return common.Address{}, false
	}
	sig := make([]byte, 65)
	copy(sig[0:32], r[:])
	copy(sig[32:64], s[:])
	sig[64] = v - 27
	pub, err := crypto.Ecrecover(hash, sig)
	if err != nil || len(pub) == 0 {
		return common.Address{}, false
	}
	var addr common.Address
	copy(addr[:], crypto.Keccak256(pub[1:])[12:])
	return addr, true
}
