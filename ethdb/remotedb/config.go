package remotedb

import (
	"time"

	rocks "github.com/go-redis/redis/v8"
)

var (
	// read timeout
	REMOTEDB_BATCH_READ_TIMEOUT = 6
	// write timeout
	REMOTEDB_BATCH_WRITE_TIMEOUT = 2 * REMOTEDB_BATCH_READ_TIMEOUT
	// retry min timeout
	REMOTEDB_RETRY_MIN_TIMEOUT = 2 * REMOTEDB_BATCH_READ_TIMEOUT
	// retry max timeout
	REMOTEDB_RETRY_MAX_TIMEOUT = 2 * REMOTEDB_BATCH_WRITE_TIMEOUT
	// iterator timeout
	REMOTEDB_ITERATOR_TIMEOUT = 1 * REMOTEDB_BATCH_READ_TIMEOUT
	// idle conns for each cluster node, save time establishing conns
	REMOTEDB_IDLE_CONNS = 2
)

// Remotedb Client Option
type Config struct {
	// clsuter addrs
	Addrs []string `toml:",omitempty"`
	// timeout
	ReadTimeout     int `toml:",omitempty"`
	WriteTimeout    int `toml:",omitempty"`
	MinRetryBackoff int `toml:",omitempty"`
	MaxRetryBackoff int `toml:",omitempty"`
	// idle conns for each node
	MinIdleConns int `toml:",omitempty"`
}

// DefaultConfig return default remotedb cuslter config, start parameter will cover default
func DefaultConfig() *Config {
	return &Config{
		ReadTimeout:     REMOTEDB_BATCH_WRITE_TIMEOUT,
		WriteTimeout:    REMOTEDB_BATCH_READ_TIMEOUT,
		MinRetryBackoff: REMOTEDB_RETRY_MIN_TIMEOUT,
		MaxRetryBackoff: REMOTEDB_RETRY_MAX_TIMEOUT,
		MinIdleConns:    REMOTEDB_IDLE_CONNS,
	}
}

// GetClusterOption return remote store cluster options from config
func (cfg *Config) GetClusterOption() *rocks.ClusterOptions {
	return &rocks.ClusterOptions{
		Addrs:           cfg.Addrs,
		ReadTimeout:     time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout:    time.Duration(cfg.WriteTimeout) * time.Second,
		MinRetryBackoff: time.Duration(cfg.MinRetryBackoff) * time.Second,
		MaxRetryBackoff: time.Duration(cfg.MaxRetryBackoff) * time.Second,
		MinIdleConns:    cfg.MinIdleConns,
	}
}

// GetIteratorOption return single node option for iterator
func (cfg *Config) GetIteratorOption(addr string) *rocks.Options {
	return &rocks.Options{
		Addr:            addr,
		ReadTimeout:     time.Duration(REMOTEDB_ITERATOR_TIMEOUT) * time.Second,
		MinRetryBackoff: time.Duration(cfg.MinRetryBackoff) * time.Second,
		MaxRetryBackoff: time.Duration(cfg.MaxRetryBackoff) * time.Second,
		MinIdleConns:    cfg.MinIdleConns,
	}
}
