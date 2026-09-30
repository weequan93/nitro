# 新镜像节点数据切换准备

状态：准备计划；尚未读取服务器最新数据检查结果，未授权或执行生产切换。

## 2026-09-29 后续检查结果（用户报告）

03:02 UTC：snapshot-sync 使用新 image ID b3571c0d…，挂载 /data_mock/validator/config；head127052205，公开head127052996，差791；高度127052141哈希一致。这是抽查，不是全链或WASM证明。

后续配置检查：文件中 block-validator.enable=true，current-module-root 为新121d…根，pending为空；staker.enable=false，父链配置为42161及预期真实RPC。与最初生成的禁用后台验证配置不同。尚未确认文件变更何时加载。

arb_latestValidated 返回 batch314208/pos93，blockhash82437bc6…、WasmRoots含旧767c…和新121d…两个根。源码 arbnode/api.go 将该接口映射到数据库中最后保存的验证记录；因此一次返回不能证明本次进程正在推进，也不能证明整个历史均在新根下重放。需检查启动日志、配置时间和间隔采样，不据此重启或覆盖数据库。检查脚本 check-snapshot-validation-progress.sh 已准备，未在服务器由助手执行。

此前“工具与流程就绪”不等于三台节点数据均可直接切换。三机的新镜像与配置由用户准备，数据就绪需单独记录。

## 当前已知来源

- 当前服务器 `/data_mock/validator/config`：从原快照恢复并连接真实父链同步，作为待核验的切换候选。现有 snapshot-sync 继续维护该副本。
- `/data_new/validator/config`：曾供 Anvil 演练写入，不能因为块高或哈希相同就直接作为正式 validator 数据。
- `/data`：受保护，不读取、不替换、不复制。本计划不停止 validator-nitro-1。
- 云 A/B：独立新数据副本状态待用户确认，不假设旧数据库可直接在新镜像下无差异续跑。

## 暂停前可完成

后续定位：服务器报告 validation-sent-limit=0，prerecorded-blocks=1，forward-blocks=0；实际Nitro选定参数没有CLI覆盖。显式新根重放消息125022703返回valid=true、10141ms。源码sendValidations在lastValidationSent >= validated + ValidationSentLimit时返回，零值会阻止提交后台验证。准备 fix-snapshot-validation-limit.py，只将limit改为8、保留其他字段，先备份再优雅停止/启动snapshot-sync，保留配置文件inode以兼容bind mount。默认仅计划，--apply才执行。未在服务器执行；后台进度恢复待核验。单字段变更及错误父链/启用质押/重复应用拒绝已做本地检查。

1. 检查 snapshot-sync 容器实际挂载、新镜像 ID、L3 chainId/head、与公共节点同高度哈希。检查脚本 check-cutover-data-readonly.sh 只查 Docker 元数据和 RPC，不打开数据库/配置/密钥。
2. 核定候选数据的真实父链连接与有效配置。后台验证曾关闭，块高接近不等于新 WASM 已完整验证。
3. 保持候选数据库追赶，按实际生产候选区间准备显式新 WASM 的执行证据；历史已重放区间的证据保留，不冒充最新区间已验证。此时生产链仍是旧 root，不用强制忽略 root 来提前发送断言。
4. 核定三机数据供给：已有独立副本则分别校验；没有则选择可靠一致性快照/停写复制和传输方案，先估容量、耗时。不能将活动 Pebble 目录的普通 rsync 当作一致备份，不能硬链接可变数据库作为隔离副本。
5. 准备交接表：每台地址、镜像 ID/digest、数据源与实际挂载、真实父链、验证 root/进度、签名配置、启动顺序与验收。密码和私钥不写入交接文档。

## 进入切换窗口后

按用户独立升级安排协调旧提交与 Fast Safe；治理暂停和节点停止是不同动作。正式启动接管候选目录前，先优雅停止占用该目录的同步实例并确认退出。同一个可写数据库目录不允许两个 Nitro 进程同时使用。

恢复成功且仍 paused 时，按新 root 验证并核对运行端点；审阅通过后单独 resume，再由 MakeNodes 正常质押/断言。生产 A/B 与最终跨度在实际暂停后固定。所有数据准备不构成 A→B 证明或提前暂停/恢复的授权。
