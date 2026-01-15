//go:build erigon
// +build erigon

package kvdb

import (
	"context"
	"reflect"
	"testing"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/dbcfg"
	"github.com/erigontech/erigon/db/kv/mdbx"
	"github.com/ethereum/go-ethereum/ethdb"
)

const (
	testBucketA       = "bucket_a"
	testBucketB       = "bucket_b"
	testBucketDefault = "bucket_default"
)

func newTestMergedDB(t *testing.T) (*MergedDB, kv.RwDB) {
	t.Helper()
	buckets := []string{testBucketA, testBucketB, testBucketDefault}
	tableCfg := kv.TableCfg{
		testBucketA:       {},
		testBucketB:       {},
		testBucketDefault: {},
		kv.Sequence:       {},
	}
	db := mdbx.New(dbcfg.ChainDB, log.New()).
		WithTableCfg(func(kv.TableCfg) kv.TableCfg { return tableCfg }).
		InMem(t, "").
		MustOpen()
	t.Cleanup(db.Close)
	merged := NewMerged(db, PrefixBuckets{
		Buckets:        buckets,
		DefaultBucket:  testBucketDefault,
		PrefixToBucket: map[byte]string{'a': testBucketA, 'b': testBucketB},
	})
	return merged, db
}

func TestMergedDBRoutesByPrefix(t *testing.T) {
	merged, raw := newTestMergedDB(t)
	batch := merged.NewBatch()
	if err := batch.Put([]byte("a1"), []byte("va1")); err != nil {
		t.Fatalf("put a1: %v", err)
	}
	if err := batch.Put([]byte("b1"), []byte("vb1")); err != nil {
		t.Fatalf("put b1: %v", err)
	}
	if err := batch.Put([]byte("z1"), []byte("vz1")); err != nil {
		t.Fatalf("put z1: %v", err)
	}
	if err := batch.Write(); err != nil {
		t.Fatalf("write batch: %v", err)
	}

	assertBucketValue(t, raw, testBucketA, []byte("a1"), []byte("va1"))
	assertBucketValue(t, raw, testBucketB, []byte("b1"), []byte("vb1"))
	assertBucketValue(t, raw, testBucketDefault, []byte("z1"), []byte("vz1"))

	if err := merged.Delete([]byte("b1")); err != nil {
		t.Fatalf("delete b1: %v", err)
	}
	has, err := merged.Has([]byte("b1"))
	if err != nil {
		t.Fatalf("has b1: %v", err)
	}
	if has {
		t.Fatalf("expected b1 to be deleted")
	}
	assertBucketMissing(t, raw, testBucketB, []byte("b1"))
}

func TestMergedIteratorOrderAndPrefix(t *testing.T) {
	merged, _ := newTestMergedDB(t)
	putKey := func(key, value string) {
		if err := merged.Put([]byte(key), []byte(value)); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}
	putKey("a1", "va1")
	putKey("a3", "va3")
	putKey("b0", "vb0")
	putKey("b2", "vb2")
	putKey("c1", "vc1")

	got, err := collectKeys(merged.NewIterator(nil, nil))
	if err != nil {
		t.Fatalf("collect all keys: %v", err)
	}
	want := []string{"a1", "a3", "b0", "b2", "c1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("iterator order mismatch: got %v want %v", got, want)
	}

	got, err = collectKeys(merged.NewIterator([]byte("a"), nil))
	if err != nil {
		t.Fatalf("collect prefix a: %v", err)
	}
	want = []string{"a1", "a3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prefix a mismatch: got %v want %v", got, want)
	}

	got, err = collectKeys(merged.NewIterator([]byte("b"), []byte("2")))
	if err != nil {
		t.Fatalf("collect prefix b start 2: %v", err)
	}
	want = []string{"b2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prefix b start mismatch: got %v want %v", got, want)
	}
}

func collectKeys(it ethdb.Iterator) ([]string, error) {
	defer it.Release()
	var out []string
	for it.Next() {
		key := append([]byte(nil), it.Key()...)
		out = append(out, string(key))
	}
	return out, it.Error()
}

func assertBucketValue(t *testing.T, db kv.RwDB, bucket string, key []byte, want []byte) {
	t.Helper()
	tx, err := db.BeginRo(context.Background())
	if err != nil {
		t.Fatalf("begin ro: %v", err)
	}
	defer tx.Rollback()
	value, err := tx.GetOne(bucket, key)
	if err != nil {
		t.Fatalf("get %s: %v", bucket, err)
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("bucket %s value mismatch: got %x want %x", bucket, value, want)
	}
}

func assertBucketMissing(t *testing.T, db kv.RwDB, bucket string, key []byte) {
	t.Helper()
	tx, err := db.BeginRo(context.Background())
	if err != nil {
		t.Fatalf("begin ro: %v", err)
	}
	defer tx.Rollback()
	has, err := tx.Has(bucket, key)
	if err != nil {
		t.Fatalf("has %s: %v", bucket, err)
	}
	if has {
		t.Fatalf("expected key %x missing from %s", key, bucket)
	}
}
