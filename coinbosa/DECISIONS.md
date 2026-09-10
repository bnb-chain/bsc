<div align="center">
  <img src="assets/coinbosa-logo.jpg" alt="Coinbosa" width="110" />

  # Journal des décisions
</div>

Les décisions structurantes du projet, avec leur justification et leurs conséquences. Une
décision qui change est amendée ici, jamais effacée : savoir ce qu'on a écarté et pourquoi
vaut autant que savoir ce qu'on a retenu.

---

## D1 — Fork de BNB Smart Chain plutôt que d'un autre client

**Retenu.** Le client dérive de `bnb-chain/bsc` v1.7.6.

Le livre blanc v2 décrivait AuRa sur le client Parity / OpenEthereum. Ce dépôt est **archivé
depuis le 6 novembre 2020** : bâtir dessus signifierait partir d'un logiciel mort, sans
correctif de sécurité.

### Point d'ancrage amont, épinglé par empreinte

Un tag peut être redéplacé ; une empreinte de commit, non. Le point d'ancrage exact est donc
consigné ici :

| | |
|---|---|
| Dépôt amont | `bnb-chain/bsc` |
| Version | `v1.7.6` |
| **Commit amont** | **`69b3758c81ec90bb827f93fda0c00f49ebf79e25`** |

Ce commit est présent dans l'historique de ce dépôt (`git cat-file -t 69b3758c…`) : la
filiation se vérifie, elle ne se croit pas sur parole.

**Écart total avec l'amont, sur le code du client** (`git diff 69b3758c..HEAD`, hors dossier
`coinbosa/` et hors CI) : `.gitignore`, `Makefile`, `README.md`, `build/ci.go`, et **une seule
ligne de code de consensus** —

```diff
  consensus/parlia/parlia.go
- defaultBlockInterval uint64 = 3000 // Default block interval in milliseconds
+ defaultBlockInterval uint64 = 5000 // Coinbosa : 5 s par bloc (livre blanc). BSC amont : 3000.
```

Autrement dit : le moteur de consensus est celui de l'amont, à un paramètre près. Tout
auditeur peut le vérifier en une commande, sans lire le reste du dépôt.

**Suivi de sécurité** — parce que la base est un logiciel tiers, la veille sur les avis de
`bnb-chain/bsc` fait partie de l'exploitation : à chaque avis publié, décider explicitement de
rebaser ou de rétroporter le correctif, et mettre à jour l'empreinte ci-dessus. Une empreinte
figée sans veille ne serait qu'une photo d'un logiciel qui vieillit.

**Conséquence** — le code amont est en double licence : bibliothèque hors `cmd/` en LGPL-3.0,
binaires de `cmd/` en GPL-3.0. Coinbosa distribuant un client recompilé, l'obligation de
publier le code source correspondant s'appliquera dès qu'un binaire sera distribué à un tiers.

---

## D2 — Consensus par preuve d'enjeu (visée, non atteinte)

**Retenu comme cible.** Les validateurs immobiliseront un enjeu pour entrer dans le
consensus. Au présent, ce n'est pas le cas : voir « État réel » plus bas.

Parlia est un consensus *Proof of Staked Authority* : enjeu immobilisé **et** nombre de places
borné. C'est le modèle de BNB Chain. Il se distingue d'une preuve d'enjeu ouverte comme celle
de Cardano, où le nombre de producteurs n'est pas plafonné.

**État réel** — le moteur sait le faire ; le contrat système, non. Pour débloquer la chaîne au
bloc 200, `CoinbosaValidatorSet` a été réduit à un set fixe modifiable par un gouverneur, sans
enjeu. La chaîne fonctionne donc aujourd'hui en preuve d'autorité de fait. Combler cet écart est
le jalon 1 de la [feuille de route](ROADMAP.md).

---

## D3 — Rémunération des validateurs par les frais de transaction

**Tranché.** Les validateurs sont rémunérés par les frais de transaction du réseau, et par rien
d'autre. Aucune part de l'offre n'est réservée aux récompenses, aucune émission n'est créée.

Contrainte vérifiée dans le code amont — `consensus/parlia/parlia.go`, ligne 1428 :

```go
// No block rewards in PoA, so the state remains as is and uncles are dropped
```

Parlia ne crée aucune monnaie. Les deux alternatives ont été écartées : un coffre pré-financé
aurait amputé l'offre distribuable et se serait épuisé ; une émission protocolaire aurait exigé
de modifier le cœur du consensus, faisant diverger Coinbosa de l'amont de façon irréversible et
rendant fausse l'affirmation d'offre fixe.

**Conséquence à assumer** — sans trafic, il n'y a pas de frais, donc pas de revenu. Le rendement
d'un validateur n'est pas faible au lancement, il est **nul**, et ne croît qu'avec l'usage réel.
Aucun validateur externe ne sera motivé économiquement avant que le volume existe. Le modèle du
livre blanc v2 — 2,5 % d'émission validateurs plus 2,5 % de soutenabilité — est abandonné.

**Interdit de rédaction** — aucun taux de rendement, actuel ou projeté, ne figure dans les
documents publics.

---

## D4 — Un actif unique : le coin natif

**Tranché.** BOSA est le coin natif de Coinbosa Chain. Le jeton BRC20 applicatif de
700 000 000 unités est retiré ; il ne sera pas distribué.

| | |
|---|---|
| Symbole | BOSA |
| Décimales | **18** |
| Offre | 700 000 000, fixée au genesis |

**Sur les 18 décimales.** Ce n'est pas un arbitrage mais une contrainte : l'unité de base de
l'EVM est le wei, valeur câblée dans le calcul du gas et dans tous les portefeuilles. Les
10 décimales n'étaient possibles que sur un jeton applicatif distinct. En choisissant l'actif
unique, elles deviennent sans objet.

**Coût du changement : nul.** Aucune unité n'a été distribuée à un tiers, il n'existe aucun
détenteur externe. Ce même changement après une première cotation aurait imposé une migration —
Polygon a mis environ un an pour MATIC vers POL, BNB Beacon Chain dix-huit mois.

Le changement est consigné dans [TOKENOMICS.md](TOKENOMICS.md) plutôt que substitué en silence :
le projet a communiqué antérieurement sur 700 000 000 à 10 décimales.

---

## D5 — Standard de jeton nommé BRC20

**Retenu.** *Bosa smart contRact 20*, conformément au livre blanc.

Un standard homonyme existe sur Bitcoin (inscriptions Ordinals), sans rapport technique. La
documentation précise systématiquement « BRC20 de Coinbosa » pour lever l'ambiguïté.

---

## D6 — Temps de bloc à 5 secondes

**Retenu.** Conformément au livre blanc.

Ce paramètre n'est pas lisible depuis le genesis : `ParliaConfig` est une structure vide depuis
la v1.7.6, et les champs `period` / `epoch` qu'on trouve dans les tutoriels sont ignorés. Le
temps de bloc est une constante Go sélectionnée par les hardforks.

**Conséquence** — le client a été patché (`defaultBlockInterval` de 3000 à 5000 ms) et **le
binaire officiel de BNB Chain ne convient plus** : ce dépôt doit être compilé.

---

## D7 — Pas de NFT

**Retenu.** Le standard BRC-721 mentionné dans le livre blanc v2 est écarté. Il ne sera
implémenté que si un besoin produit le justifie.

---

## D8 — Paiements : la volatilité est absorbée hors de la carte

**Retenu dans son principe.** Le processeur acceptera stablecoins, BOSA et autres actifs
volatils. La carte s'appuiera sur Stripe.

**Contrainte de conception** — entre l'autorisation d'une carte et le règlement au commerçant,
il s'écoule un à trois jours. Un actif volatil peut varier fortement dans cet intervalle, et
quelqu'un absorbe l'écart : le porteur, le commerçant ou l'émetteur. C'est pourquoi les
programmes de carte convertissent **au moment de l'autorisation**, et non au règlement.

Le stablecoin n'est donc pas un concurrent de BOSA dans ce montage : c'est la couche qui absorbe
la volatilité et rend BOSA dépensable chez un commerçant qui ne veut connaître que sa monnaie.

**À vérifier avant de s'engager** — les conditions d'éligibilité de Stripe Issuing pour un
programme adossé à de la crypto, et surtout la capacité des prestataires à prendre en charge un
actif vivant sur une **chaîne souveraine**. Les rampes fiat et les processeurs ne référencent
généralement que les chaînes majeures ; c'est le point de blocage le plus probable de tout
l'édifice, et il doit être levé avant d'engager des développements.

---

## D9 — Le projet antérieur est obsolète

**Retenu.** Le dossier `coinbosa blockchain` présent sur le poste de développement d'origine
décrit une pile incompatible : consensus `clique`, chainId `202603091`, coin natif `CBB`,
standard `CBS20`, blocs de 60 secondes, 3 validateurs.

Il n'a jamais été publié — aucun dépôt distant — et ne peut plus fonctionner : `clique` a été
retiré de geth, qui refuse désormais de démarrer sur un réseau non-PoS.

Il doit être archivé et marqué obsolète avant toute publication. Deux jeux de paramètres
contradictoires portant le même nom suffisent à faire rejeter un dossier.

---

## D10 — Ce que la documentation n'affirmera pas

**Retenu.** Aucune affirmation non mesurée n'entre dans les documents publics.

Sont explicitement écartés :

- **les 400 000 transactions par seconde** du livre blanc v2 — jamais mesuré. À titre de
  comparaison, les débits réellement observés en 2026 se comptent en dizaines à quelques
  milliers de transactions par seconde. Sur des blocs de 5 s, ce chiffre supposerait deux
  millions de transactions par bloc ;
- **« décentralisé », « résistant à la censure », « sans confiance »** tant qu'un seul
  validateur produit les blocs — un ingénieur interroge `getMiningValidators()` et voit un
  tableau d'un élément ;
- **« audité », « sécurité éprouvée »** en l'absence d'audit externe ;
- **« finalité »** — les clés BLS sont à zéro, le vote d'attestation est inactif. La finalité
  est probabiliste ;
- **« validateurs identifiés » présenté comme une garantie de sécurité.** Ronin comptait neuf
  validateurs identifiés et un seuil de cinq : 625 M$ ont été dérobés et l'attaque n'a été
  détectée que six jours plus tard. Douze validateurs recrutés et financés par l'éditeur ne
  sont pas douze validateurs — c'est un validateur avec douze clés.

Ce n'est pas de la prudence excessive. En droit européen, la responsabilité des dirigeants pour
une information trompeuse dans un livre blanc ne peut être limitée par aucune clause
contractuelle.

---

## D11 — Rémunération des contributeurs

**Retenu.** Les personnes qui contribuent au développement de la chaîne et des produits de
l'écosystème — y compris les volontaires — sont rémunérées pour le travail livré, sur les
allocations dédiées de l'offre (Développement, Technique, Recherche, Équipe, Sécurité et les
postes de recherche).

**Encadrement, pour une raison précise.** La rémunération est la contrepartie d'un travail
effectivement livré, jamais un gain attaché à la détention de jetons ni une récompense promise
pour avoir rejoint le projet. Cette distinction n'est pas cosmétique : promettre un gain en
échange de la simple participation, ou pour des tâches accomplies dans l'attente d'un jeton,
rapproche l'opération d'un contrat d'investissement au regard du droit américain. Rémunérer un
travail livré depuis une allocation finie est, au contraire, une opération ordinaire. La
formulation retenue dans le livre blanc respecte cette ligne.

Les allocations sont finies et inscrites au genesis ; aucune émission ne les reconstitue.

---

## D12 — Migration des détenteurs historiques (Solana et BNB Chain)

**Retenu.** Des jetons Coinbosa émis lors de phases antérieures existent sur Solana et sur BNB
Chain, détenus par des tiers. Un portail de migration leur permettra de les échanger contre du
BOSA natif.

**Conception :** migration à sens unique. Le détenteur dépose ses jetons historiques à une
adresse officielle publiée à l'avance, indique son adresse Coinbosa Chain, et reçoit du BOSA
natif ainsi que l'empreinte de la transaction comme preuve vérifiable. Le sens unique garantit
qu'aucun jeton n'existe deux fois. Spécification complète dans [docs/MIGRATION.md](docs/MIGRATION.md) ;
squelette du formulaire dans `portal/`.

**Correction de documentation qui en découle.** Le livre blanc affirmait qu'aucune unité n'avait
été distribuée à un tiers. C'était vrai du jeton BRC20 applicatif — jamais distribué — mais faux
si on l'entend des jetons historiques sur Solana et BNB Chain, qui eux ont des détenteurs. Le
livre blanc a été corrigé pour distinguer les trois : le contrat BRC20 abandonné, le coin natif
canonique, et les jetons historiques à migrer.

**Offre historique retenue : Solana uniquement.** 500 000 000 de jetons
(`8UyvxCoVXoVaftWzp7j9yo2sGL2HnHTFDV4capenyFaf`). Le jeton précédemment émis sur BNB Chain
n'existe plus, et l'adresse de contrat fournie était invalide : BNB Chain est écarté de la
migration.

**Partage établi, puis simplifié.** Le projet contrôle la **totalité des 500 000 000 de jetons
Solana**, consolidés sur le portefeuille `5pdFbZdyab9jQUnC2E4x9XGmLpAFNqoF4GyjEtpfedQf` (format
base58 vérifié, décodage 32 octets). Il n'y a **pas de détenteurs tiers** à migrer. D'où :

- **réserve de migration : 0** ;
- **offre native : 700 000 000** BOSA, revenant intégralement au projet, répartie selon les
  treize postes ;
- vérifié on-chain.

**Non-double-comptage.** Les 500 000 000 de jetons Solana ne sont pas migrés — le projet reçoit
son offre au genesis. Ils seront **retirés de la circulation sur Solana**, publiquement, pour
prouver l'absence de double compte.

**Cas résiduel.** Si un détenteur tiers apparaissait — par exemple un ancien contributeur
détenant encore des jetons —, sa migration serait honorée à parité et créditée depuis la réserve
stratégique, et non depuis une réserve dédiée. Aucune exclusion d'adresse n'est pratiquée.

**Conformité à ne pas éluder.** Le portail collecte des données personnelles (nom, prénom) et
opère un transfert de valeur : protection des données, obligations anti-blanchiment et
qualification de l'opération relèvent d'un conseil juridique préalable, pas postérieur.

### Addendum du 10 septembre 2026 — le retrait de circulation ne peut pas être exécuté

L'entrée ci-dessus reste telle qu'elle a été écrite ; le registre n'efface pas. Ce qui suit dit
ce qu'on sait depuis, et pourquoi la mesure annoncée n'aura pas lieu.

**Mesuré le 7 septembre 2026 sur Solana mainnet** (`getTokenSupply`, `getAccountInfo` et
`getTokenAccountsByOwner` sur `https://api.mainnet-beta.solana.com`) :

| | mesure |
|---|---|
| offre du jeton `8Uyvx…yFaf` | **499 999 940,39** unités (10 décimales), et non 500 000 000 |
| détenues par `5pdFbZ…edQf` | **479 990 400**, soit **96,00 %** |
| hors de ce portefeuille | **20 009 540,39 unités — 4,00 %**, chez qui nous l'ignorons |
| `mintAuthority` et `freezeAuthority` | **actives**, sur `3zADMByrBhWTnQETN2gv5Gt7jhQKyyprjLCLVVnv2Pkq` |

**Établi le 10 septembre 2026 :** l'éditeur n'a ni l'accès ni la propriété du portefeuille
`3zADMByrBhWTnQETN2gv5Gt7jhQKyyprjLCLVVnv2Pkq` qui porte les deux autorités. Trois conséquences,
et aucune n'est négociable :

- le projet **ne peut pas révoquer** ces autorités — on ne révoque pas ce qu'on ne détient pas ;
- le projet **ne peut pas garantir l'offre** de ce jeton : elle peut être regonflée à tout instant
  par le détenteur de la clé, et n'importe quel compte peut être gelé ;
- le projet **ne peut pas retirer de la circulation ce qu'il ne détient pas** — ni les 4,00 %, ni
  ce qui serait frappé après coup. La mesure de transparence annoncée plus haut est donc
  inexécutable, et la preuve d'absence de double compte ne peut pas venir de là.

**Ce que cela ne change pas.** L'offre native de BOSA sur Coinbosa Chain vaut 700 000 000, fixée
au bloc de genèse, et cela reste vérifiable au wei près : hash du bloc 0 identique à la référence
publiée, aucune émission, base de frais nulle, `scripts/audit-argent.js` la recompte et la CI le
relance chaque jour. Les deux jetons sont **indépendants** : rien n'est migré, aucune part des
700 000 000 BOSA n'est adossée au jeton Solana, et ce qui arrive sur Solana ne peut ni créer ni
détruire un seul BOSA.

**Ce que D12 devient.** Le jeton SPL `8Uyvx…yFaf` est traité comme un **artefact historique, hors
du contrôle du projet** : le projet n'assume ni son offre, ni sa valeur, ni les actes de qui
détient ses autorités. [TOKENOMICS.md](TOKENOMICS.md) est déjà rédigé dans ce sens.

**Point laissé ouvert.** L'autorité de frappe étant active, des unités peuvent apparaître après
coup. Avant toute ouverture du portail pour un cas résiduel, il faudra donc trancher explicitement
ce qu'on accepte de créditer depuis la réserve stratégique : cela ne peut pas être automatique.

**Réversible, sous condition.** L'éditeur indique ne pas être propriétaire de ce portefeuille *à
ce jour*. Si le contrôle en était acquis, la conduite à tenir serait de révoquer les deux autorités
(`SetAuthority` vers `null`) et de publier ici la signature de la transaction. Tant que ce n'est
pas fait, cet addendum reste vrai.
