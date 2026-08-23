/* ═══════════════════════════════════════════════════════════════════════════
   SCÈNES 3D — lune en croissant (héros) et planète à anneaux (socle)

   WebGL brut, sans aucune bibliothèque. Ce n'est pas un choix esthétique : la
   politique de sécurité du site interdit tout script d'origine externe
   (script-src 'self'), donc aucune bibliothèque de CDN n'est chargeable, et
   embarquer Three.js coûterait 600 Ko pour deux scènes analytiques.

   Ce fichier DOIT rester externe. Le même code en <script> inline serait
   silencieusement bloqué par la CSP : la page s'afficherait, les canvas
   resteraient noirs, et rien dans l'interface ne dirait pourquoi.
   ═══════════════════════════════════════════════════════════════════════════ */

(function () {
  "use strict";

  var canvas = document.getElementById('node-net');
  // Garde délibérée. Seule l'accueil porte les canvas, et seule l'accueil charge
  // ce fichier aujourd'hui — mais il a déjà été référencé depuis la coque
  // commune, et il le sera peut-être à nouveau. Sans cette ligne, toute page
  // sans canvas lève une exception au chargement, et une exception non rattrapée
  // interrompt le script entier, y compris ce qui vient après.
  if (!canvas) return;
  var gl = canvas.getContext('webgl', { alpha: false, antialias: false, powerPreference: 'high-performance' })
        || canvas.getContext('experimental-webgl', { alpha: false, antialias: false });

  if (!gl) {
    // Repli. #secours et #panneau appartenaient au site en une page : ils
    // n'existent plus ni dans le HTML ni dans la CSS, et les appeler ici levait
    // « Cannot read properties of null » — au moment exact où le repli sert.
    // On cache le canvas : .hero::before pose deja son degrade dessous, donc le
    // heros reste complet, simplement sans la scene animee.
    canvas.style.display = 'none';
    return;
  }

  var reduit = window.matchMedia('(prefers-reduced-motion: reduce)');

  /* ═══════════════════════════════════════════════════════════════════
     LE FRAGMENT SHADER

     Tout est calculé par pixel, sans géométrie ni bibliothèque.

     La lune n'est pas modelée en « C » : c'est une SPHÈRE éclairée de côté.
     Le croissant naît de la position de la lumière, exactement comme dans le
     ciel. C'est ce qui donne un relief juste au lieu d'une forme plate — les
     cratères s'assombrissent du bon côté et la ligne d'ombre suit la courbure.

     L'intersection rayon/sphère est ANALYTIQUE (une racine carrée) plutôt que
     marchée pas à pas : c'est un ordre de grandeur moins cher, et exact.
     Le relief passe par une perturbation de la normale, pas par un déplacement
     réel de la surface — invisible à l'œil à cette échelle, et bien plus rapide.
     ═══════════════════════════════════════════════════════════════════ */

  var FS = [
    'precision highp float;',
    'uniform vec2  uRes;',
    'uniform float uT;',
    'uniform vec2  uSouris;',
    'uniform float uPhase;',
    'uniform float uRelief;',
    'uniform float uOrbite;',
    'uniform float uTaille;',

    /* ---------- bruit ---------- */
    'float hash13(vec3 p){',
    '  p = fract(p * 0.1031);',
    '  p += dot(p, p.zyx + 31.32);',
    '  return fract((p.x + p.y) * p.z);',
    '}',
    'float bruit(vec3 x){',
    '  vec3 i = floor(x), f = fract(x);',
    '  f = f * f * (3.0 - 2.0 * f);',
    '  return mix(mix(mix(hash13(i+vec3(0,0,0)), hash13(i+vec3(1,0,0)), f.x),',
    '                 mix(hash13(i+vec3(0,1,0)), hash13(i+vec3(1,1,0)), f.x), f.y),',
    '             mix(mix(hash13(i+vec3(0,0,1)), hash13(i+vec3(1,0,1)), f.x),',
    '                 mix(hash13(i+vec3(0,1,1)), hash13(i+vec3(1,1,1)), f.x), f.y), f.z);',
    '}',

    /* Les mers lunaires : larges taches sombres, basse fréquence. */
    'float mers(vec3 p){',
    '  float s = 0.0, a = 0.5;',
    '  for (int i = 0; i < 4; i++) { s += a * bruit(p); p *= 2.07; a *= 0.5; }',
    '  return s;',
    '}',

    /* Les cratères : bruit « en crête » (1 - |2n-1|), qui produit des bourrelets',
       'circulaires au lieu de bosses molles. Quatre octaves suffisent à donner',
       'une lecture de cratères sans coûter une fortune par pixel. */
    'float crateres(vec3 p){',
    '  float s = 0.0, a = 1.0, f = 1.0;',
    '  for (int i = 0; i < 4; i++) {',
    '    float n = bruit(p * f);',
    '    n = 1.0 - abs(n * 2.0 - 1.0);',
    '    s += n * n * a;',
    '    f *= 1.94; a *= 0.48;',
    '  }',
    '  return s;',
    '}',

    'float hauteur(vec3 p){',
    '  return crateres(p * 1.75) * 0.66 + mers(p * 1.15) * 0.34;',
    '}',

    /* Normale perturbée par différences finies sur la hauteur, reprojetée',
       'tangentiellement pour ne pas déformer la silhouette. */
    'vec3 relief(vec3 n, float force){',
    '  float e = 0.022;',
    '  float h  = hauteur(n);',
    '  float hx = hauteur(n + vec3(e, 0.0, 0.0));',
    '  float hy = hauteur(n + vec3(0.0, e, 0.0));',
    '  float hz = hauteur(n + vec3(0.0, 0.0, e));',
    '  vec3 g = vec3(hx - h, hy - h, hz - h) / e;',
    '  g -= n * dot(n, g);',
    '  return normalize(n - g * force * 0.052);',
    '}',

    /* Intersection analytique rayon / sphère centrée. */
    'float iSphere(vec3 ro, vec3 rd, float r){',
    '  float b = dot(ro, rd);',
    '  float c = dot(ro, ro) - r * r;',
    '  float h = b * b - c;',
    '  if (h < 0.0) return -1.0;',
    '  return -b - sqrt(h);',
    '}',
    'float iSphereD(vec3 ro, vec3 rd, vec3 ce, float r){',
    '  return iSphere(ro - ce, rd, r);',
    '}',

    /* Ellipsoïde de rayons quelconques, dans un repère orthonormé B.
       'La direction n est PAS renormalisée après division par les rayons : c est ce
       'qui garde la distance rendue dans les unités du monde, donc comparable à
       'celles des autres objets de la scène. */
    'float iEllipse(vec3 ro, vec3 rd, vec3 ce, mat3 B, vec3 ra){',
    '  vec3 o = (ro - ce) * B;',
    '  vec3 d = rd * B;',
    '  vec3 on = o / ra;',
    '  vec3 dn = d / ra;',
    '  float a = dot(dn, dn);',
    '  float b = dot(on, dn);',
    '  float c = dot(on, on) - 1.0;',
    '  float h = b * b - a * c;',
    '  if (h < 0.0) return -1.0;',
    '  return (-b - sqrt(h)) / a;',
    '}',

    /* Champ d'étoiles : figé dans la direction du rayon, donc stable au',
       'déplacement de la souris — des étoiles qui glissent trahissent le truc. */
    'float etoiles(vec3 rd){',
    '  vec3 g = rd * 260.0;',
    '  vec3 i = floor(g), f = fract(g);',
    '  float h = hash13(i);',
    '  if (h < 0.9955) return 0.0;',
    '  vec2 c = vec2(hash13(i + 7.1), hash13(i + 13.7));',
    '  float d = length(f.xy - c);',
    '  float sc = smoothstep(0.30, 0.0, d);',
    '  float pulse = 0.72 + 0.28 * sin(uT * 1.7 + h * 63.0);',
    '  return sc * pulse * (0.35 + hash13(i + 3.3) * 0.65);',
    '}',

    'mat3 rotX(float a){ float c=cos(a), s=sin(a); return mat3(1.,0.,0., 0.,c,-s, 0.,s,c); }',
    'mat3 rotY(float a){ float c=cos(a), s=sin(a); return mat3(c,0.,s, 0.,1.,0., -s,0.,c); }',

    'void main(){',
    '  vec2 uv = (gl_FragCoord.xy - 0.5 * uRes) / uRes.y;',

    /* Caméra. La lune est décalée à droite : le creux du « C » ouvre vers la
       colonne de texte, à gauche. Sur écran étroit elle remonte et se centre. */
    '  float etroit = step(uRes.x / uRes.y, 1.05);',
    '  vec2  decal  = mix(vec2(0.34, 0.02), vec2(0.02, 0.46), etroit);',
    '  uv -= decal;',

    '  vec3 ro = vec3(uSouris.x * 0.17, uSouris.y * 0.13, 3.55);',
    '  vec3 rd = normalize(vec3(uv * 1.10, -1.0));',
    '  ro *= uTaille * mix(1.0, 1.34, etroit);',

    '  vec3 col = vec3(0.0);',

    /* --- fond : dégradé froid + deux halos aux couleurs de la marque --- */
    '  float v = uv.y * 0.5 + 0.5;',
    '  col = mix(vec3(0.013, 0.026, 0.044), vec3(0.021, 0.040, 0.063), v);',
    '  col += vec3(0.34, 0.24, 0.05) * 0.055 * exp(-length(uv - vec2(-0.55, 0.35)) * 1.9);',
    '  col += vec3(0.02, 0.16, 0.28) * 0.075 * exp(-length(uv - vec2( 0.62,-0.42)) * 1.7);',
    '  col += vec3(etoiles(rd)) * vec3(0.86, 0.92, 1.0) * 0.85;',

    /* --- direction de la lumière : elle FAIT le croissant ---
       uPhase pilote l'angle entre la lumière et l'axe de vue. Plus il est grand,
       plus le croissant est fin. C'est le seul réglage qui change la forme du C. */
    '  float ph = radians(uPhase);',
    '  vec3 L = normalize(vec3(-sin(ph), 0.155, cos(ph)));',

    /* --- la lune --- */
    '  float R = 1.0;',
    '  float tM = iSphere(ro, rd, R);',

    /* --- le satellite : orbite inclinée autour de la lune ---
       Il passe DERRIÈRE la lune une fois sur deux ; l'ordre de profondeur est
       donné par la comparaison des distances d'intersection, donc l'occultation
       est exacte et gratuite. */
    '  float ang = 2.35 + uT * uOrbite;',
    '  mat3 incl = rotX(-1.32) * rotY(0.30);',
    '  vec3 pS = incl * vec3(cos(ang) * 1.28, 0.0, sin(ang) * 1.28);',
    '  vec3 axe = normalize(incl * vec3(-sin(ang), 0.0, cos(ang)));',

    '  float tSat = iSphereD(ro, rd, pS, 0.058);',

    /* Panneaux solaires : deux ellipsoïdes très aplatis — larges dans l envergure,
       'minces dans l épaisseur. On résout l ellipsoïde dans son propre repère SANS
       'renormaliser la direction : la distance obtenue reste alors exprimée en
       'unités du monde, donc directement comparable à celle de la lune pour
       'décider qui est devant. Renormaliser aurait faussé cette comparaison, et le
       'satellite serait passé devant la lune alors qu il est derrière. */
    '  vec3 vue  = vec3(0.0, 0.0, -1.0);',
    '  vec3 aile = normalize(cross(vue, axe));',
    '  vec3 nrm  = normalize(cross(aile, axe));',
    '  vec3 tanv = aile;',
    '  mat3 B    = mat3(nrm, tanv, axe);',
    '  vec3 rayons = vec3(0.007, 0.108, 0.040);',
    '  float tP1 = iEllipse(ro, rd, pS + tanv * 0.118, B, rayons);',
    '  float tP2 = iEllipse(ro, rd, pS - tanv * 0.118, B, rayons);',

    /* --- traînée orbitale : chapelet de positions passées, occultées par la lune --- */
    '  float trainee = 0.0;',
    '  for (int i = 0; i <= 26; i++) {',
    '    float k = float(i);',
    '    float a2 = ang - k * 0.019;',
    '    vec3 p2 = incl * vec3(cos(a2) * 1.28, 0.0, sin(a2) * 1.28);',
    '    float tp = dot(p2 - ro, rd);',
    '    if (tp < 0.0) continue;',
    '    if (tM > 0.0 && tM < tp) continue;',
    '    float d = length(ro + rd * tp - p2);',
    '    float poids = 1.0 - k / 27.0;',
    '    trainee += exp(-d * 96.0) * poids * 0.72;',
    '    if (i == 0) trainee += exp(-d * 26.0) * 0.85;',
    '  }',

    '  float tObj = 1e9; int quoi = 0;',
    '  if (tM  > 0.0 && tM  < tObj) { tObj = tM;  quoi = 1; }',
    '  if (tSat > 0.0 && tSat < tObj) { tObj = tSat; quoi = 2; }',
    '  if (tP1 > 0.0 && tP1 < tObj) { tObj = tP1; quoi = 3; }',
    '  if (tP2 > 0.0 && tP2 < tObj) { tObj = tP2; quoi = 3; }',

    '  if (quoi == 1) {',
    '    vec3 p = ro + rd * tM;',
    '    vec3 n = normalize(p);',
    '    vec3 nr = relief(n, uRelief);',

    '    float dif = pow(max(dot(nr, L), 0.0), 0.70);',
    /* Terminateur légèrement adouci : une transition trop nette fait carton
       découpé, une transition trop molle efface le croissant. */
    '    float term = smoothstep(-0.045, 0.16, dot(n, L));',

    '    vec3 sol   = vec3(1.00, 0.955, 0.876);',
    '    vec3 teinte = mix(vec3(0.52, 0.47, 0.42), vec3(0.97, 0.93, 0.86),',
    '                      smoothstep(0.35, 0.72, mers(nr * 1.5)));',

    '    vec3 c = teinte * sol * (dif * 1.78) * term;',
    /* Lumière cendrée : le disque sombre doit rester lisible, sinon on perd la
       sphère et il ne reste qu un arc flottant. */
    '    float cendre = 0.30 + 0.70 * max(dot(nr, -L) * 0.5 + 0.5, 0.0);',
    '    c += vec3(0.021, 0.036, 0.066) * cendre * (0.65 + 0.35 * hauteur(nr));',
    /* Liseré bleu sur le limbe : c est lui qui referme le « C » et détache la
       lune du fond. */
    '    float fres = pow(1.0 - max(dot(n, -rd), 0.0), 3.9);',
    '    float cote = smoothstep(-0.26, 0.20, dot(n, L));',
    '    c += mix(vec3(0.08, 0.29, 0.55), vec3(1.00, 0.78, 0.34), cote) * fres * 0.66;',
    '    c += vec3(0.90, 0.72, 0.28) * pow(max(dot(reflect(-L, nr), -rd), 0.0), 22.0) * 0.10 * term;',
    '    col = c;',
    '  } else if (quoi == 2) {',
    '    vec3 n = normalize(ro + rd * tSat - pS);',
    '    float dif = max(dot(n, L), 0.0);',
    '    col = vec3(0.95, 0.92, 0.86) * (0.30 + dif * 1.35);',
    '    col += vec3(1.00, 0.78, 0.32) * 0.55;',
    '    col += vec3(0.95, 0.62, 0.18) * pow(1.0 - max(dot(n, -rd), 0.0), 2.4) * 0.5;',
    '  } else if (quoi == 3) {',
    '    vec3 pp = ro + rd * tObj;',
    '    vec3 n  = normalize(nrm * sign(dot(nrm, pp - pS)));',
    '    float dif = max(abs(dot(n, L)), 0.0);',
    '    col = mix(vec3(0.020, 0.055, 0.125), vec3(0.07, 0.27, 0.52), dif) * (0.62 + dif);',
    '    col += vec3(0.42, 0.74, 1.00) * pow(dif, 5.0) * 0.85;',
    '  } else {',
    /* Couronne : halo doux autour du disque. Il est dissymétrique — plus chaud du
       'côté d où vient la lumière — sinon le halo trahit une lune plate posée sur
       'un fond, au lieu d une sphère éclairée dans l espace. */
    '    float dl = length(uv);',
    '    float couronne = exp(-max(dl - 0.30, 0.0) * 5.6);',
    '    float cote = clamp(dot(normalize(vec3(uv, 0.35)), L) * 0.5 + 0.5, 0.0, 1.0);',
    '    col += mix(vec3(0.05, 0.10, 0.19), vec3(0.30, 0.25, 0.14), cote) * couronne * 0.50;',
    '  }',

    '  col += mix(vec3(0.38, 0.68, 1.0), vec3(1.0, 0.80, 0.38), 0.35) * trainee * 0.52;',

    /* Vignettage + léger grain : le grain casse les bandes de dégradé, très
       visibles sur un fond sombre en 8 bits. */
    '  col *= 1.0 - 0.30 * pow(length((gl_FragCoord.xy / uRes - 0.5) * vec2(1.05, 1.0)), 2.3);',
    '  col = pow(max(col, 0.0), vec3(0.4545));',
    '  col += (hash13(vec3(gl_FragCoord.xy, uT * 60.0)) - 0.5) * 0.016;',
    '  gl_FragColor = vec4(col, 1.0);',
    '}'
  ].join('\n');

  var VS = [
    'attribute vec2 aPos;',
    'void main(){ gl_Position = vec4(aPos, 0.0, 1.0); }'
  ].join('\n');

  function compiler(type, src) {
    var s = gl.createShader(type);
    gl.shaderSource(s, src);
    gl.compileShader(s);
    if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) {
      console.error('shader :', gl.getShaderInfoLog(s));
      return null;
    }
    return s;
  }

  var vs = compiler(gl.VERTEX_SHADER, VS);
  var fs = compiler(gl.FRAGMENT_SHADER, FS);
  if (!vs || !fs) {
    // Repli. #secours et #panneau appartenaient au site en une page : ils
    // n'existent plus ni dans le HTML ni dans la CSS, et les appeler ici levait
    // « Cannot read properties of null » — au moment exact où le repli sert.
    // On cache le canvas : .hero::before pose deja son degrade dessous, donc le
    // heros reste complet, simplement sans la scene animee.
    canvas.style.display = 'none';
    return;
  }

  var prog = gl.createProgram();
  gl.attachShader(prog, vs);
  gl.attachShader(prog, fs);
  gl.linkProgram(prog);
  gl.useProgram(prog);

  var buf = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, buf);
  gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1,-1, 3,-1, -1,3]), gl.STATIC_DRAW);
  var loc = gl.getAttribLocation(prog, 'aPos');
  gl.enableVertexAttribArray(loc);
  gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);

  var U = {};
  ['uRes','uT','uSouris','uPhase','uRelief','uOrbite','uTaille'].forEach(function (n) {
    U[n] = gl.getUniformLocation(prog, n);
  });

  /* ---------- dimensionnement ----------
     Le rapport de pixels est plafonné : ce shader coûte cher par pixel, et un
     écran 3x en plein format ferait chuter la fluidité sans gain visible. */
  var dpr = 1;
  function redim() {
    dpr = Math.min(window.devicePixelRatio || 1, 1.6);
    var w = Math.floor(canvas.clientWidth  * dpr);
    var h = Math.floor(canvas.clientHeight * dpr);
    if (canvas.width !== w || canvas.height !== h) {
      canvas.width = w; canvas.height = h;
      gl.viewport(0, 0, w, h);
    }
    gl.uniform2f(U.uRes, canvas.width, canvas.height);
  }

  /* ---------- réglages figés ----------
     Les curseurs de mise au point n'existent pas en production. Les valeurs
     retenues sont celles validées à l'aperçu. Câbler des curseurs absents
     lèverait une exception sur un élément nul et tuerait toute la scène. */
  var reg = { phase: 122, relief: 62, orbite: 42, taille: 100 };
  function lire() {
    gl.uniform1f(U.uPhase,  reg.phase);
    gl.uniform1f(U.uRelief, reg.relief / 100 * 1.7);
    gl.uniform1f(U.uOrbite, reg.orbite / 100 * 0.55);
    gl.uniform1f(U.uTaille, 1.62 - reg.taille / 100 * 0.62);
  }

  /* ---------- souris : parallaxe douce, amortie ---------- */
  var cible = { x: 0, y: 0 }, cour = { x: 0, y: 0 };
  window.addEventListener('pointermove', function (e) {
    cible.x = (e.clientX / window.innerWidth  - 0.5) * 2;
    cible.y = (0.5 - e.clientY / window.innerHeight) * 2;
  }, { passive: true });

  /* ---------- boucle ---------- */
  var t0 = null, raf = null, visible = true;

  function rendu(ms) {
    raf = requestAnimationFrame(rendu);
    if (!visible) return;
    if (t0 === null) t0 = ms;
    var t = (ms - t0) / 1000;

    cour.x += (cible.x - cour.x) * 0.045;
    cour.y += (cible.y - cour.y) * 0.045;

    redim();
    var force = new URLSearchParams(location.search).get('t');
    gl.uniform1f(U.uT, force !== null ? parseFloat(force) : (reduit.matches ? 6.0 : t));
    gl.uniform2f(U.uSouris, cour.x, cour.y);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
    if (reduit.matches) { cancelAnimationFrame(raf); raf = null; }
  }

  /* On ne dessine pas une scène que personne ne regarde : onglet caché ou
     canvas hors écran, la boucle s'arrête. */
  document.addEventListener('visibilitychange', function () {
    visible = !document.hidden;
    if (visible && raf === null && !reduit.matches) { t0 = null; raf = requestAnimationFrame(rendu); }
  });
  if ('IntersectionObserver' in window) {
    new IntersectionObserver(function (e) {
      visible = e[0].isIntersecting;
      if (visible && raf === null && !reduit.matches) { t0 = null; raf = requestAnimationFrame(rendu); }
    }, { threshold: 0.01 }).observe(canvas);
  }

  window.addEventListener('resize', redim, { passive: true });
  redim();
  lire();
  raf = requestAnimationFrame(rendu);
})();



(function () {
  "use strict";

  var canvas = document.getElementById('anneaux');
  // Garde délibérée. Seule l'accueil porte les canvas, et seule l'accueil charge
  // ce fichier aujourd'hui — mais il a déjà été référencé depuis la coque
  // commune, et il le sera peut-être à nouveau. Sans cette ligne, toute page
  // sans canvas lève une exception au chargement, et une exception non rattrapée
  // interrompt le script entier, y compris ce qui vient après.
  if (!canvas) return;
  var gl = canvas.getContext('webgl', { alpha: false, antialias: false, powerPreference: 'high-performance' })
        || canvas.getContext('experimental-webgl', { alpha: false, antialias: false });
  if (!gl) { canvas.style.display = 'none'; return; }

  var reduit = window.matchMedia('(prefers-reduced-motion: reduce)');

  /* ═══════════════════════════════════════════════════════════════════
     LA PLANÈTE

     Le logo n'est PAS plaqué sur une sphère. Une marque projetée sur une
     surface courbe se déforme, et ce logo ne doit pas être retouché : il est
     donc rendu comme un DISQUE plat face à la caméra, net et intact. Toute la
     profondeur vient de ce qui l'entoure — les anneaux passent devant et
     derrière lui, et c'est cette occultation qui crée le volume.

     Les anneaux sont un plan incliné, pas une image : on intersecte le rayon
     avec ce plan et on colore selon le rayon obtenu. L'ordre devant/derrière
     se règle en comparant les distances d'intersection, donc l'occultation est
     exacte et gratuite.
     ═══════════════════════════════════════════════════════════════════ */

  var FS = [
    'precision highp float;',
    'uniform vec2  uRes;',
    'uniform float uT;',
    'uniform vec2  uSouris;',
    'uniform sampler2D uLogo;',

    'float hash13(vec3 p){',
    '  p = fract(p * 0.1031); p += dot(p, p.zyx + 31.32);',
    '  return fract((p.x + p.y) * p.z);',
    '}',
    'float bruit(vec3 x){',
    '  vec3 i = floor(x), f = fract(x); f = f*f*(3.0-2.0*f);',
    '  return mix(mix(mix(hash13(i+vec3(0,0,0)),hash13(i+vec3(1,0,0)),f.x),',
    '                 mix(hash13(i+vec3(0,1,0)),hash13(i+vec3(1,1,0)),f.x),f.y),',
    '             mix(mix(hash13(i+vec3(0,0,1)),hash13(i+vec3(1,0,1)),f.x),',
    '                 mix(hash13(i+vec3(0,1,1)),hash13(i+vec3(1,1,1)),f.x),f.y),f.z);',
    '}',
    'float fbm(vec3 p){',
    '  float s=0.0, a=0.5;',
    '  for (int i=0;i<4;i++){ s += a*bruit(p); p*=2.11; a*=0.52; }',
    '  return s;',
    '}',

    'mat3 rotX(float a){ float c=cos(a),s=sin(a); return mat3(1.,0.,0., 0.,c,-s, 0.,s,c); }',
    'mat3 rotZ(float a){ float c=cos(a),s=sin(a); return mat3(c,-s,0., s,c,0., 0.,0.,1.); }',

    'float iSphere(vec3 ro, vec3 rd, vec3 ce, float r){',
    '  vec3 o = ro - ce;',
    '  float b = dot(o, rd), c = dot(o,o) - r*r, h = b*b - c;',
    '  if (h < 0.0) return -1.0;',
    '  return -b - sqrt(h);',
    '}',

    /* Profil radial des anneaux : bandes d'or et d'argent séparées par des
       divisions vides. C'est le contraste entre les deux métaux qui donne la
       lecture « gazeux » — un anneau d'une seule teinte paraît peint. */
    'vec4 anneau(float r, float ang){',
    '  float R0 = 5.180, R1 = 9.100;',
    '  if (r < R0 || r > R1) return vec4(0.0);',
    '  float u = (r - R0) / (R1 - R0);',

    '  float bandes = fbm(vec3(r * 4.0625, ang * 0.6, 0.0));',
    '  float fin    = fbm(vec3(r * 19.0625, ang * 2.2, 4.0));',

    /* Deux divisions creusées dans l épaisseur, comme celles de Saturne : sans
       elles l anneau lit comme un disque uniforme. */
    '  float d1 = smoothstep(0.012, 0.052, abs(u - 0.34));',
    '  float d2 = smoothstep(0.008, 0.038, abs(u - 0.63));',
    '  float bord = smoothstep(0.0, 0.10, u) * smoothstep(1.0, 0.88, u);',

    '  float dens = bord * d1 * d2 * (0.42 + 0.58 * bandes) * (0.72 + 0.28 * fin);',
    '  dens = clamp(dens, 0.0, 1.0);',

    /* Or vers l intérieur, argent vers l extérieur, mélangés par le bruit pour
       que la transition ne soit pas une frontière nette. */
    '  vec3 orC     = vec3(0.86, 0.66, 0.26);',
    '  vec3 argentC = vec3(0.74, 0.79, 0.84);',
    '  float m = clamp(u * 1.15 + (bandes - 0.5) * 0.55, 0.0, 1.0);',
    '  vec3 col = mix(orC, argentC, m);',
    '  col *= 0.72 + 0.55 * fin;',
    '  return vec4(col, dens);',
    '}',

    'void main(){',
    '  vec2 uv = (gl_FragCoord.xy - 0.5*uRes) / uRes.y;',
    '  float etroit = step(uRes.x/uRes.y, 1.05);',
    '  uv -= mix(vec2(-0.32, 0.0), vec2(0.0, 0.30), etroit);',

    '  vec3 ro = vec3(uSouris.x * 0.448, uSouris.y * 0.352, 14.560);',
    '  vec3 rd = normalize(vec3(uv * 1.05, -1.0));',

    '  vec3 col = vec3(0.014, 0.028, 0.047);',
    '  col += vec3(0.30, 0.22, 0.06) * 0.075 * exp(-length(uv - vec2(-0.30, 0.28)) * 2.0);',
    '  col += vec3(0.03, 0.15, 0.26) * 0.075 * exp(-length(uv - vec2( 0.42,-0.34)) * 1.9);',

    /* étoiles */
    '  vec3 g = rd * 240.0; vec3 gi = floor(g), gf = fract(g);',
    '  float hh = hash13(gi);',
    '  if (hh > 0.9962) {',
    '    vec2 c = vec2(hash13(gi+7.1), hash13(gi+13.7));',
    '    float sc = smoothstep(0.30, 0.0, length(gf.xy - c));',
    '    col += vec3(0.82,0.88,1.0) * sc * (0.70 + 0.30*sin(uT*1.6 + hh*57.0)) * 0.75;',
    '  }',

    /* --- plan des anneaux : incliné, et qui bascule très lentement --- */
    '  mat3 M = rotZ(0.20) * rotX(-1.19 + sin(uT * 0.055) * 0.045);',
    '  vec3 N = M * vec3(0.0, 0.0, 1.0);',

    '  float dn = dot(rd, N);',
    '  float tR = -1.0; float rR = 0.0; float angR = 0.0;',
    '  if (abs(dn) > 0.0006) {',
    '    float t = -dot(ro, N) / dn;',
    '    if (t > 0.0) {',
    '      vec3 p = ro + rd * t;',
    '      vec3 l = p * M;',
    '      rR = length(l.xy);',
    '      angR = atan(l.y, l.x);',
    '      tR = t;',
    '    }',
    '  }',

    /* --- le logo : disque plat face à la caméra, à z = 0 --- */
    '  float RL = 3.760;',
    '  float tL = -1.0; vec2 uvL = vec2(0.0);',
    '  if (abs(rd.z) > 1e-5) {',
    '    float t = -ro.z / rd.z;',
    '    if (t > 0.0) {',
    '      vec3 p = ro + rd * t;',
    '      if (length(p.xy) < RL) { tL = t; uvL = p.xy / RL * 0.5 + 0.5; }',
    '    }',
    '  }',

    /* --- les lunes : elles suivent le plan des anneaux, mais en sortent et y
           replongent une fois par révolution. Le mouvement est celui d une
           baleine qui perce la surface : montée lente, sommet bref, plongée. --- */
    '  float tM = 1e9; vec3 colM = vec3(0.0); float glowM = 0.0;',
    '  for (int i = 0; i < 5; i++) {',
    '    float fi = float(i);',
    '    float rayon = 5.504 + fi * 0.672;',
    '    float vit   = 0.30 - fi * 0.031;',
    '    float ph    = fi * 1.257;',
    '    float a     = uT * vit + ph;',
    /*   Le saut : une bosse douce, une seule fois par tour. */
    '    float s = sin(a * 0.5 + ph);',
    '    float saut = pow(max(s, 0.0), 3.0) * 1.984;',
    '    vec3 pos = M * vec3(cos(a) * rayon, sin(a) * rayon, saut);',
    '    float rM = 0.1664 + fi * 0.0192;',
    '    float t = iSphere(ro, rd, pos, rM);',
    '    vec3 teinte = mix(vec3(0.95,0.80,0.40), vec3(0.80,0.86,0.92), fract(fi*0.37));',
    '    if (t > 0.0 && t < tM) {',
    '      vec3 n = normalize(ro + rd*t - pos);',
    '      float dif = max(dot(n, normalize(vec3(-0.5,0.6,0.7))), 0.0);',
    '      tM = t; colM = teinte * (0.34 + dif * 1.15);',
    '    }',
    /*   Halo : à cette taille l engin ne ferait que quelques pixels sans lui. */
    '    float tp = dot(pos - ro, rd);',
    '    if (tp > 0.0) {',
    '      float d = length(ro + rd*tp - pos);',
    '      glowM += exp(-d * 6.875) * 0.5 * (0.55 + 0.45 * saut / 1.984);',
    '    }',
    '  }',

    /* --- composition, du plus loin au plus proche --- */
    '  vec4 A = (tR > 0.0) ? anneau(rR, angR) : vec4(0.0);',

    /*   Anneau DERRIÈRE le logo : atténué, comme vu à travers l ombre portée. */
    '  if (tR > 0.0 && (tL < 0.0 || tR > tL)) {',
    '    float ombre = 1.0 - 0.34 * smoothstep(4.740, 3.100, rR);',
    '    col = mix(col, A.rgb * 0.84 * ombre, A.a);',
    '  }',

    /*   Le logo. */
    '  if (tL > 0.0) {',
    '    vec4 tex = texture2D(uLogo, vec2(uvL.x, 1.0 - uvL.y));',
    '    float r2 = length(uvL * 2.0 - 1.0);',
    '    float bordD = smoothstep(1.0, 0.965, r2);',
    '    vec3 c = tex.rgb;',
    /*     Liseré chaud sur le pourtour : détache le disque du fond sans toucher
           au dessin lui-même. */
    '    c += vec3(0.95, 0.72, 0.30) * smoothstep(0.86, 1.0, r2) * 0.42;',
    '    col = mix(col, c, bordD);',
    '  }',

    /*   Anneau DEVANT le logo : pleine intensité. */
    '  if (tR > 0.0 && tL > 0.0 && tR < tL) {',
    '    col = mix(col, A.rgb, A.a * 0.92);',
    '  } else if (tR > 0.0 && tL < 0.0) {',
    '    col = mix(col, A.rgb, A.a * 0.0);',
    '  }',

    /*   Les lunes passent devant tout ce qui est plus loin qu elles. */
    '  if (tM < 1e8) {',
    '    float devantLogo = (tL < 0.0 || tM < tL) ? 1.0 : 0.0;',
    '    float devantAnn  = (tR < 0.0 || tM < tR) ? 1.0 : 0.0;',
    '    if (devantLogo > 0.5 && devantAnn > 0.5) col = colM;',
    '  }',
    '  col += mix(vec3(0.95,0.78,0.36), vec3(0.55,0.75,1.0), 0.4) * glowM * 0.42;',

    /*   Halo général autour du noyau. */
    '  float dl = length(uv);',
    '  col += vec3(0.26, 0.20, 0.09) * exp(-max(dl - 0.86, 0.0) * 4.2) * 0.34;',

    '  col *= 1.0 - 0.30 * pow(length((gl_FragCoord.xy/uRes - 0.5) * vec2(1.05,1.0)), 2.3);',
    '  col = pow(max(col, 0.0), vec3(0.4545));',
    '  col += (hash13(vec3(gl_FragCoord.xy, uT*60.0)) - 0.5) * 0.015;',
    '  gl_FragColor = vec4(col, 1.0);',
    '}'
  ].join('\n');

  var VS = 'attribute vec2 aPos; void main(){ gl_Position = vec4(aPos,0.0,1.0); }';

  function compiler(t, src) {
    var s = gl.createShader(t); gl.shaderSource(s, src); gl.compileShader(s);
    if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) { console.error('planète :', gl.getShaderInfoLog(s)); return null; }
    return s;
  }
  var vs = compiler(gl.VERTEX_SHADER, VS), fs = compiler(gl.FRAGMENT_SHADER, FS);
  if (!vs || !fs) { canvas.style.display = 'none'; return; }

  var prog = gl.createProgram();
  gl.attachShader(prog, vs); gl.attachShader(prog, fs); gl.linkProgram(prog); gl.useProgram(prog);

  var buf = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, buf);
  gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1,-1, 3,-1, -1,3]), gl.STATIC_DRAW);
  var loc = gl.getAttribLocation(prog, 'aPos');
  gl.enableVertexAttribArray(loc);
  gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);

  var U = {};
  ['uRes','uT','uSouris','uLogo'].forEach(function (n) { U[n] = gl.getUniformLocation(prog, n); });

  /* --- le logo, en texture ---
     Il est embarqué en data-URI : la politique de sécurité de contenu du site
     interdit toute ressource externe, et une image chargée depuis un autre
     domaine « salirait » la texture au sens WebGL. */
  var tex = gl.createTexture();
  gl.bindTexture(gl.TEXTURE_2D, tex);
  gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE, new Uint8Array([10,20,32,255]));
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
  gl.uniform1i(U.uLogo, 0);

  var img = new Image();
  img.onload = function () {
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, img);
    // PAS de generateMipmap ici. Le logo fait 732x732 : ce n'est pas une puissance
    // de deux, et en WebGL 1 une telle texture devient INCOMPLÈTE dès qu'on lui
    // demande des mipmaps — elle s'échantillonne alors en noir, sans la moindre
    // erreur signalée. Le filtrage linéaire simple suffit largement ici : le disque
    // est rendu plus petit que la source, jamais agrandi.
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
  };
  img.src = '/assets/logo.jpg';

  function redim() {
    var dpr = Math.min(window.devicePixelRatio || 1, 1.6);
    var w = Math.floor(canvas.clientWidth * dpr), h = Math.floor(canvas.clientHeight * dpr);
    if (canvas.width !== w || canvas.height !== h) { canvas.width = w; canvas.height = h; gl.viewport(0,0,w,h); }
    gl.uniform2f(U.uRes, canvas.width, canvas.height);
  }

  var cible = {x:0,y:0}, cour = {x:0,y:0};
  window.addEventListener('pointermove', function (e) {
    cible.x = (e.clientX / window.innerWidth - 0.5) * 2;
    cible.y = (0.5 - e.clientY / window.innerHeight) * 2;
  }, { passive: true });

  var t0 = null, raf = null, visible = false;
  function rendu(ms) {
    raf = requestAnimationFrame(rendu);
    if (!visible) return;
    if (t0 === null) t0 = ms;
    cour.x += (cible.x - cour.x) * 0.045;
    cour.y += (cible.y - cour.y) * 0.045;
    redim();
    gl.useProgram(prog);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, tex);
    var force = new URLSearchParams(location.search).get('tp');
    gl.uniform1f(U.uT, force !== null ? parseFloat(force) : (reduit.matches ? 9.0 : (ms - t0) / 1000));
    gl.uniform2f(U.uSouris, cour.x, cour.y);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
    if (reduit.matches) { cancelAnimationFrame(raf); raf = null; }
  }

  /* La scène n'est dessinée que lorsqu'elle est à l'écran : deux canvas WebGL
     qui tournent en même temps sur la même page doubleraient le coût pour rien. */
  if ('IntersectionObserver' in window) {
    new IntersectionObserver(function (e) {
      visible = e[0].isIntersecting;
      if (visible && raf === null) { t0 = null; raf = requestAnimationFrame(rendu); }
    }, { threshold: 0.02 }).observe(canvas);
  } else { visible = true; }

  window.addEventListener('resize', redim, { passive: true });
  redim();
  raf = requestAnimationFrame(rendu);
})();

