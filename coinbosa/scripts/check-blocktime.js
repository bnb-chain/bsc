// Vérifie que la chaîne produit bien des blocs de 5 s.
//
// Le temps de bloc n'est pas lu dans le genesis : c'est une constante Go du
// client. Ce contrôle est donc le seul moyen de détecter qu'on a démarré avec
// un binaire non patché — celui de BNB Chain produirait des blocs de 3 s.
const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

const RPC = process.env.RPC || 'http://127.0.0.1:8545';
const config = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'coinbosa.config.json'), 'utf8'));
const EXPECTED = config.network.blockIntervalMs / 1000;
const TOLERANCE = 0.5;
const SAMPLES = 6;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

(async () => {
  const provider = new ethers.JsonRpcProvider(RPC);

  // attendre d'avoir assez de blocs pour mesurer — avec une borne d'arrêt : si la
  // chaîne est figée, on échoue au lieu d'attendre indéfiniment.
  let head = await provider.getBlockNumber();
  const MAX_WAIT_S = Math.max(60, EXPECTED * (SAMPLES + 2) * 4);
  let waited = 0;
  while (head < SAMPLES + 1) {
    if (waited >= MAX_WAIT_S) {
      console.error(`\nECHEC : seulement ${head} bloc(s) après ${waited}s — la chaîne ne produit pas de blocs (validateur arrêté ?).`);
      process.exit(1);
    }
    await sleep(EXPECTED * 1000);
    waited += EXPECTED;
    head = await provider.getBlockNumber();
  }

  const times = [];
  for (let n = head - SAMPLES; n <= head; n++) {
    times.push((await provider.getBlock(n)).timestamp);
  }

  const deltas = [];
  for (let i = 0; i < times.length - 1; i++) deltas.push(times[i + 1] - times[i]);
  const avg = deltas.reduce((a, b) => a + b, 0) / deltas.length;

  console.log(`  intervalles mesurés : ${deltas.join(' s, ')} s`);
  console.log(`  moyenne             : ${avg.toFixed(2)} s`);
  console.log(`  attendu             : ${EXPECTED} s (± ${TOLERANCE})`);

  if (Math.abs(avg - EXPECTED) > TOLERANCE) {
    console.error(`\nECHEC : temps de bloc de ${avg.toFixed(2)} s au lieu de ${EXPECTED} s.`);
    console.error('Le client utilisé n’est probablement pas celui de ce dépôt.');
    console.error('Vérifiez consensus/parlia/parlia.go et recompilez avec `make geth`.');
    process.exit(1);
  }
  console.log('\n  temps de bloc conforme');
})().catch((e) => { console.error('ERREUR :', e.message); process.exit(1); });
