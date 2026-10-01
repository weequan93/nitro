// SPDX-License-Identifier: MIT

pragma solidity ^0.8.0;

import "../core/interfaces/IDataReader.sol";
import "../upgradeability/Synchron.sol";

/**
 * @title   PriceOracle
 * @notice  Oracle contract for managing index token prices with multi-ID versioning
 * @dev     Inherits Synchron for upgradeable proxy pattern. Prices are stored per indexToken
 *          with auto-incrementing priceId. Channel tokens are resolved to underlying index tokens
 *          via DataReader before storage. Each price record includes ask/bid/mid and an update timestamp.
 *          Freshness is validated at write time via maxTimeDeviation:
 *          backend-generated timestamp must be within ±N seconds of block.timestamp.
 *          Read paths return whatever is stored; there is no stale-price expiration check.
 *          USDT is the project's current sole stablecoin: its price is fixed at ONE_USD
 *          (= 1 USD, 30 decimals), it is exempt from batchSetPrices writes, and reads
 *          synthesize the fixed price instead of reading storage
 *          (getPriceId/getLastUpdateTime return 0 for USDT).
 */
contract PriceOracle is Synchron {

    // ============ Config Variables ============

    /// @notice Fixed price of 1 USD in oracle price units (30 decimals)
    /// @dev USDT is the project's sole stablecoin and is priced at exactly 1 USD (ONE_USD).
    ///      Because USDT never goes through batchSetPrices / storage, its price is
    ///      synthesized here on every read instead.
    uint256 public constant ONE_USD = 10 ** 30;

    /// @notice Half of the secp256k1 curve order (n/2), used to reject high-s signatures
    /// @dev Enforces EIP-2 low-s normalization to prevent signature malleability in ecrecover.
    uint256 internal constant SECP256K1_HALF_ORDER = 0x7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF5D576E7357A4501DDFE92F46681B20A0;

    /// @notice Maximum allowed deviation (seconds) between backend _timestamp and block.timestamp
    /// @dev Used in batchSetPrices to reject stale backend data. Default: 30s
    uint256 public maxTimeDeviation;

    // ============ State Variables ============

    /// @notice Whether the contract has been initialized
    /// @dev Prevents re-initialization attacks on upgradeable proxy pattern.
    ///      Set to true in constructor (implementation) and initialize() (proxy storage)
    bool public initialized;

    /// @notice Address of the governance account
    /// @dev Only gov can call admin functions. Set during initialize()
    address public gov;

    /// @notice DataReader contract for channel token → index token resolution
    /// @dev Must be set via setContract() after proxy deployment
    IDataReader public dataReader;

    /// @notice USDT token address, the project's current sole stablecoin (price fixed at 1 USD)
    /// @dev Set once via initialize(address). USDT is exempt from batchSetPrices writes
    ///      and its price is synthesized as ONE_USD on read instead of being stored.
    address public usdt;

    /// @notice List of signer addresses required to sign batchSetPrices before execution
    /// @dev Managed by gov via addSigner()/removeSigner(). Must contain at least two signers
    ///      (majority (> half) of them must sign for a batch to pass).
    address[] public signers;

    /// @notice Last used salt for batchSetPrices (must be strictly increasing to prevent replay)
    /// @dev Stored on each successful batchSetPrices to reject replayed signatures.
    uint256 public lastSalt;

    // ============ Mappings ============

    /// @notice Tracks which addresses are authorized to call batchSetPrices
    /// @dev Managed by gov via setUpdater()
    mapping(address => bool) public isUpdater;

    /// @notice Tracks whether an address is currently an active signer
    /// @dev Managed by gov via addSigner()/removeSigner()
    mapping(address => bool) public isSigner;

    /// @notice Current price ID for each index token (auto-incremented on each update)
    /// @dev ID 0 means "never set"; first update assigns ID 1
    mapping(address => uint256) priceId;

    /// @notice Price records: indexToken → priceId → PriceInfo
    mapping(address => mapping(uint256 => PriceInfo)) price;

    /// @notice Last update timestamp: indexToken → priceId → unix timestamp
    /// @dev Stores block.timestamp at the moment of on-chain recording
    mapping(address => mapping(uint256 => uint256)) lastUpdateTime;

    // ============ Structs ============

    /**
     * @notice Price information for a single index token record
     * @dev   Mid price must equal (ask + bid) / 2 (integer division, truncated toward zero).
     *        For odd sums, the caller must set priceMid to the truncated result.
     * @param indexToken  Index token address (resolved from channel token if applicable)
     * @param priceAsk    Ask price (max price, used for long entry / short exit)
     * @param priceBid    Bid price (min price, used for short entry / long exit, must be > 0)
     * @param priceMid    Mid price = (ask + bid) / 2, used as oracle reference
     */
    struct PriceInfo {
        address indexToken;  // Resolved index token address
        uint256 priceAsk;    // Ask price
        uint256 priceBid;    // Bid price
        uint256 priceMid;    // Mid price
    }

    // ============ Events ============

    /// @notice Emitted when an updater is added or removed
    /// @param account  Address of the updater
    /// @param isActive Whether the account is now an active updater
    event SetUpdater(address account, bool isActive);

    /// @notice Emitted when a signer is added
    /// @param account Address of the added signer
    event AddSigner(address account);

    /// @notice Emitted when a signer is removed
    /// @param account Address of the removed signer
    event RemoveSigner(address account);

    /// @notice Emitted when a price is successfully set for an index token
    /// @param indexToken Resolved index token address
    /// @param priceId    Assigned price ID (auto-incremented)
    /// @param priceAsk   Ask price
    /// @param priceBid   Bid price
    /// @param priceMid   Mid price
    /// @param timestamp  Block timestamp of recording
    event SetPrice(address indexToken, uint256 priceId, uint256 priceAsk, uint256 priceBid, uint256 priceMid, uint256 timestamp);

    /// @notice Emitted when maxTimeDeviation is updated
    /// @param oldValue Previous maxTimeDeviation in seconds
    /// @param newValue Updated maxTimeDeviation in seconds
    event SetMaxTimeDeviation(uint256 oldValue, uint256 newValue);

    /// @notice Emitted when a price in batchSetPrices fails validation and is skipped
    /// @param indexToken Resolved index token address
    /// @param priceAsk   Invalid ask price
    /// @param priceBid   Invalid bid price
    /// @param priceMid   Invalid mid price
    /// @param timestamp  Block timestamp when the error occurred
    event PriceErr(address indexToken, uint256 priceAsk, uint256 priceBid, uint256 priceMid, uint256 timestamp);

    // ============ Constructor ============

    /**
     * @notice Lock the implementation contract against direct initialization
     * @dev    Sets initialized = true on the implementation's own storage.
     *         When the proxy delegatecalls initialize(), the proxy's storage is used
     *         (where initialized is still false), allowing one-time init via proxy.
     *         Direct calls to initialize() on the implementation contract will revert.
     */
    constructor() {
        initialized = true;
    }

    // ============ Modifiers ============

    /// @notice Restricts function access to governance only
    /// @dev    Reverts silently if caller is not gov
    modifier onlyGov() {
        if(gov != msg.sender) revert("not gov");
        _;
    }

    // ============ Initialization ============

    /**
     * @notice Initialize the contract with caller as governance
     * @dev    Called once during deployment via proxy pattern.
     *         Sets msg.sender as initial governance address and default time parameters.
     *         Contract addresses must be set separately via setContract().
     *         The chain id is auto-derived from block.chainid (not passed in) for signature anti-replay.
     *         Registers the initial signer set; at least two signers are required.
     *         Reverts if already initialized, _usdt is address(0), _signers has fewer than 2
     *         entries, or any signer is address(0)/duplicate.
     * @param _usdt USDT token address; the project's sole stablecoin, price fixed at ONE_USD
     * @param _signers Initial array of signer addresses required to sign batchSetPrices (at least 2)
     */
    function initialize(address _usdt, address[] calldata _signers) external {
        if(initialized || _usdt == address(0)) revert();
        uint256 len = _signers.length;
        if(len < 2) revert("need at least 2 signers");
        initialized = true;
        gov = msg.sender;
        maxTimeDeviation = 30;
        usdt = _usdt;
        // Register initial signers (reject address(0) and duplicates)
        for(uint256 i = 0; i < len; i++) {
            _addSigner(_signers[i]);
        }
    }

    // ============ Admin Functions ============

    /**
     * @notice Transfer governance to a new account
     * @dev    Only callable by current governance. Reverts if account is address(0).
     * @param account New governance address (must be non-zero)
     */
    function setGov(address account) external onlyGov {
        if(account == address(0)) revert();
        gov = account;
    }

    /**
     * @notice Add or remove an address as price updater
     * @dev    Only callable by governance. Reverts if account is address(0).
     *         Updaters and signers are mutually exclusive: an address cannot be both.
     *         This prevents a single entity from simultaneously submitting and signing
     *         (which would degrade multi-sig to single-sig). When activating an address
     *         as updater, it must not currently be a signer.
     * @param account  Address to update
     * @param isActive true to authorize, false to revoke
     */
    function setUpdater(address account, bool isActive) external onlyGov {
        if(account == address(0)) revert();
        if(isActive && isSigner[account]) revert("address is signer");
        isUpdater[account] = isActive;
        emit SetUpdater(account, isActive);
    }

    /**
     * @notice Add an address as a required signer for batchSetPrices
     * @dev    Only callable by governance. Reverts if account is address(0) or already a signer.
     *         Updaters and signers are mutually exclusive: an address cannot be both.
     *         This prevents a single entity from simultaneously submitting and signing
     *         (which would degrade multi-sig to single-sig). An address that is currently
     *         an updater cannot be added as a signer.
     * @param account Address to add as signer
     */
    function addSigner(address account) external onlyGov {
        _addSigner(account);
    }

    /**
     * @dev Internal helper to register a signer address.
     *      Shared by initialize() (bulk initial signers) and addSigner() (single add).
     *      Reverts if account is address(0), already a signer, or currently an updater
     *      (updaters and signers are mutually exclusive).
     * @param account Address to add as signer
     */
    function _addSigner(address account) internal {
        if(account == address(0)) revert();
        if(isSigner[account]) revert("already signer");
        if(isUpdater[account]) revert("address is updater");
        isSigner[account] = true;
        signers.push(account);
        emit AddSigner(account);
    }

    /**
     * @notice Remove an address from the required signer list
     * @dev    Only callable by governance. Reverts if account is not a signer, or if fewer than
     *         2 signers would remain (at least 2 signers must always be configured).
     *         IMPORTANT: at least 2 signers must ALWAYS remain. If you need to fully replace
     *         the signer set, you MUST first addSigner() the new correct signer address(es), and
     *         only then removeSigner() the old ones. Attempting to remove down to fewer than 2
     *         signers reverts with "need at least 2 signers" to prevent locking the oracle.
     *         Removal preserves the relative order of the remaining signers.
     * @param account Address to remove from signers
     */
    function removeSigner(address account) external onlyGov {
        if(!isSigner[account]) revert("not signer");
        if(signers.length <= 2) revert("need at least 2 signers");
        isSigner[account] = false;
        uint256 len = signers.length;
        for(uint256 i = 0; i < len; i++) {
            if(signers[i] == account) {
                // Shift subsequent elements left to preserve order
                for(uint256 j = i; j < len - 1; j++) {
                    signers[j] = signers[j + 1];
                }
                signers.pop();
                break;
            }
        }
        emit RemoveSigner(account);
    }

    /**
     * @notice Get the list of current signers
     * @return Array of signer addresses
     */
    function getSigners() external view returns(address[] memory) {
        return signers;
    }

    /**
     * @notice Get the number of current signers
     * @return Signer count
     */
    function getSignerCount() external view returns(uint256) {
        return signers.length;
    }

    /**
     * @notice Set the maximum allowed deviation between backend timestamp and block.timestamp
     * @dev    Only callable by governance. Used in batchSetPrices timestamp validation.
     * @param _maxTimeDeviation Maximum deviation in seconds
     */
    function setMaxTimeDeviation(uint256 _maxTimeDeviation) external onlyGov {
        if(_maxTimeDeviation > 3600) revert("max 3600s");
        uint256 oldValue = maxTimeDeviation;
        maxTimeDeviation = _maxTimeDeviation;
        emit SetMaxTimeDeviation(oldValue, _maxTimeDeviation);
    }

    /**
     * @notice Set the DataReader contract address for channel token resolution
     * @dev    Only callable by governance. Reverts if _dataReader is address(0).
     *         Must be called after proxy deployment and before price operations.
     * @param _dataReader DataReader contract address
     */
    function setContract(address _dataReader) external onlyGov {
        if(_dataReader == address(0)) revert();
        dataReader = IDataReader(_dataReader);
    }

    // ============ Price Writing ============

    /**
     * @notice Batch set prices for multiple index tokens, gated by multi-signature approval
     * @dev    Only callable by isUpdater. Validates:
     *         - Backend _timestamp is within ±maxTimeDeviation of block.timestamp
     *         - priceBid > 0, priceAsk >= priceBid, priceMid == (priceAsk + priceBid) / 2 (truncated)
     *         - indexToken != usdt: USDT is the sole stablecoin, exempt from writes (fixed at ONE_USD)
     *         - _salt is strictly greater than lastSalt (anti-replay)
     *         - a majority (> half) of the configured signers have produced valid signatures,
     *           each unique and in any order
     *         Invalid entries emit PriceErr and are skipped without reverting the batch.
     *         Auto-increments priceId per token, records block.timestamp as lastUpdateTime.
     *         The signed digest binds (_chainId, address(this), _salt, _timestamp, keccak256(_prices)),
     *         using the EIP-191 personal-sign prefix.
     * @param _prices    Array of PriceInfo structs; indexToken auto-resolved for channel tokens via DataReader
     * @param _timestamp Backend-generated timestamp to validate freshness
     * @param _salt      Monotonically increasing salt (must be > lastSalt) to prevent replay
     * @param _chainId   Chain id the signature is bound to (anti-cross-chain replay)
     * @param v          Array of v components, one per valid signer (any order)
     * @param r          Array of r components, one per valid signer
     * @param s          Array of s components, one per valid signer
     */
    function batchSetPrices(
        PriceInfo[] calldata _prices,
        uint256 _timestamp,
        uint256 _salt,
        uint256 _chainId,
        uint8[] calldata v,
        bytes32[] calldata r,
        bytes32[] calldata s
    ) external {
        uint256 len = _prices.length;
        if(len == 0) revert("empty array");
        if(!isUpdater[msg.sender]) revert("not updater");

        // Anti-replay: _salt must be strictly increasing
        if(_salt <= lastSalt) revert("invalid salt");

        // Validate backend timestamp is within acceptable deviation
        {
            uint256 dev = block.timestamp > _timestamp ? block.timestamp - _timestamp : _timestamp - block.timestamp;
            if(dev > maxTimeDeviation) revert("time deviation too large");
        }

        // Verify a majority of distinct configured signers signed this exact digest (any order)
        _verifySignatures(_prices, _timestamp, _salt, _chainId, v, r, s);

        // Record _salt after successful signature verification
        lastSalt = _salt;

        for(uint256 i = 0; i < len; i++) {
            _processPrice(_prices[i]);
        }
    }

    /**
     * @dev Internal helper to validate and store a single price record.
     *      Reverts/emits PriceErr on invalid input, otherwise increments priceId and stores.
     */
    function _processPrice(PriceInfo calldata _price) internal {
        // Resolve channel token → underlying index token via DataReader
        address indexToken = dataReader.getIndexToken(_price.indexToken);
        uint256 _priceAsk = _price.priceAsk;
        uint256 _priceBid = _price.priceBid;
        uint256 _priceMid = _price.priceMid;

        // Validate prices: bid>0, ask>=bid, mid==(ask+bid)/2 (integer division truncated)
        // USDT is the sole stablecoin: reject writes and keep its price fixed at ONE_USD.
        if(_priceBid == 0 || _priceAsk < _priceBid || _priceMid != (_priceAsk + _priceBid) / 2 || indexToken == usdt) {
            emit PriceErr(indexToken, _priceAsk, _priceBid, _priceMid, block.timestamp);
            return;
        }

        // Auto-increment priceId (first record gets ID 1, 0 means "never set")
        uint256 newId = ++priceId[indexToken];
        price[indexToken][newId] = PriceInfo({
            indexToken: indexToken,
            priceAsk:   _priceAsk,
            priceBid:   _priceBid,
            priceMid:   _priceMid
        });

        // Record on-chain confirmation time (not backend _timestamp)
        lastUpdateTime[indexToken][newId] = block.timestamp;
        emit SetPrice(indexToken, newId, _priceAsk, _priceBid, _priceMid, block.timestamp);
    }

    /**
     * @notice Compute the three intermediate hashes used to build the signing digest
     * @dev    Exposed so off-chain signers can reproduce the exact digest to sign.
     *         - pricesHash = keccak256(abi.encode(_prices))
     *         - innerHash  = keccak256(abi.encode(_chainId, address(this), _salt, _timestamp, pricesHash))
     *         - digest     = keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", innerHash))
     *         The digest is what each signer must sign (EIP-191 personal-sign).
     *         Reverts if fewer than 2 signers are configured, or if _chainId != block.chainid.
     * @param _prices    Array of PriceInfo structs (same as batchSetPrices)
     * @param _timestamp Backend-generated timestamp
     * @param _salt      Monotonically increasing salt
     * @param _chainId   Chain id the signature is bound to (must equal block.chainid)
     * @return pricesHash Hash of the encoded prices array
     * @return innerHash  Hash of the (_chainId, contract, _salt, timestamp, pricesHash) tuple
     * @return digest     Final EIP-191 digest that signers must sign
     */
    function getSignHash(
        PriceInfo[] calldata _prices,
        uint256 _timestamp,
        uint256 _salt,
        uint256 _chainId
    ) public view returns(bytes32 pricesHash, bytes32 innerHash, bytes32 digest) {
        if(signers.length < 2) revert("need at least 2 signers");
        if(_chainId != block.chainid) revert("chainId mismatch");
        pricesHash = keccak256(abi.encode(_prices));
        innerHash = keccak256(abi.encode(_chainId, address(this), _salt, _timestamp, pricesHash));
        digest = keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", innerHash));
    }

    /**
     * @dev Internal helper to verify that a strict majority of the configured signers signed
     *      the batch digest (produced by getSignHash()).
     *      Signatures need NOT be in any particular order. Each submitted signature is
     *      recovered and must come from a distinct configured signer. Verification passes only
     *      when the count of distinct valid signers is strictly greater than half of all signers
     *      (e.g. 2-of-3, 3-of-4, 2-of-2). Duplicate/unknown signer signatures revert.
     */
    function _verifySignatures(
        PriceInfo[] calldata _prices,
        uint256 _timestamp,
        uint256 _salt,
        uint256 _chainId,
        uint8[] calldata v,
        bytes32[] calldata r,
        bytes32[] calldata s
    ) internal view {
        uint256 signerLen = signers.length;
        if(signerLen < 2) revert("need at least 2 signers");
        uint256 threshold = signerLen / 2; // need > threshold distinct signatures (majority)

        uint256 sigCount = v.length;
        if(sigCount != r.length || sigCount != s.length) revert("signature count mismatch");
        if(sigCount <= threshold) revert("not enough signatures");
        if(sigCount > signerLen) revert("too many signatures");

        (, , bytes32 digest) = getSignHash(_prices, _timestamp, _salt, _chainId);

        address[] memory used = new address[](sigCount);
        uint256 validCount = 0;
        for(uint256 i = 0; i < sigCount; i++) {
            // Enforce EIP-155 v ∈ {27, 28} to reject malformed signature v components.
            if(v[i] != 27 && v[i] != 28) revert("invalid signature v");
            // Enforce EIP-2 low-s to reject malleable signatures.
            if(uint256(s[i]) > SECP256K1_HALF_ORDER) revert("invalid signature s");
            // Recover the signer and require it to be an active configured signer.
            address recovered = ecrecover(digest, v[i], r[i], s[i]);
            if(!isSigner[recovered]) revert("not a signer");
            // Deduplicate: the same signer may not sign twice.
            for(uint256 j = 0; j < validCount; j++) {
                if(used[j] == recovered) revert("duplicate signature");
            }
            used[validCount++] = recovered;
        }
        // Majority check: validCount must be strictly more than half of all signers.
        if(validCount * 2 <= signerLen) revert("not enough signatures");
    }

    // ============ Price Reading ============

    /**
     * @notice Get the latest priceId for an index token
     * @dev    Returns 0 if no price has ever been set for this token.
     *         Channel tokens are auto-resolved via DataReader.
     *         USDT (the sole stablecoin): always 0, since its price is fixed at ONE_USD and
     *         is never stored through batchSetPrices.
     * @param _indexToken The index token (or channel token) address to query
     * @return Current priceId (0 if never set)
     */
    function getPriceId(address _indexToken) external view returns(uint256) {
        _indexToken = dataReader.getIndexToken(_indexToken);
        return priceId[_indexToken];
    }

    /**
     * @notice Get historical price data by indexToken and priceId
     * @dev    Channel tokens are auto-resolved. Returns default (zero) values for unset IDs.
     *         USDT (the sole stablecoin): always returns the fixed price ONE_USD for any _id
     *         (including 0), since it is never stored.
     * @param _indexToken The index token (or channel token) address to query
     * @param _id         Price ID to query
     * @return indexToken Resolved index token address
     * @return priceAsk   Ask price (0 if ID not set, ONE_USD for USDT)
     * @return priceBid   Bid price (0 if ID not set, ONE_USD for USDT)
     * @return priceMid   Mid price (0 if ID not set, ONE_USD for USDT)
     */
    function getPriceInfo(address _indexToken, uint256 _id) external view returns(address indexToken, uint256 priceAsk, uint256 priceBid, uint256 priceMid) {
        _indexToken = dataReader.getIndexToken(_indexToken);
        return _getPriceInfo(_indexToken, _id);
    }

    /**
     * @dev Internal helper to read PriceInfo from storage
     *      USDT, the project's sole stablecoin, has a fixed price of ONE_USD (= 1 USD) that
     *      is never stored; it is synthesized here for any _id (including 0).
     * @param _indexToken Resolved index token address
     * @param _id         Price ID
     * @return indexToken Resolved index token address
     * @return priceAsk   Ask price
     * @return priceBid   Bid price
     * @return priceMid   Mid price
     */
    function _getPriceInfo(address _indexToken, uint256 _id) internal view returns(address indexToken, uint256 priceAsk, uint256 priceBid, uint256 priceMid) {
        if(_indexToken == usdt) {
            return (usdt, ONE_USD, ONE_USD, ONE_USD);
        }
        PriceInfo memory p = price[_indexToken][_id];
        return (p.indexToken, p.priceAsk, p.priceBid, p.priceMid);
    }

    /**
     * @notice Get the block timestamp when a specific price record was written
     * @dev    Channel tokens are auto-resolved. Returns 0 for unset IDs.
     *         USDT (the sole stablecoin): always 0, since its price is fixed at ONE_USD and
     *         is never stored through batchSetPrices.
     * @param _indexToken The index token (or channel token) address to query
     * @param _id         Price ID to query
     * @return Unix timestamp (seconds) of the last update, or 0 if never set
     */
    function getLastUpdateTime(address _indexToken, uint256 _id) external view returns(uint256) {
        _indexToken = dataReader.getIndexToken(_indexToken);
        return lastUpdateTime[_indexToken][_id];
    }

    /**
     * @notice Get the latest (current priceId) price for an index token
     * @dev    Returns all zeros (indexToken resolved, prices 0) if no price has been set
     *         for the token yet (priceId == 0).
     *         USDT (the sole stablecoin): always returns the fixed price ONE_USD, since it
     *         is never stored.
     * @param _indexToken The index token (or channel token) address to query
     * @return indexToken Resolved index token address
     * @return priceAsk   Latest ask price
     * @return priceBid   Latest bid price
     * @return priceMid   Latest mid price
     */
    function getPrice(address _indexToken) public view returns(address indexToken, uint256 priceAsk, uint256 priceBid, uint256 priceMid) {
        _indexToken = dataReader.getIndexToken(_indexToken);
        uint256 _id = priceId[_indexToken];
        return _getPriceInfo(_indexToken, _id);
    }

    /**
     * @notice Get the latest ask (max) price for an index token
     * @dev    Convenience wrapper around getPrice(). Returns 0 if no price has been set
     *         for the token yet. For USDT (the sole stablecoin) returns ONE_USD.
     * @param _indexToken The index token (or channel token) address to query
     * @return Latest ask price
     */
    function getMaxPrice(address _indexToken) external view returns (uint256) {
        (, uint256 priceAsk, , ) = getPrice(_indexToken);
        return priceAsk;
    }

    /**
     * @notice Get the latest bid (min) price for an index token
     * @dev    Convenience wrapper around getPrice(). Returns 0 if no price has been set
     *         for the token yet. For USDT (the sole stablecoin) returns ONE_USD.
     * @param _indexToken The index token (or channel token) address to query
     * @return Latest bid price
     */
    function getMinPrice(address _indexToken) external view returns (uint256) {
        (, , uint256 priceBid, ) = getPrice(_indexToken);
        return priceBid;
    }

    /**
     * @notice Get the latest ask (max) price, bid (min) price and their last update time together
     * @dev    Resolves the current priceId once and returns ask/bid plus lastUpdateTime.
     *         Channel tokens are auto-resolved via DataReader.
     *         Returns 0 for ask/bid and lastUpdateTime if no price has been set yet (priceId == 0).
     *         For USDT (the sole stablecoin): _getPriceInfo synthesizes ONE_USD for ask/bid,
     *         and lastUpdateTime stays 0 since its price is never stored.
     * @param _indexToken The index token (or channel token) address to query
     * @return maxPrice  Latest ask (max) price
     * @return minPrice  Latest bid (min) price
     * @return lastUpdate Unix timestamp of the latest update, or 0 if never set
     */
    function getMaxMinPriceWithTime(address _indexToken) external view returns(uint256 maxPrice, uint256 minPrice, uint256 lastUpdate) {
        _indexToken = dataReader.getIndexToken(_indexToken);
        uint256 _id = priceId[_indexToken];
        (, maxPrice, minPrice, ) = _getPriceInfo(_indexToken, _id);
        lastUpdate = lastUpdateTime[_indexToken][_id];
    }
}