"""Admin & Reporting Agent: Records Reports & Grid, Vendor Context, Notifications & Password Recovery, Config & Backups, Month Close, and Custom Role Scopes."""
import json, time, pathlib, sys, os, sqlite3

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from capture import *

def done(v, k):
    return any(s['id'] == v + '-' + k for x in plan()['videos'] if x['id'] == v for s in x['scenes'])

def choose(name, label):
    snapshot()
    call('run-code', f"async page => {{ await page.getByRole('combobox', {{name:{json.dumps(name)},exact:true}}).selectOption({{label:{json.dumps(label)}}}); }}")

print("=== Starting Track 5: Admin, Reports & Governance ===")

# --- Video 20: Reports and Variance Grid ---
print("--- Track 5: Video 20 - Reports and Variance Grid ---")
V20 = '20-reports-grid'
video(V20, 'Trace reports from totals to transactions', 'Manager · Accounts',
      ['Monthly project and head reports', 'Date ranges', 'Payment drilldown', 'CSV scope', 'Variance filters'],
      ['Actuals are recorded active budget payments', 'Recovery and voided payments excluded', 'Payment detail access may restrict visible records'])

login('admin@fervid.local', 'admin123')

if not done(V20, '01-monthly'):
    goto('/reports/monthly')
    with scene(V20, '01-monthly', 'Start with the period and report level',
               'Reports compare planned budgets with recorded actuals across projects and expense heads. Choose the calendar month or custom date range to see total planned versus disbursed funds.',
               expected='Reports'):
        click('heading', 'Reports')

if not done(V20, '02-project'):
    with scene(V20, '02-project', 'Drill down from high-level totals into detailed payments',
               'Click an expense head to drill down into the individual payments that form its actuals. This connects the macro budget figure directly to specific vendor payments and bank references.',
               expected='Reports'):
        goto('/reports/heads?head=1')

if not done(V20, '03-grid-filter'):
    with scene(V20, '03-grid-filter', 'Analyze monthly status in the variance grid',
               'The variance grid organizes heads by project, displaying budget, actual spending, and operational status. Notice how heads with budget but no spending show Not paid, distinguishing them from heads with No activity.',
               expected='Variance grid'):
        goto('/grid')

# --- Video 21: Vendor Context and Linked History ---
print("--- Track 5: Video 21 - Vendor Context and Linked History ---")
V21 = '21-vendor-context'
video(V21, 'Trace a vendor across requests, payments and history', 'Administrators, managers and Accounts',
      ['Search vendor master', 'Linked requests and return navigation', 'Linked-payment scope', 'Vendor change history'],
      ['Related lists obey record permissions', 'Historical unlinked payments lack verified vendor association', 'TDS defaults are reference values, not computed tax', 'Bank data requires its own permission'])

goto('/vendors')

if not done(V21, '01-find'):
    goto('/vendors')
    with scene(V21, '01-find', 'Locate the vendor in the centralized directory',
               'Search the vendor master and open Lesson Studio Office Supplies. The vendor record brings together contact info, tax identifiers, and default payment routes in one trusted location.',
               expected='Lesson Studio Office Supplies'):
        click('link', 'Lesson Studio Office Supplies')

if not done(V21, '02-requests'):
    goto('/vendors/13')
    with scene(V21, '02-requests', 'Review all requests linked to this vendor',
               'The Requests tab consolidates all payment requests associated with this vendor. Review their status and approval trails, then easily return to the vendor record.',
               expected='Requests'):
        click('link', 'Requests', exact=True)

# --- Video 23: Notifications and Password Recovery ---
print("--- Track 5: Video 23 - Notifications and Password Recovery ---")
V23 = '23-notifications-access-recovery'
video(V23, 'Manage notifications and restore sign-in access', 'All users and administrators',
      ['Read personal notifications', 'Filter unread activity', 'Review event audiences and email setup', 'Catch template mistakes before saving', 'Offline access recovery through administrator', 'Reset a dedicated demonstration account'],
      ['In-app notifications are always enabled; email is separate and opt-in per event.', 'Email reset needs configured delivery; this demonstration installation has no SMTP.', 'Administrator identity verification and private password delivery happen outside the application.'])

goto('/notifications')

if not done(V23, '01-inapp'):
    with scene(V23, '01-inapp', 'Check personal notifications and action items',
               'The notification center alerts users when requests need action, approvals are granted, or payments are recorded. Filtering to Unread helps staff keep work queues clear.',
               expected='Notifications'):
        click('heading', 'Notifications')

if not done(V23, '02-rules'):
    goto('/admin/notifications')
    with scene(V23, '02-rules', 'Configure notification event rules and email templates',
               'Administrators configure which events trigger alerts and customize email templates. Template variables must be valid; entering an invalid tag triggers an immediate syntax warning before saving.',
               expected='Notification rules'):
        click('heading', 'Notification rules')

# Ensure demo user exists
goto('/users')
snap = snapshot()
if not any('tara@demo.fervid.local' in x.get('name', '') for x in snap):
    click('button', '＋ Add user')
    fill('Email', 'tara@demo.fervid.local')
    fill('Name', 'Tara Training')
    call('check', ref('checkbox', 'Requester', exact=False))
    fill('Password', 'TrainingOld2026!')
    click('button', 'Add User')

if not done(V23, '03-admin-reset'):
    goto('/users')
    call('run-code', "async page => { await page.getByRole('row').filter({hasText:'tara@demo.fervid.local'}).getByRole('button',{name:'Edit',exact:true}).click(); }")
    with scene(V23, '03-admin-reset', 'Administrator resets password offline for user',
               'When a user is locked out and email delivery is unavailable, the administrator verifies their identity offline and sets a secure replacement password directly in the user profile.',
               expected='Users'):
        fill('Reset password', 'TrainingNew2026!')
        click('button', 'Save user')

if not done(V23, '04-verify-login'):
    with scene(V23, '04-verify-login', 'Verify user sign-in with updated password',
               'Sign in as Tara with the replacement password to verify that access is restored and her role-specific workspace loads seamlessly.',
               expected='Tara Training'):
        login('tara@demo.fervid.local', 'TrainingNew2026!')

# Switch back to admin
login('admin@fervid.local', 'admin123')

# --- Video 24: Configuration, Audit and Backups ---
print("--- Track 5: Video 24 - Configuration, Audit and Backups ---")
V24 = '24-configuration-audit-backups'
video(V24, 'Configure controls, inspect history, and create a backup', 'Administrators and installation operators',
      ['Request numbering and document policy', 'Urgency, approval and reminder controls', 'Recoverable category lifecycle', 'Audit filters', 'Create backup and read recovery guidance'],
      ['Second approval above a threshold is a future feature.', 'In-use recoverable categories cannot be deleted.', 'Backup creation does not verify a restore. Configuration and secrets need separate operational management.'])

if not done(V24, '01-policy'):
    goto('/configuration')
    with scene(V24, '01-policy', 'Set request numbering and compliance policies',
               'System configuration controls request prefix numbering, mandatory invoice attachment policies, and payment modes offered during settlement.',
               expected='Configuration'):
        click('heading', 'Configuration')

if not done(V24, '02-audit'):
    goto('/audit')
    with scene(V24, '02-audit', 'Filter system audit trail by entity and user',
               'Every security-sensitive change and financial transaction writes an immutable entry to the Audit Log. Filter by entity, actor, or date range to inspect before and after values.',
               expected='Audit Log'):
        click('heading', 'Audit Log')

if not done(V24, '03-backup'):
    goto('/backups')
    with scene(V24, '03-backup', 'Create a consistent database and attachment backup',
               'Administrators can trigger a server backup at any time. SQLite VACUUM INTO creates a zero-downtime, fully consistent copy of both the database and uploaded attachments.',
               expected='Backups'):
        click('button', 'Create Backup')

# --- Video 25: Month Close and Documented Reopening ---
print("--- Track 5: Video 25 - Month Close and Documented Reopening ---")
V25 = '25-month-close'
video(V25, 'Close a month and reopen it with a reason', 'Administrators and budget owners',
      ['Create a blank month', 'Review full-month close indicators', 'Lock changes', 'Verify read-only budgets', 'Unlock with an audit reason'],
      ['Closing is a control on budget and payment changes, not proof that bank balances reconcile.', 'An authorised person can unlock a month; the reason belongs in the audit trail.'])

if not done(V25, '01-create'):
    goto('/months')
    with scene(V25, '01-create', 'Create a separate monthly accounting period',
               'Monthly Plans establishes accounting periods. Create a blank month to start fresh without copying prior allocations.',
               expected='2026-11'):
        fill('New month', '2026-11')
        choose('Plan type', 'Start blank')
        click('button', 'Create Month')

if not done(V25, '02-lock'):
    goto('/grid?month=2026-11')
    call('run-code', 'async page => { page.on("dialog", d => d.accept()); }')
    with scene(V25, '02-lock', 'Review close readiness indicators and lock month',
               'The Month Close panel displays unpaid budget heads, overspending, and unbudgeted activity. When ready, enter the documented reason and confirm the lock, freezing budgets and payments read-only.',
               expected='Unlock Month'):
        fill('Lock reason', 'Training: November month close executed after planning review.')
        click('button', 'Lock Month')

if not done(V25, '03-verify-lock'):
    with scene(V25, '03-verify-lock', 'Verify read-only budget freeze',
               'Opening the locked month in Budgets shows that all budget inputs and save buttons are disabled, preventing unmonitored mid-period alterations.',
               expected='Locked'):
        goto('/budgets?month=2026-11')

if not done(V25, '04-unlock'):
    goto('/grid?month=2026-11')
    call('run-code', 'async page => { page.on("dialog", d => d.accept()); }')
    with scene(V25, '04-unlock', 'Reopen period with required documented reason',
               'If an authorized adjustment is required, an administrator can reopen the month. Supplying an explicit unlock reason ensures full accountability in the permanent audit trail.',
               expected='Lock Month'):
        fill('Unlock reason', 'Authorized adjustment: reopen November for revised operations workshop venue budget.')
        click('button', 'Unlock Month')

# --- Video 27: Custom Roles and Scopes ---
print("--- Track 5: Video 27 - Custom Roles and Scopes ---")
V27 = '27-role-capabilities-scopes'
video(V27, 'Build a limited role and verify the resulting access', 'Administrators and access reviewers',
      ['Create custom role with no initial permissions', 'Separate action capabilities from record scope', 'Inspect advanced permissions', 'Assign only to a demonstration user', 'Verify actual restricted access'],
      ['Changing a role affects every user assigned to it.', 'A role name alone does not define access; capabilities and record scopes do.', 'Multiple roles can broaden access; review the complete membership set.'])

login('admin@fervid.local', 'admin123')
goto('/roles')

if not done(V27, '01-new-role'):
    with scene(V27, '01-new-role', 'Create a new custom role with zero default permissions',
               'Custom roles start completely unprivileged. Administrators select exactly which capabilities—View, Create, Edit, Approve, or Cancel—are granted, and whether the data scope covers Own records or All records.',
               expected='Roles'):
        click('button', '＋ New role')
        call('run-code', """async page => {
            await page.locator('#nr-name').fill('Training own request viewer');
            await page.locator('#nr-desc').fill('Restricted training role: view own payment requests only without creation or approval privileges.');
            await page.locator('#role-new button[type=submit]').click();
        }""")

if not done(V27, '02-grants'):
    conn = sqlite3.connect(ROOT / 'data/fervid.db')
    cur = conn.cursor()
    cur.execute("SELECT id FROM roles WHERE name = 'Training own request viewer' LIMIT 1")
    row = cur.fetchone()
    conn.close()
    if row:
        goto(f'/roles?role={row[0]}')
    with scene(V27, '02-grants', 'Configure granular View action and Own record scope',
               'For Payment requests, enable View and select Own scope under Records it can see. This restricts the user strictly to requests they submitted themselves.',
               expected='Roles'):
        call('check', ref('checkbox', 'View', nth=0))
        call('check', ref('radio', 'Own', nth=0))
        click('button', 'Save role')

if not done(V27, '03-membership'):
    login('admin@fervid.local', 'admin123')
    goto('/users')
    call('run-code', "async page => { await page.getByRole('row').filter({hasText:'tara@demo.fervid.local'}).getByRole('button',{name:'Edit',exact:true}).click(); }")
    with scene(V27, '03-membership', 'Assign custom role to user and remove broad roles',
               'In Users, assign the restricted custom role to Tara and uncheck broader roles. Restricting roles enforces least-privilege compliance across the team.',
               expected='Users'):
        call('uncheck', ref('checkbox', 'Requester', exact=False))
        call('check', ref('checkbox', 'Training own request viewer', exact=False))
        click('button', 'Save user')

if not done(V27, '04-verify-scope'):
    with scene(V27, '04-verify-scope', 'Verify restricted workspace as the affected user',
               'Sign in as Tara to inspect her workspace. The navigation is stripped down to My requests. Creation and approval actions are blocked, confirming the exact scope boundaries.',
               expected='My requests'):
        login('tara@demo.fervid.local', 'TrainingNew2026!')
        click('link', 'My requests', exact=False)

print("Track 5 Admin recording complete!")
