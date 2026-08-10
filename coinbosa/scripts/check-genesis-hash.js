// Vérifie que la chaîne branchée est bien CELLE QUI A ÉTÉ PUBLIÉE.
//
//   RPC=https://explorer.coinbosa.com/rpc node scripts/check-genesis-hash.js
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
const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

const RPC = process.env.RPC || 'http://127.0.0.1:8545';
const REF_FILE = process.env.GENESIS_REF || path.join(__dirname, '..', 'genesis', 'genesis-reference.json');
// En mode développement, l'empreinte varie à chaque génération (le validateur est un compte
// jeté) : on affiche la valeur observée au lieu d'exiger une correspondance.
const ALLOW_DEV = process.env.ALLOW_DEV_HASH === '1';

(async () => {
  const provider = new ethers.JsonRpcProvider(RPC);
  const b0 = await provider.send('eth_getBlockByNumber', ['0x0', false]);
  if (!b0) { console.error('ECHEC : le nœud ne renvoie pas le bloc 0.'); process.exit(1); }

  const observe = { hash: b0.hash, stateRoot: b0.stateRoot, extraData: b0.extraData, gasLimit: b0.gasLimit };
  console.log('  bloc 0 observé sur la chaîne :');
  console.log(`    hash       : ${observe.hash}`);
  console.log(`    stateRoot  : ${observe.stateRoot}`);
  console.log(`    gasLimit   : ${observe.gasLimit}`);

  let ref = null;
  if (fs.existsSync(REF_FILE)) {
    const parsed = JSON.parse(fs.readFileSync(REF_FILE, 'utf8'));
    if (parsed.hash && !/^0x0*$/.test(parsed.hash)) ref = parsed;
  }

  // ALLOW_DEV_HASH signifie « cette chaîne est un réseau de DÉVELOPPEMENT » : son genesis est
  // régénéré à chaque exécution avec un validateur jetable, donc son empreinte diffère par
  // construction de celle de la production. La comparer à la référence de production n'a
  // aucun sens et faisait échouer la CI en permanence — alors que la production, elle, est
  // conforme. On affiche les valeurs observées, sans jamais comparer.
  if (ALLOW_DEV) {
    console.log('\n  MODE DÉVELOPPEMENT (ALLOW_DEV_HASH=1) : empreinte affichée, NON comparée.');
    console.log('  Le genesis de développement est régénéré à chaque exécution avec un validateur');
    console.log('  jetable : son empreinte diffère par construction de celle de la production.');
    console.log('  Ce contrôle ne prouve donc RIEN ici — seule la production compte.');
    if (ref) console.log(`  (référence de production figée le ${ref.fige_le} : ${String(ref.hash).slice(0, 18)}…)`);
    return;
  }

  if (!ref) {
    const msg = `aucune empreinte de référence figée dans ${path.basename(REF_FILE)}`;
    if (false) {
      console.log(`\n  (${msg} — normal en développement, l'empreinte change à chaque génération)`);
      console.log('  Pour figer la production, copier les valeurs ci-dessus dans genesis-reference.json.');
      return;
    }
    console.error(`\nECHEC : ${msg}.`);
    console.error('  Sans référence figée, on ne peut PAS affirmer que la chaîne déployée est celle publiée,');
    console.error('  ni qu\'aucune allocation cachée n\'a été ajoutée au genesis.');
    console.error('  Figer l\'empreinte au moment du gel du genesis de production, puis relancer.');
    process.exit(1);
  }

  console.log(`\n  référence figée le ${ref.fige_le || '?'} (${path.basename(REF_FILE)}) :`);
  console.log(`    hash       : ${ref.hash}`);

  let ok = true;
  const cmp = (nom, attendu, obtenu) => {
    if (!attendu) return;
    if (String(attendu).toLowerCase() !== String(obtenu).toLowerCase()) {
      console.error(`\nECHEC : ${nom} divergent.\n    attendu : ${attendu}\n    obtenu  : ${obtenu}`);
      ok = false;
    }
  };
  cmp('hash du bloc 0', ref.hash, observe.hash);
  cmp('stateRoot du bloc 0', ref.stateRoot, observe.stateRoot);
  cmp('extraData du bloc 0', ref.extraData, observe.extraData);

  if (!ok) {
    console.error('\n  La chaîne branchée N\'EST PAS celle qui a été publiée : genesis modifié,');
    console.error('  allocation ajoutée, ou mauvais réseau. NE PAS considérer cette chaîne comme légitime.');
    process.exit(1);
  }
  console.log('\n  chaîne conforme à la référence publiée (aucune allocation cachée possible)');
})().catch((e) => { console.error('ERREUR :', e.message); process.exit(1); });
