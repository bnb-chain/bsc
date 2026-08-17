// SPDX-License-Identifier: LGPL-3.0-only
pragma solidity ^0.8.20;

/// @title  B20 standard interfaces (BEP-702)
/// @notice Source-of-truth Solidity surface for the B20 native token standard.
///         Never deployed: the implementations are stateful precompiles inside
///         the BSC node. This package exists so integrators compile against a
///         typed surface and so the ABI baseline
///         (core/vm/testdata/b20_abi_baseline.json) has a reviewable origin.
///         The Go implementation is cross-checked against the baseline by
///         TestB20ABIBaseline; keep the three in lockstep.

/// @notice Shared surface of every B20 token, both variants (BEP-702 §3.6–3.11).
interface IB20 {
    enum PausableFeature { TRANSFER, MINT, BURN, SEIZE }

    // --- events ---
    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);
    event Memo(address indexed caller, bytes32 indexed memo);
    event NameUpdated(string newName);
    event SymbolUpdated(string newSymbol);
    event ContractURIUpdated();
    event Seized(address indexed caller, address indexed from, address indexed to, uint256 value);
    event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender);
    event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender);
    event RoleAdminChanged(bytes32 indexed role, bytes32 previousAdminRole, bytes32 newAdminRole);
    event LastAdminRenounced(address indexed previousAdmin);
    event PolicyUpdated(bytes32 indexed scope, uint64 policyId);
    event Paused(address indexed updater, PausableFeature[] features);
    event Unpaused(address indexed updater, PausableFeature[] features);
    event SupplyCapUpdated(uint256 previousCap, uint256 newCap);
    event EIP712DomainChanged();

    // --- errors ---
    error NonPayable();
    error InvalidReceiver(address receiver);
    error InvalidSender(address sender);
    error InvalidSpender(address spender);
    error InvalidApprover(address approver);
    error InsufficientBalance(address sender, uint256 balance, uint256 needed);
    error InsufficientAllowance(address spender, uint256 allowance, uint256 needed);
    error SupplyCapExceeded(uint256 cap, uint256 attempted);
    error InvalidSupplyCap(uint256 currentSupply, uint256 proposedCap);
    error ContractPaused(PausableFeature feature);
    error ExpiredSignature(uint256 deadline);
    error InvalidSigner(address signer, address owner);
    error AccessControlUnauthorizedAccount(address account, bytes32 neededRole);
    error AccessControlBadConfirmation();
    error LastAdminCannotRenounce();
    error NotSoleAdmin();
    error PolicyForbids(bytes32 policyScope, uint64 policyId);
    error PolicyNotFound();
    error UnsupportedPolicyType(bytes32 policyScope);
    error EmptyFeatureSet();
    error AccountNotSeizable(address account);

    // --- ERC-20 core ---
    function name() external view returns (string memory);
    function symbol() external view returns (string memory);
    function decimals() external view returns (uint8);
    function contractURI() external view returns (string memory);
    function totalSupply() external view returns (uint256);
    function balanceOf(address account) external view returns (uint256);
    function transfer(address to, uint256 value) external returns (bool);
    function allowance(address owner, address spender) external view returns (uint256);
    function approve(address spender, uint256 value) external returns (bool);
    function transferFrom(address from, address to, uint256 value) external returns (bool);

    // --- memo variants ---
    function transferWithMemo(address to, uint256 value, bytes32 memo) external returns (bool);
    function transferFromWithMemo(address from, address to, uint256 value, bytes32 memo) external returns (bool);
    function mintWithMemo(address to, uint256 value, bytes32 memo) external;
    function burnWithMemo(uint256 value, bytes32 memo) external;

    // --- supply / seizure ---
    function mint(address to, uint256 value) external;
    function burn(uint256 value) external;
    function seizeWithMemo(address from, address to, uint256 value, bytes32 memo) external returns (bool);

    // --- metadata ---
    function updateName(string calldata newName) external;
    function updateSymbol(string calldata newSymbol) external;
    function updateContractURI(string calldata newURI) external;

    // --- roles ---
    function DEFAULT_ADMIN_ROLE() external pure returns (bytes32);
    function MINT_ROLE() external pure returns (bytes32);
    function BURN_ROLE() external pure returns (bytes32);
    function SEIZE_ROLE() external pure returns (bytes32);
    function PAUSE_ROLE() external pure returns (bytes32);
    function UNPAUSE_ROLE() external pure returns (bytes32);
    function METADATA_ROLE() external pure returns (bytes32);
    function hasRole(bytes32 role, address account) external view returns (bool);
    function getRoleAdmin(bytes32 role) external view returns (bytes32);
    function grantRole(bytes32 role, address account) external;
    function revokeRole(bytes32 role, address account) external;
    function renounceRole(bytes32 role, address callerConfirmation) external;
    function setRoleAdmin(bytes32 role, bytes32 newAdminRole) external;
    function renounceLastAdmin() external;

    // --- policy slots (six scopes; ids are keccak256 of the scope names) ---
    function TRANSFER_SENDER_POLICY() external pure returns (bytes32);
    function TRANSFER_RECEIVER_POLICY() external pure returns (bytes32);
    function TRANSFER_EXECUTOR_POLICY() external pure returns (bytes32);
    function MINT_RECEIVER_POLICY() external pure returns (bytes32);
    function SEIZE_HOLDER_POLICY() external pure returns (bytes32);
    function SEIZE_RECEIVER_POLICY() external pure returns (bytes32);
    function policyId(bytes32 scope) external view returns (uint64);
    function updatePolicy(bytes32 scope, uint64 newPolicyId) external;

    // --- pause ---
    function pause(PausableFeature[] calldata features) external;
    function unpause(PausableFeature[] calldata features) external;
    function isPaused(PausableFeature feature) external view returns (bool);
    function pausedFeatures() external view returns (PausableFeature[] memory);

    // --- supply cap ---
    function supplyCap() external view returns (uint256);
    function updateSupplyCap(uint256 newCap) external;

    // --- permit (EIP-2612 / ERC-5267) ---
    function permit(address owner, address spender, uint256 value, uint256 deadline, uint8 v, bytes32 r, bytes32 s)
        external;
    function nonces(address owner) external view returns (uint256);
    function DOMAIN_SEPARATOR() external view returns (bytes32);
    function eip712Domain() external view returns (
        bytes1 fields, string memory name, string memory version,
        uint256 chainId, address verifyingContract, bytes32 salt, uint256[] memory extensions
    );
}

/// @notice Asset-variant extensions (BEP-702 §3.12).
interface IB20Asset {
    event MultiplierUpdated(uint256 multiplier);
    event Announcement(address indexed caller, string id, string description, string uri);
    event EndAnnouncement(string id);
    event ExtraMetadataUpdated(string key, string value);

    error InvalidMultiplier();
    error InvalidMetadataKey();
    error AnnouncementInProgress();
    error AnnouncementIdAlreadyUsed(string id);
    error InternalCallMalformed(bytes call);
    error InternalCallFailed(bytes call);
    error LengthMismatch(uint256 leftLen, uint256 rightLen);
    error EmptyBatch();

    function OPERATOR_ROLE() external pure returns (bytes32);
    function WAD_PRECISION() external pure returns (uint256);
    function multiplier() external view returns (uint256);
    function updateMultiplier(uint256 newMultiplier) external;
    function toScaledBalance(uint256 rawBalance) external view returns (uint256);
    function toRawBalance(uint256 scaledBalance) external view returns (uint256);
    function scaledBalanceOf(address account) external view returns (uint256);
    function announce(bytes[] calldata internalCalls, string calldata id, string calldata description, string calldata uri)
        external;
    function isAnnouncementIdUsed(string calldata id) external view returns (bool);
    function batchMint(address[] calldata recipients, uint256[] calldata amounts) external;
    function extraMetadata(string calldata key) external view returns (string memory);
    function updateExtraMetadata(string calldata key, string calldata value) external;
}

/// @notice Stablecoin-variant extension (BEP-702 §3.13).
interface IB20Stablecoin {
    function currency() external view returns (string memory);
}

/// @notice Singleton token factory (BEP-702 §3.4).
interface IB20Factory {
    enum Variant { ASSET, STABLECOIN }

    struct B20AssetCreateParams {
        uint8   version;      // this revision: 1
        string  name;
        string  symbol;
        address initialAdmin; // zero address for an admin-less token
        uint8   decimals;     // in [6, 18]
    }

    struct B20StablecoinCreateParams {
        uint8   version;      // this revision: 1
        string  name;
        string  symbol;
        address initialAdmin;
        string  currency;     // non-empty, uppercase A-Z only
    }

    struct B20StablecoinEventParams {
        uint8  version;       // this revision: 1
        string currency;
    }

    event B20Created(
        address indexed token, Variant indexed variant,
        string name, string symbol, uint8 decimals, bytes variantEventParams
    );

    error InvalidVariant();
    error UnsupportedVersion(uint8 version, Variant variant);
    error InvalidDecimals(uint8 decimals);
    error MissingRequiredField(string field);
    error InvalidCurrency(string code);
    error TokenAlreadyExists(address token);
    error InitCallFailed(uint256 index);

    function createB20(Variant variant, bytes32 salt, bytes calldata params, bytes[] calldata initCalls)
        external returns (address token);
    function getB20Address(Variant variant, address creator, bytes32 salt) external view returns (address);
    function isB20(address account) external pure returns (bool);
    function variantOf(address token) external pure returns (Variant);
    function isB20Initialized(address token) external view returns (bool);
}

/// @notice Singleton shared policy registry (BEP-702 §3.8).
interface IPolicyRegistry {
    enum PolicyType { BLOCKLIST, ALLOWLIST }

    event PolicyCreated(uint64 indexed policyId, address indexed creator, PolicyType policyType);
    event PolicyAdminStaged(uint64 indexed policyId, address indexed currentAdmin, address indexed pendingAdmin);
    event PolicyAdminUpdated(uint64 indexed policyId, address indexed previousAdmin, address indexed newAdmin);
    /// @dev Membership changes are reported per policy type rather than through one
    /// merged event, so a consumer can subscribe to just the list it cares about.
    event AllowlistUpdated(uint64 indexed policyId, address indexed updater, bool allowed, address[] accounts);
    event BlocklistUpdated(uint64 indexed policyId, address indexed updater, bool blocked, address[] accounts);

    error Unauthorized();
    error ZeroAddress();
    error IncompatiblePolicyType();
    error BatchSizeTooLarge(uint256 maxBatchSize);
    error NoPendingAdmin();

    function createPolicy(address admin, PolicyType policyType) external returns (uint64 policyId);
    function createPolicyWithAccounts(address admin, PolicyType policyType, address[] calldata accounts)
        external returns (uint64 policyId);
    function updateAllowlist(uint64 policyId, bool allowed, address[] calldata accounts) external;
    function updateBlocklist(uint64 policyId, bool blocked, address[] calldata accounts) external;
    function stageUpdateAdmin(uint64 policyId, address newAdmin) external;
    function finalizeUpdateAdmin(uint64 policyId) external;
    function renounceAdmin(uint64 policyId) external;
    function isAuthorized(uint64 policyId, address account) external view returns (bool);
    function policyExists(uint64 policyId) external view returns (bool);
    function policyAdmin(uint64 policyId) external view returns (address);
    function pendingPolicyAdmin(uint64 policyId) external view returns (address);
}

/// @notice Singleton per-feature governance switch (BEP-702 §3.15).
interface IActivationRegistry {
    event FeatureActivated(bytes32 indexed feature, address indexed caller);
    event FeatureDeactivated(bytes32 indexed feature, address indexed caller);
    event AdminChanged(address indexed previousAdmin, address indexed newAdmin, address indexed caller);

    error FeatureNotActivated(bytes32 feature);
    error AlreadyActivated(bytes32 feature);
    error Unauthorized(address caller);
    error ZeroAdminAddress();

    function isActivated(bytes32 feature) external view returns (bool);
    function checkActivated(bytes32 feature) external view;
    function admin() external view returns (address);
    function setAdmin(address newAdmin) external;
    function activate(bytes32 feature) external;
    function deactivate(bytes32 feature) external;
}

/// @notice Canonical B20 role / policy-scope / feature constants.
library B20Constants {
    bytes32 internal constant DEFAULT_ADMIN_ROLE = bytes32(0);
    bytes32 internal constant MINT_ROLE = keccak256("MINT_ROLE");
    bytes32 internal constant BURN_ROLE = keccak256("BURN_ROLE");
    bytes32 internal constant SEIZE_ROLE = keccak256("SEIZE_ROLE");
    bytes32 internal constant PAUSE_ROLE = keccak256("PAUSE_ROLE");
    bytes32 internal constant UNPAUSE_ROLE = keccak256("UNPAUSE_ROLE");
    bytes32 internal constant METADATA_ROLE = keccak256("METADATA_ROLE");
    bytes32 internal constant OPERATOR_ROLE = keccak256("OPERATOR_ROLE");

    bytes32 internal constant TRANSFER_SENDER_POLICY = keccak256("TRANSFER_SENDER_POLICY");
    bytes32 internal constant TRANSFER_RECEIVER_POLICY = keccak256("TRANSFER_RECEIVER_POLICY");
    bytes32 internal constant TRANSFER_EXECUTOR_POLICY = keccak256("TRANSFER_EXECUTOR_POLICY");
    bytes32 internal constant MINT_RECEIVER_POLICY = keccak256("MINT_RECEIVER_POLICY");
    bytes32 internal constant SEIZE_HOLDER_POLICY = keccak256("SEIZE_HOLDER_POLICY");
    bytes32 internal constant SEIZE_RECEIVER_POLICY = keccak256("SEIZE_RECEIVER_POLICY");

    bytes32 internal constant FEATURE_B20_ASSET = keccak256("bsc.b20_asset");
    bytes32 internal constant FEATURE_B20_STABLECOIN = keccak256("bsc.b20_stablecoin");
    bytes32 internal constant FEATURE_POLICY_REGISTRY = keccak256("bsc.policy_registry");

    uint64 internal constant ALWAYS_ALLOW = 0;
    uint64 internal constant ALWAYS_BLOCK = (uint64(1) << 56) | 1;

    // Both bounds of the overflow guard (BEP-702 §3.10 / §3.12).
    uint256 internal constant MAX_SUPPLY_CAP = type(uint128).max;
    uint256 internal constant MAX_MULTIPLIER = type(uint128).max;

    address internal constant B20_FACTORY = 0x20Bf000000000000000000000000000000000000;
    address internal constant POLICY_REGISTRY = 0x7020000000000000000000000000000000000001;
    address internal constant ACTIVATION_REGISTRY = 0x7020000000000000000000000000000000000002;
}
