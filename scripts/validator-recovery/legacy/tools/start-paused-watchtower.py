#!/usr/bin/env python3
"""Start an isolated Watchtower on the replaced disposable DB and paused local fork.
Never changes /data or /data_mock, and never stops any existing container.
"""
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import urllib.request
from datetime import datetime, timezone

DATA=Path('/data_new/validator/config')
DB=DATA/'Deriw Chain/nitro'
IMAGE='sha256:b3571c0d8d4cd4e826a48895ed558927c4b003ea14050f5be3fb5ab09c62148f'
ROOT='0x121d685e2fdb0e3291592d6b90bd70d503951335d19d96455448eb7a14d17421'
ROLLUP='0xa113e2e9620a3bc088a681ebb2c234fdbeb85e21'
FORK='http://127.0.0.1:18549'
NAME='recovery-paused-watchtower'


def require(ok,msg):
    if not ok:raise RuntimeError(msg)


def run(*args):
    return subprocess.check_output(args,text=True).strip()


def rpc(method,params):
    req=urllib.request.Request(FORK,json.dumps(dict(jsonrpc='2.0',id=1,method=method,params=params)).encode(),
                               {'Content-Type':'application/json'})
    with urllib.request.urlopen(req,timeout=30) as r:data=json.load(r)
    require('error' not in data,'Fork RPC error: '+str(data.get('error')))
    return data['result']


def call(signature):
    return rpc('eth_call',[{'to':ROLLUP,'data':run('cast','calldata',signature)},'latest'])


def config_from(source):
    c=json.loads(json.dumps(source))
    info=json.loads(c['chain']['info-json'])
    require(isinstance(info,list) and info,'Invalid chain metadata')
    for x in info:
        x['parent-chain-id']=31337
        x['parent-chain-is-arbitrum']=True
        for key in ['sequencer-url','secondary-forwarding-target','feed-url','secondary-feed-url']:
            x[key]=''
    c['chain']['info-json']=json.dumps(info)
    c['chain'].pop('dev-wallet',None)
    c['parent-chain']={'id':31337,'connection':{'url':FORK}}
    n=c['node']
    n['sequencer']=False
    n['delayed-sequencer']={'enable':False}
    n['batch-poster']={'enable':False}
    n['feed']={'input':{'url':[],'secondary-url':[]},'output':{'enable':False}}
    n['staker']={'enable':True,'strategy':'Watchtower','only-create-wallet-contract':False,
                 'enable-fast-confirmation':False,'start-validation-from-staked':True}
    n.setdefault('parent-chain-reader',{})['enable']=True
    # Replace the diagnostics config: it had validation-sent-limit=0.
    n['block-validator']={'enable':True,'failure-is-fatal':True,
                         'current-module-root':'current','pending-upgrade-module-root':''}
    e=c.setdefault('execution',{})
    e['forwarding-target']='null';e['secondary-forwarding-target']=[]
    e.setdefault('sequencer',{})['enable']=False
    e.setdefault('recording-database',{})['legacy-fee-account-preimages']=True
    c['http']={'addr':'127.0.0.1','port':8249,'vhosts':['localhost','127.0.0.1'],
               'corsdomain':[],'api':['eth','net','web3','arb','arbdebug']}
    c['ws']={'addr':'127.0.0.1','port':8272,'rpcprefix':'/ws'}
    c['pprof']=False;c['metrics']=False
    c.setdefault('file-logging',{})['enable']=False
    c.setdefault('validation',{}).setdefault('wasm',{})['root-path']='/home/user/target/machines'
    return c


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--copy-report',required=True,type=Path)
    a=p.parse_args()
    evidence=json.loads(a.copy_report.read_text())
    require(evidence.get('status')=='copy_complete' and evidence.get('copiedSourceFrozen'), 'Copy not verified')
    require(evidence.get('target')==str(DB),'Wrong copy target')
    require((a.copy_report.parent/'verify.log').read_text()=='','Copy verification had differences')
    require(DB.is_dir() and DB.resolve()==DB,'Missing or redirected target database')
    require(run('findmnt','-n','-o','TARGET','-T',str(DB))=='/data_new','Unexpected target mount')
    ids=run('docker','ps','-aq').split()
    containers=json.loads(run('docker','inspect',*ids)) if ids else []
    require(not any(c['Name'].lstrip('/')==NAME for c in containers),'Test container already exists; inspect it before rerun')
    for c in containers:
        if not c['State']['Running']:continue
        for mount in c.get('Mounts',[]):
            if mount.get('Type')!='bind':continue
            src=Path(mount['Source'])
            require(not (src==DATA or src in DATA.parents or DATA in src.parents),
                    'A running container already uses target config/data: '+c['Name'])
    require(int(rpc('eth_chainId',[]),16)==31337,'Wrong fork chain ID')
    require('anvil' in rpc('web3_clientVersion',[]).lower(),'Not Anvil')
    pinned=rpc('eth_getBlockByNumber',['0x1e58e750',False])
    require(pinned and pinned['hash'].lower()=='0xc4a99db9563e557c6544abe0e4299ae14f8c641a7c96e938178d2308b5c29c72',
            'Wrong historical fork anchor')
    require(int(call('paused()'),16)==1,'Rollup is not paused')
    require(int(call('latestConfirmed()'),16)==42886,'Not expected historical recovery node')
    require(call('wasmModuleRoot()').lower()==ROOT,'Wrong fork WASM root')
    for host,port in [('127.0.0.1',8249),('127.0.0.1',8272),('127.0.0.10',52100),('127.0.0.10',52101)]:
        with socket.socket() as sock:sock.bind((host,port))
    require(run('docker','run','--rm','--network','none','--entrypoint','/bin/cat',IMAGE,
                '/home/user/target/machines/latest/module-root.txt').lower()==ROOT,'Wrong image machine root')
    entry=run('docker','run','--rm','--network','none','--entrypoint','/bin/cat',IMAGE,
              '/usr/local/bin/split-val-entry.sh')
    require(entry.count('52000')==3 and entry.count('52001')==3,'Unexpected entrypoint; review instead of guessing')
    entry=entry.replace('52000','52100').replace('52001','52101')+'\n'
    uid=int(run('docker','run','--rm','--network','none','--entrypoint','/usr/bin/id',IMAGE,'-u'))
    gid=int(run('docker','run','--rm','--network','none','--entrypoint','/usr/bin/id',IMAGE,'-g'))
    source=Path('/data_new/scripts/snapshot-sync.json')
    config=config_from(json.loads(source.read_text()))
    out=Path('/data_new/scripts')/('paused-watchtower-'+datetime.now(timezone.utc).strftime('%Y%m%d-%H%M%S-%f'))
    out.mkdir(mode=0o755)
    for filename,content in [('config.json',json.dumps(config,indent=2)+'\n'),('entry.sh',entry)]:
        path=out/filename
        fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'w') as file:file.write(content)
        os.chown(path,uid,gid)
    # No keys directory, production DB, or Docker socket is mounted.
    cid=run('docker','run','-d','--name',NAME,'--restart=no','--network','host',
            '--log-opt','max-size=100m','--log-opt','max-file=3',
            '-v',str(DATA)+':/home/user/.arbitrum',
            '-v',str(out/'config.json')+':/paused.json:ro',
            '-v',str(out/'entry.sh')+':/paused-entry.sh:ro',
            '--entrypoint','/bin/bash',IMAGE,'/paused-entry.sh','--conf.file','/paused.json')
    report={'container':NAME,'id':cid,'data':str(DATA),'fork':FORK,'rpc':'http://127.0.0.1:8249',
            'validationPorts':[52100,52101],'strategy':'Watchtower','readyForProduction':False,
            'status':'container_launched_validation_not_yet_verified'}
    (out/'launch.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(report,indent=2));print('OUTPUT',out)


if __name__=='__main__':
    main()
