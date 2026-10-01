#!/usr/bin/env python3
"""Owned local Anvil only. Stop recovery before resume; never opens a validator database.
Requires rehearse-safe-recovery.py and its mock-validator-recovery.py dependency.
"""
import argparse
import importlib.util
import json
import shutil
import socket
import subprocess
import time
from pathlib import Path

spec=importlib.util.spec_from_file_location('safe_recovery',(Path(__file__).resolve().parent / 'rehearse-safe-recovery.py'))
safe=importlib.util.module_from_spec(spec)
spec.loader.exec_module(safe)
m=safe.m


class PausedBoundary(Exception):
    pass


class PausedFork(safe.SafeFork):
    def admin(self,label,signature,*args):
        if signature=='resume()':
            m.require(self.number('paused()')==1,'Recovery unexpectedly unpaused')
            raise PausedBoundary()
        return super().admin(label,signature,*args)


def run_paused(f,summary,authority,directory,refunds):
    try:
        safe.original_rehearse(f,summary,authority,directory,refunds)
    except PausedBoundary:
        pass
    else:
        raise ValueError('Expected pre-resume boundary was not reached')
    node=f.number('latestConfirmed()')
    credits={x:str(f.number('withdrawableFunds(address)',x)) for x in refunds}
    for x in refunds:
        m.require(f.number('isStaked(address)',x)==0,'Selected stake not removed')
    # These are eth_call probes, not transactions. The known validator must be
    # rejected by the pause check rather than an unrelated authorization failure.
    blocked=[]
    for signature,args in [('stakeOnExistingNode(uint64,bytes32)',[node,m.ZERO]),
                           ('removeOldZombies(uint256)',[0])]:
        try:
            f.rpc('eth_call',[{'from':refunds[0],'to':m.ROLLUP,
                              'data':m.encode(signature,*args)},'latest'])
        except ValueError as exc:
            m.require('Pausable: paused' in str(exc),'Unexpected pause-probe error: '+str(exc))
            blocked.append(signature)
        else:
            raise ValueError('Pause check did not reject '+signature)
    report=dict(status='paused_checkpoint_ready',readyForProduction=False,
        forkRpc=f.url,forkChainId=31337,parentBlock=summary['parentBlock'],
        checkpoint=summary['checkpoint'],recoveryNode=node,paused=True,
        wasmModuleRoot=f.call(m.ROLLUP,'wasmModuleRoot()'),refundCreditsWei=credits,
        pauseRejections=blocked,safeExecutionPathTested=True,
        safeThresholdNegativeTested=f.threshold_checked,
        resumeExecuted=False,refundWithdrawalExecuted=False,zombieCleanupExecuted=False,
        validatorRestartTested=False,restakingTested=False,productionOwnerSignaturesTested=False,
        limitations='Historical fork only. Synthetic numBlocks=1; no A-to-B execution proof. No Watchtower run or atomic recovery batch in this test. Do not connect the production sync database.')
    return report


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('snapshot',type=Path)
    p.add_argument('--parent-rpc',required=True)
    p.add_argument('--refund-staker',action='append',required=True)
    p.add_argument('--out',type=Path,required=True)
    p.add_argument('--port',type=int,default=0)
    p.add_argument('--keep-alive',action='store_true')
    a=p.parse_args()
    if not 0<=a.port<=65535:p.error('Invalid port')
    for tool in ('cast','anvil'):
        if not shutil.which(tool):p.error(tool+' is required')
    refunds=list(dict.fromkeys(x.lower() for x in a.refund_staker))
    for addr in refunds:
        m.require(len(addr)==42 and addr.startswith('0x'),'Invalid address')
        bytes.fromhex(addr[2:])
    summary=json.loads((a.snapshot/'summary.json').read_text())
    authority=json.loads((a.snapshot/'authority.json').read_text())
    m.require(summary.get('status')=='checkpoint_single_message_replayed_fork_test_still_required',
              'Unexpected snapshot status')
    m.require(summary.get('checkpointAheadOfConfirmed') and summary.get('checkpointBatchesAvailable'),
              'Invalid historical checkpoint positions')
    a.out.mkdir(parents=True,exist_ok=False)
    process=None
    report=dict(status='stopped',readyForProduction=False)
    try:
        with socket.socket() as sock:
            sock.bind(('127.0.0.1',a.port));port=sock.getsockname()[1]
        with (a.out/'anvil.log').open('w') as log:
            process=subprocess.Popen(['anvil','--host','127.0.0.1','--port',str(port),
                '--chain-id','31337','--fork-chain-id','42161','--fork-url',a.parent_rpc,
                '--fork-block-number',str(int(summary['parentBlock'],16)),
                '--no-storage-caching','--accounts','0'],stdout=log,stderr=subprocess.STDOUT)
            f=PausedFork(port,process,a.out)
            for _ in range(120):
                m.require(process.poll() is None,'Owned Anvil failed; inspect anvil.log')
                try:f.rpc('web3_clientVersion',[]);break
                except (OSError,ValueError):time.sleep(.25)
            else:raise TimeoutError('Anvil startup timeout')
            report=run_paused(f,summary,authority,a.snapshot,refunds)
            report['forkRunning']=True
            (a.out/'summary.json').write_text(json.dumps(report,indent=2)+'\n')
            print(json.dumps(report,indent=2),flush=True)
            if a.keep_alive:
                print('PAUSED_FORK_READY '+f.url,flush=True)
                print('Keep this terminal open. Ctrl-C stops fork; it NEVER resumes the Rollup.',flush=True)
                while process.poll() is None:time.sleep(1)
                raise RuntimeError('Owned Anvil exited unexpectedly')
    except KeyboardInterrupt:
        print('Stopping local fork; no resume is sent.',flush=True)
    except Exception as exc:
        report.update(status='stopped',error=str(exc))
    finally:
        if process is not None and process.poll() is None:
            process.terminate()
            try:process.wait(timeout=10)
            except subprocess.TimeoutExpired:process.kill();process.wait()
        report['forkRunning']=False
        (a.out/'summary.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(report,indent=2));print('OUTPUT',a.out)
    return 0 if report['status']=='paused_checkpoint_ready' else 1


if __name__=='__main__':
    raise SystemExit(main())
