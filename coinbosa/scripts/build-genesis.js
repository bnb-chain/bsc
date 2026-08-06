// Construit le genesis Coinbosa définitif.
//
//   VALIDATOR=0x... node scripts/build-genesis.js
//   VALIDATOR=0x... ALLOW_DEV=1 node scripts/build-genesis.js   (vérification locale)
//
// Ce que fait ce script :
//   1. remplace le contrat système 0x…1000 par CoinbosaValidatorSet — sans quoi la
//      chaîne se fige au premier bloc d'epoch (le bytecode hérité de 2021 n'expose pas
//      getMiningValidators()) ;
//   2. purge tout solde hérité du réseau amont — notamment celui verrouillé dans le
//      contrat de pont 0x…1004, qui n'a aucun objet sur une chaîne souveraine (le montant
//      réel purgé est calculé puis affiché en fin de script) ;
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
// Un genesis de DEV s'ecrit sur un chemin DISTINCT : il ne doit jamais ecraser
// ni etre confondu avec le genesis de production.
const OUT = process.env.OUT || path.join(ROOT, 'genesis', ALLOW_DEV ? 'genesis-coinbosa-dev.json' : 'genesis-coinbosa.json');
const SOL = path.join(ROOT, 'contracts', 'CoinbosaValidatorSet.sol');
const CONFIG = JSON.parse(fs.readFileSync(path.join(ROOT, 'coinbosa.config.json'), 'utf8'));
const ADDR_MAP = JSON.parse(fs.readFileSync(path.join(ROOT, 'genesis', 'distribution-addresses.json'), 'utf8'));

if (!VALIDATOR || !ethers.isAddress(VALIDATOR)) {
  console.error('VALIDATOR manquant ou invalide.\n  VALIDATOR=0x… node scripts/build-genesis.js');
  process.exit(1);
}

// --- Gouverneur du contrat système : distinct de la clé de scellage en production ---
// Le gouverneur peut modifier l'ensemble des validateurs et récupérer le surplus. La clé
// de scellage, elle, signe un bloc toutes les 5 secondes : elle vit forcément EN LIGNE sur
// le serveur du validateur. Les confondre reviendrait à laisser la gouvernance de la chaîne
// sur une machine exposée en permanence — la compromission d'un serveur donnerait le
// contrôle du consensus. En production, on exige donc deux adresses différentes, et le
// gouverneur doit être un coffre multi-signatures (idéalement derrière un délai).
const GOVERNOR_ENV = process.env.GOVERNOR;
if (!ALLOW_DEV) {
  if (!GOVERNOR_ENV || !ethers.isAddress(GOVERNOR_ENV)) {
    console.error('ERREUR : GOVERNOR manquant ou invalide.');
    console.error('  En production, le gouverneur doit être une adresse multi-signatures, distincte du validateur :');
    console.error('    VALIDATOR=0x… GOVERNOR=0x… node scripts/build-genesis.js');
    process.exit(1);
  }
  if (GOVERNOR_ENV.toLowerCase() === VALIDATOR.toLowerCase()) {
    console.error('ERREUR : GOVERNOR est identique au VALIDATOR.');
    console.error('  La clé de scellage est en ligne en permanence ; lui confier la gouvernance');
    console.error('  signifierait qu\'un serveur compromis emporte le contrôle du consensus.');
    console.error('  Utilisez un coffre multi-signatures distinct comme gouverneur.');
    process.exit(1);
  }
}
// En développement, le validateur fait office de gouverneur : c'est ce qui permet de
// piloter une chaîne locale avec une seule clé jetable.
const GOVERNOR = GOVERNOR_ENV || VALIDATOR;

const ZERO = '0x0000000000000000000000000000000000000000';
const WEI = 10n ** 18n;
const TOTAL_SUPPLY = BigInt(CONFIG.nativeCoin.totalSupply);       // 700 000 000
const MIGRATION_RESERVE = BigInt(CONFIG.migration.reserve);      // 0 (le projet contrôle tout l'historique Solana)
const PROJECT_ALLOCATION = BigInt(CONFIG.projectAllocation.amount); // 700 000 000
const dist = Object.entries(CONFIG.distribution).filter(([k]) => !k.startsWith('$'));

// Contrôle de cohérence : réserve de migration + allocation projet = offre totale.
if (MIGRATION_RESERVE + PROJECT_ALLOCATION !== TOTAL_SUPPLY) {
  console.error(`ERREUR : réserve (${MIGRATION_RESERVE}) + allocation projet (${PROJECT_ALLOCATION}) ≠ offre totale (${TOTAL_SUPPLY}).`);
  process.exit(1);
}

// En développement uniquement, le validateur détient le premier poste : cela donne au
// producteur de blocs des fonds pour payer le gas des tests, sans toucher au total de
// l'offre. En production, les adresses viennent toutes de distribution-addresses.json et
// le validateur, qui produit des blocs par transactions système gratuites, n'a pas besoin
// de solde.
if (ALLOW_DEV) ADDR_MAP[dist[0][0]] = VALIDATOR;

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

// Garde fail-closed : le bytecode du contrat système 0x…1000 — donc le HASH du bloc 0,
// donc l'identité de la chaîne — dépend de la version EXACTE de solc. On refuse toute autre
// version que celle épinglée (même garde que compile.js).
const SOLC_EXPECTED = '0.8.26';
if (!solc.version().startsWith(SOLC_EXPECTED)) {
  console.error(`ERREUR : solc ${solc.version()} détecté, ${SOLC_EXPECTED} attendu. Fige la version (npm ci) et recommence.`);
  process.exit(1);
}

// --- 1. compiler le ValidatorSet avec le gouverneur voulu ---
const GOV = ethers.getAddress(GOVERNOR); // adresse checksummee (Solidity exige EIP-55)
let source = fs.readFileSync(SOL, 'utf8');
const GOV_RE = /address public constant GOVERNOR = 0x[0-9a-fA-F]{40};/;
const srcBefore = source;
source = source.replace(GOV_RE, `address public constant GOVERNOR = ${GOV};`);
// Garde dure : sans confirmation du remplacement, le contrat compilerait avec le
// GOVERNOR par defaut (0x...0001) — gouverneur silencieusement faux dans le bytecode.
if (source === srcBefore || !source.includes(`address public constant GOVERNOR = ${GOV};`)) {
  console.error('ERREUR : injection du GOVERNOR echouee — motif introuvable dans CoinbosaValidatorSet.sol.');
  process.exit(1);
}

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
  // On purge le code des contrats inter-chaînes hérités du réseau amont — pont, cross-chain,
  // light client, relayers, token manager — qui n'ont aucun objet sur une chaîne souveraine.
  // Seuls les contrats réellement utilisés gardent leur bytecode : ValidatorSet (0x…1000,
  // remplacé plus bas), SlashIndicator (0x…1001), SystemReward (0x…1002), GovHub (0x…1007).
  const KEEP = ['0x0000000000000000000000000000000000001000','0x0000000000000000000000000000000000001001','0x0000000000000000000000000000000000001002','0x0000000000000000000000000000000000001007'];
  if (v.code && KEEP.includes(addr.toLowerCase())) entry.code = v.code;
  if (v.storage && KEEP.includes(addr.toLowerCase())) entry.storage = v.storage;
  if (bal > 0n) purged += bal;                        // on jette leur solde (pont 0x…1004)
  alloc[addr] = entry;
}

// 0x…1000 reçoit le contrat de consensus ; init() au bloc 1 renseignera son état.
const VALSET = '0x0000000000000000000000000000000000001000';
const oldSize = (base.alloc[VALSET].code.length - 2) / 2;
alloc[VALSET] = { balance: '0x0', code: runtime };

// --- 3. allouer l'offre : réserve de migration + treize postes sur l'allocation projet ---
const g = { ...base, alloc };
let allocated = 0n;
const table = [];

// a) réserve de migration, à une adresse dédiée (uniquement si elle est non nulle)
if (MIGRATION_RESERVE > 0n) {
  const migrationAddr = addressFor('__migration__');
  const migAmount = MIGRATION_RESERVE * WEI;
  alloc[migrationAddr] = { balance: '0x' + migAmount.toString(16) };
  allocated += migAmount;
  table.push(['réserve de migration', '', migrationAddr, MIGRATION_RESERVE]);
}

// b) les postes, pourcentages appliqués à l'allocation projet
// Garde d'unicité : deux postes partageant une adresse fusionneraient leurs soldes sans alerte,
// détruisant la séparation comptable alors que le total resterait juste. On l'interdit.
const seen = new Set();
for (const [post, pct] of dist) {
  const amount = (PROJECT_ALLOCATION * BigInt(pct) / 100n) * WEI;
  const address = addressFor(post);
  if (seen.has(address)) {
    console.error(`ERREUR : l'adresse ${address} est utilisée par plusieurs postes (${post}). Chaque poste doit avoir sa propre adresse.`);
    process.exit(1);
  }
  seen.add(address);
  if (alloc[address]) alloc[address].balance = '0x' + (BigInt(alloc[address].balance || 0) + amount).toString(16);
  else alloc[address] = { balance: '0x' + amount.toString(16) };
  allocated += amount;
  table.push([post, pct + ' %', address, amount / WEI]);
}

// contrôle dur : l'offre allouée doit valoir exactement l'offre totale
const target = TOTAL_SUPPLY * WEI;
if (allocated !== target) {
  console.error(`ERREUR : offre allouée ${allocated} wei, attendu ${target} wei.`);
  process.exit(1);
}

// --- 4. extraData : 32B vanity + 1B compteur + (20B adresse + 48B clé BLS) + 65B sceau ---
g.extraData = '0x' + '00'.repeat(32) + '01' + VALIDATOR.slice(2).toLowerCase() + '00'.repeat(48) + '00'.repeat(65);

// Marqueur : un genesis construit en mode developpement (adresses synthetiques non
// depensables + validateur credite du 1er poste) ne doit JAMAIS partir en production.
// check-supply.js (la porte de production) refuse un genesis portant ce marqueur.
// start-node.sh est lui volontairement DEV-only : il sert justement ce genesis de dev.
if (ALLOW_DEV) g.coinbosaDev = true;

fs.writeFileSync(OUT, JSON.stringify(g, null, 1));
fs.writeFileSync(path.join(ROOT, 'genesis', 'CoinbosaValidatorSet.abi.json'), JSON.stringify(contract.abi, null, 2));

const extraLen = (g.extraData.length - 2) / 2;
console.log('solc              :', solc.version().split('+')[0]);
console.log('validateur        :', ethers.getAddress(VALIDATOR), '(clé de scellage, en ligne)');
console.log('gouverneur        :', GOV, GOV.toLowerCase() === VALIDATOR.toLowerCase()
  ? '⚠  IDENTIQUE au validateur — développement uniquement'
  : '(distinct du validateur)');
console.log('code 0x…1000      :', oldSize, '->', (runtime.length - 2) / 2, 'octets');
console.log('extraData         :', extraLen, 'octets', extraLen === 166 ? '(conforme post-Luban)' : '(INATTENDU)');
console.log('solde hérité purgé:', (purged / WEI).toLocaleString('fr-FR'), 'coins (dont le pont 0x…1004)');
console.log('');
console.log('répartition de l\'offre' + (ALLOW_DEV ? ' — ADRESSES DE DÉVELOPPEMENT, non dépensables' : '') + ' :');
for (const [post, pct, address, whole] of table) {
  console.log(`  ${post.padEnd(28)} ${String(pct).padStart(5)}  ${address}  ${whole.toLocaleString('fr-FR').padStart(13)}`);
}
console.log('  ' + '-'.repeat(74));
console.log(`  ${'TOTAL'.padEnd(28)} ${''.padStart(5)}  ${''.padEnd(42)} ${(allocated / WEI).toLocaleString('fr-FR').padStart(13)} BOSA`);
console.log('\nécrit ->', OUT);
if (ALLOW_DEV) console.log('\n⚠  Adresses de développement : remplissez genesis/distribution-addresses.json avant la production.');
