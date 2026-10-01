#!/usr/bin/env python3
"""Replay the pinned 10578-message local A-prime to B interval; no transactions.
Reuses the installed resumable engine. Does not prove parent-confirmed A to B.
"""
import hashlib
import importlib.util
from pathlib import Path

ENGINE_SHA = '7e150efdbf6c0b7ebcefc7bb9948c8a1f38896ba6ed6c32ffb3315f08c976028'
AUDIT_SHA = '92bb81aa76f3d4fde87bbc9e2e01f9465e51327f0290fcc81a6c4509da1ed3e8'
engine = (Path(__file__).resolve().parent / 'replay-retained-message-span.py')
if hashlib.sha256(engine.read_bytes()).hexdigest() != ENGINE_SHA:
    raise SystemExit('STOP: replay engine differs; keep existing files and report this error')
spec = importlib.util.spec_from_file_location('three_staker_replay_engine', engine)
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)


def check_audit(audit):
    r.require(r.digest(audit) == AUDIT_SHA, 'Wrong audit: expected three-staker-span-20260928-090354')
    r.START = 126683263
    r.END = 126693840
    r.A = r.state(audit['localStateAtConfirmedPosition'])
    r.B = r.state(audit['checkpoint'])
    r.SEND = r.B['SendRoot']
    r.require(r.END-r.START+1 == audit['positionalMessageSpan'] == 10578,
              'Unexpected interval')


r.check_audit = check_audit
if __name__ == '__main__':
    raise SystemExit(r.main())
