# Sauvegarde et restauration de la clé de scellage

**Coinbosa Chain — chainId 26262 — un seul validateur.**

> ⚠ **Ce dépôt est public.** Ce document ne contient donc **aucun secret** : ni clé, ni
> mot de passe, ni empreinte de mot de passe. Il ne contient que des chemins, des
> tailles, des commandes et des critères. Ne le complétez jamais avec un secret.

---

## 0. Pourquoi ce document existe

La chaîne Coinbosa a **un seul validateur**. Une seule clé privée signe chacun de ses
blocs. Tant qu'elle existe, la chaîne avance ; le jour où elle disparaît, plus aucun
bloc n'est produit sur chainId 26262 — **définitivement**.

Et il n'y a pas de rattrapage. Le contrat système `0x…1000` permet bien de changer
l'ensemble des validateurs, mais cela suppose d'**envoyer une transaction**, donc de la
faire **miner**, donc de disposer… de la clé de scellage. C'est un verrou circulaire, et
`scripts/rotate-validators.js` le dit déjà noir sur blanc :

> « comme plus aucun bloc n'est produit, AUCUNE transaction corrective ne peut être minée :
> on ne peut pas défaire l'opération on-chain. »

**Perdre la clé de scellage, c'est perdre la chaîne.** Pas la ralentir : la perdre. Les
soldes, l'historique, l'adresse de contrat de chaque jeton — tout reste lisible dans une
base morte que plus rien ne fait avancer. Une place d'échange qui a listé le BOSA voit
ses retraits se figer.

Ce document décrit donc trois choses, et rien d'autre :

1. **quoi copier**, exactement ;
2. **où le ranger**, et pourquoi à deux endroits qui ne se touchent jamais ;
3. **comment prouver qu'une sauvegarde est bonne** — sans la mettre en production.

Le point 3 est le cœur du sujet. **Une sauvegarde qui n'a jamais été restaurée n'est pas
une sauvegarde : c'est une hypothèse.** Le script `repetition-restauration.sh`, à côté de
ce fichier, transforme l'hypothèse en fait mesuré, sur une chaîne jetable.

---

## 1. L'état constaté

Relevé le 2026-08-31 sur `coinbosa-vps`, en lecture seule.

### 1.1 Où vit la clé aujourd'hui

```
ls -la /var/lib/coinbosa/validator
find /var/lib/coinbosa/validator/keystore -type f \
  -printf "%p|mode=%m|%u:%g|%s octets|%TY-%Tm-%Td\n"
```

| Élément | Chemin | Droits | Propriétaire | Taille |
|---|---|---|---|---|
| Coffre (clé chiffrée) | `/var/lib/coinbosa/validator/keystore/UTC--2026-08-06T12-43-18.846416274Z--3986d6b31ec55043ceaaf25f5ddea53517cbba50` | `600` | `coinbosa-val` | 491 o |
| Mot de passe | `/var/lib/coinbosa/validator/pw.txt` | `400` | `coinbosa-val` | 45 o |
| Copie du mot de passe | `/root/coinbosa-secrets/validator-password.txt` | `400` | `root` | 45 o |

Le répertoire parent est en `700 coinbosa-val`. Le compte du nœud RPC public
(`coinbosa`) ne peut pas le lire : cette séparation-là est correcte et il faut la garder.

### 1.2 Ce qui manque — le constat qui compte

```
find / -xdev -type f -name "UTC--*" -not -path "/proc/*" -not -path "/sys/*"
```

Trois résultats sur toute la machine, et **un seul est la clé de production** ; les deux
autres sont des fichiers de test livrés avec le code source (`accounts/keystore/testdata`
et un module Go de Prysm).

```
tar -tzf ~/Desktop/coinbosa-froid.tgz | grep -iE "keystore|UTC--|pw.txt|password"
  -> aucune correspondance (code de sortie 1)
```

La sauvegarde froide de l'éditeur contient `nodekey-node`, `nodekey-validator`, le
genesis, les adresses de distribution et `chaindata-node.tgz`. **Elle ne contient pas le
coffre.** Son propre `RESTAURER.txt` l'annonce d'ailleurs :

> « le coffre du validateur (UTC--…--3986d6b3) et son mot de passe : rangés SÉPARÉMENT,
> volontairement. »

Ce rangement séparé **n'a pas été trouvé**. Ni dans `/var/backups` (uniquement des
fichiers Debian : `dpkg.status`, `alternatives.tar`, `apt.extended_states`), ni dans
`/root`, ni dans `/home` (vide), ni dans une archive, ni ailleurs sur le disque.

```
crontab -l                -> no crontab for root
ls /etc/cron.d            -> e2scrub_all, sysstat, .placeholder  (rien de Coinbosa)
systemctl list-timers     -> coinbosa-watchdog, coinbosa-peer, coinbosa-journal
grep -rn "rsync -|scp |sftp|s3://|curl -T" /root/*.sh /etc/systemd/system/*
                          -> aucune correspondance
```

Aucun outil de sauvegarde n'est installé (`restic`, `borg`, `rclone`, `duplicity`,
`age` : tous absents ; seuls `rsync` et `gpg` sont présents mais inutilisés à cet effet).
`coinbosa-journal` n'est pas une sauvegarde : c'est un redémarrage propre planifié.

### 1.3 Le résumé, en une ligne

> **Le mot de passe existe en deux exemplaires — sur le même disque.
> Le coffre existe en un seul exemplaire — sur ce même disque.**

Les deux copies du mot de passe ne protègent de rien : elles tombent ensemble. Et c'est
le mauvais fichier qui a été dupliqué. Le mot de passe, perdu, laisse une chance ; le
coffre, perdu, n'en laisse aucune (§ 7).

### 1.4 Ce que le coffre contient

```
kdf = scrypt   N = 262144   r = 8   p = 1   dklen = 32   cipher = aes-128-ctr   version = 3
adresse = 0x3986d6b31ec55043ceaaf25f5ddea53517cbba50
```

`N = 262144` est le réglage **fort** de geth (et non le réglage « light »). Chaque essai
de mot de passe coûte 256 Mio de mémoire : c'est ce chiffre qui rend une attaque par
force brute déraisonnable (§ 7).

Cette adresse est **inscrite dans l'`extraData` du genesis** et vérifiée au démarrage par
`40-validator.sh`. Elle n'est pas remplaçable par une autre sans changer le bloc 0, donc
sans changer de réseau.

---

## 2. Quoi copier — exactement trois choses

| # | Pièce | Quoi | Taille |
|---|---|---|---|
| **A** | **Le coffre** | le fichier `keystore/UTC--…--3986d6b3…` | 491 o |
| **B** | **Le mot de passe** | la 1ʳᵉ ligne de `pw.txt` | 44 caractères |
| **C** | **La chaîne** | genesis + `chaindata` (déjà couvert par `coinbosa-froid.tgz`) | ~78 Mo |

**A et B ne voyagent jamais ensemble et ne sont jamais rangés ensemble.**

C'est la seule règle non négociable de ce document. Le coffre seul est un fichier
chiffré : le poser sur une clé USB est raisonnable. Le mot de passe seul est un texte
sans objet : le noter sur papier est raisonnable. **Réunis, les deux ne valent plus une
protection : ils valent la clé privée en clair.** Une sauvegarde qui contient les deux
dans la même archive, le même coffre-fort, le même gestionnaire de mots de passe ou le
même envoi de courriel a supprimé le chiffrement — elle a juste l'air d'être sûre.

### Comment geth lit le mot de passe — à connaître avant d'en refaire un

`cmd/utils/flags.go`, `MakePasswordListFromPath` :

```go
text, err := os.ReadFile(path)
lines := strings.Split(string(text), "\n")
for i := range lines { lines[i] = strings.TrimRight(lines[i], "\r") }
```

geth prend la **première ligne**, `\r` final retiré. Donc :

* une espace en fin de ligne **fait partie** du mot de passe ;
* un fichier recréé avec `echo` ajoute un `\n` — sans effet, c'est ce qu'il faut ;
* un fichier recréé avec `echo -n` fonctionne aussi ;
* un éditeur qui « nettoie » les espaces de fin **casse le mot de passe silencieusement**.

Le fichier de production fait 45 octets : **44 caractères + un saut de ligne**.

---

## 3. Où les ranger, et pourquoi deux endroits distincts

### 3.1 La règle

* **Chaque pièce en au moins deux exemplaires**, dans deux lieux physiques différents.
  Un exemplaire unique, c'est la situation d'aujourd'hui : un incendie, un vol, un
  disque mort, et c'est fini.
* **Aucun lieu ne détient A et B.** On dresse la liste des lieux ; si un lieu apparaît
  dans les deux colonnes, la sauvegarde est à refaire.

### 3.2 Un rangement qui tient

| | Pièce A — le coffre (chiffré) | Pièce B — le mot de passe (en clair) |
|---|---|---|
| Exemplaire 1 | clé USB, chez l'éditeur, hors du serveur | écrit **à la main** sur papier, enveloppe scellée, coffre-fort |
| Exemplaire 2 | clé USB, chez un tiers de confiance / coffre bancaire | seconde enveloppe scellée, **autre lieu** que l'exemplaire 1 de B |
| Exemplaire 3 (recommandé) | **impression papier** : 491 octets, soit ~660 caractères en base64, ou un QR code | — |

L'exemplaire papier du coffre n'est pas une coquetterie : 491 octets tiennent sur une
feuille, et le papier ne connaît ni la corruption de secteur, ni le format propriétaire,
ni la clé USB qu'on ne relit plus dans cinq ans.

```bash
# fabriquer la version imprimable du coffre (à faire sur la machine hors ligne)
base64 UTC--2026-08-06T12-43-18.846416274Z--3986d6b31ec55043ceaaf25f5ddea53517cbba50 \
  > coffre-a-imprimer.txt
# relecture : doit redonner un fichier de 491 octets
base64 -d coffre-a-imprimer.txt | wc -c        # attendu : 491
```

### 3.3 Ce qu'il ne faut pas faire

* ❌ mettre A et B dans le même gestionnaire de mots de passe ;
* ❌ envoyer l'un ou l'autre par courriel, messagerie, ou les déposer sur un stockage
  en ligne synchronisé avec un poste de travail ;
* ❌ écrire l'**empreinte** du mot de passe (SHA-256 ou autre) où que ce soit. Une
  empreinte non salée est vérifiable **instantanément**, sans le coût de scrypt : la
  publier annule précisément la protection décrite au § 7 ;
* ❌ recopier le mot de passe dans un fichier sur le serveur « pour ne pas l'oublier » —
  c'est exactement ce que fait déjà `/root/coinbosa-secrets/validator-password.txt`, et
  cela n'apporte aucune résilience puisque c'est le même disque.

---

## 4. La procédure de sauvegarde

Toutes les commandes de ce paragraphe **lisent** le serveur. Aucune n'écrit dessus,
aucune ne redémarre quoi que ce soit. Elles sont sans effet sur la production.

Faites-les depuis le **poste de l'éditeur**, pas depuis le serveur : ce qu'on veut, c'est
justement que la copie ne soit pas sur le même disque.

### Étape 1 — préparer un support hors ligne

```bash
mkdir -p ~/coffre-A && chmod 700 ~/coffre-A
```
**Critère :** le répertoire existe et est en `700`.

### Étape 2 — récupérer le coffre (pièce A)

```bash
NOM='UTC--2026-08-06T12-43-18.846416274Z--3986d6b31ec55043ceaaf25f5ddea53517cbba50'
ssh coinbosa-vps "cat /var/lib/coinbosa/validator/keystore/$NOM" > ~/coffre-A/"$NOM"
chmod 600 ~/coffre-A/"$NOM"
wc -c < ~/coffre-A/"$NOM"
shasum -a 256 ~/coffre-A/"$NOM"     # sha256sum sous Linux
```

**Critères, tous les deux obligatoires :**

* taille = **491** octets ;
* empreinte = `c8ff8e21ffa609f72663072aabe2eda95f1b8a00890b545c7615837069e1f894`

Cette empreinte est celle relevée sur le serveur le 2026-08-31. C'est l'empreinte d'un
fichier **déjà chiffré** : la publier ne révèle rien et ne dispense de rien.

> Conservez le **nom de fichier exact**. Il porte l'adresse, et `40-validator.sh` refuse
> de démarrer si le keystore ne contient pas exactement un fichier `UTC--*`.

### Étape 3 — transcrire le mot de passe (pièce B), à la main

Le mot de passe ne doit pas transiter par le poste de travail plus longtemps que
nécessaire, ni finir dans un historique de commandes.

```bash
ssh coinbosa-vps 'cat /var/lib/coinbosa/validator/pw.txt'
```

Recopiez les 44 caractères **à la main**, sur papier, deux fois, dans deux enveloppes
scellées destinées à deux lieux différents. Puis effacez la fenêtre de terminal.

**Critère de transcription :** il ne se vérifie pas à l'œil — il se vérifie à l'étape 4,
qui est faite pour ça. Ne notez **jamais** d'empreinte du mot de passe (§ 3.3).

### Étape 4 — vérifier, sans toucher à la production

C'est l'étape qui transforme une copie en sauvegarde. Voir § 5.

### Étape 5 — la chaîne (pièce C)

Elle est déjà couverte par `~/Desktop/coinbosa-froid.tgz` (78 Mo, genesis + chaindata +
nodekeys). À rafraîchir après chaque arrêt propre, et à ranger **avec la pièce A ou
seule — jamais avec la pièce B**.

**Critère :** `tar -tzf coinbosa-froid.tgz | grep -icE "keystore|UTC--|pw.txt"` renvoie
**0**. L'archive de chaîne ne doit contenir aucun secret.

---

## 5. Vérifier une sauvegarde SANS la mettre en production

Le réflexe naturel — « on verra bien le jour où on en aura besoin » — est précisément
celui qui fait perdre les chaînes. Et le réflexe inverse — restaurer sur le serveur
« pour voir » — est pire : il touche la production.

La bonne méthode est de **déchiffrer le coffre hors ligne** et de vérifier que l'adresse
obtenue est la bonne. C'est tout ce qu'il faut prouver : si le couple (coffre, mot de
passe) redonne `0x3986D6b3…`, alors geth saura sceller avec.

```bash
node coinbosa/deploy/verifier-coffre.js \
     ~/coffre-A/UTC--2026-08-06T12-43-18.846416274Z--3986d6b31ec55043ceaaf25f5ddea53517cbba50 \
     /chemin/vers/mot-de-passe-retranscrit.txt \
     0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50
```

Ce que le script fait : il lit le mot de passe **exactement comme geth** (§ 2), dérive la
clé par scrypt, recalcule l'adresse, la compare. Ce qu'il ne fait pas : ouvrir une
connexion réseau, écrire un fichier, afficher la clé ou le mot de passe.

**Critères :**

| Sortie | Signification |
|---|---|
| `RESULTAT : SAUVEGARDE BONNE`, code **0** | le couple redonne bien `0x3986D6b3…` |
| `incorrect password`, code **1** | mot de passe faux **ou** coffre corrompu |
| `contient une AUTRE cle`, code **1** | le coffre n'est pas celui du validateur |

Le comportement du script a été éprouvé dans les quatre cas :

```
TEST 1  bon mot de passe                     -> code 0, adresse retrouvée
TEST 2  mauvais mot de passe                 -> code 1, « incorrect password »
TEST 3  coffre corrompu d'un seul caractère  -> code 1  (le MAC détecte l'altération)
TEST 4  adresse attendue différente          -> code 1, « contient une AUTRE cle »
```

**Faites-le sur une machine hors ligne**, et effacez le fichier de mot de passe temporaire
après coup (`rm`). Refaites-le **à chaque rotation de la clé, et au moins une fois par
trimestre** : c'est le seul moyen de découvrir une clé USB morte avant d'en avoir besoin.

---

## 6. La restauration

### 6.1 Répétez-la avant d'en avoir besoin

```bash
GETH=/chemin/vers/build/bin/geth bash coinbosa/deploy/repetition-restauration.sh
```

Le script monte une chaîne **jetable** (chainId 26999, sans découverte de pairs, sans
réseau), la fait sceller, la sauvegarde selon ce document, **détruit intégralement** le
répertoire du validateur, restaure depuis les seules sauvegardes, et prouve que la chaîne
repart au même bloc avec la même adresse. Il refuse de démarrer si `coinbosa-validator`
tourne sur la machine ou si `/var/lib/coinbosa/validator` existe.

**Critère : code de sortie 0, et zéro ligne `[ECHEC]`.**

Résultat obtenu le **2026-08-31**, sur macOS arm64, avec `geth 1.7.6` compilé depuis la
branche `coinbosa-genesis-bos20` (chaîne de laboratoire, chainId 26999) :

```
preuves tentees : 15
echecs          : 0

[PREUVE OK] avant sauvegarde : c'est bien la cle jetable qui scelle
[PREUVE OK] l'archive de chaine ne contient aucun secret        (0 entree keystore/pw)
[PREUVE OK] verification hors production de la sauvegarde       (code 0)
[PREUVE OK] le repertoire du validateur n'existe plus
[PREUVE OK] le coffre restaure est bit pour bit celui sauvegarde
[PREUVE OK] le mot de passe restaure est bit pour bit celui sauvegarde
[PREUVE OK] meme reseau : le bloc 0 est identique
[PREUVE OK] meme chaine : le bloc repere #8 a le meme hash (pas un embranchement)
[PREUVE OK] meme hauteur : la chaine repart d'ou elle s'etait arretee   (>= 8, mesure 9)
[PREUVE OK] le scellage a REPRIS : de nouveaux blocs sont produits      (>= 12, mesure 12)
[PREUVE OK] meme adresse : les blocs neufs sont scelles par la cle restauree
[PREUVE OK] le mot de passe est toujours en place (seul le coffre manque)
[PREUVE OK] sans le coffre, le noeud REFUSE de demarrer
            Fatal: Failed to unlock account 0x5ff5D8… (no key for given address or file)
[PREUVE OK] apres restauration du seul coffre, la chaine reproduit des blocs (>= 15, 15)
[PREUVE OK] et toujours avec la meme adresse
```

La dernière ligne du scénario H est celle qui compte le plus : le coffre effacé, geth
**refuse de démarrer** — le mot de passe, pourtant présent, ne sert à rien. Remis, la
chaîne reprend immédiatement. C'est la démonstration que le coffre, et lui seul, est le
point unique de défaillance.

### 6.2 Le sinistre réel — le serveur a disparu

Ordre imposé. Chaque étape a son critère.

| # | Étape | Commande | Critère | Réversible |
|---|---|---|---|---|
| 1 | Machine neuve, code source | `git clone --branch coinbosa-genesis-bos20 …` puis `make geth` | `build/bin/geth version` répond | oui |
| 2 | Socle | `bash coinbosa/deploy/00-bootstrap.sh` | script en code 0 | oui |
| 3 | Restaurer la chaîne (pièce C) | `tar xzf chaindata-node.tgz -C /var/lib/coinbosa/node` | `du -sh` ≈ 78 Mo | oui |
| 4 | Nœud RPC | `bash coinbosa/deploy/30-node.sh` | `eth_blockNumber` répond | oui |
| 5 | **Vérifier le bloc 0** | `eth_getBlockByNumber(["0x0",false])` | hash = `0x8dcdadc247a98f33728cae944e20ce7c49c74b35cfba31495f85e98979018da6` | — |
| 6 | Replacer le coffre (pièce A) | `cp` dans `…/validator/keystore/`, `chmod 600` | 491 octets, empreinte du § 4 | oui |
| 7 | Replacer le mot de passe (pièce B) | écrire `pw.txt`, `chmod 400` | 45 octets, dernier octet `\n` | oui |
| 8 | **Vérifier avant de démarrer** | `node …/verifier-coffre.js …` | code **0**, adresse `0x3986D6b3…` | oui |
| 9 | Validateur | `bash coinbosa/deploy/40-validator.sh` | affiche « conforme à l'extraData du genesis ✓ » | oui |
| 10 | Arrêt propre planifié | `bash coinbosa/deploy/60-journal.sh` | timer `coinbosa-journal` actif | oui |
| 11 | **Preuve finale** | `eth_blockNumber` à 30 s d'intervalle | hauteur augmentée d'au moins **5** ; `miner` = `0x3986D6b3…` | — |

L'étape 5 est un verrou : une empreinte de bloc 0 différente signifie qu'on vient de
créer **un autre réseau**, pas de restaurer celui-ci. On s'arrête là.

L'étape 8 est le second verrou : on vérifie le coffre **avant** de lancer le validateur,
pas après. Un validateur qui démarre sur un mauvais coffre ne scelle rien et laisse
croire à une panne de nœud.

> **Ce qui a été éprouvé, et ce qui ne l'a pas été.**
> Les étapes **6, 7, 8 et 11** — replacer le coffre, replacer le mot de passe, vérifier
> hors ligne, constater que le scellage repart avec la même adresse — ont été jouées pour
> de bon par `repetition-restauration.sh` sur une chaîne jetable (§ 6.1), et elles
> passent. Les étapes **1 à 5, 9 et 10** décrivent une reconstruction de serveur complète :
> elles reprennent `RESTAURER.txt` et les scripts de déploiement, mais **elles n'ont pas
> été exécutées** — le faire supposerait une seconde machine. Tant que ce n'est pas fait,
> traitez-les comme un plan raisonné, pas comme un fait mesuré.
>
> Attention également : `coinbosa-froid.tgz` contient le `chaindata` du **nœud RPC**, pas
> celui du validateur. À la reprise, le validateur repart du genesis et se resynchronise
> depuis le nœud via `coinbosa-peer`. C'est le fonctionnement prévu, mais il rallonge
> l'étape 9 — prévoyez-le plutôt que de le découvrir le jour du sinistre.

### 6.3 Le sinistre partiel — la chaîne est là, le coffre a disparu

C'est le scénario le plus probable, et celui que le § 1.3 rend possible aujourd'hui.
Étapes 6 → 8 → 9 → 11 uniquement. Le script de répétition rejoue précisément ce cas
(section H) : geth **refuse de démarrer** sans le coffre, puis reprend la production dès
qu'on le remet.

---

## 7. Perdre le mot de passe, ou perdre le coffre

Les deux ne se valent pas du tout. C'est la raison pour laquelle la sauvegarde du **coffre**
est plus urgente que celle du mot de passe.

### 7.1 Coût réel d'un essai — mesuré

Sur un Apple M4, un cœur, avec les paramètres exacts du coffre de production
(`N=262144, r=8, p=1`) :

```
1 essai = 0,5164 s  et  256 Mio de mémoire     ->  1,94 essai/s/cœur
```

La mémoire est le vrai verrou. Chaque essai **simultané** immobilise 256 Mio, ce qui rend
les cartes graphiques et les circuits dédiés (conçus pour `N=1024, r=1`, soit 2000 fois
moins de mémoire) inutilisables ici :

| Cadence visée | Il faut… |
|---|---|
| 10⁴ essais/s | ~5 200 cœurs et 2 Tio de RAM en vol |
| 10⁶ essais/s | ~516 000 cœurs et 244 Tio de RAM en vol |
| 10⁹ essais/s | ~5×10⁸ cœurs et **238 Pio** de RAM en vol — hors d'atteinte |

### 7.2 Mot de passe perdu, coffre intact

Le fichier de production contient **44 caractères**. Leur répartition mélange les quatre
classes (minuscules, majuscules, chiffres, signes) dans des proportions cohérentes avec un
tirage aléatoire — le détail n'est pas reproduit ici, conformément au § 3.3 : dans un dépôt
public, décrire finement la composition d'un mot de passe revient à offrir un filtre à qui
voudrait le deviner. Le procédé exact de génération n'a pas pu être retrouvé (**non
vérifié** : aucune trace dans `40-validator.sh`, et le fichier est antérieur à l'écriture
des scripts de déploiement).

Temps pour parcourir la moitié de l'espace :

| Ce qui reste inconnu | 1 Mac (10 cœurs) | ~5 000 cœurs | ~500 000 cœurs + 244 Tio |
|---|---|---|---|
| **4** caractères sur 44 | 5 jours | 14 min | 8 s |
| **5** caractères sur 44 | 320 jours | 15 h | 9 min |
| **6** caractères sur 44 | 56 ans | 40 jours | 9,5 h |
| tout (44 car. aléatoires) | 2×10⁷⁰ ans | 5×10⁶⁷ ans | 5×10⁶⁵ ans |

**Conclusion :** la force brute n'a de sens que si l'on se souvient **de presque tout**.
Quatre à six caractères d'incertitude, c'est récupérable. Un mot de passe entièrement
oublié ne se retrouve pas — même à 80 bits d'entropie, il faudrait 10¹⁰ ans avec
500 000 cœurs. Il reste **une seule chance réelle** : la seconde copie du fichier,
`/root/coinbosa-secrets/validator-password.txt`, qui est aujourd'hui **identique** à
`pw.txt` (empreintes comparées, sans divulgation).

### 7.3 Coffre perdu, mot de passe intact

Il n'y a rien à attaquer. Le mot de passe seul ne contient aucune information sur la clé :
il ne sert qu'à déchiffrer un fichier qui n'existe plus. Retrouver la clé signifierait
deviner un scalaire secp256k1 de 256 bits — **1,8×10⁶⁰ années** à un milliard d'essais par
seconde, et sans même le coût de scrypt.

**Aucun budget, aucune durée, aucun matériel ne la retrouve.** Et comme aucune transaction
ne peut plus être minée (§ 0), la gouvernance on-chain ne peut pas non plus désigner un
autre validateur. Il ne resterait qu'un nouveau genesis — c'est-à-dire une autre chaîne,
et pour une place d'échange, un autre actif.

### 7.4 Ce que ça impose

> C'est **le coffre**, pas le mot de passe, qu'il faut sauvegarder en premier.
> Aujourd'hui, c'est l'inverse qui est fait : le mot de passe est en double, le coffre en
> exemplaire unique, et les deux sur le même disque.

---

## 8. Ce que ce document ne couvre pas

* **La clé du gouverneur** (`0x1EEf3830833d83AcD3152A511853fd04a0b4082A`, propriétaire du
  contrat `0x…1000`). Elle ne produit pas de blocs, mais elle seule peut faire tourner
  l'ensemble des validateurs. Elle mérite sa propre procédure, avec la même règle A/B.
* **Les clés des 13 adresses de distribution** (700 000 000 BOSA). Hors périmètre ici.
* **Le passage à plusieurs validateurs**, qui supprimerait le point unique de défaillance
  décrit au § 0 — mais qui, mal fait, **arrête la chaîne** : lire `scripts/rotate-validators.js`
  en entier avant d'y toucher (à N=2 comme à N=3, il faut 2 scelleurs distincts et en ligne).
* **`nodekey-validator`** : c'est l'identité *réseau*, elle se régénère. Elle est déjà dans
  `coinbosa-froid.tgz` et ne fait pas partie des pièces critiques.

---

## 9. Un écart à connaître avant de reconstruire

Au 2026-08-31, trois scripts de déploiement **diffèrent** entre le serveur et la branche
`coinbosa-genesis-bos20` (comparaison par `sha256sum` contre `HEAD`, pas contre l'arbre de
travail) :

| Script | Serveur `/root/` | Dépôt (`HEAD`) | Écart |
|---|---|---|---|
| `10-web.sh` | 10 185 o | 12 627 o | 49 lignes |
| `50-monitoring.sh` | 12 998 o | 15 002 o | 59 lignes |
| `60-journal.sh` | 6 803 o | 8 160 o | 24 lignes |

Le dépôt est **en avance** (redirection `www`, fenêtre de maintenance, témoin de
maintenance). Les quatre autres — dont **`30-node.sh` et `40-validator.sh`**, les seuls qui
comptent pour le validateur — sont **identiques au bit près**.

Conséquence : reconstruire depuis le dépôt ne redonne pas le serveur d'aujourd'hui à
l'identique, mais une version plus récente du tier web et de la supervision. Ce n'est pas
un problème pour la clé ; c'en est un pour qui croirait restaurer « exactement pareil ».
