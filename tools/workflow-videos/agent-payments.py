"""Payments Agent: Records Accounts Ownership & Holds, Full Payment, Multi-Installments, TDS Deductions, Manager Shortfall, Payment Evidence, and Payments Ledger Voids."""
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

def get_payment_id(ref):
    conn = sqlite3.connect(ROOT / 'data/fervid.db')
    cur = conn.cursor()
    cur.execute("SELECT id FROM payments WHERE reference_no = ? ORDER BY id DESC LIMIT 1", (ref,))
    row = cur.fetchone()
    conn.close()
    return row[0] if row else None

def ensure_fixture(title, amt, req_type='reimbursement', vendor=None, inv_no=None):
    rid = get_request_id(title)
    if rid:
        return f"/requests/{rid}"
    login('riya@demo.fervid.local', 'DemoPass2026!')
    goto(f'/requests/new?type={req_type}')
    fill('Short title', title)
    choose('Project', 'Operations')
    choose('Head', 'Operations / Office Supplies')
    fill('Amount', str(amt))
    fill('Needed by', '2026-09-30')
    if req_type == 'vendor_invoice':
        choose('Vendor', vendor or 'Lesson Studio Office Supplies')
        fill('Invoice number', inv_no or f'INV-{title[:10]}')
        fill('Invoice date', '2026-09-20')
    elif req_type == 'reimbursement':
        fill('Expense date', '2026-09-24')
    fill('Purpose', f'Synthetic workflow request for {title} training.')
    choose('Approver', 'Mira Shah')
    click('button', 'Submit request')
    rid = get_request_id(title)
    # Approve as Mira
    login('mira@demo.fervid.local', 'DemoPass2026!')
    goto(f"/requests/{rid}")
    amt_btn = next(x['name'] for x in snapshot() if x.get('role') == 'button' and 'Approve ₹' in x.get('name', ''))
    click('button', amt_btn)
    click('button', 'Approve request')
    return f"/requests/{rid}"

def ensure_payment_form(req_url):
    rid = req_url.split('/')[-1]
    goto(f"/payments/new?request={rid}")
    snap = snapshot()
    if any(x.get('name') == 'Amount actually paid' for x in snap):
        return
    goto(req_url)
    snap = snapshot()
    if any(x.get('role') == 'button' and x.get('name') == 'Record payment' for x in snap):
        click('button', 'Record payment')

def ensure_settlement_sheet(req_url, amt, mode='NEFT', utr='UTR-2026-TEST'):
    snap = snapshot()
    if any(x.get('role') == 'button' and x.get('name') == 'Confirm and save payment' for x in snap):
        return
    ensure_payment_form(req_url)
    fill('Amount actually paid', str(amt))
    choose('Payment mode', mode)
    fill('Transaction / UTR reference', utr)
    click('button', 'Payment settled →')

print("=== Starting Track 3: Payments & Settlement ===")

# --- Video 10: Accounts Ownership & Holds ---
print("--- Track 3: Video 10 - Accounts Queue, Holds and Ownership ---")
V10 = '10-accounts-ownership'
video(V10, 'Accounts queue, holds and ownership', 'Accounts team',
      ['Review approved requests', 'Explain and release holds', 'Claim and release payment work'],
      ['A hold does not cancel approval or move money.', 'Ownership avoids conflicting payment entry; a manager handles reassignment.'])

hold_req = ensure_fixture('Lesson P Hold and Unhold', '5500')

login('anil@demo.fervid.local', 'DemoPass2026!')
goto('/accounts-queue')

if not done(V10, '01-queue'):
    with scene(V10, '01-queue', 'Find an approved request in the Accounts queue',
               'The Accounts queue is the operational work list for approved requests. Open a request to compare its approved amount, payment status, requester, and supporting history. Approval authorizes the request; Accounts still needs to record what was actually paid.',
               expected='Put on hold'):
        rid = hold_req.split('/')[-1]
        call('run-code', f"async page => {{ await page.locator('a[href=\"/requests/{rid}\"]').first().click(); }}")

goto(hold_req)
snap = snapshot()
if any(x.get('role') == 'button' and x.get('name') == 'Release hold' for x in snap):
    click('button', 'Release hold')

if not done(V10, '02-hold'):
    with scene(V10, '02-hold', 'Explain why payment must wait with Put on hold',
               'Use Put on hold when an approved request cannot proceed yet. A specific reason makes the delay understandable to the requester and other Accounts staff. In this example, the team is waiting for a verified bank reference. This action pauses processing; it does not reject the request or undo the approval.',
               expected='On hold'):
        click('button', 'Put on hold')
        fill('What do you need from the requester', 'Awaiting supplier bank branch IFSC confirmation before initiating NEFT transfer.')
        click('button', 'Put on hold', nth=1)

if not done(V10, '03-unhold'):
    goto(hold_req)
    with scene(V10, '03-unhold', 'Release hold when clarification is received',
               'Once the supplier bank details are verified, Accounts releases the hold with a single click. The request immediately returns to Approved, awaiting payment, ready for processing.',
               expected='Approved'):
        click('button', 'Release hold')

# --- Video 11: Recording Full Payment ---
print("--- Track 3: Video 11 - Recording Full Payment ---")
V11 = '11-full-payment'
video(V11, 'Recording a full payment and settlement', 'Accounts team',
      ['Select payment mode and reference', 'Match approved total', 'Close request as Completed'],
      ['Recording a payment reflects money moved outside the system.', 'Request-linked payments become permanent audit records.'])

full_req = ensure_fixture('Lesson P Full Payment', '15000')

login('anil@demo.fervid.local', 'DemoPass2026!')

if not done(V11, '01-initiate'):
    goto(full_req)
    with scene(V11, '01-initiate', 'Initiate payment recording from approved request',
               'Payment processing begins from the approved request, carrying forward the authorized amount, project, head, and vendor. Accounts clicks Record payment to open the settlement form.',
               expected='Payment settled'):
        click('button', 'Record payment')

if not done(V11, '02-details'):
    ensure_payment_form(full_req)
    with scene(V11, '02-details', 'Enter payment mode and UTR transaction reference',
               'Specify the exact amount paid, payment mode, and bank transaction reference. Using real UTR numbers ensures every ledger entry connects directly to company bank statement lines.',
               expected='Confirm the payment'):
        fill('Amount actually paid', '15000')
        choose('Payment mode', 'NEFT')
        fill('Transaction / UTR reference', 'UTR-2026-NEFT-88910')
        click('button', 'Payment settled →')

if not done(V11, '03-settlement'):
    ensure_settlement_sheet(full_req, '15000', 'NEFT', 'UTR-2026-NEFT-88910')
    with scene(V11, '03-settlement', 'Confirm settlement and close request as Completed',
               'Review the payment summary. Because the paid amount exactly equals the approved figure, the request settles in full. Clicking Confirm and save payment records the transaction in the ledger and marks the request Completed.',
               expected='Completed'):
        click('button', 'Confirm and save payment')

# --- Video 12: Multi-Installment Payments ---
print("--- Track 3: Video 12 - Multi-Installment Payments ---")
V12 = '12-installments'
video(V12, 'Paying requests in multiple installments', 'Accounts team',
      ['Record partial installment payment', 'Select Keep balance payable', 'Track remaining balance', 'Complete with final installment'],
      ['Keep balance keeps the existing manager approval active for future installments.', 'Multiple payments are linked to the same request.'])

inst_req = ensure_fixture('Lesson P Installments', '12000')

login('anil@demo.fervid.local', 'DemoPass2026!')

if not done(V12, '01-first-installment'):
    ensure_payment_form(inst_req)
    with scene(V12, '01-first-installment', 'Record the first payment installment',
               'When paying in stages—such as milestone deliveries—Accounts pays a partial amount. Here, Anil records seven thousand rupees against the twelve thousand approved, leaving five thousand payable.',
               expected='Confirm the payment'):
        fill('Amount actually paid', '7000')
        choose('Payment mode', 'NEFT')
        fill('Transaction / UTR reference', 'UTR-2026-INST-01')
        click('button', 'Payment settled →')

if not done(V12, '02-keep-balance'):
    ensure_settlement_sheet(inst_req, '7000', 'NEFT', 'UTR-2026-INST-01')
    with scene(V12, '02-keep-balance', 'Select Keep balance payable for remaining amount',
               'In the settlement sheet, Accounts chooses Installment to keep the five thousand rupee balance payable under the same approval. Confirming writes the first payment to history while leaving the request open for subsequent disbursements.',
               expected='Approved'):
        call('check', ref('radio', 'Installment — pay the balance later', exact=False))
        click('button', 'Confirm and save payment')

if not done(V12, '03-second-installment'):
    ensure_payment_form(inst_req)
    with scene(V12, '03-second-installment', 'Complete final installment payment',
               'When the project delivers the second milestone, Accounts opens the same request, records the remaining five thousand rupees, and confirms final settlement, bringing total disbursements to twelve thousand rupees and closing the record.',
               expected='Completed'):
        fill('Amount actually paid', '5000')
        choose('Payment mode', 'NEFT')
        fill('Transaction / UTR reference', 'UTR-2026-INST-02')
        click('button', 'Payment settled →')
        click('button', 'Confirm and save payment')

# --- Video 13: TDS Deductions ---
print("--- Track 3: Video 13 - TDS Deductions ---")
V13 = '13-tds-deductions'
video(V13, 'Tax and statutory deductions at settlement', 'Accounts team',
      ['Enter net bank payout amount', 'Document statutory withholding reason', 'Mark obligation Fully settled despite net variance'],
      ['Withholding taxes are recorded as settlement reasons, not separate tax entries.', 'The difference between approved and paid reflects statutory deductions.'])

tds_req = ensure_fixture('Lesson P TDS Settlement', '10000', req_type='vendor_invoice', inv_no='INV-TDS-10000')

login('anil@demo.fervid.local', 'DemoPass2026!')

if not done(V13, '01-enter-net'):
    ensure_payment_form(tds_req)
    with scene(V13, '01-enter-net', 'Enter net bank payout amount',
               'When statutory deductions like TDS apply, the actual bank remittance is less than the approved vendor invoice. Here, against ten thousand rupees approved, Accounts enters the nine thousand rupee net payout.',
               expected='Confirm the payment'):
        fill('Amount actually paid', '9000')
        choose('Payment mode', 'NEFT')
        fill('Transaction / UTR reference', 'UTR-2026-TDS-9000')
        click('button', 'Payment settled →')

if not done(V13, '02-record-tds'):
    ensure_settlement_sheet(tds_req, '9000', 'NEFT', 'UTR-2026-TDS-9000')
    with scene(V13, '02-record-tds', 'Document statutory withholding reason',
               'Even though there is a one thousand rupee difference, the commercial obligation is fully satisfied. Accounts selects Fully settled and documents the ten percent TDS deduction under Section 194C in the explanation note.',
               expected='Fully settled'):
        call('check', ref('radio', 'Fully settled', exact=False))
        fill('Reason for any deduction or shortfall', 'TDS deducted at 10% under Section 194C (₹1,000 withheld). Form 16A certificate to follow.')

if not done(V13, '03-settled'):
    ensure_settlement_sheet(tds_req, '9000', 'NEFT', 'UTR-2026-TDS-9000')
    snap = snapshot()
    if any(x.get('name') == 'Reason for any deduction or shortfall' for x in snap):
        call('check', ref('radio', 'Fully settled', exact=False))
        fill('Reason for any deduction or shortfall', 'TDS deducted at 10% under Section 194C (₹1,000 withheld). Form 16A certificate to follow.')
    with scene(V13, '03-settled', 'Mark obligation Fully settled despite net variance',
               'Confirming saves the payment and closes the request as Completed. The permanent payment record clearly states both the nine thousand rupee cash disbursement and the one thousand rupee tax deduction justification.',
               expected='Completed'):
        click('button', 'Confirm and save payment')

# --- Video 14: Manager Shortfall Review ---
print("--- Track 3: Video 14 - Manager Shortfall Review ---")
V14 = '14-manager-shortfall'
video(V14, 'Manager review on underpayment shortfall', 'Accounts and Managers',
      ['Refer short payment to approving manager', 'Manager accepts shortfall and writes off balance', 'Manager disputes unauthorized deduction'],
      ['Shortfalls require manager concurrence before balance write-off.', 'Disputed shortfalls pause further processing until reconciled.'])

shortfall_accept_req = ensure_fixture('Lesson P Shortfall Accept', '9000')

login('anil@demo.fervid.local', 'DemoPass2026!')

if not done(V14, '01-refer-shortfall'):
    ensure_payment_form(shortfall_accept_req)
    with scene(V14, '01-refer-shortfall', 'Refer short payment to approving manager',
               'When payment falls short due to commercial disputes or supply penalties, Accounts cannot close the balance unilaterally. Accounts chooses "Ask manager to accept a shortfall", explains the penalty, and routes the balance decision to the manager.',
               expected='Waiting on Mira Shah'):
        fill('Amount actually paid', '6000')
        choose('Payment mode', 'NEFT')
        fill('Transaction / UTR reference', 'UTR-2026-SHORT-6000')
        click('button', 'Payment settled →')
        call('check', ref('radio', 'Ask manager to accept a shortfall', exact=False))
        fill('Reason for any deduction or shortfall', 'Vendor failed to complete milestone 2 delivery; liquidated damages penalty applied.')
        click('button', 'Confirm and save payment')

if not done(V14, '02-accept-shortfall'):
    login('mira@demo.fervid.local', 'DemoPass2026!')
    goto(shortfall_accept_req)
    with scene(V14, '02-accept-shortfall', 'Manager accepts shortfall and writes off balance',
               'Mira reviews the penalty justification. Agreeing that the remaining three thousand rupees is no longer owed, she clicks Accept and close. This permanently closes the request as Completed with partial accepted.',
               expected='Completed — partial accepted'):
        click('link', 'Decide the partial payment')
        click('button', 'Accept and close')
        fill('Note', 'Confirmed penalty deduction with project coordinator. Shortfall accepted.')
        click('button', 'Accept and close', nth=1)

# Fixture for dispute
shortfall_concern_req = ensure_fixture('Lesson P Shortfall Concern', '7000')
# Anil pays 5000 and refers shortfall
login('anil@demo.fervid.local', 'DemoPass2026!')
goto(shortfall_concern_req)
snap = snapshot()
if any(x.get('role') == 'button' and x.get('name') == 'Record payment' for x in snap):
    ensure_payment_form(shortfall_concern_req)
    fill('Amount actually paid', '5000')
    choose('Payment mode', 'NEFT')
    fill('Transaction / UTR reference', 'UTR-2026-CONCERN-5000')
    click('button', 'Payment settled →')
    call('check', ref('radio', 'Ask manager to accept a shortfall', exact=False))
    fill('Reason for any deduction or shortfall', 'Deduction made without attached explanation.')
    click('button', 'Confirm and save payment')

if not done(V14, '03-dispute-shortfall'):
    login('mira@demo.fervid.local', 'DemoPass2026!')
    goto(shortfall_concern_req)
    with scene(V14, '03-dispute-shortfall', 'Manager disputes unauthorized deduction',
               'In another case, the approver notices an unexplained two thousand rupee shortfall. Mira clicks Raise a concern and documents her objection. The request remains open under discussion until Accounts clarifies the issue.',
               expected='raised a concern'):
        click('link', 'Decide the partial payment')
        click('button', 'Raise a concern')
        fill('What is wrong', 'This deduction was not authorized by the contract; please verify invoice breakdown with vendor.')
        click('button', 'Raise concern')

# --- Video 19: Payment Evidence ---
print("--- Track 3: Video 19 - Payment Evidence ---")
V19 = '19-payment-evidence'
video(V19, 'Payment advice documents and bank reference integrity', 'Accounts and Auditors',
      ['Upload bank confirmation advice PDF', 'Attach advice during settlement confirmation', 'Verify transaction reference and download document'],
      ['Advice documents provide tamper-evident proof of external bank execution.', 'Uploaded documents are accessible through payment history.'])

evidence_req = ensure_fixture('Lesson P Payment Evidence', '6000')

login('anil@demo.fervid.local', 'DemoPass2026!')

pdf_path = str(ROOT / 'output/playwright/workflow-videos-2026-09-26/evidence/demo-payment-advice.pdf')

if not done(V19, '01-upload-advice'):
    ensure_payment_form(evidence_req)
    with scene(V19, '01-upload-advice', 'Upload bank confirmation advice PDF',
               'When settling payments, Accounts attaches bank payment advice documents directly to the transaction. Uploading the bank PDF establishes verifiable audit proof connecting the internal record to the external banking system.',
               expected='Completed'):
        fill('Amount actually paid', '6000')
        choose('Payment mode', 'NEFT')
        fill('Transaction / UTR reference', 'UTR-2026-EVIDENCE-6000')
        call('run-code', f"async page => {{ await page.locator('input[type=file]').setInputFiles({json.dumps(pdf_path)}); }}")
        click('button', 'Payment settled →')
        click('button', 'Confirm and save payment')

if not done(V19, '02-utr-trace'):
    goto(evidence_req)
    with scene(V19, '02-utr-trace', 'Verify transaction reference and download document',
               'Auditors and finance managers can open the payment record to inspect the exact UTR reference, payment timestamp, and download the original advice PDF, guaranteeing full document traceability.',
               expected='demo-payment-advice.pdf'):
        click('link', 'View the payment')

# --- Video 26: Payments Ledger & Voids ---
print("--- Track 3: Video 26 - Audit Register and Voiding Payments ---")
V26 = '26-payments-ledger-voids'
video(V26, 'Audit the payments register and void an entry', 'Administrators and Accounts',
      ['Filter payments register by method and date range', 'Inspect single payment history and attached request', 'Void an erroneous payment with mandatory explanation', 'Verify returned request in Accounts queue'],
      ['Voiding is an administrative correction, not a bank clawback.', 'Voided payments remain in the audit trail but are excluded from actuals.'])

void_req = ensure_fixture('Lesson P Void Payment', '4200')
login('anil@demo.fervid.local', 'DemoPass2026!')
goto(void_req)
snap = snapshot()
if any(x.get('role') == 'button' and x.get('name') == 'Record payment' for x in snap):
    ensure_payment_form(void_req)
    fill('Amount actually paid', '4200')
    choose('Payment mode', 'NEFT')
    fill('Transaction / UTR reference', 'UTR-2026-VOID-4200')
    click('button', 'Payment settled →')
    click('button', 'Confirm and save payment')

login('admin@fervid.local', 'admin123')

if not done(V26, '01-filter-ledger'):
    login('admin@fervid.local', 'admin123')
    goto('/payments')
    with scene(V26, '01-filter-ledger', 'Filter payments register by method and date range',
               'The Payments Register maintains a permanent record of all disbursements. Administrators can filter by payment mode, date range, or transaction reference to trace financial activity.',
               expected='UTR-2026-VOID-4200'):
        fill('Search', 'UTR-2026-VOID-4200')
        click('button', 'Filter')

if not done(V26, '02-inspect-history'):
    goto('/payments')
    fill('Search', 'UTR-2026-VOID-4200')
    click('button', 'Filter')
    with scene(V26, '02-inspect-history', 'Inspect single payment history and attached request',
               'Open the payment entry to review the full transaction profile: who recorded it, the bank UTR reference, associated budget head, and the original payment request.',
               expected='PAY-'):
        click('link', 'View')

if not done(V26, '03-void-entry'):
    goto('/payments?month=2026-06')
    with scene(V26, '03-void-entry', 'Void an erroneous payment with mandatory explanation',
               'If a payment was recorded in error—such as an external bank reversal or accidental duplicate entry—an administrator voids it. Supplying a detailed reason ensures audit compliance while removing the figure from budget actuals.',
               expected='Removed'):
        call('run-code', 'async page => { await page.locator("summary").click(); }')
        fill('Reason', 'Duplicate payout entry created during statement import; reversed with banking partner.')
        call('run-code', 'async page => { page.on("dialog", d => d.accept()); await page.locator("button.danger", { hasText: "Remove" }).click(); }')

if not done(V26, '04-reactivate'):
    goto('/payments?month=2026-06&status=voided')
    with scene(V26, '04-reactivate', 'Verify returned request in Accounts queue',
               'Voiding the payment safely excludes it from budget actuals while maintaining the permanent audit trail, displaying the documented reason for financial compliance.',
               expected='Removed'):
        click('heading', 'Payments')

print("Track 3 Payments recording complete!")
