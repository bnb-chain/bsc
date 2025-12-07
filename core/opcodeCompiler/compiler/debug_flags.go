package compiler

import (
	"os"

	ethlog "github.com/ethereum/go-ethereum/log"
)

// Package-wide debug switch for verbose logging in the MIR compiler stack.
// Default is off to keep logs clean unless explicitly enabled by tests or callers.
var (
	// DebugLogsEnabled toggles all MIR-related compiler debug logs (interpreter + parser).
	DebugLogsEnabled = false
)

func init() {
	if os.Getenv("MIR_DEBUG") == "1" || os.Getenv("MIR_DEBUG") == "true" {
		DebugLogsEnabled = true
	}
}

// EnableMIRDebugLogs toggles all MIR-related compiler debug logs.
// This is the single public entrypoint for enabling verbose MIR logging.
func EnableMIRDebugLogs(on bool) { DebugLogsEnabled = on }

func shouldLog() bool { return DebugLogsEnabled }

// mirDebugWarn emits a warning only if debug logging is enabled.
func mirDebugWarn(msg string, ctx ...interface{}) {
	if shouldLog() {
		ethlog.Warn(msg, ctx...)
	}
}

// mirDebugError emits an error only if debug logging is enabled.
func mirDebugError(msg string, ctx ...interface{}) {
	if shouldLog() {
		ethlog.Error(msg, ctx...)
	}
}
