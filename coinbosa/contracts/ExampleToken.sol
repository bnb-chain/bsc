// SPDX-License-Identifier: MIT
pragma solidity 0.8.26;

import "./BRC20.sol";

/**
 * @title ExampleToken
 * @notice Jeton de démonstration du standard BRC20 de Coinbosa Chain.
 *
 * Ce contrat n'a d'autre objet que de montrer comment une application déploie
 * son propre jeton sur le réseau, et de servir de sujet à la suite de tests.
 * Il n'est PAS le jeton BOSA : BOSA est le coin natif de la chaîne, à 18
 * décimales, et n'est pas un contrat déployé.
 */
contract ExampleToken is BRC20 {
    constructor(address owner_)
        BRC20("Example Token", "EXMPL", 18, 1_000_000 * 10 ** 18, owner_)
    {}
}
