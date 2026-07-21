// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./BRC20.sol";

/**
 * @title BosaToken
 * @notice Le jeton officiel Coinbosa (BOSA), au standard BRC20 de Coinbosa Chain.
 *
 * Offre initiale : 700 000 000 BOSA, à 10 décimales, émise en totalité à
 * l'adresse de départ passée au déploiement. Aucune émission n'a lieu ensuite
 * sans une transaction explicite du propriétaire.
 */
contract BosaToken is BRC20 {
    /// @notice Nombre de décimales du jeton BOSA.
    uint8 public constant DECIMALS = 10;

    /// @notice Offre initiale exprimée en jetons entiers, hors décimales.
    uint256 public constant INITIAL_SUPPLY_WHOLE = 700_000_000;

    /**
     * @param initialHolder Adresse de départ qui reçoit l'intégralité de l'offre
     *        initiale et devient propriétaire du contrat.
     */
    constructor(address initialHolder)
        BRC20("Coinbosa", "BOSA", DECIMALS, INITIAL_SUPPLY_WHOLE * (10 ** uint256(DECIMALS)), initialHolder)
    {}
}
