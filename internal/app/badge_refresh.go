package app

import "net/http"

// Shell counts may be memoised between reads, but must reflect a user's writes
// immediately on the redirected receipt or ledger rather than after a TTL.
func (a *App) refreshBadgesAfterMutation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			defer a.st.InvalidateBadgeCounts()
		}
		next.ServeHTTP(w, r)
	})
}
