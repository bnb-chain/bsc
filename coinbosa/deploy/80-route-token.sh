#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — servir /token/<adresse> dans l'explorateur.
#
#   sudo bash 80-route-token.sh            # applique
#   sudo ANNULER=1 bash 80-route-token.sh  # revient en arrière
#
# POURQUOI
# --------
# Le formulaire d'intégration de Bite Fast porte un champ « Token Endpoint »
# distinct du champ « Address Endpoint ». L'explorateur ne servait que
# /tx, /block et /address : `curl https://explorer.coinbosa.com/token/0x…`
# rendait **404** — mesuré le 2026-09-05.
#
# Leur donner une URL qui rend 404 ferait échouer leur intégration sans rien
# expliquer : le champ serait rempli, la page vide, et personne ne saurait
# pourquoi. Mieux vaut une route qui fonctionne.
#
# CE QU'ELLE MONTRE
# -----------------
# La même vue que /address, et c'est correct : un jeton BRC20 EST un contrat.
# La fiche d'adresse en montre le code, les soldes et les transferts. On
# n'invente pas une page « jeton » qui dirait autre chose que la chaîne.
#
# DEUX ENDROITS, ET IL FAUT LES DEUX
# ----------------------------------
# Caddy décide si l'URL est servie ; l'application décide quoi en faire. Ne
# changer que l'un des deux donne soit un 404 (Caddy refuse), soit une page
# vide (l'application ne reconnaît pas le chemin). Ce script fait Caddy ;
# explorer/app.js est modifié dans le dépôt et part par publish-static.sh.
# ---------------------------------------------------------------------------
set -euo pipefail

CADDYFILE=/etc/caddy/Caddyfile
ANCIEN='^/(tx|block|blocs?|address|adresse)/[^/]+/?$'
NOUVEAU='^/(tx|block|blocs?|address|adresse|token|jeton)/[^/]+/?$'
ANNULER="${ANNULER:-0}"

ok() { printf '    \033[32mOK\033[0m    %s\n' "$1"; }
ko() { printf '    \033[31mECHEC\033[0m %s\n' "$1"; exit 1; }
[ "$(id -u)" = 0 ] || { echo "À lancer en root (sudo)." >&2; exit 1; }

if [ "$ANNULER" = 1 ]; then
  grep -qF "$NOUVEAU" "$CADDYFILE" || { echo "==> Déjà dans l'état d'origine."; exit 0; }
  cp -a "$CADDYFILE" "$CADDYFILE.avant-token-annul-$(date +%F-%H%M)"
  python3 - "$CADDYFILE" "$NOUVEAU" "$ANCIEN" <<'PY'
import pathlib, sys
p = pathlib.Path(sys.argv[1]); s = p.read_text()
p.write_text(s.replace(sys.argv[2], sys.argv[3], 1))
PY
  caddy validate --config "$CADDYFILE" >/dev/null 2>&1 || ko "Caddyfile invalide après annulation"
  systemctl reload caddy
  echo "==> Route /token retirée."
  exit 0
fi

echo "==> État actuel"
if grep -qF "$NOUVEAU" "$CADDYFILE"; then
  ok "la route est déjà en place"
else
  grep -qF "$ANCIEN" "$CADDYFILE" || ko "motif de routage introuvable — le Caddyfile a changé, inspecter à la main"
  ok "motif de routage trouvé, une seule occurrence : $(grep -cF "$ANCIEN" "$CADDYFILE")"

  cp -a "$CADDYFILE" "$CADDYFILE.avant-token-$(date +%F-%H%M)"
  python3 - "$CADDYFILE" "$ANCIEN" "$NOUVEAU" <<'PY'
import pathlib, sys
p = pathlib.Path(sys.argv[1]); s = p.read_text()
assert s.count(sys.argv[2]) == 1, "motif absent ou multiple"
p.write_text(s.replace(sys.argv[2], sys.argv[3], 1))
print("    motif de routage étendu")
PY

  # Ce qui protège le validateur ne doit pas disparaître en passant.
  grep -q 'max_conns_per_host 24' "$CADDYFILE" \
    && ok "borne de simultanéité (24) intacte" \
    || ko "la borne a disparu — restaurer la sauvegarde, NE PAS recharger"

  caddy validate --config "$CADDYFILE" >/dev/null 2>&1 \
    && ok "Caddyfile valide" || ko "Caddyfile invalide — restaurer la sauvegarde"

  systemctl reload caddy
  sleep 2
  ok "Caddy rechargé"
fi

echo
echo "==> Épreuve depuis l'extérieur"
A=0xCa6f08e549290BbF161fF45c475fd3f7A6e65f04
for chemin in "/token/$A" "/address/$A" "/tx/0x0000000000000000000000000000000000000000000000000000000000000000" "/block/1"; do
  c=$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 "https://explorer.coinbosa.com$chemin")
  [ "$c" = 200 ] && ok "$chemin → 200" || ko "$chemin → $c"
done
# Une route trop large servirait n'importe quoi : on vérifie qu'elle reste bornée.
c=$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 "https://explorer.coinbosa.com/nimporte/$A")
[ "$c" = 200 ] && ko "/nimporte/… rend 200 : le motif est trop large" || ok "/nimporte/… → $c, la route reste bornée"

echo
echo "==> Retour arrière : sudo ANNULER=1 bash 80-route-token.sh"
