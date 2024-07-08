## Requirement

- nodejs >= v16.20.2
- npm >=  v8.19.4

## Prepare
Recommend use [nvm](https://github.com/nvm-sh/nvm) to manage node version.

Install node.js dependency:
```shell script
    npm install
```
## Run
### 1.Get Validator's Information: Version, MinGasPrice
mainnet validators version
```bash
    npm run startMainnet
```
testnet validators version
```bash
    npm run startTestnet
```

### 2.Get Transaction Count
```bash
node gettxcount.js --rpc ${url} --startNum ${start} --endNum ${end} --miner ${miner} (optional)
```

### 3. Get Performance
```bash
node get_perf.js --rpc ${url} --startNum ${start} --endNum ${end}
```
output as following
```bash
Get the performance between [ 19470 , 19670 )
txCountPerBlock = 3142.81 txCountTotal = 628562 BlockCount = 200 avgBlockTime = 3.005 inturnBlocksRatio = 0.975
txCountPerSecond = 1045.8602329450914 avgGasUsedPerBlock = 250.02062627 avgGasUsedPerSecond =  83.20153952412646
```

### 4. Get validators slash count
```bash
use the latest block 
node getslashcount.js --Rpc ${ArchiveRpc} 
use a block number
node getslashcount.js --Rpc ${ArchiveRpc} --Num ${blockNum}
```

