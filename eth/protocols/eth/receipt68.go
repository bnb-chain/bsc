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

package eth

import (
	"bytes"
	"fmt"
	"io"
	"iter"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

//TODO(Nathan): delete this file once eth/68 dropped

// This is just a sanity limit for the size of a single receipt.
const maxReceiptSize = 16 * 1024 * 1024

// decode68 parses a receipt in the eth/68 network encoding.
func (r *Receipt) decode68(b []byte) error {
	k, content, _, err := rlp.Split(b)
	if err != nil {
		return err
	}

	*r = Receipt{}
	if k == rlp.List {
		// Legacy receipt.
		return r.decodeInnerList(b, false, true)
	}
	// Typed receipt.
	if len(content) < 2 || len(content) > maxReceiptSize {
		return fmt.Errorf("invalid receipt size %d", len(content))
	}
	r.TxType = content[0]
	return r.decodeInnerList(content[1:], false, true)
}

// decodeDatabase parses a receipt in the basic database encoding.
func (r *Receipt) decodeDatabase(txType byte, b []byte) error {
	*r = Receipt{TxType: txType}
	return r.decodeInnerList(b, false, false)
}

func (r *Receipt) decodeInnerList(input []byte, readTxType, readBloom bool) error {
	input, _, err := rlp.SplitList(input)
	if err != nil {
		return fmt.Errorf("inner list: %v", err)
	}

	// txType
	if readTxType {
		var txType uint64
		txType, input, err = rlp.SplitUint64(input)
		if err != nil {
			return fmt.Errorf("invalid txType: %w", err)
		}
		if txType > 0x7f {
			return fmt.Errorf("invalid txType: too large")
		}
		r.TxType = byte(txType)
	}

	// status
	r.PostStateOrStatus, input, err = rlp.SplitString(input)
	if err != nil {
		return fmt.Errorf("invalid postStateOrStatus: %w", err)
	}
	if len(r.PostStateOrStatus) > 1 && len(r.PostStateOrStatus) != 32 {
		return fmt.Errorf("invalid postStateOrStatus length %d", len(r.PostStateOrStatus))
	}

	// gas
	r.GasUsed, input, err = rlp.SplitUint64(input)
	if err != nil {
		return fmt.Errorf("invalid gasUsed: %w", err)
	}

	// bloom
	if readBloom {
		var bloomBytes []byte
		bloomBytes, input, err = rlp.SplitString(input)
		if err != nil {
			return fmt.Errorf("invalid bloom: %v", err)
		}
		if len(bloomBytes) != types.BloomByteLength {
			return fmt.Errorf("invalid bloom length %d", len(bloomBytes))
		}
	}

	// logs
	_, rest, err := rlp.SplitList(input)
	if err != nil {
		return fmt.Errorf("invalid logs: %w", err)
	}
	if len(rest) != 0 {
		return fmt.Errorf("junk at end of receipt")
	}
	r.Logs = input
	return nil
}

// encodeForStorage produces the the storage encoding, i.e. the result matches
// the RLP encoding of types.ReceiptForStorage.
func (r *Receipt) encodeForStorage(w *rlp.EncoderBuffer) {
	list := w.List()
	w.WriteBytes(r.PostStateOrStatus)
	w.WriteUint64(r.GasUsed)
	w.Write(r.Logs)
	w.ListEnd(list)
}

// encodeForNetwork68 produces the eth/68 network protocol encoding of a receipt.
// Note this recomputes the bloom filter of the receipt.
func (r *Receipt) encodeForNetwork68(buf *receiptListBuffers, w *rlp.EncoderBuffer) {
	writeInner := func(w *rlp.EncoderBuffer) {
		list := w.List()
		w.WriteBytes(r.PostStateOrStatus)
		w.WriteUint64(r.GasUsed)
		bloom := r.bloom(&buf.bloom)
		w.WriteBytes(bloom[:])
		w.Write(r.Logs)
		w.ListEnd(list)
	}

	if r.TxType == 0 {
		writeInner(w)
	} else {
		buf.tmp.Reset()
		buf.tmp.WriteByte(r.TxType)
		buf.enc.Reset(&buf.tmp)
		writeInner(&buf.enc)
		buf.enc.Flush()
		w.WriteBytes(buf.tmp.Bytes())
	}
}

// encodeForHash68 encodes a receipt for the block receiptsRoot derivation (eth/68).
func (r *Receipt) encodeForHash68(buf *receiptListBuffers, out *bytes.Buffer) {
	// For typed receipts, add the tx type.
	if r.TxType != 0 {
		out.WriteByte(r.TxType)
	}
	// Encode list = [postStateOrStatus, gasUsed, bloom, logs].
	w := &buf.enc
	w.Reset(out)
	l := w.List()
	w.WriteBytes(r.PostStateOrStatus)
	w.WriteUint64(r.GasUsed)
	bloom := r.bloom(&buf.bloom)
	w.WriteBytes(bloom[:])
	w.Write(r.Logs)
	w.ListEnd(l)
	w.Flush()
}

type receiptListBuffers struct {
	enc   rlp.EncoderBuffer
	bloom [6]byte
	tmp   bytes.Buffer
}

func initBuffers(buf **receiptListBuffers) {
	if *buf == nil {
		*buf = new(receiptListBuffers)
	}
}

// encodeForStorage encodes a list of receipts for the database.
func (buf *receiptListBuffers) encodeForStorage(rs rlp.RawList[Receipt], decode func([]byte, *Receipt) error) (rlp.RawValue, error) {
	var out bytes.Buffer
	w := &buf.enc
	w.Reset(&out)
	outer := w.List()
	it := rs.ContentIterator()
	for it.Next() {
		var receipt Receipt
		if err := decode(it.Value(), &receipt); err != nil {
			return nil, err
		}
		receipt.encodeForStorage(w)
	}
	if it.Err() != nil {
		return nil, fmt.Errorf("bad list: %v", it.Err())
	}
	w.ListEnd(outer)
	w.Flush()
	return out.Bytes(), nil
}

// ReceiptList68 is a block receipt list as downloaded by eth/68.
// This also implements types.DerivableList for validation purposes.
type ReceiptList68 struct {
	buf   *receiptListBuffers
	items rlp.RawList[Receipt]
}

// NewReceiptList68 creates a receipt list.
// This is slow, and exists for testing purposes.
func NewReceiptList68(trs []*types.Receipt) *ReceiptList68 {
	rl := new(ReceiptList68)
	initBuffers(&rl.buf)
	enc := rlp.NewEncoderBuffer(nil)
	for _, tr := range trs {
		r := newReceipt(tr)
		r.encodeForNetwork68(rl.buf, &enc)
		rl.items.AppendRaw(enc.ToBytes())
		enc.Reset(nil)
	}
	return rl
}

func blockReceiptsToNetwork68(blockReceipts, blockBody rlp.RawValue) ([]byte, error) {
	txTypesIter, err := txTypesInBody(blockBody)
	if err != nil {
		return nil, fmt.Errorf("invalid block body: %v", err)
	}
	nextTxType, stopTxTypes := iter.Pull(txTypesIter)
	defer stopTxTypes()

	var (
		out bytes.Buffer
		buf receiptListBuffers
	)
	blockReceiptIter, _ := rlp.NewListIterator(blockReceipts)
	w := rlp.NewEncoderBuffer(&out)
	outer := w.List()
	for i := 0; blockReceiptIter.Next(); i++ {
		txType, _ := nextTxType()
		var r Receipt
		if err := r.decodeDatabase(txType, blockReceiptIter.Value()); err != nil {
			return nil, fmt.Errorf("invalid database receipt %d: %v", i, err)
		}
		r.encodeForNetwork68(&buf, &w)
	}
	w.ListEnd(outer)
	w.Flush()
	return out.Bytes(), nil
}

// setBuffers implements ReceiptsList.
func (rl *ReceiptList68) setBuffers(buf *receiptListBuffers) {
	rl.buf = buf
}

// EncodeForStorage encodes the receipts for storage into the database.
func (rl *ReceiptList68) EncodeForStorage() (rlp.RawValue, error) {
	initBuffers(&rl.buf)
	return rl.buf.encodeForStorage(rl.items, func(data []byte, r *Receipt) error {
		return r.decode68(data)
	})
}

// Derivable turns the receipts into a list that can derive the root hash.
func (rl *ReceiptList68) Derivable() types.DerivableList {
	initBuffers(&rl.buf)
	return newDerivableRawList(&rl.items, func(data []byte, outbuf *bytes.Buffer) {
		var r Receipt
		if r.decode68(data) == nil {
			r.encodeForHash68(rl.buf, outbuf)
		}
	})
}

// DecodeRLP decodes a list of receipts from the network format.
func (rl *ReceiptList68) DecodeRLP(s *rlp.Stream) error {
	return rl.items.DecodeRLP(s)
}

// EncodeRLP encodes the list into the network format of eth/68.
func (rl *ReceiptList68) EncodeRLP(w io.Writer) error {
	return rl.items.EncodeRLP(w)
}
