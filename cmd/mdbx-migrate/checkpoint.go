package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

const (
	phaseHeadersImported = "headers_imported"
	phaseSendersDone     = "senders_done"
	phaseExecutionDone   = "execution_done"
	phaseTxLookupDone    = "txlookup_done"
	phaseCopyDone        = "copy_done"
	phaseVerifyDone      = "verify_done"
)

var phaseOrder = map[string]int{
	phaseHeadersImported: 1,
	phaseSendersDone:     2,
	phaseExecutionDone:   3,
	phaseTxLookupDone:    4,
	phaseCopyDone:        5,
	phaseVerifyDone:      6,
}

type checkpoint struct {
	Mode               string `json:"mode"`
	Phase              string `json:"phase"`
	ChainID            string `json:"chain_id"`
	GenesisHash        string `json:"genesis_hash"`
	StartBlock         uint64 `json:"start_block"`
	EndBlock           uint64 `json:"end_block"`
	LastHeaderImported uint64 `json:"last_header_imported"`
	LastExecuted       uint64 `json:"last_executed"`
}

func checkpointDir(dest string) string {
	return filepath.Join(dest, "l2chaindata", ".mdbx-migrate")
}

func checkpointPath(dest string) string {
	return filepath.Join(checkpointDir(dest), "checkpoint.json")
}

func checkpointExists(dest string) (bool, error) {
	_, err := os.Stat(checkpointPath(dest))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func loadCheckpoint(dest string) (*checkpoint, error) {
	path := checkpointPath(dest)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ckpt checkpoint
	if err := json.Unmarshal(data, &ckpt); err != nil {
		return nil, fmt.Errorf("decode checkpoint: %w", err)
	}
	return &ckpt, nil
}

func writeCheckpoint(dest string, ckpt *checkpoint) error {
	if ckpt == nil {
		return errors.New("nil checkpoint")
	}
	if err := os.MkdirAll(checkpointDir(dest), 0o755); err != nil {
		return fmt.Errorf("create checkpoint dir: %w", err)
	}
	data, err := json.MarshalIndent(ckpt, "", "  ")
	if err != nil {
		return fmt.Errorf("encode checkpoint: %w", err)
	}
	if err := os.WriteFile(checkpointPath(dest), data, 0o644); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}
	return nil
}

func clearCheckpoint(dest string) error {
	path := checkpointPath(dest)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove checkpoint: %w", err)
	}
	return nil
}

func finalizeCheckpoint(dest string) error {
	ckpt, err := loadCheckpoint(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("load checkpoint: %w", err)
	}
	ckpt.Phase = phaseVerifyDone
	if err := writeCheckpoint(dest, ckpt); err != nil {
		return err
	}
	return clearCheckpoint(dest)
}

func validateCheckpoint(opts Options, info sourceChainInfo, ckpt *checkpoint) error {
	if ckpt == nil {
		return errors.New("missing checkpoint")
	}
	if ckpt.Mode != "" && ckpt.Mode != opts.Mode {
		return fmt.Errorf("checkpoint mode mismatch: %q != %q", ckpt.Mode, opts.Mode)
	}
	if ckpt.StartBlock != opts.StartBlock {
		return fmt.Errorf("checkpoint start block mismatch: %d != %d", ckpt.StartBlock, opts.StartBlock)
	}
	if ckpt.EndBlock != opts.EndBlock {
		return fmt.Errorf("checkpoint end block mismatch: %d != %d", ckpt.EndBlock, opts.EndBlock)
	}
	if ckpt.ChainID != "" && info.ChainID != "" && ckpt.ChainID != info.ChainID {
		return fmt.Errorf("checkpoint chain id mismatch: %s != %s", ckpt.ChainID, info.ChainID)
	}
	if ckpt.GenesisHash != "" && info.GenesisHash != (common.Hash{}) {
		if !strings.EqualFold(ckpt.GenesisHash, info.GenesisHash.Hex()) {
			return fmt.Errorf("checkpoint genesis hash mismatch: %s != %s", ckpt.GenesisHash, info.GenesisHash.Hex())
		}
	}
	return nil
}

func isPhaseComplete(ckpt *checkpoint, phase string) bool {
	if ckpt == nil {
		return false
	}
	return phaseIndex(ckpt.Phase) >= phaseIndex(phase)
}

func phaseIndex(phase string) int {
	if idx, ok := phaseOrder[phase]; ok {
		return idx
	}
	return -1
}
