#!/usr/bin/env python3
"""Index and verify local recovery records; never call RPC, Docker or deployment tools."""

import argparse
import ast
from collections import Counter
import hashlib
import json
from pathlib import Path
import re
import sys
from urllib.parse import unquote

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
INDEX = HERE / "source-index.json"
CATALOG = HERE / "SCRIPT-CATALOG.md"
SCRIPTS = REPO / "scripts/validator-recovery"
ROOT_DOCUMENTS = {"README.md"}
TREES = ("scripts/validator-recovery", "docs/validator-recovery", "docs/manual-withdrawal", "docs/safe-transactions")
CODE_REFERENCES = (
    "arbos/addressMap/addressMap.go", "arbos/blacklist/blacklist.go",
    "execution/gethexec/block_recorder.go", "execution/gethexec/block_recorder_test.go",
    "execution/gethexec/legacy_fee_preimages.go", "execution/gethexec/legacy_blacklist_preimages.go",
    "execution/gethexec/legacy_blacklist_preimages_test.go", "execution/gethexec/legacy_recording_hooks.go",
    "scripts/validation-122408091-findings.md", "staker/block_validator.go",
    "staker/stateless_block_validator.go", "staker/legacy/staker.go", "staker/legacy/fast_confirm.go",
    "arbnode/batch_poster.go", "execution/gethexec/sequencer.go",
    "cmd/genericconf/liveconfig.go", "cmd/nitro/nitro.go", "cmd/nitro/config/config.go",
    "contracts-legacy/src/bridge/SequencerInbox.sol", "contracts-legacy/src/rollup/RollupUserLogic.sol",
    "contracts-legacy/src/rollup/RollupAdminLogic.sol", "contracts-legacy/src/rollup/RollupCore.sol",
)
LABELS = {
    "record": "本次记录与执行流程",
    "production": "正式 v2 工具与附加模块",
    "inspection": "诊断、角色与候选采集",
    "data": "数据准备、验证进度与调优",
    "replay": "固定区间与执行重放",
    "rehearsal": "历史 fork、契约与运行演练",
    "tests": "离线测试与需另行授权的集成入口",
    "evidence": "本地保存的报告、审阅包与回执",
    "distribution": "安装器、构建器与历史分发压缩包",
    "historical": "早期工具副本与历史说明",
    "code": "分支中的实现参考",
    "withdrawal": "通用 L3 手动提款方法与说明",
    "safeadmin": "通用 Safe 黑名单与 owner 交易生成",
}
GENERATED = {INDEX, CATALOG}
ALLOWED_SUFFIXES = {".py", ".sh", ".js", ".md", ".json", ".txt", ".sha256", ".gz"}
EXCLUDED_NAMES = {"anvil.log", "anvil.pid"}


def paths():
    """Limit discovery to incident trees and explicitly selected branch sources."""
    found = set()
    for path in REPO.iterdir():
        if path.is_file() and path.name in ROOT_DOCUMENTS:
            found.add(path)
    for tree in TREES:
        base = REPO / tree
        if not base.is_dir():
            raise ValueError(f"Missing recovery tree: {tree}")
        for path in base.rglob("*"):
            if not path.is_file() or path in GENERATED or path.is_relative_to(SCRIPTS / "distribution/generated") or any(path.is_relative_to(REPO / f"docs/{name}/generated") for name in ("manual-withdrawal", "safe-transactions")):
                continue
            if "__pycache__" in path.parts or any(part.startswith(".") for part in path.relative_to(base).parts):
                continue
            if path.name in EXCLUDED_NAMES or path.suffix not in ALLOWED_SUFFIXES:
                continue
            found.add(path)
    for name in CODE_REFERENCES:
        path = REPO / name
        if not path.is_file():
            raise ValueError(f"Missing implementation reference: {name}")
        found.add(path)
    found.add(SCRIPTS / '.gitignore')
    found.add(REPO / 'docs/manual-withdrawal/.gitignore')
    found.add(REPO / 'docs/safe-transactions/.gitignore')
    for path in sorted(found):
        if path.is_symlink() or not path.resolve().is_relative_to(REPO):
            raise ValueError(f"Refuse symlink/outside file: {path.relative_to(REPO)}")
        yield path


def category(path):
    rel = path.relative_to(REPO).as_posix()
    name = path.name
    if rel.startswith("docs/manual-withdrawal/"):
        return "withdrawal"
    if rel.startswith("docs/safe-transactions/"):
        return "safeadmin"
    if rel.startswith("docs/validator-recovery/history/"):
        return "historical"
    if rel.startswith("docs/validator-recovery/") or rel in ("README.md", "scripts/validator-recovery/README.md", "scripts/validator-recovery/.gitignore"):
        return "record"
    if rel in CODE_REFERENCES:
        return "code"
    if rel.startswith("scripts/validator-recovery/evidence/"):
        return "evidence"
    if name.startswith(("test-", "test_", "integration_")) or "/tests/" in rel:
        return "tests"
    if rel.startswith("scripts/validator-recovery/toolkit/"):
        return "production"
    if rel.startswith("scripts/validator-recovery/distribution/"):
        return "distribution"
    if rel.startswith("scripts/validator-recovery/diagnostics/"):
        return "inspection"
    if rel.startswith("scripts/validator-recovery/snapshot/"):
        return "data"
    if rel.startswith("scripts/validator-recovery/legacy/packages/"):
        return "historical"
    if name.startswith(("replay-", "export-native-")):
        return "replay"
    return "rehearsal"


def description(path, kind):
    if path.suffix == ".gz":
        return "历史分发快照；只校验文件，不解压或执行。"
    if path.name.startswith("install-"):
        return "安装器；内嵌 payload 与当前源码可能属于不同版本。"
    if path.name.startswith("build-"):
        return "读取 toolkit 构建新分发包；输出 distribution/generated，不覆盖历史包。"
    if path.suffix == ".py":
        tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
        doc = ast.get_docstring(tree)
        if doc:
            return doc.splitlines()[0][:150]
    if path.suffix == ".md":
        for line in path.read_text(encoding="utf-8").splitlines():
            if line.startswith("# "):
                return line[2:][:150]
    if path.suffix == ".json":
        value = json.loads(path.read_text(encoding="utf-8"))
        if isinstance(value, dict) and isinstance(value.get("status"), str):
            return "保存状态：" + value["status"][:100]
        return "JSON 参数 / 报告 / 交易内容；查看来源与观察时间。"
    if kind == "tests":
        return "测试入口；integration_* 可连接 RPC，未在本次归档中执行。"
    if kind == "code":
        return "本分支的协议或运行实现参考。"
    return LABELS[kind]


def entry(path):
    content = path.read_bytes()
    kind = category(path)
    return {
        "path": path.relative_to(REPO).as_posix(), "category": kind,
        "bytes": len(content), "sha256": hashlib.sha256(content).hexdigest(),
        "description": description(path, kind),
    }


def render(entries):
    lines = [
        "# 脚本与资料索引", "",
        "从 [README](README.md) 进入实际记录和执行流程。此目录由 `catalog.py --refresh` 生成，"
        "按阶段保留全部相关文件的原路径；SHA-256 和字节数见 [source-index.json](source-index.json)。", "",
        "toolkit 为本次正式准备入口；diagnostics、snapshot、legacy、distribution 和 evidence 各有对应用途。固定 nonce、"
        "节点、Safe import、只读报告都属于各自观察时刻，不能据此直接执行下一次恢复。", "",
        "日志、编译缓存、私钥、keystore、密码、服务器全量数据库不纳入索引。完整远程输出"
        "未传回时，只索引已保存摘要，并在 [incident.json](incident.json) 标出来源。", "",
        "## 常用入口", "",
        "| 用途 | 脚本 |", "| --- | --- |",
        "| 候选采集、治理包与回执 | [recovery.py](../../scripts/validator-recovery/toolkit/recovery.py)、[collect.py](../../scripts/validator-recovery/toolkit/collect.py) |",
        "| 三笔预签与阶段执行检查 | [presign.py](../../scripts/validator-recovery/toolkit/presign.py) |",
        "| 固定区间重放、同包 fork | [replay-span.py](../../scripts/validator-recovery/toolkit/replay-span.py)、[verify-fork.py](../../scripts/validator-recovery/toolkit/verify-fork.py) |",
        "| Watchtower 交接与 Compose | [cutover.py](../../scripts/validator-recovery/toolkit/cutover.py)、[prepare-compose.py](../../scripts/validator-recovery/toolkit/prepare-compose.py) |",
        "| 账户及 Fast Safe | [owner-check.py](../../scripts/validator-recovery/toolkit/owner-check.py)、[fast-safe-check.py](../../scripts/validator-recovery/toolkit/fast-safe-check.py) |",
        "| 退款领取 | [withdraw-refund.sh](../../scripts/validator-recovery/toolkit/withdraw-refund.sh)；由账户管理员签名 |",
        "| L3 跨链提款领取 | [通用方法与说明](../manual-withdrawal/README.md)；按原 L3 交易重新生成 proof/claim |",
        "| Safe 黑名单与 owner 操作 | [交易生成与说明](../safe-transactions/README.md)；只生成待审阅 Builder CALL 列表 |",
        "| 验证进度 | [snapshot-validation-metrics.py](../../scripts/validator-recovery/snapshot/snapshot-validation-metrics.py)、[inspect-snapshot-validation-gates.py](../../scripts/validator-recovery/diagnostics/inspect-snapshot-validation-gates.py) |", "",
    ]
    for key, label in LABELS.items():
        selected = [x for x in entries if x["category"] == key]
        if not selected:
            continue
        lines += [f"## {label}（{len(selected)}）", "", "| 文件 | 内容 / 限制 |", "| --- | --- |"]
        for item in selected:
            safe_desc = item["description"].replace("|", "\\|").replace("\n", " ")
            lines.append(f"| [{item['path']}](../../{item['path']}) | {safe_desc} |")
        lines.append("")
    return "\n".join(lines).rstrip("\n") + "\n"


def load_relative(name):
    path = REPO / name
    if not path.resolve().is_relative_to(REPO) or path.is_symlink():
        raise ValueError("Invalid evidence path")
    return json.loads(path.read_text(encoding="utf-8"))


def verify_incident():
    """Cross-check copied constants against the retained independent reports."""
    doc = load_relative("docs/validator-recovery/incident.json")
    candidate = doc["finalCandidate"]
    review = load_relative(candidate["localReviewFile"])
    recovery = doc["production"]["recovery"]
    saved = load_relative(recovery["evidenceFile"])
    for key in ("before", "after", "checkpointBlock", "governanceNumBlocks"):
        if candidate[key] != review[key]:
            raise ValueError(f"Incident candidate mismatch: {key}")
    if candidate["replay"]["lastMessage"] - candidate["replay"]["firstMessage"] + 1 != candidate["governanceNumBlocks"]:
        raise ValueError("Incident replay interval mismatch")
    if candidate["replay"]["validatedMessagesTotal"] != candidate["governanceNumBlocks"]:
        raise ValueError("Incomplete incident replay count")
    for key, saved_key in (("transaction", "transaction"), ("safeNonce", "executedSafeNonce"), ("safeTxHash", "expectedSafeTxHash"), ("receiptBlock", "receiptBlock"), ("receiptBlockHash", "receiptBlockHash"), ("actualInboxMaxCount", "actualInboxMaxCount")):
        if recovery[key] != saved[saved_key]:
            raise ValueError(f"Incident recovery mismatch: {key}")
    for key in ("recoveryNode", "nodeHash"):
        if recovery[key] != saved["receiptPost"][key]:
            raise ValueError(f"Incident node mismatch: {key}")
    if doc["software"]["newWasmRoot"] != saved["newWasmRoot"] or set(doc["roles"]["refundAccounts"]) != set(saved["receiptPost"]["refundCredits"]):
        raise ValueError("Incident root or refund scope mismatch")
    for kind in ("zombieCleanup", "firstFastConfirmation"):
        local = doc["production"][kind]
        report = load_relative(local["evidenceFile"])
        keys = ("transaction", "sender", "receiptBlock") if kind == "zombieCleanup" else ("safeHash", "fastSafeNonce", "latestConfirmed", "latestCreated")
        for key in keys:
            if local[key] != report[key]:
                raise ValueError(f"Incident {kind} mismatch: {key}")
    if doc["finalCandidate"]["replay"]["parentConfirmedAToBProven"] or doc["production"]["resume"]["exactReceiptAccepted"]:
        raise ValueError("Incident overstates available evidence")


def verify_links():
    for path in [*HERE.glob("*.md"), REPO / "docs/manual-withdrawal/README.md", REPO / "docs/safe-transactions/README.md", SCRIPTS / "README.md", SCRIPTS / "diagnostics/README.md", SCRIPTS / "snapshot/README.md", SCRIPTS / "legacy/README.md", SCRIPTS / "distribution/README.md", SCRIPTS / "evidence/README.md"]:
        content = re.sub(r"```.*?```", "", path.read_text(encoding="utf-8"), flags=re.S)
        for target in re.findall(r"\]\(([^\s)]+)\)", content):
            if target.startswith(("https://", "http://", "#", "mailto:")):
                continue
            target = unquote(target.split("#", 1)[0].strip("<>"))
            if target and not (path.parent / target).exists():
                raise ValueError(f"Broken link in {path.relative_to(REPO)}: {target}")


def verify_migration():
    """Verify preserved distributions/reports against hashes recorded before relocation."""
    migration = load_relative('docs/validator-recovery/layout-migration.json')
    unchanged = 0
    for item in migration['movedFiles']:
        old = REPO / item['oldPath']
        current = REPO / item['newPath']
        if old.exists() or not current.is_file():
            raise ValueError('Migration path mismatch: ' + item['oldPath'])
        if item['byteIdentical']:
            if hashlib.sha256(current.read_bytes()).hexdigest() != item['beforeSha256']:
                raise ValueError('Historical content changed: ' + item['newPath'])
            unchanged += 1
    return unchanged


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--refresh", action="store_true", help="Write only source-index.json and SCRIPT-CATALOG.md")
    mode.add_argument("--verify", action="store_true", help="Verify files, incident consistency and documentation links (default)")
    args = parser.parse_args()
    entries = [entry(path) for path in paths()]
    generated = render(entries)
    document = {"schemaVersion": 1, "hashAlgorithm": "sha256", "scope": "Local incident files; generated index/catalog and logs/caches excluded.", "files": entries}
    verify_incident()
    unchanged = verify_migration()
    if args.refresh:
        INDEX.write_text(json.dumps(document, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        CATALOG.write_text(generated, encoding="utf-8")
    else:
        saved = json.loads(INDEX.read_text(encoding="utf-8"))
        if saved != document:
            previous = {x["path"]: x for x in saved["files"]}
            current = {x["path"]: x for x in entries}
            changed = sorted(name for name in previous.keys() | current.keys() if previous.get(name) != current.get(name))
            raise ValueError("Index differs; review changed files before --refresh: " + ", ".join(changed[:15]))
        if CATALOG.read_text(encoding="utf-8") != generated:
            raise ValueError("SCRIPT-CATALOG.md differs from generated contents")
    verify_links()
    print(json.dumps({"status": "catalog_refreshed" if args.refresh else "catalog_verified", "files": len(entries), "categories": dict(Counter(x["category"] for x in entries)), "incidentConsistency": "passed", "preservedMovedFiles": unchanged, "links": "passed", "rpcOrContainerAccess": False}, ensure_ascii=False))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, SyntaxError) as error:
        print(f"STOP: {error}", file=sys.stderr)
        sys.exit(1)
