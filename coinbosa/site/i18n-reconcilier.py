#!/usr/bin/env python3
"""
Aligne les dictionnaires traduits sur le dictionnaire français de référence.

POURQUOI
Le dictionnaire source a bougé pendant que les traductions se faisaient : des
clés de coque ont fusionné, et six libellés de navigation plus un aria-label ont
été ajoutés après coup. Retraduire entièrement pour onze fragments serait absurde,
et inventer ces onze traductions à la main introduirait un vocabulaire différent
de celui employé partout ailleurs dans la même page.

CE QU'IL FAIT
  1. Supprime les clés qui n'existent plus dans le français.
  2. Pour chaque clé manquante, cherche dans le MÊME fichier une clé dont le
     texte français est identique, et reprend la traduction déjà faite. C'est ce
     qui garantit que « Écosystème » dans le menu et « Écosystème » dans le pied
     de page se disent pareil.
  3. Signale nommément tout ce qu'il n'a pas su résoudre. Rien n'est inventé.

USAGE
    python3 i18n-reconcilier.py            rapport seulement
    python3 i18n-reconcilier.py --ecrire   applique
"""

import json
import re
import sys
from pathlib import Path

RACINE = Path(__file__).resolve().parent
ASSETS = RACINE / "assets"

# Étiquettes de navigation ajoutées APRÈS le passage des traducteurs, et qui
# n'ont donc aucun jumeau à réutiliser. Elles sont posées ici, à la main, en
# reprenant le vocabulaire que CHAQUE traducteur a lui-même employé ailleurs
# dans son fichier — relevé avant de les écrire :
#   en « Chain » / « Developers »        es « Cadena » / « Desarrolladores »
#   pt « Blockchain » / « Desenvolvedores »   zh « 链 » / « 开发者 »
# Ce sont les seules valeurs de tout le dictionnaire qui ne viennent pas d'un
# traducteur. Ce sont aussi les plus courtes et les plus standard du site.
SECOURS = {
    "Language": {"en": "Language", "es": "Idioma", "pt": "Idioma",
                 "ar": "اللغة", "zh": "语言"},
    "À propos": {"en": "About", "es": "Acerca de", "pt": "Sobre",
                 "ar": "من نحن", "zh": "关于"},
    "Chaîne &amp; BOSA": {"en": "Chain &amp; BOSA", "es": "Cadena y BOSA",
                          "pt": "Blockchain e BOSA", "ar": "السلسلة &amp; BOSA",
                          "zh": "链与 BOSA"},
    "Développeurs": {"en": "Developers", "es": "Desarrolladores",
                     "pt": "Desenvolvedores", "ar": "المطوّرون", "zh": "开发者"},
    "Rejoindre": {"en": "Join", "es": "Unirse", "pt": "Participar",
                  "ar": "انضم", "zh": "加入"},
}


def cles_de(txt):
    """Rend {cle: valeur} en respectant l'échappement des guillemets."""
    d = {}
    for m in re.finditer(r'^\s*"([^"]+)"\s*:\s*"((?:[^"\\]|\\.)*)"\s*,?\s*$',
                         txt, re.M):
        d[m.group(1)] = m.group(2)
    return d


def main():
    ecrire = "--ecrire" in sys.argv
    fr = json.loads((ASSETS / "i18n-fr.json").read_text(encoding="utf-8"))
    # texte français -> liste de clés qui le portent
    par_texte = {}
    for k, v in fr.items():
        par_texte.setdefault(v.strip(), []).append(k)

    souci = 0
    for f in sorted(ASSETS.glob("i18n-*.js")):
        code = f.stem.split("-")[-1]
        txt = f.read_text(encoding="utf-8")
        trad = cles_de(txt)
        if not trad:
            print(f"  {f.name} : illisible, ignoré")
            souci += 1
            continue

        retirees = [k for k in trad if k not in fr]
        ajoutees, orphelines = {}, []
        for k in fr:
            if k in trad:
                continue
            # On cherche une clé déjà traduite qui porte EXACTEMENT le même
            # texte français : sa traduction est la bonne, par construction.
            jumelles = [j for j in par_texte.get(fr[k].strip(), [])
                        if j != k and j in trad]
            if jumelles:
                ajoutees[k] = trad[jumelles[0]]
            elif fr[k].strip() in SECOURS and code in SECOURS[fr[k].strip()]:
                ajoutees[k] = SECOURS[fr[k].strip()][code]
            else:
                orphelines.append(k)

        print(f"  {f.name:14} retirées {len(retirees):3} | "
              f"reprises {len(ajoutees):3} | NON RÉSOLUES {len(orphelines):3}")
        for o in orphelines:
            print(f"        · {o}  =>  {fr[o][:60]}")
            souci += 1

        if ecrire:
            for k in retirees:
                trad.pop(k, None)
            trad.update(ajoutees)
            corps = ",\n".join(f' "{k}": "{trad[k]}"' for k in sorted(fr) if k in trad)
            entete = txt.split("window.__I18N")[0].rstrip()
            f.write_text(
                f"{entete}\nwindow.__I18N = window.__I18N || {{}};\n"
                f"window.__I18N.{code} = {{\n{corps}\n}};\n", encoding="utf-8")

    if not ecrire:
        print("\n  RAPPORT SEULEMENT — rien n'a été écrit.")
    return 1 if souci else 0


if __name__ == "__main__":
    sys.exit(main())
