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

# racine du dossier coinbosa/ (parent de deploy/)
BASE="$(cd "$(dirname "$0")/.." && pwd)"

for f in site/index.html explorer/index.html whitepaper/index.html; do
  if [ ! -f "$BASE/$f" ]; then
    echo "Introuvable : $BASE/$f" >&2
    exit 1
  fi
done

echo "==> Envoi des fichiers vers $SERVER"
rsync -avz --chmod=D755,F644 "$BASE/site/index.html"       "$SERVER:/var/www/coinbosa/site/index.html"
rsync -avz --chmod=D755,F644 "$BASE/explorer/index.html"   "$SERVER:/var/www/coinbosa/explorer/index.html"
rsync -avz --chmod=D755,F644 "$BASE/whitepaper/index.html" "$SERVER:/var/www/coinbosa/whitepaper/index.html"

echo "==> Droits + rechargement de Caddy"
ssh "$SERVER" 'chown -R caddy:caddy /var/www/coinbosa && systemctl reload caddy'

echo "==> Publié."
echo "    Site       : https://coinbosa.com"
echo "    Livre blanc: https://coinbosa.com/whitepaper/"
echo "    Explorateur: https://explorer.coinbosa.com"
