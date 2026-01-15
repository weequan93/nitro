//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"fmt"
	"sync"

	etypes "github.com/erigontech/erigon/execution/types"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbutil"
)

type L1PriceDataOfMsg struct {
	callDataUnits            uint64
	cummulativeCallDataUnits uint64
	l1GasCharged             uint64
	cummulativeL1GasCharged  uint64
}

type L1PriceData struct {
	mutex                   sync.RWMutex
	startOfL1PriceDataCache arbutil.MessageIndex
	endOfL1PriceDataCache   arbutil.MessageIndex
	msgToL1PriceData        []L1PriceDataOfMsg
}

func NewL1PriceData() *L1PriceData {
	return &L1PriceData{
		msgToL1PriceData: []L1PriceDataOfMsg{},
	}
}

func (c *Client) backlogCallDataUnits() uint64 {
	if c.cachedL1PriceData == nil {
		return 0
	}
	c.cachedL1PriceData.mutex.RLock()
	defer c.cachedL1PriceData.mutex.RUnlock()

	size := len(c.cachedL1PriceData.msgToL1PriceData)
	if size == 0 {
		return 0
	}
	return (c.cachedL1PriceData.msgToL1PriceData[size-1].cummulativeCallDataUnits -
		c.cachedL1PriceData.msgToL1PriceData[0].cummulativeCallDataUnits +
		c.cachedL1PriceData.msgToL1PriceData[0].callDataUnits)
}

func (c *Client) backlogL1GasCharged() uint64 {
	if c.cachedL1PriceData == nil {
		return 0
	}
	c.cachedL1PriceData.mutex.RLock()
	defer c.cachedL1PriceData.mutex.RUnlock()

	size := len(c.cachedL1PriceData.msgToL1PriceData)
	if size == 0 {
		return 0
	}
	return (c.cachedL1PriceData.msgToL1PriceData[size-1].cummulativeL1GasCharged -
		c.cachedL1PriceData.msgToL1PriceData[0].cummulativeL1GasCharged +
		c.cachedL1PriceData.msgToL1PriceData[0].l1GasCharged)
}

func (c *Client) cacheL1PriceDataOfMsg(seqNum arbutil.MessageIndex, receipts etypes.Receipts, block *etypes.Block, blockBuiltUsingDelayedMessage bool) {
	if c.cachedL1PriceData == nil || block == nil {
		return
	}
	var gasUsedForL1 uint64
	var callDataUnits uint64
	if !blockBuiltUsingDelayedMessage {
		for i := 1; i < len(receipts); i++ {
			gasUsedForL1 += receipts[i].GasUsedForL1
		}
		for _, tx := range block.Transactions() {
			if arbTx, ok := tx.(*etypes.ArbTx); ok {
				callDataUnits += arbTx.CalldataUnits
			}
		}
	}
	baseFee := block.BaseFee()
	if baseFee == nil {
		return
	}
	l1GasCharged := gasUsedForL1 * baseFee.Uint64()

	c.cachedL1PriceData.mutex.Lock()
	defer c.cachedL1PriceData.mutex.Unlock()

	resetCache := func() {
		c.cachedL1PriceData.startOfL1PriceDataCache = seqNum
		c.cachedL1PriceData.endOfL1PriceDataCache = seqNum
		c.cachedL1PriceData.msgToL1PriceData = []L1PriceDataOfMsg{{
			callDataUnits:            callDataUnits,
			cummulativeCallDataUnits: callDataUnits,
			l1GasCharged:             l1GasCharged,
			cummulativeL1GasCharged:  l1GasCharged,
		}}
	}
	size := len(c.cachedL1PriceData.msgToL1PriceData)
	if size == 0 ||
		c.cachedL1PriceData.startOfL1PriceDataCache == 0 ||
		c.cachedL1PriceData.endOfL1PriceDataCache == 0 ||
		arbutil.MessageIndex(size) != c.cachedL1PriceData.endOfL1PriceDataCache-c.cachedL1PriceData.startOfL1PriceDataCache+1 {
		resetCache()
		return
	}
	if seqNum != c.cachedL1PriceData.endOfL1PriceDataCache+1 {
		if seqNum > c.cachedL1PriceData.endOfL1PriceDataCache+1 {
			c.logger.Info("erigonexec: message position higher than current l1 price data cache; resetting cache")
			resetCache()
		}
		return
	}
	cummulativeCallDataUnits := c.cachedL1PriceData.msgToL1PriceData[size-1].cummulativeCallDataUnits
	cummulativeL1GasCharged := c.cachedL1PriceData.msgToL1PriceData[size-1].cummulativeL1GasCharged
	c.cachedL1PriceData.msgToL1PriceData = append(c.cachedL1PriceData.msgToL1PriceData, L1PriceDataOfMsg{
		callDataUnits:            callDataUnits,
		cummulativeCallDataUnits: cummulativeCallDataUnits + callDataUnits,
		l1GasCharged:             l1GasCharged,
		cummulativeL1GasCharged:  cummulativeL1GasCharged + l1GasCharged,
	})
	c.cachedL1PriceData.endOfL1PriceDataCache = seqNum
}

func (c *Client) getL1PricingSurplus(ctx context.Context) (int64, error) {
	_, ibs, release, err := c.latestState(ctx)
	if err != nil {
		return 0, fmt.Errorf("erigonexec: read latest state: %w", err)
	}
	defer release()

	stateAdapter := arbos.NewStateDBAdapter(ibs, nil)
	arbState, err := arbosState.OpenSystemArbosState(stateAdapter, nil, true)
	if err != nil {
		return 0, fmt.Errorf("erigonexec: open arbos state: %w", err)
	}
	surplus, err := arbState.L1PricingState().GetL1PricingSurplus()
	if err != nil {
		return 0, fmt.Errorf("erigonexec: read l1 pricing surplus: %w", err)
	}
	return surplus.Int64(), nil
}
