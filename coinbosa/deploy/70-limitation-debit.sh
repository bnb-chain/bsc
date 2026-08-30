#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Coinbosa — limitation de débit PAR ADRESSE IP sur le tier public.
#
#   sudo bash 70-limitation-debit.sh
#
# POURQUOI CE SCRIPT EXISTE
# -------------------------
# Le binaire Caddy en service est le paquet Debian standard (2.11.4). Vérifié :
#
#     caddy list-modules | grep -iE 'rate|limit'   -> AUCUNE correspondance
#     caddy list-modules | wc -l                   -> 134 modules, section
#                                                     « Non-standard modules » VIDE
#
# La limitation de débit n'existe pas dans Caddy de base : c'est un module tiers
# qu'il faut compiler dans le binaire (xcaddy). Remplacer le binaire du serveur
# web à quelques jours d'une cotation, c'est risquer l'indisponibilité totale du
# site, de l'explorateur ET du relais /rpc si le nouveau binaire refuse la
# configuration. Ce script prend donc l'autre voie : il n'ouvre PAS Caddy.
#
# Il agit sur deux étages qui existent déjà sur la machine et qui se posent
# sans interrompre quoi que ce soit :
#
#   1. fail2ban — déjà installé (1.0.2), déjà armé sur /rpc, déjà efficace
#      (99 bannissements comptés sur la prison caddy-rpc). On y ajoute ce qui
#      lui manque : une fenêtre COURTE, et une peine pour les récidivistes.
#      Application : « fail2ban-client reload ». Caddy et le nœud ne bougent pas.
#
#   2. nftables — une table SÉPARÉE, qui borne le nombre de connexions
#      simultanées et la cadence d'ouverture PAR ADRESSE IP, dans le noyau,
#      instantanément, sans attendre qu'une ligne soit écrite dans un journal.
#      Application : « nft -f ». Aucun service n'est redémarré.
#
# CE QUE CES DEUX ÉTAGES NE FONT PAS
# ----------------------------------
# Ni l'un ni l'autre ne renvoie 429. Ils écartent (bannissement, rejet paquet).
# Un intégrateur écarté ne « ralentit » pas : il tombe. D'où l'étape OBLIGATOIRE
# avant toute cotation : mettre les adresses de la place d'échange dans la liste
# blanche, via IP_BOURSE (voir plus bas). Ce script refuse de se taire là-dessus :
# il l'affiche en fin d'exécution.
#
# SEUILS — d'où ils viennent
# --------------------------
# Mesuré sur le journal réel de Caddy (25 h, 363 442 requêtes /rpc) :
#
#     médiane par (IP, minute) ............................    2 req/min
#     p99 par (IP, minute) ................................   89 req/min
#     client légitime le plus bavard (148.251.10.246) .....   90 req/min soutenu
#     seule IP au-dessus (37.187.89.167, l'abus en cours) . 1546-1663 req/min
#
# Et dans le code de l'explorateur (explorer/app.js:362, setInterval(cycle,5000)),
# un onglet consomme 5 appels toutes les 5 s, soit 60 req/min ; le premier
# affichage coûte une bouffée d'environ 22 requêtes.
#
# Les seuils ci-dessous sont donc posés au-dessus de 90 req/min avec une marge
# d'un ordre de grandeur, et non « au jugé ».
# ---------------------------------------------------------------------------
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "à lancer en root (sudo bash 70-limitation-debit.sh)" >&2; exit 1; }

# --- Seuils (surchargeables à l'appel : SEUIL_MINUTE=900 sudo -E bash …) -----

# Régime soutenu : 1200 requêtes /rpc par minute et par IP.
#   = 13 fois le client légitime le plus bavard mesuré (90 req/min)
#   = 20 onglets d'explorateur derrière une même IP (NAT d'entreprise, CGNAT)
#   Sous l'ancien seuil de 1500, l'abus en cours (1546-1663) passait de justesse
#   une minute entière avant d'être vu. 1200 le coupe plus tôt sans descendre
#   dans la zone où un partage d'adresse légitime pourrait cogner.
SEUIL_MINUTE="${SEUIL_MINUTE:-1200}"

# Régime de rafale : 300 requêtes en 10 s, soit 30 req/s pour une seule IP.
#   = 13 premiers affichages d'explorateur simultanés derrière la même IP (22 req pièce)
#   = 30 fois la cadence d'un onglet (1 req/s)
#   C'est l'étage qui manquait. Avec la seule fenêtre de 60 s, un attaquant
#   disposait de 1200 requêtes gratuites avant d'être vu ; ici il en a 300, et
#   il est vu en 10 s au lieu de 60.
SEUIL_RAFALE="${SEUIL_RAFALE:-300}"
FENETRE_RAFALE="${FENETRE_RAFALE:-10}"

# Durée de bannissement. Volontairement courte : un faux positif sur une IP
# partagée doit coûter des minutes, pas des heures. La récidive, elle, coûte cher.
PEINE="${PEINE:-30m}"

# Connexions TCP simultanées par IP vers 80/443.
#   Un navigateur ouvre 1 connexion HTTP/2 par origine (proto « h2 » constaté
#   dans le journal), 6 au plus en HTTP/1.1. 64 = une dizaine de navigateurs
#   derrière la même adresse. Au-delà, ce n'est plus de la navigation.
CONN_MAX="${CONN_MAX:-64}"

# Cadence d'ouverture de connexions par IP.
TAUX_CONN="${TAUX_CONN:-30}"
RAFALE_CONN="${RAFALE_CONN:-60}"

# Adresses de la place d'échange, séparées par des espaces. VIDE PAR DÉFAUT :
# le script le signale bruyamment, car c'est l'omission qui casse une cotation.
#   IP_BOURSE="203.0.113.10 203.0.113.11" sudo -E bash 70-limitation-debit.sh
IP_BOURSE="${IP_BOURSE:-}"

echo "==> Prérequis"
for outil in fail2ban-client fail2ban-regex nft systemctl; do
  command -v "$outil" >/dev/null 2>&1 || { echo "ARRÊT : $outil introuvable." >&2; exit 1; }
done
JOURNAL_EXPLORER=/var/log/caddy/explorer-access.log
[ -s "$JOURNAL_EXPLORER" ] || { echo "ARRÊT : $JOURNAL_EXPLORER vide ou absent — 10-web.sh n'a pas tourné." >&2; exit 1; }
echo "    fail2ban $(fail2ban-client --version 2>/dev/null | head -1), $(nft --version)"

# ---------------------------------------------------------------------------
# Liste blanche
# ---------------------------------------------------------------------------
# Trois familles d'adresses ne doivent JAMAIS être écartées :
#   - la boucle locale ;
#   - l'adresse publique de la machine elle-même : 50-monitoring.sh sonde
#     https://explorer.coinbosa.com/rpc depuis le serveur, la requête ressort et
#     revient par l'adresse publique. Sans exemption, la supervision peut se
#     faire bannir par sa propre sonde — et annoncer ensuite une fausse panne ;
#   - l'IP par laquelle l'opérateur est connecté en SSH.
echo "==> Liste blanche"
IP_PUB4=$(ip -4 -o addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | tr '\n' ' ')
IP_PUB6=$(ip -6 -o addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | tr '\n' ' ')
IP_OP=""
[ -n "${SSH_CONNECTION:-}" ] && IP_OP=$(echo "$SSH_CONNECTION" | awk '{print $1}')

BLANCHE="127.0.0.1/8 ::1 $IP_PUB4 $IP_PUB6 $IP_OP $IP_BOURSE"
BLANCHE=$(echo "$BLANCHE" | tr ' ' '\n' | grep -v '^$' | sort -u | tr '\n' ' ')
echo "    $BLANCHE"

if [ -z "$IP_BOURSE" ]; then
  echo ""
  echo "    !! IP_BOURSE est VIDE."
  echo "    !! Les seuils ci-dessous sont calibrés sur un usage de navigation, pas sur"
  echo "    !! l'interrogation soutenue d'un intégrateur. Tant que les adresses de la"
  echo "    !! place d'échange n'y sont pas, elles sont soumises au même plafond que"
  echo "    !! n'importe qui — et un bannissement de $PEINE le jour de la cotation se lit"
  echo "    !! chez elle comme « le RPC est tombé »."
  echo ""
fi

# ---------------------------------------------------------------------------
# Étage 1 — fail2ban
# ---------------------------------------------------------------------------
# Le fichier est écrit à part de coinbosa-web.conf (21-fail2ban-web.sh), pour que
# les deux scripts restent rejouables indépendamment. Le préfixe « zz- » n'est pas
# cosmétique : fail2ban lit jail.d/*.conf dans l'ordre alphabétique et la
# DERNIÈRE définition d'une section gagne. Sans ce préfixe, coinbosa-web.conf
# repasserait après et rétablirait l'ancien seuil, en silence.
echo "==> Prisons fail2ban"

cat > /etc/fail2ban/jail.d/zz-coinbosa-debit.conf <<CONF
# Coinbosa — limitation de débit par IP. Généré par deploy/70-limitation-debit.sh
# Lu APRÈS coinbosa-web.conf (ordre alphabétique) : ces valeurs font foi.

[caddy-rpc]
enabled  = true
filter   = caddy-rpc
logpath  = /var/log/caddy/explorer-access.log
backend  = auto
port     = http,https
maxretry = $SEUIL_MINUTE
findtime = 60
bantime  = $PEINE
ignoreip = $BLANCHE

# Même filtre, fenêtre courte : c'est la réaction en 10 s au lieu de 60.
[caddy-rpc-rafale]
enabled  = true
filter   = caddy-rpc
logpath  = /var/log/caddy/explorer-access.log
backend  = auto
port     = http,https
maxretry = $SEUIL_RAFALE
findtime = $FENETRE_RAFALE
bantime  = $PEINE
ignoreip = $BLANCHE

# Récidive. 37.187.89.167 a été bannie 99 fois et revient : une peine de 30 min
# ne dissuade pas une machine. Trois bannissements en 24 h ne sont pas un
# accident de navigation ; la quatrième fois coûte une semaine.
[recidive]
enabled  = true
filter   = recidive
logpath  = /var/log/fail2ban.log
backend  = auto
port     = all
protocol = all
banaction = %(banaction_allports)s
maxretry = 3
findtime = 1d
bantime  = 1w
ignoreip = $BLANCHE
CONF

# La prison « recidive » relit /var/log/fail2ban.log, mais fail2ban purge sa base
# au bout de dbpurgeage. Laissé à 1 jour, il oublie les bannissements avant que la
# fenêtre de 1 jour ne soit pleine : la prison existe et ne se déclenche jamais.
echo "==> Mémoire des bannissements (dbpurgeage)"
touch /etc/fail2ban/fail2ban.local
if grep -qE '^\s*dbpurgeage' /etc/fail2ban/fail2ban.local; then
  sed -i 's/^\s*dbpurgeage.*/dbpurgeage = 8d/' /etc/fail2ban/fail2ban.local
else
  grep -qE '^\[Definition\]' /etc/fail2ban/fail2ban.local || printf '[Definition]\n' >> /etc/fail2ban/fail2ban.local
  printf 'dbpurgeage = 8d\n' >> /etc/fail2ban/fail2ban.local
fi
grep -q 'dbpurgeage = 8d' /etc/fail2ban/fail2ban.local \
  || { echo "ARRÊT : dbpurgeage n'a pas été posé, la prison recidive serait inerte." >&2; exit 1; }

# --- Preuve que les filtres reconnaissent le journal RÉEL, pas un échantillon ---
# Une prison dont le filtre ne correspond à rien est pire qu'aucune prison : elle
# donne un état « active » et ne bannit jamais.
echo "==> Contrôle des filtres sur le journal réel"
verifier_filtre() {
  nom="$1"; journal="$2"; mini="$3"
  sortie=$(fail2ban-regex "$journal" "/etc/fail2ban/filter.d/${nom}.conf" 2>/dev/null || true)
  n=$(printf '%s' "$sortie" | grep -oE '[0-9]+ matched' | head -1 | grep -oE '^[0-9]+' || echo 0)
  if [ "${n:-0}" -lt "$mini" ]; then
    echo "ARRÊT : le filtre $nom ne reconnaît pas $journal ($n correspondance(s), $mini attendue(s))." >&2
    printf '%s\n' "$sortie" | tail -20 >&2
    rm -f /etc/fail2ban/jail.d/zz-coinbosa-debit.conf
    exit 1
  fi
  echo "    $nom sur $journal : $n correspondance(s) — OK"
}
verifier_filtre caddy-rpc "$JOURNAL_EXPLORER" 100
verifier_filtre recidive  /var/log/fail2ban.log 1

# « reload » relit la configuration sans couper les prisons en cours ni toucher
# à Caddy ou au nœud. C'est volontaire : on ne redémarre rien cette semaine.
echo "==> Rechargement de fail2ban (aucun autre service touché)"
fail2ban-client reload >/dev/null
sleep 3

for prison in caddy-rpc caddy-rpc-rafale recidive; do
  fail2ban-client status "$prison" >/dev/null 2>&1 \
    || { echo "ARRÊT : la prison $prison n'est pas active après rechargement." >&2; exit 1; }
  echo "    prison $prison : active"
done

# On vérifie que le seuil appliqué est bien celui qu'on a écrit, et pas celui
# qu'un autre fichier de jail.d aurait remis derrière notre dos.
lu=$(fail2ban-client get caddy-rpc maxretry 2>/dev/null | tr -dc '0-9')
[ "$lu" = "$SEUIL_MINUTE" ] \
  || { echo "ARRÊT : caddy-rpc applique maxretry=$lu au lieu de $SEUIL_MINUTE (conflit dans jail.d)." >&2; exit 1; }
echo "    seuil réellement appliqué à caddy-rpc : $lu req/min — conforme"

# ---------------------------------------------------------------------------
# Étage 2 — nftables
# ---------------------------------------------------------------------------
# Table SÉPARÉE. ufw tient « ip filter » / « ip6 filter », fail2ban tient
# « inet f2b-table » : on ne touche ni à l'une ni à l'autre. Priorité -5, donc
# évaluée avant elles ; « policy accept » laisse tout le reste suivre son cours.
#
# Le filtrage est écrit en négatif (« saddr != @exemptes ») plutôt qu'en posant
# un « accept » sur la liste blanche : un « accept » dans une chaîne de base ne
# termine pas l'évaluation des autres chaînes du même hook, mais le raisonnement
# est subtil et se relit mal. La forme négative ne laisse aucun doute : les
# adresses exemptées ne rencontrent simplement aucune règle.
#
# AUCUNE règle ne mentionne le port 22. C'est contrôlé plus bas, avant pose.
echo "==> Bornes par IP dans le noyau (nftables)"

EXEMPT4=$(echo "$BLANCHE" | tr ' ' '\n' | grep -E '^[0-9]+\.' | sort -u | paste -sd, -)
EXEMPT6=$(echo "$BLANCHE" | tr ' ' '\n' | grep ':' | sort -u | paste -sd, -)
[ -n "$EXEMPT4" ] || EXEMPT4="127.0.0.0/8"
[ -n "$EXEMPT6" ] || EXEMPT6="::1"

REGLES=/etc/nftables-coinbosa-debit.nft
cat > "$REGLES" <<NFT
#!/usr/sbin/nft -f
# Coinbosa — bornes par IP sur le tier public. Généré par deploy/70-limitation-debit.sh
# Ne PAS éditer à la main : relancer le script.

# Idiome rejouable : on crée la table (sans effet si elle existe), on la supprime,
# on la repose. Le tout dans une seule transaction atomique — à aucun instant le
# pare-feu n'est dans un état partiel.
table inet coinbosa-debit
delete table inet coinbosa-debit

table inet coinbosa-debit {
    set exemptes4 {
        type ipv4_addr
        flags interval
        elements = { $EXEMPT4 }
    }
    set exemptes6 {
        type ipv6_addr
        flags interval
        elements = { $EXEMPT6 }
    }

    chain limitation {
        type filter hook input priority filter - 5; policy accept;

        # « meter » indexe le compteur SUR L'ADRESSE SOURCE : chaque IP a son
        # propre compteur. Ce n'est pas un plafond global — une IP qui abuse
        # n'entame pas le quota des autres, ni celui de la place d'échange.
        tcp dport { 80, 443 } ip  saddr != @exemptes4 ct state new meter conn4 { ip saddr ct count over $CONN_MAX } counter drop
        tcp dport { 80, 443 } ip  saddr != @exemptes4 ct state new meter taux4 { ip saddr limit rate over $TAUX_CONN/second burst $RAFALE_CONN packets } counter drop

        # En IPv6, bannir une adresse ne sert à rien : un attaquant dispose d'un
        # /64 entier, soit 2^64 adresses. Le compteur est donc indexé sur le /64
        # (masque ffff:ffff:ffff:ffff::), pas sur l'adresse.
        tcp dport { 80, 443 } ip6 saddr != @exemptes6 ct state new meter conn6 { ip6 saddr and ffff:ffff:ffff:ffff:: ct count over $CONN_MAX } counter drop
        tcp dport { 80, 443 } ip6 saddr != @exemptes6 ct state new meter taux6 { ip6 saddr and ffff:ffff:ffff:ffff:: limit rate over $TAUX_CONN/second burst $RAFALE_CONN packets } counter drop
    }
}
NFT
chmod 0750 "$REGLES"

# Garde-fou : ce fichier ne doit jamais parler du port 22. Une erreur ici
# coûterait l'accès à la machine, et la chaîne n'a qu'un validateur.
if grep -qE 'dport[^,}]*\b22\b' "$REGLES"; then
  echo "ARRÊT : le jeu de règles mentionne le port 22. Rien n'a été posé." >&2
  rm -f "$REGLES"; exit 1
fi
echo "    aucune règle sur le port 22 — accès SSH intouché"

# Vérification à blanc AVANT de poser quoi que ce soit.
nft -c -f "$REGLES" || { echo "ARRÊT : jeu de règles refusé par nft. Rien n'a été posé." >&2; rm -f "$REGLES"; exit 1; }
echo "    jeu de règles validé à blanc (nft -c)"

nft -f "$REGLES"
nft list table inet coinbosa-debit >/dev/null 2>&1 \
  || { echo "ARRÊT : la table coinbosa-debit n'existe pas après pose." >&2; exit 1; }
n_regles=$(nft list table inet coinbosa-debit | grep -c 'counter drop')
[ "$n_regles" -eq 4 ] \
  || { echo "ARRÊT : $n_regles règle(s) posée(s) au lieu de 4." >&2; exit 1; }
echo "    table inet coinbosa-debit : $n_regles règles actives"

# ufw ne rejoue pas cette table au démarrage, et nftables.service est désactivé
# sur cette machine (vérifié : « systemctl is-enabled nftables » -> disabled).
# Sans unité dédiée, les bornes disparaîtraient au premier redémarrage — sans
# aucun message, et personne ne le verrait avant le prochain abus.
echo "==> Persistance au démarrage"
cat > /etc/systemd/system/coinbosa-debit.service <<UNIT
[Unit]
Description=Coinbosa — bornes de débit par IP (nftables)
After=network-pre.target ufw.service
Wants=network-pre.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/sbin/nft -f /etc/nftables-coinbosa-debit.nft
ExecStop=/usr/sbin/nft delete table inet coinbosa-debit
StandardOutput=journal

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable coinbosa-debit >/dev/null 2>&1
systemctl is-enabled coinbosa-debit >/dev/null 2>&1 \
  || { echo "ARRÊT : l'unité coinbosa-debit n'est pas activée au démarrage." >&2; exit 1; }
echo "    unité coinbosa-debit : activée au démarrage"

# ---------------------------------------------------------------------------
echo ""
echo "==> Posé. État :"
echo ""
fail2ban-client status 2>/dev/null || true
echo ""
nft list table inet coinbosa-debit
echo ""
echo "    Seuils appliqués"
echo "      soutenu  : $SEUIL_MINUTE requêtes /rpc / minute / IP   (p99 mesuré : 89)"
echo "      rafale   : $SEUIL_RAFALE requêtes /rpc / ${FENETRE_RAFALE}s / IP"
echo "      peine    : $PEINE  —  récidive (3 en 24 h) : 1 semaine"
echo "      réseau   : $CONN_MAX connexions simultanées et $TAUX_CONN nouvelles/s par IP"
echo ""
echo "    Débannir une IP     :  fail2ban-client set caddy-rpc unbanip <IP>"
echo "                           fail2ban-client set caddy-rpc-rafale unbanip <IP>"
echo "                           fail2ban-client set recidive unbanip <IP>"
echo "    Ajouter la bourse   :  IP_BOURSE=\"a.b.c.d e.f.g.h\" sudo -E bash 70-limitation-debit.sh"
echo ""
echo "    RETOUR EN ARRIÈRE (aucun redémarrage de service) :"
echo "      rm -f /etc/fail2ban/jail.d/zz-coinbosa-debit.conf && fail2ban-client reload"
echo "      systemctl disable --now coinbosa-debit"
echo "      nft delete table inet coinbosa-debit"
