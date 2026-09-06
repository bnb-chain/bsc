#!/usr/bin/env node
// =============================================================================
// COINBOSA — l'argent est-il toujours là, et rien de plus ?
//
//   RPC=https://explorer.coinbosa.com/rpc node scripts/audit-argent.js
//   node scripts/audit-argent.js --json      (sortie machine)
//
// Code de sortie : 0 si tous les invariants tiennent, 1 sinon.
//
// POURQUOI CE SCRIPT EXISTE
// -------------------------
// La nuit du 7 septembre 2026, un recomptage manuel des soldes contre le genesis
// a trouvé ce qu'aucune barrière ne cherchait : le poste `equipe` portait le
// nonce 2 alors que trois documents — dont le DOSSIER DE COTATION, lu par les
// équipes conformité — affirmaient « une seule transaction dans l'histoire de la
// chaîne ». Une seconde transaction de 100 BOSA existait depuis deux jours et
// n'était consignée nulle part.
//
// Rien n'était volé. Mais personne ne l'avait vu, et c'est cela le défaut : un
// mouvement d'argent sur cette chaîne pouvait passer inaperçu. Ce script fait de
// ce recomptage un contrôle qu'on relance, plutôt qu'une vérification qu'on a
// faite une fois.
//
// LES CINQ INVARIANTS, ET CE QUE CHACUN ATTRAPE
// ----------------------------------------------
//  1. IDENTITÉ — le bloc 0 de la chaîne interrogée est celui du dépôt.
//     Attrape : parler à la mauvaise chaîne, et rassurer sur la mauvaise chaîne.
//  2. ALLOCATION — la somme du genesis vaut exactement l'offre déclarée.
//     Attrape : une allocation modifiée dans le dépôt.
//  3. CONSERVATION — la somme de TOUT ce qu'on sait compter vaut l'offre.
//     Attrape : la création de monnaie, la destruction, et surtout les BOSA
//     détenus par une adresse que personne n'a déclarée.
//  4. COMPTABILITÉ DU CONTRAT SYSTÈME — ce qu'il détient est exactement ce qu'il
//     doit. Attrape : des frais bloqués, une créance perdue, un surplus qui
//     s'accumule sans que personne ne le sache.
//  5. MOUVEMENTS — les nonces des comptes de trésorerie sont ceux qu'on attend.
//     Attrape : un transfert qu'aucun document ne mentionne. C'est l'invariant
//     qui aurait crié le 5 septembre.
//
// CE QU'IL NE PEUT PAS FAIRE, ET IL FAUT LE DIRE
// -----------------------------------------------
// JSON-RPC ne permet pas d'énumérer les comptes d'une chaîne. Ce script ne peut
// donc pas dresser la liste des détenteurs : il mesure ce qu'il SAIT compter et
// annonce l'écart. Un écart non nul veut dire « des BOSA sont ailleurs », pas
// « des BOSA ont été créés » — la distinction compte, et le rapport la fait.
//
// Les adresses hors genesis connues et légitimes se déclarent dans
// genesis/comptes-connus.json. Déclarer une adresse n'est pas la blanchir : cela
// dit « on sait pourquoi elle détient des BOSA », et le fichier porte la raison.
// =============================================================================

const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

const RACINE = path.join(__dirname, '..');
const RPC = process.env.RPC || 'https://explorer.coinbosa.com/rpc';
const JSON_SEUL = process.argv.includes('--json');
const SYS = '0x0000000000000000000000000000000000001000';
const OFFRE = 700000000n * 10n ** 18n;

const echecs = [];
const notes = [];
let dernierTitre = null;

function titre(t) { dernierTitre = t; if (!JSON_SEUL) console.log(`\n  ${t}\n  ${'-'.repeat(t.length)}`); }
function ok(m)     { if (!JSON_SEUL) console.log(`    \x1b[32mOK\x1b[0m    ${m}`); }
function ko(m)     { echecs.push(`[${dernierTitre}] ${m}`); if (!JSON_SEUL) console.log(`    \x1b[31mECHEC\x1b[0m ${m}`); }
function note(m)   { notes.push(m); if (!JSON_SEUL) console.log(`          ${m}`); }
const bosa = (w) => ethers.formatEther(w);
const aligne = (w) => bosa(w).padStart(24);

function lireJSON(p, defaut) {
  try { return JSON.parse(fs.readFileSync(p, 'utf8')); }
  catch (e) { if (defaut !== undefined) return defaut; throw new Error(`${p} illisible : ${e.message}`); }
}

(async () => {
  const provider = new ethers.JsonRpcProvider(RPC);
  const ref = lireJSON(path.join(RACINE, 'genesis/genesis-reference.json'));
  const gen = lireJSON(path.join(RACINE, 'genesis/genesis-coinbosa.json'));
  const connus = lireJSON(path.join(RACINE, 'genesis/comptes-connus.json'), { comptes: [] });

  if (!JSON_SEUL) {
    console.log('\n  AUDIT DE L\'ARGENT — Coinbosa Chain');
    console.log('  ' + '='.repeat(72));
    console.log(`  RPC : ${RPC}`);
  }

  // --- 1. IDENTITÉ ----------------------------------------------------------
  titre('1. Identité de la chaîne');
  const b0 = await provider.getBlock(0);
  if (!b0) { ko('le bloc 0 est illisible — audit interrompu'); throw new Error('bloc 0 illisible'); }
  b0.hash === ref.hash
    ? ok(`bloc 0 : ${b0.hash.slice(0, 18)}… identique à la référence du dépôt`)
    : ko(`bloc 0 : mesuré ${b0.hash}, référence ${ref.hash} — CE N'EST PAS LA MÊME CHAÎNE`);
  b0.stateRoot === ref.stateRoot
    ? ok('stateRoot identique à la référence')
    : ko(`stateRoot : mesuré ${b0.stateRoot}, référence ${ref.stateRoot}`);
  const idChaine = Number((await provider.getNetwork()).chainId);
  idChaine === ref.chainId
    ? ok(`chainId ${idChaine}`)
    : ko(`chainId ${idChaine}, la référence dit ${ref.chainId}`);

  // --- 2. ALLOCATION --------------------------------------------------------
  titre('2. Allocation du genesis');
  const alloc = Object.entries(gen.alloc || {})
    .filter(([, v]) => v && v.balance && BigInt(v.balance) > 0n)
    .map(([a, v]) => ({ adresse: ethers.getAddress(a), genesis: BigInt(v.balance) }));
  const sommeAlloc = alloc.reduce((s, c) => s + c.genesis, 0n);
  sommeAlloc === OFFRE
    ? ok(`${alloc.length} comptes alloués, somme = ${bosa(OFFRE)} BOSA, exacte au wei`)
    : ko(`somme allouée ${bosa(sommeAlloc)} BOSA, offre déclarée ${bosa(OFFRE)} — écart ${bosa(sommeAlloc - OFFRE)}`);

  // --- 3. CONSERVATION ------------------------------------------------------
  titre('3. Conservation — où est l\'argent aujourd\'hui');
  const externes = (connus.comptes || []).map((c) => ({
    adresse: ethers.getAddress(c.adresse), raison: c.raison || '(sans raison déclarée)',
  }));
  const aMesurer = [
    ...alloc.map((c) => ({ ...c, genre: 'genesis' })),
    ...externes.map((c) => ({ ...c, genre: 'declare', genesis: 0n })),
    { adresse: ethers.getAddress(SYS), genre: 'systeme', genesis: 0n, raison: 'contrat système — frais' },
  ];

  let compte = 0n;
  const mouvements = [];
  for (const c of aMesurer) {
    const [solde, nonce] = await Promise.all([
      provider.getBalance(c.adresse), provider.getTransactionCount(c.adresse),
    ]);
    c.solde = solde; c.nonce = nonce;
    compte += solde;
    if (c.genre === 'genesis' && (nonce !== 0 || solde !== c.genesis)) mouvements.push(c);
  }

  const ecart = OFFRE - compte;
  if (!JSON_SEUL) {
    for (const c of aMesurer.filter((x) => x.solde > 0n || x.genre !== 'declare')) {
      const marque = c.genre === 'genesis' ? ' ' : c.genre === 'systeme' ? '·' : '+';
      console.log(`      ${marque} ${c.adresse}  ${aligne(c.solde)}  nonce=${c.nonce}${c.raison ? '   ' + c.raison : ''}`);
    }
  }
  if (ecart === 0n) {
    ok(`total compté = ${bosa(compte)} BOSA — l'offre est intégralement localisée`);
  } else if (ecart > 0n) {
    ko(`${bosa(ecart)} BOSA ne sont dans AUCUN compte connu de cet audit`);
    note('Ce n\'est pas une création de monnaie : c\'est de l\'argent détenu par une adresse');
    note('que personne n\'a déclarée. Identifier le destinataire, puis l\'ajouter à');
    note('genesis/comptes-connus.json avec la raison — ou expliquer pourquoi il est là.');
  } else {
    ko(`le total compté DÉPASSE l'offre de ${bosa(-ecart)} BOSA — création de monnaie`);
    note('C\'est le pire cas possible sur cette chaîne. L\'offre est censée être fixe.');
  }

  // --- 4. COMPTABILITÉ DU CONTRAT SYSTÈME -----------------------------------
  titre('4. Comptabilité du contrat système');
  const soldeSys = aMesurer.find((c) => c.genre === 'systeme').solde;
  let surplus = null;
  try {
    surplus = await new ethers.Contract(SYS, ['function surplus() view returns (uint256)'], provider).surplus();
  } catch (e) {
    ko(`surplus() illisible (${(e.shortMessage || e.message || '').slice(0, 60)}) — comptabilité NON vérifiée`);
  }
  // La créance du validateur vit dans le mapping du slot 3 (deposits).
  const validateur = ethers.getAddress(ref.validateur);
  const slot = ethers.keccak256(ethers.concat([ethers.zeroPadValue(validateur, 32), ethers.zeroPadValue('0x03', 32)]));
  const creance = BigInt(await provider.getStorage(SYS, slot));

  if (surplus !== null) {
    // Ce que le contrat détient doit être exactement ce qu'il doit, plus ce qui
    // n'est dû à personne. Un écart signifie des BOSA bloqués ou une créance perdue.
    const attendu = creance + surplus;
    attendu === soldeSys
      ? ok(`solde ${bosa(soldeSys)} = créances ${bosa(creance)} + surplus ${bosa(surplus)} — rien n'est bloqué ni perdu`)
      : ko(`solde ${bosa(soldeSys)} ≠ créances ${bosa(creance)} + surplus ${bosa(surplus)} — écart ${bosa(soldeSys - attendu)}`);
    if (surplus > 0n) note(`surplus non nul : ${bosa(surplus)} BOSA n'appartiennent à personne dans le contrat.`);
  }

  // --- 5. MOUVEMENTS --------------------------------------------------------
  titre('5. Mouvements des comptes de trésorerie');
  const attendus = connus.nonces_attendus || {};
  if (!mouvements.length) {
    ok('aucun compte du genesis n\'a bougé');
  } else {
    for (const m of mouvements) {
      const prevu = attendus[m.adresse];
      if (prevu !== undefined && prevu === m.nonce) {
        ok(`${m.adresse} : nonce ${m.nonce}, ${bosa(m.genesis - m.solde)} BOSA sortis — conforme à ce qui est déclaré`);
      } else {
        ko(`${m.adresse} : nonce ${m.nonce}${prevu !== undefined ? ` (${prevu} déclaré)` : ' (aucun nonce déclaré)'}, ` +
           `${bosa(m.genesis - m.solde)} BOSA sortis`);
        note('Un mouvement non déclaré. Retrouver la transaction, vérifier qu\'elle est');
        note('légitime, puis mettre à jour genesis/comptes-connus.json ET les documents');
        note('qui annoncent le nombre de mouvements (GARDE-TRESORERIE.md, DOSSIER-COTATION.md).');
      }
    }
  }

  // --- verdict ---------------------------------------------------------------
  const resultat = {
    rpc: RPC, chainId: idChaine, bloc0: b0.hash,
    offre: OFFRE.toString(), compte: compte.toString(), ecart: ecart.toString(),
    contratSysteme: { solde: soldeSys.toString(), creance: creance.toString(), surplus: surplus === null ? null : surplus.toString() },
    mouvements: mouvements.map((m) => ({ adresse: m.adresse, nonce: m.nonce, sortis: (m.genesis - m.solde).toString() })),
    echecs, notes,
  };
  if (JSON_SEUL) { console.log(JSON.stringify(resultat, null, 2)); process.exit(echecs.length ? 1 : 0); }

  console.log('\n  ' + '='.repeat(72));
  if (echecs.length) {
    console.log(`  ${echecs.length} INVARIANT(S) ROMPU(S) :`);
    echecs.forEach((e) => console.log(`    ✗ ${e}`));
    console.log('\n  VERDICT : NE PAS considérer la comptabilité comme vérifiée.\n');
    process.exit(1);
  }
  console.log('  VERDICT : les cinq invariants tiennent. L\'offre est intégralement');
  console.log('            localisée, rien n\'a été créé ni détruit, et aucun mouvement');
  console.log('            non déclaré n\'a eu lieu.\n');
})().catch((e) => { console.error('\nERREUR :', e.message); process.exit(1); });
