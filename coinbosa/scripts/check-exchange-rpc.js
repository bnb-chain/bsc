#!/usr/bin/env node
/**
 * check-exchange-rpc.js — conformité du RPC public à ce qu'exige l'intégration
 * technique d'une place d'échange (indexeur de dépôts + service de retrait).
 *
 *   node coinbosa/scripts/check-exchange-rpc.js
 *   RPC=https://explorer.coinbosa.com/rpc node coinbosa/scripts/check-exchange-rpc.js
 *
 * Sur un réseau de DÉVELOPPEMENT (genesis régénéré à chaque exécution, validateur
 * jetable, donc empreinte du bloc 0 différente par construction) :
 *   ALLOW_DEV_HASH=1 GENESIS=genesis/genesis-coinbosa-dev.json \
 *   RPC=http://127.0.0.1:8545 node coinbosa/scripts/check-exchange-rpc.js
 * Cette dérogation ne se contente PAS d'être crue : elle exige que le RPC soit local,
 * que le fichier de genesis désigné porte le marqueur coinbosaDev, ET que l'en-tête du
 * bloc 0 servi corresponde à ce fichier. Voir « dérogation de développement » plus bas.
 *
 * chainId : la valeur attendue reste 26262 (coinbosa.config.json, valeur de PRODUCTION).
 * Le réseau de développement en porte un autre depuis que les deux chaînes ont été
 * séparées — elles ne devaient plus partager le nombre qui entre dans le hash de scellage
 * d'une double signature. Ce script n'accepte cette autre valeur que si la chaîne a
 * d'abord PROUVÉ être celle de développement, et il la LIT alors dans le fichier genesis
 * dont le bloc 0 servi porte la trace : jamais sur déclaration, jamais sur la seule
 * présence d'ALLOW_DEV_HASH.
 *
 * LECTURE SEULE : n'envoie aucune transaction et ne déplace aucun fonds. Une seule
 * ressource est créée sur le nœud — le filtre de eth_newBlockFilter — et elle est
 * libérée par eth_uninstallFilter juste après la mesure (geth garderait sinon un
 * filtre orphelin pendant ~5 min à chaque exécution). Les appels sont ESPACÉS
 * (PAUSE_MS) : la chaîne n'a qu'un validateur, ce script ne doit jamais ressembler
 * à un test de charge — la prison fail2ban caddy-rpc bannit à 1500 req/min.
 *
 * Sortie : un tableau méthode -> verdict, puis un code de retour.
 *   0 = aucun bloqueur
 *   1 = au moins un BLOQUEUR d'intégration
 *   2 = le contrôle lui-même n'a pas pu s'exécuter (référence illisible, exception).
 *       Un 2 n'est PAS un feu vert : dans ce cas RIEN n'a été prouvé.
 *
 * RÈGLE QUI GOUVERNE TOUT CE FICHIER
 * ----------------------------------
 * Quand un contrôle ne PEUT PAS vérifier, il crie. Un contrôle muet fait cesser la
 * vigilance : il est pire que pas de contrôle du tout. Concrètement, aucun verdict OK
 * ne repose sur la seule PRÉSENCE d'une réponse — il faut qu'une VALEUR reçue ait été
 * comparée à une référence (coinbosa.config.json, genesis-reference.json, le fichier
 * de genesis, ou le nombre d'éléments attendus). Un contrôle qui ne peut pas s'exécuter
 * produit une ligne BLOQUEUR « NON VÉRIFIÉ », jamais le silence.
 *
 * Ce script est désigné par deploy/73-caddy-ws-archive.snippet comme le feu vert pour
 * rebrancher /rpc sur le nœud d'archive : un faux vert ici fait basculer la production.
 */
'use strict';

const fs = require('fs');
const path = require('path');
const crypto = require('crypto');

const RPC = process.env.RPC || 'https://explorer.coinbosa.com/rpc';
const PAUSE_MS = Number(process.env.PAUSE_MS || 900);
const WS_MS = Number(process.env.WS_MS || 25000);
const RACINE = path.join(__dirname, '..');
const { request } = new URL(RPC).protocol === 'http:' ? require('http') : require('https');

const pause = (ms) => new Promise((r) => setTimeout(r, ms));

// Chargement fail-closed des références. Sans elles, ce script ne sait pas à quoi
// comparer ce qu'il reçoit : il ne peut donc rien affirmer, et il sort en 2 plutôt
// que de dérouler des contrôles qui n'auraient plus de point de comparaison.
function lireJson(chemin, role) {
  try { return JSON.parse(fs.readFileSync(chemin, 'utf8')); }
  catch (e) {
    console.error(`ÉCHEC DU CONTRÔLE : ${role} illisible (${chemin}) : ${e.message}`);
    console.error('  Sans cette référence, aucun verdict ne peut être prononcé : rien n\'est vérifié.');
    process.exit(2);
  }
}
const CONFIG = lireJson(path.join(RACINE, 'coinbosa.config.json'), 'coinbosa.config.json');
// chainId de PRODUCTION. C'est la référence par défaut, et la seule qui vaille tant que la
// chaîne interrogée n'a pas prouvé être autre chose : voir analyserIdentite().
const CHAIN_ID_CONFIG = Number(CONFIG.network.chainId);

// ALLOW_DEV_HASH : même variable que check-genesis-hash.js, même sens — « la chaîne
// interrogée est un réseau de développement ». Elle n'est PAS un laissez-passer :
// elle bascule vers une preuve différente (cf. analyserIdentite), pas vers l'absence de preuve.
const ALLOW_DEV = process.env.ALLOW_DEV_HASH === '1';

const estBoucleLocale = (url) => {
  let hote;
  try { hote = new URL(url).hostname; } catch { return false; }
  return hote === 'localhost' || hote === '::1' || hote === '[::1]' || /^127\./.test(hote);
};
// Première condition de la dérogation, posée AVANT le moindre appel : un réseau de
// développement tourne sur la machine qui l'interroge. Ce qu'elle attrape est un accident
// réel — ALLOW_DEV_HASH resté exporté dans le shell (les modes opératoires en donnent une
// ligne à copier-coller), puis vérification d'une chaîne PUBLIQUE lancée dans la foulée :
// la dérogation aurait alors pu faire accepter le chainId du genesis de dév sur la chaîne
// de production. Même règle que check-genesis-hash.js, pour la même raison.
if (ALLOW_DEV && !estBoucleLocale(RPC)) {
  console.error(`ÉCHEC DU CONTRÔLE : ALLOW_DEV_HASH=1 vise un point d'accès DISTANT (${RPC}).`);
  console.error('  Un réseau de développement s\'interroge en local ; une chaîne distante est une vraie chaîne,');
  console.error('  et aucune dérogation de développement ne doit pouvoir s\'y appliquer.');
  console.error('  Retirer la variable (unset ALLOW_DEV_HASH) pour vérifier une chaîne distante.');
  process.exit(2);
}

// Fichier de genesis servant de référence d'allocation. Même convention que
// check-supply.js : GENESIS= le désigne, sinon production / dév selon ALLOW_DEV.
function cheminGenesis() {
  if (process.env.GENESIS) {
    const direct = path.resolve(process.env.GENESIS);
    if (fs.existsSync(direct)) return direct;
    // tolérance utile : GENESIS=genesis/… donné depuis un autre répertoire de travail
    const relatif = path.join(RACINE, process.env.GENESIS);
    return fs.existsSync(relatif) ? relatif : direct;
  }
  return path.join(RACINE, 'genesis', ALLOW_DEV ? 'genesis-coinbosa-dev.json' : 'genesis-coinbosa.json');
}
const GENESIS_FILE = cheminGenesis();

let seq = 0;
function call(payload, { timeout = 45000 } = {}) {
  const body = Buffer.from(JSON.stringify(payload));
  const u = new URL(RPC);
  return new Promise((resolve) => {
    const req = request(
      { method: 'POST', hostname: u.hostname,
        // Le repli 443 était appliqué même en http:// — une URL http://hôte/rpc sans port
        // explicite était donc interrogée sur 443 et le nœud déclaré injoignable alors
        // qu'il répondait sur 80. Le repli suit désormais le schéma.
        port: u.port || (u.protocol === 'http:' ? 80 : 443),
        // u.search était jeté : une passerelle authentifiée par ?apikey=… recevait une
        // requête sans sa clé et répondait 401, ce qui se lisait comme « nœud en panne ».
        path: u.pathname + u.search,
        headers: { 'Content-Type': 'application/json', 'Content-Length': body.length } },
      (res) => {
        let d = '';
        res.on('data', (c) => (d += c));
        res.on('end', () => {
          let json = null;
          try { json = JSON.parse(d); } catch { /* corps non JSON : c'est une information */ }
          resolve({ status: res.statusCode, raw: d, json, bytes: Buffer.byteLength(d) });
        });
      }
    );
    req.setTimeout(timeout, () => { req.destroy(); resolve({ status: 0, raw: 'TIMEOUT', json: null, bytes: 0 }); });
    req.on('error', (e) => resolve({ status: 0, raw: 'ERREUR RESEAU: ' + e.message, json: null, bytes: 0 }));
    req.end(body);
  });
}

const rpc = (method, params = []) => call({ jsonrpc: '2.0', id: ++seq, method, params });

const results = [];
function note(nom, verdict, detail) {
  results.push({ nom, verdict, detail });
  const tag = { BLOQUEUR: 'BLOQUEUR ', DEGRADE: 'DEGRADE  ', OK: 'OK       ' }[verdict];
  console.log(`  ${tag} ${nom.padEnd(46)} ${detail}`);
}
// Ligne d'information : ne compte dans AUCUN décompte et ne peut donc jamais
// verdir un contrôle. Sert aux mesures qui éclairent sans rien prouver.
const info = (texte) => console.log(`  (info)     ${texte}`);

function errMsg(r) {
  if (r.status !== 200) return `HTTP ${r.status} corps=${JSON.stringify(String(r.raw).slice(0, 60))}`;
  if (!r.json) return `réponse non JSON : ${JSON.stringify(String(r.raw).slice(0, 60))}`;
  if (Array.isArray(r.json)) return `lot de ${r.json.length} réponse(s)`;
  if (r.json.error) return `erreur ${r.json.error.code} « ${r.json.error.message} »`;
  return 'résultat présent';
}
const ok = (r) => r.status === 200 && r.json && !Array.isArray(r.json) && r.json.result !== undefined && r.json.error === undefined;
// ok() accepte result:null — et deux null se comparaient « identiques », ce qui faisait
// passer une DOUBLE ABSENCE pour une preuve de cohérence. okv() exige une valeur.
const okv = (r) => ok(r) && r.json.result !== null;

// « result: null » n'est pas une erreur au sens JSON-RPC : errMsg() le décrivait comme
// « résultat présent », ce qui envoyait l'exploitant chercher la mauvaise panne. On
// distingue donc explicitement la réponse VIDE de la réponse en erreur.
const cause = (r) => (ok(r) && r.json.result === null ? 'réponse VIDE : result = null' : errMsg(r));
const hexEgal = (a, b) => typeof a === 'string' && typeof b === 'string' && a.toLowerCase() === b.toLowerCase();
const nbEgal = (a, b) => { try { return BigInt(a) === BigInt(b); } catch { return false; } };
// Une valeur de solde/quantité doit être une chaîne hexadécimale : null et undefined
// ne sont pas des états « servis », ce sont des absences.
const estHex = (v) => typeof v === 'string' && /^0x[0-9a-f]+$/i.test(v);
// Deux extraData de 166 octets qui ne diffèrent qu'au 33e (l'adresse du validateur) se
// résumaient l'un et l'autre en « 0x000000000000000000000000… » : le message nommait un
// écart que le lecteur ne pouvait pas voir. On situe donc l'écart au lieu de le tronquer.
const ecartHex = (recu, fichier) => {
  const a = String(recu).toLowerCase(), b = String(fichier).toLowerCase();
  if (a.length !== b.length) return `longueurs différentes (${a.length} vs ${b.length} caractères) — reçu ${a.slice(0, 20)}…, fichier ${b.slice(0, 20)}…`;
  let i = 2; while (i < a.length && a[i] === b[i]) i++;
  const fen = (s) => s.slice(Math.max(2, i - 4), i + 40);
  return `1er écart à l'octet ${Math.floor((i - 2) / 2)} : reçu …${fen(a)}…, fichier …${fen(b)}…`;
};

// Vérifie qu'un lot JSON-RPC est revenu ENTIER. Un lot tronqué (proxy qui coupe, limite
// de taille) laissait les sondes manquantes sans réponse et le verdict se prononçait
// quand même sur celles qui étaient arrivées : 1 réponse sur 6 suffisait à conclure
// « tout l'historique est servi ».
function lotComplet(r, attendu) {
  if (!Array.isArray(r.json)) return { bon: false, recu: 0, pourquoi: `réponse non conforme à un lot (${errMsg(r)})` };
  const ids = new Set(r.json.map((x) => x && x.id));
  if (r.json.length !== attendu || ids.size !== attendu) {
    return { bon: false, recu: r.json.length, pourquoi: `${r.json.length} réponse(s) et ${ids.size} identifiant(s) distincts pour ${attendu} sondes` };
  }
  return { bon: true, recu: r.json.length };
}

// Adresse la plus dotée du genesis : c'est elle qui sert d'ASSERTION DE VALEUR.
// La sonde historique 0x…1000 a un solde de 0x0 au genesis : un nœud qui répond « 0x0 »
// à tout la satisfait, elle ne peut donc pas distinguer « état servi » de « zéro par défaut ».
function allocationTemoin() {
  if (!fs.existsSync(GENESIS_FILE)) return null;
  let g; try { g = JSON.parse(fs.readFileSync(GENESIS_FILE, 'utf8')); } catch { return null; }
  if (!g || !g.alloc) return null;
  let choix = null;
  for (const [addr, v] of Object.entries(g.alloc)) {
    if (!v || !v.balance) continue;
    let solde; try { solde = BigInt(v.balance); } catch { continue; }
    if (solde === 0n) continue;
    if (!choix || solde > choix.solde) choix = { addr, solde, brut: v.balance };
  }
  return choix ? { ...choix, genesis: g } : null;
}

// ---------------------------------------------------------------------------
// Identité : ce qui distingue la production d'une chaîne de développement.
// ---------------------------------------------------------------------------
// L'empreinte du bloc 0 dépend du stateRoot, donc de TOUTE l'allocation initiale : c'est
// elle qui dit LAQUELLE des chaînes répond. Le chainId, lui, vit dans la section `config`
// du genesis et n'entre PAS dans le hash du bloc — deux réseaux peuvent avoir un bloc 0
// rigoureusement identique et n'être pas la même chaîne. Les deux se vérifient donc
// ensemble, et dans cet ordre : d'abord laquelle, ensuite le chainId qu'elle a le droit
// d'annoncer. C'est le même raisonnement que check-genesis-hash.js, appliqué ici parce
// qu'un rapport remis à une bourse ne peut pas décrire une chaîne sans avoir prouvé
// LAQUELLE il décrit.
//
// La production porte 26262 ; le réseau de développement porte désormais un autre chainId
// (build-genesis.js) — les deux chaînes ne devaient plus partager le nombre qui entre dans
// le hash de scellage d'une double signature, sans quoi une équivocation commise sur la
// chaîne de CI vaudrait preuve en production. Cette fonction en tire la seule règle
// admissible : la valeur attendue reste celle de coinbosa.config.json, et une autre n'est
// retenue QUE si la chaîne a prouvé être celle de développement — elle est alors LUE dans
// le fichier genesis dont le bloc 0 servi porte la trace, jamais déclarée.
//
// Renvoie { verdict, detail, infos, chainIdAttendu, prouveDev }.
function analyserIdentite(entete, chainIdVu) {
  const sortie = (verdict, detail, extra) => Object.assign(
    { verdict, detail, infos: [], chainIdAttendu: CHAIN_ID_CONFIG, prouveDev: false }, extra || {});
  const refFichier = path.join(RACINE, 'genesis', 'genesis-reference.json');
  let ref = null;
  if (fs.existsSync(refFichier)) {
    try {
      const p = JSON.parse(fs.readFileSync(refFichier, 'utf8'));
      if (p.hash && !/^0x0*$/.test(p.hash)) ref = p;
    } catch { /* traité comme absente ci-dessous */ }
  }

  const conformeProd = ref && hexEgal(entete.hash, ref.hash) && hexEgal(entete.stateRoot, ref.stateRoot) &&
                       (!ref.extraData || hexEgal(entete.extraData, ref.extraData));

  if (conformeProd) {
    // Le CLONE : bloc 0 rigoureusement identique à la production — hash, stateRoot,
    // extraData — mais autre chainId. C'est possible parce que le chainId n'est pas dans
    // l'en-tête ; c'est aussi le montage qui permettrait de faire passer une chaîne
    // parallèle pour la nôtre. Il n'y a rien à déroger ici : une chaîne qui porte le bloc 0
    // de la production n'est PAS un réseau de développement, ALLOW_DEV_HASH ou non.
    if (chainIdVu !== null && chainIdVu !== CHAIN_ID_CONFIG) {
      return sortie('BLOQUEUR',
        `bloc 0 identique à la PRODUCTION mais chainId ${chainIdVu} servi au lieu de ${CHAIN_ID_CONFIG} — c'est un CLONE : le chainId vit dans la section config du genesis et n'entre pas dans le hash du bloc, deux réseaux peuvent donc partager leur bloc 0 sans être la même chaîne ; aucune dérogation n'ouvre ce cas`);
    }
    return sortie('OK',
      `hash et stateRoot conformes à genesis-reference.json (figé le ${ref.fige_le || '?'})` +
      (ALLOW_DEV ? ' — ALLOW_DEV_HASH=1 était inutile : cette chaîne EST la production' : ''));
  }

  if (!ALLOW_DEV) {
    if (!ref) {
      return sortie('BLOQUEUR',
        'NON VÉRIFIÉ : aucune empreinte figée dans genesis/genesis-reference.json — impossible d\'affirmer QUELLE chaîne ce RPC sert ; figer l\'empreinte de production, ou lancer avec ALLOW_DEV_HASH=1 s\'il s\'agit d\'un réseau de développement');
    }
    return sortie('BLOQUEUR',
      `la chaîne servie N'EST PAS celle qui a été publiée — bloc 0 reçu hash=${String(entete.hash).slice(0, 18)}… stateRoot=${String(entete.stateRoot).slice(0, 18)}… ; attendu hash=${String(ref.hash).slice(0, 18)}… ; corriger RPC=, ou rebrancher le nœud sur le bon datadir`);
  }

  // --- dérogation de développement : elle doit se PROUVER ---------------------
  // Une dérogation crue sur parole rouvre exactement le trou qu'on bouche : il suffirait
  // d'exporter ALLOW_DEV_HASH=1 pour que n'importe quelle chaîne devienne « conforme ».
  // On exige donc deux choses vérifiables : (1) le fichier de genesis désigné porte le
  // marqueur coinbosaDev — c'est un artefact de développement, build-genesis.js ne le pose
  // jamais en production ; (2) l'en-tête du bloc 0 SERVI correspond à ce fichier-là.
  // L'extraData contient l'adresse du validateur jetable, tirée au sort à chaque
  // génération : c'est un lien fort entre la chaîne interrogée et ce fichier précis.
  if (!fs.existsSync(GENESIS_FILE)) {
    return sortie('BLOQUEUR',
      `NON VÉRIFIÉ : ALLOW_DEV_HASH=1 mais le genesis de développement est introuvable (${GENESIS_FILE}) — la dérogation ne peut pas prouver qu'elle porte bien sur un réseau de dév ; indiquer GENESIS=chemin/vers/genesis-coinbosa-dev.json`);
  }
  let dev; try { dev = JSON.parse(fs.readFileSync(GENESIS_FILE, 'utf8')); } catch (e) {
    return sortie('BLOQUEUR',
      `NON VÉRIFIÉ : ${path.basename(GENESIS_FILE)} illisible (${e.message}) — dérogation de développement non prouvable`);
  }
  if (dev.coinbosaDev !== true) {
    return sortie('BLOQUEUR',
      `ALLOW_DEV_HASH=1 refusé : ${path.basename(GENESIS_FILE)} ne porte PAS le marqueur coinbosaDev — ce n'est pas un genesis de développement, la dérogation ne s'applique pas ; retirer ALLOW_DEV_HASH ou désigner le bon fichier`);
  }
  // Le chainId attendu sera LU dans ce fichier : il doit donc y être, et être exploitable.
  // Un genesis sans config.chainId ne peut fonder aucune attente — on ne se rabat pas en
  // silence sur la valeur de production, on refuse la dérogation.
  const cidDev = dev.config ? Number(dev.config.chainId) : NaN;
  if (!Number.isInteger(cidDev) || cidDev <= 0) {
    return sortie('BLOQUEUR',
      `ALLOW_DEV_HASH=1 refusé : ${path.basename(GENESIS_FILE)} ne déclare pas de config.chainId exploitable (${dev.config ? JSON.stringify(dev.config.chainId) : 'section config absente'}) — sans lui, aucun chainId ne peut être attendu de cette chaîne`);
  }
  // Sans eth_chainId, le lien entre la chaîne servie et ce fichier resterait incomplet :
  // la dérogation ne s'accorde pas sur une comparaison qui n'a pas eu lieu.
  if (chainIdVu === null) {
    return sortie('BLOQUEUR',
      `NON VÉRIFIÉ : eth_chainId n'a pas répondu — le chainId servi ne peut pas être confronté à ${path.basename(GENESIS_FILE)}, la dérogation de développement reste refusée`);
  }

  const ecarts = [];
  if (!hexEgal(entete.extraData, dev.extraData)) ecarts.push(`extraData (il porte l'adresse du validateur inscrite au bloc 0) — ${ecartHex(entete.extraData, dev.extraData)}`);
  if (!nbEgal(entete.gasLimit, dev.gasLimit)) ecarts.push(`gasLimit : reçu ${entete.gasLimit}, fichier ${dev.gasLimit}`);
  if (dev.difficulty && !nbEgal(entete.difficulty, dev.difficulty)) ecarts.push(`difficulty : reçu ${entete.difficulty}, fichier ${dev.difficulty}`);
  if (dev.nonce && !nbEgal(entete.nonce, dev.nonce)) ecarts.push(`nonce : reçu ${entete.nonce}, fichier ${dev.nonce}`);
  // Le reste de l'en-tête écrit par `geth init` : chaque champ du fichier qui n'est comparé
  // à rien est un champ par lequel une autre chaîne pourrait passer. On les prend tous,
  // sauf stateRoot — voir la ligne (info) rendue avec le verdict.
  if (dev.timestamp !== undefined && !nbEgal(entete.timestamp, dev.timestamp)) ecarts.push(`timestamp : reçu ${entete.timestamp}, fichier ${dev.timestamp}`);
  if (dev.mixHash && !hexEgal(entete.mixHash, dev.mixHash)) ecarts.push(`mixHash : reçu ${entete.mixHash}, fichier ${dev.mixHash}`);
  if (dev.coinbase && !hexEgal(entete.miner, dev.coinbase)) ecarts.push(`coinbase : reçu ${entete.miner}, fichier ${dev.coinbase}`);
  if (dev.parentHash && !hexEgal(entete.parentHash, dev.parentHash)) ecarts.push(`parentHash : reçu ${entete.parentHash}, fichier ${dev.parentHash}`);
  if (dev.gasUsed !== undefined && !nbEgal(entete.gasUsed, dev.gasUsed)) ecarts.push(`gasUsed : reçu ${entete.gasUsed}, fichier ${dev.gasUsed}`);
  if (cidDev !== chainIdVu) ecarts.push(`chainId : servi ${chainIdVu}, fichier ${cidDev}`);

  if (ecarts.length) {
    return sortie('BLOQUEUR',
      `le bloc 0 servi ne correspond NI à la production NI au genesis de développement local (${path.basename(GENESIS_FILE)}) : ${ecarts.join(' ; ')} — le nœud interrogé a été initialisé avec un autre genesis`);
  }
  // Seulement ICI le chainId du fichier devient la valeur attendue : après que l'en-tête du
  // bloc 0 SERVI a lié la chaîne à ce fichier précis (l'extraData porte l'adresse du
  // validateur jetable, tirée au sort à chaque génération). Ce n'est pas la variable
  // d'environnement qui l'accorde, c'est la chaîne qui l'a démontré.
  return sortie('OK',
    `réseau de DÉVELOPPEMENT prouvé : marqueur coinbosaDev + en-tête du bloc 0 liée à ${path.basename(GENESIS_FILE)} (extraData, gasLimit, difficulty, nonce, timestamp, mixHash, coinbase, parentHash, gasUsed) — chainId ${cidDev} DÉRIVÉ de ce fichier, non déclaré`,
    { chainIdAttendu: cidDev, prouveDev: true,
      infos: ['stateRoot NON comparé en mode développement (le recalculer exige de reconstruire l\'arbre de Merkle de l\'allocation : c\'est le travail de check-genesis-hash.js). L\'allocation servie est tout de même confrontée au fichier en section 3 (« allocation du genesis servie au bloc 0 »), et ce contrôle-là est BLOQUEUR.',
              'Ce que ce mode prouve : le nœud local a démarré sur CE fichier. Ce qu\'il ne prouve pas : quoi que ce soit sur la production — seul check-genesis-hash.js sans ALLOW_DEV_HASH le fait.'] });
}

// ---------------------------------------------------------------------------
// WebSocket : ouverture réelle du canal.
// ---------------------------------------------------------------------------
// La section 6 interrogeait eth_subscribe en HTTP POST, c'est-à-dire un AUTRE transport.
// Elle pouvait donc être verte sans qu'aucun point d'accès WebSocket n'existe (faux vert),
// et rouge alors qu'un wss:// fonctionnait parfaitement — geth refuse eth_subscribe hors
// WebSocket par construction (faux rouge). Ici on fait l'upgrade HTTP/1.1, on vérifie le
// 101 et le Sec-WebSocket-Accept, puis on s'abonne SUR CE CANAL et on attend une
// notification réelle : c'est ce qu'un indexeur de bourse fera.
function urlWebSocket() {
  if (process.env.WS) return process.env.WS;
  const u = new URL(RPC);
  const schema = u.protocol === 'https:' ? 'wss:' : 'ws:';
  // Devant un proxy (Caddy), /rpc et /ws cohabitent sur le même hôte : cf.
  // deploy/73-caddy-ws-archive.snippet. En direct sur geth en revanche, le port HTTP
  // ne sert PAS le WebSocket (écouteur distinct) : on prend l'adresse déclarée dans
  // coinbosa.config.json plutôt que de fabriquer une URL qui ne peut pas répondre.
  if (/\/rpc\/?$/.test(u.pathname)) return `${schema}//${u.host}${u.pathname.replace(/\/rpc\/?$/, '/ws')}`;
  if (/^(127\.|\[?::1\]?|localhost)/.test(u.hostname) && CONFIG.rpc && CONFIG.rpc.ws) return CONFIG.rpc.ws;
  return `${schema}//${u.host}/ws`;
}

function trameTexte(txt) {
  const p = Buffer.from(txt, 'utf8');
  const masque = crypto.randomBytes(4);
  let entete;
  if (p.length < 126) { entete = Buffer.from([0x81, 0x80 | p.length]); }
  else if (p.length < 65536) { entete = Buffer.alloc(4); entete[0] = 0x81; entete[1] = 0xfe; entete.writeUInt16BE(p.length, 2); }
  else { entete = Buffer.alloc(10); entete[0] = 0x81; entete[1] = 0xff; entete.writeBigUInt64BE(BigInt(p.length), 2); }
  const c = Buffer.from(p);
  for (let i = 0; i < c.length; i++) c[i] ^= masque[i % 4];
  return Buffer.concat([entete, masque, c]);
}

function essaiWebSocket(cible, msMax) {
  return new Promise((resolve) => {
    let u;
    try { u = new URL(cible); } catch { return resolve({ etape: 'url', detail: `URL WebSocket invalide : ${cible}` }); }
    const secure = u.protocol === 'wss:' || u.protocol === 'https:';
    const mod = secure ? require('https') : require('http');
    const cle = crypto.randomBytes(16).toString('base64');
    const accepteAttendu = crypto.createHash('sha1').update(cle + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64');

    let etape = 'connexion', detail = 'aucune réponse', sousId = null, notifs = 0, erreurAbo = null, fini = false, sock = null;
    const rendre = () => {
      if (fini) return; fini = true;
      clearTimeout(minuteur);
      try { if (sock) sock.destroy(); } catch { /* canal déjà fermé */ }
      try { req.destroy(); } catch { /* requête déjà terminée */ }
      resolve({ etape, detail, sousId, notifs, erreurAbo, url: cible });
    };
    const minuteur = setTimeout(rendre, msMax);

    // Une URL malformée (port hors bornes, hôte invalide) fait lever request() de façon
    // SYNCHRONE : sans ce filet, tout le contrôle sortait en 2 — donc sans aucun verdict —
    // à cause d'une seule variable d'environnement mal saisie.
    let req;
    try {
      req = mod.request({
        method: 'GET', hostname: u.hostname, port: u.port || (secure ? 443 : 80),
        path: (u.pathname || '/') + (u.search || ''),
        headers: { Host: u.host, Connection: 'Upgrade', Upgrade: 'websocket', 'Sec-WebSocket-Version': '13', 'Sec-WebSocket-Key': cle },
      });
    } catch (e) {
      clearTimeout(minuteur);
      return resolve({ etape: 'url', detail: `URL WebSocket inutilisable (${cible}) : ${e.message}`, sousId: null, notifs: 0, erreurAbo: null, url: cible });
    }

    req.on('error', (e) => { etape = 'connexion'; detail = e.message; rendre(); });
    // Réponse HTTP normale = l'upgrade a été REFUSÉ : aucun WebSocket ici.
    req.on('response', (res) => { etape = 'refus'; detail = `HTTP ${res.statusCode} au lieu de 101`; res.resume(); rendre(); });

    req.on('upgrade', (res, socket) => {
      sock = socket;
      if (res.statusCode !== 101) { etape = 'refus'; detail = `HTTP ${res.statusCode} au lieu de 101`; return rendre(); }
      if (res.headers['sec-websocket-accept'] !== accepteAttendu) {
        etape = 'poignee'; detail = 'Sec-WebSocket-Accept invalide : l\'interlocuteur ne parle pas WebSocket (proxy qui laisse passer l\'upgrade sans le traiter ?)';
        return rendre();
      }
      etape = 'ouvert'; detail = '101 accepté';
      socket.on('error', (e) => { detail += ` puis erreur de socket : ${e.message}`; rendre(); });
      socket.on('close', () => { if (!sousId || notifs === 0) { detail += ' puis canal fermé par le serveur'; rendre(); } });
      socket.write(trameTexte(JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'eth_subscribe', params: ['newHeads'] })));

      let tampon = Buffer.alloc(0), morceaux = [];
      socket.on('data', (bloc) => {
        tampon = Buffer.concat([tampon, bloc]);
        for (;;) {
          if (tampon.length < 2) return;
          const fin = (tampon[0] & 0x80) !== 0;
          const opcode = tampon[0] & 0x0f;
          let taille = tampon[1] & 0x7f, decalage = 2;
          if (taille === 126) { if (tampon.length < 4) return; taille = tampon.readUInt16BE(2); decalage = 4; }
          else if (taille === 127) { if (tampon.length < 10) return; taille = Number(tampon.readBigUInt64BE(2)); decalage = 10; }
          if (tampon[1] & 0x80) decalage += 4; // le serveur ne doit pas masquer, on tolère
          if (tampon.length < decalage + taille) return;
          const charge = tampon.slice(decalage, decalage + taille);
          tampon = tampon.slice(decalage + taille);
          if (opcode === 0x8) { etape = 'ouvert'; detail += ' puis fermeture demandée par le serveur'; return rendre(); }
          if (opcode === 0x9 || opcode === 0xa) continue; // ping/pong : sans objet ici
          if (opcode !== 0x1 && opcode !== 0x0) continue; // binaire : hors protocole JSON-RPC
          // REASSEMBLAGE DES FRAGMENTS. geth découpe un en-tête de bloc en trames de
          // 1024 octets avec FIN=0 (0x01 puis 0x00...) : traiter chaque fragment comme un
          // message entier faisait échouer JSON.parse et perdre TOUTES les notifications.
          // Le canal était alors déclaré muet alors qu'il fonctionnait parfaitement —
          // un faux rouge, l'accident symétrique de celui qu'on corrige ici.
          if (opcode === 0x1) morceaux = [charge]; else morceaux.push(charge);
          if (!fin) continue;
          const texte = Buffer.concat(morceaux).toString('utf8');
          morceaux = [];
          let m; try { m = JSON.parse(texte); } catch { continue; }
          if (m.id === 1) {
            if (m.error) { etape = 'abonnement'; erreurAbo = `${m.error.code} « ${m.error.message} »`; return rendre(); }
            sousId = m.result;
          } else if (m.method === 'eth_subscription' && m.params && m.params.subscription === sousId) {
            notifs++;
          }
          if (sousId && notifs >= 1) { etape = 'notifie'; return rendre(); }
        }
      });
    });
    req.end();
  });
}

(async () => {
  console.log(`RPC interrogé : ${RPC}`);
  console.log(`Références    : coinbosa.config.json (chainId ${CHAIN_ID_CONFIG}), genesis-reference.json, ${path.basename(GENESIS_FILE)}${ALLOW_DEV ? '  [ALLOW_DEV_HASH=1]' : ''}`);
  console.log(`Pause entre appels : ${PAUSE_MS} ms (lecture seule, aucun envoi de transaction)\n`);

  // ---- 1. Identité ------------------------------------------------------
  // Ce que cette section attrape : un RPC branché sur une AUTRE chaîne que celle qu'on
  // certifie — nœud resynchronisé depuis le genesis de dév, fork, ancien datadir,
  // variable RPC= erronée. Ces valeurs étaient auparavant AFFICHÉES sans jamais être
  // comparées à quoi que ce soit : une chaîne 1337 passait « conforme ».
  console.log('1. IDENTITÉ DE LA CHAÎNE');
  let chainIdVu = null;
  const cid = await rpc('eth_chainId'); await pause(PAUSE_MS);
  if (ok(cid)) chainIdVu = parseInt(cid.json.result, 16);
  const nv = await rpc('net_version'); await pause(PAUSE_MS);

  // Le bloc 0 est demandé ICI, avant que le chainId ne soit jugé. L'ordre n'est pas
  // cosmétique : c'est l'en-tête du bloc 0 qui établit LAQUELLE des chaînes répond, donc
  // quel chainId elle a le droit d'annoncer. Juger le chainId d'abord reviendrait à
  // trancher l'identité avant de l'avoir établie — et à devoir croire sur parole celui
  // qui lance le contrôle. Les lignes restent affichées dans l'ordre habituel.
  const b0 = await rpc('eth_getBlockByNumber', ['0x0', false]); await pause(PAUSE_MS);
  const identite = okv(b0)
    ? analyserIdentite(b0.json.result, chainIdVu)
    : { verdict: 'BLOQUEUR',
        detail: `NON VÉRIFIÉ : le bloc 0 n'est pas servi (${cause(b0)}) — sans lui, impossible de prouver QUELLE chaîne ce RPC dessert`,
        infos: [], chainIdAttendu: CHAIN_ID_CONFIG, prouveDev: false };
  // Valeur de PRODUCTION par défaut. Elle n'est remplacée que par analyserIdentite(), et
  // seulement après que la chaîne a prouvé être celle de développement.
  const chainIdAttendu = identite.chainIdAttendu;
  const source = identite.prouveDev ? path.basename(GENESIS_FILE) : 'coinbosa.config.json';

  if (!ok(cid)) {
    note('eth_chainId', 'BLOQUEUR', `${errMsg(cid)} — identité NON VÉRIFIÉE`);
  } else {
    const bon = chainIdVu === chainIdAttendu;
    note('eth_chainId', bon ? 'OK' : 'BLOQUEUR', bon
      ? `${cid.json.result} (${chainIdVu}) = chainId attendu` + (identite.prouveDev ? ` (dérivé de ${source}, réseau de développement PROUVÉ ci-dessous)` : '')
      : `${chainIdVu} servi, ${chainIdAttendu} attendu (${source}) — ce RPC ne sert PAS Coinbosa Chain : corriger RPC= ou rebrancher le nœud`);
  }

  if (!ok(nv)) {
    note('net_version', 'BLOQUEUR', `${errMsg(nv)} — identité NON VÉRIFIÉE`);
  } else if (identite.prouveDev) {
    // Sur le réseau de développement, networkId et chainId sont DÉLIBÉRÉMENT dissociés —
    // c'est même l'objet de la séparation des deux chaînes. Or le networkId n'est écrit
    // dans AUCUN fichier de genesis : c'est un drapeau de ligne de commande. Il n'existe
    // donc ici rien à quoi le comparer. Le déclarer OK serait un verdict sans comparaison
    // (exactement le faux vert qu'on a chassé de ces scripts) ; le déclarer BLOQUEUR serait
    // un faux rouge sur une chaîne de dév saine. On le MESURE et on dit ce qu'il ne prouve
    // pas — une ligne (info) ne compte dans aucun décompte et ne verdit rien.
    info(`net_version = ${nv.json.result} (eth_chainId annonce ${chainIdVu}) — réseau de développement : le networkId ne figure dans aucun genesis, rien ne permet de l'attendre, cette valeur ne vaut donc PAS verdict`);
  } else {
    // Recoupement des deux méthodes : un intermédiaire qui réécrit l'une sans l'autre
    // (ou un nœud mal configuré) se trahit ici, alors que chaque valeur prise seule
    // pouvait sembler plausible.
    const nvNum = Number(nv.json.result);
    const bon = nvNum === chainIdAttendu && (chainIdVu === null || nvNum === chainIdVu);
    note('net_version', bon ? 'OK' : 'BLOQUEUR', bon
      ? `${nv.json.result} — cohérent avec eth_chainId et avec coinbosa.config.json`
      : `${nv.json.result} servi, ${chainIdAttendu} attendu (eth_chainId annonce ${chainIdVu}) — réseau incohérent : les deux méthodes ne décrivent pas la même chaîne`);
  }

  const sy = await rpc('eth_syncing'); await pause(PAUSE_MS);
  // Un nœud en cours de synchronisation sert un état partiel : un indexeur qui s'y
  // branche croit avoir vu tous les dépôts alors qu'il en manque.
  if (!ok(sy)) note('eth_syncing', 'BLOQUEUR', errMsg(sy));
  else note('eth_syncing', sy.json.result === false ? 'OK' : 'BLOQUEUR',
    sy.json.result === false ? 'false — nœud à jour'
      : `${JSON.stringify(sy.json.result)} — le nœud SYNCHRONISE ENCORE : son historique est incomplet, attendre la fin avant de le donner à une bourse`);

  const bn = await rpc('eth_blockNumber'); await pause(PAUSE_MS);
  if (!ok(bn)) { note('eth_blockNumber', 'BLOQUEUR', errMsg(bn)); process.exit(1); }
  const head = parseInt(bn.json.result, 16);
  note('eth_blockNumber', 'OK', `tête = ${head}`);

  // Le verdict d'identité, établi plus haut (le bloc 0 a été lu avant que le chainId ne
  // soit jugé), est rendu ici pour que la lecture reste dans l'ordre : ce que le nœud
  // annonce, puis la preuve de ce qu'il est.
  note('empreinte du bloc 0 (identité de la chaîne)', identite.verdict, identite.detail);
  identite.infos.forEach((t) => info(t));

  // ---- 2. Méthodes exigées par un indexeur ------------------------------
  // Ce que cette section attrape : un nœud qui répond « 200 OK » mais ne sert pas de quoi
  // reconstruire les dépôts. Piège corrigé : quand le bloc 1 ne portait aucune transaction,
  // les deux contrôles de reçus disparaissaient EN SILENCE — aucune ligne, code de retour 0.
  console.log('\n2. MÉTHODES EXIGÉES PAR UN INDEXEUR');
  const b1 = await rpc('eth_getBlockByNumber', ['0x1', true]); await pause(PAUSE_MS);
  note('eth_getBlockByNumber (tx complètes)', okv(b1) ? 'OK' : 'BLOQUEUR',
    okv(b1) ? `bloc 1 : ${b1.json.result.transactions.length} tx` : `${errMsg(b1)} — un indexeur ne peut pas rejouer la chaîne`);

  // Recherche ACTIVE d'un bloc porteur de transactions : un contrôle qu'on ne peut pas
  // exécuter doit le dire, pas s'effacer. Sondage en UN SEUL lot (pas de martèlement).
  let porteur = null, txh = null, nbTxPorteur = 0;
  if (okv(b1) && b1.json.result.transactions.length) {
    porteur = 1; nbTxPorteur = b1.json.result.transactions.length; txh = b1.json.result.transactions[0].hash;
  } else if (head >= 1) {
    const pas = Math.max(1, Math.floor(head / 8));
    const candidats = [...new Set([2, 3, 4, 5, head, head - 1, head - 2,
      ...Array.from({ length: 8 }, (_, i) => 1 + i * pas)].filter((b) => b >= 1 && b <= head))].slice(0, 16);
    const lotB = candidats.map((b, i) => ({ jsonrpc: '2.0', id: i, method: 'eth_getBlockByNumber', params: ['0x' + b.toString(16), false] }));
    const rb = await call(lotB); await pause(PAUSE_MS);
    const etat = lotComplet(rb, candidats.length);
    if (etat.bon) {
      for (const r of rb.json.slice().sort((a, b) => a.id - b.id)) {
        const bloc = r.result;
        if (bloc && Array.isArray(bloc.transactions) && bloc.transactions.length) {
          porteur = candidats[r.id]; nbTxPorteur = bloc.transactions.length; txh = bloc.transactions[0]; break;
        }
      }
    }
    info(`bloc 1 sans transaction : ${candidats.length} blocs sondés pour en trouver un porteur${etat.bon ? '' : ` (lot incomplet : ${etat.pourquoi})`}`);
  }

  const blocRecus = '0x' + (porteur || 1).toString(16);
  const br = await rpc('eth_getBlockReceipts', [blocRecus]); await pause(PAUSE_MS);
  const attendusRecus = porteur ? nbTxPorteur : (okv(b1) ? b1.json.result.transactions.length : null);
  const brBon = okv(br) && Array.isArray(br.json.result) && (attendusRecus === null || br.json.result.length === attendusRecus);
  note('eth_getBlockReceipts', brBon ? 'OK' : 'BLOQUEUR', brBon
    ? `${br.json.result.length} reçu(s) sur le bloc ${porteur || 1}, soit exactement le nombre de tx du bloc`
    : okv(br) && Array.isArray(br.json.result)
      ? `${br.json.result.length} reçu(s) pour ${attendusRecus} tx sur le bloc ${porteur || 1} — comptes DIVERGENTS : la réconciliation d'une bourse manquera des dépôts`
      : `${errMsg(br)} — méthode indispensable à l'indexation par lot`);

  if (txh) {
    const tr = await rpc('eth_getTransactionReceipt', [txh]); await pause(PAUSE_MS);
    // Assertion de valeur : le reçu doit être CELUI qu'on a demandé.
    const trBon = okv(tr) && hexEgal(tr.json.result.transactionHash, txh);
    note('eth_getTransactionReceipt', trBon ? 'OK' : 'BLOQUEUR', trBon
      ? `reçu du bloc ${porteur} servi et rattaché à la bonne transaction`
      : okv(tr) ? `reçu servi pour ${String(tr.json.result.transactionHash).slice(0, 18)}… alors que ${String(txh).slice(0, 18)}… était demandé` : `${errMsg(tr)} — aucun service de dépôt ne peut confirmer une transaction`);
    if (brBon && trBon) {
      const same = JSON.stringify(br.json.result[0]) === JSON.stringify(tr.json.result);
      note('cohérence reçus (lot vs individuel)', same ? 'OK' : 'BLOQUEUR',
        same ? 'identiques champ pour champ' : `DIVERGENTS sur la tx ${String(txh).slice(0, 18)}… — deux vérités pour un même reçu, toute réconciliation casse`);
    } else {
      note('cohérence reçus (lot vs individuel)', 'BLOQUEUR',
        'NON VÉRIFIÉ : le recoupement exige que eth_getBlockReceipts ET eth_getTransactionReceipt aient tous deux répondu (voir les deux lignes ci-dessus)');
    }
  } else {
    // Le silence d'autrefois : deux contrôles disparaissaient sans laisser de trace.
    const ou = head >= 1 ? `aucune transaction trouvée sur les blocs sondés (tête ${head})` : 'chaîne vide';
    note('eth_getTransactionReceipt', 'BLOQUEUR',
      `NON VÉRIFIÉ : ${ou} — faire passer une transaction quelconque puis relancer, ou pointer RPC= sur un nœud portant l'historique`);
    note('cohérence reçus (lot vs individuel)', 'BLOQUEUR',
      'NON VÉRIFIÉ : sans transaction disponible, le recoupement lot/individuel n\'a pas pu être exécuté');
  }

  // La plage est bornée à la tête : geth refuse « invalid block range params » dès qu'on
  // demande au-delà du dernier bloc, ce qui faisait échouer ce contrôle sur toute chaîne
  // de moins de 100 blocs — un nœud sain déclaré inapte. Une barrière qui crie à tort
  // finit désarmée ; on mesure donc la plus grande plage réellement demandable.
  const finPlage = Math.min(100, head);
  const gl = await rpc('eth_getLogs', [{ fromBlock: '0x0', toBlock: '0x' + finPlage.toString(16) }]); await pause(PAUSE_MS);
  note(`eth_getLogs (petite plage, ${finPlage} blocs)`, okv(gl) && Array.isArray(gl.json.result) ? 'OK' : 'BLOQUEUR',
    okv(gl) && Array.isArray(gl.json.result) ? `${gl.json.result.length} journal(aux)` : `${errMsg(gl)} — pas de détection des dépôts de jetons`);

  const glBig = await rpc('eth_getLogs', [{ fromBlock: '0x0', toBlock: 'latest' }]); await pause(PAUSE_MS);
  note('eth_getLogs (plage totale 0 -> latest)', okv(glBig) && Array.isArray(glBig.json.result) ? 'OK' : 'DEGRADE',
    okv(glBig) && Array.isArray(glBig.json.result) ? 'servie intégralement' : errMsg(glBig) + ' — découpage obligatoire côté bourse');

  const fh = await rpc('eth_feeHistory', ['0x5', 'latest', [25, 50, 75]]); await pause(PAUSE_MS);
  note('eth_feeHistory', okv(fh) ? 'OK' : 'DEGRADE', okv(fh) ? 'servie' : errMsg(fh));

  const dt = await rpc('debug_traceBlockByNumber', ['0x1', {}]); await pause(PAUSE_MS);
  note('debug_traceBlockByNumber', okv(dt) ? 'OK' : 'DEGRADE',
    okv(dt) ? 'servie' : errMsg(dt) + ' — pas de détection des transferts internes');

  // ---- 3. Profondeur d'historique (LE point de blocage classique) -------
  // Ce que cette section attrape : un nœud ÉLAGUÉ présenté comme nœud d'archive. Un
  // indexeur de bourse rejoue depuis le bloc 0 ; s'il ne peut pas lire l'état ancien,
  // l'intégration s'arrête. Trois pièges corrigés ici :
  //   - « présence » ≠ « valeur » : result:null passait pour « état SERVI » ;
  //   - la sonde 0x…1000 vaut 0x0 au genesis, donc un nœud qui répond « 0x0 » à tout
  //     la satisfait : elle ne peut structurellement pas prouver qu'un état est servi ;
  //   - un lot tronqué (1 réponse sur 6) fondait quand même le verdict « archive ».
  console.log("\n3. PROFONDEUR D'HISTORIQUE (état)");
  const temoin = allocationTemoin();
  if (!temoin) {
    note('allocation du genesis servie au bloc 0', 'BLOQUEUR',
      `NON VÉRIFIÉ : aucune adresse financée lisible dans ${GENESIS_FILE} — sans référence d'allocation, « état servi » ne peut pas être distingué de « zéro par défaut » ; indiquer GENESIS=chemin/vers/le/genesis`);
  } else {
    const sv = await rpc('eth_getBalance', [temoin.addr, '0x0']); await pause(PAUSE_MS);
    if (!okv(sv) || !estHex(sv.json.result)) {
      note('allocation du genesis servie au bloc 0', 'BLOQUEUR',
        `état du bloc 0 NON servi (${cause(sv)}) — un indexeur qui rejoue depuis 0 ne peut pas démarrer ; basculer /rpc sur le nœud d'archive (deploy/73-node-archive.sh puis 73-caddy-ws-archive.snippet)`);
    } else if (BigInt(sv.json.result) !== temoin.solde) {
      note('allocation du genesis servie au bloc 0', 'BLOQUEUR',
        `${temoin.addr} vaut ${sv.json.result} au bloc 0, ${temoin.brut} attendu d'après ${path.basename(GENESIS_FILE)} — la chaîne servie n'a PAS l'allocation publiée (mauvais réseau, ou nœud qui renvoie 0x0 au lieu d'échouer)`);
    } else {
      note('allocation du genesis servie au bloc 0', 'OK',
        `${temoin.addr} = ${(temoin.solde / 10n ** 18n).toLocaleString('en-US')} BOSA, conforme à ${path.basename(GENESIS_FILE)}`);
    }
  }

  const sonde = '0x0000000000000000000000000000000000001000';
  const paliers = [...new Set([1, 1000, 100000, 200000, Math.max(1, head - 1000), Math.max(1, head - 1)]
    .filter((b) => b >= 1 && b <= head))].sort((a, b) => a - b);
  const lot = paliers.map((b, i) => ({ jsonrpc: '2.0', id: i, method: 'eth_getBalance', params: [sonde, '0x' + b.toString(16)] }));
  const rr = await call(lot); await pause(PAUSE_MS);
  const etatLot = lotComplet(rr, paliers.length);
  let plusAncienDispo = null;
  if (etatLot.bon) {
    for (const r of rr.json.slice().sort((a, b) => a.id - b.id)) {
      const b = paliers[r.id];
      // Seule une valeur hexadécimale prouve qu'un état a été SERVI : null et une
      // absence de champ sont des non-réponses, pas des états.
      const dispo = estHex(r.result);
      if (dispo && plusAncienDispo === null) plusAncienDispo = b;
      console.log(`     bloc ${String(b).padStart(9)} : ${dispo ? 'état SERVI' : 'état INDISPONIBLE — ' + (r.error && r.error.message || 'réponse vide : ' + JSON.stringify(r.result))}`);
    }
  }

  if (!etatLot.bon) {
    note('eth_getBalance au bloc 1 (rejeu depuis 0)', 'BLOQUEUR',
      `profondeur NON MESURÉE : ${etatLot.pourquoi} — le serveur (ou un proxy devant lui) n'a pas renvoyé le lot entier ; relancer, et vérifier la limite de taille de réponse du proxy avant de conclure quoi que ce soit sur l'archive`);
  } else {
    const archive = plusAncienDispo !== null && plusAncienDispo <= 1;
    note('eth_getBalance au bloc 1 (rejeu depuis 0)', archive ? 'OK' : 'BLOQUEUR',
      archive ? 'nœud archive : tout l\'historique est servi'
              : plusAncienDispo === null
                ? 'AUCUN état servi, à aucune hauteur — ce nœud ne peut alimenter aucun indexeur ; vérifier qu\'il tourne bien en --gcmode archive'
                : `état le plus ancien servi = bloc ${plusAncienDispo} ; tout ce qui précède est perdu pour un indexeur`);

    // Recherche dichotomique de la falaise exacte, si elle existe.
    if (!archive && plusAncienDispo !== null) {
      let bas = 1, haut = plusAncienDispo;
      while (haut - bas > 1) {
        const mid = Math.floor((bas + haut) / 2);
        const r = await rpc('eth_getBalance', [sonde, '0x' + mid.toString(16)]);
        await pause(PAUSE_MS);
        if (okv(r) && estHex(r.json.result)) haut = mid; else bas = mid;
      }
      console.log(`     => falaise d'état mesurée au bloc ${haut} (profondeur ${head - haut} blocs sous la tête)`);
    }
  }

  // ---- 4. Étiquettes de bloc utilisées pour les confirmations -----------
  // Ce que cette section attrape : une étiquette figée. Une bourse compte ses
  // confirmations dessus ; si « finalized » n'avance plus, les dépôts ne sont jamais
  // crédités — sans qu'aucune erreur ne soit jamais renvoyée.
  console.log('\n4. ÉTIQUETTES DE BLOC (confirmations de dépôt)');
  for (const tag of ['finalized', 'safe', 'pending']) {
    const r = await rpc('eth_getBlockByNumber', [tag, false]); await pause(PAUSE_MS);
    if (!okv(r) || !r.json.result.number) { note(`eth_getBlockByNumber("${tag}")`, tag === 'pending' ? 'DEGRADE' : 'BLOQUEUR', ok(r) ? 'result = null — étiquette non servie' : errMsg(r)); continue; }
    const n = parseInt(r.json.result.number, 16);
    const retard = head - n;
    const sain = retard < 1000;
    note(`eth_getBlockByNumber("${tag}")`, sain ? 'OK' : 'BLOQUEUR',
      sain ? `bloc ${n} (retard ${retard})` : `bloc ${n} — retard de ${retard} blocs : l'étiquette N'AVANCE PAS`);
  }

  // ---- 5. Bornes du service ---------------------------------------------
  // Ce que cette section attrape : ce que reçoit une bourse qui dépasse une limite du
  // proxy. Piège corrigé : on jugeait sur le NOMBRE de réponses et sur le code HTTP —
  // 50 réponses toutes en erreur, ou un 200 accompagné d'une page HTML, passaient pour OK.
  console.log('\n5. BORNES DU SERVICE (ce que reçoit une bourse qui les dépasse)');
  const b50 = await call(Array.from({ length: 50 }, (_, i) => ({ jsonrpc: '2.0', id: i, method: 'eth_chainId', params: [] }))); await pause(PAUSE_MS);
  const etat50 = lotComplet(b50, 50);
  const erreur50 = Array.isArray(b50.json) ? b50.json.find((r) => !r || r.error !== undefined || r.result === undefined) : null;
  const bon50 = etat50.bon && !erreur50;
  note('lot de 50 appels', bon50 ? 'OK' : 'BLOQUEUR', bon50
    ? '50 réponses, toutes servies'
    : erreur50
      ? `50 réponses mais au moins une en échec : ${erreur50.error ? `erreur ${erreur50.error.code} « ${erreur50.error.message} »` : 'réponse sans result'} — le lot est accepté puis vidé de sa substance`
      : `${etat50.pourquoi} — un indexeur qui groupe ses appels attendra des réponses qui ne viendront pas`);

  const b51 = await call(Array.from({ length: 51 }, (_, i) => ({ jsonrpc: '2.0', id: i, method: 'eth_chainId', params: [] }))); await pause(PAUSE_MS);
  const b51n = Array.isArray(b51.json) ? b51.json.length : -1;
  note('lot de 51 appels (dépassement)', b51n === 51 ? 'OK' : 'DEGRADE',
    b51n === 1 ? `refus GLOBAL : 1 seule réponse « ${b51.json[0].error && b51.json[0].error.message} » — 50 id sans réponse, le client attend dans le vide`
               : `${b51n} réponse(s)`);

  const gros = Array.from({ length: 40 }, (_, i) => ({ jsonrpc: '2.0', id: i, method: 'eth_getBlockByNumber', params: ['0x' + (i + 1).toString(16), false], pad: 'x'.repeat(900) }));
  const rGros = await call(gros); await pause(PAUSE_MS);
  // Un 200 ne prouve rien : un WAF ou un proxy peut répondre 200 avec une page HTML,
  // que le client JSON-RPC ne sait pas lire. On exige le lot complet.
  const etatGros = lotComplet(rGros, 40);
  note(`corps de requête ${Math.round(Buffer.byteLength(JSON.stringify(gros)) / 1024)} KB`,
    rGros.status === 200 && etatGros.bon ? 'OK' : 'DEGRADE',
    rGros.status === 200 && etatGros.bon ? 'accepté, 40 réponses JSON-RPC'
      : `HTTP ${rGros.status}, ${etatGros.pourquoi} — corps « ${String(rGros.raw).slice(0, 40)} » ; augmenter request_body max_size côté proxy`);

  const lq = await rpc('eth_getLogs', [{ fromBlock: '0x0', toBlock: '0x' + finPlage.toString(16), address: Array.from({ length: 21 }, (_, i) => '0x' + (i + 1).toString(16).padStart(40, '0')) }]); await pause(PAUSE_MS);
  note('eth_getLogs à 21 adresses (dépassement)', okv(lq) ? 'OK' : 'DEGRADE', okv(lq) ? 'accepté' : errMsg(lq));

  // ---- 6. WebSocket ------------------------------------------------------
  // Ce que cette section attrape : l'absence de WebSocket réel. Elle n'en ouvrait
  // aucun — elle interrogeait eth_subscribe en HTTP POST, donc un autre transport.
  console.log('\n6. WEBSOCKET');
  const cibleWs = urlWebSocket();
  const avant = head;
  const wsr = await essaiWebSocket(cibleWs, WS_MS);
  const bnApres = await rpc('eth_blockNumber'); await pause(PAUSE_MS);
  const apres = ok(bnApres) ? parseInt(bnApres.json.result, 16) : avant;

  if (wsr.etape === 'notifie') {
    note('WebSocket newHeads (canal réel)', 'OK', `${cibleWs} : 101 accepté, abonnement ${String(wsr.sousId).slice(0, 12)}…, ${wsr.notifs} notification(s) reçue(s)`);
  } else if (wsr.etape === 'abonnement') {
    note('WebSocket newHeads (canal réel)', 'BLOQUEUR',
      `${cibleWs} accepte l'upgrade mais REFUSE eth_subscribe (${wsr.erreurAbo}) — un client abonné n'apprendra jamais un nouveau bloc ; activer l'API eth sur l'écouteur WS de geth (--ws.api eth,net,web3)`);
  } else if (wsr.etape === 'ouvert') {
    note('WebSocket newHeads (canal réel)', 'BLOQUEUR', apres > avant
      ? `${cibleWs} : abonné mais AUCUNE notification en ${Math.round(WS_MS / 1000)} s alors que la tête est passée de ${avant} à ${apres} — le canal est ouvert et ne délivre rien (${wsr.detail})`
      : `NON ÉPROUVÉ : aucun bloc produit pendant les ${Math.round(WS_MS / 1000)} s d'écoute (tête toujours à ${avant}) — la notification n'a pas pu être testée ; vérifier que la chaîne avance, puis relancer`);
  } else if (wsr.etape === 'poignee') {
    note('WebSocket newHeads (canal réel)', 'BLOQUEUR', `${cibleWs} : ${wsr.detail}`);
  } else {
    // Aucun point d'accès WebSocket : c'est l'état connu de la production (2026-08-30,
    // /rpc en upgrade -> 405, /ws -> 404). Dégradé et non bloqueur : la bourse peut
    // scruter — mais elle multiplie alors ses requêtes, et fail2ban bannit à 1500/min.
    note('WebSocket newHeads (canal réel)', 'DEGRADE',
      `aucun WebSocket servi sur ${cibleWs} (${wsr.detail}) — suivi de tête par SCRUTATION uniquement ; ouvrir /ws (coinbosa/deploy/73-caddy-ws-archive.snippet) ou pointer WS= sur le bon point d'accès, puis relancer`);
  }

  // La sonde HTTP ne vaut PLUS verdict : geth refuse eth_subscribe hors WebSocket par
  // construction, donc son échec ne prouve rien de mauvais, et sa réussite ne prouve
  // rien de bon (elle signale même un intermédiaire qui répond à la place du nœud).
  const sub = await rpc('eth_subscribe', ['newHeads']); await pause(PAUSE_MS);
  info(`sonde eth_subscribe en HTTP : ${ok(sub)
    ? `un identifiant est renvoyé (INHABITUEL : geth refuse les notifications sur HTTP — un intermédiaire répond peut-être à sa place). Ne prouve rien sur le WebSocket.`
    : `${errMsg(sub)} — attendu sur HTTP, sans conséquence : seul le canal WS ci-dessus fait foi.`}`);

  const nbf = await rpc('eth_newBlockFilter'); await pause(PAUSE_MS);
  note('eth_newBlockFilter (repli obligatoire)', okv(nbf) ? 'OK' : 'BLOQUEUR',
    okv(nbf) ? 'filtre créé' : `${errMsg(nbf)} — sans WebSocket NI filtre, il ne reste que la scrutation de eth_blockNumber`);
  if (okv(nbf)) {
    // On rend la ressource : geth garde un filtre non libéré ~5 min, et ce script est
    // relancé souvent. Un contrôle de lecture ne doit pas laisser de traces derrière lui.
    const del = await rpc('eth_uninstallFilter', [nbf.json.result]); await pause(PAUSE_MS);
    info(`filtre ${nbf.json.result} ${okv(del) && del.json.result === true ? 'libéré (eth_uninstallFilter)' : 'NON libéré : ' + errMsg(del) + ' — il expirera de lui-même côté nœud'}`);
  }

  // ---- 7. Cohérence ------------------------------------------------------
  // Ce que cette section attrape : deux lectures du même bloc qui ne donnent pas la même
  // chose (cache incohérent, grappe de nœuds désaccordés) — une bourse en tirerait deux
  // vérités pour un même dépôt. Piège corrigé : quand le bloc cible était introuvable,
  // les deux lectures valaient null et null === null était déclaré « identique octet pour
  // octet ». Le contrôle se comparait à une double absence.
  console.log('\n7. COHÉRENCE');
  const cibleNum = Math.max(1, head - 500);
  const cible = '0x' + cibleNum.toString(16);
  const a = await rpc('eth_getBlockByNumber', [cible, true]); await pause(Math.max(PAUSE_MS, 2500));
  const b = await rpc('eth_getBlockByNumber', [cible, true]); await pause(PAUSE_MS);
  if (!okv(a) || !okv(b)) {
    note('même bloc, 2 lectures espacées', 'BLOQUEUR',
      `NON VÉRIFIÉ : le bloc ${cibleNum} est absent d'au moins une des deux lectures (${!okv(a) ? cause(a) : cause(b)}) — cohérence non testée, et un bloc manquant est déjà un problème en soi`);
  } else {
    const stable = JSON.stringify(a.json.result) === JSON.stringify(b.json.result);
    note('même bloc, 2 lectures espacées', stable ? 'OK' : 'BLOQUEUR',
      stable ? 'identique octet pour octet' : `DIVERGENT sur le bloc ${cibleNum} — deux réponses différentes pour un bloc figé`);
  }
  if (okv(a) && a.json.result.hash) {
    const c = await rpc('eth_getBlockByHash', [a.json.result.hash, true]); await pause(PAUSE_MS);
    const meme = okv(c) && JSON.stringify(a.json.result) === JSON.stringify(c.json.result);
    note('lecture par numéro vs par empreinte', meme ? 'OK' : 'BLOQUEUR',
      meme ? 'identique' : `DIVERGENT : le bloc ${cibleNum} lu par empreinte ne redonne pas la même chose (${okv(c) ? 'contenu différent' : errMsg(c)})`);
  } else {
    note('lecture par numéro vs par empreinte', 'BLOQUEUR',
      `NON VÉRIFIÉ : pas d'empreinte exploitable pour le bloc ${cibleNum} (voir la ligne précédente)`);
  }

  // ---- Verdict -----------------------------------------------------------
  const bloqueurs = results.filter((r) => r.verdict === 'BLOQUEUR');
  const degrades = results.filter((r) => r.verdict === 'DEGRADE');
  console.log(`\n${'='.repeat(72)}`);
  console.log(`BLOQUEURS : ${bloqueurs.length}    DÉGRADÉS : ${degrades.length}    OK : ${results.length - bloqueurs.length - degrades.length}`);
  for (const r of bloqueurs) console.log(`  BLOQUEUR  ${r.nom} — ${r.detail}`);
  process.exit(bloqueurs.length ? 1 : 0);
})().catch((e) => {
  // Code 2 : le contrôle n'a pas pu s'exécuter. Ce n'est pas un feu vert — rien n'a
  // été prouvé — et c'est pour cela qu'il est documenté en tête de fichier.
  console.error('ÉCHEC DU CONTRÔLE :', e.message);
  console.error('  Aucun verdict n\'a été rendu : ne pas lire cette exécution comme une conformité.');
  process.exit(2);
});
