#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — un accès RPC pour une place d'échange, reconnu par SECRET et non
# par adresse IP.
#
#   sudo bash 79-acces-partenaire.sh            # installe (demande le secret)
#   sudo bash 79-acces-partenaire.sh --essai    # éprouve l'accès en place
#   sudo ANNULER=1 bash 79-acces-partenaire.sh  # retire tout
#
# POURQUOI — la remarque de l'éditeur était juste
# -----------------------------------------------
# Bite Fast est aujourd'hui reconnu par son adresse : 31.97.40.12 figure dans
# `ignoreip` de la prison fail2ban. L'éditeur a fait remarquer qu'une adresse
# change — derrière Cloudflare, chez un autre hébergeur, ou dès qu'un second
# serveur s'ajoute. Le jour où elle change, l'exchange se fait bannir à
# 1 500 requêtes/minute, sans que rien ne le prévienne.
#
# Vérifié le 2026-09-05 : 31.97.40.12 appartient à HOSTINGER, pas à Cloudflare,
# et bite-fast.com sert `nginx` sans en-tête `cf-ray`. La remarque ne mord donc
# pas encore — mais elle mordra.
#
# CE QUI CHANGE
# -------------
# Un chemin dédié, `/rpc-partenaire`, qui n'accepte que les requêtes portant
# l'en-tête `Authorization: Bearer <secret>`. Le partenaire peut alors appeler
# depuis n'importe quelle adresse.
#
# CE QUI NE CHANGE PAS, ET C'EST VOULU
# ------------------------------------
# Le chemin public `/rpc` reste exactement ce qu'il est : mêmes limites, même
# prison, même borne de simultanéité. Rien de ce qui protège le validateur n'est
# desserré pour tout le monde afin d'accommoder un partenaire.
#
# LE PARTENAIRE N'EST PAS SANS LIMITE NON PLUS
# --------------------------------------------
# Sa borne de simultanéité est plus haute (48 contre 24), pas absente. Le
# validateur partage les 4 cœurs de cette machine : un partenaire qui saturerait
# le nœud arrêterait la production de blocs aussi sûrement qu'un inconnu. Un
# accès privilégié n'est pas un accès illimité.
#
# LA PRISON N'A PAS BESOIN D'ÊTRE TOUCHÉE
# ---------------------------------------
# Son filtre matche `"uri":"/rpc"` — la guillemet fermante impose l'égalité
# exacte. `/rpc-partenaire` n'y correspond donc pas : le chemin partenaire est
# hors prison par construction, sans qu'on ait à desserrer quoi que ce soit.
#
# LE SECRET NE VIT PAS DANS LE CADDYFILE
# --------------------------------------
# `/etc/caddy/coinbosa-partenaire.env`, en 600 root:root, lu par l'unité systemd
# de Caddy. Le Caddyfile ne porte que `{env.COINBOSA_PARTENAIRE_TOKEN}`. Un
# Caddyfile est souvent lisible par d'autres comptes ; un secret n'y a pas sa
# place.
#
# Conséquence à connaître : une variable d'environnement n'est lue qu'au
# DÉMARRAGE. Poser ou changer le secret impose donc `systemctl restart caddy`,
# pas un simple `reload`. Le site est indisponible une fraction de seconde ; la
# chaîne, elle, n'est pas touchée — Caddy n'est qu'un relais.
# ---------------------------------------------------------------------------
set -euo pipefail

ENVF=/etc/caddy/coinbosa-partenaire.env
CADDYFILE=/etc/caddy/Caddyfile
DROPIN=/etc/systemd/system/caddy.service.d/coinbosa-partenaire.conf
DOMAINE="${DOMAINE:-explorer.coinbosa.com}"
ANNULER="${ANNULER:-0}"
ESSAI=0; [ "${1:-}" = "--essai" ] && ESSAI=1

ok() { printf '    \033[32mOK\033[0m    %s\n' "$1"; }
ko() { printf '    \033[31mECHEC\033[0m %s\n' "$1"; exit 1; }
[ "$(id -u)" = 0 ] || { echo "À lancer en root (sudo)." >&2; exit 1; }

# --- retrait ----------------------------------------------------------------
if [ "$ANNULER" = 1 ]; then
  echo "==> Retrait de l'accès partenaire"
  sauv=$(ls -1t "$CADDYFILE".avant-partenaire-* 2>/dev/null | head -1)
  [ -n "$sauv" ] && { cp -a "$sauv" "$CADDYFILE"; echo "    Caddyfile restauré depuis $sauv"; } \
                 || echo "    aucune sauvegarde du Caddyfile — retirez le bloc à la main"
  rm -f "$DROPIN" "$ENVF"
  systemctl daemon-reload
  caddy validate --config "$CADDYFILE" >/dev/null 2>&1 && systemctl restart caddy \
    || echo "    Caddyfile invalide — NE PAS redémarrer, corriger d'abord"
  echo "==> Retiré. Le chemin public /rpc n'a jamais été modifié."
  exit 0
fi

# --- épreuve ----------------------------------------------------------------
if [ "$ESSAI" = 1 ]; then
  [ -s "$ENVF" ] || ko "aucun secret installé ($ENVF)"
  T=$(sed -n 's/^COINBOSA_PARTENAIRE_TOKEN=//p' "$ENVF" | head -1)
  [ -n "$T" ] || ko "le fichier d'environnement ne porte pas de secret"
  REQ='{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}'

  echo "==> Épreuve de l'accès partenaire"
  c=$(curl -s -o /tmp/p1 -w '%{http_code}' --max-time 20 -X POST \
      -H 'content-type: application/json' -H "Authorization: Bearer $T" \
      -d "$REQ" "https://$DOMAINE/rpc-partenaire")
  b=$(grep -o '"result":"[^"]*"' /tmp/p1 2>/dev/null | head -1)
  [ "$c" = 200 ] && [ -n "$b" ] && ok "avec le bon secret : HTTP 200, $b" \
                                || ko "avec le bon secret : HTTP $c — l'accès ne fonctionne pas"

  c=$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 -X POST \
      -H 'content-type: application/json' -d "$REQ" "https://$DOMAINE/rpc-partenaire")
  [ "$c" = 401 ] && ok "sans secret : HTTP 401" || ko "sans secret : HTTP $c, 401 attendu"

  c=$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 -X POST \
      -H 'content-type: application/json' -H "Authorization: Bearer mauvais-secret" \
      -d "$REQ" "https://$DOMAINE/rpc-partenaire")
  [ "$c" = 401 ] && ok "avec un mauvais secret : HTTP 401" || ko "mauvais secret : HTTP $c, 401 attendu"

  c=$(curl -s -o /tmp/p2 -w '%{http_code}' --max-time 20 -X POST \
      -H 'content-type: application/json' -d "$REQ" "https://$DOMAINE/rpc")
  b=$(grep -o '"result":"[^"]*"' /tmp/p2 2>/dev/null | head -1)
  [ "$c" = 200 ] && [ -n "$b" ] && ok "le chemin PUBLIC /rpc répond toujours : $b" \
                                || ko "le chemin public /rpc est cassé : HTTP $c"
  rm -f /tmp/p1 /tmp/p2
  echo
  echo "    L'exchange appelle : https://$DOMAINE/rpc-partenaire"
  echo "    avec l'en-tête     : Authorization: Bearer <le secret>"
  exit 0
fi

# --- installation -----------------------------------------------------------
echo "==> Le secret"
if [ -s "$ENVF" ]; then
  ok "un secret existe déjà — conservé (ANNULER=1 pour repartir de zéro)"
else
  echo "    Il sera transmis UNE FOIS à la place d'échange, par un canal que vous"
  echo "    choisissez. Laissez vide pour en faire tirer un au hasard."
  printf "    Secret (invisible) : "; read -rs S; echo
  if [ -z "$S" ]; then
    S=$(head -c 32 /dev/urandom | base64 | tr -d '=+/' | cut -c1-40)
    echo "    Secret tiré au hasard, 40 caractères. Il s'affichera UNE SEULE FOIS à la fin."
    MONTRER=1
  else
    [ ${#S} -ge 24 ] || ko "trop court : ${#S} caractères, 24 au minimum"
    MONTRER=0
  fi
  umask 077
  printf 'COINBOSA_PARTENAIRE_TOKEN=%s\n' "$S" > "$ENVF"
  chown root:root "$ENVF"; chmod 600 "$ENVF"
  ok "secret écrit dans $ENVF ($(stat -c '%a %U:%G' "$ENVF"))"
fi

echo "==> L'unité Caddy lit ce fichier"
install -d -m 0755 "$(dirname "$DROPIN")"
cat > "$DROPIN" <<UNIT
# Le secret de l'accès partenaire vit ici, en 600, et NON dans le Caddyfile —
# qui est souvent lisible par d'autres comptes. Caddy ne lit ses variables
# d'environnement qu'au DEMARRAGE : changer le secret impose un restart.
[Service]
EnvironmentFile=$ENVF
UNIT
systemctl daemon-reload
ok "fichier d'environnement déclaré dans l'unité"

echo "==> Le chemin /rpc-partenaire"
if grep -q 'rpc-partenaire' "$CADDYFILE"; then
  ok "le bloc existe déjà dans le Caddyfile"
else
  cp -a "$CADDYFILE" "$CADDYFILE.avant-partenaire-$(date +%F-%H%M)"
  python3 - "$CADDYFILE" <<'PY'
import pathlib, sys
p = pathlib.Path(sys.argv[1]); s = p.read_text()
ancre = "    @rpc_post {"
assert s.count(ancre) == 1, "ancre @rpc_post introuvable ou multiple"
bloc = """    # --- Acces partenaire (place d'echange) ------------------------------
    # Reconnu par SECRET, pas par adresse : le partenaire peut changer d'IP,
    # d'hebergeur, ou passer derriere un relais, sans que rien ne casse.
    # Le secret vit dans /etc/caddy/coinbosa-partenaire.env, en 600 root:root ;
    # ce fichier ne porte que la reference. Un Caddyfile est souvent lisible par
    # d'autres comptes.
    # La prison fail2ban ne voit pas ce chemin : son filtre matche "uri":"/rpc"
    # avec la guillemet fermante, donc l'egalite exacte.
    @partenaire {
        path /rpc-partenaire
        method POST
        header Authorization "Bearer {env.COINBOSA_PARTENAIRE_TOKEN}"
    }
    handle @partenaire {
        rewrite * /
        request_body {
            max_size 2MB
        }
        reverse_proxy 127.0.0.1:8547 {
            header_up Host {upstream_hostport}
            # 48 et non « aucune » : le validateur partage les 4 coeurs de cette
            # machine. Un partenaire qui saturerait le noeud arreterait la
            # production de blocs aussi surement qu'un inconnu. Un acces
            # privilegie n'est pas un acces illimite.
            transport http {
                max_conns_per_host 48
                dial_timeout 2s
                response_header_timeout 30s
            }
        }
    }
    # Tout ce qui vise ce chemin sans le bon secret s'arrete ici.
    handle /rpc-partenaire* {
        respond "unauthorized" 401
    }

"""
    p.write_text(s.replace(ancre, bloc + ancre, 1))
    print("    bloc insere avant @rpc_post")
PY
  ok "Caddyfile modifié"
fi

grep -q 'max_conns_per_host 24' "$CADDYFILE" \
  && ok "la borne du chemin PUBLIC (24) est intacte" \
  || ko "la borne publique a disparu — restaurer la sauvegarde, NE PAS redémarrer"

caddy validate --config "$CADDYFILE" >/dev/null 2>&1 \
  && ok "Caddyfile valide" || ko "Caddyfile invalide — restaurer la sauvegarde"

echo "==> Redémarrage de Caddy (obligatoire : variables d'environnement)"
systemctl restart caddy
sleep 3
systemctl is-active caddy >/dev/null && ok "Caddy actif" || ko "Caddy ne redémarre pas"

echo
"$0" --essai || ko "l'épreuve a échoué — voir ci-dessus"

if [ "${MONTRER:-0}" = 1 ]; then
  echo
  echo "==> LE SECRET, affiché une seule fois. Notez-le maintenant."
  sed -n 's/^COINBOSA_PARTENAIRE_TOKEN=//p' "$ENVF"
fi
echo
echo "==> Retrait : sudo ANNULER=1 bash 79-acces-partenaire.sh"
