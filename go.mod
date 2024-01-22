module github.com/ethereum/go-ethereum

go 1.21

toolchain go1.21.5

require (
	github.com/Azure/azure-sdk-for-go/sdk/storage/azblob v0.3.0
	github.com/VictoriaMetrics/fastcache v1.12.1
	github.com/aws/aws-sdk-go-v2 v1.17.8
	github.com/aws/aws-sdk-go-v2/config v1.1.1
	github.com/aws/aws-sdk-go-v2/credentials v1.1.1
	github.com/aws/aws-sdk-go-v2/service/route53 v1.1.1
	github.com/bnb-chain/ics23 v0.1.0
	github.com/btcsuite/btcd/btcec/v2 v2.3.2
	github.com/cespare/cp v1.1.1
	github.com/cloudflare/cloudflare-go v0.14.0
	github.com/cockroachdb/errors v1.9.1
	github.com/cockroachdb/pebble v0.0.0-20230928194634-aa077af62593
	github.com/cometbft/cometbft v0.37.0
	github.com/consensys/gnark-crypto v0.12.1
	github.com/crate-crypto/go-kzg-4844 v0.7.0
	github.com/davecgh/go-spew v1.1.1
	github.com/deckarep/golang-set/v2 v2.1.0
	github.com/docker/docker v24.0.7+incompatible
	github.com/dop251/goja v0.0.0-20230806174421-c933cf95e127
	github.com/ethereum/c-kzg-4844 v0.4.0
	github.com/fatih/color v1.13.0
	github.com/fatih/structs v1.1.0
	github.com/fjl/gencodec v0.0.0-20230517082657-f9840df7b83e
	github.com/fjl/memsize v0.0.0-20190710130421-bcb5799ab5e5
	github.com/fsnotify/fsnotify v1.6.0
	github.com/gballet/go-libpcsclite v0.0.0-20191108122812-4678299bea08
	github.com/gballet/go-verkle v0.0.0-20230607174250-df487255f46b
	github.com/go-stack/stack v1.8.1
	github.com/gofrs/flock v0.8.1
	github.com/golang-jwt/jwt/v4 v4.5.0
	github.com/golang/protobuf v1.5.3
	github.com/golang/snappy v0.0.5-0.20220116011046-fa5810519dcb
	github.com/google/gofuzz v1.2.0
	github.com/google/pprof v0.0.0-20231023181126-ff6d637d2a7b
	github.com/google/uuid v1.3.0
	github.com/gorilla/websocket v1.5.0
	github.com/graph-gophers/graphql-go v1.3.0
	github.com/hashicorp/go-bexpr v0.1.10
	github.com/hashicorp/golang-lru v0.5.5-0.20210104140557-80c98217689d
	github.com/holiman/billy v0.0.0-20230718173358-1c7e68d277a7
	github.com/holiman/bloomfilter/v2 v2.0.3
	github.com/holiman/uint256 v1.2.3
	github.com/huin/goupnp v1.3.0
	github.com/influxdata/influxdb-client-go/v2 v2.4.0
	github.com/influxdata/influxdb1-client v0.0.0-20220302092344-a9ab5670611c
	github.com/jackpal/go-nat-pmp v1.0.2
	github.com/jedisct1/go-minisign v0.0.0-20190909160543-45766022959e
	github.com/julienschmidt/httprouter v1.3.0
	github.com/karalabe/usb v0.0.3-0.20230711191512-61db3e06439c
	github.com/kylelemons/godebug v1.1.0
	github.com/logrusorgru/aurora v2.0.3+incompatible
	github.com/mattn/go-colorable v0.1.13
	github.com/mattn/go-isatty v0.0.20
	github.com/naoina/toml v0.1.2-0.20170918210437-9fafd6967416
	github.com/olekukonko/tablewriter v0.0.5
	github.com/panjf2000/ants/v2 v2.4.5
	github.com/peterh/liner v1.2.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/tsdb v0.10.0
	github.com/protolambda/bls12-381-util v0.0.0-20220416220906-d8552aa452c7
	github.com/prysmaticlabs/prysm/v4 v4.2.0
	github.com/rs/cors v1.8.2
	github.com/shirou/gopsutil v3.21.11+incompatible
	github.com/status-im/keycard-go v0.2.0
	github.com/stretchr/testify v1.8.4
	github.com/supranational/blst v0.3.11
	github.com/syndtr/goleveldb v1.0.1
	github.com/tendermint/go-amino v0.14.1
	github.com/tendermint/iavl v0.12.0
	github.com/tendermint/tendermint v0.31.15
	github.com/tidwall/wal v1.1.7
	github.com/tyler-smith/go-bip39 v1.1.0
	github.com/urfave/cli/v2 v2.25.7
	github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4 v1.1.3
	github.com/willf/bitset v1.1.3
	go.uber.org/automaxprocs v1.5.2
	golang.org/x/crypto v0.17.0
	golang.org/x/exp v0.0.0-20231214170342-aacd6d4b4611
	golang.org/x/sync v0.5.0
	golang.org/x/sys v0.15.0
	golang.org/x/text v0.14.0
	golang.org/x/time v0.3.0
	golang.org/x/tools v0.16.0
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
	gopkg.in/natefinch/npipe.v2 v2.0.0-20160621034901-c1b8fa8bdcce
	gopkg.in/yaml.v3 v3.0.1
)

require (
	contrib.go.opencensus.io/exporter/jaeger v0.2.1 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azcore v0.21.1 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v0.8.3 // indirect
	github.com/BurntSushi/toml v1.3.2 // indirect
	github.com/DataDog/zstd v1.5.2 // indirect
	github.com/aristanetworks/goarista v0.0.0-20200805130819-fd197cf57d96 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.0.2 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.3.33 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.0.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.1.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.1.1 // indirect
	github.com/aws/smithy-go v1.13.5 // indirect
	github.com/benbjohnson/clock v1.3.5 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.7.0 // indirect
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/chzyer/readline v1.5.1 // indirect
	github.com/cockroachdb/logtags v0.0.0-20230118201751-21c54148d20b // indirect
	github.com/cockroachdb/redact v1.1.3 // indirect
	github.com/cockroachdb/tokenbucket v0.0.0-20230807174530-cc333fc44b06 // indirect
	github.com/consensys/bavard v0.1.13 // indirect
	github.com/containerd/cgroups v1.1.0 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/cosmos/gogoproto v1.4.1 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.2 // indirect
	github.com/crate-crypto/go-ipa v0.0.0-20230601170251-1830d0757c80 // indirect
	github.com/davidlazar/go-crypto v0.0.0-20200604182044-b73af7476f6c // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.2.0 // indirect
	github.com/deepmap/oapi-codegen v1.8.2 // indirect
	github.com/dgraph-io/ristretto v0.0.4-0.20210318174700-74754f61e018 // indirect
	github.com/dlclark/regexp2 v1.7.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/dustin/go-humanize v1.0.0 // indirect
	github.com/elastic/gosigar v0.14.2 // indirect
	github.com/etcd-io/bbolt v1.3.3 // indirect
	github.com/ferranbt/fastssz v0.0.0-20210905181407-59cf6761a7d5 // indirect
	github.com/flynn/noise v1.0.0 // indirect
	github.com/francoispqt/gojay v1.2.13 // indirect
	github.com/garslo/gogen v0.0.0-20170306192744-1d203ffc1f61 // indirect
	github.com/getsentry/sentry-go v0.18.0 // indirect
	github.com/go-kit/kit v0.12.0 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.5.1 // indirect
	github.com/go-logr/logr v1.2.4 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.13.0 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/go-task/slim-sprig v0.0.0-20230315185526-52ccab3ef572 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/gopacket v1.1.19 // indirect
	github.com/gorilla/mux v1.8.0 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.0.1 // indirect
	github.com/gtank/merlin v0.1.1 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.5 // indirect
	github.com/herumi/bls-eth-go-binary v0.0.0-20210917013441-d37c07cfda4e // indirect
	github.com/influxdata/line-protocol v0.0.0-20210311194329-9aa0e372d097 // indirect
	github.com/ipfs/go-cid v0.4.1 // indirect
	github.com/ipfs/go-log/v2 v2.5.1 // indirect
	github.com/jbenet/go-temp-err-catcher v0.1.0 // indirect
	github.com/jmhodges/levigo v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/juju/ansiterm v0.0.0-20180109212912-720a0952cc2a // indirect
	github.com/k0kubun/go-ansi v0.0.0-20180517002512-3bf9e2903213 // indirect
	github.com/kilic/bls12-381 v0.1.0 // indirect
	github.com/klauspost/compress v1.17.2 // indirect
	github.com/klauspost/cpuid/v2 v2.2.5 // indirect
	github.com/koron/go-ssdp v0.0.4 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/leodido/go-urn v1.2.3 // indirect
	github.com/libp2p/go-buffer-pool v0.1.0 // indirect
	github.com/libp2p/go-flow-metrics v0.1.0 // indirect
	github.com/libp2p/go-libp2p v0.32.1 // indirect
	github.com/libp2p/go-libp2p-asn-util v0.4.1 // indirect
	github.com/libp2p/go-libp2p-mplex v0.9.0 // indirect
	github.com/libp2p/go-libp2p-pubsub v0.10.0 // indirect
	github.com/libp2p/go-mplex v0.7.0 // indirect
	github.com/libp2p/go-msgio v0.3.0 // indirect
	github.com/libp2p/go-nat v0.2.0 // indirect
	github.com/libp2p/go-netroute v0.2.1 // indirect
	github.com/libp2p/go-reuseport v0.4.0 // indirect
	github.com/libp2p/go-yamux/v4 v4.0.1 // indirect
	github.com/lunixbochs/vtclean v1.0.0 // indirect
	github.com/manifoldco/promptui v0.7.0 // indirect
	github.com/marten-seemann/tcp v0.0.0-20210406111302-dfbc87cc63fd // indirect
	github.com/mattn/go-runewidth v0.0.14 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/miekg/dns v1.1.56 // indirect
	github.com/mikioh/tcpinfo v0.0.0-20190314235526-30a79bb1804b // indirect
	github.com/mikioh/tcpopt v0.0.0-20190314235656-172688c1accc // indirect
	github.com/mimoo/StrobeGo v0.0.0-20210601165009-122bf33a46e0 // indirect
	github.com/minio/highwayhash v1.0.2 // indirect
	github.com/minio/sha256-simd v1.0.1 // indirect
	github.com/mitchellh/colorstring v0.0.0-20190213212951-d06e56a500db // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/mitchellh/pointerstructure v1.2.0 // indirect
	github.com/mmcloughlin/addchain v0.4.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/mr-tron/base58 v1.2.0 // indirect
	github.com/multiformats/go-base32 v0.1.0 // indirect
	github.com/multiformats/go-base36 v0.2.0 // indirect
	github.com/multiformats/go-multiaddr v0.12.1 // indirect
	github.com/multiformats/go-multiaddr-dns v0.3.1 // indirect
	github.com/multiformats/go-multiaddr-fmt v0.1.0 // indirect
	github.com/multiformats/go-multibase v0.2.0 // indirect
	github.com/multiformats/go-multicodec v0.9.0 // indirect
	github.com/multiformats/go-multihash v0.2.3 // indirect
	github.com/multiformats/go-multistream v0.5.0 // indirect
	github.com/multiformats/go-varint v0.0.7 // indirect
	github.com/naoina/go-stringutil v0.1.0 // indirect
	github.com/onsi/ginkgo/v2 v2.13.0 // indirect
	github.com/opencontainers/runtime-spec v1.1.0 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/pbnjay/memory v0.0.0-20210728143218-7b4eea64cf58 // indirect
	github.com/petermattis/goid v0.0.0-20180202154549-b0b1615b78e5 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v1.14.0 // indirect
	github.com/prometheus/client_model v0.4.0 // indirect
	github.com/prometheus/common v0.42.0 // indirect
	github.com/prometheus/procfs v0.9.0 // indirect
	github.com/prometheus/prom2json v1.3.0 // indirect
	github.com/prysmaticlabs/eth2-types v0.0.0-20210303084904-c9735a06829d // indirect
	github.com/prysmaticlabs/fastssz v0.0.0-20221107182844-78142813af44 // indirect
	github.com/prysmaticlabs/go-bitfield v0.0.0-20210809151128-385d8c5e3fb7 // indirect
	github.com/prysmaticlabs/gohashtree v0.0.3-alpha // indirect
	github.com/prysmaticlabs/prombbolt v0.0.0-20210126082820-9b7adba6db7c // indirect
	github.com/prysmaticlabs/prysm v0.0.0-20220124113610-e26cde5e091b // indirect
	github.com/quic-go/qpack v0.4.0 // indirect
	github.com/quic-go/qtls-go1-20 v0.3.4 // indirect
	github.com/quic-go/quic-go v0.39.3 // indirect
	github.com/quic-go/webtransport-go v0.6.0 // indirect
	github.com/raulk/go-watchdog v1.3.0 // indirect
	github.com/rcrowley/go-metrics v0.0.0-20201227073835-cf1acfcdf475 // indirect
	github.com/rivo/uniseg v0.4.4 // indirect
	github.com/rogpeppe/go-internal v1.9.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sasha-s/go-deadlock v0.3.1 // indirect
	github.com/schollz/progressbar/v3 v3.3.4 // indirect
	github.com/sirupsen/logrus v1.9.0 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/afero v1.10.0 // indirect
	github.com/thomaso-mirodin/intmath v0.0.0-20160323211736-5dc6d854e46e // indirect
	github.com/tidwall/gjson v1.10.2 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	github.com/tidwall/tinylru v1.1.0 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/trailofbits/go-mutexasserts v0.0.0-20230328101604-8cdbc5f3d279 // indirect
	github.com/uber/jaeger-client-go v2.25.0+incompatible // indirect
	github.com/wealdtech/go-bytesutil v1.1.1 // indirect
	github.com/wealdtech/go-eth2-types/v2 v2.5.2 // indirect
	github.com/wealdtech/go-eth2-util v1.6.3 // indirect
	github.com/xrash/smetrics v0.0.0-20201216005158-039620a65673 // indirect
	github.com/yusufpapurcu/wmi v1.2.2 // indirect
	go.etcd.io/bbolt v1.3.7 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.uber.org/dig v1.17.1 // indirect
	go.uber.org/fx v1.20.1 // indirect
	go.uber.org/mock v0.3.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.26.0 // indirect
	golang.org/x/mod v0.14.0 // indirect
	golang.org/x/net v0.19.0 // indirect
	golang.org/x/oauth2 v0.7.0 // indirect
	golang.org/x/term v0.15.0 // indirect
	google.golang.org/api v0.44.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20230410155749-daa745c078e1 // indirect
	google.golang.org/grpc v1.56.3 // indirect
	google.golang.org/protobuf v1.30.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/apimachinery v0.20.0 // indirect
	k8s.io/client-go v0.20.0 // indirect
	k8s.io/klog/v2 v2.80.0 // indirect
	k8s.io/utils v0.0.0-20201110183641-67b214c5f920 // indirect
	lukechampine.com/blake3 v1.2.1 // indirect
	rsc.io/tmplfunc v0.0.3 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.0.2 // indirect
	sigs.k8s.io/yaml v1.2.0 // indirect
)

replace (
	github.com/btcsuite/btcd => github.com/btcsuite/btcd v0.23.0
	github.com/cometbft/cometbft => github.com/bnb-chain/greenfield-tendermint v0.0.0-20230417032003-4cda1f296fb2
	github.com/grpc-ecosystem/grpc-gateway/v2 => github.com/prysmaticlabs/grpc-gateway/v2 v2.3.1-0.20210702154020-550e1cd83ec1
	github.com/syndtr/goleveldb v1.0.1 => github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7
	github.com/tendermint/tendermint => github.com/bnb-chain/tendermint v0.31.15
)
