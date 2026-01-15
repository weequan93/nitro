//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"time"

	"github.com/erigontech/erigon/db/kv"
	emdbx "github.com/erigontech/erigon/db/kv/mdbx"
	mdbxgo "github.com/erigontech/mdbx-go/mdbx"
	"github.com/ethereum/go-ethereum/metrics"
)

const mdbxMetricsInterval = 10 * time.Second

var (
	mdbxMapSizeGauge    = metrics.NewRegisteredGauge("arb/mdbx/map/size_bytes", nil)
	mdbxMapUsedGauge    = metrics.NewRegisteredGauge("arb/mdbx/map/used_bytes", nil)
	mdbxMapFreeGauge    = metrics.NewRegisteredGauge("arb/mdbx/map/free_bytes", nil)
	mdbxMapHeadroom     = metrics.NewRegisteredGaugeFloat64("arb/mdbx/map/headroom_ratio", nil)
	mdbxReadTxActive    = metrics.NewRegisteredGauge("arb/mdbx/read_tx/active", nil)
	mdbxReadTxLimit     = metrics.NewRegisteredGauge("arb/mdbx/read_tx/limit", nil)
	mdbxReadTxWaits     = metrics.NewRegisteredCounter("arb/mdbx/read_tx/waits_total", nil)
	mdbxPageFaultsTotal = metrics.NewRegisteredCounter("arb/mdbx/page_faults_total", nil)
	mdbxTxnRestarts     = metrics.NewRegisteredCounter("arb/mdbx/txn_restarts_total", nil)
)

type mdbxEnvProvider interface {
	Env() *mdbxgo.Env
}

func (c *Client) startMdbxMetrics() {
	if !metrics.Enabled {
		return
	}
	if c.metricsStop != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.metricsStop = cancel
	c.metricsDone = make(chan struct{})
	go func() {
		defer close(c.metricsDone)
		ticker := time.NewTicker(mdbxMetricsInterval)
		defer ticker.Stop()
		c.collectMdbxMetrics()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.collectMdbxMetrics()
			}
		}
	}()
}

func (c *Client) stopMdbxMetrics() {
	if c.metricsStop == nil {
		return
	}
	c.metricsStop()
	if c.metricsDone != nil {
		<-c.metricsDone
	}
	c.metricsStop = nil
	c.metricsDone = nil
}

func (c *Client) collectMdbxMetrics() {
	env := mdbxEnvFromDB(c.chainDB)
	if env == nil {
		return
	}
	info, err := env.Info(nil)
	if err != nil {
		c.logger.Warn("erigonexec: mdbx env info failed", "err", err)
		return
	}

	var mapSize uint64
	if info.MapSize > 0 {
		mapSize = uint64(info.MapSize)
	} else {
		mapSize = info.Geo.Current
	}
	var used uint64
	if info.LastPNO >= 0 {
		used = uint64(info.LastPNO+1) * uint64(info.PageSize)
		if used > mapSize {
			used = mapSize
		}
	}
	free := mapSize
	if used < mapSize {
		free = mapSize - used
	}
	headroom := 0.0
	if mapSize > 0 {
		headroom = float64(free) / float64(mapSize)
	}

	mdbxMapSizeGauge.Update(int64(mapSize))
	mdbxMapUsedGauge.Update(int64(used))
	mdbxMapFreeGauge.Update(int64(free))
	mdbxMapHeadroom.Update(headroom)
	mdbxReadTxActive.Update(int64(info.NumReaders))
	mdbxReadTxLimit.Update(int64(info.MaxReaders))

	c.metricsMu.Lock()
	if c.prefaultInit {
		if info.PageOps.Prefault >= c.lastPrefault {
			delta := info.PageOps.Prefault - c.lastPrefault
			if delta > 0 {
				mdbxPageFaultsTotal.Inc(int64(delta))
			}
		}
	} else {
		c.prefaultInit = true
	}
	c.lastPrefault = info.PageOps.Prefault
	waits := emdbx.ReadTxWaits()
	if c.lastReadTxWaits <= waits {
		mdbxReadTxWaits.Inc(int64(waits - c.lastReadTxWaits))
	} else {
		mdbxReadTxWaits.Inc(int64(waits))
	}
	c.lastReadTxWaits = waits

	restarts := emdbx.TxnRestarts()
	if c.lastTxnRestarts <= restarts {
		mdbxTxnRestarts.Inc(int64(restarts - c.lastTxnRestarts))
	} else {
		mdbxTxnRestarts.Inc(int64(restarts))
	}
	c.lastTxnRestarts = restarts
	c.metricsMu.Unlock()
}

func mdbxEnvFromDB(db kv.RwDB) *mdbxgo.Env {
	if db == nil {
		return nil
	}
	if provider, ok := db.(mdbxEnvProvider); ok {
		return provider.Env()
	}
	return nil
}
