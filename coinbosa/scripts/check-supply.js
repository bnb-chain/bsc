// Vérifie que l'offre native inscrite au genesis vaut exactement l'offre attendue.
//
// Les soldes sont lus AU BLOC 0 (genesis) : cela reflète l'allocation initiale sans
// être faussé par les frais brûlés/déposés après coup (EIP-1559), et compare le
// genesis DÉPLOYÉ (on-chain) au fichier local, adresse par adresse.
//
// Garanti : aucun solde hérité (le pont du réseau amont, notamment) ne subsiste,
// la répartition boucle sur le total, les contrats inter-chaînes sont sans code.
// Limite assumée : ce contrôle itère les adresses du FICHIER local. Une adresse CACHÉE,
// présente dans le genesis déployé mais absente du fichier, lui échapperait — aucune API
// JSON-RPC standard ne permet d'énumérer tous les comptes d'un état.
// C'est scripts/check-genesis-hash.js qui couvre ce cas : il compare le hash et le
// stateRoot du bloc 0 à une empreinte figée, et le stateRoot engage TOUT l'état initial.
// Les deux contrôles sont complémentaires — lancer les deux.
const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

const RPC = process.env.RPC || 'http://127.0.0.1:8545';
// Le fichier genesis à vérifier est paramétrable : la production vise genesis-coinbosa.json,
// la vérification mécanique (CI/local) vise genesis-coinbosa-dev.json via la variable GENESIS.
const GENESIS_FILE = process.env.GENESIS || path.join(__dirname, '..', 'genesis', 'genesis-coinbosa.json');
const config = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'coinbosa.config.json'), 'utf8'));
const genesis = JSON.parse(fs.readFileSync(GENESIS_FILE, 'utf8'));

// Refus dur PAR DÉFAUT : un genesis de développement (adresses synthétiques, validateur
// crédité) ne doit jamais passer pour un genesis de production. La seule dérogation est
// une vérification mécanique EXPLICITE (ALLOW_DEV_SUPPLY=1) : elle prouve que la tuyauterie
// offre/pont fonctionne, sans jamais valider un genesis de dev comme production.
const ALLOW_DEV_SUPPLY = process.env.ALLOW_DEV_SUPPLY === '1';
if (genesis.coinbosaDev && !ALLOW_DEV_SUPPLY) {
  console.error(`ECHEC : ${path.basename(GENESIS_FILE)} porte le marqueur coinbosaDev — genesis de DÉVELOPPEMENT, non déployable en production.`);
  process.exit(1);
}
if (genesis.coinbosaDev) {
  console.warn("⚠  MODE DÉVELOPPEMENT (ALLOW_DEV_SUPPLY=1) : contrôle mécanique sur un genesis de DÉV — NON valable comme preuve de production.");
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
