package compiler

import (
	"os"

	ethlog "github.com/ethereum/go-ethereum/log"
)

// Package-wide debug switches for verbose logging in the MIR interpreter and opcode parser.
// Default is off to keep logs clean unless explicitly enabled by tests or callers.
var (
	// DebugLogsEnabled toggles all compiler debug logs (interpreter + parser)
	DebugLogsEnabled = false
	// MIRDebugLogsEnabled controls verbose logs inside the MIR interpreter
	MIRDebugLogsEnabled = false
	// ParserDebugLogsEnabled controls verbose logs inside the opcode parser / CFG builder
	ParserDebugLogsEnabled = false
)

func init() {
	if os.Getenv("MIR_DEBUG") == "1" || os.Getenv("MIR_DEBUG") == "true" {
		DebugLogsEnabled = true
		MIRDebugLogsEnabled = true
		ParserDebugLogsEnabled = true
	}
}

// EnableMIRDebugLogs toggles MIR interpreter debug logs at runtime.
func EnableMIRDebugLogs(on bool) { MIRDebugLogsEnabled = on }

// EnableParserDebugLogs toggles opcode parser/CFG debug logs at runtime.
func EnableParserDebugLogs(on bool) { ParserDebugLogsEnabled = on }

// EnableDebugLogs toggles all compiler debug logs (interpreter + parser)
func EnableDebugLogs(on bool) { DebugLogsEnabled = on }

func shouldLog() bool { return DebugLogsEnabled || MIRDebugLogsEnabled || ParserDebugLogsEnabled }

// mirDebugWarn emits a warning only if MIRDebugLogsEnabled is true.
func mirDebugWarn(msg string, ctx ...interface{}) {
	if shouldLog() {
		ethlog.Warn(msg, ctx...)
	}
}

// mirDebugError emits an error only if MIRDebugLogsEnabled is true.
func mirDebugError(msg string, ctx ...interface{}) {
	if shouldLog() {
		ethlog.Error(msg, ctx...)
	}
}

// parserDebugWarn emits a warning only if ParserDebugLogsEnabled is true.
func parserDebugWarn(msg string, ctx ...interface{}) {
	if shouldLog() {
		ethlog.Warn(msg, ctx...)
	}
}

// parserDebugError emits an error only if ParserDebugLogsEnabled is true.
func parserDebugError(msg string, ctx ...interface{}) {
	if shouldLog() {
		ethlog.Error(msg, ctx...)
	}
}

// Note: we deliberately avoid defining any identifier named `log` in this package
// to prevent collisions with files that import ethlog as `log`.
