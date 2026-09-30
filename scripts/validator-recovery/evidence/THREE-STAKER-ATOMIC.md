# 三账户原子恢复模拟

新10578区间已由用户回传完整PASS。此前7838测试保留为旧场景证据。

脚本rehearse-three-staker-atomic.py依赖已安装的atomic、two-staker、Safe、mock及replay脚本。
固定父块0x1e5f29da/hashc06fed…41a8d、confirmed43005/created43007，三账户5cda/D38e/65fa。
核验全部10578份保存记录，记录链c6c912…173e75。

仅创建私有Anvil，执行参考顺序恢复和三账户实际退款支付，然后回滚参考状态。
再使用Safe三位owner的模拟approveHash执行原子失败/成功测试。成功停留在paused=true，
节点43008，三账户退款额度已记账；成功批次本身未支付退款、未恢复运行、未重入。
参考阶段和成功原子阶段要区分，不能将参考付款称作最终原子批次已经付款。
脚本自动结束其私有Anvil，不使用Docker，不访问节点数据库。

通过后仍剩两组：新场景运行/重入和提现回归。生产动作未授权执行。
10578只是位置跨度；A′→B证据不构成父链A→B证明。readyForProduction保持false。

离线：15项相关测试通过。服务器结果：待运行。

## 服务器回传：通过

three-staker-atomic-mock-20260928-133005：成功原子交易
0x865b9c23f37410d89accf710b4943e1fc86a84268e5ac0b1943dfc0518329406。
失败回滚交易0x0af21fad6cb6af5bae4273efe7d64b1565f99670f81897cc07f8907d830fe562。
恢复节点43008，准确hash d86bb1e3277e2cf5565f945d2954f670f4b4dc88df11f1d53359b706fd5f5da2。
全部为用户回传历史Anvil结果。最终paused=true，未测试restaking/runtime。
下一步第2组的B→C执行证据采集：replay-three-staker-followup.py。
