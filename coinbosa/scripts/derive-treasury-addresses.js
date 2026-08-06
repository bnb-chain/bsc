// Dérive les 13 adresses de répartition depuis une clé publique étendue (xpub).
//
//   XPUB=xpub6... node scripts/derive-treasury-addresses.js
//
// Pourquoi passer par un xpub
// ---------------------------
// Un xpub est une clé PUBLIQUE étendue : il permet de calculer des adresses, jamais de
// signer. Il ne donne aucun pouvoir de dépense. On peut donc l'exporter d'un portefeuille
// matériel, le poser sur cette machine, le mettre dans un ticket — sans risque.
//
// Conséquence directe : la phrase de récupération ne quitte JAMAIS l'appareil, et aucune
// clé privée n'existe côté logiciel. C'est ce qui distingue une trésorerie sérieuse d'un
// tas de clés générées sur un poste de travail.
//
// Comment obtenir le xpub (Ledger / Trezor) :
//   - chemin de compte Ethereum : m/44'/60'/0'
//   - exporter la clé publique étendue de CE compte (pas d'un autre chemin)
//
// Chaque adresse produite ici DOIT être vérifiée sur l'écran de l'appareil avant que le
// genesis ne soit figé : une adresse fausse, et la part correspondante est perdue à jamais.
const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

const XPUB = process.env.XPUB;
const ROOT = path.join(__dirname, '..');
const CONFIG = JSON.parse(fs.readFileSync(path.join(ROOT, 'coinbosa.config.json'), 'utf8'));
const ECRIRE = process.env.ECRIRE === '1';

if (!XPUB) {
  console.error('XPUB manquant.\n  XPUB=xpub6… node scripts/derive-treasury-addresses.js');
  console.error('\n  Le xpub est une clé PUBLIQUE : il ne permet pas de dépenser.');
  console.error('  Ne colle JAMAIS une phrase de récupération ni une clé privée ici.');
  process.exit(1);
}
// Garde : refuser tout ce qui ressemble à un secret.
if (/^(0x)?[0-9a-fA-F]{64}$/.test(XPUB.trim()) || XPUB.trim().split(/\s+/).length >= 12) {
  console.error('ARRÊT : ceci ressemble à une CLÉ PRIVÉE ou à une PHRASE DE RÉCUPÉRATION.');
  console.error('  Ne les expose jamais à un script. Fournis la clé publique étendue (xpub…).');
  process.exit(1);
}

let noeud;
try {
  noeud = ethers.HDNodeWallet.fromExtendedKey(XPUB.trim());
} catch (e) {
  console.error('xpub illisible :', e.message);
  process.exit(1);
}

const postes = Object.keys(CONFIG.distribution).filter((k) => !k.startsWith('$'));
const reserve = BigInt(CONFIG.migration.reserve);
const cibles = postes.concat(reserve > 0n ? ['__migration__'] : []);
const projet = BigInt(CONFIG.projectAllocation.amount);

console.log('\n  Adresses dérivées depuis le xpub fourni — chemin m/44\'/60\'/0\'/0/i');
console.log('  ' + '='.repeat(88));

const sortie = {};
const vues = new Set();
cibles.forEach((poste, i) => {
  // 0/i à partir du xpub de compte : ce sont les adresses de réception standard.
  const enfant = noeud.derivePath(`0/${i}`);
  const adresse = ethers.getAddress(enfant.address);
  if (vues.has(adresse)) { console.error(`ERREUR : collision d'adresse à l'index ${i}`); process.exit(1); }
  vues.add(adresse);
  sortie[poste] = adresse;
  const pct = CONFIG.distribution[poste];
  const montant = pct ? (projet * BigInt(pct) / 100n) : reserve;
  console.log(`  0/${String(i).padEnd(2)}  ${poste.padEnd(26)} ${String(pct ? pct + ' %' : 'réserve').padStart(8)}  ${adresse}  ${montant.toLocaleString('fr-FR').padStart(13)} BOSA`);
});

// Le gouverneur du contrat système sur un index dédié, hors des postes de trésorerie.
// Le placer sur le portefeuille matériel est bien plus solide qu'une clé chaude : modifier
// l'ensemble des validateurs exigera l'appareil physique. Il est distinct de la clé de
// scellage, qui vit en ligne sur le serveur du validateur (build-genesis.js refuse d'ailleurs
// en production un gouverneur égal au validateur).
const GOV_INDEX = cibles.length;
const gouverneur = ethers.getAddress(noeud.derivePath(`0/${GOV_INDEX}`).address);
console.log('  ' + '-'.repeat(88));
console.log(`  0/${String(GOV_INDEX).padEnd(2)}  ${'GOUVERNEUR (contrat système)'.padEnd(26)} ${''.padStart(8)}  ${gouverneur}`);
console.log('  ' + '='.repeat(88));
console.log(`  ${cibles.length} adresses de répartition + 1 gouverneur, toutes distinctes.`);
console.log(`\n  À utiliser au moment de produire le genesis :\n    GOVERNOR=${gouverneur}`);
console.log('\n  AVANT DE FIGER LE GENESIS :');
console.log('   1. vérifier CHAQUE adresse sur l\'écran de l\'appareil (index 0/0 à 0/' + (cibles.length - 1) + ')');
console.log('   2. tester une récupération de la phrase sur un appareil vierge');
console.log('   3. seulement ensuite, lancer preflight-genesis.js');

if (ECRIRE) {
  const cible = path.join(ROOT, 'genesis', 'distribution-addresses.json');
  const actuel = JSON.parse(fs.readFileSync(cible, 'utf8'));
  for (const [poste, adresse] of Object.entries(sortie)) actuel[poste] = adresse;
  fs.writeFileSync(cible, JSON.stringify(actuel, null, 2) + '\n');
  console.log(`\n  écrit -> ${path.relative(ROOT, cible)}`);
} else {
  console.log('\n  (rien n\'a été écrit — relancer avec ECRIRE=1 pour renseigner distribution-addresses.json)');
}
