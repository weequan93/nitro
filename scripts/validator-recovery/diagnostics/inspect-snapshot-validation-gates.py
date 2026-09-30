#!/usr/bin/env python3
"""Read selected validation controls, process arguments and stop markers; no changes."""
import json
import re
import subprocess
from pathlib import Path

BASE = 'node.block-validator.'
KEYS = [BASE + k for k in (
    'enable', 'validation-poll', 'prerecorded-blocks', 'recording-iter-limit',
    'validation-sent-limit', 'forward-blocks', 'memory-free-limit',
    'current-module-root', 'pending-upgrade-module-root', 'failure-is-fatal',
    'dangerous.reset-block-validation', 'dangerous.revalidation.start-block',
    'dangerous.revalidation.end-block', 'dangerous.revalidation.quit-after-revalidation')]


def run(args):
    return subprocess.run(args, capture_output=True, text=True, timeout=60, check=True)


def get(c, key):
    for part in key.split('.'):
        if not isinstance(c, dict) or part not in c:
            return None
        c = c[part]
    return safe(c)


def safe(v):
    if isinstance(v, (bool, int)) or v is None:
        return v
    if isinstance(v, str) and len(v) < 90 and re.fullmatch(r'[0-9A-Za-z.+-]*', v):
        return v
    return '[unrecognized value omitted]'


def main():
    p = Path('/data_new/scripts/snapshot-sync.json')
    if any(x.is_symlink() for x in (p, *p.parents)):
        raise ValueError('Config path contains symlink')
    c = json.loads(p.read_text())
    meta = json.loads(run(['docker', 'inspect', 'snapshot-sync']).stdout)[0]
    report = {'fileValues': {k: get(c, k) for k in KEYS}, 'actualNitroCLI': [],
              'containerStartedAt': meta['State']['StartedAt'],
              'effectiveConfigFullyVerified': False,
              'note': 'null means absent, not zero. CLI shows selected arguments only. Environment/defaults not resolved.'}
    top = run(['docker', 'top', 'snapshot-sync', '-eo', 'pid,comm']).stdout
    for line in top.splitlines()[1:]:
        fields = line.split()
        if len(fields) != 2 or fields[1] != 'nitro' or not fields[0].isdigit():
            continue
        pid = fields[0]
        try:
            argv = Path('/proc/' + pid + '/cmdline').read_bytes().decode().strip('\0').split('\0')
            selected = []
            for i, arg in enumerate(argv):
                name, sep, value = arg.removeprefix('--').partition('=')
                if arg.startswith('--') and name in KEYS:
                    if not sep:
                        value = argv[i+1] if i+1 < len(argv) and not argv[i+1].startswith('--') else '(standalone flag)'
                    selected.append({'flag': name, 'value': safe(value)})
            report['actualNitroCLI'].append({'pid': pid, 'selectedArguments': selected})
        except Exception as exc:
            report['actualNitroCLI'].append({'pid': pid, 'errorType': type(exc).__name__})
    # Search complete available logs from this start, not just the recent tail.
    logs = run(['docker', 'logs', '--since', meta['State']['StartedAt'], 'snapshot-sync'])
    markers = []
    for line in (logs.stdout + '\n' + logs.stderr).splitlines():
        if re.search(r'revalidation done|validator got error|validation not set up|low on memory|failed writing new validated', line, re.I):
            # Suppress URLs in any diagnostic line.
            markers.append(re.sub(r'(?:https?|wss?)://\S+', '[URL omitted]', line))
    report['stopOrWaitMarkers'] = markers[-30:]
    print(json.dumps(report, indent=2))


if __name__ == '__main__':
    main()
