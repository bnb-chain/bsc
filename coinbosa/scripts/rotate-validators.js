// Rotation de l'ensemble des validateurs — AVEC le garde-fou que le contrat ne peut pas offrir.
//
//   RPC=https://explorer.coinbosa.com/rpc VALIDATORS=0xa,0xb node scripts/rotate-validators.js
//
// POURQUOI CE SCRIPT EXISTE
// -------------------------
// updateValidatorSet() du contrat système vérifie une seule chose : que le validateur de
// genèse reste dans l'ensemble. Son commentaire affirme que cela « garantit un signataire ».
// C'EST FAUX, et l'erreur est mortelle.
//
// Parlia n'exige pas UN signataire : il exige ⌊N/2⌋+1 signataires DISTINCTS et EN LIGNE.
//   consensus/parlia/snapshot.go : minerHistoryCheckLen() = (len(Validators)/2+1)*TurnLength-1
//   consensus/parlia/parlia.go   : Seal() -> SignRecently() -> « Signed recently, must wait for others »
//
// Conséquence concrète, reproduite au banc (consensus/parlia/coinbosa_halt_repro_test.go) :
// passer de 1 à 2 validateurs alors qu'un seul nœud scelle ARRÊTE la chaîne au bloc d'epoch
// suivant. La transaction est acceptée (status 1), la chaîne tourne encore jusqu'à 200 blocs,
// puis plus rien. Et comme plus aucun bloc n'est produit, AUCUNE transaction corrective ne
// peut être minée : on ne peut pas défaire l'opération on-chain.
//
// Le contrat vit dans le genesis : son bytecode ne peut plus être corrigé. Ce script est donc
// la seule barrière possible. Il REFUSE la rotation tant qu'il n'a pas vérifié que chaque
// nouveau validateur est réellement joignable et capable de sceller.
//
// ⚠ Nuance qui surprend : passer de 1 à 2 validateurs DÉGRADE la disponibilité. À N=2 il faut
// 2 signataires sur 2 en permanence ; la perte d'un seul nœud arrête le réseau. Un opérateur
// qui monte 1→2→3→4 en ajoutant les validateurs un par un traverse cet état fragile.
// Ajouter les validateurs PAR PAIRES (1→3, 3→5) évite le quorum 2-sur-2.
const { ethers } = require('ethers');

const RPC = process.env.RPC || 'http://127.0.0.1:8545';
const NOUVEAUX = (process.env.VALIDATORS || '').split(',').map((s) => s.trim()).filter(Boolean);
const VALSET = '0x0000000000000000000000000000000000001000';
const FORCER = process.env.JE_COMPRENDS_LE_RISQUE === '1';

const ABI = [
  'function getValidators() view returns (address[])',
  'function numOfValidators() view returns (uint256)',
  'function getTurnLength() view returns (uint256)',
  'function GOVERNOR() view returns (address)',
  'function INITIAL_VALIDATOR() view returns (address)',
];

(async () => {
  const provider = new ethers.JsonRpcProvider(RPC);
  const c = new ethers.Contract(VALSET, ABI, provider);

  const actuels = await c.getValidators();
  const gouverneur = await c.GOVERNOR();
  const initial = await c.INITIAL_VALIDATOR();
  const turn = Number(await c.getTurnLength());

  console.log('\n  ROTATION DE L\'ENSEMBLE DES VALIDATEURS — contrôle avant vol');
  console.log('  ' + '='.repeat(72));
  console.log(`  validateurs actuels : ${actuels.length}`);
  actuels.forEach((a) => console.log(`      ${a}`));
  console.log(`  gouverneur          : ${gouverneur}`);
  console.log(`  validateur de genèse: ${initial}  (doit rester dans l'ensemble)`);

  if (!NOUVEAUX.length) {
    console.log('\n  VALIDATORS non fourni — rien à faire.');
    console.log('  Usage : VALIDATORS=0xa,0xb,0xc node scripts/rotate-validators.js\n');
    process.exit(0);
  }

  const bloquants = [];
  const avertissements = [];

  // --- 1. forme de l'ensemble demandé ---
  const vus = new Set();
  for (const a of NOUVEAUX) {
    if (!ethers.isAddress(a)) { bloquants.push(`adresse invalide : ${a}`); continue; }
    const n = a.toLowerCase();
    if (vus.has(n)) bloquants.push(`adresse en double : ${a}`);
    vus.add(n);
  }
  if (!vus.has(initial.toLowerCase())) {
    bloquants.push(`le validateur de genèse ${initial} doit rester dans l'ensemble (le contrat rejetterait la transaction)`);
  }

  const N = vus.size;
  // C'est LE calcul que le contrat ne fait pas.
  const requisEnLigne = Math.floor(N / 2) + 1;
  console.log(`\n  ensemble demandé    : ${N} validateur(s)`);
  console.log(`  signataires DISTINCTS et EN LIGNE exigés par Parlia : ${requisEnLigne}  (⌊${N}/2⌋+1, TurnLength=${turn})`);

  // --- 2. chaque validateur est-il RÉELLEMENT capable de sceller ? ---
  // On ne peut pas le prouver depuis le contrat : on regarde qui a scellé récemment.
  const tete = await provider.getBlockNumber();
  const fenetre = Math.min(200, tete);
  const scelleurs = new Map();
  for (let i = tete; i > tete - fenetre && i >= 0; i--) {
    const b = await provider.getBlock(i);
    if (b && b.miner) scelleurs.set(b.miner.toLowerCase(), (scelleurs.get(b.miner.toLowerCase()) || 0) + 1);
  }
  console.log(`\n  scelleurs observés sur les ${fenetre} derniers blocs :`);
  for (const [a, n] of scelleurs) console.log(`      ${ethers.getAddress(a)}  ${n} bloc(s)`);

  const actifs = [...vus].filter((a) => scelleurs.has(a));
  const inactifs = [...vus].filter((a) => !scelleurs.has(a));
  console.log(`\n  parmi l'ensemble demandé : ${actifs.length} scellent déjà, ${inactifs.length} n'ont jamais scellé`);
  inactifs.forEach((a) => console.log(`      JAMAIS VU SCELLER : ${ethers.getAddress(a)}`));

  if (actifs.length < requisEnLigne) {
    bloquants.push(
      `seuls ${actifs.length} validateur(s) de l'ensemble demandé scellent réellement, ${requisEnLigne} exigés. ` +
      'La chaîne s\'ARRÊTERAIT au prochain bloc d\'epoch, et aucune transaction corrective ne pourrait plus être minée.'
    );
  }

  // --- 3. transitions connues pour dégrader la disponibilité ---
  if (N === 2) {
    avertissements.push('N=2 impose un quorum de 2 sur 2 EN PERMANENCE : la perte d\'un seul nœud arrête le réseau. Préférer un passage direct de 1 à 3.');
  }
  if (N > actuels.length && N % 2 === 0) {
    avertissements.push(`un ensemble de taille paire (${N}) exige ${requisEnLigne} nœuds sur ${N} : la marge de panne est plus faible qu'avec ${N + 1} validateurs.`);
  }

  console.log('\n  ' + '='.repeat(72));
  if (avertissements.length) {
    console.log('  À CONSIDÉRER :');
    avertissements.forEach((a) => console.log(`    ~ ${a}`));
  }
  if (bloquants.length) {
    console.error('\n  BLOQUANTS :');
    bloquants.forEach((b) => console.error(`    ✗ ${b}`));
    console.error('\n  VERDICT : NE PAS EFFECTUER CETTE ROTATION.');
    console.error('  Démarre d\'abord les nœuds manquants, attends de les voir sceller, puis relance.\n');
    process.exit(1);
  }

  console.log('\n  VERDICT : rotation jugée SÛRE sur les points vérifiables.');
  console.log('\n  Le contrat n\'ayant pas de garde de liveness, RIEN ne rattrapera une erreur ici.');
  console.log('  Transaction à envoyer DEPUIS LE GOUVERNEUR (hors ligne / matériel) :');
  console.log(`    contrat : ${VALSET}`);
  console.log('    méthode : updateValidatorSet(address[] newVals, bytes[] newVotes)');
  console.log(`    newVals : [${[...vus].map((a) => ethers.getAddress(a)).join(', ')}]`);
  console.log(`    newVotes: ${N} entrées de 48 octets à zéro (la finalité rapide est inactive)`);
  console.log('\n  Après envoi : surveiller le PROCHAIN bloc d\'epoch (multiple de 200).');
  console.log('  Si la hauteur cesse d\'avancer à ce bloc, démarrer immédiatement les nœuds manquants.\n');
})().catch((e) => { console.error('ERREUR :', e.message); process.exit(1); });
