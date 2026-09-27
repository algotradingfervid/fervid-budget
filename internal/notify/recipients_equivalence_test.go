package notify

import (
	"fmt"
	"reflect"
	"testing"

	"fervidbudget/internal/store"
)

// The in-app and email recipient rules now share one audience decision
// (audienceFor). These are frozen copies of the two resolvers as they were
// written before that change, each with its own spelling of the urgent rule.
// The test below checks the current resolvers give exactly the same recipients
// for every event, include-flag combination, status and holder situation.

func legacyResolveInAppUsers(event string, cfg store.NotificationSetting, req store.Request, accountIDs []int64) []int64 {
	var ids []int64
	if cfg.IncludeRequester && req.RequesterID != 0 {
		ids = append(ids, req.RequesterID)
	}
	if cfg.IncludeManager && req.ManagerID != 0 && !(event == EventRequestUrgent && req.Status != "pending") {
		ids = append(ids, req.ManagerID)
	}
	if cfg.IncludeAccounts && !(event == EventRequestUrgent && req.Status == "pending") {
		if routesToHolder(event) && req.ProcessingBy != nil && *req.ProcessingBy != 0 {
			ids = append(ids, *req.ProcessingBy)
		} else {
			ids = append(ids, accountIDs...)
		}
	}
	seen := map[int64]bool{}
	var out []int64
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func legacyResolveRecipients(event string, cfg store.NotificationSetting, app store.MailSettings, v RequestView, accounts []string) (to, cc []string) {
	urgentPreApproval := event == EventRequestUrgent && v.Status == "pending"
	toList := splitList(cfg.ToRecipients)
	if cfg.IncludeRequester {
		toList = append(toList, v.RequesterEmail)
	}
	if cfg.IncludeManager && !(event == EventRequestUrgent && !urgentPreApproval) {
		toList = append(toList, v.ManagerEmail)
	}
	if cfg.IncludeAccounts && !urgentPreApproval {
		if routesToHolder(event) && v.HolderEmail != "" {
			toList = append(toList, v.HolderEmail)
		} else {
			toList = append(toList, accounts...)
		}
	}
	ccList := splitList(cfg.CcRecipients)
	if event == EventRequestApproved || (event == EventRequestUrgent && !urgentPreApproval) {
		ccList = append(ccList, splitList(app.ManagementRecipients)...)
	}
	to = dedupe(nil, toList)
	cc = dedupe(to, ccList)
	return to, cc
}

func TestRecipientResolversMatchTheirPreviousRules(t *testing.T) {
	events := append(append([]string{}, AllEvents...), "unknown_event", "")
	statuses := []string{"pending", "approved", "processing", "partial_review", "cancellation_requested", "completed", ""}
	int64p := func(v int64) *int64 { return &v }
	holders := []*int64{nil, int64p(0), int64p(7), int64p(1)}
	holderEmails := []string{"", "holder@test", "acc1@test"}
	people := []struct{ requester, manager int64 }{{1, 2}, {0, 2}, {1, 0}, {0, 0}, {3, 3}}
	personEmails := []struct{ requester, manager string }{{"req@test", "mgr@test"}, {"", "mgr@test"}, {"req@test", ""}, {"same@test", "same@test"}}
	accountSets := []struct {
		ids    []int64
		emails []string
	}{{nil, nil}, {[]int64{5, 6}, []string{"acc1@test", "acc2@test"}}, {[]int64{7, 1, 0}, []string{"holder@test", "req@test"}}}
	extras := []struct{ to, cc string }{{"", ""}, {"extra@test; mgr@test", "cc@test, req@test"}}
	app := store.MailSettings{ManagementRecipients: "boss@test, cc@test"}

	checked := 0
	for _, event := range events {
		for flags := 0; flags < 8; flags++ {
			for _, extra := range extras {
				cfg := store.NotificationSetting{
					Event:            event,
					IncludeRequester: flags&1 != 0,
					IncludeManager:   flags&2 != 0,
					IncludeAccounts:  flags&4 != 0,
					ToRecipients:     extra.to,
					CcRecipients:     extra.cc,
				}
				for _, status := range statuses {
					for _, holder := range holders {
						for _, p := range people {
							for _, accounts := range accountSets {
								req := store.Request{RequesterID: p.requester, ManagerID: p.manager, Status: status, ProcessingBy: holder}
								name := fmt.Sprintf("%s/flags=%d/%s/holder=%v/people=%v/accounts=%v", event, flags, status, holder, p, accounts.ids)
								if got, want := resolveInAppUsers(event, cfg, req, accounts.ids), legacyResolveInAppUsers(event, cfg, req, accounts.ids); !reflect.DeepEqual(got, want) {
									t.Fatalf("in-app %s: got %v, want %v", name, got, want)
								}
								for _, holderEmail := range holderEmails {
									for _, pe := range personEmails {
										v := RequestView{Status: status, RequesterEmail: pe.requester, ManagerEmail: pe.manager, HolderEmail: holderEmail}
										gotTo, gotCc := resolveRecipients(event, cfg, app, v, accounts.emails)
										wantTo, wantCc := legacyResolveRecipients(event, cfg, app, v, accounts.emails)
										if !reflect.DeepEqual(gotTo, wantTo) || !reflect.DeepEqual(gotCc, wantCc) {
											t.Fatalf("email %s holderEmail=%q emails=%v: got to=%v cc=%v, want to=%v cc=%v",
												name, holderEmail, pe, gotTo, gotCc, wantTo, wantCc)
										}
										checked++
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no combinations checked")
	}
}
