[bsc readme](README.original.md)

# BSC Builder

This project implements the BEP-322: Builder API Specification for BNB Smart Chain.

This project represents a minimal implementation of the protocol and is provided as is. We make no guarantees regarding its functionality or security.

See also: https://github.com/bnb-chain/BEPs/pull/322

# Usage

Builder-related settings are configured in the `config.toml` file. The following is an example of a `config.toml` file:

```
[Eth.Miner.Mev]
Enabled = false
ValidatorCommission = 100
BidSimulationLeftOver = 50
BuilderEnabled = true
BuilderAccount = {{BUILDER_ADDRESS}}

[[Eth.Miner.Mev.Validators]]
Address = {{VALIDATOR_ADDRESS}}
URL = {{VALIDATOR_URL}}
...
```

- `Enabled`: Whether to enable validator mev.
- `BuilderEnabled`: Whether to enable the builder mev.
- `BuilderAccount`: The account address to unlock of the builder.
- `BidSimulationLeftOver`: The left over of the bid simulation.
- `Validators`: A list of validators to bid for.
  - `Address`: The address of the validator.
  - `URL`: The URL of the validator.

## License

The bsc library (i.e. all code outside of the `cmd` directory) is licensed under the
[GNU Lesser General Public License v3.0](https://www.gnu.org/licenses/lgpl-3.0.en.html),
also included in our repository in the `COPYING.LESSER` file.

The bsc binaries (i.e. all code inside of the `cmd` directory) is licensed under the
[GNU General Public License v3.0](https://www.gnu.org/licenses/gpl-3.0.en.html), also
included in our repository in the `COPYING` file.
