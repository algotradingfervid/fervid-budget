"""Requests Agent: Records Request Submission, Duplicate Invoices, Urgent Requests, Manager Review & Approvals, Corrections & Rejections, and Withdraw & Cancel."""
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

def get_request_url(title):
    rid = get_request_id(title)
    return f"/requests/{rid}" if rid else None

print("=== Starting Track 2: Requests & Approvals ===")

# --- Video 05: Raising Requests ---
print("--- Track 2: Video 05 - Raising Requests ---")
video('05-raising-requests', 'Submitting standard budget requests', 'Requesters',
      ['Raise vendor invoice request', 'Fill required invoice reference and date', 'Submit reimbursement request', 'Track submission on dashboard'],
      ['Submission requires mandatory approver selection', 'Recording a request does not transfer money', 'Budget availability is checked at approval'])

login('riya@demo.fervid.local', 'DemoPass2026!')

if not done('05-raising-requests', '01-vendor-invoice'):
    goto('/requests/new?type=vendor_invoice')
    with scene('05-raising-requests', '01-vendor-invoice', 'Raise a vendor invoice payment request',
               'Requesters submit vendor invoices once goods or services have been received. Choose the verified vendor, enter the exact bill amount, and specify the project and expense head. Supplying the vendor invoice number and bill date ensures accounting traceability before moving to the approver.',
               expected='What happens next'):
        fill('Short title', 'Office stationery and printer cartridges')
        choose('Project', 'Operations')
        choose('Head', 'Operations / Office Supplies')
        fill('Amount', '4500')
        fill('Needed by', '2026-09-30')
        choose('Vendor', 'Lesson Studio Office Supplies')
        fill('Invoice number', 'INV-2026-Q01')
        fill('Invoice date', '2026-09-20')
        fill('Purpose', 'Quarterly replenishment of printing paper and toner cartridges for operations team.')
        choose('Approver', 'Mira Shah')
        click('button', 'Submit request')

if not done('05-raising-requests', '02-reimbursement'):
    goto('/requests/new?type=reimbursement')
    with scene('05-raising-requests', '02-reimbursement', 'Submit an employee reimbursement request',
               'Reimbursements repay staff for out-of-pocket expenses already incurred on behalf of the organisation. The payee is automatically fixed to the requester. Enter the expense date, purpose, and classification so the manager can review policy compliance before authorising repayment.',
               expected='What happens next'):
        fill('Short title', 'Team client working lunch')
        choose('Project', 'Operations')
        choose('Head', 'Operations / Office Supplies')
        fill('Amount', '1200')
        fill('Needed by', '2026-09-30')
        fill('Expense date', '2026-09-24')
        fill('Purpose', 'Working lunch with partner logistics coordinator to finalise delivery routes.')
        choose('Approver', 'Mira Shah')
        click('button', 'Submit request')

if not done('05-raising-requests', '03-tracking'):
    with scene('05-raising-requests', '03-tracking', 'Track pending requests on the dashboard',
               'Once submitted, requests appear in the requester’s workspace with an Awaiting approval badge. The dashboard separates pending items from returned or settled records. Requesters can inspect current approval assignments and see exactly which manager is reviewing their submission.',
               expected='Awaiting approval'):
        goto('/requests')

# --- Video 06: Duplicate Invoices ---
print("--- Track 2: Video 06 - Duplicate Invoices ---")
video('06-duplicate-invoice', 'Duplicate invoice detection and override justification', 'Requesters and Managers',
      ['Duplicate invoice alert on identical vendor and invoice number', 'Provide required override reason', 'Approver reviews duplicate warning banner'],
      ['Duplicate check matches vendor and invoice number combination', 'Override justification is recorded in the permanent audit trail'])

if not done('06-duplicate-invoice', '01-trigger-duplicate'):
    goto('/requests/new?type=vendor_invoice')
    with scene('06-duplicate-invoice', '01-trigger-duplicate', 'Attempt submission of an existing invoice number',
               'To protect against duplicate payments, Fervid Budget tracks vendor and invoice number pairs. When a requester enters an invoice number that is already active for that vendor, the system prompts for verification rather than creating an unmonitored duplicate.',
               expected='similar request'):
        fill('Short title', 'Second copy of stationery delivery')
        choose('Project', 'Operations')
        choose('Head', 'Operations / Office Supplies')
        fill('Amount', '4500')
        fill('Needed by', '2026-09-30')
        choose('Vendor', 'Lesson Studio Office Supplies')
        fill('Invoice number', 'INV-2026-Q01')
        fill('Invoice date', '2026-09-20')
        fill('Purpose', 'Supplemental shipment for training center supplies.')
        choose('Approver', 'Mira Shah')
        click('textbox', 'Short title')
        time.sleep(1)

if not done('06-duplicate-invoice', '02-warning-banner'):
    with scene('06-duplicate-invoice', '02-warning-banner', 'Review warning banner and supply override justification',
               'The page highlights that invoice INV-2026-Q01 already exists for Lesson Studio Office Supplies. If this bill genuinely represents a second legitimate charge or split billing, the requester must supply an explicit written override justification before the application permits submission.',
               expected='What happens next'):
        fill('Duplicate invoice override reason only if a match is found', 'Supplier split shipment: Part B delivery invoiced under parent purchase order reference.')
        click('button', 'Submit request')

dup_req_url = get_request_url('Second copy of stationery delivery')

if not done('06-duplicate-invoice', '03-approver-context'):
    login('mira@demo.fervid.local', 'DemoPass2026!')
    goto(dup_req_url)
    with scene('06-duplicate-invoice', '03-approver-context', 'Manager reviews duplicate exception context',
               'The assigned manager sees a dedicated duplicate warning banner directly on the approval screen. This ensures the approver is fully aware of the matching invoice record and can verify the requester’s explanation against the supporting documentation before deciding to approve.',
               expected='Approve'):
        time.sleep(1)

# --- Video 07: Urgent Requests ---
print("--- Track 2: Video 07 - Urgent Requests ---")
video('07-urgent-requests', 'Flagging urgent requests and expedited routing', 'Requesters and Managers',
      ['Mark request urgent', 'Provide mandatory business urgency rationale', 'High-priority badge in approvals queue'],
      ['Urgency does not bypass the approval policy or spending limits', 'Urgent requests follow standard manager authorization'])

login('riya@demo.fervid.local', 'DemoPass2026!')

if not done('07-urgent-requests', '01-flag-urgent'):
    goto('/requests/new?type=vendor_invoice')
    with scene('07-urgent-requests', '01-flag-urgent', 'Flag an urgent business operational requirement',
               'When immediate payment is critical to prevent operational stoppage, requesters can mark a request as urgent. Checking urgency requires a clear explanation of why expedited handling is necessary, helping managers prioritize genuine operational emergencies.',
               expected='What happens next'):
        fill('Short title', 'Emergency network router replacement')
        choose('Project', 'Operations')
        choose('Head', 'Operations / Office Supplies')
        fill('Amount', '8500')
        fill('Needed by', '2026-09-26')
        choose('Vendor', 'Lesson Studio Office Supplies')
        fill('Invoice number', 'INV-2026-ROUTER-99')
        fill('Invoice date', '2026-09-25')
        fill('Purpose', 'Core office gateway router failed; network offline until replacement hardware installed.')
        check('checkbox', 'Mark this urgent')
        fill('Why is it urgent', 'Internet connection is completely offline for entire team; urgent payment required to release replacement router today.')
        choose('Approver', 'Mira Shah')
        click('button', 'Submit request')

urgent_url = get_request_url('Emergency network router replacement')

if not done('07-urgent-requests', '02-queue-badge'):
    login('mira@demo.fervid.local', 'DemoPass2026!')
    with scene('07-urgent-requests', '02-queue-badge', 'Approver identifies high-priority urgent items',
               'Urgent items are highlighted with a prominent red badge in the approvals queue. This allows managers to immediately spot time-sensitive requests without sifting through standard submissions, accelerating review while maintaining full approval governance.',
               expected='Urgent'):
        goto('/approvals')

# --- Video 08: Manager Review and Approvals ---
print("--- Track 2: Video 08 - Manager Review and Approvals ---")
video('08-manager-review-approve', 'Reviewing and approving requests', 'Managers and Approvers',
      ['Review request purpose and vendor', 'Check budget headroom and attachments', 'Document approval comments', 'Approve full amount'],
      ['Approval authorizes Accounts to pay; it does not trigger a bank transfer', 'Managers cannot approve their own requests'])

login('mira@demo.fervid.local', 'DemoPass2026!')
req1_url = get_request_url('Office stationery and printer cartridges')
urgent_url = get_request_url('Emergency network router replacement')

if not done('08-manager-review-approve', '01-inspect-request'):
    with scene('08-manager-review-approve', '01-inspect-request', 'Inspect request details and budget context',
               'Managers open the request to review all essential details: the vendor, amount, business justification, invoice number, and budget head. The review screen displays current budget headroom so the approver can verify spending limits before authorising funds.',
               expected='Approve'):
        goto(req1_url)

goto(req1_url)
if not done('08-manager-review-approve', '02-approve-request'):
    with scene('08-manager-review-approve', '02-approve-request', 'Document approval comments and authorise payment',
               'Approvers can record a permanent note alongside their decision. Mira enters an approval note confirming delivery and clicks Approve. The request transitions immediately to Approved, awaiting payment, and moves into the Accounts queue.',
               expected='Approved'):
        click('button', 'Approve ₹4,500.00')
        click('button', 'Approve request')
        goto(req1_url)

if not done('08-manager-review-approve', '03-approve-urgent'):
    goto(urgent_url)
    with scene('08-manager-review-approve', '03-approve-urgent', 'Authorise the expedited urgent request',
               'Next, Mira reviews the urgent router request. Seeing the business-critical justification, she approves the full ₹8,500. The urgent status remains attached to the record, signalling Accounts to expedite settlement when it reaches their work queue.',
               expected='Approved'):
        click('button', 'Approve ₹8,500.00')
        click('button', 'Approve request')
        goto(urgent_url)

# --- Video 09: Corrections and Rejection ---
print("--- Track 2: Video 09 - Corrections and Rejection ---")
video('09-corrections-rejection', 'Returning for correction, rejection and "Raise it again"', 'Requesters and Managers',
      ['Return request for correction with feedback', 'Requester corrects fields and resubmits', 'Documented rejection', 'Raise it again traceable re-creation'],
      ['Rejection closes the approval cycle permanently', 'Raise it again creates a new request number while linking history'])

# Ensure return fixture exists
return_url = get_request_url('Team whiteboards and markers')
if not return_url:
    login('riya@demo.fervid.local', 'DemoPass2026!')
    goto('/requests/new?type=vendor_invoice')
    fill('Short title', 'Team whiteboards and markers')
    choose('Project', 'Operations')
    choose('Head', 'Operations / Office Supplies')
    fill('Amount', '3200')
    fill('Needed by', '2026-09-30')
    choose('Vendor', 'Lesson Studio Office Supplies')
    fill('Invoice number', 'INV-2026-WB-01')
    fill('Invoice date', '2026-09-22')
    fill('Purpose', 'Whiteboards for workshop rooms')
    choose('Approver', 'Mira Shah')
    click('button', 'Submit request')
    return_url = get_request_url('Team whiteboards and markers')

# Ensure reject fixture exists
reject_url = get_request_url('Personal premium coffee subscriptions')
if not reject_url:
    login('riya@demo.fervid.local', 'DemoPass2026!')
    goto('/requests/new?type=reimbursement')
    fill('Short title', 'Personal premium coffee subscriptions')
    choose('Project', 'Operations')
    choose('Head', 'Operations / Office Supplies')
    fill('Amount', '1800')
    fill('Needed by', '2026-09-30')
    fill('Expense date', '2026-09-24')
    fill('Purpose', 'Personal coffee machine pods for home office.')
    choose('Approver', 'Mira Shah')
    click('button', 'Submit request')
    reject_url = get_request_url('Personal premium coffee subscriptions')

if not done('09-corrections-rejection', '01-return-for-correction'):
    login('mira@demo.fervid.local', 'DemoPass2026!')
    goto(return_url)
    with scene('09-corrections-rejection', '01-return-for-correction', 'Return a request with specific feedback',
               'When a submission lacks required detail or contains an error, the manager can return it for correction. The feedback note clearly explains what must be updated. Returning pauses the approval cycle and places the request back in the requester’s hands without discarding the record.',
               expected='Returned for correction'):
        click('button', 'Return for correction')
        fill('What needs correcting', 'Please verify if wall mounting hardware is included in this quote, or add a separate line item.')
        click('button', 'Return request')
        goto(return_url)

if not done('09-corrections-rejection', '02-requester-update'):
    login('riya@demo.fervid.local', 'DemoPass2026!')
    goto(return_url)
    with scene('09-corrections-rejection', '02-requester-update', 'Requester updates details and resubmits',
               'Riya logs in and finds the request marked Returned for correction. She reviews Mira’s comment, updates the purpose to clarify that mounting hardware is included, and resubmits. Resubmission returns the request to the manager with full revision history preserved.',
               expected='Awaiting approval'):
        fill('Purpose', 'Whiteboards for workshop rooms — mounting hardware and brackets confirmed included in package.')
        click('button', 'Resubmit for approval')

if not done('09-corrections-rejection', '03-reject-request'):
    login('mira@demo.fervid.local', 'DemoPass2026!')
    goto(reject_url)
    with scene('09-corrections-rejection', '03-reject-request', 'Reject an unallowable request with clear justification',
               'When an expense falls outside company policy or budget scope, the approver rejects it with a clear written reason. Rejection terminates this specific approval cycle permanently; the record remains in history as evidence of the decision.',
               expected='Rejected'):
        click('button', 'Reject')
        fill('Reason for rejection', 'Personal home beverage subscriptions are not eligible for company reimbursement under the employee expense policy.')
        click('button', 'Reject permanently')
        goto(reject_url)

if not done('09-corrections-rejection', '04-reraise'):
    login('riya@demo.fervid.local', 'DemoPass2026!')
    goto(reject_url)
    with scene('09-corrections-rejection', '04-reraise', 'Use "Raise it again" for traceable re-creation',
               'If the requester later receives updated authorization or corrects the core terms, she can use "Raise it again". This action creates a fresh request pre-populated from the rejected details without reopening the closed record. History traces the new submission back to the rejected parent.',
               expected='What happens next'):
        click('button', 'Raise it again')

if not done('09-corrections-rejection', '05-new-cycle'):
    with scene('09-corrections-rejection', '05-new-cycle', 'Verify the new linked approval cycle',
               'The new request receives its own unique request number while linking back to the rejected predecessor. Requesters can edit details before sending it for review. This maintains complete traceability across organizational decision cycles.',
               expected='Awaiting approval'):
        click('link', 'View this request')

# --- Video 18: Withdraw and Cancel ---
print("--- Track 2: Video 18 - Withdraw and Cancel ---")
video('18-withdraw-cancel', 'Withdrawing drafts and post-approval cancellation requests', 'Requesters and Managers',
      ['Withdraw pending unapproved request', 'Request cancellation on approved unpaid request', 'Payment freeze during cancellation review', 'Manager cancellation acceptance'],
      ['Paid requests cannot be cancelled; only unpaid approved requests can be cancelled', 'Cancellation closes the request with audit trail'])

withdraw_url = get_request_url('Accidental duplicate taxi receipt')
if not withdraw_url:
    login('riya@demo.fervid.local', 'DemoPass2026!')
    goto('/requests/new?type=reimbursement')
    fill('Short title', 'Accidental duplicate taxi receipt')
    choose('Project', 'Operations')
    choose('Head', 'Operations / Office Supplies')
    fill('Amount', '650')
    fill('Needed by', '2026-09-30')
    fill('Expense date', '2026-09-24')
    fill('Purpose', 'Taxi ride to vendor office.')
    choose('Approver', 'Mira Shah')
    click('button', 'Submit request')
    withdraw_url = get_request_url('Accidental duplicate taxi receipt')

if not done('18-withdraw-cancel', '01-withdraw-pending'):
    login('riya@demo.fervid.local', 'DemoPass2026!')
    goto(withdraw_url)
    with scene('18-withdraw-cancel', '01-withdraw-pending', 'Withdraw an unapproved pending request',
               'If a requester realizes an error or no longer requires funds, they can withdraw their request while it is still pending approval. Withdrawing immediately removes the item from the manager’s queue without needing managerial intervention.',
               expected='Withdrawn'):
        click('button', 'Withdraw')

cancel_url = get_request_url('Bulk packaging boxes order')
if not cancel_url:
    login('riya@demo.fervid.local', 'DemoPass2026!')
    goto('/requests/new?type=vendor_invoice')
    fill('Short title', 'Bulk packaging boxes order')
    choose('Project', 'Operations')
    choose('Head', 'Operations / Office Supplies')
    fill('Amount', '5400')
    fill('Needed by', '2026-09-30')
    choose('Vendor', 'Lesson Studio Office Supplies')
    fill('Invoice number', 'INV-2026-BOX-10')
    fill('Invoice date', '2026-09-21')
    fill('Purpose', 'Storage boxes for archive room.')
    choose('Approver', 'Mira Shah')
    click('button', 'Submit request')
    cancel_url = get_request_url('Bulk packaging boxes order')

    # Approve it first so cancellation can be requested
    login('mira@demo.fervid.local', 'DemoPass2026!')
    goto(cancel_url)
    click('button', 'Approve ₹5,400.00')
    click('button', 'Approve request')

if not done('18-withdraw-cancel', '02-request-cancellation'):
    login('riya@demo.fervid.local', 'DemoPass2026!')
    goto(cancel_url)
    with scene('18-withdraw-cancel', '02-request-cancellation', 'Request cancellation on an approved unpaid request',
               'Once a request is approved, the requester can no longer withdraw it directly. If circumstances change—such as an order cancellation—they submit a cancellation request with full justification. This places a freeze on payment processing while the manager decides.',
               expected='Cancellation requested'):
        click('link', 'Request cancellation')
        fill('Reason', 'Supplier informed us items are out of stock and order is cancelled. No payment should be made.')
        click('button', 'Send cancellation request')

if not done('18-withdraw-cancel', '03-accept-cancellation'):
    login('mira@demo.fervid.local', 'DemoPass2026!')
    goto(cancel_url)
    with scene('18-withdraw-cancel', '03-accept-cancellation', 'Manager reviews and accepts cancellation',
               'Mira opens the cancellation request. She verifies that the vendor order was indeed cancelled and no invoice remains payable. Clicking Accept cancellation safely closes the request as Cancelled, ensuring company funds are never disbursed.',
               expected='Cancelled'):
        click('link', 'Decide the cancellation')
        click('button', 'Cancel the request')
        click('button', 'Cancel request')
        goto(cancel_url)

print("Track 2 Requests recording complete!")
