# 05 — Route / permission matrix

Every statement in this document was read out of the code at the cited
`file:line`. Nothing here is derived from a spec, a plan or a mockup. Where the
code and a document disagree, the divergence is recorded in §7 and the **code**
is what this matrix predicts.

The spine is `(*App).routes` — `internal/app/app.go:362-524`, 97 registrations.
The gate implementations are `internal/auth/auth.go:87-200`. The refusal
rendering is `internal/app/http_errors.go:158-212`. The seeded role grants are
`internal/store/migrations.go:351-402`.

> **Correction — the route inventory is short by two, and the line range has
> moved.** This matrix was read out of commit `30edd6a`. The audit repair waves
> registered two routes that did not exist then, both in Wave 3 (`1fac147`):
> `GET /requests/{id}/attachments/{attachmentID}` (`internal/app/app.go:460`,
> RT-20a below) and `POST /requests/{id}/reassign-approver`
> (`internal/app/app.go:569`, RT-78a below). `(*App).routes` now begins at
> `internal/app/app.go:405`, not `:362`. The "97 registrations" figure is
> therefore **stale**; a raw `grep -c 'mux.Handle('` over
> `internal/app/app.go` returns **96** today, which does not reconcile with 97
> plus two, so the original count is not re-derived here — reconciling it
> against every `mux.Handle` call is a separate job. What is settled: the two
> routes named above are new since this document was written, and every §1
> route table, §2 status matrix and §4 grant map below predates them.
>
> **The count reconciles, and it is still moving.** `mux.Handle` is not the only
> registrar: three routes go through `mux.HandleFunc` instead — `GET /login`,
> `POST /login` and `POST /logout` — which is RT-02, RT-03 and RT-04. At
> `30edd6a` the arithmetic was `94 + 3 = 97`, matching the "97 rows" of §1.
> `git diff 30edd6a -- internal/app/app.go` on the registration lines shows
> **additions only, no removals** — the four `-` lines it reports are the
> pre-repair versions of registrations that still exist (`GET /grid`,
> `POST /payments`, `POST /users`, `POST /login`), re-gated rather than dropped.
>
> Three routes have been **added** since, one per repair:
> `GET /requests/{id}/attachments/{attachmentID}` (F-A-05, Wave 3),
> `POST /requests/{id}/reassign-approver` (F-A-06 · A7, Wave 3) and
> `POST /configuration/recoverable-categories/{id}/delete` (F-E-06, Wave 5 —
> the door for the `recoverable_category:delete` grant §4 records as consuming
> nothing; handler `internal/app/configuration.go:166`, store
> `DeleteRecoverableCategory` at `internal/store/recoverables.go:260`). That
> makes **100** registrations as this line is written, and §1 short by three.
>
> **Do not treat 100 as final.** `internal/app/app.go` is being edited as the
> repair lands; this count went 97 → 99 → 100 during the documentation pass
> itself. Count it with `grep -c 'mux\.Handle(' internal/app/app.go` plus the
> three `HandleFunc` registrations rather than trusting any number written here.
>
> **Four more repairs this matrix predates**, all Wave 3 (`1fac147`) and all
> changing what a caller is *allowed* rather than only what a handler renders,
> so they belong here rather than in a footnote:
>
> - **`GET /grid` is gated on `grid:view`** (F-A-02/F-G-032, **high**). It asked
>   for a session alone, so a caller holding no role at all read every budget
>   and actual plus a Recent Payments table with amounts, payees and live
>   payment links, while `/payments` answered them 403. Recent Payments is
>   gated separately on `payment:view` **in the handler**, not trusted to a
>   template (`internal/app/app.go:426-431`). Falsifies **RT-07**, **RT-21**'s
>   asymmetry note, **GR-43** and its dead-grant entry in §5.
> - **`POST /payments` is double-gated `payment:create` *and* `payment:settle`**,
>   and `settlement=partial` additionally requires `payment:mark_partial`
>   (F-D-10, **high**) — the settlement write was gated more weakly than its own
>   pure preview. `mark_partial` is checked in the handler and not as a route
>   gate because it applies only to that one form value, which
>   `internal/app/app.go:441-445` says in as many words; the check itself is at
>   `:831-835`. Falsifies **RT-14**, **GR-19**, **GR-20** and GR-20's dead-grant
>   entry in §5.
> - **`POST /users` demands `user:create` when it is creating** (F-A-07). The
>   route gate stays `user:edit`; the handler asks for the verb the pressed
>   control is gated on, because one handler serves both
>   (`internal/app/app.go:1523-1535`). Falsifies **RT-86**, **GR-54** and the
>   "`user:edit` alone lets a caller mint a new user" line in §5's F-09.
> - **The `payment` data scope is enforced now** (F-A-04/F-G-003), in two halves:
>   Wave 1 (`633997b`) gave `PaymentListOptions` a `Scope`/`ViewerID` and made
>   `ListPayments` filter on `entered_by` (`internal/store/store.go:1501`), and
>   Wave 3 passed them (`internal/app/app.go:793-805`). Falsifies **RT-13**,
>   **OS-05** and **F-04**.

---

## 0. How to read this document

### 0.1 The three gate primitives, and exactly what they emit

| ID | Primitive | Code | Emits on refusal |
|---|---|---|---|
| GP-1 | `RequireLogin` | `auth.go:87-95` | `http.Redirect(..., "/login", http.StatusSeeOther)` → **303**, `Location: /login`, empty body |
| GP-2 | `RequirePermission(res, act, …)` | `auth.go:140-148` | wraps `RequireLogin` **first**; on a signed-in caller lacking the grant, `writeError(…, 403, "You do not have permission to perform this action.")` |
| GP-3 | `withCSRF(fn)` | `app.go:554-578` | body cap 21 MiB → **413**; unparseable form → **400**; `!CheckCSRF` → **403** *"Your form session expired. Refresh the page and try again."* |

`writeError` routes through the handler installed at `app.go:205-207`, i.e.
`respondError` → `renderStatus(..., "error_page", …)` (`http_errors.go:162-181`).
So **a 403 is a rendered HTML page carrying status 403**, not `http.Error` plain
text. `renderStatus` forces `Shell{Chrome: chromeNone}` for `error_page`
(`http_errors.go:121-125`), so the refusal page has no sidebar.

> **Correction to the brief.** The brief asks for `302→/login`. The code emits
> **303 See Other** (`http.StatusSeeOther`, `auth.go:90`), and
> `http_safety_test.go:126` asserts `StatusSeeOther` for an unauthenticated
> request. Every anonymous cell below therefore reads `303→/login`. A test
> asserting 302 will fail.

**An anonymous request to a gated route redirects; it never 403s.** GP-2 calls
GP-1 as its outer wrapper (`auth.go:141`), so the login redirect always wins over
the permission refusal. Corollary for POST routes: because `withCSRF` sits
*inside* the permission gate (see any `mux.Handle("POST …", a.auth.RequirePermission(…, http.HandlerFunc(a.withCSRF(…))))`),
an anonymous or unprivileged POST is refused **before** the CSRF token is ever
looked at. A CSRF 403 is only reachable by a caller who already passed the gate.

### 0.2 Cell notation

| Symbol | Meaning |
|---|---|
| `200` | success, whole page/fragment served |
| `200*` | success, but an ownership/scope rule narrows *what is shown* rather than refusing. Every `200*` carries a footnote naming the filter. |
| `303→X` | `http.StatusSeeOther` with `Location: X`. Used for the login redirect and for every successful mutation (all handlers redirect after write). |
| `403` | rendered `error_page`, status 403 |
| `400` / `404` / `409` / `413` / `502` | rendered `error_page` (or a re-rendered form) at that status |
| `405` | `net/http` ServeMux method mismatch, no handler runs |
| `?` | could not be determined from code — treat as unverified, do not assert |

### 0.3 The fixture every cell assumes

Five callers, each holding **exactly one** seeded system role
(`store/migrations.go:351-402`):

| ID | Caller | Grants | Data scope |
|---|---|---|---|
| C-anon | no session cookie | — | — |
| C-req | one `Requester` role | `request:{view,create,edit,withdraw,reraise,comment,cancel}`, `attachment:{view,create}` | `request=own` |
| C-mgr | one `Manager` role | `request:{view,comment}`, `approval:{approve,reject,return,reassign,accept_partial,cancel}`, `grid:view`, `report:view` | `request=all` |
| C-acc | one `Accounts` role | `request:{view,comment}`, `payment:{view,create,edit,void,process,settle,mark_partial,hold}`, `reservation:{reserve,release}`, `attachment:{view,create}`, `grid:view`, `report:{view,export}`, `recoverable_report:{view,export}` | `request=all`, `payment=all` |
| C-adm | one `Admin` role | all 66 pairs (`adminGrants()`, `migrations.go:404-412`) | `request=all`, `payment=all` |

The **target objects** the caller does *not* own:

| ID | Object | Definition |
|---|---|---|
| OBJ-REQ | request `T` | raised by a *different* user `R2`; `manager_id` = a *different* manager `M2`; status `processing`; `processing_by` = a *different* accountant `A2` |
| OBJ-PAY | payment `P` | `request_id = T.id`, `entered_by = A2`, not voided, month unlocked |
| OBJ-ATT | attachment `X` | row in `payment_attachments` with `payment_id = P.id` |
| OBJ-VEN | vendor `V` | any active vendor |
| OBJ-NOTIF | notification `N` | a row addressed to a *different* user |

Where a route's meaning requires a different status (e.g. `POST /hold` needs
`approved`), the row's note says so and gives both outcomes.

### 0.4 Where the seeded grants actually come from — read this before testing

`seedSystemRoles` (`store/migrations.go:414-454`) runs **only inside migration
v1** (`migrations.go:75`). The grants in §0.3 are therefore the defaults a
*fresh* database gets. Two consequences a tester must respect:

1. `reservation:reserve` and `reservation:release` reached the `Accounts` role
   through a **separate** migration, v5 (`migrations.go:300-312`), using
   `INSERT OR IGNORE` — because v1 had already run on installed databases and
   `seedSystemRoles` never re-runs. This is confirmed by the comment at
   `migrations.go:293-299` and by PROGRESS.md:256-260.
2. Any grant a *future* phase adds to a system role needs its own migration for
   the same reason. **An installed database may legitimately differ from
   §0.3.** Before asserting a 403, dump the live grant set
   (`SELECT resource, action FROM role_permissions rp JOIN roles r ON r.id=rp.role_id WHERE r.name='…'`)
   rather than trusting this table. `UpdateRolePermissions`
   (`store/permissions.go:366-413`) lets an administrator rewrite any role's
   grants wholesale, system roles included — only the *name* is immutable
   (`permissions.go:290-292`).

---

## 1. Complete route inventory

97 rows, in registration order. `RT-nn` is stable; the matrix in §2 uses the same
`nn`. Two rows were appended after this document was written and are numbered
off their neighbours rather than at the end, so the `nn` order still tracks
registration order: **RT-20a** (`GET /requests/{id}/attachments/{attachmentID}`)
and **RT-78a** (`POST /requests/{id}/reassign-approver`). See the correction in
§0 for why "97" is no longer the count.

### 1.1 Public and session-only routes

| ID | Method | Path | Route gate | CSRF | Handler | Handler-level extra checks | Notes |
|---|---|---|---|---|---|---|---|
| RT-01 | GET | `/static/` | **public** | n/a | `http.FileServer(http.Dir("web/static"))` | none | `app.go:363`. No auth wrapper at all. Serves `fervid-ds.css`, `htmx.min.js`, `fervid-logo.svg`, `fervid-app.js` and **`_kitchensink.html`**. No `index.html` present, so `GET /static/` returns a **directory listing** to an anonymous caller. |
| RT-02 | GET | `/login` | **public** | n/a | `loginForm` | none | `app.go:364`. Renders the login page even for a signed-in caller — no "already logged in" redirect. |
| RT-03 | POST | `/login` | **public** | **NO** | `loginPost` | lockout check `LoginLocked`; `CheckPassword`; `u.Active` | `app.go:365, 584-618`. **The only state-changing POST with no `withCSRF` wrapper.** Also therefore no 21 MiB body cap. Bad creds → 200 (login template with `Error`). Locked → 200. Success → `Login()` sets `fervid_session` + `fervid_csrf`, then 303→`/`. |
| RT-04 | POST | `/logout` | **none** (not even `RequireLogin`) | yes | `logoutPost` | tolerates `u.ID == 0` (`app.go:621-624`) | `app.go:366, 620-627`. Anonymous caller with a `fervid_csrf` cookie and matching token → 303→`/login`. Without the cookie → 403. |
| RT-05 | GET | `/{$}` | `RequireLogin` | n/a | `dashboard` | per-area `Can` checks (`dashboard.go:70,82,94,101`) | `app.go:375`. `{$}` matches **only** `/`. Home is the dashboard. |
| RT-06 | GET | `/` | `RequireLogin` | n/a | `notFound` | none | `app.go:376`, `http_errors.go:158-160`. The **catch-all**: in Go 1.22+ ServeMux, `GET /` matches every unclaimed GET path, and this handler answers a rendered 404 page. It exists precisely so an unbuilt screen admits it is missing instead of silently rendering the dashboard (`app.go:367-374`). |
| RT-07 | GET | `/grid` | ~~**`RequireLogin` only**~~ **`grid:view`** | n/a | `grid` | ~~**none**~~ Recent Payments gated separately on `payment:view` **in the handler** | `app.go:377, 629-649`. ~~**No `grid:view` gate.** Any signed-in caller gets the whole budget-vs-actuals matrix *and* the "Recent Payments" table with amounts, payees and `/payments/{id}` links.~~ **Fixed after this document was written (Wave 3, `1fac147`, F-A-02/F-G-032): the route asks for `grid:view` (`app.go:431`) and the panel asks for `payment:view` rather than trusting a template (`app.go:426-430`).** See F-02, and the correction in §0. |
| RT-08 | GET | `/dashboard` | `RequireLogin` | n/a | `dashboard` | same as RT-05 | `app.go:381`. A second address for the same handler; the comment at `app.go:378-380` says it is ungated by design because every area inside is gated. |
| RT-30 | GET | `/notifications` | `RequireLogin` | n/a | `notificationCentre` | store scopes every row to `user.ID` (`inapp.go:39`) | `app.go:411`. Ungated by design (`app.go:408-410`). |
| RT-31 | POST | `/notifications/read` | `RequireLogin` | yes | `notificationsMarkAllRead` | acts on `user.ID` only (`inapp.go:56`) | `app.go:412`. |
| RT-32 | GET | `/notifications/{id}/open` | `RequireLogin` | n/a | `notificationOpen` | `st.Notification(ctx, user.ID, id)` — not-yours ≡ not-found → 404 (`inapp.go:76-81`) | `app.go:413`. **A GET that mutates**: it writes a read receipt (`MarkNotificationRead`, `inapp.go:86`). Justified in-code at `inapp.go:63-72`. Redirect target comes from the stored row and must start `/` (`inapp.go:90-93`), so it is not an open-redirect. |
| RT-60 | GET | `/requests/{id}/reservation` | **`RequireLogin` only** | n/a | `reservationForm` | `loadViewableRequest`; `status=='processing' && processing_by!=nil` else 409; **`(mine && reservation:release) \|\| reservation:reassign`** else 403 (`linking.go:521-538`) | `app.go:464`. Deliberate: the route asks only for a session because release and reassign are independent cells and middleware can ask for one verb, not for either (`app.go:456-463`, PROGRESS.md:350-354). |

### 1.2 Payment requests

| ID | Method | Path | Route gate | CSRF | Handler | Handler-level extra checks | Notes |
|---|---|---|---|---|---|---|---|
| RT-51 | GET | `/requests` | `request:view` | n/a | `requests` | `effectiveScope` — a `?scope=` may only *narrow* (`requests.go:347-354`) | `app.go:441`. |
| RT-52 | GET | `/requests/export.csv` | `request:view` | n/a | `requestsExport` | `effectiveScope` (`requests.go:929`) | `app.go:442`. Literal final segment beats `{id}`. Writes an `export` audit row (`requests.go:936`). |
| RT-53 | GET | `/requests/new` | `request:create` | n/a | `requestNew` | unknown `?type=` → the chooser (`requests.go:71-77`) | `app.go:443`. |
| RT-54 | GET | `/requests/new/fields` | `request:create` | n/a | `requestFormFields` | none | `app.go:444`. htmx fragment. |
| RT-55 | POST | `/requests/duplicate-check` | `request:create` | yes | `requestDuplicateCheck` | advisory only; errors swallowed (`requests.go:266-269`); nothing matched → empty 200 | `app.go:445`. G6: never a gate on submit. |
| RT-56 | POST | `/requests` | `request:create` | yes | `requestCreate` | `store.CreateRequest` → `validateRequestInput` incl. **G8 self-approver refusal** (`store/requests.go:143`) | `app.go:446`. D1: create *and* submit in one POST. |
| RT-57 | GET | `/requests/{id}/submitted` | `request:view` | n/a | `requestSubmitted` | `loadViewableRequest` (`requests.go:219`) | `app.go:447`. |
| RT-67 | GET | `/requests/{id}/partial-review` | `request:view` | n/a | `requestPartialReview` | `loadViewableRequest`; status ≠ `partial_review` → **303→`/requests/{id}`**; no linked payment → 404 (`linking.go:377-401`) | `app.go:482`. Reading is `request:view` for everyone on the thread; both *decisions* are RT-68/69. |
| RT-68 | POST | `/requests/{id}/accept-partial` | `approval:accept_partial` | yes | `requestAcceptPartial` | **store `AcceptPartial`: `before.ManagerID != actor.ID` → ErrForbidden** (`store/store.go:980-982`); status must be `partial_review` (`store.go:983-991`) | `app.go:483`. |
| RT-69 | POST | `/requests/{id}/raise-concern` | `approval:accept_partial` | yes | `requestRaiseConcern` | store `RaiseConcern`: comment required (400) → then `ManagerID != actor.ID` → ErrForbidden → then status (`store.go:1004-1026`) | `app.go:484`. |
| RT-70 | GET | `/requests/{id}` | `request:view` | n/a | `requestDetail` | `loadViewableRequest`; `showsReturnedCorrection` swaps to the correction template for the requester holding `request:edit` (`requests.go:534-537`) | `app.go:488`. |
| RT-71 | GET | `/requests/{id}/edit` | `request:edit` | n/a | `requestEditForm` | `loadEditableRequest`: view scope → **`RequesterID != actor` → 403** → status ∉ {pending, returned} → 400 (`requests.go:587-602`) | `app.go:489`. |
| RT-72 | POST | `/requests/{id}/edit` | `request:edit` | yes | `requestEdit` | `loadEditableRequest`, then store `UpdateRequest` re-checks requester (`store/requests.go:610-611`) | `app.go:490`. `submit_action=resubmit` also calls `SubmitRequest`. |
| RT-73 | POST | `/requests/{id}/comment` | `request:comment` | yes | `requestComment` | `loadViewableRequest` only. Store `AddRequestComment` checks **existence only** — no owner check (`store/requests.go:1001-1013`) | `app.go:491`. `return_to=partial-review` is a token, not a URL (`requests.go:778-784`). |
| RT-74 | POST | `/requests/{id}/withdraw` | `request:withdraw` | yes | `requestWithdraw` | **no `loadViewableRequest`**; store `WithdrawRequest`: `RequesterID != actor` → ErrForbidden (`store/requests.go:655-656`) | `app.go:492`. Missing id → 404 from the store. |
| RT-75 | POST | `/requests/{id}/reraise` | `request:reraise` | yes | `requestReraise` | **no `loadViewableRequest`**; store: `RequesterID != actor` → ErrForbidden; status must be `rejected` (`store/requests.go:815-819`) | `app.go:493`. Redirects to the **new** request's confirmation. |
| RT-76 | POST | `/requests/{id}/approve` | `approval:approve` | yes | `requestApprove` | `ParsePaise(approved_amount)` → 400 **first** (`requests.go:717-721`); store: `ManagerID != actor` → 403; **G8 `RequesterID == actor` → 403** (`store/requests.go:687-694`) | `app.go:494`. |
| RT-77 | POST | `/requests/{id}/return` | `approval:return` | yes | `requestReturn` | store `decideRequest`: empty comment → 400 **first**, then `ManagerID != actor` → 403 (`store/requests.go:714-733`) | `app.go:495`. |
| RT-78 | POST | `/requests/{id}/reject` | `approval:reject` | yes | `requestReject` | same as RT-77 | `app.go:496`. |
| RT-78a | POST | `/requests/{id}/reassign-approver` | **`approval:reassign`** | yes | `requestReassignApprover` | **Route added after this document was written.** Fields `manager_id`, `reason`. Order: out-of-scope → **404**; `manager_id==0` → 400 *"Choose the approver this request should go to."*; blank `reason` → 400 *"Give a reason for the reassignment."*; then store `ReassignRequest` — status must be `pending`, G8 forbids the requester as target (`app.go:1615-1647`, `store/requests.go:759-801`) | `app.go:569`. The path is deliberately **not** `/reassign` — that is RT-62, the *reservation* handler on `reservation:reassign` (`app.go:564-568`). Closes GR-11 and F-09. |
| RT-79 | GET | `/requests/{id}/cancel` | `request:cancel` | n/a | `requestCancelForm` | `loadViewableRequest`; **`RequesterID != actor` → 403**; status ≠ `approved` → 400 (`requests.go:797-815`) | `app.go:501`. |
| RT-80 | POST | `/requests/{id}/cancel-request` | `request:cancel` | yes | `requestCancelAsk` | store `RequestCancellation`: empty reason → 400 first, then `RequesterID != actor` → 403 (`store/requests.go:861-880`) | `app.go:502`. |
| RT-81 | POST | `/requests/{id}/cancel` | `approval:cancel` | yes | `requestCancelOutright` | store `CancelRequest`: reason → 400 first, then `ManagerID != actor` → 403 (`store/requests.go:962-981`) | `app.go:503`. Same path as RT-79 on a different method — ServeMux gates each method on its own verb (`app.go:497-500`). |
| RT-82 | GET | `/requests/{id}/cancellation` | `approval:cancel` | n/a | `requestCancellationForm` | `loadViewableRequest`; **`ManagerID != actor` → 403** (`requests.go:837-841`) | `app.go:504`. |
| RT-83 | POST | `/requests/{id}/cancellation` | `approval:cancel` | yes | `requestCancellationDecide` | store `DecideCancellation`: decline with no note → 400 first, then `ManagerID != actor` → 403, then status must be `cancellation_requested` (`store/requests.go:909-936`) | `app.go:505`. |
| RT-84 | GET | `/approvals` | `approval:approve` | n/a | `approvals` | **scope hard-coded `"assigned"`** — the caller's own `request` scope is ignored (`requests.go:896`) | `app.go:508`. |

### 1.3 Reservation, settlement, accounts queue

| ID | Method | Path | Route gate | CSRF | Handler | Handler-level extra checks | Notes |
|---|---|---|---|---|---|---|---|
| RT-58 | POST | `/requests/{id}/record-payment` | `reservation:reserve` | yes | `requestRecordPayment` | store `ReserveRequest` conditional UPDATE `status='approved' AND processing_by IS NULL AND on_hold=0`; 0 rows → ErrForbidden → **`reservationConflict` 409** (`linking.go:82-99`, `store/store.go:730-753`) | `app.go:451`. Gated on the reservation verb, not `payment:create` (`app.go:448-450`). The conflict branch re-reads the request with **`a.st.Request` and no scope check** (`linking.go:87`). |
| RT-59 | POST | `/requests/{id}/settlement-preview` | `payment:settle` | yes | `settlementPreview` | **no `loadViewableRequest`** — plain `a.st.Request` (`linking.go:283`); `!heldByCaller` → 409; bad amount → re-rendered sheet at 400 | `app.go:454`. Pure read despite being POST (D8, `app.go:452-453`, `linking.go:280-309`). |
| RT-61 | POST | `/requests/{id}/release` | `reservation:release` | yes | `requestRelease` | `loadViewableRequest`; store `ReleaseRequest(reason, confirmed, **authorized = Can(reservation:reassign)**)`; order: `!confirmed`→400, empty reason→400, status≠processing→403, `!authorized && processing_by != actor`→403 (`linking.go:623-635`, `store/store.go:761-795`) | `app.go:465`. |
| RT-62 | POST | `/requests/{id}/reassign` | `reservation:reassign` | yes | `requestReassign` | `loadViewableRequest`; `to_user_id==0`→400; `confirm!=on`→400; target `canWorkTheQueue` (needs `payment:process`)→400; store `ReassignReservation(authorized)` fails closed without the grant (`linking.go:642-675`, `store/store.go:802-858`) | `app.go:466`. |
| RT-63 | GET | `/requests/{id}/reservation/stale` | `payment:process` | n/a | `requestStale` | `loadViewableRequest`; status ≠ processing → **409** (`linking.go:730-740`) | `app.go:470`. Q6, the 26-hour nudge. Reads nothing it can change. |
| RT-64 | POST | `/requests/{id}/hold` | `payment:hold` | yes | `requestHold` | `loadViewableRequest`; store `HoldRequest`: reason → 400 first; conditional UPDATE `status='approved' AND on_hold=0`, 0 rows → **403** (`store/store.go:1037-1059`) | `app.go:474`. L7: one grant for both directions. |
| RT-65 | POST | `/requests/{id}/unhold` | `payment:hold` | yes | `requestUnhold` | `loadViewableRequest`; conditional UPDATE `on_hold=1`, 0 rows → **403** (`store/store.go:1064-1082`) | `app.go:475`. |
| RT-66 | GET | `/accounts-queue` | `payment:process` | n/a | `accountsQueue` | unknown `?tab=` → 400 (`linking.go:147-156`); rows filtered by `Scope(u,"request")` (`linking.go:159`) | `app.go:476`. |

### 1.4 Payments and attachments

| ID | Method | Path | Route gate | CSRF | Handler | Handler-level extra checks | Notes |
|---|---|---|---|---|---|---|---|
| RT-11 | GET | `/payments/new` | `payment:create` | n/a | `paymentForm` | no `?request=` → picker, scoped by `Scope(u,"request")` (`linking.go:171-185`); with `?request=` → `paymentEntry`, `!heldByCaller` → **409** (`linking.go:233-243`) | `app.go:384`. |
| RT-12 | GET | `/payments/new/options` | `payment:create` | n/a | `paymentPickerOptions` | same picker scope | `app.go:385`. htmx fragment. |
| RT-13 | GET | `/payments` | `payment:view` | n/a | `payments` | ~~**none** — `ListPayments` takes no scope argument~~ **`Scope(u,"payment")` + `ViewerID` are passed to `ListPayments`, which filters on `entered_by` for `own`/`assigned` (`app.go:793-805`, `store/store.go:1501`)** | `app.go:386`. ~~The declared `payment` data scope is never consulted.~~ **Fixed after this document was written (Waves 1 and 3 — the store filter and the handler that passes it are separate halves).** See F-04. |
| RT-14 | POST | `/payments` | `payment:create` **+ `payment:settle`** | yes | `paymentCreate` | `request_id==0` → 400; **`settlement=partial` without `payment:mark_partial` → 403 (`app.go:831-835`)**; store `RecordPaymentForRequest` requires `status='processing' AND processing_by = actor` → ErrForbidden; `amount > approved` → 400 (G13) (`app.go:690-729`, `store/store.go:866-957`) | `app.go:387`. On any error, if a payment already exists for the request it redirects there (double-submit tolerance, `app.go:713-716`). **Second gate added after this document was written (Wave 3, `1fac147`, F-D-10): the write was gated more weakly than its own pure preview RT-59, so the route is now wrapped twice (`app.go:446-447`).** |
| RT-15 | GET | `/payments/{id}` | `payment:view` | n/a | `paymentDetail` | **linked payment: `canViewRequest(Scope(u,"request"), u, req)` → 403** (`app.go:757-762`). Unlinked historical payment: no extra check. | `app.go:388`. |
| RT-16 | GET | `/payments/{id}/edit` | `payment:edit` | n/a | `paymentEditForm` | `RequestID != nil` → **303→`/payments/{id}`** (S12, `app.go:803-806`) | `app.go:389`. |
| RT-17 | POST | `/payments/{id}/edit` | `payment:edit` | yes | `paymentEdit` | store: linked → ErrValidation → **400**; voided → 400; locked month → 409 (`store/store.go:607-617`) | `app.go:390`. |
| RT-18 | POST | `/payments/{id}/void` | `payment:void` | yes | `paymentVoid` | store: linked → ErrValidation → **400**; already voided → no-op 303; empty reason → 400; locked → 409 (`store/store.go:648-660`) | `app.go:391`. `next` is prefix-checked against `/payments` (`app.go:854-857`). |
| RT-19 | POST | `/payments/{id}/attachments` | `attachment:create` | yes | `attachmentUpload` | **no ownership check of any kind**; store `AddAttachment` checks existence, not-voided, month-unlocked only (`store/store.go:1310-1334`) | `app.go:392`. See F-03. |
| RT-20 | GET | `/attachments/{id}` | `attachment:view` | n/a | `attachmentDownload` | ~~**no ownership check**; only a path-traversal guard (`app.go:892-897`) and existence (`app.go:898-905`)~~ **Fixed after this document was written — see the correction below.** | `app.go:393`. See F-01. |
| RT-20a | GET | `/requests/{id}/attachments/{attachmentID}` | `attachment:view` | n/a | `requestAttachmentDownload` | **Route added after this document was written.** Reads `request_attachments`; `att.RequestID != pathID(r)` → 404; then `canViewRequest(Scope(u,"request"), u, req)` → 404 (`app.go:1115-1150`) | `app.go:460`. See the correction below. |

> **Correction — RT-20 split in two and both halves now check ownership.** At
> `30edd6a` a single route served both attachment tables and checked nothing but
> the path and existence, which is what F-01 below records. Wave 3 (`1fac147`)
> fixed it as F-A-01/F-B-11 and F-A-05/F-B-09:
>
> - **RT-20**, `GET /attachments/{id}`, now serves **payment** attachments only.
>   It reads `AttachmentWithPayment` and refuses unless `canReadPayment`
>   (`internal/app/app.go:1091-1113`).
> - **RT-20a**, `GET /requests/{id}/attachments/{attachmentID}`, is new and
>   serves **request** documents from `request_attachments`. It refuses an
>   attachment that does not belong to the `{id}` in the path, then applies the
>   request data scope (`internal/app/app.go:1115-1150`).
>
> Both refusals are **404**, not 403, and both are logged: `notFoundAttachment`
> emits *"The requested attachment was not found."* with a `WARN attachment
> refused` line carrying the reason (`internal/app/app.go:1044-1048`). The two
> tables were split because their id sequences are unrelated, so one URL space
> handed one reader another reader's bank advice — the route comment says so at
> `internal/app/app.go:453-458`. Consequences below: **PE-01**, **PM-20** and
> **F-01** all describe the pre-fix behaviour.

### 1.5 Budget, grid, reports, recoverables

| ID | Method | Path | Route gate | CSRF | Handler | Handler-level extra checks | Notes |
|---|---|---|---|---|---|---|---|
| RT-09 | GET | `/months` | `month:view` | n/a | `months` | none | `app.go:382`. |
| RT-10 | POST | `/months` | `month:create` | yes | `monthCreate` | store `CreateMonthPlan`; failure → months page at 400 | `app.go:383`. |
| RT-21 | GET | `/export.csv` | `grid:export` | n/a | `exportGrid` | none; writes an `export` audit row | `app.go:394`. Note the asymmetry with RT-07: the CSV of the grid is gated, the grid itself is not. |
| RT-22 | GET | `/reports/monthly` | `report:view` | n/a | `report` | mode from the path (`app.go:1428`) | `app.go:395`. |
| RT-23 | GET | `/reports/projects` | `report:view` | n/a | `report` | as RT-22 | `app.go:396`. |
| RT-24 | GET | `/reports/heads` | `report:view` | n/a | `report` | as RT-22 | `app.go:397`. |
| RT-25 | GET | `/reports/ytd.csv` | `report:export` | n/a | `exportYTD` | none | `app.go:398`. |
| RT-26 | GET | `/recoverables` | `recoverable_report:view` | n/a | `recoverablesDashboard` | ~~none~~ → the caller's `request` scope is applied to every aggregate: `recoverableViewer` (`recoverables.go:117-120`) into `RecoverableMetrics` and both `RecoverableRollups` (`recoverables.go:31-42`) | `app.go:403`. |
| RT-27 | GET | `/recoverables/list` | `recoverable_report:view` | n/a | `recoverablesList` | `ageing`/`order` allow-lists (`recoverables.go:48-71`); ~~no scope~~ → rows filtered by `recoverableScope` in SQL (`recoverables.go:122`, `store/recoverables.go:347,401`) | `app.go:404`. |
| RT-28 | GET | `/recoverables/list.csv` | `recoverable_report:export` | n/a | `exportRecoverable` | ~~none~~ → same scope as RT-27 (`recoverables.go:145`); writes an `export` audit row | `app.go:405`. Literal segment beats `{id}` (`app.go:400-402`). |
| RT-29 | GET | `/recoverables/{id}` | `recoverable_report:view` | n/a | `recoverableDetail` | `treatment != "recoverable"` → 404. ~~**No `canViewRequest`** — it loads the full request, its payment and its whole thread with `a.st.Request` (`recoverables.go:130-172`)~~ → `canViewRequest` → **404** (`recoverables.go:192-198`) | `app.go:406`. See F-05. |
| RT-37 | GET | `/budgets` | `budget:view` | n/a | `budgets` | none | `app.go:421`. |
| RT-38 | POST | `/budgets` | `budget:edit` | yes | `budgetSave` | invalid month → 400; per-head parse errors → 400; locked month → 409 | `app.go:422`. |
| RT-39 | POST | `/months/{month}/lock` | `month:lock` | yes | `lockMonth` | store `LockMonth` | `app.go:423`. |
| RT-40 | POST | `/months/{month}/unlock` | `month:lock` | yes | `unlockMonth` | store `UnlockMonth` | `app.go:424`. One grant for both directions. |

### 1.6 Masters

| ID | Method | Path | Route gate | CSRF | Handler | Handler-level extra checks | Notes |
|---|---|---|---|---|---|---|---|
| RT-41 | GET | `/projects` | `project:view` | n/a | `projects` | none | `app.go:425`. |
| RT-42 | POST | `/projects` | **`project:edit`** | yes | `projectSave` | `UpsertProject` — `id==0` means **create** | `app.go:426`. `project:create` is never checked. |
| RT-43 | GET | `/heads` | `head:view` | n/a | `heads` | none | `app.go:427`. |
| RT-44 | POST | `/heads` | **`head:edit`** | yes | `headSave` | `UpsertHead` — `id==0` means **create** | `app.go:428`. `head:create` is never checked. |
| RT-45 | GET | `/vendors` | `vendor:view` | n/a | `vendorsList` | perms handed to the store; bank columns are **not selected** without `vendor_bank:view` (`store/vendors.go:130-149`) | `app.go:431`. |
| RT-46 | GET | `/vendors/new` | `vendor:create` | n/a | `vendorNew` | empty `Bank` block only for `vendor_bank:view` (`vendors.go:73-75`) | `app.go:432`. |
| RT-47 | GET | `/vendors/search` | `vendor:view` | n/a | `vendorSearch` | `SearchVendors` selects `vendorColumns` only — **never bank** (`store/vendors.go:349`) | `app.go:433`. |
| RT-48 | GET | `/vendors/{id}` | `vendor:view` | n/a | `vendorDetail` | `VendorEditable = Can(vendor,edit)`; bank via perms | `app.go:434`. |
| RT-49 | POST | `/vendors` | `vendor:create` | yes | `vendorCreate` | bank block read only for `vendor_bank:edit` (`vendors.go:172-184`) | `app.go:435`. |
| RT-50 | POST | `/vendors/{id}` | `vendor:edit` | yes | `vendorUpdate` | bank gate applied twice — form-read and `UpdateVendor(perms)` (`vendors.go:147-150`) | `app.go:436`. |

### 1.7 Administration

| ID | Method | Path | Route gate | CSRF | Handler | Handler-level extra checks | Notes |
|---|---|---|---|---|---|---|---|
| RT-33 | GET | `/admin/notifications` | `notification:view` | n/a | `adminNotifications` | none | `app.go:417`. Distinct from RT-30 (D7). |
| RT-34 | POST | `/admin/notifications/smtp` | `notification:edit` | yes | `adminNotificationsSMTP` | store validation | `app.go:418`. SMTP password is env-only, never posted. |
| RT-35 | POST | `/admin/notifications/events/{event}` | `notification:edit` | yes | `adminNotificationEventSave` | `notify.ValidateTemplate` on subject + body → 400 (`notifications.go:109-114`) | `app.go:419`. |
| RT-36 | POST | `/admin/notifications/test` | `notification:edit` | yes | `adminNotificationsTest` | send failure → re-render at **502** (`notifications.go:146`) | `app.go:420`. Sends to `test_to`, defaulting to the caller's own address. |
| RT-85 | GET | `/users` | `user:view` | n/a | `users` | none | `app.go:509`. |
| RT-86 | POST | `/users` | **`user:edit`** (route) **+ `user:create` in the handler when `id==0`** | yes | `userSave` | **`id==0` → the handler demands `user:create`, else `user:edit`, → 403 (`app.go:1523-1535`)**; `id==0` → **create** (needs a password, 400 without); ≥12-char password rule, delegated to `auth.ValidatePassword` so every refusal names the rule (`app.go:1555-1560`); self-demotion refused → 400 (`app.go:1192-1196`); `SetUserRoles`, `SetUserDefaultApprover` (self-approver refused, `store/permissions.go:517-519`) — **all three writes now run in one `beginWriteTx` envelope, `store.SaveUser` (F-G-034)** | `app.go:510`. ~~`user:create` is never checked.~~ **Fixed after this document was written (Wave 3, `1fac147`, F-A-07 · F-A-11/F-G-036 · F-G-034).** |
| RT-87 | GET | `/roles` | `role:view` | n/a | `rolesPage` | none | `app.go:511`. |
| RT-88 | POST | `/roles` | `role:edit` | yes | `rolesSave` | pre-screens every expanded grant with `store.ValidGrant` → 400 (`app.go:1321-1326`); `UpdateRole` refuses renaming a system role → 403; `UpdateRolePermissions` re-validates (`store/permissions.go:366-376`) | `app.go:512`. `expandCells` cannot invent a grant (`permmap.go:274-295`). |
| RT-89 | POST | `/roles/new` | `role:create` | yes | `roleCreate` | empty name → 400; duplicate name → 400 | `app.go:513`. |
| RT-90 | POST | `/roles/{id}/copy` | `role:create` | yes | `roleCopy` | copies grants + scopes; new role is non-system | `app.go:514`. |
| RT-91 | POST | `/roles/{id}/delete` | `role:delete` | yes | `roleDelete` | **system role → ErrForbidden → 403** (`store/permissions.go:318-320`) | `app.go:515`. |
| RT-92 | GET | `/configuration` | `config:view` | n/a | `configuration` | none | `app.go:516`. Renders the recoverable-category fieldset too. |
| RT-93 | POST | `/configuration` | `config:edit` | yes | `configurationSave` | writes **only** keys declared by a registered `ConfigField` (`configuration.go:142-154`) | `app.go:517`. |
| RT-94 | POST | `/configuration/recoverable-categories` | **`recoverable_category:edit`** | yes | `recoverableCategorySave` | unknown `requires` value → 400 (`configuration.go:124-127`); `id==0` → **create** | `app.go:520`. Screen is `config:view`; the mutation keeps its own Phase-1 verb (`app.go:518-519`). |
| RT-95 | GET | `/audit` | `audit:view` | n/a | `auditLog` | none | `app.go:521`. |
| RT-96 | GET | `/backups` | `backup:view` | n/a | `backups` | unreadable dir → 500 | `app.go:522`. |
| RT-97 | POST | `/backups` | `backup:create` | yes | `backupCreate` | `store.Backup` + `PruneBackups` | `app.go:523`. |

### 1.8 The catch-all, 405 versus 404, and GET-on-a-POST-route

`routes()` registers `GET /` (RT-06) but **no** `POST /`. Go 1.22+ ServeMux
resolves a request in two steps: it finds the patterns whose *path* matches, then
checks the method. If some pattern matches the path but none matches the method,
it answers **405 Method Not Allowed** with an `Allow` header, and **no
registered handler runs** — which means no auth, no CSRF, no rendered error page.

Consequences a tester must encode:

| ID | Request | Result | Why |
|---|---|---|---|
| MX-1 | `POST /no-such-page` | **405** | `GET /` matches the path; POST does not match any method. Confirmed by PROGRESS.md:288-290 and `app_integration_test.go:1537-1547`, which accepts 404 *or* 405 for proof-of-absence. |
| MX-2 | `GET /no-such-page` (signed in) | **404** | `GET /` matches → `RequireLogin` → `notFound` → rendered 404. |
| MX-3 | `GET /no-such-page` (anonymous) | **303→/login** | `RequireLogin` fires before `notFound`. A 404 probe therefore *requires* a session. |
| MX-4 | `GET /requests/7/approve` (a POST-only route) | **404** (signed in) / 303→/login (anon) | `POST /requests/{id}/approve` does not match GET; `GET /` does. The specific pattern's gate never runs. |
| MX-5 | `POST /requests` vs `POST /requests/7` | 200-family vs **405** | `POST /requests` is registered (RT-56); `POST /requests/{id}` is not, and `GET /requests/{id}` matches the path. |
| MX-6 | `POST /grid`, `POST /audit`, `POST /roles/7/copy/extra` | **405**, **405**, **405** | same rule. |
| MX-7 | `DELETE /roles/7` | **405** | no DELETE pattern is registered anywhere in the app. |
| MX-8 | `GET /logout` | **404** | only `POST /logout` exists; `GET /` catches it. |

---

## 2. Expected-status matrix

`PM-nn` ↔ `RT-nn`. Every cell is the outcome for a caller holding **exactly one**
seeded role, acting on the target object defined in §0.3 which they do **not**
own.

> **CSRF assumption for every POST row.** Cells assume a **valid** token: the
> caller holds a `fervid_csrf` cookie and sends the same value as the `csrf` form
> field or the `X-CSRF-Token` header (`auth.go:167-180`). With a missing, stale
> or cross-session token the cell becomes **403** *"Your form session expired"* —
> **but only if the caller already passed the route gate**, because `withCSRF` is
> nested inside `RequirePermission`. For every cell that already reads `403`, a
> bad token changes the message and not the status, so a CSRF test must assert on
> the body, not the code.

### 2.1 Public and session-only

| ID | Route | C-anon | C-req | C-mgr | C-acc | C-adm |
|---|---|---|---|---|---|---|
| PM-01 | `GET /static/…` | 200 | 200 | 200 | 200 | 200 |
| PM-02 | `GET /login` | 200 | 200 | 200 | 200 | 200 |
| PM-03 | `POST /login` (wrong password) | 200 ᵃ | 200 ᵃ | 200 ᵃ | 200 ᵃ | 200 ᵃ |
| PM-04 | `POST /logout` | 403 ᵇ | 303→/login | 303→/login | 303→/login | 303→/login |
| PM-05 | `GET /` (dashboard) | 303→/login | 200* ᶜ | 200* ᶜ | 200* ᶜ | 200* ᶜ |
| PM-06 | `GET /anything-unrouted` | 303→/login | 404 | 404 | 404 | 404 |
| PM-07 | `GET /grid` | 303→/login | ~~**200**~~ **403** ᵈ | 200* ᵈ | 200 | 200 |
| PM-08 | `GET /dashboard` | 303→/login | 200* ᶜ | 200* ᶜ | 200* ᶜ | 200* ᶜ |
| PM-30 | `GET /notifications` | 303→/login | 200* ᵉ | 200* ᵉ | 200* ᵉ | 200* ᵉ |
| PM-31 | `POST /notifications/read` | 303→/login | 303→/notifications ᶠ | 303 ᶠ | 303 ᶠ | 303 ᶠ |
| PM-32 | `GET /notifications/{OBJ-NOTIF}/open` | 303→/login | 404 | 404 | 404 | 404 |
| PM-60 | `GET /requests/{OBJ-REQ}/reservation` | 303→/login | 403 ᵍ | **403** ʰ | **403** ⁱ | 200 ʲ |

ᵃ `loginPost` re-renders the `login` template with `Error` set; the status stays
200 (`app.go:594`, `app.go:609`). A *correct* password gives 303→`/`. The `POST`
carries **no CSRF check** (RT-03).
ᵇ No `fervid_csrf` cookie → `CheckCSRF` false → 403. If the caller first did
`GET /login`, `EnsureCSRF` set the cookie (`http_errors.go:103`) and the result
is 303→`/login`.
ᶜ 200 always; the *number of work areas* differs — `dashboard.go:70/82/94/101`
gates each area on `request:create`, `approval:approve`, `payment:process` and
the four `adminLinks` verbs. C-req sees 2 areas, C-mgr 2, C-acc 1, C-adm all + the
admin block. An area with a zero count is not rendered at all.
ᵈ ~~**Defect F-02.** No `grid:view` gate. C-req has no `grid:view` and still
receives the full budget matrix plus a "Recent Payments" table with amounts,
payees and links into `/payments/{id}`.~~ **Fixed after this document was
written (Wave 3, `1fac147`, F-A-02/F-G-032).** `GET /grid` is gated on
`grid:view` (`app.go:431`), which C-req does not hold → **403**. C-mgr, C-acc
and C-adm all hold it → 200. Recent Payments is a second gate inside the
handler, on `payment:view` (`app.go:426-430`): C-mgr holds `grid:view` but not
`payment:view`, so a Manager gets 200 with the matrix and **without** the
payments panel — the one cell in this column where 200 does not mean "the whole
page". See F-02.
ᵉ Rows filtered to `UserID` by the store (`inapp.go:39`).
ᶠ Marks only the caller's own rows read.
ᵍ `loadViewableRequest` → scope `own`, `T.requester_id ≠ caller` → ~~403~~ **404**
(changed after this document was written — F-G-002, see the §3 correction; every
403 in this table that is a *scope* refusal is now a 404, while the ownership and
route-gate 403s below are unchanged)
(`requests.go:236-239`).
ʰ Scope `all` passes; then `!(mine && release) && !reassign` → 403
(`linking.go:534-537`). C-mgr holds neither reservation verb.
ⁱ Scope `all` passes; C-acc holds `reservation:release` but is **not** the holder
(`mine==false`) and lacks `reassign` → 403.
ʲ C-adm holds `reassign` → the screen renders with the reassign target list. If
`T.status ≠ processing` the answer is **409** *"This request is not reserved by
anyone."* — and that 409 is emitted **before** the verb check
(`linking.go:527-530`), so any in-scope caller can distinguish reserved from
unreserved.

### 2.2 Payment requests

| ID | Route | C-anon | C-req | C-mgr | C-acc | C-adm |
|---|---|---|---|---|---|---|
| PM-51 | `GET /requests` | 303→/login | 200* ᵏ | 200* ˡ | 200* ˡ | 200* ˡ |
| PM-52 | `GET /requests/export.csv` | 303→/login | 200* ᵏ | 200* ˡ | 200* ˡ | 200* ˡ |
| PM-53 | `GET /requests/new` | 303→/login | 200 | 403 | 403 | 200 |
| PM-54 | `GET /requests/new/fields` | 303→/login | 200 | 403 | 403 | 200 |
| PM-55 | `POST /requests/duplicate-check` | 303→/login | 200 ᵐ | 403 | 403 | 200 ᵐ |
| PM-56 | `POST /requests` | 303→/login | 303→/requests/{new}/submitted ⁿ | 403 | 403 | 303 ⁿ |
| PM-57 | `GET /requests/{OBJ-REQ}/submitted` | 303→/login | 403 | 200 | 200 | 200 |
| PM-67 | `GET /requests/{OBJ-REQ}/partial-review` | 303→/login | 403 | 200 ᵒ | 200 ᵒ | 200 ᵒ |
| PM-68 | `POST /requests/{OBJ-REQ}/accept-partial` | 303→/login | 403 | **403** ᵖ | 403 | **403** ᵖ |
| PM-69 | `POST /requests/{OBJ-REQ}/raise-concern` | 303→/login | 403 | **403** ᵖ | 403 | **403** ᵖ |
| PM-70 | `GET /requests/{OBJ-REQ}` | 303→/login | 403 | 200 | 200 | 200 |
| PM-71 | `GET /requests/{OBJ-REQ}/edit` | 303→/login | 403 | 403 † | 403 † | **403** ʳ |
| PM-72 | `POST /requests/{OBJ-REQ}/edit` | 303→/login | 403 | 403 † | 403 † | **403** ʳ |
| PM-73 | `POST /requests/{OBJ-REQ}/comment` | 303→/login | 403 | **303** ˢ | **303** ˢ | **303** ˢ |
| PM-74 | `POST /requests/{OBJ-REQ}/withdraw` | 303→/login | 403 ᵗ | 403 † | 403 † | 403 ᵗ |
| PM-75 | `POST /requests/{OBJ-REQ}/reraise` | 303→/login | 403 ᵗ | 403 † | 403 † | 403 ᵗ |
| PM-76 | `POST /requests/{OBJ-REQ}/approve` | 303→/login | 403 | **403** ᵘ | 403 † | **403** ᵘ |
| PM-77 | `POST /requests/{OBJ-REQ}/return` | 303→/login | 403 | 403 ᵛ | 403 † | 403 ᵛ |
| PM-78 | `POST /requests/{OBJ-REQ}/reject` | 303→/login | 403 | 403 ᵛ | 403 † | 403 ᵛ |
| PM-79 | `GET /requests/{OBJ-REQ}/cancel` | 303→/login | 403 | 403 † | 403 † | **403** ʳ |
| PM-80 | `POST /requests/{OBJ-REQ}/cancel-request` | 303→/login | 403 ʷ | 403 † | 403 † | 403 ʷ |
| PM-81 | `POST /requests/{OBJ-REQ}/cancel` | 303→/login | 403 | 403 ʷ | 403 † | 403 ʷ |
| PM-82 | `GET /requests/{OBJ-REQ}/cancellation` | 303→/login | 403 | **403** ˣ | 403 † | **403** ˣ |
| PM-83 | `POST /requests/{OBJ-REQ}/cancellation` | 303→/login | 403 | 403 ʷ | 403 † | 403 ʷ |
| PM-84 | `GET /approvals` | 303→/login | 403 | 200* ʸ | 403 | 200* ʸ |

ᵏ Scope `own`: only the caller's own rows. `?scope=all` is ignored —
`effectiveScope` narrows and never widens (`requests.go:347-354`).
ˡ Scope `all`: every request in the system.
ᵐ 200 with a rendered duplicate list, or 200 with an **empty body** when nothing
matched (`requests.go:270-272`). An internal failure is also 200-empty
(`requests.go:266-269`).
ⁿ A valid submission redirects. Invalid input re-renders the form at **400**
(`requests.go:164-170`); a self-approver `manager_id` is one of those 400s
(`store/requests.go:143`).
ᵒ 200 only while `T.status == "partial_review"`. Any other status →
**303→`/requests/{id}`** (`linking.go:386-389`). No linked payment → **404**
(`linking.go:394-397`).
ᵖ **The G-rule the brief flags.** C-mgr holds `approval:accept_partial` but is
not `T.manager_id`; C-adm holds every grant and is likewise not the manager.
`AcceptPartial`/`RaiseConcern` refuse both (`store/store.go:980-982`,
`store.go:1021-1023`). For RT-69 an empty `comment` yields **400 first**.
† Gate refusal: the role does not hold the verb at all.
ʳ Scope `all` lets C-adm read `T`, then `RequesterID != actor` → 403
(`requests.go:592-594`, `requests.go:802-806`).
ˢ **Succeeds.** `request:comment` + scope `all` is enough; `AddRequestComment`
checks existence only. Any Manager/Accounts/Admin can write into the
conversation of a request that is not theirs. Intended by "everyone on the thread
may follow what is happening", but worth an explicit test — see F-08.
ᵗ No `loadViewableRequest`; the store's owner check answers. Non-existent id →
**404**.
ᵘ 403 only when `approved_amount` parses. **Send a valid amount** — an empty or
malformed one is rejected at **400** by `ParsePaise` *before* the ownership check
(`requests.go:717-721`), which would mask the finding.
ᵛ Send a non-empty `comment`/`reason`: `decideRequest` validates the text at
**400 first**, then the manager identity (`store/requests.go:715-730`).
ʷ Send a non-empty `reason`: validated at **400** before ownership
(`store/requests.go:862-865`, `requests.go:963-966`).
ˣ Not `T.manager_id` → 403 (`requests.go:837-841`).
ʸ Scope is hard-coded `"assigned"` (`requests.go:896`), so the queue shows only
rows whose `manager_id` is the caller. `T` is **not** in it.

### 2.3 Reservation, settlement, accounts queue

| ID | Route | C-anon | C-req | C-mgr | C-acc | C-adm |
|---|---|---|---|---|---|---|
| PM-58 | `POST /requests/{OBJ-REQ}/record-payment` | 303→/login | 403 | 403 | **409** ᶻ | **409** ᶻ |
| PM-59 | `POST /requests/{OBJ-REQ}/settlement-preview` | 303→/login | 403 | 403 | **409** ᵃᵃ | **409** ᵃᵃ |
| PM-61 | `POST /requests/{OBJ-REQ}/release` | 303→/login | 403 | 403 | **403** ᵃᵇ | **303→/accounts-queue** ᵃᶜ |
| PM-62 | `POST /requests/{OBJ-REQ}/reassign` | 303→/login | 403 | 403 | 403 | 303 ᵃᵈ |
| PM-63 | `GET /requests/{OBJ-REQ}/reservation/stale` | 303→/login | 403 | 403 | 200 ᵃᵉ | 200 ᵃᵉ |
| PM-64 | `POST /requests/{OBJ-REQ}/hold` | 303→/login | 403 | 403 | **403** ᵃᶠ | **403** ᵃᶠ |
| PM-65 | `POST /requests/{OBJ-REQ}/unhold` | 303→/login | 403 | 403 | **403** ᵃᵍ | **403** ᵃᵍ |
| PM-66 | `GET /accounts-queue` | 303→/login | 403 | 403 | 200* ᵃʰ | 200* ᵃʰ |

ᶻ `T` is already reserved by `A2`, so the conditional UPDATE matches 0 rows →
`ErrForbidden` → the **G15 conflict screen** at 409 naming the holder
(`linking.go:86-93`, `linking.go:58-71`). On an *approved, unclaimed, unheld*
request the answer is 303→`/payments/new?request={id}`.
ᵃᵃ `!heldByCaller` → conflict screen at 409 (`linking.go:290-293`). Note there is
**no scope check on this route** — the conflict page prints `T`'s number, payee
and holder name for any id (F-06).
ᵃᵇ Requires `confirm=on` **and** a non-empty `reason`, or the answer is **400**
before authorization (`store/store.go:762-768`). With both present: C-acc's
`authorized` is `Can(reservation:reassign)` = false and it is not the holder →
403 (`store/store.go:785-787`).
ᵃᶜ C-adm holds `reservation:reassign`, so `authorized == true` and the release of
somebody else's reservation **succeeds** (`linking.go:629-630`).
ᵃᵈ Requires `to_user_id` (else 400), `confirm=on` (else 400) and a target holding
`payment:process` (else 400, `linking.go:665-668`), plus a non-empty `reason`
(else 400 in the store).
ᵃᵉ 200 while `T.status == processing`; otherwise **409** *"This request is not
reserved by anyone."* (`linking.go:737-740`).
ᵃᶠ `T` is `processing`, so `HoldRequest`'s conditional UPDATE
(`status='approved' AND on_hold=0`) matches 0 rows → `ErrForbidden` → 403. On an
approved unheld request the answer is **303** — and it succeeds regardless of
ownership, which is L7 by design.
ᵃᵍ `T` is not on hold → 0 rows → 403. On a held request → 303.
ᵃʰ Rows filtered by `Scope(u,"request")`; both C-acc and C-adm hold `all`, so `T`
is visible. Unknown `?tab=` → **400** (`linking.go:154`).

### 2.4 Payments and attachments

| ID | Route | C-anon | C-req | C-mgr | C-acc | C-adm |
|---|---|---|---|---|---|---|
| PM-11 | `GET /payments/new` | 303→/login | 403 | 403 | 200* ᵃⁱ | 200* ᵃⁱ |
| PM-12 | `GET /payments/new/options` | 303→/login | 403 | 403 | 200* ᵃⁱ | 200* ᵃⁱ |
| PM-13 | `GET /payments` | 303→/login | 403 | 403 | **200** ᵃʲ | **200** ᵃʲ |
| PM-14 | `POST /payments` (`request_id=OBJ-REQ`) | 303→/login | 403 | 403 | **403** ᵃᵏ | **403** ᵃᵏ |
| PM-15 | `GET /payments/{OBJ-PAY}` | 303→/login | 403 | 403 | 200 ᵃˡ | 200 ᵃˡ |
| PM-16 | `GET /payments/{OBJ-PAY}/edit` | 303→/login | 403 | 403 | 303→/payments/{id} | 303→/payments/{id} |
| PM-17 | `POST /payments/{OBJ-PAY}/edit` | 303→/login | 403 | 403 | **400** ᵃᵐ | **400** ᵃᵐ |
| PM-18 | `POST /payments/{OBJ-PAY}/void` | 303→/login | 403 | 403 | **400** ᵃᵐ | **400** ᵃᵐ |
| PM-19 | `POST /payments/{OBJ-PAY}/attachments` | 303→/login | **303** ᵃⁿ | 403 | 303 | 303 |
| PM-20 | `GET /attachments/{OBJ-ATT}` | 303→/login | ~~**200**~~ **404** ᵃᵒ | 403 | 200 | 200 |
| PM-21 | `GET /export.csv` | 303→/login | 403 | 403 | **403** ᵃᵖ | 200 |

ᵃⁱ Without `?request=`: the picker, rows filtered by `Scope(u,"request")`. With
`?request={OBJ-REQ}`: `!heldByCaller` → **409** conflict screen
(`linking.go:240-243`).
ᵃʲ Plain 200 — **the whole ledger, unfiltered**. `payments` never consults
`Scope(u,"payment")`; `ListPayments` has no scope parameter (F-04).
ᵃᵏ `RecordPaymentForRequest` requires `processing_by == actor` → `ErrForbidden` →
`settlementError` re-renders the settlement sheet at **403**
(`app.go:717`, `linking.go:341-364`, `store/store.go:898-900`). Watch out: if a
payment already exists for the request the handler redirects **303** to it
instead (`app.go:713-716`). With `request_id` absent → **400**.
ᵃˡ 200 because both roles hold `request` scope `all`, satisfying
`canViewRequest` (`app.go:757-762`). A role with `payment:view` and a narrower
request scope gets **403** *"You cannot see the request behind this payment."*
ᵃᵐ Linked payment → `ErrValidation` → 400 (S12, `store/store.go:609-611`,
`store.go:649-651`). On an *unlinked historical* payment: 303 on success, 409 if
the month is locked.
ᵃⁿ **Defect F-03. The write succeeds.** `attachment:create` is a seeded
Requester grant and the handler applies no ownership check whatsoever. The 303
target is `/payments/{id}`, which C-req then cannot read (403) — but the file and
the `attach` audit row are already committed. 400 if no file is sent; 409 if the
payment's month is locked; 400 if the payment is voided.
ᵃᵒ ~~**Defect F-01. The file is served.** `attachment:view` is a seeded Requester
grant and `attachmentDownload` checks only path safety and existence.~~
**Fixed after this document was written (Wave 3, `1fac147`).** `attachment:view`
is still a seeded Requester grant, but `attachmentDownload` now refuses a payment
the caller cannot read (`app.go:1102-1104`), so this cell is **404** for an
`OBJ-ATT` outside C-req's scope and 200 only for one inside it. Request documents
moved to RT-20a entirely.
ᵃᵖ C-acc holds `report:export` but **not** `grid:export`.

### 2.5 Budget, reports, recoverables, masters, administration

Every route in this block is refused to C-req, C-mgr and C-acc unless the cell
says otherwise, and every refusal is a route-gate 403.

| ID | Route | C-anon | C-req | C-mgr | C-acc | C-adm |
|---|---|---|---|---|---|---|
| PM-09 | `GET /months` | 303→/login | 403 | 403 | 403 | 200 |
| PM-10 | `POST /months` | 303→/login | 403 | 403 | 403 | 303→/budgets?month=… ‡ |
| PM-22 | `GET /reports/monthly` | 303→/login | 403 | **200** | **200** | 200 |
| PM-23 | `GET /reports/projects` | 303→/login | 403 | **200** | **200** | 200 |
| PM-24 | `GET /reports/heads` | 303→/login | 403 | **200** | **200** | 200 |
| PM-25 | `GET /reports/ytd.csv` | 303→/login | 403 | 403 | **200** | 200 |
| PM-26 | `GET /recoverables` | 303→/login | 403 | 403 | **200** | 200 |
| PM-27 | `GET /recoverables/list` | 303→/login | 403 | 403 | **200** | 200 |
| PM-28 | `GET /recoverables/list.csv` | 303→/login | 403 | 403 | **200** | 200 |
| PM-29 | `GET /recoverables/{OBJ-REQ}` | 303→/login | 403 | 403 | **200** ᵃʳ | 200 ᵃʳ |
| PM-37 | `GET /budgets` | 303→/login | 403 | 403 | 403 | 200 |
| PM-38 | `POST /budgets` | 303→/login | 403 | 403 | 403 | 303 ᵃˢ |
| PM-39 | `POST /months/{m}/lock` | 303→/login | 403 | 403 | 403 | 303→/?month=… |
| PM-40 | `POST /months/{m}/unlock` | 303→/login | 403 | 403 | 403 | 303→/?month=… |
| PM-41 | `GET /projects` | 303→/login | 403 | 403 | 403 | 200 |
| PM-42 | `POST /projects` | 303→/login | 403 | 403 | 403 | 303→/projects |
| PM-43 | `GET /heads` | 303→/login | 403 | 403 | 403 | 200 |
| PM-44 | `POST /heads` | 303→/login | 403 | 403 | 403 | 303→/heads |
| PM-45 | `GET /vendors` | 303→/login | 403 | 403 | **403** ᵃᵗ | 200* ᵃᵘ |
| PM-46 | `GET /vendors/new` | 303→/login | 403 | 403 | 403 | 200* ᵃᵘ |
| PM-47 | `GET /vendors/search` | 303→/login | 403 | 403 | 403 | 200 ᵃᵛ |
| PM-48 | `GET /vendors/{OBJ-VEN}` | 303→/login | 403 | 403 | 403 | 200* ᵃᵘ |
| PM-49 | `POST /vendors` | 303→/login | 403 | 403 | 403 | 303→/vendors/{id} ᵃʷ |
| PM-50 | `POST /vendors/{OBJ-VEN}` | 303→/login | 403 | 403 | 403 | 303→/vendors/{id} ᵃʷ |
| PM-33 | `GET /admin/notifications` | 303→/login | 403 | 403 | 403 | 200 |
| PM-34 | `POST /admin/notifications/smtp` | 303→/login | 403 | 403 | 403 | 303 |
| PM-35 | `POST /admin/notifications/events/{e}` | 303→/login | 403 | 403 | 403 | 303 ᵃˣ |
| PM-36 | `POST /admin/notifications/test` | 303→/login | 403 | 403 | 403 | 200 / **502** ᵃʸ |
| PM-85 | `GET /users` | 303→/login | 403 | 403 | 403 | 200 |
| PM-86 | `POST /users` | 303→/login | 403 | 403 | 403 | 303→/users ᵃᶻ |
| PM-87 | `GET /roles` | 303→/login | 403 | 403 | 403 | 200 |
| PM-88 | `POST /roles` | 303→/login | 403 | 403 | 403 | 303→/roles?role=… ᵇᵃ |
| PM-89 | `POST /roles/new` | 303→/login | 403 | 403 | 403 | 303 ᵇᵃ |
| PM-90 | `POST /roles/{id}/copy` | 303→/login | 403 | 403 | 403 | 303 ᵇᵃ |
| PM-91 | `POST /roles/{id}/delete` | 303→/login | 403 | 403 | 403 | **403** on a system role ᵇᵇ |
| PM-92 | `GET /configuration` | 303→/login | 403 | 403 | 403 | 200 |
| PM-93 | `POST /configuration` | 303→/login | 403 | 403 | 403 | 303→/configuration |
| PM-94 | `POST /configuration/recoverable-categories` | 303→/login | 403 | 403 | 403 | 303 ᵇᶜ |
| PM-95 | `GET /audit` | 303→/login | 403 | 403 | 403 | 200 |
| PM-96 | `GET /backups` | 303→/login | 403 | 403 | 403 | 200 |
| PM-97 | `POST /backups` | 303→/login | 403 | 403 | 403 | 303→/backups |

‡ Invalid target month → months page at **400** (`app.go:983`).
ᵃʳ ~~**Defect F-05.** `recoverableDetail` applies no `canViewRequest`; a caller
holding `recoverable_report:view` with a *narrow* `request` scope still reads the
full request, its payment and its whole thread. Both seeded holders happen to
have scope `all`, so this is only exploitable through a custom role.~~
**Fixed after this document was written (Wave 4).** `recoverableDetail` now
applies `canViewRequest` and refuses with **404**
(`app/recoverables.go:192-198`); the register, its CSV and the summary
aggregates are scoped in SQL rather than per row — see the correction at the end
of §5. A non-recoverable request id → **404**.
ᵃˢ Bad month → 400; per-head amount parse failure → 400; locked month → 409.
ᵃᵗ The `Accounts` role holds **no** `vendor` grant (`migrations.go:382-393`), so
an accountant cannot open the vendor master. Worth a product question, not a
security defect.
ᵃᵘ Bank block present only because C-adm holds `vendor_bank:view`; the store
omits the columns from the SQL for anyone else (`store/vendors.go:130-149`).
ᵃᵛ Empty `?q=` → an empty combo list at 200 (`store/vendors.go:337-339`).
ᵃʷ Validation failure re-renders the vendor form at **400**; duplicate name →
400; unknown id on update → 404.
ᵃˣ A malformed subject/body template → **400** (`notifications.go:109-114`).
ᵃʸ 200 with a `Notice` on success; **502** with the transport error on failure
(`notifications.go:146`).
ᵃᶻ Create with no password → 400; password under 12 chars → 400; deactivating or
demoting your own admin account → 400; unknown role id → 400; making a user
their own default approver → 400.
ᵇᵃ An unknown grant in the posted matrix → **400** (`app.go:1321-1326`);
renaming a system role → **403** (`store/permissions.go:290-292`); duplicate role
name → 400.
ᵇᵇ `DeleteRole` returns `ErrForbidden` for `is_system=1`
(`store/permissions.go:318-320`). A **custom** role deletes with 303→`/roles`.
ᵇᶜ An unrecognised `requires` value → **400** (`configuration.go:124-127`).

---

## 3. Ownership and scope overlay

The rules a grant alone does not express. `OS-nn` is stable.

| ID | Rule | Constrains | Enforced at | Observable symptom |
|---|---|---|---|---|
| OS-01 | `request` data scope: `own` → `requester_id = me`; `assigned` → `manager_id = me` **or** `requester_id = me`; `all` → everything; **empty → nothing** | every caller on every single-request read | `canViewRequest` (`app/requests.go:399-410`) via `loadViewableRequest` (`requests.go:295-308`) | ~~403 *"You do not have permission to view this request."*~~ → **404 *"The requested record was not found."*** on RT-57, 67, 70, 71, 72, 73, 60, 61, 62, 63, 64, 65, 79, 82. **Changed after this document was written — see the correction below.** |
| OS-02 | The broadest scope wins across multiple roles: `all` > `assigned` > `own` > none | a user holding two or more roles | `mergeScope` + `scopeRank` (`store/permissions.go:117-137`), unioned per user by `EffectivePermissions` (`permissions.go:583-596`) | Requester + Manager on one account reads *every* request, not just their own. Grants union too (`permissions.go:565-581`) — there is no deny rule and no precedence. |
| OS-03 | A URL may only **narrow** the caller's list scope | `?scope=` on RT-51 / RT-52 | `effectiveScope` (`app/requests.go:347-354`) | `?scope=own` works for an admin; `?scope=all` posted by a Requester is silently ignored, still `own` |
| OS-04 | The scoped list SQL is the same for the rows and for every tab count | RT-51 tab counts, RT-84 | `requestWhere` (`store/requests.go:1206-1269`), reused by `ListRequests` and `CountRequests` | A tab can never promise a number the list does not show |
| OS-05 | ~~`payment` data scope is **declared, grantable and never read**~~ **`payment` data scope: `own`/`assigned` → `payments.entered_by = me`; `all` → the whole ledger** | RT-13 | ~~`Scope(u,"payment")` appears **nowhere** in `internal/app`~~ **`a.auth.Scope(u,"payment")` → `PaymentListOptions.Scope` → the `entered_by` filter (`app.go:793-805`, `store/store.go:1501`)** | ~~RT-13 returns the whole ledger to any `payment:view` holder. Setting the matrix's Payments scope radio to `own` changes nothing observable.~~ **Defect F-04, fixed after this document was written (Waves 1 and 3): the radio now narrows the ledger. See the correction under F-04.** |
| OS-06 | **G8 — nobody approves their own request** | the approver on RT-76, and reassignment | three layers: `ListApprovers` excludes the requester (`store/requests.go:551-557`); `validateRequestInput` rejects `ManagerID == RequesterID` (`store/requests.go:143`); `ApproveRequest` refuses `RequesterID == actor` (`store/requests.go:692-694`); `ReassignRequest` refuses a requester target (`requests.go:780-782`) | 403 on approve; 400 *"you cannot approve your own request"* on create; 400 on reassign-to-requester |
| OS-07 | Only `T.manager_id` may decide the partial | RT-68, RT-69 — **including an Admin holding every grant** | `AcceptPartial` (`store/store.go:980-982`), `RaiseConcern` (`store.go:1021-1023`) | 403. Confirmed as a deliberate decision in PROGRESS.md:355-357 and in the e2e selector notes (PROGRESS.md:324-325) |
| OS-08 | Only `T.manager_id` may approve, return, reject, cancel outright, or decide a cancellation | RT-76, 77, 78, 81, 82, 83 | `ApproveRequest` (`requests.go:687-688`), `decideRequest` (`requests.go:728-729`), `CancelRequest` (`requests.go:976-977`), `DecideCancellation` (`requests.go:931-932`), `requestCancellationForm` (`app/requests.go:837-841`) | 403 |
| OS-09 | Only `T.requester_id` may edit, withdraw, re-raise, resubmit or ask for cancellation | RT-71, 72, 74, 75, 79, 80 | `loadEditableRequest` (`app/requests.go:592-594`), `requestCancelForm` (`requests.go:802-806`), and again in `UpdateRequest`/`WithdrawRequest`/`ReraiseRequest`/`SubmitRequest`/`RequestCancellation` | 403 — an Admin gets it too |
| OS-10 | The reservation screen is gated by **standing**, not one verb: `(hold it AND may release) OR may reassign` | RT-60 | `reservationForm` (`app/linking.go:531-537`) | 403 *"Only the person holding this reservation can release it."* The **409** for an unreserved request fires first (`linking.go:527-530`) |
| OS-11 | `ReleaseRequest`'s `authorized` parameter **is** `reservation:reassign` | RT-61 | resolved in the handler (`app/linking.go:629-630`), consumed at `store/store.go:785-787` | Release-only holder (C-acc) cannot release somebody else's reservation → 403; a `reassign` holder (C-adm) can → 303. `confirm` and `reason` are validated **before** authorization, so a probe without them gets 400 |
| OS-12 | `ReassignReservation` fails closed without the grant | RT-61 (as `authorized`), RT-62 | `store/store.go:803-805` | 403 even if the route gate were ever loosened |
| OS-13 | A reassignment target must be able to work the queue | RT-62 | `canWorkTheQueue` = active + `payment:process` (`app/linking.go:594-596`); enforced at `linking.go:665-668` and pre-filtered by `reassignCandidates` (`linking.go:580-589`) | 400 *"That person cannot work the Accounts queue."* Without it, a reservation is stranded: the new holder 403s on `/accounts-queue`, anyone re-reserving gets 409 |
| OS-14 | Reservation is won by one atomic conditional UPDATE | RT-58 | `ReserveRequest` (`store/store.go:736-748`) | Loser gets `ErrForbidden` → the **409 conflict screen** naming the winner, never a 403 (G15, `app/linking.go:58-71`) |
| OS-15 | Recording a payment requires the caller to hold the reservation, and paid ≤ approved | RT-14 | `RecordPaymentForRequest` (`store/store.go:898-910`) | 403 (re-rendered sheet) if not the holder; **400** *"…is more than the approved…"* on over-payment (G13) |
| OS-16 | A payment linked to a request is immutable | RT-17, RT-18 | `store/store.go:609-611`, `store.go:649-651`; the form URL also redirects away (`app/app.go:803-806`) | 400 |
| OS-17 | Reading a linked payment obeys the **request's** scope, not `payment:view` | RT-15 | `paymentDetail` (`app/app.go:757-762`) | 403 *"You cannot see the request behind this payment."* — the one place the payment side defers to the request scope |
| OS-18 | `on_hold=1` implies `status='approved'` | RT-64, RT-65, and every exit from approved | conditional UPDATEs (`store/store.go:1047`, `store.go:1070`); every exit clears it (`requests.go:889`, `requests.go:941`, `requests.go:985`) | Hold on a non-approved request → 403; unhold on an unheld request → 403 |
| OS-19 | Vendor bank details are withheld by **not selecting the columns** | RT-45, 46, 47, 48, 49, 50 | `canSeeBank`/`canEditBank` (`store/vendors.go:130-141`); `vendorSelect` (`vendors.go:144-149`); form read gated at `app/vendors.go:172` | `Vendor.Bank == nil`, so a stray template expression has nothing to print. `SearchVendors` never selects bank at all |
| OS-20 | The Approvals queue ignores the caller's scope and always asks `manager_id = me` | RT-84 | `approvals` (`app/requests.go:896`) | An Admin with scope `all` still sees only requests routed to them |
| OS-21 | ~~Attachments have **no** ownership rule~~ | RT-19, RT-20, RT-20a | ~~absent by design decision (`store/store.go:1351-1352`) and never supplied by the handler~~ | **Fixed after this document was written.** The handler now supplies the rule the store deliberately left to it: read is `canReadPayment` (`app/app.go:1091-1113`, `:1014`) or the request scope (`app.go:1115-1150`), write is `canReadPayment` on the parent payment (`app.go:1067-1070`). Refusal is **404** (`app.go:1044-1048`). See the correction below |
| OS-22 | Notifications are scoped by making "not yours" indistinguishable from "gone" | RT-32 | `st.Notification(ctx, user.ID, id)` → `ErrNotFound` (`app/inapp.go:76-81`) | 404, never 403 — no enumeration oracle |
| OS-23 | **Requests are now scoped the same way** — an out-of-scope request is indistinguishable from one that does not exist | every route in OS-01 | `loadViewableRequest` (`app/requests.go:295-308`) | 404 *"The requested record was not found."* **Added after this document was written** — see the correction below |

---

> **Correction — an out-of-scope request answers 404, not 403.** Every
> "expected status" in §2 and §3 that reads **403** *because the caller's data
> scope does not reach the row* is now **404** *"The requested record was
> not found."* The change is in one place, `loadViewableRequest`
> (`internal/app/requests.go:295-308`), so it applies to every route OS-01
> lists. It was fixed in Wave 3 (`1fac147`) as finding F-G-002: a row that
> existed but was out of scope answered 403 while a row that did not exist
> answered 404, which made the status code an **existence oracle** — a
> requester could walk the id space and learn which request ids exist, and by
> extension how many requests the company raises. The handler comment says so
> at `internal/app/requests.go:288-294`. The refusal is still logged.
>
> **What did *not* change**, and is still 403:
>
> - Ownership refusals that fire *after* the scope check passes —
>   `loadEditableRequest`'s `RequesterID != actor`
>   (`app/requests.go:704-707`), and the store-level `ManagerID != actor` /
>   `RequesterID != actor` checks behind OS-06 through OS-09. Scope decides
>   whether you may *see* the row; these decide whether you may *act* on it,
>   and by then the row's existence is already known to the caller.
> - **OS-17 / RT-15**, reading a linked payment out of scope, which still
>   answers 403 *"You cannot see the request behind this payment."*
>   (`internal/app/app.go:899-900`). This is the same enumeration oracle one
>   resource over and F-G-002's fix did not reach it; recorded here as an open
>   divergence, not as fixed.

## 4. Grant → route reverse index

All 66 canonical pairs, in `resourceOrder` (`store/permissions.go:177-183`) ×
`resourceActions` (`permissions.go:150-172`) order. **Roles** are the seeded
holders from §0.3. "Routes" lists the `RT-nn` whose *route gate* names the pair;
non-route consumers are noted separately.

| ID | Pair | Routes gated on it | Other consumers | Seeded holders |
|---|---|---|---|---|
| GR-01 | `request:view` | RT-51, 52, 57, 67, 70 | nav item `requests-list` (`nav.go:61`) | R, M, A, D |
| GR-02 | `request:create` | RT-53, 54, 55, 56 | dashboard areas (`dashboard.go:70`); tab-bar centre (`nav.go:204`) | R, D |
| GR-03 | `request:edit` | RT-71, 72 | `showsReturnedCorrection` (`requests.go:536`) | R, D |
| GR-04 | `request:withdraw` | RT-74 | — | R, D |
| GR-05 | `request:reraise` | RT-75 | — | R, D |
| GR-06 | `request:comment` | RT-73 | — | R, M, A, D |
| GR-07 | `request:cancel` | RT-79, 80 | — | R, D |
| GR-08 | `approval:approve` | RT-76, 84 | nav `approvals` (`nav.go:62`); dashboard (`dashboard.go:82`); tab-bar centre; `ListApprovers` selects users holding it (`store/requests.go:556`) | M, D |
| GR-09 | `approval:reject` | RT-78 | — | M, D |
| GR-10 | `approval:return` | RT-77 | — | M, D |
| **GR-11** | **`approval:reassign`** | ~~**NONE**~~ → **RT-78a** | ~~**none**~~ → route gate on `POST /requests/{id}/reassign-approver` (`app.go:569`) | M, D |
| GR-12 | `approval:accept_partial` | RT-68, 69 | — | M, D |
| GR-13 | `approval:cancel` | RT-81, 82, 83 | — | M, D |
| GR-14 | `payment:view` | RT-13, 15 | nav `payments` (`nav.go:67`); tab bar (`nav.go:228`) | A, D |
| GR-15 | `payment:create` | RT-11, 12, 14 | tab-bar centre (`nav.go:203`) | A, D |
| GR-16 | `payment:edit` | RT-16, 17 | — | A, D |
| GR-17 | `payment:void` | RT-18 | — | A, D |
| GR-18 | `payment:process` | RT-63, 66 | nav `accounts-queue` (`nav.go:63`); dashboard (`dashboard.go:94`); `canWorkTheQueue` (`linking.go:595`) | A, D |
| GR-19 | `payment:settle` | RT-59 **and RT-14** | — | A, D |
| ~~**GR-20**~~ GR-20 | `payment:mark_partial` | ~~**NONE**~~ **RT-14, when `settlement=partial`** | ~~none — the string appears only as an *audit action name* (`store/store.go:943`) and a display key (`linking.go:856,875,937`)~~ **Consumed after this document was written (Wave 3, `1fac147`, F-D-10): `paymentCreate` refuses the partial branch without it (`app.go:831-835`). It is a handler check and not a route gate because it applies to one form value only (`app.go:444-445`).** | A, D |
| GR-21 | `payment:hold` | RT-64, 65 | — | A, D |
| GR-22 | `reservation:reserve` | RT-58 | — | A, D |
| GR-23 | `reservation:release` | RT-61 | `reservationForm` (`linking.go:532`) | A, D |
| GR-24 | `reservation:reassign` | RT-62 | `reservationForm` (`linking.go:533`); `authorized` on release (`linking.go:630`) and reassign (`linking.go:670`) | D |
| GR-25 | `attachment:view` | RT-20 | — | R, A, D |
| GR-26 | `attachment:create` | RT-19 | — | R, A, D |
| GR-27 | `vendor:view` | RT-45, 47, 48 | nav `vendors` (`nav.go:78`); dashboard admin link (`dashboard.go:120`) | D |
| GR-28 | `vendor:create` | RT-46, 49 | — | D |
| GR-29 | `vendor:edit` | RT-50 | `VendorEditable` (`vendors.go:101`) | D |
| GR-30 | `vendor_bank:view` | none (by design) | `canSeeBank` (`store/vendors.go:136`); `vendorNew` (`vendors.go:73`) | D |
| GR-31 | `vendor_bank:edit` | none (by design) | `canEditBank` (`store/vendors.go:140`); `vendorInputFromForm` (`vendors.go:172`) | D |
| GR-32 | `project:view` | RT-41 | nav `projects` (`nav.go:79`) | D |
| **GR-33** | **`project:create`** | **NONE** | none — RT-42 (`project:edit`) creates when `id==0` | D |
| GR-34 | `project:edit` | RT-42 | — | D |
| GR-35 | `head:view` | RT-43 | nav `heads` (`nav.go:80`) | D |
| **GR-36** | **`head:create`** | **NONE** | none — RT-44 (`head:edit`) creates when `id==0` | D |
| GR-37 | `head:edit` | RT-44 | — | D |
| GR-38 | `budget:view` | RT-37 | nav `budgets` (`nav.go:71`) | D |
| GR-39 | `budget:edit` | RT-38 | — | D |
| GR-40 | `month:view` | RT-09 | nav `monthly-plans` (`nav.go:72`) | D |
| GR-41 | `month:create` | RT-10 | — | D |
| GR-42 | `month:lock` | RT-39, 40 | the grid's Month Close block (`templates.go`, `{{if .Perms.Can "month" "lock"}}`) | D |
| ~~**GR-43**~~ GR-43 | `grid:view` | ~~**NONE**~~ **RT-07** | nav `variance-grid` (`nav.go:70`); tab bar (`nav.go:219,231`) — ~~**visibility only**. RT-07 is `RequireLogin`.~~ **Consumed after this document was written (Wave 3, `1fac147`, F-A-02/F-G-032): `GET /grid` is gated on it (`app.go:431`), and the Recent Payments panel inside is gated separately on `payment:view` (`app.go:426-430`).** | M, A, D |
| GR-44 | `grid:export` | RT-21 | — | D |
| GR-45 | `report:view` | RT-22, 23, 24 | nav `reports` (`nav.go:75`) | M, A, D |
| GR-46 | `report:export` | RT-25 | — | A, D |
| **GR-47** | **`recoverable_category:view`** | **NONE** | none — the list renders on RT-92, gated `config:view` | D |
| **GR-48** | **`recoverable_category:create`** | **NONE** | none — RT-94 (`recoverable_category:edit`) creates when `id==0` | D |
| GR-49 | `recoverable_category:edit` | RT-94 | — | D |
| **GR-50** | **`recoverable_category:delete`** | ~~**NONE**~~ → `POST /configuration/recoverable-categories/{id}/delete` | ~~none — no delete path exists anywhere in `internal/app` or `internal/store`~~ **Closed after this document was written** (F-E-06, Wave 5): route at `internal/app/app.go:614`, handler `recoverableCategoryDelete` (`internal/app/configuration.go:166`), store `DeleteRecoverableCategory` (`internal/store/recoverables.go:260`), which pre-checks holders inside a `beginWriteTx` | D |
| GR-51 | `recoverable_report:view` | RT-26, 27, 29 | nav `recoverables` (`nav.go:64`) | A, D |
| GR-52 | `recoverable_report:export` | RT-28 | — | A, D |
| GR-53 | `user:view` | RT-85 | nav `users` (`nav.go:83`); dashboard link (`dashboard.go:119`) | D |
| ~~**GR-54**~~ GR-54 | `user:create` | ~~**NONE**~~ **RT-86, when `id==0`** | ~~none — RT-86 (`user:edit`) creates when `id==0`~~ **Consumed after this document was written (Wave 3, `1fac147`, F-A-07): `userSave` demands `user:create` when `id==0` and `user:edit` otherwise, in the handler because one handler does both (`app.go:1523-1535`).** | D |
| GR-55 | `user:edit` | RT-86 | — | D |
| GR-56 | `role:view` | RT-87 | nav `roles` (`nav.go:84`); dashboard link (`dashboard.go:118`) | D |
| GR-57 | `role:create` | RT-89, 90 | — | D |
| GR-58 | `role:edit` | RT-88 | — | D |
| GR-59 | `role:delete` | RT-91 | — | D |
| GR-60 | `notification:view` | RT-33 | nav `notif-admin` (`nav.go:89`) | D |
| GR-61 | `notification:edit` | RT-34, 35, 36 | — | D |
| GR-62 | `config:view` | RT-92 | nav `configuration` (`nav.go:85`); dashboard link (`dashboard.go:117`) | D |
| GR-63 | `config:edit` | RT-93 | — | D |
| GR-64 | `audit:view` | RT-95 | nav `audit` (`nav.go:90`) | D |
| GR-65 | `backup:view` | RT-96 | nav `backups` (`nav.go:91`) | D |
| GR-66 | `backup:create` | RT-97 | — | D |

### 4.1 Pairs no route consumes — 9 of 66

> **Correction — five of the nine, not nine.** Four of the rows below were closed by the audit
> repair waves, all in Wave 3 (`1fac147`): **GR-11** `approval:reassign` (F-A-06/F-C-02),
> **GR-20** `payment:mark_partial` (F-D-10), **GR-43** `grid:view` (F-A-02/F-G-032) and **GR-54**
> `user:create` (F-A-07). Each row carries its own note. §4.2's arithmetic moves with them:
> **59** distinct pairs are now used as route or handler gates, 59 + the remaining 5 unconsumed +
> the 2 `vendor_bank` pairs = 66, so the vocabulary still closes exactly. Note that three of the
> four are enforced **in the handler** rather than as a route gate — `mark_partial` because it
> applies to one form value, `user:create` because one handler serves create and edit — so
> "pairs no *route* consumes" is now a narrower question than "pairs nothing enforces". See
> `docs/qa/results/REPAIR-LOG.md`.

| ID | Pair | Character of the gap |
|---|---|---|
| GR-11 | `approval:reassign` | ~~**A capability with no door.** `store.ReassignRequest` exists and is tested (`store/requests.go:759-801`; coverage A7), but nothing in `internal/app` calls it and no route is registered. Granting or revoking this cell changes nothing observable.~~ **Fixed after this document was written.** Wave 3 (`1fac147`) gave it the door: `POST /requests/{id}/reassign-approver` (`app.go:569`, handler `app.go:1615-1647`), RT-78a. The cell now gates a real route and coverage A7 is shipped. |
| GR-20 | `payment:mark_partial` | ~~**Dead as a permission.** A partial settlement is performed by RT-14 (`payment:create`) with `settlement=partial`; the string only ever appears afterwards as an audit action. Revoking the cell does not stop anybody marking a payment partial.~~ **Fixed after this document was written.** Wave 3 (`1fac147`, F-D-10) made `paymentCreate` refuse the partial branch without it (`app.go:831-835`). Revoking the cell now stops a caller marking a payment partial while still letting them settle one in full. |
| GR-33 | `project:create` | Folded into `project:edit` by the upsert handler. |
| GR-36 | `head:create` | Same. |
| GR-54 | `user:create` | ~~Same, and the sharpest of the three: `user:edit` alone lets a caller mint a new user (RT-86, `app.go:1185-1190`).~~ **Fixed after this document was written.** Wave 3 (`1fac147`, F-A-07): `userSave` asks for `user:create` when `id==0` and `user:edit` otherwise (`app.go:1523-1535`), so the button and the route agree. |
| GR-43 | `grid:view` | ~~**The dangerous one.** Nothing but nav visibility depends on it, and RT-07 is gated on a session alone.~~ **Fixed after this document was written.** Wave 3 (`1fac147`, F-A-02/F-G-032) gated RT-07 on it (`app.go:431`); the Recent Payments panel inside the grid is gated separately on `payment:view` in the handler (`app.go:426-430`), because that is the part that leaked beyond budgets. |
| GR-47 | `recoverable_category:view` | The list is rendered by RT-92 behind `config:view`. |
| GR-48 | `recoverable_category:create` | Folded into `recoverable_category:edit`. |
| GR-50 | `recoverable_category:delete` | ~~No delete exists at all. `V4` claims "admin-configurable categories" via `TestRecoverableCategoryCRUD`; the HTTP surface offers upsert only.~~ **Closed** (F-E-06, Wave 5) — see GR-50 above. The HTTP surface now offers delete as well as upsert, so `V4`'s claim holds. |

`vendor_bank:view` and `vendor_bank:edit` (GR-30/31) are **not** in this list:
they carry no route by design and are enforced at the store and form-read
boundaries, which is stronger than a route gate.

### 4.2 Routes whose gate names a pair absent from `resourceActions` — none

I checked every gate string in `routes()` (`app.go:362-524`) against
`resourceActions`. All **55** distinct `(resource, action)` pairs used as route
gates are canonical — and 55 + the 9 unconsumed pairs of §4.1 + the 2
`vendor_bank` pairs enforced below the route layer = 66, which closes the
vocabulary exactly. There is no misspelled or invented verb. `permmap_test.go` separately
proves the presentation map's coverage of the vocabulary is total and
unambiguous, and `TestPermissionVocabularyIsCanonical` pins the 21/66 counts.

> **Correction — 59, not 55, and four of the nine are gone.** Wave 3 (`1fac147`) consumed
> `approval:reassign` as a route gate (RT-78a) and `payment:mark_partial`, `grid:view` and
> `user:create` as gates in one place or another — `grid:view` on the route (`app.go:431`), the
> other two in their handlers (`app.go:831-835`, `app.go:1523-1535`). The arithmetic still closes:
> 59 + 5 + 2 = 66. `TestPermissionVocabularyIsCanonical` and the 21/66 counts are untouched — the
> vocabulary did not grow, only its consumption. See the correction under §4.1.

---

## 5. Privilege-escalation probe list

Ranked by expected severity of a failure. Each row is one executable request.
Every POST needs a live session cookie plus a matching `csrf` field unless the
probe is about CSRF itself.

| ID | Probe | Exact request | Expected | Fails if |
|---|---|---|---|---|
| PE-01 | Read any payment attachment as a Requester | `GET /attachments/1` … `/attachments/50` as C-req | ~~**200 + file bytes** — this is the *current* behaviour, and it is the defect. The *correct* behaviour is 403/404.~~ **Fixed (Wave 3, `1fac147`).** Now **404** for every attachment whose payment is outside C-req's scope; 200 only for one inside it. The probe is still worth running — it is now a regression test rather than a finding | a 200 on an out-of-scope attachment |
| PE-02 | Write a file onto a stranger's payment as a Requester | `POST /payments/{OBJ-PAY}/attachments`, multipart, `attachment=@probe.pdf`, `csrf=<token>` as C-req | **303→/payments/{id}** and a new `payment_attachments` row + `attach` audit row | Same — 303 confirms F-03. A hardened build would 403 |
| PE-03 | Read the variance grid and the recent-payments table with no `grid:view` | `GET /grid` as C-req | ~~**200**, page contains `Recent Payments for` and `/payments/` links~~ **403** — fixed in Wave 3 (F-A-02/F-G-032), `app.go:431`. Run it again as **C-mgr**, who holds `grid:view` but not `payment:view`: expect **200 without** `Recent Payments for`, which is the second half of the fix (`app.go:775-776`) | a 200 as C-req, or `Recent Payments for` present as C-mgr |
| PE-04 | Approve your own request | as C-mgr, raise nothing — instead take a request where `requester_id == manager_id == me` (only reachable by tampering, see PE-16) and `POST /requests/{id}/approve` with `approved_amount=100&csrf=…` | **403** (`store/requests.go:692-694`) | any 2xx/3xx |
| PE-05 | Approve a request assigned to a different manager | `POST /requests/{OBJ-REQ}/approve`, `approved_amount=1000&note=x&csrf=…` as C-mgr | **403**. Send a *valid* amount — an empty one 400s first and masks the check | 303→/approvals |
| PE-06 | Approve as an Admin who is not the manager | same request as C-adm | **403** — holding every grant is not being the manager | 303 |
| PE-07 | Accept a partial on someone else's request while holding the verb | `POST /requests/{OBJ-REQ}/accept-partial`, `note=ok&csrf=…` as C-mgr, then as C-adm, with `T.status='partial_review'` | **403** both times (`store/store.go:980-982`) | 303→/requests/{id} |
| PE-08 | Raise a concern on someone else's partial | `POST /requests/{OBJ-REQ}/raise-concern`, `comment=why&csrf=…` as C-adm | **403**. Send a non-empty comment — an empty one 400s first | 303 |
| PE-09 | Release a reservation you do not hold, without `reassign` | `POST /requests/{OBJ-REQ}/release`, `confirm=on&reason=probe&csrf=…` as C-acc | **403** *"only the assignee may release this request"* | 303→/accounts-queue |
| PE-10 | Same probe with `reassign` | identical body as C-adm | **303→/accounts-queue** — this *is* the design (OS-11). Record it so nobody later "fixes" it | — |
| PE-11 | Reassign a reservation without the verb | `POST /requests/{OBJ-REQ}/reassign`, `to_user_id=<A2+1>&confirm=on&reason=x&csrf=…` as C-acc | **403** (route gate) | 303 |
| PE-12 | Strand a reservation on someone who cannot work the queue | as C-adm, `POST /requests/{OBJ-REQ}/reassign`, `to_user_id=<a Requester-only user>&confirm=on&reason=x&csrf=…` | **400** *"That person cannot work the Accounts queue."* (`linking.go:665-668`) | 303 — the request is now stranded in `processing` |
| PE-13 | Read another user's request by id | `GET /requests/{OBJ-REQ}` as C-req | **403** (`requests.go:236-239`) | 200 |
| PE-14 | Same, through every sibling path | as C-req: `GET /requests/{OBJ-REQ}/submitted`, `/partial-review`, `/reservation`, `/reservation/stale`, `/cancel`, `/cancellation`, `/edit` | **403** on all seven. `/reservation/stale`, `/cancellation` and `/edit` should 403 at the *route gate* (no `payment:process`, no `approval:cancel`) — a 200 anywhere is a scope break | any 200 |
| PE-15 | Read a request by id through the recoverables side door | `GET /recoverables/{OBJ-REQ}` as a **custom** role holding only `recoverable_report:view` with `request` scope `own` (build it via `POST /roles/new` + `POST /roles`) | ~~**403** would be correct. The code returns **200** with the full request, payment and thread — F-05~~ **Fixed (Wave 4).** Now **404** — `recoverableDetail` applies `canViewRequest` and answers the same way `/requests/{id}` does, so the register is not an existence oracle either (`app/recoverables.go:192-198`) | 200 is now the regression |
| PE-16 | Tamper `manager_id` to yourself on create | `POST /requests`, full valid body, `manager_id=<my own id>&csrf=…` as C-req | **400** *"you cannot approve your own request — choose another approver"* (`store/requests.go:143`) | 303 — G8's first layer is gone |
| PE-17 | Tamper `vendor_id` on a type that forbids a vendor | `POST /requests`, `type=reimbursement&vendor_id=<V>&…` as C-req | **303**, and the stored row has `vendor_id` NULL and `vendor_payee = my name` — `UpdateRequest`/`CreateRequest` force it (`store/requests.go:579-582`) | the vendor survives on a reimbursement |
| PE-18 | Tamper the `settlement` field to close a request that was under-paid | `POST /payments`, `request_id=<a request I hold>&amount=<less than approved>&settlement=settled&paid_on=…&head_id=…&csrf=…` | **303** and status `completed` — S10 says a "settled" declaration completes even when paid < approved. Then `POST /payments` again → **303 to the existing payment** (double-submit guard) and no second row (`idx_payments_request`, S9) | a second payment row appears |
| PE-19 | Tamper `settlement` to an unknown value | same body with `settlement=cleared` | **400** *"choose payment settled or partial settlement"* (`store/store.go:869-871`) | 303 |
| PE-20 | Tamper `amount` above the approved ceiling | same body with `amount=<approved+1>` | **400** *"…is more than the approved…"* (G13, `store/store.go:907-910`) | 303 |
| PE-21 | Tamper `request_id` to a request reserved by somebody else | `POST /payments`, `request_id={OBJ-REQ}&…` as C-acc | **403**, re-rendered settlement sheet (`store/store.go:898-900`) — unless a payment already exists, in which case **303** to it | 303 to a *new* payment |
| PE-22 | Tamper `partial` with no reason | `POST /payments`, `settlement=partial` and no `partial_reason` | **400** (`store/store.go:872-874`) | 303 |
| PE-23 | Post with **no** CSRF token | any PM row that reads 303, e.g. `POST /requests/{mine}/comment` with `body=x` and **no** `csrf` field, as C-req | **403** *"Your form session expired. Refresh the page and try again."* (`app.go:572-575`) | 303 |
| PE-24 | Post with a **stale** CSRF token | same, `csrf=<a value from a previous cookie>` | **403** | 303 |
| PE-25 | Post with a **cross-session** CSRF token | log in as two users in two jars; send user B's `csrf` value with user A's `fervid_session` **and** A's `fervid_csrf` cookie | **403** — `CheckCSRF` compares the posted token to *the caller's own cookie* (`auth.go:171-179`) | 303 |
| PE-26 | CSRF-fix a login | `POST /login` with valid credentials and **no** `csrf` field, from a jar with no cookies | **303→/** — RT-03 has no CSRF wrapper at all (F-07). Record it | — |
| PE-27 | Reach an admin screen by URL as a Requester | as C-req: `GET /users`, `/roles`, `/configuration`, `/audit`, `/backups`, `/admin/notifications`, `/months`, `/budgets`, `/projects`, `/heads`, `/vendors`, `/payments`, `/accounts-queue`, `/approvals`, `/recoverables`, `/export.csv`, `/reports/monthly`, `/reports/ytd.csv` | **403** on every one. This is R6 / D2 and is already pinned by `TestRequesterOnlySessionForbiddenFromAdminRoutesByURL` | any 200 |
| PE-28 | Mutate an admin resource by URL as a Requester | as C-req: `POST /roles/new` (`name=x`), `POST /users` (`email=…`), `POST /backups`, `POST /configuration`, `POST /months/2026-07/lock` | **403** at the route gate — *before* CSRF, so the token is irrelevant | any 303 |
| PE-29 | Grant yourself a permission through the roles matrix | as C-req, `POST /roles` with `role_id=<Admin role id>&cell=administration:edit&csrf=…` | **403** (route gate `role:edit`) | 303 |
| PE-30 | Invent a permission as an Admin | as C-adm, `POST /roles` with `role_id=<a custom role>&perm=request:godmode&csrf=…` | **400** *"That permission does not exist."* (`app.go:1321-1326`); `expandCells` also drops unknown cells (`permmap.go:274-295`); `UpdateRolePermissions` re-validates (`store/permissions.go:367-371`) | 303, and a junk row lands in `role_permissions` |
| PE-31 | Rename or delete a system role | as C-adm, `POST /roles` with `role_id=<Admin>&name=Root`; then `POST /roles/<Admin>/delete` | **403** on both (`store/permissions.go:290-292`, `permissions.go:318-320`) | 303 |
| PE-32 | Read a notification addressed to someone else | `GET /notifications/{OBJ-NOTIF}/open` as C-req | **404** — never 403, so there is no id-enumeration oracle (`inapp.go:76-81`) | 200 or 303 to the row's target |
| PE-33 | Hit a mutation route with GET | `GET /requests/{OBJ-REQ}/approve`, `GET /roles/1/delete`, `GET /backups`(≠ mutation), `GET /payments/1/void` | **404** for the first, second and fourth (the catch-all `GET /` claims the path; the specific gate never runs). Anonymous → **303→/login** | 405 or 200 |
| PE-34 | Hit a read route with POST | `POST /grid`, `POST /audit`, `POST /requests/{id}`, `POST /users/1` | **405** for the first three (path matches `GET …`, method does not). `POST /users/1` → **405** as well, because `GET /users` does not match the path `/users/1` … verify: no pattern matches `/users/1` for any method, so this one is **404** for a signed-in GET and **405 or 404** for POST — treat as `?` and record the observed value | — |
| PE-35 | Oversize body to bypass the size cap | `POST /requests` with a 30 MiB body, valid csrf | **413** *"The submitted form is too large."* (`app.go:564-567`). Note RT-03 (`POST /login`) has **no** cap | 200/303, or a 500 |
| PE-36 | Path-traverse an attachment | craft an attachment row (or an old row) whose `stored_path` escapes `AttachmentDir`, then `GET /attachments/{id}` | **404** *"The requested attachment was not found."* plus a `unsafe attachment path rejected` warn log (`app.go:892-897`) | the file is served |
| PE-37 | Widen your list scope through the query string | `GET /requests?scope=all` and `GET /requests/export.csv?scope=all` as C-req | **200 with own rows only** (`requests.go:347-354`) | other people's requests appear |
| PE-38 | Read vendor bank details without `vendor_bank:view` | as a custom role holding `vendor:view` only: `GET /vendors/{V}`, `GET /vendors`, `GET /vendors/search?q=a` | **200** with no bank markup and `Vendor.Bank == nil`; grep the body for `bank_account_number`, `bank_ifsc`, `upi_id` | any bank field appears |
| PE-39 | Write vendor bank details without `vendor_bank:edit` | as a custom role with `vendor:edit` only: `POST /vendors/{V}` with `bank_account_number=999&bank_ifsc=XXXX0000001&csrf=…` | **303** and the bank columns **unchanged** — the form read drops them (`vendors.go:172`) and `UpdateVendor` re-applies the rule | the columns change |
| PE-40 | Write an app setting the product never declared | as C-adm, `POST /configuration` with `evil_key=1&csrf=…` | **303**, and `app_settings` gains **no** `evil_key` — only registered `ConfigField` keys are written (`configuration.go:142-154`) | the key appears |

---

## 6. Findings

Report only. Ranked by exploitability × blast radius. Nothing below has been
changed.

### F-01 — `GET /attachments/{id}` has no ownership check; a Requester can read every payment attachment · **High**

`attachmentDownload` (`app/app.go:881-911`) resolves the id, checks only that the
stored path is inside `AttachmentDir` and that the file exists, then
`http.ServeFile`s it. `AttachmentByID` (`store/store.go:1351-1361`) explicitly
delegates authorization: *"Authorization is intentionally left to the app's
permission layer"* — and the app layer supplies nothing beyond the
`attachment:view` route gate (`app.go:393`).

`attachment:view` is a **seeded Requester grant** (`store/migrations.go:361`).

**Exploitation.** A Requester walks `/attachments/1..N`. Every bank advice
Accounts ever uploaded — account numbers, UTR references, cheque images — is
served, including for payments against requests the caller has no scope to read.
The rest of the codebase is careful about exactly this: `paymentDetail` defers to
`canViewRequest` (`app.go:757-762`) so `payment:view` cannot be a way around
Q5/R6, and `loadViewableRequest` guards fourteen request routes. The attachment
route is the one hole in that pattern.

**The natural fix** (do not apply): resolve `payment_id → request_id` and run the
same `canViewRequest` the payment detail runs, or 404 when it fails.

> **Fixed after this document was written — Wave 3, commit `1fac147`**
> (F-A-01/F-B-11 and F-A-05/F-B-09). Close to the natural fix above, and it
> answers **404**, not 403. `attachmentDownload` resolves the attachment
> together with its payment (`AttachmentWithPayment`) and refuses unless
> `canReadPayment` (`internal/app/app.go:1091-1113`, `:1014`). The route was
> also **split**: it now serves `payment_attachments` only, and request
> documents get their own route, `GET
> /requests/{id}/attachments/{attachmentID}` (`app.go:460`, handler
> `app.go:1115-1150`), which checks the document belongs to the request in the
> path *before* applying that request's scope. The split is itself a fix — the
> two tables have unrelated id sequences, so one URL space was handing one
> reader another reader's file regardless of any ownership check
> (`app.go:453-458`). See RT-20/RT-20a and PM-20.

### F-02 — `GET /grid` is gated on a session only; `grid:view` is never checked, and the page leaks payment data · **High**

`app.go:377` registers `GET /grid` under `RequireLogin`, not
`RequirePermission("grid", "view", …)`. `grid:view` exists in the vocabulary
(`store/permissions.go:161`), is drawn in the matrix (`permmap.go:143`), is
granted to Manager and Accounts (`migrations.go:375`, `migrations.go:391`) and
gates the nav item (`nav.go:70`) and the mobile tab bar (`nav.go:219`) — but
**no route consumes it** (GR-43).

The asymmetry is stark: `GET /export.csv`, the CSV of the *same* data, **is**
gated (`grid:export`, `app.go:394`).

**Exploitation.** A Requester who holds neither `grid:view` nor `payment:view`
types `/grid` and receives (a) the whole budget-versus-actual matrix for every
project and head, and (b) the *"Recent Payments for <month>"* table — paid date,
project/head, amount, payee, and a link to `/payments/{id}` for each of the ten
most recent payments (`grid` handler `app.go:643`; template block quoted in
§1.1). The links then 403, but the figures are already on screen. Nav hiding is
the only thing that keeps an ordinary user off this page, and nav hiding is not
authorization.

> **Fixed after this document was written — Wave 3, commit `1fac147`** (F-A-02/F-G-032). The
> registration reads `mux.Handle("GET /grid", a.auth.RequirePermission("grid", "view", …))`
> (`internal/app/app.go:431`), and the comment above it (`:427-430`) names this finding. The Recent
> Payments panel is gated **separately** on `payment:view` **in the handler**, not trusted to a
> template condition, because that panel is the part that leaked beyond budgets — a caller with
> `grid:view` but no `payment:view` gets the matrix and not the payments. The asymmetry with
> `GET /export.csv` this finding calls stark is now the ordinary one: view and export are two verbs
> on one resource. GR-43 is no longer an unconsumed pair. See `docs/qa/results/REPAIR-LOG.md`,
> Wave 3.

### F-03 — `POST /payments/{id}/attachments` has no ownership check; a Requester can write onto any payment · **High**

`attachmentUpload` (`app/app.go:861-879`) stages the file and calls
`store.AddAttachment`, which validates the file, refuses a voided payment and a
locked month — and nothing else (`store/store.go:1310-1334`). The only
authorization is the `attachment:create` route gate (`app.go:392`), and
`attachment:create` is a **seeded Requester grant**
(`store/migrations.go:361`).

**Exploitation.** A Requester posts a file to `/payments/{any id}/attachments`.
The write commits, an `attach` audit row is recorded under their name, and the
file becomes part of another department's payment evidence. Combined with F-01
the same account can then read it back. The redirect target 403s, which makes the
attack *quieter*, not weaker. This is an integrity attack on the audit trail:
S12 works hard to make a linked payment immutable, and this route writes to it.

> **Fixed after this document was written — Wave 3, commit `1fac147`** (F-A-03). `attachmentUpload`
> now loads the payment first and applies `canReadPayment` — the same request-scope check the read
> path uses — refusing with 404 (`internal/app/app.go:1050-1070`). The repair log records this one
> as *"unreachable, not fixed-by-removal — so it was fixed anyway"* (decision 4): F-D-08's fix means
> every payment this product can create is linked and `AddAttachment` refuses a linked payment, so
> the route cannot be reached today. The check was added regardless, because the hole returns the
> day a free-standing payment becomes creatable. `TC-A-123` was rewritten from *"upload is
> unchecked"* to a proof of the refusal. See `docs/qa/results/REPAIR-LOG.md`, Wave 3 and decision 4.

### F-04 — the `payment` data scope is declared, grantable and never enforced · **Medium-High**

`scopedResources` marks `payment` as scoped (`store/permissions.go:185`),
`ValidScope` accepts `own`/`assigned`/`all` for it (`permissions.go:200-205`),
the roles matrix draws a scope control for it (`permmap.go:255-268` →
`scopedMatrixResources`), `rolesSave` persists it (`app.go:1306-1315`), and both
Accounts and Admin are seeded `payment=all` (`migrations.go:394`,
`migrations.go:400`).

`Scope(u, "payment")` is called **nowhere** in `internal/app`. The only `Scope`
calls are all for `"request"` (`app.go:759`, `linking.go:159`, `linking.go:175`,
`requests.go:236`, `requests.go:348`, `requests.go:474`, `requests.go:929`).
`ListPayments` takes no scope argument at all (`store/store.go:1256`).

**Exploitation / consequence.** An administrator who sets a role's Payments scope
to `own` — reasonably believing it will limit that role to the payments it
entered — gets no restriction whatsoever: `GET /payments` returns the whole
ledger and `GET /payments/{id}` is limited only by the *request*-side check
(OS-17), which is absent entirely for historical unlinked payments. This is a
silent policy failure: the UI promises a control the engine ignores. R3 ("data
scope per resource") is only half-implemented.

> **Fixed after this document was written — Waves 1 and 3** (F-A-04/F-G-003). It took two halves,
> which is why the repair log lists it twice. Wave 1 (`633997b`) gave `PaymentListOptions` a `Scope`
> and a `ViewerID` and made `ListPayments` filter on `entered_by`, mirroring `requestWhere` — only
> `own` and `assigned` narrow (`internal/store/models.go:342`, `internal/store/store.go:1501`).
> Wave 3 (`1fac147`) actually passed them: `a.st.ListPayments(…, Scope: a.auth.Scope(u, "payment"),
> ViewerID: u.ID)` (`internal/app/app.go:793-805`, and the comment at `:797-800` names this
> finding). Without the second half the roles-screen control still did nothing, so neither wave
> closes it alone. RT-13's *"the declared `payment` data scope is never consulted"* is the row this
> falsifies. See `docs/qa/results/REPAIR-LOG.md`, Waves 1 and 3.

### F-05 — `GET /recoverables/{id}` bypasses the request data scope · **Medium**

`recoverableDetail` (`app/recoverables.go:130-172`) loads the request with
`a.st.Request` and never calls `canViewRequest`. It then renders the request's
number, payee, amount, counterparty, expected return date, repayment notes, its
linked payment, **and its entire merged thread** (`RequestThread`,
`recoverables.go:164`) — every comment and every audit line.

Fourteen sibling routes go through `loadViewableRequest` for precisely this
reason, and the function's own doc comment states the rule: *"holding
request:view somewhere is not the same as being allowed to read this one
(Q5/R6)"* (`app/requests.go:226-228`).

Both seeded holders of `recoverable_report:view` happen to carry
`request=all`, so this is not exploitable by a seeded role — which is exactly why
it will survive unnoticed. A custom role with `recoverable_report:view` and
`request=own` (an entirely reasonable "recoverables analyst") reads every
recoverable request in the company. Probe PE-15.

> **Fixed after this document was written — Wave 4**, and wider than the
> finding. F-05 was raised against the detail screen alone; the audit's own
> F-G-016/F-E-03 established that the **whole register** leaked the same way,
> so the scope was pushed into the SQL rather than added to one handler:
>
> - `recoverableViewer` derives `{Scope, ViewerID}` from the caller's `request`
>   scope (`internal/app/recoverables.go:117-120`) and is passed to the
>   register (`recoverables.go:122`), the CSV export (`:145`) and the summary
>   dashboard (`:31-42`).
> - `recoverableScope` is the row predicate
>   (`internal/store/recoverables.go:347`), consumed by `RecoverableReport`
>   (`:401`), `RecoverableMetrics` (`:509`) and `RecoverableRollups` (`:548`).
>   An unrecognised or empty scope returns `AND 0` — no rows — so it fails
>   closed.
> - `recoverableDetail` applies `canViewRequest` and answers **404**
>   (`internal/app/recoverables.go:192-198`).
>
> The reason it is SQL and not a handler-side filter is recorded at
> `internal/app/recoverables.go:108-116`: the summary aggregates cannot be
> filtered row-by-row after the fact at all, and a post-query filter silently
> breaks any `LIMIT` the query later grows.

### F-06 — the reservation-conflict and settlement-preview paths read a request without a scope check · **Low-Medium**

Three call sites load a request with `a.st.Request` and no `canViewRequest`:

- `requestRecordPayment`'s conflict branch (`app/linking.go:87`),
- `settlementPreview` (`app/linking.go:283`),
- `paymentEntry` (`app/linking.go:235`).

All three then render `reservation_conflict` or a settlement sheet carrying the
request's **number, display payee and the holder's name**
(`linking.go:58-71`, `linking.go:327-335`). Reachable by anyone holding
`reservation:reserve`, `payment:settle` or `payment:create` respectively. Both
seeded holders have `request=all`, so again the seeded matrix hides it; a custom
role does not. Note the deliberate asymmetry with `reservationForm`,
`requestRelease`, `requestReassign` and `requestStale`, all of which **do**
funnel through `loadViewableRequest` and say so in their comments
(`linking.go:517-520`, `linking.go:620-622`, `linking.go:727-729`).

### F-07 — `POST /login` carries no CSRF check and no body cap · **Low-Medium**

It is the only state-changing POST in `routes()` outside `withCSRF`
(`app.go:365`; the other 45 POSTs are all wrapped — verified by counting
`mux.Handle.*"POST` against the same lines containing `withCSRF`).

Two consequences. **Login CSRF:** a third-party page can POST credentials it
controls and silently sign the victim into an attacker-held account, so
subsequent actions the victim takes are recorded against that account. **No
`MaxBytesReader`:** `loginPost` calls `r.ParseForm()` directly (`app.go:585`)
with no 21 MiB cap, so the login endpoint is the cheapest memory-pressure target
in the app. `ReadTimeout` (30 s) and `MaxHeaderBytes` bound it loosely
(`app.go:329-332`), the body does not.

Related: `auth.Manager.CSRFMiddleware` (`auth.go:191-200`) is **dead code** —
its only caller is `auth_test.go:265`. Every route uses `app.withCSRF` instead.
Two implementations of one rule is how they drift.

### F-08 — `request:comment` + scope `all` lets any Manager/Accounts/Admin write into any request's conversation · **Low (probably intended, but untested)**

`requestComment` (`app/requests.go:768-786`) authorizes with
`loadViewableRequest` only, and `store.AddRequestComment`
(`store/requests.go:1001-1013`) checks that the request exists and nothing more —
unlike `RaiseConcern`, which is the same insert plus a manager check
(`store/store.go:1021-1023`). So a Manager who is not `T.manager_id` can post
into `T`'s thread, and that comment is permanent, visible to the requester and
the approver, and rendered as part of the audit story. It is consistent with N7
("conversation thread visible to viewers"), but no test pins whether a
*non-participant* with scope `all` should be able to write rather than only read.
Decide it deliberately and add the test either way.

### F-09 — nine granted verbs are unreachable; two of them are capabilities with no door · **Low, but a correctness trap for the roles screen**

Full list and character in §4.1. The two that matter:

- **`approval:reassign` (GR-11)** — ~~`store.ReassignRequest` is implemented,
  audited under its own action name and tested (`store/requests.go:759-801`;
  coverage A7 "Admin reassign (reason + history)"). No handler calls it and no
  route registers it. **A7 is verified at the store and absent from the
  product.** Ticking or clearing the Approve cell's reassign checkbox on the
  roles screen changes nothing a user can do.~~ **Fixed after this document was
  written — Wave 3, commit `1fac147`.** `POST /requests/{id}/reassign-approver`
  (`app.go:569`, handler `app.go:1615-1647`, RT-78a) gates on this cell, so it
  is now a live grant and A7 is shipped. So the count in this heading is now
  **eight** unreachable verbs, and **one** capability with no door, not two.
- **`payment:mark_partial` (GR-20)** — ~~a partial settlement is authorized by
  `payment:create` (RT-14). Revoking `mark_partial` does not prevent it. The
  verb survives only as an audit action string (`store/store.go:943`).~~
  **Fixed after this document was written — Wave 3, commit `1fac147`** (F-D-10).
  `paymentCreate` refuses `settlement=partial` without it (`app.go:831-835`), so
  revoking the cell now prevents exactly what it names while leaving a full
  settlement alone. It is checked in the handler and not as a route gate because
  it applies to one form value only (`app.go:444-445`).

The remaining seven (`project:create`, `head:create`, `user:create`,
`recoverable_category:{view,create,delete}`, `grid:view`) are either folded into
a sibling `edit` verb by an upsert handler or, in `grid:view`'s case, F-02.
`user:create` deserves its own note: **`user:edit` alone is enough to mint a new
user account** (`app.go:1185-1190`), so a role granted "edit users but not create
users" can create users.

> **Correction — five unreachable verbs, and none of them a capability with no door.** Wave 3
> (`1fac147`) closed four of the nine: `approval:reassign` (GR-11), `payment:mark_partial` (GR-20),
> `grid:view` (GR-43, via F-02) and **`user:create` (GR-54)** — the last as F-A-07, by making
> `userSave` demand the verb the *pressed control* is gated on rather than whichever one happened to
> be on the route: `create` when `id==0`, `edit` otherwise, checked in the handler because one
> handler does both (`internal/app/app.go:1523-1535`). So the paragraph above is wrong about
> `user:create` specifically: `user:edit` alone can no longer mint a user. What is left is the
> upsert-folding family — `project:create`, `head:create`,
> `recoverable_category:{view,create,delete}` — which is a naming problem, not an authorization
> hole. See `docs/qa/results/REPAIR-LOG.md`, Wave 3.

### F-10 — `GET /notifications/{id}/open` mutates on a read · **Informational**

It writes a read receipt (`MarkNotificationRead`, `app/inapp.go:86`) on a GET
with no CSRF token. Argued in-code at `inapp.go:63-72`: the design makes each
notification row a plain anchor, the effect is per-user and non-destructive, and
the redirect target comes from the stored row rather than the query string
(`inapp.go:90-93`). Accepted as designed; recorded because a "no GET mutates"
sweep will flag it and should not be allowed to "fix" it into a POST that the
approved markup cannot send.

### F-11 — `/static/` is unauthenticated and directory-listable · **Informational**

`app.go:363` mounts `http.FileServer` with no wrapper. There is no
`web/static/index.html`, so `GET /static/` returns Go's generated directory
index to an anonymous caller, disclosing filenames including
`_kitchensink.html` — a full design-system reference page served to the
internet. No application data is exposed. PROGRESS.md:345-349 separately records
that the vendored `htmx.min.js` hash has **not** been verified against the
official registry; a supply-chain check on that file is a separate task.

### F-12 — refusal-ordering makes several ownership checks unreachable by a naive probe · **Informational, but it will cost a debugging pass**

Four handlers validate *content* before *identity*, so a probe that omits a
required field gets 400 and never exercises the ownership rule it was written to
test:

| Route | 400 fires first | Ownership check behind it |
|---|---|---|
| RT-76 `approve` | `ParsePaise(approved_amount)` (`app/requests.go:717-721`) | `ManagerID != actor`, G8 (`store/requests.go:687-694`) |
| RT-77 / RT-78 `return`/`reject` | empty reason (`store/requests.go:715-718`) | `ManagerID != actor` (`requests.go:728-729`) |
| RT-80 `cancel-request` | empty reason (`requests.go:862-865`) | `RequesterID != actor` (`requests.go:875-876`) |
| RT-61 `release` | `!confirmed`, empty reason (`store/store.go:762-768`) | assignee / `authorized` (`store.go:785-787`) |

Also: RT-60 and RT-63 emit **409** ("not reserved by anyone") *before* their
permission check (`linking.go:527-530`, `linking.go:737-740`), so a 409 there is
not evidence of authorization. Encode the field values, not just the paths.

---

## 7. Divergences, coverage and unverified items

### 7.1 Code versus documents

| ID | Claim | Document says | Code does |
|---|---|---|---|
| DV-1 | anonymous refusal status | brief: `302→/login` | **303** `http.StatusSeeOther` (`auth/auth.go:90`), asserted by `app/http_safety_test.go:126` |
| DV-2 | `unbuiltPrefixes` contents | PROGRESS.md:228 — *"It now holds exactly one line, `/admin/notifications` (Phase 5)"* | **empty** (`app/nav.go:250`), with a comment saying every navigable screen is built. The brief is right, PROGRESS.md is stale |
| DV-3 | `grid:view` governs the variance grid | `nav.go:70` gates the nav entry on it; `permmap.go:143` draws it; Manager and Accounts are seeded it | ~~**no route consumes it** — `GET /grid` is `RequireLogin` (`app.go:377`). F-02~~ **Divergence closed after this document was written** — Wave 3 (`1fac147`, F-A-02/F-G-032) gated `GET /grid` on it (`app.go:431`). The document was right and the code has caught up |
| DV-4 | A7 "Admin reassign (reason + history)" is delivered | coverage matrix marks A7 verified by `TestReassignAndReraise` | ~~store-only; no route, no handler, no screen. F-09~~ **Divergence closed after this document was written** — Wave 3 (`1fac147`) added `POST /requests/{id}/reassign-approver` (`app.go:569`, handler `app.go:1615-1647`, RT-78a). The coverage matrix's claim is now true |
| DV-5 | V4 "Admin-configurable categories" incl. delete | `recoverable_category:delete` is in the vocabulary (`permissions.go:164`) and drawn as the Recoverables *Cancel* cell (`permmap.go:114`) | ~~no delete path exists anywhere. F-09~~ **Divergence closed after this document was written** — F-E-06, Wave 5: `POST /configuration/recoverable-categories/{id}/delete` (`app.go:614`), handler `configuration.go:166`, store `DeleteRecoverableCategory` (`recoverables.go:260`). V4's delete claim is now true |
| DV-6 | R3 "data scope per resource (own/assigned/all)" | coverage matrix, verified by `TestEffectivePermissionsUnionAndBroadestScope` | ~~true for `request`, **inert for `payment`**. F-04~~ **Divergence closed after this document was written** — F-A-04/F-G-003, Waves 1 and 3: `ListPayments` filters on `entered_by` (`store/store.go:1501`) and the handler passes the caller's scope (`app.go:793-805`). R3 now holds for both scoped resources |
| DV-7 | L1 "Draft (private)" | coverage matrix keeps L1 | D1 removed drafts; `payment_requests.status` carries `CHECK (status <> 'draft')` (`migrations.go:156`). L1 is unreachable by construction |

### 7.2 Coverage IDs this document exercises

`R1`, `R2`, `R3` (partially — see DV-6), `R4`, `R6`, `R7`, `R8`, `R9`;
`A2`, `A5`, `A6`, `A7` (as a gap);
`Q1`, `Q2`, `Q3`, `Q4`, `Q5`, `Q6`;
`L6`, `L7`, `L8`, `L9`, `L11`;
`S2`, `S5`, `S6`, `S7`, `S9`, `S10`, `S11`, `S12`, `S15`;
`V4` (as a gap);
`D2`, `D3`, `D5`;
`X3`, `X4`, `X5`;
`C2`.

Not touched here: `T1`–`T12`, `A1`, `A3`, `A4`, `A8`, `L1`–`L5`, `L10`, `S1`,
`S3`, `S4`, `S8`, `S13`, `S14`, `V1`–`V3`, `V5`–`V8`, `N1`–`N8`, `D1`, `D4`,
`X1`, `X2`, `X6`, `C1`, `C3`, `C4`.

### 7.3 Unverified — do not assert these without observing them

| ID | Item | Why unverified |
|---|---|---|
| UV-1 | `POST /users/1` (and any other POST to a path no pattern matches at all) → 404 or 405 | ServeMux's method-mismatch rule needs a *path* match. `/users/1` matches no registered pattern for any method, so the answer should be 404 via the `GET /` catch-all for GET and **405 or 404 for POST** depending on how ServeMux orders the two checks. I did not run it. Probe PE-34 |
| UV-2 | Exact status when `renderStatus` fails to execute a template | falls back to `http.Error(..., 500)` plain text (`http_errors.go:134`) rather than the error page. Not reachable without a template bug |
| UV-3 | Whether the seeded grants in §0.3 match any **installed** database | `seedSystemRoles` runs only in v1 and `UpdateRolePermissions` can rewrite any role. §0.4 |
| UV-4 | The `Allow` header value on a 405 | emitted by `net/http`, not by this app; not read from code |
| UV-5 | Whether `GET /static/` directory listing is reachable in production | depends on the Caddy layer in front of the app (`deploy.sh`, not read for this document) |
| UV-6 | Badge counts leaking cross-scope data | `store.BadgeCounts` gates each spec on `perms.Can` (`store/badges.go:74`) but I did not read the per-badge SQL for scope handling |
