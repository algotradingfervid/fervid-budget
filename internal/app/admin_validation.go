package app

import "fervidbudget/internal/store"

func approverAvailable(id int64, approvers []store.User) bool {
	for _, user := range approvers {
		if user.ID == id {
			return true
		}
	}
	return false
}
