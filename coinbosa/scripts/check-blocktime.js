// Prouve, EN DIRECT, que le nœud interrogé est bien Coinbosa Chain et qu'il
// PRODUIT des blocs à la période du client patché (5 s).
//
// CE QUE CE CONTRÔLE ATTRAPE — et qui n'est attrapé nulle part ailleurs :
//
//   1. LE MAUVAIS BINAIRE. La période n'est pas lue dans le genesis : c'est une
//      constante Go (consensus/parlia/parlia.go, defaultBlockInterval). Démarrer
//      la chaîne avec le geth d'amont de BNB Chain donnerait des blocs de 3 s
//      sans qu'aucun fichier du dépôt ne change, et sans aucune erreur. Personne
//      d'autre que ce script ne s'en apercevrait.
//
//   2. LA CHAÎNE ARRÊTÉE. Un nœud dont le validateur ne scelle plus (unlock en
//      échec, clé retirée du jeu, epoch bloquée) répond encore parfaitement au
//      RPC et sert une histoire ancienne, produite jadis à 5 s. Relire l'histoire
//      revient alors à déclarer conforme une chaîne morte. On ne mesure donc que
//      des blocs dont on a observé soi-même la naissance.
//
//   3. LE NŒUD EN SYNCHRONISATION. Un nœud qui importe une histoire depuis un
//      pair voit lui aussi sa hauteur monter, sans rien produire. Il se trahit
//      par le temps : ses blocs arrivent plus vite que les intervalles qu'ils
//      déclarent. C'est le contrôle « temps de chaîne contre temps réel ».
//
//   4. LE MAUVAIS NŒUD. Le port 8545 est celui de tout geth/anvil/hardhat qui
//      traîne sur la machine, et la répétition POBS fait tourner ce script sur
//      18545 : une inversion de port validerait silencieusement une autre chaîne.
//      D'où la comparaison du chainId annoncé avec celui de la configuration.
//
//   5. LA RÉFÉRENCE DISPARUE. Si la valeur attendue s'évapore de la
//      configuration, une comparaison avec NaN est toujours fausse : le contrôle
//      ne peut plus échouer et déclare conforme n'importe quel temps de bloc.
//      Une valeur de référence absente doit faire échouer le contrôle, jamais le
//      neutraliser. Même règle pour la valeur PRÉSENTE MAIS RETOUCHÉE : elle est
//      recoupée avec la constante Go, seule source qui fait autorité.
//
// POURQUOI LE MINIMUM ET NON LA MOYENNE — c'est le cœur du contrôle.
// Parlia refuse tout en-tête tel que
//     header.MilliTimestamp() < parent.MilliTimestamp() + snap.BlockInterval + backOffTime(...)
// (consensus/parlia/ramanujanfork.go:45). La période est donc un PLANCHER DUR :
// un intervalle réel peut excéder la période (bloc manqué, backoff d'un
// validateur hors tour, machine saturée), jamais lui être inférieur. Le minimum
// des intervalles observés est par conséquent le seul estimateur honnête de la
// période, et un unique intervalle trop court suffit à condamner le binaire.
// La moyenne, elle, se laisse acheter : cinq intervalles de 3 s plus un trou de
// production de 15 s font une moyenne de 5,00 s — une chaîne à 3 s déclarée
// conforme, exactement le cas que ce script existe pour détecter.
const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

const RPC = process.env.RPC || 'http://127.0.0.1:8545';

const RACINE_COINBOSA = path.join(__dirname, '..');
const RACINE_CLIENT = path.join(__dirname, '..', '..');
const CHEMIN_CONFIG = path.join(RACINE_COINBOSA, 'coinbosa.config.json');
const CHEMIN_GENESIS = path.join(RACINE_COINBOSA, 'genesis', 'genesis-base.json');
const CHEMIN_PARLIA = path.join(RACINE_CLIENT, 'consensus', 'parlia', 'parlia.go');

// Nombre d'intervalles mesurés entre blocs dont on a vu la naissance. Il en faut
// assez pour qu'au moins un bloc ait été scellé « à l'heure » par son validateur
// en tour (backoff nul, donc intervalle égal à la période exacte) : sur une
// chaîne saine c'est le cas de la quasi-totalité des blocs, sur une chaîne
// dégradée d'une bonne part d'entre eux. Douze intervalles laissent la marge
// nécessaire sans faire attendre la CI plus d'une minute.
const SAMPLES = 12;
// Les horodatages sont à la seconde : avec une période de 5 s, cette tolérance
// n'accepte en pratique que 5 s pile. Le test est `>=` et non `>` — à `>`, une
// mesure à 4,50 s franchissait la borne.
const TOLERANCE = 0.5;

// Forks BSC qui changent la période sous la seconde : leur présence ferait de
// « 5 s » une valeur périmée, et des horodatages à la seconde ne sauraient
// même plus la mesurer.
const FORKS_SOUS_SECONDE = ['lorentzTime', 'maxwellTime', 'fermiTime'];

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Un échec s'adresse à quelqu'un qui va devoir agir : il dit ce qui manque,
// puis quoi faire.
function echec(motif, ...conduite) {
  console.error(`\nECHEC : ${motif}`);
  for (const ligne of conduite) console.error(ligne);
  process.exit(1);
}

// --- 1. La référence attendue, et sa provenance -----------------------------

let config;
try {
  config = JSON.parse(fs.readFileSync(CHEMIN_CONFIG, 'utf8'));
} catch (e) {
  echec(`coinbosa.config.json illisible (${e.message}).`,
    `Attendu à : ${CHEMIN_CONFIG}`,
    'Ce fichier porte la période et le chainId attendus : sans lui il n’y a rien à comparer,',
    'et un contrôle qui ne peut pas comparer ne doit pas conclure. Lancer ce script depuis le dépôt.');
}

const reseau = (config && config.network) || {};
const MS_ATTENDU = Number(reseau.blockIntervalMs);
const EXPECTED = MS_ATTENDU / 1000;

if (!Number.isFinite(EXPECTED) || EXPECTED <= 0) {
  echec(`coinbosa.config.json ne fournit pas de network.blockIntervalMs exploitable (lu : ${JSON.stringify(reseau.blockIntervalMs)}).`,
    'Sans valeur attendue, toute comparaison rend false et TOUT temps de bloc passerait pour conforme.',
    'Rétablir network.blockIntervalMs (5000 pour Coinbosa) dans coinbosa/coinbosa.config.json.');
}
if (EXPECTED < 1) {
  echec(`période attendue de ${EXPECTED} s : non mesurable ici.`,
    'Les horodatages RPC sont à la seconde ; une période sous la seconde exige de lire',
    'les millisecondes d’en-tête (header.MilliTimestamp). Ce script mesurerait 0 et mentirait.');
}

const CHAIN_ID_ATTENDU = Number(reseau.chainId);
if (!Number.isInteger(CHAIN_ID_ATTENDU) || CHAIN_ID_ATTENDU <= 0) {
  echec(`coinbosa.config.json ne fournit pas de network.chainId exploitable (lu : ${JSON.stringify(reseau.chainId)}).`,
    'Sans chainId attendu, ce contrôle ne saurait plus dire QUELLE chaîne il a mesurée.',
    'Rétablir network.chainId (26262 pour Coinbosa) dans coinbosa/coinbosa.config.json.');
}

// La configuration se déclare elle-même non normative sur ce point : « ces
// valeurs documentent le client patché, elles ne le configurent pas ». Passer
// cette ligne à 3000 suffirait donc à faire accepter un binaire d'amont sans
// qu'aucun contrôle ne proteste. On la recoupe avec la seule source qui fait
// autorité : la constante Go compilée dans le client.
let sourceParlia;
try {
  sourceParlia = fs.readFileSync(CHEMIN_PARLIA, 'utf8');
} catch (e) {
  echec(`consensus/parlia/parlia.go illisible (${e.message}).`,
    `Attendu à : ${CHEMIN_PARLIA}`,
    'La période attendue ne peut pas être recoupée sans le source du client, et une période',
    'attendue non recoupée est une valeur qu’il suffit de retoucher pour désarmer ce contrôle.',
    'Lancer ce script depuis le dépôt du client (coinbosa/scripts/ dans l’arbre go-ethereum).');
}
const trouve = sourceParlia.match(/defaultBlockInterval\s+uint64\s*=\s*(\d+)/);
if (!trouve) {
  echec('la constante defaultBlockInterval est introuvable dans consensus/parlia/parlia.go.',
    'Elle a été renommée ou supprimée : la période du client n’est plus vérifiable ici.',
    'Rétablir la constante, ou mettre ce contrôle à jour sur la nouvelle source de vérité.');
}
if (Number(trouve[1]) !== Math.round(EXPECTED * 1000)) {
  echec(`parlia.go déclare ${trouve[1]} ms par bloc, coinbosa.config.json en déclare ${Math.round(EXPECTED * 1000)}.`,
    'Les deux doivent dire la même chose : le client impose la période, la configuration ne fait que la documenter.',
    'Aligner coinbosa.config.json sur parlia.go — et si c’est parlia.go qui a changé, c’est l’identité',
    'du réseau qui change : la décision ne se prend pas dans un script de vérification.');
}

// Un fork BSC à période réduite rendrait « 5 s » faux à partir de son
// activation. Aucun n'est présent aujourd'hui ; on refuse qu'un arrive sans
// que la valeur attendue soit revue.
let genesisBase;
try {
  genesisBase = JSON.parse(fs.readFileSync(CHEMIN_GENESIS, 'utf8'));
} catch (e) {
  echec(`genesis/genesis-base.json illisible (${e.message}).`,
    `Attendu à : ${CHEMIN_GENESIS}`,
    'Sans lui, impossible de vérifier qu’aucun fork à période réduite n’est activé.');
}
const forksActifs = FORKS_SOUS_SECONDE.filter((k) => ((genesisBase && genesisBase.config) || {})[k] != null);
if (forksActifs.length) {
  echec(`genesis-base.json active ${forksActifs.join(', ')} : la période cesse d’être ${EXPECTED} s.`,
    'Lorentz vaut 1500 ms, Maxwell 750 ms, Fermi 450 ms (consensus/parlia/parlia.go).',
    'Retirer ces clés du genesis, ou revoir la période attendue ET ce contrôle avant de les activer.');
}

(async () => {
  const provider = new ethers.JsonRpcProvider(RPC);

  // --- 2. Identité de la chaîne, AVANT de mesurer quoi que ce soit ----------
  // Mesurer d'abord et vérifier ensuite ferait perdre une minute pour finir par
  // dire « ce n'était pas la bonne chaîne ». Et sans ce contrôle, le script
  // validerait « un point RPC qui produit des blocs de 5 s », pas Coinbosa.
  let reseauRpc;
  try {
    reseauRpc = await provider.getNetwork();
  } catch (e) {
    echec(`aucune réponse exploitable de ${RPC} (${e.message}).`,
      'Vérifier que le nœud est démarré et que son API HTTP est ouverte (--http --http.api eth,net,web3),',
      'ou pointer RPC= sur le bon point d’entrée.');
  }
  const chainIdVu = Number(reseauRpc.chainId);
  if (chainIdVu !== CHAIN_ID_ATTENDU) {
    echec(`${RPC} annonce chainId ${chainIdVu}, attendu ${CHAIN_ID_ATTENDU}.`,
      'Ce n’est pas Coinbosa Chain : mesurer son temps de bloc ne prouverait rien sur le nôtre.',
      'Corriger RPC= (la répétition POBS écoute sur 18545, la production sur 8545),',
      'ou vérifier quel nœud occupe ce port.');
  }
  // Le networkId ne conditionne rien : la répétition POBS tourne volontairement
  // avec le même chainId 26262 et un networkId 262620, et ce script doit y
  // passer. On l'affiche pour que la trace dise sur quel réseau on a mesuré.
  let networkId = 'non exposé (API net désactivée)';
  try { networkId = String(await provider.send('net_version', [])); } catch { /* information, pas verdict */ }
  console.log(`  nœud                : ${RPC}`);
  console.log(`  chainId             : ${chainIdVu} (attendu ${CHAIN_ID_ATTENDU}) — networkId ${networkId}`);
  // Le chainId ne distingue pas la répétition POBS de la production. L'identité
  // complète, c'est le hash du bloc 0 : voir check-genesis-hash.js.

  // --- 3. Mesure en direct : on n'échantillonne que ce qu'on a vu naître ----
  const depart = await provider.getBlockNumber();
  const cible = depart + SAMPLES + 1;
  const tDepart = Date.now();

  // Les deux bornes sont sur l'HORLOGE MURALE, jamais sur un accumulateur dérivé
  // de la configuration : une borne d'arrêt calculée à partir d'une valeur lue
  // dans un fichier disparaît avec elle, et la boucle d'attente devient infinie.
  const DELAI_IMMOBILE_MS = Math.max(60000, Math.round(EXPECTED * 8 * 1000));
  const DELAI_TOTAL_MS = Math.max(180000, Math.round(EXPECTED * (SAMPLES + 1) * 3 * 1000));
  const PAS_MS = Math.max(500, Math.round(EXPECTED * 500));

  console.log(`  observation         : ${SAMPLES + 1} blocs à produire à partir du bloc ${depart}`);

  let tete = depart;
  let tDernierMouvement = tDepart;
  while (tete < cible) {
    await sleep(PAS_MS);
    let vu;
    try {
      vu = await provider.getBlockNumber();
    } catch (e) {
      echec(`le nœud a cessé de répondre pendant la mesure (${e.message}).`,
        'Vérifier qu’il n’a pas été arrêté ou rechargé ; consulter son journal.');
    }
    if (vu > tete) {
      const avance = vu - tete;
      tete = vu;
      tDernierMouvement = Date.now();
      if ((tete - depart) % 4 === 0 || avance > 1) console.log(`  …${tete - depart} bloc(s) produit(s)`);
    } else if (Date.now() - tDernierMouvement > DELAI_IMMOBILE_MS) {
      // Le cas qui déclarait « conforme » une chaîne morte : le nœud répond, la
      // hauteur ne bouge plus, et relire l'histoire suffisait à passer au vert.
      echec(`la chaîne ne produit plus : tête immobile au bloc ${tete} depuis ${Math.round((Date.now() - tDernierMouvement) / 1000)} s.`,
        `Attendu : un bloc toutes les ${EXPECTED} s.`,
        'Sur le nœud : le validateur scelle-t-il ? (`--mine`, `--unlock` accepté, clé présente dans le jeu),',
        'le journal boucle-t-il sur « Failed to prepare header for sealing » (contrat système),',
        'et le quorum Parlia est-il atteint (⌊N/2⌋+1 scelleurs distincts EN LIGNE) ?');
    }
    if (Date.now() - tDepart > DELAI_TOTAL_MS) {
      echec(`${tete - depart} bloc(s) produit(s) en ${Math.round((Date.now() - tDepart) / 1000)} s, ${SAMPLES + 1} attendus.`,
        `À ${EXPECTED} s par bloc, cela aurait dû prendre environ ${Math.round(EXPECTED * (SAMPLES + 1))} s.`,
        'La chaîne produit, mais beaucoup trop lentement : validateurs manquants (chaque tour manqué',
        'coûte un backoff), machine saturée, ou période du client différente de celle annoncée.');
    }
  }
  const tFin = Date.now();

  // On ne lit QUE les blocs nés pendant l'observation. Le bloc `depart`, lui,
  // existait déjà : son horodatage n'engage personne et n'entre pas dans la mesure.
  const times = [];
  for (let n = depart + 1; n <= cible; n++) {
    const bloc = await provider.getBlock(n);
    if (!bloc || !Number.isFinite(Number(bloc.timestamp))) {
      echec(`le nœud annonce la hauteur ${tete} mais ne sert pas le bloc ${n}.`,
        'Une hauteur sans les blocs correspondants n’est pas une chaîne vérifiable :',
        'nœud en cours d’élagage, base corrompue, ou réponse RPC tronquée. Consulter son journal.');
    }
    times.push(Number(bloc.timestamp));
  }

  const deltas = [];
  for (let i = 0; i < times.length - 1; i++) deltas.push(times[i + 1] - times[i]);

  // Parlia impose des horodatages strictement croissants. Un intervalle nul ou
  // négatif n'est pas une mesure : c'est un nœud qui raconte n'importe quoi (ou
  // une période sous la seconde, invisible à cette résolution).
  if (deltas.some((d) => d <= 0)) {
    echec(`horodatages non strictement croissants : ${deltas.join(' s, ')} s.`,
      'Un intervalle nul ou négatif est impossible en Parlia. Soit le nœud sert des blocs',
      'incohérents (base corrompue, réorganisation en cours), soit la période est sous la',
      'seconde et n’est pas mesurable ainsi. Ne rien conclure de ces chiffres.');
  }

  const periode = Math.min(...deltas);
  const moyenne = deltas.reduce((a, b) => a + b, 0) / deltas.length;
  const spanChaine = times[times.length - 1] - times[0];
  const spanReel = (tFin - tDepart) / 1000;

  console.log(`  intervalles mesurés : ${deltas.join(' s, ')} s`);
  console.log(`  plancher (minimum)  : ${periode} s   [moyenne ${moyenne.toFixed(2)} s — indicative, ne décide rien]`);
  console.log(`  attendu             : ${EXPECTED} s (± ${TOLERANCE})`);
  console.log(`  temps déclaré/réel  : ${spanChaine} s de chaîne pour ${spanReel.toFixed(1)} s observées`);

  // Un nœud qui IMPORTE une histoire (synchronisation, rejeu) voit sa hauteur
  // monter sans rien produire, et ses blocs déclarent alors plus de temps qu'il
  // n'en a réellement passé. Le temps de chaîne ne peut pas dépasser le temps
  // réel entre deux blocs qu'on a vus naître. Ce contrôle-ci est insensible au
  // décalage d'horloge entre cette machine et le nœud : il compare deux durées,
  // pas deux dates — raison pour laquelle il remplace un test de fraîcheur de
  // la tête, qui lui aurait crié à tort sur la moindre horloge mal réglée.
  const MARGE_S = EXPECTED + 5;
  if (spanChaine > spanReel + MARGE_S) {
    echec(`les blocs déclarent ${spanChaine} s alors que ${spanReel.toFixed(1)} s se sont écoulées.`,
      'Ce nœud n’a pas PRODUIT ces blocs, il les a importés : hauteur qui monte pendant une',
      'synchronisation, ou histoire rejouée. Le temps de bloc mesuré serait celui d’une autre machine.',
      'Attendre la fin de la synchronisation (eth_syncing), puis relancer ce contrôle.');
  }

  if (Math.abs(periode - EXPECTED) >= TOLERANCE) {
    if (periode < EXPECTED) {
      echec(`intervalle plancher de ${periode} s au lieu de ${EXPECTED} s.`,
        'Parlia refuse tout bloc plus proche de son parent que la période du client',
        '(consensus/parlia/ramanujanfork.go:45) : mesurer plus court PROUVE que le client tourne',
        'à une autre période — typiquement le geth d’amont de BNB Chain, qui produit à 3 s.',
        'Recompiler depuis ce dépôt (`make geth`), vérifier consensus/parlia/parlia.go,',
        'puis redémarrer le nœud sur le binaire produit — le binaire en place n’est pas celui-ci.');
    }
    echec(`aucun intervalle à ${EXPECTED} s sur ${SAMPLES} mesures ; le plus court vaut ${periode} s.`,
      'La période ne peut qu’être dépassée, jamais raccourcie : ne jamais voir la valeur attendue',
      'signifie que tous les blocs ont été scellés en retard, ou que la période du client est plus longue.',
      'Vérifier combien de validateurs sont EN LIGNE (chaque tour manqué ajoute un backoff),',
      'la charge de la machine et l’horloge du nœud, puis relancer. Si l’écart persiste,',
      'c’est le binaire qu’il faut vérifier (consensus/parlia/parlia.go, `make geth`).');
  }

  console.log(`\n  temps de bloc conforme : ${SAMPLES} intervalles observés en direct, plancher ${periode} s`);
})().catch((e) => {
  // Fail-closed : une erreur imprévue n'est pas un succès. On ne conclut rien.
  console.error(`\nERREUR : ${e.message}`);
  console.error(`Le contrôle n’a pas pu aboutir sur ${RPC} — rien n’est prouvé sur le temps de bloc.`);
  process.exit(1);
});
