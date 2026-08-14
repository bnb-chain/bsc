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

PARTAGE = {"assets/style.css": "/assets/style.css", "app.js": "/app.js"}


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
    for champ, motif in [("title", r"<title>([^<]*)</title>"),
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

    # 7. Les empreintes affichées correspondent au contenu réel des ressources.
    for chemin, url in PARTAGE.items():
        attendue = empreinte(chemin)
        for p in PAGES:
            for trouvee in re.findall(rf'(?:href|src)="{re.escape(url)}\?v=([a-f0-9]+)"', src[p]):
                if trouvee != attendue:
                    defauts.append(f"{p} : {url} versionné {trouvee}, le fichier vaut {attendue}")

    return defauts


def main():
    seulement_verifier = "--verifier" in sys.argv

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
