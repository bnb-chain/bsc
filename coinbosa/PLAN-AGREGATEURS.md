<div align="center">
  <img src="assets/coinbosa-logo.jpg" alt="Coinbosa" width="90" />

  # Plan de référencement — CoinGecko et CoinMarketCap

  **BOSA · Coinbosa Chain · chainId 26262**
</div>

---

## Comment lire ce document

Ce document est la procédure pas à pas pour faire référencer **le coin BOSA** et **la chaîne
Coinbosa** sur CoinGecko et CoinMarketCap. Il est écrit pour être remis tel quel à un tiers :
une place d'échange, un agrégateur, un partenaire.

**Conventions.**

| Marque | Signification |
|---|---|
| **fait** | vérifié, avec la source à côté |
| **à faire** | rien ne l'empêche, ce n'est pas fait |
| **bloqué** | dépend d'un préalable nommé dans la ligne |
| *à fournir par l'éditeur* | l'information manque au dépôt ; elle n'est pas inventée ici |

**Dates de vérification.** Les valeurs relevées par moi le sont le **2026-09-03** (registre de
chaînes, API de recherche CoinGecko, API des plateformes CoinGecko, API des formulaires
CoinMarketCap). Les relevés d'agrégateurs datés du **2026-09-02** proviennent du travail de
recherche joint et sont signalés comme tels. Les mesures on-chain proviennent de
`DOSSIER-COTATION.md` (bloc de référence 403 466, 2026-08-30) et de `GARDE-TRESORERIE.md`.

**Ce que ce document ne fait pas.** Aucune projection de prix, aucun rendement, aucun seuil de
liquidité chiffré — les deux agrégateurs écrivent explicitement que leurs seuils ne sont pas
publiés, tout chiffre serait fabriqué. Aucune analyse juridique : les points de droit sont
signalés **« point juridique : à soumettre à un conseil »** et laissés là.

---

## 0. Le fait qui commande tout le plan

Les deux agrégateurs posent la même condition d'entrée, dans des termes différents.

| | Texte de la source | État de BOSA |
|---|---|---|
| **CoinGecko** | « Listed on at least one (1) active exchange where CoinGecko is integrated with » — page *Methodology*, section *Listing Criteria*. Et : « Projects traded only on self-serviceable centralized/decentralized exchanges may be rejected » | **aucune place ne cote BOSA** (`DOSSIER-COTATION.md` § 3 et § 16) |
| **CoinMarketCap** | « Must be traded publicly, and actively traded on at least one (1) exchange (with material volume) that has tracked listing status on CoinMarketCap » — *Listings Criteria* § B1, maj 2026-09-01 | idem |

**Conséquence, à écrire avant toute autre chose : la demande de cotation ordinaire est
irrecevable des deux côtés aujourd'hui.** Ce n'est pas un défaut de rédaction du dossier, et
aucune qualité de dossier n'y supplée. L'ordre est imposé par les agrégateurs eux-mêmes :

```
marché réel  →  cotation  →  offre en circulation vérifiée
```

CoinGecko l'écrit dans son guide du formulaire d'offre (maj 2026-07-03) : la mise à jour
d'offre exige que le jeton soit « déjà officiellement coté », et l'article des motifs de rejet
d'une mise à jour d'offre ajoute qu'elle est impossible « tant que le jeton n'est pas actif et
échangé, en l'absence de données de prix ».

**Deux demandes distinctes, pas une.** Le coin BOSA et la chaîne Coinbosa se demandent
séparément, par des canaux différents, et les deux agrégateurs ne les traitent pas au même
guichet. Le détail est aux sections 2 et 3.

**Deux portes restent ouvertes sans marché** — et seulement deux : le *Preview Listing* de
CoinGecko (§ 2.4) et le palier *Preview / Untracked* de CoinMarketCap (§ 3.3). Aucune des deux
ne donne de prix, de rang ni de capitalisation.

---

## 1. Ce qui doit exister avant de soumettre

### A. Le socle public

| # | Élément | Exigé par | État au 2026-09-03 | Débloqué par |
|---|---|---|---|---|
| A1 | Site fonctionnel décrivant l'objet du projet | CG *Listing Criteria* (1) ; CMC B1(2) | **fait** — `coinbosa.com` HTTP 200 (`DOSSIER` § 4) | — |
| A2 | Information sur **l'équipe** sur le site | CG (1) : « les sites sans information sur l'objet, l'équipe ou les profils sociaux seront considérés comme invalides » | **à faire** — `site/a-propos.html` ne nomme aucun membre d'équipe (vérifié dans le dépôt) | éditeur : décider ce qui est publié ; *identités à fournir par l'éditeur* |
| A3 | Site possédé par l'équipe, pas un constructeur de site | CG (2) : « les sites hébergés sur des constructeurs de sites (par ex. Wix) ne seront pas acceptés » | **fait** — site servi par le Caddy du projet sur son propre serveur (`DOSSIER` § 12) | — |
| A4 | Profils sociaux officiels **liés depuis le site** | CG (1) ; procédure de vérification publique | **à faire** — `site/index.html` ne référence que Telegram et GitHub ; le Facebook officiel (`facebook.com/coinbosa`, dans `coinbosa.config.json`) n'y figure pas | équipe front-end |
| A5 | Compte **X officiel du réseau** | GeckoTerminal, *Network Addition* : champ obligatoire | **bloqué** — `"twitter": ""` dans `coinbosa.config.json` **et** dans `explorer/app.js` | éditeur : ouvrir le compte. Sans lui, l'ajout de réseau GeckoTerminal ne peut pas être soumis |
| A6 | Liens identiques partout | CG, guide de vérification : « assurez-vous que votre site officiel et votre documentation reflètent la même information que celle incluse dans votre demande » | **à corriger** — le Telegram communautaire diverge : `t.me/coinbosagroup` dans `coinbosa.config.json`, `t.me/Coinbosaofficial` dans `explorer/app.js`, sur le site et dans le livre blanc publié | équipe front-end + éditeur : une seule valeur, propagée |
| A7 | Explorateur de blocs fonctionnel | CG (3) « Working block explorer » ; CMC B1(2) | **fait** — `explorer.coinbosa.com` HTTP 200 | — |
| A8 | Logo haute résolution servi par une URL stable | CG, demande de chaîne (champ *Logo*) ; GT (*Chain Logo*, lien Imgur) | **à vérifier** — l'icône publiée chez `ethereum-lists/chains` (`ipfs://bafkrei…usxb4a`) n'a été servie par **aucune** des trois passerelles testées (`DOSSIER` § 4) | équipe infra : ré-épingler, ou servir le logo depuis `coinbosa.com` |
| A9 | Documentation / livre blanc public | CG, demande de chaîne (champ *Docs*) | **fait** — `coinbosa.com/whitepaper/` HTTP 200 | — |
| A10 | **Exactitude du livre blanc publié** | CMC : « Be truthful. False or misleading claims may render your submission inadmissible » ; CG *Terms & Conditions for Listing* (12 août 2025) : garantie d'exactitude + dépublication discrétionnaire | **bloqué** — `whitepaper/index.html` ligne 159 et `site/ecosysteme.html` écrivent que la Coinbosa Card « **est déployée à l'échelle mondiale sans limitation régionale** », au présent, alors que la carte n'est pas opérationnelle et qu'aucun BIN sponsor n'existe à ce jour | éditeur : réécrire au futur conditionnel, ou retirer la section, **avant** toute soumission |
| A11 | État réel du consensus visible publiquement | CG (1), information suffisante ; CMC, cohérence du dossier | **à faire** — les mots « preuve d'autorité » n'apparaissent **nulle part** dans `site/index.html` ni `site/chaine.html`, alors que `README.md` et `DOSSIER-COTATION.md` § 9 l'écrivent sans détour | équipe front-end |
| A12 | Un seul chiffre de temps de bloc | cohérence demande / site / docs | **à trancher** — le site affiche « 5,018 s », `DOSSIER` § 2 mesure **5,0000 s** sur 2 400 intervalles et **5,0025 s** sur toute la chaîne | éditeur : retenir une valeur et sa méthode, partout |
| A13 | Fiche du réseau sur Chainlist | CG, demande de chaîne (champ *ChainList URL*) | **fait** — vérifié le 2026-09-03 sur `chainid.network/chains.json` (2 745 chaînes) : entrée 26262, `nativeCurrency {Coinbosa, BOSA, 18}`, RPC et explorateur déclarés | réserve : `explorers[0].standard = "none"` — l'explorateur ne déclare pas la conformité EIP-3091 |

### B. Le marché

| # | Élément | Exigé par | État au 2026-09-03 | Débloqué par |
|---|---|---|---|---|
| B1 | Au moins une place d'échange intégrée qui cote BOSA | CG *Methodology* ; CMC B1(3) | **bloqué** — aucun marché (`DOSSIER` § 3) | § 0 : c'est le préalable de tout |
| B2 | Un DEX en fonctionnement sur la chaîne | GeckoTerminal ; DEXScan côté CMC | **bloqué** — **zéro création de contrat par un utilisateur** sur les 403 419 blocs balayés (`DOSSIER` § 5 et § 9) | équipe chaîne : déployer un DEX. Un fork Uniswap V2 évite la voie « adaptateur » (§ 2.5) |
| B3 | Un stablecoin sur la chaîne, et une pool BOSA/stable approvisionnée | GT, champ *Stable Pool Address* (facultatif dans le formulaire courant, obligatoire dans la version antérieure) | **bloqué** — **aucun jeton BRC20 n'est déployé** (`DOSSIER` § 5) | éditeur : décider qui émet ce stablecoin et sur quel adossement. **Point juridique : à soumettre à un conseil** |
| B4 | Liquidité de la pool **verrouillée** | CG, motif de rejet « rug-pull », visant nommément « une liquidité non verrouillée » | sans objet tant qu'aucune pool n'existe — à prévoir **dès** la première | équipe chaîne, au moment du déploiement du DEX |
| B5 | Nom et ticker non conflictuels | CG, motif « usurpation d'identité ou conflit de nom/ticker » | **favorable** — `api.coingecko.com/api/v3/search?query=BOSA` renvoie `"coins":[]` (vérifié le 2026-09-03 ; appel témoin `query=bitcoin` renvoie bien des résultats). Côté CMC : **non vérifié** | actif à ne pas gaspiller : une demande mal formée peut créer une fiche parasite |

### C. L'offre et la garde

| # | Élément | Exigé par | État au 2026-09-03 | Débloqué par |
|---|---|---|---|---|
| C1 | Tokenomique complète et publiée | CG, motifs de rejet d'une mise à jour d'offre ; CMC Annex C | **fait** — `TOKENOMICS.md` et `DOSSIER` § 7 : 13 postes, 13 adresses, soldes mesurés | — |
| C2 | Liste des portefeuilles équipe / trésorerie / verrouillés | CG (4) ; CMC Annex C, une ligne par adresse | **fait** — les 13 adresses + le gouverneur sont publiés | conséquence : **toutes** sont « insider » au sens des deux méthodes (§ 4) |
| C3 | Calendrier de déblocage (CMC Annex M) | CMC : « New coin applications should strive to submit Annex M and C » | **n'existe pas** — « aucun calendrier de déblocage, aucun contrat de séquestre, aucun *timelock* » (`DOSSIER` § 3 ; `GARDE-TRESORERIE.md` § 4) | rempli honnêtement aujourd'hui, l'Annex M tient en **une ligne `cliff` au genesis pour 700 000 000 BOSA**, soit 100 % débloqué |
| C4 | Preuve de contrôle des adresses | CG, motif « rôle du soumissionnaire manquant » ; CMC, représentant joignable | **à faire** — 12 des 13 adresses ont un nonce de 0, aucune n'a jamais signé (`DOSSIER` § 15) | garde de trésorerie : une signature de message datée par adresse. Ne déplace aucun wei |
| C5 | Point d'accès API *total supply* (JSON numérique) | CG, spécification du point d'accès REST ; CMC Annex C ligne 7 | **à construire** — aucune trace dans le dépôt | équipe chaîne (§ 4.3) |
| C6 | Point d'accès API *circulating supply* | CG ; CMC Annex C ligne 8 | **à construire** | équipe chaîne |
| C7 | Page *richlist* | CMC Annex C ligne 10 | **à construire** — l'explorateur n'indexe rien (`ROADMAP.md`, jalon 5) | équipe chaîne |
| C8 | Garde multi-signatures | CG, motif « rug-pull » ; risque de dossier | **décidée après cotation**, avec cinq motifs vérifiés (`GARDE-TRESORERIE.md` § 6 : aucun Safe déployé sur la chaîne, code de création 23 620 o contre un plafond de corps de requête de 32 Ko, chainId 26262 absent des 53 réseaux de `safe-config.safe.global`, 12 clés jamais utilisées, aucune transaction corrective possible à un seul validateur) | à **déclarer** dans la demande, avec ses motifs, plutôt qu'à taire |
| C9 | Sort du jeton SPL Solana homonyme | CMC Annex C : agrégation « sur toutes les chaînes si l'actif est multi-chaînes » ; CG, dérivation de l'offre depuis les divulgations de l'équipe | **non résolu** — mint `8Uyvx…yFaf` vivant, offre 499 999 940,39, `mintAuthority` et `freezeAuthority` **actives**, 4,00 % hors du portefeuille projet (`DOSSIER` § 8) | éditeur : soit le retrait public et vérifiable, soit la correction du texte publié. Dans les deux cas, expliquer les 4,00 % |

### D. L'infrastructure que l'intégration suppose

| # | Élément | Exigé par | État au 2026-09-03 | Débloqué par |
|---|---|---|---|---|
| D1 | Point d'accès RPC public | CG (champ *RPC*, facultatif, « https uniquement ») | **fait** — `https://explorer.coinbosa.com/rpc` | — |
| D2 | Second point d'accès indépendant | pratique des places d'échange (`DOSSIER` § 12) | **à faire** — un seul serveur porte la chaîne, le RPC, l'explorateur et le site | équipe infra |
| D3 | Nœud d'archive et WebSocket | lecture de l'offre historique ; indexation tierce | **à faire** — `deploy/73-node-archive.sh` est **écrit et non appliqué** (vérifié sur le serveur : ni unité, ni datadir, ni port) | équipe infra : une exécution de script |
| D4 | API de l'explorateur lisible par l'agrégateur | CG *Methodology* : « le solde des adresses verrouillées est obtenu automatiquement depuis l'explorateur de blocs dès qu'une API est disponible » | **à faire** | équipe chaîne, avec C5–C7 |
| D5 | Avertissement `finalized` / `safe` dans le dossier remis | rien ne l'exige — c'est une prévention d'incident | **à écrire** — les deux étiquettes restent au bloc 0 à vie (`DOSSIER` § 9) | rédacteur du dossier |

---

## 2. La soumission CoinGecko

### 2.1 Deux guichets, à ne pas confondre

| Objet de la demande | Canal | Compte requis |
|---|---|---|
| **Le coin BOSA** | formulaire self-service de la Partners Platform, `partner.coingecko.com/request-form/new`, rubrique *Request & Listing* > *New Coin/Token Listing* | oui, compte CoinGecko |
| **La chaîne (asset platform)** | **ticket de support générique**, `support.coingecko.com/hc/en-us/requests/new` — l'article officiel « How to Request a New Chain Listing (Asset Platform) » (maj 2025-12-12) impose ce canal, **pas** le formulaire | non |
| **Le réseau sur GeckoTerminal** | formulaire lié depuis `about.geckoterminal.com/dex-chain-listing` | non |

Le *Support Directory* officiel confirme cette répartition ligne par ligne.

### 2.2 La demande de chaîne — les sept informations exigées

L'article officiel les énumère textuellement. Voici ce que Coinbosa peut y mettre aujourd'hui.

| Champ demandé | Valeur Coinbosa | État |
|---|---|---|
| **Name** | Coinbosa Chain | prêt |
| **Website URL** | `https://coinbosa.com` | prêt |
| **Explorer** | `https://explorer.coinbosa.com` | prêt — réserve A13 (`standard: none`) |
| **Docs** | `https://coinbosa.com/whitepaper/` | **conditionné à A10** (Coinbosa Card au présent) |
| **ChainList URL** | fiche du réseau sur Chainlist.org, alimentée par `chainid.network/chains.json` — entrée 26262 vérifiée le 2026-09-03 | prêt |
| **Logo** | image haute résolution, « si différent du logo du jeton » | **conditionné à A8** |
| **Current Token Listing** | liens vers les jetons déjà cotés sur CoinGecko vivant sur la chaîne, « le cas échéant » | **aucun** — zéro contrat déployé |

Après soumission, « l'équipe opérations de CoinGecko examine les détails de la chaîne ; si la
candidature satisfait nos critères de cotation, la chaîne/asset platform sera intégrée ».
**L'article ne publie ni délai, ni liste de critères pour ce type de demande** — aucune date ne
doit donc être annoncée en interne sur cette étape.

### 2.3 La demande de coin — l'ordre des étapes

La procédure de vérification publique est **obligatoire depuis 2026** (article maj 2026-08-16)
et se fait **avant** le formulaire. Quatre étapes, dans cet ordre :

1. **Publier un message public** depuis un compte social officiel **lié directement au site**
   (X, Facebook ou Instagram) annonçant l'intention de soumettre une demande à CoinGecko, avec
   l'URL GeckoTerminal du projet s'il y est listé, et en option un identifiant Telegram.
   → *Pour Coinbosa* : le compte X n'existe pas (A5) ; **Facebook est admis par le texte**, mais
   il doit être lié depuis le site (A4). Les deux corrections sont préalables à cette étape.
2. **Soumettre le formulaire**, en collant le lien du message dans le champ *Public Verification
   Link* (à défaut, dans *Additional Information*).
3. **À réception de l'identifiant** (`CLXXXXX` pour une cotation, `CUXXXXX` pour une mise à
   jour), **répondre à son propre message public** avec cet identifiant.
4. CoinGecko vérifie le formulaire, le message, la présence exacte de l'identifiant dans la
   réponse, et l'exactitude des données.

**Coût et délais.** La cotation est **gratuite** — *Methodology* : « aucun représentant de
CoinGecko ne vous demandera jamais de frais de cotation sous quelque forme que ce soit ». Le
formulaire propose *Regular Pass* (« jusqu'à 5 jours ») ou *Fast Pass* (« examen accéléré sous
24 heures », **1 000 USD** par demande de cotation de coin, non remboursable, sans garantie
d'approbation). L'article dédié aux délais (maj 2026-06-05) donne « typiquement 3 à 5 jours
ouvrés », refuse tout engagement, et pose la règle pratique : « si votre jeton n'est pas coté
après 2 semaines, il est probable que le projet n'a pas passé notre évaluation ».

**Ce que l'éditeur signe en soumettant** (*Terms & Conditions for Listing*, 12 août 2025,
Gecko Labs Pte. Ltd.) : CoinGecko décide « à sa discrétion », « vous ne serez pas informé » d'un
refus, l'entreprise « n'est pas tenue de fournir de motif » ; le déclarant garantit que toute
information est « vraie, exacte, complète, à jour […] et non trompeuse à quelque égard que ce
soit », **avec obligation d'indemnisation** ; et CoinGecko « se réserve le droit de publier ou
dépublier tout cryptoactif […] sans préavis » si une information est jugée inexacte. C'est ce
qui transforme le point A10 en préalable contractuel et non en question de style.
**Point juridique : à soumettre à un conseil** — la signature de ces conditions par
coinbosa, Inc. et le régime de l'indemnisation.

### 2.4 Le *Preview Listing* — la seule porte ouverte sans marché

Condition d'éligibilité : « une date et une heure de TGE claires, appuyées par des annonces
officielles vérifiables ». Quatre blocs à remplir :

| Bloc | Contenu | Ce que Coinbosa peut fournir |
|---|---|---|
| Liens de vérification | annonces officielles confirmant date et heure de TGE + preuve que la demande émane de l'équipe | *à fournir par l'éditeur* — aucune date de TGE n'est arrêtée dans le dépôt |
| Places d'échange à la date de TGE | détails et annonces ; « s'il n'y a pas encore de place confirmée, sélectionnez simplement une place comme substitut et utilisez une URL générique » | aucune place confirmée — la voie du substitut est explicitement prévue par CoinGecko |
| Date et heure de TGE | dans la section *Coin/Token Supply Information* ; case « Yet to be Confirmed » si non arrêtée | à cocher tant que la date n'est pas décidée |
| *Additional Information* | mention explicite **« PREVIEW LISTING »** | rédaction |

Une fois accordée, **la page ne suit pas le prix** tant que le jeton n'est pas activé à la date
de TGE, par la procédure d'activation dédiée.

> **Décision qui appartient à l'éditeur, pas à ce document.** Annoncer publiquement une date de
> TGE est un engagement public, et le préalable de cette voie. **Point juridique : à soumettre à
> un conseil.**

### 2.5 GeckoTerminal — la voie réaliste pour un coin natif sans CEX

**Champs de la demande *Network Addition***, tous obligatoires sauf mention : adresse e-mail,
identifiant Telegram, *Network Name*, *Explorer URL*, *Chain Logo* (lien Imgur, fond
transparent de préférence), *Native token* (ticker), **Network Twitter/X URL**, question
EVM / non-EVM. L'URL RPC est facultative, « format https uniquement, wss non acceptable ».

→ *Pour Coinbosa* : tout est disponible **sauf le compte X (A5)**, et le RPC déclaré serait
unique et sans redondance (D2).

**Le point dur, c'est le DEX.** Le formulaire demande si le DEX est un fork de l'une des ~30
implémentations supportées (Uniswap V2/V3/V4, Solidly, Curve, Balancer, Algebra, Camelot V3,
Kyberswap, Quickswap V3, Maverick V2, Traderjoe V2…), avec une option « Not a Fork ».

| Voie | Ce qu'elle impose | Recommandation |
|---|---|---|
| **Fork supporté** (ex. Uniswap V2) | déploiement du DEX, rien de plus côté indexation | **à privilégier** — évite entièrement le chantier ci-dessous |
| **« Not a Fork »** | déployer **et maintenir** une API conforme aux *GeckoTerminal Integration API Standards* (v0.1, nov. 2024) : `GET /latest-block`, `/asset?id=`, `/pair?id=`, `/events?fromBlock=&toBlock=` (bornes incluses) ; l'indexeur sonde `/events` **toutes les 2 secondes** ; l'indexation « s'arrête si les schémas sont invalides ou contiennent des valeurs inattendues (par ex. `swapEvent.priceNative=0` ou `pair.name=""`) » ; pools à 2 jetons seulement ; agrégateurs non supportés ; champ *DEX Adapter Base URL* alors obligatoire | à éviter |

**Pool de référence.** La version antérieure du formulaire rendait obligatoire le champ *Stable
Pool Address* — « une paire Natif/Stable déployée et active sur un DEX du réseau » ; le
formulaire courant le conserve **en facultatif**. Traduction pour Coinbosa : il faut un DEX
vivant **et** au moins une pool BOSA/stablecoin réellement approvisionnée, donc un stablecoin
sur la chaîne, qui n'existe pas (B3).

**Délais et tarifs — les sources officielles se contredisent, et ce plan ne tranche pas :**

| Source | Délai annoncé | Tarif |
|---|---|---|
| Article « How do I know if my DEX/Chain gets listed? » (2026-06-26) | « sous 5 jours ouvrés » | cotation GT gratuite |
| Article non-EVM (2026-07-08) | « 5 à 10 jours ouvrés » | — |
| Formulaire courant | « la file standard prend actuellement jusqu'à **3 mois** en raison du retard accumulé » | Express : « Fee: USD 15 000 », « ~7 days » |
| Article *Express Listing* (2026-05-24) | « délai garanti de 10 jours » | « à partir de 10 000 USD » |

De même sur l'articulation GT → CoinGecko : le formulaire Express affirme « Automatically
reflected on CoinGecko once GT data is live » et la page commerciale « Guaranteed listing on
CoinGecko once GT data is live », tandis que **les conditions du même service** écrivent que
l'Express « ne garantit l'approbation d'aucune soumission ». Les deux versions sont citées
ici ; aucune n'est reprise comme un fait.

> **À faire confirmer par ticket avant d'engager le déploiement d'un DEX.** Une pool de
> stablecoin contre BOSA échange techniquement une version **enveloppée** (WBOSA). CoinGecko
> liste les actifs enveloppés sur des pages **séparées** de l'actif natif. Aucune source lue
> n'explique comment le prix d'un **coin natif** est dérivé d'une pool WBOSA. Poser la question
> au support **avant** de dépenser l'ingénierie du DEX.

### 2.6 Ce que la page « chaîne » affichera — et d'où ça vient

Les deux chiffres de la page chaîne **ne viennent pas de CoinGecko** :

| Chiffre | Source réelle | Ce qu'il faut faire |
|---|---|---|
| Volume | **GeckoTerminal** | faire lister la chaîne sur GT, puis transmettre par ticket l'URL et le *slug* GT (article 2025-06-26) |
| TVL | **DefiLlama** | la chaîne doit d'abord figurer dans `api.llama.fi/v2/chains` avec un `gecko_id` correspondant à l'identifiant API CoinGecko du coin de gaz, puis transmettre par ticket la valeur du champ `name`. « Si votre protocole n'est pas listé sur DefiLlama, vous devez contacter leur équipe » |

Ce sont **deux dépendances externes de plus**, qui ne dépendent pas de l'éditeur.

---

## 3. La soumission CoinMarketCap

### 3.1 Canal unique, et une erreur d'aiguillage est définitive

CMC écrit : « The online submission form is the ONLY way to request for listings/updates […]
DO NOT reach out through other channels » (*Listings Criteria* § D, maj 2026-09-01). Et : «
Applications that are submitted to the wrong option(s) on the form will be discarded » (§ A) —
**jetées, pas mises en attente**.

Identifiants de formulaires **vérifiés le 2026-09-03** dans `support.coinmarketcap.com/api/v2/ticket_forms.json`
(32 formulaires) :

| Objet | Intitulé exact renvoyé par l'API | URL |
|---|---|---|
| Coin BOSA | `1 - [New Listing] Add cryptoasset` | `…/requests/new?ticket_form_id=360000493112` |
| **Chaîne** | `2 - [New Listing] Add exchange` — l'article *CMC Priority* l'intitule « [New Listing] Add exchange/**chain** » et pointe le **même** identifiant | `…/requests/new?ticket_form_id=360000493132` |
| Offre vérifiée (gratuit) | `4 - [Existing Cryptoasset] Update verified supply figures` | `…?ticket_form_id=360000493092` |
| Mise à jour d'info / tags | `7 - [Existing Cryptoasset] Update info` | `…?ticket_form_id=360000553872` |
| Self-Reporting Dashboard | `8 - [Self-reporting Dashboard] …` | `…?ticket_form_id=360000563011` |

### 3.2 Les quatre critères, appliqués à BOSA

| Critère CMC (§ B1) | État BOSA |
|---|---|
| (1) usage de cryptographie / consensus / registre distribué au service d'une réserve de valeur, d'un moyen d'échange, d'une unité de compte ou d'une application décentralisée | **satisfait** — chaîne EVM en production depuis le bloc 1, 2026-08-07 |
| (2) « Must have a functional website and block explorer » | **satisfait** — sous réserve de A2, A10, A11 |
| (3) « Must be traded publicly, and actively traded on at least one (1) exchange (with material volume) that has tracked listing status on CoinMarketCap » | **non satisfait** — aucun marché |
| (4) un représentant du projet joignable | *à fournir par l'éditeur* (nom, fonction, canal) |

Il n'y a **aucun autre critère listé** : pas de livre blanc exigé, pas d'audit exigé, pas de KYC
d'équipe exigé. Et **aucun seuil chiffré n'est publié** : « Getting listed is therefore not
simply a matter of ticking off a checklist or hitting predefined thresholds, as we benchmark
submissions against others in the cohort » (§ C). Le seul seuil d'ancienneté chiffré — « in
operation for at least sixty days » — vise **les places d'échange** (§ B2), pas les actifs.

### 3.3 Les quatre paliers de fiche

| Palier | Ce que c'est | Accessible à BOSA aujourd'hui |
|---|---|---|
| *Unverified listing* | page de paire DEX créée automatiquement depuis les données on-chain, non revue | **non** — aucune paire DEX n'existe |
| **Preview / Untracked** | projets ne remplissant **pas** B1/B2 mais présentant des forces sur les axes de la § C | **la seule porte** — ni rang, ni prix, ni présence dans l'API CMC Pro |
| *Tracked listing* | B1/B2 remplis → CMC Rank + API Pro | non |
| *Inactive* | fiche désactivée faute de données de marché | — |

**Sans place intégrée, CMC n'affiche ni prix ni volume**, et refuse explicitement un flux de
prix fourni par le projet : « Accepting price feeds without accompanying volume/liquidity allows
projects to manipulate market cap (and rank) by hardcoding artificial prices » (article *Price*,
maj 2026-07-23).

### 3.4 Les deux annexes, à préparer AVANT le dépôt

*Listings Criteria* § A : « New coin applications should strive to submit Annex M and C and get
onboarded to the SRD. » Ce sont des pièces du **premier** dossier.

| Annexe | Contenu exigé | Ce que Coinbosa peut y écrire aujourd'hui |
|---|---|---|
| **Annex C** — mise à jour d'offre (Google Sheets officiel, onglet `gid=1300521795`) | anciennes et nouvelles CS/TS/MS en unités exactes ; **point d'accès API total supply** (« numerical value only […] JSON ») ; **point d'accès API circulating supply** ; **Explorer URL** ; **Richlist URL** ; **une ligne par adresse** contrôlée par l'équipe, allouée en privé ou réservée, avec quantité, % de l'offre, propriétaire, verrouillé/déverrouillé, allocation, usage futur, date de distribution (JJ/MM/AAAA) et preuve. Adresses de réserve **surlignées en rouge**, hyperliées, sans doublon, **agrégées sur toutes les chaînes** si l'actif est multi-chaînes. Soumission par partage public du classeur | TS/MS = 700 000 000 ; les 13 lignes d'adresses existent et sont publiées ; **les trois pièces manquantes sont C5, C6, C7** ; l'agrégation multi-chaînes rouvre la question du SPL Solana (C9) |
| **Annex M** — calendrier de déblocage (onglet `gid=609936952`, téléversé par *Bulk Upload* du SRD) | `allocationName`, `startDate`/`endDate` (`YYYY-MM-DD hh:mm:ss`), `vestingType` ∈ {cliff, linear, inflationary, deflationary}, `vestingEvery`, `vestingFrequency`, `rate`, `tokenAmount`. CMC précise : « If all tokens are unlocked on TGE, select 'Cliff' » et « Unlocked Supply (UCS) is not the same as circulating supply (CS) » | **une seule ligne honnête** : `cliff`, au genesis, 700 000 000 BOSA — soit **100 % débloqué**. C'est l'état réel (`DOSSIER` § 3) ; toute autre ligne serait fausse |

**Le SRD n'est pas une porte d'entrée** : le deck officiel pose « Prerequisite: Asset must be
listed on CMC (minimally as an untracked listing) ». Et l'offre auto-déclarée s'affiche **à côté**
de l'offre vérifiée, « without any ranking implications ».

### 3.5 L'ajout de la chaîne — ce qui est publié, et ce qui ne l'est pas

- **Même guichet que les places d'échange** : formulaire 2.
- **Tarifs publiés** (CMC Priority § C3) : « Dexscan Chain integration + DEX bundle (**USD 80K**) »
  ou « UTM tracking (USD 20K) » ; et séparément « Chain ranking page: **$50K** ».
  **Aucune voie gratuite d'intégration de chaîne n'est décrite dans la documentation.**
- **Aucune spécification technique publique.** Le lien « Click for details » du bundle à 80 000 USD
  renvoie au paragraphe DEXScan des *Listings Criteria*, pas à un cahier des charges ; une
  recherche dans les 88 articles du centre d'aide ne remonte aucun article dédié.
- **DEXScan n'est pas un explorateur** : c'est un indexeur automatique de **paires DEX**. Sur
  Coinbosa Chain, il n'aurait **aucune paire à indexer** (B2, B3). CMC exige par ailleurs que le
  projet dispose de son **propre** explorateur (B1(2)) — c'est le cas.

### 3.6 Ce que l'argent achète chez CMC, et ce qu'il n'achète pas

| Service (CMC Priority, maj 2026-08-02) | Tarif publié | Délai annoncé | Applicable à BOSA ? |
|---|---|---|---|
| C1 — listing/mise à jour d'un coin | 5 000 USD | ~24 h ouvrées après paiement | **non** — le tarif est conditionné : « Price (asset must be listed on an existing CMC exchange that is feeding API data to CMC) » |
| C2 — listing/mise à jour d'une place | 50 000 USD (30 000 + 20 000/an) | ~14 jours ouvrés | sans objet |
| C3 — mises à jour sur mesure | > 5 000 USD | ~14 jours ouvrés | c'est la ligne où figurent les tarifs chaîne |
| **Offre en circulation** | **jamais payante** | — | « To avoid pay-2-win, we do not accept payment for rank-related updates […] Circulating supply updates for forms 4 and 5 will remain free » |

**Aucun intermédiaire n'est mandaté** : « We do not sanction any external service to assist in
the listing application ». CMC publie une « Hall of Shame » nominative et prévient : « If you
are scammed by such services, we will not be in a position to recover your funds. » Tout devis
promettant un délai ou un résultat garanti, hors CMCP, est illégitime par cette doctrine.

### 3.7 Ce qui diffère de CoinGecko

| Point | CoinGecko | CoinMarketCap |
|---|---|---|
| Canal chaîne | ticket de support générique | formulaire 2, le même que les places |
| Canal coin | formulaire Partners Platform (compte requis) | formulaire 1 (Zendesk) |
| Voie gratuite pour la chaîne | oui, non tarifée | **aucune décrite** — 80 000 / 50 000 USD publiés |
| Vérification publique préalable | **obligatoire** (message social + identifiant en réponse) | non documentée ; preuve de représentation exigée dans le ticket |
| Erreur d'aiguillage | non documentée | **demande jetée** |
| Accélérateur payant | Fast Pass 1 000 USD (coin) | CMCP C1 5 000 USD, inapplicable sans marché |
| Offre en circulation | mise à jour après cotation, formulaire dédié | formulaire 4, **gratuit et non accélérable** |
| Pièces attendues au premier dépôt | tokenomique + portefeuilles + explorateur | **Annex C + Annex M**, explicitement recommandées dès la première demande |
| Seuil de places pour l'offre vérifiée | non publié | « at least 3 CMC-supported exchanges », présenté comme *general guideline* |

---

## 4. L'offre en circulation — le point qui bloque le plus

### 4.1 Les deux définitions, appliquées telles quelles

| | Définition publiée | Application à BOSA |
|---|---|---|
| **CoinGecko** | *Supply Methodology* (maj 2026-05-16) : `Circulating Supply = Total Supply − Uncirculated Wallets`, les *uncirculated wallets* incluant explicitement trésorerie/fondation, fonds écosystème/marketing/partenariats et avoirs équipe et fondateurs « **même s'ils sont techniquement débloqués** » | les 700 000 000 sont sur 13 adresses du projet + le gouverneur → **CS = 0** |
| **CoinMarketCap** | *Supply* (maj 2026-08-11) : `CS = TS − insider wallets`. Exclus, verrouillés ou non : vente privée, écosystème/bounty/marketing/opérations/airdrops, masternodes et staking (au cas par cas), équipe/fondation/trésorerie/séquestre. « Assets that are […] allocated to insiders […] are generally not regarded as circulating, regardless of whether they are unlocked » | **CS = 0**, donc capitalisation classée nulle, donc statut *Unranked* |

> **Formulation à respecter.** Ce « 0 » est **l'application des formules publiées** au chiffre
> mesuré dans `DOSSIER-COTATION.md` § 3 (« offre en circulation au sens détenue hors du projet :
> 0 BOSA (0,00 %) »). Ce n'est **pas** une décision des agrégateurs ni une prédiction de leur
> décision : ils pourraient aussi bien ne publier aucune CS vérifiée. À écrire ainsi, et pas
> autrement.

**Conséquence directe :** le projet ne peut pas annoncer un autre chiffre sans se mettre en
contradiction avec la méthode publiée de l'agrégateur — ce qui est exactement le motif
« information trompeuse » des deux côtés. **Le levier n'est pas la rédaction du dossier : c'est
la distribution réelle hors du projet.**

### 4.2 Ce que chacun accepte comme preuve

**CoinGecko**, par ordre de priorité déclaré :

1. « CoinGecko privilégiera **toujours** la dérivation de l'offre totale et en circulation à
   partir des **divulgations de l'équipe** et de la **récupération de l'offre via des sources
   on-chain** » (article maj 2026-08-05).
2. Pour une mise à jour : « un lien d'explorateur montrant l'offre totale et la méthode de
   dérivation de l'offre en circulation » + « l'information de tokenomique complète » + la
   **liste complète** des portefeuilles verrouillés / vesting / équipe (formulaire de
   divulgation si la liste est longue).
3. Un point d'accès REST, **facultatif**.

**CoinMarketCap** : l'Annex C (§ 3.4), et rien d'autre. CMC refuse de reprendre les chiffres du
projet tels quels — « We may not take the figures from APIs/whitepapers/blog posts because we
have our own calculation schematic » — puis met à jour « by (i) referencing deductible wallet
balances or (ii) using relevant block explorer APIs **if there is scrutability and
reproducibility** ».

> Coinbosa est **bien placé sur ce point précis**, et c'est le seul du dossier : le genesis est
> **reproductible octet pour octet** depuis le dépôt public (`DOSSIER` § 1), les 13 soldes sont
> lisibles par `eth_getBalance`, et la réconciliation tombe **exactement** sur 700 000 000 au wei
> près. « Scrutability and reproducibility » est précisément ce que cela veut dire.

### 4.3 Le point d'accès REST — spécification exacte (CoinGecko)

Si l'éditeur choisit cette voie — et l'Annex C de CMC exige de toute façon deux points d'accès
API (lignes 7 et 8) —, **une seule construction sert les deux agrégateurs** :

| Exigence CoinGecko | Détail |
|---|---|
| Format | point d'accès REST simple, **décimales incluses**, réponse **JSON** |
| Transport | **HTTPS obligatoire** — « les points d'accès HTTP ne sont pas acceptés » |
| Authentification | **aucune** — ni mot de passe, ni clé d'API |
| Débit | suffisant pour un sondage **toutes les 30 minutes** |
| Pare-feu | si Cloudflare est en place, autoriser dans le WAF les en-têtes `X-Requested-With: com.coingecko` et `User-Agent: CoinGecko +https://coingecko.com/` |

**Pièces à construire, et par qui :**

| Pièce | Contenu | Qui | Ce qui la débloque |
|---|---|---|---|
| `/api/supply/total` | `700000000` (valeur numérique, JSON) | équipe chaîne | rien — l'offre est fixe et vérifiable |
| `/api/supply/circulating` | valeur dérivée : total − soldes des adresses du projet | équipe chaîne | la **liste** des adresses déduites, qui est déjà publiée. Aujourd'hui la valeur vaut **0** |
| Page *richlist* | classement des détenteurs, lisible publiquement | équipe chaîne | l'explorateur n'indexe rien (`ROADMAP.md`, jalon 5) — c'est le vrai préalable |
| API de l'explorateur pour les soldes | CG lit « le solde des adresses verrouillées […] automatiquement depuis l'explorateur de blocs dès qu'une API est disponible » | équipe chaîne | idem |

Une contrainte technique s'y ajoute côté GeckoTerminal : la FAQ des standards d'intégration
(question 6) impose que le **format des adresses de contrat** soit « standardisé selon
l'explorateur de blocs respectif », faute de quoi le mappage automatique *Coin Market Mapping*
échoue sur CoinGecko **et** GeckoTerminal.

### 4.4 L'ordre imposé, à ne pas essayer d'inverser

```
1. un marché réel, sur une place intégrée
2. la cotation (CoinGecko : formulaire ; CMC : formulaire 1)
3. l'offre en circulation vérifiée (CoinGecko : formulaire d'offre ; CMC : formulaire 4)
```

Aucune des trois étapes ne s'achète pour passer devant les autres : CMC refuse tout paiement lié
au rang, et le tarif C1 lui-même est conditionné à l'existence d'un marché intégré. Sur
GeckoTerminal, un jeton non listé sur CoinGecko ne peut pas faire corriger son offre — « les
données d'offre sont automatiquement issues de la chaîne ; pour les mettre à jour, le jeton doit
d'abord être listé sur CoinGecko ».

### 4.5 Le jeton SPL Solana, dans cette section précisément

L'Annex C exige l'agrégation « sur toutes les chaînes si l'actif est multi-chaînes ». Or il
existe un jeton homonyme vivant sur Solana : offre 499 999 940,39, `mintAuthority` **active**,
4,00 % détenus hors du portefeuille projet (`DOSSIER` § 8). Trois questions doivent être
tranchées **par l'éditeur** avant de remplir l'annexe :

1. BOSA natif et le SPL sont-ils déclarés comme **un** actif multi-chaînes, ou comme **deux**
   actifs distincts ? Le second cas suppose que le texte publié cesse de laisser croire au
   premier.
2. Le retrait de circulation annoncé dans `TOKENOMICS.md` a-t-il lieu — publiquement et
   vérifiablement — ou le texte est-il corrigé ?
3. `mintAuthority` et `freezeAuthority` sont-elles révoquées ? Tant qu'elles sont actives,
   l'affirmation « offre fixe, aucune émission supplémentaire » est vraie **de la chaîne
   Coinbosa** et fausse **du jeton homonyme**. Un agrégateur qui trouve cet écart seul le lira
   comme une dissimulation.

---

## 5. Les motifs de refus connus, et comment chacun se prévient ici

**Liste officielle CoinGecko** (article 4498809321369, maj 2026-08-12), huit motifs, appliqués à
Coinbosa :

| Motif | Comment il s'applique ici | Prévention | Qui |
|---|---|---|---|
| **Présence insuffisante de l'actif** — au moins une place active intégrée ; rejet possible si l'actif n'est traité que sur des places auto-référençables | **il s'applique pleinement** : zéro marché | rien d'autre qu'un marché réel. À défaut : *Preview Listing* (§ 2.4) | éditeur |
| **Information insuffisante** — site sans objet, équipe ou réseaux sociaux | s'applique **partiellement** : pas d'équipe nommée (A2), Facebook non lié depuis le site (A4), pas de compte X (A5) | corriger A2, A4, A5 avant de soumettre | éditeur + front-end |
| **Atteinte à une marque déposée / contenu inapproprié** — « rejet définitif et non susceptible d'appel » | risque identifié : le nom **BRC20** est homonyme d'un standard Bitcoin. `README.md` et `docs/INTEGRATION.md` le disent déjà | reprendre la même précision — « BRC20 de Coinbosa » — dans le formulaire. **Point juridique : à soumettre à un conseil** pour la recherche d'antériorité de marque | éditeur |
| **Usurpation d'identité / conflit de nom ou ticker** | **favorable aujourd'hui** : aucune fiche BOSA sur CoinGecko (vérifié 2026-09-03). Non vérifié côté CMC | soumettre une fois, proprement ; une demande dupliquée est elle-même un motif | éditeur |
| **Candidature malveillante / rôle du soumissionnaire manquant** — preuve d'affiliation exigée | à traiter : la procédure de vérification publique **est** cette preuve côté CG ; côté CMC, le représentant joignable (B1(4)) | exécuter les 4 étapes de § 2.3 dans l'ordre | éditeur |
| **Problèmes de contrat intelligent** — « si votre jeton est suspecté de présenter des risques de contrat potentiellement malveillants […] la candidature sera rejetée » | BOSA est un **coin natif**, sans contrat de jeton. Le risque se déplace sur le contrat système `0x…1000` et sur les trois contrats hérités sans source (`DOSSIER` § 13) | joindre le dossier de cotation : genesis reproductible, bytecode identique à l'amont pour `0x…1001`/`1002`/`1007`, source publiée pour `0x…1000` | équipe chaîne |
| **Risque de rug-pull** — signalé « lorsqu'un jeton a une **liquidité non verrouillée** » ou « n'est **pas échangé** sur une ou plusieurs places » | **les deux branches sont vraies aujourd'hui** | verrouiller la LP dès la première pool (B4) ; publier `GARDE-TRESORERIE.md` avec ses motifs (C8) ; fournir la preuve de contrôle (C4) | éditeur + garde de trésorerie |
| **Demande dupliquée ou spam** | à éviter mécaniquement | une demande, un canal, pas de relance de statut | éditeur |

**Côté CoinMarketCap**, les motifs documentés d'inadmissibilité et de sanction :

| Motif | Prévention ici |
|---|---|
| « Be truthful. False or misleading claims may render your submission inadmissible » | **c'est le point A10** : la phrase sur la Coinbosa Card doit être corrigée avant dépôt |
| Dépôt sur la mauvaise option du formulaire → **demande jetée** | utiliser les identifiants du § 3.1, vérifiés |
| Soumissions fragmentaires (« Avoid piecemeal submissions »), hyperbole, formulations vagues | le livre blanc écrit que l'écosystème « **garantit** des transactions instantanées » — à reformuler |
| Demandes en double, relances de statut | une seule demande, aucune relance |
| « Projects that attempt to manipulate or artificially inflate their figures will be **permanently disqualified** » | ne jamais publier une CS supérieure à celle que les formules donnent (§ 4.1) |
| Tentative de corruption d'un employé | interdiction absolue ; n'utiliser aucun intermédiaire (§ 3.6) |

**Motifs de délisting CMC** (§ E), à connaître avant d'entrer : faible liquidité ou activité
suspecte ; cessation du développement ; « The project's listing on CMC was the result of
misleading, incomplete, or false information » ; projet sous enquête ou sur liste de
surveillance d'un régulateur ; mauvaise réception ; atteinte à la propriété intellectuelle. Et
le **délisting automatique pour volume nul** (article maj 2026-08-29) : quand toutes les places
cessent de coter, « CoinMarketCap will deactivate the listings after a few days grace period » —
la durée du délai de grâce n'est pas chiffrée.

**L'audit externe n'est un critère de cotation chez aucun des deux.** Chez CoinGecko, le seul
article traitant du sujet renvoie à un partenariat Hacken pour l'affichage d'un rapport, et
précise que ces données « ne sont pas endossées par CoinGecko ». Chez CMC, l'audit n'apparaît ni
dans B1/B2 ni en § C ; un « Smart contract audit badge » est vendu 50 000 USD/an — c'est un
produit d'affichage, pas une condition. **Mais** le motif « Smart Contract Issues » de CoinGecko,
lui, est écrit : l'absence d'audit ne ferme pas la porte, elle prive seulement le dossier d'une
réponse toute faite si la question est posée.

---

## 6. Le calendrier réaliste

**Aucune durée n'est inventée ici.** Là où la source ne publie pas de délai, c'est écrit.

| Phase | Contenu | Dépend de nous ? | Durée annoncée par la source | Ce qui la fait déraper |
|---|---|---|---|---|
| **P0** — mise en cohérence | A2, A4, A5, A6, A8, A10, A11, A12 : équipe sur le site, Facebook lié, compte X ouvert, Telegram unifié, logo servi, Card au futur, consensus dit, un seul temps de bloc | **oui, entièrement** | — | rien, sauf la décision éditoriale sur A10 |
| **P1** — pièces d'offre | C4 (preuve de contrôle, 13 signatures), C5–C7 (API total, API circulating, richlist), D3 (archive + WebSocket, script déjà écrit) | **oui** | — | l'indexation de l'explorateur, préalable réel de la richlist |
| **P2** — un marché | B2 (DEX), B3 (stablecoin + pool), B4 (LP verrouillée) — **ou** une place d'échange tierce qui cote BOSA | **mixte** — le DEX dépend de nous, une cotation CEX **non** | — | § 2.5 : la question WBOSA doit être tranchée par ticket **avant** l'ingénierie |
| **P3** — GeckoTerminal | *Network Addition* + DEX | **non** | sources contradictoires : « 5 jours ouvrés », « 5 à 10 jours ouvrés », « jusqu'à 3 mois » (file standard) ; Express : « ~7 jours » à 15 000 USD ou « 10 jours garantis » à partir de 10 000 USD | le compte X (A5) bloque la soumission elle-même |
| **P4a** — CoinGecko, chaîne | ticket de support, 7 champs (§ 2.2) | **non** | **aucun délai publié**, aucun critère publié | dépendances GT (volume) et DefiLlama (TVL) pour l'affichage |
| **P4b** — CoinGecko, coin | vérification publique puis formulaire | **non** | « jusqu'à 5 jours » (Regular) / « sous 24 heures » (Fast Pass, 1 000 USD) ; article dédié : « typiquement 3 à 5 jours ouvrés », sans garantie ; « si votre jeton n'est pas coté après 2 semaines, il est probable que le projet n'a pas passé notre évaluation » | l'exigence de marché (§ 0) — sinon, viser le *Preview Listing* |
| **P4c** — CMC, coin | formulaire 1 + Annex C + Annex M | **non** | tier gratuit : « days to months/years », explicitement | Annex M affichera 100 % débloqué (C3) |
| **P4d** — CMC, chaîne | formulaire 2 | **non** | **aucun délai publié** en tier gratuit | aucune voie gratuite décrite ; tarifs publiés 80 000 / 50 000 USD |
| **P5** — offre vérifiée | CG : formulaire d'offre ; CMC : formulaire 4 | **non** | CG : mise à jour impossible avant cotation **et** avant trading. CMC : « general guideline » de 3 places supportées avec activité matérielle | tant que CS = 0, cette phase n'a pas d'objet |

**Ce qui ne dépend pas de nous, énuméré sans ambiguïté :** la décision de cotation elle-même des
deux côtés (CoinGecko « n'est pas tenue de fournir de motif », CMC « benchmark submissions
against others in the cohort ») ; les délais de file de GeckoTerminal ; l'intégration DefiLlama ;
la décision d'une place d'échange tierce de coter BOSA ; les tarifs et délais publiés, qui « peuvent
changer sans préavis » et sont à revérifier avant chaque dépôt.

---

## 7. Ce qui manque encore, et que personne ne peut contourner

**1. Un marché.** C'est le préalable écrit des deux méthodologies (§ 0). Aujourd'hui : zéro
place, zéro paire, zéro BOSA détenu hors du projet. Aucun paiement, aucune rédaction et aucun
intermédiaire n'y substituent quoi que ce soit.

**2. De quoi faire un marché.** Un DEX, un stablecoin, une pool approvisionnée — sur une chaîne
où **zéro contrat utilisateur** a été déployé en 403 419 blocs (`DOSSIER` § 5, § 9). C'est un
chantier d'ingénierie et une décision d'adossement, pas une formalité de dossier. **Point
juridique : à soumettre à un conseil** pour l'émission d'un stablecoin par l'éditeur.

**3. Une offre réellement distribuée.** Tant que les 700 000 000 sont sur des adresses du
projet, les deux formules publiées donnent CS = 0, donc capitalisation classée nulle chez CMC et
absence de capitalisation chez CoinGecko. Le levier est la distribution, pas la présentation.

**4. Des documents publiés exacts.** Deux affirmations du site et du livre blanc sont fausses ou
invérifiables en l'état : la Coinbosa Card « déployée à l'échelle mondiale » (A10) et le silence
sur le jeton SPL Solana toujours vivant et toujours émissible (C9). CMC en fait un motif
d'inadmissibilité, CoinGecko une violation de garantie contractuelle assortie d'une
dépublication discrétionnaire. **C'est le seul point de cette liste qui se corrige en une
journée, sans budget et sans dépendance externe.** Il faut le faire en premier.

**5. Un compte X officiel.** Champ obligatoire du formulaire GeckoTerminal — c'est-à-dire de la
seule voie ouverte à un coin natif sans CEX. `"twitter": ""` dans deux fichiers du dépôt.

**6. Une infrastructure qu'un intégrateur peut brancher.** Un seul RPC, un seul serveur, pas
d'archive au-delà de ~36 heures, pas de WebSocket, `finalized` et `safe` figés au bloc 0 à vie.
`deploy/73-node-archive.sh` traite l'archive et le WebSocket et **n'est pas appliqué** ; la
redondance matérielle, elle, reste entièrement à faire.

**Ce qui, en revanche, est déjà en état de soutenir un examen** — et qui doit être mis en avant
plutôt que laissé à découvrir : le genesis **reproductible octet pour octet** depuis le dépôt
public, la réconciliation d'offre **exacte au wei**, les 13 adresses publiées avec leurs soldes,
l'entrée Chainlist active, et un dossier de cotation qui énonce lui-même ses écarts. C'est très
exactement ce que CMC appelle « scrutability and reproducibility ».

---

## Annexe A — Ce que ce plan ne sait pas

À lire avant toute soumission ; rien de ce qui suit n'a été comblé par une hypothèse.

1. **Les champs exacts des deux formulaires de cotation.** `partner.coingecko.com/request-form/new`
   renvoie HTTP 403 sans session authentifiée ; côté CMC, le formulaire 1 comporte 66 champs
   selon l'API `ticket_forms.json`, mais leurs libellés n'ont pas pu être lus (pages en 403,
   `ticket_fields.json` en 401). **Aucune liste de champs de ce document n'est exhaustive.**
   Ouvrir les deux formulaires à blanc, avec un compte, avant de rédiger le dossier.
2. **Les seuils de liquidité et de volume.** Aucun n'est publié, des deux côtés, et les deux
   l'écrivent explicitement. Un seuil de liquidité existe bien chez CoinGecko (mentionné pour
   l'affichage des paires) mais sa valeur n'est publiée nulle part. Tout chiffre trouvé ailleurs
   serait inventé.
3. **Les critères et le délai de la demande de chaîne CoinGecko.** L'article dit « si la
   candidature satisfait nos critères » sans les énoncer, et ne publie aucun délai. On ignore si
   une présence préalable sur GeckoTerminal est exigée, et si un nombre minimum de jetons cotés
   sur la chaîne l'est — le champ *Current Token Listing* est marqué « if applicable », ce qui
   suggère que non ; c'est une lecture, pas une certitude.
4. **Le prix d'un coin natif dérivé d'une pool WBOSA.** Non documenté. CoinGecko liste les actifs
   enveloppés sur des pages séparées. À confirmer par ticket **avant** d'engager le DEX (§ 2.5).
5. **L'éligibilité d'un actif sans aucun marché au palier CMC *Preview / Untracked*.** La
   documentation crée ce palier pour les projets ne remplissant pas B1/B2, mais n'écrit nulle
   part qu'un actif sans **la moindre** place y est éligible. À poser comme question directe
   dans le ticket, pas à présenter comme acquis.
6. **Les exigences minimales de l'explorateur.** Aucune source ne pose de standard (ni EIP-3091,
   ni API compatible Etherscan, ni marque tierce). L'impact réel du `standard: "none"` de
   l'entrée Chainlist de Coinbosa est **inconnu**.
7. **Les contradictions internes des sources GeckoTerminal** (délais, tarif Express, garantie de
   report sur CoinGecko) : citées en double au § 2.5, non tranchées.
8. **Le conflit de ticker « BOSA » côté CoinMarketCap** : non vérifié (pas d'API publique de
   recherche sans clé). Vérifié seulement côté CoinGecko, le 2026-09-03.
9. **La règle des 60 jours d'exploitation** est écrite pour les places d'échange (CMC § B2). On
   ignore si elle s'applique à une chaîne déposée sur le même formulaire 2.
10. **Périmètre temporel.** Tarifs et délais cités sont ceux affichés les 2026-09-02 / 2026-09-03
    et « peuvent changer sans préavis ». À revérifier avant chaque dépôt.

---

## Annexe B — Sources

**Repères du dépôt** — `WHITEPAPER.md`, `TOKENOMICS.md`, `DOSSIER-COTATION.md` (§ 1, 3, 4, 5, 7,
8, 9, 12, 13, 15, 16), `GARDE-TRESORERIE.md` (§ 1, 4, 6, 7), `ROADMAP.md`, `README.md`,
`POBS.md`, `docs/INTEGRATION.md`, `coinbosa.config.json`, `explorer/app.js`, `site/index.html`,
`site/a-propos.html`, `site/ecosysteme.html`, `whitepaper/index.html`, `deploy/73-node-archive.sh`.

**Vérifié directement le 2026-09-03** — `https://chainid.network/chains.json` (2 745 chaînes,
entrée 26262) · `https://api.coingecko.com/api/v3/search?query=BOSA` (`"coins":[]`) ·
`https://api.coingecko.com/api/v3/asset_platforms` (465 plateformes, aucune correspondance) ·
`https://support.coinmarketcap.com/api/v2/ticket_forms.json` (32 formulaires, identifiants du § 3.1).
Non rejouable ce jour : `https://api.geckoterminal.com/api/v2/networks` (HTTP 403) — le constat
d'absence de réseau « bosa » date donc du **2026-09-02**, de même que le relevé des plateformes
GeckoTerminal.

**CoinGecko** — *How to Request a New Chain Listing (Asset Platform)* · *How to List a New
Cryptocurrency on CoinGecko* · *Methodology* (Listing Criteria) · *Terms & Conditions for
Listing* (12 août 2025) · *Why is my token not listed on CoinGecko?* (4498809321369) · *How long
does it take…* · *Verification Guide for Listing & Update Requests* · *CoinGecko Supply
Methodology* · *Total Supply/Circulating Supply API Endpoint Requirement* · *Guide: How to Use
the CoinGecko Supply Update Form* · *Understanding Supply Update Request Rejection Reasons* ·
*How to Preview List Tokens on CoinGecko* · *Support Directory* · *Fast Pass* (FAQ et
couverture) · *How to Add a New Exchange on CoinGecko* · *Why Don't I See My Trading Pairs* ·
*How to update TVL / Volume on my Chain page* · *How do I get my EVM Chain/DEX listed on
GeckoTerminal* · *DEX Forks supported by GeckoTerminal* · *GeckoTerminal DEX Express Listing* ·
*Can I update the supply information of tokens listed on GeckoTerminal* · *How do I update
Security Audit reports/ratings* · `about.geckoterminal.com/dex-chain-listing`.

**CoinMarketCap** — *Listings Criteria* (maj 2026-09-01) · *Supply (Circulating, Total, Max)*
(maj 2026-08-11) · *CMC Priority (CMCP)* (maj 2026-08-02) · *Ranking* (maj 2026-08-27) ·
*Category-Specific Listings Criteria* · *Link to Request Form* · *Cryptoasset Listings* ·
*Exchange Listings* · *Price (Market Pair, Cryptoasset)* · *Market Data* · *Delisting Coins /
Tokens with Zero Volume* (maj 2026-08-29) · *Self-reporting Portal* · Annex C et Annex M
(classeur Google officiel, onglets `gid=1300521795` et `gid=609936952`) · deck officiel du
Self-Reporting Dashboard.

---

*Document de travail interne — coinbosa, Inc. Aucun engagement de cotation n'est pris ici, par
personne. Aucun rendement, aucune projection de prix.*
