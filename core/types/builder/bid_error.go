package builder

import "errors"

const (
	InvalidBidParamError           = -38001
	InvalidPayBidTxError           = -38002
	MevNotRunningError             = -38003
	MevBusyError                   = -38004
	MevNotInTurnError              = -38005
	BidBlockPermissionRevokedError = -38006
	BidBlockPreSealVerifyError     = -38007
	BidBlockTooLateError           = -38008
)

var (
	ErrMevNotRunning = newBidError(errors.New("the validator stop accepting bids for now, try again later"), MevNotRunningError)
	ErrMevBusy       = newBidError(errors.New("the validator is working on too many bids, try again later"), MevBusyError)
	ErrMevNotInTurn  = newBidError(errors.New("the validator is not in-turn to propose currently, try again later"), MevNotInTurnError)
)

// bidError is an API error that encompasses an invalid bid with JSON error
// code and a binary data blob.
type bidError struct {
	error
	code int
}

// ErrorCode returns the JSON error code for an invalid bid.
// See: https://github.com/ethereum/wiki/wiki/JSON-RPC-Error-Codes-Improvement-Proposal
func (e *bidError) ErrorCode() int {
	return e.code
}

func NewInvalidBidError(message string) *bidError {
	return newBidError(errors.New(message), InvalidBidParamError)
}

func NewInvalidPayBidTxError(message string) *bidError {
	return newBidError(errors.New(message), InvalidPayBidTxError)
}

func NewBidBlockPermissionRevokedError(message string) *bidError {
	return newBidError(errors.New(message), BidBlockPermissionRevokedError)
}

func NewBidBlockPreSealVerifyError(message string) *bidError {
	return newBidError(errors.New(message), BidBlockPreSealVerifyError)
}

func NewBidBlockTooLateError(message string) *bidError {
	return newBidError(errors.New(message), BidBlockTooLateError)
}

func newBidError(err error, code int) *bidError {
	return &bidError{
		error: err,
		code:  code,
	}
}
