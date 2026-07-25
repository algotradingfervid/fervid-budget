# Payment Requests — Coverage Matrix (audited)

Every atomic requirement from the approved design (`docs/payment-requests-design.html`), bound to the phase, plan task, and the named test that proves it. **Status after the adversarial audit + remediation pass: 92 / 92 VERIFIED.** Each row cites the test that exercises it; negative (X*) IDs cite a proof-of-absence assertion.

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
| T1 | Treatment first, then type | P2·T3/13 `TestValidateRequestInputPerType` + `TestRequesterSubmitsAndCannotSeeOthers` (treatment renders before type) |
| T2 | Type decides required fields + payee | P2·T3/4 `TestValidateRequestInputPerType` |
| T3 | Bank details excluded from form | P2·T13 `TestRequesterSubmitsAndCannotSeeOthers` (no bank/ifsc fields) |
| T4 | Mobile-first form | P2·T13 `TestRequesterSubmitsAndCannotSeeOthers` (single-column `request-form`) |
| T5 | Always-captured incl. urgent flag | P2·T4 `TestCreateRequestDraftAssignsNumberAndForcesPayee` (urgent round-trips) |
| T6 | Vendor-invoice fields | P2·T3 `TestValidateRequestInputPerType` |
| T7 | Vendor-advance fields | P2·T3 `TestValidateRequestInputPerType` |
| T8 | Reimbursement fields (payee=self) | P2·T3/4 `TestCreateRequestDraftAssignsNumberAndForcesPayee` |
| T9 | Employee advance refundable→recoverable / non-refundable→budget | P2·T3 `TestValidateRequestInputPerType` + P4·T3 `TestValidateRecoverable` |
| T10 | Attachments optional now; admin toggle to require | P2·T4A/5 `TestAppSettingRoundTripAndDefault` + P5·T8 `TestRequireAttachmentsTogglePersistsAndDrivesSubmitEnforcement` |
| T11 | Reimbursement/personal-advance payee = logged-in employee | P2·T4 `TestCreateRequestDraftAssignsNumberAndForcesPayee` |
| T12 | Inactive projects/heads hidden from new, shown on historical | P2·T4 `TestRequestRetainsHistoricalProjectHead` |

## Approval & manager
| ID | Requirement | Verified by |
|---|---|---|
| A1 | Requester picks manager | P2·T3 `TestValidateRequestInputPerType` (manager_id required) |
| A2 | Approve; may adjust amount | P2·T8/15 `TestApproveRequestAdjustsAmountAndIsAssignedOnly` |
| A3 | Reject with required reason | P2·T9 `TestReturnAndRejectRequireTextAndAreAssignedOnly` |
| A4 | Return with required comments | P2·T9 `TestReturnAndRejectRequireTextAndAreAssignedOnly` |
| A5 | Managers see all; approve assigned | P2·T8/12 `TestListRequestsByScopeAndCount` |
| A6 | No bulk approval | P2·T13 `TestRequesterSubmitsAndCannotSeeOthers` (bulk-approve 404/405) |
| A7 | Admin reassign (reason + history) | P2·T10 `TestReassignAndReraise` |
| A8 | Edit-while-pending: history + re-notify + reset + reroute | P2·T6 `TestUpdateRequestPendingReroutesAndResetsReminder` + P5·T6 `TestNotifyRequestEditedEmailsManager` |

## Requester actions
| ID | Requirement | Verified by |
|---|---|---|
| Q1 | Edit & resubmit sent-back | P2·T6/9 `TestReturnAndRejectRequireTextAndAreAssignedOnly` |
| Q2 | Withdraw pending | P2·T7 `TestWithdrawRequestFromPending` |
| Q3 | Re-raise rejected | P2·T10 `TestReassignAndReraise` |
| Q4 | See payment outcome | P3·T2/11 `TestSettlementFlowCompletesAndShowsOutcome` |
| Q5 | Requester sees only own | P2·T12/13 `TestListRequestsByScopeAndCount` + `TestRequesterSubmitsAndCannotSeeOthers` |
| Q6 | Add clarification while On hold, no field change | P3·T8 `TestHoldUnholdPreserveApprovedFieldsAndAllowComments` |

## Lifecycle & states
| ID | Requirement | Verified by |
|---|---|---|
| L1 | Draft (private) | P2·T4 `TestCreateRequestDraftAssignsNumberAndForcesPayee` |
| L2 | Pending approval | P2·T5 `TestSubmitRequestTransitionsAndRespectsAttachmentFlag` |
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
| S1 | Both entry points | P3·T10/13 `TestReservationEntryPointAndPrefill` |
| S2 | Atomic reservation → Processing (either path) | P3·T3/10 `TestReserveRequestMovesApprovedToProcessing` |
| S3 | Dropdown lists only approved·unclaimed·not-on-hold | P3·T9 `TestLinkablePaymentRequestsFilterAndSearch` |
| S4 | Dropdown search (no/requester/payee/project/head/amount) | P3·T9 `TestLinkablePaymentRequestsFilterAndSearch` |
| S5 | Concurrency: second caller rejected | P3·T3/10 `TestReserveRequestIsAtomicUnderConcurrency` (-race) + `TestReserveRejectsSecondCallerWithJustTaken` |
| S6 | No auto-release; resume/release; authorized reassign | P3·T4/12 `TestReleaseRequestRequiresConfirmAndAuthority` |
| S7 | Cancel/switch releases only after explicit confirm | P3·T4/12 `TestReleaseRequestRequiresConfirmAndAuthority` |
| S8 | Stale-Processing reminder after 1 calendar day | P5·T4/7 `TestRunRemindersNudgesStaleProcessing` |
| S9 | One request → one payment | P3·T1/5 `TestPaymentRequestIndexRejectsDuplicateLinkButAllowsNullHistoricals` |
| S10 | "Payment settled" → Completed even if paid < approved | P3·T5/11 `TestRecordPaymentSettledCompletesEvenWhenUnderApproved` |
| S11 | "Partial settlement" → review → accept / raise-concern | P3·T7/11/12 `TestAcceptPartialAndRaiseConcern` |
| S12 | Recorded payment immutable | P3·T6/11 `TestLinkedPaymentIsImmutable` |
| S13 | Payment saves only after popup confirmed | P3·T5 `TestRecordPaymentPartialNeedsReasonAndRoutesToReview` |
| S14 | Prefill payment from request | P3·T10 `TestReservationEntryPointAndPrefill` (unblocked by ProcessingBy in select/scan) |
| S15 | Every future payment must link to approved request | P3·T5/10 `TestRecordPaymentRequiresActorReservation` + `TestPaymentCreateRequiresReservedRequest` |

## Recoverables
| ID | Requirement | Verified by |
|---|---|---|
| V1 | Budget vs recoverable classification | P4·T3 `TestCreateRecoverableRequestEnforcesCategoryRules` |
| V2 | Excluded from budget actuals | P4·T4 `TestGridAndReportExcludeRecoverablePayments` |
| V3 | Separate recoverable report/total | P4·T5/6 `TestRecoverablePaymentExcludedFromActualsButInRecoverableReport` |
| V4 | Admin-configurable categories | P4·T1/2/7 `TestRecoverableCategoryCRUD` |
| V5 | Category rules (EMD/PBG→project; ICD→counterparty; emp-adv auto) | P4·T3 `TestValidateRecoverable` |
| V6 | Records counterparty/employee, return date, notes | P4·T3/5 `TestValidateRecoverable` |
| V7 | Closes on payment; retains classification+return info | P4·T8 `TestRecoverableRequestClosesOnPaymentRetainingClassification` |
| V8 | Recoverable may link to a real project | P4·T5 `TestRecoverableReportShowsLinkedProject` |

## Notifications, reminders & conversation
| ID | Requirement | Verified by |
|---|---|---|
| N1 | In-app by default (queues) for every event | P2·T14 `TestDashboardShowsGatedWorkAreasWithCounts` + P5·T6 `TestNotifyDisabledEventSendsNoEmail` |
| N2 | Email per-event configurable | P5·T3/8 `TestNotificationSettingUpsertAndFetch` |
| N3 | Approval email → Accounts + requester + management | P5·T6 `TestNotifyApprovedResolvesAccountsRequesterAndManagement` |
| N4 | Daily manager reminder after 3 calendar days | P5·T7 `TestRunRemindersEmailsManagerAfterThreeCalendarDays` |
| N5 | Stale-processing nudge after 1 day | P5·T7 `TestRunRemindersNudgesStaleProcessing` |
| N6 | Urgent pre/post emails; no authority | P5·T6 `TestNotifyUrgentSubmitEmailsManagerAndConfersNoApprovalAuthority` |
| N7 | Conversation thread visible to viewers | P2·T11 `TestRequestCommentsAndAttachments` |
| N8 | SMTP + management configurable; password env-only | P5·T1/3/5/8 `TestSMTPPasswordLoadsFromEnvOnly` + `TestAppSettingsRoundTripAndNeverStoresPassword` |

## Dashboard & UX
| ID | Requirement | Verified by |
|---|---|---|
| D1 | Unified dashboard with work areas | P2·T14 `TestDashboardShowsGatedWorkAreasWithCounts` |
| D2 | Only permitted areas appear | P1·T12 `TestRequesterOnlySessionForbiddenFromAdminRoutesByURL` + P2·T14 `TestDashboardShowsGatedWorkAreasWithCounts` |
| D3 | Export lists/reports by permission | P2·T13 `TestRequesterSubmitsAndCannotSeeOthers` (export gated) |
| D4 | Queues show counts | P2·T14 `TestDashboardShowsGatedWorkAreasWithCounts` |
| D5 | Copy-previous-request NOT built | P2·T13 `TestRequesterSubmitsAndCannotSeeOthers` (`/copy` → 404) |

## Scope boundaries (proof-of-absence)
| ID | Requirement | Verified by |
|---|---|---|
| X1 | No tax/TDS calculation | P2 `TestNoTaxColumnsInSchema` (no tax/tds column, table, or route) |
| X2 | No recoverable repayment tracking | P4·T9 `TestNoRecoverableRepaymentTracking` |
| X3 | No refund/return-of-money | P3·T14 `TestNoRefundRoute` (route 404 + no store method) |
| X4 | No forfeiture/write-off | P4·T9 `TestNoForfeitureRoute` (route 404 + no store method) |
| X5 | No direct request-less payment path | P3·T5/10 `TestPaymentCreateRequiresReservedRequest` |
| X6 | Historical payments unchanged (request_id NULL) | P3·T1/6 `TestMigrationV3IsIdempotentAndLeavesHistoricalPaymentsUntouched` |

## Cross-cutting
| ID | Requirement | Verified by |
|---|---|---|
| C1 | Versioned migration runner | P1·T1/2 `TestMigrateAppliesAndIsIdempotent` |
| C2 | Audit history for request mutations | P2·T4/10 `TestCreateRequestDraftAssignsNumberAndForcesPayee` (entity `payment_request`) |
| C3 | Currency INR paise throughout | P2·T4 `TestCreateRequestDraftAssignsNumberAndForcesPayee` (50000 paise → ₹500.00) |
| C4 | Request number unique, monotonic per year | P2·T1/2 `TestNextRequestNumberIsMonotonicPerYear` |

---

**Audit result: 92 / 92 VERIFIED.** Structural fixes applied during remediation: single monotonic migration sequence v1–v5; `Request` gains `ProcessingBy`/`ProcessingAt`/`ReminderLastSent`/`SubmittedAt` with matching select/scan; unified `.Perms.Can` nav mechanism; `Request2` PageData field across P2/P3; `app_settings` owned by Phase 2 and extended by Phase 5; `recoverable_categories` FK kept as SQLite-deferred; proof-of-absence tests added for X1/X3/X4; `ReleaseRequest(…,confirmed,authorized bool)` aligned across overview and Phase 3. Plan-defect cleanups: no broken-then-corrected snippets, no forward-referenced symbols, no unused scaffolding.
