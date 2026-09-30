#!/usr/bin/env python3
"""Read-only Linux sync diagnosis. Writes reports only; no keys/config dumps."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import time
import urllib.request
from collections import Counter


def read(path):
    try:
        return Path(path).read_text()
    except OSError:
        return ''


def command(args):
    try:
        p = subprocess.run(args, capture_output=True, text=True, timeout=15)
        return p.stdout if p.returncode == 0 else 'UNAVAILABLE: ' + args[0]
    except (OSError, subprocess.TimeoutExpired):
        return 'UNAVAILABLE: ' + args[0]


def head():
    try:
        req = urllib.request.Request('http://127.0.0.1:8349',
            json.dumps(dict(jsonrpc='2.0', id=1, method='eth_blockNumber', params=[])).encode(),
            {'Content-Type': 'application/json'})
        with urllib.request.urlopen(req, timeout=3) as r:
            return int(json.load(r)['result'], 16)
    except Exception:
        return None


def disks():
    return {a[2]: list(map(int, a[3:])) for line in read('/proc/diskstats').splitlines()
            if len(a := line.split()) > 13 and re.fullmatch(r'vd[a-z]+|nvme\d+n\d+', a[2])}


def snapshot(pids):
    procs = {}
    for pid in pids:
        base = Path('/proc') / pid
        waits = Counter()
        for task in base.glob('task/*'):
            state = re.search(r'^State:\s+(.*)', read(task / 'status'), re.M)
            if state and state[1].startswith('D'):
                waits[read(task / 'wchan').strip() or 'unknown'] += 1
        procs[pid] = {'comm': read(base / 'comm').strip(),
                      'io': read(base / 'io'), 'blocked_threads': dict(waits)}
    vm = dict(line.split() for line in read('/proc/vmstat').splitlines())
    return {'time': time.monotonic(), 'head': head(), 'disks': disks(),
            'cpu': list(map(int, read('/proc/stat').splitlines()[0].split()[1:])),
            'swap_in': int(vm.get('pswpin', 0)), 'swap_out': int(vm.get('pswpout', 0)),
            'processes': procs,
            'pressure': {k: read('/proc/pressure/' + k) for k in ('io', 'memory', 'cpu')}}


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--out', required=True)
    a = p.parse_args()
    if not Path('/proc/diskstats').exists():
        p.error('Run on the Linux validator server')
    out = Path(a.out)
    out.mkdir(mode=0o700, parents=True, exist_ok=False)
    inventory, pids, limits = {}, [], {}
    for name in ('snapshot-sync', 'validator-nitro-1'):
        top = command(['docker', 'top', name, '-eo', 'pid,comm'])
        inventory[name] = top
        pids.extend(line.split()[0] for line in top.splitlines()[1:]
                    if line.split() and line.split()[0].isdigit())
        # Deliberately select only resource settings, never environment/arguments.
        fmt = '{{json .HostConfig}}'
        try:
            hc = json.loads(command(['docker', 'inspect', '--format', fmt, name]))
            limits[name] = {k: hc.get(k) for k in ('Memory', 'MemorySwap', 'NanoCpus',
                'CpuQuota', 'CpuPeriod', 'CpusetCpus', 'BlkioWeight',
                'BlkioDeviceReadBps', 'BlkioDeviceWriteBps',
                'BlkioDeviceReadIOps', 'BlkioDeviceWriteIOps')}
        except ValueError:
            limits[name] = {'error': 'inspect unavailable'}
    cg = {}
    for pid in pids:
        for line in read('/proc/' + pid + '/cgroup').splitlines():
            if line.startswith('0::'):
                base = Path('/sys/fs/cgroup') / line[3:].lstrip('/')
                cg[pid] = {k: read(base / k) for k in
                    ('io.max', 'io.stat', 'cpu.max', 'cpu.stat', 'memory.events')}
    initial = {'processes': inventory, 'docker_limits': limits, 'cgroup_v2': cg,
               'memory': read('/proc/meminfo'),
               'mount': command(['findmnt', '-T', '/data_mock', '-o', 'TARGET,SOURCE,FSTYPE,OPTIONS'])}
    (out / 'system.json').write_text(json.dumps(initial, indent=2))
    print('Sampling execution and I/O for about 30 seconds...', flush=True)
    samples = [snapshot(pids)]
    for _ in range(6):
        time.sleep(5)
        samples.append(snapshot(pids))
    intervals = []
    for b, e in zip(samples, samples[1:]):
        dt = e['time'] - b['time']
        row = {'seconds': round(dt, 2), 'disks': {}}
        for name, end in e['disks'].items():
            if name not in b['disks']:
                continue
            d = [x-y for x, y in zip(end, b['disks'][name])]
            row['disks'][name] = {
                'read_iops': round(d[0]/dt, 1), 'write_iops': round(d[4]/dt, 1),
                'read_MiB_s': round(d[2]/2048/dt, 2), 'write_MiB_s': round(d[6]/2048/dt, 2),
                'read_await_ms': round(d[3]/d[0], 2) if d[0] else None,
                'write_await_ms': round(d[7]/d[4], 2) if d[4] else None,
                'inflight_at_end': end[8], 'busy_percent': round(d[9]/dt/10, 1),
                'average_queue': round(d[10]/dt/1000, 2)}
        intervals.append(row)
    start, end = samples[0], samples[-1]
    dt = end['time'] - start['time']
    summary = {'start_head': start['head'], 'end_head': end['head'],
        'sample_seconds': round(dt, 2),
        'blocks_per_second': round((end['head']-start['head'])/dt, 3)
            if start['head'] is not None and end['head'] is not None else None,
        'swap_in_pages': end['swap_in']-start['swap_in'],
        'swap_out_pages': end['swap_out']-start['swap_out'],
        'blocked_thread_samples': [s['processes'] for s in samples],
        'disk_intervals': intervals,
        'note': 'Busy percentage does not prove provisioned IOPS/bandwidth saturation.'}
    # Keep potentially verbose process counters in evidence, not terminal output.
    (out / 'summary.json').write_text(json.dumps(summary, indent=2))
    (out / 'samples.json').write_text(json.dumps(samples, indent=2))
    logs = ''
    # Docker logs may use stderr; collect separately without returning command lines/configs.
    try:
        r = subprocess.run(['docker', 'logs', '--since', '5m', '--tail', '2000', 'snapshot-sync'],
                           capture_output=True, text=True, timeout=15)
        logs = r.stdout + r.stderr
    except (OSError, subprocess.TimeoutExpired):
        pass
    selected = [line for line in logs.splitlines() if any(x in line for x in
                ('created block', 'InboxTracker', 'compaction', 'stall', 'fatal'))]
    selected = [line for line in selected if 'args=' not in line]
    (out / 'progress.log').write_text('\n'.join(selected[-50:]))
    kernel = command(['journalctl', '-k', '--since', '30 minutes ago', '--no-pager', '-n', '500'])
    (out / 'kernel.log').write_text('\n'.join(line for line in kernel.splitlines()
        if re.search(r'vdd|I/O error|timeout|reset|ext4|blocked for|oom', line, re.I)))
    print(json.dumps({k: v for k, v in summary.items()
                      if k not in ('blocked_thread_samples', 'disk_intervals')}, indent=2))
    print('OUTPUT', out)
    print('Share summary.json, system.json, progress.log and kernel.log; no private key is needed.')


if __name__ == '__main__':
    main()
