# 最终账户角色：维护前准备

目前仅准备，不执行生产停机、暂停、升级、退款或签名。

## 两个Safe分别做什么

| 地址 | 用途 | 已观察到的门槛 |
|---|---|---|
| 0xFbB37c66372f7B40361fBC8C8A235ae92711399D | 治理多签，经UpgradeExecutor管理Rollup暂停、恢复和WASM等配置 | 3/4 |
| 0x5E16561173EA0422549c3De12B68C8A7D3a76672 | validator断言快速确认 | 3/3 |

治理Safe在此前固定块盘点中具有UpgradeExecutor的EXECUTOR_ROLE。治理调用路径为Safe→0x1333480e92de9511dc9bb01f70901ff3ee94f613→Rollup 0xA113e2E9620a3Bc088a681eBB2C234FDbeb85e21。不能仅凭对三台validator密钥的控制，就认定能操作这个治理Safe。

此前观察的治理Safe owners（执行前重新核对）：

- 0xa0c2aed24f5474b2815b2ff61d0f5a01970217c3
- 0xc60f0ed09edd696e60574f714cbd7cfec004dd70
- 0xc63b7a2dacfa3aed4ea158f4f51ffbe020b9de4c
- 0x09ad976b259d9174f4250f0244873c3bc876e2ce

需要确认团队可安排其中至少三位签名者，以及他们使用的Safe页面或工具。只需公开身份和工具名称，不提供密钥或密码。快速确认Safe的owners是65fa、5cda、21d4，不能代替上述治理签名。

用户随后确认使用Arbitrum One上的官方Safe Global UI。界面路径已明确；三位实际签名者就绪仍需窗口确认。已准备production-safe-ui-prep-20260929/DRAFT-pause.safe.json及审阅说明，只有离线单次pause调用，未创建提案或选择nonce，不代表已批准签署。恢复及resume文件在相应实际状态固定后独立生成。

## 已完成的历史覆盖

最终角色补测final-roles-fast-confirm-20260929-085552通过：退款d38e/5cda/65fa；d38e退出；65fa/5cda/21d4同普通节点质押；普通确认；快速确认3/3及两签拒绝；暂停拒绝及恢复后早于普通期限的快确认。Safe批准不会因Rollup暂停自动清除。

相关全跨度重放、提款样本、原子失败回滚及Nitro临时签名人重启已有历史证据，不因本次通过而再复制数据库或重跑长跨度。仍不等于原三台真实进程的自动快确认/真实签名已经验收。10578是历史位置跨度，A′→B不是父链A→B执行证明。旧候选和nonce不可直接用于生产。

## 三台只读采集

Mac执行`pbcopy < /Users/super/Documents/coinw/dex/nitro-der2646/install-final-validator-prep.sh`，将完整安装块分别粘贴到三台服务器。安装目录是各机`$HOME/recovery-prep-tools`，无需另外创建/data_new或搬移数据库。

安装后，各机在工具目录执行以下命令，将ROLE替换为表中的值：

```sh
cd "$HOME/recovery-prep-tools"
ROLE=current
python3 inspect-final-validator-prep.py --role "$ROLE" --out "validator-prep-$ROLE-$(date +%Y%m%d-%H%M%S).json"
```

| 机器 | ROLE | 预期地址 | 恢复后 |
|---|---|---|---|
| 当前服务器 | current | 0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95 | 重新质押 |
| 云A | cloud-a | 0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a | 重新质押 |
| 云B | cloud-b | 0x21d4ea822a07f737c5e69f7951d517e5f2974849 | 新增质押 |

脚本仅读Docker元数据和有限日志，报告镜像ID/RepoDigests、挂载路径、Docker重启策略、显式CLI开关及公开发送身份。不打开/data、配置、密钥、数据库文件，不执行docker exec/start/stop/pull。缺失值表示未知；镜像inspect失败不必然意味着镜像不存在。若日志已轮转，身份null也不等于节点配置错误。

## 下一步材料

1. 根据三份报告核定跨云新镜像可取得的不可变标识，不能拿本地image ID当registry manifest digest。
2. 核定三机数据来源/对齐方案；/data_mock是真实父链同步副本，/data_new的演练数据库不能直接投产。/data仍受保护。
3. 对缺失的快速确认设置由负责人提供脱敏的单项配置说明，不能根据null猜测为关闭。
4. 确认治理Safe三位签名者、签名工具、维护时段及防自动拉起旧节点的方案，形成具体暂停包再请求执行授权。
5. 实际暂停后固定最终A/B与恢复调用数据，核验通过后才签署恢复；初期普通断言验收、后续快确认运行验收分别执行。

历史通过并不自动授权生产操作。本文件不包含待执行生产交易。
