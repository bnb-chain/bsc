#!/usr/bin/env python3
"""Génère le genesis Coinbosa (Parlia/PoSA) à partir du genesis BSC de référence.

- reprend le bytecode des 10 contrats système (0x1000..0x2000)
- remplace le chainId, l'extraData (validateurs Coinbosa) et l'alloc initiale
"""
import json, sys

REF = "testnet-ref/testnet/genesis.json"
OUT = "genesis-coinbosa-parlia.json"

CHAIN_ID = 26262
VALIDATORS = ["3B78F3D76c6739c34872A34F9090cCb7607DD334"]  # sans 0x
TREASURY = "0x6941E70390d0B168D9b5439e349E4Af6D20E85Da"
FAUCET = "0x9a27D52a921c87905BC949bA221b0A55f8558c71"

# epoch volontairement très grand : le set de validateurs reste celui de
# l'extraData tant que les contrats système n'ont pas été régénérés avec
# bsc-genesis-contract (INIT_VALIDATORSET_BYTES est figé dans le bytecode).
PARLIA = {"period": 3, "epoch": 1000000000}

ref = json.load(open(REF))

# --- contrats système : on ne garde que le code ---
sys_alloc = {}
for addr, val in ref["alloc"].items():
    a = addr.lower().replace("0x", "")
    if a.startswith("0" * 35):  # 0x0000...1000 -> 0x0000...2000
        sys_alloc["0x" + a] = {k: v for k, v in val.items() if k in ("code", "balance", "storage")}

if len(sys_alloc) < 10:
    sys.exit(f"ERREUR: seulement {len(sys_alloc)} contrats système trouvés")

# --- extraData : 32B vanity + N*20B validateurs + 65B seal ---
vals = "".join(v + "00" * 48 for v in VALIDATORS)  # 20B adresse + 48B clé BLS
extra = "0x" + "00" * 32 + f"{len(VALIDATORS):02x}" + vals + "00" * 65

alloc = dict(sys_alloc)
alloc[TREASURY] = {"balance": "0x52b7d2dcc80cd2e4000000"}   # 100 000 000 CBA
alloc[FAUCET] = {"balance": "0x422ca8b0a00a425000000"}      #   5 000 000 CBA
alloc["0x" + VALIDATORS[0]] = {"balance": "0x21e19e0c9bab2400000"}  # 10 000 CBA

genesis = {
    "config": {
        "chainId": CHAIN_ID,
        "homesteadBlock": 0, "eip150Block": 0, "eip155Block": 0, "eip158Block": 0,
        "byzantiumBlock": 0, "constantinopleBlock": 0, "petersburgBlock": 0,
        "istanbulBlock": 0, "muirGlacierBlock": 0,
        "ramanujanBlock": 0, "nielsBlock": 0, "mirrorSyncBlock": 0, "brunoBlock": 0,
        "eulerBlock": 0, "gibbsBlock": 0, "nanoBlock": 0, "moranBlock": 0,
        "planckBlock": 0, "lubanBlock": 0, "platoBlock": 0,
        "berlinBlock": 0, "londonBlock": 0, "hertzBlock": 0, "hertzfixBlock": 0,
        "shanghaiTime": 0, "keplerTime": 0,
        "parlia": PARLIA,
    },
    "nonce": "0x0",
    "timestamp": "0x0",
    "extraData": extra,
    "gasLimit": "0x2625a00",
    "difficulty": "0x1",
    "mixHash": "0x" + "00" * 32,
    "coinbase": "0x" + "00" * 20,
    "alloc": alloc,
    "number": "0x0",
    "gasUsed": "0x0",
    "parentHash": "0x" + "00" * 32,
}

json.dump(genesis, open(OUT, "w"), indent=2)
print(f"{OUT} écrit")
print(f"  chainId          : {CHAIN_ID}")
print(f"  contrats système : {len(sys_alloc)}")
print(f"  validateurs      : {VALIDATORS}")
print(f"  extraData        : {(len(extra)-2)//2} octets")
print(f"  comptes alloués  : {len(alloc)}")
