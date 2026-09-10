#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — publier l'offre en circulation, lue sur la chaîne.
#
#   sudo bash 85-offre-en-circulation.sh            # installe et sert
#   sudo ANNULER=1 bash 85-offre-en-circulation.sh  # retire
#
# POURQUOI
# --------
# CoinGecko et CoinMarketCap exigent la même chose, et c'est une exigence dure :
# une URL qui renvoie l'offre en circulation en NOMBRE BRUT, sans habillage,
# qu'ils interrogent eux-mêmes. Sans elle, le dossier n'avance pas — ils ne
# recopient pas un chiffre écrit dans un document.
#
# CE QUE « EN CIRCULATION » VEUT DIRE ICI, ET POURQUOI CE N'EST PAS 700 000 000
# -----------------------------------------------------------------------------
# L'offre TOTALE vaut 700 000 000 BOSA, fixée au bloc de genèse. L'offre en
# CIRCULATION est ce qui n'est pas détenu par le projet — c'est le sens que les
# deux agrégateurs donnent au terme, et celui qui sert à calculer une
# capitalisation.
#
#     circulation = 700 000 000
#                 − les 13 postes de trésorerie
#                 − le gouverneur
#                 − le contrat système (frais dus au validateur)
#
# Au 2026-09-10 cela fait 100 BOSA : le dépôt du 5 septembre vers bite-fast.com,
# seul mouvement sorti des comptes du projet. Le chiffre est petit, et c'est la
# vérité — annoncer davantage serait gonfler une capitalisation.
#
# LE CHIFFRE N'EST JAMAIS ÉCRIT À LA MAIN
# ---------------------------------------
# Il est recalculé à chaque passage depuis les soldes réels, par le RPC local.
# Un nombre en dur dans un fichier finit toujours par mentir : celui-ci ne peut
# pas se tromper sans que la chaîne se soit trompée d'abord.
#
# ET IL REFUSE DE PUBLIER CE QU'IL N'A PAS PU CALCULER
# ----------------------------------------------------
# Si un solde est illisible, le script GARDE l'ancienne valeur et le journalise.
# Écrire 0, ou écrire 700 000 000, sur une lecture ratée serait pire que ne rien
# écrire : les agrégateurs recopient ce qu'ils lisent.
# ---------------------------------------------------------------------------
set -euo pipefail

RACINE=/var/www/coinbosa/site/api
OUTIL=/usr/local/bin/coinbosa-offre-circulation
ANNULER="${ANNULER:-0}"

ok() { printf '    \033[32mOK\033[0m    %s\n' "$1"; }
ko() { printf '    \033[31mECHEC\033[0m %s\n' "$1"; exit 1; }
[ "$(id -u)" = 0 ] || { echo "À lancer en root (sudo)." >&2; exit 1; }

if [ "$ANNULER" = 1 ]; then
  systemctl disable --now coinbosa-offre.timer 2>/dev/null || true
  rm -f /etc/systemd/system/coinbosa-offre.{service,timer} "$OUTIL"
  rm -rf "$RACINE"
  systemctl daemon-reload
  echo "==> Retiré. Le Caddyfile n'a pas été modifié (la route sert un dossier absent : 404)."
  exit 0
fi

echo "==> Préalables"
command -v python3 >/dev/null || ko "python3 absent"
ok "python3 présent"

install -d -o caddy -g caddy "$RACINE"

cat > "$OUTIL" <<'OUTIL_FIN'
#!/usr/bin/env python3
"""Calcule l'offre en circulation depuis la chaîne, et l'écrit en clair.

En circulation = offre totale − ce que le projet détient. Les comptes du projet
sont ceux du genesis, plus le gouverneur, plus le contrat système. Tout le reste
est, par définition, hors du projet.

Le fichier n'est réécrit que si le calcul a abouti : sur une lecture ratée on
garde la valeur précédente. Les agrégateurs recopient ce qu'ils lisent ; leur
servir un zéro parce qu'un socket a hoqueté serait pire que de ne rien servir.
"""
import json
import syslog
import urllib.request
from pathlib import Path

RPC = "http://127.0.0.1:8545"
SORTIE = Path("/var/www/coinbosa/site/api")
WEI = 10 ** 18

# Les comptes du projet. Ils sont GRAVES DANS LE BLOC 0 et ne peuvent plus
# changer : les inscrire ici n'est pas un raccourci, c'est recopier une
# constante publique. Le fichier genesis n'est pas present sur le serveur —
# verifie le 2026-09-10 — donc le lire n'etait pas une option.
# Source : genesis/genesis-coinbosa.json du depot, section alloc.
PROJET = [
    "0x223C546d25032E209556e9607041F0A1EFe4674D",
    "0x31CAD23D872c4cf7Eb22FC4B27f3094654b95DF8",
    "0x41Ab22491Ba87eda15927286D744ebdaAE5B2FC9",
    "0x47f0c3e1D2c9EA164986c58612CafD39bb89ED41",
    "0x59dcf9E2A5C17D6C32dC00feCdd8419954494E3f",
    "0x69B3C57Ba943c31489Eb6A1d7727f550B42512F8",
    "0x6baA7353Ed90dACB4d6C1A2DA53cbf77DF7F2E32",
    "0x7a8E70400Af9b66E22cefF574Dba9B293f3Ca6b5",
    "0xCa6f08e549290BbF161fF45c475fd3f7A6e65f04",
    "0xF85C43a06032F557323545dC3353f31dF1fBDD65",
    "0xb3B91c44f7D48e814aC37c3ED3C691eEDd728b1b",
    "0xd53de8724Fef3Dc24bF12a34adEf68c3Cd30c07E",
    "0xf4cEbe2d34A9a996cAD0c02345d6c3fB69B0E6C1",
    "0x1EEf3830833d83AcD3152A511853fd04a0b4082A",  # gouverneur
    "0x0000000000000000000000000000000000001000",  # contrat systeme (frais)
]


def rpc(methode, params):
    corps = json.dumps({"jsonrpc": "2.0", "id": 1,
                        "method": methode, "params": params}).encode()
    r = urllib.request.Request(RPC, data=corps,
                               headers={"content-type": "application/json"})
    with urllib.request.urlopen(r, timeout=15) as rep:
        d = json.load(rep)
    if "error" in d:
        raise RuntimeError(f"{methode} : {d['error']}")
    return d["result"]


def main():
    syslog.openlog("coinbosa-offre")
    projet = PROJET
    total = 700_000_000 * WEI
    detenu = 0
    for adr in projet:
        detenu += int(rpc("eth_getBalance", [adr, "latest"]), 16)

    circulation = total - detenu

    # GARDE-FOU. Une circulation negative, ou anormalement grande, ne veut pas
    # dire que la chaine a change : elle veut dire que cette liste de comptes ne
    # decrit plus le projet. Dans les deux cas on refuse de publier, parce que
    # les agregateurs recopient ce qu ils lisent et qu un faux chiffre d offre
    # se propage partout avant qu on s en apercoive.
    if circulation < 0:
        raise RuntimeError(f"circulation negative ({circulation} wei) — "
                           "la liste des comptes du projet ne correspond plus")
    if circulation > total // 100:
        raise RuntimeError(
            f"circulation de {circulation / WEI:.6f} BOSA, soit plus de 1 % de "
            "l offre : verifier PROJET avant de publier ce chiffre")

    n = circulation / WEI
    SORTIE.mkdir(parents=True, exist_ok=True)
    # Nombre brut, sans unité, sans espace, sans saut de ligne superflu : c'est
    # exactement ce que les agrégateurs attendent.
    brut = f"{n:.6f}".rstrip("0").rstrip(".")
    (SORTIE / "circulating-supply").write_text(brut)
    (SORTIE / "total-supply").write_text("700000000")
    # Variante avec extension : Caddy sert le fichier sans extension SANS
    # content-type, et avec nosniff certains clients refusent alors de
    # l'afficher. Le .txt evite d'avoir a toucher au Caddyfile pour cela.
    (SORTIE / "circulating-supply.txt").write_text(brut)
    (SORTIE / "total-supply.txt").write_text("700000000")
    (SORTIE / "supply.json").write_text(json.dumps({
        "total": "700000000",
        "circulating": f"{n:.6f}".rstrip("0").rstrip("."),
        "unit": "BOSA",
        "decimals": 18,
        "definition": "circulating = total supply minus all project-held accounts "
                      "(13 genesis treasury accounts, governor, system contract)",
        "source": "computed from on-chain balances at each run",
        "chainId": 26262,
    }, indent=2, ensure_ascii=False) + "\n")
    syslog.syslog(syslog.LOG_INFO,
                  f"offre en circulation : {n} BOSA sur {len(projet)} comptes projet")


if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        # On ne touche PAS aux fichiers : l'ancienne valeur reste servie.
        syslog.openlog("coinbosa-offre")
        syslog.syslog(syslog.LOG_ERR,
                      f"calcul impossible, ancienne valeur conservee : {e}")
        raise SystemExit(1)
OUTIL_FIN
chmod 0755 "$OUTIL"
ok "outil installé : $OUTIL"

cat > /etc/systemd/system/coinbosa-offre.service <<'UNIT'
[Unit]
Description=Coinbosa — recalculer l'offre en circulation depuis la chaîne
After=coinbosa-node.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/coinbosa-offre-circulation
User=root
NoNewPrivileges=yes
ProtectSystem=strict
ReadWritePaths=/var/www/coinbosa/site/api
ProtectHome=yes
PrivateTmp=yes
UNIT

cat > /etc/systemd/system/coinbosa-offre.timer <<'UNIT'
[Unit]
Description=Coinbosa — offre en circulation, toutes les dix minutes

[Timer]
OnBootSec=90s
OnUnitActiveSec=10min
AccuracySec=30s

[Install]
WantedBy=timers.target
UNIT

systemctl daemon-reload
systemctl enable --now coinbosa-offre.timer >/dev/null 2>&1
ok "minuteur actif (toutes les 10 min)"

echo
echo "==> Premier calcul"
"$OUTIL" || ko "le calcul a échoué — voir : journalctl -t coinbosa-offre -n 20"
chown -R caddy:caddy "$RACINE"
for f in circulating-supply circulating-supply.txt total-supply total-supply.txt supply.json; do
  [ -s "$RACINE/$f" ] || ko "$f vide ou absent"
  printf '    %-22s %s\n' "$f" "$(head -c 80 "$RACINE/$f" | tr '\n' ' ')"
done

echo
echo "==> Route Caddy"
if grep -q 'handle /api/\*' /etc/caddy/Caddyfile; then
  ok "la route /api existe déjà"
else
  echo "    À AJOUTER À LA MAIN dans le bloc du site de /etc/caddy/Caddyfile,"
  echo "    puis : sudo caddy validate --config /etc/caddy/Caddyfile && sudo systemctl reload caddy"
  echo
  cat <<'CADDY'
        # Offre en circulation — CoinGecko et CoinMarketCap l'interrogent
        # directement. Nombre brut, pas de cache long : la valeur bouge.
        handle /api/* {
            header Content-Type "text/plain; charset=utf-8"
            header Access-Control-Allow-Origin "*"
            header Cache-Control "public, max-age=300"
            file_server
        }
CADDY
fi

cat <<FIN

==> Ensuite
    Vérifier depuis l'extérieur :
      curl https://coinbosa.com/api/circulating-supply
      curl https://coinbosa.com/api/supply.json

    Ce sont ces deux URL qu'on donne à CoinGecko et à CoinMarketCap.

    Retrait : sudo ANNULER=1 bash 85-offre-en-circulation.sh
FIN
