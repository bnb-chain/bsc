// Barrière d'audit des dépendances Go, basée sur govulncheck.
//
//   node coinbosa/scripts/audit-go.js          (à lancer depuis la RACINE du dépôt)
//
// govulncheck est l'outil officiel de l'équipe Go. Contrairement à un scanner de
// dépendances classique, il fait une analyse d'ATTEIGNABILITÉ : il ne signale une faille
// que si le code vulnérable est réellement appelable depuis nos points d'entrée. C'est
// exactement ce qu'il faut ici — on ne veut pas d'une liste gonflée de failles théoriques,
// on veut celles qui comptent.
//
// Toute faille atteignable absente de go-vuln-allowlist.json fait ÉCHOUER la CI. Une
// dérogation exige une justification et une date d'expiration, pour qu'elle ne devienne
// pas un trou permanent.
const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const ROOT = path.join(__dirname, '..');            // coinbosa/
const REPO = path.join(ROOT, '..');                 // racine du dépôt (module Go)
const ALLOWLIST = path.join(ROOT, 'go-vuln-allowlist.json');
const CIBLE = process.env.GOVULN_TARGET || './cmd/geth/...';
const TODAY = process.env.AUDIT_TODAY || new Date().toISOString().slice(0, 10);

// UNE BARRIÈRE QUI NE SAIT PAS QU'ELLE A ÉCHOUÉ NE PROTÈGE RIEN.
//
// Ce script a rendu « audit Go conforme » sur un arbre de travail où cmd/geth
// n'était même pas présent (sparse-checkout). govulncheck n'avait chargé AUCUN
// paquet, n'avait donc trouvé aucune faille — et le script a conclu que les
// neuf dérogations connues étaient devenues inutiles. Un scan qui n'analyse
// rien trouve zéro faille : c'est le faux vert le plus dangereux qui soit,
// parce qu'il ressemble exactement à une bonne nouvelle.
//
// Le garde `if (!blocs.length)` ne suffisait pas : govulncheck émet son objet
// `config` AVANT de charger les paquets. Le flux JSON n'était donc pas vide,
// seulement dépourvu de résultats.
//
// On exige désormais TROIS preuves que le scan a réellement eu lieu.

// La chaîne Go analysée doit être CELLE QUE LE PROJET EMBARQUE.
//
// govulncheck rapporte aussi les failles de la bibliothèque standard, et celles-ci
// dépendent entièrement de la version du compilateur. Lancé avec un Go local
// différent de celui épinglé dans go.mod, le scan décrit un binaire qui n'existe
// nulle part : avec un Go plus ANCIEN il invente des failles que la production n'a
// pas ; avec un Go plus RÉCENT — et déjà corrigé — il en cache que la production a.
// Ce second sens est un faux vert, et c'est le seul qui compte ici.
//
// Constaté : lancé avec go1.26.5 alors que go.mod épingle go1.25.13, le scan a
// signalé sept failles stdlib toutes déjà corrigées dans 1.25.13. Sept faux
// positifs — et, dans l'autre sens, le même mécanisme aurait tu de vraies failles.
function chaineEpinglee() {
  const mod = fs.readFileSync(path.join(REPO, 'go.mod'), 'utf8');
  const m = mod.match(/^toolchain\s+(go[\d.]+)\s*$/m);
  if (m) return m[1];
  const g = mod.match(/^go\s+([\d.]+)\s*$/m);
  return g ? 'go' + g[1] : null;
}

function listerCible() {
  // 1. La cible existe et se charge. `go list` le dit sans ambiguïté.
  try {
    const out = execFileSync('go', ['list', '-e', '-f', '{{.ImportPath}}|{{if .Error}}KO{{end}}', CIBLE],
      { cwd: REPO, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 64 * 1024 * 1024,
        env: ENV_GO });
    const lignes = out.split('\n').filter((l) => l.trim());
    const casses = lignes.filter((l) => l.endsWith('|KO'));
    return { total: lignes.length, casses: casses.length };
  } catch (e) {
    return { total: 0, casses: 0, erreur: String(e.stderr || e.message).slice(0, 600) };
  }
}

const CHAINE = chaineEpinglee();
const ENV_GO = CHAINE ? { ...process.env, GOTOOLCHAIN: CHAINE } : process.env;

function run() {
  if (!CHAINE) {
    console.error("ECHEC : go.mod ne déclare aucune chaîne Go — impossible de savoir");
    console.error('  quelle bibliothèque standard analyser. Ajouter une directive `toolchain`.');
    process.exit(1);
  }
  const cible = listerCible();
  if (!cible.total || cible.casses || cible.erreur) {
    console.error(`ECHEC : la cible ${CIBLE} ne se charge pas — le scan n'analyserait RIEN.`);
    console.error(`  paquets trouvés : ${cible.total}, en erreur : ${cible.casses}`);
    if (cible.erreur) console.error('  ' + cible.erreur);
    console.error("\n  Cause la plus fréquente : arbre de travail incomplet (git sparse-checkout).");
    console.error('  Vérifier avec : git sparse-checkout list   puis   git sparse-checkout disable');
    process.exit(1);
  }
  console.log(`  cible ${CIBLE} : ${cible.total} paquet(s) chargé(s)`);
  console.log(`  chaîne Go analysée : ${CHAINE} (épinglée dans go.mod)`);

  let sortie, err = '';
  try {
    sortie = execFileSync('govulncheck', ['-format', 'json', CIBLE],
      { cwd: REPO, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 256 * 1024 * 1024,
        env: ENV_GO });
  } catch (e) {
    // govulncheck sort en non-zéro dès qu'il trouve quelque chose : on lit quand même.
    err = String(e.stderr || '');
    if (e.stdout && e.stdout.length) sortie = e.stdout;
    else {
      console.error("ECHEC : govulncheck n'a pas pu s'exécuter.");
      console.error(String(e.stderr || e.message).slice(0, 1000));
      process.exit(1);
    }
  }

  // 2. Aucune erreur de chargement dans stderr. govulncheck les y écrit tout en
  //    continuant d'émettre du JSON sur stdout.
  const symptomes = ['errors with the provided package patterns', 'could not import',
                     'no required module provides package', 'loading packages'];
  const vu = symptomes.filter((m) => err.includes(m));
  if (vu.length) {
    console.error('ECHEC : govulncheck a signalé des erreurs de chargement — analyse incomplète.');
    console.error('  symptômes : ' + vu.join(', '));
    console.error('  ' + err.split('\n').filter((l) => l.trim()).slice(0, 8).join('\n  '));
    process.exit(1);
  }
  return sortie;
}

// govulncheck émet des objets JSON CONCATÉNÉS et indentés, pas une ligne par objet :
// découper sur les retours à la ligne perdrait la quasi-totalité du flux (et ferait
// conclure « aucune faille » — exactement le faux vert qu'on cherche à éviter). On
// découpe donc en comptant les accolades, en ignorant celles qui sont dans une chaîne.
function decouper(txt) {
  const out = [];
  let prof = 0, debut = 0, chaine = false, echap = false;
  for (let i = 0; i < txt.length; i++) {
    const c = txt[i];
    if (chaine) {
      if (echap) echap = false;
      else if (c === '\\') echap = true;
      else if (c === '"') chaine = false;
      continue;
    }
    if (c === '"') chaine = true;
    else if (c === '{') { if (prof === 0) debut = i; prof++; }
    else if (c === '}') { prof--; if (prof === 0) out.push(txt.slice(debut, i + 1)); }
  }
  return out;
}

const osvs = new Map();          // id -> résumé
const atteignables = new Set();
const blocs = decouper(run());
if (!blocs.length) { console.error("ECHEC : govulncheck n'a produit aucun objet JSON exploitable."); process.exit(1); }
let aScanne = false, goVu = null;
for (const bloc of blocs) {
  let o; try { o = JSON.parse(bloc); } catch { continue; }
  // Forcer GOTOOLCHAIN ne suffit pas : il faut que le scan CONFIRME l'avoir
  // respecté. govulncheck inscrit la version reellement utilisee dans son
  // objet de configuration — on la relit plutot que de faire confiance.
  if (o.config && o.config.go_version) goVu = o.config.go_version;
  // 3. Preuve que le scan a eu lieu. Verifie sur le cas d'echec reel : une
  //    cible qui ne se charge pas produit EXACTEMENT un objet, { config }, et
  //    rien d'autre. Des qu'un objet d'un autre type apparait — progress, osv,
  //    finding — c'est que govulncheck est alle au-dela du prologue. On ne se
  //    fie pas au libelle du message de progression : il change avec la
  //    version de l'outil, et une barriere ne doit pas dependre de ca.
  if (Object.keys(o).some((k) => k !== 'config')) aScanne = true;
  if (o.osv) osvs.set(o.osv.id, { resume: (o.osv.summary || '').trim() });
  if (o.finding && o.finding.osv) {
    // Une trace qui nomme une `function` prouve un chemin d'appel réel jusqu'au code
    // vulnérable. Sans fonction, le module est seulement présent dans l'arbre : la faille
    // existe mais notre code ne l'atteint pas.
    if ((o.finding.trace || []).some((t) => t.function)) atteignables.add(o.finding.osv);
  }
}

if (goVu && CHAINE && goVu !== CHAINE) {
  console.error(`ECHEC : le scan a tourné avec ${goVu}, or go.mod épingle ${CHAINE}.`);
  console.error("  Les failles de la bibliothèque standard dépendent de la version du");
  console.error('  compilateur : ce résultat décrit un binaire qui n\'est pas le nôtre.');
  console.error(`  Relancer avec : GOTOOLCHAIN=${CHAINE} node coinbosa/scripts/audit-go.js`);
  process.exit(1);
}
if (!aScanne) {
  console.error("ECHEC : govulncheck n'a jamais annoncé de scan — aucun paquet n'a été analysé.");
  console.error('  Le flux JSON ne contient que son prologue. Conclure « 0 faille » ici serait faux.');
  process.exit(1);
}

const allow = JSON.parse(fs.readFileSync(ALLOWLIST, 'utf8')).derogations || [];
const byId = new Map(allow.map((d) => [d.avis, d]));

const inconnus = [], expirees = [], couvertes = [];
for (const id of atteignables) {
  const d = byId.get(id);
  const info = osvs.get(id) || {};
  if (!d) { inconnus.push({ id, ...info }); continue; }
  if (!d.expire_le || d.expire_le < TODAY) { expirees.push({ id, ...info, expire_le: d.expire_le }); continue; }
  couvertes.push({ id, ...info, expire_le: d.expire_le });
}
const perimees = allow.filter((d) => !atteignables.has(d.avis));

console.log(`  govulncheck sur ${CIBLE} : ${atteignables.size} faille(s) ATTEIGNABLE(S)`);
if (couvertes.length) {
  console.log('\n  Dérogées (connues, justifiées, datées) :');
  for (const c of couvertes) console.log(`    ${c.id}  expire le ${c.expire_le}  ${(c.resume || '').slice(0, 70)}`);
}
if (perimees.length) {
  console.log('\n  ⚠  Dérogations devenues inutiles — à retirer de go-vuln-allowlist.json :');
  for (const p of perimees) console.log(`    ${p.avis}`);
}
if (expirees.length) {
  console.error('\nECHEC : dérogation EXPIRÉE — à réexaminer, pas à prolonger sans regarder :');
  for (const e of expirees) console.error(`    ${e.id}  expirée le ${e.expire_le}  ${(e.resume || '').slice(0, 70)}`);
}
if (inconnus.length) {
  console.error('\nECHEC : faille atteignable NON dérogée :');
  for (const u of inconnus) console.error(`    ${u.id}  ${(u.resume || '').slice(0, 90)}\n      https://pkg.go.dev/vuln/${u.id}`);
  console.error('\n  Mettre à jour la dépendance, ou ajouter une dérogation JUSTIFIÉE et DATÉE dans go-vuln-allowlist.json.');
}
if (inconnus.length || expirees.length) process.exit(1);
console.log('\n  audit Go conforme');
