# 1.Background
This is to support some projects with customized tokens that they want to integrate into the BSC faucet tool.

## 1.1. How to Integrate Your Token
- Step 1: Submmit a Pull Request to [bsc develop branch](https://github.com/bnb-chain/bsc/tree/develop) with relevant token information: `symbol`, `amount`, `icon`, `addr`. Append these information in [Section 2: Token List](#2token-list)
- Step 2: Wait for approval, we will review the request, and once it is approved, the faucet tool will start to support the customized token and list it on https://www.bnbchain.org/en/testnet-faucet.
- Step 3: Deposit your test token to designated address(0xaa25aa7a19f9c426e07dee59b12f944f4d9f1dd3) on the BSC testnet.

# 2.Token List
## 2.1.DemoToken
- symbol: DEMO
- amount: 10000000000000000000
- icon: ./demotoken.png
- addr: https://testnet.bscscan.com/address/0xe15c158d768c306dae87b96430a94f884333e55d
- fundTx:
