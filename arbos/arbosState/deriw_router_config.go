// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbosState

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/offchainlabs/nitro/arbos/addressSet"
	"github.com/offchainlabs/nitro/arbos/deriwpolicy"
	"github.com/offchainlabs/nitro/arbos/storage"
)

const maxDeriwRouterConfigUint64 = ^uint64(0)

// DeriwRouterConfigUpdateDelay gives validators and users time to inspect a
// proposed consensus-route change before it becomes active.
const DeriwRouterConfigUpdateDelay uint64 = 7 * 24 * 60 * 60

const (
	activeRouterOffset uint64 = iota
	activeCanonicalGatewayRouterOffset
	activeRevisionOffset
	pendingRouterOffset
	pendingCanonicalGatewayRouterOffset
	pendingActivationTimestampOffset
	pendingRevisionOffset
)

var (
	activeTokenGatewaysSubspace  SubspaceID = []byte{0}
	pendingTokenGatewaysSubspace SubspaceID = []byte{1}
)

// DeriwRouterConfigState stores both the route currently enforced by ArbSys
// and a separately staged route. A staged route is activated by the start-block
// internal transaction once its timestamp is reached.
type DeriwRouterConfigState struct {
	activeRouter                  storage.StorageBackedAddress
	activeCanonicalGatewayRouter  storage.StorageBackedAddress
	activeRevision                storage.StorageBackedUint64
	activeTokenGateways           *addressSet.AddressSet
	pendingRouter                 storage.StorageBackedAddress
	pendingCanonicalGatewayRouter storage.StorageBackedAddress
	pendingActivationTimestamp    storage.StorageBackedUint64
	pendingRevision               storage.StorageBackedUint64
	pendingTokenGateways          *addressSet.AddressSet
}

func openDeriwRouterConfigState(sto *storage.Storage) *DeriwRouterConfigState {
	return &DeriwRouterConfigState{
		activeRouter:                  sto.OpenStorageBackedAddress(activeRouterOffset),
		activeCanonicalGatewayRouter:  sto.OpenStorageBackedAddress(activeCanonicalGatewayRouterOffset),
		activeRevision:                sto.OpenStorageBackedUint64(activeRevisionOffset),
		activeTokenGateways:           addressSet.OpenAddressSet(sto.OpenCachedSubStorage(activeTokenGatewaysSubspace)),
		pendingRouter:                 sto.OpenStorageBackedAddress(pendingRouterOffset),
		pendingCanonicalGatewayRouter: sto.OpenStorageBackedAddress(pendingCanonicalGatewayRouterOffset),
		pendingActivationTimestamp:    sto.OpenStorageBackedUint64(pendingActivationTimestampOffset),
		pendingRevision:               sto.OpenStorageBackedUint64(pendingRevisionOffset),
		pendingTokenGateways:          addressSet.OpenAddressSet(sto.OpenCachedSubStorage(pendingTokenGatewaysSubspace)),
	}
}

func (config *DeriwRouterConfigState) active() (deriwpolicy.RouterOnlySendConfig, uint64, bool, error) {
	revision, err := config.activeRevision.Get()
	if err != nil || revision == 0 {
		return deriwpolicy.RouterOnlySendConfig{}, revision, false, err
	}
	router, err := config.activeRouter.Get()
	if err != nil {
		return deriwpolicy.RouterOnlySendConfig{}, 0, false, err
	}
	canonicalRouter, err := config.activeCanonicalGatewayRouter.Get()
	if err != nil {
		return deriwpolicy.RouterOnlySendConfig{}, 0, false, err
	}
	gateways, err := config.activeTokenGateways.AllMembers(deriwpolicy.MaxApprovedTokenGateways + 1)
	if err != nil {
		return deriwpolicy.RouterOnlySendConfig{}, 0, false, err
	}
	result := deriwpolicy.RouterOnlySendConfig{
		Router:                 router,
		CanonicalGatewayRouter: canonicalRouter,
		ApprovedTokenGateways:  gateways,
	}
	if err := deriwpolicy.ValidateRouterOnlySendConfig(result); err != nil {
		return deriwpolicy.RouterOnlySendConfig{}, 0, false, fmt.Errorf("invalid active Deriw router configuration: %w", err)
	}
	return result, revision, true, nil
}

func (config *DeriwRouterConfigState) pending() (deriwpolicy.RouterOnlySendConfig, uint64, uint64, bool, error) {
	activationTimestamp, err := config.pendingActivationTimestamp.Get()
	if err != nil || activationTimestamp == 0 {
		return deriwpolicy.RouterOnlySendConfig{}, 0, activationTimestamp, false, err
	}
	revision, err := config.pendingRevision.Get()
	if err != nil {
		return deriwpolicy.RouterOnlySendConfig{}, 0, 0, false, err
	}
	router, err := config.pendingRouter.Get()
	if err != nil {
		return deriwpolicy.RouterOnlySendConfig{}, 0, 0, false, err
	}
	canonicalRouter, err := config.pendingCanonicalGatewayRouter.Get()
	if err != nil {
		return deriwpolicy.RouterOnlySendConfig{}, 0, 0, false, err
	}
	gateways, err := config.pendingTokenGateways.AllMembers(deriwpolicy.MaxApprovedTokenGateways + 1)
	if err != nil {
		return deriwpolicy.RouterOnlySendConfig{}, 0, 0, false, err
	}
	result := deriwpolicy.RouterOnlySendConfig{
		Router:                 router,
		CanonicalGatewayRouter: canonicalRouter,
		ApprovedTokenGateways:  gateways,
	}
	if revision == 0 {
		return deriwpolicy.RouterOnlySendConfig{}, 0, 0, false, fmt.Errorf("pending Deriw router configuration has no revision")
	}
	if err := deriwpolicy.ValidateRouterOnlySendConfig(result); err != nil {
		return deriwpolicy.RouterOnlySendConfig{}, 0, 0, false, fmt.Errorf("invalid pending Deriw router configuration: %w", err)
	}
	return result, revision, activationTimestamp, true, nil
}

func replaceDeriwTokenGateways(target *addressSet.AddressSet, gateways []common.Address) error {
	if err := target.Clear(); err != nil {
		return err
	}
	for _, gateway := range gateways {
		if err := target.Add(gateway); err != nil {
			return err
		}
	}
	return nil
}

func (config *DeriwRouterConfigState) setActive(routes deriwpolicy.RouterOnlySendConfig, revision uint64) error {
	if revision == 0 {
		return fmt.Errorf("active Deriw router configuration revision must not be zero")
	}
	if err := deriwpolicy.ValidateRouterOnlySendConfig(routes); err != nil {
		return err
	}
	// Mark the route uninitialized until every component has been committed.
	// This fails closed if an unexpected storage error is not transactionally
	// reverted by the caller.
	if err := config.activeRevision.Set(0); err != nil {
		return err
	}
	if err := config.activeRouter.Set(routes.Router); err != nil {
		return err
	}
	if err := config.activeCanonicalGatewayRouter.Set(routes.CanonicalGatewayRouter); err != nil {
		return err
	}
	if err := replaceDeriwTokenGateways(config.activeTokenGateways, routes.ApprovedTokenGateways); err != nil {
		return err
	}
	return config.activeRevision.Set(revision)
}

func (config *DeriwRouterConfigState) clearPending() error {
	// Disarm activation before clearing the staged payload.
	if err := config.pendingActivationTimestamp.Set(0); err != nil {
		return err
	}
	if err := config.pendingRouter.Set(common.Address{}); err != nil {
		return err
	}
	if err := config.pendingCanonicalGatewayRouter.Set(common.Address{}); err != nil {
		return err
	}
	if err := replaceDeriwTokenGateways(config.pendingTokenGateways, nil); err != nil {
		return err
	}
	return config.pendingRevision.Set(0)
}

// ActiveDeriwRouterConfig returns the consensus route currently enforced by
// ArbSys. The boolean is false only before a route has been initialized.
func (state *ArbosState) ActiveDeriwRouterConfig() (deriwpolicy.RouterOnlySendConfig, uint64, bool, error) {
	return state.deriwRouterConfig.active()
}

// PendingDeriwRouterConfig returns the staged route and its activation time.
func (state *ArbosState) PendingDeriwRouterConfig() (deriwpolicy.RouterOnlySendConfig, uint64, uint64, bool, error) {
	return state.deriwRouterConfig.pending()
}

// ScheduleDeriwRouterConfig stages a complete replacement. The active route is
// deliberately untouched until the start-block hook reaches activationTime.
func (state *ArbosState) ScheduleDeriwRouterConfig(
	routes deriwpolicy.RouterOnlySendConfig,
	currentTimestamp uint64,
	activationTimestamp uint64,
) error {
	if err := deriwpolicy.ValidateRouterOnlySendConfig(routes); err != nil {
		return err
	}
	if currentTimestamp > maxDeriwRouterConfigUint64-DeriwRouterConfigUpdateDelay ||
		activationTimestamp < currentTimestamp+DeriwRouterConfigUpdateDelay {
		return fmt.Errorf(
			"Deriw router configuration must activate at least %v seconds in the future",
			DeriwRouterConfigUpdateDelay,
		)
	}

	activeRevision, err := state.deriwRouterConfig.activeRevision.Get()
	if err != nil {
		return err
	}
	if activeRevision == maxDeriwRouterConfigUint64 {
		return fmt.Errorf("Deriw router configuration revision overflow")
	}
	pendingRevision := activeRevision + 1

	// Disarm any previous proposal before overwriting its payload. The final
	// timestamp write below is the commit marker.
	if err := state.deriwRouterConfig.pendingActivationTimestamp.Set(0); err != nil {
		return err
	}
	if err := state.deriwRouterConfig.pendingRouter.Set(routes.Router); err != nil {
		return err
	}
	if err := state.deriwRouterConfig.pendingCanonicalGatewayRouter.Set(routes.CanonicalGatewayRouter); err != nil {
		return err
	}
	if err := replaceDeriwTokenGateways(state.deriwRouterConfig.pendingTokenGateways, routes.ApprovedTokenGateways); err != nil {
		return err
	}
	if err := state.deriwRouterConfig.pendingRevision.Set(pendingRevision); err != nil {
		return err
	}
	return state.deriwRouterConfig.pendingActivationTimestamp.Set(activationTimestamp)
}

func (state *ArbosState) CancelScheduledDeriwRouterConfig() error {
	return state.deriwRouterConfig.clearPending()
}

// ActivateDeriwRouterConfigIfNecessary is called from the consensus start-block
// internal transaction. Every validator therefore changes routes at the same
// block boundary without depending on a sequencer-side filter or follow-up tx.
func (state *ArbosState) ActivateDeriwRouterConfigIfNecessary(currentTimestamp uint64) error {
	routes, revision, activationTimestamp, pending, err := state.PendingDeriwRouterConfig()
	if err != nil || !pending || currentTimestamp < activationTimestamp {
		return err
	}
	if err := state.deriwRouterConfig.setActive(routes, revision); err != nil {
		return err
	}
	return state.deriwRouterConfig.clearPending()
}

// initializeDeriwRouterConfigForActivation persists the app-compiled defaults
// exactly once. A chain may instead preconfigure an active route through the
// delayed governance flow before enabling DeriwOS router-only sends.
func (state *ArbosState) initializeDeriwRouterConfigForActivation() error {
	_, _, configured, err := state.ActiveDeriwRouterConfig()
	if err != nil || configured {
		return err
	}
	chainID, err := state.ChainId()
	if err != nil {
		return err
	}
	routes, configured := deriwpolicy.RouterOnlySendConfigForChainID(chainID)
	if !configured {
		return fmt.Errorf("no bootstrap Deriw router configuration for chain ID %v", chainID)
	}
	return state.deriwRouterConfig.setActive(routes, 1)
}

func (state *ArbosState) canInitializeDeriwRouterConfig() error {
	_, _, configured, err := state.ActiveDeriwRouterConfig()
	if err != nil || configured {
		return err
	}
	chainID, err := state.ChainId()
	if err != nil {
		return err
	}
	routes, configured := deriwpolicy.RouterOnlySendConfigForChainID(chainID)
	if !configured {
		return fmt.Errorf("no bootstrap Deriw router configuration for chain ID %v", chainID)
	}
	return deriwpolicy.ValidateRouterOnlySendConfig(routes)
}
