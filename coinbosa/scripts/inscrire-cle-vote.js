// =============================================================================
// COINBOSA — étape 6 : inscrire la clé de vote BLS on-chain.
//
//   cd coinbosa
//   RPC=https://explorer.coinbosa.com/rpc \
//   CLE_VOTE=<96 caracteres hex, sans 0x> node scripts/inscrire-cle-vote.js --simuler
//
//   ... puis, pour envoyer réellement, la même commande SANS --simuler.
//
//   --calldata   sort la transaction a signer et s'arrete. Aucun secret demande.
//
// TROIS FAÇONS DE SIGNER, ET LA PREMIÈRE EST LA BONNE
// ----------------------------------------------------
// Le gouverneur est dérivé du même xpub que la trésorerie
// (`scripts/derive-treasury-addresses.js`, chemin de compte m/44'/60'/0').
// Autrement dit : **sa clé privée vit sur un portefeuille matériel et n'en sort
// jamais.** Il n'existe donc, normalement, AUCUNE clé privée brute à saisir ici.
//
//   1. `node scripts/signer-navigateur.js` — le chemin prévu. Ouvre une page
//      locale qui fait signer MetaMask, et donc le Ledger derrière lui. La clé
//      ne quitte pas l'appareil.
//   2. `--calldata` — sort `to` et `data`, à coller dans n'importe quel outil
//      de signature hors ligne.
//   3. La saisie ci-dessous — utile UNIQUEMENT si tu détiens vraiment une clé
//      privée brute pour cette adresse. Elle est lue à l'écran, sans écho, et ne
//      passe NI par la ligne de commande (elle irait dans l'historique du shell
//      et dans la table des processus), NI par une variable d'environnement, NI
//      par un fichier.
//
// POURQUOI CE SCRIPT PLUTÔT QUE rotate-validators.js
// ---------------------------------------------------
// rotate-validators.js sert à CHANGER l'ensemble des validateurs, et il dérive
// des clés de vote MARQUE-PLACE — keccak(adresse) tronqué. Utilisé ici, il
// écraserait la vraie clé BLS par une valeur inventée. Ce sont deux opérations
// différentes ; elles méritent deux outils.
//
// LE GARDE-FOU CENTRAL, ET IL EST ABSOLU
// ---------------------------------------
// updateValidatorSet() est la MÊME fonction qui sert à ajouter des validateurs.
// Le dépôt reproduit au banc qu'un passage de 1 à N validateurs, alors qu'un
// seul nœud scelle, ARRÊTE la chaîne au bloc d'epoch suivant — et sans retour :
// plus aucun bloc n'étant produit, aucune transaction corrective ne peut être
// minée.
//
// Ce script REFUSE donc toute modification de `newVals`. Il relit l'ensemble
// courant sur la chaîne et le réémet À L'IDENTIQUE. Seuls les 48 octets de la
// clé de vote changent. Avec cette contrainte, l'opération ne PEUT PAS arrêter
// la chaîne — c'est ce qui la rend sûre, pas la prudence de l'opérateur.
//
// Se tromper sur la clé, à l'inverse, est bénin : le contrat vérifie la longueur
// mais pas la validité cryptographique. Une clé fausse laisse le nœud silencieux,
// exactement comme aujourd'hui, et se corrige par un second appel.
// =============================================================================

const { ethers } = require('ethers');
const readline = require('readline');

const RPC = process.env.RPC || 'https://explorer.coinbosa.com/rpc';
const VALSET = '0x0000000000000000000000000000000000001000';
const SIMULER = process.argv.includes('--simuler');
const CALLDATA_SEULE = process.argv.includes('--calldata');
const CLE_VOTE = (process.env.CLE_VOTE || '').trim().replace(/^0x/i, '');

const ABI = [
  'function updateValidatorSet(address[] newVals, bytes[] newVotes)',
  'function getValidators() view returns (address[])',
  'function GOVERNOR() view returns (address)',
  'function INITIAL_VALIDATOR() view returns (address)',
];

/// Lit un secret sans l'afficher et sans le laisser dans un historique.
function lireSecret(invite) {
  return new Promise((resolve, rejeter) => {
    if (!process.stdin.isTTY) {
      // Refus délibéré : accepter une entrée redirigée, c'est accepter qu'elle
      // vienne d'un fichier ou d'un `echo` — donc de l'historique du shell.
      return rejeter(new Error('entrée non interactive : lance ce script depuis un vrai terminal'));
    }
    const rl = readline.createInterface({ input: process.stdin, output: process.stdout, terminal: true });
    let vu = false;
    rl._writeToOutput = (s) => { if (!vu) { rl.output.write(invite); vu = true; } };
    rl.question(invite, (rep) => { rl.close(); process.stdout.write('\n'); resolve(rep.trim()); });
  });
}

(async () => {
  // --- 1. la clé publique BLS ------------------------------------------------
  if (!/^[0-9a-fA-F]{96}$/.test(CLE_VOTE)) {
    console.error('ECHEC : CLE_VOTE doit faire exactement 96 caracteres hexadecimaux (48 octets), sans 0x.');
    console.error(`        recu : ${CLE_VOTE.length} caractere(s).`);
    console.error('        Elle se releve dans bls/keystore/keystore-*.json, champ "pubkey".');
    process.exit(1);
  }
  if (/^0+$/.test(CLE_VOTE)) {
    console.error('ECHEC : cle de vote entierement nulle — c est exactement l etat actuel, cela n inscrirait rien.');
    process.exit(1);
  }

  const provider = new ethers.JsonRpcProvider(RPC);
  const c = new ethers.Contract(VALSET, ABI, provider);
  const reseau = await provider.getNetwork();

  const actuels = await c.getValidators();
  const gouverneur = await c.GOVERNOR();
  const initial = await c.INITIAL_VALIDATOR();

  console.log('\n  INSCRIPTION DE LA CLE DE VOTE BLS');
  console.log('  ' + '='.repeat(72));
  console.log(`  RPC                 : ${RPC}`);
  console.log(`  chain ID            : ${reseau.chainId}`);
  console.log(`  validateurs actuels : ${actuels.length}`);
  actuels.forEach((a) => console.log(`      ${a}`));
  console.log(`  gouverneur          : ${gouverneur}`);
  console.log(`  validateur de genese: ${initial}`);
  console.log(`  cle de vote a poser : ${CLE_VOTE.slice(0, 16)}…${CLE_VOTE.slice(-8)}  (48 octets)`);

  // --- 2. le garde-fou : l'ensemble ne bouge pas ----------------------------
  if (actuels.length !== 1) {
    // Le script sait poser UNE cle sur UN validateur. A plusieurs, il faudrait
    // relire la cle de vote de chacun pour ne pas l'ecraser — ce qu'il ne fait pas.
    console.error(`\n  REFUS : ${actuels.length} validateurs sur la chaine.`);
    console.error('  Ce script ne sait poser une cle que sur un ensemble d UN validateur ;');
    console.error('  au-dela il faudrait relire la cle de vote de chacun pour ne pas l ecraser.');
    process.exit(1);
  }
  const listeVals = [ethers.getAddress(actuels[0])];
  if (listeVals[0].toLowerCase() !== initial.toLowerCase()) {
    console.error('\n  REFUS : le validateur present n est pas celui de la genese — situation non prevue.');
    process.exit(1);
  }
  const listeVotes = ['0x' + CLE_VOTE.toLowerCase()];
  console.log('\n  newVals REEMIS A L IDENTIQUE : l ensemble ne change pas, la chaine ne peut pas s arreter.');

  // --- 3. la calldata, et sa forme ------------------------------------------
  const iface = new ethers.Interface(ABI);
  const data = iface.encodeFunctionData('updateValidatorSet', [listeVals, listeVotes]);
  const octets = (data.length - 2) / 2;
  console.log(`  calldata            : ${octets} octets`);
  if (octets !== 292) {
    console.error(`\n  REFUS : calldata de ${octets} octets au lieu de 292 attendus. NE RIEN SIGNER.`);
    process.exit(1);
  }
  // La clé doit se retrouver TELLE QUELLE dans la calldata : c'est le dernier
  // controle avant signature, et il attrape une erreur d'encodage silencieuse.
  if (!data.toLowerCase().includes(CLE_VOTE.toLowerCase())) {
    console.error('\n  REFUS : la cle ne se retrouve pas dans la calldata encodee. NE RIEN SIGNER.');
    process.exit(1);
  }
  console.log('  la cle est bien presente dans la calldata encodee.');

  // --- 4. simulation : la chaine execute la transaction sans la publier ------
  let simulationOk = false;
  try {
    await provider.call({ from: gouverneur, to: VALSET, data });
    simulationOk = true;
    console.log('\n  SIMULATION : la transaction PASSE (eth_call, rien n a ete publie).');
  } catch (e) {
    console.error(`\n  REFUS : la chaine REJETTE cet appel : ${(e.shortMessage || e.message || '').slice(0, 200)}`);
    process.exit(1);
  }
  // Contre-epreuve : la meme depuis une autre adresse DOIT etre refusee. Si elle
  // passe, la garde « only governor » ne fonctionne pas et il faut s arreter.
  try {
    await provider.call({ from: listeVals[0], to: VALSET, data });
    console.error('\n  REFUS : l appel passe AUSSI depuis le validateur — la garde « only governor »');
    console.error('  ne protege rien. C est une anomalie du contrat, ne rien envoyer.');
    process.exit(1);
  } catch (e) {
    const m = (e.shortMessage || e.message || '');
    if (!/only governor/i.test(m)) {
      console.error(`\n  REFUS : refus attendu « only governor », obtenu : ${m.slice(0, 160)}`);
      process.exit(1);
    }
    console.log('  CONTRE-EPREUVE : depuis une autre adresse, la chaine repond « only governor ».');
  }

  const gaz = await provider.estimateGas({ from: gouverneur, to: VALSET, data });
  const prix = (await provider.getFeeData()).gasPrice;
  const solde = await provider.getBalance(gouverneur);
  const cout = gaz * prix;
  console.log(`\n  gaz estime          : ${gaz}`);
  console.log(`  prix du gaz         : ${ethers.formatUnits(prix, 'gwei')} gwei`);
  console.log(`  cout                : ${ethers.formatEther(cout)} BOSA`);
  console.log(`  solde du gouverneur : ${ethers.formatEther(solde)} BOSA`);
  if (solde < cout) { console.error('\n  REFUS : solde insuffisant.'); process.exit(1); }

  if (CALLDATA_SEULE) {
    console.log('\n  ' + '='.repeat(72));
    console.log('  TRANSACTION A SIGNER — a coller dans ton outil de signature');
    console.log(`    reseau   : Coinbosa Chain, chainId ${reseau.chainId}`);
    console.log(`    depuis   : ${gouverneur}`);
    console.log(`    vers     : ${VALSET}`);
    console.log('    valeur   : 0');
    console.log(`    gaz      : ${gaz}   (prevois ${(gaz * 12n) / 10n})`);
    console.log(`    prix gaz : ${ethers.formatUnits(prix, 'gwei')} gwei`);
    console.log('    data     :');
    console.log(`      ${data}`);
    console.log('\n  Aucun secret n a ete demande. Rien n a ete envoye.\n');
    return;
  }

  if (SIMULER) {
    console.log('\n  --simuler : RIEN N A ETE ENVOYE. Aucune cle privee n a ete demandee.');
    console.log('  Pour envoyer reellement : node scripts/signer-navigateur.js (portefeuille materiel),');
    console.log('  ou --calldata pour signer ailleurs, ou cette meme commande sans --simuler si et');
    console.log('  seulement si tu detiens une cle privee brute.\n');
    return;
  }

  // --- 5. signature ----------------------------------------------------------
  console.log('\n  ' + '='.repeat(72));
  console.log('  ENVOI REEL. La cle privee du gouverneur va etre demandee.');
  console.log('  Elle ne s affichera pas, et n est ecrite nulle part.');
  console.log('  Ctrl-C maintenant si tu ne veux pas envoyer.\n');

  let pk = await lireSecret('  Cle privee du gouverneur (0x… ou 64 hex) : ');
  let portefeuille;
  try {
    portefeuille = new ethers.Wallet(pk.startsWith('0x') ? pk : '0x' + pk, provider);
  } catch {
    // On DIAGNOSTIQUE sans jamais rien afficher de ce qui a ete tape. Un message
    // qui dit seulement « invalide » laisse l operateur sans piste — et la piste
    // la plus probable, ici, c est qu il n existe pas de cle privee brute du tout.
    const brut = pk.replace(/^0x/i, '');
    const mots = pk.trim().split(/\s+/).length;
    pk = null;
    console.error('\n  REFUS : ce que tu as saisi n est pas une cle privee brute.');
    console.error(`         longueur recue : ${brut.length} caracteres (64 attendus, prefixe 0x exclu)`);
    console.error(`         entierement hexadecimal : ${/^[0-9a-fA-F]*$/.test(brut) ? 'oui' : 'NON'}`);
    if (mots >= 12) {
      console.error('\n  Cela ressemble a une PHRASE DE RECUPERATION. Ne la saisis jamais ici :');
      console.error('  elle ouvre toute la tresorerie, pas seulement le gouverneur.');
    }
    console.error('\n  RAPPEL — le gouverneur est derive du meme xpub que la tresorerie');
    console.error('  (m/44\'/60\'/0\'), donc sa cle privee vit sur ton portefeuille MATERIEL');
    console.error('  et n en sort pas. Il n y a normalement rien a saisir ici.');
    console.error('\n  Utilise plutot :');
    console.error('    node scripts/signer-navigateur.js     (MetaMask + Ledger — le chemin prevu)');
    console.error('    node scripts/inscrire-cle-vote.js --calldata   (pour signer ailleurs)');
    console.error('\n  Rien n a ete envoye.');
    process.exit(1);
  }
  pk = null;   // on ne la garde pas en clair une seconde de plus que necessaire

  // LA VERIFICATION QUI EVITE L ACCIDENT : signer avec la mauvaise cle ferait
  // partir une transaction depuis un compte quelconque, qui reverterait « only
  // governor » APRES avoir coute du gaz — et surtout apres avoir expose la cle.
  if (portefeuille.address.toLowerCase() !== gouverneur.toLowerCase()) {
    console.error(`\n  REFUS : cette cle correspond a ${portefeuille.address},`);
    console.error(`          le gouverneur est ${gouverneur}.`);
    console.error('  Rien n a ete envoye.');
    process.exit(1);
  }
  console.log(`  cle reconnue : ${portefeuille.address} — c est bien le gouverneur.`);

  const tx = await portefeuille.sendTransaction({ to: VALSET, data, gasLimit: (gaz * 12n) / 10n });
  console.log(`\n  transaction envoyee : ${tx.hash}`);
  console.log('  attente du recu…');
  const recu = await tx.wait(1);
  if (!recu || recu.status !== 1) {
    console.error(`\n  ECHEC : la transaction a ete minee mais son statut est ${recu && recu.status}.`);
    process.exit(1);
  }
  console.log(`  MINEE au bloc ${recu.blockNumber}, statut 1, gaz consomme ${recu.gasUsed}.`);

  // --- 6. ce qu'il faut regarder ensuite ------------------------------------
  const prochainEpoch = (Math.floor(recu.blockNumber / 200) + 1) * 200;
  console.log('\n  ' + '='.repeat(72));
  console.log("  L EFFET EST DIFFERE. L extraData n est reecrite qu aux blocs d epoch.");
  console.log(`  Prochain bloc d epoch : ${prochainEpoch}  (dans ~${Math.round((prochainEpoch - recu.blockNumber) * 5 / 60)} min)`);
  console.log('  A ce bloc, les 48 octets de la cle de vote doivent cesser d etre nuls.');
  console.log('  Verification : node scripts/check-epoch.js  ou l etape 7 du document.\n');
})().catch((e) => { console.error('\nERREUR :', e.message); process.exit(1); });
