# 三账户节点负责人：只读核对

更新：以最终角色current=65fa、cloud-a=5cda、cloud-b=21d4为准，d38e已退役无需节点采集。优先使用便携inspect-final-validator-prep.py及install-final-validator-prep.sh，安装在各机$HOME/recovery-prep-tools。下面旧版操作说明保留供参考；不要再将d38e列为运行节点。

目的：为后续恢复窗口确认实际容器、镜像及公开质押身份。当前阶段保持节点运行，不解锁密钥、不签名、不发送交易。

使用 inspect-recovery-operator.py（Python3标准库）。每个实际节点负责人执行一次；同一机器有多个提交容器时分别执行。预期地址由负责人选择：

- 0x5cda45a9ae0e52f1d5110dc3819f6fb96bade33a
- 0x21d4ea822a07f737c5e69f7951d517e5f2974849
- 0x65fa6c1d1efc338dfe8f07d5de84b3e7b76cbc95

先列出容器，再选择正确容器与地址。脚本和报告放在当前工具目录；不访问/data，不提供--config参数。

```sh
docker ps -a --format 'table {{.Names}}	{{.Image}}	{{.Status}}'
read -rp 'Expected public staking address: ' STAKER
read -rp 'Validator container name: ' CONTAINER
python3 inspect-recovery-operator.py --expected-staker "$STAKER" --container "$CONTAINER" --out "operator-inventory-$(date +%Y%m%d-%H%M%S).json"
```

只回传脚本生成的报告，并文字说明负责人、节点是否提供重要RPC/同步服务、签名方式（本地keystore/外部签名器/合约钱包）。不要回传完整docker inspect、环境变量、私钥或密码。

若不是Docker节点，说明进程管理方式；不要为了检查临时启动第二个节点。若缺少历史启动身份，报告null仅表示未在最近5000行找到，不表示账户错误。报告不能证明当前密钥可用或有效运行配置已全部解析。
