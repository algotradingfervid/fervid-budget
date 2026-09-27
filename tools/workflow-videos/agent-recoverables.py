"""Recoverables Agent: Records Advances & Deposits, Recovery Returns, and Recovery Reconciliation."""
import json, time, pathlib, sys, os, sqlite3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from capture import *

def done(v, k):
    return any(s['id'] == v + '-' + k for x in plan()['videos'] if x['id'] == v for s in x['scenes'])

def choose(name, label):
    snapshot()
    call('run-code', f"async page => {{ await page.getByRole('combobox', {{name:{json.dumps(name)},exact:true}}).selectOption({{label:{json.dumps(label)}}}); }}")

def get_request_id(title):
    conn = sqlite3.connect(ROOT / 'data/fervid.db')
    cur = conn.cursor()
    cur.execute("SELECT id FROM payment_requests WHERE short_title = ? ORDER BY id DESC LIMIT 1", (title,))
    row = cur.fetchone()
    conn.close()
    return row[0] if row else None

print("=== Starting Track 4: Recoverables ===")

# --- Video 15: Advances and Refundable Deposits ---
print("--- Track 4: Video 15 - Advances and Refundable Deposits ---")
V15 = '15-advances-deposits'
video(V15, 'Advances and refundable deposits', 'Requester · Manager · Accounts',
      ['Choose recoverable type', 'Specify expected return date and terms', 'Manager approval and Accounts payout', 'Verify exclusion from budget actuals'],
      ['Payment recording does not transfer money', 'Recoverables do not count as budget expense'])

login('riya@demo.fervid.local', 'DemoPass2026!')

if not done(V15, '01-choose-type'):
    goto('/requests/new?type=recoverable')
    with scene(V15, '01-choose-type', 'Choose the recoverable agreement type',
               'Recoverables track money expected back—such as security deposits or temporary employee travel advances. The request form enforces specific fields including the expected return date, recipient counterparty, and refund conditions before submission.',
               expected='Security deposit'):
        choose('Category', 'Security deposit')
        time.sleep(1)
        fill('Short title', 'Annual training facility security deposit')
        fill('Expected return date', '2026-11-30')
        fill('Paid to', 'City Workshop Center Ltd')
        fill('Counterparty company', 'City Workshop Center Ltd')
        fill('Repayment or refund terms', 'Full refund upon handover inspection at conclusion of lease agreement.')
        fill('Amount', '10000')
        fill('Needed by', '2026-09-30')
        fill('Purpose', 'Refundable security deposit for quarterly workshop classroom space.')
        choose('Approver', 'Mira Shah')

if not done(V15, '02-terms'):
    with scene(V15, '02-terms', 'Specify repayment terms and return date',
               'Every recoverable agreement must have an expected return date. Accounts uses this date to track overdue exposures and follow up on outstanding funds. Submitting without a return date produces an error, protecting the company from untracked cash disbursements.',
               expected='What happens next'):
        click('button', 'Submit request')

deposit_id = get_request_id('Annual training facility security deposit')
deposit_url = f"/requests/{deposit_id}"

if not done(V15, '03-approval-payout'):
    login('mira@demo.fervid.local', 'DemoPass2026!')
    goto(deposit_url)
    click('button', 'Approve ₹10,000.00')
    click('button', 'Approve request')

    login('anil@demo.fervid.local', 'DemoPass2026!')
    goto(deposit_url)
    with scene(V15, '03-approval-payout', 'Manager approval and Accounts payout',
               'Mira reviews the terms and approves the deposit. Anil in Accounts then records the payment. Crucially, recoverable payments are flagged as non-expense disbursements and excluded from budget actuals, preserving accurate operational expense metrics.',
               expected='Completed'):
        click('button', 'Record payment')
        choose('Payment mode', 'NEFT')
        fill('Transaction / UTR reference', 'DEMO-DEP-PAYOUT-10000')
        click('button', 'Payment settled →')
        click('button', 'Confirm and save payment')

# --- Video 16: Recovery Returns ---
print("--- Track 4: Video 16 - Recovery Returns ---")
V16 = '16-recovery-returns'
video(V16, 'Recover an advance in stages', 'Accounts',
      ['Read outstanding balance', 'Partial return', 'Overbalance protection', 'Full recovery'],
      ['Entries are permanent', 'Original payouts remain unchanged'])

# Ensure advance fixture exists and is paid
advance_id = get_request_id('Regional client conference travel advance')
if not advance_id:
    login('riya@demo.fervid.local', 'DemoPass2026!')
    goto('/requests/new?type=employee_advance')
    call('check', ref('radio', 'Refundable or recoverable', exact=False))
    time.sleep(1)
    fill('Short title', 'Regional client conference travel advance')
    fill('Expected return date', '2026-10-15')
    fill('Repayment or refund terms', 'Unspent advance returned within 5 business days of conference conclusion.')
    fill('Amount', '7000')
    fill('Needed by', '2026-09-30')
    fill('What the money is for', 'Travel advance for attending regional operations summit.')
    fill('Purpose', 'Demonstrate validation and complete return history')
    choose('Approver', 'Mira Shah')
    click('button', 'Submit request')
    advance_id = get_request_id('Regional client conference travel advance')

    login('mira@demo.fervid.local', 'DemoPass2026!')
    goto(f"/requests/{advance_id}")
    click('button', 'Approve ₹7,000.00')
    click('button', 'Approve request')

    login('anil@demo.fervid.local', 'DemoPass2026!')
    goto(f"/requests/{advance_id}")
    click('button', 'Record payment')
    choose('Payment mode', 'UPI')
    fill('Transaction / UTR reference', 'DEMO-ADV-PAYOUT-7000')
    click('button', 'Payment settled →')
    click('button', 'Confirm and save payment')

login('anil@demo.fervid.local', 'DemoPass2026!')
goto(f"/recoverables/{advance_id}")
call('run-code', 'async page => { page.on("dialog", d => d.accept()); }')

if not done(V16, '01-balance'):
    with scene(V16, '01-balance', 'Payout completion is not recovery completion',
               'The advance has been paid out, but seven thousand rupees remains outstanding. The recoverable screen separates total paid, recovered, and the balance still due. Payout is historical evidence of money leaving; recovery tracks money returning.',
               expected='Outstanding balance'):
        click('heading', 'Recoverable details')

if not done(V16, '02-partial'):
    with scene(V16, '02-partial', 'Record the first partial cash return',
               'The employee returns two thousand rupees unspent funds. Accounts chooses Money returned, enters the bank reference, and saves. This reduces the outstanding balance to five thousand rupees while preserving the original seven thousand rupee payout history.',
               expected='5,000.00'):
        choose('Recovery type', 'Money returned')
        fill('Amount received or reconciled (₹)', '2000')
        fill('Bank / accounting reference', 'RET-UPI-2000')
        fill('Evidence and explanation', 'First partial return of unspent travel allowance.')
        click('button', 'Record recovery')

if not done(V16, '03-overbalance'):
    with scene(V16, '03-overbalance', 'Reject a recovery above the balance',
               'If an operator accidentally enters six thousand rupees when only five thousand remains, Fervid Budget rejects the transaction. This overbalance protection prevents negative exposure and forces correction before saving.',
               expected='Recovery was not saved'):
        choose('Recovery type', 'Money returned')
        fill('Amount received or reconciled (₹)', '6000')
        fill('Bank / accounting reference', 'RET-ERR-6000')
        fill('Evidence and explanation', 'Accidental over-return entry.')
        click('button', 'Record recovery')

if not done(V16, '04-close'):
    with scene(V16, '04-close', 'Complete recovery and retain full audit history',
               'Accounts corrects the figure to five thousand rupees and confirms. The outstanding balance becomes zero and the record achieves Fully Reconciled status. The entire dated return trail is permanently archived.',
               expected='Fully reconciled'):
        fill('Amount received or reconciled (₹)', '5000')
        fill('Bank / accounting reference', 'RET-FINAL-5000')
        fill('Evidence and explanation', 'Final settlement of remaining travel advance.')
        click('button', 'Record recovery')

# --- Video 17: Recovery Reconciliation ---
print("--- Track 4: Video 17 - Recovery Reconciliation ---")
V17 = '17-recovery-reconciliation'
video(V17, 'Reconcile expenses and authorised adjustments', 'Accounts',
      ['Expense reconciliation', 'Documented adjustment', 'Evidence trail'],
      ['Entries reduce the recoverable only', 'No automatic budget expense or tax posting'])

# Ensure reconciliation advance exists and is paid
recon_id = get_request_id('Field survey equipment setup advance 2')
if not recon_id:
    login('riya@demo.fervid.local', 'DemoPass2026!')
    goto('/requests/new?type=employee_advance')
    call('check', ref('radio', 'Refundable or recoverable', exact=False))
    time.sleep(1)
    fill('Short title', 'Field survey equipment setup advance 2')
    fill('Expected return date', '2026-10-20')
    fill('Repayment or refund terms', 'Reconciled against approved expense bills.')
    fill('Amount', '5000')
    fill('Needed by', '2026-09-30')
    fill('What the money is for', 'Field advance for site measurement tools.')
    fill('Purpose', 'Demonstrate expense reconciliation')
    choose('Approver', 'Mira Shah')
    click('button', 'Submit request')
    recon_id = get_request_id('Field survey equipment setup advance 2')

    login('mira@demo.fervid.local', 'DemoPass2026!')
    goto(f"/requests/{recon_id}")
    click('button', 'Approve ₹5,000.00')
    click('button', 'Approve request')

    login('anil@demo.fervid.local', 'DemoPass2026!')
    goto(f"/requests/{recon_id}")
    click('button', 'Record payment')
    choose('Payment mode', 'UPI')
    fill('Transaction / UTR reference', 'DEMO-RECON-ADV-5000-2')
    click('button', 'Payment settled →')
    click('button', 'Confirm and save payment')

login('anil@demo.fervid.local', 'DemoPass2026!')
goto(f"/recoverables/{recon_id}")
call('run-code', 'async page => { page.on("dialog", d => d.accept()); }')

if not done(V17, '01-distinction'):
    with scene(V17, '01-distinction', 'Choose the right recovery reconciliation route',
               'Not all advances return in cash. Employees frequently reconcile advances by submitting approved expense receipts. Accounts opens the advance to choose between cash return, expense reconciliation, or an authorised deduction.',
               expected='Recoverable details'):
        click('heading', 'Recoverable details')

if not done(V17, '02-expense'):
    with scene(V17, '02-expense', 'Reconcile against approved expense receipts',
               'The employee provides verified bills for three thousand five hundred rupees. Accounts selects Expense reconciled, records the expense voucher reference, and enters the supporting details. This reduces outstanding exposure against verified company spending.',
               expected='1,500.00'):
        choose('Recovery type', 'Expense reconciled')
        fill('Amount received or reconciled (₹)', '3500')
        fill('Bank / accounting reference', 'EXP-BILL-3500')
        fill('Evidence and explanation', 'Hardware bills verified and approved by project manager.')
        click('button', 'Record recovery')

if not done(V17, '03-adjustment'):
    with scene(V17, '03-adjustment', 'Explain who authorised an adjustment',
               'For the remaining one thousand five hundred rupees, management waives wear-and-tear deductions. Accounts records an Authorised adjustment with the manager’s name and approval reference, completely clearing the exposure.',
               expected='Fully reconciled'):
        choose('Recovery type', 'Balance adjustment')
        fill('Amount received or reconciled (₹)', '1500')
        fill('Bank / accounting reference', 'ADJ-AUTH-MIRA-1500')
        fill('Evidence and explanation', 'Authorised equipment adjustment approved by Mira Shah.')
        click('button', 'Record recovery')

print("Track 4 Recoverables recording complete!")
