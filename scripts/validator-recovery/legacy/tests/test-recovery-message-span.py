"""Offline boundary/guard checks; no RPC access."""
import importlib.util
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location('audit', (Path(__file__).resolve().parent / '../../diagnostics/audit-recovery-message-span.py'))
audit = importlib.util.module_from_spec(spec)
spec.loader.exec_module(audit)


class SpanTests(unittest.TestCase):
    def test_batch_transition(self):
        def batch(height):
            return 9 if height < 100 else 10 if height < 110 else 11
        self.assertEqual(audit.first_block_in_batch(batch, 50, 120, 10), 100)

    def test_boundary_zero_is_previous_block(self):
        first = audit.first_block_in_batch(lambda h: 9 if h < 100 else 10, 70, 120, 10)
        for pos, expected in [(0, 99), (1, 100), (5, 104)]:
            self.assertEqual(first - 1 + pos, expected)
        b_height, b_message, a_height = 120, 110, 99
        a_message = b_message - (b_height-a_height)
        self.assertEqual(a_message, 89)
        self.assertEqual((b_message+1)-(a_message+1), 21)

    def test_missing_batch_rejected(self):
        with self.assertRaises(ValueError):
            audit.first_block_in_batch(lambda h: 9 if h < 100 else 11, 70, 120, 10)

    def test_bad_brackets_rejected(self):
        for lo, hi, target in [(1, 1, 10), (-1, 20, 10), (10, 20, 10), (1, 9, 10)]:
            with self.subTest(case=(lo, hi, target)), self.assertRaises(ValueError):
                audit.first_block_in_batch(lambda h: h, lo, hi, target)

    def test_rpc_mutation_rejected_before_network(self):
        for method in ['eth_sendTransaction', 'anvil_mine', 'evm_snapshot', 'personal_unlockAccount']:
            with self.assertRaises(ValueError):
                audit.rpc('http://unused.invalid', method, [])

    def test_global_state_normalization(self):
        self.assertEqual(audit.gs(dict(BlockHash='0xAA', SendRoot='0xBB', Batch='0x10', PosInBatch='2')),
                         dict(BlockHash='0xaa', SendRoot='0xbb', Batch=16, PosInBatch=2))


if __name__ == '__main__':
    unittest.main()
