// Vérifie que la chaîne franchit un bloc d'epoch — et qu'elle le franchit SOUS
// LES YEUX du contrôle.
//
// C'est le contrôle de non-régression le plus important du dépôt. Tous les 200
// blocs, Parlia interroge getMiningValidators() sur le contrat système 0x…1000
// pour reconstruire l'extraData. Le contrat hérité de BNB Chain n'expose pas
// cette fonction : la chaîne se fige alors définitivement, en bouclant sur
// « Failed to prepare header for sealing ». CoinbosaValidatorSet corrige ce
// défaut, et ce script s'assure qu'on ne le réintroduit pas.
//
// ------------------------------------------------------------------------
// CE QUE CE CONTRÔLE ATTRAPE, et pourquoi il est écrit ainsi
// ------------------------------------------------------------------------
//
// L'accident redouté n'arrive PAS forcément au bloc 200. Le piège décrit dans
// AGENTS.md — passer de 1 à N validateurs alors qu'un seul nœud scelle — arrête
// le réseau au PROCHAIN bloc d'epoch, quel qu'il soit, silencieusement, et sans
// retour possible : plus aucun bloc n'étant produit, aucune transaction
// corrective ne peut être minée.
//
// La version précédente de ce script ne regardait que le bloc 200 et sortait de
// sa boucle dès que la tête le dépassait. Une chaîne morte au bloc 3199, juste
// avant le bloc d'epoch 3200 qu'elle ne franchira jamais, était déclarée
// conforme en 0,3 s après UN SEUL appel eth_getBlockByNumber. Le contrôle
// disait « bloc d'epoch franchi » d'une chaîne cliniquement morte.
//
// D'où les six règles qui structurent ce fichier :
//
//   1. La frontière contrôlée est DÉDUITE DE LA TÊTE observée au démarrage, pas
//      d'une constante. Le script attend donc toujours un franchissement qu'il
//      n'a pas encore vu, quel que soit l'âge de la chaîne.
//   2. Rien n'est déclaré franchi sans avoir été OBSERVÉ : on exige des blocs
//      produits pendant le contrôle, et un bloc d'epoch postérieur au bloc de
//      départ. Une chaîne figée échoue, elle ne sort plus en silence — et une
//      chaîne qui traîne finit par échouer sur un délai maximal, au lieu de
//      tourner sans jamais rendre de verdict.
//   3. L'attendu ne se déduit jamais de l'observé. Le nombre de validateurs
//      inscrit dans l'en-tête est confronté au contrat système, source
//      INDÉPENDANTE ; sans ce recoupement, un en-tête annonçant zéro validateur
//      se validait tout seul (98 = 32+1+0×68+65).
//   4. Le contrat est lu AU BLOC CONTRÔLÉ (blockTag), pas à `latest` : c'est
//      l'état qui a produit l'extraData. Parlia fait exactement cela —
//      getCurrentValidators(header.ParentHash, N-1), parlia.go:1224.
//   5. Le script sait sur quel réseau il parle : le chainId est comparé avant
//      tout le reste. 8545 et 18545 hébergent deux réseaux différents dans ce
//      dépôt (CI et répétition POBS) ; auditer le mauvais nœud et publier le
//      résultat comme celui de Coinbosa serait une fausse déclaration.
//   6. La frontière elle-même est confirmée par le CLIENT interrogé
//      (parlia_getSnapshot → epoch_length), pas seulement par le fichier de
//      documentation, qui « ne configure pas le client » de son propre aveu.
//
// Aucun de ces contrôles ne peut « se taire » : quand une preuve manque, le
// script échoue en disant laquelle et quoi faire.
const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

const RPC = process.env.RPC || 'http://127.0.0.1:8545';
const ROOT = path.join(__dirname, '..');
const config = JSON.parse(fs.readFileSync(path.join(ROOT, 'coinbosa.config.json'), 'utf8'));
const EPOCH = config.network.epochLength;
const CHAIN_ID = config.network.chainId;
const BLOC_MS = config.network.blockIntervalMs;
const VALSET = '0x0000000000000000000000000000000000001000';
const ADRESSE_NULLE = '0x0000000000000000000000000000000000000000';

// Tailles imposées par consensus/parlia/parlia.go (extraVanity, extraSeal,
// validatorNumberSize, validatorBytesLength). Elles ne sont pas négociables :
// un octet d'écart et le client refuse l'en-tête.
const VANITY = 32;
const COMPTEUR = 1;
const ADRESSE = 20;
const CLE_BLS = 48;
const SCEAU = 65;
const PAR_VALIDATEUR = ADRESSE + CLE_BLS; // 68

const SONDE_MS = 5000;
const SONDES_FIGEE = 12; // 12 × 5 s = 60 s sans le moindre bloc

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Un message d'échec s'adresse à quelqu'un qui devra agir : il dit ce qui
// manque et quoi faire, pas seulement « échec ».
function echec(titre, ...aFaire) {
  console.error(`\nECHEC : ${titre}`);
  for (const ligne of aFaire) if (ligne !== '') console.error(ligne);
  process.exit(1);
}

// ------------------------------------------------------------------------
// Configuration : refuser de travailler avec une cible qu'on ne sait pas
// calculer. Sans cette garde, un epochLength absent donnait une cible NaN,
// `head >= NaN` toujours faux, et le script tournait jusqu'à ce qu'on le tue :
// ni vert, ni rouge, aucun verdict — et personne pour s'en apercevoir.
// ------------------------------------------------------------------------
if (!Number.isInteger(EPOCH) || EPOCH < 2) {
  echec(
    `network.epochLength illisible dans coinbosa.config.json : ${JSON.stringify(EPOCH)}`,
    `Sans cette valeur le script ne sait pas quelle frontière contrôler.`,
    `À faire : rétablir "epochLength": 200 dans coinbosa/coinbosa.config.json`,
    `(200 = defaultEpochLength, consensus/parlia/parlia.go:58).`
  );
}
if (!Number.isInteger(CHAIN_ID) || CHAIN_ID < 1) {
  echec(
    `network.chainId illisible dans coinbosa.config.json : ${JSON.stringify(CHAIN_ID)}`,
    `À faire : rétablir "chainId": 26262 dans coinbosa/coinbosa.config.json.`
  );
}
if (!Number.isFinite(BLOC_MS) || BLOC_MS < 100) {
  echec(
    `network.blockIntervalMs illisible dans coinbosa.config.json : ${JSON.stringify(BLOC_MS)}`,
    `Cette valeur sert à calculer le délai d'attente maximal du franchissement.`,
    `À faire : rétablir "blockIntervalMs": 5000 dans coinbosa/coinbosa.config.json.`
  );
}

// ------------------------------------------------------------------------
// La frontière d'epoch est une constante du CLIENT Go, pas du fichier JSON de
// documentation. Ce recoupement n'échoue que sur une CONTRADICTION avérée avec
// un genesis présent sur le disque : contrôler la mauvaise frontière tout en
// affirmant contrôler celle du client serait une fausse déclaration.
// ------------------------------------------------------------------------
function recouperEpochAvecGenesis() {
  const candidats = process.env.GENESIS
    ? [path.resolve(process.env.GENESIS)]
    : [path.join(ROOT, 'genesis', 'genesis-coinbosa-dev.json'), path.join(ROOT, 'genesis', 'genesis-coinbosa.json')];

  for (const fichier of candidats) {
    if (!fs.existsSync(fichier)) continue;
    let epochGenesis;
    try {
      epochGenesis = JSON.parse(fs.readFileSync(fichier, 'utf8')).config?.parlia?.epoch;
    } catch (e) {
      continue; // un genesis illisible est le problème d'un autre contrôle
    }
    if (epochGenesis === undefined) continue;
    if (Number(epochGenesis) !== EPOCH) {
      echec(
        `frontière d'epoch contradictoire : ${EPOCH} documenté, ${epochGenesis} dans ${path.relative(ROOT, fichier)}`,
        `Le script contrôlerait une frontière qui n'est pas celle de la chaîne.`,
        `À faire : aligner network.epochLength (coinbosa.config.json), config.parlia.epoch`,
        `du genesis, et defaultEpochLength (consensus/parlia/parlia.go:58) — puis relancer.`
      );
    }
    return `${path.relative(ROOT, fichier)} (config.parlia.epoch)`;
  }
  return null;
}

// ------------------------------------------------------------------------
// La frontière d'epoch est une constante du CLIENT Go : le fichier JSON ne fait
// que la documenter (« ces valeurs documentent le client patché, elles ne le
// configurent pas »). parlia_getSnapshot expose epoch_length, c'est-à-dire la
// valeur réellement en vigueur dans le moteur de consensus interrogé. C'est la
// seule source qui prouve qu'on contrôle LA BONNE frontière : un client dont
// l'epoch passerait à 100 ou à 500 (forks Lorentz/Maxwell, parlia.go:59-60)
// rendrait ce contrôle muet sur la vraie frontière tout en restant vert.
// ------------------------------------------------------------------------
async function epochDuClient(provider, bloc) {
  try {
    const snap = await provider.send('parlia_getSnapshot', ['0x' + bloc.toString(16)]);
    const e = Number(snap && snap.epoch_length);
    return Number.isInteger(e) && e > 0 ? e : null;
  } catch (e) {
    return null; // API parlia non exposée : on le dira, on ne l'inventera pas
  }
}

// ------------------------------------------------------------------------
// Décodage de l'extraData d'un bloc d'epoch :
//   32 vanity | 1 compteur | N × (20 adresse + 48 clé BLS) | 65 sceau
// Toute anomalie de structure fait échouer : un en-tête qu'on ne sait pas lire
// n'est pas un en-tête conforme.
// ------------------------------------------------------------------------
function decoderEnTeteEpoch(numero, bloc) {
  if (!bloc) {
    echec(
      `le bloc d'epoch ${numero} est introuvable sur ${RPC}`,
      `Le nœud a annoncé une tête au-delà de ce bloc mais ne sait pas le servir.`,
      `À faire : vérifier que RPC pointe bien sur le nœud de la chaîne contrôlée et`,
      `que sa synchronisation n'est pas partielle (--syncmode full).`
    );
  }
  const hex = (bloc.extraData || '0x').toLowerCase();
  if (!/^0x([0-9a-f]{2})*$/.test(hex)) {
    echec(`bloc d'epoch ${numero} : extraData illisible (${bloc.extraData})`,
      `À faire : le nœud interrogé ne sert pas un en-tête Parlia valide — vérifier RPC.`);
  }
  const octets = (hex.length - 2) / 2;
  const minimum = VANITY + COMPTEUR + PAR_VALIDATEUR + SCEAU; // 166 octets, 1 validateur

  // Un compteur nul, c'est la signature d'un contrat système qui rend un jeu
  // VIDE : plus personne pour sceller le bloc suivant, la chaîne s'arrête sans
  // erreur. L'ancienne version validait ce cas (98 === 32+1+0×68+65) parce
  // qu'elle comparait l'extraData à elle-même.
  const nb = octets >= VANITY + COMPTEUR ? parseInt(hex.slice(66, 68), 16) : NaN;
  if (!Number.isInteger(nb) || nb < 1) {
    echec(
      `bloc d'epoch ${numero} : l'en-tête annonce ${Number.isInteger(nb) ? nb : 'un nombre illisible de'} validateur(s)`,
      `Un bloc d'epoch sans validateur laisse la chaîne sans scelleur au bloc suivant.`,
      `À faire : vérifier que getMiningValidators() sur ${VALSET} rend un jeu non vide`,
      `(CoinbosaValidatorSet.sol rend INITIAL_VALIDATOR quand le tableau est vide).`
    );
  }

  const attendu = VANITY + COMPTEUR + nb * PAR_VALIDATEUR + SCEAU;
  if (octets < minimum || octets !== attendu) {
    echec(
      `bloc d'epoch ${numero} : extraData de ${octets} octets, ${attendu} attendus pour ${nb} validateur(s)`,
      `Disposition attendue : 32 vanity + 1 compteur + ${nb} × 68 (20 adresse + 48 clé BLS) + 65 sceau.`,
      `Deux causes connues : (a) le bloc ${numero} n'est pas un bloc d'epoch pour le client`,
      `interrogé — epochLength documenté ${EPOCH} ≠ epoch réel (parlia.go:58, forks Lorentz/Maxwell) ;`,
      `(b) le client embarque un champ supplémentaire dans l'en-tête (turnLength post-Bohr,`,
      `attestation de vote). À faire : recompiler le client de ce dépôt (make geth) et relancer.`
    );
  }

  const adresses = [];
  const cles = [];
  for (let i = 0; i < nb; i++) {
    const debut = 68 + 136 * i; // 2 (0x) + 2 × (32 vanity + 1 compteur) puis 136 par validateur
    adresses.push('0x' + hex.slice(debut, debut + 40));
    cles.push('0x' + hex.slice(debut + 40, debut + 136));
  }

  // Une adresse nulle ou dupliquée dans le jeu gonfle le quorum Parlia
  // (⌊N/2⌋+1 scelleurs distincts et en ligne) sans ajouter de scelleur réel :
  // c'est l'arrêt de chaîne garanti au bloc d'epoch suivant.
  for (const a of adresses) {
    if (a === ADRESSE_NULLE) {
      echec(
        `bloc d'epoch ${numero} : l'adresse nulle figure dans le jeu de validateurs`,
        `Personne ne détient la clé de 0x0 : ce membre compte dans le quorum et ne scellera jamais.`,
        `À faire : corriger le jeu via updateValidatorSet() (le contrat refuse déjà "zero address"`,
        `— une adresse nulle ici signifie un genesis mal construit : voir scripts/build-genesis.js).`
      );
    }
  }
  const uniques = new Set(adresses);
  if (uniques.size !== adresses.length) {
    echec(
      `bloc d'epoch ${numero} : le jeu de validateurs contient un doublon (${adresses.join(', ')})`,
      `Un doublon gonfle N sans ajouter de scelleur : le quorum ⌊N/2⌋+1 devient inatteignable.`,
      `À faire : corriger le jeu via updateValidatorSet() puis attendre le prochain bloc d'epoch.`
    );
  }

  return { octets, nb, adresses, cles, scelleur: (bloc.miner || '').toLowerCase(), horodatage: Number(bloc.timestamp) };
}

const listeTriee = (t) => t.map((x) => x.toLowerCase()).slice().sort().join(', ');

(async () => {
  const provider = new ethers.JsonRpcProvider(RPC);
  const valset = new ethers.Contract(
    VALSET,
    ['function getMiningValidators() view returns (address[], bytes[])'],
    provider
  );

  // --------------------------------------------------------------------
  // 1. Sur quel réseau parle-t-on ? Le chainId était demandé trois fois par
  //    ethers puis intégralement jeté : le script auditait n'importe quel
  //    endpoint et rapportait le résultat comme s'il s'agissait de Coinbosa.
  // --------------------------------------------------------------------
  let reseau;
  try {
    reseau = await provider.getNetwork();
  } catch (e) {
    echec(
      `aucune réponse JSON-RPC sur ${RPC} (${e.shortMessage || e.message})`,
      `À faire : démarrer le nœud, ou pointer la variable RPC sur le bon port`,
      `(8545 = nœud de CI, 18545 = répétition POBS).`
    );
  }
  if (Number(reseau.chainId) !== CHAIN_ID) {
    echec(
      `chainId ${reseau.chainId} sur ${RPC}, ${CHAIN_ID} attendu — ce n'est pas Coinbosa Chain`,
      `Tout ce que ce script aurait affirmé aurait porté sur un autre réseau.`,
      `À faire : pointer RPC sur le nœud Coinbosa (RPC=http://127.0.0.1:8545 en CI,`,
      `RPC=http://127.0.0.1:18545 pour la répétition POBS).`
    );
  }

  const sourceEpoch = recouperEpochAvecGenesis();

  // --------------------------------------------------------------------
  // 2. La frontière à franchir se déduit de la TÊTE observée : on contrôle
  //    toujours le PROCHAIN franchissement, jamais un franchissement passé
  //    qu'on n'a pas vu. C'est ce qui fait échouer une chaîne figée à 3199.
  // --------------------------------------------------------------------
  const h0 = await provider.getBlockNumber();
  const blocDepart = await provider.getBlock(h0);
  if (!blocDepart) {
    echec(`le nœud annonce une tête au bloc ${h0} mais ne sait pas servir ce bloc`,
      `À faire : vérifier l'état de synchronisation du nœud interrogé sur ${RPC}.`);
  }
  const epochPrecedent = Math.floor(h0 / EPOCH) * EPOCH;
  const epochNum = epochPrecedent + EPOCH;
  const cible = epochNum + 2; // 2 blocs après la frontière : la chaîne doit REPARTIR

  const epochClient = await epochDuClient(provider, h0);
  if (epochClient !== null && epochClient !== EPOCH) {
    echec(
      `le client interrogé travaille par epochs de ${epochClient} blocs, ${EPOCH} documenté`,
      `Le script contrôlerait une frontière qui n'est pas celle où ce client interroge le`,
      `contrat système : il pourrait passer au vert sans avoir rien vérifié de l'epoch réel.`,
      `À faire : aligner network.epochLength (coinbosa.config.json) sur la valeur du client`,
      `(defaultEpochLength, consensus/parlia/parlia.go:58 — 500 après Lorentz, 1000 après Maxwell),`,
      `ou interroger le client de ce dépôt.`
    );
  }

  const sources = [
    epochClient !== null
      ? 'confirmé par le client (parlia_getSnapshot)'
      : 'NON confirmé par le client : parlia_getSnapshot indisponible sur ce nœud',
  ];
  if (sourceEpoch) sources.push(`recoupé avec ${sourceEpoch}`);

  console.log(`  réseau : chainId ${reseau.chainId} sur ${RPC}`);
  console.log(`  epoch  : ${EPOCH} blocs — ${sources.join(' ; ')}`);
  console.log(`  tête au démarrage : bloc ${h0}`);
  console.log(`  franchissement à observer : bloc d'epoch ${epochNum}, confirmé jusqu'au bloc ${cible}`);

  // --------------------------------------------------------------------
  // 3. Contrôle immédiat du dernier bloc d'epoch DÉJÀ franchi. Il ne prouve
  //    pas la vivacité (il est peut-être vieux de mille blocs) mais il coûte
  //    deux appels et évite d'attendre vingt minutes avant de découvrir un jeu
  //    de validateurs déjà cassé.
  // --------------------------------------------------------------------
  let enTetePrecedent = null;
  if (epochPrecedent > 0) {
    enTetePrecedent = decoderEnTeteEpoch(epochPrecedent, await provider.getBlock(epochPrecedent));
    console.log(`\n  dernier bloc d'epoch franchi : ${epochPrecedent}, ${enTetePrecedent.nb} validateur(s)`);
    console.log(`    ${enTetePrecedent.adresses.join(', ')}`);

    // L'état historique n'est pas garanti sur un nœud élagué. On le dit au lieu
    // de le taire, et on ne compte pas ce recoupement comme fait : le
    // recoupement DÉCISIF porte sur le bloc d'epoch observé plus bas, dont
    // l'état est toujours à portée.
    try {
      const [valsAvant, votesAvant] = await valset.getMiningValidators({ blockTag: epochPrecedent - 1 });
      confronterAvecContrat(epochPrecedent, enTetePrecedent, valsAvant, votesAvant, '    ');
    } catch (e) {
      console.log(`    recoupement contrat non effectué : état du bloc ${epochPrecedent - 1} indisponible`);
      console.log(`    (${(e.shortMessage || e.message).slice(0, 120)}) — nœud élagué ? --gcmode archive le rendrait vérifiable.`);
    }
  } else {
    // Avant le premier bloc d'epoch, le seul en-tête étendu disponible est celui
    // du genesis : il porte déjà le jeu initial, on s'en sert comme référence pour
    // le scelleur. On ne s'en sert QUE s'il suit exactement la disposition d'un
    // bloc d'epoch — un genesis exotique ne doit pas faire échouer un contrôle
    // dont ce n'est pas l'objet.
    const g = await provider.getBlock(0);
    const octets0 = g ? (g.extraData.length - 2) / 2 : 0;
    const nb0 = octets0 >= VANITY + COMPTEUR ? parseInt(g.extraData.slice(66, 68), 16) : 0;
    if (nb0 >= 1 && octets0 === VANITY + COMPTEUR + nb0 * PAR_VALIDATEUR + SCEAU) {
      enTetePrecedent = decoderEnTeteEpoch(0, g);
      console.log(`\n  jeu inscrit au bloc 0 : ${enTetePrecedent.adresses.join(', ')}`);
    } else {
      console.log(`\n  bloc 0 sans jeu de validateurs lisible : le scelleur sera confronté au seul jeu du bloc d'epoch`);
    }
  }

  // --------------------------------------------------------------------
  // 4. Attendre le franchissement. Trois façons d'échouer, aucune de se taire :
  //    chaîne figée (12 sondes identiques), délai global dépassé, tête qui
  //    n'atteint jamais la cible.
  // --------------------------------------------------------------------
  const ATTENTE_MAX_S = Math.max(180, Math.ceil((cible - h0 + 40) * (BLOC_MS / 1000)));
  const depart = Date.now();
  let derniere = -1;
  let figee = 0;
  let reculSignale = false;
  let head = h0;

  console.log(`\n  attente du bloc ${cible} (délai maximal ${ATTENTE_MAX_S} s)`);

  while (true) {
    head = await provider.getBlockNumber();
    if (head >= cible) break;

    const ecoule = Math.round((Date.now() - depart) / 1000);
    if (ecoule > ATTENTE_MAX_S) {
      await diagnosticArret(
        `cible ${cible} non atteinte en ${ecoule} s : tête ${head}, contre ${h0} au démarrage du contrôle`,
        head
      );
    }
    if (head === derniere) {
      if (++figee >= SONDES_FIGEE) {
        await diagnosticArret(`la chaîne est figée au bloc ${head} depuis ${figee * SONDE_MS / 1000} s`, head);
      }
    } else {
      // Une tête qui recule est signalée UNE fois : répéter l'avertissement à
      // chaque sonde noierait le verdict final dans le bruit — et un contrôle
      // qu'on ne lit plus ne protège plus de rien.
      if (head < derniere && derniere !== -1 && !reculSignale) {
        reculSignale = true;
        console.log(`  ATTENTION : la tête a reculé de ${derniere - head} bloc(s) (${derniere} → ${head}).`);
        console.log(`  Un arrêt brutal fait repartir geth au dernier arrêt propre (AGENTS.md) ; une chaîne`);
        console.log(`  qui oscille ne franchira pas la frontière : le délai maximal ci-dessus tranchera.`);
      }
      figee = 0;
      if (head % 50 === 0 || head > epochNum - 5) console.log(`  bloc ${head}…`);
      derniere = head;
    }
    await sleep(SONDE_MS);
  }

  // --------------------------------------------------------------------
  // 5. Preuve de fraîcheur, sans dépendre d'aucune horloge locale : le bloc
  //    d'epoch doit être POSTÉRIEUR au bloc de départ, et la chaîne doit avoir
  //    avancé. Un nœud qui rejoue un vieux bloc ne peut pas satisfaire cela.
  // --------------------------------------------------------------------
  const blocEpoch = await provider.getBlock(epochNum);
  const enTete = decoderEnTeteEpoch(epochNum, blocEpoch);
  if (epochNum <= h0 || enTete.horodatage <= Number(blocDepart.timestamp)) {
    echec(
      `le bloc d'epoch ${epochNum} n'a pas été produit pendant ce contrôle`,
      `Son horodatage (${enTete.horodatage}) n'est pas postérieur à celui du bloc de départ ${h0} (${blocDepart.timestamp}).`,
      `Ce contrôle ne prouverait alors aucun franchissement observé.`,
      `À faire : relancer contre un nœud qui produit réellement des blocs.`
    );
  }

  console.log(`\n  bloc d'epoch ${epochNum} scellé par ${enTete.scelleur}`);
  console.log(`  extraData : ${enTete.octets} octets, ${enTete.nb} validateur(s)`);
  console.log(`    ${enTete.adresses.join(', ')}`);

  // --------------------------------------------------------------------
  // 6. Le recoupement décisif : l'en-tête contre le contrat système, lu À
  //    L'ÉTAT QUI L'A PRODUIT (bloc d'epoch − 1, comme parlia.go:1224). Si cet
  //    appel échoue, c'est exactement le défaut hérité de BNB Chain.
  // --------------------------------------------------------------------
  let vals;
  let votes;
  try {
    [vals, votes] = await valset.getMiningValidators({ blockTag: epochNum - 1 });
  } catch (e) {
    echec(
      `getMiningValidators() ne répond pas au bloc ${epochNum - 1} : ${e.shortMessage || e.message}`,
      `C'est le symptôme du contrat système défaillant : Parlia appelle cette fonction pour`,
      `reconstruire l'extraData du bloc d'epoch. Si elle revert, le bloc devient improduisible`,
      `et la chaîne s'arrête définitivement.`,
      `À faire : vérifier que ${VALSET} porte bien le bytecode de CoinbosaValidatorSet`,
      `(node scripts/check-genesis-hash.js) et que le nœud expose l'état du bloc ${epochNum - 1}.`
    );
  }
  confronterAvecContrat(epochNum, enTete, vals, votes, '  ');

  // Le scelleur doit appartenir au jeu actif à cet instant. Parlia n'applique le
  // nouveau jeu qu'au bloc d'epoch + ⌊N/2⌋ (snapshot.go:374) : le scelleur peut
  // donc légitimement venir du jeu précédent. On accepte l'union des deux — ce
  // qui laisse quand même tomber un bloc scellé par un inconnu.
  const jeuAdmis = new Set(enTete.adresses);
  if (enTetePrecedent) for (const a of enTetePrecedent.adresses) jeuAdmis.add(a);
  if (!jeuAdmis.has(enTete.scelleur)) {
    echec(
      `le bloc d'epoch ${epochNum} est scellé par ${enTete.scelleur}, qui n'appartient à aucun jeu de validateurs connu`,
      `Jeu du bloc ${epochNum} : ${enTete.adresses.join(', ')}`,
      enTetePrecedent ? `Jeu du bloc ${epochPrecedent} : ${enTetePrecedent.adresses.join(', ')}` : '',
      `Un bloc scellé hors du jeu signifie que RPC ne pointe pas sur la chaîne annoncée.`,
      `À faire : vérifier l'adresse du nœud interrogé et le genesis dont il est parti.`
    );
  }

  // --------------------------------------------------------------------
  // 7. Vivacité du contrat à `latest` : il doit répondre maintenant aussi.
  //    Une divergence avec le jeu du bloc d'epoch n'est pas une faute — c'est
  //    une rotation en attente — mais elle doit être DITE, parce que c'est
  //    exactement l'état qui précède l'arrêt de chaîne d'AGENTS.md.
  // --------------------------------------------------------------------
  let valsMaintenant;
  try {
    [valsMaintenant] = await valset.getMiningValidators();
  } catch (e) {
    echec(
      `getMiningValidators() ne répond plus à latest : ${e.shortMessage || e.message}`,
      `Le prochain bloc d'epoch sera improduisible et la chaîne s'arrêtera sans erreur.`,
      `À faire : vérifier le bytecode de ${VALSET} avant le bloc ${epochNum + EPOCH}.`
    );
  }
  if (!valsMaintenant.length) {
    echec(
      `getMiningValidators() rend un jeu VIDE à latest`,
      `Au prochain bloc d'epoch (${epochNum + EPOCH}), la chaîne n'aura plus aucun scelleur.`,
      `À faire : rétablir le jeu via updateValidatorSet() AVANT ce bloc — après, plus aucune`,
      `transaction ne pourra être minée.`
    );
  }
  if (listeTriee(valsMaintenant) !== listeTriee(enTete.adresses)) {
    console.log(`\n  ATTENTION : rotation en attente d'application.`);
    console.log(`  jeu du bloc d'epoch ${epochNum} : ${enTete.adresses.length} — ${enTete.adresses.join(', ')}`);
    console.log(`  jeu du contrat à latest     : ${valsMaintenant.length} — ${valsMaintenant.join(', ')}`);
    console.log(`  Elle s'appliquera au bloc ${epochNum + EPOCH}. Si les entrants n'ont pas été VUS sceller,`);
    console.log(`  le réseau s'arrêtera là, sans retour possible (AGENTS.md). Contrôle : scripts/rotate-validators.js.`);
  }

  const finale = await provider.getBlockNumber();
  console.log(`\n  franchissement OBSERVÉ : bloc d'epoch ${epochNum} produit pendant le contrôle`);
  console.log(`  ${finale - h0} bloc(s) produit(s) depuis le démarrage, chaîne à ${finale}`);
  console.log(`  en-tête et contrat système d'accord sur ${enTete.nb} validateur(s), clés BLS conformes`);

  // ----------------------------------------------------------------------
  // Confrontation en-tête ↔ contrat système. L'attendu vient du CONTRAT, jamais
  // de l'octet compteur qu'on est en train de contrôler : sans cela le contrôle
  // se comparait à lui-même et ne pouvait rien détecter.
  // ----------------------------------------------------------------------
  function confronterAvecContrat(numero, entete, valsContrat, votesContrat, marge) {
    if (!valsContrat.length) {
      echec(
        `getMiningValidators() rend un jeu VIDE à l'état du bloc ${numero - 1}`,
        `Le bloc d'epoch ${numero} n'aurait dû pouvoir être scellé par personne.`,
        `À faire : vérifier le bytecode de ${VALSET} — CoinbosaValidatorSet rend toujours`,
        `au moins INITIAL_VALIDATOR, jamais un tableau vide.`
      );
    }
    if (entete.nb !== valsContrat.length) {
      echec(
        `bloc d'epoch ${numero} : l'en-tête annonce ${entete.nb} validateur(s), le contrat système en rend ${valsContrat.length}`,
        `Les deux sources décrivent le même instant et se contredisent : l'une des deux ment.`,
        `en-tête : ${entete.adresses.join(', ')}`,
        `contrat : ${valsContrat.join(', ')}`,
        `À faire : vérifier le bytecode de ${VALSET} et le client utilisé (make geth).`
      );
    }
    if (listeTriee(entete.adresses) !== listeTriee(valsContrat)) {
      echec(
        `bloc d'epoch ${numero} : le jeu inscrit dans l'en-tête n'est pas celui du contrat système`,
        `en-tête : ${listeTriee(entete.adresses)}`,
        `contrat : ${listeTriee(valsContrat)}`,
        `Le nœud interrogé ne produit pas ses en-têtes à partir de ce contrat : soit RPC pointe`,
        `sur une autre chaîne, soit le client n'est pas celui de ce dépôt.`,
        `À faire : contrôler RPC, puis recompiler le client (make geth) et relancer.`
      );
    }
    if (votesContrat) {
      // Les clés BLS sont indexées position par position par Parlia
      // (parlia.go:1960) : une clé manquante décale tout le tableau.
      if (votesContrat.length !== valsContrat.length) {
        echec(
          `bloc d'epoch ${numero} : ${valsContrat.length} validateur(s) pour ${votesContrat.length} clé(s) de vote`,
          `Parlia associe adresses et clés par position : un tableau plus court corrompt`,
          `l'association et rend la finalité rapide inexploitable.`,
          `À faire : vérifier updateValidatorSet() — le contrat impose une clé de 48 octets par validateur.`
        );
      }
      for (let i = 0; i < votesContrat.length; i++) {
        const longueur = ethers.dataLength(votesContrat[i]);
        if (longueur !== CLE_BLS) {
          echec(
            `bloc d'epoch ${numero} : clé de vote ${i} de ${longueur} octets, ${CLE_BLS} attendus`,
            `À faire : corriger le jeu via updateValidatorSet() (VOTE_ADDRESS_LENGTH = 48).`
          );
        }
      }
      // Les clés de l'en-tête suivent l'ordre des adresses TRIÉES (parlia.go
      // trie le jeu avant de l'écrire) : on compare donc paire par paire.
      const parAdresse = new Map();
      valsContrat.forEach((v, i) => parAdresse.set(v.toLowerCase(), votesContrat[i].toLowerCase()));
      for (let i = 0; i < entete.nb; i++) {
        const attendue = parAdresse.get(entete.adresses[i]);
        if (attendue !== entete.cles[i]) {
          echec(
            `bloc d'epoch ${numero} : la clé BLS de ${entete.adresses[i]} diffère entre l'en-tête et le contrat`,
            `en-tête : ${entete.cles[i]}`,
            `contrat : ${attendue}`,
            `À faire : vérifier le client utilisé et le contenu de voteAddresses dans ${VALSET}.`
          );
        }
      }
      console.log(`${marge}contrat système d'accord : ${valsContrat.length} validateur(s), ${votesContrat.length} clé(s) BLS de 48 octets`);
    } else {
      console.log(`${marge}contrat système d'accord sur le jeu de ${valsContrat.length} validateur(s)`);
    }
  }

  // ----------------------------------------------------------------------
  // Message d'arrêt : c'est ici qu'on nomme le piège d'AGENTS.md. Une chaîne
  // figée à un bloc AVANT une frontière d'epoch, avec un jeu de validateurs plus
  // grand que le nombre de scelleurs réels, est morte de façon irréversible.
  // ----------------------------------------------------------------------
  async function diagnosticArret(titre, tete) {
    const lignes = [];
    const resteAvantEpoch = epochNum - tete;

    // Le jeu que le contrat annonce MAINTENANT est la pièce à conviction : s'il
    // exige plus de scelleurs distincts que le dernier bloc d'epoch n'en portait,
    // on tient la cause exacte de l'arrêt — le passage 1 -> N d'AGENTS.md.
    if (enTetePrecedent) {
      try {
        const [valsMaintenant] = await valset.getMiningValidators();
        if (valsMaintenant.length > enTetePrecedent.adresses.length) {
          lignes.push(`CAUSE PROBABLE : le contrat système annonce ${valsMaintenant.length} validateur(s) —`);
          lignes.push(`${valsMaintenant.join(', ')} —`);
          lignes.push(`alors que le dernier bloc d'epoch (${epochPrecedent}) n'en portait que ${enTetePrecedent.adresses.length}.`);
          lignes.push(`Parlia exige ⌊N/2⌋+1 scelleurs DISTINCTS et EN LIGNE : ce jeu s'applique au bloc`);
          lignes.push(`d'epoch ${epochNum} et le rend improduisible si les entrants ne scellent pas.`);
          lignes.push(`C'est le piège documenté dans AGENTS.md — et il est irréversible on-chain.`);
          lignes.push('');
        }
      } catch (e) {
        lignes.push(`De plus, getMiningValidators() ne répond plus : ${(e.shortMessage || e.message).slice(0, 100)}`);
        lignes.push('');
      }
    }

    if (tete < epochNum && resteAvantEpoch <= 2) {
      lignes.push(`La chaîne n'a pas franchi le bloc d'epoch ${epochNum} : il lui manquait ${resteAvantEpoch} bloc(s).`);
      lignes.push(`C'est le symptôme exact du bloc d'epoch improduisible : Parlia appelle`);
      lignes.push(`getMiningValidators() sur ${VALSET} pour reconstruire l'extraData ; si l'appel`);
      lignes.push(`revert, ou si le jeu rendu exige plus de scelleurs distincts qu'il n'y en a en`);
      lignes.push(`ligne (⌊N/2⌋+1, snapshot.go:243), le bloc n'est jamais scellé.`);
      lignes.push(`Aucune transaction corrective ne peut plus être minée : l'état est irréversible on-chain.`);
      lignes.push(`À faire : lire le journal du nœud (« Failed to prepare header for sealing »), puis`);
      lignes.push(`comparer le jeu du contrat au nombre de nœuds qui scellent réellement.`);
    } else {
      lignes.push(`Aucun franchissement n'a pu être observé : ce contrôle ne prouve rien tant que la`);
      lignes.push(`chaîne n'a pas produit le bloc d'epoch ${epochNum} sous ses yeux.`);
      lignes.push(`À faire : vérifier que le validateur scelle (journal du nœud, --mine, compte`);
      lignes.push(`déverrouillé), puis relancer. Le franchissement demande jusqu'à ${EPOCH} blocs,`);
      lignes.push(`soit environ ${Math.round(EPOCH * BLOC_MS / 1000 / 60)} min à ${BLOC_MS / 1000} s par bloc.`);
    }
    echec(titre, ...lignes);
  }
})().catch((e) => {
  console.error(`\nERREUR : ${e.message}`);
  console.error(`Le contrôle n'a pas pu conclure sur ${RPC} — il ne dit donc RIEN de la chaîne.`);
  console.error(`À faire : corriger la cause ci-dessus et relancer ; ne pas interpréter cet arrêt comme un succès.`);
  process.exit(1);
});
