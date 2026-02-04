// Copyright 2021-2024, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package arbos

import (
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/erigontech/erigon-lib/common/dbg"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/l1pricing"
	"github.com/offchainlabs/nitro/arbos/util"
	"github.com/offchainlabs/nitro/util/arbmath"
)

var internalTxDebug = dbg.EnvBool("ERIGON_BAD_ROOT_DEBUG", false)

func InternalTxStartBlock(
	chainId,
	l1BaseFee *big.Int,
	l1BlockNum uint64,
	header,
	lastHeader *types.Header,
) *types.ArbitrumInternalTx {

	l2BlockNum := header.Number.Uint64()
	timePassed := header.Time - lastHeader.Time

	if l1BaseFee == nil {
		l1BaseFee = big.NewInt(0)
	}
	data, err := util.PackInternalTxDataStartBlock(l1BaseFee, l1BlockNum, l2BlockNum, timePassed)
	if err != nil {
		panic(fmt.Sprintf("Failed to pack internal tx %v", err))
	}
	return &types.ArbitrumInternalTx{
		ChainId: chainId,
		Data:    data,
	}
}

func ApplyInternalTxUpdate(tx *types.ArbitrumInternalTx, state *arbosState.ArbosState, evm *vm.EVM) error {
	if len(tx.Data) < 4 {
		return fmt.Errorf("internal tx data is too short (only %v bytes, at least 4 required)", len(tx.Data))
	}
	selector := *(*[4]byte)(tx.Data[:4])
	switch selector {
	case InternalTxStartBlockMethodID:
		inputs, err := util.UnpackInternalTxDataStartBlock(tx.Data)
		if err != nil {
			return err
		}

		l1BlockNumber := util.SafeMapGet[uint64](inputs, "l1BlockNumber")
		l2BlockNumber := util.SafeMapGet[uint64](inputs, "l2BlockNumber")
		timePassedRaw := util.SafeMapGet[uint64](inputs, "timePassed")
		l1BaseFee := util.SafeMapGet[*big.Int](inputs, "l1BaseFee")
		timePassed := timePassedRaw
		if state.ArbOSVersion() < params.ArbosVersion_3 {
			// (incorrectly) use the L2 block number instead
			timePassed = l2BlockNumber
		}
		if state.ArbOSVersion() < params.ArbosVersion_8 {
			// in old versions we incorrectly used an L1 block number one too high
			l1BlockNumber++
		}

		oldL1BlockNumber, err := state.Blockhashes().L1BlockNumber()
		state.Restrict(err)

		l2BaseFee, err := state.L2PricingState().BaseFeeWei()
		state.Restrict(err)

		var prevHash common.Hash
		var prevHashBlock uint64
		if evm.Context.BlockNumber.Sign() > 0 {
			prevHashBlock = evm.Context.BlockNumber.Uint64() - 1
			prevHash = evm.Context.GetHash(prevHashBlock)
		}
		if internalTxDebug {
			baseFee := "<nil>"
			if evm.Context.BaseFee != nil {
				baseFee = evm.Context.BaseFee.String()
			}
			baseFeeInBlock := "<nil>"
			if evm.Context.BaseFeeInBlock != nil {
				baseFeeInBlock = evm.Context.BaseFeeInBlock.String()
			}
			log.Warn("arbos internal tx startblock",
				"block_number", evm.Context.BlockNumber,
				"block_time", evm.Context.Time,
				"header_base_fee", baseFee,
				"header_base_fee_in_block", baseFeeInBlock,
				"l2_base_fee_before", l2BaseFee,
				"l1_base_fee", l1BaseFee,
				"l1_block_number", l1BlockNumber,
				"l2_block_number", l2BlockNumber,
				"time_passed", timePassed,
				"time_passed_raw", timePassedRaw,
				"old_l1_block_number", oldL1BlockNumber,
				"prev_hash_block", prevHashBlock,
				"prev_hash", prevHash,
				"arbos_version", state.ArbOSVersion(),
			)
		}
		if l1BlockNumber > oldL1BlockNumber {
			state.Restrict(state.Blockhashes().RecordNewL1Block(l1BlockNumber-1, prevHash, state.ArbOSVersion()))
		}

		currentTime := evm.Context.Time

		if internalTxDebug {
			if backlogBefore, err := state.L2PricingState().GasBacklog(); err == nil {
				log.Warn("arbos internal tx backlog before",
					"block_number", evm.Context.BlockNumber,
					"backlog", backlogBefore,
				)
			}
		}

		// Try to reap 2 retryables
		_ = state.RetryableState().TryToReapOneRetryable(currentTime, evm, util.TracingDuringEVM)
		_ = state.RetryableState().TryToReapOneRetryable(currentTime, evm, util.TracingDuringEVM)

		state.L2PricingState().UpdatePricingModel(l2BaseFee, timePassed, false)
		if internalTxDebug {
			newBaseFee, err := state.L2PricingState().BaseFeeWei()
			if err != nil {
				log.Warn("arbos internal tx pricing base fee read failed",
					"block_number", evm.Context.BlockNumber,
					"err", err,
				)
			} else {
				backlogAfter, _ := state.L2PricingState().GasBacklog()
				log.Warn("arbos internal tx pricing update",
					"block_number", evm.Context.BlockNumber,
					"l2_base_fee_before", l2BaseFee,
					"l2_base_fee_after", newBaseFee,
					"header_base_fee", evm.Context.BaseFee,
					"time_passed", timePassed,
					"gas_backlog", backlogAfter,
				)
			}
		}

		return state.UpgradeArbosVersionIfNecessary(currentTime, evm.StateDB, evm.ChainConfig())
	case InternalTxBatchPostingReportMethodID:
		inputs, err := util.UnpackInternalTxDataBatchPostingReport(tx.Data)
		if err != nil {
			return err
		}
		batchTimestamp := util.SafeMapGet[*big.Int](inputs, "batchTimestamp")
		batchPosterAddress := util.SafeMapGet[common.Address](inputs, "batchPosterAddress")
		batchDataGas := util.SafeMapGet[uint64](inputs, "batchDataGas")
		l1BaseFeeWei := util.SafeMapGet[*big.Int](inputs, "l1BaseFeeWei")

		l1p := state.L1PricingState()
		perBatchGas, err := l1p.PerBatchGasCost()
		if err != nil {
			log.Warn("L1Pricing PerBatchGas failed", "err", err)
		}
		gasSpent := arbmath.SaturatingAdd(perBatchGas, arbmath.SaturatingCast[int64](batchDataGas))
		weiSpent := arbmath.BigMulByUint(l1BaseFeeWei, arbmath.SaturatingUCast[uint64](gasSpent))
		if internalTxDebug {
			batchTimestampStr := "<nil>"
			if batchTimestamp != nil {
				batchTimestampStr = batchTimestamp.String()
			}
			l1BaseFeeWeiStr := "<nil>"
			if l1BaseFeeWei != nil {
				l1BaseFeeWeiStr = l1BaseFeeWei.String()
			}
			l1FeesAvailableBefore := "<err>"
			if fees, feeErr := l1p.L1FeesAvailable(); feeErr == nil {
				l1FeesAvailableBefore = fees.String()
			}
			log.Warn("arbos internal tx batch posting report",
				"block_number", evm.Context.BlockNumber,
				"block_time", evm.Context.Time,
				"batch_timestamp", batchTimestampStr,
				"poster", batchPosterAddress,
				"batch_data_gas", batchDataGas,
				"per_batch_gas", perBatchGas,
				"gas_spent", gasSpent,
				"l1_base_fee_wei", l1BaseFeeWeiStr,
				"wei_spent", weiSpent,
				"l1_fees_available_before", l1FeesAvailableBefore,
				"arbos_version", state.ArbOSVersion(),
			)

			logBatchPosterAccounts := func(phase string) {
				if evm == nil {
					log.Warn("arbos internal tx batch posting accounts",
						"phase", phase,
						"poster", batchPosterAddress,
						"evm_nil", true,
					)
					return
				}
				if evm.StateDB == nil {
					log.Warn("arbos internal tx batch posting accounts",
						"phase", phase,
						"block_number", evm.Context.BlockNumber,
						"poster", batchPosterAddress,
						"state_db_nil", true,
					)
					return
				}

				fields := []interface{}{
					"phase", phase,
					"block_number", evm.Context.BlockNumber,
					"poster", batchPosterAddress,
				}
				addAccountState := func(label string, addr common.Address) {
					fields = append(fields,
						label+"_addr", addr,
						label+"_balance", evm.StateDB.GetBalance(addr).String(),
						label+"_nonce", evm.StateDB.GetNonce(addr),
						label+"_codehash", evm.StateDB.GetCodeHash(addr).Hex(),
						label+"_codesize", evm.StateDB.GetCodeSize(addr),
					)
				}

				addAccountState("l1_pricer_pool", l1pricing.L1PricerFundsPoolAddress)
				addAccountState("batch_poster", batchPosterAddress)

				payRewardsTo, payRewardsErr := l1p.PayRewardsTo()
				if payRewardsErr != nil {
					fields = append(fields, "pay_rewards_to_err", payRewardsErr)
				} else {
					addAccountState("pay_rewards_to", payRewardsTo)
				}

				posterPayTo, posterPayToErr := func() (common.Address, error) {
					batchPosterTable := l1p.BatchPosterTable()
					posterState, err := batchPosterTable.OpenPoster(batchPosterAddress, false)
					if err != nil {
						return common.Address{}, err
					}
					return posterState.PayTo()
				}()
				if posterPayToErr != nil {
					fields = append(fields, "poster_pay_to_err", posterPayToErr)
				} else {
					addAccountState("poster_pay_to", posterPayTo)
				}

				log.Warn("arbos internal tx batch posting accounts", fields...)
			}

			logBatchPosterAccounts("pre")
			defer logBatchPosterAccounts("post")
		}
		err = l1p.UpdateForBatchPosterSpending(
			evm.StateDB,
			evm,
			state.ArbOSVersion(),
			batchTimestamp.Uint64(),
			evm.Context.Time,
			batchPosterAddress,
			weiSpent,
			l1BaseFeeWei,
			util.TracingDuringEVM,
		)
		if err != nil {
			log.Warn("L1Pricing UpdateForSequencerSpending failed", "err", err)
		}
		if internalTxDebug {
			l1FeesAvailableAfter := "<err>"
			if fees, feeErr := l1p.L1FeesAvailable(); feeErr == nil {
				l1FeesAvailableAfter = fees.String()
			}
			log.Warn("arbos internal tx batch posting report applied",
				"block_number", evm.Context.BlockNumber,
				"poster", batchPosterAddress,
				"l1_fees_available_after", l1FeesAvailableAfter,
			)
		}
		return nil
	default:
		return fmt.Errorf("unknown internal tx method selector: %v", hex.EncodeToString(tx.Data[:4]))
	}
}
