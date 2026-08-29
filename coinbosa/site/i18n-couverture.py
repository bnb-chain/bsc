#!/usr/bin/env python3
"""
Mesure ce qui ÉCHAPPE à la traduction, page par page.

Un instrumenteur qui marque 90 % du texte laisse un site à moitié français. Cet
outil regarde le résultat avec l'œil du visiteur : tout texte visible qui n'est
couvert par aucun data-i18n, sur lui-même ou sur un de ses ancêtres, est listé
nommément.

La première version remontait les ancêtres à la fenêtre glissante et déclarait
non couverts des fragments qui l'étaient — « Nom du réseau » notamment. On tient
donc ici une vraie pile d'éléments ouverts : c'est la seule façon de savoir avec
certitude sous quoi vit un fragment.

USAGE
    python3 i18n-couverture.py            liste les fragments non couverts
    python3 i18n-couverture.py --tout     affiche aussi le détail complet
"""

import sys
from html.parser import HTMLParser
from pathlib import Path
import re

RACINE = Path(__file__).resolve().parent
PAGES = ["index.html", "ecosysteme.html", "chaine.html",
         "developpeurs.html", "a-propos.html"]

VIDES = {"area", "base", "br", "col", "embed", "hr", "img", "input",
         "link", "meta", "source", "track", "wbr"}

# Contenu jamais destiné à la lecture, ou volontairement non traduit.
MUETTES = {"script", "style", "svg", "noscript", "code", "pre"}

A_UNE_LETTRE = re.compile(r"[A-Za-zÀ-ÿ]{2,}")


class Sonde(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.pile = []          # [(nom, porte_i18n)]
        self.nus = []
        self.couverts = 0
        self.muet = 0

    def handle_starttag(self, tag, attrs):
        if tag in VIDES:
            return
        marque = any(a[0].startswith("data-i18n") for a in attrs)
        self.pile.append((tag, marque))
        if tag in MUETTES:
            self.muet += 1

    def handle_endtag(self, tag):
        if tag in VIDES:
            return
        for i in range(len(self.pile) - 1, -1, -1):
            if self.pile[i][0] == tag:
                if tag in MUETTES:
                    self.muet = max(0, self.muet - 1)
                del self.pile[i:]
                return

    def handle_data(self, data):
        t = data.strip()
        if not t or not A_UNE_LETTRE.search(t):
            return
        if self.muet:
            return
        if any(m for _, m in self.pile):
            self.couverts += 1
        else:
            self.nus.append((self.getpos()[0], t[:80]))


def main():
    tout = "--tout" in sys.argv
    total_nu = total_ok = 0
    for page in PAGES:
        s = Sonde()
        s.feed((RACINE / page).read_text(encoding="utf-8"))
        total_nu += len(s.nus)
        total_ok += s.couverts
        pc = 100 * s.couverts / max(1, s.couverts + len(s.nus))
        print(f"  {page:22} couverts {s.couverts:3}  nus {len(s.nus):3}   {pc:5.1f} %")
        for ligne, t in (s.nus if tout else s.nus[:10]):
            print(f"        ligne {ligne:4} · {t}")
        if not tout and len(s.nus) > 10:
            print(f"        … et {len(s.nus) - 10} autres")
    pc = 100 * total_ok / max(1, total_ok + total_nu)
    print(f"\n  TOTAL   couverts {total_ok}   non couverts {total_nu}   {pc:.1f} %")
    return 0 if total_nu == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
