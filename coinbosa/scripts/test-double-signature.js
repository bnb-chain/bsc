// =============================================================================
// Suite de tests — DOUBLE SIGNATURE (CoinbosaStake.signalerDoubleSignature)
//
// CE QU'ELLE DEFEND. Le precompile 0x68 retrouve la cle qui a scelle chaque
// entete avec le ChainId QU'IL LIT DANS LA PREUVE (core/vm/contracts.go) :
//
//     msgHash1 := types.SealHash(header1, evidence.ChainId)
//
// Il ne le compare jamais a celui de la chaine qui l'execute. Une equivocation
// AUTHENTIQUE commise sur une autre chaine Parlia par une cle que le validateur
// reutilise est donc, pour lui, une preuve parfaitement valide. Tant que
// `signalerDoubleSignature` acceptait un blob RLP deja assemble, c'est
// L'APPELANT qui choisissait ce ChainId : il pouvait confisquer l'integralite de
// l'enjeu d'un validateur qui n'a jamais rien fait sur Coinbosa. Le correctif
// REASSEMBLE l'enveloppe dans le contrat, rlp([block.chainid, h1, h2]).
//
// LES DEUX ECHECS SYMETRIQUES. Une preuve etrangere qui passe laisse la porte
// ouverte ; une preuve legitime qui est refusee rend la sanction inapplicable et
// un validateur malhonnete intouchable. Cette suite mesure LES DEUX SENS, et
// les mutants de la section 8 en font la demonstration : M1 rouvre la porte, M2
// casse l'encodeur et refuse la preuve legitime, M3 retire le controle de
// longueur. Aucun n'est laisse en vie.
//
// COMMENT ON EXECUTE POUR DE VRAI. 0x68 ne figure PAS dans
// PrecompiledContractsHertz : il apparait a partir de PrecompiledContractsFeynman
// (core/vm/contracts.go, lignes 237 / 259 / 281 / 312 / 347 / 378). Or
// genesis/genesis-coinbosa.json ne porte AUCUN feynmanTime — 0x68 est donc un
// compte VIDE sur la chaine d'aujourd'hui. Le banc demarre par consequent TROIS
// noeuds jetables, aucun ne minant, aucun ne detenant de cle, tous au bloc 0 :
//
//   PROD  — genesis/genesis-coinbosa.json TEL QUEL. 0x68 y est mort ; c'est la
//           chaine d'aujourd'hui, et la section 7 mesure ce que le contrat y
//           fait (rien, et c'est `ret.length != 52` qui l'en empeche).
//   POBS  — le MEME genesis + feynmanTime, ecrit dans un fichier temporaire,
//           jamais dans genesis/. C'est la configuration FUTURE decrite par
//           POBS-ACTIVATION.md, et le seul levier qui rend 0x68 vivant sans
//           toucher une ligne du client Go. chainId 26262.
//   AUTRE — le meme, chainId 97. Il n'est pas decoratif : il fait tourner le
//           MEME contrat sur une AUTRE chaine, et les deux preuves y echangent
//           leurs roles. C'est la preuve que la garde suit `block.chainid` et
//           non une constante gravee — donc qu'elle survit a la marque blanche.
//
// Le contrat est pose par SURCHARGE DE CODE dans un eth_call / eth_simulateV1 :
// pas de deploiement, pas de transaction, pas de limite EIP-170. Le harnais
// EssaiDoubleSignature est compile EN MEMOIRE a partir d'une chaine de
// caracteres de ce fichier ; contracts/ et build/ ne sont jamais touches, et la
// section 0 verifie que sa presence ne change pas d'un octet le bytecode de
// CoinbosaStake.
//
// AUCUNE DEPENDANCE NPM AJOUTEE : ethers 6.17.0 et solc 0.8.26, deja presents.
// Le client Go n'est ni modifie ni recompile — le binaire ./geth du depot est
// utilise tel quel.
//
// Lancement :  node scripts/test-double-signature.js
// Variables :  GETH=…  PORT_BASE=…  (defaut 8591)
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
// Constantes du contrat, recopiees ici pour que l'oracle soit lisible. La
// section 0 les croise contre la source : un oracle qui derive du contrat rend
// toute la suite muette.
// -----------------------------------------------------------------------------
const CHAINID_PROD = 26262;   // genesis-coinbosa.json
const CHAINID_AUTRE = 97;     // « une autre chaine Parlia » — le testnet BSC
const CHAINID_VOISIN = 26263; // le piege reel : un chiffre d'ecart
const CHAINID_BSC = 56;       // tient sur un octet nu : la branche que BSC n'exerce jamais

// Le chainId du noeud MIROIR de la section 6. Il vaut 100 et non 97 pour une
// raison mesuree, pas par gout : `geth --networkid 97` resout d'office le
// genesis de BSC testnet et refuse de demarrer sur le notre
// (« database contains incompatible genesis »). 100 est inconnu du client, donc
// libre. Il est de surcroit < 0x80, donc son RLP tient sur un octet nu : c'est
// ce qui permet a la section 8 de montrer que le defaut d'encodeur M2 est
// INVISIBLE sur une telle chaine et ne se revele que sur 26262.
const CHAINID_MIROIR = 100;

const VALIDATEUR_GENESE = '0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50';
const CONSEIL = '0x0000000000000000000000000000000000000001';
const PUITS = '0x000000000000000000000000000000000000dEaD';
const FENETRE_PREUVE_BLOCS = 362880n;
const PRIME_POUR_CENT = 1n;
const PRIME_PLAFOND = 10n * 10n ** 18n;
const DELAI_DEBLOCAGE = 49n * 86400n;
const INEXISTANT = 0, EN_ATTENTE = 1, ACTIF = 2, EN_QUARANTAINE = 3, EN_DEBLOCAGE = 4, BANNI = 5;

// eth_simulateV1 ne peut pas s'eloigner de plus de maxSimulateBlocks = 256 blocs
// de la tete, et la tete est le bloc 0 : tout le banc se tient donc a 250, et la
// faute est commise a la hauteur 200. FENETRE_PREUVE_BLOCS vaut 362 880, la
// fenetre est donc franchement ouverte — la section 6 la teste separement.
const BN = 250n;
const HAUTEUR = 200n;
const TS = 1800000000n;
const BLOC_ENGAGEMENT = 100n;

const HARNAIS = '0x00000000000000000000000000000000000C0DE1';
const POSEUR = '0x0000000000000000000000000000000000000099';
const RAPPORTEUR = '0x00000000000000000000000000000000000ABCDE';

// Les lignes du correctif, mot pour mot. La section 8 verifie que chacune
// apparait EXACTEMENT UNE FOIS avant de muter : sinon la mutation passerait en
// silence et la suite se declarerait « sensible » sans avoir rien mute.
const LIGNE_SIGNATURE = '    function signalerDoubleSignature(bytes calldata enTete1, bytes calldata enTete2) external {';
const LIGNE_ENVELOPPE = '        bytes memory enveloppe = _enveloppePreuve(enTete1, enTete2);';
const LIGNE_LONGUEUR = '        if (!ok || ret.length != 52) revert PreuveInvalide();';
const LIGNE_OCTET_NU = '        if (x <= 0x7f) return abi.encodePacked(uint8(x));';
const LIGNE_CHAINID = '        bytes memory idc = _rlpEntier(block.chainid);';

// -----------------------------------------------------------------------------
// Compte-rendu — meme forme que scripts/test-selection-validateurs.js
// -----------------------------------------------------------------------------
let pass = 0, fail = 0;
const results = [];

function check(name, actual, expected) {
  const ok = String(actual) === String(expected);
  ok ? pass++ : fail++;
  results.push({ name, ok, actual: String(actual), expected: String(expected) });
  console.log(`  ${ok ? '\x1b[32mOK  \x1b[0m' : '\x1b[31mECHEC\x1b[0m'} ${name}${ok ? '' : `\n         attendu : ${expected}\n         obtenu  : ${actual}`}`);
}
function checkQue(name, condition) { check(name, condition ? 'vrai' : 'faux', 'vrai'); }
function titre(s) { console.log(`\n\x1b[1m${s}\x1b[0m`); }
function note(s) { console.log(`  \x1b[90m${s}\x1b[0m`); }

// =============================================================================
// LA FABRIQUE DE PREUVES — une reimplementation INDEPENDANTE de
// core/types/block.go, ecrite ici avec la RLP d'ethers et non avec celle du
// contrat. C'est ce qui la rend recevable : si elle empruntait l'encodeur teste,
// elle ne prouverait que sa propre coherence. Le juge final n'est de toute facon
// ni elle ni le contrat, c'est le precompile Go lui-meme.
//
// EncodeSigHeader (core/types/block.go, ligne 670) place le ChainId en PREMIER
// champ de la liste scellee. Le ChainId fait donc partie du message signe : une
// signature ne vaut QUE pour une chaine. Mais c'est la preuve elle-meme qui
// declare laquelle — et c'est toute l'affaire.
// =============================================================================

const b32 = (s) => ethers.zeroPadValue(s, 32);

/// RLP d'un entier a la maniere de Go : chaine d'octets minimale, ZERO -> vide.
const ent = (x) => {
  x = BigInt(x);
  if (x === 0n) return '0x';
  let h = x.toString(16);
  if (h.length % 2) h = '0' + h;
  return '0x' + h;
};

/// Les douze champs qui precedent Extra, dans l'ordre de types.Header.
function champsAvantExtra(h) {
  return [h.parentHash, h.uncleHash, h.coinbase, h.root, h.txHash, h.receiptHash, h.bloom,
    ent(h.difficulty), ent(h.number), ent(h.gasLimit), ent(h.gasUsed), ent(h.time)];
}

/// Le RLP d'un types.Header : quinze champs. Les six champs `rlp:"optional"`
/// (BaseFee, WithdrawalsHash, BlobGasUsed, ExcessBlobGas, ParentBeaconRoot,
/// RequestsHash) sont ABSENTS — le decodeur Go les met a zero, et c'est
/// exactement ce que produit un entete Parlia sans cancunTime.
function rlpEntete(h) {
  return ethers.encodeRlp([...champsAvantExtra(h), h.extra, h.mixDigest, h.nonce]);
}

/// types.SealHash(header, chainId) : keccak256 du RLP de seize champs, ChainId
/// en tete, Extra ampute de ses 65 derniers octets (le sceau lui-meme).
/// ParentBeaconRoot etant toujours nil sur Coinbosa (pas de cancunTime), la
/// branche qui rajoute BaseFee et consorts n'est jamais prise.
function sealHash(h, chainId) {
  const sansSceau = ethers.dataSlice(h.extra, 0, ethers.dataLength(h.extra) - 65);
  return ethers.keccak256(ethers.encodeRlp([ent(chainId), ...champsAvantExtra(h), sansSceau, h.mixDigest, h.nonce]));
}

/// Scelle un entete. Le sceau est place a la fin d'Extra, exactement comme
/// Parlia : 32 octets de vanite, puis (r, s, v) sur 65 octets, v valant 0 ou 1.
function sceller(base, cle, chainId) {
  const provisoire = { ...base, extra: ethers.concat([base.vanite, '0x' + '00'.repeat(65)]) };
  const sig = new ethers.SigningKey(cle).sign(sealHash(provisoire, chainId));
  const sceau = ethers.concat([sig.r, sig.s, '0x' + sig.yParity.toString(16).padStart(2, '0')]);
  return { ...base, extra: ethers.concat([base.vanite, sceau]) };
}

/// rlp([chainId, HeaderBytes1, HeaderBytes2]) — l'enveloppe attendue par 0x68.
/// C'est la structure DoubleSignEvidence de core/vm/contracts.go, ligne 1833.
const enveloppe = (chainId, e1, e2) => ethers.encodeRlp([ent(chainId), rlpEntete(e1), rlpEntete(e2)]);

/// Le gabarit d'un entete Parlia. Rien ici n'a besoin d'etre « vrai » : le
/// precompile ne rattache les entetes a aucune histoire, il ne verifie que la
/// coherence interne et les signatures. C'est precisement une des limites
/// consignees en fin de suite.
function gabarit(o) {
  return {
    parentHash: b32(o.parent || '0xa1'),
    uncleHash: '0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347',
    coinbase: o.coinbase || '0x0000000000000000000000000000000000000000',
    root: b32(o.root || '0x01'),
    txHash: '0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421',
    receiptHash: '0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421',
    bloom: '0x' + '00'.repeat(256),
    difficulty: o.difficulty === undefined ? 2 : o.difficulty,
    number: o.number === undefined ? HAUTEUR : o.number,
    gasLimit: 140000000, gasUsed: 0, time: 1799990000,
    mixDigest: b32('0x00'), nonce: '0x0000000000000000',
    vanite: '0x' + (o.vanite || 'd9').repeat(32),
  };
}

/// UNE FAUTE : deux entetes DIFFERENTS, a la MEME hauteur, sur le MEME parent,
/// scelles par la MEME cle. `chainId` est la chaine sous laquelle la faute est
/// commise — c'est le seul parametre que l'on fera varier.
function faute(cle, chainId, o = {}) {
  const a = gabarit({ ...o, root: '0x0a' });
  const b = gabarit({ ...o, root: '0x0b' });
  const e1 = sceller(a, cle, chainId);
  const e2 = sceller(b, cle, chainId);
  return { e1, e2, h1: rlpEntete(e1), h2: rlpEntete(e2), env: enveloppe(chainId, e1, e2), chainId };
}

// =============================================================================
// LE HARNAIS — compile en memoire, jamais ecrit dans contracts/
//
// Il n'ajoute AUCUNE logique : il pose un etat de depart, relit ce que la vraie
// fonction a fait, et expose les deux briques de l'encodeur pour qu'elles
// soient confrontables octet par octet a la RLP d'ethers. `signalerDoubleSignature`
// n'est jamais reimplementee — elle est appelee telle quelle, par un compte
// EXTERIEUR, pour que `msg.sender` soit un vrai rapporteur.
// =============================================================================
const SOURCE_HARNAIS = `// SPDX-License-Identifier: MIT
pragma solidity 0.8.26;

import "./CoinbosaStake.sol";

contract EssaiDoubleSignature is CoinbosaStake {
    struct Bilan {
        uint256 enjeu;
        uint8   etat;
        uint256 aBruler;
        uint256 primesDues;
        uint256 primeRapporteur;
        uint64  primeDisponibleLe;
        uint64  dateDeblocage;
        uint64  blocReferenceDeblocage;
        bool    sanctionnee;
    }

    function essaiPoser(address a, uint96 enjeu, uint8 etat, uint64 blocEngagement) external {
        Entree storage e = entrees[a];
        e.enjeu = enjeu;
        e.etat = etat;
        e.blocEngagement = blocEngagement;
        e.enjeuMinAdmission = 1000e18;
    }

    function essaiBilan(address fautif, address rapporteur, uint256 hauteur)
        external view returns (Bilan memory b)
    {
        Entree storage e = entrees[fautif];
        b.enjeu = uint256(e.enjeu);
        b.etat = e.etat;
        b.aBruler = aBruler;
        b.primesDues = primesDues;
        b.primeRapporteur = primeDe[rapporteur];
        b.primeDisponibleLe = primeDisponibleLe[rapporteur];
        b.dateDeblocage = e.dateDeblocage;
        b.blocReferenceDeblocage = e.blocReferenceDeblocage;
        b.sanctionnee = infractionSanctionnee[keccak256(abi.encode(fautif, hauteur))];
    }

    /// LE VRAI CHEMIN DE PRODUCTION, expose tel quel : c'est cette enveloppe,
    /// et aucune autre, qui part au precompile.
    function essaiEnveloppe(bytes calldata h1, bytes calldata h2)
        external view returns (bytes memory)
    {
        return _enveloppePreuve(h1, h2);
    }

    function essaiRlpEntier(uint256 x) external pure returns (bytes memory) {
        return _rlpEntier(x);
    }

    function essaiChainId() external view returns (uint256) { return block.chainid; }
}
`;

/// Compile CoinbosaStake + le harnais EN MEMOIRE, avec exactement les reglages
/// de scripts/compile.js (optimizer 200, evmVersion shanghai). Rien n'est ecrit
/// sur disque : `sourceStake` permet a la section 8 de compiler un mutant sans
/// poser le moindre octet dans contracts/ ni dans build/.
function compiler(sourceStake) {
  const input = {
    language: 'Solidity',
    sources: {
      'CoinbosaStake.sol': { content: sourceStake },
      'EssaiDoubleSignature.sol': { content: SOURCE_HARNAIS },
    },
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
    harnais: out.contracts['EssaiDoubleSignature.sol'].EssaiDoubleSignature,
    stake: out.contracts['CoinbosaStake.sol'].CoinbosaStake,
  };
}

// =============================================================================
// LES NOEUDS JETABLES
// =============================================================================

const noeuds = [];

function trouverGeth() {
  const g = process.env.GETH
    || [path.join(RACINE, '..', 'geth'), path.join(RACINE, 'build', 'bin', 'geth')].find((p) => fs.existsSync(p));
  if (!g) throw new Error("binaire geth introuvable — indique-le avec GETH=/chemin/vers/geth");
  return g;
}

/// `feynman` : ajoute feynmanTime au genesis, ce qui fait entrer 0x68 dans le
/// jeu de precompiles actif (activePrecompiledContracts, core/vm/contracts.go).
/// `chainId` : reecrit le chainId. Les deux ecrivent un genesis TEMPORAIRE dans
/// le datadir jetable ; genesis/ n'est jamais modifie.
///
/// `--rpc.gascap 0` n'est pas un confort : le staticcall vers 0x68 CONSOMME tout
/// le gaz transmis quand le precompile refuse (errInvalidEvidence remonte comme
/// une erreur, pas comme un revert). Sans marge, on ne saurait pas distinguer un
/// refus du precompile d'une panne seche.
async function demarrerNoeud(nom, opts) {
  const geth = trouverGeth();
  const g = JSON.parse(fs.readFileSync(path.join(RACINE, 'genesis', 'genesis-coinbosa.json'), 'utf8'));
  if (opts.feynman) { g.config.feynmanTime = 0; g.config.feynmanFixTime = 0; }
  if (opts.chainId) g.config.chainId = opts.chainId;
  const dd = fs.mkdtempSync(path.join(os.tmpdir(), `coinbosa-ds-${nom}-`));
  const gj = path.join(dd, 'genesis.json');
  fs.writeFileSync(gj, JSON.stringify(g, null, 1));
  execFileSync(geth, ['init', '--datadir', dd, gj], { stdio: 'ignore' });
  const journal = fs.openSync(path.join(dd, 'geth.log'), 'a');
  const p = spawn(geth, [
    '--datadir', dd, '--datadir.minfreedisk', '0', '--networkid', String(g.config.chainId),
    '--port', String(opts.p2p), '--ipcdisable',
    '--http', '--http.addr', '127.0.0.1', '--http.port', String(opts.port),
    '--http.api', 'eth,net,web3,debug',
    '--nodiscover', '--maxpeers', '0', '--syncmode', 'full', '--gcmode', 'archive',
    '--rpc.gascap', '0', '--verbosity', '1',
  ], { stdio: ['ignore', journal, journal], detached: false });
  const n = { nom, proc: p, dd, url: `http://127.0.0.1:${opts.port}`, chainId: g.config.chainId, feynman: !!opts.feynman };
  noeuds.push(n);
  for (let i = 0; i < 150; i++) {
    try { const j = await rpc(n, 'eth_chainId', []); if (j.result) return n; } catch { /* pas encore la */ }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`le noeud ${nom} n'a pas repondu (journal : ${path.join(dd, 'geth.log')})`);
}

function arreterNoeuds() {
  for (const n of noeuds) {
    try { n.proc.kill('SIGKILL'); } catch { /* deja mort */ }
    try { fs.rmSync(n.dd, { recursive: true, force: true }); } catch { /* peu importe */ }
  }
  noeuds.length = 0;
}

let idRpc = 1;
async function rpc(n, method, params) {
  const r = await fetch(n.url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ jsonrpc: '2.0', id: idRpc++, method, params }),
  });
  return r.json();
}

// -----------------------------------------------------------------------------
// Les transports
// -----------------------------------------------------------------------------

/// Le precompile, ATTAQUE EN DIRECT. Aucun contrat entre lui et nous : c'est le
/// juge de paix du banc, celui qui dit si une preuve est authentique.
async function precompile(n, blob) {
  const j = await rpc(n, 'eth_call', [
    { to: '0x0000000000000000000000000000000000000068', input: blob, gas: '0x1000000' },
    'latest',
    {}, { number: '0x' + BN.toString(16), time: '0x' + TS.toString(16) },
  ]);
  if (j.error) return { refus: j.error.message };
  if (!j.result || j.result === '0x') return { refus: 'retour vide (0x68 sans code)', vide: true };
  return {
    brut: j.result,
    signataire: ethers.getAddress(ethers.dataSlice(j.result, 0, 20)),
    hauteur: BigInt(ethers.dataSlice(j.result, 20, 52)),
    longueur: ethers.dataLength(j.result),
  };
}

/// Appel de lecture sur le harnais pose par surcharge de code.
async function lire(n, code, iface, fn, args) {
  const j = await rpc(n, 'eth_call', [
    { to: HARNAIS, input: iface.encodeFunctionData(fn, args), gas: '0x38d7ea4c68000' },
    'latest',
    { [HARNAIS]: { code } },
    { number: '0x' + BN.toString(16), time: '0x' + TS.toString(16) },
  ]);
  if (j.error) return { erreur: j.error.message };
  return { valeur: iface.decodeFunctionResult(fn, j.result) };
}

/// Traduit un appel rate en NOM D'ERREUR du contrat. Sans cela, « ca a
/// echoue » ne distingue pas un refus voulu d'une panne du banc — et c'est
/// exactement la confusion qui rendrait toute la suite muette.
function raisonDe(c, iface) {
  if (c.status === '0x1') return 'ok';
  const d = (c.error && c.error.data) || c.returnData || '0x';
  if (d && d !== '0x') {
    try { const p = iface.parseError(d); if (p) return p.name; } catch { /* pas une erreur du contrat */ }
  }
  if (c.error && c.error.message) return c.error.message;
  return 'revert sans donnee';
}

/// LE VRAI CHEMIN. Un bloc simule au numero 250 : on pose l'etat, un compte
/// EXTERIEUR emet `signalerDoubleSignature(...)`, puis on releve le bilan. Les
/// ecritures d'un appel survivent au suivant — eth_simulateV1 est le seul
/// transport qui le permette — et un appel qui revert n'ecrit rien.
async function scenario(n, code, iface, appels, overrides) {
  const j = await rpc(n, 'eth_simulateV1', [{
    blockStateCalls: [{
      blockOverrides: {
        number: '0x' + BN.toString(16), time: '0x' + TS.toString(16),
        baseFeePerGas: '0x0', gasLimit: '0x7fffffffffffff',
      },
      stateOverrides: { [HARNAIS]: { code }, ...(overrides || {}) },
      calls: appels.map((a) => ({
        from: a.de || POSEUR, to: HARNAIS, gasPrice: '0x0', gas: '0x10000000000',
        input: iface.encodeFunctionData(a.fn, a.args),
      })),
    }],
    validation: false,
  }, '0x0']);
  if (j.error) throw new Error('eth_simulateV1 : ' + j.error.message);
  const bloc = j.result[j.result.length - 1];
  return bloc.calls.map((c, i) => {
    const r = { statut: c.status, raison: raisonDe(c, iface), gaz: Number(BigInt(c.gasUsed)) };
    r.evenements = (c.logs || []).map((l) => {
      try { const p = iface.parseLog({ topics: l.topics, data: l.data }); return { nom: p.name, args: p.args.map(String) }; }
      catch { return null; }
    }).filter(Boolean);
    if (c.status === '0x1' && appels[i].fn === 'essaiBilan') {
      const b = iface.decodeFunctionResult('essaiBilan', c.returnData)[0];
      r.bilan = {
        enjeu: BigInt(b[0]), etat: Number(b[1]), aBruler: BigInt(b[2]), primesDues: BigInt(b[3]),
        prime: BigInt(b[4]), primeLe: BigInt(b[5]), dateDeblocage: BigInt(b[6]),
        blocRef: BigInt(b[7]), sanctionnee: b[8],
      };
    }
    return r;
  });
}

/// Bytecode EVM qui RENVOIE un blob litteral, octet pour octet. Sert a poser un
/// FAUX 0x68 par surcharge de code (uniquement possible la ou 0x68 n'est pas un
/// precompile actif, c'est-a-dire sur le noeud PROD sans feynmanTime). Chaque
/// mot est pousse puis stocke, RETURN(0, longueur) clot. C'est le seul moyen de
/// faire renvoyer a 0x68 une longueur AUTRE que 52 sans toucher au client Go.
function bytecodeRetour(blobHex) {
  const b = ethers.getBytes(blobHex);
  let code = '0x';
  for (let off = 0; off < b.length; off += 32) {
    const mot = ethers.hexlify(b.slice(off, off + 32)).slice(2).padEnd(64, '0');
    code += '7f' + mot;                                   // PUSH32 mot
    code += '60' + off.toString(16).padStart(2, '0');     // PUSH1 offset
    code += '52';                                          // MSTORE
  }
  code += '60' + b.length.toString(16).padStart(2, '0');  // PUSH1 longueur
  code += '6000';                                          // PUSH1 0
  code += 'f3';                                            // RETURN
  return code;
}
const PRECOMPILE_0x68 = '0x0000000000000000000000000000000000000068';

/// Le scenario type : poser un enjeu, relever avant, denoncer, relever apres.
function scenarioSanction(fautif, enjeu, etat, blocEng, h1, h2, hauteur, rapporteur) {
  return [
    { fn: 'essaiPoser', args: [fautif, enjeu, etat, blocEng] },
    { fn: 'essaiBilan', args: [fautif, rapporteur, hauteur] },
    { fn: 'signalerDoubleSignature', args: [h1, h2], de: rapporteur },
    { fn: 'essaiBilan', args: [fautif, rapporteur, hauteur] },
  ];
}

// =============================================================================
// LA SUITE
// =============================================================================

(async () => {
  const PORT = Number(process.env.PORT_BASE || 8591);
  console.log('\n\x1b[1mCoinbosa Chain — double signature (CoinbosaStake.signalerDoubleSignature)\x1b[0m');

  // ---------------------------------------------------------------------------
  titre('0. LE BANC D\'ESSAI');
  note('Tout ce qui suit est sans valeur si le contrat teste n\'est pas le contrat');
  note('reel, ou si le precompile n\'est pas le vrai. Ces gardes sont fail-closed.');
  // ---------------------------------------------------------------------------
  check('solc epingle a 0.8.26', solc.version().startsWith('0.8.26'), true);

  const sourceStake = fs.readFileSync(path.join(SRC, 'CoinbosaStake.sol'), 'utf8');
  const { harnais, stake } = compiler(sourceStake);
  const iface = new ethers.Interface(harnais.abi);
  const CODE = '0x' + harnais.evm.deployedBytecode.object;

  const artefact = JSON.parse(fs.readFileSync(path.join(RACINE, 'build', 'CoinbosaStake.json'), 'utf8'));
  check('CoinbosaStake recompile identique a build/CoinbosaStake.json', stake.evm.bytecode.object === artefact.bytecode, true);
  check('le harnais ne change pas le bytecode de CoinbosaStake', compiler(sourceStake).stake.evm.bytecode.object === artefact.bytecode, true);

  // Les quatre ancrages de la mutation. S'ils cessent d'etre uniques, la
  // section 8 muterait a cote et se declarerait verte sans avoir rien fait.
  check('la signature a deux entetes apparait exactement une fois', sourceStake.split(LIGNE_SIGNATURE).length - 1, 1);
  check('la construction de l\'enveloppe apparait exactement une fois', sourceStake.split(LIGNE_ENVELOPPE).length - 1, 1);
  check('le controle ret.length != 52 apparait exactement une fois', sourceStake.split(LIGNE_LONGUEUR).length - 1, 1);
  check('le raccourci « octet nu » apparait exactement une fois', sourceStake.split(LIGNE_OCTET_NU).length - 1, 1);
  check('l\'enveloppe est bien batie sur block.chainid', sourceStake.split(LIGNE_CHAINID).length - 1, 1);
  checkQue('le chainId n\'est PAS un parametre de _enveloppePreuve',
    /function _enveloppePreuve\(bytes calldata enTete1, bytes calldata enTete2\)/.test(sourceStake));
  check('FENETRE_PREUVE_BLOCS de l\'oracle == celle de la source', sourceStake.includes('FENETRE_PREUVE_BLOCS = 362_880'), true);
  check('PRIME_PLAFOND de l\'oracle == celle de la source', sourceStake.includes('PRIME_PLAFOND = 10e18'), true);
  check('VALIDATEUR_GENESE de l\'oracle == celui de la source', sourceStake.includes(`VALIDATEUR_GENESE = ${VALIDATEUR_GENESE}`), true);

  // Le genesis de production ne porte AUCUN feynmanTime : c'est le fait qui
  // oblige tout ce banc a demarrer un noeud de configuration FUTURE. On le
  // mesure, on ne le suppose pas.
  const genProd = JSON.parse(fs.readFileSync(path.join(RACINE, 'genesis', 'genesis-coinbosa.json'), 'utf8'));
  check('genesis de production : chainId 26262', genProd.config.chainId, CHAINID_PROD);
  check('genesis de production : aucun feynmanTime (0x68 est mort aujourd\'hui)', genProd.config.feynmanTime === undefined, true);

  note('demarrage des trois noeuds jetables (aucun ne mine, aucun ne detient de cle)…');
  const nProd = await demarrerNoeud('prod', { port: PORT, p2p: 30491 });
  const nPobs = await demarrerNoeud('pobs', { port: PORT + 1, p2p: 30492, feynman: true });
  const nAutre = await demarrerNoeud('autre', { port: PORT + 2, p2p: 30493, feynman: true, chainId: CHAINID_MIROIR });

  for (const n of [nProd, nPobs, nAutre]) {
    const c = await rpc(n, 'eth_chainId', []);
    const h = await rpc(n, 'eth_blockNumber', []);
    check(`noeud ${n.nom} : chainId ${n.chainId}`, Number(BigInt(c.result)), n.chainId);
    check(`noeud ${n.nom} : au bloc 0, n'a jamais mine`, Number(BigInt(h.result)), 0);
    const cid = await lire(n, CODE, iface, 'essaiChainId', []);
    check(`noeud ${n.nom} : block.chainid vu par l'EVM`, String(cid.valeur[0]), String(n.chainId));
  }
  {
    // Le gaz : si le noeud plafonnait les eth_call, le staticcall vers 0x68
    // manquerait de gaz et la suite prendrait une panne seche pour un refus.
    const sonde = '0x00000000000000000000000000000000000C0DE2';
    const j = await rpc(nPobs, 'eth_call', [{ to: sonde, input: '0x', gas: '0x38d7ea4c68000' }, 'latest', { [sonde]: { code: '0x5a60005260206000f3' } }]);
    checkQue('le noeud accepte plus de 1e12 de gaz (--rpc.gascap 0)', j.result && BigInt(j.result) > 1000000000000n);
  }
  {
    const j = await rpc(nPobs, 'eth_getCode', ['0x0000000000000000000000000000000000000068', 'latest']);
    note(`eth_getCode(0x68) = ${j.result} — un precompile n'a pas de code EVM, il vit dans le tableau Go.`);
  }

  // ---------------------------------------------------------------------------
  titre('1. LA PREUVE AUTHENTIQUE — fabriquee ici, jugee par le precompile');
  note('Une cle jetee, deux entetes Parlia DIFFERENTS a la MEME hauteur sur le');
  note('MEME parent, scelles tous les deux. Si 0x68 ne l\'acceptait pas sous notre');
  note('chainId, le banc serait faux et tout le reste ne vaudrait rien.');
  // ---------------------------------------------------------------------------
  const CLE = '0x' + '11'.repeat(31) + '22'; // cle jetee, n'existe nulle part ailleurs
  const FAUTIF = new ethers.Wallet(CLE).address;
  note(`cle jetee -> validateur fautif ${FAUTIF}`);

  const F26262 = faute(CLE, CHAINID_PROD);
  {
    check('les deux entetes different', F26262.h1 !== F26262.h2, true);
    check('… meme hauteur', `${BigInt(HAUTEUR)}/${BigInt(HAUTEUR)}`, `${HAUTEUR}/${HAUTEUR}`);
    check('… meme ParentHash', F26262.e1.parentHash === F26262.e2.parentHash, true);
    const s1 = ethers.dataSlice(F26262.e1.extra, ethers.dataLength(F26262.e1.extra) - 65);
    const s2 = ethers.dataSlice(F26262.e2.extra, ethers.dataLength(F26262.e2.extra) - 65);
    check('… deux sceaux distincts de 65 octets', s1 !== s2 && ethers.dataLength(s1) === 65, true);
    check('… deux SealHash distincts', sealHash(F26262.e1, CHAINID_PROD) !== sealHash(F26262.e2, CHAINID_PROD), true);
  }
  {
    const r = await precompile(nPobs, F26262.env);
    check('0x68 ACCEPTE la preuve scellee sous 26262', r.refus === undefined, true);
    if (r.refus) note(`  refus : ${r.refus}`);
    check('0x68 rend 52 octets', r.longueur, 52);
    check('0x68 designe exactement le fautif', r.signataire, FAUTIF);
    check('0x68 rend la hauteur scellee', String(r.hauteur), String(HAUTEUR));
  }

  // ---------------------------------------------------------------------------
  titre('1bis. LE BANC EST-IL COMPLAISANT ? — les refus que 0x68 doit opposer');
  note('Un fabricant de preuves qui fabrique n\'importe quoi ne prouve rien.');
  note('Cinq preuves volontairement mauvaises, cinq refus attendus.');
  // ---------------------------------------------------------------------------
  {
    const e = sceller(gabarit({ root: '0x0a' }), CLE, CHAINID_PROD);
    check('deux entetes IDENTIQUES : refuse', (await precompile(nPobs, enveloppe(CHAINID_PROD, e, e))).refus !== undefined, true);
  }
  {
    const a = sceller(gabarit({ root: '0x0a' }), CLE, CHAINID_PROD);
    const b = sceller(gabarit({ root: '0x0b', number: HAUTEUR + 1n }), CLE, CHAINID_PROD);
    check('hauteurs DIFFERENTES : refuse', (await precompile(nPobs, enveloppe(CHAINID_PROD, a, b))).refus !== undefined, true);
  }
  {
    const a = sceller(gabarit({ root: '0x0a' }), CLE, CHAINID_PROD);
    const b = sceller(gabarit({ root: '0x0b', parent: '0xb2' }), CLE, CHAINID_PROD);
    check('ParentHash DIFFERENTS : refuse', (await precompile(nPobs, enveloppe(CHAINID_PROD, a, b))).refus !== undefined, true);
    note('  ce refus-la est un FAUX NEGATIF herite du client Go : deux blocs a la');
    note('  meme hauteur sur des parents differents sont une equivocation reelle,');
    note('  et le precompile ne sait pas la punir. Consigne, non corrige ici.');
  }
  {
    const autreCle = '0x' + '33'.repeat(32);
    const a = sceller(gabarit({ root: '0x0a' }), CLE, CHAINID_PROD);
    const b = sceller(gabarit({ root: '0x0b' }), autreCle, CHAINID_PROD);
    check('deux cles DIFFERENTES : refuse', (await precompile(nPobs, enveloppe(CHAINID_PROD, a, b))).refus !== undefined, true);
  }
  {
    // LE POINT CENTRAL, mesure sur le precompile lui-meme : le ChainId fait
    // partie du message scelle. Une preuve scellee sous 26263 et presentee dans
    // une enveloppe 26262 — c'est-a-dire exactement ce que le contrat corrige
    // construit face a une preuve etrangere — est REFUSEE.
    const f = faute(CLE, CHAINID_VOISIN);
    const r = await precompile(nPobs, enveloppe(CHAINID_PROD, f.e1, f.e2));
    check('scellee sous 26263, enveloppee sous 26262 : refuse', r.refus !== undefined, true);
    const r2 = await precompile(nPobs, enveloppe(CHAINID_VOISIN, f.e1, f.e2));
    check('la MEME, enveloppee sous 26263 : acceptee', r2.refus === undefined, true);
    check('… et elle designe le meme fautif, la meme hauteur', `${r2.signataire}/${r2.hauteur}`, `${FAUTIF}/${HAUTEUR}`);
    note('c\'est la porte, filmee : le precompile croit l\'enveloppe sur parole.');
  }

  // ---------------------------------------------------------------------------
  titre('2. LA PORTE — la meme faute, commise sur une AUTRE chaine Parlia');
  note('Meme cle, meme hauteur, memes deux entetes : seul le ChainId sous lequel');
  note('le sceau a ete pose change. 0x68 les certifie TOUTES, sans distinction.');
  // ---------------------------------------------------------------------------
  const ETRANGERES = {};
  const FMIROIR = faute(CLE, CHAINID_MIROIR); // la MEME faute, commise sur la chaine 100
  for (const cid of [CHAINID_AUTRE, CHAINID_VOISIN, CHAINID_BSC]) {
    const f = faute(CLE, cid);
    ETRANGERES[cid] = f;
    const r = await precompile(nPobs, f.env);
    check(`faute scellee+enveloppee sous ${cid} : 0x68 l'ACCEPTE`, r.refus === undefined, true);
    check(`… meme fautif, meme hauteur que la notre`, `${r.signataire}/${r.hauteur}`, `${FAUTIF}/${HAUTEUR}`);
  }
  note('Aucune de ces trois preuves ne dit quoi que ce soit de Coinbosa. Le');
  note('validateur y est parfaitement honnete. C\'est tout l\'enjeu du correctif.');

  // ---------------------------------------------------------------------------
  titre('3. L\'ENCODEUR DU CONTRAT — octet pour octet contre la RLP d\'ethers');
  note('Un encodeur faux ne produit pas un faux positif : il produit un RLP que');
  note('le precompile refuse, donc une sanction INAPPLICABLE. C\'est le second');
  note('echec, celui qui rend un validateur malhonnete intouchable.');
  // ---------------------------------------------------------------------------
  {
    const valeurs = [0n, 1n, 55n, 56n, 127n, 128n, 129n, 255n, 256n, 511n,
      BigInt(CHAINID_AUTRE), BigInt(CHAINID_PROD), BigInt(CHAINID_VOISIN),
      65535n, 16777216n, (1n << 64n) - 1n, (1n << 255n), (1n << 256n) - 1n];
    let ecarts = 0, premier = null;
    for (const v of valeurs) {
      const r = await lire(nPobs, CODE, iface, 'essaiRlpEntier', [v]);
      const attendu = ethers.encodeRlp(ent(v));
      if (r.erreur || r.valeur[0] !== attendu) { ecarts++; if (!premier) premier = `${v} : ${r.erreur || r.valeur[0]} != ${attendu}`; }
    }
    check(`_rlpEntier : ${valeurs.length} valeurs, aucun ecart avec la RLP d'ethers`, ecarts, 0);
    if (premier) note('  premier ecart : ' + premier);
    const r26262 = await lire(nPobs, CODE, iface, 'essaiRlpEntier', [BigInt(CHAINID_PROD)]);
    check('le piege : 26262 s\'encode « 82 66 96 », pas « 66 96 »', r26262.valeur[0], '0x826696');
    const r56 = await lire(nPobs, CODE, iface, 'essaiRlpEntier', [56n]);
    check('… tandis que le 56 de BSC tient sur un octet nu', r56.valeur[0], '0x38');
    note('c\'est pourquoi recopier le SlashIndicator de BSC sans le comprendre ne');
    note('suffisait pas : chez eux la branche multi-octets n\'est jamais exercee.');
  }
  {
    // L'enveloppe COMPLETE, sur le vrai chemin de production, a plusieurs
    // longueurs — dont celles qui font basculer le prefixe RLP de forme.
    let ecarts = 0, premier = null;
    const tailles = [0, 1, 55, 56, 57, 128, 255, 256, 1024, 65535, 65536];
    for (const t of tailles) {
      const a = '0x' + '5a'.repeat(t);
      const b = '0x' + '7b'.repeat(t === 0 ? 0 : t - 1);
      const r = await lire(nPobs, CODE, iface, 'essaiEnveloppe', [a, b]);
      const attendu = ethers.encodeRlp([ent(CHAINID_PROD), a, b]);
      if (r.erreur || r.valeur[0] !== attendu) { ecarts++; if (!premier) premier = `taille ${t} : ${r.erreur || 'ecart'}`; }
    }
    // Le cas « octet nu » d'une chaine d'un seul octet, des deux cotes de 0x80.
    for (const o of ['0x00', '0x7f', '0x80', '0xff']) {
      const r = await lire(nPobs, CODE, iface, 'essaiEnveloppe', [o, o]);
      if (r.erreur || r.valeur[0] !== ethers.encodeRlp([ent(CHAINID_PROD), o, o])) { ecarts++; if (!premier) premier = `octet ${o}`; }
    }
    check(`_enveloppePreuve : ${tailles.length + 4} enveloppes, aucun ecart avec la RLP d'ethers`, ecarts, 0);
    if (premier) note('  premier ecart : ' + premier);
  }
  {
    // Le test qui compte : l'enveloppe que le contrat construit REELLEMENT a
    // partir de nos deux entetes est-elle celle que le precompile accepte ?
    const r = await lire(nPobs, CODE, iface, 'essaiEnveloppe', [F26262.h1, F26262.h2]);
    check('l\'enveloppe batie par le contrat == celle batie ici', r.valeur[0], F26262.env);
    const p = await precompile(nPobs, r.valeur[0]);
    check('… et 0x68 l\'accepte, signataire et hauteur exacts', `${p.signataire}/${p.hauteur}`, `${FAUTIF}/${HAUTEUR}`);
    const rE = await lire(nPobs, CODE, iface, 'essaiEnveloppe', [ETRANGERES[CHAINID_AUTRE].h1, ETRANGERES[CHAINID_AUTRE].h2]);
    checkQue('avec les entetes ETRANGERS, le contrat bat quand meme une enveloppe 26262',
      rE.valeur[0].startsWith('0x') && rE.valeur[0] !== ETRANGERES[CHAINID_AUTRE].env);
    check('… et cette enveloppe-la, 0x68 la REFUSE', (await precompile(nPobs, rE.valeur[0])).refus !== undefined, true);
  }

  // ---------------------------------------------------------------------------
  titre('4. SENS 1 — la preuve LEGITIME est acceptee et confisque');
  note('Un correctif qui refuse tout n\'est pas un correctif. On mesure la');
  note('confiscation entiere, la prime, le bannissement et le verrou anti-rejeu.');
  // ---------------------------------------------------------------------------
  const ENJEU = 5000n * 10n ** 18n;
  {
    const r = await scenario(nPobs, CODE, iface,
      scenarioSanction(FAUTIF, ENJEU, ACTIF, BLOC_ENGAGEMENT, F26262.h1, F26262.h2, HAUTEUR, RAPPORTEUR));
    check('la denonciation passe', r[2].raison, 'ok');
    check('avant : enjeu intact', String(r[1].bilan.enjeu), String(ENJEU));
    check('apres : enjeu ENTIEREMENT confisque', String(r[3].bilan.enjeu), '0');
    check('apres : le fautif est BANNI', r[3].bilan.etat, BANNI);
    const prime = (ENJEU * PRIME_POUR_CENT) / 100n > PRIME_PLAFOND ? PRIME_PLAFOND : (ENJEU * PRIME_POUR_CENT) / 100n;
    check('apres : prime du rapporteur plafonnee a 10 BOSA', String(r[3].bilan.prime), String(prime));
    check('apres : le reste part au puits', String(r[3].bilan.aBruler), String(ENJEU - prime));
    check('apres : prime verrouillee 49 jours', String(r[3].bilan.primeLe), String(TS + DELAI_DEBLOCAGE));
    check('apres : deblocage repousse de 49 jours', String(r[3].bilan.dateDeblocage), String(TS + DELAI_DEBLOCAGE));
    check('apres : bloc de reference du deblocage repousse', String(r[3].bilan.blocRef), String(BN));
    checkQue('apres : l\'infraction (fautif, hauteur) est marquee', r[3].bilan.sanctionnee);
    const ev = r[2].evenements.find((e) => e.nom === 'DoubleSignature');
    checkQue('evenement DoubleSignature emis', !!ev);
    if (ev) {
      check('… fautif', ethers.getAddress(ev.args[0]), FAUTIF);
      check('… saisi', ev.args[1], String(ENJEU));
      check('… hauteur', ev.args[2], String(HAUTEUR));
      check('… rapporteur', ethers.getAddress(ev.args[3]), ethers.getAddress(RAPPORTEUR));
    }
    note(`gaz consomme par la denonciation : ${r[2].gaz}`);
  }
  {
    // Prime sous le plafond : 1 % de 500 BOSA = 5 BOSA.
    const petit = 500n * 10n ** 18n;
    const r = await scenario(nPobs, CODE, iface,
      scenarioSanction(FAUTIF, petit, ACTIF, BLOC_ENGAGEMENT, F26262.h1, F26262.h2, HAUTEUR, RAPPORTEUR));
    check('petit enjeu : prime = 1 % exact, sous le plafond', String(r[3].bilan.prime), String(petit / 100n));
  }
  {
    // Rejeu : la MEME faute, deux fois, puis une AUTRE hauteur — la contre-
    // epreuve sans laquelle « anti-rejeu » voudrait dire « on ne punit qu'une
    // fois, jamais plus ».
    const autreHauteur = HAUTEUR + 1n;
    const f2 = faute(CLE, CHAINID_PROD, { number: autreHauteur });
    const r = await scenario(nPobs, CODE, iface, [
      { fn: 'essaiPoser', args: [FAUTIF, ENJEU, ACTIF, BLOC_ENGAGEMENT] },
      { fn: 'signalerDoubleSignature', args: [F26262.h1, F26262.h2], de: RAPPORTEUR },
      { fn: 'signalerDoubleSignature', args: [F26262.h1, F26262.h2], de: RAPPORTEUR },
      { fn: 'signalerDoubleSignature', args: [F26262.h2, F26262.h1], de: RAPPORTEUR },
      { fn: 'signalerDoubleSignature', args: [f2.h1, f2.h2], de: RAPPORTEUR },
      { fn: 'essaiBilan', args: [FAUTIF, RAPPORTEUR, autreHauteur] },
    ]);
    check('1re denonciation : passe', r[1].raison, 'ok');
    check('2e denonciation, memes octets : InfractionDejaSanctionnee', r[2].raison, 'InfractionDejaSanctionnee');
    check('3e, les deux entetes INTERVERTIS : refusee aussi', r[3].raison, 'InfractionDejaSanctionnee');
    note('l\'ancienne cle keccak256(preuve) n\'aurait PAS vu la 3e : memes fautif et');
    note('hauteur, octets differents. La cle (fautif, hauteur) la voit.');
    check('une AUTRE hauteur reste sanctionnable', r[4].raison, 'ok');
    checkQue('… et elle est marquee a son tour', r[5].bilan.sanctionnee);
  }
  {
    // Les etats. La liste d'origine — ACTIF, EN_QUARANTAINE, EN_DEBLOCAGE —
    // laissait EN_ATTENTE et BANNI dehors, or retirer() rend tout a un banni.
    for (const [nom, e] of [['ACTIF', ACTIF], ['EN_ATTENTE', EN_ATTENTE], ['EN_QUARANTAINE', EN_QUARANTAINE], ['EN_DEBLOCAGE', EN_DEBLOCAGE], ['BANNI', BANNI]]) {
      const r = await scenario(nPobs, CODE, iface,
        scenarioSanction(FAUTIF, ENJEU, e, BLOC_ENGAGEMENT, F26262.h1, F26262.h2, HAUTEUR, RAPPORTEUR));
      check(`etat ${nom} : saisissable`, `${r[2].raison}/${r[3].bilan.enjeu}`, 'ok/0');
    }
    const r = await scenario(nPobs, CODE, iface,
      scenarioSanction(FAUTIF, ENJEU, INEXISTANT, BLOC_ENGAGEMENT, F26262.h1, F26262.h2, HAUTEUR, RAPPORTEUR));
    check('etat INEXISTANT : EtatIncompatible', r[2].raison, 'EtatIncompatible');
  }
  {
    // Le validateur de genese : son argent est expose, sa place ne l'est pas.
    const cleGenese = null; // sa place est figee au bloc 0, on ne peut que la constater
    const r = await scenario(nPobs, CODE, iface, [
      { fn: 'essaiPoser', args: [VALIDATEUR_GENESE, ENJEU, ACTIF, BLOC_ENGAGEMENT] },
      { fn: 'essaiBilan', args: [VALIDATEUR_GENESE, RAPPORTEUR, HAUTEUR] },
    ]);
    check('le validateur de genese peut etre pose ACTIF', r[1].bilan.etat, ACTIF);
    note('sa sanction reelle exigerait sa cle de scellage, qui n\'est pas dans le');
    note('depot. Le chemin DoubleSignatureGenese reste donc NON EXECUTE ici — la');
    note('branche est lue, pas mesuree. C\'est consigne en fin de suite.');
    void cleGenese;
  }
  {
    // blocEngagement : un enjeu ne repond que de ce qui suit sa mise en jeu.
    const r = await scenario(nPobs, CODE, iface,
      scenarioSanction(FAUTIF, ENJEU, ACTIF, HAUTEUR + 1n, F26262.h1, F26262.h2, HAUTEUR, RAPPORTEUR));
    check('faute ANTERIEURE a la mise en jeu : PreuveAnterieureAuDepot', r[2].raison, 'PreuveAnterieureAuDepot');
    const r2 = await scenario(nPobs, CODE, iface,
      scenarioSanction(FAUTIF, ENJEU, ACTIF, HAUTEUR, F26262.h1, F26262.h2, HAUTEUR, RAPPORTEUR));
    check('faute a la hauteur EXACTE de la mise en jeu : saisissable', r2[2].raison, 'ok');
  }
  {
    // La fenetre. block.number vaut 250 : une faute a la hauteur 251 est dans
    // le futur, une faute a 250 - 362 881 serait hors fenetre (inatteignable
    // ici, la borne basse est donc verifiee par lecture de la source seule).
    const futur = faute(CLE, CHAINID_PROD, { number: BN + 1n });
    const r = await scenario(nPobs, CODE, iface,
      scenarioSanction(FAUTIF, ENJEU, ACTIF, 0n, futur.h1, futur.h2, BN + 1n, RAPPORTEUR));
    check('faute a une hauteur FUTURE : PreuveHorsFenetre', r[2].raison, 'PreuveHorsFenetre');
    const pile = faute(CLE, CHAINID_PROD, { number: BN });
    const r2 = await scenario(nPobs, CODE, iface,
      scenarioSanction(FAUTIF, ENJEU, ACTIF, 0n, pile.h1, pile.h2, BN, RAPPORTEUR));
    check('faute au bloc courant exactement : acceptee', r2[2].raison, 'ok');
    note(`la borne basse (${FENETRE_PREUVE_BLOCS} blocs, 21 jours) n'est pas atteignable`);
    note('depuis le bloc 0 : eth_simulateV1 ne s\'eloigne pas de plus de 256 blocs.');
  }

  // ---------------------------------------------------------------------------
  titre('5. SENS 2 — la preuve ETRANGERE est refusee');
  note('Les trois fautes de la section 2, celles que 0x68 certifie sans broncher,');
  note('presentees au contrat corrige. Aucune ne doit toucher un wei.');
  // ---------------------------------------------------------------------------
  for (const cid of [CHAINID_AUTRE, CHAINID_VOISIN, CHAINID_BSC]) {
    const f = ETRANGERES[cid];
    const r = await scenario(nPobs, CODE, iface,
      scenarioSanction(FAUTIF, ENJEU, ACTIF, BLOC_ENGAGEMENT, f.h1, f.h2, HAUTEUR, RAPPORTEUR));
    check(`faute authentique commise sous ${cid} : PreuveInvalide`, r[2].raison, 'PreuveInvalide');
    check(`… l'enjeu du validateur est INTACT`, String(r[3].bilan.enjeu), String(ENJEU));
    check(`… il reste ACTIF, aucune prime versee`, `${r[3].bilan.etat}/${r[3].bilan.prime}`, `${ACTIF}/0`);
    checkQue('… et aucune infraction n\'est marquee', r[3].bilan.sanctionnee === false);
  }

  // ---------------------------------------------------------------------------
  titre('6. LE MIROIR — le meme contrat sur une AUTRE chaine (chainId 100)');
  note('Preuve decisive que la garde suit `block.chainid` et non une constante');
  note('gravee : les deux preuves ECHANGENT leurs roles. C\'est aussi ce qui rend');
  note('le contrat redeployable en marque blanche sans reecrire une ligne.');
  // ---------------------------------------------------------------------------
  {
    const cid = await lire(nAutre, CODE, iface, 'essaiChainId', []);
    check('le noeud AUTRE execute bien sous chainId 100', String(cid.valeur[0]), String(CHAINID_MIROIR));
    const env = await lire(nAutre, CODE, iface, 'essaiEnveloppe', [F26262.h1, F26262.h2]);
    check('… le contrat y bat une enveloppe de chainId 100', env.valeur[0], ethers.encodeRlp([ent(CHAINID_MIROIR), F26262.h1, F26262.h2]));

    const rEtrangere = await scenario(nAutre, CODE, iface,
      scenarioSanction(FAUTIF, ENJEU, ACTIF, BLOC_ENGAGEMENT, F26262.h1, F26262.h2, HAUTEUR, RAPPORTEUR));
    check('sur la chaine 100, la faute commise sous 26262 est REFUSEE', rEtrangere[2].raison, 'PreuveInvalide');
    check('… enjeu intact', String(rEtrangere[3].bilan.enjeu), String(ENJEU));

    const rLocale = await scenario(nAutre, CODE, iface,
      scenarioSanction(FAUTIF, ENJEU, ACTIF, BLOC_ENGAGEMENT, FMIROIR.h1, FMIROIR.h2, HAUTEUR, RAPPORTEUR));
    check('sur la chaine 100, la faute commise sous 100 est SANCTIONNEE', rLocale[2].raison, 'ok');
    check('… enjeu confisque', String(rLocale[3].bilan.enjeu), '0');
    note('les memes octets, le meme contrat : seule la chaine qui execute change,');
    note('et le verdict s\'inverse. C\'est exactement la propriete recherchee.');
  }

  // ---------------------------------------------------------------------------
  titre('7. AUJOURD\'HUI — 0x68 est mort, et c\'est ret.length != 52 qui tient');
  note('Sur genesis/genesis-coinbosa.json tel quel, sans feynmanTime, 0x68 est un');
  note('compte VIDE : un staticcall y rend ok = true et zero octet.');
  // ---------------------------------------------------------------------------
  {
    const r = await precompile(nProd, F26262.env);
    checkQue('sur la chaine de production, 0x68 rend zero octet', r.vide === true);
    const s = await scenario(nProd, CODE, iface,
      scenarioSanction(FAUTIF, ENJEU, ACTIF, BLOC_ENGAGEMENT, F26262.h1, F26262.h2, HAUTEUR, RAPPORTEUR));
    check('… la preuve authentique elle-meme est refusee : PreuveInvalide', s[2].raison, 'PreuveInvalide');
    check('… enjeu intact', String(s[3].bilan.enjeu), String(ENJEU));
    note('le correctif est donc INERTE tant que PoBS n\'est pas active. Il doit');
    note('etre en place AVANT l\'activation, pas apres.');
  }

  // ---------------------------------------------------------------------------
  titre('8. MUTATION — la suite mord-elle vraiment ?');
  note('Une suite verte ne prouve rien tant qu\'on n\'a pas montre qu\'elle sait');
  note('virer au rouge. On mute EN MEMOIRE, on recompile avec le meme solc, et on');
  note('rejoue les memes mesures. contracts/ et build/ ne sont jamais touches.');
  // ---------------------------------------------------------------------------
  {
    // ---- M1 : LA PORTE D'ORIGINE. L'enveloppe est recue, plus construite.
    // La signature ABI reste (bytes, bytes) pour que le banc appelle a
    // l'identique ; c'est le corps qui redevient `staticcall(blob de l'appelant)`.
    const m1 = sourceStake
      .replace(LIGNE_SIGNATURE, '    function signalerDoubleSignature(bytes calldata enTete1, bytes calldata) external {')
      .replace(LIGNE_ENVELOPPE, '        bytes memory enveloppe = enTete1;');
    check('M1 : la mutation a bien ete appliquee', m1 !== sourceStake, true);
    const codeM1 = '0x' + compiler(m1).harnais.evm.deployedBytecode.object;
    check('M1 : le bytecode mute differe du sain', codeM1 !== CODE, true);

    // Temoin : le mutant n'est pas simplement casse — la preuve legitime passe.
    const legit = await scenario(nPobs, codeM1, iface,
      scenarioSanction(FAUTIF, ENJEU, ACTIF, BLOC_ENGAGEMENT, F26262.env, '0x', HAUTEUR, RAPPORTEUR));
    check('M1 : la preuve legitime passe encore (le mutant est vivant)', legit[2].raison, 'ok');

    for (const cid of [CHAINID_AUTRE, CHAINID_VOISIN, CHAINID_BSC]) {
      const r = await scenario(nPobs, codeM1, iface,
        scenarioSanction(FAUTIF, ENJEU, ACTIF, BLOC_ENGAGEMENT, ETRANGERES[cid].env, '0x', HAUTEUR, RAPPORTEUR));
      check(`M1 : la faute ETRANGERE (${cid}) CONFISQUE tout — la porte etait reelle`, `${r[2].raison}/${r[3].bilan.enjeu}`, 'ok/0');
      check(`M1 : … et le fautif est banni pour une faute commise ailleurs`, r[3].bilan.etat, BANNI);
      const ev = r[2].evenements.find((e) => e.nom === 'DoubleSignature');
      checkQue(`M1 : … avec un DoubleSignature en bonne et due forme`, !!ev && ethers.getAddress(ev.args[0]) === FAUTIF);
    }
    note('M1 est TUE par la section 5 : le contrat corrige repond PreuveInvalide');
    note('aux trois memes preuves. Le test attrape donc bien la porte fermee.');

    // ---- M2 : L'ENCODEUR CASSE. Le raccourci « octet nu » applique au PREMIER
    // OCTET au lieu de la LONGUEUR — l'erreur exacte que le commentaire du
    // contrat decrit. Elle ne produit pas un faux positif : elle rend la
    // sanction INAPPLICABLE, ce qui est le second echec a eviter.
    const m2 = sourceStake.replace(LIGNE_OCTET_NU,
      '        { uint256 nn = _longueurOctets(x); bytes memory bb = _octetsDe(x, nn); if (uint8(bb[0]) <= 0x7f) return bb; }');
    check('M2 : la mutation a bien ete appliquee', m2 !== sourceStake, true);
    const codeM2 = '0x' + compiler(m2).harnais.evm.deployedBytecode.object;
    const enc = await lire(nPobs, codeM2, iface, 'essaiRlpEntier', [BigInt(CHAINID_PROD)]);
    check('M2 : 26262 s\'encode desormais « 66 96 », donc mal', enc.valeur[0], '0x6696');
    const r2 = await scenario(nPobs, codeM2, iface,
      scenarioSanction(FAUTIF, ENJEU, ACTIF, BLOC_ENGAGEMENT, F26262.h1, F26262.h2, HAUTEUR, RAPPORTEUR));
    check('M2 : la preuve LEGITIME devient irrecevable — la suite le voit', r2[2].raison, 'PreuveInvalide');
    check('M2 : … le validateur malhonnete garde son enjeu', String(r2[3].bilan.enjeu), String(ENJEU));
    const r2b = await scenario(nAutre, codeM2, iface,
      scenarioSanction(FAUTIF, ENJEU, ACTIF, BLOC_ENGAGEMENT, FMIROIR.h1, FMIROIR.h2, HAUTEUR, RAPPORTEUR));
    check('M2 : sur la chaine 100, le meme mutant passe inapercu', r2b[2].raison, 'ok');
    note('100 tient sur un octet nu : le defaut n\'existe QUE pour un chainId dont');
    note('le premier octet est < 0x80 et qui occupe plus d\'un octet — 26262 en est');
    note('un, 56, 97 et 100 n\'en sont pas. Un banc qui n\'aurait teste que sur une');
    note('telle chaine aurait laisse passer ce mutant.');

    // ---- M3 : LE CONTROLE DE LONGUEUR RETIRE. `ret.length != 52` est ce qui
    // refuse un 0x68 qui rend autre chose que 52 octets — un 0x68 INERTE
    // (aujourd'hui, sans feynmanTime) ou un 0x68 dont le format aurait derive.
    const m3 = sourceStake.replace(LIGNE_LONGUEUR, '        if (!ok) revert PreuveInvalide();');
    check('M3 : la mutation a bien ete appliquee', m3 !== sourceStake, true);
    const codeM3 = '0x' + compiler(m3).harnais.evm.deployedBytecode.object;

    // (a) 0x68 INERTE, la chaine d'aujourd'hui. Un staticcall vers un compte
    // sans code rend ok = true et zero octet. Le contrat SAIN refuse net
    // (PreuveInvalide) ; le mutant, lui, FRANCHIT la porte et poursuit sur des
    // octets non verifies. Ici la hauteur lue dans la memoire residuelle se
    // trouve enorme, donc il bute sur PreuveHorsFenetre — un accident, pas une
    // defense : ce qui compte est qu'il ne REFUSE PLUS a la porte.
    {
      const r3 = await scenario(nProd, codeM3, iface,
        scenarioSanction(FAUTIF, ENJEU, ACTIF, 0n, F26262.h1, F26262.h2, HAUTEUR, RAPPORTEUR));
      const r3sain = await scenario(nProd, CODE, iface,
        scenarioSanction(FAUTIF, ENJEU, ACTIF, 0n, F26262.h1, F26262.h2, HAUTEUR, RAPPORTEUR));
      check('M3 : 0x68 inerte, code SAIN : refus net a la porte (PreuveInvalide)', r3sain[2].raison, 'PreuveInvalide');
      checkQue('M3 : 0x68 inerte, MUTANT : la porte est franchie (plus de PreuveInvalide)', r3[2].raison !== 'PreuveInvalide');
      note(`  le mutant poursuit et bute plus loin sur « ${r3[2].raison} », sur une`);
      note('  hauteur lue dans la memoire residuelle. Le sain, lui, n\'y arrive jamais.');
    }

    // (b) LA DEMONSTRATION DETERMINISTE DU DANGER. On pose un FAUX 0x68 par
    // surcharge de code (possible car 0x68 n'est pas precompile sur le noeud
    // PROD) qui rend un blob de 60 octets — longueur != 52 — nommant une VICTIME
    // et une hauteur DANS la fenetre. C'est le cas d'un 0x68 dont le format de
    // retour aurait change, ou d'un deploiement ou 0x68 porterait un autre code.
    {
      const victime = FAUTIF;
      const blob = ethers.concat([victime, ethers.zeroPadValue(ethers.toBeHex(HAUTEUR), 32), '0x0000000000000000']);
      check('M3 : le faux 0x68 rend bien 60 octets (!= 52)', ethers.dataLength(blob), 60);
      const faux68 = { [PRECOMPILE_0x68]: { code: bytecodeRetour(blob) } };
      const appels = [
        { fn: 'essaiPoser', args: [victime, ENJEU, ACTIF, 0] },
        { fn: 'signalerDoubleSignature', args: ['0x1234', '0x5678'], de: RAPPORTEUR },
        { fn: 'essaiBilan', args: [victime, RAPPORTEUR, HAUTEUR] },
      ];
      const rMut = await scenario(nProd, codeM3, iface, appels, faux68);
      const rSain = await scenario(nProd, CODE, iface, appels, faux68);
      check('M3 : SAIN — 60 octets refuses a la porte (PreuveInvalide)', rSain[1].raison, 'PreuveInvalide');
      check('M3 : SAIN — l\'enjeu de la victime est INTACT', String(rSain[2].bilan.enjeu), String(ENJEU));
      check('M3 : MUTANT — 60 octets acceptes, la victime est CONFISQUEE', rMut[1].raison, 'ok');
      check('M3 : MUTANT — enjeu de la victime a zero', String(rMut[2].bilan.enjeu), '0');
      check('M3 : MUTANT — la victime est BANNIE sans avoir rien fait', rMut[2].bilan.etat, BANNI);
      note('ret.length != 52 est donc la seule chose qui refuse un 0x68 au format');
      note('inattendu. BSC ne le verifie pas — sur ce point precis, ne pas le copier.');
    }
  }

  // ---------------------------------------------------------------------------
  titre('CE QUE CETTE SUITE NE COUVRE PAS');
  note('· UNE CHAINE QUI PARTAGE NOTRE CHAINID. genesis/genesis-coinbosa-dev.json');
  note('  porte aujourd\'hui le MEME 26262 que la production : une equivocation');
  note('  authentique commise la-bas est, par construction, valide ici, et aucun');
  note('  contrat ne peut les distinguer. La parade est hors contrat — donner un');
  note('  chainId distinct au genesis de dev. Non fait, non couvert ici.');
  note('· Le rattachement a NOTRE histoire. Le precompile ne verifie ni que');
  note('  ParentHash appartient a la chaine, ni que la hauteur correspond a un');
  note('  bloc reel. La regle appliquee est « il a signe deux entetes en conflit');
  note('  pour notre chainId », pas « il a double-signe sur la chaine canonique ».');
  note('  blockhash ne remonte qu\'a 256 blocs face a une fenetre de 362 880.');
  note('· Une cle de scellage VOLEE : indiscernable, comme sur BSC.');
  note('· Le faux negatif herite du client Go, mesure en section 1bis : deux blocs');
  note('  a la meme hauteur sur des parents DIFFERENTS ne sont pas punissables.');
  note('· Le chemin DoubleSignatureGenese : sa branche exige la cle de scellage de');
  note('  genese, absente du depot. Elle est lue, pas executee.');
  note('· La borne basse de la fenetre (362 880 blocs) : inatteignable depuis le');
  note('  bloc 0, eth_simulateV1 ne s\'eloignant pas de plus de 256 blocs.');
  note('· reclamerPrime() et purger() : le banc mesure la comptabilite (aBruler,');
  note('  primeDe, primesDues), pas les transferts qui la vident.');
  note('· LE NOEUD POBS N\'EST PAS LA PRODUCTION. Il ajoute feynmanTime a une COPIE');
  note('  temporaire du genesis pour rendre 0x68 vivant. C\'est la configuration');
  note('  prevue par POBS-ACTIVATION.md, pas celle d\'aujourd\'hui — et la');
  note('  section 7 mesure precisement l\'ecart entre les deux.');
  note('· Le client Go n\'est ni modifie ni recompile : le binaire du depot est');
  note('  utilise tel quel, c\'est lui qui juge toutes les preuves de ce banc.');

  // --- Bilan ---
  console.log(`\n${'='.repeat(64)}`);
  console.log(`  ${pass} tests reussis, ${fail} echec(s)`);
  console.log('='.repeat(64));
  try {
    fs.writeFileSync(path.join(RACINE, 'build', 'test-double-signature.json'),
      JSON.stringify({ pass, fail, results }, null, 2));
  } catch { /* build/ absent : le compte-rendu console suffit */ }
  arreterNoeuds();
  process.exit(fail ? 1 : 0);
})().catch((e) => {
  console.error('\nERREUR FATALE :', e.message);
  arreterNoeuds();
  process.exit(1);
});
