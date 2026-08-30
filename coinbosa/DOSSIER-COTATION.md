<div align="center">
  <img src="assets/coinbosa-logo.jpg" alt="Coinbosa" width="90" />

  # Dossier de cotation — Coinbosa Chain

  **BOSA · chainId 26262 · consensus Parlia**
</div>

---

## Comment lire ce document

Chaque chiffre de ce dossier a été **interrogé sur la chaîne ou dans le dépôt**, jamais
recopié d'un document antérieur. La commande qui l'établit est citée à côté. Ce qui n'a pas
pu être vérifié est écrit **« non vérifié »**, avec la raison.

Trois faits contredisent des documents publiés du projet. Ils sont dans ce dossier, aux
sections [8](#8--le-jeton-historique-sur-solana--écart-mesuré) et
[11](#11--écarts-relevés-entre-les-documents-publiés-et-la-chaîne). Une place d'échange les
trouvera en quelques minutes ; il vaut mieux qu'elle les lise ici.

**Bloc de référence des mesures : 403 466**, horodaté `2026-08-30T22:18:59Z`.
Les mesures d'agrégat (balayage complet) portent sur les blocs 1 à 403 419.

```
RPC=https://explorer.coinbosa.com/rpc
```

---

## 1 — Identité de la chaîne

| Élément | Valeur | Comment l'obtenir |
|---|---|---|
| Nom | Coinbosa Chain | `chainid.network/chains.json`, entrée 26262 |
| chainId décimal | **26262** | `net_version` → `"26262"` |
| chainId hexadécimal | **0x6696** | `eth_chainId` → `"0x6696"` |
| Empreinte du bloc 0 | `0x8dcdadc247a98f33728cae944e20ce7c49c74b35cfba31495f85e98979018da6` | `eth_getBlockByNumber("0x0")` |
| Racine d'état du bloc 0 | `0x93682eb9182a55531d47014b76a285b45d3e720a2951f9ffbdc67f52995f8c03` | idem |
| Horodatage du bloc 0 | **0** — soit `1970-01-01T00:00:00Z` | idem, champ `timestamp` |
| Date du **premier bloc produit** (bloc 1) | **2026-08-07T13:39:55Z** (unix `1786109995`) | `eth_getBlockByNumber("0x1")` |
| Âge de la chaîne au bloc de référence | **23,360 jours** | `(ts(403466) − ts(1)) / 86400` |
| Consensus | **Parlia** | `genesis-coinbosa.json` → `config.parlia` |
| Client | **fork de `bnb-chain/bsc` v1.7.6**, commit `7315f42a`, `go1.25.12`, `linux` | décodage RLP de l'`extraData` d'un en-tête |

Le genesis porte `timestamp: 0`. Ce n'est pas une erreur de lecture : le bloc 0 n'est pas
daté, la chaîne commence réellement au **bloc 1**. Une place d'échange qui affiche « date de
lancement » doit utiliser la date du bloc 1, pas celle du bloc 0.

**Provenance du binaire.** L'`extraData` de chaque en-tête contient la version du client qui
l'a scellé. Décodée :

```
extraData(bloc 1) → liste RLP : [0x010706, "7315f42a", "go1.25.12", "lin"]
                    soit version 1.7.6, commit 7315f42a, Go 1.25.12, Linux
```

Le commit `7315f42a` existe dans le dépôt public :
`7315f42aa 2026-08-06 10-web.sh : corrige la panne de Caddy au premier déploiement des journaux d'accès`.
Le binaire en production correspond donc à un état publié du dépôt.

### Le genesis est reproductible

C'est le point le plus important de cette section, parce qu'il rend l'identité de la chaîne
vérifiable **sans faire confiance à l'éditeur**.

Le fichier genesis a été **reconstruit à partir du dépôt public**, avec `solc 0.8.26` et les
deux adresses publiques (validateur, gouverneur) :

```bash
VALIDATOR=0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50 \
GOVERNOR=0x1EEf3830833d83AcD3152A511853fd04a0b4082A \
OUT=/tmp/genesis-rebuilt.json node scripts/build-genesis.js
```

Résultat : **identique octet pour octet** au genesis publié.

```
4d93164f2364323d0156b8a1255dea060a10496459d755389a51babab75b7ce7  /tmp/genesis-rebuilt.json
4d93164f2364323d0156b8a1255dea060a10496459d755389a51babab75b7ce7  coinbosa/genesis/genesis-coinbosa.json
```

Un tiers peut donc, seul : reconstruire le genesis, l'initialiser avec `geth init`, obtenir
l'empreinte du bloc 0, et la comparer à celle servie par le RPC public. Si les deux
coïncident — c'est le cas — **aucune allocation cachée n'est possible** : la racine d'état
engage la totalité de l'état initial, un seul wei ajouté à une adresse même inconnue la
changerait.

Le contrôle est aussi outillé dans le dépôt :

```
$ RPC=https://explorer.coinbosa.com/rpc node scripts/check-genesis-hash.js
  chaîne conforme à la référence publiée (aucune allocation cachée possible)   [code de sortie 0]
```

---

## 2 — Paramètres du réseau

| Paramètre | Valeur mesurée | Méthode |
|---|---|---|
| Temps de bloc **cible** | 5 s | `genesis.config.parlia.period = 5` ; constante Go du client |
| Temps de bloc **mesuré** | **5,0000 s exactement** | 2 400 intervalles, 3 fenêtres de 800 blocs (démarrage / milieu / récent) : moyenne 5,0000 · min 5 · max 5 · 100,00 % à 5 s |
| Contrôle à la milliseconde | **5,000 s**, min = max | champ `milliTimestamp` des en-têtes, mêmes fenêtres |
| Temps de bloc **moyen sur toute la chaîne** | **5,0025 s** | 403 418 intervalles, blocs 1 → 403 419. L'écart à 5,0000 vient de 23 anomalies (§ 10) |
| Epoch | **200 blocs** | `genesis.config.parlia.epoch = 200` ; `defaultEpochLength = 200` dans `consensus/parlia/parlia.go:58` |
| Vérification empirique de l'epoch | **2 016 blocs sur 2 016** multiples de 200 portent une transaction système de 693 gaz ; **aucun** autre bloc n'en porte | balayage complet des 403 419 blocs |
| **Plafond de gaz au bloc 0** | **40 000 000** (`0x2625a00`) | `eth_getBlockByNumber("0x0")` |
| **Plafond de gaz aujourd'hui** | **55 000 000** (`0x3473bc0`) | `eth_getBlockByNumber("latest")` |
| Bloc où le plafond actuel est atteint | **bloc 327** | dichotomie : b325 = 54 932 051 · b326 = 54 985 694 · **b327 = 55 000 000** |
| Stabilité du plafond depuis | **55 000 000 sur les 2 016 blocs échantillonnés au-delà du bloc 400**, et sur les fenêtres milieu et récente | balayage complet |
| `baseFeePerGas` | **0** sur le bloc 0 et sur le bloc courant | `eth_getBlockByNumber` |
| Prix du gaz recommandé | **1 gwei** (`0x3b9aca00`) | `eth_gasPrice`, `eth_maxPriorityFeePerGas` |
| Machine virtuelle | EVM, jeu d'instructions **Shanghai** | `genesis.config.shanghaiTime = 0` |
| Format d'adresse | `0x…` 20 octets, somme de contrôle EIP-55 | standard EVM |
| Transactions signées | héritées d'Ethereum : legacy, EIP-2930, EIP-1559 | client geth 1.7.6 |

**Les deux plafonds diffèrent, et c'est normal.** Le genesis fixe 40 000 000 ; le validateur
est lancé avec `--miner.gaslimit 55000000`. Geth fait converger le plafond vers la consigne
par pas de 1/1024 par bloc — 327 blocs, soit 27 minutes. Un document qui n'annoncerait qu'une
seule de ces deux valeurs serait faux la moitié du temps.

---

## 3 — Le coin natif

| Élément | Valeur | Source |
|---|---|---|
| Nom | **Coinbosa** | `chainid.network/chains.json` → `nativeCurrency.name` |
| Symbole | **BOSA** | idem, `nativeCurrency.symbol` |
| Décimales | **18** | idem ; imposées par l'EVM (unité de base = wei) |
| Offre totale | **700 000 000 BOSA** = `7e26` wei | somme des allocations du bloc 0 |
| Émission | **aucune** — pas de récompense de bloc | le moteur Parlia ne crédite aucun coin ; § 4 |
| Destruction (*burn*) | **aucune** — `baseFeePerGas = 0`, rien n'est brûlé | mesuré : l'offre est conservée au wei près |
| **Offre en circulation** | **0** — voir ci-dessous | § 7 |

### Conservation de l'offre — vérifiée au wei près

La chaîne a connu **une seule transaction utilisateur** depuis le bloc 1 (§ 9). L'offre se
répartit donc, au bloc de référence, sur exactement 15 comptes :

```
13 adresses de répartition   699 998 999,999979 BOSA
gouverneur (0x1EEf…082A)           1 000,000000 BOSA
contrat système 0x…1000                0,000021 BOSA   (frais de la transaction unique)
--------------------------------------------------------
TOTAL                        700 000 000,000000 BOSA
```

Le total tombe **exactement** sur 700 000 000. Aucune émission, aucune destruction.

### Ce que « offre en circulation » veut dire ici

La notion n'a pas de contenu marchand aujourd'hui : **BOSA n'a pas de marché**, aucune place
d'échange ne le cote, et aucun BOSA n'est détenu par un tiers. Les 700 000 000 sont sur
13 clés du projet, plus 1 000 BOSA sur l'adresse du gouverneur — elle aussi du projet.

- **Offre en circulation au sens « détenue hors du projet » : 0 BOSA (0,00 %).**
- **Offre débloquée (aucun verrou contractuel, aucun séquestre) : 700 000 000 BOSA (100 %).**

Ce second chiffre est celui qui compte pour un risque de marché : **il n'existe aucun
calendrier de déblocage, aucun contrat de séquestre, aucun *timelock***. Les 13 adresses sont
des clés simples, dépensables immédiatement et intégralement. Un document qui annoncerait un
« calendrier de déblocage » serait faux : il n'y en a pas.

---

## 4 — Points d'accès

| Ressource | URL | État vérifié |
|---|---|---|
| RPC JSON public | `https://explorer.coinbosa.com/rpc` | **répond** ; POST uniquement (GET → 405) |
| Explorateur | `https://explorer.coinbosa.com` | HTTP **200** |
| Site | `https://coinbosa.com` | HTTP **200** (`www` redirige en 301 vers l'apex) |
| Livre blanc | `https://coinbosa.com/whitepaper/` | HTTP **200** |
| Dépôt | `https://github.com/Coinbosa/coinbosa-chain` | HTTP **200** |
| Registre de chaînes | `chainid.network/chains.json` | **entrée 26262 présente** parmi 2 735 chaînes |
| WebSocket | **aucun** | ni route `wss` dans Caddy, ni `--ws` dans l'unité systemd du nœud |
| Point d'accès d'archive | **aucun** | nœud en `--gcmode full` ; § 12 |

État du dépôt au moment de la rédaction : branche `coinbosa-genesis-bos20`, commit
`a734cd2da9f2af0212b08c7286e38b2d332e8ed4`.

**Logo du registre — non vérifié.** L'entrée **publiée** du registre (`_data/icons/coinbosa.json` chez `ethereum-lists/chains`) référence l'icône
`ipfs://bafkreiaid7e3fiurx6ubdyjrbzlmrkvvrax5z5gjuoavdgv5dnu4usxb4a`. Trois passerelles IPFS
publiques (`ipfs.io`, `cloudflare-ipfs.com`, `dweb.link`) n'ont **pas** servi le fichier —
délai d'attente dépassé ou 504. Le contenu n'est peut-être plus épinglé. À confirmer auprès
du service d'épinglage avant qu'une place d'échange ne récupère le logo depuis le registre.

---

## 5 — Le standard de jeton

| Élément | Valeur |
|---|---|
| Nom du standard | **BRC20** |
| Interface | `coinbosa/contracts/IBRC20.sol` |
| Implémentation de référence | `coinbosa/contracts/BRC20.sol` |
| Version Solidity | `pragma solidity 0.8.26` |
| Surface | ERC-20 intégrale — `name`, `symbol`, `decimals`, `totalSupply`, `balanceOf`, `transfer`, `approve`, `allowance`, `transferFrom`, événements `Transfer`/`Approval` — plus `getOwner()` |

BRC20 est **compatible ERC-20 au niveau de l'interface** : tout outil, portefeuille ou
indexeur qui lit un ERC-20 lit un BRC20 sans modification.

**Aucun jeton BRC20 n'est déployé sur la chaîne à ce jour.** Vérifié par balayage complet des
403 419 blocs : **zéro création de contrat** par un utilisateur depuis le bloc 1. Les seuls
contrats existants sont les quatre contrats système du bloc 0 (§ 9).

---

## 6 — Contrats système déployés

Quatre adresses portent du code. Elles sont inscrites au bloc 0 ; aucune ne peut être
redéployée.

| Adresse | Rôle | Taille | Origine | Code source dans le dépôt |
|---|---|---|---|---|
| `0x…1000` | `CoinbosaValidatorSet` — jeu de validateurs, frais | **6 060 o** | **écrit pour Coinbosa** | **oui** — `contracts/CoinbosaValidatorSet.sol` |
| `0x…1001` | `SlashIndicator` — sanctions | 7 339 o | hérité de BNB Chain, **inchangé** | **non** |
| `0x…1002` | `SystemReward` | 1 802 o | hérité de BNB Chain, **inchangé** | **non** |
| `0x…1007` | `GovHub` | 4 861 o | hérité de BNB Chain, **inchangé** | **non** |

Vérification faite pour les quatre : le bytecode servi par `eth_getCode` est **identique** à
celui inscrit dans `genesis-coinbosa.json`. Pour `0x…1001`, `0x…1002` et `0x…1007`, il est en
outre identique au bytecode du gabarit amont `genesis-base.json` — ces trois contrats n'ont
pas été modifiés. Pour `0x…1000`, il diffère de l'amont (5 758 o → 6 060 o) : c'est le
contrat de remplacement écrit pour Coinbosa.

Les autres adresses système du réseau amont (`0x…1003` à `0x…1006`, `0x…1008`, `0x…2000`)
ont été **purgées de leur code et de leur solde** : elles servaient au pont inter-chaînes,
sans objet sur une chaîne souveraine. Vérifié : **0 octet** à chacune.

Conséquence pour la sécurité : **il n'existe aucun pont inter-chaînes sur Coinbosa Chain.**
Aucun BOSA ne peut entrer ou sortir par un mécanisme de protocole.

---

## 7 — Répartition de l'offre

Soldes réels lus au bloc 403 277 (`eth_getBalance`), et non recopiés du livre blanc.

| # | Poste | Adresse | Solde mesuré (BOSA) | Part de l'offre | Nonce |
|---|---|---|---|---|---|
| 1 | Développement | `0xCa6f08e549290BbF161fF45c475fd3f7A6e65f04` | 140 000 000 | 20,0000 % | 0 |
| 2 | Technique | `0xf4cEbe2d34A9a996cAD0c02345d6c3fB69B0E6C1` | 70 000 000 | 10,0000 % | 0 |
| 3 | Recherche | `0xb3B91c44f7D48e814aC37c3ED3C691eEDd728b1b` | 70 000 000 | 10,0000 % | 0 |
| 4 | Équipe | `0x41Ab22491Ba87eda15927286D744ebdaAE5B2FC9` | **69 998 999,999979** | 9,9999 % | **1** |
| 5 | Fonds financier Card | `0x59dcf9E2A5C17D6C32dC00feCdd8419954494E3f` | 70 000 000 | 10,0000 % | 0 |
| 6 | Fonds de liquidité | `0xF85C43a06032F557323545dC3353f31dF1fBDD65` | 70 000 000 | 10,0000 % | 0 |
| 7 | Recherche IA | `0x7a8E70400Af9b66E22cefF574Dba9B293f3Ca6b5` | 70 000 000 | 10,0000 % | 0 |
| 8 | Recherche finance/fintech | `0x6baA7353Ed90dACB4d6C1A2DA53cbf77DF7F2E32` | 35 000 000 | 5,0000 % | 0 |
| 9 | Distribution publique | `0x47f0c3e1D2c9EA164986c58612CafD39bb89ED41` | 35 000 000 | 5,0000 % | 0 |
| 10 | Sécurité | `0x31CAD23D872c4cf7Eb22FC4B27f3094654b95DF8` | 21 000 000 | 3,0000 % | 0 |
| 11 | Réserve stratégique | `0x69B3C57Ba943c31489Eb6A1d7727f550B42512F8` | 21 000 000 | 3,0000 % | 0 |
| 12 | Audit | `0x223C546d25032E209556e9607041F0A1EFe4674D` | 14 000 000 | 2,0000 % | 0 |
| 13 | Événements / formation | `0xd53de8724Fef3Dc24bF12a34adEf68c3Cd30c07E` | 14 000 000 | 2,0000 % | 0 |
| | **Sous-total 13 adresses** | | **699 998 999,999979** | **99,99986 %** | |
| — | Gouverneur | `0x1EEf3830833d83AcD3152A511853fd04a0b4082A` | 1 000 | 0,00014 % | 0 |
| — | Contrat `0x…1000` (frais perçus) | `0x0000000000000000000000000000000000001000` | 0,000021 | ~0 % | — |
| | **TOTAL** | | **700 000 000,000000** | **100 %** | |

Le poste **Équipe** est le seul à avoir bougé : nonce 1, une transaction de **1 000 BOSA**
vers le gouverneur au bloc 160 399, plus 0,000021 BOSA de frais. Les 12 autres postes ont un
nonce de 0 : **aucune sortie de fonds** depuis le genesis.

### Ce qu'un tiers peut vérifier seul

- Le **solde exact** de chacune des 13 adresses, à tout instant — `eth_getBalance`.
- Que la somme fait **exactement 700 000 000 BOSA** et n'a jamais changé.
- Qu'**aucune émission cachée n'existe** — l'empreinte du bloc 0 servie par le RPC est celle
  qu'il reconstruit lui-même depuis le dépôt public (§ 1).
- Que **les 13 adresses ne portent aucun code** : `eth_getCode` renvoie 0 octet aux treize.
  Ce sont des clés simples, **pas des coffres multi-signatures**.
- Que **12 des 13 n'ont jamais émis de transaction** — `eth_getTransactionCount` = 0.
- Que **tous les mouvements historiques** de la chaîne sont visibles : une seule transaction
  utilisateur, `0xb10cf391c74a81336e7e4037f84e30ceacab52a59d239452453360b5a9790544`.

### Ce qu'il doit croire sur parole

- **Que le projet contrôle réellement ces 13 clés.** Rien on-chain ne le prouve pour les 12
  adresses au nonce 0 : elles n'ont jamais signé. Une signature de message par adresse
  (*proof of control*) lèverait ce doute en une heure — voir § 15.
- **Que les 13 clés sont indépendantes les unes des autres.** Elles ne le sont pas au sens
  d'une garde répartie : la marche à suivre retenue par le projet
  (`docs/GENESIS-PRODUCTION.md`) dérive **les 13 adresses et le gouverneur d'un seul `xpub`**,
  donc **d'une seule phrase de récupération de portefeuille matériel**
  (`scripts/derive-treasury-addresses.js`, chemin `m/44'/60'/0'/0/i`). **Treize adresses ne
  signifient donc pas treize détenteurs : un seul secret ouvre les 700 000 000 BOSA, et
  ouvre aussi la gouvernance du consensus.** C'est le fait le plus lourd de ce dossier.
- **Que les fonds ne bougeront pas.** Aucun verrou technique ne l'empêche.
- **L'affectation des postes** (« recherche », « liquidité »…) : ce sont des étiquettes de
  document, sans traduction on-chain.

---

## 8 — Le jeton historique sur Solana : écart mesuré

Le projet a émis, avant cette chaîne, un jeton SPL sur Solana. Le livre blanc
(`WHITEPAPER.md` § 4) écrit qu'il est « **détenu dans sa totalité par le projet** » et que les
500 000 000 unités « **seront retirées de la circulation sur Solana, de manière publique et
vérifiable** ».

**Mesure faite sur Solana mainnet le 2026-08-30**, mint
`8UyvxCoVXoVaftWzp7j9yo2sGL2HnHTFDV4capenyFaf` :

| Fait mesuré | Valeur | Méthode |
|---|---|---|
| Le jeton **existe toujours** | oui | `getAccountInfo` → `isInitialized: true` |
| Offre en circulation | **499 999 940,39** (10 décimales) | `getTokenSupply` |
| Détenu par le portefeuille projet `5pdFbZ…edQf` | **479 990 400** | `getTokenAccountsByOwner` |
| Part détenue par ce portefeuille | **96,00 %** | calcul |
| **Détenu ailleurs** | **20 009 540,39 — soit 4,00 %** | calcul |
| `mintAuthority` | **`3zADMByrBhWTnQETN2gv5Gt7jhQKyyprjLCLVVnv2Pkq` — active** | `getAccountInfo` |
| `freezeAuthority` | **`3zADMByrBhWTnQETN2gv5Gt7jhQKyyprjLCLVVnv2Pkq` — active** | `getAccountInfo` |

Trois conclusions, toutes défavorables au texte publié :

1. **Le retrait de circulation n'a pas eu lieu.** Le jeton existe, son offre est quasi
   intacte.
2. **Le projet ne détient pas la totalité.** 4,00 % — plus de 20 millions d'unités — sont
   hors du portefeuille désigné comme portefeuille projet. Si des détenteurs tiers existent,
   la phrase « sans détenteurs tiers, ils ne donnent lieu à aucune migration » ne tient plus,
   et la réserve de migration à 0 devient une décision à réexaminer.
3. **L'autorité d'émission n'est pas révoquée.** Plus d'unités peuvent être créées sur
   Solana à tout moment, par le détenteur de cette clé.

**Non vérifié :** la liste détaillée des détenteurs Solana. `getTokenLargestAccounts` est
limité par débit sur les points d'accès publics (HTTP 429 sur trois tentatives ;
`solana-rpc.publicnode.com` exige un jeton). Les 4,00 % pourraient appartenir à d'autres
portefeuilles du projet — **rien ne le prouve depuis l'extérieur, et rien ne le réfute**.

**Non vérifié :** l'affirmation « des jetons avaient aussi été émis sur BNB Chain ; ce jeton
n'existe plus ». Aucune adresse de contrat n'est publiée pour lui, la vérification est donc
impossible. Une place d'échange demandera cette adresse.

Une place d'échange qui liste BOSA fera cette vérification. Si le dossier annonce « offre
totale 700 000 000 » sans mentionner les ~500 000 000 unités SPL toujours vivantes et
toujours émissibles, l'écart sera lu comme une dissimulation.

---

## 9 — L'état du consensus, sans fard

### Ce qui tourne aujourd'hui

| Fait | Valeur mesurée | Méthode |
|---|---|---|
| Moteur | Parlia | configuration du genesis |
| Nature réelle | **preuve d'AUTORITÉ** — aucun enjeu, aucun dépôt | §  ci-dessous |
| **Nombre de validateurs** | **1** | `numOfValidators()` sur `0x…1000` → `1` |
| Adresse du validateur | `0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50` | `getValidators()` |
| Producteurs distincts sur **toute l'histoire** | **1** — le même sur les 403 419 blocs | balayage complet, champ `miner` |
| Longueur de tour (*turn length*) | 1 | `getTurnLength()` |
| Places maximales | 41 | `MAX_VALIDATORS()` |
| Pairs réseau du nœud RPC | **1** (le validateur) | `net_peerCount` → `0x1` |
| Difficulté des blocs | `0x2` — « en tour », toujours | balayage |

**Tolérance aux pannes : nulle.** Un seul validateur produit tous les blocs. S'il s'arrête,
la chaîne s'arrête — c'est arrivé, § 10. **Sécurité byzantine : nulle.** Il n'y a pas de
majorité à obtenir : le producteur unique décide seul de l'ordre des transactions et peut en
exclure.

### Aucune sanction n'est possible aujourd'hui

| Fait | Vérification |
|---|---|
| Le contrat système ne contient **aucune** notion de sanction | 0 occurrence de `slash`, `jail`, `stake`, `bond`, `delegat` dans `CoinbosaValidatorSet.sol` |
| Le contrat de sanction est **inerte** | `SlashIndicator` est bien déployé (7 339 o à `0x…1001`), mais `misdemeanor(address)` et `felony(address)` **échouent** (`execution reverted`) — les fonctions cibles n'existent pas dans le contrat de validateurs |
| Un échec de sanction **n'arrête pas** la chaîne | l'erreur est journalisée puis avalée, symétriquement à la production et à la vérification (`consensus/parlia/parlia.go`) |

**Aucun validateur ne risque de fonds. Aucun comportement fautif n'a de conséquence
automatique.** Ce n'est pas un défaut de configuration, c'est l'état de la conception.

### Aucune finalité

| Fait | Valeur | Méthode |
|---|---|---|
| Étiquette `finalized` | **bloc 0** | `eth_getBlockByNumber("finalized")` |
| Étiquette `safe` | **bloc 0** | `eth_getBlockByNumber("safe")` |
| Clé de vote BLS du validateur | **48 octets, tous nuls** | `getMiningValidators()` |

La finalité rapide de Parlia repose sur des attestations BLS. La clé de vote est nulle :
**aucune attestation n'est produite, aucun bloc n'est jamais finalisé.** Les étiquettes
`finalized` et `safe` resteront au bloc 0 indéfiniment.

> **À l'attention de l'équipe d'intégration d'une place d'échange :** ne fondez **jamais** le
> crédit d'un dépôt sur l'étiquette `finalized` ou `safe`. Elles ne progresseront pas. Utilisez
> un **nombre de confirmations** fixe. Une intégration standard qui attend `finalized` ne
> créditera **aucun** dépôt.

### La couche d'enjeu PoBS : spécifiée, NON DÉPLOYÉE

`coinbosa/POBS.md` spécifie une preuve d'enjeu bornée — 41 places, enjeu minimum 1 000 BOSA,
déblocage 49 jours, voie retenue « bifurcation du client ». **Rien n'en est construit.** Le
document lui-même l'écrit en tête : *« Spécification. Rien de ce qui suit n'est construit à ce
jour. »*

Vérifié indépendamment sur la chaîne :

```
eth_getCode(0x…2001)  → 0 octet    (Staking)
eth_getCode(0x…2002)  → 0 octet    (StakeHub)
eth_getCode(0x…2003)  → 0 octet    (StakeCredit)
```

Deux verrous du contrat figé au bloc 0 commandent tout ce qui suivra, et doivent être connus
d'une place d'échange :

- **Le gouverneur est une constante du bytecode.** Seule l'adresse
  `0x1EEf3830833d83AcD3152A511853fd04a0b4082A` peut appeler `updateValidatorSet`. Elle est
  gravée : aucun contrat d'enjeu ne pourra jamais piloter le jeu de validateurs directement.
  Vérifié : cette adresse **ne porte aucun code** — c'est une clé simple, ni multi-signatures,
  ni sous délai.
- **Le validateur de genèse est permanent.** `updateValidatorSet` refuse tout ensemble qui ne
  contient pas `INITIAL_VALIDATOR`. Le validateur actuel **ne peut jamais être exclu, ni
  sanctionné, ni remplacé.** Une place sur 41 est immuable, définitivement.

### Trafic réel de la chaîne

Balayage complet des blocs 1 à 403 419 :

| Fait | Valeur |
|---|---|
| Transactions totales | **2 026** |
| dont transactions **système** (signées par le validateur) | **2 025** — 7 d'initialisation au bloc 1, 2 017 aux blocs d'epoch, 1 de dépôt de frais |
| dont transactions **utilisateur** | **1** |
| Créations de contrat par un utilisateur | **0** |
| Blocs vides | 401 400 sur 403 419 — **99,4995 %** |

La transaction utilisateur unique :

```
hash    0xb10cf391c74a81336e7e4037f84e30ceacab52a59d239452453360b5a9790544
bloc    160 399
de      0x41Ab22491Ba87eda15927286D744ebdaAE5B2FC9   (poste « Équipe »)
vers    0x1EEf3830833d83AcD3152A511853fd04a0b4082A   (gouverneur)
montant 1 000 BOSA          gaz 21 000 à 1 gwei = 0,000021 BOSA
```

**Le réseau n'a pas d'usage.** Ce n'est pas un jugement, c'est la mesure. Une place d'échange
le constatera immédiatement et en tirera ses conclusions sur la profondeur du marché.

---

## 10 — Disponibilité mesurée

Balayage des 403 418 intervalles entre blocs consécutifs, blocs 1 → 403 419.

| Indicateur | Valeur |
|---|---|
| Intervalles à **exactement 5 s** | **403 395 / 403 418 = 99,99430 %** |
| Intervalles anormaux | **23** |
| Temps cumulé perdu | **1 019 s = 17,0 minutes** |
| Fenêtre observée | 23,358 jours (2026-08-07 → 2026-08-30) |
| **Disponibilité temporelle** | **99,9495 %** |
| **Plus longue interruption** | **659 s — 11 minutes** |

### Les interruptions de 10 s et plus

| Blocs | Durée | Horodatage UTC du dernier bloc avant l'arrêt |
|---|---|---|
| 22 237 → 22 238 | **659 s** | 2026-08-08 20:32:55 |
| 48 537 → 48 538 | 82 s | 2026-08-10 09:18:01 |
| 83 613 → 83 614 | 82 s | 2026-08-12 10:02:26 |
| 22 363 → 22 364 | 66 s | 2026-08-08 20:54:40 |
| 22 397 → 22 398 | 56 s | 2026-08-08 20:58:31 |
| 22 276 → 22 277 | 22 s | 2026-08-08 20:47:08 |
| 24 528 → 24 529 | 14 s | 2026-08-08 23:56:57 |
| 204 093 → 204 094 | 12 s | — |
| 25 143 → 25 144 · 246 140 → 246 141 | 11 s | — |
| 58 203 → 58 204 · 321 377 → 321 378 · 373 264 → 373 265 | 10 s | — |

Les 9 écarts restants valent 8 ou 9 secondes.

**Deux motifs à signaler, parce qu'une place d'échange les verra dans ses journaux :**

- **Le 2026-08-08 entre 20:32 et 20:58 UTC**, cinq incidents en 26 minutes, dont l'arrêt de
  11 minutes. Sur une chaîne à un seul validateur, un arrêt du processus est un arrêt du
  réseau : aucune transaction n'est minée pendant ce temps, et aucune transaction corrective
  n'est possible.
- **Un écart quotidien de 9 à 12 secondes, entre 04:17 et 04:23 UTC.** Cause **identifiée
  et attendue** : l'unité `coinbosa-journal.timer` du serveur déclenche un **arrêt propre
  planifié** (`OnCalendar=*-*-* 04:17:00`, `RandomizedDelaySec=300`) destiné à borner la perte
  d'état de geth. Le motif colle à la minuterie sur **neuf jours consécutifs** :

  | Date | Heure UTC du dernier bloc avant l'écart | Écart |
  |---|---|---|
  | 2026-08-22 | 04:22:46 | 9 s |
  | 2026-08-23 | 04:19:00 | 9 s |
  | 2026-08-24 | 04:20:04 | 9 s |
  | 2026-08-25 | 04:21:08 | 9 s |
  | 2026-08-26 | 04:17:52 | 10 s |
  | 2026-08-27 | 04:19:02 | 9 s |
  | 2026-08-28 | 04:20:11 | 9 s |
  | 2026-08-29 | 04:22:00 | 10 s |
  | 2026-08-30 | 04:19:25 | 9 s |

  Coût : **1 bloc manqué par jour**, à heure connue. C'est un compromis assumé — un arrêt
  propre quotidien protège d'un rembobinage bien plus coûteux. À déclarer à la place
  d'échange plutôt qu'à laisser découvrir dans ses journaux de surveillance.

- **Deux écarts hors de cette minuterie restent inexpliqués** : bloc 204 093 le 2026-08-19 à
  09:23:43 UTC (12 s) et bloc 246 140 le 2026-08-21 à 19:47:45 UTC (11 s). **Cause non
  identifiée** : les journaux du serveur n'ont pas été fouillés, la production étant tenue en
  lecture seule pour la rédaction de ce dossier.

---

## 11 — Écarts relevés entre les documents publiés et la chaîne

Trois écarts. Ils sont listés ici pour que la place d'échange les lise dans le dossier plutôt
que de les découvrir.

**a) Le contrôle d'offre du projet échoue sur la chaîne réelle.**

```
$ RPC=https://explorer.coinbosa.com/rpc node scripts/check-supply.js
  offre native on-chain : 699,998,999 BOSA
  attendu               : 700,000,000 BOSA
  ECHEC : soldes on-chain divergents du genesis :
    0x41Ab…2FC9 : 69,998,999 au lieu de 70,000,000
                                                        [code de sortie 1]
```

Ce n'est **pas** une anomalie d'offre. Le script veut lire les soldes **au bloc 0**, mais
l'état du bloc 0 a été purgé (nœud non-archive) : il se rabat sur le bloc courant, où les
1 000 BOSA transférés au gouverneur ne sont plus dans la liste des 13 adresses. L'offre
totale reste **exactement 700 000 000** (§ 3). Le script doit être corrigé pour tenir compte
du gouverneur et du contrat de frais, sans quoi il criera « ÉCHEC » à chaque vérification —
et le premier auditeur externe qui le lancera conclura à un trou dans l'offre.

**b) Le retrait des jetons Solana est annoncé mais n'a pas eu lieu**, et 4,00 % de ces jetons
sont hors du portefeuille projet. § 8.

**c) « 13 adresses de répartition » se lit comme une garde répartie ; ce n'en est pas une.**
Un seul secret dérive les treize et le gouverneur. § 7.

---

## 12 — Intégration technique pour une place d'échange

Testé méthode par méthode contre `https://explorer.coinbosa.com/rpc`.

### Disponible

`eth_chainId` · `net_version` · `net_listening` · `net_peerCount` · `eth_blockNumber` ·
`eth_gasPrice` · `eth_maxPriorityFeePerGas` · `eth_feeHistory` · `eth_getBalance` ·
`eth_getTransactionCount` · `eth_getCode` · `eth_call` · `eth_estimateGas` ·
`eth_getBlockByNumber` · `eth_getBlockByHash` · `eth_getTransactionByHash` ·
`eth_getTransactionReceipt` · `eth_getBlockReceipts` · `eth_getLogs` · `eth_newFilter` ·
`eth_syncing` · **`eth_sendRawTransaction`** (la diffusion de transactions fonctionne).

### Absent — à connaître avant de câbler une intégration

| Manque | Conséquence |
|---|---|
| **État historique** au-delà de ~36 h | `eth_getBalance(addr, "0x1")` → *« historical state … is not available »*. **Profondeur mesurée par dichotomie : 25 758 blocs, soit 35,8 heures** — le plus ancien bloc dont l'état est servi était le **377 800** alors que la chaîne était au **403 558**. Cette borne **avance** : chaque vidage de tampon détruit définitivement une tranche d'historique. **Un indexeur qui rejoue depuis le bloc 0 ne peut pas reconstruire les soldes.** |
| **Namespace `debug_`** | Pas de `debug_traceTransaction`. Aucun traçage d'appels internes. |
| **`txpool_*`** | Impossible d'observer les transactions en attente. |
| **`admin_*`, `web3_clientVersion`** | La version du client n'est pas lisible par RPC (elle l'est via l'`extraData` — § 1). |
| **WebSocket** | Aucun `wss://`. Pas d'abonnement `eth_subscribe` : seule l'interrogation périodique est possible. |
| **`finalized` / `safe`** | Bloquées au bloc 0. **Ne pas s'en servir.** § 9. |

### Limites en vigueur

| Limite | Valeur | Origine |
|---|---|---|
| Requêtes par lot | **50** | `--rpc.batch-request-limit 50` |
| Taille de réponse par lot | 5 000 000 octets | `--rpc.batch-response-max-size` |
| Taille du corps de requête | **32 Ko** | Caddy, `request_body max_size` |
| Plage de `eth_getLogs` | **5 000 blocs** | `--rangelimit` ; au-delà : *« exceed maximum block range: 5000 »* |
| Adresses ou sujets par position de filtre | **20** | `--rpc.logquerylimit 20` ; au-delà : *« exceed max addresses or topics per search position »* |
| Méthode HTTP | **POST uniquement** sur `/rpc` ; tout le reste → **405** | Caddy |

### Le correctif existe dans le dépôt, il n'est pas appliqué

`coinbosa/deploy/73-node-archive.sh` déploie un **second nœud RPC en mode archive** et
**ajoute le point d'accès WebSocket manquant**, sur des ports distincts (8547 / 8548 / 30305),
sans toucher au validateur, au nœud 8545 ni à Caddy. Le script est **réversible**
(`systemctl disable --now coinbosa-node-archive` + suppression du datadir).

**Il n'est pas déployé.** Vérifié sur le serveur :

```
systemctl list-unit-files | grep coinbosa   → coinbosa-node, coinbosa-validator,
                                              coinbosa-journal, coinbosa-peer, coinbosa-watchdog
                                              (aucune unité coinbosa-node-archive)
ls /var/lib/coinbosa/                       → .ethereum, node, validator  (pas de node-archive)
ss -ltnp                                    → 127.0.0.1:8545 seulement ; ni 8547 ni 8548
```

Les deux manques les plus bloquants pour une intégration de place d'échange — **archive** et
**WebSocket** — sont donc à une exécution de script de distance, sur du travail déjà écrit.

### Points d'accès et redondance

**Un seul point d'accès RPC public, servi par un seul nœud, sur un seul serveur, derrière un
seul Caddy — la même machine qui héberge le site et l'explorateur, et qui fait tourner le
validateur.** Il n'y a **aucune redondance** : perte du serveur = perte simultanée de la
chaîne, du RPC, de l'explorateur et du site. La plupart des places d'échange exigent au
minimum deux points d'accès indépendants.

---

## 13 — L'état des audits

**Aucun audit externe n'a été publié à ce jour. Aucun n'a été engagé, à la connaissance de ce
dossier.** `docs/SECURITY-HARDENING.md` l'écrit : *« Le réseau n'a pas fait l'objet d'un audit
externe. »* `WHITEPAPER.md` § 3 : *« Il n'existe ni passerelle vers un autre réseau, ni audit
externe. »*

### Ce qui existe : un audit interne

`coinbosa/docs/AUDIT.md` documente une auto-évaluation adversariale — **36 trouvailles brutes,
19 confirmées, correctifs appliqués** — puis un second passage. Elle a trouvé des défauts
sérieux, dont un qui aurait arrêté la chaîne au démarrage (un `require` révocable sur le
chemin de consensus dans `init()`). Le document se qualifie lui-même : *« Cet audit est une
auto-évaluation. Il ne remplace pas un audit de sécurité externe. »*

**Une auto-évaluation ne compte pas comme un audit dans un dossier de cotation.**

### Le périmètre qu'un audit externe couvrirait — et celui qu'il ne couvrirait pas

Deux fichiers Solidity sont les candidats naturels à un audit externe :

| Fichier | Taille | Déployé sur la chaîne ? |
|---|---|---|
| `contracts/CoinbosaValidatorSet.sol` | 12 474 o de source → 6 060 o de bytecode | **oui**, à `0x…1000` |
| `contracts/BRC20.sol` | 7 792 o | **non** — aucun jeton BRC20 n'existe (§ 5) |

Le fait qu'une place d'échange voudra connaître, dit sans détour :

> **Ce périmètre couvre 1 des 4 contrats système réellement déployés.** Les trois autres —
> `SlashIndicator` (`0x…1001`, 7 339 o), `SystemReward` (`0x…1002`, 1 802 o), `GovHub`
> (`0x…1007`, 4 861 o) — sont du **bytecode hérité de BNB Chain, inscrit tel quel au bloc 0,
> sans code source dans le dépôt Coinbosa**. Ils ne peuvent donc pas être relus au niveau du
> source par l'auditeur, et ils ne peuvent pas être remplacés : leur bytecode fixe la racine
> d'état du bloc 0, donc l'identité de la chaîne.

Atténuation réelle : ces trois contrats sont **identiques octet pour octet** au bytecode
amont de BNB Chain (vérifié, § 6), un code éprouvé en production depuis des années sur une
chaîne majeure. Et deux d'entre eux sont inertes ici — `SlashIndicator` échoue à tout appel
de sanction (§ 9). Cela réduit le risque ; cela ne le documente pas.

Le second contrat candidat, `BRC20.sol`, **n'est déployé nulle part**. L'auditer protège les
émetteurs de jetons futurs, pas la chaîne ni le coin natif.

### Ce que l'audit externe doit vérifier en priorité

`docs/GENESIS-PRODUCTION.md` § 5 fixe la règle de conception à contrôler avant toute autre :

> **Aucune fonction du chemin de consensus ne doit pouvoir échouer.** Un `revert` sur ce
> chemin rend le bloc improduisible, donc **arrête la chaîne** — et sur un réseau à un seul
> validateur, aucune transaction corrective ne peut alors être minée.

### Ce qui tourne en intégration continue

Compilation des contrats, banc de test BRC20, audits de dépendances npm et Go, contrôles de
genesis. Ce sont des contrôles de non-régression, pas un audit de sécurité.

Deux dérogations npm sont ouvertes et datées (`audit-allowlist.json`) : `GHSA-ph9p-34f9-6g65`
et `GHSA-52f5-9888-hmc6`, toutes deux sur le paquet `tmp`, chemin non atteignable
(dépendance de compilation de `solc`, jamais exécutée par un nœud). **Elles expirent le
2026-11-06.**

---

## 14 — Facteurs de risque

Repris **sans adoucissement** de `WHITEPAPER.md` § 10 et de `POBS.md`, avec les mesures de ce
dossier en regard.

**Risques liés à l'offre.** *« L'offre est aujourd'hui concentrée. Tant qu'elle n'est pas
répartie sur des adresses distinctes sous multi-signatures, une seule clé contrôle la
totalité des jetons. Si cette clé contrôle aussi la liste des validateurs, elle contrôle
simultanément la monnaie et le consensus. C'est le risque le plus important du projet. »*
→ **Mesuré : c'est exactement l'état actuel.** Les 13 adresses ne portent aucun code (donc
aucune multi-signature) et la marche à suivre retenue les dérive, avec le gouverneur, d'un
seul `xpub`.

**Risques liés à l'éditeur.** *« Le réseau dépend d'une société unique. Les difficultés de
cette société sont les difficultés du réseau, tant que celui-ci n'est pas opéré par des
validateurs indépendants. »* → **Mesuré : 1 validateur, 1 serveur, 1 pair réseau.**

**Risques liés au jeton.** *« BOSA peut perdre toute valeur. Il n'a pas de marché à la date de
ce document. Sa liquidité future, s'il en acquiert une, n'est pas garantie. »*

**Risques de mise en œuvre.** *« La couche d'enjeu, le passage à plusieurs validateurs, la
finalité rapide et l'infrastructure publique sont des travaux qui peuvent échouer, prendre du
retard, ou aboutir autrement que décrit. »*

**Risques technologiques.** *« Le réseau n'a pas été audité. Le contrat de consensus, s'il
comportait un défaut, pourrait arrêter la chaîne. Un client mal configuré pourrait diverger du
reste du réseau. Ces risques sont réels tant que l'audit externe n'a pas eu lieu. »*

**Risques propres au consensus, repris de `POBS.md` :**

- **Aucun validateur ne risque de fonds.** Le consensus est une preuve d'autorité ; la couche
  d'enjeu n'existe pas.
- **La sanction est inerte.** `misdemeanor` et `felony` échouent ; l'échec est avalé.
- **Le validateur de genèse est inéjectable à vie.** Le contrat figé le garantit. *« Un
  lecteur qui découvre seul qu'un validateur est inéjectable le lira comme une
  dissimulation. »*
- **Le gouverneur est une clé simple, gravée dans le bytecode**, sans multi-signature ni
  délai. Elle seule peut changer le jeu de validateurs, et elle ne peut pas être remplacée
  sans changer l'identité de la chaîne.
- **Quand la couche d'enjeu existera, sa sécurité économique sera faible au départ.** Enjeu
  minimum prévu : 1 000 BOSA, soit **0,000143 %** de l'offre ; les 41 places coûteraient
  41 000 BOSA, **0,0059 %** de l'offre — *« la plus petite adresse de trésorerie pourrait les
  acheter 341 fois »*.
- **Le déblocage de 49 jours ne protégera de rien tant qu'aucun détecteur automatique de
  double signature n'existera** — *« le détecteur est un prérequis, pas un raffinement. »*

**Risques d'exploitation mesurés dans ce dossier :**

- **Aucune redondance d'infrastructure** : un serveur porte la chaîne, le RPC, l'explorateur
  et le site (§ 12).
- **Disponibilité 99,9495 %** sur 23 jours, dont un arrêt de 11 minutes, un bloc manqué par
  jour à 04:17 UTC (arrêt propre planifié, identifié) et deux écarts inexpliqués (§ 10).
- **Aucun bloc n'est jamais finalisé** ; `finalized` reste au bloc 0 (§ 9).
- **État historique limité à ~36 heures** (25 758 blocs mesurés) : un rapprochement
  comptable rétroactif est impossible sur le point d'accès public (§ 12).
- **Le jeton SPL Solana reste vivant et émissible** (§ 8).

---

## 15 — Ce qui manque au dossier, et qu'aucune rédaction ne remplace

Ce dossier peut être écrit mieux. Il ne peut pas être écrit *autour* de ce qui suit. Les trois
manques ci-dessous sont ceux qu'une place d'échange refusera de contourner, et le temps de
rédaction ne les raccourcit pas.

### 1. L'audit externe

Aucun audit publié, aucun engagé. Le périmètre utile couvre **1 des 4 contrats système
déployés** ; les trois autres sont du bytecode hérité sans source. La priorité, fixée par le
projet lui-même : **vérifier qu'aucune fonction du chemin de consensus ne peut échouer** — un
`revert` y arrête la chaîne, définitivement, faute de pouvoir miner la transaction
corrective. Aucune formulation ne remplace le rapport signé d'un tiers.

### 2. Le second validateur

**Un seul validateur produit 100 % des blocs depuis le bloc 1.** Ni tolérance aux pannes, ni
sécurité byzantine, ni finalité. Le producteur unique peut réordonner ou exclure des
transactions, et son arrêt arrête le réseau — c'est mesuré, § 10. La cible affichée est 4 puis
12 validateurs ; la mesure dit 1.

À écrire noir sur blanc si le second validateur n'est pas en place à la cotation : **le
validateur de genèse ne pourra jamais être exclu**, quel que soit le nombre de validateurs
ajoutés ensuite.

### 3. La garde multi-signatures

**Les 700 000 000 BOSA sont sur 13 clés simples** — `eth_getCode` renvoie 0 octet aux treize —
**dérivées d'un seul secret, qui dérive aussi le gouverneur du consensus.** Un seul
compromis emporte donc à la fois la totalité de la monnaie et le contrôle du jeu de
validateurs. C'est le risque que le livre blanc désigne lui-même comme *« le plus important du
projet »*, et il est intact.

Sa résolution ne peut pas être rédigée : elle exige de créer des coffres à seuil ≥ 2 sur N,
avec des signataires distincts, et d'y **déplacer les fonds** — treize transactions on-chain,
publiquement vérifiables.

### Trois pièces de plus, moins lourdes, qu'une place d'échange demandera

- **Preuve de contrôle des 13 adresses.** Douze n'ont jamais signé (nonce 0) : rien ne prouve
  que le projet détient leurs clés. Une signature de message par adresse
  (`personal_sign` d'une phrase datée) règle la question en une heure, sans déplacer un wei.
- **Le sort réel du jeton SPL Solana.** Soit la preuve publique du retrait de circulation,
  soit la correction du livre blanc — et dans les deux cas, l'explication des **4,00 %**
  détenus hors du portefeuille projet, plus la révocation de `mintAuthority` et
  `freezeAuthority` si le jeton doit être considéré comme clos.
- **Un second point d'accès RPC indépendant**, sur une autre machine, avec un nœud
  d'archive et le WebSocket : sans état historique au-delà de 36 heures ni redondance,
  l'intégration comptable d'une place d'échange n'a pas de filet. Le script existe déjà
  (`deploy/73-node-archive.sh`, réversible) — mais il couvre l'archive et le WebSocket **sur
  la même machine** ; la redondance matérielle, elle, reste entièrement à faire.

---

## 16 — Key figures (English)

*Every figure below was queried directly against the chain or the public repository on
2026-08-30. Reference block: **403,466**.*

### Chain identity

| Item | Value |
|---|---|
| Network name | Coinbosa Chain |
| Chain ID (decimal) | **26262** |
| Chain ID (hex) | **0x6696** |
| Genesis block hash | `0x8dcdadc247a98f33728cae944e20ce7c49c74b35cfba31495f85e98979018da6` |
| Genesis state root | `0x93682eb9182a55531d47014b76a285b45d3e720a2951f9ffbdc67f52995f8c03` |
| Genesis timestamp | **0** (`1970-01-01T00:00:00Z`) — the genesis block is undated |
| **First produced block (block 1)** | **2026-08-07 13:39:55 UTC** |
| Chain age at reference block | **23.36 days** |
| Consensus engine | **Parlia** — currently **Proof of Authority** |
| Client | fork of `bnb-chain/bsc` **v1.7.6**, commit `7315f42a`, go1.25.12, linux |
| Genesis reproducibility | **byte-for-byte reproducible** from the public repo + solc 0.8.26 (SHA-256 `4d93164f…b75b7ce7`) |

### Network parameters

| Item | Value |
|---|---|
| Target block time | 5 s |
| **Measured block time** | **5.0000 s exactly** (2,400 sampled intervals; millisecond timestamps min = max = 5.000 s) |
| Chain-wide average | **5.0025 s** over 403,418 intervals (includes 23 anomalies) |
| Epoch length | **200 blocks** (empirically confirmed: 2,016 of 2,016 epoch blocks carry a 693-gas system tx; no other block does) |
| **Gas limit at block 0** | **40,000,000** |
| **Gas limit today** | **55,000,000** — reached at block **327**, constant since |
| Base fee | **0** (no fee burn) |
| Gas price | **1 gwei** |
| EVM revision | Shanghai |
| **Finality** | **none** — `finalized` and `safe` both return **block 0**; validator BLS vote key is 48 zero bytes |

### Native coin

| Item | Value |
|---|---|
| Name / Symbol / Decimals | **Coinbosa / BOSA / 18** |
| Total supply | **700,000,000 BOSA** — fixed at genesis |
| Inflation | **none** — no block reward, the consensus engine mints nothing |
| Burn | **none** — base fee is 0 |
| Supply conservation | verified to the wei: 699,998,999.999979 (13 addresses) + 1,000 (governor) + 0.000021 (system contract `0x…1000`) = **700,000,000.000000** |
| **Circulating supply (held outside the project)** | **0 BOSA (0.00%)** — no market, no exchange listing, no third-party holder |
| **Unlocked supply (no contractual lock, no escrow, no timelock)** | **700,000,000 BOSA (100%)** |
| Vesting schedule | **none exists** |

### Supply distribution — 13 addresses, measured balances

| # | Bucket | Address | BOSA | Share | Nonce |
|---|---|---|---|---|---|
| 1 | Development | `0xCa6f08e549290BbF161fF45c475fd3f7A6e65f04` | 140,000,000 | 20.00% | 0 |
| 2 | Technical | `0xf4cEbe2d34A9a996cAD0c02345d6c3fB69B0E6C1` | 70,000,000 | 10.00% | 0 |
| 3 | Research | `0xb3B91c44f7D48e814aC37c3ED3C691eEDd728b1b` | 70,000,000 | 10.00% | 0 |
| 4 | Team | `0x41Ab22491Ba87eda15927286D744ebdaAE5B2FC9` | **69,998,999.999979** | 10.00% | **1** |
| 5 | Card financial fund | `0x59dcf9E2A5C17D6C32dC00feCdd8419954494E3f` | 70,000,000 | 10.00% | 0 |
| 6 | Liquidity fund | `0xF85C43a06032F557323545dC3353f31dF1fBDD65` | 70,000,000 | 10.00% | 0 |
| 7 | AI research | `0x7a8E70400Af9b66E22cefF574Dba9B293f3Ca6b5` | 70,000,000 | 10.00% | 0 |
| 8 | Finance/fintech research | `0x6baA7353Ed90dACB4d6C1A2DA53cbf77DF7F2E32` | 35,000,000 | 5.00% | 0 |
| 9 | Public distribution | `0x47f0c3e1D2c9EA164986c58612CafD39bb89ED41` | 35,000,000 | 5.00% | 0 |
| 10 | Security | `0x31CAD23D872c4cf7Eb22FC4B27f3094654b95DF8` | 21,000,000 | 3.00% | 0 |
| 11 | Strategic reserve | `0x69B3C57Ba943c31489Eb6A1d7727f550B42512F8` | 21,000,000 | 3.00% | 0 |
| 12 | Audit | `0x223C546d25032E209556e9607041F0A1EFe4674D` | 14,000,000 | 2.00% | 0 |
| 13 | Events / training | `0xd53de8724Fef3Dc24bF12a34adEf68c3Cd30c07E` | 14,000,000 | 2.00% | 0 |
| — | Governor | `0x1EEf3830833d83AcD3152A511853fd04a0b4082A` | 1,000 | 0.0001% | 0 |

**Custody disclosure.** All thirteen addresses return **0 bytes** from `eth_getCode`: they are
**plain externally-owned accounts, not multisig vaults**. The project's own genesis procedure
derives **all thirteen addresses and the governor from a single hardware-wallet `xpub`**
(`m/44'/60'/0'/0/i`). **Thirteen addresses therefore do not mean thirteen custodians: one
recovery phrase opens the entire 700,000,000 BOSA supply and the consensus governance key.**

Twelve of the thirteen have **never signed a transaction** (nonce 0), so on-chain evidence of
key control does not exist for them.

### Consensus — stated plainly

| Item | Value |
|---|---|
| Consensus | Parlia, operated as **Proof of Authority** |
| **Validators** | **1** — `0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50` |
| Distinct block producers over the entire chain | **1**, across all 403,419 blocks |
| Max validator slots | 41 |
| **Slashing** | **impossible today** — `misdemeanor()` and `felony()` revert; the failure is swallowed |
| Genesis validator | **permanently un-removable** — `updateValidatorSet` rejects any set that omits it |
| Governor | a **plain key** hard-coded in the genesis bytecode; no multisig, no timelock |
| **PoBS staking layer** | **specified, NOT DEPLOYED** — `0x…2001`, `0x…2002`, `0x…2003` all return 0 bytes of code |
| Byzantine fault tolerance | **none** |
| Fault tolerance | **none** — one validator stops, the chain stops |
| Network peers | **1** |

### Chain activity (full scan, blocks 1 → 403,419)

| Item | Value |
|---|---|
| Total transactions | **2,026** |
| System transactions (validator-signed) | **2,025** |
| **User transactions** | **1** |
| User contract deployments | **0** |
| Empty blocks | 401,400 (**99.4995%**) |
| Deployed BRC20 tokens | **0** |

### Availability (measured, 23.36 days)

| Item | Value |
|---|---|
| Intervals at exactly 5 s | **403,395 / 403,418 = 99.99430%** |
| Abnormal intervals | 23 |
| Total time lost | **1,019 s = 17.0 min** |
| **Time-based availability** | **99.9495%** |
| **Longest halt** | **659 s (11 min)**, blocks 22,237 → 22,238, 2026-08-08 20:32:55 UTC |
| Recurring pattern | one 9–10 s gap per day between **04:17 and 04:23 UTC** — **identified**: a scheduled clean restart (`coinbosa-journal.timer`, `OnCalendar=*-*-* 04:17:00`), matched on nine consecutive days. Cost: **1 missed block per day**, at a known hour |
| Unexplained gaps | 2 remain: 2026-08-19 09:23:43 UTC (12 s) and 2026-08-21 19:47:45 UTC (11 s) |

### Endpoints

| Resource | URL |
|---|---|
| Public JSON-RPC | `https://explorer.coinbosa.com/rpc` (POST only) |
| Block explorer | `https://explorer.coinbosa.com` |
| Website | `https://coinbosa.com` |
| Whitepaper | `https://coinbosa.com/whitepaper/` |
| Repository | `https://github.com/Coinbosa/coinbosa-chain` |
| Chain registry | listed in `chainid.network/chains.json` under chainId 26262 |
| WebSocket | **none** |
| Archive node | **none** |

### Token standard

**BRC20** — a full ERC-20-compatible interface (`IBRC20.sol` / `BRC20.sol`, Solidity 0.8.26),
plus `getOwner()`. Any ERC-20 tooling works unchanged. **No BRC20 token has been deployed on
the chain to date.**

### Integration warnings for an exchange

1. **Never use the `finalized` or `safe` block tags.** Both are permanently stuck at block 0.
   Use a fixed confirmation count instead. A standard integration waiting on `finalized` will
   credit **no** deposit.
2. **No historical state beyond ~36 hours.** The public node runs `--gcmode full`. Measured
   by binary search: the oldest block whose state is served was **377,800** while the chain was
   at **403,558** — a window of **25,758 blocks (35.8 hours)**, and that boundary **moves
   forward**. An indexer replaying from block 0 cannot reconstruct balances against this
   endpoint.
3. **No `debug_`, no `txpool_`, no WebSocket.** Polling only, no call tracing, no mempool
   visibility.
4. **Limits:** 50 requests per batch, 32 KB request body, 5,000-block `eth_getLogs` range.
5. **Single point of failure.** One server hosts the validator, the RPC node, the explorer and
   the website. There is no redundant endpoint.
6. **The fix is written but not applied.** `coinbosa/deploy/73-node-archive.sh` deploys a
   second, archive-mode RPC node **and** the missing WebSocket endpoint, on separate ports,
   without touching the validator, the live node or Caddy — and it is reversible. Verified on
   the server: **no such unit, no datadir, no listening port.** Archive and WebSocket are one
   script execution away.

### Audits

**No external audit has been published or commissioned to date.** An internal adversarial
self-assessment exists (`docs/AUDIT.md`: 36 raw findings → 19 confirmed → fixed), and it
states plainly that it does not replace an external audit.

Two Solidity contracts are the candidates for external review:
`CoinbosaValidatorSet.sol` (deployed at `0x…1000`) and `BRC20.sol` (not deployed anywhere).
**That scope covers 1 of the 4 system contracts actually deployed on the chain.** The other
three — `SlashIndicator` (`0x…1001`), `SystemReward` (`0x…1002`), `GovHub` (`0x…1007`) — are
BNB Chain bytecode written into block 0 with **no source in the Coinbosa repository**. They
have been verified byte-identical to the upstream bytecode, and they cannot be replaced: their
bytecode fixes the genesis state root, hence the chain's identity.

### Legacy Solana token — discrepancy against the published whitepaper

The whitepaper states the 500,000,000 legacy SPL tokens are held **in full** by the project
and **will be removed from circulation, publicly and verifiably**. Measured on Solana mainnet,
mint `8UyvxCoVXoVaftWzp7j9yo2sGL2HnHTFDV4capenyFaf`:

| Item | Measured |
|---|---|
| Token still exists | **yes** |
| Supply | **499,999,940.39** (10 decimals) |
| Held by the designated project wallet `5pdFbZ…edQf` | **479,990,400 — 96.00%** |
| **Held elsewhere** | **20,009,540.39 — 4.00%** |
| `mintAuthority` | **still active** (`3zADMByrBhWTnQETN2gv5Gt7jhQKyyprjLCLVVnv2Pkq`) |
| `freezeAuthority` | **still active** (same key) |

**The retirement has not happened, the project does not hold 100%, and more tokens can still
be minted on Solana.** Not verified: the identity of the holders of the remaining 4.00%
(public Solana RPC rate-limited `getTokenLargestAccounts`), and the claim that a legacy BNB
Chain token "no longer exists" (no contract address published for it).

### What is missing, and cannot be written around

1. **An external audit** — none published, none commissioned; scope would cover 1 of the
   4 deployed system contracts.
2. **A second validator** — one validator has produced 100% of blocks since block 1; the
   genesis validator can never be removed.
3. **Multisig custody** — 700,000,000 BOSA sit on 13 plain keys derived from one secret, which
   also derives the consensus governor.

Secondary, but an exchange will ask: **proof of control** (signed messages from the 12
never-used addresses), **the real status of the Solana SPL token**, and **a second,
independent, archive-capable RPC endpoint**.

---

## Sources et reproductibilité

Toutes les valeurs de ce dossier proviennent de :

- `https://explorer.coinbosa.com/rpc` — appels JSON-RPC cités dans chaque section
- `https://api.mainnet-beta.solana.com` — pour § 8
- `https://chainid.network/chains.json` — registre public des chaînes
- le dépôt `Coinbosa/coinbosa-chain`, branche `coinbosa-genesis-bos20`, commit `a734cd2da`
- les scripts de vérification du dépôt : `check-genesis-hash.js`, `check-supply.js`,
  `build-genesis.js`

Les balayages complets de la chaîne (403 419 blocs, temps de bloc et transactions) ont été
exécutés **en lecture seule** contre le nœud RPC, sans aucune écriture ni redémarrage.

Aucune valeur de ce dossier n'a été recopiée d'un document antérieur. Là où un chiffre publié
et la chaîne divergent, c'est la **chaîne** qui a été retenue, et l'écart est signalé (§ 11).
