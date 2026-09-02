<div align="center">
  <img src="assets/coinbosa-logo.jpg" alt="Coinbosa" width="90" />

  # Plan de mise sur le marché — Coinbosa Chain et BOSA

  **Document de travail — éditeur : coinbosa, Inc. (Delaware, États-Unis)**
</div>

---

## Comment lire ce document

Ce plan décrit la mise sur le marché de **BOSA**, coin natif de **Coinbosa Chain**
(chainId 26262, consensus Parlia, blocs de 5 s). Il sera lu par des tiers : une place
d'échange, un agrégateur, un partenaire.

Trois règles de rédaction, appliquées ligne par ligne :

1. **Chaque chiffre porte sa source.** Les mesures de chaîne renvoient au dossier de
   cotation ; les règles des agrégateurs renvoient à leurs articles publiés, lus le
   **2026-09-02**.
2. **Chaque action dit qui la fait et ce qui la débloque.** Une action sans propriétaire
   n'est pas une action.
3. **Ce qui manque est écrit « à fournir par l'éditeur »**, jamais comblé par une
   estimation.

Aucune promesse de rendement, aucune projection de prix, aucun conseil juridique ne figure
ici. Là où un point relève du droit, il est marqué **point juridique : à soumettre à un
conseil**.

**Conventions de source.** `[D§n]` = `DOSSIER-COTATION.md` section n · `[T]` =
`TOKENOMICS.md` · `[R]` = `ROADMAP.md` · `[W]` = `WHITEPAPER.md` · `[G§n]` =
`GARDE-TRESORERIE.md` · `[DEC]` = `DECISIONS.md` · `[CG]` = documentation CoinGecko ·
`[CMC]` = documentation CoinMarketCap · `[GT]` = documentation GeckoTerminal.

---

## 0. La contrainte qui commande tout le plan

Un plan marketing ordinaire commence par l'audience. Celui-ci ne peut pas, parce que les
deux agrégateurs qui décident de la visibilité de BOSA posent la même condition d'entrée, et
que le projet ne la remplit pas.

| Agrégateur | Condition écrite | État de BOSA |
|---|---|---|
| CoinGecko | « votre cryptomonnaie doit être activement échangeable sur une place d'échange suivie par CoinGecko » ; Methodology : « Listed on at least one (1) active exchange where CoinGecko is integrated with » | **aucune place ne cote BOSA** `[D§3]` |
| CoinMarketCap | critère B1(3) : « Must be traded publicly, and actively traded on at least one (1) exchange (with material volume) that has tracked listing status on CoinMarketCap » | idem |

Conséquences directes, à écrire une fois pour toutes :

- **Aucun budget ne contourne cette condition.** CMC conditionne explicitement son option
  payante C1 (5 000 USD) à « asset must be listed on an existing CMC exchange that is feeding
  API data to CMC ». CoinGecko rappelle que la cotation est gratuite et qu'« aucun
  représentant ne demandera jamais de frais de cotation ».
- **L'ordre est imposé : marché d'abord, cotation ensuite, offre en circulation en dernier.**
  CoinGecko refuse toute mise à jour d'offre « tant que le jeton n'est pas actif et échangé,
  en l'absence de données de prix ».
- **Le seul palier ouvert à un actif sans marché** est la cotation anticipée (*Preview
  Listing* chez CoinGecko, *Preview / Untracked* chez CMC). Il ne donne ni prix, ni rang, ni
  présence dans l'API CMC Pro. Réserve honnête : CMC crée ce palier pour les projets qui ne
  remplissent pas B1/B2, mais **n'écrit nulle part** qu'un actif sans la moindre place y est
  éligible — c'est une lecture du texte, à poser comme question dans le ticket plutôt qu'à
  tenir pour acquise.
- **L'offre en circulation calculée par les deux agrégateurs vaut 0 aujourd'hui.**
  CoinGecko : `Circulating Supply = Total Supply − Uncirculated Wallets`, les avoirs
  trésorerie / fondation / équipe étant exclus « même s'ils sont techniquement débloqués ».
  CMC : `CS = TS − insider wallets`. Les 700 000 000 BOSA sont sur 13 clés du projet plus le
  gouverneur `[D§7]`. **Ceci est l'application de leur formule publiée à un chiffre mesuré,
  pas une prédiction de leur décision** : les deux peuvent aussi ne publier aucune offre
  vérifiée. Le levier est la distribution réelle hors du projet, pas la rédaction du dossier.

**Ce plan sert donc d'abord à rendre une candidature recevable, et ensuite seulement à la
faire connaître.**

---

## 1. Ce que Coinbosa est réellement

### 1.1 La phrase qui tient devant quelqu'un qui vérifie

> **Coinbosa Chain est un réseau EVM souverain en production depuis le 7 août 2026, dont
> l'identité est reproductible octet pour octet depuis un dépôt public, portant un coin natif
> à offre définitivement fixe, édité par une société constituée au Delaware et adossé à une
> école de formation déjà en activité. Le réseau est aujourd'hui opéré par un seul validateur,
> le coin n'a pas de marché, et aucun audit externe n'a été publié.**

Cette phrase est écrite pour être vérifiée en une demi-heure, sans faire confiance à
l'éditeur. Elle ne contient aucun superlatif parce qu'aucun superlatif ne survivrait à la
vérification.

### 1.2 Les quatre actifs réellement vérifiables

| Actif | Ce qu'il vaut devant un tiers | Vérification |
|---|---|---|
| **Genesis reproductible** | Un tiers reconstruit le genesis depuis le dépôt public avec `solc 0.8.26`, obtient l'empreinte `0x8dcdadc2…018da6` et la compare à celle servie par le RPC. **Aucune allocation cachée n'est possible** : la racine d'état engage l'état initial entier | SHA-256 `4d93164f…b75b7ce7`, identique au genesis publié `[D§1]` |
| **Offre conservée au wei** | 699 998 999,999979 (13 adresses) + 1 000 (gouverneur) + 0,000021 (contrat `0x…1000`) = **700 000 000,000000**. Aucune émission (pas de récompense de bloc), aucune destruction (`baseFeePerGas = 0`) | `[D§3]`, `[T]` |
| **Régularité mesurée** | Temps de bloc **5,0000 s** sur 2 400 intervalles échantillonnés ; **5,0025 s** de moyenne sur 403 418 intervalles ; disponibilité temporelle **99,9495 %** sur 23,36 jours | `[D§2]`, `[D§10]` |
| **Academy en production** | Le seul produit de l'écosystème réellement en activité, avec plusieurs milliers d'étudiants formés | `[R]`, `[W] § 5` |

### 1.3 Ce qu'on ne dit pas, et pourquoi

`DECISIONS.md` D10 fixe déjà cette liste. Elle est reprise ici parce qu'un plan marketing est
précisément l'endroit où elle se perd.

| Interdit de langage | Fait mesuré qui le rend faux |
|---|---|
| « décentralisé », « résistant à la censure », « sans confiance » | **1 validateur** produit 100 % des blocs depuis le bloc 1 ; **1 pair réseau** `[D§9]` |
| « finalité » | `finalized` et `safe` restent au **bloc 0** ; clé de vote BLS = 48 octets nuls `[D§9]` |
| « audité », « sécurité éprouvée » | **aucun audit externe publié ni engagé** `[D§13]` |
| « preuve d'enjeu », « staking » | PoBS est **spécifié, non déployé** — 0 octet à `0x…2001`, `0x…2002`, `0x…2003` `[POBS.md]` |
| « 400 000 transactions par seconde » | jamais mesuré `[DEC D10]` |
| Coinbosa Card au présent | **pas opérationnelle, aucun sponsor BIN à ce jour** |
| « calendrier de déblocage » | **il n'en existe aucun** : 13 clés simples, dépensables immédiatement `[D§3]` |

### 1.4 Le chantier préalable que personne n'aime : les documents publiés sont en écart

C'est le point le plus urgent de ce plan, et il ne coûte que du temps de rédaction.

| Document public | Écart constaté | Preuve |
|---|---|---|
| `coinbosa.com/whitepaper/` | **zéro occurrence du mot « risque »** ; aucune mention du validateur unique, de la preuve d'autorité, de l'absence d'audit ni de l'absence de finalité | `grep -ci risque whitepaper/index.html` → **0** |
| `coinbosa.com/whitepaper/` et `WHITEPAPER.md` | affirment la Coinbosa Card « **déployée à l'échelle mondiale sans limitation régionale** », au présent, alors que le produit n'est pas actif | `whitepaper/index.html` ligne 159 |
| `WHITEPAPER.md` | ne contient **ni § 3 « ni passerelle, ni audit externe » ni § 10 « facteurs de risque »**, que le dossier de cotation cite pourtant | `grep "^## " WHITEPAPER.md` → 8 titres, dont 7 sections numérotées, aucune sur les risques ; `[D§13]`, `[D§14]` citent ces passages |
| `WHITEPAPER.md` / `TOKENOMICS.md` | annoncent le retrait de circulation des 500 000 000 jetons SPL Solana — **il n'a pas eu lieu**, 4,00 % sont hors du portefeuille projet, `mintAuthority` et `freezeAuthority` sont **actives** | `[D§8]` |
| `docs/INTEGRATION.md` | écrit encore « les points d'accès RPC publics ne sont pas encore ouverts » alors que `https://explorer.coinbosa.com/rpc` répond | `[D§4]` |
| Pages du site | zéro occurrence du mot « risque » sur les cinq pages | `grep -ci risque site/*.html` → 0 partout |

**Pourquoi c'est bloquant et pas cosmétique.** En soumettant, le projet garantit
contractuellement que toute information fournie est « vraie, exacte, complète, à jour, non
contrefaisante et non trompeuse à quelque égard que ce soit », avec obligation
d'indemnisation, et CoinGecko « se réserve le droit de publier ou dépublier tout cryptoactif
sans préavis » s'il estime une information inexacte (*Terms & Conditions for Listing*, version
du 12 août 2025). CoinGecko demande en outre expressément que « votre site officiel et votre
documentation reflètent la même information que celle incluse dans votre demande ». CMC écrit :
« Be truthful. False or misleading claims may render your submission inadmissible », et liste
parmi ses motifs de délisting : « The project's listing on CMC was the result of misleading,
incomplete, or false information ».

Le dossier de cotation du projet est, lui, exact et complet. **L'écart n'est pas entre le
projet et la réalité : il est entre les documents publics du projet et son propre dossier
interne.** C'est réparable en quelques jours, et c'est la première dépense de ce plan.

---

## 2. À qui on parle

Six segments. Pour chacun : ce qu'il cherche, où il se trouve, ce qu'on lui donne, ce qui
manque pour le servir.

### 2.1 Les étudiants et anciens de Coinbosa Academy — l'actif à traiter comme tel

Plusieurs milliers d'étudiants ont été formés `[W § 5]`, `[R]`. C'est la seule audience que le
projet possède déjà, et la seule qui ne coûte pas d'acquisition.

- **Ce qu'ils cherchent :** comprendre, puis utiliser. Ils viennent de la formation FinTech,
  blockchain, IA et trading — pas de la spéculation sur un nouveau jeton.
- **Où ils sont :** dans les cours de l'Academy (en ligne et séminaires régionaux) et sur
  **`t.me/Coinbosaofficial`**, seul canal de discussion déclaré officiel par le site.
- **Ce qu'on leur donne :** un module « vérifier une chaîne soi-même » construit sur les
  scripts du dépôt (`check-genesis-hash.js`, `check-custody.js`). Le projet devient un support
  pédagogique au lieu d'être un argumentaire.
- **Ce qui manque — à fournir par l'éditeur :** l'effectif exact, les pays, la plateforme de
  cours, l'existence d'une base de contacts et **la base légale de sa réutilisation à des fins
  de communication**. *Point juridique : à soumettre à un conseil.*
- **Interdit associé :** aucune contrepartie en BOSA promise pour une participation, une
  inscription ou un parrainage `[DEC D11]`.

### 2.2 Les développeurs EVM

- **Ce qu'ils cherchent :** un RPC joignable, un explorateur lisible, un guide juste, la
  certitude de ne pas perdre leur travail.
- **Où ils sont :** le dépôt `github.com/Coinbosa/coinbosa-chain`, et nulle part ailleurs pour
  les questions techniques (règle déjà publiée sur le site).
- **Ce qu'on leur donne :** `docs/INTEGRATION.md` corrigé, l'entrée `chainid.network`
  (chainId 26262, présente et vérifiée), et l'avertissement que le standard **BRC20** de
  Coinbosa n'a aucun rapport avec le BRC-20 des inscriptions Bitcoin.
- **Ce qui manque :** un point d'accès WebSocket, un nœud d'archive, un second RPC. Le script
  `deploy/73-node-archive.sh` existe, est réversible, et **n'est pas déployé** `[D§12]`.
- **Fait à ne pas cacher :** **zéro contrat utilisateur n'a jamais été déployé** sur la chaîne
  `[D§5]`. Le premier développeur externe sera le premier tout court.

### 2.3 Les agrégateurs et les places d'échange — une audience avec une liste écrite

Ce segment ne se convainc pas, il se satisfait. Ses attentes sont publiées :

| Ce qu'il vérifie | Source | État Coinbosa |
|---|---|---|
| Site fonctionnel donnant objet, équipe et réseaux sociaux | CG Methodology, critère 1 | site en 6 langues ; **profils sociaux : Telegram seul** |
| Site possédé par l'équipe, pas un constructeur de sites | CG Methodology, critère 2 | **satisfait** — hébergement propre |
| « Working block explorer » | CG critère 3 ; CMC B1(2) | explorateur en ligne (HTTP 200) mais **il n'indexe rien** : ni historique par adresse, ni vérification de source `[README]` |
| Offre en circulation clairement communiquée | CG critère 4 | **0 hors projet**, documenté `[D§3]` |
| Au moins une place d'échange active intégrée | CG et CMC B1(3) | **aucune** |
| Représentant du projet joignable | CMC B1(4) | **à nommer par l'éditeur** |

### 2.4 Les candidats validateurs

- **Ce qu'ils cherchent :** un revenu.
- **Ce qu'on leur dit, sans adoucissement :** la rémunération vient **uniquement** des frais de
  transaction ; sans trafic, « le rendement n'est pas faible au lancement, il est **nul** »
  `[T]`. La chaîne a enregistré **1 transaction utilisateur** sur 403 419 blocs `[D§9]`.
- **Ce qu'on ne publie jamais :** un taux de rendement, actuel ou projeté `[T]`, `[R]`.
- **Ce qu'il faut écrire noir sur blanc :** le validateur de genèse **ne peut jamais être
  exclu, ni sanctionné, ni remplacé** — la garde est inscrite dans le bytecode du bloc 0
  `[POBS.md]`. Un candidat qui le découvre seul le lira comme une dissimulation.

### 2.5 Les partenaires paiement et rampes fiat

- **Ce qu'ils cherchent :** une chaîne qu'ils référencent déjà.
- **L'obstacle, nommé par le projet lui-même :** « les processeurs de paiement et les rampes
  fiat ne référencent en général que les chaînes majeures […] c'est le point de blocage le plus
  probable de tout l'édifice » `[R jalon 7]`, `[DEC D8]`.
- **Conséquence pour la communication :** la Coinbosa Card ne fait l'objet d'aucune annonce
  tant qu'un prestataire n'a pas signé. *Point juridique : à soumettre à un conseil.*

### 2.6 La presse et les médias spécialisés

Délibérément en dernier. Un article publié avant que le livre blanc soit corrigé transforme un
écart réparable en citation permanente. **Aucune sollicitation presse avant la fin de la
phase A.**

---

## 3. Les trois phases

**Définition de J0, à ne pas confondre :** le coin est en ligne depuis le **bloc 1, le
2026-08-07 13:39:55 UTC** — c'est cette date qu'un agrégateur doit afficher comme date de
lancement du réseau, le bloc 0 portant `timestamp: 0` `[D§1]`. **J0 désigne ici l'ouverture du
premier marché réel de BOSA**, seul événement qui change quoi que ce soit pour un agrégateur.

**Les six rôles.** Ce sont des fonctions, pas nécessairement six personnes ; l'éditeur les
nomme. Un rôle sans nom est un rôle non tenu.

`ÉDITEUR` (coinbosa, Inc. — décisions engageantes, paiements, signatures) · `CHAÎNE`
(exploitation du serveur et des nœuds) · `DOSSIER` (responsable unique des guichets
agrégateurs, contact déclaré) · `RÉDACTION` (documents publics et site, 6 langues) ·
`ACADEMY` (équipe formation) · `MODÉRATION` (Telegram et compte X).

---

### Phase A — Avant l'ouverture du marché

**Objectif :** rendre une candidature recevable et rendre les documents publics exacts. Rien
d'autre. Aucune soumission n'est déposée pendant cette phase.

**Pourquoi cet ordre est non négociable :** un refus coûte cher et se répare mal. CoinGecko
pose que « si votre jeton n'est pas coté après 2 semaines, il est probable que le projet n'a
pas passé notre évaluation », qu'une atteinte à une marque est « définitive et non susceptible
d'appel », et que pour une place d'échange « les rejets sont définitifs » avec resoumission
« dans environ 90 jours ». CMC jette purement et simplement une demande déposée sur le mauvais
formulaire.

| # | Action | Qui | Ce qui la débloque |
|---|---|---|---|
| A1 | Corriger les documents publics : retirer l'affirmation présente sur la Card, rétablir une section facteurs de risque (validateur unique, preuve d'autorité, pas d'audit, pas de finalité, offre concentrée), corriger `docs/INTEGRATION.md` | `RÉDACTION` + `ÉDITEUR` | rien d'externe — une décision de l'éditeur |
| A2 | Créer le **compte X officiel du réseau** et le déclarer sur `/a-propos.html` | `ÉDITEUR` + `MODÉRATION` | rien. **Bloque deux guichets** : le formulaire GeckoTerminal exige un « Network Twitter/X URL » obligatoire, et la procédure de vérification publique de CoinGecko n'accepte qu'un message depuis **X, Facebook ou Instagram** — Telegram n'y figure que comme identifiant optionnel |
| A3 | Nommer le représentant joignable et publier une adresse au domaine | `ÉDITEUR` | critère CMC B1(4) |
| A4 | Exécuter `deploy/73-node-archive.sh` (nœud archive + WebSocket), puis monter un **second point d'accès sur une autre machine** | `CHAÎNE` | le script est écrit et réversible ; la redondance matérielle demande un serveur — coût à fournir par l'éditeur |
| A5 | Indexer l'explorateur et exposer **deux points d'accès d'offre** (total, circulation) | `CHAÎNE` | décision d'indexer. Contraintes écrites : HTTPS obligatoire, **aucune authentification**, JSON, valeur numérique avec décimales, sondable toutes les 30 min ; si un WAF est devant, autoriser `X-Requested-With: com.coingecko` et `User-Agent: CoinGecko +https://coingecko.com/` |
| A6 | Réparer le logo du registre de chaînes ou le fournir en direct | `CHAÎNE` | l'icône IPFS déclarée dans `ethereum-lists/chains` **n'a été servie par aucune des trois passerelles testées** `[D§4]` |
| A7 | Publier une **preuve de contrôle** des 13 adresses (signature de message datée, `personal_sign`) | `ÉDITEUR` + `CHAÎNE` | accès aux clés. 12 des 13 n'ont jamais signé `[D§7]` ; CoinGecko rejette pour « rôle du soumissionnaire manquant, preuve d'affiliation exigée » |
| A8 | Trancher publiquement le sort du jeton SPL Solana : soit retrait de circulation prouvé **et** révocation de `mintAuthority` / `freezeAuthority`, soit correction du texte publié **et** explication des 4,00 % détenus ailleurs | `ÉDITEUR` | détention de la clé `3zADM…2Pkq`. Sans cela, un jeton « Coinbosa » vivant et **encore émissible** subsiste sur Solana : matière première idéale pour une usurpation, et motif de rejet écrit (« conflit de nom/ticker », « demande dupliquée ») |
| A9 | Choisir et construire la voie de marché (§ 3.1 ci-dessous) | `ÉDITEUR` + `CHAÎNE` | décision, puis développement |
| A10 | **Verrouiller la liquidité** de la pool et publier le verrou | `CHAÎNE` | l'existence de la pool (A9). Le motif de rejet « rug pull » vise nommément la « liquidité non verrouillée » et le jeton « non échangé sur une ou plusieurs places » |
| A11 | Remplir **Annex C** (mise à jour d'offre) et **Annex M** (calendrier de déblocage) au modèle officiel CMC | `DOSSIER` | A5 (points d'accès et richlist). CMC recommande de les joindre **dès la première demande** |
| A12 | Fixer et **annoncer publiquement** la date et l'heure de J0 | `ÉDITEUR` | tout ce qui précède. C'est la condition d'éligibilité de la cotation anticipée : « une date et une heure de TGE claires, appuyées par des annonces officielles vérifiables ». Règle interne du projet : aucune date n'est annoncée avant d'être tenable `[G§7]` |

**Sur A11, une honnêteté qui coûte.** Renseignée exactement aujourd'hui, l'Annex M ne peut
contenir qu'**une ligne `cliff` au genesis pour 700 000 000 BOSA**, soit 100 % de l'offre
débloquée — parce qu'il n'existe ni séquestre, ni *timelock*, ni calendrier `[D§3]`. Le projet
a par ailleurs décidé, avec des raisons vérifiées, de **ne pas mettre la trésorerie sous
multi-signatures avant cotation** : aucune infrastructure Safe n'existe sur la chaîne (0 octet
aux adresses canoniques), le relais `/rpc` plafonne le corps des requêtes à **32 Ko** alors que
le seul singleton Safe 1.4.1 pèse 23 620 octets de code de création, le chainId 26262 n'est pas
dans les 53 réseaux de la configuration Safe, et aucune transaction corrective n'est possible
sur un réseau à un validateur `[G§6]`. **Cette décision se déclare, elle ne se dissimule pas** :
elle est défendable écrite, indéfendable découverte. À noter que le verrouillage du LP (A10)
est une opération distincte et beaucoup plus étroite — c'est elle, et non la garde de la
trésorerie, que vise le motif de rejet.

#### 3.1 La voie de marché — deux options documentées, aucune gratuite en travail

| Voie | Ce qu'elle exige | Points durs propres à Coinbosa |
|---|---|---|
| **A — DEX sur la chaîne, puis GeckoTerminal** | un DEX vivant, une pool native/stable réellement approvisionnée, et une adresse de pool à déclarer | **aucun jeton BRC20 n'est déployé** et **aucun contrat utilisateur n'a jamais été créé** `[D§5]` : il faut donc un stablecoin sur la chaîne, qui n'existe pas. Déployer un **fork Uniswap V2** évite entièrement la voie « adaptateur » (API `/latest-block`, `/asset`, `/pair`, `/events` à écrire et à maintenir, sondée toutes les 2 s). **Vérifier avant de planifier** que chaque contrat passe la limite de **32 Ko** du corps de requête `/rpc` `[D§12]`, ou déployer depuis le serveur |
| **B — DEX intégré directement comme place d'échange chez CoinGecko** | pour un DEX spot, « la documentation API publique n'est **pas** obligatoire si l'intégration peut être réalisée via l'adresse de factory et de router » | dépend du même DEX que la voie A ; les rejets de place sont **définitifs** avec resoumission à ~90 jours |

**Question à poser par ticket avant d'engager le développement, et non après :** comment le
prix d'un **coin natif** est dérivé d'une pool qui échange techniquement une version enveloppée
(WBOSA), sachant que les agrégateurs listent les actifs enveloppés sur des pages **séparées** de
l'actif natif. La documentation lue ne le dit pas. Engager un déploiement sur une hypothèse non
confirmée serait la dépense la plus évitable de ce plan.

**Piège à écrire noir sur blanc :** la place d'échange de l'écosystème (**NextFuture**, « en
construction » `[R]`) ne peut pas être le seul marché de BOSA. CoinGecko : « Projects traded
only on self-serviceable centralized/decentralized exchanges may be rejected due to security
concerns », et le motif « présence insuffisante » vise ce cas. Faire coter BOSA sur sa propre
place et s'en prévaloir auprès d'un agrégateur est le raccourci qui ferme le dossier.

#### Signes de réussite de la phase A — tous binaires, tous vérifiables par un tiers

- `coinbosa.com/whitepaper/` contient une section facteurs de risque et **aucune** affirmation
  au présent sur un produit non actif, dans les six langues du site.
- Un compte X officiel existe et figure sur la page des canaux officiels.
- Deux points d'accès RPC indépendants répondent ; un `wss://` répond ; un nœud d'archive sert
  l'état au bloc 1.
- L'explorateur sert un historique par adresse et deux points d'accès d'offre en HTTPS, JSON,
  sans authentification.
- Les 13 signatures de contrôle sont publiées et vérifiables.
- Le sort du jeton SPL est tranché et publié.
- Une pool BOSA/stablecoin existe, est approvisionnée, son LP est verrouillé et le verrou est
  public.
- Annex C et Annex M sont remplies, relues, et ne contiennent aucune ligne inventée.

---

### Phase B — La semaine de l'ouverture du marché

**Objectif :** ouvrir le marché, déposer une seule fois par guichet, dans le bon ordre, sans
publier un seul chiffre invérifiable.

| Jour | Action | Qui | Détail imposé par la source |
|---|---|---|---|
| J−3 | **Message public de vérification** depuis le compte X officiel, annonçant l'intention de soumettre une demande à CoinGecko, avec l'URL GeckoTerminal si elle existe | `ÉDITEUR` | étape 1 de la procédure obligatoire depuis 2026 : le message doit précéder le formulaire |
| J0 | Ouverture du marché à l'heure annoncée | `CHAÎNE` | aucune communication de prix, aucune capture d'écran de cours |
| J0 | Dépôt **GeckoTerminal — Network Addition**, via le formulaire lié depuis `about.geckoterminal.com/dex-chain-listing` | `DOSSIER` | champs obligatoires : e-mail, identifiant Telegram, Network Name, Explorer URL, Chain Logo (lien Imgur, fond transparent de préférence), ticker du jeton natif, **Network X URL**, question EVM / non-EVM. RPC facultatif, **https uniquement, wss refusé** |
| J0+1 | Dépôt **CoinGecko — cotation du coin**, sur la Partners Platform | `DOSSIER` | coller le lien du message public dans « Public Verification Link » ; choisir Regular Pass (jusqu'à 5 jours) ou Fast Pass (24 h, 1 000 USD, non remboursable, sans garantie d'approbation) |
| J0+1 | À réception de l'identifiant `CLXXXXX`, **répondre à son propre message public** avec cet identifiant | `ÉDITEUR` | étape 3 de la vérification ; sans elle, le dossier n'est pas examiné |
| J0+2 | Dépôt **CoinGecko — chaîne (asset platform)** par **ticket de support générique**, pas par le formulaire | `DOSSIER` | 7 informations exigées : Name, Website URL, Explorer, Docs, **ChainList URL**, Logo, Current Token Listing. Aucun délai ni critère n'est publié pour ce guichet |
| J0+2 | Dépôt **CMC — formulaire 1**, `ticket_form_id=360000493112`, avec Annex C et Annex M | `DOSSIER` | **une demande déposée sur la mauvaise option est jetée, pas mise en attente**. Le formulaire 2 est celui des places d'échange et des chaînes |
| J0→J0+7 | Veille anti-usurpation quotidienne (§ 5) | `MODÉRATION` | l'ouverture d'un marché est le moment où les faux comptes apparaissent |

**Atout à ne pas gaspiller :** au 2026-09-02, `api.coingecko.com/api/v3/search?query=BOSA`
retourne une liste `coins` **vide** — aucun conflit de ticker détectable. Le conflit de nom ou
de ticker figure parmi les motifs de rejet écrits.

**Champ déjà satisfait :** la « ChainList URL » exigée pour la chaîne est disponible —
`chainid.network/chains.json` contient l'entrée chainId **26262**, nom « Coinbosa Chain »,
`nativeCurrency` Coinbosa / BOSA / 18, RPC et explorateur déclarés (vérifié le 2026-09-02 ;
2 745 chaînes). *Réserve :* le champ `standard` de l'explorateur vaut **`none`**, c'est-à-dire
que la conformité EIP-3091 des URL n'est pas déclarée. Aucune source officielle n'en fait un
motif de rejet, et l'impact réel est inconnu.

**Ce qu'on n'attend pas de cette semaine :** un rang, un prix affiché, une capitalisation. Voir
§ 0. Le dire à l'avance en interne évite de faire du bruit sur un silence normal.

**Interdits de la semaine :** aucune demande dupliquée, aucune relance de statut — CMC écrit
« DO NOT submit duplicate requests or repeatedly ask for status updates as it will add to the
queue and delay the process ».

#### Signes de réussite de la phase B

- Un identifiant de demande obtenu par guichet, et l'identifiant CoinGecko publié en réponse au
  message public.
- Le marché tient une semaine sans intervention manuelle.
- La chaîne n'a pas connu d'arrêt non déclaré (l'écart quotidien de 9 à 12 s entre 04:17 et
  04:23 UTC, dû à l'arrêt propre planifié `coinbosa-journal.timer`, est **annoncé à l'avance**
  plutôt que découvert dans les journaux de surveillance de la place `[D§10]`).
- Zéro affirmation invérifiable publiée.
- Le registre des faux comptes est ouvert et tenu.

---

### Phase C — Les 90 jours suivants

**Objectif :** transformer une fiche en dossier tenable, et faire exister ce qui manque
réellement — pas mieux le raconter.

| # | Action | Qui | Ce qui la débloque | Signe de réussite |
|---|---|---|---|---|
| C1 | **Second validateur** en production | `CHAÎNE` | jalon 3 de la feuille de route ; matériel sur un hébergeur distinct | le champ `miner` de la chaîne porte **deux producteurs distincts** ; un nœud coupé, la chaîne continue |
| C2 | **Audit externe engagé** | `ÉDITEUR` | devis, à fournir. Priorité fixée par le projet : vérifier qu'**aucune fonction du chemin de consensus ne peut échouer** `[D§13]` | lettre de mission publiée, avec son périmètre — en disant qu'il couvre **1 des 4 contrats système déployés**, les trois autres étant du bytecode hérité sans source |
| C3 | **Distribution réelle hors du projet** | `ÉDITEUR` | décision. C'est le **seul** levier sur l'offre en circulation ; aucun paiement ne l'achète, CMC refusant tout paiement lié au rang | le nombre d'adresses détentrices hors des 15 adresses du projet est supérieur à 0 et croît, mesurable par n'importe qui |
| C4 | **Note de réseau mensuelle** chiffrée (§ 4) | `CHAÎNE` écrit, `RÉDACTION` publie | rien | trois notes publiées à 90 jours, chaque chiffre reproductible par la commande citée |
| C5 | Mise à jour de l'offre **une fois seulement** le jeton coté et échangé | `DOSSIER` | cotation effective ; CoinGecko refuse toute mise à jour d'offre avant | demande déposée sur le bon guichet (CMC formulaire 4, gratuit et non accélérable par paiement) |
| C6 | Raccorder **un** produit de l'écosystème au paiement en BOSA | `ÉDITEUR` | choix du produit — Academy, VPN ou Omni AI ; le raccordement n'est annoncé que lorsqu'il fonctionne `[R]` | un paiement réel, vérifiable sur la chaîne |
| C7 | Décider quoi faire d'un silence | `DOSSIER` | la règle CoinGecko des 2 semaines | si aucune cotation à 2 semaines : **ne pas resoumettre**, corriger le défaut identifié, et se contenter de la présence GeckoTerminal — que CoinGecko désigne lui-même comme la position de repli d'un jeton refusé |

**Rappel sur les dépendances externes d'une page de chaîne :** les deux chiffres affichés sur
une page de chaîne CoinGecko viennent de tiers — le **volume** de GeckoTerminal, la **TVL** de
**DefiLlama**, qui exige que la chaîne figure dans `api.llama.fi/v2/chains` avec un `gecko_id`
correspondant, à demander à l'équipe DefiLlama **avant** la demande CoinGecko. Deux dossiers de
plus, à ouvrir en phase C et pas avant.

---

## 4. Le contenu

**La règle unique, qui remplace une charte éditoriale :** *aucun chiffre publié sans la
commande qui le reproduit.* Le projet a déjà cette discipline dans son dossier de cotation ; il
s'agit de l'appliquer à la communication.

| Quoi | Où | Fréquence | Qui produit |
|---|---|---|---|
| **Note de réseau** — temps de bloc mesuré, disponibilité, nombre de validateurs, réconciliation de l'offre au wei, incidents, chacun avec sa commande | site + Telegram + X | mensuelle | `CHAÎNE` écrit, `RÉDACTION` publie |
| **Journal des corrections** — toute correction d'un document publié, datée et motivée. Le format existe déjà dans `TOKENOMICS.md` | site | à chaque correction | `RÉDACTION` |
| **Dossier de cotation publié** | site, en FR et EN | une fois, puis à chaque mesure nouvelle | `DOSSIER` |
| **Guide d'intégration** corrigé (RPC public, limites de lot 50, corps 32 Ko, plage `eth_getLogs` 5 000 blocs, `finalized` inutilisable) | dépôt + site développeurs | une fois, puis à chaque changement | `CHAÎNE` |
| **Fiches produit honnêtes** — pour chacun des cinq produits, ce qui fonctionne et ce qui ne fonctionne pas. Card : « pas opérationnelle, aucun sponsor BIN à ce jour » | site, page écosystème | une fois, puis à chaque changement d'état | `RÉDACTION` |
| **Module Academy « vérifier une chaîne soi-même »** | Academy | trimestriel | `ACADEMY` |
| **Réponses techniques** | issues GitHub uniquement | au fil de l'eau | `CHAÎNE` |

**Langues.** FR et EN à chaque publication — les formulaires, les guichets et les agrégateurs
sont en anglais, et la version anglaise n'est pas une traduction de courtoisie mais la pièce du
dossier. AR, ES, PT et ZH suivent : le site les porte déjà.

**Ce qu'on ne produit pas :** analyses de prix, comparaisons avec d'autres chaînes, sessions de
questions-réponses sans contenu technique, visuels annonçant un produit non livré.

---

## 5. La communauté

### 5.1 Où elle se construit

| Canal | Statut | Rôle |
|---|---|---|
| `t.me/Coinbosaofficial` | existant, **seul canal de discussion officiel** déclaré par le site | discussion générale, annonces |
| `github.com/Coinbosa/coinbosa-chain` | existant | **toute** question technique |
| Compte X officiel | **à créer (A2)** | exigé par les guichets ; annonces uniquement |
| `/.well-known/security.txt` | existant | **seul** canal de signalement de faille |

La page « à propos » du site pose déjà la règle qui vaut politique de sécurité : *« Un compte,
un groupe ou un site qui se présente comme officiel sans figurer ici ne l'est pas. »* La tenir à
jour, à la minute où un canal est créé, est une mesure de sécurité — pas une tâche de
communication.

### 5.2 Comment on la modère

- **Trois interdits de canal**, valables aussi pour les membres de l'équipe : pas de conseil
  d'achat, pas de commentaire de prix, pas de promesse de rendement.
- **Message épinglé permanent**, repris mot pour mot du site : *« Aucun membre de l'équipe ne
  vous demandera jamais votre phrase de récupération ni votre clé privée. »*
- **Une faille se signale par `security.txt`**, jamais dans le groupe : un signalement public
  expose le défaut avant sa correction.
- **Qui :** `MODÉRATION`, au moins une personne nommée, avec un remplaçant. Un canal de milliers
  de personnes sans modérateur nommé est un canal hostile.
- **Escalade :** tout ce qui touche à l'offre, aux clés ou au consensus remonte à `ÉDITEUR`
  avant réponse.

### 5.3 Détecter les faux comptes — les vecteurs propres à ce projet

Les règles génériques ne servent à rien ici. Voici les quatre vecteurs que ce projet expose
réellement, et la détection associée.

| Vecteur | Pourquoi il est crédible **ici** | Détection |
|---|---|---|
| **Un « jeton BOSA » sur une chaîne majeure** | le jeton SPL `8Uyvx…yFaf` **existe toujours** sur Solana, son offre est quasi intacte, **4,00 % sont détenus hors du portefeuille projet**, et son `mintAuthority` est **active** : de nouvelles unités authentiques peuvent être créées à tout moment `[D§8]` | surveiller `getTokenSupply` sur le mint et l'apparition de paires DEX portant ce jeton ; A8 supprime le vecteur à la racine |
| **Faux portail de migration** | `docs/MIGRATION.md` existe et est cité publiquement — un faux portail « migrez vos jetons » a un prétexte tout prêt | veille sur les noms de domaine contenant « coinbosa » + « migration » / « claim » ; rappeler qu'aucune migration ne demande jamais de phrase de récupération |
| **Offre de « staking BOSA »** | PoBS est **spécifié et non déployé** — 0 octet aux trois adresses d'enjeu `[POBS.md]` | **règle absolue et publiable : toute offre de mise en jeu de BOSA est frauduleuse par construction aujourd'hui.** N'importe qui peut le vérifier avec `eth_getCode` |
| **Faux compte X** | le compte officiel n'existe pas encore | le créer **avant** la visibilité, pas après ; publier la liste datée des comptes officiels |

**Cadence de veille :** hebdomadaire avant J0, **quotidienne** la semaine de J0, hebdomadaire
ensuite. Requêtes minimales sur X, Telegram et un moteur de recherche : `coinbosa`, `BOSA`,
`$BOSA`, `coinbosa airdrop`, `coinbosa migration`, `coinbosa staking`, `coinbosa presale`.

**Registre :** `MODÉRATION` tient une liste datée des signalements et des comptes usurpateurs,
publiée sur le site. *Point juridique : les démarches de retrait auprès des plateformes et
toute action sur la marque sont à soumettre à un conseil.*

---

## 6. Ce qu'on ne fait pas, et pourquoi

Cette section protège le projet. Chaque interdit porte la raison qui le rend non négociable.

| Interdit | Raison, sourcée |
|---|---|
| **Aucune promesse de rendement** | la rémunération d'un validateur est exactement la somme des frais des transactions incluses ; sans trafic elle est **nulle**. Publier un taux serait faux `[T]` |
| **Aucune projection de prix, aucune capitalisation implicite** | BOSA n'a pas de marché ; la formule publiée des deux agrégateurs donne une offre en circulation de 0 `[D§3]`. Toute capitalisation annoncée serait construite sur un chiffre que les agrégateurs eux-mêmes ne retiennent pas |
| **Aucun achat d'influence non déclaré** | CMC ne mandate **aucun** prestataire externe et publie une « Hall of Shame » nominative des services promettant une cotation, en précisant : « If you are scammed by such services, we will not be in a position to recover your funds ». CoinGecko : la cotation est gratuite et « aucun représentant ne vous demandera jamais de frais de cotation ». **Seuls trois paiements sont officiels** — Fast Pass, Express Listing, CMC Priority — et aucun ne garantit l'approbation. S'ils sont engagés, ils sont inscrits au budget et déclarés en interne |
| **Aucun volume artificiel** | « Projects that attempt to manipulate or artificially inflate their figures will be permanently disqualified from the rankings » ; la faible liquidité et l'activité suspecte sont des motifs de délisting `[CMC]` |
| **Aucune récompense promise pour participer, s'inscrire ou parrainer** | rémunérer un travail livré depuis une allocation finie est une opération ordinaire ; promettre un gain attaché à la participation en rapproche la qualification. *Point juridique : à soumettre à un conseil* `[DEC D11]` |
| **Aucune revendication de décentralisation, d'audit ou de finalité** | un ingénieur appelle `getMiningValidators()` et voit un tableau d'un élément ; `finalized` renvoie le bloc 0 ; aucun audit n'existe `[DEC D10]`, `[D§9]`, `[D§13]` |
| **Aucune cotation sur la seule place du groupe présentée comme un marché** | « Projects traded only on self-serviceable centralized/decentralized exchanges may be rejected due to security concerns » `[CG]` |
| **Aucune soumission avant que les documents publics soient exacts** | garantie contractuelle de véracité et **dépublication discrétionnaire** en cas d'inexactitude `[CG Terms & Conditions for Listing, 12 août 2025]` ; « Be truthful… may render your submission inadmissible » `[CMC]` |
| **Aucune demande dupliquée, aucune relance de statut** | allonge la file et retarde l'examen `[CMC]` ; motif de rejet « demande dupliquée ou spam » `[CG]` |
| **Aucune date annoncée qui ne soit tenable** | règle interne préexistante : « une échéance annoncée puis manquée vaut moins que rien » `[G§7]` |

---

## 7. Le budget

Deux natures de postes, à ne pas mélanger : ce qui a un **tarif publié** (repris tel quel, lu
le 2026-09-02) et ce qui exige un **devis** (laissé vide plutôt qu'estimé).

### 7.1 Postes à tarif publié

| Poste | Montant publié | Incompressible ? |
|---|---|---|
| Cotation CoinGecko (coin, chaîne), CoinMarketCap, GeckoTerminal | **0 USD** — « Listing on GeckoTerminal is FREE » ; CoinGecko et CMC : cotation gratuite | — |
| Mise à jour de l'offre en circulation (CMC formulaire 4) | **0 USD**, et **non accélérable par paiement** : « we do not accept payment for rank-related updates » | — |
| CoinGecko **Fast Pass**, cotation de coin | **1 000 USD** par demande (200 USD pour une mise à jour), non remboursable, « ne garantit pas l'approbation » | **non** |
| GeckoTerminal **Express Listing** | **deux montants publiés qui se contredisent** : « Fee: USD 15 000 / ~7 days » (formulaire courant) et « à partir de 10 000 USD, délai garanti de 10 jours » (article du 2026-05-24). Les deux sont cités, aucun n'est tranché | **non** |
| CMC Priority **C1** (coin) | **5 000 USD**, ETA ~24 h ouvrées — **inapplicable tant que BOSA n'est pas coté sur une place alimentant l'API de CMC** | **non**, et sans objet aujourd'hui |
| CMC — intégration de chaîne | **80 000 USD** (bundle DEXScan + DEX) ou **20 000 USD** (UTM tracking) ; page de classement de chaîne **50 000 USD**. Aucune voie gratuite d'intégration de chaîne n'est décrite | **non** |
| CMC — badge d'audit de contrat | **50 000 USD / an** — produit d'affichage, **pas** une condition de cotation | **non** |

**Contradiction à ne pas trancher dans ce plan.** Le service Express affirme « Guaranteed
listing on CoinGecko once GT data is live » sur sa page commerciale, tandis que les conditions
du même service écrivent qu'il « ne garantit l'approbation d'aucune soumission ». Les deux
versions sont citées ; aucune n'est retenue comme un fait.

**Délais publiés, également contradictoires** : 5 jours ouvrés (article 22611791394585), 5 à
10 jours ouvrés (article 41889080628889), « jusqu'à 3 mois en raison du retard accumulé »
(formulaire courant). Ne rien promettre en interne sur cette base.

### 7.2 Postes à devis — montants à fournir par l'éditeur

| Poste | Contenu | Incompressible ? |
|---|---|---|
| **Second serveur et redondance RPC** | une machine distincte, un nœud d'archive, un point WebSocket. Le script est écrit et réversible ; il reste l'hébergement | **oui** — un serveur unique porte aujourd'hui la chaîne, le RPC, l'explorateur et le site `[D§12]`. Si ce serveur tombe pendant l'examen d'une candidature, le critère « working block explorer » tombe avec lui |
| **Indexation de l'explorateur + points d'accès d'offre** | historique par adresse, richlist, deux endpoints JSON en HTTPS sans authentification | **oui** — exigé par l'Annex C de CMC et par la méthode de vérification d'offre de CoinGecko |
| **Construction du marché** | DEX (fork Uniswap V2 de préférence), stablecoin BRC20, pool approvisionnée, verrou de LP | **oui** si la voie A est retenue — sans marché, aucune candidature n'est recevable (§ 0) |
| **Liquidité initiale de la pool** | montant en BOSA et en stablecoin | **oui**, et **aucun seuil chiffré n'est publié par les agrégateurs** : les deux écrivent que leurs critères de liquidité ne sont pas divulgués. Tout chiffre trouvé ailleurs serait inventé |
| **Audit externe** | périmètre : `CoinbosaValidatorSet.sol` (déployé) et `BRC20.sol` (non déployé) — soit **1 des 4 contrats système déployés** | **oui**, mais pas au titre des agrégateurs : aucun n'en fait une condition de cotation. Il l'est au titre de la règle du projet — un `revert` sur le chemin de consensus arrête la chaîne, et aucune transaction corrective n'est minable `[D§13]` |
| **Modération et veille** | au moins une personne nommée, plus un remplaçant | **oui** |
| **Rédaction et maintien des 6 langues** | le site les porte déjà ; le coût est celui du maintien | **oui** pour FR et EN, qui sont des pièces de dossier |

**Ce qui n'est pas chiffrable ici :** aucune donnée financière ne figure dans le dépôt —
trésorerie disponible, dépenses engagées, effectif et coût de l'équipe. **À fournir par
l'éditeur.**

**La ligne à retenir :** *aucun montant de ce tableau n'achète une cotation.* Les seuls
paiements officiels achètent du délai, et aucun ne garantit l'approbation. Le poste le plus
rentable du budget reste **A1 — corriger les documents publics**, dont le coût est du temps de
rédaction.

---

## 8. Ce que ce plan ne peut pas trancher

Trois manques d'information, et trois constats que je signale plutôt que de les recopier.

### 8.1 Informations manquantes — à fournir par l'éditeur

1. **L'Academy n'est mesurée nulle part.** « Plusieurs milliers d'étudiants » est la seule
   donnée du dépôt : ni effectif, ni pays, ni plateforme, ni base de contacts, ni base légale
   de réutilisation. Le segment le plus solide du plan est donc décrit sans un chiffre.
2. **Aucune donnée financière.** Trésorerie, dépenses engagées, effectif, coûts : rien dans le
   dépôt. Le budget ne peut être qu'une structure de postes assortie de tarifs externes.
3. **NextFuture est un angle mort.** La place d'échange de l'écosystème est « en construction »
   `[R]` sans propriétaire, ni statut, ni juridiction documentés — alors que c'est le premier
   marché naturel de BOSA et **précisément le type de marché que les deux agrégateurs
   escomptent**. *Point juridique : à soumettre à un conseil.*

À quoi s'ajoute une vérification à faire par l'éditeur : **existe-t-il déjà un compte X,
Facebook ou Instagram officiel ?** Le dépôt n'en référence aucun et la page des canaux
officiels du site n'en liste aucun.

### 8.2 Constats de la recherche que je ne reprends pas tels quels

1. **« Un explorateur maison est admissible. »** Exact au sens littéral — aucune source
   n'exige de marque tierce. Mais la méthode de CoinGecko suppose « un point d'accès API de
   l'explorateur » pour lire les soldes verrouillés, et l'explorateur Coinbosa **n'indexe
   rien** : ni historique par adresse, ni vérification de source `[README]`. Qu'un explorateur
   sans index satisfasse le critère « working block explorer » **n'est pas établi**. Je le
   traite comme un risque, pas comme un acquis.
2. **« Verrouiller la liquidité et passer en multi-signatures adresse le motif rug-pull. »**
   Vrai pour le LP. **Faux comme raccourci pour la trésorerie** : le projet a décidé, avec des
   raisons vérifiées `[G§6]`, de ne pas passer la trésorerie sous multi-signatures avant
   cotation — et l'exécuter dans l'urgence créerait un risque supérieur et irréversible. Les
   deux opérations sont distinctes et ce plan les sépare.
3. **« Une pool sur GeckoTerminal fera vivre la page du coin natif. »** Non établi. Les
   agrégateurs listent les actifs enveloppés sur des pages **séparées** de l'actif natif, et
   aucune source lue ne dit comment le prix d'un coin natif est dérivé d'une pool WBOSA. À
   faire confirmer par ticket **avant** d'engager le déploiement du DEX, pas après.

**Périmètre temporel.** Tous les tarifs et délais cités ont été lus le **2026-09-02** et
peuvent changer sans préavis. Ils sont à revérifier avant toute soumission et avant tout
engagement de dépense.

---

## Sources

**Dépôt** — `WHITEPAPER.md` · `TOKENOMICS.md` · `DOSSIER-COTATION.md` · `ROADMAP.md` ·
`README.md` · `POBS.md` · `GARDE-TRESORERIE.md` · `DECISIONS.md` · `docs/INTEGRATION.md` ·
`site/*.html` · `whitepaper/index.html`

**CoinGecko** — Methodology et Listing Criteria · Terms & Conditions for Listing (12 août 2025)
· How to List a New Cryptocurrency · How to Request a New Chain Listing (Asset Platform) ·
Verification Guide for Listing/Update Requests · CoinGecko Supply Methodology · Total
Supply/Circulating Supply API Endpoint Requirement · Why is my token not listed · How long does
it take · How to Preview List Tokens · How to Add a New Exchange · How to update TVL / Volume on
my Chain page · Fast Pass

**GeckoTerminal** — `about.geckoterminal.com/dex-chain-listing` et son formulaire · DEX Forks
supported · Integration API Standards v0.1 · DEX Express Listing · How do I know if my DEX/Chain
gets listed

**CoinMarketCap** — Listings Criteria (maj 2026-09-01) · Supply (Circulating, Total, Max) ·
CMC Priority · Ranking · Category-Specific Listings Criteria · Delisting Coins/Tokens with Zero
Volume · Annex C et Annex M (modèles officiels) · Self-Reporting Portal · formulaires 1, 2, 4, 7
et 8

**Mesures externes du 2026-09-02** — `chainid.network/chains.json` (entrée 26262 présente,
2 745 chaînes) · `api.geckoterminal.com/api/v2/networks` (aucun réseau « bosa ») ·
`api.coingecko.com/api/v3/asset_platforms` (465 plateformes, aucune en 26262) ·
`api.coingecko.com/api/v3/search?query=BOSA` (aucun coin)
