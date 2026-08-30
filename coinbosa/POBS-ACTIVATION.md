# PoBS — procédure d'activation et de répétition

*Procédure d'exploitation. Elle décrit ce qu'il faut faire, dans quel ordre, avec
quelle commande et quel chiffre observer. Elle ne décrit aucun état atteint : à ce
jour, aucune étape n'a été exécutée.*

Ce document complète `POBS.md` (spécification, §5 « Procédure de bascule »). Il ne
remplace ni `AGENTS.md`, ni `deploy/README.md`, dont les règles restent en vigueur.

**Portée.** Voie B — bifurcation du client. Le consensus lit le jeu de validateurs
dans un contrat d'enjeu au lieu de `0x…1000`, à partir d'une porte temporelle
appelée `pobsTime` (notée **T** ci-dessous). `contracts/CoinbosaValidatorSet.sol`
n'est pas modifié ; le genesis n'est pas modifié.

**Rappel de cadre.** La chaîne a **un** validateur. Une erreur de bascule l'arrête,
et un arrêt supprime le seul moyen de le corriger — plus aucun bloc n'est produit,
donc plus aucune transaction corrective ne peut être minée.

---

## 0. Ce qui a été RÉELLEMENT mesuré pour écrire ce document

Tout le reste de ce document est une prescription. Cette section-ci, et elle seule,
rapporte des commandes lancées dans `/Users/protocole/repo`. Aucun déploiement,
aucune transaction, aucun accès au serveur de production.

### 0.1 Le piège 1 → N, rejoué

```
$ go test ./consensus/parlia/ -run Coinbosa -v 2>&1 | tail -30
=== RUN   TestCoinbosaAddSecondValidatorHaltsChain
N=1  minerHistoryCheckLen=0  SignRecently(V1)=false
N=2  minerHistoryCheckLen=1  SignRecently(V1)=true  SignRecently(V2)=false  inturn(201)=0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50
=> bloc 201 : V1 refuse de sceller (Seal: "Signed recently, must wait for others"),
   V2 n'a pas de noeud => arret de la chaine.
--- PASS: TestCoinbosaAddSecondValidatorHaltsChain (0.00s)
PASS
ok  	github.com/ethereum/go-ethereum/consensus/parlia	2.349s
```

Le test **compile et passe**. Il est analysé au §4.

### 0.2 Les bancs PoBS déjà présents dans l'arbre

```
$ go test ./consensus/parlia/ -run 'Pobs|PoBS' -v
--- PASS: TestPobsPrematureForkNoContractCode (0.00s)
    contrat absent -> getCurrentValidators renvoie l'erreur :
    "abi: attempting to unmarshal an empty string while arguments are expected"
--- PASS: TestPobsPrematureForkEmptyValidatorSet (0.00s)
    ensemble vide : decodage SANS erreur, len(valSet)=0
    verifyHeader (parlia.go:622) -> errInvalidSpanValidators
    soit exactement : invalid validator list on sprint end block
--- PASS: TestPobsQuorumTableForEachSetSize (0.00s)
    N= 1 -> 1 scelleur exigé   N= 2 -> 2   N= 3 -> 2   N= 4 -> 3   N= 5 -> 3
    N= 6 -> 4   N= 7 -> 4   N= 8 -> 5   N= 9 -> 5   N=10 -> 6   N=11 -> 6   N=12 -> 7
ok  	github.com/ethereum/go-ethereum/consensus/parlia	1.828s

$ go test ./core/forkid/ -run Coinbosa -v
--- PASS: TestCoinbosaForkOrderRefusesNewTimestampGate (0.00s)
    config actuelle : CheckConfigForkOrder OK
    porte ajoutee dans la liste ordonnee -> le noeud REFUSE de demarrer :
    unsupported fork ordering: bohrTime not enabled, but pascalTime enabled at timestamp 1800000000
--- PASS: TestCoinbosaPobsGateP2PCompat (0.00s)
    forkid ANCIEN  avant=416a4a3c next=0        forkid NOUVEAU avant=416a4a3c next=1800000000
    forkid ANCIEN  apres=416a4a3c next=0        forkid NOUVEAU apres=bba58f79 next=0
    AVANT la porte : ancien <-> nouveau s'appairent (fenetre de deploiement sure)
    APRES la porte : ancien juge le nouveau -> local incompatible or needs update
    APRES la porte : nouveau juge l'ancien -> remote needs update
ok  	github.com/ethereum/go-ethereum/core/forkid	0.661s
```

Ces deux fichiers (`consensus/parlia/coinbosa_pobs_activation_test.go`,
`core/forkid/coinbosa_pobs_gate_test.go`) sont **non suivis par git** :

```
$ git status --short
?? consensus/parlia/coinbosa_pobs_activation_test.go
?? core/forkid/coinbosa_pobs_gate_test.go
```

Ils ont été écrits par un autre intervenant. Voir étape **7**.

### 0.3 Le dépôt ne peut pas produire de client — BLOQUANT

```
$ go build ./... > /tmp/gobuild.txt 2>&1 ; echo "EXIT=$?"
EXIT=1
    eth/ethconfig/config.go:38:2: no required module provides package .../miner/minerconfig
    eth/api_backend.go:46:2:      no required module provides package .../miner
    signer/core/cliui.go:29:2:    no required module provides package .../console/prompt

$ ls -d cmd miner console build
ls: build: No such file or directory
ls: cmd: No such file or directory
ls: console: No such file or directory
ls: miner: No such file or directory

$ git sparse-checkout list
.github accounts beacon coinbosa common consensus core crypto eth ethclient
ethdb event internal log metrics node p2p params rlp rpc
```

Le checkout est **sparse**. `cmd/`, `miner/`, `console/`, `build/` sont absents, donc
`make geth` est impossible, donc `build/bin/geth` — exigé par `deploy/30-node.sh:30`,
`deploy/40-validator.sh:30`, `deploy/50-monitoring.sh` et `scripts/start-node.sh:41` —
n'existe pas.

Le sous-ensemble qui nous intéresse compile, lui :

```
$ go build ./params/... ./core/forkid/... ./core/systemcontracts/... ./consensus/parlia/... ./core/ ; echo "EXIT=$?"
EXIT=0
```

**Conséquence :** la répétition du §3, qui est le seul filet entre nous et un arrêt
définitif, est aujourd'hui **inexécutable**. C'est l'étape 1.

### 0.4 Constantes de la chaîne, relues à la source

| Grandeur | Valeur | Source lue |
|---|---|---|
| chainId | 26262 | `genesis/genesis-coinbosa.json` |
| période de bloc | 5 s | `consensus/parlia/parlia.go:61` (`defaultBlockInterval uint64 = 5000`) |
| epoch | 200 blocs | `genesis-coinbosa.json` → `parlia.epoch` ; `coinbosa.config.json` → `epochLength` |
| **durée d'un epoch** | **1 000 s = 16 min 40 s** | 200 × 5 s |
| gasLimit | `0x2625a00` = 40 000 000 | `genesis-coinbosa.json` |
| extraData du bloc 0 | 166 octets = 32 + 1 + 68×1 + 65 | idem |
| entrées d'alloc | 23 | idem |
| forks actifs | la chaîne des forks **s'arrête à `keplerTime: 0`** — pas de `feynmanTime`, pas de `bohrTime` | idem |
| hash du bloc 0 | `0x8dcdadc247a98f33728cae944e20ce7c49c74b35cfba31495f85e98979018da6` | `genesis/genesis-reference.json` |
| stateRoot du bloc 0 | `0x93682eb9182a55531d47014b76a285b45d3e720a2951f9ffbdc67f52995f8c03` | idem |
| validateur de genèse | `0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50` | idem |
| gouverneur | `0x1EEf3830833d83AcD3152A511853fd04a0b4082A` | idem |
| offre | 700 000 000 BOSA | `coinbosa.config.json` → `nativeCoin.totalSupply` |

Points de code cités plus bas, tous relus :
`parlia.go:983` `prepareValidators` · `parlia.go:1208` `verifyValidators` ·
`parlia.go:1919` `getCurrentValidators` · `parlia.go:1941` l'adresse lue
(`systemcontracts.ValidatorContract`) · `parlia.go:1960` la boucle
`voteAddrMap[valSet[i]] = &(voteAddrSet)[i]` · `parlia.go:1744` le journal
« Signed recently, must wait for others » · `parlia.go:145` et `:623`
`errInvalidSpanValidators` · `snapshot.go:243`
`minerHistoryCheckLen() = (len(Validators)/2+1)*TurnLength - 1` ·
`params/config.go:1564-1570` l'ordre `keplerTime` → `feynmanTime` → … → `bohrTime` ·
`core/systemcontracts/const.go:5-22` les adresses système `0x…1000` à `0x…3000`.

### 0.5 Ce que la surveillance de production regarde — et ne regarde pas

`deploy/50-monitoring.sh` (l. 17-21) surveille : la hauteur qui avance, la synchro
des deux nœuds, les services, le disque, le certificat TLS. Les alertes partent vers
Sentry par requête HTTP (l. 82-84), et le code de retour est contrôlé (l. 86-90).

```
$ grep -n "DoubleSign" coinbosa/deploy/50-monitoring.sh
(aucune ligne)
```

**Aucune alerte sur une double signature, ni sur un changement de jeu de
validateurs.** Voir étape 4.

### 0.6 Ce que je n'ai PAS pu vérifier, et que je n'affirme donc pas

- Le comportement du binaire `geth` : `cmd/`, `miner/`, `console/` sont absents. Je
  n'ai lu ni `geth init`, ni le code du mineur, ni les lignes de journal qu'il
  produit. Tout ce qui concerne le mineur ci-dessous est à **confirmer d'abord sur
  la chaîne jetable**, jamais à supposer.
- La chaîne de production : hors périmètre par consigne. Aucun chiffre de ce
  document ne vient d'un nœud vivant.
- Le bytecode du contrat d'enjeu : il n'existe pas encore.

---

## 1. Vue d'ensemble

Trois artefacts doivent exister et être cohérents, dans cet ordre :

| | Artefact | Contrainte d'ordre |
|---|---|---|
| **A** | contrat d'enjeu déployé à une adresse fixe, **avec son état d'amorçage écrit** | — |
| **B** | binaire client portant la porte, épinglant **adresse ET codehash** de A | A d'abord : le codehash n'existe pas avant le déploiement |
| **C** | **tous** les nœuds tournant sur B | avant T |

**La porte est double.** `pobsTime` seul ne suffit pas. Le client ne bascule sa
lecture vers le contrat d'enjeu que si, **dans l'état du bloc parent**, l'adresse
épinglée porte un code dont le keccak vaut le codehash épinglé. Sinon il continue de
lire `0x…1000`.

Trois raisons, et la troisième est celle qui compte :

1. Le codehash n'existe qu'après le déploiement : l'ordre A → B devient *impossible*
   à inverser, au lieu d'être seulement déconseillé.
2. « T franchi alors que le contrat n'est pas là » devient un **no-op inoffensif**
   au lieu d'un arrêt (mesuré au §0.2 : contrat absent → `abi: attempting to
   unmarshal an empty string` → `Prepare` échoue → aucun bloc d'epoch produit).
3. La condition est dérivée de l'**état partagé** : tous les nœuds la calculent à
   l'identique. Un repli calculé côté client — horloge locale, variable
   d'environnement, fichier de configuration, échec d'un `eth_call` en temps limité —
   scinderait la chaîne au lieu de la protéger.

> **Règle qui commande tout le reste.**
> Un échec **local** (délai dépassé, plafond de gaz RPC, état du parent absent,
> RPC muet) n'ouvre **jamais** de repli : il arrête *ce* nœud, bruyamment.
> Seuls des faits **déterministes**, observés identiquement par tous les nœuds
> (code absent à l'adresse épinglée, codehash différent, ensemble vide, longueurs
> de tableaux inégales, plus de 41 élus, doublons) peuvent déclencher une règle de
> correction.
> Violée une seule fois, cette règle transforme PoBS en générateur de bifurcations.

**La bascule n'a lieu ni à T, ni au bloc suivant.** `prepareValidators`
(`parlia.go:983`) ne fait rien hors bloc d'epoch. Elle a lieu au **premier bloc
d'epoch dont l'état du parent est au-delà de T** — noté **E** — soit **au plus tard
T + 1 000 s**. C'est E, pas T, qu'on surveille.

**La fenêtre C → T est la seule où une erreur se rattrape sans coût.** Mesuré
(§0.2) : avant T, un nœud sur l'ancien binaire et un nœud sur le nouveau
s'appairent normalement.

**Deux points de non-retour, et deux seulement** — développés au §5 :

- **franchir T** (la partition p2p commence à la seconde exacte) ;
- **étendre l'ensemble de 1 à N** (si les entrants ne scellent pas, la chaîne
  s'arrête et rien n'est plus corrigeable on-chain).

---

## 2. La séquence — 31 étapes

Chaque étape porte une commande exacte et un critère **chiffré**. Une étape dont le
critère n'est pas atteint arrête la procédure ; on ne passe pas à la suivante.

Conventions :
- `$REPO` = `/Users/protocole/repo`
- `$RPC_PROD` = l'URL RPC de production
- `$ADDR` = adresse du contrat d'enjeu · `$CODEHASH` = keccak du code déployé
- **T** = valeur de `pobsTime` · **E** = premier bloc d'epoch post-T

---

### PHASE 0 — PRÉREQUIS (étapes 1 à 7)

Aucune ligne de PoBS n'est écrite tant que les sept ne sont pas vertes.

#### Étape 1 — Restaurer l'arbre client complet

```bash
cd /Users/protocole/repo
git sparse-checkout add cmd miner console build
go build ./... ; echo "EXIT=$?"
```

**Critère :** `EXIT=0`.
**Aujourd'hui : `EXIT=1`** (§0.3). Tant que ce n'est pas vert, **tout le reste de ce
document est interdit** — y compris la répétition. Passer outre revient à basculer la
production sans répétition, ce qui est un arrêt de chaîne à retardement.

#### Étape 2 — Produire et empreindre le binaire de référence

```bash
cd /Users/protocole/repo
make geth
./build/bin/geth version | head -5
shasum -a 256 build/bin/geth
```

**Critères :** `build/bin/geth` existe et est exécutable ; `geth version` affiche la
version attendue ; l'empreinte SHA-256 est **notée** — c'est elle qui identifiera le
« binaire d'avant » lors d'un retour en arrière (étapes 20 et 24).

#### Étape 3 — Sauvegarder la clé de scellage, restauration TESTÉE

Vérifié dans le dépôt : **aucun script de `deploy/` ne sauvegarde le keystore.**
`40-validator.sh:34` exige `pw.txt` dans le **même** `$DATADIR` que `keystore/`
(l. 53) : le mot de passe disparaît avec le même disque que la clé qu'il protège.

1. Copier le keystore chiffré hors du serveur.
2. Conserver le mot de passe **ailleurs**, sur un autre support et un autre porteur.
3. **Restaurer sur une machine vierge** et démarrer un nœud sur la chaîne jetable à
   partir de la seule sauvegarde.

**Critère :** le nœud restauré **scelle** au moins 3 blocs sur la chaîne jetable.

`docs/GENESIS-PRODUCTION.md` pose déjà la règle pour la trésorerie : « une sauvegarde
jamais testée n'est pas une sauvegarde ». Elle n'a jamais été appliquée à la clé de
scellage. Sans cette preuve, la procédure ne démarre pas : une couche d'enjeu bâtie
au-dessus d'une clé non sauvegardée n'ajoute que de la valeur immobilisée sur une
chaîne qui peut mourir d'une panne de disque.

#### Étape 4 — Rendre la double signature observable

`verifySeal` détecte l'équivocation et se contente de `log.Warn("DoubleSign detected")`.
`50-monitoring.sh` ne lit pas ce journal (§0.5).

Ajouter à la sonde une lecture de `journalctl -u coinbosa-validator` et de
`journalctl -u coinbosa-node` cherchant `DoubleSign detected`, alertant par le canal
Sentry déjà éprouvé du script (l. 82-90 : le code HTTP de retour y est déjà contrôlé, et un envoi refusé est journalisé en `daemon.crit`).

**Critère :** une ligne `DoubleSign detected` injectée artificiellement dans le
journal déclenche une alerte Sentry avec un code HTTP `200`.

Sans cette étape, la période de déblocage de 49 jours ne protège de rien : elle se
compare à un délai de détection infini.

#### Étape 5 — Amender `check-supply.js` AVANT le premier dépôt d'enjeu

`scripts/check-supply.js` compare **adresse par adresse** le solde on-chain au solde
du genesis (l. 64-70), puis exige `total === EXPECTED` (l. 87) et signale toute
divergence par adresse (l. 90-94). Il lit au bloc 0, **sauf si l'état du bloc 0 a été
purgé** — auquel cas il bascule sur `latest` (l. 51-60).

Dès le premier dépôt d'enjeu, des BOSA quittent une adresse du genesis pour le
contrat d'enjeu. Sur un nœud dont l'état du bloc 0 est purgé, la barrière passe au
rouge et la CI bloque.

Amendement : sommer les adresses du genesis **plus** le contrat d'enjeu **plus**
l'adresse de retrait de circulation, et comparer le **total** au lieu d'exiger
l'égalité par adresse au bloc courant.

**Critère :** `node scripts/check-supply.js` sort en `0` sur la chaîne jetable
**après** un dépôt d'enjeu, et le total affiché vaut exactement 700 000 000 BOSA.

#### Étape 6 — Geler la rotation par `0x…1000`

Entre le déploiement du contrat d'enjeu et E, **deux** chemins de gouvernance du jeu
de validateurs coexistent. Le gouverneur — une clé simple, sans code, irremplaçable
(`AGENTS.md`) — commande encore `updateValidatorSet` sur `0x…1000`. Une rotation
faite dans cette fenêtre serait écrasée à E, ou arrêterait la chaîne avant.

Décision à consigner : **aucune rotation par `0x…1000` entre l'étape 12 et E.**
Surveiller l'événement `ValidatorSetUpdated`.

**Critère :** zéro occurrence de l'événement sur toute la fenêtre.

Après E, le gouverneur n'est plus dans la boucle de l'élection. C'est précisément
l'intérêt de la voie B, et c'est à publier.

#### Étape 7 — Coordonner les fichiers de test PoBS non suivis

```bash
cd /Users/protocole/repo && git status --short
```

Deux fichiers écrits par un autre intervenant sont présents et non suivis (§0.2). Les
lire et se coordonner **avant** d'écrire la moindre ligne ; `git fetch` puis rebase
avant tout push (`AGENTS.md`).

**Critère :** les deux fichiers sont lus, leur sort est tranché (versionnés ou
retirés), et `go test ./consensus/parlia/ ./core/forkid/ -count=1` sort en `0`.

---

### PHASE 1 — RÉPÉTITION (étape 8)

#### Étape 8 — Exécuter intégralement le §3, sur chaîne jetable

**Critère :** R0 à R14 tous verts, sans exception et sans étape sautée.
Compter ≈ 3 h de chaîne pour R4 → R14, hors préparation.

---

### PHASE 2 — LE CONTRAT EN PRODUCTION (étapes 9 à 13)

#### Étape 9 — Figer le bytecode

Interdits absolus dans le contrat d'enjeu :

- **`SELFDESTRUCT`** — `cancunTime` est absent de `genesis-coinbosa.json` (§0.4) :
  `SELFDESTRUCT` détruit encore le code. Un contrat autodestructible ferait
  **silencieusement** revenir le consensus à `0x…1000` ;
- **`DELEGATECALL`** et tout schéma de proxy — un proxy remettrait, derrière une
  adresse et un codehash figés, une clé d'administration capable de réécrire le jeu
  de validateurs ;
- **`constructor` et `immutable`** — si le contrat est un jour posé par injection de
  code, aucun constructeur ne s'exécute et les `immutable` resteraient à zéro. Seules
  des `constant`, et **l'état vide doit être un état initial valide**.

Trois propriétés du chemin de lecture, à démontrer au banc et non à espérer :

- `getMiningValidators()` ne peut **jamais** revert : aucun `require`, aucun
  `revert`, aucun `assert`, aucun modificateur, aucun appel externe ;
- `vals.length == votes.length` **par construction** — une seule variable de taille
  pour les deux allocations ;
- chaque clé de vote fait **exactement 48 octets**, et l'ensemble rendu contient
  **toujours au moins un membre**.

**Critères :** l'analyse du bytecode ne trouve aucun des trois opcodes interdits ;
les bancs 1 à 7 du §3/R7 passent.

#### Étape 10 — Clé de déploiement à usage unique, adresse publiée d'avance

L'adresse d'un `CREATE` vaut `keccak256(rlp([expéditeur, nonce]))[12:]` : elle est
calculable **avant** que la transaction n'existe.

1. Générer une clé qui ne servira **qu'à ce déploiement** — jamais la clé de
   scellage, jamais le gouverneur, jamais une adresse de trésorerie.
2. Calculer l'adresse pour `nonce = 0`.
3. **La publier** (documentation, livre blanc, explorateur, liste de chaînes) avant
   d'envoyer quoi que ce soit.

**Critère :** l'adresse calculée est **identique** à celle obtenue à l'étape R5 de la
répétition (même clé, même nonce, même bytecode). Si elle diffère, un des trois a
changé : ne pas déployer.

#### Étape 11 — Financer la clé au strict nécessaire

**Critère :** le solde de la clé de déploiement couvre le coût du déploiement plus
20 %, et rien de plus.

#### Étape 12 — Déployer, par transaction ordinaire

Le déploiement est miné par le client **actuel** : il n'exige rien de la porte.

**Critère :** reçu de statut `1`, et l'adresse du contrat créé est **exactement**
celle publiée à l'étape 10.

#### Étape 13 — Vérifier le code déployé

```bash
cast code $ADDR --rpc-url $RPC_PROD | tee /tmp/code.hex | wc -c
cast keccak $(cat /tmp/code.hex)
```

**Critères, les trois :**
1. `eth_getCode` renvoie plus de 0 octet ;
2. le keccak du code vaut **exactement** le `$CODEHASH` relevé à R5 sur la chaîne
   jetable ;
3. `getMiningValidators()` en lecture seule se décode sans erreur.

Un seul de ces trois qui échoue : **ne pas continuer.** Le binaire de l'étape 17
épinglera ce codehash ; s'il est faux, la porte ne s'ouvrira jamais — ce qui est le
comportement sûr, mais autant le savoir maintenant.

---

### PHASE 3 — AMORÇAGE DU CONTRAT (étapes 14 à 16)

#### Étape 14 — Inscrire le validateur de genèse et déposer son enjeu

Le bloc 0 est figé : on ne peut pas pré-allouer un enjeu. Le dépôt est une
transaction ordinaire, depuis une adresse de trésorerie.

Deux propriétés **séparées**, à publier séparément :

- **son argent est engagé et saisissable** comme celui des autres ;
- **sa place est inconditionnelle** : elle ne dépend ni du solde déposé, ni d'un
  retrait, ni d'une sanction — parce que c'est aujourd'hui la seule clé de scellage
  détenue.

À écrire noir sur blanc dans le livre blanc : **une sanction du validateur de genèse
peut lui coûter des BOSA, jamais sa place.** `POBS.md §2` avertit déjà qu'un lecteur
qui découvre seul un validateur inéjectable le lira comme une dissimulation ;
l'asymétrie argent/place est plus subtile, donc plus dommageable si elle est tue.

**Nuance qui change tout après E.** Le verrou 2
(`require(sealerPresent, "genesis validator must remain a validator")`) vit dans le
contrat figé, et **après E le consensus ne lit plus ce contrat**. La permanence du
validateur de genèse cesse d'être une contrainte subie : elle devient un **choix** du
nouveau contrat. Il faut donc l'y réinscrire explicitement — ou l'abandonner
sciemment, en sachant que rien ne garantirait plus qu'un membre de l'ensemble
détienne une clé de scellage.

**Critère :** `getMiningValidators()` sur `$ADDR` renvoie exactement 1 adresse,
égale à `0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50`, et 1 clé de vote de 48 octets.

#### Étape 15 — Régler le minimum d'enjeu

1 000 BOSA = 0,000143 % de l'offre. Les 41 places coûtent 41 000 BOSA, soit 0,0059 %
— la plus petite adresse de trésorerie financée les achète 341 fois. Le minimum doit
donc pouvoir monter **sans nouvelle bifurcation** : c'est une variable d'état, pas
une constante Solidity (une constante exigerait un nouveau bytecode, donc un nouveau
codehash, donc un nouveau binaire).

Bornes et vitesses gravées dans le bytecode, donc non contournables :

- plancher **1 000 BOSA**, jamais franchissable vers le bas ;
- plafond, et un préavis d'au moins 48 h avant tout changement ;
- **facteur ×2 au maximum par changement** — aucun appel unique ne redessine
  l'ensemble ;
- **non-rétroactivité : une hausse ne peut JAMAIS évincer un validateur en place.**
  Chaque entrée mémorise le minimum en vigueur le jour de son admission, et
  l'éligibilité se teste contre lui. Sans cette règle, le réglage du minimum est un
  levier d'éviction de masse — donc un vecteur d'arrêt de chaîne déguisé en
  paramètre économique.

**Critère :** la valeur lue on-chain est celle décidée ; une simulation `eth_call`
d'une hausse au-delà du facteur 2 **échoue**.

#### Étape 16 — PORTE DE PARITÉ (contrôle C4)

Sur **3 blocs consécutifs**, comparer le retour **brut** (les octets, pas la valeur
décodée) de `getMiningValidators()` sur `0x…1000` et sur `$ADDR` :

```bash
for n in $(seq 3); do
  h=$(cast block-number --rpc-url $RPC_PROD)
  a=$(cast call 0x0000000000000000000000000000000000001000 "getMiningValidators()" --block $h --rpc-url $RPC_PROD)
  b=$(cast call $ADDR                                      "getMiningValidators()" --block $h --rpc-url $RPC_PROD)
  [ "$a" = "$b" ] && echo "bloc $h : IDENTIQUE" || { echo "bloc $h : DIVERGENT"; exit 1; }
  sleep 6
done
```

**Critère : 3 sur 3 identiques.** Tant que ce n'est pas vrai, **on ne fixe pas T.**

C'est ce contrôle qui rend la bascule vérifiable *avant* qu'elle ait lieu.

---

### PHASE 4 — LE BINAIRE BIFURQUÉ (étapes 17 à 21)

#### Étape 17 — Compiler avec le triplet épinglé

Le binaire embarque `(ADRESSE, CODEHASH, pobsTime = T)`, avec **T ≥ maintenant + 48 h**.

Points durs du patch, chacun adossé à une mesure :

- **`PobsTime *uint64`**, nom terminé par `Time`. `core/forkid` collecte les forks par
  réflexion sur ce critère : un autre nom ou un autre type sortirait silencieusement
  la porte de l'identifiant de fork, et supprimerait tout l'isolement mesuré au §0.2.
- **Position dans `CheckConfigForkOrder`.** Mesuré : une porte ajoutée dans la liste
  ordonnée fait échouer le contrôle sur la config Coinbosa réelle —
  « `unsupported fork ordering: bohrTime not enabled, but pascalTime enabled` » — et
  `core/genesis.go` appelle ce contrôle en dur : **tous les nœuds refusent de
  démarrer, validateur compris.** La déclarer `optional: true`, et l'insérer juste
  après `keplerTime` (`params/config.go:1564`), c'est-à-dire après le dernier fork
  réellement actif sur Coinbosa.
- **Le prédicat s'évalue sur le PARENT.** `getCurrentValidators` lit l'état de
  `header.ParentHash` (`parlia.go:1919-1921`), et ses deux appelants lui passent
  `header.ParentHash` / `header.Number - 1`. C'est le motif déjà établi du dépôt
  (`bohrFork.go` teste `IsBohr(parent.Number, parent.Time)` quand la lecture porte
  sur l'état du parent). Écrire la garde **à l'identique** dans `prepareValidators`
  et `verifyValidators` : une asymétrie entre les deux est une scission de consensus
  immédiate.
- **Trois gardes inconditionnelles** — pas sous `IsPobs`, elles valent aussi pour
  l'ancien contrat — insérées entre le décodage et la boucle `voteAddrMap`
  (`parlia.go:1960`) :

  ```go
  if len(valSet) != len(voteAddrSet) { return nil, nil, fmt.Errorf("pobs: longueurs inegales %d != %d", len(valSet), len(voteAddrSet)) }
  if len(valSet) == 0             { return nil, nil, errors.New("pobs: ensemble de validateurs vide") }
  if len(valSet) > 41             { return nil, nil, fmt.Errorf("pobs: %d validateurs > MAX_VALIDATORS", len(valSet)) }
  ```

  La première n'est pas décorative : le décodage ABI accepte **sans erreur** deux
  tableaux de longueurs différentes, et la boucle de `parlia.go:1960` **panique**
  alors — ce n'est pas une erreur rattrapée, c'est le processus qui tombe, sur chaque
  nœud qui importe le bloc.
- **`CheckCompatible`** : ajouter la garde `isForkTimestampIncompatible` pour
  `PobsTime`, sur le modèle de celle de Bohr. Sans elle, reculer une porte déjà
  franchie passerait inaperçu.
- **Journalisation** : au démarrage, une ligne `Info` inconditionnelle donnant le
  triplet (adresse, codehash, T) et l'état `ACTIVE` / `pending` ; au bloc
  d'activation, une ligne **unique** en `Warn` nommant l'adresse lue, le nombre de
  validateurs et la comparaison avec l'ancien contrat. Le premier jour, les deux
  contrats renvoient le même ensemble : sans cette ligne, la bascule est
  littéralement invisible.
- **Refus de démarrer (*fail-closed*)** : si `PobsTime != nil` et qu'on est déjà dans
  la fenêtre ou au-delà, vérifier code non vide + codehash attendu à la tête ; sinon
  `log.Crit`. Mieux vaut un nœud qui refuse de démarrer qu'un nœud qui démarre et
  fige la chaîne 16 minutes plus tard.
- **Ne pas** inscrire l'adresse du contrat d'enjeu dans la carte `systemContracts` :
  cette conception n'émet aucune transaction système vers lui. Le jour où ce serait
  nécessaire, la garde devra être conditionnée par le fork, sinon un binaire neuf
  reclasserait rétroactivement d'anciennes transactions.

**Critère :** `go build ./... ; echo "EXIT=$?"` → `EXIT=0`.

#### Étape 18 — Rejouer les bancs de non-régression

```bash
cd /Users/protocole/repo
go test ./consensus/parlia/ ./core/forkid/ ./params/... ./core/ -count=1
```

**Critères, tous obligatoires :**

1. `TestCoinbosaAddSecondValidatorHaltsChain` — PASS ;
2. ordre des forks sur la config `genesis-coinbosa.json` **réelle** — PASS ;
3. forkid : `parTemps` passe de `[]` à `[T]` ; appairage conservé avant T, rompu
   dans les deux sens après ;
4. longueurs inégales → **erreur nommée**, jamais de panique ;
5. ensemble vide → erreur nommée ;
6. plus de 41 élus → erreur nommée ;
7. porte évaluée sur le **parent** : un bloc d'epoch à cheval sur T lit le bon
   contrat ;
8. gaz de `getMiningValidators()` à 41 élus **< 5 000 000** — dix fois sous le plus
   bas plafond RPC raisonnablement configurable ;
9. classement canonique : mêmes candidats, dix ordres d'insertion, dix classements
   identiques octet pour octet.

#### Étape 19 — Déployer le binaire sur le nœud RPC EN PREMIER

```bash
systemctl stop coinbosa-node
# installer le nouveau binaire
systemctl start coinbosa-node
```

Le nœud RPC ne produit rien. S'il refuse de démarrer, on l'apprend **sans avoir
touché au seul producteur de blocs**.

**Critères :** C3 (hash **et** stateRoot du bloc 0 identiques aux valeurs de
`genesis-reference.json`, à l'octet près) ; la ligne `Info` du triplet apparaît au
journal ; C6 (`admin.peers.length ≥ 1`).

#### Étape 20 — Déployer sur le validateur EN DERNIER

```bash
systemctl stop coinbosa-validator
# installer le nouveau binaire
systemctl start coinbosa-validator
```

**Arrêt par `systemctl stop` uniquement** — jamais `kill -9`, jamais `pkill geth`,
jamais une coupure. `AGENTS.md` : `--pathdb.sync` est propagé dans deux structures
puis jamais lu par `triedb/pathdb` ; un arrêt sale fait repartir le nœud au dernier
arrêt **propre**, ce qui produit une réorganisation — au pire moment.

**Critères :** C2 (premier bloc en ≤ 15 s, `Δh ≥ 12` en 60 s) ; C3 ; C6 ;
le triplet affiché est **identique** à celui du nœud RPC.

#### Étape 21 — Observer 2 blocs d'epoch franchis AVANT T

**Critère C5 :** entre deux blocs d'epoch consécutifs, `Δh = 200` exactement,
`Δt = 1 000 s ± 50 s`, et l'extraData mesure `32 + 1 + 68·N + 65` octets — soit
**166** pour N = 1.

C'est ce palier qui attrape une erreur d'intégration du binaire pendant qu'elle est
encore gratuite.

---

### PHASE 5 — LE FRANCHISSEMENT (étapes 22 à 26)

#### Étape 22 — Lecture en aveugle, 24 h

Dans la fenêtre `[T − 24 h, T)`, à chaque bloc d'epoch, le client lit les **deux**
contrats et journalise en `Info` : adresses, nombre, égalité ou non.

**Critère :** **288 comparaisons** (24 h ÷ 1 000 s ≈ 86 par jour et par contrat ;
viser au minimum 80) **toutes égales**. Une seule divergence → ne pas franchir.

Le basculement cesse alors d'être un saut : au moment où il arrive, on a déjà des
dizaines de preuves que le nouveau contrat répond correctement, sur la chaîne réelle.

#### Étape 23 — Décision GO / NO-GO

**GO exige les six, sans exception :**

| | Condition |
|---|---|
| 1 | R0 → R14 tous verts (étape 8) |
| 2 | C4 = 3/3 (étape 16) |
| 3 | C5 passé deux fois avec le nouveau binaire (étape 21) |
| 4 | lecture en aveugle sans divergence (étape 22) |
| 5 | `admin.peers.length ≥ 1` sur le validateur **et** sur le nœud RPC |
| 6 | sauvegarde de la clé de scellage restaurée et **vue sceller** (étape 3) |

Une seule manque : **NO-GO**. Repousser T — c'est encore possible tant que la tête ne
l'a pas atteint (voir §5).

#### Étape 24 — Franchir T

Rien à faire : la porte est temporelle. Surveiller.

**Critères, à la seconde de T :**
- `admin.peers.length ≥ 1` sur les deux nœuds. **Zéro pair à cet instant précis
  identifie exactement un nœud resté sur l'ancien binaire** : le mettre à jour
  immédiatement ;
- la production de blocs ne s'interrompt pas : `Δh = 24 ± 2` sur 120 s.

#### Étape 25 — Identifier E et vérifier le no-op

E = premier multiple de 200 dont le parent est au-delà de T.

**Critères C7, les quatre :**
1. E est produit **au plus tard T + 1 000 s** ;
2. le nombre de validateurs à E vaut **1** ;
3. les octets de validateurs de E sont **identiques** à ceux du bloc d'epoch
   précédent ;
4. la ligne `Warn` d'activation est présente **une seule fois** au journal.

**C'est la décision centrale de cette procédure : à E, le contrat d'enjeu renvoie
exactement le même ensemble que `0x…1000`.** Changer de *source de vérité* et changer
d'*ensemble* sont deux événements distincts, dont un seul est réversible. Les fusionner,
c'est franchir simultanément deux pièges dont le second (§4) tue silencieusement.
Séparés, chacun s'observe et se rattrape.

#### Étape 26 — Survivre deux epochs

**Critère C8 :** `Δh = 60 ± 3` sur les 300 s qui suivent E, **et** E+200 atteint en
`1 000 s ± 50 s`.

C'est le seul contrôle qui prouve que le **second** epoch passe aussi — le premier
peut passer par chance, sur un cache encore chaud.

---

### PHASE 6 — L'EXTENSION 1 → N (étapes 27 à 31)

**Opération séparée, postérieure, et jamais le même jour que la bascule.**

#### Étape 27 — Provisionner l'entrant

Hébergeur distinct, zone distincte, système autonome distinct.

**Critère :** le nœud entrant est synchronisé à **≤ 2 blocs** de la tête du
validateur, mesuré : `eth_syncing == false` et `|h_entrant − h_validateur| ≤ 2`.

#### Étape 28 — Preuve de vivacité de l'entrant

Mettre l'entrant en `--mine` **avant** qu'il soit membre.

**Critère :** son journal affiche, à chaque bloc, `unauthorized validator: 0x<sa
propre adresse>`. C'est la preuve la plus proche de « vu sceller » qu'on puisse
obtenir d'un non-membre : elle établit qu'il mine, qu'il est synchronisé, et qu'il
utilise bien cette adresse.

*Cette ligne exacte est à confirmer d'abord sur la chaîne jetable : `miner/` est
absent du dépôt et je n'ai pas pu lire comment le mineur la remonte (§0.6).*

#### Étape 29 — Dépôt d'enjeu de l'entrant

**Critères :** reçu de statut `1` ; l'entrant apparaît au classement du contrat ;
sa clé de vote fait exactement **48 octets** et n'est pas nulle.

#### Étape 30 — Élection, un entrant par palier

**Combien peut-on ajouter d'un coup ?** Soit `S₀` le nombre de scelleurs **distincts
et prouvés en ligne** de l'ensemble courant. Aujourd'hui **S₀ = 1**.

- **Borne prouvée**, sans aucune hypothèse sur les entrants : il faut
  `⌊N₁/2⌋+1 ≤ S₀`, donc avec `S₀ = 1` → **`N₁ ≤ 1`**. Autrement dit : avec un seul
  scelleur détenu, le nombre de validateurs qu'on peut ajouter en sécurité *prouvée*
  est **zéro**.
- **Borne praticable**, sous l'hypothèse « au plus un entrant défaille » :
  `N₁ ≤ 2·S₀ + 1` → **`N₁ ≤ 3`**, et seulement si les étapes 27 et 28 sont vertes
  pour chaque entrant.

**Règle arrêtée : ne jamais viser `N₁` tel que `⌊N₁/2⌋+1 > S₀ + 1`.**
`S₀ = 1` → `N₁ ≤ 3`. `S₀ = 3` constatés → `N₁ ≤ 7`. Et **un seul entrant par palier
tant que `S₀ < 3`**, avec un epoch complet d'observation entre deux.

**Critère C9 :** le champ `miner` des blocs E′+1, E′+2, E′+3 porte **3 valeurs
distinctes**. Une adresse répétée, ou une hauteur figée → démarrer immédiatement les
nœuds manquants.

#### Étape 31 — Confirmer sur la durée

**Critère :** `Δh` conforme sur **30 epochs consécutifs** (≥ 6 000 blocs, ≈ 8 h 20),
chaque membre ayant scellé au moins un tour.

**Rien n'est publié avant que cette étape soit verte** — ni « PoBS est en service »,
ni le nombre de validateurs. Rétracter coûte plus cher que le silence, et `POBS.md §7`
rappelle que tant que ce document n'est pas exécuté, le consensus de Coinbosa est une
preuve d'autorité. Publier au même moment l'asymétrie argent/place du validateur de
genèse (étape 14) : la taire est ce qui se lira comme une dissimulation.

---

## 3. La répétition sur chaîne jetable — R0 à R14

Exécutable telle quelle une fois l'étape 1 verte. Un epoch dure **1 000 s** :
R4 → R14 traverse ≈ 10 epochs, soit **≈ 3 h de chaîne**, hors préparation.

Ne jamais afficher ni versionner une clé ou un mot de passe. `.gitignore` couvre
`node*/`, `keystore/`, `UTC--*`, `*.key`, `pw.txt`, `.env*` — ne pas contourner.

### R0 — Arbre client — BLOQUANT

```bash
cd /Users/protocole/repo
go build ./... ; echo "EXIT=$?"
make geth
./build/bin/geth version | head -3
```

**Critère :** `EXIT=0` **et** `build/bin/geth` exécutable.
**Aujourd'hui : `EXIT=1`** (§0.3).

### R1 — Trois clés jetables

```bash
export DEV=/tmp/coinbosa-repet
mkdir -p $DEV/n1 $DEV/n2 $DEV/n3
umask 077 && printf '%s' "<mot de passe jetable>" > $DEV/pw.txt && chmod 600 $DEV/pw.txt
for i in 1 2 3; do
  /Users/protocole/repo/build/bin/geth account new --datadir $DEV/n$i --password $DEV/pw.txt
done
```

**Critère :** 3 adresses **distinctes**, notées `$V1 $V2 $V3`.

### R2 — Genesis jetable

```bash
cd /Users/protocole/repo/coinbosa
ALLOW_DEV=1 VALIDATOR=$V1 node scripts/build-genesis.js
grep -c '"coinbosaDev"' genesis/genesis-coinbosa-dev.json
ALLOW_DEV_SUPPLY=1 GENESIS=genesis/genesis-coinbosa-dev.json node scripts/check-supply.js
```

**Critères :** le `grep` vaut **1** ; `check-supply.js` sort en `0` avec un total de
700 000 000 BOSA au wei près.

`genesis/genesis-coinbosa-dev.json` est gitignoré : il n'existe pas dans le dépôt et
se régénère ici. `scripts/start-node.sh:50` l'initialise sans le créer — c'est
pourquoi cette étape le précède.

En mode dev, `build-genesis.js:82` crédite `$V1` du premier poste (20 % = 140 000 000
BOSA) : il a de quoi payer le gaz. Et `build-genesis.js:62` pose `GOVERNOR = VALIDATOR` :
une seule clé pilote tout — ce qui est exactement ce qu'on veut sur une chaîne jetable,
et exactement ce que le script **refuse** en production (l. 52-53).

### R3 — Trois nœuds, isolés du réseau réel

```bash
cd /Users/protocole/repo
for i in 1 2 3; do
  ./build/bin/geth init --datadir $DEV/n$i coinbosa/genesis/genesis-coinbosa-dev.json
done

./build/bin/geth --datadir $DEV/n1 --networkid 262620 --port 31001 --ipcdisable \
  --http --http.addr 127.0.0.1 --http.port 18545 \
  --http.api eth,net,web3,parlia,admin,debug \
  --mine --miner.etherbase $V1 --unlock $V1 --password $DEV/pw.txt --allow-insecure-unlock \
  --nodiscover --syncmode full --gcmode archive --verbosity 3 > $DEV/n1.log 2>&1 &

# n2 et n3 : mêmes options, ports 31002/18546 et 31003/18547, SANS --mine,
# reliés par admin_addPeer depuis n1.
```

`--networkid 262620` ≠ 26262 : **isolation p2p dure**, en plus d'un hash de bloc 0
différent. Le `chainId` reste 26262 — il entre dans le hash de scellage, le changer
perdrait la fidélité de la répétition. `--gcmode archive` : les `eth_call`
historiques du contrôle de parité exigent l'état des blocs passés.

**Critère :** `admin.peers.length == 2` sur chacun des trois nœuds.

### R4 — Référence AVANT toute modification

```bash
cd /Users/protocole/repo/coinbosa
RPC=http://127.0.0.1:18545 node scripts/check-blocktime.js
RPC=http://127.0.0.1:18545 node scripts/check-epoch.js
RPC=http://127.0.0.1:18545 ALLOW_DEV_HASH=1 node scripts/check-genesis-hash.js
```

**Critères :** temps de bloc `5 s ± 0,5` (le script échoue au-delà) ; bloc 200 franchi ;
extraData de **166 octets** pour 1 validateur ; hash du bloc 0 **noté** (en mode dev il
est affiché, jamais comparé — il diffère par construction de celui de la production).

### R5 — Déployer le contrat d'enjeu, clé dédiée au nonce 0

**Critères :** `eth_getCode` non vide ; `$ADDR` et `$CODEHASH = keccak256(code)`
**notés**. Ces deux valeurs devront être reproduites **à l'identique** en production
(même clé de déploiement, même nonce 0, même bytecode) : c'est ce qui rend le binaire
de répétition et le binaire de production identiques à T près.

### R6 — Amorcer, puis PORTE DE PARITÉ

Inscrire `$V1`, déposer l'enjeu, régler le minimum.

**Critère C4 :** sur **3 blocs consécutifs**, le retour **brut** de
`getMiningValidators()` est **identique** sur `0x…1000` et sur `$ADDR`. **3 sur 3.**

### R7 — Les neuf bancs du contrat

À faire passer avant de compiler quoi que ce soit :

1. `vals.length == votes.length` sur 10 000 états aléatoires, avec la boucle
   `voteAddrMap` de `parlia.go:1960` rejouée telle quelle ;
2. chaque `votes[i]` fait exactement 48 octets, sur les mêmes états ;
3. le retour se dépaquette sans erreur par `UnpackIntoInterface` ;
4. classement canonique : dix ordres d'insertion, dix classements identiques ;
5. l'ensemble publié ne dépasse jamais ce que la population en ligne peut soutenir ;
6. aucun état atteignable ne fait revert `getMiningValidators` (fuzz) ;
7. gaz de la lecture à 41 validateurs **< 5 000 000** ;
8. une fausse preuve de double signature est **refusée** ;
9. l'ensemble rendu contient toujours au moins un membre.

**Critère :** 9 sur 9.

### R8 — Binaire bifurqué

`pobsTime = maintenant + 1 800 s` (≈ 2 epochs d'observation). Redémarrer **n2, n3,
puis n1**, par arrêt propre.

**Critères :** hash **et** stateRoot du bloc 0 inchangés sur les 3 nœuds ;
`admin.peers.length == 2` partout (appairage conservé avant T — mesuré au §0.2) ;
`Δh = 24 ± 2` en 120 s.

### R9 — Deux epochs AVANT T avec le nouveau binaire

**Critère C5 :** `Δh = 200` exactement, `Δt = 1 000 s ± 50 s`, extraData 166 octets.

### R10 — Franchissement

**Critères C7 puis C8.** En particulier : les octets de validateurs de E doivent être
**identiques** à ceux du bloc d'epoch précédent — la bascule est un no-op.

### R11 — Préparer l'extension : V2 et V3 en `--mine`, sans être membres

**Critère :** le journal de chaque entrant affiche
`unauthorized validator: 0x<son adresse>`.
Constaté ici, puis **exigé en production** (étape 28) avant toute extension.

### R12 — Extension 1 → 3 par le contrat d'enjeu

**Critère C9 :** 3 valeurs de `miner` distinctes sur 3 blocs consécutifs.

### R13 — TEST DESTRUCTIF — l'étape la plus instructive de toute la répétition

```bash
# 1. arrêt PROPRE de n3 : N=3, quorum 2, il reste V1+V2  -> la chaîne DOIT tenir
# 2. arrêt PROPRE de n2 : il ne reste que V1             -> la chaîne DOIT s'arrêter
# 3. redémarrage de n2                                   -> la chaîne DOIT repartir
```

**Critères :**
- après (1) : `Δh = 24 ± 2` en 120 s ;
- après (2) : hauteur **figée pendant ≥ 60 s** et
  `grep -c "Signed recently" $DEV/n1.log` **> 0** ;
- après (3) : reprise en **≤ 30 s**.

Ce test établit la distinction à publier, et elle n'est pas cosmétique : un arrêt
causé par un scelleur **hors ligne mais existant** se répare en le rallumant ; un
arrêt causé par un membre **dont personne ne détient la clé** ne se répare pas du
tout.

### R14 — Répéter le RETOUR EN ARRIÈRE

Sur une **seconde** chaîne jetable : fixer T, installer le binaire bifurqué, puis
réinstaller l'ancien **avant** T.

**Critères :** hauteur ininterrompue (`Δh = 24 ± 2` de part et d'autre du
remplacement) ; extraData du bloc d'epoch suivant inchangée ; hash du bloc 0
inchangé.

On répète la marche arrière **avant** d'en avoir besoin. C'est le seul moment où on
peut se permettre de la découvrir.

---

## 4. Le piège 1 → N

### 4.1 Le test, et ce qu'il garantit exactement

```
$ cd /Users/protocole/repo && go test ./consensus/parlia/ -run Coinbosa -v 2>&1 | tail -30
=== RUN   TestCoinbosaAddSecondValidatorHaltsChain
N=1  minerHistoryCheckLen=0  SignRecently(V1)=false
N=2  minerHistoryCheckLen=1  SignRecently(V1)=true  SignRecently(V2)=false  inturn(201)=0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50
=> bloc 201 : V1 refuse de sceller (Seal: "Signed recently, must wait for others"),
   V2 n'a pas de noeud => arret de la chaine.
--- PASS: TestCoinbosaAddSecondValidatorHaltsChain (0.00s)
PASS
ok  	github.com/ethereum/go-ethereum/consensus/parlia	2.349s
```

Le test **compile et passe**. Fichier :
`/Users/protocole/repo/consensus/parlia/coinbosa_halt_repro_test.go`.

**Ce qu'il garantit.** Il reconstruit l'état du snapshot juste après le bloc d'epoch
200 sur une chaîne Coinbosa (Bohr inactif, `TurnLength = 1`, epoch 200,
`Recents = {200: V1}`), puis interroge les **vraies** fonctions du moteur —
`Snapshot.minerHistoryCheckLen()` (`snapshot.go:243`) et `Snapshot.SignRecently()`.
Il établit deux faits :

1. à N=1, `minerHistoryCheckLen = 0` : le validateur unique **n'est pas** bloqué au
   bloc 201 ;
2. dès que l'ensemble passe à 2, `minerHistoryCheckLen = 1` et V1 **est** bloqué au
   201 — alors même que la garde `sealerPresent` du contrat figé est satisfaite.

Le bloc 201 revient donc à V2. Et `Seal()` ne renvoie **pas** d'erreur dans ce cas :
il journalise `"Signed recently, must wait for others"` (`parlia.go:1744`) et
**retourne `nil`**. Aucune erreur, aucune panique, aucune alerte : la hauteur cesse
simplement d'avancer.

**Ce qu'il ne garantit PAS.** Il ne fait tourner aucun nœud, ne mine rien, ne rejoue
pas `Snapshot.apply()` sur de vrais en-têtes. C'est une preuve de la **règle
arithmétique** et de son effet sur `SignRecently`, pas une reproduction de bout en
bout. La reproduction de bout en bout est **R13**, sur chaîne jetable, exprès.

### 4.2 La table du quorum, mesurée pour chaque taille

```
$ go test ./consensus/parlia/ -run TestPobsQuorumTableForEachSetSize -v
N= 1 -> 1    N= 2 -> 2    N= 3 -> 2    N= 4 -> 3    N= 5 -> 3    N= 6 -> 4
N= 7 -> 4    N= 8 -> 5    N= 9 -> 5    N=10 -> 6    N=11 -> 6    N=12 -> 7
   (scelleurs DISTINCTS et EN LIGNE exigés)
```

C'est `⌊N/2⌋+1`, en division **entière**.

**1 → 3 n'est pas plus sûr que 1 → 2** face au scénario « aucun entrant ne
fonctionne » : les deux exigent **2** scelleurs distincts et en ligne, et nous n'en
détenons qu'un. La parité ne protège de rien — `AGENTS.md` le dit déjà, la table le
chiffre.

*Nuance que la table rend visible et que je ne masque pas :* 1 → 3 tolère la
défaillance d'**un** entrant (2 requis, 2 restants), là où 1 → 2 n'en tolère aucune.
C'est un gain réel. Il ne couvre pas le cas qui tue.

### 4.3 Ce qui protège, et rien d'autre

Un entrant ne peut pas avoir scellé avant : **on ne scelle qu'une fois membre.**
`scripts/rotate-validators.js` en tire la conséquence — il **bloque** toute rotation
comportant un entrant tant que `JE_COMPRENDS_LE_RISQUE=1` n'est pas posé (l. 127-134),
et il simule la transaction par `eth_call` avant de laisser faire quoi que ce soit
(l. 172-186). Ne pas contourner cette garde.

Ce que l'on peut rendre **vérifiable**, et qui est exigé aux étapes 27 à 30 :
synchronisation à ≤ 2 blocs ; `unauthorized validator: 0x<son adresse>` au journal de
l'entrant en `--mine` ; un entrant par palier tant que `S₀ < 3` ; trois `miner`
distincts sur trois blocs après l'extension.

### 4.4 Deux pièges voisins, mesurés eux aussi

- **Ensemble vide.** Le décodage réussit **sans erreur** avec `len(valSet) = 0`
  (§0.2). Le bloc d'epoch est ensuite refusé par tous les nœuds, **y compris son
  propre producteur** : `verifyHeader` (`parlia.go:622-623`) rend
  `errInvalidSpanValidators`, soit exactement
  `invalid validator list on sprint end block`. D'où la garde `len(valSet) == 0` de
  l'étape 17, et le plancher structurel du contrat (étape 9).
- **Contrat absent.** `eth_call` sur un compte sans code renvoie 0 octet **sans
  erreur** ; c'est le décodage ABI qui échoue :
  `abi: attempting to unmarshal an empty string while arguments are expected` (§0.2).
  Côté production, `prepareValidators` → `Prepare` échoue : le bloc d'epoch n'est
  jamais construit. C'est la raison d'être de la porte double (§1).

---

## 5. Réversible, et irréversible

### 5.1 Réversible

| Quoi | Jusqu'à quand | Comment |
|---|---|---|
| Le binaire client | **avant T** | réinstaller le binaire précédent (empreinte SHA-256 de l'étape 2), redémarrage propre. La chaîne n'a jamais lu le nouveau contrat : la bascule n'a lieu qu'à un bloc d'epoch dont le parent est au-delà de T |
| Le contrat d'enjeu lui-même | **avant T** | c'est un contrat ordinaire ; il peut rester déployé et alimenté sans le moindre effet. Le consensus l'ignore |
| La valeur de T | tant qu'aucun nœud ne l'a franchie | recompiler avec une autre valeur. La garde `isForkTimestampIncompatible` de l'étape 17 refuse de reculer une porte déjà franchie par la tête ; **sans elle, la modification passerait inaperçue** |
| L'ordre des étapes 1 à 23 | intégralement | rien n'est encore engagé publiquement |

### 5.2 Réversible en théorie, pas en pratique : la fenêtre T → E

Elle dure **≤ 1 000 s**. Formellement, rien n'a encore été lu. Mais la **partition p2p
a déjà commencé** — mesuré (§0.2) : à la seconde de T, l'ancien binaire juge le
nouveau `local incompatible or needs update`, et le nouveau juge l'ancien
`remote needs update`, dans les deux sens. Revenir en arrière exigerait de redescendre
*tous* les nœuds sur l'ancien binaire dans ce délai.

**Traiter T comme le point de non-retour.**

### 5.3 Irréversible après E

- **Le bloc E est scellé et importé.** Son `extraData` enregistre un ensemble de
  validateurs dérivé du nouveau contrat. Rien ne le dé-scelle.
- **Tout nœud resté sur l'ancien binaire est déconnecté à T**, dans les deux sens. Il
  ne revient pas sans mise à jour. Si c'est le nœud RPC, **l'explorateur devient muet
  au moment le plus critique de l'opération** — d'où l'ordre de l'étape 19.
- **Annuler PoBS après E n'est pas un retour en arrière : c'est une nouvelle
  bifurcation en avant** (une seconde porte désactivant la première). Elle exige que
  la chaîne **produise encore des blocs**. Si la chaîne est arrêtée, il n'y a plus ni
  transaction ni gouvernance : il ne reste qu'un remplacement de binaire coordonné
  hors chaîne **plus** un recul manuel de la tête du validateur sous E — ce qui
  produit une **réorganisation**.
  Avec un validateur, un nœud RPC, aucune bourse et aucun indexeur tiers
  (`POBS.md §3`), c'est survivable. Le jour où il y a une bourse, ça ne l'est plus.
  **C'est l'argument le plus fort pour le faire maintenant.**
- **L'argent des tiers.** Avec 49 jours de déblocage, tout BOSA immobilisé par un
  validateur tiers ne peut lui revenir avant 49 jours, quoi qu'il arrive. **Un retour
  en arrière du client ne rembourse rien.**
- **La parole publique.** Rétracter « PoBS est en service » coûte plus cher que de ne
  l'avoir jamais dit. D'où l'étape 31.

### 5.4 Irréversible après l'extension 1 → N

C'est le second point de non-retour, et il est indépendant du premier. Si les
entrants ne scellent pas, la chaîne s'arrête au bloc d'epoch suivant, **sans erreur
ni panique** (§4.1), et **aucune transaction corrective ne peut plus être minée** :
l'opération ne se défait pas on-chain.

Réparation possible **uniquement** si un membre de l'ensemble dispose encore d'un
nœud en ligne : le rallumer suffit (R13, cas 3). Si le membre manquant est une
adresse dont **personne ne détient la clé**, il n'y a pas de réparation.

---

## 6. Les contrôles d'arrêt

À quel moment on renonce, et sur quel signal. Aucun critère ne s'énonce « vérifier
que ça marche » : chacun est un nombre qu'on compare, ou un octet qu'on égale.

| # | Quand | Contrôle | Critère de succès | Sur échec |
|---|---|---|---|---|
| **C1** | en continu | 2 × `eth_blockNumber` à 120 s d'écart | `Δh = 24 ± 2` (5 s/bloc). `Δh < 22` = anomalie | **arrêt de la procédure** |
| **C2** | après chaque redémarrage | reprise de la production | 1ᵉʳ bloc ≤ 15 s ; `Δh ≥ 12` en 60 s | restaurer le binaire précédent |
| **C3** | après chaque nouveau binaire, sur **chaque** nœud | `eth_getBlockByNumber("0x0")` | `hash = 0x8dcdadc247a98f33728cae944e20ce7c49c74b35cfba31495f85e98979018da6` et `stateRoot = 0x93682eb9182a55531d47014b76a285b45d3e720a2951f9ffbdc67f52995f8c03`, à l'octet près | mauvais binaire ou mauvais genesis — **ne pas démarrer** |
| **C4** | **avant** de fixer T | parité des deux contrats | retour **brut** identique sur `0x…1000` et `$ADDR`, **3 blocs consécutifs, 3/3** | ne pas fixer T |
| **C5** | avant T, nouveau binaire | 2 blocs d'epoch consécutifs | `Δh = 200` exactement ; `Δt = 1 000 s ± 50 s` ; extraData `32+1+68·N+65` → **166** pour N=1 | ne pas franchir T |
| **C6** | avant **et** après T | `admin.peers.length` sur validateur et nœud RPC | **≥ 1** dans les deux cas | après T, un pair manquant = un nœud resté sur l'ancien binaire — le mettre à jour immédiatement |
| **C7** | à la bascule | E = 1ᵉʳ multiple de 200 dont le parent est au-delà de T | E produit **au plus tard T + 1 000 s** ; N à E = **1** ; octets de validateurs **identiques** à l'epoch précédent ; ligne `Warn` d'activation présente **une fois** | bascule non conforme — préparer le remplacement de binaire |
| **C8** | après la bascule | survie sur **deux** epochs | `Δh = 60 ± 3` sur les 300 s après E ; **et** E+200 atteint en `1 000 s ± 50 s` | seul contrôle qui prouve que le **second** epoch passe aussi |
| **C9** | à l'extension | champ `miner` des blocs E′+1 … E′+3 | **3 valeurs distinctes** | une adresse répétée, ou hauteur figée → démarrer immédiatement les nœuds manquants |
| **C10** | tout le temps | mode d'arrêt | `systemctl stop` **uniquement** | `AGENTS.md` : `--pathdb.sync` n'est jamais lu par `triedb/pathdb` ; un arrêt sale repart au dernier arrêt **propre** |
| **C11** | avant T | triplet épinglé | validateur et nœud RPC affichent le **même** (adresse, codehash, T) | deux T différents = partition programmée — ne pas franchir |
| **C12** | fenêtre `[T−24 h, T)` | lecture en aveugle des deux contrats | **≥ 80 comparaisons, toutes égales** | une seule divergence → NO-GO |

### 6.1 Les trois signaux qui font renoncer immédiatement, sans délibérer

1. **`C1` rouge à n'importe quel moment** — la chaîne n'avance plus. Ne rien envoyer,
   ne rien redémarrer à la hâte : diagnostiquer d'abord, un redémarrage sale
   (`C10`) transformerait un arrêt en réorganisation.
2. **`C4` autre que 3/3** — les deux contrats ne disent pas la même chose. La bascule
   ne serait pas un no-op, et ce qui devait être un changement de source de vérité
   deviendrait aussi un changement d'ensemble.
3. **`C6` à zéro pair après T** — un nœud est resté sur l'ancien binaire et vient
   d'être éjecté du réseau. Si c'est le nœud RPC, on vient de perdre la seule vue
   extérieure au moment où on en a le plus besoin.

---

## 7. Ce que ce document ne couvre pas

- **Le contenu du contrat d'enjeu.** Sa conception (état, élection, sanction,
  gouvernance) est un autre document. Ici, il n'apparaît que par ses obligations :
  ne jamais revert sur le chemin de lecture, longueurs égales, clés de 48 octets,
  au moins un membre, pas de `SELFDESTRUCT`, pas de proxy, pas d'`immutable`.
- **Les trois blocages de `POBS.md §6`** — clé de scellage non sauvegardée,
  gouverneur en clé simple, treize adresses de trésorerie dérivées d'une seule
  graine. Seul le premier est traité ici (étape 3), parce qu'il conditionne la
  survie de la chaîne pendant l'opération. Les deux autres restent entiers, et
  aucune étape de ce document ne les corrige.
- **Toute affirmation sur le mineur.** `miner/` est absent du dépôt (§0.3) : les
  lignes de journal citées aux étapes 28 et R11 sont à confirmer sur la chaîne
  jetable, jamais à supposer.
