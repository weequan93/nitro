import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

BASE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('three_rejoin', BASE/'../tools/rehearse-three-staker-rejoin.py')
x = importlib.util.module_from_spec(spec)
spec.loader.exec_module(x)
S = json.loads((BASE/'../../evidence/reported-results/three-staker-fork-20260928-090354.json').read_text())['report']
P = json.loads((BASE/'../../evidence/reported-results/three-staker-atomic-mock-20260928-133005.json').read_text())['report']


class Tests(unittest.TestCase):
    def test_all_48_records_checked_and_replay_globals_restored(self):
        r=x.r
        manifest=dict(version=1,parentBlock=S['parentBlock'],parentBlockHash=x.w.ANCHOR,
            candidateSha256=r.digest(S),atomicPassSha256=r.digest(P),startingMessage=126693840,
            startingState=S['checkpoint'],endingPosition=[321868,0],wasmModuleRoot=r.ROOT)
        md=r.digest(manifest); chain=md; previous=S['checkpoint']
        with tempfile.TemporaryDirectory() as tmp:
            d=Path(tmp);(d/'messages').mkdir()
            (d/'manifest.json').write_text(json.dumps(manifest))
            for i,msg in enumerate(range(126693841,126693889),1):
                end=(x.END if i==48 else dict(S['checkpoint'],BlockHash='0x'+format(msg,'064x'),PosInBatch=130+i))
                record=dict(message=msg,wasmModuleRoot=r.ROOT,manifestSha256=md,previousRecordSha256=chain,
                    start=previous,end=end,validationResponse=dict(valid=True,globalstate=end),
                    canonicalHeader=dict(number=hex(msg),hash=end['BlockHash'],parentHash=previous['BlockHash'],sendRoot=end['SendRoot']))
                (d/'messages'/f'{msg}.json').write_text(json.dumps(record));chain=r.digest(record);previous=end
            result=dict(status='followup_span_replay_passed',executionReplayed=True,numBlocks=48,
                validatedMessagesTotal=48,startMessage=126693840,endMessage=126693888,
                startState=S['checkpoint'],endState=x.END,wasmModuleRoot=r.ROOT,recordChainSha256=chain)
            (d/'summary.json').write_text(json.dumps(result))
            old=r.START,r.END,r.A,r.B
            with patch.object(x,'END_CHAIN',chain):
                self.assertEqual(x.verify_followup(d,S,P)['recordsChecked'],48)
                self.assertEqual((r.START,r.END,r.A,r.B),old)
                p=d/'messages/126693862.json';bad=json.loads(p.read_text());bad['validationResponse']['valid']=False;p.write_text(json.dumps(bad))
                with self.assertRaises(ValueError):x.verify_followup(d,S,P)
                self.assertEqual((r.START,r.END,r.A,r.B),old)

    def test_third_account_missing_or_on_old_branch_rejected(self):
        stakes={a:10**12 for a in x.STAKERS}
        members={a:dict(amount=10**12,node=43009,challenge=0,staked=1,credit=0,zombie=0,whitelisted=1) for a in x.STAKERS}
        x.check_members(members,stakes)
        members[x.STAKERS[2]]['node']=43007
        with self.assertRaises(ValueError):x.check_members(members,stakes)
        del members[x.STAKERS[2]]
        with self.assertRaises(ValueError):x.check_members(members,stakes)

    def test_normal_calldata_is_48_not_64(self):
        assertion='('+x.m.state_tuple(S['checkpoint'])+','+x.m.state_tuple(x.END)+',48)'
        raw=x.m.encode('newStakeOnNewNode('+x.m.ASSERTION+',bytes32,uint256)',assertion,x.m.ZERO,321868)
        words=x.two.words('0x'+raw[10:],13)
        self.assertEqual(int(words[10],16),48)
        self.assertEqual(int(words[12],16),321868)

if __name__=='__main__':unittest.main()
