# 安装与分发

| 子目录 | 内容与行为 |
| --- | --- |
| `installers/` | 当时提供的安装 shell，内嵌 payload 和校验保持原样 |
| `bundles/` | 历史 `.gz` / `.tar.gz` 分发快照，保持原字节 |
| `builders/` | 从当前 `../toolkit` 构建基础包及附加模块 |
| `generated/` | 新构建结果；与历史安装器、压缩包分开存放 |

构建器仅生成文件，不调用生产 RPC、Docker、签名或部署。构建器默认在 `generated/` 写对应安装器及 payload；完整包的 manifest 在生成的压缩包内，不改写 toolkit 的历史基础 `MANIFEST.sha256`。

从仓库根目录构建当前完整 v2 包：

```bash
python3 scripts/validator-recovery/distribution/builders/build-recovery-toolkit-bundle.py
```

这是一个文件构建命令，不是运行恢复流程。新包保持服务器工具目录名 `production-recovery-toolkit-v2`，包含当前 addon。各 addon builder 同样保持原服务器安装目标及依赖校验。

完整包包含 addon，因此与已经安装的旧基础包 manifest 不同；原完整包安装器会拒绝覆盖不同版本。既有服务器继续按具体 addon 依赖顺序更新，不能通过忽略校验覆盖。历史包不重生成或假定已经包含后加模块。
