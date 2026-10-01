# /data_new 复用评估：待运行检查

用户报告 data-new-reuse-report.virlO2 显示没有运行容器占用候选目录。最后演练容器 recovery-nitro-three-restart 在2026-09-28 15:21:08 UTC退出，exitCode=0。目录来自停写后的/data_mock复制，复制核对为size/mtime/metadata/file-set，并非全量checksum。

保存的演练验证终点126693888（batch321868/pos0）与真实父链同步节点的canonical block hash和SendRoot相符。该检查没有打开当前数据库，也没有证明历史父链记录或整个验证区间可直接继承。仍不批准生产复用。

## 已准备的下一步

prepare-data-new-parent-check.py 默认仅生成配置；--start明确启动data-new-parent-check，并开始写/data_new候选目录。真实父链固定42161/http://10.1.2.16:8547，RPC8249，验证服务52100/52101，使用已测试image ID。质押、排序、批次提交、feed和交易转发关闭；不挂载keys或Docker socket；不复用演练临时私钥配置。

首次检查有意保留validation-sent-limit=0：初始化读取已有检查点，暂不发送后台验证任务。故不能把这个阶段检查点不动当作新的故障。节点仍可同步、回滚父链派生数据并写数据库；不是只读启动。没有validation reset或assume-valid新写入请求。

不停止、不修改snapshot-sync或validator-nitro-1，保留/data_mock独立候选。运行前检查挂载占用与端口；重复名字拒绝替换。若链配置/父链历史不兼容，先保留失败日志，不通过删除数据或放松校验强行启动。

## 待验收

实际连接42161、现有DB head及同高度hash、现有validated endpoint/root、父链同步/重组日志、是否加载了演练链配置或无法兼容的元数据。确认后才决定在此候选启用limit=8继续验证。历史B的受信任起点不能被改称为完整父链A→B证明。

脚本本地只完成配置构造、移除模拟签名配置、固定RPC及错误父链拒绝检查；服务器实际启动尚未执行。
