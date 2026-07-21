<div align="center">
  <img src="assets/coinbosa-logo.jpg" alt="Coinbosa" width="170" />

  # Coinbosa Chain

  Chaîne EVM souveraine · Consensus Parlia · Standard de jeton BOS20
</div>

---

Coinbosa Chain est une blockchain compatible Ethereum, dérivée de
[BNB Smart Chain](https://github.com/bnb-chain/bsc), avec son propre genesis, son propre
réseau de validateurs et son standard de jeton.

Projet de **Coinbosa Inc** (Delaware).

Le choix d'une chaîne compatible EVM donne accès sans adaptation à l'outillage existant —
wallets, ponts, bibliothèques, explorateurs, auditeurs — là où une machine virtuelle
propriétaire imposerait de tout reconstruire.

Ce dossier contient tout ce qui est propre à Coinbosa. Le reste du dépôt est le client BSC
amont, laissé intact pour pouvoir suivre ses mises à jour.

---

## Le jeton BOSA

| | |
|---|---|
| Nom | Coinbosa |
| Symbole | BOSA |
| Décimales | 10 |
| Offre initiale | 700 000 000 BOSA |
| Standard | BOS20 |

BOSA est un **jeton BOS20 déployé sur** Coinbosa Chain, et non le coin natif qui paie le gas —
même rapport qu'entre CAKE et BNB sur BNB Chain. C'est ce choix qui rend les 10 décimales
possibles : sur une chaîne EVM, le coin natif est structurellement à 18 décimales, parce que
l'unité de base est le wei et que cette valeur est câblée dans le calcul du gas et dans les
wallets. La structure `core.Genesis` du client ne comporte d'ailleurs aucun champ nom, symbole
ou décimales.

L'intégralité de l'offre est émise à une **adresse de départ** fournie au déploiement. Elle
n'est écrite en dur nulle part dans le code.

---

## Le standard BOS20

BOS20 est compatible ERC-20 : tout wallet, pont ou service qui parle ERC-20 fonctionne avec un
jeton BOS20 sans adaptation. S'y ajoutent :

- `getOwner()` — expose le propriétaire de manière standardisée pour les explorateurs
- `mint()` / `burn()` — émission réservée au propriétaire, destruction ouverte au porteur
- `increaseAllowance()` / `decreaseAllowance()` — évite la condition de course de `approve`
- autorisation infinie (`type(uint256).max`) non décrémentée, pour économiser du gas

```
contracts/
  IBOS20.sol                 interface du standard
  BOS20.sol                  implémentation de référence, réutilisable pour tout jeton
  BosaToken.sol              le jeton BOSA officiel
  CoinbosaValidatorSet.sol   contrat système du consensus
```

---

## Le contrat système du consensus

Un fork de BSC hérite de dix contrats système pré-déployés aux adresses `0x…1000` à `0x…2000`.
Celui de `0x…1000` gouverne le consensus : Parlia lui demande la liste des validateurs à chaque
bloc d'epoch, tous les 200 blocs.

Repris tel quel depuis BNB Chain, ce contrat **arrête la chaîne au bloc 200**. Deux causes qui
se cumulent :

1. Le bytecode livré dans le genesis est la version d'origine de 2021. Sa table de dispatch ne
   contient pas `getMiningValidators()` — l'appel tombe sur un sélecteur inconnu, sans fonction
   de repli, et revert avec des données vides.
2. Sur le vrai BSC, cette fonction est injectée plus tard par le client Go, aux hardforks Euler
   et Luban. Ce mécanisme identifie le réseau par le **hash de son genesis**. Un genesis
   souverain est inconnu du client, le réseau est classé `Default`, et aucune table de mise à
   niveau n'a d'entrée pour `Default`. Le contrat reste donc figé en version 2021, à vie.

`CoinbosaValidatorSet.sol` le remplace : 5 758 octets contre 16 429, il implémente exactement
la surface d'appel que Parlia exige, et rien de plus. Aucune fonction du chemin consensus ne
peut revert — c'est la règle de conception, puisqu'un revert rend le bloc improduisible. Le set
de validateurs vit en storage et se met à jour par `updateValidatorSet()`, sans nouveau genesis.

**Contrepartie assumée :** on abandonne l'économie de BSC — staking, slashing, cross-chain,
gouvernance on-chain. Coinbosa est une chaîne à autorité parlant le protocole Parlia. Retrouver
l'économie complète suppose de régénérer les dix contrats de façon cohérente depuis
[`bsc-genesis-contract`](https://github.com/bnb-chain/bsc-genesis-contract).

---

## Caractéristiques du réseau

| | |
|---|---|
| Chain ID | `26262` |
| Consensus | Parlia (Proof of Staked Authority) |
| Temps de bloc | 3 secondes |
| Epoch | 200 blocs |
| Client | `geth` v1.7.6, commit `69b3758c8` |
| Compatibilité EVM | Shanghai |

Le temps de bloc et l'epoch ne se règlent **pas** dans le genesis. En v1.7.6, `ParliaConfig`
est une structure vide : ce sont des constantes Go pilotées par les hardforks — 200 blocs et
3 s par défaut, 500 et 1,5 s après Lorentz, 1000 et 0,75 s après Maxwell. Les tutoriels qui
recopient `period` et `epoch` dans le genesis sont périmés.

---

## Démarrage

```bash
npm install

# générer la clé du validateur, puis le genesis correspondant
./bin/coinbosa-geth account new --datadir node1
VALIDATOR=0xVotreValidateur node scripts/build-genesis.js

node scripts/compile.js      # compiler les contrats BOS20
./scripts/start-node.sh      # lancer le nœud validateur
./scripts/start-explorer.sh  # explorateur sur http://127.0.0.1:8080
```

### Déployer BOSA

L'adresse de départ reçoit l'intégralité des 700 M BOSA et devient propriétaire du contrat.

```bash
HOLDER=0xVotreAdresseDeDepart \
PRIVATE_KEY=0xCleDuCompteQuiPaieLeGas \
RPC=http://127.0.0.1:8545 \
node scripts/deploy-bosa.js
```

### Tests

La suite s'exécute contre une vraie chaîne, pas contre un simulateur.

```bash
RPC=http://127.0.0.1:8545 node scripts/test-bos20.js
```

26 tests couvrent les métadonnées, les transferts, les autorisations, l'émission, la
destruction, la propriété et les événements — y compris tous les cas qui doivent échouer.

---

## État d'avancement

**Vérifié sur la chaîne :** production de blocs Parlia à 3 s · franchissement des blocs d'epoch
(bloc 200 scellé, chaîne poursuivie sans erreur) · transactions natives · déploiement et
exécution de contrats · jeton BOS20 complet et testé · API JSON-RPC et WebSocket · persistance
de l'état après redémarrage.

**Ce qui reste à faire :**

1. **Un seul validateur.** Aucune tolérance aux pannes, aucune sécurité byzantine. Il en faut
   au minimum 4, idéalement 7 à 21 sur des machines distinctes. C'est la priorité.
2. **Pas de finalité rapide.** Les clés BLS sont à zéro, le vote d'attestation est inactif.
3. **Pas de staking ni de slashing.** Conséquence assumée du contrat système simplifié.
4. **Forks postérieurs à Kepler non activés.** Solidity vise Cancun par défaut : compiler sans
   `evmVersion: 'shanghai'` produit l'opcode `MCOPY`, que la chaîne rejette.
5. **L'explorateur n'indexe rien.** Il interroge le RPC en direct : ni historique par adresse,
   ni vérification de code source. Blockscout est la cible, mais ses versions 11 et suivantes
   ne sont plus open source depuis le 22 avril 2026 — il faut épingler `v10.2.6`, dernière
   version sous GPLv3, sans quoi le rebranding est contractuellement interdit.
6. **Chain ID non enregistré.** `26262` est libre aujourd'hui, mais rien ne le réserve tant
   qu'une PR n'a pas été acceptée sur `ethereum-lists/chains`.

---

## Sécurité des clés

Les clés de validateur utilisées en développement sont sans valeur. **Les clés de production
doivent être générées sur le serveur cible**, ne jamais transiter par un poste de travail ni
par ce dépôt. Le `.gitignore` exclut `node*/`, `pw.txt` et `.env` — ne le contournez pas.

---

## Licence

Le client hérite de la licence de `bnb-chain/bsc` (LGPL-3.0 / GPL-3.0).
Les contrats et scripts de ce dossier sont sous licence MIT.
Le logo Coinbosa est la propriété de Coinbosa Inc et n'est couvert par aucune de ces licences.
