<div align="center">
  <img src="../assets/coinbosa-logo.jpg" alt="Coinbosa" width="110" />

  # Durcissement de sécurité
</div>

Ce document est le résultat d'un audit adversarial du code de Coinbosa Chain : sept dimensions
passées au crible, chaque finding réfuté avant d'être retenu. Il liste ce qui a été corrigé,
ce qui reste à faire, et la configuration de production — distincte de la configuration de
développement livrée dans les scripts.

Le réseau **n'a pas fait l'objet d'un audit externe**. Ce document est une auto-évaluation ;
il ne le remplace pas.

---

## Corrigé dans le code

| Sévérité | Problème | Correctif |
|---|---|---|
| **Haute** | **XSS stocké on-chain** dans l'explorateur : `name()` / `symbol()` d'un jeton, contrôlés par son déployeur, étaient injectés en `innerHTML` sans échappement. Un jeton dont `name()` vaut `<img src=x onerror=…>` exécutait du script chez tout visiteur ouvrant l'onglet Tokens. | Échappement HTML systématique (`esc()`) des valeurs issues de contrats, et plafonnement de leur longueur. |
| **Moyenne** | **`updateValidatorSet` pouvait figer la chaîne** : remplacer le set par des adresses sans nœud vivant laissait le réseau sans signataire au bloc d'epoch suivant — arrêt irréversible, en une transaction. | Le contrat exige désormais que le `GOVERNOR` reste dans le set, et rejette les clés de vote en double. |
| **Moyenne** | **Contrats inter-chaînes hérités** (pont, cross-chain, light client, relayers) conservaient leur bytecode ; seul le solde était purgé. | Leur code est retiré du genesis. Vérifié empiriquement : la chaîne démarre et franchit l'epoch sans eux. |
| **Basse** | **Ports RPC divergents** (nœud sur 8595, explorateur sur 8545) : l'explorateur ne joignait jamais le nœud et basculait en silence sur des données de démonstration. | Port unifié sur 8545 partout ; l'URL RPC de l'explorateur gère aussi les hôtes sans port. |
| **Basse** | **Adresses de distribution en double** non détectées : deux postes partageant une adresse fusionnaient leurs soldes sans alerte. | `build-genesis.js` rejette tout doublon d'adresse. |
| **Basse** | **`check-supply` ne vérifiait que les soldes.** | Il vérifie maintenant aussi que les contrats inter-chaînes sont sans code. |
| **Info** | Second constructeur de genesis mort et trompeur (`make_genesis.py`, symbole « CBA », référence manquante). | Supprimé. |
| **Info** | Commentaires périmés (650 M / réserve 50 M) et champs de genesis inertes incohérents. | Corrigés. |

---

## Ce que la CI verte ne prouve PAS

L'intégration continue démarre un nœud, mesure le temps de bloc, exécute les tests BRC20 et
franchit un bloc d'epoch. C'est utile, mais il faut savoir ce qu'elle **ne** démontre pas :

- **Rien sur le multi-validateurs.** Elle teste **un** validateur. La tolérance aux pannes, la
  rotation, le slashing et la finalité rapide ne sont pas couverts — ils n'existent pas encore.
- **Rien sur la résistance à la réorganisation.** Avec un validateur, l'opérateur peut réécrire
  la chaîne. La CI ne peut pas prouver le contraire.
- **Rien sur la sécurité du contrat de consensus sous charge** — pas de test à 41 validateurs,
  pas de test avec un plafond de gas d'appel réduit.
- **Rien sur la configuration de production.** La CI utilise la configuration de développement.

---

## Configuration de production — obligatoire avant toute valeur réelle

Les scripts livrés (`start-node.sh`) sont marqués **DÉVELOPPEMENT LOCAL**. La production exige
une configuration différente.

### Les clés ne vivent pas dans le nœud

En développement, le nœud déverrouille la clé de scellage dans son propre processus
(`--unlock`, `--allow-insecure-unlock`). **En production, c'est interdit.** La clé de scellage
doit être détenue par un **signeur distant** (Clef, web3signer) ou un HSM/KMS, hors du processus
exposé au réseau. Aucune clé privée en clair sur le serveur.

### Le RPC est fermé

| Réglage | Développement | Production |
|---|---|---|
| `--http.api` | `eth,net,web3,parlia` | idem — **jamais `debug`**, ni `txpool` si inutile |
| `--http.corsdomain` | origine locale | liste explicite des origines autorisées |
| `--http.vhosts` | `127.0.0.1,localhost` | le domaine réel, jamais `*` |
| Exposition | `127.0.0.1` | derrière un reverse-proxy TLS, avec authentification et limitation de débit |
| Méthodes admin | — | fermées |

### Séparation des rôles

Aujourd'hui, une clé unique peut à la fois **sceller les blocs**, **gouverner la liste des
validateurs** (`updateValidatorSet`) et **retirer les fonds** (`sweepSurplus`). C'est le risque
numéro un du dossier. En production, ces rôles doivent être séparés :

- **`GOVERNOR`** = un portefeuille **multi-signatures** (Safe) avec délai (timelock), distinct de
  la clé de scellage et de la trésorerie.
- **Clé de scellage** = par validateur, générée sur son serveur, jamais partagée.
- **Trésorerie** = multi-signatures dédié.

### Le genesis de production

Le genesis de développement place l'offre sur des adresses synthétiques non dépensables et
finance le validateur. **Il ne doit jamais être déployé en production.** La production suppose :
remplir `distribution-addresses.json` avec de vraies adresses multi-signatures, construire
**sans** `ALLOW_DEV`, et ne pas financer le validateur.

---

## Vecteurs d'attaque et parades

### Un seul validateur = réorganisation et censure triviales

Tant qu'un validateur produit tous les blocs, il peut réorganiser la chaîne et censurer des
transactions. **Parade :** passer à au moins 4 validateurs (cible 12), sur des entités, des
infrastructures et des clés distinctes. À dire publiquement tant que ce n'est pas le cas.

### Déni de service par frais quasi nuls

Des frais quasi nuls rendent le spam de transactions bon marché. **Parade :** fixer un prix de
gas minimal (`--miner.gasprice`, `--txpool.pricelimit`), borner le mempool, réévaluer la limite
de gas par bloc, surveiller le disque.

### Partition silencieuse par binaire non patché

Le temps de bloc de 5 s est une constante du client, pas du genesis, et le binaire s'annonce
avec la même version que le réseau amont. Un validateur qui lancerait par erreur le binaire
officiel (3 s) pourrait se raccorder puis voir ses blocs rejetés — partition sans signal.
**Parade :** un marqueur d'identité réseau distinct, et une procédure de déploiement qui impose
le binaire Coinbosa.

### Compromission de clé

**Parade :** génération des clés sur le serveur cible, sauvegardes chiffrées et testées,
multi-signatures pour l'offre et pour `updateValidatorSet`, procédure de remplacement de clé
documentée (via la gouvernance, jamais par réécriture de l'historique git).

---

## Ce qui reste à faire

Ces points relèvent du serveur et de l'exécution, pas d'un correctif de fichier :

1. Séparer le `GOVERNOR`, la clé de scellage et la trésorerie sous multi-signatures et timelock.
2. Sortir la clé de scellage du nœud RPC (signeur distant / HSM).
3. Fermer le RPC (proxy TLS, origines explicites, `debug` retiré, limitation de débit).
4. Passer à plusieurs validateurs indépendants.
5. Anti-DoS : prix de gas minimal, bornes mempool.
6. Marqueur d'identité réseau distinct.
7. Supervision (hauteur de bloc, mempool, disque, accès RPC non autorisé) et plan d'incident.
8. Audit de sécurité **externe** avant toute mise en valeur du réseau.
