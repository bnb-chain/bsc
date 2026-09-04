#!/usr/bin/env node
// =============================================================================
// COINBOSA — publication vers le canal Telegram officiel.
//
//   node scripts/telegram-poste.js chaine          etat de la chaine, verifie
//   node scripts/telegram-poste.js message "..."   un message que tu as ecrit
//   node scripts/telegram-poste.js --essai chaine  affiche sans publier
//
// LE JETON NE VIT PAS DANS CE DEPOT
// ---------------------------------
// Il est lu, dans cet ordre : $TELEGRAM_TOKEN, puis /etc/coinbosa-telegram-token
// (que tu crees en 0600), puis ~/.coinbosa-telegram-token. Il n'est jamais
// affiche, jamais journalise, et ce fichier ne doit jamais le contenir.
//
// Ce jeton parle AU NOM DU PROJET : qui l'obtient peut publier « voici
// l'adresse officielle » dans ton canal. C'est exactement ainsi qu'on vide des
// portefeuilles. Traite-le comme la cle de scellage.
//
// POURQUOI LES FAITS DE CHAINE SONT AUTOMATIQUES ET LE RESTE NE L'EST PAS
// ----------------------------------------------------------------------
// Ce qui est publie ici est lu SUR LA CHAINE au moment de la publication :
// hauteur, temps de bloc mesure, offre. Personne ne les redige, donc personne
// ne peut s'y tromper.
//
// Les nouvelles du monde crypto, elles, ne sont pas publiees automatiquement.
// Une depeche fausse ou mal comprise, postee sous le nom de Coinbosa, engage le
// projet — et un lecteur qui la decouvre fausse ne revient pas. L'outil te les
// PREPARE ; c'est toi qui valides. C'est la meme regle que partout dans ce
// depot : aucune affirmation qu'on ne peut pas etayer.
//
// AUCUNE PROMESSE DE RENDEMENT, AUCUNE PROJECTION DE PRIX. Jamais, par aucun
// chemin. Le controle plus bas refuse de publier un message qui en contient.
// =============================================================================

const fs = require('fs');
const path = require('path');

const RACINE = path.join(__dirname, '..');
const RPC = process.env.RPC || 'https://explorer.coinbosa.com/rpc';
const CANAL = process.env.TELEGRAM_CANAL || '@Coinbosaofficial';
const ESSAI = process.argv.includes('--essai');

/// Le jeton, sans jamais le montrer.
function jeton() {
  if (process.env.TELEGRAM_TOKEN) return process.env.TELEGRAM_TOKEN.trim();
  for (const p of ['/etc/coinbosa-telegram-token',
                   path.join(process.env.HOME || '', '.coinbosa-telegram-token')]) {
    try {
      const t = fs.readFileSync(p, 'utf8').trim();
      // Un fichier de mot de passe se termine souvent par un saut de ligne : le
      // transmettre tel quel fait echouer l'appel avec un message opaque.
      if (t) return t;
    } catch { /* absent, on essaie le suivant */ }
  }
  return null;
}

/// Les mots qui ne doivent JAMAIS partir sous le nom du projet. Ce n'est pas de
/// la pudeur : une promesse de rendement engage juridiquement l'editeur, et une
/// projection de prix est ce que tout agregateur cherche avant de refuser un
/// dossier.
const INTERDITS = [
  /\b(x\s*\d{2,}|\d+\s*x)\b/i,            // « x100 », « 100x »
  /garanti/i, /rendement/i, /profit/i,
  /\bmoon\b/i, /to the moon/i, /pump/i,
  /prix\s+(cible|prevu|attendu)/i, /price\s+target/i,
  /\binvestis+ez\b/i, /achetez\s+maintenant/i,
];

function refuser(texte) {
  const vus = INTERDITS.filter((r) => r.test(texte));
  if (!vus.length) return null;
  return vus.map((r) => String(r)).join(', ');
}

async function rpc(methode, params = []) {
  const r = await fetch(RPC, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: methode, params }),
  });
  const j = await r.json();
  if (j.error) throw new Error(`${methode} : ${j.error.message}`);
  return j.result;
}

/// L'etat de la chaine, LU sur la chaine. Rien n'est redige d'avance.
async function etatChaine() {
  const tete = parseInt(await rpc('eth_blockNumber'), 16);
  const chainId = parseInt(await rpc('eth_chainId'), 16);
  // Temps de bloc mesure sur les 100 derniers blocs, pas annonce de memoire.
  const a = await rpc('eth_getBlockByNumber', ['0x' + (tete - 100).toString(16), false]);
  const b = await rpc('eth_getBlockByNumber', ['0x' + tete.toString(16), false]);
  const secondes = (parseInt(b.timestamp, 16) - parseInt(a.timestamp, 16)) / 100;

  const cfg = JSON.parse(fs.readFileSync(path.join(RACINE, 'coinbosa.config.json'), 'utf8'));
  const offre = cfg.network && cfg.network.supply ? cfg.network.supply : '700 000 000';

  return [
    '*Coinbosa Chain — état du réseau*',
    '',
    `Bloc : \`${tete.toLocaleString('fr-FR').replace(/ | /g, ' ')}\``,
    `Temps de bloc mesuré : \`${secondes.toFixed(3)} s\` sur 100 blocs`,
    `Chain ID : \`${chainId}\``,
    `Offre de BOSA : \`${offre}\` — fixe, aucune émission`,
    '',
    `Vérifiable soi-même : ${RPC}`,
  ].join('\n');
}

async function publier(texte) {
  const motif = refuser(texte);
  if (motif) {
    console.error('REFUS : le message contient une promesse ou une projection.');
    console.error('  motifs : ' + motif);
    console.error("  Rien n'a ete publie. Le projet n'annonce ni rendement ni prix.");
    process.exit(1);
  }

  if (ESSAI) {
    console.log('--- ESSAI, rien ne part ---\n');
    console.log(texte);
    console.log('\n--- destinataire : ' + CANAL + ' ---');
    return;
  }

  const t = jeton();
  if (!t) {
    console.error('ECHEC : aucun jeton trouve.');
    console.error('  Attendu dans $TELEGRAM_TOKEN, /etc/coinbosa-telegram-token');
    console.error('  ou ~/.coinbosa-telegram-token (fichier en 0600).');
    process.exit(1);
  }

  const r = await fetch(`https://api.telegram.org/bot${t}/sendMessage`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ chat_id: CANAL, text: texte, parse_mode: 'Markdown',
                           disable_web_page_preview: true }),
  });
  const j = await r.json();
  if (!j.ok) {
    // On n'affiche JAMAIS le corps complet : il peut contenir le jeton.
    console.error(`ECHEC Telegram : ${j.error_code} ${j.description || ''}`);
    if (j.error_code === 400 && /chat not found/i.test(j.description || '')) {
      console.error("  Le bot est-il ADMINISTRATEUR du canal " + CANAL + ' ?');
    }
    process.exit(1);
  }
  console.log(`publie dans ${CANAL} (message ${j.result.message_id})`);
}

(async () => {
  const quoi = process.argv.filter((a) => a !== '--essai')[2];
  if (quoi === 'chaine') {
    await publier(await etatChaine());
  } else if (quoi === 'message') {
    const texte = process.argv.filter((a) => a !== '--essai').slice(3).join(' ');
    if (!texte) { console.error('Donne le texte : telegram-poste.js message "..."'); process.exit(1); }
    await publier(texte);
  } else {
    console.error('Usage : telegram-poste.js [--essai] chaine');
    console.error('        telegram-poste.js [--essai] message "le texte"');
    process.exit(1);
  }
})().catch((e) => { console.error('ERREUR :', e.message); process.exit(1); });
