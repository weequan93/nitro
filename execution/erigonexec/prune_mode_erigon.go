//go:build erigon
// +build erigon

package erigonexec

import (
	"encoding"
	"fmt"
	"strings"

	"github.com/erigontech/erigon/db/kv/prune"
)

func parsePruneMode(value string) (prune.Mode, string, error) {
	normalized := strings.TrimSpace(strings.ToLower(value))
	if normalized == "" || normalized == "auto" {
		normalized = "archive"
	}

	var mode prune.Mode
	if unmarshaler, ok := interface{}(&mode).(encoding.TextUnmarshaler); ok {
		if err := unmarshaler.UnmarshalText([]byte(normalized)); err != nil {
			return prune.Mode{}, normalized, err
		}
		label := mode.String()
		if label == "" {
			label = normalized
		}
		return mode, label, nil
	}

	mode, err := prune.FromCli(normalized, 0, 0)
	if err != nil {
		return prune.Mode{}, normalized, fmt.Errorf("erigonexec: prune mode %q unsupported by this erigon build", normalized)
	}
	label := mode.String()
	if label == "" {
		label = normalized
	}
	return mode, label, nil
}
