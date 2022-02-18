# Usage 
## Top Address
### Environment Flag
```
METRICS_TOP_ADDRESS_ENABLED=true geth ...
```
### Http Endpoints
```
http://localhost:6001/topEOA/{topN}
http://localhost:6001/topContract/{topN}
```
### Example Output
```json
{"map":{"0x094eC87475aACa970B57d5B17CC2fc1b7A005490":2030,"0x0a10Fce605fdd59FFB4a4E15d932bAac05e4689d":7481,"0x23FE208B228b8938613987cEaC3e34e75F590283":2041,"0x428348c907F53c4FB72229A394D153Af0694A1d9":3921,"0x51383c0060C6Dc212071D7Ad063C64C7f7A163Cf":3031,"0x631Fc1EA2270e98fbD9D92658eCe0F5a269Aa161":13660,"0x833E8b8d6E45246b851e1fA89d109F8429F3006c":3208,"0xA3fd5cC5BA356433b28209d812Ff0Cf261881e1B":2062,"0xDe31acB1217E08139a655B5572B58d2d65acd87c":2857,"0xEF575087F1e7BeC54046F98119c8C392a37c51dd":16263},"start":"2022-02-15T17:00:50.797885+08:00","end":"2022-02-15T17:11:49.577221+08:00"}
```

## Main Process (Mining, Importing)
### Environment Flag
```
METRICS_MP_METRICS_ENABLED=true geth ...
```


## Go Trace
### Environment Flag
```
METRICS_TOP_ADDRESS_ENABLED=true geth ...
```
### Http Endpoints
```
http://localhost:6002/trace/start?duration={seconds}
```
A file will be generated with name trace_{unixTimestamp}.out for analysis.


## Go Lock
### Environment Flag
```
METRICS_GO_LOCK_ENABLED=true geth ...
```
To analysis the mutex, you call download the profile and analysis it with pprof.
```
//to look at the holders of contended mutexes
go tool pprof http://localhost:6060/debug/pprof/mutex
```

Get multi profiles in a pre-defined intervals (e.g., 300 seconds).
```
./multi_pprof.sh http://localhost:6060/debug/pprof/mutex 300 mutux_
```

## Go GC
```
go tool pprof http://localhost:6060/debug/pprof/heap
```