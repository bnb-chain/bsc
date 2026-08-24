// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package vm

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// B20 mutable metadata and the remaining IB20 views. Metadata writes are not
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

	b20TopicNameUpdated         = eventTopic("NameUpdated(address,string)")
	b20TopicSymbolUpdated       = eventTopic("SymbolUpdated(address,string)")
	b20TopicContractURIUpdated  = eventTopic("ContractURIUpdated()")
	b20TopicEIP712DomainChanged = eventTopic("EIP712DomainChanged()")
)

// dispatchMetadata handles the metadata writers and the views that report
// configuration rather than balances. ok is false when sel is none of them.
func (t b20Token) dispatchMetadata(sel [4]byte, args []byte) (ret []byte, err error, ok bool) {
	switch sel {
	case selContractURI:
		return encString(t.s.contractURI()), nil, true
	case selSupplyCap:
		return encU256(t.s.supplyCap()), nil, true
	case selPausedFeatures:
		return encodeTuple(abiWordArray(t.pausedFeatures())), nil, true
	case selEIP712Domain:
		return t.eip712Domain(), nil, true

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

func (t b20Token) ensureMetadataWrite() error {
	if t.ctx.ReadOnly {
		return ErrWriteProtection
	}
	return t.ensureRole(roleMetadata)
}

func (t b20Token) updateName(v string) error {
	if err := t.ensureMetadataWrite(); err != nil {
		return err
	}
	if !t.s.setName(v) {
		return ErrOutOfGas
	}
	if !t.ctx.AddLog([]common.Hash{b20TopicNameUpdated, addrKey(t.ctx.Caller)}, encString(v)) {
		return ErrOutOfGas
	}
	if !t.ctx.AddLog([]common.Hash{b20TopicEIP712DomainChanged}, nil) {
		return ErrOutOfGas
	}
	return nil
}

func (t b20Token) updateSymbol(v string) error {
	if err := t.ensureMetadataWrite(); err != nil {
		return err
	}
	if !t.s.setSymbol(v) {
		return ErrOutOfGas
	}
	if !t.ctx.AddLog([]common.Hash{b20TopicSymbolUpdated, addrKey(t.ctx.Caller)}, encString(v)) {
		return ErrOutOfGas
	}
	return nil
}

func (t b20Token) updateContractURI(v string) error {
	if err := t.ensureMetadataWrite(); err != nil {
		return err
	}
	if !t.s.setContractURI(v) {
		return ErrOutOfGas
	}
	if !t.ctx.AddLog([]common.Hash{b20TopicContractURIUpdated}, nil) {
		return ErrOutOfGas
	}
	return nil
}

// pausedFeatures lists the set pause bits in ascending order. The bitmask is
// read once: reporting four features must not cost four SLOADs.
func (t b20Token) pausedFeatures() []common.Hash {
	p := t.s.paused()
	out := make([]common.Hash, 0, b20PauseSeize+1)
	for f := uint(0); f <= b20PauseSeize; f++ {
		if new(uint256.Int).Rsh(p, f).Uint64()&1 == 1 {
			out = append(out, wU8(byte(f)))
		}
	}
	return out
}

// eip712Domain implements ERC-5267. fields is 0x0f — name, version, chainId
// and verifyingContract are in use, salt and extensions are not — and matches
// the four fields domainSeparator actually hashes.
func (t b20Token) eip712Domain() []byte {
	var fields common.Hash
	fields[0] = 0x0f // bytes1 sits at the high end of its word
	return encodeTuple(
		abiWord(fields),
		abiString(t.s.name()),
		abiString(b20EIP712Version),
		abiWord(wU256(t.ctx.ChainID())),
		abiWord(addrKey(t.ctx.Self)),
		abiWord(common.Hash{}), // salt: unused
		abiWordArray(nil),      // extensions: none
	)
}
