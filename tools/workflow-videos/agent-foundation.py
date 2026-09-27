"""Foundation Agent: Records Users & Access, Orientation, Vendor Directory, Planning Masters, and Monthly Budget."""
import json, time, pathlib, sys, os

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from capture import *

def done(v, k):
    return any(s['id'] == v + '-' + k for x in plan()['videos'] if x['id'] == v for s in x['scenes'])

def choose(name, label):
    snapshot()
    call('run-code', f"async page => {{ await page.getByRole('combobox', {{name:{json.dumps(name)},exact:true}}).selectOption({{label:{json.dumps(label)}}}); }}")

def spin(name, value):
    call('fill', ref('spinbutton', name), str(value))

def rowsave(name):
    snapshot()
    call('run-code', f"async page => {{ await page.getByRole('row').filter({{has:page.getByRole('textbox',{{name:{json.dumps(name)},exact:true}})}}).getByRole('button',{{name:'Save',exact:true}}).click(); }}")

print("--- Step 1: Video 22 - People and Access ---")
video('22-people-access', 'People and access', 'Administrators',
      ['Create users with single roles', 'Separate manager, accounts and requester roles', 'Enforce unique email and active sign-in status'],
      ['User accounts must be managed intentionally', 'Passwords shown are demonstration credentials', 'Permissions follow assigned roles'])

login()
goto('/users')

if not done('22-people-access', '01-users-overview'):
    with scene('22-people-access', '01-users-overview', 'Review active users and role separation',
               'Administrators manage user accounts and assign roles. Here we view existing team members. Creating separate accounts preserves an audit trail of who requested, approved, and recorded each payment.',
               expected='Users'):
        click('heading', 'Users')

if not done('22-people-access', '02-manager-role'):
    with scene('22-people-access', '02-manager-role', 'Assign a role, then save the account',
               'Choose only the role the person needs. Manager access supports reviewing requests, deciding approvals, and handling shortfalls. Account status is separate from role membership. The Active option controls whether the person can sign in. This is a demo password; in a real installation, use your organisation’s account and password procedures.',
               expected='Mira Shah'):
        click('button', '＋ Add user')
        fill('Email', 'mira@demo.fervid.local')
        fill('Name', 'Mira Shah')
        check('checkbox', 'Manager — Review and decide on payment requests.')
        fill('Password', 'DemoPass2026!')
        click('button', 'Add User')

if not done('22-people-access', '03-accounts-role'):
    with scene('22-people-access', '03-accounts-role', 'Separate payment processing from approval',
               'Next, create Anil in Accounts. This person will claim approved work, record payment details, and manage recovery entries where permitted. Creating a user does not send a payment or create an accounting entry. The role controls available actions; the scope configured for that role controls which records those actions can reach.',
               expected='Anil Rao'):
        click('button', '＋ Add user')
        fill('Email', 'anil@demo.fervid.local')
        fill('Name', 'Anil Rao')
        check('checkbox', 'Accounts — Process approved requests and record payments.')
        fill('Password', 'DemoPass2026!')
        click('button', 'Add User')

if not done('22-people-access', '04-requester-role'):
    with scene('22-people-access', '04-requester-role', 'Give requesters their own identity',
               'Riya will raise and track her own requests. Keeping separate accounts makes the requester, manager, and payment recorder visible in history. Avoid sharing one account across the team: shared credentials make it harder to establish who made a decision. We will use Riya’s account for the submission workflows in this course.',
               expected='Riya Menon'):
        click('button', '＋ Add user')
        fill('Email', 'riya@demo.fervid.local')
        fill('Name', 'Riya Menon')
        check('checkbox', 'Requester — Raise and manage your own payment requests.')
        fill('Password', 'DemoPass2026!')
        click('button', 'Add User')

snapshot('foundation-users-created')

print("--- Step 2: Video 01 - Orientation ---")
v = '01-orientation'
video(v, 'Find your way and choose the right request', 'Everyone',
      ['Role-specific home', 'Action queues', 'Five request types', 'Coming-soon boundaries'],
      ['Menu visibility depends on permissions', 'Recording a payment does not transfer money', 'Coming-soon modules are unavailable'])

goto('/')
if not done(v, '01-home'):
    with scene(v, '01-home', 'Start with the action dashboard',
               'Fervid Budget connects requests, approvals, payment records and budget monitoring. The home screen highlights work waiting on you, open requests and relevant queues. Follow a card to work on its records. This demonstration uses synthetic data, and the administrator sees more navigation than a typical requester.',
               expected='Needs my action'):
        click('link', 'Needs my action', exact=False)
        goto('/')

if not done(v, '02-types'):
    with scene(v, '02-types', 'Choose the business situation',
               'Start a new request by choosing the business situation. Vendor invoices cover an invoice already received. Vendor advances pay a supplier before an invoice exists. Reimbursements repay your own spending. Employee advances fund upcoming organisation expenses. Deposits or guarantees track money expected back. Choosing correctly determines the evidence and follow-up fields.',
               expected='Deposit or guarantee'):
        click('link', '＋ New request')
        snapshot()

if not done(v, '03-form'):
    with scene(v, '03-form', 'Inspect the form before entering data',
               'The invoice route asks for a vendor, an amount and invoice evidence, then identifies the project, expense head and approver. A request asks for authorisation; it does not record that a bank transfer happened. Submit only once the business details and documents are ready. We demonstrate completed submissions in the request lessons.',
               expected='Vendor'):
        click('link', 'Vendor invoice', exact=False)
        snapshot()
        call('press', 'PageDown')

goto('/')
if not done(v, '04-boundaries'):
    with scene(v, '04-boundaries', 'Understand what is available today',
               'Use the side navigation to move between the operational workflow and budget information. The items under Coming soon are placeholders: invoices, payments received, inventory and purchase orders are not working modules here. The vendor invoice request is available separately. Treat the payments ledger as a record of payments, with banking performed outside this application.',
               expected='Coming soon'):
        snapshot()
        call('run-code', "async page => { await page.getByRole('navigation',{name:'Coming soon',exact:true}).scrollIntoViewIfNeeded(); }")

print("--- Step 3: Video 02 - Vendor Directory ---")
video('02-vendor-directory', 'Vendor onboarding and payment defaults', 'Administrators and Accounts',
      ['Create a vendor', 'Validate tax identifiers', 'Save contact and payment preferences'],
      ['Vendor details are manually maintained.', 'Saving bank or UPI details does not verify the beneficiary or initiate a transfer.'])

goto('/vendors/new')
if not done('02-vendor-directory', '01-identity'):
    with scene('02-vendor-directory', '01-identity', 'Identify the vendor',
               'Start with a recognizable vendor name and a short label. The directory keeps contact, tax, and payment preferences together so requesters can reuse them. This example uses a fictional office supplier. The directory is maintained by your team; entering a supplier does not independently verify its identity.'):
        fill('Vendor name', 'Lesson Studio Office Supplies')
        fill('Short name', 'Studio Supplies')
        fill('Categories', 'Office supplies, stationery')
        fill('Contact person', 'Dev Mehta')
        fill('Email', 'dev@example.test')

if not done('02-vendor-directory', '02-payment-defaults'):
    with scene('02-vendor-directory', '02-payment-defaults', 'Capture payment preferences',
               'Add the vendor’s preferred payment route and internal notes before saving. Here we use a demonstration UPI address. These are reference details for the people processing the request. Fervid records the workflow and payment evidence, but saving an address does not validate ownership or send money to that address.'):
        fill('UPI ID', 'studio.demo@upi')
        fill('Internal notes', 'Demonstration vendor. Verify beneficiary details independently before transferring funds.')
        click('button', 'Save vendor')

snapshot('vendor-saved')

print("--- Step 4: Video 03 - Planning Masters (Part 1: Structure) ---")
v = '03-planning-masters'
video(v, 'Build the project and expense-head structure', 'Administrators',
      ['Create and rename a project', 'Create a head with due day', 'Edit head due day', 'Retire and restore a head'],
      ['Each row saves separately', 'Due day indicates expected timing, not an approval gate', 'Retirement preserves history but blocks new use'])

goto('/projects')
if not done(v, '01-project'):
    with scene(v, '01-project', 'Create a planning project',
               'Budgets are organised into projects, then expense heads, then a figure for each month. Create the project first. Here we add Lesson Plan Events with an explicit order. The project starts active, but cannot receive a payment until it has an expense head. Save the form and verify the new editable row.',
               expected='Name for Lesson Plan Events'):
        fill('Project name', 'Lesson Plan Events')
        spin('Order', '4')
        click('button', 'Add Project')

if not done(v, '02-rename'):
    with scene(v, '02-rename', 'Rename the project on its own row',
               'A row has its own Save button. We refine the project name to Lesson Plan Workshops and save that row. Editing a name keeps the same project identity. Existing heads remain attached to it. Sort order controls presentation; it is not a spending limit or an approval priority.',
               expected='Name for Lesson Plan Workshops'):
        fill('Name for Lesson Plan Events', 'Lesson Plan Workshops')
        rowsave('Name for Lesson Plan Events')

goto('/heads')
if not done(v, '03-head'):
    with scene(v, '03-head', 'Add an expense head and due day',
               'Now select the project and add Venue hire as an expense head. The due day is a day of the month, from one to thirty-one, and supports timing indicators in the variance grid. It is not an automatic payment schedule. Budgets will be assigned separately, after this structure is saved.',
               expected='Head name for Lesson Plan Venue hire'):
        choose('Project', 'Lesson Plan Workshops')
        fill('Head name', 'Lesson Plan Venue hire')
        spin('Due day', '15')
        spin('Order', '1')
        click('button', 'Add Head')

if not done(v, '04-edit-due'):
    with scene(v, '04-edit-due', 'Adjust the head without changing its project',
               'Update the expected due day from fifteen to twenty and save this head only. Keep the project association deliberate: moving a head changes how its history is grouped. Due dates help people notice timing, but they do not send money or replace the request and approval workflow.',
               expected='Due day for Lesson Plan Venue hire'):
        spin('Due day for Lesson Plan Venue hire', '20')
        rowsave('Head name for Lesson Plan Venue hire')

print("--- Step 5: Video 04 - Monthly Budget ---")
v4 = '04-monthly-budget'
video(v4, 'Set a monthly budget and recover from invalid entries', 'Administrators and budget reviewers',
      ['Set October head budgets', 'Rejected-field feedback and correction', 'Saved-value persistence', 'Filtered and full-month totals'],
      ['Budgets have no autosave', 'Invalid submitted values are rejected separately', 'Budget overruns are visible, not an automatic payment block', 'Filtered totals are not company-wide totals'])

goto('/budgets?month=2026-10')
if not done(v4, '01-plan'):
    with scene(v4, '01-plan', 'Set amounts for a specific month',
               'The budget screen stores one amount per expense head per month. We open October and allocate fifty thousand rupees to office rent, then thirty thousand to the new workshop venue head. Project totals are the sum of their heads. These are plans, so entering a budget does not record an expense or create a payment.',
               expected='₹30,000.00'):
        fill('Budget for Operations / Office Rent', '50000')
        fill('Budget for Lesson Plan Workshops / Lesson Plan Venue hire', '30000')
        click('button', 'Save Budgets')

if not done(v4, '02-invalid'):
    with scene(v4, '02-invalid', 'See exactly which value was rejected',
               'A typo should be corrected at its source. We deliberately enter a negative venue budget and save. The page identifies the rejected field and retains the invalid text. Other valid submitted values may be saved, so read the saved and rejected counts instead of treating this as an all-or-nothing transaction.',
               expected='cannot be negative'):
        fill('Budget for Lesson Plan Workshops / Lesson Plan Venue hire', '-100')
        click('button', 'Save Budgets')
        call('press', 'Home')

if not done(v4, '03-correct'):
    with scene(v4, '03-correct', 'Correct the figure and verify persistence',
               'Replace the negative entry with thirty thousand rupees and save again. Then reload the same month to verify the stored amount. Typing alone is not autosave. Actual spending remains separate and updates from payment records. A budget figure provides comparison and visibility; it does not by itself prevent an overspend.',
               expected='₹30,000.00'):
        fill('Budget for Lesson Plan Workshops / Lesson Plan Venue hire', '30000')
        click('button', 'Save Budgets')
        goto('/budgets?month=2026-10')

if not done(v4, '04-filter'):
    with scene(v4, '04-filter', 'Separate the visible slice from the company total',
               'Open the variance grid for this month and search for the workshop project. The visible metrics now describe the matching rows, while the scope note states the full month company total. Compare the thirty-thousand venue budget with the eighty-thousand full-month plan. Clearing filters restores the full list. Always confirm scope before interpreting a total.',
               expected='Full month company total'):
        click('link', 'View grid')
        fill('Search', 'Lesson Plan')
        time.sleep(.7)
        click('button', 'Apply')

if not done(v4, '05-clear'):
    with scene(v4, '05-clear', 'Read no-activity and not-paid states correctly',
               'Clear the search to return to the full month. A budgeted head with no payment is Not paid, whereas a head with neither budget nor spending is No activity. Those states mean different things when reviewing readiness to close. Month Close reviews the full month even when a search was applied. Locking is covered separately.',
               expected='No activity'):
        click('link', 'Clear filters')
        call('press', 'PageDown')

print("--- Step 6: Video 03 - Planning Masters (Part 2: Retire and Restore) ---")
v = '03-planning-masters'
goto('/heads')
if not done(v, '05-retire'):
    with scene(v, '05-retire', 'Retire an unused head without deleting history',
               'Retirement is an active-status change, not deletion. We temporarily retire our demonstration venue head and inspect October: its saved budget remains visible for history, but the budget input is disabled. Existing records are retained. Before retiring a real head, check outstanding requests because inactive project or head combinations cannot receive new payment entries.',
               expected='Retired'):
        snapshot()
        call('run-code', "async page => { const row=page.getByRole('row').filter({has:page.getByRole('textbox',{name:'Head name for Lesson Plan Venue hire',exact:true})}); await row.getByRole('checkbox',{name:'Active',exact:true}).uncheck(); await row.getByRole('button',{name:'Save',exact:true}).click(); }")
        goto('/budgets?month=2026-10')
        snapshot()

if not done(v, '06-restore'):
    with scene(v, '06-restore', 'Reactivate the same head',
               'Reactivate the same head on the Heads page and save its row. Returning to October shows that the thirty-thousand budget is still present and editable. This preserves the original identity and history. Create a new head only when you mean a distinct category, rather than duplicating an existing category to work around its retired status.',
               expected='₹30,000.00'):
        goto('/heads')
        snapshot()
        call('run-code', "async page => { const row=page.getByRole('row').filter({has:page.getByRole('textbox',{name:'Head name for Lesson Plan Venue hire',exact:true})}); await row.getByRole('checkbox',{name:'Inactive',exact:true}).check(); await row.getByRole('button',{name:'Save',exact:true}).click(); }")
        goto('/budgets?month=2026-10')
        fill('Budget for Lesson Plan Workshops / Lesson Plan Venue hire', '30000')
        click('button', 'Save Budgets')
        snapshot()

print("Track 1 Foundation recording complete!")
