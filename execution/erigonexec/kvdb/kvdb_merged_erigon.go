//go:build erigon
// +build erigon

package kvdb

import (
	"bytes"
	"container/heap"
	"context"

	"github.com/erigontech/erigon/db/kv"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/syndtr/goleveldb/leveldb"
)

// PrefixBuckets defines per-prefix bucket routing and iteration order.
type PrefixBuckets struct {
	Buckets         []string
	DefaultBucket   string
	PrefixToBucket  map[byte]string
}

// MergedDB exposes a single ethdb view across multiple MDBX buckets.
type MergedDB struct {
	db  kv.RwDB
	cfg PrefixBuckets
}

// NewMerged creates a merged ethdb adapter across per-prefix buckets.
func NewMerged(db kv.RwDB, cfg PrefixBuckets) *MergedDB {
	if cfg.DefaultBucket == "" && len(cfg.Buckets) > 0 {
		cfg.DefaultBucket = cfg.Buckets[len(cfg.Buckets)-1]
	}
	return &MergedDB{
		db:  db,
		cfg: cfg,
	}
}

func (d *MergedDB) Close() error {
	d.db.Close()
	return nil
}

func (d *MergedDB) Has(key []byte) (bool, error) {
	tx, err := d.db.BeginRo(context.Background())
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	return tx.Has(d.bucketForKey(key), key)
}

func (d *MergedDB) Get(key []byte) ([]byte, error) {
	tx, err := d.db.BeginRo(context.Background())
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	value, err := tx.GetOne(d.bucketForKey(key), key)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, leveldb.ErrNotFound
	}
	copied := make([]byte, len(value))
	copy(copied, value)
	return copied, nil
}

func (d *MergedDB) Put(key []byte, value []byte) error {
	return d.db.Update(context.Background(), func(tx kv.RwTx) error {
		return tx.Put(d.bucketForKey(key), key, value)
	})
}

func (d *MergedDB) Delete(key []byte) error {
	return d.db.Update(context.Background(), func(tx kv.RwTx) error {
		return tx.Delete(d.bucketForKey(key), key)
	})
}

func (d *MergedDB) Stat(property string) (string, error) {
	_ = property
	return "", errNotSupported
}

func (d *MergedDB) NewBatch() ethdb.Batch {
	return &mergedBatch{db: d}
}

func (d *MergedDB) NewBatchWithSize(size int) ethdb.Batch {
	_ = size
	return &mergedBatch{db: d}
}

func (d *MergedDB) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	if bucket, ok := d.bucketForPrefix(prefix); ok {
		return newBucketIterator(d.db, bucket, prefix, start)
	}
	return newMergedIterator(d.db, d.cfg.Buckets, prefix, start)
}

func (d *MergedDB) Compact(start []byte, limit []byte) error {
	_ = start
	_ = limit
	return nil
}

func (d *MergedDB) NewSnapshot() (ethdb.Snapshot, error) {
	tx, err := d.db.BeginRo(context.Background())
	if err != nil {
		return nil, err
	}
	return &mergedSnapshot{
		tx:  tx,
		cfg: d.cfg,
	}, nil
}

func (d *MergedDB) HasAncient(kind string, number uint64) (bool, error) {
	_ = kind
	_ = number
	return false, errNotSupported
}

func (d *MergedDB) Ancient(kind string, number uint64) ([]byte, error) {
	_ = kind
	_ = number
	return nil, errNotSupported
}

func (d *MergedDB) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	_ = kind
	_ = start
	_ = count
	_ = maxBytes
	return nil, errNotSupported
}

func (d *MergedDB) Ancients() (uint64, error) {
	return 0, errNotSupported
}

func (d *MergedDB) Tail() (uint64, error) {
	return 0, errNotSupported
}

func (d *MergedDB) AncientSize(kind string) (uint64, error) {
	_ = kind
	return 0, errNotSupported
}

func (d *MergedDB) ReadAncients(fn func(ethdb.AncientReaderOp) error) error {
	if fn == nil {
		return nil
	}
	return fn(d)
}

func (d *MergedDB) ModifyAncients(fn func(ethdb.AncientWriteOp) error) (int64, error) {
	_ = fn
	return 0, errNotSupported
}

func (d *MergedDB) TruncateHead(n uint64) (uint64, error) {
	_ = n
	return 0, errNotSupported
}

func (d *MergedDB) TruncateTail(n uint64) (uint64, error) {
	_ = n
	return 0, errNotSupported
}

func (d *MergedDB) Sync() error {
	return nil
}

func (d *MergedDB) MigrateTable(kind string, convert func([]byte) ([]byte, error)) error {
	_ = kind
	_ = convert
	return errNotSupported
}

func (d *MergedDB) AncientDatadir() (string, error) {
	return "", errNotSupported
}

func (d *MergedDB) WasmDataBase() (ethdb.KeyValueStore, uint32) {
	return d, 0
}

func (d *MergedDB) WasmTargets() []ethdb.WasmTarget {
	return nil
}

func (d *MergedDB) bucketForKey(key []byte) string {
	if len(key) == 0 {
		return d.cfg.DefaultBucket
	}
	if bucket, ok := d.cfg.PrefixToBucket[key[0]]; ok {
		return bucket
	}
	return d.cfg.DefaultBucket
}

func (d *MergedDB) bucketForPrefix(prefix []byte) (string, bool) {
	if len(prefix) == 0 {
		return "", false
	}
	if bucket, ok := d.cfg.PrefixToBucket[prefix[0]]; ok {
		return bucket, true
	}
	return d.cfg.DefaultBucket, true
}

type mergedSnapshot struct {
	tx  kv.Tx
	cfg PrefixBuckets
}

func (s *mergedSnapshot) Has(key []byte) (bool, error) {
	bucket := bucketForSnapshotKey(s.cfg, key)
	return s.tx.Has(bucket, key)
}

func (s *mergedSnapshot) Get(key []byte) ([]byte, error) {
	bucket := bucketForSnapshotKey(s.cfg, key)
	value, err := s.tx.GetOne(bucket, key)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, leveldb.ErrNotFound
	}
	copied := make([]byte, len(value))
	copy(copied, value)
	return copied, nil
}

func (s *mergedSnapshot) Release() {
	s.tx.Rollback()
}

func bucketForSnapshotKey(cfg PrefixBuckets, key []byte) string {
	if len(key) == 0 {
		return cfg.DefaultBucket
	}
	if bucket, ok := cfg.PrefixToBucket[key[0]]; ok {
		return bucket
	}
	return cfg.DefaultBucket
}

type mergedBatch struct {
	db    *MergedDB
	ops   []batchOp
	bytes int
}

func (b *mergedBatch) Put(key []byte, value []byte) error {
	copiedKey := append([]byte(nil), key...)
	copiedValue := append([]byte(nil), value...)
	b.ops = append(b.ops, batchOp{key: copiedKey, value: copiedValue})
	b.bytes += len(copiedKey) + len(copiedValue)
	return nil
}

func (b *mergedBatch) Delete(key []byte) error {
	copiedKey := append([]byte(nil), key...)
	b.ops = append(b.ops, batchOp{key: copiedKey, delete: true})
	b.bytes += len(copiedKey)
	return nil
}

func (b *mergedBatch) ValueSize() int {
	return b.bytes
}

func (b *mergedBatch) Write() error {
	if len(b.ops) == 0 {
		return nil
	}
	err := b.db.db.Update(context.Background(), func(tx kv.RwTx) error {
		for _, op := range b.ops {
			bucket := b.db.bucketForKey(op.key)
			if op.delete {
				if err := tx.Delete(bucket, op.key); err != nil {
					return err
				}
				continue
			}
			if err := tx.Put(bucket, op.key, op.value); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		b.Reset()
	}
	return err
}

func (b *mergedBatch) Reset() {
	b.ops = nil
	b.bytes = 0
}

func (b *mergedBatch) Replay(w ethdb.KeyValueWriter) error {
	for _, op := range b.ops {
		if op.delete {
			if err := w.Delete(op.key); err != nil {
				return err
			}
			continue
		}
		if err := w.Put(op.key, op.value); err != nil {
			return err
		}
	}
	return nil
}

type bucketIterator struct {
	cursor  kv.Cursor
	prefix  []byte
	start   []byte
	key     []byte
	value   []byte
	order   int
	started bool
	done    bool
	err     error
}

func (it *bucketIterator) advance() bool {
	if it.done || it.err != nil {
		return false
	}
	var (
		key []byte
		val []byte
		err error
	)
	if !it.started {
		it.started = true
		seekKey := seekKey(it.prefix, it.start)
		switch {
		case len(seekKey) == 0:
			key, val, err = it.cursor.First()
		default:
			key, val, err = it.cursor.Seek(seekKey)
		}
	} else {
		key, val, err = it.cursor.Next()
	}
	if err != nil {
		it.err = err
		it.done = true
		return false
	}
	if key == nil {
		it.done = true
		return false
	}
	if !hasPrefix(key, it.prefix) {
		it.done = true
		return false
	}
	it.key = key
	it.value = val
	return true
}

type iterHeap []*bucketIterator

func (h iterHeap) Len() int { return len(h) }
func (h iterHeap) Less(i, j int) bool {
	cmp := bytes.Compare(h[i].key, h[j].key)
	if cmp == 0 {
		return h[i].order < h[j].order
	}
	return cmp < 0
}
func (h iterHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *iterHeap) Push(x interface{}) {
	*h = append(*h, x.(*bucketIterator))
}

func (h *iterHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

type mergedIterator struct {
	tx      kv.Tx
	iters   []*bucketIterator
	heap    iterHeap
	current *bucketIterator
	key     []byte
	value   []byte
	err     error
	started bool
	done    bool
}

func newBucketIterator(db kv.RwDB, bucket string, prefix []byte, start []byte) ethdb.Iterator {
	tx, err := db.BeginRo(context.Background())
	if err != nil {
		return &iterator{err: err, done: true}
	}
	cursor, err := tx.Cursor(bucket)
	if err != nil {
		tx.Rollback()
		return &iterator{err: err, done: true}
	}
	prefixCopy := append([]byte(nil), prefix...)
	startCopy := append([]byte(nil), start...)
	return &iterator{
		tx:     tx,
		cursor: cursor,
		prefix: prefixCopy,
		start:  startCopy,
	}
}

func newMergedIterator(db kv.RwDB, buckets []string, prefix []byte, start []byte) ethdb.Iterator {
	tx, err := db.BeginRo(context.Background())
	if err != nil {
		return &mergedIterator{err: err, done: true}
	}
	prefixCopy := append([]byte(nil), prefix...)
	startCopy := append([]byte(nil), start...)
	iters := make([]*bucketIterator, 0, len(buckets))
	for i, bucket := range buckets {
		cursor, err := tx.Cursor(bucket)
		if err != nil {
			for _, it := range iters {
				it.cursor.Close()
			}
			tx.Rollback()
			return &mergedIterator{err: err, done: true}
		}
		iters = append(iters, &bucketIterator{
			cursor: cursor,
			prefix: prefixCopy,
			start:  startCopy,
			order:  i,
		})
	}
	return &mergedIterator{
		tx:    tx,
		iters: iters,
	}
}

func (it *mergedIterator) Next() bool {
	if it.done || it.err != nil {
		return false
	}
	if !it.started {
		it.started = true
		for _, iter := range it.iters {
			if iter.advance() {
				heap.Push(&it.heap, iter)
				continue
			}
			if iter.err != nil {
				it.err = iter.err
				it.done = true
				return false
			}
		}
	} else if it.current != nil {
		if it.current.advance() {
			heap.Push(&it.heap, it.current)
		} else if it.current.err != nil {
			it.err = it.current.err
			it.done = true
			return false
		}
		it.current = nil
	}
	if len(it.heap) == 0 {
		it.done = true
		return false
	}
	it.current = heap.Pop(&it.heap).(*bucketIterator)
	it.key = it.current.key
	it.value = it.current.value
	return true
}

func (it *mergedIterator) Error() error {
	return it.err
}

func (it *mergedIterator) Key() []byte {
	if it.done {
		return nil
	}
	return it.key
}

func (it *mergedIterator) Value() []byte {
	if it.done {
		return nil
	}
	return it.value
}

func (it *mergedIterator) Release() {
	for _, iter := range it.iters {
		if iter.cursor != nil {
			iter.cursor.Close()
		}
	}
	if it.tx != nil {
		it.tx.Rollback()
	}
}

func seekKey(prefix []byte, start []byte) []byte {
	if len(prefix) == 0 {
		return start
	}
	if len(start) == 0 {
		return prefix
	}
	key := make([]byte, len(prefix)+len(start))
	copy(key, prefix)
	copy(key[len(prefix):], start)
	return key
}
