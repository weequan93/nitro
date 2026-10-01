import contextlib
import copy
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
spec = importlib.util.spec_from_file_location("manual_withdrawal", HERE / "prepare.py")
p = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p)

TX = "0x" + "a1" * 32
ITEM = "0x" + "11" * 32
ROOT = "0x" + "22" * 32
BLOCK_HASH = "0x" + "33" * 32


def word(value):
    return value.to_bytes(32, "big")


def event(index=3, value=0, payload=b"", log_index=0):
    data = bytes(12) + bytes.fromhex("44" * 20) + word(101) + word(99) + word(123)
    data += word(value) + word(192) + word(len(payload)) + payload + bytes((-len(payload)) % 32)
    return {"address": p.ARBSYS, "topics": [p.TOPIC, "0x" + "00" * 12 + "55" * 20, ITEM, "0x" + word(index).hex()],
            "data": "0x" + data.hex(), "logIndex": hex(log_index), "removed": False}


def proof(send=ITEM, root=ROOT):
    return send + root[2:] + word(96).hex() + word(2).hex() + "bb" * 32 + "cc" * 32


class FakeClient:
    def __init__(self, spent=False, registered=True):
        self.spent = spent
        self.registered = registered
        self.simulations = []
        self.proof = proof()
        self.outbox = "0x" + "00" * 12 + p.OUTBOX[2:]
        self.receipt = {"transactionHash": TX, "status": "0x1", "blockNumber": "0x65",
                        "blockHash": BLOCK_HASH, "logs": [event()]}

    def calldata(self, signature, *args):
        return "0x08635a95" if signature == p.EXECUTE else "0x00"

    def call(self, url, target, signature, *args, tag="latest"):
        if signature == "outbox()":
            return self.outbox
        if signature == "isSpent(uint256)":
            return "0x" + word(int(self.spent)).hex()
        if signature == "constructOutboxProof(uint64,uint64)":
            return self.proof
        if signature == p.ITEM:
            return ITEM
        if signature == "calculateMerkleRoot(bytes32[],uint256,bytes32)":
            return ROOT
        if signature == "roots(bytes32)":
            return BLOCK_HASH if self.registered else p.ZERO
        raise AssertionError(signature)

    def rpc(self, url, method, params):
        if method == "eth_chainId":
            return hex(2886 if url == p.L3_RPC else 42161)
        if method == "eth_getTransactionReceipt":
            return copy.deepcopy(self.receipt)
        if method == "eth_getBlockByNumber":
            if params[0] == "0x65":
                return {"hash": BLOCK_HASH, "sendCount": "0x4", "sendRoot": ROOT}
            return {"number": "0x20", "hash": BLOCK_HASH}
        if method == "eth_call":
            self.simulations.append(params)
            return "0x"
        raise AssertionError("Unexpected RPC method: " + method)


class Tests(unittest.TestCase):
    def invoke(self, fake, directory, *options):
        with mock.patch.object(p, "Client", return_value=fake), \
                mock.patch.object(sys, "argv", ["prepare.py", TX, "--out", str(directory), *options]), \
                contextlib.redirect_stdout(io.StringIO()):
            p.main()

    def test_event_native_value_and_dynamic_payload(self):
        decoded = p.decode_event(event(value=2**80, payload=b"abc"))
        self.assertEqual(decoded["value"], str(2**80))
        self.assertEqual(decoded["data"], "0x616263")
        self.assertEqual(p.message_args(decoded)[-2:], [str(2**80), "0x616263"])
        self.assertEqual(p.gateway_details(decoded)["payloadKind"], "other")

    def test_event_truncation_offset_and_removed_rejected(self):
        for bad in (dict(event(), removed=True), dict(event(), data="0x"),
                    dict(event(payload=b"abc"), data=event(payload=b"abc")["data"][:-2])):
            with self.assertRaises(ValueError):
                p.decode_event(bad)
        raw = bytearray(p.hexbytes(event()["data"]))
        raw[160:192] = word(224)
        with self.assertRaises(ValueError):
            p.decode_event(dict(event(), data="0x" + raw.hex()))

    def test_proof_dynamic_bounds(self):
        self.assertEqual(len(p.decode_proof(proof())["proof"]), 2)
        for bad in (proof()[:-2], proof() + "00", "0x" + "00" * 128):
            with self.assertRaises(ValueError):
                p.decode_proof(bad)

    def test_gateway_recipient_raw_amount(self):
        payload = bytes.fromhex("2e567b36")
        payload += b"".join(bytes(12) + bytes([n]) * 20 for n in (1, 2, 3))
        payload += word(2**200) + word(160) + word(0)
        details = p.gateway_details(p.decode_event(event(payload=payload)))
        self.assertEqual(details["recipient"], "0x" + "03" * 20)
        self.assertEqual(details["amountRaw"], str(2**200))

    def test_already_claimed_has_no_claim_script(self):
        with tempfile.TemporaryDirectory() as temp:
            out = Path(temp) / "output"
            fake = FakeClient(spent=True)
            self.invoke(fake, out)
            self.assertEqual(list(out.iterdir()), [out / "summary.json"])
            self.assertEqual(json.loads((out / "summary.json").read_text())["results"][0]["withdrawals"][0]["status"], "already_claimed")
            self.assertFalse(fake.simulations)

    def test_unknown_root_skips_simulation_and_records_wait(self):
        with tempfile.TemporaryDirectory() as temp:
            out = Path(temp) / "output"
            fake = FakeClient(registered=False)
            self.invoke(fake, out)
            report = json.loads((out / "summary.json").read_text())
            self.assertEqual(report["results"][0]["withdrawals"][0]["status"], "root_not_confirmed")
            self.assertFalse(fake.simulations)
            self.assertIn("Root not confirmed", (out / "claim-cli.sh").read_text())

    def test_ready_claim_simulated_at_saved_parent_block(self):
        with tempfile.TemporaryDirectory() as temp:
            out = Path(temp) / "output"
            fake = FakeClient()
            self.invoke(fake, out)
            self.assertEqual(fake.simulations[0][1], "0x20")
            self.assertEqual(fake.simulations[0][0]["value"], "0x0")
            report = json.loads((out / "summary.json").read_text())
            self.assertFalse(report["transactionsSent"])
            self.assertEqual(report["results"][0]["withdrawals"][0]["status"], "claim_prepared")
            self.assertEqual(json.loads((out / "wallet-claim.json").read_text())["index"], "3")
            subprocess.run(["bash", "-n", str(out / "claim-cli.sh")], check=True)
            self.assertIn("--keystore", (out / "claim-cli.sh").read_text())

    def test_multiple_withdrawals_need_explicit_index(self):
        with tempfile.TemporaryDirectory() as temp:
            out = Path(temp) / "output"
            fake = FakeClient()
            fake.receipt["logs"].append(event(index=2, log_index=1))
            with self.assertRaises(ValueError):
                self.invoke(fake, out)
            self.assertFalse(out.exists())
            self.invoke(fake, out, "--status-only")
            self.assertEqual(len(json.loads((out / "summary.json").read_text())["results"][0]["withdrawals"]), 2)

    def test_proof_send_mismatch_rejected_before_files_written(self):
        with tempfile.TemporaryDirectory() as temp:
            out = Path(temp) / "output"
            fake = FakeClient()
            fake.proof = proof(send=BLOCK_HASH)
            with self.assertRaises(ValueError):
                self.invoke(fake, out)
            self.assertFalse(out.exists())

    def test_wrong_outbox_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            fake = FakeClient()
            fake.outbox = p.ZERO
            with self.assertRaises(ValueError):
                self.invoke(fake, Path(temp) / "output")

    def test_rpc_broadcast_methods_denied(self):
        client = object.__new__(p.Client)
        with self.assertRaises(ValueError), mock.patch.object(subprocess, "run") as run:
            client.rpc("unused", "eth_sendRawTransaction", ["unused"])
        run.assert_not_called()


if __name__ == "__main__":
    unittest.main()
