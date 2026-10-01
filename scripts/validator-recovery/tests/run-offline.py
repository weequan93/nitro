#!/usr/bin/env python3
"""Run existing offline recovery tests and syntax checks; skip RPC/fork CLI integrations."""

import argparse
import ast
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys

BASE = Path(__file__).resolve().parents[1]
REPO = BASE.parents[1]


def run(command):
    result = subprocess.run(command, capture_output=True, text=True,
                            env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1"))
    output = result.stdout + result.stderr
    if result.returncode:
        raise RuntimeError("Failed: " + " ".join(command) + "\n" + output[-6000:])
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--syntax-only", action="store_true")
    args = parser.parse_args()
    spec = importlib.util.spec_from_file_location("incident_catalog", REPO / "docs/validator-recovery/catalog.py")
    catalog = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(catalog)
    counts = {"python": 0, "shell": 0, "json": 0}
    for path in catalog.paths():
        if path.suffix == ".py":
            ast.parse(path.read_text(), filename=str(path))
            counts["python"] += 1
        elif path.suffix == ".sh":
            run(["bash", "-n", str(path)])
            counts["shell"] += 1
        elif path.suffix == ".json":
            json.loads(path.read_text())
            counts["json"] += 1
    tests = 0
    suites = 0
    if not args.syntax_only:
        commands = [[sys.executable, "-m", "unittest", "discover", "-s", str(BASE / "toolkit/tests"), "-p", "test_*.py", "-v"]]
        commands.append([sys.executable, "-m", "unittest", "discover", "-s", str(REPO / "docs/manual-withdrawal/tests"), "-v"])
        commands.append([sys.executable, "-m", "unittest", "discover", "-s", str(REPO / "docs/safe-transactions/tests"), "-v"])
        for path in sorted((BASE / "legacy/tests").glob("test-*.py")):
            tree = ast.parse(path.read_text())
            if any(isinstance(n, ast.ClassDef) and any(ast.unparse(b).endswith("TestCase") for b in n.bases) for n in tree.body):
                commands.append([sys.executable, str(path)])
        for command in commands:
            output = run(command)
            totals = re.findall(r"Ran (\d+) tests?", output)
            if len(totals) != 1:
                raise RuntimeError("Expected one unittest summary: " + " ".join(command))
            tests += int(totals[0])
            suites += 1
    print(json.dumps({"status": "offline_checks_passed", "syntax": counts,
                      "unittestSuites": suites, "tests": tests,
                      "rpcOrContainerAccess": False}))


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, SyntaxError, ValueError, OSError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
