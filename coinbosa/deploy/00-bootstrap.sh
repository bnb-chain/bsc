#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — durcissement de base d'un VPS Ubuntu (tier public).
# À lancer en root (ou via sudo) SUR le VPS :  sudo bash 00-bootstrap.sh
#
# Ce script est SÛR à relancer (idempotent) et NE MODIFIE PAS la config SSH,
# pour ne jamais te verrouiller dehors.
# ---------------------------------------------------------------------------
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "Ce script doit être lancé en root (sudo bash 00-bootstrap.sh)." >&2
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive

echo "==> Mise à jour du système"
apt-get update -y
apt-get upgrade -y

echo "==> Paquets de base"
apt-get install -y ufw fail2ban unattended-upgrades ca-certificates curl \
  debian-keyring debian-archive-keyring apt-transport-https gnupg

echo "==> Pare-feu UFW"
# IMPORTANT : on AUTORISE SSH AVANT d'activer le pare-feu (sinon lock-out).
ufw allow OpenSSH        >/dev/null 2>&1 || ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw default deny incoming
ufw default allow outgoing
# --force : évite la question interactive « Proceed (y|n) ? »
ufw --force enable
ufw status verbose

echo "==> fail2ban (protection brute-force SSH)"
systemctl enable --now fail2ban

echo "==> Mises à jour de sécurité automatiques"
# active unattended-upgrades sans interaction
cat > /etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
EOF
systemctl enable --now unattended-upgrades 2>/dev/null || true

echo "==> Fuseau horaire + swap si RAM faible (optionnel)"
# petit fichier d'échange si moins de ~1,5 Go de RAM et aucun swap présent
mem_kb="$(awk '/MemTotal/{print $2}' /proc/meminfo)"
if [ "${mem_kb:-0}" -lt 1600000 ] && ! swapon --show | grep -q .; then
  if [ ! -f /swapfile ]; then
    echo "    création d'un swap de 2 Go"
    fallocate -l 2G /swapfile || dd if=/dev/zero of=/swapfile bs=1M count=2048
    chmod 600 /swapfile
    mkswap /swapfile
    swapon /swapfile
    grep -q '/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
  fi
fi

echo ""
echo "==> Durcissement de base terminé."
echo "    SSH : non modifié (aucun risque de lock-out)."
echo "    Étape suivante : sudo SITE_DOMAIN=... EXPLORER_DOMAIN=... bash 10-web.sh"
