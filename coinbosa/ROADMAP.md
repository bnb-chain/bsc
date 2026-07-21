<div align="center">
  <img src="assets/coinbosa-logo.jpg" alt="Coinbosa" width="120" />

  # Feuille de route — Coinbosa Chain
</div>

---

## Où en est le projet

Le socle technique de la chaîne fonctionne et est vérifié. Ce qui manque n'est plus de la
recherche, mais de l'exécution : des machines, des services, des interfaces.

| | État |
|---|---|
| Client Coinbosa compilé, 5 s par bloc | fait, mesuré |
| Genesis souverain, chainId 26262 | fait |
| Franchissement des blocs d'epoch | fait, vérifié aux blocs 200, 400 et 600 |
| Standard de jeton BRC20 | fait, 26 tests sur 26 |
| Jeton BOSA, 700 M, 10 décimales | fait, déployé et vérifié |
| Explorateur de blocs minimal | fait, sans indexation |
| Couche d'enjeu (staking, sanctions) | à faire — chantier structurant |
| Réseau multi-validateurs | à faire |
| RPC public | à faire |
| Front-end | à faire |

---

## L'écosystème Coinbosa

La chaîne n'est pas un but en soi : elle sert des produits qui existent déjà ou qui arrivent.

| Produit | État | Lien avec la chaîne |
|---|---|---|
| **Coinbosa Academy** | en production | à raccorder : paiement en BOSA |
| **NextFuture** | en construction | échange ; cotation et échange du BOSA |
| **Coinbosa Card** | à venir | carte virtuelle ; dépense du solde BOSA |
| **Neobanq** | plateforme existante | rails bancaires et interface crypto |
| **Coinbosa VPN** | en cours | abonnement payable en BOSA |
| **bite-fast** | jeton de l'écosystème | à porter en BRC20 sur la chaîne |

Deux modes de paiement sont visés partout : **en jetons de l'écosystème**, et **en monnaie
classique par carte**. Le second suppose un prestataire de paiement, donc une entité qui
encaisse — c'est un chantier réglementaire autant que technique.

Une cotation sur des places d'échange externes est envisagée. Elle n'a pas de dépendance
technique avec la chaîne elle-même, mais elle en a une avec tout le reste de cette feuille de
route : aucune place sérieuse ne référence un réseau à validateur unique et sans explorateur
public.

---

## Jalons

### Jalon 1 — La couche d'enjeu

*Chantier structurant.* Le moteur de consensus, Parlia, est conçu pour la preuve d'enjeu — son
nom signifie *Proof of Staked Authority*. Mais le contrat système actuel expose un set de
validateurs fixe, modifiable par un gouverneur : il n'y a ni dépôt, ni élection par l'enjeu, ni
sanction. Cette version minimale était nécessaire pour débloquer la chaîne au bloc 200 ; elle
n'est pas la cible.

- Contrat d'enjeu : dépôt, retrait, période de déblocage
- Élection du set de validateurs par le montant immobilisé, à chaque epoch
- Distribution des récompenses aux validateurs et aux délégataires
- Sanctions : absence de production, double signature, mise en quarantaine
- Gouvernance des paramètres

**Risque à surveiller** — toute fonction du chemin consensus qui revert rend le bloc
improduisible et arrête définitivement le réseau. Chaque évolution de ce contrat doit passer par
le contrôle de franchissement d'epoch en intégration continue.

**Critère de réussite** — un validateur rejoint le set en immobilisant des jetons, sans
intervention manuelle ; un validateur fautif est sanctionné automatiquement.

### Jalon 2 — Le réseau tient debout

*Sans ce jalon, tous les suivants sont sans objet.* Aujourd'hui un seul validateur produit
tous les blocs : c'est une base de données avec des signatures, pas un réseau.

- Provisionner les serveurs, un par validateur, sur des hébergeurs et des zones distincts
- Générer les clés **sur chaque serveur**, jamais ailleurs
- Cérémonie initiale : constitution du set de validateurs conformément au livre blanc
- Raccorder les nœuds entre eux par bootnodes, vérifier la rotation des producteurs de blocs
- Éprouver la résilience : couper un nœud, la chaîne doit continuer

**Critère de réussite** — un nœud arrêté, la chaîne continue de produire des blocs.

### Jalon 3 — Le réseau est joignable

- Nœud RPC public derrière un reverse proxy, avec limitation de débit et TLS
- Point d'accès WebSocket pour les abonnements temps réel
- Nœud de secours, sauvegardes, supervision et alertes
- Enregistrement du chainId sur `ethereum-lists/chains` afin d'apparaître dans les wallets

**Critère de réussite** — n'importe qui peut ajouter Coinbosa dans son wallet et envoyer une
transaction.

### Jalon 4 — Le réseau est lisible

L'explorateur actuel interroge le RPC en direct : pratique pour observer, insuffisant pour un
produit. Sans base de données, pas d'historique par adresse, pas de vérification de code
source, pas de suivi des porteurs.

- Explorateur indexé, avec base de données
- Vérification publique du code source des contrats
- Suivi des porteurs de BOSA et des transferts

**Attention licence** — Blockscout n'est plus open source depuis le 22 avril 2026. Sa version
11 et les suivantes interdisent contractuellement de retirer la marque. Pour un produit
rebrandable, il faut épingler la version `v10.2.6`, dernière sous GPLv3.

**Critère de réussite** — un tiers peut auditer une transaction sans accès au serveur.

### Jalon 5 — Le réseau est présentable

Site public et explorateur au niveau des grandes chaînes publiques, en six langues dont
l'arabe. La spécification complète — identité visuelle dérivée du logo, architecture
multilingue, critères de réception mesurables — est dans **[FRONTEND.md](FRONTEND.md)**.

- Site public multilingue, rendu statique ou serveur, thèmes clair et sombre
- Ajout du réseau au wallet en un clic, sans copier-coller de paramètres
- Explorateur indexé reprenant la charte de l'explorateur actuel
- Robinet de test et documentation d'intégration

**Critère de réussite** — Lighthouse au-dessus de 90 en performance et en accessibilité sur
mobile, six langues sans chaîne non traduite, et un développeur extérieur qui intègre BOSA
sans nous solliciter.

### Jalon 6 — L'écosystème est branché

- Paiement en BOSA sur Coinbosa Academy
- Cotation du BOSA sur NextFuture
- Portage du jeton **bite-fast** en BRC20
- Abonnement Coinbosa VPN payable en BOSA
- Coinbosa Card adossée au solde on-chain
- Paiement par carte classique via un prestataire

### Jalon 7 — Ouverture extérieure

- Audit de sécurité externe des contrats et du client patché
- Passerelle vers un réseau majeur, sans laquelle BOSA reste enfermé sur sa propre chaîne
- Cotation sur des places d'échange externes
- Publication de la tokenomique

---

## Ce qui n'est pas au programme

- **BRC-721 et les NFT** — écartés. Le standard n'est pas implémenté et ne le sera pas tant
  qu'un besoin produit ne le justifie pas, malgré sa mention dans le livre blanc v2.
- **Les contrats système complets de BNB Chain** — non repris. La couche d'enjeu de Coinbosa
  sera écrite sur mesure pour son architecture, plutôt que d'hériter de la mécanique
  inter-chaînes de BNB, sans objet ici.

---

## Points de vigilance

**Les clés de validateur.** Elles doivent être générées sur le serveur qui les utilise, ne
jamais transiter par un poste de travail, un dépôt ou une messagerie. Une clé compromise, c'est
un validateur compromis.

**L'adresse de départ.** Elle détient les 700 000 000 BOSA et la propriété du contrat. Sa clé
privée mérite le même traitement qu'une clé de coffre : conservation hors ligne, sauvegarde
séparée, et à terme un portefeuille multi-signatures plutôt qu'une clé unique.

**Le paiement en monnaie classique et la cotation externe** relèvent autant du droit que de la
technique. Les traiter comme des sujets purement techniques serait une erreur de séquencement.

---

## Reprendre le projet sur une autre machine

Tout est dans ce dépôt. Rien d'essentiel ne vit sur la machine de développement actuelle.

```bash
git clone https://github.com/Coinbosa/coinbosa-chain
cd coinbosa-chain

# 1. compiler le client Coinbosa — le binaire officiel de BNB Chain ne convient pas,
#    il produit des blocs de 3 s au lieu de 5 s
make geth

# 2. installer les dépendances des contrats
cd coinbosa && npm install

# 3. générer la clé du validateur, puis le genesis correspondant
../build/bin/geth account new --datadir node1
VALIDATOR=0xLAdresseObtenue node scripts/build-genesis.js

# 4. compiler les contrats, lancer le nœud
node scripts/compile.js
./scripts/start-node.sh
```

**Prérequis** — Go 1.25 ou plus, Node 20 ou plus, environ 5 Go d'espace disque libre pour la
compilation du client.

Tous les paramètres du réseau et du jeton sont dans **`coinbosa.config.json`**. C'est le seul
fichier à modifier pour changer une valeur ; rien n'est codé en dur dans les scripts.

Déploiement du jeton, une fois le nœud lancé :

```bash
node scripts/deploy-bosa.js     # lit l'adresse de départ depuis coinbosa.config.json
```
