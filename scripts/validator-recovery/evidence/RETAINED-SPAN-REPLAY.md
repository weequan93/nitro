# 保留链A′→B连续消息重放

状态：操作者回传retained-span-replay-20260928-014843，retained_span_replay_passed。
7838条消息均在同一轮验证通过，终点为固定B。耗时6748.15秒，约1小时52分28秒。
完成时间2026-09-27T19:41:12.187610+00:00；nextMessage=null。
recordChainSha256=dfdc67af2b21cb262d39a297e7517c482cba94219c5aa19bbe6ea1fd8f482435。
以上依据用户回传摘要，本地未直接读取服务器7838份记录。
此前6项离线测试通过，覆盖断点续跑、失败不计入通过、记录缺口/错误WASM根拒绝、
起始状态不连续拒绝及禁止发送交易。无需重新启动已通过的这轮重放。

## 固定范围

- 审计依据：recovery-message-span-20260928-013625/summary.json。
- A′：消息126229453之后，BlockHash 0x2de7415d0a7d5e4fe6339a83ffe2cb43da57d6cd4c7118e4eea584151f0451c2。
- B：消息126237291之后，BlockHash 0x2654eb1fc5b10d89fa6e3a75b5ec06641aa9c2856b39c0c07468c113b2014189。
- 实际执行消息126229454–126237291，包含两端，共7838条。
- 明确WASM根0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421。

## 运行与恢复

replay-retained-message-span.py为独立Python标准库脚本（Linux文件锁），无需ARCHIVE_RPC、cast或私钥。
使用snapshot-sync的8349 RPC，逐条调用validationInputsAt及validateMessageNumber(full=true,明确root)。
不操作Docker、不开启Anvil、不直接访问节点数据库、不发送交易。
记录/执行会消耗snapshot-sync CPU和I/O，可能影响同步速度；不声称完全不影响运行节点。
默认串行，不通过并行请求增加压力。

```sh
cd /data_new/scripts
RUN="/data_new/scripts/retained-span-replay-$(date +%Y%m%d-%H%M%S)"
echo "$RUN"
nohup python3 -u replay-retained-message-span.py \
  --audit recovery-message-span-20260928-013625/summary.json \
  --node-rpc http://127.0.0.1:8349 \
  --out "$RUN" > "$RUN.log" 2>&1 &
echo $! > "$RUN.pid"
```

观察日志：tail -n 20 "$RUN.log"。进度：cat "$RUN/summary.json"。
新终端需将RUN重新设置成首次输出的实际目录。不要靠创建一个新目录来断点续跑。

若进程已退出且需续跑，在同一个RUN目录加--resume，日志追加而不是覆盖：

```sh
nohup python3 -u replay-retained-message-span.py \
  --audit recovery-message-span-20260928-013625/summary.json \
  --node-rpc http://127.0.0.1:8349 \
  --out "$RUN" --resume >> "$RUN.log" 2>&1 &
echo $! > "$RUN.pid"
```

同目录进程锁拒绝重叠运行。错误时检查failed/current message及日志，不绕过失败消息。
可用--max-messages N限制单次工作量；batch_complete仅代表分段结束，不代表完整通过。

## 检查与证据

每条输入StartState等于上一条通过的实际返回终点。显式root的执行必须valid=true，
且返回终点等于该条记录的ExpectedEndState；验证前后核对规范区块头与父块哈希。
消息Id、批次位置递增、最终B以及原A′/B固定区块均检查。
续跑扫描全部已保存消息记录、检查连续性及摘要链，并复核当前链上的边界/最近终点。

每条通过记录原子保存到messages/<消息号>.json，保存输入内容的规范JSON SHA256、
起止状态、执行返回、WASM根、区块头及前条记录摘要。完整输入体积可能很大，默认不保存。
last-attempt.json保存最近一次组装的验证结果；超时或进程中断的未提交消息需重新执行。

执行RPC自身重新记录输入，并非接收该保存输入摘要所对应的blob；报告明确这一限制。
这是一组由节点RPC提供证据的连续单消息重放，不是独立离线证明或跨消息持久WASM进程。

完整通过要求status=retained_span_replay_passed，validatedMessagesTotal=7838，executionReplayed=true。
validatedMessagesThisRun只统计当前进程新完成消息，不能与总数混淆。
父链A与A′不同：即使通过，也不形成A→B证明或生产numBlocks批准。
readyForProduction=false、parentConfirmedAToBProven=false、productionNumBlocksApproved=false始终保留。
