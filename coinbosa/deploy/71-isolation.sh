#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — cloisonner le nœud RPC public pour qu'il ne puisse jamais affamer
# le validateur.
#
#   sudo bash 71-isolation.sh            # applique
#   sudo ANNULER=1 bash 71-isolation.sh  # retire tout (supprime les fichiers)
#
# POURQUOI CE SCRIPT EXISTE
# -------------------------
# Le validateur et le nœud RPC public tournent sur la MÊME machine, 4 cœurs,
# sans swap, et — mesuré le 31 août 2026 — sans AUCUN cloisonnement :
#
#     systemctl show coinbosa-validator.service -p CPUQuotaPerSecUSec -p MemoryMax
#         CPUQuotaPerSecUSec=infinity   MemoryMax=infinity   CPUWeight=[not set]
#     systemctl show coinbosa-node.service      -p CPUQuotaPerSecUSec -p MemoryMax
#         CPUQuotaPerSecUSec=infinity   MemoryMax=infinity   CPUWeight=[not set]
#
# La chaîne n'a QU'UN validateur. Si le nœud public — celui que n'importe qui
# sur Internet peut solliciter via https://explorer.coinbosa.com/rpc — consomme
# les 4 cœurs, le validateur ne scelle plus. Il n'y a alors aucun second
# producteur pour prendre le relais, et aucune transaction corrective ne peut
# être minée. C'est le risque le plus direct pendant une cotation.
#
# LE PRINCIPE, ET IL EST DISSYMÉTRIQUE
# ------------------------------------
# Le validateur est PRIORISÉ, jamais BORNÉ. Le nœud public est BORNÉ.
# Ce sont deux réglages opposés et les confondre coûterait la chaîne : une
# borne mémoire sur le validateur le ferait TUER par le noyau (pas de swap sur
# cette machine), c'est-à-dire arrêt de production. Le script REFUSE donc
# d'écrire CPUQuota= ou MemoryMax= sur le validateur ; c'est vérifié plus bas
# par une assertion sur le fichier généré, pas par la seule bonne volonté.
#
# CE QUI EST RETENU, ET POURQUOI
# ------------------------------
#   CPUWeight=   part proportionnelle de CPU, et UNIQUEMENT en cas de
#                contention. Quand la machine est libre, elle ne borne rien :
#                le validateur garde les 4 cœurs. C'est exactement « prioriser
#                sans contraindre ». Validateur 10000, nœud 100 : sous
#                saturation le validateur passe 100 fois avant le nœud.
#   CPUQuota=    plafond DUR (cgroup cpu.max). Réservé au nœud : 200 %, soit
#                2 cœurs sur 4. C'est la borne qui garantit que 2 cœurs restent
#                toujours disponibles pour le validateur et le système, quoi
#                que fasse l'Internet public.
#   MemoryHigh=  seuil de FREINAGE : au-delà, le noyau récupère de la mémoire
#                et ralentit le groupe, mais ne tue personne. Sur le nœud, en
#                première ligne, avant la borne dure.
#   MemoryMax=   borne DURE : au-delà, le tueur de mémoire frappe DANS ce
#                groupe. Sur le nœud seulement : le nœud redémarre tout seul
#                (Restart=on-failure) et la chaîne continue de produire.
#   MemoryMin=   PROTECTION contre la récupération (l'inverse d'une borne) :
#   MemoryLow=   le noyau s'interdit de reprendre au validateur la mémoire
#                protégée. C'est le pendant mémoire de CPUWeight.
#
# CE QUI EST ÉCARTÉ, ET POURQUOI
# ------------------------------
#   IOWeight=    INAPPLICABLE sur cette machine, vérifié :
#                  cat /sys/fs/cgroup/cgroup.subtree_control   -> cpu memory pids
#                    (« io » absent : le contrôleur n'est pas délégué)
#                  ls /sys/fs/cgroup/system.slice/coinbosa-node.service/
#                    -> io.pressure seul ; PAS de io.weight
#                  cat /sys/block/sda/queue/scheduler           -> [none] mq-deadline
#                Les poids d'E/S de cgroup v2 exigent BFQ ou le modèle de coût
#                blk-iocost. Avec l'ordonnanceur « none » et sans io.cost
#                configuré, écrire IOWeight= produirait un réglage accepté par
#                systemd et SANS AUCUN EFFET — un faux vert. On ne l'écrit pas.
#                (Mesure du 31 août : /proc/pressure/io full avg10=0.00 —
#                aucune famine d'E/S constatée aujourd'hui.)
#   Nice=        appliqué au moment de l'exec : exigerait un REDÉMARRAGE du
#                validateur. De plus, une fois le contrôleur cpu actif, ce sont
#                les poids de cgroup qui arbitrent ENTRE groupes ; nice n'ordonne
#                que les fils d'un même groupe. Coût réel (un arrêt de
#                production), bénéfice nul : écarté.
#   OOMScoreAdjust= sur le nœud : appliqué à l'exec, exigerait un redémarrage.
#                MemoryMax= obtient le confinement recherché À CHAUD. À poser
#                au prochain redémarrage naturel du nœud, pas avant.
#                (Le validateur porte déjà OOMScoreAdjust=-900 dans son unité.)
#   AllowedCPUs= épinglerait le validateur sur N cœurs : c'est une CONTRAINTE,
#                pas une priorité — il ne pourrait plus emprunter les autres
#                cœurs lors d'une pointe. CPUWeight fait mieux : illimité au
#                repos, prioritaire sous charge. Écarté.
#
# TOUT CE QUI EST RETENU S'APPLIQUE À CHAUD
# -----------------------------------------
#   man systemctl, « set-property » : « The changes are applied immediately,
#   and stored on disk for future boots ».
#   AUCUN redémarrage de service n'est nécessaire, et ce script n'en contient
#   aucun : le validateur ne s'arrête pas une seconde.
#
# EFFET DE BORD À CONNAÎTRE
# -------------------------
# system.slice n'a aujourd'hui que « memory pids » dans son subtree_control ;
# poser un CPUWeight/CPUQuota sur un service qu'il contient fait activer par
# systemd le contrôleur « cpu » sur TOUTE la tranche. Chaque service de
# system.slice gagne alors un fichier cpu.weight (valeur par défaut 100, donc
# comportement inchangé), et la comptabilité CPU par cgroup s'active. C'est
# bénin et réversible, mais ce n'est pas rien : c'est écrit ici pour que
# personne ne le découvre après coup.
#
# RÉVERSIBILITÉ
# -------------
# Rien n'est réécrit : on ne pose que des surcharges dans
# /etc/systemd/system/<unité>.d/71-isolation.conf. `ANNULER=1` les supprime et
# remet les valeurs neutres à chaud. Les unités d'origine ne sont jamais
# touchées.
# ---------------------------------------------------------------------------
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "À lancer en root." >&2; exit 1; }

VAL=coinbosa-validator.service
NOD=coinbosa-node.service
CONF=71-isolation.conf

# --- Bornes. Modifiables ici, et nulle part ailleurs. ----------------------
# Établies sur la consommation RÉELLE mesurée le 31 août 2026 sur 30 s :
#   validateur  199 ms de CPU / 30 000 ms  = 0,66 % d'un cœur, 356 Mio
#   nœud        334 ms de CPU / 30 000 ms  = 1,11 % d'un cœur, 359 Mio
#   machine     4 cœurs, 15 Gio de RAM, 14 Gio disponibles, AUCUN swap
# Les bornes du nœud sont donc à ~180x sa consommation CPU et ~8x sa mémoire :
# elles ne peuvent pas le gêner en régime normal, elles n'existent que pour
# plafonner un emballement.
POIDS_VAL="${POIDS_VAL:-10000}"      # 1..10000 — le maximum
POIDS_NOD="${POIDS_NOD:-100}"        # la valeur par défaut : 100x moins prioritaire
QUOTA_NOD="${QUOTA_NOD:-200%}"       # 2 cœurs sur 4 ; laisse 2 cœurs au reste
MEM_HAUT_NOD="${MEM_HAUT_NOD:-2G}"   # freinage
MEM_MAX_NOD="${MEM_MAX_NOD:-3G}"     # borne dure
MEM_MIN_VAL="${MEM_MIN_VAL:-1G}"     # protection dure du validateur
MEM_BAS_VAL="${MEM_BAS_VAL:-2G}"     # protection souple du validateur
POIDS_USER="${POIDS_USER:-20}"       # cf. plus bas : les sessions SSH

# La protection mémoire n'a d'effet que si les ancêtres en réservent autant.
#   man systemd.resource-control : « For a protection to be effective, it is
#   generally required to set a corresponding allocation on all ancestors ».
# Sans cette ligne sur system.slice, MemoryMin= sur le validateur vaudrait 0 :
# un réglage qui a l'air posé et ne protège rien.
MEM_MIN_SYS="${MEM_MIN_SYS:-1200M}"
MEM_BAS_SYS="${MEM_BAS_SYS:-2400M}"

lire_cg() { cat "/sys/fs/cgroup/$1" 2>/dev/null || echo ABSENT; }
hauteur_val() {
  sudo -u coinbosa-val /opt/coinbosa-chain/build/bin/geth attach --exec 'eth.blockNumber' \
    /var/lib/coinbosa/validator/geth.ipc 2>/dev/null | tr -cd '0-9'
}

# ===========================================================================
# ANNULATION
# ===========================================================================
if [ "${ANNULER:-0}" = "1" ]; then
  echo "==> Retrait des surcharges"
  rm -f "/etc/systemd/system/$VAL.d/$CONF" \
        "/etc/systemd/system/$NOD.d/$CONF" \
        "/etc/systemd/system/system.slice.d/$CONF" \
        "/etc/systemd/system/user.slice.d/$CONF"
  rmdir --ignore-fail-on-non-empty \
        "/etc/systemd/system/$VAL.d" "/etc/systemd/system/$NOD.d" \
        "/etc/systemd/system/system.slice.d" "/etc/systemd/system/user.slice.d" 2>/dev/null || true
  systemctl daemon-reload
  # Supprimer le fichier ne suffit pas : le noyau garde les valeurs déjà
  # écrites dans le cgroup vivant. On les remet explicitement à neutre, à chaud.
  # UNE PROPRIÉTÉ PAR COMMANDE : groupées, un seul rejet (une valeur refusée par
  # cette version de systemd) ferait échouer le lot entier et laisserait des
  # bornes en place — un retour arrière à moitié fait est pire que pas de retour
  # arrière, parce qu'il a l'air d'avoir marché.
  # Les valeurs ci-dessous ne sont pas « des défauts supposés » : ce sont
  # exactement celles relevées avant toute modification (31 août 2026) —
  #   memory.min=0  memory.low=0  memory.high=max  memory.max=max
  #   system.slice/cpu.weight=100   user.slice/cpu.weight=100
  neutraliser() { # $1=unité  $2=propriété=valeur
    systemctl set-property "$1" "$2" 2>/dev/null \
      || echo "    (avertissement : $1 $2 refusé — à vérifier à la main)"
  }
  for P in CPUWeight=100 MemoryMin=0 MemoryLow=0; do neutraliser "$VAL" "$P"; done
  for P in CPUWeight=100 CPUQuota= MemoryHigh=infinity MemoryMax=infinity; do neutraliser "$NOD" "$P"; done
  for P in MemoryMin=0 MemoryLow=0; do neutraliser system.slice "$P"; done
  neutraliser user.slice CPUWeight=100

  # set-property dépose ses propres fichiers 50-*.conf : on les retire aussi,
  # sinon « annuler » laisserait des traces qui ressembleraient à la config.
  # Boucles explicites : l'expansion d'accolades s'évalue AVANT celle des
  # variables, un « {"$VAL",...} » marche par accident et se casse en silence.
  for D in "$VAL" "$NOD" system.slice user.slice; do
    for P in CPUWeight CPUQuota MemoryMin MemoryLow MemoryHigh MemoryMax; do
      rm -f "/etc/systemd/system/$D.d/50-$P.conf"
    done
    rmdir --ignore-fail-on-non-empty "/etc/systemd/system/$D.d" 2>/dev/null || true
  done
  systemctl daemon-reload
  echo "    Retiré. cpu.max du nœud = $(lire_cg system.slice/$NOD/cpu.max)"
  exit 0
fi

# ===========================================================================
# 1. PRÉ-VOL — on refuse d'agir sur un terrain qu'on n'a pas vérifié
# ===========================================================================
echo "==> Pré-vol"

MNT=$(findmnt -no FSTYPE /sys/fs/cgroup || true)
[ "$MNT" = "cgroup2" ] || { echo "    ÉCHEC : /sys/fs/cgroup n'est pas en cgroup v2 ($MNT). Rien n'est appliqué." >&2; exit 1; }
echo "    cgroup v2 ................. oui"
echo "    systemd ................... $(systemctl --version | head -1)"

for u in "$VAL" "$NOD"; do
  systemctl is-active --quiet "$u" || { echo "    ÉCHEC : $u n'est pas actif. On n'ajuste pas ce qui ne tourne pas." >&2; exit 1; }
done
echo "    $VAL ... actif"
echo "    $NOD ....... actif"

CTRL=$(cat /sys/fs/cgroup/cgroup.controllers)
for c in cpu memory; do
  case " $CTRL " in *" $c "*) ;; *) echo "    ÉCHEC : contrôleur « $c » indisponible ($CTRL)." >&2; exit 1;; esac
done
echo "    contrôleurs cpu + memory .. disponibles"

# La hauteur AVANT. C'est le témoin qui servira à prouver, à la fin, que la
# production n'a pas été interrompue. Sans point de départ, la vérification
# finale ne vaudrait rien.
H_AVANT=$(hauteur_val)
[ -n "$H_AVANT" ] || { echo "    ÉCHEC : hauteur du validateur illisible. On n'applique rien à l'aveugle." >&2; exit 1; }
echo "    hauteur du validateur ..... $H_AVANT"

# ===========================================================================
# 2. LES SURCHARGES
# ===========================================================================
echo "==> Écriture des surcharges"

install -d -m 0755 "/etc/systemd/system/$VAL.d"
cat > "/etc/systemd/system/$VAL.d/$CONF" <<UNIT
# Posé par 71-isolation.sh — supprimer ce fichier annule la priorisation.
#
# LE VALIDATEUR EST PRIORISÉ, JAMAIS BORNÉ.
# Aucun CPUQuota=, aucun MemoryMax=, aucun MemoryHigh= ici. Volontairement.
# Sur une machine SANS SWAP, une borne mémoire atteinte = processus tué par le
# noyau = arrêt de la production de blocs, sur une chaîne qui n'a qu'un seul
# producteur. C'est la panne qu'on cherche justement à éviter.
[Service]
# Part de CPU maximale en cas de contention ; aucune limite au repos.
CPUWeight=$POIDS_VAL
StartupCPUWeight=$POIDS_VAL
# Protection — l'inverse d'une borne : le noyau s'interdit de récupérer cette
# mémoire. Mesuré à 356 Mio d'usage réel, $MEM_MIN_VAL laisse de la marge.
MemoryMin=$MEM_MIN_VAL
MemoryLow=$MEM_BAS_VAL
UNIT

install -d -m 0755 "/etc/systemd/system/$NOD.d"
cat > "/etc/systemd/system/$NOD.d/$CONF" <<UNIT
# Posé par 71-isolation.sh — supprimer ce fichier annule le cloisonnement.
#
# LE NŒUD PUBLIC EST BORNÉ. C'est lui qui est exposé à Internet
# (https://explorer.coinbosa.com/rpc) ; c'est donc lui qui peut s'emballer.
[Service]
# 100 contre 10000 : sous saturation, le validateur passe d'abord.
CPUWeight=$POIDS_NOD
StartupCPUWeight=$POIDS_NOD
# Plafond DUR : 2 cœurs sur 4. Quoi qu'il arrive côté RPC, 2 cœurs restent
# libres pour le validateur et le système. Mesuré à 1,11 % d'un cœur en régime
# normal : cette borne est ~180x au-dessus de l'usage réel.
# Arbitrage assumé : sous une charge RPC extrême, le nœud RALENTIT. Un
# explorateur lent se rattrape ; une chaîne arrêtée, non.
CPUQuota=$QUOTA_NOD
# Freinage d'abord (récupération, pas de mort), borne dure ensuite. Si la
# borne dure est atteinte, c'est le NŒUD qui tombe et redémarre seul
# (Restart=on-failure) — le validateur n'est pas concerné.
MemoryHigh=$MEM_HAUT_NOD
MemoryMax=$MEM_MAX_NOD
UNIT

# --- system.slice : sans quoi la protection du validateur vaut zéro ---------
install -d -m 0755 /etc/systemd/system/system.slice.d
cat > "/etc/systemd/system/system.slice.d/$CONF" <<UNIT
# Posé par 71-isolation.sh.
# man systemd.resource-control : « For a protection to be effective, it is
# generally required to set a corresponding allocation on all ancestors ».
# system.slice a memory.min=0 aujourd'hui : sans cette réservation, le
# MemoryMin= du validateur serait plafonné à 0 par son parent — un réglage
# visible dans systemctl show, et sans le moindre effet réel.
[Slice]
MemoryMin=$MEM_MIN_SYS
MemoryLow=$MEM_BAS_SYS
UNIT

# --- user.slice : la menace réellement observée ----------------------------
# Le 31 août 2026, un `grep -rls / ...` lancé le 28 août depuis une session SSH
# consommait 99,9 % d'un cœur DEPUIS 48 HEURES (PID 1326248, cgroup
# user.slice/user-0.slice/session-4066.scope). Un cœur sur quatre, en
# permanence, pour rien.
# Pondérer les deux services entre eux n'aurait RIEN fait contre ça :
# user.slice et system.slice sont frères sous la racine, ils s'arbitrent au
# niveau du dessus. C'est donc ici, et seulement ici, que ce cas se traite.
install -d -m 0755 /etc/systemd/system/user.slice.d
cat > "/etc/systemd/system/user.slice.d/$CONF" <<UNIT
# Posé par 71-isolation.sh.
# Une commande d'administration égarée ne doit pas pouvoir concurrencer la
# production de blocs. 20 et non 1 : on garde délibérément de quoi ouvrir une
# session SSH pour reprendre la main, même machine saturée.
[Slice]
CPUWeight=$POIDS_USER
UNIT

# --- GARDE-FOU : on relit ce qu'on vient d'écrire --------------------------
# Une faute de frappe qui déplacerait CPUQuota= ou MemoryMax= vers le fichier
# du validateur coûterait la chaîne. On ne se fie pas à l'intention : on
# vérifie le fichier réellement posé, et on annule tout s'il est faux.
if grep -Eq '^\s*(CPUQuota|MemoryMax|MemoryHigh)\s*=' "/etc/systemd/system/$VAL.d/$CONF"; then
  echo "    ÉCHEC : une borne dure s'est glissée dans la surcharge du VALIDATEUR." >&2
  rm -f "/etc/systemd/system/$VAL.d/$CONF" "/etc/systemd/system/$NOD.d/$CONF" \
        "/etc/systemd/system/system.slice.d/$CONF" "/etc/systemd/system/user.slice.d/$CONF"
  exit 1
fi
echo "    surcharge validateur ...... sans borne dure (vérifié)"
echo "    4 fichiers posés"

# ===========================================================================
# 3. APPLICATION À CHAUD — sans redémarrer quoi que ce soit
# ===========================================================================
echo "==> Application à chaud (aucun redémarrage)"
systemctl daemon-reload

# On NE SUPPOSE PAS que daemon-reload a poussé les valeurs jusqu'au noyau : on
# lit le cgroup, et on ne force que si c'est nécessaire. `set-property` est
# documenté « applied immediately » (man systemctl).
if [ "$(lire_cg "system.slice/$NOD/cpu.max")" = "ABSENT" ] \
   || [ "$(lire_cg "system.slice/$NOD/cpu.max")" = "max 100000" ]; then
  echo "    daemon-reload n'a pas suffi — application explicite"
  systemctl set-property "$VAL" CPUWeight="$POIDS_VAL" MemoryMin="$MEM_MIN_VAL" MemoryLow="$MEM_BAS_VAL"
  systemctl set-property "$NOD" CPUWeight="$POIDS_NOD" CPUQuota="$QUOTA_NOD" MemoryHigh="$MEM_HAUT_NOD" MemoryMax="$MEM_MAX_NOD"
  systemctl set-property system.slice MemoryMin="$MEM_MIN_SYS" MemoryLow="$MEM_BAS_SYS"
  systemctl set-property user.slice CPUWeight="$POIDS_USER"
else
  echo "    valeurs poussées par daemon-reload"
fi

# ===========================================================================
# 4. VÉRIFICATION — sur le noyau, pas sur l'intention
# ===========================================================================
echo "==> Vérification (valeurs lues dans /sys/fs/cgroup)"

ECHEC=0
attendu() { # $1=chemin cgroup  $2=valeur attendue  $3=libellé
  local v; v=$(lire_cg "$1")
  if [ "$v" = "$2" ]; then printf '    OK    %-34s %s\n' "$3" "$v"
  else printf '    ÉCHEC %-34s lu=%s attendu=%s\n' "$3" "$v" "$2"; ECHEC=1; fi
}
# « refuse » atteste l'ABSENCE de borne sur le validateur. $2 est la valeur que
# porte un cgroup NON borné. « ABSENT » est distingué : ce n'est pas une borne,
# c'est le contrôleur qui n'est pas actif — donc rien n'a été appliqué du tout.
# Confondre les deux afficherait « le validateur est borné » pour un cas qui est
# en réalité « le réglage n'a pas pris » : deux pannes opposées à réparer.
refuse() { # $1=chemin  $2=valeur d'un cgroup NON borné  $3=libellé
  local v; v=$(lire_cg "$1")
  if [ "$v" = "$2" ]; then printf '    OK    %-34s %s (aucune borne)\n' "$3" "$v"
  elif [ "$v" = "ABSENT" ]; then
    printf '    ÉCHEC %-34s contrôleur inactif — le réglage n a PAS pris\n' "$3"; ECHEC=1
  else printf '    ÉCHEC %-34s lu=%s — le validateur NE DOIT PAS être borné\n' "$3" "$v"; ECHEC=1; fi
}

attendu "system.slice/$VAL/cpu.weight"   "$POIDS_VAL"          "validateur cpu.weight"
refuse  "system.slice/$VAL/cpu.max"      "max 100000"          "validateur cpu.max"
refuse  "system.slice/$VAL/memory.max"   "max"                 "validateur memory.max"
refuse  "system.slice/$VAL/memory.high"  "max"                 "validateur memory.high"
attendu "system.slice/$NOD/cpu.weight"   "$POIDS_NOD"          "nœud cpu.weight"
attendu "system.slice/$NOD/cpu.max"      "200000 100000"       "nœud cpu.max (=200%)"
attendu "user.slice/cpu.weight"          "$POIDS_USER"         "user.slice cpu.weight"

echo "    --- protection mémoire (effective, parent compris) ---"
printf '    %-38s %s\n' "system.slice memory.min"  "$(lire_cg system.slice/memory.min)"
printf '    %-38s %s\n' "validateur memory.min"    "$(lire_cg "system.slice/$VAL/memory.min")"
printf '    %-38s %s\n' "nœud memory.high"         "$(lire_cg "system.slice/$NOD/memory.high")"
printf '    %-38s %s\n' "nœud memory.max"          "$(lire_cg "system.slice/$NOD/memory.max")"

# --- LE CRITÈRE QUI COMPTE : la chaîne produit-elle encore ? ---------------
# Toutes les cases vertes ci-dessus ne valent rien si le validateur a cessé de
# sceller. 25 s couvrent 5 intervalles de 5 s ; on exige STRICTEMENT plus de
# 3 blocs, ce qui laisse un bloc de marge d'arrondi sans tolérer un arrêt.
echo "==> Production de blocs (25 s d'observation)"
sleep 25
H_APRES=$(hauteur_val)
DELTA=$(( ${H_APRES:-0} - H_AVANT ))
echo "    hauteur $H_AVANT -> ${H_APRES:-illisible}  (+$DELTA blocs en ~25 s, attendu ~5)"
if [ "${H_APRES:-0}" -le "$H_AVANT" ]; then
  echo "    ÉCHEC : la hauteur n'a PAS progressé strictement." >&2; ECHEC=1
elif [ "$DELTA" -lt 3 ]; then
  echo "    ÉCHEC : $DELTA bloc(s) en 25 s — la production a ralenti." >&2; ECHEC=1
else
  echo "    OK    production nominale"
fi

if [ "$ECHEC" -ne 0 ]; then
  echo
  echo "  !! Au moins un contrôle a échoué. Annuler immédiatement :"
  echo "       sudo ANNULER=1 bash 71-isolation.sh"
  exit 1
fi

echo
echo "==> Cloisonnement en place. Aucun service n'a été redémarré."
echo "    Retour arrière : sudo ANNULER=1 bash 71-isolation.sh"
echo "    Desserrer le nœud si le RPC devient le facteur limitant (à chaud) :"
echo "       sudo systemctl set-property $NOD CPUQuota=300%"
