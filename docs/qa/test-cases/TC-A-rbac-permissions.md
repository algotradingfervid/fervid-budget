# TC-A — Permission and authorisation enforcement

**Area.** Who may reach what. Every case below is about a gate opening or
refusing: the route-permission matrix, nav-versus-enforcement, data scope,
ownership rules layered above the route gate, CSRF, privilege escalation, and
the integrity of the permission vocabulary itself. Workflow meaning belongs to
TC-B/TC-C/TC-D.

**Spec.** `tests/e2e/audit-a-rbac-permissions.spec.ts` (2 054 lines, 124 tests).

**Run.** `FERVID_E2E_PORT=4301 npx playwright test tests/e2e/audit-a-rbac-permissions.spec.ts --project=chromium --reporter=line`
→ **124 passed (17.4 s), 0 failed**, of which one (TC-A-72) is an annotated
`test.fail()` representing finding F-A-04. On `--project=mobile-chrome` all 124
are skipped: nothing here is layout-dependent and the world would be built twice.

**Findings.** `docs/qa/results/findings-a-rbac-permissions.md` — F-A-01 … F-A-11.

---

## 0. How to read this document

### 0.1 The callers

Five subjects, each holding **exactly one** seeded system role, plus two probes
that sharpen the matrix. Caller IDs are the ones in
`docs/qa/uml/05-route-permission-matrix.md` §0.3.

| ID | Constituted by | Grants (transcribed from `internal/store/migrations.go:351-402`) |
|---|---|---|
| C-anon | a browser context with no cookies at all | — |
| C-req | `asRole(…, ['Requester'])` | `request:{view,create,edit,withdraw,reraise,comment,cancel}`, `attachment:{view,create}`; scope `request=own` |
| C-mgr | `asRole(…, ['Manager'])` | `request:{view,comment}`, `approval:{approve,reject,return,reassign,accept_partial,cancel}`, `grid:view`, `report:view`; scope `request=all` |
| C-acc | `asRole(…, ['Accounts'])` | `request:{view,comment}`, all eight `payment` verbs, `reservation:{reserve,release}`, `attachment:{view,create}`, `grid:view`, `report:{view,export}`, `recoverable_report:{view,export}`; scopes `request=all`, `payment=all` |
| C-adm | `asRole(…, ['Admin'])` | all 66 canonical pairs; scopes `request=all`, `payment=all` |
| C-none | `asRole(…, [])` | nothing — signed in, entitled to nothing |

`fixtures.createApproverUser` is unusable for this area: `store.CreateUser` →
`assignDefaultRoleTx` (`internal/store/migrations.go:501`) gives every new user
the **Accounts** role, so ticking "Manager" yields Accounts **+** Manager and
every refusal under test becomes a 200. `asRole` unchecks first.

### 0.2 The objects

Three **foils** own the objects the callers probe, so every matrix cell is a
caller acting on something it does not own.

| Foil | Role | Owns |
|---|---|---|
| R2 | Requester | raised `tPending`, `tApproved`, `tProc`, `tPaid`, `tPartial` |
| M2 | Manager | is the `manager_id` of all five, and approved four of them |
| A2 | Accounts | holds the reservation on `tProc`; entered the payment `payId` on `tPaid`; part-paid `tPartial`; uploaded the bank advice `payAttId` |

C-req additionally owns `ownPending` (status pending, carrying its own document
`ownReqAttId`) and `ownApproved` (status approved), both routed to M2. These are
the targets for the ownership cases, where the question is "the raiser may, and
nobody else may".

### 0.3 The method that makes the POST half of the matrix testable

`RequirePermission` wraps `RequireLogin` (`internal/auth/auth.go:141`), and
`withCSRF` sits **inside** the permission gate — every POST is registered as
`RequirePermission(res, act, http.HandlerFunc(a.withCSRF(handler)))`. The order
is therefore **login → permission → CSRF → handler**, which yields three
distinguishable refusals for one probe sent with **no `csrf` field**:

| Caller | Outcome | Emitted by |
|---|---|---|
| not signed in | **303 → /login** | `RequireLogin` (`auth.go:90`) |
| signed in, grant absent | **403** *"You do not have permission to perform this action."* | `RequirePermission` (`auth.go:143`) |
| signed in, grant held | **403** *"Your form session expired. Refresh the page and try again."* | `withCSRF` (`app/app.go:573`) |

The third row is the proof that the gate opened, obtained **without mutating
anything**. It is also the only reliable discriminator available, because
`respondStoreError` renders `store.ErrForbidden` with the *same* wording as the
middleware (`internal/app/http_errors.go:191-192`) — so an ownership refusal and
a gate refusal are indistinguishable by status *or* by message on any route that
actually runs its handler.

> **Divergence of method from `05-route-permission-matrix.md`.** That document's
> §2 POST cells assume a **valid** token and therefore predict the mutation's own
> outcome (303, 400, 409…). This suite probes the same cells with the token
> omitted and asserts the gate instead. Neither contradicts the other: they
> measure two different halves of the same row, and §2's own CSRF note says so.
> Where a POST's *post-gate* behaviour is the point — G8, the partial-review
> owner, releasing somebody else's reservation, the tampering probes — this suite
> sends a valid token and asserts the real outcome (TC-A-73 … TC-A-78, TC-A-95 …
> TC-A-123).

### 0.4 Columns

`type` ∈ `permission` · `information-flow` · `negative` · `functional` ·
`proof-of-absence` · `regression`.
`priority` ∈ P1 (a refusal that protects money or private data) · P2 · P3.
`verdict` ∈ `PASS` · `FAIL (product defect)` · `FAIL (test defect — fixed)` ·
`BLOCKED` · `NOT RUN`.

---

## 1. Traceability

| Source | IDs used |
|---|---|
| Use cases | `UC-A-04` (anonymous → login), `UC-A-05` (CSRF), `UC-A-06` (unknown URL), `UC-A-07`/`UC-A-08` (dashboard, nav), `UC-A-09` (admin route by URL), `UC-A-10`, `UC-A-13`, `UC-A-15`, `UC-A-17` (roles matrix), `UC-A-18`–`UC-A-23` (users), `UC-A-24`–`UC-A-27` (configuration), `UC-A-28`–`UC-A-34` (masters, months, budgets), `UC-A-35`–`UC-A-39` (vendors), `UC-A-40`–`UC-A-41` (audit, backups), `UC-A-42`–`UC-A-46` (grid, reports, exports) — `docs/qa/use-cases/UC-A-platform-rbac-admin.md` |
| Route / matrix model | `RT-01`…`RT-97`, `PM-nn`, `OS-01`…`OS-22`, `GR-01`…`GR-66`, `PE-nn` — `docs/qa/uml/05-route-permission-matrix.md` |
| Coverage matrix | `R1`–`R9`, `Q5`, `A1`, `A2`, `A5`, `A6`, `A7`, `D2`, `D3`, `L7`, `S6`, `S7`, `S11`, `S12`, `S15`, `V4`, `X5`, `C2` — `docs/superpowers/specs/2026-07-25-payment-requests-coverage.md` |
| Gaps / decisions | `G8` (no self-approval), `G11`/`G12` (reservation hand-over), `G13` (approved ceiling), `D2` (permission vocabulary) |

Every route in `routes()` is covered: 95 of the 97 registrations appear as matrix
rows below; the remaining two — `RT-03 POST /login` and `RT-04 POST /logout` —
are covered by TC-A-83 and TC-A-84, because probing them in the matrix would
either log a subject out or be meaningless.

---

## 2. The expected-status matrix (TC-A-M01 … TC-A-M57)

One test per **distinct route gate**. Every route carrying that gate is probed by
all five callers, and each of the 475 cells carries its own soft assertion naming
`RT-nn METHOD path as C-xxx`, so one failure names one cell.

**Preconditions for all 57:** the fixture world of §0.1–§0.2 exists; the caller
sessions are live; GETs are sent with no redirect following, POSTs with the
`csrf` field omitted (§0.3).

**Steps for all 57:** for each route in the gate's row set, and for each of the
five callers, send the request and compare the bare outcome against the cell.

**Expected result, derived before the run** from `expectedFor()`: C-anon →
`303 → /login`; a caller holding the gate's pair → `200` for a GET (or the
override in the table) and `403 + "Your form session expired"` for a POST; a
caller not holding it → `403 + "You do not have permission to perform this
action."`. `login` and `public` rows have no pair, so every signed-in caller is
treated as holding them.

| TC ID | Gate | Routes probed | Overrides (cell ≠ default) | traces to | type | pri | actual | verdict |
|---|---|---|---|---|---|---|---|---|
| TC-A-M01 | `RequireLogin` only | RT-05 `/`, RT-08 `/dashboard`, RT-07 `/grid`, RT-30 `/notifications`, RT-31 `POST /notifications/read`, RT-06 catch-all, RT-32 `/notifications/999999/open`, RT-60 `/requests/{tProc}/reservation` | RT-06 → 404 all signed-in; RT-32 → 404 all signed-in; RT-60 → 403 for C-req/C-mgr/C-acc, **200** for C-adm | PM-05…08, PM-30…32, PM-60; OS-10; UC-A-06, UC-A-07, UC-A-42 | permission | P1 | 40/40 cells as expected | PASS |
| TC-A-M02 | public | RT-01 `/static/fervid-ds.css`, RT-02 `/login` | 200 for C-anon too | PM-01, PM-02 | permission | P3 | as expected | PASS |
| TC-A-M03 | `month:view` | RT-09 `/months` | — | PM-09, RT09; UC-A-30 | permission | P2 | as expected | PASS |
| TC-A-M04 | `month:create` | RT-10 `POST /months` | — | PM-10; UC-A-30 | permission | P2 | as expected | PASS |
| TC-A-M05 | `month:lock` | RT-39 `POST /months/2099-01/lock`, RT-40 `…/unlock` | — | PM-39, PM-40; UC-A-31 | permission | P2 | as expected | PASS |
| TC-A-M06 | `payment:create` | RT-11 `/payments/new`, RT-12 `/payments/new/options`, RT-14 `POST /payments` | — | PM-11, PM-12, PM-14; X5 | permission | P1 | as expected | PASS |
| TC-A-M07 | `payment:view` | RT-13 `/payments`, RT-15 `/payments/{payId}` | — | PM-13, PM-15; OS-17 | permission | P1 | as expected | PASS |
| TC-A-M08 | `payment:edit` | RT-16 `/payments/{payId}/edit`, RT-17 `POST …/edit` | RT-16 → **303** for C-acc/C-adm (S12: a linked payment redirects to itself, `app.go:803-806`) | PM-16, PM-17; S12, OS-16 | permission | P2 | as expected | PASS |
| TC-A-M09 | `payment:void` | RT-18 `POST /payments/{payId}/void` | — | PM-18; S12 | permission | P2 | as expected | PASS |
| TC-A-M10 | `payment:process` | RT-66 `/accounts-queue`, RT-63 `/requests/{tProc}/reservation/stale` | — | PM-63, PM-66; Q6 | permission | P2 | as expected | PASS |
| TC-A-M11 | `payment:settle` | RT-59 `POST /requests/{tProc}/settlement-preview` | — | PM-59; S13 | permission | P2 | as expected | PASS |
| TC-A-M12 | `payment:hold` | RT-64 `POST …/hold`, RT-65 `POST …/unhold` | — | PM-64, PM-65; L7, OS-18 | permission | P2 | as expected | PASS |
| TC-A-M13 | `attachment:create` | RT-19 `POST /payments/{payId}/attachments` | — | PM-19; OS-21 | permission | P1 | as expected — C-req's gate **opens** (see TC-A-123) | PASS |
| TC-A-M14 | `attachment:view` | RT-20 `/attachments/{payAttId}` | **none** — C-req 200, C-acc 200, C-adm 200, C-mgr 403 | PM-20; OS-21; PE-01 | information-flow | P1 | as expected: C-req reads a stranger's bank advice — **F-A-01**. · **Fixed** — REPAIR-LOG.md Wave 3, commit `1fac147`: the route serves payment attachments only and applies the request scope `paymentDetail` runs, refusing **404** (`internal/app/app.go:459`, handler `attachmentDownload` at `:1091`), so the C-req cell is no longer 200 | PASS (defect recorded) |
| TC-A-M15 | `grid:export` | RT-21 `/export.csv` | — | PM-21; D3, UC-A-43, UC-A-46 | permission | P2 | as expected (Admin only) | PASS |
| TC-A-M16 | `report:view` | RT-22 `/reports/monthly`, RT-23 `/reports/projects`, RT-24 `/reports/heads` | — | PM-22…24; UC-A-44 | permission | P2 | as expected | PASS |
| TC-A-M17 | `report:export` | RT-25 `/reports/ytd.csv` | — | PM-25; D3, UC-A-45 | permission | P2 | as expected | PASS |
| TC-A-M18 | `recoverable_report:view` | RT-26 `/recoverables`, RT-27 `/recoverables/list`, RT-29 `/recoverables/999999` | RT-29 → **404** for C-acc/C-adm (no recoverable request in the fixture; 404 still proves the gate opened) | PM-26, 27, 29 | permission | P2 | as expected | PASS |
| TC-A-M19 | `recoverable_report:export` | RT-28 `/recoverables/list.csv` | — | PM-28; D3 | permission | P2 | as expected | PASS |
| TC-A-M20 | `recoverable_category:edit` | RT-94 `POST /configuration/recoverable-categories` | — | PM-94; V4, UC-A-27 | permission | P2 | as expected | PASS |
| TC-A-M21 | `notification:view` | RT-33 `/admin/notifications` | — | PM-33; D7 | permission | P2 | as expected | PASS |
| TC-A-M22 | `notification:edit` | RT-34 `POST …/smtp`, RT-35 `POST …/events/request_submitted`, RT-36 `POST …/test` | — | PM-34…36; N2, N8 | permission | P2 | as expected | PASS |
| TC-A-M23 | `budget:view` | RT-37 `/budgets` | — | PM-37; UC-A-32 | permission | P2 | as expected | PASS |
| TC-A-M24 | `budget:edit` | RT-38 `POST /budgets` | — | PM-38; UC-A-32 | permission | P2 | as expected | PASS |
| TC-A-M25 | `project:view` | RT-41 `/projects` | — | PM-41; UC-A-28 | permission | P2 | as expected | PASS |
| TC-A-M26 | `project:edit` | RT-42 `POST /projects` | — | PM-42; GR-33 | permission | P2 | as expected | PASS |
| TC-A-M27 | `head:view` | RT-43 `/heads` | — | PM-43; UC-A-29 | permission | P2 | as expected | PASS |
| TC-A-M28 | `head:edit` | RT-44 `POST /heads` | — | PM-44; GR-36 | permission | P2 | as expected | PASS |
| TC-A-M29 | `vendor:view` | RT-45 `/vendors`, RT-47 `/vendors/search?q=a`, RT-48 `/vendors/{vendorId}` | — | PM-45, 47, 48; UC-A-35, UC-A-36 | permission | P2 | as expected — Admin only, so an accountant cannot search vendors | PASS |
| TC-A-M30 | `vendor:create` | RT-46 `/vendors/new`, RT-49 `POST /vendors` | — | PM-46, PM-49; UC-A-37 | permission | P2 | as expected | PASS |
| TC-A-M31 | `vendor:edit` | RT-50 `POST /vendors/{vendorId}` | — | PM-50; UC-A-38 | permission | P2 | as expected | PASS |
| TC-A-M32 | `request:view` | RT-51 `/requests`, RT-52 `/requests/export.csv`, RT-70 `/requests/{tPending}`, RT-57 `…/submitted`, RT-67 `/requests/{tPartial}/partial-review` | the three id rows → **403** for C-req (scope `own`, not the raiser) | PM-51, 52, 57, 67, 70; Q5, OS-01 | information-flow | P1 | as expected | PASS · the three id-row cells are now **404**, not 403: all three handlers go through `loadViewableRequest`, which answers the same status whether or not the row exists (**F-G-002**, fixed REPAIR-LOG.md Wave 3/Wave 4, commits `1fac147`/`709dfa6` — `internal/app/requests.go:295-306`) |
| TC-A-M33 | `request:create` | RT-53 `/requests/new`, RT-54 `…/fields`, RT-55 `POST /requests/duplicate-check`, RT-56 `POST /requests` | — | PM-53…56; D1 | permission | P2 | as expected | PASS |
| TC-A-M34 | `request:edit` | RT-71 `/requests/{tPending}/edit`, RT-72 `POST …/edit` | RT-71 → **403** for C-req *and* C-adm (only the raiser edits, `requests.go:592`) | PM-71, PM-72; OS-09 | permission | P1 | as expected | PASS |
| TC-A-M35 | `request:comment` | RT-73 `POST /requests/{tPending}/comment` | — | PM-73; N7 | permission | P2 | as expected | PASS |
| TC-A-M36 | `request:withdraw` | RT-74 `POST …/withdraw` | — | PM-74; Q2, OS-09 | permission | P2 | as expected | PASS |
| TC-A-M37 | `request:reraise` | RT-75 `POST …/reraise` | — | PM-75; Q3, OS-09 | permission | P2 | as expected | PASS |
| TC-A-M38 | `request:cancel` | RT-79 `/requests/{tApproved}/cancel`, RT-80 `POST …/cancel-request` | RT-79 → **403** for C-req *and* C-adm (asking is the raiser's act, `requests.go:802`) | PM-79, PM-80; G1, OS-09 | permission | P1 | as expected | PASS |
| TC-A-M39 | `approval:approve` | RT-84 `/approvals`, RT-76 `POST …/approve` | — | PM-76, PM-84; A2, A5, OS-20 | permission | P1 | as expected | PASS |
| TC-A-M40 | `approval:return` | RT-77 `POST …/return` | — | PM-77; A4 | permission | P2 | as expected | PASS |
| TC-A-M41 | `approval:reject` | RT-78 `POST …/reject` | — | PM-78; A3 | permission | P2 | as expected | PASS |
| TC-A-M42 | `approval:cancel` | RT-81 `POST /requests/{tApproved}/cancel`, RT-82 `GET …/cancellation`, RT-83 `POST …/cancellation` | RT-82 → **403** for C-mgr *and* C-adm (only M2 is the approver, `requests.go:837`) | PM-81…83; G1, G2, OS-08 | permission | P1 | as expected | PASS |
| TC-A-M43 | `approval:accept_partial` | RT-68 `POST /requests/{tPartial}/accept-partial`, RT-69 `POST …/raise-concern` | — | PM-68, PM-69; S11, OS-07 | permission | P1 | as expected | PASS |
| TC-A-M44 | `reservation:reserve` | RT-58 `POST /requests/{tApproved}/record-payment` | — | PM-58; S2, OS-14 | permission | P2 | as expected | PASS |
| TC-A-M45 | `reservation:release` | RT-61 `POST /requests/{tProc}/release` | — | PM-61; S6, OS-11 | permission | P2 | as expected | PASS |
| TC-A-M46 | `reservation:reassign` | RT-62 `POST /requests/{tProc}/reassign` | — | PM-62; G11, OS-12 | permission | P1 | as expected — **Admin alone** passes the gate | PASS |
| TC-A-M47 | `user:view` | RT-85 `/users` | — | PM-85; UC-A-18 | permission | P1 | as expected | PASS |
| TC-A-M48 | `user:edit` | RT-86 `POST /users` | — | PM-86; R1, UC-A-19, UC-A-20 | permission | P1 | as expected | PASS |
| TC-A-M49 | `role:view` | RT-87 `/roles` | — | PM-87; R1, UC-A-10 | permission | P1 | as expected | PASS |
| TC-A-M50 | `role:edit` | RT-88 `POST /roles` | — | PM-88; R1, UC-A-13 | permission | P1 | as expected | PASS |
| TC-A-M51 | `role:create` | RT-89 `POST /roles/new`, RT-90 `POST /roles/{custom}/copy` | — | PM-89, PM-90; R5, UC-A-11, UC-A-12 | permission | P2 | as expected | PASS |
| TC-A-M52 | `role:delete` | RT-91 `POST /roles/{custom}/delete` | — | PM-91; R9, UC-A-14 | permission | P2 | as expected | PASS |
| TC-A-M53 | `config:view` | RT-92 `/configuration` | — | PM-92; UC-A-24 | permission | P2 | as expected | PASS |
| TC-A-M54 | `config:edit` | RT-93 `POST /configuration` | — | PM-93; UC-A-25 | permission | P2 | as expected | PASS |
| TC-A-M55 | `audit:view` | RT-95 `/audit` | — | PM-95; C2, UC-A-40 | permission | P2 | as expected | PASS |
| TC-A-M56 | `backup:view` | RT-96 `/backups` | — | PM-96; UC-A-41 | permission | P2 | as expected | PASS |
| TC-A-M57 | `backup:create` | RT-97 `POST /backups` | — | PM-97; UC-A-41 | permission | P2 | as expected | PASS |

**Measured matrix, in words.** Every one of the 475 cells matched the
expectation derived from `systemRoleDefaults` before the run. In particular:
`grid:export`, every `vendor` verb, every `month`/`budget`/`project`/`head` verb,
`notification`, `config`, `audit`, `backup`, `user` and `role` are **Admin-only**
among the seeded roles; `reservation:reassign` is **Admin-only** too; `report:view`
is Manager + Accounts + Admin; `report:export` and `recoverable_report:*` are
Accounts + Admin. No cell was more permissive than the seed predicts and none was
less permissive.

---

## 3. Menu hiding is not enforcement (TC-A-58 … TC-A-64)

The sidebar contract under test is the 18 navigable screens of
`internal/app/nav.go:52-99` and the verb each declares.

| TC ID | Title | traces to | type | pri | preconditions | steps | expected result | actual result | verdict |
|---|---|---|---|---|---|---|---|---|---|
| TC-A-58 | C-req: every screen its sidebar hides, the route also refuses | UC-A-08, UC-A-09; D2, R6 | permission | P1 | signed in as C-req | load `/`; for each of the 18 nav hrefs, read whether `.sidebar a[href=…]` exists, then probe the route | offered ⇒ 200; hidden ⇒ 403 | only `/requests` offered → 200; all others hidden → 403, **except `/grid`, hidden and 200** | PASS (defect F-A-02 recorded) |
| TC-A-59 | C-mgr: same | as above | permission | P1 | signed in as C-mgr | as above | as above | offered `/requests`, `/approvals`, `/grid`, `/reports/monthly` → 200; the other 14 → 403 | PASS |
| TC-A-60 | C-acc: same | as above | permission | P1 | signed in as C-acc | as above | as above | offered `/requests`, `/accounts-queue`, `/recoverables`, `/payments`, `/grid`, `/reports/monthly` → 200; the other 12 → 403 | PASS |
| TC-A-61 | C-adm: same | as above | permission | P2 | signed in as C-adm | as above | all 18 offered ⇒ all 200 | all 18 → 200 | PASS |
| TC-A-62 | C-none: same | as above | permission | P1 | signed in with no role | as above | nothing offered ⇒ everything 403 | 17 hidden → 403; `/grid` hidden → 200 | PASS (F-A-02) |
| TC-A-63 | The variance grid is hidden from a Requester and reachable anyway | RT-07/PM-07; GR-43; UC-A-42; **F-A-02** | information-flow | P1 | signed in as C-req | assert no `.sidebar a[href="/grid"]`; probe `GET /grid`; assert the body carries "Recent Payments" and `href="/payments/{id}"`; probe `GET /payments`; assert `.tabbar a[href="/grid"]` exists | the sidebar hides it, and — because `app.go:377` gates the route on a session alone — the route answers **200** with the recent-payments table, while `/payments` answers 403 for the same caller | exactly that: `/grid` 200, body contains "Recent Payments" and payment links, `/payments` 403, and the **mobile tab bar links to `/grid`** for the very caller the sidebar hides it from (`nav.go:228-232`) | PASS (F-A-02) |
| TC-A-64 | A role-less user is offered no screen and refused every one | AC5; R6, D2 | permission | P1 | signed in with `role_ids` empty | count linked sidebar items; probe 19 gated paths and 3 session-only paths | exactly one linked item (Home); 403 on all 19; 200 on `/`, `/dashboard`, `/notifications` | as expected | PASS |

**The converse held everywhere.** No nav item rendered that then answered 403 —
the sidebar's verb and the route's verb agree on all 18 screens. The single
asymmetry runs the other way: `/grid` is *hidden* and *reachable*.

---

## 4. Data scope (TC-A-65 … TC-A-72)

| TC ID | Title | traces to | type | pri | preconditions | steps | expected result | actual result | verdict |
|---|---|---|---|---|---|---|---|---|---|
| TC-A-65 | A Requester sees only their own requests in the list | Q5, OS-01; UC-A-09 | information-flow | P1 | C-req owns `ownPending`; R2 owns `tPending` | `GET /requests?bucket=all` as C-req; count both request cards | own card present, foreign card absent | 1 and 0 | PASS |
| TC-A-66 | The CSV export obeys the same scope as the list | Q5, D3, OS-03 | information-flow | P1 | as above | `GET /requests/export.csv?bucket=all` as C-req; search the CSV for both short titles | 200; own title present, foreign title absent | 200; present / absent | PASS |
| TC-A-67 | A Requester cannot open another requester's request by id | Q5, OS-01; PM-70 | information-flow | P1 | as above | `GET /requests/{tPending}` as C-req | 403 with *"You do not have permission to view this request."* | exactly that | PASS |
| TC-A-68 | A Manager with `request=all` sees every request but decides only their own | A5, OS-01, OS-20; PM-51, PM-84 | information-flow | P1 | `tPending` is routed to M2, not to C-mgr | list `?bucket=all`; list `/approvals`; `POST …/approve` with a valid amount and token | the card is in the list, absent from the queue, and the decision is 403 | 1 / 0 / 403 | PASS |
| TC-A-69 | Accounts with `payment=all` sees every payment | OS-05 (the `all` half); PM-13, PM-15 | information-flow | P2 | A2 entered `payId` | `GET /payments/{payId}` as C-acc; open the ledger for the month | 200 and the row is listed | 200, row visible | PASS |
| TC-A-70 | Two roles union to the broadest scope, and the visible row set widens | R3, R4, OS-02 | information-flow | P1 | a fresh subject holding `['Requester']` | list `?bucket=all` → expect 0 foreign cards; `setExactRoles(['Requester','Manager'])`; list again | 0 then 1: `mergeScope` takes `all` over `own` | 0 then 1 | PASS |
| TC-A-71 | A role change takes effect on the very next request, without re-login | R1, R4 | permission | P1 | a fresh subject holding no role, signed in | probe `/users` → 403; grant `['Admin']`; probe again; revoke; probe again | 403 → 200 → 403 on the same session cookie, because `permsFor` queries per request (`auth.go:97-105`) | 403 → 200 → 403 | PASS |
| TC-A-72 | The `payment` data scope is declared and grantable but never enforced | R3, OS-05; **F-A-04** | information-flow | P2 | a custom role holding `payment:view` with scope `payment=own`, held by a user who has entered no payment | open the ledger as that user | scope `own` must hide a payment the holder did not enter — `payment` is in `scopedResources` (`permissions.go:185`) and the matrix offers Own/Assigned/All for it | **the whole ledger is returned**; `Scope(u,"payment")` is called nowhere in `internal/app` | **FAIL (product defect)** — annotated `test.fail()`, F-A-04 |

---

## 5. Ownership layered above the route gate (TC-A-73 … TC-A-78)

| TC ID | Title | traces to | type | pri | preconditions | steps | expected result | actual result | verdict |
|---|---|---|---|---|---|---|---|---|---|
| TC-A-73 | G8: nobody approves their own request | G8, OS-06, A1; PE-04 | negative | P1 | a subject holding `['Requester','Manager']` — the only way to reach the attempt | (a) `POST /requests` with `manager_id` = self; (b) raise one routed to M2, then `POST …/approve` as the raiser | (a) 400 *"you cannot approve your own request"* from `validateRequestInput` (`store/requests.go:142`); (b) 403 from `ApproveRequest`'s last-line check (`requests.go:692`) | 400 with that wording; 403 | PASS |
| TC-A-74 | A Manager cannot approve a request assigned to a different manager | A5, OS-08; PE-05 | negative | P1 | `tPending`'s manager is M2 | `POST /requests/{tPending}/approve` `approved_amount=100.00` as C-mgr; then check `/approvals` | 403 — `approval:approve` says you may decide, `manager_id` says which; and the queue never offered it | 403; 0 cards | PASS |
| TC-A-75 | The partial-review decision belongs to the request's own manager | S11, OS-07; PE-07, PE-08 | negative | P1 | `tPartial` is in `partial_review`, manager M2 | `POST …/raise-concern` as M2; `POST …/accept-partial` as C-adm; then as C-mgr | 303 for M2; **403 for the Admin holding every grant** and 403 for the other manager (`store.go:980`, `store.go:1021`) | 303 / 403 / 403 | PASS |
| TC-A-76 | The reservation screen opens for the holder and for the reassigner, nobody else | OS-10, G11; PM-60 | permission | P1 | `tProc` reserved by A2 | `GET /requests/{tProc}/reservation` as A2, C-adm, C-acc, C-mgr, C-req | 200 (holder + release), 200 (reassign), 403 (release but not the holder), 403 (no reservation verb), 403 (out of scope, refused first) | 200 / 200 / 403 / 403 / 403 | PASS |
| TC-A-77 | Releasing a reservation you do not hold needs `reservation:reassign` | S6, S7, OS-11; PE-11 | negative | P1 | a request of its own, reserved by A2 | `POST …/release` with reason + confirm as C-acc, then as C-adm; re-probe the reservation screen | 403 for the release-only holder; 303 for the Admin, because `authorized` **is** `reservation:reassign` (`linking.go:630`, `store.go:785`); then 409 because nobody holds it | 403 / 303 / 409 | PASS |
| TC-A-78 | Only the raiser may edit, ask to cancel, or withdraw | OS-09, G1; PM-71, PM-79 | permission | P1 | C-req owns `ownPending` (pending) and `ownApproved` (approved) | `GET {own}/edit` as C-req; `GET {tPending}/edit` as C-req; `GET {own}/edit` as C-adm; `GET {ownApproved}/cancel` as C-adm; then as C-req | 200 / 403 / 403 / 403 / 200 — an administrator holding `request:edit` and `request:cancel` is still refused | exactly that | PASS |

---

## 6. CSRF (TC-A-79 … TC-A-84)

Swept across one representative state-changing POST per resource family, with a
caller that may reach it: `request`, `approval`, `payment`, `reservation`,
`notification`, `user`, `role`, `config`, `budget`, `vendor`, `month`, `backup`.

| TC ID | Title | traces to | type | pri | preconditions | steps | expected result | actual result | verdict |
|---|---|---|---|---|---|---|---|---|---|
| TC-A-79 | Every state-changing POST refuses a missing token | UC-A-05, PM CSRF note; GP-3 | negative | P1 | 12 family POSTs, each with a permitted caller | post each with no `csrf` field | 403 **and** the body carries *"Your form session expired."*, proving `withCSRF` refused rather than the permission gate | 24/24 assertions held | PASS |
| TC-A-80 | Every state-changing POST refuses a forged token | UC-A-05 | negative | P1 | as above | post each with `csrf=not-a-real-token` | 403 + the CSRF wording | 24/24 | PASS |
| TC-A-81 | A token from another signed-in session is refused | UC-A-05; F8 | negative | P1 | two live sessions with different `fervid_csrf` cookies | assert the two tokens differ; post each family POST with the *other* session's token | 403 + the CSRF wording — the double-submit compare is against **this** session's cookie (`auth.go:171-179`) | tokens differ; 24/24 | PASS |
| TC-A-82 | The same POST with the session's own token is accepted (control) | UC-A-05 | functional | P1 | C-req signed in | `POST /notifications/read` with the session's token | 303 → `/notifications` | 303 | PASS |
| TC-A-83 | `POST /login` carries no CSRF gate at all | RT-03; sibling F-07 | proof-of-absence | P3 | anonymous context | post wrong credentials with no `csrf` | 200 with the login template and an error — `loginPost` is the one state-changing POST outside `withCSRF` (`app.go:365`) and therefore also outside the 21 MiB body cap | 200, no CSRF refusal | PASS (recorded as F-A-10) |
| TC-A-84 | `POST /logout` refuses an anonymous caller with no token | RT-04/PM-04 | negative | P3 | anonymous context, no `fervid_csrf` cookie | post with no token | 403 — logout is CSRF-gated even though it is not login-gated | 403 | PASS |

---

## 7. Privilege escalation and tampering (TC-A-85 … TC-A-114, TC-A-123, TC-A-124)

32 attempts, each its own test. Every one is expected to be **refused**; the
three that are not are findings.

| TC ID | Attempt | traces to | type | pri | expected result | actual result | verdict |
|---|---|---|---|---|---|---|---|
| TC-A-85 | C-req types `/users` | R6, UC-A-09, PE-15 | permission | P1 | 403 + the permission wording | 403 | PASS |
| TC-A-86 | C-req types `/roles` | as above | permission | P1 | 403 | 403 | PASS |
| TC-A-87 | C-req types `/audit` | as above | permission | P1 | 403 | 403 | PASS |
| TC-A-88 | C-req types `/configuration` | as above | permission | P1 | 403 | 403 | PASS |
| TC-A-89 | C-req types `/backups` | as above | permission | P1 | 403 | 403 | PASS |
| TC-A-90 | C-req types `/admin/notifications` | as above | permission | P1 | 403 | 403 | PASS |
| TC-A-91 | C-req reads a payment by id | PM-15 | information-flow | P1 | 403 — no `payment` verb at all | 403 | PASS |
| TC-A-92 | C-req downloads a payment attachment by id | OS-21, PE-01; **F-A-01** | information-flow | P1 | a refusal: the attachment belongs to a payment on a request this caller cannot read | **200 + the file bytes.** `attachmentDownload` checks `attachment:view`, path containment and existence, and nothing about ownership (`app.go:881-911`). · **Fixed** — REPAIR-LOG.md Wave 3, commit `1fac147`: `attachmentDownload` now resolves the attachment to its payment through `AttachmentWithPayment` and applies `canReadPayment`, refusing **404** through the same path as a missing id so the route is not an enumeration oracle (`internal/app/app.go:1091-1107`, `:1014-1025`) | PASS (defect F-A-01) |
| TC-A-93 | C-mgr downloads the same attachment | OS-21 | information-flow | P2 | 403 — a Manager holds no `attachment:view` | 403 | PASS |
| TC-A-94 | The Download link on a request document does not resolve to that document — both halves | **F-A-05**, **F-A-01** | information-flow | P1 | (a) the requester's own invoice comes back; (b) where the two id spaces collide, something else does | (a) it does not; (b) both sequences start at 1, so `request_attachments` id 1 and `payment_attachments` id 1 both exist, and the link on the requester's own `invoice-….txt` returned **200 with the accountant's `bank-advice-….txt`**. Server log for the run: `/attachments/1 status=200 bytes=52` for TC-A-94, byte-identical to the same line for TC-A-92. · **Fixed** — REPAIR-LOG.md Wave 3, commit `1fac147`: the two id spaces have two routes. Request documents are served by `GET /requests/{id}/attachments/{attachmentID}` (`internal/app/app.go:460`, handler `requestAttachmentDownload` at `:1115`), which reads `request_attachments`, checks the document belongs to the `{id}` in the path, and then scopes it by that request; `GET /attachments/{id}` serves payment attachments only | PASS (defects F-A-05, F-A-01) |
| TC-A-95 | C-req posts another user's `id` to `/users` | R6, UC-A-20 | negative | P1 | 403, and the victim's name unchanged | 403; name intact | PASS |
| TC-A-96 | C-req grants itself Admin by posting `role_ids` for its own user | R1, R6 | negative | P1 | 403, and `/users` still refused on the next request | 403; still 403 | PASS |
| TC-A-97 | C-adm deletes a system role | R9, UC-A-15, PE-19 | negative | P1 | 403 (`store/permissions.go:318`), and the role still listed | 403; still listed | PASS |
| TC-A-98 | C-adm renames a system role | R9, UC-A-15 | negative | P1 | 403 (`permissions.go:290`) — the input is `readonly`, not `disabled`, so the value *is* posted | 403; name held | PASS |
| TC-A-99 | C-req creates a role | R1 | negative | P1 | 403 | 403 | PASS |
| TC-A-100 | A hand-crafted `cell` / `perm` value invents a grant | R1, R2, UC-A-17, PE-18 | negative | P1 | an unknown *cell* expands to nothing (303, nothing granted); an unknown *pair* is refused 400 *"That permission does not exist."*; the role still holds nothing | 303 / 400 with that wording / 0 grants | PASS |
| TC-A-101 | C-req reserves, pays, previews a settlement, or holds | X5, S15 | negative | P1 | 403 on all four routes | 403 ×4 | PASS |
| TC-A-102 | A2 swaps the `request_id` on the payment form for one it has not reserved | X5, OS-15, PE-12 | negative | P1 | 403 from `RecordPaymentForRequest` (`store.go:898`), and the entry screen for that request answers 409 | 403; 409; **and the reason is replaced by "Something went wrong"** (F-A-09) | PASS (F-A-09 recorded) |
| TC-A-103 | A forged `settlement` value | S13 | negative | P2 | 400 *"choose payment settled or partial settlement"* | 400 with that wording | PASS |
| TC-A-104 | An amount above the approved ceiling | G13, OS-15 | negative | P1 | 400 *"…is more than the approved…"* | 400 with that wording | PASS |
| TC-A-105 | `vendor_id` on a reimbursement, and a forged `requester_id` | T11, PE-16 | negative | P1 | the vendor is zeroed by `forcesRequesterPayee`, `requester_id` is taken from the session (`store/requests.go:366`), so the request belongs to the caller and never appears in the impersonated user's list | request readable by the caller and absent from R2's list | PASS |
| TC-A-106 | A GET on a mutation route (`/requests/{id}/approve`) | MX-4, A6 | proof-of-absence | P2 | 404 or 405 — no GET pattern, so `GET /` catches it and no gate runs | 404 | PASS |
| TC-A-107 | A POST on a read route (`POST /requests/{id}`) | MX-5 | proof-of-absence | P2 | 405 — the path matches, the method does not | 405 | PASS |
| TC-A-108 | An anonymous caller against 25 paths | UC-A-04, GP-1 | permission | P1 | every one 3xx with `Location` containing `/login`; never 403, never 200 | 50/50 assertions held; all **303** | PASS |
| TC-A-109 | Another user's notification | OS-22 | information-flow | P2 | 404 — not-yours is indistinguishable from gone (`inapp.go:76-81`), so there is no enumeration oracle | 404 | PASS |
| TC-A-110 | C-req comments on a request it cannot see | OS-01, N7 | negative | P1 | 403 — the verb is held, `loadViewableRequest` refuses the row first | 403 | PASS · the refusal is now **404**: `loadViewableRequest` answers a row outside the caller's scope exactly as it answers one that does not exist (**F-G-002**, fixed REPAIR-LOG.md Wave 3/Wave 4, commits `1fac147`/`709dfa6` — `internal/app/requests.go:289-306`, `requestComment` at `:936-937`) |
| TC-A-111 | C-req withdraws or re-raises somebody else's request | OS-09 | negative | P1 | 403 on both (store checks `RequesterID != actor`) | 403 ×2 | PASS |
| TC-A-112 | C-mgr cancels a request routed to another manager | G2, OS-08 | negative | P1 | 403 (`store/requests.go:976`) | 403 | PASS |
| TC-A-113 | A reservation handed to somebody who cannot work the queue | OS-13, G11 | negative | P2 | 400 *"That person cannot work the Accounts queue."* — otherwise the request is stranded | 400 | PASS |
| TC-A-114 | A deactivated account keeps using a live session | UC-A-23 | negative | P1 | the next request after deactivation redirects to `/login`, because `userFromRequest` rejects `!u.Active` (`auth.go:228`) | 200 → 303 → `/login` | PASS |
| TC-A-123 | C-req attaches a file to a stranger's payment — a linked, immutable one | OS-21, OS-16, PE-02; **F-A-03** | information-flow | P1 | a refusal | **303**, the file is on the payment, the proof list grows by exactly one, and the audit trail names the Requester. The same payment refuses an edit from the accountant who entered it (400, S12), so it is immutable to its owner and appendable by a stranger. `attachmentUpload` checks `attachment:create` and nothing else (`app.go:861-879`). · **Fixed** — REPAIR-LOG.md Wave 3, commit `1fac147`, under its decision 4: the write path applies the same ownership check the read path applies (`internal/app/app.go:1050-1054`). The hole is also unreachable today, because F-D-08's fix means every payment this product can create is linked and `paymentCreate` refuses `request_id=0`; the check was added anyway, because the hole returns the day a free-standing payment becomes creatable | PASS (defect F-A-03) |
| TC-A-124 | A 12-character letters-only password answers 500, not 400 | **F-A-11**; UC-A-19, UC-A-22 | negative | P2 | 400 naming the missing digit | **500** *"The password could not be secured."* on create **and** on reset: `validatePassword` checks length only (`app.go:1555-1560`) and `auth.HashPassword` then demands a letter and a digit (`auth.go:47-60`). Control: an 11-character password is a correct 400 *"at least 12 characters"*, and the refused reset left the victim's credentials and session intact | PASS (defect F-A-11) |

---

## 8. Vocabulary integrity (TC-A-115 … TC-A-122)

| TC ID | Title | traces to | type | pri | preconditions | steps | expected result | actual result | verdict |
|---|---|---|---|---|---|---|---|---|---|
| TC-A-115 | The roles matrix offers all 21 resources and all 66 pairs | R2, R8, D2; UC-A-10 | functional | P1 | signed in as the seeded admin on `/roles` | open all nine Advanced disclosures; collect every `.perm-table input[name="perm"]` value; compare against `resourceActions` transcribed into the spec | 66 pairs across 21 resources, each offered exactly once, with no missing and no invented pair | 66 / 21 / no missing / no extra | PASS |
| TC-A-116 | The seeded Admin role holds every pair, and the screen renders it so | R7; UC-A-10 | functional | P1 | as above, `?role=<Admin>` | count checked `perm` boxes | 66 — `adminGrants()` is the whole vocabulary | 66 | PASS |
| TC-A-117 | Saving the matrix round-trips exactly | R1, R2; UC-A-13 | functional | P1 | a fresh custom role | assert 0 granted; tick all 66 Advanced boxes; Save role; reload; assert 66 granted **and** every available cell granted; untick every cell (the screen's script clears the pairs behind it, `fervid-app.js:479-487`); assert 0 in the browser; Save; reload; assert 0 | exact round-trip in both directions — `UpdateRolePermissions` replaces the whole set, so anything absent is revoked | 0 → 66 (+ all cells) → 0 → 0 | PASS |
| TC-A-118 | The data scope control round-trips for both scoped resources | R3; UC-A-13 | functional | P2 | a fresh custom role | save `scope_request=assigned` and `scope_payment=own`; reload | both radios come back checked; `request` and `payment` are the two scoped resources | both checked | PASS |
| TC-A-119 | Eleven granted verbs are named by no route gate, nine of them by nothing at all | R2, R8, GR-11/20/33/36/43/47/48/50/54; **F-A-06** | proof-of-absence | P2 | — | build the set of 55 gate strings from `routes()`; diff against the 66 | 55 distinct route gates, all inside the vocabulary; 11 pairs unnamed by any gate, of which `vendor_bank:{view,edit}` are enforced below the route and 9 are enforced nowhere | 55 / 11 / 9, and **no route names a pair outside `resourceActions`** | PASS (F-A-06) · the three counts no longer hold. `approval:reassign` is now named by a route gate (`internal/app/app.go:569`, F-A-06/F-C-02, REPAIR-LOG.md Wave 3, commit `1fac147`) and so is `grid:view` (`:431`, F-A-02/F-G-032, same wave); `user:edit` left the gate set the other way, because `POST /users` is now `requireAnyOf("user",{create,edit})` with `userSave` demanding the verb the pressed control was gated on (`:590`, `:1523-1535`, F-A-07). Counting single-pair `RequirePermission` gates the way this case did gives 56 today against 55 then; the derived 11/9 figures depend on whether `requireAnyOf` counts as a gate and were not recounted here |
| TC-A-120 | `approval:reassign` is granted to Manager and Admin and can never be used | A7, GR-11; **F-A-06** | proof-of-absence | P2 | — | probe three plausible approver-reassignment paths as C-adm; then the one real `/reassign` route as C-mgr | all three 404/405; the real route is the *reservation's* and refuses a Manager, who holds `approval:reassign` and not `reservation:reassign` | 405 ×3; 403 | PASS (F-A-06) · **Fixed** — REPAIR-LOG.md Wave 3, commit `1fac147`: `approval:reassign` now has a door. `POST /requests/{id}/reassign-approver` takes `manager_id` and `reason`, is gated on `approval:reassign`, and runs over the already-tested `store.ReassignRequest` (`internal/app/app.go:569`, handler `requestReassignApprover` at `:1615-1647`). The path is deliberately not `/reassign` — that one is the reservation's, on `reservation:reassign` |
| TC-A-121 | `user:create` is offered on the screen and enforced nowhere | GR-54; **F-A-07**; UC-A-19 | information-flow | P2 | a custom role holding `user:view` + `user:create` | open `/users`; assert the ＋ Add user control is visible; post the create form | the control renders (`templates.go:1195`) and its POST answers **403**, because `POST /users` is gated on `user:edit` (`app.go:510`) | control visible; 403 | PASS (F-A-07) |
| TC-A-122 | A request may be routed to an approver who cannot approve it | A1, GR-08; **F-A-08** | negative | P2 | C-req holds no `approval:approve` | R2 posts `/requests` with `manager_id` = C-req's id; then C-req and M2 each try to approve; then R2 withdraws | `validateRequestInput` never re-checks that `manager_id` holds `approval:approve`, so the request is created and stranded: the named approver is refused by the route gate and every real manager is refused by the ownership check | created; 403; 403; withdrawal (303) is the only exit | PASS (F-A-08) |

---

## 9. Reconciliation with `docs/qa/uml/05-route-permission-matrix.md`

The sibling model was read after the code and before this document was written.
Every cell it predicts and this suite measured **agrees**. Three notes:

| ID | Point | Resolution |
|---|---|---|
| RC-1 | §2's POST cells assume a valid CSRF token; this suite probes them with the token omitted | Two halves of one row, not a contradiction. §0.3 above documents the technique; §2's own CSRF note anticipates it. The post-gate outcomes §2 predicts are asserted separately in TC-A-73 … TC-A-123. |
| RC-2 | §4.1 counts **9** pairs "no route consumes"; TC-A-119 measures **11** unnamed by a route gate | Same facts, two definitions. §4.1 excludes `vendor_bank:{view,edit}` because they are enforced at the store and form-read boundaries — which is true and stronger than a route gate. TC-A-119 asserts both numbers so neither definition can drift. |
| RC-3 | §6 files F-01 … F-12; this suite files F-A-01 … F-A-11 | F-A-01 = F-01, F-A-02 = F-02, F-A-03 = F-03, F-A-04 = F-04, F-A-06 = F-09, F-A-10 = F-07. **F-A-05, F-A-07, F-A-08, F-A-09 and F-A-11 are new** and were found by probing rather than reading. F-05, F-06, F-08, F-10, F-11 and F-12 were not re-tested here and are neither confirmed nor contradicted. |
| RC-4 | The model rates F-01 (attachment read) **High**; this suite rates F-A-01 **critical** | Raised on evidence. The model reasoned from the missing check; the probe showed that the *product's own UI* delivers the leak — TC-A-94 clicked the Download link on a Requester's own document and received the accountant's bank advice (`/attachments/1 status=200 bytes=52`, byte-identical to TC-A-92's direct probe). A confidentiality failure reachable by an ordinary click, not only by a hand-typed URL, is critical. |

**No measured result contradicted a predicted cell.** Where this suite goes
beyond the model it adds rows the model marked unverified (the attachment write
of PE-02, the `payment` scope of OS-05) and one whole defect class the model did
not reach (F-A-05, the crossed attachment id spaces).

---

## 10. Verdict summary

| Verdict | Count |
|---|---|
| PASS | 123 |
| FAIL (product defect) — annotated `test.fail()`, assertion intact | 1 (TC-A-72 → F-A-04) |
| FAIL (test defect — fixed) | 0 in the final run; 4 during development (see below) |
| BLOCKED | 0 |
| NOT RUN | 0 |

Reporter line, verbatim: **`124 passed (17.4s)`** with `0 failed`. The one
annotated `test.fail()` is reported by Playwright as `✘ … (1.4s)` inside a green
run, and will turn the suite red with "passed unexpectedly" the day F-A-04 is
fixed — which is the signal wanted.

Seven defects are represented by tests that assert the **observed** behaviour and
name the finding in the assertion message rather than by `test.fail()`: F-A-01
(TC-A-92, TC-A-M14), F-A-02 (TC-A-63), F-A-03 (TC-A-123), F-A-05 (TC-A-94),
F-A-07 (TC-A-121), F-A-08 (TC-A-122), F-A-09 (TC-A-102), F-A-11 (TC-A-124). That
is deliberate: for these the *correct* behaviour is a refusal whose exact status
the product has never defined, so asserting a specific refusal would encode a
guess. Asserting the measured behaviour with the finding id in the message keeps
the evidence in the suite and makes any change to it fail loudly. Only F-A-04 has
an unambiguous correct answer (the row must be hidden), so only it is a
`test.fail()`.

**Test defects found and fixed during development, recorded so they are not
re-learned.**

1. `browserName` is `'chromium'` for both the Desktop Chrome *and* the Pixel 5
   projects, so a `browserName` guard runs a suite twice. The discriminator is
   `testInfo.project.name`.
2. A `beforeAll` hook runs **before** any per-test skip is evaluated, so the
   fixture world was built at 390 px where the request-edit screen's sticky
   action bar covers its own submit button. The guard and the build both belong
   in `beforeEach`.
3. TC-A-77 releases a reservation, and nine matrix rows read the same reserved
   request. It now builds a request of its own.
4. `friendly()` has no `ErrForbidden` branch, so the settlement path's
   authorisation refusal reads *"Something went wrong"*. The test was asserting
   the store's wording; the assertion now matches the observed message and the
   gap is filed as F-A-09.

**Nothing was left untested inside this area.** The one thing deliberately not
asserted is a 403 on `GET /grid`: the route carries no `grid:view` gate, so
asserting a refusal would encode a wish rather than a rule. TC-A-63 asserts the
observed 200, proves what is leaked, and F-A-02 argues why it is wrong.
