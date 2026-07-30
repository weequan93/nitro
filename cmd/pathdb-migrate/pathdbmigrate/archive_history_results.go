// Copyright 2023-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	archiveResultAccountIndexFile = "account-index"
	archiveResultStorageIndexFile = "storage-index"
	archiveResultAccountDataFile  = "account-data"
	archiveResultStorageDataFile  = "storage-data"
)

type archiveResultMemoryLimiter struct {
	lock  sync.Mutex
	limit uint64
	used  uint64
}

func newArchiveResultMemoryLimiter(limitMB int) *archiveResultMemoryLimiter {
	return &archiveResultMemoryLimiter{limit: uint64(limitMB) * 1024 * 1024}
}

func (l *archiveResultMemoryLimiter) tryAcquire(size uint64) bool {
	l.lock.Lock()
	defer l.lock.Unlock()

	if l.limit == 0 || size > l.limit-l.used {
		return false
	}
	l.used += size
	return true
}

func (l *archiveResultMemoryLimiter) release(size uint64) {
	l.lock.Lock()
	defer l.lock.Unlock()

	if size > l.used {
		panic(fmt.Sprintf("archive result memory release %d exceeds retained bytes %d", size, l.used))
	}
	l.used -= size
}

func encodedArchiveHistorySize(encoded *encodedArchiveHistory) uint64 {
	if encoded == nil {
		return 0
	}
	return uint64(len(encoded.accountIndex)) +
		uint64(len(encoded.storageIndex)) +
		uint64(len(encoded.accountData)) +
		uint64(len(encoded.storageData))
}

type spilledArchiveHistory struct {
	directory    string
	accounts     uint64
	storageSlots uint64
	size         uint64
}

func spillEncodedArchiveHistory(
	parent string,
	order uint64,
	encoded *encodedArchiveHistory,
) (*spilledArchiveHistory, error) {
	if encoded == nil {
		return nil, fmt.Errorf("cannot spill nil archive history result")
	}
	directory := filepath.Join(parent, fmt.Sprintf("%020d", order))
	if err := os.Mkdir(directory, 0700); err != nil {
		return nil, fmt.Errorf("create archive result spill directory: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(directory)
		}
	}()

	sections := []struct {
		name string
		data []byte
	}{
		{name: archiveResultAccountIndexFile, data: encoded.accountIndex},
		{name: archiveResultStorageIndexFile, data: encoded.storageIndex},
		{name: archiveResultAccountDataFile, data: encoded.accountData},
		{name: archiveResultStorageDataFile, data: encoded.storageData},
	}
	for _, section := range sections {
		path := filepath.Join(directory, section.name)
		if err := os.WriteFile(path, section.data, 0600); err != nil {
			return nil, fmt.Errorf("write archive result spill section %s: %w", section.name, err)
		}
	}
	cleanup = false
	return &spilledArchiveHistory{
		directory:    directory,
		accounts:     encoded.accounts,
		storageSlots: encoded.storageSlots,
		size:         encodedArchiveHistorySize(encoded),
	}, nil
}

func (s *spilledArchiveHistory) load() (*encodedArchiveHistory, error) {
	readSection := func(name string) ([]byte, error) {
		path := filepath.Join(s.directory, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read archive result spill section %s: %w", name, err)
		}
		return data, nil
	}

	accountIndex, err := readSection(archiveResultAccountIndexFile)
	if err != nil {
		return nil, err
	}
	storageIndex, err := readSection(archiveResultStorageIndexFile)
	if err != nil {
		return nil, err
	}
	accountData, err := readSection(archiveResultAccountDataFile)
	if err != nil {
		return nil, err
	}
	storageData, err := readSection(archiveResultStorageDataFile)
	if err != nil {
		return nil, err
	}
	encoded := &encodedArchiveHistory{
		accountIndex: accountIndex,
		storageIndex: storageIndex,
		accountData:  accountData,
		storageData:  storageData,
		accounts:     s.accounts,
		storageSlots: s.storageSlots,
	}
	if size := encodedArchiveHistorySize(encoded); size != s.size {
		return nil, fmt.Errorf("archive result spill size mismatch: have %d want %d", size, s.size)
	}
	if err := validateEncodedArchiveHistory(encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

func (s *spilledArchiveHistory) remove() error {
	if s == nil || s.directory == "" {
		return nil
	}
	return os.RemoveAll(s.directory)
}

func (r *archiveTransitionResult) materialize() (*encodedArchiveHistory, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.encoded != nil {
		return r.encoded, nil
	}
	if r.spilled != nil {
		return r.spilled.load()
	}
	return nil, fmt.Errorf("archive transition %d has no encoded result", r.job.order)
}

func (r *archiveTransitionResult) release(memory *archiveResultMemoryLimiter) error {
	if r.memoryBytes != 0 {
		memory.release(r.memoryBytes)
		r.memoryBytes = 0
	}
	r.encoded = nil
	if r.spilled == nil {
		return nil
	}
	err := r.spilled.remove()
	r.spilled = nil
	return err
}
