package app

import (
	"encoding/json"
	"fervidbudget/internal/store"
	"net/http"
	"sort"
	"strings"
)

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

func (a *App) recordAudit(r *http.Request, in store.AuditInput) {
	if err := a.st.RecordAudit(r.Context(), in); err != nil {
		a.log.ErrorContext(r.Context(), "audit write failed",
			"request_id", requestID(r),
			"action", in.Action,
			"entity_type", in.EntityType,
			"error", err,
		)
	}
}
