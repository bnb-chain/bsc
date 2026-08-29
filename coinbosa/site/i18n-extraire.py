#!/usr/bin/env python3
"""
Instrumente les pages du site pour la traduction, et extrait le dictionnaire source.

POURQUOI CET OUTIL
------------------
Le site est en HTML statique : cinq fichiers, aucune compilation. Le rendre
multilingue à la main voudrait dire marquer plusieurs centaines de fragments un
par un, dans cinq fichiers — et en oublier. Cet outil marque, extrait, et
contrôle.

CE QU'IL FAIT
-------------
Il repère les fragments de texte destinés à l'écran, leur donne une clé stable,
pose un attribut data-i18n dessus, et écrit le dictionnaire français dans
assets/i18n-fr.json. Les autres langues se déduisent de ce fichier.

CE QU'IL NE FAIT PAS
--------------------
Il ne réécrit pas la structure des pages. Il ne touche ni au contenu de <script>,
<style>, <svg>, ni aux valeurs techniques (URL, nombres seuls, symboles). En mode
par défaut il n'écrit RIEN : il rend un rapport. Il faut --ecrire pour agir.

USAGE
-----
    python3 i18n-extraire.py            rapport seulement, n'écrit rien
    python3 i18n-extraire.py --ecrire   pose les attributs et écrit le dictionnaire
"""

import html as H
import json
import re
import sys
import unicodedata
from pathlib import Path

RACINE = Path(__file__).resolve().parent
PAGES = ["index.html", "ecosysteme.html", "chaine.html",
         "developpeurs.html", "a-propos.html"]

# Zones dont le contenu n'est jamais du texte d'écran.
ZONES_MORTES = re.compile(r"<(script|style|svg|noscript)\b.*?</\1>", re.S | re.I)

# Balises dont on traduit le contenu ENTIER, marquage interne compris. On prend
# l'élément comme unité : « Une chaîne pour <span>les relier</span> » se traduit
# d'un bloc, sinon la phrase se disloque et l'ordre des mots ne suit plus.
BALISES = ("h1", "h2", "h3", "h4", "p", "li", "td", "th", "figcaption",
           "summary", "button", "label", "blockquote", "dt", "dd",
           # <title> est le premier texte que voient un moteur de recherche et
           # l'aperçu d'un lien partagé. Le laisser en français rendait la page
           # française pour tout le monde, quelle que soit la langue affichée.
           "title")

# Attributs visibles ou lus par les lecteurs d'écran.
ATTRS = ("alt", "title", "aria-label", "placeholder")

# Marquage en ligne toléré à l'intérieur d'un fragment traduit.
EN_LIGNE = re.compile(r"</?(b|i|em|strong|span|br|small|code|sup|sub|a)\b[^>]*>", re.I)

# Un fragment sans lettre est technique : un nombre, un symbole, une flèche.
A_UNE_LETTRE = re.compile(r"[A-Za-zÀ-ÿ]{2,}")


def slug(texte, n=42):
    """Clé lisible et stable, dérivée du texte français."""
    t = re.sub(r"<[^>]+>", "", texte)
    t = H.unescape(t)
    t = unicodedata.normalize("NFKD", t).encode("ascii", "ignore").decode()
    t = re.sub(r"[^a-zA-Z0-9]+", "-", t).strip("-").lower()
    return t[:n].rstrip("-") or "vide"


def traduisible(frag):
    """Un fragment mérite-t-il une traduction ?"""
    nu = EN_LIGNE.sub("", frag).strip()
    nu = H.unescape(nu)
    if not nu:
        return False
    if not A_UNE_LETTRE.search(nu):          # nombres, symboles, unités
        return False
    if re.fullmatch(r"[\d\s.,%·×—–-]+", nu):
        return False
    if nu.startswith(("http://", "https://", "0x")):
        return False
    return True


def instrumenter(page):
    """Rend (html_modifie, {cle: texte_fr}, [avertissements])."""
    src = (RACINE / page).read_text(encoding="utf-8")
    dico, avert = {}, []
    prefixe = page.replace(".html", "").replace("index", "accueil")

    # On neutralise les zones mortes le temps de l'analyse, puis on les remet.
    coffre = []

    def ranger(m):
        coffre.append(m.group(0))
        return f"\x00{len(coffre)-1}\x00"

    travail = ZONES_MORTES.sub(ranger, src)

    vus = {}

    def cle_unique(base):
        if base not in vus:
            vus[base] = 0
            return base
        vus[base] += 1
        return f"{base}-{vus[base] + 1}"

    # --- 1. contenu des balises de texte ---------------------------------
    def sur_balise(m):
        ouvre, nom, inner, ferme = m.group(0), m.group(1), m.group(3), m.group(4)
        deja = re.search(r'data-i18n="([^"]+)"', m.group(2))
        if deja:
            # Déjà marqué — à la main, ou par un passage précédent. On ne touche
            # pas au balisage, mais on RELÈVE quand même le texte : sans cela une
            # clé posée à la main n'entrerait jamais dans le dictionnaire, et la
            # page resterait française dans toutes les langues sans que rien ne
            # le signale.
            dico[deja.group(1)] = inner.strip()
            return ouvre
        if "\x00" in inner:                   # contient une zone morte
            return ouvre
        if not traduisible(inner):
            return ouvre
        if re.search(r"<(?!/?(b|i|em|strong|span|br|small|code|sup|sub|a)\b)", inner):
            return ouvre                      # marquage non trivial : on laisse
        k = cle_unique(f"{prefixe}.{nom}.{slug(inner)}")
        dico[k] = inner.strip()
        return f"<{nom}{m.group(2)} data-i18n=\"{k}\">{inner}</{ferme}>"

    travail = re.sub(
        r"<(" + "|".join(BALISES) + r")\b([^>]*)>(.*?)</(" + "|".join(BALISES) + r")>",
        sur_balise, travail, flags=re.S | re.I)

    # --- 1 bis. relève des marques posées à la main ------------------------
    # Le passage precedent ne regarde que les balises de BALISES. Une cle posee a
    # la main sur un element hors liste — un <a> de navigation, par exemple —
    # n'entrait donc JAMAIS dans le dictionnaire : la page affichait la cle
    # correctement en francais et restait francaise dans toutes les autres
    # langues, sans que rien ne le signale. coque.py a attrape exactement ce cas.
    for m in re.finditer(r'<(\w+)([^>]*\bdata-i18n="([^"]+)"[^>]*)>(.*?)</\1>',
                         travail, re.S):
        cle, inner = m.group(3), m.group(4)
        if cle not in dico and "\x00" not in inner:
            dico[cle] = inner.strip()

    # --- 2. attributs visibles -------------------------------------------
    def sur_attr(m):
        avant, nom, val = m.group(1), m.group(2), m.group(3)
        if not traduisible(val):
            return m.group(0)
        k = cle_unique(f"{prefixe}.attr.{nom}.{slug(val)}")
        dico[k] = val
        return f'{avant}{nom}="{val}" data-i18n-attr-{nom}="{k}"'

    for a in ATTRS:
        travail = re.sub(r'(\s)(' + a + r')="([^"]{2,})"', sur_attr, travail)

    # --- 3. métadonnées de partage ---------------------------------------
    for prop in ('name="description"', 'property="og:description"',
                 'name="twitter:description"', 'property="og:title"',
                 'name="twitter:title"'):
        m = re.search(r'<meta\s+' + prop + r'\s+content="([^"]+)"', travail)
        if m and traduisible(m.group(1)):
            k = f"{prefixe}.meta.{slug(prop)}"
            dico[k] = m.group(1)

    # On remet les zones mortes.
    travail = re.sub(r"\x00(\d+)\x00", lambda m: coffre[int(m.group(1))], travail)

    # Contrôle : rien d'autre que nos ajouts ne doit avoir bougé.
    # On retire les marques des DEUX cotes : la page d'entree peut deja en
    # porter, posees a la main ou par un passage precedent. Ne les retirer que
    # du resultat faisait crier l'outil a chaque relance.
    def sans_marques(t):
        return re.sub(r'\s+data-i18n(-attr-[a-z-]+)?="[^"]*"', "", t)

    if sans_marques(travail) != sans_marques(src):
        avert.append(f"{page} : la page a changé au-delà des attributs ajoutés")

    return travail, dico, avert


def main():
    ecrire = "--ecrire" in sys.argv
    total, tous, avert = {}, 0, []
    print(f"  {'page':22} {'fragments':>10}  {'mots':>7}")
    for page in PAGES:
        html, dico, av = instrumenter(page)
        avert += av
        mots = sum(len(re.sub(r"<[^>]+>", "", v).split()) for v in dico.values())
        print(f"  {page:22} {len(dico):>10}  {mots:>7}")
        total.update({k: v for k, v in dico.items()})
        tous += len(dico)
        if ecrire:
            (RACINE / page).write_text(html, encoding="utf-8")

    print(f"  {'TOTAL':22} {tous:>10}  "
          f"{sum(len(re.sub(r'<[^>]+>', '', v).split()) for v in total.values()):>7}")

    doublons = tous - len(total)
    if doublons:
        print(f"  clés fusionnées entre pages : {doublons}")

    if avert:
        print("\n  AVERTISSEMENTS :")
        for a in avert:
            print(f"    - {a}")

    if ecrire:
        p = RACINE / "assets" / "i18n-fr.json"
        p.write_text(json.dumps(total, ensure_ascii=False, indent=1,
                                sort_keys=True) + "\n", encoding="utf-8")
        print(f"\n  écrit : {p.relative_to(RACINE)}  ({len(total)} clés)")
    else:
        print("\n  RAPPORT SEULEMENT — rien n'a été écrit. Relancer avec --ecrire.")
    return 1 if avert else 0


if __name__ == "__main__":
    sys.exit(main())
