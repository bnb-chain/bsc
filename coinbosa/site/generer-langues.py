#!/usr/bin/env python3
# ---------------------------------------------------------------------------
# Coinbosa — rendre les cinq traductions VISIBLES pour un moteur de recherche.
#
#   python3 site/generer-langues.py              # génère site/_langues/<lg>/
#   python3 site/generer-langues.py --verifier   # échoue si la sortie n'est plus à jour
#
# POURQUOI CE SCRIPT EXISTE
# -------------------------
# Le site est traduit en six langues par `assets/i18n.js`, qui remplace le texte
# DANS LE NAVIGATEUR du visiteur. Le HTML envoyé par le serveur, lui, est en
# français — toujours, quelle que soit la langue.
#
# Google indexe ce qu'il reçoit. Il reçoit du français. Les versions anglaise,
# espagnole, portugaise, chinoise et arabe n'existent donc PAS pour un moteur de
# recherche : pas d'URL distincte, rien à indexer, rien à classer. Cinq des six
# publics du site ne peuvent pas le trouver dans leur langue.
#
# Ce script produit ce qui manque : une page réelle par langue, avec le texte
# déjà dans le HTML, à une URL propre — /en/, /es/, /pt/, /zh/, /ar/ — et les
# balises `hreflang` qui relient les six versions entre elles.
#
# CE QU'IL NE FAIT PAS, ET POURQUOI
# ---------------------------------
# Il ne traduit rien. Il applique les dictionnaires qui existent déjà dans
# `assets/i18n-<lg>.js`, exactement comme le ferait le navigateur — mêmes clés,
# mêmes valeurs. Une page générée dit donc mot pour mot ce que voit aujourd'hui
# un visiteur qui choisit cette langue. S'il y a une erreur de traduction, elle
# était déjà en ligne ; ce script la rend seulement indexable.
#
# Il n'écrit jamais dans `site/` : la sortie va dans `site/_langues/`, qui est
# dérivé et jamais versionné. Une page générée ne peut donc pas diverger de sa
# source — on la regénère, on ne la corrige pas.
#
# LES CHEMINS SONT DÉJÀ ABSOLUS, ET C'EST CE QUI REND L'OPÉRATION SÛRE
# --------------------------------------------------------------------
# Toutes les ressources sont référencées en `/assets/…`, `/app.js`, `/chaine.html`.
# Déplacer une page dans `/en/` ne casse donc aucun lien vers une feuille de style
# ou un script. Seuls les liens vers les AUTRES PAGES doivent être préfixés, pour
# qu'un visiteur anglophone qui clique reste en anglais.
# ---------------------------------------------------------------------------

import json
import re
import sys
from html.parser import HTMLParser
from pathlib import Path

RACINE = Path(__file__).resolve().parent
SORTIE = RACINE / "_langues"
DOMAINE = "https://coinbosa.com"

# Les cinq pages du site. La clé est le fichier, la valeur son chemin public.
PAGES = {
    "index.html": "/",
    "ecosysteme.html": "/ecosysteme.html",
    "chaine.html": "/chaine.html",
    "developpeurs.html": "/developpeurs.html",
    "a-propos.html": "/a-propos.html",
}

# locale : ce qu'on met dans <html lang>. rtl : sens d'écriture.
LANGUES = {
    "fr": {"locale": "fr", "rtl": False, "nom": "Français"},
    "en": {"locale": "en", "rtl": False, "nom": "English"},
    "es": {"locale": "es", "rtl": False, "nom": "Español"},
    "pt": {"locale": "pt", "rtl": False, "nom": "Português"},
    "zh": {"locale": "zh-Hans", "rtl": False, "nom": "中文"},
    "ar": {"locale": "ar", "rtl": True, "nom": "العربية"},
}

# Balises sans fermeture : elles ne portent jamais de contenu à traduire, mais
# elles portent des attributs (meta, link).
ORPHELINES = {"area", "base", "br", "col", "embed", "hr", "img", "input",
              "link", "meta", "param", "source", "track", "wbr"}


def lire(p: Path) -> str:
    return p.read_text(encoding="utf-8")


def dictionnaire(lg: str) -> dict:
    """Extrait le dictionnaire du fichier .js, sans exécuter de JavaScript."""
    src = lire(RACINE / "assets" / f"i18n-{lg}.js")
    d = src.index("{", src.index(f"window.__I18N.{lg}"))
    # On repart de l'accolade ouvrante et on lit un objet JSON complet.
    dec = json.JSONDecoder()
    obj, _ = dec.raw_decode(src[d:])
    return obj


class Reperage(HTMLParser):
    """Relève les positions exactes des éléments à traduire.

    On ne reconstruit PAS le HTML : on note des décalages dans le texte
    d'origine, puis on fait la chirurgie dessus. Tout ce qu'on ne touche pas
    reste identique à l'octet près — commentaires, indentation, casse des
    attributs. Un générateur qui réécrit tout finit toujours par changer
    quelque chose que personne n'a demandé.
    """

    def __init__(self, texte: str):
        super().__init__(convert_charrefs=False)
        self.texte = texte
        # décalage du début de chaque ligne, pour convertir (ligne, colonne)
        self.lignes = [0]
        for m in re.finditer("\n", texte):
            self.lignes.append(m.end())
        self.contenus = []   # {cle, inner_debut, inner_fin}
        self.attributs = []  # {tag_debut, tag_fin, paires:[(attr, cle)]}
        self.pile = []
        self.html_tag = None

    def _pos(self) -> int:
        l, c = self.getpos()
        return self.lignes[l - 1] + c

    def handle_starttag(self, tag, attrs):
        debut = self._pos()
        brut = self.get_starttag_text() or ""
        fin = debut + len(brut)
        a = dict(attrs)

        if tag == "html" and self.html_tag is None:
            self.html_tag = (debut, fin, a)

        paires = [(n[len("data-i18n-attr-"):], v)
                  for n, v in attrs if n.startswith("data-i18n-attr-") and v]
        if paires:
            self.attributs.append({"tag_debut": debut, "tag_fin": fin, "paires": paires})

        if tag in ORPHELINES or brut.endswith("/>"):
            return

        cle = a.get("data-i18n")
        self.pile.append({"tag": tag, "cle": cle, "inner_debut": fin})

    def handle_endtag(self, tag):
        # On dépile jusqu'à la balise correspondante : le HTML du dépôt est
        # bien formé, mais une balise implicitement fermée ne doit pas décaler
        # tout le reste.
        for i in range(len(self.pile) - 1, -1, -1):
            if self.pile[i]["tag"] == tag:
                for e in self.pile[i:]:
                    if e["cle"]:
                        self.contenus.append({"cle": e["cle"],
                                              "inner_debut": e["inner_debut"],
                                              "inner_fin": self._pos()})
                del self.pile[i:]
                return


def alternates(chemin: str, langue_courante: str) -> str:
    """Les balises qui relient les six versions — sans elles, Google voit six
    pages concurrentes au lieu de six traductions d'une même page."""
    out = []
    for lg, cfg in LANGUES.items():
        url = DOMAINE + ("" if lg == "fr" else "/" + lg) + chemin
        out.append(f'<link rel="alternate" hreflang="{cfg["locale"]}" href="{url}">')
    out.append(f'<link rel="alternate" hreflang="x-default" href="{DOMAINE}{chemin}">')
    return "\n".join(out)


def jsonld() -> str:
    """Ce que Google a besoin de savoir et qu'aucune balise ne lui dit."""
    cfg = json.loads(lire(RACINE.parent / "coinbosa.config.json"))
    liens = cfg.get("links", {})
    # `sameAs` ne sert qu'aux comptes qui REPRÉSENTENT l'organisation. Le site
    # lui-même et l'explorateur n'y ont pas leur place : ce sont ses pages, pas
    # d'autres profils d'elle.
    reseaux = [v for k, v in liens.items()
               if isinstance(v, str) and v.startswith("http")
               and k in ("github", "facebook", "twitter", "telegram", "linkedin", "youtube")]
    d = {
        "@context": "https://schema.org",
        "@graph": [
            {"@type": "Organization", "@id": f"{DOMAINE}/#organisation",
             "name": "Coinbosa", "url": DOMAINE,
             "logo": f"{DOMAINE}/assets/logo.jpg",
             **({"sameAs": sorted(set(reseaux))} if reseaux else {})},
            {"@type": "WebSite", "@id": f"{DOMAINE}/#site",
             "url": DOMAINE, "name": "Coinbosa",
             "publisher": {"@id": f"{DOMAINE}/#organisation"}},
        ],
    }
    return ('<script type="application/ld+json">'
            + json.dumps(d, ensure_ascii=False, separators=(",", ":"))
            + "</script>")


def traduire(texte: str, lg: str, dico: dict, chemin: str) -> tuple[str, int, int]:
    """Rend la page traduite, plus le nombre de contenus et d'attributs posés."""
    r = Reperage(texte)
    r.feed(texte)
    r.close()

    edits = []   # (debut, fin, remplacement)
    poses_c = poses_a = 0

    for c in r.contenus:
        v = dico.get(c["cle"])
        if v is None:
            continue                      # clé absente : on laisse le français
        edits.append((c["inner_debut"], c["inner_fin"], v))
        poses_c += 1

    for a in r.attributs:
        brut = texte[a["tag_debut"]:a["tag_fin"]]
        neuf = brut
        for attr, cle in a["paires"]:
            v = dico.get(cle)
            if v is None:
                continue
            # On remplace la valeur de CET attribut, pas une autre qui lui
            # ressemblerait : l'ancre porte le nom d'attribut suivi de "=".
            m = re.search(r'(\s' + re.escape(attr) + r'=")((?:[^"])*)(")', neuf)
            if not m:
                continue
            neuf = neuf[:m.start(2)] + v.replace('"', "&quot;") + neuf[m.end(2):]
            poses_a += 1
        if neuf != brut:
            edits.append((a["tag_debut"], a["tag_fin"], neuf))

    # <html lang> et sens d'écriture
    if r.html_tag:
        debut, fin, _ = r.html_tag
        brut = texte[debut:fin]
        cfg = LANGUES[lg]
        neuf = re.sub(r'\slang="[^"]*"', f' lang="{cfg["locale"]}"', brut)
        neuf = re.sub(r'\sdir="[^"]*"', "", neuf)
        neuf = neuf[:-1].rstrip() + f' dir="{"rtl" if cfg["rtl"] else "ltr"}"' \
                                    f' data-i18n-source="{lg}">'
        edits.append((debut, fin, neuf))

    edits.sort(key=lambda e: e[0], reverse=True)
    for d, f, v in edits:
        texte = texte[:d] + v + texte[f:]

    # --- liens internes : rester dans la langue ------------------------------
    # Les ressources (/assets, /app.js) ne bougent pas ; seules les pages du
    # site sont préfixées, sinon un clic renverrait le visiteur en français.
    for fichier, public in PAGES.items():
        cible = f"/{lg}/" if public == "/" else f"/{lg}{public}"
        texte = texte.replace(f'href="{public}"', f'href="{cible}"')
    texte = texte.replace('href="/whitepaper/"',
                          f'href="/whitepaper/{"en/" if lg == "en" else ""}"')

    # --- canonique, og:url, hreflang, données structurées --------------------
    url = DOMAINE + (f"/{lg}/" if chemin == "/" else f"/{lg}{chemin}")
    texte = re.sub(r'<link rel="canonical" href="[^"]*">',
                   f'<link rel="canonical" href="{url}">', texte, count=1)
    texte = re.sub(r'(<meta property="og:url" content=")[^"]*(")',
                   lambda m: m.group(1) + url + m.group(2), texte, count=1)

    # On RETIRE d'abord ce qui existe, puis on pose. La source française porte
    # les mêmes balises (elle doit déclarer ses alternatives, sinon la relation
    # n'est pas réciproque et Google l'ignore) ; sans ce nettoyage, la page
    # générée en aurait deux jeux.
    texte = re.sub(r'\n?<link rel="alternate" hreflang="[^"]*" href="[^"]*">', "", texte)
    texte = re.sub(r'\n?<script type="application/ld\+json">.*?</script>', "", texte, flags=re.S)

    # PAS de saut de ligne final : le retrait ci-dessus consomme celui qui
    # PRECEDE chaque balise, jamais celui qui suit la derniere. En poser un
    # rendait la fonction non idempotente — une ligne vide de plus a chaque
    # execution, donc une page qui change sans raison, donc un sitemap qui
    # bouge, donc un commit. Verifie le 2026-09-10 sur une double execution.
    injection = "\n" + alternates(chemin, lg) + "\n" + jsonld()
    m = re.search(r'<link rel="canonical"[^>]*>', texte)
    if m:
        texte = texte[:m.end()] + injection + texte[m.end():]

    return texte, poses_c, poses_a


def sitemap() -> int:
    """Le plan du site, produit ici et nulle part ailleurs.

    Il listait sept URL — les cinq pages françaises et deux livres blancs —
    alors que le site en sert désormais trente. Un plan écrit à la main finit
    toujours par oublier une page ; celui-ci est dérivé de la même table que la
    génération, donc il ne peut pas s'en écarter.

    Chaque URL déclare ses cinq sœurs par `xhtml:link` : c'est la forme que
    Google recommande pour un site multilingue, et elle vaut relation
    réciproque même si une page est atteinte sans être explorée.
    """
    import datetime, subprocess
    def maj(fichier: str) -> str:
        # La date du dernier commit qui a touché la page, pas celle du disque :
        # une copie de dépôt fraîche donnerait sinon « tout a changé aujourd'hui ».
        try:
            d = subprocess.run(["git", "log", "-1", "--format=%cs", "--", f"site/{fichier}"],
                               cwd=RACINE.parent, capture_output=True, text=True, timeout=10)
            if d.returncode == 0 and d.stdout.strip():
                return d.stdout.strip()
        except Exception:
            pass
        return datetime.date.today().isoformat()

    lignes = ['<?xml version="1.0" encoding="UTF-8"?>',
              '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"',
              '        xmlns:xhtml="http://www.w3.org/1999/xhtml">']
    n = 0
    for fichier, chemin in PAGES.items():
        d = maj(fichier)
        for lg, cfg in LANGUES.items():
            url = DOMAINE + ("" if lg == "fr" else "/" + lg) + chemin
            lignes.append(f"  <url><loc>{url}</loc><lastmod>{d}</lastmod>"
                          f"<changefreq>weekly</changefreq>")
            for lg2, cfg2 in LANGUES.items():
                u2 = DOMAINE + ("" if lg2 == "fr" else "/" + lg2) + chemin
                lignes.append(f'    <xhtml:link rel="alternate" hreflang="{cfg2["locale"]}" href="{u2}"/>')
            lignes.append(f'    <xhtml:link rel="alternate" hreflang="x-default" href="{DOMAINE}{chemin}"/>')
            lignes.append("  </url>")
            n += 1
    for u in (f"{DOMAINE}/whitepaper/", f"{DOMAINE}/whitepaper/en/"):
        lignes.append(f"  <url><loc>{u}</loc><changefreq>monthly</changefreq></url>")
        n += 1
    lignes.append("</urlset>")
    (RACINE / "sitemap.xml").write_text("\n".join(lignes) + "\n", encoding="utf-8")
    return n


def poser_sur_le_francais() -> list:
    """Le français doit déclarer ses alternatives comme les autres.

    Une relation `hreflang` n'est prise en compte que si elle est RÉCIPROQUE :
    si /en/ pointe vers / mais que / ne pointe pas vers /en/, Google écarte les
    deux. La page source porte donc les mêmes balises que ses traductions.
    """
    touchees = []
    for fichier, chemin in PAGES.items():
        f = RACINE / fichier
        s = lire(f)
        avant = s
        s = re.sub(r'\n?<link rel="alternate" hreflang="[^"]*" href="[^"]*">', "", s)
        s = re.sub(r'\n?<script type="application/ld\+json">.*?</script>', "", s, flags=re.S)
        m = re.search(r'<link rel="canonical"[^>]*>', s)
        if not m:
            continue
        s = s[:m.end()] + "\n" + alternates(chemin, "fr") + "\n" + jsonld() + s[m.end():]
        if s != avant:
            f.write_text(s, encoding="utf-8")
            touchees.append(fichier)
    return touchees


def main() -> int:
    verifier = "--verifier" in sys.argv
    manquants, total_c, total_a = [], 0, 0
    lignes = []

    if not verifier:
        fr = poser_sur_le_francais()
        if fr:
            print(f"\n    français : hreflang + données structurées posés sur "
                  f"{len(fr)} page(s)")
        n_sitemap = sitemap()
        print(f"    sitemap : {n_sitemap} URL")

    for lg in LANGUES:
        if lg == "fr":
            continue
        dico = dictionnaire(lg)
        dossier = SORTIE / lg
        if not verifier:
            dossier.mkdir(parents=True, exist_ok=True)
        pc = pa = 0
        for fichier, chemin in PAGES.items():
            src = lire(RACINE / fichier)
            out, c, a = traduire(src, lg, dico, chemin)
            pc += c
            pa += a
            cible = dossier / fichier
            if verifier:
                if not cible.exists() or lire(cible) != out:
                    manquants.append(str(cible.relative_to(RACINE.parent)))
            else:
                cible.write_text(out, encoding="utf-8")
        total_c += pc
        total_a += pa
        lignes.append(f"    {lg}  {len(PAGES)} pages   {pc:4} contenus   {pa:3} attributs"
                      f"   {len(dico)} clés au dictionnaire")

    if verifier:
        if manquants:
            print("  ÉCHEC — sortie absente ou périmée :")
            for m in manquants[:10]:
                print(f"    {m}")
            print("  Relancer : python3 site/generer-langues.py")
            return 1
        print(f"  à jour — {len(LANGUES) - 1} langues × {len(PAGES)} pages")
        return 0

    print("\n  GÉNÉRATION DES VERSIONS DE LANGUE")
    print("  " + "=" * 66)
    for l in lignes:
        print(l)
    print(f"\n    total : {(len(LANGUES) - 1) * len(PAGES)} pages écrites dans "
          f"{SORTIE.relative_to(RACINE.parent)}/")
    print(f"    {total_c} contenus et {total_a} attributs traduits")
    print("\n    Ces pages sont DÉRIVÉES : ne jamais les corriger à la main.")
    print("    Corriger la source française ou le dictionnaire, puis regénérer.\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
