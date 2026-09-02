// =============================================================================
// BRC20 — l'émission est-elle DÉFINITIVEMENT close ?
//
// CE QUE CE BANC DÉFEND. `mintingFinished()` est la fonction qu'une place
// d'échange ou un agrégateur interroge pour confirmer qu'un jeton a une offre
// fixe. Il y a DEUX façons de clore l'émission, pas une : poser le drapeau avec
// `finishMinting()`, ou abandonner la propriété — puisque `mint()` est
// `onlyOwner` et que personne ne peut être le propriétaire nul.
//
// Ne rapporter que le drapeau répondait « false » sur un jeton dont l'offre
// était pourtant scellée pour toujours : une garantie sous-déclarée, sur le
// standard dont hérite CHAQUE jeton de la chaîne.
//
// COMMENT ON EXÉCUTE LE CONTRAT SANS RIEN AJOUTER. Ni hardhat, ni foundry, ni
// EVM en mémoire — et on ne peut pas en ajouter : la version de solc fige le
// bytecode du contrat système, donc le hash du bloc 0, donc l'identité de la
// chaîne. On passe donc par un `eth_call` SANS destinataire : l'EVM exécute le
// constructeur du harnais, qui déploie un BRC20, joue la séquence, et renvoie
// ses réponses. Aucun déploiement, aucune transaction, aucun compte à financer.
//
// SA VALEUR TIENT À LA MUTATION. On recompile aussi la version d'AVANT le
// correctif ; elle DOIT échouer. Un banc qui passe dans les deux cas ne prouve
// rien.
// =============================================================================

const fs = require('fs');
const path = require('path');
const os = require('os');
const { spawn, execFileSync } = require('child_process');
const solc = require('solc');
const { ethers } = require('ethers');

const RACINE = path.join(__dirname, '..');
const SRC = path.join(RACINE, 'contracts');

let pass = 0, fail = 0;
const vert = (s) => `\x1b[32m${s}\x1b[0m`, rouge = (s) => `\x1b[31m${s}\x1b[0m`;

function check(nom, obtenu, attendu) {
  const ok = String(obtenu) === String(attendu);
  ok ? pass++ : fail++;
  console.log(`  ${ok ? vert('OK  ') : rouge('ECHEC')} ${nom}` +
    (ok ? '' : `\n         attendu : ${attendu}\n         obtenu  : ${obtenu}`));
}

/// Le harnais. Son CONSTRUCTEUR fait tout le travail et renvoie le résultat :
/// c'est ce qui permet de jouer une séquence d'écritures dans un simple
/// `eth_call`, où l'état est jeté à la fin de l'appel.
const HARNAIS = `
// SPDX-License-Identifier: MIT
pragma solidity 0.8.26;
import "./BRC20.sol";
contract EssaiEmission {
    constructor() {
        BRC20 t = new BRC20("Essai", "ESS", 18, 1000, address(this));
        bool avant = t.mintingFinished();
        t.renounceOwnership();
        bool apres = t.mintingFinished();
        bool mintRefuse;
        try t.mint(address(this), 1) { mintRefuse = false; } catch { mintRefuse = true; }
        bytes memory r = abi.encode(avant, apres, mintRefuse);
        assembly { return(add(r, 32), mload(r)) }
    }
}`;

/// Compile EN MÉMOIRE, avec exactement les réglages de scripts/compile.js.
/// `sourceBRC20` permet de compiler une version MUTÉE sans rien poser sur le
/// disque : contracts/ et build/ ne sont jamais touchés.
function compiler(sourceBRC20) {
  const sources = { 'EssaiEmission.sol': { content: HARNAIS } };
  for (const f of fs.readdirSync(SRC).filter((f) => /^(BRC20|IBRC20)\.sol$/.test(f))) {
    sources[f] = { content: f === 'BRC20.sol' && sourceBRC20
      ? sourceBRC20 : fs.readFileSync(path.join(SRC, f), 'utf8') };
  }
  const out = JSON.parse(solc.compile(JSON.stringify({
    language: 'Solidity',
    sources,
    settings: {
      optimizer: { enabled: true, runs: 200 },
      evmVersion: 'shanghai',
      outputSelection: { '*': { '*': ['evm.bytecode.object'] } },
    },
  })));
  const errs = (out.errors || []).filter((e) => e.severity === 'error');
  if (errs.length) { errs.forEach((e) => console.error(e.formattedMessage)); process.exit(1); }
  return '0x' + out.contracts['EssaiEmission.sol']['EssaiEmission'].evm.bytecode.object;
}

/// Un nœud sur le VRAI genesis, qui ne mine jamais et reste au bloc 0 : un
/// moteur EVM, rien d'autre. `--ipcdisable` parce que le chemin du socket
/// dépasse la limite Unix de 103 caractères sous un répertoire temporaire.
let noeud = null;
function demarrerNoeud() {
  const geth = process.env.GETH
    || [path.join(RACINE, '..', 'geth'), path.join(RACINE, 'build', 'bin', 'geth')]
       .find((p) => fs.existsSync(p));
  if (!geth) throw new Error('binaire geth introuvable — définir GETH=/chemin/vers/geth');
  const genesis = path.join(RACINE, 'genesis', 'genesis-coinbosa.json');
  const dd = fs.mkdtempSync(path.join(os.tmpdir(), 'brc20-essai-'));
  const port = Number(process.env.RPC_PORT || 8623);
  execFileSync(geth, ['init', '--datadir', dd, genesis], { stdio: 'ignore' });
  noeud = spawn(geth, ['--datadir', dd, '--datadir.minfreedisk', '0', '--networkid', '26262',
    '--http', '--http.addr', '127.0.0.1', '--http.port', String(port), '--http.api', 'eth,net,web3',
    '--nodiscover', '--maxpeers', '0', '--ipcdisable', '--port', '30623', '--verbosity', '1'],
    { stdio: 'ignore' });
  return `http://127.0.0.1:${port}`;
}
function arreterNoeud() { if (noeud) { try { noeud.kill('SIGKILL'); } catch { /* déjà mort */ } } }

async function attendre(rpc) {
  for (let i = 0; i < 60; i++) {
    try {
      const r = await fetch(rpc, { method: 'POST', headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'eth_blockNumber', params: [] }) });
      if ((await r.json()).result !== undefined) return;
    } catch { /* pas encore prêt */ }
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error('le nœud ne répond pas');
}

/// Exécute le constructeur du harnais et décode ses trois réponses.
async function jouer(rpc, bytecode) {
  const r = await fetch(rpc, { method: 'POST', headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'eth_call',
      params: [{ data: bytecode, gas: '0x1c9c380' }, 'latest'] }) });
  const j = await r.json();
  if (j.error) throw new Error('eth_call : ' + j.error.message);
  const [avant, apres, mintRefuse] =
    ethers.AbiCoder.defaultAbiCoder().decode(['bool', 'bool', 'bool'], j.result);
  return { avant, apres, mintRefuse };
}

(async () => {
  const rpc = demarrerNoeud();
  await attendre(rpc);
  console.log('\nBRC20 — clôture de l\'émission\n');

  console.log('  --- 1. le contrat tel qu\'il est ---');
  const actuel = await jouer(rpc, compiler(null));
  check('avant tout, l\'émission est ouverte', actuel.avant, false);
  check('après renounceOwnership, mintingFinished() dit TRUE', actuel.apres, true);
  check('et mint() est bien refusé', actuel.mintRefuse, true);

  // --- 2. mutation : on remet la version d'avant le correctif -------------
  // Sans cette section, rien ne prouve que le banc attrape quoi que ce soit.
  console.log('\n  --- 2. mutation : mintingFinished() ne rapporte que le drapeau ---');
  const source = fs.readFileSync(path.join(SRC, 'BRC20.sol'), 'utf8');
  const mute = source.replace(
    'return _mintingFinished || _owner == address(0);', 'return _mintingFinished;');
  if (mute === source) { console.error('  ancre de mutation introuvable'); process.exit(1); }
  const avantCorrectif = await jouer(rpc, compiler(mute));
  check('la version mutée sous-déclare la garantie', avantCorrectif.apres, false);
  check('alors que mint() est pourtant refusé', avantCorrectif.mintRefuse, true);

  console.log(`\n${'='.repeat(56)}\n  ${pass} tests reussis, ${fail} echec(s)\n${'='.repeat(56)}`);
  arreterNoeud();
  process.exit(fail ? 1 : 0);
})().catch((e) => { console.error('\nERREUR FATALE :', e.message); arreterNoeud(); process.exit(1); });
