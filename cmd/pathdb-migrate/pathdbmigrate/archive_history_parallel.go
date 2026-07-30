// Copyright 2023-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"context"
	"fmt"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/snappy"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/triedb"
)

type archiveTransitionJob struct {
	order       uint64
	block       uint64
	anchorBlock uint64
	parentRoot  common.Hash
	root        common.Hash
}

type archiveTransitionResult struct {
	job         archiveTransitionJob
	encoded     *encodedArchiveHistory
	spilled     *spilledArchiveHistory
	memoryBytes uint64
	err         error
}

type archiveBlockEventKind uint8

const (
	archiveBlockSkipped archiveBlockEventKind = iota
	archiveBlockSelectedAnchor
	archiveBlockSameRoot
	archiveBlockTransition
)

type archiveBlockEvent struct {
	kind            archiveBlockEventKind
	block           uint64
	root            common.Hash
	anchorBlock     uint64
	anchorRoot      common.Hash
	transitionOrder uint64
	reason          string
	err             error
}

func validateArchiveHistorySectionSize(name string, size uint64) error {
	if size > uint64(math.MaxInt) || snappy.MaxEncodedLen(int(size)) < 0 {
		return fmt.Errorf(
			"archive history %s is %d bytes and exceeds the Snappy single-block limit; choose a later archive-history.start-block after the missing-state gap",
			name,
			size,
		)
	}
	return nil
}

func validateEncodedArchiveHistory(encoded *encodedArchiveHistory) error {
	sections := []struct {
		name string
		size uint64
	}{
		{name: "account index", size: uint64(len(encoded.accountIndex))},
		{name: "storage index", size: uint64(len(encoded.storageIndex))},
		{name: "account data", size: uint64(len(encoded.accountData))},
		{name: "storage data", size: uint64(len(encoded.storageData))},
	}
	for _, section := range sections {
		if err := validateArchiveHistorySectionSize(section.name, section.size); err != nil {
			return err
		}
	}
	return nil
}

func computeArchiveTransition(
	ctx context.Context,
	src ethdb.Database,
	trieDB *triedb.Database,
	job archiveTransitionJob,
	cfg ArchiveHistoryConfig,
	dstChainData string,
	spillGate chan struct{},
) (*encodedArchiveHistory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	transitionGap := job.block - job.anchorBlock
	if transitionGap >= cfg.SpillGap {
		if spillGate != nil {
			select {
			case spillGate <- struct{}{}:
				defer func() { <-spillGate }()
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		log.Warn(
			"Large archive state gap will use a disk-backed trie diff",
			"anchorBlock", job.anchorBlock,
			"block", job.block,
			"gap", transitionGap,
			"spillGap", cfg.SpillGap,
		)
		encoded, err := archiveHistoryOriginsSpilled(
			ctx, src, trieDB, job.parentRoot, job.root, cfg, dstChainData,
		)
		if err != nil {
			return nil, err
		}
		if err := validateEncodedArchiveHistory(encoded); err != nil {
			return nil, err
		}
		return encoded, nil
	}
	accounts, storages, accountCount, slotCount, err := archiveHistoryOrigins(src, trieDB, job.parentRoot, job.root)
	if err != nil {
		return nil, err
	}
	accountIndex, storageIndex, accountData, storageData, err := encodeArchiveHistory(accounts, storages)
	if err != nil {
		return nil, err
	}
	encoded := &encodedArchiveHistory{
		accountIndex: accountIndex,
		storageIndex: storageIndex,
		accountData:  accountData,
		storageData:  storageData,
		accounts:     accountCount,
		storageSlots: slotCount,
	}
	if err := validateEncodedArchiveHistory(encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

func scanArchiveHistoryBlocks(
	ctx context.Context,
	src ethdb.Database,
	cfg ArchiveHistoryConfig,
	start uint64,
	end uint64,
	initialRoot common.Hash,
	initialAnchorBlock uint64,
	initialHaveAnchor bool,
	events chan<- archiveBlockEvent,
	jobs chan<- archiveTransitionJob,
	tokens chan struct{},
) {
	defer close(events)
	defer close(jobs)

	prevRoot := initialRoot
	anchorBlock := initialAnchorBlock
	haveAnchor := initialHaveAnchor
	var transitionOrder uint64

	sendEvent := func(event archiveBlockEvent) bool {
		select {
		case events <- event:
			return true
		case <-ctx.Done():
			return false
		}
	}
	for block := start + 1; block <= end; block++ {
		if err := ctx.Err(); err != nil {
			return
		}
		_, root, err := canonicalHeaderAndRoot(src, block)
		if err != nil {
			sendEvent(archiveBlockEvent{block: block, err: err})
			return
		}
		if !haveAnchor {
			if !archiveTrieRootAvailable(src, root) {
				if block == end {
					sendEvent(archiveBlockEvent{
						block: block,
						root:  root,
						err:   fmt.Errorf("archive-history target block %d root %s is unavailable in source hashdb and cannot be skipped", block, root),
					})
					return
				}
				if !sendEvent(archiveBlockEvent{
					kind:        archiveBlockSkipped,
					block:       block,
					root:        root,
					anchorBlock: anchorBlock,
					anchorRoot:  prevRoot,
					reason:      "state root is unavailable",
				}) {
					return
				}
				continue
			}
			haveAnchor = true
			prevRoot = root
			anchorBlock = block
			if !sendEvent(archiveBlockEvent{kind: archiveBlockSelectedAnchor, block: block, root: root}) {
				return
			}
			continue
		}
		if root == prevRoot {
			anchorBlock = block
			if !sendEvent(archiveBlockEvent{kind: archiveBlockSameRoot, block: block, root: root}) {
				return
			}
			continue
		}
		if !archiveTrieRootAvailable(src, root) {
			if !cfg.SkipMissingStates || block == end {
				sendEvent(archiveBlockEvent{
					block: block,
					root:  root,
					err:   fmt.Errorf("block %d archive state root %s is unavailable in source hashdb", block, root),
				})
				return
			}
			if !sendEvent(archiveBlockEvent{
				kind:        archiveBlockSkipped,
				block:       block,
				root:        root,
				anchorBlock: anchorBlock,
				anchorRoot:  prevRoot,
				reason:      "state root is unavailable",
			}) {
				return
			}
			continue
		}

		transitionOrder++
		select {
		case tokens <- struct{}{}:
		case <-ctx.Done():
			return
		}
		job := archiveTransitionJob{
			order:       transitionOrder,
			block:       block,
			anchorBlock: anchorBlock,
			parentRoot:  prevRoot,
			root:        root,
		}
		if !sendEvent(archiveBlockEvent{
			kind:            archiveBlockTransition,
			block:           block,
			root:            root,
			anchorBlock:     anchorBlock,
			anchorRoot:      prevRoot,
			transitionOrder: transitionOrder,
		}) {
			<-tokens
			return
		}
		select {
		case jobs <- job:
		case <-ctx.Done():
			return
		}
		prevRoot = root
		anchorBlock = block
	}
}

func (m *Migrator) runArchiveHistoryParallel(
	ctx context.Context,
	src ethdb.Database,
	dst ethdb.Database,
	freezer ethdb.AncientWriter,
	start uint64,
	end uint64,
	initialRoot common.Hash,
	initialAnchorBlock uint64,
	initialHaveAnchor bool,
	initialStateID uint64,
	initialStats archiveHistoryStats,
	started time.Time,
) (uint64, archiveHistoryStats, error) {
	cfg := m.config.ArchiveHistory
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	window := cfg.MaxInFlight
	if window == 0 {
		window = cfg.Workers
	}
	resultSpillParent, resultSpillPrefix := archiveSpillDirectory(cfg, m.config.Dst.ChainData)
	if err := os.MkdirAll(resultSpillParent, 0755); err != nil {
		return initialStateID, initialStats, fmt.Errorf("create archive result spill parent: %w", err)
	}
	resultSpillDirectory, err := os.MkdirTemp(resultSpillParent, resultSpillPrefix+"results-")
	if err != nil {
		return initialStateID, initialStats, fmt.Errorf("create archive result spill directory: %w", err)
	}
	defer os.RemoveAll(resultSpillDirectory)

	resultMemory := newArchiveResultMemoryLimiter(cfg.ResultMemoryLimit)
	var spilledResults atomic.Uint64
	var spilledResultBytes atomic.Uint64
	defer func() {
		log.Info(
			"Parallel archive result buffering summary",
			"workers", cfg.Workers,
			"maxInFlight", window,
			"resultMemoryLimitMB", cfg.ResultMemoryLimit,
			"spilledResults", spilledResults.Load(),
			"spilledBytes", spilledResultBytes.Load(),
		)
	}()

	jobs := make(chan archiveTransitionJob, window)
	results := make(chan archiveTransitionResult, window)
	events := make(chan archiveBlockEvent, window)
	tokens := make(chan struct{}, window)
	spillGate := make(chan struct{}, 1)

	var workerWG sync.WaitGroup
	for worker := 0; worker < cfg.Workers; worker++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			trieDB := triedb.NewDatabase(src, triedb.HashDefaults)
			defer trieDB.Close()
			for {
				select {
				case job, ok := <-jobs:
					if !ok {
						return
					}
					encoded, err := computeArchiveTransition(
						runCtx, src, trieDB, job, cfg, m.config.Dst.ChainData, spillGate,
					)
					result := archiveTransitionResult{job: job, err: err}
					if err == nil {
						size := encodedArchiveHistorySize(encoded)
						if resultMemory.tryAcquire(size) {
							result.encoded = encoded
							result.memoryBytes = size
						} else {
							result.spilled, result.err = spillEncodedArchiveHistory(
								resultSpillDirectory,
								job.order,
								encoded,
							)
							if result.err != nil {
								result.err = fmt.Errorf("spill completed archive transition: %w", result.err)
							} else {
								spilledResults.Add(1)
								spilledResultBytes.Add(size)
							}
							encoded = nil
						}
					}
					select {
					case results <- result:
					case <-runCtx.Done():
						return
					}
				case <-runCtx.Done():
					return
				}
			}
		}()
	}
	var scannerWG sync.WaitGroup
	scannerWG.Add(1)
	go func() {
		defer scannerWG.Done()
		scanArchiveHistoryBlocks(
			runCtx,
			src,
			cfg,
			start,
			end,
			initialRoot,
			initialAnchorBlock,
			initialHaveAnchor,
			events,
			jobs,
			tokens,
		)
	}()

	defer func() {
		cancel()
		scannerWG.Wait()
		workerWG.Wait()
	}()

	stateID := initialStateID
	stats := initialStats
	totalBlocks := end - start
	pending := make(map[uint64]archiveTransitionResult, window)

	awaitResult := func(order uint64) (archiveTransitionResult, error) {
		if result, ok := pending[order]; ok {
			delete(pending, order)
			return result, nil
		}
		for {
			select {
			case result := <-results:
				if result.job.order == order {
					return result, nil
				}
				pending[result.job.order] = result
			case <-runCtx.Done():
				return archiveTransitionResult{}, runCtx.Err()
			}
		}
	}
	releaseResult := func(result *archiveTransitionResult) {
		if err := result.release(resultMemory); err != nil {
			log.Warn(
				"Failed to remove completed archive result spill",
				"block", result.job.block,
				"order", result.job.order,
				"err", err,
			)
		}
	}
	for event := range events {
		if event.err != nil {
			return stateID, stats, event.err
		}
		stats.blocks++
		m.stats.setArchiveHistoryProgress(event.block, stateID, stats)

		switch event.kind {
		case archiveBlockSkipped:
			recordSkippedArchiveState(
				event.block,
				event.root,
				event.anchorBlock,
				event.anchorRoot,
				event.reason,
				cfg.ProgressEvery,
				&stats,
			)
		case archiveBlockSelectedAnchor:
			stats.availableBlocks++
			rawdb.WriteStateID(dst, event.root, stateID)
			log.Info(
				"Selected first available archive state",
				"block", event.block,
				"root", event.root,
				"skippedBlocks", stats.skippedBlocks,
			)
		case archiveBlockSameRoot:
			stats.availableBlocks++
			rawdb.WriteStateID(dst, event.root, stateID)
		case archiveBlockTransition:
			result, err := awaitResult(event.transitionOrder)
			<-tokens
			if err != nil {
				return stateID, stats, err
			}
			if result.err != nil {
				releaseResult(&result)
				if cfg.SkipMissingStates && isMissingArchiveState(result.err) {
					return stateID, stats, fmt.Errorf(
						"block %d parallel archive transition found missing trie data after scheduling dependent work: %w; retry with --archive-history.workers 1 or choose a later start block",
						event.block,
						result.err,
					)
				}
				return stateID, stats, fmt.Errorf("block %d archive history origins: %w", event.block, result.err)
			}
			encoded, err := result.materialize()
			if err != nil {
				releaseResult(&result)
				return stateID, stats, fmt.Errorf("block %d load archive history result: %w", event.block, err)
			}
			if encoded.accounts == 0 {
				releaseResult(&result)
				return stateID, stats, fmt.Errorf(
					"block %d root changed from %s to %s but no account changes were found",
					event.block,
					event.anchorRoot,
					event.root,
				)
			}
			stateID++
			meta := encodeArchiveHistoryMeta(event.anchorRoot, event.root, event.block)
			if err := rawdb.WriteStateHistory(
				freezer,
				stateID,
				meta,
				encoded.accountIndex,
				encoded.storageIndex,
				encoded.accountData,
				encoded.storageData,
			); err != nil {
				releaseResult(&result)
				return stateID, stats, fmt.Errorf("write archive history id %d block %d: %w", stateID, event.block, err)
			}
			rawdb.WriteStateID(dst, event.root, stateID)
			stats.transitions++
			stats.availableBlocks++
			stats.accounts += encoded.accounts
			stats.storageSlots += encoded.storageSlots
			releaseResult(&result)
		default:
			return stateID, stats, fmt.Errorf("unknown archive block event kind %d", event.kind)
		}
		m.stats.setArchiveHistoryProgress(event.block, stateID, stats)
		if shouldLogArchiveProgress(stats.blocks, cfg.ProgressEvery, event.block, end) {
			logArchiveProgress(
				"Parallel full archive history progress",
				event.block,
				end,
				event.root,
				stateID,
				totalBlocks,
				started,
				stats,
			)
		}
	}
	if err := runCtx.Err(); err != nil {
		return stateID, stats, err
	}
	return stateID, stats, nil
}
