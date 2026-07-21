// Déploie le jeton BOSA (BRC20) sur Coinbosa Chain.
//
//   HOLDER=0xVotreAdresseDeDepart PRIVATE_KEY=0x... RPC=http://... node scripts/deploy-bosa.js
//
// HOLDER reçoit l'intégralité des 700 000 000 BOSA et devient propriétaire du contrat.
const fs = require('fs');
const path = require('path');
const { ethers } = require('ethers');

const RPC = process.env.RPC || 'http://127.0.0.1:8545';
const PRIVATE_KEY = process.env.PRIVATE_KEY;
const KEYSTORE_DIR = process.env.KEYSTORE || path.join(__dirname, '..', 'node1', 'keystore');
const PASSWORD_FILE = process.env.PASSWORD_FILE || path.join(__dirname, '..', 'pw.txt');

// L'adresse de départ par défaut est celle de la configuration du réseau.
const config = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'coinbosa.config.json'), 'utf8'));
const HOLDER = process.env.HOLDER || config.token.initialHolder;

if (!HOLDER || !ethers.isAddress(HOLDER)) {
  console.error('Adresse de départ manquante ou invalide.');
  console.error('Renseignez token.initialHolder dans coinbosa.config.json, ou passez HOLDER=0x…');
  process.exit(1);
}
if (ethers.getAddress(HOLDER) !== HOLDER) {
  console.error(`Somme de contrôle EIP-55 invalide pour ${HOLDER}`);
  console.error(`Attendu : ${ethers.getAddress(HOLDER.toLowerCase())}`);
  process.exit(1);
}

const artifactPath = path.join(__dirname, '..', 'build', 'BosaToken.json');
if (!fs.existsSync(artifactPath)) {
  console.error('build/BosaToken.json absent. Lancez d’abord : node scripts/compile.js');
  process.exit(1);
}
const artifact = JSON.parse(fs.readFileSync(artifactPath, 'utf8'));

(async () => {
  const provider = new ethers.JsonRpcProvider(RPC);

  // Le déployeur vient soit d'une clé privée, soit d'un keystore geth.
  let wallet;
  if (PRIVATE_KEY) {
    wallet = new ethers.Wallet(PRIVATE_KEY, provider);
  } else {
    const pwd = fs.readFileSync(PASSWORD_FILE, 'utf8').trim();
    const files = fs.readdirSync(KEYSTORE_DIR).filter((f) => f.startsWith('UTC--'));
    if (!files.length) throw new Error(`aucun keystore dans ${KEYSTORE_DIR}`);
    for (const f of files) {
      const w = (await ethers.Wallet.fromEncryptedJson(fs.readFileSync(path.join(KEYSTORE_DIR, f), 'utf8'), pwd)).connect(provider);
      if ((await provider.getBalance(w.address)) > 0n) { wallet = w; break; }
    }
    if (!wallet) throw new Error('aucun compte du keystore ne dispose de fonds pour payer le gas');
  }

  const net = await provider.getNetwork();

  console.log(`Réseau     : chainId ${net.chainId}, bloc ${await provider.getBlockNumber()}`);
  console.log(`Déployeur  : ${wallet.address}`);
  console.log(`Bénéficiaire (adresse de départ) : ${HOLDER}`);

  const balance = await provider.getBalance(wallet.address);
  if (balance === 0n) {
    console.error('Le déployeur n’a aucun fonds pour payer le gas.');
    process.exit(1);
  }

  const factory = new ethers.ContractFactory(artifact.abi, artifact.bytecode, wallet);
  const token = await factory.deploy(HOLDER);
  console.log(`\nTransaction : ${token.deploymentTransaction().hash}`);
  console.log('En attente de confirmation…');
  await token.waitForDeployment();

  const address = await token.getAddress();
  const receipt = await provider.getTransactionReceipt(token.deploymentTransaction().hash);
  const decimals = await token.decimals();
  const supply = await token.totalSupply();

  console.log(`\n  Contrat BOSA : ${address}`);
  console.log(`  Bloc         : ${receipt.blockNumber}`);
  console.log(`  Gas          : ${receipt.gasUsed}`);
  console.log(`  Nom          : ${await token.name()}`);
  console.log(`  Symbole      : ${await token.symbol()}`);
  console.log(`  Décimales    : ${decimals}`);
  console.log(`  Offre        : ${supply / 10n ** BigInt(decimals)} BOSA`);
  console.log(`  Propriétaire : ${await token.getOwner()}`);

  const out = { network: Number(net.chainId), address, holder: HOLDER, deployTx: token.deploymentTransaction().hash, block: receipt.blockNumber };
  fs.writeFileSync(path.join(__dirname, '..', 'build', 'bosa-deployment.json'), JSON.stringify(out, null, 2));
  console.log('\n  Détails écrits dans build/bosa-deployment.json');
})().catch((e) => { console.error('ERREUR :', e.message); process.exit(1); });
