// Vérifie que l'offre native inscrite au genesis vaut exactement l'offre attendue.
//
// Les soldes sont lus AU BLOC 0 (genesis) : cela reflète l'allocation initiale sans
// être faussé par les frais brûlés/déposés après coup (EIP-1559), et compare le
// genesis DÉPLOYÉ (on-chain) au fichier local, adresse par adresse.
//
// Garanti : aucun solde hérité (le pont du réseau amont, notamment) ne subsiste,
// la répartition boucle sur le total, les contrats inter-chaînes sont sans code.
// Limite : ce contrôle itère les adresses du FICHIER local ; l'intégrité complète du
// genesis déployé (détection d'une adresse cachée absente du fichier) est garantie par
// la comparaison du HASH du bloc 0 en intégration continue, pas par ce script seul.
const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

const RPC = process.env.RPC || 'http://127.0.0.1:8545';
const config = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'coinbosa.config.json'), 'utf8'));
const genesis = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'genesis', 'genesis-coinbosa.json'), 'utf8'));

// Refus dur : un genesis de développement (adresses synthétiques, validateur crédité)
// ne doit jamais passer pour un genesis de production.
if (genesis.coinbosaDev) {
  console.error("ECHEC : genesis-coinbosa.json porte le marqueur coinbosaDev — genesis de DÉVELOPPEMENT, non déployable en production.");
  process.exit(1);
}

const EXPECTED = BigInt(config.nativeCoin.totalSupply) * 10n ** 18n;

(async () => {
  const provider = new ethers.JsonRpcProvider(RPC);

  let total = 0n;
  const mismatches = [];
  for (const [addr, v] of Object.entries(genesis.alloc)) {
    const declared = v.balance ? BigInt(v.balance) : 0n;
    if (declared === 0n) continue;
    const onchain = await provider.getBalance(addr, 0); // au bloc de genèse (block 0)
    if (onchain !== declared) mismatches.push({ addr, declared, onchain });
    total += onchain;
  }

  // le contrat de pont hérité doit être vide en solde ET purgé de son bytecode
  const bridge = await provider.getBalance('0x0000000000000000000000000000000000001004', 0);
  const XCHAIN = ['0x0000000000000000000000000000000000001003','0x0000000000000000000000000000000000001004',
                  '0x0000000000000000000000000000000000001005','0x0000000000000000000000000000000000001006',
                  '0x0000000000000000000000000000000000001008','0x0000000000000000000000000000000000002000'];
  const withCode = [];
  for (const a of XCHAIN) { const c = await provider.getCode(a); if (c && c !== '0x') withCode.push(a); }

  const whole = (x) => (x / 10n ** 18n).toLocaleString('en-US');
  console.log(`  offre native on-chain : ${whole(total)} BOSA`);
  console.log(`  attendu               : ${whole(EXPECTED)} BOSA`);
  console.log(`  pont 0x…1004          : ${whole(bridge)} BOSA`);
  console.log(`  contrats inter-chaînes avec code : ${withCode.length}`);

  let ok = true;
  if (total !== EXPECTED) { console.error(`\nECHEC : offre de ${whole(total)}, attendu ${whole(EXPECTED)}.`); ok = false; }
  if (bridge !== 0n) { console.error(`\nECHEC : le pont hérité détient encore ${whole(bridge)} BOSA.`); ok = false; }
  if (withCode.length) { console.error(`\nECHEC : contrats inter-chaînes hérités encore présents (bytecode) : ${withCode.join(', ')}`); ok = false; }
  if (mismatches.length) {
    console.error('\nECHEC : soldes on-chain divergents du genesis :');
    mismatches.forEach((m) => console.error(`  ${m.addr} : ${whole(m.onchain)} au lieu de ${whole(m.declared)}`));
    ok = false;
  }
  if (!ok) process.exit(1);
  console.log('\n  offre native conforme, contrats inter-chaînes purgés');
})().catch((e) => { console.error('ERREUR :', e.message); process.exit(1); });
