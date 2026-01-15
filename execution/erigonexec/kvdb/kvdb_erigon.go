//go:build erigon
// +build erigon

package kvdb

import (
	"bytes"
	"context"
	"errors"

	"github.com/erigontech/erigon/db/kv"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/syndtr/goleveldb/leveldb"
)

var errNotSupported = errors.New("erigon kvdb: ancients not supported")

type DB struct {
	db     kv.RwDB
	bucket string
}

func New(db kv.RwDB, bucket string) *DB {
	return &DB{
		db:     db,
		bucket: bucket,
	}
}

func (d *DB) Close() error {
	d.db.Close()
	return nil
}

func (d *DB) Has(key []byte) (bool, error) {
	tx, err := d.db.BeginRo(context.Background())
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	return tx.Has(d.bucket, key)
}

func (d *DB) Get(key []byte) ([]byte, error) {
	tx, err := d.db.BeginRo(context.Background())
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	value, err := tx.GetOne(d.bucket, key)
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

func (d *DB) Put(key []byte, value []byte) error {
	return d.db.Update(context.Background(), func(tx kv.RwTx) error {
		return tx.Put(d.bucket, key, value)
	})
}

func (d *DB) Delete(key []byte) error {
	return d.db.Update(context.Background(), func(tx kv.RwTx) error {
		return tx.Delete(d.bucket, key)
	})
}

func (d *DB) Stat(property string) (string, error) {
	_ = property
	return "", errNotSupported
}

func (d *DB) NewBatch() ethdb.Batch {
	return &batch{db: d}
}

func (d *DB) NewBatchWithSize(size int) ethdb.Batch {
	_ = size
	return &batch{db: d}
}

func (d *DB) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	tx, err := d.db.BeginRo(context.Background())
	if err != nil {
		return &iterator{err: err, done: true}
	}
	cursor, err := tx.Cursor(d.bucket)
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

func (d *DB) Compact(start []byte, limit []byte) error {
	_ = start
	_ = limit
	return nil
}

func (d *DB) NewSnapshot() (ethdb.Snapshot, error) {
	tx, err := d.db.BeginRo(context.Background())
	if err != nil {
		return nil, err
	}
	return &snapshot{
		tx:     tx,
		bucket: d.bucket,
	}, nil
}

func (d *DB) HasAncient(kind string, number uint64) (bool, error) {
	_ = kind
	_ = number
	return false, errNotSupported
}

func (d *DB) Ancient(kind string, number uint64) ([]byte, error) {
	_ = kind
	_ = number
	return nil, errNotSupported
}

func (d *DB) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	_ = kind
	_ = start
	_ = count
	_ = maxBytes
	return nil, errNotSupported
}

func (d *DB) Ancients() (uint64, error) {
	return 0, errNotSupported
}

func (d *DB) Tail() (uint64, error) {
	return 0, errNotSupported
}

func (d *DB) AncientSize(kind string) (uint64, error) {
	_ = kind
	return 0, errNotSupported
}

func (d *DB) ReadAncients(fn func(ethdb.AncientReaderOp) error) error {
	if fn == nil {
		return nil
	}
	return fn(d)
}

func (d *DB) ModifyAncients(fn func(ethdb.AncientWriteOp) error) (int64, error) {
	_ = fn
	return 0, errNotSupported
}

func (d *DB) TruncateHead(n uint64) (uint64, error) {
	_ = n
	return 0, errNotSupported
}

func (d *DB) TruncateTail(n uint64) (uint64, error) {
	_ = n
	return 0, errNotSupported
}

func (d *DB) Sync() error {
	return nil
}

func (d *DB) MigrateTable(kind string, convert func([]byte) ([]byte, error)) error {
	_ = kind
	_ = convert
	return errNotSupported
}

func (d *DB) AncientDatadir() (string, error) {
	return "", errNotSupported
}

func (d *DB) WasmDataBase() (ethdb.KeyValueStore, uint32) {
	return d, 0
}

func (d *DB) WasmTargets() []ethdb.WasmTarget {
	return nil
}

type batch struct {
	db    *DB
	ops   []batchOp
	bytes int
}

type batchOp struct {
	key    []byte
	value  []byte
	delete bool
}

func (b *batch) Put(key []byte, value []byte) error {
	copiedKey := append([]byte(nil), key...)
	copiedValue := append([]byte(nil), value...)
	b.ops = append(b.ops, batchOp{key: copiedKey, value: copiedValue})
	b.bytes += len(copiedKey) + len(copiedValue)
	return nil
}

func (b *batch) Delete(key []byte) error {
	copiedKey := append([]byte(nil), key...)
	b.ops = append(b.ops, batchOp{key: copiedKey, delete: true})
	b.bytes += len(copiedKey)
	return nil
}

func (b *batch) ValueSize() int {
	return b.bytes
}

func (b *batch) Write() error {
	if len(b.ops) == 0 {
		return nil
	}
	err := b.db.db.Update(context.Background(), func(tx kv.RwTx) error {
		for _, op := range b.ops {
			if op.delete {
				if err := tx.Delete(b.db.bucket, op.key); err != nil {
					return err
				}
				continue
			}
			if err := tx.Put(b.db.bucket, op.key, op.value); err != nil {
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

func (b *batch) Reset() {
	b.ops = nil
	b.bytes = 0
}

func (b *batch) Replay(w ethdb.KeyValueWriter) error {
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

type snapshot struct {
	tx     kv.Tx
	bucket string
}

func (s *snapshot) Has(key []byte) (bool, error) {
	return s.tx.Has(s.bucket, key)
}

func (s *snapshot) Get(key []byte) ([]byte, error) {
	value, err := s.tx.GetOne(s.bucket, key)
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

func (s *snapshot) Release() {
	s.tx.Rollback()
}

type iterator struct {
	tx      kv.Tx
	cursor  kv.Cursor
	prefix  []byte
	start   []byte
	key     []byte
	value   []byte
	err     error
	started bool
	done    bool
}

func (it *iterator) Next() bool {
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
		seekKey := it.seekKey()
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

func (it *iterator) Error() error {
	return it.err
}

func (it *iterator) Key() []byte {
	if it.done {
		return nil
	}
	return it.key
}

func (it *iterator) Value() []byte {
	if it.done {
		return nil
	}
	return it.value
}

func (it *iterator) Release() {
	if it.cursor != nil {
		it.cursor.Close()
	}
	if it.tx != nil {
		it.tx.Rollback()
	}
}

func (it *iterator) seekKey() []byte {
	if len(it.prefix) == 0 {
		return it.start
	}
	if len(it.start) == 0 {
		return it.prefix
	}
	key := make([]byte, len(it.prefix)+len(it.start))
	copy(key, it.prefix)
	copy(key[len(it.prefix):], it.start)
	return key
}

func hasPrefix(key []byte, prefix []byte) bool {
	if len(prefix) == 0 {
		return true
	}
	return bytes.HasPrefix(key, prefix)
}
