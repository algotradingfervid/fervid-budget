package app

import (
	"html/template"

	"fervidbudget/internal/money"
	"fervidbudget/internal/store"
)

// templateFuncs returns the functions the page templates can call, such as
// {{money .Amount}} or {{plural .Count "head" "heads"}}. The templates are
// parsed once at startup with this map, so a name used in a template but
// missing here fails at startup, not when the page is first opened.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"money": money.FormatPaise,
		"short": money.FormatShort,
		"date":  dateText,
		"datep": datepText,
		"select": func(a, b string) template.HTMLAttr {
			if a == b {
				return "selected"
			}
			return ""
		},
		"check": func(v bool) template.HTMLAttr {
			if v {
				return "checked"
			}
			return ""
		},
		"voided":           func(p store.Payment) bool { return p.VoidedAt != nil },
		"usedPct":          usedPct,
		"usedText":         usedText,
		"remainingText":    remainingText,
		"dueClass":         dueClass,
		"dueText":          dueText,
		"boolText":         boolText,
		"actionText":       actionText,
		"actionClass":      actionClass,
		"entityText":       entityText,
		"jsonPretty":       jsonPretty,
		"hasText":          hasText,
		"fileSize":         fileSize,
		"barClass":         barClass,
		"varClass":         varClass,
		"statusText":       statusText,
		"reportDetailURL":  reportDetailURL,
		"budgetInputValue": budgetInputValue,
		"statusFor":        statusForView,
		"planStatusText":   planStatusText,
		"paymentMode":      paymentModeText,
		"vendorType":       vendorTypeText,
		"vendorStatus":     vendorStatusText,
		"categories":       categoryChain,
		"plural":           plural,
		// Payment requests. `deref` exists because several request columns are
		// nullable (*int64) and html/template cannot compare a pointer to an int.
		"deref": func(p *int64) int64 {
			if p == nil {
				return 0
			}
			return *p
		},
		// requiresKey round-trips a category row through the hidden "requires"
		// field, so the flags→key mapping is not re-derived in HTML.
		"requiresKey": func(c store.RecoverableCategory) string {
			switch {
			case c.RequiresProject && c.RequiresCounterparty:
				return "both"
			case c.RequiresProject:
				return "project"
			case c.RequiresCounterparty:
				return "counterparty"
			default:
				return "none"
			}
		},
		"notifGlyph":   notifGlyph,
		"notifBody":    notifBody,
		"reqStatus":    requestStatusText,
		"statusPill":   statusPill,
		"rowPill":      rowStatusPill,
		"typeLabel":    typeLabel,
		"recoverable":  recoverableLabel,
		"inWords":      money.InWords,
		"amountValue":  amountValue,
		"dateLong":     formatLongDate,
		"requestTypes": func() []requestTypeOption { return requestTypeOptions },
		"requestTabs":  func() []requestTab { return requestTabs },
		"approvalTabs": func() []approvalTab { return approvalTabs },
		"threadDot":    threadDot,
		"threadGlyph":  threadGlyph,
		"threadValue":  threadValue,
		"threadField":  threadField,
		"fileKind":     fileKind,
		// Payment linking and settlement. Display only: every rule these
		// read from is enforced in the store.
		"hhmm":               hhmm,
		"since":              since,
		"reservedLabel":      reservedLabel,
		"stale":              stale,
		"activeHold":         activeHold,
		"sub":                subPaise,
		"approvedOf":         approvedOf,
		"trailAction":        trailAction,
		"auditTone":          auditTone,
		"auditGlyph":         auditGlyph,
		"auditPhrase":        auditPhrase,
		"trailBody":          trailBody,
		"initials":           initials,
		"roleAuditChanges":   roleAuditChanges,
		"vendorAuditChanges": vendorAuditChanges,
		"approverAvailable":  approverAvailable,
		// Configuration. The screen renders this table, so adding a section
		// means appending to it and nothing else.
		"configSections": func() []ConfigSection { return configSections },
		// The audit screen's two filters. Built from the strings the store
		// actually writes, so an option always matches the rows it selects.
		"auditEntities": auditEntities,
		"auditActions":  auditActions,
		"waitingOn":     waitingOn,
		// The requests list's pager arithmetic. `sub` is already taken by the money
		// subtraction the settlement screens use, and an offset can never go
		// negative, so these two are their own pair.
		"add": func(a, b int) int { return a + b },
		"sub0": func(a, b int) int {
			if b <= 0 || a-b < 0 {
				return 0
			}
			return a - b
		},
		"card": func(r store.Request, viewerID int64) requestCardData {
			return requestCardData{Req: r, ViewerID: viewerID}
		},
	}
}
