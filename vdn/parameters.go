package vdn

import "time"

const (
	GossipMaxSize = 10 * 1 << 20 // 10 MiB

	// RespTimeout is the maximum time for complete response transfer.
	RespTimeout = 10 * time.Second

	// Time to first byte timeout. The maximum time to wait for first byte of
	// request response (time-to-first-byte). The client is expected to give up if
	// they don't receive the first byte within 5 seconds.
	ttfbTimeout = 5 * time.Second
)
