package bundlepool

import (
	"time"

	"github.com/ethereum/go-ethereum/log"
)

type Config struct {
	PriceLimit uint64 // Minimum gas price to enforce for acceptance into the pool
	PriceBump  uint64 // Minimum price bump percentage to replace an already existing transaction (nonce)

	GlobalSlots     uint64 // Maximum number of bundle slots for all accounts
	GlobalQueue     uint64 // Maximum number of non-executable bundle slots for all accounts
	MaxBundleBlocks uint64 // Maximum number of blocks for calculating MinimalBundleGasPrice

	BundleGasPricePercentile      uint8         // Percentile of the recent minimal mev gas price
	BundleGasPricerExpireTime     time.Duration // Store time duration amount of recent mev gas price
	UpdateBundleGasPricerInterval time.Duration // Time interval to update MevGasPricePool
}

// DefaultConfig contains the default configurations for the bundle pool.
var DefaultConfig = Config{
	PriceLimit: 1,
	PriceBump:  10,

	GlobalSlots: 4096 + 1024, // urgent + floating queue capacity with 4:1 ratio
	GlobalQueue: 1024,

	MaxBundleBlocks:               50,
	BundleGasPricePercentile:      90,
	BundleGasPricerExpireTime:     time.Minute,
	UpdateBundleGasPricerInterval: time.Second,
}

// sanitize checks the provided user configurations and changes anything that's
// unreasonable or unworkable.
func (config *Config) sanitize() Config {
	conf := *config
	if conf.PriceLimit < 1 {
		log.Warn("Sanitizing invalid txpool price limit", "provided", conf.PriceLimit, "updated", DefaultConfig.PriceLimit)
		conf.PriceLimit = DefaultConfig.PriceLimit
	}
	if conf.PriceBump < 1 {
		log.Warn("Sanitizing invalid txpool price bump", "provided", conf.PriceBump, "updated", DefaultConfig.PriceBump)
		conf.PriceBump = DefaultConfig.PriceBump
	}
	if conf.GlobalSlots < 1 {
		log.Warn("Sanitizing invalid txpool bundle slots", "provided", conf.GlobalSlots, "updated", DefaultConfig.GlobalSlots)
		conf.GlobalSlots = DefaultConfig.GlobalSlots
	}
	if conf.GlobalQueue < 1 {
		log.Warn("Sanitizing invalid txpool global queue", "provided", conf.GlobalQueue, "updated", DefaultConfig.GlobalQueue)
		conf.GlobalQueue = DefaultConfig.GlobalQueue
	}
	if conf.MaxBundleBlocks < 1 {
		log.Warn("Sanitizing invalid txpool max bundle blocks", "provided", conf.MaxBundleBlocks, "updated", DefaultConfig.MaxBundleBlocks)
		conf.MaxBundleBlocks = DefaultConfig.MaxBundleBlocks
	}
	if conf.BundleGasPricePercentile >= 100 {
		log.Warn("Sanitizing invalid txpool bundle gas price percentile", "provided", conf.BundleGasPricePercentile, "updated", DefaultConfig.BundleGasPricePercentile)
		conf.BundleGasPricePercentile = DefaultConfig.BundleGasPricePercentile
	}
	if conf.BundleGasPricerExpireTime < 1 {
		log.Warn("Sanitizing invalid txpool bundle gas pricer expire time", "provided", conf.BundleGasPricerExpireTime, "updated", DefaultConfig.BundleGasPricerExpireTime)
		conf.BundleGasPricerExpireTime = DefaultConfig.BundleGasPricerExpireTime
	}
	if conf.UpdateBundleGasPricerInterval < time.Second {
		log.Warn("Sanitizing invalid txpool update BundleGasPricer interval", "provided", conf.UpdateBundleGasPricerInterval, "updated", DefaultConfig.UpdateBundleGasPricerInterval)
		conf.UpdateBundleGasPricerInterval = DefaultConfig.UpdateBundleGasPricerInterval
	}
	return conf
}
