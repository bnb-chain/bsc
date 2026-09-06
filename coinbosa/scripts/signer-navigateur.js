// =============================================================================
// COINBOSA — faire signer l'inscription de la clé de vote par un portefeuille
// MATÉRIEL, sans jamais manipuler de clé privée.
//
//   cd coinbosa
//   RPC=https://explorer.coinbosa.com/rpc \
//   CLE_VOTE=<96 caracteres hex> node scripts/signer-navigateur.js
//
//   ... puis ouvrir l'adresse affichée dans le navigateur où vit MetaMask.
//
// POURQUOI CE CHEMIN EXISTE
// -------------------------
// `scripts/inscrire-cle-vote.js` demande une clé privée brute. Or le gouverneur
// est dérivé du même xpub que la trésorerie — chemin de compte m/44'/60'/0',
// voir `scripts/derive-treasury-addresses.js`. **Sa clé privée vit sur un
// appareil matériel et n'en sort pas.** Il n'y a donc, en principe, rien à
// saisir : demander une clé brute était une erreur de conception, pas une
// erreur de l'opérateur.
//
// Ici, la clé ne quitte jamais l'appareil. Ce programme :
//   · calcule la transaction, avec EXACTEMENT les mêmes gardes que l'autre outil ;
//   · la fait exécuter à blanc par la chaîne (eth_call) avant de la proposer ;
//   · sert une page locale sur 127.0.0.1 qui la présente à MetaMask ;
//   · MetaMask la transmet au Ledger, qui affiche et signe.
//
// CE QU'IL NE FAIT PAS, DÉLIBÉRÉMENT
// -----------------------------------
// Il n'écoute que sur 127.0.0.1 — jamais sur 0.0.0.0. Une page de gouvernance
// accessible depuis le réseau local serait une invitation. Il ne sert que trois
// fichiers, nommés en dur : aucune requête ne peut remonter l'arborescence.
// Il ne stocke rien et meurt avec le terminal.
// =============================================================================

const { ethers } = require('ethers');
const http = require('http');
const fs = require('fs');
const path = require('path');

const RPC = process.env.RPC || 'https://explorer.coinbosa.com/rpc';
const VALSET = '0x0000000000000000000000000000000000001000';
const PORT = Number(process.env.PORT || 8777);
const CLE_VOTE = (process.env.CLE_VOTE || '').trim().replace(/^0x/i, '');
const RACINE = path.join(__dirname, 'signer');

const ABI = [
  'function updateValidatorSet(address[] newVals, bytes[] newVotes)',
  'function getValidators() view returns (address[])',
  'function GOVERNOR() view returns (address)',
  'function INITIAL_VALIDATOR() view returns (address)',
];

(async () => {
  if (!/^[0-9a-fA-F]{96}$/.test(CLE_VOTE)) {
    console.error('ECHEC : CLE_VOTE doit faire exactement 96 caracteres hexadecimaux (48 octets), sans 0x.');
    console.error(`        recu : ${CLE_VOTE.length} caractere(s).`);
    console.error('        Elle se releve dans bls/keystore/keystore-*.json, champ "pubkey".');
    process.exit(1);
  }
  if (/^0+$/.test(CLE_VOTE)) {
    console.error('ECHEC : cle de vote entierement nulle — c est l etat actuel, cela n inscrirait rien.');
    process.exit(1);
  }

  const provider = new ethers.JsonRpcProvider(RPC);
  const c = new ethers.Contract(VALSET, ABI, provider);
  const reseau = await provider.getNetwork();
  const actuels = await c.getValidators();
  const gouverneur = await c.GOVERNOR();
  const initial = await c.INITIAL_VALIDATOR();

  // --- le garde-fou central, identique a l'autre outil ------------------------
  // updateValidatorSet est la MEME fonction qui sert a ajouter des validateurs, et
  // passer de 1 a N alors qu un seul noeud scelle arrete la chaine au bloc d epoch
  // suivant, sans retour possible. On reemet donc l ensemble A L IDENTIQUE.
  if (actuels.length !== 1) {
    console.error(`\n  REFUS : ${actuels.length} validateurs sur la chaine — cet outil n en gere qu un.`);
    process.exit(1);
  }
  const listeVals = [ethers.getAddress(actuels[0])];
  if (listeVals[0].toLowerCase() !== initial.toLowerCase()) {
    console.error('\n  REFUS : le validateur present n est pas celui de la genese — situation non prevue.');
    process.exit(1);
  }
  const listeVotes = ['0x' + CLE_VOTE.toLowerCase()];

  const iface = new ethers.Interface(ABI);
  const data = iface.encodeFunctionData('updateValidatorSet', [listeVals, listeVotes]);
  const octets = (data.length - 2) / 2;
  if (octets !== 292) { console.error(`\n  REFUS : calldata de ${octets} octets au lieu de 292.`); process.exit(1); }
  if (!data.toLowerCase().includes(CLE_VOTE.toLowerCase())) {
    console.error('\n  REFUS : la cle ne se retrouve pas dans la calldata encodee.'); process.exit(1);
  }

  // --- la chaine execute la transaction sans la publier -----------------------
  try {
    await provider.call({ from: gouverneur, to: VALSET, data });
  } catch (e) {
    console.error(`\n  REFUS : la chaine REJETTE cet appel : ${(e.shortMessage || e.message || '').slice(0, 200)}`);
    process.exit(1);
  }
  // Contre-epreuve : la meme depuis une autre adresse DOIT etre refusee.
  let gardeOk = false;
  try { await provider.call({ from: listeVals[0], to: VALSET, data }); }
  catch (e) { gardeOk = /only governor/i.test(e.shortMessage || e.message || ''); }
  if (!gardeOk) {
    console.error('\n  REFUS : la garde « only governor » ne se comporte pas comme attendu — ne rien envoyer.');
    process.exit(1);
  }

  const gaz = await provider.estimateGas({ from: gouverneur, to: VALSET, data });
  const prix = (await provider.getFeeData()).gasPrice;
  const gazLimite = (gaz * 12n) / 10n;

  const params = {
    chainId: Number(reseau.chainId),
    contrat: VALSET,
    gouverneur,
    validateur: listeVals[0],
    cleVote: CLE_VOTE.toLowerCase(),
    data,
    gaz: gaz.toString(),
    gazHex: '0x' + gazLimite.toString(16),
    cout: ethers.formatEther(gaz * prix),
  };

  // --- le serveur local -------------------------------------------------------
  // Trois fichiers, nommes en dur. Aucun chemin ne vient de la requete, donc
  // aucune remontee d arborescence n est possible.
  const SERVIS = {
    '/': ['index.html', 'text/html; charset=utf-8'],
    '/index.html': ['index.html', 'text/html; charset=utf-8'],
    '/signer.js': ['signer.js', 'text/javascript; charset=utf-8'],
  };

  const serveur = http.createServer((q, r) => {
    const chemin = (q.url || '/').split('?')[0];
    if (chemin === '/params.json') {
      r.writeHead(200, { 'content-type': 'application/json; charset=utf-8', 'cache-control': 'no-store' });
      return r.end(JSON.stringify(params));
    }
    const entree = SERVIS[chemin];
    if (!entree) { r.writeHead(404); return r.end('non'); }
    let contenu;
    try { contenu = fs.readFileSync(path.join(RACINE, entree[0])); }
    catch { r.writeHead(500); return r.end('fichier de page introuvable'); }
    r.writeHead(200, { 'content-type': entree[1], 'cache-control': 'no-store' });
    r.end(contenu);
  });

  serveur.on('error', (e) => {
    if (e.code === 'EADDRINUSE') {
      console.error(`\n  ECHEC : le port ${PORT} est deja pris. Relance avec PORT=8778 par exemple.`);
    } else console.error('\n  ECHEC serveur :', e.message);
    process.exit(1);
  });

  serveur.listen(PORT, '127.0.0.1', () => {
    console.log('\n  INSCRIPTION DE LA CLE DE VOTE — signature par portefeuille materiel');
    console.log('  ' + '='.repeat(72));
    console.log(`  chain ID       : ${params.chainId}`);
    console.log(`  gouverneur     : ${gouverneur}`);
    console.log(`  validateur     : ${listeVals[0]}`);
    console.log(`  cle de vote    : ${CLE_VOTE.slice(0, 16)}…${CLE_VOTE.slice(-8)}`);
    console.log(`  calldata       : ${octets} octets`);
    console.log(`  gaz estime     : ${gaz}   (limite proposee ${gazLimite})`);
    console.log(`  cout           : ${params.cout} BOSA`);
    console.log('\n  SIMULATION : la transaction passe. CONTRE-EPREUVE : « only governor » depuis');
    console.log('  une autre adresse. Rien n a ete publie.');
    console.log('\n  ' + '='.repeat(72));
    console.log(`  OUVRE CETTE ADRESSE dans le navigateur ou vit MetaMask :\n`);
    console.log(`      http://127.0.0.1:${PORT}\n`);
    console.log('  Le Ledger doit etre branche, deverrouille, application Ethereum ouverte,');
    console.log('  et le compte du gouverneur selectionne dans MetaMask.');
    console.log('\n  Aucune cle privee ne transite par ce programme ni par la page.');
    console.log('  Ctrl-C pour arreter le serveur.\n');
  });
})().catch((e) => { console.error('\nERREUR :', e.message); process.exit(1); });
