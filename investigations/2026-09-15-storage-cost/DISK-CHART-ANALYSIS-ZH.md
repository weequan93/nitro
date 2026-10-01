> **最新纠正：用户确认该节点 `archive=false`。** 此前基于“archive 节点”口述作出的“当前节点保留全部历史状态”判断撤回。保留全部区块不等于保留全部历史状态。下文旧分析中的 hash archive 归因已被此更正替代；链上记录数量、磁盘增长和 compaction 采样数据仍有效。用户进一步确认 nodeconfig caching.archive 一直为 false；后续按 hash 非归档节点分析，不再推定此前开启 archive。

# 磁盘截图补充调查

用户提供的图为实例 ins-f8sifl9g 的磁盘利用率，覆盖 8 月 13 日到 9 月 24 日，最新 96.17%。用户已确认图中利用率突然下降均来自扩容，每次增加 300 GB。用户进一步确认该实例为保留全部历史数据的 archive 节点。尚未取得服务器访问方式、实际 state-scheme、准确总容量或目录占用数据。

## 图中能看到什么

- 9 月 15 日附近之后，上升斜率明显变陡。
- 按图像粗读，8 月下旬下降后到 9 月 11 日，约 81% → 95%，用时约 17 天，即每天约 0.8 个百分点。
- 9 月 19 日附近下降后到 9 月 24 日，约 81% → 96.17%，用时约 5 天，即每天约 3 个百分点。
- 用户已确认期间扩容，因此不能直接将不同阶段的百分比斜率之比当作 GB/天之比。此前“3–4 倍”的物理增长比较已撤回。
- 用户确认几次利用率突然下降均是扩容造成，实际已用字节没有因此减少；排除把这些下降解释为清理或裁剪。

## 与链上证据的对应

新 PriceOracle 于 9 月 15 日 09:58:25 UTC（新加坡 17:58:25）开始接受价格更新；旧 PriceFeed 最后索引到的更新为 09:57:38 UTC。具体交易链接见 TIMELINE-SEP09-24.md。

新合约每个有效 token 更新永久增加五个历史槽位，旧 PriceFeed 对应历史记录增加一个。历史记录保持在当前 live state 中，普通旧状态裁剪不会移除它们。此机制可以解释持续状态增长的来源，但是否占该实例新增磁盘的主要部分，仍需服务器目录增长数据。

198 个已观测 token 的直接 mapping 计数给出以下日增量：

| 日期 | 新增价格历史记录 | 新增历史槽位 | 32 字节逻辑值总量（非磁盘字节） |
|---|---:|---:|---:|
| 9 月 16 日（UTC） | 3,984,025 | 19,920,125 | 0.637 GB |
| 9 月 17 日（UTC） | 3,790,496 | 18,952,480 | 0.606 GB |
| 9 月 21 日（UTC） | 4,125,938 | 20,629,690 | 0.660 GB |
| 9 月 22 日（UTC） | 4,327,406 | 21,637,030 | 0.692 GB |
| 9 月 23 日（UTC） | 4,370,539 | 21,852,695 | 0.699 GB |

9 月 19 日、20 日零点的历史状态在公共 RPC 上不可用，未补造日数据。以上覆盖 198 个 token，不保证覆盖全部 token。上述 GB 仅为槽位的逻辑值长度，不含 key、trie、数据库编码、写放大、archive 历史、日志和收据，不能作为实际磁盘增量。

对同一批 60 个 token，旧时期（9 月 9–14 日）的事件 round ID 差值折算约 142.45 万条/天，新时期（9 月 16–24 日）的直接状态计数折算约 186.35 万条/天。乘以每条历史槽位由 1 增至 5，新增历史槽位速率约为旧时期的 6.54 倍。旧时期各 token 的观测窗口不同，新旧窗口也包含不同工作日/周末；该值是匹配 token 的观测速率估计，不是全链或物理磁盘的精确倍数。

## 还缺什么，才能确认这台实例的原因

1. 实例角色：执行 RPC/sequencer/validator、DA 存储、还是 explorer 数据库。
2. 磁盘总容量与实际链数据路径；挂载是否变动。
3. 每次扩容的准确时间、扩容前后容量与原始利用率读数；已知每次增加 300 GB。
4. 服务器只读检查：文件系统用量、各一级数据目录大小；区分执行数据库当前 state、ancients、历史索引、应用日志、快照和迁移副本。再结合 archive/state-scheme/retention 等已部署配置进行解释。

如果是 DA 实例，五个 EVM storage slot 不能直接解释 DA 磁盘；需要比较 DA 对象字节和保留策略。如果是 explorer 实例，需要检查其数据库表/索引和归档策略。当前不能把这些情况混为执行节点。

Arbitrum 在 9 月 23 日启用 priority tip collection，只解释后来 ETH 支出增加，不能解释此图磁盘增速。

磁盘当前只余 3.83 个百分点。若容量不变且近期每天约 3 个百分点的斜率延续，剩余空间约为 1–2 天；这是截图外推，不是监控预测。未执行任何清理、裁剪、配置修改或链上交易。

## 用户确认每次扩容 300 GB 后的估算

扩容瞬间若已用空间近似不变，设扩容前利用率为 u1、扩容后为 u2，扩容后容量为 C2：

`C2 = 300 GB × u1 / (u1 - u2)`

最后一次下降按截图约 95% → 81% 粗读，则扩容后容量约 2,036 GB。由于曲线像素、准确扩容时刻和瞬间写入不可知，这只能提示“约 2 TB”的量级，不能代替实际容量记录，也未对三次扩容跳变完成一致性拟合。

若最后一次扩容后容量约 2,036 GB，从约 81% 到 96.17% 对应新增约 309 GB；按约五天计算，约 62 GB/天。取更宽的截图误差范围，可暂按约 50–70 GB/天理解，等待真实容量/时间确认。

若图中三次下降各为 +300 GB，则 8 月下旬第一次扩容后到 9 月 11 日这一段的容量，应比最后一次扩容后小 600 GB。用上述约 2,036 GB 的末期容量推算，早期该段约 1,436 GB。约 81% → 95% 的增长在约 17 天内对应约 12 GB/天。两阶段约为五倍的量级；这是依赖粗读容量和时间的探索性估算，不是已验证的精确增速或因果归因。

该物理增长估算不能由逻辑槽位字节直接解释：9 月 23 日观测 token 新增槽位的逻辑值约 0.699 GB，而服务器估算约几十 GB/天。必须检查具体目录及数据库模式，才能识别 trie、历史版本、索引、日志等各自占比。不能把这个差值直接声称为已测得的数据库写放大。

## 已确认实例为 archive 节点

用户确认保留全部历史数据。这进一步支持“应用工作负载变化增加 archive 状态历史”的解释，但目前没有生产目录占用、数据库统计或已部署 state-scheme，不能量化各类字节的贡献。

本仓库代码：

- execution/gethexec/blockchain.go 的 Archive 配置传入 core.BlockChainConfig.ArchiveMode；默认 StateScheme 为 hash。仓库默认值不能替代已部署配置。
- go-ethereum/core/blockchain.go:writeBlockWithState 写入 block、receipt 和 preimages，再提交状态。
- hash 模式、archive 开启且没有 sparse archive 跳过设置时，执行 bc.triedb.Commit(root, false)，不进入下面普通节点的历史 trie 回收路径。保留下来的既有新状态叶节点，也有变化的祖先树节点版本等内容。
- path 模式走不同持久化路径，使用状态历史和历史索引；不能套用 hash archive 的具体空间模型。保留完整历史时仍需要相应历史记录。

因此，新 oracle 不断增加新的 token/priceId 历史键，同时更新计数、salt 等已有键，会增加 live state 与 archive 状态历史。物理数据库还包括 trie 节点编码、key、收据、索引等，不应拿约 0.7 GB/天的槽位逻辑值当作磁盘需求。

重要限制：旧 oracle 覆盖已有槽位时，archive 同样会保留相关状态历史。因此，“新增历史槽位从 1 到 5”或匹配 token 的“新增历史槽位速率约 6.54 倍”不等于 archive 磁盘增长必然是 5 倍或 6.54 倍。此前图像约五倍的物理增长也是容量粗估，不能以倍数接近作为因果证明。

下一步需确认 execution.caching.state-scheme，并获得实际链数据目录的占用分布。若要求保留全部历史状态，关闭 archive、截断状态历史或普通裁剪不能作为满足原需求的修复方案。本次未修改配置、未迁移数据库。

## 已确认当前为 hash archive，计划迁移 path

用户确认当前 state-scheme 为 hash，后续计划升级 path。因此上文 hash archive 的持久化路径适用于已声明的运行模式；仍未取得线上二进制版本、完整运行参数和目录大小，不能将代码默认的逐块提交设置视为已核实的生产设置。

调查结论进一步收敛到：9 月 15 日新 PriceOracle 的高频追加历史记录，增加当前状态以及 hash archive 保留的状态树版本。该机制与磁盘增速变化一致，但物理字节贡献仍需目录/数据库统计验证。

Path archive 是改变历史状态的存储方式，不会停止 PriceOracle 创建新历史记录，也不会自动删除当前状态中的既有历史价格。保留完整 archive 是用户要求，迁移验收应包含旧块历史查询能力，而不仅是 latest 同步成功。具体工具是否转换全部旧历史，必须按迁移工具支持范围检查，不能由 state-scheme=path 推断。

## 服务器实测：2026-09-24 05:16–05:31 UTC（15 分钟，采样尚未完成）

用户提供了四次 du -m 读数。每五分钟一次，均为执行数据库分配空间的目录扫描，非原子快照。

| UTC 时间 | ancient（MiB） | l2chaindata 总量（MiB） |
|---|---:|---:|
| 05:16:16 | 214,868 | 1,732,125 |
| 05:21:16 | 214,871 | 1,732,473 |
| 05:26:16 | 214,876 | 1,732,587 |
| 05:31:16 | 214,880 | 1,732,888 |


15 分钟总净增 763 MiB，其中 ancient 增加 12 MiB，非 ancient 部分增加 751 MiB，占增量 98.43%。三个五分钟窗口总净增分别为 348、114、301 MiB；尚未观察到净回落，但存在增长速率波动。

若这 15 分钟速率持续全天，总量约增加 71.53 GiB/天（76.81 GB/天），非 ancient 部分约 70.41 GiB/天。该结果是短窗外推，不是全天实测，仍可能受 compaction、索引构建、同步追赶等影响。应让已有 30 分钟采样完成，无需重启采样。

目前已能确认实际净增长集中在非 ancient 执行数据库，不是 ancient 区块历史，也不是 Docker 日志。仍不能仅凭 du 将非 ancient 内的具体增量完全分配给状态树：该部分包含状态和索引等数据。此前链上证明的新 oracle 追加记录与 hash archive 保留状态版本仍是主要原因假说；最终字节归因需数据库分类统计或对应运行指标。

## 完整 30 分钟采样：2026-09-24 05:16:16–05:46:16 UTC

此完整窗口更新前面的 15 分钟速率估计。

| UTC 时间 | ancient（MiB） | l2chaindata 总量（MiB） |
|---|---:|---:|
| 05:16:16 | 214,868 | 1,732,125 |
| 05:21:16 | 214,871 | 1,732,473 |
| 05:26:16 | 214,876 | 1,732,587 |
| 05:31:16 | 214,880 | 1,732,888 |
| 05:36:16 | 214,880 | 1,733,029 |
| 05:41:16 | 214,883 | 1,733,287 |
| 05:46:16 | 214,887 | 1,733,472 |


总净增 1,347 MiB；ancient 净增 19 MiB；非 ancient 净增 1,328 MiB，占增量 98.59%。所有五分钟端点均比前一个端点增加；不能据此排除窗口内或跨更长时间的 compaction 波动。

若持续此速率，总量约增加 63.14 GiB/天（67.80 GB/天），其中非 ancient 62.25 GiB/天，ancient 0.89 GiB/天。每次扩容若为十进制 300 GB，约覆盖 4.42 天；若供应商实际指 300 GiB，则约 4.75 天。以上均为半小时短窗外推，不是已实测的全天速率。

用户此前 df 报告可用 384G 是较早时间的读数，不能作为此采样结束时的精确剩余容量。持续增长确实集中于非 ancient 执行数据库；具体状态树、索引、后台重建等所占比例尚未测定。不能将本次结果表述为已证明全部字节由 PriceOracle 写入或已经排除 compaction。

下一步优先获取已暴露的数据库/索引运行指标与启动以来同步状态，确定数据库引擎及是否有历史索引回填；不能在运行中的数据库目录上启动需要独占锁的离线 inspect/compact 工具。若没有相关指标，先收集顶层数据库文件类型及近期经过筛选的索引、compaction、同步日志，再选择低影响的分类统计方式。保留用户完整 archive 要求。

## 后续文件与索引日志核对

用户列出的数据库顶层文件为数字文件名 `.sst`，可确认有 LSM 排序表文件；仅凭该后缀不能唯一识别 Pebble/其他兼容引擎，也不能把每个 SST 当成纯状态树数据。

用户提供 09-24 05:41:04 的两条 `Log index head rendering` 日志，范围 115359124–125392273，processed=1、remaining=0、elapsed=2.355s，随后 finished。仓库 go-ethereum/core/filtermaps/indexer.go:tryIndexHead 中 firstblock/lastblock 输出 indexedRange 的边界，而 processed 是 AfterLast 与本次开始索引位置的差。因此该记录表示这次 head indexing 前进了一个区块并完成，不是重建约一千万个区块的证据。

这段经过 since/tail/grep 筛选的日志未显示大量回填，但不构成对所有后台索引或 compaction 的排除。下一步应优先读已存在的 metrics，检查数据库 compaction debt、进行中 compaction、SST/disk size 等；不应打开运行中数据库做需要独占锁的离线扫描或为了诊断启用公网 debug 接口。

## 已取得 Pebble 数据库监控

用户从现有 metrics 端点提供：

- l2chaindata_disk_size = 1592290349952 bytes，约 1482.93 GiB / 1.448 TiB，与非 ancient 目录用量的量级吻合。
- compact_debt = 0；compact_estimateDebt = 0。
- compact_inprogress = 0；compact_live_count = 0；compact_live_size = 0。
- tables_level0 = 0；tables_level0_sublevels = 0。
- compact_writedelay_counter = 0；compact_writedelay_duration = 0。
- compact_input = 4686287052676；compact_output = 4680442904242。

go-ethereum/ethdb/pebble/pebble.go 中，disk_size 来自 Pebble stats.DiskSpaceUsage；debt 来自 Compact.EstimatedDebt；inprogress 来自 Compact.NumInProgress；input/output 通过各层 compaction 字节计数增量累计。后两项是累计整理 I/O，允许同一逻辑数据被多次重写，不是当前额外磁盘占用，也不能直接用 input-output 计算可释放空间。

采样时没有 compaction 积压、进行中的 compaction 临时空间或 L0 文件堆积证据。结合半小时净增长和此前普通 head index 完成日志，更支持 archive 长期留存写入而非“等待 compaction 完成即可消失”的解释。单点指标不排除此前/其他时间的 compaction 波动；也未完成键空间分类，不能断言新增字节全部由 PriceOracle 贡献。

当前调查已形成三层证据：
1. on-chain：9 月 15 日新 oracle 开始，每条有效记录产生五个历史槽位；持续新增数百万条/天。
2. implementation：用户确认 hash archive；其历史状态树版本长期保留。
3. server：净增约 67.8 GB/天（半小时外推），98.6% 位于非 ancient 数据库；本次 metrics 无整理积压。

建议按“新 oracle 状态增长 + hash archive 历史保留是主要原因，精确字节归因仍未量化”报告，而非宣称磁盘泄漏或已完全排除所有其他贡献。优先评估保留完整历史的 path archive 迁移；准确节省比例应以真实迁移副本实测。常规 compaction 不会自动删除仍需保留的历史状态，不能承诺一次整理能显著释放这部分存储。

## archive=false 后的修正调查方向

若该值确实生效，当前应按 hash full node（非 archive）分析。旧区块/交易保留与历史状态可查询性是两个不同要求。

源码显示非 archive 路径会按保留窗口 Dereference 历史 root，但 hashdb 的引用回收主要处理内存 dirty trie；已经持久化的旧 trie 节点不会因为简单切换 archive=false 或普通 Pebble compaction 就自动按状态可达性清除。正常周期提交或缓存容量触发落盘也可能留下后续过时的节点。这是待量化的存储来源，而不是声称该实例已经验证有多少无效节点。

因此需区分：当前仍存活的新 oracle 历史记录、hashdb 已落盘但可能不再可达的节点、其他索引与数据。compaction debt=0 只表示没有数据库整理债务，不代表链状态层面没有可裁剪数据。

已证实的 oracle live-state 追加机制不依赖 archive 开关，仍成立。此前“hash archive 每块保留全部历史状态”不能继续作为该节点磁盘增速的解释。也不能仅据 archive=false 排除从 archive 数据库/快照继承了旧历史数据。

优先问明：archive=false 来自哪里（实际启动配置还是某个文件），此前是否一直为 false、是否切换过或使用过 archive 快照。当前不建议直接停机裁剪或改变保留策略，须先确认所需历史查询能力及数据可达性。

## 用户确认 caching.archive 一直为 false

撤回 archive 模式归因，也不再以“以前开启 archive 后关闭”为当前主假设。保留已验证的磁盘采样和 oracle 记录增长事实。

新的可验证机制：go-ethereum/triedb/hashdb/database.go 的 Cap 方法将仍有引用的 dirty trie 节点刷入持久化数据库，以满足内存上限；Dereference 仅在 dirties 内存表中处理引用，已经提交的节点不在表内时直接返回。非 archive 运行时也会有容量触发刷盘及周期 trie commit。因此若负载上升令刷盘早于内存回收，后续过时的落盘 hash 节点可能积累。此机制由代码支持，但尚未测得该实例的 flush/GC 增量或准确贡献。

仓库默认 caching.block-age=30m、trie-dirty-cache=1024 MiB、trie-time-limit=1h，只是默认值，不能当作线上已生效值。应获取用户配置中的 caching 段，并读取 hashdb_memcache_flush/commit/gc 的 bytes/nodes 计数差以及 chain_triedb_size，验证是否发生大量提前刷盘。累积刷盘字节不是物理净增长字节；不同节点的缓存、留存窗口和数据库编码会改变对应关系。

## hashdb 运行时数据：2026-09-24 06:14:12–06:15:12 UTC

已确认 archive 始终为 false。两个 60 秒间隔采样：

- chain_triedb_size：1073639305 → 1073639005 bytes，稳定接近 1 GiB。
- hashdb_memcache_flush_bytes：224845240347 → 224895554424，增加 50314077 bytes（47.983 MiB）。
- hashdb_memcache_flush_nodes：564734118 → 564859727，增加 125609 个节点。
- hashdb_memcache_gc_bytes/nodes：均为 0 → 0。
- hashdb_memcache_commit_bytes/nodes：均为 0 → 0。
- l2chaindata_disk_size：1593467941088 → 1593467941088，本分钟快照未增加。

代码将 Cap 的缓存容量刷盘计入 flush 指标，将 Commit 路径实际从内存提交的节点计入 commit 指标，将 Dereference 实际回收内存节点计入 gc 指标。GC 为零不等于 GC 函数从未被调用；Commit 为零也不能证明函数从未被调用。只说明这些计数没有记录相应回收/提交节点。

本数据明确支持 hashdb 持续容量刷盘、缓存贴近约 1 GiB，且没有观测到内存节点回收。按短窗速率外推为约 72.45 GB/天的 hashdb 逻辑刷盘量，不是物理净增长量。既有 30 分钟 du 净增外推为 67.80 GB/天，两个窗口与编码口径不同，数量级接近不能当作严格字节归因证明。

以缓存表观大小除以逻辑刷盘速率，得到约 21.34 分钟的粗略周转量级；Size 包含缓存元数据等开销而 flush 字节口径不同，不能将其当成精确节点停留时间。若生效 block-age 仍为仓库默认 30 分钟，容量刷盘可能发生在足够老的根可回收前，解释了 GC 计数为零及大量持久化。这仍需用户 caching 配置确认，也未量化已落盘节点中有多少已经过时。

建议下一步仅确认 caching 的 block-age、block-count、trie-dirty-cache、trie-time-limit 等字段与可用内存/容器上限，再决定是否做受控缓存配置实验。不能仅据这一分钟数据断言所有新增字节可裁剪，或直接缩短保留窗口/承诺调大缓存能完全解决。

## caching 配置与部署版本默认值核对

用户提供 caching 仅有 archive=false，旁边 rpc.gas-cap=300000000。已按线上镜像标签对应提交 16c17ee3a9c4 核对 execution/gethexec/blockchain.go，而非仅检查当前工作区：默认 block-age=30m、block-count=128、trie-dirty-cache=1024 MiB、trie-time-limit=1h（累计区块处理时间）、state-scheme=hash。前提是没有其他启动参数/配置源覆盖。

该提交引用 go-ethereum e582a7d5fb4887cbd3b4c7bb4fe74d2a5b06bddc；已核对该版本的 core/blockchain.go 和 triedb/hashdb/database.go，确认容量触发 Cap 发生在此处回收循环前，回收要求同时超过 block-count 和 block-age，Dereference 不会在 dirty map 缺失时进入持久化数据库删除节点。

约 1 GiB 缓存、每分钟约 48 MiB 的逻辑刷盘、零回收节点，与默认 30 分钟最小保留窗口之间存在明显的容量/保留时长不匹配迹象。新 oracle 提高状态写入负载后，可能使更多节点在有机会回收前落盘，这是目前具体且有运行证据支持的主要机制，但准确过时字节占比仍未知。

rpc.gas-cap 控制 RPC 模拟调用的 gas 上限，不是脏状态缓存大小或状态回收窗口参数。优先评估在内存余量允许时增加 trie-dirty-cache，并保持现有保留窗口以便做单变量对照；尚未取得 free -h 与容器内存上限，未给出无条件生产变更或执行重启。增加缓存目标是降低未来写入，不会自动回收已有约 1.48 TiB 键值数据库。

## 内存条件及受控缓存试验建议（尚未实施）

用户提供 free -h：30 GiB 总内存、10 GiB used、19 GiB buff/cache、19 GiB available、604 MiB free；swap 1.9 GiB 基本用满。Docker HostConfig.Memory=0，即没有此项显式容器硬上限，不代表宿主机内存无限，也不排除其他资源限制。

按当前快照，可评估将 caching.trie-dirty-cache 从默认 1024 MiB 调至 4096 MiB；保留 archive=false、block-age=30m 和其他参数。名义缓存增量 3 GiB；它不是进程 RSS 上限，Go 堆与其他缓存仍有开销。19 GiB available 比 free 更适合判断可用余量，但单次读数不能覆盖 RPC 峰值。已满 swap 可能是历史换页残留，应以 vmstat 1 6 后续区间的 si/so 判断是否正在持续换页；第一行是开机以来平均，不应据此定论。若持续明显换页，不应直接继续增加缓存。

提供 caching-proposed-fragment.json，仅为现有配置里 caching 段的合并片段，不是可覆盖整个节点配置的完整文件。未修改线上配置，未执行重启。生效需要按既有部署方式在可接受的 RPC 中断窗口内优雅重启，保留原配置便于回退。

验证应在节点追平、缓存稳定并跨过 30 分钟保留窗口后进行，至少观测约两小时，同时比较 hashdb GC 节点/字节增量、flush 字节速率、内存/换页和数据库净增长。重启后累计计数清零，须比较新采样间的差值。不能把扩容缓存后刚启动的低刷盘率当成长期收益；更大缓存的初始填充本身会延迟刷盘。原物理净增长基线为 63.14 GiB/天（半小时窗口外推），应尽量在相似负载及更长窗口比较。增加缓存不自动回收既有磁盘旧状态。
