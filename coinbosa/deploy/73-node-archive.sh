#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — SECOND nœud RPC en mode ARCHIVE, destiné à l'indexeur d'une place
# d'échange. Ajoute aussi le point d'accès WebSocket qui manque aujourd'hui.
#
#   sudo bash 73-node-archive.sh
#
# POURQUOI. Le nœud en service (coinbosa-node, port 8545) tourne en
# « --gcmode full » avec le schéma d'état PATH. Mesuré le 2026-08-30 sur
# https://explorer.coinbosa.com/rpc : eth_getBalance échoue avec
# « historical state … is not available » pour TOUT bloc antérieur au 377600,
# alors que la chaîne était à 403392. Un indexeur de bourse qui rejoue depuis le
# bloc 0 ne peut donc pas reconstruire les soldes : l'intégration s'arrête là.
# Le schéma PATH ne sait pas servir un état plus ancien que sa couche de disque,
# et cette couche AVANCE : chaque vidage de tampon détruit définitivement une
# tranche d'historique supplémentaire.
#
# CE QUE CE SCRIPT NE FAIT PAS — volontairement :
#   * il ne touche NI au validateur, NI au nœud 8545, NI à Caddy ;
#   * il n'ouvre aucun port au monde (tout reste sur 127.0.0.1) ;
#   * il s'appaire au nœud RPC local, JAMAIS au validateur.
# Le basculement de Caddy vers ce nœud est une étape MANUELLE, décrite à la fin.
#
# RÉVERSIBLE : systemctl disable --now coinbosa-node-archive && rm -rf le datadir.
# ---------------------------------------------------------------------------
set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "à lancer en root (sudo bash 73-node-archive.sh)" >&2; exit 1; }

REPO="${REPO:-/opt/coinbosa-chain}"
GENESIS="${GENESIS:-$REPO/coinbosa/genesis/genesis-coinbosa.json}"
DATADIR="${DATADIR:-/var/lib/coinbosa/node-archive}"
CHAIN_ID="${CHAIN_ID:-26262}"
HTTP_PORT="${HTTP_PORT:-8547}"     # 8546 est le défaut WS de geth : on l'évite.
WS_PORT="${WS_PORT:-8548}"
P2P_PORT="${P2P_PORT:-30305}"
PEER_RPC_IPC="${PEER_RPC_IPC:-/var/lib/coinbosa/node/geth.ipc}"
GETH="$REPO/build/bin/geth"

[ -x "$GETH" ] || { echo "ERREUR : $GETH introuvable." >&2; exit 1; }
[ -f "$GENESIS" ] || { echo "ERREUR : genesis introuvable : $GENESIS" >&2; exit 1; }

# Garde : ne jamais servir un genesis de développement au public.
if grep -q '"coinbosaDev"' "$GENESIS" 2>/dev/null && [ "${ALLOW_DEV_NODE:-}" != "1" ]; then
  echo "ERREUR : genesis de développement (coinbosaDev). Refus." >&2; exit 1
fi

# Garde : l'espace disque. Un nœud archive conserve TOUT l'état intermédiaire.
LIBRE_GO=$(df -BG --output=avail / | tail -1 | tr -dc '0-9')
[ "${LIBRE_GO:-0}" -ge 20 ] || { echo "ERREUR : moins de 20 Go libres (${LIBRE_GO} Go)." >&2; exit 1; }
echo "==> Espace libre : ${LIBRE_GO} Go"

id coinbosa >/dev/null 2>&1 || useradd --system --home /var/lib/coinbosa --shell /usr/sbin/nologin coinbosa
install -d -o coinbosa -g coinbosa -m 0750 "$DATADIR"

echo "==> Initialisation en schéma HASH (obligatoire : le schéma PATH ne fait pas d'archive)"
if [ ! -d "$DATADIR/geth" ]; then
  sudo -u coinbosa "$GETH" init --datadir "$DATADIR" --state.scheme hash "$GENESIS"
else
  echo "    déjà initialisé"
fi


echo "==> Service systemd (coinbosa-node-archive)"
# --gcmode archive + --state.scheme hash : conserve l'état de CHAQUE bloc. C'est la
#   seule configuration qui permet eth_getBalance / eth_call à une hauteur ancienne.
# --ws : point d'accès WebSocket, absent du nœud actuel. Beaucoup d'intégrations
#   suivent la tête par abonnement (eth_subscribe newHeads) ; sans WS elles doivent
#   scruter, ce qui multiplie les requêtes et rapproche du seuil de bannissement.
# --http.api / --ws.api : eth,net seulement. Ni admin, ni personal, ni debug, ni
#   txpool sur les interfaces réseau — le nœud n'a aucune clé, et on n'ouvre pas
#   d'espace de pilotage. (debug reste hors ligne : voir la note en fin de script.)
# --rpc.batch-request-limit 200 : un indexeur groupe ses appels ; 50 le force à
#   multiplier les requêtes HTTP, donc à s'approcher du seuil fail2ban. 200 reste
#   très en deçà du défaut geth (1000).
# PAS de --rangelimit ici : la borne de 5 000 blocs sur eth_getLogs reste souhaitable
#   côté public, mais elle est le principal frein au rejeu. On la conserve : voir la
#   variable RANGELIMIT ci-dessous pour l'assouplir en connaissance de cause.
RANGELIMIT_FLAG="--rangelimit"
[ "${SANS_RANGELIMIT:-0}" = "1" ] && RANGELIMIT_FLAG=""

cat > /etc/systemd/system/coinbosa-node-archive.service <<UNIT
[Unit]
Description=Coinbosa Chain — nœud RPC ARCHIVE (lecture seule, sans clé) pour indexeurs
After=network-online.target
Wants=network-online.target

[Service]
User=coinbosa
Group=coinbosa
ExecStart=$GETH \\
  --datadir $DATADIR \\
  --networkid $CHAIN_ID \\
  --port $P2P_PORT \\
  --gcmode archive --state.scheme hash \\
  --syncmode full \\
  --http --http.addr 127.0.0.1 --http.port $HTTP_PORT \\
  --http.api eth,net \\
  --ws --ws.addr 127.0.0.1 --ws.port $WS_PORT \\
  --ws.api eth,net \\
  --ws.origins "*" \\
  --rpc.batch-request-limit 200 \\
  --rpc.batch-response-max-size 25000000 \\
  $RANGELIMIT_FLAG \\
  --rpc.logquerylimit 20 \\
  --http.vhosts "localhost,127.0.0.1" \\
  --nodiscover \\
  --maxpeers 4 \\
  --verbosity 3
Restart=on-failure
RestartSec=5
# Arrêt propre : geth doit écrire son état. Tué trop tôt, il rembobine au redémarrage.
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
# Le validateur est sur CETTE machine. Un rejeu archive re-exécute 400 000 blocs et
# peut monopoliser un cœur : on le met explicitement derrière le validateur.
Nice=10
CPUWeight=20
IOWeight=20

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable coinbosa-node-archive
systemctl start coinbosa-node-archive

echo "==> Appairage sur le nœud RPC LOCAL (jamais sur le validateur)"
# Le validateur n'est PAS sollicité : ce nœud se synchronise depuis la réplique de
# lecture. Si celle-ci hoquette, la production de blocs n'est pas concernée.
install -d -m 0755 /usr/local/bin
cat > /usr/local/bin/coinbosa-archive-peer <<PEERSH
#!/usr/bin/env bash
set -uo pipefail
GETH=$GETH
A_IPC=$DATADIR/geth.ipc
R_IPC=$PEER_RPC_IPC
[ -S "\$A_IPC" ] && [ -S "\$R_IPC" ] || exit 0
n=\$(sudo -u coinbosa "\$GETH" attach --exec 'net.peerCount' "\$A_IPC" 2>/dev/null || echo 0)
[ "\${n:-0}" -gt 0 ] 2>/dev/null && exit 0
enode=\$(sudo -u coinbosa "\$GETH" attach --exec 'admin.nodeInfo.enode' "\$R_IPC" 2>/dev/null | tr -d '"')
case "\$enode" in enode://*) ;; *) logger -t coinbosa "archive : enode illisible"; exit 0 ;; esac
enode=\$(printf '%s' "\$enode" | sed 's/?discport=[0-9]*//')
sudo -u coinbosa "\$GETH" attach --exec "admin.addPeer(\\"\$enode\\")" "\$A_IPC" >/dev/null 2>&1
logger -t coinbosa "archive : appairage retabli"
PEERSH
chmod 0755 /usr/local/bin/coinbosa-archive-peer

cat > /etc/systemd/system/coinbosa-archive-peer.service <<'UNIT'
[Unit]
Description=Coinbosa — appairage du nœud archive sur le nœud RPC
After=coinbosa-node-archive.service
[Service]
Type=oneshot
ExecStart=/usr/local/bin/coinbosa-archive-peer
UNIT
cat > /etc/systemd/system/coinbosa-archive-peer.timer <<'UNIT'
[Unit]
Description=Coinbosa — vérifie l'appairage du nœud archive toutes les minutes
[Timer]
OnBootSec=45s
OnUnitActiveSec=60s
AccuracySec=5s
[Install]
WantedBy=timers.target
UNIT
systemctl daemon-reload
systemctl enable --now coinbosa-archive-peer.timer >/dev/null 2>&1
systemctl start coinbosa-archive-peer.service >/dev/null 2>&1 || true

echo "==> Vérification (le nœud doit RÉPONDRE, pas seulement être « actif »)"
sleep 8
systemctl is-active --quiet coinbosa-node-archive || {
  echo "ERREUR : service inactif." >&2; journalctl -u coinbosa-node-archive -n 30 --no-pager >&2; exit 1; }
# ---------------------------------------------------------------------------
# ACCIDENT ÉVITÉ. « bn » est du texte rendu par le nœud, et il entrait tel quel
# dans $((bn)). L'évaluation arithmétique de bash RELIT son texte et évalue
# l'indice d'un tableau : une réponse « 0x1,HTTP_PORT[$(commande)] » exécutait
# « commande » ICI, dans un script lancé en root. Reproduit, pas supposé.
# Et le test « non vide » laissait passer n'importe quelle chaîne, en affichant
# un numéro de bloc inventé. On exige donc une vraie quantité « 0x… ».
# ---------------------------------------------------------------------------
hexok() {
  case "${1:-}" in
    0x|0x*[!0-9a-fA-F]*) return 1 ;;   # « 0x » seul, ou un caractère hors hexadécimal
    0x*)                 return 0 ;;
    *)                   return 1 ;;
  esac
}

bn=""
for _ in $(seq 1 20); do
  bn=$(curl -s -X POST -H 'Content-Type: application/json' \
        --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
        "http://127.0.0.1:$HTTP_PORT" | sed -n 's/.*"result":"\([^"]*\)".*/\1/p') || true
  hexok "$bn" && break
  bn=""          # réponse non conforme = pas de réponse : on continue d'attendre
  sleep 2
done
[ -n "$bn" ] || { echo "ERREUR : pas de hauteur exploitable sur 127.0.0.1:$HTTP_PORT." >&2; exit 1; }
echo "    nœud archive à la hauteur $((16#${bn#0x}))"

# On PROUVE que ce nœud est sur LA MÊME chaîne : même empreinte de bloc 0 que le
# nœud en service. Un genesis divergent donnerait un nœud qui répond joyeusement
# à propos d'une autre chaîne — exactement le faux vert qu'il faut rendre impossible.
g0(){ curl -s -X POST -H 'Content-Type: application/json' \
      --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x0",false]}' \
      "http://127.0.0.1:$1" | sed -n 's/.*"hash":"\([^"]*\)".*/\1/p'; }
H_ARCH=$(g0 "$HTTP_PORT"); H_PROD=$(g0 8545)
# Une empreinte de bloc, c'est « 0x » + 64 chiffres hexadécimaux, rien d'autre.
# Sans cette exigence, deux nœuds répondant le MÊME texte malformé passaient le
# test d'égalité ci-dessous et le genesis était déclaré identique sans preuve.
empreinte_ok() { case "${1:-}" in 0x*[!0-9a-fA-F]*) return 1 ;; 0x????????????????????????????????????????????????????????????????) return 0 ;; *) return 1 ;; esac; }
if ! empreinte_ok "$H_ARCH" || ! empreinte_ok "$H_PROD"; then
  echo "ARRÊT : empreinte de bloc 0 illisible — archive=<${H_ARCH:0:20}> production=<${H_PROD:0:20}>" >&2
  systemctl disable --now coinbosa-node-archive >/dev/null 2>&1 || true
  exit 1
fi
if [ -z "$H_ARCH" ] || [ "$H_ARCH" != "$H_PROD" ]; then
  echo "ARRÊT : genesis divergent — archive=$H_ARCH  production=$H_PROD" >&2
  systemctl disable --now coinbosa-node-archive >/dev/null 2>&1 || true
  exit 1
fi
echo "    même genesis que le nœud 8545 : $H_ARCH"

cat <<'FIN'

==> Nœud archive lancé. IL DOIT MAINTENANT REJOUER LA CHAÎNE DEPUIS LE BLOC 0.

  Suivre l'avancement :
      watch -n 30 'curl -s -X POST -H "Content-Type: application/json" \
        --data "{\"jsonrpc\":\"2.0\",\"method\":\"eth_syncing\",\"params\":[],\"id\":1}" \
        http://127.0.0.1:8547'

  CRITÈRE D'ACCEPTATION — les deux doivent être vrais AVANT tout basculement :
    1) eth_syncing renvoie false ET eth_blockNumber est à moins de 5 blocs du
       nœud 8545 ;
    2) l'état du bloc 1 est SERVI (c'est tout l'objet de l'opération) :
        curl -s -X POST -H 'Content-Type: application/json' \
          --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBalance",
                   "params":["0x0000000000000000000000000000000000001000","0x1"]}' \
          http://127.0.0.1:8547
       -> doit renvoyer "result", PAS "historical state ... is not available".

  Tant que ces deux critères ne sont pas remplis, NE PAS toucher à Caddy.
  Le nœud 8545 continue de servir l'explorateur pendant tout le rejeu.

  BASCULEMENT (étape manuelle, à faire seulement après les deux critères) :
    voir 73-caddy-ws-archive.snippet — il donne le bloc Caddyfile à insérer pour
    router /rpc vers 8547 et ouvrir /ws vers 8548.

  RETOUR ARRIÈRE : remettre reverse_proxy 127.0.0.1:8545 dans le Caddyfile,
    caddy validate puis systemctl reload caddy. Le nœud 8545 n'a jamais été arrêté.
FIN
