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
//
// ─────────────────────────────────────────────────────────────────────────────────────────
// CE QUE CE CONTRÔLE PROUVE, ET CONTRE QUOI IL A ÉTÉ DURCI
// ─────────────────────────────────────────────────────────────────────────────────────────
// La version précédente ne demandait au nœud que `eth_chainId` puis un `eth_call`. Ses trois
// entrées — xpub, fichier d'adresses, chainId attendu — venaient donc TOUTES du côté audité :
// elle prouvait leur cohérence mutuelle, jamais un fait de la chaîne déployée. Un faux nœud
// de soixante lignes répondant `0x6696` et l'adresse voulue lui faisait afficher
// « 13/13 conformes, écarts : 0 » et sortir 0, sans qu'aucune chaîne Coinbosa n'existe
// derrière. C'est l'accident que les quatre ancrages ci-dessous rendent impossible :
//
//   [1] le bloc 0 servi par le nœud doit être CELUI QUI EST PUBLIÉ (hash ET stateRoot
//       comparés à genesis-reference.json). Un nœud qui n'a pas de bloc 0, ou dont le bloc 0
//       diffère, est écarté avant toute conclusion ;
//   [2] le gouverneur lu dans le bytecode figé doit être celui de la référence publiée ;
//   [3] chaque poste doit DÉTENIR AU BLOC 0 la part que lui donne coinbosa.config.json —
//       une adresse correctement dérivée mais vide n'est pas une trésorerie ;
//   [4] la liste des postes à vérifier vient de coinbosa.config.json, jamais du fichier
//       audité : un poste effacé du fichier d'adresses ne peut plus disparaître du rapport.
//
// Ce que ce contrôle N'EST PAS : un client léger. Il ne revalide pas le sceau des blocs. Un
// nœud entièrement hostile peut rejouer l'en-tête publié du bloc 0 ; en revanche il ne peut
// plus se contenter d'inventer des réponses, et il doit servir un état cohérent avec le
// stateRoot publié. Le lancer contre un nœud dont on répond, ou contre le RPC publié.
const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

const XPUB = process.env.XPUB;
const RPC = process.env.RPC || 'http://127.0.0.1:8545';
// Le nœud public tourne en `--gcmode full` (deploy/30-node.sh) : l'état du bloc 0 y est
// élagué, donc les soldes de genèse n'y sont PAS lisibles. Mettre ce drapeau à 1 exige la
// preuve complète — à réserver au nœud d'archive (deploy/73-node-archive.sh), sans quoi on
// obtiendrait un rouge sur une chaîne pourtant saine, et une barrière qui crie à tort finit
// désarmée.
const EXIGER_SOLDES = process.env.EXIGER_SOLDES_BLOC0 === '1';
const ROOT = path.join(__dirname, '..');
const CONFIG = JSON.parse(fs.readFileSync(path.join(ROOT, 'coinbosa.config.json'), 'utf8'));
const ADDRS = JSON.parse(fs.readFileSync(path.join(ROOT, 'genesis', 'distribution-addresses.json'), 'utf8'));
const REF = JSON.parse(fs.readFileSync(path.join(ROOT, 'genesis', 'genesis-reference.json'), 'utf8'));
const VALSET = '0x0000000000000000000000000000000000001000';
const WEI = 10n ** 18n;
const DURCI = 0x80000000;                 // marqueur BIP-32 d'un index durci (i')

const arret = (...lignes) => { lignes.forEach((l) => console.error(l)); process.exit(1); };
// Ce que ces deux aides évitent : prendre une réponse NORMALE du nœud pour une panne
// inconnue. En lot (ethers groupe les appels), une erreur JSON-RPC ressort sous le
// libellé opaque « could not coalesce error » et `shortMessage` ne contient plus rien
// d'exploitable ; le message réel du nœud vit dans e.error.message. On regarde donc TOUT
// ce qui est disponible avant de décider, et on affiche le message du nœud, pas l'emballage.
const texteErreur = (e) => [e && e.shortMessage, e && e.message, e && e.error && e.error.message,
  e && e.info && e.info.error && e.info.error.message].filter(Boolean).join(' | ');
const messageNoeud = (e) => (e && e.error && e.error.message)
  || (e && e.info && e.info.error && e.info.error.message) || (e && e.shortMessage) || (e && e.message) || '';
const bosa = (w) => (w / WEI).toLocaleString('fr-FR');

if (!XPUB) {
  arret("XPUB manquant.\n  read -rsp 'xpub : ' XPUB && echo && XPUB=\"$XPUB\" node scripts/check-treasury-derivation.js");
}
// Même garde que derive-treasury-addresses.js : refuser tout ce qui ressemble à un secret.
if (/^(0x)?[0-9a-fA-F]{64}$/.test(XPUB.trim()) || XPUB.trim().split(/\s+/).length >= 12) {
  arret('ARRÊT : ceci ressemble à une CLÉ PRIVÉE ou à une PHRASE DE RÉCUPÉRATION.',
        "  Ce contrôle n'a besoin que de la clé PUBLIQUE étendue (xpub…).");
}

let noeud;
try { noeud = ethers.HDNodeWallet.fromExtendedKey(XPUB.trim()); }
catch (e) { arret('xpub illisible : ' + e.message); }
if (noeud.privateKey) arret('ARRÊT : clé étendue PRIVÉE fournie. Fournir le xpub (neutre).');

// ── Le chemin annoncé doit être le chemin réellement emprunté ────────────────────────────
// Ce que cette garde attrape : un xpub exporté au MAUVAIS NIVEAU. `fromExtendedKey` accepte
// n'importe quel nœud ; sur une clé de niveau adresse (profondeur 5), `derivePath('0/i')`
// dérive en réalité m/44'/60'/0'/0/0/0/i — et l'ancien script imprimait quand même
// « chemin m/44'/60'/0'/0/i ». Il certifiait donc un chemin qu'il n'avait pas contrôlé.
// L'accident évité : une trésorerie déclarée conforme à un chemin où la restauration décrite
// dans GARDE-TRESORERIE.md ne retrouverait rien. Sur une clé importée, `path` vaut null :
// profondeur et index sont les seuls témoins disponibles.
if (noeud.depth !== 3 || noeud.index < DURCI) {
  arret(`ARRÊT : profondeur ${noeud.depth}, index 0x${noeud.index.toString(16)} — ce n'est pas un xpub de COMPTE BIP-44.`,
        "  Attendu : le nœud de compte m/44'/60'/0' (profondeur 3, index durci).",
        `  Fourni  : ${noeud.depth === 5 ? 'une clé de niveau adresse' : noeud.depth === 4 ? 'une clé de niveau chaîne (0 externe)' : noeud.depth === 0 ? 'la clé maître' : 'un autre niveau'}.`,
        "  À faire : sur l'appareil (Ledger/Trezor), exporter la clé publique étendue du COMPTE",
        "  Ethereum m/44'/60'/0' — pas celle d'un autre niveau, sans quoi les adresses dérivées",
        '  ici ne sont pas celles que la procédure de restauration retrouvera.');
}
const COMPTE = noeud.index - DURCI;
const CHEMIN = `m/44'/60'/${COMPTE}'/0/i`;

// ── La liste faisant autorité vient de la configuration, pas du fichier audité ───────────
// Ce que cette garde attrape : un poste effacé, renommé ou ajouté dans
// genesis/distribution-addresses.json. L'ancien script construisait sa liste à partir de ce
// fichier PUIS filtrait les adresses nulles : remettre `reserveStrategique` à 0x000…0 le
// faisait disparaître du tableau ET du dénominateur, et le rapport affichait « 12/12 » —
// un sans-faute apparent sur un poste de 21 000 000 BOSA non vérifié. Le dénominateur ne
// doit jamais être fourni par la pièce examinée.
const postes = Object.keys(CONFIG.distribution).filter((k) => !k.startsWith('$'));
if (postes.length === 0) {
  arret('ÉCHEC : aucun poste de répartition dans coinbosa.config.json → distribution.',
        "  Un effectif nul ne vaut PAS conformité : il n'y aurait rien à vérifier.");
}
const pctTotal = postes.reduce((s, k) => s + CONFIG.distribution[k], 0);
if (pctTotal !== 100) {
  arret(`ÉCHEC : la répartition de coinbosa.config.json totalise ${pctTotal} %, 100 attendu.`,
        '  Les parts attendues au bloc 0 seraient calculées sur une base fausse : on ne conclut pas.');
}

// ── L'index de migration, calqué sur le générateur ───────────────────────────────────────
// Ce que cette garde attrape : deux modèles d'index divergents. derive-treasury-addresses.js
// n'ajoute `__migration__` aux cibles que si migration.reserve > 0, et place le gouverneur
// juste après. L'ancien contrôle excluait `__migration__` SANS CONDITION : il annonçait
// « les 14 adresses descendent du même nœud » après n'en avoir dérivé que 13, et le jour où
// la réserve deviendra non nulle il chercherait le gouverneur en 0/13 alors qu'il sera en
// 0/14 — criant alors sur une trésorerie correcte. On reprend donc le modèle du générateur.
const RESERVE = BigInt(CONFIG.migration.reserve);
const cibles = postes.concat(RESERVE > 0n ? ['__migration__'] : []);
const PROJET = BigInt(CONFIG.projectAllocation.amount);
// Même contrôle de cohérence que build-genesis.js : réserve + allocation projet = offre
// totale. Ce que cette garde attrape : une base de calcul fausse. Sans elle, les parts
// « attendues » comparées aux soldes du bloc 0 seraient calculées sur un total qui n'est
// pas celui de l'offre — et un écart réel pourrait passer pour une conformité.
if (RESERVE + PROJET !== BigInt(CONFIG.nativeCoin.totalSupply)) {
  arret(`ÉCHEC : réserve (${RESERVE}) + allocation projet (${PROJET}) ≠ offre totale (${CONFIG.nativeCoin.totalSupply}).`,
        '  coinbosa.config.json est incohérent : les parts attendues au bloc 0 ne peuvent pas',
        '  être calculées. À faire : corriger la configuration (build-genesis.js refuse déjà de produire).');
}
const attenduWei = (poste) => (poste === '__migration__'
  ? RESERVE * WEI
  : (PROJET * BigInt(CONFIG.distribution[poste]) / 100n) * WEI);

// Couverture : chaque cible doit être présente ET renseignée. Une adresse nulle n'est pas un
// filtre, c'est un poste NON RENSEIGNÉ — le fichier le dit lui-même dans son $comment.
for (const cible of cibles) {
  if (!(cible in ADDRS)) {
    arret(`ÉCHEC : poste absent de genesis/distribution-addresses.json : « ${cible} ».`,
          '  Ce poste est déclaré dans coinbosa.config.json et reçoit une part de l\'offre au bloc 0.',
          '  À faire : renseigner son adresse (XPUB=… node scripts/derive-treasury-addresses.js) avant de conclure.');
  }
  if (ADDRS[cible] === ethers.ZeroAddress) {
    arret(`ÉCHEC : poste non renseigné (adresse nulle) : « ${cible} » — part attendue ${bosa(attenduWei(cible))} BOSA.`,
          '  Une adresse nulle ne se filtre pas silencieusement : elle signifie que la part de ce poste',
          "  n'est rattachée à aucun détenteur vérifiable. À faire : renseigner l'adresse réelle.");
  }
  try { ethers.getAddress(ADDRS[cible]); }
  catch { arret(`ÉCHEC : adresse illisible pour « ${cible} » : ${JSON.stringify(ADDRS[cible])}.`,
                '  À faire : corriger genesis/distribution-addresses.json (adresse 0x… sur 20 octets).'); }
}
// Réserve nulle : l'adresse de migration ne doit rien porter. build-genesis.js ne l'alloue
// pas (ligne « if (MIGRATION_RESERVE > 0n) ») ; une adresse renseignée ici serait un reliquat
// que personne ne vérifie, ou une adresse glissée hors du modèle d'index.
if (RESERVE === 0n && ADDRS.__migration__ && ADDRS.__migration__ !== ethers.ZeroAddress) {
  arret(`ÉCHEC : __migration__ renseigné (${ADDRS.__migration__}) alors que migration.reserve vaut 0.`,
        "  Aucune part n'est allouée à cette adresse au bloc 0 : elle n'est donc rattachée à rien,",
        '  et elle ne fait pas partie des index dérivés. À faire : la remettre à la valeur nulle,',
        '  ou porter migration.reserve à sa valeur réelle si une réserve doit exister.');
}
// Clé étrangère : ce que cette garde attrape, c'est le RENOMMAGE. Rebaptiser un poste dans le
// fichier d'adresses changeait l'étiquette affichée en face d'une adresse sans la moindre
// plainte — et le poste d'origine sortait du contrôle par la porte de derrière.
const connus = new Set(cibles.concat(['__migration__']));
for (const k of Object.keys(ADDRS)) {
  if (k.startsWith('$') || connus.has(k)) continue;
  arret(`ÉCHEC : clé inconnue dans genesis/distribution-addresses.json : « ${k} ».`,
        '  Elle ne correspond à aucun poste de coinbosa.config.json → distribution.',
        '  À faire : soit la retirer, soit déclarer le poste dans la configuration (il serait alors',
        "  alloué au bloc 0 par build-genesis.js). Un poste renommé ici sort sinon du contrôle.");
}
// La référence publiée doit exister : sans empreinte figée, rien n'ancre la chaîne branchée.
if (!REF.hash || /^0x0*$/.test(REF.hash) || !REF.stateRoot || /^0x0*$/.test(REF.stateRoot)) {
  arret('ÉCHEC : genesis/genesis-reference.json ne contient pas d\'empreinte figée (hash / stateRoot).',
        '  Sans elle, aucune conclusion : le nœud interrogé pourrait être n\'importe quelle chaîne.',
        '  À faire : figer l\'empreinte du bloc 0 au gel du genesis de production, puis relancer.');
}

(async () => {
  const p = new ethers.JsonRpcProvider(RPC, undefined, { staticNetwork: true });

  // ── [1] Identité du réseau ─────────────────────────────────────────────────────────────
  // Un chainId est un nombre AUTO-DÉCLARÉ : n'importe quel processus peut répondre 26262.
  // Il reste utile comme premier filtre (on n'interroge pas la mauvaise chaîne par erreur),
  // mais il ne peut pas rester la seule preuve d'identité — c'est exactement ce qui rendait
  // le faux nœud indétectable.
  const reseau = await p.getNetwork();
  if (reseau.chainId !== BigInt(CONFIG.network.chainId)) {
    arret(`ÉCHEC : chainId ${reseau.chainId}, attendu ${CONFIG.network.chainId} (coinbosa.config.json).`,
          `  Le RPC interrogé (${RPC}) n'est pas Coinbosa Chain. À faire : corriger RPC=…`);
  }
  if (REF.chainId && reseau.chainId !== BigInt(REF.chainId)) {
    arret(`ÉCHEC : chainId ${reseau.chainId} ≠ ${REF.chainId} figé dans genesis-reference.json.`,
          '  La configuration et la référence publiée se contredisent : ne rien conclure.');
  }

  // ── [1 bis] Le bloc 0 servi doit être CELUI QUI EST PUBLIÉ ─────────────────────────────
  // C'est l'ancrage qui manquait. Le stateRoot engage TOUT l'état initial : le comparer à la
  // référence figée écarte une chaîne substituée, un genesis de développement, un nœud qui
  // n'a jamais été initialisé — et le faux nœud qui ne savait répondre qu'à eth_chainId.
  let b0;
  try { b0 = await p.send('eth_getBlockByNumber', ['0x0', false]); }
  catch (e) { arret('ÉCHEC : le nœud a refusé eth_getBlockByNumber(0x0) : ' + messageNoeud(e),
                    '  Sans le bloc 0, aucune adresse ne peut être rattachée à la chaîne. À faire :',
                    '  interroger un nœud Coinbosa complet (deploy/30-node.sh) et relancer.'); }
  if (!b0 || !b0.hash) {
    arret('ÉCHEC : le nœud ne renvoie pas le bloc 0.',
          "  Ce n'est pas une chaîne Coinbosa exploitable : rien ne peut être ancré.",
          '  À faire : vérifier RPC=… et que le nœud a bien été initialisé (geth init).');
  }
  const eq = (a, b) => String(a).toLowerCase() === String(b).toLowerCase();
  if (!eq(b0.hash, REF.hash) || !eq(b0.stateRoot, REF.stateRoot)) {
    arret("ÉCHEC : le bloc 0 servi n'est pas celui qui est publié.",
          `    hash observé    : ${b0.hash}`,
          `    hash publié     : ${REF.hash}`,
          `    stateRoot observé : ${b0.stateRoot}`,
          `    stateRoot publié  : ${REF.stateRoot}`,
          "  Trois causes possibles, à trancher AVANT toute conclusion sur la trésorerie :",
          '   1. le RPC pointe une chaîne de DÉVELOPPEMENT (genesis-coinbosa-dev.json) — auquel cas',
          '      ce contrôle est sans objet : les adresses de dév sont synthétiques, non dérivées',
          "      d'un xpub, et il n'y a aucune filiation à prouver ;",
          '   2. la chaîne branchée a été substituée, ou son genesis modifié (allocation ajoutée) ;',
          '   3. la référence publiée est périmée.',
          '  À faire : lancer scripts/check-genesis-hash.js contre ce même RPC pour instruire le point.');
  }

  // ── [2] Le gouverneur, lu dans le bytecode figé du bloc 0 ──────────────────────────────
  // Le contrat système doit exister : sans code à 0x…1000, l'appel GOVERNOR() ne renvoie rien
  // d'exploitable — et un faux nœud répondait n'importe quoi à sa place.
  const code = await p.getCode(VALSET);
  if (!code || code === '0x') {
    arret(`ÉCHEC : aucun code au contrat système ${VALSET} sur ce nœud.`,
          "  Le gouverneur est une constante de ce bytecode : sans lui, il n'y a rien à lire.",
          "  Ce n'est pas une chaîne Coinbosa. À faire : vérifier RPC=…");
  }
  let gouverneur;
  try { gouverneur = ethers.getAddress(await new ethers.Contract(VALSET, ['function GOVERNOR() view returns (address)'], p).GOVERNOR()); }
  catch (e) { arret('ÉCHEC : GOVERNOR() illisible sur le contrat système : ' + messageNoeud(e),
                    '  À faire : vérifier que le nœud sert bien Coinbosa Chain et non un autre réseau.'); }
  // Le gouverneur lu doit être celui de la référence publiée : sinon la trésorerie prouvée
  // ici et le consensus réellement en place ne sont pas ceux qui ont été annoncés.
  if (REF.gouverneur && !eq(gouverneur, REF.gouverneur)) {
    arret(`ÉCHEC : gouverneur ${gouverneur} sur la chaîne, ${REF.gouverneur} publié dans genesis-reference.json.`,
          '  GOVERNOR est une constante du bytecode du bloc 0 : cet écart signifie que le contrat',
          "  système n'est pas celui qui a été publié. À faire : ne rien conclure de rassurant, et",
          '  instruire avec scripts/check-genesis-hash.js et scripts/check-custody.js.');
  }

  // ── [3] Les soldes AU BLOC 0 : une adresse dérivée mais vide n'est pas une trésorerie ───
  // Sonde préalable : le nœud sert-il l'état du bloc 0 ? Le nœud public tourne en
  // `--gcmode full`, son état de genèse est élagué — condition NORMALE, pas une anomalie.
  // On ne se tait pas pour autant : l'impossibilité est CONSTATÉE (le nœud répond une erreur
  // d'état historique), affichée, et le verdict final est dégradé en PREUVE PARTIELLE.
  // Ce qui reste interdit, c'est de passer sous silence une part non vérifiée.
  let soldesLisibles = true;
  let motifElagage = '';
  try {
    await p.getBalance(VALSET, 0);
  } catch (e) {
    if (/historical state|missing trie node|not available|state is not available|pruned|no state/i.test(texteErreur(e))) {
      soldesLisibles = false;
      motifElagage = messageNoeud(e);
    } else {
      arret('ÉCHEC : lecture de solde au bloc 0 impossible pour une raison inattendue : ' + messageNoeud(e),
            "  Ce n'est pas un élagage d'état connu. On ne conclut pas sur un nœud dont on ne",
            '  comprend pas la réponse. À faire : instruire la réponse du RPC, puis relancer.');
    }
  }

  console.log(`\n  Filiation des adresses — chemin ${CHEMIN} (xpub de compte, profondeur ${noeud.depth})`);
  console.log(`  RPC ${RPC} — bloc 0 conforme à genesis-reference.json (figé le ${REF.fige_le || '?'})`);
  console.log('  ' + '='.repeat(108));

  let conformes = 0, ecarts = 0, soldesEcarts = 0;
  const vues = new Map();
  for (let i = 0; i < cibles.length; i++) {
    const poste = cibles[i];
    const derivee = ethers.getAddress(noeud.derivePath(`0/${i}`).address);
    const inscrite = ethers.getAddress(ADDRS[poste]);
    const ok = derivee === inscrite;
    ok ? conformes++ : ecarts++;
    // Collision : deux postes inscrits sur la MÊME adresse fusionneraient leurs soldes sans
    // alerte — la séparation comptable disparaît alors que le total reste juste, et le
    // rapport de garde attribuerait à un poste des fonds qui appartiennent à deux. Même
    // garde que build-genesis.js (`seen`) et derive-treasury-addresses.js (`vues`).
    if (vues.has(inscrite)) {
      arret(`ÉCHEC : collision d'adresse — « ${poste} » et « ${vues.get(inscrite)} » portent la même adresse ${inscrite}.`,
            '  Leurs soldes fusionnent : la séparation comptable disparaît alors que le total reste',
            "  juste. À faire : donner une adresse distincte à chaque poste (une par index 0/i).");
    }
    vues.set(inscrite, poste);

    let colonneSolde = 'non lisible';
    if (soldesLisibles) {
      const attendu = attenduWei(poste);
      const solde = await p.getBalance(inscrite, 0);
      if (solde === attendu) {
        colonneSolde = bosa(solde).padStart(15) + ' BOSA';
      } else {
        soldesEcarts++;
        colonneSolde = `ÉCART solde : ${bosa(solde)} au lieu de ${bosa(attendu)} BOSA`;
      }
    }
    console.log(`  0/${String(i).padEnd(2)}  ${poste.padEnd(26)} ${inscrite}  ${(ok ? 'conforme' : 'ÉCART -> ' + derivee).padEnd(52)} ${colonneSolde}`);
  }

  // Le gouverneur occupe l'index qui suit les cibles — exactement comme dans le générateur.
  const govDerive = ethers.getAddress(noeud.derivePath(`0/${cibles.length}`).address);
  const govOk = govDerive === gouverneur;
  console.log('  ' + '-'.repeat(108));
  console.log(`  0/${String(cibles.length).padEnd(2)}  ${'GOUVERNEUR (sur la chaîne)'.padEnd(26)} ${gouverneur}  ${govOk ? 'conforme' : 'ÉCART -> ' + govDerive}`);
  console.log('  ' + '='.repeat(108));
  console.log(`\n  postes conformes : ${conformes}/${cibles.length}   écarts de filiation : ${ecarts}   gouverneur : ${govOk ? 'conforme' : 'ÉCART'}`);
  console.log(`  soldes au bloc 0 : ${soldesLisibles ? (soldesEcarts ? soldesEcarts + ' ÉCART(S)' : cibles.length + '/' + cibles.length + ' conformes') : 'NON VÉRIFIÉS (état du bloc 0 élagué sur ce nœud)'}`);
  if (COMPTE !== 0) {
    console.log(`\n  ⚠  Le xpub fourni est celui du compte ${COMPTE}', alors que GARDE-TRESORERIE.md décrit`);
    console.log(`     le compte 0'. La filiation ci-dessus vaut pour ${CHEMIN} : corriger le document.`);
  }

  if (ecarts || !govOk || soldesEcarts) {
    console.error("\n  RÉSULTAT : la filiation n'est PAS celle que décrit la procédure du dépôt.");
    if (ecarts) console.error(`  ${ecarts} adresse(s) inscrite(s) ne descendent pas du xpub fourni.`);
    if (!govOk) console.error(`  Le gouverneur de la chaîne (${gouverneur}) ne descend pas de ce xpub (attendu ${govDerive}).`);
    if (soldesEcarts) console.error(`  ${soldesEcarts} poste(s) ne détiennent pas au bloc 0 la part que leur donne coinbosa.config.json.`);
    console.error('  Ne rien conclure de rassurant : établir d\'où viennent réellement ces adresses,');
    console.error('  et corriger GARDE-TRESORERIE.md en conséquence.\n');
    process.exit(1);
  }

  if (!soldesLisibles) {
    // Verdict volontairement dégradé : ce qui n'a pas été vérifié est NOMMÉ, et la sortie
    // dit comment obtenir la preuve complète. Une sortie 0 ne doit jamais laisser croire
    // qu'on a prouvé plus que ce qu'on a lu.
    console.log(`\n  PREUVE PARTIELLE — l'état du bloc 0 n'est pas servi par ce nœud (« ${motifElagage.slice(0, 70)} »).`);
    console.log('  PROUVÉ ICI  : les ' + (cibles.length + 1) + ' adresses (postes + gouverneur) descendent du MÊME nœud de compte,');
    console.log('                donc de la MÊME graine ; le bloc 0 servi est celui qui est publié.');
    console.log('  NON PROUVÉ  : que chaque poste DÉTIENNE au bloc 0 la part qui lui revient.');
    console.log('  Pour la preuve complète, relancer contre un nœud d\'archive (deploy/73-node-archive.sh) :');
    console.log('    RPC=http://127.0.0.1:8547 EXIGER_SOLDES_BLOC0=1 XPUB="$XPUB" node scripts/check-treasury-derivation.js');
    console.log('  À défaut : scripts/check-custody.js réconcilie les soldes COURANTS au wei près.');
    if (EXIGER_SOLDES) {
      console.error('\n  ÉCHEC : EXIGER_SOLDES_BLOC0=1 demandait la preuve complète, ce nœud ne peut pas la fournir.\n');
      process.exit(1);
    }
    console.log('');
    process.exit(0);
  }

  console.log('\n  RÉSULTAT : les ' + (cibles.length + 1) + ' adresses descendent du MÊME nœud de compte,');
  console.log('  donc de la MÊME graine. La trésorerie et le gouverneur tombent ensemble.');
  console.log('  C\'est le fait à divulguer, pas à taire — GARDE-TRESORERIE.md le fait.');
  console.log(`  Et chacune détient au bloc 0 exactement la part que lui donne coinbosa.config.json.\n`);
  process.exit(0);
})().catch((e) => {
  // Filet : une erreur imprévue ne doit jamais ressembler à un succès silencieux.
  console.error('ÉCHEC : ' + messageNoeud(e));
  console.error('  Aucune conclusion n\'a pu être établie — ne pas présenter cette sortie comme une vérification.');
  process.exit(1);
});
