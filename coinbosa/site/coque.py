#!/usr/bin/env python3
"""
Propage la coque commune du site (en-tête, pied, empreintes de version) et
contrôle la cohérence des cinq pages.

POURQUOI CET OUTIL EXISTE
-------------------------
Le site est servi en HTML statique pur : une page = un fichier, aucune
dépendance, aucun processus, aucune étape de compilation. C'est cohérent avec la
contrainte « aucun conteneur » et c'est ce qui rend le déploiement trivial.

Le prix de ce choix est que l'en-tête et le pied sont recopiés dans cinq
fichiers. Ajouter une entrée au menu à la main, c'est cinq éditions — et la
cinquième sera oubliée un jour. Cet outil supprime ce risque.

CE QU'IL NE FAIT PAS
--------------------
Il ne génère PAS les pages. Le contenu propre à chaque page n'est jamais touché :
on peut éditer chaque fichier directement, à la main, sans jamais lancer ce
script. Il ne remplace que deux blocs délimités — <header>…</header> et
<footer>…</footer> — et rien d'autre. C'est délibéré : un générateur complet
écraserait silencieusement toute correction faite directement dans une page.

USAGE
-----
    python3 coque.py            propage la coque + recalcule les empreintes
    python3 coque.py --verifier contrôle seulement, n'écrit rien (code de
                                sortie non nul si un défaut est trouvé)
"""

import collections
import hashlib
import html
import json
import re
import subprocess
import sys
from pathlib import Path

RACINE = Path(__file__).resolve().parent
REFERENCE = "index.html"

# L'ordre est celui de la barre de navigation. Une page absente de cette table
# n'est pas traitée : c'est la liste qui fait foi.
PAGES = {
    "index.html": None,                     # l'accueil n'a pas d'entrée dans le menu
    "ecosysteme.html": "/ecosysteme.html",
    "chaine.html": "/chaine.html",
    "developpeurs.html": "/developpeurs.html",
    "a-propos.html": "/a-propos.html",
}

PARTAGE = {"assets/style.css": "/assets/style.css", "app.js": "/app.js",
           "assets/scene.js": "/assets/scene.js",
           # Le moteur de traduction. Sans empreinte, un visiteur deja venu
           # garderait l ancienne version et verrait un site a moitie traduit.
           "assets/i18n.js": "/assets/i18n.js"}


def lire(p):
    return (RACINE / p).read_text(encoding="utf-8")


def bloc(html, balise):
    """Extrait <balise …>…</balise> — le premier trouvé, avec ses délimiteurs."""
    d = html.find(f"<{balise}")
    if d < 0:
        return None, -1, -1
    f = html.find(f"</{balise}>", d)
    if f < 0:
        return None, -1, -1
    f += len(balise) + 3
    return html[d:f], d, f


def empreinte(p):
    """Empreinte courte du contenu d'une ressource partagée.

    Elle est ajoutée en paramètre d'URL (?v=…) pour que ces fichiers puissent
    être mis en cache un an SANS risque de servir du périmé : quand le contenu
    change, l'URL change, donc le navigateur redemande le fichier. Sans elle, le
    même réglage de cache serait un piège — un visiteur qui revient garderait
    l'ancien style pendant un an.
    """
    return hashlib.sha256((RACINE / p).read_bytes()).hexdigest()[:10]


# ── Propagation ───────────────────────────────────────────────────────────────

def propager(ecrire=True):
    ref = lire(REFERENCE)
    entete_ref, _, _ = bloc(ref, "header")
    pied_ref, _, _ = bloc(ref, "footer")
    if not entete_ref or not pied_ref:
        sys.exit(f"ARRÊT — {REFERENCE} n'a pas d'en-tête ou de pied identifiable.")

    # La page de référence est l'accueil : son en-tête ne porte aucun aria-current.
    # On repart donc d'un en-tête neutre et on marque l'entrée voulue page par page.
    neutre = re.sub(r'\s*aria-current="page"', "", entete_ref)

    modifiees = []
    for page, actif in PAGES.items():
        s = lire(page)
        entete = neutre
        if actif:
            # Marque les DEUX occurrences : barre principale et menu mobile.
            entete = neutre.replace(f'<a href="{actif}"', f'<a href="{actif}" aria-current="page"')
            if entete.count('aria-current="page"') != 2:
                sys.exit(f"ARRÊT — {page} : {entete.count('aria-current=')} marqueur(s) "
                         f"au lieu de 2 pour {actif}. La table PAGES ne correspond plus au menu.")

        avant = s
        _, d, f = bloc(s, "header")
        if d >= 0:
            s = s[:d] + entete + s[f:]
        _, d, f = bloc(s, "footer")
        if d >= 0:
            s = s[:d] + pied_ref + s[f:]

        if s != avant:
            modifiees.append(page)
            if ecrire:
                (RACINE / page).write_text(s, encoding="utf-8")

    return modifiees


def versionner(ecrire=True):
    """Recalcule les empreintes des ressources partagées dans les cinq pages."""
    emp = {chemin: empreinte(chemin) for chemin in PARTAGE}
    touchees = []
    for page in PAGES:
        s = lire(page)
        avant = s
        for chemin, url in PARTAGE.items():
            # Le motif accepte une version déjà présente : l'opération est
            # idempotente, relancer le script ne cumule pas les paramètres.
            s = re.sub(rf'((?:href|src)="{re.escape(url)})(\?v=[a-f0-9]+)?"',
                       rf'\g<1>?v={emp[chemin]}"', s)
        if s != avant:
            touchees.append(page)
            if ecrire:
                (RACINE / page).write_text(s, encoding="utf-8")

    if ecrire:
        try:
            rev = subprocess.run(["git", "rev-parse", "--short", "HEAD"], cwd=RACINE,
                                 capture_output=True, text=True, check=True).stdout.strip()
        except Exception:
            rev = "inconnue"
        (RACINE / "version.json").write_text(
            json.dumps({"site": "coinbosa.com", "revision": rev,
                        "style": emp["assets/style.css"], "script": emp["app.js"],
                        "pages": ["/" if p == "index.html" else "/" + p for p in PAGES]},
                       ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return emp, touchees


# ── Contrôles ─────────────────────────────────────────────────────────────────

def verifier():
    """Contrôles mécaniques. Chacun a déjà attrapé un vrai défaut sur ce site."""
    defauts = []
    src = {p: lire(p) for p in PAGES}

    # 1. Coque identique partout (aria-current mis à part, qui DOIT varier).
    ref_e = re.sub(r'\s*aria-current="page"', "", bloc(src[REFERENCE], "header")[0] or "")
    ref_p = bloc(src[REFERENCE], "footer")[0]
    for p in PAGES:
        e = re.sub(r'\s*aria-current="page"', "", bloc(src[p], "header")[0] or "")
        if e != ref_e:
            defauts.append(f"{p} : en-tête divergent de {REFERENCE}")
        if bloc(src[p], "footer")[0] != ref_p:
            defauts.append(f"{p} : pied divergent de {REFERENCE}")

    # 2. La page active est marquée une fois par menu, et sur la bonne entrée.
    for p, actif in PAGES.items():
        trouves = re.findall(r'<a href="([^"]+)"[^>]*aria-current="page"', src[p])
        if actif is None and trouves:
            defauts.append(f"{p} : l'accueil ne devrait porter aucun marqueur de page active")
        elif actif and (sorted(set(trouves)) != [actif] or len(trouves) != 2):
            defauts.append(f"{p} : marqueur de page active incorrect ({trouves})")

    # 3. Métadonnées propres à chaque page. Deux canonical identiques disent aux
    #    moteurs que deux pages n'en sont qu'une : l'une des deux disparaît.
    # <title> porte desormais un data-i18n : le motif doit tolerer des
    # attributs, sinon le controle conclut « title absent » sur une page qui
    # en a un — un faux defaut qui masque les vrais.
    for champ, motif in [("title", r"<title\b[^>]*>([^<]*)</title>"),
                         ("canonical", r'<link rel="canonical" href="([^"]*)"'),
                         ("og:url", r'<meta property="og:url" content="([^"]*)"'),
                         ("description", r'<meta name="description" content="([^"]*)"')]:
        vus = {}
        for p in PAGES:
            m = re.search(motif, src[p])
            if not m:
                defauts.append(f"{p} : {champ} absent")
                continue
            vus[p] = m.group(1)
        for val, n in collections.Counter(vus.values()).items():
            if n > 1:
                pages = [p for p, v in vus.items() if v == val]
                defauts.append(f"{champ} identique sur {', '.join(pages)}")

    # 4. La politique de sécurité interdit le script inline (script-src 'self').
    #    Un gestionnaire onclick= serait silencieusement mort en production.
    for p in PAGES:
        for m in re.findall(r"<script(?![^>]*\bsrc=)([^>]*)>", src[p]):
            if "application/ld+json" not in m:
                defauts.append(f"{p} : script inline — bloqué par la CSP")
        if re.search(r"\son(?:click|load|error|submit|change|input|focus|blur)=", src[p]):
            defauts.append(f"{p} : attribut d'événement inline — bloqué par la CSP")

    # 4 bis. Traduction : toute clé posée dans une page doit exister dans le
    #    dictionnaire source, et chaque langue doit couvrir les mêmes clés que le
    #    français. Sans ce contrôle, une clé oubliée passe inaperçue : la page
    #    s'affiche, simplement le fragment reste en français au milieu du reste.
    #    C'est le défaut le plus difficile à voir à l'œil et le plus facile à
    #    laisser filer.
    fr_json = RACINE / "assets" / "i18n-fr.json"
    if fr_json.exists():
        source = json.loads(fr_json.read_text(encoding="utf-8"))
        posees = set()
        for p in PAGES:
            posees |= set(re.findall(r'data-i18n(?:-attr-[a-z-]+)?="([^"]+)"', src[p]))
        manquantes = posees - set(source)
        if manquantes:
            defauts.append(f"clés posées dans les pages mais absentes du "
                           f"dictionnaire français : {sorted(manquantes)[:5]}")
        # Toute cle doit ENCODER SON PROPRE TEXTE. C'est l'invariant qui fait
        # tomber une traduction perimee : si le francais change, la cle change,
        # et l'ancienne traduction disparait au lieu de survivre en silence.
        # Les cles de metadonnees ne le respectaient pas — elles etaient nommees
        # d'apres la propriete HTML — et 25 traductions de l'ANCIEN site ont
        # ainsi ete servies apres une refonte complete, sur les titres de page
        # et les apercus de partage, c'est-a-dire le texte le plus expose.
        import unicodedata

        def _slug(t, n=42):
            t = re.sub(r"<[^>]+>", "", t)
            # Le desechappement DOIT etre fait, exactement comme dans
            # i18n-extraire.py : sans lui « Chaine &amp; BOSA » donne
            # « chaine-amp-bosa » d'un cote et « chaine-bosa » de l'autre, et le
            # controle accuse a tort des cles parfaitement valides.
            t = html.unescape(t)
            t = unicodedata.normalize("NFKD", t).encode("ascii", "ignore").decode()
            t = re.sub(r"[^a-zA-Z0-9]+", "-", t).strip("-").lower()
            return t[:n].rstrip("-") or "vide"

        sans_texte = []
        for cle, val in source.items():
            # Les cles app.* sont fabriquees par app.js A L'EXECUTION, a partir
            # du NOM du produit : elles ne peuvent pas encoder leur texte. Leur
            # fraicheur est garantie autrement, par le controle qui suit.
            if cle.startswith("app."):
                continue
            queue = cle.rsplit(".", 1)[-1]
            queue = re.sub(r"-\d+$", "", queue)          # suffixe d'unicite
            attendu = _slug(val, len(queue) if queue else 42)
            if queue and attendu and not queue.startswith(attendu[:12]):
                sans_texte.append(cle)
        if len(sans_texte) > 3:
            defauts.append(f"{len(sans_texte)} cle(s) n'encodent pas leur texte, "
                           f"ex. {sorted(sans_texte)[:3]} — une refonte du contenu "
                           f"y laisserait des traductions perimees")

        # Fraicheur des libelles generes : le dictionnaire doit dire EXACTEMENT
        # ce que app.js contient aujourd'hui. Sinon quelqu'un a modifie une
        # description de produit sans relancer l'extraction, et le site affiche
        # l'ancien texte traduit a cote du nouveau texte francais.
        appjs = RACINE / "app.js"
        if appjs.exists():
            js = appjs.read_text(encoding="utf-8")
            perimees = []
            for cle, val in source.items():
                if not cle.startswith("app.produit."):
                    continue
                if val not in js:
                    perimees.append(cle)
            if perimees:
                defauts.append(f"{len(perimees)} libelle(s) app.* ne correspondent plus a "
                               f"app.js, ex. {sorted(perimees)[:3]} — relancer "
                               f"i18n-extraire.py --ecrire")

        # DERIVE DU FRANCAIS SOUS UNE TRADUCTION DEJA FAITE.
        # La cle est tronquee a 42 caracteres : une reecriture qui ne touche que
        # la suite du texte laisse la cle inchangee, donc la traduction en place
        # — mais elle traduit desormais autre chose. C'est arrive : quatre cles
        # ont servi en cinq langues un discours que l'editeur avait fait retirer,
        # parce que seuls les caracteres au-dela du 42e avaient change. Aucun des
        # controles precedents ne le voyait : les cles etaient toutes presentes,
        # toutes traduites, aucune inconnue. Le site etait « complet » et faux.
        #
        # On enregistre donc l'empreinte du texte francais AU MOMENT ou les
        # traductions sont scellees (python3 coque.py --sceller-traductions,
        # a lancer une fois les traductions verifiees). Toute reecriture
        # ulterieure du francais casse l'empreinte et la publication est refusee
        # jusqu'a retraduction. Une cle absente du manifeste n'affirme rien.
        manifeste = RACINE / "assets" / "i18n-source.json"
        if manifeste.exists():
            scelle = json.loads(manifeste.read_text(encoding="utf-8"))
            derive = [c for c, v in source.items()
                      if c in scelle and _emp_txt(v) != scelle[c]]
            if derive:
                defauts.append(
                    f"{len(derive)} cle(s) dont le francais a ete reecrit depuis le "
                    f"scellement : leurs traductions disent encore l'ancien texte, "
                    f"ex. {sorted(derive)[:3]} — retraduire, puis "
                    f"python3 coque.py --sceller-traductions")
        else:
            defauts.append("assets/i18n-source.json absent : rien ne garantit que "
                           "les traductions correspondent au francais actuel — "
                           "lancer python3 coque.py --sceller-traductions")

        for f in sorted((RACINE / "assets").glob("i18n-*.js")):
            code = f.stem.split("-")[-1]
            txt = f.read_text(encoding="utf-8")
            cles = set(re.findall(r'^\s*"([^"]+)"\s*:', txt, re.M))
            if not cles:
                defauts.append(f"i18n-{code}.js : aucune clé lisible")
                continue
            absentes = set(source) - cles
            etrangeres = cles - set(source)
            if absentes:
                defauts.append(f"i18n-{code}.js : {len(absentes)} clé(s) non traduite(s), "
                               f"ex. {sorted(absentes)[:3]}")
            if etrangeres:
                defauts.append(f"i18n-{code}.js : {len(etrangeres)} clé(s) inconnue(s), "
                               f"ex. {sorted(etrangeres)[:3]}")

    # 5. Hiérarchie des titres : un saut de niveau fait annoncer par une synthèse
    #    vocale une sous-section qui n'existe pas.
    for p in PAGES:
        niv = [int(m) for m in re.findall(r"<h([1-6])\b", src[p])]
        if niv.count(1) != 1:
            defauts.append(f"{p} : {niv.count(1)} <h1> (il en faut exactement un)")
        for a, b in zip(niv, niv[1:]):
            if b > a + 1:
                defauts.append(f"{p} : saut de titre h{a} -> h{b}")
                break

    # 6. Balises équilibrées.
    for p in PAGES:
        for t in ("section", "div", "main", "ol", "ul", "table", "article",
                  "header", "footer", "nav", "p", "li", "span", "button", "a"):
            o = len(re.findall(rf"<{t}\b", src[p]))
            f = src[p].count(f"</{t}>")
            if o != f:
                defauts.append(f"{p} : <{t}> ouvert {o} fois, fermé {f} fois")

    # 6 bis. Aucun attribut repete dans une meme balise.
    # L'extracteur AJOUTAIT son marqueur data-i18n-attr-* sans retirer le
    # precedent : chaque relance en empilait un de plus. Les pages servies en
    # portaient jusqu'a 16 copies pour une seule valeur utile, soit 32 ko de
    # repetition sur cinq pages. Rien ne cassait a l'ecran — le navigateur
    # garde le premier et ignore le reste — donc rien ne le signalait, et la
    # page grossissait a chaque publication. Ce controle rend le defaut
    # bruyant plutot que silencieux.
    _bal = re.compile(r"<[a-zA-Z][^>]*>", re.S)
    _att = re.compile(r"\s([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=")
    for p in PAGES:
        for t in _bal.findall(src[p]):
            for nom, n in collections.Counter(_att.findall(t)).items():
                if n > 1:
                    defauts.append(f"{p} : attribut {nom} repete {n} fois dans "
                                   f"une meme balise ({t[:60]}...)")
                    break
            else:
                continue
            break

    # 7. Les empreintes affichées correspondent au contenu réel des ressources.
    for chemin, url in PARTAGE.items():
        attendue = empreinte(chemin)
        for p in PAGES:
            for trouvee in re.findall(rf'(?:href|src)="{re.escape(url)}\?v=([a-f0-9]+)"', src[p]):
                if trouvee != attendue:
                    defauts.append(f"{p} : {url} versionné {trouvee}, le fichier vaut {attendue}")

    return defauts


def _emp_txt(t):
    """Empreinte d'une valeur francaise, pour detecter sa reecriture."""
    return hashlib.sha256(t.strip().encode("utf-8")).hexdigest()[:16]


def sceller_traductions():
    """Fige le francais actuel comme etant celui que les traductions traduisent.

    A NE LANCER QU'APRES avoir verifie les traductions. Ce fichier est une
    affirmation : « ces traductions correspondent a ces textes francais ».
    Le sceller sur un francais non traduit rend la barriere muette.
    """
    src = json.loads((RACINE / "assets" / "i18n-fr.json").read_text(encoding="utf-8"))
    cible = RACINE / "assets" / "i18n-source.json"
    avant = json.loads(cible.read_text(encoding="utf-8")) if cible.exists() else {}
    apres = {c: _emp_txt(v) for c, v in src.items()}
    cible.write_text(json.dumps(apres, ensure_ascii=False, indent=1,
                                sort_keys=True) + "\n", encoding="utf-8")
    neuves = len(set(apres) - set(avant))
    bougees = sum(1 for c in apres if c in avant and apres[c] != avant[c])
    print(f"  traductions scellees : {len(apres)} cles "
          f"({neuves} nouvelles, {bougees} reecrites, "
          f"{len(set(avant) - set(apres))} disparues)")


def main():
    seulement_verifier = "--verifier" in sys.argv

    if "--sceller-traductions" in sys.argv:
        sceller_traductions()
        return 0

    if not seulement_verifier:
        modifiees = propager()
        emp, touchees = versionner()
        print(f"  coque propagée      : {', '.join(modifiees) if modifiees else 'déjà à jour'}")
        print(f"  empreintes          : style={emp['assets/style.css']} script={emp['app.js']}"
              f" ({', '.join(touchees) if touchees else 'déjà à jour'})")

    defauts = verifier()
    if defauts:
        print(f"\n  {len(defauts)} DÉFAUT(S) :")
        for d in defauts:
            print(f"    - {d}")
        sys.exit(1)
    print(f"\n  {len(PAGES)} pages contrôlées — aucun défaut.")


if __name__ == "__main__":
    main()
