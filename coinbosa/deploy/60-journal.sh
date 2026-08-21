#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — borner la perte d'état par un arrêt propre planifié.
#
#   sudo bash 60-journal.sh
#
# POURQUOI CE SCRIPT EXISTE
# -------------------------
# Sous le schéma d'état « path », l'état n'atteint le disque qu'à l'aplatissement
# des couches (au-delà de 128) ou à l'arrêt propre du nœud. Il n'existe AUCUN
# vidage périodique en temps : debug_setTrieFlushInterval refuse explicitement ce
# schéma. Et --pathdb.sync ne change rien — mesuré trois fois au banc, la tête
# revenait à 0 avec comme sans le drapeau.
#
# Il en découle une règle unique, et elle a été mesurée :
#
#     perte maximale après un arrêt brutal
#         = les blocs écoulés depuis le dernier ARRÊT PROPRE
#
# Au banc : arrêt propre à la hauteur 31, puis kill -9 à 43 -> reprise à 32.
# Onze blocs perdus, pas quarante-trois. Le fichier merkle.journal n'est pas
# supprimé après lecture : il reste le point de reprise jusqu'au prochain arrêt
# propre.
#
# Sans arrêt propre planifié, cette borne dérive sans limite. Constaté en
# production le 19 août : le journal du validateur datait de NEUF JOURS, soit
# environ 144 700 blocs exposés. Une minuterie quotidienne ramène la borne à
# une journée, pour toujours.
#
# Ce n'est pas un contournement en attendant mieux : c'est le SEUL mécanisme du
# dossier dont l'efficacité a été mesurée, et il ne coûte ni redéploiement, ni
# machine, ni argent.
# ---------------------------------------------------------------------------
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "À lancer en root." >&2; exit 1; }

HEURE="${HEURE:-04:17}"   # heure creuse ; minute décalée pour éviter la cohue des tâches à l'heure ronde

echo "==> Écriture du script d'arrêt propre"
cat > /usr/local/bin/coinbosa-journal <<'SCRIPT'
#!/usr/bin/env bash
# Redémarre proprement les deux nœuds, UN PAR UN, et vérifie que chacun est
# revenu avant de toucher au suivant. Les redémarrer ensemble laisserait la
# chaîne sans nœud pendant la fenêtre — inutile, et c'est exactement le genre de
# détail qui transforme une mesure d'hygiène en incident.
set -u

DSN=$(cat /etc/coinbosa-sentry-dsn 2>/dev/null || true)

alerte() {  # $1=niveau $2=titre $3=détail
  logger -t coinbosa-journal "[$1] $2 — $3"
  [ -n "$DSN" ] || return 0
  proto="${DSN%%://*}"; reste="${DSN#*://}"
  cle="${reste%%@*}"; hote="${reste#*@}"; projet="${hote##*/}"; hote="${hote%%/*}"
  charge=$(printf '{"level":"%s","logger":"coinbosa-journal","platform":"other","server_name":"%s","message":{"formatted":"%s — %s"},"tags":{"composant":"chaine","reseau":"coinbosa"}}' \
    "$1" "$(hostname)" "$2" "$3")
  curl -s -o /dev/null -m 20 -X POST "$proto://$hote/api/$projet/store/" \
    -H "Content-Type: application/json" \
    -H "X-Sentry-Auth: Sentry sentry_version=7, sentry_key=$cle, sentry_client=coinbosa-journal/1.0" \
    --data "$charge" || true
}

journal_de() { stat -c %Y "/var/lib/coinbosa/$1/geth/triedb/merkle.journal" 2>/dev/null || echo 0; }
hauteur_de() {
  local u=coinbosa; [ "$1" = validator ] && u=coinbosa-val
  sudo -u "$u" /opt/coinbosa-chain/build/bin/geth attach --exec 'eth.blockNumber' \
    "/var/lib/coinbosa/$1/geth.ipc" 2>/dev/null | tr -cd '0-9'
}

for R in node validator; do
  SVC="coinbosa-$R"
  systemctl is-active --quiet "$SVC" || { alerte error "noeud deja arrete" "$SVC n etait pas actif — rien a redemarrer"; continue; }

  AVANT_J=$(journal_de "$R")
  AVANT_H=$(hauteur_de "$R")

  # systemctl restart envoie SIGTERM : le nœud persiste son état avant de sortir.
  # Mesuré au banc : « Persisted dirty state to file … merkle.journal ».
  systemctl restart "$SVC"

  # On attend le retour du service ET la reprise de la hauteur. Un service
  # « active » qui ne produit plus de blocs serait un faux vert.
  OK=0
  for i in $(seq 1 30); do
    sleep 2
    systemctl is-active --quiet "$SVC" || continue
    H=$(hauteur_de "$R")
    # STRICTEMENT supérieur : un noeud qui revient « active » mais ne scelle
    # plus rendrait la meme hauteur, et l egalite le ferait passer pour un
    # succes. C est precisement le faux vert que ce controle doit attraper.
    [ -n "${H:-}" ] && [ "${H:-0}" -gt "${AVANT_H:-0}" ] && { OK=1; break; }
  done

  APRES_J=$(journal_de "$R")

  if [ "$OK" -ne 1 ]; then
    alerte fatal "REDEMARRAGE PROPRE ECHOUE" "$SVC n a pas repris apres 60 s (hauteur avant $AVANT_H)"
    # On s'arrête là : toucher au second nœud alors que le premier est mal en
    # point transformerait une gêne en panne totale.
    exit 1
  fi

  # geth journalise « Unclean shutdown detected » au demarrage suivant chaque
  # arret brutal. Quatre avaient eu lieu en production sans que rien ne les
  # remonte : le fait etait dans les journaux, personne ne le lisait.
  BRUT=$(journalctl -u "$SVC" --no-pager -o cat --since "-3 min" 2>/dev/null | grep -c "Unclean shutdown detected" || true)
  if [ "${BRUT:-0}" -gt 0 ]; then
    alerte error "arrets brutaux passes detectes" "$SVC : geth signale ${BRUT} arret(s) non propre(s) dans son historique"
  fi

  if [ "$APRES_J" -le "$AVANT_J" ]; then
    alerte error "journal NON rafraichi" "$SVC : merkle.journal n a pas ete reecrit — la borne de perte ne progresse pas"
  fi
done

logger -t coinbosa-journal "[info] arret propre planifie termine — borne de perte remise a zero"
exit 0
SCRIPT
chmod 0755 /usr/local/bin/coinbosa-journal

echo "==> Unités systemd"
cat > /etc/systemd/system/coinbosa-journal.service <<'UNIT'
[Unit]
Description=Coinbosa — arrêt propre planifié, pour borner la perte d'état.
Documentation=file:///opt/coinbosa-chain/coinbosa/deploy/60-journal.sh
After=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/coinbosa-journal
TimeoutStartSec=300
UNIT

cat > /etc/systemd/system/coinbosa-journal.timer <<UNIT
[Unit]
Description=Coinbosa — déclenche l'arrêt propre quotidien.

[Timer]
OnCalendar=*-*-* ${HEURE}:00
# Si la machine était éteinte à l'heure dite, on rattrape au démarrage : une
# minuterie qui saute silencieusement laisse la borne dériver sans que personne
# ne le voie.
Persistent=true
# Étalement : évite que plusieurs machines d'un même parc redémarrent à la
# seconde près si le déploiement est un jour répliqué.
RandomizedDelaySec=300

[Install]
WantedBy=timers.target
UNIT

systemctl daemon-reload
systemctl enable --now coinbosa-journal.timer

echo "==> État"
systemctl list-timers --no-legend coinbosa-journal.timer | sed 's/^/    /'
echo
echo "  Prochain passage : $(systemctl show coinbosa-journal.timer -p NextElapseUSecRealtime --value)"
echo "  Essai à blanc    : sudo systemctl start coinbosa-journal && journalctl -t coinbosa-journal -n 5"
