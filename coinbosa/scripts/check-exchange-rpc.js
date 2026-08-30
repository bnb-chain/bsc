#!/usr/bin/env node
/**
 * check-exchange-rpc.js — conformité du RPC public à ce qu'exige l'intégration
 * technique d'une place d'échange (indexeur de dépôts + service de retrait).
 *
 *   node coinbosa/scripts/check-exchange-rpc.js
 *   RPC=https://explorer.coinbosa.com/rpc node coinbosa/scripts/check-exchange-rpc.js
 *
 * LECTURE SEULE. N'envoie aucune transaction, ne déplace aucun fonds, ne modifie
 * rien sur le serveur. Les appels sont ESPACÉS (voir PAUSE_MS) : la chaîne n'a
 * qu'un validateur, ce script ne doit jamais ressembler à un test de charge.
 *
 * Sortie : un tableau méthode -> verdict, puis un code de retour.
 *   0 = aucun bloqueur   1 = au moins un BLOQUEUR d'intégration
 *
 * Un « bloqueur » n'est pas une opinion : c'est un comportement qui fait échouer
 * une intégration standard (indexeur qui rejoue depuis le bloc 0, service de
 * retrait qui lit un solde, sonde de santé). Chaque verdict cite la réponse reçue.
 */
'use strict';

const RPC = process.env.RPC || 'https://explorer.coinbosa.com/rpc';
const PAUSE_MS = Number(process.env.PAUSE_MS || 900);
const { request } = new URL(RPC).protocol === 'http:' ? require('http') : require('https');

const pause = (ms) => new Promise((r) => setTimeout(r, ms));

let seq = 0;
function call(payload, { timeout = 45000 } = {}) {
  const body = Buffer.from(JSON.stringify(payload));
  const u = new URL(RPC);
  return new Promise((resolve) => {
    const req = request(
      { method: 'POST', hostname: u.hostname, port: u.port || 443, path: u.pathname,
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

function errMsg(r) {
  if (r.status !== 200) return `HTTP ${r.status} corps=${JSON.stringify(String(r.raw).slice(0, 60))}`;
  if (!r.json) return `réponse non JSON : ${JSON.stringify(String(r.raw).slice(0, 60))}`;
  if (Array.isArray(r.json)) return `lot de ${r.json.length} réponse(s)`;
  if (r.json.error) return `erreur ${r.json.error.code} « ${r.json.error.message} »`;
  return 'résultat présent';
}
const ok = (r) => r.status === 200 && r.json && !Array.isArray(r.json) && r.json.result !== undefined && r.json.error === undefined;

(async () => {
  console.log(`RPC interrogé : ${RPC}`);
  console.log(`Pause entre appels : ${PAUSE_MS} ms (lecture seule, aucun envoi de transaction)\n`);

  // ---- 1. Identité ------------------------------------------------------
  console.log('1. IDENTITÉ DE LA CHAÎNE');
  const cid = await rpc('eth_chainId'); await pause(PAUSE_MS);
  note('eth_chainId', ok(cid) ? 'OK' : 'BLOQUEUR', ok(cid) ? `${cid.json.result} (${parseInt(cid.json.result, 16)})` : errMsg(cid));
  const nv = await rpc('net_version'); await pause(PAUSE_MS);
  note('net_version', ok(nv) ? 'OK' : 'BLOQUEUR', ok(nv) ? nv.json.result : errMsg(nv));
  const sy = await rpc('eth_syncing'); await pause(PAUSE_MS);
  note('eth_syncing', ok(sy) ? 'OK' : 'BLOQUEUR', ok(sy) ? JSON.stringify(sy.json.result) : errMsg(sy));
  const bn = await rpc('eth_blockNumber'); await pause(PAUSE_MS);
  if (!ok(bn)) { note('eth_blockNumber', 'BLOQUEUR', errMsg(bn)); process.exit(1); }
  const head = parseInt(bn.json.result, 16);
  note('eth_blockNumber', 'OK', `tête = ${head}`);

  // ---- 2. Méthodes exigées par un indexeur ------------------------------
  console.log('\n2. MÉTHODES EXIGÉES PAR UN INDEXEUR');
  const b1 = await rpc('eth_getBlockByNumber', ['0x1', true]); await pause(PAUSE_MS);
  note('eth_getBlockByNumber (tx complètes)', ok(b1) ? 'OK' : 'BLOQUEUR',
    ok(b1) ? `bloc 1 : ${b1.json.result.transactions.length} tx` : errMsg(b1));

  const br = await rpc('eth_getBlockReceipts', ['0x1']); await pause(PAUSE_MS);
  note('eth_getBlockReceipts', ok(br) ? 'OK' : 'BLOQUEUR',
    ok(br) ? `${br.json.result.length} reçu(s) sur le bloc 1` : errMsg(br));

  let txh = ok(b1) && b1.json.result.transactions[0] ? b1.json.result.transactions[0].hash : null;
  if (txh) {
    const tr = await rpc('eth_getTransactionReceipt', [txh]); await pause(PAUSE_MS);
    note('eth_getTransactionReceipt', ok(tr) && tr.json.result ? 'OK' : 'BLOQUEUR', ok(tr) ? 'reçu servi' : errMsg(tr));
    // Recoupement lot / individuel : une divergence casse toute réconciliation.
    if (ok(br) && ok(tr)) {
      const same = JSON.stringify(br.json.result[0]) === JSON.stringify(tr.json.result);
      note('cohérence reçus (lot vs individuel)', same ? 'OK' : 'BLOQUEUR', same ? 'identiques champ pour champ' : 'DIVERGENTS');
    }
  }

  const gl = await rpc('eth_getLogs', [{ fromBlock: '0x0', toBlock: '0x64' }]); await pause(PAUSE_MS);
  note('eth_getLogs (petite plage, 100 blocs)', ok(gl) ? 'OK' : 'BLOQUEUR', ok(gl) ? `${gl.json.result.length} journal(aux)` : errMsg(gl));

  const glBig = await rpc('eth_getLogs', [{ fromBlock: '0x0', toBlock: 'latest' }]); await pause(PAUSE_MS);
  note('eth_getLogs (plage totale 0 -> latest)', ok(glBig) ? 'OK' : 'DEGRADE',
    ok(glBig) ? 'servie intégralement' : errMsg(glBig) + ' — découpage obligatoire côté bourse');

  const fh = await rpc('eth_feeHistory', ['0x5', 'latest', [25, 50, 75]]); await pause(PAUSE_MS);
  note('eth_feeHistory', ok(fh) ? 'OK' : 'DEGRADE', ok(fh) ? 'servie' : errMsg(fh));

  const dt = await rpc('debug_traceBlockByNumber', ['0x1', {}]); await pause(PAUSE_MS);
  note('debug_traceBlockByNumber', ok(dt) ? 'OK' : 'DEGRADE',
    ok(dt) ? 'servie' : errMsg(dt) + ' — pas de détection des transferts internes');

  // ---- 3. Profondeur d'historique (LE point de blocage classique) -------
  console.log("\n3. PROFONDEUR D'HISTORIQUE (état)");
  const sonde = '0x0000000000000000000000000000000000001000';
  const paliers = [1, 1000, 100000, 200000, Math.max(1, head - 1000), head - 1];
  const lot = paliers.map((b, i) => ({ jsonrpc: '2.0', id: i, method: 'eth_getBalance', params: [sonde, '0x' + b.toString(16)] }));
  const rr = await call(lot); await pause(PAUSE_MS);
  let plusAncienDispo = null;
  if (Array.isArray(rr.json)) {
    for (const r of rr.json.sort((a, b) => a.id - b.id)) {
      const b = paliers[r.id];
      const dispo = r.result !== undefined;
      if (dispo && plusAncienDispo === null) plusAncienDispo = b;
      console.log(`     bloc ${String(b).padStart(9)} : ${dispo ? 'état SERVI' : 'état INDISPONIBLE — ' + (r.error && r.error.message || '')}`);
    }
  }
  const archive = plusAncienDispo !== null && plusAncienDispo <= 1;
  note('eth_getBalance au bloc 1 (rejeu depuis 0)', archive ? 'OK' : 'BLOQUEUR',
    archive ? 'nœud archive : tout l\'historique est servi'
            : `état le plus ancien servi = bloc ${plusAncienDispo} ; tout ce qui précède est perdu pour un indexeur`);

  // Recherche dichotomique de la falaise exacte, si elle existe.
  if (!archive && plusAncienDispo !== null) {
    let bas = 1, haut = plusAncienDispo;
    while (haut - bas > 1) {
      const mid = Math.floor((bas + haut) / 2);
      const r = await rpc('eth_getBalance', [sonde, '0x' + mid.toString(16)]);
      await pause(PAUSE_MS);
      if (ok(r)) haut = mid; else bas = mid;
    }
    console.log(`     => falaise d'état mesurée au bloc ${haut} (profondeur ${head - haut} blocs sous la tête)`);
  }

  // ---- 4. Étiquettes de bloc utilisées pour les confirmations -----------
  console.log('\n4. ÉTIQUETTES DE BLOC (confirmations de dépôt)');
  for (const tag of ['finalized', 'safe', 'pending']) {
    const r = await rpc('eth_getBlockByNumber', [tag, false]); await pause(PAUSE_MS);
    if (!ok(r) || r.json.result === null) { note(`eth_getBlockByNumber("${tag}")`, tag === 'pending' ? 'DEGRADE' : 'BLOQUEUR', ok(r) ? 'result = null' : errMsg(r)); continue; }
    const n = parseInt(r.json.result.number, 16);
    const retard = head - n;
    const sain = retard < 1000;
    note(`eth_getBlockByNumber("${tag}")`, sain ? 'OK' : 'BLOQUEUR',
      sain ? `bloc ${n} (retard ${retard})` : `bloc ${n} — retard de ${retard} blocs : l'étiquette N'AVANCE PAS`);
  }

  // ---- 5. Bornes du service ---------------------------------------------
  console.log('\n5. BORNES DU SERVICE (ce que reçoit une bourse qui les dépasse)');
  const b50 = await call(Array.from({ length: 50 }, (_, i) => ({ jsonrpc: '2.0', id: i, method: 'eth_chainId', params: [] }))); await pause(PAUSE_MS);
  note('lot de 50 appels', Array.isArray(b50.json) && b50.json.length === 50 ? 'OK' : 'BLOQUEUR',
    Array.isArray(b50.json) ? `${b50.json.length} réponse(s)` : errMsg(b50));

  const b51 = await call(Array.from({ length: 51 }, (_, i) => ({ jsonrpc: '2.0', id: i, method: 'eth_chainId', params: [] }))); await pause(PAUSE_MS);
  const b51n = Array.isArray(b51.json) ? b51.json.length : -1;
  note('lot de 51 appels (dépassement)', b51n === 51 ? 'OK' : 'DEGRADE',
    b51n === 1 ? `refus GLOBAL : 1 seule réponse « ${b51.json[0].error && b51.json[0].error.message} » — 50 id sans réponse, le client attend dans le vide`
               : `${b51n} réponse(s)`);

  const gros = Array.from({ length: 40 }, (_, i) => ({ jsonrpc: '2.0', id: i, method: 'eth_getBlockByNumber', params: ['0x' + (i + 1).toString(16), false], pad: 'x'.repeat(900) }));
  const rGros = await call(gros); await pause(PAUSE_MS);
  note(`corps de requête ${Math.round(Buffer.byteLength(JSON.stringify(gros)) / 1024)} KB`,
    rGros.status === 200 ? 'OK' : 'DEGRADE',
    rGros.status === 200 ? 'accepté' : `HTTP ${rGros.status}, corps « ${String(rGros.raw).slice(0, 40)} » — réponse NON JSON-RPC`);

  const lq = await rpc('eth_getLogs', [{ fromBlock: '0x0', toBlock: '0x64', address: Array.from({ length: 21 }, (_, i) => '0x' + (i + 1).toString(16).padStart(40, '0')) }]); await pause(PAUSE_MS);
  note('eth_getLogs à 21 adresses (dépassement)', ok(lq) ? 'OK' : 'DEGRADE', ok(lq) ? 'accepté' : errMsg(lq));

  // ---- 6. WebSocket ------------------------------------------------------
  console.log('\n6. WEBSOCKET');
  const sub = await rpc('eth_subscribe', ['newHeads']); await pause(PAUSE_MS);
  note('eth_subscribe (newHeads)', ok(sub) ? 'OK' : 'DEGRADE',
    ok(sub) ? 'servie' : errMsg(sub) + ' — suivi de tête par scrutation uniquement');
  const nbf = await rpc('eth_newBlockFilter'); await pause(PAUSE_MS);
  note('eth_newBlockFilter (repli obligatoire)', ok(nbf) ? 'OK' : 'BLOQUEUR', ok(nbf) ? 'filtre créé' : errMsg(nbf));

  // ---- 7. Cohérence ------------------------------------------------------
  console.log('\n7. COHÉRENCE');
  const cible = '0x' + Math.max(1, head - 500).toString(16);
  const a = await rpc('eth_getBlockByNumber', [cible, true]); await pause(2500);
  const b = await rpc('eth_getBlockByNumber', [cible, true]); await pause(PAUSE_MS);
  const stable = ok(a) && ok(b) && JSON.stringify(a.json.result) === JSON.stringify(b.json.result);
  note('même bloc, 2 lectures espacées', stable ? 'OK' : 'BLOQUEUR', stable ? 'identique octet pour octet' : 'DIVERGENT');
  if (ok(a)) {
    const c = await rpc('eth_getBlockByHash', [a.json.result.hash, true]); await pause(PAUSE_MS);
    const meme = ok(c) && JSON.stringify(a.json.result) === JSON.stringify(c.json.result);
    note('lecture par numéro vs par empreinte', meme ? 'OK' : 'BLOQUEUR', meme ? 'identique' : 'DIVERGENT');
  }

  // ---- Verdict -----------------------------------------------------------
  const bloqueurs = results.filter((r) => r.verdict === 'BLOQUEUR');
  const degrades = results.filter((r) => r.verdict === 'DEGRADE');
  console.log(`\n${'='.repeat(72)}`);
  console.log(`BLOQUEURS : ${bloqueurs.length}    DÉGRADÉS : ${degrades.length}    OK : ${results.length - bloqueurs.length - degrades.length}`);
  for (const r of bloqueurs) console.log(`  BLOQUEUR  ${r.nom} — ${r.detail}`);
  process.exit(bloqueurs.length ? 1 : 0);
})().catch((e) => { console.error('ÉCHEC DU CONTRÔLE :', e.message); process.exit(2); });
