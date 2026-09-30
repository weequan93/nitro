# snapshot-sync → 验证节点：本机分阶段交接

## 当前准备范围

`cutover.py prepare` 只读取现有工具包、`snapshot-sync.json` 和 Docker 元数据，生成独立配置。
不查询父链、不停止容器、不读数据库内容或 keystore、不访问 `/data` 文件。
安装与 prepare 都不启用质押。不会改动原工具包的 Safe 交易、签名或 nonce。

新容器名：`validator-recovered-watchtower`。
数据只使用 `/data_mock/validator/config`；`/data_new/validator/config` 是演练副本，不用于生产切换。
固定镜像：`sha256:b3571c0d8d4cd4e826a48895ed558927c4b003ea14050f5be3fb5ab09c62148f`。
RPC 保持 `http://127.0.0.1:8349`，WS 8372；此 Watchtower 配置未开启 metrics，验收用 RPC 和日志。
验证预录/发送/前瞻三个限额保留准备时 snapshot 配置中的正值。

## 1. 现在可执行：生成计划

```bash
(
set -euo pipefail
export PATH="$HOME/.foundry/bin:$PATH"
cd /data_new/scripts/production-recovery-toolkit-v2
PLAN="/data_new/scripts/validator-cutover-$(date +%Y%m%d-%H%M%S)"
python3 cutover.py prepare \
  --package /data_new/scripts/presign-window-20260929-154302-sequence/recovery \
  --out "$PLAN"
printf '%s\n' "$PLAN" > /data_new/scripts/validator-cutover.current
)
```

输出 `plan.json`、`watchtower.json`、`makenodes-requirements.json`。
最后一个是待完成事项，**不是可启动的 MakeNodes 配置**。
计划包含旧 validator 的挂载路径元数据，方便核对 `/keys` 的来源；不会读取目录内容。
snapshot 配置或启动时间改变后，重新 prepare，不修改旧计划来绕过检查。

## 2. 实际恢复成功后：交接给 Watchtower

先按 PRESIGN-RUNBOOK 完成 pause、recovery 的执行检查与实际回执验收。
本步骤需要 nonce 11 **父链执行交易 hash**，不是 SafeTxHash，也不是签名提案 id。
恢复需保持暂停，新 WASM 根已生效，B 为已确认恢复节点，退款仍为链上 credit。

```bash
(
set -euo pipefail
export PATH="$HOME/.foundry/bin:$PATH"
cd /data_new/scripts/production-recovery-toolkit-v2
PLAN=$(cat /data_new/scripts/validator-cutover.current)
read -rp '已执行 recovery 的父链交易 hash: ' RECOVERY_TX
python3 cutover.py start-watchtower --plan "$PLAN" --recovery-tx "$RECOVERY_TX"
)
```

工具先重新核对交易字段、Safe 成功事件、NodeCreated、恢复块状态、当前暂停/新根/节点/退款状态和本地 B 的 hash/sendRoot。
它还检查旧生产容器内是否残留 Nitro 子进程（`tail` 作为 PID 1 不能代表 Nitro 已停止）。
只有这些检查通过，才执行 `docker stop -t -1 snapshot-sync`，无强制 kill 超时。
原容器保留，原 snapshot 配置不修改；不会启动旧 validator。
释放数据库和端口后启动无密钥 Watchtower；没有 `/keys`、Docker socket、`/data` 挂载。

Watchtower 核心设置：

```json
{
  "enable": true,
  "strategy": "Watchtower",
  "start-validation-from-staked": true,
  "enable-fast-confirmation": false,
  "use-smart-contract-wallet": false,
  "only-create-wallet-contract": false
}
```

`start-validation-from-staked` 是采用可信断言作为起点，不是补做被跳过的验证。
实际起点由链上 latestStaked/confirmed 查询决定，不是配置里写死任意 B。
恢复前不得提前启动此 Watchtower 来采用旧断言。

## 3. 验收 B 之后的验证进展

```bash
(
set -euo pipefail
export PATH="$HOME/.foundry/bin:$PATH"
cd /data_new/scripts/production-recovery-toolkit-v2
PLAN=$(cat /data_new/scripts/validator-cutover.current)
python3 cutover.py check --plan "$PLAN" \
  --out "/data_new/scripts/watchtower-check-$(date +%Y%m%d-%H%M%S)"
docker logs --since 5m --tail 3000 validator-recovered-watchtower 2>&1 |
  grep -Ei 'running as validator|assume-valid|validated execution|ERROR|CRIT' |
  tail -n 30
)
```

首次尚未越过 B 会停止报告成功，等待启动/验证完成后再次 check。
要求 latestValidated 只含新 WASM 根，位置严格超过 B，验证块仍在本地链上。
间隔一段时间再检查，比较进展并审阅错误日志。一次观察不等于持续健康。
程序不会自动填写 `accepted:true`、执行 resume 或声称生产签名已验证。

## 4. 随后切入 MakeNodes

本机账户应为 `0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95`。
另外两台分别为 `0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a` 和 `0x21d4ea822a07f737c5e69f7951d517e5f2974849`。
退役的 d38e 只办理退款。

还需核对本机 `/keys` 的宿主路径，以及密码通过交互、配置还是启动脚本提供。
只提供方式和路径，不在聊天中粘贴密码、私钥、完整环境变量或完整含密钥配置。
若需要读取 `/data` 中的 keystore，由操作人另行安排允许的挂载方案，本工具不会读取或复制它。
Nitro 此版本没有 `parent-chain-wallet.password-file` 原生选项，不能凭空加入该字段。

签名环境核对完成后，另行生成正式 MakeNodes 启动配置与命令：
保留 `start-validation-from-staked=true`，关闭 fast confirmation，指定账户与真实 `/keys` 挂载。
先接受 Watchtower 运行结果，再按已审阅流程执行 resume；正式 MakeNodes 的启动会使其自动发送质押/断言交易。
两个容器不能同时使用同一份可写数据库。

## 异常处理与边界

### 首版缺少 AnyTrust reader 配置的修复

若这次容器退出并出现 `rest-aggregator.enable must be set for reader mode`，
安装 DA 修复补丁后，对原计划执行下列命令；不要重新 prepare，也不要启动 snapshot。

```bash
(
set -euo pipefail
export PATH="$HOME/.foundry/bin:$PATH"
cd /data_new/scripts/production-recovery-toolkit-v2
python3 cutover.py repair-da \
  --plan /data_new/scripts/validator-cutover-20260929-174736 --apply
)
```

工具要求失败容器已停止、身份/挂载/启动参数匹配、日志有这条特定错误、
snapshot 仍停止且数据库没有其他容器占用。它重新核对父链恢复状态，
备份原计划、配置、launch 报告，仅补入原 snapshot 配置中的 AnyTrust REST reader。
保留配置文件 inode 与权限，使已有只读 bind mount 能看到更新；只启动原 Watchtower。
新旧 DA 字段同时存在且不一致，或源配置缺少有效 REST 地址时会停止，不自行猜测。
修复后等待节点启动，再执行本文件第 3 节 check。

### 通用处理

- `start-watchtower` 留下 launch.json 后不得盲目重跑；先看 failedAfterStage 和 Docker 状态。
- 启动失败不会自动重启 snapshot。先确定新容器是否已占用数据库，再安排修复。
- 不删除数据库、不 reset validation，不回滚链上 WASM。旧生产节点也不要直接重新启动。
- 新容器 restart=no；主机重启后需人工检查。原 snapshot 容器的旧重启策略未被修改。
- 本工具只能检查本机进程。其他云节点保持协调停机由操作人确认。
- 节点接入后会写 `/data_mock`；没有再复制 7 TB 数据，也不把 mock 父链的演练数据库用于生产。
- 门槛是链下实时检查，不是父链原子锁；执行期间避免并行启动节点或修改链上治理状态。
- 已有 A′→B 重放不能证明父链 A→B，治理采用 B 的信任决定仍然成立。

## 实现依据与验证范围

本地 `staker/legacy/staker.go` 的 Initialize 调用 InitAssumeValid；
`cmd/nitro/nitro.go` 的 validatorNeedsKey 分支排除 Watchtower。
上游参考：[Nitro legacy staker](https://github.com/OffchainLabs/nitro/blob/master/staker/legacy/staker.go)。
本地单元测试覆盖参数拒绝、密钥隔离、数据库占用、回执门槛和验证进度判定。
本次准备没有在生产服务器启动新容器；运行成功需后续实际验收。
