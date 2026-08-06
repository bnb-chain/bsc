#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — pousse les fichiers du tier public vers le VPS.
# À lancer DEPUIS TON POSTE (là où se trouve le dépôt), pas sur le VPS :
#
#   SERVER=root@203.0.113.10 bash publish-static.sh
#
# Relance-le à chaque mise à jour du site / explorateur / livre blanc.
# ---------------------------------------------------------------------------
set -euo pipefail

: "${SERVER:?Définis SERVER, ex: export SERVER=root@<ip>}"

# Si tu te connectes en utilisateur sudo (non root), lance avec SUDO=sudo :
#   SERVER=deploy@<ip> SUDO=sudo bash publish-static.sh
SUDO="${SUDO:-}"
RSYNC_PATH="${SUDO:+sudo }rsync"

# racine du dossier coinbosa/ (parent de deploy/)
BASE="$(cd "$(dirname "$0")/.." && pwd)"

# Le JavaScript vit dans des fichiers .js séparés (la CSP interdit le script en ligne).
# Si un app.js n'est pas publié, la page se charge SANS son script : contrôle bloquant.
for f in site/index.html site/app.js \
         explorer/index.html explorer/app.js \
         whitepaper/index.html whitepaper/app.js; do
  if [ ! -f "$BASE/$f" ]; then
    echo "Introuvable : $BASE/$f" >&2
    exit 1
  fi
done

echo "==> Envoi des fichiers vers $SERVER"
rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/site/index.html"       "$SERVER:/var/www/coinbosa/site/index.html"
rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/site/app.js"           "$SERVER:/var/www/coinbosa/site/app.js"
rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/explorer/index.html"   "$SERVER:/var/www/coinbosa/explorer/index.html"
rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/explorer/app.js"       "$SERVER:/var/www/coinbosa/explorer/app.js"
rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/whitepaper/index.html" "$SERVER:/var/www/coinbosa/whitepaper/index.html"
rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/whitepaper/app.js"     "$SERVER:/var/www/coinbosa/whitepaper/app.js"

# Favicons — générés depuis le LOGO OFFICIEL (assets/coinbosa-logo.jpg), jamais un dessin.
# Régénération si besoin :
#   sips -s format png -z 32 32   assets/coinbosa-logo.jpg --out deploy/static/favicon-32.png
#   sips -s format png -z 180 180 assets/coinbosa-logo.jpg --out deploy/static/apple-touch-icon.png
for d in site explorer whitepaper; do
  rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/deploy/static/favicon-32.png"       "$SERVER:/var/www/coinbosa/$d/favicon-32.png"
  rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/deploy/static/apple-touch-icon.png" "$SERVER:/var/www/coinbosa/$d/apple-touch-icon.png"
done

# Assets SEO / partage servis à la racine
rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/assets/coinbosa-logo.jpg" "$SERVER:/var/www/coinbosa/site/og-image.jpg"
rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/deploy/static/robots.txt"  "$SERVER:/var/www/coinbosa/site/robots.txt"
rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/deploy/static/sitemap.xml" "$SERVER:/var/www/coinbosa/site/sitemap.xml"
rsync -avz --rsync-path="$RSYNC_PATH" "$BASE/deploy/static/robots.txt"  "$SERVER:/var/www/coinbosa/explorer/robots.txt"

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
