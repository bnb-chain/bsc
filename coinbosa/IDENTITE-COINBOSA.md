# Prouver que cette chaîne est la nôtre

**Objet.** Un homonyme existe. Ce document ne traite pas ce fait comme un litige : il le
traite comme une **voie d'attaque**. Une fois BOSA public, des gens enverront des fonds au
mauvais « Coinbosa », suivront le mauvais canal, croiront la mauvaise annonce. Le document
donne de quoi **prouver à n'importe quel tiers** — place d'échange, agrégateur, utilisateur —
que cette chaîne-ci est celle de l'éditeur, et de le prouver **sans avoir à nous croire**.

> **Avertissement, dit une fois.** Tout ce qui relève du droit des marques, de la
> concurrence ou de la conformité est **hors du périmètre de ce document** : *point juridique,
> à soumettre à un conseil*. Rien de ce qui suit n'est un avis juridique et rien n'y qualifie
> un tiers. Ce document ne fait que **mesurer** et **outiller la preuve**.

**Méthode.** Tout ce qui est chiffré ou cité ici a été mesuré le **2026-09-03** contre le RPC
public `https://explorer.coinbosa.com/rpc` (lecture seule), le DNS public, les API publiques
de `chainid.network`, CoinGecko et GitHub, et les fichiers du dépôt. Chaque affirmation porte
la commande qui la reproduit. Là où une source ne dit rien, c'est écrit.

**Documents voisins, à ne pas dupliquer.** La détection et la réponse aux imitations vivent
dans `SURVEILLANCE-LANCEMENT.md` (§ 6). Les manques du dossier de cotation vivent dans
`DOSSIER-COTATION.md` (§ 7, § 8, § 15). Les guichets et leurs exigences vivent dans
`PLAN-AGREGATEURS.md`. Ce document ne les recopie pas : il y renvoie, et signale au § 5 **une
ligne de `SURVEILLANCE-LANCEMENT.md` devenue fausse**.

---

## 1 — Ce qui est déjà infalsifiable, et pourquoi

### La preuve la plus forte qui existe, et elle est déjà en place

L'empreinte du **bloc 0** est le hachage Keccak-256 de l'en-tête de genèse. Cet en-tête
contient, entre autres champs, la **racine d'état** — qui engage cryptographiquement *chaque
wei* de l'allocation initiale — et l'**`extraData`**, qui contient l'adresse du validateur de
genèse. Changer un seul bit de l'un de ces champs change l'empreinte.

La conséquence tient en une phrase : **personne d'autre ne peut servir cette empreinte sans
faire tourner exactement cette chaîne** — et s'il fait tourner exactement cette chaîne, il
sert le genesis de l'éditeur, pas le sien. Un homonyme peut prendre le nom, le logo, le
ticker, un domaine voisin. Il ne peut pas prendre l'empreinte.

| Ancre | Valeur | Comment un tiers l'obtient |
|---|---|---|
| chainId décimal | **26262** | `net_version` |
| chainId hexadécimal | **0x6696** | `eth_chainId` |
| Empreinte du bloc 0 | `0x8dcdadc247a98f33728cae944e20ce7c49c74b35cfba31495f85e98979018da6` | `eth_getBlockByNumber("0x0")`, champ `hash` |
| Racine d'état du bloc 0 | `0x93682eb9182a55531d47014b76a285b45d3e720a2951f9ffbdc67f52995f8c03` | idem, champ `stateRoot` |
| Validateur de genèse | `0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50` | idem, octets 33 à 52 de `extraData` |
| Empreinte SHA-256 du fichier genesis | `4d93164f2364323d0156b8a1255dea060a10496459d755389a51babab75b7ce7` | `shasum -a 256 genesis/genesis-coinbosa.json` |

### La vérification en une requête

Un tiers colle ceci et n'a besoin de rien d'autre — ni compte, ni clé d'API, ni outil
installé au-delà de `curl` et `python3` :

```bash
curl -s -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x0",false]}' \
  https://explorer.coinbosa.com/rpc \
| python3 -c "import sys,json;b=json.load(sys.stdin)['result'];e=bytes.fromhex(b['extraData'][2:]);\
print('genesis   :',b['hash']);print('stateRoot :',b['stateRoot']);\
print('validateur: 0x'+e[33:53].hex());print('cle BLS   : 0x'+e[53:101].hex())"
```

Sortie obtenue le 2026-09-03, à comparer au tableau ci-dessus :

```
genesis   : 0x8dcdadc247a98f33728cae944e20ce7c49c74b35cfba31495f85e98979018da6
stateRoot : 0x93682eb9182a55531d47014b76a285b45d3e720a2951f9ffbdc67f52995f8c03
validateur: 0x3986d6b31ec55043ceaaf25f5ddea53517cbba50
cle BLS   : 0x000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000
```

Les 96 zéros de la dernière ligne sont les 48 octets de la clé BLS : elle est **nulle**.

### Pourquoi l'`extraData` compte autant que l'empreinte

L'`extraData` du bloc 0 fait **166 octets**, découpés selon le format Parlia post-Luban :

| Décalage | Longueur | Contenu mesuré |
|---|---|---|
| 0 | 32 | vanité, tous octets nuls |
| 32 | 1 | nombre de validateurs : **1** |
| 33 | 20 | adresse : `0x3986d6b31ec55043ceaaf25f5ddea53517cbba50` |
| 53 | 48 | clé publique BLS du validateur : **tous octets nuls** |
| 101 | 65 | sceau, tous octets nuls (genesis non scellé) |

32 + 1 + (20 + 48) + 65 = **166**. Le découpage n'est pas une hypothèse : il est contraint par
la longueur totale.

Deux faits en sortent, et les deux servent ce document :

1. **L'adresse `0x3986…ba50` est à l'intérieur de la préimage du bloc 0.** C'est le lien le
   plus fort qui puisse exister entre une adresse et une chaîne : on ne peut pas l'ajouter
   après coup, on ne peut pas la contester, et on la lit en une requête. C'est ce qui fait
   d'elle le signataire naturel de la déclaration du § 3.
2. **L'emplacement de clé BLS est nul dans le genesis.** C'est la contrepartie, côté
   configuration, du fait déjà établi que `finalized` répond bloc 0 alors que la tête est à
   plus de 453 000 : `lubanBlock` et `platoBlock` valent 0, le mécanisme de finalité rapide
   est donc activé, mais aucune clé de vote n'est inscrite au genesis et aucun processus
   `geth` ne porte de drapeau de vote. **Le dispositif est en place, personne ne vote.**
   Ce point ne concerne pas l'identité — il concerne la sécurité, et il est rappelé au § 6
   pour qu'on ne vende jamais l'un pour l'autre.

### L'ancre qui ne demande même pas de nous faire confiance

Le fichier genesis est **reconstructible octet pour octet depuis le dépôt public**, à partir
des deux seules adresses publiques (validateur, gouverneur) — reconstruction établie et
publiée dans `DOSSIER-COTATION.md` § 1 :

```bash
VALIDATOR=0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50 \
GOVERNOR=0x1EEf3830833d83AcD3152A511853fd04a0b4082A \
OUT=/tmp/genesis-rebuilt.json node scripts/build-genesis.js
shasum -a 256 /tmp/genesis-rebuilt.json
# 4d93164f2364323d0156b8a1255dea060a10496459d755389a51babab75b7ce7
```

Un tiers qui refuse de croire le RPC peut donc : reconstruire le genesis depuis le dépôt,
l'initialiser lui-même avec `geth init`, obtenir l'empreinte du bloc 0, et la comparer à celle
servie par le RPC. Les deux coïncident. **Aucune allocation cachée n'est possible**, et aucun
homonyme ne peut produire un genesis qui se reconstruit depuis *ce* dépôt avec *ces* adresses.

### Ce que cette ancre ne prouve pas

Elle prouve qu'**une chaîne est celle-ci**. Elle ne dit rien de **qui en est l'éditeur** :
l'empreinte du bloc 0 ne porte aucun nom, aucune société, aucun domaine. Le lien entre
« cette chaîne » et « coinbosa, Inc. » ne peut venir que d'une signature contrôlée par
l'éditeur, adossée à une adresse elle-même ancrée dans la chaîne. C'est exactement l'objet du
§ 3.

---

## 2 — L'état des lieux : ce qui tient, ce qui manque

### Ce qui est publié et joignable

| Élément | Valeur | Mesure du 2026-09-03 |
|---|---|---|
| Site | `https://coinbosa.com` | HTTP **200**, HSTS `includeSubDomains`, CSP stricte, certificat Let's Encrypt valide jusqu'au 2026-10-24 |
| Explorateur | `https://explorer.coinbosa.com` | HTTP **200**, même adresse IP que le site (`168.231.113.53`) |
| RPC public | `https://explorer.coinbosa.com/rpc` | répond ; `eth_chainId` → `0x6696`, tête au bloc `0x6f2c7` (**455 367**) |
| Livre blanc | `https://coinbosa.com/whitepaper/` | HTTP **200** |
| `security.txt` | `https://coinbosa.com/.well-known/security.txt` | HTTP **200** (RFC 9116), expire le 2027-07-26 |
| Registre des chaînes | entrée **26262** de `chainid.network/chains.json` | **présente et complète** sur 2 745 chaînes : `Coinbosa Chain`, `shortName: bosa`, RPC, explorateur, `nativeCurrency BOSA` |
| Dépôt | `https://github.com/Coinbosa/coinbosa-chain` | HTTP **200**, licence LGPL-3.0, dernier envoi 2026-09-02 |
| Organisation GitHub | `Coinbosa` | créée le **2022-05-11** — antériorité publique et datée, indépendante de nous |

**Unicité mesurée dans le registre des chaînes.** Sur les 2 745 entrées, `shortName: bosa`,
`chain: BOSA` et `nativeCurrency.symbol: BOSA` sont portés par **une seule entrée : la
nôtre**. Le seul autre nom contenant « bosa » est `BOSagora Mainnet` (chainId 2151), qui ne
partage ni ticker ni nom. Le chainId **262620 reste libre**.

**Aucun conflit de ticker chez CoinGecko.** `api.coingecko.com/api/v3/search?query=BOSA` et
la même requête sur `coinbosa` rendent toutes deux `{"coins":[],"exchanges":[],"icos":[],
"categories":[],"nfts":[]}` — liste **vide**. *(CoinMarketCap n'a pas été interrogé : sa
recherche publique n'est pas ouverte sans clé. La source ne dit donc rien pour ce guichet.)*

### Ce qui manque, ou qui se contredit

C'est la moitié utile de ce paragraphe. Les cinq écarts ci-dessous ne sont pas des finitions :
chacun est un point d'entrée pour une confusion, et chacun est vérifiable par un tiers.

#### a) TRANCHÉ le 2026-09-04 — et pire que prévu : un compte au nom du projet lui échappe

**Le compte officiel est `@coinbosa6476`** (« Coinbosa Group »), confirmé par l'éditeur et
déclaré depuis dans les trois sources du dépôt.

**`@coinbosacrypto` n'est PAS le compte du projet : l'éditeur en a perdu les accès.** Il existe,
il sert `<title>Coinbosa (@coinbosacrypto) / X</title>` et la description *« Coinbosa, the future
of the African fintech and Blockchain. »* — c'était donc bien un compte du projet, et il lui
échappe aujourd'hui. Un compte portant notre nom, hors de notre contrôle, est la situation
qu'un imitateur n'a même pas besoin de créer : elle existe.

**Et l'organisation GitHub du projet le déclare toujours** — `api.github.com/orgs/Coinbosa`
porte encore `twitter_username: coinbosacrypto`. Notre propre page publique sert d'aval à ce
compte. C'est le geste le plus urgent du dossier, et il ne prend que deux clics.

Le constat qui suit décrit l'état d'AVANT cet arbitrage ; il est conservé parce qu'il explique
comment on en est arrivé là.

Or le dépôt déclare l'inverse, à trois endroits :

| Fichier | Ligne | Contenu |
|---|---|---|
| `coinbosa.config.json` | 71 | `"twitter": ""` |
| `site/app.js` | 32 | `twitter: ""` |
| `explorer/app.js` | 46 | `twitter: '' // à remplir` |

Conséquences en chaîne : `PLAN-AGREGATEURS.md` (A5) et `PLAN-MARKETING.md` (A2) classent le
compte X comme « à créer » et le tiennent pour **bloquant** sur GeckoTerminal ; et
`SURVEILLANCE-LANCEMENT.md` (lignes 666-668) publie la phrase *« Aucun compte X / Twitter […]
Tout compte X se présentant comme Coinbosa est une imitation »*. **Cette phrase est fausse au
2026-09-03**, et telle quelle, la page anti-fraude du projet désavouerait le compte du projet.
Voir § 5.

**TRANCHÉ le 2026-09-04.** L'éditeur a confirmé que le compte existe et est le sien, et il est
désormais déclaré dans `coinbosa.config.json`, `site/app.js` et `explorer/app.js` — les trois
portaient un champ vide. La question ouverte ci-dessus est close.

#### b) Trois adresses Telegram circulaient — TRANCHÉ le 2026-09-04

**L'éditeur a confirmé que `t.me/Coinbosaofficial` est son groupe.** C'est désormais la seule
adresse Telegram déclarée, dans la configuration comme sur le site. Les deux autres ont été
retirées : `t.me/coinbosa` est un compte personnel, et `t.me/coinbosagroup` n'était pas un nom
réservé.

**Ce qui reste à faire, et que seul l'éditeur peut faire :** la description publique du groupe
annonce encore « Proof of authority », renvoie à `coinbosa.org` — un domaine qui ne résout pas —
et se dit exploitée par une « Coinbosa Foundation », quand l'éditeur est coinbosa, Inc.,
Delaware, et que le consensus est Parlia. Un tiers qui vérifie y verra trois contradictions.

Le constat qui suit est celui d'avant l'arbitrage ; il est conservé parce qu'il explique
pourquoi la description doit être corrigée.

| Adresse | Déclarée où | Ce que Telegram sert le 2026-09-03 |
|---|---|---|
| `t.me/coinbosa` | `coinbosa.config.json` (`telegram`), `explorer/app.js`, `site/app.js` — étiquetée **« canal officiel »** | titre `COINBOSA`, description *« You can contact @coinbosa right away »* → c'est un **compte utilisateur**, pas un canal |
| `t.me/coinbosagroup` | `coinbosa.config.json` (`telegramCommunity`) jusqu'au 2026-09-04, **RETIRÉ depuis** — et nulle part ailleurs | **aucun titre, aucune description** : la page générique servie pour un nom d'utilisateur libre. Rien n'indique qu'il soit détenu |
| `t.me/Coinbosaofficial` | `site/*.html` (6 langues), `explorer/app.js`, `whitepaper/*` — **seule adresse servie par le site en ligne** | groupe, **14 membres**, description : *« High performance blockchain based on **Proof of authority**. **https://coinbosa.org** — This account is owned and operated by the **Coinbosa Foundation**. This group is used for informational purposes only. »* |

Trois problèmes distincts, par ordre de gravité :

1. **Le seul canal que le site déclare officiel se présente sous un autre nom d'entité et un
   autre domaine.** `https://coinbosa.com/a-propos.html` écrit *« Les canaux officiels, et
   rien d'autre »* puis ne cite que `t.me/Coinbosaofficial` — dont la description publique dit
   « Coinbosa Foundation » et « coinbosa.org », quand le site, le livre blanc et le pied de
   page de toutes les pages disent **« coinbosa, Inc., Delaware »** et **coinbosa.com**. Un
   tiers qui suit le lien officiel lit deux entités et deux domaines. *`coinbosa.org` ne
   résout pas aujourd'hui : ni enregistrement A, ni serveur de noms, `curl` échoue à établir
   la connexion.* **Deux issues, et l'éditeur seul peut trancher en regardant la liste des
   administrateurs du groupe :** ou bien le groupe est le sien et sa description est un
   héritage à corriger immédiatement, ou bien il ne l'est pas et le site doit cesser de le
   déclarer officiel. Tant que ce n'est pas tranché, aucune déclaration signée ne doit
   nommer cette adresse.
2. **`t.me/coinbosagroup` était publié dans le dépôt sans signe de détention — retiré le 2026-09-04.** Un nom
   d'utilisateur Telegram publié dans une configuration publique et non réservé est une
   invitation : le premier venu peut le prendre et se réclamer du fichier de configuration du
   projet lui-même. **À réserver ou à retirer du fichier — pas à laisser tel quel.**
3. **`t.me/coinbosa` est étiqueté « canal » et n'en est pas un.** L'étiquette est fausse,
   et un canal d'annonces reste à créer si le projet en veut un.

#### c) Les liens ne concordent pas entre la configuration et les pages en ligne

| Lien | `coinbosa.config.json` | Servi par `https://coinbosa.com` |
|---|---|---|
| Facebook | `https://www.facebook.com/coinbosa` (HTTP 200) | **absent** de la page d'accueil et de `/a-propos.html` |
| X | `""` | absent |
| Telegram | `t.me/coinbosa` + `t.me/coinbosagroup` (jusqu'au 2026-09-04) | `t.me/Coinbosaofficial` uniquement — **c'est désormais aussi ce que déclare la configuration** |
| Dépôt | `github.com/Coinbosa/coinbosa-chain` | identique ✔ |

`PLAN-AGREGATEURS.md` (A4, A6) exige déjà cette mise en cohérence pour les guichets. Ici
l'enjeu est différent et plus dur : **des liens divergents rendent impossible toute
déclaration d'identité fermée**, puisqu'il n'existe pas de liste dont on puisse dire « ceci
est tout, et rien d'autre ».

#### d) Le domaine ne porte aucune assertion, et les adresses de contact publiées ne reçoivent pas

Mesures faites contre le résolveur public `1.1.1.1` :

| Enregistrement sur `coinbosa.com` | Résultat |
|---|---|
| `TXT` | **aucun** — le domaine ne porte aujourd'hui **aucune** assertion vérifiable, ni de vérification d'agrégateur, ni de vérification GitHub, ni SPF |
| `MX` | **aucun** |
| `TXT _dmarc` | **aucun** |
| `CAA` | **aucun** — aucune autorité de certification n'est restreinte pour ce domaine |
| `NS` | `ns1`–`ns4.resellerclub.com` |

Deux conséquences directes :

- **`security@coinbosa.com` est publié dans `deploy/static/security.txt` (RFC 9116) et
  `info@coinbosa.com` dans le profil GitHub de l'organisation, alors que le domaine n'a aucun
  `MX`.** À défaut de `MX`, un serveur expéditeur se rabat sur l'enregistrement `A`
  (`168.231.113.53`) ; le port 25 y est **injoignable depuis le poste de mesure**. *Réserve
  honnête : ce dernier point peut venir du réseau de mesure autant que du serveur. Ce qui ne
  dépend d'aucun poste, en revanche, c'est l'absence totale de `MX`.* Le canal par lequel un
  tiers signalerait une imitation est donc, au mieux, incertain.
- **L'absence de `TXT` est une bonne nouvelle opérationnelle** : rien n'entre en conflit avec
  l'enregistrement que le § 3 propose d'ajouter.

#### e) Des artefacts hérités, sous l'organisation officielle, contredisent le texte publié

L'organisation `Coinbosa` sur GitHub porte **4 dépôts publics**. Deux d'entre eux sont
antérieurs à la chaîne et lisibles par n'importe qui :

| Dépôt | Créé | Ce qu'un tiers y lit |
|---|---|---|
| `Coinbosa/smart-chain-token` | 2022-06-30 | un fichier `info` déclarant `"symbol": "Bosa"`, `"type": "BEP2"`, `"status": "active"`, `"explorer": "https://explorer.binance.org/asset/coinbosa"` |
| `Coinbosa/Coinbosa-Token-solana` | 2022-05-11 | malgré son nom, des contrats **NEO / NEP-5** (`NEP5.Contract`, `neo-ico-contracts.sln`, `Crowdsale.Contract`) |

`DOSSIER-COTATION.md` § 8 note que l'affirmation *« des jetons avaient aussi été émis sur BNB
Chain ; ce jeton n'existe plus »* est **invérifiable faute d'adresse publiée**. Le fichier
ci-dessus ne donne pas davantage d'adresse — un actif BEP2 est désigné par un symbole, pas par
une adresse de contrat — mais il déclare le statut **« active »**, sous l'organisation
officielle, en contradiction avec le texte publié. *(La page d'explorateur citée répond
aujourd'hui en erreur 500 ; la source ne permet donc pas de dire si l'actif existe encore.)*

Ce sont des artefacts du projet, pas d'un tiers. Ils n'en sont que plus dangereux : un
homonyme, ou n'importe qui, peut les brandir **sans mentir** sur leur provenance.

### Ce que tout cela donne, vu de l'extérieur

Aujourd'hui, un tiers diligent qui part de `coinbosa.com` et suit le seul canal que le site
déclare officiel trouve **deux noms d'entité et deux domaines**. Il ne trouve **aucune
signature**, **aucun enregistrement DNS d'identité**, **aucune page qui dise ce qui n'est
*pas* à nous**. La confusion qu'un homonyme pourrait exploiter n'a, pour l'essentiel, pas
besoin de lui : elle est **déjà publiée par nous**. C'est aussi la bonne nouvelle — ce qu'on a
créé, on peut le corriger seul, sans dépendre de personne.

---

## 3 — La déclaration signée

C'est la pièce maîtresse. Un homonyme peut copier un nom, un logo, un texte, un domaine
voisin. **Il ne peut pas produire une signature d'une adresse inscrite dans le genesis de
cette chaîne.**

### Le principe, en trois lignes

La signature de message Ethereum (**EIP-191**, dite `personal_sign`) hache
`"\x19Ethereum Signed Message:\n" + longueur + message`, puis signe. La vérification récupère
l'adresse du signataire depuis la signature et le message. Trois propriétés en découlent, et
ce sont elles qui font la valeur de la pièce :

1. **La préimage ne contient aucun chainId.** La vérification est donc **indépendante de toute
   chaîne** : le vérificateur n'a besoin d'aucun nœud Coinbosa, d'aucune connexion à nos
   serveurs, d'aucune permission.
2. **La vérification est hors ligne.** Message + signature + adresse suffisent. Le
   vérificateur peut les tenir de nous, d'un tiers, ou d'une capture d'écran qu'il retape.
3. **Signer un message ne déplace aucun fonds** et n'expose aucune clé. Il n'y a pas de
   transaction, pas de frais, pas de risque de rejeu sur la chaîne.

> **Règle absolue, sans exception.** La signature est produite **par le portefeuille ou le
> fichier de clés qui détient déjà la clé**, sur la machine où il se trouve. Aucune clé
> privée, aucune phrase de récupération ne doit être saisie, copiée, transmise ou stockée
> ailleurs — ni dans un document, ni dans un terminal partagé, ni dans un message.

### Qui signe

**Signataire principal : `0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50`**, le validateur de
genèse. C'est le meilleur choix possible, et pour une raison précise : cette adresse est
**écrite dans l'`extraData` du bloc 0** (§ 1), donc dans la préimage de l'empreinte de genèse.
Le raisonnement que le vérificateur peut tenir seul, sans nous, est court et sans trou :

> *« Je lis le bloc 0 de la chaîne 26262 → j'y trouve l'adresse `0x3986…ba50` → je vérifie que
> cette adresse a signé ce message → donc celui qui a écrit ce message détient la clé inscrite
> dans le genesis de cette chaîne. »*

*Contrainte opérationnelle à connaître avant de décider :* cette clé est la clé de scellement
des blocs, elle vit sur la machine du validateur (`deploy/40-validator.sh`). La signature doit
être produite **sur cette machine ou hors ligne depuis son fichier de clés**, et le fichier de
clés ne doit pas en sortir.

**Ancre de repli, si l'éditeur préfère ne pas solliciter la clé du validateur :**
`0x1EEf3830833d83AcD3152A511853fd04a0b4082A`, le gouverneur — présent dans l'allocation du
genesis, donc engagé par la racine d'état, et détenu sur portefeuille matériel selon
`docs/GENESIS-PRODUCTION.md`. **Le compromis est réel et doit être assumé :** cette adresse
n'est pas lisible en une requête au bloc 0 ; le vérificateur doit passer par le genesis
reconstruit depuis le dépôt (§ 1). La preuve est aussi solide, elle demande une étape de plus.

**Preuve de contrôle des 13 adresses de trésorerie :** c'est une demande distincte, adressée
aux places d'échange, déjà spécifiée dans `DOSSIER-COTATION.md` § 15. Le même mécanisme et le
même format de message servent. Ne pas la confondre avec la déclaration d'identité : l'une
prouve *qui nous sommes*, l'autre prouve *que nous détenons les fonds*.

### Le message exact à signer

Le message est **auto-portant** : il nomme lui-même la chaîne, les domaines et les canaux.
C'est ce qui le rend inutilisable par un copieur — le republier, c'est republier une phrase
qui désigne `coinbosa.com`.

```
COINBOSA -- DECLARATION D'IDENTITE
version: 1
date: 2026-09-03
expire: 2027-09-03
editeur: coinbosa, Inc. -- Delaware, United States
chain-id: 26262
genesis: 0x8dcdadc247a98f33728cae944e20ce7c49c74b35cfba31495f85e98979018da6
state-root: 0x93682eb9182a55531d47014b76a285b45d3e720a2951f9ffbdc67f52995f8c03
signataire: 0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50
site: https://coinbosa.com
explorateur: https://explorer.coinbosa.com
rpc: https://explorer.coinbosa.com/rpc
depot: https://github.com/Coinbosa/coinbosa-chain
x: https://x.com/coinbosa6476
telegram: https://t.me/Coinbosaofficial
facebook: https://www.facebook.com/coinbosa
securite: https://github.com/Coinbosa/coinbosa-chain/security/advisories/new
nonce: <32 caracteres hexadecimaux tires au hasard>
Les points d'acces ci-dessus sont les seuls declares par l'editeur a cette date.
Cette declaration ne porte sur aucun autre reseau, aucun autre jeton, aucune autre organisation.
```

**Cinq règles de forme, non négociables — une seule enfreinte invalide la signature :**

| Règle | Pourquoi |
|---|---|
| **ASCII pur, aucun caractère accentué** | un accent transcrit d'un encodage à l'autre change les octets et casse la vérification. Supprimer la classe entière de pannes coûte moins cher que la diagnostiquer chez un tiers |
| **Fins de ligne `LF`, jamais `CRLF`** | même raison ; un aller-retour par un éditeur Windows suffit à casser |
| **Aucun saut de ligne final, aucun BOM** | même raison |
| **Aucun `<…>` ne subsiste** | `telegram:` et `nonce:` doivent être remplis avant signature. `openssl rand -hex 16` produit le nonce |
| **Le fichier publié est l'original** | on ne re-tape jamais un message signé ; on sert le fichier signé |

Publier aussi l'empreinte du fichier, pour que le vérificateur sache qu'il tient les bons
octets avant même de vérifier :

```bash
shasum -a 256 coinbosa-identity.txt
```

Le champ `expire` n'est pas décoratif : il **borne la validité**. Toute déclaration doit être
resignée à chaque changement de canal, et au plus tard à l'échéance. **On ne supprime jamais
une déclaration ancienne** — on en publie une nouvelle qui la remplace, en incrémentant
`version`. Une déclaration qui disparaît sans remplaçante est, en soi, un signal d'alerte.

### Où le publier — trois endroits, pour qu'une copie ne puisse pas se substituer

| Emplacement | Fichiers | Ce qu'il apporte |
|---|---|---|
| **1. Le domaine** | `https://coinbosa.com/.well-known/coinbosa-identity.txt` et `.sig` | emplacement normalisé, servi en HTTPS par le domaine que la déclaration nomme |
| **2. Le dépôt** | `identity/coinbosa-identity.txt` et `.sig`, à chemin stable | horodaté par l'historique Git, public, indépendant de notre hébergement |
| **3. Les canaux** | message épinglé sur `@coinbosa6476` et sur `t.me/Coinbosaofficial` | atteint ceux qui ne liront jamais un fichier `.well-known` |

**Détail d'implémentation à ne pas rater.** `deploy/publish-static.sh` **exclut `.well-known/`**
de la synchronisation générale (tableau `COMMUN`, option `--exclude '.well-known/'`) : déposer
le fichier dans `site/.well-known/` **ne suffit pas**, il ne partira jamais. Il faut ajouter
deux lignes explicites, sur le modèle exact de celles qui publient déjà `security.txt` :

```bash
rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/identity/coinbosa-identity.txt" \
  "$SERVER:/var/www/coinbosa/site/.well-known/coinbosa-identity.txt"
rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/identity/coinbosa-identity.txt.sig" \
  "$SERVER:/var/www/coinbosa/site/.well-known/coinbosa-identity.txt.sig"
```

### La liaison inverse : l'enregistrement DNS qui ferme la boucle

Sans elle, il reste une faille : n'importe qui peut **recopier** le message et la signature
sur son propre site. La signature restera valide — elle l'est intrinsèquement — et un lecteur
pressé pourrait croire qu'elle atteste le site où il la lit.

L'enregistrement `TXT` supprime cette faille, parce que **seul le détenteur du domaine peut
l'écrire** :

```
coinbosa.com.  TXT  "coinbosa-chain-id=26262 genesis=0x8dcdadc247a98f33728cae944e20ce7c49c74b35cfba31495f85e98979018da6 signer=0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50"
```

La liaison devient **bidirectionnelle**, et c'est ce qui la rend non copiable :

- la **clé** dit : *« coinbosa.com est à moi »* → c'est la signature ;
- le **domaine** dit : *« cette clé est la mienne »* → c'est le `TXT`.

Un copieur peut reproduire la première moitié. Il ne peut pas écrire dans notre zone DNS pour
produire la seconde, et il ne peut pas faire mentir la sienne : s'il publie ce `TXT` sur son
propre domaine, il y écrit `coinbosa.com`. `coinbosa.com` ne portant **aucun `TXT`
aujourd'hui** (§ 2 d), l'ajout n'entre en conflit avec rien. Vérification par un tiers :

```bash
dig +short TXT coinbosa.com
```

### Comment un tiers vérifie, sans outil spécial

Quatre voies, de la plus accessible à la plus stricte. **Toutes donnent le même résultat, et
aucune ne passe par nos serveurs.**

**Voie 1 — un portefeuille.** Tout portefeuille offrant « Vérifier un message » /
*Verify message* : on colle le message, la signature et l'adresse. Le portefeuille répond.
C'est la même primitive EIP-191, indépendante de la chaîne.

**Voie 2 — une commande, avec Foundry.**

```bash
cast wallet verify --address 0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50 \
  --message "$(cat coinbosa-identity.txt)" \
  "$(cat coinbosa-identity.txt.sig)"
```

**Voie 3 — quatre lignes, avec Node et `ethers`.** *(Chemin testé le 2026-09-03 : signature
puis vérification, adresse récupérée identique ; une altération d'un seul octet du message
rend une adresse différente.)*

```js
const { ethers } = require("ethers");
const msg = require("fs").readFileSync("coinbosa-identity.txt", "utf8");
const sig = require("fs").readFileSync("coinbosa-identity.txt.sig", "utf8").trim();
console.log(ethers.verifyMessage(msg, sig));   // doit rendre 0x3986D6b3...CBba50
```

**Voie 4 — le contrôle indépendant, celui qui compte.** Vérifier la signature ne suffit pas :
il faut que l'adresse signataire soit **celle de la chaîne**. C'est la requête du § 1, et elle
ne demande que `curl` et `python3`. Le tiers vérifie que l'adresse rendue par les octets 33 à
52 de l'`extraData` du bloc 0 est bien celle qui a signé.

**Le protocole complet tient en quatre gestes**, et un tiers peut les exécuter en cinq
minutes, sans compte, sans clé d'API, sans nous demander quoi que ce soit :

| # | Geste | Outil | Ce qu'il établit |
|---|---|---|---|
| 1 | lire le bloc 0 du RPC | `curl` | l'empreinte, la racine d'état, l'adresse du validateur |
| 2 | vérifier la signature du message | portefeuille, `cast` ou 4 lignes de JS | le signataire détient cette clé |
| 3 | comparer 1 et 2 | l'œil | le signataire est inscrit dans le genesis de la chaîne 26262 |
| 4 | lire le `TXT` de `coinbosa.com` | `dig` | le domaine revendique cette même clé |

**Ce qu'un homonyme peut faire, et ce qu'il ne peut pas.** Il peut recopier le texte : il
publiera alors une phrase qui désigne `coinbosa.com`, notre chainId et notre empreinte de
genèse — la copie plaide contre lui. Il peut recopier la signature : elle restera valide pour
`0x3986…ba50`, adresse qu'il ne contrôle pas, inscrite dans un genesis qu'il n'a pas produit,
et le `TXT` de son domaine ne pourra pas la revendiquer. **Il ne peut pas produire une
signature nouvelle** : il faudrait la clé. C'est tout l'intérêt de la pièce — elle n'est pas
déclarative, elle est calculatoire.

---

## 4 — Ce qu'il faut poser ailleurs

Chaque ligne dit **qui** le fait et **ce qui la débloque**. Rien ici ne demande de transaction,
de dépense, ni de décision de trésorerie.

| # | Action | Qui | Ce qui la débloque |
|---|---|---|---|
| **I1** | ~~Trancher les trois Telegram~~ — **FAIT le 2026-09-04** : l'éditeur a confirmé que `t.me/Coinbosaofficial` est son groupe ; les deux autres adresses sont retirées. **Reste :** corriger la description publique du groupe (« Proof of authority », `coinbosa.org`, « Coinbosa Foundation ») | éditeur | rien — c'est une modification dans Telegram |
| **I2** | ~~Déclarer le compte X~~ — **FAIT le 2026-09-04** : c'est `@coinbosa6476` qui est déclaré dans `coinbosa.config.json`, `site/app.js` et `explorer/app.js`. `@coinbosacrypto`, d'abord retenu d'après le champ GitHub, s'est révélé HORS DU CONTRÔLE de l'éditeur | fait | — |
| **I3** | ~~Une seule liste de liens~~ — **FAIT le 2026-09-04** : configuration, site et explorateur portent les mêmes sept liens, vérifié champ par champ | fait | — |
| **I4** | **Produire et publier la déclaration signée** + le `.sig` + l'empreinte SHA-256 ; ajouter les deux lignes `rsync` à `deploy/publish-static.sh` | éditeur (signature) + exploitation (publication) | I1 ; l'accès au fichier de clés du validateur, ou le choix de l'ancre de repli (§ 3) |
| **I5** | **Poser l'enregistrement `TXT`** de liaison inverse sur `coinbosa.com` | éditeur | accès à la zone DNS chez ResellerClub. Aucun conflit : le domaine ne porte aucun `TXT` |
| **I6** | **Vérifier le domaine chez GitHub** (organisation → domaines vérifiés, par `TXT`) : l'organisation est aujourd'hui `is_verified: false` | éditeur | même accès DNS que I5. Gratuit, immédiat, visible de tous sur la page de l'organisation |
| **I7** | **Vérification de domaine chez les agrégateurs** — procédure publique CoinGecko, formulaire GeckoTerminal | dossier | l'existence de `@coinbosacrypto` **change la donne enregistrée** : `SURVEILLANCE-LANCEMENT.md` § 6 conclut que « c'est la page Facebook qui portera cette preuve d'identité » faute de compte X, et `PLAN-AGREGATEURS.md` A5 tient le guichet GeckoTerminal pour bloqué. **Les deux constats sont à revoir** une fois I2 fait |
| **I8** | **Page « Identité officielle »** sur `coinbosa.com` | porte-parole écrit, éditeur valide | son contenu est **déjà spécifié** par `SURVEILLANCE-LANCEMENT.md` § 6 (*« Ce qu'on publie, une fois, à un seul endroit »*). Trois ajouts, et pas davantage : le lien vers la déclaration signée et son mode d'emploi ; le compte X ; la correction des lignes Telegram |
| **I9** | **Rendre joignable ce qu'on publie** : `MX` pour `coinbosa.com`, ou remplacer `security@coinbosa.com` et `info@coinbosa.com` par l'URL d'avis de sécurité GitHub dans `deploy/static/security.txt` et le profil de l'organisation | éditeur | accès DNS ou décision éditoriale. Un canal de signalement injoignable est pire qu'absent : il absorbe les alertes en silence |
| **I10** | **Poser un enregistrement `CAA`** sur `coinbosa.com`, restreignant l'émission de certificats à l'autorité utilisée | exploitation | accès DNS. Mesure d'identité, pas de confort : elle réduit le nombre d'acteurs capables de produire un certificat valide pour notre nom |
| **I11** | **Réépingler l'icône du registre des chaînes** (référencée par IPFS dans `chainlist/coinbosa.json`, non servie par les passerelles testées) | exploitation | rien. Motif détaillé dans `SURVEILLANCE-LANCEMENT.md` § 6 : qui veut notre logo et ne l'obtient pas de la source officielle le prendra ailleurs |
| **I12** | **Régler les artefacts hérités** de l'organisation GitHub : le fichier `info` de `smart-chain-token` déclare un actif BEP2 « active » ; `Coinbosa-Token-solana` contient du NEO/NEP-5 | éditeur | une décision : archiver, corriger, ou documenter. Lié à `DOSSIER-COTATION.md` § 8, à ne pas trancher séparément |
| **I13** | **Corriger `SURVEILLANCE-LANCEMENT.md` lignes 666-668** — voir § 5 | rédaction | I2 |

**Ordre imposé par les dépendances :** I1 → I2 → I3 → I4 → I5. Les autres sont
indépendantes. **I1 est le verrou :** tant que le champ `telegram` de la déclaration ne peut
pas être rempli avec une adresse dont l'éditeur affirme la détention, la pièce maîtresse du
§ 3 ne peut pas être produite.

---

## 5 — La surveillance des imitations

**Le dispositif de détection et de réponse est dans `SURVEILLANCE-LANCEMENT.md`** : les
familles de menace (§ 1, M8 en particulier), les sondes et leurs lignes de base (§ 6, *Comment
on détecte*), les gestes des cinq premières minutes (§ 4, M8), la table d'acheminement (§ 3),
et le contenu exact de la page publique (§ 6). **Ce paragraphe n'en recopie rien.** Il ajoute
les trois choses que l'homonymie change, et qui ne sont pas dans ce document-là.

### a) Une correction à faire avant toute publication

`SURVEILLANCE-LANCEMENT.md`, lignes 666-668, publie :

> *« **Aucun compte X / Twitter**, **aucun serveur Discord** : les champs correspondants sont
> **vides** dans `site/app.js`. Tout compte X ou Discord se présentant comme Coinbosa est une
> imitation, aujourd'hui, sans exception. »*

La moitié « Discord » reste exacte. **La moitié « X » était fausse, et l'est autrement qu'on ne croyait** : le compte officiel est `@coinbosa6476`, et `@coinbosacrypto` existe
et est déclaré par l'organisation GitHub du projet (§ 2 a). Publier cette page telle quelle
ferait désavouer par le projet son propre compte — et offrirait à quiconque le passage
inverse : *« leur propre page dit que ce compte n'est pas à eux »*. **I13 corrige, I2 la
rend correcte.**

### b) La règle qui change, quand le nom est partagé

Quand un nom est partagé, **la recherche par nom ne discrimine plus rien**. Toute la
surveillance doit basculer sur les ancres du § 1, et la règle de publication devient :

> **On ne qualifie personne. On publie un test.**
>
> *« La chaîne de coinbosa, Inc. répond `0x6696` à `eth_chainId` et sert l'empreinte de bloc 0
> `0x8dcd…8da6`. Toute chaîne qui répond autre chose n'est pas celle-ci. »*

Cette phrase est **vérifiable en une requête, par n'importe qui, sans nous croire**. Elle ne
nomme aucun tiers, ne le caractérise pas, ne l'accuse de rien — ce qui la met hors du terrain
juridique tout en étant plus efficace qu'une dénonciation : elle donne au lecteur de quoi
trancher lui-même, au lieu de lui demander de choisir entre deux affirmations.

### c) Ce qu'on cherche en plus, propre à l'homonymie

Ces cinq signaux s'**ajoutent** aux sondes de `SURVEILLANCE-LANCEMENT.md` § 6. Chacun porte sa
ligne de base mesurée le 2026-09-03, sans quoi une variation n'est pas interprétable.

| Signal | Où l'on regarde | Ligne de base mesurée le 2026-09-03 |
|---|---|---|
| **`coinbosa.org` reprend vie** | `dig +short A coinbosa.org` | **aucune réponse** — le domaine ne résout pas. Il est nommé comme site par le groupe que notre propre site déclare officiel (§ 2 b) : sa réapparition serait immédiatement crédible auprès de nos propres visiteurs |
| **`t.me/coinbosagroup` est pris par un tiers** | `curl -s https://t.me/coinbosagroup \| grep og:title` | **aucun titre, aucune description**. Ce nom était publié dans `coinbosa.config.json` jusqu'au 2026-09-04 : s'il devenait un groupe actif que nous ne détenons pas, notre propre fichier l'aurait légitimé. Retiré depuis ; la surveillance reste utile au cas où quelqu'un le prendrait en se réclamant de nous |
| **Un jeton nommé BOSA apparaît chez un agrégateur** | `api.coingecko.com/api/v3/search?query=BOSA` et `query=coinbosa` | **listes vides** pour les deux requêtes |
| **Une seconde chaîne réclame le nom** | entrée `chainid.network/chains.json` : `chainId`, `shortName`, `chain`, `nativeCurrency.symbol` | **26262 seule** sur les quatre champs, parmi 2 745 chaînes. `262620` libre. Seul autre « bosa » : `BOSagora Mainnet` (2151), sans recouvrement de ticker |
| **`@coinbosacrypto`, hors de notre contrôle** | surveiller ce compte lui-même | l'éditeur en a perdu les accès. Ce qu'il publiera engagera notre nom sans nous. À surveiller en priorité, et à désavouer publiquement dès que le compte officiel est déclaré partout |
| **Comptes X voisins de `@coinbosa6476`** | recherche sur X | à établir : tant que le compte n'est pas déclaré publiquement, on ne peut pas distinguer un voisin d'un officiel |

**Ce qu'on publie quand on trouve.** Le mode opératoire est celui de
`SURVEILLANCE-LANCEMENT.md` § 6 — un seul endroit, une seule fois, la page « Identité
officielle ». Une seule règle s'y ajoute, et elle découle de (b) : **on met à jour la liste
positive** (« voici ce qui est à nous, voici le test »), **on ne publie pas de liste
d'accusés**.

---

## 6 — Ce que ce document ne résout pas

**Le volet juridique.** Voir l'avertissement d'ouverture.

**Une signature prouve une clé, pas une personne morale.** Le § 3 établit que le signataire
détient la clé inscrite dans le genesis de la chaîne 26262, et que le domaine `coinbosa.com`
revendique cette clé. Il n'établit **pas** que « coinbosa, Inc. » existe, ni qu'elle est
immatriculée où le site l'écrit, ni qu'elle détient le domaine en droit. Ces liens-là passent
par des registres, pas par de la cryptographie. **Ne jamais présenter la déclaration signée
comme une preuve d'existence légale.**

**La déclaration ne rend pas la garde plus sûre — et elle en dépend.** Le fait le plus lourd
du dossier reste intact : **un seul secret dérive les 13 adresses de trésorerie et le
gouverneur** (`DOSSIER-COTATION.md` § 7 et § 15). Si l'ancre de repli du § 3 est retenue, la
clé qui signe l'identité est dérivée du secret qui ouvre 700 000 000 BOSA **et** la
gouvernance du consensus. Signer n'y ajoute aucun risque — la signature n'expose pas la clé —
mais **cela ne répare rien**. La garde à seuil reste entièrement à faire.

**L'identité solide ne rend pas la chaîne sûre.** Le § 1 le montre au passage :
`finalized` répond bloc 0 quand la tête est au-delà de 455 000, la clé BLS du genesis est
nulle, **un seul validateur produit 100 % des blocs**. Un tiers qui vérifie notre identité
vérifiera ensuite notre disponibilité, et trouvera cela. **Ne jamais vendre l'un pour
l'autre :** un projet parfaitement identifié peut être parfaitement fragile.

**Rien ici ne crée un marché ni n'obtient une cotation.** Les guichets exigent un marché
actif ; ce document ne fournit qu'une pièce d'identité. Voir `PLAN-AGREGATEURS.md`.

**Les deux questions qui bloquaient la suite sont tranchées, le 2026-09-04 :**
`t.me/Coinbosaofficial` est bien le groupe de l'éditeur, et `@coinbosacrypto` **ne l'est plus** —
il en a perdu les accès. Le compte officiel est `@coinbosa6476`.

**Ce qui reste, et qui n'appartient qu'à l'éditeur :** corriger `twitter_username` sur
l'organisation GitHub, et corriger la description publique du groupe Telegram, qui annonce
encore « Proof of authority », renvoie à `coinbosa.org` — un domaine qui ne résout pas — et se
dit exploitée par une « Coinbosa Foundation ».

**L'héritage n'est pas couvert ici.** Le jeton SPL Solana et l'écart mesuré sur son retrait de
circulation restent traités par `DOSSIER-COTATION.md` § 8. Les artefacts GitHub hérités (I12)
en dépendent et ne doivent pas être tranchés séparément.

**Aucune surveillance n'est automatisée par ce document.** Les cinq signaux du § 5 sont des
lignes de base et des commandes, **pas des sondes déployées**. Tant que personne ne les
exécute à intervalle régulier, la ligne de base ne sert à rien : une valeur de référence sans
mesure suivante ne détecte aucun changement.

---

### Sources et reproductibilité

| Affirmation | Commande ou source |
|---|---|
| Empreinte, racine d'état, `extraData` du bloc 0 | `eth_getBlockByNumber("0x0")` sur `https://explorer.coinbosa.com/rpc` |
| chainId, hauteur de la chaîne | `eth_chainId` → `0x6696` ; `net_version` → `26262` ; `eth_blockNumber` → `0x6f2c7` |
| `finalized` au bloc 0 | `eth_getBlockByNumber("finalized")` → `number: 0x0` |
| Genesis reproductible, SHA-256 `4d93164f…` | `scripts/build-genesis.js` ; `DOSSIER-COTATION.md` § 1 |
| Entrée 26262, unicité, 2 745 chaînes, 262620 libre | `https://chainid.network/chains.json` |
| Recherches CoinGecko vides | `https://api.coingecko.com/api/v3/search?query=BOSA` et `?query=coinbosa` |
| `@coinbosacrypto` existe mais ÉCHAPPE au projet ; officiel = `@coinbosa6476` | `https://api.github.com/orgs/Coinbosa` champ `twitter_username` ; `<title>` de `https://x.com/coinbosacrypto` |
| Organisation créée 2022-05-11, non vérifiée, 4 dépôts | `https://api.github.com/orgs/Coinbosa` |
| Fichier `info` BEP2 | `https://raw.githubusercontent.com/Coinbosa/smart-chain-token/master/info` |
| Descriptions Telegram des trois adresses | `og:title` et `og:description` de `https://t.me/<nom>` |
| `coinbosa.org` ne résout pas | `dig +short A coinbosa.org @1.1.1.1` — aucune réponse |
| Absence de `TXT`, `MX`, `_dmarc`, `CAA` sur `coinbosa.com` | `dig +short <type> coinbosa.com @1.1.1.1` — aucune réponse pour les quatre |
| Codes HTTP du site, de l'explorateur, du livre blanc, de `security.txt` | `curl -o /dev/null -w "%{http_code}"` |
| Certificat, en-têtes de sécurité | `openssl s_client -connect coinbosa.com:443` ; `curl -sI https://coinbosa.com/` |
| Chaîne signature → vérification EIP-191 | testée le 2026-09-03 avec `ethers` : adresse récupérée identique ; message altéré d'un octet → adresse différente |
| `.well-known/` exclu de la publication | `deploy/publish-static.sh`, tableau `COMMUN`, `--exclude '.well-known/'` |

*Document au 2026-09-03. Toute valeur mesurée porte sa date ; celles du § 2 et du § 5 sont des
lignes de base destinées à être re-mesurées.*
