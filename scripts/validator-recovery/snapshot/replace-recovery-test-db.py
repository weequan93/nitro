#!/usr/bin/env python3
"""Replace the explicitly authorized disposable DB. Never stops validator-nitro-1.
Default is read-only preflight. --execute stops relevant containers and copies.
"""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
from datetime import datetime, timezone

SOURCE=Path('/data_mock/validator/config/Deriw Chain/nitro')
TARGET=Path('/data_new/validator/config/Deriw Chain/nitro')
ALLOWED={'snapshot-sync','recovery-watchtower','recovery-makenodes','validator-debug-nitro-1'}
PROTECTED='validator-nitro-1'


def require(ok,message):
    if not ok:raise RuntimeError(message)


def output(args):
    return subprocess.check_output(args,text=True).strip()


def running():
    ids=output(['docker','ps','-q']).split()
    return json.loads(output(['docker','inspect',*ids])) if ids else []


def overlaps(a,b):
    return a==b or a in b.parents or b in a.parents


def relevant(containers):
    found=[]
    for c in containers:
        name=c['Name'].lstrip('/')
        for mount in c.get('Mounts',[]):
            if mount.get('Type')!='bind':continue
            path=Path(mount['Source'])
            if overlaps(path,SOURCE) or overlaps(path,TARGET):
                require(name!=PROTECTED,'Protected container shares a copy path; STOP')
                require(name in ALLOWED,'Unexpected container using source/target: '+name)
                # A source reader may be stopped only if it is the sync container.
                if overlaps(path,SOURCE):
                    require(name=='snapshot-sync','Unexpected source consumer: '+name)
                found.append(name)
                break
    return sorted(set(found))


def paths():
    for path,mount in [(SOURCE,'/data_mock'),(TARGET,'/data_new')]:
        require(path.is_dir() and path.resolve()==path,'Missing or redirected path: '+str(path))
        require(output(['findmnt','-n','-o','TARGET','-T',str(path)])==mount,'Unexpected mount for '+str(path))
        require(not output(['find',str(path),'-type','l','-print','-quit']), 'Symlink inside DB; review required')
    require(os.stat(SOURCE).st_dev!=os.stat(TARGET).st_dev,'Source and target are not separate filesystems')
    mounts=json.loads(output(['findmnt','--json','--list','-o','TARGET']))
    for entry in mounts['filesystems']:
        line=entry['target']
        # Protect against crossing nested mount points.
        if line.startswith(str(SOURCE)+'/') or line.startswith(str(TARGET)+'/'):
            raise RuntimeError('Nested DB mount: '+line)
    require((SOURCE/'l2chaindata').is_dir() and (SOURCE/'arbitrumdata').is_dir(),'Unexpected source layout')


def run_logged(args,path):
    with path.open('w') as log:
        proc=subprocess.Popen(args,stdout=log,stderr=subprocess.STDOUT)
        print('Log:',path,flush=True)
        try:
            rc=proc.wait()
        except BaseException:
            if proc.poll() is None:
                proc.terminate()
                try:proc.wait(timeout=30)
                except subprocess.TimeoutExpired:proc.kill();proc.wait()
            raise
    require(rc==0,'Command failed (code '+str(rc)+'); inspect '+str(path))


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--execute',action='store_true')
    p.add_argument('--bwlimit-mib',type=int,default=50,help='Transfer rate limit; default 50 MiB/s')
    a=p.parse_args()
    require(a.bwlimit_mib>0,'Bandwidth limit must be positive')
    require(os.geteuid()==0,'Run as root')
    for tool in ['docker','rsync','findmnt','find']:
        require(shutil.which(tool),'Missing tool: '+tool)
    paths()
    containers=running()
    stop=relevant(containers)
    source_running='snapshot-sync' in stop
    protected=next((c for c in containers if c['Name'].lstrip('/')==PROTECTED),None)
    require(protected is not None,'Expected important validator is not running; investigate first')
    report={'source':str(SOURCE),'target':str(TARGET),'containersToStop':stop,
            'protectedContainer':PROTECTED,'sourceWasRunning':source_running,
            'mode':'execute' if a.execute else 'preflight','readyForProduction':False}
    print(json.dumps(report,indent=2),flush=True)
    if not a.execute:
        print('PREFLIGHT PASSED. No container stopped; no DB changed.')
        return
    out=Path('/data_new/scripts')/('db-replacement-'+datetime.now(timezone.utc).strftime('%Y%m%d-%H%M%S-%f'))
    out.mkdir(parents=True,exist_ok=False)
    (out/'plan.json').write_text(json.dumps(report,indent=2))
    try:
        for name in stop:
            require(name!=PROTECTED and name in ALLOWED,'Invalid stop target')
            subprocess.run(['docker','stop','--timeout','-1',name],check=True)
        require(not relevant(running()),'A DB consumer is still running')
        paths()
        # Recheck the now-stable source. No broad /data_new deletion is used.
        args=['rsync','-a','--one-file-system','--delete-before','--inplace','--whole-file',
              '--bwlimit='+str(a.bwlimit_mib*1024)]
        run_logged([*args,'--dry-run','--itemize-changes','--stats',str(SOURCE)+'/',str(TARGET)+'/'],out/'stopped-dry-run.log')
        run_logged([*args,'--info=progress2','--stats',str(SOURCE)+'/',str(TARGET)+'/'],out/'copy.log')
        require(not relevant(running()),'DB container started during copy; target requires recheck')
        # This verifies metadata/size/file set, not a full 6.8 TB checksum scan.
        run_logged(['rsync','-ani','--one-file-system','--delete',str(SOURCE)+'/',str(TARGET)+'/'],out/'verify.log')
        require(not (out/'verify.log').read_text().strip(),'Post-copy differences remain; target not ready')
        important=next((c for c in running() if c['Name'].lstrip('/')==PROTECTED),None)
        require(important and important['Id']==protected['Id'] and
                important['State']['StartedAt']==protected['State']['StartedAt'],
                'Important container changed during copy; investigate')
        report.update(status='copy_complete',verification='rsync size/mtime/metadata/file-set comparison; not full checksum',
                      copiedSourceFrozen=True,testContainersStarted=False)
        if source_running:
            subprocess.run(['docker','start','snapshot-sync'],check=True)
            require('snapshot-sync' in relevant(running()),'snapshot-sync did not remain running')
            report['syncContainerRestarted']=True
        print('COPY COMPLETE. Target test containers remain stopped.',flush=True)
    except BaseException as exc:
        report.update(status='stopped',error=type(exc).__name__+': '+str(exc),targetUsable=False)
        print('STOP: target must not be started. Inspect logs. No automatic restart on failure.',flush=True)
        raise
    finally:
        (out/'summary.json').write_text(json.dumps(report,indent=2)+'\n')
        print('OUTPUT',out,flush=True)


if __name__=='__main__':
    main()
