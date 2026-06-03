// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.27;

import { IGraphPayments } from "@graphprotocol/interfaces/contracts/horizon/IGraphPayments.sol";
import { IGraphTallyCollector } from "@graphprotocol/interfaces/contracts/horizon/IGraphTallyCollector.sol";
import { IGraphToken } from "@graphprotocol/interfaces/contracts/contracts/token/IGraphToken.sol";
import { IDataService } from "@graphprotocol/interfaces/contracts/data-service/IDataService.sol";

import { Initializable } from "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";
import { Ownable2StepUpgradeable } from "@openzeppelin/contracts-upgradeable/access/Ownable2StepUpgradeable.sol";
import { UUPSUpgradeable } from "@openzeppelin/contracts-upgradeable/proxy/utils/UUPSUpgradeable.sol";
import { ReentrancyGuardUpgradeable } from "@openzeppelin/contracts-upgradeable/utils/ReentrancyGuardUpgradeable.sol";
import { DataService } from "@graphprotocol/horizon/contracts/data-service/DataService.sol";

/**
 * @title SubstreamsDataService
 * @notice A minimal Horizon data service for Substreams indexing and querying.
 * @dev Upgradeable (UUPS) so SDSCE can patch issues or extend the service. The
 * contract is deployed behind an ERC1967 proxy; the implementation constructor
 * wires the immutable Controller (via {DataService}/{GraphDirectory}) and
 * GraphTallyCollector and disables initializers, while {initialize} sets the
 * owner and provision range on the proxy.
 *
 * Upgrade authority and privileged parameters are both controlled by the
 * {OwnableUpgradeable} owner. SDSCE is unaffiliated with The Graph governance, so
 * this owner is an SDSCE-controlled address rather than the Graph governor.
 */
contract SubstreamsDataService is
    Initializable,
    Ownable2StepUpgradeable,
    UUPSUpgradeable,
    ReentrancyGuardUpgradeable,
    DataService
{
    /// @notice GraphTallyCollector address for payment collection
    IGraphTallyCollector public immutable GRAPH_TALLY_COLLECTOR;

    /// @notice Data-service cut applied on every collection, in PPM (1% = 10000).
    /// @dev SDSCE charges a fixed 1% data-service cut which is burned (deflationary).
    /// The deployer retains 0% — the entire cut received by this contract is burned.
    uint256 public constant BURN_TAX_PPM = 10_000;

    /// @notice Mapping of service provider to their registration status
    mapping(address => bool) public isRegistered;

    /// @notice Mapping of service provider to their payments destination
    mapping(address => address) public paymentsDestination;

    /// @dev Reserved storage to allow future upgrades to append state safely.
    uint256[50] private __gap;

    /// @notice Emitted when an indexer sets their payments destination
    event PaymentsDestinationSet(address indexed indexer, address indexed destination);

    /// @notice Emitted when the 1% data-service cut is burned on a collection
    event BurnTaxApplied(address indexed indexer, uint256 tokensBurned);

    /// @notice Error when indexer is not registered
    error SubstreamsDataServiceIndexerNotRegistered(address indexer);

    /// @notice Error when service provider in RAV doesn't match caller
    error SubstreamsDataServiceIndexerMismatch(address ravServiceProvider, address indexer);

    /// @notice Error when payment type is not supported
    error SubstreamsDataServiceInvalidPaymentType(IGraphPayments.PaymentTypes paymentType);

    /// @notice Error when a zero payments destination is supplied
    error SubstreamsDataServiceZeroPaymentsDestination();

    /**
     * @notice Modifier to check if indexer is registered
     */
    modifier onlyRegisteredIndexer(address indexer) {
        require(isRegistered[indexer], SubstreamsDataServiceIndexerNotRegistered(indexer));
        _;
    }

    /**
     * @notice Constructor for the implementation contract.
     * @dev Sets immutables (baked into implementation bytecode, shared across
     * upgrades) and disables initializers so the implementation cannot be
     * initialized directly — only through the proxy via {initialize}.
     * @param controller The Graph Controller address
     * @param graphTallyCollector The GraphTallyCollector address
     */
    constructor(
        address controller,
        address graphTallyCollector
    ) DataService(controller) {
        GRAPH_TALLY_COLLECTOR = IGraphTallyCollector(graphTallyCollector);
        _disableInitializers();
    }

    /**
     * @notice Initialize the proxy.
     * @param initialOwner Owner authorized to set parameters and upgrade the contract
     * @param minimumProvisionTokens Minimum tokens required for a provider provision
     */
    function initialize(
        address initialOwner,
        uint256 minimumProvisionTokens
    ) external initializer {
        __Ownable_init(initialOwner);
        __Ownable2Step_init();
        __UUPSUpgradeable_init();
        __ReentrancyGuard_init();
        __DataService_init();
        _setProvisionTokensRange(minimumProvisionTokens, type(uint256).max);
    }

    /**
     * @notice Set the provision tokens range accepted by the data service.
     * @dev Owner-only. Sets the minimum; maximum is unbounded.
     */
    function setProvisionTokensRange(
        uint256 minimumProvisionTokens
    ) external onlyOwner {
        _setProvisionTokensRange(minimumProvisionTokens, type(uint256).max);
    }

    /// @inheritdoc IDataService
    function register(
        address indexer,
        bytes calldata data
    ) external override onlyAuthorizedForProvision(indexer) onlyValidProvision(indexer) {
        (address paymentsDestination_) = abi.decode(data, (address));

        isRegistered[indexer] = true;
        _setPaymentsDestination(indexer, paymentsDestination_);

        emit ServiceProviderRegistered(indexer, data);
    }

    /// @inheritdoc IDataService
    function acceptProvisionPendingParameters(
        address indexer,
        bytes calldata
    ) external override onlyAuthorizedForProvision(indexer) {
        _acceptProvisionParameters(indexer);
        emit ProvisionPendingParametersAccepted(indexer);
    }

    /// @inheritdoc IDataService
    function startService(address, bytes calldata) external pure override {
        // No-op for Substreams - service starts implicitly on registration
    }

    /// @inheritdoc IDataService
    function stopService(address, bytes calldata) external pure override {
        // No-op for Substreams - service stops implicitly on deregistration
    }

    /// @inheritdoc IDataService
    function collect(
        address indexer,
        IGraphPayments.PaymentTypes paymentType,
        bytes calldata data
    ) external override
      nonReentrant
      onlyAuthorizedForProvision(indexer)
      onlyValidProvision(indexer)
      onlyRegisteredIndexer(indexer)
      returns (uint256)
    {
        uint256 paymentCollected = 0;

        if (paymentType == IGraphPayments.PaymentTypes.QueryFee) {
            paymentCollected = _collectQueryFees(indexer, data);
        } else {
            revert SubstreamsDataServiceInvalidPaymentType(paymentType);
        }

        emit ServicePaymentCollected(indexer, paymentType, paymentCollected);
        return paymentCollected;
    }

    /// @inheritdoc IDataService
    function slash(address, bytes calldata) external pure override {
        // Intentional no-op. SDSCE operates under a whitelist trust model rather
        // than an on-chain slashing/dispute mechanism: providers are vetted off
        // chain, and consumer risk is bounded by their escrow balance. Slashing
        // would require DisputeManager integration and a verifiable-output model
        // for substreams, which is out of scope for the Community Edition. This
        // is a deliberate trust-model decision, not a missing feature.
    }

    /**
     * @notice Set the payments destination for an indexer
     * @param paymentsDestination_ The destination address for payments
     */
    function setPaymentsDestination(address paymentsDestination_) external {
        _setPaymentsDestination(msg.sender, paymentsDestination_);
    }

    /**
     * @notice Collect query fees using GraphTallyCollector
     * @param indexer The indexer address
     * @param data Encoded SignedRAV
     * @return The amount of tokens collected
     */
    function _collectQueryFees(address indexer, bytes calldata data) private returns (uint256) {
        // The provider-supplied dataServiceCut is intentionally ignored: SDSCE
        // enforces a fixed BURN_TAX_PPM (1%) data-service cut, so collection terms
        // cannot be set by the caller.
        (IGraphTallyCollector.SignedRAV memory signedRav, ) = abi.decode(
            data,
            (IGraphTallyCollector.SignedRAV, uint256)
        );

        require(
            signedRav.rav.serviceProvider == indexer,
            SubstreamsDataServiceIndexerMismatch(signedRav.rav.serviceProvider, indexer)
        );

        // GraphPayments routes the data-service cut (BURN_TAX_PPM of the
        // post-protocol-tax amount) to this contract and the remainder to the
        // provider's payments destination. Measure what this contract receives so we
        // can burn exactly that amount.
        IGraphToken graphToken = _graphToken();
        uint256 balanceBefore = graphToken.balanceOf(address(this));

        uint256 tokensCollected = GRAPH_TALLY_COLLECTOR.collect(
            IGraphPayments.PaymentTypes.QueryFee,
            abi.encode(signedRav, BURN_TAX_PPM, paymentsDestination[indexer]),
            0 // collect full amount
        );

        // Burn the data-service cut (0% retained by the deployer).
        uint256 taxReceived = graphToken.balanceOf(address(this)) - balanceBefore;
        if (taxReceived > 0) {
            graphToken.burn(taxReceived);
            emit BurnTaxApplied(indexer, taxReceived);
        }

        return tokensCollected;
    }

    /**
     * @notice Internal function to set payments destination
     */
    function _setPaymentsDestination(address indexer, address destination) internal {
        require(destination != address(0), SubstreamsDataServiceZeroPaymentsDestination());
        paymentsDestination[indexer] = destination;
        emit PaymentsDestinationSet(indexer, destination);
    }

    /// @dev UUPS upgrade authorization — only the owner may upgrade the implementation.
    function _authorizeUpgrade(address newImplementation) internal override onlyOwner {}
}
