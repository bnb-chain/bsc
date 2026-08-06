# Créer les coins — procédure du genesis de production

Les 700 000 000 BOSA n'existent pas encore. Ils naîtront **au bloc 0**, en une seule
opération : le genesis inscrit l'offre et ses détenteurs, puis plus rien ne peut la changer —
le moteur de consensus ne crée pas de monnaie.

Il n'y a donc **pas de deuxième essai**. Une adresse mal recopiée, une clé perdue, un poste
oublié : la seule correction possible serait de relancer un réseau, c'est-à-dire d'abandonner
le premier. Ce document est la marche à suivre pour que cela n'arrive pas.

> **État au 6 août 2026 : NO-GO.** Les 13 adresses de répartition valent toutes `0x0`, aucun
> gouverneur n'est désigné, un seul validateur existe. `node scripts/preflight-genesis.js`
> le confirme. Rien de ce qui suit ne doit être lancé avant que ce contrôle dise GO.

---

## Ce qui sera créé

| | |
|---|---|
| Offre totale | 700 000 000 BOSA, **fixée définitivement** au bloc 0 |
| Décimales | 18 (imposées par l'EVM) |
| Émission ultérieure | **aucune** — le consensus ne crée pas de monnaie |
| Réserve de migration | 0 (le projet détient tout l'historique Solana) |
| Répartition | 13 postes, 100 % — détail dans `coinbosa.config.json` |

---

## Étape 1 — Les coffres qui détiendront l'offre

Chaque poste reçoit entre 14 et 140 millions de BOSA. Une clé unique par poste serait un
point de défaillance unique : sa perte ou son vol emporte la part entière, définitivement.

**Chaque adresse de `genesis/distribution-addresses.json` doit être un coffre
multi-signatures (seuil ≥ 2 sur N).** Les signataires doivent être des personnes distinctes,
avec des sauvegardes conservées séparément.

Deux ordres de grandeur pour fixer les idées : le poste `developpement` porte 140 000 000
BOSA, soit 20 % de tout ce qui existera. `reserveStrategique` en porte 21 000 000.

- Générer les coffres **avant** toute autre étape.
- Vérifier chaque adresse par **copier-coller, jamais à la main** : la somme de contrôle
  EIP-55 est vérifiée par le préflight, mais elle ne protège pas d'une adresse valide qui
  n'est simplement pas la bonne.
- **Tester une récupération** sur au moins un coffre avant d'y envoyer quoi que ce soit :
  une sauvegarde jamais testée n'est pas une sauvegarde.

## Étape 2 — Les validateurs

Avec un seul validateur, le réseau n'a ni tolérance aux pannes ni sécurité byzantine : la
machine s'arrête, la chaîne s'arrête. **Minimum 4 validateurs à clés distinctes** (le livre
blanc en vise 12).

- Générer chaque clé de scellage **sur son propre serveur** (`geth account new`), jamais sur
  un poste de travail, jamais copiée d'une machine à l'autre.
- Serveurs séparés, idéalement chez des hébergeurs différents.
- La clé de scellage signe un bloc toutes les 5 secondes : elle est **en ligne en
  permanence**. Elle ne doit donc jamais détenir de fonds ni gouverner quoi que ce soit.

## Étape 3 — Le gouverneur

Le gouverneur du contrat système peut modifier l'ensemble des validateurs. C'est le pouvoir
le plus sensible de la chaîne.

- Ce doit être un **coffre multi-signatures distinct**, idéalement derrière un délai
  (*timelock*) qui laisse le temps de réagir à une décision anormale.
- `build-genesis.js` **refuse** en production un gouverneur égal au validateur.

## Étape 4 — Prouver le retrait des 500 M Solana

Le livre blanc annonce que les 500 000 000 jetons Solana historiques sont retirés de la
circulation, pour qu'ils ne soient pas comptés deux fois avec l'offre native.

Tant qu'aucune transaction publique ne le prouve, c'est une **intention**, pas un fait — et
elle ne doit pas être écrite au présent. Publier l'identifiant de transaction vérifiable
(portefeuille projet : `5pdFbZdyab9jQUnC2E4x9XGmLpAFNqoF4GyjEtpfedQf`), puis mettre à jour
le livre blanc pour le citer.

## Étape 5 — Audit externe

Le contrat système (`CoinbosaValidatorSet.sol`) et le genesis produit doivent être relus par
un tiers indépendant. La règle de conception à vérifier en priorité : **aucune fonction du
chemin de consensus ne doit pouvoir échouer** — un `revert` y rend le bloc improduisible,
donc arrête la chaîne.

---

## Étape 6 — Le contrôle avant vol

```bash
cd coinbosa
npm ci

VALIDATOR=0x…               # clé de scellage du premier validateur
GOVERNOR=0x…                # coffre multi-signatures, distinct
VALIDATORS=0xa,0xb,0xc,0xd  # toutes les clés de scellage

VALIDATOR=$VALIDATOR GOVERNOR=$GOVERNOR VALIDATORS=$VALIDATORS \
  node scripts/preflight-genesis.js
```

Le script dit **GO** ou **NO-GO**. Il vérifie ce qui est vérifiable depuis la machine :
adresses renseignées, valides EIP-55, distinctes, non dérivées du mode développement ;
arithmétique de l'offre exacte ; séparation validateur / gouverneur / trésorerie ; nombre de
validateurs ; version de solc ; disponibilité du fichier d'empreinte.

Il liste séparément les **attestations** qu'il ne peut pas vérifier (nature multi-signatures
des coffres, origine des clés, preuve du burn Solana, audit externe). Elles restent sous la
responsabilité de l'éditeur : le script ne les coche jamais tout seul.

**Ne pas continuer tant que le verdict n'est pas GO.**

## Étape 7 — Produire le genesis

```bash
VALIDATOR=$VALIDATOR GOVERNOR=$GOVERNOR node scripts/build-genesis.js
```

Sans `ALLOW_DEV=1`, le script écrit `genesis/genesis-coinbosa.json` et refuse de démarrer si
une adresse manque. Relire la table de répartition affichée **ligne à ligne** : c'est le
dernier moment où une erreur se corrige gratuitement.

## Étape 8 — Vérifier avant d'ouvrir au public

```bash
../build/bin/geth init --datadir node1 genesis/genesis-coinbosa.json
# démarrer le nœud, puis :
RPC=http://127.0.0.1:8545 node scripts/check-supply.js
RPC=http://127.0.0.1:8545 node scripts/check-genesis-hash.js
```

- `check-supply.js` : l'offre on-chain vaut exactement 700 000 000, chaque poste a le solde
  prévu, le pont hérité est vide et sans code.
- `check-genesis-hash.js` : compare l'empreinte du bloc 0 à `genesis-reference.json`.

## Étape 9 — Figer l'empreinte

Recopier `hash`, `stateRoot` et `extraData` du bloc 0 dans `genesis/genesis-reference.json`,
renseigner `fige_le`, puis committer.

C'est **cette empreinte** qui rend la promesse « aucune émission cachée » vérifiable par
n'importe qui : `stateRoot` est la racine de Merkle de tout l'état initial, donc un seul wei
ajouté à une adresse quelconque — même inconnue — la change. Sans empreinte figée, la
promesse ne serait qu'une affirmation.

## Étape 10 — Après le lancement

- Faire tourner `check-genesis-hash.js` contre le RPC public : il prouve à tout moment que
  la chaîne servie est bien celle qui a été publiée.
- Publier les adresses de répartition pour qu'elles soient consultables dans l'explorateur.
- Enregistrer `chainId 26262` sur `ethereum-lists/chains`.

---

## Résumé des garde-fous automatiques

| Garde-fou | Où | Ce qu'il empêche |
|---|---|---|
| Adresse nulle refusée | `build-genesis.js` | envoyer l'offre dans le vide |
| Adresses partagées refusées | `build-genesis.js` | fusionner deux postes sans s'en apercevoir |
| Gouverneur = validateur refusé | `build-genesis.js` | qu'un serveur compromis emporte le consensus |
| Version de solc épinglée | `build-genesis.js`, `compile.js` | changer le bytecode, donc l'identité de la chaîne |
| Total ≠ offre refusé | `build-genesis.js` | créer ou perdre des coins par arrondi |
| Marqueur `coinbosaDev` | `check-supply.js` | déployer un genesis de développement en production |
| Empreinte du bloc 0 | `check-genesis-hash.js` | une allocation cachée, ou une chaîne substituée |
| Verdict GO / NO-GO | `preflight-genesis.js` | lancer la création avec une condition non remplie |
