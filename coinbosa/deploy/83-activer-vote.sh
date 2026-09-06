#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — étape 8 : donner la clé de vote au validateur, et le redémarrer.
#
#   sudo bash 83-activer-vote.sh            # applique, avec retour arrière automatique
#   sudo ANNULER=1 bash 83-activer-vote.sh  # retire les drapeaux et redémarre
#
# C'EST L'ÉTAPE LA PLUS RISQUÉE, ET LE RISQUE N'EST PAS LA CLÉ
# -------------------------------------------------------------
# `coinbosa-validator` est le SEUL nœud qui scelle. S'il ne remonte pas, la
# production de blocs s'arrête — toute la chaîne avec. Et il ne remontera pas si
# le portefeuille BLS ne s'ouvre pas : NewVoteManager échoue, New() propage, le
# nœud sort (eth/backend.go:508-511). Avec StartLimitBurst=5 sur 600 s, systemd
# cesse de réessayer après cinq échecs et la chaîne reste morte.
#
# CE QUE CE SCRIPT AJOUTE AU DOCUMENT : IL NE COMPTE PAS SUR TOI POUR REGARDER
# ----------------------------------------------------------------------------
# La procédure écrite dit « si le nœud ne démarre pas, allez au § 6.1 ». Cela
# suppose quelqu'un devant l'écran, qui comprend ce qu'il lit, la nuit, vite.
# L'incident du 3 septembre a montré ce que vaut cette hypothèse : la sonde a
# crié pendant dix-neuf heures et personne n'a vu.
#
# Ici, le script SURVEILLE lui-même la remontée. Si le validateur n'a pas
# reproduit de bloc dans le délai imparti, il REMET LE FICHIER D'ORIGINE,
# redémarre, et vérifie que la chaîne repart — sans rien demander à personne.
# Le pire cas cesse d'être « la chaîne est arrêtée » pour devenir « la chaîne
# tourne comme avant, et le vote n'est pas activé ».
#
# CE QUI EST VÉRIFIÉ AVANT DE TOUCHER À QUOI QUE CE SOIT
# ------------------------------------------------------
#   · le portefeuille BLS existe ;
#   · le fichier de mot de passe ne contient AUCUN saut de ligne — c'est le
#     piège qui empêche le nœud de démarrer, et le seul qui soit silencieux :
#     la CLI lit la première ligne, le nœud lit le fichier entier ;
#   · le portefeuille s'OUVRE réellement avec ce fichier — pas « il existe » :
#     il s'ouvre. C'est la seule preuve qui vaille ;
#   · la clé est DÉJÀ inscrite on-chain et visible dans l'extraData d'un bloc
#     d'epoch. Activer le vote avant l'inscription ne casse rien, mais ne sert
#     à rien : personne ne reconnaîtrait ces votes.
# ---------------------------------------------------------------------------
set -euo pipefail

UNITE=/etc/systemd/system/coinbosa-validator.service
DD=/var/lib/coinbosa/validator
PW="$DD/bls-pw.txt"
GETH=/opt/coinbosa-chain/build/bin/geth
U=coinbosa-val
RPC=https://explorer.coinbosa.com/rpc
DELAI="${DELAI:-120}"          # secondes accordees a la remontee
ANNULER="${ANNULER:-0}"

ok() { printf '    \033[32mOK\033[0m    %s\n' "$1"; }
ko() { printf '    \033[31mECHEC\033[0m %s\n' "$1"; exit 1; }
[ "$(id -u)" = 0 ] || { echo "À lancer en root (sudo)." >&2; exit 1; }

hauteur() { sudo -u "$U" "$GETH" attach --exec 'eth.blockNumber' "$DD/geth.ipc" 2>/dev/null | tr -cd '0-9'; }

# Attend que la chaine AVANCE. Pas « le service est actif » : un service actif qui
# ne scelle pas est exactement la panne qu'on veut detecter.
attendre_production() {   # $1 = hauteur de depart, $2 = secondes
  local depart="$1" limite="$2" t=0 h
  while [ "$t" -lt "$limite" ]; do
    sleep 5; t=$((t+5))
    h=$(hauteur)
    if [ -n "$h" ] && [ "$h" -gt "$depart" ] 2>/dev/null; then echo "$h"; return 0; fi
  done
  return 1
}

restaurer() {   # remet l'unite d'origine et rallume, quoi qu'il arrive
  local sauv="$1"
  echo
  printf '    \033[31m>>> RETOUR ARRIERE AUTOMATIQUE\033[0m\n'
  cp -a "$sauv" "$UNITE"
  systemctl daemon-reload
  systemctl reset-failed coinbosa-validator 2>/dev/null || true
  systemctl restart coinbosa-validator
  local h0 h1
  h0=$(hauteur); h1=$(attendre_production "${h0:-0}" 90) \
    && echo "    la chaine a repris : bloc $h1 — le vote n'est PAS active, rien d'autre n'a change." \
    || echo "    LA CHAINE N'A PAS REPRIS. Intervenir a la main : journalctl -u coinbosa-validator -n 80"
}

# --- retrait volontaire -----------------------------------------------------
if [ "$ANNULER" = 1 ]; then
  sauv=$(ls -1t "$UNITE".avant-vote-* 2>/dev/null | head -1)
  [ -n "$sauv" ] || ko "aucune sauvegarde de l'unité à restaurer"
  h0=$(hauteur)
  cp -a "$sauv" "$UNITE"; systemctl daemon-reload
  systemctl reset-failed coinbosa-validator 2>/dev/null || true
  systemctl restart coinbosa-validator
  h1=$(attendre_production "${h0:-0}" 90) \
    && { ok "unité restaurée depuis $sauv, chaîne repartie au bloc $h1"; exit 0; } \
    || ko "la chaîne n'a pas repris après restauration — intervenir à la main"
fi

echo "==> Préalables — rien n'est modifié tant qu'ils ne passent pas tous"
[ -f "$UNITE" ] || ko "unité introuvable : $UNITE"
[ -d "$DD/bls/wallet" ] || ko "aucun portefeuille BLS dans $DD/bls/wallet — faire l'étape 2/3 d'abord"
ok "portefeuille BLS présent"

[ -f "$PW" ] || ko "fichier de mot de passe absent : $PW"
n=$(wc -l < "$PW"); c=$(wc -c < "$PW")
[ "$n" -eq 0 ] || ko "$PW contient $n saut(s) de ligne — le nœud REFUSERA de démarrer. Le réécrire avec printf '%s'"
ok "mot de passe : $c octets, 0 saut de ligne"

# Droits. `bls account new` laisse bls/keystore en 755 et son json en 664 — mesure
# du 2026-09-06. Le repertoire du validateur etant en 700, rien n'est lisible de
# l'exterieur AUJOURD'HUI : c'est le parent qui rattrape, pas ces bits-la. On les
# resserre quand meme, parce qu'une restauration ou un chown -R suffirait a lever
# ce rattrapage — et qu'a ce moment-la le compte du noeud RPC public pourrait lire
# la cle de vote chiffree.
for c in "$DD/bls" "$DD/bls/keystore" "$DD/bls/wallet"; do
  [ -e "$c" ] || continue
  m=$(stat -c '%a' "$c"); [ "$m" = 700 ] && continue
  chmod 700 "$c" && ok "$c : $m -> 700"
done
for f in "$DD"/bls/keystore/*.json; do
  [ -e "$f" ] || continue
  m=$(stat -c '%a' "$f"); [ "$m" = 600 ] && continue
  chmod 600 "$f" && ok "$(basename "$f") : $m -> 600"
done
m=$(stat -c '%a' "$DD")
[ "$m" = 700 ] || ko "$DD est en $m au lieu de 700 — la cle de vote chiffree est lisible par d autres comptes de la machine. Corriger AVANT d activer le vote."
ok "repertoire du validateur en 700"

# La preuve qui compte : le portefeuille s'ouvre AVEC CE FICHIER. Le fichier
# n'ayant aucun saut de ligne, la CLI et le nœud lisent la même chaîne — ce
# succès vaut donc pour le démarrage du nœud.
SORTIE=$(sudo -u "$U" "$GETH" bls account list --datadir "$DD" --blspassword "$PW" 2>&1) \
  || ko "le portefeuille NE S'OUVRE PAS avec ce fichier — ne pas redémarrer le validateur"
PUB=$(printf '%s' "$SORTIE" | grep -oE '(0x)?[0-9a-fA-F]{96}' | head -1 | sed 's/^0x//')
[ -n "$PUB" ] || ko "clé publique illisible dans la sortie de bls account list"
ok "le portefeuille s'ouvre — clé ${PUB:0:16}…${PUB: -8}"

# La clé est-elle reconnue PAR LA CHAINE ? On lit l'extraData d'un bloc d'epoch,
# qui est la source dont Parlia tire réellement les VoteAddress.
tete=$(curl -s -X POST -H 'content-type: application/json' -m 20 \
  -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' "$RPC" \
  | sed -n 's/.*"result":"\([^"]*\)".*/\1/p')
case "$tete" in 0x*[!0-9a-fA-Fx]*|''|0x) ko "hauteur illisible depuis $RPC" ;; esac
epoch=$(( (16#${tete#0x}) / 200 * 200 ))
extra=$(curl -s -X POST -H 'content-type: application/json' -m 20 \
  -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_getBlockByNumber\",\"params\":[\"0x$(printf %x $epoch)\",false]}" "$RPC" \
  | sed -n 's/.*"extraData":"0x\([^"]*\)".*/\1/p')
[ -n "$extra" ] || ko "extraData du bloc $epoch illisible"
# vanity(32) + nb(1) + [adresse(20) + cle(48)]...  -> la cle du premier validateur
cle_chaine=$(printf '%s' "$extra" | cut -c $((33*2+20*2+1))-$((33*2+20*2+96)))
if printf '%s' "$cle_chaine" | grep -qE '^0+$'; then
  ko "la chaîne ne porte AUCUNE clé de vote au bloc d'epoch $epoch — faire l'étape 6 (inscription on-chain) puis attendre le bloc d'epoch suivant"
fi
if [ "$(printf '%s' "$cle_chaine" | tr 'A-F' 'a-f')" != "$(printf '%s' "$PUB" | tr 'A-F' 'a-f')" ]; then
  ko "la clé inscrite on-chain (${cle_chaine:0:16}…) DIFFÈRE de celle du portefeuille (${PUB:0:16}…) — ne pas redémarrer"
fi
ok "la clé du portefeuille est bien celle inscrite on-chain (bloc d'epoch $epoch)"

if grep -q -- '--vote' "$UNITE"; then
  ok "les drapeaux sont déjà en place — rien à faire"
  exit 0
fi

# --- modification -----------------------------------------------------------
SAUV="$UNITE.avant-vote-$(date +%F-%H%M%S)"
cp -a "$UNITE" "$SAUV"
ok "sauvegarde de l'unité : $SAUV"

python3 - "$UNITE" "$PW" <<'PY'
import pathlib, sys
p = pathlib.Path(sys.argv[1]); pw = sys.argv[2]; s = p.read_text()
# On s'accroche a --mine, present dans l'ExecStart du validateur et nulle part
# ailleurs. Une ancre unique, verifiee, plutot qu'une insertion a l'aveugle.
assert s.count('--mine') == 1, f"ancre --mine absente ou multiple ({s.count('--mine')})"
s = s.replace('--mine', f'--vote \\\n  --blspassword {pw} \\\n  --mine', 1)
p.write_text(s)
print("    drapeaux --vote et --blspassword ajoutés")
PY

systemd-analyze verify "$UNITE" >/dev/null 2>&1 \
  && ok "unité syntaxiquement valide" \
  || { cp -a "$SAUV" "$UNITE"; ko "unité invalide — fichier d'origine restauré, rien n'a été redémarré"; }

grep -q -- '--vote' "$UNITE" && grep -q -- "--blspassword $PW" "$UNITE" \
  && ok "les deux drapeaux sont présents dans l'unité" \
  || { cp -a "$SAUV" "$UNITE"; ko "les drapeaux ne sont pas là — fichier d'origine restauré"; }

# --- redémarrage, sous surveillance -----------------------------------------
echo
echo "==> Redémarrage du SEUL scelleur de la chaîne"
H0=$(hauteur); echo "    hauteur avant : ${H0:-?}"
systemctl daemon-reload
systemctl reset-failed coinbosa-validator 2>/dev/null || true
systemctl restart coinbosa-validator

echo "    attente de la reprise de production (jusqu'à ${DELAI}s)…"
if H1=$(attendre_production "${H0:-0}" "$DELAI"); then
  ok "la chaîne produit à nouveau : bloc $H1 (elle était à ${H0:-?})"
else
  echo
  printf '    \033[31mECHEC\033[0m la chaîne N A PAS repris en %ss.\n' "$DELAI"
  journalctl -u coinbosa-validator -n 25 --no-pager | sed 's/^/        /'
  restaurer "$SAUV"
  exit 1
fi

echo
echo "==> Le nœud a-t-il chargé la clé ?"
J=$(journalctl -u coinbosa-validator --since "-${DELAI}s" --no-pager 2>/dev/null || true)
manquantes=0
for l in "Read BLS wallet password successfully" "Open BLS wallet successfully" \
         "Initialized keymanager successfully" "Create voteManager successfully"; do
  if printf '%s' "$J" | grep -qF "$l"; then ok "$l"
  else printf '    \033[31mMANQUE\033[0m %s\n' "$l"; manquantes=$((manquantes+1)); fi
done
if [ "$manquantes" -gt 0 ]; then
  echo
  echo "    $manquantes ligne(s) attendue(s) manquante(s). La chaîne TOURNE, mais rien ne"
  echo "    prouve que la clé est chargée. Ces lignes sont de niveau info : vérifier que"
  echo "    le service tourne bien avec --verbosity 3 avant de conclure à une panne."
  echo "    Retour arrière si besoin : sudo ANNULER=1 bash 83-activer-vote.sh"
fi

cat <<TEXTE

==> Étape 9 — attendre 41 blocs

    Le compteur repart de zéro à chaque redémarrage : il faut plus de 40 blocs
    avant que le nœud commence à voter (vote_manager.go:143-147). Environ 3 min 25.

    Puis le contrôle décisif — « finalized » doit cesser de valoir 0 :

      curl -s -X POST -H 'content-type: application/json' \\
        -d '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["finalized",false]}' \\
        $RPC | head -c 120

    Retour arrière : sudo ANNULER=1 bash 83-activer-vote.sh
TEXTE
