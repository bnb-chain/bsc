// Construit le genesis Coinbosa définitif.
//
//   VALIDATOR=0x... node scripts/build-genesis.js
//   VALIDATOR=0x... ALLOW_DEV=1 node scripts/build-genesis.js   (vérification locale)
//
// Ce que fait ce script :
//   1. remplace le contrat système 0x…1000 par CoinbosaValidatorSet — sans quoi la
//      chaîne se fige au premier bloc d'epoch (le bytecode hérité de 2021 n'expose pas
//      getMiningValidators()) ;
//   2. purge tout solde hérité du réseau amont — notamment les ~180 M de coins verrouillés
//      dans le contrat de pont 0x…1004, qui n'a aucun objet sur une chaîne souveraine ;
//   3. alloue l'intégralité de l'offre — 700 000 000 BOSA — aux adresses de la répartition,
//      selon les parts de coinbosa.config.json, avec un contrôle que le total est exact ;
//   4. inscrit le validateur dans l'extraData du genesis.
const fs = require('fs');
const path = require('path');
const solc = require('solc');
const { ethers } = require('ethers');

const VALIDATOR = process.env.VALIDATOR;
const ALLOW_DEV = process.env.ALLOW_DEV === '1';
const ROOT = path.join(__dirname, '..');
const BASE = process.env.BASE || path.join(ROOT, 'genesis', 'genesis-base.json');
const OUT = process.env.OUT || path.join(ROOT, 'genesis', 'genesis-coinbosa.json');
const SOL = path.join(ROOT, 'contracts', 'CoinbosaValidatorSet.sol');
const CONFIG = JSON.parse(fs.readFileSync(path.join(ROOT, 'coinbosa.config.json'), 'utf8'));
const ADDR_MAP = JSON.parse(fs.readFileSync(path.join(ROOT, 'genesis', 'distribution-addresses.json'), 'utf8'));

if (!VALIDATOR || !ethers.isAddress(VALIDATOR)) {
  console.error('VALIDATOR manquant ou invalide.\n  VALIDATOR=0x… node scripts/build-genesis.js');
  process.exit(1);
}

const ZERO = '0x0000000000000000000000000000000000000000';
const WEI = 10n ** 18n;
const TOTAL_SUPPLY = BigInt(CONFIG.nativeCoin.totalSupply); // 700 000 000
const dist = Object.entries(CONFIG.distribution).filter(([k]) => !k.startsWith('$'));

// La répartition doit boucler à 100 %.
const pctSum = dist.reduce((s, [, p]) => s + p, 0);
if (pctSum !== 100) { console.error(`ERREUR : la répartition totalise ${pctSum} %, 100 attendu.`); process.exit(1); }

// Adresse de chaque poste : réelle si fournie, sinon dérivée en mode développement.
function addressFor(post) {
  const a = ADDR_MAP[post];
  if (a && ethers.isAddress(a) && a !== ZERO) return ethers.getAddress(a);
  if (!ALLOW_DEV) {
    console.error(`ERREUR : adresse manquante pour le poste « ${post} » dans distribution-addresses.json.`);
    console.error('Renseignez une vraie adresse, ou lancez avec ALLOW_DEV=1 pour une vérification locale.');
    process.exit(1);
  }
  // adresse déterministe, non dépensable, clairement synthétique
  return ethers.getAddress('0x' + ethers.id('coinbosa-dev:' + post).slice(-40));
}

// --- 1. compiler le ValidatorSet avec le gouverneur voulu ---
let source = fs.readFileSync(SOL, 'utf8');
source = source.replace(/address public constant GOVERNOR = 0x[0-9a-fA-F]{40};/,
  `address public constant GOVERNOR = ${VALIDATOR};`);

const out = JSON.parse(solc.compile(JSON.stringify({
  language: 'Solidity',
  sources: { 'CoinbosaValidatorSet.sol': { content: source } },
  settings: {
    optimizer: { enabled: true, runs: 200 },
    evmVersion: 'shanghai',
    metadata: { bytecodeHash: 'none' },
    outputSelection: { '*': { '*': ['evm.deployedBytecode.object', 'abi'] } },
  },
})));
const errs = (out.errors || []).filter((e) => e.severity === 'error');
if (errs.length) { errs.forEach((e) => console.error(e.formattedMessage)); process.exit(1); }
const contract = out.contracts['CoinbosaValidatorSet.sol']['CoinbosaValidatorSet'];
const runtime = '0x' + contract.evm.deployedBytecode.object;

// --- 2. repartir des contrats système, sans aucun solde hérité ---
const base = JSON.parse(fs.readFileSync(BASE, 'utf8'));
const alloc = {};
let purged = 0n;
for (const [addr, v] of Object.entries(base.alloc)) {
  const isSystem = addr.toLowerCase().startsWith('0x' + '0'.repeat(35));
  const bal = v.balance ? BigInt(v.balance) : 0n;
  if (!isSystem) { purged += bal; continue; }        // on jette les comptes de test
  const entry = { balance: '0x0' };                   // geth exige un champ balance sur chaque compte
  if (v.code) entry.code = v.code;                    // on garde le code des contrats système
  if (v.storage) entry.storage = v.storage;
  if (bal > 0n) purged += bal;                        // on jette leur solde (pont 0x…1004)
  alloc[addr] = entry;
}

// 0x…1000 reçoit le contrat de consensus ; init() au bloc 1 renseignera son état.
const VALSET = '0x0000000000000000000000000000000000001000';
const oldSize = (base.alloc[VALSET].code.length - 2) / 2;
alloc[VALSET] = { balance: '0x0', code: runtime };

// --- 3. allouer l'offre selon la répartition ---
const g = { ...base, alloc };
let allocated = 0n;
const table = [];
for (const [post, pct] of dist) {
  const amount = (TOTAL_SUPPLY * BigInt(pct) / 100n) * WEI;
  const address = addressFor(post);
  if (alloc[address]) alloc[address].balance = '0x' + (BigInt(alloc[address].balance || 0) + amount).toString(16);
  else alloc[address] = { balance: '0x' + amount.toString(16) };
  allocated += amount;
  table.push([post, pct, address, amount / WEI]);
}

// contrôle dur : l'offre allouée doit valoir exactement 700 000 000 BOSA
const target = TOTAL_SUPPLY * WEI;
if (allocated !== target) {
  console.error(`ERREUR : offre allouée ${allocated} wei, attendu ${target} wei.`);
  process.exit(1);
}

// --- 4. extraData : 32B vanity + 1B compteur + (20B adresse + 48B clé BLS) + 65B sceau ---
g.extraData = '0x' + '00'.repeat(32) + '01' + VALIDATOR.slice(2).toLowerCase() + '00'.repeat(48) + '00'.repeat(65);

fs.writeFileSync(OUT, JSON.stringify(g, null, 1));
fs.writeFileSync(path.join(ROOT, 'genesis', 'CoinbosaValidatorSet.abi.json'), JSON.stringify(contract.abi, null, 2));

const extraLen = (g.extraData.length - 2) / 2;
console.log('solc              :', solc.version().split('+')[0]);
console.log('gouverneur        :', VALIDATOR);
console.log('code 0x…1000      :', oldSize, '->', (runtime.length - 2) / 2, 'octets');
console.log('extraData         :', extraLen, 'octets', extraLen === 166 ? '(conforme post-Luban)' : '(INATTENDU)');
console.log('solde hérité purgé:', (purged / WEI).toLocaleString('fr-FR'), 'coins (dont le pont 0x…1004)');
console.log('');
console.log('répartition de l\'offre' + (ALLOW_DEV ? ' — ADRESSES DE DÉVELOPPEMENT, non dépensables' : '') + ' :');
for (const [post, pct, address, whole] of table) {
  console.log(`  ${post.padEnd(26)} ${String(pct).padStart(3)} %  ${address}  ${whole.toLocaleString('fr-FR').padStart(13)}`);
}
console.log('  ' + '-'.repeat(72));
console.log(`  ${'TOTAL'.padEnd(26)} 100 %  ${''.padEnd(42)} ${(allocated / WEI).toLocaleString('fr-FR').padStart(13)} BOSA`);
console.log('\nécrit ->', OUT);
if (ALLOW_DEV) console.log('\n⚠  Adresses de développement : remplissez genesis/distribution-addresses.json avant la production.');
