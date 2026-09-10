"""Local fake-node tests: python3 -B scripts/test_nitro_getlogs_load.py."""

import http.server
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock


SCRIPT = Path(__file__).with_name("nitro-getlogs-load.py")


class DiagnosticsTest(unittest.TestCase):
    def test_reset_identifies_failed_preflight(self):
        spec = importlib.util.spec_from_file_location("nitro_load", SCRIPT)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with mock.patch.object(module, "fetch", side_effect=ConnectionResetError(104, "Connection reset by peer")):
            with mock.patch("builtins.print"):
                with self.assertRaisesRegex(ValueError, "eth_blockNumber at http://127.0.0.1:8449 failed.*No eth_getLogs"):
                    module.rpc("http://127.0.0.1:8449", "eth_blockNumber", [])


class LoadTest(unittest.TestCase):
    def setUp(self):
        self.queries = []
        self.large = False
        self.slow = False
        self.growing = False
        self.metrics_count = 0
        test = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def reply(self, data):
                encoded = json.dumps(data).encode()
                self.send_response(200)
                self.send_header("Content-Length", str(len(encoded)))
                self.end_headers()
                try:
                    self.wfile.write(encoded)
                except (BrokenPipeError, ConnectionResetError):
                    pass  # Expected when testing client timeouts/response caps.

            def do_GET(self):
                test.metrics_count += 1
                self.reply({"memstats": {
                    "HeapAlloc": (3 if test.growing and test.metrics_count > 1 else 1) * 1024 ** 3,
                    "HeapInuse": 2 * 1024 ** 3,
                    "HeapIdle": 1024 ** 3, "HeapReleased": 512 * 1024 ** 2,
                    "Sys": 3 * 1024 ** 3, "NextGC": 2 * 1024 ** 3, "NumGC": 10,
                }, "system/cpu/goroutines": 100})

            def do_POST(self):
                payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                if payload["method"] == "eth_blockNumber":
                    self.reply({"result": "0x64"})
                elif payload["method"] == "eth_chainId":
                    self.reply({"result": "0x1234"})
                else:
                    test.queries.append(payload)
                    if test.slow:
                        time.sleep(0.15)
                    if test.large:
                        self.reply({"result": ["x" * 9000]})
                    elif payload["id"] % 2 == 0:
                        self.reply({"error": {"code": -32002, "message": "request timed out"}})
                    else:
                        self.reply({"result": []})

            def log_message(self, *args):
                pass

        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.temp = tempfile.TemporaryDirectory()

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()
        self.temp.cleanup()

    def run_test(self, *extra):
        url = "http://127.0.0.1:%s" % self.server.server_port
        output = Path(self.temp.name) / "out"
        result = subprocess.run([
            sys.executable, "-B", str(SCRIPT), "--url", url, "--metrics-url", url,
            "--output", str(output), "--duration", "2", "--max-requests", "6",
            "--concurrency", "3", "--rps", "0", "--cooldown", "0.06", "--interval", "0.02",
            "--from-block", "90", "--to-block", "100", "--max-range", "3", *extra,
        ], capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        summary = json.loads((output / "summary.json").read_text())
        return output, summary

    def test_bounded_load_and_rpc_errors(self):
        address = "0x" + "01" * 20
        topic = "0x" + "02" * 32
        output, summary = self.run_test("--address", address, "--topic", topic)
        self.assertEqual(summary["issued"], 6)
        self.assertEqual(summary["completed"], 6)
        self.assertEqual(summary["outcomes"], {"ok": 3, "rpc_error": 3})
        self.assertGreaterEqual(self.metrics_count, 3)
        self.assertEqual(len((output / "requests.jsonl").read_text().splitlines()), 6)
        for request in self.queries:
            query = request["params"][0]
            first, last = int(query["fromBlock"], 16), int(query["toBlock"], 16)
            self.assertTrue(90 <= first <= last <= 100)
            self.assertTrue(1 <= last - first + 1 <= 3)
            self.assertEqual(query["address"], address)
            self.assertEqual(query["topics"], [topic])

    def test_heap_guard_prevents_load(self):
        _, summary = self.run_test("--stop-heap-gib", "0.5")
        self.assertEqual(summary["issued"], 0)
        self.assertIn("threshold", summary["reason"])
        self.assertEqual(self.queries, [])

    def test_fixed_block_no_metrics(self):
        _, summary = self.run_test("--from-block", "99", "--to-block", "99", "--no-metrics")
        self.assertEqual(summary["completed"], 6)
        self.assertEqual(self.metrics_count, 0)
        for request in self.queries:
            self.assertEqual(request["params"][0], {"fromBlock": "0x63", "toBlock": "0x63"})

    def test_large_responses_are_streamed(self):
        self.large = True
        _, summary = self.run_test()
        self.assertEqual(summary["outcomes"], {"http_200_large": 6})

    def test_client_timeouts_are_distinct_from_rpc_timeouts(self):
        self.slow = True
        _, summary = self.run_test("--timeout", "0.03")
        self.assertEqual(summary["outcomes"], {"client_timeout": 6})

    def test_heap_guard_stops_running_load(self):
        self.growing = True
        _, summary = self.run_test("--stop-heap-gib", "2", "--max-requests", "1000", "--rps", "50")
        self.assertGreater(summary["issued"], 0)
        self.assertLess(summary["issued"], 1000)
        self.assertEqual(summary["issued"], summary["completed"])
        self.assertIn("threshold", summary["reason"])


if __name__ == "__main__":
    unittest.main()
