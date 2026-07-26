<div align="center">
  <img src="coinbosa/assets/coinbosa-logo.jpg" alt="Coinbosa" width="140" />

  # Coinbosa Chain

  Blockchain EVM souveraine — coin natif **BOSA**, standard de jeton **BRC20**.
</div>

---

Coinbosa Chain est un réseau compatible Ethereum, sécurisé par le consensus **Parlia**
(preuve d'enjeu). Son coin natif, **BOSA**, a une offre **fixe de 700 000 000** unités,
inscrite au bloc de genèse — aucune émission n'est possible.

**Éditeur** — coinbosa, Inc., Delaware, United States.
**En ligne** — [coinbosa.com](https://coinbosa.com) · [explorateur](https://explorer.coinbosa.com) · [livre blanc](https://coinbosa.com/whitepaper/)

## Où est le code Coinbosa

Tout le travail spécifique à Coinbosa vit dans le dossier **[`coinbosa/`](coinbosa/)** :

| Dossier | Contenu |
|---|---|
| [`coinbosa/contracts/`](coinbosa/contracts) | contrat système du consensus (`CoinbosaValidatorSet`) + standard de jeton `BRC20` |
| [`coinbosa/scripts/`](coinbosa/scripts) | construction du genesis, contrôles d'offre / epoch / temps de bloc, tests |
| [`coinbosa/genesis/`](coinbosa/genesis) | modèle de genesis et adresses de distribution |
| [`coinbosa/site`](coinbosa/site) · [`explorer`](coinbosa/explorer) · [`whitepaper`](coinbosa/whitepaper) | site public, explorateur et livre blanc |
| [`coinbosa/deploy/`](coinbosa/deploy) | déploiement du tier public (Caddy + TLS automatique) |
| [`coinbosa/docs/`](coinbosa/docs) | audit de sécurité, durcissement, décisions |

👉 Commence par **[`coinbosa/README.md`](coinbosa/README.md)** et le **[livre blanc](coinbosa/WHITEPAPER.md)**.

## À propos du client (et des références « BNB » dans ce dépôt)

Le client dérive de **[BNB Smart Chain](https://github.com/bnb-chain/bsc)** (lui-même dérivé
de go-ethereum). **C'est pourquoi le reste de ce dépôt est le code du client amont, qui
conserve ses références à BNB/BSC — c'est normal pour un fork.** La seule modification du
client propre à Coinbosa est l'intervalle de bloc porté à **5 secondes**, dans
[`consensus/parlia/parlia.go`](consensus/parlia/parlia.go). Le contrat système du consensus,
lui, est **réécrit sur mesure** dans `coinbosa/contracts/`.

## Caractéristiques

| | |
|---|---|
| Chain ID | `26262` |
| Consensus | Parlia — preuve d'enjeu |
| Temps de bloc | 5 secondes |
| Coin natif | BOSA (18 décimales) |
| Offre | 700 000 000, fixée au genesis |
| Standard de jeton | BRC20 (compatible ERC-20) |
| Compatibilité EVM | Shanghai |

## Licence

Le client hérite de la licence de `bnb-chain/bsc` (LGPL-3.0 / GPL-3.0). Les contrats et
outils du dossier `coinbosa/` sont sous licence MIT. Le logo Coinbosa est la propriété de
coinbosa, Inc.
