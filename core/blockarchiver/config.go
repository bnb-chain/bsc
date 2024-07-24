package blockarchiver

type BlockArchiverConfig struct {
	RPCAddress     string
	SPAddress      string
	BucketName     string
	BlockCacheSize int64
}

var DefaultBlockArchiverConfig = BlockArchiverConfig{
	BlockCacheSize: 50000,
}
