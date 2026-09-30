# 三账户新候选：10578条本地重放

## 已有证据

用户回传同父块0x1e5f29da的三账户盘点、B单消息重放、候选打包、跨度审计。
报告保存于reported-results/three-staker-*-20260928-090354.json。

- A：父链confirmed43005，位置(321809,0)，hash59b30b…7ec9be。
- A′：本地同位置消息126683262，hashefa72d…eb5c07。
- B：消息126693840，位置(321867,130)，hash057c6c…6b73a7。
- 新WASM：121d685e…d17421。
- A和A′哈希不同；相同SendRoot不等同于相同完整状态。

## 下一步

replay-three-staker-span.py固定这份审计摘要和已有重放引擎文件SHA，
逐条记录并调用显式新WASM重放126683263至126693840，共10578条。
支持--resume和--max-messages；输出manifest、每消息记录链和summary。
不广播交易、不访问Docker或直接数据库，只调用本机8349的节点RPC，消耗CPU/I/O。
不得将旧7838报告当作本区间完成证据。

服务器完整重放结果待回传。之后对新候选进行三账户退款/重入演练。
此处没有宣称父链A→B执行证明、生产numBlocks获批或生产准备完成。

## 离线验证

python3 -m unittest test-three-staker-span.py test-retained-message-span.py
共8项通过：新锚点固定、续跑范围、失败不提交、记录缺口、错误WASM、状态不连续、禁止交易RPC。
