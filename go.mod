module github.com/ethereum/go-ethereum

go 1.17

require (
	github.com/Azure/azure-storage-blob-go v0.7.0
	github.com/VictoriaMetrics/fastcache v1.6.0
	github.com/aws/aws-sdk-go-v2 v1.2.0
	github.com/aws/aws-sdk-go-v2/config v1.1.1
	github.com/aws/aws-sdk-go-v2/credentials v1.1.1
	github.com/aws/aws-sdk-go-v2/service/route53 v1.1.1
	github.com/btcsuite/btcd v0.22.0-beta
	github.com/cespare/cp v1.1.1
	github.com/cloudflare/cloudflare-go v0.14.0
	github.com/consensys/gnark-crypto v0.4.1-0.20210426202927-39ac3d4b3f1f
	github.com/davecgh/go-spew v1.1.1
	github.com/deckarep/golang-set v1.8.0
	github.com/deepmap/oapi-codegen v1.8.2 // indirect
	github.com/docker/docker v1.6.1
	github.com/dop251/goja v0.0.0-20220405120441-9037c2b61cbf
	github.com/edsrzf/mmap-go v1.0.0
	github.com/etcd-io/bbolt v1.3.3 // indirect
	github.com/fatih/color v1.9.0
	github.com/fjl/memsize v0.0.0-20190710130421-bcb5799ab5e5
	github.com/gballet/go-libpcsclite v0.0.0-20191108122812-4678299bea08
	github.com/go-stack/stack v1.8.0
	github.com/golang/protobuf v1.5.2
	github.com/golang/snappy v0.0.4
	github.com/google/gofuzz v1.2.0
	github.com/google/pprof v0.0.0-20220829040838-70bd9ae97f40
	github.com/google/uuid v1.3.0
	github.com/gorilla/websocket v1.4.2
	github.com/graph-gophers/graphql-go v1.3.0
	github.com/hashicorp/go-bexpr v0.1.10
	github.com/hashicorp/golang-lru v0.5.5-0.20210104140557-80c98217689d
	github.com/holiman/bloomfilter/v2 v2.0.3
	github.com/holiman/uint256 v1.2.0
	github.com/huin/goupnp v1.0.3
	github.com/influxdata/influxdb v1.8.3
	github.com/influxdata/influxdb-client-go/v2 v2.4.0
	github.com/jackpal/go-nat-pmp v1.0.2
	github.com/jedisct1/go-minisign v0.0.0-20190909160543-45766022959e
	github.com/jmhodges/levigo v1.0.0 // indirect
	github.com/julienschmidt/httprouter v1.3.0
	github.com/karalabe/usb v0.0.2
	github.com/logrusorgru/aurora v2.0.3+incompatible
	github.com/magiconair/properties v1.8.6 // indirect
	github.com/mattn/go-colorable v0.1.8
	github.com/mattn/go-isatty v0.0.14
	github.com/naoina/toml v0.1.2-0.20170918210437-9fafd6967416
	github.com/olekukonko/tablewriter v0.0.5
	github.com/panjf2000/ants/v2 v2.4.5
	github.com/peterh/liner v1.2.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/tsdb v0.10.0
	github.com/prysmaticlabs/prysm v0.0.0-20220124113610-e26cde5e091b
	github.com/rjeczalik/notify v0.9.1
	github.com/rs/cors v1.7.0
	github.com/shirou/gopsutil v3.21.4-0.20210419000835-c7a38de76ee5+incompatible
	github.com/status-im/keycard-go v0.0.0-20200402102358-957c09536969
	github.com/stretchr/testify v1.7.0
	github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7
	github.com/tendermint/go-amino v0.14.1
	github.com/tendermint/iavl v0.12.0
	github.com/tendermint/tendermint v0.31.12
	github.com/tidwall/wal v1.1.7
	github.com/tyler-smith/go-bip39 v1.1.0
	github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4 v1.1.3
	github.com/willf/bitset v1.1.3
	golang.org/x/crypto v0.0.0-20211117183948-ae814b36b871
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/sys v0.0.0-20220310020820-b874c991c1a5
	golang.org/x/text v0.3.7
	golang.org/x/time v0.0.0-20210220033141-f8bda1e9f3ba
	golang.org/x/tools v0.1.8
	gopkg.in/natefinch/npipe.v2 v2.0.0-20160621034901-c1b8fa8bdcce
	gopkg.in/urfave/cli.v1 v1.20.0
)

require (
	contrib.go.opencensus.io/exporter/jaeger v0.2.1 // indirect
	github.com/Azure/azure-pipeline-go v0.2.2 // indirect
	github.com/BurntSushi/toml v0.3.1 // indirect
	github.com/StackExchange/wmi v0.0.0-20210224194228-fe8f1750fd46 // indirect
	github.com/aristanetworks/goarista v0.0.0-20200521140103-6c3304613b30 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.0.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.0.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.1.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.1.1 // indirect
	github.com/aws/smithy-go v1.1.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.1.1 // indirect
	github.com/chzyer/readline v1.5.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.0 // indirect
	github.com/dgraph-io/ristretto v0.0.4-0.20210318174700-74754f61e018 // indirect
	github.com/dlclark/regexp2 v1.4.1-0.20201116162257-a2a8dda75c91 // indirect
	github.com/dustin/go-humanize v1.0.0 // indirect
	github.com/ferranbt/fastssz v0.0.0-20210905181407-59cf6761a7d5 // indirect
	github.com/fsnotify/fsnotify v1.4.9 // indirect
	github.com/go-kit/kit v0.10.0 // indirect
	github.com/go-logfmt/logfmt v0.5.0 // indirect
	github.com/go-logr/logr v0.2.1 // indirect
	github.com/go-ole/go-ole v1.2.5 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20200121045136-8c9f03a8e57e // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/google/go-cmp v0.5.5 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.2.2 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.0.1 // indirect
	github.com/herumi/bls-eth-go-binary v0.0.0-20210917013441-d37c07cfda4e // indirect
	github.com/influxdata/line-protocol v0.0.0-20210311194329-9aa0e372d097 // indirect
	github.com/json-iterator/go v1.1.11 // indirect
	github.com/juju/ansiterm v0.0.0-20180109212912-720a0952cc2a // indirect
	github.com/k0kubun/go-ansi v0.0.0-20180517002512-3bf9e2903213 // indirect
	github.com/klauspost/cpuid/v2 v2.0.9 // indirect
	github.com/konsorten/go-windows-terminal-sequences v1.0.3 // indirect
	github.com/lunixbochs/vtclean v1.0.0 // indirect
	github.com/manifoldco/promptui v0.7.0 // indirect
	github.com/mattn/go-ieproxy v0.0.0-20190702010315-6dee0af9227d // indirect
	github.com/mattn/go-runewidth v0.0.9 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.1 // indirect
	github.com/minio/highwayhash v1.0.1 // indirect
	github.com/minio/sha256-simd v1.0.0 // indirect
	github.com/mitchellh/colorstring v0.0.0-20190213212951-d06e56a500db // indirect
	github.com/mitchellh/mapstructure v1.4.1 // indirect
	github.com/mitchellh/pointerstructure v1.2.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/naoina/go-stringutil v0.1.0 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v1.11.0 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.30.0 // indirect
	github.com/prometheus/procfs v0.7.3 // indirect
	github.com/prysmaticlabs/eth2-types v0.0.0-20210303084904-c9735a06829d // indirect
	github.com/prysmaticlabs/go-bitfield v0.0.0-20210809151128-385d8c5e3fb7 // indirect
	github.com/prysmaticlabs/prombbolt v0.0.0-20210126082820-9b7adba6db7c // indirect
	github.com/rcrowley/go-metrics v0.0.0-20190826022208-cac0b30c2563 // indirect
	github.com/russross/blackfriday/v2 v2.0.1 // indirect
	github.com/schollz/progressbar/v3 v3.3.4 // indirect
	github.com/shurcooL/sanitized_anchor_name v1.0.0 // indirect
	github.com/sirupsen/logrus v1.6.0 // indirect
	github.com/supranational/blst v0.3.5 // indirect
	github.com/thomaso-mirodin/intmath v0.0.0-20160323211736-5dc6d854e46e // indirect
	github.com/tidwall/gjson v1.10.2 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	github.com/tidwall/tinylru v1.1.0 // indirect
	github.com/tklauser/go-sysconf v0.3.5 // indirect
	github.com/tklauser/numcpus v0.2.2 // indirect
	github.com/uber/jaeger-client-go v2.25.0+incompatible // indirect
	github.com/urfave/cli/v2 v2.3.0 // indirect
	github.com/wealdtech/go-bytesutil v1.1.1 // indirect
	github.com/wealdtech/go-eth2-types/v2 v2.5.2 // indirect
	github.com/wealdtech/go-eth2-util v1.6.3 // indirect
	go.etcd.io/bbolt v1.3.5 // indirect
	go.opencensus.io v0.23.0 // indirect
	golang.org/x/mod v0.5.1 // indirect
	golang.org/x/net v0.0.0-20211112202133-69e39bad7dc2 // indirect
	golang.org/x/oauth2 v0.0.0-20210514164344-f6687ab2804c // indirect
	golang.org/x/term v0.0.0-20201126162022-7de9c90e9dd1 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	google.golang.org/api v0.34.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20210426193834-eac7f76ac494 // indirect
	google.golang.org/grpc v1.40.0 // indirect
	google.golang.org/protobuf v1.27.1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	k8s.io/apimachinery v0.18.3 // indirect
	k8s.io/client-go v0.18.3 // indirect
	k8s.io/klog v1.0.0 // indirect
	k8s.io/klog/v2 v2.3.0 // indirect
	k8s.io/utils v0.0.0-20200520001619-278ece378a50 // indirect
	sigs.k8s.io/structured-merge-diff/v3 v3.0.0 // indirect
	sigs.k8s.io/yaml v1.2.0 // indirect
)

replace (
	github.com/ferranbt/fastssz => github.com/prysmaticlabs/fastssz v0.0.0-20220110145812-fafb696cae88
	github.com/gogo/protobuf v1.1.1 => github.com/gogo/protobuf v1.3.2
	github.com/gogo/protobuf v1.3.1 => github.com/gogo/protobuf v1.3.2
	github.com/grpc-ecosystem/grpc-gateway/v2 => github.com/prysmaticlabs/grpc-gateway/v2 v2.3.1-0.20210702154020-550e1cd83ec1
	github.com/tendermint/tendermint => github.com/bnb-chain/tendermint v0.31.12
)
