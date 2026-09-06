#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — étape 5 : sauvegarder la clé BLS avant de l'inscrire on-chain.
#
#   bash 82-sauvegarde-bls.sh            # depuis le POSTE DE L'ÉDITEUR, pas le serveur
#   COFFRE=~/ailleurs bash 82-sauvegarde-bls.sh
#
# À LANCER SUR LE MAC. Le principe de `SAUVEGARDE-CLE.md § 2` est que la pièce
# chiffrée (A) et le mot de passe (B) ne voyagent ni ne se rangent jamais
# ensemble. Ce script ne touche QUE la pièce A. Le mot de passe reste où il est :
# dans ta tête et sur ton papier. Il n'est ni lu, ni copié, ni demandé.
#
# CE QU'IL EMPORTE — et pourquoi les deux
# ---------------------------------------
#   A1  bls/keystore/keystore-<petnom>.json   le keystore chiffré
#   A2  bls/wallet/                           le portefeuille, chiffré lui aussi
#
# `bls account new` écrit le keystore PUIS l'importe dans le portefeuille. Le nœud,
# lui, n'ouvre que le PORTEFEUILLE. Le keystore seul devrait suffire à le
# reconstituer via `geth bls account import`, mais cette restauration n'a pas été
# testée — donc on emporte les deux, et on ne parie pas.
#
# CE QUE CETTE SAUVEGARDE VAUT, EXACTEMENT
# ----------------------------------------
# Moins que celle de la clé de scellage, et il faut le dire clairement. La clé de
# scellage perdue, c'est la fin de la chaîne : son adresse est gravée dans le bloc 0.
# La clé BLS perdue, c'est une gêne : on en regénère une et on la réinscrit par une
# transaction du gouverneur. Cette sauvegarde évite un redémarrage du seul scelleur
# et une transaction — elle n'évite pas une catastrophe, parce qu'il n'y en a pas.
#
# En revanche la règle de SÉPARATION garde toute sa force, pour une autre raison :
# c'est la seule chose qui empêche un VOL. Et une clé de vote volée détruit en
# silence la valeur probante de `finalized`, sans qu'aucune alarme ne se déclenche.
# ---------------------------------------------------------------------------
set -euo pipefail

HOTE="${HOTE:-coinbosa-vps}"
DD=/var/lib/coinbosa/validator
COFFRE="${COFFRE:-$HOME/coffre-bls-A}"

ok() { printf '    \033[32mOK\033[0m    %s\n' "$1"; }
ko() { printf '    \033[31mECHEC\033[0m %s\n' "$1"; exit 1; }

[ "$(id -u)" != 0 ] || ko "à lancer avec TON compte, pas en root — le coffre t'appartient"
command -v ssh >/dev/null || ko "ssh introuvable"

echo "==> Ce qui est sur le serveur"
LISTE=$(ssh "$HOTE" "sudo find $DD/bls -type f -printf '%P\t%s\n'" 2>/dev/null) \
  || ko "impossible de lire $DD/bls sur $HOTE"
[ -n "$LISTE" ] || ko "aucun fichier sous $DD/bls — la clé n'a pas été créée"
printf '%s\n' "$LISTE" | sed 's/^/    /'
NB=$(printf '%s\n' "$LISTE" | wc -l | tr -d ' ')
ok "$NB fichier(s) à sauvegarder"

echo
echo "==> Empreintes AVANT copie (relevées sur le serveur)"
AVANT=$(ssh "$HOTE" "sudo find $DD/bls -type f -exec sha256sum {} + | sed 's#$DD/bls/##' | sort -k2")
printf '%s\n' "$AVANT" | sed 's/^/    /'

echo
echo "==> Copie vers $COFFRE"
[ -e "$COFFRE" ] && ko "$COFFRE existe déjà — ne pas écraser une sauvegarde. Choisis COFFRE=..."
mkdir -p "$COFFRE"; chmod 700 "$COFFRE"
# tar préserve l'arborescence du portefeuille, que `scp` d'un fichier ne rendrait pas.
ssh "$HOTE" "sudo tar -C $DD -cf - bls" | tar -C "$COFFRE" -xf - \
  || ko "la copie a échoué — $COFFRE est incomplet, l'effacer avant de recommencer"
chmod -R go-rwx "$COFFRE"
ok "copie faite"

echo
echo "==> Preuve — le coffre est identique à l'original"
# Une sauvegarde qu'on n'a pas relue n'est pas une sauvegarde. On recompte, on
# recompare, et on refuse de dire « fait » sur autre chose que des empreintes égales.
APRES=$(cd "$COFFRE/bls" && find . -type f -exec shasum -a 256 {} + | sed 's#\./##' | awk '{print $1"  "$2}' | sort -k2)
NORM_AV=$(printf '%s\n' "$AVANT" | awk '{print $1"  "$2}' | sort -k2)
if [ "$NORM_AV" = "$APRES" ]; then
  ok "$NB empreinte(s) identiques — la copie est fidèle"
else
  echo "    --- serveur ---"; printf '%s\n' "$NORM_AV" | sed 's/^/    /'
  echo "    --- coffre  ---"; printf '%s\n' "$APRES"  | sed 's/^/    /'
  ko "les empreintes diffèrent — NE PAS considérer cette sauvegarde comme valide"
fi

PUB=$(ssh "$HOTE" "sudo python3 -c \"import json,glob;print(json.load(open(glob.glob('$DD/bls/keystore/*.json')[0]))['pubkey'])\"" 2>/dev/null || true)
[ -n "$PUB" ] && printf '%s\n' "$PUB" > "$COFFRE/cle-publique.txt"

cat <<TEXTE

==> Fait. Le coffre : $COFFRE

    Ce qu'il contient (pièce A, chiffrée) :
      bls/keystore/…json     le keystore
      bls/wallet/…           le portefeuille
      cle-publique.txt       la clé PUBLIQUE, non secrète

    CE QUI RESTE À TA MAIN, et que ce script ne peut pas faire :

      1. Copier ce dossier sur un SECOND support physique, rangé ailleurs.
         Un seul exemplaire sur le Mac n'est pas une sauvegarde : c'est la
         même panne de disque que le serveur, à un jour près.

      2. Transcrire le mot de passe BLS À LA MAIN sur papier, et le ranger
         DANS UN AUTRE LIEU que ce coffre. Jamais les deux ensemble : c'est
         la seule chose qui protège d'un vol.

    Le keystore utilise pbkdf2 (262 144 itérations, HMAC-SHA256), PAS scrypt.
    Contrairement au coffre de la clé de scellage, il n'impose à l'attaquant
    aucun coût en MÉMOIRE — seulement du calcul, qui se parallélise très bien
    sur carte graphique. Un mot de passe court ou devinable y résiste donc
    beaucoup moins bien. C'est une raison de plus de ne pas ranger le papier
    à côté du coffre.
TEXTE
