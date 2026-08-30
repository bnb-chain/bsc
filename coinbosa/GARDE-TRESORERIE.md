# La garde des 700 000 000 BOSA

Ce document décrit **comment l'offre de Coinbosa Chain est détenue aujourd'hui**, ce que
cette structure implique, et ce qui n'a pas encore été fait. Il est écrit pour être lu par
une équipe risque : il n'y a rien à en retrancher pour publication, et rien n'y est présenté
comme meilleur qu'il ne l'est.

Chaque fait de ce document a été produit par une commande. Les commandes sont réunies à la
section 8, et sont exécutables par un tiers depuis le point d'accès public
`https://explorer.coinbosa.com/rpc`.

**État observé au bloc 403 400, le 2026-08-30T22:13:29Z.**

---

## 1. En une page

| | |
|---|---|
| Offre native | 700 000 000 BOSA, fixée au bloc 0, non émettable ensuite |
| Nombre de détenteurs | **15** comptes, dont 13 postes de répartition |
| Réconciliation | **exacte au wei** — écart 0 avec l'offre déclarée |
| Nature des 13 postes | **13 clés simples**. `eth_getCode` renvoie **0 octet** sur chacune : aucun contrat, donc **ni multi-signatures, ni délai, ni blocage temporel** |
| Nature du gouverneur | **clé simple** également, 0 octet de code |
| Origine des clés | la procédure du dépôt dérive les 13 postes **et** le gouverneur d'une **seule graine**, en dérivation **non durcie**. Que les adresses déployées viennent bien de cette procédure reste une **déclaration de l'éditeur** — cela se confirme en une commande (§ 7.2) |
| Conséquence, si la procédure a été suivie | une seule phrase de récupération commande **la totalité de l'offre** et **la liste des validateurs** |
| Clé de scellage du validateur | **distincte**, sur le serveur, et **ne détient aucun fonds** (solde 0) |
| Mouvements depuis le bloc 0 | **un seul** : 1 000 BOSA du poste `equipe` vers le gouverneur, au bloc 160 399 |
| Rotations du jeu de validateurs | **aucune** depuis le bloc 1 |
| Acquisition progressive (*vesting*) | **aucune**. Aucun poste n'est bloqué ni libéré par palier |

C'est la première ligne de risque du dossier, et elle est assumée telle quelle plutôt que
présentée autrement.

---

## 2. La structure de garde, telle qu'elle est

### 2.1 Une seule graine, quatorze adresses

La procédure de création figure dans `scripts/derive-treasury-addresses.js`. Elle prend en
entrée une **clé publique étendue** (*xpub*) de compte Ethereum — `m/44'/60'/0'` — et en
dérive :

| Rôle | Chemin |
|---|---|
| 13 postes de répartition | `m/44'/60'/0'/0/0` à `m/44'/60'/0'/0/12` |
| Gouverneur du contrat système | `m/44'/60'/0'/0/13` |

Le script a été rejoué sur une graine **jetable** générée pour l'occasion, et le résultat
recoupé par un calcul indépendant : **14 adresses sur 14** correspondent exactement à
`m/44'/60'/0'/0/i`. La mécanique décrite est donc bien celle qui est implémentée.

Il en découle, sans ambiguïté : **telle qu'elle est écrite, cette procédure fait descendre
les treize postes et le gouverneur du même nœud de compte, donc de la même graine.** Une
phrase de récupération unique reconstitue les quatorze clés privées.

Un point de méthode, pour ne pas surinterpréter : ce qui précède établit ce que **fait le
script**, pas d'où viennent les adresses réellement inscrites au bloc 0 — aucune donnée de
la chaîne ne relie deux adresses à un même parent. La confirmation tient en une commande,
sans manipuler le moindre secret : rejouer le script avec le xpub réel (une clé **publique**)
et comparer les quatorze adresses obtenues à celles du genesis et au gouverneur lu sur la
chaîne. Tant que ce contrôle n'a pas été produit, l'origine commune est une **déclaration**,
et ce document la traite comme telle. Le reste de cette section décrit ses conséquences si
elle est exacte — et l'ensemble des indices disponibles (procédure documentée, gouverneur
distinct du validateur, absence de tout porte-clés de trésorerie sur le serveur) est
cohérent avec elle.

Le script n'accepte qu'un *xpub* : il refuse une clé privée ou une phrase de récupération, et
aucune clé privée n'existe côté logiciel du fait de son exécution. C'est une bonne propriété,
mais elle porte sur l'**outil**, pas sur la **garde** : elle ne dit rien de l'endroit où la
graine se trouve réellement (voir section 5).

### 2.2 La dérivation est non durcie — ce que cela ajoute au risque

Les chemins `0/i` sont **non durcis** (pas d'apostrophe). C'est une propriété standard de
BIP-32, et elle a une conséquence rarement énoncée :

> Quiconque détient **le xpub de compte** *et* **une seule clé privée enfant** peut
> reconstituer la clé privée du compte, et donc **les quatorze clés** — les treize postes de
> trésorerie **et** le gouverneur.

Ce n'est pas une hypothèse. La reconstitution a été exécutée sur une graine jetable, en
partant du xpub et de la seule clé de l'index `0/3` : la clé de compte est retrouvée, et les
14 clés `0/0` à `0/13` sont reconstruites, gouverneur compris.

Deux effets pratiques :

1. **Le xpub n'est pas un objet anodin.** Le commentaire en tête de
   `scripts/derive-treasury-addresses.js` invite à le poser « dans un ticket — sans risque ».
   Cette affirmation n'est vraie que tant qu'aucune clé enfant ne fuit. Combinés, xpub et une
   clé enfant valent la graine.
2. **Le cloisonnement entre postes est nul.** La compromission d'un seul poste — le plus
   petit, `audit`, 14 000 000 BOSA — n'est pas la perte de ce poste : c'est la perte de tout,
   dès lors que le xpub est également accessible à l'attaquant.

### 2.3 Ce qui est correctement séparé

La **clé de scellage du validateur** est un objet distinct, généré sur le serveur, qui n'est
pas issu de la graine de trésorerie. Vérifié :

- son adresse `0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50` a un **solde nul** ;
- le seul porte-clés présent sur le serveur de production contient **ce seul fichier** ;
- aucun porte-clés de trésorerie, aucune graine, aucun *xpub* n'a été trouvé sur le serveur.

Autrement dit : **la compromission du serveur ne donne pas accès aux fonds.** Elle donne la
production des blocs, ce qui est grave, mais c'est un autre périmètre. Cette séparation-là
existe réellement.

À noter, sans détour : le nœud validateur tourne avec `--unlock`, `--allow-insecure-unlock`
et un mot de passe en clair dans un fichier du serveur. C'est une clé chaude assumée ; elle
ne porte aucune valeur, mais elle produit les blocs.

### 2.4 Ce que le gouverneur peut, et ce qu'il ne peut pas

Le gouverneur est une **constante du bytecode** du contrat système `0x…1000`, lui-même inscrit
dans le bloc 0.

Il peut :
- `updateValidatorSet` — remplacer le jeu de validateurs, sous la seule contrainte que le
  validateur de genèse y reste ;
- `sweepSurplus` — retirer les fonds arrivés sur le contrat système hors du chemin de dépôt.

Il **ne peut pas** :
- créer de la monnaie. Aucune fonction d'émission n'existe, ni dans le contrat, ni dans le
  moteur de consensus. L'offre est close ;
- déplacer les fonds des treize postes. Ceux-ci ne sont accessibles qu'avec leurs propres
  clés — lesquelles, il est vrai, descendent de la même graine.

Il **ne peut pas non plus être remplacé.** `GOVERNOR` est déclaré `constant` ; le contrat vit
dans le genesis ; modifier son bytecode changerait la racine d'état du bloc 0, donc l'identité
de la chaîne. **Cette adresse gouverne le réseau pour toute sa durée de vie.** Sa perte fige
définitivement le jeu de validateurs ; sa compromission livre la production des blocs.

---

## 3. Les détenteurs, et la réconciliation de l'offre

Relevé au bloc 403 400. Colonne « code » : taille du bytecode à l'adresse — **0 signifie une
clé unique**, un multi-signatures y porterait plusieurs milliers d'octets. Colonne « nonce » :
nombre de transactions jamais émises par cette clé.

| Poste | Part | Adresse | Solde (BOSA) | Code | Nonce |
|---|---|---|---|---|---|
| `developpement` | 20 % | `0xCa6f08e549290BbF161fF45c475fd3f7A6e65f04` | 140 000 000 | 0 | 0 |
| `technique` | 10 % | `0xf4cEbe2d34A9a996cAD0c02345d6c3fB69B0E6C1` | 70 000 000 | 0 | 0 |
| `recherche` | 10 % | `0xb3B91c44f7D48e814aC37c3ED3C691eEDd728b1b` | 70 000 000 | 0 | 0 |
| `equipe` | 10 % | `0x41Ab22491Ba87eda15927286D744ebdaAE5B2FC9` | 69 998 999,999979 | 0 | 1 |
| `fondsFinancierCard` | 10 % | `0x59dcf9E2A5C17D6C32dC00feCdd8419954494E3f` | 70 000 000 | 0 | 0 |
| `fondsLiquidite` | 10 % | `0xF85C43a06032F557323545dC3353f31dF1fBDD65` | 70 000 000 | 0 | 0 |
| `rechercheIA` | 10 % | `0x7a8E70400Af9b66E22cefF574Dba9B293f3Ca6b5` | 70 000 000 | 0 | 0 |
| `rechercheFinanceFintech` | 5 % | `0x6baA7353Ed90dACB4d6C1A2DA53cbf77DF7F2E32` | 35 000 000 | 0 | 0 |
| `securite` | 3 % | `0x31CAD23D872c4cf7Eb22FC4B27f3094654b95DF8` | 21 000 000 | 0 | 0 |
| `audit` | 2 % | `0x223C546d25032E209556e9607041F0A1EFe4674D` | 14 000 000 | 0 | 0 |
| `evenementsFormation` | 2 % | `0xd53de8724Fef3Dc24bF12a34adEf68c3Cd30c07E` | 14 000 000 | 0 | 0 |
| `distributionPublique` | 5 % | `0x47f0c3e1D2c9EA164986c58612CafD39bb89ED41` | 35 000 000 | 0 | 0 |
| `reserveStrategique` | 3 % | `0x69B3C57Ba943c31489Eb6A1d7727f550B42512F8` | 21 000 000 | 0 | 0 |
| **Gouverneur** | — | `0x1EEf3830833d83AcD3152A511853fd04a0b4082A` | 1 000 | 0 | 0 |
| **Contrat système** | frais | `0x0000000000000000000000000000000000001000` | 0,000021 | 6 060 | — |

**Douze des treize clés de trésorerie n'ont jamais signé quoi que ce soit** (nonce 0). Ce
détail compte pour la suite : toute mise sous multi-signatures suppose de les faire signer
pour la première fois.

### 3.1 Le total tombe exactement juste

```
somme des 15 comptes : 700 000 000 000 000 000 000 000 000 wei
offre déclarée       : 700 000 000 000 000 000 000 000 000 wei
écart                :                                    0 wei
```

L'exactitude de cette somme n'est pas une coïncidence arithmétique, elle **ferme la
question du détenteur caché** :

1. l'empreinte du bloc 0 observée sur la chaîne est identique à la référence figée et
   publiée dans le dépôt (`genesis/genesis-reference.json`) : l'allocation initiale est donc
   celle qui est publiée, et pas une autre ;
2. le moteur de consensus ne crée pas de monnaie, et rien n'est brûlé (la base de frais vaut
   zéro) : l'offre totale est constante ;
3. si quinze comptes totalisent l'offre entière, **tous les autres comptes de la chaîne sont
   à zéro**.

Aucune énumération de comptes n'est possible en JSON-RPC ; c'est ce raisonnement, et non une
liste, qui établit le résultat.

### 3.2 L'unique mouvement de l'histoire de la chaîne

| | |
|---|---|
| Bloc | 160 399, le 2026-08-16T20:42:33Z |
| Transaction | `0xb10cf391c74a81336e7e4037f84e30ceacab52a59d239452453360b5a9790544` |
| De | `0x41Ab22491Ba87eda15927286D744ebdaAE5B2FC9` (poste `equipe`) |
| Vers | `0x1EEf3830833d83AcD3152A511853fd04a0b4082A` (gouverneur) |
| Montant | 1 000 BOSA |
| Frais | 0,000021 BOSA, portés par le contrat système, dus au validateur |

Objet : approvisionner le gouverneur en gaz, celui-ci n'ayant reçu aucune allocation au
genesis. Conséquences à énoncer plutôt qu'à laisser découvrir :

- le poste `equipe` détient **69 998 999,999979 BOSA**, soit **1 000,000021 BOSA de moins**
  que les 10 % annoncés ;
- le gouverneur, dont `docs/GENESIS-PRODUCTION.md` écrit qu'« elle ne détient aucun fonds
  (vérifié : solde nul) », **détient 1 000 BOSA**. Cette phrase du dépôt est périmée ;
- l'inventaire complet des événements du contrat système sur toute la chaîne ne contient
  que **deux entrées** : l'initialisation du bloc 1, et le dépôt de frais de ce
  bloc 160 399. **Le gouverneur n'a jamais agi**, et `sweepSurplus` n'a jamais été appelé.

---

## 4. Absence d'acquisition progressive

Aucun poste ne fait l'objet d'un blocage temporel ni d'une libération par paliers : ces
mécanismes supposeraient un contrat, et les treize adresses n'en portent aucun. Les
140 000 000 BOSA du poste `developpement` comme les 70 000 000 du poste `equipe` sont
mobilisables immédiatement, à la seule discrétion du détenteur de la graine.

`TOKENOMICS.md` relève par ailleurs qu'un poste « équipe » disponible immédiatement est un
signal d'alerte usuel. Il l'est ici, et rien dans l'état actuel ne le corrige.

---

## 5. Ce que ce document n'établit pas

La distinction est volontaire : ce qui suit relève de la **déclaration de l'éditeur**, pas de
la vérification, et doit être traité comme tel.

| Point | Statut |
|---|---|
| Les quatorze adresses déployées viennent-elles réellement de la procédure documentée, donc d'une graine unique ? | **Non vérifié.** Aucune donnée de la chaîne ne relie deux adresses à un même parent. Se confirme sans secret, en rejouant le script avec le xpub réel (§ 7.2). |
| La graine est-elle née sur un portefeuille matériel, sans jamais toucher un ordinateur ? | **Non vérifié.** La procédure du dépôt le prescrit, et le script de dérivation refuse toute entrée secrète. Mais aucune donnée de la chaîne ne distingue une signature matérielle d'une signature logicielle. |
| Où la graine se trouve-t-elle physiquement, et en combien d'exemplaires ? | **Non vérifié.** Hors de portée d'une observation technique. |
| Une restauration de la graine a-t-elle été testée sur un appareil vierge ? | **Non vérifié.** Aucune trace dans le dépôt. |
| Combien de personnes peuvent atteindre la graine ? | **Non vérifié.** |
| Le xpub de compte a-t-il été diffusé, et à qui ? | **Non vérifié.** Il n'apparaît ni dans le dépôt (recherche `xpub6` : aucune occurrence hors documentation du script), ni sur le serveur de production (recherche par nom et par contenu : aucune). Sa circulation hors de ces deux endroits est hors de portée d'une observation technique. |

Ce qui **a** été vérifié sur le serveur de production : il n'y porte **aucune** clé de
trésorerie, aucun *xpub*, aucune graine. Le seul secret présent est la clé de scellage, qui
ne détient aucun fonds.

---

## 6. Ce qui n'est pas fait avant cotation, et pourquoi

La mise de l'offre sous multi-signatures est inscrite comme **bloquante** dans `ROADMAP.md`.
Elle ne sera pas faite avant cotation. La raison n'est pas le confort : c'est que l'exécuter
dans l'urgence créerait un risque supérieur à celui qu'elle corrige. Les éléments, tous
vérifiés :

1. **Aucune infrastructure multi-signatures n'existe sur cette chaîne.** `eth_getCode`
   renvoie **0 octet** aux adresses canoniques de Safe 1.3.0 et 1.4.1 (singleton, fabrique de
   procurations, gestionnaire de repli, MultiSend), ainsi qu'au déployeur déterministe
   `0x4e59…4956C`, à Multicall3 et à CreateX. Tout serait à déployer, et **les adresses
   obtenues ne coïncideraient pas** avec celles que reconnaissent les outils tiers.
2. **Le point d'accès public ne peut pas porter ce déploiement.** Le code de création du
   singleton Safe 1.4.1 pèse **23 620 octets**, soit une transaction signée d'environ
   **46 Ko**. Le relais `/rpc` plafonne le corps des requêtes à **32 Ko**. Le déploiement
   devrait donc être émis depuis le serveur de production lui-même — c'est-à-dire en
   manipulant la machine qui fait tourner **l'unique validateur**.
3. **Aucun outil de signature n'existe pour cette chaîne.** La configuration publique de Safe
   (`safe-config.safe.global`) énumère **53 réseaux** ; le chainId **26262 n'en fait pas
   partie**. Il n'y a donc ni interface, ni service de transactions, ni collecte de signatures.
   Chaque signature passerait par des scripts écrits pour l'occasion, sous contrainte de
   calendrier, sans historique d'exécution.
4. **Douze des treize clés n'ont jamais signé.** Les mettre sous coffre suppose treize
   transferts irréversibles portant l'intégralité de l'offre, exécutés par des clés dont
   aucune n'a jamais servi.
5. **Il n'existe aucune transaction corrective possible.** Le réseau a **un** validateur. Si
   la chaîne s'arrête pendant l'opération, plus aucun bloc n'est produit, donc plus aucune
   correction n'est minable. Un coffre mal paramétré contenant 700 000 000 BOSA ne se défait
   pas.

**Conclusion assumée :** avant cotation, la mise sous multi-signatures ne réduirait pas le
risque, elle le déplacerait vers un risque plus grand et irréversible. Elle doit venir après,
après répétition intégrale sur un réseau jetable.

### Le rôle du gouverneur ne peut pas être déplacé sur une autre graine

Cette mesure est souvent proposée, et elle est **techniquement impossible ici** : `GOVERNOR`
est une constante gravée dans le bytecode du bloc 0, et aucune fonction ne permet de la
changer. Séparer la monnaie du consensus ne peut donc pas passer par le déplacement du
gouverneur.

Elle ne peut passer que par le **déplacement de la trésorerie** vers des adresses issues
d'une **seconde** graine, le gouverneur restant sur la première. C'est une opération plus
simple qu'un coffre multi-signatures — treize transferts vers treize adresses ordinaires,
sans contrat à déployer, sans outillage à écrire — mais elle reste irréversible et porte
l'intégralité de l'offre. Elle relève de la même catégorie de décision, et non d'un correctif
de dernière minute.

---

## 7. Ce qui est prévu, dans cet ordre

Aucune date n'est donnée : une échéance annoncée puis manquée vaut moins que rien.

**Sans aucune transaction, sans toucher au réseau :**

1. **Publier ce document** et les quinze adresses. La structure de garde devient vérifiable
   par un tiers, y compris dans ce qu'elle a de défavorable.
2. **Confirmer la filiation des adresses** avec `scripts/check-treasury-derivation.js`, qui
   compare les quatorze adresses dérivées du xpub réel à celles inscrites au bloc 0 et au
   gouverneur lu sur la chaîne. Un xpub est une clé **publique** : ce contrôle ne manipule
   aucun secret, n'émet aucune transaction, n'écrit aucun fichier. Le résultat confirme la
   structure décrite ici, ou la contredit — les deux sont utiles.
3. **Tester la restauration de la graine** sur un appareil vierge et hors ligne. Aujourd'hui,
   la perte de cette graine est la perte de l'offre entière et le gel définitif du jeu de
   validateurs. Une sauvegarde jamais restaurée n'est pas une sauvegarde.
4. **Traiter le xpub comme un élément sensible** : le retirer des canaux ordinaires, et
   corriger le commentaire du script qui le présente comme inoffensif.
5. **Corriger les affirmations périmées du dépôt** : le solde nul du gouverneur, et le
   contrôle `scripts/check-supply.js` qui annonce un ÉCHEC d'offre depuis le transfert du
   bloc 160 399 alors que l'offre est intacte. `scripts/check-custody.js` réconcilie
   correctement et doit être le contrôle de référence.

**Après cotation, chacune précédée d'une répétition intégrale sur un réseau jetable :**

6. Coffre multi-signatures pour les **postes les plus gros d'abord** — `developpement`
   (140 000 000 BOSA, 20 % de l'offre) en tête. Un coffre déployé, une position déplacée, une
   vérification publique, avant d'en faire un deuxième.
7. Déplacement du reste de la trésorerie vers une **seconde graine**, distincte de celle qui
   porte le gouverneur, pour que la monnaie et le consensus cessent de tomber ensemble.
8. Blocage temporel des postes qui doivent l'être — au premier chef `equipe` — ce qui suppose
   un contrat, donc les étapes précédentes.

Le passage à plusieurs validateurs relève d'un autre chantier (`coinbosa/POBS-ACTIVATION.md`)
et comporte ses propres conditions d'arrêt de chaîne.

---

## 8. Vérifier soi-même

Toutes ces commandes sont en **lecture seule** et fonctionnent depuis n'importe quelle machine.

```bash
# 1. La chaîne est bien celle qui est publiée : empreinte du bloc 0 contre référence figée
cd coinbosa && RPC=https://explorer.coinbosa.com/rpc node scripts/check-genesis-hash.js

# 2. Réconciliation complète de la garde : détenteurs, nature des clés, total au wei,
#    et inventaire de TOUS les événements du contrat système depuis le bloc 0
cd coinbosa && RPC=https://explorer.coinbosa.com/rpc node scripts/check-custody.js
```

Contrôle de filiation — réservé à l'éditeur, puisqu'il exige le xpub. Un xpub est une clé
**publique** : ce contrôle ne manipule aucun secret et n'émet aucune transaction. `read -rs`
tient le xpub hors de l'historique du shell.

```bash
cd coinbosa
read -rsp 'xpub de compte : ' XPUB && echo
XPUB="$XPUB" RPC=https://explorer.coinbosa.com/rpc node scripts/check-treasury-derivation.js
unset XPUB
```

Attendu si la procédure du dépôt a bien été suivie : `postes conformes : 13/13`,
`écarts : 0`, `gouverneur : conforme`, et code de sortie 0.

⚠ Ce contrôle est **lecture seule**, mais ne jamais lancer `scripts/derive-treasury-addresses.js`
avec `ECRIRE=1` : cette variable réécrit `genesis/distribution-addresses.json`.

Contrôles ponctuels, sans le dépôt :

```bash
R=https://explorer.coinbosa.com/rpc

# le solde d'un poste (ici developpement)
curl -s -X POST $R -H 'content-type: application/json' -d '{"jsonrpc":"2.0","id":1,
  "method":"eth_getBalance","params":["0xCa6f08e549290BbF161fF45c475fd3f7A6e65f04","latest"]}'

# la présence de code : "0x" = clé simple, pas de multi-signatures
curl -s -X POST $R -H 'content-type: application/json' -d '{"jsonrpc":"2.0","id":1,
  "method":"eth_getCode","params":["0xCa6f08e549290BbF161fF45c475fd3f7A6e65f04","latest"]}'

# le gouverneur, lu dans le bytecode figé du contrat système (sélecteur de GOVERNOR())
curl -s -X POST $R -H 'content-type: application/json' -d '{"jsonrpc":"2.0","id":1,
  "method":"eth_call","params":[{"to":"0x0000000000000000000000000000000000001000",
  "data":"0x6dc0ae22"},"latest"]}'

# l'unique transfert de l'histoire de la chaîne
curl -s -X POST $R -H 'content-type: application/json' -d '{"jsonrpc":"2.0","id":1,
  "method":"eth_getTransactionByHash","params":
  ["0xb10cf391c74a81336e7e4037f84e30ceacab52a59d239452453360b5a9790544"]}'
```

---

## 9. Résumé du risque, sans atténuation

- Si la procédure du dépôt a été suivie — ce qui reste à confirmer par la commande de la
  section 7.2 — une **phrase de récupération unique** commande 700 000 000 BOSA et la liste
  des validateurs. Sa perte est définitive ; sa compromission est totale.
- La **dérivation non durcie** supprime tout cloisonnement entre les treize postes : le xpub
  et **une seule** clé enfant reconstituent l'ensemble, gouverneur compris.
- L'adresse du gouverneur est **gravée dans le bloc 0** et ne peut jamais être remplacée.
- La chaîne repose sur **un validateur**, dont la clé de scellage est chaude sur le serveur.
  Cette clé ne porte aucun fonds, mais son arrêt arrête le réseau, et un réseau arrêté ne
  peut plus être corrigé par une transaction.
- Aucun poste n'est soumis à un **blocage temporel**.

Ces cinq points sont les vrais. Ils sont écrits ici pour qu'une équipe risque n'ait pas à
les découvrir seule.
