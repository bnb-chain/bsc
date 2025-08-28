package common

import "sync/atomic"

// Global debug flags for cross-module debugging
var (
	// ParliaHashMismatchFlag indicates if there's a hash mismatch in parlia consensus
	// Use atomic operations to ensure thread safety
	parliaHashMismatchFlag int64
)

// SetParliaHashMismatch sets the parlia hash mismatch flag to true
func SetParliaHashMismatch() {
	atomic.StoreInt64(&parliaHashMismatchFlag, 1)
}

// IsParliaHashMismatch checks if parlia hash mismatch flag is set
func IsParliaHashMismatch() bool {
	return atomic.LoadInt64(&parliaHashMismatchFlag) == 1
}

// ResetParliaHashMismatch resets the parlia hash mismatch flag to false
func ResetParliaHashMismatch() {
	atomic.StoreInt64(&parliaHashMismatchFlag, 0)
}
