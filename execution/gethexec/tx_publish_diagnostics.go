// Copyright 2021-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethexec

import (
	"time"

	"github.com/ethereum/go-ethereum/metrics"
)

// These histograms are intentionally enabled by the existing RPC stage
// diagnostics threshold. With the threshold disabled, the publish path only
// performs the duration check at its outer boundary and records nothing.
// Values are microseconds so short state and queue waits remain visible.
var (
	txPublishStateAtHistogram        = metrics.NewRegisteredHistogram("arb/txpublish/state_at", nil, metrics.NewBoundedHistogramSample())
	txPublishPrecheckHistogram       = metrics.NewRegisteredHistogram("arb/txpublish/precheck", nil, metrics.NewBoundedHistogramSample())
	txPublishFilterHistogram         = metrics.NewRegisteredHistogram("arb/txpublish/filter", nil, metrics.NewBoundedHistogramSample())
	txPublishPublisherHistogram      = metrics.NewRegisteredHistogram("arb/txpublish/publisher", nil, metrics.NewBoundedHistogramSample())
	txPublishForwarderTotalHistogram = metrics.NewRegisteredHistogram("arb/txpublish/forwarder_total", nil, metrics.NewBoundedHistogramSample())
	txPublishForwarderTargetHistogram = metrics.NewRegisteredHistogram("arb/txpublish/forwarder_target", nil, metrics.NewBoundedHistogramSample())
	txPublishQueueEnqueueHistogram   = metrics.NewRegisteredHistogram("arb/txpublish/queue_enqueue", nil, metrics.NewBoundedHistogramSample())
	txPublishQueueWaitHistogram      = metrics.NewRegisteredHistogram("arb/txpublish/queue_wait", nil, metrics.NewBoundedHistogramSample())
	txPublishQueuePopHistogram       = metrics.NewRegisteredHistogram("arb/txpublish/queue_pop", nil, metrics.NewBoundedHistogramSample())
	txPublishNonceRetryHistogram     = metrics.NewRegisteredHistogram("arb/txpublish/nonce_retry_wait", nil, metrics.NewBoundedHistogramSample())
	txPublishSequenceHistogram       = metrics.NewRegisteredHistogram("arb/txpublish/sequence", nil, metrics.NewBoundedHistogramSample())
	txPublishBlockExecutionHistogram = metrics.NewRegisteredHistogram("arb/txpublish/block_execution", nil, metrics.NewBoundedHistogramSample())
	txPublishConsensusWriteHistogram = metrics.NewRegisteredHistogram("arb/txpublish/consensus_write", nil, metrics.NewBoundedHistogramSample())
	txPublishBlockWriteHistogram     = metrics.NewRegisteredHistogram("arb/txpublish/block_write", nil, metrics.NewBoundedHistogramSample())
	txPublishInFlightGauge            = metrics.NewRegisteredGauge("arb/txpublish/inflight", nil)
)

type txPublishDiagnostics struct {
	threshold time.Duration
	enabled   bool
}

func newTxPublishDiagnostics(threshold time.Duration) txPublishDiagnostics {
	return txPublishDiagnostics{threshold: threshold, enabled: threshold > 0}
}

func (d txPublishDiagnostics) start() time.Time {
	if !d.enabled {
		return time.Time{}
	}
	return time.Now()
}

func (d txPublishDiagnostics) observe(hist metrics.Histogram, started time.Time) {
	if d.enabled && !started.IsZero() {
		hist.Update(time.Since(started).Microseconds())
	}
}
