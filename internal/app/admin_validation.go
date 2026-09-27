package app

// A template helper used by the user and request forms.

import "fervidbudget/internal/store"

// approverAvailable reports whether the user with this id is in approvers,
// the list of people who may currently approve. The forms use it to warn when
// a saved default approver can no longer approve.
func approverAvailable(id int64, approvers []store.User) bool {
	for _, user := range approvers {
		if user.ID == id {
			return true
		}
	}
	return false
}
