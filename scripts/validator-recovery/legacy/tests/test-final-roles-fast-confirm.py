import copy
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock

spec = importlib.util.spec_from_file_location('final_roles', (Path(__file__).resolve().parent / '../tools/rehearse-final-roles-fast-confirm.py'))
x = importlib.util.module_from_spec(spec)
spec.loader.exec_module(x)


class Tests(unittest.TestCase):
    def members(self):
        members = {a: dict(amount=10**12, node=43009, challenge=0, staked=1,
                          credit=0, zombie=0, whitelisted=1) for a in x.LIVE}
        members[x.RETIRED] = dict(amount=0, node=0, challenge=0, staked=0, credit=0, zombie=0, whitelisted=1)
        return members, {a: 10**12 for a in x.LIVE}

    def test_final_role_sets_are_distinct(self):
        self.assertEqual(set(x.REFUNDS) - set(x.LIVE), {x.RETIRED})
        self.assertEqual(set(x.LIVE) - set(x.REFUNDS), {x.NEW})
        members, stakes = self.members()
        x.check_members(members, stakes)
        for addr, field, bad in ((x.NEW, 'staked', 0), (x.NEW, 'node', 43007),
                                 (x.RETIRED, 'staked', 1), (x.RETIRED, 'credit', 10**12),
                                 (x.LIVE[0], 'challenge', 2)):
            changed = copy.deepcopy(members)
            changed[addr][field] = bad
            with self.subTest(address=addr, field=field), self.assertRaises(ValueError):
                x.check_members(changed, stakes)

    def test_fast_signatures_sorted_and_no_retired_owner(self):
        raw = bytes.fromhex(x.approval_signatures(list(reversed(x.LIVE)))[2:])
        self.assertEqual(len(raw), 3*65)
        actual = ['0x'+raw[i+12:i+32].hex() for i in range(0, len(raw), 65)]
        self.assertEqual(actual, sorted(x.LIVE, key=lambda a: int(a, 16)))
        for i in range(0, len(raw), 65):
            self.assertEqual(raw[i+32:i+64], bytes(32))
            self.assertEqual(raw[i+64], 1)
        for wrong in (x.LIVE[:2], x.REFUNDS, [x.LIVE[0]]*3):
            with self.assertRaises(ValueError): x.approval_signatures(wrong)

    def test_receipt_status_is_not_enough_and_governance_event_not_accepted(self):
        topic, txhash = '0x'+'ab'*32, '0x'+'cd'*32
        good = dict(address=x.FAST, topics=[topic], data=txhash+'00'*32)
        self.assertTrue(x.fast_success(dict(logs=[good]), topic, txhash))
        for changed in (dict(good, address=x.m.SAFE), dict(good, topics=['0x'+'ee'*32]),
                        dict(good, data='0x'+'ee'*64)):
            self.assertFalse(x.fast_success(dict(status='0x1', logs=[changed]), topic, txhash))
        self.assertFalse(x.fast_success(dict(status='0x1', logs=[]), topic, txhash))

    def test_negative_check_does_not_accept_transport_error_or_success(self):
        fake = Mock()
        fake.rpc.side_effect = ValueError('execution reverted: GS020')
        x.expect_revert(fake, 'threshold', {}, 'GS020')
        fake.rpc.side_effect = ValueError('connection refused')
        with self.assertRaises(ValueError): x.expect_revert(fake, 'threshold', {}, 'GS020')
        fake.rpc.side_effect = None
        fake.rpc.return_value = '0x'
        with self.assertRaises(ValueError): x.expect_revert(fake, 'threshold', {}, 'GS020')

    def test_fork_auth_and_no_fast_safe_impersonation(self):
        with tempfile.TemporaryDirectory() as tmp:
            process = Mock()
            process.poll.return_value = None
            fork = x.Fork(18550, process, Path(tmp))
            with self.assertRaises(ValueError): fork.impersonate(x.FAST)
            with self.assertRaises(ValueError): fork.rpc('eth_sendTransaction', [{}])
            with self.assertRaises(ValueError): fork.impersonate(x.NEW)
            fork.authorized = True
            process.poll.return_value = 0
            with self.assertRaises(ValueError): fork.rpc('eth_chainId', [])

    def test_bad_historical_confirmer_stops_before_mutation(self):
        fake = Mock()
        fake.call.return_value = '0x'+'00'*32
        with self.assertRaises(ValueError): x.fast_identity(fake)
        fake.rpc.assert_not_called()

    def test_safe_target_calldata_and_node_hash(self):
        nodehash = '0x'+'aa'*32
        data = x.m.encode('fastConfirmNextNode(bytes32,bytes32,bytes32)',
                         x.j.END['BlockHash'], x.j.END['SendRoot'], nodehash)
        self.assertEqual(x.two.words('0x'+data[10:], 3),
                         [x.j.END['BlockHash'], x.j.END['SendRoot'], nodehash])


if __name__ == '__main__':
    unittest.main()
