package types

import (
	"bytes"
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/google/uuid"
	"math"
)

const (
	PaymentGasUsageBaseLine = 21000
	PuissantStatusReportURL = "https://explorer.48.club/api/v1/puissant_update"
)

var (
	PuiErrInvalidPayment = errors.New("puiErr: invalid payment")
	PuiErrTxNoRun        = errors.New("puiErr: tx no run")
	PuiErrTxConflict     = errors.New("puiErr: conflict")
)

type PuissantID [16]byte

func (p PuissantID) Bytes() []byte { return p[:] }

func (p PuissantID) Hex() string { return hexutil.Encode(p[:]) }

func (p PuissantID) String() string {
	return p.Hex()
}

func HexToPuissantID(s string) PuissantID { return BytesToPuissantID(common.FromHex(s)) }

func BytesToPuissantID(b []byte) PuissantID {
	var a PuissantID
	a.setBytes(b)
	return a
}

func (p PuissantID) IsPuissant() bool {
	return p != PuissantID{}
}

func (p PuissantID) setBytes(b []byte) {
	if len(b) > len(p) {
		b = b[len(b)-16:]
	}
	copy(p[16-len(b):], b)
}

// GenPuissantID generates a PuissantID from a list of transactions.
// Note! The transactions must set isAcceptReverting() before calling this function.
func GenPuissantID(txs []*Transaction) PuissantID {
	var msg bytes.Buffer
	msg.Grow(len(txs) * 33)

	for _, tx := range txs {
		msg.Write(tx.Hash().Bytes())
		if tx.AcceptsReverting() {
			msg.WriteByte(math.MaxUint8)
		} else {
			msg.WriteByte(0)
		}
	}

	return PuissantID(uuid.NewMD5(uuid.NameSpaceDNS, []byte(msg.String())))
}
