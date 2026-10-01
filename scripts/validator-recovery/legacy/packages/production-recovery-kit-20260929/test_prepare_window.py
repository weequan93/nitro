import copy
import importlib.util
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('prep', Path(__file__).with_name('prepare-window.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


def fixture():
    a = {'rollup': m.ROLLUP, 'parentBlock': '0x1234', 'parentBlockHash': '0x'+'11'*32,
         'slots': {k: {'codeHash': v} for k,v in m.CODE_HASHES.items()},
         'calls': {'paused()': '0x0', 'wasmModuleRoot()': m.OLD_ROOT,
                   'bridge()':'0x'+'0'*24+'53a7559d1e57e371f3d1e55fea97e9b6748418a3',
                   'outbox()':'0x'+'0'*24+'47da6c41d03ac0608924e86f61577df558114bd8'},
         'adminContract': {'address':m.EXECUTOR,
             'proxySlots': {'implementation': {'codeHash':'0x0d88feac198ef1b50b99fddf06aa9f6b1050bfe7211d6f04173de9b6d8953bcb'}},
             'calls': {'hasRole:EXECUTOR_ROLE()': {'authority':m.SAFE,'raw':'0x1'}}},
         'nodeNumbers': {'latestConfirmed':43067,'latestNodeCreated':43069},
         'confirmedNode': {'number':43067,'after':{'machineStatus':1}},
         'localChain': {'head':126693840}}
    a['slots']['admin']['address']=m.EXECUTOR
    p = {'rollup':m.ROLLUP,'parentBlock':a['parentBlock'],'parentBlockHash':a['parentBlockHash'],
         'chainId':42161,'status':'inventory_complete','errors':[], 'wasmModuleRoot':m.OLD_ROOT,
         'paused':False,'nodes':{'confirmed':43067,'created':43069},
         'safe': {'address':m.SAFE,'threshold':3,'nonce':10,'owners':sorted(m.OWNERS),
                  'guard':m.ZERO,'modules':[],
                  'codeHash':'0xd7d408ebcd99b2b70be43e20253d6d92a8ea8fab29bd3be7f55b10032331fb4c',
                  'fallbackHandler':'0xfd0732dc9e303f09fcef3a7388ad10a83459ec99'},
         'stakers':[{'address':x,'isStaked':True,'currentChallenge':0,'requiresChallengeReview':False,
                     'whitelisted':True,'isZombie':False,'amountStakedWei':str(10**12),
                     'withdrawableWei':'0'} for x in sorted(m.STAKERS)]}
    return a,p


class PreparationTests(unittest.TestCase):
    def test_normal_and_paused_phases(self):
        a,p=fixture()
        m.check_inventory(a,p,'before')
        with self.assertRaises(m.PreparationError): m.check_inventory(a,p,'paused')
        a['calls']['paused()']='0x1';p['paused']=True
        m.check_inventory(a,p,'paused')

    def test_drift_is_rejected(self):
        def challenge(a,p): p['stakers'][0]['currentChallenge']=1
        def missing(a,p): p['stakers'].pop()
        def owner(a,p): p['safe']['owners'][0]=m.ZERO
        def code(a,p): a['slots']['primaryImplementation']['codeHash']='0x00'
        def network(a,p): p['chainId']=31337
        def anchor(a,p): p['parentBlockHash']='0x00'
        def root(a,p): p['wasmModuleRoot']=m.NEW_ROOT
        def role(a,p): a['adminContract']['calls']['hasRole:EXECUTOR_ROLE()']['raw']='0x0'
        def errors(a,p): p['errors']=[{'check':'owners'}]
        for mutate in (challenge,missing,owner,code,network,anchor,root,role,errors):
            with self.subTest(mutation=mutate.__name__):
                a,p=fixture();mutate(a,p)
                with self.assertRaises(m.PreparationError): m.check_inventory(a,p,'before')

    def test_rpc_mutations_never_reach_transport(self):
        with patch.object(m.urllib.request,'urlopen',side_effect=AssertionError('network reached')):
            for method in ('eth_sendTransaction','eth_sendRawTransaction','anvil_impersonateAccount','evm_mine'):
                with self.assertRaises(m.PreparationError): m.rpc('http://invalid',method,[])

    def test_protected_output_and_existing_report(self):
        for root in ('/data','/data_mock','/data_new/validator'):
            with self.assertRaises(m.PreparationError): m.safe_output(Path(root)/'new-report')
        with tempfile.TemporaryDirectory() as d:
            with self.assertRaises(m.PreparationError): m.safe_output(Path(d))
            m.safe_output(Path(d)/'new-report')

    def test_pause_calldata_decodes_exact_target_and_selector(self):
        inner=m.encode('pause()')
        self.assertEqual(inner,'0x8456cb59')
        b=bytes.fromhex(m.encode('executeCall(address,bytes)',m.ROLLUP,inner)[2:])
        self.assertEqual(b[4:36],bytes.fromhex(m.ROLLUP[2:]).rjust(32,b'\0'))
        self.assertEqual(int.from_bytes(b[36:68],'big'),64)
        self.assertEqual(int.from_bytes(b[68:100],'big'),4)
        self.assertEqual(b[100:104],bytes.fromhex(inner[2:]))
        self.assertEqual(b[104:],b'\0'*28)

if __name__=='__main__':unittest.main()
