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
#   coinbosa-telegram-alerte signaler  <niveau> <titre> <detail>   # un ETAT
#   coinbosa-telegram-alerte noter     <niveau> <titre> <detail>   # un EVENEMENT
#   coinbosa-telegram-alerte maintenir <titre>                     # « toujours la »
#   coinbosa-telegram-alerte passe-finie
#
# Un incident qui dure ne doit pas produire une notification toutes les dix
# minutes : on prévient à la première occurrence, on rappelle toutes les six
# heures, et on prévient du RETOUR À LA NORMALE — c'est l'information la plus
# attendue quand on a été réveillé pour rien.
#
# UN ETAT N'EST PAS UN EVENEMENT, ET LES CONFONDRE CASSE LES DEUX
# ---------------------------------------------------------------
# `signaler` decrit un ETAT que la sonde RETESTE a chaque passe : « le disque est
# plein », « le service est arrete ». Tant qu il dure il est resignale ; le jour ou
# il cesse de l etre, c est qu il est resolu, et on le dit.
#
# `noter` decrit un EVENEMENT deja passe au moment ou on l apprend : le battement
# quotidien, un rembobinage, une fenetre de maintenance depassee. Il ne sera JAMAIS
# resignale — il n y a plus rien a retester.
#
# Faire passer un evenement par `signaler` produit exactement ce qui s est vu le
# 6 septembre 2026 a 00:00:35 : le battement quotidien — le message dont le seul
# role est de dire que TOUT VA BIEN — est parti en 🔴 « Premiere occurrence », puis
# a ete annonce « resolu, duree 0 min » deux minutes plus tard. Sur le rembobinage
# le meme defaut serait grave : la chaine forke, le canal alerte, et deux minutes
# apres il annonce que c est rentre dans l ordre.
set -uo pipefail
JETON=/etc/coinbosa-telegram-token
ETAT=/var/lib/coinbosa-alertes
CANAL="${TELEGRAM_CANAL:-@Coinbosaofficial}"
RAPPEL=${COINBOSA_RAPPEL_SEC:-21600}      # six heures

# LE JETON NE DOIT PAS PASSER PAR LA LIGNE DE COMMANDE.
#
# /proc/<pid>/cmdline est en -r--r--r-- : lisible par TOUT compte de la machine.
# Mesure du 8 septembre 2026, sur la production, avec un jeton factice :
#
#   AVANT   le compte `coinbosa` lit : JETON-SECRET-123
#   APRES   il lit : RIEN
#           ligne vue : curl -sS --config - --max-time 6 -o /dev/null
#
# `coinbosa` est le compte qui fait tourner le noeud RPC EXPOSE A INTERNET. Ce
# jeton parle au nom du projet : qui l'obtient peut publier « voici l'adresse
# officielle » dans le canal, et c'est ainsi qu'on vide des portefeuilles.
#
# La parade : l'URL — seule partie qui porte le secret — arrive par l'ENTREE
# STANDARD, via `curl --config -`. Le reste des options peut rester en clair.
envoyer() {  # $1 = texte
  local t
  t=$(cat "$JETON" 2>/dev/null | tr -d '\r\n') || return 0
  [ -n "$t" ] || return 0
  # Forme attendue d'un jeton BotFather : <chiffres>:<alphanumerique>. On la
  # verifie avant usage — un fichier corrompu produirait sinon une URL malformee,
  # et un jeton contenant un guillemet casserait le fichier de configuration.
  case "$t" in
    *[!0-9A-Za-z:_-]*|'') logger -t coinbosa-telegram "jeton de forme inattendue — envoi refuse"; return 1 ;;
  esac
  # On ne journalise JAMAIS le corps de la réponse : il peut contenir le jeton.
  local code
  code=$(printf 'url = "https://api.telegram.org/bot%s/sendMessage"\n' "$t" \
    | curl -sS --config - --max-time 15 -o /dev/null -w '%{http_code}' \
    -X POST \
    --data-urlencode "chat_id=$CANAL" \
    --data-urlencode "text=$1" \
    --data-urlencode "disable_web_page_preview=true" 2>/dev/null || echo 000)
  if [ "$code" != 200 ]; then
    logger -t coinbosa-telegram "envoi refuse par Telegram (HTTP $code)"
    return 1
  fi
  return 0
}

cle() { printf '%s' "$1" | md5sum | cut -c1-16; }   # une empreinte par TITRE

case "${1:-}" in
  signaler)
    niveau="${2:-error}"; titre="${3:-sans titre}"; detail="${4:-}"
    # Un « info » n a pas d etat a suivre : ni rappel, ni resolution. On le
    # reoriente ICI plutot que de faire confiance a l appelant — c est ce mauvais
    # aiguillage, et lui seul, qui a fait partir le battement quotidien en rouge.
    if [ "$niveau" = info ]; then exec "$0" noter "$niveau" "$titre" "$detail"; fi
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
  noter)
    # Un evenement : un seul message, aucun etat cree, aucune resolution a venir.
    niveau="${2:-info}"; titre="${3:-sans titre}"; detail="${4:-}"
    case "$niveau" in info) icone='ℹ️' ;; *) icone='🟠' ;; esac
    envoyer "$icone Coinbosa — $titre
$detail"
    ;;
  maintenir)
    # « Cet etat est toujours la, mais je ne notifie pas maintenant. »
    # Sans cette branche, taire un incident pendant la fenetre de maintenance
    # reviendrait a le declarer resolu a la fin de la passe : le faux vert exact
    # que ce depot traque partout ailleurs.
    k=$(cle "${2:-sans titre}")
    if [ -f "$ETAT/actives/$k" ]; then : > "$ETAT/passe/$k"; fi
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
  *) echo "usage: $0 {signaler|noter} <niveau> <titre> <detail> | maintenir <titre> | passe-finie" >&2; exit 1 ;;
esac
AIDE_FIN
chmod 0700 "$AIDE"
ok "aide installée : $AIDE"

# --- essai : on n'installe rien de plus, on prouve juste le canal ------------
if [ "$ESSAI" = 1 ]; then
  [ -s "$JETON" ] || ko "aucun jeton dans $JETON — rien à essayer"
  # NE PAS annoncer « envoyé » sans le verifier. La premiere version de ce
  # script le faisait : elle imprimait OK que Telegram ait accepte ou non, et un
  # jeton invalide passait pour un canal fonctionnel. C'est exactement le faux
  # vert que ce depot traque partout ailleurs — il n'a pas sa place ici non plus.
  t=$(tr -d '\r\n' < "$JETON")
  case "$t" in *[!0-9A-Za-z:_-]*|'') ko "le jeton contient un caractere inattendu — ne pas l utiliser" ;; esac
  # Meme parade que dans l'aide : l'URL passe par l'entree standard, jamais par
  # la ligne de commande, que tout compte de la machine peut lire dans /proc.
  rep=$(printf 'url = "https://api.telegram.org/bot%s/sendMessage"\n' "$t" \
    | curl -sS --config - --max-time 15 -w '\n%{http_code}' \
    -X POST \
    --data-urlencode "chat_id=$CANAL" \
    --data-urlencode "text=Coinbosa — essai du canal d'alerte. Si vous lisez ceci, la supervision peut vous joindre." \
    --data-urlencode "disable_web_page_preview=true" 2>/dev/null || echo $'\n000')
  code=$(printf '%s' "$rep" | tail -1)
  if [ "$code" = 200 ]; then
    ok "Telegram a ACCEPTE le message (HTTP 200) — il doit etre dans $CANAL"
  else
    # On n'affiche que la description, jamais le corps entier : il peut porter le jeton.
    desc=$(printf '%s' "$rep" | head -n -1 | python3 -c "import json,sys;print(json.load(sys.stdin).get('description',''))" 2>/dev/null || echo '')
    printf '    \033[31mECHEC\033[0m Telegram a REFUSE (HTTP %s) %s\n' "$code" "$desc"
    [ "$code" = 401 ] && echo "    -> le jeton est invalide."
    [ "$code" = 400 ] && echo "    -> le bot est-il ADMINISTRATEUR de $CANAL ?"
    exit 1
  fi
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
#    Le MODE suit la nature de l'alerte, que la sonde connait et nous transmet :
#    un ETAT peut etre resolu plus tard, un EVENEMENT non. Envoyer un evenement
#    par `signaler` le ferait annoncer « resolu » a la passe suivante.
ancre = '  logger -t coinbosa-watchdog -p "$prio" "[$niveau] $titre — $detail"'
assert s.count(ancre) == 1, "ancre de la fonction alerte introuvable ou multiple"
s = s.replace(ancre, ancre + '\n'
  '  # Porte aussi l alerte dans Telegram. Le | true est deliberе : si Telegram\n'
  '  # est injoignable, la supervision NE DOIT PAS s arreter pour autant.\n'
  '  local mode=signaler; [ "$nature" = ponctuel ] && mode=noter\n'
  '  /usr/local/bin/coinbosa-telegram-alerte "$mode" "$niveau" "$titre" "$detail" 2>/dev/null || true', 1)

# 1 bis. pendant la maintenance on ne notifie pas — mais se taire n'est pas
#    guerir. Sans cette ligne, la fin de passe annoncerait « resolu » un incident
#    qui dure encore, simplement parce qu'on a choisi de ne pas en parler.
ancre2 = '    logger -t coinbosa-watchdog "pendant maintenance, non remonte : $2 — $3"'
assert s.count(ancre2) == 1, "ancre de alerte_transitoire introuvable ou multiple"
s = s.replace(ancre2, ancre2 + '\n'
  '    # Ne pas notifier ne veut pas dire que c est rentre dans l ordre : sans cette\n'
  '    # ligne, la fin de passe annoncerait « resolu » un incident qui dure encore.\n'
  '    /usr/local/bin/coinbosa-telegram-alerte maintenir "$2" 2>/dev/null || true', 1)

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
  # Un jeton en 644 est lisible par tout compte de la machine. Ce jeton publie au
  # nom du projet : on ne se contente pas de le SIGNALER, on le corrige.
  avant=$(stat -c '%a' "$JETON")
  chmod 600 "$JETON"; chown root:root "$JETON"
  if [ "$avant" = 600 ]; then ok "jeton présent, droits 600"
  else ok "jeton présent — droits resserrés de $avant à 600 (il était lisible par d'autres comptes)"; fi
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
