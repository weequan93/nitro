// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package arbnode

import (
	"os"
	"strings"
	"sync"
	"time"
)

var (
	inboxDebugOnce    sync.Once
	inboxDebugEnabled bool
	inboxDebugMu      sync.Mutex
	inboxDebugNext    time.Time
)

func inboxDebugEnabledFunc() bool {
	inboxDebugOnce.Do(func() {
		val := strings.TrimSpace(strings.ToLower(os.Getenv("NITRO_DEBUG_INBOX")))
		if val == "1" || val == "true" || val == "yes" || val == "y" || val == "on" {
			inboxDebugEnabled = true
			return
		}
		val = strings.TrimSpace(strings.ToLower(os.Getenv("NITRO_DEBUG_PARENTCHAIN")))
		if val == "1" || val == "true" || val == "yes" || val == "y" || val == "on" {
			inboxDebugEnabled = true
		}
	})
	return inboxDebugEnabled
}

func inboxDebugLogAllowed() bool {
	if !inboxDebugEnabledFunc() {
		return false
	}
	now := time.Now()
	inboxDebugMu.Lock()
	defer inboxDebugMu.Unlock()
	if now.Before(inboxDebugNext) {
		return false
	}
	inboxDebugNext = now.Add(2 * time.Second)
	return true
}
