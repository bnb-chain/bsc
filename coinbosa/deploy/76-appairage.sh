#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — l'appairage se vérifie par l'IDENTITÉ du pair, pas par son nombre.
#
#   sudo bash 76-appairage.sh            # installe le contrôleur corrigé
#   sudo ANNULER=1 bash 76-appairage.sh  # remet la version précédente
#
# L'INCIDENT QUI A MOTIVÉ CE SCRIPT — 3 septembre 2026
# ----------------------------------------------------
# Le nœud RPC public et le nœud d'archive sont restés DIX-NEUF HEURES figés au
# bloc 459638 pendant que le validateur avançait jusqu'à 473194 : 13 556 blocs
# d'écart. Le point d'accès public a servi des données périmées de dix-neuf
# heures, sans que rien ne se répare.
#
# Les deux nœuds RPC étaient appairés L'UN À L'AUTRE — un îlot isolé — et aucun
# n'était connecté au validateur. Le validateur produisait des blocs que
# personne ne recevait.
#
# LE CONTRÔLEUR AVAIT REGARDÉ, ET S'ÉTAIT DÉCLARÉ SATISFAIT
# ---------------------------------------------------------
# L'ancien /usr/local/bin/coinbosa-peer-check tenait en une ligne de décision :
#
#     n=$(geth attach --exec 'net.peerCount' …)
#     [ "${n:-0}" -gt 0 ] && exit 0
#
# N'IMPORTE QUEL pair le satisfaisait. Quand le nœud d'archive est arrivé, il a
# occupé cette place : peerCount valait 1, le contrôleur sortait content, et
# l'appairage avec le validateur ne s'est jamais rétabli. La sonde d'alerte, elle,
# a bien crié — mais la réparation automatique regardait le mauvais chiffre.
#
# Compter n'est pas vérifier. Un pair n'est pas LE pair.
#
# CE QUE FAIT LA VERSION CORRIGÉE
# -------------------------------
#   · elle relève l'identifiant de nœud du validateur (les 128 caractères
#     hexadécimaux de son enode), et cherche CET identifiant dans admin.peers ;
#   · elle traite les DEUX nœuds RPC — l'ancien n'en connaissait qu'un, le nœud
#     d'archive ayant été ajouté après lui ;
#   · elle n'exige rien du nombre de pairs : deux nœuds appairés entre eux et
#     coupés du validateur sont un ÉCHEC, quel que soit leur compte ;
#   · elle journalise ce qu'elle a fait, et ce qu'elle a trouvé.
# ---------------------------------------------------------------------------
set -euo pipefail

CIBLE=/usr/local/bin/coinbosa-peer-check
ANNULER="${ANNULER:-0}"
[ "$(id -u)" = 0 ] || { echo "À lancer en root (sudo)." >&2; exit 1; }

if [ "$ANNULER" = 1 ]; then
  sauv=$(ls -1t "$CIBLE".avant-* 2>/dev/null | head -1)
  [ -n "$sauv" ] || { echo "Aucune sauvegarde à restaurer." >&2; exit 1; }
  cp -a "$sauv" "$CIBLE"; chmod 0755 "$CIBLE"
  echo "==> Version précédente restaurée depuis $sauv"
  exit 0
fi

[ -f "$CIBLE" ] && cp -a "$CIBLE" "$CIBLE.avant-$(date +%F-%H%M)"

cat > "$CIBLE" <<'CHECK'
#!/usr/bin/env bash
# Maintient l'appairage des nœuds RPC AVEC LE VALIDATEUR. Idempotent.
#
# Ne teste JAMAIS net.peerCount : deux nœuds RPC appairés entre eux et coupés du
# validateur affichent un pair chacun et ne reçoivent plus aucun bloc. C'est
# exactement ce qui a figé la production dix-neuf heures le 3 septembre 2026.
# On cherche l'identifiant DU VALIDATEUR parmi les pairs, rien d'autre.
set -uo pipefail
GETH=/opt/coinbosa-chain/build/bin/geth
VAL_IPC=/var/lib/coinbosa/validator/geth.ipc
VAL_USER=coinbosa-val
NODE_USER=coinbosa

[ -S "$VAL_IPC" ] || exit 0

enode=$("$GETH" attach --exec 'admin.nodeInfo.enode' "$VAL_IPC" 2>/dev/null | tr -d '"')
# Valider la FORME avant usage : en cas d'échec, geth écrit son message d'erreur
# sur la sortie standard, et ce message finirait passé tel quel à addPeer().
case "$enode" in
  enode://*) ;;
  *) logger -t coinbosa "appairage : enode du validateur illisible"; exit 0 ;;
esac

# L'identifiant de nœud : les 128 hexadécimaux entre « enode:// » et « @ ».
id=${enode#enode://}; id=${id%%@*}
[ ${#id} -eq 128 ] || { logger -t coinbosa "appairage : identifiant validateur de longueur ${#id}, attendu 128"; exit 0; }

# Hauteur de reference : celle du validateur, seul producteur.
hv=$("$GETH" attach --exec 'eth.blockNumber' "$VAL_IPC" 2>/dev/null || echo 0)
case "$hv" in ''|*[!0-9]*) hv=0 ;; esac
[ "$hv" -gt 0 ] || { logger -t coinbosa "appairage : hauteur du validateur illisible"; exit 0; }

# Un bloc toutes les 5 s : 12 blocs, c'est une minute de retard. En dessous, ce
# n'est pas un decrochage, c'est le temps de propagation.
SEUIL=${COINBOSA_RETARD_MAX:-12}

for d in node node-archive; do
  ipc="/var/lib/coinbosa/$d/geth.ipc"
  [ -S "$ipc" ] || continue

  # LE BON CRITERE EST « EST-CE QUE JE RECOIS LES BLOCS », PAS « QUI EST MON PAIR ».
  #
  # Premiere version de ce script : exiger que le validateur figure parmi les
  # pairs de CHAQUE noeud. C'etait faux, et mesure sur la production — le noeud
  # d'archive recoit ses blocs VIA le noeud public (validateur -> public ->
  # archive), une topologie parfaitement saine. Le controleur rappelait donc
  # addPeer chaque minute sur un noeud qui n'avait aucun probleme : le defaut
  # miroir de celui qu'on corrige.
  #
  # Ce qui a reellement echoue le 3 septembre, c'est que les noeuds NE
  # RECEVAIENT PLUS RIEN. C'est cela qu'on mesure : le retard sur le validateur.
  h=$(sudo -u "$NODE_USER" "$GETH" attach --exec 'eth.blockNumber' "$ipc" 2>/dev/null || echo 0)
  case "$h" in ''|*[!0-9]*) h=0 ;; esac
  retard=$(( hv > h ? hv - h : 0 ))
  [ "$retard" -le "$SEUIL" ] && continue

  # Il decroche. On regarde alors seulement s'il faut refaire le lien direct.
  vu=$(sudo -u "$NODE_USER" "$GETH" attach \
        --exec "admin.peers.filter(function(p){return p.enode.substr(8,128)=='$id'}).length" "$ipc" 2>/dev/null || echo 0)
  case "$vu" in ''|*[!0-9]*) vu=0 ;; esac
  nb=$(sudo -u "$NODE_USER" "$GETH" attach --exec 'net.peerCount' "$ipc" 2>/dev/null || echo '?')
  sudo -u "$NODE_USER" "$GETH" attach --exec "admin.addPeer(\"$enode\")" "$ipc" >/dev/null 2>&1
  logger -t coinbosa "appairage $d : $retard blocs de retard (bloc $h contre $hv), $nb pair(s), validateur parmi eux : $vu — reconnexion demandée"
done
CHECK

chmod 0755 "$CIBLE"
echo "==> Contrôleur installé : $CIBLE"

# La sauvegarde ne vaut que si le remplaçant marche : on l'exerce tout de suite.
echo "==> Épreuve"
if "$CIBLE"; then echo "    exécution : ok"; else echo "    ECHEC à l'exécution" >&2; exit 1; fi

G=/opt/coinbosa-chain/build/bin/geth
enode=$("$G" attach --exec 'admin.nodeInfo.enode' /var/lib/coinbosa/validator/geth.ipc 2>/dev/null | tr -d '"')
id=${enode#enode://}; id=${id%%@*}
for d in node node-archive; do
  ipc="/var/lib/coinbosa/$d/geth.ipc"
  [ -S "$ipc" ] || continue
  hv=$("$G" attach --exec 'eth.blockNumber' /var/lib/coinbosa/validator/geth.ipc 2>/dev/null || echo 0)
  h=$(sudo -u coinbosa "$G" attach --exec 'eth.blockNumber' "$ipc" 2>/dev/null || echo 0)
  r=$(( hv > h ? hv - h : 0 ))
  if [ "$r" -le 12 ]; then echo "    $d : a jour (bloc $h, validateur $hv)"
  else echo "    $d : $r blocs de retard — reconnexion demandée"; fi
done

echo
echo "==> Retour arrière : sudo ANNULER=1 bash 76-appairage.sh"
