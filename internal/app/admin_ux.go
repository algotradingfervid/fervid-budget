package app

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

type BackupRow struct{ Name, CreatedAt, Status, RestoreCommand string }

func (a *App) authorizedLoginNext(r *http.Request, u store.User, raw string) string {
	target := auth.SafeReturnPath(raw)
	parsed, _ := url.Parse(target)
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if target == "/" {
		return target
	}
	resources := map[string]string{"grid": "grid", "months": "month", "budgets": "budget", "projects": "project", "heads": "head", "vendors": "vendor", "requests": "request", "payments": "payment", "reports": "report", "recoverables": "recoverable_report", "users": "user", "roles": "role", "configuration": "config", "audit": "audit", "backups": "backup", "notifications": "notification", "admin": "notification"}
	if !knownLoginPage(parts) {
		return "/"
	}
	resource := resources[parts[0]]
	action := "view"
	if parts[0] == "approvals" {
		resource, action = "approval", "approve"
	}
	if parts[0] == "accounts-queue" {
		resource, action = "payment", "process"
	}
	if len(parts) > 1 && parts[1] == "new" {
		action = "create"
	}
	if resource == "" || !a.auth.Can(u, resource, action) {
		return "/"
	}
	if resource == "role" && parsed.Query().Get("role") != "" {
		id, err := strconv.ParseInt(parsed.Query().Get("role"), 10, 64)
		if err != nil || id <= 0 {
			return "/"
		}
		if _, err = a.st.Role(r.Context(), id); err != nil {
			return "/"
		}
	}
	if len(parts) > 1 {
		if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil && id > 0 {
			if resource == "vendor" {
				if _, err := a.st.Vendor(r.Context(), id, a.auth.Permissions(u)); err != nil {
					return "/"
				}
				if len(parts) == 3 {
					resourceForTab := map[string]string{"requests": "request", "payments": "payment", "history": "audit"}[parts[2]]
					if !a.auth.Can(u, resourceForTab, "view") {
						return "/"
					}
				}
			}
			if resource == "recoverable_report" {
				req, err := a.st.Request(r.Context(), id)
				if err != nil || req.Treatment != "recoverable" || !canViewRequest(a.auth.Scope(u, "request"), u, req) {
					return "/"
				}
			}
			if resource == "request" {
				req, err := a.st.Request(r.Context(), id)
				if err != nil || !canViewRequest(a.auth.Scope(u, "request"), u, req) {
					return "/"
				}
			}
			if resource == "payment" {
				pay, err := a.st.Payment(r.Context(), id)
				if err != nil {
					return "/"
				}
				scope := a.auth.Scope(u, "payment")
				if scope != "all" && (scope == "" || pay.EnteredBy != u.ID) {
					return "/"
				}
				if pay.RequestID != nil {
					req, err := a.st.Request(r.Context(), *pay.RequestID)
					if err != nil || !canViewRequest(a.auth.Scope(u, "request"), u, req) {
						return "/"
					}
				}
			}
		}
	}
	return target
}

func knownLoginPage(parts []string) bool {
	if len(parts) == 1 {
		switch parts[0] {
		case "grid", "months", "budgets", "projects", "heads", "vendors", "requests", "payments", "recoverables", "users", "roles", "configuration", "audit", "backups", "notifications", "approvals", "accounts-queue":
			return true
		}
		return false
	}
	if len(parts) == 2 {
		switch parts[0] {
		case "reports":
			return parts[1] == "monthly" || parts[1] == "projects" || parts[1] == "heads"
		case "admin":
			return parts[1] == "notifications"
		case "recoverables":
			if parts[1] == "list" {
				return true
			}
		case "vendors", "requests", "payments":
			if parts[1] == "new" {
				return true
			}
		default:
			return false
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		return err == nil && id > 0
	}
	if len(parts) == 3 && parts[0] == "vendors" {
		id, err := strconv.ParseInt(parts[1], 10, 64)
		return err == nil && id > 0 && (parts[2] == "requests" || parts[2] == "payments" || parts[2] == "history")
	}
	return false
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func (a *App) backupRows(names []string) []BackupRow {
	rows := []BackupRow{}
	for _, name := range names {
		row := BackupRow{Name: name, CreatedAt: "Unknown — inspect manifest", Status: "Needs inspection"}
		manifestValid := false
		path := filepath.Join(a.cfg.BackupDir, name)
		var manifest struct {
			CreatedAt string `json:"created_at"`
		}
		if contents, err := os.ReadFile(filepath.Join(path, "manifest.json")); err == nil && json.Unmarshal(contents, &manifest) == nil {
			if created, err := time.Parse(time.RFC3339, manifest.CreatedAt); err == nil {
				row.CreatedAt = created.Local().Format("02 Jan 2006, 15:04 MST")
				manifestValid = true
			}
		}
		db, de := os.Stat(filepath.Join(path, "fervid.db"))
		at, ae := os.Stat(filepath.Join(path, "attachments"))
		if manifestValid && de == nil && ae == nil && !db.IsDir() && at.IsDir() {
			row.Status = "Files present · not restore-tested"
		}
		row.RestoreCommand = "FERVID_DB=" + shellQuote(a.cfg.DBPath) + " FERVID_ATTACHMENT_DIR=" + shellQuote(a.cfg.AttachmentDir) + " go run ./cmd/server --restore " + shellQuote(path)
		rows = append(rows, row)
	}
	return rows
}

// vendorAuditChanges describes recorded facts only. Older audit rows did not
// capture a complete snapshot; never reconstruct them from today's vendor.
func vendorAuditChanges(entry store.AuditEntry) []string {
	var before, after map[string]any
	_ = json.Unmarshal([]byte(entry.BeforeJSON), &before)
	_ = json.Unmarshal([]byte(entry.AfterJSON), &after)
	if after["audit_version"] != float64(2) {
		return []string{"Detailed field changes were not captured for this earlier entry."}
	}
	fields := [][2]string{{"name", "Vendor name"}, {"display_name", "Short name"}, {"vendor_type", "Type"}, {"status", "Status"}, {"categories", "Categories"}, {"gstin", "GSTIN"}, {"pan", "PAN"}, {"msme_udyam", "MSME / Udyam"}, {"tds_section", "TDS section"}, {"tds_rate", "TDS rate"}, {"contact_person", "Contact person"}, {"phone", "Phone"}, {"email", "Email"}, {"address", "Address"}, {"city", "City"}, {"state", "State"}, {"state_code", "State code"}, {"notes", "Internal notes"}}
	display := func(value string) string {
		if value == "" {
			return "Not set"
		}
		return "“" + value + "”"
	}
	out := []string{}
	for _, field := range fields {
		old, oldKnown := before[field[0]].(string)
		value, newKnown := after[field[0]].(string)
		if !newKnown {
			continue
		}
		if entry.Action == "create" {
			if value != "" {
				out = append(out, field[1]+": "+display(value))
			}
			continue
		}
		if oldKnown && old != value {
			out = append(out, field[1]+": "+display(old)+" → "+display(value))
		}
	}
	if changed, _ := after["bank_changed"].(bool); changed {
		out = append(out, "Payment details changed. Bank and UPI values are kept out of the audit log.")
	}
	if len(out) == 0 {
		out = append(out, "No field values changed.")
	}
	return out
}

func roleAuditChanges(entry store.AuditEntry) []string {
	var before, after map[string]any
	_ = json.Unmarshal([]byte(entry.BeforeJSON), &before)
	_ = json.Unmarshal([]byte(entry.AfterJSON), &after)
	oldList, oldOK := before["permissions"].([]any)
	newList, newOK := after["permissions"].([]any)
	if !oldOK || !newOK {
		return []string{"Detailed permission changes were not captured for this entry."}
	}
	oldSet, newSet := map[string]bool{}, map[string]bool{}
	for _, v := range oldList {
		if key, ok := v.(string); ok {
			oldSet[key] = true
		}
	}
	for _, v := range newList {
		if key, ok := v.(string); ok {
			newSet[key] = true
		}
	}
	keys := []string{}
	seen := map[string]bool{}
	for key := range oldSet {
		keys = append(keys, key)
		seen[key] = true
	}
	for key := range newSet {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	resourceLabel := func(key string) string {
		labels := map[string]string{"request": "Payment requests", "payment": "Payments", "approval": "Approvals", "attachment": "Attachments", "vendor": "Vendors", "vendor_bank": "Vendor bank details", "role": "Roles", "user": "Users", "recoverable_report": "Recoverables", "recoverable_category": "Recoverable categories", "grid": "Variance grid", "budget": "Budgets", "month": "Months", "head": "Heads", "project": "Projects", "config": "Configuration", "notification": "Notifications", "audit": "Audit log", "backup": "Backups", "reservation": "Reservations", "report": "Reports"}
		if label := labels[key]; label != "" {
			return label
		}
		return entityText(key)
	}
	grantText := func(granted bool) string {
		if granted {
			return "Granted"
		}
		return "Not granted"
	}
	out := []string{}
	for _, key := range keys {
		if oldSet[key] == newSet[key] {
			continue
		}
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			continue
		}
		out = append(out, resourceLabel(parts[0])+" · "+entityText(parts[1])+": "+grantText(oldSet[key])+" → "+grantText(newSet[key]))
	}
	oldScopes, _ := before["data_scopes"].(map[string]any)
	newScopes, _ := after["data_scopes"].(map[string]any)
	keys = nil
	seen = map[string]bool{}
	for key := range oldScopes {
		keys = append(keys, key)
		seen[key] = true
	}
	for key := range newScopes {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	scopeText := func(raw any) string {
		value, _ := raw.(string)
		if value == "" {
			return "None"
		}
		return entityText(value)
	}
	for _, key := range keys {
		oldValue, newValue := scopeText(oldScopes[key]), scopeText(newScopes[key])
		if oldValue != newValue {
			out = append(out, resourceLabel(key)+" · Record access: "+oldValue+" → "+newValue)
		}
	}
	if len(out) == 0 {
		return []string{"No permissions or record access changed."}
	}
	return out
}

const loginFallbackNotice = "You’re signed in. The requested page isn’t available to your account, so we’ve opened Home."
