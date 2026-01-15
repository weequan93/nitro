//go:build erigon
// +build erigon

package erigonexec

import "github.com/offchainlabs/nitro/cmd/conf"

func InitConfigUnsupportedWithErigon(cfg conf.InitConfig) []string {
	var reasons []string
	if cfg.Force {
		reasons = append(reasons, "init.force")
	}
	if cfg.Url != "" {
		reasons = append(reasons, "init.url")
	}
	if cfg.Latest != "" {
		reasons = append(reasons, "init.latest")
	}
	if cfg.LatestBase != conf.InitConfigDefault.LatestBase {
		reasons = append(reasons, "init.latest-base")
	}
	if cfg.ValidateChecksum != conf.InitConfigDefault.ValidateChecksum {
		reasons = append(reasons, "init.validate-checksum")
	}
	if cfg.DownloadPath != conf.InitConfigDefault.DownloadPath {
		reasons = append(reasons, "init.download-path")
	}
	if cfg.DownloadPoll != conf.InitConfigDefault.DownloadPoll {
		reasons = append(reasons, "init.download-poll")
	}
	if cfg.DevInit {
		reasons = append(reasons, "init.dev-init")
	}
	if cfg.DevInit && cfg.DevInitAddress != "" {
		reasons = append(reasons, "init.dev-init-address")
	}
	if cfg.DevInit && cfg.DevInitBlockNum != conf.InitConfigDefault.DevInitBlockNum {
		reasons = append(reasons, "init.dev-init-blocknum")
	}
	if cfg.DevInit && cfg.DevMaxCodeSize != conf.InitConfigDefault.DevMaxCodeSize {
		reasons = append(reasons, "init.dev-max-code-size")
	}
	if cfg.Empty {
		reasons = append(reasons, "init.empty")
	}
	if cfg.ImportWasm {
		reasons = append(reasons, "init.import-wasm")
	}
	if cfg.ImportFile != "" {
		reasons = append(reasons, "init.import-file")
	}
	if cfg.GenesisJsonFile != "" {
		reasons = append(reasons, "init.genesis-json-file")
	}
	if cfg.AccountsPerSync != conf.InitConfigDefault.AccountsPerSync {
		reasons = append(reasons, "init.accounts-per-sync")
	}
	if cfg.ThenQuit {
		reasons = append(reasons, "init.then-quit")
	}
	if cfg.Prune != "" {
		reasons = append(reasons, "init.prune")
	}
	if cfg.PruneBloomSize != conf.InitConfigDefault.PruneBloomSize {
		reasons = append(reasons, "init.prune-bloom-size")
	}
	if cfg.PruneThreads != conf.InitConfigDefault.PruneThreads {
		reasons = append(reasons, "init.prune-threads")
	}
	if cfg.PruneTrieCleanCache != conf.InitConfigDefault.PruneTrieCleanCache {
		reasons = append(reasons, "init.prune-trie-clean-cache")
	}
	if cfg.RecreateMissingStateFrom > 0 {
		reasons = append(reasons, "init.recreate-missing-state-from")
	}
	if cfg.RebuildLocalWasm != conf.InitConfigDefault.RebuildLocalWasm {
		reasons = append(reasons, "init.rebuild-local-wasm")
	}
	if cfg.ReorgToBatch >= 0 {
		reasons = append(reasons, "init.reorg-to-batch")
	}
	if cfg.ReorgToMessageBatch >= 0 {
		reasons = append(reasons, "init.reorg-to-message-batch")
	}
	if cfg.ReorgToBlockBatch >= 0 {
		reasons = append(reasons, "init.reorg-to-block-batch")
	}
	return reasons
}
