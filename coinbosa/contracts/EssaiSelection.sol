// SPDX-License-Identifier: MIT
pragma solidity 0.8.26;

import "./CoinbosaStake.sol";

/**
 * EssaiSelection — HARNAIS DE TEST. **N'EST PAS DESTINE AU DEPLOIEMENT.**
 *
 * NE JAMAIS mettre ce contrat dans un genesis, ni dans un upgrade.go, ni le
 * deployer sur une chaine publique. Il expose des ecritures d'etat ARBITRAIRES
 * (jeu elu, classement, enjeux, etats, attestation d'amorcage) sans aucun
 * controle d'appelant : quiconque l'appellerait pourrait se nommer validateur.
 * Il n'existe que pour etre pose par SURCHARGE DE CODE dans un eth_call
 * (`stateOverride.code`), ou l'etat ecrit est jete a la fin de l'appel et ou
 * personne n'a jamais la main dessus.
 *
 * POURQUOI IL EXISTE. L'invariant de surete de CoinbosaStake — t <= 2q-1, avec
 * t la taille du jeu elu et q le nombre de scelleurs AVERES qu'il contient —
 * vit dans `_construire`, une fonction `internal view` que rien n'atteint de
 * l'exterieur : `_recalculer` n'est appelee que par `enregistrerScellage()`, au
 * seul bloc ou (block.number+1) % 200 == 0, et seulement si l'appel franchit
 * les trois gardes systeme. Il n'existe aucun cadre de test Solidity dans ce
 * depot (ni hardhat, ni foundry) et on ne peut en ajouter aucun : la version de
 * solc fige le bytecode du contrat systeme, donc l'identite de la chaine.
 * Ce harnais est la seule facon d'amener `_construire` sur un etat CHOISI sans
 * toucher une ligne de CoinbosaStake ni une dependance npm.
 *
 * CE QU'IL NE FAIT PAS. Il ne reimplemente RIEN. Il n'appelle que les fonctions
 * reelles (`_construire`, `_recalculer`, `enregistrerScellage`, `_vuProduire`,
 * `_eligible`, `_mot`, `getMiningValidators`) et se contente de leur donner un
 * etat de depart puis de rapporter ce qu'elles ont produit. Les grandeurs q et
 * t1 rendues ci-dessous sont DEDUITES de la sortie de `_construire` a l'aide du
 * predicat `_vuProduire` du contrat lui-meme, jamais recalculees par une regle
 * concurrente : un harnais qui refait le calcul qu'il teste ne teste rien.
 *
 * La taille deployee depasse peut-etre EIP-170 ; c'est sans effet, une
 * surcharge de code par eth_call n'est pas soumise a cette limite.
 */
contract EssaiSelection is CoinbosaStake {
    /// Un validateur du scenario. `elu` le place dans l'ANCIEN cache de
    /// consensus (celui sur lequel kMax est calcule), `classe` le met au
    /// classement (le vivier ou puisent les deux passages). Les deux sont
    /// independants : c'est precisement leur ECART qui fait vivre l'invariant —
    /// un titulaire avere mais devenu inegible gonfle p sans pouvoir gonfler q.
    struct Membre {
        address adresse;
        uint96 enjeu;
        uint96 enjeuMinAdmission;
        uint64 dernierBlocScelle;
        uint64 dateCandidature;
        uint32 absences;
        uint8 etat;
        bool elu;
        bool classe;
    }

    /// Un etat de depart complet. `motsBruts` / `nbClassesForce` / `nbElusForce`
    /// servent aux etats que le chemin honnete ne sait pas produire : classement
    /// non trie, mot pointant sur l'adresse nulle, doublon, compteur hors
    /// bornes. Ils existent pour verifier les gardes d'indice, pas pour tricher.
    struct Scenario {
        Membre[] membres;
        uint256[] motsBruts;
        uint256 nbClassesForce;
        uint256 nbElusForce;
        address atteste;
        uint64 attesteDepuis;
        bool amorcageClos;
    }

    /// Ce que le test observe. `jeu`/`votes` sont exactement ce que Parlia lira
    /// (getMiningValidators, repli compris), `q` compte les averes DEDANS : c'est
    /// la grandeur qui decide si le quorum ⌊N/2⌋+1 est tenable ou non.
    struct Bilan {
        address[] jeu;
        bytes[] votes;
        uint256 nbAvant;
        uint256 nbApres;
        uint256 pReel;
        uint256 q;
        bool appelSystemeOk;
    }

    // ------------------------------------------------------------------
    // Installation de l'etat
    // ------------------------------------------------------------------

    function _installer(Scenario calldata s) internal {
        uint256 nE = 0;
        uint256 nC = 0;
        for (uint256 i = 0; i < s.membres.length; ++i) {
            Membre calldata m = s.membres[i];
            Entree storage e = entrees[m.adresse];
            e.enjeu = m.enjeu;
            e.enjeuMinAdmission = m.enjeuMinAdmission;
            e.dernierBlocScelle = m.dernierBlocScelle;
            e.dateCandidature = m.dateCandidature;
            e.absencesConsecutives = m.absences;
            e.etat = m.etat;
            e.voteA = keccak256(abi.encodePacked("essai-voteA", m.adresse));
            e.voteB = bytes16(keccak256(abi.encodePacked("essai-voteB", m.adresse)));
            if (m.elu && nE < MAX_PLACES) {
                elusAdresse[nE] = m.adresse;
                // La cle de la place 0 vient des constantes dans le vrai
                // _ecrire ; l'imiter ici evite un faux ecart au moment de
                // comparer l'avant et l'apres.
                if (m.adresse == VALIDATEUR_GENESE) {
                    elusVoteA[nE] = VOTE_GENESE_A;
                    elusVoteB[nE] = VOTE_GENESE_B;
                } else {
                    elusVoteA[nE] = e.voteA;
                    elusVoteB[nE] = e.voteB;
                }
                unchecked { ++nE; }
            }
            if (m.classe && nC < TAILLE_CLASSEMENT) {
                classement[nC] = _mot(m.adresse, m.enjeu);
                unchecked { ++nC; }
            }
        }

        // Le classement REEL est trie par mot decroissant (cf. §7 du contrat).
        // Le harnais le trie donc lui aussi : tester _construire sur un
        // classement desordonne testerait un etat que le contrat ne produit
        // jamais, et masquerait le fait que les deux passages s'arretent au
        // premier candidat servi.
        for (uint256 i = 1; i < nC; ++i) {
            uint256 v = classement[i];
            uint256 j = i;
            while (j > 0 && classement[j - 1] < v) {
                classement[j] = classement[j - 1];
                unchecked { --j; }
            }
            classement[j] = v;
        }

        nbElus = nE;
        nbClasses = nC;

        for (uint256 i = 0; i < s.motsBruts.length && i < TAILLE_CLASSEMENT; ++i) {
            classement[i] = s.motsBruts[i];
        }
        if (s.motsBruts.length != 0) nbClasses = s.motsBruts.length;
        if (s.nbClassesForce != 0) nbClasses = s.nbClassesForce;
        if (s.nbElusForce != 0) nbElus = s.nbElusForce;

        attesteEnCours = s.atteste;
        attesteDepuisBloc = s.attesteDepuis;
        amorcageTermine = s.amorcageClos;
    }

    // ------------------------------------------------------------------
    // Les trois points d'observation
    // ------------------------------------------------------------------

    /// LE point de l'invariant, nu : `_construire` seule, avec un kMax impose.
    /// Isoler ainsi evite de confondre trois plafonds distincts — 2q-1 (la
    /// garde du passage 2), nAnc+1 (le rail de croissance) et 41 (MAX_PLACES).
    /// Un test qui ne passe que par `_recalculer` croit tester l'invariant alors
    /// que c'est souvent nAnc+1 qui a mordu en premier.
    ///
    /// `t1` = taille a la fin du PASSAGE 1, rededuite de la sortie : les averes
    /// occupent forcement un prefixe contigu a partir de l'indice 1, les non
    /// averes viennent apres. Cette separation permet d'attribuer chaque place a
    /// son passage, donc de dire QUI a viole quoi.
    function essaiConstruire(Scenario calldata s, uint256 kMax)
        external
        returns (address[] memory sel, uint256 t, uint256 q, uint256 t1)
    {
        _installer(s);
        (sel, t) = _construire(kMax);
        for (uint256 i = 0; i < t; ++i) {
            if (_vuProduire(sel[i])) { ++q; }
        }
        t1 = 1;
        while (t1 < t && _vuProduire(sel[t1])) { ++t1; }
    }

    /// Le pipeline d'epoque complet — kMax, quarantaines, _construire,
    /// _remplacer, _ecrire — appele directement. Ce que rend `jeu` est ce que le
    /// client lira reellement au bloc d'epoch.
    function essaiRecalculer(Scenario calldata s) external returns (Bilan memory b) {
        _installer(s);
        address[] memory anc = _ancien();
        b.nbAvant = nbElus;
        _recalculer();
        b.appelSystemeOk = true;
        _cloturer(b, anc);
    }

    /// LE VRAI POINT D'ENTREE, en deux temps.
    ///
    /// `enregistrerScellage()` exige msg.sender == block.coinbase. Le harnais ne
    /// peut donc PAS l'appeler lui-meme : l'EVM de BSC refuse d'executer le
    /// moindre code a l'adresse du coinbase (core/vm/interpreter.go,
    /// ErrCoinbaseAsContract — verifie sur ce binaire). Poser le harnais a
    /// l'adresse du scelleur est impossible par construction, et c'est une
    /// propriete de la chaine, pas une limite de l'outillage.
    ///
    /// La suite passe donc par `eth_simulateV1` : un premier appel pose l'etat
    /// (`essaiInstaller`), un second est le VRAI appel systeme — emis par le
    /// scelleur, vers le contrat, dans le meme bloc simule, au bloc 199 ou
    /// (block.number+1) % 200 == 0. Les trois gardes de `_estAppelSysteme` sont
    /// franchies pour de vrai, aucune n'est contournee. Un troisieme appel lit
    /// le bilan. C'est le seul chemin qui prouve que `_recalculer` est REELLEMENT
    /// atteignable, et le seul ou un revert de la transaction systeme se voit.
    function essaiInstaller(Scenario calldata s) external {
        _installer(s);
    }

    /// Le bilan seul, lu apres coup. `anc` est l'ancien jeu, que l'appelant a
    /// releve avant l'appel systeme : pReel doit se mesurer sur LUI, pas sur le
    /// jeu d'apres.
    function essaiBilan(address[] calldata anc) external view returns (Bilan memory b) {
        b.nbApres = nbElus;
        _cloturer(b, anc);
    }

    /// Croisement des predicats : le test recalcule son oracle en JavaScript, et
    /// un oracle qui derive rend le reste de la suite muette. On lui fait donc
    /// verifier ses deux briques de base contre les fonctions du contrat.
    function essaiPredicats(Scenario calldata s, address[] calldata qui)
        external
        returns (bool[] memory vu, bool[] memory elig, uint256[] memory mots)
    {
        _installer(s);
        vu = new bool[](qui.length);
        elig = new bool[](qui.length);
        mots = new uint256[](nbClasses > TAILLE_CLASSEMENT ? TAILLE_CLASSEMENT : nbClasses);
        for (uint256 i = 0; i < qui.length; ++i) {
            vu[i] = _vuProduire(qui[i]);
            elig[i] = _eligible(qui[i]);
        }
        for (uint256 i = 0; i < mots.length; ++i) mots[i] = classement[i];
    }

    // ------------------------------------------------------------------

    function _ancien() internal view returns (address[] memory anc) {
        uint256 n = nbElus;
        if (n > MAX_PLACES) n = MAX_PLACES;
        anc = new address[](n);
        for (uint256 i = 0; i < n; ++i) anc[i] = elusAdresse[i];
    }

    /// pReel est mesure APRES l'appel, sur l'ANCIEN jeu garde en memoire : c'est
    /// la seule mesure fidele, car `enregistrerScellage()` inscrit le scelleur
    /// du bloc AVANT de declencher le recalcul, et ce scelleur-la compte.
    function _cloturer(Bilan memory b, address[] memory anc) internal view {
        for (uint256 i = 0; i < anc.length; ++i) {
            if (_vuProduire(anc[i])) { ++b.pReel; }
        }
        (address[] memory v, bytes[] memory w) = this.getMiningValidators();
        b.jeu = v;
        b.votes = w;
        b.nbApres = nbElus;
        for (uint256 i = 0; i < v.length; ++i) {
            if (_vuProduire(v[i])) { ++b.q; }
        }
    }
}
