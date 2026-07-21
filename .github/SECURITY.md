# Politique de sécurité — Coinbosa Chain

## Signaler une vulnérabilité

**N'ouvrez pas d'issue publique pour une faille de sécurité.** Une issue est visible de tous, y
compris de qui voudrait exploiter la faille avant qu'elle soit corrigée.

Utilisez l'onglet **Security → Report a vulnerability** de ce dépôt, qui ouvre un canal privé
entre vous et les mainteneurs.

Un rapport utile contient : le composant touché, les étapes de reproduction, l'impact que vous
estimez, et la version ou le commit concerné. Une preuve de concept accélère beaucoup le
traitement.

### Ce à quoi vous pouvez vous attendre

| | |
|---|---|
| Accusé de réception | sous 72 heures |
| Première évaluation | sous 7 jours |
| Publication | après correction, avec mention de votre découverte si vous le souhaitez |

Nous ne pratiquons pas de programme de récompense à ce jour. Nous le dirons ici si cela change,
plutôt que de le laisser supposer.

## Périmètre

**Concerné** — le client dans ce dépôt, en particulier les modifications Coinbosa de
`consensus/parlia/`, les contrats de `coinbosa/contracts/`, les scripts de génération du genesis
et de déploiement, et la configuration du réseau.

`CoinbosaValidatorSet` mérite une attention particulière : il gouverne le consensus. Toute
fonction de son chemin critique qui pourrait revert arrêterait la chaîne, puisqu'un revert rend
le bloc improduisible.

**Hors périmètre** — les vulnérabilités du client BNB Smart Chain amont non introduites par nos
modifications relèvent de [`bnb-chain/bsc`](https://github.com/bnb-chain/bsc/security). Les
dépendances tierces relèvent de leurs propres mainteneurs, mais signalez-les nous tout de même
si elles nous exposent.

## État de la sécurité — sans détour

Ce réseau **n'a pas fait l'objet d'un audit externe**. Il ne doit porter aucune valeur réelle
tant que ce ne sera pas le cas.

Faiblesses connues et assumées à ce stade :

- **Un seul validateur.** Aucune tolérance aux pannes ni sécurité byzantine. Le passage à
  plusieurs validateurs est le chantier prioritaire.
- **Pas de finalité rapide.** Les clés BLS sont à zéro, le vote d'attestation est inactif.
- **La couche d'enjeu n'est pas implémentée.** Le contrat système expose un set de validateurs
  fixe : ni dépôt, ni élection par l'enjeu, ni sanction automatique. Un validateur fautif ne
  peut être écarté que manuellement.
- **Concentration de l'offre.** L'intégralité des jetons et la propriété du contrat sont
  détenues par une adresse unique, protégée par une seule clé privée.

Ces points sont documentés plutôt que tus, parce que les découvrir soi-même dans un dépôt qui
prétend le contraire coûte bien plus cher en confiance.

## Gestion des clés

Les clés de validateur doivent être **générées sur le serveur qui les utilise**. Elles ne
doivent jamais transiter par un poste de travail, une messagerie, ni ce dépôt.

Le `.gitignore` exclut `node*/`, `pw.txt` et `.env`. Ce n'est pas une formalité : un keystore
poussé par mégarde est compromis définitivement, y compris après suppression du commit.

Si une clé a fuité, considérez-la comme compromise et remplacez le validateur par
`updateValidatorSet()` — ne tentez pas de « nettoyer » l'historique git.
