package log

import (
	"sync/atomic"

	"golang.org/x/exp/slog"
)

// LoggerFilter is used to print log when check func returns true.
type LoggerFilter interface {
	check() bool
}

type EveryN struct {
	N       uint32
	counter uint32
}

func (e *EveryN) check() bool {
	if e == nil || e.N == 0 {
		return true
	}
	c := atomic.AddUint32(&e.counter, 1)
	return c%e.N == 0
}

var _ LoggerFilter = &EveryN{}

type ifCondition struct {
	Condition bool
}

func (i *ifCondition) check() bool {
	if i == nil || i.Condition {
		return true
	}
	return false
}

var _ LoggerFilter = &ifCondition{}

func TraceBy(filter LoggerFilter, msg string, ctx ...interface{}) {
	if filter == nil || filter.check() {
		Root().Write(LevelTrace, msg, ctx...)
	}
}

func DebugBy(filter LoggerFilter, msg string, ctx ...interface{}) {
	if filter == nil || filter.check() {
		Root().Write(slog.LevelDebug, msg, ctx...)
	}
}

func InfoBy(filter LoggerFilter, msg string, ctx ...interface{}) {
	if filter == nil || filter.check() {
		Root().Write(slog.LevelInfo, msg, ctx...)
	}
}

func WarnBy(filter LoggerFilter, msg string, ctx ...interface{}) {
	if filter == nil || filter.check() {
		Root().Write(slog.LevelWarn, msg, ctx...)
	}
}

func ErrorBy(filter LoggerFilter, msg string, ctx ...interface{}) {
	if filter == nil || filter.check() {
		Root().Write(slog.LevelError, msg, ctx...)
	}
}

func TraceIf(condition bool, msg string, ctx ...interface{}) {
	filter := &ifCondition{condition}
	TraceBy(filter, msg, ctx...)
}

func DebugIf(condition bool, msg string, ctx ...interface{}) {
	filter := &ifCondition{condition}
	DebugBy(filter, msg, ctx...)
}

func InfoIf(condition bool, msg string, ctx ...interface{}) {
	filter := &ifCondition{condition}
	InfoBy(filter, msg, ctx...)
}

func WarnIf(condition bool, msg string, ctx ...interface{}) {
	filter := &ifCondition{condition}
	WarnBy(filter, msg, ctx...)
}

func ErrorIf(condition bool, msg string, ctx ...interface{}) {
	filter := &ifCondition{condition}
	ErrorBy(filter, msg, ctx...)
}
