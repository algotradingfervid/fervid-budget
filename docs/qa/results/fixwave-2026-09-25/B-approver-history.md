# Fix wave 2026-09-25 — cluster B: approver and history

Seven confirmed defects from the 2026-09-25 skeptic pass. Each was reproduced
through the real HTTP routes in `internal/app/approver_history_test.go` before
it was fixed, and every fixed workflow was then driven from the browser
(desktop 1440px and phone 390px) with a Playwright script against a freshly
seeded server; 29/29 browser checks passed.

Owner decisions applied: reassignment is for the request's own approver or a
holder of an explicit admin-level grant, nobody may reassign to themselves;
reassign must work for every status the deactivation warning lists; history
rows name people, never ids. The admin-level grant chosen is `user:edit` — the
grant that deactivates a person is the grant that rescues what they leave
behind (F-G-025) — rather than a new pair in the 66-pair vocabulary, which
would have needed a migration to reach the Admin role on existing databases.

| Id | Status | How it was fixed | Tests |
|----|--------|------------------|-------|
| rbac-8 | Fixed | `requestReassignApprover` now asks `mayReassignApprover` (`internal/app/requests.go`): the caller must hold `approval:reassign` and be the request's current approver, or hold `user:edit`; anybody else gets 403. `store.ReassignRequest` refuses `newManagerID == actor.ID` ("you cannot reassign a request to yourself") whoever the actor is. The detail page computes `CanReassignApprover` once and gates both the button and the sheet on it, and the sheet disables the reader's own name as well as the current approver's. A Manager who is not the approver sees no control and cannot approve either. | `TestOnlyTheCurrentApproverOrAnAdministratorMayReassignAnApproval`; `TestApproverReassignmentRoute` updated (it had the administrator reassign the request to themselves — now to a third approver, because self-assignment is the defect) |
| rbac-9 | Fixed | `store.RequestThread` resolves the ids a `manager_id` change carries into user names (`nameUsersInChanges`), so the change chip reads "Approver Mona Manager → Max Manager" for every reader. | `TestHistoryNamesTheApproversAChangeMovedBetween` |
| rbac-10 | Fixed | The edit form's banner and submit button carry `data-follows-select="apr"` and a `data-follows-text` template; a small progressive enhancement in `web/static/fervid-app.js` (`initFollowSelects`) re-reads them from the selected approver on every change. Server-rendered text is unchanged for the stored approver. | `TestEditFormLabelsFollowTheChosenApprover` (markup); browser check at 1440px and 390px ("Save and notify Mona Manager" → "Save and notify Max Manager", "Editing tells Max Manager again") |
| deactivation-2 | Fixed | `store.ReassignRequest` accepts every status `RequestsAwaitingApprover` warns about (`reassignableStatuses`: pending, returned, cancellation_requested, partial_review) and the UPDATE matches the row's own status; `decision_reason` is only overwritten while pending so a returned request keeps the correction the requester is reading. `store.Reassignable` gates the control. The warning copy now names the control it means ("Reassign approval"). The new approver can then decide the cancellation or the partial. | `TestReassignmentRescuesEveryStatusTheDeactivationWarningLists` |
| history-1 | Fixed | The eight payment-side `payment_request` audit summaries in `internal/store/store.go` (process, release, reservation reassign, settle, mark_partial, accept_partial, hold, unhold) now start with the actor's name like the request-side ones, e.g. "Aarav Accounts lifted the hold". The partial-review trail already strips a leading actor name (`trailBody`), so it does not double up. | `TestDetailThreadNamesTheActorForAccountsEvents`; `TestTrailBodyDropsTheActorNameTheHeadAlreadyCarries` gained a case |
| copy-1 | Fixed | `statusPhrase` (`internal/store/requests.go`) names a status with its article and in plain words; all eight transition refusals use it ("an approved request cannot be returned", "a completed (partial accepted) request cannot be returned", "a request with Accounts cannot be withdrawn"). | `TestIllegalTransitionRefusalsReadAsSentences` |
| copy-2 | Fixed | Same change: "an approved request cannot be withdrawn". TC-B-071's substring assertion still holds. | `TestIllegalTransitionRefusalsReadAsSentences` |
| hold-1 | Fixed | New route `POST /requests/{id}/attachments` (`attachment:create`) → `requestAttachmentUpload`, the door the Phase-2 route table planned. It accepts only the request's own requester, on any request that is not closed (rejected, withdrawn, cancelled, completed, completed_partial), stages the file the way the request form does and writes it through `store.AddRequestAttachment` — no field on the request changes. The hold banner offers "Add a document" to the requester. | `TestRequesterCanAddADocumentWhileTheRequestIsOnHold` |

## Review repairs

The reviewer raised one blocking and four minor issues against the fix above.
All five were repaired; each was reproduced by a failing test first, and every
repaired workflow was driven from the browser again (desktop 1440px and phone
390px) against a freshly seeded server on :4900; 23/23 browser checks passed.

| Id | Issue | Repair | Tests |
|----|-------|--------|-------|
| history-1 (blocking) | TC-G-063 filtered the audit rows on `summary.includes('Reserved')` and the actor-named summary is lower-case, so `audit-g-information-flow` went 1 failed / 55 passed. | The assertion now matches `'reserved the request for processing'` — the new wording in full, so a regression to the bare "Reserved request for processing" is caught rather than let through. Nothing else in the test changed. | `tests/e2e/audit-g-information-flow.spec.ts` TC-G-063, whole suite green |
| hold-1 (minor) | `POST /requests/{id}/attachments` accepted a document in any non-closed status, while the design and the screen grant it on hold only; the status was also read outside the store transaction. | The handler gates on `req.OnHold` (400 "This request is not on hold, so no document can be added to it", pointing a pending or returned request at Edit) and calls the new `store.AddHeldRequestAttachment`, which re-reads the row inside the write transaction and refuses unless it is the actor's own and on hold at that moment — so a hold lifted between the page load and the upload refuses too. `closedStatuses` is gone; `AddRequestAttachment` (the edit path's door) is unchanged. | `TestRequesterCanAddADocumentWhileTheRequestIsOnHold` gained the approved-not-held and pending cases (control absent, POST 400, nothing written); `TestHeldRequestAttachmentIsOpenExactlyAsWideAsTheHold` (store: pending, approved, held, stranger, lifted) |
| rbac-8 (minor) | `mayReassignApprover` ran against a request read before the write transaction, and `ReassignRequest`'s UPDATE matched on status only, so a former approver racing an administrator could still move the request. | `ReassignRequest` takes `asAdministrator bool`. Without it the store refuses inside the transaction unless the row's `manager_id` is the actor (403 "this request is with … now, so it is not yours to reassign"), and the UPDATE also matches `manager_id`. The route passes `a.auth.Can(u, "user", "edit")` — the same grant `mayReassignApprover` honours. | `TestReassignmentIsRefusedToAnApproverTheRequestHasAlreadyLeft` (store); `TestReassignAndReraise` and the F-A-08 test updated for the signature |
| deactivation-2 (minor) | The `approval_reassigned` notice said "{{number}} needs your approval — it was reassigned to you" for a returned, frozen or partial-review request too. | The seed now reads "{{number}} was reassigned to you as its approver", with a body that says what that can mean (the approval, a cancellation request, a partial-settlement review, or a returned request coming back on resubmission). Migration v13 (`UpReassignedNotificationWording`) carries it onto an installed database, and only where the row still reads exactly as v11 wrote it, so an administrator's own wording is never overwritten. | `TestMigrationV13RewordsTheReassignmentNoticeWithoutTouchingAnAdminsEdit`; browser: Mona's notice for a frozen request and Max's for a returned one both read the new subject, and the admin sheet shows it as the seeded template |
| deactivation-2 manual (minor) | The manual described the old rules and quoted the old wording. | `manager/reassigning.mts` (who may reassign, the statuses the control appears in, the refusals, the two FAQ answers, the self-assignment rule), `reference/error-messages.mts` (the three reassign refusals), `getting-started/a-request-in-detail.mts`, `reference/request-lifecycle.mts` and `requester/getting-paid.mts` (actor-named history lines) were rewritten and `docs/manual` rebuilt with `node tests/manual/build/build.mts` from the committed shots. | Build: 59 pages, 98/98 shots shown |

Browser evidence (scratchpad `fixwave-b2/shots`): `history1-audit-screen`,
`history1-thread`, `hold1-approved-not-held-{desktop,phone}`,
`hold1-refusal-not-held`, `hold1-held-upload-works`, `hold1-after-lift`,
`rbac8-stale-approver-refused{,-phone}`, `rbac8-current-approver-hands-on`,
`deact2-notice-frozen{,-phone}`, `deact2-frozen-request`,
`deact2-notice-returned`, `deact2-admin-notifications`.

## Notes

- From a browser, a stale reassignment meets the route's own refusal ("Only the
  approver this request was sent to, or an administrator, can reassign it"),
  because the route re-reads the request on the POST. The store's
  in-transaction check is what closes the window between that read and the
  write; it is proven by the store test, since the race cannot be staged from
  the UI.
- The manual's screenshots were not re-taken: `make manual` photographs the
  developer's 582-request dataset on :8080, which is not available here. The
  four shots that show the old "Reserved request for processing" line will
  refresh with the next capture run; the prose already describes the new one.
- The thread's "Attachment added" row names the file but not the uploader; it
  never showed an id, so it is outside this cluster.
