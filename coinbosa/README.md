<div align="center">
  <img src="assets/coinbosa-logo.jpg" alt="Coinbosa" width="170" />

  # Coinbosa Chain

  Réseau blockchain ouvert, public et à validateurs autorisés,
  compatible avec le protocole Ethereum.
</div>

---

## Le réseau

Coinbosa Network est une blockchain compatible Ethereum, sécurisée par un groupe de
validateurs connus et identifiés, selon un consensus de type **preuve d'autorité**.

Contrairement à un réseau à preuve de travail ou à preuve d'enjeu ouverte, l'identité de
chaque validateur est vérifiée avant son entrée dans le consensus, et la liste des validateurs
est gouvernée par un contrat intelligent. C'est ce qui permet des blocs courts et des frais de
transaction très faibles, sans dépendre d'une capitalisation élevée pour assurer la sécurité
du réseau.

L'implémentation actuelle repose sur le client [BNB Smart Chain](https://github.com/bnb-chain/bsc)
et son moteur de consensus **Parlia**. Voir la section « Écarts avec le livre blanc » pour les
raisons de ce choix.

### Éditeur

**coinbosa, Inc.** — Delaware C Corporation Subsidiary, constituée le 5 janvier 2026.
Dossier Delaware n° 10460257.

Agent enregistré : Legalinc Corporate Services Inc., 131 Continental Dr, Suite 305,
Newark, DE 19713, États-Unis.

---

## Le jeton BOSA

| | |
|---|---|
| Nom | Coinbosa |
| Symbole | BOSA |
| Décimales | 10 |
| Offre initiale | 700 000 000 BOSA |
| Standard | BOS20 |

BOSA est un **jeton déployé sur** Coinbosa Chain, et non le coin natif qui paie le gas — même
rapport qu'entre CAKE et BNB sur BNB Chain. C'est ce choix qui rend les 10 décimales
possibles : sur une chaîne EVM, le coin natif est structurellement à 18 décimales, parce que
l'unité de base est le wei et que cette valeur est câblée dans le calcul du gas et dans les
wallets. La structure `core.Genesis` du client ne comporte d'ailleurs aucun champ nom, symbole
ou décimales.

L'intégralité de l'offre est émise à une **adresse de départ** fournie au déploiement. Elle
n'est écrite en dur nulle part dans le code.

---

## Le standard de jeton

Le standard définit la liste des règles qu'un jeton Coinbosa doit implémenter. Il est
l'équivalent de l'ERC-20 d'Ethereum et du BEP-20 de BNB Chain, et reste **compatible ERC-20** :
tout wallet, pont ou service qui parle ERC-20 fonctionne sans adaptation.

S'y ajoutent :

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

Le standard NFT équivalent à l'ERC-721 reste à implémenter.

---

## Le contrat système du consensus

Un fork de BSC hérite de dix contrats système pré-déployés aux adresses `0x…1000` à `0x…2000`.
Celui de `0x…1000` gouverne le consensus : le moteur lui demande la liste des validateurs à
chaque bloc d'epoch, tous les 200 blocs.

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
la surface d'appel exigée par le consensus, et rien de plus. Aucune fonction du chemin
consensus ne peut revert — c'est la règle de conception, puisqu'un revert rend le bloc
improduisible. Le set de validateurs vit en storage et se met à jour par `updateValidatorSet()`,
sans nouveau genesis. C'est ce contrat qui portera la gouvernance de la liste des validateurs.

---

## Caractéristiques du réseau

| | |
|---|---|
| Chain ID | `26262` |
| Consensus | Parlia (preuve d'autorité avec enjeu) |
| Temps de bloc | 3 secondes |
| Epoch | 200 blocs |
| Client | `geth` v1.7.6, commit `69b3758c8` |
| Compatibilité EVM | Shanghai |

Le temps de bloc et l'epoch ne se règlent **pas** dans le genesis. `ParliaConfig` est une
structure vide depuis la v1.7.6 : ce sont des constantes Go pilotées par les hardforks — 200
blocs et 3 s par défaut, 500 et 1,5 s après Lorentz, 1000 et 0,75 s après Maxwell.

---

## Écarts avec le livre blanc

Le livre blanc v2 décrit une architecture qui ne correspond plus à l'implémentation. Ces
écarts sont documentés ici plutôt que passés sous silence, parce qu'ils touchent au code.

| Livre blanc v2 | Implémentation | Pourquoi |
|---|---|---|
| Consensus AuRa, client Parity / OpenEthereum | Consensus Parlia, client geth (fork BSC) | **OpenEthereum est abandonné depuis 2021** et son dépôt est archivé. Construire dessus aujourd'hui signifierait partir d'un client non maintenu, sans correctifs de sécurité. Parlia appartient à la même famille — validateurs connus, autorité — mais reste activement développé. |
| Temps de bloc 5 s | 3 s | Constante Go du client, non configurable par le genesis. |
| 12 validateurs à la cérémonie initiale | 1 validateur | À faire. C'est le chantier prioritaire. |
| Standard `BRC20` / `BRC-721` | `BOS20` | **À trancher** — voir ci-dessous. |
| Frais quasi nuls, gas à 1 gwei | conforme | Le calcul du livre blanc reste valable. |
| 400 000 transactions par seconde | non mesuré | Aucun réseau EVM à validateurs connus n'atteint cet ordre de grandeur. Ce chiffre n'est pas repris ici tant qu'il n'a pas été mesuré sur le réseau réel. |

### Point à trancher : BOS20 ou BRC20 ?

Le livre blanc nomme le standard **BRC20**, avec **BRC-721** pour les NFT. Le code actuel
implémente **BOS20**. Les deux sont fonctionnellement identiques ; seul le nom diffère. Le
renommage est mécanique — noms de fichiers, de contrats et d'interface — mais il doit être
décidé avant toute publication, parce que le nom du standard apparaîtra dans chaque contrat
déployé sur le réseau.

À noter : `BRC-20` désigne déjà un standard largement connu sur Bitcoin (inscriptions
Ordinals). Réutiliser ce nom exposerait à une confusion permanente dans la documentation, les
recherches et les intégrations tierces.

---

## Démarrage

```bash
npm install

# générer la clé du validateur, puis le genesis correspondant
./bin/coinbosa-geth account new --datadir node1
VALIDATOR=0xVotreValidateur node scripts/build-genesis.js

node scripts/compile.js      # compiler les contrats
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

**Vérifié sur la chaîne :** production de blocs à 3 s · franchissement des blocs d'epoch
(bloc 200 scellé, chaîne poursuivie sans erreur) · transactions natives · déploiement et
exécution de contrats · jeton complet et testé · API JSON-RPC et WebSocket · persistance de
l'état après redémarrage.

**Ce qui reste à faire :**

1. **Passer de 1 à 12 validateurs**, conformément au livre blanc. Aujourd'hui, avec un seul
   validateur, le réseau n'a ni tolérance aux pannes ni sécurité byzantine.
2. **Gouvernance des validateurs** — cérémonie initiale, vote d'ajout et de révocation,
   remplacement de clé compromise.
3. **Finalité rapide** — les clés BLS sont à zéro, le vote d'attestation est inactif.
4. **Standard NFT** — l'équivalent ERC-721 n'est pas implémenté.
5. **Forks postérieurs à Kepler non activés.** Solidity vise Cancun par défaut : compiler sans
   `evmVersion: 'shanghai'` produit l'opcode `MCOPY`, que la chaîne rejette.
6. **L'explorateur n'indexe rien.** Il interroge le RPC en direct : ni historique par adresse,
   ni vérification de code source.
7. **Chain ID non enregistré.** `26262` est libre aujourd'hui, mais rien ne le réserve tant
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
Le logo Coinbosa est la propriété de coinbosa, Inc. et n'est couvert par aucune de ces licences.
