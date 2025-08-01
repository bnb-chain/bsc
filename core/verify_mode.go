package core

import (
	"fmt"
)

type VerifyMode uint32

const (
	LocalVerify VerifyMode = iota
	NoneVerify
)

func (mode VerifyMode) IsValid() bool {
	return mode >= LocalVerify && mode <= NoneVerify
}

func (mode VerifyMode) String() string {
	switch mode {
	case LocalVerify:
		return "local"
	case NoneVerify:
		return "none"
	default:
		return "unknown"
	}
}

func (mode VerifyMode) MarshalText() ([]byte, error) {
	switch mode {
	case LocalVerify:
		return []byte("local"), nil
	case NoneVerify:
		return []byte("none"), nil
	default:
		return nil, fmt.Errorf("unknown verify mode %d", mode)
	}
}

func (mode *VerifyMode) UnmarshalText(text []byte) error {
	switch string(text) {
	case "local":
		*mode = LocalVerify
	case "none":
		*mode = NoneVerify
	default:
		return fmt.Errorf(`unknown sync mode %q, want "local" or "none"`, text)
	}
	return nil
}

func (mode VerifyMode) NoTries() bool {
	return mode != LocalVerify
}
