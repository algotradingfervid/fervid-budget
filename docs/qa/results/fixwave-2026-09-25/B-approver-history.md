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

## Notes

- The seeded `approval_reassigned` notification subject still says "needs your
  approval"; for a reassigned cancellation or partial review it is really a
  decision. The templates live in the database (migration seed), so they were
  left alone.
- The thread's "Attachment added" row names the file but not the uploader; it
  never showed an id, so it is outside this cluster.
- `docs/manual` and `tests/manual/build/content` quote the old audit lines
  ("Reserved request for processing", "Hold lifted") and the old reassign
  refusal ("only a pending request can be reassigned"); the manual is rebuilt
  by `make manual` and was not touched here.
