# 7838参数的Nitro自动提交及正常重启

状态：操作者回传 nitro-span-restart-20260928-084752，nitro_restart_rehearsal_passed。
原始回传报告已归档 reported-results/nitro-span-restart-20260928-084752.json；未直接连接服务器复核。

## 实际回传结果

- 治理numBlocks=7838，恢复节点精确哈希与顺序/原子演练一致。
- 普通节点42887，numBlocks=64；Nitro临时密钥真实签名、自动创建及确认通过。
- 临时账户：0x353ae9082a039a92d478bc245bd621597f9f8081，未使用账户冒用提交普通断言。
- 创建交易：0x330d505ddc639fafacaef77e9e088b179723d273b0773315ed40088abe4a29c2。
- 确认交易：0x6eba89653fd7ef2c3618291fc462257a36b2905994eb108410a218e4cbb4a482。
- 正常重启后观察90秒，验证/质押状态保持一致，无额外发送账户nonce或断言。
- testContainerStopped=true，ownedForkStillRunning=false，watchtowerAutoRestarted=false。
- 原账户运行及生产签名未测试；readyForProduction=false。

同一历史场景不需重复运行。收尾和下一阶段见SIMULATION-CLOSEOUT-20260928.md。

## 已执行的方法（留档）
入口 rehearse-nitro-span-restart.py，复用既有运行器的挂载、端口、镜像、签名、重启和清理检查。

前置 atomic-span-mock-20260928-084057 已通过7838原子批次。
本轮建立新的历史fork，使用7838顺序Safe恢复作为Nitro运行环境；
不把这轮称为原子批次和Nitro串联在同一条交易路径的验收。

容器名 recovery-nitro-span-restart；fork 18550、RPC8249、验证端口52100/52101。
仅使用/data_new/validator/config演练副本。旧测试容器存在时拒绝覆盖；
发现其他运行容器共享该数据库时拒绝启动。
不访问/data数据库，不修改snapshot-sync，不操作validator-nitro-1。
原Safe owners只在fork模拟，新Nitro账户使用真实生成的临时密钥签名，不冒用原账户。

```sh
export PATH="$HOME/.foundry/bin:$PATH"
export ARCHIVE_RPC
cd /data_new/scripts
python3 rehearse-nitro-span-restart.py \
  candidate-fork-126237291 \
  --atomic-pass atomic-span-mock-20260928-084057/summary.json \
  --previous-pass nitro-restart-mock-20260928-010622/summary.json \
  --span-audit recovery-message-span-20260928-013625/summary.json \
  --span-replay retained-span-replay-20260928-014843 \
  --out "nitro-span-restart-$(date +%Y%m%d-%H%M%S)"
```

预期：status=nitro_restart_rehearsal_passed，test=nitro_span_7838_restart，
governanceNumBlocks=7838，normalConfirmedNode=42887，普通断言numBlocks=64；
自动创建/确认通过，正常重启后观察90秒无额外nonce/断言，
testContainerStopped=true、ownedForkStillRunning=false。

不重复7838条WASM重放，只检查保存记录。运行器可以复用既有B→C验证进度，
并未证明这64条在本次全新重放。父链A→B执行未证明，生产参数仍未批准。
下一阶段按本轮实际结果归档验收差距及生产操作计划；生产执行需另行授权。
