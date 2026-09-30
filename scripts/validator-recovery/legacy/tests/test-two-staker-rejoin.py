"""Offline guard/ABI tests. No RPC, keys, Docker or node database access."""
import copy
import importlib.util
import unittest
import tempfile
from unittest.mock import patch
from pathlib import Path

spec = importlib.util.spec_from_file_location('two', (Path(__file__).resolve().parent / '../tools/rehearse-two-staker-rejoin.py'))
two = importlib.util.module_from_spec(spec)
spec.loader.exec_module(two)


def fixture():
    summary = dict(status='checkpoint_single_message_replayed_fork_test_still_required',
        parentBlock='0x1e58e750', confirmedNode=42883, bridgeInboxCount=320189,
        checkpoint={k: v for k, v in two.interval.BEFORE.items() if k != 'machineStatus'},
        newWasmRoot=two.interval.ROOT, checkpointMessage=126237291,
        checkpointAheadOfConfirmed=True, checkpointBatchesAvailable=True)
    evidence = dict(status='nitro_restart_rehearsal_passed', gracefulRestartTested=True,
        normalConfirmedNode=42887, numBlocks=64, automaticCreationTested=True,
        automaticConfirmationTested=True, validatedEndpoint=dict(
            GlobalState={k: v for k, v in two.interval.AFTER.items() if k != 'machineStatus'},
            WasmRoots=[two.interval.ROOT]))
    return summary, dict(parentBlockHash=two.ANCHOR), evidence


class Guards(unittest.TestCase):
    def test_fixed_fixture(self):
        two.check_fixture(*fixture())

    def test_wrong_anchor_and_candidate(self):
        for key, value in [('parentBlock', '0x1'), ('confirmedNode', 42885),
                           ('checkpointMessage', 126237292), ('bridgeInboxCount', 320190)]:
            with self.subTest(key=key):
                s, a, e = fixture()
                s[key] = value
                with self.assertRaises(ValueError):
                    two.check_fixture(s, a, e)

    def test_prior_evidence_not_sufficient(self):
        for key, value in [('status', 'stopped'), ('gracefulRestartTested', False),
                           ('numBlocks', 1), ('automaticConfirmationTested', False)]:
            with self.subTest(key=key):
                s, a, e = fixture()
                e[key] = value
                with self.assertRaises(ValueError):
                    two.check_fixture(s, a, e)

    def test_endpoint_and_root_must_match(self):
        for field in ['GlobalState', 'WasmRoots']:
            s, a, e = fixture()
            e['validatedEndpoint'][field] = None
            with self.assertRaises(ValueError):
                two.check_fixture(s, a, e)

    def test_membership_checks_both_accounts(self):
        state = dict(isStaked=True, latestStakedNode=42887, amountStakedWei='1000000000000',
            currentChallenge=0, isZombie=False, whitelisted=True, withdrawableWei='0')
        members = {addr: copy.deepcopy(state) for addr in two.STAKERS}
        stakes = {addr: 10**12 for addr in two.STAKERS}
        two.check_joined(members, stakes)
        for addr in two.STAKERS:
            for field, value in [('isStaked', False), ('latestStakedNode', 158),
                                 ('amountStakedWei', '0'), ('currentChallenge', 1),
                                 ('isZombie', True), ('whitelisted', False), ('withdrawableWei', '1')]:
                with self.subTest(addr=addr, field=field):
                    bad = copy.deepcopy(members)
                    bad[addr][field] = value
                    with self.assertRaises(ValueError):
                        two.check_joined(bad, stakes)

    def test_static_return_abi(self):
        raw = '0x' + ''.join(format(n, '064x') for n in range(12))
        self.assertEqual(int(two.words(raw, 12)[6], 16), 6)
        for bad in [raw[:-1], '0x', '0x' + 'z'*768]:
            with self.assertRaises(ValueError):
                two.words(bad, 12)

    def test_cast_creation_and_join_encoding(self):
        m = two.m
        assertion = '(' + m.state_tuple(two.interval.BEFORE) + ',' + m.state_tuple(two.interval.AFTER) + ',64)'
        h = '0x' + '12'*32
        calldata = m.encode('newStakeOnNewNode(' + m.ASSERTION + ',bytes32,uint256)',
                            assertion, h, 320189)
        args = two.words('0x' + calldata[10:], 13)
        self.assertEqual(int(args[10], 16), 64)
        self.assertEqual(args[11], h)
        self.assertEqual(int(args[12], 16), 320189)
        join = m.encode('newStakeOnExistingNode(uint64,bytes32)', 42887, h)
        args = two.words('0x' + join[10:], 2)
        self.assertEqual(int(args[0], 16), 42887)
        self.assertEqual(args[1], h)

    def test_recovery_hash_matches_normal_first_child_formula(self):
        prev, acc = '0x'+'11'*32, '0x'+'22'*32
        old = two.interval.expected_hash(prev, acc, 64)
        actual = two.recovery_node_hash(two.interval.BEFORE, two.interval.AFTER,
                                        64, False, prev, acc, two.interval.ROOT)
        self.assertEqual(actual, old)
        sibling = two.recovery_node_hash(two.interval.BEFORE, two.interval.AFTER,
                                         64, True, prev, acc, two.interval.ROOT)
        span = two.recovery_node_hash(two.interval.BEFORE, two.interval.AFTER,
                                      7838, False, prev, acc, two.interval.ROOT)
        self.assertEqual(len({old, sibling, span}), 3)

    def test_governance_variant_passes_exact_hash_and_span(self):
        before, after = two.interval.BEFORE, {k: v for k, v in two.interval.AFTER.items() if k != 'machineStatus'}
        parent = ['0x'+'00'*32 for _ in range(12)]
        parent[0] = two.interval.state_hash(before, 320189)
        parent[9] = hex(42884)
        parent[11] = '0x'+'11'*32
        child = list(parent)
        child[11] = '0x'+'22'*32
        captured = []
        with tempfile.TemporaryDirectory() as temp:
            f = object.__new__(two.TwoStakerFork)
            f.output = Path(temp)
            f.governance_span = 7838
            f.candidate_summary = dict(confirmedNode=42883, confirmedInboxMaxCount=320189,
                oldConfirmedState=before, checkpoint=after)
            def node(n):
                if n == 42883:
                    return parent
                if n == 42884:
                    return child
                if n == 42886:
                    row = list(parent)
                    row[11] = f.governance_commitment['expectedNodeHash']
                    return row
                raise AssertionError(n)
            def call(to, sig, *args):
                return {'bridge()': '0x'+'00'*12+'33'*20,
                        'sequencerMessageCount()': hex(320189),
                        'sequencerInboxAccs(uint256)': '0x'+'44'*32,
                        'wasmModuleRoot()': two.interval.ROOT}[sig]
            f.node, f.call = node, call
            f.number = lambda sig: 42886
            original = '(' + two.m.state_tuple(before) + ',' + two.m.state_tuple(after) + ',1)'
            with patch.object(two.safe.SafeFork, 'admin', lambda self, label, sig, *args: captured.append(args)):
                f.admin('baseline', 'forceCreateNode(uint64,uint256,'+two.m.ASSERTION+',bytes32)',
                        42883, 320189, original, two.m.ZERO)
            self.assertTrue(captured[0][2].endswith(',7838)'))
            self.assertNotEqual(captured[0][3], two.m.ZERO)
            self.assertTrue(f.exact_governance_hash_checked)
            self.assertTrue(f.governance_commitment['hasSibling'])
            self.assertEqual(f.governance_commitment['lastHash'], child[11])


if __name__ == '__main__':
    unittest.main()
