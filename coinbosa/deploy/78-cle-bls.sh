#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — créer la clé BLS de vote. Étapes 1 à 4 de ACTIVER-LE-VOTE.md.
#
#   sudo bash 78-cle-bls.sh
#
# À LANCER SUR LE SERVEUR, EN SESSION DIRECTE. Le mot de passe est demandé à
# l'écran, sans écho. Il ne passe par AUCUNE ligne de commande, donc il
# n'apparaît ni dans `ps`, ni dans l'historique du shell, ni dans un journal.
#
# CE QUE CE SCRIPT NE FAIT PAS
# ----------------------------
# Il ne touche NI au service du validateur, NI à la chaîne. Après lui, le nœud
# tourne exactement comme avant : la clé existe sur le disque, elle n'est ni
# déclarée on-chain, ni utilisée. Les étapes 5 à 9 restent à faire, et l'une
# d'elles REDÉMARRE le seul producteur de blocs de la chaîne — c'est le vrai
# risque de l'opération, pas la clé.
#
# LE PIÈGE QUI FAIT TOMBER LE NŒUD, ET QUI EST MESURÉ ICI
# -------------------------------------------------------
# La ligne de commande lit le fichier de mot de passe et n'en garde que la
# PREMIÈRE LIGNE, saut de ligne exclu (`cmd/utils/prompt.go`). Le nœud en
# marche, lui, lit le FICHIER ENTIER, saut de ligne compris
# (`core/vote/vote_signer.go`).
#
# Un fichier terminé par « \n » crée donc un portefeuille protégé par
# « motdepasse », que le nœud tentera d'ouvrir avec « motdepasse\n ». Il échoue,
# et le nœud REFUSE DE DÉMARRER. Sur une chaîne à un seul producteur, cela veut
# dire : la chaîne s'arrête, et l'erreur n'apparaît qu'au redémarrage — bien
# après qu'on ait cru l'étape réussie.
#
# Mesuré sur ce serveur : /var/lib/coinbosa/validator/pw.txt — le mot de passe
# de la clé de SCELLAGE — fait 45 octets et SE TERMINE par un saut de ligne. Il
# ne doit donc jamais être réutilisé ici. Ce script écrit avec `printf '%s'`,
# qui n'ajoute rien, et VÉRIFIE ensuite qu'il n'y a aucun saut de ligne.
#
# Corollaire : si et seulement si le fichier n'en a pas, les deux chemins de
# lecture rendent la même chaîne — et alors le succès de `bls account list`
# devient une PREUVE que le nœud saura ouvrir le portefeuille.
# ---------------------------------------------------------------------------
set -euo pipefail

DD=/var/lib/coinbosa/validator
PW="$DD/bls-pw.txt"
GETH=/opt/coinbosa-chain/build/bin/geth
U=coinbosa-val

ok() { printf '    \033[32mOK\033[0m    %s\n' "$1"; }
ko() { printf '    \033[31mECHEC\033[0m %s\n' "$1"; exit 1; }

[ "$(id -u)" = 0 ] || { echo "À lancer en root (sudo)." >&2; exit 1; }
[ -x "$GETH" ]     || ko "binaire geth introuvable : $GETH"
[ -d "$DD" ]       || ko "répertoire du validateur introuvable : $DD"

echo "==> Préalables"
"$GETH" bls account --help >/dev/null 2>&1 || ko "ce binaire ne gère pas les clés BLS"
ok "le binaire gère les clés BLS"

if [ -d "$DD/bls/wallet" ]; then
  echo
  echo "    Un portefeuille BLS existe DÉJÀ dans $DD/bls/wallet."
  echo "    Ce script ne l'écrasera pas. Si son mot de passe est connu, passez à"
  echo "    l'étape 4 du document. S'il est inconnu, traitez-le comme une perte"
  echo "    (§ 3.3) — n'effacez rien avant d'avoir lu ce paragraphe."
  exit 1
fi
ok "aucun portefeuille BLS existant — départ propre"

[ -f "$PW" ] && ko "$PW existe déjà — vérifiez-le avant de recommencer"

# --- le mot de passe, demandé à l'écran --------------------------------------
echo
echo "==> Mot de passe du portefeuille BLS"
echo "    Contraintes vérifiées dans le code du client :"
echo "      · au moins 10 caractères"
echo "      · uniquement des caractères ASCII imprimables (pas d'accent, pas d'emoji)"
echo "    Il ne s'affichera pas. Gardez-le : sans lui, la clé est inutilisable."
echo
printf "    Mot de passe        : "; read -rs MDP; echo
printf "    Confirmez           : "; read -rs MDP2; echo
echo

[ "$MDP" = "$MDP2" ] || ko "les deux saisies diffèrent"
unset MDP2
[ ${#MDP} -ge 10 ]   || ko "trop court : ${#MDP} caractères, 10 au minimum"
case "$MDP" in
  *[!\ -~]*) ko "contient un caractère non ASCII imprimable (accent, emoji, tabulation…)" ;;
esac
ok "mot de passe accepté (${#MDP} caractères, ASCII imprimable)"

# --- écriture SANS saut de ligne ---------------------------------------------
# printf '%s' n'ajoute rien. On écrit depuis CE processus : le secret ne passe
# par aucun argument de commande, donc jamais par `ps`.
umask 077
printf '%s' "$MDP" > "$PW"
unset MDP
chown "$U:$U" "$PW"
chmod 600 "$PW"

n=$(wc -l < "$PW"); c=$(wc -c < "$PW")
[ "$n" -eq 0 ]  || ko "le fichier contient $n saut(s) de ligne — refaites-le, NE POURSUIVEZ PAS"
[ "$c" -ge 10 ] || ko "le fichier ne fait que $c octets"
ok "fichier écrit : $c octets, 0 saut de ligne, $(stat -c '%a %U:%G' "$PW")"

# --- portefeuille puis compte -------------------------------------------------
echo
echo "==> Portefeuille BLS"
sudo -u "$U" "$GETH" bls wallet create --datadir "$DD" --blspassword "$PW" 2>&1 \
  | sed 's/^/    /' || ko "création du portefeuille refusée"
[ -d "$DD/bls/wallet" ] || ko "le portefeuille n'a pas été créé"
ok "portefeuille créé : $DD/bls/wallet"

echo
echo "==> Clé BLS"
sudo -u "$U" "$GETH" bls account new --datadir "$DD" --blspassword "$PW" 2>&1 \
  | sed 's/^/    /' || ko "création de la clé refusée"

# --- la preuve ----------------------------------------------------------------
# `bls account list` ouvre le portefeuille avec le mot de passe lu comme le fait
# la CLI. Le fichier n'ayant AUCUN saut de ligne, la CLI et le nœud lisent la
# même chaîne : ce succès prouve donc que le nœud saura l'ouvrir au démarrage.
echo
echo "==> Preuve — le portefeuille s'ouvre"
SORTIE=$(sudo -u "$U" "$GETH" bls account list --datadir "$DD" --blspassword "$PW" 2>&1) \
  || ko "le portefeuille ne s'ouvre pas — NE REDÉMARREZ PAS le validateur"
printf '%s\n' "$SORTIE" | sed 's/^/    /'
ok "le portefeuille s'ouvre avec ce fichier — le nœud le pourra aussi"

PUB=$(printf '%s' "$SORTIE" | grep -oE '0x[0-9a-fA-F]{96}' | head -1)
echo
if [ -n "$PUB" ]; then
  echo "==> Clé PUBLIQUE (non secrète — c'est elle qu'on inscrit on-chain)"
  echo "    $PUB"
else
  echo "==> Clé publique : non reconnue automatiquement dans la sortie ci-dessus."
  echo "    Relevez-la à la main ; elle fait 96 caractères hexadécimaux."
fi

cat <<'FIN'

==> Fait. Le validateur n'a PAS été touché : il tourne comme avant.

    CE QUI RESTE, et l'ordre compte :
      5. SAUVEGARDER — le keystore ET le répertoire bls/wallet, plus le mot de
         passe, hors de cette machine. Voir ACTIVER-LE-VOTE.md § 7.
      6. Inscrire la clé publique on-chain (transaction depuis le gouverneur).
      7. Attendre le bloc d'epoch et vérifier le basculement.
      8. Ajouter les deux drapeaux au service — CETTE ÉTAPE REDÉMARRE LE SEUL
         PRODUCTEUR DE BLOCS. C'est le vrai risque, pas la clé.
      9. Attendre 41 blocs, puis vérifier que « finalized » avance.

    Ne faites pas l'étape 8 avant d'avoir fait la 5.
FIN
