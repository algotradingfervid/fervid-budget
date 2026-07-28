package store

import (
	"context"
	"testing"
	"time"
)

// A payment dated the day the product is showing the user must be accepted.
//
// validatePayment measured paid_on against time.Now().UTC() while
// validMonthOrCurrent defaults the ledger and the grid to
// time.Now().Format("2006-01") in the server's own zone. For any zone ahead of
// Greenwich the two disagreed between local midnight and the offset: the screens
// had rolled to the new day, the guard had not, and an accountant entering a
// payment dated the day in front of them was told "paid on cannot be a future
// date". In IST that was every day from 00:00 to 05:30, and it is what made 27
// audit-a cases fail when the suite happened to run at 05:09.
//
// The zone below is constructed from the current UTC time so that the local date
// is ALWAYS exactly one day ahead — the test does not depend on the hour it runs
// at, which is the whole failing of waiting for the window to come round again.
func TestPaidOnAcceptsTodayInAZoneAheadOfUTC(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, headID := seedActorAndHead(t, s, ctx)

	utcNow := time.Now().UTC()
	secondsIntoDay := utcNow.Hour()*3600 + utcNow.Minute()*60 + utcNow.Second()
	// One hour past the local midnight that is still ahead of UTC's.
	offset := (24*3600 - secondsIntoDay) + 3600

	restore := time.Local
	time.Local = time.FixedZone("AheadOfUTC", offset)
	defer func() { time.Local = restore }()

	localToday := time.Now().Format("2006-01-02")
	utcToday := time.Now().UTC().Format("2006-01-02")
	if localToday == utcToday {
		t.Fatalf("the constructed zone did not put local ahead of UTC: both %q", localToday)
	}

	// This is precisely the date validMonthOrCurrent would put on the screen.
	err := s.validatePayment(ctx, PaymentInput{HeadID: headID, PaidOn: localToday, Amount: 1000}, false)
	if err != nil {
		t.Fatalf("a payment dated today (%s, local) was refused while UTC was still on %s: %v",
			localToday, utcToday, err)
	}
}

// The guard still has to refuse a date that is genuinely in the future — moving
// it off UTC must not have turned it off.
func TestPaidOnStillRefusesAGenuineFutureDate(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, headID := seedActorAndHead(t, s, ctx)

	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	err := s.validatePayment(ctx, PaymentInput{HeadID: headID, PaidOn: tomorrow, Amount: 1000}, false)
	if err == nil {
		t.Fatalf("a payment dated tomorrow (%s) was accepted", tomorrow)
	}
}
