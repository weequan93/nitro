from pathlib import Path
import difflib
import subprocess

commit = '16c17ee3a9c43a8d8b1c0c25222ce0a9d4351457'
assert subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip() == commit
out = Path('grant-diagnostic')
out.mkdir(exist_ok=True)
def replace(s, old, new):
    assert s.count(old) == 1, 'Unexpected source: ' + old
    return s.replace(old, new, 1)
def patch(path, edits):
    before = Path(path).read_text()
    after = before
    for old, new in edits:
        after = replace(after, old, new)
    return ''.join(difflib.unified_diff(before.splitlines(True), after.splitlines(True), fromfile='a/'+path, tofile='b/'+path))

baseline = patch('cmd/replay/main.go', [
    ('glogger.Verbosity(log.LevelError)', 'glogger.Verbosity(log.LevelDebug)'),
    ('var newBlock *types.Block', 'var newBlock *types.Block\n\tvar diagnosticReceipts types.Receipts'),
    ('newBlock, _, _, err = arbos.ProduceBlock(', 'newBlock, _, diagnosticReceipts, err = arbos.ProduceBlock('),
    ('newBlockHash := newBlock.Hash()', '''newBlockHash := newBlock.Hash()
    for i, r := range diagnosticReceipts {
        log.Info("Diagnostic receipt", "index", i, "txHash", r.TxHash, "status", r.Status, "gasUsed", r.GasUsed)
    }
    diagnosticJSON, diagnosticErr := json.Marshal(map[string]interface{}{
        "BlockHash": newBlockHash, "StateRoot": newBlock.Root(),
        "ReceiptsRoot": newBlock.ReceiptHash(), "GasUsed": newBlock.GasUsed(),
    })
    if diagnosticErr != nil { panic(diagnosticErr) }
    fmt.Fprintf(os.Stdout, "DIAGNOSTIC_RESULT %s\\n", diagnosticJSON)'''),
])
baseline += patch('wavmio/native.go', [
    ('\t"errors"\n', ''),
    ('errors.New("preimage not found")', 'fmt.Errorf("preimage not found: type=%v hash=%s", ty, hash.Hex())'),
])
(out/'baseline.patch').write_text(baseline)
(out/'duplicate.patch').write_text(patch('../../../arbos/addressMap/addressMap.go', [
    ('if present || err != nil {\n\t\treturn err\n\t}', 'if present || err != nil {\n\t\treturn errors.New("address already present in address map")\n\t}'),
]))
original = subprocess.check_output(['git', 'show', 'HEAD:Dockerfile'], text=True)
(out/'Dockerfile').write_text(original + '''
FROM node-builder AS grant-replay-builder
USER root
WORKDIR /workspace
COPY grant-diagnostic/*.patch /tmp/grant-diagnostic/
RUN git apply /tmp/grant-diagnostic/baseline.patch && go build -o /diagnostic/replay-baseline ./cmd/replay
RUN git apply /tmp/grant-diagnostic/duplicate.patch && go build -o /diagnostic/replay-duplicate-error ./cmd/replay
FROM debian:bookworm-slim AS grant-replay-diagnostic
COPY --from=grant-replay-builder /diagnostic/ /usr/local/bin/
RUN /usr/local/bin/replay-baseline --help && /usr/local/bin/replay-duplicate-error --help
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/replay-baseline"]
''')
Path('.nitro-tag.txt').write_text(commit+'\n')
for name in ('baseline.patch', 'duplicate.patch'):
    subprocess.run(['git', 'apply', '--check', str(out/name)], check=True)
print('Diagnostic build files ready; original source files unchanged.')
