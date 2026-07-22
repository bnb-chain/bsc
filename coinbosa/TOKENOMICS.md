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

## L'offre native est le total migré de l'offre historique

Des jetons Coinbosa ont été émis lors de phases antérieures : **500 000 000 sur Solana** et
**200 000 000 sur BNB Chain**, soit 700 000 000 au total — exactement l'offre native. Ce n'est pas
une coïncidence : l'offre native **est** le total historique, transposé à parité sur Coinbosa
Chain par la [migration](docs/MIGRATION.md).

L'offre de 700 000 000 BOSA se décompose donc en deux blocs :

- une **réserve de migration**, égale au montant de jetons historiques détenus par des tiers
  (public et anciens contributeurs), créditée à ces détenteurs à mesure qu'ils migrent, un jeton
  pour un BOSA ;
- une **allocation projet**, égale au reste — la part que le projet détient déjà sous forme
  historique, sur son portefeuille concentré —, organisée selon les treize postes ci-dessous.

**Les pourcentages des treize postes s'appliquent à l'allocation projet, pas au total de
700 000 000.** Les montants absolus seront fixés une fois connu le partage entre l'offre détenue
par le projet et celle détenue par des tiers ; jusque-là, ce document en donne la structure, pas
les valeurs en jetons.

---

## Répartition de l'allocation projet

*Pourcentages appliqués à l'allocation projet (l'offre historique concentrée sur le portefeuille
du projet). Les montants en BOSA seront publiés après réconciliation.*

| Poste | Part | Objet |
|---|---|---|
| **Développement** | 20 % | construction du réseau, des contrats et des applications |
| **Technique** | 10 % | infrastructure, nœuds, exploitation, outillage |
| **Recherche** | 10 % | travaux de recherche du protocole |
| **Équipe** | 10 % | rémunération des contributeurs |
| **Fonds financier** | 10 % | fonds de dépôt adossé à Coinbosa Card |
| **Fonds de liquidité** | 10 % | tenue de marché et profondeur de carnet |
| **Recherche IA** | 10 % | travaux d'intelligence artificielle |
| **Recherche finance et fintech** | 5 % | travaux sur les usages financiers |
| **Sécurité** | 3 % | sécurisation du réseau et réponse aux incidents |
| **Audit** | 2 % | audits externes du code et des contrats |
| **Événements et formation** | 2 % | formation et rencontres de l'écosystème |
| **Distribution publique et communauté** | 5 % | mise en circulation initiale |
| **Réserve stratégique** | 3 % | imprévus, partenariats, opportunités |
| **Total** | **100 %** | *de l'allocation projet* |

> Les treize postes bouclent à 100 % de l'allocation projet. Les deux derniers — distribution
> publique et communauté (5 %) et réserve stratégique (3 %) — complètent les onze premiers ; le
> premier parce qu'une part détenue par des tiers est nécessaire à l'existence d'un marché, le
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
