from capture import *
with scene('15-advances-deposits','04-approval','Authorise the amount','The manager sees the same recovery agreement before approval. Approval authorises an amount and sends the request to Accounts; it does not mark cash as recovered. We approve the seven thousand rupees requested. Recovery remains a separate later event, because an employee can receive the advance today and return or reconcile it after the visit.'):
 click('button','Approve ₹7,000.00');click('button','Approve request')
login('anil@demo.fervid.local','DemoPass2026!');goto('/requests/2')
print([(n.get('role'),n.get('name')) for n in snapshot() if n.get('role') in ['button','textbox','link']][-20:])
