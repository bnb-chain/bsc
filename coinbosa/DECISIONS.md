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

**Conséquence** — le code amont est en double licence : bibliothèque hors `cmd/` en LGPL-3.0,
binaires de `cmd/` en GPL-3.0. Coinbosa distribuant un client recompilé, l'obligation de
publier le code source correspondant s'appliquera dès qu'un binaire sera distribué à un tiers.

---

## D2 — Consensus par preuve d'enjeu

**Retenu.** Les validateurs immobilisent un enjeu pour entrer dans le consensus.

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

**Modèle de réconciliation retenu :** migration à **parité** (un jeton Solana pour un BOSA natif).
L'offre native de 700 000 000 se décompose en deux blocs : une **réserve de migration** égale au
montant de jetons Solana détenus par des tiers, créditée aux détenteurs à mesure qu'ils migrent,
et une **allocation projet** égale au reste, organisée selon les treize postes. Les pourcentages
des treize postes s'appliquent donc à l'allocation projet, pas au total.

**Anciens contributeurs :** leurs jetons sont **honorés comme ceux de tout détenteur**. La
migration à sens unique ne permet pas de reprendre des jetons dans un portefeuille tiers, et
aucune exclusion d'adresse n'est retenue.

**Partage établi :** sur les 500 000 000 de jetons Solana, **450 000 000 sont détenus par le
projet** sur le portefeuille `5pdFbZdyab9jQUnC2E4x9XGmLpAFNqoF4GyjEtpfedQf` (format base58 vérifié,
décodage 32 octets), et **50 000 000 par des tiers**. D'où :

- **réserve de migration : 50 000 000** BOSA, pour les détenteurs tiers ;
- **allocation projet : 650 000 000** BOSA, répartie selon les treize postes ;
- total : 700 000 000, vérifié on-chain.

**Non-double-comptage.** Les 450 000 000 de jetons Solana du projet ne sont pas migrés — le
projet reçoit son allocation au genesis. Ces 450 000 000 seront **retirés de la circulation sur
Solana**, publiquement, pour prouver que le projet ne migre pas ses propres jetons en plus de son
allocation.

**Conformité à ne pas éluder.** Le portail collecte des données personnelles (nom, prénom) et
opère un transfert de valeur : protection des données, obligations anti-blanchiment et
qualification de l'opération relèvent d'un conseil juridique préalable, pas postérieur.
