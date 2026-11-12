"""Offline image-switch lifecycle tests; all Docker operations are simulated."""

from copy import deepcopy
import importlib.util
import json
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import patch


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


switcher = load("image_switch", "switch-rpc-subsecond-image.py")
fixtures = load("image_switch_fixtures", "test_prepare_rpc_subsecond_switch.py")


class SwitchTests(unittest.TestCase):
    def setUp(self):
        self.fixture = fixtures.PreparationTests()
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        f = self.fixture
        self.prep, self.checker, self.ops = fixtures.trial, f.checker, f.ops
        f.info.update(Id="original", Image=self.prep.BASE, RestartCount=0,
                      State={"Running": True, "StartedAt": "2026-11-12T04:54:30Z", "Pid": 100,
                             "ExitCode": 0, "OOMKilled": False}, HostConfig={}, Mounts=[])
        f.info["Config"].update(Image="original:tag", Cmd=["--conf.file", "/config/node.json"],
                                 Entrypoint=["/usr/local/bin/split-val-entry.sh"], Env=[])
        f.info["Config"]["Labels"].update({"com.docker.compose.project": "main", "com.docker.compose.service": "nitro",
            "com.docker.compose.project.config_files": str(f.compose), "com.docker.compose.config-hash": "a" * 64})
        f.baseline.update(container_id="original", started_at=f.info["State"]["StartedAt"], image_ref="original:tag")
        f.baseline["hashes"]["container_layout_without_image"] = switcher.layout(f.info, self.ops)
        self.path = f.prepare()
        self.plan = json.loads(self.path.read_text())
        self.current = deepcopy(f.info)
        self.signals, self.creates = [], []
        self.exit_code = 0
        self.log = "INFO shutting down because of sigint\nINFO Blockchain stopped\n"
        self.fail_up = False
        f.stack.enter_context(patch.object(self.ops, "inspect", side_effect=self.inspect))
        f.stack.enter_context(patch.object(self.ops, "run", side_effect=self.read))
        f.stack.enter_context(patch.object(self.ops, "service_run", side_effect=self.create))
        f.stack.enter_context(patch.object(switcher.subprocess, "Popen", side_effect=self.stop))
        f.stack.enter_context(patch.object(switcher.subprocess, "run", side_effect=self.logs))
        f.stack.enter_context(patch.object(switcher, "signal_preflight"))

    def inspect(self, kind, name):
        if kind == "image":
            return {"Id": self.fixture.tag_id if name == self.prep.TAG else self.prep.BASE}
        self.assertIsNotNone(self.current)
        return deepcopy(self.current)

    def read(self, command, timeout=15):
        if command[:3] == ["docker", "container", "ls"]:
            return self.current["Id"] + "\n" if self.current else ""
        if command[-3:] == ["config", "--hash", "nitro"]:
            candidate = self.plan["override"] in command
            return "nitro " + ("b" if candidate else "a") * 64 + "\n"
        return self.fixture.render(command)

    def stop(self, command, **kwargs):
        self.assertEqual(command, ["docker", "container", "stop", "--signal", "SIGTERM", "--timeout", "-1", self.current["Id"]])
        self.assertEqual(kwargs["pass_fds"], (42,))
        self.signals.append(command)
        self.current["State"].update(Running=False, Pid=0, ExitCode=self.exit_code,
                                     FinishedAt="2026-11-12T14:20:00Z")
        return SimpleNamespace(poll=lambda: 0, wait=lambda **kwargs: 0)

    def logs(self, command, **kwargs):
        self.assertEqual(command[:2], ["docker", "logs"])
        self.assertEqual(kwargs["stderr"], switcher.subprocess.STDOUT)
        return SimpleNamespace(stdout=self.log)

    def create(self, command, log_path, timeout):
        self.assertEqual(command[-8:], ["up", "-d", "--no-deps", "--no-build", "--pull", "never", "--force-recreate", "nitro"])
        self.creates.append(command)
        if self.fail_up:
            self.current = None
            raise RuntimeError("simulated failure after removing old container")
        candidate = self.plan["override"] in command
        self.current = deepcopy(self.fixture.info)
        self.current.update(Id="new-candidate" if candidate else "new-base", Image=self.prep.CANDIDATE if candidate else self.prep.BASE)
        self.current["Config"]["Image"] = self.prep.TAG if candidate else "original:tag"
        labels = self.current["Config"]["Labels"]
        labels["com.docker.compose.project.config_files"] = ",".join(self.plan["baseline"]["compose_files"] + ([self.plan["override"]] if candidate else []))
        labels["com.docker.compose.config-hash"] = ("b" if candidate else "a") * 64
        self.current["State"].update(StartedAt="2026-11-12T14:21:00Z", Pid=200)

    def switch(self, rollback=False):
        return switcher.switch(self.path, rollback=rollback, prep=self.prep, checker=self.checker, lock_fd=42)

    def test_apply_and_rollback_preserve_json_and_scope_service_to_main(self):
        original = self.fixture.config_path.read_bytes()
        self.switch()
        self.assertEqual(self.current["Image"], self.prep.CANDIDATE)
        self.assertEqual(json.loads((self.path.parent / "apply.json").read_text())["state"], "started_health_pending")
        self.switch(rollback=True)
        self.assertEqual(self.current["Image"], self.prep.BASE)
        self.assertEqual(self.fixture.config_path.read_bytes(), original)
        self.assertEqual(len(self.signals), 2)
        self.assertEqual(len(self.creates), 2)

    def test_bad_exit_is_retained_without_recreation(self):
        self.exit_code = 137
        with self.assertRaisesRegex(ValueError, "exit normally"):
            self.switch()
        self.assertEqual(self.creates, [])
        self.assertEqual(self.current["Id"], "original")

    def test_missing_shutdown_markers_are_not_accepted(self):
        self.log = "INFO stopping\n"
        with self.assertRaisesRegex(ValueError, "markers missing"):
            self.switch()
        self.assertEqual(self.creates, [])

    def test_flush_failure_is_retained_without_recreation(self):
        self.log += "ERROR failed to close database on shutdown\n"
        with self.assertRaisesRegex(ValueError, "close/flush"):
            self.switch()
        self.assertEqual(self.creates, [])

    def test_references_and_input_changes_are_rejected_before_signal(self):
        self.fixture.tag_id = "sha256:other"
        with self.assertRaisesRegex(ValueError, "image reference changed"):
            self.switch()
        self.fixture.tag_id = self.prep.CANDIDATE
        self.fixture.config_path.write_text("{}")
        with self.assertRaisesRegex(ValueError, "Node JSON"):
            self.switch()
        self.assertEqual(self.signals, [])

    def test_prepare_instance_cannot_be_replaced_silently(self):
        self.current["State"]["StartedAt"] = "2026-11-12T14:00:00Z"
        with self.assertRaisesRegex(ValueError, "restarted since"):
            self.switch()
        self.assertEqual(self.signals, [])

    def test_failed_up_with_missing_container_can_roll_back_using_owned_evidence(self):
        self.fail_up = True
        with self.assertRaisesRegex(RuntimeError, "simulated failure"):
            self.switch()
        self.assertIsNone(self.current)
        self.fail_up = False
        self.switch(rollback=True)
        self.assertEqual(self.current["Image"], self.prep.BASE)
        self.assertEqual(len(self.signals), 1)

    def test_repeated_apply_does_not_restart_candidate(self):
        self.switch()
        self.switch()
        self.assertEqual(len(self.signals), 1)
        self.assertEqual(len(self.creates), 1)

    def test_corrupt_shutdown_evidence_cannot_authorize_missing_container_recovery(self):
        self.fail_up = True
        with self.assertRaises(RuntimeError):
            self.switch()
        (self.path.parent / "apply.shutdown.private.log").write_text("changed")
        self.fail_up = False
        with self.assertRaisesRegex(ValueError, "evidence changed"):
            self.switch(rollback=True)
        self.assertIsNone(self.current)
        self.assertEqual(len(self.creates), 1)

    def test_signal_preflight_requires_native_pid_one(self):
        proc = self.fixture.root / "proc"
        (proc / "100").mkdir(parents=True)
        (proc / "100/comm").write_text("nitro\n")
        # Use the actual function, which the lifecycle fixture otherwise mocks.
        real = load("signal_preflight_actual", "switch-rpc-subsecond-image.py")
        real.signal_preflight(self.current, proc)
        (proc / "100/comm").write_text("bash\n")
        with self.assertRaisesRegex(ValueError, "native Nitro as PID 1"):
            real.signal_preflight(self.current, proc)


if __name__ == "__main__":
    unittest.main()
