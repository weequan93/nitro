import json,subprocess
from pathlib import Path
BASE=Path(__file__).resolve().parent
CAST=str(Path.home()/'.foundry/bin/cast')
selector=subprocess.check_output([CAST,'sig','spent(uint256)'],text=True).strip()
rows=[]
for start in range(0,32,8):
 body=[dict(jsonrpc='2.0',id=i,method='eth_call',params=[{'to':'0x47da6c41d03ac0608924e86f61577df558114bd8','data':selector+format(i,'064x')},'0x1e5f29da']) for i in range(start,min(start+8,32))]
 result=json.loads(subprocess.check_output(['curl','--fail','-sS','--max-time','40','https://arb1.arbitrum.io/rpc','-H','Content-Type: application/json','--data-binary',json.dumps(body)],text=True))
 assert isinstance(result,list) and len(result)==len(body)
 for row in result: assert 'result' in row, row
 rows.extend(result)
(BASE/'spent-bitmaps.json').write_text(json.dumps(rows,indent=2))
missing=[i for i in range(8121) if not (int(next(x['result'] for x in rows if x['id']==i//255),16)>>(i%255))&1]
print('Unspent indices:',missing)
