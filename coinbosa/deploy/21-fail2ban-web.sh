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
# Seuil calibré sur l'usage RÉEL de l'explorateur, pas sur une intuition.
# Un visiteur consomme ~21 appels JSON-RPC toutes les 5 s (hauteur, prix du gas, les
# derniers blocs, la liste des validateurs), soit ~252 requetes/minute. Un seuil a 240
# bannissait donc CHAQUE visiteur au bout d'une minute de navigation — un deni de
# service contre nos propres utilisateurs, invisible tant que personne ne reste.
# 1500/min laisse passer plusieurs onglets ouverts par la meme IP (bureau partage,
# NAT d'entreprise) tout en arretant un flood reel, qui se compte en milliers.
maxretry = 1500
findtime = 60
bantime  = 30m
ignoreip = $IGNORE
CONF

echo "==> Vérification des filtres sur de VRAIES lignes de journal"
# Un filtre qui compile peut très bien ne rien reconnaître : la prison serait alors
# inerte et on croirait le serveur protégé alors qu'il ne l'est pas. On exige donc
# qu'il TROUVE l'adresse dans un échantillon représentatif du journal de Caddy.
SAMPLE=$(mktemp)
cat > "$SAMPLE" <<'SAMPLEEOF'
{"level":"info","ts":1785000000.123,"logger":"http.log.access","msg":"handled request","request":{"remote_ip":"203.0.113.77","remote_port":"51234","proto":"HTTP/2.0","method":"GET","host":"explorer.coinbosa.com","uri":"/wp-login.php","headers":{}},"duration":0.001,"size":0,"status":404,"resp_headers":{}}
{"level":"info","ts":1785000001.456,"logger":"http.log.access","msg":"handled request","request":{"remote_ip":"203.0.113.77","remote_port":"51235","proto":"HTTP/2.0","method":"POST","host":"explorer.coinbosa.com","uri":"/rpc","headers":{}},"duration":0.004,"size":88,"status":200,"resp_headers":{}}
SAMPLEEOF

verifier_filtre() {
  local nom="$1" attendu="$2"
  local sortie n
  sortie=$(fail2ban-regex "$SAMPLE" "/etc/fail2ban/filter.d/${nom}.conf" 2>/dev/null || true)
  # fail2ban-regex affiche « Lines: N lines, ... M matched »
  n=$(printf '%s' "$sortie" | grep -oE '[0-9]+ matched' | head -1 | grep -oE '^[0-9]+' || echo 0)
  if [ "${n:-0}" -lt "$attendu" ]; then
    echo "ARRÊT : le filtre $nom ne reconnaît pas le journal de Caddy ($n correspondance(s), $attendu attendue(s))." >&2
    echo "        Une prison inerte donnerait une fausse impression de protection." >&2
    printf '%s\n' "$sortie" | tail -20 >&2
    rm -f /etc/fail2ban/jail.d/coinbosa-web.conf "$SAMPLE"
    exit 1
  fi
  echo "    filtre $nom : $n correspondance(s) sur l'échantillon — OK"
}

if command -v fail2ban-regex >/dev/null 2>&1; then
  verifier_filtre caddy-status 1   # la ligne 404
  verifier_filtre caddy-rpc    1   # la ligne /rpc
else
  echo "ARRÊT : fail2ban-regex introuvable, impossible de PROUVER que les filtres marchent." >&2
  rm -f /etc/fail2ban/jail.d/coinbosa-web.conf "$SAMPLE"
  exit 1
fi
rm -f "$SAMPLE"

systemctl enable fail2ban >/dev/null 2>&1 || true
systemctl restart fail2ban

echo ""
echo "==> Prisons web actives. État :"
sleep 2
fail2ban-client status 2>/dev/null || true
echo ""
echo "    Détail d'une prison :  fail2ban-client status caddy-rpc"
echo "    Débannir une IP     :  fail2ban-client set caddy-rpc unbanip <IP>"
