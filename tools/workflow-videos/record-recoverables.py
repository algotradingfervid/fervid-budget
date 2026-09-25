from capture import *
def fields():
 print([(n.get('role'),n.get('name'),n.get('text')) for n in snapshot() if n.get('role') in ['textbox','combobox','checkbox','button','spinbutton']],flush=True)
video('15-advances-deposits','Advances and refundable deposits','Requester · Manager · Accounts',['Choose recoverable type','Required terms','Approval and payout'],['Payment recording does not transfer money','Recoverables do not count as budget expense'])
login('riya@demo.fervid.local','DemoPass2026!');goto('/requests/new')
with scene('15-advances-deposits','01-choose','Choose who receives the advance','The request type determines who receives the money and what information is required. An employee advance goes to the requester for upcoming organisation spending. A vendor advance goes to a supplier before an invoice exists. A deposit or guarantee goes to a counterparty and is always recoverable. Here we follow a seven thousand rupee employee advance.'):
 click('link','Employee advance',exact=False)
with scene('15-advances-deposits','02-details','Specify the recovery agreement','Give the advance a recognisable title, an expected return date, and clear repayment terms. These fields make the future recovery actionable. We enter seven thousand rupees for a field visit, with unused funds due back after the trip. The paid to field identifies the requester. An advance is different from a reimbursement, where the employee has already spent the money.'):
 fill('Short title','Lesson E field visit advance');fill('Expected return date','2026-10-15');fill('Repayment or refund terms','Return unused cash after field visit; reconcile supported expenses separately.');fill('Amount','7000');fill('Needed by','2026-09-30');fill('What the money is for','Field visit travel and site materials');fill('Purpose','Demonstrate return of unused advance funds');select('Approver','2')
with scene('15-advances-deposits','03-submit','Submit for a decision','Review the details before submitting. A submitted request is a request for authorisation, not a payment. The approver can inspect the purpose, amount, and recovery terms before deciding. This example starts with the full amount, then follows the separate Accounts payment step. We use synthetic records so no real money is moved.',expected='Lesson E field visit advance'):
 click('button','Submit request')
print(text())
