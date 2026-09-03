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
| **Moyenne** | **`updateValidatorSet` pouvait figer la chaîne** : remplacer le set par des adresses sans nœud vivant laissait le réseau sans signataire au bloc d'epoch suivant — arrêt irréversible, en une transaction. | Le contrat exige que le **validateur de genèse** (`INITIAL_VALIDATOR`) reste dans le set, et rejette les doublons d'adresse comme de clé de vote. Ce n'est **pas** le `GOVERNOR`, et cette garde ne suffit pas à garantir la liveness — voir l'encadré sous le tableau. |
| **Moyenne** | **Contrats inter-chaînes hérités** (pont, cross-chain, light client, relayers) conservaient leur bytecode ; seul le solde était purgé. | Leur code est retiré du genesis. Vérifié empiriquement : la chaîne démarre et franchit l'epoch sans eux. |
| **Basse** | **Ports RPC divergents** (nœud sur 8595, explorateur sur 8545) : l'explorateur ne joignait jamais le nœud et basculait en silence sur des données de démonstration. | Port unifié sur 8545 partout. **Depuis, le repli sur des données de démonstration a été entièrement supprimé** : l'explorateur n'affiche plus que ce qui vient de la chaîne, et un avis explicite quand aucun nœud ne répond. L'accès passe par le relais same-origin `/rpc`. |
| **Basse** | **Adresses de distribution en double** non détectées : deux postes partageant une adresse fusionnaient leurs soldes sans alerte. | `build-genesis.js` rejette tout doublon d'adresse. |
| **Basse** | **`check-supply` ne vérifiait que les soldes.** | Il vérifie maintenant aussi que les contrats inter-chaînes sont sans code. |
| **Info** | Second constructeur de genesis mort et trompeur (`make_genesis.py`, symbole « CBA », référence manquante). | Supprimé. |
| **Info** | Commentaires périmés (650 M / réserve 50 M) et champs de genesis inertes incohérents. | Corrigés. |

> **C'est `INITIAL_VALIDATOR` qui doit rester dans le set, pas `GOVERNOR`.** Ce document a
> longtemps écrit l'inverse. L'accident évité : un opérateur qui lit « le `GOVERNOR` doit
> rester dans le set » compose sa rotation autour du gouverneur — or le gouverneur ne détient
> aucune clé de scellage, il gouverne hors ligne, c'est tout l'intérêt de la séparation. Deux
> issues, mauvaises toutes les deux :
>
> - il omet le validateur de genèse → `CoinbosaValidatorSet.sol` (l. 223) rejette la
>   transaction, `"genesis validator must remain a validator"`, et il croit à un bug du contrat ;
> - il ajoute le gouverneur **à côté** du validateur de genèse → la transaction passe, et la
>   chaîne s'arrête au bloc d'epoch suivant. C'est le scénario reproduit par
>   `consensus/parlia/coinbosa_halt_repro_test.go` : à N=2, le validateur de genèse est bloqué
>   par `errRecentlySigned` et le second signataire n'existe sur aucune machine.
>
> La garde du contrat empêche un set **sans aucun** scelleur. Elle ne garantit **pas** la
> liveness : Parlia veut ⌊N/2⌋+1 signataires réellement en ligne. La seule barrière opérable
> reste `scripts/rotate-validators.js`, qui exige d'avoir **vu sceller** les nouveaux
> validateurs avant la bascule d'epoch.

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

### La clé de scellage vit dans le processus validateur — c'est l'état actuel, pas un oubli

En développement **comme en production**, le nœud déverrouille la clé de scellage dans son
propre processus : `deploy/40-validator.sh` (l. 111-113) passe `--unlock`, `--password` et
`--allow-insecure-unlock`. Ce document a longtemps écrit « en production, c'est interdit » —
c'était une cible, formulée comme une description. La confusion est dangereuse.

**L'accident que cette correction évite :** un opérateur aligne la production sur l'ancienne
phrase et retire `--unlock` sans avoir branché de signeur distant. Le nœud démarre
normalement, sans la moindre erreur au lancement — puis plus aucun bloc n'est scellé.
Vérifié sur ce dépôt : `eth/backend.go` (l. 797) passe `wallet.SignData` à
`parlia.Authorize()` ; appelé sur un compte keystore verrouillé, `SignData` rend
`authentication needed: password or unlock`, et rend une signature de 65 octets une fois le
compte déverrouillé. La panne est donc silencieuse et totale.

Ce que la configuration actuelle compense, faute de HSM : le validateur n'expose **aucun**
HTTP, tourne sous un utilisateur dédié, avec `--nodiscover` et `--netrestrict 127.0.0.0/8` ;
le RPC public est un **autre** processus, qui ne détient aucune clé (`deploy/30-node.sh`).
Un signeur distant (Clef, web3signer) ou un HSM reste l'objectif — il n'est pas en place.

### Le RPC public est un relais, pas un geth ouvert

Ce que les scripts posent **réellement**, et non ce qu'il faudrait poser un jour :

| Réglage | Ce que posent `30-node.sh` / `73-node-archive.sh` | Pourquoi |
|---|---|---|
| `--http.api` | `eth,net` | Ni `admin`, ni `debug`, ni `txpool`, ni `personal`. `web3` tombe avec le reste : `web3_clientVersion` donnait la version exacte du client, recoupable avec `go-vuln-allowlist.json`. |
| `--http.addr` | `127.0.0.1` | Le port du nœud reste fermé au pare-feu. Caddy relaie `/rpc`, en POST uniquement, corps plafonné à 32 Ko. |
| `--http.vhosts` | `localhost,127.0.0.1` | Garde anti-DNS-rebinding. **Ne pas y mettre le domaine public** — voir la note ci-dessous. |
| `--http.corsdomain` | `https://explorer.coinbosa.com` | Liste explicite, jamais `*`. |
| `--nodiscover` | sur **les deux** nœuds | C'est la condition qui justifie les dérogations QUIC / WebTransport / DTLS de `go-vuln-allowlist.json`. |
| Bornes de lecture | `--rangelimit`, `--rpc.logquerylimit 20`, lots bornés (50 côté public, 200 côté archive) | Le défaut geth est de 1000 appels par lot : une seule requête HTTP en portait mille, ce qui contournait toute limitation de débit comptée en requêtes. |

> **Sur `--http.vhosts`, l'ancienne consigne « le domaine réel » était à la fois inexacte et
> inutile — et il faut savoir pourquoi, sinon quelqu'un la remettra.** `node/rpcstack.go`
> (l. 459) laisse passer **toute** requête dont l'en-tête `Host` est une adresse IP, sans
> jamais consulter la liste des vhosts. Or c'est exactement ce que pose le relais :
> `header_up Host {upstream_hostport}` (`deploy/10-web.sh` l. 215), soit `127.0.0.1:<port>`.
> Le chemin public ne dépend donc pas de ce drapeau. Vérifié sur un nœud local : avec
> `--http.vhosts "explorer.coinbosa.com"` **comme** avec `localhost,127.0.0.1`, une requête
> portant `Host: 127.0.0.1:<port>` reçoit `200`. Ce drapeau ne protège plus que les accès
> faits **par nom d'hôte** : avec la liste minimale, `Host: evil.example.com` reçoit
> `403 invalid host specified`. Y ajouter le domaine public n'ouvrirait donc rien d'utile et
> élargirait la seule surface qui reste gardée. On garde la liste minimale.

### Séparation des rôles

En développement, une même clé peut sceller et gouverner. **Dans le genesis de production, les
deux adresses sont déjà distinctes** — `genesis-reference.json` : validateur
`0x3986D6b3…Bba50`, gouverneur `0x1EEf3830…4082A` — et `build-genesis.js` refuse de produire un
genesis de production où elles seraient égales. La phrase « une clé unique peut tout faire »,
qui figurait ici, ne décrit plus la chaîne lancée ; la laisser aurait fait chercher un risque
déjà traité, au détriment de ceux qui restent.

Ce qui reste réellement à durcir :

- **`GOVERNOR`** : aujourd'hui une adresse **unique**, dérivée d'un portefeuille matériel.
  Cible : multi-signatures (Safe) avec délai. À faire **avant** toute valeur réelle — la
  constante est gravée dans le bytecode du bloc 0, elle ne se remplace pas (voir
  `docs/GENESIS-PRODUCTION.md`).
- **Clé de scellage** : une par validateur, générée sur son serveur, jamais partagée. Elle ne
  gouverne rien et ne doit détenir aucun fonds.
- **Trésorerie** : les 13 postes de `genesis/distribution-addresses.json`, à passer sous
  multi-signatures.

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

1. Mettre le `GOVERNOR` et la trésorerie sous multi-signatures et timelock. La séparation
   d'**adresses** gouverneur / scellage, elle, est déjà faite (`genesis-reference.json`).
2. Sortir la clé de scellage du processus validateur (signeur distant / HSM). **Ne pas retirer
   `--unlock` avant que le signeur réponde** : le scellage s'arrêterait sans message d'erreur
   au démarrage (voir plus haut).
3. Fermer le RPC — **fait, et vérifié sur le service public**. Mesure du 3 septembre 2026, en
   lecture seule sur `https://explorer.coinbosa.com/rpc` : `admin_*`, `debug_*`, `txpool_*`,
   `personal_*`, `parlia_*` et `web3_clientVersion` répondent tous « does not exist/is not
   available » ; seuls `eth` et `net` sont servis, et `eth_accounts` rend une liste vide.
   Reste à confirmer la limitation de débit (`deploy/70-limitation-debit.sh`) et la
   surveillance des abus.
4. Passer à plusieurs validateurs indépendants — **jamais** par un appel manuel à
   `updateValidatorSet` : `scripts/rotate-validators.js` uniquement. À traiter d'abord : le
   `SlashIndicator` hérité (0x…1001) est incompatible avec `CoinbosaValidatorSet` (pas de
   `misdemeanor` / `felony`), l'appel de sanction revert et l'erreur n'est que journalisée —
   donc aucune sanction n'est appliquée. Sans conséquence à N=1 ; bloquant dès N≥2.
5. Anti-DoS : prix de gas minimal, bornes mempool. (`--rangelimit`, `--rpc.logquerylimit 20`
   et les lots bornés sont déjà posés côté nœud.)
6. Marqueur d'identité réseau distinct.
7. Supervision (hauteur de bloc, fork, disque, TLS) et plan d'incident : `deploy/50-monitoring.sh`
   existe. Reste à confirmer qu'il est bien déployé et qu'un canal d'alerte aboutit réellement.
8. Audit de sécurité **externe** avant toute mise en valeur du réseau.
