package app

// Audit log helpers: writing an audit row from a handler, and turning the
// before/after JSON stored on vendor and role audit rows into the readable
// "field: old → new" lines the audit screens show.

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"

	"fervidbudget/internal/store"
)

// vendorAuditFields are the vendor fields the audit log describes, in display
// order, as {JSON key, label}.
var vendorAuditFields = [][2]string{{"name", "Vendor name"}, {"display_name", "Short name"}, {"vendor_type", "Type"}, {"status", "Status"}, {"categories", "Categories"}, {"gstin", "GSTIN"}, {"pan", "PAN"}, {"msme_udyam", "MSME / Udyam"}, {"tds_section", "TDS section"}, {"tds_rate", "TDS rate"}, {"contact_person", "Contact person"}, {"phone", "Phone"}, {"email", "Email"}, {"address", "Address"}, {"city", "City"}, {"state", "State"}, {"state_code", "State code"}, {"notes", "Internal notes"}}

// vendorAuditChanges lists what a vendor audit row recorded: every set field
// for a create, and each changed field as "old → new" for an update. Bank and
// UPI values are never shown, only the fact that they changed. Rows written
// before full snapshots were stored (no audit_version 2) get one sentence
// saying so; they are never filled in from today's vendor.
func vendorAuditChanges(entry store.AuditEntry) []string {
	var before, after map[string]any
	_ = json.Unmarshal([]byte(entry.BeforeJSON), &before)
	_ = json.Unmarshal([]byte(entry.AfterJSON), &after)
	if after["audit_version"] != float64(2) {
		return []string{"Detailed field changes were not captured for this earlier entry."}
	}
	display := func(value string) string {
		if value == "" {
			return "Not set"
		}
		return "“" + value + "”"
	}
	out := []string{}
	for _, field := range vendorAuditFields {
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

// auditResourceLabels names each permission resource in role audit lines.
// A resource missing here falls back to entityText.
var auditResourceLabels = map[string]string{"request": "Payment requests", "payment": "Payments", "approval": "Approvals", "attachment": "Attachments", "vendor": "Vendors", "vendor_bank": "Vendor bank details", "role": "Roles", "user": "Users", "recoverable_report": "Recoverables", "recoverable_category": "Recoverable categories", "grid": "Variance grid", "budget": "Budgets", "month": "Months", "head": "Heads", "project": "Projects", "config": "Configuration", "notification": "Notifications", "audit": "Audit log", "backup": "Backups", "reservation": "Reservations", "report": "Reports"}

// auditResourceLabel returns the display name of a permission resource.
func auditResourceLabel(resource string) string {
	if label := auditResourceLabels[resource]; label != "" {
		return label
	}
	return entityText(resource)
}

// roleAuditChanges lists what a role audit row changed: each permission
// granted or removed ("Payments · Create: Not granted → Granted"), then each
// changed record-access scope ("… · Record access: Own → All"), each in key
// order. A row without both permission lists gets one sentence saying the
// details were not captured.
func roleAuditChanges(entry store.AuditEntry) []string {
	var before, after map[string]any
	_ = json.Unmarshal([]byte(entry.BeforeJSON), &before)
	_ = json.Unmarshal([]byte(entry.AfterJSON), &after)
	oldList, oldOK := before["permissions"].([]any)
	newList, newOK := after["permissions"].([]any)
	if !oldOK || !newOK {
		return []string{"Detailed permission changes were not captured for this entry."}
	}
	oldSet, newSet := stringSet(oldList), stringSet(newList)
	grantText := func(granted bool) string {
		if granted {
			return "Granted"
		}
		return "Not granted"
	}
	out := []string{}
	for _, key := range sortedKeyUnion(oldSet, newSet) {
		if oldSet[key] == newSet[key] {
			continue
		}
		resource, action, ok := strings.Cut(key, ":")
		if !ok {
			continue
		}
		out = append(out, auditResourceLabel(resource)+" · "+entityText(action)+": "+grantText(oldSet[key])+" → "+grantText(newSet[key]))
	}

	oldScopes, _ := before["data_scopes"].(map[string]any)
	newScopes, _ := after["data_scopes"].(map[string]any)
	scopeText := func(raw any) string {
		value, _ := raw.(string)
		if value == "" {
			return "None"
		}
		return entityText(value)
	}
	for _, key := range sortedKeyUnion(oldScopes, newScopes) {
		oldValue, newValue := scopeText(oldScopes[key]), scopeText(newScopes[key])
		if oldValue != newValue {
			out = append(out, auditResourceLabel(key)+" · Record access: "+oldValue+" → "+newValue)
		}
	}
	if len(out) == 0 {
		return []string{"No permissions or record access changed."}
	}
	return out
}

// stringSet returns the strings in list as a set, skipping other values.
func stringSet(list []any) map[string]bool {
	set := map[string]bool{}
	for _, v := range list {
		if s, ok := v.(string); ok {
			set[s] = true
		}
	}
	return set
}

// sortedKeyUnion returns every key found in a or b, once each, sorted.
func sortedKeyUnion[V any](a, b map[string]V) []string {
	keys := slices.Collect(maps.Keys(a))
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

// recordAudit writes an audit row for a handler. A failed write is logged
// with the request id and does not fail the request.
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
