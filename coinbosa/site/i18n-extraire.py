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
from html.parser import HTMLParser
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


# Le nom de la marque, ses declinaisons, et les valeurs qui n'ont pas de
# traduction : les traduire serait une faute, pas un oubli.
JAMAIS = {"coinbosa", "coinbosa, inc.", "bosa", "coin", "brc20", "parlia",
          "evm", "github", "metamask", "trust wallet", "nextfuture", "neobanq"}


IDENTIFIANT = re.compile(
    r"^(?:[\w./-]+\.(?:js|json|toml|ya?ml|py|sh|sol|html|css|md|txt)"
    r"|[a-z]+_[A-Za-z]\w*|[a-z]+[A-Z]\w*\(\)?)$")


def traduisible(frag):
    """Un fragment mérite-t-il une traduction ?"""
    nu = EN_LIGNE.sub("", frag).strip()
    if "\x00" in nu:
        return False
    nu_texte = H.unescape(re.sub(r"<[^>]+>", "", nu)).strip()
    if nu_texte.lower() in JAMAIS:
        return False
    # foundry.toml, hardhat.config.js, wallet_addEthereumChain : des noms de
    # fichier et d'API. Les traduire rendrait l'instruction inexecutable.
    if IDENTIFIANT.match(nu_texte):
        return False
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


class _Orphelins(HTMLParser):
    """Repère les textes qu'aucun ancêtre marqué ne couvre.

    On garde une vraie pile d'éléments ouverts : c'est la seule façon de savoir
    avec certitude sous quoi vit un fragment. Une fenêtre glissante sur le texte
    brut se trompe, et se trompe en silence.
    """

    VIDES = {"area", "base", "br", "col", "embed", "hr", "img", "input",
             "link", "meta", "source", "track", "wbr"}
    # Conteneurs de mise en page : ils n'ont jamais vocation a porter une
    # traduction, ils englobent d'autres blocs.
    STRUCTURE = {"html", "body", "main", "section", "article", "header",
                 "footer", "nav", "div", "ul", "ol", "table", "tbody", "thead",
                 "tr", "form", "figure", "picture"}
    MUETTES = {"script", "style", "svg", "noscript", "code", "pre", "title"}

    def __init__(self, texte):
        super().__init__(convert_charrefs=False)
        self.texte = texte
        self.lignes = [0]
        for l in texte.split("\n")[:-1]:
            self.lignes.append(self.lignes[-1] + len(l) + 1)
        self.pile = []          # [(nom, marque, debut_balise, fin_balise)]
        self.muet = 0
        self.cibles = []        # [(debut_balise, fin_balise, nom)]

    def _abs(self):
        l, c = self.getpos()
        return self.lignes[l - 1] + c

    def handle_starttag(self, tag, attrs):
        if tag in self.VIDES:
            return
        debut = self._abs()
        fin = self.texte.find(">", debut)
        marque = any(a[0] == "data-i18n" for a in attrs)   # PAS startswith : un
            # data-i18n-attr-aria-label ne traduit que cet attribut, pas le
            # contenu. Confondre les deux faisait passer toute une section
            # pour traduite parce que son aria-label l etait — les deux
            # boutons du heros sont restes francais en arabe a cause de ca.
        self.pile.append((tag, marque, debut, fin))
        if tag in self.MUETTES:
            self.muet += 1

    def handle_endtag(self, tag):
        if tag in self.VIDES:
            return
        for i in range(len(self.pile) - 1, -1, -1):
            if self.pile[i][0] == tag:
                if tag in self.MUETTES:
                    self.muet = max(0, self.muet - 1)
                del self.pile[i:]
                return

    def handle_data(self, data):
        if self.muet or not self.pile:
            return
        if not data.strip() or not A_UNE_LETTRE.search(data):
            return
        if any(m for _, m, _, _ in self.pile):
            return
        # On ne vise pas l'element le plus interne : dans
        # « <span><b>Aucun membre…</b> Une telle demande…</span> », marquer le
        # <b> d'abord interdit ensuite de marquer le <span>, et la seconde
        # moitie de la phrase reste francaise. On remonte donc jusqu'au premier
        # ancetre NON structurel, et on garde toute la chaine en dessous : le
        # poseur essaiera du plus englobant au plus interne, et s'arretera au
        # premier qui ne contient que du texte et du marquage en ligne.
        chaine = []
        for nom, _, debut, fin in self.pile:
            if nom in self.STRUCTURE and not chaine:
                continue
            chaine.append((debut, fin, nom))
        if not chaine:
            # Toute la chaine est structurelle : le texte vit directement dans un
            # <div>, comme les etiquettes « Jalon 6 · en cours ». Sans ce repli
            # elles n'etaient jamais marquees et restaient francaises partout.
            nom, _, debut, fin = self.pile[-1]
            chaine = [(debut, fin, nom)]
        self.cibles.append(chaine)


def rattraper_orphelins(html, dico, cle_unique, prefixe):
    """Pose data-i18n sur le parent immédiat de chaque texte non couvert."""
    s = _Orphelins(html)
    try:
        s.feed(html)
    except Exception:
        return html                      # analyseur en échec : on ne touche à rien

    # Une cible par chaine : on retient le candidat le plus englobant qui ne
    # contient que du texte et du marquage en ligne.
    retenues = []
    for chaine in s.cibles:
        for debut, fin, nom in chaine:
            if fin <= debut:
                continue
            ferme = html.find(f"</{nom}>", fin)
            if ferme < 0:
                continue
            inner = html[fin + 1:ferme]
            # Les zones mortes (svg, script, style) sont rangees le temps de
            # l'analyse et remplacees par un marqueur \x00N\x00. Sans ce
            # controle, le marqueur entrait dans le dictionnaire et laissait un
            # chiffre parasite en tete de la traduction — « 5 Copier ».
            if "\x00" in inner:
                continue
            if re.search(r"<(?!/?(b|i|em|strong|span|br|small|sup|sub|code|a)\b)", inner):
                continue
            if not traduisible(inner):
                continue
            retenues.append((debut, fin, nom))
            break

    # De la fin vers le début : insérer décale tout ce qui suit.
    vues = set()
    for debut, fin, nom in sorted(set(retenues), reverse=True):
        if (debut, fin) in vues:
            continue
        vues.add((debut, fin))
        if fin <= debut:
            continue
        ferme = html.find(f"</{nom}>", fin)
        if ferme < 0:
            continue
        inner = html[fin + 1:ferme]
        # Un parent qui contient un autre bloc n'est pas une feuille : le
        # traduire d'un morceau écraserait la structure interne.
        # code et a sont admis comme en ligne, comme dans la passe principale :
        # une phrase du type « appelle <code>x</code> puis <code>y</code> » se
        # traduit d un bloc. Les traducteurs ont pour consigne de ne pas toucher
        # au contenu de <code>, et le controle de balisage le verifie.
        if re.search(r"<(?!/?(b|i|em|strong|span|br|small|sup|sub|code|a)\b)", inner):
            continue
        if "data-i18n" in inner:
            # Un parent et son enfant ne peuvent pas porter chacun une cle :
            # appliquer la traduction du parent remplace l enfant, dont la cle
            # devient alors sans objet. Le resultat depend de l ordre de
            # parcours du DOM — donc il est fragile et invisible a la relecture.
            continue
        if not traduisible(inner):
            continue
        k = cle_unique(f"{prefixe}.{nom}.{slug(inner)}")
        dico[k] = inner.strip()
        html = html[:fin] + f' data-i18n="{k}"' + html[fin:]
    return html


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

    # --- 1 ter. rattrapage des orphelins ----------------------------------
    # Les passes precedentes travaillent sur une liste de balises. Tout texte
    # qui vit ailleurs — un <a class="btn"> du heros, une etiquette dans un
    # <div> — leur echappe et reste francais dans toutes les langues.
    # La capture de la page arabe l'a montre : les deux boutons du heros
    # s'affichaient en francais au milieu d'une page arabe.
    #
    # On ne devine pas : on demande a un vrai analyseur quels textes ne sont
    # couverts par AUCUN ancetre marque, et on marque leur parent immediat.
    travail = rattraper_orphelins(travail, dico, cle_unique, prefixe)

    # --- 2. attributs visibles -------------------------------------------
    def sur_attr(m):
        avant, nom, val = m.group(1), m.group(2), m.group(3)
        if not traduisible(val):
            return m.group(0)
        k = cle_unique(f"{prefixe}.attr.{nom}.{slug(val)}")
        dico[k] = val
        return f'{avant}{nom}="{val}" data-i18n-attr-{nom}="{k}"'

    # sur_attr AJOUTE le marqueur. Relancer l'extraction sur une page deja
    # marquee en empilait donc un de plus a chaque passage : les pages servies
    # portaient jusqu'a 16 copies de data-i18n-attr-aria-label sur un meme
    # element, pour une seule valeur utile. Le navigateur garde la premiere et
    # ignore le reste — rien ne cassait a l'ecran, et la page grossissait a
    # chaque publication. On efface donc les marqueurs avant de les reposer :
    # l'extraction devient idempotente, relancable sans degrader la page.
    travail = re.sub(r'\s+data-i18n-attr-[a-z-]+="[^"]*"', "", travail)
    for a in ATTRS:
        travail = re.sub(r'(\s)(' + a + r')="([^"]{2,})"', sur_attr, travail)

    # --- 3. métadonnées de partage ---------------------------------------
    for prop in ('name="description"', 'property="og:description"',
                 'name="twitter:description"', 'property="og:title"',
                 'name="twitter:title"'):
        m = re.search(r'<meta\s+' + prop + r'\s+content="([^"]+)"', travail)
        if m and traduisible(m.group(1)):
            # La cle encode le TEXTE, pas seulement la propriete. Sinon une
            # description reecrite garde la meme cle : l'outil de reconciliation
            # la croit inchangee et conserve la traduction de l'ANCIEN texte.
            # C'est arrive a la refonte — les titres et descriptions de partage,
            # donc le premier texte que lit un moteur de recherche, sont restes
            # en anglais de la version precedente sans que rien ne le signale.
            k = f"{prefixe}.meta.{slug(prop)}.{slug(m.group(1), 28)}"
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


def depuis_app_js():
    """Releve les libelles que app.js fabrique a l'execution.

    app.js construit la grille des produits, les pastilles d'etat et l'entree
    « Explorateur » du menu APRES le chargement. Ces textes ne sont dans aucune
    page : sans cette lecture, ils disparaissent du dictionnaire a chaque
    reconstruction, et le site affiche « Explorateur » et « Education » en
    francais au milieu d'une page anglaise. C'est arrive apres la refonte.
    """
    src = (RACINE / "app.js")
    if not src.exists():
        return {}
    s = src.read_text(encoding="utf-8")
    d = {}

    for m in re.finditer(r'cle:\s*"(app\.statut\.\w+)"', s):
        cle = m.group(1)
        lab = re.search(r'label:\s*"([^"]+)"[^}]*' + re.escape(cle), s)
        if not lab:
            # label precede cle dans l'objet : on relit la ligne entiere
            ligne = next((l for l in s.split("\n") if cle in l), "")
            lab = re.search(r'label:\s*"([^"]+)"', ligne)
        if lab:
            d[cle] = lab.group(1)

    for m in re.finditer(r'data-i18n="(app\.[\w.-]+)">([^<\']+)', s):
        d[m.group(1)] = m.group(2).strip()

    # La fiche du reseau : etiquettes du tableau stats, puis les valeurs
    # correspondantes dans CONTENT.network.
    for m in re.finditer(r'c:\s*"(\w+)",\s*k:\s*"([^"]+)"', s):
        d[f"app.stat.{m.group(1)}"] = m.group(2)

    reseau = {
        "app.reseau.evm": "evm",
        "app.reseau.consensus": "consensus",
        "app.reseau.standard": "tokenStandard",
        "app.reseau.note-offre": "supplyNote",
    }
    for cle, champ in reseau.items():
        m = re.search(champ + r':\s*"([^"]+)"', s)
        if m:
            d[cle] = m.group(1)
    m = re.search(r'decimals:\s*"([^"]+)"', s)
    if m:
        # app.js assemble « 18 » et « décimales » : la valeur affichee est la somme.
        d["app.reseau.decimales"] = m.group(1) + " décimales"

    try:
        i = s.index("products: [")
        bloc = s[i:s.index("// Le socle", i)]
        for obj in re.findall(r"\{([^{}]*)\}", bloc):
            nom = re.search(r'name:\s*"([^"]+)"', obj)
            cat = re.search(r'category:\s*"([^"]+)"', obj)
            des = re.search(r'desc:\s*"((?:[^"\\]|\\.)*)"', obj)
            if nom and cat and des:
                k = re.sub(r"[^a-z0-9]+", "-", nom.group(1).lower())
                d[f"app.produit.{k}.cat"] = cat.group(1)
                d[f"app.produit.{k}.desc"] = des.group(1)
    except ValueError:
        pass
    return d


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

    depuis_js = depuis_app_js()
    nouvelles = {k: v for k, v in depuis_js.items() if k not in total}
    total.update(nouvelles)
    print(f"  {'app.js (genere)':22} {len(depuis_js):>10}")

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
