/**
 ** Account-Abstraction (EIP-4337) singleton EntryPoint implementation.
 ** Only one instance required on each chain.
 **/
// SPDX-License-Identifier: GPL-3.0
pragma solidity ^0.8.12;

/* solhint-disable avoid-low-level-calls */
/* solhint-disable no-inline-assembly */
/* solhint-disable reason-string */

import "../nitro/ArbGasInfo.sol";
import "../nitro/ArbInfo.sol";
import "../nitro/ArbOwnerPublic.sol";
import "../nitro/ArbSys.sol";

import "../interfaces/IPaymaster.sol";
import "../interfaces/IEntryPoint.sol";
import "./StakeManager.sol";

contract EntryPoint is IEntryPoint, StakeManager {
    //a memory copy of UserOp fields (except that dynamic byte arrays: callData
    struct MemoryUserOp {
        address callContract;
        address paymaster;
        uint256 callGasLimit;
        uint256 verificationGasLimit;
        uint256 preVerificationGas;
        uint256 maxFeePerGas;
        uint256 maxPriorityFeePerGas;
    }

    struct UserOpInfo {
        MemoryUserOp mUserOp;
        uint256 prefund;
        uint256 contextOffset;
        uint256 preOpGas;
    }

    using UserOperationLib for UserOperation;

    // gas fee receipt address
    // solhint-disable-next-line var-name-mixedcase
    address private L1_PRICER_FUNDS_POOL_ADDRESS;
    address private INFRA_FEE_ACCOUNT;
    address private NETWORK_FEE_ACCOUNT;

    // todo use proxy upgradable
    /**
     * @param _paymasterStake - minimum required locked stake for a paymaster
     * @param _unstakeDelaySec - minimum time (in seconds) a paymaster stake must be locked
     */
    constructor(
        address _l1PriceFundsPoolAddress,
        uint256 _paymasterStake,
        uint32 _unstakeDelaySec
    ) StakeManager(_paymasterStake, _unstakeDelaySec) {
        require(
            ArbSys(address(0x64)).arbOSVersion() > 9,
            "arbOSVersion less than 9"
        );
        require(
            _l1PriceFundsPoolAddress != address(0),
            "invalid l1PriceFundsPoolAddress"
        );
        require(_unstakeDelaySec > 0, "invalid unstakeDelay");
        require(_paymasterStake > 0, "invalid paymasterStake");

        L1_PRICER_FUNDS_POOL_ADDRESS = _l1PriceFundsPoolAddress;
        INFRA_FEE_ACCOUNT = ArbOwnerPublic(address(0x6b)).getInfraFeeAccount();
        NETWORK_FEE_ACCOUNT = ArbOwnerPublic(address(0x6b))
            .getNetworkFeeAccount();

        require(
            NETWORK_FEE_ACCOUNT != address(0),
            "invalid networkFeeReceiver"
        );
    }

    /**
     * Execute a UserOperation.
     * @param op the operations to execute
     */
    function handleOp(UserOperation calldata op) public {
        UserOpInfo memory opInfo;
        _validatePrepayment(op, opInfo);

        uint256 totalCost = _executeUserOp(op, opInfo);
        _compensate(
            payable(L1_PRICER_FUNDS_POOL_ADDRESS),
            payable(INFRA_FEE_ACCOUNT),
            totalCost
        );
    }

    /**
     * compensate the caller's beneficiary address with the collected fees of UserOperation.
     * @param l1PriceFundsPoolAddress the address to receive the fees
     * @param totalCost amount to transfer.
     */
    function _compensate(
        address payable l1PriceFundsPoolAddress,
        address payable infraFeeAccount,
        uint256 totalCost
    ) internal {
        uint256 l1Fee = (totalCost * 97) / 100;
        uint256 l2Fee = totalCost - l1Fee;

        (bool ok, ) = l1PriceFundsPoolAddress.call{value: l1Fee}("");
        require(ok);

        (bool success, ) = infraFeeAccount.call{value: l2Fee}("");
        require(success);
    }

    /**
     * execute a user op, if failed execute PostOp
     * @param userOp the userOp to execute
     * @param opInfo the opInfo filled by validatePrepayment for this userOp.
     * @return collected the total amount this userOp paid.
     */
    function _executeUserOp(
        UserOperation calldata userOp,
        UserOpInfo memory opInfo
    ) private returns (uint256 collected) {
        uint256 preGas = gasleft();
        bytes memory context = getMemoryBytesFromOffset(opInfo.contextOffset);

        try this.innerHandleOp(userOp.callData, opInfo, context) returns (
            uint256 _actualGasCost
        ) {
            collected = _actualGasCost;
        } catch {
            uint256 actualGas = preGas - gasleft() + opInfo.preOpGas;
            collected = _handlePostOp(
                IPaymaster.PostOpMode.postOpReverted,
                opInfo,
                context,
                actualGas
            );
        }
    }

    /**
     * inner function to handle a UserOperation.
     * Must be declared "external" to open a call context, but it can only be called by handleOp.
     */
    function innerHandleOp(
        bytes calldata callData,
        UserOpInfo memory opInfo,
        bytes calldata context
    ) external returns (uint256 actualGasCost) {
        uint256 preGas = gasleft();
        require(msg.sender == address(this));

        IPaymaster.PostOpMode mode = IPaymaster.PostOpMode.opSucceeded;
        if (callData.length > 0) {
            address callContract = opInfo.mUserOp.callContract;
            uint256 callGasLimit = opInfo.mUserOp.callGasLimit;
            (bool success, bytes memory result) = address(callContract).call{
                gas: callGasLimit
            }(callData);
            if (!success) {
                if (result.length > 0) {
                    emit UserOperationRevertReason(msg.sender, result);
                }
                mode = IPaymaster.PostOpMode.opReverted;
            }
        }

        unchecked {
            uint256 actualGas = preGas - gasleft() + opInfo.preOpGas;
            return _handlePostOp(mode, opInfo, context, actualGas);
        }
    }

    /**
     * copy general fields from userOp into the memory opInfo structure.
     */
    function _copyUserOpToMemory(
        UserOperation calldata userOp,
        MemoryUserOp memory mUserOp
    ) internal pure {
        mUserOp.callContract = userOp.callContract;
        mUserOp.callGasLimit = userOp.callGasLimit;
        mUserOp.verificationGasLimit = userOp.verificationGasLimit;
        mUserOp.preVerificationGas = userOp.preVerificationGas;
        mUserOp.maxFeePerGas = userOp.maxFeePerGas;
        mUserOp.maxPriorityFeePerGas = userOp.maxPriorityFeePerGas;
        bytes calldata paymasterAndData = userOp.paymasterAndData;

        require(paymasterAndData.length >= 20, "invalid paymasterAndData");
        mUserOp.paymaster = address(bytes20(paymasterAndData[:20]));
    }

    /**
     * Simulate a call to paymaster.validatePaymasterUserOp.
     * Validation succeeds if the call doesn't revert.
     * @param userOp the user operation to validate.
     * @return preOpGas total gas used by validation (aka. gasUsedBeforeOperation)
     * @return prefund the amount the paymaster had to prefund
     * @return deadline until what time this userOp is valid (the minimum value of paymaster's deadline)
     */
    function simulateValidation(
        UserOperation calldata userOp
    ) external returns (uint256 preOpGas, uint256 prefund, uint256 deadline) {
        uint256 preGas = gasleft();
        UserOpInfo memory outOpInfo;

        deadline = _validatePrepayment(userOp, outOpInfo);
        prefund = outOpInfo.prefund;
        preOpGas = preGas - gasleft();

        require(
            msg.sender == address(0),
            "must be called off-chain with from=zero-addr"
        );
    }

    function simulateCallContract(
        UserOperation calldata userOp
    ) external returns (uint256 callGasUsed) {
        uint256 preGas = gasleft();

        address callContract = userOp.callContract;
        uint256 callGasLimit = userOp.callGasLimit;
        bytes memory callData = userOp.callData;
        (bool success, ) = address(callContract).call{gas: callGasLimit}(
            callData
        );
        if (!success) {
            revert("faild to execute call contract");
        }

        callGasUsed = preGas - gasleft();

        require(
            msg.sender == address(0),
            "must be called off-chain with from=zero-addr"
        );
    }

    /**
     * call paymaster.validatePaymasterUserOp.
     * validate paymaster is staked and has enough deposit.
     * revert with proper FailedOp in case paymaster reverts.
     * decrement paymaster's deposit
     */
    function _validatePaymasterPrepayment(
        UserOperation calldata op,
        UserOpInfo memory opInfo,
        uint256 requiredPreFund
    ) internal returns (bytes memory context, uint256 deadline) {
        unchecked {
            // check paymaster fund is enough
            // decrease paymaster deposit after
            MemoryUserOp memory mUserOp = opInfo.mUserOp;
            address paymaster = mUserOp.paymaster;
            DepositInfo storage paymasterInfo = deposits[paymaster];
            uint256 deposit = paymasterInfo.deposit;
            bool staked = paymasterInfo.staked;
            if (!staked) {
                revert FailedOp(paymaster, "not staked");
            }
            if (deposit < requiredPreFund) {
                revert FailedOp(paymaster, "paymaster deposit too low");
            }
            paymasterInfo.deposit = uint112(deposit - requiredPreFund);

            // verify if paymaster is willing to pay gas fee
            try
                IPaymaster(paymaster).validatePaymasterUserOp{
                    gas: mUserOp.verificationGasLimit
                }(op)
            returns (bytes memory _context, uint256 _deadline) {
                // solhint-disable-next-line not-rely-on-time
                if (_deadline != 0 && _deadline < block.timestamp) {
                    revert FailedOp(paymaster, "expired");
                }
                context = _context;
                deadline = _deadline;
            } catch Error(string memory revertReason) {
                revert FailedOp(paymaster, revertReason);
            } catch {
                revert FailedOp(paymaster, "unknown error");
            }
        }
    }

    /**
     * validate paymaster.
     * also make sure total validation doesn't exceed verificationGasLimit
     * this method is called off-chain (simulateValidation()) and on-chain (from handleOp)
     * decrement paymaster's deposit
     * @param userOp the userOp to validate
     */
    function _validatePrepayment(
        UserOperation calldata userOp,
        UserOpInfo memory outOpInfo
    ) private returns (uint256 deadline) {
        uint256 preGas = gasleft();
        MemoryUserOp memory mUserOp = outOpInfo.mUserOp;
        _copyUserOpToMemory(userOp, mUserOp);

        // validate all numeric values in userOp are well below 128 bit, so they can safely be added
        // and multiplied without causing overflow
        uint256 maxGasValues = userOp.maxFeePerGas |
            userOp.maxPriorityFeePerGas |
            userOp.callGasLimit |
            mUserOp.preVerificationGas |
            userOp.verificationGasLimit;
        require(maxGasValues <= type(uint120).max, "gas values overflow");

        // validate paymaster
        uint256 requiredPreFund = _getRequiredPrefund(mUserOp);
        bytes memory context;
        uint256 paymasterDeadline;
        (context, paymasterDeadline) = _validatePaymasterPrepayment(
            userOp,
            outOpInfo,
            requiredPreFund
        );
        if (paymasterDeadline != 0 && paymasterDeadline < deadline) {
            deadline = paymasterDeadline;
        }

        unchecked {
            uint256 gasUsed = preGas - gasleft();
            if (userOp.verificationGasLimit < gasUsed) {
                revert FailedOp(
                    mUserOp.paymaster,
                    "Used more than verificationGasLimit"
                );
            }

            outOpInfo.prefund = requiredPreFund;
            outOpInfo.contextOffset = getOffsetOfMemoryBytes(context);
            outOpInfo.preOpGas = preGas - gasleft() + userOp.preVerificationGas;
        }
    }

    /**
     * process post-operation.
     * called just after the callData is executed.
     * if a paymaster validation returned a non-empty context, its postOp is called.
     * the excess amount is refunded to the paymaster
     * @param mode - whether is called from innerHandleOp, or outside (postOpReverted)
     * @param opInfo userOp fields and info collected during validation
     * @param context the context returned in validatePaymasterUserOp
     * @param actualGas the gas used so far by this user operation
     */
    function _handlePostOp(
        IPaymaster.PostOpMode mode,
        UserOpInfo memory opInfo,
        bytes memory context,
        uint256 actualGas
    ) private returns (uint256 actualGasCost) {
        uint256 preGas = gasleft();
        unchecked {
            MemoryUserOp memory mUserOp = opInfo.mUserOp;
            uint256 gasPrice = getUserOpGasPrice(mUserOp);
            address paymaster = mUserOp.paymaster;

            if (context.length > 0) {
                actualGasCost = actualGas * gasPrice;
                if (mode != IPaymaster.PostOpMode.postOpReverted) {
                    IPaymaster(paymaster).postOp{
                        gas: mUserOp.verificationGasLimit
                    }(mode, context, actualGasCost);
                } else {
                    // solhint-disable-next-line no-empty-blocks
                    try
                        IPaymaster(paymaster).postOp{
                            gas: mUserOp.verificationGasLimit
                        }(mode, context, actualGasCost)
                    {} catch Error(string memory reason) {
                        revert FailedOp(paymaster, reason);
                    } catch {
                        revert FailedOp(paymaster, "postOp revert");
                    }
                }
            }

            actualGas += preGas - gasleft();
            actualGasCost = actualGas * gasPrice;
            if (opInfo.prefund < actualGasCost) {
                revert FailedOp(paymaster, "prefund below actualGasCost");
            }
            uint256 refund = opInfo.prefund - actualGasCost;
            internalIncrementDeposit(paymaster, refund);

            bool success = mode == IPaymaster.PostOpMode.opSucceeded;
            emit UserOperationEvent(
                msg.sender,
                mUserOp.paymaster,
                actualGasCost,
                gasPrice,
                success
            );
        } // unchecked
    }

    function _getRequiredPrefund(
        MemoryUserOp memory mUserOp
    ) internal view returns (uint256 requiredPrefund) {
        unchecked {
            // when using a Paymaster, the verificationGasLimit is used also to as a limit for the postOp call.
            // our security model might call postOp eventually twice
            // so the verificationGasLimit shoud x3 times
            uint256 mul = 3;
            uint256 requiredGas = mUserOp.callGasLimit +
                mUserOp.verificationGasLimit *
                mul +
                mUserOp.preVerificationGas;

            // TODO: copy logic of gasPrice?
            requiredPrefund = requiredGas * getUserOpGasPrice(mUserOp);
        }
    }

    /**
     * the gas price this UserOp agrees to pay.
     * relayer/miner might submit the TX with higher priorityFee, but the user should not
     */
    function getUserOpGasPrice(
        MemoryUserOp memory mUserOp
    ) internal view returns (uint256) {
        unchecked {
            uint256 maxFeePerGas = mUserOp.maxFeePerGas;
            uint256 maxPriorityFeePerGas = mUserOp.maxPriorityFeePerGas;

            if (block.basefee > 0) {
                maxFeePerGas = block.basefee;
                maxPriorityFeePerGas = 0;
            }

            return min(maxFeePerGas, maxPriorityFeePerGas + block.basefee);
        }
    }

    function min(uint256 a, uint256 b) internal pure returns (uint256) {
        return a < b ? a : b;
    }

    function getOffsetOfMemoryBytes(
        bytes memory data
    ) internal pure returns (uint256 offset) {
        assembly {
            offset := data
        }
    }

    function getMemoryBytesFromOffset(
        uint256 offset
    ) internal pure returns (bytes memory data) {
        assembly {
            data := offset
        }
    }
}
