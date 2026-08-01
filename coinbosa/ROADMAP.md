<div align="center">
  <img src="assets/coinbosa-logo.jpg" alt="Coinbosa" width="120" />

  # Feuille de route — Coinbosa Chain
</div>

---

## Où en est le projet

Le socle technique fonctionne et a été vérifié manuellement de bout en bout (démarrage du nœud,
5 s par bloc, franchissement d'epoch, contrats). L'intégration continue **rejoue ce banc à chaque
push** sur une machine vierge ; son état réel se lit dans l'onglet Actions du dépôt — il n'est pas
affirmé « vert » ici tant qu'un run n'a pas été observé vert. Ce qui manque relève désormais de
l'exécution, pas de la recherche.

| | État |
|---|---|
| Client Coinbosa compilé, 5 s par bloc | fait, mesuré |
| Chaîne souveraine, chainId 26262 | fait |
| Franchissement des blocs d'epoch | fait, vérifié aux blocs 200, 400, 600, 800 |
| Contrat système sur mesure | fait |
| Standard de jeton BRC20 | fait, banc complet (propriété en deux étapes et clôture d'émission incluses) |
| Explorateur multilingue | fait, sans indexation |
| Intégration continue | rejoue le banc (nœud, 5 s, BRC20, epoch) à chaque push — état dans l'onglet Actions |
| Genesis : 700 M répartis, pont purgé | fait, vérifié on-chain |
| Mise de l'offre sous multi-signatures | à faire — bloquant |
| Couche d'enjeu (staking, sanctions) | à faire |
| Réseau multi-validateurs | à faire |
| RPC public, explorateur indexé, site | à faire |

---

## L'écosystème

La chaîne sert des produits, elle n'est pas une fin en soi.

| Produit | Nature | État |
|---|---|---|
| **Coinbosa Academy** | école de formation — forex, actions, puis crypto | en production |
| **NextFuture** | place d'échange crypto — marché au comptant et contrats à terme | en construction |
| **Coinbosa Card** | carte crypto prépayée et virtuelle, dépôts en crypto, dépense à l'international | à venir |
| **bite-fast** | place d'échange crypto externe | existante |
| **Neobanq** | plateforme bancaire | existante |
| **Coinbosa VPN** | service d'abonnement | en cours |

Aucun de ces produits n'est raccordé à la chaîne à ce jour. Chaque raccordement sera annoncé
lorsqu'il fonctionnera, et pas avant.

---

## Jalons

### Jalon 1 — Le genesis définitif

*Bloquant : tout le reste en dépend.* Le genesis actuel est un genesis de développement. Son
offre n'a jamais été conçue — elle est l'addition d'un héritage du réseau amont et de valeurs
posées pour tester. Or le genesis porte désormais **l'intégralité de l'offre BOSA**, puisqu'il
n'existe plus de jeton applicatif.

**Ce qui est fait**, vérifié sur la chaîne : l'offre vaut exactement 700 000 000 BOSA, répartie
sur les treize postes ; le solde hérité du pont du réseau amont est purgé ; le franchissement
d'epoch reste assuré.

**Ce qui reste**, et qui est bloquant : remplacer les adresses de développement par de vraies
adresses **multi-signatures** générées sur le serveur, dans `genesis/distribution-addresses.json`,
puis publier chaque adresse. Tant que la même clé peut détenir l'offre et contrôler la liste des
validateurs, une seule personne contrôle la monnaie et le consensus.

**Critère de réussite** — chaque poste est détenu par une adresse multi-signatures publiée,
vérifiable sur l'explorateur.

### Jalon 2 — La couche d'enjeu

Le moteur de consensus est conçu pour la preuve d'enjeu — Parlia signifie *Proof of Staked
Authority*. Le contrat système, lui, expose un set de validateurs fixe modifiable par une clé.
Tant que c'est le cas, il n'y a pas de preuve d'enjeu : il y a une preuve d'autorité avec une
couche de staking décorative.

- Contrat d'enjeu : dépôt, retrait, période de déblocage
- Élection du set de validateurs par le montant immobilisé, à chaque epoch
- Sanctions : absence de production, double signature, mise en quarantaine
- `updateValidatorSet` passe sous multi-signatures et délai de contestation, restreint aux
  urgences tracées publiquement

**Deux risques à traiter dès la conception**, tous deux capables d'arrêter le réseau :

Toute fonction du chemin consensus qui échoue rend le bloc improduisible. Le contrôle de
franchissement d'epoch en intégration continue est obligatoire à chaque évolution.

Le plafond de gas des appels de lecture est réglable **par nœud**. Si l'élection est calculée
dans le contrat, deux validateurs configurés différemment peuvent obtenir des résultats
différents pour le même bloc — le réseau se partitionne, puis s'arrête. Le calcul doit tenir
dans une enveloppe de gas bornée et documentée, imposée à tous les nœuds.

**Critère de réussite** — un validateur rejoint le set en immobilisant des jetons, sans
intervention manuelle ; un validateur fautif est sanctionné automatiquement.

### Jalon 3 — Le réseau tient debout

Un validateur unique produit aujourd'hui tous les blocs : c'est un registre signé, pas un
réseau.

- Provisionner un serveur par validateur, sur des hébergeurs et des zones distincts
- Générer les clés **sur chaque serveur**, jamais ailleurs
- Raccorder les nœuds par bootnodes, vérifier la rotation des producteurs
- Éprouver la résilience : couper un nœud, la chaîne doit continuer

**À dire franchement** — les récompenses venant uniquement des frais de transaction, un
validateur externe n'a aucun intérêt économique à rejoindre le réseau tant que le volume
n'existe pas. Les premiers validateurs seront donc adossés au projet, ce qui doit être écrit
plutôt que présenté comme une décentralisation.

**Critère de réussite** — un nœud arrêté, la chaîne continue de produire des blocs.

### Jalon 4 — Le réseau est joignable

- Point d'accès RPC public derrière un reverse proxy, avec limitation de débit et TLS
- Point d'accès WebSocket, nœud de secours, sauvegardes, supervision
- Enregistrement du chainId 26262 sur `ethereum-lists/chains`

Ce jalon conditionne plus qu'il n'y paraît : sans réseau joignable de l'extérieur, ni les
portefeuilles, ni les explorateurs tiers, ni les outils de trésorerie multi-signatures ne
peuvent s'y connecter.

**Critère de réussite** — n'importe qui peut ajouter Coinbosa à son portefeuille et envoyer une
transaction.

### Jalon 5 — Le réseau est lisible

- Explorateur indexé, avec base de données
- Vérification publique du code source des contrats
- Suivi de la répartition de l'offre et des transferts

**Attention licence** — Blockscout n'est plus open source depuis le 22 avril 2026 ; ses versions
11 et suivantes interdisent contractuellement de retirer la marque. Pour un produit à notre nom,
épingler `v10.2.6`, dernière version sous GPLv3.

**Critère de réussite** — un tiers peut vérifier la répartition de l'offre sans accès au serveur.
C'est précisément ce qu'une place de cotation contrôle en premier.

### Jalon 6 — Le réseau est présentable

Site public et explorateur au niveau des grandes chaînes publiques, en six langues dont l'arabe.
Spécification complète dans **[FRONTEND.md](FRONTEND.md)**.

**Critère de réussite** — Lighthouse au-dessus de 90 en performance et accessibilité sur mobile,
six langues sans chaîne non traduite.

### Jalon 7 — L'écosystème est branché

- Paiement en BOSA sur Coinbosa Academy et Coinbosa VPN
- Cotation du BOSA sur NextFuture, au comptant puis à terme
- Coinbosa Card adossée au solde on-chain
- Passerelle avec bite-fast

**Le point de blocage à lever en premier**, avant tout développement : les processeurs de
paiement et les rampes fiat ne référencent en général que les chaînes majeures. Qu'un prestataire
accepte un actif vivant sur une chaîne souveraine n'a rien d'acquis. Si la réponse est non, il
faudra un déploiement canonique de BOSA sur une chaîne liquide, avec passerelle vers Coinbosa —
ce qui change l'architecture, pas seulement le calendrier.

### Jalon 9 — Ouverture extérieure

- Audit de sécurité externe des contrats et du client modifié
- Passerelle vers un réseau majeur
- Cotation sur des places d'échange externes
- Publication du livre blanc et des métriques de transparence

---

## Ce qui n'est pas au programme

- **Les NFT (BRC-721)** — écartés. Le standard ne sera implémenté que si un besoin produit le
  justifie.
- **Une émission monétaire** — le moteur de consensus ne crée aucune monnaie, et l'offre de
  700 000 000 BOSA est définitive.
- **Les contrats système complets du réseau amont** — la couche d'enjeu de Coinbosa sera écrite
  pour son architecture, plutôt que d'hériter d'une mécanique inter-chaînes sans objet ici.

---

## Points de vigilance

**Les clés de validateur** doivent être générées sur le serveur qui les utilise, et ne jamais
transiter par un poste de travail, une messagerie ou ce dépôt.

**La concentration de l'offre et du pouvoir.** Tant que la même clé détient l'offre et contrôle
la liste des validateurs, une seule personne contrôle simultanément la monnaie et le consensus.
C'est l'obstacle numéro un du dossier, devant tous les autres, et il survivrait à un changement
de chaîne. Le jalon 1 le traite.

**Le rendement des validateurs est nul sans trafic.** Ce n'est pas un défaut à corriger, c'est
une conséquence assumée du choix de ne pas créer de monnaie. Il faut le dire, et ne jamais
publier de taux de rendement.

**Les paiements en monnaie classique et la cotation externe** relèvent autant du droit que de la
technique.

---

## Reprendre le projet sur une autre machine

Tout est dans ce dépôt.

```bash
git clone https://github.com/Coinbosa/coinbosa-chain
cd coinbosa-chain

# 1. compiler le client Coinbosa — le binaire officiel du réseau amont ne convient pas,
#    il produit des blocs de 3 s au lieu de 5 s
make geth

# 2. installer les dépendances (reproductible, depuis le lock)
cd coinbosa && npm ci

# 3. générer la clé du validateur, puis le genesis de DÉVELOPPEMENT correspondant.
#    ALLOW_DEV=1 écrit genesis/genesis-coinbosa-dev.json (adresses synthétiques + marqueur
#    coinbosaDev) : jamais confondu avec la production.
../build/bin/geth account new --datadir node1
VALIDATOR=0xLAdresseObtenue ALLOW_DEV=1 node scripts/build-genesis.js

# 4. compiler les contrats, lancer le nœud (start-node.sh est DEV-only, garde explicite)
node scripts/compile.js
COINBOSA_DEV=1 ./scripts/start-node.sh
```

**Prérequis** — Go 1.25 ou plus, Node 20 ou plus, environ 5 Go d'espace disque pour la
compilation.

Les paramètres du réseau sont dans **`coinbosa.config.json`**, l'économie du jeton dans
**[TOKENOMICS.md](TOKENOMICS.md)**, et les décisions structurantes dans
**[DECISIONS.md](DECISIONS.md)**.
