//go:build erigon
// +build erigon

package erigonexec

import (
	"os"
	"strings"
)

func debugErigonCommitEnabled() bool {
	return strings.TrimSpace(os.Getenv("NITRO_DEBUG_ERIGON_COMMIT")) != ""
}
