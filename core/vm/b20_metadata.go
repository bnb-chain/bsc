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

// B20 mutable metadata and the remaining IB20 views (BEP-702 section 3.6).
//
// Name, symbol and contractURI are rewritable under METADATA_ROLE. None of them
// is pause-gated: the pause bits cover value movement, and freezing an issuer's
// ability to correct a display string would not protect a holder.
//
// Renaming rewrites the EIP-712 domain, because the domain is derived from the
// live name rather than cached at creation. updateName therefore emits
// EIP712DomainChanged alongside NameUpdated — outstanding permit signatures
// stop verifying at that block, and an ERC-5267 consumer needs the signal to
// re-read the domain. updateSymbol does not: the symbol is not a domain field.

var (
	selContractURI       = selector("contractURI()")
	selUpdateName        = selector("updateName(string)")
	selUpdateSymbol      = selector("updateSymbol(string)")
	selUpdateContractURI = selector("updateContractURI(string)")
	selPausedFeatures    = selector("pausedFeatures()")
	selSupplyCap         = selector("supplyCap()")
	selEIP712Domain      = selector("eip712Domain()")

	b20TopicNameUpdated         = eventTopic("NameUpdated(string)")
	b20TopicSymbolUpdated       = eventTopic("SymbolUpdated(string)")
	b20TopicContractURIUpdated  = eventTopic("ContractURIUpdated()")
	b20TopicEIP712DomainChanged = eventTopic("EIP712DomainChanged()")
)

// dispatchMetadata handles the metadata writers and the views that report
// configuration rather than balances. ok is false when sel is none of them.
func (t b20Token) dispatchMetadata(sel [4]byte, args []byte) (ret []byte, err error, ok bool) {
	switch sel {
	// views
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

// ensureMetadataWrite applies the gates every metadata writer shares.
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
	t.s.setName(v)
	t.ctx.AddLog([]common.Hash{b20TopicNameUpdated}, encString(v))
	t.ctx.AddLog([]common.Hash{b20TopicEIP712DomainChanged}, nil)
	return nil
}

func (t b20Token) updateSymbol(v string) error {
	if err := t.ensureMetadataWrite(); err != nil {
		return err
	}
	t.s.setSymbol(v)
	t.ctx.AddLog([]common.Hash{b20TopicSymbolUpdated}, encString(v))
	return nil
}

// updateContractURI emits an argument-less event: the URI is a pointer to
// off-chain metadata that a consumer has to fetch anyway, so the log carries
// the invalidation signal rather than a copy of the string.
func (t b20Token) updateContractURI(v string) error {
	if err := t.ensureMetadataWrite(); err != nil {
		return err
	}
	t.s.setContractURI(v)
	t.ctx.AddLog([]common.Hash{b20TopicContractURIUpdated}, nil)
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
