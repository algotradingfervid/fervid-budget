from capture import *
ids={}
login()
for label in ['Installments','TDS settlement','Keep balance','Write off','Concern']:
 goto('/requests/new');click('link','Reimbursement',exact=False);select('Project','Operations');select('Head','Operations / Office Supplies');fill('Short title','Lesson P '+label);fill('Amount','12000');fill('Expense date','2026-09-25');fill('Purpose','Synthetic '+label+' workflow example for payment training.');select('Approver','Mira Shah');click('button','Submit request');ids[label]=next(x['url'] for x in snapshot() if x.get('name')=='View this request')
(RUN/'fixtures.json').write_text(json.dumps(ids,indent=2));print(ids,flush=True)
login('mira@demo.fervid.local','DemoPass2026!')
for path in ids.values():
 goto(path);click('button','Approve ₹12,000.00');click('button','Approve request')
login('anil@demo.fervid.local','DemoPass2026!');goto(ids['Installments']);click('button','Record payment');fill('Amount actually paid','7000');select('Payment mode','NEFT');fill('Transaction / UTR reference','DEMO-P-INST-7000');click('button','Payment settled →');snapshot('installment-preview')
print([(x.get('role'),x.get('name')) for x in snapshot() if x.get('role') in ['button','textbox','radio']])
