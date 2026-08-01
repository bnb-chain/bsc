<div align="center">
  <img src="../assets/coinbosa-logo.jpg" alt="Coinbosa" width="90" />

  # Audit interne — sécurité, bugs, dysfonctionnements
</div>

Audit adversarial automatisé de l'ensemble du code Coinbosa (contrats & consensus, genesis
& scripts, sécurité web, secrets, dysfonctionnements, honnêteté, déploiement). Chaque
trouvaille a été soumise à une contre-vérification (tentative de réfutation) avant d'être
retenue. **36 trouvailles brutes → 19 confirmées → correctifs ci-dessous.**

> Cet audit est une auto-évaluation. Il **ne remplace pas** un audit de sécurité externe,
> qui reste requis avant toute mise en valeur du réseau (voir [SECURITY-HARDENING.md](SECURITY-HARDENING.md)).

---

## Corrigé

### Consensus — `contracts/CoinbosaValidatorSet.sol`

| Sévérité | Problème | Correctif |
|---|---|---|
| **Majeur** | `init()` contenait `require(!alreadyInit)` — un **vecteur de revert sur le chemin de consensus** (viole la règle d'or). Une transaction utilisateur appelant `init()` avant la system-tx du bloc 1 (trivial à gasPrice 0) faisait **revert** la system-tx → **bloc 1 improduisible → chaîne arrêtée au démarrage**. | Garde **idempotente** : `if (alreadyInit) return;`. Tout appel superflu devient un no-op ; plus aucun revert possible. |
| Info | Les cumuls de `deposit()` (`+=`) étaient en arithmétique *checked* (revert théorique sur overflow) sur un chemin de consensus. | Enveloppés dans `unchecked { … }` (l'offre fixe de 700 M exclut tout overflow réel). |

### Genesis & scripts

| Sévérité | Problème | Correctif |
|---|---|---|
| **Majeur** | `build-genesis.js` : le `GOVERNOR` était injecté par `String.replace` **sans vérifier** le remplacement → si le motif changeait, le contrat compilait silencieusement avec le gouverneur par défaut `0x…0001`. | Garde dure : échec si le motif n'est pas trouvé/remplacé. Adresse **checksummée EIP-55**. |
| **Majeur** | Le genesis de **développement** (adresses synthétiques + validateur crédité) s'écrivait au **même chemin** que la prod, sans marqueur. | Chemin distinct (`genesis-coinbosa-dev.json`) + marqueur `coinbosaDev`. `check-supply.js` **refuse** un genesis marqué dev. |
| **Majeur** | `check-supply.js` lisait les soldes à `latest` — or la **base fee brûlée** (EIP-1559) fait diminuer l'offre après le genesis → **faux échec** sur une chaîne saine. | Soldes lus **au bloc 0** (genesis) : reflète l'allocation initiale, stable, et compare le genesis déployé au fichier local, adresse par adresse. |
| Mineur | `check-blocktime.js` : boucle d'attente **sans borne** (boucle infinie si la chaîne est figée). | Borne d'arrêt : échec si trop peu de blocs après un délai. |
| Mineur | `genesis-base.json` : adresse validateur réelle `0x9822…` figée dans l'extraData du gabarit. | Mise à **zéro** (l'extraData est de toute façon réécrit par `build-genesis.js`). |
| Mineur | `coinbosa.config.json` : commentaire de répartition « 650 M » (faux, c'est 700 M) ; `projectHeld` à 450 M (le projet détient toute l'offre Solana de 500 M) ; chiffres « ~180 M » non vérifiés. | Alignés : 700 M, 500 M, chiffres non vérifiés retirés des commentaires. |

### Sécurité web — `explorer/index.html` & `site/index.html`

| Sévérité | Problème | Correctif |
|---|---|---|
| **Majeur** | Explorateur **sans `<meta viewport>`** → tout le CSS responsive neutralisé sur mobile. | Balise ajoutée. |
| **Majeur** | Décodage ABI de `getMiningValidators()` : offset des adresses à **256 au lieu de 192** (mot 4 au lieu du mot 3) → l'explorateur affichait de **faux validateurs** (erreur masquée par un `try/catch`). | Offset corrigé à 192. |
| Mineur | Échappement `esc()` limité à `name()`/`symbol()` ; autres champs RPC interpolés bruts, y compris dans des `onclick`. | `short()` échappe sa sortie ; `hx()` valide les valeurs hex des `onclick` (contexte chaîne JS, où `esc()` ne protège pas) ; `safeUrl()` restreint les `href`. |
| Mineur | Garde `?rpc=` : regex d'hôte **non ancrée** (`localhost.attacker.tld` matchait). | Regex ancrée en correspondance exacte. |
| Mineur | Lien livre blanc du site sur la branche `main` (inexistante) ; `master` ailleurs. | Aligné sur `master` (branche par défaut). |
| Info | `href` (produits, liens sociaux) assignés sans liste blanche de schéma. | Schéma validé (`http(s)`/relatif/ancre) avant assignation, côté site et explorateur. |

---

## Écarté après vérification (faux positifs ou décisions d'ingénierie)

- **`approve` race (BRC20)** — comportement standard ERC-20, accepté ; `increase/decreaseAllowance` sont fournis. Aucun correctif contractuel requis.
- **`check-epoch.js` — suffixe d'attestation Plato** — le contrôle strict de longueur d'extraData est **correct pour cette configuration** (pas de VotePool, clés de vote à zéro → aucun suffixe d'attestation produit). Le rendre « tolérant » affaiblirait un contrôle d'intégrité valide. À revoir **uniquement** si l'attestation Plato est un jour activée.
- **`check-blocktime.js` — « le temps de bloc n'est pas dans le genesis »** — le commentaire est **exact** : `ParliaConfig` est une structure vide en v1.7.6, `period`/`epoch` sont des constantes Go. Les champs présents dans `genesis-base.json` sont inertes (ignorés par le client).

## Reste à faire (hors correctif de fichier)

- **`start-node.sh`** est explicitement **développement uniquement** (clé déverrouillée dans le process, `eth` exposé). La configuration de production (signeur distant, RPC fermé) est décrite dans [SECURITY-HARDENING.md](SECURITY-HARDENING.md) — c'est un chantier serveur, pas un correctif de fichier.
- **Politique de frais (EIP-1559)** : confirmer **sur la chaîne réelle** que le client patché redirige la base fee au lieu de la brûler (test on-chain : somme des soldes avant/après une transaction), sinon fixer `baseFeePerGas` au genesis et documenter la politique.
- **Compilation & tests** : la compilation du contrat et le **banc BRC20** (métadonnées, transferts, autorisations, émission et sa clôture définitive, destruction, propriété en deux étapes et abandon, événements) s'exécutent en **intégration continue** sur chaque push (machine vierge). L'état du dernier run se lit dans l'onglet Actions — il n'est pas déclaré « vert » avant d'avoir été observé vert.

---

## Verdict

Pour le **tier public** (site, explorateur, livre blanc) et le **code** : **prêt après ces correctifs**.
Pour la **chaîne porteuse de valeur** : inchangé — un **audit externe**, la **mise sous multi-signatures**
de l'offre et le **passage à plusieurs validateurs** restent des prérequis (feuille de route).

---

## Re-scan go-live (2ᵉ passe)

Un second re-scan complet (9 chasseurs → 3 relecteurs seniors → verdict) a **re-vérifié ligne à ligne**
le chemin de consensus et le chemin argent/genesis de production : **SAINS**, correctifs de la 1ʳᵉ passe
confirmés. Défauts additionnels corrigés :

| Sévérité | Problème | Correctif |
|---|---|---|
| **Majeur** | Livre blanc : la « Note de correction » affirmait que les jetons Solana/BNB « ont des détenteurs et relèvent de la migration » — contredit tout le reste (projet détient tout, non migrés). | Reformulé : sans détenteurs tiers, aucune migration (HTML + `WHITEPAPER.md`). |
| **Verrou** | `.gitignore` n'excluait que `node1/`/`node2/` — un datadir renommé aurait pu versionner la **clé du validateur**. | `node*/`, `keystore/`, `**/keystore/`, `UTC--*`, `*.key`, `.env*`. |
| Reco | Pages servies en direct sans `<!DOCTYPE html>` → *quirks mode*. | Doctype + `<html lang="fr">` sur site, explorateur, livre blanc. |
| Reco | Explorateur : réponse RPC hostile pouvait geler/planter la page (`n` non borné, `coinv()` throwait). | `n` borné (≤128) ; `coinv()` renvoie 0 sur hex invalide. |
| Reco | Lien livre blanc du site divulguait le dépôt GitHub. | Pointe vers `https://coinbosa.com/whitepaper/` (hébergé). |
| Mineur | Cosmétique/robustesse : garde de schéma du lien explorateur (nav), echo `:8595`→`:8545`, commentaire `coinbosaDev`, `sshd -T` sous `set -e`, IP opérateur fail2ban via `sudo`. | Corrigés. |

**Reste (opérationnel, non-code) :** remplir `distribution-addresses.json` avec de vraies adresses
multi-signatures générées **sur le serveur cible** (déjà gardé : `build-genesis.js` refuse la prod sinon).
**Post-lancement acceptable :** en-tête CSP Caddy (à ajouter après vérification du rendu en ligne),
`Ownable2Step` + plafond de mint pour les **contrats de jeton BRC20** applicatifs (sans effet sur le coin
natif BOSA, dont l'offre est fixée au genesis).

**Verdict go-live du tier public : PRÊT.**
