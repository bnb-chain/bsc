#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — porter les alertes de supervision dans Telegram.
#
#   sudo bash 77-alerte-telegram.sh            # installe et branche
#   sudo bash 77-alerte-telegram.sh --essai    # envoie un message de contrôle
#   sudo ANNULER=1 bash 77-alerte-telegram.sh  # débranche
#
# POURQUOI — l'incident du 3 septembre 2026
# -----------------------------------------
# Le nœud RPC public a servi des données périmées pendant DIX-NEUF HEURES. La
# sonde avait crié dès la première minute, et elle a crié toutes les dix minutes
# jusqu'au bout. Personne n'a vu : les alertes partaient vers Sentry, que
# l'éditeur n'ouvre pas plusieurs fois par jour.
#
# Le délai de réaction ne se gagne pas en détectant mieux — la détection
# marchait. Il se gagne en prévenant LÀ OÙ LA PERSONNE REGARDE.
#
# LA DÉDUPLICATION N'EST PAS UN CONFORT, C'EST CE QUI REND L'ALERTE UTILISABLE
# ---------------------------------------------------------------------------
# Ces dix-neuf heures ont produit 177 alertes en deux jours. Les renvoyer telles
# quelles dans Telegram, c'est faire couper les notifications dans l'heure — et
# alors le canal ne sert plus à rien le jour où ça compte vraiment.
#
# On envoie donc :
#   · la PREMIÈRE occurrence d'un incident, tout de suite ;
#   · un rappel toutes les six heures tant qu'il dure, pas plus ;
#   · le RETOUR À LA NORMALE, qui est l'information qu'on attend le plus quand
#     on a été réveillé la nuit ;
#   · le battement quotidien. Un canal muet est ambigu : tout va bien, ou le
#     canal est mort ? Un message par jour tranche.
#
# LE JETON
# --------
# Lu dans /etc/coinbosa-telegram-token, en 0600, root seul. Jamais affiché,
# jamais journalisé, jamais dans le dépôt. Sans lui, ce dispositif reste inerte
# et le watchdog continue exactement comme avant.
# ---------------------------------------------------------------------------
set -euo pipefail

JETON=/etc/coinbosa-telegram-token
AIDE=/usr/local/bin/coinbosa-telegram-alerte
CHIEN=/usr/local/bin/coinbosa-watchdog
ETAT=/var/lib/coinbosa-alertes
CANAL="${TELEGRAM_CANAL:-@Coinbosaofficial}"
ANNULER="${ANNULER:-0}"
ESSAI=0; [ "${1:-}" = "--essai" ] && ESSAI=1

[ "$(id -u)" = 0 ] || { echo "À lancer en root (sudo)." >&2; exit 1; }
ok() { printf '    \033[32mOK\033[0m    %s\n' "$1"; }
ko() { printf '    \033[31mECHEC\033[0m %s\n' "$1"; exit 1; }

# --- débranchement ---------------------------------------------------------
if [ "$ANNULER" = 1 ]; then
  sauv=$(ls -1t "$CHIEN".avant-telegram-* 2>/dev/null | head -1)
  [ -n "$sauv" ] && { cp -a "$sauv" "$CHIEN"; echo "==> Watchdog restauré depuis $sauv"; } \
                 || echo "==> Aucune sauvegarde du watchdog ; il faudra retirer les appels à la main."
  rm -f "$AIDE"
  echo "==> Débranché. Le jeton et l'historique d'alertes sont conservés."
  exit 0
fi

# --- l'aide qui parle à Telegram -------------------------------------------
install -d -m 0700 "$ETAT" "$ETAT/actives" "$ETAT/passe"
cat > "$AIDE" <<'AIDE_FIN'
#!/usr/bin/env bash
# Porte une alerte de supervision dans Telegram, SANS noyer le destinataire.
#
#   coinbosa-telegram-alerte signaler <niveau> <titre> <detail>
#   coinbosa-telegram-alerte passe-finie
#
# Un incident qui dure ne doit pas produire une notification toutes les dix
# minutes : on prévient à la première occurrence, on rappelle toutes les six
# heures, et on prévient du RETOUR À LA NORMALE — c'est l'information la plus
# attendue quand on a été réveillé pour rien.
set -uo pipefail
JETON=/etc/coinbosa-telegram-token
ETAT=/var/lib/coinbosa-alertes
CANAL="${TELEGRAM_CANAL:-@Coinbosaofficial}"
RAPPEL=${COINBOSA_RAPPEL_SEC:-21600}      # six heures

envoyer() {  # $1 = texte
  local t
  t=$(cat "$JETON" 2>/dev/null | tr -d '\r\n') || return 0
  [ -n "$t" ] || return 0
  # On ne journalise JAMAIS le corps de la réponse : il peut contenir le jeton.
  local code
  code=$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' \
    -X POST "https://api.telegram.org/bot$t/sendMessage" \
    --data-urlencode "chat_id=$CANAL" \
    --data-urlencode "text=$1" \
    --data-urlencode "disable_web_page_preview=true" 2>/dev/null || echo 000)
  [ "$code" = 200 ] || logger -t coinbosa-telegram "envoi refuse par Telegram (HTTP $code)"
}

cle() { printf '%s' "$1" | md5sum | cut -c1-16; }   # une empreinte par TITRE

case "${1:-}" in
  signaler)
    niveau="${2:-error}"; titre="${3:-sans titre}"; detail="${4:-}"
    k=$(cle "$titre")
    : > "$ETAT/passe/$k"                              # vu pendant cette passe
    f="$ETAT/actives/$k"
    maintenant=$(date +%s)
    if [ ! -f "$f" ]; then
      printf '%s\n%s\n%s\n' "$titre" "$maintenant" "$maintenant" > "$f"
      envoyer "🔴 Coinbosa — $titre
$detail

Première occurrence. Prochain rappel dans 6 h si ça dure."
    else
      dernier=$(sed -n 3p "$f" 2>/dev/null || echo 0)
      depuis=$(sed -n 2p "$f" 2>/dev/null || echo "$maintenant")
      if [ $(( maintenant - dernier )) -ge "$RAPPEL" ]; then
        sed -i "3s/.*/$maintenant/" "$f" 2>/dev/null
        h=$(( (maintenant - depuis) / 3600 ))
        envoyer "🔴 Coinbosa — $titre (toujours en cours depuis ${h} h)
$detail"
      fi
    fi
    ;;
  passe-finie)
    # Tout incident actif qui n'a PAS été signalé pendant cette passe est resolu.
    for f in "$ETAT"/actives/*; do
      [ -e "$f" ] || continue
      k=$(basename "$f")
      [ -e "$ETAT/passe/$k" ] && continue
      titre=$(sed -n 1p "$f"); depuis=$(sed -n 2p "$f" 2>/dev/null || echo 0)
      duree=$(( ($(date +%s) - depuis) / 60 ))
      rm -f "$f"
      envoyer "🟢 Coinbosa — résolu : $titre
Durée : ${duree} min."
    done
    rm -f "$ETAT"/passe/* 2>/dev/null
    ;;
  *) echo "usage: $0 signaler <niveau> <titre> <detail> | passe-finie" >&2; exit 1 ;;
esac
AIDE_FIN
chmod 0700 "$AIDE"
ok "aide installée : $AIDE"

# --- essai : on n'installe rien de plus, on prouve juste le canal ------------
if [ "$ESSAI" = 1 ]; then
  [ -s "$JETON" ] || ko "aucun jeton dans $JETON — rien à essayer"
  "$AIDE" signaler info "essai du canal" "Si tu lis ceci dans Telegram, le canal d'alerte fonctionne."
  ok "message d'essai envoyé — vérifie $CANAL"
  rm -f "$ETAT"/actives/* "$ETAT"/passe/* 2>/dev/null
  exit 0
fi

# --- branchement dans le watchdog ------------------------------------------
[ -f "$CHIEN" ] || ko "watchdog introuvable ($CHIEN) — lancer 50-monitoring.sh d'abord"
if grep -q 'coinbosa-telegram-alerte' "$CHIEN"; then
  ok "watchdog déjà branché"
else
  cp -a "$CHIEN" "$CHIEN.avant-telegram-$(date +%F-%H%M)"
  python3 - "$CHIEN" <<'PY'
import pathlib, sys
p = pathlib.Path(sys.argv[1]); s = p.read_text()

# 1. chaque alerte part aussi vers Telegram, apres le journal et Sentry.
ancre = '  logger -t coinbosa-watchdog -p daemon.err "[$niveau] $titre — $detail"'
assert s.count(ancre) == 1, "ancre de la fonction alerte introuvable ou multiple"
s = s.replace(ancre, ancre + '\n'
  '  # Porte aussi l alerte dans Telegram. Le | true est deliberе : si Telegram\n'
  '  # est injoignable, la supervision NE DOIT PAS s arreter pour autant.\n'
  '  /usr/local/bin/coinbosa-telegram-alerte signaler "$niveau" "$titre" "$detail" 2>/dev/null || true', 1)

# 2. en fin de passe, on annonce ce qui est revenu a la normale.
assert s.count('\nexit 0\n') >= 1, "fin de script introuvable"
s = s.rstrip('\n')
assert s.endswith('exit 0'), "le script ne se termine pas par exit 0"
s = s[:-len('exit 0')] + (
  '# Ce qui n a pas ete signale pendant cette passe est revenu a la normale :\n'
  '# c est l information qu on attend le plus apres avoir ete reveille.\n'
  '/usr/local/bin/coinbosa-telegram-alerte passe-finie 2>/dev/null || true\n\n'
  'exit 0\n')
p.write_text(s)
print("    watchdog branché : alerte + fin de passe")
PY
  bash -n "$CHIEN" || ko "le watchdog ne compile plus — restaurer la sauvegarde"
  ok "watchdog modifié et syntaxiquement valide"
fi

# --- contrôle ---------------------------------------------------------------
echo "==> Contrôle"
if [ -s "$JETON" ]; then
  ok "jeton présent ($(stat -c '%a' "$JETON") — 600 attendu)"
  [ "$(stat -c '%a' "$JETON")" = 600 ] || echo "    ATTENTION : droits trop larges, faire chmod 600 $JETON"
else
  echo "    Le jeton manque : le dispositif est en place mais INERTE."
  echo "    Une seule commande, à lancer par l'éditeur :"
  echo
  echo "      printf '%s' 'LE_JETON_DE_BOTFATHER' | sudo tee $JETON >/dev/null && sudo chmod 600 $JETON"
  echo
  echo "    Puis :  sudo bash 77-alerte-telegram.sh --essai"
fi
echo
echo "==> Débranchement : sudo ANNULER=1 bash 77-alerte-telegram.sh"
