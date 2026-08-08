#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — VALIDATEUR (produit les blocs).
#
#   sudo bash 40-validator.sh
#
# Processus SÉPARÉ du nœud RPC, volontairement.
# ---------------------------------------------------------------------------
# Pourquoi un processus distinct
# ------------------------------
# Pour sceller, geth doit garder la clé DÉVERROUILLÉE en mémoire. Si le même
# processus servait aussi le RPC public, toute faille du JSON-RPC — ou une méthode
# comme eth_sendTransaction, qui utilise justement le compte déverrouillé — donnerait
# à un inconnu la capacité de signer avec la clé du validateur.
#
# Ici le validateur n'ouvre AUCUN port HTTP ni WebSocket. Il ne parle qu'à la boucle
# locale, en P2P, avec le nœud RPC. La surface publique reste celle du nœud sans clé.
#
# Le jour où le validateur déménage sur sa propre machine, seul le pair local change.
# ---------------------------------------------------------------------------
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "à lancer en root (sudo bash 40-validator.sh)" >&2; exit 1; }

REPO="${REPO:-/opt/coinbosa-chain}"
GENESIS="${GENESIS:-$REPO/coinbosa/genesis/genesis-coinbosa.json}"
DATADIR="${DATADIR:-/var/lib/coinbosa/validator}"
CHAIN_ID="${CHAIN_ID:-26262}"
P2P_PORT="${P2P_PORT:-30304}"
GETH="$REPO/build/bin/geth"

[ -x "$GETH" ] || { echo "ERREUR : $GETH introuvable — compiler d'abord (make geth)." >&2; exit 1; }
[ -f "$GENESIS" ] || { echo "ERREUR : genesis introuvable : $GENESIS" >&2; exit 1; }
[ -f "$DATADIR/pw.txt" ] || { echo "ERREUR : $DATADIR/pw.txt introuvable (mot de passe de la clé)." >&2; exit 1; }

# Utilisateur DÉDIÉ au validateur, distinct de celui du nœud RPC.
# Séparer les processus ne suffit pas : tant que les deux tournent sous le même compte,
# une compromission du nœud exposé à Internet donne la lecture du keystore ET du mot de
# passe du validateur — donc la clé qui produit les blocs. La séparation des identités
# est ce qui rend la séparation des processus réellement utile.
VAL_USER="${VAL_USER:-coinbosa-val}"
id "$VAL_USER" >/dev/null 2>&1 || useradd --system --home /var/lib/coinbosa --shell /usr/sbin/nologin "$VAL_USER"
# Le parent doit rester TRAVERSABLE par les deux comptes, sinon le validateur ne peut
# meme pas atteindre son propre repertoire. Les donnees sensibles sont protegees par
# le 0700 des sous-dossiers, pas par le parent.
chmod 0755 "$(dirname "$DATADIR")"
chown -R "$VAL_USER":"$VAL_USER" "$DATADIR"
chmod 0700 "$DATADIR"
echo "==> Utilisateur du validateur : $VAL_USER (le nœud RPC ne peut pas lire ce répertoire)"

# L'adresse du validateur est lue depuis le keystore : on ne la code jamais en dur,
# et on refuse de démarrer si le keystore est vide ou en contient plusieurs.
mapfile -t KEYS < <(ls "$DATADIR/keystore/" 2>/dev/null | grep '^UTC--' || true)
[ "${#KEYS[@]}" -eq 1 ] || { echo "ERREUR : ${#KEYS[@]} clé(s) dans $DATADIR/keystore, exactement 1 attendue." >&2; exit 1; }
VALIDATOR="0x${KEYS[0]##*--}"
echo "==> Validateur : $VALIDATOR"

# Garde : cette adresse doit être celle inscrite dans l'extraData du genesis, sinon le
# moteur attend un signataire différent et la chaîne n'avance pas.
EXTRA=$(python3 -c "import json;print(json.load(open('$GENESIS'))['extraData'])")
# extraData = 0x | 32 octets de vanity | 1 octet compteur | 20 octets d'adresse | 48 BLS | 65 sceau
# En caractères hex depuis le début de la chaîne : 2 ("0x") + 64 (vanity) + 2 (compteur) = 68.
INSCRIT="0x${EXTRA:68:40}"
if [ "${INSCRIT,,}" != "${VALIDATOR,,}" ]; then
  echo "ERREUR : le validateur du keystore ($VALIDATOR) n'est pas celui du genesis ($INSCRIT)." >&2
  echo "  Le réseau attendrait des blocs signés par une clé absente de cette machine." >&2
  exit 1
fi
echo "    conforme à l'extraData du genesis ✓"

echo "==> Initialisation (au premier lancement seulement)"
if [ ! -d "$DATADIR/geth/chaindata" ]; then
  sudo -u "$VAL_USER" "$GETH" init --datadir "$DATADIR" "$GENESIS" 2>&1 | grep -i "genesis block hash" || true
else
  echo "    déjà initialisé"
fi

echo "==> Service systemd"
# Aucun --http, aucun --ws : ce processus n'est joignable que par IPC (fichier local)
# et par P2P sur la boucle locale. --netrestrict borne les pairs à localhost tant que
# le validateur et le nœud RPC partagent la machine.
cat > /etc/systemd/system/coinbosa-validator.service <<UNIT
[Unit]
Description=Coinbosa Chain — validateur (production de blocs)
After=network-online.target
Wants=network-online.target

[Service]
User=$VAL_USER
Group=$VAL_USER
ExecStart=$GETH \\
  --datadir $DATADIR \\
  --networkid $CHAIN_ID \\
  --port $P2P_PORT \\
  --nodiscover \\
  --netrestrict 127.0.0.0/8 \\
  --mine \\
  --miner.etherbase $VALIDATOR \\
  --unlock $VALIDATOR \\
  --password $DATADIR/pw.txt \\
  --allow-insecure-unlock \\
  --syncmode full --gcmode full \\
  --verbosity 3
Restart=on-failure
RestartSec=5
# ARRÊT PROPRE — indispensable. geth garde l'état récent en mémoire et ne l'écrit sur
# disque qu'à l'arrêt (ou périodiquement). Tué trop vite, il redémarre sur un état
# incomplet : « Unclean shutdown detected », puis rembobinage — jusqu'au genesis dans le
# pire des cas. Le validateur reminerait alors une chaîne concurrente.
# geth traite SIGINT comme une demande d'arrêt propre et vide son état ; on lui laisse
# largement le temps de le faire.
KillSignal=SIGINT
TimeoutStopSec=300
SendSIGKILL=yes
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$DATADIR
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable coinbosa-validator
systemctl restart coinbosa-validator

echo "==> Vérification de la production de blocs"
sleep 8
systemctl is-active --quiet coinbosa-validator || {
  echo "ERREUR : le validateur n'est pas actif." >&2
  journalctl -u coinbosa-validator -n 25 --no-pager >&2
  exit 1
}

# On ne se fie pas au statut du service : on exige que la hauteur AVANCE réellement.
h1=$(sudo -u "$VAL_USER" "$GETH" attach --exec 'eth.blockNumber' "$DATADIR/geth.ipc" 2>/dev/null || echo "")
echo "    hauteur observée : ${h1:-?}"
for i in $(seq 1 12); do
  sleep 6
  h2=$(sudo -u "$VAL_USER" "$GETH" attach --exec 'eth.blockNumber' "$DATADIR/geth.ipc" 2>/dev/null || echo "")
  if [ -n "${h2:-}" ] && [ -n "${h1:-}" ] && [ "$h2" -gt "$h1" ] 2>/dev/null; then
    echo "    la chaîne AVANCE : $h1 -> $h2 ✓"
    echo ""
    echo "==> Validateur en service. enode pour l'appairage local :"
    sudo -u "$VAL_USER" "$GETH" attach --exec 'admin.nodeInfo.enode' "$DATADIR/geth.ipc" 2>/dev/null
    exit 0
  fi
done
echo "ERREUR : aucun bloc produit après ~70 s." >&2
journalctl -u coinbosa-validator -n 30 --no-pager >&2
exit 1
