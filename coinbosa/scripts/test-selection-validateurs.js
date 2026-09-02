// =============================================================================
// Suite de tests — SELECTION DES VALIDATEURS (CoinbosaStake._construire)
//
// CE QU'ELLE DEFEND. Parlia n'avance que si ⌊N/2⌋+1 scelleurs DISTINCTS et EN
// LIGNE produisent reellement (snapshot.go, minerHistoryCheckLen a TurnLength=1).
// Retourne : un jeu de taille N n'est soutenable que si N <= 2q-1, ou q est le
// nombre de membres du jeu qu'on a VU sceller. Ajouter un validateur qui mise
// mais ne produit jamais augmente N sans augmenter q ; en ajouter un de trop
// fait passer le quorum au-dessus du nombre de producteurs, et LA CHAINE
// S'ARRETE — sans retour possible, puisqu'une chaine qui ne produit plus ne peut
// plus recevoir la transaction qui la reparerait. Ce n'est pas une hypothese :
// le test Go TestCoinbosaAddSecondValidatorHaltsChain l'a mesure.
//
// La ligne qui tient tout cela debout est dans le PASSAGE 2 de _construire :
//
//     if (q == 0 || t + 1 > 2 * q - 1) break;
//
// COMMENT ON EXECUTE LE CONTRAT SANS RIEN AJOUTER. Il n'y a dans ce depot ni
// hardhat, ni foundry, ni EVM en memoire, et on ne peut pas en ajouter : la
// version de solc fige le bytecode du contrat systeme, donc le hash du bloc 0,
// donc l'identite de la chaine. `geth --dev` n'existe pas non plus dans ce
// binaire (le mode developer a ete retire du client BSC ; `./geth --dev` repond
// « flag provided but not defined »). Le chemin retenu, entierement verifie :
//
//   1. un noeud initialise sur le VRAI genesis Coinbosa, qui ne mine jamais,
//      ne detient aucune cle, et reste au bloc 0 : un moteur EVM, rien d'autre ;
//   2. le harnais contracts/EssaiSelection.sol (qui HERITE de CoinbosaStake et
//      n'en modifie pas une ligne) pose par SURCHARGE DE CODE dans un eth_call —
//      pas de deploiement, pas de transaction, pas de limite EIP-170 ;
//   3. `blockOverrides` pour choisir block.number (la fenetre de 200 blocs de
//      _vuProduire, et le declencheur (n+1) % 200 == 0) et block.timestamp ;
//   4. `debug_traceCall` quand il faut LIRE LES EVENEMENTS : JeuRecalcule(t,p,kMax)
//      est le temoignage du contrat sur son propre calcul ;
//   5. `eth_simulateV1` pour la section 6, la seule qui emprunte le VRAI point
//      d'entree — `enregistrerScellage()` emise par le scelleur, au bloc 199,
//      avec les trois gardes systeme franchies pour de vrai.
//
// Chaque cas est un appel isole : les ecritures sont jetees a la fin, les cas
// sont independants par construction, aucun bloc n'est mine.
//
// AUCUNE DEPENDANCE NPM AJOUTEE : ethers 6.17.0 et solc 0.8.26, deja presents.
// package.json, package-lock.json, compile.js et CoinbosaStake.sol ne sont ni
// lus en ecriture ni modifies. Le mutant de la section 8 est compile EN MEMOIRE,
// a partir d'une copie de la source ; build/ n'est jamais touche.
//
// Lancement :  node scripts/test-selection-validateurs.js
// Variables :  RPC=…  (defaut : la suite demarre son propre noeud jetable)
//              GETH=… CAS=… GRAINE=…
// =============================================================================

const fs = require('fs');
const path = require('path');
const os = require('os');
const { spawn, execFileSync } = require('child_process');
const solc = require('solc');
const { ethers } = require('ethers');

const RACINE = path.join(__dirname, '..');
const SRC = path.join(RACINE, 'contracts');

// -----------------------------------------------------------------------------
// Constantes du contrat, recopiees ici pour que l'oracle soit lisible. Elles sont
// verifiees contre la source en section 0 : un oracle qui derive du contrat rend
// toute la suite muette, et c'est la panne la plus facile a ne pas voir.
// -----------------------------------------------------------------------------
const GENESE = '0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50';
const MAX_PLACES = 41;
const TAILLE_CLASSEMENT = 82;
const EPOQUE = 200n;
const DELAI_CANDIDATURE = 7n * 86400n;
const SEUIL_QUARANTAINE = 50;
const M160 = (1n << 160n) - 1n;
const INEXISTANT = 0, EN_ATTENTE = 1, ACTIF = 2, EN_QUARANTAINE = 3, EN_DEBLOCAGE = 4, BANNI = 5;

// Bloc 199 : c'est le SEUL point d'epoque atteignable depuis le bloc 0 par
// eth_simulateV1 (maxSimulateBlocks = 256), et (199+1) % 200 == 0 y declenche
// _recalculer. Toute la suite s'y tient pour que les sections 1 a 7 et la
// section 6 parlent du meme bloc.
const BN_DEFAUT = 199n;
const BN = BN_DEFAUT;
const TS = 1800000000n; // horodatage large : tous les delais de 7 jours sont franchis

// La ligne de garde, mot pour mot. La section 8 verifie qu'elle apparait
// EXACTEMENT UNE FOIS : sinon la porte de mutation passerait en silence, et la
// suite se declarerait « sensible » sans avoir rien mute.
const LIGNE_GARDE = '            if (q == 0 || t + 1 > 2 * q - 1) break;';

// -----------------------------------------------------------------------------
// Compte-rendu — meme forme que scripts/test-brc20.js
// -----------------------------------------------------------------------------
let pass = 0, fail = 0;
const results = [];

function check(name, actual, expected) {
  const ok = String(actual) === String(expected);
  ok ? pass++ : fail++;
  results.push({ name, ok, actual: String(actual), expected: String(expected) });
  console.log(`  ${ok ? '\x1b[32mOK  \x1b[0m' : '\x1b[31mECHEC\x1b[0m'} ${name}${ok ? '' : `\n         attendu : ${expected}\n         obtenu  : ${actual}`}`);
}

/// Variante booleenne : « la propriete tient », sans avoir a ecrire `, true` partout.
function checkQue(name, condition) {
  check(name, condition ? 'vrai' : 'faux', 'vrai');
}

function titre(s) { console.log(`\n\x1b[1m${s}\x1b[0m`); }
function note(s) { console.log(`  \x1b[90m${s}\x1b[0m`); }

// =============================================================================
// LE BANC D'ESSAI
// =============================================================================

/// Compile CoinbosaStake + EssaiSelection EN MEMOIRE, avec exactement les
/// reglages de scripts/compile.js (optimizer 200, evmVersion shanghai). Rien
/// n'est ecrit sur disque : `sourceStake` permet a la section 8 de compiler un
/// mutant sans jamais poser le moindre octet dans contracts/ ni dans build/.
function compiler(sourceStake) {
  const sources = {
    'CoinbosaStake.sol': { content: sourceStake },
    'EssaiSelection.sol': { content: fs.readFileSync(path.join(SRC, 'EssaiSelection.sol'), 'utf8') },
  };
  const input = {
    language: 'Solidity',
    sources,
    settings: {
      optimizer: { enabled: true, runs: 200 },
      evmVersion: 'shanghai',
      outputSelection: { '*': { '*': ['abi', 'evm.bytecode.object', 'evm.deployedBytecode.object'] } },
    },
  };
  const out = JSON.parse(solc.compile(JSON.stringify(input)));
  const errs = (out.errors || []).filter((e) => e.severity === 'error');
  if (errs.length) throw new Error('compilation :\n' + errs.map((e) => e.formattedMessage).join('\n'));
  return {
    harnais: out.contracts['EssaiSelection.sol'].EssaiSelection,
    stake: out.contracts['CoinbosaStake.sol'].CoinbosaStake,
  };
}

/// Adresse du harnais. Elle n'a aucune importance pour _construire, mais elle ne
/// doit JAMAIS valoir block.coinbase : l'EVM de BSC refuse d'executer le moindre
/// code a l'adresse du coinbase (core/vm/interpreter.go, ErrCoinbaseAsContract).
const HARNAIS = '0x00000000000000000000000000000000000C0DE1';

let RPC = process.env.RPC || null;
let noeud = null; // le processus geth demarre par la suite, s'il y en a un
let idRpc = 1;

async function rpc(method, params) {
  const r = await fetch(RPC, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ jsonrpc: '2.0', id: idRpc++, method, params }),
  });
  return r.json();
}

/// Demarre un noeud JETABLE sur le vrai genesis. Il ne mine pas, ne detient
/// aucune cle, ne parle a personne et reste au bloc 0.
///
/// `--rpc.gascap 0` n'est PAS un detail de confort : sans lui le gaz d'un
/// eth_call est plafonne a 50 000 000, tres en dessous du SEUIL_GAZ_SYSTEME de
/// 1e12 ; `_estAppelSysteme()` refuserait alors TOUJOURS, la section 6 verrait
/// SanctionIgnoree partout et se declarerait verte pour la pire des raisons.
/// `--datadir.minfreedisk 0` : geth s'arrete de lui-meme sous 1 Gio libre.
async function demarrerNoeud() {
  const geth = process.env.GETH
    || [path.join(RACINE, '..', 'geth'), path.join(RACINE, 'build', 'bin', 'geth')].find((p) => fs.existsSync(p));
  if (!geth) throw new Error("binaire geth introuvable — indique-le avec GETH=/chemin/vers/geth");
  const genesis = path.join(RACINE, 'genesis', 'genesis-coinbosa.json');
  const dd = fs.mkdtempSync(path.join(os.tmpdir(), 'coinbosa-essai-'));
  const port = Number(process.env.RPC_PORT || 8599);
  execFileSync(geth, ['init', '--datadir', dd, genesis], { stdio: 'ignore' });
  const journal = fs.openSync(path.join(dd, 'geth.log'), 'a');
  const p = spawn(geth, [
    '--datadir', dd, '--datadir.minfreedisk', '0', '--networkid', '26262',
    // Port p2p distinct de celui de scripts/start-node.sh : la suite ne doit
    // jamais entrer en collision avec un noeud de developpement en cours.
    '--port', '30499', '--ipcdisable',
    '--http', '--http.addr', '127.0.0.1', '--http.port', String(port),
    '--http.api', 'eth,net,web3,debug',
    '--nodiscover', '--maxpeers', '0', '--syncmode', 'full', '--gcmode', 'archive',
    '--rpc.gascap', '0', '--verbosity', '1',
  ], { stdio: ['ignore', journal, journal], detached: false });
  noeud = { proc: p, dd, port };
  RPC = `http://127.0.0.1:${port}`;
  for (let i = 0; i < 100; i++) {
    try { const j = await rpc('eth_chainId', []); if (j.result) return; } catch { /* pas encore la */ }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`le noeud jetable n'a pas repondu sur ${RPC} (journal : ${path.join(dd, 'geth.log')})`);
}

function arreterNoeud() {
  if (!noeud) return;
  try { noeud.proc.kill('SIGKILL'); } catch { /* deja mort */ }
  try { fs.rmSync(noeud.dd, { recursive: true, force: true }); } catch { /* peu importe */ }
  noeud = null;
}

// -----------------------------------------------------------------------------
// Les trois transports
// -----------------------------------------------------------------------------

/// Appel simple : le resultat ABI, ou {erreur} si l'EVM a revert. Le revert est
/// une VALEUR ici, pas une exception : sur le chemin de consensus c'est le
/// resultat le plus grave, il doit pouvoir etre affirme.
async function appeler(code, iface, fn, args, bloc) {
  const n = bloc === undefined ? BN : BigInt(bloc);
  const j = await rpc('eth_call', [
    { to: HARNAIS, input: iface.encodeFunctionData(fn, args), gas: '0x38d7ea4c68000' },
    'latest',
    { [HARNAIS]: { code } },
    { number: '0x' + n.toString(16), time: '0x' + TS.toString(16) },
  ]);
  if (j.error) return { erreur: j.error.message };
  return { valeur: iface.decodeFunctionResult(fn, j.result) };
}

/// Appel trace : en plus du resultat, les EVENEMENTS et le gaz consomme.
async function tracer(code, iface, fn, args) {
  const j = await rpc('debug_traceCall', [
    { to: HARNAIS, input: iface.encodeFunctionData(fn, args), gas: '0x38d7ea4c68000' },
    'latest',
    {
      tracer: 'callTracer', tracerConfig: { withLog: true },
      stateOverrides: { [HARNAIS]: { code } },
      blockOverrides: { number: '0x' + BN.toString(16), time: '0x' + TS.toString(16) },
    },
  ]);
  if (j.error) return { erreur: j.error.message, evenements: [] };
  const t = j.result;
  const evenements = (t.logs || []).map((l) => {
    try { const p = iface.parseLog({ topics: l.topics, data: l.data }); return { nom: p.name, args: p.args.map(String) }; }
    catch { return null; }
  }).filter(Boolean);
  if (t.error) return { erreur: t.error, evenements };
  return { valeur: iface.decodeFunctionResult(fn, t.output), evenements, gaz: Number(BigInt(t.gasUsed)) };
}

/// LE VRAI CHEMIN. Un bloc simule au numero 199, trois appels qui se suivent
/// dans le meme etat : on pose le scenario, le SCELLEUR emet
/// `enregistrerScellage()` vers le contrat, puis on releve le bilan.
///
/// Pourquoi trois appels et pas un : `enregistrerScellage()` exige
/// msg.sender == block.coinbase, et le harnais ne peut pas se faire passer pour
/// le coinbase (l'EVM de BSC interdit tout code a cette adresse). Il faut donc
/// que l'appel systeme vienne d'un compte EXTERIEUR, et eth_simulateV1 est le
/// seul transport ou l'etat pose par un appel survit jusqu'au suivant.
async function simulerEpoque(code, iface, scenario, scelleur, ancien, gazSysteme, prixGaz) {
  const j = await rpc('eth_simulateV1', [{
    blockStateCalls: [{
      blockOverrides: {
        number: '0x' + BN.toString(16), time: '0x' + TS.toString(16),
        feeRecipient: scelleur, baseFeePerGas: '0x0', gasLimit: '0x7fffffffffffff',
      },
      stateOverrides: { [HARNAIS]: { code } },
      calls: [
        { from: '0x0000000000000000000000000000000000000099', to: HARNAIS, gasPrice: '0x0', gas: '0x4000000', input: iface.encodeFunctionData('essaiInstaller', [scenario]) },
        { from: scelleur, to: HARNAIS, gasPrice: prixGaz || '0x0', gas: gazSysteme || '0x10000000000', input: iface.encodeFunctionData('enregistrerScellage', []) },
        { from: '0x0000000000000000000000000000000000000099', to: HARNAIS, gasPrice: '0x0', gas: '0x4000000', input: iface.encodeFunctionData('essaiBilan', [ancien]) },
      ],
    }],
    validation: false,
  }, '0x0']);
  if (j.error) throw new Error('eth_simulateV1 : ' + j.error.message);
  const bloc = j.result[j.result.length - 1];
  const sys = bloc.calls[1];
  const evenements = (sys.logs || []).map((l) => {
    try { const p = iface.parseLog({ topics: l.topics, data: l.data }); return { nom: p.name, args: p.args.map(String) }; }
    catch { return null; }
  }).filter(Boolean);
  const b = bloc.calls[2].status === '0x1'
    ? lireBilan(iface.decodeFunctionResult('essaiBilan', bloc.calls[2].returnData)[0]) : null;
  return { statut: sys.status, gaz: Number(BigInt(sys.gasUsed)), evenements, bilan: b, numero: Number(BigInt(bloc.number)) };
}

function lireBilan(b) {
  return {
    jeu: b[0].map((a) => ethers.getAddress(a)), votes: b[1].map(String),
    nbAvant: Number(b[2]), nbApres: Number(b[3]), pReel: Number(b[4]), q: Number(b[5]),
  };
}

// =============================================================================
// L'ORACLE — une reimplementation INDEPENDANTE de _construire
//
// Il n'existe que pour une raison : sans lui, la suite ne peut affirmer que des
// PROPRIETES (« t ne depasse pas 2q-1 »), et une propriete ne distingue pas un
// contrat juste d'un contrat trop prudent. Le mutant qui remplace `>` par `>=`
// respecte toutes les proprietes de surete et rend pourtant un jeu trop petit
// d'une place a chaque epoque : seul un oracle EXACT le voit.
// =============================================================================

const mot = (a, enjeu) => (BigInt(enjeu) << 160n) | (M160 - BigInt(a));
const adresseDuMot = (w) => ethers.getAddress('0x' + ((M160 - (w & M160)) & M160).toString(16).padStart(40, '0'));

/// Reconstruit l'etat tel que _installer l'ecrit, avec les deux predicats du
/// contrat traduits terme a terme (section 0 les croise contre le contrat lui-meme).
function etat(sc, bn) {
  const BN = bn === undefined ? BN_DEFAUT : BigInt(bn);
  const E = new Map();
  for (const m of sc.membres) E.set(ethers.getAddress(m.adresse), m);

  const vuProduire = (a) => {
    const e = E.get(ethers.getAddress(a));
    if (!e) return false;
    const d = BigInt(e.dernierBlocScelle);
    return d !== 0n && BN >= d && BN - d < EPOQUE;
  };
  const eligible = (a) => {
    a = ethers.getAddress(a);
    if (a === ethers.ZeroAddress || a === GENESE) return false;
    const e = E.get(a);
    if (!e) return false;
    if (e.etat !== ACTIF && e.etat !== EN_ATTENTE) return false;
    if (BigInt(e.enjeu) < BigInt(e.enjeuMinAdmission)) return false;
    if (e.etat === EN_ATTENTE && TS < BigInt(e.dateCandidature) + DELAI_CANDIDATURE) return false;
    return true;
  };

  const elus = sc.membres.filter((m) => m.elu).slice(0, MAX_PLACES).map((m) => ethers.getAddress(m.adresse));
  let cl = sc.membres.filter((m) => m.classe).slice(0, TAILLE_CLASSEMENT).map((m) => mot(m.adresse, m.enjeu));
  cl.sort((a, b) => (a < b ? 1 : a > b ? -1 : 0)); // mot DECROISSANT, comme _inserer le maintient
  if (sc.motsBruts.length) {
    for (let i = 0; i < sc.motsBruts.length && i < TAILLE_CLASSEMENT; i++) cl[i] = BigInt(sc.motsBruts[i]);
    cl = cl.slice(0, sc.motsBruts.length);
  }
  const nbClasses = sc.nbClassesForce || cl.length;
  const nbElus = sc.nbElusForce || elus.length;
  return { E, vuProduire, eligible, elus, cl, nbClasses, nbElus };
}

/// L'oracle de _construire. Miroir exact des deux passages, garde comprise.
function oracleConstruire(sc, kMax) {
  const st = etat(sc);
  const sel = [GENESE];
  let t = 1;
  let q = st.vuProduire(GENESE) ? 1 : 0;
  const nc = Math.min(st.nbClasses, TAILLE_CLASSEMENT);

  for (let i = 0; i < nc && t < kMax; i++) {           // PASSAGE 1 — les averes
    const a = adresseDuMot(st.cl[i] ?? 0n);
    if (!st.eligible(a) || !st.vuProduire(a)) continue;
    sel.push(a); t++; q++;
  }
  const t1 = t;
  for (let i = 0; i < nc && t < kMax; i++) {           // PASSAGE 2 — les non averes
    const a = adresseDuMot(st.cl[i] ?? 0n);
    if (!st.eligible(a) || st.vuProduire(a)) continue;
    if (q === 0 || t + 1 > 2 * q - 1) break;           // LA GARDE
    sel.push(a); t++;
  }
  return { sel, t, q, t1 };
}

/// L'oracle du plafond calcule par _recalculer. Trois bornes distinctes qu'un
/// test doit savoir separer, sans quoi il croit tester l'invariant alors que
/// c'est le rail de croissance qui a mordu.
function oracleKMax(sc) {
  const st = etat(sc);
  const nAnc = Math.min(st.nbElus, MAX_PLACES);
  let pReel = 0;
  for (let i = 0; i < nAnc; i++) if (st.elus[i] && st.vuProduire(st.elus[i])) pReel++;
  let amorcage = 0;
  if (sc.atteste !== ethers.ZeroAddress
      && !st.vuProduire(sc.atteste)
      && BN <= BigInt(sc.attesteDepuis) + 2n * EPOQUE) amorcage = 1;
  let p = pReel + amorcage;
  if (p === 0) p = 1;
  let kMax = 2 * p - 1;
  if (kMax > MAX_PLACES) kMax = MAX_PLACES;
  if (kMax > nAnc + 1) kMax = nAnc + 1;
  return { nAnc, pReel, amorcage, p, kMax };
}

// =============================================================================
// FABRIQUE DE SCENARIOS
// =============================================================================

const adr = (i) => ethers.getAddress('0x' + (BigInt('0x2000000000000000000000000000000000000000') + BigInt(i)).toString(16).padStart(40, '0'));

function membre(o) {
  return {
    adresse: o.a,
    enjeu: o.enjeu ?? 1000n,
    enjeuMinAdmission: o.min ?? 1000n,
    dernierBlocScelle: o.dbs ?? 0,
    dateCandidature: o.dc ?? 0,
    absences: o.abs ?? 0,
    etat: o.etat ?? ACTIF,
    elu: !!o.elu,
    classe: !!o.classe,
  };
}
function scenario(membres, o = {}) {
  return {
    membres,
    motsBruts: o.motsBruts ?? [],
    nbClassesForce: o.nbClassesForce ?? 0,
    nbElusForce: o.nbElusForce ?? 0,
    atteste: o.atteste ?? ethers.ZeroAddress,
    attesteDepuis: o.attesteDepuis ?? 0,
    amorcageClos: !!o.amorcageClos,
  };
}
/// La genese avec sa marque de production. C'est le seul levier qui fait passer
/// q de 0 a 1 sans toucher au classement — donc le levier qui isole la garde.
const genese = (avere) => membre({ a: GENESE, etat: INEXISTANT, enjeu: 0n, min: 0n, elu: true, dbs: avere ? 150 : 0 });
const avere = (i, o = {}) => membre({ a: adr(i), dbs: o.dbs ?? 150, enjeu: o.enjeu ?? 5000n, elu: o.elu ?? false, classe: o.classe ?? true, etat: o.etat ?? ACTIF, min: o.min ?? 1000n, abs: o.abs ?? 0 });
const dormant = (i, o = {}) => membre({ a: adr(i), dbs: 0, enjeu: o.enjeu ?? 4000n, elu: o.elu ?? false, classe: o.classe ?? true, etat: o.etat ?? ACTIF, min: o.min ?? 1000n, dc: o.dc ?? 0 });

// =============================================================================
// LES SIX PROPRIETES UNIVERSELLES
//
// Elles sont evaluees sur CHAQUE etat du balayage, et ce sont elles — pas les
// valeurs temoins des cas nommes — qui font le travail. Chacune ferme une panne
// distincte ; aucune n'est impliquee par une autre.
// =============================================================================

function proprietes(sc, kMax, r) {
  const st = etat(sc);
  const gVu = st.vuProduire(GENESE);
  const sel = r.sel.slice(0, r.t);
  const manques = [];

  // P1 — la forme du jeu. Un doublon ou une adresse nulle ne fait pas revert le
  // contrat : il fait PANIQUER le noeud Go dans la boucle voteAddrMap de
  // parlia.go, donc tous les noeuds d'un coup puisqu'ils lisent le meme etat.
  if (!(sel[0] === GENESE && r.t >= 1 && r.t <= kMax
        && new Set(sel).size === sel.length && !sel.includes(ethers.ZeroAddress)
        && sel.slice(1).every((a) => st.eligible(a)))) manques.push('P1-forme');

  // P2 — q == 0 => t == 1. C'est le PREMIER terme de la garde, et il est
  // irremplacable : avec q == 0 le second terme calcule 2*0-1 sur un uint256,
  // qui en solidity 0.8 ne « deborde » pas mais PANIQUE (Panic 0x11). Sans ce
  // terme, le bloc d'epoque revert et devient improduisible.
  if (r.q === 0 && r.t !== 1) manques.push('P2-q0');

  // P3 — le PASSAGE 2 ne pousse jamais t au-dela de 2q-1. Formule ainsi, et non
  // « t <= 2q-1 » tout court, parce que t1 (la taille a la sortie du passage 1)
  // peut deja depasser cette borne quand la genese ne produit pas — voir la
  // section 9. P3 est exactement le contrat de la garde, ni plus ni moins.
  if (!(r.t <= Math.max(r.t1, 2 * r.q - 1))) manques.push('P3-passage2');

  // P4 — l'invariant lui-meme, dans son domaine de validite : des lors que la
  // place 0 produit, le jeu entier respecte t <= 2q-1.
  if (gVu && !(r.t <= 2 * r.q - 1)) manques.push('P4-invariant');

  // P5 — la meme chose dite comme Parlia la lit : le quorum est tenable. C'est
  // la formulation qui compte, parce qu'elle ne parle pas de 2q-1 : une
  // reecriture de la formule interne ne peut pas la satisfaire par accident.
  if (gVu && !(Math.floor(r.t / 2) + 1 <= r.q)) manques.push('P5-quorum');

  // P6 — le passage 1 est exhaustif. Une garde mal placee LA ferait affamer les
  // producteurs reels : le jeu resterait petit et l'invariant serait respecte
  // pour la pire des raisons. Si t < kMax, aucun avere eligible n'attend dehors.
  if (r.t < kMax) {
    const nc = Math.min(st.nbClasses, TAILLE_CLASSEMENT);
    for (let i = 0; i < nc; i++) {
      const a = adresseDuMot(st.cl[i] ?? 0n);
      if (st.eligible(a) && st.vuProduire(a) && !sel.includes(a)) { manques.push('P6-passage1'); break; }
    }
  }
  return manques;
}

// =============================================================================
// GENERATEUR STRATIFIE
//
// Un tirage uniforme ne visiterait jamais q <= 1 : c'est pourtant la seule zone
// ou la garde decide quoi que ce soit. On stratifie donc sur les grandeurs qui
// la font mordre, et surtout sur l'ECART entre pReel et q — des titulaires
// averes mais devenus ineligibles, qui gonflent le plafond sans pouvoir gonfler
// le quorum. C'est cet ecart, et lui seul, qui produit les etats interessants.
// =============================================================================

function tirage(graine) {
  let s = BigInt(graine) | 1n;
  return () => { s = (s * 6364136223846793005n + 1442695040888963407n) & ((1n << 64n) - 1n); return Number((s >> 11n) % 1000000n) / 1000000; };
}

function genererScenario(rnd) {
  const ri = (n) => Math.floor(rnd() * n);
  const parmi = (a) => a[ri(a.length)];
  const membres = [genese(rnd() < 0.5)];
  if (rnd() < 0.1) membres[0].classe = true; // la genese au classement : doit etre ignoree

  const nA = parmi([0, 0, 1, 1, 2, 3, 5, 10, 20]);   // averes eligibles, classes
  const nB = parmi([0, 1, 1, 2, 3, 5, 10, 20, 40]);  // non averes eligibles, classes
  const nH = parmi([0, 0, 1, 2, 5, 20]);             // averes ELUS mais hors classement : pReel > q
  const nX = parmi([0, 1, 3, 8]);                    // bruit ineligible au classement
  let k = 1;
  for (let i = 0; i < nA; i++) membres.push(avere(k++, { enjeu: BigInt(1000 + ri(9000)), dbs: 1 + ri(198), elu: rnd() < 0.5 }));
  for (let i = 0; i < nB; i++) membres.push(dormant(k++, { enjeu: BigInt(1000 + ri(9000)), elu: rnd() < 0.2 }));
  for (let i = 0; i < nH; i++) membres.push(avere(k++, { enjeu: BigInt(1000 + ri(9000)), dbs: 1 + ri(198), elu: true, classe: false, etat: parmi([EN_DEBLOCAGE, EN_QUARANTAINE, BANNI]) }));
  for (let i = 0; i < nX; i++) {
    membres.push(membre({
      a: adr(k++), enjeu: BigInt(ri(2000)), min: 1000n, classe: true,
      dbs: rnd() < 0.5 ? 1 + ri(198) : 0,
      dc: TS - BigInt(ri(20)) * 86400n,
      etat: parmi([EN_ATTENTE, EN_QUARANTAINE, EN_DEBLOCAGE, BANNI, INEXISTANT, ACTIF]),
    }));
  }
  const o = {};
  if (rnd() < 0.08) o.nbClassesForce = 100 + ri(150); // au-dela de TAILLE_CLASSEMENT
  if (rnd() < 0.05) o.nbElusForce = 50 + ri(200);     // au-dela de MAX_PLACES
  if (rnd() < 0.15) { o.atteste = adr(1 + ri(Math.max(1, k - 1))); o.attesteDepuis = ri(199); }
  return { sc: scenario(membres, o), kMax: parmi([1, 2, 3, 4, 5, 7, 11, 21, 41]) };
}

// =============================================================================
// LA CAMPAGNE — le noyau reutilise tel quel par la section 8
//
// Une seule fonction : elle prend un bytecode et rend la liste des echecs. Le
// code sain doit en rendre ZERO ; chaque mutant doit en rendre au moins un. La
// section 8 ne rejoue donc pas des sondes taillees sur mesure — elle rejoue LA
// SUITE, ce qui est la seule facon de prouver que c'est la suite qui mord.
// =============================================================================

const CAS_DISCRIMINANTS = () => {
  const l = [];
  l.push(['q=0 avec 40 dormants au portillon', scenario([genese(false), avere(1, { elu: true, classe: false, etat: EN_DEBLOCAGE }), ...Array.from({ length: 40 }, (_, i) => dormant(10 + i))]), 41]);
  l.push(['q=0 sans aucun dormant (temoin)', scenario([genese(false), avere(1, { elu: true, classe: false, etat: EN_DEBLOCAGE })]), 41]);
  l.push(['q=1, la borne 2q-1 vaut 1', scenario([genese(true), dormant(1), dormant(2)]), 3]);
  l.push(['q=2, la borne 2q-1 vaut 3', scenario([genese(true), avere(1), dormant(2), dormant(3)]), 5]);
  l.push(['q=3, la borne 2q-1 vaut 5', scenario([genese(true), avere(1), avere(2), dormant(3), dormant(4), dormant(5)]), 11]);
  l.push(['41 places exigent 21 averes', scenario([genese(true), ...Array.from({ length: 20 }, (_, i) => avere(1 + i)), ...Array.from({ length: 40 }, (_, i) => dormant(100 + i, { enjeu: 100n + BigInt(i) }))]), 41]);
  l.push(['aucun dormant : t = q', scenario([genese(true), ...Array.from({ length: 20 }, (_, i) => avere(1 + i))]), 41]);
  l.push(['kMax mord avant 2q-1', scenario([genese(true), avere(1), dormant(2), dormant(3)]), 2]);
  l.push(['genese hors ligne, un avere au classement', scenario([genese(false), avere(1, { elu: true }), dormant(2)]), 3]);
  l.push(['classement sature de dormants tres riches', scenario([genese(true), avere(1, { enjeu: 1000n }), ...Array.from({ length: 60 }, (_, i) => dormant(100 + i, { enjeu: 900000n - BigInt(i) }))]), 41]);
  return l;
};

async function campagne(code, iface, nSweep, graine) {
  const echecs = [];
  for (const [nom, sc, kMax] of CAS_DISCRIMINANTS()) {
    const r = await appeler(code, iface, 'essaiConstruire', [sc, kMax]);
    if (r.erreur) { echecs.push(`${nom} : REVERT (${r.erreur})`); continue; }
    const obt = { sel: r.valeur[0].map((a) => ethers.getAddress(a)), t: Number(r.valeur[1]), q: Number(r.valeur[2]), t1: Number(r.valeur[3]) };
    const att = oracleConstruire(sc, kMax);
    if (obt.t !== att.t || obt.q !== att.q || obt.t1 !== att.t1 || obt.sel.slice(0, obt.t).join() !== att.sel.join()) {
      echecs.push(`${nom} : oracle (t=${obt.t}/${att.t}, q=${obt.q}/${att.q})`);
    }
    for (const m of proprietes(sc, kMax, obt)) echecs.push(`${nom} : ${m}`);
  }
  const rnd = tirage(graine);
  for (let n = 0; n < nSweep; n++) {
    const { sc, kMax } = genererScenario(rnd);
    const r = await appeler(code, iface, 'essaiConstruire', [sc, kMax]);
    if (r.erreur) { echecs.push(`balayage#${n} : REVERT (${r.erreur})`); continue; }
    const obt = { sel: r.valeur[0].map((a) => ethers.getAddress(a)), t: Number(r.valeur[1]), q: Number(r.valeur[2]), t1: Number(r.valeur[3]) };
    const att = oracleConstruire(sc, kMax);
    if (obt.t !== att.t || obt.q !== att.q || obt.t1 !== att.t1 || obt.sel.slice(0, obt.t).join() !== att.sel.join()) {
      echecs.push(`balayage#${n} : oracle (t=${obt.t}/${att.t}, q=${obt.q}/${att.q}, kMax=${kMax})`);
    }
    for (const m of proprietes(sc, kMax, obt)) echecs.push(`balayage#${n} : ${m}`);
  }
  return echecs;
}

// =============================================================================
// LA SUITE
// =============================================================================

(async () => {
  const N_BALAYAGE = Number(process.env.CAS || 600);
  const GRAINE = BigInt(process.env.GRAINE || 20260902);

  console.log('\n\x1b[1mCoinbosa Chain — selection des validateurs (CoinbosaStake._construire)\x1b[0m');

  // ---------------------------------------------------------------------------
  titre('0. LE BANC D\'ESSAI');
  // Tout ce qui suit est sans valeur si le contrat teste n'est pas le contrat
  // reel, ou si l'oracle ne parle pas des memes predicats que lui. Ces gardes
  // sont fail-closed : elles font tomber la suite, pas un simple avertissement.
  // ---------------------------------------------------------------------------
  check('solc epingle a 0.8.26', solc.version().startsWith('0.8.26'), true);

  const sourceStake = fs.readFileSync(path.join(SRC, 'CoinbosaStake.sol'), 'utf8');
  const { harnais, stake } = compiler(sourceStake);
  const iface = new ethers.Interface(harnais.abi);
  const CODE = '0x' + harnais.evm.deployedBytecode.object;

  // Le bytecode de creation recompile ici doit etre IDENTIQUE, octet pour octet,
  // a l'artefact du depot : c'est ce qui garantit qu'on teste le code qui partira
  // au fork, et non une variante compilee avec d'autres reglages.
  const artefact = JSON.parse(fs.readFileSync(path.join(RACINE, 'build', 'CoinbosaStake.json'), 'utf8'));
  check('CoinbosaStake recompile identique a build/CoinbosaStake.json', stake.evm.bytecode.object === artefact.bytecode, true);
  check('le harnais ne change pas le bytecode de CoinbosaStake', compiler(sourceStake).stake.evm.bytecode.object === artefact.bytecode, true);
  check('la ligne de garde apparait exactement une fois', sourceStake.split(LIGNE_GARDE).length - 1, 1);
  check('VALIDATEUR_GENESE de l\'oracle == celui de la source', sourceStake.includes(`VALIDATEUR_GENESE = ${GENESE}`), true);
  check('EPOQUE de l\'oracle == celle de la source', sourceStake.includes('EPOQUE = 200'), true);
  check('MAX_PLACES de l\'oracle == celui de la source', sourceStake.includes('MAX_PLACES = 41'), true);
  check('TAILLE_CLASSEMENT de l\'oracle == celle de la source', sourceStake.includes('TAILLE_CLASSEMENT = 82'), true);

  if (!RPC) { note('aucun RPC fourni : demarrage d\'un noeud jetable sur le vrai genesis…'); await demarrerNoeud(); }
  const reseau = await rpc('eth_chainId', []);
  check('le noeud repond, chainId 26262', Number(BigInt(reseau.result)), 26262);
  const hauteur = await rpc('eth_blockNumber', []);
  check('le noeud est au bloc 0 et n\'a jamais mine', Number(BigInt(hauteur.result)), 0);
  // LE PIEGE N°1 de tout ce montage : si le noeud plafonne le gaz des eth_call
  // (--rpc.gascap, 50 000 000 par defaut), gasleft() ne peut pas depasser le
  // SEUIL_GAZ_SYSTEME de 1e12, `_estAppelSysteme()` refuse TOUJOURS, et la
  // section 6 vire au vert sans avoir rien execute. On mesure, on ne suppose pas.
  {
    const sonde = '0x00000000000000000000000000000000000C0DE2';
    const j = await rpc('eth_call', [
      { to: sonde, input: '0x', gas: '0x38d7ea4c68000' }, 'latest',
      { [sonde]: { code: '0x5a60005260206000f3' } }, // GAS ; MSTORE ; RETURN 32
    ]);
    const dispo = j.result ? BigInt(j.result) : 0n;
    check('le noeud accepte plus de 1e12 de gaz (--rpc.gascap 0)', dispo > 1000000000000n, true);
    if (dispo <= 1000000000000n) note(`  gasleft() mesure : ${dispo} — relance le noeud avec --rpc.gascap 0`);
  }
  note(`RPC ${RPC} — harnais pose par surcharge de code a ${HARNAIS} (${harnais.evm.deployedBytecode.object.length / 2} octets)`);

  // Croisement de l'oracle contre le contrat : si _vuProduire ou _eligible ne
  // veulent pas dire la meme chose des deux cotes, tout le reste ment.
  {
    const qui = [GENESE, adr(1), adr(2), adr(3), adr(4), adr(5), adr(6), ethers.ZeroAddress];
    const sc = scenario([
      genese(true),
      avere(1), dormant(2),
      membre({ a: adr(3), etat: EN_ATTENTE, dc: TS - DELAI_CANDIDATURE, classe: true }),        // juste admis
      membre({ a: adr(4), etat: EN_ATTENTE, dc: TS - DELAI_CANDIDATURE + 1n, classe: true }),   // une seconde trop tot
      membre({ a: adr(5), etat: ACTIF, enjeu: 999n, min: 1000n, classe: true }),                // sous son minimum d'admission
      membre({ a: adr(6), etat: BANNI, dbs: 150, classe: true }),
    ]);
    const r = await appeler(CODE, iface, 'essaiPredicats', [sc, qui]);
    check('essaiPredicats ne revert pas', r.erreur === undefined, true);
    const st = etat(sc);
    const vuC = r.valeur[0], eligC = r.valeur[1];
    check('_vuProduire : oracle et contrat d\'accord sur 8 adresses', qui.map((a, i) => vuC[i] === st.vuProduire(a)).every(Boolean), true);
    check('_eligible : oracle et contrat d\'accord sur 8 adresses', qui.map((a, i) => eligC[i] === st.eligible(a)).every(Boolean), true);
    const motsC = r.valeur[2].map(BigInt);
    check('le classement injecte est bien trie par mot decroissant', motsC.every((w, i) => i === 0 || motsC[i - 1] > w), true);
    check('l\'oracle reconstruit le meme classement que le contrat', motsC.join() === st.cl.slice(0, motsC.length).join(), true);
  }

  // Petit utilitaire local : un cas nomme de _construire.
  const construire = async (sc, kMax, bloc) => {
    const r = await appeler(CODE, iface, 'essaiConstruire', [sc, kMax], bloc);
    if (r.erreur) return { erreur: r.erreur };
    return { sel: r.valeur[0].map((a) => ethers.getAddress(a)), t: Number(r.valeur[1]), q: Number(r.valeur[2]), t1: Number(r.valeur[3]) };
  };

  // ---------------------------------------------------------------------------
  titre('1. LA GARDE, ISOLEE — _construire avec un kMax impose');
  note('kMax est donne a la main : c\'est la seule facon de savoir que c\'est bien 2q-1');
  note('qui a mordu, et non le rail nAnc+1 ou le plafond de 41 places.');
  // ---------------------------------------------------------------------------
  {
    // L'etat VIDE doit etre un etat initial valide : le contrat est pose par
    // SetCode au fork, sans constructeur, donc il demarre a zero partout.
    const r = await construire(scenario([]), 1);
    check('etat vide : le jeu vaut [genese] et rien d\'autre', r.t, 1);
    check('etat vide : aucun revert', r.erreur === undefined, true);
  }
  {
    // q == 0 ET des candidats qui attendent : LE cas du premier terme de la
    // garde. Sans lui, 2*0-1 panique et le bloc d'epoque devient improduisible.
    const sc = scenario([genese(false), avere(1, { elu: true, classe: false, etat: EN_DEBLOCAGE }), ...Array.from({ length: 40 }, (_, i) => dormant(10 + i))]);
    const r = await construire(sc, 41);
    check('q=0 avec 40 dormants et 41 places libres : t reste a 1', r.t, 1);
    check('q=0 : aucun revert (le terme q==0 fait son travail)', r.erreur === undefined, true);
    check('q=0 : q vaut bien 0', r.q, 0);
  }
  {
    // Le temoin. Sans lui, on ne saurait pas si la sensibilite mesuree en
    // section 8 vient de la garde ou d'un artefact du scenario.
    const r = await construire(scenario([genese(false), avere(1, { elu: true, classe: false, etat: EN_DEBLOCAGE })]), 41);
    check('temoin q=0 sans aucun dormant : t = 1', r.t, 1);
  }
  {
    // q = 1 : 2q-1 = 1, donc t+1 = 2 > 1 des le premier candidat. C'est la
    // reponse chiffree a « n'importe qui mise 1 000 BOSA et fige le reseau » :
    // avec un seul producteur, personne n'entre, jamais.
    const r = await construire(scenario([genese(true), dormant(1), dormant(2), dormant(3)]), 41);
    check('q=1 : aucun dormant n\'entre, meme avec 40 places libres', r.t, 1);
    check('q=1 : la part achetable par un non-producteur est nulle', r.t - r.q, 0);
  }
  {
    // q = 2 : la borne vaut 3, et elle est ATTEINTE, pas depassee.
    const r = await construire(scenario([genese(true), avere(1), dormant(2), dormant(3), dormant(4)]), 41);
    check('q=2 : t atteint exactement 2q-1 = 3', r.t, 3);
    check('q=2 : un seul dormant entre', r.t - r.q, 1);
    check('q=2 : quorum ⌊3/2⌋+1 = 2 <= q = 2', Math.floor(r.t / 2) + 1 <= r.q, true);
  }
  {
    const r = await construire(scenario([genese(true), avere(1), avere(2), ...Array.from({ length: 6 }, (_, i) => dormant(10 + i))]), 41);
    check('q=3 : t atteint exactement 2q-1 = 5', r.t, 5);
    check('q=3 : la part achetable vaut q-1 = 2', r.t - r.q, 2);
  }
  {
    // Le tableau complet des places achetables, a pleine taille : 41 places
    // exigent 21 producteurs reels — exactement le quorum de 41, marge zero.
    const sc = scenario([genese(true), ...Array.from({ length: 20 }, (_, i) => avere(1 + i, { enjeu: 900000n - BigInt(i) })), ...Array.from({ length: 40 }, (_, i) => dormant(100 + i, { enjeu: 500000n - BigInt(i) }))]);
    const r = await construire(sc, 41);
    check('41 places : t = 41', r.t, 41);
    check('41 places : il a fallu 21 averes', r.q, 21);
    check('41 places : la part maximale d\'un non-producteur est 20', r.t - r.q, 20);
    check('41 places : quorum 21 <= q 21, vivant a marge zero', Math.floor(r.t / 2) + 1 <= r.q, true);
  }
  {
    // Le plafond 2q-1 mord AVANT kMax : c'est la situation qu'il faut savoir
    // distinguer, sinon on croit tester la garde alors que c'est kMax qui a agi.
    const r = await construire(scenario([genese(true), avere(1), ...Array.from({ length: 5 }, (_, i) => dormant(10 + i))]), 11);
    check('kMax=11 mais q=2 : c\'est 2q-1 qui arrete, t = 3', r.t, 3);
    checkQue('t < kMax : la borne active est bien 2q-1', r.t < 11);
  }
  {
    // L'inverse : kMax mord avant la garde.
    const r = await construire(scenario([genese(true), avere(1), dormant(2), dormant(3)]), 2);
    check('kMax=2 avec q=2 : c\'est kMax qui arrete, t = 2', r.t, 2);
  }
  {
    // 20 averes, aucun dormant : t = q = 21. La garde ne s'exprime pas, le
    // passage 1 remplit seul. Verifie que la garde n'affame pas les producteurs.
    const r = await construire(scenario([genese(true), ...Array.from({ length: 20 }, (_, i) => avere(1 + i))]), 41);
    check('20 averes, aucun dormant : t = 21 et q = 21', `${r.t}/${r.q}`, '21/21');
  }
  {
    // Le classement sature de tres gros enjeux jamais vus produire — le scenario
    // que l'attaquant achete. Il n'obtient que q-1 places, quelle que soit sa mise.
    const sc = scenario([genese(true), avere(1, { enjeu: 1000n }), ...Array.from({ length: 60 }, (_, i) => dormant(100 + i, { enjeu: 5000000n - BigInt(i) }))]);
    const r = await construire(sc, 41);
    check('60 dormants a 5000x la mise : t plafonne a 2q-1 = 3', r.t, 3);
    check('60 dormants : l\'attaquant obtient q-1 = 1 place', r.t - r.q, 1);
    checkQue('60 dormants : le jeu reste vivant', Math.floor(r.t / 2) + 1 <= r.q);
  }

  // ---------------------------------------------------------------------------
  titre('2. LES FILTRES — ce qui ne doit JAMAIS entrer dans le jeu');
  note('Le classement est un vivier public : n\'importe qui peut y deposer. Les');
  note('filtres sont ce qui empeche le bruit d\'atteindre le consensus.');
  // ---------------------------------------------------------------------------
  {
    // La genese occupe la place 0 hors election. La laisser passer dans le
    // classement lui donnerait DEUX places, _ecrire refuserait tout le recalcul
    // (RecalculRefuse(3)) et le jeu se figerait pour toujours.
    const sc = scenario([membre({ a: GENESE, etat: ACTIF, enjeu: 9000000n, min: 0n, dbs: 150, elu: true, classe: true }), avere(1), dormant(2)]);
    const r = await construire(sc, 41);
    check('genese presente dans le classement : elle n\'y est pas reprise', r.sel.slice(0, r.t).filter((a) => a === GENESE).length, 1);
    checkQue('genese dans le classement : aucun doublon dans le jeu', new Set(r.sel.slice(0, r.t)).size === r.t);
  }
  {
    // Un mot dont les 160 bits bas valent 2^160-1 designe address(0).
    const sc = scenario([genese(true), avere(1)], { motsBruts: [(9999n << 160n) | M160, mot(adr(1), 5000n)] });
    const r = await construire(sc, 41);
    checkQue('mot pointant sur l\'adresse nulle : ignore', !r.sel.slice(0, r.t).includes(ethers.ZeroAddress));
    check('mot nul ignore : le reste du classement est quand meme servi', r.t, 2);
  }
  {
    // Les quatre etats qui excluent, chacun teste seul pour que l'echec designe
    // le coupable.
    for (const [nom, e] of [['EN_QUARANTAINE', EN_QUARANTAINE], ['EN_DEBLOCAGE', EN_DEBLOCAGE], ['BANNI', BANNI], ['INEXISTANT', INEXISTANT]]) {
      const sc = scenario([genese(true), avere(1), membre({ a: adr(2), etat: e, enjeu: 9000000n, min: 1000n, classe: true, dbs: 150 })]);
      const r = await construire(sc, 41);
      checkQue(`${nom} n\'entre jamais dans le jeu, meme avec le plus gros enjeu`, !r.sel.slice(0, r.t).includes(adr(2)));
    }
  }
  {
    // Le delai de candidature de 7 jours, teste a la seconde pres : c'est le
    // genre de borne ou un `<` mis pour un `<=` ne se voit jamais.
    const tot = scenario([genese(true), avere(1), membre({ a: adr(2), etat: EN_ATTENTE, dc: TS - DELAI_CANDIDATURE + 1n, enjeu: 9000n, min: 1000n, classe: true })]);
    const pile = scenario([genese(true), avere(1), membre({ a: adr(2), etat: EN_ATTENTE, dc: TS - DELAI_CANDIDATURE, enjeu: 9000n, min: 1000n, classe: true })]);
    checkQue('EN_ATTENTE a 7 jours moins une seconde : refuse', !(await construire(tot, 41)).sel.slice(0, 3).includes(adr(2)));
    checkQue('EN_ATTENTE a 7 jours pile : admis', (await construire(pile, 41)).sel.slice(0, 3).includes(adr(2)));
  }
  {
    // Non-retroactivite du minimum d'enjeu : l'eligibilite se teste contre
    // enjeuMinAdmission, jamais contre le minimum courant. Un enjeu tombe sous
    // son propre minimum d'admission exclut ; un minimum releve apres coup, non.
    const sc = scenario([genese(true), avere(1), membre({ a: adr(2), etat: ACTIF, enjeu: 999n, min: 1000n, classe: true })]);
    checkQue('enjeu sous son minimum d\'admission : exclu', !(await construire(sc, 41)).sel.slice(0, 3).includes(adr(2)));
    const sc2 = scenario([genese(true), avere(1), membre({ a: adr(2), etat: ACTIF, enjeu: 1000n, min: 1000n, classe: true })]);
    checkQue('enjeu egal a son minimum d\'admission : admis', (await construire(sc2, 41)).sel.slice(0, 3).includes(adr(2)));
  }
  {
    // Les gardes d'indice. Un nbClasses ou un nbElus hors bornes ferait paniquer
    // une boucle sur un tableau de taille fixe — et une panique sur le chemin de
    // consensus tue le noeud, elle ne se rattrape pas.
    const sc = scenario([genese(true), avere(1), dormant(2)], { nbClassesForce: 250 });
    const r = await construire(sc, 41);
    check('nbClasses = 250 : borne a 82, aucune panique', r.erreur === undefined, true);
    check('nbClasses = 250 : le resultat reste sain', r.t, 3);
    const sc2 = scenario([genese(true), avere(1), dormant(2)], { nbElusForce: 300 });
    const r2 = await construire(sc2, 41);
    check('nbElus = 300 : _construire ne le lit pas, aucune panique', r2.erreur === undefined, true);
  }

  // ---------------------------------------------------------------------------
  titre('3. LES BORNES DE _vuProduire — ou passe la frontiere « avere »');
  note('q se calcule entierement a partir de ce predicat. Chaque bascule change q,');
  note('donc change la borne 2q-1, donc change t : la frontiere est observable.');
  // ---------------------------------------------------------------------------
  {
    // La fenetre est `d != 0 && block.number >= d && block.number - d < 200`.
    // Ses deux bords sont observes a des hauteurs DIFFERENTES : au bloc 199 le
    // bord ancien n'existe pas (d = 0 veut deja dire « jamais vu »), il faut
    // monter au bloc 1000 pour que d = 800 tombe pile a 200 blocs d'ecart.
    const cas = [
      ['d = 0 : « jamais vu sceller », et non « vu au bloc 0 »', 199, 0, false],
      ['d = 1 au bloc 199 : le plus ancien observable ici', 199, 1, true],
      ['d = bloc courant : avere', 199, 199, true],
      ['d = bloc + 1 (dans le futur) : refuse par la garde block.number >= d', 199, 200, false],
      ['d = bloc - 199 au bloc 1000 : dernier bloc DANS la fenetre', 1000, 801, true],
      ['d = bloc - 200 au bloc 1000 : premier bloc HORS fenetre', 1000, 800, false],
    ];
    for (const [nom, bloc, d, attendu] of cas) {
      const sc = scenario([membre({ a: GENESE, etat: INEXISTANT, enjeu: 0n, min: 0n, elu: true, dbs: d }),
                           membre({ a: adr(1), etat: ACTIF, enjeu: 9000n, min: 1000n, dbs: d, classe: true })]);
      const r = await construire(sc, 41, bloc);
      // Les deux membres partagent le meme dernierBlocScelle : avere => q vaut 2,
      // non avere => q vaut 0. La bascule est donc lisible sur q seul.
      check(`${nom}`, r.q, attendu ? 2 : 0);
    }
    const sc = scenario([genese(false), dormant(1), dormant(2)]);
    check('genese a dernierBlocScelle = 0 : q vaut 0, pas 1', (await construire(sc, 41)).q, 0);
  }

  // ---------------------------------------------------------------------------
  titre('4. L\'AMORCAGE — il gonfle le plafond, jamais le quorum');
  note('L\'attestation d\'amorcage ajoute 1 a p, donc 2 a kMax. Elle n\'ajoute rien');
  note('a q. La garde doit donc contenir la place ainsi ouverte : une attestation');
  note('ne s\'achete pas en siege.');
  // ---------------------------------------------------------------------------
  {
    const sc = scenario([genese(true), dormant(1)], { atteste: adr(1), attesteDepuis: 100 });
    const k = oracleKMax(sc);
    check('attestation active : p passe de 1 a 2', k.p, 2);
    check('attestation active : kMax passe de 1 a 2', k.kMax, 2);
    const r = await construire(sc, k.kMax);
    check('mais q reste a 1, donc la garde tient : t = 1', r.t, 1);
    note('le commentaire de _amorcageCompte annonce une « sortie honnete » de l\'impasse');
    note('1 -> 2 ; cote contrat elle est INERTE tant que q vaut 1. C\'est un constat,');
    note('pas un echec : la seule croissance possible passe par un scellage reel.');
  }
  {
    // L'attesté qui produit pour de vrai est deja compte dans pReel : garder
    // l'attestation le compterait deux fois et gonflerait kMax d'un cran de trop.
    const sc = scenario([genese(true), avere(1, { elu: true })], { atteste: adr(1), attesteDepuis: 100 });
    check('attesté qui scelle vraiment : l\'amorcage ne le compte pas deux fois', oracleKMax(sc).amorcage, 0);
  }
  {
    const sc = scenario([genese(true), dormant(1)], { atteste: adr(1), attesteDepuis: 0 });
    // block.number 199 <= 0 + 400 : encore valide. A l'inverse, une attestation
    // de plus de deux epoques expire.
    check('attestation dans sa fenetre de deux epoques : comptee', oracleKMax(sc).amorcage, 1);
  }

  // ---------------------------------------------------------------------------
  titre('5. LE PIPELINE D\'EPOQUE COMPLET — _recalculer, evenements compris');
  note('Ici kMax n\'est plus impose : il est calcule par le contrat. Les evenements');
  note('sont le temoignage du contrat sur son propre calcul.');
  // ---------------------------------------------------------------------------
  const recalculer = async (sc) => {
    const r = await tracer(CODE, iface, 'essaiRecalculer', [sc]);
    if (r.erreur) return { erreur: r.erreur, evenements: r.evenements };
    return { bilan: lireBilan(r.valeur[0]), evenements: r.evenements, gaz: r.gaz };
  };
  {
    // Demarrage a froid : l'etat vide est un etat initial VALIDE. Le contrat
    // etant pose par SetCode sans constructeur, c'est le premier etat reel.
    const r = await recalculer(scenario([]));
    check('demarrage a froid : aucun revert', r.erreur === undefined, true);
    check('demarrage a froid : JeuRecalcule(1,1,1)', r.evenements.map((e) => `${e.nom}(${e.args})`).join(), 'JeuRecalcule(1,1,1)');
    check('demarrage a froid : le jeu vaut [genese]', r.bilan.jeu.join(), GENESE);
    check('demarrage a froid : la cle de vote fait 48 octets', (r.bilan.votes[0].length - 2) / 2, 48);
  }
  {
    // Le rail de croissance : +1 place par epoque, jamais plus. Un saut de 1 a 5
    // places allumerait quatre noeuds d'un coup, et personne ne constaterait
    // l'echec avant les 200 blocs suivants. Ici trois titulaires produisent
    // (p = 3, donc 2p-1 = 5 places autorisees par l'invariant) et vingt
    // candidats AVERES et eligibles attendent : c'est nAnc+1 qui tranche.
    const sc = scenario([
      genese(true),
      avere(1, { elu: true, enjeu: 900000n }),
      avere(2, { elu: true, enjeu: 899999n }),
      ...Array.from({ length: 20 }, (_, i) => avere(10 + i, { enjeu: 500000n - BigInt(i) })),
    ]);
    const k = oracleKMax(sc);
    check('trois producteurs : l\'invariant autoriserait 2p-1 = 5 places', 2 * k.p - 1, 5);
    check('mais le rail de croissance borne kMax a nAnc+1 = 4', k.kMax, 4);
    const r = await recalculer(sc);
    check('20 candidats averes au portillon : le jeu n\'en prend qu\'UN', r.bilan.nbApres, 4);
    check('croissance bornee : JeuRecalcule(4,3,4)', r.evenements.map((e) => `${e.nom}(${e.args})`).join(), 'AmorcageTermine(3),JeuRecalcule(4,3,4)');
    checkQue('croissance bornee : le jeu reste vivant', Math.floor(r.bilan.nbApres / 2) + 1 <= r.bilan.q);
    // Bonus non cherche, mais qu'il faut figer : a trois producteurs REELS
    // l'amorcage se referme, definitivement. Le conseil d'amorcage n'a plus
    // aucun levier a partir de ce recalcul — c'est la clause d'extinction du
    // seul pouvoir discretionnaire du contrat, et elle se declenche toute seule.
    check('a trois producteurs reels, l\'amorcage se referme de lui-meme', r.evenements[0].nom, 'AmorcageTermine');
  }
  {
    // Croissance legitime : sans ce cas, toute la suite passerait sur un contrat
    // qui renverrait eternellement [genese].
    const sc = scenario([genese(true), avere(1, { elu: true }), dormant(2), dormant(3)]);
    const r = await recalculer(sc);
    check('croissance legitime : JeuRecalcule(3,2,3)', r.evenements.map((e) => `${e.nom}(${e.args})`).join(), 'JeuRecalcule(3,2,3)');
    check('croissance legitime : le jeu passe a 3', r.bilan.nbApres, 3);
    checkQue('croissance legitime : quorum 2 <= q 2, vivant', Math.floor(r.bilan.nbApres / 2) + 1 <= r.bilan.q);
  }
  {
    // Le jeu ne bouge pas : RecalculInchange, et le cache precedent est garde.
    const sc = scenario([genese(true), ...Array.from({ length: 60 }, (_, i) => dormant(100 + i, { enjeu: 900000n - BigInt(i) }))]);
    const r = await recalculer(sc);
    check('classement sature, un seul producteur : RecalculInchange(1)', r.evenements.map((e) => `${e.nom}(${e.args})`).join(), 'RecalculInchange(1)');
    check('classement sature : le jeu reste a 1', r.bilan.nbApres, 1);
  }
  {
    // Un titulaire avere mais devenu ineligible : il compte dans pReel (donc
    // gonfle kMax) et ne peut pas compter dans q. C'est l'ecart exact que la
    // garde absorbe, et c'est un etat qu'un simple demanderRetrait() produit.
    const sc = scenario([genese(true), avere(1, { elu: true, classe: false, etat: EN_DEBLOCAGE }), dormant(2), dormant(3)]);
    const k = oracleKMax(sc);
    check('titulaire avere devenu ineligible : pReel = 2 donc kMax = 3', `${k.pReel}/${k.kMax}`, '2/3');
    const r = await recalculer(sc);
    check('mais q = 1 : le jeu RETRECIT a 1 au lieu de grandir a 3', r.bilan.nbApres, 1);
    check('retrecissement : JeuRecalcule(1,2,3)', r.evenements.map((e) => `${e.nom}(${e.args})`).join(), 'JeuRecalcule(1,2,3)');
    checkQue('retrecir n\'abaisse jamais la disponibilite : quorum 1 <= q 1', Math.floor(r.bilan.nbApres / 2) + 1 <= r.bilan.q);
  }
  {
    // q = 0 sur le pipeline complet, avec des candidats qui attendent.
    const sc = scenario([genese(false), avere(1, { elu: true, classe: false, etat: EN_DEBLOCAGE }), avere(2, { elu: true, classe: false, etat: EN_DEBLOCAGE }), ...Array.from({ length: 10 }, (_, i) => dormant(10 + i))]);
    const r = await recalculer(sc);
    check('q=0 sur le pipeline complet : aucun revert', r.erreur === undefined, true);
    check('q=0 : le jeu retombe sur [genese]', r.bilan.nbApres, 1);
  }
  {
    // La quarantaine s'evalue AVANT _construire : un membre a 50 absences
    // consecutives sort du vivier dans le meme recalcul.
    const sc = scenario([genese(true), avere(1, { elu: true, abs: SEUIL_QUARANTAINE })]);
    const r = await recalculer(sc);
    check('50 absences : mise en quarantaine emise', r.evenements.some((e) => e.nom === 'MiseEnQuarantaine'), true);
    checkQue('quarantaine : le mis en quarantaine ne figure plus au jeu', !r.bilan.jeu.includes(adr(1)));
  }
  {
    // Ce que le client lit vraiment. Un doublon ne fait pas revert le contrat :
    // il fait paniquer le noeud Go dans voteAddrMap, et tous les noeuds tombent
    // ensemble puisqu'ils lisent le meme etat.
    const sc = scenario([genese(true), ...Array.from({ length: 20 }, (_, i) => avere(1 + i, { elu: true })), ...Array.from({ length: 20 }, (_, i) => dormant(100 + i))]);
    const r = await recalculer(sc);
    check('jeu a 21 elus : getMiningValidators rend autant de cles que d\'adresses', r.bilan.jeu.length === r.bilan.votes.length, true);
    checkQue('toutes les cles de vote font exactement 48 octets', r.bilan.votes.every((v) => (v.length - 2) / 2 === 48));
    checkQue('aucun doublon dans ce que lit le client', new Set(r.bilan.jeu).size === r.bilan.jeu.length);
    checkQue('aucune adresse nulle dans ce que lit le client', !r.bilan.jeu.includes(ethers.ZeroAddress));
    check('la place 0 est le validateur de genese', r.bilan.jeu[0], GENESE);
  }

  // ---------------------------------------------------------------------------
  titre('6. LE VRAI CHEMIN SYSTEME — enregistrerScellage() au bloc 199');
  note('Les sections 1 a 5 atteignent _construire par le harnais. Celle-ci prouve');
  note('que le contrat reel y arrive tout seul : trois gardes systeme franchies');
  note('pour de vrai, un bloc simule au numero 199, aucune fonction contournee.');
  // ---------------------------------------------------------------------------
  {
    const sc = scenario([genese(false), avere(1, { elu: true }), dormant(2)]);
    // La genese est le scelleur : enregistrerScellage() ecrit son
    // dernierBlocScelle AVANT de declencher le recalcul, donc elle devient averee
    // dans le calcul meme. Le choix du scelleur est un levier de scenario, pas
    // un detail : c'est la seule facon HONNETE de faire varier q de 0 a 1.
    const r = await simulerEpoque(CODE, iface, sc, GENESE, [GENESE, adr(1)]);
    check('le bloc simule est bien le 199 (bloc d\'epoque)', r.numero, 199);
    check('la transaction systeme reussit', r.statut, '0x1');
    check('_recalculer est reellement atteinte : JeuRecalcule(3,2,3)', r.evenements.map((e) => `${e.nom}(${e.args})`).join(), 'JeuRecalcule(3,2,3)');
    check('le scelleur du bloc est compte comme producteur : pReel = 2', r.bilan.pReel, 2);
    check('le jeu ecrit compte 3 membres', r.bilan.nbApres, 3);
    checkQue('quorum tenable sur le chemin reel', Math.floor(r.bilan.nbApres / 2) + 1 <= r.bilan.q);
    note(`gaz de la transaction systeme au bloc d'epoque : ${r.gaz}`);
  }
  {
    // q = 0 par le vrai chemin : le scelleur du bloc n'est PAS eligible, la
    // genese n'a jamais produit. C'est l'etat exact ou le premier terme de la
    // garde est le seul rempart entre la chaine et un bloc improduisible.
    const scelleur = adr(1);
    const sc = scenario([genese(false), avere(1, { elu: true, classe: false, etat: EN_DEBLOCAGE }), ...Array.from({ length: 5 }, (_, i) => dormant(10 + i))]);
    const r = await simulerEpoque(CODE, iface, sc, scelleur, [GENESE, scelleur]);
    check('q=0 par le vrai chemin : la transaction systeme NE REVERT PAS', r.statut, '0x1');
    check('q=0 par le vrai chemin : le jeu retombe sur [genese]', r.bilan.nbApres, 1);
    check('q=0 par le vrai chemin : q vaut 0', r.bilan.q, 0);
    note('c\'est ce cas, et lui seul, que le mutant de la section 8 fait exploser.');
  }
  {
    // Le COUT REEL du bloc d'epoque, mesure sur la transaction systeme elle-meme
    // et non sur l'appel du harnais (qui paierait en plus l'ecriture du
    // scenario). Un _recalculer qui depasserait la limite de gaz du bloc serait
    // un arret de chaine de plus, et il n'apparaitrait dans aucun test de logique.
    const membres = [genese(true), ...Array.from({ length: 20 }, (_, i) => avere(1 + i, { elu: true, enjeu: 900000n - BigInt(i) }))];
    for (let i = 0; i < 62; i++) membres.push(dormant(100 + i, { enjeu: 500000n - BigInt(i) }));
    const sc = scenario(membres);
    const anc = [GENESE, ...Array.from({ length: 20 }, (_, i) => adr(1 + i))];
    const r = await simulerEpoque(CODE, iface, sc, GENESE, anc);
    check('cas le plus lourd (21 elus, 82 classes) : la transaction systeme reussit', r.statut, '0x1');
    note(`gaz de la transaction systeme, cas le plus lourd : ${r.gaz} (limite du bloc : 40 000 000)`);
    checkQue('le bloc d\'epoque le plus lourd tient dans la limite de gaz du bloc', r.gaz < 40000000);
    checkQue('cas le plus lourd : le quorum reste tenable', Math.floor(r.bilan.nbApres / 2) + 1 <= r.bilan.q);
  }
  {
    // Les gardes systeme ne sont pas decoratives : un appel qui n'en franchit
    // pas les trois rend la main sans rien ecrire, et le dit dans le journal.
    const sc = scenario([genese(true), avere(1, { elu: true }), dormant(2)]);
    const r = await simulerEpoque(CODE, iface, sc, GENESE, [GENESE, adr(1)], '0x2000000');
    check('gaz sous 1e12 : l\'appel n\'est pas reconnu comme systeme', r.evenements.map((e) => e.nom).join(), 'SanctionIgnoree');
    check('gaz sous 1e12 : aucun recalcul, le cache precedent est intact', r.bilan.nbApres, 2);
  }

  // ---------------------------------------------------------------------------
  titre(`7. BALAYAGE — ${N_BALAYAGE} etats tires au sort, oracle exact + 6 proprietes`);
  note('Generateur STRATIFIE : un tirage uniforme ne visiterait jamais q <= 1, la');
  note(`seule zone ou la garde decide. Graine ${GRAINE} — rejouable a l'identique.`);
  // ---------------------------------------------------------------------------
  {
    const t0 = Date.now();
    const rnd = tirage(GRAINE);
    const couverture = { q0: 0, q0AvecCandidats: 0, geneseHorsLigne: 0, passage2: 0, borneAtteinte: 0, kMaxAtteint: 0, t1SuperieurBorne: 0 };
    let reverts = 0, oracleFaux = 0;
    const manquesTotaux = {};
    let premierEcart = null;

    for (let n = 0; n < N_BALAYAGE; n++) {
      const { sc, kMax } = genererScenario(rnd);
      const r = await construire(sc, kMax);
      if (r.erreur) { reverts++; if (!premierEcart) premierEcart = `revert : ${r.erreur}`; continue; }
      const att = oracleConstruire(sc, kMax);
      if (r.t !== att.t || r.q !== att.q || r.t1 !== att.t1 || r.sel.slice(0, r.t).join() !== att.sel.join()) {
        oracleFaux++;
        if (!premierEcart) premierEcart = `oracle : t=${r.t}/${att.t} q=${r.q}/${att.q} t1=${r.t1}/${att.t1} kMax=${kMax}`;
      }
      for (const m of proprietes(sc, kMax, r)) manquesTotaux[m] = (manquesTotaux[m] || 0) + 1;

      const st = etat(sc);
      if (r.q === 0) couverture.q0++;
      if (r.q === 0 && sc.membres.some((m) => m.classe && st.eligible(m.adresse) && !st.vuProduire(m.adresse))) couverture.q0AvecCandidats++;
      if (!st.vuProduire(GENESE)) couverture.geneseHorsLigne++;
      if (r.t > r.t1) couverture.passage2++;
      if (r.q > 0 && r.t === 2 * r.q - 1) couverture.borneAtteinte++;
      if (r.t === kMax) couverture.kMaxAtteint++;
      if (r.t1 > 2 * r.q - 1) couverture.t1SuperieurBorne++;
    }

    check(`${N_BALAYAGE} etats : aucun revert sur le chemin de consensus`, reverts, 0);
    check(`${N_BALAYAGE} etats : le jeu construit est exactement celui de l'oracle`, oracleFaux, 0);
    for (const p of ['P1-forme', 'P2-q0', 'P3-passage2', 'P4-invariant', 'P5-quorum', 'P6-passage1']) {
      check(`${N_BALAYAGE} etats : ${p}`, manquesTotaux[p] || 0, 0);
    }
    if (premierEcart) note(`premier ecart : ${premierEcart}`);
    note(`couverture — q=0 : ${couverture.q0} etats, dont ${couverture.q0AvecCandidats} avec des candidats qui attendent`);
    note(`couverture — genese hors ligne : ${couverture.geneseHorsLigne} · passage 2 actif : ${couverture.passage2}`);
    note(`couverture — borne 2q-1 atteinte : ${couverture.borneAtteinte} · kMax atteint : ${couverture.kMaxAtteint}`);
    note(`couverture — t1 deja au-dela de 2q-1 (cf. section 9) : ${couverture.t1SuperieurBorne}`);
    checkQue('le balayage a bien visite q = 0 avec des candidats au portillon', couverture.q0AvecCandidats > 0);
    checkQue('le balayage a bien fait travailler le passage 2', couverture.passage2 > 0);
    checkQue('le balayage a bien touche la borne 2q-1', couverture.borneAtteinte > 0);
    note(`duree du balayage : ${((Date.now() - t0) / 1000).toFixed(1)} s`);
  }

  // ---------------------------------------------------------------------------
  titre('8. MUTATION — la suite mord-elle vraiment ?');
  note('Une suite verte ne prouve rien tant qu\'on n\'a pas montre qu\'elle sait');
  note('virer au rouge. On mute la ligne de garde EN MEMOIRE, on recompile avec le');
  note('meme solc, et on rejoue LA CAMPAGNE — pas des sondes taillees sur mesure.');
  note('contracts/CoinbosaStake.sol et build/ ne sont jamais touches.');
  // ---------------------------------------------------------------------------
  {
    const mutants = [
      ['M1  garde entierement supprimee', ''],
      ['M2  terme « q == 0 » retire', '            if (t + 1 > 2 * q - 1) break;'],
      ['M3  ordre du || inverse', '            if (t + 1 > 2 * q - 1 || q == 0) break;'],
      ['M4  « > » remplace par « >= » (jeu trop petit d\'une place)', '            if (q == 0 || t + 1 >= 2 * q - 1) break;'],
      ['M5  « 2q-1 » remplace par « 2q+1 » (une place de trop)', '            if (q == 0 || t + 1 > 2 * q + 1) break;'],
    ];

    // Le code sain doit d'abord rendre ZERO echec sur exactement la meme
    // campagne : sans cette mesure de reference, « le mutant echoue » ne veut rien dire.
    const echecsSains = await campagne(CODE, iface, 120, GRAINE + 1n);
    check('code sain : la campagne de mutation ne trouve aucun echec', echecsSains.length, 0);
    if (echecsSains.length) note('  ' + echecsSains.slice(0, 3).join(' | '));

    for (const [nom, remplacement] of mutants) {
      const mutantSrc = sourceStake.replace(LIGNE_GARDE, remplacement);
      check(`${nom} : la mutation a bien ete appliquee`, mutantSrc !== sourceStake, true);
      const codeMut = '0x' + compiler(mutantSrc).harnais.evm.deployedBytecode.object;
      check(`${nom} : le bytecode mute differe du sain`, codeMut !== CODE, true);
      const echecs = await campagne(codeMut, iface, 120, GRAINE + 1n);
      check(`${nom} : la suite le TUE`, echecs.length > 0, true);
      const familles = [...new Set(echecs.map((e) => e.split(' : ')[1].split(' ')[0]))].slice(0, 4);
      note(`  ${echecs.length} echec(s), premieres familles : ${familles.join(', ')}`);
    }
    note('M3 merite une phrase : inverser l\'ordre du || suffit a ramener le revert,');
    note('parce que Solidity evalue le membre de GAUCHE d\'abord. « q == 0 en premier »');
    note('n\'est donc pas une coquetterie d\'ecriture, c\'est la ligne de defense.');
    note('M4 n\'est tue par AUCUNE propriete de surete — seul l\'oracle exact le voit :');
    note('un contrat trop prudent passe tous les tests de surete et fige la chaine.');
  }

  // ---------------------------------------------------------------------------
  titre('9. CONSTATS — ce que la suite met au jour, et qu\'elle FIGE');
  note('Les verifications ci-dessous ne valident pas un comportement : elles');
  note('epinglent un comportement MESURE, pour qu\'aucun changement ne passe');
  note('inapercu. Chacune designe un ecart entre ce que le contrat dit et ce');
  note('qu\'il fait. Elles sont vertes parce qu\'elles decrivent, pas parce que');
  note('tout va bien.');
  // ---------------------------------------------------------------------------
  {
    // CONSTAT 1 — le commentaire ligne ~721 se trompe sur le mecanisme.
    // « 2 * q - 1 sur un uint256 deborde et rend un plafond astronomique » decrit
    // un comportement `unchecked`. Ce code n'est pas dans un bloc unchecked : en
    // solc 0.8.26 l'arithmetique est VERIFIEE, et 2*0-1 PANIQUE (Panic 0x11).
    // La garde est donc encore plus indispensable que le commentaire ne le dit —
    // ce n'est pas un jeu ingerable qu'elle evite, c'est un bloc improduisible.
    const codeM2 = '0x' + compiler(sourceStake.replace(LIGNE_GARDE, '            if (t + 1 > 2 * q - 1) break;')).harnais.evm.deployedBytecode.object;
    const sc = scenario([genese(false), avere(1, { elu: true, classe: false, etat: EN_DEBLOCAGE }), dormant(2)]);
    const r = await appeler(codeM2, iface, 'essaiConstruire', [sc, 41]);
    checkQue('sans le terme q==0, 2*q-1 ne « deborde » pas : il REVERT (Panic 0x11)', String(r.erreur || '').includes('underflow or overflow'));
    const rSain = await appeler(CODE, iface, 'essaiConstruire', [sc, 41]);
    check('avec le terme q==0, le meme etat passe sans encombre', rSain.erreur === undefined, true);
    note('a corriger dans le commentaire du contrat (pas dans le code : la garde est juste).');
  }
  {
    // CONSTAT 2 — le PASSAGE 1 ne porte aucune garde, et son commentaire affirme
    // que l'invariant « se conserve de lui-meme ». C'est vrai SEULEMENT si la
    // genese produit. Si elle est hors ligne depuis 200 blocs, on part de
    // t=1, q=0, et le premier ajove du passage 1 donne t=2, q=1 : 2 <= 1 est faux.
    const sc = scenario([genese(false), avere(1, { elu: true }), avere(2, { elu: true, classe: false, etat: EN_DEBLOCAGE }), dormant(3)]);
    const r = await construire(sc, 3);
    check('genese hors ligne + un avere eligible : t = 2', r.t, 2);
    check('… avec q = 1, donc t1 = 2 depasse la borne 2q-1 = 1', `${r.t1}/${2 * r.q - 1}`, '2/1');
    checkQue('… et le quorum de 2 n\'a qu\'UN producteur : jeu non soutenable', Math.floor(r.t / 2) + 1 > r.q);
    note('la cause n\'est PAS la garde du passage 2 : c\'est la place 0, accordee');
    note('sans condition a la genese. Le contrat l\'assume (rendre un jeu sans elle');
    note('reviendrait a nommer des validateurs dont personne n\'a la cle), mais la');
    note('consequence n\'est ecrite nulle part : quand la genese est hors ligne,');
    note('l\'invariant ne tient plus, et c\'est le passage 1 qui l\'enfreint.');
    note('C\'est pour cela que la propriete P3 est formulee « t <= max(t1, 2q-1) » :');
    note('la garde repond du passage 2, pas de la place 0.');
  }
  {
    // CONSTAT 3 — _remplacer teste sa marge contre p (les producteurs de
    // l'ANCIEN jeu) et non contre q (ceux du NOUVEAU). La garde est exacte si et
    // seulement si p <= q, et rien ne le garantit : il suffit d'un titulaire qui
    // a scelle cette epoque puis demande son retrait. Il compte dans pReel,
    // calcule avant ; il ne compte pas dans q, car EN_DEBLOCAGE n'est pas eligible.
    const membres = [
      genese(true),
      avere(1, { elu: true, enjeu: 1000n }),
      avere(2, { elu: true, enjeu: 1000n }),
      avere(3, { elu: true, classe: false, etat: EN_DEBLOCAGE, enjeu: 1000n }), // a demande son retrait
      dormant(4, { elu: true, classe: false, enjeu: 1000n }),
    ];
    for (let i = 0; i < 40; i++) membres.push(dormant(100 + i, { enjeu: 5000n }));
    const sc = scenario(membres);
    const k = oracleKMax(sc);
    const avantRemplacement = await construire(sc, k.kMax);
    const apres = await recalculer(sc);
    check('_construire rend un jeu SOUTENABLE : t = 5, q = 3', `${avantRemplacement.t}/${avantRemplacement.q}`, '5/3');
    checkQue('… quorum 3 <= q 3 : la chaine tiendrait', Math.floor(avantRemplacement.t / 2) + 1 <= avantRemplacement.q);
    check('_remplacer permute ensuite un avere contre un candidat jamais vu', apres.bilan.nbApres, 5);
    check('… et q tombe de 3 a 2', apres.bilan.q, 2);
    checkQue('… le quorum de 3 n\'a plus que 2 producteurs : jeu NON soutenable', Math.floor(apres.bilan.nbApres / 2) + 1 > apres.bilan.q);
    note('la marge de _remplacer (t <= 2p-3) mesure la mauvaise grandeur. L\'echange');
    note('remplace un avere du NOUVEAU jeu : c\'est q qui doit encaisser, pas p.');
    note('l\'attaquant n\'a besoin d\'aucun complice — une surenchere de 5 %, et');
    note('qu\'un producteur quitte le jeu dans la meme epoque. Cet evenement est public.');
    note('HORS PERIMETRE de l\'invariant teste ici : _construire fait son travail.');
    note('Piste : passer q a _remplacer et exiger t <= 2*min(p,q)-3, ou n\'autoriser');
    note('l\'echange que si l\'entrant est lui-meme _vuProduire.');
  }

  // ---------------------------------------------------------------------------
  titre('CE QUE CETTE SUITE NE COUVRE PAS');
  note('· Parlia lui-meme. On prouve que le contrat ne DEMANDE jamais un jeu qui');
  note('  casse le quorum ; on n\'execute pas le consensus. minerHistoryCheckLen et');
  note('  TurnLength sont poses en hypothese, et le quorum est recalcule ici comme');
  note('  ⌊N/2⌋+1, ce qui suppose TurnLength = 1. Si Bohr est active un jour, ces');
  note('  affirmations deviennent fausses EN SILENCE. TestCoinbosaAddSecondValidator-');
  note('  HaltsChain reste le seul lien avec le vrai moteur, et n\'est pas remplace.');
  note('· L\'atteignabilite des etats. Les scenarios sont ecrits directement en');
  note('  memoire du contrat. Aucun n\'est prouve atteignable par une suite de');
  note('  transactions publiques (verifie a la main pour les constats 2 et 3 seuls).');
  note('  Sur-approximation assumee : on teste PLUS d\'etats que la realite n\'en');
  note('  produit, jamais moins — donc jamais un faux vert.');
  note('· Le temps. Un seul bloc d\'epoque, block.number fige a 199. Rien sur les');
  note('  enchainements : montee place par place jusqu\'a 41, retrecissement apres');
  note('  pannes en serie, expiration d\'attestation, quarantaine sur 30 jours,');
  note('  deblocage a 49 jours.');
  note('· Le classement lui-meme : il est injecte deja trie. Un defaut dans');
  note('  _inserer, _retirerDuClassement ou la surenchere de 5 % reste invisible.');
  note('· Tout l\'argent : depot, retrait, slash, purge, primes, double signature,');
  note('  gouvernance du minimum, unicite des cles BLS. C\'est la que vivent les');
  note('  pires bugs d\'un contrat d\'enjeu, et cette suite n\'en dit rien.');
  note('· Le gaz : mesure a 21 elus et 82 classes (section 6), pas a 41 elus avec');
  note('  quarantaines et repousses de deblocage en cascade. La marge est large,');
  note('  mais le pire cas n\'est pas atteint.');
  note('· Un seul compilateur, une seule cible EVM : 0.8.26 / shanghai. C\'est voulu');
  note('  — le bytecode fixe l\'identite de la chaine — mais cela ne dit rien du');
  note('  comportement ailleurs.');

  // --- Bilan ---
  console.log(`\n${'='.repeat(64)}`);
  console.log(`  ${pass} tests reussis, ${fail} echec(s)`);
  console.log('='.repeat(64));
  try {
    fs.writeFileSync(path.join(RACINE, 'build', 'test-selection-validateurs.json'),
      JSON.stringify({ graine: String(GRAINE), balayage: N_BALAYAGE, pass, fail, results }, null, 2));
  } catch { /* build/ absent : le compte-rendu console suffit */ }
  arreterNoeud();
  process.exit(fail ? 1 : 0);
})().catch((e) => {
  console.error('\nERREUR FATALE :', e.message);
  arreterNoeud();
  process.exit(1);
});
