// Copyright 2023-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"

	"github.com/offchainlabs/nitro/util/dbutil"
)

type Migrator struct {
	config *Config
	stats  Stats
}

type selectedState struct {
	header *types.Header
	root   common.Hash
}

func NewMigrator(config *Config) *Migrator {
	return &Migrator{
		config: config,
	}
}

func (m *Migrator) Stats() *Stats {
	return &m.stats
}

func openChainDB(config *DBConfig, name string, readonly bool, ignoreUnfinished bool) (ethdb.Database, error) {
	db, err := node.OpenDatabase(node.InternalOpenOptions{
		DbEngine:  config.DBEngine,
		Directory: config.ChainData,
		DatabaseOptions: node.DatabaseOptions{
			AncientsDirectory:  config.ancientPath(),
			MetricsNamespace:   config.Namespace,
			Cache:              config.Cache,
			Handles:            config.Handles,
			ReadOnly:           readonly,
			PebbleExtraOptions: config.Pebble.ExtraOptions(name),
		},
	})
	if err != nil {
		return nil, err
	}
	if !ignoreUnfinished {
		err = dbutil.UnfinishedConversionCheck(db)
	}
	if err != nil {
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return nil, err
	}
	return db, nil
}

func selectState(db ethdb.Database, spec string) (*selectedState, error) {
	if strings.EqualFold(spec, "latest") || spec == "" {
		header := rawdb.ReadHeadHeader(db)
		if header == nil {
			return nil, errors.New("missing head header")
		}
		return &selectedState{header: header, root: header.Root}, nil
	}
	number, err := strconv.ParseUint(spec, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid block %q: use latest or a block number", spec)
	}
	hash := rawdb.ReadCanonicalHash(db, number)
	if hash == (common.Hash{}) {
		return nil, fmt.Errorf("missing canonical hash for block %d", number)
	}
	header := rawdb.ReadHeader(db, hash, number)
	if header == nil {
		return nil, fmt.Errorf("missing header for canonical block %d hash %s", number, hash)
	}
	return &selectedState{header: header, root: header.Root}, nil
}

func ensureHashSource(db ethdb.Database) error {
	scheme := rawdb.ReadStateScheme(db)
	if scheme != rawdb.HashScheme {
		return fmt.Errorf("source state scheme must be hash, got %q", scheme)
	}
	return nil
}

func ensureDestinationReady(db ethdb.Database, expected *selectedState) error {
	scheme := rawdb.ReadStateScheme(db)
	if scheme == rawdb.PathScheme {
		return errors.New("destination already contains pathdb state; use a fresh copy of the hash database")
	}
	state, err := selectState(db, strconv.FormatUint(expected.header.Number.Uint64(), 10))
	if err != nil {
		return fmt.Errorf("destination selected block mismatch: %w", err)
	}
	if state.header.Hash() != expected.header.Hash() {
		return fmt.Errorf(
			"destination block hash mismatch at %d: have %s want %s",
			expected.header.Number.Uint64(),
			state.header.Hash(),
			expected.header.Hash(),
		)
	}
	if state.root != expected.root {
		return fmt.Errorf("destination state root mismatch: have %s want %s", state.root, expected.root)
	}
	return nil
}

func ensureSelectedStateStillCanonical(db ethdb.Database, expected *selectedState, label string) error {
	current, err := selectState(db, strconv.FormatUint(expected.header.Number.Uint64(), 10))
	if err != nil {
		return fmt.Errorf("recheck %s selected block: %w", label, err)
	}
	if current.header.Hash() != expected.header.Hash() || current.root != expected.root {
		return fmt.Errorf(
			"%s selected state changed during conversion at block %d: selectedBlock=%s canonicalBlock=%s selectedRoot=%s canonicalRoot=%s",
			label,
			expected.header.Number.Uint64(),
			expected.header.Hash(),
			current.header.Hash(),
			expected.root,
			current.root,
		)
	}
	return nil
}

func (m *Migrator) Run(ctx context.Context) error {
	if m.config.FindStateRoot != "" {
		return m.findStateRoot(ctx)
	}
	if m.config.CompareDatabases {
		return m.compareDatabases(ctx)
	}
	if m.config.RepairPathState {
		return m.repairPathState(ctx)
	}
	if m.config.AccountHistory.Enable {
		return m.runAccountHistory(ctx)
	}
	if m.config.ArchiveHistory.Enable {
		return m.runArchiveHistory(ctx)
	}
	if m.config.VerifyOnly {
		dst, err := openChainDB(&m.config.Dst, "dst", false, m.config.IgnoreUnfinished)
		if err != nil {
			return err
		}
		defer dst.Close()
		state, err := selectState(dst, m.config.Block)
		if err != nil {
			return err
		}
		log.Info(
			"Selected destination state for verification",
			"number", state.header.Number.Uint64(),
			"block", state.header.Hash(),
			"root", state.root,
		)
		if err := VerifyPathState(ctx, dst, state.root); err != nil {
			return err
		}
		if m.config.CleanupLegacy || m.config.StrictCleanup {
			if err := m.cleanupLegacyHashState(ctx, dst); err != nil {
				return err
			}
		}
		if m.config.StrictCleanup {
			if err := m.cleanupHashdbSnapshots(ctx, dst); err != nil {
				return err
			}
		}
		if m.config.Compact {
			if err := compactDestination(dst); err != nil {
				return err
			}
		}
		if m.config.IgnoreUnfinished {
			if err := dbutil.DeleteUnfinishedConversionCanary(dst); err != nil {
				return err
			}
			if err := dst.SyncKeyValue(); err != nil {
				return err
			}
			log.Info("Deleted unfinished conversion canary after successful verification")
		}
		return nil
	}

	src, err := openChainDB(&m.config.Src, "src", true, false)
	if err != nil {
		return err
	}
	defer src.Close()

	if err := ensureHashSource(src); err != nil {
		return err
	}
	state, err := selectState(src, m.config.Block)
	if err != nil {
		return err
	}
	log.Info(
		"Selected state",
		"number", state.header.Number.Uint64(),
		"block", state.header.Hash(),
		"root", state.root,
		"stateWorkers", m.config.StateWorkers,
		"stateMaxInFlight", m.config.StateMaxInFlight,
	)

	if !m.config.Migrate {
		log.Info("Dry-run only: traversing source hash state without writing pathdb data")
		m.stats.Reset()
		return m.convertState(ctx, src, nil, state.root, false)
	}

	dst, err := openChainDB(&m.config.Dst, "dst", false, false)
	if err != nil {
		return err
	}
	defer dst.Close()
	if err := ensureDestinationReady(dst, state); err != nil {
		return err
	}

	m.stats.Reset()
	if err := dbutil.PutUnfinishedConversionCanary(dst); err != nil {
		return err
	}
	err = m.convertState(ctx, src, dst, state.root, true)
	if err == nil {
		err = ensureSelectedStateStillCanonical(src, state, "source")
	}
	if err == nil {
		err = ensureSelectedStateStillCanonical(dst, state, "destination")
	}
	if err == nil {
		err = m.writePathMetadata(dst, state.root)
	}
	if err == nil {
		err = dst.SyncKeyValue()
	}
	if err != nil {
		return err
	}
	log.Info(
		"Pathdb migration finished",
		"accountNodes", m.stats.AccountNodes(),
		"accountLeaves", m.stats.AccountLeaves(),
		"storageTries", m.stats.StorageTries(),
		"storageNodes", m.stats.StorageNodes(),
		"storageLeaves", m.stats.StorageLeaves(),
		"MB", m.stats.Bytes()/1024/1024,
		"batches", m.stats.Batches(),
		"elapsed", m.stats.Elapsed(),
	)
	if m.config.Verify {
		if err := VerifyPathState(ctx, dst, state.root); err != nil {
			return err
		}
	}
	if m.config.CleanupLegacy || m.config.StrictCleanup {
		if err := m.cleanupLegacyHashState(ctx, dst); err != nil {
			return err
		}
	}
	if m.config.StrictCleanup {
		if err := m.cleanupHashdbSnapshots(ctx, dst); err != nil {
			return err
		}
	}
	if m.config.Compact {
		if err := compactDestination(dst); err != nil {
			return err
		}
	}
	if err := dbutil.DeleteUnfinishedConversionCanary(dst); err != nil {
		return err
	}
	if err := dst.SyncKeyValue(); err != nil {
		return err
	}
	return nil
}

// repairPathState replaces only the destination PathDB trie layer and its
// current-root metadata. The source and destination canonical block must match,
// and the destination must not contain state history. A failed repair leaves
// the unfinished-conversion canary in place so the database cannot be started
// accidentally before a successful retry or restore from backup.
func (m *Migrator) repairPathState(ctx context.Context) error {
	src, err := openChainDB(&m.config.Src, "src-repair", true, false)
	if err != nil {
		return err
	}
	defer src.Close()
	if err := ensureHashSource(src); err != nil {
		return err
	}
	state, err := selectState(src, m.config.Block)
	if err != nil {
		return err
	}

	dst, err := openChainDB(&m.config.Dst, "dst-repair", false, m.config.IgnoreUnfinished)
	if err != nil {
		return err
	}
	defer dst.Close()
	if scheme := rawdb.ReadStateScheme(dst); scheme != rawdb.PathScheme {
		return fmt.Errorf("destination state scheme must be path, got %q", scheme)
	}
	dstState, err := selectState(dst, m.config.Block)
	if err != nil {
		return fmt.Errorf("destination selected block: %w", err)
	}
	if dstState.header.Hash() != state.header.Hash() || dstState.root != state.root {
		return fmt.Errorf(
			"source and destination canonical state differ at block %d: sourceBlock=%s destinationBlock=%s sourceRoot=%s destinationRoot=%s",
			state.header.Number.Uint64(), state.header.Hash(), dstState.header.Hash(), state.root, dstState.root,
		)
	}

	currentRoot := pathAccountRoot(dst)
	log.Info("PathDB repair preflight",
		"block", state.header.Number.Uint64(),
		"canonicalRoot", state.root.Hex(),
		"currentPathRoot", currentRoot.Hex(),
		"stateWorkers", m.config.StateWorkers,
		"stateMaxInFlight", m.config.StateMaxInFlight)
	if currentRoot == state.root && !m.config.ForceRepairPathState {
		log.Info("Destination PathDB already matches the selected canonical state")
		if err := VerifyPathState(ctx, dst, state.root); err == nil {
			if m.config.IgnoreUnfinished {
				if err := dbutil.DeleteUnfinishedConversionCanary(dst); err != nil {
					return err
				}
				if err := dst.SyncKeyValue(); err != nil {
					return err
				}
				log.Info("Deleted unfinished conversion canary after successful repair verification")
			}
			return nil
		} else {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			log.Warn("Destination PathDB root matches but full verification failed; rewriting the selected canonical state", "err", err)
		}
	} else if currentRoot == state.root {
		log.Warn("Skipping initial destination verification and forcing PathDB state rewrite")
	}
	if rawdb.ReadPersistentStateID(dst) != 0 {
		return errors.New("destination persistent state ID is nonzero; refusing to replace a PathDB with existing history")
	}
	ancientDir, err := dst.AncientDatadir()
	if err != nil {
		return fmt.Errorf("destination ancient directory: %w", err)
	}
	freezer, err := rawdb.NewStateFreezer(ancientDir, false, false)
	if err != nil {
		return fmt.Errorf("open destination state freezer: %w", err)
	}
	frozen, frozenErr := freezer.Ancients()
	closeErr := freezer.Close()
	if frozenErr != nil {
		return fmt.Errorf("read destination state history size: %w", frozenErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close destination state freezer: %w", closeErr)
	}
	if frozen != 0 {
		return fmt.Errorf("destination contains %d state history entries; refusing in-place PathDB repair", frozen)
	}

	m.stats.Reset()
	if err := dbutil.PutUnfinishedConversionCanary(dst); err != nil {
		return err
	}
	log.Info("PathDB state rewrite started",
		"block", state.header.Number.Uint64(),
		"root", state.root,
		"stateWorkers", m.config.StateWorkers,
		"stateMaxInFlight", m.config.StateMaxInFlight,
		"idealBatchSize", m.config.IdealBatchSize)
	if err := m.convertState(ctx, src, dst, state.root, true); err != nil {
		return fmt.Errorf("rewrite destination PathDB trie: %w", err)
	}
	if err := ensureSelectedStateStillCanonical(src, state, "source"); err != nil {
		return err
	}
	if err := ensureSelectedStateStillCanonical(dst, state, "destination"); err != nil {
		return err
	}
	if err := m.writePathMetadata(dst, state.root); err != nil {
		return fmt.Errorf("write repaired PathDB metadata: %w", err)
	}
	if err := dst.SyncKeyValue(); err != nil {
		return fmt.Errorf("sync repaired PathDB: %w", err)
	}
	if err := VerifyPathState(ctx, dst, state.root); err != nil {
		return fmt.Errorf("verify repaired PathDB: %w", err)
	}
	if err := dbutil.DeleteUnfinishedConversionCanary(dst); err != nil {
		return err
	}
	if err := dst.SyncKeyValue(); err != nil {
		return err
	}
	log.Info("PathDB repair completed",
		"block", state.header.Number.Uint64(),
		"root", state.root.Hex(),
		"accountNodes", m.stats.AccountNodes(),
		"storageNodes", m.stats.StorageNodes(),
		"MB", m.stats.Bytes()/1024/1024,
		"elapsed", m.stats.Elapsed())
	return nil
}

// compareDatabases compares canonical headers in two offline chain databases.
// It is useful for proving whether a converted destination came from the same
// snapshot as its source; neither database is opened for writing.
func (m *Migrator) compareDatabases(ctx context.Context) error {
	src, err := openChainDB(&m.config.Src, "src-compare", true, false)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := openChainDB(&m.config.Dst, "dst-compare", true, true)
	if err != nil {
		return err
	}
	defer dst.Close()
	srcHead := rawdb.ReadHeadHeader(src)
	if srcHead == nil {
		return errors.New("missing source head header")
	}
	dstHead := rawdb.ReadHeadHeader(dst)
	if dstHead == nil {
		return errors.New("missing destination head header")
	}
	srcEnd, err := selectState(src, m.config.CompareEnd)
	if err != nil {
		return err
	}
	dstEnd, err := selectState(dst, m.config.CompareEnd)
	if err != nil {
		return err
	}
	end := srcEnd.header.Number.Uint64()
	if dstEnd.header.Number.Uint64() < end {
		end = dstEnd.header.Number.Uint64()
	}
	if m.config.CompareStart > end {
		return fmt.Errorf("compare-start-block %d is greater than end block %d", m.config.CompareStart, end)
	}
	dstPathRoot := pathAccountRoot(dst)
	var (
		pathRootBlock uint64
		pathRootFound bool
		lastSrcRoot   common.Hash
		lastDstRoot   common.Hash
	)
	for number := m.config.CompareStart; number <= end; number++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		srcHash := rawdb.ReadCanonicalHash(src, number)
		dstHash := rawdb.ReadCanonicalHash(dst, number)
		if srcHash != dstHash {
			return fmt.Errorf("first canonical mismatch at block %d: source=%s destination=%s", number, srcHash, dstHash)
		}
		if srcHash == (common.Hash{}) {
			return fmt.Errorf("missing canonical block %d in source and/or destination", number)
		}
		srcHeader := rawdb.ReadHeader(src, srcHash, number)
		dstHeader := rawdb.ReadHeader(dst, dstHash, number)
		if srcHeader == nil || dstHeader == nil {
			return fmt.Errorf("missing header at block %d", number)
		}
		if srcHeader.Root != dstHeader.Root {
			return fmt.Errorf("first state-root mismatch at block %d: source=%s destination=%s block=%s", number, srcHeader.Root, dstHeader.Root, srcHash)
		}
		lastSrcRoot = srcHeader.Root
		lastDstRoot = dstHeader.Root
		if srcHeader.Root == dstPathRoot {
			pathRootBlock = number
			pathRootFound = true
		}
		if number%10000 == 0 {
			log.Info("Compared canonical headers", "number", number, "end", end)
		}
	}
	log.Info("Source and destination canonical headers match", "start", m.config.CompareStart, "end", end,
		"sourceHeaderRoot", lastSrcRoot.Hex(),
		"destinationHeaderRoot", lastDstRoot.Hex(),
		"destinationPathRoot", dstPathRoot.Hex(),
		"pathRootFound", pathRootFound,
		"pathRootBlock", pathRootBlock,
		"sourceHead", srcHead.Number.Uint64(),
		"destinationHead", dstHead.Number.Uint64(),
		"sourceHeadRoot", srcHead.Root.Hex(),
		"destinationHeadRoot", dstHead.Root.Hex())
	return nil
}

// findStateRoot scans canonical headers in a read-only source database. It is
// intentionally separate from migration so operators can recover the exact
// end block for an already-converted destination without running an RPC node.
func (m *Migrator) findStateRoot(ctx context.Context) error {
	src, err := openChainDB(&m.config.Src, "src-scan", true, false)
	if err != nil {
		return err
	}
	defer src.Close()

	endState, err := selectState(src, m.config.FindEndBlock)
	if err != nil {
		return err
	}
	start := m.config.FindStartBlock
	end := endState.header.Number.Uint64()
	if start > end {
		return fmt.Errorf("find-start-block %d is greater than end block %d", start, end)
	}
	target := common.HexToHash(m.config.FindStateRoot)
	for number := start; number <= end; number++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		hash := rawdb.ReadCanonicalHash(src, number)
		if hash != (common.Hash{}) {
			header := rawdb.ReadHeader(src, hash, number)
			if header != nil && header.Root == target {
				log.Info("Found state root", "number", number, "block", hash, "root", target)
				return nil
			}
		}
		if number%10000 == 0 {
			log.Info("Scanning canonical headers", "number", number, "end", end)
		}
	}
	return fmt.Errorf("state root %s not found in canonical blocks %d-%d", target, start, end)
}

func (m *Migrator) convertState(ctx context.Context, src ethdb.Database, dst ethdb.Database, root common.Hash, write bool) error {
	srcTrieDB := triedb.NewDatabase(src, triedb.HashDefaults)
	defer srcTrieDB.Close()

	if root != types.EmptyRootHash && !rawdb.HasLegacyTrieNode(src, root) {
		return fmt.Errorf("source is missing legacy trie root %s", root)
	}
	accountTrie, err := trie.New(trie.StateTrieID(root), srcTrieDB)
	if err != nil {
		return fmt.Errorf("open source account trie %s: %w", root, err)
	}
	writer := newPathWriter(dst, m.config.IdealBatchSize, write, &m.stats)
	if err := m.copyAccountTrie(ctx, writer, srcTrieDB, accountTrie, root); err != nil {
		return err
	}
	return writer.Flush()
}

func (m *Migrator) copyAccountTrie(ctx context.Context, writer *pathWriter, srcTrieDB *triedb.Database, accountTrie *trie.Trie, stateRoot common.Hash) error {
	if m.config.StateWorkers == 1 {
		return m.copyAccountTrieSequential(ctx, writer, srcTrieDB, accountTrie, stateRoot)
	}
	return m.copyAccountTrieParallel(ctx, writer, srcTrieDB, accountTrie, stateRoot)
}

func (m *Migrator) copyAccountTrieSequential(ctx context.Context, writer *pathWriter, srcTrieDB *triedb.Database, accountTrie *trie.Trie, stateRoot common.Hash) error {
	it, err := accountTrie.NodeIterator(nil)
	if err != nil {
		return err
	}
	for it.Next(true) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if blob := it.NodeBlob(); len(blob) > 0 {
			if err := writer.WriteAccountNode(common.CopyBytes(it.Path()), common.CopyBytes(blob)); err != nil {
				return err
			}
			m.stats.accountNodes.Add(1)
			m.stats.bytes.Add(uint64(len(blob)))
		}
		if !it.Leaf() {
			continue
		}
		m.stats.accountLeaves.Add(1)
		accountHashBytes := common.CopyBytes(it.LeafKey())
		if len(accountHashBytes) != common.HashLength {
			return fmt.Errorf("unexpected account trie leaf key length %d", len(accountHashBytes))
		}
		var account types.StateAccount
		if err := rlp.DecodeBytes(common.CopyBytes(it.LeafBlob()), &account); err != nil {
			return fmt.Errorf("decode account leaf %x: %w", accountHashBytes, err)
		}
		if account.Root == types.EmptyRootHash {
			continue
		}
		accountHash := common.BytesToHash(accountHashBytes)
		if err := m.copyStorageTrie(ctx, writer, srcTrieDB, stateRoot, accountHash, account.Root); err != nil {
			return err
		}
	}
	if err := it.Error(); err != nil {
		return fmt.Errorf("iterate account trie: %w", err)
	}
	return nil
}

type storageTrieJob struct {
	accountHash common.Hash
	storageRoot common.Hash
}

func (m *Migrator) copyAccountTrieParallel(ctx context.Context, writer *pathWriter, srcTrieDB *triedb.Database, accountTrie *trie.Trie, stateRoot common.Hash) error {
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	maxInFlight := m.config.StateMaxInFlight
	if maxInFlight == 0 {
		maxInFlight = m.config.StateWorkers
	}
	queueSize := maxInFlight - m.config.StateWorkers
	jobs := make(chan storageTrieJob, queueSize)
	var (
		workers   sync.WaitGroup
		errOnce   sync.Once
		workerErr error
	)
	recordWorkerError := func(err error) {
		errOnce.Do(func() {
			workerErr = err
			cancel()
		})
	}

	for i := 0; i < m.config.StateWorkers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			storageWriter := writer.sibling()
			for job := range jobs {
				if err := workerCtx.Err(); err != nil {
					return
				}
				m.stats.activeStorageWorkers.Add(1)
				err := m.copyStorageTrie(workerCtx, storageWriter, srcTrieDB, stateRoot, job.accountHash, job.storageRoot)
				m.stats.activeStorageWorkers.Add(^uint64(0))
				if err != nil {
					recordWorkerError(err)
					return
				}
				m.stats.storageJobsCompleted.Add(1)
			}
			if err := storageWriter.Flush(); err != nil {
				recordWorkerError(err)
			}
		}()
	}

	producerErr := m.copyAccountTrieAndScheduleStorage(workerCtx, writer, accountTrie, jobs)
	if producerErr != nil {
		cancel()
	}
	close(jobs)
	workers.Wait()
	if workerErr != nil {
		return workerErr
	}
	if producerErr != nil {
		if errors.Is(producerErr, context.Canceled) && ctx.Err() != nil {
			return ctx.Err()
		}
		return producerErr
	}
	return nil
}

func (m *Migrator) copyAccountTrieAndScheduleStorage(ctx context.Context, writer *pathWriter, accountTrie *trie.Trie, jobs chan<- storageTrieJob) error {
	it, err := accountTrie.NodeIterator(nil)
	if err != nil {
		return err
	}
	for it.Next(true) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if blob := it.NodeBlob(); len(blob) > 0 {
			if err := writer.WriteAccountNode(common.CopyBytes(it.Path()), common.CopyBytes(blob)); err != nil {
				return err
			}
			m.stats.accountNodes.Add(1)
			m.stats.bytes.Add(uint64(len(blob)))
		}
		if !it.Leaf() {
			continue
		}
		m.stats.accountLeaves.Add(1)
		accountHashBytes := common.CopyBytes(it.LeafKey())
		if len(accountHashBytes) != common.HashLength {
			return fmt.Errorf("unexpected account trie leaf key length %d", len(accountHashBytes))
		}
		var account types.StateAccount
		if err := rlp.DecodeBytes(common.CopyBytes(it.LeafBlob()), &account); err != nil {
			return fmt.Errorf("decode account leaf %x: %w", accountHashBytes, err)
		}
		if account.Root == types.EmptyRootHash {
			continue
		}
		job := storageTrieJob{
			accountHash: common.BytesToHash(accountHashBytes),
			storageRoot: account.Root,
		}
		m.stats.storageJobsScheduled.Add(1)
		select {
		case jobs <- job:
		case <-ctx.Done():
			m.stats.storageJobsScheduled.Add(^uint64(0))
			return ctx.Err()
		}
	}
	if err := it.Error(); err != nil {
		return fmt.Errorf("iterate account trie: %w", err)
	}
	return nil
}

func (m *Migrator) copyStorageTrie(ctx context.Context, writer *pathWriter, srcTrieDB *triedb.Database, stateRoot common.Hash, accountHash common.Hash, storageRoot common.Hash) error {
	m.stats.storageTries.Add(1)
	storageTrie, err := trie.New(trie.StorageTrieID(stateRoot, accountHash, storageRoot), srcTrieDB)
	if err != nil {
		return fmt.Errorf("open source storage trie account %s root %s: %w", accountHash, storageRoot, err)
	}
	it, err := storageTrie.NodeIterator(nil)
	if err != nil {
		return err
	}
	for it.Next(true) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if blob := it.NodeBlob(); len(blob) > 0 {
			if err := writer.WriteStorageNode(accountHash, common.CopyBytes(it.Path()), common.CopyBytes(blob)); err != nil {
				return err
			}
			m.stats.storageNodes.Add(1)
			m.stats.bytes.Add(uint64(len(blob)))
		}
		if it.Leaf() {
			m.stats.storageLeaves.Add(1)
		}
	}
	if err := it.Error(); err != nil {
		return fmt.Errorf("iterate storage trie account %s root %s: %w", accountHash, storageRoot, err)
	}
	return nil
}

func (m *Migrator) writePathMetadata(dst ethdb.Database, root common.Hash) error {
	batch := dst.NewBatch()
	rawdb.WritePersistentStateID(batch, 0)
	rawdb.WriteStateID(batch, root, 0)
	rawdb.WriteSnapSyncStatusFlag(batch, rawdb.StateSyncFinished)
	if m.config.DiscardSnapshot {
		rawdb.DeleteSnapshotRoot(batch)
		rawdb.DeleteSnapshotGenerator(batch)
	}
	if err := batch.Write(); err != nil {
		return err
	}
	m.stats.batches.Add(1)
	return nil
}

type pathWriter struct {
	db             ethdb.Database
	batch          ethdb.Batch
	idealBatchSize int
	write          bool
	stats          *Stats
}

func newPathWriter(dst ethdb.Database, idealBatchSize int, write bool, stats *Stats) *pathWriter {
	var batch ethdb.Batch
	if write {
		batch = dst.NewBatch()
	}
	return &pathWriter{
		db:             dst,
		batch:          batch,
		idealBatchSize: idealBatchSize,
		write:          write,
		stats:          stats,
	}
}

func (w *pathWriter) sibling() *pathWriter {
	return newPathWriter(w.db, w.idealBatchSize, w.write, w.stats)
}

func (w *pathWriter) WriteAccountNode(path []byte, blob []byte) error {
	if !w.write {
		return nil
	}
	rawdb.WriteAccountTrieNode(w.batch, path, blob)
	return w.flushIfNeeded()
}

func (w *pathWriter) WriteStorageNode(accountHash common.Hash, path []byte, blob []byte) error {
	if !w.write {
		return nil
	}
	rawdb.WriteStorageTrieNode(w.batch, accountHash, path, blob)
	return w.flushIfNeeded()
}

func (w *pathWriter) flushIfNeeded() error {
	if w.batch.ValueSize() < w.idealBatchSize {
		return nil
	}
	return w.Flush()
}

func (w *pathWriter) Flush() error {
	if !w.write || w.batch.ValueSize() == 0 {
		return nil
	}
	if err := w.batch.Write(); err != nil {
		return err
	}
	if w.stats != nil {
		w.stats.batches.Add(1)
	}
	w.batch.Reset()
	return nil
}

func (m *Migrator) cleanupLegacyHashState(ctx context.Context, db ethdb.Database) error {
	log.Info("Cleaning legacy hash-scheme trie nodes")
	it := db.NewIterator(nil, nil)
	defer it.Release()

	batch := db.NewBatch()
	for it.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !rawdb.IsLegacyTrieNode(it.Key(), it.Value()) {
			continue
		}
		size := len(it.Key()) + len(it.Value())
		key := common.CopyBytes(it.Key())
		if err := batch.Delete(key); errors.Is(err, ethdb.ErrBatchTooLarge) {
			if err := flushCleanupBatch(batch, &m.stats); err != nil {
				return err
			}
			if err := batch.Delete(key); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		m.stats.legacyNodes.Add(1)
		m.stats.legacyBytes.Add(uint64(size))
		if batch.ValueSize() >= m.config.IdealBatchSize {
			if err := flushCleanupBatch(batch, &m.stats); err != nil {
				return err
			}
		}
	}
	if err := it.Error(); err != nil {
		return fmt.Errorf("iterate destination database for legacy cleanup: %w", err)
	}
	if err := flushCleanupBatch(batch, &m.stats); err != nil {
		return err
	}
	if err := db.SyncKeyValue(); err != nil {
		return err
	}
	log.Info(
		"Legacy hash-scheme trie cleanup finished",
		"nodes", m.stats.LegacyNodes(),
		"MB", m.stats.LegacyBytes()/1024/1024,
		"batches", m.stats.Batches(),
	)
	return nil
}

func (m *Migrator) cleanupHashdbSnapshots(ctx context.Context, db ethdb.Database) error {
	log.Info("Cleaning stale hashdb snapshot flat-state entries")
	it := db.NewIterator(nil, nil)
	defer it.Release()

	batch := db.NewBatch()
	rawdb.DeleteSnapshotDisabled(batch)
	rawdb.DeleteSnapshotRoot(batch)
	rawdb.DeleteSnapshotJournal(batch)
	rawdb.DeleteSnapshotGenerator(batch)
	rawdb.DeleteSnapshotRecoveryNumber(batch)

	for it.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !isSnapshotFlatStateKey(it.Key()) {
			continue
		}
		size := len(it.Key()) + len(it.Value())
		key := common.CopyBytes(it.Key())
		if err := batch.Delete(key); errors.Is(err, ethdb.ErrBatchTooLarge) {
			if err := flushCleanupBatch(batch, &m.stats); err != nil {
				return err
			}
			if err := batch.Delete(key); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		m.stats.snapshotNodes.Add(1)
		m.stats.snapshotBytes.Add(uint64(size))
		if batch.ValueSize() >= m.config.IdealBatchSize {
			if err := flushCleanupBatch(batch, &m.stats); err != nil {
				return err
			}
		}
	}
	if err := it.Error(); err != nil {
		return fmt.Errorf("iterate destination database for snapshot cleanup: %w", err)
	}
	if err := flushCleanupBatch(batch, &m.stats); err != nil {
		return err
	}
	if err := db.SyncKeyValue(); err != nil {
		return err
	}
	log.Info(
		"Hashdb snapshot flat-state cleanup finished",
		"entries", m.stats.SnapshotNodes(),
		"MB", m.stats.SnapshotBytes()/1024/1024,
		"batches", m.stats.Batches(),
	)
	return nil
}

func isSnapshotFlatStateKey(key []byte) bool {
	return (bytes.HasPrefix(key, rawdb.SnapshotAccountPrefix) && len(key) == len(rawdb.SnapshotAccountPrefix)+common.HashLength) ||
		(bytes.HasPrefix(key, rawdb.SnapshotStoragePrefix) && len(key) == len(rawdb.SnapshotStoragePrefix)+2*common.HashLength)
}

func flushCleanupBatch(batch ethdb.Batch, stats *Stats) error {
	if batch.ValueSize() == 0 {
		return nil
	}
	if err := batch.Write(); err != nil {
		return err
	}
	if stats != nil {
		stats.batches.Add(1)
	}
	batch.Reset()
	return nil
}

func compactDestination(db ethdb.Database) error {
	log.Info("Compacting destination key-value database")
	if err := db.Compact(nil, nil); err != nil {
		return err
	}
	if err := db.SyncKeyValue(); err != nil {
		return err
	}
	log.Info("Destination compaction completed")
	return nil
}

func VerifyPathState(ctx context.Context, db ethdb.Database, root common.Hash) error {
	started := time.Now()
	lastProgress := started
	var accountNodes uint64
	var accountLeaves uint64
	var storageTries uint64
	var storageNodes uint64
	var storageLeaves uint64
	logProgress := func(force bool) {
		if !force && time.Since(lastProgress) < 30*time.Second {
			return
		}
		log.Info("Pathdb verification progress",
			"accountNodes", accountNodes,
			"accountLeaves", accountLeaves,
			"storageTries", storageTries,
			"storageNodes", storageNodes,
			"storageLeaves", storageLeaves,
			"elapsed", time.Since(started))
		lastProgress = time.Now()
	}

	if _, err := rawdb.ParseStateScheme(rawdb.PathScheme, db); err != nil {
		return err
	}
	if root != types.EmptyRootHash {
		blob := rawdb.ReadAccountTrieNode(db, nil)
		if len(blob) == 0 {
			return errors.New("pathdb root account trie node is missing")
		}
		if got := crypto.Keccak256Hash(blob); got != root {
			return fmt.Errorf("pathdb root hash mismatch: have %s want %s", got, root)
		}
	}

	pathConfig := *pathdb.Defaults
	pathConfig.SnapshotNoBuild = true
	pathConfig.NoAsyncFlush = true
	pathConfig.NoAsyncGeneration = true
	pathTrieDB := triedb.NewDatabase(db, &triedb.Config{PathDB: &pathConfig})
	defer pathTrieDB.Close()

	accountTrie, err := trie.New(trie.StateTrieID(root), pathTrieDB)
	if err != nil {
		return fmt.Errorf("open destination account trie %s: %w", root, err)
	}
	accountIt, err := accountTrie.NodeIterator(nil)
	if err != nil {
		return err
	}
	for accountIt.Next(true) {
		accountNodes++
		logProgress(false)
		if err := ctx.Err(); err != nil {
			return err
		}
		if !accountIt.Leaf() {
			continue
		}
		accountLeaves++
		accountHashBytes := common.CopyBytes(accountIt.LeafKey())
		if len(accountHashBytes) != common.HashLength {
			return fmt.Errorf("unexpected destination account trie leaf key length %d", len(accountHashBytes))
		}
		var account types.StateAccount
		if err := rlp.DecodeBytes(common.CopyBytes(accountIt.LeafBlob()), &account); err != nil {
			return fmt.Errorf("decode destination account leaf %x: %w", accountHashBytes, err)
		}
		if account.Root == types.EmptyRootHash {
			continue
		}
		storageTries++
		storageTrie, err := trie.New(trie.StorageTrieID(root, common.BytesToHash(accountHashBytes), account.Root), pathTrieDB)
		if err != nil {
			return fmt.Errorf("open destination storage trie account %x root %s: %w", accountHashBytes, account.Root, err)
		}
		storageIt, err := storageTrie.NodeIterator(nil)
		if err != nil {
			return err
		}
		for storageIt.Next(true) {
			storageNodes++
			if storageIt.Leaf() {
				storageLeaves++
			}
			logProgress(false)
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if err := storageIt.Error(); err != nil {
			return fmt.Errorf("iterate destination storage trie account %x root %s: %w", accountHashBytes, account.Root, err)
		}
	}
	if err := accountIt.Error(); err != nil {
		return fmt.Errorf("iterate destination account trie: %w", err)
	}
	logProgress(true)
	log.Info("Pathdb verification completed",
		"root", root,
		"accountNodes", accountNodes,
		"accountLeaves", accountLeaves,
		"storageTries", storageTries,
		"storageNodes", storageNodes,
		"storageLeaves", storageLeaves,
		"elapsed", time.Since(started))
	return nil
}
