<div align="center">
  <img src="../assets/coinbosa-logo.jpg" alt="Coinbosa" width="90" />

  # Déploiement — tier public
</div>

Ce dossier déploie le **tier public** de Coinbosa sur **un** VPS Ubuntu :

- `coinbosa.com` (+ `www`) → le site vitrine
- `coinbosa.com/whitepaper/` → le livre blanc
- `explorer.coinbosa.com` → l'explorateur

Le reverse-proxy est **Caddy**, qui obtient et **renouvelle les certificats TLS
automatiquement** (Let's Encrypt). Aucune intervention pour les certificats.

> **Hors périmètre ici, volontairement :** le **validateur** porteur de valeur, le **nœud**
> RPC de la chaîne, et **Coinbosa Card** (fonds clients). Ce sont des périmètres séparés,
> à déployer plus tard sur leur propre machine — voir
> [`docs/SECURITY-HARDENING.md`](../docs/SECURITY-HARDENING.md).

---

## Ce dont j'ai besoin de toi avant de lancer

1. **L'IP du VPS** et un **accès SSH** (root ou un utilisateur `sudo`). Concrètement :
   ajoute la clé publique SSH de ce poste à `~/.ssh/authorized_keys` du serveur, ou
   donne-moi un moyen d'entrer. Je vérifie avec `ssh <user>@<ip> "echo ok"`.
2. **Le DNS pointé** vers l'IP du VPS — c'est **obligatoire avant** que Caddy puisse
   émettre les certificats. Trois enregistrements de type **A** (et `AAAA` si IPv6) :

   | Nom | Type | Valeur |
   |---|---|---|
   | `coinbosa.com` | A | `<IP du VPS>` |
   | `www.coinbosa.com` | A | `<IP du VPS>` |
   | `explorer.coinbosa.com` | A | `<IP du VPS>` |

   La propagation DNS peut prendre de quelques minutes à quelques heures.

**Ne me communique jamais** de clé privée, de mot de passe en clair, ni de phrase de
récupération dans le chat. Un accès par clé SSH suffit.

---

## Ordre d'exécution (ce que je lancerai sur le VPS)

```bash
# 0. Depuis ce poste — vérifier l'accès
ssh <user>@<ip> "echo ok"

# 1. Sur le VPS — durcissement de base (pare-feu, fail2ban, MAJ auto)
sudo bash 00-bootstrap.sh

# 2. Sur le VPS — installer Caddy + arborescence web + config
sudo SITE_DOMAIN=coinbosa.com EXPLORER_DOMAIN=explorer.coinbosa.com bash 10-web.sh

# 3. Depuis ce poste — pousser les fichiers du site/explorateur/livre blanc
SERVER=<user>@<ip> bash publish-static.sh

# 4. Sur le VPS — prisons fail2ban du tier web (après 10-web.sh, qui crée les journaux)
sudo bash 21-fail2ban-web.sh

# 5. Sur le VPS — durcissement SSH, une fois l'accès par clé confirmé (voir plus bas)
sudo bash 20-ssh-hardening.sh
```

À l'étape 2, Caddy tente d'émettre les certificats dès qu'il démarre : **le DNS doit
déjà pointer** vers le VPS, sinon l'émission échoue (elle sera réessayée
automatiquement une fois le DNS en place).

### ⚠ Mise à jour d'un site DÉJÀ en ligne : publier AVANT de resserrer

La politique de sécurité du contenu (CSP) interdit désormais le JavaScript en ligne
(`script-src 'self'`), et tout le JavaScript vit dans des fichiers `app.js`.

Sur un serveur déjà en service, l'ordre compte :

1. **`publish-static.sh` d'abord** — les nouvelles pages et leurs `app.js`. Elles
   fonctionnent parfaitement sous l'ancienne CSP, qui autorisait déjà `'self'`.
2. **`10-web.sh` ensuite** — resserre la CSP.

Dans l'autre sens, entre les deux commandes, les anciennes pages (script en ligne) se
retrouveraient bloquées par la nouvelle CSP : site muet le temps de la bascule.

---

## Ce que chaque script fait

- **`00-bootstrap.sh`** — met à jour le système ; installe et **active le pare-feu UFW en
  autorisant SSH *avant* de l'activer** (pas de lock-out) ; ouvre 80/443 ; installe
  `fail2ban` et les mises à jour de sécurité automatiques. Il **ne touche pas** à la
  configuration SSH (aucun risque de te verrouiller).
- **`10-web.sh`** — installe Caddy depuis son dépôt officiel, crée
  `/var/www/coinbosa/{site,explorer,whitepaper}`, écrit `/etc/caddy/Caddyfile` à partir du
  gabarit et des domaines fournis, recharge Caddy.
- **`publish-static.sh`** — copie (rsync) les trois pages HTML, **leurs `app.js`**, les
  favicons, les fichiers SEO et `security.txt` (dans `/.well-known/`), puis recharge Caddy.
  Il **s'arrête** si un `app.js` manque : une page publiée sans son script serait morte.
  À relancer à chaque mise à jour du front.
- **`21-fail2ban-web.sh`** — prisons fail2ban pour le web : rafales de 4xx (balayage de
  chemins) et abus du relais `/rpc`. Les filtres sont confrontés à un échantillon réel du
  journal de Caddy et le script **refuse de s'activer** s'ils ne reconnaissent rien — une
  prison inerte donnerait une fausse impression de protection.
- **`20-ssh-hardening.sh`** — coupe l'authentification par mot de passe (voir plus bas).

---

## Après le déploiement

- L'explorateur **n'invente aucune donnée**. Tant qu'aucun nœud ne répond, il affiche un
  avis sobre (« aucun nœud public raccordé ») et des listes vides. Il devient vivant dès
  qu'un nœud écoute en local : Caddy relaie déjà `https://explorer.coinbosa.com/rpc` vers
  `127.0.0.1:8545`, en POST uniquement, sans jamais exposer le port 8545.
- Option : faire pointer le lien « livre blanc » du site vers `https://coinbosa.com/whitepaper/`
  au lieu de GitHub — **une seule ligne** dans l'objet `CONTENT.links` du site (`whitepaper`).

---

## Durcissement SSH — `20-ssh-hardening.sh`

Par défaut, un VPS Ubuntu accepte le mot de passe et le login root : n'importe qui peut
tenter sa chance en continu. Ce script coupe les deux, l'accès ne se faisant plus que par
clé.

**Il est conçu pour qu'il soit impossible de se verrouiller dehors.** Il refuse d'agir si
aucune clé n'est installée ; il exige la **preuve**, dans le journal, que la session en
cours est bien authentifiée par clé (et non par mot de passe) ; il vérifie que
`sshd_config` inclut réellement `sshd_config.d/` — sans quoi le durcissement serait ignoré
en silence ; il contrôle la configuration **effective** avec `sshd -T` plutôt que le
fichier écrit ; il recharge sans jamais redémarrer, donc les sessions ouvertes survivent.

Et surtout, il **arme un retour arrière automatique** : sans confirmation de ta part, la
configuration d'origine revient toute seule.

```bash
# à lancer DEPUIS une session SSH par clé (c'est elle qui sert de preuve)
sudo bash 20-ssh-hardening.sh
```

Ensuite, **sans fermer la session en cours** :

1. ouvrir une **deuxième** fenêtre et se connecter : `ssh root@<ip>` ;
2. si ça marche, confirmer **depuis cette nouvelle session** :

```bash
sudo touch /run/coinbosa-ssh-confirmed && sudo systemctl stop coinbosa-ssh-rollback.timer
```

Sans cette confirmation, au bout de 15 minutes (`GRACE_MIN` pour changer le délai) le
durcissement est annulé et l'accès par mot de passe revient. Une sauvegarde de la
configuration est écrite dans `/root/coinbosa-sshd-backup-*.tar`.
