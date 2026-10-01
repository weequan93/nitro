# 临时账户的 Nitro 自动签名/提交演练

2026-09-28：用户要求先完成模拟，再做生产。原账户所在机器的实际签名配合暂缓。

## 目的与范围

新建历史 Anvil fork（127.0.0.1:18550，chainId31337），经已有 Safe 路径完成历史恢复。
生成临时私钥，仅在新fork授予白名单及余额，让真实Nitro MakeNodes自动创建并确认
普通断言42887。脚本不模拟此测试地址的身份，不代替Nitro构造普通创建/确认交易。
Safe及退款账户仍使用模拟身份准备治理状态，与测试账户实际签名证据分开记录。
本次前置采用顺序恢复，原子恢复已有独立通过证据，不声称本轮覆盖全部测试。

## 运行

新增文件：rehearse-nitro-autosubmit.py。
依赖同目录已有resume-original-staker-current-fork.py、rehearse-safe-recovery.py及
现有mock-validator-recovery.py（同目录或recovery-mock-tools目录）。
需要现有Python3、Docker、cast、anvil、findmnt和当前终端导出的ARCHIVE_RPC。

```bash
export PATH="$HOME/.foundry/bin:$PATH"
export ARCHIVE_RPC
cd /data_new/scripts

python3 rehearse-nitro-autosubmit.py \
  candidate-fork-126237291 \
  --out "nitro-autosubmit-mock-$(date +%Y%m%d-%H%M%S)"
```

若终端尚未设置ARCHIVE_RPC，先隐藏输入，不贴出URL：

```bash
read -rsp 'Archive RPC URL: ' ARCHIVE_RPC
printf '\n'
export ARCHIVE_RPC
```

## 影响范围与退出行为

- 前置要求recovery-paused-watchtower运行，8249验证终点为(320189,0)/6fef0856…，
  数据为/data_new/validator/config且镜像与已验收版本一致；条件不符拒绝切换。
- 新fork准备完成后，仅正常停止recovery-paused-watchtower释放演练数据库。
- 新建recovery-nitro-autosubmit，使用该数据库、8249及52100/52101端口。
- 不挂载生产密钥目录；新配置只含新生成临时密钥，权限0600。
- 不停止snapshot-sync，不改/data_mock，不打开/data文件或改变重要validator-nitro-1。
- 不改变旧18549 fork。本次使用全新的独立18550 fork。
- 默认等候自动运行最多1200秒，每30秒报告进度。只在新fork挖空块推进确认期限；
  空块不提供真实后续批次。
- 成功/失败后尝试正常停止本次测试容器，再关闭新fork。若无法确认容器停止，
  保留fork并在报告中标记。旧Watchtower不自动重启。
- 不删除旧容器或数据库；已有同名新测试容器时拒绝重跑。

## 验收和回传

预期summary.json状态nitro_autosubmit_passed。检查两笔Nitro创建/确认交易、发送人、
31337链ID、签名字段、目标、普通断言numBlocks64、精确expectedNodeHash、
latestConfirmed42887、验证终点和Outbox根映射。
signed-transactions.json保留交易/回执，nitro.log供失败诊断。
回传summary.json即可；不要贴config.json（临时私钥）或未经筛选的anvil.log（可能含RPC认证信息）。

本地已检查配置替换、数据库路径隔离、签名交易解析及拒绝42161链等逻辑。
Docker/Anvil真实运行待服务器执行，不提前标为通过。

## 限制

可复用已有B->C验证进度，不声称本轮新重放全部64条消息。治理A->B仍为synthetic
numBlocks1，无A->B执行证明。测试账户成功不替代原5cda实际机器的签名验收。
readyForProduction始终false。

## 首次启动前失败与重试

nitro-autosubmit-mock-20260928-004914：Safe恢复及测试账户授权通过，随后
prepare_test_container阶段OSError；没有返回containerId，ownedForkStillRunning=false。
尚未测试Nitro自动提交。原报告没有errno，不能直接认定是端口冲突。

修订脚本使用SO_REUSEADDR并实际listen探测：避免TIME_WAIT误报，同时仍拒绝
活跃监听。最多等待30秒；失败记录errno、系统错误说明及具体探测端口。
新增--retry-report仅接受未启动测试容器、fork已关闭的相应失败报告，并继续拒绝
已有同名测试容器或其他运行容器占用演练数据库。停止的Watchtower无需为重试先重启；
使用上次启动前检查证据，运行结束仍核对实际验证终点。

更新脚本后：

```bash
python3 rehearse-nitro-autosubmit.py \
  candidate-fork-126237291 \
  --retry-report nitro-autosubmit-mock-20260928-004914/summary.json \
  --out "nitro-autosubmit-retry-$(date +%Y%m%d-%H%M%S)"
```

本地测试通过：端口复用设置、活跃监听拒绝、探测socket关闭、符合条件的失败报告
允许重试及已经启动过测试容器的报告拒绝。尚未在用户Linux服务器确认故障原因。

## 2026-09-28 00:57 SGT 服务器重试通过

nitro-autosubmit-retry-20260928-005748返回nitro_autosubmit_passed。
新账户0x80518359447a87d1197e8fdd890c85ef068c4c43未被模拟身份，Nitro自动创建
及确认42887，numBlocks64、验证终点与Outbox核查通过。
创建：0x75127bfe59830185227bfc8d7b3d3c829d31238daa13382fc7390b1c0321d69d。
确认：0xf365d7f9abb7401d72d994e3a63b629ff897702019840229ae4a318e166af232。
testContainerStopped=true，ownedForkStillRunning=false，watchtowerAutoRestarted=false。
原首次OSError没有errno，成功重试不能倒推出唯一根因。最终覆盖汇总见SIMULATION-ACCEPTANCE.md。

## 下一项：正常重启后保留状态（待运行）

上一轮fork已停止，不能直接docker start旧容器。因此重新建立相同历史测试前置，
再在同一个仍运行的新fork内测试Nitro正常重启。使用独立容器名
recovery-nitro-restart-check，保留旧recovery-nitro-autosubmit及报告，不删除任何容器。

更新rehearse-nitro-autosubmit.py后执行：

```bash
python3 rehearse-nitro-autosubmit.py \
  candidate-fork-126237291 \
  --previous-pass nitro-autosubmit-retry-20260928-005748/summary.json \
  --restart-check \
  --out "nitro-restart-mock-$(date +%Y%m%d-%H%M%S)"
```

--previous-pass核验上次成功报告、原测试容器ID/镜像/停止状态及数据库与配置挂载。
脚本使用新临时密钥，先完成相同自动创建/确认，再正常停止并重启本次测试容器。
检查同一容器确实重新启动、相同测试地址、验证终点和B/C规范区块哈希恢复。
随后每5秒检查一次，观察90秒，要求创建/确认节点、质押状态和发送nonce不变，
没有待处理nonce偏移。额外交易或节点变化将判为失败，不掩盖异常。

成功状态nitro_restart_rehearsal_passed；详细证据restart-before.json、restart-check.json。
结束后同样停止本次容器和新fork，不重启旧Watchtower。不涉及重要节点和原账户签名机器。
这是正常关闭/启动、固定数据边界内的短时持久化检查，不覆盖强杀、Anvil重启、
新批次持续流入、长期运行或故障灾备。当前只有本地模拟逻辑测试通过，服务器结果待回传。

### 服务器结果：重启通过

nitro-restart-mock-20260928-010622返回nitro_restart_rehearsal_passed。
临时测试账户923087020097899a17330a572c3b2224fd5d543d自动创建/确认42887后，
正常重启并完成90秒稳定观察。没有额外发送nonce或断言；验证终点、质押状态保留。
创建交易0xc042a3c8db6615faff6f16abd998204b488e121194f166f6a509ec947b291095；
确认交易0xb5f4633a59db0b82fc43614538ecdbc7dbaaff17f818ce86a9f9710f42a8bd4c。
本次测试容器和18550 fork已停止，未重启旧Watchtower。readyForProduction仍false。
