# Règles de travail sur ce dépôt

Plusieurs assistants travaillent sur ce dépôt en parallèle. Ce fichier existe pour qu'une
modification faite d'un côté n'annule pas silencieusement une garantie posée de l'autre.

**La production appartient à l'éditeur.** Rien ne part en ligne sans son accord explicite.

---

## Ce qui est irréversible — à ne jamais toucher sans décision explicite

Une chaîne publique ne se corrige pas après coup. Les points suivants, une fois figés,
n'ont pas de « deuxième essai » :

| Invariant | Pourquoi | Où c'est gardé |
|---|---|---|
| **Offre de 700 000 000 BOSA, fixée au bloc 0** | Le consensus ne crée pas de monnaie. Une erreur de répartition est définitive. | `build-genesis.js` (contrôle au wei près) |
| **solc épinglé à `0.8.26`** | Le bytecode du contrat système fixe le **hash du bloc 0**, donc l'identité de la chaîne. Changer de compilateur change le réseau. | garde *fail-closed* dans `compile.js` et `build-genesis.js` |
| **Aucune fonction du chemin de consensus ne peut `revert`** | Un `revert` rend le bloc improduisible : la chaîne s'arrête. | règle de conception de `CoinbosaValidatorSet.sol` |
| **Gouverneur ≠ clé de scellage** | La clé de scellage vit en ligne en permanence ; lui confier la gouvernance ferait qu'un serveur compromis emporte le consensus. | refus en production dans `build-genesis.js` |
| **Aucune clé privée dans le dépôt** | `.gitignore` couvre `node*/`, `keystore/`, `UTC--*`, `*.key`, `pw.txt`, `.env*`. | à ne pas contourner |
| **Clé du gouverneur irremplaçable** | `GOVERNOR` est `constant` et aucune fonction ne le change. Sa perte fige l'ensemble des validateurs à vie ; sa compromission donne le consensus. | impossible à corriger — voir `docs/GENESIS-PRODUCTION.md` |
| **Empreinte du bloc 0 figée** | `stateRoot` engage tout l'état initial : c'est ce qui rend vérifiable l'absence d'allocation cachée. | `genesis-reference.json` + `check-genesis-hash.js` |

## Contraintes de projet

- **Aucun conteneur.** Docker a été entièrement retiré du dépôt ; le déploiement est natif
  (`coinbosa/deploy/`). Ne pas réintroduire de `Dockerfile`, `docker-compose`, ni de
  workflow qui publie une image.
- **Aucune signature d'IA** dans les commits : pas de `Co-Authored-By`, pas de mention
  d'assistant dans les messages.
- **Ne jamais afficher de chiffre qui ne vienne pas de la chaîne.** L'explorateur a
  contenu des blocs, transactions et jetons fabriqués en dur : tout a été retiré. Quand
  aucun nœud ne répond, il affiche un avis, pas des données inventées.
- **Le logo ne change pas.** `coinbosa/assets/coinbosa-logo.jpg` est la source des
  favicons ; ne pas le remplacer ni le redessiner.

## Barrières automatiques (elles échoueront, ce n'est pas un bug)

Ces contrôles bloquent la CI. Si l'un d'eux passe au rouge, c'est qu'il a trouvé quelque
chose — le corriger, pas le contourner :

| Contrôle | Ce qu'il empêche |
|---|---|
| `coinbosa/scripts/audit-deps.js` | une faille npm non dérogée, ou une dérogation expirée |
| `coinbosa/scripts/audit-go.js` | une faille Go **atteignable** non dérogée (govulncheck) |
| `coinbosa/scripts/check-supply.js` | une offre on-chain différente du genesis ; un genesis de dév en production |
| `coinbosa/scripts/check-genesis-hash.js` | une allocation cachée ; une chaîne substituée |
| `coinbosa/scripts/preflight-genesis.js` | créer les coins avec une condition non remplie |

Une dérogation d'audit exige **une preuve de non-atteignabilité et une date d'expiration**
(`audit-allowlist.json`, `go-vuln-allowlist.json`). Sans expiration, une dérogation
« provisoire » devient un trou permanent que plus personne ne regarde.

## Honnêteté des affirmations

Le dépôt a déjà porté la phrase « intégration continue verte de bout en bout » alors que la
CI ne pouvait pas passer. La règle qui en découle :

> On n'écrit pas qu'une chose est vérifiée avant d'avoir observé la vérification passer.

Cela vaut pour la CI, les mesures de performance, les statuts des produits de l'écosystème,
et le retrait de circulation des jetons Solana — annoncé dans le livre blanc, non prouvé
publiquement à ce jour.

## Travailler à plusieurs

- `git fetch` puis rebase **avant** tout push : ne pas écraser le travail de l'autre.
- Un commit par intention, avec un message qui dit **pourquoi**, pas seulement quoi.
- Les propositions touchant le genesis, les contrats, les clés ou `coinbosa/deploy/` sont
  relues avant d'aller en production.
