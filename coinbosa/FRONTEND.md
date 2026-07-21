<div align="center">
  <img src="assets/coinbosa-logo.jpg" alt="Coinbosa" width="120" />

  # Site web et explorateur — spécification
</div>

---

## L'exigence

Un site au niveau des grandes chaînes publiques — Solana, Avalanche, Sui — et multilingue.
Même exigence pour l'explorateur.

Ce document existe parce que « faire comme Solana » n'est pas une consigne exploitable telle
quelle. Ce qui suit traduit cette exigence en décisions concrètes : ce qui est en jeu, comment
c'est construit, et comment on saura que c'est atteint.

---

## Ce qui fait la différence sur ces sites

Les sites de référence ne se distinguent pas par un effet visuel, mais par quatre choses
mesurables :

**La vitesse perçue.** Le contenu apparaît en moins d'une seconde, sans écran de chargement.
Cela impose un rendu côté serveur ou statique, pas une application qui s'assemble dans le
navigateur.

**Le mouvement, avec parcimonie.** Une animation d'entrée orchestrée, quelques réactions au
survol. Jamais d'animation permanente qui tourne dans le vide : c'est la marque des sites
faits à la chaîne.

**La typographie.** Une hiérarchie franche, des titres larges et serrés, un corps de texte
aéré. C'est ce qui sépare visuellement un site professionnel d'un gabarit, bien plus que la
palette.

**La densité maîtrisée.** Beaucoup d'informations, jamais tassées. Le vide est utilisé comme
matériau.

Un site « joli » sans ces quatre points reste un gabarit. Un site qui les respecte tient la
comparaison même avec une palette sobre.

---

## Identité visuelle

La palette n'est pas inventée : elle est extraite du logo.

| Rôle | Valeur | Origine |
|---|---|---|
| Or | `#CB9211` | lettrage du logo |
| Or clair | `#E0AA2E` | survol, états actifs |
| Bleu | `#015E99` | disque du logo |
| Bleu clair | `#2B86C4` | liens, accents secondaires |
| Fond sombre | `#060C14` | noir bleuté, dérivé du fond du logo |
| Fond clair | `#F5F7FA` | |

**Règle d'usage** — l'or est réservé aux actions et aux points d'attention. Le bleu porte les
surfaces et la profondeur. Si les deux se disputent l'attention sur un écran, c'est l'or qui
recule.

Le logo ne doit jamais être recoloré, détouré, ni recadré.

Les deux thèmes, clair et sombre, sont obligatoires. L'explorateur suit déjà cette palette et
sert de référence d'implémentation.

---

## Les langues

Six langues au lancement. La liste est extensible : ajouter une langue consiste à ajouter une
entrée au dictionnaire, rien d'autre.

| Code | Langue | Sens d'écriture |
|---|---|---|
| `en` | English | ltr |
| `fr` | Français | ltr |
| `es` | Español | ltr |
| `pt` | Português | ltr |
| `ar` | العربية | **rtl** |
| `zh` | 中文 | ltr |

### Ce que l'arabe impose

L'écriture droite-à-gauche n'est pas une traduction de plus : elle inverse la mise en page.
Elle doit être prise en compte dès le premier écran, parce que la rattraper plus tard suppose
de reprendre chaque règle de style.

En pratique : utiliser `margin-inline-start` plutôt que `margin-left`, `inset-inline-start`
plutôt que `left`, `text-align: start` plutôt que `left`. Les flèches et les chevrons doivent
se retourner. Les nombres, les adresses et les empreintes restent en gauche-à-droite.

L'explorateur applique déjà ces règles ; s'en inspirer plutôt que de repartir de zéro.

### Ce qui ne doit jamais être codé en dur

Les nombres, les dates et les durées passent par `Intl.NumberFormat` et
`Intl.RelativeTimeFormat` avec la locale active. Un « il y a 3 min » écrit à la main sera faux
dans cinq langues sur six.

### Référencement multilingue

Chaque langue a sa propre URL — `/fr/`, `/ar/`… — jamais un simple paramètre. Les pages se
déclarent mutuellement par `hreflang`, avec un `x-default` sur l'anglais. Une langue servie
sans URL propre n'est pas indexée, et le travail de traduction ne rapporte rien.

---

## Le site

### Technique

Framework à rendu statique ou serveur — Next.js, Astro ou SvelteKit. Le critère n'est pas la
préférence, mais le rendu : une page servie déjà écrite, pas assemblée dans le navigateur.

Traductions dans des fichiers JSON, une par langue, chargées à la demande. Aucune chaîne de
caractères visible ne vit dans le code des composants.

### Pages

| Page | Rôle |
|---|---|
| Accueil | ce qu'est Coinbosa, en un écran |
| Le réseau | consensus, validateurs, performances mesurées |
| BOSA | le jeton, ses usages, où l'obtenir |
| Écosystème | Academy, NextFuture, Card, Neobanq, VPN |
| Développeurs | comment se connecter, déployer, intégrer |
| Explorateur | lien vers l'explorateur |

### Connexion au réseau

Un bouton « ajouter Coinbosa à mon wallet » qui déclenche `wallet_addEthereumChain`. Un
visiteur doit pouvoir passer du site à une transaction sans copier-coller un seul paramètre.

---

## L'explorateur

L'explorateur actuel (`explorer/index.html`) est déjà multilingue, aux couleurs de la marque,
en thème clair et sombre. Il est **volontairement minimal** : il interroge le RPC en direct,
sans base de données.

Ce que cela interdit, et qui devra être repris dans la version indexée :

- l'historique des transactions d'une adresse
- la recherche sur autre chose qu'un identifiant exact
- la liste des porteurs d'un jeton
- la vérification publique du code source des contrats

La version de production suppose donc un indexeur. **Attention licence** : Blockscout n'est
plus open source depuis le 22 avril 2026, et ses versions 11 et suivantes interdisent
contractuellement de retirer la marque. Pour un produit à notre nom, épingler `v10.2.6`,
dernière version sous GPLv3.

L'interface actuelle sert de référence de style : reprendre sa palette, ses composants et son
mécanisme de langue plutôt que de repartir d'un gabarit.

---

## Critères de réception

Un livrable est accepté s'il satisfait **tous** les points suivants. Ce sont des mesures, pas
des impressions.

**Performance** — Lighthouse au-dessus de 90 en performance et en accessibilité, sur mobile,
sur connexion lente simulée. Premier affichage utile sous 1,5 s.

**Langues** — les six langues complètes, sans chaîne non traduite. L'arabe correctement
inversé, vérifié écran par écran. Aucun nombre ni aucune date formatés à la main.

**Thèmes** — clair et sombre traités avec le même soin. Le thème sombre n'est pas une
inversion du clair.

**Responsive** — de 320 px à 2560 px, sans débordement horizontal. Les tableaux et le code
défilent dans leur propre conteneur, jamais la page.

**Accessibilité** — navigation complète au clavier avec focus visible, contraste minimum 4,5:1
sur le texte, `prefers-reduced-motion` respecté.

**Référencement** — une URL par langue, `hreflang` réciproques, `x-default` sur l'anglais.

---

## Ce qu'il faut éviter

Ces choix trahissent immédiatement un site fait sans direction artistique :

- le dégradé violet-bleu sur fond blanc, devenu le cliché du site crypto
- les animations permanentes en arrière-plan, qui consomment de la batterie sans rien apporter
- les emoji en guise d'icônes de section
- tout centrer par défaut
- les textes de remplissage laissés en production
- les captures d'écran d'interface qui n'existent pas encore

Et surtout : annoncer des chiffres non mesurés. Le livre blanc mentionne 400 000 transactions
par seconde. Tant que ce chiffre n'a pas été mesuré sur le réseau réel, il ne doit apparaître
nulle part — le premier visiteur technique qui le vérifiera décrédibilisera tout le reste.
