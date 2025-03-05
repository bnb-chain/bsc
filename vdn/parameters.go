package vdn

import "time"

const (
	GossipMaxSize = 10 * 1 << 20 // 10 MiB
	RespTimeout   = 10 * time.Second
)
