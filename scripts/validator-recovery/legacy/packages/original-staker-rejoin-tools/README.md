# 原账户退款后重新质押：第二个 Anvil 测试

运行位置：deriw-prod-validator 服务器，两个 fork 所在机器。

前提：
- 18547 保持运行，已完成 Nitro 新账户正常质押/提交/确认，latestConfirmed=42686。
- 18548 保持运行，从 recovery-session-20260924-204010/snapshot 完成治理模拟，
  latestConfirmed=latestNodeCreated=42685，原账户已退款、提款、清理 zombie。
- 不需要额外复制 original-restake-template.json；本脚本自动从18547提取并检查。
- python3、Foundry cast 已安装。不需要输入私钥或Archive RPC。

解压到 /data_new/scripts 后运行：

    bash /data_new/scripts/original-staker-rejoin-tools/run.sh

可选参数：输出目录（必须尚不存在）。默认在当前目录生成带时间戳的证据目录。

行为：
1. 18547只读查询，18548严格检查chain31337和Anvil身份、恢复状态/节点hash。
2. 检查原EOA白名单、已退出质押、无zombie、无待领退款。
3. 读取Nitro在18547已成功执行的newStakeOnNewNode交易模板。
4. 18548分配模拟ETH、impersonate原账户并满足提交间隔，原账户正常质押/创建。
5. 核对后继节点承诺、满足确认期限，原账户调用普通confirmNextNode。
6. 写入交易hash、回执及summary.json，最后停止impersonation。

预期：

    PASS original-restake-create ...
    PASS original-normal-confirm ...
    OUTPUT ...

不修改18547，不使用生产私钥，不向真实父链发交易，不启动另一个数据库进程。
如果中途失败，请提供错误和输出目录中的交易hash，不要直接重复运行或删除证据。
目标fork已经发生部分写入时，脚本会拒绝把它当作初始恢复状态继续。

限制：这是原账户合约级复用测试，采用Anvil impersonation及Nitro已有calldata；
不等同于原validator二进制/真实签名配置完整运行，也不构成生产恢复批准。
