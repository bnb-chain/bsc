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
// L'ACCIDENT QUE CE FICHIER EXISTE POUR ÉVITER
// -------------------------------------------
// Un contrôle qui ne PEUT PAS vérifier doit crier, jamais se taire : un contrôle qui ment
// est pire que pas de contrôle, parce qu'il fait cesser la vigilance. La version
// précédente se taisait. `npm audit` sort en code 1 aussi bien quand il TROUVE des failles
// que quand il ÉCHOUE (registre injoignable, 401/429/503 sur le seul endpoint d'avis,
// arbre de travail sans package-lock.json) ; il émet ses pannes en JSON, donc elles
// commencent par « { » et franchissaient la seule garde en place. Le message d'erreur
// n'ayant aucune vulnérabilité à lire, le script imprimait « 0 vulnérabilité » puis
// « audit conforme », exit 0 — sur un audit qui n'avait rien audité. C'est le faux vert
// exact qui avait déjà piégé audit-go.js sur un arbre incomplet.
// Chaque contrôle ci-dessous nomme l'accident qu'il attrape.
//
// Sortie 0 = un rapport RÉEL a été obtenu, et il ne contient que des vulnérabilités
//            dérogées, justifiées, datées et non expirées.
// Sortie 1 = tout le reste, y compris « je n'ai pas pu vérifier ».
const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const ROOT = path.join(__dirname, '..');
const ALLOWLIST = path.join(ROOT, 'audit-allowlist.json');

// L'horloge vient du système, et de nulle part ailleurs. La version précédente lisait
// AUDIT_TODAY dans l'environnement : une variable posée dans un profil, un runner ou un
// workflow suffisait à figer la date et à rendre TOUTE dérogation éternelle, sans rien
// afficher d'anormal — le garde-fou anti-« trou permanent » désactivé en silence.
// Le mécanisme d'expiration se met à l'épreuve en écrivant une date passée dans
// audit-allowlist.json, pas en reculant l'horloge.
const AUJOURDHUI = new Date().toISOString().slice(0, 10);

// Format de rapport que ce script sait lire (npm 7 à 11 émettent la version 2 ; la CI
// épingle Node 20, donc npm 10). Tout autre format est refusé, jamais interprété : un
// rapport npm 6 (clé « advisories ») ou un futur format 3 ne remplirait pas la clé
// « vulnerabilities », et on y lirait « 0 faille » alors qu'il en annonce.
const FORMAT_ATTENDU = 2;

// Une dérogation est un DÉLAI, pas une dispense. Au-delà d'un an elle ne force plus aucun
// réexamen : « expire_le: 2099-01-01 » est un trou permanent déguisé en date valide.
const HORIZON_MAX_JOURS = 366;

// Longueur minimale de la preuve de non-atteignabilité. Le seuil n'évalue pas la qualité
// du texte — il interdit seulement qu'un « TODO » ou un « ok » tienne lieu de preuve
// pendant que le script imprime, lui, « non atteignables, justifiées et datées ».
const LONGUEUR_PREUVE = 40;

const SEVERITES = ['info', 'low', 'moderate', 'high', 'critical'];
const JOUR_ISO = /^\d{4}-\d{2}-\d{2}$/;

// --- accumulation des échecs -------------------------------------------------------
// On ne s'arrête pas au premier problème : celui qui devra agir doit voir tout ce qui
// bloque en une seule exécution, pas en découvrir un par run de CI.
const echecs = [];
function echec(titre, ...details) { echecs.push({ titre, details }); }

// Arrêt immédiat, réservé au cas « je n'ai pas de rapport » : sans rapport, il n'y a rien
// à dire sur les failles, et surtout rien à déclarer conforme.
function abandon(titre, lignes) {
  console.error(`\nECHEC : impossible de vérifier — ${titre}`);
  for (const l of lignes) console.error(l);
  process.exit(1);
}

// Une date réelle, ou rien. La comparaison de chaînes de la version précédente
// (`d.expire_le < TODAY`) était lexicographique et sans validation : « 2026-8-15 » (mois
// sans zéro) est lexicographiquement SUPÉRIEUR à « 2026-09-02 », donc une dérogation
// expirée depuis des semaines restait « valide » ; « hier » ou « à revoir », supérieurs à
// toute date ISO, la rendaient éternelle. On n'accepte donc que le format exact, et on
// vérifie par aller-retour Date→ISO que le jour EXISTE (2027-02-31 passe la regex mais
// Date le décale au 3 mars).
function jourValide(v) {
  if (typeof v !== 'string' || !JOUR_ISO.test(v)) return null;
  const d = new Date(`${v}T00:00:00Z`);
  if (Number.isNaN(d.getTime()) || d.toISOString().slice(0, 10) !== v) return null;
  return d;
}

// --- 1. obtenir un rapport, ou dire pourquoi on n'en a pas -------------------------
function lancerNpmAudit() {
  try {
    const sortie = execFileSync('npm', ['audit', '--omit=dev', '--json'], {
      cwd: ROOT, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 32 * 1024 * 1024,
    });
    return { sortie, stderr: '' };
  } catch (e) {
    // Le code de sortie ne DISCRIMINE RIEN : `npm audit` sort en 1 quand il trouve des
    // failles ET quand il échoue. On récupère la sortie telle quelle ; c'est son CONTENU,
    // examiné ci-dessous, qui décide s'il s'agit d'un rapport ou d'une panne.
    return { sortie: typeof e.stdout === 'string' ? e.stdout : '', stderr: String(e.stderr || e.message || '') };
  }
}

// Rend null si la sortie est un rapport exploitable ; sinon { texte, retenter }.
function raisonInexploitable(sortie) {
  const brut = String(sortie || '').trim();
  if (!brut) return { texte: 'npm audit n\'a rien écrit sur sa sortie standard', retenter: true };

  let r;
  try { r = JSON.parse(brut); } catch (e) { return { texte: `sortie illisible, ce n'est pas du JSON (${e.message})`, retenter: true }; }
  if (!r || typeof r !== 'object' || Array.isArray(r)) return { texte: 'sortie JSON qui n\'est pas un objet de rapport', retenter: false };

  // ACCIDENT : prendre une PANNE pour un audit propre. npm émet ses erreurs en JSON —
  // {"error":{"code":"ENOLOCK"}} quand il manque package-lock.json, {"message":"...
  // ECONNREFUSED ..."} quand le registre ou le proxy est mort, idem sur 401/429/503 de
  // l'endpoint d'avis. Ces objets commencent par « { », donc la garde précédente
  // (« le flux commence par { ») les laissait passer, et un rapport sans vulnérabilité à
  // lire ressortait en « audit conforme ».
  if (r.error || r.message) {
    const detail = (r.error && typeof r.error === 'object') ? (r.error.code || r.error.summary || r.error.detail || '') : (r.error || '');
    const texte = `npm audit a renvoyé une ERREUR, pas un rapport : ${String(r.message || detail || '(sans détail)').slice(0, 300)}`;
    // ENOLOCK est structurel (il manque un fichier) : réessayer ne fait que perdre du
    // temps. Une panne réseau, elle, mérite une seconde chance avant de crier.
    const structurel = (r.error && typeof r.error === 'object' && r.error.code === 'ENOLOCK');
    return { texte, retenter: !structurel };
  }

  // ACCIDENT : lire « 0 faille » dans un format qu'on ne comprend pas. Un rapport npm 6
  // range ses avis sous « advisories » : la clé « vulnerabilities » est absente, donc
  // aucun avis relevé, donc conforme — pendant que ses propres compteurs annoncent une
  // faille critique. On refuse le format inconnu au lieu de l'interpréter.
  if (r.auditReportVersion !== FORMAT_ATTENDU) {
    return { texte: `format de rapport inattendu : auditReportVersion=${JSON.stringify(r.auditReportVersion)} (ce script lit la version ${FORMAT_ATTENDU})`, retenter: false };
  }
  if (!r.vulnerabilities || typeof r.vulnerabilities !== 'object' || Array.isArray(r.vulnerabilities)) {
    return { texte: 'rapport sans section « vulnerabilities » exploitable', retenter: false };
  }
  const meta = r.metadata;
  if (!meta || typeof meta !== 'object') return { texte: 'rapport sans section « metadata »', retenter: false };
  if (!meta.vulnerabilities || typeof meta.vulnerabilities !== 'object') return { texte: 'rapport sans compteurs « metadata.vulnerabilities »', retenter: false };

  // ACCIDENT : « 0 faille » sur un arbre où RIEN n'a été chargé — le scénario exact qui a
  // rendu audit-go.js menteur. Un audit qui n'a analysé aucune dépendance n'a rien prouvé :
  // « 0 dépendance analysée » n'est pas « 0 vulnérabilité ». C'est la preuve, positive et
  // vérifiable, qu'un arbre a bien été lu.
  const deps = meta.dependencies;
  if (!deps || typeof deps !== 'object' || !(Number(deps.total) > 0)) {
    return { texte: `aucune dépendance analysée (metadata.dependencies.total = ${deps && typeof deps === 'object' ? JSON.stringify(deps.total) : 'absent'}) — « 0 dépendance » n'est pas « 0 faille »`, retenter: false };
  }
  return null;
}

// Une barrière qui crie à tort finit désarmée : un 429 ou une coupure d'une seconde sur
// l'endpoint d'avis ne doit pas rougir la CI d'une chaîne saine. On réessaie donc les
// pannes plausiblement passagères — et seulement celles-là — avant de conclure.
function dormir(ms) {
  if (ms > 0) Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);
}

const ATTENTES_MS = [0, 2000, 5000];
let rapport = null;
let derniereRaison = null;
let derniereStderr = '';
for (let i = 0; i < ATTENTES_MS.length; i += 1) {
  dormir(ATTENTES_MS[i]);
  const { sortie, stderr } = lancerNpmAudit();
  derniereStderr = stderr;
  derniereRaison = raisonInexploitable(sortie);
  if (!derniereRaison) { rapport = JSON.parse(sortie); break; }
  if (!derniereRaison.retenter) break;
  if (i + 1 < ATTENTES_MS.length) {
    console.error(`  tentative ${i + 1}/${ATTENTES_MS.length} inexploitable (${derniereRaison.texte}) — nouvel essai dans ${ATTENTES_MS[i + 1] / 1000}s`);
  }
}

if (!rapport) {
  abandon('l\'audit n\'a RIEN pu analyser.', [
    `  Raison : ${derniereRaison.texte}`,
    ...(derniereStderr.trim() ? [`  npm (stderr) : ${derniereStderr.trim().slice(0, 500)}`] : []),
    '',
    '  Ce résultat ne veut PAS dire « aucune vulnérabilité » : le contrôle n\'a pas pu',
    '  s\'exécuter, donc rien n\'est prouvé. À vérifier, dans cet ordre :',
    '    1. package-lock.json présent à la racine de coinbosa/ et `npm ci` déjà passé —',
    '       npm audit exige un lockfile, sinon il répond ENOLOCK ;',
    '    2. accès au registre npm, proxy d\'entreprise compris. npm audit interroge',
    '       /-/npm/v1/security/advisories/bulk : cet endpoint peut répondre 401, 429 ou',
    '       503 pendant que le reste du registre fonctionne — `npm ci --no-audit` ne le',
    '       touche même pas et passe sans bruit. Reproduire à la main :',
    '         npm audit --omit=dev --json | head -30',
    '    3. version de npm : ce script lit auditReportVersion 2 (npm 7 à 11). Si npm a',
    '       changé de format, c\'est CE SCRIPT qu\'il faut mettre à jour — pas la barrière',
    '       qu\'il faut retirer.',
    '  Tant que ce point n\'est pas réglé, aucun « conforme » n\'est prononçable.',
  ]);
}

// --- 2. relever les avis, et vérifier qu'on a compris le rapport --------------------
const compteurs = rapport.metadata.vulnerabilities;
const depsTotal = Number(rapport.metadata.dependencies.total);
const paquetsVulnerables = Object.keys(rapport.vulnerabilities);

const trouves = new Map();  // id d'avis -> { paquet, severite, titre, url }
const avisSansId = [];      // avis qu'on ne sait pas nommer : indérogeables

function idDeVia(via) {
  if (typeof via.url !== 'string') return null;
  const segment = via.url.split('/').filter(Boolean).pop();
  return segment && segment.trim() ? segment.trim() : null;
}

// `via` mélange deux natures : des OBJETS (l'avis lui-même) et des CHAÎNES (le nom du
// paquet par lequel la vulnérabilité arrive — solc est vulnérable « via tmp »). On suit
// les deux, pour pouvoir affirmer ensuite que CHAQUE paquet signalé par npm a bien été
// rattaché à au moins un avis nommé.
function avisDuPaquet(nom, chaine) {
  const ids = new Set();
  if (chaine.includes(nom)) return ids;  // garde-fou : cycle dans le graphe « via »
  const v = rapport.vulnerabilities[nom];
  if (!v || !Array.isArray(v.via)) return ids;
  for (const via of v.via) {
    if (typeof via === 'string') {
      for (const id of avisDuPaquet(via, [...chaine, nom])) ids.add(id);
      continue;
    }
    if (!via || typeof via !== 'object') continue;
    const id = idDeVia(via);
    // ACCIDENT : sauter en silence un avis dépourvu d'URL. Sans URL il n'a pas
    // d'identifiant, donc il ne peut ni être dérogé ni être reconnu — la version
    // précédente faisait `continue` et pouvait ainsi annoncer « critical 1 » sur sa
    // première ligne et « audit conforme » sur la dernière. Un avis qu'on ne sait pas
    // nommer doit bloquer, pas disparaître.
    if (!id) {
      avisSansId.push({ paquet: nom, titre: via.title || '(sans titre)', severite: via.severity || '?' });
      continue;
    }
    ids.add(id);
    if (!trouves.has(id)) {
      trouves.set(id, { paquet: via.name || nom, severite: via.severity, titre: via.title || '(sans titre)', url: via.url });
    }
  }
  return ids;
}

const paquetsSansAvis = paquetsVulnerables.filter((nom) => avisDuPaquet(nom, []).size === 0);

if (avisSansId.length) {
  echec(
    'avis sans identifiant : impossible à déroger, donc bloquant',
    ...avisSansId.map((a) => `${a.paquet} — [${a.severite}] ${a.titre} (aucun champ « url » dans le rapport npm)`),
    'Une dérogation se réfère à un identifiant d\'avis (GHSA-…). Un avis anonyme ne peut pas',
    'être vérifié : corriger la dépendance, ou obtenir un rapport d\'un registre qui publie',
    'les URL d\'avis.',
  );
}
if (paquetsSansAvis.length) {
  echec(
    'paquets signalés vulnérables par npm mais rattachés à aucun avis identifié',
    `paquet(s) : ${paquetsSansAvis.join(', ')}`,
    'Le script mesurerait alors autre chose que ce qu\'il annonce. Comparer avec la sortie de',
    '`npm audit --omit=dev --json` et mettre CE SCRIPT à jour si npm a changé la forme de « via ».',
  );
}

// ACCIDENT : afficher les compteurs de npm et conclure d'après une lecture indépendante,
// sans jamais confronter les deux. Les compteurs de `metadata` et la liste
// `vulnerabilities` décrivent le même rapport : s'ils divergent, c'est que le script ne
// lit pas ce que npm a écrit — et le vert ne porterait plus sur rien.
const sommeSeverites = SEVERITES.reduce((s, k) => s + (Number(compteurs[k]) || 0), 0);
const totalAnnonce = Number(compteurs.total);
if (!Number.isFinite(totalAnnonce) || sommeSeverites !== totalAnnonce) {
  echec(
    'compteurs internes du rapport npm incohérents',
    `metadata.vulnerabilities.total = ${JSON.stringify(compteurs.total)}, somme des sévérités = ${sommeSeverites}`,
    'Rapport suspect : vérifier la sortie brute de `npm audit --omit=dev --json`.',
  );
} else if (totalAnnonce !== paquetsVulnerables.length) {
  echec(
    'désaccord entre ce que npm compte et ce que le script a lu',
    `npm annonce ${totalAnnonce} paquet(s) vulnérable(s) ; la section « vulnerabilities » en contient ${paquetsVulnerables.length}`,
    'Ne pas retirer la barrière : mettre ce script en accord avec le format réellement émis',
    'par la version de npm utilisée (`npm --version`), puis relancer.',
  );
}

// --- 3. lire et VALIDER la liste des dérogations -----------------------------------
let derogations;
try {
  const contenu = JSON.parse(fs.readFileSync(ALLOWLIST, 'utf8'));
  derogations = contenu.derogations;
} catch (e) {
  abandon('la liste des dérogations est inutilisable.', [
    `  Raison : audit-allowlist.json illisible (${e.message})`,
    '  Sans liste de dérogations lisible, aucune vulnérabilité ne peut être déclarée couverte.',
    `  Fichier : ${ALLOWLIST}`,
  ]);
}
if (!Array.isArray(derogations)) {
  abandon('la liste des dérogations est inutilisable.', [
    '  Raison : audit-allowlist.json ne contient pas de tableau « derogations ».',
    '  Attendu : { "derogations": [ … ] } — un tableau vide est légitime, une clé absente',
    '  est une faute de frappe qui ferait passer toute dérogation pour inexistante.',
    `  Fichier : ${ALLOWLIST}`,
  ]);
}

const valides = new Map();     // avis -> dérogation complète et vérifiée
const invalides = new Map();   // avis -> dérogation rejetée (pour un message précis)

derogations.forEach((d, i) => {
  const ou = `audit-allowlist.json → derogations[${i}]${d && typeof d.avis === 'string' ? ` (${d.avis})` : ''}`;
  if (!d || typeof d !== 'object' || Array.isArray(d)) { echec(`${ou} : ce n'est pas un objet de dérogation`); return; }

  // ACCIDENT : le script imprimait « Dérogées (non atteignables, justifiées et datées) »
  // sans jamais avoir lu ni preuve ni justification — une fausse déclaration produite par
  // le contrôle lui-même. Une entrée réduite à un identifiant et une date lointaine
  // suffisait à obtenir ce certificat. On vérifie donc tout ce que cette phrase affirme.
  const manques = [];
  if (typeof d.avis !== 'string' || !d.avis.trim()) manques.push('« avis » : l\'identifiant de l\'avis, tel qu\'il apparaît dans l\'URL du rapport npm (ex. GHSA-ph9p-34f9-6g65)');
  if (typeof d.paquet !== 'string' || !d.paquet.trim()) manques.push('« paquet » : le nom du paquet concerné, pour que l\'affichage ne soit pas une devinette');
  if (typeof d.severite !== 'string' || !SEVERITES.includes(d.severite)) manques.push(`« severite » : une valeur parmi ${SEVERITES.join(', ')} (reçu ${JSON.stringify(d.severite)})`);
  const preuve = typeof d.pourquoi === 'string' ? d.pourquoi.trim() : '';
  if (preuve.length < LONGUEUR_PREUVE) {
    manques.push(`« pourquoi » : la preuve que le code vulnérable n'est pas atteignable ici — quel appel, dans quel fichier, et pourquoi il n'est jamais franchi (${preuve.length} caractère(s) fournis, ${LONGUEUR_PREUVE} minimum : un « TODO » n'est pas une preuve)`);
  }
  const expire = jourValide(d.expire_le);
  if (!expire) manques.push(`« expire_le » : une date RÉELLE au format AAAA-MM-JJ (reçu ${JSON.stringify(d.expire_le)}) — « 2026-8-15 », « 2027-02-31 » ou « hier » ne sont pas des dates comparables`);

  if (manques.length) {
    echec(`${ou} : dérogation incomplète — sans ces champs, « non atteignable, justifiée et datée » serait une affirmation invérifiée`, ...manques.map((m) => `manque ${m}`));
    if (typeof d.avis === 'string' && d.avis.trim()) invalides.set(d.avis.trim(), d);
    return;
  }

  // Une dérogation est un délai borné : au-delà de l'horizon, elle ne force plus aucun
  // réexamen et redevient le trou permanent qu'elle prétend éviter.
  const restants = Math.round((expire.getTime() - Date.parse(`${AUJOURDHUI}T00:00:00Z`)) / 86400000);
  if (restants > HORIZON_MAX_JOURS) {
    echec(
      `${ou} : date d'expiration trop lointaine (${d.expire_le}, dans ${restants} jours)`,
      `Une dérogation est un délai, pas une dispense : au plus ${HORIZON_MAX_JOURS} jours.`,
      'Rapprocher expire_le, et prévoir le réexamen à cette date.',
    );
    invalides.set(d.avis.trim(), d);
    return;
  }
  if (valides.has(d.avis.trim())) {
    echec(`${ou} : dérogation en double pour ${d.avis.trim()}`, 'Deux entrées pour le même avis : l\'une des deux est ignorée en silence. En garder une seule.');
    return;
  }
  valides.set(d.avis.trim(), d);
});

// --- 4. confronter les avis trouvés aux dérogations --------------------------------
const inconnus = [];
const expirees = [];
const rejetees = [];
const desaccords = [];
const couvertes = [];
for (const [id, info] of trouves) {
  const d = valides.get(id);
  if (!d) {
    if (invalides.has(id)) rejetees.push({ id, ...info });
    else inconnus.push({ id, ...info });
    continue;
  }
  if (d.expire_le < AUJOURDHUI) { expirees.push({ id, ...info, expire_le: d.expire_le }); continue; }
  // ACCIDENT : une dérogation qui ne porte plus sur ce que le rapport décrit. Les
  // sévérités affichées venaient du rapport npm, pas de la dérogation : le contrôle se
  // comparait à lui-même et ne pouvait pas voir qu'un avis avait changé de paquet ou de
  // gravité depuis que la preuve de non-atteignabilité avait été écrite.
  if (d.paquet !== info.paquet || d.severite !== info.severite) {
    desaccords.push({ id, attendu: `${d.paquet} [${d.severite}]`, constate: `${info.paquet} [${info.severite}]` });
    continue;
  }
  couvertes.push({ id, ...info, expire_le: d.expire_le });
}

// ACCIDENT le plus retors : un registre, un miroir interne ou un proxy qui répond 200
// avec un jeu d'avis VIDE produit un rapport structurellement parfait (auditReportVersion
// 2, dépendances chargées, compteurs cohérents) — aucune garde de forme ne peut le
// distinguer d'un audit réellement propre. Le seul signal observable est que les
// dérogations connues, vraies la veille, ne correspondent soudain plus à rien. La version
// précédente dégradait ce canari en simple ⚠ : c'est exactement le symptôme qui avait
// trahi audit-go.js, imprimé puis ignoré. Ici il fait ÉCHOUER.
// LIMITE CONNUE, à dire plutôt qu'à cacher : ce témoin n'existe que tant qu'au moins une
// dérogation est en vigueur. Si audit-allowlist.json se vide un jour, plus rien ici ne
// distingue « registre muet » de « rien à signaler » ; il faudra alors un témoin explicite
// (un avis connu qu'on interroge exprès). Les contrôles de forme, eux, restent actifs.
const perimees = derogations.filter((d) => d && typeof d.avis === 'string' && !trouves.has(d.avis.trim()));

// --- 5. rapport --------------------------------------------------------------------
console.log(`  npm audit : ${totalAnnonce} vulnérabilité(s) sur ${depsTotal} dépendance(s) de production — critical ${compteurs.critical || 0}, high ${compteurs.high || 0}, moderate ${compteurs.moderate || 0}, low ${compteurs.low || 0}`);

if (couvertes.length) {
  console.log('\n  Dérogées (preuve de non-atteignabilité présente, paquet et sévérité conformes au rapport, échéance vérifiée) :');
  for (const c of couvertes) console.log(`    ${c.id}  ${c.paquet}  [${c.severite}]  expire le ${c.expire_le}`);
}

if (perimees.length) {
  echec(
    `${perimees.length} dérogation(s) ne correspondent à aucun avis du rapport`,
    ...perimees.map((p) => `${p.avis}  ${p.paquet || '(paquet non renseigné)'}`),
    'Deux causes possibles, et il faut trancher, pas ignorer :',
    '  a) la dépendance a été corrigée ou l\'avis retiré — RETIRER ces lignes de',
    '     audit-allowlist.json, dans le même commit que la correction ;',
    '  b) l\'audit n\'a rien analysé — un miroir, un cache ou un proxy a répondu 200 avec',
    '     zéro avis. Le rapport est alors parfaitement bien formé et indiscernable d\'un',
    '     audit propre : ces dérogations sont le seul témoin qui reste. Vérifier avec',
    '     `npm config get registry` et `npm audit --omit=dev --json`.',
    'Si TOUTES les dérogations deviennent inutiles d\'un coup, c\'est (b) jusqu\'à preuve du contraire.',
  );
}
if (expirees.length) {
  echec(
    'dérogation EXPIRÉE — elle doit être réexaminée, pas prolongée sans regarder',
    ...expirees.map((e) => `${e.id}  ${e.paquet}  [${e.severite}]  expirée le ${e.expire_le} (aujourd'hui ${AUJOURDHUI})`),
    'Reprendre la preuve de non-atteignabilité : est-elle toujours vraie ? Si oui, redater',
    'la dérogation ; si la dépendance est corrigeable, la corriger et supprimer l\'entrée.',
  );
}
if (desaccords.length) {
  echec(
    'la dérogation ne porte plus sur ce que le rapport décrit',
    ...desaccords.map((d) => `${d.id} : dérogation écrite pour ${d.attendu}, rapport npm ${d.constate}`),
    'La preuve de non-atteignabilité a été écrite pour un autre paquet ou une autre gravité :',
    'la réécrire après vérification, et mettre « paquet » et « severite » en accord avec le rapport.',
  );
}
if (rejetees.length) {
  echec(
    'vulnérabilité couverte uniquement par une dérogation REJETÉE ci-dessus',
    ...rejetees.map((u) => `${u.id}  ${u.paquet}  [${u.severite}]  ${u.titre}`),
    'Tant que la dérogation n\'est pas complète, cette vulnérabilité compte comme non dérogée.',
  );
}
if (inconnus.length) {
  echec(
    'vulnérabilité NON dérogée',
    ...inconnus.map((u) => `${u.id}  ${u.paquet}  [${u.severite}]  ${u.titre}  ${u.url}`),
    'Corriger la dépendance, ou ajouter dans audit-allowlist.json une dérogation JUSTIFIÉE :',
    'avis, paquet, severite, « pourquoi » (preuve que le code vulnérable n\'est pas atteignable',
    'ici : quel appel, dans quel fichier), et « expire_le » (date AAAA-MM-JJ réelle, au plus',
    `${HORIZON_MAX_JOURS} jours). Rappel : ne pas changer la version de solc pour faire taire un`,
    'audit — son bytecode fixe le hash du bloc 0, donc l\'identité de la chaîne.',
  );
}

if (echecs.length) {
  console.error(`\nECHEC : audit NON conforme — ${echecs.length} problème(s) :`);
  for (const e of echecs) {
    console.error(`\n  • ${e.titre}`);
    for (const d of e.details) console.error(`      ${d}`);
  }
  console.error('\n  Un contrôle qui se tait quand il ne peut pas vérifier est pire que pas de contrôle :');
  console.error('  corriger la cause, ne pas contourner la barrière.');
  process.exit(1);
}

// Le vert dit ce qu'il a PROUVÉ, pas seulement qu'il est vert.
console.log(`\n  audit conforme — rapport npm réel (format ${FORMAT_ATTENDU}), ${depsTotal} dépendance(s) analysée(s), ${trouves.size} avis identifié(s), ${couvertes.length} dérogation(s) vérifiée(s), 0 non dérogée`);
