#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — met les adresses IP d'une place d'échange hors de portée des
# prisons fail2ban du tier web.
#
#   sudo IPS="203.0.113.4 203.0.113.5 198.51.100.0/24" bash 74-allowlist-bourse.sh
#
# POURQUOI — mesuré sur le serveur le 2026-08-30 :
#   [caddy-rpc]    maxretry=1500 findtime=60  bantime=30m  -> 25 requêtes/s max
#   [caddy-status] maxretry=60   findtime=60  bantime=1h   -> 60 réponses 4xx/min
#   fail2ban-client status caddy-rpc :  « Total banned: 99 »  (1 IP bannie au moment
#   de la mesure). La prison n'est donc PAS théorique : elle bannit déjà.
#
# Ce que cela fait à une bourse :
#   * un indexeur qui rejoue 400 000 blocs en parallèle dépasse 1500 req/min en
#     quelques secondes -> banni 30 min, sur les ports 80 ET 443 ;
#   * le bannissement est « reject with icmp port-unreachable » (nft f2b-table) :
#     le client voit « connexion refusée », JAMAIS un HTTP 429. Rien dans la
#     réponse ne lui dit qu'il a été limité — c'est une panne muette de son côté ;
#   * une sonde de santé qui fait GET /rpc (405) une fois par seconde atteint 60
#     réponses 4xx en une minute -> bannie UNE HEURE par caddy-status ;
#   * si le service de retrait de la bourse partage l'IP de l'indexeur, les
#     retraits s'arrêtent aussi pendant le bannissement.
#
# RÉVERSIBLE : supprimer /etc/fail2ban/jail.d/coinbosa-bourse.conf puis
#              systemctl reload fail2ban.
# ---------------------------------------------------------------------------
set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "à lancer en root (sudo ... bash 74-allowlist-bourse.sh)" >&2; exit 1; }
[ -n "${IPS:-}" ] || { echo "ERREUR : renseigner IPS=\"a.b.c.d e.f.g.h/24\" (IP fournies par la bourse)." >&2; exit 1; }

command -v fail2ban-client >/dev/null 2>&1 || { echo "ERREUR : fail2ban absent." >&2; exit 1; }

# Validation de FORME. Une entrée mal écrite serait acceptée par fail2ban en
# silence et la liste blanche ne protégerait rien : on refuse d'écrire un fichier
# qui donnerait une fausse impression de sécurité.
for ip in $IPS; do
  if ! printf '%s' "$ip" | grep -qE '^([0-9]{1,3}\.){3}[0-9]{1,3}(/[0-9]{1,2})?$|^[0-9a-fA-F:]+(/[0-9]{1,3})?$'; then
    echo "ERREUR : « $ip » n'est ni une IPv4/CIDR ni une IPv6/CIDR valide." >&2; exit 1
  fi
done
echo "==> Adresses à exempter : $IPS"

BASE="127.0.0.1/8 ::1"
OP_IP=""
[ -n "${SSH_CONNECTION:-}" ] && OP_IP=$(echo "$SSH_CONNECTION" | awk '{print $1}')
[ -n "$OP_IP" ] && BASE="$BASE $OP_IP"

# jail.d est lu par ordre alphabétique : ce fichier passe APRÈS coinbosa-web.conf
# et redéfinit ignoreip pour les deux prisons concernées.
cat > /etc/fail2ban/jail.d/coinbosa-bourse.conf <<CONF
# Coinbosa — exemption des intégrateurs (places d'échange). Généré par 74-allowlist-bourse.sh
# Ce fichier SURCHARGE ignoreip défini dans coinbosa-web.conf.
[caddy-rpc]
ignoreip = $BASE $IPS

[caddy-status]
ignoreip = $BASE $IPS
CONF
chmod 0644 /etc/fail2ban/jail.d/coinbosa-bourse.conf

echo "==> Rechargement de fail2ban"
systemctl reload fail2ban || systemctl restart fail2ban
sleep 2
systemctl is-active --quiet fail2ban || { echo "ERREUR : fail2ban inactif après rechargement." >&2; exit 1; }

echo "==> PREUVE que l'exemption est réellement prise en compte"
# On n'affirme rien : on relit la valeur effective depuis fail2ban lui-même.
ECHEC=0
for jail in caddy-rpc caddy-status; do
  EFFECTIF=$(fail2ban-client get "$jail" ignoreip 2>/dev/null || echo "")
  echo "    [$jail] ignoreip effectif : $EFFECTIF"
  for ip in $IPS; do
    printf '%s' "$EFFECTIF" | grep -qF -- "$ip" || { echo "    ÉCHEC : $ip absente de $jail" >&2; ECHEC=1; }
  done
done
[ "$ECHEC" -eq 0 ] || { echo "ERREUR : l'exemption n'est pas effective." >&2; exit 1; }

# Débannir tout de suite les IP concernées si elles sont déjà bannies.
for jail in caddy-rpc caddy-status; do
  for ip in $IPS; do
    fail2ban-client set "$jail" unbanip "$ip" >/dev/null 2>&1 || true
  done
done

echo ""
echo "==> Exemption active pour : $IPS"
echo "    État des prisons :  fail2ban-client status caddy-rpc"
echo "    Retirer l'exemption : rm /etc/fail2ban/jail.d/coinbosa-bourse.conf && systemctl reload fail2ban"
