#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — installe Caddy (TLS auto) et sert le tier public.
# À lancer en root SUR le VPS, avec les domaines en variables :
#
#   sudo SITE_DOMAIN=coinbosa.com EXPLORER_DOMAIN=explorer.coinbosa.com bash 10-web.sh
#
# Idempotent. Le DNS des domaines doit pointer vers ce VPS pour que Caddy
# puisse émettre les certificats.
# ---------------------------------------------------------------------------
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "Ce script doit être lancé en root (sudo ... bash 10-web.sh)." >&2
  exit 1
fi

SITE_DOMAIN="${SITE_DOMAIN:-coinbosa.com}"
EXPLORER_DOMAIN="${EXPLORER_DOMAIN:-explorer.coinbosa.com}"
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a

echo "==> Dépendances (au cas où 10-web.sh est lancé seul)"
apt-get update -y
apt-get install -y curl gnupg ca-certificates apt-transport-https debian-keyring debian-archive-keyring

echo "==> Installation de Caddy (dépôt officiel)"
if ! command -v caddy >/dev/null 2>&1; then
  install -d -m 0755 /usr/share/keyrings
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
    | gpg --batch --yes --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
    | tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
  apt-get update -y
  apt-get install -y caddy
else
  echo "    Caddy déjà installé : $(caddy version)"
fi

echo "==> Arborescence web"
install -d -o caddy -g caddy /var/www/coinbosa/site
install -d -o caddy -g caddy /var/www/coinbosa/explorer
install -d -o caddy -g caddy /var/www/coinbosa/whitepaper

# Journal d'accès : indispensable pour la forensique ET comme source de la prison
# fail2ban HTTP (21-fail2ban-web.sh). Sans journal, aucun bannissement possible.
install -d -o caddy -g caddy -m 0750 /var/log/caddy

# page d'attente tant que les vrais fichiers ne sont pas poussés
for d in site explorer whitepaper; do
  if [ ! -f "/var/www/coinbosa/$d/index.html" ]; then
    printf '<!doctype html><meta charset=utf-8><title>Coinbosa</title><p>Coinbosa — déploiement en cours.</p>\n' \
      > "/var/www/coinbosa/$d/index.html"
    chown caddy:caddy "/var/www/coinbosa/$d/index.html"
  fi
done

echo "==> Écriture de /etc/caddy/Caddyfile (domaines : $SITE_DOMAIN / $EXPLORER_DOMAIN)"
cat > /etc/caddy/Caddyfile <<EOF
# Généré par 10-web.sh — Coinbosa tier public. TLS automatique (Let's Encrypt).

# Options globales : délais serrés (anti-slowloris / pic de listing).
{
    servers {
        timeouts {
            read_body   10s
            read_header 5s
            idle        2m
        }
    }
}

# --- Site vitrine + livre blanc ---
$SITE_DOMAIN, www.$SITE_DOMAIN {
    encode gzip zstd

    log {
        output file /var/log/caddy/site-access.log {
            roll_size 50MiB
            roll_keep 10
        }
        format json
    }

    handle_path /whitepaper* {
        root * /var/www/coinbosa/whitepaper
        file_server
    }

    handle {
        root * /var/www/coinbosa/site
        file_server
    }

    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains"
        X-Content-Type-Options    "nosniff"
        X-Frame-Options           "DENY"
        Referrer-Policy           "strict-origin-when-cross-origin"
        Content-Security-Policy   "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; base-uri 'none'; object-src 'none'; form-action 'none'; frame-ancestors 'none'; upgrade-insecure-requests"
        Permissions-Policy        "accelerometer=(), autoplay=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), midi=(), payment=(), usb=(), interest-cohort=()"
        -Server
    }
}

# --- Explorateur ---
$EXPLORER_DOMAIN {
    encode gzip zstd

    log {
        output file /var/log/caddy/explorer-access.log {
            roll_size 50MiB
            roll_keep 10
        }
        format json
    }

    # --- Relais JSON-RPC en MÊME ORIGINE ---
    # L'explorateur appelle https://$EXPLORER_DOMAIN/rpc : même schéma, même hôte, même
    # port (443). C'est ce qui permet de garder la CSP stricte (connect-src 'self') ET
    # le port 8545 du nœud FERMÉ au pare-feu — le nœud n'est jamais exposé directement.
    # Seul POST est relayé, avec un corps borné (une requête JSON-RPC légitime est petite).
    @rpc_post {
        path /rpc
        method POST
    }
    handle @rpc_post {
        request_body {
            max_size 32KB
        }
        reverse_proxy 127.0.0.1:8545
    }
    # Toute autre méthode sur /rpc (GET, OPTIONS, HEAD…) est refusée.
    handle /rpc* {
        respond "method not allowed" 405
    }

    handle {
        root * /var/www/coinbosa/explorer
        file_server
    }

    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains"
        X-Content-Type-Options    "nosniff"
        X-Frame-Options           "DENY"
        Referrer-Policy           "strict-origin-when-cross-origin"
        Content-Security-Policy   "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; base-uri 'none'; object-src 'none'; form-action 'none'; frame-ancestors 'none'; upgrade-insecure-requests"
        Permissions-Policy        "accelerometer=(), autoplay=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), midi=(), payment=(), usb=(), interest-cohort=()"
        -Server
    }
}
EOF

echo "==> Validation de la configuration"
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile

echo "==> Rechargement de Caddy"
systemctl enable caddy
systemctl reload caddy 2>/dev/null || systemctl restart caddy

echo ""
echo "==> Tier web prêt."
echo "    Pousse les fichiers depuis ton poste :  SERVER=<user>@<ip> bash publish-static.sh"
echo "    Suivre l'émission des certificats :      journalctl -u caddy -f"
