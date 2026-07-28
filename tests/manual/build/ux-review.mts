// The UX review that came out of photographing every screen.
//
// Each finding says how it is known, because that changes how much it should be
// trusted: `measured` was produced by a machine against the running product and
// can be re-run; `code` was read out of the source and is cited to file:line;
// `judgement` is an opinion formed by looking at the screenshots, and is the
// only kind a reasonable person might simply disagree with.
//
// Findings marked `fixed` were repaired during the review and the fix verified
// by re-running the capture. Findings marked `open` were left alone on purpose:
// each needs a product decision, not a patch.

export type Severity = 'high' | 'medium' | 'low';
export type Evidence = 'measured' | 'code' | 'judgement';
export type Status = 'fixed' | 'open';

export interface Finding {
  id: string;
  title: string;
  severity: Severity;
  status: Status;
  evidence: Evidence;
  /** Where it lives: a file:line, a screen, or a count of screens. */
  where: string;
  /** What is wrong. Plain statement of fact. */
  what: string;
  /** Why it matters to somebody using the product. */
  why: string;
  /** What was done, or what should be. */
  fix: string;
  /** Screenshot ids that show it. */
  shots?: string[];
}

export const METHOD = `Ninety-eight screens were driven in a real browser as all four roles — requester,
approver, accounts and administrator — and photographed. While each screen was open it was also
measured: axe-core was run with best-practice rules enabled, the page was asked to scroll sideways at
1440, 1024 and 768 pixels, the heading outline was extracted, and every interactive target was sized.

That combination is deliberate. The product's own suite runs axe as the administrator only, filters
out the best-practice rules, checks two widths rather than three, and measures tap targets by height
alone — so the findings below are concentrated in the gaps that leaves rather than duplicating what
is already guarded.`;

export const FINDINGS: Finding[] = [
  // ------------------------------------------------------------------ open
  {
    id: 'legacy-role-column',
    title: 'The Add user panel cannot create a requester or an approver, and its innocuous option grants the payment role',
    severity: 'high',
    status: 'open',
    evidence: 'code',
    where: 'internal/app/templates.go:1347, internal/store/migrations.go:676-680, internal/app/templates.go:66',
    what: `Authorisation moved to real roles — the four seeded rows in "roles", joined through
      "user_roles" — but three places still read the superseded two-value "users.role" column.

      The Add user panel offers a single dropdown with exactly two choices, "Data entry" and "Admin"
      (templates.go:1347). Neither is the name of an actual role. The Edit panel, by contrast, has
      proper checkboxes for every role (templates.go:1320), so the two panels disagree about what a
      role even is.

      What "Data entry" does is decided by assignDefaultRoleTx (migrations.go:676-680), which reads:
      the role is Accounts unless the legacy value is exactly "admin". So choosing "Data entry"
      grants the Accounts role, which carries payment:create, payment:settle, payment:void,
      payment:hold and reservation:reserve.

      The same legacy column drives the caption under the signed-in person's name in the sidebar
      (templates.go:66). Every one of the 52 non-administrators in the current database has
      "data_entry" in that column, so managers, accounts staff and requesters are all captioned
      "Data entry" on every page they open.`,
    why: `An administrator adding a colleague has no way to make them a requester or an approver from
      that panel, and the option that sounds harmless is the one that hands over the ability to
      record and void payments. The person adding the account is not told this. Meanwhile the
      sidebar tells every user they are something they are not, which quietly undermines trust in
      the rest of the screen.`,
    fix: `Drive both the create panel and the sidebar caption from the real roles, as the edit panel
      already does. Until then, create the account and immediately open Edit to set the intended
      roles — and check the roles column on the people screen, which does show the true values.`,
    shots: ['admin/user-new-sheet', 'admin/user-edit-sheet', 'admin/users', 'manager/request-pending'],
  },
  {
    id: 'approver-cannot-see-attachments',
    title: 'An approver cannot open the documents attached to the request they are approving',
    severity: 'high',
    status: 'open',
    evidence: 'code',
    where: 'role_permissions (live database); internal/app/app.go:467-468',
    what: `The Manager role holds no attachment:view grant. The attachment download routes are gated
      on exactly that permission, so an approver following the invoice or receipt attached to a
      request receives a refusal. The same role also holds no payment:view, vendor:view or
      recoverable_report:view, so the payments ledger, the vendor master and the recoverables
      register are all closed to it.`,
    why: `Approving is the one moment in the whole workflow where somebody is asked to judge whether a
      payment is justified, and it is the moment the supporting document matters most. Asking for
      that judgement while withholding the evidence pushes the check outside the product, where it
      leaves no record.`,
    fix: `Decide whether this is intended. If approvers are meant to see what they approve, grant
      attachment:view to the Manager role — it is a data change, not a code change. If it is
      intended, the request screen should say so rather than simply showing nothing.`,
    shots: ['manager/request-pending', 'manager/payments-refused'],
  },
  {
    id: 'users-page-weight',
    title: 'The people screen grows with the square of the number of people',
    severity: 'medium',
    status: 'open',
    evidence: 'measured',
    where: '/users — 346 KB of markup at 56 people; /heads — 470 KB at 229 rows',
    what: `The users screen renders a complete edit panel for every person on the page
      (templates.go:1300), and inside each panel a full list of every possible approver
      (templates.go:1348). At 56 people that is 56 panels each holding 55 options — roughly three
      thousand option elements — and 346 KB of HTML before any of it is looked at. The expense heads
      screen reaches 470 KB the same way, by rendering every row's editor up front.`,
    why: `The cost is quadratic, so it worsens faster than the organisation grows: the same page in
      the QA fixture reached several megabytes. Everyone pays it on every visit, to render panels
      that are almost always closed.`,
    fix: `Load a person's edit panel when it is opened rather than rendering all of them in advance,
      or share one approver list between the panels instead of repeating it per row.`,
    shots: ['admin/users', 'admin/user-edit-sheet', 'admin/heads'],
  },
  {
    id: 'edit-control-ungated',
    title: 'The Edit control on the people screen is shown to anyone who can view the page',
    severity: 'low',
    status: 'open',
    evidence: 'code',
    where: 'internal/app/templates.go:1291 (control) vs internal/app/app.go:598 (route)',
    what: `Every other control on that screen is wrapped in a permission check; this one is not. The
      save is still properly gated — POST /users demands user:create or user:edit, and userSave
      re-checks the verb — so nothing can actually be changed without the grant. In the shipped
      configuration only administrators hold user:view, so no one currently sees a control they
      cannot use.`,
    why: `It is a latent inconsistency rather than a live hole: the moment a custom role is given
      user:view without user:edit — which the roles screen makes easy — that role gets a panel it
      can fill in and cannot submit.`,
    fix: `Wrap the control in the same permission check the other controls use.`,
    shots: ['admin/users', 'admin/user-edit-sheet'],
  },
  {
    id: 'unreachable-empty-copy',
    title: 'Several empty states cannot be reached, and one contradicts itself',
    severity: 'low',
    status: 'open',
    evidence: 'measured',
    where: 'Request and payment timelines; the dashboard; internal/app/http_errors.go:190 vs internal/app/app.go:2169',
    what: `Five "nothing has happened yet" timelines cannot occur: a request is given its first
      history entry in the same transaction that creates it, so the empty case is unreachable. The
      dashboard's "Nothing is waiting on you" was not reachable for any of the 56 accounts in the
      database. Separately, a locked month is explained by two different sentences depending on which
      code path refuses: "This month is locked and cannot be changed." and "This month is locked.
      Unlock it with a reason before changing budgets or payments." The second is more useful; the
      first appears more often.`,
    why: `Dead copy is a maintenance cost that looks like a feature — it gets translated, reviewed and
      kept current forever without ever being read. Two sentences for one condition make the product
      feel like two products.`,
    fix: `Delete the unreachable copy, and settle on the sentence that tells the reader what to do.`,
    shots: ['requester/requests-search-empty', 'accounts/payments-ledger-empty', 'admin/months-locked'],
  },
  {
    id: 'controls-that-cannot-work',
    title: 'Three controls are offered to people the product will then refuse',
    severity: 'medium',
    status: 'open',
    evidence: 'code',
    where: 'internal/app/templates.go:3476, :791, and the reservation routes',
    what: `The Reports screen renders an Export CSV button with no permission check
      (templates.go:3476), while the route behind it requires report:export — a grant the approver
      role does not hold. The Monthly plans link immediately beside it *is* guarded, so the omission
      is local to this one control rather than a general convention.

      On the payment queue's Paid tab, rows the reader settled themselves render a Resume control
      (templates.go:791) pointing at the payment form. The request is no longer approved, so the form
      answers a conflict.

      Accounts holds reservation:release but not reservation:reassign, so a claim held by a colleague
      can only be taken by an administrator — reasonable, but the queue does not say so.`,
    why: `A control that is visible is a promise. Discovering it was never yours to press teaches
      people to distrust the rest of the screen, and it is a worse experience than not seeing the
      control at all — they have already decided to do the thing before being told no.`,
    fix: `Guard the Export button on report:export the way the link beside it is guarded, and render
      the Paid tab's control as a link to the request rather than to the payment form.`,
    shots: ['manager/reports-monthly', 'accounts/queue-paid', 'accounts/conflict-already-paid'],
  },
  {
    id: 'recoverable-offered-then-refused',
    title: 'Three of the four request types offer a treatment they will always reject',
    severity: 'medium',
    status: 'open',
    evidence: 'code',
    where: 'internal/app/templates.go:1949 against internal/store/requests.go:305, :321, :334',
    what: `The treatment choice — **Budget expense** or **Refundable or recoverable** — is part of the
      shared top of the request form and renders identically whichever type was picked. The store
      accepts "recoverable" for an employee advance only: a vendor invoice, a vendor advance and a
      reimbursement are each refused with their own sentence, of the form "a vendor invoice is a
      budget expense".

      So on three types out of four, a requester who picks the second option, fills in the
      recoverable fields it reveals — category, expected return date, repayment terms — and submits,
      is told the choice was never available.`,
    why: `The refusal arrives after the work, not before it, and it contradicts a control the form
      presented as a genuine choice. The requester has no way to know which types accept it, because
      the form looks the same on all four.`,
    fix: `Render the recoverable option only on the types that accept it, or accept it on the types
      that offer it. Either is defensible; showing it everywhere and honouring it once is not.`,
    shots: ['requester/form-recoverable', 'requester/choose-type'],
  },
  {
    id: 'returned-request-has-no-edit-link',
    title: 'A returned request offers no way to reach the full edit form',
    severity: 'low',
    status: 'open',
    evidence: 'code',
    where: 'internal/app/templates.go — request_returned has no Edit control',
    what: `When an approver returns a request, its raiser is shown the request_returned screen, which
      carries the approver's reason and a correct-and-resubmit form. The Edit request button lives on
      the ordinary request_detail template, which this screen replaces — so /requests/{id}/edit
      remains permitted and functional but is reachable only by typing the address.`,
    why: `The correct-and-resubmit form covers the common case, but a requester who needs to change
      something outside it — the project, the head, the type — has no route to the screen that would
      let them, and no indication that one exists.`,
    fix: `Add the Edit control to the returned-request screen, or extend the inline form to cover the
      fields a returned request most often needs changed.`,
    shots: ['requester/detail-returned', 'requester/edit-form'],
  },
  {
    id: 'concern-promises-a-state-that-does-not-exist',
    title: 'The shortfall concern panel promises a status the product never sets',
    severity: 'low',
    status: 'open',
    evidence: 'code',
    where: 'internal/app/templates.go:1007',
    what: `The Raise a concern panel is subtitled "The request stays open as Partial — under
      discussion". No template ever renders that state: the badge continues to read "Partial —
      manager review" before and after. The stylesheet even carries a .pill.discussion rule for it,
      which nothing emits.`,
    why: `The approver is told their objection changes something visible, and it does not. The next
      person to look at the request sees exactly what was there before, so a raised concern is
      invisible to everyone except through the conversation thread.`,
    fix: `Either introduce the state the panel promises, or change the subtitle to describe what
      actually happens — the request stays where it is and the concern is recorded in the thread.`,
    shots: ['manager/partial-concern-sheet', 'manager/partial-review'],
  },
  {
    id: 'desktop-has-no-filter-drawer',
    title: 'Two panels exist only on narrow screens',
    severity: 'low',
    status: 'open',
    evidence: 'measured',
    where: 'web/static/fervid-ds.css — .m-filters and .tabbar are display:none above 860px',
    what: `The filter drawer on the requests list and the overflow navigation drawer are both reached
      from controls that are hidden above 860 pixels. On a desktop the equivalents are the inline
      filter bar and the sidebar, so nothing is missing — but the two drawers are unreachable and
      therefore unphotographable at desktop width.`,
    why: `Noted so the absence is understood as deliberate rather than as a gap in this review.`,
    fix: `No action. Recorded for completeness.`,
  },

  // ----------------------------------------------------------------- fixed
  {
    id: 'dashboard-empty-title',
    title: 'Every row on the Accounts and administrator home screens ended in a separator with nothing after it',
    severity: 'medium',
    status: 'fixed',
    evidence: 'measured',
    where: 'internal/app/templates.go:3434 and internal/store/store.go:1408',
    what: `The "Approved and unclaimed" panel renders each row as "{{.Number}} · {{.ShortTitle}}", but
      the query behind those rows selected twenty-two columns and short_title was not among them. The
      field was therefore empty on every row, and each one read "PR-2026-000036 ·" — a request number,
      a separator, and then nothing.`,
    why: `The home screen is the first thing both roles see, and the panel is meant to let somebody
      recognise work at a glance. A number alone does not do that: it is the title that says whether
      a request is a licence renewal or a material purchase. The dangling separator also reads as a
      rendering fault, which invites doubt about whatever else is on the screen.`,
    fix: `The query now selects short_title. The same rows read "PR-2026-000036 · Spare parts
      procurement". Found by an author writing the manual page for this screen, who noticed the
      screenshot did not match what the page was going to claim about it.`,
    shots: ['shared/dashboard-accounts', 'shared/dashboard-admin'],
  },
  {
    id: 'sidebar-gutter',
    title: 'Content sat flush against the sidebar on every screen',
    severity: 'high',
    status: 'fixed',
    evidence: 'measured',
    where: 'web/static/fervid-ds.css — 35 block types across all 98 screens',
    what: `The gap between the sidebar and the content was applied through an opt-in list of
      ".page > h1, .page > .panel, …" selectors. A ".page-inner" wrapper was later introduced between
      the two, and the list stopped matching anything. Only the two blocks carrying their own padding
      stayed indented, so a page's heading began 28 pixels in while every card, banner, table and
      form below it began at zero. Measuring every direct child across all 98 screens found 35 block
      types affected. The right-hand margin, meanwhile, was a leftover of the content width cap
      rather than a deliberate gutter, so the page was visibly lopsided.`,
    why: `It reads as carelessness on every screen at once, and the misalignment between a heading and
      the content beneath it is exactly the sort of thing that makes a product feel unfinished
      without the viewer being able to say why.`,
    fix: `The gutter now lives on the content column itself, so a new block cannot be forgotten —
      there is no list to add it to. The two full-bleed bands cancel it and re-apply it as padding,
      keeping their edge-to-edge background while their text lines up with everything below.`,
    shots: ['accounts/payment-form', 'accounts/payment-picker', 'manager/request-pending'],
  },
  {
    id: 'horizontal-overflow',
    title: 'Four screens scrolled sideways, for three different reasons',
    severity: 'medium',
    status: 'fixed',
    evidence: 'measured',
    where: '/roles, /payments/{id}/edit and the two role panels, at 1024px',
    what: `Three separate boxes could not shrink below their contents. The table wrapper set
      "width: 100%; overflow: auto" but left min-width at its automatic default, which as a grid item
      resolves to the table's full width — so it grew instead of scrolling. The payment form's three
      column templates used pixel floors summing to 766 pixels inside a 666-pixel column. And the
      permission matrix's visually-hidden radio was absolutely positioned with no positioned
      ancestor, so it resolved against the document, escaped the table's scrolling wrapper entirely
      and dragged the page 39 pixels sideways.`,
    why: `A page that scrolls sideways hides content behind an edge the reader has no reason to
      suspect, and on a trackpad it makes vertical scrolling feel like it is fighting back.`,
    fix: `All three now shrink properly. No screen scrolls sideways at 1440, 1024 or 768.`,
    shots: ['admin/roles', 'accounts/payment-edit'],
  },
  {
    id: 'pill-contrast',
    title: 'Closed-state badges failed the contrast threshold',
    severity: 'medium',
    status: 'fixed',
    evidence: 'measured',
    where: '.pill.rejected and .pill.cancelled — six screens',
    what: `Closed states were quietened with "opacity: .78" applied to the whole badge, which fades
      the text along with the frame. "Rejected — final" measured 3.2:1 against the card behind it
      where the standard asks for 4.5:1, and it is 11.5-pixel bold text — small, where legibility
      matters most. This single rule was the cause of every colour-contrast failure across the run.`,
    why: `The badge is the one part of a request row that carries its state. Fading the word to the
      point of illegibility removes the row's meaning for exactly the readers who need it clearest.`,
    fix: `The frame is faded instead of the text, which keeps the badge visually recessed and takes
      the label back over 5:1.`,
    shots: ['requester/requests-closed', 'manager/approvals-decided', 'requester/detail-rejected'],
  },
  {
    id: 'listbox-role',
    title: 'The payment picker announced itself as a listbox but contained buttons',
    severity: 'medium',
    status: 'fixed',
    evidence: 'measured',
    where: 'internal/app/templates.go:588 — flagged critical by axe',
    what: `The list of approved requests declared role="listbox", which promises a set of selectable
      option children. Its children are forms whose buttons claim a request, and plain links for
      anybody without the reservation grant — neither of which is an option.`,
    why: `Assistive technology was told to expect a control that does not exist, so the rows were
      liable to be announced as a broken selection widget rather than as the actions they are. This
      is the screen where money leaves the organisation.`,
    fix: `The container now declares role="group", which is what it actually is and keeps its
      accessible name. The keyboard handling reads a class selector rather than the role, so nothing
      depended on the old value. The same latent mistake remains in the vendor combo box, whose
      "Add a new vendor" link and no-results message sit inside a listbox without being options.`,
    shots: ['accounts/payment-picker'],
  },
  {
    id: 'login-lang',
    title: 'The sign-in page did not declare its language',
    severity: 'low',
    status: 'fixed',
    evidence: 'measured',
    where: 'internal/app/templates.go:93',
    what: `Every page rendered through the application shell carries lang="en". The sign-in screen is
      the one page built from its own standalone template, and it opened with a bare <html> tag.`,
    why: `It is the first page every user meets and the only one they meet before signing in. Without
      a language a screen reader falls back to the reader's own default voice, which mispronounces
      the form it is reading aloud.`,
    fix: `The sign-in template now declares lang="en" like every other page.`,
    shots: ['shared/login', 'shared/login-rejected'],
  },
  {
    id: 'empty-state-link',
    title: 'The one action in an empty state was distinguishable only by colour',
    severity: 'low',
    status: 'fixed',
    evidence: 'measured',
    where: '.empty — the ledger, and every other empty table',
    what: `Empty states put their single useful action inline in the sentence — "No payments match
      these filters. Record a payment" — where the link differed from the surrounding words by
      1.34:1. Colour-only differentiation needs at least 3:1, and there was no underline.`,
    why: `An empty screen is precisely where a reader most needs to be shown the way out, and this is
      the only thing on it to act upon.`,
    fix: `Links inside empty states are underlined, so the difference no longer depends on colour.`,
    shots: ['accounts/payments-ledger-empty'],
  },
];
