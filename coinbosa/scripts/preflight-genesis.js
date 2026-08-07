// Contrôle AVANT VOL du genesis de production — verdict GO / NO-GO.
//
//   VALIDATOR=0x… GOVERNOR=0x… VALIDATORS=0xa,0xb,0xc,0xd node scripts/preflight-genesis.js
//
// Le genesis fixe l'offre, les détenteurs et l'identité de la chaîne POUR TOUJOURS : il n'y
// a pas de « deuxième essai » une fois le réseau public. Ce script rassemble en un seul
// endroit tout ce qui doit être vrai avant de le produire, et refuse de dire GO tant qu'un
// point n'est pas rempli.
//
// Il ne remplace pas le jugement : certaines conditions (les adresses sont-elles VRAIMENT
// des coffres multi-signatures ? le burn Solana est-il publié ?) ne sont pas vérifiables
// depuis cette machine. Elles sont listées à part, comme attestations explicites à fournir,
// jamais cochées d'office.
const fs = require('fs');
const path = require('path');
const { ethers } = require('ethers');

const ROOT = path.join(__dirname, '..');
const CONFIG = JSON.parse(fs.readFileSync(path.join(ROOT, 'coinbosa.config.json'), 'utf8'));
const ADDR = JSON.parse(fs.readFileSync(path.join(ROOT, 'genesis', 'distribution-addresses.json'), 'utf8'));
const ZERO = '0x0000000000000000000000000000000000000000';
const MIN_VALIDATORS = Number(process.env.MIN_VALIDATORS || 4);

const bloquants = [];
const avertissements = [];
const ok = [];
const A = (m) => ok.push(m);
const X = (m) => bloquants.push(m);
const W = (m) => avertissements.push(m);

console.log('\n  CONTRÔLE AVANT CRÉATION DU GENESIS DE PRODUCTION');
console.log('  ' + '='.repeat(66));

// ── 1. Adresses de la répartition ────────────────────────────────────────────
const postes = Object.entries(CONFIG.distribution).filter(([k]) => !k.startsWith('$'));
const reserve = BigInt(CONFIG.migration.reserve);
const attendus = postes.map(([k]) => k).concat(reserve > 0n ? ['__migration__'] : []);

const vues = new Map();
let adressesOk = 0;
for (const poste of attendus) {
  const a = ADDR[poste];
  if (!a || a === ZERO || /^0x0+$/.test(a)) { X(`adresse non renseignée pour le poste « ${poste} »`); continue; }
  if (!ethers.isAddress(a)) { X(`adresse invalide pour « ${poste} » : ${a}`); continue; }
  // EIP-55 : une adresse mal recopiée passe souvent le test de longueur mais pas la somme
  // de contrôle. Sur un transfert de 140 millions de coins, ce contrôle n'est pas cosmétique.
  try {
    if (ethers.getAddress(a) !== a && a !== a.toLowerCase()) {
      X(`somme de contrôle EIP-55 invalide pour « ${poste} » : ${a} (adresse probablement mal recopiée)`);
      continue;
    }
  } catch { X(`adresse illisible pour « ${poste} » : ${a}`); continue; }
  const norm = a.toLowerCase();
  if (vues.has(norm)) { X(`adresse partagée par « ${poste} » et « ${vues.get(norm)} » : ${a} — chaque poste doit avoir la sienne`); continue; }
  vues.set(norm, poste);
  adressesOk++;
}
if (adressesOk === attendus.length) A(`${adressesOk} adresses de répartition renseignées, valides, distinctes`);

// Adresses de développement : build-genesis les dérive de « coinbosa-dev:<poste> ».
// Si l'une d'elles se retrouvait dans le fichier de production, l'offre serait envoyée à
// une adresse dont personne ne détient la clé — perte définitive.
for (const poste of attendus) {
  const a = ADDR[poste];
  if (!a || !ethers.isAddress(a)) continue;
  const derive = ethers.getAddress('0x' + ethers.id('coinbosa-dev:' + poste).slice(-40));
  if (a.toLowerCase() === derive.toLowerCase()) X(`« ${poste} » porte une adresse de DÉVELOPPEMENT non dépensable (${a}) — les fonds seraient perdus`);
}

// ── 2. Arithmétique de l'offre ───────────────────────────────────────────────
const total = BigInt(CONFIG.nativeCoin.totalSupply);
const projet = BigInt(CONFIG.projectAllocation.amount);
const somme = postes.reduce((s, [, p]) => s + p, 0);
if (somme !== 100) X(`la répartition totalise ${somme} %, 100 attendu`); else A('répartition à 100 %');
if (reserve + projet !== total) X(`réserve (${reserve}) + allocation projet (${projet}) ≠ offre totale (${total})`);
else A(`offre cohérente : ${total.toLocaleString('fr-FR')} BOSA`);
// Une part qui ne tombe pas juste laisserait des wei orphelins.
for (const [poste, pct] of postes) {
  if ((projet * BigInt(pct)) % 100n !== 0n) X(`la part de « ${poste} » (${pct} %) ne divise pas exactement l'allocation`);
}

// ── 3. Séparation des rôles ──────────────────────────────────────────────────
const VALIDATOR = process.env.VALIDATOR;
const GOVERNOR = process.env.GOVERNOR;
if (!VALIDATOR || !ethers.isAddress(VALIDATOR)) X('VALIDATOR absent ou invalide');
if (!GOVERNOR || !ethers.isAddress(GOVERNOR)) X('GOVERNOR absent ou invalide (doit être un coffre multi-signatures)');
if (VALIDATOR && GOVERNOR && ethers.isAddress(VALIDATOR) && ethers.isAddress(GOVERNOR)) {
  if (VALIDATOR.toLowerCase() === GOVERNOR.toLowerCase()) X('GOVERNOR identique au VALIDATOR — la clé de scellage est en ligne en permanence, elle ne doit pas gouverner la chaîne');
  else A('gouverneur distinct de la clé de scellage');
  for (const [norm, poste] of vues) {
    if (norm === VALIDATOR.toLowerCase()) X(`le VALIDATOR est aussi l'adresse de trésorerie « ${poste} » — séparer scellage et détention des fonds`);
    if (norm === GOVERNOR.toLowerCase()) W(`le GOVERNOR est aussi l'adresse de trésorerie « ${poste} » — acceptable si c'est le coffre principal, à confirmer explicitement`);
  }
}

// ── 4. Nombre de validateurs ─────────────────────────────────────────────────
const liste = (process.env.VALIDATORS || '').split(',').map((s) => s.trim()).filter(Boolean);
if (!liste.length) {
  X(`aucun validateur listé (VALIDATORS=0xa,0xb,…). Il en faut au moins ${MIN_VALIDATORS} à clés séparées : avec un seul, le réseau n'a ni tolérance aux pannes ni sécurité byzantine`);
} else {
  const invalides = liste.filter((a) => !ethers.isAddress(a));
  if (invalides.length) X(`validateurs invalides : ${invalides.join(', ')}`);
  const uniques = new Set(liste.map((a) => a.toLowerCase()));
  if (uniques.size !== liste.length) X('la liste des validateurs contient des doublons — chaque validateur doit avoir sa propre clé');
  if (uniques.size < MIN_VALIDATORS) X(`${uniques.size} validateur(s) distinct(s), ${MIN_VALIDATORS} exigés au minimum`);
  else A(`${uniques.size} validateur(s) distinct(s)`);
  // Abaisser le seuil est possible, mais ne doit JAMAIS ressembler à un contrôle propre :
  // un réseau à moins de 4 validateurs n'a ni tolérance aux pannes ni sécurité byzantine.
  if (MIN_VALIDATORS < 4) {
    W(`seuil de validateurs ABAISSÉ à ${MIN_VALIDATORS} (défaut : 4). Avec ${uniques.size} validateur(s), ` +
      'le réseau s\'arrête si la machine tombe et un seul opérateur produit tous les blocs. ' +
      'Ce n\'est PAS un réseau décentralisé : cela doit être écrit publiquement, pas découvert par un tiers.');
  }
}

// ── 5. Compilateur ───────────────────────────────────────────────────────────
try {
  const solc = require('solc');
  if (solc.version().startsWith('0.8.26')) A(`solc ${solc.version().split('+')[0]} (version épinglée)`);
  else X(`solc ${solc.version()} — 0.8.26 attendu ; le bytecode du contrat système fixe le hash du bloc 0`);
} catch { X('solc introuvable — lancer npm ci'); }

// ── 6. Empreinte de référence ────────────────────────────────────────────────
const refPath = path.join(ROOT, 'genesis', 'genesis-reference.json');
if (fs.existsSync(refPath)) {
  const ref = JSON.parse(fs.readFileSync(refPath, 'utf8'));
  if (ref.hash && !/^0x0*$/.test(ref.hash)) W(`genesis-reference.json contient DÉJÀ une empreinte (${ref.hash.slice(0, 18)}…) — si tu régénères le genesis, elle ne correspondra plus. La remettre à 0x0, ou vérifier que c'est bien la chaîne voulue`);
  else A('genesis-reference.json prêt à recevoir l\'empreinte du bloc 0');
} else X('genesis/genesis-reference.json absent');

// ── 7. Ce qui ne se vérifie pas depuis cette machine ─────────────────────────
const attestations = [
  'chaque adresse de répartition est un COFFRE MULTI-SIGNATURES (seuil ≥ 2 sur N), pas une clé unique',
  'le gouverneur est un multi-signatures, idéalement derrière un délai (timelock)',
  'les clés de validateur ont été générées SUR LEURS SERVEURS respectifs, jamais copiées depuis un poste de travail',
  `le retrait de circulation des ${BigInt(CONFIG.migration.sourceSupply).toLocaleString('fr-FR')} jetons Solana est PROUVÉ par une transaction publique vérifiable`,
  'un audit externe du contrat système et du genesis a été rendu',
  'les sauvegardes des clés (phrases de récupération) sont testées et conservées hors ligne',
];

// ── Verdict ──────────────────────────────────────────────────────────────────
console.log('\n  Vérifié :');
ok.forEach((m) => console.log(`    ✓ ${m}`));
if (avertissements.length) {
  console.log('\n  À confirmer :');
  avertissements.forEach((m) => console.log(`    ~ ${m}`));
}
if (bloquants.length) {
  console.log('\n  Bloquants :');
  bloquants.forEach((m) => console.log(`    ✗ ${m}`));
}
console.log('\n  Attestations à fournir explicitement (non vérifiables depuis cette machine) :');
attestations.forEach((m) => console.log(`    ? ${m}`));

console.log('\n  ' + '='.repeat(66));
if (bloquants.length) {
  console.log(`  VERDICT : NO-GO — ${bloquants.length} point(s) bloquant(s). Le genesis de production NE DOIT PAS être produit.\n`);
  process.exit(1);
}
console.log('  VERDICT : GO sur les points automatiquement vérifiables.');
console.log('  Les attestations ci-dessus restent sous la responsabilité de l\'éditeur.\n');
