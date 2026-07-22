<div align="center">
  <img src="../assets/coinbosa-logo.jpg" alt="Coinbosa" width="110" />

  # Portail de migration — spécification
</div>

Des jetons Coinbosa ont été émis lors de phases antérieures sur **Solana** et sur **BNB Chain**.
Le portail de migration permet à leurs détenteurs de les échanger contre du **BOSA natif** sur
Coinbosa Chain.

Ce document décrit le fonctionnement du portail, le formulaire, la preuve remise au détenteur, et
les règles de sécurité et de conformité. Il sert de cahier des charges pour le développement.

---

## Principe : une migration à sens unique

La migration ne crée pas de pont permanent entre les réseaux. C'est un **échange à sens unique** :

1. le détenteur **dépose** ses jetons historiques à une adresse officielle, sur leur réseau
   d'origine ;
2. ces jetons sont **retirés de la circulation** (conservés à une adresse hors-usage, ou
   détruits si le contrat le permet) ;
3. un montant équivalent de **BOSA natif** est **crédité** au détenteur sur Coinbosa Chain.

Ce sens unique est ce qui garantit qu'un jeton n'existe jamais deux fois. Il n'y a pas de retour
possible : une fois migrés, les jetons historiques ne circulent plus.

---

## Le formulaire

Le détenteur renseigne :

| Champ | Rôle | Contrôle |
|---|---|---|
| **Prénom** | identification du migrant | non vide |
| **Nom** | identification du migrant | non vide |
| **Réseau d'origine** | Solana ou BNB Chain | liste fermée |
| **Adresse Coinbosa Chain (0x…)** | destination du BOSA natif | format et **somme de contrôle EIP-55** vérifiés |
| **Empreinte de la transaction de dépôt** | preuve du dépôt sur le réseau d'origine | format propre au réseau |

La saisie du nom et du prénom est une donnée d'identification (voir *Conformité* plus bas).

L'adresse de destination est le point le plus sensible : une adresse mal recopiée envoie le BOSA
à un destinataire irrécupérable. Le portail **rejette** toute adresse dont la somme de contrôle
EIP-55 est invalide, et affiche l'adresse normalisée pour relecture avant validation.

---

## Le déroulement

```
  1. Le détenteur ouvre le portail et choisit son réseau d'origine.

  2. Le portail affiche l'ADRESSE OFFICIELLE DE DÉPÔT pour ce réseau.
     Cette adresse est publiée à l'avance, dans ce dépôt et sur le site,
     pour que le détenteur puisse la vérifier ailleurs que sur le portail.

  3. Le détenteur envoie ses jetons historiques à cette adresse,
     depuis son propre portefeuille.

  4. Il revient sur le portail et renseigne le formulaire :
     prénom, nom, adresse 0x de destination, empreinte du dépôt.

  5. Le projet vérifie le dépôt sur le réseau d'origine :
     bon jeton, bonne adresse de dépôt, montant, expéditeur.

  6. Le projet crédite le BOSA natif à l'adresse 0x, au taux publié.

  7. Le détenteur reçoit la PREUVE : l'empreinte de la transaction
     sur Coinbosa Chain, vérifiable par quiconque sur l'explorateur.
```

L'étape 2 est une règle de sécurité, pas un détail : l'adresse de dépôt doit pouvoir être
vérifiée **hors du portail**. Un portail compromis qui afficherait une fausse adresse de dépôt
détournerait les fonds ; publier l'adresse à l'avance, dans le dépôt et sur le site, permet au
détenteur de la recouper.

---

## La preuve remise au détenteur

La preuve n'est pas un message de confirmation : c'est l'**empreinte de la transaction Coinbosa
Chain** qui a crédité le BOSA. Elle est vérifiable par n'importe qui, sans passer par le projet,
sur l'explorateur du réseau. C'est le sens de « preuve » : quelque chose que le détenteur peut
contrôler lui-même.

Le portail conserve, pour chaque migration : le réseau d'origine, l'empreinte du dépôt,
l'empreinte du crédit, le montant, et l'adresse de destination. Cet historique est consultable
par le détenteur.

---

## Sécurité

- **Adresses de dépôt publiées à l'avance et vérifiables ailleurs que sur le portail.** C'est la
  première protection contre le détournement.
- **Vérification du dépôt côté serveur**, jamais sur la seule déclaration du détenteur. Le
  montant crédité découle du dépôt réellement constaté sur le réseau d'origine.
- **Contrôle de la somme EIP-55** de l'adresse de destination, pour éviter les pertes par
  faute de frappe.
- **Idempotence** : une même empreinte de dépôt ne peut donner lieu qu'à un seul crédit. Sans
  cela, un dépôt pourrait être réclamé plusieurs fois.
- **Journalisation** de chaque étape, pour que toute migration soit reconstituable.
- Le crédit du BOSA suppose une clé qui contrôle les fonds de migration. Cette clé doit être
  **sous multi-signatures**, comme les autres postes de l'offre.

---

## Conformité

Le portail collecte un nom et un prénom, et opère un transfert de valeur. Ces deux faits ont des
conséquences qui ne sont pas optionnelles :

- **Données personnelles.** Le nom et le prénom sont des données personnelles. Leur collecte
  suppose une finalité déclarée, une base légale, une durée de conservation, et l'information du
  détenteur. Selon les juridictions servies, un cadre de protection des données s'applique.
- **Lutte contre le blanchiment.** Un échange de valeur adossé à une identité relève, au-delà de
  certains seuils et selon les juridictions, d'obligations de connaissance du client et de
  filtrage. Le niveau exact dépend du statut réglementaire retenu pour l'opération.
- **Statut de l'opération.** Convertir des jetons pour des tiers peut, selon la juridiction,
  constituer une activité réglementée. Ce point doit être tranché par un conseil juridique avant
  l'ouverture du portail, pas après.

Ces éléments ne bloquent pas la conception technique, mais ils conditionnent l'ouverture au
public. Ils sont énoncés ici pour qu'ils soient traités en amont.

---

## Offre historique constatée

**Un seul réseau d'origine : Solana.**

| Réseau | Offre | Contrat |
|---|---|---|
| Solana | 500 000 000 | `8UyvxCoVXoVaftWzp7j9yo2sGL2HnHTFDV4capenyFaf` |

Le jeton précédemment émis sur **BNB Chain n'existe plus** et n'entre pas dans la migration ;
l'adresse de contrat fournie était par ailleurs invalide. BNB Chain est donc écarté : seule la
migration depuis Solana est retenue.

Une **part de l'offre Solana est concentrée sur un portefeuille** contrôlé par le projet ; le
reste est détenu par des tiers. Le montant exact conservé par le projet détermine le partage
entre l'allocation projet et la réserve de migration (voir *Réconciliation*).

## Réconciliation avec l'offre native

L'offre native est de 700 000 000 BOSA. La migration transpose à parité les jetons Solana détenus
par des tiers ; ce montant constitue la **réserve de migration**. Le reste des 700 000 000
constitue l'**allocation projet**, répartie selon les treize postes de la tokenomique.

Pour fixer les montants, une seule donnée manque : **le montant de jetons Solana détenu par des
tiers** — c'est-à-dire l'offre en circulation (500 000 000) moins ce que le projet conserve sur
son portefeuille. Réserve de migration et allocation projet en découlent directement.

**Anciens contributeurs partis avec des jetons.** Des membres d'équipe ayant quitté le projet
détiennent chacun environ 1 000 000 de jetons historiques. Décision retenue : leur migration est
**honorée comme celle de tout détenteur**. Une migration à sens unique ne permet pas de reprendre
des jetons dans un portefeuille tiers, et aucune exclusion d'adresse n'est pratiquée.

## Ce qui doit être établi avant l'ouverture

1. **Le montant détenu par des tiers** sur Solana — l'offre en circulation moins ce que le projet
   conserve *(voir Réconciliation)*.
2. **Le taux de conversion** — a priori la parité, à confirmer.
3. **L'adresse officielle de dépôt** sur Solana.
4. **Le niveau de connaissance du client** requis, et le prestataire éventuel.

Le formulaire et le déroulement ne dépendent pas de ces valeurs : ils sont construits. Mais le
portail **ne s'ouvre au public** qu'une fois ces points établis et publiés.

---

## État

Un squelette du formulaire, avec la validation côté client, est fourni dans `portal/`. Il
**n'est pas fonctionnel en l'état** : il ne collecte rien et n'envoie rien. Il attend un service
sécurisé qui vérifie les dépôts, crédite le BOSA et conserve l'historique — service qui doit être
développé et audité avant toute mise en service réelle.
