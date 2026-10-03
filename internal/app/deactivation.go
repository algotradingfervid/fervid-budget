package app

import (
	"fmt"
	"net/http"
	"sort"

	"fervidbudget/internal/store"
)

// Confirmation before deactivating a head or a user that still has unfinished
// requests. Instead of saving, the handler shows a page listing those requests;
// confirming re-posts the same form with confirm=on.

// DeactivationConfirm is the content of that confirmation page.
type DeactivationConfirm struct {
	Heading     string
	Lead        string
	Consequence string
	ConfirmText string
	Action      string // where the confirmed form posts
	Back        string // where Cancel goes
	Requests    []store.OpenRequest
	Fields      []formField
	// AskPassword is set when the original submit carried a new password. It is
	// asked for again rather than echoed into the page.
	AskPassword bool
}

// formField is one posted form value, re-sent as a hidden input on confirm.
type formField struct{ Name, Value string }

// confirmDeactivation shows the confirmation page for c. Every posted field
// except csrf, confirm and password is carried over, in name order; a password
// is asked for again rather than copied into the page.
func (a *App) confirmDeactivation(w http.ResponseWriter, r *http.Request, c DeactivationConfirm) {
	names := make([]string, 0, len(r.PostForm))
	for name := range r.PostForm {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		switch name {
		case "csrf", "confirm":
			continue
		case "password":
			c.AskPassword = r.PostForm.Get("password") != ""
			continue
		}
		for _, v := range r.PostForm[name] {
			c.Fields = append(c.Fields, formField{name, v})
		}
	}
	a.render(w, r, "confirm_deactivate", PageData{Title: c.Heading, Deactivation: &c})
}

// headDeactivationNeedsConfirm reports whether headSave must stop and ask
// (having shown the confirmation page). It asks only when an active head is
// being switched off, not yet confirmed, and requests that can still be paid
// are filed under it.
func (a *App) headDeactivationNeedsConfirm(w http.ResponseWriter, r *http.Request, id int64) (bool, error) {
	if id == 0 || r.FormValue("active") == "on" || r.FormValue("confirm") == "on" {
		return false, nil
	}
	active, err := a.st.HeadIsActive(r.Context(), id)
	if err != nil || !active {
		return false, err
	}
	open, err := a.st.OpenRequestsForHead(r.Context(), id)
	if err != nil || len(open) == 0 {
		return false, err
	}
	a.confirmDeactivation(w, r, DeactivationConfirm{
		Heading:     "Deactivate " + r.FormValue("name") + "?",
		Lead:        fmt.Sprintf("%s still filed under this head.", countRequests(len(open), "is", "are")),
		Consequence: "A payment needs an active head, so none of these can be paid while it is inactive. Reactivate the head to pay them.",
		ConfirmText: "I understand these requests cannot be paid until the head is active again",
		Action:      "/heads",
		Back:        "/heads",
		Requests:    open,
	})
	return true, nil
}

// userDeactivationNeedsConfirm is the same check for a user who still has
// requests waiting for their approval.
func (a *App) userDeactivationNeedsConfirm(w http.ResponseWriter, r *http.Request, id int64) (bool, error) {
	if id == 0 || r.FormValue("active") == "on" || r.FormValue("confirm") == "on" {
		return false, nil
	}
	target, err := a.st.UserByID(r.Context(), id)
	if err != nil || !target.Active {
		return false, err
	}
	waiting, err := a.st.RequestsAwaitingApprover(r.Context(), id)
	if err != nil || len(waiting) == 0 {
		return false, err
	}
	name := r.FormValue("name")
	if name == "" {
		name = target.Name
	}
	a.confirmDeactivation(w, r, DeactivationConfirm{
		Heading:     "Deactivate " + name + "?",
		Lead:        fmt.Sprintf("%s waiting on %s to decide.", countRequests(len(waiting), "is", "are"), name),
		Consequence: "Once deactivated they cannot sign in, so these requests stay stuck until someone reassigns them. Open each one and use Reassign approval.",
		ConfirmText: "I will reassign these requests to another approver",
		Action:      "/users",
		Back:        "/users",
		Requests:    waiting,
	})
	return true, nil
}

// countRequests says "1 unfinished request <one>" or "N unfinished requests
// <many>", for example "2 unfinished requests are".
func countRequests(n int, one, many string) string {
	if n == 1 {
		return "1 unfinished request " + one
	}
	return fmt.Sprintf("%d unfinished requests %s", n, many)
}
