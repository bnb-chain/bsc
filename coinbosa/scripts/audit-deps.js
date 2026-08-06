// Barrière d'audit des dépendances npm.
//
//   node scripts/audit-deps.js
//
// Lance `npm audit` et ÉCHOUE sur toute vulnérabilité qui n'est pas explicitement
// dérogée dans audit-allowlist.json. Une dérogation doit porter la preuve que le code
// vulnérable n'est pas atteignable chez nous, et une date d'expiration : sans quoi une
// dérogation posée « en attendant » deviendrait un trou permanent que plus personne ne
// regarde. C'est ce qui distingue un audit vert d'un audit vert PROUVÉ.
//
// Sortie 0 = aucune vulnérabilité, ou uniquement des vulnérabilités dérogées et valides.
// Sortie 1 = vulnérabilité inconnue, ou dérogation expirée.
const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const ROOT = path.join(__dirname, '..');
const ALLOWLIST = path.join(ROOT, 'audit-allowlist.json');
// Date injectable pour les tests ; sinon date du jour.
const TODAY = process.env.AUDIT_TODAY || new Date().toISOString().slice(0, 10);

function runAudit() {
  try {
    // npm audit sort en code 1 dès qu'il trouve quelque chose : ce n'est pas une erreur
    // d'exécution, on lit quand même le JSON produit.
    return execFileSync('npm', ['audit', '--omit=dev', '--json'], {
      cwd: ROOT, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 32 * 1024 * 1024,
    });
  } catch (e) {
    if (e.stdout && e.stdout.trim().startsWith('{')) return e.stdout;
    console.error('ERREUR : npm audit n\'a pas produit de JSON exploitable.');
    console.error(String(e.stderr || e.message).slice(0, 800));
    process.exit(1);
  }
}

const report = JSON.parse(runAudit());
const allow = JSON.parse(fs.readFileSync(ALLOWLIST, 'utf8')).derogations || [];
const byId = new Map(allow.map((d) => [d.avis, d]));

// --- relever chaque avis distinct trouvé par npm audit ---
const trouves = new Map(); // id -> {paquet, severite, titre, url}
for (const [paquet, v] of Object.entries(report.vulnerabilities || {})) {
  for (const via of v.via || []) {
    if (typeof via !== 'object' || !via.url) continue;
    const id = String(via.url).split('/').pop();
    if (!trouves.has(id)) trouves.set(id, { paquet, severite: via.severity, titre: via.title, url: via.url });
  }
}

const inconnus = [];
const expirees = [];
const couvertes = [];
for (const [id, info] of trouves) {
  const d = byId.get(id);
  if (!d) { inconnus.push({ id, ...info }); continue; }
  if (!d.expire_le || d.expire_le < TODAY) { expirees.push({ id, ...info, expire_le: d.expire_le }); continue; }
  couvertes.push({ id, ...info, expire_le: d.expire_le });
}
// Dérogation qui ne correspond à plus rien : la dépendance a été corrigée, il faut nettoyer.
const perimees = allow.filter((d) => !trouves.has(d.avis));

// --- rapport ---
const m = (report.metadata && report.metadata.vulnerabilities) || {};
console.log(`  npm audit : ${m.total || 0} vulnérabilité(s) — critical ${m.critical || 0}, high ${m.high || 0}, moderate ${m.moderate || 0}, low ${m.low || 0}`);

if (couvertes.length) {
  console.log('\n  Dérogées (non atteignables, justifiées et datées) :');
  for (const c of couvertes) console.log(`    ${c.id}  ${c.paquet}  [${c.severite}]  expire le ${c.expire_le}`);
}
if (perimees.length) {
  console.log('\n  ⚠  Dérogations devenues inutiles — à retirer de audit-allowlist.json :');
  for (const p of perimees) console.log(`    ${p.avis}  ${p.paquet}`);
}
if (expirees.length) {
  console.error('\nECHEC : dérogation EXPIRÉE — il faut la réexaminer, pas la prolonger sans regarder :');
  for (const e of expirees) console.error(`    ${e.id}  ${e.paquet}  [${e.severite}]  expirée le ${e.expire_le}`);
}
if (inconnus.length) {
  console.error('\nECHEC : vulnérabilité NON dérogée :');
  for (const u of inconnus) console.error(`    ${u.id}  ${u.paquet}  [${u.severite}]  ${u.titre}\n      ${u.url}`);
  console.error('\n  Corrige la dépendance, ou ajoute une dérogation JUSTIFIÉE (preuve de non-atteignabilité + date d\'expiration) dans audit-allowlist.json.');
}

if (inconnus.length || expirees.length) process.exit(1);
console.log('\n  audit conforme');
