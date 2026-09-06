// =============================================================================
// Page de signature locale — Coinbosa, inscription de la clé de vote BLS.
//
// CE FICHIER NE VOIT JAMAIS DE CLÉ PRIVÉE. Il ne fait que présenter une
// transaction au portefeuille du navigateur, qui la transmet au portefeuille
// matériel. Le secret reste dans l'appareil, et c'est tout l'intérêt.
//
// Les paramètres sont servis par scripts/signer-navigateur.js sur 127.0.0.1 :
// ils sont calculés côté Node, avec les mêmes gardes que
// scripts/inscrire-cle-vote.js, et re-vérifiés ici avant tout envoi. Une garde
// qui n'existe qu'à un seul endroit finit toujours par être contournée.
// =============================================================================
'use strict';

const $ = (id) => document.getElementById(id);
let P = null;              // parametres servis par le lanceur
let compte = null;

function etat(texte, classe) {
  const e = $('etat');
  e.textContent = texte;
  e.className = 'etat' + (classe ? ' ' + classe : '');
}

function ligne(dl, cle, valeur) {
  const dt = document.createElement('dt'); dt.textContent = cle;
  const dd = document.createElement('dd'); dd.textContent = valeur;
  dl.append(dt, dd);
}

/// Une quantité JSON-RPC : « 0x » suivi d'au moins un chiffre hexadécimal.
const estQuantite = (v) => typeof v === 'string' && /^0x[0-9a-fA-F]+$/.test(v);

async function charger() {
  const r = await fetch('params.json', { cache: 'no-store' });
  P = await r.json();

  const dl = $('params');
  ligne(dl, 'réseau', `Coinbosa Chain — chainId ${P.chainId}`);
  ligne(dl, 'depuis', P.gouverneur);
  ligne(dl, 'vers', P.contrat);
  ligne(dl, 'valeur', '0 BOSA');
  ligne(dl, 'gaz', String(P.gaz));
  ligne(dl, 'coût estimé', P.cout + ' BOSA');
  ligne(dl, 'clé de vote', P.cleVote);
  ligne(dl, 'validateur', P.validateur);
  $('data').textContent = P.data;
  $('decode').textContent =
    `updateValidatorSet([${P.validateur}], [0x${P.cleVote}]) — l'ensemble des ` +
    `validateurs est réémis à l'identique : seuls les 48 octets de la clé changent.`;

  // GARDE CÔTÉ PAGE. Le lanceur a déjà vérifié tout cela ; on ne lui fait pas
  // confiance pour autant. Les 96 caractères de la clé DOIVENT se retrouver
  // tels quels dans la calldata, sinon on n'envoie rien.
  if (!/^[0-9a-f]{96}$/i.test(P.cleVote)) return refuser('la clé de vote servie ne fait pas 96 caractères hexadécimaux.');
  if ((P.data.length - 2) / 2 !== 292) return refuser(`calldata de ${(P.data.length - 2) / 2} octets au lieu de 292.`);
  if (!P.data.toLowerCase().includes(P.cleVote.toLowerCase())) return refuser("la clé ne se retrouve pas dans la calldata.");
  if (!window.ethereum) return refuser("aucun portefeuille détecté dans ce navigateur. Installe MetaMask, ou utilise --calldata.");

  etat('Prêt. Connecte ton portefeuille.');
}

function refuser(pourquoi) {
  etat('REFUS : ' + pourquoi + " Rien ne sera envoyé.", 'ko');
  $('connecter').disabled = true;
}

async function connecter() {
  try {
    etat('Connexion…');
    const comptes = await window.ethereum.request({ method: 'eth_requestAccounts' });
    compte = (comptes[0] || '').toLowerCase();

    const idHex = await window.ethereum.request({ method: 'eth_chainId' });
    const id = parseInt(idHex, 16);
    if (id !== P.chainId) {
      etat(`Mauvais réseau : le portefeuille est sur chainId ${id}, il faut ${P.chainId}. ` +
           `Bascule sur Coinbosa Chain dans MetaMask puis reconnecte.`, 'ko');
      return;
    }
    // LA VÉRIFICATION QUI ÉVITE L'ACCIDENT : signer depuis un autre compte ferait
    // partir une transaction qui reverte « only governor » APRÈS avoir coûté du gaz.
    if (compte !== P.gouverneur.toLowerCase()) {
      etat(`Ce compte n'est pas le gouverneur.\ncompte connecté : ${compte}\n` +
           `attendu         : ${P.gouverneur.toLowerCase()}\n` +
           `Sélectionne le bon compte dans MetaMask (celui du Ledger) et reconnecte.`, 'ko');
      return;
    }
    etat('Portefeuille connecté, c\'est bien le gouverneur. Simule avant d\'envoyer.', 'ok');
    $('simuler').disabled = false;
    $('envoyer').disabled = false;
  } catch (e) {
    etat('Connexion refusée : ' + (e.message || e), 'ko');
  }
}

async function simuler() {
  try {
    etat('Simulation en cours (eth_call — rien n\'est publié)…');
    const r = await window.ethereum.request({
      method: 'eth_call',
      params: [{ from: compte, to: P.contrat, data: P.data }, 'latest'],
    });
    // La fonction ne retourne rien : un résultat vide signifie « pas de revert ».
    if (r === '0x' || r === '0x0' || r === null) {
      etat('SIMULATION : la transaction passe. Rien n\'a été publié.', 'ok');
    } else {
      etat('Résultat inattendu de la simulation : ' + r + ' — ne pas envoyer.', 'ko');
      $('envoyer').disabled = true;
    }
  } catch (e) {
    etat('La chaîne REJETTE cet appel : ' + (e.message || e) + ' — ne rien envoyer.', 'ko');
    $('envoyer').disabled = true;
  }
}

async function envoyer() {
  $('envoyer').disabled = true;
  $('simuler').disabled = true;
  try {
    // On resimule JUSTE AVANT d'envoyer. L'état de la chaîne a pu changer entre
    // le clic sur « Simuler » et celui-ci, et une transaction qui reverte coûte
    // du gaz pour rien.
    etat('Contrôle final avant signature…');
    await window.ethereum.request({
      method: 'eth_call',
      params: [{ from: compte, to: P.contrat, data: P.data }, 'latest'],
    });

    etat('Vérifie maintenant sur l\'écran de ton appareil, puis confirme.\n' +
         `destination attendue : ${P.contrat}\nmontant attendu : 0`);
    const hash = await window.ethereum.request({
      method: 'eth_sendTransaction',
      params: [{ from: compte, to: P.contrat, data: P.data, value: '0x0', gas: P.gazHex }],
    });
    etat('Transaction envoyée : ' + hash + '\nAttente du reçu…');

    let recu = null;
    for (let i = 0; i < 60 && !recu; i++) {
      await new Promise((r) => setTimeout(r, 3000));
      recu = await window.ethereum.request({ method: 'eth_getTransactionReceipt', params: [hash] });
    }
    if (!recu) { etat('Pas de reçu après 3 minutes. Transaction : ' + hash, 'ko'); return; }
    if (!estQuantite(recu.status) || parseInt(recu.status, 16) !== 1) {
      etat('La transaction a été minée mais son statut est ' + recu.status + ' — elle a échoué.', 'ko');
      return;
    }
    const bloc = parseInt(recu.blockNumber, 16);
    const epoch = (Math.floor(bloc / 200) + 1) * 200;
    etat(`MINÉE au bloc ${bloc}, statut 1.\n\n` +
         `L'effet est différé : l'extraData n'est réécrite qu'aux blocs d'epoch.\n` +
         `Prochain bloc d'epoch : ${epoch} (~${Math.round((epoch - bloc) * 5 / 60)} min).\n` +
         `Ensuite, sur le serveur : sudo bash /usr/local/sbin/coinbosa-activer-vote.sh`, 'ok');
  } catch (e) {
    etat('Échec : ' + (e.message || e), 'ko');
    $('simuler').disabled = false;
  }
}

$('connecter').addEventListener('click', connecter);
$('simuler').addEventListener('click', simuler);
$('envoyer').addEventListener('click', envoyer);
charger().catch((e) => refuser('paramètres illisibles : ' + (e.message || e)));
