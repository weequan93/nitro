#!/usr/bin/env python3
"""Read selected snapshot-sync config fields only; no DB/key reads or mutations."""
import json
import subprocess
import urllib.request
from pathlib import Path

CONFIG = Path('/data_new/scripts/snapshot-sync.json')
FIELDS = ('parent-chain.id', 'node.staker.enable', 'node.staker.strategy',
          'node.block-validator.enable', 'node.block-validator.current-module-root',
          'node.block-validator.pending-upgrade-module-root',
          'node.staker.dangerous.without-block-validator',
          'node.staker.dangerous.ignore-rollup-wasm-module-root',
          'node.batch-poster.enable', 'node.delayed-sequencer.enable',
          'execution.sequencer.enable')


def field(config, key):
    for part in key.split('.'):
        if not isinstance(config, dict) or part not in config:
            return None
        config = config[part]
    # Never print arbitrarily large/nested values from a config.
    return config if isinstance(config, (bool, int)) or config is None or (
        isinstance(config, str) and len(config) <= 80) else 'not_scalar'


def main():
    # Reject symlinks so this fixed-path reader cannot follow a link into /data.
    if any(p.is_symlink() for p in (CONFIG, *CONFIG.parents)):
        raise ValueError('Config path contains symlink')
    c = json.loads(CONFIG.read_text())
    meta = json.loads(subprocess.check_output(
        ['docker', 'inspect', 'snapshot-sync'], text=True, timeout=30))[0]
    cfg = meta.get('Config') or {}
    argv = (cfg.get('Entrypoint') or []) + (cfg.get('Cmd') or [])
    overrides = []
    config_argument_seen = False
    for index, token in enumerate(argv):
        if not isinstance(token, str):
            continue
        if token == '--conf.file=/snapshot-sync.json' or (
            token == '--conf.file' and index + 1 < len(argv) and argv[index+1] == '/snapshot-sync.json'):
            config_argument_seen = True
        if token.startswith('--') and token[2:].split('=', 1)[0] in FIELDS:
            overrides.append(token[2:].split('=', 1)[0])
    report = dict(configFile=str(CONFIG), selectedFileValues={k: field(c, k) for k in FIELDS},
                  configuredParentIsExpectedProductionRPC=field(c, 'parent-chain.connection.url') == 'http://10.1.2.16:8547',
                  containerRunning=meta['State']['Running'], imageId=meta['Image'],
                  expectedConfigArgumentSeen=config_argument_seen,
                  selectedCLIOverrideNames=overrides,
                  effectiveConfigFullyVerified=False,
                  note='File values only; unknown/missing values are not defaults. Wrapper/environment overrides are not resolved. No changes made.')
    body = json.dumps(dict(jsonrpc='2.0', id=1, method='arb_latestValidated', params=[])).encode()
    try:
        req = urllib.request.Request('http://127.0.0.1:8349', body, {'Content-Type': 'application/json'})
        with urllib.request.urlopen(req, timeout=30) as response:
            result = json.load(response)
        if 'error' in result:
            report['latestValidated'] = {'rpcErrorCode': result['error'].get('code')}
        else:
            v = result.get('result')
            report['latestValidated'] = ({k: v.get(k) for k in ('GlobalState', 'WasmRoots')} if isinstance(v, dict) else None)
    except Exception as exc:
        report['latestValidated'] = {'errorType': type(exc).__name__}
    print(json.dumps(report, indent=2))


if __name__ == '__main__':
    main()
