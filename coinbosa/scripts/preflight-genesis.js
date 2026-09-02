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
//
// RÈGLE DE CONCEPTION — c'est son absence qui avait produit un faux vert
// ----------------------------------------------------------------------
// Un point qui n'a RIEN pu examiner ne sort JAMAIS en ✓. Une liste vide, un seuil illisible,
// un fichier absent, une valeur qui n'est confrontée à aucune autre source : tout cela est
// un contrôle IMPOSSIBLE, et un contrôle impossible se déclare bloquant, pas vert. Un
// contrôle qui ment est pire que pas de contrôle : il fait cesser la vigilance.
//
// LES ACCIDENTS QUE CE SCRIPT DOIT ATTRAPER
// -----------------------------------------
//  1. Une adresse de trésorerie mal recopiée : 140 000 000 BOSA envoyés à une adresse dont
//     personne ne détient la clé (§ 1 — forme EIP-55 EXIGÉE, pas seulement tolérée).
//  2. Une adresse de développement non dépensable laissée en production (§ 1).
//  3. Une répartition qui ne boucle pas, ou des wei orphelins par arrondi (§ 2).
//  4. La clé de scellage, en ligne en permanence, promue gouverneur de la chaîne (§ 3).
//  5. Une déclaration de validateurs qui n'est qu'un écho du shell de l'opérateur (§ 4).
//  6. Un compilateur autre que celui qui a produit le bytecode publié (§ 5).
//  7. Le contrat système ou l'état initial modifiés depuis le gel : le hash du bloc 0
//     changerait, donc l'identité de la chaîne (§ 5 bis).
//  8. Un environnement encore armé pour le développement : le GO porterait sur un genesis
//     que la commande suivante ne produira pas (§ 0).
const fs = require('fs');
const path = require('path');
const { ethers } = require('ethers');

const ROOT = path.join(__dirname, '..');
const CONFIG = JSON.parse(fs.readFileSync(path.join(ROOT, 'coinbosa.config.json'), 'utf8'));
const ADDR = JSON.parse(fs.readFileSync(path.join(ROOT, 'genesis', 'distribution-addresses.json'), 'utf8'));
const ZERO = '0x0000000000000000000000000000000000000000';

const bloquants = [];
const avertissements = [];
const ok = [];
const A = (m) => ok.push(m);
const X = (m) => bloquants.push(m);
const W = (m) => avertissements.push(m);

console.log('\n  CONTRÔLE AVANT CRÉATION DU GENESIS DE PRODUCTION');
console.log('  ' + '='.repeat(66));

// ── 0. L'environnement doit être désarmé ─────────────────────────────────────
// L'accident : la procédure documentée enchaîne le préflight (étape 6) puis build-genesis
// (étape 7) DANS LE MÊME TERMINAL. Une variable restée d'une vérification locale est
// honorée par les scripts suivants — build-genesis.js lit ALLOW_DEV, BASE et OUT, et
// écrirait alors un genesis de développement, sur un autre chemin, à partir d'un autre
// état initial. Le GO rendu ici porterait sur un objet que personne ne va produire.
// Un contrôle ne peut pas certifier ce qui ne sera pas construit.
const VARS_A_DESARMER = {
  ALLOW_DEV: 'build-genesis.js écrirait genesis-coinbosa-dev.json avec des adresses synthétiques NON DÉPENSABLES, pas le genesis de production',
  BASE: 'build-genesis.js partirait d\'un AUTRE état initial que genesis/genesis-base.json — chainId, gasLimit et contrats système compris',
  OUT: 'le genesis serait écrit ailleurs que genesis/genesis-coinbosa.json, et la suite de la procédure vérifierait un autre fichier',
  GENESIS: 'check-supply.js (étape 8) contrôlerait un autre fichier que le genesis de production',
  GENESIS_REF: 'check-genesis-hash.js (étape 8) comparerait à une autre empreinte que genesis/genesis-reference.json',
  ALLOW_DEV_SUPPLY: 'check-supply.js accepterait un genesis portant le marqueur coinbosaDev, c\'est-à-dire un genesis de développement en production',
  ALLOW_DEV_HASH: 'check-genesis-hash.js cesserait d\'exiger la concordance avec l\'empreinte figée',
};
for (const [nom, effet] of Object.entries(VARS_A_DESARMER)) {
  const valeur = process.env[nom];
  if (valeur !== undefined && valeur !== '') {
    X(`${nom}=${valeur} est armé dans l'environnement : ${effet}. Faire « unset ${nom} » puis relancer ce contrôle.`);
  }
}

// Seuil de validateurs exigé. L'accident : Number('quatre') vaut NaN, et TOUTE comparaison
// avec NaN est fausse — « 1 < NaN » comme « NaN < 4 ». Une faute de frappe faisait donc
// disparaître à la fois le refus et l'avertissement obligatoire, et un réseau à un seul
// validateur ressortait en ✓. On refuse un seuil qu'on ne sait pas lire, au lieu de le
// comparer : une valeur non comparable ne doit jamais pouvoir choisir la branche verte.
const MIN_BRUT = process.env.MIN_VALIDATORS;
let MIN_VALIDATORS = 4;
if (MIN_BRUT !== undefined && MIN_BRUT.trim() !== '') {
  const brut = MIN_BRUT.trim();
  if (!/^[0-9]+$/.test(brut) || Number(brut) < 1) {
    MIN_VALIDATORS = null;   // seuil indéfini : aucun comptage ne pourra conclure
    X(`MIN_VALIDATORS = « ${MIN_BRUT} » n'est pas un entier ≥ 1 : le seuil de validateurs est indéfini et le comptage du § 4 ne conclut rien. Corriger la valeur (par exemple MIN_VALIDATORS=1, qui déclenche l'avertissement d'abaissement) ou retirer la variable pour retomber sur le défaut de 4.`);
  } else {
    MIN_VALIDATORS = Number(brut);
  }
}

// ── 1. Adresses de la répartition ────────────────────────────────────────────
const distributionLisible = CONFIG.distribution && typeof CONFIG.distribution === 'object' && !Array.isArray(CONFIG.distribution);
if (!distributionLisible) X('coinbosa.config.json → distribution absent ou n\'est pas un objet : la répartition de l\'offre est illisible, rien ne peut être vérifié');
const postes = distributionLisible ? Object.entries(CONFIG.distribution).filter(([k]) => !k.startsWith('$')) : [];
const reserve = BigInt(CONFIG.migration.reserve);
const attendus = postes.map(([k]) => k).concat(reserve > 0n ? ['__migration__'] : []);

const vues = new Map();
let adressesOk = 0;
for (const poste of attendus) {
  const a = ADDR[poste];
  if (!a || a === ZERO || /^0x0+$/.test(a)) { X(`adresse non renseignée pour le poste « ${poste} »`); continue; }
  if (!ethers.isAddress(a)) { X(`adresse invalide pour « ${poste} » : ${a}`); continue; }
  // EIP-55 : la somme de contrôle d'une adresse Ethereum vit dans la CASSE de ses lettres.
  // Une adresse tout en minuscules n'en porte donc AUCUNE — et c'est précisément la forme
  // qu'on obtient en copiant depuis un terminal, un tableur ou un explorateur. L'ancienne
  // version se contentait de tolérer cette forme : une transposition de deux caractères y
  // passait en ✓, et build-genesis.js re-checksummait ensuite la faute, si bien que la
  // relecture ligne à ligne de l'étape 7 affichait une adresse d'apparence légitime.
  // 140 000 000 BOSA pour le seul poste « developpement », définitivement inaccessibles.
  // On EXIGE donc la forme casse mixte : c'est elle, et elle seule, qui fait qu'une faute
  // de frappe se voit.
  let canonique;
  try { canonique = ethers.getAddress(a); }
  catch { X(`adresse illisible pour « ${poste} » : ${a}`); continue; }
  if (a !== canonique) {
    X(`« ${poste} » : ${a} n'est pas sous forme EIP-55. Une adresse en minuscules (ou en majuscules) ne porte AUCUNE somme de contrôle : une faute de frappe y passe inaperçue. Recopier l'adresse depuis sa source — le coffre lui-même — dans sa forme à casse mixte. La forme canonique de CETTE chaîne-ci est ${canonique}, mais la recopier ne prouverait rien si la saisie est déjà fautive.`);
    continue;
  }
  const norm = a.toLowerCase();
  if (vues.has(norm)) { X(`adresse partagée par « ${poste} » et « ${vues.get(norm)} » : ${a} — chaque poste doit avoir la sienne`); continue; }
  vues.set(norm, poste);
  adressesOk++;
}
// Une boucle qui ne s'exécute jamais n'a rien constaté : « 0 adresses valides » comparé à
// « 0 attendues » émettait un ✓ sur un examen qui n'avait pas eu lieu. C'est exactement la
// forme du faux vert qu'on répare ailleurs : l'absence de plainte prise pour une conformité.
if (!attendus.length) {
  X('aucun poste de répartition lisible dans coinbosa.config.json → distribution : il n\'y a rien à examiner, et une liste vide n\'est pas une conformité');
} else if (adressesOk === attendus.length) {
  A(`${adressesOk} adresses de répartition renseignées, valides, EIP-55, distinctes`);
}

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
// Une part écrite en texte (« 20 » au lieu de 20) ferait de la somme une concaténation :
// le total ne vaudrait plus 100 et le message d'erreur parlerait d'un pourcentage absurde.
// On nomme la vraie cause plutôt que de laisser deviner.
for (const [poste, pct] of postes) {
  if (!Number.isInteger(pct) || pct <= 0) X(`la part de « ${poste} » vaut ${JSON.stringify(pct)} : ce n'est pas un pourcentage entier positif, la répartition n'est pas calculable`);
}
const somme = postes.reduce((s, [, p]) => s + p, 0);
if (somme !== 100) X(`la répartition totalise ${somme} %, 100 attendu`); else A('répartition à 100 %');
if (reserve + projet !== total) X(`réserve (${reserve}) + allocation projet (${projet}) ≠ offre totale (${total})`);
else A(`offre cohérente : ${total.toLocaleString('fr-FR')} BOSA`);
// Une part qui ne tombe pas juste laisserait des wei orphelins.
for (const [poste, pct] of postes) {
  if (Number.isInteger(pct) && (projet * BigInt(pct)) % 100n !== 0n) X(`la part de « ${poste} » (${pct} %) ne divise pas exactement l'allocation`);
}

// ── 3. Séparation des rôles ──────────────────────────────────────────────────
const VALIDATOR = process.env.VALIDATOR;
const GOVERNOR = process.env.GOVERNOR;
// Ces deux adresses sont recopiées à la main dans un shell, puis figées pour toujours :
// GOVERNOR est injecté en `constant` dans le bytecode du contrat système — donc dans le
// hash du bloc 0 — et aucune fonction ne peut le changer ; VALIDATOR est écrit dans
// l'extraData du bloc 0 et annoncé au consensus. Une faute de frappe sur VALIDATOR
// désigne un scelleur dont personne n'a la clé : la chaîne s'arrête au bloc d'epoch, et
// comme plus aucun bloc n'est produit, aucune transaction corrective ne peut être minée.
// C'est le seul moment où la somme de contrôle EIP-55 peut encore rattraper la saisie.
const roleEIP55 = (nom, valeur) => {
  if (!valeur || !ethers.isAddress(valeur)) return;
  let c;
  try { c = ethers.getAddress(valeur); } catch { return; }
  if (valeur !== c) X(`${nom}=${valeur} n'est pas sous forme EIP-55 : sous cette forme l'adresse ne porte aucune somme de contrôle et une faute de frappe ne se verrait pas. Relancer avec ${nom}=${c} après avoir revérifié la saisie à la source (« geth account list » côté serveur pour le validateur, la page du coffre pour le gouverneur).`);
};
if (!VALIDATOR || !ethers.isAddress(VALIDATOR)) X('VALIDATOR absent ou invalide');
if (!GOVERNOR || !ethers.isAddress(GOVERNOR)) X('GOVERNOR absent ou invalide (doit être un coffre multi-signatures)');
roleEIP55('VALIDATOR', VALIDATOR);
roleEIP55('GOVERNOR', GOVERNOR);
if (VALIDATOR && GOVERNOR && ethers.isAddress(VALIDATOR) && ethers.isAddress(GOVERNOR)) {
  if (VALIDATOR.toLowerCase() === GOVERNOR.toLowerCase()) X('GOVERNOR identique au VALIDATOR — la clé de scellage est en ligne en permanence, elle ne doit pas gouverner la chaîne');
  else A('gouverneur distinct de la clé de scellage');
  for (const [norm, poste] of vues) {
    if (norm === VALIDATOR.toLowerCase()) X(`le VALIDATOR est aussi l'adresse de trésorerie « ${poste} » — séparer scellage et détention des fonds`);
    if (norm === GOVERNOR.toLowerCase()) W(`le GOVERNOR est aussi l'adresse de trésorerie « ${poste} » — acceptable si c'est le coffre principal, à confirmer explicitement`);
  }
}

// ── 4. Jeu de validateurs ────────────────────────────────────────────────────
// L'accident : VALIDATORS n'est lu par AUCUN autre script du dépôt. La valeur observée et
// la valeur attendue venaient donc du même endroit — le shell de l'opérateur — et le
// « ✓ 4 validateur(s) distincts » ne faisait que répéter ce qu'on venait de taper. Quatre
// adresses incrémentales dont personne ne détient la clé, ou les quatre coffres de
// trésorerie déjà validés au § 1, produisaient exactement la même coche. Un contrôle dont
// l'observation et l'attente ont la même source ne vérifie rien.
// On confronte donc la déclaration à deux sources que l'opérateur ne fournit pas dans la
// même commande : ce que le dépôt enregistre (coinbosa.config.json → validators.current)
// et ce que build-genesis.js inscrit réellement dans extraData, à savoir UN seul validateur.
const declares = (process.env.VALIDATORS || '').split(',').map((s) => s.trim()).filter(Boolean);
const valides = [];
for (const v of declares) {
  if (!ethers.isAddress(v)) { X(`VALIDATORS contient « ${v} », qui n'est pas une adresse — corriger la liste, ne pas la compléter au jugé`); continue; }
  if (/^0x0+$/i.test(v)) { X('VALIDATORS contient l\'adresse nulle : personne n\'en détient la clé, ce « validateur » ne scellera jamais un bloc'); continue; }
  let c;
  try { c = ethers.getAddress(v); } catch { X(`VALIDATORS contient « ${v} », illisible comme adresse`); continue; }
  if (v !== c) W(`le validateur ${v} n'est pas déclaré sous forme EIP-55 : cette écriture ne porte aucune somme de contrôle, une faute de frappe y passerait inaperçue. Forme EIP-55 : ${c}`);
  valides.push(c);
}
// Les entrées rejetées ne sont PLUS comptées : l'ancienne version construisait l'ensemble
// des uniques à partir de la liste BRUTE, si bien que « aaa,bbb,ccc,ddd » comptait pour
// quatre validateurs distincts tout en étant signalé comme invalide.
const uniques = new Set(valides.map((a) => a.toLowerCase()));
if (uniques.size !== valides.length) X('la liste des validateurs contient des doublons — chaque validateur doit avoir sa propre clé, sur son propre serveur');

if (!declares.length) {
  X(`aucun validateur listé (VALIDATORS=0xa,0xb,…). Sans liste, ce point n'a rien examiné — ce n'est pas une conformité. Il en faut autant que n'en déclare coinbosa.config.json → validators.current, et au moins ${MIN_VALIDATORS === null ? '(seuil illisible, voir plus haut)' : MIN_VALIDATORS} à clés séparées : avec un seul, le réseau n'a ni tolérance aux pannes ni sécurité byzantine`);
} else {
  const attenduDepot = CONFIG.validators && CONFIG.validators.current;
  if (!Number.isInteger(attenduDepot)) {
    X('coinbosa.config.json → validators.current absent ou non entier : la déclaration VALIDATORS ne peut être confrontée à aucune source indépendante, elle ne serait qu\'un écho du shell. Renseigner le nombre réel de validateurs dans coinbosa.config.json.');
  } else if (uniques.size !== attenduDepot) {
    X(`VALIDATORS déclare ${uniques.size} validateur(s) distinct(s), coinbosa.config.json → validators.current en annonce ${attenduDepot} : l'un des deux ment. Corriger la source avant de continuer — soit la liste passée en ligne de commande, soit le dépôt, mais pas en les faisant diverger.`);
  } else {
    A(`${uniques.size} validateur(s) distinct(s), conforme à coinbosa.config.json → validators.current`);
  }

  // La clé qui scelle le bloc 0 DOIT faire partie du jeu qu'on déclare : sinon la liste
  // décrit un réseau, et l'extraData en démarre un autre.
  if (VALIDATOR && ethers.isAddress(VALIDATOR) && !uniques.has(VALIDATOR.toLowerCase())) {
    X(`VALIDATOR (${VALIDATOR}), la clé qui scelle le bloc 0 et que build-genesis.js inscrit dans extraData, ne figure pas dans VALIDATORS. La liste décrit alors un autre jeu de validateurs que celui qui démarrera la chaîne.`);
  }
  // Le gouverneur peut modifier l'ensemble des validateurs à vie. S'il scelle aussi, sa
  // clé vit en ligne en permanence : un serveur compromis emporte la gouvernance.
  if (GOVERNOR && ethers.isAddress(GOVERNOR) && uniques.has(GOVERNOR.toLowerCase())) {
    X(`le GOVERNOR (${GOVERNOR}) figure parmi les validateurs : la gouvernance de la chaîne reposerait sur une clé en ligne 24 h/24. Utiliser un coffre multi-signatures qui ne scelle aucun bloc.`);
  }
  // Un validateur qui est aussi un coffre confond scellage et détention des fonds : la clé
  // chaude du serveur signerait les blocs ET disposerait de la trésorerie.
  for (const v of uniques) {
    if (vues.has(v)) X(`le validateur ${ethers.getAddress(v)} est aussi l'adresse de trésorerie « ${vues.get(v)} » — une clé en ligne détiendrait les fonds du poste`);
  }

  if (MIN_VALIDATORS === null) {
    // Seuil illisible : déjà bloqué au § 0. On n'émet AUCUN ✓ ici — un comptage sans seuil
    // ne conclut rien, et se taire vaudrait approbation.
  } else if (!(uniques.size >= MIN_VALIDATORS)) {
    // Formulé par la négation : toute valeur non comparable tombe du côté bloquant.
    X(`${uniques.size} validateur(s) distinct(s), ${MIN_VALIDATORS} exigés au minimum. Ajouter des validateurs à clés séparées, ou abaisser le seuil EXPLICITEMENT (MIN_VALIDATORS=…) et assumer publiquement le niveau de décentralisation réel.`);
  } else {
    A(`seuil de validateurs atteint : ${uniques.size} ≥ ${MIN_VALIDATORS}`);
  }
  // Abaisser le seuil est possible, mais ne doit JAMAIS ressembler à un contrôle propre :
  // un réseau à moins de 4 validateurs n'a ni tolérance aux pannes ni sécurité byzantine.
  if (MIN_VALIDATORS !== null && !(MIN_VALIDATORS >= 4)) {
    W(`seuil de validateurs ABAISSÉ à ${MIN_VALIDATORS} (défaut : 4). Avec ${uniques.size} validateur(s), ` +
      'le réseau s\'arrête si la machine tombe et un seul opérateur produit tous les blocs. ' +
      'Ce n\'est PAS un réseau décentralisé : cela doit être écrit publiquement, pas découvert par un tiers.');
  }
  // Ce que le genesis fait RÉELLEMENT, par opposition à ce qu'on vient de déclarer :
  // build-genesis.js écrit « 01 » + une seule adresse dans extraData. Déclarer N > 1 ne
  // démarre pas un réseau à N validateurs — et les ajouter ensuite trop vite ARRÊTE la
  // chaîne (Parlia exige ⌊N/2⌋+1 scelleurs distincts et en ligne, voir AGENTS.md).
  if (uniques.size > 1) {
    W(`build-genesis.js n'inscrit qu'UN validateur dans l'extraData du bloc 0 ; les ${uniques.size - 1} autres n'existeront qu'APRÈS le bloc 0, via updateValidatorSet(). Le réseau ne démarre donc PAS à ${uniques.size} validateurs, et les ajouter avant de les avoir VUS sceller arrête la chaîne de façon irréversible — passer par scripts/rotate-validators.js.`);
  }
}

// ── 5. Compilateur ───────────────────────────────────────────────────────────
let solc = null;
try {
  solc = require('solc');
  if (solc.version().startsWith('0.8.26')) A(`solc ${solc.version().split('+')[0]} (version épinglée)`);
  else { X(`solc ${solc.version()} — 0.8.26 attendu ; le bytecode du contrat système fixe le hash du bloc 0`); solc = null; }
} catch { X('solc introuvable — lancer npm ci'); }

// ── 5 bis. Les intrants qui déterminent le hash du bloc 0 ────────────────────
// L'accident : le script épinglait solc en disant lui-même pourquoi — « le bytecode du
// contrat système fixe le hash du bloc 0 » — puis ne regardait NI la source dont ce
// bytecode est issu, NI l'état initial de base. Résultat mesuré : ajouter une seule
// constante à CoinbosaValidatorSet.sol changeait le bytecode (12 122 → 12 184 octets) donc
// le hash du bloc 0, et le préflight rendait un GO au bit près identique. Pire, il rendait
// GO sur un arbre où contracts/ et genesis-base.json étaient purement ABSENTS.
// Ces deux fichiers portent l'identité de la chaîne : genesis-base.json contient chainId,
// gasLimit et parlia{period,epoch}, et le contrat système est embarqué dans le bloc 0.
const CONTRAT = path.join(ROOT, 'contracts', 'CoinbosaValidatorSet.sol');
const BASE_FILE = path.join(ROOT, 'genesis', 'genesis-base.json');
const PROD_FILE = path.join(ROOT, 'genesis', 'genesis-coinbosa.json');
const VALSET = '0x0000000000000000000000000000000000001000';

let source = null;
let ancragesOk = true;
if (!fs.existsSync(CONTRAT)) {
  X('contracts/CoinbosaValidatorSet.sol absent : c\'est le contrat embarqué dans le bloc 0, son bytecode EST une partie de l\'identité de la chaîne. Sans lui, build-genesis.js ne peut rien produire et ce contrôle n\'a rien à examiner.');
} else {
  source = fs.readFileSync(CONTRAT, 'utf8');
  // build-genesis.js remplace ces deux constantes avant de compiler ; sans les motifs
  // exacts il s'arrête. Les vérifier ici évite de découvrir l'échec à l'étape 7, après
  // avoir annoncé GO.
  for (const nom of ['GOVERNOR', 'INITIAL_VALIDATOR']) {
    if (!new RegExp(`address public constant ${nom} = 0x[0-9a-fA-F]{40};`).test(source)) {
      ancragesOk = false;
      X(`contracts/CoinbosaValidatorSet.sol ne contient plus le motif « address public constant ${nom} = 0x…; » : build-genesis.js ne pourra pas y injecter l'adresse et refusera de produire le genesis. Rétablir la déclaration sous cette forme exacte.`);
    }
  }
}

let base = null;
if (!fs.existsSync(BASE_FILE)) {
  X('genesis/genesis-base.json absent : c\'est l\'état initial dont build-genesis.js part (contrats système, chainId, gasLimit, paramètres parlia). Sans lui, aucun genesis de production ne peut être construit ni vérifié.');
} else {
  try { base = JSON.parse(fs.readFileSync(BASE_FILE, 'utf8')); }
  catch (e) { X(`genesis/genesis-base.json illisible (${e.message}) — fichier tronqué ou écrasé`); }
  if (base && (typeof base !== 'object' || Array.isArray(base) || !base.alloc || !base.config)) {
    X('genesis/genesis-base.json n\'a pas la forme d\'un genesis (objet avec « config » et « alloc ») — fichier écrasé ?');
    base = null;
  }
  if (base && !(base.alloc[VALSET] && base.alloc[VALSET].code)) {
    X(`genesis/genesis-base.json ne contient pas le contrat système ${VALSET} : build-genesis.js le déréférence pour mesurer l'ancien code et échouerait sur une erreur illisible.`);
    base = null;
  }
}

// Cohérence d'identité réseau : genesis-base.json et coinbosa.config.json décrivent le
// MÊME réseau. S'ils divergent, l'un des deux documents ment — et c'est celui qui est
// compilé dans le bloc 0 qui gagne, silencieusement.
if (base) {
  const net = CONFIG.network || {};
  // « Je n'ai pas pu comparer » n'est pas « ça concorde » : une valeur absente de l'un des
  // deux documents est bloquante, jamais verte.
  const entier = (v) => { try { return (v === undefined || v === null) ? undefined : BigInt(v).toString(); } catch { return `illisible(${v})`; } };
  const confronter = (quoi, vu, attendu, degat) => {
    if (vu === undefined || vu === null || attendu === undefined || attendu === null) {
      X(`${quoi} : la valeur manque dans genesis-base.json ou dans coinbosa.config.json — la comparaison n'a pas eu lieu, ce n'est pas une conformité. Renseigner les deux documents avant de produire le genesis.`);
      return;
    }
    if (String(vu) !== String(attendu)) X(`${quoi} : ${vu} dans genesis-base.json ≠ ${attendu} déclaré dans coinbosa.config.json. ${degat}`);
    else A(`${quoi} cohérent entre genesis-base.json et coinbosa.config.json : ${attendu}`);
  };
  confronter('chainId', base.config.chainId, net.chainId,
    'Deux chaînes peuvent avoir un bloc 0 rigoureusement identique et rester deux réseaux distincts, dont l\'un aux jetons sans valeur.');
  confronter('gasLimit', entier(base.gasLimit), entier(net.gasLimit),
    'Le gasLimit figure dans l\'en-tête du bloc 0 : il entre donc dans son hash.');
  const parlia = base.config.parlia || {};
  confronter('longueur d\'epoch (parlia.epoch)', parlia.epoch, net.epochLength,
    'Le bloc d\'epoch est celui où le contrat système est interrogé : les deux documents ne décriraient pas le même réseau.');
  confronter('intervalle de bloc (parlia.period, en ms)', parlia.period === undefined ? undefined : Number(parlia.period) * 1000, net.blockIntervalMs,
    'Le temps de bloc annoncé publiquement ne serait pas celui de la chaîne produite.');
}

// Reproductibilité : genesis-reference.json promet que « regénérer le genesis avec les
// mêmes adresses et le même solc doit redonner exactement le même hash ». Tant qu'une
// empreinte est figée et que le genesis publié est au dépôt, cette promesse se REDÉMONTRE
// ici, sans manifeste séparé et sans interroger le moindre nœud : on recompile le contrat
// système avec le gouverneur et le validateur figés dans la référence, et le bytecode doit
// être celui du genesis publié. C'est ce contrôle, et lui seul, qui attrape une
// modification de contracts/ ou de genesis-base.json AVANT de reconstruire le genesis.
const refPath = path.join(ROOT, 'genesis', 'genesis-reference.json');
let ref = null;
if (fs.existsSync(refPath)) {
  try { ref = JSON.parse(fs.readFileSync(refPath, 'utf8')); } catch (e) { X(`genesis/genesis-reference.json illisible (${e.message})`); }
}
// Une empreinte n'est « figée » que si elle a la forme d'un hash de 32 octets ET n'est pas
// une remise à zéro. Une valeur vide, tronquée (« 0x ») ou de mauvaise longueur ne prouve
// rien : elle est traitée plus bas comme un fichier corrompu, pas comme une absence.
const HASH_32 = /^0x[0-9a-fA-F]{64}$/;
const refFigee = !!(ref && typeof ref === 'object' && !Array.isArray(ref) && typeof ref.hash === 'string'
  && HASH_32.test(ref.hash) && !/^0x0+$/.test(ref.hash));

if (refFigee && solc && source && ancragesOk && base) {
  if (!fs.existsSync(PROD_FILE)) {
    W('genesis/genesis-coinbosa.json absent alors qu\'une empreinte est figée : la reproductibilité des intrants (contrat système, état de base) ne peut pas être rejouée depuis le dépôt. Récupérer le genesis publié pour pouvoir la redémontrer.');
  } else if (!ethers.isAddress(ref.gouverneur || '') || !ethers.isAddress(ref.validateur || '')) {
    X('genesis-reference.json ne nomme pas le gouverneur et le validateur figés : impossible de recompiler le contrat système tel qu\'il a été publié, donc impossible de prouver que les intrants du bloc 0 n\'ont pas bougé.');
  } else {
    try {
      const prod = JSON.parse(fs.readFileSync(PROD_FILE, 'utf8'));
      let src = source;
      const injecter = (nom, valeur) => {
        src = src.replace(new RegExp(`address public constant ${nom} = 0x[0-9a-fA-F]{40};`), `address public constant ${nom} = ${valeur};`);
      };
      injecter('GOVERNOR', ethers.getAddress(ref.gouverneur));
      injecter('INITIAL_VALIDATOR', ethers.getAddress(ref.validateur));
      // Réglages identiques à build-genesis.js : optimiseur, version d'EVM et absence de
      // hash de métadonnées font partie du bytecode produit.
      const out = JSON.parse(solc.compile(JSON.stringify({
        language: 'Solidity',
        sources: { 'CoinbosaValidatorSet.sol': { content: src } },
        settings: {
          optimizer: { enabled: true, runs: 200 },
          evmVersion: 'shanghai',
          metadata: { bytecodeHash: 'none' },
          outputSelection: { '*': { '*': ['evm.deployedBytecode.object'] } },
        },
      })));
      const errs = (out.errors || []).filter((e) => e.severity === 'error');
      if (errs.length) {
        X(`contracts/CoinbosaValidatorSet.sol ne compile plus : ${errs[0].formattedMessage.split('\n')[0]}. Le genesis de production ne peut pas être reconstruit.`);
      } else {
        const runtime = '0x' + out.contracts['CoinbosaValidatorSet.sol'].CoinbosaValidatorSet.evm.deployedBytecode.object;
        const publie = (prod.alloc && prod.alloc[VALSET] && prod.alloc[VALSET].code) || null;
        if (!publie) {
          X(`genesis/genesis-coinbosa.json ne contient pas le code du contrat système ${VALSET} : le genesis publié est incomplet ou a été écrasé.`);
        } else if (runtime !== publie) {
          X(`contracts/CoinbosaValidatorSet.sol ne reproduit PLUS le bytecode du genesis publié (${(runtime.length - 2) / 2} octets recompilés contre ${(publie.length - 2) / 2} publiés). La logique du contrat a changé depuis le gel du ${ref.fige_le} : régénérer le genesis donnerait un stateRoot différent, donc un bloc 0 différent, donc UNE AUTRE CHAÎNE que celle publiée. Revenir à la version figée du contrat, ou assumer explicitement le lancement d'un nouveau réseau (et remettre genesis-reference.json à 0x0).`);
        } else {
          A('contrat système : recompilé depuis contracts/, il reproduit exactement le bytecode du genesis publié');
        }
        // L'état initial de base entre aussi entièrement dans le bloc 0 : tous les champs
        // hors « alloc » sont recopiés tels quels par build-genesis.js, et le code des
        // contrats système conservés vient de là.
        const sansVolatile = (o) => { const c = { ...o }; delete c.alloc; delete c.extraData; delete c.coinbosaDev; return JSON.stringify(c); };
        if (sansVolatile(base) !== sansVolatile(prod)) {
          X('genesis/genesis-base.json ne correspond plus à l\'en-tête du genesis publié (config, gasLimit, timestamp, difficulty…). Ces champs sont recopiés tels quels dans le bloc 0 : les régénérer produirait un autre hash. Comparer genesis-base.json à genesis-coinbosa.json et rétablir la version figée.');
        } else {
          A('genesis-base.json : en-tête identique à celui du genesis publié');
        }
        let deriveCode = 0;
        for (const [addr, v] of Object.entries(prod.alloc || {})) {
          if (!v.code || addr.toLowerCase() === VALSET) continue;
          const b = base.alloc[addr];
          if (!b || b.code !== v.code || JSON.stringify(b.storage) !== JSON.stringify(v.storage)) deriveCode++;
        }
        if (deriveCode) X(`${deriveCode} contrat(s) système du genesis publié n'ont plus le même code dans genesis-base.json : le stateRoot du bloc 0 changerait. Rétablir genesis-base.json dans sa version figée.`);
        else A('contrats système conservés : code identique entre genesis-base.json et le genesis publié');
      }
    } catch (e) {
      X(`reproduction des intrants du bloc 0 impossible (${e.message}) — ce n'est pas un succès : corriger la cause avant de produire le genesis`);
    }
  }
} else if (!refFigee) {
  // Aucune empreinte figée : il n'existe encore rien à quoi confronter les intrants. On le
  // DIT, au lieu de laisser croire que le contrat système et l'état de base ont été vérifiés.
  W('aucune empreinte exploitable dans genesis-reference.json : les intrants du hash du bloc 0 (contracts/CoinbosaValidatorSet.sol, genesis/genesis-base.json) ne sont donc comparés à AUCUNE référence, et leur intégrité n\'est PAS vérifiée ici. C\'est le gel de l\'empreinte (étape 9) qui rendra ce contrôle possible.');
} else {
  // Empreinte figée, mais un intrant manque (compilateur, contrat, état de base) : ces
  // manques sont déjà bloquants plus haut. On le redit ici pour que le silence de cette
  // section ne soit pas lu comme une vérification réussie.
  W('la reproduction des intrants du bloc 0 n\'a PAS pu être rejouée (compilateur ou fichier manquant — voir les points bloquants) : rien ne prouve ici que le contrat système et l\'état de base sont toujours ceux du réseau publié.');
}

// ── 6. Empreinte de référence ────────────────────────────────────────────────
// L'accident : le contrôle ne lisait qu'un NOM DE CHAMP. Un fichier réduit à {}, un tableau
// [], une clé renommée (« empreinte » au lieu de « hash ») rendaient tous « ✓ prêt à
// recevoir l'empreinte » — y compris quand l'empreinte de production était toujours là,
// sous un autre nom. L'opérateur régénérait alors le genesis en croyant qu'aucune identité
// de chaîne n'était encore engagée, et publiait une autre chaîne que celle annoncée.
if (!fs.existsSync(refPath)) {
  X('genesis/genesis-reference.json absent');
} else if (ref === null) {
  // JSON illisible : déjà bloqué au § 5 bis.
} else if (typeof ref !== 'object' || Array.isArray(ref)) {
  X('genesis/genesis-reference.json n\'est pas un objet JSON — fichier corrompu ou écrasé. Rétablir le fichier depuis le dépôt avant toute régénération.');
} else if (!Object.prototype.hasOwnProperty.call(ref, 'hash')) {
  X('genesis/genesis-reference.json ne porte pas de champ « hash » : le schéma est perdu (clé renommée ? fichier écrasé ?). Impossible d\'affirmer qu\'aucune empreinte de production n\'est déjà figée — et si elle l\'est, régénérer le genesis publierait une autre chaîne. Rétablir le fichier avant de continuer.');
} else if (refFigee) {
  W(`genesis-reference.json contient DÉJÀ une empreinte (${ref.hash.slice(0, 18)}…) figée le ${ref.fige_le || '(date non renseignée)'} — si tu régénères le genesis, elle ne correspondra plus. La remettre à 0x0, ou vérifier que c'est bien la chaîne voulue`);
  // La référence figée nomme le validateur et le gouverneur du réseau publié. Les injecter
  // différemment produit un autre bytecode et une autre extraData, donc une autre chaîne —
  // qui porterait pourtant le nom et l'empreinte annoncés publiquement.
  if (ref.validateur && VALIDATOR && ethers.isAddress(VALIDATOR) && ref.validateur.toLowerCase() !== VALIDATOR.toLowerCase()) {
    X(`la référence figée le ${ref.fige_le} nomme le validateur ${ref.validateur}, VALIDATOR vaut ${VALIDATOR} : régénérer produirait une AUTRE chaîne que celle publiée. Utiliser la clé figée, ou assumer explicitement un nouveau réseau.`);
  }
  if (ref.gouverneur && GOVERNOR && ethers.isAddress(GOVERNOR) && ref.gouverneur.toLowerCase() !== GOVERNOR.toLowerCase()) {
    X(`la référence figée nomme le gouverneur ${ref.gouverneur}, GOVERNOR vaut ${GOVERNOR} : GOVERNOR est injecté en « constant » dans le bytecode du contrat système, donc dans le hash du bloc 0. Deux valeurs = deux chaînes.`);
  }
  if (ref.chainId !== undefined && CONFIG.network && ref.chainId !== CONFIG.network.chainId) {
    X(`la référence figée porte chainId ${ref.chainId}, coinbosa.config.json déclare ${CONFIG.network.chainId}`);
  }
} else if (typeof ref.hash !== 'string' || !/^0x[0-9a-fA-F]*$/.test(ref.hash)) {
  X(`genesis-reference.json → hash vaut ${JSON.stringify(ref.hash)} : ce n'est pas une valeur hexadécimale. Fichier corrompu — le rétablir depuis le dépôt.`);
} else if (!/^0x0+$/.test(ref.hash)) {
  // Ni une empreinte de 32 octets, ni une remise à zéro délibérée : « » ou « 0x » sont des
  // restes d'une écriture ratée. Les accepter reviendrait à affirmer qu'aucune identité de
  // chaîne n'est engagée alors qu'on n'en sait rien.
  X(`genesis-reference.json → hash vaut « ${ref.hash} » : ni une empreinte de 32 octets, ni une remise à zéro explicite. Sur une valeur tronquée on ne peut PAS affirmer qu'aucune empreinte de production n'est déjà figée. Rétablir le fichier, ou écrire délibérément 0x0 pour signifier « pas encore figée ».`);
} else {
  A('genesis-reference.json prêt à recevoir l\'empreinte du bloc 0 (remise à zéro explicite)');
}

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
