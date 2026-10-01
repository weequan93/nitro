#!/usr/bin/env python3
"""Fresh historical Anvil + Nitro MakeNodes with a NEW disposable signing key.
Only stops recovery-paused-watchtower; never opens production keystores.
Requires existing rehearse-safe-recovery.py, mock dependency and
resume-original-staker-current-fork.py alongside this file.
"""
import argparse
import errno
import importlib.util
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import time
from urllib.parse import urlsplit

BASE = Path(__file__).resolve().parent


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, BASE / filename)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


interval = load('recovery_interval', 'resume-original-staker-current-fork.py')
safe, m = interval.safe, interval.m
DATA = Path('/data_new/validator/config')
SCRIPTS = Path('/data_new/scripts')
WATCH = 'recovery-paused-watchtower'
TEST = 'recovery-nitro-autosubmit'
IMAGE = 'sha256:b3571c0d8d4cd4e826a48895ed558927c4b003ea14050f5be3fb5ab09c62148f'
FORK = 'http://127.0.0.1:18550'
NODE = 'http://127.0.0.1:8249'
ANCHOR = '0xc4a99db9563e557c6544abe0e4299ae14f8c641a7c96e938178d2308b5c29c72'


def run(*args, timeout=60):
    p = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
    m.require(p.returncode == 0, 'Local command failed (arguments/output withheld)')
    return p.stdout.strip()


def save(path, obj):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as f:
        f.write(obj if isinstance(obj, str) else json.dumps(obj, indent=2) + '\n')


def node_words(f, n):
    data = f.call(m.ROLLUP, 'getNode(uint64)', n)[2:]
    m.require(len(data) == 768, 'Unexpected Node ABI')
    return ['0x' + data[i:i+64] for i in range(0, len(data), 64)]


def advance(f, goal):
    current = int(f.rpc('eth_call', [{'data':'0x4360005260206000f3'}, 'latest']), 16)
    delta = max(0, goal-current)
    m.require(delta <= 10000, 'Unexpected EVM deadline distance')
    if delta:
        f.rpc('anvil_mine', [hex(delta)])
    m.require(int(f.rpc('eth_call', [{'data':'0x4360005260206000f3'}, 'latest']),16) >= goal,
              'EVM clock did not advance')


def containers():
    ids = run('docker', 'ps', '-aq').split()
    return json.loads(run('docker', 'inspect', *ids)) if ids else []


def overlaps(source):
    p = Path(source).resolve()
    return p == DATA or p in DATA.parents or DATA in p.parents


def check_ports(ports, wait_seconds=30):
    """Ignore harmless TIME_WAIT, but reject a live TCP listener (including wildcard binds)."""
    deadline = time.monotonic() + wait_seconds
    while True:
        sockets = []
        try:
            for host, port in ports:
                s = socket.socket()
                sockets.append(s)
                s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                s.bind((host, port))
                s.listen(1)
            return
        except OSError as exc:
            if exc.errno != errno.EADDRINUSE or time.monotonic() >= deadline:
                exc.checked_endpoint = {'host':host, 'port':port}
                raise
        finally:
            for s in sockets:
                s.close()
        time.sleep(1)


def retry_evidence(path):
    path = path.absolute()
    m.require(path.resolve() == path and path.parent.parent == SCRIPTS,
              'Retry report must be an existing run summary under /data_new/scripts')
    report = json.loads(path.read_text())
    m.require(report.get('status') == 'stopped' and
              report.get('failedStage') in ('prepare_test_container','check_test_ports') and
              report.get('errorType') == 'OSError' and
              report.get('ownedForkStillRunning') is False and not report.get('containerId') and
              report.get('container') == TEST and report.get('fork') == FORK,
              'Only a pre-launch setup OSError with a stopped fork can be retried this way')
    cfg = json.loads((path.parent/'config.json').read_text())
    m.require(cfg['parent-chain'] == {'id':31337,'connection':{'url':FORK}} and
              cfg['node']['staker']['strategy'] == 'MakeNodes' and
              cfg['node']['block-validator']['current-module-root'] == 'current',
              'Prior test config does not match this rehearsal')
    return str(path)


def previous_pass_evidence(path, items):
    path = path.absolute()
    m.require(path.resolve() == path and path.parent.parent == SCRIPTS, 'Previous report must be under /data_new/scripts')
    report = json.loads(path.read_text())
    m.require(report.get('status') == 'nitro_autosubmit_passed' and
              report.get('container') == 'recovery-nitro-autosubmit' and
              report.get('testContainerStopped') is True and report.get('ownedForkStillRunning') is False and
              report.get('normalConfirmedNode') == 42887 and report.get('numBlocks') == 64 and
              report.get('validatedEndpoint',{}).get('GlobalState') ==
                  {k:v for k,v in interval.AFTER.items() if k != 'machineStatus'} and
              report.get('validatedEndpoint',{}).get('WasmRoots') == [interval.ROOT],
              'Previous pass does not match the expected completed rehearsal')
    c = next((x for x in items if x.get('Id') == report.get('containerId')), None)
    m.require(c and not c['State']['Running'] and c['Image'] == IMAGE and
              c['Name'].lstrip('/') == report['container'], 'Previous test container identity/state changed')
    mounts = {x['Destination']: x.get('Source') for x in c.get('Mounts',[])}
    m.require(mounts.get('/home/user/.arbitrum') == str(DATA) and
              mounts.get('/test.json') == str(path.parent/'config.json'), 'Previous test mounts differ')
    return str(path)


def preflight(snapshot, out, retry_report=None, previous_pass=None):
    m.require(DATA.resolve() == DATA and (DATA/'Deriw Chain/nitro').is_dir(), 'Unexpected test DB path')
    m.require(run('findmnt', '-n', '-o', 'TARGET', '-T', str(DATA)) == '/data_new', 'Wrong DB mount')
    m.require(out.parent.resolve() == SCRIPTS and SCRIPTS.resolve() == SCRIPTS, 'Output must be under /data_new/scripts')
    items = containers()
    m.require(not any(c['Name'].lstrip('/') == TEST for c in items), 'Test container already exists; inspect prior report')
    watch = next((c for c in items if c['Name'].lstrip('/') == WATCH), None)
    m.require(watch and watch['Image'] == IMAGE, 'Expected Watchtower with pinned image missing')
    if retry_report is not None:
        retry_evidence(retry_report)
    if previous_pass is not None:
        previous_pass_evidence(previous_pass,items)
    if not watch['State']['Running']:
        m.require(retry_report is not None or previous_pass is not None,
                  'Stopped Watchtower requires verified --retry-report or --previous-pass evidence')
    for c in items:
        if not c['State']['Running']:
            continue
        for mount in c.get('Mounts', []):
            if mount.get('Source') and overlaps(mount['Source']):
                m.require(c['Name'].lstrip('/') == WATCH, 'Another running container uses disposable DB')
    mounts = {x['Destination']: x for x in watch['Mounts']}
    m.require(mounts.get('/home/user/.arbitrum', {}).get('Source') == str(DATA), 'Wrong Watchtower data mount')
    files = []
    for dest in ('/paused.json', '/paused-entry.sh'):
        p = Path(mounts[dest]['Source'])
        m.require(p.resolve() == p and SCRIPTS in p.parents, 'Unexpected Watchtower artifact path')
        files.append(p)
    cfg = json.loads(files[0].read_text())
    m.require(cfg['parent-chain']['id'] == 31337 and
              cfg['parent-chain']['connection']['url'] == 'http://127.0.0.1:18549', 'Wrong Watchtower parent')
    m.require('52100' in files[1].read_text() and '52101' in files[1].read_text(), 'Wrong validation ports')
    if watch['State']['Running']:
        latest = interval.rpc_at(NODE, 'arb_latestValidated', [])
        m.require(latest['GlobalState'] == {k:v for k,v in interval.AFTER.items() if k != 'machineStatus'}
                  and latest['WasmRoots'] == [interval.ROOT], 'Unexpected prior validation endpoint')
        for height, state in ((126237291, interval.BEFORE), (126237355, interval.AFTER)):
            block = interval.rpc_at(NODE, 'eth_getBlockByNumber', [hex(height),False])
            m.require(block and block['hash'].lower() == state['BlockHash'], 'Disposable DB canonical block mismatch')
    else:
        print('Using prior validated endpoint evidence; the runtime endpoint will be rechecked in this run.',flush=True)
    summary = json.loads((snapshot/'summary.json').read_text())
    authority = json.loads((snapshot/'authority.json').read_text())
    m.require(summary['parentBlock'] == '0x1e58e750' and authority['parentBlockHash'].lower() == ANCHOR,
              'Wrong historical candidate')
    m.require(summary['checkpoint'] == {k:v for k,v in interval.BEFORE.items() if k != 'machineStatus'}
              and summary['newWasmRoot'] == interval.ROOT, 'Wrong checkpoint/root')
    check_ports([('127.0.0.1',18550)])
    return cfg, files[1], summary, authority


def runtime_config(cfg, key):
    cfg = json.loads(json.dumps(cfg))
    cfg['parent-chain'] = {'id':31337, 'connection':{'url':FORK}}
    cfg['node']['staker'] = {
        'enable':True, 'strategy':'MakeNodes', 'use-smart-contract-wallet':False,
        'only-create-wallet-contract':False, 'enable-fast-confirmation':False,
        'start-validation-from-staked':True, 'staker-interval':'5s', 'make-assertion-interval':'10s',
        'parent-chain-wallet':{'private-key':key},
    }
    cfg['node']['block-validator'] = {'enable':True, 'failure-is-fatal':True,
        'current-module-root':'current', 'pending-upgrade-module-root':''}
    cfg['node']['sequencer'] = False
    cfg['node']['batch-poster'] = {'enable':False}
    cfg['node']['delayed-sequencer'] = {'enable':False}
    cfg['node']['feed'] = {'input':{'url':[], 'secondary-url':[]}, 'output':{'enable':False}}
    cfg['execution']['sequencer'] = {'enable':False}
    cfg['execution']['forwarding-target'] = 'null'
    cfg['execution']['secondary-forwarding-target'] = []
    cfg['pprof'] = False
    cfg['metrics'] = False
    return cfg


def verify_transactions(f, start, addr, expected, out):
    logs = f.rpc('eth_getLogs', [{'address':m.ROLLUP, 'fromBlock':hex(start), 'toBlock':'latest'}])
    creation_sig = 'newStakeOnNewNode(' + m.ASSERTION + ',bytes32,uint256)'
    create_selector = run('cast','sig',creation_sig).lower()
    confirm_selector = run('cast','sig','confirmNextNode(bytes32,bytes32)').lower()
    evidence = {}
    for h in dict.fromkeys(x['transactionHash'] for x in logs):
        tx = f.rpc('eth_getTransactionByHash',[h])
        if tx['from'].lower() != addr:
            continue
        m.require(tx['to'].lower() == m.ROLLUP and int(tx['chainId'],16) == 31337, 'Wrong signed tx target/chain')
        m.require(int(tx.get('r','0x0'),16) and int(tx.get('s','0x0'),16), 'Missing ECDSA signature')
        receipt = f.rpc('eth_getTransactionReceipt',[h])
        m.require(int(receipt['status'],16) == 1, 'Nitro transaction failed')
        data = tx['input'].lower()
        if data[:10] == create_selector:
            words = [data[i:i+64] for i in range(10,len(data),64)]
            m.require(len(words)==13 and int(words[10],16)==64, 'Unexpected normal assertion message count')
            m.require('0x'+words[11] == expected.lower(), 'Creation expectedNodeHash differs')
            evidence['creation'] = {'transaction':tx,'receipt':receipt}
        elif data[:10] == confirm_selector:
            m.require(data == m.encode('confirmNextNode(bytes32,bytes32)',
                       interval.AFTER['BlockHash'],interval.SENDROOT).lower(), 'Wrong normal confirm endpoint')
            evidence['confirmation'] = {'transaction':tx,'receipt':receipt}
    m.require(set(evidence)=={'creation','confirmation'}, 'Expected Nitro signed creation and confirmation missing')
    save(out/'signed-transactions.json',evidence)
    return {k:v['transaction']['hash'] for k,v in evidence.items()}


def stable_chain_state(f, addr):
    return {
        'created':f.number('latestNodeCreated()'), 'confirmed':f.number('latestConfirmed()'),
        'isStaked':f.number('isStaked(address)',addr),
        'stakedNode':f.number('latestStakedNode(address)',addr),
        'stakerRaw':f.call(m.ROLLUP,'getStaker(address)',addr),
        'confirmedNodeRaw':f.call(m.ROLLUP,'getNode(uint64)',42887),
        'nonce':f.rpc('eth_getTransactionCount',[addr,'latest']),
        'pendingNonce':f.rpc('eth_getTransactionCount',[addr,'pending']),
    }


def check_restart(f, addr, out, timeout):
    baseline=stable_chain_state(f,addr)
    m.require(baseline['created']==42887 and baseline['confirmed']==42887 and
              baseline['isStaked']==1 and baseline['stakedNode']==42887 and
              baseline['nonce']==baseline['pendingNonce'], 'Unsettled state before restart')
    c=json.loads(run('docker','inspect',TEST))[0]
    before_id=c['Id']; before_started=c['State']['StartedAt']
    save(out/'restart-before.json',baseline)
    print('RESTART_CHECK: gracefully stopping only '+TEST,flush=True)
    run('docker','stop','--timeout','-1',TEST,timeout=300)
    m.require(not json.loads(run('docker','inspect',TEST))[0]['State']['Running'],'Test failed to stop')
    check_ports([('127.0.0.1',8249),('127.0.0.1',8272),('127.0.0.10',52100),('127.0.0.10',52101)])
    run('docker','start',TEST)
    deadline=time.monotonic()+timeout
    last_print=0
    while time.monotonic()<deadline:
        c=json.loads(run('docker','inspect',TEST))[0]
        m.require(c['State']['Running'],'Restarted test exited')
        try:
            latest=interval.rpc_at(NODE,'arb_latestValidated',[])
        except (OSError,ValueError):
            latest=None
        if latest and latest.get('GlobalState')=={k:v for k,v in interval.AFTER.items() if k!='machineStatus'} and latest.get('WasmRoots')==[interval.ROOT]:
            break
        if time.monotonic()-last_print>=30:
            print('WAIT restarted Nitro to reopen its DB and recover the validated endpoint.',flush=True)
            last_print=time.monotonic()
        time.sleep(5)
    else:
        raise TimeoutError('Restarted Nitro did not recover the endpoint')
    m.require(c['Id']==before_id and c['State']['StartedAt']!=before_started,'Container restart not observed')
    for height,state in ((126237291,interval.BEFORE),(126237355,interval.AFTER)):
        b=interval.rpc_at(NODE,'eth_getBlockByNumber',[hex(height),False])
        m.require(b and b['hash'].lower()==state['BlockHash'],'Canonical block changed after restart')
    observed=0
    for _ in range(18):
        time.sleep(5)
        c=json.loads(run('docker','inspect',TEST))[0]
        m.require(c['State']['Running'],'Restarted test exited during observation')
        m.require(stable_chain_state(f,addr)==baseline,'Unexpected node/stake/nonce change after restart')
        endpoint=interval.rpc_at(NODE,'arb_latestValidated',[])
        m.require(endpoint==latest,'Validated endpoint changed during fixed-fork observation')
        observed+=5
        if observed%30==0:
            print('RESTART_CHECK stable for '+str(observed)+' seconds; no extra sender nonce or assertion.',flush=True)
    logs=subprocess.run(['docker','logs','--since',c['State']['StartedAt'],TEST],capture_output=True,text=True,timeout=30)
    m.require(logs.returncode==0,'Restart logs unavailable')
    identity_lines=[line for line in (logs.stdout+'\n'+logs.stderr).splitlines() if 'running as validator' in line]
    m.require(any(('txsender='+addr) in line.lower() and
                  ('actingaswallet='+addr) in line.lower()
                  for line in identity_lines),'Restarted signer identity not found in startup logs')
    evidence={'status':'restart_persistence_passed','containerId':before_id,
              'startedAtBefore':before_started,'startedAtAfter':c['State']['StartedAt'],
              'observationSeconds':observed,'sameTemporarySigner':addr,
              'chainStateBefore':baseline,'chainStateAfter':stable_chain_state(f,addr),
              'validatedEndpoint':latest,'noAdditionalSenderNonceObserved':True,
              'scope':'Graceful Nitro restart on the SAME running fixed fork; not Anvil restart, crash recovery, or new-batch liveness.'}
    save(out/'restart-check.json',evidence)
    print('PASS Nitro graceful restart retained validation/stake state without an extra transaction during observation.',flush=True)
    return evidence


def main():
    global TEST
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('snapshot',type=Path)
    p.add_argument('--out',type=Path,required=True)
    p.add_argument('--timeout-seconds',type=int,default=1200)
    p.add_argument('--retry-report',type=Path,help='Prior stopped pre-launch setup summary; never resumes an opened test DB blindly')
    p.add_argument('--previous-pass',type=Path,help='Completed autosubmit report authorizing reuse of its stopped disposable DB')
    p.add_argument('--restart-check',action='store_true',help='Use a separate test container and test graceful restart after automatic confirmation')
    a = p.parse_args()
    if a.restart_check:
        TEST='recovery-nitro-restart-check'
    m.require(not (a.retry_report and a.previous_pass),'Select only one prior evidence report')
    m.require(a.previous_pass is None or a.restart_check,'--previous-pass is for the new restart rehearsal')
    m.require(120 <= a.timeout_seconds <= 3600, 'Timeout outside 120..3600 seconds')
    for tool in ('cast','anvil','docker','findmnt'):
        m.require(shutil.which(tool), tool+' is required')
    upstream = os.environ.get('ARCHIVE_RPC','')
    parts = urlsplit(upstream)
    m.require(parts.scheme in ('http','https') and parts.hostname, 'Set ARCHIVE_RPC in this terminal first')
    out = a.out.absolute()
    cfg, entry, summary, authority = preflight(a.snapshot, out, a.retry_report, a.previous_pass)
    out.mkdir(mode=0o755,exist_ok=False)
    report = {'status':'running', 'readyForProduction':False, 'productionSigningTested':False,
              'originalAccountRuntimeTested':False, 'fork':FORK, 'container':TEST}
    if a.retry_report:
        report['retryOf'] = str(a.retry_report.absolute())
    if a.previous_pass:
        report['previousPass'] = str(a.previous_pass.absolute())
    process = None
    launched = False
    stage = 'prepare'
    try:
        stage = 'start_owned_anvil'
        fd = os.open(out/'anvil.log',os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'w') as log:
            process = subprocess.Popen(['anvil','--host','127.0.0.1','--port','18550',
                '--chain-id','31337','--fork-chain-id','42161','--fork-url',upstream,
                '--fork-block-number',str(int(summary['parentBlock'],16)),
                '--no-storage-caching','--accounts','0'],stdout=log,stderr=subprocess.STDOUT)
        f = safe.SafeFork(18550,process,out)
        for _ in range(120):
            m.require(process.poll() is None, 'Owned Anvil exited')
            try:
                f.rpc('web3_clientVersion',[])
                break
            except (OSError,ValueError):
                time.sleep(.25)
        else:
            raise TimeoutError('Anvil startup timed out')
        stage = 'safe_recovery_on_new_fork'
        safe.original_rehearse(f,summary,authority,a.snapshot,[interval.OLD])
        m.require(f.number('latestConfirmed()')==42886 and f.number('paused()')==0,'Unexpected recovered state')
        stage = 'new_test_signer'
        # This is a fresh disposable key, never an existing production wallet.
        n = 0
        while not 0 < n < 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141:
            n = secrets.randbits(256)
        key = format(n,'064x')
        addr = run('cast','wallet','address','--private-key',key).lower()
        m.require(addr != interval.OLD and len(addr)==42,'Invalid fresh test address')
        report['testAddress'] = addr
        f.admin('authorize disposable Nitro signer','setValidator(address[],bool[])','['+addr+']','[true]')
        f.rpc('anvil_setBalance',[addr,hex(10**20)])
        # Test sender is never impersonated or installed as an unlocked Anvil account.
        m.require(addr not in [x.lower() for x in f.rpc('eth_accounts',[])], 'Test signer unexpectedly unlocked')
        m.require(f.number('isStaked(address)',addr)==0 and f.number('isValidator(address)',addr)==1,'Test signer precondition failed')
        bridge='0x'+f.call(m.ROLLUP,'bridge()')[-40:]
        m.require(int(f.call(bridge,'sequencerMessageCount()'),16)==320189,'Unexpected fixed fork inbox')
        before=node_words(f,42886)
        m.require(before[0].lower()==interval.state_hash(interval.BEFORE,320189).lower(),'Recovery commitment mismatch')
        expected=interval.expected_hash(before[11],f.call(bridge,'sequencerInboxAccs(uint256)',320188),64)
        advance(f,int(before[10],16)+f.number('minimumAssertionPeriod()')+1)
        start=int(f.rpc('eth_blockNumber',[]),16)+1
        stage = 'prepare_test_container'
        owner=entry.stat()
        save(out/'config.json',runtime_config(cfg,key))
        save(out/'entry.sh',entry.read_text())
        for name in ('config.json','entry.sh'):
            os.chown(out/name,owner.st_uid,owner.st_gid)
        # All fork preparation is complete before the sole old test container is stopped.
        print('Stopping only '+WATCH+' to release the disposable database.',flush=True)
        run('docker','stop','--timeout','-1',WATCH,timeout=300)
        for c in containers():
            if c['State']['Running']:
                m.require(not any(x.get('Source') and overlaps(x['Source']) for x in c.get('Mounts',[])),
                          'Disposable DB still used by a running container')
        report['watchtowerStopped'] = True
        stage = 'check_test_ports'
        check_ports([('127.0.0.1',8249),('127.0.0.1',8272),('127.0.0.10',52100),('127.0.0.10',52101)])
        stage = 'launch_test_container'
        cid=run('docker','run','-d','--name',TEST,'--restart=no','--network','host',
            '--log-opt','max-size=100m','--log-opt','max-file=3',
            '-v',str(DATA)+':/home/user/.arbitrum',
            '-v',str(out/'config.json')+':/test.json:ro',
            '-v',str(out/'entry.sh')+':/test-entry.sh:ro',
            '--entrypoint','/bin/bash',IMAGE,'/test-entry.sh','--conf.file','/test.json')
        launched=True
        report['containerId']=cid
        print('NITRO_STARTED '+addr+'; waiting for automatic creation and confirmation.',flush=True)
        stage='wait_for_nitro'
        deadline=time.monotonic()+a.timeout_seconds
        moved=False
        last_print=0
        while time.monotonic()<deadline:
            created=f.number('latestNodeCreated()')
            confirmed=f.number('latestConfirmed()')
            m.require(created in (42886,42887) and confirmed in (42886,42887),'Unexpected node progression')
            if created==42887 and not moved:
                node=node_words(f,42887)
                m.require(node[11].lower()==expected.lower() and int(node[3],16)==42886,'Wrong automatically created node')
                m.require(node[0].lower()==interval.state_hash(interval.AFTER,320189).lower(),'Wrong automatic endpoint')
                m.require(f.number('latestStakedNode(address)',addr)==42887,'Test account did not stake on child')
                print('PASS Nitro automatically created node 42887; advancing fork challenge clock.',flush=True)
                advance(f,max(int(node[4],16),int(node_words(f,42886)[5],16))+1)
                moved=True
            if confirmed==42887:
                break
            state=json.loads(run('docker','inspect',TEST))[0]['State']
            m.require(state['Running'],'Nitro container exited; inspect saved test logs')
            if time.monotonic()-last_print>30:
                print('WAIT created='+str(created)+' confirmed='+str(confirmed),flush=True)
                last_print=time.monotonic()
            time.sleep(5)
        else:
            raise TimeoutError('Nitro did not complete within configured timeout')
        stage='verify_runtime_evidence'
        txs=verify_transactions(f,start,addr,expected,out)
        latest=interval.rpc_at(NODE,'arb_latestValidated',[])
        m.require(latest['GlobalState']=={k:v for k,v in interval.AFTER.items() if k!='machineStatus'}
                  and latest['WasmRoots']==[interval.ROOT],'Runtime validation endpoint mismatch')
        report['runtimeEndpointChecked']=True
        outbox='0x'+f.call(m.ROLLUP,'outbox()')[-40:]
        m.require(outbox=='0x47da6c41d03ac0608924e86f61577df558114bd8','Outbox changed')
        m.require(f.call(outbox,'roots(bytes32)',interval.SENDROOT).lower()==interval.AFTER['BlockHash'],'Outbox endpoint mismatch')
        report.update(status='nitro_autosubmit_passed',normalConfirmedNode=42887,numBlocks=64,
            temporaryKeySigningTested=True,automaticCreationTested=True,automaticConfirmationTested=True,
            testSignerImpersonated=False,transactions=txs,validatedEndpoint=latest,
            limitations='Historical fork only. Synthetic governance jump numBlocks=1; no A-to-B proof. '
            'New test key, not original account. Existing validated DB progress may be reused; '
            'not proof that all 64 messages were freshly replayed in this run. No production action.')
        print('PASS Nitro signed, created and confirmed the ordinary assertion.',flush=True)
        if a.restart_check:
            stage='restart_persistence_check'
            evidence=check_restart(f,addr,out,a.timeout_seconds)
            report.update(status='nitro_restart_rehearsal_passed',gracefulRestartTested=True,
                          restartObservationSeconds=evidence['observationSeconds'],
                          noAdditionalSenderNonceObserved=True)
    except (Exception,KeyboardInterrupt) as exc:
        report.update(status='stopped',failedStage=stage,errorType=type(exc).__name__)
        if isinstance(exc,OSError) and exc.errno is not None:
            report['errorErrno']=exc.errno
            report['errorDescription']=os.strerror(exc.errno)
        if hasattr(exc,'checked_endpoint'):
            report['failedEndpoint']=exc.checked_endpoint
    finally:
        safe_to_stop_fork=True
        if launched:
            try:
                run('docker','stop','--timeout','-1',TEST,timeout=300)
                m.require(not json.loads(run('docker','inspect',TEST))[0]['State']['Running'],'Test still running')
                report['testContainerStopped']=True
            except Exception:
                safe_to_stop_fork=False
                report['testContainerStopped']=False
            try:
                logs=subprocess.run(['docker','logs','--tail','10000',TEST],capture_output=True,text=True,timeout=30)
                save(out/'nitro.log',logs.stdout+'\n'+logs.stderr)
            except Exception:
                report['testLogCollectionFailed']=True
        if process is not None and process.poll() is None and safe_to_stop_fork:
            process.terminate()
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill();process.wait()
        report['ownedForkStillRunning']=process is not None and process.poll() is None
        report['watchtowerAutoRestarted']=False
        save(out/'summary.json',report)
    print(json.dumps(report,indent=2));print('OUTPUT',out)
    return 0 if report['status'] in ('nitro_autosubmit_passed','nitro_restart_rehearsal_passed') else 1


if __name__=='__main__':
    raise SystemExit(main())
