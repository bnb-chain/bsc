/* ═══════════════════════════════════════════════════════════════════════════
   LIVRE BLANC — le peu de script dont la page a besoin.

   L'en-tête, le pied, le thème et le menu sont déjà pris en charge par /app.js,
   partagé avec le reste du site. Ce fichier ne fait donc qu'une chose : câbler
   le bouton d'impression.

   Il reste externe parce que la politique de sécurité du site interdit tout
   script en ligne (script-src 'self'). Un onclick= serait silencieusement mort
   en production : la page s'afficherait, le bouton ne ferait rien, et rien dans
   l'interface ne dirait pourquoi.
   ═══════════════════════════════════════════════════════════════════════════ */

(function () {
  "use strict";

  var b = document.getElementById("imprimer");
  // La garde n'est pas décorative : ce fichier est chargé par la version
  // française ET par la version anglaise, et une page qui perdrait ce bouton
  // lèverait une exception au chargement — laquelle interromprait tout le
  // script, y compris ce qui viendrait après.
  if (!b) return;

  b.addEventListener("click", function () { window.print(); });

  /* La version anglaise a sa propre URL et sa coque traduite EN DUR. Mais
     /app.js, partage avec le reste du site, injecte a l'execution l'entree
     « Explorateur » du menu depuis ses libelles francais : on lisait donc un mot
     francais au milieu d'une barre de navigation anglaise. On le corrige APRES
     son passage, faute de pouvoir l'en empecher sans dupliquer tout le script. */
  if (document.documentElement.lang === "en") {
    var corrige = function () {
      var liens = document.querySelectorAll(".nav a, .mobile-menu a");
      for (var i = 0; i < liens.length; i++) {
        if (liens[i].textContent.trim() === "Explorateur") liens[i].textContent = "Explorer";
      }
    };
    corrige();
    // app.js s'execute sur DOMContentLoaded comme ce fichier : l'ordre de
    // chargement n'est pas garanti, d'ou une seconde passe differee.
    setTimeout(corrige, 60);
  }
})();
