#!/usr/bin/env python3
"""Run two diagnostic binaries offline with only exported witness files mounted."""
import argparse
import json
import subprocess
import sys
import uuid
from pathlib import Path

LEGACY = '0x9c8e05504b6482b9903a168aed23589999c6bc9b96b8faa297df171409a6b419'
CURRENT = '0x8e5465ac51d40a9e216fb39659b9271745825ec1238eb3a13fcca2a18c6c516a'


def outcome(baseline, changed):
    if baseline != CURRENT:
        return 'baseline_not_reproduced_do_not_interpret_mutation'
    if changed == LEGACY:
        return 'duplicate_error_change_reproduces_legacy_block_hash'
    return 'duplicate_error_change_does_not_reproduce_legacy_block_hash'


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('input',type=Path)
    p.add_argument('--out',type=Path,required=True)
    p.add_argument('--image',default='deriw-grant-replay:16c17ee3-amd64')
    p.add_argument('--timeout',type=int,default=600)
    a=p.parse_args()
    e=json.loads(a.input.read_text());e=e.get('result',e)
    if e.get('Id') != 125022703 or e.get('ExpectedEndState',{}).get('BlockHash') != CURRENT:
        p.error('This experiment is only for saved case 125022703 with the known expected hash')
    a.out.mkdir(parents=True,exist_ok=False);a.out=a.out.resolve()
    inputs=a.out/'input'
    exporter=(Path(__file__).resolve().parent / '../legacy/tools/export-native-input.py')
    subprocess.run([sys.executable,str(exporter),str(a.input.resolve()),'--out',str(inputs)],check=True,stdout=subprocess.DEVNULL)
    args=json.loads((inputs/'args.json').read_text())
    args=[v.replace(str(inputs)+'/', '/input/') for v in args]
    report={'results':{},'note':'Native diagnostic experiment; does not replace execution of the original old WASM.'}
    for binary in ('replay-baseline','replay-duplicate-error'):
        name='grant-replay-'+uuid.uuid4().hex[:12]
        command=['docker','run','--rm','--name',name,'--network','none','--read-only',
                 '--cap-drop','ALL','--security-opt','no-new-privileges',
                 '--mount',f'type=bind,src={inputs},dst=/input,readonly',
                 '--entrypoint','/usr/local/bin/'+binary,a.image]+args
        log=a.out/(binary+'.log')
        try:
            with log.open('wb') as f:
                r=subprocess.run(command,stdout=f,stderr=subprocess.STDOUT,timeout=a.timeout)
            text=log.read_text(errors='replace')
            parsed=[json.loads(line.split('DIAGNOSTIC_RESULT ',1)[1]) for line in text.splitlines() if line.startswith('DIAGNOSTIC_RESULT ')]
            row={'exitCode':r.returncode,'result':parsed[-1] if parsed else None,
                 'missingPreimageReported':'preimage not found' in text.lower()}
        except subprocess.TimeoutExpired:
            subprocess.run(['docker','rm','-f',name],capture_output=True)
            row={'error':'timeout','result':None}
        report['results'][binary]=row
        print(binary,json.dumps(row),flush=True)
        if binary=='replay-baseline' and (row.get('exitCode')!=0 or (row.get('result') or {}).get('BlockHash')!=CURRENT):
            break
    base=report['results']['replay-baseline'].get('result') or {}
    changed=report['results'].get('replay-duplicate-error',{}).get('result') or {}
    report['conclusion']=outcome(base.get('BlockHash'),changed.get('BlockHash'))
    if base.get('BlockHash') == CURRENT and report['results'].get('replay-duplicate-error', {}).get('exitCode') != 0:
        report['conclusion']='mutation_failed_inspect_log_no_causal_conclusion'
    (a.out/'summary.json').write_text(json.dumps(report,indent=2)+'\n')
    print('CONCLUSION',report['conclusion'])
    print('OUTPUT',a.out)


if __name__=='__main__':
    main()
