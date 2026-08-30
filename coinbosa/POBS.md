# PoBS — preuve d'enjeu bornée

*Spécification. Rien de ce qui suit n'est construit à ce jour.*

**Preuve d'enjeu bornée** — *Proof of Bounded Stake*, **PoBS**. Le nom désigne
une propriété vérifiable du dispositif : les validateurs immobilisent des BOSA
pour entrer dans un ensemble dont **le nombre de places est plafonné à 41**
(`MAX_VALIDATORS`, inscrit dans le contrat système du bloc 0). C'est ce qui le
distingue d'une preuve d'enjeu ouverte, où le nombre de producteurs n'est pas
borné.

Ce document arrête ce qu'il faut construire, ce qui l'empêche aujourd'hui, et ce
qui reste à trancher. Il ne décrit aucun état atteint.

---

## 1. État vérifié le 30 août 2026

Mesuré sur la chaîne et dans le dépôt, pas de mémoire.

| Fait | Vérification |
|---|---|
| Le contrat système ne contient **aucune** notion d'enjeu | 0 occurrence de `stake`, `bond`, `slash`, `jail`, `delegat`, `unbond` dans `CoinbosaValidatorSet.sol` |
| La pile d'enjeu amont est **absente** | `eth_getCode` sur `0x…2001` (Staking), `0x…2002` (StakeHub), `0x…2003` (StakeCredit) → **0 octet** aux trois |
| La sanction est **inerte** | `SlashIndicator` déployé (7 339 octets) à `0x…1001`, mais `misdemeanor(address)` et `felony(address)` sont absents du contrat cible : l'appel échoue |
| Un échec de sanction **n'arrête pas** la chaîne | l'erreur est journalisée et avalée symétriquement, côté production et côté vérification (`parlia.go`) |
| Places de validateur | **41** au maximum |
| Validateurs aujourd'hui | **1** |
| Offre | 700 000 000 BOSA, sur **13 clés simples** — aucun contrat, aucun multi-signatures |

**Conséquence :** aujourd'hui, aucun validateur ne risque de fonds. Le consensus
est une preuve d'autorité. C'est ce que le site et le livre blanc écrivent, et
cela reste vrai tant que ce document n'est pas exécuté.

---

## 2. Les deux verrous du contrat figé

Le bytecode de `CoinbosaValidatorSet` est inscrit dans le bloc 0. Il fixe la
racine d'état, donc l'empreinte du bloc 0, donc **l'identité de la chaîne**.
En modifier la logique produirait un autre réseau, et le réseau en production
deviendrait inatteignable. Ce contrat ne sera donc jamais modifié.

Il porte deux verrous qui commandent toute la conception.

### Verrou 1 — le gouverneur est une constante

```solidity
address public constant GOVERNOR = 0x…;
function updateValidatorSet(...) external {
    require(msg.sender == GOVERNOR, "only governor");
```

Seule cette adresse peut changer le jeu de validateurs, et elle est gravée dans
le bytecode. **Un contrat d'enjeu ne peut donc pas piloter le jeu de validateurs
lui-même** : il n'est pas le gouverneur et ne peut pas le devenir.

Vérifié sur la chaîne : cette adresse ne porte aucun code. C'est une clé simple,
ni multi-signatures, ni délai.

### Verrou 2 — le validateur de genèse est permanent

```solidity
require(sealerPresent, "genesis validator must remain a validator");
```

`updateValidatorSet` refuse tout ensemble qui ne contient pas
`INITIAL_VALIDATOR`. Cette garde a été écrite pour empêcher un arrêt de chaîne —
elle garantit qu'au moins un signataire disposant d'une clé de scellage reste
dans l'ensemble. Elle a un revers : **le validateur de genèse ne peut jamais
être sorti, ni sanctionné, ni remplacé.** Une place sur 41 est immuable.

Cela doit être écrit dans le livre blanc. Un lecteur qui découvre seul qu'un
validateur est inéjectable le lira comme une dissimulation.

---

## 3. Trois voies, et celle que je recommande

### Voie A — PoBS gouverné *(la plus simple, la moins convaincante)*

Le contrat d'enjeu tient les dépôts et calcule l'ensemble élu. Le gouverneur lit
ce résultat et appelle `updateValidatorSet`.

- Aucun changement du client, aucun fork.
- **La confiance reste entièrement sur la clé du gouverneur.** Il peut ignorer
  l'élection, ou élire qui il veut. Ce n'est pas une preuve d'enjeu : c'est une
  preuve d'autorité avec un contrat d'enjeu à côté. Une bourse le verra.

### Voie B — bifurcation du client *(recommandée)*

Le client est modifié pour lire l'ensemble des validateurs dans le **nouveau**
contrat d'enjeu à partir d'une hauteur de bloc convenue, au lieu de `0x…1000`.
Le genesis n'est pas touché : l'identité de la chaîne est préservée.

- C'est le chemin qu'a suivi BNB Chain avec sa propre couche d'enjeu.
- L'élection devient réellement automatique : le gouverneur n'est plus dans la
  boucle.
- **Le moment le moins coûteux pour le faire est maintenant.** Une bifurcation
  exige que tous les nœuds passent à la nouvelle règle au même bloc. Il y a
  aujourd'hui **un validateur, un nœud RPC, aucune bourse, aucun indexeur
  tiers** : la coordination est triviale. Dans un an, avec des places d'échange
  et des intégrations, elle devient un chantier à part entière.

Le verrou 2 subsiste : le validateur de genèse reste permanent, puisque c'est le
contrat figé qui le garantit et que le nouveau contrat ne peut pas le contredire
sans risque. À moins de le traiter explicitement dans la nouvelle règle — à
trancher, voir §4.

### Voie C — repartir d'un genesis neuf

Tout est propre, rien n'est figé. Mais on perd les 387 000 blocs déjà produits, l'empreinte du
bloc 0 publiée partout, et la chaîne redevient un projet du jour. **Non
recommandée** tant que la voie B est ouverte.

---

## 4. Les paramètres — arrêtés le 30 août 2026

**Voie retenue : B — bifurcation du client.**

| Paramètre | Valeur arrêtée |
|---|---|
| Enjeu minimum | **1 000 BOSA** (10²¹ wei) |
| Période de déblocage | **7 semaines — 49 jours** |
| Places | **41**, dont **une occupée à vie** par le validateur de genèse |
| Délégation | **non ouverte** au premier jalon |

### Réserve énoncée sur le minimum, et ce que la conception en fait

1 000 BOSA représente **0,000143 %** de l'offre. Les 41 places coûtent donc
41 000 BOSA, soit **0,0059 % de l'offre** — la plus petite adresse de trésorerie
pourrait les acheter **341 fois**. À ce niveau, l'enjeu ne protège pas
économiquement le consensus : il rend l'entrée traçable et engage un dépôt, rien
de plus. Ce n'est pas une objection à la valeur choisie, c'est ce qu'elle
signifie, et le livre blanc devra le dire ainsi plutôt que laisser croire à une
garantie économique.

**Conséquence de conception :** le minimum est un **paramètre de gouvernance
borné**, modifiable sans nouvelle bifurcation. Borné, parce qu'un minimum
librement modifiable serait une porte dérobée : il suffirait de le porter à
100 000 000 pour vider les 41 places d'un coup.

### La période de déblocage

49 jours, soit plus que Cosmos (21) et Polkadot (28). C'est un choix
conservateur, et il est bon : le déblocage ne protège que s'il dépasse le délai
de **détection** d'une faute. Réserve à traiter : tant qu'aucun détecteur
automatique de double signature n'existe, la durée du déblocage ne protège de
rien, quelle qu'elle soit. Le détecteur est donc un prérequis, pas un
raffinement.

### Décisions du 31 août 2026

**Chemin d'écriture → calcul de vivacité côté client, depuis les en-têtes.**
Le contrat n'a pas besoin qu'on lui écrive qui a scellé : le client compte
lui-même les producteurs distincts sur les 200 derniers en-têtes canoniques
(`header.Coinbase`), donnée qu'il possède déjà. C'est strictement meilleur qu'une
transaction système par bloc :

- rien de plus n'est ajouté au chemin de consensus, donc aucune façon
  supplémentaire d'arrêter la chaîne ;
- la donnée est **déterministe** — mêmes en-têtes, même compte sur tous les
  nœuds — donc aucun risque de scission ;
- aucun coût en gaz, aucune transaction à émettre, rien à ordonnancer.

Le client refuse alors tout ensemble de taille supérieure à `2a−1`, où `a` est le
nombre de producteurs distincts observés. Le contrat garde sa propre garde : deux
verrous indépendants valent mieux qu'un.

**Capture → le plancher reste à 1 000 BOSA, et la sécurité est dite pour ce
qu'elle est.**
Relever le plancher ne réglerait pas le problème de lancement : tant que personne
n'a immobilisé de BOSA, prendre les places est bon marché **quel que soit le
minimum**. Ce qui coûte cher à un attaquant, ce n'est pas d'atteindre le
plancher, c'est de **surenchérir sur les titulaires en place** — et cette
protection croît d'elle-même à mesure que de l'enjeu réel entre.

Le plancher bas est donc conservé parce qu'il garde l'entrée accessible, ce qui
est un argument. En contrepartie, le livre blanc doit écrire que **la sécurité
économique d'une preuve d'enjeu naissante est faible, et qu'elle croît avec
l'enjeu déposé** — plutôt que de laisser croire à une garantie immédiate.

### Ce qui reste ouvert

| Point | Question |
|---|---|
| Sévérité des sanctions | Quel montant retiré pour une absence de production, quel montant pour une double signature ? |
| Destination des fonds sanctionnés | Retrait de circulation, ou redistribution aux validateurs honnêtes ? Ni au gouverneur ni à l'éditeur. |
| Amorçage du validateur de genèse | Immobilise-t-il 1 000 BOSA comme les autres, ou est-il inscrit d'office ? |
| Détenteur du pouvoir de sanction | Le client, un contrat, ou une adresse ? |

### Recommandations d'origine, conservées pour mémoire

### 4.1 Le dépôt

| Paramètre | Question | Recommandation |
|---|---|---|
| Montant minimum | Combien un validateur doit-il immobiliser ? | Un montant qui rende une attaque plus coûteuse que son gain. Avec 41 places et 700 M d'offre, un minimum trop bas rend les places achetables ; trop haut, personne ne candidate. À fixer **en proportion de l'offre**, pas en valeur absolue. |
| Origine des fonds | Un validateur peut-il utiliser des BOSA reçus de la trésorerie ? | Non, sinon l'enjeu est fictif : l'éditeur se cautionne lui-même. |
| Délégation | Un porteur peut-il déléguer à un validateur sans opérer de nœud ? | **Pas au premier jalon.** La délégation double la surface (parts, récompenses, retraits partiels) et n'est pas nécessaire pour sortir de la preuve d'autorité. |

### 4.2 Le retrait

| Paramètre | Question | Recommandation |
|---|---|---|
| Période de déblocage | Combien de temps entre la demande de retrait et la disponibilité des fonds ? | Elle doit dépasser le délai de détection d'une faute. Trop courte, un validateur fautif retire avant d'être sanctionné. C'est **le paramètre le plus important du dispositif**. |
| Retrait partiel | Peut-on descendre sous le minimum sans sortir ? | Non. Sous le minimum, la place est libérée. |

### 4.3 La sanction

C'est ce qui distingue une preuve d'enjeu d'un dépôt de garantie décoratif.

| Faute | Question | Recommandation |
|---|---|---|
| Absence de production | Un validateur qui ne scelle pas son tour | Sanction faible et progressive, puis mise en quarantaine. Une panne n'est pas une malveillance. |
| Double signature | Deux blocs différents à la même hauteur | Sanction lourde et immédiate. C'est la seule faute qui attaque la chaîne elle-même. |
| Destination | Où vont les fonds sanctionnés ? | Ni au gouverneur, ni à l'éditeur — sinon la sanction devient un revenu, donc une incitation perverse. Retrait de circulation ou redistribution aux validateurs honnêtes. |

**Rappel technique :** `SlashIndicator` est déployé mais ses points d'entrée
n'existent pas dans le contrat cible. La sanction ne fonctionnera que par le
nouveau contrat, dans la voie B.

### 4.4 L'élection

| Paramètre | Question | Recommandation |
|---|---|---|
| Critère | Qui occupe les 41 places ? | Les 41 plus gros enjeux, ou une borne par validateur pour éviter qu'un acteur en prenne plusieurs. À trancher. |
| Fréquence | À quel rythme l'ensemble est-il recalculé ? | À l'epoch (200 blocs), pour rester aligné sur le mécanisme existant. |
| Place du validateur de genèse | Occupe-t-il une des 41 places, ou une 42ᵉ hors élection ? | **Décision à prendre et à publier.** Le verrou 2 le rend permanent quoi qu'il arrive. |

---

## 5. Procédure de bascule

Aucune de ces étapes ne doit être improvisée : la chaîne a un seul validateur, et
une erreur de bascule l'arrête définitivement.

1. Écrire le contrat d'enjeu et son banc de test.
2. Rejouer la bascule **sur une chaîne jetable**, pas sur la production.
3. Vérifier que le piège documenté est levé : passer de 1 à *n* validateurs
   arrête le réseau au bloc d'epoch suivant si les entrants n'ont pas été **vus
   sceller**. Parlia exige ⌊N/2⌋+1 signataires distincts **et en ligne** — établi
   par test exécuté.
4. Provisionner les serveurs des validateurs entrants, sur des hébergeurs et des
   zones distincts.
5. Faire produire des blocs à chaque entrant sur la chaîne jetable, et le
   constater.
6. Fixer la hauteur d'activation, et publier la version du client.
7. Basculer, en gardant la possibilité de revenir en arrière tant que la hauteur
   n'est pas franchie.

---

## 6. Ce qui doit être réglé AVANT

Trois blocages précèdent toute couche d'enjeu. Les ignorer reviendrait à bâtir
sur du sable.

1. **La clé de scellage n'est sauvegardée nulle part.** Ni sur le serveur, ni
   dans la sauvegarde froide, qui ne contient que l'identité réseau. Si la
   machine est perdue, plus aucun bloc n'est jamais produit — et une couche
   d'enjeu n'y changerait rien.
2. **Le gouverneur est une clé simple**, et il commande le jeu de validateurs.
   Dans la voie A, il commanderait aussi l'élection. Une clé unique qui contrôle
   simultanément la monnaie et le consensus est le risque dominant du projet.
3. **Les 13 adresses de trésorerie sont des clés simples**, dérivées d'une seule
   graine. Tant que c'est vrai, l'enjeu immobilisé par un validateur tiers est
   garanti par un dispositif que l'éditeur peut vider seul.

---

## 7. Ce que ce document ne dit pas

- Aucun montant, aucune durée, aucun taux : ce sont les décisions du §4.
- Aucune date. Le jalon dépend de décisions non prises.
- Aucune affirmation que PoBS existe. **Tant que ce document n'est pas exécuté,
  le consensus de Coinbosa est une preuve d'autorité**, et le site doit continuer
  de l'écrire ainsi.
