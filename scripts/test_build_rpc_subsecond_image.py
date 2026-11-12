"""Exercise image verification with a fake Docker CLI; no daemon or node calls."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("build-rpc-subsecond-image.sh")

FAKE_DOCKER = '''#!/usr/bin/env python3
import hashlib, json, os, sys
from pathlib import Path
a = sys.argv[1:]
with open(os.environ["RPC_VERIFY_CALLS"], "a") as f:
    f.write(json.dumps(a) + "\\n")
candidate = "sha256:candidate"
mode = os.environ["RPC_VERIFY_MODE"]
if a[:2] == ["image", "inspect"]:
    if "--format" in a:
        print(candidate)
    else:
        config = {"Entrypoint": ["/usr/local/bin/split-val-entry.sh"], "Env": ["EXAMPLE=1"]}
        newconfig = dict(config)
        if mode == "config_changed": newconfig["Env"] = ["EXAMPLE=2"]
        print(json.dumps([{"Config": config, "RootFS": {"Layers": ["base"]}},
                          {"Config": newconfig, "RootFS": {"Layers": ["base", "native"]}}]))
elif a[0] == "run":
    if candidate not in a: raise SystemExit("Probe must use the resolved image ID")
    if a[-1] == "--version":
        print("Version: rpc-subsecond-b7df6d1a1, time: test")
    elif a[-1] == "--help":
        stream = sys.stdout if mode == "stdout_help" else sys.stderr
        print("--persistent.pebble.compaction-output-bytes-per-second\\n"
              "--persistent.pebble.max-concurrent-compactions\\n"
              "--rpc.stage-diagnostics-threshold", file=stream)
        if mode == "help_failed": raise SystemExit(8)
    elif "/usr/bin/sha256sum" in a:
        digest = hashlib.sha256(Path(os.environ["RPC_VERIFY_PAYLOAD"]).read_bytes()).hexdigest()
        print(("bad" if mode == "binary_changed" else digest) + "  /usr/local/bin/nitro")
    else: raise SystemExit("Unexpected probe")
else: raise SystemExit("Unexpected Docker operation")
'''


class ImageVerificationTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.addCleanup(self.folder.cleanup)
        self.root = Path(self.folder.name)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.write_cli("docker", FAKE_DOCKER)
        self.write_cli("timeout", "#!/usr/bin/env python3\nimport os,sys\nos.execvp(sys.argv[2],sys.argv[2:])\n")
        self.write_cli("sha256sum", "#!/usr/bin/env python3\nimport hashlib,sys\nfrom pathlib import Path\nprint(hashlib.sha256(Path(sys.argv[1]).read_bytes()).hexdigest()+'  '+sys.argv[1])\n")
        (self.root / "payload").mkdir()
        (self.root / "payload/nitro").write_bytes(b"tested native payload")
        (self.root / "payload/checks.txt").write_text("RPC stage diagnostics including subsecond boundaries: PASS\n")
        (self.root / "runtime-before.json").write_text("{}")
        (self.root / "source/scripts").mkdir(parents=True)
        (self.root / "source/scripts/check-main-runtime.py").write_text("print('PASS: unchanged runtime baseline')\n")
        source = SCRIPT.read_text()
        self.functions = source[source.index("status() {"):source.index("if [[ $VERIFY_ONLY == 1 ]]; then")]

    def write_cli(self, name, text):
        p = self.bin / name
        p.write_text(text)
        p.chmod(0o755)

    def verify(self, mode="stderr_help"):
        env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ["PATH"],
                   BUILD=str(self.root), SOURCE=str(self.root / "source"),
                   IMAGE="candidate:tag", BASE="sha256:base", SOURCE_REV="nitro", GETH_REV="geth",
                   EXPECTED_BASE="sha256:base", STAGE="image_verification", BUILDER_CREATED="0", WORKER_CREATED="0",
                   RPC_VERIFY_MODE=mode, RPC_VERIFY_CALLS=str(self.root / "calls.jsonl"),
                   RPC_VERIFY_PAYLOAD=str(self.root / "payload/nitro"))
        program = "set -euo pipefail\n" + self.functions + "\ntrap cleanup EXIT\nverify_image\n"
        return subprocess.run(["bash", "-c", program], env=env, capture_output=True, text=True)

    def state(self):
        return json.loads((self.root / "status.json").read_text())["state"]

    def test_help_on_stderr_completes_without_build_or_service_operations(self):
        result = self.verify()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.state(), "assembled_and_verified_not_deployed")
        self.assertIn("--rpc.stage-diagnostics-threshold", (self.root / "help.txt").read_text())
        self.assertEqual((self.root / "image-id.txt").read_text(), "IMAGE_ID=sha256:candidate\n")
        calls = [json.loads(s) for s in (self.root / "calls.jsonl").read_text().splitlines()]
        self.assertTrue(all(a[0] in ("run", "image") for a in calls))
        self.assertTrue(all("--network" in a and "--rm" in a for a in calls if a[0] == "run"))

    def test_help_on_stdout_also_completes(self):
        result = self.verify("stdout_help")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.state(), "assembled_and_verified_not_deployed")

    def test_failed_probe_is_not_hidden_by_captured_help(self):
        result = self.verify("help_failed")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.state(), "failed")
        self.assertFalse((self.root / "image-id.txt").exists())

    def test_runtime_config_change_and_different_binary_are_rejected(self):
        for mode in ("config_changed", "binary_changed"):
            with self.subTest(mode=mode):
                result = self.verify(mode)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.state(), "failed")
                self.assertFalse((self.root / "image-id.txt").exists())

    def test_resume_validates_identity_and_preserves_previous_status(self):
        source = SCRIPT.read_text().split("# Preserve the first attempt's status and log", 1)[1]
        code = source.split("<<'PY'\n", 1)[1].split("\nPY\n", 1)[0]
        record = {"stage": "image_verification", "state": "failed", "image": "candidate:tag",
                  "source_revision": "nitro", "geth_revision": "geth", "base_image": "sha256:base"}
        original = json.dumps(record)
        (self.root / "status.json").write_text(original)
        (self.root / "build.log").write_text("original failed attempt")
        command = ["python3", "-B", "-", str(self.root), "candidate:tag", "nitro", "geth", "sha256:base"]
        result = subprocess.run(command, input=code, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        backups = list(self.root.glob("status.before-verify-*.json"))
        self.assertEqual(len(backups), 1)
        self.assertEqual(backups[0].read_text(), original)
        self.assertEqual((self.root / "build.log").read_text(), "original failed attempt")
        command[-2] = "unreviewed-geth"
        result = subprocess.run(command, input=code, capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len(list(self.root.glob("status.before-verify-*.json"))), 1)


if __name__ == "__main__":
    unittest.main()
