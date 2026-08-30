// Réconcilie la garde de l'offre : QUI détient les 700 000 000 BOSA, et sous quelle forme.
//
//   RPC=https://explorer.coinbosa.com/rpc node scripts/check-custody.js
//
// LECTURE SEULE. Aucune transaction n'est émise, aucune clé n'est lue, rien n'est écrit.
//
// Pourquoi ce contrôle en plus de check-supply.js
// -----------------------------------------------
// check-supply.js compare les soldes du FICHIER genesis à la chaîne. L'état du bloc 0
// ayant été purgé (nœud --gcmode full), il retombe sur le bloc courant : dès qu'un poste
// a bougé d'un wei, il annonce un ÉCHEC d'offre alors que l'offre est intacte — elle a
// seulement changé de main. Ce script raisonne autrement : il additionne TOUS les
// détenteurs connus (13 postes + gouverneur + contrat système) et exige le total exact.
//
// L'argument qui rend la somme suffisante :
//   1. l'empreinte du bloc 0 est figée et publiée (check-genesis-hash.js) : l'allocation
//      initiale est donc prouvée, et elle vaut exactement 700 000 000 BOSA ;
//   2. le moteur de consensus ne crée pas de monnaie et ne brûle rien (baseFee nulle) :
//      l'offre totale est donc constante ;
//   3. si un sous-ensemble de comptes totalise l'offre entière, TOUS les autres comptes
//      de la chaîne sont à zéro. Il n'y a pas de détenteur caché.
// C'est ce raisonnement — et non une énumération, impossible en JSON-RPC — qui ferme
// la question.
const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

const RPC = process.env.RPC || 'http://127.0.0.1:8545';
const ROOT = path.join(__dirname, '..');
const CONFIG = JSON.parse(fs.readFileSync(path.join(ROOT, 'coinbosa.config.json'), 'utf8'));
const ADDRS = JSON.parse(fs.readFileSync(path.join(ROOT, 'genesis', 'distribution-addresses.json'), 'utf8'));
const REF = JSON.parse(fs.readFileSync(path.join(ROOT, 'genesis', 'genesis-reference.json'), 'utf8'));

const VALSET = '0x0000000000000000000000000000000000001000';
const ATTENDU = BigInt(CONFIG.nativeCoin.totalSupply) * 10n ** 18n;
const PLAGE = 5000;                       // --rangelimit du nœud : 5 000 blocs par requête
const TOPIC = {
  '0xc45a2277fda002f812af4dda0deb46fbfa0eb91b3175b73a56e878af02bdf793': 'ValidatorSetUpdated',
  '0x1ed371ca1748e85e2d9554206ef61b0e69b21d41f30b4d4987d7b006fb4801cc': 'ValidatorDeposit',
  '0xc20af58baaae9e9347897f7e714ad24bf44b9bc415c3ad4bc843cc5f06e0c82a': 'ValidatorClaimed',
  '0x167beb24b4e0b809e5ad14deac40057e2206ebff29b4f1240a7d26bf69aaebe3': 'SurplusSwept',
};

const bosa = (w) => Number(ethers.formatEther(w)).toLocaleString('fr-FR', { maximumFractionDigits: 18 });
let echecs = 0;
const echec = (m) => { echecs++; console.error('  ÉCHEC : ' + m); };

(async () => {
  const p = new ethers.JsonRpcProvider(RPC, undefined, { staticNetwork: true });
  const reseau = await p.getNetwork();
  if (reseau.chainId !== BigInt(CONFIG.network.chainId)) {
    console.error(`ÉCHEC : chainId ${reseau.chainId}, attendu ${CONFIG.network.chainId}.`);
    process.exit(1);
  }
  const tete = await p.getBlockNumber();
  const bloc = await p.getBlock(tete);
  console.log(`\n  Garde de l'offre — Coinbosa Chain (chainId ${reseau.chainId})`);
  console.log(`  observé au bloc ${tete}, ${new Date(bloc.timestamp * 1000).toISOString()}`);
  console.log('  ' + '='.repeat(96));

  // --- 1. l'identité de la chaîne : l'allocation initiale est-elle celle qui est publiée ---
  const b0 = await p.getBlock(0);
  if (b0.hash !== REF.hash) echec(`empreinte du bloc 0 ${b0.hash}, référence ${REF.hash}`);
  else console.log(`\n  [1] bloc 0 conforme à genesis-reference.json — allocation initiale prouvée`);
  if (b0.stateRoot && REF.stateRoot && b0.stateRoot !== REF.stateRoot) {
    echec(`stateRoot du bloc 0 ${b0.stateRoot}, référence ${REF.stateRoot}`);
  }

  // --- 2. le gouverneur, lu DANS le bytecode figé, pas dans un fichier ---
  const c = new ethers.Contract(VALSET, [
    'function GOVERNOR() view returns (address)',
    'function INITIAL_VALIDATOR() view returns (address)',
    'function getValidators() view returns (address[])',
    'function totalInComing() view returns (uint256)',
  ], p);
  const gouverneur = await c.GOVERNOR();
  const validateur = await c.INITIAL_VALIDATOR();
  const valideurs = await c.getValidators();
  console.log(`\n  [2] gouverneur (constante du bytecode) : ${gouverneur}`);
  console.log(`      validateur de genèse                : ${validateur}`);
  console.log(`      jeu de validateurs courant          : ${valideurs.length} — ${valideurs.join(', ')}`);
  if (gouverneur.toLowerCase() === validateur.toLowerCase()) echec('gouverneur = validateur');

  // --- 3. chaque détenteur : solde, code, nombre de transactions émises ---
  console.log('\n  [3] détenteurs');
  console.log('  ' + '-'.repeat(96));
  console.log('      poste                        part      solde (BOSA)          code   nonce');
  let total = 0n;
  let sansCode = 0;
  const ligne = (nom, adr, part, solde, code, nonce) => {
    console.log(`      ${nom.padEnd(27)} ${String(part).padStart(6)}  ${bosa(solde).padStart(20)}  ${String(code).padStart(5)}o  ${String(nonce).padStart(6)}   ${adr}`);
  };
  for (const [poste, adr] of Object.entries(ADDRS)) {
    if (poste.startsWith('$')) continue;
    if (adr === ethers.ZeroAddress) continue;                     // réserve de migration nulle
    const solde = await p.getBalance(adr, tete);
    const code = (await p.getCode(adr, tete)).length / 2 - 1;
    const nonce = await p.getTransactionCount(adr, tete);
    total += solde;
    if (code === 0) sansCode++;
    ligne(poste, ethers.getAddress(adr), (CONFIG.distribution[poste] ?? '—') + ' %', solde, code, nonce);
  }
  const soldeGouv = await p.getBalance(gouverneur, tete);
  const codeGouv = (await p.getCode(gouverneur, tete)).length / 2 - 1;
  const nonceGouv = await p.getTransactionCount(gouverneur, tete);
  total += soldeGouv;
  ligne('GOUVERNEUR', ethers.getAddress(gouverneur), '—', soldeGouv, codeGouv, nonceGouv);

  const soldeValset = await p.getBalance(VALSET, tete);
  const dus = await c.totalInComing();
  total += soldeValset;
  ligne('contrat système 0x…1000', VALSET, 'frais', soldeValset, (await p.getCode(VALSET, tete)).length / 2 - 1, '—');
  if (soldeValset !== dus) {
    console.log(`      (dont ${bosa(dus)} dus aux validateurs via claim(), ${bosa(soldeValset - dus)} de surplus)`);
  }

  // --- 4. la somme doit tomber juste au wei ---
  console.log('  ' + '-'.repeat(96));
  console.log(`      total détenu : ${total.toString()} wei  (${bosa(total)} BOSA)`);
  console.log(`      offre fixée  : ${ATTENDU.toString()} wei  (${bosa(ATTENDU)} BOSA)`);
  if (total !== ATTENDU) echec(`écart de ${(total - ATTENDU).toString()} wei — il existe un détenteur non listé, ou une combustion`);
  else console.log('      écart        : 0 wei — aucun détenteur en dehors de cette liste');

  // --- 5. nature de la garde : combien de postes sont de simples clés ---
  console.log(`\n  [4] nature de la garde`);
  console.log(`      postes sans code (clé simple, ni multi-signatures ni délai) : ${sansCode} / ${Object.keys(ADDRS).filter((k) => !k.startsWith('$') && ADDRS[k] !== ethers.ZeroAddress).length}`);
  console.log(`      gouverneur sans code                                        : ${codeGouv === 0 ? 'oui' : 'non'}`);
  console.log('      (un contrat multi-signatures porterait du code ; 0 octet = clé unique)');

  // --- 6. tout ce que le contrat système a jamais émis ---
  console.log(`\n  [5] historique complet des événements du contrat système (0 → ${tete})`);
  const evenements = [];
  for (let from = 0; from <= tete; from += PLAGE) {
    const to = Math.min(from + PLAGE - 1, tete);
    const logs = await p.send('eth_getLogs', [{
      fromBlock: '0x' + from.toString(16), toBlock: '0x' + to.toString(16), address: VALSET,
    }]);
    for (const l of logs) evenements.push(l);
  }
  for (const l of evenements) {
    console.log(`      bloc ${String(parseInt(l.blockNumber, 16)).padStart(8)}  ${(TOPIC[l.topics[0]] || l.topics[0]).padEnd(20)}  tx ${l.transactionHash}`);
  }
  const rotations = evenements.filter((l) => TOPIC[l.topics[0]] === 'ValidatorSetUpdated').length;
  const balayages = evenements.filter((l) => TOPIC[l.topics[0]] === 'SurplusSwept').length;
  console.log(`      → ${evenements.length} événement(s). Rotations du jeu de validateurs : ${rotations}`);
  console.log(`        (1 = le init() du bloc 1 seul ; au-delà, le gouverneur a agi ${rotations - 1} fois)`);
  console.log(`      → sweepSurplus appelé : ${balayages} fois`);

  console.log('\n  ' + '='.repeat(96));
  if (echecs) { console.error(`\n  ${echecs} ÉCHEC(S).\n`); process.exit(1); }
  console.log('  Réconciliation complète : l\'offre est intégralement localisée.\n');
})().catch((e) => { console.error('ÉCHEC : ' + (e.shortMessage || e.message)); process.exit(1); });
