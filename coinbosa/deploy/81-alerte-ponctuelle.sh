#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — un événement n'est pas un état, et la sonde doit le savoir.
#
#   sudo bash 81-alerte-ponctuelle.sh            # applique
#   sudo ANNULER=1 bash 81-alerte-ponctuelle.sh  # remet la version précédente
#
# CE QUI S'EST PASSÉ — 6 septembre 2026, 00:00:35 UTC
# ---------------------------------------------------
# Le canal Telegram a publié une alerte ROUGE :
#
#     🔴 Coinbosa — battement quotidien
#     chaine a 508362 — supervision operationnelle
#     Première occurrence. Prochain rappel dans 6 h si ça dure.
#
# puis, deux minutes plus tard :
#
#     🟢 Coinbosa — résolu : battement quotidien
#     Durée : 0 min.
#
# La chaîne n'avait rien. Au même instant : validateur, nœud public et nœud
# d'archive tous les trois au même bloc, quatre services actifs, disque à 4 %.
# Le battement quotidien est précisément le message dont le seul rôle est de
# dire QUE TOUT VA BIEN — et il est parti en rouge.
#
# LA CAUSE : DEUX CHOSES DIFFÉRENTES PASSAIENT PAR LE MÊME TUYAU
# ---------------------------------------------------------------
# L'aide Telegram sait suivre un ÉTAT : elle prévient à la première occurrence,
# rappelle toutes les six heures, et annonce la résolution quand l'état cesse
# d'être signalé. Ce mécanisme est juste — pour un état.
#
# Or la sonde y faisait passer aussi des ÉVÉNEMENTS : des faits déjà accomplis
# au moment où on les apprend, qui ne seront jamais re-signalés parce qu'il n'y
# a plus rien à retester. Un événement enregistré comme un état est donc, à la
# passe suivante, déclaré « résolu ».
#
# Trois appels sur onze étaient dans ce cas :
#
#   · battement quotidien           — celui qui a crié
#   · REMBOBINAGE DETECTE           — le plus grave
#   · fenetre de maintenance depassee
#
# LE VRAI DANGER N'EST PAS LE BATTEMENT, C'EST LE REMBOBINAGE
# -----------------------------------------------------------
# Sur une chaîne à un seul producteur, un rembobinage est l'incident qu'on
# n'a pas le droit de manquer. Avec le défaut ci-dessus, il partait en 🔴 —
# puis DEUX MINUTES PLUS TARD le canal annonçait « résolu : REMBOBINAGE
# DETECTE, durée 0 min ». L'opérateur réveillé la nuit lisait que c'était
# rentré dans l'ordre, alors que la chaîne avait forké.
#
# Un canal d'alerte qui dément ses propres alarmes est pire que pas d'alerte :
# la première fois on se lève, la deuxième on n'y croit plus.
#
# CE QUE CE SCRIPT CHANGE, ET RIEN D'AUTRE
# ----------------------------------------
#   1. alerte() reçoit la NATURE de ce qu'elle porte : un état, ou un
#      événement ponctuel. Les trois appels ci-dessus sont marqués.
#   2. Un « info » est journalisé en daemon.info, plus en daemon.err. Le
#      battement quotidien n'apparaît plus parmi les erreurs de la machine.
#   3. Pendant la fenêtre de maintenance, un incident tu n'est plus déclaré
#      résolu : on le maintient explicitement. Ne pas en parler n'est pas
#      guérir — c'était un faux vert en attente.
#   4. On installe enfin le garde-fou « RPC public INCOHERENT », écrit dans
#      50-monitoring.sh mais JAMAIS déployé : la sonde en production ne l'a
#      pas. Il refuse une hauteur de bloc qui ne serait pas « 0x »+hexadécimal,
#      avant de la réinjecter dans le corps JSON de la requête suivante.
#
# Ce script ne touche NI la chaîne, NI les nœuds, NI Caddy. La sonde est un
# script sans état lancé par un timer : le pire cas est une passe manquée.
#
# PRÉALABLE — l'aide doit connaître « noter » et « maintenir »
# ------------------------------------------------------------
# Elle est installée par 77-alerte-telegram.sh. Ce script le VÉRIFIE et refuse
# de continuer sinon, plutôt que d'installer une sonde qui appellerait une
# commande inexistante.
# ---------------------------------------------------------------------------
set -euo pipefail

CHIEN=/usr/local/bin/coinbosa-watchdog
AIDE=/usr/local/bin/coinbosa-telegram-alerte
ANNULER="${ANNULER:-0}"

ok() { printf '    \033[32mOK\033[0m    %s\n' "$1"; }
ko() { printf '    \033[31mECHEC\033[0m %s\n' "$1"; exit 1; }
[ "$(id -u)" = 0 ] || { echo "À lancer en root (sudo)." >&2; exit 1; }

if [ "$ANNULER" = 1 ]; then
  sauv=$(ls -1t "$CHIEN".avant-ponctuel-* 2>/dev/null | head -1)
  [ -n "$sauv" ] || { echo "Aucune sauvegarde à restaurer." >&2; exit 1; }
  cp -a "$sauv" "$CHIEN"; chmod 0755 "$CHIEN"
  bash -n "$CHIEN" || ko "la sauvegarde restaurée ne compile pas"
  echo "==> Version précédente restaurée depuis $sauv"
  exit 0
fi

echo "==> Préalables"
[ -f "$CHIEN" ] || ko "sonde introuvable ($CHIEN) — lancer 50-monitoring.sh d'abord"
[ -f "$AIDE" ]  || ko "aide Telegram introuvable ($AIDE) — lancer 77-alerte-telegram.sh d'abord"
grep -q '^  noter)'     "$AIDE" || ko "l'aide ne connaît pas « noter » — relancer : sudo bash 77-alerte-telegram.sh"
grep -q '^  maintenir)' "$AIDE" || ko "l'aide ne connaît pas « maintenir » — relancer : sudo bash 77-alerte-telegram.sh"
ok "l'aide Telegram connaît « noter » et « maintenir »"

# La correction est idempotente, mais les PREUVES tournent a chaque fois : un
# script qui se declare « deja fait » sans rien verifier est un faux vert.
DEJA=0
grep -q 'nature="${4:-etat}"' "$CHIEN" && DEJA=1

if [ "$DEJA" = 1 ]; then
  ok "la sonde porte déjà la correction — on passe directement aux preuves"
else
  cp -a "$CHIEN" "$CHIEN.avant-ponctuel-$(date +%F-%H%M)"
  ok "sauvegarde : $CHIEN.avant-ponctuel-$(date +%F-%H%M)"

echo
echo "==> Correction"
python3 - "$CHIEN" <<'PY'
import pathlib, sys
p = pathlib.Path(sys.argv[1]); s = p.read_text()

def un(vieux, neuf, quoi):
    global s
    assert s.count(vieux) == 1, f"ancre absente ou multiple : {quoi}"
    s = s.replace(vieux, neuf, 1)
    print(f"    · {quoi}")

# 1. alerte() apprend la NATURE de ce qu'elle porte, et cesse de journaliser
#    un « info » comme une erreur.
un('alerte() {  # $1=niveau  $2=titre  $3=détail\n'
   '  local niveau="$1" titre="$2" detail="${3:-}"\n'
   '  logger -t coinbosa-watchdog -p daemon.err "[$niveau] $titre — $detail"',
   '# $4 vaut « ponctuel » pour un EVENEMENT — deja passe au moment ou on l apprend :\n'
   '# battement quotidien, rembobinage, fenetre de maintenance depassee — et reste vide\n'
   '# pour un ETAT que la passe suivante retestera. La distinction n est pas cosmetique :\n'
   '# c est elle qui decide si l alerte pourra etre annoncee « resolue » plus tard. Un\n'
   '# etat se resout ; un evenement, lui, a simplement eu lieu.\n'
   'alerte() {  # $1=niveau  $2=titre  $3=détail  [$4=ponctuel]\n'
   '  local niveau="$1" titre="$2" detail="${3:-}" nature="${4:-etat}"\n'
   '  # Un « info » n est pas une erreur. Le journaliser en daemon.err faisait\n'
   '  # apparaitre le battement quotidien — la preuve que tout va bien — au milieu des\n'
   '  # erreurs de la machine.\n'
   '  local prio=daemon.err; [ "$niveau" = info ] && prio=daemon.info\n'
   '  logger -t coinbosa-watchdog -p "$prio" "[$niveau] $titre — $detail"',
   'alerte() : nature de l alerte + priorite de journal')

# 2. le mode d'envoi suit cette nature.
un('  /usr/local/bin/coinbosa-telegram-alerte signaler "$niveau" "$titre" "$detail" 2>/dev/null || true',
   '  local mode=signaler; [ "$nature" = ponctuel ] && mode=noter\n'
   '  /usr/local/bin/coinbosa-telegram-alerte "$mode" "$niveau" "$titre" "$detail" 2>/dev/null || true',
   'envoi Telegram : signaler pour un etat, noter pour un evenement')

# 3. se taire pendant la maintenance n'est pas guerir.
un('    logger -t coinbosa-watchdog "pendant maintenance, non remonte : $2 — $3"',
   '    logger -t coinbosa-watchdog "pendant maintenance, non remonte : $2 — $3"\n'
   '    # Ne pas notifier ne veut pas dire que c est rentre dans l ordre : sans cette\n'
   '    # ligne, la fin de passe annoncerait « resolu » un incident qui dure encore.\n'
   '    /usr/local/bin/coinbosa-telegram-alerte maintenir "$2" 2>/dev/null || true',
   'maintenance : l incident tu reste actif, il n est pas declare resolu')

# 4. les trois evenements ponctuels.
un('"le temoin $TEMOIN a expire depuis $((maintenant - ${fin:-$maintenant}))s — l arret propre n a pas termine"',
   '"le temoin $TEMOIN a expire depuis $((maintenant - ${fin:-$maintenant}))s — l arret propre n a pas termine" ponctuel',
   'ponctuel : fenetre de maintenance depassee')
un('alerte fatal "REMBOBINAGE DETECTE" "hauteur passee de $precedent a $hv — fork probable"',
   'alerte fatal "REMBOBINAGE DETECTE" "hauteur passee de $precedent a $hv — fork probable" ponctuel',
   'ponctuel : REMBOBINAGE DETECTE')
un('alerte info "battement quotidien" "chaine a $(hauteur "$VAL_IPC" "$VAL_USER") — supervision operationnelle"',
   'alerte info "battement quotidien" "chaine a $(hauteur "$VAL_IPC" "$VAL_USER") — supervision operationnelle" ponctuel',
   'ponctuel : battement quotidien')

# 5. le garde-fou ecrit dans le depot et jamais deploye.
un('if [ -z "${hex_h:-}" ]; then\n'
   '  alerte_transitoire error "RPC public muet" "eth_blockNumber sans reponse sur https://$DOMAINE/rpc"\n'
   'else\n'
   '  t0=$(date +%s)\n',
   '# --- filtre de confiance sur ce que le RPC vient de repondre -----------------\n'
   '# ACCIDENT EVITE. La valeur ci-dessus ne vient pas de nous : elle vient du\n'
   '# reseau, et elle repart aussitot dans le CORPS JSON de la requete suivante\n'
   '# (eth_getLogs, plus bas), entre guillemets doubles. Un relais casse, un nœud\n'
   '# compromis ou un intermediaire qui repondrait\n'
   '#     {"result":"0x1\\",\\"toBlock\\":\\"0x0"}\n'
   '# ne casse pas la requete : il la REECRIT. La plage interrogee devient vide,\n'
   '# eth_getLogs repond en quelques millisecondes, et la sonde conclut « index des\n'
   "# journaux en bonne sante » alors que l'index est mort. C'est exactement le\n"
   '# faux vert du 12 aout 2026 — six jours de chaine illisible sans une alerte —\n'
   '# mais fabrique a la demande et invisible dans le journal.\n'
   '#\n'
   "# Une quantite JSON-RPC, c'est « 0x » suivi d'AU MOINS un chiffre hexadecimal.\n"
   "# Tout le reste n'est pas une hauteur de bloc : c'est une panne du RPC. On la\n"
   "# dit, et surtout on ne s'en sert pas.\n"
   'hex_ok=0\n'
   'case "${hex_h:-}" in\n'
   '  0x|0x*[!0-9a-fA-F]*) ;;   # « 0x » tout seul, ou un caractere hors hexadecimal\n'
   '  0x*)                 hex_ok=1 ;;\n'
   'esac\n'
   '\n'
   'if [ -z "${hex_h:-}" ]; then\n'
   '  alerte_transitoire error "RPC public muet" "eth_blockNumber sans reponse sur https://$DOMAINE/rpc"\n'
   'elif [ "$hex_ok" = 0 ]; then\n'
   '  # Voix pleine, pas alerte_transitoire : un redemarrage produit du SILENCE,\n'
   "  # jamais une quantite malformee. Ce defaut-la n'a aucune excuse transitoire,\n"
   '  # le taire pendant la fenetre de maintenance rendrait la sonde aveugle.\n'
   '  alerte error "RPC public INCOHERENT" \\\n'
   '    "eth_blockNumber a repondu <${hex_h:0:40}> la ou une quantite 0x... est attendue — valeur REFUSEE sans etre utilisee ; verifier le relais Caddy et le nœud RPC"\n'
   'else\n'
   '  t0=$(date +%s)\n'
   '  # $hex_h est ici PROUVE « 0x »+hexadecimal par le filtre ci-dessus : il ne peut\n'
   "  # plus contenir de guillemet, donc plus reecrire le JSON qui l'entoure.\n",
   'garde-fou « RPC public INCOHERENT » (jamais deploye jusqu ici)')

p.write_text(s)
PY
fi

bash -n "$CHIEN" || ko "la sonde ne compile plus — restaurer : sudo ANNULER=1 bash 81-alerte-ponctuelle.sh"
ok "la sonde compile"

echo
echo "==> Épreuve — la sonde tourne pour de vrai"
# Une sauvegarde ne vaut que si le remplaçant marche. On l'exerce maintenant,
# pas à la prochaine minute pendant qu'on a le dos tourné.
if "$CHIEN"; then ok "passe complète exécutée, code 0"
else ko "la sonde a échoué — restaurer : sudo ANNULER=1 bash 81-alerte-ponctuelle.sh"; fi

echo
echo "==> Preuve — le battement quotidien ne crée plus d'incident"
# On rejoue le battement dans un état ISOLÉ : la production n'est pas touchée.
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
sed "s#^ETAT=.*#ETAT=$T/etat#" "$AIDE" > "$T/aide"
python3 - "$T/aide" <<'PY'
import re, sys, pathlib
p = pathlib.Path(sys.argv[1]); s = p.read_text()
# On neutralise l'envoi pour VOIR le message sans le publier dans le canal.
s = re.sub(r"envoyer\(\) \{.*?\n\}\n",
           'envoyer() { echo "    | $1" | head -1; return 0; }\n', s, flags=re.S)
p.write_text(s)
PY
chmod +x "$T/aide"; mkdir -p "$T/etat/actives" "$T/etat/passe"

"$T/aide" signaler info "battement quotidien" "chaine saine"
"$T/aide" passe-finie
restants=$(ls -1 "$T/etat/actives" 2>/dev/null | wc -l)
[ "$restants" = 0 ] || ko "le battement a laissé $restants incident(s) actif(s)"
ok "aucun incident actif créé — donc aucune fausse « résolution » à venir"

# Et l'inverse doit rester vrai : un ETAT, lui, doit bien se résoudre.
# Deux passes, parce que le mecanisme en demande deux : un etat ENCORE signale
# doit RESTER actif — c est exactement ce qui empeche la fausse resolution.
"$T/aide" signaler error "disque presque plein" "92% utilises"
[ -n "$(ls -1 "$T/etat/actives")" ] || ko "un état n'a PAS été enregistré — la déduplication est cassée"
"$T/aide" passe-finie
[ -n "$(ls -1 "$T/etat/actives")" ] || ko "un état ENCORE signalé a été déclaré résolu — faux vert"
ok "un état encore signalé reste actif — aucune fausse résolution"
"$T/aide" passe-finie
[ -z "$(ls -1 "$T/etat/actives")" ] || ko "un état qui a cessé n'a pas été déclaré résolu"
ok "un état qui cesse est annoncé résolu — le mécanisme utile est intact"

# Pendant la maintenance : on ne notifie pas, mais on ne guérit pas non plus.
"$T/aide" signaler error "service arrete" "coinbosa-node"
"$T/aide" maintenir "service arrete"
"$T/aide" passe-finie
[ -n "$(ls -1 "$T/etat/actives")" ] || ko "un incident maintenu a été déclaré résolu — faux vert de maintenance"
ok "un incident tu pendant la maintenance reste actif"

echo
echo "==> Retour arrière : sudo ANNULER=1 bash 81-alerte-ponctuelle.sh"
