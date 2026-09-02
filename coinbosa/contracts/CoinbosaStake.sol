// SPDX-License-Identifier: MIT
pragma solidity 0.8.26;

/**
 * CoinbosaStake — couche d'enjeu PoBS (preuve d'enjeu bornee), voie B.
 *
 * A partir de pobsTime, le moteur Parlia lit le jeu de validateurs ICI au lieu
 * de 0x...1000 (consensus/parlia/parlia.go, getCurrentValidators). La signature
 * lue est exactement celle du contrat fige :
 *
 *   getMiningValidators() -> (address[], bytes[])      selecteur 0x4df6e0c3
 *
 * (verifie : keccak256("getMiningValidators()")[0:4] = 0x4df6e0c3, identique a
 *  ce que p.validatorSetABI.Pack produit.)
 *
 * REGLE D'OR, heritee du contrat fige et NON negociable : aucune fonction du
 * chemin de consensus ne peut revert. Un revert rend le bloc improduisible
 * (Prepare) ou invalide (Finalize) — et sur une chaine a UN validateur, plus
 * aucun bloc ne veut dire plus aucune transaction corrective. Les cinq
 * fonctions concernees sont :
 *
 *   getMiningValidators()   lecture, chaque bloc d'epoch
 *   getValidators()         lecture, chemin pre-Luban + outils
 *   getTurnLength()         lecture, mort tant que Bohr est inactif
 *   slash(address)          ecriture, tx systeme          selecteur 0xc96be4cb
 *   enregistrerScellage()   ecriture, tx systeme, chaque bloc
 *
 * Elles n'ont ni require, ni revert, ni modifier, ni appel externe, ni boucle
 * non bornee. Toute condition de refus s'exprime par un evenement suivi d'un
 * `return`. Le reste du contrat — depot, retrait, sanction hors consensus,
 * gouvernance — REVERT volontiers : un echec silencieux qui garde l'argent de
 * quelqu'un serait bien pire qu'un echec bruyant.
 *
 * NI CONSTRUCTOR, NI IMMUTABLE. Le deploiement peut se faire par injection de
 * bytecode au fork (core/systemcontracts/upgrade.go:1258 fait statedb.SetCode
 * SANS executer de constructeur) : une variable `immutable` resterait a zero, et
 * un contrat dont VALIDATEUR_GENESE vaut zero rend un ensemble sans scelleur au
 * premier bloc d'epoch. Corollaire assume : l'ETAT VIDE doit etre un etat
 * initial VALIDE. Il l'est — nbElus == 0 declenche le repli genese, et
 * enjeuMinimum() rend le plancher quand _enjeuMinimum vaut 0. Il n'y a donc
 * aucune fonction init() a proteger.
 *
 * NI PROXY, NI DELEGATECALL, NI SELFDESTRUCT. Le client epingle le codeHash de
 * cette adresse ; un code qui peut changer ou disparaitre ferait retomber le
 * consensus ailleurs, silencieusement.
 */
contract CoinbosaStake {
    // =====================================================================
    // 1. CONSTANTES GRAVEES
    //
    // Motif blanc-marque identique a CoinbosaValidatorSet : les adresses
    // ci-dessous sont REECRITES dans la source par le script de deploiement
    // avant compilation. Elles ne peuvent pas etre des `immutable` (voir
    // l'en-tete) ni des variables d'etat : une variable d'etat serait a zero
    // dans un deploiement par SetCode.
    // =====================================================================

    /// VALIDATEUR DE GENESE. Valeur par defaut = le validateur reellement
    /// inscrit dans l'extraData du bloc 0 de Coinbosa Chain (decode :
    /// extraData[33:53]). Cette adresse est la SEULE dont on sait, par
    /// construction, qu'un noeud detient la cle de scellage. C'est pour cela
    /// qu'elle occupe la place 0 sans condition et qu'elle est le repli de
    /// toutes les fonctions de lecture : rendre un ensemble sans elle
    /// reviendrait a annoncer au consensus des validateurs dont personne ne
    /// detient la cle, et la chaine s'arreterait au bloc d'epoch.
    address internal constant VALIDATEUR_GENESE = 0x3986D6b31EC55043CeaAF25f5dDEa53517CBba50;

    /// Cle de vote BLS du validateur de genese, coupee en deux morceaux de
    /// 32 + 16 octets. Elle doit etre BYTE-IDENTIQUE a celle de l'extraData du
    /// bloc 0 (decode : extraData[53:101] = 48 octets nuls). Stockee en deux
    /// morceaux de taille FIXE et jamais en `bytes` : c'est ce qui rend
    /// structurellement impossible de rendre une cle de longueur != 48.
    bytes32 internal constant VOTE_GENESE_A = bytes32(0);
    bytes16 internal constant VOTE_GENESE_B = bytes16(0);

    /// CONSEIL — seul pouvoir : proposer un nouveau minimum d'enjeu, dans un
    /// couloir borne, avec preavis et sous veto. Il ne peut ni sanctionner, ni
    /// admettre, ni exclure, ni toucher un wei. Valeur par defaut volontairement
    /// INERTE : si le script de deploiement oublie de la reecrire, la
    /// gouvernance est morte et le minimum reste au plancher. Une gouvernance
    /// morte est un incident ; une gouvernance ouverte par defaut serait une
    /// porte derobee.
    address internal constant CONSEIL = 0x0000000000000000000000000000000000000001;

    /// CONSEIL D'AMORCAGE — seul pouvoir : attester qu'un candidat produit
    /// reellement des blocs, au plus 4 fois dans la vie du contrat, un seul
    /// candidat expose a la fois. Meme defaut inerte, meme raison.
    address internal constant CONSEIL_AMORCAGE = 0x0000000000000000000000000000000000000003;

    /// Destination des fonds confisques. Adresse publique sans cle : n'importe
    /// qui verifie le retrait de circulation avec un seul eth_getBalance, sans
    /// lire ce code. Un compteur interne serait aussi sur, mais pas verifiable
    /// de l'exterieur.
    address internal constant PUITS = 0x000000000000000000000000000000000000dEaD;

    uint256 public constant MAX_PLACES = 41;
    uint256 internal constant LONGUEUR_ADRESSE_VOTE = 48;

    /// Bornes DURES du minimum d'enjeu. Le plancher est la decision de
    /// l'editeur (1 000 BOSA = 0,000143 % de l'offre). Le plafond vaut 0,5 % de
    /// l'offre : a ce niveau, remplir les 41 places coute 143 500 000 BOSA, soit
    /// 20,5 % de l'offre — plus que le plus gros poste de tresorerie. Au-dela on
    /// ne renforcerait plus rien, on rendrait les places invendables. Sans ces
    /// deux bornes, une cle de conseil compromise met le minimum a zero (les 41
    /// places deviennent gratuites) ou a l'infini (plus aucun candidat).
    uint256 public constant ENJEU_MIN_PLANCHER = 1_000e18;
    uint256 public constant ENJEU_MIN_PLAFOND = 3_500_000e18;

    /// 49 jours — decision de l'editeur. Le deblocage n'a qu'une fonction :
    /// garder les fonds saisissables assez longtemps pour qu'une faute commise
    /// pendant le service soit encore punissable.
    uint256 internal constant DELAI_DEBLOCAGE = 49 days;

    /// Garde anti-horloge. L'horodatage des blocs est ecrit par les validateurs
    /// eux-memes : une majorite qui s'entend pourrait l'avancer et raccourcir sa
    /// propre purge. Il faut donc AUSSI avoir produit des blocs. 80 % de 49
    /// jours a 5 s/bloc (parlia period = 5 dans genesis-coinbosa.json) laisse la
    /// marge d'une chaine qui produit un peu lentement, sans laisser celle d'une
    /// chaine qui ment sur l'heure.
    uint256 internal constant BLOCS_DEBLOCAGE_MIN = 677_376;

    /// Registre de candidats BORNE a 2 x MAX_PLACES. Un registre non borne rend
    /// le cout d'insertion proportionnel au nombre de candidats : au-dela de la
    /// limite de gaz du bloc (40 000 000), plus personne ne pourrait ni deposer
    /// ni retirer, et les fonds immobilises deviendraient inaccessibles.
    uint256 internal constant TAILLE_CLASSEMENT = 82;

    uint256 internal constant EPOQUE = 200; // genesis-coinbosa.json : parlia.epoch

    /// Troisieme garde des appels systeme. Voir _estAppelSysteme().
    uint256 internal constant SEUIL_GAZ_SYSTEME = 1e12;

    /// Surenchere minimale pour prendre la place d'un titulaire : +5 %. Sans
    /// marge, un candidat qui depose un wei de plus fait sauter le dernier, qui
    /// riposte au bloc suivant. Un jeu de validateurs qui permute a chaque
    /// epoque est un jeu qui ne produit plus : a chaque bascule, un noeud eteint
    /// et un noeud allume, et le quorum vacille.
    uint256 internal constant SURENCHERE_NUM = 1050;
    uint256 internal constant SURENCHERE_DEN = 1000;

    /// Un candidat frais attend 7 jours avant d'etre elisible. Le temps que
    /// quelqu'un le voie, l'interroge, et constate que son noeud tourne.
    uint256 internal constant DELAI_CANDIDATURE = 7 days;

    uint32 internal constant PAS_ABSENCE = 10;          // 0,1 % d'enjeu tous les 10 tours manques
    uint32 internal constant SEUIL_QUARANTAINE = 50;    // absences consecutives
    uint256 internal constant BLOCS_SILENCE = 4_320;    // 6 h a 5 s/bloc
    uint256 internal constant DUREE_QUARANTAINE = 24 hours;
    uint8 internal constant SEUIL_BANNISSEMENT = 3;     // quarantaines en 30 jours
    uint256 internal constant FENETRE_QUARANTAINES = 30 days;

    /// 21 jours a 5 s/bloc. Une preuve de double signature plus ancienne n'a
    /// plus d'objet : la periode de purge de 49 jours la couvre largement.
    uint256 internal constant FENETRE_PREUVE_BLOCS = 362_880;

    /// Prime de denonciation : 1 % du montant confisque, plafonnee a 10 BOSA, et
    /// ELLE-MEME verrouillee 49 jours. Sans le plafond ET sans le verrou, un
    /// validateur double-signerait volontairement, se denoncerait depuis une
    /// seconde adresse et recupererait une part de son propre enjeu
    /// immediatement : la sanction deviendrait un guichet de retrait rapide.
    /// Interdire l'auto-denonciation ne sert a rien — une seconde adresse coute
    /// zero. Seuls le plafond et le verrou la rendent non rentable.
    uint256 internal constant PRIME_POUR_CENT = 1;
    uint256 internal constant PRIME_PLAFOND = 10e18;

    uint256 internal constant DELAI_GOUVERNANCE = 14 days;
    uint8 internal constant MAX_ATTESTATIONS_AMORCAGE = 4;

    // Etats d'une entree.
    uint8 internal constant INEXISTANT = 0;
    uint8 internal constant EN_ATTENTE = 1;
    uint8 internal constant ACTIF = 2;
    uint8 internal constant EN_QUARANTAINE = 3;
    uint8 internal constant EN_DEBLOCAGE = 4;
    uint8 internal constant BANNI = 5;

    // =====================================================================
    // 2. ETAT
    //
    // Trois familles separees par leur chemin d'acces. C'est la decision
    // structurante du contrat : CE QUE LE CONSENSUS LIT A CHAQUE EPOQUE N'EST
    // JAMAIS CE QUE LA LOGIQUE METIER CALCULE. Toute l'election se fait a
    // l'ECRITURE, une fois par epoque ; la lecture ne fait que parcourir un
    // cache deja calcule. Sans cette separation, le cout de getMiningValidators
    // dependrait du nombre de candidats et finirait par depasser le plafond de
    // gaz de l'eth_call (RPCGasCap, 50 000 000 par defaut) — un plafond
    // REGLABLE PAR NOEUD, donc une source de divergence entre noeuds.
    // =====================================================================

    // --- A. cache de consensus : ecrit une fois par epoque, lu par le client ---
    uint256 public nbElus;
    address[41] internal elusAdresse;
    bytes32[41] internal elusVoteA; // octets 0..31 de la cle BLS
    bytes16[41] internal elusVoteB; // octets 32..47 de la cle BLS

    // --- B. classement : trie, borne, jamais lu par le client ---
    //
    // mot = enjeu (96 bits de poids fort) | (type(uint160).max - adresse)
    // Trie par mot DECROISSANT, cela donne exactement : enjeu decroissant, puis
    // adresse croissante. Les adresses etant uniques, le mot est unique :
    // l'ordre est un ordre TOTAL STRICT, sans ex aequo possible, et le contenu
    // du classement est une fonction pure de l'ensemble {(adresse, enjeu)},
    // independante de l'ordre d'arrivee des depots. Sans cela, deux enjeux egaux
    // seraient departages par la position dans le tableau, donc par l'historique
    // des depots et retraits d'AUTRUI : un validateur ejecte sans qu'on puisse
    // lui expliquer pourquoi.
    uint256[82] internal classement;
    uint256 public nbClasses;

    // --- C. entrees : le detail par validateur ---
    struct Entree {
        // emplacement 1 — exactement 256 bits
        uint96 enjeu;
        uint64 dernierBlocScelle;
        uint64 dateCandidature;
        uint32 absencesConsecutives;
        // emplacement 2
        uint96 enjeuMinAdmission; // le minimum EN VIGUEUR le jour de l'admission
        uint64 dateDeblocage;
        uint64 blocReferenceDeblocage;
        uint8 etat;
        uint8 quarantainesRecentes;
        // emplacement 3
        bytes32 voteA;
        // emplacement 4 — 128 + 64 + 64 = exactement 256 bits
        bytes16 voteB;
        uint64 dateDerniereQuarantaine;
        /// Bloc ou l'argent a ete MIS EN JEU. Un enjeu ne repond pas d'une
        /// faute commise AVANT d'etre depose : signalerDoubleSignature refuse
        /// une preuve de hauteur inferieure. Ecrit a la CREATION de l'entree
        /// uniquement — jamais par ajouterEnjeu(), jamais par
        /// sortirDeQuarantaine(), sans quoi il suffirait de se faire mettre en
        /// quarantaine pour blanchir son passe. Vaut 0 pour une entree qui n'a
        /// rien depose, et 0 est la valeur la PLUS sanctionnable : le defaut
        /// penche du cote sur.
        uint64 blocEngagement;
    }

    mapping(address => Entree) internal entrees;
    /// Unicite des cles de vote. Deux validateurs partageant une cle BLS
    /// produiraient des attestations indistinguables le jour ou la finalite
    /// rapide serait activee.
    mapping(bytes32 => address) public proprietaireCleVote;

    // --- comptabilite des sanctions ---
    uint256 public aBruler;     // confisque, pas encore envoye au puits
    uint256 public totalPurge;  // cumul envoye au puits
    uint256 public primesDues;
    mapping(address => uint256) public primeDe;
    mapping(address => uint64) public primeDisponibleLe;
    /// Cle = (fautif, hauteur), PAS le hash de la preuve : voir le long
    /// commentaire de signalerDoubleSignature, section 10.
    mapping(bytes32 => bool) public infractionSanctionnee;

    // --- gouvernance du minimum d'enjeu ---
    uint96 internal _enjeuMinimum;      // 0 == plancher (etat vide valide)
    uint96 internal _enjeuMinPropose;
    uint64 public dateEffetProposition; // 0 == aucune proposition en cours
    uint64 public dateDernierChangement;
    uint256 public cycleProposition;
    uint256 internal _vetoCompte;
    mapping(uint256 => mapping(address => bool)) internal _vetoDonne;

    // --- amorcage ---
    bool public amorcageTermine;
    uint8 public attestationsUtilisees;
    address public attesteEnCours;
    uint64 public attesteDepuisBloc;

    // =====================================================================
    // 3. EVENEMENTS ET ERREURS
    // =====================================================================

    event Depot(address indexed validateur, uint256 montant, bytes cleVote);
    event EnjeuAugmente(address indexed validateur, uint256 ajout, uint256 total);
    event CautionDeposee(address indexed validateur, uint256 ajout, uint256 total);
    event RetraitDemande(address indexed validateur, uint64 dateDeblocage);
    event Retire(address indexed validateur, uint256 montant);
    event Declasse(address indexed validateur);
    event JeuRecalcule(uint256 taille, uint256 p, uint256 kMax);
    event RecalculInchange(uint256 taille);
    /// Un refus laisse le cache PRECEDENT intact : la chaine continue de
    /// produire avec l'ensemble d'avant, et le journal porte l'alerte. C'est la
    /// seule traduction de « echouer de facon visible » compatible avec
    /// l'interdiction du revert.
    event RecalculRefuse(uint256 raison);
    event SanctionAbsence(address indexed validateur, uint256 perte, uint32 absences);
    event SanctionIgnoree(address indexed appelant);
    event MiseEnQuarantaine(address indexed validateur, uint8 etat);
    event SortieDeQuarantaine(address indexed validateur);
    event DoubleSignature(address indexed fautif, uint256 saisi, uint256 hauteur, address indexed rapporteur);
    /// Publie tel quel : le validateur de genese ne peut PAS etre exclu, meme
    /// pour double signature. Seul son argent est expose. C'est le revers assume
    /// du verrou 2 du contrat fige, et un lecteur qui le decouvre seul y lirait
    /// une dissimulation.
    event DoubleSignatureGenese(address indexed fautif, uint256 saisi, uint256 hauteur);
    event Purge(uint256 montant);
    event PrimeReclamee(address indexed rapporteur, uint256 montant);
    event MinimumPropose(uint256 valeur, uint64 dateEffet, uint256 cycle);
    event MinimumApplique(uint256 valeur);
    event MinimumRejete(uint256 cycle, uint256 vetos);
    event ProductionAttestee(address indexed validateur);
    event AttestationExpiree(address indexed validateur);
    event AmorcageTermine(uint256 p);

    error GeneseNonCandidate();
    error ReserveeAuValidateurDeGenese();
    error DejaInscrit();
    error CleVoteInvalide();
    error CleVoteNulle();
    error CleVoteDejaPrise();
    error EnjeuInsuffisant();
    error EnjeuTropGrand();
    error MontantNul();
    error ClassementSature();
    error EtatIncompatible();
    error DeblocageTemps();
    error DeblocageHauteur();
    error QuarantaineEnCours();
    error TransfertEchoue();
    error RienAReclamer();
    error PreuveInvalide();
    error InfractionDejaSanctionnee();
    error PreuveHorsFenetre();
    error PreuveAnterieureAuDepot();
    error NonAutorise();
    error HorsBornes();
    error TropRapide();
    error PropositionEnCours();
    error AucuneProposition();
    error AmorcageClos();

    // =====================================================================
    // 4. CHEMIN DE CONSENSUS — LECTURE. AUCUN REVERT ATTEIGNABLE.
    // =====================================================================

    /// Appelee par Parlia a chaque bloc d'epoch, sur l'etat du bloc PARENT,
    /// pour construire l'extraData.
    ///
    /// Cinq proprietes, chacune fermant une panne mesuree :
    ///
    /// 1. vals.length == votes.length TOUJOURS — les deux `new` prennent la
    ///    MEME variable n. L'ABI Go n'echoue PAS sur des longueurs inegales
    ///    (mesure : unpack de 2 adresses / 1 cle -> err = nil) ; c'est la boucle
    ///    voteAddrMap de parlia.go qui PANIQUE en « index out of range ». Un
    ///    panic n'est pas une erreur rattrapee : le processus tombe, et TOUS les
    ///    noeuds tombent puisqu'ils lisent le meme etat. Le cache est de surcroit
    ///    stocke en tableaux de taille FIXE : l'inegalite de longueur n'a aucune
    ///    representation possible dans cet etat.
    /// 2. chaque votes[i] fait exactement 48 octets — abi.encodePacked(bytes32,
    ///    bytes16) ne peut rien produire d'autre. Le decodeur Go accepte
    ///    silencieusement 0, 47 ou 49 octets et remplit un [48]byte par copie
    ///    tronquee : la cle fausse partirait dans l'extraData et ne serait
    ///    decouverte que le jour de l'activation de la finalite rapide.
    /// 3. aucune adresse nulle, aucun doublon — le contrat fige filtrait les
    ///    doublons dans updateValidatorSet ; cette garde sort du chemin a
    ///    pobsTime et doit etre reecrite ici.
    /// 4. vals[0] == VALIDATEUR_GENESE — miroir du verrou 2 du contrat fige. Ce
    ///    verrou n'est PAS herite : des que Parlia lit ailleurs que 0x...1000,
    ///    le require(sealerPresent) n'est plus dans le chemin. Ne pas le
    ///    reecrire, c'est autoriser un ensemble dont aucun membre n'a de cle
    ///    detenue par un noeud.
    /// 5. aucun require, aucun revert, aucun appel externe, aucune division,
    ///    aucune soustraction : il n'existe aucun chemin de panne EVM autre que
    ///    l'epuisement de gaz — exclu par la marge (124 SLOAD froids au pire,
    ///    ~290 000 gaz contre 50 000 000 de plafond RPC).
    function getMiningValidators()
        external
        view
        returns (address[] memory vals, bytes[] memory votes)
    {
        uint256 n = nbElus;
        if (n == 0 || n > MAX_PLACES) return _repli();

        vals = new address[](n);
        votes = new bytes[](n);

        for (uint256 i = 0; i < n; ++i) {
            address a = elusAdresse[i];
            if (a == address(0)) return _repli();
            for (uint256 j = 0; j < i; ++j) {
                if (vals[j] == a) return _repli();
            }
            vals[i] = a;
            votes[i] = abi.encodePacked(elusVoteA[i], elusVoteB[i]);
        }
        if (vals[0] != VALIDATEUR_GENESE) return _repli();
    }

    /// Le repli n'est PAS un changement d'etat pour le reseau : il rend
    /// l'ensemble de production d'aujourd'hui. C'est le comportement neutre, et
    /// c'est ce qui fait de la bascule un non-evenement le premier jour — le
    /// cache est vide, le repli repond, et les deux contrats rendent les memes
    /// octets. En cas d'incoherence on retombe TOUJOURS sur le validateur de
    /// genese, JAMAIS sur un ensemble partiel : retrecir n'abaisse jamais la
    /// disponibilite (le quorum ⌊N/2⌋+1 baisse avec N), publier un ensemble
    /// partiel, si.
    function _repli()
        internal
        pure
        returns (address[] memory vals, bytes[] memory votes)
    {
        vals = new address[](1);
        vals[0] = VALIDATEUR_GENESE;
        votes = new bytes[](1);
        votes[0] = abi.encodePacked(VOTE_GENESE_A, VOTE_GENESE_B);
    }

    /// Chemin pre-Luban (mort ici) + compatibilite outils et explorateur.
    function getValidators() external view returns (address[] memory vals) {
        uint256 n = nbElus;
        if (n == 0 || n > MAX_PLACES) {
            vals = new address[](1);
            vals[0] = VALIDATEUR_GENESE;
            return vals;
        }
        vals = new address[](n);
        for (uint256 i = 0; i < n; ++i) {
            address a = elusAdresse[i];
            if (a == address(0)) {
                vals = new address[](1);
                vals[0] = VALIDATEUR_GENESE;
                return vals;
            }
            vals[i] = a;
        }
    }

    /// Requis uniquement si Bohr est active un jour. 1 = comportement
    /// historique, et c'est la valeur sur laquelle tout le reste du contrat
    /// raisonne (le quorum ⌊N/2⌋+1 vient de minerHistoryCheckLen a TurnLength=1).
    function getTurnLength() external pure returns (uint256) {
        return 1;
    }

    function numOfValidators() external view returns (uint256) {
        return nbElus;
    }

    function isCurrentValidator(address who) public view returns (bool) {
        uint256 n = nbElus;
        if (n > MAX_PLACES) n = MAX_PLACES;
        for (uint256 i = 0; i < n; ++i) {
            if (elusAdresse[i] == who) return true;
        }
        return n == 0 && who == VALIDATEUR_GENESE;
    }

    function estElu(address who) external view returns (bool) {
        return isCurrentValidator(who);
    }

    // =====================================================================
    // 5. CHEMIN DE CONSENSUS — ECRITURE. AUCUN REVERT ATTEIGNABLE.
    // =====================================================================

    /// Trois conditions, et la troisieme n'est pas decorative.
    ///
    /// Les deux premieres sont celles de BSC (parlia.go IsSystemTransaction :
    /// to appartient aux contrats systeme, gasPrice == 0, sender == coinbase).
    /// Elles NE SUFFISENT PAS ici : params.InitialBaseFeeForBSC vaut 0, donc une
    /// transaction a prix de gaz nul est parfaitement recevable sur cette
    /// chaine, et le validateur en exercice pourrait en signer une depuis sa
    /// propre adresse de coinbase pour fabriquer une sanction contre un rival.
    ///
    /// La troisieme ferme la porte. getSystemMessage fixe GasLimit =
    /// MaxUint64/2 ~= 9,2e18 et applyMessage passe cette valeur telle quelle a
    /// evm.Call sans deduire de gaz intrinseque. Une transaction d'utilisateur,
    /// elle, ne peut jamais depasser la limite de gaz du bloc — 40 000 000 dans
    /// genesis-coinbosa.json. Entre 4e7 et 9,2e18, le seuil de 1e12 laisse un
    /// facteur 25 000 d'un cote et 9 000 000 de l'autre.
    ///
    /// Aucune de ces gardes n'est un require : la fonction rend la main. La
    /// sanction est perdue, la chaine ne l'est pas, et le journal le dit.
    function _estAppelSysteme() internal view returns (bool) {
        return msg.sender == block.coinbase && tx.gasprice == 0 && gasleft() > SEUIL_GAZ_SYSTEME;
    }

    /// Appelee par le moteur quand le validateur en tour n'a pas scelle
    /// (parlia.go : slash spoiled validators). Le selecteur est impose par le
    /// client : keccak256("slash(address)")[0:4] = 0xc96be4cb.
    ///
    /// Sanction FAIBLE et PROGRESSIVE : 0,1 % de l'enjeu par tranche de 10 tours
    /// manques. Cent tours coutent ~1 %. Une panne n'est pas une malveillance ;
    /// la peine doit etre reparable. La mise en quarantaine, elle, est evaluee
    /// au recalcul d'epoque et non ici : garder ce chemin purement arithmetique
    /// est ce qui garantit qu'il ne peut pas revert.
    ///
    /// AUCUN TRANSFERT ICI. Un appel externe sur le chemin de consensus, c'est
    /// un revert possible a chaque bloc. On ne fait qu'incrementer un compteur ;
    /// purger() envoie les fonds au puits, hors consensus, quand quelqu'un le
    /// demande.
    function slash(address v) external {
        if (!_estAppelSysteme()) {
            emit SanctionIgnoree(msg.sender);
            return;
        }
        Entree storage e = entrees[v];
        if (e.etat != ACTIF) return;
        unchecked {
            uint32 n = e.absencesConsecutives + 1;
            e.absencesConsecutives = n;
            if (n % PAS_ABSENCE == 0) {
                uint96 perte = e.enjeu / 1000; // division par une constante non nulle
                e.enjeu -= perte;             // perte <= enjeu par construction
                aBruler += perte;
                emit SanctionAbsence(v, perte, n);
            }
        }
    }

    /// Nouvelle transaction systeme, un appel par bloc, apres slash().
    ///
    /// C'est LE point d'appui de tout le contrat : sans mesure on-chain de qui
    /// scelle REELLEMENT, l'invariant de surete du §5.3 serait une supposition.
    /// Une ecriture d'emplacement par bloc (~5 000 gaz sur 40 000 000).
    ///
    /// ET SI ELLE N'EST JAMAIS APPELEE — client non deploye, retour arriere,
    /// anomalie — dernierBlocScelle reste a 0, p vaut 1, kMax vaut 1, et
    /// l'ensemble se fige sur [genese]. La panne va dans le sens SUR.
    function enregistrerScellage() external {
        if (!_estAppelSysteme()) {
            emit SanctionIgnoree(msg.sender);
            return;
        }
        Entree storage e = entrees[msg.sender]; // == block.coinbase == le scelleur
        unchecked {
            e.dernierBlocScelle = uint64(block.number);
            e.absencesConsecutives = 0; // meme emplacement : un seul SSTORE
        }
        // Le client lit getMiningValidators() sur l'etat du bloc 199 pour
        // construire l'en-tete du bloc 200 : le cache doit etre ecrit au dernier
        // bloc de l'epoque, pas au premier de la suivante.
        if ((block.number + 1) % EPOQUE == 0) _recalculer();
    }

    // =====================================================================
    // 6. L'ELECTION — recalcul d'epoque
    // =====================================================================

    /// INVARIANT DE SURETE, et c'est un theoreme, pas une precaution.
    ///
    /// Parlia exige ⌊N/2⌋+1 scelleurs DISTINCTS ET EN LIGNE (snapshot.go
    /// minerHistoryCheckLen a TurnLength=1 ; mesure : N=1 -> 0, N=2 -> 1). Pose a
    /// l'envers — quelle taille N l'effectif en ligne peut-il soutenir ? :
    ///
    ///     p >= ⌊N/2⌋ + 1   <=>   ⌊N/2⌋ <= p - 1   <=>   N <= 2p - 1
    ///
    /// Le contrat n'ecrit JAMAIS un ensemble de taille superieure a 2p - 1, ou p
    /// est le nombre de membres du cache courant reellement vus sceller pendant
    /// l'epoque ecoulee. Consequences :
    ///   . p = 1 (aujourd'hui) -> N <= 1 -> l'ensemble reste [genese]. Le contrat
    ///     REFUSE TOUT SEUL d'arreter la chaine, sans gouverneur, sans
    ///     surveillance. C'est la reponse a « n'importe qui immobilise 1 000
    ///     BOSA, prend une place, ne scelle jamais, et fige le reseau ».
    ///   . remplir les 41 places demande 21 producteurs averes — exactement le
    ///     quorum de 41.
    ///   . si des validateurs tombent, p baisse et l'ensemble RETRECIT TOUT SEUL
    ///     jusqu'a retrouver un quorum tenable. La chaine se repare au lieu de
    ///     se figer.
    function _recalculer() internal {
        uint256 nAnc = nbElus;
        // Garde d'indice : nbElus ne peut depasser MAX_PLACES par construction,
        // mais un depassement ferait PANIQUER les boucles ci-dessous sur un
        // tableau de 41 — et une panique sur le chemin de consensus tue le
        // noeud. On borne plutot que d'esperer.
        if (nAnc > MAX_PLACES) nAnc = MAX_PLACES;

        uint256 pReel = 0;
        for (uint256 i = 0; i < nAnc; ++i) {
            if (_vuProduire(elusAdresse[i])) {
                unchecked { ++pReel; }
            }
        }

        uint256 p = pReel + _amorcageCompte();

        // L'amorcage se referme de lui-meme des que trois producteurs REELS sont
        // observes : a ce stade kMax vaut 5 et la regle automatique croit seule.
        if (pReel >= 3 && !amorcageTermine) {
            amorcageTermine = true;
            emit AmorcageTermine(pReel);
        }

        // ECRIT AVANT la soustraction, pas apres : 2*0 - 1 sur un uint256
        // deborde et rendrait un kMax astronomique, donc un ensemble ingerable.
        if (p == 0) p = 1;

        uint256 kMax = 2 * p - 1;
        if (kMax > MAX_PLACES) kMax = MAX_PLACES;

        // CROISSANCE BORNEE A UNE PLACE PAR EPOQUE. L'invariant ci-dessus
        // suffit en theorie ; ce rail-ci rend chaque extension OBSERVABLE. Un
        // saut de 1 a 5 places en un bloc d'epoch, meme conforme a N <= 2p-1,
        // allume quatre noeuds d'un coup et ne laisse personne constater
        // l'echec avant les 200 blocs suivants. Le retrecissement, lui, n'est
        // jamais borne : reduire est toujours sur.
        if (kMax > nAnc + 1) kMax = nAnc + 1;

        _evaluerQuarantaines(nAnc);

        (address[] memory sel, uint256 t) = _construire(kMax);
        _remplacer(sel, t, p);
        _ecrire(sel, t, nAnc, p, kMax);
    }

    /// Un membre est « avere » s'il a scelle un bloc dans l'epoque ecoulee.
    /// dernierBlocScelle == 0 signifie « jamais vu sceller » et NON « vu au bloc
    /// 0 » : sans ce test, une entree fraiche compterait comme productrice et
    /// l'invariant s'appuierait sur une observation qui n'a pas eu lieu.
    function _vuProduire(address a) internal view returns (bool) {
        unchecked {
            uint256 d = entrees[a].dernierBlocScelle;
            return d != 0 && block.number >= d && block.number - d < EPOQUE;
        }
    }

    /// L'IMPASSE 1 -> 2, et la seule sortie honnete.
    ///
    /// Avec p = 1 l'invariant fige l'ensemble a [genese] pour toujours : un
    /// candidat ne peut pas prouver qu'il sait sceller tant qu'il n'a pas de
    /// place, et il n'aura pas de place tant qu'il ne l'a pas prouve. Aucun
    /// contrat ne peut trancher cela — le contrat fige le dit lui-meme : « un
    /// contrat ne peut pas savoir quels noeuds tournent ».
    ///
    /// La sortie est bornee, publique, et se referme d'elle-meme : au plus UN
    /// validateur non prouve expose a la fois, au plus 4 attestations sur la vie
    /// du contrat, expiration au bout de deux epoques sans scellage, et
    /// amorcageTermine irreversible des que p reel atteint 3. Le conseil
    /// d'amorcage ne peut RIEN d'autre : ni exclure, ni saisir, ni classer, ni
    /// changer un parametre.
    ///
    /// Ce que le contrat ne remplace pas : la procedure hors chaine (rejouer sur
    /// une chaine jetable, provisionner le serveur, VOIR l'entrant sceller). Il
    /// en reduit la portee a un seul geste, une seule fois, et l'eteint ensuite.
    function _amorcageCompte() internal returns (uint256) {
        address att = attesteEnCours;
        if (att == address(0)) return 0;
        if (_vuProduire(att)) {
            // Il scelle vraiment : il est deja compte dans pReel. Garder
            // l'attestation le compterait DEUX fois et gonflerait kMax d'un cran
            // de trop — exactement l'erreur que l'invariant sert a eviter.
            attesteEnCours = address(0);
            return 0;
        }
        if (block.number > uint256(attesteDepuisBloc) + 2 * EPOQUE) {
            attesteEnCours = address(0);
            emit AttestationExpiree(att);
            return 0;
        }
        return 1;
    }

    /// Quarantaine et bannissement, evalues ici et non dans slash() : ce chemin
    /// peut se permettre des ecritures, celui de slash doit rester le plus court
    /// possible.
    ///
    /// Le critere de silence exige dernierBlocScelle != 0. Sans cette condition,
    /// un validateur fraichement elu — qui n'a par definition jamais scelle —
    /// serait mis en quarantaine des son premier recalcul, et l'ensemble
    /// deviendrait une porte a tambour. Un membre qui ne produit pas est de
    /// toute facon exclu de p : l'ensemble retrecit tout seul. La quarantaine
    /// n'est que la mesure plus dure qui s'y ajoute.
    function _evaluerQuarantaines(uint256 nAnc) internal {
        for (uint256 i = 0; i < nAnc; ++i) {
            address a = elusAdresse[i];
            // La place du validateur de genese est inconditionnelle : le contrat
            // fige la lui garantit, et le contredire ici risquerait l'arret.
            if (a == VALIDATEUR_GENESE || a == address(0)) continue;
            Entree storage e = entrees[a];
            if (e.etat != ACTIF) continue;

            bool silence;
            unchecked {
                uint256 d = e.dernierBlocScelle;
                silence = d != 0 && block.number >= d && block.number - d >= BLOCS_SILENCE;
            }
            if (e.absencesConsecutives < SEUIL_QUARANTAINE && !silence) continue;

            e.etat = EN_QUARANTAINE;
            e.absencesConsecutives = 0;
            _repousserDeblocage(a);
            unchecked {
                if (block.timestamp > uint256(e.dateDerniereQuarantaine) + FENETRE_QUARANTAINES) {
                    e.quarantainesRecentes = 1; // fenetre glissante de 30 jours
                } else {
                    e.quarantainesRecentes += 1;
                }
            }
            e.dateDerniereQuarantaine = uint64(block.timestamp);
            if (e.quarantainesRecentes >= SEUIL_BANNISSEMENT) e.etat = BANNI;
            emit MiseEnQuarantaine(a, e.etat);
        }
    }

    /// Construction de l'ensemble en MEMOIRE. Rien n'est ecrit ici : c'est ce
    /// qui rend possible le refus integral du §_ecrire.
    ///
    /// Deux passages, et l'ordre compte. Le passage 1 sert d'abord les
    /// producteurs averes : ce sont eux qui font vivre le quorum. Le passage 2
    /// complete avec les non averes tant qu'il reste de la marge. Les deux
    /// predicats etant complementaires, aucun doublon n'est possible entre les
    /// deux passages — inutile de payer une recherche de doublon a chaque tour.
    function _construire(uint256 kMax) internal view returns (address[] memory sel, uint256 t) {
        sel = new address[](kMax);
        sel[0] = VALIDATEUR_GENESE; // place 0, hors election, inconditionnelle
        t = 1;
        // q compte les scelleurs AVERES parmi ceux qu'on place ICI. kMax a ete
        // calcule sur l'ANCIEN ensemble : s'y fier reviendrait a mesurer la
        // vivacite d'un ensemble qu'on est en train de remplacer. Un titulaire
        // qui demande honnetement son retrait sort du vivier APRES ce calcul —
        // l'ensemble grossissait alors dans l'epoque meme ou il perdait un
        // scelleur, et la chaine s'arretait au bloc d'epoch suivant, sans
        // retour possible puisque plus aucune transaction ne peut etre minee.
        uint256 q = _vuProduire(VALIDATEUR_GENESE) ? 1 : 0;

        uint256 nc = nbClasses;
        if (nc > TAILLE_CLASSEMENT) nc = TAILLE_CLASSEMENT; // garde d'indice

        // PASSAGE 1 — les averes. Chaque ajout incremente t ET q : l'invariant
        // t <= 2q-1 se conserve de lui-meme, ce passage n'a aucune garde a porter.
        for (uint256 i = 0; i < nc && t < kMax; ++i) {
            address a = _adresseDuMot(classement[i]);
            if (!_eligible(a) || !_vuProduire(a)) continue;
            sel[t] = a;
            unchecked { ++t; ++q; }
        }
        // PASSAGE 2 — les non averes. Chaque ajout incremente t SEUL : c'est ici,
        // et uniquement ici, que l'invariant peut etre viole.
        for (uint256 i = 0; i < nc && t < kMax; ++i) {
            address a = _adresseDuMot(classement[i]);
            if (!_eligible(a) || _vuProduire(a)) continue;
            // q == 0 en premier : sans ce test, 2 * q - 1 sur un uint256
            // deborde et rend un plafond astronomique — la meme erreur que
            // celle deja evitee par « if (p == 0) p = 1; » plus haut.
            if (q == 0 || t + 1 > 2 * q - 1) break;
            sel[t] = a;
            unchecked { ++t; }
        }
    }

    /// L'eligibilite se teste contre enjeuMinAdmission, JAMAIS contre
    /// enjeuMinimum(). C'est le quatrieme verrou de la gouvernance et celui qui
    /// ferme vraiment la porte : un relevement du minimum ne s'applique qu'aux
    /// nouveaux entrants et ne peut evincer personne. L'attaque « monter le
    /// minimum a 100 M et vider les 41 places » n'a pas de traduction dans ce
    /// code.
    function _eligible(address a) internal view returns (bool) {
        // Le validateur de genese occupe la place 0 hors election ; le laisser
        // passer ici lui donnerait DEUX places, et le controle de doublon de
        // _ecrire refuserait alors tout le recalcul — l'ensemble se figerait.
        if (a == address(0) || a == VALIDATEUR_GENESE) return false;
        Entree storage e = entrees[a];
        uint8 s = e.etat;
        if (s != ACTIF && s != EN_ATTENTE) return false;
        if (e.enjeu < e.enjeuMinAdmission) return false;
        if (s == EN_ATTENTE && block.timestamp < uint256(e.dateCandidature) + DELAI_CANDIDATURE) {
            return false;
        }
        return true;
    }

    /// Passage 3 — remplacement, AU PLUS UN par epoque, et seulement avec DEUX
    /// crans de marge de quorum.
    ///
    /// Remplacer un producteur avere par un candidat qui n'a jamais scelle fait,
    /// au pire, tomber p a p-1 sans changer N. Pour que l'invariant tienne
    /// encore apres, il faut N <= 2(p-1) - 1 = 2p - 3 : deux crans, pas un. A
    /// N=3 avec p=2, la condition 3 <= 1 est fausse : aucun remplacement. La
    /// regle est plus stricte quand la marge est mince et transparente quand
    /// elle est large.
    ///
    /// « Au plus un » n'est pas une economie de gaz : chaque permutation eteint
    /// un noeud et en allume un autre. Une rotation multiple dans la meme epoque
    /// est une facon methodique de perdre le quorum.
    function _remplacer(address[] memory sel, uint256 t, uint256 p) internal view {
        if (p < 2 || t < 2) return;
        unchecked {
            if (t + 3 > 2 * p) return; // exige t <= 2p - 3, sans soustraction
        }

        // Le sortant : le membre de plus faible rang, place 0 exclue.
        uint256 idx = 0;
        uint256 motMin = type(uint256).max;
        for (uint256 i = 1; i < t; ++i) {
            uint256 m = _mot(sel[i], entrees[sel[i]].enjeu);
            if (m < motMin) {
                motMin = m;
                idx = i;
            }
        }
        if (idx == 0) return;

        uint256 nc = nbClasses;
        if (nc > TAILLE_CLASSEMENT) nc = TAILLE_CLASSEMENT;
        uint256 enjeuSortant = entrees[sel[idx]].enjeu;

        for (uint256 i = 0; i < nc; ++i) {
            address a = _adresseDuMot(classement[i]);
            if (!_eligible(a)) continue;
            bool pris = false;
            for (uint256 j = 0; j < t; ++j) {
                if (sel[j] == a) {
                    pris = true;
                    break;
                }
            }
            if (pris) continue;
            if (uint256(entrees[a].enjeu) * SURENCHERE_DEN >= enjeuSortant * SURENCHERE_NUM) {
                sel[idx] = a;
            }
            return; // le classement est trie : le suivant ne fera pas mieux
        }
    }

    /// VALIDATION INTEGRALE PUIS ECRITURE TOUT OU RIEN.
    ///
    /// Un ensemble invalide n'est jamais ecrit a moitie. Un refus laisse le
    /// cache PRECEDENT intact : la chaine continue de produire avec l'ensemble
    /// d'avant. C'est la seule facon d'echouer visiblement sans revert.
    function _ecrire(
        address[] memory sel,
        uint256 t,
        uint256 nAnc,
        uint256 p,
        uint256 kMax
    ) internal {
        if (t == 0 || t > MAX_PLACES || sel[0] != VALIDATEUR_GENESE) {
            emit RecalculRefuse(1);
            return;
        }
        for (uint256 i = 0; i < t; ++i) {
            if (sel[i] == address(0)) {
                emit RecalculRefuse(2);
                return;
            }
            for (uint256 j = 0; j < i; ++j) {
                if (sel[i] == sel[j]) {
                    emit RecalculRefuse(3);
                    return;
                }
            }
        }
        // La longueur des cles de vote n'est pas verifiee ici : elle est
        // structurellement de 48 octets (bytes32 + bytes16). Une verification
        // serait un mensonge rassurant sur une propriete deja garantie par les
        // types.

        if (nAnc == t) {
            bool identique = true;
            for (uint256 i = 0; i < t; ++i) {
                if (elusAdresse[i] != sel[i]) {
                    identique = false;
                    break;
                }
            }
            if (identique) {
                emit RecalculInchange(t);
                return; // economie de gaz : le cas courant
            }
        }

        // Pertes de place. Les fonds ne quittent jamais le contrat moins de 49
        // jours apres la DERNIERE SECONDE ou l'adresse occupait une place. Sans
        // cet invariant applique a TOUTES les voies de sortie, « se faire
        // declasser » deviendrait la sortie rapide : il suffirait de
        // s'organiser un declassement pour echapper a une sanction en cours
        // d'instruction.
        for (uint256 i = 0; i < nAnc; ++i) {
            address a = elusAdresse[i];
            if (a == address(0)) continue;
            bool garde = false;
            for (uint256 j = 0; j < t; ++j) {
                if (sel[j] == a) {
                    garde = true;
                    break;
                }
            }
            if (!garde) _repousserDeblocage(a);
        }

        for (uint256 i = 0; i < t; ++i) {
            address a = sel[i];
            elusAdresse[i] = a;
            if (i == 0) {
                // La cle du validateur de genese vient des CONSTANTES, jamais de
                // son entree : c'est ce qui la garde byte-identique a
                // l'extraData du bloc 0, meme s'il a depose une caution.
                elusVoteA[0] = VOTE_GENESE_A;
                elusVoteB[0] = VOTE_GENESE_B;
            } else {
                Entree storage e = entrees[a];
                elusVoteA[i] = e.voteA;
                elusVoteB[i] = e.voteB;
                if (e.etat == EN_ATTENTE) e.etat = ACTIF;
            }
        }
        nbElus = t;
        emit JeuRecalcule(t, p, kMax);
    }

    // =====================================================================
    // 7. LE CLASSEMENT — insertion et retrait bornes
    // =====================================================================

    function _mot(address a, uint256 montant) internal pure returns (uint256) {
        unchecked {
            return (montant << 160) | uint256(type(uint160).max - uint160(a));
        }
    }

    function _adresseDuMot(uint256 m) internal pure returns (address) {
        unchecked {
            return address(type(uint160).max - uint160(m));
        }
    }

    /// Insertion par decalage dans un tableau trie de 82 mots au maximum : au
    /// pire 82 lectures et 82 ecritures, payees par le deposant. C'est ce cout
    /// BORNE qui interdit l'attaque par famine.
    function _inserer(address a, uint256 montant) internal {
        uint256 mot = _mot(a, montant);
        uint256 n = nbClasses;
        if (n > TAILLE_CLASSEMENT) n = TAILLE_CLASSEMENT;

        if (n < TAILLE_CLASSEMENT) {
            uint256 k = n;
            while (k > 0 && classement[k - 1] < mot) {
                classement[k] = classement[k - 1];
                unchecked { --k; }
            }
            classement[k] = mot;
            unchecked { nbClasses = n + 1; }
            return;
        }

        // Classement plein. Un candidat qui ne bat pas le 82e voit son depot
        // REFUSE, pas conserve : accepter l'argent et le garder inerte serait la
        // defaillance silencieuse que la regle du depot interdit.
        uint256 dernier = classement[TAILLE_CLASSEMENT - 1];
        if (mot <= dernier) revert ClassementSature();

        address sortant = _adresseDuMot(dernier);
        uint256 j = TAILLE_CLASSEMENT - 1;
        while (j > 0 && classement[j - 1] < mot) {
            classement[j] = classement[j - 1];
            unchecked { --j; }
        }
        classement[j] = mot;

        // Le declasse conserve ses fonds mais passe en deblocage : aucune sortie
        // ne raccourcit jamais les 49 jours, meme celle qu'on n'a pas choisie.
        Entree storage s = entrees[sortant];
        if (s.etat == EN_ATTENTE || s.etat == ACTIF || s.etat == EN_QUARANTAINE) {
            s.etat = EN_DEBLOCAGE;
            _repousserDeblocage(sortant);
            emit Declasse(sortant);
        }
    }

    function _retirerDuClassement(address a) internal {
        uint256 n = nbClasses;
        if (n > TAILLE_CLASSEMENT) n = TAILLE_CLASSEMENT;
        for (uint256 i = 0; i < n; ++i) {
            if (_adresseDuMot(classement[i]) == a) {
                for (uint256 j = i; j + 1 < n; ++j) {
                    classement[j] = classement[j + 1];
                }
                classement[n - 1] = 0;
                unchecked { nbClasses = n - 1; }
                return;
            }
        }
    }

    /// Le deblocage est MONOTONE CROISSANT : il ne recule jamais. Applique a
    /// toutes les voies de sortie — demande volontaire, declassement par
    /// surenchere, quarantaine, perte de place au recalcul, bannissement.
    function _repousserDeblocage(address a) internal {
        Entree storage e = entrees[a];
        uint256 cible = block.timestamp + DELAI_DEBLOCAGE;
        if (uint256(e.dateDeblocage) < cible) e.dateDeblocage = uint64(cible);
        if (uint256(e.blocReferenceDeblocage) < block.number) {
            e.blocReferenceDeblocage = uint64(block.number);
        }
    }

    // =====================================================================
    // 8. LE DEPOT — hors consensus, DOIT revert
    // =====================================================================

    /// Un depot inferieur au minimum est REFUSE, pas conserve. Une cle de vote
    /// de mauvaise longueur est REFUSEE. Un second depot est REFUSE (passer par
    /// ajouterEnjeu). Sur ce chemin, un echec silencieux qui garde l'argent de
    /// quelqu'un serait bien pire qu'un revert.
    function deposer(bytes calldata cleVote) external payable {
        // Le validateur de genese occupe la place 0 hors election. Le laisser
        // candidater lui donnerait DEUX places sur 41. Sa caution passe par
        // deposerCaution(), qui est saisissable mais ne donne aucune place.
        if (msg.sender == VALIDATEUR_GENESE) revert GeneseNonCandidate();

        Entree storage e = entrees[msg.sender];
        if (e.etat != INEXISTANT) revert DejaInscrit();

        // LA garde qui ferme le trou de la cle BLS. Le decodeur Go accepte
        // silencieusement 0, 47 ou 49 octets et remplit un [48]byte par copie
        // tronquee ou completee de zeros : la longueur doit etre imposee a
        // l'ECRITURE, jamais esperee a la lecture.
        if (cleVote.length != LONGUEUR_ADRESSE_VOTE) revert CleVoteInvalide();

        bytes32 cleA;
        bytes16 cleB;
        assembly {
            cleA := calldataload(cleVote.offset)
            // Masque : ne garder que les 16 octets de POIDS FORT du second mot.
            // Sans lui, les octets 48..63 de la calldata (bourrage ABI ou champ
            // suivant) resteraient dans la variable et seraient ecrits dans
            // l'emplacement partage : la cle stockee ne serait plus celle que
            // l'appelant a fournie.
            cleB := and(
                calldataload(add(cleVote.offset, 32)),
                0xffffffffffffffffffffffffffffffff00000000000000000000000000000000
            )
        }
        // La cle nulle est celle du validateur de genese. L'autoriser ailleurs
        // creerait un doublon de cle BLS avec lui.
        if (cleA == bytes32(0) && cleB == bytes16(0)) revert CleVoteNulle();

        bytes32 h = keccak256(cleVote);
        if (proprietaireCleVote[h] != address(0)) revert CleVoteDejaPrise();

        uint256 min = enjeuMinimum();
        if (msg.value < min) revert EnjeuInsuffisant();
        // uint96 va jusqu'a 7,9e28 wei, soit 113 fois l'offre entiere (7e26) :
        // le refus n'est atteignable qu'avec une monnaie qui n'existe pas.
        if (msg.value > type(uint96).max) revert EnjeuTropGrand();

        _inserer(msg.sender, msg.value); // peut revert ClassementSature

        e.enjeu = uint96(msg.value);
        e.enjeuMinAdmission = uint96(min); // non-retroactivite : voir _eligible
        e.dateCandidature = uint64(block.timestamp);
        e.etat = EN_ATTENTE;
        e.blocEngagement = uint64(block.number);
        e.voteA = cleA;
        e.voteB = cleB;
        proprietaireCleVote[h] = msg.sender;
        emit Depot(msg.sender, msg.value, cleVote);
    }

    /// Interdit en EN_DEBLOCAGE et BANNI : augmenter son enjeu pendant une purge
    /// rouvrirait une place en cours de fermeture et rembobinerait le compteur
    /// des 49 jours. Ne remet pas dateCandidature a zero : l'anciennete est
    /// acquise.
    function ajouterEnjeu() external payable {
        if (msg.sender == VALIDATEUR_GENESE) revert GeneseNonCandidate();
        Entree storage e = entrees[msg.sender];
        uint8 s = e.etat;
        if (s != EN_ATTENTE && s != ACTIF && s != EN_QUARANTAINE) revert EtatIncompatible();
        if (msg.value == 0) revert MontantNul();
        uint256 nv = uint256(e.enjeu) + msg.value;
        if (nv > type(uint96).max) revert EnjeuTropGrand();

        // Retirer PUIS reinserer : le classement doit rester trie, et le retrait
        // prealable garantit qu'il reste une place libre pour la reinsertion.
        _retirerDuClassement(msg.sender);
        e.enjeu = uint96(nv);
        _inserer(msg.sender, nv);
        emit EnjeuAugmente(msg.sender, msg.value, nv);
    }

    /// La caution du validateur de genese. Sa place est acquise a vie par le
    /// contrat fige, independamment de tout : sa caution est donc du collateral
    /// PUR — saisissable par les sanctions comme celle de n'importe qui, sans
    /// aucun effet sur sa place. Elle n'entre pas au classement, sans quoi il
    /// occuperait une seconde place.
    ///
    /// A PUBLIER TEL QUEL : le validateur de genese ne peut pas etre exclu, meme
    /// pour double signature. Seul son argent est expose.
    function deposerCaution() external payable {
        if (msg.sender != VALIDATEUR_GENESE) revert ReserveeAuValidateurDeGenese();
        if (msg.value == 0) revert MontantNul();
        Entree storage e = entrees[msg.sender];
        uint256 nv = uint256(e.enjeu) + msg.value;
        if (nv > type(uint96).max) revert EnjeuTropGrand();
        e.enjeu = uint96(nv);
        // JAMAIS repousse : une caution reversee apres une sanction ne doit pas
        // effacer l'exposition acquise au premier depot.
        if (e.blocEngagement == 0) e.blocEngagement = uint64(block.number);
        // ACTIF le rend sanctionnable par slash() comme les autres. C'est la
        // seule facon de dire « son argent est engage » sans mentir sur sa place.
        if (e.etat == INEXISTANT || e.etat == EN_DEBLOCAGE) e.etat = ACTIF;
        emit CautionDeposee(msg.sender, msg.value, nv);
    }

    // =====================================================================
    // 9. LE RETRAIT
    // =====================================================================

    /// LA PLACE EST LIBEREE A LA DEMANDE, LES FONDS 49 JOURS PLUS TARD.
    ///
    /// La periode de deblocage n'a aucune raison de garder la PLACE occupee.
    /// Garder la place 49 jours creerait exactement la configuration mortelle :
    /// un validateur eteint son noeud, demande son retrait, et occupe encore une
    /// des 41 places pendant sept semaines. Avec 41 places dont 5 mortes, le
    /// quorum ⌊41/2⌋+1 = 21 se calcule toujours sur 41. Reduire l'ensemble
    /// n'abaisse jamais la disponibilite ; le laisser peuple de fantomes, si.
    ///
    /// Le sortant doit garder son noeud allume jusqu'au recalcul suivant (au
    /// plus 200 blocs). C'est une consigne d'exploitation, pas une garantie du
    /// contrat.
    ///
    /// Retrait PARTIEL : interdit. Descendre sous le minimum, c'est sortir.
    function demanderRetrait() external {
        Entree storage e = entrees[msg.sender];
        uint8 s = e.etat;
        if (s != EN_ATTENTE && s != ACTIF && s != EN_QUARANTAINE) revert EtatIncompatible();
        if (msg.sender != VALIDATEUR_GENESE) _retirerDuClassement(msg.sender);
        e.etat = EN_DEBLOCAGE;
        _repousserDeblocage(msg.sender);
        emit RetraitDemande(msg.sender, e.dateDeblocage);
    }

    /// DOUBLE CONDITION : temps ET hauteur.
    ///
    /// L'horodatage seul est ecrit par les validateurs. Une majorite qui
    /// s'entendrait pourrait avancer l'horloge et raccourcir sa propre purge. La
    /// condition de hauteur, calee a 80 % de 49 jours a 5 s/bloc, rend la
    /// manoeuvre inoperante : il faut vraiment produire 677 376 blocs. Et
    /// l'echec est visible — le retrait n'est simplement pas disponible.
    function retirer() external {
        Entree storage e = entrees[msg.sender];
        uint8 s = e.etat;
        if (s != EN_DEBLOCAGE && s != BANNI) revert EtatIncompatible();
        if (block.timestamp < uint256(e.dateDeblocage)) revert DeblocageTemps();
        if (block.number < uint256(e.blocReferenceDeblocage) + BLOCS_DEBLOCAGE_MIN) {
            revert DeblocageHauteur();
        }

        uint256 montant = e.enjeu;
        e.enjeu = 0;
        _retirerDuClassement(msg.sender);

        if (s == BANNI) {
            // Les fonds sortent, l'adresse NON. Remettre l'entree a INEXISTANT
            // rouvrirait la porte a un nouveau deposer() depuis la meme adresse
            // de consensus, et la cle de vote redeviendrait disponible.
            e.dernierBlocScelle = 0;
        } else {
            delete proprietaireCleVote[keccak256(abi.encodePacked(e.voteA, e.voteB))];
            delete entrees[msg.sender]; // un rentrant repart de zero
        }

        if (montant > 0) {
            (bool ok, ) = payable(msg.sender).call{value: montant}("");
            if (!ok) revert TransfertEchoue();
        }
        emit Retire(msg.sender, montant);
    }

    /// Retour en EN_ATTENTE, donc repassage par les 7 jours d'admission : c'est
    /// la peine. Un retour direct en ACTIF ferait de la quarantaine une pause.
    function sortirDeQuarantaine() external {
        Entree storage e = entrees[msg.sender];
        if (e.etat != EN_QUARANTAINE) revert EtatIncompatible();
        if (block.timestamp < uint256(e.dateDerniereQuarantaine) + DUREE_QUARANTAINE) {
            revert QuarantaineEnCours();
        }
        e.etat = EN_ATTENTE;
        e.dateCandidature = uint64(block.timestamp);
        e.absencesConsecutives = 0;
        emit SortieDeQuarantaine(msg.sender);
    }

    /// Ouvert a tous : sort du classement une entree BANNIE qui y occuperait
    /// encore un rang. Sans cette porte, un acteur pourrait se faire bannir
    /// volontairement pour boucher un rang du classement et gener les candidats
    /// suivants.
    function nettoyerClassement(address a) external {
        if (entrees[a].etat != BANNI) revert EtatIncompatible();
        _retirerDuClassement(a);
    }

    // =====================================================================
    // 10. LA SANCTION LOURDE — double signature, hors consensus
    // =====================================================================

    // --- L'ENCODEUR RLP DE LA PREUVE -------------------------------------
    //
    // Ces quatre fonctions n'existent que pour que le contrat construise
    // LUI-MEME l'enveloppe remise au precompile 0x68, au lieu de relayer un
    // blob fourni par l'appelant. La raison est detaillee juste en dessous.
    // Elles produisent de la RLP CANONIQUE, la seule que geth accepte :
    // longueurs minimales, aucun zero de tete, et raccourci « octet nu »
    // reserve aux chaines de LONGUEUR 1 valant moins de 0x80.

    /// Nombre d'octets significatifs de x (0 pour x == 0).
    function _longueurOctets(uint256 x) internal pure returns (uint256 n) {
        while (x != 0) {
            unchecked { ++n; }
            x >>= 8;
        }
    }

    /// Les n octets de poids faible de x, en gros-boutien.
    function _octetsDe(uint256 x, uint256 n) internal pure returns (bytes memory b) {
        b = new bytes(n);
        for (uint256 i = 0; i < n; ) {
            b[i] = bytes1(uint8(x >> (8 * (n - 1 - i))));
            unchecked { ++i; }
        }
    }

    /// Prefixe de longueur RLP. base = 0x80 pour une chaine, 0xc0 pour une liste.
    function _prefixeRlp(uint256 len, uint256 base) internal pure returns (bytes memory) {
        if (len <= 55) return abi.encodePacked(uint8(base + len));
        uint256 n = _longueurOctets(len);
        // La RLP ne definit pas de longueur-de-longueur au-dela de 8 octets
        // (0xb7 + 8 = 0xbf, 0xf7 + 8 = 0xff). Inatteignable ici — il faudrait
        // 2^64 octets de calldata — mais la borne rend l'encodeur TOTAL : au
        // dela, le prefixe deborderait sur la plage voisine au lieu de refuser.
        if (n > 8) revert PreuveInvalide();
        return abi.encodePacked(uint8(base + 55 + n), _octetsDe(len, n));
    }

    /// RLP d'un entier. Go encode le ChainId comme un *big.Int, donc comme une
    /// chaine d'octets minimale. ZERO -> chaine VIDE (0x80), JAMAIS 0x00 : geth
    /// rejette 0x00 (« rlp: non-canonical integer »).
    function _rlpEntier(uint256 x) internal pure returns (bytes memory) {
        if (x == 0) return hex"80";
        // LE PIEGE, ET COINBOSA EST LE CAS QUI LE REVELE. Le raccourci « octet
        // nu » vaut pour une chaine de LONGUEUR 1 dont l'octet est < 0x80 — pas
        // pour un PREMIER OCTET < 0x80. Notre chainId 26262 vaut 0x6696 : son
        // premier octet 0x66 est bien inferieur a 0x80, et pourtant l'encodage
        // correct est « 82 66 96 ». Emettre « 66 96 » ferait mal parser la liste
        // entiere. C'est le test x <= 0x7f, et non un test sur le premier octet,
        // qui separe les deux cas. Le chainId 56 de BSC, lui, tient sur un octet
        // nu : la branche multi-octets n'est jamais exercee chez eux, donc
        // recopier leur code sans la comprendre ne suffisait pas.
        if (x <= 0x7f) return abi.encodePacked(uint8(x));
        uint256 n = _longueurOctets(x);
        return abi.encodePacked(uint8(0x80 + n), _octetsDe(x, n));
    }

    /// RLP d'une chaine d'octets.
    function _rlpOctets(bytes calldata s) internal pure returns (bytes memory) {
        if (s.length == 1 && uint8(s[0]) <= 0x7f) return s;
        return abi.encodePacked(_prefixeRlp(s.length, 0x80), s);
    }

    /// L'ACCIDENT QUE CETTE FORME EVITE : LE PRECOMPILE CROIT LA PREUVE SUR
    /// PAROLE QUANT A LA CHAINE OU LA FAUTE A ETE COMMISE.
    ///
    /// Le precompile 0x68 attend le RLP d'un DoubleSignEvidence
    /// {ChainId, HeaderBytes1, HeaderBytes2}. Pour retrouver la cle qui a scelle
    /// chaque entete, il calcule (core/vm/contracts.go) :
    ///
    ///     msgHash1 := types.SealHash(header1, evidence.ChainId)
    ///
    /// Il utilise donc le ChainId ECRIT DANS LA PREUVE. Il ne le compare jamais
    /// a celui de la chaine qui l'execute, et aucun autre endroit du client Go
    /// ne fait cette comparaison a sa place. Or le ChainId est le PREMIER champ
    /// scelle (core/types/block.go, EncodeSigHeader) : il fait partie du message
    /// signe, si bien qu'une signature ne vaut QUE pour une chaine — mais c'est
    /// la preuve elle-meme qui declare laquelle.
    ///
    /// CE QUI ARRIVERAIT SANS CETTE GARDE. Si la fonction acceptait un blob RLP
    /// deja assemble, c'est l'APPELANT qui choisirait ce ChainId. Il lui
    /// suffirait alors d'une equivocation parfaitement AUTHENTIQUE commise par
    /// le validateur vise sur n'importe quelle AUTRE chaine Parlia — un reseau
    /// d'essai, ou une chaine montee pour l'occasion — des lors que ce
    /// validateur y reutilise sa cle de scellage. Le precompile certifierait la
    /// preuve sans broncher, et cette fonction confisquerait l'INTEGRALITE de
    /// l'enjeu d'un validateur qui n'a jamais rien fait de mal sur Coinbosa. La
    /// reutilisation d'une meme cle entre un banc d'essai et la production est
    /// un piege classique, et PoBS accueille jusqu'a 41 validateurs, chacun
    /// avec ses habitudes. Les deux bornes existantes n'y suffisent pas :
    /// « hauteur > block.number » n'ecarte qu'une chaine PLUS HAUTE que la
    /// notre, et FENETRE_PREUVE_BLOCS laisse 21 jours — un reseau jeune passe
    /// les deux.
    ///
    /// D'OU CETTE FORME ETRANGE. La fonction ne recoit PAS de preuve toute
    /// faite. Elle recoit les deux entetes bruts et REASSEMBLE elle-meme
    /// rlp([block.chainid, enTete1, enTete2]). Le ChainId cesse d'etre une
    /// donnee de l'appelant pour devenir une donnee de la chaine : il n'y a plus
    /// rien a falsifier. C'est exactement pour cette raison que le
    /// SlashIndicator de bnb-chain/bsc construit lui aussi son RLP au lieu
    /// d'accepter un blob. Et le sens de la panne est le bon : un defaut de
    /// notre encodeur ne peut produire qu'un FAUX NEGATIF — le precompile
    /// refuse un RLP malforme, la sanction echoue bruyamment — jamais un faux
    /// positif.
    ///
    /// block.chainid plutot qu'une constante gravee : c'est litteralement la
    /// propriete recherchee, « la chaine qui EXECUTE » ; et le produit est en
    /// marque blanche, donc un partenaire qui redeploie sur son propre chainId
    /// obtient un contrat juste sans avoir a reecrire cette ligne.
    ///
    /// LE CONTROLE ret.length != 52 N'EST PAS DECORATIF, IL EST INDISPENSABLE.
    /// Le precompile 0x68 ne figure PAS dans PrecompiledContractsHertz
    /// (core/vm/contracts.go), et Feynman est inactif sur Coinbosa : 0x68 n'a
    /// donc AUCUN CODE aujourd'hui. Un staticcall vers un compte sans code rend
    /// ok = true et zero octet. Avec le seul `if (!ok) revert`, n'importe
    /// quelles donnees passeraient pour une preuve valide et permettraient de
    /// confisquer l'enjeu d'un validateur honnete. La bifurcation doit activer
    /// 0x68 a pobsTime ; ce controle de longueur reste le filet si l'ordre de
    /// deploiement etait inverse. Noter que le SlashIndicator de BSC, lui, ne
    /// verifie PAS cette longueur : sur ce point precis il ne faut pas le
    /// copier, parce que chez eux Feynman est actif depuis longtemps.
    ///
    /// Le format de retour est fixe par le precompile : 20 octets d'adresse de
    /// signataire, puis 32 octets de hauteur, soit 52 exactement.
    ///
    /// CE QUE CETTE FONCTION NE PEUT PAS FAIRE, ET QU'IL FAUT SAVOIR.
    /// Reconstruire le RLP ne protege de rien contre une chaine qui PARTAGE
    /// notre chainId : une equivocation authentique commise la-bas est, par
    /// construction, une equivocation valide ici, et aucun contrat ne peut les
    /// distinguer. Le precompile ne verifie ni que ParentHash appartient a notre
    /// histoire, ni que la hauteur correspond a un bloc reel : la regle
    /// reellement appliquee est « il a signe deux entetes en conflit POUR NOTRE
    /// CHAINID », pas « il a double-signe sur la chaine canonique ». La parade
    /// est hors contrat, et elle est double : donner un chainId DISTINCT a tout
    /// genesis autre que la production (genesis-coinbosa-dev.json porte
    /// aujourd'hui le meme 26262 que la production), et interdire par regle
    /// d'exploitation qu'une cle de scellage de production soit chargee sur un
    /// noeud qui n'est pas la production. Une cle volee reste, elle aussi,
    /// indiscernable — comme sur BSC.
    ///
    /// Ouverte a tous, sans condition de gaz : la preuve est cryptographique,
    /// elle n'a pas besoin d'etre autorisee. En revanche un refus du precompile
    /// n'est pas un revert ordinaire : errInvalidEvidence remonte comme une
    /// erreur, donc le staticcall CONSOMME tout le gaz transmis. Le denonciateur
    /// doit prevoir large.
    /// Assemble rlp([block.chainid, enTete1, enTete2]) : l'enveloppe EXACTE
    /// remise au precompile. Le chainId n'est PAS un parametre de cette
    /// fonction et NE DOIT JAMAIS LE DEVENIR — c'est toute la propriete de
    /// surete de cette section. Isolee ici pour etre confrontable octet par
    /// octet au rlp.EncodeToBytes de Go par un harnais de test.
    function _enveloppePreuve(bytes calldata enTete1, bytes calldata enTete2)
        internal
        view
        returns (bytes memory)
    {
        bytes memory idc = _rlpEntier(block.chainid);
        bytes memory e1 = _rlpOctets(enTete1);
        bytes memory e2 = _rlpOctets(enTete2);
        return bytes.concat(
            _prefixeRlp(idc.length + e1.length + e2.length, 0xc0), idc, e1, e2
        );
    }

    function signalerDoubleSignature(bytes calldata enTete1, bytes calldata enTete2) external {
        // L'enveloppe est CONSTRUITE ICI, jamais recue : rlp([chainId, h1, h2]).
        bytes memory enveloppe = _enveloppePreuve(enTete1, enTete2);

        (bool ok, bytes memory ret) = address(0x68).staticcall(enveloppe);
        if (!ok || ret.length != 52) revert PreuveInvalide();

        address fautif;
        uint256 hauteur;
        assembly {
            let q := add(ret, 32)
            fautif := shr(96, mload(q))
            hauteur := mload(add(q, 20))
        }

        if (hauteur > block.number || block.number - hauteur > FENETRE_PREUVE_BLOCS) {
            revert PreuveHorsFenetre();
        }

        // LA CLE ANTI-REJEU PORTE SUR LA FAUTE, PAS SUR SES OCTETS.
        //
        // L'ancienne cle, keccak256(preuve), etait contournable. Le hash de
        // scellement ne couvre pas tout l'entete : EncodeSigHeader n'ajoute
        // BaseFee, WithdrawalsHash, BlobGasUsed, ExcessBlobGas et RequestsHash
        // QUE si ParentBeaconRoot != nil — et Coinbosa n'a pas de cancunTime,
        // donc ParentBeaconRoot est TOUJOURS nil. Modifier BaseFee, ou le
        // retirer purement (les champs sont `rlp:"optional"` et le decodeur Go
        // met a zero ceux qui manquent), changeait donc les octets, donc leur
        // keccak, sans changer le SealHash, ni la signature, ni la cle
        // recuperee. Intervertir les deux entetes, ou remplacer (r, s, v) par
        // (r, n-s, v^1), avaient le meme effet. La famille d'encodages d'une
        // MEME faute est infinie : l'ancienne garde ne gardait rien.
        //
        // (fautif, hauteur) est exactement ce que le precompile CERTIFIE, et la
        // seule chose que l'attaquant ne peut pas faire varier : la hauteur est
        // scellee, et le precompile impose deja que les deux entetes portent la
        // meme. Deux equivocations distinctes a la meme hauteur comptent donc
        // pour une seule — sans consequence, la confiscation est deja totale.
        //
        // Ce n'est pas une redondance avec le bannissement. BSC se passe d'une
        // telle table parce que sa sanction est terminale pour tous ; ici le
        // VALIDATEUR_GENESE reste ACTIF apres sanction (sa place est figee au
        // bloc 0, voir plus bas), donc sans cette table une seule faute prouvee
        // resterait reencaissable a chaque deposerCaution() ulterieur.
        bytes32 k = keccak256(abi.encode(fautif, hauteur));
        if (infractionSanctionnee[k]) revert InfractionDejaSanctionnee();

        Entree storage e = entrees[fautif];

        // TOUT ETAT SAUF INEXISTANT. L'ancienne liste — ACTIF, EN_QUARANTAINE,
        // EN_DEBLOCAGE — laissait deux echappatoires. EN_ATTENTE d'abord :
        // deposer() y place tout entrant, et sortirDeQuarantaine() y ramene.
        // BANNI ensuite, et c'est le plus grave, parce que retirer() rend
        // l'INTEGRALITE de l'enjeu a un banni (§9). Un validateur qui venait
        // d'equivoquer pouvait donc se faire mettre en quarantaine trois fois —
        // BLOCS_SILENCE, puis DUREE_QUARANTAINE, puis les 7 jours de
        // DELAI_CANDIDATURE que sortirDeQuarantaine() reenclenche, soit environ
        // huit jours par cycle — atteindre BANNI en moins de 21 jours, devenir
        // insaisissable AVANT l'expiration de FENETRE_PREUVE_BLOCS, puis
        // repartir avec tout son enjeu au 49e jour. La sanction porte sur
        // l'ARGENT ENGAGE, pas sur le siege : les sieges, c'est l'affaire de la
        // machine a etats.
        if (e.etat == INEXISTANT) revert EtatIncompatible();

        // UN ENJEU NE REPOND QUE DE CE QUI SUIT SA MISE EN JEU. Sans cette
        // ligne, une equivocation ANTERIEURE au depot — commise dans une vie
        // anterieure de la meme cle, ou sur un banc d'essai qui partagerait
        // notre chainId — permettrait de saisir un enjeu qui ne la couvrait pas.
        // Elle ne ferme PAS le cas d'un attaquant qui controle une replique de
        // notre chaine et choisit donc ses hauteurs (voir la note de limites
        // ci-dessus) ; elle ferme le cas de la faute preexistante.
        if (hauteur < uint256(e.blocEngagement)) revert PreuveAnterieureAuDepot();

        infractionSanctionnee[k] = true;
        uint256 saisi = e.enjeu;
        e.enjeu = 0;

        uint256 prime;
        // CONSEIL, CONSEIL_AMORCAGE et VALIDATEUR_GENESE sont exclus de la
        // prime : leur part tombe au puits. Sans cela, la sanction deviendrait
        // un revenu pour ceux qui ont deja du pouvoir, donc une incitation a
        // sanctionner.
        if (
            msg.sender != CONSEIL &&
            msg.sender != CONSEIL_AMORCAGE &&
            msg.sender != VALIDATEUR_GENESE
        ) {
            prime = (saisi * PRIME_POUR_CENT) / 100;
            if (prime > PRIME_PLAFOND) prime = PRIME_PLAFOND;
        }

        unchecked {
            aBruler += saisi - prime;
            if (prime > 0) {
                primeDe[msg.sender] += prime;
                primesDues += prime;
                uint256 c = block.timestamp + DELAI_DEBLOCAGE;
                if (uint256(primeDisponibleLe[msg.sender]) < c) {
                    primeDisponibleLe[msg.sender] = uint64(c);
                }
            }
        }

        if (fautif == VALIDATEUR_GENESE) {
            // Sa place demeure. Le contrat fige l'impose et le contrat neuf ne
            // peut pas le contredire sans risquer l'arret : il est aujourd'hui
            // le seul dont une cle de scellage est detenue par un noeud.
            emit DoubleSignatureGenese(fautif, saisi, hauteur);
        } else {
            // L'equivocation est la seule faute qui attaque la chaine elle-meme.
            // Elle n'a pas de version accidentelle : confiscation integrale,
            // eviction, bannissement definitif de l'adresse ET de sa cle de vote.
            e.etat = BANNI;
            _retirerDuClassement(fautif);
            _repousserDeblocage(fautif);
            emit DoubleSignature(fautif, saisi, hauteur, msg.sender);
        }
    }

    /// En TIRAGE, jamais en poussee depuis le chemin de sanction : un transfert
    /// pousse est un appel externe, donc un revert possible la ou il est
    /// interdit. Et la prime est verrouillee 49 jours comme le reste — c'est ce
    /// qui empeche l'auto-denonciation de servir de guichet de sortie rapide.
    function reclamerPrime() external {
        uint256 m = primeDe[msg.sender];
        if (m == 0) revert RienAReclamer();
        if (block.timestamp < uint256(primeDisponibleLe[msg.sender])) revert DeblocageTemps();
        primeDe[msg.sender] = 0;
        unchecked { primesDues -= m; }
        (bool ok, ) = payable(msg.sender).call{value: m}("");
        if (!ok) revert TransfertEchoue();
        emit PrimeReclamee(msg.sender, m);
    }

    /// Ouverte a tous. Redistribuer aux validateurs honnetes ferait de la
    /// sanction un revenu, donc une incitation a sanctionner. Ni le CONSEIL, ni
    /// le gouverneur du contrat fige, ni l'editeur ne peuvent toucher un wei
    /// confisque : aucune fonction ne le permet, et il n'y a pas de proxy.
    function purger() external {
        uint256 m = aBruler;
        if (m == 0) revert RienAReclamer();
        aBruler = 0;
        unchecked { totalPurge += m; }
        (bool ok, ) = payable(PUITS).call{value: m}("");
        if (!ok) revert TransfertEchoue();
        emit Purge(m);
    }

    // =====================================================================
    // 11. LA GOUVERNANCE DU MINIMUM — quatre verrous
    // =====================================================================

    /// L'etat VIDE est l'etat initial valide : sans cela, un deploiement par
    /// SetCode (qui n'execute aucun constructeur) laisserait le minimum a zero
    /// et les 41 places deviendraient gratuites.
    function enjeuMinimum() public view returns (uint256) {
        uint256 v = _enjeuMinimum;
        return v == 0 ? ENJEU_MIN_PLANCHER : v;
    }

    /// VERROU 1 — bornes absolues gravees [1 000 ; 3 500 000] BOSA.
    /// VERROU 2 — vitesse bornee : ni x2 ni /2 d'un coup, et 14 jours entre deux
    ///            changements. Aller du plancher au plafond demande 12 paliers,
    ///            soit au moins 168 jours : une derive lente et publiquement
    ///            observable, jamais un geste.
    /// VERROU 3 — delai d'execution de 14 jours, sous veto (voir opposerVeto).
    /// VERROU 4 — non-retroactivite, dans _eligible : c'est celui qui ferme
    ///            vraiment la porte.
    function proposerEnjeuMinimum(uint256 nouveau) external {
        if (msg.sender != CONSEIL) revert NonAutorise();
        if (nouveau < ENJEU_MIN_PLANCHER || nouveau > ENJEU_MIN_PLAFOND) revert HorsBornes();
        if (dateEffetProposition != 0) revert PropositionEnCours();
        uint256 courant = enjeuMinimum();
        if (nouveau == courant) revert HorsBornes();
        if (nouveau > courant * 2 || nouveau * 2 < courant) revert HorsBornes();
        if (block.timestamp < uint256(dateDernierChangement) + DELAI_GOUVERNANCE) revert TropRapide();

        _enjeuMinPropose = uint96(nouveau);
        dateEffetProposition = uint64(block.timestamp + DELAI_GOUVERNANCE);
        unchecked { cycleProposition += 1; }
        _vetoCompte = 0;
        emit MinimumPropose(nouveau, dateEffetProposition, cycleProposition);
    }

    /// Les validateurs ne peuvent pas AGIR sur le parametre, seulement REFUSER.
    /// Le pouvoir de proposer n'emporte pas le pouvoir d'imposer.
    function opposerVeto() external {
        if (dateEffetProposition == 0) revert AucuneProposition();
        if (!isCurrentValidator(msg.sender)) revert NonAutorise();
        uint256 c = cycleProposition;
        if (_vetoDonne[c][msg.sender]) revert NonAutorise();
        _vetoDonne[c][msg.sender] = true;
        unchecked { _vetoCompte += 1; }
        if (_vetoCompte * 2 > nbElus) {
            dateEffetProposition = 0;
            _enjeuMinPropose = 0;
            emit MinimumRejete(c, _vetoCompte);
        }
    }

    /// Ouverte a tous : le CONSEIL propose, n'importe qui execute apres le
    /// delai. Une execution reservee au proposant lui donnerait le pouvoir de
    /// garder une proposition en suspens indefiniment.
    function appliquerEnjeuMinimum() external {
        uint64 d = dateEffetProposition;
        if (d == 0) revert AucuneProposition();
        if (block.timestamp < uint256(d)) revert TropRapide();
        uint96 v = _enjeuMinPropose;
        _enjeuMinimum = v;
        dateDernierChangement = uint64(block.timestamp);
        dateEffetProposition = 0;
        _enjeuMinPropose = 0;
        emit MinimumApplique(v);
    }

    // =====================================================================
    // 12. L'AMORCAGE — borne, public, auto-dissolvant
    // =====================================================================

    function attesterProduction(address v) external {
        if (msg.sender != CONSEIL_AMORCAGE) revert NonAutorise();
        if (amorcageTermine) revert AmorcageClos();
        // Au plus UN validateur non prouve expose a la fois. C'est irreductible,
        // et c'est precisement le pas 1 -> 2 que la procedure hors chaine
        // encadre : rejeu sur chaine jetable, entrant VU sceller.
        if (attesteEnCours != address(0)) revert PropositionEnCours();
        if (attestationsUtilisees >= MAX_ATTESTATIONS_AMORCAGE) revert AmorcageClos();
        if (v == VALIDATEUR_GENESE || !_eligible(v)) revert EtatIncompatible();

        attesteEnCours = v;
        attesteDepuisBloc = uint64(block.number);
        unchecked { attestationsUtilisees += 1; }
        emit ProductionAttestee(v);
    }

    // =====================================================================
    // 13. LECTURES D'EXPLOITATION (hors consensus)
    // =====================================================================

    function infosValidateur(address a)
        external
        view
        returns (
            uint256 enjeu,
            uint8 etat,
            uint64 dernierBlocScelle,
            uint64 dateDeblocage,
            uint256 enjeuMinAdmission,
            uint32 absences,
            bytes memory cleVote
        )
    {
        Entree storage e = entrees[a];
        return (
            e.enjeu,
            e.etat,
            e.dernierBlocScelle,
            e.dateDeblocage,
            e.enjeuMinAdmission,
            e.absencesConsecutives,
            abi.encodePacked(e.voteA, e.voteB)
        );
    }

    /// p — le nombre de membres de l'ensemble courant reellement vus sceller
    /// pendant l'epoque en cours. La grandeur qui commande tout : la taille
    /// maximale de l'ensemble au prochain recalcul vaut min(41, 2p-1, N+1).
    /// A publier dans la supervision : c'est elle, et non « la hauteur avance »,
    /// qui dit si la chaine a de la marge.
    function producteursVus() external view returns (uint256 p) {
        uint256 n = nbElus;
        if (n > MAX_PLACES) n = MAX_PLACES;
        for (uint256 i = 0; i < n; ++i) {
            if (_vuProduire(elusAdresse[i])) {
                unchecked { ++p; }
            }
        }
    }

    function motDuClassement(uint256 i) external view returns (uint256 mot, address candidat, uint256 enjeu) {
        mot = classement[i];
        candidat = _adresseDuMot(mot);
        enjeu = mot >> 160;
    }

    /// Pas de receive() ni de fallback(). Un virement simple vers ce contrat
    /// REVERT, et c'est voulu : des fonds arrives hors deposer() ne seraient
    /// comptabilises nulle part et resteraient bloques a vie, puisqu'aucune
    /// fonction ne balaie le surplus. Un echec visible vaut mieux qu'un depot
    /// perdu en silence.
}
