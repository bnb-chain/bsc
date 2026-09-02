// Vérifie l'offre native de Coinbosa Chain — ET vérifie d'abord CONTRE QUOI il la vérifie.
//
//   RPC=https://explorer.coinbosa.com/rpc node scripts/check-supply.js
//
// LECTURE SEULE : aucune transaction, aucune écriture, aucune clé lue.
//
// CE QUE CE CONTRÔLE PROUVE
// -------------------------
//   1. que le nœud interrogé est bien Coinbosa Chain : chainId attendu, en-tête du bloc 0
//      identique au fichier genesis vérifié, empreinte du bloc 0 identique à l'empreinte
//      publiée dans genesis-reference.json ;
//   2. que l'allocation initiale déployée est celle du fichier, COMPTE PAR COMPTE — les 23
//      entrées de `alloc`, y compris les dix déclarées à zéro (contrats système et adresses
//      inter-chaînes héritées), soldes ET bytecode ;
//   3. qu'au bloc courant les six adresses inter-chaînes héritées sont toujours vides et
//      sans code, et que les comptes du genesis ne totalisent pas PLUS que l'offre.
//
// CE QU'IL NE PROUVE PAS — et qu'il ne doit donc jamais laisser croire
// --------------------------------------------------------------------
//   - Il ne mesure pas l'offre en circulation aujourd'hui. La somme des comptes du genesis
//     au bloc courant n'est pas l'offre : les fonds se déplacent vers des adresses que ce
//     script ne connaît pas. La réconciliation de garde est le travail de check-custody.js.
//   - Il n'énumère pas les comptes de la chaîne : une adresse CACHÉE, présente dans le
//     genesis déployé mais absente du fichier, lui échapperait par construction (aucune API
//     JSON-RPC standard ne permet d'énumérer un état). C'est l'empreinte du bloc 0 —
//     stateRoot, racine de Merkle de TOUT l'état initial — qui ferme ce cas ; ce contrôle
//     la compare lui aussi, et check-genesis-hash.js en fait son objet unique.
//
// POURQUOI L'ANCRAGE D'IDENTITÉ EST EN TÊTE
// -----------------------------------------
// Une version antérieure lisait des soldes sans jamais demander à qui elle parlait : lancée
// sans variable d'environnement, elle visait 127.0.0.1:8545 et certifiait « offre conforme »
// contre n'importe quel nœud qui servait les bons chiffres — y compris un nœud annonçant
// chainId 1. Ce dossier part chez une place d'échange : « conforme » doit vouloir dire
// « conforme SUR LA CHAÎNE PUBLIÉE », pas « ce nœud et ce fichier sont d'accord ».
const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

const ROOT = path.join(__dirname, '..');
const RPC = process.env.RPC || 'http://127.0.0.1:8545';
// Le fichier genesis à vérifier est paramétrable : la production vise genesis-coinbosa.json,
// la vérification mécanique (CI/local) vise genesis-coinbosa-dev.json via la variable GENESIS.
const GENESIS_FILE = process.env.GENESIS || path.join(ROOT, 'genesis', 'genesis-coinbosa.json');
const REF_FILE = process.env.GENESIS_REF || path.join(ROOT, 'genesis', 'genesis-reference.json');
const config = JSON.parse(fs.readFileSync(path.join(ROOT, 'coinbosa.config.json'), 'utf8'));
const genesis = JSON.parse(fs.readFileSync(GENESIS_FILE, 'utf8'));

const WEI = 10n ** 18n;
// Offre figée dans le contrôle lui-même. coinbosa.config.json est un fichier du dépôt :
// il peut être modifié dans le même commit que le genesis, et le contrôle défendrait alors
// docilement le nouveau chiffre. L'invariant « 700 000 000, fixé au bloc 0 » est irréversible
// (AGENTS.md) : il est écrit ici en dur pour qu'un changement de configuration se voie.
const OFFRE_FIGEE = 700000000n;

// Adresses inter-chaînes héritées du réseau amont. Sur une chaîne souveraine elles n'ont
// aucun objet : ni solde, ni bytecode, jamais — au genesis comme aujourd'hui. Un bytecode
// qui réapparaît à l'une d'elles serait un pont opérationnel non annoncé.
const XCHAIN = {
  '0x0000000000000000000000000000000000001003': 'LightClient hérité',
  '0x0000000000000000000000000000000000001004': 'TokenHub hérité (le pont)',
  '0x0000000000000000000000000000000000001005': 'RelayerIncentivize hérité',
  '0x0000000000000000000000000000000000001006': 'RelayerHub hérité',
  '0x0000000000000000000000000000000000001008': 'TokenManager hérité',
  '0x0000000000000000000000000000000000002000': 'CrossChain hérité',
};
// Le contrat système 0x…1000 reçoit les frais de chaque bloc par transaction système
// (parlia.distributeToValidator → deposit(address), consensus/parlia/parlia.go). Son solde
// au bloc COURANT est donc normalement non nul sur une chaîne qui tourne : l'exiger à zéro
// ferait crier ce contrôle sur une chaîne parfaitement saine. Au bloc 0, en revanche, il est
// à zéro comme tout le reste, et c'est là qu'il est contrôlé strictement.
const VALSET = '0x0000000000000000000000000000000000001000';

// Signatures d'élagage d'état de geth. Le filtre précédent acceptait le simple fragment
// « not available » : un 503 de passerelle, une limitation de débit ou une méthode désactivée
// suffisaient à déplacer silencieusement le point de mesure vers la tête ET à faire imprimer
// « état du bloc 0 purgé », un diagnostic que rien n'avait établi. Un rapport trompeur parce
// qu'il est explicatif est pire qu'une erreur brute.
const ELAGAGE = [
  /missing trie node/i,
  /historical state[\s\S]{0,120}not available/i,
  /state is not available/i,
  /state unavailable/i,
];

const bosa = (x) => {
  const e = x / WEI, r = x % WEI;
  return r === 0n ? `${e.toLocaleString('en-US')} BOSA` : `${e.toLocaleString('en-US')} BOSA + ${r} wei`;
};
const bas = (s) => String(s == null ? '' : s).toLowerCase();
const octets = (c) => (c === '0x' ? 'aucun code' : `${c.length / 2 - 1} octets`);
const hexNb = (s) => BigInt(s == null ? 0 : s);
// L'URL est imprimée dans le rapport : on retire un éventuel identifiant/mot de passe.
const rpcAffiche = RPC.replace(/\/\/[^@/]*@/, '//***@');

const echecs = [];
const noter = (titre, ...quoiFaire) => echecs.push({ titre, quoiFaire });
function refus(titre, ...quoiFaire) {
  console.error(`\nECHEC : ${titre}`);
  quoiFaire.forEach((l) => console.error(`  ${l}`));
  process.exit(1);
}

// ---------------------------------------------------------------------------
// 0. Contrôles de FICHIER — ils ne dépendent d'aucun nœud
// ---------------------------------------------------------------------------

// Refus dur PAR DÉFAUT : un genesis de développement (adresses synthétiques, validateur
// crédité) ne doit jamais passer pour un genesis de production.
const ALLOW_DEV_SUPPLY = process.env.ALLOW_DEV_SUPPLY === '1';
if (genesis.coinbosaDev && !ALLOW_DEV_SUPPLY) {
  refus(`${path.basename(GENESIS_FILE)} porte le marqueur coinbosaDev — genesis de DÉVELOPPEMENT, non déployable en production.`,
    'Pour une vérification mécanique assumée : ALLOW_DEV_SUPPLY=1 node scripts/check-supply.js',
    'Pour un contrôle de production : GENESIS=genesis/genesis-coinbosa.json et un RPC de production.');
}
// La dérogation ne vaut que pour un genesis de DÉV. Posée sur un genesis de production, elle
// n'aurait rien à assouplir mais relâcherait l'ancrage d'identité ci-dessous : on la refuse
// au lieu de la laisser désarmer un contrôle sans que personne s'en aperçoive.
if (ALLOW_DEV_SUPPLY && !genesis.coinbosaDev) {
  refus(`ALLOW_DEV_SUPPLY=1 alors que ${path.basename(GENESIS_FILE)} ne porte pas le marqueur coinbosaDev.`,
    'Cette dérogation ne s\'applique qu\'à un genesis de développement.',
    'Retirer ALLOW_DEV_SUPPLY, ou viser le genesis de dév avec GENESIS=genesis/genesis-coinbosa-dev.json.');
}

if (BigInt(config.nativeCoin.totalSupply) !== OFFRE_FIGEE) {
  refus(`coinbosa.config.json annonce une offre de ${config.nativeCoin.totalSupply} BOSA, ${OFFRE_FIGEE} figés dans ce contrôle.`,
    'L\'offre est un invariant irréversible (AGENTS.md) : elle est fixée au bloc 0 et ne se corrige pas après coup.',
    'Si ce changement est délibéré, il passe par une décision explicite — pas par une modification de configuration.');
}
const EXPECTED = OFFRE_FIGEE * WEI;

// Somme des soldes DÉCLARÉS dans le fichier. Ce contrôle est distinct de la somme lue
// on-chain : quand les deux coïncident (aucun écart), la seconde ne prouve rien de plus que
// la première, et une panne de l'une serait masquée par l'autre. On les sépare pour que
// chacune échoue pour sa propre raison.
if (!genesis.alloc || typeof genesis.alloc !== 'object' || !Object.keys(genesis.alloc).length) {
  refus(`${path.basename(GENESIS_FILE)} ne contient aucune section \`alloc\`.`,
    'Sans allocation à confronter, ce contrôle n\'a rien à vérifier : il ne conclut pas.',
    'Vérifier GENESIS, ou régénérer le genesis avec scripts/build-genesis.js.');
}
let declareTotal = 0n;
for (const v of Object.values(genesis.alloc)) declareTotal += v.balance ? BigInt(v.balance) : 0n;
if (declareTotal !== EXPECTED) {
  refus(`${path.basename(GENESIS_FILE)} alloue ${bosa(declareTotal)}, ${bosa(EXPECTED)} attendus.`,
    'Le fichier lui-même ne boucle pas sur l\'offre : régénérer le genesis (scripts/build-genesis.js) et recommencer.');
}

const CHAINID_ATTENDU = BigInt(config.network.chainId);
const chainIdFichier = genesis.config ? genesis.config.chainId : null;
if (chainIdFichier == null || BigInt(chainIdFichier) !== CHAINID_ATTENDU) {
  refus(`${path.basename(GENESIS_FILE)} déclare chainId ${chainIdFichier === null ? '(absent)' : chainIdFichier}, ${CHAINID_ATTENDU} attendu.`,
    'Ce fichier n\'est pas un genesis de Coinbosa Chain — vérifier GENESIS.');
}

// Empreinte publiée du bloc 0 de production. Elle sert dans les DEUX sens :
//   production     — le bloc 0 observé doit lui être IDENTIQUE ;
//   développement  — il doit en DIFFÉRER, ce qui est la seule preuve disponible que la
//                    dérogation de dév s'applique bien à un réseau qui n'est pas la production.
let ref = null;
if (fs.existsSync(REF_FILE)) {
  const parsed = JSON.parse(fs.readFileSync(REF_FILE, 'utf8'));
  if (parsed.hash && !/^0x0*$/.test(parsed.hash)) ref = parsed;
}

(async () => {
  const provider = new ethers.JsonRpcProvider(RPC);

  // -------------------------------------------------------------------------
  // 1. Ancrage : à QUI parle-t-on ?
  // -------------------------------------------------------------------------
  let reseau;
  try {
    reseau = await provider.getNetwork();
  } catch (e) {
    refus(`aucune réponse JSON-RPC de ${rpcAffiche} (${e.shortMessage || e.message}).`,
      'Vérifier que le nœud est démarré et que RPC pointe sur lui.');
  }
  if (reseau.chainId !== CHAINID_ATTENDU) {
    refus(`le nœud ${rpcAffiche} annonce chainId ${reseau.chainId}, ${CHAINID_ATTENDU} attendu (Coinbosa Chain).`,
      'Ce n\'est PAS la chaîne à vérifier : rien de ce qui suit n\'aurait de valeur.',
      'Corriger RPC pour viser un nœud Coinbosa.');
  }

  const b0 = await provider.send('eth_getBlockByNumber', ['0x0', false]);
  if (!b0) {
    refus(`le nœud ${rpcAffiche} ne renvoie pas le bloc 0.`,
      'Sans l\'en-tête du bloc 0, l\'identité de la chaîne n\'est pas établissable.',
      'Utiliser un nœud qui sert l\'historique complet (--syncmode full).');
  }

  // En-tête du bloc 0 observé contre le FICHIER vérifié. Tous ces champs viennent
  // littéralement du fichier au moment du `geth init` : s'ils concordent, le nœud a bien été
  // initialisé avec ce genesis-là. Seul stateRoot n'est pas recalculable ici — c'est
  // exactement ce que couvre la comparaison d'empreinte juste après.
  const champs = [
    ['extraData', genesis.extraData, b0.extraData, bas],
    ['gasLimit', genesis.gasLimit, b0.gasLimit, hexNb],
    ['difficulty', genesis.difficulty, b0.difficulty, hexNb],
    ['timestamp', genesis.timestamp, b0.timestamp, hexNb],
    ['nonce', genesis.nonce, b0.nonce, hexNb],
    ['mixHash', genesis.mixHash, b0.mixHash, bas],
    ['coinbase', genesis.coinbase, b0.miner, bas],
  ];
  // Un champ absent d'un côté n'est pas comparé — et le rapport dit combien l'ont été,
  // pour qu'on ne prenne pas une comparaison partielle pour une comparaison complète.
  const enTete = champs.filter(([, a, o]) => a != null && o != null)
    .map(([n, a, o, f]) => [n, String(f(a)), String(f(o))]);
  const enTeteEcarts = enTete.filter(([, a, o]) => a !== o);
  if (enTeteEcarts.length) {
    console.error(`\nECHEC : le bloc 0 du nœud ne correspond pas à ${path.basename(GENESIS_FILE)} :`);
    enTeteEcarts.forEach(([n, a, o]) => console.error(`  ${n} : ${o} sur la chaîne, ${a} dans le fichier`));
    console.error('  Le nœud a été initialisé avec un AUTRE genesis : les soldes lus ci-après ne diraient');
    console.error('  rien du fichier vérifié. Corriger RPC, ou viser le bon fichier avec GENESIS.');
    process.exit(1);
  }

  const dev = !!genesis.coinbosaDev;
  if (!ref) {
    refus(`aucune empreinte de référence figée dans ${path.basename(REF_FILE)}.`,
      dev ? 'Sans elle, impossible d\'établir que ce nœud n\'est PAS la production : la dérogation de dév serait crue sur parole.'
          : 'Sans elle, on ne peut pas affirmer que la chaîne branchée est celle qui a été publiée.',
      'Restaurer genesis/genesis-reference.json (dépôt), ou figer l\'empreinte au gel du genesis de production.');
  }
  const memeChaine = bas(b0.hash) === bas(ref.hash) && bas(b0.stateRoot) === bas(ref.stateRoot);
  if (!dev && !memeChaine) {
    console.error(`\nECHEC : le bloc 0 du nœud n'est pas celui publié (${path.basename(REF_FILE)}, figé le ${ref.fige_le || '?'}) :`);
    console.error(`  hash      : ${b0.hash}\n              attendu ${ref.hash}`);
    console.error(`  stateRoot : ${b0.stateRoot}\n              attendu ${ref.stateRoot}`);
    console.error('  Genesis modifié, allocation ajoutée, ou mauvais réseau. NE PAS traiter cette chaîne');
    console.error('  comme Coinbosa Chain, et ne pas produire ce rapport comme preuve d\'offre.');
    process.exit(1);
  }
  if (dev && memeChaine) {
    refus('ALLOW_DEV_SUPPLY=1 est posé alors que le nœud interrogé EST le réseau de production.',
      `Le bloc 0 observé est identique à l'empreinte publiée (${ref.hash}).`,
      'La dérogation de développement désarmerait des contrôles sur la production : elle est refusée.',
      'Retirer ALLOW_DEV_SUPPLY et relancer avec le genesis de production.');
  }

  const tete = await provider.getBlockNumber();
  console.log('  chaîne interrogée');
  console.log(`    RPC        : ${rpcAffiche}`);
  console.log(`    chainId    : ${reseau.chainId}`);
  console.log(`    bloc 0     : ${b0.hash}`);
  console.log(`    stateRoot  : ${b0.stateRoot}`);
  console.log(`    bloc courant : ${tete.toLocaleString('en-US')}`);
  console.log(`    genesis    : ${path.basename(GENESIS_FILE)} (en-tête du bloc 0 : ${enTete.length}/${champs.length} champs comparés, tous conformes)`);
  if (dev) {
    console.log(`    empreinte  : DIFFÉRENTE de la production (${String(ref.hash).slice(0, 18)}…) — réseau de développement confirmé`);
    console.warn('\n⚠  MODE DÉVELOPPEMENT (ALLOW_DEV_SUPPLY=1) : contrôle mécanique sur un genesis de DÉV.');
    console.warn('   Ce rapport n\'est PAS une preuve de production.');
  } else {
    console.log(`    empreinte  : CONFORME à ${path.basename(REF_FILE)}, figée le ${ref.fige_le || '?'}`);
  }

  // -------------------------------------------------------------------------
  // 2. Où lire les soldes ? Le bloc 0 quand son état est servi, sinon la tête
  // -------------------------------------------------------------------------
  // L'état du bloc 0 finit par être élagué : geth ne conserve l'état historique que sur une
  // fenêtre glissante (le nœud de production tourne en --gcmode full). L'en-tête reste
  // lisible, l'état non. Le repli est légitime, mais il DOIT être établi et non supposé :
  // en-tête du bloc 0 présent (vérifié ci-dessus) ET refus d'état portant une signature
  // d'élagage. Toute autre erreur arrête le contrôle.
  const sonde = Object.keys(genesis.alloc)[0];
  let bloc = 0;
  let motifRepli = null;
  try {
    await provider.getBalance(sonde, 0);
  } catch (e) {
    const texte = [e && e.shortMessage, e && e.message, e && e.error && e.error.message,
      e && e.info && e.info.error && e.info.error.message].filter(Boolean).join(' | ');
    const brut = (e && e.error && e.error.code) != null ? e.error.code
      : (e && e.info && e.info.error && e.info.error.code);
    const codeJsonRpc = typeof brut === 'number' ? brut : undefined;
    const msgNoeud = (e && e.info && e.info.error && e.info.error.message)
      || (e && e.error && e.error.message) || (e && e.shortMessage) || (e && e.message) || '';
    const elague = ELAGAGE.some((r) => r.test(texte)) && (codeJsonRpc === undefined || codeJsonRpc === -32000);
    if (!elague) {
      refus("la lecture d'état au bloc 0 a échoué, et l'erreur n'est PAS un élagage d'état :",
        `  « ${msgNoeud} »${codeJsonRpc !== undefined ? ` (code JSON-RPC ${codeJsonRpc})` : ''}`,
        'Ce contrôle ne bascule pas de point de mesure sur une erreur qu\'il ne comprend pas :',
        'un rapport mesuré ailleurs que là où il l\'annonce ne vaut rien.',
        'Traiter l\'erreur du nœud (passerelle, limitation de débit, méthode désactivée) puis relancer.');
    }
    bloc = tete;
    motifRepli = msgNoeud;
  }

  const auGenesis = bloc === 0;
  if (!auGenesis) {
    console.log(`\n  point de mesure : bloc courant ${tete.toLocaleString('en-US')}`);
    console.log('    (état du bloc 0 élagué sur ce nœud — en-tête servi, état refusé :');
    console.log(`     « ${motifRepli.slice(0, 120)} »)`);
    console.log('    L\'allocation initiale n\'est donc PAS relue compte par compte ici.');
  } else {
    console.log('\n  point de mesure : bloc 0 (allocation initiale, état servi par le nœud)');
  }

  // -------------------------------------------------------------------------
  // 3. Les 23 comptes du genesis, au point de mesure — AUCUN n'est sauté
  // -------------------------------------------------------------------------
  // La version précédente sortait de la boucle sur `declared === 0n` : les dix comptes
  // déclarés à zéro — contrats système et adresses inter-chaînes — n'étaient JAMAIS lus.
  // Ils pouvaient donc détenir n'importe quelle quantité de BOSA sans que le total annoncé
  // bouge d'un wei : une émission cachée sur un contrat système passait « conforme ».
  // Un solde déclaré à zéro est une AFFIRMATION, elle se vérifie comme les autres.
  let totalMesure = 0n;
  const ecarts = [];
  const codesDivergents = [];
  const soldes = {};
  for (const [addr, v] of Object.entries(genesis.alloc)) {
    const declare = v.balance ? BigInt(v.balance) : 0n;
    const onchain = await provider.getBalance(addr, bloc);
    soldes[addr] = onchain;
    // Au bloc 0, le solde doit être EXACTEMENT celui déclaré. Au bloc courant, les fonds ont
    // pu bouger légitimement (ils changent de main, l'offre ne bouge pas) : exiger l'égalité
    // ferait crier ce contrôle à chaque transfert de trésorerie — c'est ce qui s'est produit
    // en production, où il annonçait « ÉCHEC : offre de 699 998 999 » pour 1 000 BOSA
    // simplement transférés au gouverneur. On n'y garde que ce qui reste vrai : la borne
    // d'émission (§4) et les adresses inter-chaînes, qui n'ont aucune raison d'être créditées.
    if (auGenesis && onchain !== declare) ecarts.push({ addr, declare, onchain });
    totalMesure += onchain;

    const codeDeclare = bas(v.code || '0x') || '0x';
    const codeOnchain = bas(await provider.getCode(addr, bloc)) || '0x';
    // Le bytecode se compare au même point de mesure que les soldes. La version précédente
    // lisait le code à la TÊTE tout en lisant les soldes au bloc 0, puis concluait « contrats
    // inter-chaînes purgés » — une affirmation sur le GENESIS établie ailleurs qu'au genesis.
    // SELFDESTRUCT efface encore le code sur cette chaîne (shanghaiTime:0, pas de cancunTime) :
    // un pont hérité présent au bloc 0 puis détruit satisfaisait le contrôle.
    if (codeOnchain !== codeDeclare) {
      codesDivergents.push({ addr, declare: codeDeclare, onchain: codeOnchain });
    }
  }

  if (auGenesis) {
    console.log(`  allocation lue au bloc 0 (${Object.keys(genesis.alloc).length} comptes) : ${bosa(totalMesure)}`);
    console.log(`  attendu                               : ${bosa(EXPECTED)}`);
  } else {
    console.log(`  comptes du genesis au bloc ${tete.toLocaleString('en-US')} (${Object.keys(genesis.alloc).length} comptes) : ${bosa(totalMesure)}`);
    console.log(`  (ce n'est PAS l'offre en circulation : les fonds sortis de ces comptes n'y figurent plus.`);
    console.log('   La réconciliation de garde est faite par check-custody.js.)');
  }

  if (auGenesis && totalMesure !== EXPECTED) {
    noter(`l'allocation lue au bloc 0 vaut ${bosa(totalMesure)}, ${bosa(EXPECTED)} attendus.`,
      'Le genesis déployé n\'alloue pas l\'offre annoncée : ne pas publier ce réseau comme Coinbosa Chain.');
  }
  if (ecarts.length) {
    const caches = ecarts.filter((m) => m.declare === 0n);
    noter('soldes du bloc 0 divergents du fichier :',
      ...ecarts.map((m) => `${m.addr} : ${bosa(m.onchain)} on-chain, ${bosa(m.declare)} déclarés (${m.onchain - m.declare > 0n ? '+' : ''}${m.onchain - m.declare} wei)`),
      ...(caches.length ? ['Les comptes déclarés à ZÉRO qui détiennent des fonds au bloc 0 sont une ÉMISSION',
        'CACHÉE dans le genesis déployé : arrêter tout déploiement et régénérer le genesis.'] : []),
      'Sinon : le nœud n\'a pas été initialisé avec ce fichier — vérifier GENESIS et le datadir.');
  }
  if (codesDivergents.length) {
    noter(`bytecode divergent du fichier au ${auGenesis ? 'bloc 0' : 'bloc courant'} :`,
      ...codesDivergents.map((c) => `${c.addr} : on-chain ${octets(c.onchain)}, fichier ${octets(c.declare)}`),
      'Un contrat système substitué change ce que fait la chaîne ; un contrat inter-chaînes',
      'qui réapparaît est un pont opérationnel non annoncé. Comparer avec build-genesis.js.');
  }

  // -------------------------------------------------------------------------
  // 4. Au bloc COURANT : ce qui reste vérifiable sans énumérer la chaîne
  // -------------------------------------------------------------------------
  // Lire l'allocation au bloc 0 ne dit rien de ce qui a été créé DEPUIS. Deux affirmations
  // tiennent au bloc courant sans jamais crier à tort :
  //   a) les six adresses inter-chaînes héritées restent vides et sans code — rien de
  //      légitime ne les crédite, aucune clé ne les contrôle ;
  //   b) les comptes du genesis ne peuvent pas totaliser PLUS que l'offre. L'offre totale est
  //      constante (le consensus ne crée pas de monnaie) : un sous-ensemble de comptes ne
  //      peut donc jamais dépasser 700 000 000. Un dépassement est une émission, quelles que
  //      soient les circulations internes. C'est le seul énoncé qui reste vrai quoi qu'il
  //      arrive aux transferts de trésorerie.
  let totalTete = totalMesure;
  if (auGenesis) {
    totalTete = 0n;
    for (const addr of Object.keys(genesis.alloc)) totalTete += await provider.getBalance(addr, tete);
  }
  if (totalTete > EXPECTED) {
    noter(`les comptes du genesis totalisent ${bosa(totalTete)} au bloc ${tete}, soit PLUS que l'offre (${bosa(EXPECTED)}).`,
      'L\'offre totale est constante : un sous-ensemble de comptes ne peut pas la dépasser.',
      'Il y a eu ÉMISSION. Geler la chaîne pour l\'exchange et remonter les blocs concernés.');
  }

  let xchainAnomalies = 0;
  for (const [addr, role] of Object.entries(XCHAIN)) {
    const solde = await provider.getBalance(addr, tete);
    const code = bas(await provider.getCode(addr, tete)) || '0x';
    if (solde !== 0n) {
      xchainAnomalies++;
      noter(`${addr} (${role}) détient ${bosa(solde)} au bloc ${tete}.`,
        'Cette adresse doit rester vide : aucune clé ne la contrôle et rien de légitime ne la crédite.',
        'Identifier la transaction qui l\'a créditée avant toute publication de chiffre d\'offre.');
    }
    if (code !== '0x') {
      xchainAnomalies++;
      noter(`${addr} (${role}) porte du bytecode au bloc ${tete} (${octets(code)}).`,
        'Un contrat inter-chaînes déployé après le genesis est un pont non annoncé :',
        'il permettrait d\'émettre ou de sortir des BOSA hors du cadre publié.');
    }
  }

  const potFrais = await provider.getBalance(VALSET, tete);
  console.log(`  contrats inter-chaînes (${Object.keys(XCHAIN).length}) au bloc courant : ${xchainAnomalies ? `${xchainAnomalies} ANOMALIE(S) — voir échecs ci-dessous` : 'sans solde et sans code'}`);
  console.log(`  frais accumulés sur 0x…1000 au bloc courant : ${bosa(potFrais)}`);
  console.log('    (alimenté par les dépôts de frais à chaque bloc : un solde non nul y est attendu sur');
  console.log('     une chaîne active. Au bloc 0 il est contrôlé strictement, comme tout le reste.)');
  console.log(`  borne d'émission (comptes du genesis ≤ ${bosa(EXPECTED)}) : ${totalTete > EXPECTED ? 'DÉPASSÉE' : 'respectée'}`);

  // -------------------------------------------------------------------------
  // 5. Verdict — il dit ce qui a été prouvé, et à quel bloc
  // -------------------------------------------------------------------------
  if (echecs.length) {
    for (const e of echecs) {
      console.error(`\nECHEC : ${e.titre}`);
      e.quoiFaire.forEach((l) => console.error(`  ${l}`));
    }
    process.exit(1);
  }

  console.log('');
  if (auGenesis) {
    console.log(`  CONFORME AU BLOC 0 : les ${Object.keys(genesis.alloc).length} comptes du genesis déployé portent exactement les soldes`);
    console.log(`  et le bytecode de ${path.basename(GENESIS_FILE)}, pour un total de ${bosa(EXPECTED)}.`);
  } else {
    console.log(`  CONFORME AU BLOC ${tete.toLocaleString('en-US')} : aucun résidu inter-chaînes, aucune émission décelable sur les`);
    console.log('  comptes du genesis (borne respectée). L\'allocation initiale n\'a pas été relue ici —');
    console.log(`  elle est engagée par le stateRoot du bloc 0, conforme à ${path.basename(REF_FILE)} ci-dessus.`);
  }
  if (dev) {
    console.log('  Réseau de DÉVELOPPEMENT (empreinte différente de la production) : vaut pour la mécanique,');
    console.log('  pas comme preuve de production.');
  }
  console.log('  Ce contrôle ne mesure pas l\'offre en circulation : voir check-custody.js.');
})().catch((e) => {
  console.error('\nERREUR : le contrôle n\'a pas pu aller à son terme —', e.shortMessage || e.message);
  console.error('  Aucune conclusion n\'est tirée : un contrôle interrompu ne vaut pas un contrôle passé.');
  process.exit(1);
});
