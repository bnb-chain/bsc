// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./IBOS20.sol";

/**
 * @title BOS20
 * @notice Implémentation de référence du standard BOS20 de Coinbosa Chain.
 *
 * Sécurité : la logique de transfert refuse l'adresse zéro dans les deux sens,
 * les autorisations infinies ne sont pas décrémentées, et la propriété peut être
 * transférée ou définitivement abandonnée.
 */
contract BOS20 is IBOS20 {
    mapping(address => uint256) private _balances;
    mapping(address => mapping(address => uint256)) private _allowances;

    uint256 private _totalSupply;
    string private _name;
    string private _symbol;
    uint8 private immutable _decimals;
    address private _owner;

    /// @notice Émis lorsque la propriété du contrat change.
    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);

    modifier onlyOwner() {
        require(msg.sender == _owner, "BOS20: reserve au proprietaire");
        _;
    }

    constructor(string memory name_, string memory symbol_, uint8 decimals_, uint256 initialSupply_, address owner_) {
        require(owner_ != address(0), "BOS20: proprietaire nul");
        _name = name_;
        _symbol = symbol_;
        _decimals = decimals_;
        _owner = owner_;
        emit OwnershipTransferred(address(0), owner_);

        if (initialSupply_ > 0) {
            _mint(owner_, initialSupply_);
        }
    }

    // --- Lectures ---

    function name() public view override returns (string memory) { return _name; }
    function symbol() public view override returns (string memory) { return _symbol; }
    function decimals() public view override returns (uint8) { return _decimals; }
    function totalSupply() public view override returns (uint256) { return _totalSupply; }
    function getOwner() public view override returns (address) { return _owner; }
    function balanceOf(address account) public view override returns (uint256) { return _balances[account]; }

    function allowance(address owner_, address spender) public view override returns (uint256) {
        return _allowances[owner_][spender];
    }

    // --- Transferts ---

    function transfer(address recipient, uint256 amount) public override returns (bool) {
        _transfer(msg.sender, recipient, amount);
        return true;
    }

    function approve(address spender, uint256 amount) public override returns (bool) {
        _approve(msg.sender, spender, amount);
        return true;
    }

    function transferFrom(address sender, address recipient, uint256 amount) public override returns (bool) {
        uint256 current = _allowances[sender][msg.sender];
        if (current != type(uint256).max) {
            require(current >= amount, "BOS20: autorisation insuffisante");
            unchecked { _approve(sender, msg.sender, current - amount); }
        }
        _transfer(sender, recipient, amount);
        return true;
    }

    /// @notice Augmente l'autorisation accordée à `spender`, sans condition de course.
    function increaseAllowance(address spender, uint256 addedValue) public returns (bool) {
        _approve(msg.sender, spender, _allowances[msg.sender][spender] + addedValue);
        return true;
    }

    /// @notice Réduit l'autorisation accordée à `spender`.
    function decreaseAllowance(address spender, uint256 subtractedValue) public returns (bool) {
        uint256 current = _allowances[msg.sender][spender];
        require(current >= subtractedValue, "BOS20: autorisation en dessous de zero");
        unchecked { _approve(msg.sender, spender, current - subtractedValue); }
        return true;
    }

    // --- Émission et destruction ---

    /// @notice Crée `amount` jetons et les attribue à `account`.
    function mint(address account, uint256 amount) public onlyOwner returns (bool) {
        _mint(account, amount);
        return true;
    }

    /// @notice Détruit `amount` jetons du solde de l'appelant.
    function burn(uint256 amount) public returns (bool) {
        _burn(msg.sender, amount);
        return true;
    }

    // --- Propriété ---

    function transferOwnership(address newOwner) public onlyOwner {
        require(newOwner != address(0), "BOS20: nouveau proprietaire nul");
        emit OwnershipTransferred(_owner, newOwner);
        _owner = newOwner;
    }

    /// @notice Abandonne définitivement la propriété. Irréversible.
    function renounceOwnership() public onlyOwner {
        emit OwnershipTransferred(_owner, address(0));
        _owner = address(0);
    }

    // --- Interne ---

    function _transfer(address sender, address recipient, uint256 amount) internal {
        require(sender != address(0), "BOS20: envoi depuis l'adresse nulle");
        require(recipient != address(0), "BOS20: envoi vers l'adresse nulle");
        uint256 senderBalance = _balances[sender];
        require(senderBalance >= amount, "BOS20: solde insuffisant");
        unchecked {
            _balances[sender] = senderBalance - amount;
            _balances[recipient] += amount;
        }
        emit Transfer(sender, recipient, amount);
    }

    function _mint(address account, uint256 amount) internal {
        require(account != address(0), "BOS20: emission vers l'adresse nulle");
        _totalSupply += amount;
        unchecked { _balances[account] += amount; }
        emit Transfer(address(0), account, amount);
    }

    function _burn(address account, uint256 amount) internal {
        require(account != address(0), "BOS20: destruction depuis l'adresse nulle");
        uint256 accountBalance = _balances[account];
        require(accountBalance >= amount, "BOS20: solde insuffisant pour destruction");
        unchecked {
            _balances[account] = accountBalance - amount;
            _totalSupply -= amount;
        }
        emit Transfer(account, address(0), amount);
    }

    function _approve(address owner_, address spender, uint256 amount) internal {
        require(owner_ != address(0), "BOS20: autorisation depuis l'adresse nulle");
        require(spender != address(0), "BOS20: autorisation vers l'adresse nulle");
        _allowances[owner_][spender] = amount;
        emit Approval(owner_, spender, amount);
    }
}
