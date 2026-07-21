// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title IBRC20
 * @notice Interface du standard de jeton BRC20 de Coinbosa Chain.
 *
 * BRC20 est compatible ERC-20 : tout wallet ou service qui parle ERC-20
 * fonctionne avec un jeton BRC20 sans adaptation. S'y ajoute `getOwner()`,
 * qui expose le propriétaire du contrat de manière standardisée pour les
 * explorateurs et les places de marché.
 */
interface IBRC20 {
    /// @notice Quantité totale de jetons en circulation.
    function totalSupply() external view returns (uint256);

    /// @notice Nombre de décimales du jeton.
    function decimals() external view returns (uint8);

    /// @notice Symbole du jeton, par exemple "BOSA".
    function symbol() external view returns (string memory);

    /// @notice Nom complet du jeton.
    function name() external view returns (string memory);

    /// @notice Propriétaire du contrat.
    function getOwner() external view returns (address);

    /// @notice Solde de `account`.
    function balanceOf(address account) external view returns (uint256);

    /// @notice Transfère `amount` jetons vers `recipient`.
    function transfer(address recipient, uint256 amount) external returns (bool);

    /// @notice Montant que `spender` peut encore dépenser pour `owner`.
    function allowance(address owner, address spender) external view returns (uint256);

    /// @notice Autorise `spender` à dépenser `amount` jetons.
    function approve(address spender, uint256 amount) external returns (bool);

    /// @notice Transfère `amount` de `sender` vers `recipient` via une autorisation.
    function transferFrom(address sender, address recipient, uint256 amount) external returns (bool);

    /// @notice Émis à chaque transfert, y compris les émissions et destructions.
    event Transfer(address indexed from, address indexed to, uint256 value);

    /// @notice Émis à chaque modification d'autorisation.
    event Approval(address indexed owner, address indexed spender, uint256 value);
}
