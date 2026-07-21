# Coinbosa Chain

Chaîne EVM souveraine dérivée de [BNB Smart Chain](https://github.com/bnb-chain/bsc), avec son
propre genesis, son propre réseau de validateurs et son standard de jeton **BOS20**.

Ce dossier contient tout ce qui est **propre à Coinbosa**. Le reste du dépôt est le client
BSC amont, laissé intact pour pouvoir suivre ses mises à jour.

| | |
|---|---|
| Nom du réseau | Coinbosa Chain |
| Chain ID | `26262` |
| Consensus | Parlia (Proof of Staked Authority) |
| Temps de bloc | 3 secondes |
| Standard de jeton | BOS20 |
| Client | `geth` v1.7.6, commit `69b3758c8` |

## Le jeton BOSA

| | |
|---|---|
| Nom | Coinbosa |
| Symbole | BOSA |
| Décimales | 10 |
| Offre initiale | 700 000 000 BOSA |
| Standard | BOS20 |

BOSA est un **jeton BOS20 déployé sur Coinbosa Chain**, et non le coin natif qui paie le gas.
C'est le même rapport qu'entre CAKE et BNB sur BNB Chain. Ce choix est ce qui permet les
10 décimales : sur une chaîne EVM, le coin natif est structurellement à 18 décimales, car
l'unité de base est le wei et cette valeur est câblée dans le calcul du gas et dans les wallets.

## Le standard BOS20

BOS20 est compatible ERC-20 : tout wallet, pont ou service qui parle ERC-20 fonctionne avec
un jeton BOS20 sans adaptation. S'y ajoutent :

- `getOwner()` — expose le propriétaire de manière standardisée pour les explorateurs
- `mint()` / `burn()` — émission réservée au propriétaire, destruction ouverte au porteur
- `increaseAllowance()` / `decreaseAllowance()` — évite la condition de course de `approve`
- autorisation infinie (`type(uint256).max`) non décrémentée, pour économiser du gas

```
contracts/
  IBOS20.sol      interface du standard
  BOS20.sol       implémentation de référence, réutilisable pour tout jeton
  BosaToken.sol   le jeton BOSA officiel
```

## Démarrage

```bash
npm install

# compiler les contrats
node scripts/compile.js

# lancer un nœud validateur local
./scripts/start-node.sh

# lancer l'explorateur sur http://127.0.0.1:8080
./scripts/start-explorer.sh
```

## Déployer BOSA

L'adresse de départ reçoit l'intégralité des 700 M BOSA et devient propriétaire du contrat.
Elle n'est écrite en dur nulle part : elle se passe au déploiement.

```bash
HOLDER=0xVotreAdresseDeDepart \
PRIVATE_KEY=0xCleDuCompteQuiPaieLeGas \
RPC=http://127.0.0.1:8545 \
node scripts/deploy-bosa.js
```

## Tests

La suite de tests s'exécute contre une vraie chaîne, pas contre un simulateur.

```bash
./scripts/start-node.sh &        # laisser le nœud démarrer
node scripts/compile.js
RPC=http://127.0.0.1:8545 node scripts/test-bos20.js
```

26 tests couvrent les métadonnées, les transferts, les autorisations, l'émission, la
destruction, la propriété et les événements — y compris les cas qui doivent échouer.

## État actuel, sans enjolivement

**Ce qui fonctionne, vérifié sur la chaîne :** production de blocs Parlia à 3 s, transactions
natives, déploiement et exécution de contrats, jeton BOS20 complet, API JSON-RPC et WebSocket,
persistance de l'état après redémarrage.

**Ce qui ne fonctionne pas encore :**

1. **La chaîne s'arrête au bloc 200.** C'est le premier bloc d'epoch : Parlia interroge alors
   `getMiningValidators()` sur le contrat système `0x…1000`, qui revert. La cause est que les
   contrats système proviennent du genesis de BNB Chain testnet et portent **le set de
   validateurs de Binance**, pas celui de Coinbosa. Ils doivent être régénérés depuis
   [`bsc-genesis-contract`](https://github.com/bnb-chain/bsc-genesis-contract) avec nos propres
   validateurs. C'est le chantier bloquant numéro un.

2. **Un seul validateur.** Aucune tolérance aux pannes, aucune sécurité byzantine. Il en faut
   au minimum 4, idéalement 7 à 21 sur des machines distinctes.

3. **Pas de finalité rapide.** Les clés BLS sont à zéro, le vote d'attestation est donc inactif.

4. **Forks postérieurs à Kepler non activés.** Solidity vise Cancun par défaut : compiler sans
   `evmVersion: 'shanghai'` produit l'opcode `MCOPY`, que la chaîne rejette.

5. **L'explorateur n'indexe rien.** Il interroge le RPC en direct : pas d'historique par
   adresse, pas de vérification de code source. Blockscout est la cible, mais sa version 11 et
   suivantes ne sont plus open source depuis le 22 avril 2026 — pour un produit rebrandable,
   il faut épingler la version `v10.2.6`, dernière sous GPLv3.

## Licence

Le client hérite de la licence de `bnb-chain/bsc` (LGPL-3.0 / GPL-3.0).
Les contrats et scripts de ce dossier sont sous licence MIT.
