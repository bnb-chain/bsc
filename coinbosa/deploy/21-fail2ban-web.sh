#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — prison fail2ban pour le tier web (Caddy).
#
#   Bannit les IP qui martèlent le site, l'explorateur ou le relais /rpc :
#   balayage de chemins (4xx en rafale) et abus du JSON-RPC.
#
# À lancer en root SUR le VPS, APRÈS 10-web.sh (qui crée les journaux) :
#
#   sudo bash 21-fail2ban-web.sh
#
# Prérequis : /var/log/caddy/*.log alimentés au format json par 10-web.sh.
# ---------------------------------------------------------------------------
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "à lancer en root (sudo bash 21-fail2ban-web.sh)" >&2; exit 1; }

export DEBIAN_FRONTEND=noninteractive
command -v fail2ban-server >/dev/null 2>&1 || { apt-get update -y; apt-get install -y fail2ban; }

echo "==> Filtre : réponses 4xx en rafale dans le journal JSON de Caddy"
# Caddy écrit une ligne JSON par requête. On capte l'IP source et le statut.
cat > /etc/fail2ban/filter.d/caddy-status.conf <<'CONF'
[Definition]
# Journal JSON de Caddy : {"level":"info",...,"request":{"remote_ip":"1.2.3.4",...},"status":404,...}
failregex = ^.*"remote_ip":"<HOST>".*"status":(?:400|401|403|404|405|429)\b.*$
ignoreregex =
datepattern = "ts":{EPOCH}
CONF

echo "==> Filtre : abus du relais JSON-RPC (/rpc)"
cat > /etc/fail2ban/filter.d/caddy-rpc.conf <<'CONF'
[Definition]
# Toute requête vers /rpc. Le seuil de la prison fait le tri : un explorateur
# légitime interroge quelques fois par 5 s, pas des centaines de fois par minute.
failregex = ^.*"remote_ip":"<HOST>".*"uri":"/rpc(?:\?[^"]*)?".*$
ignoreregex =
datepattern = "ts":{EPOCH}
CONF

echo "==> Prisons"
# L'IP de l'opérateur est mise en liste blanche pour ne jamais se bannir soi-même.
OP_IP=""
if [ -n "${SSH_CONNECTION:-}" ]; then OP_IP=$(echo "$SSH_CONNECTION" | awk '{print $1}'); fi
IGNORE="127.0.0.1/8 ::1"
[ -n "$OP_IP" ] && IGNORE="$IGNORE $OP_IP"
echo "    liste blanche : $IGNORE"

cat > /etc/fail2ban/jail.d/coinbosa-web.conf <<CONF
# Coinbosa — prisons du tier web. Généré par deploy/21-fail2ban-web.sh
[caddy-status]
enabled  = true
filter   = caddy-status
logpath  = /var/log/caddy/site-access.log
           /var/log/caddy/explorer-access.log
backend  = auto
port     = http,https
maxretry = 60
findtime = 60
bantime  = 1h
ignoreip = $IGNORE

[caddy-rpc]
enabled  = true
filter   = caddy-rpc
logpath  = /var/log/caddy/explorer-access.log
backend  = auto
port     = http,https
# Limite de débit effective du relais JSON-RPC : au-delà de 240 requêtes/minute
# depuis une même IP, on bannit. L'explorateur en fait ~10 toutes les 5 s.
maxretry = 240
findtime = 60
bantime  = 30m
ignoreip = $IGNORE
CONF

echo "==> Vérification des filtres avant activation"
# On refuse d'activer un filtre qui ne compile pas (sinon la prison est inerte
# et on croirait le serveur protégé alors qu'il ne l'est pas).
for f in caddy-status caddy-rpc; do
  fail2ban-regex --help >/dev/null 2>&1 || break
  if ! fail2ban-regex /dev/null "/etc/fail2ban/filter.d/$f.conf" >/dev/null 2>&1; then
    echo "ARRÊT : le filtre $f ne compile pas — prisons non activées." >&2
    rm -f /etc/fail2ban/jail.d/coinbosa-web.conf
    exit 1
  fi
  echo "    filtre $f : OK"
done

systemctl enable fail2ban >/dev/null 2>&1 || true
systemctl restart fail2ban

echo ""
echo "==> Prisons web actives. État :"
sleep 2
fail2ban-client status 2>/dev/null || true
echo ""
echo "    Détail d'une prison :  fail2ban-client status caddy-rpc"
echo "    Débannir une IP     :  fail2ban-client set caddy-rpc unbanip <IP>"
