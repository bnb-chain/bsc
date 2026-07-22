<div align="center">
  <img src="assets/coinbosa-logo.jpg" alt="Coinbosa" width="170" />

  # Coinbosa Chain

  Réseau blockchain ouvert, public et à validateurs autorisés,
  compatible avec le protocole Ethereum.
</div>

---

## Le réseau

Coinbosa Network est une blockchain compatible Ethereum, sécurisée par un consensus de
**preuve d'enjeu** : les validateurs immobilisent des jetons pour participer à la production
des blocs, et les perdent en cas de faute.

Le moteur de consensus, Parlia, est conçu pour ce modèle — son nom même signifie *Proof of
Staked Authority*. Il combine l'immobilisation d'un enjeu et un nombre de validateurs borné,
ce qui permet des blocs courts et des frais très faibles, là où un consensus ouvert à tous
impose des compromis de vitesse.

L'implémentation actuelle repose sur le client [BNB Smart Chain](https://github.com/bnb-chain/bsc)
et son moteur de consensus **Parlia**. Voir la section « Écarts avec le livre blanc » pour les
raisons de ce choix.

**Éditeur** — coinbosa, Inc., Delaware, United States.

---

## Le jeton BOSA

| | |
|---|---|
| Nom | Coinbosa |
| Symbole | BOSA |
| Décimales | 18 |
| Offre | 700 000 000 BOSA, fixée au genesis |
| Nature | coin natif de la chaîne |

BOSA est le **coin natif** de Coinbosa Chain : il paie le gas et sert d'enjeu au consensus. Il
n'existe pas de second actif portant ce nom.

Les 18 décimales ne sont pas un choix mais une contrainte de l'EVM, dont l'unité de base est le
wei. L'offre de 700 000 000 est fixée au bloc de genèse et **aucune émission n'est possible** :
le moteur de consensus ne crée pas de monnaie.

La répartition complète et la rémunération des validateurs sont dans
**[TOKENOMICS.md](TOKENOMICS.md)**.

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
  IBRC20.sol                 interface du standard
  BRC20.sol                  implémentation de référence, réutilisable pour tout jeton
  ExampleToken.sol           jeton de démonstration du standard
  CoinbosaValidatorSet.sol   contrat système du consensus
```


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
la surface d'appel exigée par le consensus, et rien de plus. Aucune fonction du chemin consensus
ne peut revert — c'est la règle de conception, puisqu'un revert rend le bloc improduisible.

**Ce contrat ne porte pas encore l'enjeu.** Dans sa version actuelle, le set de validateurs est
fixe et modifiable par un gouverneur : c'est ce qu'il fallait pour débloquer la chaîne, pas la
cible. La couche d'enjeu — dépôt, élection par le montant immobilisé, récompenses, sanctions,
période de déblocage — reste à écrire. C'est le chantier décrit au jalon 2 de la
[feuille de route](ROADMAP.md).

---

## Caractéristiques du réseau

| | |
|---|---|
| Chain ID | `26262` |
| Consensus | Parlia — preuve d'enjeu (*Proof of Staked Authority*) |
| Temps de bloc | **5 secondes** |
| Epoch | 200 blocs |
| Client | `geth` v1.7.6 patché pour Coinbosa |
| Compatibilité EVM | Shanghai |

### Pourquoi le client est patché

Le temps de bloc ne se règle **pas** dans le genesis. `ParliaConfig` est une structure vide
depuis la v1.7.6 : le temps de bloc et l'epoch sont des constantes Go pilotées par les
hardforks — 3 s par défaut, 1,5 s après Lorentz, 0,75 s après Maxwell. Les tutoriels qui
recopient `period` et `epoch` dans le genesis sont périmés : ces champs sont ignorés.

Obtenir les 5 secondes du livre blanc impose donc de modifier le client lui-même :

```diff
  consensus/parlia/parlia.go
- defaultBlockInterval uint64 = 3000 // Default block interval in milliseconds
+ defaultBlockInterval uint64 = 5000 // Coinbosa : 5 s par bloc (livre blanc)
```

**Le binaire officiel de BNB Chain ne convient donc pas** : il faut compiler ce dépôt.

```bash
make geth      # produit build/bin/geth
```

---

## Écarts avec le livre blanc

Le livre blanc v2 décrit une architecture qui ne correspond plus à l'implémentation. Ces
écarts sont documentés ici plutôt que passés sous silence, parce qu'ils touchent au code.

| Livre blanc v2 | Implémentation | Pourquoi |
|---|---|---|
| Consensus AuRa, client Parity / OpenEthereum | Consensus Parlia, client geth (fork BSC) | **OpenEthereum est abandonné depuis 2021** et son dépôt est archivé. Construire dessus aujourd'hui signifierait partir d'un client non maintenu, sans correctifs de sécurité. Parlia appartient à la même famille — validateurs connus, autorité — mais reste activement développé. |
| Temps de bloc 5 s | **conforme** | Obtenu en patchant le client, voir plus haut. |
| Standard `BRC20` / `BRC-721` | `BRC20` conforme ; **BRC-721 écarté** | Les NFT ne sont pas au programme : le standard ne sera implémenté que si un besoin produit le justifie. |
| Frais quasi nuls, gas à 1 gwei | conforme | Le calcul du livre blanc reste valable. |
| 12 validateurs à la cérémonie initiale | 1 validateur | À faire. C'est le chantier prioritaire. |
| 400 000 transactions par seconde | non mesuré | Aucun réseau EVM à validateurs connus n'atteint cet ordre de grandeur. Ce chiffre n'est pas repris ici tant qu'il n'a pas été mesuré sur le réseau réel. |

Le livre blanc v2 date de 2021-2022. Les écarts ci-dessus tiennent à l'évolution de
l'écosystème depuis, pas à un changement d'ambition.

**Note sur le nom `BRC20`** — ici, l'acronyme signifie *Bosa smart contRact 20*. Un standard
homonyme existe sur Bitcoin (inscriptions Ordinals), sans aucun rapport technique avec
celui-ci. Préciser « BRC20 de Coinbosa » dans les intégrations tierces évitera la confusion.

---

## Paramètres

Tous les paramètres du réseau et du jeton vivent dans **`coinbosa.config.json`**. C'est le seul
fichier à modifier pour changer une valeur : rien n'est codé en dur dans les scripts.

La feuille de route, les jalons et la marche à suivre pour reprendre le projet sur une autre
machine sont dans **[ROADMAP.md](ROADMAP.md)**.

La spécification du site public et de l'explorateur — identité visuelle, architecture
multilingue, critères de réception — est dans **[FRONTEND.md](FRONTEND.md)**.

Les décisions structurantes du projet, avec leur justification et les points encore ouverts,
sont consignées dans **[DECISIONS.md](DECISIONS.md)**, et l'économie du jeton dans **[TOKENOMICS.md](TOKENOMICS.md)**.

---

## Démarrage

```bash
npm install

# générer la clé du validateur, puis le genesis correspondant
./build/bin/geth account new --datadir node1
VALIDATOR=0xVotreValidateur node scripts/build-genesis.js

node scripts/compile.js      # compiler les contrats
./scripts/start-node.sh      # lancer le nœud validateur
./scripts/start-explorer.sh  # explorateur sur http://127.0.0.1:8080
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
5. **Forks postérieurs à Kepler non activés.** Solidity vise Cancun par défaut : compiler sans
   `evmVersion: 'shanghai'` produit l'opcode `MCOPY`, que la chaîne rejette.
6. **L'explorateur n'indexe rien.** Il est multilingue et aux couleurs de la marque, mais
   interroge le RPC en direct : ni historique par adresse, ni vérification de code source.
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
