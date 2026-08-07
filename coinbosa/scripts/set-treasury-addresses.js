// Renseigne les adresses de répartition à partir d'une liste fournie à la main.
//
//   node scripts/set-treasury-addresses.js adresses.txt
//   node scripts/set-treasury-addresses.js adresses.txt --ecrire
//
// À utiliser quand les adresses viennent d'un portefeuille qui n'expose pas de clé
// publique étendue (Trust Wallet, MetaMask, un échange…). Le fichier d'entrée contient
// une adresse par ligne, dans l'ordre des postes affichés ci-dessous ; les lignes vides et
// celles commençant par # sont ignorées.
//
// Ce script ne fabrique aucune clé et n'en manipule aucune : il ne fait que vérifier des
// adresses publiques, puis les inscrire. Il refuse d'écrire si le moindre contrôle échoue —
// une erreur ici est définitive une fois le bloc 0 produit.
const fs = require('fs');
const path = require('path');
const { ethers } = require('ethers');

const ROOT = path.join(__dirname, '..');
const CONFIG = JSON.parse(fs.readFileSync(path.join(ROOT, 'coinbosa.config.json'), 'utf8'));
const CIBLE = path.join(ROOT, 'genesis', 'distribution-addresses.json');
const ECRIRE = process.argv.includes('--ecrire');
const FICHIER = process.argv.slice(2).find((a) => !a.startsWith('--'));

const postes = Object.keys(CONFIG.distribution).filter((k) => !k.startsWith('$'));
const reserve = BigInt(CONFIG.migration.reserve);
const cibles = postes.concat(reserve > 0n ? ['__migration__'] : []);
const projet = BigInt(CONFIG.projectAllocation.amount);

if (!FICHIER) {
  console.log('\n  Fournis un fichier contenant UNE ADRESSE PAR LIGNE, dans cet ordre :\n');
  cibles.forEach((poste, i) => {
    const pct = CONFIG.distribution[poste];
    const montant = pct ? (projet * BigInt(pct) / 100n) : reserve;
    console.log(`   ${String(i + 1).padStart(2)}. ${poste.padEnd(26)} ${String(pct ? pct + ' %' : 'réserve').padStart(8)}  ${montant.toLocaleString('fr-FR').padStart(13)} BOSA`);
  });
  console.log(`   ${String(cibles.length + 1).padStart(2)}. GOUVERNEUR (contrat système)      — ne détient pas de fonds, gouverne le consensus`);
  console.log('\n  Puis :  node scripts/set-treasury-addresses.js adresses.txt');
  console.log('  Rien n\'est écrit sans --ecrire.\n');
  process.exit(0);
}

const lignes = fs.readFileSync(FICHIER, 'utf8')
  .split('\n').map((l) => l.trim())
  .filter((l) => l && !l.startsWith('#'));

const attendu = cibles.length + 1; // postes + gouverneur
const erreurs = [];
if (lignes.length !== attendu) {
  erreurs.push(`${lignes.length} adresse(s) fournie(s), ${attendu} attendues (${cibles.length} postes + 1 gouverneur)`);
}

// Refus dur de tout ce qui ressemble à un secret : une phrase de récupération ou une clé
// privée dans un fichier texte serait le pire des accidents.
for (const l of lignes) {
  if (/^(0x)?[0-9a-fA-F]{64}$/.test(l) || l.split(/\s+/).length >= 12) {
    console.error('\nARRÊT : le fichier contient ce qui ressemble à une CLÉ PRIVÉE ou à une PHRASE DE RÉCUPÉRATION.');
    console.error('  Ce fichier ne doit contenir que des ADRESSES publiques (0x… sur 42 caractères).');
    console.error('  Supprime ce fichier, et considère le secret comme compromis.\n');
    process.exit(1);
  }
}

const vues = new Map();
const valides = [];
lignes.forEach((brut, i) => {
  const nom = i < cibles.length ? cibles[i] : 'GOUVERNEUR';
  if (!ethers.isAddress(brut)) { erreurs.push(`ligne ${i + 1} (${nom}) : « ${brut} » n'est pas une adresse valide`); return; }
  let a;
  try { a = ethers.getAddress(brut); } catch { erreurs.push(`ligne ${i + 1} (${nom}) : adresse illisible`); return; }
  // Une adresse recopiée à la main peut avoir la bonne longueur mais une casse fausse :
  // si l'entrée est en casse mixte, elle doit passer la somme de contrôle EIP-55.
  if (brut !== brut.toLowerCase() && brut !== a) {
    erreurs.push(`ligne ${i + 1} (${nom}) : somme de contrôle EIP-55 invalide — adresse probablement mal recopiée`);
    return;
  }
  if (/^0x0+$/.test(a)) { erreurs.push(`ligne ${i + 1} (${nom}) : adresse nulle`); return; }
  const cle = a.toLowerCase();
  if (vues.has(cle)) { erreurs.push(`ligne ${i + 1} (${nom}) : adresse déjà utilisée par « ${vues.get(cle)} » — chaque poste doit avoir la sienne`); return; }
  vues.set(cle, nom);
  valides.push({ nom, adresse: a, index: i });
});

console.log('\n  Adresses fournies');
console.log('  ' + '='.repeat(86));
for (const v of valides) {
  const pct = CONFIG.distribution[v.nom];
  const montant = v.index < cibles.length ? (pct ? (projet * BigInt(pct) / 100n) : reserve) : null;
  console.log(`  ${String(v.index + 1).padStart(2)}. ${v.nom.padEnd(26)} ${v.adresse}  ${montant !== null ? montant.toLocaleString('fr-FR').padStart(13) + ' BOSA' : '(gouverneur)'}`);
}
console.log('  ' + '='.repeat(86));

if (erreurs.length) {
  console.error('\n  ECHEC — rien n\'a été écrit :');
  erreurs.forEach((e) => console.error(`    ✗ ${e}`));
  console.error('');
  process.exit(1);
}

console.log(`\n  ${valides.length} adresses valides, toutes distinctes.`);
console.log('\n  AVANT DE CONTINUER — vérifie que tu peux VRAIMENT récupérer chacune :');
console.log('    · la phrase de récupération de chaque portefeuille est sauvegardée hors ligne ;');
console.log('    · tu as testé une restauration sur un appareil vierge, au moins une fois ;');
console.log('    · une adresse dont tu perds la clé emporte sa part DÉFINITIVEMENT.');

if (!ECRIRE) {
  console.log('\n  (rien n\'a été écrit — relancer avec --ecrire pour renseigner distribution-addresses.json)\n');
  process.exit(0);
}

const actuel = JSON.parse(fs.readFileSync(CIBLE, 'utf8'));
for (const v of valides) if (v.index < cibles.length) actuel[v.nom] = v.adresse;
fs.writeFileSync(CIBLE, JSON.stringify(actuel, null, 2) + '\n');
const gouverneur = valides.find((v) => v.nom === 'GOUVERNEUR');
console.log(`\n  écrit -> ${path.relative(ROOT, CIBLE)}`);
console.log(`\n  Pour produire le genesis :\n    VALIDATOR=0x… GOVERNOR=${gouverneur ? gouverneur.adresse : '0x…'} node scripts/build-genesis.js`);
console.log('  Lance d\'abord preflight-genesis.js : il dira GO ou NO-GO.\n');
