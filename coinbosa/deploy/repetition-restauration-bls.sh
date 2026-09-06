#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# repetition-restauration-bls.sh
#
# REPETITION DE RESTAURATION DE LA CLE DE VOTE BLS — sur un datadir jetable.
#
#   GETH=/chemin/vers/geth bash repetition-restauration-bls.sh
#
# POURQUOI CE SCRIPT EXISTE
# -------------------------
# `ACTIVER-LE-VOTE.md § 7.1` dit, mot pour mot, a propos de la sauvegarde :
#
#     « Le keystore seul devrait suffire a reconstituer un portefeuille via
#       `geth bls account import`, mais JE N'AI PAS TESTE CETTE RESTAURATION :
#       sauvegardez les deux. »
#
# Un « devrait suffire » n'est pas une sauvegarde, c'est une esperance. Le jour
# ou l'on restaure, on ne veut pas decouvrir laquelle des deux pieces etait la
# bonne. Ce script remplace l'esperance par une mesure.
#
# CE QU'IL ETABLIT, PIECE PAR PIECE
# ----------------------------------
#   A. creation d'un portefeuille BLS et d'une cle, sur un datadir jetable ;
#   B. sauvegarde des DEUX pieces : le keystore, et le repertoire wallet ;
#   C. destruction integrale ;
#   D. restauration des DEUX pieces -> la cle publique doit revenir identique ;
#   E. destruction, puis restauration du SEUL WALLET -> c'est ce que le noeud
#      ouvre reellement au demarrage (`vote_signer.go`) ; doit suffire ;
#   F. destruction, puis restauration du SEUL KEYSTORE + `bls account import`
#      -> LE CHEMIN NON TESTE. On sait enfin s'il marche, et a quel prix ;
#   G. le piege du saut de ligne : un fichier de mot de passe termine par « \n »
#      est lu differemment par la ligne de commande (premiere ligne) et par le
#      noeud (fichier entier). On le reproduit pour montrer ce qui casse.
#
# RIEN ICI NE TOUCHE A LA PRODUCTION. Aucun reseau, aucun bloc scelle, aucun
# service. Tout se passe dans un repertoire temporaire efface a la sortie.
# ---------------------------------------------------------------------------
set -uo pipefail

GETH="${GETH:-}"
[ -n "$GETH" ] && [ -x "$GETH" ] || { echo "GETH=/chemin/vers/geth est obligatoire." >&2; exit 2; }

# =========================== GARDE-FOUS PRODUCTION ==========================
# Ce script cree et detruit des portefeuilles BLS. Lance par erreur sur le
# serveur, un chemin mal ecrit pourrait effacer celui du validateur. On refuse
# donc de demarrer des qu'une trace de production est visible — la meme garde
# que repetition-restauration.sh, pour la meme raison.
if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet coinbosa-validator 2>/dev/null; then
  echo "REFUS : le service coinbosa-validator tourne sur cette machine." >&2
  exit 2
fi
if [ -d /var/lib/coinbosa/validator ]; then
  echo "REFUS : /var/lib/coinbosa/validator existe — cette machine est un validateur." >&2
  exit 2
fi

LABO=$(mktemp -d -t coinbosa-bls-repetition)
DD="$LABO/datadir"
COFFRE="$LABO/coffre"
PW="$LABO/motdepasse.txt"
MDP='RepetitionBLS-2026!'          # jetable, ne sert qu'ici
mkdir -p "$DD" "$COFFRE"
printf '%s' "$MDP" > "$PW"          # printf '%s' : AUCUN saut de ligne
chmod 600 "$PW"

TENTEES=0; ECHECS=0
titre() { printf '\n--- %s ---\n' "$1"; }
preuve() {  # $1 = ce qu'on etablit, $2 = attendu, $3 = obtenu
  TENTEES=$((TENTEES+1))
  if [ "$2" = "$3" ]; then printf '  [PREUVE OK] %s\n              attendu = %s\n              obtenu  = %s\n' "$1" "$2" "$3"
  else ECHECS=$((ECHECS+1)); printf '  [ECHEC]     %s\n              attendu = %s\n              obtenu  = %s\n' "$1" "$2" "$3"; fi
}
cle_publique() { "$GETH" bls account list --datadir "$DD" --blspassword "$PW" 2>/dev/null \
                  | grep -oE '(0x)?[0-9a-fA-F]{96}' | head -1 | sed 's/^0x//'; }

echo '==============================================================='
echo 'REPETITION DE RESTAURATION — CLE DE VOTE BLS'
echo '==============================================================='
echo "  binaire : $GETH"
echo "  labo    : $LABO"

# --------------------------------------------------------------- A + B ------
titre 'A. creation du portefeuille et de la cle'
"$GETH" bls wallet create --datadir "$DD" --blspassword "$PW" >/dev/null 2>&1
"$GETH" bls account new  --datadir "$DD" --blspassword "$PW" >/dev/null 2>&1
ORIGINE=$(cle_publique)
preuve "une cle BLS existe et le portefeuille s'ouvre" "96" "${#ORIGINE}"
echo "  cle publique : ${ORIGINE:0:16}…${ORIGINE: -8}"

titre 'B. sauvegarde des deux pieces'
cp -a "$DD/bls/keystore" "$COFFRE/keystore"
cp -a "$DD/bls/wallet"   "$COFFRE/wallet"
NB_K=$(find "$COFFRE/keystore" -type f | wc -l | tr -d ' ')
NB_W=$(find "$COFFRE/wallet"   -type f | wc -l | tr -d ' ')
preuve "le coffre contient le keystore" "1" "$((NB_K > 0 ? 1 : 0))"
preuve "le coffre contient le portefeuille" "1" "$((NB_W > 0 ? 1 : 0))"
echo "  keystore : $NB_K fichier(s) ; wallet : $NB_W fichier(s)"

# ------------------------------------------------------------------- D ------
titre 'D. destruction, puis restauration des DEUX pieces'
rm -rf "$DD/bls"
preuve "tout est bien detruit" "" "$(cle_publique)"
mkdir -p "$DD/bls"
cp -a "$COFFRE/keystore" "$DD/bls/keystore"
cp -a "$COFFRE/wallet"   "$DD/bls/wallet"
preuve "la cle publique revient IDENTIQUE" "$ORIGINE" "$(cle_publique)"

# ------------------------------------------------------------------- E ------
# Le noeud n'ouvre QUE le portefeuille (core/vote/vote_signer.go). Si le wallet
# seul suffit, alors c'est LUI la piece vitale, et le keystore est un confort.
titre 'E. le PORTEFEUILLE seul suffit-il ? (c est ce que le noeud ouvre)'
rm -rf "$DD/bls"
mkdir -p "$DD/bls"
cp -a "$COFFRE/wallet" "$DD/bls/wallet"
SANS_KEYSTORE=$(cle_publique)
preuve "sans le keystore, le portefeuille s'ouvre quand meme" "$ORIGINE" "$SANS_KEYSTORE"

# ------------------------------------------------------------------- F ------
# LE CHEMIN QUE LE DOCUMENT DONNE COMME NON TESTE.
titre 'F. le KEYSTORE seul, reimporte — le chemin jamais verifie'
rm -rf "$DD/bls"
mkdir -p "$DD/bls"
cp -a "$COFFRE/keystore" "$DD/bls/keystore"
AVANT_IMPORT=$(cle_publique)
preuve "avec le keystore seul, le portefeuille NE s'ouvre PAS" "" "$AVANT_IMPORT"

FICHIER=$(find "$DD/bls/keystore" -name '*.json' | head -1)
echo "  import de : $(basename "$FICHIER")"
SORTIE_IMPORT=$("$GETH" bls account import --datadir "$DD" \
                  --blspassword "$PW" --importedaccountpassword "$PW" "$FICHIER" 2>&1)
CODE_IMPORT=$?
APRES_IMPORT=$(cle_publique)
preuve "apres 'bls account import', la cle publique revient IDENTIQUE" "$ORIGINE" "$APRES_IMPORT"
if [ "$APRES_IMPORT" != "$ORIGINE" ]; then
  echo "  sortie de l'import (pour diagnostic, code $CODE_IMPORT) :"
  printf '%s\n' "$SORTIE_IMPORT" | tail -6 | sed 's/^/    /'
fi

# ------------------------------------------------------------------- G ------
# Le piege documente en tete de 78-cle-bls.sh, reproduit ici pour qu'il cesse
# d'etre une affirmation et devienne une observation.
titre 'G. le piege du saut de ligne dans le fichier de mot de passe'
PW_SALE="$LABO/motdepasse-avec-saut.txt"
printf '%s\n' "$MDP" > "$PW_SALE"; chmod 600 "$PW_SALE"
preuve "le fichier « sale » contient bien un saut de ligne" "1" "$(wc -l < "$PW_SALE" | tr -d ' ')"
AVEC_SAUT=$("$GETH" bls account list --datadir "$DD" --blspassword "$PW_SALE" 2>/dev/null \
             | grep -oE '(0x)?[0-9a-fA-F]{96}' | head -1 | sed 's/^0x//')
preuve "la LIGNE DE COMMANDE l'accepte (elle ne lit que la premiere ligne)" "$ORIGINE" "$AVEC_SAUT"
echo "  Le noeud, lui, lit le FICHIER ENTIER (core/vote/vote_signer.go) : il"
echo "  chercherait a ouvrir le portefeuille avec le mot de passe SUIVI d'un saut"
echo "  de ligne, echouerait, et REFUSERAIT DE DEMARRER. Sur une chaine a un seul"
echo "  scelleur, cela arrete la chaine — et l'erreur n'apparait qu'au redemarrage."
echo "  C'est pourquoi 78-cle-bls.sh ecrit avec printf '%s' et verifie ensuite."

# ---------------------------------------------------------------- BILAN -----
echo
echo '==============================================================='
echo 'BILAN'
echo '==============================================================='
printf '  preuves tentees : %s\n  echecs          : %s\n' "$TENTEES" "$ECHECS"
echo
if [ "$ECHECS" -eq 0 ]; then
  echo "  RESULTAT : la restauration de la cle de vote BLS est DEMONTREE."
  echo "             Le portefeuille seul suffit au noeud ; le keystore seul se"
  echo "             reimporte et rend la MEME cle publique. Les deux pieces sont"
  echo "             donc chacune suffisante, et les sauvegarder toutes les deux"
  echo "             est une marge, pas une necessite."
else
  echo "  RESULTAT : $ECHECS preuve(s) ont ECHOUE. Lire le detail ci-dessus AVANT"
  echo "             de se fier a la procedure de sauvegarde BLS."
fi
rm -rf "$LABO"
echo "  labo efface : $LABO"
[ "$ECHECS" -eq 0 ] || exit 1
