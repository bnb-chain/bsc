#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — pousse les fichiers du tier public vers le VPS.
# À lancer DEPUIS TON POSTE (là où se trouve le dépôt), pas sur le VPS :
#
#   SERVER=root@203.0.113.10 bash publish-static.sh
#
# Relance-le à chaque mise à jour du site / explorateur / livre blanc.
#
# Le site fait cinq pages qui partagent une coque, une feuille de style et deux
# scripts. Publier page par page a déjà failli casser la production : une page
# neuve réclamait un app.js resté ancien. Ce script MIROITE donc les dossiers
# entiers, il ne choisit pas les fichiers un par un.
# ---------------------------------------------------------------------------
set -euo pipefail

: "${SERVER:?Définis SERVER, ex: export SERVER=root@<ip>}"

# Si tu te connectes en utilisateur sudo (non root), lance avec SUDO=sudo :
#   SERVER=deploy@<ip> SUDO=sudo bash publish-static.sh
SUDO="${SUDO:-}"
RSYNC_PATH="${SUDO:+sudo }rsync"

# racine du dossier coinbosa/ (parent de deploy/)
BASE="$(cd "$(dirname "$0")/.." && pwd)"

# ---------------------------------------------------------------------------
# Contrôle avant envoi. Le JavaScript vit dans des fichiers .js séparés (la CSP
# interdit le script en ligne) : un .js manquant charge la page SANS son script.
# ---------------------------------------------------------------------------
for f in site/index.html site/ecosysteme.html site/chaine.html \
         site/developpeurs.html site/a-propos.html \
         site/app.js site/assets/style.css site/assets/scene.js \
         site/robots.txt site/sitemap.xml site/version.json \
         explorer/index.html explorer/app.js \
         whitepaper/index.html whitepaper/app.js; do
  [ -f "$BASE/$f" ] || { echo "Introuvable : $BASE/$f" >&2; exit 1; }
done

# Les empreintes de cache inscrites dans les pages doivent correspondre aux
# fichiers réellement envoyés, sinon le visiteur reçoit une page neuve avec un
# style ancien. coque.py le vérifie ; on refuse de publier s'il proteste.
if command -v python3 >/dev/null 2>&1; then
  echo "==> Contrôle de la coque et des empreintes"
  ( cd "$BASE/site" && python3 coque.py --verifier ) || {
    echo "Le site est incohérent — publication refusée. Lance : python3 coque.py" >&2
    exit 1
  }
fi

# ---------------------------------------------------------------------------
# Envoi. --delete pour que la suppression d'une page ici la supprime là-bas.
# Les exclusions protègent ce qui vit sur le serveur sans venir de ce dossier :
#   .well-known/  security.txt, installé plus bas
#   og-image.jpg  copié depuis assets/coinbosa-logo.jpg, plus bas
#   favicon/apple copiés depuis deploy/static/, plus bas
#   coque.py      outil de construction : n'a rien à faire en ligne
# ---------------------------------------------------------------------------
COMMUN=(-avz --delete --rsync-path="$RSYNC_PATH"
        --exclude '.well-known/' --exclude 'og-image.jpg'
        --exclude 'favicon-32.png' --exclude 'apple-touch-icon.png'
        --exclude 'coque.py' --exclude '.DS_Store')

echo "==> Envoi des fichiers vers $SERVER"
rsync "${COMMUN[@]}" "$BASE/site/"       "$SERVER:/var/www/coinbosa/site/"
rsync "${COMMUN[@]}" "$BASE/explorer/"   "$SERVER:/var/www/coinbosa/explorer/"
rsync "${COMMUN[@]}" "$BASE/whitepaper/" "$SERVER:/var/www/coinbosa/whitepaper/"

# Favicons — générés depuis le LOGO OFFICIEL (assets/coinbosa-logo.jpg), jamais un dessin.
# Régénération si besoin :
#   sips -s format png -z 32 32   assets/coinbosa-logo.jpg --out deploy/static/favicon-32.png
#   sips -s format png -z 180 180 assets/coinbosa-logo.jpg --out deploy/static/apple-touch-icon.png
for d in site explorer whitepaper; do
  rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/deploy/static/favicon-32.png"       "$SERVER:/var/www/coinbosa/$d/favicon-32.png"
  rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/deploy/static/apple-touch-icon.png" "$SERVER:/var/www/coinbosa/$d/apple-touch-icon.png"
done

# Image de partage servie à la racine du site
rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/assets/coinbosa-logo.jpg" "$SERVER:/var/www/coinbosa/site/og-image.jpg"

# security.txt (RFC 9116) — il déclare le canal de signalement de faille. Il doit être
# servi à l'emplacement normalisé /.well-known/, sinon personne ne le trouve.
ssh "$SERVER" "${SUDO} install -d -o caddy -g caddy /var/www/coinbosa/site/.well-known /var/www/coinbosa/explorer/.well-known"
rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/deploy/static/security.txt" "$SERVER:/var/www/coinbosa/site/.well-known/security.txt"
rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/deploy/static/security.txt" "$SERVER:/var/www/coinbosa/explorer/.well-known/security.txt"

echo "==> Droits + rechargement de Caddy"
ssh "$SERVER" "${SUDO} chown -R caddy:caddy /var/www/coinbosa && ${SUDO} chmod -R u=rwX,go=rX /var/www/coinbosa && ${SUDO} systemctl reload caddy"

echo "==> Publié."
echo "    Site       : https://coinbosa.com"
echo "    Livre blanc: https://coinbosa.com/whitepaper/"
echo "    Explorateur: https://explorer.coinbosa.com"
