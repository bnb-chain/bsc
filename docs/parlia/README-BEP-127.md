## BEP-127: Temporary Maintenance Mode for Validators

Temporary Maintenance is supposed to last one or a few hours. The validator seat will be temporarily dropped from the block producing rotation during the maintenance. Since long-time offline maintenance is not encouraged, the validator will still be slashed if the maintenance lasts too long. To lower the impact from poorly-operating validators who forget to claim its maintenance, they will be forced to enter Temporary Maintenance mode too.

- **enterMaintenance**: Validator can claim itself to enter scheduled maintenance by sending a transaction signed by the consensus key. 
- **exitMaintenance**: The validator can claim itself to exit maintenance by sending another transaction.

More details in [BEP-127](https://github.com/bnb-chain/BEPs/blob/master/BEP127.md).


## How to enter/exit maintenance

### Running `geth`
make sure you have unlocked the consensus address of your validator

### Running `built-in interactive`
```shell
$ geth attach geth.ipc
```

This command will:
* Start up `geth`'s built-in interactive [JavaScript console](https://geth.ethereum.org/docs/interacting-with-geth/javascript-console),
  (via the trailing `console` subcommand) through which you can interact using [`web3` methods](https://web3js.readthedocs.io/en/)
  (note: the `web3` version bundled within `geth` is very old, and not up to date with official docs),
  as well as `geth`'s own [management APIs](https://geth.ethereum.org/docs/interacting-with-geth/rpc).


### enter maintenance
```
web3.eth.sendTransaction({
   from: "consensus address of your validator",
   to: "0x0000000000000000000000000000000000001000",
   data: "0x9369d7de"
})
```

### exit maintenance
```
web3.eth.sendTransaction({
   from: "consensus address of your validator",
   to: "0x0000000000000000000000000000000000001000",
   data: "0x04c4fec6"
})
```



