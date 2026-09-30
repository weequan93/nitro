#!/usr/bin/env python3
"""Fixed-candidate local fork withdrawal tests. Never broadcasts to production.
Requires rehearse-safe-recovery.py and its existing mock dependency beside this file.
"""
import importlib.util
import json
from pathlib import Path

BASE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('safe_recovery', BASE/'rehearse-safe-recovery.py')
safe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(safe)
m = safe.m
SAFE_REHEARSE = safe.rehearse
BOX = '0x47da6c41d03ac0608924e86f61577df558114bd8'
CALLER = '0x000000000000000000000000000000000000bEEF'
SAMPLES = [{'index': 8118, 'root': '0x99169e9a23c3976cc37e40a0208db31aac5e7f5fdc0f02f223e8ee22b1a8bbc3', 'proof': ['0x0000000000000000000000000000000000000000000000000000000000000000', '0x47da6011ef7814b756f9023ba374bbe312ac93dd337c27417f7a7a630db8e0bf', '0x6066f869fc670d07561bc654581c8114914a21f40a3e7ae0cf7b75ae54d19a1f', '0x0000000000000000000000000000000000000000000000000000000000000000', '0x60e83cc3c17099de0bc3c88d2d5eaf1a2efd179f4408b77f9d1d844b6f1f8ac7', '0xa29cd4dff0325fdc566c0322bd8f558177f4529d10e925c85a789f55129b4dad', '0x0000000000000000000000000000000000000000000000000000000000000000', '0x12a56d425597d160ac118e8469f7e272fdb2b9f0e7db870495a1990ba1657872', '0x19b8884c9accedb8331ecdd17bacdc640bcdd22dd5ec36622cca32e5ce475beb', '0xa4cf301636b3ad84a497631f930c05fe5450303ae015888f13af5de1f01076e7', '0x5ce16eb9d1bb56d09e8315bea7332dbbc88129a1125a7ac19588c98ab7dcf499', '0xc8eddf92b9d2a843206d857bfe34bd36afc9200e7d2440af46c9845939b57b33', '0xc5abf8a5d944081db897c67aa345a70c591f53b1492564004d4df39852b29765'], 'itemHash': '0xce6da0c931d8c8f2da54547deb23b93bd6d2b1c90c8de025d83a1a64ac3c6bad', 'recipient': '0x42af584a2d7a3db334e894907654c06ad062e724', 'token': '0xfd086bc7cd5c481dcc9c85ebe478a1c0b69fcbb9', 'amount': 10980000, 'calldata': '0x08635a9500000000000000000000000000000000000000000000000000000000000001200000000000000000000000000000000000000000000000000000000000001fb60000000000000000000000006121117fccecdd6dfa7b3230eacd4f53e12905db000000000000000000000000483754470664ea3d99fa79ae803e56ca0afe4827000000000000000000000000000000000000000000000000000000000780346c00000000000000000000000000000000000000000000000000000000018d9050000000000000000000000000000000000000000000000000000000006ab6759e000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000002e0000000000000000000000000000000000000000000000000000000000000000d000000000000000000000000000000000000000000000000000000000000000047da6011ef7814b756f9023ba374bbe312ac93dd337c27417f7a7a630db8e0bf6066f869fc670d07561bc654581c8114914a21f40a3e7ae0cf7b75ae54d19a1f000000000000000000000000000000000000000000000000000000000000000060e83cc3c17099de0bc3c88d2d5eaf1a2efd179f4408b77f9d1d844b6f1f8ac7a29cd4dff0325fdc566c0322bd8f558177f4529d10e925c85a789f55129b4dad000000000000000000000000000000000000000000000000000000000000000012a56d425597d160ac118e8469f7e272fdb2b9f0e7db870495a1990ba165787219b8884c9accedb8331ecdd17bacdc640bcdd22dd5ec36622cca32e5ce475beba4cf301636b3ad84a497631f930c05fe5450303ae015888f13af5de1f01076e75ce16eb9d1bb56d09e8315bea7332dbbc88129a1125a7ac19588c98ab7dcf499c8eddf92b9d2a843206d857bfe34bd36afc9200e7d2440af46c9845939b57b33c5abf8a5d944081db897c67aa345a70c591f53b1492564004d4df39852b2976500000000000000000000000000000000000000000000000000000000000001242e567b36000000000000000000000000fd086bc7cd5c481dcc9c85ebe478a1c0b69fcbb90000000000000000000000008fb358679749fd952ea5f090b0ea3675722b08f500000000000000000000000042af584a2d7a3db334e894907654c06ad062e7240000000000000000000000000000000000000000000000000000000000a78aa000000000000000000000000000000000000000000000000000000000000000a000000000000000000000000000000000000000000000000000000000000000600000000000000000000000000000000000000000000000000000000000001fad0000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000', 'l3Tx': '0x49743dd28aaa6dec4e9b8083b9c70b3e3c5c3b0908ed4adc9cdf913937e881fd'}, {'index': 8119, 'root': '0xac44727106df865ba77137bcea131330935688cf48174d77b2b41a65613b03cf', 'proof': ['0x12cce8c2540c0b50798bca9087141d436315cdb144e6e4d69b121c5b327a0c63', '0x47da6011ef7814b756f9023ba374bbe312ac93dd337c27417f7a7a630db8e0bf', '0x6066f869fc670d07561bc654581c8114914a21f40a3e7ae0cf7b75ae54d19a1f', '0x0000000000000000000000000000000000000000000000000000000000000000', '0x60e83cc3c17099de0bc3c88d2d5eaf1a2efd179f4408b77f9d1d844b6f1f8ac7', '0xa29cd4dff0325fdc566c0322bd8f558177f4529d10e925c85a789f55129b4dad', '0x0000000000000000000000000000000000000000000000000000000000000000', '0x12a56d425597d160ac118e8469f7e272fdb2b9f0e7db870495a1990ba1657872', '0x19b8884c9accedb8331ecdd17bacdc640bcdd22dd5ec36622cca32e5ce475beb', '0xa4cf301636b3ad84a497631f930c05fe5450303ae015888f13af5de1f01076e7', '0x5ce16eb9d1bb56d09e8315bea7332dbbc88129a1125a7ac19588c98ab7dcf499', '0xc8eddf92b9d2a843206d857bfe34bd36afc9200e7d2440af46c9845939b57b33', '0xc5abf8a5d944081db897c67aa345a70c591f53b1492564004d4df39852b29765'], 'itemHash': '0x5758ffae7c67f3a577d2c0db3fe4b57bb2a96d576810264e83ed1c0efa7a0f4c', 'recipient': '0x2a59fde59e038739b7c9191beb70dadd997b8f20', 'token': '0xfd086bc7cd5c481dcc9c85ebe478a1c0b69fcbb9', 'amount': 10960000, 'calldata': '0x08635a9500000000000000000000000000000000000000000000000000000000000001200000000000000000000000000000000000000000000000000000000000001fb70000000000000000000000006121117fccecdd6dfa7b3230eacd4f53e12905db000000000000000000000000483754470664ea3d99fa79ae803e56ca0afe48270000000000000000000000000000000000000000000000000000000007854ad800000000000000000000000000000000000000000000000000000000018dab96000000000000000000000000000000000000000000000000000000006ab7bee1000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000002e0000000000000000000000000000000000000000000000000000000000000000d12cce8c2540c0b50798bca9087141d436315cdb144e6e4d69b121c5b327a0c6347da6011ef7814b756f9023ba374bbe312ac93dd337c27417f7a7a630db8e0bf6066f869fc670d07561bc654581c8114914a21f40a3e7ae0cf7b75ae54d19a1f000000000000000000000000000000000000000000000000000000000000000060e83cc3c17099de0bc3c88d2d5eaf1a2efd179f4408b77f9d1d844b6f1f8ac7a29cd4dff0325fdc566c0322bd8f558177f4529d10e925c85a789f55129b4dad000000000000000000000000000000000000000000000000000000000000000012a56d425597d160ac118e8469f7e272fdb2b9f0e7db870495a1990ba165787219b8884c9accedb8331ecdd17bacdc640bcdd22dd5ec36622cca32e5ce475beba4cf301636b3ad84a497631f930c05fe5450303ae015888f13af5de1f01076e75ce16eb9d1bb56d09e8315bea7332dbbc88129a1125a7ac19588c98ab7dcf499c8eddf92b9d2a843206d857bfe34bd36afc9200e7d2440af46c9845939b57b33c5abf8a5d944081db897c67aa345a70c591f53b1492564004d4df39852b2976500000000000000000000000000000000000000000000000000000000000001242e567b36000000000000000000000000fd086bc7cd5c481dcc9c85ebe478a1c0b69fcbb90000000000000000000000008fb358679749fd952ea5f090b0ea3675722b08f50000000000000000000000002a59fde59e038739b7c9191beb70dadd997b8f200000000000000000000000000000000000000000000000000000000000a73c8000000000000000000000000000000000000000000000000000000000000000a000000000000000000000000000000000000000000000000000000000000000600000000000000000000000000000000000000000000000000000000000001fae0000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000', 'l3Tx': '0xc08b61d23f44eb7a045ffcff6623b935900d9ea7484887393c4209be9236abe7'}]


def test_claims(f, phase):
    results = []
    for item in SAMPLES:
        idx = item['index']
        mapping = f.call(BOX, 'roots(bytes32)', item['root'])
        m.require(int(mapping,16) != 0, 'Historical withdrawal root missing')
        proofarg = '['+','.join(item['proof'])+']'
        computed = f.call(BOX,'calculateMerkleRoot(bytes32[],uint256,bytes32)',proofarg,idx,item['itemHash'])
        m.require(computed.lower()==item['root'].lower(), 'Merkle root mismatch')
        spent = bool(int(f.call(BOX,'isSpent(uint256)',idx),16))
        row = dict(index=idx,root=item['root'],rootMapping=mapping,alreadySpent=spent)
        # Already claimed at the pinned historical state: verify replay is rejected.
        if spent:
            reject_duplicate(f,item)
            row['duplicateRejected'] = True
            results.append(row)
            print('PASS',phase,'already-spent rejection',idx,flush=True)
            continue
        m.require(f.authorized and f.process.poll() is None,'Owned fork not ready')
        snapshot = f.rpc('evm_snapshot',[])
        try:
            f.impersonate(CALLER)
            before = int(f.call(item['token'],'balanceOf(address)',item['recipient']),16)
            f.send(phase+' withdrawal '+str(idx),CALLER,BOX,item['calldata'])
            receipt = f.steps[-1]['receipt']
            after = int(f.call(item['token'],'balanceOf(address)',item['recipient']),16)
            m.require(after-before==item['amount'],'Recipient token balance delta mismatch')
            m.require(int(f.call(BOX,'isSpent(uint256)',idx),16)==1,'Spent flag not set')
            reject_duplicate(f,item)
            row.update(executed=True,balanceIncrease=str(after-before),recipient=item['recipient'],
                       token=item['token'],duplicateRejected=True,receipt=receipt)
        finally:
            m.require(f.process.poll() is None,'Fork exited before test rollback')
            m.require(f.rpc('evm_revert',[snapshot]) is True,'Failed to restore fork after claim')
        m.require(int(f.call(BOX,'isSpent(uint256)',idx),16)==0,'Claim test was not rolled back')
        results.append(row)
        print('PASS',phase,'token payment / spent / duplicate rejection / rollback',idx,flush=True)
    (f.output/(phase+'-withdrawals.json')).write_text(json.dumps(results,indent=2))
    return results


def reject_duplicate(f,item):
    expected = m.encode('AlreadySpent(uint256)',item['index']).lower()
    try:
        f.rpc('eth_call',[{'from':CALLER,'to':BOX,'data':item['calldata']},'latest'])
    except ValueError as exc:
        m.require(expected in str(exc).lower(),'Revert was not the expected AlreadySpent error: '+str(exc))
    else:
        raise ValueError('Duplicate withdrawal unexpectedly succeeded')


def rehearse(f, summary, authority, directory, refunds):
    m.require(f.rpc('eth_chainId',[])==hex(31337),'Not isolated chain')
    m.require('anvil' in f.rpc('web3_clientVersion',[]).lower(),'Not Anvil')
    block=f.rpc('eth_getBlockByNumber',[summary['parentBlock'],False])
    m.require(block['hash'].lower()==authority['parentBlockHash'].lower(),'Wrong pinned fork')
    m.require(summary['checkpointBlock']==126237291,'This test bundle is for candidate 126237291')
    m.require(summary['checkpoint']['SendRoot'].lower()==SAMPLES[1]['root'].lower(),'Wrong candidate root')
    m.require('0x'+f.call(m.ROLLUP,'outbox()')[-40:]==BOX,'Wrong Outbox')
    f.authorized=True
    before=test_claims(f,'before-recovery')
    report=SAFE_REHEARSE(f,summary,authority,directory,refunds)
    after=test_claims(f,'after-recovery')
    for a,b in zip(before,after):
        m.require(a['alreadySpent']==b['alreadySpent'],'Original spent state changed')
        m.require(a.get('executed',False)==b.get('executed',False),'Claim behavior changed')
        if a['root']!=summary['checkpoint']['SendRoot']:
            m.require(a['rootMapping']==b['rootMapping'],'Historical root mapping changed')
    m.require(any(x['index']==8119 and x.get('executed') for x in after),
              '8119 not exercised as unclaimed; inspect pinned snapshot')
    report.update(withdrawalSamples=[x['index'] for x in after],
        withdrawalBeforeAfterExecutionTested=True,withdrawalTestTransactionsReverted=True,
        fullWithdrawalHistoryAudited=False,readyForProduction=False)
    report['limitations'] += ' Withdrawal execution coverage is samples 8118/8119 only; claim test state is reverted.'
    return report


if __name__=='__main__':
    m.LocalFork=safe.SafeFork
    m.rehearse=rehearse
    raise SystemExit(m.main())
