// Banc du contrat systeme 0x…1000 (CoinbosaValidatorSet), contre une chaine REELLE.
//
//   RPC=http://127.0.0.1:8545 node scripts/test-validatorset.js
//   VALSET=0x… RPC=… node scripts/test-validatorset.js   (copie temoin, pour se tester soi-meme)
//
// LECTURE SEULE. Tout passe par eth_call : aucune transaction n'est signee, rien n'est
// envoye. Le banc peut donc etre pointe sur la chaine de production sans rien y changer.
//
// CE QUI N'ETAIT COUVERT NULLE PART AILLEURS
// -----------------------------------------
// Le seul controle existant sur ce contrat etait le franchissement d'epoch (check-epoch.js) :
// il observe que la chaine ne s'est pas arretee, il n'interroge pas le contrat. Or ce
// contrat est sur le CHEMIN DE CONSENSUS : Parlia appelle getMiningValidators() a chaque
// bloc d'epoch et init/deposit/distributeFinalityReward en transaction systeme. Un revert
// sur l'un de ces chemins ne produit pas une erreur applicative : il rend le bloc
// improduisible ou invalide, et la chaine s'arrete. C'est ce que ce banc verifie.
//
// L'ACCIDENT QUE CE FICHIER DOIT ATTRAPER, ET LA RAISON DE SA FORME
// ----------------------------------------------------------------
// « La transaction a echoue » n'est PAS une preuve que la garde attendue a joue. Un appel
// vers une fonction DISPARUE (supprimee, renommee, signature changee) revert lui aussi —
// sans message. Un banc qui se contente d'attraper une exception declarerait alors
// « rejetee comme prevu » sur un contrat qui n'a plus aucune garde : le faux vert exact
// que ce depot corrige partout ailleurs. Chaque rejet attendu ici exige donc le MOTIF
// Error(string) precis. Un revert nu (data vide) est compte comme un ECHEC.
//
// Symetriquement, un banc qui ne contient que des rejets peut etre integralement vert
// alors que la fonction n'existe plus. D'ou le TEMOIN POSITIF : une rotation a l'identique
// (le set courant, reinjecte tel quel) doit passer. S'il echoue, les rejets ci-dessus ne
// prouvent plus rien et le banc le dit.
const { ethers } = require('ethers');

const RPC = process.env.RPC || 'http://127.0.0.1:8545';
// VALSET n'existe que pour pouvoir pointer le banc sur une COPIE temoin et verifier qu'il
// sait echouer. En exploitation, on ne le pose pas : le contrat systeme est a 0x…1000.
const VALSET = process.env.VALSET || '0x0000000000000000000000000000000000001000';

const ABI = [
  'function init()',
  'function getMiningValidators() view returns (address[] vals, bytes[] votes)',
  'function getValidators() view returns (address[])',
  'function getTurnLength() view returns (uint256)',
  'function numOfValidators() view returns (uint256)',
  'function isCurrentValidator(address who) view returns (bool)',
  'function deposit(address valAddr) payable',
  'function distributeFinalityReward(address[] validatorsIn, uint256[] weights)',
  'function updateValidatorSet(address[] newVals, bytes[] newVotes)',
  'function claim()',
  'function GOVERNOR() view returns (address)',
  'function INITIAL_VALIDATOR() view returns (address)',
  'function VOTE_ADDRESS_LENGTH() view returns (uint256)',
  'function alreadyInit() view returns (bool)',
];

// Selecteur de Error(string) : c'est lui qui distingue un revert QUI DIT POURQUOI d'un
// revert nu — donc une garde qui a joue d'une fonction qui a disparu.
const SELECTEUR_ERROR_STRING = '0x08c379a0';

let pass = 0, fail = 0;

function check(nom, obtenu, attendu) {
  const ok = String(obtenu).toLowerCase() === String(attendu).toLowerCase();
  ok ? pass++ : fail++;
  console.log(`  ${ok ? '\x1b[32mOK  \x1b[0m' : '\x1b[31mECHEC\x1b[0m'} ${nom}${ok ? '' : `\n         attendu : ${attendu}\n         obtenu  : ${obtenu}`}`);
  return ok;
}

// Rejet ATTENDU, avec son motif. Trois issues, une seule est un succes :
//   - pas de revert du tout            -> ECHEC (la garde ne joue pas)
//   - revert avec un AUTRE motif       -> ECHEC (ce n'est pas cette garde qui a joue)
//   - revert nu, sans Error(string)    -> ECHEC (fonction disparue / signature changee)
async function attendreRejet(nom, promesse, motifAttendu) {
  try {
    await promesse;
    fail++;
    console.log(`  \x1b[31mECHEC\x1b[0m ${nom} — l'appel a REUSSI, la garde « ${motifAttendu} » n'a pas joue`);
    return;
  } catch (e) {
    const data = e && e.data ? String(e.data) : '0x';
    if (!data.startsWith(SELECTEUR_ERROR_STRING)) {
      fail++;
      console.log(`  \x1b[31mECHEC\x1b[0m ${nom} — revert SANS motif (data=${data.slice(0, 12)}…).`);
      console.log(`         Un revert nu n'est pas la preuve d'une garde : la fonction a-t-elle disparu ou change de signature ?`);
      return;
    }
    const motif = String(e.reason || '');
    if (!motif.includes(motifAttendu)) {
      fail++;
      console.log(`  \x1b[31mECHEC\x1b[0m ${nom} — rejete pour une AUTRE raison`);
      console.log(`         motif attendu : ${motifAttendu}\n         motif obtenu  : ${motif}`);
      return;
    }
    pass++;
    console.log(`  \x1b[32mOK  \x1b[0m ${nom} (rejete : « ${motif} »)`);
  }
}

// Chemin qui ne doit JAMAIS revert (regle d'or du contrat : un revert ici arrete la chaine).
async function attendreSucces(nom, promesse) {
  try {
    await promesse;
    pass++;
    console.log(`  \x1b[32mOK  \x1b[0m ${nom}`);
  } catch (e) {
    fail++;
    console.log(`  \x1b[31mECHEC\x1b[0m ${nom} — a REVERTE : ${e && (e.reason || e.shortMessage || e.message)}`);
  }
}

(async () => {
  const provider = new ethers.JsonRpcProvider(RPC);
  const c = new ethers.Contract(VALSET, ABI, provider);
  const reseau = await provider.getNetwork();
  console.log(`\nCoinbosaValidatorSet — banc du chemin consensus`);
  console.log(`  RPC      : ${RPC}  (chainId ${reseau.chainId}, bloc ${await provider.getBlockNumber()})`);
  console.log(`  contrat  : ${VALSET}${VALSET.toLowerCase() === '0x0000000000000000000000000000000000001000' ? '' : '   ⚠ COPIE, pas le contrat systeme'}\n`);

  // GARDE D'ENTREE. Sans code a cette adresse, eth_call renvoie « 0x » sans lever :
  // toutes les lectures decoderaient dans le vide et tous les rejets attendus seraient
  // muets. Un banc lance dans ce cas ne verifie RIEN — il doit refuser de rendre un
  // verdict, jamais sortir en 0.
  const code = await provider.getCode(VALSET);
  if (code === '0x' || code.length < 4) {
    console.error(`ERREUR : aucun code a ${VALSET} sur ce RPC. Rien a verifier — pas de verdict.`);
    process.exit(1);
  }
  console.log(`  bytecode : ${(code.length - 2) / 2} octets deployes\n`);

  const governor = await c.GOVERNOR();
  const initial = await c.INITIAL_VALIDATOR();
  const longueurVote = await c.VOTE_ADDRESS_LENGTH();
  const n = await c.numOfValidators();
  const [vals, votes] = await c.getMiningValidators();
  const listed = await c.getValidators();

  console.log(`  gouverneur           : ${governor}`);
  console.log(`  validateur de genese : ${initial}`);
  console.log(`  numOfValidators()    : ${n}`);
  console.log(`  getMiningValidators  : ${vals.length} adresse(s)\n`);

  console.log('LECTURES DU CHEMIN DE CONSENSUS');
  check('alreadyInit() est vrai apres le bloc 1', await c.alreadyInit(), true);
  check('numOfValidators() >= 1', n >= 1n, true);
  check('getMiningValidators() non vide', vals.length > 0, true);
  check('getMiningValidators contient le validateur de genese', vals.some((a) => a.toLowerCase() === initial.toLowerCase()), true);
  check('getValidators() a la meme longueur', listed.length, vals.length);
  // Sans cette egalite, le controle de longueur des cles ci-dessous serait VIDE :
  // [].every() vaut true. Un tableau de votes vide passerait donc « verifie ».
  check('autant de cles de vote que de validateurs', votes.length, vals.length);
  check('getTurnLength() vaut 1 (Bohr inactif)', await c.getTurnLength(), 1n);
  check('isCurrentValidator(INITIAL_VALIDATOR)', await c.isCurrentValidator(initial), true);
  check(`chaque cle de vote fait ${longueurVote} octets`, votes.length > 0 && votes.every((v) => BigInt(ethers.getBytes(v).length) === longueurVote), true);
  // Le gouverneur ne scelle pas : le confondre avec le validateur de genese poserait la
  // gouvernance sur la cle chaude du serveur de scellage. Les deux DOIVENT differer.
  check('gouverneur distinct du validateur de genese', governor.toLowerCase() !== initial.toLowerCase(), true);

  // L'extraData du bloc 0 est ce que le moteur de consensus lit pour savoir qui scelle.
  // Si la constante du contrat s'en ecarte, le contrat annonce a Parlia un validateur dont
  // personne ne detient la cle : la chaine s'arrete au bloc d'epoch suivant. Rien d'autre
  // ne compare ces deux valeurs.
  if (VALSET.toLowerCase() === '0x0000000000000000000000000000000000001000') {
    const bloc0 = await provider.send('eth_getBlockByNumber', ['0x0', false]);
    check("INITIAL_VALIDATOR est inscrit dans l'extraData du bloc 0",
      String(bloc0.extraData).toLowerCase().includes(initial.slice(2).toLowerCase()), true);
  }

  console.log('\nCHEMINS QUI NE DOIVENT JAMAIS REVERT (un revert ici arrete la chaine)');
  // init() est appelee en transaction systeme au bloc 1, et n'a aucun controle d'acces :
  // n'importe qui peut la rappeler. Si un `require(!alreadyInit)` y etait ajoute, la
  // transaction systeme du bloc 1 reverterait le jour ou un tiers la devance — bloc 1
  // improduisible, chaine mort-nee. Elle doit rester un no-op.
  await attendreSucces('init() rappelee est un no-op (ne revert pas)',
    provider.call({ to: VALSET, data: c.interface.encodeFunctionData('init') }));
  check('alreadyInit() reste vrai apres init() superflu', await c.alreadyInit(), true);
  check('numOfValidators() inchange apres init() superflu', await c.numOfValidators(), n);

  // deposit() est appelee a chaque bloc, distributeFinalityReward() a chaque epoch, toutes
  // deux en transaction systeme. Elles doivent encaisser les arguments degeneres sans broncher.
  await attendreSucces('deposit(0x0) ne revert pas',
    provider.call({ to: VALSET, data: c.interface.encodeFunctionData('deposit', [ethers.ZeroAddress]) }));
  await attendreSucces('distributeFinalityReward([], []) ne revert pas',
    provider.call({ to: VALSET, data: c.interface.encodeFunctionData('distributeFinalityReward', [[], []]) }));

  console.log('\nTEMOIN POSITIF (sans lui, les rejets ci-dessous ne prouvent rien)');
  // Rotation A L'IDENTIQUE : on reinjecte le set courant. Elle traverse TOUTES les gardes
  // de updateValidatorSet et doit aboutir. Si ce temoin echoue, c'est que la fonction est
  // devenue inatteignable — et les « rejets » qui suivent ne seraient que le bruit d'une
  // fonction absente, pas la preuve de gardes vivantes.
  const votesCourants = votes.map((v) => ethers.hexlify(v));
  await attendreSucces('rotation a l\'identique acceptee depuis le gouverneur (eth_call, rien n\'est envoye)',
    provider.call({ from: governor, to: VALSET, data: c.interface.encodeFunctionData('updateValidatorSet', [vals, votesCourants]) }));

  console.log('\nGARDES DE updateValidatorSet (motif exige, un revert nu ne compte pas)');
  const etranger = ethers.Wallet.createRandom().address;
  await attendreRejet('rotation par un non-gouverneur',
    provider.call({ from: etranger, to: VALSET, data: c.interface.encodeFunctionData('updateValidatorSet', [vals, votesCourants]) }),
    'only governor');

  // Retirer le validateur de genese, c'est retirer la seule adresse dont on sache qu'une
  // cle de scellage existe : plus aucun signataire au bloc d'epoch suivant, arret definitif
  // (aucune transaction corrective ne peut plus etre minee). Cette garde est le dernier
  // filet du contrat — elle ne garantit pas la liveness pour autant, voir le commentaire
  // de updateValidatorSet dans CoinbosaValidatorSet.sol.
  const autre = '0x0000000000000000000000000000000000009999';
  const voteAutre = '0x' + '11'.repeat(Number(longueurVote));
  await attendreRejet('rotation qui retire le validateur de genese',
    provider.call({ from: governor, to: VALSET, data: c.interface.encodeFunctionData('updateValidatorSet', [[autre], [voteAutre]]) }),
    'genesis validator must remain a validator');

  await attendreRejet('rotation vers un ensemble vide',
    provider.call({ from: governor, to: VALSET, data: c.interface.encodeFunctionData('updateValidatorSet', [[], []]) }),
    'bad length');

  await attendreRejet('rotation avec autant de votes que de validateurs non respecte',
    provider.call({ from: governor, to: VALSET, data: c.interface.encodeFunctionData('updateValidatorSet', [[initial], []]) }),
    'length mismatch');

  await attendreRejet("rotation contenant l'adresse nulle",
    provider.call({ from: governor, to: VALSET, data: c.interface.encodeFunctionData('updateValidatorSet', [[initial, ethers.ZeroAddress], [votesCourants[0] || voteAutre, voteAutre]]) }),
    'zero address');

  // Une cle BLS de longueur libre laisserait passer une extraData que le moteur ne sait
  // pas relire : bloc d'epoch invalide.
  await attendreRejet('rotation avec une cle de vote de mauvaise longueur',
    provider.call({ from: governor, to: VALSET, data: c.interface.encodeFunctionData('updateValidatorSet', [[initial], ['0x1234']]) }),
    'bad vote address');

  // Un doublon gonflerait N sans ajouter de scelleur : le quorum ⌊N/2⌋+1 monterait
  // au-dessus du nombre de machines reellement capables de signer.
  await attendreRejet('rotation avec un validateur en double',
    provider.call({ from: governor, to: VALSET, data: c.interface.encodeFunctionData('updateValidatorSet', [[initial, initial], [voteAutre, '0x' + '22'.repeat(Number(longueurVote))]]) }),
    'duplicate validator');

  console.log('\nAUTRES GARDES');
  await attendreRejet('claim() sans solde a reclamer',
    provider.call({ from: etranger, to: VALSET, data: c.interface.encodeFunctionData('claim') }),
    'nothing to claim');

  // Un banc qui n'a rien execute ne doit jamais sortir en 0 : le silence d'un controle est
  // indiscernable d'un succes pour qui lit la coche verte.
  if (pass + fail === 0) {
    console.error('\nERREUR : aucune verification n\'a ete executee — pas de verdict.');
    process.exit(1);
  }
  console.log(`\n${'='.repeat(58)}`);
  console.log(`  ${pass} verification(s) reussie(s), ${fail} echec(s)`);
  console.log('='.repeat(58));
  process.exit(fail ? 1 : 0);
})().catch((e) => { console.error('\nERREUR FATALE :', e.message); process.exit(1); });
