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
mainnet validators version
```bash
    npm run startMainnet
```
testnet validators version
```bash
    npm run startTestnet
```
Transaction count
```bash
node gettxcount.js --rpc ${url} --startNum ${start} --endNum ${end} --miner ${miner} (optional)
```