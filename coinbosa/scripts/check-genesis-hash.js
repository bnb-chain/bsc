// Vérifie que la chaîne branchée est bien CELLE QUI A ÉTÉ PUBLIÉE.
//
//   RPC=https://explorer.coinbosa.com/rpc node scripts/check-genesis-hash.js
//   ALLOW_DEV_HASH=1 RPC=http://127.0.0.1:8545 node scripts/check-genesis-hash.js   (réseau de dév local)
//
// Pourquoi ce contrôle existe
// ---------------------------
// check-supply.js parcourt les adresses du FICHIER genesis local : il prouve que ces
// adresses-là ont le bon solde, mais il ne peut pas voir une adresse CACHÉE qui aurait été
// ajoutée au genesis réellement déployé. Aucune API JSON-RPC standard ne permet d'énumérer
// tous les comptes d'un état.
//
// La parade est cryptographique : l'en-tête du bloc 0 contient `stateRoot`, la racine de
// l'arbre de Merkle de TOUT l'état initial. Un seul wei ajouté à une adresse quelconque —
// même inconnue de nous — change `stateRoot`, donc change le hash du bloc 0. Comparer le
// hash du bloc 0 à la valeur figée à la publication détecte donc N'IMPORTE QUELLE
// allocation cachée, sans avoir à énumérer quoi que ce soit.
//
// C'est ce contrôle, et lui seul, qui rend vérifiable la promesse « aucune émission cachée ».
//
// LES ACCIDENTS QUE CE SCRIPT DOIT ATTRAPER
// -----------------------------------------
// 1. Allocation cachée : le genesis déployé contient une adresse absente du fichier publié.
//    Attrapé par la comparaison du hash et du stateRoot du bloc 0.
// 2. Réseau substitué (clone) : même bloc 0, chainId différent. Le chainId vit dans la
//    section `config` du genesis, qui n'entre PAS dans le hash du bloc — deux chaînes
//    peuvent avoir un bloc 0 rigoureusement identique et être deux réseaux distincts, dont
//    l'un aux jetons sans valeur. Attrapé par la comparaison explicite du chainId observé.
// 3. Nœud (ou proxy) qui RECOPIE les valeurs attendues : une couche RPC modifiée peut
//    renvoyer le hash, le stateRoot et l'extraData figés au-dessus d'un état tout autre.
//    Attrapé en recalculant keccak256(RLP(en-tête)) à partir des champs renvoyés : un
//    en-tête recopié en partie ne peut pas hacher vers la valeur qu'il annonce.
// 4. Référence amputée : un genesis-reference.json privé de stateRoot ou d'extraData
//    faisait « passer » des comparaisons qui n'avaient jamais eu lieu. Attrapé en validant
//    la référence au chargement, champ par champ.
// 5. Référence qui ne correspond plus au genesis publié : le bloc 0 est reconstruit
//    HORS-LIGNE depuis genesis/genesis-coinbosa.json (arbre de Merkle-Patricia recalculé
//    ici même) et doit reproduire l'empreinte figée. C'est la seule étape qui ne fait
//    confiance à aucun nœud, et c'est elle qui prouve la reproductibilité annoncée dans
//    genesis-reference.json.
// 6. Mode développement employé ailleurs que sur un réseau de développement — voir le
//    bloc MODE DÉVELOPPEMENT plus bas.
//
// RÈGLE DE CONCEPTION — c'est son absence qui avait produit un faux vert
// ----------------------------------------------------------------------
// Un chemin qui n'a RIEN comparé ne sort JAMAIS en 0. Le silence d'un contrôle est
// indiscernable d'un succès pour celui qui lit la coche verte, et un contrôle qui ment
// fait cesser la vigilance : il est pire que pas de contrôle du tout.
//   sortie 0 : les comparaisons ont eu lieu ET concordent
//   sortie 1 : divergence PROUVÉE — la chaîne n'est pas celle qui a été publiée
//   sortie 2 : contrôle IMPOSSIBLE (donnée manquante, mode mal employé) — ce n'est pas un succès
const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

const RACINE = path.join(__dirname, '..');
// Plusieurs points d'accès acceptés (séparés par une virgule) : un vérificateur tiers ne
// devrait pas s'en remettre à un seul nœud, qui peut mentir de bout en bout. Quand
// plusieurs sont fournis, ils doivent tous servir le même bloc 0.
const RPCS = (process.env.RPC || 'http://127.0.0.1:8545').split(/[\s,]+/).filter(Boolean);
const ALLOW_DEV = process.env.ALLOW_DEV_HASH === '1';
// Vérification HORS-LIGNE seule : confronte le genesis publié à l'empreinte figée, sans
// interroger de nœud. Utile là où aucune chaîne de production n'est joignable (intégration
// continue) : elle attrape une allocation ajoutée au fichier publié après le gel. Elle ne
// remplace jamais la vérification de la chaîne — la sortie le dit explicitement.
const SANS_CHAINE = process.env.SANS_CHAINE === '1';
const REF_FILE = process.env.GENESIS_REF || path.join(RACINE, 'genesis', 'genesis-reference.json');
// Le fichier genesis dont on reconstruit le bloc 0 : la production vise genesis-coinbosa.json,
// le mode dév vise le genesis régénéré localement (celui passé à `geth init`).
const GENESIS_FILE = process.env.GENESIS ||
  path.join(RACINE, 'genesis', ALLOW_DEV ? 'genesis-coinbosa-dev.json' : 'genesis-coinbosa.json');

// « Je n'ai pas pu vérifier » n'est pas « tout va bien » : sortie 2, jamais 0.
const impossible = (titre, ...suite) => {
  console.error(`\nCONTROLE IMPOSSIBLE : ${titre}`);
  suite.forEach((l) => console.error(`  ${l}`));
  process.exit(2);
};
// « J'ai vérifié et ça diverge » : sortie 1.
const divergence = (titre, ...suite) => {
  console.error(`\nECHEC : ${titre}`);
  suite.forEach((l) => console.error(`  ${l}`));
  process.exit(1);
};

const K = ethers.keccak256;
const RLP = ethers.encodeRlp;
const RACINE_TRIE_VIDE = K(RLP('0x'));                     // racine d'un arbre vide
const CODE_VIDE = K('0x');                                 // keccak256("") : compte sans code
const ONCLES_VIDES = '0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347';
const BLOOM_VIDE = '0x' + '00'.repeat(256);
const HASH_NUL = '0x' + '00'.repeat(32);

// RLP encode les entiers en gros-boutiste MINIMAL : "0x0" s'encode comme chaîne vide.
// Se tromper ici donne un hash faux et donc une accusation fausse — d'où la normalisation
// systématique de tout ce qui vient du réseau ou d'un fichier.
const qte = (v) => {
  const n = BigInt(v ?? 0);
  if (n === 0n) return '0x';
  let h = n.toString(16);
  if (h.length % 2) h = '0' + h;
  return '0x' + h;
};
const octets = (v) => ethers.hexlify(ethers.getBytes(v));           // champ de longueur fixe/libre
const cale = (v, n) => ethers.toBeHex(BigInt(v ?? 0), n);           // idem, complété à n octets

// ---------------------------------------------------------------------------------------
// Arbre de Merkle-Patricia : reconstruction hors-ligne du stateRoot
// ---------------------------------------------------------------------------------------
// Ce que ça attrape : une empreinte figée qui ne correspondrait plus au genesis publié —
// que la dérive vienne du fichier (allocation ajoutée après le gel) ou de la référence
// (empreinte d'un autre réseau). Sans ce calcul, genesis-reference.json est une valeur
// qu'on ne peut que croire sur parole ; avec lui, elle se redémontre depuis le dépôt, sans
// interroger le moindre nœud.
const nibbles = (hex) => Array.from(hex.slice(2), (c) => parseInt(c, 16));

// Préfixe hexadécimal (« hex prefix ») : encode la parité du chemin et le type de nœud.
const prefixe = (chemin, feuille) => {
  const drapeau = feuille ? 2 : 0;
  const sortie = [chemin.length % 2 ? ((drapeau + 1) << 4) | chemin[0] : drapeau << 4];
  for (let i = chemin.length % 2; i < chemin.length; i += 2) sortie.push((chemin[i] << 4) | chemin[i + 1]);
  return '0x' + sortie.map((b) => b.toString(16).padStart(2, '0')).join('');
};

// Un nœud fils est inclus TEL QUEL s'il tient sur moins de 32 octets, sinon par son hash.
const lien = (noeud) => {
  const enc = RLP(noeud);
  return (enc.length - 2) / 2 < 32 ? noeud : K(enc);
};

const batir = (paires, prof) => {
  if (paires.length === 1) return [prefixe(paires[0].k.slice(prof), true), paires[0].v];
  let commun = prof;
  while (commun < paires[0].k.length && paires.every((p) => p.k[commun] === paires[0].k[commun])) commun++;
  if (commun > prof) return [prefixe(paires[0].k.slice(prof, commun), false), lien(batir(paires, commun))];
  const branche = new Array(17).fill('0x');
  const groupes = new Map();
  for (const p of paires) {
    if (p.k.length === prof) { branche[16] = p.v; continue; }
    if (!groupes.has(p.k[prof])) groupes.set(p.k[prof], []);
    groupes.get(p.k[prof]).push(p);
  }
  for (const [n, groupe] of groupes) branche[n] = lien(batir(groupe, prof + 1));
  return branche;
};

const racineTrie = (paires) => {
  if (paires.length === 0) return RACINE_TRIE_VIDE;
  paires.sort((a, b) => (a.c < b.c ? -1 : 1));
  return K(RLP(batir(paires, 0)));
};

// Les comptes du genesis à solde nul et sans code font PARTIE de l'arbre : geth les écrit
// tels quels au bloc 0 (les retirer change la racine). Les précompilés inter-chaînes de
// Coinbosa sont dans ce cas — d'où l'absence de tout filtrage ici.
const stateRootDepuisAlloc = (alloc) => {
  const paires = [];
  const champsInconnus = new Set();
  for (const [adresse, compte] of Object.entries(alloc || {})) {
    for (const cle of Object.keys(compte)) {
      if (!['balance', 'nonce', 'code', 'storage'].includes(cle)) champsInconnus.add(cle);
    }
    const code = compte.code && compte.code !== '0x' ? compte.code : null;
    const stockage = compte.storage && Object.keys(compte.storage).length ? compte.storage : null;
    let racineStockage = RACINE_TRIE_VIDE;
    if (stockage) {
      racineStockage = racineTrie(Object.entries(stockage).map(([emplacement, valeur]) => {
        const c = K(cale(emplacement, 32));
        return { c, k: nibbles(c), v: RLP(qte(valeur)) };
      }));
    }
    const c = K(ethers.getAddress(adresse.startsWith('0x') ? adresse : `0x${adresse}`));
    const enregistrement = [qte(compte.nonce), qte(compte.balance), racineStockage, code ? K(code) : CODE_VIDE];
    paires.push({ c, k: nibbles(c), v: RLP(enregistrement) });
  }
  return { racine: racineTrie(paires), champsInconnus: [...champsInconnus] };
};

// ---------------------------------------------------------------------------------------
// Bloc 0 reconstruit depuis un FICHIER genesis, sans nœud
// ---------------------------------------------------------------------------------------
// Les règles reproduites ici sont celles du client Coinbosa (core/genesis.go, toBlockWithRoot) :
//  - base fee initiale : 0 sur une chaîne Parlia (params.InitialBaseFeeForBSC), 1 gwei ailleurs ;
//  - withdrawalsRoot : seulement hors chaîne BSC/Parlia, donc jamais ici ;
//  - Cancun/Prague ajouteraient des champs à l'en-tête : le script REFUSE de deviner
//    plutôt que de calculer un hash faux et d'accuser à tort une chaîne saine.
const blocZeroDepuisGenesis = (g, nom) => {
  const cfg = g.config || {};
  if (!cfg.chainId) impossible(`${nom} ne déclare pas de config.chainId.`, 'Un genesis sans chainId ne désigne aucun réseau : fichier tronqué ou mal formé.');
  if (BigInt(g.number ?? 0) !== 0n) impossible(`${nom} déclare number=${g.number} : ce n'est pas un bloc 0.`);
  const horodatage = BigInt(g.timestamp ?? 0);
  const actif = (t) => t !== undefined && t !== null && BigInt(t) <= horodatage;
  if (actif(cfg.cancunTime) || actif(cfg.pragueTime) || actif(cfg.osakaTime)) {
    impossible(`${nom} active Cancun/Prague dès le bloc 0 : l'en-tête porte alors des champs`,
      'supplémentaires (blobGasUsed, excessBlobGas, parentBeaconBlockRoot, requestsHash).',
      'Étendre la liste ordonnée des champs dans ce script AVANT de s\'en servir comme preuve.');
  }
  const parlia = !!cfg.parlia;
  const london = cfg.londonBlock !== undefined && cfg.londonBlock !== null && Number(cfg.londonBlock) <= 0;
  const { racine, champsInconnus } = stateRootDepuisAlloc(g.alloc);
  const champs = [
    cale(g.parentHash ?? 0, 32), ONCLES_VIDES, cale(g.coinbase ?? 0, 20), racine,
    RACINE_TRIE_VIDE, RACINE_TRIE_VIDE, BLOOM_VIDE,
    qte(g.difficulty), qte(g.number), qte(g.gasLimit), qte(g.gasUsed), qte(g.timestamp),
    octets(g.extraData), cale(g.mixHash ?? 0, 32), cale(g.nonce ?? 0, 8),
  ];
  if (g.baseFeePerGas !== undefined && g.baseFeePerGas !== null) champs.push(qte(g.baseFeePerGas));
  else if (london) champs.push(parlia ? qte(0) : qte(1000000000));
  if (!parlia && actif(cfg.shanghaiTime)) champs.push(RACINE_TRIE_VIDE);
  return {
    hash: K(RLP(champs)),
    stateRoot: racine,
    extraData: octets(g.extraData),
    gasLimit: qte(g.gasLimit),
    chainId: Number(cfg.chainId),
    champsInconnus,
  };
};

// ---------------------------------------------------------------------------------------
// Recalcul du hash à partir de l'en-tête RENVOYÉ par le nœud
// ---------------------------------------------------------------------------------------
// Ce que ça attrape : un nœud, ou un proxy placé devant lui, qui recopie les trois valeurs
// attendues (hash, stateRoot, extraData) dans un en-tête par ailleurs incohérent. Sans ce
// recalcul, le contrôle ne vérifie que l'écho de trois chaînes de caractères — un en-tête
// mathématiquement impossible passait pour conforme.
const ORDRE_OPTIONNELS = [
  ['baseFeePerGas', 'q'], ['withdrawalsRoot', 'd'], ['blobGasUsed', 'q'],
  ['excessBlobGas', 'q'], ['parentBeaconBlockRoot', 'd'], ['requestsHash', 'd'],
];
const ORDRE_ENTETE = [
  ['parentHash', 'd'], ['sha3Uncles', 'd'], ['miner', 'd'], ['stateRoot', 'd'],
  ['transactionsRoot', 'd'], ['receiptsRoot', 'd'], ['logsBloom', 'd'],
  ['difficulty', 'q'], ['number', 'q'], ['gasLimit', 'q'], ['gasUsed', 'q'], ['timestamp', 'q'],
  ['extraData', 'd'], ['mixHash', 'd'], ['nonce', 'd'],
];
const hashDepuisEnTete = (b) => {
  const champs = [];
  const presents = [];
  for (const [cle, type] of ORDRE_ENTETE) {
    if (b[cle] === undefined || b[cle] === null) return { erreur: `champ ${cle} absent de la réponse du nœud` };
    try { champs.push(type === 'q' ? qte(b[cle]) : octets(b[cle])); }
    catch (e) { return { erreur: `champ ${cle} mal formé (${b[cle]})` }; }
  }
  for (const [cle, type] of ORDRE_OPTIONNELS) {
    if (b[cle] === undefined || b[cle] === null) continue;
    presents.push(cle);
    try { champs.push(type === 'q' ? qte(b[cle]) : octets(b[cle])); }
    catch (e) { return { erreur: `champ ${cle} mal formé (${b[cle]})` }; }
  }
  return { hash: K(RLP(champs)), presents };
};

const lireJson = (fichier, quoi) => {
  if (!fs.existsSync(fichier)) return null;
  try { return JSON.parse(fs.readFileSync(fichier, 'utf8')); } catch (e) {
    impossible(`${quoi} illisible (${fichier}) : ${e.message}`, 'Rétablir le fichier depuis le dépôt publié, puis relancer.');
  }
};

const estBoucleLocale = (url) => {
  let hote;
  try { hote = new URL(url).hostname; } catch { return false; }
  return hote === 'localhost' || hote === '::1' || hote === '[::1]' || /^127\./.test(hote);
};

(async () => {
  // --- 1. l'empreinte ATTENDUE, reconstruite hors-ligne depuis le fichier genesis --------
  const genesis = lireJson(GENESIS_FILE, 'genesis');
  if (!genesis) {
    impossible(`fichier genesis introuvable : ${GENESIS_FILE}`,
      ALLOW_DEV
        ? 'En mode dév, l\'empreinte attendue se reconstruit depuis le genesis passé à `geth init`.'
        : 'C\'est le fichier publié dont la référence figée est censée être l\'empreinte.',
      ALLOW_DEV
        ? 'Le générer (VALIDATOR=0x… ALLOW_DEV=1 node scripts/build-genesis.js) ou pointer GENESIS=… dessus.'
        : 'Le récupérer depuis le dépôt publié (genesis/genesis-coinbosa.json), puis relancer.');
  }
  // Un genesis de dév ne doit jamais servir de référence de production : sans cette porte,
  // « la chaîne est conforme » se dirait d'un réseau jetable aux adresses synthétiques.
  if (genesis.coinbosaDev && !ALLOW_DEV) {
    impossible(`${path.basename(GENESIS_FILE)} porte le marqueur coinbosaDev — genesis de DÉVELOPPEMENT.`,
      'Aucune affirmation de production ne peut en sortir.',
      'Viser genesis/genesis-coinbosa.json, ou assumer le mode dév avec ALLOW_DEV_HASH=1 (nœud local).');
  }
  const horsLigne = blocZeroDepuisGenesis(genesis, path.basename(GENESIS_FILE));
  if (horsLigne.champsInconnus.length) {
    console.warn(`  ⚠  champs d'alloc non pris en compte dans le recalcul : ${horsLigne.champsInconnus.join(', ')}`);
  }
  console.log(`  bloc 0 reconstruit hors-ligne depuis ${path.basename(GENESIS_FILE)} :`);
  console.log(`    hash       : ${horsLigne.hash}`);
  console.log(`    stateRoot  : ${horsLigne.stateRoot}`);
  console.log(`    chainId    : ${horsLigne.chainId}`);

  // --- 2. la référence figée : validée, puis confrontée à la reconstruction -------------
  const brut = lireJson(REF_FILE, 'référence figée');
  let ref = null;
  if (brut) {
    // Une référence amputée d'un champ faisait sauter la comparaison correspondante EN
    // SILENCE. On exige donc les quatre champs, bien formés, avant de s'en servir.
    const attendus = { hash: 66, stateRoot: 66, extraData: 0 };
    for (const [cle, taille] of Object.entries(attendus)) {
      const v = brut[cle];
      if (!v || typeof v !== 'string' || !/^0x[0-9a-fA-F]+$/.test(v) || /^0x0*$/.test(v) || (taille && v.length !== taille)) {
        impossible(`${cle} absent ou mal formé dans ${path.basename(REF_FILE)}.`,
          'Une référence incomplète fait sauter des comparaisons sans le dire : c\'est un faux vert.',
          'Rétablir l\'empreinte complète (hash, stateRoot, extraData, chainId) figée au gel du genesis.');
      }
    }
    if (!Number.isInteger(brut.chainId) || brut.chainId <= 0) {
      impossible(`chainId absent ou invalide dans ${path.basename(REF_FILE)}.`,
        'Sans lui, un clone du réseau (même bloc 0, autre chainId) passerait pour la chaîne publiée.');
    }
    ref = brut;
  }

  let attendu;      // l'empreinte contre laquelle la chaîne sera jugée
  let origine;      // d'où elle vient — affiché, pour que personne ne surestime la preuve

  if (ALLOW_DEV) {
    // MODE DÉVELOPPEMENT (ALLOW_DEV_HASH=1)
    // -------------------------------------
    // Le genesis de dév est régénéré à chaque exécution avec un validateur jetable : son
    // empreinte diffère par construction de celle de la production, et la comparer à
    // genesis-reference.json n'a aucun sens. Mais « ne rien comparer » est ce qui avait
    // désarmé ce contrôle : un nœud servant une allocation cachée passait en ajoutant un
    // mot à la ligne de commande, et la CI affichait une coche « anti-allocation cachée »
    // au-dessus de zéro vérification.
    // Ici, le mode dév compare la chaîne au genesis de dév QUE CE DÉPÔT VIENT DE PRODUIRE.
    // Prouvé : le nœud a démarré sur ce fichier-là, allocation initiale comprise.
    // Non prouvé : quoi que ce soit sur la production.
    if (SANS_CHAINE) {
      impossible('SANS_CHAINE=1 avec ALLOW_DEV_HASH=1 : il n\'y a rien à prouver hors-ligne.',
        'Un genesis de dév n\'a pas d\'empreinte figée à confronter — son seul juge est le nœud',
        'qui l\'a chargé. Retirer l\'un des deux drapeaux.');
    }
    if (!genesis.coinbosaDev) {
      impossible(`${path.basename(GENESIS_FILE)} ne porte pas le marqueur coinbosaDev.`,
        'ALLOW_DEV_HASH n\'est pas un interrupteur « ne pas vérifier » : il désigne un genesis de dév.',
        'Pour vérifier la production, retirer ALLOW_DEV_HASH et laisser la comparaison stricte s\'appliquer.');
    }
    // Un réseau de développement s'interroge sur la machine qui l'exécute. Cette garde
    // attrape le cas réel : ALLOW_DEV_HASH=1 resté exporté dans le shell (les modes
    // opératoires en donnent une ligne à copier-coller), puis vérification de PRODUCTION
    // lancée dans la foulée — elle devenait un no-op vert.
    const distants = RPCS.filter((u) => !estBoucleLocale(u));
    if (distants.length) {
      impossible(`le mode dév vise un point d'accès distant : ${distants.join(', ')}`,
        'Un réseau de développement tourne en local ; une chaîne distante est une vraie chaîne.',
        'Retirer ALLOW_DEV_HASH de l\'environnement (unset ALLOW_DEV_HASH) et relancer la comparaison stricte.');
    }
    // Le chainId ne suffit PAS comme garde ici, et il ne remplace pas celle qui suit.
    // Il est désormais DISTINCT de celui de la production — le genesis de dév porte 262620
    // (build-genesis.js), pour qu'une signature produite ici ne vaille rien là-bas — et il
    // est comparé plus bas comme les autres champs, dérivé du FICHIER genesis désigné, pas
    // d'une valeur de configuration. Mais un chainId se DÉCLARE : n'importe quel nœud peut
    // annoncer 262620. L'empreinte du bloc 0, elle, ne se déclare pas ; c'est donc elle qui
    // doit différer de la production, sinon le « mode dév » s'appliquerait à la chaîne de
    // production elle-même.
    if (ref && horsLigne.hash.toLowerCase() === String(ref.hash).toLowerCase()) {
      impossible('l\'empreinte attendue est celle de la PRODUCTION : ce n\'est pas un réseau de développement.',
        'Retirer ALLOW_DEV_HASH=1 : la production se vérifie en mode strict, contre genesis-reference.json.');
    }
    attendu = { hash: horsLigne.hash, stateRoot: horsLigne.stateRoot, extraData: horsLigne.extraData, chainId: horsLigne.chainId, gasLimit: horsLigne.gasLimit };
    origine = `genesis de DÉVELOPPEMENT reconstruit hors-ligne (${path.basename(GENESIS_FILE)})`;
    console.log(`\n  MODE DÉVELOPPEMENT (ALLOW_DEV_HASH=1) : la chaîne locale est comparée au genesis de dév`);
    console.log('  de ce dépôt, pas à la production. Ce que ce run prouve : le nœud a démarré sur CE');
    console.log('  fichier, sans allocation ajoutée. Ce qu\'il ne prouve pas : rien sur la production.');
    if (ref) console.log(`  (référence de production figée le ${ref.fige_le} : ${String(ref.hash).slice(0, 18)}…, non utilisée ici)`);
  } else {
    if (!ref) {
      impossible(`aucune empreinte de référence figée dans ${path.basename(REF_FILE)}.`,
        'Sans référence figée, on ne peut PAS affirmer que la chaîne déployée est celle publiée,',
        'ni qu\'aucune allocation cachée n\'a été ajoutée au genesis.',
        'Figer l\'empreinte au moment du gel du genesis de production, puis relancer.');
    }
    // La référence figée doit être REDÉMONTRABLE depuis le genesis publié. Ce que ça
    // attrape : un fichier genesis modifié après le gel (allocation ajoutée au dépôt), ou
    // une référence recopiée d'un autre réseau. Les deux se voient ici sans nœud.
    const ecarts = [];
    if (horsLigne.hash.toLowerCase() !== String(ref.hash).toLowerCase()) ecarts.push(`hash : reconstruit ${horsLigne.hash}, figé ${ref.hash}`);
    if (horsLigne.stateRoot.toLowerCase() !== String(ref.stateRoot).toLowerCase()) ecarts.push(`stateRoot : reconstruit ${horsLigne.stateRoot}, figé ${ref.stateRoot}`);
    if (horsLigne.extraData.toLowerCase() !== String(ref.extraData).toLowerCase()) ecarts.push(`extraData : reconstruit ${horsLigne.extraData}, figé ${ref.extraData}`);
    if (horsLigne.chainId !== ref.chainId) ecarts.push(`chainId : fichier ${horsLigne.chainId}, figé ${ref.chainId}`);
    if (ecarts.length) {
      divergence(`${path.basename(GENESIS_FILE)} ne reproduit PAS l'empreinte figée dans ${path.basename(REF_FILE)}.`,
        ...ecarts,
        'Deux causes possibles, toutes deux graves : le fichier genesis publié a été modifié après',
        'le gel (allocation ajoutée), ou la référence figée n\'est pas celle du réseau publié.',
        'Ne rien déployer et comparer au genesis scellé avant d\'aller plus loin.');
    }
    attendu = { hash: ref.hash, stateRoot: ref.stateRoot, extraData: ref.extraData, chainId: ref.chainId, gasLimit: horsLigne.gasLimit };
    origine = `référence figée le ${ref.fige_le || '?'} (${path.basename(REF_FILE)}), redémontrée depuis ${path.basename(GENESIS_FILE)}`;
    console.log(`\n  référence figée le ${ref.fige_le || '?'} (${path.basename(REF_FILE)}) : reproduite à l'identique par la reconstruction hors-ligne.`);
    if (SANS_CHAINE) {
      console.log('\n  VÉRIFICATION HORS-LIGNE SEULE (SANS_CHAINE=1) — 4 comparaisons faites :');
      console.log(`  ${path.basename(GENESIS_FILE)} reproduit l'empreinte figée (hash, stateRoot, extraData, chainId) :`);
      console.log('  aucune allocation n\'a été ajoutée au genesis PUBLIÉ depuis le gel.');
      console.log('  AUCUNE chaîne n\'a été interrogée : ceci ne dit RIEN du réseau déployé. Pour cela,');
      console.log('  relancer sans SANS_CHAINE avec RPC=… — le contrôle refait celui-ci, puis compare le nœud.');
      return;
    }
  }

  // --- 3. la chaîne réellement branchée --------------------------------------------------
  const lireChaine = async (url) => {
    const provider = new ethers.JsonRpcProvider(url, undefined, { staticNetwork: true });
    const b0 = await provider.send('eth_getBlockByNumber', ['0x0', false]);
    if (!b0) impossible(`le nœud ${url} ne renvoie pas le bloc 0.`, 'Nœud en cours de synchronisation, ou point d\'accès qui n\'est pas une chaîne Coinbosa.');
    const chainId = Number(BigInt(await provider.send('eth_chainId', [])));
    return { b0, chainId };
  };

  const { b0, chainId } = await lireChaine(RPCS[0]);
  console.log(`\n  bloc 0 observé sur la chaîne (${RPCS[0]}) :`);
  console.log(`    hash       : ${b0.hash}`);
  console.log(`    stateRoot  : ${b0.stateRoot}`);
  console.log(`    gasLimit   : ${b0.gasLimit}`);
  console.log(`    chainId    : ${chainId}`);

  let ok = true;
  let comparaisons = 0;
  // Un champ attendu manquant n'est plus sauté en silence : c'est une comparaison qui
  // n'a pas eu lieu, donc un échec.
  const cmp = (nom, valeurAttendue, obtenu) => {
    comparaisons++;
    if (valeurAttendue === undefined || valeurAttendue === null || valeurAttendue === '') {
      console.error(`\nECHEC : ${nom} — aucune valeur attendue disponible, comparaison IMPOSSIBLE.`);
      ok = false;
      return;
    }
    if (String(valeurAttendue).toLowerCase() !== String(obtenu).toLowerCase()) {
      console.error(`\nECHEC : ${nom} divergent.\n    attendu : ${valeurAttendue}\n    obtenu  : ${obtenu}`);
      ok = false;
    }
  };

  // Cohérence structurelle : gratuite, et elle disqualifie tout de suite un en-tête bricolé.
  if (BigInt(b0.number ?? 0) !== 0n) {
    console.error(`\nECHEC : le nœud présente comme bloc 0 un bloc numéroté ${b0.number}.`);
    ok = false;
  }
  if (String(b0.parentHash).toLowerCase() !== HASH_NUL) {
    console.error(`\nECHEC : parentHash non nul (${b0.parentHash}) — le bloc 0 n'a pas de parent.`);
    ok = false;
  }
  // Le hash annoncé doit être celui de l'en-tête renvoyé, recalculé ici.
  const recalcul = hashDepuisEnTete(b0);
  if (recalcul.erreur) {
    console.error(`\nECHEC : en-tête du bloc 0 inexploitable (${recalcul.erreur}) — champs manquants ou mal formés.`);
    ok = false;
  } else if (recalcul.hash.toLowerCase() !== String(b0.hash).toLowerCase()) {
    console.error('\nECHEC : le hash annoncé n\'est pas celui de l\'en-tête renvoyé.');
    console.error(`    annoncé par le nœud : ${b0.hash}`);
    console.error(`    keccak256(RLP(en-tête)) : ${recalcul.hash}`);
    console.error(`    champs optionnels vus : ${recalcul.presents.join(', ') || 'aucun'}`);
    console.error('    Un nœud honnête ne peut pas produire cet écart : la couche RPC recopie des valeurs');
    console.error('    attendues au-dessus d\'un autre en-tête. Interroger un second point d\'accès et');
    console.error('    contrôler ce qui se trouve devant ce nœud.');
    ok = false;
  }

  cmp('hash du bloc 0', attendu.hash, b0.hash);
  cmp('stateRoot du bloc 0', attendu.stateRoot, b0.stateRoot);
  cmp('extraData du bloc 0', attendu.extraData, b0.extraData);
  cmp('gasLimit du bloc 0', attendu.gasLimit, qte(b0.gasLimit));
  // Le chainId n'entre pas dans le hash du bloc : sans cette comparaison, un clone servant
  // le vrai bloc 0 sous un autre identifiant de réseau passait pour la chaîne publiée.
  cmp('chainId du réseau', attendu.chainId, chainId);

  // Plusieurs points d'accès : ils doivent servir le même bloc 0. Un seul nœud peut mentir
  // de bout en bout ; deux exploitants distincts qui mentent à l'identique, c'est une autre
  // affaire.
  for (const url of RPCS.slice(1)) {
    const autre = await lireChaine(url);
    comparaisons++;
    if (String(autre.b0.hash).toLowerCase() !== String(b0.hash).toLowerCase() || autre.chainId !== chainId) {
      console.error(`\nECHEC : ${url} sert un autre bloc 0 (${autre.b0.hash}, chainId ${autre.chainId}).`);
      console.error('    Les points d\'accès interrogés ne sont pas sur la même chaîne : identifier lequel ment.');
      ok = false;
    } else {
      console.log(`  second point d'accès concordant : ${url}`);
    }
  }

  if (!ok) {
    divergence('la chaîne branchée N\'EST PAS celle attendue : genesis modifié, allocation ajoutée,',
      'réseau substitué, ou couche RPC falsifiée. NE PAS considérer cette chaîne comme légitime.',
      `Empreinte attendue : ${origine}.`);
  }
  // Garde-fou de dernier recours : si aucune comparaison n'a été faite, il n'y a rien à
  // annoncer. C'est la forme exacte du défaut que ce script a porté.
  if (comparaisons < 5) {
    impossible(`seulement ${comparaisons} comparaison(s) effectuée(s) — le contrôle n'a pas fait son travail.`,
      'Bug de ce script : ne pas interpréter cette sortie comme une conformité.');
  }

  if (ALLOW_DEV) {
    console.log(`\n  nœud local conforme au genesis de développement de ce dépôt (${comparaisons} comparaisons).`);
    console.log('  Prouvé : le bloc 0 servi est exactement celui de ce fichier, stateRoot compris — donc');
    console.log('  aucune allocation ajoutée par rapport à lui. NON prouvé : quoi que ce soit sur la production.');
  } else {
    console.log(`\n  chaîne conforme à la référence publiée (aucune allocation cachée possible)`);
    console.log(`  Prouvé : ${comparaisons} comparaisons — bloc 0 identique à l'empreinte figée, empreinte`);
    console.log(`  redémontrée hors-ligne depuis ${path.basename(GENESIS_FILE)}, en-tête recalculé par keccak256(RLP),`);
    console.log(`  et chainId ${chainId} conforme.`);
  }
})().catch((e) => {
  // Une erreur de transport (nœud injoignable, TLS, JSON-RPC) n'est PAS une conformité :
  // sortie 2, jamais 0.
  console.error(`\nCONTROLE IMPOSSIBLE : ${e.message}`);
  console.error(`  Point(s) d'accès interrogé(s) : ${RPCS.join(', ')}`);
  console.error('  Vérifier que le nœud répond, puis relancer : tant qu\'il ne répond pas, rien n\'est prouvé.');
  process.exit(2);
});
