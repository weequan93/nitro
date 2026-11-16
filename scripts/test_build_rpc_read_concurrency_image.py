"""Verify the read-concurrency image recipe without Docker or service access."""

import importlib.util
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch


HERE = Path(__file__).parent
SPEC = importlib.util.spec_from_file_location("subsecond_verification_cases", HERE / "test_build_rpc_subsecond_image.py")
base = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(base)
SCRIPT = HERE / "build-rpc-read-concurrency-image.sh"


class ReadConcurrencyImageVerificationTests(base.ImageVerificationTests):
    def setUp(self):
        change = patch.object(base, "SCRIPT", SCRIPT)
        change.start()
        self.addCleanup(change.stop)
        super().setUp()
        source = SCRIPT.read_text()
        start = source.index("status() {")
        self.functions = source[start:source.index("if [[ $VERIFY_ONLY == 1 ]]; then", start)]
        self.write_cli("docker", base.FAKE_DOCKER.replace("rpc-subsecond-b7df6d1a1", "rpc-read-nitro"))
        (self.root / "payload/checks.txt").write_text("RPC shared-read and diagnostics checks: PASS\n")

    def test_help_works_without_root_or_docker(self):
        result = subprocess.run(["bash", str(SCRIPT), "--help"], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("SOURCE_REV", result.stdout)


if __name__ == "__main__":
    unittest.main()
