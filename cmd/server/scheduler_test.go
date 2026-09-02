package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localithelp/db"
)

// TestScheduler drives every job against a temp DB with a fixed clock: each
// send stamps its row, a second tick at the same instant sends nothing, and
// the time windows are honoured.
func TestScheduler(t *testing.T) {
	if err := db.Open(filepath.Join(t.TempDir(), "sched.db")); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	site = siteConfig{BaseURL: "https://example.test", Email: "me@example.test", Phone: "0400 000 000"}
	mail, notifyEmail = nil, "me@example.test"

	// Tuesday 25 Aug 2026, 17:00 Melbourne.
	now := time.Date(2026, 8, 25, 17, 0, 0, 0, db.Melbourne)
	tomorrow := now.AddDate(0, 0, 1)

	mk := func(name, email string, start time.Time, status string) int64 {
		id, err := db.InsertBooking(&db.Booking{Name: name, Email: email, Phone: "0400 111 222", Suburb: "Donvale",
			Address: "12 Smith St, Donvale VIC 3111", ServiceSlug: "email-outlook", Issue: "Outlook won't open", Mode: "onsite"})
		if err != nil {
			t.Fatal(err)
		}
		if !start.IsZero() {
			if err := db.ScheduleBooking(id, start, 60); err != nil {
				t.Fatal(err)
			}
		}
		if status != "" && status != db.BookingBooked {
			if err := db.UpdateBookingStatus(id, status); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	remind := mk("Ann", "ann@example.test", tomorrow.Add(-7*time.Hour), db.BookingBooked) // tomorrow 10:00
	noEmail := mk("Bob", "", tomorrow.Add(-6*time.Hour), db.BookingBooked)
	cancelled := mk("Cat", "cat@example.test", tomorrow.Add(-5*time.Hour), db.BookingCancelled)
	soon := mk("Dan", "dan@example.test", now.Add(50*time.Minute), db.BookingBooked)
	later := mk("Eve", "eve@example.test", now.Add(90*time.Minute), db.BookingBooked)
	fresh := mk("Fay", "fay@example.test", time.Time{}, "") // status new → digest only
	// A scheduled visit stepped back to contacted still gets its reminder and
	// heads-up — only cancelled/spam switch them off.
	conRemind := mk("Hal", "hal@example.test", tomorrow.Add(-4*time.Hour), db.BookingContacted) // tomorrow 13:00
	conSoon := mk("Ivy", "ivy@example.test", now.Add(55*time.Minute), db.BookingContacted)

	s := runScheduledJobs(now)
	if s.Reminders != 2 || s.Alerts != 2 || !s.Digest { // first tick after 07:30 also claims the day's digest
		t.Fatalf("first tick: %+v", s)
	}
	stamped := func(id int64) (bool, bool) {
		b, err := db.GetBooking(id)
		if err != nil {
			t.Fatal(err)
		}
		return !b.ReminderSentAt.IsZero(), !b.AdminAlertAt.IsZero()
	}
	if r, _ := stamped(remind); !r {
		t.Error("tomorrow's booking should be stamped as reminded")
	}
	if r, _ := stamped(conRemind); !r {
		t.Error("tomorrow's contacted booking should be stamped as reminded")
	}
	for _, id := range []int64{noEmail, cancelled, soon, later} {
		if r, _ := stamped(id); r {
			t.Errorf("booking %d should not be reminded", id)
		}
	}
	if _, a := stamped(soon); !a {
		t.Error("booking in 50 min should be alerted")
	}
	if _, a := stamped(conSoon); !a {
		t.Error("contacted booking in 55 min should be alerted")
	}
	if _, a := stamped(later); a {
		t.Error("booking in 90 min should not be alerted yet")
	}

	// Same instant again: nothing new.
	if s := runScheduledJobs(now); s.Reminders != 0 || s.Alerts != 0 || s.Digest {
		t.Fatalf("second tick should be quiet: %+v", s)
	}
	// 40 minutes on: Eve is now within the hour.
	if s := runScheduledJobs(now.Add(40 * time.Minute)); s.Alerts != 1 {
		t.Fatalf("Eve should be alerted: %+v", s)
	}

	// Reminders only go in the 4–8 pm window.
	mk("Gus", "gus@example.test", now.AddDate(0, 0, 2).Add(-8*time.Hour), db.BookingBooked) // day after tomorrow, 9:00
	if s := runScheduledJobs(now.AddDate(0, 0, 1).Add(-7 * time.Hour)); s.Reminders != 0 {  // 10:00 tomorrow
		t.Fatalf("no reminders in the morning: %+v", s)
	}
	if s := runScheduledJobs(now.AddDate(0, 0, 1)); s.Reminders != 1 { // 17:00 tomorrow
		t.Fatalf("Gus should be reminded: %+v", s)
	}

	// Digest: not before 07:30, then exactly once for the day.
	morning := time.Date(2026, 8, 27, 7, 15, 0, 0, db.Melbourne) // the 26th was already claimed by the ticks above
	if s := runScheduledJobs(morning); s.Digest {
		t.Fatal("digest before 07:30")
	}
	if s := runScheduledJobs(morning.Add(20 * time.Minute)); !s.Digest {
		t.Fatal("digest expected at 07:35")
	}
	if s := runScheduledJobs(morning.Add(2 * time.Hour)); s.Digest {
		t.Fatal("digest sent twice in one day")
	}
	d, err := buildDigest(morning)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Today) != 1 || len(d.New) != 1 || d.New[0].ID != fresh {
		t.Fatalf("digest content: today=%d new=%d", len(d.Today), len(d.New))
	}
	// A day with nothing on sends nothing.
	quiet := time.Date(2026, 9, 30, 8, 0, 0, 0, db.Melbourne)
	if err := db.UpdateBookingStatus(fresh, db.BookingSpam); err != nil {
		t.Fatal(err)
	}
	if s := runScheduledJobs(quiet); s.Digest {
		t.Fatal("empty digest should not send")
	}
}

func TestSchedulerMailTemplates(t *testing.T) {
	site = siteConfig{BaseURL: "https://example.test", Email: "me@example.test", Phone: "0400 000 000"}
	start := time.Date(2026, 8, 26, 10, 0, 0, 0, db.Melbourne)
	b := &db.Booking{ID: 9, Name: "Ann <b>x</b>", Email: "ann@example.test", Phone: "0400 111 222", Suburb: "Donvale",
		Address: "12 Smith St, Donvale VIC 3111", ServiceSlug: "email-outlook", Issue: "Outlook won't open", StartAt: start, DurationMin: 60,
		AdminNotes: "Bring the spare SSD"}
	out, err := renderMail("booking-reminder", newBookingMailData(b))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "<b>x</b>") || !strings.Contains(out, "Wednesday 26 August 2026, 10:00 am") {
		t.Errorf("booking-reminder:\n%s", out)
	}
	d := bookingSoonData{bookingMailData: newBookingMailData(b), In: "45 min", Notes: b.AdminNotes, MapURL: "https://www.google.com/maps/dir/?api=1&destination=12+Smith+St"}
	out, err = renderMail("booking-soon", d)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"In 45 min", "Bring the spare SSD", "/admin/bookings/9", "maps/dir"} {
		if !strings.Contains(out, want) {
			t.Errorf("booking-soon missing %q:\n%s", want, out)
		}
	}
	dg := digestData{Date: "Wednesday 26 August", Today: bookingRows([]db.Booking{*b}),
		Overdue: []digestInvoice{{Invoice: db.Invoice{ID: 3, Number: 1002, TotalCents: 15000}, Ref: "INV-1002", Customer: "Ann", Due: "1 Aug 2026", DaysLate: 25}},
		Quotes:  []db.Quote{{ID: 4, Name: "Bob", Email: "bob@example.test", TotalCost: 2500}}}
	out, err = renderMail("digest", dg)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Today's visits", "INV-1002", "$150.00", "25d", "Bob", "/admin\""} {
		if !strings.Contains(out, want) {
			t.Errorf("digest missing %q:\n%s", want, out)
		}
	}
	// nil mailer: every send is a logged no-op
	mail = nil
	if err := sendBookingReminder(b); err != nil {
		t.Error(err)
	}
	if err := sendBookingSoon(b, start.Add(-45*time.Minute)); err != nil {
		t.Error(err)
	}
	if err := sendDigest(dg); err != nil {
		t.Error(err)
	}
}
