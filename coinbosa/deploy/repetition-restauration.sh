#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# repetition-restauration.sh
#
# REPETITION DE RESTAURATION DE LA CLE DE SCELLAGE — sur une chaine JETABLE.
#
# Une sauvegarde qui n'a jamais ete restauree n'est pas une sauvegarde : c'est
# une hypothese. Ce script transforme l'hypothese en fait mesure. Il :
#
#   A. fabrique une chaine de laboratoire (meme moteur Parlia qu'en production)
#      avec un validateur dont il genere lui-meme la cle ;
#   B. la fait sceller pendant quelques blocs ;
#   C. en prend une sauvegarde selon la procedure de SAUVEGARDE-CLE.md
#      (coffre et mot de passe dans DEUX endroits distincts) ;
#   D. VERIFIE la sauvegarde hors production, sans la brancher sur la chaine ;
#   E. DETRUIT integralement le repertoire du validateur ;
#   F. restaure depuis la sauvegarde seule ;
#   G. PROUVE que la chaine repart au meme bloc, sur la meme chaine, scellee par
#      la meme adresse ;
#   H. rejoue le scenario reel le plus probable : la chaine est intacte, seul le
#      coffre a disparu.
#
# RIEN ICI NE TOUCHE A LA PRODUCTION. Tout se passe dans un repertoire jetable,
# sur un chainId different (26999), sans decouverte de pairs, sans reseau.
#
# Usage :
#   GETH=/chemin/vers/geth bash repetition-restauration.sh
#
# Variables :
#   GETH      chemin du binaire geth compile depuis CE depot   (obligatoire)
#   COINBOSA  racine du dossier coinbosa/ du depot             (deduit par defaut)
#   LABO      repertoire de travail jetable                    (cree par defaut)
#
# Code de sortie : 0 = toutes les preuves obtenues, 1 = au moins une a echoue.
# ---------------------------------------------------------------------------
set -uo pipefail

# =========================== GARDE-FOUS PRODUCTION ==========================
# Ce script lance un noeud qui SCELLE des blocs. Lance par erreur sur la machine
# du validateur, il pourrait entrer en concurrence avec la production. On refuse
# donc de demarrer si la moindre trace de production est visible.
if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet coinbosa-validator 2>/dev/null; then
  echo "REFUS : le service coinbosa-validator tourne sur cette machine." >&2
  echo "        Cette repetition ne doit JAMAIS etre jouee sur le serveur de production." >&2
  exit 2
fi
if [ -d /var/lib/coinbosa/validator ]; then
  echo "REFUS : /var/lib/coinbosa/validator existe — cette machine est un validateur." >&2
  exit 2
fi

# ================================ REGLAGES =================================
GETH="${GETH:-}"
ICI="$(cd "$(dirname "$0")" && pwd)"
COINBOSA="${COINBOSA:-$(cd "$ICI/.." && pwd)}"
LABO="${LABO:-$(mktemp -d "${TMPDIR:-/tmp}/coinbosa-repetition-XXXXXX")}"

# chainId DIFFERENT de la production (26262). Une chaine de laboratoire ne doit
# jamais pouvoir etre confondue avec le reseau reel, ni par un outil, ni par un
# humain qui relit un journal six mois plus tard.
CHAIN_LABO=26999
PORT_P2P=30399
PORT_HTTP=8599
BLOCS_AVANT_SAUVEGARDE=8      # hauteur minimale a atteindre avant de sauvegarder
BLOCS_APRES_RESTAURATION=3    # blocs neufs exiges pour prouver que le scellage reprend

DATADIR="$LABO/validateur"
COFFRE_A="$LABO/sauvegarde-A-coffre"        # le keystore chiffre, et RIEN d'autre
COFFRE_B="$LABO/sauvegarde-B-motdepasse"    # le mot de passe, et RIEN d'autre
SAUV_CHAINE="$LABO/sauvegarde-C-chaine"     # la chaine (equivalent du froid)

[ -n "$GETH" ] || { echo "REFUS : variable GETH non definie (chemin du binaire geth)." >&2; exit 2; }
[ -x "$GETH" ] || { echo "REFUS : $GETH n'est pas un binaire executable." >&2; exit 2; }
[ -f "$COINBOSA/scripts/build-genesis.js" ] || { echo "REFUS : $COINBOSA/scripts/build-genesis.js introuvable." >&2; exit 2; }
command -v node >/dev/null 2>&1 || { echo "REFUS : node est requis." >&2; exit 2; }

# ============================== OUTILLAGE ==================================
ECHECS=0
PREUVES=0

titre() { echo; echo "==============================================================="; echo "$*"; echo "==============================================================="; }
etape() { echo; echo "--- $* ---"; }

# sha256 portable (Linux : sha256sum ; macOS : shasum -a 256)
sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  else shasum -a 256 "$1" | awk '{print $1}'; fi
}

preuve_egale() {  # preuve_egale "libelle" attendu obtenu
  PREUVES=$((PREUVES+1))
  if [ "$2" = "$3" ]; then
    printf "  [PREUVE OK] %s\n              attendu = %s\n              obtenu  = %s\n" "$1" "$2" "$3"
  else
    printf "  [ECHEC]     %s\n              attendu = %s\n              obtenu  = %s\n" "$1" "$2" "$3"
    ECHECS=$((ECHECS+1))
  fi
}

preuve_min() {  # preuve_min "libelle" seuil valeur
  PREUVES=$((PREUVES+1))
  if [ "$3" -ge "$2" ] 2>/dev/null; then
    printf "  [PREUVE OK] %s\n              critere >= %s   mesure = %s\n" "$1" "$2" "$3"
  else
    printf "  [ECHEC]     %s\n              critere >= %s   mesure = %s\n" "$1" "$2" "$3"
    ECHECS=$((ECHECS+1))
  fi
}

rpc() {  # rpc methode params_json
  curl -s --max-time 8 -X POST -H 'Content-Type: application/json' \
    --data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$1\",\"params\":$2}" \
    "http://127.0.0.1:$PORT_HTTP" 2>/dev/null
}

hauteur() {
  local r; r="$(rpc eth_blockNumber '[]')"
  echo "$r" | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{try{const j=JSON.parse(s);console.log(j.result?parseInt(j.result,16):-1)}catch(e){console.log(-1)}})'
}

champ_bloc() {  # champ_bloc <numero_decimal|latest> <champ>
  local n="$1"
  if [ "$n" != "latest" ]; then n="0x$(printf '%x' "$n")"; else n='latest'; fi
  rpc eth_getBlockByNumber "[\"$n\",false]" | \
    node -e 'let s="";const f=process.argv[1];process.stdin.on("data",d=>s+=d).on("end",()=>{try{const j=JSON.parse(s);console.log(j.result?j.result[f]:"ABSENT")}catch(e){console.log("ABSENT")}})' "$2"
}

demarrer_noeud() {  # demarrer_noeud <etiquette-du-journal>
  "$GETH" --datadir "$DATADIR" \
    --networkid "$CHAIN_LABO" \
    --port "$PORT_P2P" --nodiscover --maxpeers 0 --nat none --netrestrict 127.0.0.0/8 \
    --ipcdisable \
    --http --http.addr 127.0.0.1 --http.port "$PORT_HTTP" --http.api eth,net,web3,miner \
    --mine --miner.etherbase "$VALIDATEUR" --miner.gaslimit 40000000 \
    --unlock "$VALIDATEUR" --password "$DATADIR/pw.txt" --allow-insecure-unlock \
    --syncmode full --gcmode full --pathdb.sync \
    --verbosity 3 > "$LABO/geth-$1.log" 2>&1 &
  echo $! > "$LABO/geth.pid"
}

attendre_rpc() {  # attendre_rpc <secondes>
  local n=0
  while [ "$n" -lt "$1" ]; do
    if [ "$(hauteur)" != "-1" ]; then return 0; fi
    sleep 1; n=$((n+1))
  done
  return 1
}

attendre_hauteur() {  # attendre_hauteur <cible> <secondes>
  local n=0 h
  while [ "$n" -lt "$2" ]; do
    h="$(hauteur)"
    if [ "$h" != "-1" ] && [ "$h" -ge "$1" ] 2>/dev/null; then return 0; fi
    sleep 1; n=$((n+1))
  done
  return 1
}

arreter_noeud() {
  # ARRET PROPRE, exactement comme le service systemd de production : geth garde
  # de l'etat en memoire, un SIGKILL le ferait repartir en arriere.
  [ -f "$LABO/geth.pid" ] || return 0
  local pid; pid="$(cat "$LABO/geth.pid")"
  kill -0 "$pid" 2>/dev/null || { rm -f "$LABO/geth.pid"; return 0; }
  kill -INT "$pid" 2>/dev/null
  local n=0
  while [ "$n" -lt 120 ]; do
    kill -0 "$pid" 2>/dev/null || { rm -f "$LABO/geth.pid"; return 0; }
    sleep 1; n=$((n+1))
  done
  echo "  ATTENTION : le noeud n'a pas rendu la main en 120 s, arret force."
  kill -9 "$pid" 2>/dev/null
  rm -f "$LABO/geth.pid"
}

nettoyer() { arreter_noeud 2>/dev/null; }
trap nettoyer EXIT INT TERM

titre "REPETITION DE RESTAURATION DE LA CLE DE SCELLAGE"
echo "date          : $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo "machine       : $(uname -s) $(uname -m)"
echo "geth          : $GETH"
echo "version geth  : $("$GETH" version 2>/dev/null | grep -i '^Version' | head -1)"
echo "laboratoire   : $LABO"
echo "chainId labo  : $CHAIN_LABO   (la production est 26262 — aucune confusion possible)"

# ===================================================================
titre "A. MISE EN PLACE — une cle de scellage jetable, generee ici"
# ===================================================================
mkdir -p "$DATADIR"
chmod 700 "$DATADIR"

etape "A.1 mot de passe jetable (jamais affiche)"
# 33 octets aleatoires en base64 = 44 caracteres, la meme forme que le mot de
# passe de production. Il est ecrit directement dans le fichier : il ne passe ni
# par la sortie standard, ni par la ligne de commande, ni par l'historique.
umask 077
openssl rand -base64 33 > "$DATADIR/pw.txt"
chmod 400 "$DATADIR/pw.txt"
echo "  ecrit : $DATADIR/pw.txt  ($(wc -c < "$DATADIR/pw.txt" | tr -d ' ') octets, droits 400)"
echo "  (contenu volontairement non affiche)"

etape "A.2 creation du coffre"
SORTIE_COMPTE="$("$GETH" account new --datadir "$DATADIR" --password "$DATADIR/pw.txt" 2>&1)"
VALIDATEUR="$(echo "$SORTIE_COMPTE" | grep -oi '0x[0-9a-fA-F]\{40\}' | head -1)"
if [ -z "$VALIDATEUR" ]; then
  echo "ECHEC : impossible de creer le compte."; echo "$SORTIE_COMPTE"; exit 1
fi
FICHIER_COFFRE="$(find "$DATADIR/keystore" -name 'UTC--*' -type f | head -1)"
echo "  validateur jetable : $VALIDATEUR"
echo "  coffre             : $FICHIER_COFFRE"
echo "  taille             : $(wc -c < "$FICHIER_COFFRE" | tr -d ' ') octets"
# On lit le fichier et on n'en extrait QUE les parametres de chiffrement.
# Surtout pas de require() ici : le fichier n'a pas d'extension .json, node le
# prendrait pour du JavaScript et recracherait tout le coffre a l'ecran.
echo "  parametres         : $(node -e 'const fs=require("fs");const j=JSON.parse(fs.readFileSync(process.argv[1],"utf8"));const c=j.crypto||j.Crypto;console.log("kdf="+c.kdf+" N="+c.kdfparams.n+" r="+c.kdfparams.r+" p="+c.kdfparams.p+" dklen="+c.kdfparams.dklen)' "$FICHIER_COFFRE" 2>/dev/null)"

etape "A.3 genesis de laboratoire (moteur Parlia, comme en production)"
VALIDATOR="$VALIDATEUR" ALLOW_DEV=1 OUT="$LABO/genesis-labo.json" \
  node "$COINBOSA/scripts/build-genesis.js" > "$LABO/genesis.log" 2>&1
if [ ! -f "$LABO/genesis-labo.json" ]; then
  echo "ECHEC : genesis non genere."; tail -20 "$LABO/genesis.log"; exit 1
fi
# On force le chainId du laboratoire. C'est la barriere qui garantit qu'un noeud
# de test ne pourra jamais dialoguer avec le reseau reel ni etre pris pour lui.
node -e '
const fs=require("fs");const p=process.argv[1];const g=JSON.parse(fs.readFileSync(p,"utf8"));
g.config.chainId=parseInt(process.argv[2],10);
fs.writeFileSync(p,JSON.stringify(g,null,2));
console.log("  chainId force a "+g.config.chainId+", extraData "+((g.extraData.length-2)/2)+" octets, "+Object.keys(g.alloc).length+" comptes");
' "$LABO/genesis-labo.json" "$CHAIN_LABO"

etape "A.4 initialisation"
"$GETH" init --datadir "$DATADIR" "$LABO/genesis-labo.json" > "$LABO/init-1.log" 2>&1
grep -i "genesis block hash\|Successfully wrote" "$LABO/init-1.log" | head -3 | sed 's/^/  /'

# ===================================================================
titre "B. LA CHAINE SCELLE"
# ===================================================================
demarrer_noeud "1-avant"
if ! attendre_rpc 60; then
  echo "ECHEC : le noeud n'a pas ouvert son RPC en 60 s."; tail -30 "$LABO/geth-1-avant.log"; exit 1
fi
echo "  RPC ouvert."
echo "  attente de $BLOCS_AVANT_SAUVEGARDE blocs (5 s par bloc, ~$((BLOCS_AVANT_SAUVEGARDE*5)) s)..."
if ! attendre_hauteur "$BLOCS_AVANT_SAUVEGARDE" 180; then
  echo "ECHEC : la chaine de laboratoire ne produit pas de blocs."
  tail -40 "$LABO/geth-1-avant.log"; exit 1
fi

GENESE_HASH="$(champ_bloc 0 hash)"
HAUTEUR_AVANT="$(hauteur)"
HASH_REPERE="$(champ_bloc "$BLOCS_AVANT_SAUVEGARDE" hash)"
MINEUR_AVANT="$(champ_bloc "$BLOCS_AVANT_SAUVEGARDE" miner)"
echo
echo "  hash du bloc 0                 : $GENESE_HASH"
echo "  hauteur atteinte               : $HAUTEUR_AVANT"
echo "  bloc repere                    : #$BLOCS_AVANT_SAUVEGARDE"
echo "  hash du bloc repere            : $HASH_REPERE"
echo "  scelle par                     : $MINEUR_AVANT"
preuve_egale "avant sauvegarde : c'est bien la cle jetable qui scelle" \
  "$(echo "$VALIDATEUR" | tr 'A-Z' 'a-z')" "$(echo "$MINEUR_AVANT" | tr 'A-Z' 'a-z')"

etape "B.1 arret propre (SIGINT, comme le service de production)"
arreter_noeud
echo "  noeud arrete."

# ===================================================================
titre "C. SAUVEGARDE — trois pieces, trois endroits"
# ===================================================================
mkdir -p "$COFFRE_A" "$COFFRE_B" "$SAUV_CHAINE"
chmod 700 "$COFFRE_A" "$COFFRE_B"

etape "C.1 sauvegarde A — le coffre chiffre, SEUL"
cp "$FICHIER_COFFRE" "$COFFRE_A/"
NOM_COFFRE="$(basename "$FICHIER_COFFRE")"
SHA_COFFRE="$(sha256 "$COFFRE_A/$NOM_COFFRE")"
echo "  $COFFRE_A/$NOM_COFFRE"
echo "  sha256 : $SHA_COFFRE"

etape "C.2 sauvegarde B — le mot de passe, SEUL, ailleurs"
cp "$DATADIR/pw.txt" "$COFFRE_B/motdepasse.txt"
chmod 400 "$COFFRE_B/motdepasse.txt"
SHA_MDP="$(sha256 "$COFFRE_B/motdepasse.txt")"
echo "  $COFFRE_B/motdepasse.txt"
echo "  sha256 : $SHA_MDP"
echo "  Les deux ne sont JAMAIS reunis : reunis, ils valent la cle en clair."

etape "C.3 sauvegarde C — la chaine (sans coffre ni mot de passe)"
tar -czf "$SAUV_CHAINE/chaine.tgz" -C "$DATADIR" \
  --exclude=keystore --exclude=pw.txt --exclude=geth.ipc . 2>/dev/null
cp "$LABO/genesis-labo.json" "$SAUV_CHAINE/genesis.json"
SHA_CHAINE="$(sha256 "$SAUV_CHAINE/chaine.tgz")"
echo "  $SAUV_CHAINE/chaine.tgz  ($(wc -c < "$SAUV_CHAINE/chaine.tgz" | tr -d ' ') octets)"
echo "  sha256 : $SHA_CHAINE"
echo "  verification : l'archive de chaine ne contient NI coffre NI mot de passe"
FUITES="$(tar -tzf "$SAUV_CHAINE/chaine.tgz" 2>/dev/null | grep -c -E 'keystore|pw\.txt|UTC--')"
preuve_egale "l'archive de chaine ne contient aucun secret" "0" "$FUITES"

# ===================================================================
titre "D. VERIFIER LA SAUVEGARDE SANS LA METTRE EN PRODUCTION"
# ===================================================================
echo "C'est le coeur du sujet : on prouve que le couple (coffre A + mot de passe B)"
echo "redonne la bonne cle, en le dechiffrant HORS LIGNE, sans toucher a la chaine."
echo
node "$ICI/verifier-coffre.js" "$COFFRE_A/$NOM_COFFRE" "$COFFRE_B/motdepasse.txt" "$VALIDATEUR"
CODE_VERIF=$?
preuve_egale "verification hors production de la sauvegarde" "0" "$CODE_VERIF"

# ===================================================================
titre "E. DESTRUCTION — on efface tout le repertoire du validateur"
# ===================================================================
echo "  rm -rf $DATADIR"
rm -rf "$DATADIR"
if [ -e "$DATADIR" ]; then EXISTE=oui; else EXISTE=non; fi
preuve_egale "le repertoire du validateur n'existe plus" "non" "$EXISTE"
echo "  A cet instant, il ne reste que les trois sauvegardes. Rien d'autre."

# ===================================================================
titre "F. RESTAURATION depuis les seules sauvegardes"
# ===================================================================
etape "F.1 la chaine"
mkdir -p "$DATADIR"; chmod 700 "$DATADIR"
tar -xzf "$SAUV_CHAINE/chaine.tgz" -C "$DATADIR"
echo "  chaine restauree depuis $SAUV_CHAINE/chaine.tgz"

etape "F.2 le coffre (sauvegarde A)"
mkdir -p "$DATADIR/keystore"; chmod 700 "$DATADIR/keystore"
cp "$COFFRE_A/$NOM_COFFRE" "$DATADIR/keystore/"
chmod 600 "$DATADIR/keystore/$NOM_COFFRE"
preuve_egale "le coffre restaure est bit pour bit celui sauvegarde" \
  "$SHA_COFFRE" "$(sha256 "$DATADIR/keystore/$NOM_COFFRE")"

etape "F.3 le mot de passe (sauvegarde B)"
cp "$COFFRE_B/motdepasse.txt" "$DATADIR/pw.txt"
chmod 400 "$DATADIR/pw.txt"
preuve_egale "le mot de passe restaure est bit pour bit celui sauvegarde" \
  "$SHA_MDP" "$(sha256 "$DATADIR/pw.txt")"

# ===================================================================
titre "G. PREUVE — la chaine reprend au meme bloc, avec la meme adresse"
# ===================================================================
demarrer_noeud "2-apres"
if ! attendre_rpc 60; then
  echo "ECHEC : le noeud restaure n'a pas ouvert son RPC en 60 s."
  tail -40 "$LABO/geth-2-apres.log"
  ECHECS=$((ECHECS+1))
else
  GENESE_APRES="$(champ_bloc 0 hash)"
  HAUTEUR_REPRISE="$(hauteur)"
  HASH_REPERE_APRES="$(champ_bloc "$BLOCS_AVANT_SAUVEGARDE" hash)"

  echo "  hash du bloc 0 apres restauration : $GENESE_APRES"
  echo "  hauteur a la reprise              : $HAUTEUR_REPRISE"
  echo "  hash du bloc repere apres         : $HASH_REPERE_APRES"
  echo

  preuve_egale "meme reseau : le bloc 0 est identique" "$GENESE_HASH" "$GENESE_APRES"
  preuve_egale "meme chaine : le bloc repere #$BLOCS_AVANT_SAUVEGARDE a le meme hash (pas un embranchement)" \
    "$HASH_REPERE" "$HASH_REPERE_APRES"
  preuve_min "meme hauteur : la chaine repart d'ou elle s'etait arretee" \
    "$HAUTEUR_AVANT" "$HAUTEUR_REPRISE"

  CIBLE=$((HAUTEUR_REPRISE + BLOCS_APRES_RESTAURATION))
  echo
  echo "  attente de $BLOCS_APRES_RESTAURATION blocs NEUFS (cible : hauteur $CIBLE)..."
  if attendre_hauteur "$CIBLE" 120; then
    HAUTEUR_FIN="$(hauteur)"
    MINEUR_APRES="$(champ_bloc latest miner)"
    preuve_min "le scellage a REPRIS : de nouveaux blocs sont produits" "$CIBLE" "$HAUTEUR_FIN"
    preuve_egale "meme adresse : les blocs neufs sont scelles par la cle restauree" \
      "$(echo "$VALIDATEUR" | tr 'A-Z' 'a-z')" "$(echo "$MINEUR_APRES" | tr 'A-Z' 'a-z')"
  else
    echo "  [ECHEC] aucun bloc neuf en 120 s apres restauration."
    tail -30 "$LABO/geth-2-apres.log"
    ECHECS=$((ECHECS+1)); PREUVES=$((PREUVES+2))
  fi
fi
arreter_noeud

# ===================================================================
titre "H. LE SCENARIO REEL : la chaine survit, le coffre disparait"
# ===================================================================
echo "C'est exactement le risque de la production Coinbosa aujourd'hui : le"
echo "chaindata est sauvegarde (froid), le coffre ne l'est pas."
echo

etape "H.1 on efface UNIQUEMENT le coffre — le mot de passe reste en place"
# On n'efface QUE le keystore. Effacer aussi pw.txt serait une preuve molle :
# geth s'arreterait des la lecture du fichier de mot de passe, sans jamais
# chercher la cle, et on ne prouverait rien sur le coffre lui-meme.
# Ici le mot de passe est bien la : le seul element manquant est le coffre.
rm -rf "$DATADIR/keystore"
if [ -f "$DATADIR/pw.txt" ]; then MDP_PRESENT=oui; else MDP_PRESENT=non; fi
preuve_egale "le mot de passe est toujours en place (seul le coffre manque)" "oui" "$MDP_PRESENT"
echo "  keystore/ supprime ; pw.txt conserve ; la chaine intacte."

etape "H.2 le noeud est-il capable de sceller sans le coffre ?"
# On relance dans les memes conditions. On attend un ECHEC : c'est ce qui
# demontre que le coffre est bien le point unique de defaillance.
"$GETH" --datadir "$DATADIR" --networkid "$CHAIN_LABO" \
  --port "$PORT_P2P" --nodiscover --maxpeers 0 --nat none --netrestrict 127.0.0.0/8 \
  --ipcdisable \
  --http --http.addr 127.0.0.1 --http.port "$PORT_HTTP" --http.api eth,net,web3 \
  --mine --miner.etherbase "$VALIDATEUR" \
  --unlock "$VALIDATEUR" --password "$DATADIR/pw.txt" --allow-insecure-unlock \
  --syncmode full --gcmode full --verbosity 3 > "$LABO/geth-3-sans-coffre.log" 2>&1 &
PID_SANS=$!
n=0; MORT=non
while [ "$n" -lt 30 ]; do
  kill -0 "$PID_SANS" 2>/dev/null || { MORT=oui; break; }
  sleep 1; n=$((n+1))
done
if [ "$MORT" = "non" ]; then kill -9 "$PID_SANS" 2>/dev/null; fi
preuve_egale "sans le coffre, le noeud REFUSE de demarrer (le coffre est bien vital)" "oui" "$MORT"
echo "  message de geth :"
grep -i -m2 "password\|unlock\|no key\|Fatal" "$LABO/geth-3-sans-coffre.log" | sed 's/^/    /'

etape "H.3 restauration du seul coffre, depuis la sauvegarde A"
mkdir -p "$DATADIR/keystore"; chmod 700 "$DATADIR/keystore"
cp "$COFFRE_A/$NOM_COFFRE" "$DATADIR/keystore/"; chmod 600 "$DATADIR/keystore/$NOM_COFFRE"
# Le mot de passe n'a pas ete touche : on remet le coffre, et rien d'autre.
demarrer_noeud "4-recouvre"
if attendre_rpc 60; then
  H_AVANT_FIN="$(hauteur)"
  CIBLE2=$((H_AVANT_FIN + BLOCS_APRES_RESTAURATION))
  echo "  hauteur a la reprise : $H_AVANT_FIN — attente de $BLOCS_APRES_RESTAURATION blocs neufs..."
  if attendre_hauteur "$CIBLE2" 120; then
    preuve_min "apres restauration du seul coffre, la chaine reproduit des blocs" "$CIBLE2" "$(hauteur)"
    preuve_egale "et toujours avec la meme adresse" \
      "$(echo "$VALIDATEUR" | tr 'A-Z' 'a-z')" "$(echo "$(champ_bloc latest miner)" | tr 'A-Z' 'a-z')"
  else
    echo "  [ECHEC] pas de bloc neuf apres restauration du coffre."
    tail -30 "$LABO/geth-4-recouvre.log"; ECHECS=$((ECHECS+1)); PREUVES=$((PREUVES+2))
  fi
else
  echo "  [ECHEC] RPC indisponible apres restauration du coffre."
  tail -30 "$LABO/geth-4-recouvre.log"; ECHECS=$((ECHECS+2)); PREUVES=$((PREUVES+2))
fi
arreter_noeud

# ===================================================================
titre "BILAN"
# ===================================================================
echo "preuves tentees : $PREUVES"
echo "echecs          : $ECHECS"
echo "journaux        : $LABO"
echo
if [ "$ECHECS" -eq 0 ]; then
  echo "RESULTAT : la procedure de sauvegarde et de restauration de la cle de"
  echo "           scellage est DEMONTREE. Une chaine detruite est repartie au"
  echo "           meme bloc, sur la meme chaine, scellee par la meme adresse,"
  echo "           a partir des seules sauvegardes."
  echo
  echo "Pour effacer le laboratoire :  rm -rf $LABO"
  exit 0
else
  echo "RESULTAT : $ECHECS preuve(s) NON obtenue(s). La procedure n'est PAS"
  echo "           demontree en l'etat. Voir les journaux ci-dessus."
  echo
  echo "Laboratoire conserve pour analyse : $LABO"
  exit 1
fi
