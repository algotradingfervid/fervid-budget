# Payment Requests — Coverage Matrix (audited)

Every atomic requirement from the approved design (`docs/payment-requests-design.html`), bound to the phase, plan task, and the named test that proves it. Each row cites the test that exercises it; negative (X*) IDs cite a proof-of-absence assertion.

**"92 / 92 VERIFIED" was wrong and is retracted.** The 2026-07-27 audit (`docs/qa/results/AUDIT-REPORT.md`) found the verification column, not the requirement IDs, unsound: **A7** cited a store-level test (`TestReassignAndReraise`) that never touched an HTTP route, for a feature that had no route at all (`F-A-06`/`F-C-02`); **L1** still described a "Draft (private)" state that a prior build decision (D1) had already removed, so the row's own citation could not verify what its text said. Both are corrected below, individually, with the commit that settled each and the test that now proves it. Nothing else in the matrix has been re-audited — see `docs/qa/results/REPAIR-LOG.md` for what has and hasn't been re-verified since `30edd6a`.

**Citation drift beyond A7/L1.** The audit's own note said "roughly six" cited test names could not be found (`docs/qa/results/AUDIT-REPORT.md:283`) — that turns out to be at least the six rows (T5, T8, T11, L1, C2, C3) that all cited one renamed test, `TestCreateRequestDraftAssignsNumberAndForcesPayee`. A direct `grep` for every `Test*` name in this file against the current test suite turned up 18 distinct names that did not exist under those spellings. A second pass traced all but two of them to a current test verifying the same requirement — most were simple renames as the product changed underneath them (draft removal, migration renumbering as the chain grew to v10, the reservation/settlement tests picking up more specific names). Corrected in place, each with the `file:line` read to confirm it still proves the row: T1, T3, T4, A6, D3, D5 (`TestRequesterSubmitsAndCannotSeeOthers` had been split into several purpose-named tests), T5, T8, T11, C2, C3 (`TestCreateRequestDraftAssignsNumberAndForcesPayee`, per L1's note), Q4, S6, S7, S1, S14, X6, N1, N3, N4, N5, S8, N6, N8, V1, V5, L2, S5. **V6 and A8's notify-half remain genuinely unresolved** — no current test could be found that verifies them and neither should be assumed covered; see those rows.

Legend: **P1** RBAC · **P2** Requests · **P3** Linking/Settlement · **P4** Recoverables · **P5** Notifications. `T#` = plan task number.

## Roles & permissions
| ID | Requirement | Verified by |
|---|---|---|
| R1 | Admin-managed per-screen permissions, not hard-coded | P1·T4/9/10 `TestUpdateRolePermissionsPersistsAndValidates` |
| R2 | Granular actions per resource | P1·T3/4 `TestPermissionVocabularyIsCanonical` |
| R3 | Data scope per resource (own/assigned/all) | P1·T4/7 `TestEffectivePermissionsUnionAndBroadestScope` |
| R4 | Multiple roles per user; access = union | P1·T6/7/11 `TestEffectivePermissionsUnionAndBroadestScope` |
| R5 | Create role by copying | P1·T5/10 `TestCopyRoleDuplicatesGrantsAndScopes` |
| R6 | Permissions govern data server-side (URL blocked) | P1·T9/12 `TestRequesterOnlySessionForbiddenFromAdminRoutesByURL` |
| R7 | 4 starter roles seeded; Admin combines | P1·T8(v1 seed) `TestSeedSystemRolesMatchDefaults` |
| R8 | Separate permission sets across all resources | P1·T3/8 `TestPermissionVocabularyIsCanonical` |
| R9 | Delete non-system; system roles protected | P1·T3/8 `TestDeleteRoleRejectsSystemRole` |

## Request form & types
| ID | Requirement | Verified by |
|---|---|---|
| T1 | Treatment first, then type | P2·T3/13 `TestValidateRequestInputPerType` + `TestRequestNewTypeChooser` (renamed from `TestRequesterSubmitsAndCannotSeeOthers`, which no longer exists — the type-chooser assertions moved to their own test; verified at `internal/app/requests_test.go:323-360`) |
| T2 | Type decides required fields + payee | P2·T3/4 `TestValidateRequestInputPerType` |
| T3 | Bank details excluded from form | P2·T13 `TestRequestFormIsAdaptiveAndTypeIsNeverAControl` (renamed from `TestRequesterSubmitsAndCannotSeeOthers`; the test's own comment tags this assertion "T3" — `internal/app/requests_test.go:378-383`) |
| T4 | Mobile-first form | P2·T13 `TestRequestFormIsAdaptiveAndTypeIsNeverAControl` (same rename as T3; asserts the adaptive single-form-per-type structure, `internal/app/requests_test.go:369-428` — no separate CSS-layout assertion found under this name, so the "mobile-first" half of this row is verified structurally, not visually) |
| T5 | Always-captured incl. urgent flag | P2·T4 `TestCreateRequestIsAtomicCreateAndSubmit` (urgent round-trips — renamed from `TestCreateRequestDraftAssignsNumberAndForcesPayee` when D1 removed drafts; verified at `internal/store/requests_test.go:300-303`) |
| T6 | Vendor-invoice fields | P2·T3 `TestValidateRequestInputPerType` |
| T7 | Vendor-advance fields | P2·T3 `TestValidateRequestInputPerType` |
| T8 | Reimbursement fields (payee=self) | P2·T3/4 `TestCreateRequestIsAtomicCreateAndSubmit` (forced payee verified at `internal/store/requests_test.go:294-296`; see T5 note on the rename) |
| T9 | Employee advance refundable→recoverable / non-refundable→budget | P2·T3 `TestValidateRequestInputPerType` (the `employee_advance` cases; `TestValidateRecoverable`, the second citation, no longer exists — see V1's note, same replacement) |
| T10 | Attachments optional now; admin toggle to require | P2·T4A/5 `TestAppSettingRoundTripAndDefault` + P5·T8 `TestConfigurationScreenReadsAndWritesAppSettings` (toggle persists, `internal/app/requests_test.go:1328-1433`) + `TestCompulsoryAttachmentsAskForAReasonRatherThanBlocking` (drives submit enforcement, `internal/app/requests_test.go:466-486`) — both renamed from `TestRequireAttachmentsTogglePersistsAndDrivesSubmitEnforcement`, which no longer exists |
| T11 | Reimbursement/personal-advance payee = logged-in employee | P2·T4 `TestCreateRequestIsAtomicCreateAndSubmit` (same rename as T5/T8) |
| T12 | Inactive projects/heads hidden from new, shown on historical | P2·T4 `TestRequestRetainsHistoricalProjectHead` |

## Approval & manager
| ID | Requirement | Verified by |
|---|---|---|
| A1 | Requester picks manager | P2·T3 `TestValidateRequestInputPerType` (manager_id required) |
| A2 | Approve; may adjust amount | P2·T8/15 `TestApproveRequestAdjustsAmountAndIsAssignedOnly` |
| A3 | Reject with required reason | P2·T9 `TestReturnAndRejectRequireTextAndAreAssignedOnly` |
| A4 | Return with required comments | P2·T9 `TestReturnAndRejectRequireTextAndAreAssignedOnly` |
| A5 | Managers see all; approve assigned | P2·T8/12 `TestListRequestsByScopeAndCount` |
| A6 | No bulk approval | P2·T13 `TestNoBulkApproveOrCopyEndpointExists` (renamed from `TestRequesterSubmitsAndCannotSeeOthers`; verified at `internal/app/requests_test.go:1508-1523`) |
| A7 | Admin reassign (reason + history) | **Not verified at `30edd6a`** — `TestReassignAndReraise` is a store-level test of `ReassignRequest`; no HTTP route called it, so the requirement had no way to be exercised at all (`F-A-06`/`F-C-02`, `docs/qa/results/AUDIT-REPORT.md:213`). **Fixed in Wave 3, commit `1fac147`:** `POST /requests/{id}/reassign-approver` (`internal/app/app.go:569`, handler `requestReassignApprover` at `app.go:1615-1647`), gated on `approval:reassign`, fields `manager_id` + `reason`, over the same `store.ReassignRequest`. Verified end-to-end by `TestApproverReassignmentRoute` (`internal/app/app_integration_test.go:2191`), which drives the real route and confirms the refusal, the mandatory reason, the mandatory-approver-can-approve check, and the `approval_reassign` audit row. **Verified as of `1fac147`.** |
| A8 | Edit-while-pending: history + re-notify + reset + reroute | P2·T6 `TestUpdateRequestPendingReroutesAndResetsReminder` (history/reset/reroute, exists and verified) + **`UNVERIFIED`** for the re-notify half — `TestNotifyRequestEditedEmailsManager` no longer exists, and no equivalent test invoking `EventRequestEdited` (`internal/notify/events.go:9`) end-to-end was found in `internal/notify/service_test.go`. The event constant exists; whether anything still fires it on an edit was not re-checked here. |

## Requester actions
| ID | Requirement | Verified by |
|---|---|---|
| Q1 | Edit & resubmit sent-back | P2·T6/9 `TestReturnAndRejectRequireTextAndAreAssignedOnly` |
| Q2 | Withdraw pending | P2·T7 `TestWithdrawRequestFromPending` |
| Q3 | Re-raise rejected | P2·T10 `TestReassignAndReraise` |
| Q4 | See payment outcome | P3·T2/11 `TestSettlementFlowCompletesAndShowsPaymentDetail` (renamed from `TestSettlementFlowCompletesAndShowsOutcome`; verified at `internal/app/linking_test.go:560`) |
| Q5 | Requester sees only own | P2·T12/13 `TestListRequestsByScopeAndCount` + `TestScopeNarrowsFromTheURLButNeverWidens` (renamed from `TestRequesterSubmitsAndCannotSeeOthers`; verified at `internal/app/requests_test.go:231-259`) |
| Q6 | Add clarification while On hold, no field change | P3·T8 `TestHoldUnholdPreserveApprovedFieldsAndAllowComments` |

## Lifecycle & states
| ID | Requirement | Verified by |
|---|---|---|
| L1 | ~~Draft (private)~~ **No draft state exists** | **Row description was stale, not the test.** Decision **D1** (`docs/superpowers/specs/2026-07-25-design-system-adoption-spec.md:47-48`) removed drafts before the audit ran: `CreateRequest`/`SubmitRequest` collapsed into one atomic operation that creates a request already `pending`, and `status` carries `CHECK (status <> 'draft')` (`internal/store/migrations.go:159`). D1 itself said this row "changes meaning… and becomes a proof-of-absence test," but the matrix text was never updated to match, and the cited test, `TestCreateRequestDraftAssignsNumberAndForcesPayee`, does not exist in the repo — a second, independent problem the audit flagged (`docs/qa/results/AUDIT-REPORT.md:283`: "roughly six of the cited test names do not exist"). **Verified by `TestNoDraftStateExists`** (`internal/store/requests_test.go:315-341`): `draft` is absent from the status enum and from `canTransition`, a direct `INSERT … status='draft'` is rejected by the CHECK constraint, and a freshly created request is immediately numbered and `pending`. |
| L2 | Pending approval | P2·T5 `TestCreateRequestIsAtomicCreateAndSubmit` (renamed from `TestSubmitRequestTransitionsAndRespectsAttachmentFlag`, which predates D1's create-and-submit merge; asserts `status == "pending"` immediately on create, `internal/store/requests_test.go:284-286`) |
| L3 | Returned | P2·T9 `TestReturnAndRejectRequireTextAndAreAssignedOnly` |
| L4 | Rejected — final, read-only | P2·T9 `TestReturnAndRejectRequireTextAndAreAssignedOnly` |
| L5 | Withdrawn | P2·T7 `TestWithdrawRequestFromPending` |
| L6 | Approved | P2·T8 `TestApproveRequestAdjustsAmountAndIsAssignedOnly` |
| L7 | On hold (only Accounts lifts) | P3·T8/12 `TestHoldUnholdPreserveApprovedFieldsAndAllowComments` |
| L8 | Processing (reserved) | P3·T3 `TestReserveRequestMovesApprovedToProcessing` |
| L9 | Partial — manager review | P3·T5 `TestRecordPaymentPartialNeedsReasonAndRoutesToReview` |
| L10 | Completed; drops out of link list | P3·T5/9 `TestRecordPaymentSettledCompletesEvenWhenUnderApproved` |
| L11 | Legal transition enforcement | P2·T3 `TestCanTransition` + P3·T3/5/7 `TestAcceptPartialAndRaiseConcern` |

## Linking, processing & settlement
| ID | Requirement | Verified by |
|---|---|---|
| S1 | Both entry points | P3·T10/13 `TestReservationEntryPointReservesAndRedirects` (queue entry point, renamed from `TestReservationEntryPointAndPrefill`; verified at `internal/app/linking_test.go:105-120`) + `TestSeededAccountsRoleTakesARequestForProcessing` (picker entry point, `internal/app/linking_test.go:726`) |
| S2 | Atomic reservation → Processing (either path) | P3·T3/10 `TestReserveRequestMovesApprovedToProcessing` |
| S3 | Dropdown lists only approved·unclaimed·not-on-hold | P3·T9 `TestLinkablePaymentRequestsFilterAndSearch` |
| S4 | Dropdown search (no/requester/payee/project/head/amount) | P3·T9 `TestLinkablePaymentRequestsFilterAndSearch` |
| S5 | Concurrency: second caller rejected | P3·T3/10 `TestReserveRequestIsAtomicUnderConcurrency` (-race, `internal/store/linking_test.go:231`) — `TestReserveRejectsSecondCallerWithJustTaken` no longer exists as a separate test; the second-caller-rejected assertion now lives inside the concurrency test above rather than its own case |
| S6 | No auto-release; resume/release; authorized reassign | P3·T4/12 `TestReleaseRequestRequiresConfirmReasonAndAuthority` (renamed from `TestReleaseRequestRequiresConfirmAndAuthority`; verified at `internal/store/linking_test.go:287`) |
| S7 | Cancel/switch releases only after explicit confirm | P3·T4/12 `TestReleaseRequestRequiresConfirmReasonAndAuthority` (same rename as S6) |
| S8 | Stale-Processing reminder after 1 elapsed day | P5·T4/7 `TestRunRemindersNotifiesTheAssignedAccountant` (renamed from `TestRunRemindersNudgesStaleProcessing`; verified at `internal/notify/reminders_test.go:57-95`. "Calendar day" is corrected to "elapsed day" — `calendarDaysBetween` was deleted per decision 2 in `docs/qa/results/REPAIR-LOG.md:25`, finding F-F-08: the thresholds were always elapsed-day arithmetic, and the copy now says so) |
| S9 | One request → one payment | P3·T1/5 `TestPaymentRequestIndexRejectsDuplicateLinkButAllowsNullHistoricals` |
| S10 | "Payment settled" → Completed even if paid < approved | P3·T5/11 `TestRecordPaymentSettledCompletesEvenWhenUnderApproved` |
| S11 | "Partial settlement" → review → accept / raise-concern | P3·T7/11/12 `TestAcceptPartialAndRaiseConcern` |
| S12 | Recorded payment immutable | P3·T6/11 `TestLinkedPaymentIsImmutable` |
| S13 | Payment saves only after popup confirmed | P3·T5 `TestRecordPaymentPartialNeedsReasonAndRoutesToReview` |
| S14 | Prefill payment from request | P3·T10 `TestReservationEntryPointReservesAndRedirects` (renamed from `TestReservationEntryPointAndPrefill`; the redirect target `/payments/new?request={id}` carries the prefill parameter, `internal/app/linking_test.go:105-120` — unblocked by `ProcessingBy` in select/scan) |
| S15 | Every future payment must link to approved request | P3·T5/10 `TestRecordPaymentRequiresActorReservation` + `TestPaymentCreateRequiresReservedRequest` |

## Recoverables
| ID | Requirement | Verified by |
|---|---|---|
| V1 | Budget vs recoverable classification | P4·T3 `TestValidateRequestInputPerType` (renamed from `TestCreateRecoverableRequestEnforcesCategoryRules`, which no longer exists — the "employee_advance budget needs head" / recoverable-vs-budget cases now live in this table-driven test, `internal/store/requests_test.go:99-198`) |
| V2 | Excluded from budget actuals | P4·T4 `TestGridAndReportExcludeRecoverablePayments` |
| V3 | Separate recoverable report/total | P4·T5/6 `TestRecoverablePaymentExcludedFromActualsButInRecoverableReport` |
| V4 | Admin-configurable categories | P4·T1/2/7 `TestRecoverableCategoryCRUD` |
| V5 | Category rules (EMD/PBG→project; ICD→counterparty; emp-adv auto) | P4·T3 `TestSeededCategoriesMatchPhase2Rules` (renamed from `TestValidateRecoverable`; checks every seeded category's `requires_project`/`requires_counterparty` against `recoverableCategoryRules`, `internal/store/recoverables_test.go:64-94`) |
| V6 | Records counterparty/employee, return date, notes | **`UNVERIFIED`.** `TestValidateRecoverable` no longer exists and no equivalent test asserting counterparty/employee/return-date/notes are recorded on create was found in `internal/store/recoverables_test.go` or `requests_test.go`. `TestAdminAddedCategoryRulesAreEnforced` (`internal/store/recoverables_test.go:359-393`) exercises these fields incidentally but proves category enforcement, not that the fields round-trip in general. Needs a real re-check, not a citation swap. |
| V7 | Closes on payment; retains classification+return info | P4·T8 `TestRecoverableRequestClosesOnPaymentRetainingClassification` |
| V8 | Recoverable may link to a real project | P4·T5 `TestRecoverableReportShowsLinkedProject` |

## Notifications, reminders & conversation
| ID | Requirement | Verified by |
|---|---|---|
| N1 | In-app by default (queues) for every event | P2·T14 `TestDashboardShowsGatedWorkAreasWithCounts` + P5·T6 `TestNotifyDisabledEventStillWritesInAppButSendsNoEmail` (renamed from `TestNotifyDisabledEventSendsNoEmail`; verified at `internal/notify/service_test.go:153-182`) |
| N2 | Email per-event configurable | P5·T3/8 `TestNotificationSettingUpsertAndFetch` |
| N3 | Approval email → Accounts + requester + management | P5·T6 `TestNotifyResolvesAccountsGroup` (renamed from `TestNotifyApprovedResolvesAccountsRequesterAndManagement`; verified at `internal/notify/service_test.go:214-248` — asserts the email reaches the requester and Accounts; the "management" leg is not independently asserted in this test, so treat that half as reduced confidence, not re-verified from scratch) |
| N4 | Daily manager reminder after 3 elapsed days | P5·T7 `TestRunRemindersFiresOnceThenRespectsTheCadence` (renamed from `TestRunRemindersEmailsManagerAfterThreeCalendarDays`; verified at `internal/notify/reminders_test.go:12-56`. "Calendar days" corrected to "elapsed days" — see the S8 note, same fix, F-F-08) |
| N5 | Stale-processing nudge after 1 elapsed day | P5·T7 `TestRunRemindersNotifiesTheAssignedAccountant` (renamed from `TestRunRemindersNudgesStaleProcessing`; same test as S8, `internal/notify/reminders_test.go:57-95`) |
| N6 | Urgent pre/post emails; no authority | P5·T6 `TestResolveRecipientsForUrgentDependsOnStatus` (renamed from `TestNotifyUrgentSubmitEmailsManagerAndConfersNoApprovalAuthority`; verified at `internal/notify/service_test.go:58-82`) |
| N7 | Conversation thread visible to viewers | P2·T11 `TestRequestCommentsAndAttachments` |
| N8 | SMTP + management configurable; password env-only | P5·T1/3/5/8 `TestSMTPPasswordLoadsFromEnvOnly` + `TestMailSettingsRoundTripAndNeverStoreAPassword` (renamed from `TestAppSettingsRoundTripAndNeverStoresPassword`; verified at `internal/store/notifications_test.go:10`) |

## Dashboard & UX
| ID | Requirement | Verified by |
|---|---|---|
| D1 | Unified dashboard with work areas | P2·T14 `TestDashboardShowsGatedWorkAreasWithCounts` |
| D2 | Only permitted areas appear | P1·T12 `TestRequesterOnlySessionForbiddenFromAdminRoutesByURL` + P2·T14 `TestDashboardShowsGatedWorkAreasWithCounts` |
| D3 | Export lists/reports by permission | P2·T13 `TestRequestExportIsScopedAndCarriesThePhase2Columns` (renamed from `TestRequesterSubmitsAndCannotSeeOthers`; verified at `internal/app/requests_test.go:286-322`) |
| D4 | Queues show counts | P2·T14 `TestDashboardShowsGatedWorkAreasWithCounts` |
| D5 | Copy-previous-request NOT built | P2·T13 `TestNoBulkApproveOrCopyEndpointExists` (renamed from `TestRequesterSubmitsAndCannotSeeOthers`; same test proves A6, `internal/app/requests_test.go:1508-1523`) |

## Scope boundaries (proof-of-absence)
| ID | Requirement | Verified by |
|---|---|---|
| X1 | No tax/TDS calculation | P2 `TestNoTaxColumnsInSchema` (no tax/tds column, table, or route) |
| X2 | No recoverable repayment tracking | P4·T9 `TestNoRecoverableRepaymentTracking` |
| X3 | No refund/return-of-money | P3·T14 `TestNoRefundRoute` (route 404 + no store method) |
| X4 | No forfeiture/write-off | P4·T9 `TestNoForfeitureRoute` (route 404 + no store method) |
| X5 | No direct request-less payment path | P3·T5/10 `TestPaymentCreateRequiresReservedRequest` |
| X6 | Historical payments unchanged (request_id NULL) | P3·T1/6 `TestMigrationV4IsIdempotentAndLeavesHistoricalPaymentsUntouched` (renamed from `TestMigrationV3…` when an unplanned vendor-master migration was inserted as v2, per `docs/qa/uml/01-domain-model.md` DV1; verified at `internal/store/linking_test.go:136`) |

## Cross-cutting
| ID | Requirement | Verified by |
|---|---|---|
| C1 | Versioned migration runner | P1·T1/2 `TestMigrateAppliesAndIsIdempotent` |
| C2 | Audit history for request mutations | P2·T4/10 `TestCreateRequestIsAtomicCreateAndSubmit` (entity `payment_request`, verified at `internal/store/requests_test.go:308-311`; see T5 note on the rename) |
| C3 | Currency INR paise throughout | P2·T4 `TestCreateRequestIsAtomicCreateAndSubmit` (50000 paise → ₹500.00, verified at `internal/store/requests_test.go:304-307`) |
| C4 | Request number unique, monotonic per year | P2·T1/2 `TestNextRequestNumberIsMonotonicPerYear` |

---

**Original audit result, "92 / 92 VERIFIED," is retracted — see the header note and the corrected A7/L1 rows above.** Everything else below this line is the unaudited original text of this footer, kept for its structural history and not re-verified as part of the 2026-07-27 audit: single monotonic migration sequence v1–v5 (**stale — migrations run v1–v10 as of this writing, and may grow further as repair waves land; do not hardcode an upper bound** — v8 `payments.head_id` nullable, v9 nine more notification events, v10 `payments.vendor_id`, all three from the audit repair. Cited by version rather than by line: the authority is the header roster and the `migrations` slice in `internal/store/migrations.go`, which is still being edited, so any line number written here would be stale within the day. See `docs/qa/results/REPAIR-LOG.md`); `Request` gains `ProcessingBy`/`ProcessingAt`/`ReminderLastSent`/`SubmittedAt` with matching select/scan; unified `.Perms.Can` nav mechanism; `Request2` PageData field across P2/P3; `app_settings` owned by Phase 2 and extended by Phase 5; `recoverable_categories` FK kept as SQLite-deferred; proof-of-absence tests added for X1/X3/X4; `ReleaseRequest(…,confirmed,authorized bool)` aligned across overview and Phase 3. Plan-defect cleanups: no broken-then-corrected snippets, no forward-referenced symbols, no unused scaffolding.
