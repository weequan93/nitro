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

func (m *Migrator) Run(ctx context.Context) error {
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
		batch:          batch,
		idealBatchSize: idealBatchSize,
		write:          write,
		stats:          stats,
	}
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
		if err := ctx.Err(); err != nil {
			return err
		}
		if !accountIt.Leaf() {
			continue
		}
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
		storageTrie, err := trie.New(trie.StorageTrieID(root, common.BytesToHash(accountHashBytes), account.Root), pathTrieDB)
		if err != nil {
			return fmt.Errorf("open destination storage trie account %x root %s: %w", accountHashBytes, account.Root, err)
		}
		storageIt, err := storageTrie.NodeIterator(nil)
		if err != nil {
			return err
		}
		for storageIt.Next(true) {
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
	log.Info("Pathdb verification completed", "root", root)
	return nil
}
