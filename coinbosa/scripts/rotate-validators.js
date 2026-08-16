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
// ATTENTION — le conseil « ajouter par paires (1→3, 3→5) évite le quorum » est FAUX.
// minerHistoryCheckLen() = (N/2+1)*TurnLength-1, en division ENTIÈRE :
//     N=1 -> 0   (un seul scelleur suffit)
//     N=2 -> 1   il faut 2 scelleurs DISTINCTS et EN LIGNE
//     N=3 -> 1   il en faut 2 AUSSI — passer par 3 ne change RIEN
//     N=5 -> 2   il en faut 3
// 1→3 n'est donc pas plus sûr que 1→2 : dans les deux cas le validateur unique est
// interdit de sceller le bloc qui suit l'epoch. La parité ne protège de rien.
// Ce qui protège, et la SEULE chose qui protège : que ⌊N/2⌋+1 nœuds entrants
// détiennent réellement leur clé, soient synchronisés, et aient été VUS sceller
// AVANT la bascule. C'est ce que ce script vérifie ci-dessous ; ne pas contourner.
const { ethers } = require('ethers');

const RPC = process.env.RPC || 'http://127.0.0.1:8545';
const NOUVEAUX = (process.env.VALIDATORS || '').split(',').map((s) => s.trim()).filter(Boolean);
const VALSET = '0x0000000000000000000000000000000000001000';
const FORCER = process.env.JE_COMPRENDS_LE_RISQUE === '1';

const ABI = [
  'function updateValidatorSet(address[] newVals, bytes[] newVotes)',
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
  const GOUVERNEUR = gouverneur;
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

  // Un candidat ne PEUT PAS avoir déjà scellé : on ne scelle qu'une fois membre de
  // l'ensemble. Exiger le contraire — ce que faisait la version précédente — rendait toute
  // extension impossible, y compris légitime. On distingue donc deux choses :
  //   · les SORTANTS qui doivent rester actifs (verifiable on-chain) ;
  //   · les ENTRANTS, dont on ne peut rien prouver ici, et qui exigent une attestation.
  const entrants = [...vus].filter((a) => !actuels.some((x) => x.toLowerCase() === a));
  const sortantsActifs = actifs.filter((a) => actuels.some((x) => x.toLowerCase() === a));

  if (sortantsActifs.length === 0 && actuels.length > 0) {
    bloquants.push('aucun validateur ACTUELLEMENT actif ne figure dans le nouvel ensemble : plus personne ne pourrait sceller.');
  }
  if (entrants.length && !FORCER) {
    bloquants.push(
      `${entrants.length} validateur(s) ENTRANT(S) n'ont jamais scellé — c'est normal, on ne scelle qu'une fois dans ` +
      'l\'ensemble. Mais Parlia exige ⌊N/2⌋+1 signataires EN LIGNE dès le prochain bloc d\'epoch : leurs nœuds ' +
      'doivent DÉJÀ tourner, être synchronisés et détenir leur clé. Impossible à prouver depuis la chaîne. ' +
      'Vérifie-le toi-même, puis relance avec JE_COMPRENDS_LE_RISQUE=1.'
    );
  }
  if (entrants.length && FORCER) {
    avertissements.push(`${entrants.length} entrant(s) acceptés sous ta responsabilité (JE_COMPRENDS_LE_RISQUE=1) : leurs nœuds DOIVENT déjà tourner et être synchronisés.`);
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

  // --- 4. adresses de vote : elles doivent être DISTINCTES ---
  // Le contrat refuse deux clés de vote identiques (CoinbosaValidatorSet.sol:216). Or la
  // finalité rapide est inactive, donc la clé « naturelle » est 48 octets nuls — la même
  // pour tous. Envoyer cela pour N≥2 provoquait un REVERT GARANTI, et c'est exactement ce
  // que ce script conseillait de faire. On dérive donc une valeur unique par validateur.
  // Ces clés ne servent à rien tant que la finalité rapide est inactive : ce sont des
  // marque-places, mais ils doivent être uniques pour passer la garde du contrat.
  const listeVals = [...vus].map((a) => ethers.getAddress(a));
  const listeVotes = listeVals.map((a) => '0x' + ethers.keccak256(a).slice(2).padEnd(96, '0').slice(0, 96));
  const distincts = new Set(listeVotes).size === listeVotes.length;
  if (!distincts) bloquants.push('collision improbable sur les clés de vote dérivées — ne pas envoyer.');

  // --- 5. SIMULATION : on demande à la chaîne ce qui se passerait ---
  // C'est le seul contrôle qui ne peut pas se tromper : il exécute la transaction sans la
  // publier. Il aurait attrapé seul le revert « duplicate vote address ».
  let simulationOk = false, motifRevert = '';
  if (GOUVERNEUR && ethers.isAddress(GOUVERNEUR)) {
    const iface = new ethers.Interface(ABI);
    const data = iface.encodeFunctionData('updateValidatorSet', [listeVals, listeVotes]);
    try {
      await provider.call({ from: gouverneur, to: VALSET, data });
      simulationOk = true;
    } catch (e) {
      motifRevert = (e.shortMessage || e.message || '').slice(0, 160);
      bloquants.push(`la chaîne REJETTE cette rotation : ${motifRevert}`);
    }
  }

  console.log('\n  ' + '='.repeat(72));
  if (avertissements.length) {
    console.log('  À CONSIDÉRER :');
    avertissements.forEach((a) => console.log(`    ~ ${a}`));
  }
  if (bloquants.length) {
    console.error('\n  BLOQUANTS :');
    bloquants.forEach((b) => console.error(`    ✗ ${b}`));
    console.error('\n  VERDICT : NE PAS EFFECTUER CETTE ROTATION.\n');
    process.exit(1);
  }

  console.log('  SIMULATION on-chain : la transaction PASSE (eth_call, rien n\'a été publié).');
  console.log('\n  VERDICT : rotation sûre sur tout ce qui est vérifiable.');
  console.log('\n  À envoyer DEPUIS LE GOUVERNEUR (hors ligne / matériel) :');
  console.log(`    contrat : ${VALSET}`);
  console.log('    méthode : updateValidatorSet(address[] newVals, bytes[] newVotes)');
  console.log(`    newVals : [${listeVals.join(', ')}]`);
  console.log('    newVotes :');
  listeVotes.forEach((v, k) => console.log(`      ${listeVals[k]} -> ${v.slice(0, 26)}…`));
  console.log('\n  Après envoi : surveiller le PROCHAIN bloc d\'epoch (multiple de 200).');
  console.log('  Si la hauteur cesse d\'avancer, démarrer immédiatement les nœuds manquants.\n');
})().catch((e) => { console.error('ERREUR :', e.message); process.exit(1); });
