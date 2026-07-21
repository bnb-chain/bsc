// Déploie le jeton BOSA (BRC20) sur Coinbosa Chain.
//
//   HOLDER=0xVotreAdresseDeDepart PRIVATE_KEY=0x... RPC=http://... node scripts/deploy-bosa.js
//
// HOLDER reçoit l'intégralité des 700 000 000 BOSA et devient propriétaire du contrat.
const fs = require('fs');
const path = require('path');
const { ethers } = require('ethers');

const RPC = process.env.RPC || 'http://127.0.0.1:8545';
const HOLDER = process.env.HOLDER;
const PRIVATE_KEY = process.env.PRIVATE_KEY;

if (!HOLDER || !ethers.isAddress(HOLDER)) {
  console.error('HOLDER manquant ou invalide. Indiquez l’adresse de départ qui recevra les 700 M BOSA :');
  console.error('  HOLDER=0x… PRIVATE_KEY=0x… node scripts/deploy-bosa.js');
  process.exit(1);
}
if (!PRIVATE_KEY) {
  console.error('PRIVATE_KEY manquante : clé privée du compte qui paie le déploiement.');
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
  const wallet = new ethers.Wallet(PRIVATE_KEY, provider);
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
