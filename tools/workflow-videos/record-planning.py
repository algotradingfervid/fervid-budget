import sys,json,time
from capture import *
def choose(name,label):
 snapshot();call('run-code',"async page => { await page.getByRole('combobox', {name:"+json.dumps(name)+",exact:true}).selectOption({label:"+json.dumps(label)+"}); }")
def spin(name,value):call('fill',ref('spinbutton',name),value)
def rowsave(name):
 snapshot();call('run-code',"async page => { await page.getByRole('row').filter({has:page.getByRole('textbox',{name:"+json.dumps(name)+",exact:true})}).getByRole('button',{name:'Save',exact:true}).click(); }")
def done(v,k):return any(s['id']==v+'-'+k for x in plan()['videos'] if x['id']==v for s in x['scenes'])
mode=sys.argv[1]
if mode=='orientation':
 v='01-orientation';video(v,'Find your way and choose the right request','Everyone',['Role-specific home','Action queues','Five request types','Coming-soon boundaries'],['Menu visibility depends on permissions','Recording a payment does not transfer money','Coming-soon modules are unavailable'])
 goto('/')
 with scene(v,'01-home','Start with the action dashboard','Fervid Budget connects requests, approvals, payment records and budget monitoring. The home screen highlights work waiting on you, open requests and relevant queues. Follow a card to work on its records. This demonstration uses synthetic data, and the administrator sees more navigation than a typical requester.',expected='Needs my action'):
  click('link','Needs my action',exact=False);goto('/')
 with scene(v,'02-types','Choose the business situation','Start a new request by choosing the business situation. Vendor invoices cover an invoice already received. Vendor advances pay a supplier before an invoice exists. Reimbursements repay your own spending. Employee advances fund upcoming organisation expenses. Deposits or guarantees track money expected back. Choosing correctly determines the evidence and follow-up fields.',expected='Deposit or guarantee'):
  click('link','＋ New request');snapshot()
 with scene(v,'03-form','Inspect the form before entering data','The invoice route asks for a vendor, an amount and invoice evidence, then identifies the project, expense head and approver. A request asks for authorisation; it does not record that a bank transfer happened. Submit only once the business details and documents are ready. We demonstrate completed submissions in the request lessons.',expected='Vendor'):
  click('link','Vendor invoice',exact=False);snapshot();call('press','PageDown')
 goto('/')
 with scene(v,'04-boundaries','Understand what is available today','Use the side navigation to move between the operational workflow and budget information. The items under Coming soon are placeholders: invoices, payments received, inventory and purchase orders are not working modules here. The vendor invoice request is available separately. Treat the payments ledger as a record of payments, with banking performed outside this application.',expected='Coming soon'):
  snapshot();call('run-code',"async page => { await page.getByRole('navigation',{name:'Coming soon',exact:true}).scrollIntoViewIfNeeded(); }")
if mode=='masters':
 v='03-planning-masters';video(v,'Build the project and expense-head structure','Administrators',['Create and rename a project','Create a head with due day','Edit head due day','Retire and restore a head'],['Each row saves separately','Due day indicates expected timing, not an approval gate','Retirement preserves history but blocks new use'])
 goto('/projects')
 with scene(v,'01-project','Create a planning project','Budgets are organised into projects, then expense heads, then a figure for each month. Create the project first. Here we add Lesson Plan Events with an explicit order. The project starts active, but cannot receive a payment until it has an expense head. Save the form and verify the new editable row.',expected='Name for Lesson Plan Events'):
  fill('Project name','Lesson Plan Events');spin('Order','4');click('button','Add Project')
 with scene(v,'02-rename','Rename the project on its own row','A row has its own Save button. We refine the project name to Lesson Plan Workshops and save that row. Editing a name keeps the same project identity. Existing heads remain attached to it. Sort order controls presentation; it is not a spending limit or an approval priority.',expected='Name for Lesson Plan Workshops'):
  fill('Name for Lesson Plan Events','Lesson Plan Workshops');rowsave('Name for Lesson Plan Events')
 goto('/heads')
 with scene(v,'03-head','Add an expense head and due day','Now select the project and add Venue hire as an expense head. The due day is a day of the month, from one to thirty-one, and supports timing indicators in the variance grid. It is not an automatic payment schedule. Budgets will be assigned separately, after this structure is saved.',expected='Head name for Lesson Plan Venue hire'):
  choose('Project','Lesson Plan Workshops');fill('Head name','Lesson Plan Venue hire');spin('Due day','15');spin('Order','1');click('button','Add Head')
 with scene(v,'04-edit-due','Adjust the head without changing its project','Update the expected due day from fifteen to twenty and save this head only. Keep the project association deliberate: moving a head changes how its history is grouped. Due dates help people notice timing, but they do not send money or replace the request and approval workflow.',expected='Due day for Lesson Plan Venue hire'):
  spin('Due day for Lesson Plan Venue hire','20');rowsave('Head name for Lesson Plan Venue hire')
 print([(n.get('role'),n.get('name')) for n in snapshot() if n.get('role')=='checkbox'])
if mode=='budget':
 v='04-monthly-budget';video(v,'Set a monthly budget and recover from invalid entries','Administrators and budget reviewers',['Set October head budgets','Rejected-field feedback and correction','Saved-value persistence','Filtered and full-month totals'],['Budgets have no autosave','Invalid submitted values are rejected separately','Budget overruns are visible, not an automatic payment block','Filtered totals are not company-wide totals'])
 goto('/budgets?month=2026-10')
 with scene(v,'01-plan','Set amounts for a specific month','The budget screen stores one amount per expense head per month. We open October and allocate fifty thousand rupees to office rent, then twenty-five thousand to the new workshop venue head. Project totals are the sum of their heads. These are plans, so entering a budget does not record an expense or create a payment.',expected='₹25,000.00'):
  fill('Budget for Operations / Office Rent','50000');fill('Budget for Lesson Plan Workshops / Lesson Plan Venue hire','25000');click('button','Save Budgets')
 with scene(v,'02-invalid','See exactly which value was rejected','A typo should be corrected at its source. We deliberately enter a negative venue budget and save. The page identifies the rejected field and retains the invalid text. Other valid submitted values may be saved, so read the saved and rejected counts instead of treating this as an all-or-nothing transaction.',expected='cannot be negative'):
  fill('Budget for Lesson Plan Workshops / Lesson Plan Venue hire','-100');click('button','Save Budgets');call('press','Home')
 with scene(v,'03-correct','Correct the figure and verify persistence','Replace the negative entry with thirty thousand rupees and save again. Then reload the same month to verify the stored amount. Typing alone is not autosave. Actual spending remains separate and updates from payment records. A budget figure provides comparison and visibility; it does not by itself prevent an overspend.',expected='₹30,000.00'):
  fill('Budget for Lesson Plan Workshops / Lesson Plan Venue hire','30000');click('button','Save Budgets');goto('/budgets?month=2026-10')
 with scene(v,'04-filter','Separate the visible slice from the company total','Open the variance grid for this month and search for the workshop project. The visible metrics now describe the matching rows, while the scope note states the full month company total. Compare the thirty-thousand venue budget with the eighty-thousand full-month plan. Clearing filters restores the full list. Always confirm scope before interpreting a total.',expected='Full month company total'):
  click('link','View grid');fill('Search','Lesson Plan');time.sleep(.7);click('button','Apply')
 with scene(v,'05-clear','Read no-activity and not-paid states correctly','Clear the search to return to the full month. A budgeted head with no payment is Not paid, whereas a head with neither budget nor spending is No activity. Those states mean different things when reviewing readiness to close. Month Close reviews the full month even when a search was applied. Locking is covered separately.',expected='No activity'):
  click('link','Clear filters');call('press','PageDown')
if mode=='retire':
 v='03-planning-masters';goto('/heads')
 with scene(v,'05-retire','Retire an unused head without deleting history','Retirement is an active-status change, not deletion. We temporarily retire our demonstration venue head and inspect October: its saved budget remains visible for history, but the budget input is disabled. Existing records are retained. Before retiring a real head, check outstanding requests because inactive project or head combinations cannot receive new payment entries.',expected='Retired'):
  snapshot();call('run-code',"async page => { const row=page.getByRole('row').filter({has:page.getByRole('textbox',{name:'Head name for Lesson Plan Venue hire',exact:true})}); await row.getByRole('checkbox',{name:'Active',exact:true}).uncheck(); await row.getByRole('button',{name:'Save',exact:true}).click(); }")
  goto('/budgets?month=2026-10');snapshot();call('run-code',"async page => { await page.getByRole('textbox',{name:'Budget for Lesson Plan Workshops / Lesson Plan Venue hire',exact:true}).scrollIntoViewIfNeeded(); }")
 with scene(v,'06-restore','Reactivate the same head','Reactivate the same head on the Heads page and save its row. Returning to October shows that the thirty-thousand budget is still present and editable. This preserves the original identity and history. Create a new head only when you mean a distinct category, rather than duplicating an existing category to work around its retired status.',expected='₹30,000.00'):
  goto('/heads');snapshot();call('run-code',"async page => { const row=page.getByRole('row').filter({has:page.getByRole('textbox',{name:'Head name for Lesson Plan Venue hire',exact:true})}); await row.getByRole('checkbox',{name:'Inactive',exact:true}).check(); await row.getByRole('button',{name:'Save',exact:true}).click(); }")
  goto('/budgets?month=2026-10');snapshot();call('run-code',"async page => { await page.getByRole('textbox',{name:'Budget for Lesson Plan Workshops / Lesson Plan Venue hire',exact:true}).scrollIntoViewIfNeeded(); }")
if mode=='vendor-discover':
 goto('/vendors');click('link','Lesson Studio Office Supplies');print([(n.get('role'),n.get('name')) for n in snapshot() if n.get('role') in ['link','heading','button']]);click('link','Requests',exact=True);print(text())
if mode=='restore':
 v='03-planning-masters';goto('/heads')
 with scene(v,'06-restore','Reactivate the same head','Reactivate the same head on the Heads page and save its row. Returning to October shows that the thirty-thousand budget is still present and editable. This preserves the original identity and history. Create a new head only when you mean a distinct category, rather than duplicating an existing category to work around its retired status.',expected='₹30,000.00'):
  snapshot();call('run-code',"async page => { const row=page.getByRole('row').filter({has:page.getByRole('textbox',{name:'Head name for Lesson Plan Venue hire',exact:true})}); await row.getByRole('checkbox',{name:'Inactive',exact:true}).check(); await row.getByRole('button',{name:'Save',exact:true}).click(); }")
  goto('/budgets?month=2026-10');snapshot();call('run-code',"async page => { await page.getByRole('textbox',{name:'Budget for Lesson Plan Workshops / Lesson Plan Venue hire',exact:true}).scrollIntoViewIfNeeded(); }")
if mode=='vendor':
 v='21-vendor-context';video(v,'Trace a vendor across requests, payments and history','Administrators, managers and Accounts',['Search vendor master','Linked requests and return navigation','Linked-payment scope','Vendor change history'],['Related lists obey record permissions','Historical unlinked payments lack verified vendor association','TDS defaults are reference values, not computed tax','Bank data requires its own permission'])
 goto('/vendors')
 with scene(v,'01-find','Find the correct vendor record','Search the vendor master and open Lesson Studio Office Supplies. The record centralises identity, contact information and payment defaults. Restricted bank details require a separate permission. Tax defaults are reference information; the application does not calculate tax. Confirm the correct vendor identity before relying on request or payment history.',expected='Lesson Studio Office Supplies'):
  fill('Search','Lesson Studio');click('button','Apply');click('link','Lesson Studio Office Supplies')
 with scene(v,'02-requests','Follow a linked request and return to context','The Requests tab gathers requests associated with this vendor within your access. Open a request number to inspect its status, business purpose and supporting evidence, then return to the vendor list. A pending request is still awaiting a decision; a request amount is not evidence that the same amount has already been paid.',expected='Only records your permissions allow'):
  click('link','Requests',exact=True);ns=snapshot();target=next(x for x in ns if x.get('role')=='link' and x.get('name','').startswith('PR-'));call('click',target['ref']);call('go-back')
 with scene(v,'03-payment-scope','Know which payments belong in this view','The Payments tab uses verified links through this vendor’s requests. It includes voided linked records so their history remains visible. Older unlinked payments do not have a verified vendor association and are excluded. An empty list means no linked payments within your access, not necessarily that the organisation has never paid this vendor.',expected='Historical unlinked payments'):
  click('link','Payments',exact=True)
 with scene(v,'04-history','Inspect vendor changes with attribution','History records changes to the vendor, with the time and the person responsible. Expand Details to inspect what changed. This is the vendor audit context, not a replacement for the individual request conversation or payment evidence. Access to history and sensitive bank fields is controlled separately from basic vendor viewing.',expected='Change'):
  click('link','History',exact=True);snapshot();click('generic','Details',nth=0)
if mode=='vendor-history':
 v='21-vendor-context';goto('/vendors/1/payments')
 with scene(v,'04-history','Inspect vendor changes with attribution','History records changes to the vendor, with the time and the person responsible. Expand Details to inspect what changed. This is the vendor audit context, not a replacement for the individual request conversation or payment evidence. Access to history and sensitive bank fields is controlled separately from basic vendor viewing.',expected='Change'):
  click('link','History',exact=True);click('generic','Details',nth=0)
if mode=='role-home':
 v='01-orientation';login('riya@demo.fervid.local','DemoPass2026!')
 with scene(v,'05-requester-home','See the requester workspace','Riya signs in with the requester role. Her home and navigation focus on her requests and notifications. The administrator menus seen earlier are absent. Open My requests to follow the status of work she raised. Visibility depends on granted permissions and record scope, so an empty or missing section is not evidence that company-wide data does not exist.',expected='My requests'):
  click('link','▤ My requests');snapshot();goto('/')
 login()
if mode=='vendor-paid':
 v='21-vendor-context';goto('/vendors/1/history')
 with scene(v,'05-linked-payment','Inspect a recorded vendor payment','A linked payment now appears after Accounts records the vendor invoice settlement. Open the payment to inspect the amount, paid date, reference and evidence. In this demonstration the recorded amount is two thousand five hundred rupees. The record documents a payment; it does not itself move money. Review the evidence and the related request before treating it as fully reconciled.',expected='2,500.00'):
  click('link','Payments',exact=True);snapshot();links=[n for n in snapshot() if n.get('role')=='link' and n.get('name','')=='Payment '+sys.argv[2]];assert links,'No linked payment yet';call('click',links[0]['ref']);snapshot();call('press','PageDown')
