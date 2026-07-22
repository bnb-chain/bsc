// Vérifie que l'offre native lue sur la chaîne vaut exactement l'offre attendue.
//
// C'est le contrôle qui garantit qu'aucun solde hérité (le pont du réseau amont
// portait ~180 M de coins) ne subsiste, et que la répartition boucle sur le total.
const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

const RPC = process.env.RPC || 'http://127.0.0.1:8545';
const config = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'coinbosa.config.json'), 'utf8'));
const genesis = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'genesis', 'genesis-coinbosa.json'), 'utf8'));

const EXPECTED = BigInt(config.nativeCoin.totalSupply) * 10n ** 18n;

(async () => {
  const provider = new ethers.JsonRpcProvider(RPC);

  let total = 0n;
  const mismatches = [];
  for (const [addr, v] of Object.entries(genesis.alloc)) {
    const declared = v.balance ? BigInt(v.balance) : 0n;
    if (declared === 0n) continue;
    const onchain = await provider.getBalance(addr);
    if (onchain !== declared) mismatches.push({ addr, declared, onchain });
    total += onchain;
  }

  // le contrat de pont hérité doit être vide
  const bridge = await provider.getBalance('0x0000000000000000000000000000000000001004');

  const whole = (x) => (x / 10n ** 18n).toLocaleString('en-US');
  console.log(`  offre native on-chain : ${whole(total)} BOSA`);
  console.log(`  attendu               : ${whole(EXPECTED)} BOSA`);
  console.log(`  pont 0x…1004          : ${whole(bridge)} BOSA`);

  let ok = true;
  if (total !== EXPECTED) { console.error(`\nECHEC : offre de ${whole(total)}, attendu ${whole(EXPECTED)}.`); ok = false; }
  if (bridge !== 0n) { console.error(`\nECHEC : le pont hérité détient encore ${whole(bridge)} BOSA.`); ok = false; }
  if (mismatches.length) {
    console.error('\nECHEC : soldes on-chain divergents du genesis :');
    mismatches.forEach((m) => console.error(`  ${m.addr} : ${whole(m.onchain)} au lieu de ${whole(m.declared)}`));
    ok = false;
  }
  if (!ok) process.exit(1);
  console.log('\n  offre native conforme');
})().catch((e) => { console.error('ERREUR :', e.message); process.exit(1); });
