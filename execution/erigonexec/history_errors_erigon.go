//go:build erigon
// +build erigon

package erigonexec

import (
	"fmt"
	"strings"
)

type historyPrunedError struct {
	mode string
	err  error
}

func (e historyPrunedError) Error() string {
	if e.mode == "" {
		return fmt.Sprintf("erigonexec: historical data pruned: %v", e.err)
	}
	return fmt.Sprintf("erigonexec: historical data pruned (mode=%s): %v", e.mode, e.err)
}

func (e historyPrunedError) Unwrap() error {
	return e.err
}

func (c *Client) historyPruned() bool {
	return c.pruneMode.History.Enabled()
}

func (c *Client) wrapHistoryError(err error) error {
	if err == nil || !c.historyPruned() {
		return err
	}
	if !isHistoryMissingError(err) {
		return err
	}
	mode := c.pruneModeLabel
	if mode == "" {
		mode = c.pruneMode.String()
	}
	return historyPrunedError{mode: mode, err: err}
}

func isHistoryMissingError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, token := range []string{
		"history",
		"historical",
		"prun",
		"txnum",
		"tx num",
		"txnums",
	} {
		if strings.Contains(msg, token) {
			return true
		}
	}
	return false
}
