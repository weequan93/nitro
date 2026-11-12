"""Offline deployment checks; never accesses Docker or a live node."""

from contextlib import ExitStack, redirect_stdout
from copy import deepcopy
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import subprocess
import tempfile
import unittest
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("trial-compaction-pacing.py")
spec = importlib.util.spec_from_file_location("pacing_trial", SCRIPT)
trial = importlib.util.module_from_spec(spec)
spec.loader.exec_module(trial)
RATE = 64 * 1024**2


class TrialTests(unittest.TestCase):
    def setUp(self):
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.root = Path(self.stack.enter_context(tempfile.TemporaryDirectory()))
        self.source = self.root / "node.json"
        self.config = {"persistent": {"pebble": {"max-concurrent-compactions": 2}},
                       "execution": {"caching": {"database-cache": 2048}},
                       "unrelated": "literal $value must survive"}
        self.original = (json.dumps(self.config) + "\n").encode()
        self.source.write_bytes(self.original)
        self.source.chmod(0o640)
        self.compose_file = self.root / "compose.yaml"
        self.compose_file.write_text("services: {}\n")
        self.base = {
            "Id": "before", "Image": "sha256:pinned",
            "State": {"Running": True, "StartedAt": "2026-11-11T03:00:00Z", "OOMKilled": False},
            "Config": {"Image": "repo:unchanged", "Cmd": ["--conf.file", "/config/node.json", "$literal"],
                       "Entrypoint": ["/usr/local/bin/split-val-entry.sh"],
                       "Env": ["SECRET=unprinted"], "Labels": {
                           "com.docker.compose.project": "main", "com.docker.compose.service": "nitro",
                           "com.docker.compose.project.config_files": str(self.compose_file),
                           "com.docker.compose.project.working_dir": str(self.root),
                           "com.docker.compose.config-hash": "a" * 64}},
            "HostConfig": {"NetworkMode": "main_default", "PortBindings": {"8449/tcp": [{"HostPort": "1100"}]}},
            "Mounts": [{"Type": "bind", "Source": str(self.source), "Destination": "/config/node.json", "RW": False}],
        }
        self.info = deepcopy(self.base)
        self.model = {"services": {"nitro": {"image": "repo:unchanged", "command": ["$literal"]}}}
        self.image_id = "sha256:pinned"
        self.hash = "a" * 64
        self.operations = []
        self.commands = []
        self.fail_up = False
        self.stack.enter_context(patch.object(trial, "run", side_effect=self.docker_read))
        self.stack.enter_context(patch.object(trial, "service_run", side_effect=self.service_run))
        self.stack.enter_context(patch.object(trial, "native_argv", return_value=self.base["Config"]["Cmd"]))
        self.stack.enter_context(patch.object(trial.os, "fchown"))
        self.output = self.stack.enter_context(redirect_stdout(io.StringIO()))

    def docker_read(self, command, timeout=15):
        self.commands.append(command)
        if command[:3] == ["docker", "container", "inspect"]:
            self.assertIsNotNone(self.info)
            return json.dumps([self.info])
        if command[:3] == ["docker", "image", "inspect"]:
            return json.dumps([{"Id": self.image_id}])
        if command[:3] == ["docker", "container", "ls"]:
            return self.info["Id"] + "\n" if self.info else ""
        if command[-3:] == ["config", "--hash", "nitro"]:
            return "nitro " + self.hash + "\n"
        if command[-3:] == ["config", "--format", "json"]:
            return json.dumps(self.model)
        self.fail("Unexpected Docker request: " + repr(command))

    def service_run(self, command, log_path, timeout):
        self.operations.append((command, self.source.read_bytes()))
        if command[-4:] == ["stop", "-t", "120", "nitro"]:
            self.info["State"]["Running"] = False
        elif command[-1] == "nitro" and "up" in command:
            if self.fail_up:
                self.info = None  # Compose removed the old container, then failed.
                raise RuntimeError("simulated creation failure")
            self.info = deepcopy(self.base)
            self.info["Id"] = "replacement-" + str(len(self.operations))
            self.info["State"]["StartedAt"] = "2026-11-11T03:10:00Z"
        else:
            self.fail("Unexpected service operation")

    def prepare(self):
        return trial.prepare(self.root / "backups", RATE)

    def test_prepare_backs_up_exact_bytes_without_touching_service(self):
        path = self.prepare()
        self.assertEqual(self.operations, [])
        self.assertEqual(self.source.read_bytes(), self.original)
        self.assertEqual((path.parent / "before.json").read_bytes(), self.original)
        for name, mode in ((path.parent, 0o700), (path, 0o600), (path.parent / "before.json", 0o600)):
            self.assertEqual(stat.S_IMODE(name.stat().st_mode), mode)
        changed = json.loads((path.parent / "candidate.json").read_bytes())
        changed["persistent"]["pebble"].pop("compaction-output-bytes-per-second")
        self.assertEqual(changed, self.config)
        self.assertNotIn("unprinted", self.output.getvalue())
        self.assertIn("ROLLBACK:", self.output.getvalue())

    def test_apply_then_rollback_preserve_runtime_and_restore_exact_json(self):
        path = self.prepare()
        trial.activate(path)
        self.assertEqual(self.operations[0][1], self.original)  # Stop precedes JSON edit.
        self.assertEqual(trial.lookup(json.loads(self.operations[1][1]), trial.KEY), RATE)
        self.assertEqual(trial.runtime_fingerprint(self.info), trial.runtime_fingerprint(self.base))
        self.assertEqual(self.info["Config"]["Cmd"][-1], "$literal")
        self.assertIn("--force-recreate", self.operations[1][0])
        self.assertEqual(self.operations[1][0][-4:], ["--pull", "never", "--force-recreate", "nitro"])
        trial.activate(path, rollback=True)
        self.assertEqual(self.source.read_bytes(), self.original)
        self.assertEqual(stat.S_IMODE(self.source.stat().st_mode), 0o640)
        self.assertEqual(len(self.operations), 4)
        record = json.loads((path.parent / "rollback.json").read_text())
        self.assertEqual(record["state"], "started_health_pending")

    def test_all_preflight_drift_rejected_before_stop(self):
        for drift in ("json", "compose_file", "compose_model", "image", "runtime", "restart", "candidate"):
            with self.subTest(drift=drift):
                path = self.prepare()
                if drift == "json":
                    self.source.write_bytes(self.original + b" ")
                elif drift == "compose_file":
                    self.compose_file.write_text("changed\n")
                elif drift == "compose_model":
                    self.hash = "b" * 64
                elif drift == "image":
                    self.image_id = "sha256:other"
                elif drift == "runtime":
                    self.info["Config"]["Cmd"].append("--different")
                elif drift == "restart":
                    self.info["State"]["StartedAt"] = "later"
                else:
                    (path.parent / "candidate.json").write_text("{}")
                with self.assertRaises((ValueError, RuntimeError)):
                    trial.activate(path)
                self.assertEqual(self.operations, [])
                self.source.write_bytes(self.original)
                self.compose_file.write_text("services: {}\n")
                self.hash = "a" * 64
                self.image_id = "sha256:pinned"
                self.info = deepcopy(self.base)

    def test_resource_drift_refuses_preparation(self):
        self.info["HostConfig"]["NanoCpus"] = 2 * 10**9
        with self.assertRaisesRegex(ValueError, "resource differs"):
            self.prepare()
        self.assertEqual(self.operations, [])

    def test_all_original_compose_files_retained(self):
        extra = self.root / "override.yaml"
        extra.write_text("services: {}\n")
        self.info["Config"]["Labels"]["com.docker.compose.project.config_files"] += "," + str(extra)
        path = self.prepare()
        prefix = json.loads(path.read_text())["compose"]
        self.assertEqual(prefix[-4:], ["-f", str(self.compose_file), "-f", str(extra)])

    def test_failure_after_removal_has_working_explicit_rollback(self):
        path = self.prepare()
        self.fail_up = True
        with self.assertRaises(RuntimeError):
            trial.activate(path)
        self.assertEqual(trial.lookup(json.loads(self.source.read_text()), trial.KEY), RATE)
        self.assertIsNone(self.info)
        self.fail_up = False
        trial.activate(path, rollback=True)
        self.assertEqual(self.source.read_bytes(), self.original)
        self.assertTrue(self.info["State"]["Running"])

    def test_rollback_does_not_overwrite_subsequent_edits(self):
        path = self.prepare()
        trial.activate(path)
        self.source.write_text('{"other": "operator edit"}')
        with self.assertRaisesRegex(ValueError, "refusing to overwrite"):
            trial.activate(path, rollback=True)
        self.assertEqual(len(self.operations), 2)

    def test_bound_config_permission_drift_rejected(self):
        path = self.prepare()
        self.source.chmod(0o600)
        with self.assertRaisesRegex(ValueError, "permissions changed"):
            trial.activate(path)
        self.assertEqual(self.operations, [])

    def test_rollback_never_assumes_disk_json_is_loaded(self):
        path = self.prepare()
        trial.activate(path)
        self.source.write_bytes(self.original)  # File alone does not reload Nitro.
        trial.activate(path, rollback=True)
        self.assertEqual(len(self.operations), 4)

    def test_unused_plan_rollback_is_noop(self):
        trial.activate(self.prepare(), rollback=True)
        self.assertEqual(self.operations, [])

    def test_reject_config_loaders_and_select_specific_bind(self):
        for name in (trial.KEY, "conf.string", "conf.env-prefix", "conf.s3.secret-key"):
            with self.subTest(name=name), self.assertRaises(ValueError):
                trial.local_config(self.base, ["--conf.file=/config/node.json", "--" + name + "=value"])
        self.base["Mounts"].insert(0, {"Type": "bind", "Source": "/unavailable", "Destination": "/config"})
        path, config = trial.local_config(self.base, ["--conf.file=/config/node.json"])
        self.assertEqual(path, self.source.resolve())
        self.assertEqual(config, self.config)


class ConfigTests(unittest.TestCase):
    def test_flat_and_nested_change_only_pacing(self):
        self.assertEqual(trial.candidate({trial.KEY: 0, "other": 3}, RATE), {trial.KEY: RATE, "other": 3})
        result = trial.candidate({"persistent": {"pebble": {"max-concurrent-compactions": 2}}}, RATE)
        self.assertEqual(result["persistent"]["pebble"], {"max-concurrent-compactions": 2,
                                                        "compaction-output-bytes-per-second": RATE})

    def test_ambiguous_and_invalid_configs_refused(self):
        for config in ([], {trial.KEY: 1}, {trial.KEY: False}, {trial.KEY: 0, "persistent": {}},
                       {"persistent.pebble": {}}, {"persistent": {"pebble.compaction-output-bytes-per-second": 0}},
                       {"persistent": None}):
            with self.subTest(config=config), self.assertRaises(ValueError):
                trial.candidate(config, RATE)
        for rate in (True, 0, -1, 2**63):
            with self.subTest(rate=rate), self.assertRaises(ValueError):
                trial.candidate({}, rate)

    def test_service_errors_go_to_private_log(self):
        with tempfile.TemporaryDirectory() as folder:
            log = Path(folder) / "private.log"
            with patch.object(trial.subprocess, "run", return_value=subprocess.CompletedProcess([], 1)) as call:
                with self.assertRaisesRegex(RuntimeError, "private log"):
                    trial.service_run(["docker", "compose", "stop"], log, 10)
                self.assertEqual(call.call_args.kwargs["stderr"], subprocess.STDOUT)
                self.assertEqual(call.call_args.kwargs["stdin"], subprocess.DEVNULL)


class ConcurrencyTests(TrialTests):
    def setUp(self):
        super().setUp()
        self.config["persistent"]["pebble"]["compaction-output-bytes-per-second"] = RATE
        self.original = (json.dumps(self.config) + "\n").encode()
        self.source.write_bytes(self.original)
        os.utime(self.source, (1, 1))

    def prepare(self):
        return trial.prepare(self.root / "backups", 1, trial.CONCURRENCY_KEY)

    def test_prepare_backs_up_exact_bytes_without_touching_service(self):
        path = self.prepare()
        self.assertEqual(self.operations, [])
        changed = json.loads((path.parent / "candidate.json").read_bytes())
        self.assertEqual(changed["persistent"]["pebble"]["compaction-output-bytes-per-second"], RATE)
        changed["persistent"]["pebble"]["max-concurrent-compactions"] = 2
        self.assertEqual(changed, self.config)

    def test_apply_then_rollback_preserve_runtime_and_restore_exact_json(self):
        path = self.prepare()
        trial.activate(path)
        applied = json.loads(self.source.read_bytes())
        self.assertEqual(trial.lookup(applied, trial.CONCURRENCY_KEY), 1)
        self.assertEqual(trial.lookup(applied, trial.KEY), RATE)
        trial.activate(path, rollback=True)
        self.assertEqual(self.source.read_bytes(), self.original)
        self.assertEqual(trial.runtime_fingerprint(self.info), trial.runtime_fingerprint(self.base))
        self.assertEqual(len(self.operations), 4)

    def test_all_preflight_drift_rejected_before_stop(self):
        # Exercise concurrency plans with every existing drift/recovery test.
        for kind in ("json", "candidate", "native"):
            os.utime(self.source, (1, 1))
            path = self.prepare()
            if kind == "json":
                self.source.write_bytes(self.original + b" ")
            elif kind == "candidate":
                replacement = json.loads((path.parent / "candidate.json").read_bytes())
                replacement["persistent"]["pebble"]["compaction-output-bytes-per-second"] = 1
                data = json.dumps(replacement).encode()
                (path.parent / "candidate.json").write_bytes(data)
                plan = json.loads(path.read_bytes())
                plan["candidate_sha"] = trial.digest(data)
                trial.save(path, plan)
            with (patch.object(trial, "native_argv", return_value=["changed"])
                  if kind == "native" else ExitStack()):
                with self.assertRaises(ValueError):
                    trial.activate(path)
            self.assertEqual(self.operations, [])
            self.source.write_bytes(self.original)

    def test_failure_after_removal_has_working_explicit_rollback(self):
        path = self.prepare()
        self.fail_up = True
        with self.assertRaises(RuntimeError):
            trial.activate(path)
        self.fail_up = False
        trial.activate(path, rollback=True)
        self.assertEqual(self.source.read_bytes(), self.original)

    def baseline(self):
        layout = deepcopy(self.base)
        layout["Image"] = "image_checked_separately"
        model = deepcopy(self.model)
        model["services"]["nitro"].pop("image")
        before = self.root / "loaded.json"
        before.write_bytes(self.original)
        baseline = self.root / "baseline.json"
        trial.save(baseline, {
            "schema": 1, "container_id": self.base["Id"], "started_at": self.base["State"]["StartedAt"],
            "image": self.base["Image"], "hashes": {
                "bound_json": trial.digest(self.original),
                "native_arguments": trial.digest(json.dumps(self.base["Config"]["Cmd"]).encode()),
                "container_layout_without_image": trial.runtime_fingerprint(layout),
                "compose_model_without_main_image": trial.digest(json.dumps(model, sort_keys=True).encode()),
            }})
        return baseline, before

    def pending(self):
        baseline, before = self.baseline()
        proposed = trial.candidate(self.config, 1, trial.CONCURRENCY_KEY)
        self.source.write_text(json.dumps(proposed))
        return baseline, before

    def test_pending_edit_is_verified_against_loaded_baseline_and_applied(self):
        baseline, before = self.pending()
        pending_bytes = self.source.read_bytes()
        path = trial.prepare(self.root / "backups", 1, trial.CONCURRENCY_KEY, baseline, before)
        self.assertEqual(self.operations, [])
        self.assertEqual(self.source.read_bytes(), pending_bytes)
        trial.activate(path)
        self.assertEqual(self.source.read_bytes(), pending_bytes)
        self.assertEqual(self.operations[0][1], pending_bytes)
        trial.activate(path, rollback=True)
        self.assertEqual(self.source.read_bytes(), self.original)

    def test_pending_apply_failure_has_working_rollback(self):
        baseline, before = self.pending()
        path = trial.prepare(self.root / "backups", 1, trial.CONCURRENCY_KEY, baseline, before)
        self.fail_up = True
        with self.assertRaises(RuntimeError):
            trial.activate(path)
        self.assertIsNone(self.info)
        self.fail_up = False
        trial.activate(path, rollback=True)
        self.assertEqual(self.source.read_bytes(), self.original)

    def test_rollback_remains_available_with_stopped_or_unready_native_process(self):
        baseline, before = self.pending()
        path = trial.prepare(self.root / "backups", 1, trial.CONCURRENCY_KEY, baseline, before)
        trial.activate(path)
        self.info["State"]["Running"] = False
        with patch.object(trial, "native_argv", side_effect=RuntimeError("native process unavailable")):
            trial.activate(path, rollback=True)
        self.assertEqual(self.source.read_bytes(), self.original)
        self.assertTrue(self.info["State"]["Running"])

    def test_pending_plan_refuses_later_edits_before_stopping(self):
        baseline, before = self.pending()
        path = trial.prepare(self.root / "backups", 1, trial.CONCURRENCY_KEY, baseline, before)
        self.source.write_bytes(self.source.read_bytes() + b" ")
        with self.assertRaisesRegex(ValueError, "refusing to overwrite"):
            trial.activate(path)
        self.assertEqual(self.operations, [])

    def test_prepare_and_apply_cli_handles_pending_edit(self):
        baseline, before = self.pending()
        with patch.object(trial.sys, "argv", [str(SCRIPT), "--compactions", "1", "--prepare-and-apply",
                "--pending-baseline", str(baseline), "--loaded-config", str(before),
                "--root", str(self.root / "backups")]):
            trial.main()
        self.assertEqual(trial.lookup(json.loads(self.source.read_bytes()), trial.CONCURRENCY_KEY), 1)
        self.assertEqual(len(self.operations), 2)

    def test_pending_edit_rejects_unproven_or_additional_changes(self):
        for drift in ("backup", "other_setting", "restarted", "baseline_hash"):
            with self.subTest(drift=drift):
                self.info = deepcopy(self.base)
                baseline, before = self.pending()
                if drift == "backup":
                    before.write_bytes(before.read_bytes() + b" ")
                elif drift == "other_setting":
                    data = json.loads(self.source.read_bytes())
                    data["execution"]["caching"]["database-cache"] = 4096
                    self.source.write_text(json.dumps(data))
                elif drift == "restarted":
                    self.info["State"]["StartedAt"] = "2026-11-12T03:00:00Z"
                else:
                    saved = json.loads(baseline.read_bytes())
                    saved["hashes"]["native_arguments"] = "wrong"
                    trial.save(baseline, saved)
                with self.assertRaises(ValueError):
                    trial.prepare(self.root / "backups", 1, trial.CONCURRENCY_KEY, baseline, before)
                self.assertEqual(self.operations, [])

    def test_edited_json_without_loaded_baseline_is_refused(self):
        self.pending()
        with self.assertRaisesRegex(ValueError, "loaded settings"):
            self.prepare()
        self.assertEqual(self.operations, [])

    def test_concurrency_requires_existing_pacing_and_cli_cannot_shadow_it(self):
        for pacing in (0, 32 * 1024**2, None, True):
            data = deepcopy(self.config)
            data["persistent"]["pebble"]["compaction-output-bytes-per-second"] = pacing
            with self.assertRaises(ValueError):
                trial.candidate(data, 1, trial.CONCURRENCY_KEY)
        with patch.object(trial, "native_argv", return_value=self.base["Config"]["Cmd"] +
                          ["--" + trial.CONCURRENCY_KEY, "2"]):
            with self.assertRaisesRegex(ValueError, "CLI concurrency"):
                self.prepare()


if __name__ == "__main__":
    unittest.main()
