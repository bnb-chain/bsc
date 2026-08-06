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

function run() {
  try {
    return execFileSync('govulncheck', ['-format', 'json', CIBLE],
      { cwd: REPO, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 256 * 1024 * 1024 });
  } catch (e) {
    // govulncheck sort en non-zéro dès qu'il trouve quelque chose : on lit quand même.
    if (e.stdout && e.stdout.length) return e.stdout;
    console.error('ERREUR : govulncheck n\'a pas pu s\'exécuter.');
    console.error(String(e.stderr || e.message).slice(0, 1000));
    process.exit(1);
  }
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
if (!blocs.length) { console.error('ERREUR : govulncheck n\'a produit aucun objet JSON exploitable.'); process.exit(1); }
for (const bloc of blocs) {
  let o; try { o = JSON.parse(bloc); } catch { continue; }
  if (o.osv) osvs.set(o.osv.id, { resume: (o.osv.summary || '').trim() });
  if (o.finding && o.finding.osv) {
    // Une trace qui nomme une `function` prouve un chemin d'appel réel jusqu'au code
    // vulnérable. Sans fonction, le module est seulement présent dans l'arbre : la faille
    // existe mais notre code ne l'atteint pas.
    if ((o.finding.trace || []).some((t) => t.function)) atteignables.add(o.finding.osv);
  }
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
