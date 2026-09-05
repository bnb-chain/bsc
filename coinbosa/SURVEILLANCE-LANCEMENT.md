<div align="center">
  <img src="assets/coinbosa-logo.jpg" alt="Coinbosa" width="90" />

  # Surveillance de la mise en ligne publique

  **BOSA · chainId 26262 · consensus Parlia**
</div>

---

## Ce que ce document est

Le dispositif de surveillance des semaines qui suivent l'ouverture au public : ce qu'on
regarde, à partir de quelle valeur on réveille quelqu'un, qui est réveillé, et ce qu'il fait
dans les cinq premières minutes.

Il est écrit **avant** l'événement parce qu'aucun de ces gestes ne s'improvise à 3 h du matin,
et parce que plusieurs d'entre eux — au premier chef ceux qui touchent à la trésorerie — n'ont
**aucune** réponse technique une fois le fait accompli.

Chaque chiffre porte sa source : un fichier du dépôt, une mesure du dossier de cotation, ou un
relevé de recherche daté. Ce qui n'existe pas est écrit **« à fournir par l'éditeur »** plutôt
qu'inventé.

**Ce document n'est pas** un plan de cotation (voir `DOSSIER-COTATION.md`), ni un plan de
sécurité de l'infrastructure (voir `docs/SECURITY-HARDENING.md`), ni un avis juridique.

---

## Les six contraintes qui commandent tout le reste

Elles ne sont pas des réserves de style. Elles déterminent quels signaux sont observables et
quels gestes sont possibles.

| # | Contrainte, mesurée | Ce qu'elle interdit |
|---|---|---|
| 1 | **Un seul validateur** produit 100 % des blocs depuis le bloc 1 ; **1 pair réseau** (`DOSSIER-COTATION.md` § 9) | Aucun basculement. Le validateur s'arrête, le réseau s'arrête, et **aucune transaction corrective ne peut être minée** |
| 2 | **Une seule machine** porte le validateur, le nœud RPC, l'explorateur et le site, derrière **un seul** Caddy (`DOSSIER-COTATION.md` § 12) | Aucune montée en charge en urgence. Le seul levier disponible en cinq minutes est de **limiter** ou d'**exempter**, jamais d'ajouter de la capacité |
| 3 | **Une seule graine** dérive les 13 adresses de trésorerie **et** le gouverneur du consensus (`DOSSIER-COTATION.md` § 7, `GARDE-TRESORERIE.md` § 2.1) | Aucun cloisonnement. Un secret compromis emporte la monnaie **et** le jeu de validateurs simultanément |
| 4 | **Aucun verrou** : ni multi-signatures, ni séquestre, ni *timelock* sur les 700 000 000 BOSA (`DOSSIER-COTATION.md` § 3) | Aucun gel, aucune annulation. Une sortie de fonds est définitive à la seconde où elle est minée |
| 5 | **Aucune finalité** : `finalized` et `safe` restent au bloc 0, clé BLS à 48 octets nuls (`DOSSIER-COTATION.md` § 9) | Aucun repère de non-réversibilité. Un rembobinage n'est jamais exclu par le protocole |
| 6 | **Aucun audit externe** publié ni engagé ; la couche d'enjeu PoBS est spécifiée, **non déployée** (`DOSSIER-COTATION.md` § 13, `POBS.md`) | Aucun tiers n'a validé le chemin de consensus. Aucun validateur ne risque de fonds, aucune sanction n'est possible |

**Conséquence de méthode :** la surveillance ne peut pas empêcher la plupart de ces événements.
Elle sert à les **voir tôt** et à **dire vrai vite**. C'est la seule chose qui reste quand rien
ne peut être annulé.

---

## Le fait le moins intuitif de tout ce document

**Aujourd'hui, 0 BOSA est détenu hors du projet** (`DOSSIER-COTATION.md` § 3). Tant que c'est
vrai :

- personne d'autre que l'éditeur ne peut émettre une transaction sur la chaîne — la chaîne a
  connu **une seule** transaction utilisateur en 403 419 blocs, et elle vient d'une adresse du
  projet (`DOSSIER-COTATION.md` § 9) ;
- personne ne peut saturer l'espace de bloc, puisque le gaz se paie en BOSA ;
- il n'y a pas de marché à manipuler.

**Le compte à rebours de la surveillance ne démarre donc pas à la mise en ligne du site : il
démarre à la première sortie de fonds vers un tiers**, très probablement depuis le poste
« Distribution publique » `0x47f0c3e1D2c9EA164986c58612CafD39bb89ED41`
(`DOSSIER-COTATION.md` § 7). Ce jour-là, et pas avant, les familles M7 et M8 deviennent réelles.

Avant ce jour, ce qui est réellement exposé au public, c'est **le site, l'explorateur et le
RPC** — donc les familles M1, M2, M8 et M9.

---

## 1 — Ce qu'on surveille, par famille de menace

Pour chaque famille : le **signal observable** qui la trahit avant qu'elle aboutisse, où il se
lit, et s'il est capté aujourd'hui.

### M1 — Saturation de la porte publique

Une seule machine, un seul Caddy, un seul nœud RPC. C'est la surface la plus exposée et la plus
banale.

| Signal observable | Où il se lit | Capté aujourd'hui |
|---|---|---|
| Requêtes `/rpc` par minute et par IP | `/var/log/caddy/explorer-access.log` | **oui** — prisons fail2ban, et préavis dans `72-…-cotation.sh` (sonde § 7) |
| CPU inactif de la machine | `/proc/stat` | **oui** — contrôle G de `72`, et détection de processus glouton (sonde § 8) |
| Latence de `eth_blockNumber` | mesure directe sur `https://explorer.coinbosa.com/rpc` | **oui** — sonde § 1 de `72` (seuil 1 500 ms) |
| **Intervalle entre blocs qui s'écarte de 5 s** | en-têtes de la chaîne | **partiellement** — la stagnation est vue à 60 s, la *dérive* (5,5 s, 7 s) ne l'est pas |
| Connexions TCP simultanées par IP | `nftables`, table `coinbosa-debit` | **oui** — pose de `70-limitation-debit.sh` |

> **Le signal précoce qui manque.** Avant qu'un nœud tombe, il ralentit. Le validateur produit
> à 5,0000 s exactement, min = max, sur 2 400 intervalles mesurés (`DOSSIER-COTATION.md` § 2) :
> une moyenne glissante à **5,3 s** serait donc déjà une anomalie franche, bien avant les 60 s
> de stagnation qui déclenchent l'alerte actuelle. Voir § 2, seuil S4.

### M2 — Auto-déni : bannir soi-même une vague légitime ou un intégrateur

C'est la panne la plus probable des premiers jours, et la plus vicieuse : de l'intérieur tout
est vert, de l'extérieur la chaîne a disparu. Le bannissement est un `reject icmp
port-unreachable`, **jamais un HTTP 429** (`74-allowlist-bourse.sh`, en-tête) : le client ne
reçoit aucune indication qu'il a été limité.

| Signal observable | Où il se lit | Capté aujourd'hui |
|---|---|---|
| Liste des IP bannies, prison `caddy-rpc` | `fail2ban-client status caddy-rpc` | **oui** — sonde § 6 de `72` |
| Liste des IP bannies, prison `caddy-status` | idem | **oui** — sonde § 6 de `72` |
| Liste des IP bannies, prison **`caddy-rpc-rafale`** (300 req/10 s) | idem | **NON** — angle mort, voir § 5 |
| Liste des IP bannies, prison **`recidive`** (bannissement **tous ports, 1 semaine**) | idem | **NON** — angle mort, voir § 5 |
| IP qui s'approche du seuil | journal Caddy, dernière minute | **oui, mais mal calibré** — le préavis est calculé sur 1 500 req/min alors que le seuil réel est 1 200 (§ 5) |

> **Pourquoi c'est aigu ici.** Un onglet d'explorateur consomme **60 requêtes par minute**
> (`explorer/app.js:362`, cycle de 5 s × 5 appels) et **~22 requêtes** au premier affichage
> (`70-limitation-debit.sh`, en-tête). Derrière un CGNAT d'opérateur mobile africain ou un NAT
> d'entreprise, **20 visiteurs simultanés suffisent à atteindre 1 200 req/min** — c'est
> exactement le calcul qui a servi à poser le seuil, et c'est aussi ce qui le rend fragile un
> jour d'affluence.

### M3 — Arrêt de la chaîne

| Signal observable | Où il se lit | Capté aujourd'hui |
|---|---|---|
| Hauteur du validateur figée | IPC du validateur | **oui** — `50-monitoring.sh`, `STAGNATION_MAX=60` |
| Hauteur figée **vue de l'extérieur** | RPC public | **oui, seulement pendant la fenêtre** — sonde § 4 de `72` |
| Service `coinbosa-validator` inactif | `systemctl is-active` | **oui** — `50` |
| Écart entre le validateur et le nœud public | comparaison des deux hauteurs | **oui** — `50`, alerte au-delà de 20 blocs |
| Disque saturé (5 s/bloc, ça grossit tous les jours) | `df` | **oui** — `50`, seuil 85 % |

> **Le piège documenté.** Un ajout de validateurs mal préparé **arrête le réseau au bloc
> d'epoch suivant** : Parlia exige ⌊N/2⌋+1 signataires distincts **et en ligne**, établi par
> test exécuté (`POBS.md` § 5). Toute modification du jeu de validateurs pendant la fenêtre de
> lancement est donc un événement à traiter comme une opération à risque d'arrêt, jamais comme
> un réglage.

### M4 — Rembobinage et divergence

Le risque le plus coûteux vis-à-vis d'un tiers qui crédite des dépôts.

| Signal observable | Où il se lit | Capté aujourd'hui |
|---|---|---|
| Hauteur qui **recule** | comparaison au relevé précédent | **oui** — `50` (« REMBOBINAGE DETECTE »), et sonde § 4 de `72` |
| Empreintes divergentes à hauteur égale entre validateur et nœud | les deux IPC | **oui** — `50` (« FORK : hash divergents ») |
| Ancienneté du dernier arrêt propre | journal du nœud | **indirectement** — `60-journal.sh` la borne à 24 h, mais rien ne l'affiche |

> **La cause connue, chiffrée.** Sous le schéma d'état « path », un arrêt brutal fait repartir
> le nœud **au dernier arrêt propre**. Constaté en production le 19 août : le journal du
> validateur datait de **neuf jours**, soit environ **144 700 blocs exposés**
> (`60-journal.sh`, en-tête). `60-journal.sh` ramène cette borne à une journée. C'est le seul
> mécanisme du dossier dont l'efficacité a été mesurée — et il ne protège que si **personne
> n'exécute jamais `kill -9` sur un geth** (`deploy/README.md`).

### M5 — Compromission de la graine (trésorerie et gouverneur)

**C'est la famille où le dispositif actuel ne voit rien du tout.**

| Signal observable | Où il se lit | Capté aujourd'hui |
|---|---|---|
| Nonce d'une des 13 adresses de trésorerie passe de 0 à 1 | `eth_getTransactionCount` | **NON** |
| Solde d'un des 15 détenteurs connus change | `eth_getBalance` | **NON** |
| Total des 15 détenteurs ≠ 700 000 000 BOSA | `scripts/check-custody.js` | **NON** (script existant, jamais programmé) |
| Solde ou nonce du gouverneur `0x1EEf…082A` change | RPC | **NON** |

**Pourquoi le seuil est « un seul événement ».** Douze des treize adresses n'ont **jamais**
signé (nonce 0), et la chaîne entière compte **une** transaction utilisateur en 403 419 blocs
(`DOSSIER-COTATION.md` § 7 et § 9). Le bruit de fond est nul. **Tout mouvement est donc soit
une opération planifiée de l'éditeur, soit une compromission** — il n'existe pas de troisième
cas, et c'est ce qui rend ce signal exploitable sans aucune tolérance.

> **Prérequis sans lequel ce signal ne sert à rien :** un **registre des opérations de
> trésorerie planifiées**, tenu par l'éditeur, daté, avec l'adresse source, le montant et la
> destination attendue. Sans lui, l'astreinte ne peut pas qualifier un mouvement en cinq
> minutes, et perdra ces cinq minutes à téléphoner. Ce registre n'existe pas — **à fournir par
> l'éditeur.**

### M6 — Prise de contrôle du jeu de validateurs

Le gouverneur est une **constante gravée dans le bytecode du bloc 0** : `0x1EEf…082A`, clé
simple, ni multi-signatures ni délai, et elle **ne peut pas être remplacée** sans changer
l'identité de la chaîne (`POBS.md` § 2, `GARDE-TRESORERIE.md` § 6).

| Signal observable | Où il se lit | Capté aujourd'hui |
|---|---|---|
| `numOfValidators()` ≠ 1 | appel sur `0x…1000` | **NON** |
| `getValidators()[0]` ≠ `0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50` | idem | **NON** |
| Champ `miner` d'un bloc ≠ validateur connu | en-têtes | **NON** |
| Événement émis par le contrat système | `eth_getLogs` sur `0x…1000` | **NON** (`GARDE-TRESORERIE.md` § 8 décrit l'inventaire, rien ne le programme) |

> **Ce qu'un attaquant tenant le gouverneur peut et ne peut pas faire.** Il **ne peut pas**
> exclure le validateur de genèse : `updateValidatorSet` refuse tout ensemble qui ne le
> contient pas (`POBS.md` § 2, verrou 2). En revanche il **peut ajouter** des validateurs
> hors ligne — et par le piège de M3, cela **arrête la chaîne au prochain bloc d'epoch**, tous
> les 200 blocs, soit au plus **16 minutes 40** après l'appel. C'est le scénario d'attaque le
> plus court du projet, et rien ne le surveille aujourd'hui.

### M7 — Saturation de l'espace de bloc

Sans objet tant que 0 BOSA circule hors du projet. Devient réel à la première distribution.

| Signal observable | Où il se lit | Capté aujourd'hui |
|---|---|---|
| `gasUsed` d'un bloc non multiple de 200 devient > 0 | en-têtes | **NON** |
| `gasUsed` soutenu au-delà de 10 % du plafond | en-têtes | **NON** |
| Première création de contrat par un utilisateur (`to: null`) | transactions | **NON** |
| Transactions en attente | `txpool_*` | **IMPOSSIBLE** — la famille `txpool_` n'est pas exposée (`DOSSIER-COTATION.md` § 12) |

> **Angle mort structurel :** le mempool n'est pas observable. Un spam n'est donc détectable
> **qu'après** inclusion, jamais pendant sa montée.
>
> **Coût d'une saturation, calculé — trois valeurs mesurées, une division.** Plafond de gaz
> 55 000 000, prix recommandé 1 gwei, transfert simple 21 000 gaz (`DOSSIER-COTATION.md`
> § 2) : 55 000 000 ÷ 21 000 = **2 619 transferts par bloc**, à 0,000021 BOSA pièce = **0,055
> BOSA par bloc**, soit sur 17 280 blocs = **≈ 950 BOSA par jour** pour tenir la chaîne pleine
> 24 h. Rapporté à l'offre : 0,000136 %. *Le prix plancher réellement accepté par le nœud
> (`--miner.gasprice`) n'est pas publié dans le dépôt : à confirmer par l'éditeur, il change
> ce calcul.*

### M8 — Faux jetons, imitations, hameçonnage

Traité intégralement au § 6.

### M9 — Fraude visant l'éditeur (faux intermédiaires de cotation)

Menace dirigée contre la société, pas contre la chaîne. Elle arrive par messagerie privée dès
que le projet devient visible.

| Signal observable | Capté aujourd'hui |
|---|---|
| Un tiers propose une cotation « garantie » ou « accélérée » hors des canaux officiels | **NON** — aucune procédure |
| Un tiers se présente comme représentant de CoinGecko ou de CoinMarketCap et demande des frais | **NON** |

**Le test qui tranche, en une phrase, sans expertise.** Les deux agrégateurs écrivent que la
cotation est **gratuite** : *« Faire coter votre jeton ou votre place d'échange sur CoinGecko
est gratuit et aucun représentant de CoinGecko ne vous demandera jamais de frais de cotation
sous quelque forme que ce soit »* (page Methodology) ; CoinMarketCap : *« We do not sanction
any external service to assist in the listing application of any cryptoasset project or
exchange »*, avec une « Hall of Shame » nominative et l'avertissement *« If you are scammed by
such services, we will not be in a position to recover your funds »* (article CMC Priority).
Les **seuls** paiements officiels sont les accélérateurs achetés **directement** aux
agrégateurs (Fast Pass, Express Listing, CMC Priority). Tout le reste est une fraude — relevé
de recherche du 2026-09-02.

### M10 — Panne de la surveillance elle-même

| Signal observable | Capté aujourd'hui |
|---|---|
| Sentry refuse les événements | **oui** — `50` journalise « CANAL D ALERTE HORS SERVICE » et le contrôle H de `72` le compte sur 24 h |
| La voie humaine (Telegram / webhook / ntfy) refuse | **oui** — `72` journalise « VOIE HUMAINE HORS SERVICE » |
| **Le serveur est mort et plus aucune sonde ne parle** | **NON** — voir ci-dessous |

> **Le trou le plus grave du dispositif.** Toutes les sondes tournent **sur la machine
> qu'elles surveillent**. Si la machine ou son réseau tombe, **aucune alerte ne part** : le
> silence est indistinguable du bon fonctionnement. Le seul contre-signal existant est le
> **battement quotidien** de `50` et le **résumé toutes les 6 h** de `72` — mais leur
> *absence* ne réveille personne, faute d'un veilleur extérieur. Il faut une sonde **hors du
> serveur** (§ 3).

---

## 2 — Les seuils

Ce qui est **posé** est en service dans les scripts du dépôt. Ce qui est **à poser** n'existe
pas encore et doit être construit ; la valeur proposée est justifiée par une mesure, jamais par
un usage.

### Seuils posés

| Réf | Signal | Seuil | D'où vient la valeur |
|---|---|---|---|
| S1 | Hauteur du validateur figée | **60 s** | 12 blocs manqués à 5 s : au-delà, la chaîne est réellement arrêtée (`50-monitoring.sh`) |
| S2 | Écart nœud public / validateur | **20 blocs** | `50-monitoring.sh` |
| S3 | Hauteur figée vue du RPC public | **60 s** | sonde § 4 de `72` |
| S5 | Latence `eth_blockNumber` mesurée sur la machine | **1 500 ms** | au-delà, c'est un défaut serveur ; un client distant verra pire (`72`, sonde § 1) |
| S6 | `eth_getLogs` sur 5 000 blocs | **8 s** | un index sain répond en 1 s (`50-monitoring.sh`) |
| S7 | Requêtes `/rpc` par IP et par minute | **1 200** → bannissement 30 min | médiane mesurée 2 req/min, p99 89, client légitime le plus bavard **90** ; l'abus constaté tournait à 1 546-1 663. 1 200 = 13 × le client légitime le plus bavard (`70`, 25 h de journal, 363 442 requêtes) |
| S8 | Rafale `/rpc` par IP | **300 en 10 s** | 13 premiers affichages d'explorateur simultanés derrière une même IP (22 requêtes pièce) ; réaction en 10 s au lieu de 60 (`70`) |
| S9 | Récidive | **3 bannissements en 24 h → 1 semaine, tous ports** | une peine de 30 min ne dissuade pas une machine ; l'IP 37.187.89.167 a été bannie 99 fois et revenait (`70`) |
| S10 | Réponses 4xx par IP et par minute | **60 → 1 h** | prison `caddy-status` (`21-fail2ban-web.sh`). **Un `GET /rpc` renvoie 405 et compte** : une sonde de santé mal écrite se fait bannir une heure (`72`, section J) |
| S11 | Connexions TCP simultanées par IP | **64** | 1 connexion HTTP/2 par origine, 6 au plus en HTTP/1.1 : 64 ≈ dix navigateurs derrière la même adresse (`70`) |
| S12 | Occupation disque | **85 %** | `50-monitoring.sh` |
| S13 | Certificat TLS | **21 jours** restants | `50-monitoring.sh` |
| S14 | Processus glouton hors chaîne | **≥ 50 % d'un cœur pendant ≥ 300 s** | un `grep -r /` d'audit oublié a tenu un cœur sur quatre pendant plus de 48 h le 28 août (`72`, sonde § 8) |
| S15 | CPU inactif au contrôle du jour J | **≥ 60 %** | machine saine relevée à 97,3 % le 27 août ; 73,2 % le 30 août avec le processus glouton (`72`, contrôle G) |
| S16 | Mémoire disponible | **≥ 4 000 Mo** | machine à 15 Gio, **sans swap** (`71-isolation.sh`) |

### Seuils à poser

| Réf | Signal | Seuil proposé | Justification |
|---|---|---|---|
| S4 | **Dérive** du temps de bloc | moyenne glissante sur 60 blocs **≥ 5,30 s** | la production mesurée est de **5,0000 s exactement, min = max**, sur 2 400 intervalles (`DOSSIER-COTATION.md` § 2). 5,30 s est 6 % au-dessus d'une valeur qui n'a jamais varié : c'est un signal franc, et il précède l'arrêt au lieu de le constater |
| S17 | **Nonce** d'une des 13 adresses ou du gouverneur | **+1** | 12 des 13 sont à nonce 0 depuis le bloc 1 ; la chaîne compte 1 transaction utilisateur en 403 419 blocs (`DOSSIER-COTATION.md` § 7, § 9) |
| S18 | **Solde** d'un des 15 détenteurs | **tout écart, au wei** | le total tombe exactement sur 700 000 000 BOSA et n'a jamais bougé (`DOSSIER-COTATION.md` § 3) |
| S19 | Jeu de validateurs | **toute modification** de `numOfValidators()` ou de `getValidators()` | 1 producteur unique sur 403 419 blocs (`DOSSIER-COTATION.md` § 9) ; et un ajout mal préparé arrête la chaîne au prochain epoch (`POBS.md` § 5) |
| S20 | Création de contrat par un utilisateur | **la première** | 0 création en 403 419 blocs (`DOSSIER-COTATION.md` § 5) |
| S21 | `gasUsed` d'un bloc non multiple de 200 | **> 0** — niveau information | seuls les blocs d'epoch portent une transaction système, de **693 gaz** ; 99,4995 % des blocs sont vides (`DOSSIER-COTATION.md` § 2, § 9) |
| S22 | `gasUsed` soutenu | **> 5 500 000 sur 12 blocs consécutifs** — niveau alerte | 10 % du plafond de 55 000 000 = 261 transferts par bloc ≈ 52 par seconde, un régime que la chaîne n'a jamais connu |
| S23 | Prisons `caddy-rpc-rafale` et `recidive` | **liste de bannis non vide** | ce sont les deux prisons qui frappent le plus fort (10 s de fenêtre ; 1 semaine tous ports) et les seules que rien ne regarde (§ 5) |
| S24 | Présence de « bosa » chez un agrégateur, sans dépôt de notre part | **la première occurrence** | relevé du 2026-09-02 : `api.coingecko.com/api/v3/search?query=BOSA` rend une liste `coins` **vide**, `asset_platforms` (465 entrées) ne contient aucun `chain_identifier` 26262, et l'API GeckoTerminal `networks` (3 pages) aucun réseau « bosa ». La ligne de base est donc **zéro**, ce qui rend toute apparition non sollicitée immédiatement qualifiable |
| S25 | Absence de battement | **pas de battement quotidien reçu depuis 26 h** | le battement de `50` est quotidien ; 26 h laissent passer une dérive de minuterie sans laisser passer une mort de machine |

**Ce qui n'a délibérément pas de seuil chiffré :** la liquidité, le volume, le nombre de places
d'échange. Les deux agrégateurs écrivent que ces critères sont **non divulgués** — CoinGecko :
*« un ensemble interne de critères… ainsi que plusieurs autres facteurs d'évaluation non
divulgués »* ; CoinMarketCap : *« not simply a matter of ticking off a checklist or hitting
predefined thresholds »*. Tout chiffre publié ailleurs sur ces sujets est inventé ou périmé
(relevé de recherche du 2026-09-02). Le projet ne s'en donne donc aucun.

---

## 3 — Qui est prévenu, comment, sous quel délai

### Les voies qui existent

| Voie | Ce qu'elle porte | Limite connue |
|---|---|---|
| **Journal système** (`journalctl -t coinbosa-watchdog`, `-t coinbosa-cotation`) | tout, toujours | personne ne le lit spontanément |
| **Sentry** | alertes de `50-monitoring.sh` et `60-journal.sh` | **Sentry groupe par empreinte et n'envoie un courriel qu'à la PREMIÈRE occurrence d'un groupe.** « validateur injoignable » ayant déjà été vu le 28 août, une récidive ne réveillerait personne (`72`, en-tête) |
| **Voie humaine poussée** — Telegram, webhook Slack/Discord/Google Chat, ou ntfy | alertes de la sonde de cotation uniquement | **n'est utilisée que pendant la fenêtre armée** (§ 5) |
| **`security.txt`** — `security@coinbosa.com` et avis de sécurité GitHub | signalements venus de l'extérieur | expire le **2027-07-26** (`deploy/static/security.txt`) |

### Ce que le dispositif garantit techniquement

| Étage | Délai | Source |
|---|---|---|
| Passage de la sonde de base | **120 s** en régime normal | `50-monitoring.sh`, minuteur `OnUnitActiveSec=120s` |
| Passage des deux sondes en fenêtre de lancement | **30 s** par défaut, réglable de 10 à 119 s | `72 on --intervalle` |
| Durée maximale d'une fenêtre armée | **30 jours** par activation | `72`, borne `--jours` 1 à 30 |
| Refroidissement d'une alerte **grave** | 5 min | `coinbosa-alerte-humain` |
| Refroidissement d'une alerte **attention** | 30 min | idem |
| Refroidissement d'une alerte **info** | 6 h | idem |
| Mention « PERSISTE DEPUIS N min » | à partir de 120 s | idem |
| Résumé « tout va bien » | toutes les 6 h | `72`, sonde § 9 |
| Battement de cœur | quotidien | `50-monitoring.sh` |

**Garde-fou déjà en place, à conserver :** `72 on` **refuse de s'activer** tant que la voie
humaine n'a pas répondu 2xx, et il rappelle qu'un 2xx prouve que le serveur a pris le message,
**pas** qu'il a atteint le téléphone. La seule preuve est de regarder l'appareil.

### Ce qui manque, et qui ne s'écrit pas dans un script

| Élément | État |
|---|---|
| Nom de l'astreinte, par semaine | **à fournir par l'éditeur** |
| Délai d'accusé de réception engagé pour une alerte grave | **à fournir par l'éditeur** — le dispositif technique permet une détection en 30 s ; il ne dit rien du temps humain |
| Suppléant si l'astreinte ne répond pas | **à fournir par l'éditeur** |
| Règles d'alerte configurées côté Sentry, et surveillance d'un battement manquant | **à fournir par l'éditeur** — le DSN ne figure pas dans le dépôt, l'état de la configuration est inconnu d'ici |
| Porte-parole désigné pour la communication de crise | **à fournir par l'éditeur** |
| **Sonde extérieure** au serveur (S25) | **à construire** — sans elle, la mort de la machine est silencieuse |

### Table d'acheminement proposée

| Gravité | Familles | Voie | Cible |
|---|---|---|---|
| **P0 — réveiller** | M3 arrêt · M4 rembobinage · M5 mouvement de trésorerie · M6 jeu de validateurs · M10 silence de la machine | voie humaine poussée **et** Sentry **et** journal | astreinte, immédiat |
| **P1 — traiter dans l'heure** | M1 saturation · M2 bannissement effectif · M7 spam soutenu | voie humaine poussée, priorité normale | astreinte |
| **P2 — traiter le jour même** | préavis de bannissement · certificat · disque · dérive de temps de bloc | Sentry et journal | opérateur |
| **P3 — traiter dans la semaine** | M8 imitation détectée · M9 sollicitation frauduleuse | courriel `security@coinbosa.com` | éditeur, plus le porte-parole pour M8 |

*Qui fait quoi :* l'**astreinte** exécute les gestes du § 4 ; l'**éditeur** décide seul de tout
ce qui touche à la trésorerie, au gouverneur et à la parole publique ; le **porte-parole**
publie, et lui seul.

---

## 4 — Les gestes des cinq premières minutes

Écrits à l'avance, exécutables tels quels. Toutes les commandes de diagnostic sont en
**lecture seule** sauf mention contraire.

### Réflexe zéro, valable pour toute alerte : est-ce une maintenance ?

```bash
cat /run/coinbosa-maintenance          # témoin de fenêtre de maintenance ; une échéance = arrêt propre en cours
systemctl list-timers coinbosa-journal.timer
```

L'arrêt propre planifié tombe vers **04:17 UTC** (aléa de 300 s), coûte **1 bloc manqué par
jour** et 23 à 74 s d'indisponibilité mesurées (`60-journal.sh` ; `DOSSIER-COTATION.md` § 10 ;
`72`, section J). Une alerte dans cette fenêtre n'est probablement pas un incident.

### M3 — La chaîne n'avance plus

```bash
systemctl is-active coinbosa-validator coinbosa-node caddy
journalctl -u coinbosa-validator -n 50 --no-pager
df -h /                                 # saturation disque : cause fréquente et silencieuse
```

1. **Ne jamais** `kill -9`, `pkill geth`, ni couper la machine — l'arrêt brutal fait repartir
   le nœud au dernier arrêt propre (`deploy/README.md`).
2. Si le service est tombé : `sudo systemctl start coinbosa-validator`, puis vérifier que la
   hauteur repart.
3. Si le disque est plein : libérer, puis redémarrer **par systemd uniquement**.
4. Si le datadir ou la clé de scellage est perdu : `deploy/SAUVEGARDE-CLE.md`, procédure de
   restauration. **Ce qui la débloque :** que la sauvegarde ait été faite **et restaurée au
   moins une fois** (`deploy/repetition-restauration.sh`, chaîne jetable 26999). *Que cette
   répétition ait été exécutée est à confirmer par l'éditeur.*
5. **Qui :** l'astreinte pour 1-3 ; l'éditeur seul pour 4.

### M4 — Rembobinage ou divergence

1. **Geler toute écriture externe** : prévenir tout intégrateur de **suspendre les crédits de
   dépôt**. *Ce qui le débloque :* une liste de contacts d'intégrateurs — **à fournir par
   l'éditeur.**
2. Relever l'ampleur : hauteur actuelle contre dernière hauteur connue
   (`/var/lib/coinbosa-monitoring/derniere-hauteur`).
3. Vérifier que la chaîne servie est toujours la bonne :
   ```bash
   RPC=https://explorer.coinbosa.com/rpc node scripts/check-genesis-hash.js
   ```
4. **Ne rien redémarrer** avant d'avoir noté la hauteur : un redémarrage supplémentaire peut
   aggraver le recul.
5. Rappeler par écrit la règle d'intégration : **ne jamais se fonder sur `finalized` ni `safe`,
   qui resteront au bloc 0** ; utiliser un nombre fixe de confirmations
   (`DOSSIER-COTATION.md` § 9, `docs/INTEGRATION.md`).

### M1 — Saturation

```bash
sudo bash deploy/72-surveillance-cotation.sh controle     # sections F et G : débit par IP, ressources
fail2ban-client status caddy-rpc
```

1. Identifier l'IP dominante de la dernière minute (contrôle F).
2. **Si elle est légitime** (intégrateur, vague d'utilisateurs derrière un NAT) → exempter,
   voir M2.
3. **Si elle est hostile** → ne rien faire : la prison rafale la coupe en 10 s, la récidive en
   une semaine. Resserrer seulement si l'attaque est distribuée :
   ```bash
   SEUIL_MINUTE=600 SEUIL_RAFALE=150 sudo -E bash deploy/70-limitation-debit.sh
   ```
4. **Ce qu'il ne faut pas espérer :** il n'existe **aucun levier de capacité** en cinq minutes.
   Le second nœud et le WebSocket sont écrits et **non déployés** (`deploy/73-node-archive.sh` ;
   `DOSSIER-COTATION.md` § 12), et ils resteraient de toute façon **sur la même machine**.
   La redondance matérielle est un prérequis, pas un geste d'urgence.

### M2 — Un intégrateur ou une vague légitime se fait bannir

```bash
# constat
fail2ban-client status caddy-rpc; fail2ban-client status caddy-rpc-rafale
fail2ban-client status caddy-status; fail2ban-client status recidive

# libération immédiate, prison par prison
for j in caddy-rpc caddy-rpc-rafale caddy-status recidive; do
  fail2ban-client set "$j" unbanip <IP>
done
```

Puis rendre l'exemption durable — **et c'est ici qu'il faut connaître le piège d'ordre de
lecture** :

```bash
# la voie qui tient : elle repose les trois prisons ET les exemptions nftables
IP_BOURSE="<IP1> <IP2>" sudo -E bash deploy/70-limitation-debit.sh
```

`deploy/74-allowlist-bourse.sh` n'exempte que `caddy-rpc` et `caddy-status`, et son fichier
`zz-coinbosa-bourse.conf` est lu **avant** `zz-coinbosa-debit.conf` (ordre alphabétique, la
dernière définition gagne) : une fois `70` installé, l'exemption de `74` sur `caddy-rpc` est
écrasée. Le script s'en aperçoit et **échoue bruyamment** — c'est une bonne conception, pas une
panne. `74` reste utile pour `caddy-status` seul.

Les exemptions posées à chaud (`fail2ban-client set … addignoreip`) **ne survivent pas à un
redémarrage de fail2ban** (`72`, garde-fou n°2) : elles dépannent, elles ne durent pas.

### M5 — Mouvement inattendu sur la trésorerie

```bash
RPC=https://explorer.coinbosa.com/rpc node scripts/check-custody.js
```

1. **Qualifier en une minute** : le mouvement figure-t-il au registre des opérations planifiées ?
   *Ce qui le débloque :* que ce registre existe — **à fournir par l'éditeur.**
2. **S'il n'y figure pas, poser l'hypothèse maximale** : la graine est compromise. Elle dérive
   les 13 adresses **et** le gouverneur (`GARDE-TRESORERIE.md` § 2.1) ; il faut donc supposer
   que le jeu de validateurs est également à la main de l'attaquant.
3. **Il n'y a aucun geste technique de blocage.** Ni multi-signatures, ni séquestre, ni
   *timelock*, ni possibilité de gel : les 13 adresses sont des clés simples, dépensables
   immédiatement et intégralement (`DOSSIER-COTATION.md` § 3). Le déplacement des fonds vers
   des adresses neuves exige de **signer avec la même graine**, ce qui n'a de sens que si le
   compromis est partiel — et rien ne permet de le savoir dans les cinq premières minutes.
4. **Le seul geste utile est donc la parole**, immédiatement : publier le fait, l'heure, les
   adresses et le montant, sur tous les canaux officiels du § 6, **avant** que quiconque le
   découvre sur l'explorateur. **Qui :** le porte-parole, sur décision de l'éditeur.
5. Point juridique : à soumettre à un conseil.

### M6 — Le jeu de validateurs a changé

1. Constater :
   ```bash
   # numOfValidators() et getValidators() sur 0x0000000000000000000000000000000000001000
   RPC=https://explorer.coinbosa.com/rpc node scripts/check-custody.js
   ```
2. **Compter le temps** : le prochain bloc d'epoch tombe au plus tard **200 blocs plus tard,
   soit 16 min 40**. Si les entrants ne scellent pas, la chaîne s'arrête à ce bloc
   (`POBS.md` § 5).
3. Si le changement n'a pas été décidé par l'éditeur → traiter comme M5 : la graine est
   compromise.
4. **Il n'existe pas de geste de reprise en main.** Le gouverneur est une constante du
   bytecode : on ne peut ni le révoquer, ni le remplacer, ni le mettre sous délai
   (`GARDE-TRESORERIE.md` § 6). La seule contre-mesure est de reprendre la main **avec la même
   clé**, si elle n'est pas perdue.

### M7 — Spam transactionnel

1. Relever `gasUsed` sur les 12 derniers blocs et l'origine des transactions.
2. **Ne pas espérer un marché des frais** : `baseFeePerGas` vaut 0 et le prix recommandé est
   fixe à 1 gwei (`DOSSIER-COTATION.md` § 2) — il n'existe aucun mécanisme d'enchère pour
   évincer le spam.
3. Les leviers sont côté nœud (prix plancher accepté, limites de la file de transactions) et
   relèvent d'un changement de configuration du validateur, donc d'un redémarrage :
   **décision de l'éditeur**, jamais un geste d'astreinte.

### M8 — Un faux jeton ou un faux canal apparaît

Voir § 6. Le geste des cinq minutes tient en deux lignes : **publier le démenti sur les canaux
officiels**, et **signaler à la plateforme concernée**. Ne jamais dialoguer publiquement avec
l'imitateur.

---

## 5 — Ce qui existe déjà dans `deploy/`

Ces six scripts sont lus, pas résumés de mémoire. Ce tableau dit ce qu'ils couvrent **et où
s'arrête leur couverture** ; il ne redécrit pas leur fonctionnement, documenté dans leurs
propres en-têtes.

| Script | Couvre | S'arrête à |
|---|---|---|
| `50-monitoring.sh` | vivacité de la chaîne, synchronisation des deux nœuds, services, disque, certificat, index des journaux ; alerte Sentry + journal ; battement quotidien ; toutes les 120 s | ne regarde **ni la trésorerie, ni le jeu de validateurs, ni le contenu des blocs** ; n'utilise **pas** la voie humaine poussée ; ne détecte pas la **dérive** du temps de bloc, seulement l'arrêt |
| `60-journal.sh` | borne la perte d'état à 24 h par un arrêt propre planifié à 04:17 UTC — le seul mécanisme du dossier dont l'efficacité a été **mesurée** | ne protège pas d'un `kill -9` par un opérateur ; l'ancienneté réelle du dernier arrêt propre n'est **affichée nulle part** |
| `70-limitation-debit.sh` | limitation par IP à deux étages (fail2ban 1 200/min et 300/10 s, récidive 1 semaine ; nftables connexions et cadence), sans ouvrir Caddy ; seuils calibrés sur 25 h de journal réel | **n'émet jamais de 429** : un client écarté tombe sans le savoir. Les exemptions ne sont posées qu'**au moment de l'exécution** (`IP_BOURSE`, IP SSH de l'opérateur) |
| `71-isolation.sh` | priorise le validateur sans le borner, borne le nœud public (2 cœurs sur 4, 3 Gio) ; s'applique à chaud, réversible | ne couvre **pas** les entrées-sorties disque (contrôleur `io` non délégué sur cette machine, écarté sciemment) ; ne protège de rien si la machine entière tombe |
| `72-surveillance-cotation.sh` | la seule pièce qui mesure **ce qu'une bourse voit** : latence, cohérence d'identité, stagnation vue de l'extérieur, profondeur d'historique, piège fail2ban, processus glouton ; voie humaine poussée **exigée et prouvée** avant activation ; liste de contrôle exécutable ; mesure depuis l'extérieur | **temporaire par conception** (§ ci-dessous) ; ne surveille ni trésorerie ni validateurs ; deux prisons sur quatre lui échappent ; ses seuils de bannissement sont périmés |
| `74-allowlist-bourse.sh` | exempte des IP d'intégrateur, **et prouve l'exemption** en relisant `fail2ban-client get … ignoreip` plutôt qu'en l'annonçant | ne couvre que `caddy-rpc` et `caddy-status` ; sur `caddy-rpc` il est **écrasé** par `70` (ordre de lecture) ; ne touche ni `caddy-rpc-rafale`, ni `recidive`, ni nftables |

### Les dix manques, par ordre de gravité

1. **Rien ne surveille la trésorerie ni le gouverneur.** C'est le risque que le livre blanc
   lui-même désigne comme *« le plus important du projet »* (`DOSSIER-COTATION.md` § 14), et
   c'est le seul pour lequel aucune sonde n'existe. `scripts/check-custody.js` fait exactement
   ce contrôle, en lecture seule, et **n'est programmé nulle part**. → seuils S17, S18.
2. **Rien ne surveille le jeu de validateurs.** Un attaquant tenant le gouverneur peut arrêter
   la chaîne en 16 min 40 (§ M6) sans qu'aucune sonde ne s'en aperçoive avant l'arrêt. → S19.
3. **Aucune sonde n'est extérieure au serveur.** Machine morte = silence, et le silence
   ressemble au bon fonctionnement. Seule `72 dehors` mesure de l'extérieur, et elle est
   **manuelle**. → S25.
4. **La voie humaine poussée n'est armée que pendant la fenêtre de cotation.** À l'échéance, le
   dispositif **se désarme tout seul** (borne 1 à 30 jours) et `coinbosa-watchdog` retombe sur
   Sentry + journal — c'est-à-dire sur le canal dont l'en-tête de `72` démontre qu'il ne
   réveille personne à la deuxième occurrence d'un même défaut. **La voie la plus fiable est
   celle qui s'éteint la première.** `off` conserve `coinbosa-alerte-humain` et sa
   configuration : plus rien ne les appelle, voilà tout.
5. **Seuil de bannissement périmé dans deux fichiers.** `70` a abaissé `caddy-rpc` de 1 500 à
   **1 200** req/min le 31 août, en vérifiant la valeur réellement appliquée. Mais la sonde § 7
   de `72` calcule son préavis sur **1 500** (`SEUIL=1500 × 60 / 100 = 900`) : le préavis part
   donc à **75 %** du seuil réel au lieu des 60 % annoncés. Le contrôle F affiche de même une
   « marge avant ban » calculée sur 1 500, et l'en-tête de `74` cite `maxretry=1500` comme
   relevé du 30 août. **Trois endroits à corriger.**
6. **Deux prisons sur quatre ne sont surveillées par rien** : `caddy-rpc-rafale` (300 req/10 s)
   et `recidive` (**1 semaine, tous ports, tous protocoles**). Ce sont les plus brutales, et
   celles qu'un intégrateur qui rejoue l'historique déclenchera en premier. → S23.
7. **`74` ne suffit plus une fois `70` posé**, par ordre de lecture de `jail.d/`. La voie
   correcte est `IP_BOURSE=… bash 70-limitation-debit.sh`, qui couvre les trois prisons **et**
   les ensembles nftables. À écrire dans `deploy/README.md`, qui ne documente aujourd'hui ni
   `50`, ni `60`, ni `70`, ni `71`, ni `72`, ni `74`.
8. **Aucune sonde ne regarde le contenu des blocs** : ni `gasUsed`, ni les créations de
   contrat, ni les transferts. → S20, S21, S22.
9. **La dérive du temps de bloc n'est pas détectée**, seulement l'arrêt. Sur une chaîne qui
   produit à 5,0000 s avec min = max, c'est laisser passer le seul signal précoce disponible.
   → S4.
10. **Aucune veille d'imitation.** Aucun script ne regarde les agrégateurs, les domaines
    voisins, ni les canaux sociaux. → S24 et § 6.

**Ce qui a été fait vaut d'être dit :** ces scripts refusent systématiquement le faux vert —
`70` refuse de s'activer si ses filtres ne reconnaissent pas le journal réel, `72` annule son
activation si le *drop-in* systemd n'a pas produit l'effet attendu et exige un 2xx de la voie
humaine, `74` relit la valeur effective au lieu de l'annoncer, `71` refuse d'écrire une borne
mémoire sur le validateur. C'est la bonne discipline, et les manques ci-dessus se comblent dans
le même esprit.

---

## 6 — Faux jetons et imitations

### La phrase qui protège le mieux, parce qu'elle est vérifiable

> **BOSA n'a pas d'adresse de contrat.** C'est le **coin natif** de Coinbosa Chain, comme
> l'ether l'est d'Ethereum. **Aucun jeton BRC20 n'est déployé sur la chaîne à ce jour**, et
> **zéro contrat n'a été créé par un utilisateur** depuis le bloc 1, sur 403 419 blocs balayés
> (`DOSSIER-COTATION.md` § 5 et § 9).
>
> **Toute « adresse de contrat de BOSA » qui circule est donc, par construction, une
> contrefaçon.** Ce test ne demande aucune compétence technique et ne peut pas vieillir : le
> jour où un contrat officiel existera, il sera annoncé sur les canaux ci-dessous, avec son
> adresse, et ce document sera mis à jour.

### Les trois vérités qui tuent les fraudes les plus probables

| Ce qu'on vous propose | Pourquoi c'est faux aujourd'hui |
|---|---|
| « Staking BOSA, N % de rendement » | La couche d'enjeu **n'existe pas** : `eth_getCode` rend **0 octet** aux trois adresses de la pile d'enjeu, `PoBS` est une spécification non déployée (`POBS.md` § 1). Les validateurs ne sont rémunérés que par les frais de transaction, et **sans trafic le revenu est nul, pas faible** (`TOKENOMICS.md`). Le projet ne publie **aucun taux de rendement** |
| « Précommande / liste d'attente de la Coinbosa Card, payez ici » | La carte **n'est pas opérationnelle** et aucun sponsor de BIN n'est engagé à ce jour. Aucun paiement n'est collecté pour elle |
| « Prévente / airdrop officiel de BOSA » | **0 BOSA est détenu hors du projet** (`DOSSIER-COTATION.md` § 3). Toute distribution future sera annoncée sur les canaux ci-dessous, jamais par message privé |

### L'héritage qui doit être réglé avant qu'un tiers ne s'en serve

Le jeton SPL historique sur Solana, mint `8UyvxCoVXoVaftWzp7j9yo2sGL2HnHTFDV4capenyFaf`,
mesuré le 2026-08-30 (`DOSSIER-COTATION.md` § 8) :

- offre **499 999 940,39**, le retrait de circulation annoncé **n'a pas eu lieu** ;
- **4,00 %** — plus de 20 millions d'unités — sont hors du portefeuille désigné comme
  portefeuille projet ;
- **`mintAuthority` et `freezeAuthority` sont actives** : de nouvelles unités peuvent être
  créées à tout moment par le détenteur de cette clé.

C'est simultanément un écart avec le texte publié **et** un vecteur d'imitation : un tiers peut
présenter ce jeton comme « le BOSA » et il ne mentira qu'à moitié. Deux issues, l'une ou
l'autre, **décision de l'éditeur** : retrait de circulation public et vérifiable avec révocation
des deux autorités, ou correction du livre blanc. *Ce qui la débloque :* le contrôle de la clé
d'autorité `3zADMByrBhWTnQETN2gv5Gt7jhQKyyprjLCLVVnv2Pkq` — **à confirmer par l'éditeur.**

De même, l'affirmation *« des jetons avaient aussi été émis sur BNB Chain ; ce jeton n'existe
plus »* est **invérifiable** faute d'adresse publiée (`DOSSIER-COTATION.md` § 8). Publier
l'adresse, ou écrire qu'elle ne peut pas être produite.

### Comment on détecte

| Sonde | Ce qu'elle regarde | Ligne de base mesurée | Fréquence proposée |
|---|---|---|---|
| **Agrégateurs** | `api.coingecko.com/api/v3/search?query=BOSA`, `.../asset_platforms`, `api.geckoterminal.com/api/v2/networks` | **vide / absent** au 2026-09-02 : aucune entrée « bosa », aucun `chain_identifier` 26262 sur 465 plateformes | quotidienne |
| **Chaîne** | toute création de contrat (`to: null`) ; tout BRC20 déployé dont `name()` ou `symbol()` contient « bosa » ou « coinbosa » | **0 création** en 403 419 blocs | à chaque bloc, ou au pire toutes les 5 min |
| **Registre de chaînes** | l'entrée 26262 de `chainid.network/chains.json` (nom, RPC, explorateur, icône) | entrée **présente** ; **l'icône IPFS n'est servie par aucune des trois passerelles testées** (`DOSSIER-COTATION.md` § 4) | hebdomadaire |
| **Domaines voisins** | enregistrements ressemblant à `coinbosa.com`, `explorer.coinbosa.com`, `coinbosa-academy.com` | — | quotidienne — **outil à choisir par l'éditeur** |
| **Canaux sociaux** | comptes se présentant comme officiels | voir la liste ci-dessous | quotidienne |

> **Un signal d'imitation déjà présent, et il vient du dossier lui-même :** l'icône référencée
> par le registre public des chaînes n'est plus servie. Un tiers qui veut le logo de Coinbosa
> ne l'obtiendra pas de la source officielle, et le prendra ailleurs — c'est-à-dire chez
> n'importe qui. Réépingler ce fichier est une mesure anti-imitation, pas une finition.

### Ce qu'on publie, une fois, à un seul endroit

Une page **« Identité officielle »** sur `coinbosa.com`, liée depuis le site et depuis
l'explorateur, contenant exactement ceci — et **rien qui ne soit vérifiable** :

**Ce qui est à nous**

| | |
|---|---|
| Éditeur | coinbosa, Inc., Delaware, États-Unis |
| Site | `https://coinbosa.com` |
| Explorateur | `https://explorer.coinbosa.com` |
| RPC public | `https://explorer.coinbosa.com/rpc` |
| Dépôt | `https://github.com/Coinbosa/coinbosa-chain` |
| Canal Telegram | `https://t.me/coinbosa` |
| Groupe Telegram | `https://t.me/Coinbosaofficial` |
| Facebook | `https://www.facebook.com/coinbosa` |
| Academy | `https://coinbosa-academy.com` |
| Signalement de faille | `security@coinbosa.com` et les avis de sécurité du dépôt |
| chainId | `26262` (`0x6696`) |
| Empreinte du bloc 0 | `0x8dcdadc247a98f33728cae944e20ce7c49c74b35cfba31495f85e98979018da6` |
| Les 15 adresses de détention | telles que publiées en `DOSSIER-COTATION.md` § 7 |

*(Liens relevés dans `site/app.js`, objet `CONTENT.links`, et `deploy/static/security.txt`.)*

**Ce qui n'est pas à nous — la moitié qui compte**

- **Le compte X officiel est `@coinbosa6476`** (« Coinbosa Group »), confirmé par l'éditeur le
  2026-09-04 et déclaré depuis dans `coinbosa.config.json`, `site/app.js` et `explorer/app.js` —
  les trois portaient un champ `twitter` vide jusque-là.
- **`@coinbosacrypto` N'EST PAS le compte du projet.** Il existe, il répond
  `Coinbosa (@coinbosacrypto) / X`, il porte donc le nom du projet — mais **l'éditeur en a perdu
  les accès** (confirmé le 2026-09-04). C'est un compte au nom de Coinbosa qui échappe au projet :
  exactement la situation qu'un imitateur exploite, et elle existe déjà.
  **Aggravant :** l'organisation GitHub du projet le déclare encore
  (`api.github.com/orgs/Coinbosa` → `twitter_username: coinbosacrypto`). Notre propre page sert
  donc d'aval public à un compte hors de notre contrôle. À corriger par l'éditeur — c'est le
  geste le plus urgent de cette section.
- Tout autre compte X se présentant comme Coinbosa est une imitation.
- **Le canal Telegram officiel est `t.me/Coinbosaofficial`, et lui seul.** L'éditeur a confirmé
  le 2026-09-04 que ce groupe est le sien. Deux autres adresses étaient déclarées dans la
  configuration et ont été retirées : `t.me/coinbosa`, qui est un compte **personnel** et non un
  groupe, et `t.me/coinbosagroup`, dont le nom **n'était pas réservé** — le publier le
  légitimait pour qui l'aurait pris.
- **Le bot d'alerte est `@Coinbosa_bot`** (nom rendu par l'API `getMe`, vérifié le 2026-09-04),
  et non `coinbosa_officiel_bot` comme annoncé d'abord. Déclarer officiel un compte qui n'existe
  pas est précisément ce que cette page existe pour éviter.
- **Aucun serveur Discord** : le champ est vide. Tout serveur Discord se présentant comme
  Coinbosa est une imitation, aujourd'hui, sans exception.
- **Aucune adresse de contrat pour BOSA**, sur aucune chaîne.
- **Aucune vente, aucune prévente, aucun airdrop, aucun programme de staking.**
- **Aucune Coinbosa Card en service**, donc aucun paiement collecté pour elle.
- **Aucun intermédiaire mandaté** pour obtenir une cotation. La cotation est gratuite chez les
  deux agrégateurs.

Cette seconde liste est la plus efficace des deux : une fraude s'installe presque toujours dans
un espace que le projet n'a **jamais démenti**.

**Qui :** le porte-parole publie ; l'éditeur valide. **Ce qui le débloque :** rien — cette page
peut être écrite aujourd'hui, sans transaction, sans déploiement, sans décision de trésorerie.
C'est la mesure la moins chère et la plus tôt disponible de tout ce document.

### Deux réserves sur les leviers d'agrégateur, à ne pas trancher ici

- Le ticker « BOSA » est **libre** chez CoinGecko au 2026-09-02 (recherche vide), et le
  **conflit de nom ou de ticker figure parmi les motifs de rejet publiés**. Prendre l'identité
  tôt est donc défensif. Mais **la candidature d'un actif sans marché est irrecevable en
  l'état** — CoinGecko exige *« au moins une place d'échange active intégrée »*, CoinMarketCap
  *« actively traded on at least one (1) exchange (with material volume) »*. Les paliers
  d'attente existent (Preview Listing chez CoinGecko, Untracked chez CoinMarketCap) ; pour
  CoinMarketCap, **l'éligibilité d'un actif sans aucun marché à ce palier est déduite du texte,
  jamais écrite** — à poser comme question dans le ticket, pas à considérer comme acquis
  (relevé de recherche du 2026-09-02, incertitude nommée).
- La procédure de vérification publique de CoinGecko impose un message depuis **un compte
  social officiel lié directement au site** — X, Facebook ou Instagram. Le projet n'ayant ni X
  ni Instagram, **c'est la page Facebook qui portera cette preuve d'identité**. Elle devient de
  ce fait un actif de sécurité : sa compromission compromettrait la preuve elle-même.

---

## 7 — Ce qui reste à fournir par l'éditeur

Rassemblé ici pour qu'aucune de ces lignes ne se perde dans le corps du document.

| # | Élément | Bloque quoi |
|---|---|---|
| 1 | Nom de l'astreinte, du suppléant, du porte-parole ; délai d'accusé engagé | tout le § 3 |
| 2 | **Registre des opérations de trésorerie planifiées** | la qualification d'un mouvement en cinq minutes (§ 4, M5) |
| 3 | État réel des règles d'alerte Sentry, et surveillance d'un battement manquant | l'acheminement P0 |
| 4 | Confirmation que `70`, `71`, `72` et `74` sont **effectivement installés** sur le serveur | tout le § 5 — le dossier du 30 août ne relevait que cinq unités systemd, et ces scripts sont datés du 31 |
| 5 | Confirmation que la sauvegarde de la clé de scellage a été faite **et restaurée au moins une fois** | le geste 4 de M3 |
| 6 | Prix de gaz plancher réellement accepté par le nœud | le calcul du coût d'une saturation (§ M7) |
| 7 | Contrôle de la clé d'autorité du jeton SPL Solana | l'issue de l'héritage Solana (§ 6) |
| 8 | Adresse du jeton historique sur BNB Chain, ou constat qu'elle est introuvable | la même |
| 9 | Réépinglage de l'icône du registre de chaînes | la source officielle du logo (§ 6) |
| 10 | Liste de contacts des intégrateurs, pour la suspension des crédits | le geste 1 de M4 |

---

## Sources

**Dépôt** — `DOSSIER-COTATION.md` (mesures du 2026-08-30, bloc de référence 403 466) ·
`GARDE-TRESORERIE.md` · `POBS.md` · `TOKENOMICS.md` · `ROADMAP.md` · `README.md` ·
`docs/INTEGRATION.md` · `site/app.js` · `explorer/app.js` · `deploy/README.md` ·
`deploy/static/security.txt` · `deploy/21-fail2ban-web.sh` · `deploy/50-monitoring.sh` ·
`deploy/60-journal.sh` · `deploy/70-limitation-debit.sh` · `deploy/71-isolation.sh` ·
`deploy/72-surveillance-cotation.sh` · `deploy/73-node-archive.sh` ·
`deploy/74-allowlist-bourse.sh` · `deploy/SAUVEGARDE-CLE.md` ·
`deploy/repetition-restauration.sh` · `scripts/check-custody.js` ·
`scripts/check-genesis-hash.js` · `scripts/check-exchange-rpc.js`

**Agrégateurs, relevés du 2026-09-02** — CoinGecko : page Methodology (section *Listing
Criteria*), articles « Why is my token not listed on CoinGecko? », « Verification Guide for
Listing Update Requests », « How to Preview List Tokens », « How to Request a New Chain Listing
(Asset Platform) ». CoinMarketCap : « Listings Criteria » (maj 2026-09-01), « Supply
(Circulating, Total, Max) », « CMC Priority (CMCP) ». Relevés d'API :
`api.coingecko.com/api/v3/search?query=BOSA`, `api.coingecko.com/api/v3/asset_platforms`,
`api.geckoterminal.com/api/v2/networks`, `chainid.network/chains.json`.

**Ce document ne contient** aucune projection de prix, aucune promesse de rendement, aucun seuil
de liquidité chiffré, et aucun avis juridique ou fiscal.
