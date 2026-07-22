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

## Ce qui doit être établi avant l'ouverture

Aucune de ces valeurs n'est avancée tant qu'elle n'est pas établie et vérifiable :

1. **L'offre historique en circulation** sur Solana et sur BNB Chain, avec les adresses de
   contrat correspondantes.
2. **Le taux de conversion** appliqué à chaque réseau d'origine — en tenant compte du passage
   éventuel à 18 décimales.
3. **L'allocation de BOSA natif** réservée à la migration au sein de l'offre de 700 000 000, et
   son adresse.
4. **Les adresses officielles de dépôt** sur chaque réseau d'origine.
5. **Le prestataire et le niveau** de connaissance du client, s'ils sont requis.

Le formulaire et le déroulement décrits ci-dessus ne dépendent pas de ces valeurs : ils peuvent
être construits dès maintenant. Mais le portail **ne s'ouvre au public** qu'une fois ces cinq
points établis et publiés.

---

## État

Un squelette du formulaire, avec la validation côté client, est fourni dans `portal/`. Il
**n'est pas fonctionnel en l'état** : il ne collecte rien et n'envoie rien. Il attend un service
sécurisé qui vérifie les dépôts, crédite le BOSA et conserve l'historique — service qui doit être
développé et audité avant toute mise en service réelle.
