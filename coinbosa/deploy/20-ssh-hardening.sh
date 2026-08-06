#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — durcissement SSH du serveur public.
#
#   Désactive l'authentification par MOT DE PASSE et le login root par mot de
#   passe. L'accès par CLÉ reste le seul chemin d'entrée.
#
# À lancer en root SUR le VPS :
#
#   sudo bash 20-ssh-hardening.sh
#
# ---------------------------------------------------------------------------
# SÉCURITÉ — ce script est conçu pour qu'il soit IMPOSSIBLE de se verrouiller
# dehors. Il refuse d'agir si l'accès par clé n'est pas prouvé, il vérifie la
# configuration EFFECTIVE (sshd -T) et non le fichier écrit, il recharge sans
# jamais redémarrer, et il arme un RETOUR ARRIÈRE AUTOMATIQUE : si tu ne
# confirmes pas depuis une NOUVELLE session dans le délai imparti, la
# configuration d'origine est restaurée toute seule.
# ---------------------------------------------------------------------------
set -euo pipefail

DROPIN=/etc/ssh/sshd_config.d/10-coinbosa-hardening.conf
SENTINEL=/run/coinbosa-ssh-confirmed
ROLLBACK_UNIT=coinbosa-ssh-rollback
GRACE_MIN="${GRACE_MIN:-15}"      # minutes avant retour arrière automatique

die() { echo "ARRÊT : $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "à lancer en root (sudo bash 20-ssh-hardening.sh)."

# --- Le service SSH s'appelle 'ssh' (Debian/Ubuntu) ou 'sshd' (RHEL) ---------
SSH_UNIT=ssh
systemctl list-unit-files 2>/dev/null | grep -q '^ssh\.service' || SSH_UNIT=sshd
systemctl list-unit-files 2>/dev/null | grep -q "^${SSH_UNIT}\.service" \
  || die "aucun service ssh/sshd trouvé — serveur inattendu, on ne touche à rien."

echo "==> 1/7  Vérification : au moins une clé publique autorisée"
KEYCOUNT=0
for f in /root/.ssh/authorized_keys /home/*/.ssh/authorized_keys; do
  [ -f "$f" ] || continue
  n=$(grep -cE '^[[:space:]]*(ssh-(rsa|ed25519|dss)|ecdsa-sha2-|sk-)' "$f" 2>/dev/null || true)
  [ "$n" -gt 0 ] && echo "    $n clé(s) dans $f"
  KEYCOUNT=$((KEYCOUNT + n))
done
[ "$KEYCOUNT" -gt 0 ] \
  || die "aucune clé publique dans authorized_keys. Installe ta clé AVANT de durcir, sinon tu perds l'accès."

echo "==> 2/7  Vérification : la session courante est bien authentifiée PAR CLÉ"
# Sans cette preuve, on pourrait couper le mot de passe alors que c'est le seul
# moyen d'entrer. On identifie la session par son IP + port source.
if [ -z "${SSH_CONNECTION:-}" ]; then
  die "pas de session SSH détectée (\$SSH_CONNECTION vide).
     Lance ce script DEPUIS une session SSH par clé, pas depuis une console web/série :
     c'est cette session qui prouve que l'accès par clé fonctionne."
fi
CLIENT_IP=$(echo "$SSH_CONNECTION" | awk '{print $1}')
CLIENT_PORT=$(echo "$SSH_CONNECTION" | awk '{print $2}')
echo "    session : ${CLIENT_IP}:${CLIENT_PORT}"

AUTH_OK=0
if journalctl -u "$SSH_UNIT" --since "-24h" --no-pager 2>/dev/null \
     | grep -qE "Accepted publickey for .* from ${CLIENT_IP} port ${CLIENT_PORT}\b"; then
  AUTH_OK=1
elif [ -f /var/log/auth.log ] \
     && grep -qE "Accepted publickey for .* from ${CLIENT_IP} port ${CLIENT_PORT}\b" /var/log/auth.log 2>/dev/null; then
  AUTH_OK=1
fi
[ "$AUTH_OK" -eq 1 ] \
  || die "impossible de PROUVER que cette session utilise une clé (elle est peut-être par mot de passe).
     Reconnecte-toi explicitement par clé, puis relance :
       ssh -o PreferredAuthentications=publickey -o PubkeyAuthentication=yes root@<ip>"
echo "    authentification par clé confirmée dans le journal"

echo "==> 3/7  Vérification : les fichiers déposés dans sshd_config.d sont bien pris en compte"
# Piège classique : sans la directive Include, le drop-in est ignoré en silence
# (et on croirait le serveur durci alors qu'il ne l'est pas).
grep -qE '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config\.d/\*\.conf' /etc/ssh/sshd_config \
  || die "/etc/ssh/sshd_config ne contient pas 'Include /etc/ssh/sshd_config.d/*.conf'.
     Le durcissement serait ignoré en silence. Ajoute cette ligne EN TÊTE du fichier, puis relance."
install -d -m 0755 /etc/ssh/sshd_config.d

echo "==> 4/7  Sauvegarde et écriture du durcissement"
BACKUP=/root/coinbosa-sshd-backup-$(date +%Y%m%d-%H%M%S).tar
tar -cf "$BACKUP" /etc/ssh/sshd_config /etc/ssh/sshd_config.d 2>/dev/null || true
chmod 600 "$BACKUP"
echo "    sauvegarde : $BACKUP"

cat > "$DROPIN" <<'CONF'
# Coinbosa — durcissement SSH. Généré par deploy/20-ssh-hardening.sh
# L'accès se fait EXCLUSIVEMENT par clé publique.
PasswordAuthentication no
KbdInteractiveAuthentication no
ChallengeResponseAuthentication no
PermitRootLogin prohibit-password
PermitEmptyPasswords no
MaxAuthTries 3
LoginGraceTime 30
CONF
chmod 0644 "$DROPIN"

echo "==> 5/7  Validation de la syntaxe, PUIS de la configuration EFFECTIVE"
if ! sshd -t 2>/tmp/sshd-test.err; then
  rm -f "$DROPIN"
  cat /tmp/sshd-test.err >&2
  die "configuration sshd invalide — durcissement annulé, rien n'a été appliqué."
fi
# sshd -T dit ce que le serveur appliquera VRAIMENT (ordre des Include compris).
# On ne se fie jamais au seul contenu du fichier écrit.
EFF=$(sshd -T 2>/dev/null || true)
check_eff() {
  echo "$EFF" | grep -qiE "^$1 $2\$" || {
    rm -f "$DROPIN"
    die "la configuration effective ne prend PAS '$1 $2' (une directive antérieure gagne).
     Durcissement annulé, rien n'a changé. Vérifie l'ordre des directives dans /etc/ssh/sshd_config."
  }
}
check_eff passwordauthentication no
check_eff permitrootlogin prohibit-password
check_eff kbdinteractiveauthentication no
echo "    configuration effective conforme (mot de passe désactivé, root par clé uniquement)"

echo "==> 6/7  Armement du retour arrière automatique (${GRACE_MIN} min)"
rm -f "$SENTINEL"
systemctl stop "${ROLLBACK_UNIT}.timer" 2>/dev/null || true
systemd-run --quiet --unit="$ROLLBACK_UNIT" --on-active="${GRACE_MIN}min" \
  /bin/bash -c "[ -f '$SENTINEL' ] || { rm -f '$DROPIN'; systemctl reload $SSH_UNIT; logger -t coinbosa 'durcissement SSH annule automatiquement (non confirme)'; }" \
  || die "impossible d'armer le retour arrière (systemd-run). Durcissement NON appliqué."
echo "    si tu ne confirmes pas, la configuration d'origine revient toute seule"

echo "==> 7/7  Rechargement de SSH (reload, jamais restart : les sessions ouvertes survivent)"
systemctl reload "$SSH_UNIT"

cat <<EOF

──────────────────────────────────────────────────────────────────────────────
  DURCISSEMENT APPLIQUÉ — MAIS PAS ENCORE CONFIRMÉ.

  NE FERME PAS cette session.

  1. Ouvre une DEUXIÈME fenêtre de terminal et connecte-toi :

       ssh root@$(hostname -I 2>/dev/null | awk '{print $1}')

  2. Si ça marche, confirme DEPUIS CETTE NOUVELLE SESSION :

       sudo touch $SENTINEL && sudo systemctl stop ${ROLLBACK_UNIT}.timer

  Sans cette confirmation, dans ${GRACE_MIN} minutes la configuration d'origine
  est restaurée automatiquement et l'accès par mot de passe revient.

  Sauvegarde de la configuration : $BACKUP
──────────────────────────────────────────────────────────────────────────────
EOF
