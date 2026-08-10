# Faire reconnaître Coinbosa Chain par les portefeuilles

## Pourquoi il ne faut pas construire un portefeuille

Tous les portefeuilles compatibles Ethereum sont **déjà multi-chaînes** : MetaMask, Trust
Wallet, Rabby, Coinbase Wallet, Ledger Live. Ils savent tous parler à Coinbosa — c'est une
chaîne EVM standard, avec des adresses `0x` et le même format de transaction.

Ce qui manque n'est pas un logiciel, c'est une **inscription au registre**. Aujourd'hui, un
utilisateur doit ajouter le réseau à la main, et il verra « 26262 » sans nom ni logo. Une fois
la chaîne enregistrée, elle apparaît partout avec son nom, son symbole, son explorateur et
**son logo** — sans qu'aucun portefeuille n'ait à être modifié.

Écrire un portefeuille maison coûterait des mois, obligerait à gérer des clés privées
d'utilisateurs, et n'apporterait rien de plus que ce que fait l'inscription. C'est le mauvais
chantier.

## Ce que contient ce dossier

| Fichier | Destination dans `ethereum-lists/chains` |
|---|---|
| `eip155-26262.json` | `_data/chains/eip155-26262.json` |
| `coinbosa.json` | `_data/icons/coinbosa.json` |
| `coinbosa-96.png` | à téléverser sur IPFS (ce fichier n'est pas commité chez eux) |

Le chainId **26262 est libre** — vérifié sur les 2 681 chaînes du registre.

## Marche à suivre

### 1. Téléverser le logo sur IPFS

Le registre exige une image de 96×96 accessible par IPFS. `coinbosa-96.png` est déjà à la
bonne taille, généré depuis le logo officiel (jamais redessiné).

Passer par un service qui épingle durablement — sinon le logo disparaîtra le jour où le
fichier ne sera plus répliqué :

- [pinata.cloud](https://pinata.cloud) (offre gratuite suffisante)
- ou [web3.storage](https://web3.storage)

Récupérer le CID obtenu, de la forme `bafybei…`.

### 2. Renseigner le CID

Ouvrir `coinbosa.json` et remplacer `REMPLACER_PAR_LE_CID` par le CID.

### 3. Proposer la modification

```bash
git clone https://github.com/ethereum-lists/chains
cd chains
cp <ce-dossier>/eip155-26262.json _data/chains/
cp <ce-dossier>/coinbosa.json      _data/icons/
git checkout -b add-coinbosa-26262
git add _data/chains/eip155-26262.json _data/icons/coinbosa.json
git commit -m "Add Coinbosa Chain (26262)"
git push origin add-coinbosa-26262
```

Puis ouvrir la pull request sur `ethereum-lists/chains`.

### Ce que le registre vérifie

Leur intégration continue rejette une chaîne dont le RPC ne répond pas, ou dont le
`eth_chainId` ne correspond pas au fichier. Vérifié de notre côté :

```
eth_chainId        -> 0x6696   (26262)
net_version        -> 26262
web3_clientVersion -> Geth/v1.7.6-…
```

Le point d'accès `https://explorer.coinbosa.com/rpc` est public, en HTTPS, et répond.

## Ce que ça change concrètement

Une fois la pull request fusionnée, et sans que personne n'ait à installer quoi que ce soit :

- **chainlist.org** propose « Coinbosa Chain » — ajout en un clic dans MetaMask ;
- les portefeuilles qui consomment le registre affichent **le nom, le symbole BOSA et le
  logo** au lieu d'un numéro nu ;
- un utilisateur qui reçoit des BOSA les voit **avec le logo Coinbosa**, pas comme un jeton
  inconnu ;
- l'explorateur est lié automatiquement depuis les transactions.

## Les jetons BRC20, c'est un registre différent

Ce dossier concerne le **coin natif** BOSA. Pour qu'un jeton BRC20 (celui de Bite Fast, par
exemple) s'affiche avec SON logo, il faut une liste de jetons distincte, au format
[Uniswap Token List](https://tokenlists.org) — à héberger sur `coinbosa.com`. À faire quand
un premier jeton sera réellement déployé, pas avant : une liste qui référence un jeton
inexistant est pire que pas de liste.

## Avant de proposer la chaîne — à savoir

Le registre est public et durable. Deux limites actuelles seront visibles de tous :

- **un seul point d'accès RPC**, sur une seule machine. Les chaînes sérieuses en publient
  plusieurs. Une panne rendra la chaîne injoignable pour tous les portefeuilles qui utilisent
  cette entrée.
- **un seul validateur** : si la machine tombe, le réseau s'arrête.

Rien n'empêche l'inscription, mais mieux vaut avoir un second point d'accès avant d'attirer
du monde dessus.
