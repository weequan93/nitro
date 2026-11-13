# getLogs / sendRaw 共用历史读取路径的并发修正

本次优化前的现场基线：Nitro `b7df6d1a17d33f304bee1ac553e00e3e35a27b93`，geth `bf399f01dffc34785f6f1653a3ddaeed30d9d5fb`，
镜像 `sha256:0ea40d56b156757fc121c864ba30775f7bdd8924a45dec5d59b8b6228cd50215`。
现场证据及边界见 `../rpc-overnight-review-20261113/findings-20261113.md`。

## 改动与适用范围

`chainFreezer.ReadAncients` 原先对底层 `Freezer.writeLock` 获取独占锁。
canonical hash 查询及部分 header 读取会经过这里，包括在 ancient 中找不到记录、回退读取热数据库的过程。
因此，一个较慢的只读回调会挡住经过同一路径的其他读取者。

修正将包装器改为 `RLock/RUnlock`，与 `Freezer.ReadAncients` 的既有语义一致。
整个回调仍持有读锁，append、truncate head/tail、close 依然受同一写锁排斥。
时代文件后端 eradb 的 getter 有自己的并发保护，其读取接口声明为 thread-safe。

这是共享数据库读取路径的优化。getLogs 的未索引 header 扫描直接使用该路径；
区块生产/执行中的历史读取也可受益。它不改变 nonce 校验、交易执行或区块/收据内容，
也不能保证 sendRaw 中所有状态访问、计算或写入长尾都由此消除。
现场 207 条 nonce-too-low 应由该账户的调用方修正，不改变节点 nonce 规则来掩盖这些错误。

## 验证

- 新回归用例在旧实现上复现只读回调互相阻塞，改动后允许两个读取回调同时进入。
- append、truncate head、truncate tail、close 与读取回调仍互斥，写入完成前回调读到的 count/tail/data 保持一致。
- 8 个读取者在真实热库→freezer 分批迁移期间读取 canonical hash、header、body、receipts，验证数据可见性。
- 本地 Darwin/arm64 的完整 rawdb、eradb `-race` 测试通过。
- Linux/amd64 在离线、4 CPU / 4 GiB 的隔离容器中验证；全部测试组通过，最终状态和各组结果记录在 `validation.json`、`linux-*-race.txt`。
- 候选镜像验证助手的 6 项模拟检查通过，包括帮助输出在 stderr、二进制/镜像配置差异拒绝、失败保留及验证恢复。

`benchmark-*.txt`、`linux-benchmark-*.txt` 是相同 2048-header 热库读取模型的前后各 3 轮测试。
它们不包含 RPC、真实云盘或生产执行工作量，只用于验证解除读者串行后的吞吐变化。
分析见 `validation.json`，不能据此直接预测线上 p95/p99 改善倍数。
本地 8 并发的中位吞吐比为 2.117；Linux 容器 4 并发为 2.061。分配次数均为 16 次/操作。

## 服务器构建

`scripts/build-rpc-read-concurrency-image.sh` 使用一个完整 pinned Nitro commit 构建候选，
其中的 geth gitlink 必须为已测试的 `9557fc573cdde179c42c17c6e1927f00b4b23133`。
基于现场已运行的镜像继承原有运行配置和资源层，只附加经过测试的新 native binary。
构建资源限制为 2 CPU / 10 GiB，另检查可用内存和磁盘空间。

构建中重跑 rawdb/eradb 竞态、getLogs 查询/取消及 RPC 亚秒诊断兼容检查。
版本、帮助、native hash、镜像配置及基础层继承检查全部通过后，状态才会变为 `assembled_and_verified_not_deployed`。
`activity.jsonl` 保存各构建阶段时间，供排除这段构建负载对采集数据的影响。

这份助手只负责构建与验证。候选镜像完成后，切换仍应使用绑定当前实例的运行时计划，遵守用户要求的 Nitro 正常退出与数据库关闭检查。
旧的 subsecond 切换计划绑定旧镜像，不能作为本次新候选的切换计划。
切换后应将启动预热区间单独标记，按相近区块 gas、交易/查询数量和后台负载比较 ≥500 ms / ≥1 s 请求比例，
同时观察 debt、写停顿及失败类别；合成读取基准不能替代这一步。

## 本次候选的切换入口

`prepare-rpc-read-switch.py --build /data/rpc-read-build.yadX8u` 在构建完成后准备新计划，不改变服务。
它固定候选源码为 Nitro `a3955fae953a50ef54d8f1420be301ce869dcab6`、geth `9557fc573cdde179c42c17c6e1927f00b4b23133`，
从构建目录读取候选镜像 ID，并核对最终验证状态、测试 PASS、native payload hash、镜像内 native hash、镜像平台、配置及基础层。
计划保存构建证据哈希，仍核对运行时输入、现有 cache 默认值及 image-only Compose 差异。
构建脚本的源码版本与之后安装的切换助手版本可以不同；候选 native 必须始终对应上述已测试版本。

`switch-rpc-read-image.py --apply PLAN` 和 `--rollback PLAN` 复用现有正常退出状态机，
共用旧切换助手和 compaction 操作锁。SIGTERM + 无限 Docker stop 等待、退出码、数据库关闭日志、
停止实例身份和受哈希保护的恢复证据检查均保留。构建证据还会在停服前、正常退出后再次核对。
它只在成功启动目标容器后报告 `started_health_pending`，后续仍需检查出块、syncing、publisher health 并启动新采集。

离线测试 `test_rpc_read_switch.py` 覆盖计划绑定、未完成/错误版本构建拒绝、二进制及镜像漂移拒绝、
构建证据变化、正常 apply/rollback、重复 apply，以及正常退出后证据变化和重建失败回滚。
测试全部使用模拟 Docker；它们不代表服务器候选已经完成构建或部署。
本地执行 `PYTHONPATH=scripts python3 -B -m unittest -v test_rpc_read_switch test_prepare_rpc_subsecond_switch test_switch_rpc_subsecond_image test_build_rpc_read_concurrency_image test_build_rpc_subsecond_image test_check_main_runtime`，43 项通过。

## 切换后的采集阶段

`start-rpc-read-capture.py` 接收新计划、预期镜像、旧采集目录和新输出目录。
它核对 apply journal 中的新实例身份、部署输入、native PID1、compaction/pacing，
要求 RPC head 实际前进、`eth_syncing=false`、publisher health 返回无错误和 `null`，
并确认完整亚秒计数器与数据库 metrics 可读。源文件、vdb 与磁盘预算也在结束旧采集前检查。

就绪后只向指定目录的旧 suite parent 发送 SIGTERM，等待退出；其它 suite 会导致拒绝接管。
随后原地 exec 完整 suite，记录资源、cache/IO、慢事件和 RPC buckets，持续 24 小时、按小时分段。
旧目录保留。新目录的 `phase.json` 记录新实例、健康结果和从节点启动算起的 30 分钟排除边界，
已有 report 不会自动应用这个边界，
比较性能时需要按 `comparison_not_before_utc` 排除启动区间，再匹配负载；此时间规则不是缓存已经预热完成的证明。

启动入口的离线测试涵盖 head 不前进、syncing、publisher 错误、缺失 bucket、实例变化、
指定旧 suite 交接、慢退出等待、旧文件保留、完整采集命令及预热边界。

## 已采集慢请求的读取

`review-rpc-tails.py --suite-dir CAPTURE --log-dir LOGS --until CUTOFF` 固定截止时间，
读取该 suite 保存的计数器、资源、cache、发布阶段滚动分位，以及指定目录的 `nitro.log` / `nitro-*.log.gz`。
它依赖同目录的 `review-rpc-tail-data.py` 和 `review-rpc-tail-archives.py`，
不请求节点 RPC，也不改服务设置。日志副本、输入质量及完整 JSON 报告默认保存在采集目录同级的新 review 目录，避免占用实时采集的输出预算。
报告记录本次分析的起止时间，供后续比较排除分析读取本身的负载。

输出区分成功/失败的 >=500ms、500–<1000ms、>=1/3/5s 数量及各自有效覆盖；
打印两接口的长尾尖峰和全部 >=1s 计数器区间，并对齐附近磁盘、CPU、compaction 与发布阶段快照。
阶段 quantile 从微秒换算为毫秒，但仍是共享滚动统计，不能当作某条请求的阶段耗时相加或相减。
`phase.json` 的启动排除边界用于附加的 post-warmup 汇总，原始全程统计及其长尾记录仍保留。

归档时间优先使用绝对时间；无时区的终端时间用已保存 watcher 的 method/trace 校准，无法校准时明确保留未知。
错误分组隐藏地址和 URL，并对齐日志中的失败与 >=1s 阶段记录数量，帮助辨别日志覆盖不足。
1s 阶段阈值没有保存的 500–<1000ms 逐笔阶段不能恢复；这些请求只提供时间窗口与后台负载线索。
日志时间可能滞后、详情可能丢弃，计数相同也只证明总数相符，不证明逐笔完整或磁盘竞争的因果。

离线检查命令：`PYTHONPATH=scripts python3 -B -m unittest -v test_review_rpc_tail_data test_review_rpc_tail_archives test_review_rpc_tails`。
28 项通过，涵盖固定截止、阈值关系、失败分组覆盖、UTC/本地时间校准、轮转重复、损坏 gzip、同容器重启、
日志读取范围，以及计数器→资源→发布阶段→归档的端到端合成用例。
