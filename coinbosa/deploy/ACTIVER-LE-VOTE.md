# Activer le vote de finalité rapide

> **État au 2026-09-03.** Toutes les mesures de ce document ont été relevées le
> 2026-09-03 sur `https://explorer.coinbosa.com/rpc`, **en lecture seule**
> (`eth_blockNumber`, `eth_getBlockByNumber`, `eth_call`, `eth_estimateGas`).
> Aucune transaction n'a été émise, aucun accès au serveur de production n'a eu
> lieu, aucune des étapes du § 4 n'a été exécutée.
>
> Les références de code renvoient à l'arbre `/Users/protocole/repo` (client geth
> Coinbosa) et à `/Users/protocole/repo/coinbosa` (chaîne, contrats, déploiement).

---

## 0. Pourquoi ce document existe

`finalized` et `safe` sont les deux étiquettes qu'une place d'échange interroge pour
décider qu'un dépôt est irréversible. Sur Coinbosa Chain, elles répondent **bloc 0**.

La finalité rapide n'est pas absente du réseau : elle est **activée dans la
configuration** et **inutilisée**. `lubanBlock` et `platoBlock` valent tous deux `0`
dans `genesis/genesis-coinbosa.json` — le mécanisme est en place depuis le premier
bloc. Ce qui manque tient en une phrase : **la clé de vote inscrite pour l'unique
validateur vaut 48 octets nuls**, et une clé nulle ne correspond à aucun porteur.

Ce document décrit la remise en service de ce mécanisme : quelle clé créer, où la
déclarer, comment vérifier que ça marche, et comment revenir en arrière.

**Aucune clé privée et aucune phrase de récupération n'apparaît dans ce document.**
Les étapes qui manipulent un secret sont marquées **ÉDITEUR SEUL** et doivent être
exécutées par l'éditeur, sur la machine concernée, sans témoin et sans copier-coller
vers un canal partagé.

---

## 1. Ce qui ne marche pas aujourd'hui

**En trois phrases.** La tête de chaîne est au bloc **455 369** tandis que `finalized`
et `safe` renvoient tous deux le bloc **0**, de hash
`0x8dcdadc247a98f33728cae944e20ce7c49c74b35cfba31495f85e98979018da6` — le genesis
(mesuré ce jour par RPC). La cause immédiate est dans les en-têtes : le dernier bloc
d'epoch, le **455 200**, porte 166 octets d'`extraData` qui se décomposent en 32 de
vanité + 1 compteur + 20 d'adresse de validateur + **48 octets de clé BLS tous nuls**
+ 65 de sceau, soit **zéro octet d'attestation** ; faute d'attestation,
`snap.Attestation` reste `nil` et `parlia.GetJustifiedNumberAndHash` retourne
explicitement le hash du bloc 0 (`consensus/parlia/parlia.go:2227-2232`,
`GetFinalizedHeader` fait de même à `:2254-2256`). La cause racine est on-chain :
`eth_call` sur le contrat système `0x…1000`, méthode `getMiningValidators()`
(sélecteur `0x4df6e0c3`), rend **un** validateur — `0x3986d6b31ec55043ceaaf25f5ddea53517cbba50`
— et une adresse de vote de **48 octets nuls**.

### 1.1 Le relevé, tel quel

```
tete (eth_blockNumber)     455369   (0x6f2c9)
finalized                  bloc 0   0x8dcdadc2…9018da6
safe                       bloc 0   0x8dcdadc2…9018da6
bloc d'epoch 455200        extraData = 166 octets
  vanite (32)              db8301070688373331356634326189676f312e32352e3132836c696e55419176
  compteur                 1
  validateur (20)          0x3986d6b31ec55043ceaaf25f5ddea53517cbba50
  cle BLS (48)             000000…000000     <- nulle
  attestation              0 octet
  sceau (65)               present
```

Contrat `0x0000000000000000000000000000000000001000`, lectures directes :

| Appel | Sélecteur | Réponse |
|---|---|---|
| `GOVERNOR()` | `0x6dc0ae22` | `0x1eEF3830833d83aCd3152A511853fd04A0b4082a` |
| `INITIAL_VALIDATOR()` | `0x258718a7` | `0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50` |
| `numOfValidators()` | `0x1e526e45` | `1` |
| `alreadyInit()` | `0xa78abc16` | `true` |
| `voteAddresses(0)` | `0x498b77f3` | 48 octets, tous nuls |
| solde du gouverneur | — | 1 000 BOSA |
| **nonce du gouverneur** | — | **0** |

> Le nonce `0` signifie que **la clé du gouverneur n'a jamais émis la moindre
> transaction sur cette chaîne**. Le chemin de signature de l'étape 6 n'a donc jamais
> été exercé. C'est à considérer avant de le faire pour la première fois sur une
> transaction qui compte.

### 1.2 Pourquoi lancer le nœud avec `--vote` ne suffirait pas

C'est le point central, et c'est celui qu'on rate le plus souvent.

Pour qu'un vote soit produit, `core/vote/vote_manager.go:115-214` exige **six
conditions cumulatives** :

| # | Condition | Où |
|---|---|---|
| 1 | le téléchargeur n'est pas en cours de synchronisation | `:117-132` |
| 2 | `eth.IsMining()` est vrai — **`--mine` est indispensable, `--vote` seul ne suffit pas** | `:138-142` |
| 3 | **41 blocs** se sont écoulés depuis le début du minage (`blocksNumberSinceMining = 40`, test `<=`) — ≈ 3 min 25 s à 5 s/bloc | `:25`, `:143-147` |
| 4 | `engine.IsActiveValidatorAt(...)` est vrai | `:157-163` |
| 5 | les trois règles anti-double-vote passent (`UnderRules`) | `:175`, `:271-325` |
| 6 | le vote est accepté par la `VotePool`, qui rejette toute `VoteAddress` inconnue du snapshot | `core/vote/vote_pool.go:178-182` → `parlia.go:1655-1666` |

La condition **4** compare **octet à octet** la clé publique BLS locale à la
`VoteAddress` du validateur dans le snapshot :

```go
// consensus/parlia/parlia.go:1613-1624
validatorInfo, ok := validators[p.val]
return ok && (checkVoteKeyFn == nil || (validatorInfo != nil && checkVoteKeyFn(&validatorInfo.VoteAddress)))
```

Une clé publique BLS12-381 réelle est un point de courbe : elle ne peut **jamais**
être égale à 48 octets nuls. Le test échoue à chaque bloc et `vote_manager.go:161` se
contente d'une trace de niveau *debug* :

```
local validator with voteKey is not within the validatorSet at curHead
```

Le service tourne aujourd'hui avec `--verbosity 3` (`deploy/40-validator.sh:113`), soit
*info* : **cette trace n'apparaîtrait même pas dans le journal**. Le nœud démarrerait,
chargerait la clé, et resterait silencieux sans aucune erreur visible.

> **Conclusion opérationnelle.** Ajouter `--vote` sans avoir d'abord inscrit la clé
> publique on-chain ne produit rien, et ne produit aucun message pour le dire.

---

## 2. Ce que l'activation change — et ce qu'elle ne change pas

### 2.1 Ce qu'elle change

* `finalized` et `safe` cessent de renvoyer le bloc 0 et **avancent** avec la chaîne.
* Les en-têtes portent une **attestation** : l'`extraData` s'allonge — sur tous les
  blocs à partir du n° 3, pas seulement aux blocs d'epoch
  (`assembleVoteAttestation`, `parlia.go:1044`).
* Le quorum à N = 1 vaut **1** : `CeilDiv(len(snap.Validators)*2, 3)` = `CeilDiv(2,3)` = 1
  (`parlia.go:1068`). Un validateur unique correctement déclaré suffit donc à justifier
  ses propres blocs.

### 2.2 Ce qu'elle ne change pas

* **Le nombre de validateurs reste 1.** L'appel décrit au § 4 conserve
  `newVals = [INITIAL_VALIDATOR]` : il remplace 48 octets, il n'ajoute personne. Aucune
  tolérance aux pannes n'est gagnée.
* **Le bloc 0, le `chainId` 26262, l'offre de 700 000 000 BOSA** : inchangés. Aucun
  redémarrage de chaîne, aucun nouveau genesis.
* **La clé de scellage** (`0x3986…bA50`, secp256k1) n'est ni touchée, ni remplacée, ni
  exposée. Elle continue de signer les blocs exactement comme aujourd'hui.
* **Aucune sanction n'est ajoutée.** Le moniteur `--monitor.maliciousvote` porte en
  commentaire, dans le code : *« do malicious vote slashing. **TODO** »*
  (`core/monitor/malicious_vote_monitor.go:25-26`). Il compte des métriques ; il ne
  sanctionne rien. Le dépôt ne contient **aucune source Solidity** pour le contrat
  `0x…1001` (`SlashContract`) : je n'ai donc pas pu établir ce que son bytecode
  implémente, et je ne l'affirme dans aucun sens.
* **`CoinbosaStake.sol` et « PoBS » ne sont pas concernés.** Le contrat n'est déployé
  nulle part (il n'apparaît pas dans `scripts/build-genesis.js`, et l'`alloc` du
  genesis ne porte de code que pour `0x…1000`, `0x…1001`, `0x…1002` (1 802 o) et `0x…1007` (4 861 o)) ;
  même déployé, Parlia n'appelle en dur que `ValidatorContract = 0x…1000`
  (`core/systemcontracts/const.go:5`, utilisé `parlia.go:1941`) ; et `grep -rn
  'pobsTime\|IsPobs' params/ consensus/parlia/` ne rend qu'un commentaire dans un
  fichier de test. Le sujet est indépendant de celui-ci.
* **Ce que l'attestation ne démontre pas.** Constat de fait, sans interprétation : à
  N = 1, l'attestation est la signature d'**une seule clé**, détenue par **le même
  opérateur** que la clé de scellage. Elle rend `finalized` exploitable par un
  intégrateur ; elle ne constitue pas une garantie byzantine. Le README du projet
  (`coinbosa/README.md:228-230`) et `docs/INTEGRATION.md:226-236` décrivent déjà cette
  limite ; ils devront être relus après l'activation, car leur formulation actuelle
  (« la finalité rapide est inactive ») deviendra fausse.

> **Point juridique : à soumettre à un conseil.** Tout ce qui touche à la manière dont
> la finalité obtenue peut être **présentée** à un tiers, à un intégrateur ou dans un
> document public sort du périmètre de ce document.

---

## 3. La clé BLS

### 3.1 Ce qu'elle est

Une clé privée **BLS12-381**, distincte en tout point de la clé de scellage secp256k1.
Elle est produite par `bls.RandKey()` (`cmd/geth/blsaccountcmd.go:327`), chiffrée dans
un keystore au format EIP-2335, puis importée dans un portefeuille Prysm.

| | Clé de **scellage** | Clé de **vote** (BLS) |
|---|---|---|
| Courbe | secp256k1 | BLS12-381 |
| Adresse / clé publique | `0x3986…bA50` (20 o) | 48 octets |
| Signe | les **blocs** (sceau de 65 o) | les **votes** (`VoteEnvelope`) |
| Détient des BOSA | c'est l'`etherbase` | **non, jamais** |
| Peut émettre une transaction | oui | **non** |
| Inscrite dans | l'`extraData` du **genesis** — non remplaçable | le contrat `0x…1000` — **remplaçable** |
| Sa perte | **arrête la chaîne définitivement** | gêne, ne casse rien (§ 3.3) |
| Documentée par | `deploy/SAUVEGARDE-CLE.md` | le présent document, § 7 |

### 3.2 Ce qu'elle signe — exactement

Elle signe un `VoteEnvelope`, dont la charge utile est un `VoteData` de quatre champs :
`SourceNumber`, `SourceHash`, `TargetNumber`, `TargetHash`
(`core/vote/vote_manager.go:166-172`). Rien d'autre. Elle ne signe **aucun bloc**,
**aucune transaction**, **aucun message de gouvernance**.

Détail à connaître : le signataire n'utilise que le **premier** compte du portefeuille —
`PubKey: pubKeys[0]` (`core/vote/vote_signer.go:70-78`). Créer plusieurs comptes BLS
dans le même portefeuille n'apporte rien et rend le choix ambigu. **Un portefeuille, un
compte.**

### 3.3 Si elle est PERDUE

**La chaîne ne s'arrête pas.** C'est la différence fondamentale avec la clé de scellage.
Les blocs continuent d'être scellés par la clé secp256k1, les transactions passent, le
réseau vit.

Ce qui se passe :

1. Le nœud **refuse de démarrer** tant que `--vote` est dans le service, parce que
   `NewVoteManager` échoue et que `New()` propage l'erreur (`eth/backend.go:508-511`).
   Avec `Restart=on-failure`, `RestartSec=5` et `StartLimitBurst=5`
   (`deploy/40-validator.sh:96-97, 117-118`), systemd abandonne après **5 échecs en
   600 s** — et là, oui, la production des blocs s'arrête. **Le remède est immédiat :
   retirer les deux drapeaux (§ 6) et redémarrer.**
2. Une fois `--vote` retiré, `finalized` et `safe` **se figent** à leur dernière valeur
   atteinte — ils ne repartent **pas** à zéro. `updateAttestation` sort par un `return`
   silencieux quand l'en-tête ne porte pas d'attestation
   (`consensus/parlia/snapshot.go:205-207`) : la dernière attestation connue reste dans
   le snapshot.
3. **La récupération est simple** : créer une **nouvelle** clé BLS (étapes 1 à 4) et
   refaire **un** appel `updateValidatorSet` avec la nouvelle clé publique. Il n'y a rien
   à récupérer de l'ancienne clé, et rien n'est perdu définitivement.

**Coût d'une perte : une transaction du gouverneur, un redémarrage du validateur, et
l'attente d'un bloc d'epoch (≤ 200 blocs, soit ≤ 16 min 40 s).**

### 3.4 Si elle est VOLÉE

Le profil de risque est **entièrement différent** de celui d'une perte — et, sur cette
chaîne, il est plus étroit qu'on ne l'imagine.

**Ce que le voleur ne peut PAS faire :**

* sceller un bloc — cela demande la clé secp256k1, qu'il n'a pas ;
* déplacer un seul BOSA — la clé BLS ne détient rien et ne signe aucune transaction ;
* modifier l'ensemble des validateurs — cela demande la clé du gouverneur ;
* prendre le contrôle de la chaîne.

**Ce qu'il peut faire :** signer des `VoteEnvelope` au nom du validateur. La
conséquence réelle est que **la signature d'attestation n'est plus la preuve d'un seul
détenteur** : deux parties peuvent en produire. Comme le quorum vaut 1, la valeur
probante de `finalized` vis-à-vis d'un intégrateur est détruite, même si la chaîne
canonique, elle, reste produite par le seul scelleur légitime.

**Deux points à ne pas surestimer, et que je donne comme tels :**

* les protections anti-double-vote (`UnderRules`, `vote_manager.go:271-325`) reposent
  sur le **journal local** du nœud (`<datadir>/voteJournal`). Un voleur exécutant un
  autre nœud a un journal vide : ces règles **ne le contraignent pas**.
* le moniteur `--monitor.maliciousvote` **détecte et compte**, il ne sanctionne pas
  (§ 2.2). Je n'ai pas établi ce que le bytecode de `0x…1001` fait ; je n'affirme donc
  ni qu'une sanction existe, ni qu'elle n'existe pas.

**La bonne nouvelle, et elle est structurelle : une clé de vote volée est
RÉVOCABLE.** Le remède est le même qu'au § 3.3 — nouvelle clé, un appel
`updateValidatorSet`, effet au bloc d'epoch suivant. Contrairement à la clé de
scellage, dont la compromission est irréparable puisqu'elle est gravée dans le
bloc 0, celle-ci se remplace en moins de vingt minutes.

**Coût d'un vol : identique à celui d'une perte, plus la nécessité de considérer comme
non probante toute attestation produite entre le vol et la révocation.**

---

## 4. La marche à suivre

**Lire tout le § 4 avant de lancer la première commande.** Les étapes 1 à 3 et 6
touchent des secrets et sont marquées **ÉDITEUR SEUL**.

Contexte de production, repris de `deploy/40-validator.sh` :

```
binaire     /opt/coinbosa-chain/build/bin/geth
datadir     /var/lib/coinbosa/validator      (ABSOLU — voir l'encadré ci-dessous)
utilisateur coinbosa-val
service     /etc/systemd/system/coinbosa-validator.service
```

> **Le piège du datadir relatif — il ne vous concerne pas ici, mais il concerne le
> script de développement.** `geth bls wallet create` écrit dans
> `filepath.Join(cfg.Node.DataDir, "bls/wallet")` **sans** passer par `ResolvePath`
> (`cmd/geth/blsaccountcmd.go:225` et `:260`), alors que le nœud, lui, résout le chemin
> via `stack.ResolvePath()` (`eth/backend.go:505-507`), qui préfixe tout chemin **non
> absolu** par `<datadir>/geth` (`node/config.go:378-401`). Avec un datadir relatif, la
> CLI crée le portefeuille dans `node1/bls/wallet` et le nœud le cherche dans
> `node1/geth/node1/bls/wallet`. **La production utilise un datadir absolu
> (`40-validator.sh:27`) et n'est pas affectée. `scripts/start-node.sh:55` utilise
> `--datadir node1` et l'est.**

### Étape 0 — Relever l'état AVANT (2 min, aucun risque)

```bash
RPC=https://explorer.coinbosa.com/rpc
curl -s -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["finalized",false]}' $RPC \
  | python3 -c "import sys,json;print('finalized =', int(json.load(sys.stdin)['result']['number'],16))"
```

**Attendu aujourd'hui : `finalized = 0`.** Notez la valeur : c'est votre point de
comparaison au § 5.

### Étape 1 — Le fichier de mot de passe — **ÉDITEUR SEUL**

**Cette étape crée un secret.** Elle se fait sur le serveur, en session directe, et le
mot de passe ne doit apparaître dans aucun ticket, aucun message, aucun fichier de ce
dépôt.

Contraintes, toutes vérifiées dans le code :

| Contrainte | Origine |
|---|---|
| **≥ 10 caractères** | `signer/core/validation.go:29-31` |
| **ASCII 7 bits imprimable uniquement** | `signer/core/validation.go:24, 32-34` |
| **AUCUN saut de ligne final** | voir l'encadré ci-dessous |
| **Fichier distinct de `pw.txt`** | `pw.txt` protège la clé de scellage **et se termine par un `\n`** (`SAUVEGARDE-CLE.md § 2`) |

> **La divergence CLI / nœud, et pourquoi elle fait tomber le nœud.**
> La CLI lit le fichier via `MakePasswordListFromPath` (`cmd/utils/flags.go:1705-1719`),
> qui découpe sur `\n`, puis `GetPassPhraseWithList` retourne **l'élément 0**
> (`cmd/utils/prompt.go:51-58`) : donc **la première ligne, saut de ligne exclu**.
> Le nœud en marche, lui, fait `os.ReadFile` et passe **le fichier ENTIER**, saut de
> ligne compris (`core/vote/vote_signer.go:43, 50-53`).
> Un fichier terminé par `\n` crée donc un portefeuille avec `motdepasse` que le nœud
> tentera d'ouvrir avec `motdepasse\n` : `wallet.OpenWallet` échoue
> (`vote_signer.go:54-57`) et **le nœud refuse de démarrer**.
>
> Corollaire utile : **si et seulement si le fichier n'a pas de saut de ligne final**,
> les deux chemins de lecture rendent la même chaîne — et alors le succès de
> `geth bls account list` (étape 4) devient une **preuve** que le nœud saura ouvrir le
> portefeuille. C'est tout l'intérêt de la contrainte.

```bash
# ÉDITEUR SEUL — sur le serveur. Remplacer <MOT-DE-PASSE> par le vôtre.
# printf '%s' n'ajoute PAS de saut de ligne. N'utilisez ni echo, ni un éditeur de
# texte : la plupart ajoutent ou « nettoient » silencieusement la fin de fichier.
sudo -u coinbosa-val sh -c "printf '%s' '<MOT-DE-PASSE>' > /var/lib/coinbosa/validator/bls-pw.txt"
sudo chmod 600 /var/lib/coinbosa/validator/bls-pw.txt
sudo chown coinbosa-val:coinbosa-val /var/lib/coinbosa/validator/bls-pw.txt
```

Contrôle, **sans afficher le secret** :

```bash
echo "sauts de ligne : $(sudo wc -l < /var/lib/coinbosa/validator/bls-pw.txt)"   # attendu : 0
echo "longueur       : $(sudo wc -c < /var/lib/coinbosa/validator/bls-pw.txt)"   # attendu : >= 10
```

> Si `sauts de ligne` affiche autre chose que `0`, **refaites le fichier**. Ne
> poursuivez pas : l'erreur ne se manifesterait qu'à l'étape 8, au redémarrage du seul
> nœud qui scelle.

### Étape 2 — Créer le portefeuille BLS — **ÉDITEUR SEUL**

**Cette commande crée un portefeuille chiffré par le mot de passe de l'étape 1.**

```bash
sudo -u coinbosa-val /opt/coinbosa-chain/build/bin/geth bls wallet create \
  --datadir /var/lib/coinbosa/validator \
  --blspassword /var/lib/coinbosa/validator/bls-pw.txt
```

**Attendu :**

```
Create BLS wallet successfully!
```

Ce que cela a écrit : le répertoire `/var/lib/coinbosa/validator/bls/wallet`
(constante `BLSWalletPath`, `blsaccountcmd.go:35-38` ; keymanager `local`, `:242-247`).

**Si la commande dit `BLS wallet already exists in <DATADIR>/bls/wallet`** (`:230-231`) :
un portefeuille existe déjà. **Ne le supprimez pas.** Passez à l'étape 4 pour voir ce
qu'il contient, et si son mot de passe est inconnu, arrêtez-vous et traitez le cas comme
une perte (§ 3.3) plutôt que d'écraser quoi que ce soit.

**Si la commande dit `Password invalid:`** : le mot de passe ne respecte pas les
contraintes de l'étape 1. Refaites le fichier.

### Étape 3 — Créer la clé BLS — **ÉDITEUR SEUL**

**Cette commande génère une clé privée.**

```bash
sudo -u coinbosa-val /opt/coinbosa-chain/build/bin/geth bls account new \
  --datadir /var/lib/coinbosa/validator \
  --blspassword /var/lib/coinbosa/validator/bls-pw.txt
```

> **Ne passez JAMAIS `--show-private-key`.** Ce drapeau existe
> (`blsaccountcmd.go:42-46`) et affiche la clé privée à l'écran, donc dans le
> défilement du terminal, dans un éventuel enregistrement de session, et dans
> l'historique de la console. Il n'est utile à rien ici.

**Attendu, dans cet ordre :**

```
Successfully create a BLS account.
Importing BLS account, this may take a while...
Successfully import created BLS account.
```

Ce que cela a écrit :
`/var/lib/coinbosa/validator/bls/keystore/keystore-<petnom>.json`, un keystore
**chiffré** (`blsaccountcmd.go:320-321, 358`), puis l'import dans le portefeuille
(`:368-375`).

> **Un seul mot de passe, malgré ce que dit l'aide.** La description de la commande
> affirme que le mot de passe du compte est « différent du mot de passe du
> portefeuille » (`blsaccountcmd.go:116-119`), mais le code fait
> `accountPassword := w.Password()` (`:324`) — et de même à l'import d'une clé externe,
> `:415`. **La documentation de la commande contredit son propre code.** Il n'y a qu'un
> secret à protéger, celui de l'étape 1.

**Exécutez cette commande UNE seule fois** (§ 3.2 : seul `pubKeys[0]` est utilisé).

### Étape 4 — Relever la clé PUBLIQUE (non secrète)

```bash
sudo -u coinbosa-val /opt/coinbosa-chain/build/bin/geth bls account list \
  --datadir /var/lib/coinbosa/validator \
  --blspassword /var/lib/coinbosa/validator/bls-pw.txt
```

**Attendu :**

```
(keymanager kind) imported wallet

Showing 1 BLS account

Account 0 | <petnom-genere>
[BLS public key] 0x<96 caractères hexadécimaux>
```

Contrôles à faire **ici et pas plus tard** :

* `Showing 1 BLS account` — **exactement 1**. Si vous en voyez plusieurs, seul le
  premier votera et le contrat n'en acceptera qu'un : reprenez avant de continuer.
* La clé publique fait **96 caractères hexadécimaux** après `0x`, soit 48 octets.
* Elle n'est **pas** nulle.

Cette valeur est **publique** : elle est destinée à être écrite dans un contrat et dans
les en-têtes de blocs. Vous pouvez la copier, l'envoyer, la coller. Notez-la.

> Le fait que cette commande réussisse prouve que le portefeuille s'ouvre avec le
> contenu du fichier de mot de passe — **à condition que le contrôle « sauts de
> ligne : 0 » de l'étape 1 soit passé.** Sans lui, cette réussite ne dit rien du nœud.

### Étape 5 — Sauvegarder AVANT d'aller plus loin

Ne poursuivez pas sans avoir appliqué le § 7. Une clé créée et non sauvegardée qui
devient la clé de vote officielle est une régression : elle transforme une pièce
jetable en pièce qu'on doit reconstituer.

### Étape 6 — Inscrire la clé publique on-chain — **ÉDITEUR SEUL**

**Cette étape utilise la clé du gouverneur.** C'est la seule étape irréversible par
elle-même (elle est annulable par une seconde transaction, § 6, mais pas par une
touche « annuler »).

**Pourquoi cette étape est indispensable.** La chaîne de dépendance est entièrement
lisible dans le code : la `VoteAddress` du snapshot vient de l'`extraData` des blocs
d'epoch (`consensus/parlia/snapshot.go:401-413`) ; cette `extraData` est écrite par
`prepareValidators` (`parlia.go:983-1015`) à partir de `getCurrentValidators`
(`:1919-1963`), qui fait un `eth_call` à `getMiningValidators()` sur `0x…1000`. **Une
clé présente seulement dans le portefeuille local ne sera jamais reconnue.**

L'appel exact — et le seul qui soit sûr :

```
contrat   0x0000000000000000000000000000000000001000
fonction  updateValidatorSet(address[] newVals, bytes[] newVotes)
selecteur 0x8001f54c
newVals   [ 0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50 ]     <- UNE SEULE adresse
newVotes  [ 0x<votre cle publique BLS, 48 octets> ]
emetteur  0x1eEF3830833d83aCd3152A511853fd04A0b4082a          <- le GOUVERNEUR
```

> ### DANGER — la seule erreur qui arrête la chaîne
>
> `updateValidatorSet` est **la même fonction** que celle qui sert à **ajouter** des
> validateurs. Le dépôt documente et reproduit au banc qu'un passage de 1 à N
> validateurs alors qu'un seul nœud scelle **arrête la chaîne au bloc d'epoch suivant,
> de façon non réparable on-chain** (`contracts/CoinbosaValidatorSet.sol:170-201` ;
> `scripts/rotate-validators.js:1-38` ; test `TestCoinbosaAddSecondValidatorHaltsChain`,
> `POBS-ACTIVATION.md:28-40` — au bloc 201, V1 refuse de sceller
> (« Signed recently, must wait for others ») et V2 n'a pas de nœud).
>
> **`newVals` doit contenir exactement une adresse, celle du validateur de genèse.**
> Tant que `newVals` est inchangé, cet appel ne remplace que 48 octets et ne peut pas
> arrêter la chaîne.
>
> À l'inverse, se tromper sur `newVotes` est **bénin** : le contrat vérifie la longueur
> (48 octets, `:216`) mais **pas** la validité cryptographique. Une clé fausse mais de
> bonne longueur laisse simplement le nœud silencieux, exactement comme aujourd'hui,
> et se corrige par un second appel.

**Simulez d'abord. `eth_call` n'écrit rien et exécute la transaction pour de vrai.**

```bash
RPC=https://explorer.coinbosa.com/rpc
GOV=0x1eEF3830833d83aCd3152A511853fd04A0b4082a
VAL=3986d6b31ec55043ceaaf25f5ddea53517cbba50

# Les 96 caracteres hexadecimaux releves a l'etape 4, SANS le prefixe 0x.
CLE=<vos-96-caracteres-hexadecimaux>

DATA="0x8001f54c\
0000000000000000000000000000000000000000000000000000000000000040\
0000000000000000000000000000000000000000000000000000000000000080\
0000000000000000000000000000000000000000000000000000000000000001\
000000000000000000000000${VAL}\
0000000000000000000000000000000000000000000000000000000000000001\
0000000000000000000000000000000000000000000000000000000000000020\
0000000000000000000000000000000000000000000000000000000000000030\
${CLE}00000000000000000000000000000000"

# GARDE — ne jamais simuler, et surtout ne jamais signer, une calldata malformee.
if [ ${#DATA} -eq 586 ] && printf '%s\n' "$DATA" | grep -qE '^0x[0-9a-fA-F]*$'; then
  echo "calldata : $(( (${#DATA} - 2) / 2 )) octets   (attendu : 292)"
  curl -s -X POST -H 'Content-Type: application/json' \
    --data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_call\",\"params\":[{\"from\":\"$GOV\",\"to\":\"0x0000000000000000000000000000000000001000\",\"data\":\"$DATA\"},\"latest\"]}" $RPC
else
  echo "ABANDON : calldata de ${#DATA} caracteres au lieu de 586, ou caractere non hexadecimal."
  echo "Verifiez CLE (96 caracteres hex, sans 0x). NE SIGNEZ RIEN."
fi
```

**Attendu : `{"jsonrpc":"2.0","id":1,"result":"0x"}`** — la fonction ne retourne rien,
un `result` vide signifie **pas de revert**. La calldata fait **292 octets** (4 + 9 mots
de 32).

Vérification de contrôle, également relevée ce jour : la même simulation lancée avec
`from` = l'adresse du **validateur** au lieu du gouverneur rend bien
`execution reverted: only governor`. Si votre simulation « depuis le gouverneur »
rend un revert, **ne signez rien** et cherchez l'erreur dans la calldata.

Gaz estimé pour cet appel, mesuré ce jour : **95 203**. À 1 gwei
(`eth_gasPrice` = 1 000 000 000), le coût est de l'ordre de **0,0001 BOSA** ; le
gouverneur détient 1 000 BOSA.

**La signature et l'émission.**

* La clé privée du gouverneur **ne doit apparaître nulle part** : ni sur une ligne de
  commande (elle irait dans l'historique du shell et dans la table des processus), ni
  dans un fichier de ce dépôt, ni dans une variable d'environnement exportée. Le dépôt
  indique l'**adresse** du gouverneur (`DOSSIER-COTATION.md:72, :410` ;
  `deploy/SAUVEGARDE-CLE.md:485`) et rien d'autre — c'est la bonne façon de faire.
* `SAUVEGARDE-CLE.md § 8` note que cette clé « ne produit pas de blocs, mais elle seule
  peut faire tourner l'ensemble des validateurs » et qu'elle « mérite sa propre
  procédure ». Cette procédure n'existe pas encore.
* **Je n'ai pas pu établir qui détient cette clé ni par quel outil elle est signée.**
  Le nonce du gouverneur est `0` : ce chemin n'a jamais été exercé sur cette chaîne
  (§ 1.1). Il serait raisonnable de l'exercer d'abord sur une transaction sans
  conséquence — un envoi de valeur nulle vers le gouverneur lui-même — avant de signer
  celle-ci.
* Le RPC public est en lecture ; l'émission doit passer par un nœud qui accepte les
  transactions. **Je n'ai pas établi quel point d'entrée sert à cela en production.**

### Étape 7 — Attendre le bloc d'epoch et vérifier le basculement

L'effet est **différé**. L'`extraData` n'est réécrite qu'aux blocs multiples de 200
(`parlia.go:983-990`, `epoch: 200` dans le genesis), et le snapshot ne bascule qu'à ce
même bloc puisque `minerHistoryCheckLen()` = `(N/2+1) × TurnLength − 1` vaut **0** pour
N = 1 et `TurnLength = 1` (`snapshot.go:243-245`, `:374` ; `defaultTurnLength = 1`,
`parlia.go:65`).

**Attente : au plus 200 blocs, soit ≤ 16 min 40 s à 5 s/bloc.**

```bash
RPC=https://explorer.coinbosa.com/rpc
# 1) la chaine avance-t-elle toujours ?
curl -s -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' $RPC

# 2) la cle est-elle inscrite ? (relire le contrat)
curl -s -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[{"to":"0x0000000000000000000000000000000000001000","data":"0x4df6e0c3"},"latest"]}' $RPC
```

**Attendu (2) :** la réponse doit contenir **votre clé** à la place des 48 zéros.

```bash
# 3) l'extraData du prochain bloc d'epoch porte-t-elle la cle ?
#    remplacer 0xNNNNNN par le premier multiple de 200 APRES la transaction
curl -s -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0xNNNNNN",false]}' $RPC \
 | python3 -c "
import sys,json
b=bytes.fromhex(json.load(sys.stdin)['result']['extraData'][2:])
print('extraData      ', len(b), 'octets')
print('validateur     0x'+b[33:53].hex())
print('cle BLS        0x'+b[53:101].hex())
print('cle nulle ?    ', b[53:101]==bytes(48))
print('attestation    ', len(b)-101-65, 'octets')"
```

**Attendu :** `cle nulle ?  False`, et la clé affichée est la vôtre.
`attestation` vaut encore `0` à ce stade — c'est normal, personne n'a encore voté.

**N'allez pas à l'étape 8 tant que ce contrôle n'est pas passé.**

### Étape 8 — Ajouter les deux drapeaux au service

> **C'est l'étape la plus risquée du document, et le risque n'est pas la clé : c'est le
> redémarrage.** `coinbosa-validator` est le **seul** nœud qui scelle. S'il ne remonte
> pas, la production de blocs s'arrête. Et il ne remontera pas si le portefeuille ne
> s'ouvre pas : `NewVoteManager` échoue, `New()` propage, le nœud sort
> (`eth/backend.go:508-511`). Avec `StartLimitBurst=5` sur `StartLimitIntervalSec=600`,
> systemd cesse de réessayer après cinq échecs.
>
> **Avant de lancer cette étape :** ayez le § 6 (retour en arrière) sous les yeux, et
> une seconde session SSH déjà ouverte.

Ajouter au `ExecStart` de `/etc/systemd/system/coinbosa-validator.service`, à côté des
`--mine --miner.etherbase 0x3986…bA50` déjà présents (`deploy/40-validator.sh:102-113`) :

```
  --vote \
  --blspassword /var/lib/coinbosa/validator/bls-pw.txt \
```

`--blswallet` et `--vote-journal-path` sont **inutiles ici** : leurs valeurs par défaut
sont calculées à partir du datadir (`flags.go:1887-1893` → `<datadir>/bls/wallet` ;
`flags.go:1879-1885` → `<datadir>/voteJournal`) et le datadir de production est absolu.
`--blspassword`, en revanche, est **de fait obligatoire** : sans lui, `BLSPasswordFile`
reste vide, `ResolvePath("")` rend `<datadir>/geth` — un **répertoire** — et
`os.ReadFile` échoue (`vote_signer.go:43`).

Il n'existe **aucun drapeau `--blsaccount`** dans cet arbre (vérifié par `grep` sur
`cmd/utils/flags.go` et `cmd/geth/main.go`). Les six drapeaux réellement déclarés et
enregistrés sont : `--vote`, `--disablevoteattestation`, `--monitor.maliciousvote`,
`--blspassword`, `--blswallet`, `--vote-journal-path`
(`flags.go:1262-1296`, `cmd/geth/main.go:183-188`).

```bash
sudo systemctl daemon-reload
sudo systemctl restart coinbosa-validator
sudo journalctl -u coinbosa-validator -n 60 --no-pager
```

**Attendu, aux niveaux *info* — donc visibles avec `--verbosity 3` :**

```
Read BLS wallet password successfully
Open BLS wallet successfully
Initialized keymanager successfully
Create voteManager successfully
```

Ces quatre lignes viennent de `core/vote/vote_signer.go:48, 58, 65` et
`eth/backend.go:512`. **Si vous les voyez, la clé est chargée.**

**Si le nœud ne démarre pas** — messages `Open BLS wallet failed`,
`Failed to Initialize voteManager`, ou `Read BLS wallet password` en erreur — **allez
immédiatement au § 6.1**. La cause la plus probable est le saut de ligne du fichier de
mot de passe (étape 1).

### Étape 9 — Attendre 41 blocs

Le compteur `blockCountSinceMining` repart de zéro à chaque redémarrage et il faut
`blockCountSinceMining > 40` (`vote_manager.go:143-147`). **≈ 3 min 25 s.** Passez au
§ 5.

---

## 5. Comment on vérifie que ça marche

**Sans ce paragraphe, l'activation n'est pas terminée.** Les quatre contrôles ci-dessous
sont indépendants ; les deux premiers suffisent à conclure.

### 5.1 Le contrôle décisif — `finalized` avance

```bash
RPC=https://explorer.coinbosa.com/rpc
for i in 1 2 3; do
  curl -s -X POST -H 'Content-Type: application/json' \
    --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["finalized",false]}' $RPC \
    | python3 -c "import sys,json;r=json.load(sys.stdin)['result'];print('finalized =',int(r['number'],16),r['hash'])"
  curl -s -X POST -H 'Content-Type: application/json' \
    --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["safe",false]}' $RPC \
    | python3 -c "import sys,json;r=json.load(sys.stdin)['result'];print('safe      =',int(r['number'],16))"
  echo "---"; sleep 30
done
```

| | Avant | Après, attendu |
|---|---|---|
| `finalized` | `0` | un nombre **> 0**, qui **croît** d'un relevé à l'autre |
| `safe` | `0` | idem, `>= finalized` |
| hash de `finalized` | `0x8dcdadc2…9018da6` (genesis) | **autre chose** |

Un `finalized` qui reste à `0` après trois relevés espacés de 30 s signifie que la
chaîne des six conditions du § 1.2 est rompue quelque part. Reprenez au 5.3.

### 5.2 L'attestation est dans les en-têtes

```bash
RPC=https://explorer.coinbosa.com/rpc
curl -s -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["latest",false]}' $RPC \
 | python3 -c "
import sys,json
r=json.load(sys.stdin)['result']; b=bytes.fromhex(r['extraData'][2:]); n=int(r['number'],16)
epoch = (n % 200 == 0)
base = 32 + 1 + 68 + 65 if epoch else 32 + 65
print('bloc', n, '| epoch' if epoch else '| ordinaire')
print('extraData    ', len(b), 'octets   (reference sans attestation :', base, ')')
print('attestation  ', len(b)-base, 'octets')
print('VERDICT      ', 'OK' if len(b) > base else 'AUCUNE ATTESTATION')"
```

**Le critère robuste est celui-ci : `extraData` doit être STRICTEMENT plus longue que
sa référence** — 166 octets sur un bloc d'epoch, 97 sur un bloc ordinaire (constantes
`extraVanity = 32`, `extraSeal = 65`, `validatorNumberSize = 1`,
`validatorBytesLength = 20 + 48 = 68`, `parlia.go:67-74`).

> **Ordre de grandeur, calculé et non observé.** À partir de la structure
> `types.VoteAttestation` (`core/types/vote.go:47-52` — `VoteAddressSet` uint64,
> `AggSignature` 96 octets, `Data` de 4 champs, `Extra` vide) et des règles RLP,
> l'attestation devrait faire **environ 177 octets** pour N = 1 : un bloc ordinaire
> passerait de 97 à ~274 octets, un bloc d'epoch de 166 à ~343. **Je n'ai pas observé
> ces valeurs sur une chaîne réelle** — ne les traitez pas comme un critère de
> réussite ; le critère est « strictement plus long ».

### 5.3 Le nœud dit-il qu'il vote

Les messages « vote manager produced vote » sont de niveau **debug**
(`vote_manager.go:210`) et **n'apparaissent pas** à `--verbosity 3`. Deux façons de
voir quand même :

* **Le compteur.** `votesManagerCounter` est enregistré sous `votesManager/local`
  (`vote_manager.go:28`) et incrémenté à chaque vote produit (`:213`). S'il est exposé
  par la supervision en place (`deploy/50-monitoring.sh`), il doit croître d'environ
  un par bloc. **Je n'ai pas vérifié que ce compteur est effectivement collecté par le
  dispositif de ce client.**
* **Le journal, temporairement.** Passer à `--verbosity 4` fait apparaître les traces
  *debug*, dont, en cas d'échec, la phrase qui identifie exactement le problème :
  `local validator with voteKey is not within the validatorSet at curHead` (§ 1.2).
  **Repasser à 3 ensuite** : `verbosity 4` est bavard et grossit les journaux.

### 5.4 Ce qui doit rester vrai

```bash
RPC=https://explorer.coinbosa.com/rpc
curl -s -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[{"to":"0x0000000000000000000000000000000000001000","data":"0x1e526e45"},"latest"]}' $RPC
```

**Attendu : `…0001`.** `numOfValidators()` doit valoir **1**. S'il vaut autre chose,
l'ensemble des validateurs a été modifié — c'est la situation décrite dans l'encadré
DANGER de l'étape 6, et la chaîne s'arrêtera au prochain bloc d'epoch.

Vérifiez aussi que le bloc 0 est intact — il ne devrait pas y avoir de raison qu'il ne
le soit pas, mais c'est un contrôle à coût nul :

```
bloc 0            0x8dcdadc247a98f33728cae944e20ce7c49c74b35cfba31495f85e98979018da6
racine d'etat 0   0x93682eb9182a55531d47014b76a285b45d3e720a2951f9ffbdc67f52995f8c03
```

---

## 6. Revenir en arrière

Il y a **deux retours possibles**, de portées très différentes. Le premier suffit
presque toujours.

### 6.1 Retour côté nœud — immédiat, sans coût

**Quand :** le nœud ne démarre pas, ou vous voulez arrêter de voter sans toucher à la
chaîne.

```bash
# retirer les deux lignes --vote et --blspassword de ExecStart
sudo systemctl daemon-reload
sudo systemctl restart coinbosa-validator
sudo systemctl status coinbosa-validator --no-pager
sudo journalctl -u coinbosa-validator -n 30 --no-pager
```

**Coût :** l'interruption de la production de blocs pendant le redémarrage. Le service
est réglé avec `KillSignal=SIGINT` et `TimeoutStopSec=300` (`40-validator.sh:125-126`) :
geth garde l'état récent en mémoire et l'écrit à l'arrêt, **il faut le laisser
terminer**. Ne le tuez pas.

**Effet sur la finalité :** `finalized` et `safe` **se figent** à leur dernière valeur.
Ils ne repartent **pas** à zéro (`snapshot.go:205-207` : pas d'attestation dans
l'en-tête → `return` silencieux, la dernière attestation reste dans le snapshot).
La chaîne reste utilisable ; elle cesse simplement de progresser en finalité.

**Ce retour ne remet pas la clé publique à zéro on-chain.** C'est sans danger : une clé
inscrite dont personne ne se sert produit exactement l'état d'aujourd'hui.

### 6.2 Retour on-chain — remettre la clé nulle

**Quand :** clé volée (§ 3.4), ou décision de revenir à l'état documenté aujourd'hui
dans `README.md` et `docs/INTEGRATION.md`.

C'est le **même appel** qu'à l'étape 6, avec 48 octets nuls au lieu de votre clé. Le
contrat l'accepte : `require(newVotes[i].length == VOTE_ADDRESS_LENGTH)` porte sur la
longueur, pas sur la valeur (`CoinbosaValidatorSet.sol:216`), et la garde d'unicité ne
peut pas se déclencher avec une seule entrée (`:220`). Faites la même simulation
`eth_call` avant de signer.

```
newVals   [ 0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50 ]     <- toujours UNE SEULE
newVotes  [ 0x000000…000000 ]                                <- 48 octets nuls
```

**Coût :** une transaction du gouverneur (≈ 95 000 de gaz), plus l'attente d'un bloc
d'epoch (≤ 16 min 40 s). **Faites d'abord le 6.1**, sinon le nœud continuera d'essayer
de voter dans l'intervalle.

### 6.3 Ce qui n'est PAS annulable

* **Les attestations déjà produites** restent dans les en-têtes des blocs concernés,
  définitivement. C'est de l'histoire de chaîne : on n'y revient pas.
* **La valeur atteinte par `finalized`** ne redescend pas (6.1). Là encore, ce n'est pas
  un problème : c'est un état meilleur que celui d'aujourd'hui.

### 6.4 Ce que je n'ai pas pu chiffrer

Le coût, pour les clients tiers **déjà synchronisés** (explorateur, indexeurs), du
passage de `snap.Attestation = nil` à une attestation présente. J'ai lu le chemin de
vérification d'en-tête (`parlia.go:470-575`) mais **je n'ai pas testé** une chaîne qui
se met à produire des attestations en cours de route. Le raisonnement dit que ce devrait
être transparent — `lubanBlock` et `platoBlock` valent 0, donc tous les nœuds sont
« Luban-conscients » depuis toujours — mais **ce raisonnement n'est pas une mesure**.

---

## 7. Sauvegarder cette seconde clé

**Lisez d'abord `deploy/SAUVEGARDE-CLE.md` en entier.** Sa règle centrale s'applique
telle quelle :

> **A et B ne voyagent jamais ensemble et ne sont jamais rangés ensemble.**
> (`SAUVEGARDE-CLE.md § 2`)

Chaque pièce en au moins deux exemplaires, dans deux lieux physiques différents ; aucun
lieu ne détient à la fois le matériel chiffré et le mot de passe. C'est identique ici.

### 7.1 Ce qui change par rapport à la clé de scellage

| | Clé de scellage (`SAUVEGARDE-CLE.md`) | Clé de vote BLS (ce document) |
|---|---|---|
| Pièce A | **un fichier**, `keystore/UTC--…--3986d6b3…`, 491 o | **deux choses** : le keystore `bls/keystore/keystore-<petnom>.json` **et** le répertoire de portefeuille `bls/wallet` |
| Pièce B | 1ʳᵉ ligne de `pw.txt`, **fichier terminé par `\n`** | **fichier SANS `\n` final** — l'inverse |
| Paramètres de chiffrement | scrypt `N=262144, r=8, p=1`, connus et mesurés (§ 1.4, § 7.1) | format EIP-2335, **paramètres non établis** (voir 7.3) |
| Perte de B | catastrophe évitable de justesse (§ 7.2) | **gêne** : on regénère (§ 3.3) |
| Perte de A | **fin de la chaîne** (§ 7.3) | **gêne** : on regénère (§ 3.3) |
| Compromission | **irréparable** — l'adresse est dans le bloc 0 | **révocable en < 20 min** (§ 3.4) |
| Urgence relative | maximale | réelle mais **secondaire** |

**Ce qui doit être sauvegardé, exactement :**

```
A1   /var/lib/coinbosa/validator/bls/keystore/keystore-<petnom>.json   (chiffre)
A2   /var/lib/coinbosa/validator/bls/wallet/                           (repertoire, chiffre)
B    le mot de passe de l'etape 1, transcrit A LA MAIN, sur papier
C    la cle PUBLIQUE (96 caracteres hex) — publique, a noter sans precaution
```

> **Pourquoi A1 **et** A2.** `bls account new` écrit le keystore **puis** l'importe dans
> le portefeuille (`blsaccountcmd.go:320-321, 358, 368-375`). Le nœud, lui, n'ouvre que
> le **portefeuille** (`vote_signer.go:50-53`). Le keystore seul devrait suffire à
> reconstituer un portefeuille via `geth bls account import`, mais **je n'ai pas testé
> cette restauration** : sauvegardez les deux.

### 7.2 L'attitude à adopter, et elle diffère

`SAUVEGARDE-CLE.md § 7.4` conclut que **c'est le coffre, pas le mot de passe, qu'il faut
sauvegarder en premier**, parce que le coffre perdu est irrécupérable. **Ce
raisonnement ne se transpose pas ici.** Pour la clé BLS, ni A ni B n'est irrécupérable :
tout se regénère en une transaction. La sauvegarde reste utile — elle évite un
redémarrage du seul scelleur et une transaction du gouverneur — mais **elle n'a pas le
caractère existentiel de celle de la clé de scellage.**

En revanche, la règle de **séparation** garde toute sa force, pour une raison
différente : c'est la seule chose qui empêche un vol (§ 3.4), et un vol détruit la
valeur probante de `finalized` en silence, sans qu'aucune alarme ne se déclenche.

### 7.3 Ce que je n'ai pas pu établir sur le chiffrement

`SAUVEGARDE-CLE.md § 1.4` donne les paramètres exacts du coffre secp256k1
(`scrypt N=262144, r=8, p=1`) et en tire, mesures à l'appui, le coût d'une attaque par
force brute. **Je ne peux pas faire la même chose ici.** Le keystore BLS est produit par
`keystorev4.New()` (`blsaccountcmd.go:326`), issu de
`prysmaticlabs/prysm/v5` — dépendance **non vendorisée** dans `/Users/protocole/repo`
(il n'y a aucun répertoire `vendor`). Je n'ai lu ni sa fonction de dérivation, ni ses
paramètres, ni le contenu qu'elle écrit dans `bls/wallet`. **Je ne donne donc aucun
chiffre sur la résistance de ce keystore.** Les paramètres réels seront lisibles dans
le fichier `keystore-<petnom>.json` une fois créé, dans son champ `crypto.kdf`.

**Conséquence pratique : ne présumez pas que le mot de passe BLS bénéficie de la même
protection que celui de la clé de scellage.** Choisissez-le au moins aussi fort, et
distinct.

---

## 8. Ce qui reste incertain

Tout ce qui suit est **non vérifié**. Rien n'est présenté comme un fait.

1. **Le contenu du répertoire `bls/wallet`.** Produit par
   `prysmaticlabs/prysm/v5/validator/accounts` (`blsaccountcmd.go:244-250`), dépendance
   non vendorisée. J'ai établi le **répertoire parent** — `<DATADIR>/bls/wallet` — et
   rien de plus : ni les noms de fichiers, ni le format, ni les paramètres de
   chiffrement (§ 7.3).

2. **L'état réel du serveur de production.** Je n'y ai eu **aucun accès**. J'ignore s'il
   existe déjà un portefeuille ou un keystore BLS sur la machine, quel est le contenu
   du fichier de mot de passe, et si le binaire en service a bien été compilé depuis cet
   arbre. **L'étape 2 pourrait donc buter sur un portefeuille préexistant.**
   L'information selon laquelle « les deux processus geth tournent sans aucun drapeau de
   vote » m'a été transmise : **je ne l'ai pas revérifiée.**

3. **La clé du gouverneur `0x1eEF…082A`.** Le dépôt en donne l'adresse
   (`DOSSIER-COTATION.md:72, :410` ; `SAUVEGARDE-CLE.md:485`). **Je n'ai aucun moyen de
   savoir qui la détient ni comment la transaction de l'étape 6 serait signée**, et je
   n'ai pas établi quel point d'entrée RPC accepte les transactions en production. Son
   nonce à `0` indique que ce chemin n'a jamais servi (§ 1.1).

4. **La séquence du § 4 n'a jamais été exécutée.** Elle est **dérivée par lecture** des
   sources et des scripts de déploiement — pas d'un essai. Je n'ai pas compilé le
   binaire. **Un banc d'essai local — chaîne jetable, datadir absolu — reste le seul
   moyen de la valider avant toute action sur la production**, et je recommande de le
   faire : le seul redémarrage du § 4 porte sur le seul nœud qui scelle.

5. **Le coût pour les clients tiers déjà synchronisés** du passage à des en-têtes
   porteurs d'attestations (§ 6.4).

6. **Les valeurs d'`extraData` après activation** (§ 5.2) sont **calculées** à partir de
   la structure RLP, **non observées**.

7. **Ce que fait le bytecode de `0x…1001`** (`SlashContract`, 7 339 octets dans le
   genesis). Le dépôt n'en contient aucune source Solidity. Je n'affirme donc ni qu'une
   sanction de double-vote existe, ni qu'elle n'existe pas (§ 2.2, § 3.4).

8. **La collecte du compteur `votesManager/local`** par le dispositif de supervision de
   ce client (§ 5.3).

9. **Point juridique : à soumettre à un conseil.** Tout ce qui touche à la manière dont
   la finalité obtenue peut être décrite, annoncée ou documentée publiquement, et à ce
   qu'une finalité à un seul signataire autorise à affirmer, sort du périmètre de ce
   document. Je n'en traite aucun aspect.

---

## Annexe A — Un piège dans l'outillage existant

`scripts/rotate-validators.js:168` fabrique les adresses de vote ainsi :

```js
const listeVotes = listeVals.map((a) => '0x' + ethers.keccak256(a).slice(2).padEnd(96, '0').slice(0, 96));
```

Le commentaire qui précède (`:161-166`) l'assume : ce sont des **marque-places**,
destinés uniquement à satisfaire la garde d'unicité du contrat, parce que la clé
« naturelle » — 48 octets nuls — serait identique pour tous et provoquerait un revert
`duplicate vote address` dès N ≥ 2.

**Ces 48 octets ne sont pas des clés publiques BLS12-381 valides et ne correspondent à
aucune clé privée.** Si une rotation était effectuée avec ce script, les validateurs
concernés seraient inscrits on-chain avec des clés factices, et l'activation du vote
resterait impossible pour eux jusqu'à une nouvelle transaction `updateValidatorSet`
portant leurs **vraies** clés publiques.

Le contrat ne peut pas s'en apercevoir : il vérifie la longueur (48 octets) et
l'unicité, **jamais la validité cryptographique**. Ce document l'a vérifié en pratique —
la simulation `eth_call` du § 4, étape 6, réussit avec 48 octets de remplissage
arbitraire.

---

## Annexe B — Pourquoi `CoinbosaStake.sol` ne répond pas à cette question

Trois raisons **indépendantes**, chacune suffisante :

1. **Il n'est déployé nulle part.** Il n'apparaît pas dans `scripts/build-genesis.js` ;
   l'`alloc` du genesis ne porte de code que pour `0x…1000` (6 060 octets déployés),
   `0x…1001`, `0x…1002` (1 802 o) et `0x…1007` (4 861 o). Les seules références à `CoinbosaStake` dans
   l'arbre sont `contracts/EssaiSelection.sol` et deux scripts d'essai
   (`scripts/test-selection-validateurs.js`, `scripts/test-double-signature.js`).
2. **Même déployé, le consensus ne le lirait pas.** Parlia appelle en dur
   `systemcontracts.ValidatorContract = 0x…1000` (`core/systemcontracts/const.go:5`,
   utilisé `parlia.go:1941`), et la bifurcation « PoBS » décrite dans
   `POBS-ACTIVATION.md` n'existe pas dans le client :
   `grep -rn 'pobsTime\|IsPobs' params/ consensus/parlia/` ne rend qu'un commentaire
   dans `consensus/parlia/coinbosa_pobs_activation_test.go:14`.
3. **Décisif quand bien même : il force la clé de vote du validateur de genèse à zéro.**
   `VOTE_GENESE_A = bytes32(0)` et `VOTE_GENESE_B = bytes16(0)`
   (`CoinbosaStake.sol:73-74`) ; la place 0 est réservée au validateur de genèse et sa
   clé vient de ces constantes, jamais de son entrée (`:882-891`, commentaire
   explicite) ; et `deposer()` lui interdit de candidater
   (`if (msg.sender == VALIDATEUR_GENESE) revert GeneseNonCandidate();`, `:997`).

Autrement dit, **sous `CoinbosaStake` tel qu'écrit, l'unique validateur actuel resterait
structurellement incapable de voter.** Son stockage de clés BLS
(`elusVoteA[41]`/`elusVoteB[41]`, `:193-194` ; garde de longueur 48 à l'écriture,
`:1009` ; refus de la clé nulle, `:1027` ; refus des doublons, `:1030`) ne concerne que
d'éventuels validateurs **entrants**.

---

## Annexe C — Deux points de lecture qui prêtent à confusion

**C.1 — `INITIAL_VALIDATOR` vaut `0x…0002` dans la source, et ce n'est pas une erreur.**
`contracts/CoinbosaValidatorSet.sol:30` et `:41` déclarent `GOVERNOR = 0x…0001` et
`INITIAL_VALIDATOR = 0x…0002`. Ce sont des **valeurs de marque blanche**, réécrites à la
génération par `scripts/build-genesis.js:127, 137` avec les adresses réelles. Les
lectures on-chain du § 1.1 confirment que le bytecode déployé porte bien
`0x1eEF…082A` et `0x3986…bA50`. **Ne modifiez jamais ces constantes à la main.**

**C.2 — `geth bls account generate-proof` ne sert à rien sur cette chaîne.** La
sous-commande existe (`blsaccountcmd.go:198-213, 605-666`) mais sa description la
réserve explicitement à la création de validateur « on BSC after feynman upgrade ». Le
genesis Coinbosa **n'active pas Feynman** (aucun `feynmanTime` dans
`genesis/genesis-coinbosa.json`) et `CoinbosaValidatorSet.updateValidatorSet` ne vérifie
**aucune** signature de possession. **Aucune preuve de propriété n'est requise ici** —
ce qui est aussi la raison pour laquelle une clé factice passe (annexe A).

---

## Annexe D — Aucune procédure d'activation n'existait avant ce document

```
$ grep -rn -- '--vote|--blspassword|--blswallet|vote-journal-path|bls wallet create|bls account new' \
    /Users/protocole/repo/coinbosa/
(aucun resultat)
```

Les documents du projet **constatent** l'état sans dire comment en sortir :
`README.md:233` (« les clés BLS sont à zéro, le vote d'attestation est inactif ») et
`docs/INTEGRATION.md:173-178, :231`. La procédure du § 4 n'était écrite nulle part
ailleurs que dans le code source du client.

**Ces deux documents devront être relus après l'activation** : leurs formulations
actuelles deviendront fausses, et `docs/INTEGRATION.md` — celui que lit un intégrateur —
donne aujourd'hui une consigne de confirmations fondée sur l'absence de finalité.
