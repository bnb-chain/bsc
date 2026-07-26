// Compile les contrats BRC20 avec résolution des imports locaux.
const fs = require('fs');
const path = require('path');
const solc = require('solc');

const SRC = path.join(__dirname, '..', 'contracts');
const OUT = path.join(__dirname, '..', 'build');

const sources = {};
for (const f of fs.readdirSync(SRC).filter((f) => f.endsWith('.sol'))) {
  sources[f] = { content: fs.readFileSync(path.join(SRC, f), 'utf8') };
}

const input = {
  language: 'Solidity',
  sources,
  settings: {
    optimizer: { enabled: true, runs: 200 },
    // Coinbosa Chain n'active pas Cancun : viser shanghai évite l'opcode MCOPY,
    // que la chaîne rejette avec "invalid opcode".
    evmVersion: 'shanghai',
    outputSelection: { '*': { '*': ['abi', 'evm.bytecode.object', 'evm.deployedBytecode.object'] } },
  },
};

function findImport(p) {
  const candidate = path.join(SRC, path.basename(p));
  if (fs.existsSync(candidate)) return { contents: fs.readFileSync(candidate, 'utf8') };
  return { error: 'introuvable : ' + p };
}

const out = JSON.parse(solc.compile(JSON.stringify(input), { import: findImport }));

const errors = (out.errors || []).filter((e) => e.severity === 'error');
const warnings = (out.errors || []).filter((e) => e.severity === 'warning');

if (errors.length) {
  console.error('ÉCHEC DE COMPILATION :');
  errors.forEach((e) => console.error(e.formattedMessage));
  process.exit(1);
}

fs.mkdirSync(OUT, { recursive: true });
// Garde fail-closed : refuse toute version de solc autre que celle épinglée.
// Le bytecode du consensus dépend de la version exacte du compilateur.
const SOLC_EXPECTED = '0.8.26';
if (!solc.version().startsWith(SOLC_EXPECTED)) {
  console.error(`ERREUR : solc ${solc.version()} détecté, ${SOLC_EXPECTED} attendu. Fige la version (npm ci) et recommence.`);
  process.exit(1);
}
console.log('solc', solc.version());
console.log('avertissements :', warnings.length);
warnings.forEach((w) => console.log('  ·', w.message.split('\n')[0]));
console.log('');

for (const [file, contracts] of Object.entries(out.contracts)) {
  for (const [name, c] of Object.entries(contracts)) {
    const size = c.evm.deployedBytecode.object.length / 2;
    if (size === 0) continue; // interfaces
    fs.writeFileSync(path.join(OUT, name + '.json'), JSON.stringify({ abi: c.abi, bytecode: c.evm.bytecode.object }, null, 2));
    const limit = 24576; // EIP-170
    console.log(
      `  ${name.padEnd(12)} ${String(size).padStart(6)} octets déployés` +
        `  ${size > limit ? '⚠ DÉPASSE la limite EIP-170' : `(${((size / limit) * 100).toFixed(1)} % de la limite EIP-170)`}`
    );
  }
}
console.log('\nartefacts écrits dans build/');
