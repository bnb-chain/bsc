// Réconcilie la garde de l'offre : QUI détient les 700 000 000 BOSA, et sous quelle forme.
//
//   RPC=https://explorer.coinbosa.com/rpc node scripts/check-custody.js
//
// LECTURE SEULE. Aucune transaction n'est émise, aucune clé n'est lue, rien n'est écrit.
//
// Pourquoi ce contrôle en plus de check-supply.js
// -----------------------------------------------
// check-supply.js compare les soldes du FICHIER genesis à la chaîne. L'état du bloc 0
// ayant été purgé (nœud --gcmode full), il retombe sur le bloc courant : dès qu'un poste
// a bougé d'un wei, il annonce un ÉCHEC d'offre alors que l'offre est intacte — elle a
// seulement changé de main. Ce script raisonne autrement : il additionne TOUS les
// détenteurs connus (13 postes + gouverneur + contrat système) et exige le total exact.
//
// L'argument qui rend la somme suffisante :
//   1. l'empreinte du bloc 0 est figée et publiée (check-genesis-hash.js) : l'allocation
//      initiale est donc prouvée, et elle vaut exactement 700 000 000 BOSA ;
//   2. le moteur de consensus ne crée pas de monnaie et ne brûle rien (baseFee nulle) :
//      l'offre totale est donc constante ;
//   3. si un sous-ensemble de comptes totalise l'offre entière, TOUS les autres comptes
//      de la chaîne sont à zéro. Il n'y a pas de détenteur caché.
// C'est ce raisonnement — et non une énumération, impossible en JSON-RPC — qui ferme
// la question.
//
// CE QUE CE SCRIPT REFUSE DE FAIRE
// --------------------------------
// Le point 3 ci-dessus n'est un argument que si trois prémisses tiennent, et un audit a
// montré qu'aucune n'était vérifiée :
//   a. on parle bien à un nœud qui SUIT la chaîne publiée. Une réplique fraîchement
//      `geth init` avec le même fichier genesis rend le même chainId et le même bloc 0 —
//      ces deux valeurs ne prouvent donc RIEN sur la fraîcheur. Pointé sur elle, ce script
//      concluait « offre intégralement localisée » sur un nœud resté au bloc 0 ;
//   b. les adresses additionnées sont DISTINCTES. Deux postes pointant sur la même adresse
//      font compter son solde deux fois : la somme retombe juste au wei et un détenteur
//      réel reste hors du contrôle ;
//   c. une réponse vide est une réponse. Un nœud sans index de logs rend `[]` : l'inventaire
//      des actions du gouverneur passait alors de « impossible » à « rien à signaler ».
// La règle appliquée partout ci-dessous : quand un contrôle ne PEUT PAS conclure, il
// échoue en le disant. Il ne se tait jamais. Un contrôle qui se tait fait cesser la
// vigilance, ce qui est pire que pas de contrôle du tout.
const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

// RPC est EXIGÉE, sans repli. Ce que le repli `http://127.0.0.1:8545` attrapait : c'est
// exactement le port ouvert par scripts/start-node.sh. Un oubli de la variable suffisait
// donc à auditer sa propre machine en croyant auditer la production, avec une sortie
// indiscernable — à la ligne du numéro de bloc près. Une adresse locale reste parfaitement
// légitime (auditer depuis son propre nœud synchronisé est même la bonne méthode) : ce
// qu'on interdit, c'est le nœud IMPLICITE, celui que personne n'a choisi.
const RPC = process.env.RPC;
if (!RPC) {
  console.error('\n  ÉCHEC : RPC non définie — refus de conclure sur un nœud implicite.');
  console.error('           → relancer en désignant le point d\'accès, par exemple :');
  console.error('             RPC=https://explorer.coinbosa.com/rpc node scripts/check-custody.js\n');
  process.exit(1);
}

const ROOT = path.join(__dirname, '..');
const CONFIG = JSON.parse(fs.readFileSync(path.join(ROOT, 'coinbosa.config.json'), 'utf8'));
const ADDRS = JSON.parse(fs.readFileSync(path.join(ROOT, 'genesis', 'distribution-addresses.json'), 'utf8'));
const REF = JSON.parse(fs.readFileSync(path.join(ROOT, 'genesis', 'genesis-reference.json'), 'utf8'));
const GENESIS_FILE = path.join(ROOT, 'genesis', 'genesis-coinbosa.json');

const VALSET = '0x0000000000000000000000000000000000001000';
const ATTENDU = BigInt(CONFIG.nativeCoin.totalSupply) * 10n ** 18n;
const PLAGE = 5000;                       // --rangelimit du nœud : 5 000 blocs par requête
const INTERVALLE = Number(CONFIG.network.blockIntervalMs || 5000) / 1000;  // 5 s/bloc
const RETARD_MAX = 60 * INTERVALLE;       // 60 blocs : au-delà, le nœud ne suit plus la chaîne
const ATTENTE_MAX = 12 * INTERVALLE;      // vivacité : on laisse 12 blocs pour voir la tête bouger
const TOPIC = {
  '0xc45a2277fda002f812af4dda0deb46fbfa0eb91b3175b73a56e878af02bdf793': 'ValidatorSetUpdated',
  '0x1ed371ca1748e85e2d9554206ef61b0e69b21d41f30b4d4987d7b006fb4801cc': 'ValidatorDeposit',
  '0xc20af58baaae9e9347897f7e714ad24bf44b9bc415c3ad4bc843cc5f06e0c82a': 'ValidatorClaimed',
  '0x167beb24b4e0b809e5ad14deac40057e2206ebff29b4f1240a7d26bf69aaebe3': 'SurplusSwept',
};

const bosa = (w) => Number(ethers.formatEther(w)).toLocaleString('fr-FR', { maximumFractionDigits: 18 });
const bas = (a) => String(a).toLowerCase();

let echecs = 0;
// echec() : le constat est établi, mais le reste de l'inventaire garde de la valeur pour
// celui qui devra agir — on continue et on retient le verdict jusqu'à la fin.
const echec = (m, quoiFaire) => {
  echecs++;
  console.error('  ÉCHEC : ' + m);
  if (quoiFaire) console.error('          → ' + quoiFaire);
};
// fatal() : la suite du script imprimerait des lignes rassurantes qui ne voudraient plus
// rien dire (mauvaise chaîne, nœud figé…). On s'arrête AVANT de rassurer à tort.
const fatal = (m, quoiFaire) => {
  console.error('\n  ÉCHEC : ' + m);
  if (quoiFaire) console.error('          → ' + quoiFaire);
  console.error(`\n  Contrôle INTERROMPU sur ${RPC} — aucune conclusion sur la garde n'est publiable.\n`);
  process.exit(1);
};

// Le validateur de genèse est inscrit dans l'extraData du bloc 0 : 32 octets de « vanity »,
// 1 octet de compte, puis N × (adresse 20 o + clé BLS 48 o), puis 65 octets de sceau
// (format Luban). Le relire ici donne une SECONDE source, indépendante de l'appel de
// contrat : un point d'accès qui ment sur INITIAL_VALIDATOR() doit aussi réécrire l'en-tête
// du bloc 0 — ce que la comparaison de hash rend impossible.
const validateurDeExtraData = (hex) => {
  const h = bas(hex).replace(/^0x/, '');
  if (h.length < 132) return null;
  const n = parseInt(h.slice(64, 66), 16);
  if (!Number.isInteger(n) || n < 1) return null;
  if (h.length !== (32 + 1 + n * 68 + 65) * 2) return null;   // forme inattendue : on ne devine pas
  return ethers.getAddress('0x' + h.slice(66, 106));
};

(async () => {
  const p = new ethers.JsonRpcProvider(RPC, undefined, { staticNetwork: true });
  const reseau = await p.getNetwork();
  if (reseau.chainId !== BigInt(CONFIG.network.chainId)) {
    fatal(`chainId ${reseau.chainId}, attendu ${CONFIG.network.chainId} sur ${RPC}.`,
      'ce point d\'accès sert un AUTRE réseau : corriger RPC.');
  }

  // --- 0. le nœud interrogé suit-il la chaîne ? ---------------------------------------
  // Ce que ce bloc attrape : l'audit d'une réplique morte pris pour un audit de production.
  // chainId et empreinte du bloc 0 sont IDENTIQUES sur toute copie du même fichier genesis ;
  // seules la hauteur de la tête, sa fraîcheur et sa progression distinguent un nœud qui
  // suit la chaîne d'un nœud qui l'a seulement initialisée.
  //
  // eth_syncing est un signal d'appoint : certaines passerelles RPC ne l'implémentent pas.
  // Son absence n'autorise rien — la fraîcheur et la progression, elles, restent exigées.
  let syncNote = null;
  try {
    const sync = await p.send('eth_syncing', []);
    if (sync !== false) {
      fatal(`le nœud ${RPC} se déclare EN COURS DE SYNCHRONISATION (${JSON.stringify(sync)}).`,
        'attendre la fin de la synchronisation, ou viser un nœud à jour : ses soldes sont incomplets.');
    }
  } catch (e) {
    syncNote = `eth_syncing indisponible sur ce point d'accès (${e.shortMessage || e.message}) — fraîcheur et progression restent exigées ci-dessous`;
  }

  const tete = await p.getBlockNumber();
  if (tete === 0) {
    fatal('tête au bloc 0 : ce nœud n\'a jamais produit ni reçu un seul bloc.',
      'c\'est une réplique fraîchement `geth init` — même genesis, donc même chainId et même bloc 0 que la production, mais AUCUN état courant. Viser un nœud synchronisé.');
  }
  const bloc = await p.send('eth_getBlockByNumber', ['0x' + tete.toString(16), false]);
  if (!bloc || bloc.timestamp === undefined) {
    fatal(`le nœud ne renvoie pas l'en-tête du bloc ${tete} qu'il annonce pourtant comme sa tête.`,
      'point d\'accès incohérent ou tronqué : ne pas s\'en servir pour un audit.');
  }
  const tsTete = parseInt(bloc.timestamp, 16);
  const retard = Math.floor(Date.now() / 1000) - tsTete;
  if (retard > RETARD_MAX) {
    fatal(`tête vieille de ${retard} s (bloc ${tete}, seuil ${RETARD_MAX} s = ${RETARD_MAX / INTERVALLE} blocs) : le nœud ne suit pas la chaîne.`,
      'nœud arrêté, en retard ou déconnecté — ses soldes sont ceux d\'un passé arbitraire. Le resynchroniser, ou viser un nœud à jour. (Vérifier aussi l\'horloge de cette machine : ce contrôle la compare à l\'horodatage du bloc.)');
  }
  if (retard < -RETARD_MAX) {
    fatal(`tête horodatée ${-retard} s dans le FUTUR (bloc ${tete}) : incohérence d'horloge.`,
      'soit l\'horloge de cette machine retarde, soit le point d\'accès fabrique ses en-têtes. Corriger l\'horloge et relancer avant de conclure quoi que ce soit.');
  }

  console.log(`\n  Garde de l'offre — Coinbosa Chain (chainId ${reseau.chainId})`);
  console.log(`  point d'accès interrogé : ${RPC}`);
  console.log(`  observé au bloc ${tete}, ${new Date(tsTete * 1000).toISOString()} (retard ${retard} s)`);
  if (syncNote) console.log(`  note : ${syncNote}`);
  console.log('  ' + '='.repeat(96));

  // --- 1. l'identité de la chaîne : l'allocation initiale est-elle celle qui est publiée ---
  // Ce que ce bloc attrape : une absence prise pour un accord. La comparaison du stateRoot
  // était conditionnée à la PRÉSENCE des deux valeurs : un nœud, un proxy ou un client qui
  // omet le champ faisait tomber la condition à faux, et le script imprimait quand même
  // « allocation initiale prouvée ». Ici, un champ manquant est un ÉCHEC, jamais un accord.
  const b0 = await p.send('eth_getBlockByNumber', ['0x0', false]);
  let identiteOk = true;
  const compare = (nom, attendu, obtenu) => {
    if (!attendu) {
      identiteOk = false;
      return echec(`genesis-reference.json incomplet : ${nom} absent — comparaison impossible.`,
        'restaurer la référence figée à la publication du genesis ; sans elle, rien ne prouve que cette chaîne est celle qui a été publiée.');
    }
    if (!obtenu) {
      identiteOk = false;
      return echec(`le nœud ne fournit pas ${nom} du bloc 0 — empreinte invérifiable.`,
        'ce point d\'accès tronque les en-têtes : interroger un nœud complet.');
    }
    if (bas(attendu) !== bas(obtenu)) {
      identiteOk = false;
      return echec(`${nom} du bloc 0 : ${obtenu}, référence ${attendu}.`,
        'la chaîne branchée N\'EST PAS celle qui a été publiée (genesis modifié, allocation ajoutée, ou mauvais réseau).');
    }
  };
  if (!b0) {
    identiteOk = false;
    echec('le nœud ne renvoie pas le bloc 0 — l\'allocation initiale ne peut pas être prouvée.',
      'interroger un nœud disposant de l\'historique complet.');
  } else {
    compare('hash', REF.hash, b0.hash);
    compare('stateRoot', REF.stateRoot, b0.stateRoot);
    compare('extraData', REF.extraData, b0.extraData);
  }
  if (identiteOk) console.log('\n  [1] bloc 0 conforme à genesis-reference.json (hash + stateRoot + extraData)');
  else console.log('\n  [1] bloc 0 NON conforme — voir les ÉCHECS ci-dessus ; « allocation initiale prouvée » ne peut PAS être écrit.');

  // --- 2. les documents publiés se tiennent-ils entre eux ? ---------------------------
  // Ce que ce bloc attrape : la substitution d'adresse. L'argument « si la somme fait
  // l'offre entière, tous les autres comptes sont à zéro » suppose que les adresses
  // additionnées soient DISTINCTES et que la liste soit COMPLÈTE. Sans ces deux gardes,
  // remplacer un poste par l'adresse d'un autre fait compter deux fois le même solde : le
  // total retombe juste au wei, et un détenteur réel de 14 000 000 BOSA sort du contrôle
  // sans que rien ne proteste. Deux postes portent exactement la même part sur la chaîne
  // réelle : la substitution est donc exacte sans le moindre calcul.
  //
  // Portée honnête de l'ancrage : genesis-coinbosa.json est le fichier versionné dont
  // l'empreinte du bloc 0 a été dérivée. On ne peut pas recalculer son stateRoot ici (il
  // faudrait reconstruire le trie, et l'état du bloc 0 est purgé sur les nœuds --gcmode
  // full). Exiger que les TROIS documents publiés concordent — parts, liste d'adresses,
  // allocation du genesis — ne remplace pas cette preuve, mais interdit d'éditer la seule
  // liste d'adresses après coup, ce qui était le chemin d'attaque effectivement reproduit.
  console.log('\n  [2] cohérence des documents publiés (config ↔ adresses ↔ genesis)');
  const echecsAvant2 = echecs;   // sert à ne PAS écrire « adresses distinctes » quand elles ne le sont pas
  // Un fichier d'ancrage absent ou illisible est un ÉCHEC énoncé, pas un contrôle qu'on
  // saute : sans lui, plus rien n'empêche d'éditer la seule liste d'adresses après coup.
  let GEN = null;
  try {
    GEN = JSON.parse(fs.readFileSync(GENESIS_FILE, 'utf8'));
  } catch (e) {
    echec(`genesis/genesis-coinbosa.json illisible (${e.message}) : la liste des postes ne peut pas être ancrée au bloc 0.`,
      'récupérer le genesis de production versionné dans le dépôt (il n\'est pas ignoré par git) et relancer depuis une copie complète du dépôt.');
  }
  if (GEN && Number(GEN.config && GEN.config.chainId) !== Number(CONFIG.network.chainId)) {
    echec(`genesis-coinbosa.json déclare chainId ${GEN.config && GEN.config.chainId}, config ${CONFIG.network.chainId}.`,
      'ce fichier genesis n\'est pas celui de ce réseau : ne pas l\'utiliser comme ancrage.');
  }
  if (GEN && (!GEN.extraData || bas(GEN.extraData) !== bas(REF.extraData))) {
    echec('l\'extraData de genesis-coinbosa.json diffère de genesis-reference.json.',
      'le fichier genesis local n\'est pas celui dont l\'empreinte a été publiée : régénérer ou récupérer le bon fichier avant de conclure.');
  }

  const postesConfig = Object.keys(CONFIG.distribution).filter((k) => !k.startsWith('$'));
  const sommeParts = postesConfig.reduce((s, k) => s + Number(CONFIG.distribution[k]), 0);
  if (sommeParts !== 100) {
    echec(`les parts de coinbosa.config.json totalisent ${sommeParts} %, pas 100 %.`,
      'une répartition qui ne fait pas 100 % laisse une fraction de l\'offre non attribuée : corriger la config.');
  }
  const projet = BigInt(CONFIG.projectAllocation.amount) * 10n ** 18n;
  const reserve = BigInt(CONFIG.migration.reserve) * 10n ** 18n;
  if (projet + reserve !== ATTENDU) {
    echec(`allocation projet (${bosa(projet)}) + réserve de migration (${bosa(reserve)}) ≠ offre fixée (${bosa(ATTENDU)}).`,
      'la somme des enveloppes déclarées doit épuiser l\'offre, sinon une part reste sans détenteur nommé.');
  }

  // Liste des postes à additionner, et attendu de chacun au bloc 0.
  const postes = [];                                  // { nom, adr, attendu }
  const vus = new Map();                              // adresse minuscule -> premier porteur
  let doublons = 0;
  const unicite = (nom, adr) => {
    const k = bas(adr);
    if (vus.has(k)) {
      doublons++;
      echec(`adresse dupliquée : « ${nom} » et « ${vus.get(k)} » portent tous deux ${ethers.getAddress(adr)}.`,
        'la somme compterait deux fois le même solde : l\'argument de couverture (« tout le reste est à zéro ») devient NUL, et un détenteur réel échappe au contrôle. Corriger genesis/distribution-addresses.json.');
      return false;
    }
    vus.set(k, nom);
    return true;
  };
  for (const [poste, adr] of Object.entries(ADDRS)) {
    if (poste.startsWith('$')) continue;
    if (poste === '__migration__') {
      // La réserve vaut 0 : l'adresse nulle est alors le seul état cohérent, et il n'y a
      // rien à additionner. Toute autre combinaison est une incohérence, pas un détail.
      if (reserve === 0n) {
        if (adr !== ethers.ZeroAddress) {
          echec(`réserve de migration nulle mais __migration__ porte l'adresse ${adr}.`,
            'soit la réserve n\'est pas nulle et la config ment, soit cette adresse n\'a rien à faire là : trancher avant de publier.');
        }
        continue;
      }
      if (adr === ethers.ZeroAddress) {
        echec(`réserve de migration de ${bosa(reserve)} BOSA mais __migration__ vaut l'adresse nulle.`,
          'cette part de l\'offre n\'a aucun détenteur nommé : renseigner l\'adresse de réserve.');
        continue;
      }
      if (unicite(poste, adr)) postes.push({ nom: poste, adr, attendu: reserve });
      continue;
    }
    if (CONFIG.distribution[poste] === undefined) {
      echec(`« ${poste} » figure dans distribution-addresses.json mais n'a aucune part dans coinbosa.config.json.`,
        'poste inconnu de la répartition : soit il est de trop, soit sa part manque.');
      continue;
    }
    if (adr === ethers.ZeroAddress) {
      // Un `continue` muet ici sortait le poste de la somme ET de la liste : sa part
      // devenait invisible, alors qu'elle représente jusqu'à 20 % de l'offre.
      echec(`poste « ${poste} » (${CONFIG.distribution[poste]} %) non attribué : adresse nulle.`,
        'renseigner une vraie adresse de détention dans genesis/distribution-addresses.json ; tant qu\'elle est nulle, cette part de l\'offre n\'est rattachée à personne.');
      continue;
    }
    if (unicite(poste, adr)) {
      postes.push({ nom: poste, adr, attendu: projet * BigInt(CONFIG.distribution[poste]) / 100n });
    }
  }
  const manquants = postesConfig.filter((k) => !postes.some((x) => x.nom === k));
  if (manquants.length) {
    echec(`postes de la répartition absents de l'inventaire : ${manquants.join(', ')}.`,
      'chaque poste doit être additionné ; un poste manquant fausse la somme et laisse sa part hors du contrôle.');
  }

  // Ancrage : l'allocation du bloc 0 doit être EXACTEMENT cette liste, aux montants près.
  // (Sauté seulement si le fichier est illisible — cas déjà signalé comme ÉCHEC ci-dessus,
  // donc le verdict reste rouge : on n'ajoute pas treize échecs dérivés du même constat.)
  if (GEN) {
    const allocNonNulle = Object.entries(GEN.alloc || {})
      .filter(([, v]) => v && v.balance && BigInt(v.balance) > 0n)
      .map(([a, v]) => [bas(a.startsWith('0x') ? a : '0x' + a), BigInt(v.balance)]);
    const allocMap = new Map(allocNonNulle);
    let sommeAlloc = 0n;
    for (const [, v] of allocNonNulle) sommeAlloc += v;
    for (const { nom, adr, attendu } of postes) {
      const dansGenesis = allocMap.get(bas(adr));
      if (dansGenesis === undefined) {
        echec(`« ${nom} » (${ethers.getAddress(adr)}) ne reçoit RIEN dans l'allocation du bloc 0.`,
          'cette adresse a été ajoutée à la liste après le gel du genesis : elle n\'est pas ancrée au bloc 0 dont l\'empreinte est publiée.');
      } else if (dansGenesis !== attendu) {
        echec(`« ${nom} » reçoit ${dansGenesis} wei au bloc 0, mais sa part vaut ${attendu} wei.`,
          'la liste d\'adresses et le genesis publié se contredisent : ne pas conclure avant d\'avoir tranché lequel fait foi.');
      }
      allocMap.delete(bas(adr));
    }
    for (const [a, v] of allocMap) {
      echec(`compte non listé doté au bloc 0 : ${a} reçoit ${bosa(v)} BOSA.`,
        'ce détenteur est absent de distribution-addresses.json : il ne serait pas additionné, et la somme resterait juste par compensation. L\'ajouter ou expliquer sa présence.');
    }
    if (sommeAlloc !== ATTENDU) {
      echec(`l'allocation du bloc 0 totalise ${sommeAlloc} wei, offre fixée ${ATTENDU} wei.`,
        'le fichier genesis local ne distribue pas l\'offre annoncée : il n\'est pas utilisable comme ancrage.');
    }
  }
  // Le résumé ne peut affirmer « adresses distinctes, montants conformes » que si aucun
  // ÉCHEC n'a été relevé dans cette section : sinon la ligne contredirait ce qui précède.
  if (echecs === echecsAvant2) {
    console.log(`      ${postes.length} poste(s) attribué(s), adresses distinctes, montants conformes à l'allocation du bloc 0`);
  } else {
    console.log(`      ${postes.length} poste(s) retenu(s) — documents NON concordants ou ancrage impossible : la liste additionnée en [4] n'est PAS démontrée complète (voir ÉCHECS ci-dessus)`);
  }
  console.log(`      parts : ${sommeParts} % — enveloppes : ${bosa(projet)} projet + ${bosa(reserve)} migration = ${bosa(ATTENDU)} BOSA`);

  // --- 3. le gouverneur et le jeu de validateurs -------------------------------------
  // Ce que ce bloc attrape deux choses :
  //  a) une valeur FABRIQUÉE par le repli défensif du contrat. getValidators() renvoie
  //     [INITIAL_VALIDATOR] quand `validators` est VIDE (CoinbosaValidatorSet.sol) — pour
  //     que la chaîne ne puisse pas se suicider. La ligne affichée est donc identique sur
  //     la production et sur un nœud dont l'état est vierge : seuls alreadyInit() et
  //     numOfValidators() tranchent.
  //  b) un point d'accès qui choisit lui-même le gouverneur qu'il montre. GOVERNOR() et
  //     l'empreinte du bloc 0 viennent du MÊME RPC : un proxy qui sert un bloc 0
  //     authentique et ment sur l'appel de contrat passait les deux contrôles. On confronte
  //     donc à genesis-reference.json — déjà chargé, jamais lu jusqu'ici — et, pour le
  //     validateur, à l'extraData du bloc 0, qui est une source indépendante.
  //
  // Toutes les lectures sont épinglées au bloc `tete` : sans blockTag elles partent à
  // `latest`, qui a nécessairement dérivé de plusieurs blocs pendant la centaine d'appels
  // séquentiels de ce script — on comparerait alors des grandeurs prises à deux états.
  const c = new ethers.Contract(VALSET, [
    'function GOVERNOR() view returns (address)',
    'function INITIAL_VALIDATOR() view returns (address)',
    'function getValidators() view returns (address[])',
    'function totalInComing() view returns (uint256)',
    'function alreadyInit() view returns (bool)',
    'function numOfValidators() view returns (uint256)',
  ], p);
  const gouverneur = await c.GOVERNOR({ blockTag: tete });
  const validateur = await c.INITIAL_VALIDATOR({ blockTag: tete });
  const valideurs = await c.getValidators({ blockTag: tete });
  const initFait = await c.alreadyInit({ blockTag: tete });
  const nbValideurs = await c.numOfValidators({ blockTag: tete });

  console.log(`\n  [3] gouverneur (constante du bytecode) : ${gouverneur}`);
  console.log(`      validateur de genèse                : ${validateur}`);
  console.log(`      jeu de validateurs courant          : ${valideurs.length} — ${valideurs.join(', ')}`);
  console.log(`      état du contrat système             : alreadyInit=${initFait}, numOfValidators=${nbValideurs}`);

  if (!initFait) {
    echec('CoinbosaValidatorSet n\'est PAS initialisé (alreadyInit = false) : le jeu de validateurs affiché est le repli défensif du contrat, pas son état.',
      'sur ce nœud, le bloc 1 n\'a jamais été exécuté — c\'est une réplique vierge, pas la chaîne. Viser un nœud synchronisé.');
  } else if (BigInt(valideurs.length) !== nbValideurs) {
    echec(`getValidators() rend ${valideurs.length} adresse(s) mais numOfValidators() en compte ${nbValideurs} : repli défensif actif ou état incohérent.`,
      'ne pas lire la ligne ci-dessus comme l\'état réel du jeu de validateurs.');
  } else if (!valideurs.some((v) => bas(v) === bas(validateur))) {
    echec('le validateur de genèse ne figure pas dans le jeu courant, ce que le contrat interdit (garde anti-arrêt de updateValidatorSet).',
      'état impossible on-chain : le point d\'accès fabrique sa réponse, ou la chaîne interrogée n\'est pas celle-ci.');
  }

  if (!REF.gouverneur || !REF.validateur) {
    echec('genesis-reference.json incomplet : gouverneur/validateur absents — la comparaison est impossible.',
      'sans référence publiée, l\'adresse du gouverneur est celle que le point d\'accès veut bien montrer. Restaurer la référence.');
  } else {
    if (bas(gouverneur) !== bas(REF.gouverneur)) {
      echec(`gouverneur lu ${gouverneur}, référence publiée ${REF.gouverneur}.`,
        'ce point d\'accès ne sert pas le contrat système publié : suspendre l\'audit et vérifier le RPC avant toute autre conclusion.');
    }
    if (bas(validateur) !== bas(REF.validateur)) {
      echec(`validateur de genèse lu ${validateur}, référence publiée ${REF.validateur}.`,
        'même cause que ci-dessus : la réponse de contrat et la référence figée divergent.');
    }
  }
  // Recoupement indépendant de l'appel de contrat : l'extraData du bloc 0, dont le hash
  // est déjà comparé au [1]. Une forme d'en-tête inattendue est signalée, pas devinée.
  const vExtra = b0 && b0.extraData ? validateurDeExtraData(b0.extraData) : null;
  if (!vExtra) {
    echec('extraData du bloc 0 illisible ou de forme inattendue : le recoupement du validateur de genèse est impossible.',
      'vérifier que le nœud sert bien l\'en-tête complet du bloc 0 (format Parlia/Luban attendu).');
  } else if (bas(vExtra) !== bas(validateur)) {
    echec(`validateur inscrit dans l'extraData du bloc 0 : ${vExtra}, mais INITIAL_VALIDATOR() rend ${validateur}.`,
      'l\'en-tête du bloc 0 et la réponse de contrat se contredisent : le point d\'accès fabrique l\'une des deux.');
  }
  if (bas(gouverneur) === bas(validateur)) {
    echec('gouverneur = validateur : la clé qui scelle les blocs détiendrait aussi la gouvernance.',
      'un serveur de scellage compromis emporterait alors le consensus (invariant du dépôt : gouverneur ≠ clé de scellage).');
  }

  // --- 4. chaque détenteur : solde, code, nombre de transactions émises ---------------
  console.log('\n  [4] détenteurs');
  console.log('  ' + '-'.repeat(96));
  console.log('      poste                        part      solde (BOSA)          code   nonce');
  let total = 0n;
  let sansCode = 0;
  const ligne = (nom, adr, part, solde, code, nonce) => {
    console.log(`      ${nom.padEnd(27)} ${String(part).padStart(6)}  ${bosa(solde).padStart(20)}  ${String(code).padStart(5)}o  ${String(nonce).padStart(6)}   ${adr}`);
  };
  for (const { nom, adr } of postes) {
    const solde = await p.getBalance(adr, tete);
    const code = (await p.getCode(adr, tete)).length / 2 - 1;
    const nonce = await p.getTransactionCount(adr, tete);
    total += solde;
    if (code === 0) sansCode++;
    ligne(nom, ethers.getAddress(adr), (CONFIG.distribution[nom] ?? '—') + ' %', solde, code, nonce);
  }
  // Le gouverneur et le contrat système entrent dans la somme : ils doivent donc entrer
  // dans le contrôle d'unicité au même titre que les postes, sinon un poste pointant sur
  // l'un des deux ferait compter son solde deux fois.
  unicite('GOUVERNEUR', gouverneur);
  const soldeGouv = await p.getBalance(gouverneur, tete);
  const codeGouv = (await p.getCode(gouverneur, tete)).length / 2 - 1;
  const nonceGouv = await p.getTransactionCount(gouverneur, tete);
  total += soldeGouv;
  ligne('GOUVERNEUR', ethers.getAddress(gouverneur), '—', soldeGouv, codeGouv, nonceGouv);

  unicite('contrat système 0x…1000', VALSET);
  const soldeValset = await p.getBalance(VALSET, tete);
  const dus = await c.totalInComing({ blockTag: tete });
  total += soldeValset;
  ligne('contrat système 0x…1000', VALSET, 'frais', soldeValset, (await p.getCode(VALSET, tete)).length / 2 - 1, '—');
  // Ce que ce contrôle attrape : un contrat système qui doit plus qu'il ne détient. Le
  // « surplus » était une simple ligne d'information, imprimée en NÉGATIF sans que rien
  // ne proteste. Or un solde inférieur aux sommes dues signifie que des claim() de
  // validateurs échoueront faute de fonds — c'est exactement l'anomalie de garde que ce
  // script existe pour détecter.
  if (soldeValset < dus) {
    echec(`contrat système insolvable : ${bosa(dus)} BOSA dus aux validateurs pour ${bosa(soldeValset)} BOSA détenus (manque ${bosa(dus - soldeValset)}).`,
      'les appels à claim() échoueront. Vérifier l\'historique de sweepSurplus() ci-dessous et l\'intégrité du point d\'accès.');
  } else if (soldeValset > dus) {
    console.log(`      (dont ${bosa(dus)} dus aux validateurs via claim(), ${bosa(soldeValset - dus)} de surplus)`);
  }

  // --- 5. la somme doit tomber juste au wei -------------------------------------------
  console.log('  ' + '-'.repeat(96));
  console.log(`      total détenu : ${total.toString()} wei  (${bosa(total)} BOSA)`);
  console.log(`      offre fixée  : ${ATTENDU.toString()} wei  (${bosa(ATTENDU)} BOSA)`);
  if (total !== ATTENDU) {
    echec(`écart de ${(total - ATTENDU).toString()} wei — il existe un détenteur non listé, ou une combustion.`
      + (doublons ? ` (${doublons} adresse(s) dupliquée(s) ont été écartées de la somme : c'est la signature d'une substitution d'adresse, voir ci-dessus)` : ''),
    'reprendre poste par poste : la couverture de l\'offre n\'est plus démontrée.');
  } else if (doublons > 0) {
    // Somme juste ET doublon détecté : c'est précisément la signature de la substitution
    // d'adresse. On refuse d'imprimer la phrase de couverture.
    console.log('      écart        : 0 wei, mais des adresses ont été comptées deux fois — ce zéro ne prouve RIEN (voir ÉCHECS).');
  } else {
    console.log('      écart        : 0 wei — aucun détenteur en dehors de cette liste');
  }

  // --- 6. nature de la garde : combien de postes sont de simples clés ------------------
  console.log('\n  [5] nature de la garde');
  console.log(`      postes sans code (clé simple, ni multi-signatures ni délai) : ${sansCode} / ${postes.length}`);
  console.log(`      gouverneur sans code                                        : ${codeGouv === 0 ? 'oui' : 'non'}`);
  console.log('      (un contrat multi-signatures porterait du code ; 0 octet = clé unique)');

  // --- 7. tout ce que le contrat système a jamais émis ---------------------------------
  // Ce que ce bloc attrape : une liste vide prise pour une conformité. Un nœud sans index
  // de logs, un proxy qui filtre ou une base élaguée répondent `[]` au lieu d'une erreur ;
  // la section passait alors d'« inventaire complet » à « rien à signaler » sans un mot —
  // alors que c'est la SEULE partie du script capable de révéler une action du gouverneur
  // (updateValidatorSet, sweepSurplus). Le script imprimait même sa propre contradiction :
  // « le gouverneur a agi -1 fois ».
  console.log(`\n  [6] historique complet des événements du contrat système (0 → ${tete})`);
  const evenements = [];
  for (let from = 0; from <= tete; from += PLAGE) {
    const to = Math.min(from + PLAGE - 1, tete);
    const logs = await p.send('eth_getLogs', [{
      fromBlock: '0x' + from.toString(16), toBlock: '0x' + to.toString(16), address: VALSET,
    }]);
    for (const l of logs) evenements.push(l);
  }
  for (const l of evenements) {
    console.log(`      bloc ${String(parseInt(l.blockNumber, 16)).padStart(8)}  ${(TOPIC[l.topics[0]] || l.topics[0]).padEnd(20)}  tx ${l.transactionHash}`);
  }
  const rotations = evenements.filter((l) => TOPIC[l.topics[0]] === 'ValidatorSetUpdated').length;
  const balayages = evenements.filter((l) => TOPIC[l.topics[0]] === 'SurplusSwept').length;
  const depots = evenements.filter((l) => TOPIC[l.topics[0]] === 'ValidatorDeposit').length;
  console.log(`      → ${evenements.length} événement(s)`);

  if (evenements.length === 0) {
    echec(`aucun log du contrat système sur 0 → ${tete} : l'inventaire des actions du gouverneur est IMPOSSIBLE, pas vide.`,
      'le bloc 1 émet ValidatorSetUpdated par construction : un résultat vide signifie que ce nœud n\'indexe pas les logs, qu\'un proxy les filtre, ou que sa base a été élaguée. Interroger un nœud d\'archive.');
  } else if (rotations < 1) {
    echec('ValidatorSetUpdated jamais observé : le init() du bloc 1 est introuvable dans les logs.',
      'l\'historique servi est incomplet — les rotations du jeu de validateurs ne peuvent pas être inventoriées sur ce nœud.');
  } else {
    console.log(`        rotations du jeu de validateurs : ${rotations} (1 = le init() du bloc 1 seul ; au-delà, le gouverneur a agi ${rotations - 1} fois)`);
  }
  // Recoupement avec un fait indépendant des logs : si le contrat détient des frais, c'est
  // que deposit() a été appelée, donc que ValidatorDeposit a été émis. Un solde sans dépôt
  // visible dénonce un historique tronqué que le compte d'événements seul ne révèle pas.
  if (soldeValset > 0n && depots === 0) {
    echec(`le contrat système détient ${bosa(soldeValset)} BOSA de frais mais aucun ValidatorDeposit n'est visible : les logs servis sont incomplets.`,
      'ne pas conclure « aucune action du gouverneur » à partir de cet inventaire ; interroger un nœud d\'archive.');
  }
  console.log(`      → sweepSurplus appelé : ${balayages} fois`);

  // --- 8. vivacité : la chaîne produit-elle encore, et ce nœud la suit-il ? ------------
  // Ce que ce bloc attrape : un nœud figé dont la tête est pourtant récente — arrêté il y a
  // moins d'une minute, ou proxy qui rejoue une hauteur constante. Sur un réseau à 5 s/bloc,
  // une tête qui ne bouge pas pendant douze intervalles n'est pas une chaîne qui produit.
  // L'attente bornée évite l'inverse — échouer à tort sur une exécution très rapide.
  console.log('\n  [7] vivacité');
  const finAttente = Date.now() + ATTENTE_MAX * 1000;
  let tete2 = await p.getBlockNumber();
  while (tete2 <= tete && Date.now() < finAttente) {
    await new Promise((r) => setTimeout(r, Math.min(3000, INTERVALLE * 1000)));
    tete2 = await p.getBlockNumber();
  }
  if (tete2 <= tete) {
    echec(`la tête n'a pas avancé (${tete} → ${tete2}) en ${ATTENTE_MAX} s, soit ${ATTENTE_MAX / INTERVALLE} intervalles de bloc.`,
      'la chaîne ne produit plus, ou ce nœud est figé / arrêté / rejoue une hauteur constante. Les soldes ci-dessus sont ceux d\'un instantané qui n\'avance pas : vérifier le nœud avant toute conclusion.');
  } else {
    console.log(`      tête passée de ${tete} à ${tete2} pendant le contrôle : la chaîne produit et ce nœud la suit`);
  }

  console.log('\n  ' + '='.repeat(96));
  if (echecs) {
    console.error(`\n  ${echecs} ÉCHEC(S) — la garde de l'offre N'EST PAS démontrée sur ${RPC}.\n`);
    process.exit(1);
  }
  console.log(`  Réconciliation complète sur ${RPC}, bloc ${tete} : l'offre est intégralement localisée.\n`);
})().catch((e) => {
  console.error('\n  ÉCHEC : ' + (e.shortMessage || e.message));
  console.error(`          → contrôle interrompu sur ${RPC} : aucune conclusion sur la garde n'est publiable.\n`);
  process.exit(1);
});
