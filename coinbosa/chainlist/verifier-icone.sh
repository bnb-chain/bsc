#!/usr/bin/env bash
# Vérifie que l'icône référencée est RÉELLEMENT servie par le réseau IPFS public,
# et qu'elle correspond bien au logo officiel.
#
#   bash verifier-icone.sh
#
# À lancer AVANT d'ouvrir la pull request. Le registre ethereum-lists/chains est public
# et durable : y publier un CID que personne ne peut résoudre afficherait une icône
# cassée dans tous les portefeuilles, sans moyen simple de la corriger ensuite.
set -uo pipefail
cd "$(dirname "$0")"

CID=$(sed -n 's/.*"ipfs:\/\/\([^"]*\)".*/\1/p' coinbosa.json)
[ -n "$CID" ] || { echo "aucun CID dans coinbosa.json" >&2; exit 1; }
ATTENDU=$(shasum -a 256 coinbosa-96.png 2>/dev/null | cut -d' ' -f1 || sha256sum coinbosa-96.png | cut -d' ' -f1)

echo "  CID       : $CID"
echo "  empreinte : ${ATTENDU:0:40}"
echo ""

for g in https://ipfs.io/ipfs https://dweb.link/ipfs https://w3s.link/ipfs https://gateway.pinata.cloud/ipfs; do
  code=$(curl -sS -L -o /tmp/icone-verif -w '%{http_code}' --max-time 45 "$g/$CID" 2>/dev/null || echo 000)
  obtenu=$(shasum -a 256 /tmp/icone-verif 2>/dev/null | cut -d' ' -f1 || sha256sum /tmp/icone-verif 2>/dev/null | cut -d' ' -f1)
  if [ "$code" = "200" ] && [ "$obtenu" = "$ATTENDU" ]; then
    echo "  ✓ $g — servie et conforme au logo officiel"
    rm -f /tmp/icone-verif
    echo ""
    echo "  L'icône est publiquement résolvable. La pull request peut être ouverte."
    exit 0
  fi
  printf "  ✗ %-38s HTTP %s\n" "$g" "$code"
done
rm -f /tmp/icone-verif

cat <<'AIDE'

  AUCUNE passerelle ne sert ce CID. Ne pas ouvrir la pull request en l'état.

  Cause la plus fréquente : le fichier a été téléversé en PRIVÉ. Pinata place les
  nouveaux fichiers dans un espace privé par défaut, et un fichier privé n'est jamais
  servi par les passerelles publiques — donc jamais visible dans un portefeuille.

  Sur pinata.cloud :
    · ouvrir le fichier dans « Files » ;
    · vérifier que le réseau est PUBLIC (et non « Private ») ;
    · si besoin, le re-téléverser en choisissant explicitement le réseau public ;
    · vérifier aussi qu'il est bien « Pinned » — un fichier non épinglé finit par
      disparaître, et le logo disparaîtrait des portefeuilles avec lui.

  Le CID change lorsqu'on re-téléverse en public : reporter le nouveau dans
  coinbosa.json, puis relancer ce script.
AIDE
exit 1
