// Vérifie que la chaîne franchit un bloc d'epoch.
//
// C'est le contrôle de non-régression le plus important du dépôt. Tous les 200
// blocs, Parlia interroge getMiningValidators() sur le contrat système 0x…1000
// pour reconstruire l'extraData. Le contrat hérité de BNB Chain n'expose pas
// cette fonction : la chaîne se fige alors définitivement, en bouclant sur
// « Failed to prepare header for sealing ». CoinbosaValidatorSet corrige ce
// défaut, et ce script s'assure qu'on ne le réintroduit pas.
const { ethers } = require('ethers');
const fs = require('fs');
const path = require('path');

const RPC = process.env.RPC || 'http://127.0.0.1:8545';
const config = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'coinbosa.config.json'), 'utf8'));
const EPOCH = config.network.epochLength;
const VALSET = '0x0000000000000000000000000000000000001000';

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

(async () => {
  const provider = new ethers.JsonRpcProvider(RPC);
  const target = EPOCH + 2;

  console.log(`  cible : bloc ${target} (premier epoch à ${EPOCH})`);
  let last = -1, stalls = 0;

  while (true) {
    const head = await provider.getBlockNumber();
    if (head >= target) break;

    if (head === last) {
      if (++stalls >= 12) {
        console.error(`\nECHEC : la chaîne est figée au bloc ${head}.`);
        if (head === EPOCH - 1) {
          console.error(`C'est exactement le symptôme du contrat système défaillant :`);
          console.error(`getMiningValidators() sur ${VALSET} doit répondre sans revert.`);
        }
        process.exit(1);
      }
    } else {
      stalls = 0;
      if (head % 50 === 0 || head > EPOCH - 5) console.log(`  bloc ${head}…`);
      last = head;
    }
    await sleep(5000);
  }

  // le bloc d'epoch doit porter l'extraData étendu : 32 vanity + 1 compteur
  // + N × (20 adresse + 48 clé BLS) + 65 sceau
  const epochBlock = await provider.getBlock(EPOCH);
  const extraLen = (epochBlock.extraData.length - 2) / 2;
  const count = parseInt(epochBlock.extraData.slice(66, 68), 16);
  const expected = 32 + 1 + count * 68 + 65;

  console.log(`\n  bloc ${EPOCH} scellé par ${epochBlock.miner}`);
  console.log(`  extraData : ${extraLen} octets, ${count} validateur(s)`);

  if (extraLen !== expected) {
    console.error(`ECHEC : extraData de ${extraLen} octets, ${expected} attendus.`);
    process.exit(1);
  }

  // et le contrat système doit répondre
  const valset = new ethers.Contract(
    VALSET,
    ['function getMiningValidators() view returns (address[], bytes[])'],
    provider
  );
  const [vals] = await valset.getMiningValidators();
  console.log(`  getMiningValidators() : ${vals.length} validateur(s) — ${vals.join(', ')}`);

  if (!vals.length) { console.error('ECHEC : set de validateurs vide.'); process.exit(1); }

  console.log(`\n  bloc d'epoch franchi, chaîne à ${await provider.getBlockNumber()}`);
})().catch((e) => { console.error('ERREUR :', e.message); process.exit(1); });
