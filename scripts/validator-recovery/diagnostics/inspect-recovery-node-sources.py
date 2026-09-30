#!/usr/bin/env python3
"""Read-only local Docker/RPC inventory; excludes commands, environment and wallet config."""
import argparse
import json
import subprocess
import urllib.request
from pathlib import Path


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--out', type=Path, required=True)
    a = p.parse_args()
    if a.out.exists():
        p.error('Output already exists')
    report = {'containers': [], 'rpc': [], 'readyForProduction': False}
    ids = subprocess.check_output(['docker','ps','-aq'], text=True).split()
    if ids:
        items = json.loads(subprocess.check_output(['docker','inspect',*ids],text=True))
        for c in items:
            if not any(x in c['Name'].lower() for x in ('nitro','recovery','validator')):
                continue
            report['containers'].append({
                'name': c['Name'].lstrip('/'), 'imageId': c['Image'],
                'imageTag': c['Config']['Image'], 'running': c['State']['Running'],
                'status': c['State']['Status'], 'network': c['HostConfig']['NetworkMode'],
                'ports': c['NetworkSettings'].get('Ports'),
                'mounts': [{'source': m.get('Source'), 'destination': m['Destination'],
                            'rw':m['RW']} for m in c['Mounts']]})
    for port in (8449,8149,8249):
        result = {'port':port}
        for method in ('web3_clientVersion','eth_chainId','eth_blockNumber'):
            req = urllib.request.Request(f'http://127.0.0.1:{port}',json.dumps(
                dict(jsonrpc='2.0',id=1,method=method,params=[])).encode(),
                {'Content-Type':'application/json'})
            try:
                with urllib.request.urlopen(req,timeout=5) as r:
                    obj=json.load(r)
                value=obj.get('result')
                result[method]=value if 'error' not in obj else {'error':obj['error']}
                if method=='eth_blockNumber' and value:
                    result['headDecimal']=int(value,16)
            except Exception as exc:
                result[method]={'errorType':type(exc).__name__}
                break
        report['rpc'].append(result)
    report['filesystem'] = subprocess.run(['df','-hT','/data','/data_new'],
        capture_output=True,text=True).stdout
    report['note']='A responsive RPC/head does not establish production parent connectivity or database integrity. No node was started or modified.'
    with a.out.open('x') as f:
        json.dump(report,f,indent=2)
    print(json.dumps(report,indent=2))
    print('OUTPUT',a.out)


if __name__=='__main__':
    main()
