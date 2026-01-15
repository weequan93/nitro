// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package l1pricing

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/erigontech/erigon-lib/common/dbg"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbcompress"
	"github.com/offchainlabs/nitro/arbos/storage"
	"github.com/offchainlabs/nitro/arbos/util"
	"github.com/offchainlabs/nitro/cmd/chaininfo"
	"github.com/offchainlabs/nitro/util/arbmath"
	am "github.com/offchainlabs/nitro/util/arbmath"
)

type L1PricingState struct {
	storage *storage.Storage

	// parameters
	batchPosterTable   *BatchPostersTable
	payRewardsTo       storage.StorageBackedAddress
	equilibrationUnits storage.StorageBackedBigUint
	inertia            storage.StorageBackedUint64
	perUnitReward      storage.StorageBackedUint64
	// variables
	lastUpdateTime     storage.StorageBackedUint64 // timestamp of the last update from L1 that we processed
	fundsDueForRewards storage.StorageBackedBigInt
	// funds collected since update are recorded as the balance in account L1PricerFundsPoolAddress
	unitsSinceUpdate     storage.StorageBackedUint64  // calldata units collected for since last update
	pricePerUnit         storage.StorageBackedBigUint // current price per calldata unit
	lastSurplus          storage.StorageBackedBigInt  // introduced in ArbOS version 2
	perBatchGasCost      storage.StorageBackedInt64   // introduced in ArbOS version 3
	amortizedCostCapBips storage.StorageBackedUint64  // in basis points; introduced in ArbOS version 3
	l1FeesAvailable      storage.StorageBackedBigUint
}

var (
	BatchPosterTableKey      = []byte{0}
	BatchPosterAddress       = common.HexToAddress("0xA4B000000000000000000073657175656e636572")
	BatchPosterPayToAddress  = BatchPosterAddress
	L1PricerFundsPoolAddress = common.HexToAddress("0xA4B00000000000000000000000000000000000f6")

	ErrInvalidTime = errors.New("invalid timestamp")
	l1PricingDebug = dbg.EnvBool("ERIGON_BAD_ROOT_DEBUG", false)
)

const (
	payRewardsToOffset uint64 = iota
	equilibrationUnitsOffset
	inertiaOffset
	perUnitRewardOffset
	lastUpdateTimeOffset
	fundsDueForRewardsOffset
	unitsSinceOffset
	pricePerUnitOffset
	lastSurplusOffset
	perBatchGasCostOffset
	amortizedCostCapBipsOffset
	l1FeesAvailableOffset
)

const (
	InitialInertia            = 10
	InitialPerUnitReward      = 10
	InitialPerBatchGasCostV6  = 100_000
	InitialPerBatchGasCostV12 = 210_000 // overridden as part of the upgrade
)

// one minute at 100000 bytes / sec
var InitialEquilibrationUnitsV0 = arbmath.UintToBig(60 * params.TxDataNonZeroGasEIP2028 * 100000)
var InitialEquilibrationUnitsV6 = arbmath.UintToBig(params.TxDataNonZeroGasEIP2028 * 10000000)

func InitializeL1PricingState(sto *storage.Storage, initialRewardsRecipient common.Address, initialL1BaseFee *big.Int) error {
	bptStorage := sto.OpenCachedSubStorage(BatchPosterTableKey)
	if err := InitializeBatchPostersTable(bptStorage); err != nil {
		return err
	}
	bpTable := OpenBatchPostersTable(bptStorage)
	if _, err := bpTable.AddPoster(BatchPosterAddress, BatchPosterPayToAddress); err != nil {
		return err
	}
	if err := sto.SetByUint64(payRewardsToOffset, util.AddressToHash(initialRewardsRecipient)); err != nil {
		return err
	}
	equilibrationUnits := sto.OpenStorageBackedBigUint(equilibrationUnitsOffset)
	if err := equilibrationUnits.SetChecked(InitialEquilibrationUnitsV0); err != nil {
		return err
	}
	if err := sto.SetUint64ByUint64(inertiaOffset, InitialInertia); err != nil {
		return err
	}
	fundsDueForRewards := sto.OpenStorageBackedBigInt(fundsDueForRewardsOffset)
	if err := fundsDueForRewards.SetChecked(common.Big0); err != nil {
		return err
	}
	if err := sto.SetUint64ByUint64(perUnitRewardOffset, InitialPerUnitReward); err != nil {
		return err
	}
	pricePerUnit := sto.OpenStorageBackedBigInt(pricePerUnitOffset)
	if err := pricePerUnit.SetSaturatingWithWarning(initialL1BaseFee, "initial L1 base fee (storing in price per unit)"); err != nil {
		return err
	}
	return nil
}

func OpenL1PricingState(sto *storage.Storage) *L1PricingState {
	return &L1PricingState{
		sto,
		OpenBatchPostersTable(sto.OpenCachedSubStorage(BatchPosterTableKey)),
		sto.OpenStorageBackedAddress(payRewardsToOffset),
		sto.OpenStorageBackedBigUint(equilibrationUnitsOffset),
		sto.OpenStorageBackedUint64(inertiaOffset),
		sto.OpenStorageBackedUint64(perUnitRewardOffset),
		sto.OpenStorageBackedUint64(lastUpdateTimeOffset),
		sto.OpenStorageBackedBigInt(fundsDueForRewardsOffset),
		sto.OpenStorageBackedUint64(unitsSinceOffset),
		sto.OpenStorageBackedBigUint(pricePerUnitOffset),
		sto.OpenStorageBackedBigInt(lastSurplusOffset),
		sto.OpenStorageBackedInt64(perBatchGasCostOffset),
		sto.OpenStorageBackedUint64(amortizedCostCapBipsOffset),
		sto.OpenStorageBackedBigUint(l1FeesAvailableOffset),
	}
}

func (ps *L1PricingState) BatchPosterTable() *BatchPostersTable {
	return ps.batchPosterTable
}

func (ps *L1PricingState) PayRewardsTo() (common.Address, error) {
	return ps.payRewardsTo.Get()
}

func (ps *L1PricingState) SetPayRewardsTo(addr common.Address) error {
	return ps.payRewardsTo.Set(addr)
}

func (ps *L1PricingState) EquilibrationUnits() (*big.Int, error) {
	return ps.equilibrationUnits.Get()
}

func (ps *L1PricingState) SetEquilibrationUnits(equilUnits *big.Int) error {
	return ps.equilibrationUnits.SetChecked(equilUnits)
}

func (ps *L1PricingState) Inertia() (uint64, error) {
	return ps.inertia.Get()
}

func (ps *L1PricingState) SetInertia(inertia uint64) error {
	return ps.inertia.Set(inertia)
}

func (ps *L1PricingState) PerUnitReward() (uint64, error) {
	return ps.perUnitReward.Get()
}

func (ps *L1PricingState) SetPerUnitReward(weiPerUnit uint64) error {
	return ps.perUnitReward.Set(weiPerUnit)
}

func (ps *L1PricingState) LastUpdateTime() (uint64, error) {
	return ps.lastUpdateTime.Get()
}

func (ps *L1PricingState) SetLastUpdateTime(t uint64) error {
	return ps.lastUpdateTime.Set(t)
}

func (ps *L1PricingState) FundsDueForRewards() (*big.Int, error) {
	return ps.fundsDueForRewards.Get()
}

func (ps *L1PricingState) SetFundsDueForRewards(amt *big.Int) error {
	return ps.fundsDueForRewards.SetSaturatingWithWarning(amt, "L1 pricer funds due for rewards")

}

func (ps *L1PricingState) UnitsSinceUpdate() (uint64, error) {
	return ps.unitsSinceUpdate.Get()
}

func (ps *L1PricingState) SetUnitsSinceUpdate(units uint64) error {
	if l1PricingDebug {
		if oldUnits, err := ps.unitsSinceUpdate.Get(); err == nil {
			log.Warn("l1pricing units since update set", "old", oldUnits, "new", units)
		} else {
			log.Warn("l1pricing units since update set (read failed)", "new", units, "err", err)
		}
	}
	return ps.unitsSinceUpdate.Set(units)
}

func (ps *L1PricingState) GetL1PricingSurplus() (*big.Int, error) {
	fundsDueForRefunds, err := ps.BatchPosterTable().TotalFundsDue()
	if err != nil {
		return nil, err
	}
	fundsDueForRewards, err := ps.FundsDueForRewards()
	if err != nil {
		return nil, err
	}
	haveFunds, err := ps.L1FeesAvailable()
	if err != nil {
		return nil, err
	}
	needFunds := arbmath.BigAdd(fundsDueForRefunds, fundsDueForRewards)
	return arbmath.BigSub(haveFunds, needFunds), nil
}

func (ps *L1PricingState) LastSurplus() (*big.Int, error) {
	return ps.lastSurplus.Get()
}

func (ps *L1PricingState) SetLastSurplus(val *big.Int, arbosVersion uint64) error {
	if arbosVersion < params.ArbosVersion_7 {
		return ps.lastSurplus.Set_preVersion7(val)
	}
	return ps.lastSurplus.SetSaturatingWithWarning(val, "L1 pricer last surplus")
}

func (ps *L1PricingState) AddToUnitsSinceUpdate(units uint64) error {
	oldUnits, err := ps.unitsSinceUpdate.Get()
	if err != nil {
		return err
	}
	newUnits := oldUnits + units
	if l1PricingDebug {
		log.Warn("l1pricing units since update add", "old", oldUnits, "delta", units, "new", newUnits)
	}
	return ps.unitsSinceUpdate.Set(newUnits)
}

func (ps *L1PricingState) PricePerUnit() (*big.Int, error) {
	return ps.pricePerUnit.Get()
}

func (ps *L1PricingState) SetPricePerUnit(price *big.Int) error {
	return ps.pricePerUnit.SetChecked(price)
}

func (ps *L1PricingState) PerBatchGasCost() (int64, error) {
	return ps.perBatchGasCost.Get()
}

func (ps *L1PricingState) SetPerBatchGasCost(cost int64) error {
	return ps.perBatchGasCost.Set(cost)
}

func (ps *L1PricingState) AmortizedCostCapBips() (uint64, error) {
	return ps.amortizedCostCapBips.Get()
}

func (ps *L1PricingState) SetAmortizedCostCapBips(cap uint64) error {
	return ps.amortizedCostCapBips.Set(cap)
}

func (ps *L1PricingState) L1FeesAvailable() (*big.Int, error) {
	return ps.l1FeesAvailable.Get()
}

func (ps *L1PricingState) SetL1FeesAvailable(val *big.Int) error {
	if l1PricingDebug {
		if old, err := ps.l1FeesAvailable.Get(); err == nil {
			log.Warn("l1pricing l1 fees available set", "old", old, "new", val)
		} else {
			log.Warn("l1pricing l1 fees available set (read failed)", "new", val, "err", err)
		}
	}
	return ps.l1FeesAvailable.SetChecked(val)
}

func (ps *L1PricingState) AddToL1FeesAvailable(delta *big.Int) (*big.Int, error) {
	old, err := ps.L1FeesAvailable()
	if err != nil {
		return nil, err
	}
	new := new(big.Int).Add(old, delta)
	if l1PricingDebug {
		log.Warn("l1pricing l1 fees available add", "old", old, "delta", delta, "new", new)
	}
	if err := ps.SetL1FeesAvailable(new); err != nil {
		return nil, err
	}
	return new, nil
}

func (ps *L1PricingState) TransferFromL1FeesAvailable(
	recipient common.Address,
	amount *big.Int,
	evm *vm.EVM,
	scenario util.TracingScenario,
	purpose string,
) (*big.Int, error) {
	if err := util.TransferBalance(&L1PricerFundsPoolAddress, &recipient, amount, evm, scenario, purpose); err != nil {
		return nil, err
	}
	old, err := ps.L1FeesAvailable()
	if err != nil {
		return nil, err
	}
	updated := new(big.Int).Sub(old, amount)
	if updated.Sign() < 0 {
		return nil, core.ErrInsufficientFunds
	}
	if l1PricingDebug {
		log.Warn("l1pricing l1 fees available transfer", "old", old, "amount", amount, "new", updated, "recipient", recipient, "purpose", purpose)
	}
	if err := ps.SetL1FeesAvailable(updated); err != nil {
		return nil, err
	}
	return updated, nil
}

// UpdateForBatchPosterSpending updates the pricing model based on a payment by a batch poster
func (ps *L1PricingState) UpdateForBatchPosterSpending(
	statedb vm.StateDB,
	evm *vm.EVM,
	arbosVersion uint64,
	updateTime, currentTime uint64,
	batchPoster common.Address,
	weiSpent *big.Int,
	l1Basefee *big.Int,
	scenario util.TracingScenario,
) error {
	if l1PricingDebug {
		log.Warn("l1pricing batch poster input",
			"arbos_version", arbosVersion,
			"update_time", updateTime,
			"current_time", currentTime,
			"batch_poster", batchPoster,
			"wei_spent", weiSpent,
			"l1_base_fee", l1Basefee,
		)
	}
	if arbosVersion < params.ArbosVersion_10 {
		return ps._preversion10_UpdateForBatchPosterSpending(statedb, evm, arbosVersion, updateTime, currentTime, batchPoster, weiSpent, l1Basefee, scenario)
	}

	batchPosterTable := ps.BatchPosterTable()
	posterState, err := batchPosterTable.OpenPoster(batchPoster, true)
	if err != nil {
		return err
	}

	fundsDueForRewards, err := ps.FundsDueForRewards()
	if err != nil {
		return err
	}

	l1FeesAvailable, err := ps.L1FeesAvailable()
	if err != nil {
		return err
	}

	// compute allocation fraction -- will allocate updateTimeDelta/timeDelta fraction of units and funds to this update
	lastUpdateTime, err := ps.LastUpdateTime()
	if err != nil {
		return err
	}
	if lastUpdateTime == 0 && updateTime > 0 { // it's the first update, so there isn't a last update time
		lastUpdateTime = updateTime - 1
	}
	if updateTime > currentTime || updateTime < lastUpdateTime {
		return ErrInvalidTime
	}
	if l1PricingDebug {
		logBatchPosterState := func(phase string) {
			fields := []interface{}{
				"phase", phase,
				"batch_poster", batchPoster,
			}
			addUint64 := func(key string, val uint64, err error) {
				if err != nil {
					fields = append(fields, key+"_err", err)
					return
				}
				fields = append(fields, key, val)
			}
			addBig := func(key string, val *big.Int, err error) {
				if err != nil {
					fields = append(fields, key+"_err", err)
					return
				}
				fields = append(fields, key, val)
			}
			addAddr := func(key string, val common.Address, err error) {
				if err != nil {
					fields = append(fields, key+"_err", err)
					return
				}
				fields = append(fields, key, val)
			}
			unitsSinceUpdate, unitsSinceUpdateErr := ps.UnitsSinceUpdate()
			addUint64("units_since_update", unitsSinceUpdate, unitsSinceUpdateErr)
			lastUpdateTime, lastUpdateTimeErr := ps.LastUpdateTime()
			addUint64("last_update_time", lastUpdateTime, lastUpdateTimeErr)
			fundsDueForRewards, fundsDueForRewardsErr := ps.FundsDueForRewards()
			addBig("funds_due_for_rewards", fundsDueForRewards, fundsDueForRewardsErr)
			l1FeesAvailable, l1FeesAvailableErr := ps.L1FeesAvailable()
			addBig("l1_fees_available", l1FeesAvailable, l1FeesAvailableErr)
			pricePerUnit, pricePerUnitErr := ps.PricePerUnit()
			addBig("price_per_unit", pricePerUnit, pricePerUnitErr)
			totalFundsDue, totalFundsDueErr := batchPosterTable.TotalFundsDue()
			addBig("total_funds_due", totalFundsDue, totalFundsDueErr)
			posterFundsDue, posterFundsDueErr := posterState.FundsDue()
			addBig("poster_funds_due", posterFundsDue, posterFundsDueErr)

			payRewardsTo, payRewardsErr := ps.PayRewardsTo()
			addAddr("pay_rewards_to", payRewardsTo, payRewardsErr)
			posterPayTo, posterPayToErr := posterState.PayTo()
			addAddr("poster_pay_to", posterPayTo, posterPayToErr)

			if evm != nil && evm.StateDB != nil {
				fields = append(fields,
					"l1_pricer_balance", evm.StateDB.GetBalance(L1PricerFundsPoolAddress).String(),
					"poster_balance", evm.StateDB.GetBalance(batchPoster).String(),
				)
				if payRewardsErr == nil {
					fields = append(fields, "pay_rewards_balance", evm.StateDB.GetBalance(payRewardsTo).String())
				}
				if posterPayToErr == nil {
					fields = append(fields, "poster_pay_to_balance", evm.StateDB.GetBalance(posterPayTo).String())
				}
			}
			log.Warn("l1pricing batch poster state", fields...)
		}
		logBatchPosterState("pre")
		defer logBatchPosterState("post")
	}
	allocationNumerator := updateTime - lastUpdateTime
	allocationDenominator := currentTime - lastUpdateTime
	if allocationDenominator == 0 {
		allocationNumerator = 1
		allocationDenominator = 1
	}

	// allocate units to this update
	unitsSinceUpdate, err := ps.UnitsSinceUpdate()
	if err != nil {
		return err
	}
	unitsAllocated := am.SaturatingUMul(unitsSinceUpdate, allocationNumerator) / allocationDenominator
	unitsSinceUpdate -= unitsAllocated
	if l1PricingDebug {
		log.Warn("l1pricing batch poster allocation",
			"last_update_time", lastUpdateTime,
			"units_since_update", unitsSinceUpdate,
			"units_allocated", unitsAllocated,
			"allocation_numerator", allocationNumerator,
			"allocation_denominator", allocationDenominator,
		)
	}
	if err := ps.SetUnitsSinceUpdate(unitsSinceUpdate); err != nil {
		return err
	}

	// impose cap on amortized cost, if there is one
	if arbosVersion >= params.ArbosVersion_3 {
		amortizedCostCapBips, err := ps.AmortizedCostCapBips()
		if err != nil {
			return err
		}
		if amortizedCostCapBips != 0 {
			weiSpentCap := am.BigMulByBips(
				am.BigMulByUint(l1Basefee, unitsAllocated),
				am.SaturatingCastToBips(amortizedCostCapBips),
			)
			if am.BigLessThan(weiSpentCap, weiSpent) {
				// apply the cap on assignment of amortized cost;
				// the difference will be a loss for the batch poster
				weiSpent = weiSpentCap
				if l1PricingDebug {
					log.Warn("l1pricing batch poster cap applied",
						"cap_bips", amortizedCostCapBips,
						"wei_spent_cap", weiSpentCap,
						"wei_spent", weiSpent,
					)
				}
			}
		}
	}

	dueToPoster, err := posterState.FundsDue()
	if err != nil {
		return err
	}
	err = posterState.SetFundsDue(am.BigAdd(dueToPoster, weiSpent))
	if err != nil {
		return err
	}
	perUnitReward, err := ps.PerUnitReward()
	if err != nil {
		return err
	}
	fundsDueForRewards = am.BigAdd(fundsDueForRewards, am.BigMulByUint(am.UintToBig(unitsAllocated), perUnitReward))
	if err := ps.SetFundsDueForRewards(fundsDueForRewards); err != nil {
		return err
	}
	if l1PricingDebug {
		log.Warn("l1pricing batch poster due",
			"poster_funds_due", dueToPoster,
			"funds_due_for_rewards", fundsDueForRewards,
			"per_unit_reward", perUnitReward,
		)
	}

	// pay rewards, as much as possible
	paymentForRewards := am.BigMulByUint(am.UintToBig(perUnitReward), unitsAllocated)
	if am.BigLessThan(l1FeesAvailable, paymentForRewards) {
		paymentForRewards = l1FeesAvailable
	}
	fundsDueForRewards = am.BigSub(fundsDueForRewards, paymentForRewards)
	if err := ps.SetFundsDueForRewards(fundsDueForRewards); err != nil {
		return err
	}
	payRewardsTo, err := ps.PayRewardsTo()
	if err != nil {
		return err
	}
	if l1PricingDebug {
		log.Warn("l1pricing batch poster rewards",
			"payment_for_rewards", paymentForRewards,
			"pay_rewards_to", payRewardsTo,
		)
	}
	l1FeesAvailable, err = ps.TransferFromL1FeesAvailable(
		payRewardsTo, paymentForRewards, evm, scenario, "batchPosterReward",
	)
	if err != nil {
		return err
	}

	// settle up payments owed to the batch poster, as much as possible
	balanceDueToPoster, err := posterState.FundsDue()
	if err != nil {
		return err
	}
	balanceToTransfer := balanceDueToPoster
	if am.BigLessThan(l1FeesAvailable, balanceToTransfer) {
		balanceToTransfer = l1FeesAvailable
	}
	if balanceToTransfer.Sign() > 0 {
		addrToPay, err := posterState.PayTo()
		if err != nil {
			return err
		}
		if l1PricingDebug {
			log.Warn("l1pricing batch poster refund",
				"balance_to_transfer", balanceToTransfer,
				"poster_pay_to", addrToPay,
			)
		}
		l1FeesAvailable, err = ps.TransferFromL1FeesAvailable(
			addrToPay, balanceToTransfer, evm, scenario, "batchPosterRefund",
		)
		if err != nil {
			return err
		}
		balanceDueToPoster = am.BigSub(balanceDueToPoster, balanceToTransfer)
		err = posterState.SetFundsDue(balanceDueToPoster)
		if err != nil {
			return err
		}
	}

	// update time
	if err := ps.SetLastUpdateTime(updateTime); err != nil {
		return err
	}

	// adjust the price
	if unitsAllocated > 0 {
		totalFundsDue, err := batchPosterTable.TotalFundsDue()
		if err != nil {
			return err
		}
		fundsDueForRewards, err = ps.FundsDueForRewards()
		if err != nil {
			return err
		}
		surplus := am.BigSub(l1FeesAvailable, am.BigAdd(totalFundsDue, fundsDueForRewards))

		inertia, err := ps.Inertia()
		if err != nil {
			return err
		}
		equilUnits, err := ps.EquilibrationUnits()
		if err != nil {
			return err
		}
		inertiaUnits := am.BigDivByUint(equilUnits, inertia)
		price, err := ps.PricePerUnit()
		if err != nil {
			return err
		}

		allocPlusInert := am.BigAddByUint(inertiaUnits, unitsAllocated)
		oldSurplus, err := ps.LastSurplus()
		if err != nil {
			return err
		}

		desiredDerivative := am.BigDiv(new(big.Int).Neg(surplus), equilUnits)
		actualDerivative := am.BigDivByUint(am.BigSub(surplus, oldSurplus), unitsAllocated)
		changeDerivativeBy := am.BigSub(desiredDerivative, actualDerivative)
		priceChange := am.BigDiv(am.BigMulByUint(changeDerivativeBy, unitsAllocated), allocPlusInert)

		if err := ps.SetLastSurplus(surplus, arbosVersion); err != nil {
			return err
		}
		newPrice := am.BigAdd(price, priceChange)
		if newPrice.Sign() < 0 {
			newPrice = common.Big0
		}
		if l1PricingDebug {
			log.Warn("l1pricing batch poster price",
				"surplus", surplus,
				"old_surplus", oldSurplus,
				"price", price,
				"new_price", newPrice,
				"equil_units", equilUnits,
				"inertia", inertia,
			)
		}
		if err := ps.SetPricePerUnit(newPrice); err != nil {
			return err
		}
	}
	return nil
}

func (ps *L1PricingState) getPosterUnitsWithoutCache(tx *types.Transaction, posterAddr common.Address, brotliCompressionLevel uint64) uint64 {

	if posterAddr != BatchPosterAddress {
		return 0
	}
	txBytes, merr := tx.MarshalBinary()
	txType := tx.Type()
	if !util.TxTypeHasPosterCosts(txType) || merr != nil {
		return 0
	}

	l1Bytes, err := byteCountAfterBrotliLevel(txBytes, brotliCompressionLevel)
	if err != nil {
		panic(fmt.Sprintf("failed to compress tx: %v", err))
	}
	return l1Bytes * params.TxDataNonZeroGasEIP2028
}

// GetPosterInfo returns the poster cost and the calldata units for a transaction
func (ps *L1PricingState) GetPosterInfo(tx *types.Transaction, poster common.Address, brotliCompressionLevel uint64) (*big.Int, uint64) {
	if poster != BatchPosterAddress {
		return common.Big0, 0
	}
	var units uint64
	if cachedUnits := tx.GetCachedCalldataUnits(brotliCompressionLevel); cachedUnits != nil {
		units = *cachedUnits
	} else {
		// The cache is empty or invalid, so we need to compute the calldata units
		units = ps.getPosterUnitsWithoutCache(tx, poster, brotliCompressionLevel)
		tx.SetCachedCalldataUnits(brotliCompressionLevel, units)
	}

	// Approximate the l1 fee charged for posting this tx's calldata
	pricePerUnit, _ := ps.PricePerUnit()
	return am.BigMulByUint(pricePerUnit, units), units
}

// We don't have the full tx in gas estimation, so we assume it might be a bit bigger in practice.
const estimationPaddingUnits = 16 * params.TxDataNonZeroGasEIP2028
const estimationPaddingBasisPoints = 100

var randomNonce = binary.BigEndian.Uint64(crypto.Keccak256([]byte("Nonce"))[:8])
var randomGasTipCap = new(big.Int).SetBytes(crypto.Keccak256([]byte("GasTipCap"))[:4])
var randomGasFeeCap = new(big.Int).SetBytes(crypto.Keccak256([]byte("GasFeeCap"))[:4])
var RandomGas = uint64(binary.BigEndian.Uint32(crypto.Keccak256([]byte("Gas"))[:4]))
var randV = arbmath.BigMulByUint(chaininfo.ArbitrumOneChainConfig().ChainID, 3)
var randR = crypto.Keccak256Hash([]byte("R")).Big()
var randS = crypto.Keccak256Hash([]byte("S")).Big()

// The returned tx will be invalid, likely for a number of reasons such as an invalid signature.
// It's only used to check how large it is after brotli level 0 compression.
func makeFakeTxForMessage(message *core.Message) *types.Transaction {
	nonce := message.Nonce
	if nonce == 0 {
		nonce = randomNonce
	}
	gasTipCap := message.GasTipCap
	if gasTipCap.Sign() == 0 {
		gasTipCap = randomGasTipCap
	}
	gasFeeCap := message.GasFeeCap
	if gasFeeCap.Sign() == 0 {
		gasFeeCap = randomGasFeeCap
	}
	// During gas estimation, we don't want the gas limit variability to change the L1 cost.
	gas := message.GasLimit
	if gas == 0 || message.TxRunMode == core.MessageGasEstimationMode {
		gas = RandomGas
	}
	return types.NewTx(&types.DynamicFeeTx{
		Nonce:      nonce,
		GasTipCap:  gasTipCap,
		GasFeeCap:  gasFeeCap,
		Gas:        gas,
		To:         message.To,
		Value:      message.Value,
		Data:       message.Data,
		AccessList: message.AccessList,
		V:          randV,
		R:          randR,
		S:          randS,
	})
}

func (ps *L1PricingState) PosterDataCost(message *core.Message, poster common.Address, brotliCompressionLevel uint64) (*big.Int, uint64) {
	tx := message.Tx
	if tx != nil {
		return ps.GetPosterInfo(tx, poster, brotliCompressionLevel)
	}

	// Otherwise, we don't have an underlying transaction, so we're likely in gas estimation.
	// We'll instead make a fake tx from the message info we do have, and then pad our cost a bit to be safe.
	tx = makeFakeTxForMessage(message)
	units := ps.getPosterUnitsWithoutCache(tx, poster, brotliCompressionLevel)
	units = arbmath.UintMulByBips(units+estimationPaddingUnits, arbmath.OneInBips+estimationPaddingBasisPoints)
	pricePerUnit, _ := ps.PricePerUnit()
	return am.BigMulByUint(pricePerUnit, units), units
}

func byteCountAfterBrotliLevel(input []byte, level uint64) (uint64, error) {
	compressed, err := arbcompress.CompressLevel(input, level)
	if err != nil {
		return 0, err
	}
	return uint64(len(compressed)), nil
}
