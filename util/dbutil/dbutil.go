// Copyright 2021-2024, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package dbutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/cockroachdb/pebble"
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
)

func IsErrNotFound(err error) bool {
	return errors.Is(err, leveldb.ErrNotFound) || errors.Is(err, pebble.ErrNotFound) || errors.Is(err, memorydb.ErrMemorydbNotFound)
}

var pebbleNotExistErrorRegex = regexp.MustCompile("pebble: database .* does not exist")

func isPebbleNotExistError(err error) bool {
	return err != nil && pebbleNotExistErrorRegex.MatchString(err.Error())
}

func isLeveldbNotExistError(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

// IsNotExistError returns true if the error is a "database not found" error.
// It must return false if err is nil.
func IsNotExistError(err error) bool {
	return isLeveldbNotExistError(err) || isPebbleNotExistError(err)
}

var unfinishedConversionCanaryKey = []byte("unfinished-conversion-canary-key")

const unfinishedConversionCanaryFile = "UNFINISHED_MDBX_CONVERSION"

func PutUnfinishedConversionCanary(db ethdb.KeyValueStore) error {
	return db.Put(unfinishedConversionCanaryKey, []byte{1})
}

func DeleteUnfinishedConversionCanary(db ethdb.KeyValueStore) error {
	return db.Delete(unfinishedConversionCanaryKey)
}

func UnfinishedConversionCheck(db ethdb.KeyValueStore) error {
	unfinished, err := db.Has(unfinishedConversionCanaryKey)
	if err != nil {
		return fmt.Errorf("Failed to check UnfinishedConversionCanaryKey existence: %w", err)
	}
	if unfinished {
		return errors.New("Unfinished conversion canary key detected")
	}
	return nil
}

func UnfinishedConversionCanaryPath(dir string) string {
	return filepath.Join(dir, unfinishedConversionCanaryFile)
}

func PutUnfinishedConversionCanaryFile(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := UnfinishedConversionCanaryPath(dir)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte("in-progress"), 0o644)
}

func DeleteUnfinishedConversionCanaryFile(dir string) error {
	path := UnfinishedConversionCanaryPath(dir)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func HasUnfinishedConversionCanaryFile(dir string) bool {
	info, err := os.Stat(UnfinishedConversionCanaryPath(dir))
	if err != nil {
		return false
	}
	return !info.IsDir()
}
