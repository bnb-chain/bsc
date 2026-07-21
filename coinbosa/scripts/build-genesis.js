// Construit le genesis Coinbosa complet : contrat ValidatorSet + extraData + allocations.
//
//   VALIDATOR=0x... node scripts/build-genesis.js
//
// Le contrat CoinbosaValidatorSet remplace le BSCValidatorSet à l'adresse 0x...1000.
// C'est ce remplacement qui permet à la chaîne de franchir les blocs d'epoch : le
// bytecode hérité de BNB Chain est une version de 2021 dont la table de dispatch ne
// contient pas getMiningValidators(), la fonction que Parlia appelle tous les 200 blocs.
const fs = require('fs');
const path = require('path');
const solc = require('solc');

const VALIDATOR = process.env.VALIDATOR;
const BASE = process.env.BASE || path.join(__dirname, '..', 'genesis', 'genesis-base.json');
const OUT = process.env.OUT || path.join(__dirname, '..', 'genesis', 'genesis-coinbosa.json');
const SOL = path.join(__dirname, '..', 'contracts', 'CoinbosaValidatorSet.sol');

if (!VALIDATOR || !/^0x[0-9a-fA-F]{40}$/.test(VALIDATOR)) {
  console.error('VALIDATOR manquant ou invalide.\n  VALIDATOR=0x… node scripts/build-genesis.js');
  process.exit(1);
}

// --- 1. compiler le ValidatorSet avec le gouverneur voulu ---
let source = fs.readFileSync(SOL, 'utf8');
source = source.replace(/address public constant GOVERNOR = 0x[0-9a-fA-F]{40};/,
  `address public constant GOVERNOR = ${VALIDATOR};`);

const input = {
  language: 'Solidity',
  sources: { 'CoinbosaValidatorSet.sol': { content: source } },
  settings: {
    optimizer: { enabled: true, runs: 200 },
    evmVersion: 'shanghai',
    metadata: { bytecodeHash: 'none' },
    outputSelection: { '*': { '*': ['evm.deployedBytecode.object', 'abi'] } },
  },
};
const out = JSON.parse(solc.compile(JSON.stringify(input)));
const errs = (out.errors || []).filter((e) => e.severity === 'error');
if (errs.length) { errs.forEach((e) => console.error(e.formattedMessage)); process.exit(1); }

const c = out.contracts['CoinbosaValidatorSet.sol']['CoinbosaValidatorSet'];
const runtime = '0x' + c.evm.deployedBytecode.object;

// --- 2. assembler le genesis ---
const g = JSON.parse(fs.readFileSync(BASE, 'utf8'));
const VALSET = '0x0000000000000000000000000000000000001000';

const oldSize = (g.alloc[VALSET].code.length - 2) / 2;
g.alloc[VALSET].code = runtime;
delete g.alloc[VALSET].storage; // init() au bloc 1 renseigne l'état

// extraData post-Luban : 32B vanity + 1B compteur + (20B adresse + 48B clé BLS) + 65B sceau
const addr = VALIDATOR.slice(2).toLowerCase();
g.extraData = '0x' + '00'.repeat(32) + '01' + addr + '00'.repeat(48) + '00'.repeat(65);

// le validateur doit disposer de fonds pour payer le gas
g.alloc[VALIDATOR] = { balance: '0x21e19e0c9bab2400000' }; // 10 000

fs.writeFileSync(OUT, JSON.stringify(g, null, 1));
fs.writeFileSync(path.join(__dirname, '..', 'genesis', 'CoinbosaValidatorSet.abi.json'), JSON.stringify(c.abi, null, 2));

const extraLen = (g.extraData.length - 2) / 2;
console.log('solc                :', solc.version().split('+')[0]);
console.log('gouverneur          :', VALIDATOR);
console.log('code 0x…1000        :', oldSize, '->', (runtime.length - 2) / 2, 'octets');
console.log('extraData           :', extraLen, 'octets', extraLen === 166 ? '(conforme post-Luban)' : '(INATTENDU)');
console.log('contrats système    :', Object.keys(g.alloc).filter((k) => /^0x0{35}/.test(k)).length);
console.log('écrit ->', OUT);
