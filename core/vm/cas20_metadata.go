package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// CAS20 mutable metadata and the remaining IB20 views. Metadata writes are not
// pause-gated, and a rename changes the live EIP-712 domain — it emits
// EIP712DomainChanged and invalidates outstanding permits (BEP-702 3.6).

var (
	selContractURI       = selector("contractURI()")
	selUpdateName        = selector("updateName(string)")
	selUpdateSymbol      = selector("updateSymbol(string)")
	selUpdateContractURI = selector("updateContractURI(string)")
	selPausedFeatures    = selector("pausedFeatures()")
	selSupplyCap         = selector("supplyCap()")
	selEIP712Domain      = selector("eip712Domain()")

	cas20TopicNameUpdated         = eventTopic("NameUpdated(address,string)")
	cas20TopicSymbolUpdated       = eventTopic("SymbolUpdated(address,string)")
	cas20TopicContractURIUpdated  = eventTopic("ContractURIUpdated()")
	cas20TopicEIP712DomainChanged = eventTopic("EIP712DomainChanged()")
)

// dispatchMetadata handles the metadata writers and the views that report
// configuration rather than balances. ok is false when sel is none of them.
func (t cas20Token) dispatchMetadata(sel [4]byte, args []byte) (ret []byte, err error, ok bool) {
	switch sel {
	case selContractURI:
		v, ok := t.s.contractURI()
		if !ok {
			return nil, ErrOutOfGas, true
		}
		return encString(v), nil, true
	case selSupplyCap:
		return encU256(t.s.supplyCap()), nil, true
	case selPausedFeatures:
		return encodeTuple(abiWordArray(t.pausedFeatures())), nil, true
	case selEIP712Domain:
		d, ok := t.eip712Domain()
		if !ok {
			return nil, ErrOutOfGas, true
		}
		return d, nil, true

	// writes (METADATA_ROLE)
	case selUpdateName:
		v, err := readStringArg(args, 0)
		if err != nil {
			return nil, err, true
		}
		return nil, t.updateName(v), true
	case selUpdateSymbol:
		v, err := readStringArg(args, 0)
		if err != nil {
			return nil, err, true
		}
		return nil, t.updateSymbol(v), true
	case selUpdateContractURI:
		v, err := readStringArg(args, 0)
		if err != nil {
			return nil, err, true
		}
		return nil, t.updateContractURI(v), true
	}
	return nil, nil, false
}

func (t cas20Token) ensureMetadataWrite() error {
	if t.ctx.ReadOnly {
		return ErrWriteProtection
	}
	return t.ensureRole(roleMetadata)
}

func (t cas20Token) updateName(v string) error {
	if err := t.ensureMetadataWrite(); err != nil {
		return err
	}
	if !t.s.setName(v) {
		return ErrOutOfGas
	}
	if !t.ctx.AddLog([]common.Hash{cas20TopicNameUpdated, addrKey(t.ctx.Caller)}, encString(v)) {
		return ErrOutOfGas
	}
	if !t.ctx.AddLog([]common.Hash{cas20TopicEIP712DomainChanged}, nil) {
		return ErrOutOfGas
	}
	return nil
}

func (t cas20Token) updateSymbol(v string) error {
	if err := t.ensureMetadataWrite(); err != nil {
		return err
	}
	if !t.s.setSymbol(v) {
		return ErrOutOfGas
	}
	if !t.ctx.AddLog([]common.Hash{cas20TopicSymbolUpdated, addrKey(t.ctx.Caller)}, encString(v)) {
		return ErrOutOfGas
	}
	return nil
}

func (t cas20Token) updateContractURI(v string) error {
	if err := t.ensureMetadataWrite(); err != nil {
		return err
	}
	if !t.s.setContractURI(v) {
		return ErrOutOfGas
	}
	if !t.ctx.AddLog([]common.Hash{cas20TopicContractURIUpdated}, nil) {
		return ErrOutOfGas
	}
	return nil
}

// pausedFeatures lists the set pause bits in ascending order. The bitmask is
// read once: reporting four features must not cost four SLOADs.
func (t cas20Token) pausedFeatures() []common.Hash {
	p := t.s.paused()
	out := make([]common.Hash, 0, cas20PauseSeize+1)
	for f := uint(0); f <= cas20PauseSeize; f++ {
		if new(uint256.Int).Rsh(p, f).Uint64()&1 == 1 {
			out = append(out, wU8(byte(f)))
		}
	}
	return out
}

// eip712Domain implements ERC-5267. fields is 0x0f — name, version, chainId
// and verifyingContract are in use, salt and extensions are not — and matches
// the four fields domainSeparator actually hashes.
func (t cas20Token) eip712Domain() ([]byte, bool) {
	name, ok := t.s.name()
	if !ok {
		return nil, false
	}
	var fields common.Hash
	fields[0] = 0x0f // bytes1 sits at the high end of its word
	return encodeTuple(
		abiWord(fields),
		abiString(name),
		abiString(cas20EIP712Version),
		abiWord(wU256(t.ctx.ChainID())),
		abiWord(addrKey(t.ctx.Self)),
		abiWord(common.Hash{}), // salt: unused
		abiWordArray(nil),      // extensions: none
	), true
}
