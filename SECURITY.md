# Security Policy

## Supported Versions

Please see [Releases](https://github.com/bnb-chain/bsc/releases). We recommend using the [most recently released version](https://github.com/bnb-chain/bsc/releases/latest).

## Audit reports

Audit reports are published in the `docs` folder: https://github.com/bnb-chain/bsc/tree/master/docs/audits

| Scope  | Date     | Report Link                                                                                              |
| ------ | -------- | -------------------------------------------------------------------------------------------------------- |
| `geth` | 20170425 | [pdf](https://github.com/ethereum/go-ethereum/blob/master/docs/audits/2017-04-25_Geth-audit_Truesec.pdf) |
| `clef` | 20180914 | [pdf](https://github.com/ethereum/go-ethereum/blob/master/docs/audits/2018-09-14_Clef-audit_NCC.pdf) |
| `Discv5` | 20191015 | [pdf](https://github.com/ethereum/go-ethereum/blob/master/docs/audits/2019-10-15_Discv5_audit_LeastAuthority.pdf) |
| `Discv5` | 20200124 | [pdf](https://github.com/ethereum/go-ethereum/blob/master/docs/audits/2020-01-24_DiscV5_audit_Cure53.pdf) |

## Reporting a Vulnerability

**Please do not file a public ticket** mentioning the vulnerability.

To find out how to disclose a vulnerability in Ethereum visit [https://bugcrowd.com/binance](https://bugcrowd.com/binance) or email bounty@ethereum.org. Please read the [disclosure page](https://github.com/bnb-chain/bsc/security/advisories) for more information about publicly disclosed security vulnerabilities.

Use the built-in `geth version-check` feature to check whether the software is affected by any known vulnerability. This command will fetch the latest [`vulnerabilities.json`](https://geth.ethereum.org/docs/vulnerabilities/vulnerabilities.json) file which contains known security vulnerabilities concerning `geth`, and cross-check the data against its own version number.
