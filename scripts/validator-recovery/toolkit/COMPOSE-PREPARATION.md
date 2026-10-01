# Compose 准备：保留 /data_mock 数据库

适用于当前服务器账户 `0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95`。
`prepare-compose.py --apply` 只生成文件、复制加密 keystore；不解密、不启动容器、不发送交易。

## 文件布局

| 用途 | 宿主机路径 | 容器路径 |
|---|---|---|
| 独立 Compose 文件 | `/data_new/validator/compose.recovered.json` | 无 |
| 新配置 | `/data_new/validator/config/nodeconfig.json` | `/nodeconfig.json`，只读 |
| 本机加密密钥副本 | `/data_new/validator/keys/recovered-65fa/account.json` | `/keys/account.json`，只读 |
| 已通过 Watchtower 验证的数据库 | `/data_mock/validator/config` | `/home/user/.arbitrum`，读写 |

Compose 支持 JSON 格式。显式使用这个文件和项目 `deriw-recovered`；旧 Compose 文件保持原样。
不使用 `/data_new/validator/config/Deriw Chain/nitro` 的演练数据库。
唯一允许读取的旧 `/data` 文件内容位于 `/data/validator/keys`，只复制匹配本机账户的一个 V3 加密文件。

## 准备

```bash
cd /data_new/scripts/production-recovery-toolkit-v2
python3 prepare-compose.py \
  --plan /data_new/scripts/validator-cutover-20260929-174736 \
  --apply \
  --out "/data_new/scripts/compose-prepared-$(date +%Y%m%d-%H%M%S)"
```

配置从已修复 DA 的 Watchtower 配置派生，只变更策略为 MakeNodes，并指定钱包目录/账户。
保留新 WASM、`start-validation-from-staked=true`、AnyTrust REST reader、验证开关及其调优参数。
旧 `nodeconfig.json` 如存在且不同，先在输出目录私密备份。
不同的既有 `compose.recovered.json` 或目标密钥会中止准备，不覆盖。
准备时 Watchtower 继续运行。生成的配置一旦启动并解锁，便有自动质押/断言能力。

## 管理员本地验证副本（不发送交易）

```bash
export PATH="$HOME/.foundry/bin:$PATH"
cast wallet address \
  --keystore /data_new/validator/keys/recovered-65fa/account.json
```

由管理员在终端的密码提示处输入。预期输出是本机账户 `0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95`（大小写无关）。
不要将密码放进配置、命令行参数或聊天；此检查不签名、不广播。
准备报告的 `decryptedAddressVerified=false` 表示准备程序只检查了地址头和复制一致性，尚未做此解密检查。

## 后续切换次序

1. 审核准备报告、实际复制账户、管理员到位及现有 resume 的执行检查。
2. 核验 resume 的真实执行回执与当前链上状态；Safe nonce 12 不等于已经执行。
3. 重新核对镜像、白名单、余额、质押状态和父链连接。
4. 使用 `docker stop -t -1 validator-recovered-watchtower` 优雅停止 Watchtower，确认所有其他数据库使用者均停止。
5. 显式用 `compose.recovered.json` 创建单个 `validator` 服务；然后用 `docker start -ai validator-recovered-makenodes` 在终端启动并输入密码。
6. 核验账户、运行验证进度和真实质押/断言交易回执，才标记生产运行通过。

上面是切换顺序说明，准备命令不会执行它。不要直接运行旧 Compose 的 `up`，不要并行启动 snapshot-sync / Watchtower / MakeNodes。
Compose 的 `manual-start` profile 防止默认启动；显式指定服务仍可以启动，因此它不是数据库互斥锁。
新容器 `restart: no`、TTY 和 stdin 开启。Nitro 使用交互输入密码；每次重启需要管理员再次输入。
Docker 前台启动支持 stdin：<https://docs.docker.com/reference/cli/docker/container/start/>。
分离终端用 Ctrl-P、Ctrl-Q；Ctrl-C 会向容器发送信号：<https://docs.docker.com/reference/cli/docker/container/attach/>。

## 范围

本准备不复制约 7 TB 数据库、不读取其内容、不移动验证检查点、不重放历史、不解密私钥、不声称 A→B 已证明。
不表示其他两台云服务器已经完成切换。三台必须分别使用各自的密钥和经过确认的数据计划。
