from capture import *
V='19-payment-evidence'
with scene(V,'03-trace','Verify proof and the request link','The completed payment is read only and includes a Proof section with a download link. Review its stored reference and settlement status, then open the original request to inspect the approval and payment trail. Request-linked payments cannot be edited or cancelled here, so the confirmation step is the point to correct an amount, reference, or outcome before saving.', 'Lesson P Evidence vendor invoice'):
 require('Proof');call('press','PageDown');click('link','Open the request');call('press','PageDown')
d=plan()
for v in d['videos']:
 if v['id']=='19-payment-evidence':v['features'][0]='Attach payment proof before final confirmation'
save(d)
# Final review of this agent's captures and key visible financial outcomes.
assert len(d['videos'])==6
for v in d['videos']:
 for s in v['scenes']:
  assert (RUN/s['recording']).is_file() and (RUN/s['screenshot']).is_file()
  assert s['verified'] and s['rawDuration']>0
checks={
 '11-full-payment-03-review':['Payment saved. The request is completed.','₹12,000.00'],
 '12-installments-04-trail':['₹7,000.00','₹5,000.00','Completed'],
 '13-deductions-settlement-02-settled':['₹10,800.00','₹1,200.00','Fully settled'],
 '14-manager-shortfall-02-keep':['₹4,000.00','Balance payable'],
 '14-manager-shortfall-03-writeoff':['₹4,000.00','Completed — partial accepted'],
 '14-manager-shortfall-04-concern':['Partial — under discussion'],
 '19-payment-evidence-02-proof':['Proof','/attachments/1','Read-only']}
results=[]
for sid,terms in checks.items():
 content=(RUN/'snapshots'/f'{sid}.json').read_text()
 results.append({'scene':sid,'terms':terms,'allFound':all(t in content for t in terms),'missing':[t for t in terms if t not in content]})
(RUN/'capture-qa.json').write_text(json.dumps({'videos':len(d['videos']),'scenes':sum(len(v['scenes']) for v in d['videos']),'checks':results},indent=2));print(json.dumps(results,indent=2))
