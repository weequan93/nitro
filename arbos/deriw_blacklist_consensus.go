// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbos

import (
	"github.com/ethereum/go-ethereum/arbitrum/multigas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/blacklist"
	"github.com/offchainlabs/nitro/arbos/storage"
	arbosutil "github.com/offchainlabs/nitro/arbos/util"
)

func appendUniqueAddress(addresses []common.Address, seen map[common.Address]struct{}, address common.Address) []common.Address {
	if _, ok := seen[address]; ok {
		return addresses
	}
	seen[address] = struct{}{}
	return append(addresses, address)
}

func (p *TxProcessor) isEmergencyBlacklistRemoval() bool {
	if p.msg == nil || p.msg.Tx == nil || p.msg.To == nil || *p.msg.To != types.DeriwBlacklistAddress ||
		p.msg.From != p.originalFrom || p.msg.Value == nil || p.msg.Value.Sign() != 0 || !blacklist.IsEmergencyRemovalInput(p.msg.Data) {
		return false
	}
	if len(p.msg.SetCodeAuthorizations) != 0 {
		return false
	}
	return p.state.Blacklist().BlacklistOwner().IsMemberFree(p.originalFrom) || p.state.ChainOwners().IsMemberFree(p.originalFrom)
}

func (p *TxProcessor) incrementDeriwFailedNoopNonces() {
	if p.msg.From != p.originalFrom {
		p.evm.StateDB.SetNonce(p.msg.From, p.evm.StateDB.GetNonce(p.msg.From)+1, tracing.NonceChangeEoACall)
	}
	p.evm.StateDB.SetNonce(p.originalFrom, p.evm.StateDB.GetNonce(p.originalFrom)+1, tracing.NonceChangeEoACall)
}

// checkTopLevelDeriwBlacklist enforces the deliberately narrow DeriwOS 1
// policy. It checks only the signed sender, effective subaccount parent, and
// explicit top-level destination. For transaction types whose L1 sender is
// aliased on L2, it checks both identities. It does not inspect funding-only
// deposit/retryable-submission transactions, calldata, derived contract
// addresses, EIP-7702 targets, refund addresses, nested EVM calls, or
// protocol-generated ArbitrumInternalTx execution.
func (p *TxProcessor) checkTopLevelDeriwBlacklist(gasRemaining *uint64) (multigas.MultiGas, error) {
	if p == nil || p.state == nil || p.msg == nil || p.state.DeriwOSVersion() < arbosState.DeriwOSVersion_ConsensusBlacklist || p.MsgIsNonMutating() {
		return multigas.ZeroGas(), nil
	}
	if p.msg.Tx != nil {
		txType := p.msg.Tx.Type()
		switch txType {
		case types.ArbitrumDepositTxType, types.ArbitrumSubmitRetryableTxType, types.ArbitrumInternalTxType:
			return multigas.ZeroGas(), nil
		}
	}
	if p.isEmergencyBlacklistRemoval() {
		return multigas.ZeroGas(), nil
	}

	seen := make(map[common.Address]struct{}, 4)
	addresses := make([]common.Address, 0, 4)
	addresses = appendUniqueAddress(addresses, seen, p.originalFrom)
	addresses = appendUniqueAddress(addresses, seen, p.msg.From)
	if p.msg.To != nil {
		addresses = appendUniqueAddress(addresses, seen, *p.msg.To)
	}
	if p.msg.Tx != nil {
		txType := p.msg.Tx.Type()
		if arbosutil.DoesTxTypeAlias(&txType) {
			addresses = appendUniqueAddress(addresses, seen, arbosutil.InverseRemapL1Address(p.originalFrom))
		}
	}

	quarantined := false
	for _, address := range addresses {
		quarantined = p.state.Blacklist().IsQuarantinedFree(address) || quarantined
	}

	// The union rule reads both legacy address sets once for each unique
	// top-level participant.
	checkGas := multigas.StorageAccessReadGas(uint64(len(addresses)) * 2 * storage.StorageReadCost)
	if checkGas.SingleGas() > *gasRemaining {
		usedGas := multigas.ComputationGas(*gasRemaining)
		*gasRemaining = 0
		p.incrementDeriwFailedNoopNonces()
		if quarantined {
			p.deriwBlacklistViolation = vm.ErrDeriwBlacklisted
			return usedGas, vm.ErrDeriwBlacklisted
		}
		return usedGas, vm.ErrOutOfGas
	}

	*gasRemaining -= checkGas.SingleGas()
	if !quarantined {
		return checkGas, nil
	}

	p.deriwBlacklistViolation = vm.ErrDeriwBlacklisted
	checkGas = checkGas.SaturatingAdd(multigas.ComputationGas(*gasRemaining))
	*gasRemaining = 0
	p.incrementDeriwFailedNoopNonces()
	return checkGas, vm.ErrDeriwBlacklisted
}
