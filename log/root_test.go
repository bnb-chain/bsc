package log

import (
	"testing"
)

// TestSetDefaultCustomLogger should properly set the default logger when
// custom loggers are provided.
func TestSetDefaultCustomLogger(t *testing.T) {
	type customLogger struct {
		Logger // Implement the Logger interface
	}

	customLog := &customLogger{}
	SetDefault(customLog)
	if Root() != customLog {
		t.Error("expected custom logger to be set as default")
	}
}
