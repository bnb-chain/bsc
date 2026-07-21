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

## D3 — D'où viennent les récompenses de validateurs

**Ouvert.** Décision requise avant d'écrire le contrat d'enjeu.

Contrainte vérifiée dans le code amont — `consensus/parlia/parlia.go`, ligne 1428 :

```go
// No block rewards in PoA, so the state remains as is and uncles are dropped
```

**Parlia ne crée aucune monnaie.** Le revenu d'un validateur se limite aux frais de transaction,
redistribués depuis le solde système. Sur une chaîne à frais quasi nuls et à faible volume, ce
revenu tend vers zéro — et personne n'immobilise du capital pour un rendement nul.

Le modèle du livre blanc v2 — 2,5 % d'émission pour les validateurs plus 2,5 % pour la
soutenabilité — **n'est donc pas implémentable en l'état**.

Trois options, aucune gratuite :

| Option | Mécanisme | Coût |
|---|---|---|
| Réserve dédiée | une part des 700 M finance les récompenses | l'offre est finie : la réserve s'épuise |
| Émission par le client | patcher Parlia pour créer une récompense de bloc | modification du cœur du consensus |
| Frais réels | les validateurs vivent du volume, comme sur Ethereum | contredit l'argument des frais quasi nuls |

---

## D4 — Deux actifs portent le nom BOSA

**Ouvert.** Décision requise avant toute publication.

| | Coin natif | Jeton BRC20 |
|---|---|---|
| Rôle | paie le gas | actif applicatif |
| Décimales | 18, imposées par l'EVM | 10 |
| Offre | fixée au genesis | 700 000 000 |

Les 18 décimales du coin natif ne sont pas un choix : l'unité de base de l'EVM est le wei, et
cette valeur est câblée dans le calcul du gas comme dans les wallets.

Deux actifs homonymes aux décimales différentes sont une source de confusion pour les
utilisateurs, les intégrateurs et les places de cotation. Le point sera tranché avec la
tokenomique.

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
