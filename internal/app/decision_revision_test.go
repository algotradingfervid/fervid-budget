package app

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// Returning and rejecting are bound to the request revision the approver saw,
// like approving: a stale or blank revision is refused with the stale-screen
// message and leaves the request pending. A blank revision is also refused
// when deciding a requested cancellation (the stale case is covered in
// cancellation_revision_test.go).
func TestReturnRejectAndCancellationDecisionRefuseAMissingRevision(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Decision revision")
	mgrID := seedSecondApprover(t, s)
	mgr, err := s.st.UserByID(s.ctx, mgrID)
	if err != nil {
		t.Fatal(err)
	}
	s.grantAlso(mgrID, "Approver who may cancel",
		[]store.Grant{{Resource: "approval", Action: "cancel"}},
		[]store.ScopeGrant{{Resource: "request", Scope: "all"}})
	requester := s.seedRequester("decision-revision@example.test", "Decision Requester", "RequesterPass123")
	raise := func(title string) int64 {
		id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget", Type: "reimbursement",
			ShortTitle: title, ProjectID: 1, HeadID: headID, Amount: 10000, Purpose: "taxi", ManagerID: mgrID, ExpenseDate: "2026-09-26"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	s.login("kavita@example.test", "ApproverPass123")
	refused := func(path string, form url.Values, id int64, wantStatus string) {
		t.Helper()
		resp := s.postForm(path, form)
		requireStatus(t, resp, http.StatusBadRequest)
		if body := responseBody(t, resp); !strings.Contains(body, staleDecision) {
			t.Fatalf("%s refusal did not explain the stale screen:\n%s", path, body)
		}
		if got, _ := s.st.Request(s.ctx, id); got.Status != wantStatus {
			t.Fatalf("%s: a decision without the reviewed revision changed the status to %q", path, got.Status)
		}
	}
	screen := func(id int64) string {
		return requestRevisionFromPage(t, responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", id), nil, "")))
	}

	for _, c := range []struct{ verb, field, want string }{
		{"return", "comment", "returned"},
		{"reject", "reason", "rejected"},
	} {
		id := raise("Decision " + c.verb)
		path := fmt.Sprintf("/requests/%d/%s", id, c.verb)
		seen := screen(id)
		if _, err := s.st.AddRequestComment(s.ctx, requester, id, "One more detail"); err != nil {
			t.Fatal(err)
		}
		refused(path, url.Values{c.field: {"Needs a look"}, "revision": {seen}}, id, "pending")
		refused(path, url.Values{c.field: {"Needs a look"}, "revision": {""}}, id, "pending")
		requireStatus(t, s.postForm(path, url.Values{c.field: {"Needs a look"}, "revision": {screen(id)}}), http.StatusSeeOther)
		if got, _ := s.st.Request(s.ctx, id); got.Status != c.want {
			t.Fatalf("fresh %s left status %q", c.verb, got.Status)
		}
	}

	asked := raise("Decision cancellation")
	if err := s.st.ApproveRequest(s.ctx, mgr, asked, 10000, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.st.RequestCancellation(s.ctx, requester, asked, "Order withdrawn"); err != nil {
		t.Fatal(err)
	}
	decidePath := fmt.Sprintf("/requests/%d/cancellation", asked)
	refused(decidePath, url.Values{"decision": {"decline"}, "note": {"Still needed"}, "revision": {""}}, asked, "cancellation_requested")
	refused(decidePath, url.Values{"decision": {"accept"}, "revision": {""}}, asked, "cancellation_requested")
}

// The store's ApproveRequest, ReturnRequest, RejectRequest,
// DecideCancellation and CancelRequest take the revision as an optional
// trailing argument and skip the check when it is left out, which is right
// for seed data and tests but wrong for a handler. Handlers must call the
// ...AtRevision forms, whose revision argument cannot be left out. This scans
// the package's own source so a handler that slips back to the optional form
// fails here rather than in production.
func TestHandlersUseTheRevisionBoundDecisionMethods(t *testing.T) {
	optional := map[string]bool{
		"ApproveRequest": true, "ReturnRequest": true, "RejectRequest": true,
		"DecideCancellation": true, "CancelRequest": true,
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && optional[sel.Sel.Name] {
				t.Errorf("%s calls %s, which skips the revision check when none is passed; call %sAtRevision instead",
					fset.Position(call.Pos()), sel.Sel.Name, sel.Sel.Name)
			}
			return true
		})
	}
}
