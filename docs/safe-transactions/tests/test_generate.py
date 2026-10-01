import contextlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("safe_admin", HERE / "generate.py")
g = importlib.util.module_from_spec(spec)
spec.loader.exec_module(g)

SAFE = "0x2f996bc558818d33de37af36bee7de24ba3fc4df"
ACCOUNT = "0x26a5ccec74e4e193cb47350e473b85410ea02d82"
SECOND = "0x" + "ab" * 20


class Tests(unittest.TestCase):
    def test_known_blacklist_owner_calldata(self):
        package = g.build("blacklist-owner", SAFE, [ACCOUNT])
        tx = package["transactions"][0]
        self.assertEqual(tx["to"], g.BLACKLIST)
        self.assertEqual(tx["data"], "0x8dabebe8" + "00" * 12 + ACCOUNT[2:])
        self.assertEqual(tx["contractInputsValues"], {"newOwner": ACCOUNT})
        self.assertEqual(package["chainId"], "2886")
        self.assertEqual(tx["value"], "0")

    def test_distinct_blacklist_directions_and_chain_owner(self):
        for action, selector, target in [("blacklist-from", "0x328ce6ec", g.BLACKLIST),
                                         ("blacklist-to", "0x9e01565a", g.BLACKLIST),
                                         ("chain-owner", "0x481f8dbf", g.CHAIN_OWNER)]:
            with self.subTest(action=action):
                tx = g.build(action, SAFE, [ACCOUNT])["transactions"][0]
                self.assertEqual(tx["data"], selector + "00" * 12 + ACCOUNT[2:])
                self.assertEqual(tx["to"], target)

    def test_safe_signer_calls_safe_with_resulting_threshold(self):
        package = g.build("safe-owner", SAFE, [ACCOUNT], 42161, 3)
        tx = package["transactions"][0]
        self.assertEqual(tx["to"], SAFE)
        self.assertEqual(tx["data"], "0x0d582f13" + "00" * 12 + ACCOUNT[2:] + (3).to_bytes(32, "big").hex())
        self.assertEqual(tx["contractInputsValues"]["_threshold"], "3")
        self.assertEqual(package["chainId"], "42161")
        self.assertNotIn("nonce", package)

    def test_blacklist_batch_preserves_requested_addresses(self):
        package = g.build("blacklist-from", SAFE, [ACCOUNT, SECOND])
        self.assertEqual([tx["contractInputsValues"]["addr"] for tx in package["transactions"]], [ACCOUNT, SECOND])
        self.assertTrue(all(tx["value"] == "0" and tx["to"] == g.BLACKLIST for tx in package["transactions"]))

    def test_wrong_precompile_chain_rejected(self):
        with self.assertRaises(ValueError):
            g.build("blacklist-owner", SAFE, [ACCOUNT], 42161)

    def test_duplicate_and_invalid_addresses_rejected(self):
        for targets in ([ACCOUNT, ACCOUNT.upper().replace("0X", "0x")], ["0x123"], ["0x" + "00" * 20], []):
            with self.subTest(targets=targets), self.assertRaises(ValueError):
                g.build("blacklist-to", SAFE, targets)

    def test_invalid_safe_owner_and_threshold_rejected(self):
        for targets, threshold in [([SAFE], 3), (["0x" + "00" * 19 + "01"], 3),
                                   ([ACCOUNT], None), ([ACCOUNT], 0), ([ACCOUNT], -1),
                                   ([ACCOUNT, SECOND], 3)]:
            with self.subTest(targets=targets, threshold=threshold), self.assertRaises(ValueError):
                g.build("safe-owner", SAFE, targets, 2886, threshold)
        with self.assertRaises(ValueError):
            g.build("blacklist-owner", SAFE, [ACCOUNT], 2886, 3)

    def test_check_scripts_are_readonly_and_valid_shell(self):
        for action in g.ACTIONS:
            package = g.build(action, SAFE, [ACCOUNT], threshold=3 if action == "safe-owner" else None)
            script = g.readonly_check(action, package)
            subprocess.run(["bash", "-n"], input=script, text=True, check=True)
            self.assertNotIn("cast send", script)
            self.assertIn('--from "$SAFE"', script)
            if action.startswith("blacklist-"):
                self.assertIn(g.BLACKLIST_PUBLIC, script)
                self.assertIn('"$BLACKLIST_OWNER" = true', script)
            self.assertIn("No transaction sent", script)

    def test_output_roundtrip_and_existing_directory_protected(self):
        with tempfile.TemporaryDirectory() as temp:
            out = Path(temp) / "package"
            argv = ["generate.py", "blacklist-owner", "--safe", SAFE, "--address", ACCOUNT, "--out", str(out)]
            with mock.patch.object(sys, "argv", argv), contextlib.redirect_stdout(io.StringIO()):
                g.main()
                with self.assertRaises(FileExistsError):
                    g.main()
            package = json.loads((out / "safe-import.json").read_text())
            self.assertEqual(package["transactions"][0]["contractMethod"]["name"], "addBlacklistOwner")
            self.assertTrue((out / "REVIEW.md").is_file())
            self.assertTrue((out / "check-readonly.sh").is_file())


if __name__ == "__main__":
    unittest.main()
