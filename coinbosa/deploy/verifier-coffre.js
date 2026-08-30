#!/usr/bin/env node
// ---------------------------------------------------------------------------
// verifier-coffre.js — Vérifie qu'une SAUVEGARDE du coffre du validateur est
// exploitable, SANS la mettre en production et SANS toucher à la chaîne.
//
//   node verifier-coffre.js <coffre-UTC--...> <fichier-mot-de-passe> [adresse-attendue]
//
// Ce que le script fait :
//   1. relit le fichier de mot de passe EXACTEMENT comme geth le lit ;
//   2. déchiffre le coffre en mémoire (scrypt + AES-128-CTR) ;
//   3. recalcule l'adresse à partir de la clé privée obtenue ;
//   4. la compare à l'adresse attendue.
//
// Ce que le script ne fait JAMAIS :
//   - il n'ouvre aucune connexion réseau ;
//   - il n'écrit aucun fichier, ne modifie pas le coffre ;
//   - il n'affiche ni la clé privée, ni le mot de passe, ni le moindre octet
//     de l'un ou de l'autre.
//
// Code de sortie : 0 = sauvegarde BONNE, 1 = sauvegarde INEXPLOITABLE.
// ---------------------------------------------------------------------------
'use strict';
const fs = require('fs');
const path = require('path');
const { Wallet, getAddress } = require('ethers');

const [, , coffrePath, motDePassePath, adresseAttendueArg] = process.argv;

function mourir(msg) {
  console.error('ECHEC : ' + msg);
  process.exit(1);
}

if (!coffrePath || !motDePassePath) {
  console.error('usage : node verifier-coffre.js <coffre-UTC--...> <fichier-mot-de-passe> [adresse-attendue]');
  process.exit(2);
}
if (!fs.existsSync(coffrePath)) mourir('coffre introuvable : ' + coffrePath);
if (!fs.existsSync(motDePassePath)) mourir('fichier de mot de passe introuvable : ' + motDePassePath);

// --- Lecture du mot de passe, à l'identique de geth -------------------------
// geth (cmd/utils/flags.go, MakePasswordList) lit le fichier entier, le découpe
// sur "\n" et ne retient que la PREMIÈRE ligne, dont il retire un "\r" final.
// Toute autre convention (fichier lu en entier, espaces conservés) donnerait un
// mot de passe différent de celui que geth utilise réellement : la vérification
// serait fausse dans un sens comme dans l'autre.
const brut = fs.readFileSync(motDePassePath, 'utf8');
const motDePasse = brut.split('\n')[0].replace(/\r$/, '');
if (motDePasse.length === 0) mourir('la première ligne du fichier de mot de passe est vide');

// --- Adresse attendue -------------------------------------------------------
// À défaut d'argument, on prend celle inscrite dans le NOM du fichier coffre.
// C'est volontaire : le nom de fichier est une donnée indépendante du contenu
// chiffré, donc les comparer vérifie réellement quelque chose.
let adresseAttendue = adresseAttendueArg;
if (!adresseAttendue) {
  const base = path.basename(coffrePath);
  const m = base.match(/--([0-9a-fA-F]{40})$/);
  if (!m) mourir("aucune adresse attendue fournie et le nom du coffre n'en contient pas");
  adresseAttendue = '0x' + m[1];
}
try {
  adresseAttendue = getAddress(adresseAttendue);
} catch (e) {
  mourir('adresse attendue invalide : ' + adresseAttendue);
}

// --- Déchiffrement ----------------------------------------------------------
const json = fs.readFileSync(coffrePath, 'utf8');
let meta;
try {
  meta = JSON.parse(json);
} catch (e) {
  mourir('le coffre n\'est pas un JSON valide (fichier tronqué ou corrompu ?)');
}

// geth ecrit la section sous la cle "crypto" ; certains outils (ethers) ecrivent
// "Crypto". On accepte les deux, sinon l'affichage mentirait sur un fichier valide.
const crypto = meta.crypto || meta.Crypto || {};
console.log('coffre            : ' + coffrePath);
console.log('taille            : ' + fs.statSync(coffrePath).size + ' octets');
console.log('version keystore  : ' + meta.version);
console.log('kdf               : ' + crypto.kdf);
if (crypto.kdfparams) {
  const kp = crypto.kdfparams;
  console.log('parametres scrypt : N=' + kp.n + ' r=' + kp.r + ' p=' + kp.p + ' dklen=' + kp.dklen);
}
console.log('adresse attendue  : ' + adresseAttendue);
console.log('');
console.log('dechiffrement en cours (scrypt N=262144 : quelques secondes, c\'est normal)...');

const t0 = Date.now();
Wallet.fromEncryptedJson(json, motDePasse).then((portefeuille) => {
  const dt = ((Date.now() - t0) / 1000).toFixed(2);
  const obtenue = getAddress(portefeuille.address);
  console.log('dechiffrement OK en ' + dt + ' s');
  console.log('adresse obtenue   : ' + obtenue);
  console.log('');
  if (obtenue !== adresseAttendue) {
    console.error('ECHEC : le coffre se dechiffre, mais il contient une AUTRE cle que celle attendue.');
    console.error('  attendue : ' + adresseAttendue);
    console.error('  obtenue  : ' + obtenue);
    console.error('  Cette sauvegarde ne fera PAS produire de blocs sur la chaine.');
    process.exit(1);
  }
  console.log('RESULTAT : SAUVEGARDE BONNE.');
  console.log('  Le coffre et ce mot de passe redonnent bien la cle de ' + obtenue + '.');
  console.log('  Aucune connexion reseau n\'a ete ouverte, aucun fichier modifie.');
  process.exit(0);
}).catch((e) => {
  const dt = ((Date.now() - t0) / 1000).toFixed(2);
  console.error('');
  console.error('ECHEC apres ' + dt + ' s : le coffre n\'a PAS pu etre dechiffre.');
  console.error('  Cause rapportee : ' + (e && e.shortMessage ? e.shortMessage : (e && e.message)));
  console.error('');
  console.error('  Deux explications possibles, et une seule est rattrapable :');
  console.error('   - le mot de passe fourni n\'est pas celui du coffre  -> essayer l\'autre exemplaire ;');
  console.error('   - le fichier coffre est corrompu (MAC invalide)      -> cette copie est PERDUE,');
  console.error('     il faut en verifier une autre immediatement.');
  process.exit(1);
});
