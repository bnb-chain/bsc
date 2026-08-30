#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — surveillance renforcée PENDANT la fenêtre de cotation.
#
#   sudo bash 72-surveillance-cotation.sh on   [--jours 7] [--intervalle 30]
#   sudo bash 72-surveillance-cotation.sh off                 <-- UNE commande, tout rentre
#   sudo bash 72-surveillance-cotation.sh etat
#   sudo bash 72-surveillance-cotation.sh controle            <-- liste de contrôle du jour J
#   sudo bash 72-surveillance-cotation.sh canal <voie>        <-- configure la voie humaine
#   sudo bash 72-surveillance-cotation.sh epreuve             <-- prouve la voie humaine
#        bash 72-surveillance-cotation.sh dehors [URL]        <-- à lancer HORS serveur
#
# ---------------------------------------------------------------------------
# CE QUE CE DISPOSITIF AJOUTE, ET POURQUOI
# ----------------------------------------
# La sonde en place (coinbosa-watchdog, toutes les 120 s) répond à la question
# « la chaîne est-elle vivante ? ». Elle y répond bien. Pendant une cotation la
# question change : « une BOURSE peut-elle, à cette seconde, lire la chaîne,
# créditer un dépôt et diffuser un retrait ? » — et surtout : « si la réponse
# devient non, un HUMAIN le saura-t-il en moins de cinq minutes ? »
#
# Trois manques ont été constatés, chiffres à l'appui, avant d'écrire ce script.
#
#   1. AUCUNE VOIE VERS UN HUMAIN N'EST PROUVÉE.
#      Le canal Sentry ingère : zéro ligne « CANAL D ALERTE HORS SERVICE » en
#      huit jours de journal, et six battements quotidiens acceptés. Mais
#      l'ingestion n'est pas la notification. Sentry groupe les évènements par
#      empreinte ; la règle par défaut n'envoie un courriel qu'à la PREMIÈRE
#      occurrence d'un groupe. « validateur injoignable » a déjà été vu le
#      28 août : une récidive pendant la cotation serait rangée dans le groupe
#      existant et ne réveillerait personne. Ce script ajoute donc une voie
#      DIRECTE (Telegram / webhook / ntfy) qui ne dépend d'aucune règle
#      configurée ailleurs, et il REFUSE de s'activer tant que cette voie n'a
#      pas répondu 2xx.
#
#   2. RIEN NE MESURE CE QU'UNE BOURSE VOIT.
#      La sonde n6 existante vérifie eth_getLogs. Elle ne vérifie ni le chainId,
#      ni la cohérence des réponses, ni la profondeur d'historique, ni — surtout
#      — si la bourse est en train de se faire BANNIR par notre propre fail2ban.
#
#   3. LE PIÈGE fail2ban.
#      La prison caddy-rpc bannit 30 minutes, au pare-feu, toute IP dépassant
#      1500 requêtes /rpc par minute (relevé : maxretry=1500 findtime=60
#      bantime=1800). 99 bannissements ont déjà eu lieu, un est actif. Le débit
#      actuel plafonne à 88 req/min pour l'IP la plus active — marge ×17. Une
#      bourse qui rattrape l'historique ou un teneur de marché derrière une seule
#      IP franchit ce seuil sans effort. Le bannissement est SILENCIEUX : de
#      notre côté tout est vert, de leur côté la chaîne a disparu. Ce script
#      surveille la prison et prévient AVANT (à 60 % du seuil) puis APRÈS.
#
# ---------------------------------------------------------------------------
# RÈGLE DE CONSTRUCTION : ce dispositif est TEMPORAIRE et se retire d'une seule
# commande. Il n'édite AUCUN fichier existant. Il pose des ajouts (« drop-in »
# systemd, unités neuves, scripts neufs) que « off » supprime intégralement.
# Il porte en outre une ÉCHÉANCE : passée la date, il se désarme tout seul. Une
# surveillance renforcée qu'on oublie d'éteindre devient un bruit de fond, et un
# bruit de fond, on cesse de le lire.
#
# RÈGLE DE SÉCURITÉ : toutes les lectures de la chaîne sont en LECTURE SEULE
# (eth.blockNumber, eth.getBlock, requêtes HTTP GET/POST de consultation).
# Aucune transaction n'est émise, aucun compte n'est déverrouillé, le validateur
# n'est jamais redémarré par ce script.
# ---------------------------------------------------------------------------
set -euo pipefail

ACTION="${1:-aide}"; shift || true

REP_ETAT=/var/lib/coinbosa-monitoring
CONF_CANAL=/etc/coinbosa-alerte-humain          # 0600, jamais affiché
CONF_FENETRE=$REP_ETAT/cotation-fenetre         # échéance epoch + paramètres
DROPIN_WD=/etc/systemd/system/coinbosa-watchdog.timer.d/90-cotation.conf
DROPIN_JR=/etc/systemd/system/coinbosa-journal.timer.d/90-cotation.conf
BIN_ALERTE=/usr/local/bin/coinbosa-alerte-humain
BIN_SONDE=/usr/local/bin/coinbosa-sonde-bourse
UNIT_SVC=/etc/systemd/system/coinbosa-cotation.service
UNIT_TMR=/etc/systemd/system/coinbosa-cotation.timer

DOMAINE="${DOMAINE:-explorer.coinbosa.com}"
RPC="https://$DOMAINE/rpc"
CHAIN_ID_ATTENDU=0x6696       # 26262
NET_ID_ATTENDU=26262
IP_PUBLIQUE_ATTENDUE="${IP_PUBLIQUE:-}"

exige_root() { [ "$(id -u)" -eq 0 ] || { echo "à lancer en root (sudo bash $0 $ACTION)" >&2; exit 1; }; }

# ===========================================================================
#  VOIE HUMAINE
# ===========================================================================
# Trois transports, aucun démon à installer : tout passe par curl, déjà présent,
# et la sortie du pare-feu est ouverte (ufw : « Default: allow (outgoing) »).
# Joignabilité vérifiée depuis la machine avant d'écrire ces lignes :
#   api.telegram.org -> 302   hooks.slack.com -> 302   ntfy.sh -> 200
# Aucun agent de courrier n'est installé (ni sendmail, ni mail, ni msmtp) : la
# voie « courriel depuis la machine » exigerait un service en plus, ce que la
# contrainte interdit. Le courriel reste possible PAR Sentry, mais Sentry ne
# peut pas être la seule voie, pour la raison de groupage exposée plus haut.
ecrire_canal() {
  exige_root
  local spec="${1:-}"
  case "$spec" in
    TELEGRAM\|*\|*) ;;
    WEBHOOK\|http*)  ;;
    NTFY\|http*)     ;;
    *) cat >&2 <<'AIDE'
Forme attendue (une seule ligne, entre apostrophes) :

  TELEGRAM|<jeton_du_bot>|<identifiant_de_conversation>
      Créer le bot auprès de @BotFather, puis récupérer l'identifiant en
      écrivant une fois au bot et en lisant getUpdates. Notification poussée sur
      le téléphone, sans compte payant.

  WEBHOOK|https://hooks.slack.com/services/...
      Slack, Discord (ajouter /slack à l'URL), Google Chat : tous acceptent
      un corps JSON {"text":"…"}.

  NTFY|https://ntfy.sh/<sujet-prive-et-long>
      Aucun compte. Le sujet EST le secret : prendre 32 caractères aléatoires.
      Application mobile ntfy, notification poussée.

Exemple :
  sudo bash 72-surveillance-cotation.sh canal 'NTFY|https://ntfy.sh/coinbosa-xxxxxxxxxxxxxxxx'
AIDE
       exit 1 ;;
  esac
  install -d -m 0750 "$REP_ETAT"
  printf '%s' "$spec" > "$CONF_CANAL"
  chmod 0600 "$CONF_CANAL"
  echo "==> Voie humaine enregistrée dans $CONF_CANAL (0600, contenu jamais affiché)."
  echo "    Transport : ${spec%%|*}"
}

# Le script d'alerte est posé sur la machine : la sonde périodique s'en sert.
poser_alerte_humain() {
cat > "$BIN_ALERTE" <<'ALERTE'
#!/usr/bin/env bash
# coinbosa-alerte-humain <niveau> <cle> <titre> <detail>
#   niveau : info | attention | grave
#   cle    : identifiant stable de l'alerte, sert au refroidissement
# Pousse un message vers un HUMAIN. Ne remplace pas le journal ni Sentry : il
# s'y ajoute. Le journal reste la trace, cette voie est le réveil.
set -uo pipefail
NIVEAU="${1:-info}"; CLE="${2:-generique}"; TITRE="${3:-}"; DETAIL="${4:-}"
CONF=/etc/coinbosa-alerte-humain
REP=/var/lib/coinbosa-monitoring
FROID_INFO=21600      # 6 h
FROID_ATTENTION=1800  # 30 min
FROID_GRAVE=300       # 5 min

logger -t coinbosa-cotation -p daemon.err "[$NIVEAU] $TITRE — $DETAIL"
[ -r "$CONF" ] || { logger -t coinbosa-cotation -p daemon.crit \
  "AUCUNE VOIE HUMAINE CONFIGUREE — l alerte reste dans le journal"; exit 0; }

# --- refroidissement : une voie qui sonne toutes les 30 s est une voie qu'on
# --- coupe. On espace, et on dit depuis combien de temps ça dure.
case "$NIVEAU" in
  grave)     FROID=$FROID_GRAVE ;;
  attention) FROID=$FROID_ATTENTION ;;
  *)         FROID=$FROID_INFO ;;
esac
mkdir -p "$REP" 2>/dev/null || true
MARQUE="$REP/froid-$(printf '%s' "$CLE" | tr -c 'a-zA-Z0-9_.-' '_')"
MAINTENANT=$(date +%s)
PREMIER=$MAINTENANT
if [ -r "$MARQUE" ]; then
  DERNIER=$(cut -d' ' -f1 "$MARQUE" 2>/dev/null || echo 0)
  PREMIER=$(cut -d' ' -f2 "$MARQUE" 2>/dev/null || echo "$MAINTENANT")
  [ -z "$PREMIER" ] && PREMIER=$MAINTENANT
  if [ $(( MAINTENANT - ${DERNIER:-0} )) -lt "$FROID" ]; then
    logger -t coinbosa-cotation "refroidissement actif ($CLE), non repousse"
    exit 0
  fi
fi
DUREE=$(( MAINTENANT - PREMIER ))
echo "$MAINTENANT $PREMIER" > "$MARQUE"
SUITE=""
[ "$DUREE" -ge 120 ] && SUITE=" — PERSISTE DEPUIS $((DUREE/60)) min"

case "$NIVEAU" in
  grave)     PICTO="[GRAVE]"; PRIO=urgent ;;
  attention) PICTO="[ATTENTION]"; PRIO=high ;;
  *)         PICTO="[info]"; PRIO=default ;;
esac
HOTE=$(hostname)
TEXTE="$PICTO COINBOSA $TITRE
$DETAIL$SUITE
machine $HOTE — $(date -u '+%Y-%m-%d %H:%M:%S UTC')"

SPEC=$(cat "$CONF")
TRANSPORT="${SPEC%%|*}"; RESTE="${SPEC#*|}"
CODE=000
case "$TRANSPORT" in
  TELEGRAM)
    JETON="${RESTE%%|*}"; CHAT="${RESTE##*|}"
    CODE=$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' \
      "https://api.telegram.org/bot$JETON/sendMessage" \
      --data-urlencode "chat_id=$CHAT" --data-urlencode "text=$TEXTE" \
      --data "disable_notification=false" 2>/dev/null || echo 000) ;;
  WEBHOOK)
    CHARGE=$(printf '%s' "$TEXTE" | python3 -c 'import json,sys; print(json.dumps({"text":sys.stdin.read()}))' 2>/dev/null) || CHARGE='{"text":"coinbosa alerte"}'
    CODE=$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' -X POST "$RESTE" \
      -H 'Content-Type: application/json' --data "$CHARGE" 2>/dev/null || echo 000) ;;
  NTFY)
    CODE=$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' -X POST "$RESTE" \
      -H "Title: COINBOSA $TITRE" -H "Priority: $PRIO" -H "Tags: warning" \
      --data "$TEXTE" 2>/dev/null || echo 000) ;;
esac

case "$CODE" in
  200|201|202|204) logger -t coinbosa-cotation "voie humaine OK ($TRANSPORT, $CODE) : $TITRE" ;;
  *) logger -t coinbosa-cotation -p daemon.crit \
       "VOIE HUMAINE HORS SERVICE — $TRANSPORT a repondu $CODE. Personne n a ete prevenu de : $TITRE" ;;
esac
exit 0
ALERTE
chmod 0700 "$BIN_ALERTE"
}

# ===========================================================================
#  SONDE « ŒIL DE LA BOURSE »
# ===========================================================================
poser_sonde() {
cat > "$BIN_SONDE" <<'SONDE'
#!/usr/bin/env bash
# Sonde de cotation. Mesure ce qu'une BOURSE voit, pas ce que la machine croit.
# Elle ne remplace pas coinbosa-watchdog : celle-ci regarde la santé interne,
# celle-là regarde la porte d'entrée. Les deux tournent en parallèle.
set -uo pipefail

DOMAINE=explorer.coinbosa.com
RPC="https://$DOMAINE/rpc"
REP=/var/lib/coinbosa-monitoring
FENETRE=$REP/cotation-fenetre
ALERTE=/usr/local/bin/coinbosa-alerte-humain
GETH=/opt/coinbosa-chain/build/bin/geth
VAL_IPC=/var/lib/coinbosa/validator/geth.ipc
VAL_USER=coinbosa-val

CHAIN_ID_ATTENDU=0x6696
NET_ID_ATTENDU=26262
LAT_SEUIL_MS=1500         # depuis la machine : tout ce qui dépasse est un défaut serveur
BAN_PREAVIS_PCT=60        # préavis à 60 % du seuil fail2ban
RESUME_H=${RESUME_H:-6}   # résumé « tout va bien » toutes les N heures

# --- échéance : la surveillance renforcée se désarme toute seule -------------
if [ -r "$FENETRE" ]; then
  FIN=$(sed -n 's/^fin=//p' "$FENETRE" | tr -cd '0-9')
  if [ -n "${FIN:-}" ] && [ "$(date +%s)" -ge "$FIN" ]; then
    logger -t coinbosa-cotation "echeance atteinte — desarmement automatique"
    "$ALERTE" info echeance "surveillance de cotation terminee" \
      "echeance atteinte : frequence revenue a 120 s, sonde de cotation desarmee. La sonde de base et l arret propre restent actifs. Passer 72-surveillance-cotation.sh off pour retirer aussi les fichiers d unites." || true
    rm -f /etc/systemd/system/coinbosa-watchdog.timer.d/90-cotation.conf \
          /etc/systemd/system/coinbosa-journal.timer.d/90-cotation.conf 2>/dev/null || true
    rmdir /etc/systemd/system/coinbosa-watchdog.timer.d \
          /etc/systemd/system/coinbosa-journal.timer.d 2>/dev/null || true
    systemctl --no-block disable --now coinbosa-cotation.timer 2>/dev/null || true
    systemctl --no-block daemon-reload 2>/dev/null || true
    systemctl --no-block restart coinbosa-watchdog.timer 2>/dev/null || true
    rm -f "$FENETRE"
    exit 0
  fi
fi

# --- fenêtre de maintenance : on réutilise le témoin EXISTANT ----------------
# Prouvé opérationnel : « fenetre de maintenance active, encore 415s » relevé le
# 30 août à 04:19:33. Pendant l'arrêt propre planifié, les pannes qu'un
# redémarrage explique ne réveillent pas d'humain ; tout le reste, si.
MAINTENANCE=0
TEMOIN=/run/coinbosa-maintenance
if [ -r "$TEMOIN" ]; then
  f=$(tr -cd '0-9' < "$TEMOIN"); n=$(date +%s)
  [ -n "$f" ] && [ "$n" -lt "$f" ] 2>/dev/null && MAINTENANCE=1
fi
crier()      { "$ALERTE" "$@"; }
crier_doux() { [ "$MAINTENANCE" = 1 ] && { logger -t coinbosa-cotation "pendant maintenance, non pousse : $3"; return 0; }; "$ALERTE" "$@"; }

rpc() { curl -s -X POST "$RPC" -H 'content-type: application/json' -d "$1" -m 20 2>/dev/null; }
res() { sed -n 's/.*"result"[[:space:]]*:[[:space:]]*"\{0,1\}\([^",}]*\)"\{0,1\}.*/\1/p'; }

DEFAUTS=0

# --- 1. la porte d'entrée répond-elle, et en combien de temps ? --------------
T0=$(date +%s%N)
REP_H=$(rpc '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}')
MS=$(( ( $(date +%s%N) - T0 ) / 1000000 ))
HEX=$(printf '%s' "$REP_H" | res)
if [ -z "${HEX:-}" ]; then
  DEFAUTS=$((DEFAUTS+1))
  crier_doux grave rpc-muet "RPC PUBLIC MUET" "eth_blockNumber sans reponse sur $RPC — aucune bourse ne peut lire la chaine"
else
  HAUTEUR=$(( 16#${HEX#0x} ))
  if [ "$MS" -ge "$LAT_SEUIL_MS" ]; then
    DEFAUTS=$((DEFAUTS+1))
    crier_doux attention rpc-lent "RPC PUBLIC LENT" "eth_blockNumber a mis ${MS} ms depuis la machine elle-meme (seuil ${LAT_SEUIL_MS} ms) — un client distant verra pire"
  fi

  # --- 2. cohérence : une bourse refuse d'intégrer une chaîne qui se
  # ---    contredit. chainId et networkId doivent concorder, toujours.
  CID=$(rpc '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' | res)
  NID=$(rpc '{"jsonrpc":"2.0","id":1,"method":"net_version","params":[]}' | res)
  if [ "${CID:-}" != "$CHAIN_ID_ATTENDU" ] || [ "${NID:-}" != "$NET_ID_ATTENDU" ]; then
    DEFAUTS=$((DEFAUTS+1))
    crier grave incoherence "IDENTITE DE CHAINE INCOHERENTE" \
      "eth_chainId=${CID:-vide} (attendu $CHAIN_ID_ATTENDU) net_version=${NID:-vide} (attendu $NET_ID_ATTENDU)"
  fi

  # --- 3. le nœud se déclare-t-il synchronisé ? -------------------------------
  SYNC=$(rpc '{"jsonrpc":"2.0","id":1,"method":"eth_syncing","params":[]}')
  if ! printf '%s' "$SYNC" | grep -q '"result":false'; then
    DEFAUTS=$((DEFAUTS+1))
    crier_doux grave desync "LE NOEUD PUBLIC SE DIT DESYNCHRONISE" "eth_syncing renvoie ${SYNC:0:160}"
  fi

  # --- 4. la hauteur AVANCE vue de l'extérieur --------------------------------
  # Le watchdog vérifie la hauteur du validateur par IPC. Ce n'est pas la même
  # chose : le validateur peut avancer pendant que le nœud public sert une
  # hauteur figée. C'est ce que la bourse verrait, et elle en conclurait que la
  # chaîne est morte.
  M=$REP/cotation-derniere-hauteur
  P=$(cut -d' ' -f1 "$M" 2>/dev/null || echo ""); Q=$(cut -d' ' -f2 "$M" 2>/dev/null || echo "")
  N=$(date +%s)
  if [ -n "$P" ] && [ "$HAUTEUR" -le "$P" ] 2>/dev/null; then
    E=$(( N - ${Q:-$N} ))
    if [ "$HAUTEUR" -lt "$P" ]; then
      DEFAUTS=$((DEFAUTS+1))
      crier grave rembobinage "REMBOBINAGE VU DEPUIS LE RPC PUBLIC" "hauteur passee de $P a $HAUTEUR — fork ou reprise sur etat incomplet"
      echo "$HAUTEUR $N" > "$M"
    elif [ "$E" -ge 60 ]; then
      DEFAUTS=$((DEFAUTS+1))
      crier_doux grave stagnation "LA CHAINE N AVANCE PLUS (vue publique)" "hauteur bloquee a $HAUTEUR depuis ${E}s — 5 s/bloc attendus"
    fi
  else
    echo "$HAUTEUR $N" > "$M"
  fi

  # --- 5. profondeur d'historique : une bourse rattrape le passé --------------
  # Le nœud tourne en --gcmode full : l'ÉTAT ancien n'existe pas, seuls les
  # BLOCS et les JOURNAUX remontent au genesis. On vérifie donc ce qui doit
  # marcher — les blocs et les journaux — et on documente ce qui ne marchera
  # jamais plutôt que de le laisser découvrir par la bourse.
  G=$(rpc '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x0",false]}')
  printf '%s' "$G" | grep -q '"number":"0x0"' || {
    DEFAUTS=$((DEFAUTS+1))
    crier attention histoire "HISTORIQUE AMPUTE" "eth_getBlockByNumber(0x0) ne rend plus le bloc de genese — reponse: ${G:0:160}"
  }
  T1=$(date +%s)
  DEB=$(( HAUTEUR > 5000 ? HAUTEUR - 4999 : 1 ))
  L=$(rpc "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_getLogs\",\"params\":[{\"fromBlock\":\"$(printf '0x%x' $DEB)\",\"toBlock\":\"$HEX\"}]}")
  DL=$(( $(date +%s) - T1 ))
  if [ -z "${L:-}" ]; then
    DEFAUTS=$((DEFAUTS+1))
    crier_doux grave getlogs "INDEX DES JOURNAUX MORT" "eth_getLogs sur 5000 blocs sans reponse en ${DL}s — aucun indexeur ne peut suivre"
  elif printf '%s' "$L" | grep -q '"error"'; then
    DEFAUTS=$((DEFAUTS+1))
    crier attention getlogs "eth_getLogs EN ERREUR SUR 5000 BLOCS" "la plage maximale annoncee aux bourses n est plus tenue : ${L:0:200}"
  elif [ "$DL" -ge 8 ]; then
    DEFAUTS=$((DEFAUTS+1))
    crier attention getlogs-lent "INDEX DES JOURNAUX LENT" "eth_getLogs sur 5000 blocs a mis ${DL}s (seuil 8 s)"
  fi
fi

# --- 6. LE PIÈGE : fail2ban bannit-il la bourse ? ----------------------------
# Un bannissement est invisible depuis l'intérieur : de notre côté tout est
# vert, du leur la chaîne a disparu pendant 30 minutes. C'est le défaut le plus
# probable d'une fenêtre de cotation, et le seul qu'aucune sonde ne regardait.
if command -v fail2ban-client >/dev/null 2>&1; then
  ETAT_J=$(fail2ban-client status caddy-rpc 2>/dev/null || true)
  BANNIES=$(printf '%s' "$ETAT_J" | sed -n 's/.*Banned IP list:[[:space:]]*//p')
  if [ -n "${BANNIES// /}" ]; then
    crier grave ban-rpc "UNE IP EST BANNIE DU RPC" \
      "fail2ban/caddy-rpc bloque : $BANNIES — si c est la bourse ou son indexeur, elle voit la chaine HORS LIGNE. Liberer : fail2ban-client set caddy-rpc unbanip <IP>"
  fi
  ETAT_S=$(fail2ban-client status caddy-status 2>/dev/null || true)
  BAN_S=$(printf '%s' "$ETAT_S" | sed -n 's/.*Banned IP list:[[:space:]]*//p')
  if [ -n "${BAN_S// /}" ]; then
    crier attention ban-status "UNE IP EST BANNIE DU SITE" \
      "fail2ban/caddy-status bloque : $BAN_S (declenche par 60 reponses 4xx/min ; un GET /rpc renvoie 405 et compte)"
  fi
fi

# --- 7. préavis : qui s'approche du seuil de bannissement ? ------------------
# Prévenir AVANT vaut mieux que débannir après. Seuil relevé : 1500 req/min.
LOG=/var/log/caddy/explorer-access.log
if [ -r "$LOG" ]; then
  CHAUDE=$(tail -c 4000000 "$LOG" 2>/dev/null | python3 -c '
import sys,json,collections,time
now=time.time(); c=collections.Counter()
for l in sys.stdin:
    try: o=json.loads(l)
    except Exception: continue
    if now-o.get("ts",0)>60: continue
    r=o.get("request",{})
    if str(r.get("uri","")).startswith("/rpc"): c[r.get("remote_ip")]+=1
if c:
    ip,n=c.most_common(1)[0]
    print(f"{ip} {n}")
' 2>/dev/null || true)
  if [ -n "${CHAUDE:-}" ]; then
    IP=${CHAUDE%% *}; NB=${CHAUDE##* }
    SEUIL=$(( 1500 * BAN_PREAVIS_PCT / 100 ))
    if [ "${NB:-0}" -ge "$SEUIL" ] 2>/dev/null; then
      crier attention preavis-ban "UNE IP VA ETRE BANNIE" \
        "$IP a fait $NB requetes /rpc dans la derniere minute (bannissement a 1500/min, 30 min). Si c est la bourse : fail2ban-client set caddy-rpc addignoreip $IP"
    fi
  fi
fi

# --- 8. la machine est-elle accaparée par autre chose que la chaîne ? --------
# Constat du 28 août : un « grep -r / » oublié par un audit consommait 100 %
# d'un cœur sur quatre, sans interruption, pendant plus de 48 h. Rien ne le
# signalait. Sur la machine qui héberge l'UNIQUE validateur, ça ne se reproduit
# pas pendant une cotation.
GLOUTON=$(ps -eo pcpu,etimes,user,comm,args --sort=-pcpu --no-headers 2>/dev/null \
  | awk '$1>=50 && $2>=300 && $4!="geth" {printf "%s (%s%% CPU depuis %d min) ", $4, $1, $2/60; }' | head -c 300)
if [ -n "${GLOUTON// /}" ]; then
  crier attention glouton "UN PROCESSUS ACCAPARE LA MACHINE" \
    "$GLOUTON — le validateur partage ces coeurs. Verifier : ps -eo pid,pcpu,etimes,args --sort=-pcpu | head"
fi

# --- 9. résumé périodique : le silence doit vouloir dire quelque chose -------
BAT=$REP/cotation-dernier-resume
N=$(date +%s); D=$(cat "$BAT" 2>/dev/null || echo 0)
if [ $(( N - ${D:-0} )) -ge $(( RESUME_H * 3600 )) ]; then
  echo "$N" > "$BAT"
  CH=$(df --output=pcent / 2>/dev/null | tail -1 | tr -cd '0-9')
  "$ALERTE" info resume "tout va bien" \
    "hauteur ${HAUTEUR:-?} — latence ${MS:-?} ms — defauts ce passage: $DEFAUTS — disque ${CH:-?}%" || true
fi
exit 0
SONDE
chmod 0700 "$BIN_SONDE"
}

# ===========================================================================
#  ACTIVATION
# ===========================================================================
activer() {
  exige_root
  local JOURS=7 INTERVALLE=30 RESUME=6
  while [ $# -gt 0 ]; do
    case "$1" in
      --jours)      JOURS="$2"; shift 2 ;;
      --intervalle) INTERVALLE="$2"; shift 2 ;;
      --resume)     RESUME="$2"; shift 2 ;;
      *) echo "option inconnue : $1" >&2; exit 1 ;;
    esac
  done
  # Bornes. En dessous de 10 s les passages se chevauchent (un passage de la
  # sonde de base dure ~1,3 s en temps réel, mesuré : « Starting » 21:57:23,
  # « Finished » 21:57:24). Au-dessus de 119 s on ne renforce plus rien, et la
  # preuve d'application ci-dessous ne saurait plus distinguer le réglage posé
  # de celui d'origine.
  case "$INTERVALLE" in ''|*[!0-9]*) echo "--intervalle doit être un entier de secondes" >&2; exit 1;; esac
  case "$JOURS"      in ''|*[!0-9]*) echo "--jours doit être un entier de jours" >&2; exit 1;; esac
  [ "$INTERVALLE" -ge 10 ] && [ "$INTERVALLE" -le 119 ] || {
    echo "--intervalle hors bornes ($INTERVALLE) : attendu entre 10 et 119 secondes." >&2; exit 1; }
  [ "$JOURS" -ge 1 ] && [ "$JOURS" -le 30 ] || {
    echo "--jours hors bornes ($JOURS) : attendu entre 1 et 30." >&2; exit 1; }

  # --- garde-fou n°1 : pas d'activation sans voie humaine PROUVÉE -----------
  # Une surveillance renforcée dont les alertes finissent dans un journal que
  # personne ne lit n'est pas une surveillance : c'est une décoration. On exige
  # donc un 2xx du transport AVANT de toucher aux minuteries.
  [ -r "$CONF_CANAL" ] || {
    echo "ARRÊT : aucune voie humaine configurée." >&2
    echo "        sudo bash $0 canal 'NTFY|https://ntfy.sh/<sujet-long-et-secret>'" >&2
    exit 1; }
  install -d -m 0750 "$REP_ETAT"
  poser_alerte_humain
  echo "==> Épreuve de la voie humaine (obligatoire)"
  rm -f "$REP_ETAT"/froid-epreuve
  "$BIN_ALERTE" info epreuve "EPREUVE D ACTIVATION" \
    "si tu lis ce message sur ton telephone, la voie humaine fonctionne. Surveillance de cotation en cours d activation."
  if journalctl -t coinbosa-cotation --since "-1 min" --no-pager 2>/dev/null | grep -q "VOIE HUMAINE HORS SERVICE"; then
    echo "ARRÊT : le transport a refusé le message. Rien n'a été modifié." >&2
    journalctl -t coinbosa-cotation --since "-1 min" --no-pager | tail -3 >&2
    exit 1
  fi
  echo "    transport accepté (2xx). VÉRIFIE MAINTENANT ton téléphone :"
  echo "    un 2xx prouve que le serveur a pris le message, pas qu'il t'a atteint."

  # --- garde-fou n°2 : ne pas se bannir soi-même ----------------------------
  # La sonde interroge le RPC PAR LA PORTE PUBLIQUE, donc son trafic est vu par
  # fail2ban avec l'IP publique de la machine. Relevé : 168.231.113.53 apparaît
  # bien dans /var/log/caddy/explorer-access.log, et la liste blanche de la
  # prison ne contient que 127.0.0.0/8, ::1 et l'IP de l'opérateur.
  local IPPUB
  IPPUB="${IP_PUBLIQUE_ATTENDUE:-$(ip -4 addr show scope global 2>/dev/null | sed -n 's/.*inet \([0-9.]*\).*/\1/p' | head -1)}"
  if [ -n "${IPPUB:-}" ] && command -v fail2ban-client >/dev/null 2>&1; then
    if fail2ban-client get caddy-rpc ignoreip 2>/dev/null | grep -q "$IPPUB"; then
      echo "==> $IPPUB déjà en liste blanche fail2ban."
    else
      fail2ban-client set caddy-rpc    addignoreip "$IPPUB" >/dev/null 2>&1 || true
      fail2ban-client set caddy-status addignoreip "$IPPUB" >/dev/null 2>&1 || true
      echo "==> $IPPUB ajoutée à la liste blanche fail2ban (non persistant : reprendre après un restart de fail2ban)."
    fi
  fi

  # --- échéance ------------------------------------------------------------
  local FIN; FIN=$(( $(date +%s) + JOURS * 86400 ))
  { echo "fin=$FIN"; echo "jours=$JOURS"; echo "intervalle=$INTERVALLE"; echo "resume=$RESUME";
    echo "pose=$(date -u '+%Y-%m-%dT%H:%M:%SZ')"; } > "$CONF_FENETRE"
  chmod 0640 "$CONF_FENETRE"

  poser_sonde

  # --- fréquence de la sonde de base : drop-in, aucun fichier existant touché
  install -d -m 0755 "$(dirname "$DROPIN_WD")"
  cat > "$DROPIN_WD" <<UNIT
# TEMPORAIRE — posé par deploy/72-surveillance-cotation.sh. Retiré par « off ».
# Fenêtre de cotation : la sonde de base passe de 120 s à ${INTERVALLE} s.
# La ligne vide REMET À ZÉRO la valeur héritée : sans elle, systemd empile les
# deux périodes et garde la plus courte de façon non évidente.
[Timer]
OnUnitActiveSec=
OnUnitActiveSec=${INTERVALLE}s
AccuracySec=5s
UNIT

  # --- arrêt propre planifié : on le GARDE, mais on le rend PRÉVISIBLE ------
  # Voir la note de décision en fin de fichier. RandomizedDelaySec=300 fait
  # dériver l'heure réelle de 04:17 à 04:22 selon les jours (relevé sur six
  # jours : 04:17:13, 04:18:03, 04:19:08, 04:19:13, 04:20:19, 04:20:58). Une
  # maintenance qu'on annonce à une bourse doit tomber à la minute annoncée.
  install -d -m 0755 "$(dirname "$DROPIN_JR")"
  cat > "$DROPIN_JR" <<'UNIT'
# TEMPORAIRE — posé par deploy/72-surveillance-cotation.sh. Retiré par « off ».
# On ne suspend PAS l'arrêt propre planifié (justification en fin de script) :
# on supprime seulement son aléa, pour pouvoir annoncer la minute exacte.
[Timer]
RandomizedDelaySec=0
UNIT

  # --- sonde « œil de la bourse » ------------------------------------------
  cat > "$UNIT_SVC" <<UNIT
[Unit]
Description=Coinbosa — sonde de cotation (ce qu une bourse voit)
After=coinbosa-node.service caddy.service

[Service]
Type=oneshot
Environment=RESUME_H=${RESUME}
ExecStart=$BIN_SONDE
TimeoutStartSec=90
# Plus basse priorité que la chaîne : le validateur ne doit jamais attendre
# derrière une sonde. Mesuré : la sonde coûte < 0,3 s de CPU par passage.
Nice=10
IOSchedulingClass=idle
CPUWeight=20
UNIT

  cat > "$UNIT_TMR" <<UNIT
[Unit]
Description=Coinbosa — sonde de cotation, toutes les ${INTERVALLE}s

[Timer]
OnBootSec=120s
OnUnitActiveSec=${INTERVALLE}s
AccuracySec=5s

[Install]
WantedBy=timers.target
UNIT

  systemctl daemon-reload
  systemctl restart coinbosa-watchdog.timer
  systemctl restart coinbosa-journal.timer

  # --- PREUVE que le drop-in a bien pris, sinon ANNULATION ------------------
  # OnUnitActiveSec est un réglage de type LISTE : sans la ligne vide qui le
  # remet à zéro, systemd EMPILE 120 s et la nouvelle valeur, et l'intervalle
  # réel devient imprévisible. Cette mécanique n'a pas pu être éprouvée hors de
  # cette machine (aucun Linux jetable n'était disponible à l'écriture) : elle
  # est donc vérifiée ICI, sur pièce. Un dispositif qui se croit renforcé sans
  # l'être est pire qu'un dispositif absent — il rassure.
  local PER NB
  PER=$(systemctl show coinbosa-watchdog.timer -p TimersMonotonic --value 2>/dev/null)
  NB=$(printf '%s\n' "$PER" | grep -c 'OnUnitActiveUSec=' || true)
  if [ "${NB:-0}" -ne 1 ] || printf '%s' "$PER" | grep -q 'OnUnitActiveUSec=2min'; then
    echo "ARRÊT : le drop-in n'a pas produit l'effet attendu." >&2
    echo "        relevé : $PER" >&2
    echo "        (attendu : UNE seule ligne OnUnitActiveUSec, et pas 2min)" >&2
    rm -f "$DROPIN_WD" "$DROPIN_JR" "$UNIT_TMR" "$UNIT_SVC" "$CONF_FENETRE" "$BIN_SONDE"
    rmdir "$(dirname "$DROPIN_WD")" "$(dirname "$DROPIN_JR")" 2>/dev/null || true
    systemctl daemon-reload
    systemctl restart coinbosa-watchdog.timer
    systemctl restart coinbosa-journal.timer
    echo "        Tout a été remis en l'état. Rien n'est activé." >&2
    exit 1
  fi
  echo "==> Période de la sonde de base, VÉRIFIÉE après application :"
  echo "    $PER"

  systemctl enable --now coinbosa-cotation.timer >/dev/null 2>&1

  echo "==> Premier passage de la sonde de cotation"
  "$BIN_SONDE" && echo "    sonde exécutée sans erreur"

  echo ""
  echo "==> ACTIF jusqu'au $(date -u -d "@$FIN" '+%Y-%m-%d %H:%M UTC' 2>/dev/null || echo "+$JOURS j")"
  echo "    sonde de base      : toutes les ${INTERVALLE}s (au lieu de 120 s)"
  echo "    sonde de cotation  : toutes les ${INTERVALLE}s"
  echo "    résumé « tout va bien » : toutes les ${RESUME} h"
  echo "    désactivation      : sudo bash $0 off"
  systemctl list-timers --no-legend coinbosa-watchdog.timer coinbosa-cotation.timer coinbosa-journal.timer | sed 's/^/    /'
}

# ===========================================================================
#  DÉSACTIVATION — une seule commande, et tout rentre dans l'ordre
# ===========================================================================
desactiver() {
  exige_root
  echo "==> Retrait du dispositif de cotation"
  systemctl disable --now coinbosa-cotation.timer >/dev/null 2>&1 || true
  rm -f "$UNIT_TMR" "$UNIT_SVC" "$DROPIN_WD" "$DROPIN_JR" "$BIN_SONDE"
  rmdir "$(dirname "$DROPIN_WD")" "$(dirname "$DROPIN_JR")" 2>/dev/null || true
  rm -f "$CONF_FENETRE" "$REP_ETAT"/cotation-derniere-hauteur "$REP_ETAT"/cotation-dernier-resume
  rm -f "$REP_ETAT"/froid-* 2>/dev/null || true
  systemctl daemon-reload
  systemctl restart coinbosa-watchdog.timer
  systemctl restart coinbosa-journal.timer

  # On PROUVE le retour à l'état d'origine plutôt que de l'annoncer.
  # Format réel de la propriété, relevé sur la machine :
  #   { OnUnitActiveUSec=2min ; next_elapse=... }
  # Ce n'est PAS une valeur en microsecondes : un test qui chercherait
  # « 120000000 » ne correspondrait jamais et annoncerait un échec à chaque
  # désactivation réussie.
  local P
  P=$(systemctl show coinbosa-watchdog.timer -p TimersMonotonic --value 2>/dev/null)
  echo "    coinbosa-watchdog.timer : $P"
  case "$P" in
    *OnUnitActiveUSec=2min*) echo "    ✓ période revenue à 2 min (120 s)" ;;
    *) echo "    ✗ période NON revenue à 2 min — vérifier : systemctl cat coinbosa-watchdog.timer" >&2 ;;
  esac
  if [ -e "$DROPIN_WD" ] || [ -e "$DROPIN_JR" ]; then
    echo "    ✗ un drop-in subsiste : $DROPIN_WD $DROPIN_JR" >&2
  else
    echo "    ✓ aucun drop-in résiduel"
  fi
  if systemctl list-unit-files coinbosa-cotation.timer >/dev/null 2>&1; then
    echo "    ✗ coinbosa-cotation.timer encore connue de systemd" >&2
  else
    echo "    ✓ coinbosa-cotation.timer retirée"
  fi
  echo "    ✓ $BIN_ALERTE et $CONF_CANAL sont CONSERVÉS (voie humaine réutilisable)."
  echo "      Pour les retirer aussi : rm -f $BIN_ALERTE $CONF_CANAL"
  echo ""
  echo "    La sonde de base coinbosa-watchdog (120 s) et l'arrêt propre planifié"
  echo "    restent en place : ils n'ont jamais été désactivés."
}

etat() {
  set +e   # un relevé d'état ne doit jamais s'interrompre parce qu'une pièce manque
  echo "=== Fenêtre de cotation ==="
  if [ -r "$CONF_FENETRE" ]; then
    cat "$CONF_FENETRE" | sed 's/^/    /'
    local F; F=$(sed -n 's/^fin=//p' "$CONF_FENETRE" | tr -cd '0-9')
    echo "    reste : $(( ( ${F:-0} - $(date +%s) ) / 3600 )) h"
  else
    echo "    inactive"
  fi
  echo "=== Voie humaine ==="
  if [ -r "$CONF_CANAL" ]; then echo "    transport : $(cut -d'|' -f1 "$CONF_CANAL") (secret non affiché)";
  else echo "    AUCUNE"; fi
  echo "=== Minuteries ==="
  systemctl list-timers --no-legend coinbosa-watchdog.timer coinbosa-cotation.timer coinbosa-journal.timer 2>/dev/null | sed 's/^/    /'
  echo "=== Période effective de la sonde de base (2min = état d'origine) ==="
  systemctl show coinbosa-watchdog.timer -p TimersMonotonic --value 2>/dev/null | sed 's/^/    /'
}

epreuve() {
  exige_root
  [ -x "$BIN_ALERTE" ] || poser_alerte_humain
  rm -f "$REP_ETAT"/froid-epreuve
  "$BIN_ALERTE" grave epreuve "EPREUVE MANUELLE" "test de la voie humaine lance depuis le serveur a $(date -u '+%H:%M:%SZ')"
  echo "Message poussé. Regarde le journal ET ton téléphone :"
  journalctl -t coinbosa-cotation --since "-1 min" --no-pager | tail -3
}

# ===========================================================================
#  LISTE DE CONTRÔLE DU JOUR J — exécutable, pas déclarative
# ===========================================================================
controle() {
  # Un contrôle est un RELEVÉ, pas une exécution qui doit réussir. Sous « set -e »
  # (posé en tête de fichier), un « grep -c » qui ne trouve rien — donc un simple
  # zéro, l'information cherchée — tue le script au milieu de la liste. Constaté :
  # le premier essai s'arrêtait net après la section E. On désarme ici, et ici
  # seulement ; le verdict est rendu par le compteur $KO, pas par le code de sortie
  # de la dernière commande.
  set +e
  local KO=0 OK=0 NA=0
  # v <libellé> <critère chiffré> <valeur mesurée> <verdict 0|1>
  # Le verdict est CALCULÉ avant l'appel, jamais évalué ici. Une chaîne de test
  # passée à eval() se casse dès qu'une valeur mesurée contient un guillemet —
  # et un test cassé se lit comme un succès. C'est précisément le faux vert
  # qu'on cherche à éliminer.
  v() {
    case "${4:-}" in
      1)  printf '  [ OK ] %-44s %s\n' "$1" "$3"; OK=$((OK+1)) ;;
      na) printf '  [ n/a] %-44s %s\n' "$1" "$3"; NA=$((NA+1)) ;;
      *)  printf '  [ KO ] %-44s %s\n' "$1" "$3"
          printf '         %-44s attendu : %s\n' "" "$2"; KO=$((KO+1)) ;;
    esac
  }
  # comparaison décimale sans bc — bc N'EST PAS installé sur la machine
  # (vérifié : « command -v bc » ne rend rien). Un test qui s'appuierait dessus
  # renverrait 0 et passerait toujours.
  inf() { awk -v a="$1" -v b="$2" 'BEGIN{exit !(a+0 < b+0)}' && echo 1 || echo 0; }
  sup() { awk -v a="$1" -v b="$2" 'BEGIN{exit !(a+0 >= b+0)}' && echo 1 || echo 0; }
  jrpc() { curl -s -X POST "$RPC" -H 'content-type: application/json' -d "$1" -m "${2:-25}" 2>/dev/null; }

  echo "===================================================================="
  echo " CONTRÔLE COINBOSA — $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
  echo " RPC : $RPC"
  echo "===================================================================="

  # ---------------------------------------------------------------- A ------
  echo; echo "-- A. La chaîne vit et avance (vue publique) --"
  local H1 H2 N1 N2 D
  H1=$(jrpc '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' | sed -n 's/.*"result":"\([^"]*\)".*/\1/p')
  if [ -z "$H1" ]; then
    v "hauteur lisible" "un nombre" "AUCUNE RÉPONSE" 0
    N2=0
  else
    sleep 12
    H2=$(jrpc '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' | sed -n 's/.*"result":"\([^"]*\)".*/\1/p')
    [ -z "$H2" ] && H2="$H1"
    N1=$(( 16#${H1#0x} )); N2=$(( 16#${H2#0x} )); D=$(( N2 - N1 ))
    v "hauteur avance en 12 s" ">= 2 blocs (5 s/bloc)" "$N1 -> $N2  (+$D)" "$( [ "$D" -ge 2 ] && echo 1 || echo 0 )"
  fi

  # ---------------------------------------------------------------- B ------
  echo; echo "-- B. Identité et cohérence (ce que la bourse enregistre) --"
  local CID NID SYN SYNV
  CID=$(jrpc '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}'  | sed -n 's/.*"result":"\([^"]*\)".*/\1/p')
  NID=$(jrpc '{"jsonrpc":"2.0","id":1,"method":"net_version","params":[]}'  | sed -n 's/.*"result":"\([^"]*\)".*/\1/p')
  SYN=$(jrpc '{"jsonrpc":"2.0","id":1,"method":"eth_syncing","params":[]}')
  SYNV=$(printf '%s' "$SYN" | sed -n 's/.*"result":\([^,}]*\).*/\1/p')
  v "eth_chainId"  "$CHAIN_ID_ATTENDU" "${CID:-vide}" "$( [ "$CID" = "$CHAIN_ID_ATTENDU" ] && echo 1 || echo 0 )"
  v "net_version"  "$NET_ID_ATTENDU"   "${NID:-vide}" "$( [ "$NID" = "$NET_ID_ATTENDU" ] && echo 1 || echo 0 )"
  v "eth_syncing"  "false"             "${SYNV:-vide}" "$( [ "$SYNV" = "false" ] && echo 1 || echo 0 )"

  # ---------------------------------------------------------------- C ------
  echo; echo "-- C. Latence de la porte publique (mesurée D'ICI) --"
  local LF LC
  LF=$(curl -s -o /dev/null -w '%{time_starttransfer}' -X POST "$RPC" -H 'content-type: application/json' \
       -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' -m 30 2>/dev/null)
  LC=$(curl -s -o /dev/null -w '%{time_starttransfer}' -X POST "$RPC" -H 'content-type: application/json' \
       -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' -m 30 2>/dev/null)
  v "latence, connexion neuve"  "< 2,000 s" "${LF:-?} s" "$(inf "${LF:-99}" 2.0)"
  v "latence, second appel"     "< 1,000 s" "${LC:-?} s" "$(inf "${LC:-99}" 1.0)"
  echo "         Lancée SUR le serveur, cette mesure mesure le serveur, pas le chemin."
  echo "         Le chiffre honnête vient de :  bash 72-surveillance-cotation.sh dehors"
  echo "         (relevé depuis un poste à 210 ms de RTT : neuve 652 ms, réutilisée méd. 216 ms)"

  # ---------------------------------------------------------------- D ------
  echo; echo "-- D. Profondeur d'historique --"
  local GEN GOK T0 DL CODE DEB BAL_NOW BAL_OLD
  GEN=$(jrpc '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x0",false]}' 30)
  GOK=$(printf '%s' "$GEN" | grep -c '"number":"0x0"')
  v "bloc de genèse lisible" "présent" "$( [ "$GOK" -ge 1 ] && echo "bloc 0 servi" || echo "ABSENT" )" "$( [ "$GOK" -ge 1 ] && echo 1 || echo 0 )"
  if [ "${N2:-0}" -gt 0 ]; then
    DEB=$(( N2 > 5000 ? N2 - 4999 : 1 ))
    T0=$(date +%s)
    CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$RPC" -H 'content-type: application/json' \
      -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_getLogs\",\"params\":[{\"fromBlock\":\"$(printf '0x%x' "$DEB")\",\"toBlock\":\"$(printf '0x%x' "$N2")\"}]}" -m 40 2>/dev/null)
    DL=$(( $(date +%s) - T0 ))
    v "eth_getLogs sur 5000 blocs" "HTTP 200 en < 8 s" "HTTP ${CODE} en ${DL} s" \
      "$( [ "$CODE" = 200 ] && [ "$DL" -lt 8 ] && echo 1 || echo 0 )"
  else
    v "eth_getLogs sur 5000 blocs" "HTTP 200 en < 8 s" "hauteur inconnue" na
  fi
  BAL_NOW=$(jrpc '{"jsonrpc":"2.0","id":1,"method":"eth_getBalance","params":["0x3986d6b31ec55043ceaaf25f5ddea53517cbba50","latest"]}')
  v "eth_getBalance latest" "un résultat" "$(printf '%s' "$BAL_NOW" | sed -n 's/.*"result":"\([^"]*\)".*/\1/p' | head -c 24)" \
    "$( printf '%s' "$BAL_NOW" | grep -q '"result"' && echo 1 || echo 0 )"
  BAL_OLD=$(jrpc '{"jsonrpc":"2.0","id":1,"method":"eth_getBalance","params":["0x3986d6b31ec55043ceaaf25f5ddea53517cbba50","0x1"]}')
  echo "  [constat] état au bloc 1 : $(printf '%s' "$BAL_OLD" | head -c 92)"
  echo "            --gcmode full : l'ÉTAT ancien n'est pas conservé. À DÉCLARER, pas à corriger."

  # ---------------------------------------------------------------- E ------
  echo; echo "-- E. Le piège fail2ban (le défaut le plus probable d'une cotation) --"
  if command -v fail2ban-client >/dev/null 2>&1; then
    local BR BS IPP IGN
    BR=$(fail2ban-client status caddy-rpc    2>/dev/null | sed -n 's/.*Banned IP list:[[:space:]]*//p')
    BS=$(fail2ban-client status caddy-status 2>/dev/null | sed -n 's/.*Banned IP list:[[:space:]]*//p')
    v "aucune IP bannie sur caddy-rpc"    "liste vide" "«${BR}»" "$( [ -z "${BR// /}" ] && echo 1 || echo 0 )"
    v "aucune IP bannie sur caddy-status" "liste vide" "«${BS}»" "$( [ -z "${BS// /}" ] && echo 1 || echo 0 )"
    IPP=$(ip -4 addr show scope global 2>/dev/null | sed -n 's/.*inet \([0-9.]*\).*/\1/p' | head -1)
    IGN=$(fail2ban-client get caddy-rpc ignoreip 2>/dev/null | grep -c "${IPP:-@@}")
    v "IP publique du serveur en liste blanche" "présente" "${IPP:-inconnue}" "$( [ "${IGN:-0}" -ge 1 ] && echo 1 || echo 0 )"
    echo "         Débannir une bourse :  fail2ban-client set caddy-rpc unbanip <IP>"
    echo "         La blanchir           :  fail2ban-client set caddy-rpc addignoreip <IP>"
  else
    v "prisons fail2ban" "interrogeables" "fail2ban-client absent" na
  fi

  # ---------------------------------------------------------------- F ------
  echo; echo "-- F. Débit /rpc et marge avant bannissement (seuil 1500/min) --"
  if [ -r /var/log/caddy/explorer-access.log ]; then
    tail -c 4000000 /var/log/caddy/explorer-access.log 2>/dev/null | python3 -c '
import sys,json,collections,time
now=time.time(); c=collections.Counter(); tot=0
for l in sys.stdin:
    try: o=json.loads(l)
    except Exception: continue
    if now-o.get("ts",0)>60: continue
    r=o.get("request",{}); tot+=1
    if str(r.get("uri","")).startswith("/rpc"): c[r.get("remote_ip")]+=1
print(f"  total requetes vues sur la derniere minute : {tot}")
if not c: print("  aucune requete /rpc sur la derniere minute")
for ip,n in c.most_common(6):
    m="[ KO ]" if n>=900 else "[ OK ]"
    print(f"  {m} {ip:20s} {n:5d} req/min   marge avant ban x{1500/max(n,1):.1f}")
' 2>/dev/null || echo "  [ n/a] analyse du journal impossible"
    echo "         Référence 30/08 : 274 req/min au total, 88 req/min pour l'IP la plus active."
  else
    v "journal Caddy lisible" "/var/log/caddy/explorer-access.log" "illisible" na
  fi

  # ---------------------------------------------------------------- G ------
  echo; echo "-- G. Ressources (le validateur partage ces cœurs) --"
  local A B IDLE MEM DISK
  A=$(awk '/^cpu /{t=0; for(i=2;i<=NF;i++) t+=$i; print t" "$5}' /proc/stat)
  sleep 3
  B=$(awk '/^cpu /{t=0; for(i=2;i<=NF;i++) t+=$i; print t" "$5}' /proc/stat)
  IDLE=$(awk -v a="$A" -v b="$B" 'BEGIN{split(a,x," ");split(b,y," ");
        dt=y[1]-x[1]; di=y[2]-x[2]; printf "%.1f", (dt>0? di*100/dt : 0)}')
  MEM=$(free -m | awk '/^Mem:/{print $7}')
  DISK=$(df --output=pcent / | tail -1 | tr -cd '0-9')
  v "CPU inactif (mesuré sur 3 s)" ">= 60 %"    "${IDLE} %"  "$(sup "$IDLE" 60)"
  v "mémoire disponible"           ">= 4000 Mo" "${MEM} Mo"  "$(sup "${MEM:-0}" 4000)"
  v "occupation disque /"          "< 85 %"     "${DISK} %"  "$(inf "${DISK:-100}" 85)"
  echo "         Référence 27/08 (machine saine) : 97,3 % inactif. Relevé 30/08 : 73,2 %,"
  echo "         un « grep -r / » d'audit oublié consommait un cœur entier."
  echo "  processus les plus gourmands :"
  ps -eo pcpu,etimes,user,args --sort=-pcpu --no-headers 2>/dev/null | head -4 | cut -c1-104 | sed 's/^/    /'

  # ---------------------------------------------------------------- H ------
  echo; echo "-- H. Services et voies d'alerte --"
  local s
  for s in coinbosa-validator coinbosa-node caddy coinbosa-watchdog.timer coinbosa-journal.timer; do
    v "$s" "active" "$(systemctl is-active "$s" 2>/dev/null)" \
      "$( systemctl is-active --quiet "$s" && echo 1 || echo 0 )"
  done
  local PANNE
  PANNE=$(journalctl -t coinbosa-watchdog --since '-24 h' --no-pager 2>/dev/null | grep -c 'CANAL D ALERTE HORS SERVICE' || true)
  v "canal Sentry, pannes sur 24 h" "0" "${PANNE:-?}" "$( [ "${PANNE:-1}" -eq 0 ] && echo 1 || echo 0 )"
  v "voie humaine configurée" "un transport" \
    "$( [ -r "$CONF_CANAL" ] && cut -d'|' -f1 "$CONF_CANAL" || echo AUCUNE )" \
    "$( [ -r "$CONF_CANAL" ] && echo 1 || echo 0 )"
  echo "         Prouver la voie humaine :  sudo bash 72-surveillance-cotation.sh epreuve   (puis REGARDER le téléphone)"

  # ---------------------------------------------------------------- I ------
  echo; echo "-- I. Certificat TLS --"
  local FC RC
  FC=$(echo | timeout 15 openssl s_client -connect "$DOMAINE:443" -servername "$DOMAINE" 2>/dev/null \
       | openssl x509 -noout -enddate 2>/dev/null | cut -d= -f2)
  if [ -n "$FC" ]; then
    RC=$(( ( $(date -d "$FC" +%s 2>/dev/null || echo 0) - $(date +%s) ) / 86400 ))
    v "certificat $DOMAINE" ">= 21 jours" "$RC jours (jusqu'au $FC)" "$( [ "$RC" -ge 21 ] && echo 1 || echo 0 )"
  else
    v "certificat $DOMAINE" "lisible" "illisible" 0
  fi

  # ---------------------------------------------------------------- J ------
  echo; echo "-- J. À DÉCLARER à la bourse (constats, pas pannes) --"
  echo "     · Pas de WebSocket. eth_subscribe -> « notifications not supported », /ws -> 404."
  echo "       Substitut vérifié fonctionnel : eth_newFilter + eth_getFilterChanges."
  echo "     · web3_clientVersion et txpool_* absents (le nœud n'expose que eth,net)."
  echo "     · eth_getLogs : plage maximale 5000 blocs (~6 h 56 min à 5 s/bloc). Paginer."
  echo "     · Pas d'état historique (--gcmode full) : eth_getBalance à un vieux bloc échoue."
  echo "     · GET /rpc renvoie 405 et compte dans la prison caddy-status (60 réponses"
  echo "       4xx/min = 1 h de bannissement). Une sonde de santé doit faire un POST."
  echo "     · Maintenance quotidienne : redémarrage propre des deux nœuds vers 04:17 UTC,"
  echo "       23 à 74 s d'indisponibilité mesurées sur les six derniers jours."

  echo
  echo "===================================================================="
  echo " BILAN : $OK conforme(s), $KO en défaut, $NA non mesurable(s)"
  echo "===================================================================="
  set -e
  [ "$KO" -eq 0 ]
}

# ===========================================================================
#  MESURE DEPUIS L'EXTÉRIEUR — à lancer sur un poste QUI N'EST PAS le serveur
# ===========================================================================
# Une latence mesurée sur la machine mesure la machine, pas le chemin. Une
# bourse est ailleurs : elle paie la résolution DNS, le trajet réseau et la
# poignée TLS. Cette commande est la seule qui donne le chiffre honnête, et
# elle ne touche à rien : ce sont des lectures HTTPS.
dehors() {
  local U="${1:-$RPC}"
  echo "Mesure depuis CE poste vers $U — 15 appels, connexion réutilisée après le premier."
  python3 - "$U" <<'PY'
import subprocess,sys,statistics
U=sys.argv[1]
w="%{time_starttransfer} %{time_appconnect} %{http_code}\n"
one=["-s","-o","/dev/null","-w",w,"-m","30","-X","POST",U,
     "-H","content-type: application/json",
     "-d",'{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}']
cmd=["curl"]+one
for _ in range(14): cmd+=["--next"]+one
out=subprocess.run(cmd,capture_output=True,text=True).stdout.split()
t=[float(out[i]) for i in range(0,len(out),3)]
a=[float(out[i+1]) for i in range(0,len(out),3)]
c=[out[i+2] for i in range(0,len(out),3)]
froid=[x for x,y in zip(t,a) if y>0]; chaud=[x for x,y in zip(t,a) if y==0]
print(f"  codes HTTP        : {sorted(set(c))}  (attendu ['200'])")
if froid: print(f"  connexion NEUVE   : {min(froid)*1000:.0f} ms   (critere : < 2000 ms)")
if chaud: print(f"  connexion REUTIL. : min {min(chaud)*1000:.0f} / med {statistics.median(chaud)*1000:.0f} / max {max(chaud)*1000:.0f} ms   (critere : med < 1000 ms)")
print("  Reference relevee le 30/08/2026 depuis un poste a ~210 ms de RTT :")
print("    neuve 917 ms (jusqu'a 2332 ms) — reutilisee med 227 ms, max 498 ms.")
print("    Le serveur lui-meme ne consomme que ~17 ms : le reste est le reseau.")
PY
}

case "$ACTION" in
  on|activer)   activer "$@" ;;
  off|arret)    desactiver ;;
  etat|status)  etat ;;
  canal)        ecrire_canal "${1:-}" ;;
  epreuve|test) epreuve ;;
  controle|check) controle ;;
  dehors)       dehors "${1:-}" ;;
  *) sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//' ;;
esac

# ---------------------------------------------------------------------------
# DÉCISION : FAUT-IL SUSPENDRE L'ARRÊT PROPRE DE 04:17 UTC PENDANT LA COTATION ?
# ---------------------------------------------------------------------------
# NON. On le garde. Voici le calcul, avec les chiffres relevés.
#
# CE QU'IL COÛTE, MESURÉ. Sur les six derniers passages (journalctl -u
# coinbosa-journal.service), l'unité a duré 23 s, 50 s, 60 s, 70 s, 73 s et
# 74 s. Pendant ce temps le nœud RPC redémarre d'abord — le relais /rpc renvoie
# une erreur — puis le validateur — la production de blocs s'interrompt. Soit,
# au pire, environ 75 s par jour d'indisponibilité partielle, une fois par
# vingt-quatre heures.
#
# CE QU'IL RAPPORTE, MESURÉ. Il borne la perte d'état en cas d'arrêt BRUTAL à
# une journée de blocs. Sans lui, la borne dérive : le 19 août, le journal du
# validateur datait de neuf jours, soit environ 144 700 blocs exposés. Sept
# jours de cotation sans arrêt propre, ce sont ~121 000 blocs (7 × 86400 / 5)
# qu'un SIGKILL de l'hyperviseur ferait rembobiner.
#
# LA COMPARAISON EST SANS APPEL. D'un côté 75 s d'indisponibilité par jour,
# annonçables. De l'autre, la possibilité qu'une chaîne cotée rembobine une
# semaine : les dépôts déjà crédités par la bourse disparaissent, les retraits
# déjà honorés aussi. Ce n'est plus un incident technique, c'est la fin de la
# cotation et probablement du jeton. On ne prend pas ce risque pour économiser
# une minute par jour.
#
# CE QU'ON CHANGE QUAND MÊME, ET POURQUOI. Le minuteur porte
# RandomizedDelaySec=300 : l'heure réelle a dérivé de 04:17:13 à 04:20:58 sur
# six jours. Une maintenance qu'on annonce à une place d'échange doit tomber à
# la minute annoncée, sinon leur supervision l'enregistre comme une panne. Le
# « drop-in » posé par ce script met cet aléa à zéro, et rien d'autre. « off »
# le retire.
#
# CE QU'IL FAUT FAIRE EN PLUS, ET QUI N'EST PAS DU RESSORT D'UN SCRIPT :
# déclarer à la bourse une fenêtre de maintenance quotidienne de 04:15 à
# 04:25 UTC. Une indisponibilité annoncée n'est pas un incident.
#
# ---------------------------------------------------------------------------
# CE QUE CE DISPOSITIF COÛTE À LA MACHINE — chiffres mesurés, pas estimés
# ---------------------------------------------------------------------------
# Mesures faites le 30/08/2026 sur la machine (4 vCPU, 16 Go) :
#   · geth attach --exec eth.blockNumber : wall 0,11-0,12 s, CPU 0,12-0,13 s,
#     pic mémoire 80 Mo. La sonde de base en fait quatre par passage.
#   · sonde TLS (openssl s_client)        : wall 0,03 s, CPU ~0,05 s.
#   · appel curl vers le RPC public       : wall 0,03 s, CPU ~0,02 s.
#   -> coût d'un passage de la sonde de base : ~0,65 s de CPU.
#   -> coût d'un passage de la sonde de cotation : ~0,25 s de CPU (8 appels
#      HTTP courts, une lecture de journal, aucun geth attach).
#
# BUDGET, RAPPORTÉ À UN CŒUR :
#   sonde de base à 120 s : 0,65/120 = 0,54 % d'un cœur   (existant)
#   sonde de base à  30 s : 0,65/30  = 2,17 % d'un cœur
#   sonde de cotation 30 s: 0,25/30  = 0,83 % d'un cœur
#   AJOUT NET             : +2,46 % d'un cœur, soit +0,62 % de la machine.
#
# METTRE CE CHIFFRE EN FACE DU RESTE. Relevé le 30/08 : %user moyen 24,1 %,
# inactif 73,2 % (sar). Le 27/08, avant l'incident décrit plus bas : %user
# 1,14 %, inactif 97,3 %. Autrement dit la chaîne entière — deux geth, Caddy,
# fail2ban, la sonde actuelle — coûte 1,14 % de quatre cœurs. Le renforcement
# proposé ajoute 0,62 % d'un cœur, soit 0,16 % de la machine.
#
# CE QUI COÛTE VRAIMENT, ET QU'IL FAUT ARRÊTER D'ABORD. Un processus
# « grep -rls -e coinbosa-secrets -e validator-password / » lancé le 28 août à
# 21:25:36 par une session d'audit tourne encore. Il est descendu dans /proc
# (l'option --exclude-dir=/proc ne filtre pas un chemin absolu) et lit
# /proc/kcore, qui est sans fin : 453 To déjà lus (rchar). Compteurs du noyau
# relevés à dix secondes d'intervalle : 933 tics utilisateur + 67 tics système,
# soit 10,0 s de CPU pour 10 s écoulées — UN CŒUR PLEIN, en continu, depuis plus
# de quarante-huit heures, sur la machine qui héberge l'unique validateur.
# La preuve croisée est dans sar : 97,3 % d'inactivité le 27, 72,3 % le 29.
# Ce seul processus coûte QUARANTE FOIS ce que coûte tout le renforcement
# proposé ici. Il n'écrit rien (wchar=0, write_bytes=0) et ne peut donc pas
# corrompre l'état ; il ne consomme que du CPU et de la bande passante mémoire.
# Le tuer est sans risque pour la chaîne et rend 25 % de la machine :
#     kill 1326248 1326247        # à vérifier avec ps avant, le PID peut changer
#
# AUTRES POSTES, MESURÉS :
#   · Mémoire : la sonde de base pointe à 80 Mo (geth attach, séquentiel) ;
#     disponible relevé : 14 907 Mo. Sans effet.
#   · Disque : journald occupe 370 Mo, / est à 4 % de 193 Go. Le passage à 30 s
#     ajoute ~2900 passages/jour × ~300 o de journal = moins de 1 Mo/jour.
#   · Réseau et fail2ban : les sondes interrogent le RPC PAR L'EXTÉRIEUR, donc
#     leur trafic est compté par la prison caddy-rpc avec l'IP publique de la
#     machine. À 30 s, cela fait ~12 requêtes/min pour un seuil de 1500/min :
#     marge ×125. Le script met malgré tout cette IP en liste blanche, parce
#     qu'un auto-bannissement rendrait les deux sondes aveugles en même temps.
#   · Entrées/sorties disque : les sondes ne lisent que des sockets, du /proc et
#     la queue d'un journal déjà en cache. Le service porte IOSchedulingClass=idle
#     et CPUWeight=20 : en cas de contention, le validateur passe devant.
# ---------------------------------------------------------------------------
