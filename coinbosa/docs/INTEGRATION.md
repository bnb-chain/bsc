# Intégrer Coinbosa Chain

Guide destiné aux développeurs qui veulent connecter une application, déployer un contrat ou
accepter des paiements en BOSA.

Coinbosa est une chaîne EVM : **tout ce que vous savez faire sur Ethereum ou BNB Chain
fonctionne ici sans adaptation.** Les bibliothèques, les wallets et les outils de déploiement
sont les mêmes. Ce guide se limite donc à ce qui est spécifique à Coinbosa.

---

## Paramètres du réseau

| | |
|---|---|
| Nom | Coinbosa Chain |
| Chain ID | `26262` |
| Symbole du coin natif | BOSA |
| Décimales du coin natif | 18 |
| Temps de bloc | 5 secondes |

> Les points d'accès RPC publics ne sont pas encore ouverts. En attendant, lancez un nœud
> local — voir [le README](../README.md). Les valeurs ci-dessous supposent un nœud local.

| | |
|---|---|
| RPC HTTP | `http://127.0.0.1:8545` |
| RPC WebSocket | `ws://127.0.0.1:8546` |
| Explorateur | `http://127.0.0.1:8080` |

---

## Ajouter le réseau à un wallet

### Depuis une application web

```js
await window.ethereum.request({
  method: 'wallet_addEthereumChain',
  params: [{
    chainId: '0x6696',                    // 26262
    chainName: 'Coinbosa Chain',
    nativeCurrency: { name: 'Bosa', symbol: 'BOSA', decimals: 18 },
    rpcUrls: ['http://127.0.0.1:8545'],
    blockExplorerUrls: ['http://127.0.0.1:8080'],
  }],
});
```

Préférez toujours cet appel à une notice expliquant comment saisir les valeurs à la main : un
chain ID mal recopié envoie les fonds sur un réseau qui n'est pas le vôtre.

---

## Se connecter

```js
import { ethers } from 'ethers';

const provider = new ethers.JsonRpcProvider('http://127.0.0.1:8545');
console.log(await provider.getBlockNumber());
```

Toutes les méthodes JSON-RPC standard d'Ethereum sont disponibles, plus l'espace de noms
`parlia` propre au consensus.

---

## Les deux actifs : à ne pas confondre

C'est la source d'erreur la plus fréquente sur Coinbosa. **Deux actifs distincts portent le
symbole BOSA.**

| | Coin natif | Jeton BRC20 |
|---|---|---|
| Rôle | paie le gas | actif applicatif |
| Décimales | **18** | **10** |
| Lecture du solde | `provider.getBalance()` | `token.balanceOf()` |
| Envoi | `wallet.sendTransaction()` | `token.transfer()` |
| Offre | fixée au genesis | 700 000 000 |

Les 18 décimales du coin natif sont imposées par l'EVM : l'unité de base est le wei, et cette
valeur est câblée dans le calcul du gas comme dans les wallets. Elle n'est pas modifiable.

**Conséquence pratique** — n'utilisez jamais `ethers.parseEther()` pour un montant en jeton
BRC20. Vous obtiendriez 10⁸ fois la valeur voulue.

```js
// coin natif — 18 décimales
const gas = ethers.parseEther('1.5');

// jeton BRC20 — 10 décimales, toujours lues depuis le contrat
const decimals = await token.decimals();
const montant = ethers.parseUnits('1.5', decimals);
```

Lisez `decimals()` depuis le contrat plutôt que de coder `10` en dur : votre code restera juste
face à un autre jeton BRC20.

---

## Le standard BRC20

BRC20 — *Bosa smart contRact 20* — est le standard de jeton de Coinbosa. Il est **compatible
ERC-20** : toute bibliothèque, tout wallet et tout service qui parle ERC-20 fonctionne sans
adaptation.

> Un standard homonyme existe sur Bitcoin (inscriptions Ordinals), sans aucun rapport technique
> avec celui-ci. Précisez « BRC20 de Coinbosa » dans vos intégrations pour lever l'ambiguïté.

### Interface

Toutes les fonctions ERC-20 : `name`, `symbol`, `decimals`, `totalSupply`, `balanceOf`,
`transfer`, `approve`, `allowance`, `transferFrom`, plus les événements `Transfer` et `Approval`.

S'y ajoutent :

| Fonction | Objet |
|---|---|
| `getOwner()` | propriétaire du contrat, exposé de façon standardisée |
| `mint(address,uint256)` | émission, réservée au propriétaire |
| `burn(uint256)` | destruction, ouverte au porteur |
| `increaseAllowance(address,uint256)` | évite la condition de course de `approve` |
| `decreaseAllowance(address,uint256)` | idem |

Une autorisation fixée à `type(uint256).max` n'est jamais décrémentée, ce qui économise du gas
sur les usages répétés.

### Créer son propre jeton

Héritez de `BRC20` et laissez le constructeur faire le reste.

```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./BRC20.sol";

contract MonJeton is BRC20 {
    constructor(address proprietaire)
        BRC20("Mon Jeton", "MJT", 18, 1_000_000 * 10 ** 18, proprietaire)
    {}
}
```

---

## Accepter des paiements en BOSA

Surveillez les événements `Transfer` vers votre adresse plutôt que d'interroger les soldes en
boucle.

```js
const abi = ['event Transfer(address indexed from, address indexed to, uint256 value)'];
const token = new ethers.Contract(ADRESSE_JETON, abi, provider);

token.on(token.filters.Transfer(null, MON_ADRESSE), (from, to, value, event) => {
  console.log(`reçu ${ethers.formatUnits(value, 10)} BOSA de ${from}`);
  console.log(`bloc ${event.log.blockNumber}, tx ${event.log.transactionHash}`);
});
```

### Combien de confirmations attendre

Coinbosa fonctionne aujourd'hui avec **un seul validateur** et **sans finalité rapide** : les
clés BLS sont à zéro, le vote d'attestation est inactif. Un bloc n'est donc pas définitif au
sens où il l'est sur un réseau à finalité prouvée.

Pour des montants significatifs, attendez plusieurs blocs et adaptez ce seuil au risque que
vous acceptez. Ce guide sera mis à jour quand la finalité rapide sera active.

---

## Déployer un contrat

Rien de spécifique, à une exception près.

**Compilez en visant `shanghai`.** Solidity cible Cancun par défaut depuis la version 0.8.25 et
produit alors l'opcode `MCOPY`, que Coinbosa rejette avec `invalid opcode`. Le contrat se
déploie sans erreur, puis échoue au premier appel — un symptôme déroutant si on n'en connaît pas
la cause.

<details>
<summary>Foundry</summary>

```toml
# foundry.toml
[profile.default]
evm_version = "shanghai"
```
</details>

<details>
<summary>Hardhat</summary>

```js
// hardhat.config.js
module.exports = {
  solidity: { version: '0.8.26', settings: { evmVersion: 'shanghai' } },
  networks: {
    coinbosa: { url: 'http://127.0.0.1:8545', chainId: 26262 },
  },
};
```
</details>

<details>
<summary>solc en ligne de commande</summary>

```bash
solc --evm-version shanghai --optimize --bin --abi MonContrat.sol
```
</details>

---

## Limites actuelles

À connaître avant de bâtir dessus :

- **Pas de RPC public.** Il faut faire tourner son propre nœud.
- **Un seul validateur.** Aucune tolérance aux pannes.
- **Pas de finalité rapide.** Voir la section sur les confirmations.
- **Explorateur sans indexation.** Ni historique par adresse, ni vérification de code source.
- **Aucun audit externe.** Ne placez pas de valeur réelle sur ce réseau à ce stade.
- **Pas de pont.** BOSA ne circule pas hors de Coinbosa Chain.

L'avancement de ces points est suivi dans [ROADMAP.md](../ROADMAP.md).

---

## Obtenir de l'aide

Ouvrez une issue sur [le dépôt](https://github.com/Coinbosa/coinbosa-chain/issues).

Pour une faille de sécurité, n'ouvrez pas d'issue publique : suivez
[la politique de sécurité](../../.github/SECURITY.md).
