package remotedb

import (
	"time"

	rocks "github.com/go-redis/redis/v8"
)

var (
	// read timeout
	REMOTEDB_BATCH_READ_TIMEOUT  = 6 * time.Second
	// write timeout
	REMOTEDB_BATCH_WRITE_TIMEOUT = 2 * REMOTEDB_BATCH_READ_TIMEOUT
	// retry min timeout
	REMOTEDB_RETRY_MIN_TIMEOUT   = 2 * REMOTEDB_BATCH_READ_TIMEOUT
	// retry max timeout
	REMOTEDB_RETRY_MAX_TIMEOUT   = 2 * REMOTEDB_BATCH_WRITE_TIMEOUT
	// iterator timeout
	REMOTEDB_ITERATOR_TIMEOUT    = 1 * REMOTEDB_BATCH_READ_TIMEOUT
	// idle conns for each cluster node, save time establishing conns
	REMOTEDB_IDLE_CONNS          = 2
)

// remotedb config 
type Config struct{
	Addrs           []string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	MinRetryBackoff time.Duration
	MaxRetryBackoff time.Duration
	MinIdleConns    int
}

// DefaultConfig return default remotedb cuslter config, start parameter will cover default
func DefaultConfig() *Config {
	return &Config {
		ReadTimeout:     REMOTEDB_BATCH_WRITE_TIMEOUT,
		WriteTimeout:    REMOTEDB_BATCH_READ_TIMEOUT,
		MinRetryBackoff: REMOTEDB_RETRY_MIN_TIMEOUT,
		MaxRetryBackoff: REMOTEDB_RETRY_MAX_TIMEOUT,
		MinIdleConns:    REMOTEDB_IDLE_CONNS,
	}
}

// GetClusterOption return remote store cluster options from config
func (cfg *Config) GetClusterOption() *rocks.ClusterOptions {
	return &rocks.ClusterOptions {
		Addrs:           cfg.Addrs,
		ReadTimeout:     cfg.ReadTimeout,
		WriteTimeout:    cfg.WriteTimeout,
		MinRetryBackoff: cfg.MinRetryBackoff,
		MaxRetryBackoff: cfg.MaxRetryBackoff,
		MinIdleConns:    cfg.MinIdleConns,
	}
}

// GetIteratorOption return single node option for iterator
func (cfg *Config) GetIteratorOption(addr string) *rocks.Options {
	return &rocks.Options {
		Addr:            addr,
		ReadTimeout:     REMOTEDB_ITERATOR_TIMEOUT,
		MinRetryBackoff: cfg.MinRetryBackoff,
		MaxRetryBackoff: cfg.MaxRetryBackoff,
		MinIdleConns:    cfg.MinIdleConns,
	}
}