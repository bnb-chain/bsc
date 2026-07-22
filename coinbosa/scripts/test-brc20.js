// Suite de tests BRC20 exécutée contre une vraie chaîne Coinbosa.
const fs = require('fs');
const path = require('path');
const { ethers } = require('ethers');

// Le déployeur est fourni soit par sa clé privée (PRIVATE_KEY), soit par un
// keystore geth (KEYSTORE + PASSWORD_FILE). Les deux comptes de test sont
// créés et financés à la volée : la suite ne dépend d'aucune allocation du genesis.
const RPC = process.env.RPC || 'http://127.0.0.1:8545';
const KEYSTORE_DIR = process.env.KEYSTORE || path.join(__dirname, '..', 'node1', 'keystore');
const PASSWORD_FILE = process.env.PASSWORD_FILE || path.join(__dirname, '..', 'pw.txt');
const ARTIFACT = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'build', 'ExampleToken.json'), 'utf8'));

let pass = 0, fail = 0;
const results = [];

function check(name, actual, expected) {
  const ok = String(actual) === String(expected);
  ok ? pass++ : fail++;
  results.push({ name, ok, actual: String(actual), expected: String(expected) });
  console.log(`  ${ok ? '\x1b[32mOK  \x1b[0m' : '\x1b[31mECHEC\x1b[0m'} ${name}${ok ? '' : `\n         attendu : ${expected}\n         obtenu  : ${actual}`}`);
}

async function expectRevert(name, promise) {
  try {
    const tx = await promise;
    await tx.wait();
    fail++; results.push({ name, ok: false });
    console.log(`  \x1b[31mECHEC\x1b[0m ${name} — la transaction aurait dû échouer`);
  } catch (e) {
    pass++; results.push({ name, ok: true });
    console.log(`  \x1b[32mOK  \x1b[0m ${name} (rejetée comme prévu)`);
  }
}

(async () => {
  const provider = new ethers.JsonRpcProvider(RPC);
  const net = await provider.getNetwork();
  console.log(`\nCoinbosa Chain — chainId ${net.chainId}, bloc ${await provider.getBlockNumber()}\n`);

  // --- déployeur ---
  let deployer;
  if (process.env.PRIVATE_KEY) {
    deployer = new ethers.Wallet(process.env.PRIVATE_KEY, provider);
  } else {
    const pwd = fs.readFileSync(PASSWORD_FILE, 'utf8').trim();
    const files = fs.readdirSync(KEYSTORE_DIR).filter((f) => f.startsWith('UTC--'));
    if (!files.length) throw new Error(`aucun keystore dans ${KEYSTORE_DIR}`);
    // on retient le premier compte approvisionné
    for (const f of files) {
      const w = (await ethers.Wallet.fromEncryptedJson(fs.readFileSync(path.join(KEYSTORE_DIR, f), 'utf8'), pwd)).connect(provider);
      if ((await provider.getBalance(w.address)) > 0n) { deployer = w; break; }
    }
    if (!deployer) throw new Error('aucun compte du keystore ne dispose de fonds');
  }

  // --- comptes de test, créés et financés par le déployeur ---
  const alice = ethers.Wallet.createRandom().connect(provider);
  const bob = ethers.Wallet.createRandom().connect(provider);
  console.log(`  déployeur : ${deployer.address}`);
  console.log(`  alice     : ${alice.address}`);
  console.log(`  bob       : ${bob.address}`);
  for (const w of [alice, bob]) {
    await (await deployer.sendTransaction({ to: w.address, value: ethers.parseEther('1') })).wait();
  }
  console.log('  (alice et bob financés pour le gas)\n');

  // --- Déploiement ---
  console.log('DÉPLOIEMENT');
  const factory = new ethers.ContractFactory(ARTIFACT.abi, ARTIFACT.bytecode, deployer);
  const token = await factory.deploy(deployer.address);
  await token.waitForDeployment();
  const addr = await token.getAddress();
  const dtx = await provider.getTransactionReceipt(token.deploymentTransaction().hash);
  console.log(`  adresse : ${addr}`);
  console.log(`  bloc    : ${dtx.blockNumber}, gas ${dtx.gasUsed}\n`);

  const DEC = 18n;
  const UNIT = 10n ** DEC;
  const SUPPLY = 1_000_000n * UNIT;

  // --- Métadonnées ---
  console.log('MÉTADONNÉES (jeton d’exemple du standard BRC20)');
  check('name() vaut "Example Token"', await token.name(), 'Example Token');
  check('symbol() vaut "EXMPL"', await token.symbol(), 'EXMPL');
  check('decimals() vaut 18', await token.decimals(), 18);
  check('totalSupply() vaut 1 000 000', await token.totalSupply(), SUPPLY);
  check('offre lisible = 1000000', (await token.totalSupply()) / UNIT, 1000000n);
  check('getOwner() est le propriétaire', await token.getOwner(), deployer.address);
  check('le propriétaire détient tout', await token.balanceOf(deployer.address), SUPPLY);

  // --- Transferts ---
  console.log('\nTRANSFERTS');
  await (await token.transfer(alice.address, 1000n * UNIT)).wait();
  check('transfer crédite le destinataire', await token.balanceOf(alice.address), 1000n * UNIT);
  check('transfer débite l’émetteur', await token.balanceOf(deployer.address), SUPPLY - 1000n * UNIT);
  await expectRevert('transfer au-delà du solde rejeté', token.connect(alice).transfer(bob.address, 99999n * UNIT));
  await expectRevert('transfer vers l’adresse nulle rejeté', token.transfer(ethers.ZeroAddress, 1n));

  // --- Autorisations ---
  console.log('\nAUTORISATIONS');
  await (await token.approve(alice.address, 500n * UNIT)).wait();
  check('approve enregistre l’autorisation', await token.allowance(deployer.address, alice.address), 500n * UNIT);
  await (await token.connect(alice).transferFrom(deployer.address, bob.address, 200n * UNIT)).wait();
  check('transferFrom transfère bien', await token.balanceOf(bob.address), 200n * UNIT);
  check('transferFrom décrémente l’autorisation', await token.allowance(deployer.address, alice.address), 300n * UNIT);
  await expectRevert('transferFrom au-delà de l’autorisation rejeté', token.connect(alice).transferFrom(deployer.address, bob.address, 999n * UNIT));

  await (await token.increaseAllowance(alice.address, 100n * UNIT)).wait();
  check('increaseAllowance augmente', await token.allowance(deployer.address, alice.address), 400n * UNIT);
  await (await token.decreaseAllowance(alice.address, 150n * UNIT)).wait();
  check('decreaseAllowance réduit', await token.allowance(deployer.address, alice.address), 250n * UNIT);

  const MAX = ethers.MaxUint256;
  await (await token.approve(alice.address, MAX)).wait();
  await (await token.connect(alice).transferFrom(deployer.address, bob.address, 10n * UNIT)).wait();
  check('autorisation infinie non décrémentée', await token.allowance(deployer.address, alice.address), MAX);

  // --- Émission et destruction ---
  console.log('\nÉMISSION ET DESTRUCTION');
  const before = await token.totalSupply();
  await (await token.mint(alice.address, 50n * UNIT)).wait();
  check('mint augmente l’offre', await token.totalSupply(), before + 50n * UNIT);
  await expectRevert('mint par un non-propriétaire rejeté', token.connect(alice).mint(alice.address, 1n));
  const beforeBurn = await token.totalSupply();
  await (await token.connect(alice).burn(25n * UNIT)).wait();
  check('burn réduit l’offre', await token.totalSupply(), beforeBurn - 25n * UNIT);

  // --- Propriété ---
  console.log('\nPROPRIÉTÉ');
  await (await token.transferOwnership(alice.address)).wait();
  check('transferOwnership change le propriétaire', await token.getOwner(), alice.address);
  await expectRevert('l’ancien propriétaire ne peut plus émettre', token.mint(deployer.address, 1n));
  await (await token.connect(alice).transferOwnership(deployer.address)).wait();
  check('propriété rendue au déployeur', await token.getOwner(), deployer.address);

  // --- Événements ---
  console.log('\nÉVÉNEMENTS');
  const tx = await token.transfer(alice.address, 7n * UNIT);
  const rc = await tx.wait();
  const parsed = rc.logs.map((l) => token.interface.parseLog(l)).filter(Boolean);
  check('un événement Transfer est émis', parsed.filter((p) => p.name === 'Transfer').length, 1);
  check('le montant de l’événement est correct', parsed[0].args[2], 7n * UNIT);

  // --- Bilan ---
  console.log(`\n${'='.repeat(52)}`);
  console.log(`  ${pass} tests réussis, ${fail} échec(s)`);
  console.log('='.repeat(52));
  fs.writeFileSync(path.join(__dirname, '..', 'build', 'test-results.json'), JSON.stringify({ address: addr, pass, fail, results }, null, 2));
  process.exit(fail ? 1 : 0);
})().catch((e) => { console.error('\nERREUR FATALE :', e.message); process.exit(1); });
