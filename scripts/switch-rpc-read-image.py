#!/usr/bin/env python3
"""Switch a verified RPC shared-read image using the normal-exit lifecycle."""

import importlib.util
from pathlib import Path
import subprocess
import sys


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


if __name__ == "__main__":
    try:
        prep = load("rpc_read_profile", "prepare-rpc-read-switch.py")
        lifecycle = load("rpc_read_lifecycle", "switch-rpc-subsecond-image.py")
        lifecycle.main(prep_loader=prep.profile_for_plan)
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print("Read-concurrency image switch stopped: " + str(error), file=sys.stderr)
        raise SystemExit(1)
