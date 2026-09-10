<div align="center">
  <img src="assets/coinbosa-logo.jpg" alt="Coinbosa" width="120" />

  # Économie du jeton BOSA
</div>

---

## Un seul actif

BOSA est le **coin natif** de Coinbosa Chain. Il n'existe pas de second actif portant ce nom.

| | |
|---|---|
| Nom | Coinbosa |
| Symbole | BOSA |
| Décimales | **18** |
| Offre totale | **700 000 000** BOSA |
| Émission | aucune — l'offre est fixée au bloc de genèse |

**Sur les 18 décimales.** Ce n'est pas un choix, c'est une contrainte de l'EVM : l'unité de base
est le wei, et cette valeur est câblée dans le calcul du gas comme dans tous les portefeuilles.
Un coin natif ne peut pas en avoir un autre nombre. Une version antérieure de ce projet prévoyait
10 décimales sur un jeton applicatif distinct ; ce jeton a été retiré au profit d'un actif unique,
et la question ne se pose plus.

**Sur l'absence d'émission.** Le moteur de consensus ne crée aucune monnaie — le code amont est
explicite, `consensus/parlia/parlia.go` : `// No block rewards in PoA`. L'offre de 700 000 000
BOSA est donc définitive. Aucun mécanisme du protocole ne peut l'augmenter.

---

## Offre native et jetons historiques

L'offre native est de **700 000 000 BOSA**, fixée au genesis, et revient **intégralement au
projet**, répartie selon les treize postes ci-dessous.

### Les jetons historiques sur Solana — ce que la mesure dit

Des jetons Coinbosa historiques existent sur **Solana**, mint
`8UyvxCoVXoVaftWzp7j9yo2sGL2HnHTFDV4capenyFaf`. *(Des jetons avaient aussi été émis sur
BNB Chain ; ce jeton n'existe plus.)*

Ce paragraphe affirmait qu'ils étaient « détenus dans leur totalité par le projet » et
qu'« il n'y a donc pas de détenteurs tiers à migrer ». **Mesuré le 2026-09-07 sur Solana
mainnet, ce n'est pas exact** — et il vaut mieux le lire ici que le découvrir en une
requête :

| | mesure |
|---|---|
| offre du jeton | **499 999 940,39** unités (décimales : 10) |
| détenues par le portefeuille projet `5pdFbZ…edQf` | **479 990 400**, soit **96,00 %** |
| **hors de ce portefeuille** | **20 009 540,39 unités — 4,00 %** |
| autorité de frappe (`mintAuthority`) | **active**, sur `3zADMByrBhWTnQETN2gv5Gt7jhQKyyprjLCLVVnv2Pkq` |
| autorité de gel (`freezeAuthority`) | **active**, sur la même adresse |

*Reproductible : `getTokenSupply`, `getAccountInfo` et `getTokenAccountsByOwner` sur
`https://api.mainnet-beta.solana.com`.*

**Nous ignorons si les 4,00 % restants appartiennent à d'autres portefeuilles du projet ou
à des tiers.** Tant que ce n'est pas établi, il ne faut pas écrire qu'il n'y a pas de
détenteur tiers.

**Et l'offre mesurée aujourd'hui n'est pas un plafond** : l'autorité de frappe étant active
(voir plus bas), rien n'empêche que ce nombre augmente demain.

### Le jeton Solana n'est pas sous le contrôle du projet

**C'est le fait le plus important de cette section, et il a été établi le 10 septembre 2026 :
l'éditeur n'a ni l'accès ni la propriété du portefeuille
`3zADMByrBhWTnQETN2gv5Gt7jhQKyyprjLCLVVnv2Pkq` qui détient les deux autorités.**

Trois conséquences en découlent, et aucune n'est négociable :

1. **Le projet ne peut pas révoquer ces autorités.** On ne révoque pas une autorité qu'on ne
   détient pas. La « révocation publique » qu'annonçait la version précédente de ce document
   n'est pas une action que l'éditeur peut entreprendre.

2. **Le projet ne peut donc pas garantir l'offre du jeton Solana.** Tant que l'autorité de
   frappe existe, son détenteur peut créer de nouvelles unités quand il veut ; l'autorité de
   gel lui permet en outre de bloquer n'importe quel compte. **Aucune promesse d'offre fixe
   ne peut être faite sur ce jeton**, et ce document n'en fait plus.

3. **Le projet ne peut pas retirer de la circulation ce qu'il ne détient pas.** Ni les
   20 009 540,39 unités hors de son portefeuille, ni celles qui pourraient être frappées
   après coup. La phrase « aucun jeton ne sera compté deux fois » ne peut pas être tenue, et
   elle est retirée.

> ### Ce que cela ne change pas — et c'est l'essentiel
>
> **L'offre native de BOSA sur Coinbosa Chain n'est pas affectée.** Elle vaut
> 700 000 000 BOSA, fixée au bloc de genèse, et ce fait est vérifiable par n'importe qui en
> quelques requêtes : le hash du bloc 0 est identique à la référence publiée, le moteur de
> consensus ne crée pas de monnaie, la base de frais vaut zéro, et la somme des comptes
> retombe sur l'offre déclarée au wei près. `scripts/audit-argent.js` le recompte, et la CI
> le relance chaque jour.
>
> **Les deux jetons sont indépendants.** Rien n'est migré, aucune part des 700 000 000 BOSA
> n'est adossée au jeton Solana, et ce qui arrive sur Solana ne peut ni créer ni détruire un
> seul BOSA.

**Le jeton SPL `8Uyvx…yFaf` doit donc être traité comme un artefact historique, hors du
contrôle du projet.** Il ne doit pas être confondu avec BOSA sur Coinbosa Chain, et le projet
n'assume ni son offre, ni sa valeur, ni les actes de qui détient ses autorités.

Cette situation peut évoluer : l'éditeur indique ne pas être propriétaire de ce portefeuille
**à ce jour**. Si le contrôle en était acquis, la conduite à tenir serait de révoquer les deux
autorités (`SetAuthority` vers `null`) et de publier ici la signature de la transaction. Tant
que ce n'est pas fait, ce paragraphe reste vrai.

Ces jetons Solana **ne sont pas migrés** : le projet reçoit son offre native directement au
genesis, et rien n'est prélevé sur les 700 000 000 BOSA au titre d'une migration. L'offre
native est indépendante de ce qui existe sur Solana.

Le [portail de migration](docs/MIGRATION.md) reste disponible pour le cas résiduel d'un détenteur
tiers qui apparaîtrait — par exemple un ancien contributeur —, crédité alors depuis la réserve
stratégique plutôt que depuis une réserve dédiée.

---

## Répartition de l'offre

*Pourcentages appliqués à l'offre native de 700 000 000 BOSA.*

| Poste | Part | BOSA | Objet |
|---|---|---|---|
| **Développement** | 20 % | 140 000 000 | construction du réseau, des contrats et des applications |
| **Technique** | 10 % | 70 000 000 | infrastructure, nœuds, exploitation, outillage |
| **Recherche** | 10 % | 70 000 000 | travaux de recherche du protocole |
| **Équipe** | 10 % | 70 000 000 | rémunération des contributeurs |
| **Fonds financier** | 10 % | 70 000 000 | fonds de dépôt adossé à Coinbosa Card |
| **Fonds de liquidité** | 10 % | 70 000 000 | tenue de marché et profondeur de carnet |
| **Recherche IA** | 10 % | 70 000 000 | travaux d'intelligence artificielle |
| **Recherche finance et fintech** | 5 % | 35 000 000 | travaux sur les usages financiers |
| **Sécurité** | 3 % | 21 000 000 | sécurisation du réseau et réponse aux incidents |
| **Audit** | 2 % | 14 000 000 | audits externes du code et des contrats |
| **Événements et formation** | 2 % | 14 000 000 | formation et rencontres de l'écosystème |
| **Distribution publique et communauté** | 5 % | 35 000 000 | mise en circulation initiale |
| **Réserve stratégique** | 3 % | 21 000 000 | imprévus, partenariats, opportunités |
| **Total** | **100 %** | **700 000 000** | |

> Les treize postes bouclent à 100 % de l'offre. Les deux derniers — distribution
> publique et communauté (5 %) et réserve stratégique (3 %) — complètent les onze premiers ; le
> premier parce qu'une part mise en circulation est nécessaire à l'existence d'un marché, le
> second parce qu'une trésorerie sans marge oblige à puiser dans un poste déjà affecté.

---

## Rémunération des validateurs

**Les validateurs sont rémunérés par les frais de transaction du réseau. Il n'existe aucune autre
source.**

Aucune part de l'offre n'est réservée aux récompenses de validation, et aucune émission ne peut
être créée. Le revenu d'un validateur est exactement la somme des frais des transactions qu'il
inclut, redistribuée depuis le solde système à chaque bloc.

**Conséquence à connaître avant d'engager un validateur externe :** sans trafic, il n'y a pas de
frais, donc pas de revenu. Le rendement n'est pas faible au lancement, il est **nul**. Il croît
avec l'usage réel du réseau, et avec rien d'autre.

Ce document ne publie donc **aucun taux de rendement**, ni actuel ni projeté. Le revenu d'un
validateur se calcule à partir de données publiques :

```
revenu = Σ (frais des transactions incluses)
```

Chacun peut le vérifier bloc par bloc sur le réseau.

---

## Ce que ce document ne promet pas

Aucun rendement, aucune appréciation de valeur, aucun engagement de cotation.

BOSA sert à payer les frais de transaction du réseau et à participer au consensus. Toute autre
utilité — paiement dans les produits de l'écosystème, adossement de la carte, règlement chez des
commerçants — dépend de raccordements qui **ne sont pas réalisés** à la date de ce document.
Chacun sera annoncé lorsqu'il fonctionnera, et pas avant.

---

## Ce qui reste à trancher

**Les calendriers de blocage.** Chaque poste doit recevoir une date de mise à disposition et une
durée d'acquisition. Un poste « Équipe » disponible immédiatement est un signal d'alarme pour
toute place de cotation ; l'usage est un blocage initial d'au moins douze mois, suivi d'une
libération progressive.

**Les adresses de détention.** Chaque poste doit avoir son adresse, publiée, vérifiable sur
l'explorateur. Aujourd'hui l'offre n'est pas répartie : elle est concentrée, ce qui est
l'obstacle numéro un du dossier devant tout le reste.

**Le passage en multi-signatures.** Les postes significatifs ne doivent pas dépendre d'une clé
unique. Tant que la même clé contrôle l'offre et la liste des validateurs, une seule personne
contrôle simultanément la monnaie et le consensus.

---

## Journal des corrections

Ce projet a communiqué antérieurement sur un jeton BOSA de **700 000 000 unités à 10 décimales**,
déployé comme contrat applicatif sur la chaîne. Cette structure est **abandonnée** au profit d'un
actif unique : le coin natif, à 18 décimales, pour la même offre de 700 000 000.

Aucune unité n'ayant été distribuée à un tiers, ce changement n'affecte aucun détenteur. Il est
consigné ici plutôt que substitué en silence.
