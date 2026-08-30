// Confirme — ou infirme — que les adresses INSCRITES AU BLOC 0 et le gouverneur lu sur la
// chaîne descendent bien du xpub de trésorerie, donc d'une seule graine.
//
//   cd coinbosa
//   read -rsp 'xpub de compte : ' XPUB && echo
//   XPUB="$XPUB" RPC=https://explorer.coinbosa.com/rpc node scripts/check-treasury-derivation.js
//   unset XPUB
//
// C'est la question que pose en premier l'analyse de risque d'une place d'échange, et elle
// se tranche SANS MANIPULER LE MOINDRE SECRET : un xpub est une clé PUBLIQUE. Aucune clé
// privée n'est lue, aucune transaction n'est émise, aucun fichier n'est écrit.
//
// `read -rs` évite que le xpub finisse dans l'historique du shell. Le xpub reste néanmoins
// un élément sensible : la dérivation est NON DURCIE, donc xpub + une seule clé privée
// enfant reconstituent les quatorze clés. Voir GARDE-TRESORERIE.md, section 2.2.
const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

const XPUB = process.env.XPUB;
const RPC = process.env.RPC || 'http://127.0.0.1:8545';
const ROOT = path.join(__dirname, '..');
const CONFIG = JSON.parse(fs.readFileSync(path.join(ROOT, 'coinbosa.config.json'), 'utf8'));
const ADDRS = JSON.parse(fs.readFileSync(path.join(ROOT, 'genesis', 'distribution-addresses.json'), 'utf8'));
const VALSET = '0x0000000000000000000000000000000000001000';

if (!XPUB) {
  console.error("XPUB manquant.\n  read -rsp 'xpub : ' XPUB && echo && XPUB=\"$XPUB\" node scripts/check-treasury-derivation.js");
  process.exit(1);
}
// Même garde que derive-treasury-addresses.js : refuser tout ce qui ressemble à un secret.
if (/^(0x)?[0-9a-fA-F]{64}$/.test(XPUB.trim()) || XPUB.trim().split(/\s+/).length >= 12) {
  console.error('ARRÊT : ceci ressemble à une CLÉ PRIVÉE ou à une PHRASE DE RÉCUPÉRATION.');
  console.error('  Ce contrôle n\'a besoin que de la clé PUBLIQUE étendue (xpub…).');
  process.exit(1);
}

let noeud;
try { noeud = ethers.HDNodeWallet.fromExtendedKey(XPUB.trim()); }
catch (e) { console.error('xpub illisible :', e.message); process.exit(1); }
if (noeud.privateKey) { console.error('ARRÊT : clé étendue PRIVÉE fournie. Fournir le xpub (neutre).'); process.exit(1); }

(async () => {
  const p = new ethers.JsonRpcProvider(RPC, undefined, { staticNetwork: true });
  const reseau = await p.getNetwork();
  if (reseau.chainId !== BigInt(CONFIG.network.chainId)) {
    console.error(`ÉCHEC : chainId ${reseau.chainId}, attendu ${CONFIG.network.chainId}.`);
    process.exit(1);
  }
  const gouverneur = await new ethers.Contract(VALSET, ['function GOVERNOR() view returns (address)'], p).GOVERNOR();

  const postes = Object.keys(ADDRS).filter((k) => !k.startsWith('$') && ADDRS[k] !== ethers.ZeroAddress
    && !(k === '__migration__'));
  console.log(`\n  Filiation des adresses — chemin m/44'/60'/0'/0/i`);
  console.log('  ' + '='.repeat(92));

  let conformes = 0, ecarts = 0;
  postes.forEach((poste, i) => {
    const derivee = ethers.getAddress(noeud.derivePath(`0/${i}`).address);
    const inscrite = ethers.getAddress(ADDRS[poste]);
    const ok = derivee === inscrite;
    ok ? conformes++ : ecarts++;
    console.log(`  0/${String(i).padEnd(2)}  ${poste.padEnd(26)} ${inscrite}  ${ok ? 'conforme' : 'ÉCART -> ' + derivee}`);
  });

  const govDerive = ethers.getAddress(noeud.derivePath(`0/${postes.length}`).address);
  const govOk = govDerive === ethers.getAddress(gouverneur);
  console.log('  ' + '-'.repeat(92));
  console.log(`  0/${String(postes.length).padEnd(2)}  ${'GOUVERNEUR (sur la chaîne)'.padEnd(26)} ${ethers.getAddress(gouverneur)}  ${govOk ? 'conforme' : 'ÉCART -> ' + govDerive}`);
  console.log('  ' + '='.repeat(92));
  console.log(`\n  postes conformes : ${conformes}/${postes.length}   écarts : ${ecarts}   gouverneur : ${govOk ? 'conforme' : 'ÉCART'}`);

  if (ecarts === 0 && govOk) {
    console.log('\n  RÉSULTAT : les ' + (postes.length + 1) + ' adresses descendent du MÊME nœud de compte,');
    console.log('  donc de la MÊME graine. La trésorerie et le gouverneur tombent ensemble.');
    console.log('  C\'est le fait à divulguer, pas à taire — GARDE-TRESORERIE.md le fait.\n');
    process.exit(0);
  }
  console.log('\n  RÉSULTAT : la filiation n\'est PAS celle que décrit la procédure du dépôt.');
  console.log('  Ne rien conclure de rassurant : établir d\'où viennent réellement ces adresses,');
  console.log('  et corriger GARDE-TRESORERIE.md en conséquence.\n');
  process.exit(1);
})().catch((e) => { console.error('ÉCHEC : ' + (e.shortMessage || e.message)); process.exit(1); });
