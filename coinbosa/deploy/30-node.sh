#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — nœud RPC (SANS clé de scellage) sur le serveur web.
#
#   sudo GENESIS=/opt/coinbosa-chain/coinbosa/genesis/genesis-coinbosa.json bash 30-node.sh
#
# Ce nœud SUIT la chaîne et répond aux requêtes de l'explorateur. Il ne produit
# aucun bloc et ne détient AUCUNE clé.
# ---------------------------------------------------------------------------
# Pourquoi ce script ne fait pas tourner de validateur
# ----------------------------------------------------
# La clé de scellage signe un bloc toutes les 5 secondes : elle doit être déverrouillée
# en permanence dans le processus. La poser sur la machine qui sert le site public
# reviendrait à faire dépendre la production de blocs de la sécurité d'un serveur web —
# une faille dans Caddy, une page, ou n'importe quel composant exposé, et l'attaquant
# obtient la clé qui fait avancer la chaîne.
#
# Le validateur se déploie donc sur une AUTRE machine, sans rien d'exposé publiquement.
# Ici, on ne met qu'un nœud de lecture.
# ---------------------------------------------------------------------------
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "à lancer en root (sudo bash 30-node.sh)" >&2; exit 1; }

REPO="${REPO:-/opt/coinbosa-chain}"
GENESIS="${GENESIS:-$REPO/coinbosa/genesis/genesis-coinbosa.json}"
DATADIR="${DATADIR:-/var/lib/coinbosa/node}"
CHAIN_ID="${CHAIN_ID:-26262}"
RPC_PORT="${RPC_PORT:-8545}"
GETH="$REPO/build/bin/geth"

[ -x "$GETH" ] || { echo "ERREUR : $GETH introuvable — compiler d'abord : cd $REPO && make geth" >&2; exit 1; }
[ -f "$GENESIS" ] || {
  echo "ERREUR : genesis introuvable : $GENESIS" >&2
  echo "  Le genesis de PRODUCTION se génère avec de vraies adresses de répartition :" >&2
  echo "    VALIDATOR=0x… GOVERNOR=0x… node scripts/build-genesis.js" >&2
  echo "  Voir docs/GENESIS-PRODUCTION.md." >&2
  exit 1
}

# Garde : un genesis de développement ne doit pas servir un réseau public.
if grep -q '"coinbosaDev"' "$GENESIS" 2>/dev/null; then
  if [ "${ALLOW_DEV_NODE:-}" != "1" ]; then
    echo "ERREUR : ce genesis porte le marqueur coinbosaDev (développement)." >&2
    echo "  Le servir publiquement ferait passer un réseau jetable pour Coinbosa Chain." >&2
    echo "  Pour un réseau d'essai assumé : ALLOW_DEV_NODE=1 $0" >&2
    exit 1
  fi
  echo "⚠  RÉSEAU D'ESSAI : genesis de développement, ce n'est PAS le réseau de production."
fi

echo "==> Utilisateur de service (sans shell, sans privilèges)"
id coinbosa >/dev/null 2>&1 || useradd --system --home /var/lib/coinbosa --shell /usr/sbin/nologin coinbosa
# Le parent est PARTAGE avec le validateur, qui tourne sous un autre compte : il doit
# rester traversable (0755), sinon le validateur ne peut plus atteindre son propre
# repertoire. Les secrets sont proteges par le 0700 des sous-dossiers.
install -d -o root -g root -m 0755 /var/lib/coinbosa
install -d -o coinbosa -g coinbosa -m 0750 "$DATADIR"

echo "==> Initialisation du genesis (au premier lancement seulement)"
if [ ! -d "$DATADIR/geth" ]; then
  sudo -u coinbosa "$GETH" init --datadir "$DATADIR" "$GENESIS"
else
  echo "    déjà initialisé"
fi

PEER_LINE=""

echo "==> Service systemd"
# --http.addr 127.0.0.1 : le RPC n'est JAMAIS exposé directement. Caddy le relaie sur
#   https://explorer.…/rpc, en POST uniquement, et le pare-feu garde 8545 fermé.
# --http.api eth,net,web3 : surface minimale. Ni admin, ni personal, ni debug, ni txpool —
#   ces espaces de noms permettent d'inspecter ou de piloter le nœud.
cat > /etc/systemd/system/coinbosa-node.service <<UNIT
[Unit]
Description=Coinbosa Chain — nœud RPC (lecture seule, sans clé)
After=network-online.target
Wants=network-online.target

[Service]
User=coinbosa
Group=coinbosa
ExecStart=$GETH \\
  --datadir $DATADIR \\
  --networkid $CHAIN_ID \\
  --port 30303 \\
  --http --http.addr 127.0.0.1 --http.port $RPC_PORT \\
  --http.api eth,net,web3 \\
  --http.corsdomain "https://explorer.coinbosa.com" \\
  --http.vhosts "localhost,127.0.0.1" \\
  --syncmode full --gcmode full \\
  --maxpeers 25 \\
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
${PEER_LINE}
# Durcissement : le nœud ne doit pouvoir écrire que dans son répertoire de données.
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
systemctl enable coinbosa-node
systemctl restart coinbosa-node

# --- Appairage PERSISTANT et auto-réparateur -------------------------------------
# admin.addPeer() ne survit pas à un redémarrage, et une tentative depuis le service
# lui-même échoue silencieusement (le bac à sable systemd du nœud interdit l'accès).
# Un nœud RPC isolé continue de répondre en affichant une hauteur FIGÉE : l'explorateur
# montrerait une chaîne arrêtée sans qu'aucune alerte ne se déclenche. C'est précisément
# le genre de panne muette qu'il faut rendre impossible.
# Ce service surveille donc l'appairage en continu et le rétablit, ce qui couvre aussi
# les déconnexions passagères, pas seulement les redémarrages.
VALIDATOR_IPC="${VALIDATOR_IPC:-/var/lib/coinbosa/validator/geth.ipc}"
NODE_USER="${NODE_USER:-coinbosa}"
VAL_USER="${VAL_USER:-coinbosa-val}"
if [ -S "$VALIDATOR_IPC" ]; then
  echo "==> Surveillance de l'appairage avec le validateur"
  cat > /usr/local/bin/coinbosa-peer-check <<PEERSH
#!/usr/bin/env bash
# Rétablit l'appairage nœud RPC <-> validateur si nécessaire. Idempotent.
set -uo pipefail
GETH=$GETH
NODE_IPC=$DATADIR/geth.ipc
VAL_IPC=$VALIDATOR_IPC
# Chaque noeud tourne sous SON utilisateur : le repertoire du validateur est en 0700 et
# le compte du noeud RPC ne peut pas le lire. Ce script tourne en root et bascule donc
# sur le bon compte pour chaque socket.
NODE_USER=$NODE_USER
VAL_USER=$VAL_USER
[ -S "\$NODE_IPC" ] && [ -S "\$VAL_IPC" ] || exit 0
n=\$(sudo -u "\$NODE_USER" "\$GETH" attach --exec 'net.peerCount' "\$NODE_IPC" 2>/dev/null || echo 0)
[ "\${n:-0}" -gt 0 ] 2>/dev/null && exit 0
enode=\$(sudo -u "\$VAL_USER" "\$GETH" attach --exec 'admin.nodeInfo.enode' "\$VAL_IPC" 2>/dev/null | tr -d '"')
# Valider la FORME avant usage : en cas d'echec, geth ecrit son message d'erreur sur la
# sortie standard. Sans ce controle, ce message etait passe tel quel a addPeer() et le
# script annoncait « appairage retabli » alors qu'il n'avait rien fait.
case "\$enode" in
  enode://*) ;;
  *) logger -t coinbosa "appairage impossible : enode du validateur illisible"; exit 0 ;;
esac
sudo -u "\$NODE_USER" "\$GETH" attach --exec "admin.addPeer(\\"\$enode\\")" "\$NODE_IPC" >/dev/null 2>&1
logger -t coinbosa "appairage nœud RPC <-> validateur retabli"
PEERSH
  chmod 0755 /usr/local/bin/coinbosa-peer-check

  cat > /etc/systemd/system/coinbosa-peer.service <<'UNIT'
[Unit]
Description=Coinbosa — maintient l'appairage entre le nœud RPC et le validateur
After=coinbosa-node.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/coinbosa-peer-check
UNIT

  cat > /etc/systemd/system/coinbosa-peer.timer <<'UNIT'
[Unit]
Description=Coinbosa — vérifie l'appairage toutes les minutes

[Timer]
OnBootSec=45s
OnUnitActiveSec=60s
AccuracySec=5s

[Install]
WantedBy=timers.target
UNIT
  systemctl daemon-reload
  systemctl enable --now coinbosa-peer.timer >/dev/null 2>&1
  systemctl start coinbosa-peer.service >/dev/null 2>&1 || true
else
  echo "==> Validateur absent de cette machine — pas de surveillance d'appairage local."
fi

echo "==> Vérification"
sleep 5
if ! systemctl is-active --quiet coinbosa-node; then
  echo "ERREUR : le nœud n'est pas actif." >&2
  journalctl -u coinbosa-node -n 20 --no-pager >&2
  exit 1
fi

# On interroge le nœud pour PROUVER qu'il répond, plutôt que se fier au statut du service.
for i in $(seq 1 20); do
  bn=$(curl -s -X POST -H 'Content-Type: application/json' \
        --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
        "http://127.0.0.1:$RPC_PORT" 2>/dev/null | sed -n 's/.*"result":"\([^"]*\)".*/\1/p') || true
  [ -n "${bn:-}" ] && { echo "    nœud opérationnel — bloc $((bn))"; break; }
  sleep 2
done
[ -n "${bn:-}" ] || { echo "ERREUR : le nœud ne répond pas sur le RPC local." >&2; exit 1; }

echo ""
echo "==> Nœud RPC en service."
echo "    Le port $RPC_PORT reste FERMÉ au pare-feu : Caddy le relaie sur /rpc."
echo "    Journal : journalctl -u coinbosa-node -f"
echo ""
echo "    Le VALIDATEUR (avec sa clé de scellage) se déploie sur une AUTRE machine."
