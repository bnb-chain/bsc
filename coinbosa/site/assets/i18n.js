/* ═══════════════════════════════════════════════════════════════════════════
   TRADUCTION DU SITE

   Le site est servi en HTML statique : cinq fichiers, aucune compilation. Le
   texte source est en français, directement dans les pages. Ce fichier ne
   remplace pas ce texte : il le remplace À L'AFFICHAGE, à partir d'un
   dictionnaire chargé pour la langue demandée.

   POURQUOI CE MONTAGE

   La politique de sécurité du site interdit tout script en ligne
   (script-src 'self'). Une bibliothèque de CDN est donc impossible, et le
   dictionnaire ne peut pas être collé dans la page. Chaque langue vit dans son
   propre fichier .js, chargé À LA DEMANDE par injection d'une balise <script> —
   la seule voie que la CSP autorise sans élargir la politique.

   Le FRANÇAIS n'a pas de fichier. Il est déjà dans le HTML : on le photographie
   au chargement, et revenir au français consiste à restaurer cette photo. Cela
   évite d'expédier deux fois le même texte.

   ORDRE DE DÉCISION DE LA LANGUE, du plus explicite au plus général :
     1. ?lang=xx dans l'URL — permet de PARTAGER un lien dans une langue donnée.
        Sans cela, un lien envoyé à une bourse s'ouvrirait dans la langue du
        destinataire, jamais dans la nôtre.
     2. le choix déjà fait par le visiteur sur ce navigateur ;
     3. la langue du navigateur, si nous la parlons ;
     4. l'ANGLAIS. Pas le français : le marché visé est international, et un
        visiteur allemand, russe ou japonais ne doit pas recevoir du français.
   ═══════════════════════════════════════════════════════════════════════════ */

(function () {
  "use strict";

  var LANGUES = {
    fr: { nom: "Français",   locale: "fr",    rtl: false },
    en: { nom: "English",    locale: "en",    rtl: false },
    es: { nom: "Español",    locale: "es",    rtl: false },
    pt: { nom: "Português",  locale: "pt",    rtl: false },
    ar: { nom: "العربية",     locale: "ar",    rtl: true  },
    zh: { nom: "中文",         locale: "zh-CN", rtl: false }
  };
  var SOURCE = "fr";            // la langue écrite dans le HTML
  var MEMOIRE = "coinbosa.lang";

  window.__I18N = window.__I18N || {};

  /* ── La photo du français ──────────────────────────────────────────────
     Prise AVANT toute traduction. Sans elle, passer en anglais puis revenir
     au français afficherait les clés au lieu du texte. */
  var photo = { html: {}, attrs: {}, titre: document.title };

  function photographier() {
    var n = document.querySelectorAll("[data-i18n]");
    for (var i = 0; i < n.length; i++) {
      var c = n[i].getAttribute("data-i18n");
      // On ne REphotographie jamais une cle deja connue : au second passage le
      // DOM porte deja la traduction, et l'ecraser ferait passer l'anglais pour
      // la source francaise. Le retour au francais afficherait alors l'anglais.
      if (!(c in photo.html)) photo.html[c] = n[i].innerHTML;
    }
    var t = document.querySelectorAll("*");
    for (var j = 0; j < t.length; j++) {
      var at = t[j].attributes;
      for (var k = 0; k < at.length; k++) {
        var nom = at[k].name;
        if (nom.indexOf("data-i18n-attr-") === 0) {
          var cible = nom.slice("data-i18n-attr-".length);
          photo.attrs[at[k].value] = t[j].getAttribute(cible);
        }
      }
    }
  }

  /* ── Choix de la langue ───────────────────────────────────────────────── */
  function choisir() {
    var p = null;
    try { p = new URLSearchParams(location.search).get("lang"); } catch (e) {}
    if (p && LANGUES[p]) return p;
    var m = null;
    try { m = localStorage.getItem(MEMOIRE); } catch (e) {}
    if (m && LANGUES[m]) return m;
    var nav = navigator.languages || [navigator.language || ""];
    for (var i = 0; i < nav.length; i++) {
      var deux = String(nav[i]).slice(0, 2).toLowerCase();
      if (LANGUES[deux]) return deux;
    }
    return "en";
  }

  /* ── Application ──────────────────────────────────────────────────────── */
  function appliquer(code) {
    var cfg = LANGUES[code];
    if (!cfg) return;
    var dico = code === SOURCE ? null : (window.__I18N[code] || null);

    // Une langue demandée dont le dictionnaire n'est pas là : on ne fait RIEN
    // plutôt que d'afficher un site à moitié traduit.
    if (code !== SOURCE && !dico) return;

    document.documentElement.setAttribute("lang", cfg.locale);
    document.documentElement.setAttribute("dir", cfg.rtl ? "rtl" : "ltr");

    var n = document.querySelectorAll("[data-i18n]");
    for (var i = 0; i < n.length; i++) {
      var cle = n[i].getAttribute("data-i18n");
      var v = dico ? dico[cle] : photo.html[cle];
      // Une clé absente du dictionnaire retombe sur le français plutôt que de
      // laisser un trou : un texte non traduit se lit, une case vide non.
      if (v == null) v = photo.html[cle];
      if (v != null && n[i].innerHTML !== v) n[i].innerHTML = v;
    }

    var t = document.querySelectorAll("*");
    for (var j = 0; j < t.length; j++) {
      var at = t[j].attributes;
      for (var k = 0; k < at.length; k++) {
        if (at[k].name.indexOf("data-i18n-attr-") !== 0) continue;
        var cible = at[k].name.slice("data-i18n-attr-".length);
        var c2 = at[k].value;
        var w = dico ? dico[c2] : photo.attrs[c2];
        if (w == null) w = photo.attrs[c2];
        if (w != null) t[j].setAttribute(cible, w);
      }
    }

    var tt = document.querySelector("title[data-i18n]");
    if (tt) document.title = tt.textContent;

    var sel = document.getElementById("lang");
    if (sel && sel.value !== code) sel.value = code;

    document.documentElement.setAttribute("data-lang", code);

    /* app.js reformate les cellules chiffrees qu aucune cle ne couvre — les
       montants de la tokenomique, les tuiles de chiffres. Il doit le refaire
       APRES chaque application de langue, sinon les nombres restent au format
       de la langue precedente. On previent plutot que d appeler : ce fichier
       ne doit rien savoir de app.js, qui peut etre absent d une page. */
    try {
      document.dispatchEvent(new CustomEvent("coinbosa:i18n", { detail: code }));
    } catch (e) {
      /* CustomEvent absent : le site reste utilisable, seuls les separateurs
         de milliers gardent la forme francaise. */
    }
  }

  /* ── Chargement d'une langue, puis application ─────────────────────────── */
  var enCours = {};

  function charger(code, fini) {
    if (code === SOURCE || window.__I18N[code]) return fini();
    if (enCours[code]) { enCours[code].push(fini); return; }
    enCours[code] = [fini];
    var s = document.createElement("script");
    // L'empreinte est réécrite par coque.py en même temps que les autres.
    s.src = "/assets/i18n-" + code + ".js";
    s.async = true;
    s.onload = function () {
      var f = enCours[code]; enCours[code] = null;
      for (var i = 0; i < f.length; i++) f[i]();
    };
    s.onerror = function () {
      // Le fichier manque ou n'a pas pu être servi : on reste dans la langue
      // courante. Mieux vaut un site lisible qu'un site vide.
      enCours[code] = null;
      if (window.console) console.warn("i18n : dictionnaire " + code + " indisponible");
    };
    document.head.appendChild(s);
  }

  function basculer(code, memoriser) {
    charger(code, function () {
      appliquer(code);
      if (memoriser) { try { localStorage.setItem(MEMOIRE, code); } catch (e) {} }
    });
  }

  /* ── Le sélecteur ─────────────────────────────────────────────────────── */
  function selecteur(courante) {
    var sel = document.getElementById("lang");
    if (!sel) return;
    var html = "";
    for (var c in LANGUES) {
      if (!LANGUES.hasOwnProperty(c)) continue;
      html += '<option value="' + c + '"' + (c === courante ? " selected" : "") +
              ">" + LANGUES[c].nom + "</option>";
    }
    sel.innerHTML = html;
    sel.addEventListener("change", function (e) { basculer(e.target.value, true); });
  }

  /* ── Rappel pour le contenu genere apres le chargement ────────────────
     app.js fabrique une partie de l'interface — la grille des produits, les
     faits du reseau, l'entree « Explorateur » du menu — apres que ce fichier a
     deja fait son travail. Ces elements naissaient donc toujours en francais,
     quelle que soit la langue affichee. app.js appelle cette fonction apres
     chaque rendu ; elle photographie les nouveaux venus, puis les traduit. */
  window.CoinbosaI18n = {
    rafraichir: function () {
      photographier();
      appliquer(document.documentElement.getAttribute("data-lang") || SOURCE);
    },
    langue: function () {
      return document.documentElement.getAttribute("data-lang") || SOURCE;
    }
  };

  function demarrer() {
    photographier();
    var code = choisir();
    selecteur(code);
    if (code !== SOURCE) basculer(code, false);
    else appliquer(SOURCE);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", demarrer);
  } else {
    demarrer();
  }
})();
