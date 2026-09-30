#!/usr/bin/env python3
"""Prepare a non-staking snapshot-sync config. Does not start Nitro or edit its DB."""
import json
import os
import socket
import subprocess
import urllib.request
from pathlib import Path

DATA = Path('/data_mock/validator/config')
OUT = Path('/data_new/scripts/snapshot-sync.json')
IMAGE = 'sha256:b3571c0d8d4cd4e826a48895ed558927c4b003ea14050f5be3fb5ab09c62148f'
PARENT = 'http://10.1.2.16:8547'


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def main():
    if OUT.exists():
        raise SystemExit('STOP: snapshot-sync.json already exists')
    assert DATA.is_dir() and not DATA.is_symlink()
    mount = json.loads(run('findmnt','-J','-T',str(DATA)))['filesystems'][0]
    assert mount['target'] == '/data_mock', 'Not mounted on independent /data_mock'
    assert os.stat(DATA).st_dev != os.stat('/data_new/validator/config').st_dev
    ids = run('docker','ps','-q').split()
    if ids:
        for container in json.loads(run('docker','inspect',*ids)):
            for m in container['Mounts']:
                if not m.get('Source'):
                    continue
                src = Path(m['Source']).resolve()
                if src == DATA.resolve() or src in DATA.resolve().parents or DATA.resolve() in src.parents:
                    raise SystemExit('STOP: running container mounts snapshot data: '+container['Name'])
    for host, port in [('127.0.0.1',8349),('127.0.0.1',8372),
                       ('127.0.0.10',52000),('127.0.0.10',52001)]:
        with socket.socket() as s:
            s.bind((host,port))
    req = urllib.request.Request(PARENT,json.dumps(dict(jsonrpc='2.0',id=1,
        method='eth_chainId',params=[])).encode(),{'Content-Type':'application/json'})
    with urllib.request.urlopen(req,timeout=15) as r:
        chain = json.load(r)
    assert chain.get('result') == '0xa4b1', 'Parent RPC must be Arbitrum One42161'
    c = json.loads((DATA/'nodeConfig.json').read_text())
    info = json.loads(c['chain']['info-json'])
    assert isinstance(info,list) and info
    for entry in info:
        assert entry.get('parent-chain-id') == 42161, 'Snapshot chain metadata is not production parent'
        for key in ('sequencer-url','secondary-forwarding-target','feed-url','secondary-feed-url'):
            entry[key] = ''
    c['chain']['info-json'] = json.dumps(info)
    c['chain'].pop('dev-wallet',None)
    c['parent-chain'] = {'id':42161,'connection':{'url':PARENT}}
    n = c['node']
    n['sequencer'] = False
    n['batch-poster'] = {'enable':False}
    n['delayed-sequencer'] = {'enable':False}
    n['staker'] = {'enable':False,'only-create-wallet-contract':False}
    n['feed'] = {'input':{'url':[],'secondary-url':[]},'output':{'enable':False}}
    n.setdefault('parent-chain-reader',{})['enable'] = True
    n['block-validator'] = {'enable':False,'failure-is-fatal':False,
        'current-module-root':'0x767c9a47cced7ccc3bf419a7efdd9ffb0f23a5dba42f30f3de64f32e2f82c55f',
        'pending-upgrade-module-root':''}
    e = c.setdefault('execution',{})
    e['forwarding-target'] = 'null'
    e['secondary-forwarding-target'] = []
    e.setdefault('sequencer',{})['enable'] = False
    e.setdefault('caching',{})['archive'] = True
    e.setdefault('recording-database',{})['legacy-fee-account-preimages'] = True
    c['http'] = {'addr':'127.0.0.1','port':8349,'vhosts':['localhost','127.0.0.1'],
                 'corsdomain':[],'api':['eth','net','web3','arb','arbdebug']}
    c['ws'] = {'addr':'127.0.0.1','port':8372,'rpcprefix':'/ws'}
    c['pprof'] = False
    c['metrics'] = False
    c.setdefault('file-logging',{})['enable'] = False
    uid = int(run('docker','run','--rm','--network','none','--entrypoint','/usr/bin/id',IMAGE,'-u'))
    gid = int(run('docker','run','--rm','--network','none','--entrypoint','/usr/bin/id',IMAGE,'-g'))
    fd = os.open(OUT,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,'w') as f:
        json.dump(c,f,indent=2)
    os.chown(OUT,uid,gid)
    print('CONFIG READY:',OUT)
    print('DATA:',DATA)
    print('PARENT CHAIN: 42161')
    print('RPC: http://127.0.0.1:8349')
    print('STAKER / SEQUENCER / BATCH POSTER / BACKGROUND VALIDATION: disabled')
    print('Original config and database were not modified; container not started.')


if __name__=='__main__':
    main()
