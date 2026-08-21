  // 1) Thème appliqué AVANT le rendu (anti-flash), de façon synchrone.
(function () {
    try {
      var t = localStorage.getItem('coinbosa-theme');
      if (t !== 'light' && t !== 'dark') t = 'dark';
      document.documentElement.setAttribute('data-theme', t);
    } catch (e) {
      document.documentElement.setAttribute('data-theme', 'dark');
    }
  })();

  // 2) Contenu éditable (voir en tête de bloc).
/* ================================================================
   CONTENU ÉDITABLE — modifiez ce bloc SANS toucher au HTML.
   ----------------------------------------------------------------
   · links   : une valeur VIDE ("") masque automatiquement le lien.
   · products: la grille ET les pastilles d'état sont rendues d'ici.
               Changer un statut ("building" -> "live") = UNE ligne.
               status ∈ "live" | "building" | "soon" | "external"
   · network : alimente la section « socle » (faits chaîne + BOSA).
   ================================================================ */
var CONTENT = {

  links: {
    website:           "https://coinbosa.com", // site principal
    explorer:          "https://explorer.coinbosa.com",   // explorateur de la chaîne (sobre, façon Solscan)
    whitepaper:        "https://coinbosa.com/whitepaper/",   // livre blanc (hébergé sur le site)
    github:            "https://github.com/Coinbosa/coinbosa-chain",   // dépôt(s)
    facebook:          "https://www.facebook.com/coinbosa",   // page Facebook
    telegram:          "https://t.me/coinbosa",   // canal Telegram officiel
    telegramCommunity: "https://t.me/Coinbosaofficial",   // groupe communautaire Telegram
    twitter:           "",   // X / Twitter
    discord:           ""    // serveur Discord
  },

  // Entité éditrice & copyright — rendu du pied de page depuis ici.
  entity: {
    name:         "coinbosa, Inc.",
    jurisdiction: "Delaware, United States",
    copyright:    "© 2022–2026 coinbosa, Inc."
  },

  // Statuts affichés HONNÊTEMENT. Ordre = ordre d'affichage.
  products: [
    {
      name: "Coinbosa Academy",
      category: "Éducation",
      status: "live",
      url: "https://coinbosa-academy.com",
      desc: "École de trading et d'éducation aux marchés financiers. Disponible aujourd'hui."
    },
    {
      name: "NextFuture",
      category: "Marchés",
      status: "live",
      url: "https://nexfutur.com",
      desc: "Bourse spot & futures, opérationnelle. Spot, futures, copy trading, P2P."
    },
    {
      name: "Coinbosa Card",
      category: "Paiements",
      status: "soon",
      url: "https://coinbosa.cards",
      desc: "Carte de paiement virtuelle et physique. Site en ligne ; paiements pas encore actifs."
    },
    {
      name: "Coinbosa VPN",
      category: "Confidentialité",
      status: "building",
      // url retirée : le domaine sert un certificat TLS qui ne correspond pas à
      // bosavpn.com — le visiteur recevait un avertissement de sécurité plein
      // écran depuis un lien de coinbosa.com. À rétablir dès que le certificat
      // est corrigé : il suffit de remettre cette ligne.
      // url: "https://bosavpn.com",
      desc: "Réseau privé et confidentialité en ligne. En développement."
    },
    {
      name: "BOSA Omni AI",
      category: "Intelligence artificielle",
      status: "building",
      desc: "Un lieu de travail, pas un modèle de plus : étudiants, entrepreneurs, marketeurs, créateurs, développeurs. Chacun apporte ses propres clés, tout s'exécute sur sa machine. En construction."
    },
    {
      name: "Neobanq",
      category: "Banque",
      status: "building",
      desc: "Plateforme bancaire de microfinance. Projet distinct, en développement."
    }
    ],

  // Le socle. Ces faits sont vérifiables — ne rien inventer.
  network: {
    name:          "Coinbosa Chain",
    evm:           "Compatible EVM",
    consensus:     "Parlia · PoSA",
    chainId:       "26262",
    blockTime:     "5 s",
    coin:          "BOSA",
    decimals:      "18",
    supply:        "700 000 000",
    supplyNote:    "offre fixe, aucune émission",
    tokenStandard: "BRC20 · compatible ERC-20"
  }
};

  // 3) Logique applicative — exécutée une fois le DOM prêt. Un seul script pour toute la page.
  document.addEventListener("DOMContentLoaded", function () {
(function () {
  "use strict";

  var reduceMQ = window.matchMedia('(prefers-reduced-motion: reduce)');

  /* ---------- helpers ---------- */
  function el(tag, cls, html) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (html != null) n.innerHTML = html;
    return n;
  }
  function esc(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  var STATUS = {
    live:     { label: "En production",  cls: "st-live" },
    building: { label: "En construction", cls: "st-building" },
    soon:     { label: "À venir",         cls: "st-soon" },
    external: { label: "Externe",         cls: "st-external" }
  };

  var ARROW = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M5 12h14M13 6l6 6-6 6" stroke-linecap="round" stroke-linejoin="round"/></svg>';
  var EXTLINK = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M14 5h5v5M19 5l-8 8M18 13.5V18a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h4.5" stroke-linecap="round" stroke-linejoin="round"/></svg>';

  /* ---------- icônes sociales (inline, aucune dépendance) ---------- */
  var ICONS = {
    website:  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.5 2.5 2.5 15 0 18M12 3c-2.5 2.5-2.5 15 0 18"/></svg>',
    explorer: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><circle cx="11" cy="11" r="7"/><path d="M20 20l-3.6-3.6" stroke-linecap="round"/></svg>',
    whitepaper:'<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><path d="M7 3h7l4 4v14H7z" stroke-linejoin="round"/><path d="M14 3v4h4M9.5 13h5M9.5 16.5h5" stroke-linecap="round"/></svg>',
    github:   '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 2a10 10 0 0 0-3.16 19.49c.5.09.68-.22.68-.48v-1.7c-2.78.6-3.37-1.34-3.37-1.34-.45-1.16-1.11-1.47-1.11-1.47-.91-.62.07-.6.07-.6 1 .07 1.53 1.03 1.53 1.03.9 1.53 2.36 1.09 2.94.83.09-.65.35-1.09.63-1.34-2.22-.25-4.55-1.11-4.55-4.94 0-1.09.39-1.98 1.03-2.68-.1-.25-.45-1.27.1-2.65 0 0 .84-.27 2.75 1.02a9.5 9.5 0 0 1 5 0c1.91-1.29 2.75-1.02 2.75-1.02.55 1.38.2 2.4.1 2.65.64.7 1.03 1.59 1.03 2.68 0 3.84-2.34 4.68-4.57 4.93.36.31.68.92.68 1.85v2.74c0 .27.18.58.69.48A10 10 0 0 0 12 2Z"/></svg>',
    facebook: '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M22 12a10 10 0 1 0-11.56 9.88v-6.99H7.9V12h2.54V9.8c0-2.5 1.49-3.89 3.78-3.89 1.09 0 2.24.2 2.24.2v2.46h-1.26c-1.24 0-1.63.77-1.63 1.56V12h2.78l-.44 2.89h-2.34v6.99A10 10 0 0 0 22 12Z"/></svg>',
    telegram: '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M21.94 4.3 18.7 19.6c-.24 1.08-.88 1.34-1.78.84l-4.92-3.63-2.37 2.28c-.26.26-.48.48-.99.48l.35-5.02 9.13-8.25c.4-.35-.09-.55-.62-.2L4.62 13.1l-4.86-1.52c-1.06-.33-1.08-1.06.22-1.57L20.57 2.8c.88-.33 1.65.2 1.37 1.5Z"/></svg>',
    telegramCommunity: '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M21.94 4.3 18.7 19.6c-.24 1.08-.88 1.34-1.78.84l-4.92-3.63-2.37 2.28c-.26.26-.48.48-.99.48l.35-5.02 9.13-8.25c.4-.35-.09-.55-.62-.2L4.62 13.1l-4.86-1.52c-1.06-.33-1.08-1.06.22-1.57L20.57 2.8c.88-.33 1.65.2 1.37 1.5Z"/></svg>',
    twitter:  '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M18.24 2.25h3.31l-7.23 8.26 8.5 11.24h-6.65l-5.21-6.82-5.96 6.82H1.68l7.73-8.84L1.25 2.25h6.82l4.71 6.23 5.46-6.23Zm-1.16 17.52h1.83L7.01 4.13H5.04l12.04 15.64Z"/></svg>',
    discord:  '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M20.32 4.57A19.8 19.8 0 0 0 15.4 3.05l-.24.5a18.3 18.3 0 0 1 4.34 1.35 16.4 16.4 0 0 0-13-.02A18 18 0 0 1 8.85 3.5l-.25-.45A19.7 19.7 0 0 0 3.68 4.57C.9 8.68.14 12.68.52 16.63a19.9 19.9 0 0 0 6.06 3.05l.79-1.28c-.66-.25-1.28-.55-1.87-.9.16-.11.31-.23.46-.35a14.2 14.2 0 0 0 12.08 0c.15.13.3.24.46.35-.59.35-1.21.65-1.87.9l.79 1.28a19.8 19.8 0 0 0 6.06-3.05c.45-4.58-.76-8.55-3.6-12.06ZM8.68 14.2c-1.18 0-2.15-1.08-2.15-2.4 0-1.33.95-2.41 2.15-2.41s2.17 1.09 2.15 2.4c0 1.33-.95 2.41-2.15 2.41Zm6.64 0c-1.18 0-2.15-1.08-2.15-2.4 0-1.33.95-2.41 2.15-2.41s2.17 1.09 2.15 2.4c0 1.33-.94 2.41-2.15 2.41Z"/></svg>'
  };
  var LABELS = {
    website: "Site", explorer: "Explorateur", whitepaper: "Whitepaper", github: "GitHub",
    facebook: "Facebook", telegram: "Telegram", telegramCommunity: "Communauté Telegram",
    twitter: "X (Twitter)", discord: "Discord"
  };
  function isExt(url) { return /^https?:\/\//i.test(url); }
  function anchorAttrs(url) { return isExt(url) ? ' target="_blank" rel="noopener noreferrer"' : ''; }

  /* ================= RENDU : marquee ================= */
  (function renderMarquee() {
    var track = document.getElementById('marquee-track');
    if (!track) return;
    var one = '';
    CONTENT.products.forEach(function (p) {
      one += '<span class="marquee-item"><span class="dot"></span>' + esc(p.name) +
             ' <span class="cat">' + esc(p.category) + '</span></span>';
    });
    one += '<span class="marquee-item"><span class="dot"></span>' + esc(CONTENT.network.name) +
           ' <span class="cat">Le socle</span></span>';
    // dupliqué pour un défilement continu
    track.innerHTML = one + one;
  })();

  /* ================= RENDU : grille produits ================= */
  (function renderProducts() {
    var grid = document.getElementById('product-grid');
    if (!grid) return;
    CONTENT.products.forEach(function (p, i) {
      var st = STATUS[p.status] || STATUS.soon;
      var hasUrl = p.url && String(p.url).trim() !== '';
      // Carte cliquable : l'élément carte devient un lien si une url est fournie.
      var card = el(hasUrl ? 'a' : 'article', 'card reveal' + (hasUrl ? ' card-linked' : ''));
      card.style.transitionDelay = (Math.min(i, 6) * 0.06) + 's';
      var html =
        '<div class="card-top">' +
          '<span class="card-cat">' + esc(p.category) + '</span>' +
          '<span class="badge ' + st.cls + '"><span class="pip"></span>' + esc(st.label) + '</span>' +
        '</div>' +
        '<h3>' + esc(p.name) + '</h3>' +
        '<p>' + esc(p.desc) + '</p>';
      if (hasUrl) {
        var aff = isExt(p.url) ? EXTLINK : ARROW;
        html += '<span class="card-link">Découvrir ' + aff + '</span>';
        card.setAttribute('href', /^(https?:\/\/|\/|#)/.test(p.url) ? p.url : '#');
        card.setAttribute('aria-label', p.name + ' — ouvrir le site' + (isExt(p.url) ? ' (nouvel onglet)' : ''));
        if (isExt(p.url)) { card.target = '_blank'; card.rel = 'noopener noreferrer'; }
      }
      card.innerHTML = html;
      grid.appendChild(card);
    });
  })();

  /* ================= RENDU : socle (stats) ================= */
  (function renderNetwork() {
    var g = document.getElementById('stat-grid');
    if (!g) return;
    var n = CONTENT.network;
    var stats = [
      { k: "Machine",     v: esc(n.evm),           hl: false },
      { k: "Consensus",   v: esc(n.consensus),     hl: false },
      { k: "Chain ID",    v: esc(n.chainId),       hl: false },
      { k: "Temps de bloc", v: esc(n.blockTime),   hl: false },
      { k: "Coin natif",  v: esc(n.coin) + '<small>' + esc(n.decimals) + ' décimales</small>', hl: true },
      { k: "Offre totale", v: esc(n.supply) + '<small>' + esc(n.supplyNote) + '</small>', hl: true },
      { k: "Standard de jeton", v: esc(n.tokenStandard), hl: false }
    ];
    stats.forEach(function (s) {
      var d = el('div', 'stat' + (s.hl ? ' hl' : ''));
      d.innerHTML = '<div class="k">' + esc(s.k) + '</div><div class="v">' + s.v + '</div>';
      g.appendChild(d);
    });
  })();

  /* ================= RENDU : liens (nav, join, footer) ================= */
  (function renderLinks() {
    var L = CONTENT.links;

    // Nav : lien explorateur (si présent)
    if (L.explorer) {
      var navHtml = '<a href="' + esc(/^(https?:\/\/|\/|#)/.test(L.explorer) ? L.explorer : '#') + '"' + anchorAttrs(L.explorer) + '>Explorateur</a>';
      var ne = document.getElementById('nav-extra');
      var mne = document.getElementById('mnav-extra');
      if (ne) ne.outerHTML = navHtml;
      if (mne) mne.outerHTML = navHtml;
    } else {
      var ne2 = document.getElementById('nav-extra'); if (ne2) ne2.remove();
      var mne2 = document.getElementById('mnav-extra'); if (mne2) mne2.remove();
    }

    // Section « Rejoindre »
    var join = document.getElementById('join-cta');
    if (join) {
      var order = ['website', 'telegram', 'telegramCommunity', 'twitter', 'discord', 'github'];
      var made = 0;
      order.forEach(function (k) {
        if (!L[k] || made >= 3) return;
        var primary = (made === 0);
        var a = el('a', 'btn ' + (primary ? 'btn-primary' : 'btn-ghost'));
        a.href = /^(https?:\/\/|\/|#)/.test(L[k]) ? L[k] : '#'; a.textContent = LABELS[k];
        if (isExt(L[k])) { a.target = '_blank'; a.rel = 'noopener noreferrer'; }
        join.appendChild(a);
        made++;
      });
      if (made === 0) {
        join.appendChild(el('p', 'lead', 'Les canaux officiels seront publiés prochainement.'));
      }
    }

    // Footer : icônes sociales
    var soc = document.getElementById('ft-socials');
    if (soc) {
      ['website', 'facebook', 'telegram', 'telegramCommunity', 'twitter', 'discord', 'github'].forEach(function (k) {
        if (!L[k]) return;
        var a = el('a', null, ICONS[k] || '');
        a.href = /^(https?:\/\/|\/|#)/.test(L[k]) ? L[k] : '#'; a.setAttribute('aria-label', LABELS[k]); a.title = LABELS[k];
        if (isExt(L[k])) { a.target = '_blank'; a.rel = 'noopener noreferrer'; }
        soc.appendChild(a);
      });
    }

    // Footer : colonne Réseau
    fillFooterCol('ft-network', ['explorer', 'whitepaper', 'github']);
    // Footer : colonne Communauté
    fillFooterCol('ft-community', ['facebook', 'telegram', 'telegramCommunity', 'twitter', 'discord']);

    function fillFooterCol(id, keys) {
      var ul = document.getElementById(id);
      if (!ul) return;
      var added = 0;
      keys.forEach(function (k) {
        if (!L[k]) return;
        var li = el('li');
        var a = el('a', null, esc(LABELS[k]));
        a.href = /^(https?:\/\/|\/|#)/.test(L[k]) ? L[k] : '#';
        if (isExt(L[k])) { a.target = '_blank'; a.rel = 'noopener noreferrer'; }
        li.appendChild(a); ul.appendChild(li); added++;
      });
      if (added === 0) ul.appendChild(el('li', null, '<span class="soon">À venir</span>'));
    }
  })();

  /* ================= RENDU : pied de page (écosystème + entité) ================= */
  (function renderFooterMeta() {
    // Colonne « Écosystème » : dérivée de CONTENT.products (map sur les noms,
    // évite toute dérive au renommage). On garde les produits Coinbosa (hors partenaire externe).
    var eco = document.getElementById('ft-eco');
    if (eco) {
      CONTENT.products.forEach(function (p) {
        if (p.status === 'external') return;
        var li = el('li');
        var a = el('a', null, esc(p.name));
        a.href = '/ecosysteme.html';
        li.appendChild(a);
        eco.appendChild(li);
      });
    }
    // Copyright & entité éditrice : depuis CONTENT.entity.
    var e = CONTENT.entity || {};
    var cp = document.getElementById('ft-copyright');
    if (cp && e.copyright) cp.textContent = e.copyright;
    var ent = document.getElementById('ft-entity');
    if (ent && e.name) {
      ent.textContent = 'Édité par ' + e.name + (e.jurisdiction ? ', ' + e.jurisdiction : '') + '.';
    }
  })();

  /* ================= THÈME (bascule persistante) ================= */
  (function themeToggle() {
    var btn = document.getElementById('theme-toggle');
    if (!btn) return;
    function sync() {
      var t = document.documentElement.getAttribute('data-theme') || 'dark';
      btn.setAttribute('aria-pressed', t === 'light' ? 'true' : 'false');
      btn.setAttribute('aria-label', t === 'light' ? 'Activer le thème sombre' : 'Activer le thème clair');
    }
    sync();
    btn.addEventListener('click', function () {
      var cur = document.documentElement.getAttribute('data-theme') || 'dark';
      var next = cur === 'light' ? 'dark' : 'light';
      document.documentElement.setAttribute('data-theme', next);
      try { localStorage.setItem('coinbosa-theme', next); } catch (e) {}
      sync();
    });
  })();

  /* ================= MENU MOBILE (accessible) ================= */
  (function mobileMenu() {
    var btn = document.getElementById('menu-btn');
    var menu = document.getElementById('mobile-menu');
    if (!btn || !menu) return;
    function close() { menu.classList.remove('open'); btn.setAttribute('aria-expanded', 'false'); btn.setAttribute('aria-label', 'Ouvrir le menu'); }
    function open() { menu.classList.add('open'); btn.setAttribute('aria-expanded', 'true'); btn.setAttribute('aria-label', 'Fermer le menu'); }
    btn.addEventListener('click', function () {
      if (menu.classList.contains('open')) close(); else open();
    });
    menu.addEventListener('click', function (e) { if (e.target.closest('a')) close(); });
    document.addEventListener('keydown', function (e) { if (e.key === 'Escape' && menu.classList.contains('open')) { close(); btn.focus(); } });
    document.addEventListener('click', function (e) {
      if (menu.classList.contains('open') && !menu.contains(e.target) && !btn.contains(e.target)) close();
    });
  })();

  /* ================= EN-TÊTE : ombre au défilement ================= */
  (function headerShadow() {
    var hdr = document.getElementById('top');
    if (!hdr) return;
    function onScroll() { hdr.classList.toggle('scrolled', window.scrollY > 8); }
    onScroll();
    window.addEventListener('scroll', onScroll, { passive: true });
  })();

  /* ================= RÉVÉLATIONS AU DÉFILEMENT ================= */
  (function reveals() {
    var items = document.querySelectorAll('.reveal');
    if (reduceMQ.matches || !('IntersectionObserver' in window)) {
      items.forEach(function (n) { n.classList.add('in'); });
      return;
    }
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (e.isIntersecting) { e.target.classList.add('in'); io.unobserve(e.target); }
      });
    }, { threshold: 0.14, rootMargin: '0px 0px -6% 0px' });
    items.forEach(function (n) { io.observe(n); });
  })();

  /* ================= HERO : léger parallaxe au défilement (UN seul effet scroll-driven) =====
     Transform uniquement (compositor-friendly), borné, coupé si prefers-reduced-motion.
     ========================================================================================= */
  (function heroParallax() {
    if (reduceMQ.matches) return;
    var inner = document.querySelector('.hero-inner');
    var hero = document.querySelector('.hero');
    if (!inner || !hero) return;
    var ticking = false, active = true;
    if ('IntersectionObserver' in window) {
      new IntersectionObserver(function (e) { active = e[0].isIntersecting; }, { threshold: 0 }).observe(hero);
    }
    function update() {
      ticking = false;
      if (!active) return;
      var y = window.scrollY || window.pageYOffset || 0;
      var shift = Math.min(y * 0.14, 80); // doux et borné
      inner.style.transform = 'translate3d(0,' + shift.toFixed(1) + 'px,0)';
    }
    window.addEventListener('scroll', function () {
      if (!ticking) { ticking = true; requestAnimationFrame(update); }
    }, { passive: true });
    update();
    if (reduceMQ.addEventListener) {
      reduceMQ.addEventListener('change', function () {
        if (reduceMQ.matches) inner.style.transform = '';
      });
    }
  })();

  /* ================= HERO : réseau de nœuds =================
     RETIRÉ. Le canvas #node-net porte désormais la lune en croissant, rendue en
     WebGL par assets/scene.js. Un canvas ne peut pas avoir deux contextes : le
     second getContext() renvoie null, donc garder les deux aurait fait
     disparaître l'une des deux scènes selon l'ordre de chargement — sans erreur
     et sans que rien ne l'indique.
     ========================================================================= */


  /* ================= BLOCS DE CODE : copie ================= */
  (function copierCode() {
    var boutons = document.querySelectorAll('.copy');
    if (!boutons.length) return;
    boutons.forEach(function (b) {
      b.addEventListener('click', function () {
        var bloc = b.closest('.code');
        var code = bloc && bloc.querySelector('code');
        if (!code) return;
        var texte = code.textContent;
        // Un retour visible est indispensable : sans lui, l'utilisateur reclique
        // en croyant que rien ne s'est passé.
        function ok() {
          var avant = b.innerHTML;
          b.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" aria-hidden="true"><path d="M5 12l4 4L19 6" stroke-linecap="round" stroke-linejoin="round"/></svg>Copié';
          b.classList.add('done');
          setTimeout(function () { b.innerHTML = avant; b.classList.remove('done'); }, 1800);
        }
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(texte).then(ok, repli);
        } else {
          repli();
        }
        // Repli pour les contextes où l'API presse-papiers est refusée.
        function repli() {
          var ta = document.createElement('textarea');
          ta.value = texte;
          ta.setAttribute('readonly', '');
          ta.style.position = 'fixed'; ta.style.opacity = '0';
          document.body.appendChild(ta);
          ta.select();
          try { document.execCommand('copy'); ok(); } catch (e) {}
          document.body.removeChild(ta);
        }
      });
    });
  })();

})();
  });
