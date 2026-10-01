# 脚本与资料索引

从 [README](README.md) 进入实际记录和执行流程。此目录由 `catalog.py --refresh` 生成，按阶段保留全部相关文件的原路径；SHA-256 和字节数见 [source-index.json](source-index.json)。

toolkit 为本次正式准备入口；diagnostics、snapshot、legacy、distribution 和 evidence 各有对应用途。固定 nonce、节点、Safe import、只读报告都属于各自观察时刻，不能据此直接执行下一次恢复。

日志、编译缓存、私钥、keystore、密码、服务器全量数据库不纳入索引。完整远程输出未传回时，只索引已保存摘要，并在 [incident.json](incident.json) 标出来源。

## 常用入口

| 用途 | 脚本 |
| --- | --- |
| 候选采集、治理包与回执 | [recovery.py](../../scripts/validator-recovery/toolkit/recovery.py)、[collect.py](../../scripts/validator-recovery/toolkit/collect.py) |
| 三笔预签与阶段执行检查 | [presign.py](../../scripts/validator-recovery/toolkit/presign.py) |
| 固定区间重放、同包 fork | [replay-span.py](../../scripts/validator-recovery/toolkit/replay-span.py)、[verify-fork.py](../../scripts/validator-recovery/toolkit/verify-fork.py) |
| Watchtower 交接与 Compose | [cutover.py](../../scripts/validator-recovery/toolkit/cutover.py)、[prepare-compose.py](../../scripts/validator-recovery/toolkit/prepare-compose.py) |
| 账户及 Fast Safe | [owner-check.py](../../scripts/validator-recovery/toolkit/owner-check.py)、[fast-safe-check.py](../../scripts/validator-recovery/toolkit/fast-safe-check.py) |
| 退款领取 | [withdraw-refund.sh](../../scripts/validator-recovery/toolkit/withdraw-refund.sh)；由账户管理员签名 |
| L3 跨链提款领取 | [通用方法与说明](../manual-withdrawal/README.md)；按原 L3 交易重新生成 proof/claim |
| Safe 黑名单与 owner 操作 | [交易生成与说明](../safe-transactions/README.md)；只生成待审阅 Builder CALL 列表 |
| 验证进度 | [snapshot-validation-metrics.py](../../scripts/validator-recovery/snapshot/snapshot-validation-metrics.py)、[inspect-snapshot-validation-gates.py](../../scripts/validator-recovery/diagnostics/inspect-snapshot-validation-gates.py) |

## 本次记录与执行流程（12）

| 文件 | 内容 / 限制 |
| --- | --- |
| [README.md](../../README.md) | 本次记录与执行流程 |
| [docs/validator-recovery/EXECUTION-RECORD.md](../../docs/validator-recovery/EXECUTION-RECORD.md) | 实际执行与证据 |
| [docs/validator-recovery/LAYOUT.md](../../docs/validator-recovery/LAYOUT.md) | 目录规划与迁移 |
| [docs/validator-recovery/README.md](../../docs/validator-recovery/README.md) | Deriw validator 恢复工作记录 |
| [docs/validator-recovery/RUNBOOK.md](../../docs/validator-recovery/RUNBOOK.md) | 恢复流程与后续验收 |
| [docs/validator-recovery/TROUBLESHOOTING.md](../../docs/validator-recovery/TROUBLESHOOTING.md) | 本次故障、判断和处置 |
| [docs/validator-recovery/VALIDATION-RESULTS.md](../../docs/validator-recovery/VALIDATION-RESULTS.md) | 归档验证记录 |
| [docs/validator-recovery/catalog.py](../../docs/validator-recovery/catalog.py) | Index and verify local recovery records; never call RPC, Docker or deployment tools. |
| [docs/validator-recovery/incident.json](../../docs/validator-recovery/incident.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [docs/validator-recovery/layout-migration.json](../../docs/validator-recovery/layout-migration.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/.gitignore](../../scripts/validator-recovery/.gitignore) | 本次记录与执行流程 |
| [scripts/validator-recovery/README.md](../../scripts/validator-recovery/README.md) | Validator 恢复脚本 |

## 正式 v2 工具与附加模块（24）

| 文件 | 内容 / 限制 |
| --- | --- |
| [scripts/validator-recovery/toolkit/COMPOSE-PREPARATION.md](../../scripts/validator-recovery/toolkit/COMPOSE-PREPARATION.md) | Compose 准备：保留 /data_mock 数据库 |
| [scripts/validator-recovery/toolkit/CUTOVER-RUNBOOK.md](../../scripts/validator-recovery/toolkit/CUTOVER-RUNBOOK.md) | snapshot-sync → 验证节点：本机分阶段交接 |
| [scripts/validator-recovery/toolkit/MANIFEST.sha256](../../scripts/validator-recovery/toolkit/MANIFEST.sha256) | 正式 v2 工具与附加模块 |
| [scripts/validator-recovery/toolkit/PRESIGN-RUNBOOK.md](../../scripts/validator-recovery/toolkit/PRESIGN-RUNBOOK.md) | 一次集中签署，操作员分阶段执行 |
| [scripts/validator-recovery/toolkit/RUNBOOK.md](../../scripts/validator-recovery/toolkit/RUNBOOK.md) | 执行手册：先准备，获得具体授权后才进入生产窗口 |
| [scripts/validator-recovery/toolkit/START-HERE.md](../../scripts/validator-recovery/toolkit/START-HERE.md) | Deriw 恢复工具包 v2 |
| [scripts/validator-recovery/toolkit/TEST-RESULTS.md](../../scripts/validator-recovery/toolkit/TEST-RESULTS.md) | v2 测试记录（2026-09-29） |
| [scripts/validator-recovery/toolkit/collect.py](../../scripts/validator-recovery/toolkit/collect.py) | Collect a pinned candidate and locate local A-prime; no chain writes or WASM replay. |
| [scripts/validator-recovery/toolkit/core.py](../../scripts/validator-recovery/toolkit/core.py) | Deriw preparation primitives. Public clients have an explicit read-only RPC allowlist. |
| [scripts/validator-recovery/toolkit/cutover.py](../../scripts/validator-recovery/toolkit/cutover.py) | Prepare and gate a keyless Watchtower handover. Never enables MakeNodes. |
| [scripts/validator-recovery/toolkit/fast-safe-check.py](../../scripts/validator-recovery/toolkit/fast-safe-check.py) | Read-only fast-confirmation Safe identity check; cannot enumerate offline signatures. |
| [scripts/validator-recovery/toolkit/owner-check.py](../../scripts/validator-recovery/toolkit/owner-check.py) | Read-only refund/runtime checks. Does not read keystores or send transactions. |
| [scripts/validator-recovery/toolkit/prepare-compose.py](../../scripts/validator-recovery/toolkit/prepare-compose.py) | Prepare Compose/config and copy one encrypted keystore. Never starts a validator. |
| [scripts/validator-recovery/toolkit/presign.py](../../scripts/validator-recovery/toolkit/presign.py) | Prepare fixed consecutive Safe transactions; only owned forks may send transactions. |
| [scripts/validator-recovery/toolkit/recovery.py](../../scripts/validator-recovery/toolkit/recovery.py) | Prepare/review Deriw recovery packages. Never signs or broadcasts to production. |
| [scripts/validator-recovery/toolkit/reference/FAST-CONFIRMATION-ADDENDUM-20260929.md](../../scripts/validator-recovery/toolkit/reference/FAST-CONFIRMATION-ADDENDUM-20260929.md) | 快速确认补充：纳入生产维护流程 |
| [scripts/validator-recovery/toolkit/reference/FINAL-ROLES-PREPARATION-20260929.md](../../scripts/validator-recovery/toolkit/reference/FINAL-ROLES-PREPARATION-20260929.md) | 最终账户角色：维护前准备 |
| [scripts/validator-recovery/toolkit/reference/PAUSE-CONTENT-REVIEW.md](../../scripts/validator-recovery/toolkit/reference/PAUSE-CONTENT-REVIEW.md) | 暂停交易专项复核结果 |
| [scripts/validator-recovery/toolkit/reference/operator-mapping.json](../../scripts/validator-recovery/toolkit/reference/operator-mapping.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/toolkit/reference/v2-fast-safe-check.json](../../scripts/validator-recovery/toolkit/reference/v2-fast-safe-check.json) | 保存状态：fast_safe_identity_passed |
| [scripts/validator-recovery/toolkit/reference/v2-fork-summary.json](../../scripts/validator-recovery/toolkit/reference/v2-fork-summary.json) | 保存状态：exact_package_fork_passed_with_arbsys_log_shim |
| [scripts/validator-recovery/toolkit/replay-span.py](../../scripts/validator-recovery/toolkit/replay-span.py) | Sequential, resumable replay of the input audit local A-prime -> B interval. |
| [scripts/validator-recovery/toolkit/verify-fork.py](../../scripts/validator-recovery/toolkit/verify-fork.py) | Execute the exact prepared CALL list only on a newly owned loopback Anvil 31337. |
| [scripts/validator-recovery/toolkit/withdraw-refund.sh](../../scripts/validator-recovery/toolkit/withdraw-refund.sh) | 正式 v2 工具与附加模块 |

## 诊断、角色与候选采集（22）

| 文件 | 内容 / 限制 |
| --- | --- |
| [scripts/validator-recovery/diagnostics/README.md](../../scripts/validator-recovery/diagnostics/README.md) | 诊断与候选采集 |
| [scripts/validator-recovery/diagnostics/audit-recovery-message-span.py](../../scripts/validator-recovery/diagnostics/audit-recovery-message-span.py) | Read-only historical A-position/B message-span audit. No signing or chain mutation. |
| [scripts/validator-recovery/diagnostics/audit-safe-pause-live.py](../../scripts/validator-recovery/diagnostics/audit-safe-pause-live.py) | Read-only public inventory plus an owned local 31337 fork pause rehearsal. |
| [scripts/validator-recovery/diagnostics/check-cutover-data-readonly.sh](../../scripts/validator-recovery/diagnostics/check-cutover-data-readonly.sh) | 诊断、角色与候选采集 |
| [scripts/validator-recovery/diagnostics/check-recovery-candidate-drift.py](../../scripts/validator-recovery/diagnostics/check-recovery-candidate-drift.py) | Read-only comparison of a rehearsed checkpoint with fresh parent-chain authority data. |
| [scripts/validator-recovery/diagnostics/check-snapshot-validation-progress.sh](../../scripts/validator-recovery/diagnostics/check-snapshot-validation-progress.sh) | 诊断、角色与候选采集 |
| [scripts/validator-recovery/diagnostics/collect-production-candidate.py](../../scripts/validator-recovery/diagnostics/collect-production-candidate.py) | Read-only candidate collection. Never signs or declares production readiness. |
| [scripts/validator-recovery/diagnostics/diagnose-snapshot-sync.py](../../scripts/validator-recovery/diagnostics/diagnose-snapshot-sync.py) | Read-only Linux sync diagnosis. Writes reports only; no keys/config dumps. |
| [scripts/validator-recovery/diagnostics/inspect-data-new-reuse.py](../../scripts/validator-recovery/diagnostics/inspect-data-new-reuse.py) | Inventory prior /data_new use without opening databases or starting a node. |
| [scripts/validator-recovery/diagnostics/inspect-final-validator-prep.py](../../scripts/validator-recovery/diagnostics/inspect-final-validator-prep.py) | Read-only Docker preparation report. Does not open configs, keys or databases. |
| [scripts/validator-recovery/diagnostics/inspect-original-validator-runtime.py](../../scripts/validator-recovery/diagnostics/inspect-original-validator-runtime.py) | Read-only recovery inventory. Never opens keystores or starts/stops a container. |
| [scripts/validator-recovery/diagnostics/inspect-original-wallet-metadata.py](../../scripts/validator-recovery/diagnostics/inspect-original-wallet-metadata.py) | Read public keystore headers and selected Docker launch switches; never unlock/sign. |
| [scripts/validator-recovery/diagnostics/inspect-recovery-node-sources.py](../../scripts/validator-recovery/diagnostics/inspect-recovery-node-sources.py) | Read-only local Docker/RPC inventory; excludes commands, environment and wallet config. |
| [scripts/validator-recovery/diagnostics/inspect-recovery-operator.py](../../scripts/validator-recovery/diagnostics/inspect-recovery-operator.py) | Portable read-only inventory for a selected recovery validator operator (standard library only). |
| [scripts/validator-recovery/diagnostics/inspect-remote-validator.py](../../scripts/validator-recovery/diagnostics/inspect-remote-validator.py) | Portable read-only inventory for the original validator operator (standard library only). |
| [scripts/validator-recovery/diagnostics/inspect-snapshot-cutover-config.py](../../scripts/validator-recovery/diagnostics/inspect-snapshot-cutover-config.py) | Read selected snapshot-sync config fields only; no DB/key reads or mutations. |
| [scripts/validator-recovery/diagnostics/inspect-snapshot-validation-gates.py](../../scripts/validator-recovery/diagnostics/inspect-snapshot-validation-gates.py) | Read selected validation controls, process arguments and stop markers; no changes. |
| [scripts/validator-recovery/diagnostics/mkdiag.py](../../scripts/validator-recovery/diagnostics/mkdiag.py) | 诊断、角色与候选采集 |
| [scripts/validator-recovery/diagnostics/probe-production-participants.py](../../scripts/validator-recovery/diagnostics/probe-production-participants.py) | Pinned read-only production staker, pending-node and Safe inventory. No signing. |
| [scripts/validator-recovery/diagnostics/probe-recovery-authority.py](../../scripts/validator-recovery/diagnostics/probe-recovery-authority.py) | Read-only Rollup proxy inventory. No keys, signing or transactions. |
| [scripts/validator-recovery/diagnostics/probe-snapshot-next-validation.sh](../../scripts/validator-recovery/diagnostics/probe-snapshot-next-validation.sh) | 诊断、角色与候选采集 |
| [scripts/validator-recovery/diagnostics/run-diagnostic.py](../../scripts/validator-recovery/diagnostics/run-diagnostic.py) | Run two diagnostic binaries offline with only exported witness files mounted. |

## 数据准备、验证进度与调优（12）

| 文件 | 内容 / 限制 |
| --- | --- |
| [scripts/validator-recovery/snapshot/README.md](../../scripts/validator-recovery/snapshot/README.md) | Snapshot 数据与验证准备 |
| [scripts/validator-recovery/snapshot/fix-snapshot-validation-limit.py](../../scripts/validator-recovery/snapshot/fix-snapshot-validation-limit.py) | Plan or apply one config fix; restart only snapshot-sync, never production validators. |
| [scripts/validator-recovery/snapshot/prepare-data-new-parent-check.py](../../scripts/validator-recovery/snapshot/prepare-data-new-parent-check.py) | Prepare a non-staking real-parent check of /data_new; --start explicitly opens it writable. |
| [scripts/validator-recovery/snapshot/prepare-snapshot-diagnostics.py](../../scripts/validator-recovery/snapshot/prepare-snapshot-diagnostics.py) | Enable on-demand diagnostics on the stopped snapshot-sync node, without staking. |
| [scripts/validator-recovery/snapshot/prepare-snapshot-sync.py](../../scripts/validator-recovery/snapshot/prepare-snapshot-sync.py) | Prepare a non-staking snapshot-sync config. Does not start Nitro or edit its DB. |
| [scripts/validator-recovery/snapshot/replace-recovery-test-db.py](../../scripts/validator-recovery/snapshot/replace-recovery-test-db.py) | Replace the explicitly authorized disposable DB. Never stops validator-nitro-1. |
| [scripts/validator-recovery/snapshot/snapshot-validation-metrics.py](../../scripts/validator-recovery/snapshot/snapshot-validation-metrics.py) | Enable or disable local validation metrics; restart only snapshot-sync. |
| [scripts/validator-recovery/snapshot/tune-snapshot-recording-eight.py](../../scripts/validator-recovery/snapshot/tune-snapshot-recording-eight.py) | Plan/apply/rollback bounded validation preparation tuning; restart only snapshot-sync. |
| [scripts/validator-recovery/snapshot/tune-snapshot-recording-four-to-sixteen.py](../../scripts/validator-recovery/snapshot/tune-snapshot-recording-four-to-sixteen.py) | Plan/apply/rollback bounded validation preparation tuning; restart only snapshot-sync. |
| [scripts/validator-recovery/snapshot/tune-snapshot-recording-sixteen.py](../../scripts/validator-recovery/snapshot/tune-snapshot-recording-sixteen.py) | Plan/apply/rollback bounded validation preparation tuning; restart only snapshot-sync. |
| [scripts/validator-recovery/snapshot/tune-snapshot-recording-thirtytwo.py](../../scripts/validator-recovery/snapshot/tune-snapshot-recording-thirtytwo.py) | Plan/apply/rollback bounded validation preparation tuning; restart only snapshot-sync. |
| [scripts/validator-recovery/snapshot/tune-snapshot-validation.py](../../scripts/validator-recovery/snapshot/tune-snapshot-validation.py) | Plan/apply/rollback bounded validation preparation tuning; restart only snapshot-sync. |

## 固定区间与执行重放（5）

| 文件 | 内容 / 限制 |
| --- | --- |
| [scripts/validator-recovery/legacy/tools/export-native-input.py](../../scripts/validator-recovery/legacy/tools/export-native-input.py) | Export the saved single-batch, Keccak-only case for cmd/replay native mode. |
| [scripts/validator-recovery/legacy/tools/replay-production-candidate.py](../../scripts/validator-recovery/legacy/tools/replay-production-candidate.py) | Replay the collected candidate message via local arbdebug; never send transactions. |
| [scripts/validator-recovery/legacy/tools/replay-retained-message-span.py](../../scripts/validator-recovery/legacy/tools/replay-retained-message-span.py) | Sequential, resumable replay of the fixed local A-prime -> B interval. |
| [scripts/validator-recovery/legacy/tools/replay-three-staker-followup.py](../../scripts/validator-recovery/legacy/tools/replay-three-staker-followup.py) | Replay B to the fixed parent inbox boundary for the next runtime rehearsal. |
| [scripts/validator-recovery/legacy/tools/replay-three-staker-span.py](../../scripts/validator-recovery/legacy/tools/replay-three-staker-span.py) | Replay the pinned 10578-message local A-prime to B interval; no transactions. |

## 历史 fork、契约与运行演练（21）

| 文件 | 内容 / 限制 |
| --- | --- |
| [scripts/validator-recovery/legacy/README.md](../../scripts/validator-recovery/legacy/README.md) | 历史演练与工具包 |
| [scripts/validator-recovery/legacy/tools/mock-validator-recovery.py](../../scripts/validator-recovery/legacy/tools/mock-validator-recovery.py) | Contract-only recovery rehearsal. Starts a private Anvil; NEVER signs production transactions. |
| [scripts/validator-recovery/legacy/tools/pack-candidate-fork.py](../../scripts/validator-recovery/legacy/tools/pack-candidate-fork.py) | Build a pinned fork snapshot from collected candidate and replay evidence; read-only RPCs. |
| [scripts/validator-recovery/legacy/tools/prepare-recovery-fork.py](../../scripts/validator-recovery/legacy/tools/prepare-recovery-fork.py) | Collect a recovery checkpoint for fork analysis. Read-only; no signing or transactions. |
| [scripts/validator-recovery/legacy/tools/rehearse-atomic-recovery.py](../../scripts/validator-recovery/legacy/tools/rehearse-atomic-recovery.py) | Historical private Anvil only: Safe batch success and late-failure rollback. |
| [scripts/validator-recovery/legacy/tools/rehearse-atomic-span-recovery.py](../../scripts/validator-recovery/legacy/tools/rehearse-atomic-span-recovery.py) | Fixed historical Anvil: atomic recovery with the audited 7838 position span. |
| [scripts/validator-recovery/legacy/tools/rehearse-final-roles-fast-confirm.py](../../scripts/validator-recovery/legacy/tools/rehearse-final-roles-fast-confirm.py) | Historical owned Anvil: retire d38e, stake 65fa/5cda/21d4, test 3/3 fast Safe. |
| [scripts/validator-recovery/legacy/tools/rehearse-nitro-autosubmit.py](../../scripts/validator-recovery/legacy/tools/rehearse-nitro-autosubmit.py) | Fresh historical Anvil + Nitro MakeNodes with a NEW disposable signing key. |
| [scripts/validator-recovery/legacy/tools/rehearse-nitro-span-restart.py](../../scripts/validator-recovery/legacy/tools/rehearse-nitro-span-restart.py) | 7838 governance + Nitro signing and graceful restart, historical fork only. |
| [scripts/validator-recovery/legacy/tools/rehearse-paused-recovery.py](../../scripts/validator-recovery/legacy/tools/rehearse-paused-recovery.py) | Owned local Anvil only. Stop recovery before resume; never opens a validator database. |
| [scripts/validator-recovery/legacy/tools/rehearse-safe-recovery.py](../../scripts/validator-recovery/legacy/tools/rehearse-safe-recovery.py) | Local fork only: recovery via Safe approveHash + execTransaction, no production keys. |
| [scripts/validator-recovery/legacy/tools/rehearse-three-staker-atomic.py](../../scripts/validator-recovery/legacy/tools/rehearse-three-staker-atomic.py) | Pinned three-staker Anvil rehearsal; no Docker or production transactions. |
| [scripts/validator-recovery/legacy/tools/rehearse-three-staker-nitro-restart.py](../../scripts/validator-recovery/legacy/tools/rehearse-three-staker-nitro-restart.py) | Three-staker historical atomic recovery + Nitro signing and graceful restart. |
| [scripts/validator-recovery/legacy/tools/rehearse-three-staker-rejoin.py](../../scripts/validator-recovery/legacy/tools/rehearse-three-staker-rejoin.py) | Owned historical Anvil: atomic recovery, resume, three refunds/rejoins, normal confirmation. |
| [scripts/validator-recovery/legacy/tools/rehearse-three-staker-withdrawals.py](../../scripts/validator-recovery/legacy/tools/rehearse-three-staker-withdrawals.py) | Historical fork: spent preservation, natural unspent payment, duplicate rejection. |
| [scripts/validator-recovery/legacy/tools/rehearse-two-staker-rejoin.py](../../scripts/validator-recovery/legacy/tools/rehearse-two-staker-rejoin.py) | Historical Anvil-only test: refund both known stakers, rejoin, ordinary confirm. |
| [scripts/validator-recovery/legacy/tools/rehearse-withdrawal-recovery.py](../../scripts/validator-recovery/legacy/tools/rehearse-withdrawal-recovery.py) | Fixed-candidate local fork withdrawal tests. Never broadcasts to production. |
| [scripts/validator-recovery/legacy/tools/resume-assertion-path.py](../../scripts/validator-recovery/legacy/tools/resume-assertion-path.py) | Validate a contiguous local message path with one WASM root, stopping at any mismatch. |
| [scripts/validator-recovery/legacy/tools/resume-original-staker-current-fork.py](../../scripts/validator-recovery/legacy/tools/resume-original-staker-current-fork.py) | Continue the specific paused 18549 Anvil: Safe resume, original EOA restake, |
| [scripts/validator-recovery/legacy/tools/run-validator-recovery-mock.sh](../../scripts/validator-recovery/legacy/tools/run-validator-recovery-mock.sh) | 历史 fork、契约与运行演练 |
| [scripts/validator-recovery/legacy/tools/start-paused-watchtower.py](../../scripts/validator-recovery/legacy/tools/start-paused-watchtower.py) | Start an isolated Watchtower on the replaced disposable DB and paused local fork. |

## 离线测试与需另行授权的集成入口（28）

| 文件 | 内容 / 限制 |
| --- | --- |
| [scripts/validator-recovery/legacy/packages/original-staker-rejoin-tools/test-original-staker-rejoin.py](../../scripts/validator-recovery/legacy/packages/original-staker-rejoin-tools/test-original-staker-rejoin.py) | Replay a Nitro-created assertion from a refunded original account on a SECOND Anvil. |
| [scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/test_prepare_window.py](../../scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/test_prepare_window.py) | 测试入口；integration_* 可连接 RPC，未在本次归档中执行。 |
| [scripts/validator-recovery/legacy/packages/validator-recovery-mock/test-mock-validator-recovery.py](../../scripts/validator-recovery/legacy/packages/validator-recovery-mock/test-mock-validator-recovery.py) | Local tests of rehearsal guards and postconditions; does not access any RPC. |
| [scripts/validator-recovery/legacy/tests/fixtures/withdrawal-145d72e9/receipt.json](../../scripts/validator-recovery/legacy/tests/fixtures/withdrawal-145d72e9/receipt.json) | 保存状态：0x1 |
| [scripts/validator-recovery/legacy/tests/test-atomic-span-recovery.py](../../scripts/validator-recovery/legacy/tests/test-atomic-span-recovery.py) | Offline checks for parameter capture, evidence guards and result handling. |
| [scripts/validator-recovery/legacy/tests/test-final-roles-fast-confirm.py](../../scripts/validator-recovery/legacy/tests/test-final-roles-fast-confirm.py) | 测试入口；integration_* 可连接 RPC，未在本次归档中执行。 |
| [scripts/validator-recovery/legacy/tests/test-final-validator-prep.py](../../scripts/validator-recovery/legacy/tests/test-final-validator-prep.py) | 测试入口；integration_* 可连接 RPC，未在本次归档中执行。 |
| [scripts/validator-recovery/legacy/tests/test-mock-validator-recovery.py](../../scripts/validator-recovery/legacy/tests/test-mock-validator-recovery.py) | Local tests of rehearsal guards and postconditions; does not access any RPC. |
| [scripts/validator-recovery/legacy/tests/test-nitro-span-restart.py](../../scripts/validator-recovery/legacy/tests/test-nitro-span-restart.py) | Offline runtime adapter guards; no Docker, RPC or node DB access. |
| [scripts/validator-recovery/legacy/tests/test-original-staker-rejoin.py](../../scripts/validator-recovery/legacy/tests/test-original-staker-rejoin.py) | Replay a Nitro-created assertion from a refunded original account on a SECOND Anvil. |
| [scripts/validator-recovery/legacy/tests/test-recovery-message-span.py](../../scripts/validator-recovery/legacy/tests/test-recovery-message-span.py) | Offline boundary/guard checks; no RPC access. |
| [scripts/validator-recovery/legacy/tests/test-retained-message-span.py](../../scripts/validator-recovery/legacy/tests/test-retained-message-span.py) | Offline end-to-end resume/failure checks with deterministic RPC fixtures. |
| [scripts/validator-recovery/legacy/tests/test-three-staker-atomic.py](../../scripts/validator-recovery/legacy/tests/test-three-staker-atomic.py) | 测试入口；integration_* 可连接 RPC，未在本次归档中执行。 |
| [scripts/validator-recovery/legacy/tests/test-three-staker-followup.py](../../scripts/validator-recovery/legacy/tests/test-three-staker-followup.py) | 测试入口；integration_* 可连接 RPC，未在本次归档中执行。 |
| [scripts/validator-recovery/legacy/tests/test-three-staker-nitro-restart.py](../../scripts/validator-recovery/legacy/tests/test-three-staker-nitro-restart.py) | 测试入口；integration_* 可连接 RPC，未在本次归档中执行。 |
| [scripts/validator-recovery/legacy/tests/test-three-staker-rejoin.py](../../scripts/validator-recovery/legacy/tests/test-three-staker-rejoin.py) | 测试入口；integration_* 可连接 RPC，未在本次归档中执行。 |
| [scripts/validator-recovery/legacy/tests/test-three-staker-span.py](../../scripts/validator-recovery/legacy/tests/test-three-staker-span.py) | Offline pinned fixture / first-message / resume regression checks. |
| [scripts/validator-recovery/legacy/tests/test-three-staker-withdrawals.py](../../scripts/validator-recovery/legacy/tests/test-three-staker-withdrawals.py) | 测试入口；integration_* 可连接 RPC，未在本次归档中执行。 |
| [scripts/validator-recovery/legacy/tests/test-two-staker-rejoin.py](../../scripts/validator-recovery/legacy/tests/test-two-staker-rejoin.py) | Offline guard/ABI tests. No RPC, keys, Docker or node database access. |
| [scripts/validator-recovery/tests/run-offline.py](../../scripts/validator-recovery/tests/run-offline.py) | Run existing offline recovery tests and syntax checks; skip RPC/fork CLI integrations. |
| [scripts/validator-recovery/toolkit/tests/integration_current.py](../../scripts/validator-recovery/toolkit/tests/integration_current.py) | Contract-only smoke fixture from current pending old-WASM assertions. NEVER production evidence. |
| [scripts/validator-recovery/toolkit/tests/integration_historical.py](../../scripts/validator-recovery/toolkit/tests/integration_historical.py) | Test-only historical fixture, not a production package generator or replay. |
| [scripts/validator-recovery/toolkit/tests/safe-receipt-fixture.json](../../scripts/validator-recovery/toolkit/tests/safe-receipt-fixture.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/toolkit/tests/state-fixture.json](../../scripts/validator-recovery/toolkit/tests/state-fixture.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/toolkit/tests/test_compose_preparation.py](../../scripts/validator-recovery/toolkit/tests/test_compose_preparation.py) | 测试入口；integration_* 可连接 RPC，未在本次归档中执行。 |
| [scripts/validator-recovery/toolkit/tests/test_cutover.py](../../scripts/validator-recovery/toolkit/tests/test_cutover.py) | 测试入口；integration_* 可连接 RPC，未在本次归档中执行。 |
| [scripts/validator-recovery/toolkit/tests/test_presign.py](../../scripts/validator-recovery/toolkit/tests/test_presign.py) | 测试入口；integration_* 可连接 RPC，未在本次归档中执行。 |
| [scripts/validator-recovery/toolkit/tests/test_toolkit.py](../../scripts/validator-recovery/toolkit/tests/test_toolkit.py) | 测试入口；integration_* 可连接 RPC，未在本次归档中执行。 |

## 本地保存的报告、审阅包与回执（190）

| 文件 | 内容 / 限制 |
| --- | --- |
| [scripts/validator-recovery/evidence/ATOMIC-SPAN-MOCK.md](../../scripts/validator-recovery/evidence/ATOMIC-SPAN-MOCK.md) | 7838 原子恢复演练 |
| [scripts/validator-recovery/evidence/DATA-CUTOVER-PREPARATION.md](../../scripts/validator-recovery/evidence/DATA-CUTOVER-PREPARATION.md) | 新镜像节点数据切换准备 |
| [scripts/validator-recovery/evidence/DATA-NEW-REUSE-REVIEW.md](../../scripts/validator-recovery/evidence/DATA-NEW-REUSE-REVIEW.md) | /data_new 复用评估：待运行检查 |
| [scripts/validator-recovery/evidence/DECISION.md](../../scripts/validator-recovery/evidence/DECISION.md) | Recovery decision — 2026-09-23 investigation |
| [scripts/validator-recovery/evidence/FAST-CONFIRMATION-ADDENDUM-20260929.md](../../scripts/validator-recovery/evidence/FAST-CONFIRMATION-ADDENDUM-20260929.md) | 快速确认补充：纳入生产维护流程 |
| [scripts/validator-recovery/evidence/FINAL-ROLES-PREPARATION-20260929.md](../../scripts/validator-recovery/evidence/FINAL-ROLES-PREPARATION-20260929.md) | 最终账户角色：维护前准备 |
| [scripts/validator-recovery/evidence/GOVERNANCE-SPAN-MOCK.md](../../scripts/validator-recovery/evidence/GOVERNANCE-SPAN-MOCK.md) | 7838位置跨度治理参数演练 |
| [scripts/validator-recovery/evidence/NITRO-AUTOSUBMIT-MOCK.md](../../scripts/validator-recovery/evidence/NITRO-AUTOSUBMIT-MOCK.md) | 临时账户的 Nitro 自动签名/提交演练 |
| [scripts/validator-recovery/evidence/NITRO-SPAN-RESTART.md](../../scripts/validator-recovery/evidence/NITRO-SPAN-RESTART.md) | 7838参数的Nitro自动提交及正常重启 |
| [scripts/validator-recovery/evidence/OPERATOR-READONLY-CHECK-20260929.md](../../scripts/validator-recovery/evidence/OPERATOR-READONLY-CHECK-20260929.md) | 三账户节点负责人：只读核对 |
| [scripts/validator-recovery/evidence/ORIGINAL-VALIDATOR-OPERATOR-PLAN.md](../../scripts/validator-recovery/evidence/ORIGINAL-VALIDATOR-OPERATOR-PLAN.md) | 原 validator 负责人配合操作说明 |
| [scripts/validator-recovery/evidence/PAUSE-105018-REVIEW.md](../../scripts/validator-recovery/evidence/PAUSE-105018-REVIEW.md) | 暂停包 pause-review-20260929-105018 审阅记录 |
| [scripts/validator-recovery/evidence/PAUSE-REVIEW-20260929.md](../../scripts/validator-recovery/evidence/PAUSE-REVIEW-20260929.md) | 独立治理暂停：调用审阅页 |
| [scripts/validator-recovery/evidence/PRODUCTION-EXECUTION-DRAFT.md](../../scripts/validator-recovery/evidence/PRODUCTION-EXECUTION-DRAFT.md) | 生产恢复执行草案 |
| [scripts/validator-recovery/evidence/PRODUCTION-PLAN-CURRENT-20260929.md](../../scripts/validator-recovery/evidence/PRODUCTION-PLAN-CURRENT-20260929.md) | Deriw 三账户生产恢复准备方案 |
| [scripts/validator-recovery/evidence/PRODUCTION-RECOVERY-PLAN.md](../../scripts/validator-recovery/evidence/PRODUCTION-RECOVERY-PLAN.md) | Deriw 生产恢复准备方案 |
| [scripts/validator-recovery/evidence/PRODUCTION-REFRESH-20260928.md](../../scripts/validator-recovery/evidence/PRODUCTION-REFRESH-20260928.md) | 生产只读盘点更新 |
| [scripts/validator-recovery/evidence/PRODUCTION-TOOLING-READINESS-20260929.md](../../scripts/validator-recovery/evidence/PRODUCTION-TOOLING-READINESS-20260929.md) | 实际文件核对：尚未达到“只等暂停后填参数” |
| [scripts/validator-recovery/evidence/PROGRESS-20260925.md](../../scripts/validator-recovery/evidence/PROGRESS-20260925.md) | Deriw 恢复演练记录（2026-09-25） |
| [scripts/validator-recovery/evidence/README.md](../../scripts/validator-recovery/evidence/README.md) | 证据归档 |
| [scripts/validator-recovery/evidence/RECOVERY-PARAMETERS.md](../../scripts/validator-recovery/evidence/RECOVERY-PARAMETERS.md) | 恢复断言参数审阅（2026-09-27） |
| [scripts/validator-recovery/evidence/RETAINED-SPAN-REPLAY.md](../../scripts/validator-recovery/evidence/RETAINED-SPAN-REPLAY.md) | 保留链A′→B连续消息重放 |
| [scripts/validator-recovery/evidence/SIMULATION-ACCEPTANCE.md](../../scripts/validator-recovery/evidence/SIMULATION-ACCEPTANCE.md) | 恢复模拟验收汇总 |
| [scripts/validator-recovery/evidence/SIMULATION-CLOSEOUT-20260928.md](../../scripts/validator-recovery/evidence/SIMULATION-CLOSEOUT-20260928.md) | 历史恢复模拟收尾与生产准备 |
| [scripts/validator-recovery/evidence/THREE-STAKER-ATOMIC.md](../../scripts/validator-recovery/evidence/THREE-STAKER-ATOMIC.md) | 三账户原子恢复模拟 |
| [scripts/validator-recovery/evidence/THREE-STAKER-CLOSEOUT-20260929.md](../../scripts/validator-recovery/evidence/THREE-STAKER-CLOSEOUT-20260929.md) | 三账户恢复模拟结论与生产准备 |
| [scripts/validator-recovery/evidence/THREE-STAKER-SPAN.md](../../scripts/validator-recovery/evidence/THREE-STAKER-SPAN.md) | 三账户新候选：10578条本地重放 |
| [scripts/validator-recovery/evidence/TWO-STAKER-REJOIN-MOCK.md](../../scripts/validator-recovery/evidence/TWO-STAKER-REJOIN-MOCK.md) | 两个历史质押账户共同恢复演练 |
| [scripts/validator-recovery/evidence/check-executed-recovery-03e938.py](../../scripts/validator-recovery/evidence/check-executed-recovery-03e938.py) | Read-only public RPC verification against the locally reviewed Safe fields. |
| [scripts/validator-recovery/evidence/check-first-recovered-fast-confirm.py](../../scripts/validator-recovery/evidence/check-first-recovered-fast-confirm.py) | 本地保存的报告、审阅包与回执 |
| [scripts/validator-recovery/evidence/check-pause-105018.py](../../scripts/validator-recovery/evidence/check-pause-105018.py) | Offline review of the user-reported pause fields. No RPC or signing. |
| [scripts/validator-recovery/evidence/confirmation-tx-request.json](../../scripts/validator-recovery/evidence/confirmation-tx-request.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/confirmation-tx.json](../../scripts/validator-recovery/evidence/confirmation-tx.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/confirmations.json](../../scripts/validator-recovery/evidence/confirmations.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/confirmed-endpoint-public.json](../../scripts/validator-recovery/evidence/confirmed-endpoint-public.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/current-request.json](../../scripts/validator-recovery/evidence/current-request.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/current-response.json](../../scripts/validator-recovery/evidence/current-response.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/executed-recovery-03e938/receipt.json](../../scripts/validator-recovery/evidence/executed-recovery-03e938/receipt.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/executed-recovery-03e938/summary.json](../../scripts/validator-recovery/evidence/executed-recovery-03e938/summary.json) | 保存状态：public_rpc_recovery_receipt_and_state_matched |
| [scripts/validator-recovery/evidence/first-recovered-fast-confirm.json](../../scripts/validator-recovery/evidence/first-recovered-fast-confirm.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/operator-mapping/address-check-20260929-003541.json](../../scripts/validator-recovery/evidence/operator-mapping/address-check-20260929-003541.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/operator-mapping/check-addresses.py](../../scripts/validator-recovery/evidence/operator-mapping/check-addresses.py) | Read-only public Arbitrum One address-to-stake comparison. No keys or writes. |
| [scripts/validator-recovery/evidence/operator-mapping/check-fast-confirm-safe.py](../../scripts/validator-recovery/evidence/operator-mapping/check-fast-confirm-safe.py) | Read-only fast confirmer and Safe roles inventory; no private keys or transactions. |
| [scripts/validator-recovery/evidence/operator-mapping/fast-confirm-safe-20260929-003917.json](../../scripts/validator-recovery/evidence/operator-mapping/fast-confirm-safe-20260929-003917.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/operator-mapping/user-mapping-20260929.json](../../scripts/validator-recovery/evidence/operator-mapping/user-mapping-20260929.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/pause-call-review-20260929.json](../../scripts/validator-recovery/evidence/pause-call-review-20260929.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/presign-window-20260929-154302-review/recovery.safe-fields.json](../../scripts/validator-recovery/evidence/presign-window-20260929-154302-review/recovery.safe-fields.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/presign-window-20260929-154302-review/recovery.safe-import.json](../../scripts/validator-recovery/evidence/presign-window-20260929-154302-review/recovery.safe-import.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/presign-window-20260929-154302-review/resume.safe-fields.json](../../scripts/validator-recovery/evidence/presign-window-20260929-154302-review/resume.safe-fields.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/presign-window-20260929-154302-review/resume.safe-import.json](../../scripts/validator-recovery/evidence/presign-window-20260929-154302-review/resume.safe-import.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/presign-window-20260929-154302-review/review.json](../../scripts/validator-recovery/evidence/presign-window-20260929-154302-review/review.json) | 保存状态：offline_pasted_calls_and_hashes_matched |
| [scripts/validator-recovery/evidence/reported-results/atomic-span-mock-20260928-084057.json](../../scripts/validator-recovery/evidence/reported-results/atomic-span-mock-20260928-084057.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/db-replacement-20260928-062110-757499.json](../../scripts/validator-recovery/evidence/reported-results/db-replacement-20260928-062110-757499.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/final-roles-fast-confirm-20260929-085552.json](../../scripts/validator-recovery/evidence/reported-results/final-roles-fast-confirm-20260929-085552.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/governance-span-mock-20260928-081940.json](../../scripts/validator-recovery/evidence/reported-results/governance-span-mock-20260928-081940.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/nitro-span-restart-20260928-084752.json](../../scripts/validator-recovery/evidence/reported-results/nitro-span-restart-20260928-084752.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/production-participants-refresh-20260928-085449.json](../../scripts/validator-recovery/evidence/reported-results/production-participants-refresh-20260928-085449.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/production-refresh-20260928-085449.json](../../scripts/validator-recovery/evidence/reported-results/production-refresh-20260928-085449.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/production-window-prep-20260929-082849.json](../../scripts/validator-recovery/evidence/reported-results/production-window-prep-20260929-082849.json) | 保存状态：preparation_inventory_passed |
| [scripts/validator-recovery/evidence/reported-results/three-staker-atomic-mock-20260928-133005.json](../../scripts/validator-recovery/evidence/reported-results/three-staker-atomic-mock-20260928-133005.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/three-staker-candidate-20260928-090119.json](../../scripts/validator-recovery/evidence/reported-results/three-staker-candidate-20260928-090119.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/three-staker-followup-20260928-134148.json](../../scripts/validator-recovery/evidence/reported-results/three-staker-followup-20260928-134148.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/three-staker-fork-20260928-090354.json](../../scripts/validator-recovery/evidence/reported-results/three-staker-fork-20260928-090354.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/three-staker-nitro-restart-20260928-231557-partial.json](../../scripts/validator-recovery/evidence/reported-results/three-staker-nitro-restart-20260928-231557-partial.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/three-staker-participants-20260928-090354.json](../../scripts/validator-recovery/evidence/reported-results/three-staker-participants-20260928-090354.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/three-staker-rejoin-mock-20260928-140344.json](../../scripts/validator-recovery/evidence/reported-results/three-staker-rejoin-mock-20260928-140344.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/three-staker-replay-20260928-090354.json](../../scripts/validator-recovery/evidence/reported-results/three-staker-replay-20260928-090354.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/three-staker-runtime-db-precheck-20260928.txt](../../scripts/validator-recovery/evidence/reported-results/three-staker-runtime-db-precheck-20260928.txt) | 本地保存的报告、审阅包与回执 |
| [scripts/validator-recovery/evidence/reported-results/three-staker-span-20260928-090354.json](../../scripts/validator-recovery/evidence/reported-results/three-staker-span-20260928-090354.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/three-staker-span-replay-20260928-091247-passed.json](../../scripts/validator-recovery/evidence/reported-results/three-staker-span-replay-20260928-091247-passed.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/three-staker-span-replay-20260928-091247-started.json](../../scripts/validator-recovery/evidence/reported-results/three-staker-span-replay-20260928-091247-started.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/reported-results/three-staker-withdrawals-20260929-004616-partial.json](../../scripts/validator-recovery/evidence/reported-results/three-staker-withdrawals-20260929-004616-partial.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/review-presign-154302.py](../../scripts/validator-recovery/evidence/review-presign-154302.py) | Offline re-encoding and EIP-712 check of pasted production proposal imports. |
| [scripts/validator-recovery/evidence/safe-child-code.json](../../scripts/validator-recovery/evidence/safe-child-code.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/safe-fbb37-request.json](../../scripts/validator-recovery/evidence/safe-fbb37-request.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/safe-fbb37-response.json](../../scripts/validator-recovery/evidence/safe-fbb37-response.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/safe-latest-request.json](../../scripts/validator-recovery/evidence/safe-latest-request.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/safe-latest-response.json](../../scripts/validator-recovery/evidence/safe-latest-response.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/safe-request.json](../../scripts/validator-recovery/evidence/safe-request.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/safe-response.json](../../scripts/validator-recovery/evidence/safe-response.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/safe-ui-prep-20260929/DRAFT-pause.safe.json](../../scripts/validator-recovery/evidence/safe-ui-prep-20260929/DRAFT-pause.safe.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/safe-ui-prep-20260929/PAUSE-CONTENT-REVIEW.md](../../scripts/validator-recovery/evidence/safe-ui-prep-20260929/PAUSE-CONTENT-REVIEW.md) | 暂停交易专项复核结果 |
| [scripts/validator-recovery/evidence/safe-ui-prep-20260929/README.md](../../scripts/validator-recovery/evidence/safe-ui-prep-20260929/README.md) | 官方Safe Global UI：治理交易准备 |
| [scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-1/summary.json](../../scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-1/summary.json) | 保存状态：review_stopped |
| [scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-2/live-inventory.json](../../scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-2/live-inventory.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-2/summary.json](../../scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-2/summary.json) | 保存状态：review_stopped |
| [scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-3/live-inventory.json](../../scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-3/live-inventory.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-3/queue-response.txt](../../scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-3/queue-response.txt) | 本地保存的报告、审阅包与回执 |
| [scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-3/receipt-and-queue-review.json](../../scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-3/receipt-and-queue-review.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-3/steps.json](../../scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-3/steps.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-3/summary.json](../../scripts/validator-recovery/evidence/safe-ui-prep-20260929/live-pause-audit-3/summary.json) | 保存状态：pause_review_and_fork_passed |
| [scripts/validator-recovery/evidence/safe-ui-prep-20260929/review.json](../../scripts/validator-recovery/evidence/safe-ui-prep-20260929/review.json) | 保存状态：offline_draft_prepared |
| [scripts/validator-recovery/evidence/stake-details-decoded.json](../../scripts/validator-recovery/evidence/stake-details-decoded.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/stake-details-request.json](../../scripts/validator-recovery/evidence/stake-details-request.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/stake-details-response.json](../../scripts/validator-recovery/evidence/stake-details-response.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/stakes-request.json](../../scripts/validator-recovery/evidence/stakes-request.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/stakes-response.json](../../scripts/validator-recovery/evidence/stakes-response.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package/REVIEW.md](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package/REVIEW.md) | recovery：需审阅，程序没有广播能力 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package/checksums.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package/checksums.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package/package.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package/package.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package/safe-import.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package/safe-import.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-2/REVIEW.md](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-2/REVIEW.md) | recovery：需审阅，程序没有广播能力 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-2/checksums.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-2/checksums.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-2/package.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-2/package.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-2/safe-import.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-2/safe-import.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-3/REVIEW.md](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-3/REVIEW.md) | recovery：需审阅，程序没有广播能力 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-3/checksums.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-3/checksums.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-3/package.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-3/package.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-3/safe-import.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-3/safe-import.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-4/REVIEW.md](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-4/REVIEW.md) | recovery：需审阅，程序没有广播能力 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-4/checksums.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-4/checksums.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-4/package.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-4/package.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-4/safe-import.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-contract-package-4/safe-import.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/negative-trace.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/negative-trace.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/negative.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/negative.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x0974272bc3f2f6613a4cec5389cfd7564b02e0b760a5d7abe4da495d65a1f3c7.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x0974272bc3f2f6613a4cec5389cfd7564b02e0b760a5d7abe4da495d65a1f3c7.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x22d3ca6722556d7f5ec0d2c3098718cfcef14aea6e388b4ba69e80cad026cf15.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x22d3ca6722556d7f5ec0d2c3098718cfcef14aea6e388b4ba69e80cad026cf15.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x23df4dc6cc9fbc9ebc5c39da7b5536767b7c3b415d320e755ff350d16efe1b30.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x23df4dc6cc9fbc9ebc5c39da7b5536767b7c3b415d320e755ff350d16efe1b30.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x2ab62be0f5de95e3172523285ea66a29fd441a344987012ff6181a33c06f2191.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x2ab62be0f5de95e3172523285ea66a29fd441a344987012ff6181a33c06f2191.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x33ac5886a4c864916632e2e711793619965263cae31603a651df1e080a4849e4.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x33ac5886a4c864916632e2e711793619965263cae31603a651df1e080a4849e4.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x3950cc085fbc196eaaa6248589f42b1d8654da62ad61d0bbbeba7005cd72f2c5.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x3950cc085fbc196eaaa6248589f42b1d8654da62ad61d0bbbeba7005cd72f2c5.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x3d365c494ea0add5dcb4ef44bc72231a3810a4bdfd82dc531d9b94a2c7126cfe.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x3d365c494ea0add5dcb4ef44bc72231a3810a4bdfd82dc531d9b94a2c7126cfe.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x49969850fa96d04ba7580743e8a9d8fad2ba55696e8970906825defb5480ecc1.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x49969850fa96d04ba7580743e8a9d8fad2ba55696e8970906825defb5480ecc1.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x52e97c3dd9ebeaaf83c139bad2c9b3c00c948f08f0c73a692c40c1ec65a71697.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x52e97c3dd9ebeaaf83c139bad2c9b3c00c948f08f0c73a692c40c1ec65a71697.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x683ec56c24910bce4626034ab37cae120d2d19460e1e5213487a588ed5b6680c.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x683ec56c24910bce4626034ab37cae120d2d19460e1e5213487a588ed5b6680c.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x6e03e8e9cee475e9010228253d52a0c1818fb65bc8d251492c52083dd6159946.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x6e03e8e9cee475e9010228253d52a0c1818fb65bc8d251492c52083dd6159946.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x7e78fdef5ebc7a9e3c73010946640652bf8387d0122516752843d0062c56aa00.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x7e78fdef5ebc7a9e3c73010946640652bf8387d0122516752843d0062c56aa00.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x8f59109bab8a4250e5bc00ec0f52dd60de588808736f8ac5beee25b5186a6cac.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x8f59109bab8a4250e5bc00ec0f52dd60de588808736f8ac5beee25b5186a6cac.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x95561923f4c52fbe1da790aefe1213b80953ffc676cf0cdd17152411b1f51907.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x95561923f4c52fbe1da790aefe1213b80953ffc676cf0cdd17152411b1f51907.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x96843e7bcdce975a46ac3b647e3b10a790ebfdb89d0a536cfd75511489ae74f4.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0x96843e7bcdce975a46ac3b647e3b10a790ebfdb89d0a536cfd75511489ae74f4.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0xa3ec9912295d664fed65cb74c1d420a06b3dc336d5e4518b6789f66fd48b9667.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0xa3ec9912295d664fed65cb74c1d420a06b3dc336d5e4518b6789f66fd48b9667.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0xbe75eec130057543477b727441c5bb3a2a5758b47113120721dc68ec4079afe4.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0xbe75eec130057543477b727441c5bb3a2a5758b47113120721dc68ec4079afe4.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0xd3612687cab715f45ba541fb1b02ba349f709ddc635d0e62c22e2f9556b2895e.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0xd3612687cab715f45ba541fb1b02ba349f709ddc635d0e62c22e2f9556b2895e.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0xfd7da7ff79036f8129eda03727503b19db703b4a270c98e093936eb67544555f.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/receipt-0xfd7da7ff79036f8129eda03727503b19db703b4a270c98e093936eb67544555f.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/refund-test.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/refund-test.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/resume-test.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/resume-test.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/simulation-pause.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/simulation-pause.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/success.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/success.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/summary.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-followups/summary.json) | 保存状态：exact_package_fork_passed_with_arbsys_log_shim |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run/summary.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run/summary.json) | 保存状态：stopped |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/negative.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/negative.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x040c8dbee233714e0eeaa511e12931e2f710ac3dedd44beebf32fb8894b1e4b4.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x040c8dbee233714e0eeaa511e12931e2f710ac3dedd44beebf32fb8894b1e4b4.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x307a63d9bbf3edbeeba355cae62646554beaa98c6a6720b9d9bafa80dfb11963.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x307a63d9bbf3edbeeba355cae62646554beaa98c6a6720b9d9bafa80dfb11963.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x358acb96f9dad0aae497f63a7bfef9de8dbee3c700d781489126f3802dd3df6f.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x358acb96f9dad0aae497f63a7bfef9de8dbee3c700d781489126f3802dd3df6f.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x424203e37a22a655566e2ca54488568f1819a03cee916d889fc060e8f104bdb4.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x424203e37a22a655566e2ca54488568f1819a03cee916d889fc060e8f104bdb4.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x449e419a0a50c25c0fd35e9c03bd4db785401dd894d19bac6ba5a3be81342f87.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x449e419a0a50c25c0fd35e9c03bd4db785401dd894d19bac6ba5a3be81342f87.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x68badb1ccc2e110e3b99d4693fc154c3e17f5768656dc3cea152a44347010a70.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x68badb1ccc2e110e3b99d4693fc154c3e17f5768656dc3cea152a44347010a70.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x7458420285a4402a47752dad42ec83c1849542d148f7928e4a30eb55d2d2bd76.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x7458420285a4402a47752dad42ec83c1849542d148f7928e4a30eb55d2d2bd76.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x9d4e52dd729bd8fafef3bc4d43ea47acaae570501015d79f1f59ac02d356be95.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0x9d4e52dd729bd8fafef3bc4d43ea47acaae570501015d79f1f59ac02d356be95.json) | 保存状态：0x0 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0xa2df733ca1c9f3cf4dd764fd430b1723d728593d9ef023412e2b3e5eecaffe72.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0xa2df733ca1c9f3cf4dd764fd430b1723d728593d9ef023412e2b3e5eecaffe72.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0xa51ae46fcd9b2660ba2535d926c80e6db6a891057fced8c9f24413b32929e4ae.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0xa51ae46fcd9b2660ba2535d926c80e6db6a891057fced8c9f24413b32929e4ae.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0xd1d0f9791cb23fb18f5c72d5e9f993d2f3da5fb3e9de5fd220ce659d76ae13c7.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0xd1d0f9791cb23fb18f5c72d5e9f993d2f3da5fb3e9de5fd220ce659d76ae13c7.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0xd7a0bbd9fbb38338515ceca7d0220b28fdeaf49868c415d5b806c41f232b7b0c.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/receipt-0xd7a0bbd9fbb38338515ceca7d0220b28fdeaf49868c415d5b806c41f232b7b0c.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/simulation-pause.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/simulation-pause.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/summary.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-2/summary.json) | 保存状态：stopped |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/negative-trace.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/negative-trace.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0x079ff5a25a59da7854f15ede4e83065093acf7eec771286cbbe2ecdab4ba5a73.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0x079ff5a25a59da7854f15ede4e83065093acf7eec771286cbbe2ecdab4ba5a73.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0x1f460ea51a335bee0323abb09b4dda63e2c0b6eb5043c1042a410ba79653f3d1.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0x1f460ea51a335bee0323abb09b4dda63e2c0b6eb5043c1042a410ba79653f3d1.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0x305498715b15c34febdb7504df2a070bc5a69e9a207bc6551343eb0c7c4fccc8.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0x305498715b15c34febdb7504df2a070bc5a69e9a207bc6551343eb0c7c4fccc8.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0x4a70a322889ec7166ca7c65bb3ce7b733b107b778d032b09e0d7bd5847d5a84c.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0x4a70a322889ec7166ca7c65bb3ce7b733b107b778d032b09e0d7bd5847d5a84c.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0x6e97d8cd858dc7a0ca0989b408d2e6bc5407b187cbc7afdbc49c1eeb3c0a355f.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0x6e97d8cd858dc7a0ca0989b408d2e6bc5407b187cbc7afdbc49c1eeb3c0a355f.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0xa6dc8284a5fe5db61a617d6e1c8588bee92b4c9d0bb5dff2549118fe37c4db58.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0xa6dc8284a5fe5db61a617d6e1c8588bee92b4c9d0bb5dff2549118fe37c4db58.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0xbb031419c4285826cedb9793b0b1b0a00263700d1bc8c21b2c4035ffac5ea1ad.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0xbb031419c4285826cedb9793b0b1b0a00263700d1bc8c21b2c4035ffac5ea1ad.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0xcf191156d33488de4134ff968da0ab08bed537c2146540d82d47e149417ef330.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/receipt-0xcf191156d33488de4134ff968da0ab08bed537c2146540d82d47e149417ef330.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/simulation-pause.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/simulation-pause.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/summary.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-3/summary.json) | 保存状态：stopped |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/negative-trace.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/negative-trace.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/negative.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/negative.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x22d3ca6722556d7f5ec0d2c3098718cfcef14aea6e388b4ba69e80cad026cf15.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x22d3ca6722556d7f5ec0d2c3098718cfcef14aea6e388b4ba69e80cad026cf15.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x2ab62be0f5de95e3172523285ea66a29fd441a344987012ff6181a33c06f2191.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x2ab62be0f5de95e3172523285ea66a29fd441a344987012ff6181a33c06f2191.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x33ac5886a4c864916632e2e711793619965263cae31603a651df1e080a4849e4.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x33ac5886a4c864916632e2e711793619965263cae31603a651df1e080a4849e4.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x49969850fa96d04ba7580743e8a9d8fad2ba55696e8970906825defb5480ecc1.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x49969850fa96d04ba7580743e8a9d8fad2ba55696e8970906825defb5480ecc1.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x7e78fdef5ebc7a9e3c73010946640652bf8387d0122516752843d0062c56aa00.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x7e78fdef5ebc7a9e3c73010946640652bf8387d0122516752843d0062c56aa00.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x8f59109bab8a4250e5bc00ec0f52dd60de588808736f8ac5beee25b5186a6cac.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x8f59109bab8a4250e5bc00ec0f52dd60de588808736f8ac5beee25b5186a6cac.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x95561923f4c52fbe1da790aefe1213b80953ffc676cf0cdd17152411b1f51907.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x95561923f4c52fbe1da790aefe1213b80953ffc676cf0cdd17152411b1f51907.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x96843e7bcdce975a46ac3b647e3b10a790ebfdb89d0a536cfd75511489ae74f4.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0x96843e7bcdce975a46ac3b647e3b10a790ebfdb89d0a536cfd75511489ae74f4.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0xa3ec9912295d664fed65cb74c1d420a06b3dc336d5e4518b6789f66fd48b9667.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0xa3ec9912295d664fed65cb74c1d420a06b3dc336d5e4518b6789f66fd48b9667.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0xbe75eec130057543477b727441c5bb3a2a5758b47113120721dc68ec4079afe4.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0xbe75eec130057543477b727441c5bb3a2a5758b47113120721dc68ec4079afe4.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0xd3612687cab715f45ba541fb1b02ba349f709ddc635d0e62c22e2f9556b2895e.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0xd3612687cab715f45ba541fb1b02ba349f709ddc635d0e62c22e2f9556b2895e.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0xfd7da7ff79036f8129eda03727503b19db703b4a270c98e093936eb67544555f.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/receipt-0xfd7da7ff79036f8129eda03727503b19db703b4a270c98e093936eb67544555f.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/simulation-pause.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/simulation-pause.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/success.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/success.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/summary.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/current-fork-run-4/summary.json) | 保存状态：exact_package_fork_passed_with_arbsys_log_shim |
| [scripts/validator-recovery/evidence/toolkit-v2-validation/fast-safe-live.json](../../scripts/validator-recovery/evidence/toolkit-v2-validation/fast-safe-live.json) | 保存状态：fast_safe_identity_passed |
| [scripts/validator-recovery/evidence/withdrawal-fixture-three/inspect-spent.py](../../scripts/validator-recovery/evidence/withdrawal-fixture-three/inspect-spent.py) | 本地保存的报告、审阅包与回执 |
| [scripts/validator-recovery/evidence/zombie-cleanup-57531e3c/check.py](../../scripts/validator-recovery/evidence/zombie-cleanup-57531e3c/check.py) | Read-only verification of cleanup receipt and a pinned current parent state. |
| [scripts/validator-recovery/evidence/zombie-cleanup-57531e3c/raw-state.json](../../scripts/validator-recovery/evidence/zombie-cleanup-57531e3c/raw-state.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/zombie-cleanup-57531e3c/receipt.json](../../scripts/validator-recovery/evidence/zombie-cleanup-57531e3c/receipt.json) | 保存状态：0x1 |
| [scripts/validator-recovery/evidence/zombie-cleanup-57531e3c/summary.json](../../scripts/validator-recovery/evidence/zombie-cleanup-57531e3c/summary.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |
| [scripts/validator-recovery/evidence/zombie-cleanup-57531e3c/transaction.json](../../scripts/validator-recovery/evidence/zombie-cleanup-57531e3c/transaction.json) | JSON 参数 / 报告 / 交易内容；查看来源与观察时间。 |

## 安装器、构建器与历史分发压缩包（37）

| 文件 | 内容 / 限制 |
| --- | --- |
| [scripts/validator-recovery/distribution/README.md](../../scripts/validator-recovery/distribution/README.md) | 安装与分发 |
| [scripts/validator-recovery/distribution/builders/build-compose-prep-addon.py](../../scripts/validator-recovery/distribution/builders/build-compose-prep-addon.py) | 读取 toolkit 构建新分发包；输出 distribution/generated，不覆盖历史包。 |
| [scripts/validator-recovery/distribution/builders/build-cutover-addon.py](../../scripts/validator-recovery/distribution/builders/build-cutover-addon.py) | 读取 toolkit 构建新分发包；输出 distribution/generated，不覆盖历史包。 |
| [scripts/validator-recovery/distribution/builders/build-cutover-da-fix.py](../../scripts/validator-recovery/distribution/builders/build-cutover-da-fix.py) | 读取 toolkit 构建新分发包；输出 distribution/generated，不覆盖历史包。 |
| [scripts/validator-recovery/distribution/builders/build-presign-addon.py](../../scripts/validator-recovery/distribution/builders/build-presign-addon.py) | 读取 toolkit 构建新分发包；输出 distribution/generated，不覆盖历史包。 |
| [scripts/validator-recovery/distribution/builders/build-recovery-toolkit-bundle.py](../../scripts/validator-recovery/distribution/builders/build-recovery-toolkit-bundle.py) | 读取 toolkit 构建新分发包；输出 distribution/generated，不覆盖历史包。 |
| [scripts/validator-recovery/distribution/bundles/atomic-span-recovery-tools.tar.gz](../../scripts/validator-recovery/distribution/bundles/atomic-span-recovery-tools.tar.gz) | 历史分发快照；只校验文件，不解压或执行。 |
| [scripts/validator-recovery/distribution/bundles/original-staker-rejoin-tools.tar.gz](../../scripts/validator-recovery/distribution/bundles/original-staker-rejoin-tools.tar.gz) | 历史分发快照；只校验文件，不解压或执行。 |
| [scripts/validator-recovery/distribution/bundles/production-recovery-toolkit-v2.tar.gz](../../scripts/validator-recovery/distribution/bundles/production-recovery-toolkit-v2.tar.gz) | 历史分发快照；只校验文件，不解压或执行。 |
| [scripts/validator-recovery/distribution/bundles/recovery-compose-prep.gz](../../scripts/validator-recovery/distribution/bundles/recovery-compose-prep.gz) | 历史分发快照；只校验文件，不解压或执行。 |
| [scripts/validator-recovery/distribution/bundles/recovery-cutover-addon.gz](../../scripts/validator-recovery/distribution/bundles/recovery-cutover-addon.gz) | 历史分发快照；只校验文件，不解压或执行。 |
| [scripts/validator-recovery/distribution/bundles/recovery-cutover-da-fix.gz](../../scripts/validator-recovery/distribution/bundles/recovery-cutover-da-fix.gz) | 历史分发快照；只校验文件，不解压或执行。 |
| [scripts/validator-recovery/distribution/bundles/recovery-presign-addon.gz](../../scripts/validator-recovery/distribution/bundles/recovery-presign-addon.gz) | 历史分发快照；只校验文件，不解压或执行。 |
| [scripts/validator-recovery/distribution/bundles/validator-recovery-mock.tar.gz](../../scripts/validator-recovery/distribution/bundles/validator-recovery-mock.tar.gz) | 历史分发快照；只校验文件，不解压或执行。 |
| [scripts/validator-recovery/distribution/installers/install-final-roles-fast-confirm.sh](../../scripts/validator-recovery/distribution/installers/install-final-roles-fast-confirm.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-final-validator-prep.sh](../../scripts/validator-recovery/distribution/installers/install-final-validator-prep.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-fix-snapshot-validation-limit.sh](../../scripts/validator-recovery/distribution/installers/install-fix-snapshot-validation-limit.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-inspect-data-new-reuse.sh](../../scripts/validator-recovery/distribution/installers/install-inspect-data-new-reuse.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-inspect-snapshot-cutover-config.sh](../../scripts/validator-recovery/distribution/installers/install-inspect-snapshot-cutover-config.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-inspect-snapshot-validation-gates.sh](../../scripts/validator-recovery/distribution/installers/install-inspect-snapshot-validation-gates.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-prepare-data-new-parent-check.sh](../../scripts/validator-recovery/distribution/installers/install-prepare-data-new-parent-check.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-production-recovery-kit.sh](../../scripts/validator-recovery/distribution/installers/install-production-recovery-kit.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-production-recovery-toolkit-v2.sh](../../scripts/validator-recovery/distribution/installers/install-production-recovery-toolkit-v2.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-recovery-compose-prep.sh](../../scripts/validator-recovery/distribution/installers/install-recovery-compose-prep.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-recovery-cutover-addon.sh](../../scripts/validator-recovery/distribution/installers/install-recovery-cutover-addon.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-recovery-cutover-da-fix.sh](../../scripts/validator-recovery/distribution/installers/install-recovery-cutover-da-fix.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-recovery-operator-inspector.sh](../../scripts/validator-recovery/distribution/installers/install-recovery-operator-inspector.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-recovery-presign-addon.sh](../../scripts/validator-recovery/distribution/installers/install-recovery-presign-addon.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-snapshot-validation-metrics.sh](../../scripts/validator-recovery/distribution/installers/install-snapshot-validation-metrics.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-three-staker-nitro-restart.sh](../../scripts/validator-recovery/distribution/installers/install-three-staker-nitro-restart.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-three-staker-rejoin.sh](../../scripts/validator-recovery/distribution/installers/install-three-staker-rejoin.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-three-staker-withdrawals.sh](../../scripts/validator-recovery/distribution/installers/install-three-staker-withdrawals.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-tune-snapshot-recording-eight.sh](../../scripts/validator-recovery/distribution/installers/install-tune-snapshot-recording-eight.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-tune-snapshot-recording-four-to-sixteen.sh](../../scripts/validator-recovery/distribution/installers/install-tune-snapshot-recording-four-to-sixteen.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-tune-snapshot-recording-sixteen.sh](../../scripts/validator-recovery/distribution/installers/install-tune-snapshot-recording-sixteen.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-tune-snapshot-recording-thirtytwo.sh](../../scripts/validator-recovery/distribution/installers/install-tune-snapshot-recording-thirtytwo.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |
| [scripts/validator-recovery/distribution/installers/install-tune-snapshot-validation.sh](../../scripts/validator-recovery/distribution/installers/install-tune-snapshot-validation.sh) | 安装器；内嵌 payload 与当前源码可能属于不同版本。 |

## 早期工具副本与历史说明（25）

| 文件 | 内容 / 限制 |
| --- | --- |
| [docs/validator-recovery/history/VALIDATOR-RECOVERY-MOCK.md](../../docs/validator-recovery/history/VALIDATOR-RECOVERY-MOCK.md) | 8149 validator 恢复演练 |
| [docs/validator-recovery/history/validation-125022703-recovery-plan.md](../../docs/validator-recovery/history/validation-125022703-recovery-plan.md) | 125022703：执行差异证据与保留历史的修复方案 |
| [docs/validator-recovery/history/validator-recovery-coordination.md](../../docs/validator-recovery/history/validator-recovery-coordination.md) | Deriw validator 恢复协作清单 |
| [scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/mock-validator-recovery.py](../../scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/mock-validator-recovery.py) | Contract-only recovery rehearsal. Starts a private Anvil; NEVER signs production transactions. |
| [scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/rehearse-atomic-recovery.py](../../scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/rehearse-atomic-recovery.py) | Historical private Anvil only: Safe batch success and late-failure rollback. |
| [scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/rehearse-atomic-span-recovery.py](../../scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/rehearse-atomic-span-recovery.py) | Fixed historical Anvil: atomic recovery with the audited 7838 position span. |
| [scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/rehearse-safe-recovery.py](../../scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/rehearse-safe-recovery.py) | Local fork only: recovery via Safe approveHash + execTransaction, no production keys. |
| [scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/rehearse-two-staker-rejoin.py](../../scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/rehearse-two-staker-rejoin.py) | Historical Anvil-only test: refund both known stakers, rejoin, ordinary confirm. |
| [scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/replay-retained-message-span.py](../../scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/replay-retained-message-span.py) | Sequential, resumable replay of the fixed local A-prime -> B interval. |
| [scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/resume-original-staker-current-fork.py](../../scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/resume-original-staker-current-fork.py) | Continue the specific paused 18549 Anvil: Safe resume, original EOA restake, |
| [scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/validator-recovery-evidence/ATOMIC-SPAN-MOCK.md](../../scripts/validator-recovery/legacy/packages/atomic-span-recovery-tools/validator-recovery-evidence/ATOMIC-SPAN-MOCK.md) | 7838 原子恢复演练 |
| [scripts/validator-recovery/legacy/packages/original-staker-rejoin-tools/README.md](../../scripts/validator-recovery/legacy/packages/original-staker-rejoin-tools/README.md) | 原账户退款后重新质押：第二个 Anvil 测试 |
| [scripts/validator-recovery/legacy/packages/original-staker-rejoin-tools/run.sh](../../scripts/validator-recovery/legacy/packages/original-staker-rejoin-tools/run.sh) | 早期工具副本与历史说明 |
| [scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/MANIFEST.sha256](../../scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/MANIFEST.sha256) | 早期工具副本与历史说明 |
| [scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/RUNBOOK.md](../../scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/RUNBOOK.md) | Deriw 三节点恢复：维护准备包 |
| [scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/inspect-recovery-operator.py](../../scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/inspect-recovery-operator.py) | Portable read-only inventory for a selected recovery validator operator (standard library only). |
| [scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/operator-roster.json](../../scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/operator-roster.json) | 保存状态：template_not_executable |
| [scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/prepare-window.py](../../scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/prepare-window.py) | Collect a pinned read-only inventory and a non-signable pause review draft. |
| [scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/probe-production-participants.py](../../scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/probe-production-participants.py) | Pinned read-only production staker, pending-node and Safe inventory. No signing. |
| [scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/probe-recovery-authority.py](../../scripts/validator-recovery/legacy/packages/production-recovery-kit-20260929/probe-recovery-authority.py) | Read-only Rollup proxy inventory. No keys, signing or transactions. |
| [scripts/validator-recovery/legacy/packages/validator-recovery-mock/VALIDATOR-RECOVERY-MOCK.md](../../scripts/validator-recovery/legacy/packages/validator-recovery-mock/VALIDATOR-RECOVERY-MOCK.md) | 8149 validator 恢复演练 |
| [scripts/validator-recovery/legacy/packages/validator-recovery-mock/mock-validator-recovery.py](../../scripts/validator-recovery/legacy/packages/validator-recovery-mock/mock-validator-recovery.py) | Contract-only recovery rehearsal. Starts a private Anvil; NEVER signs production transactions. |
| [scripts/validator-recovery/legacy/packages/validator-recovery-mock/prepare-recovery-fork.py](../../scripts/validator-recovery/legacy/packages/validator-recovery-mock/prepare-recovery-fork.py) | Collect a recovery checkpoint for fork analysis. Read-only; no signing or transactions. |
| [scripts/validator-recovery/legacy/packages/validator-recovery-mock/probe-recovery-authority.py](../../scripts/validator-recovery/legacy/packages/validator-recovery-mock/probe-recovery-authority.py) | Read-only Rollup proxy inventory. No keys, signing or transactions. |
| [scripts/validator-recovery/legacy/packages/validator-recovery-mock/run-validator-recovery-mock.sh](../../scripts/validator-recovery/legacy/packages/validator-recovery-mock/run-validator-recovery-mock.sh) | 早期工具副本与历史说明 |

## 分支中的实现参考（22）

| 文件 | 内容 / 限制 |
| --- | --- |
| [arbnode/batch_poster.go](../../arbnode/batch_poster.go) | 本分支的协议或运行实现参考。 |
| [arbos/addressMap/addressMap.go](../../arbos/addressMap/addressMap.go) | 本分支的协议或运行实现参考。 |
| [arbos/blacklist/blacklist.go](../../arbos/blacklist/blacklist.go) | 本分支的协议或运行实现参考。 |
| [cmd/genericconf/liveconfig.go](../../cmd/genericconf/liveconfig.go) | 本分支的协议或运行实现参考。 |
| [cmd/nitro/config/config.go](../../cmd/nitro/config/config.go) | 本分支的协议或运行实现参考。 |
| [cmd/nitro/nitro.go](../../cmd/nitro/nitro.go) | 本分支的协议或运行实现参考。 |
| [contracts-legacy/src/bridge/SequencerInbox.sol](../../contracts-legacy/src/bridge/SequencerInbox.sol) | 本分支的协议或运行实现参考。 |
| [contracts-legacy/src/rollup/RollupAdminLogic.sol](../../contracts-legacy/src/rollup/RollupAdminLogic.sol) | 本分支的协议或运行实现参考。 |
| [contracts-legacy/src/rollup/RollupCore.sol](../../contracts-legacy/src/rollup/RollupCore.sol) | 本分支的协议或运行实现参考。 |
| [contracts-legacy/src/rollup/RollupUserLogic.sol](../../contracts-legacy/src/rollup/RollupUserLogic.sol) | 本分支的协议或运行实现参考。 |
| [execution/gethexec/block_recorder.go](../../execution/gethexec/block_recorder.go) | 本分支的协议或运行实现参考。 |
| [execution/gethexec/block_recorder_test.go](../../execution/gethexec/block_recorder_test.go) | 本分支的协议或运行实现参考。 |
| [execution/gethexec/legacy_blacklist_preimages.go](../../execution/gethexec/legacy_blacklist_preimages.go) | 本分支的协议或运行实现参考。 |
| [execution/gethexec/legacy_blacklist_preimages_test.go](../../execution/gethexec/legacy_blacklist_preimages_test.go) | 本分支的协议或运行实现参考。 |
| [execution/gethexec/legacy_fee_preimages.go](../../execution/gethexec/legacy_fee_preimages.go) | 本分支的协议或运行实现参考。 |
| [execution/gethexec/legacy_recording_hooks.go](../../execution/gethexec/legacy_recording_hooks.go) | 本分支的协议或运行实现参考。 |
| [execution/gethexec/sequencer.go](../../execution/gethexec/sequencer.go) | 本分支的协议或运行实现参考。 |
| [scripts/validation-122408091-findings.md](../../scripts/validation-122408091-findings.md) | Message 122408091: legacy replay witness gap |
| [staker/block_validator.go](../../staker/block_validator.go) | 本分支的协议或运行实现参考。 |
| [staker/legacy/fast_confirm.go](../../staker/legacy/fast_confirm.go) | 本分支的协议或运行实现参考。 |
| [staker/legacy/staker.go](../../staker/legacy/staker.go) | 本分支的协议或运行实现参考。 |
| [staker/stateless_block_validator.go](../../staker/stateless_block_validator.go) | 本分支的协议或运行实现参考。 |

## 通用 L3 手动提款方法与说明（5）

| 文件 | 内容 / 限制 |
| --- | --- |
| [docs/manual-withdrawal/.gitignore](../../docs/manual-withdrawal/.gitignore) | 通用 L3 手动提款方法与说明 |
| [docs/manual-withdrawal/README.md](../../docs/manual-withdrawal/README.md) | Deriw L3 manual withdrawal |
| [docs/manual-withdrawal/claim-with-wallet.js](../../docs/manual-withdrawal/claim-with-wallet.js) | 通用 L3 手动提款方法与说明 |
| [docs/manual-withdrawal/prepare.py](../../docs/manual-withdrawal/prepare.py) | Check Deriw L3 withdrawals and prepare Outbox claims; never sign or broadcast. |
| [docs/manual-withdrawal/tests/test_prepare.py](../../docs/manual-withdrawal/tests/test_prepare.py) | 通用 L3 手动提款方法与说明 |

## 通用 Safe 黑名单与 owner 交易生成（4）

| 文件 | 内容 / 限制 |
| --- | --- |
| [docs/safe-transactions/.gitignore](../../docs/safe-transactions/.gitignore) | 通用 Safe 黑名单与 owner 交易生成 |
| [docs/safe-transactions/README.md](../../docs/safe-transactions/README.md) | Safe transactions: blacklist and owners |
| [docs/safe-transactions/generate.py](../../docs/safe-transactions/generate.py) | Generate Safe Builder imports for blacklist additions and owner grants; offline only. |
| [docs/safe-transactions/tests/test_generate.py](../../docs/safe-transactions/tests/test_generate.py) | 通用 Safe 黑名单与 owner 交易生成 |
