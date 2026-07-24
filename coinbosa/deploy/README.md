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
```

À l'étape 2, Caddy tente d'émettre les certificats dès qu'il démarre : **le DNS doit
déjà pointer** vers le VPS, sinon l'émission échoue (elle sera réessayée
automatiquement une fois le DNS en place).

---

## Ce que chaque script fait

- **`00-bootstrap.sh`** — met à jour le système ; installe et **active le pare-feu UFW en
  autorisant SSH *avant* de l'activer** (pas de lock-out) ; ouvre 80/443 ; installe
  `fail2ban` et les mises à jour de sécurité automatiques. Il **ne touche pas** à la
  configuration SSH (aucun risque de te verrouiller).
- **`10-web.sh`** — installe Caddy depuis son dépôt officiel, crée
  `/var/www/coinbosa/{site,explorer,whitepaper}`, écrit `/etc/caddy/Caddyfile` à partir du
  gabarit et des domaines fournis, recharge Caddy.
- **`publish-static.sh`** — copie (rsync) les trois fichiers HTML autonomes du dépôt vers
  le serveur, puis recharge Caddy. À relancer à chaque mise à jour du front.

---

## Après le déploiement

- L'explorateur, sans nœud raccordé, affiche ses **données de démonstration** : il
  interroge un RPC qui n'existe pas encore. Il deviendra « live » quand on déploiera un
  nœud (périmètre séparé) et qu'on pointera son RPC dessus.
- Option : faire pointer le lien « livre blanc » du site vers `https://coinbosa.com/whitepaper/`
  au lieu de GitHub — **une seule ligne** dans l'objet `CONTENT.links` du site (`whitepaper`).

---

## Durcissement SSH (optionnel, à faire *après* avoir confirmé l'accès par clé)

Non automatisé exprès, pour ne pas risquer de te verrouiller. Une fois que la connexion
par clé fonctionne pour toi **et** pour ce poste, on pourra désactiver l'authentification
par mot de passe :

```bash
# /etc/ssh/sshd_config.d/10-hardening.conf
PasswordAuthentication no
PermitRootLogin prohibit-password
# puis : sudo systemctl reload ssh
```
