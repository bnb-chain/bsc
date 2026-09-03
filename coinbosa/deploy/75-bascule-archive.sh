#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — basculer /rpc vers le nœud ARCHIVE et ouvrir /ws.
#
#   sudo bash 75-bascule-archive.sh            # applique
#   sudo ANNULER=1 bash 75-bascule-archive.sh  # revient au nœud 8545
#
# POURQUOI CE SCRIPT EXISTE, ET POURQUOI IL N'EST PAS LE FRAGMENT
# ---------------------------------------------------------------
# 73-caddy-ws-archive.snippet donne le bloc Caddyfile « idéal » à insérer EN
# REMPLACEMENT du bloc @rpc_post. Mais le Caddyfile réellement en production
# contient, dans ce même bloc, une protection que le fragment ne connaît pas :
#
#     transport http { max_conns_per_host 24 ... }
#
# Elle borne le nombre de requêtes SIMULTANÉES atteignant geth. Elle a été
# dimensionnée sur 11 536 requêtes /rpc réelles — médiane 1,09 ms, p99,9
# 2,13 s, un facteur 2000 — précisément parce que le validateur partage les
# 4 cœurs de cette machine et que c'est par là qu'un abus arrête la production.
#
# Appliquer le fragment tel quel EFFACERAIT cette protection, sans que rien ne
# le signale : le relais continuerait de fonctionner, et la borne aurait disparu.
# Ce script fait donc une modification CHIRURGICALE — il change le port, relève
# la taille de corps, ajoute /ws, et ne touche à rien d'autre.
#
# CE QU'IL CHANGE, ET POURQUOI
# ----------------------------
#   8545 -> 8547   Le nœud public élague son état : « historical state ... is
#                  not available » sur toute lecture au bloc 0. Mesuré : une
#                  place d'échange qui vérifie l'allocation du genesis se heurte
#                  à ce mur. Le nœud d'archive, lui, rend 140 000 000 BOSA sur
#                  le poste « développement » au bloc 0.
#   32KB -> 512KB  32 Ko rejetait un lot d'appels un peu gros AVEC un corps
#                  « 413 » en texte brut, que le client JSON-RPC ne sait pas
#                  interpréter — l'intégrateur voit une erreur incompréhensible.
#   + /ws          eth_subscribe répondait « notifications not supported », et
#                  /ws rendait 404. Sans WebSocket, une intégration doit
#                  scruter, ce qui multiplie ses requêtes — et la prison
#                  fail2ban caddy-rpc bannit à 1500 requêtes/minute.
#
# PRÉALABLES, VÉRIFIÉS PAR LE SCRIPT AVANT DE TOUCHER À QUOI QUE CE SOIT
# ---------------------------------------------------------------------
#   1. le nœud d'archive répond et a rattrapé le nœud public (< 5 blocs) ;
#   2. il SERT l'état du bloc 0 — c'est tout l'objet de l'opération ;
#   3. son WebSocket répond 101 sur 8548 ;
#   4. le Caddyfile contient bien une seule cible 8545 et un seul max_size.
# Si l'un manque, le script s'arrête sans rien modifier.
# ---------------------------------------------------------------------------
set -euo pipefail

CADDYFILE=/etc/caddy/Caddyfile
ANNULER="${ANNULER:-0}"
POSTE_DEV=0xCa6f08e549290BbF161fF45c475fd3f7A6e65f04   # 20 % = 140 000 000 au bloc 0

rpc() { curl -s --max-time 10 -X POST -H 'content-type: application/json' -d "$2" "http://127.0.0.1:$1"; }
hex() { python3 -c "import sys;print(int(sys.argv[1],16))" "$1" 2>/dev/null || echo 0; }
ok()  { printf '    \033[32mOK\033[0m    %s\n' "$1"; }
ko()  { printf '    \033[31mECHEC\033[0m %s\n' "$1"; exit 1; }

[ "$(id -u)" = 0 ] || { echo "À lancer en root (sudo)." >&2; exit 1; }

# --- retour arrière --------------------------------------------------------
if [ "$ANNULER" = 1 ]; then
  echo "==> Retour au nœud 8545"
  python3 - "$CADDYFILE" <<'PY'
import pathlib, re, sys
p = pathlib.Path(sys.argv[1]); s = p.read_text()
s = s.replace("reverse_proxy 127.0.0.1:8547", "reverse_proxy 127.0.0.1:8545", 1)
s = s.replace("max_size 512KB", "max_size 32KB", 1)
# retire le bloc WebSocket ajouté, et lui seul
s = re.sub(r"    # --- WebSocket JSON-RPC.*?\n    handle /ws\* \{\n        respond \"method not allowed\" 405\n    \}\n",
           "", s, count=1, flags=re.S)
p.write_text(s)
print("    Caddyfile ramené à son état antérieur")
PY
  caddy validate --config "$CADDYFILE" >/dev/null 2>&1 || ko "Caddyfile invalide après annulation — NE PAS recharger"
  systemctl reload caddy
  echo "==> Revenu au nœud 8545. Le nœud d'archive tourne toujours (il n'est plus exposé)."
  exit 0
fi

# --- préalables ------------------------------------------------------------
echo "==> Préalables"
A=$(hex "$(rpc 8547 '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' | sed 's/.*result":"//;s/".*//')")
P=$(hex "$(rpc 8545 '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' | sed 's/.*result":"//;s/".*//')")
[ "$A" -gt 0 ] || ko "le nœud d'archive ne répond pas sur 8547"
D=$(( P > A ? P - A : A - P ))
[ "$D" -lt 5 ] && ok "archive $A, public $P — écart $D bloc(s)" || ko "écart de $D blocs : le rejeu n'est pas terminé"

R=$(rpc 8547 "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_getBalance\",\"params\":[\"$POSTE_DEV\",\"0x0\"]}")
case "$R" in
  *'"result"'*) V=$(hex "$(echo "$R" | sed 's/.*result":"//;s/".*//')")
                # 140 000 000 BOSA exactement. Un nœud d'archive qui répondrait
                # « 0 » à tout passerait un simple test « pas d'erreur » : on
                # exige donc la VALEUR, pas seulement l'absence d'erreur.
                [ "$V" = 140000000000000000000000000 ] \
                  && ok "état du bloc 0 servi — 140 000 000 BOSA sur le poste développement" \
                  || ko "état du bloc 0 servi mais valeur inattendue ($V)" ;;
  *) ko "l'état du bloc 0 n'est pas servi par 8547 : $(echo "$R" | cut -c1-90)" ;;
esac

C=$(curl -s -o /dev/null -w '%{http_code}' --max-time 8 -H 'Connection: Upgrade' \
    -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
    -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' http://127.0.0.1:8548/ 2>/dev/null || true)
[ "$C" = 101 ] && ok "WebSocket 8548 : handshake 101" || ko "WebSocket 8548 répond $C, attendu 101"

grep -c 'reverse_proxy 127.0.0.1:8545' "$CADDYFILE" | grep -qx 1 || ko "cible 8545 absente ou multiple dans le Caddyfile"
grep -c 'max_size 32KB' "$CADDYFILE" | grep -qx 1 || ko "max_size 32KB absent ou multiple"
grep -q 'path /ws' "$CADDYFILE" && ko "un bloc /ws existe déjà — inspecter à la main"
ok "Caddyfile dans l'état attendu"

# --- modification ----------------------------------------------------------
echo "==> Modification du Caddyfile"
cp -a "$CADDYFILE" "$CADDYFILE.avant-archive-$(date +%F-%H%M)"
python3 - "$CADDYFILE" <<'PY'
import pathlib, sys
p = pathlib.Path(sys.argv[1]); s = p.read_text()
s = s.replace("reverse_proxy 127.0.0.1:8545", "reverse_proxy 127.0.0.1:8547", 1)
s = s.replace("max_size 32KB", "max_size 512KB", 1)
ancre = "    handle /rpc* {"
assert s.count(ancre) == 1, "ancre handle /rpc* introuvable ou multiple"
ws = """    # --- WebSocket JSON-RPC ---------------------------------------------
    # Sans WebSocket, une integration doit scruter : ses requetes se multiplient
    # et la prison fail2ban caddy-rpc bannit a 1500 requetes/minute. geth sert le
    # WS a la racine de son ecouteur, d'ou la reecriture de /ws en /.
    @ws {
        path /ws
        header Connection *Upgrade*
        header Upgrade    websocket
    }
    handle @ws {
        rewrite * /
        reverse_proxy 127.0.0.1:8548 {
            header_up Host {upstream_hostport}
        }
    }
    handle /ws* {
        respond "method not allowed" 405
    }
"""
p.write_text(s.replace(ancre, ws + ancre, 1))
print("    /rpc -> 8547, corps 512 Ko, bloc /ws ajoute")
PY

# La borne de simultanéité ne doit PAS avoir disparu : c'est elle qui empêche
# un abus du RPC public d'arrêter la production de blocs.
grep -q 'max_conns_per_host 24' "$CADDYFILE" \
  && ok "borne de simultanéité (max_conns_per_host 24) préservée" \
  || ko "la borne de simultanéité a disparu — NE PAS recharger, restaurer la sauvegarde"

caddy validate --config "$CADDYFILE" >/dev/null 2>&1 \
  && ok "Caddyfile valide" || ko "Caddyfile invalide — restaurer la sauvegarde, ne pas recharger"

echo "==> Rechargement de Caddy"
systemctl reload caddy
sleep 3
ok "Caddy rechargé"

echo
echo "==> Vérification depuis l'extérieur (à faire aussi depuis un autre réseau)"
echo "    curl -s -X POST -H 'content-type: application/json' \\"
echo "      --data '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_getBalance\",\"params\":[\"$POSTE_DEV\",\"0x0\"]}' \\"
echo "      https://explorer.coinbosa.com/rpc"
echo "    -> doit rendre un result, plus « historical state ... is not available »"
echo
echo "    Retour arrière : sudo ANNULER=1 bash 75-bascule-archive.sh"
