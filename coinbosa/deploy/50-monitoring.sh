#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — surveillance de la chaîne et alertes.
#
#   sudo SENTRY_DSN='https://…@…ingest.sentry.io/…' bash 50-monitoring.sh
#   sudo bash 50-monitoring.sh          (sans DSN : alertes en journal seulement)
#
# ---------------------------------------------------------------------------
# Pourquoi
# --------
# Une chaîne qui s'arrête ne le dit à personne. Les services restent « actifs », le RPC
# répond, l'explorateur affiche une hauteur — simplement figée. C'est exactement ce qui
# s'est produit après le premier redémarrage : le nœud servait une chaîne arrêtée sans
# qu'aucune alerte ne parte.
#
# Ce dispositif vérifie ce qui compte VRAIMENT et alerte quand ça casse :
#   · la hauteur AVANCE (le seul test qui prouve que la chaîne est vivante)
#   · les deux nœuds sont synchronisés entre eux
#   · les services tournent
#   · le disque ne sature pas (5 s/bloc, ça grossit tous les jours)
#   · le certificat TLS n'expire pas
#
# Les alertes partent vers Sentry par simple requête HTTP (aucun SDK, donc rien à
# installer et aucune dépendance à maintenir), et sont toujours écrites dans le journal
# système même sans DSN.
# ---------------------------------------------------------------------------
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "à lancer en root (sudo bash 50-monitoring.sh)" >&2; exit 1; }

REPO="${REPO:-/opt/coinbosa-chain}"
GETH="$REPO/build/bin/geth"
NODE_IPC="${NODE_IPC:-/var/lib/coinbosa/node/geth.ipc}"
VAL_IPC="${VAL_IPC:-/var/lib/coinbosa/validator/geth.ipc}"
NODE_USER="${NODE_USER:-coinbosa}"
VAL_USER="${VAL_USER:-coinbosa-val}"
DOMAINE="${DOMAINE:-explorer.coinbosa.com}"
DISQUE_SEUIL="${DISQUE_SEUIL:-85}"     # % d'occupation avant alerte
CERT_JOURS="${CERT_JOURS:-21}"          # alerte si le certificat expire dans moins de N jours
DSN="${SENTRY_DSN:-}"

install -d -m 0750 /var/lib/coinbosa-monitoring
[ -n "$DSN" ] && { printf '%s' "$DSN" > /etc/coinbosa-sentry-dsn; chmod 0600 /etc/coinbosa-sentry-dsn; }

echo "==> Écriture de la sonde"
cat > /usr/local/bin/coinbosa-watchdog <<'WATCH'
#!/usr/bin/env bash
# Sonde Coinbosa. Alerte si la chaîne n'avance plus, si un nœud décroche, si le disque
# sature ou si le certificat expire. Conçue pour ne JAMAIS échouer bruyamment elle-même :
# une sonde qui plante est une sonde qui ne prévient plus.
set -uo pipefail

GETH=/opt/coinbosa-chain/build/bin/geth
NODE_IPC=/var/lib/coinbosa/node/geth.ipc
VAL_IPC=/var/lib/coinbosa/validator/geth.ipc
NODE_USER=coinbosa
VAL_USER=coinbosa-val
ETAT=/var/lib/coinbosa-monitoring/derniere-hauteur
DOMAINE=explorer.coinbosa.com
DISQUE_SEUIL=85
CERT_JOURS=21
STAGNATION_MAX=60   # 12 blocs manques a 5 s : au-dela, la chaine est reellement arretee
DSN=$(cat /etc/coinbosa-sentry-dsn 2>/dev/null || true)

alerte() {  # $1=niveau  $2=titre  $3=détail
  local niveau="$1" titre="$2" detail="${3:-}"
  logger -t coinbosa-watchdog -p daemon.err "[$niveau] $titre — $detail"
  [ -n "$DSN" ] || return 0
  # Envoi direct à Sentry (protocole "store"), sans SDK.
  local proto reste cle hote projet url
  proto="${DSN%%://*}"; reste="${DSN#*://}"
  cle="${reste%%@*}"; reste="${reste#*@}"
  hote="${reste%%/*}"; projet="${reste##*/}"
  url="$proto://$hote/api/$projet/store/"
  local charge
  charge=$(printf '{"level":"%s","logger":"coinbosa-watchdog","platform":"other","server_name":"%s","message":{"formatted":"%s — %s"},"tags":{"composant":"chaine","reseau":"coinbosa"}}' \
    "$niveau" "$(hostname)" "$titre" "$detail")
  # On VÉRIFIE que Sentry a accepté. Un canal d'alerte dont personne n'a prouvé qu'il
  # fonctionne est un placebo : le jour de la panne, le silence serait pris pour « tout va
  # bien ». Un envoi refusé est donc journalisé comme une panne à part entière.
  local code
  code=$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' -X POST "$url" \
    -H "Content-Type: application/json" \
    -H "X-Sentry-Auth: Sentry sentry_version=7, sentry_client=coinbosa-watchdog/1.0, sentry_key=$cle" \
    --data "$charge" 2>/dev/null || echo 000)
  case "$code" in
    200|201|202) ;;
    *) logger -t coinbosa-watchdog -p daemon.crit \
         "CANAL D ALERTE HORS SERVICE — Sentry a repondu $code. Les alertes ne partent PAS." ;;
  esac
}

hauteur() { sudo -u "$2" "$GETH" attach --exec 'eth.blockNumber' "$1" 2>/dev/null | tr -cd '0-9'; }

# --- 1. la chaîne avance-t-elle ? C'est LE test de vie. -----------------------
hv=$(hauteur "$VAL_IPC" "$VAL_USER")
hn=$(hauteur "$NODE_IPC" "$NODE_USER")

if [ -z "${hv:-}" ]; then
  alerte error "validateur injoignable" "aucune reponse sur $VAL_IPC"
else
  # On memorise la hauteur ET l'instant. Comparer deux mesures sans regarder le temps
  # ecoule produit un faux positif des qu'on releve deux fois en moins de 5 s : aucun bloc
  # n'a pu naitre entre les deux. Une sonde qui crie au loup finit par etre ignoree, donc
  # elle ne doit alerter que sur une stagnation REELLE.
  precedent=$(cut -d' ' -f1 "$ETAT" 2>/dev/null || echo "")
  quand=$(cut -d' ' -f2 "$ETAT" 2>/dev/null || echo "")
  maintenant=$(date +%s)

  if [ -n "$precedent" ] && [ "$hv" -lt "$precedent" ] 2>/dev/null; then
    # Un recul est anormal a tout instant : on alerte sans attendre.
    alerte fatal "REMBOBINAGE DETECTE" "hauteur passee de $precedent a $hv — fork probable"
    echo "$hv $maintenant" > "$ETAT"
  elif [ -n "$precedent" ] && [ "$hv" -eq "$precedent" ] 2>/dev/null; then
    # Stagnation : on ne crie qu'au-dela de STAGNATION_MAX (12 blocs manques).
    ecoule=$(( maintenant - ${quand:-$maintenant} ))
    if [ "$ecoule" -ge "$STAGNATION_MAX" ]; then
      alerte fatal "LA CHAINE N AVANCE PLUS" "hauteur bloquee a $hv depuis ${ecoule}s"
    fi
    # On NE met PAS a jour l'horodatage : sinon le compteur repartirait de zero a chaque
    # passage et la stagnation ne serait jamais detectee.
  else
    echo "$hv $maintenant" > "$ETAT"
  fi
fi

# --- 2. les deux nœuds racontent-ils la même chaîne ? ------------------------
if [ -n "${hv:-}" ] && [ -n "${hn:-}" ]; then
  ecart=$((hv - hn)); [ "$ecart" -lt 0 ] && ecart=$((-ecart))
  [ "$ecart" -gt 20 ] && alerte error "noeud RPC decroche" "validateur=$hv noeud=$hn ecart=$ecart blocs"
  # Un désaccord de hash à hauteur égale = fork silencieux, le pire des cas.
  if [ "$ecart" -eq 0 ] && [ "$hv" -gt 0 ]; then
    a=$(sudo -u "$VAL_USER" "$GETH" attach --exec "eth.getBlock($hv).hash" "$VAL_IPC" 2>/dev/null | tr -d '"')
    b=$(sudo -u "$NODE_USER" "$GETH" attach --exec "eth.getBlock($hv).hash" "$NODE_IPC" 2>/dev/null | tr -d '"')
    [ -n "$a" ] && [ -n "$b" ] && [ "$a" != "$b" ] && alerte fatal "FORK : hash divergents" "bloc $hv : $a vs $b"
  fi
else
  [ -z "${hn:-}" ] && alerte error "noeud RPC injoignable" "aucune reponse sur $NODE_IPC"
fi

# --- 3. services ------------------------------------------------------------
for s in coinbosa-validator coinbosa-node caddy; do
  systemctl is-active --quiet "$s" || alerte fatal "service arrete" "$s"
done

# --- 4. disque : la chaine grossit tous les jours ----------------------------
pct=$(df --output=pcent / 2>/dev/null | tail -1 | tr -cd '0-9')
[ -n "${pct:-}" ] && [ "$pct" -ge "$DISQUE_SEUIL" ] && alerte error "disque presque plein" "${pct}% utilises sur /"

# --- 5. certificat TLS ------------------------------------------------------
fin=$(echo | timeout 15 openssl s_client -connect "$DOMAINE:443" -servername "$DOMAINE" 2>/dev/null \
      | openssl x509 -noout -enddate 2>/dev/null | cut -d= -f2)
if [ -n "${fin:-}" ]; then
  reste=$(( ( $(date -d "$fin" +%s 2>/dev/null || echo 0) - $(date +%s) ) / 86400 ))
  [ "$reste" -lt "$CERT_JOURS" ] 2>/dev/null && alerte error "certificat TLS expire bientot" "$reste jours restants ($DOMAINE)"
fi

# --- 6. le RPC public est-il UTILISABLE par un tiers ? (sonde n6) -------------
# Le 12 aout 2026, FilterMaps — l'index des journaux de geth — a echoue a demarrer
# et n'est jamais reparti. Pendant SIX JOURS eth_getLogs a expire au bout de 30 s :
# la chaine etait illisible pour toute bourse, tout portefeuille, tout indexeur.
# Aucune alerte n'est partie. Le chien de garde regardait la hauteur, les services
# et le disque — tous verts — et la chaine produisait bien ses blocs.
#
# Une chaine qui avance mais que personne ne peut lire est en panne. Cette sonde
# mesure ce qu'un TIERS ferait vraiment, et par la meme porte que lui : une requete
# HTTPS sur le relais public, pas un appel local qui contournerait Caddy et le
# nœud RPC.
LOGS_SEUIL=8   # secondes ; un index sain repond en 1 s

hex_h=$(curl -s -X POST "https://$DOMAINE/rpc" -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' -m 20 \
  | sed -n 's/.*"result"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

if [ -z "${hex_h:-}" ]; then
  alerte error "RPC public muet" "eth_blockNumber sans reponse sur https://$DOMAINE/rpc"
else
  t0=$(date +%s)
  rep=$(curl -s -X POST "https://$DOMAINE/rpc" -H 'content-type: application/json' \
    -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_getLogs\",\"params\":[{\"fromBlock\":\"$hex_h\",\"toBlock\":\"$hex_h\"}]}" \
    -m $(( LOGS_SEUIL + 4 )))
  duree=$(( $(date +%s) - t0 ))

  # Trois pannes distinctes, trois messages distincts : « ca ne marche pas » ne dit
  # pas par ou commencer un dimanche a 3 h du matin.
  if [ -z "${rep:-}" ]; then
    alerte error "index des journaux MORT" "eth_getLogs sans reponse en ${duree}s — aucun tiers ne peut lire la chaine"
  elif echo "$rep" | grep -qE '"error"[[:space:]]*:'; then
    msg=$(echo "$rep" | sed -n 's/.*"message"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
    alerte error "index des journaux en erreur" "eth_getLogs repond: ${msg:-erreur} — chercher FilterMaps dans journalctl -u coinbosa-node"
  elif [ "$duree" -ge "$LOGS_SEUIL" ]; then
    alerte error "index des journaux LENT" "eth_getLogs a mis ${duree}s (seuil ${LOGS_SEUIL}s) — index en reconstruction, ou qui derive"
  fi
fi

# Battement de coeur quotidien. Sans lui, l'absence d'alerte est ambigue : canal muet
# parce que tout va bien, ou parce qu'il est casse ? Un evenement de niveau info par jour
# permet de distinguer les deux — s'il manque, c'est la supervision elle-meme qui est morte.
BATTEMENT=/var/lib/coinbosa-monitoring/dernier-battement
hier=$(date -d 'yesterday' +%Y-%m-%d 2>/dev/null || echo "")
aujourdhui=$(date +%Y-%m-%d)
if [ "$(cat "$BATTEMENT" 2>/dev/null)" != "$aujourdhui" ]; then
  echo "$aujourdhui" > "$BATTEMENT"
  alerte info "battement quotidien" "chaine a $(hauteur "$VAL_IPC" "$VAL_USER") — supervision operationnelle"
fi

exit 0
WATCH
chmod 0755 /usr/local/bin/coinbosa-watchdog

echo "==> Service et minuteur"
cat > /etc/systemd/system/coinbosa-watchdog.service <<'UNIT'
[Unit]
Description=Coinbosa — sonde de surveillance de la chaîne
After=coinbosa-node.service coinbosa-validator.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/coinbosa-watchdog
UNIT

cat > /etc/systemd/system/coinbosa-watchdog.timer <<'UNIT'
[Unit]
Description=Coinbosa — surveillance toutes les 2 minutes

[Timer]
OnBootSec=90s
OnUnitActiveSec=120s
AccuracySec=10s

[Install]
WantedBy=timers.target
UNIT

systemctl daemon-reload
systemctl enable --now coinbosa-watchdog.timer >/dev/null 2>&1

echo "==> Premier passage"
/usr/local/bin/coinbosa-watchdog && echo "    sonde exécutée sans erreur"

echo ""
echo "==> Surveillance active (toutes les 2 minutes)."
if [ -n "$DSN" ]; then
  echo "==> Épreuve du canal d'alerte (on ne se contente pas d'écrire le DSN)"
  proto="${DSN%%://*}"; reste="${DSN#*://}"; cle="${reste%%@*}"; reste="${reste#*@}"
  hote="${reste%%/*}"; projet="${reste##*/}"
  code=$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' -X POST "$proto://$hote/api/$projet/store/" \
    -H "Content-Type: application/json" \
    -H "X-Sentry-Auth: Sentry sentry_version=7, sentry_client=coinbosa-watchdog/1.0, sentry_key=$cle" \
    --data '{"level":"info","logger":"coinbosa-watchdog","platform":"other","message":{"formatted":"epreuve d installation — si tu lis ceci, le canal d alerte fonctionne"}}' 2>/dev/null || echo 000)
  case "$code" in
    200|201|202) echo "    ✓ Sentry a répondu $code — le canal FONCTIONNE, va voir l'événement d'épreuve" ;;
    401|403)     echo "    ✗ Sentry a répondu $code — DSN refusé (clé invalide ou révoquée). Alertes en journal seulement." >&2 ;;
    000)         echo "    ✗ Sentry injoignable — réseau bloqué ou hôte erroné. Alertes en journal seulement." >&2 ;;
    *)           echo "    ✗ Sentry a répondu $code — canal NON fonctionnel. Alertes en journal seulement." >&2 ;;
  esac
else
  echo "    ⚠ aucun DSN Sentry : alertes en JOURNAL uniquement (journalctl -t coinbosa-watchdog)"
fi
echo "    voir les alertes : journalctl -t coinbosa-watchdog -f"
echo "    état du minuteur : systemctl list-timers coinbosa-watchdog.timer"
