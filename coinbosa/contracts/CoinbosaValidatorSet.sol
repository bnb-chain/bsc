// SPDX-License-Identifier: MIT
pragma solidity 0.8.26;

/**
 * CoinbosaValidatorSet — remplacement du BSCValidatorSet a l'adresse 0x...1000.
 *
 * Implemente EXACTEMENT la surface d'appel que le moteur Parlia (bsc v1.7.6)
 * exige du ValidatorContract quand Luban+Plato+Kepler sont ACTIFS et
 * Feynman/Bohr/Cancun/Maxwell INACTIFS :
 *
 *   init()                                            parlia.go:2049  (bloc 1, system tx)
 *   getMiningValidators()                             parlia.go:1934  (eth_call, blocs %200==0)
 *   deposit(address) payable                          parlia.go:2082  (system tx, chaque bloc)
 *   distributeFinalityReward(address[],uint256[])     parlia.go:1348  (system tx, blocs %200==0)
 *   getTurnLength()                                   bohrFork.go:61  (mort tant que Bohr off)
 *   getValidators()                                   lubanFork.go:28 (mort tant que Luban on)
 *
 * REGLE D'OR : aucune de ces fonctions ne doit JAMAIS revert, sinon le bloc
 * devient improduisible (Prepare) ou invalide (Finalize).
 */
contract CoinbosaValidatorSet {
    // ---------------------------------------------------------------------
    // Parametres de deploiement (white-label : seules ces 2 constantes changent)
    // ---------------------------------------------------------------------

    /// GOUVERNANCE. REECRITE automatiquement par scripts/build-genesis.js avec
    /// l'adresse passee dans GOVERNOR. Seule adresse autorisee a faire tourner le set
    /// de validateurs et a balayer le surplus. Elle ne scelle AUCUN bloc : elle peut
    /// donc vivre hors ligne (portefeuille materiel, multi-signatures).
    address public constant GOVERNOR = 0x0000000000000000000000000000000000000001;

    /// VALIDATEUR DE GENESE. REECRITE automatiquement par scripts/build-genesis.js
    /// avec l'adresse passee dans VALIDATOR. Ne pas la modifier a la main : elle doit
    /// rester IDENTIQUE au validateur inscrit dans l'extraData du genesis, sinon le
    /// moteur de consensus attend des blocs signes par une cle que personne ne detient.
    ///
    /// C'est cette adresse — et non le gouverneur — qui sert de validateur par defaut
    /// et de garde anti-arret : elle est, par construction, la seule dont la cle de
    /// scellage est detenue par un noeud mineur. Confondre les deux reviendrait a
    /// exiger la presence d'une adresse incapable de produire un bloc.
    address public constant INITIAL_VALIDATOR = 0x0000000000000000000000000000000000000002;

    // Doit etre byte-identique a la cle BLS presente dans l'extraData du genesis.
    bytes public constant GENESIS_VOTE_ADDRESS =
        hex"000000000000000000000000000000000000000000000000"
        hex"000000000000000000000000000000000000000000000000";

    uint256 public constant MAX_VALIDATORS = 41;
    uint256 public constant VOTE_ADDRESS_LENGTH = 48;

    // ---------------------------------------------------------------------
    // Etat
    // ---------------------------------------------------------------------
    bool public alreadyInit;                     // slot 0
    address[] public validators;                 // slot 1
    bytes[] public voteAddresses;                // slot 2
    mapping(address => uint256) public incoming; // slot 3
    uint256 public totalInComing;                // slot 4

    event ValidatorSetUpdated(uint256 count);
    event ValidatorDeposit(address indexed validator, uint256 amount);
    event ValidatorClaimed(address indexed validator, uint256 amount);
    event SurplusSwept(address indexed to, uint256 amount);

    // ---------------------------------------------------------------------
    // Consensus : lecture
    // ---------------------------------------------------------------------

    /// Appelee par Parlia a chaque bloc d'epoch (number % 200 == 0), sur l'etat
    /// du bloc parent, pour construire l'extraData. Retour : (adresses, cles BLS).
    /// Fallback defensif : si l'etat est vide (init() jamais execute), on renvoie
    /// quand meme le validateur de genese -> la chaine ne peut pas se suicider.
    function getMiningValidators()
        external
        view
        returns (address[] memory vals, bytes[] memory votes)
    {
        uint256 n = validators.length;
        if (n == 0) {
            vals = new address[](1);
            vals[0] = INITIAL_VALIDATOR;
            votes = new bytes[](1);
            votes[0] = GENESIS_VOTE_ADDRESS;
            return (vals, votes);
        }
        vals = new address[](n);
        votes = new bytes[](n);
        for (uint256 i = 0; i < n; ++i) {
            vals[i] = validators[i];
            votes[i] = voteAddresses[i];
        }
    }

    /// Chemin pre-Luban (code mort ici) + compatibilite outils/explorateurs.
    function getValidators() external view returns (address[] memory vals) {
        uint256 n = validators.length;
        if (n == 0) {
            vals = new address[](1);
            vals[0] = INITIAL_VALIDATOR;
            return vals;
        }
        vals = new address[](n);
        for (uint256 i = 0; i < n; ++i) {
            vals[i] = validators[i];
        }
    }

    /// Requis uniquement si Bohr est active un jour. 1 = comportement historique.
    function getTurnLength() external pure returns (uint256) {
        return 1;
    }

    function numOfValidators() external view returns (uint256) {
        return validators.length;
    }

    function isCurrentValidator(address who) external view returns (bool) {
        uint256 n = validators.length;
        for (uint256 i = 0; i < n; ++i) {
            if (validators[i] == who) return true;
        }
        return n == 0 && who == INITIAL_VALIDATOR;
    }

    // ---------------------------------------------------------------------
    // Consensus : ecriture (system tx, msg.sender = coinbase, gasPrice = 0)
    // ---------------------------------------------------------------------

    /// Bloc 1 uniquement. Un revert ici rend le bloc 1 improduisible : la garde
    /// est donc IDEMPOTENTE (pas de require). Sans controle d'acces, n'importe qui
    /// peut appeler init() ; si une tx utilisateur le fait avant la system-tx du
    /// bloc 1, un `require(!alreadyInit)` ferait revert la system-tx et suiciderait
    /// la chaine au bloc 1. Le `if (alreadyInit) return;` rend tout appel superflu
    /// inoffensif (no-op), en conservant l'etat validators=[INITIAL_VALIDATOR].
    function init() external {
        if (alreadyInit) return;
        alreadyInit = true;
        validators.push(INITIAL_VALIDATOR);
        voteAddresses.push(GENESIS_VOTE_ADDRESS);
        emit ValidatorSetUpdated(1);
    }

    /// Appelee a CHAQUE bloc ou SystemAddress a un solde > 0.
    /// Volontairement sans aucun modifier : chaque require est un risque d'arret
    /// de chaine. Qu'un tiers appelle deposit en payant est inoffensif.
    function deposit(address valAddr) external payable {
        if (msg.value > 0) {
            // unchecked : garantit formellement l'absence de revert sur ce chemin
            // de consensus (regle d'or). L'invariant d'offre fixe (700M BOSA)
            // exclut tout overflow reel de ces cumuls.
            unchecked {
                incoming[valAddr] += msg.value;
                totalInComing += msg.value;
            }
            emit ValidatorDeposit(valAddr, msg.value);
        }
    }

    /// Appelee a chaque bloc d'epoch (Plato actif). Sans VotePool les deux
    /// tableaux arrivent toujours VIDES. No-op strict : jamais de revert.
    function distributeFinalityReward(
        address[] calldata, /* validatorsIn */
        uint256[] calldata  /* weights */
    ) external {
        // volontairement vide
    }

    // ---------------------------------------------------------------------
    // Administration (hors consensus)
    // ---------------------------------------------------------------------

    /// Rotation / ajout de validateurs SANS nouveau genesis.
    /// ATTENTION : prend effet au prochain bloc d'epoch (multiple de 200).
    ///
    /// DANGER — CE QUE CETTE FONCTION NE PEUT PAS VERIFIER
    /// ---------------------------------------------------
    /// Parlia n'exige pas UN signataire : il exige ⌊N/2⌋+1 signataires DISTINCTS et
    /// EN LIGNE (snapshot.go minerHistoryCheckLen(), parlia.go Seal -> SignRecently,
    /// « Signed recently, must wait for others »). Un contrat ne peut pas savoir quels
    /// noeuds tournent : AUCUNE garde ici ne peut donc garantir la liveness.
    ///
    /// Passer de 1 a 2 validateurs alors qu'un seul noeud scelle ARRETE la chaine au
    /// bloc d'epoch suivant. La transaction est pourtant acceptee (status 1) et le
    /// reseau tourne encore jusqu'a 200 blocs avant de se figer. Comme plus aucun bloc
    /// n'est produit, aucune transaction corrective ne peut plus etre minee : cela ne
    /// se defait pas on-chain. Reproduit au banc :
    ///   consensus/parlia/coinbosa_halt_repro_test.go
    ///
    /// A noter : N=2 impose un quorum 2-sur-2 permanent — passer de 1 a 2 DEGRADE la
    /// disponibilite. Monter par paires (1 -> 3 -> 5) evite cet etat.
    ///
    /// NE JAMAIS appeler cette fonction directement. Passer par
    /// coinbosa/scripts/rotate-validators.js, qui verifie que les nouveaux validateurs
    /// scellent REELLEMENT avant d'autoriser la rotation.
    function updateValidatorSet(address[] calldata newVals, bytes[] calldata newVotes)
        external
    {
        require(msg.sender == GOVERNOR, "only governor");
        uint256 n = newVals.length;
        require(n > 0 && n <= MAX_VALIDATORS, "bad length");
        require(newVotes.length == n, "length mismatch");
        // Garde anti-arret : INITIAL_VALIDATOR doit rester dans le set. Sans cela, un
        // appel qui remplace le set par des adresses dont aucune cle n'est detenue
        // par un noeud mineur laisse le reseau sans signataire au prochain bloc
        // d'epoch, et la chaine s'arrete irreversiblement. C'est le validateur de
        // genese — et non le gouverneur — qui detient une cle de scellage : exiger sa
        // presence garantit un signataire. Exiger celle du gouverneur ne garantirait
        // rien, puisqu'il ne produit aucun bloc.
        bool sealerPresent = false;
        for (uint256 i = 0; i < n; ++i) {
            require(newVals[i] != address(0), "zero address");
            require(newVotes[i].length == VOTE_ADDRESS_LENGTH, "bad vote address");
            if (newVals[i] == INITIAL_VALIDATOR) sealerPresent = true;
            for (uint256 j = 0; j < i; ++j) {
                require(newVals[i] != newVals[j], "duplicate validator");
                require(keccak256(newVotes[i]) != keccak256(newVotes[j]), "duplicate vote address");
            }
        }
        require(sealerPresent, "genesis validator must remain a validator");
        delete validators;
        delete voteAddresses;
        for (uint256 i = 0; i < n; ++i) {
            validators.push(newVals[i]);
            voteAddresses.push(newVotes[i]);
        }
        emit ValidatorSetUpdated(n);
    }

    /// Retrait des frais de bloc accumules par un validateur.
    function claim() external {
        uint256 amount = incoming[msg.sender];
        require(amount > 0, "nothing to claim");
        incoming[msg.sender] = 0;
        totalInComing -= amount;
        (bool ok, ) = payable(msg.sender).call{value: amount}("");
        require(ok, "transfer failed");
        emit ValidatorClaimed(msg.sender, amount);
    }

    /// Fonds arrives hors deposit() (transferts directs, selfdestruct).
    function surplus() public view returns (uint256) {
        uint256 bal = address(this).balance;
        return bal > totalInComing ? bal - totalInComing : 0;
    }

    function sweepSurplus(address payable to) external {
        require(msg.sender == GOVERNOR, "only governor");
        uint256 s = surplus();
        require(s > 0, "no surplus");
        (bool ok, ) = to.call{value: s}("");
        require(ok, "transfer failed");
        emit SurplusSwept(to, s);
    }

    receive() external payable {}
}
