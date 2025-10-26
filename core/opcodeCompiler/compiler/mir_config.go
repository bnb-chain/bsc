package compiler

// MirBestEffort, when enabled, relaxes strict stack checks during CFG build
// and attempts to continue despite local under/overflow, producing a partial CFG.
var MirBestEffort bool = false

// SetMirBestEffort sets best-effort mode for MIR CFG generation.
func SetMirBestEffort(enable bool) { MirBestEffort = enable }
