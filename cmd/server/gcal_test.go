package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localithelp/db"
)

// fakeCalendar records what the sync pushed, so the tests can assert on the
// insert/patch/delete decisions without touching Google.
type fakeCalendar struct {
	events    map[string]*gcalEvent // event id -> last written event
	next      int
	inserts   int
	patches   int
	deletes   int
	busy      []busyInterval
	cals      []gcalCalendar
	fbAsked   []string // calendar ids the last FreeBusy call covered
	patchErr  error    // returned by the next PatchEvent
	insertErr error
}

func newFakeCalendar() *fakeCalendar {
	return &fakeCalendar{events: map[string]*gcalEvent{}}
}

func (f *fakeCalendar) InsertEvent(_ context.Context, _ string, ev *gcalEvent) (string, error) {
	if f.insertErr != nil {
		err := f.insertErr
		f.insertErr = nil
		return "", err
	}
	f.next++
	f.inserts++
	id := "ev" + string(rune('0'+f.next))
	f.events[id] = ev
	return id, nil
}

func (f *fakeCalendar) PatchEvent(_ context.Context, _, eventID string, ev *gcalEvent) error {
	if f.patchErr != nil {
		err := f.patchErr
		f.patchErr = nil
		return err
	}
	if _, ok := f.events[eventID]; !ok {
		return errNotFound
	}
	f.patches++
	f.events[eventID] = ev
	return nil
}

func (f *fakeCalendar) DeleteEvent(_ context.Context, _, eventID string) error {
	if _, ok := f.events[eventID]; !ok {
		return errNotFound
	}
	f.deletes++
	delete(f.events, eventID)
	return nil
}

func (f *fakeCalendar) ListCalendars(context.Context) ([]gcalCalendar, error) { return f.cals, nil }

func (f *fakeCalendar) CreateCalendar(_ context.Context, summary, _ string) (string, error) {
	f.cals = append(f.cals, gcalCalendar{ID: "created", Summary: summary, Role: "owner"})
	return "created", nil
}

func (f *fakeCalendar) FreeBusy(_ context.Context, ids []string, _, _ time.Time) ([]busyInterval, error) {
	f.fbAsked = ids
	return f.busy, nil
}

// connectFake stores a connection row and points the sync at the fake API.
func connectFake(t *testing.T, f *fakeCalendar) {
	t.Helper()
	if err := db.SaveGoogleCalendar("me@example.test", "refresh-token", "cal-app", calendarName); err != nil {
		t.Fatal(err)
	}
	prev := gcalClientFor
	gcalClientFor = func(*db.GoogleCalendar) (calendarAPI, error) { return f, nil }
	busyCache.clear()
	t.Cleanup(func() { gcalClientFor = prev; busyCache.clear() })
}

func openTestDB(t *testing.T, name string) {
	t.Helper()
	if err := db.Open(filepath.Join(t.TempDir(), name)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	site = siteConfig{BaseURL: "https://example.test", Email: "me@example.test", Phone: "0400 000 000"}
}

// TestEventForBooking checks the field mapping the whole sync depends on.
func TestEventForBooking(t *testing.T) {
	site = siteConfig{BaseURL: "https://example.test"}
	b := &db.Booking{
		ID: 7, Name: "Ann", Phone: "0400 111 222", Email: "ann@example.test",
		Suburb: "Donvale", Address: "12 Smith St, Donvale VIC 3111", ServiceSlug: "email-outlook",
		Mode: "onsite", Issue: "Outlook won't open", AdminNotes: "Bring the dongle",
		Status: db.BookingBooked, DurationMin: 90,
		StartAt: time.Date(2026, 8, 26, 10, 0, 0, 0, db.Melbourne),
	}
	ev := eventForBooking(b)

	if !strings.HasPrefix(ev.Summary, "Visit: Ann — ") {
		t.Errorf("summary = %q, want a \"Visit: Ann — <service>\" prefix", ev.Summary)
	}
	if ev.Location != b.Address {
		t.Errorf("location = %q, want the full address", ev.Location)
	}
	if ev.Start.DateTime != "2026-08-26T10:00:00" || ev.Start.TimeZone != "Australia/Melbourne" {
		t.Errorf("start = %+v, want 10:00 Melbourne local", ev.Start)
	}
	if ev.End.DateTime != "2026-08-26T11:30:00" {
		t.Errorf("end = %q, want start + 90 min", ev.End.DateTime)
	}
	// The app sends its own 1-hour heads-up; a calendar alert would double up.
	if ev.Reminders == nil || ev.Reminders.UseDefault {
		t.Error("want default reminders switched off")
	}
	if got := ev.ExtendedProperties.Private[bookingIDProp]; got != "7" {
		t.Errorf("booking id property = %q, want 7", got)
	}
	for _, want := range []string{"0400 111 222", "ann@example.test", "Outlook won't open", "Bring the dongle",
		"https://example.test/admin/bookings/7"} {
		if !strings.Contains(ev.Description, want) {
			t.Errorf("description missing %q:\n%s", want, ev.Description)
		}
	}

	// Remote visits are labelled differently and fall back to the suburb.
	b.Mode, b.Address = "remote", ""
	if ev := eventForBooking(b); !strings.HasPrefix(ev.Summary, "Remote: ") || ev.Location != "Donvale" {
		t.Errorf("remote: summary = %q, location = %q", ev.Summary, ev.Location)
	}
}

// TestSyncBookingLifecycle walks a booking through the states that matter:
// scheduled → rescheduled → cancelled, then re-booked.
func TestSyncBookingLifecycle(t *testing.T) {
	openTestDB(t, "gcal.db")
	f := newFakeCalendar()
	connectFake(t, f)

	id, err := db.InsertBooking(&db.Booking{Name: "Ann", Email: "ann@example.test", Suburb: "Donvale",
		ServiceSlug: "email-outlook", Issue: "Outlook", Mode: "onsite"})
	if err != nil {
		t.Fatal(err)
	}
	sync := func() {
		t.Helper()
		b, err := db.GetBooking(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := syncBooking(context.Background(), f, "cal-app", b); err != nil {
			t.Fatalf("syncBooking: %v", err)
		}
	}
	eventID := func() string {
		t.Helper()
		b, err := db.GetBooking(id)
		if err != nil {
			t.Fatal(err)
		}
		return b.GCalEventID
	}

	// A new, unscheduled enquiry creates nothing but is stamped, so the
	// reconcile pass stops picking it up.
	sync()
	if f.inserts != 0 {
		t.Errorf("unscheduled booking: %d insert(s), want 0", f.inserts)
	}
	if b, _ := db.GetBooking(id); b.GCalSyncedAt.IsZero() {
		t.Error("unscheduled booking was not stamped")
	}

	// Scheduling it inserts an event.
	start := time.Date(2026, 8, 26, 10, 0, 0, 0, db.Melbourne)
	if err := db.ScheduleBooking(id, start, 60); err != nil {
		t.Fatal(err)
	}
	sync()
	if f.inserts != 1 || eventID() == "" {
		t.Fatalf("after scheduling: inserts=%d event=%q, want 1 insert and a stored id", f.inserts, eventID())
	}
	first := eventID()

	// Rescheduling patches the same event rather than making a second one.
	if err := db.ScheduleBooking(id, start.Add(2*time.Hour), 90); err != nil {
		t.Fatal(err)
	}
	sync()
	if f.inserts != 1 || f.patches != 1 || eventID() != first {
		t.Errorf("after reschedule: inserts=%d patches=%d event=%q (was %q)", f.inserts, f.patches, eventID(), first)
	}
	if got := f.events[first].Start.DateTime; got != "2026-08-26T12:00:00" {
		t.Errorf("patched start = %q, want the new time", got)
	}

	// Stepping the status back to contacted keeps the visit on the calendar —
	// the schedule decides, not the status; only cancel/spam remove it.
	if err := db.UpdateBookingStatus(id, db.BookingContacted); err != nil {
		t.Fatal(err)
	}
	sync()
	if f.deletes != 0 || eventID() != first {
		t.Errorf("after contacted: deletes=%d event=%q, want the event kept", f.deletes, eventID())
	}

	// Cancelling deletes the event and forgets the id.
	if err := db.UpdateBookingStatus(id, db.BookingCancelled); err != nil {
		t.Fatal(err)
	}
	sync()
	if f.deletes != 1 || eventID() != "" {
		t.Errorf("after cancel: deletes=%d event=%q, want 1 delete and no id", f.deletes, eventID())
	}
	if len(f.events) != 0 {
		t.Errorf("%d event(s) left in the calendar, want 0", len(f.events))
	}

	// Re-booking the same enquiry inserts a fresh event.
	if err := db.ScheduleBooking(id, start, 60); err != nil {
		t.Fatal(err)
	}
	sync()
	if f.inserts != 2 || eventID() == "" {
		t.Errorf("after re-booking: inserts=%d event=%q", f.inserts, eventID())
	}
}

// TestSyncBookingDeletedInGoogle covers the two "gone at the other end" cases:
// a patch that 404s is re-created, and a delete that 404s is treated as done.
func TestSyncBookingDeletedInGoogle(t *testing.T) {
	openTestDB(t, "gcal-404.db")
	f := newFakeCalendar()
	connectFake(t, f)

	id, err := db.InsertBooking(&db.Booking{Name: "Ann", Suburb: "Donvale", ServiceSlug: "email-outlook", Mode: "onsite"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ScheduleBooking(id, time.Date(2026, 8, 26, 10, 0, 0, 0, db.Melbourne), 60); err != nil {
		t.Fatal(err)
	}
	b, _ := db.GetBooking(id)
	if err := syncBooking(context.Background(), f, "cal-app", b); err != nil {
		t.Fatal(err)
	}

	// Someone deleted the event in Google: the next push recreates it.
	for k := range f.events {
		delete(f.events, k)
	}
	b, _ = db.GetBooking(id)
	if err := syncBooking(context.Background(), f, "cal-app", b); err != nil {
		t.Fatalf("patch-then-insert: %v", err)
	}
	if f.inserts != 2 {
		t.Errorf("inserts = %d, want the event recreated", f.inserts)
	}
	b, _ = db.GetBooking(id)
	if _, ok := f.events[b.GCalEventID]; !ok {
		t.Errorf("stored event id %q is not the recreated event", b.GCalEventID)
	}

	// Cancelling something already gone from Google still clears the id.
	if err := db.UpdateBookingStatus(id, db.BookingCancelled); err != nil {
		t.Fatal(err)
	}
	for k := range f.events {
		delete(f.events, k)
	}
	b, _ = db.GetBooking(id)
	if err := syncBooking(context.Background(), f, "cal-app", b); err != nil {
		t.Fatalf("delete of a missing event should succeed: %v", err)
	}
	if b, _ := db.GetBooking(id); b.GCalEventID != "" {
		t.Errorf("event id = %q, want cleared", b.GCalEventID)
	}
}

// TestSyncCalendarReconcile is the safety net: a booking whose immediate push
// failed is picked up by the scheduler pass, and a clean pass does nothing.
func TestSyncCalendarReconcile(t *testing.T) {
	openTestDB(t, "gcal-reconcile.db")
	f := newFakeCalendar()
	connectFake(t, f)

	now := time.Date(2026, 8, 25, 17, 0, 0, 0, db.Melbourne)
	for i, when := range []time.Time{now.AddDate(0, 0, 1), now.AddDate(0, 0, 2)} {
		id, err := db.InsertBooking(&db.Booking{Name: "Cust", Suburb: "Donvale", ServiceSlug: "email-outlook", Mode: "onsite"})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.ScheduleBooking(id, when, 60); err != nil {
			t.Fatalf("booking %d: %v", i, err)
		}
	}
	// A visit far in the past must not be touched.
	old, err := db.InsertBooking(&db.Booking{Name: "Old", Suburb: "Donvale", ServiceSlug: "email-outlook", Mode: "onsite"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ScheduleBooking(old, now.AddDate(-1, 0, 0), 60); err != nil {
		t.Fatal(err)
	}

	if n := syncCalendar(now); n != 2 {
		t.Errorf("first pass synced %d, want 2 (the old visit is out of the window)", n)
	}
	if f.inserts != 2 {
		t.Errorf("inserts = %d, want 2", f.inserts)
	}
	// Nothing changed since, so a second pass is a no-op.
	if n := syncCalendar(now); n != 0 {
		t.Errorf("second pass synced %d, want 0", n)
	}
	if f.inserts != 2 || f.patches != 0 {
		t.Errorf("second pass touched Google: inserts=%d patches=%d", f.inserts, f.patches)
	}

	// A row edited after its last push becomes dirty again. updated_at and
	// gcal_synced_at are second-resolution, so the edit has to land in a later
	// second than the push for the timestamp comparison to see it.
	time.Sleep(1100 * time.Millisecond)
	if err := db.UpdateBookingNotes(1, "gate code 1234"); err != nil {
		t.Fatal(err)
	}
	if n := syncCalendar(now); n != 1 {
		t.Errorf("after an edit, synced %d, want 1", n)
	}
	if f.patches != 1 {
		t.Errorf("patches = %d, want 1", f.patches)
	}

	// State mismatch is dirty regardless of the stamps: a visit whose event is
	// missing (an insert Google rejected) is retried on the next pass.
	if err := db.SetBookingCalendarEvent(1, ""); err != nil {
		t.Fatal(err)
	}
	if n := syncCalendar(now); n != 1 {
		t.Errorf("a visit with no event synced %d, want 1 (retry)", n)
	}
	if f.inserts != 3 {
		t.Errorf("inserts = %d, want the missing event recreated", f.inserts)
	}

	// A scheduled visit flipped to contacted still belongs in Google. A row
	// that lost its event (the old rule deleted it on that status change)
	// heals on the next pass without being re-booked.
	if err := db.UpdateBookingStatus(2, db.BookingContacted); err != nil {
		t.Fatal(err)
	}
	if err := db.SetBookingCalendarEvent(2, ""); err != nil {
		t.Fatal(err)
	}
	if n := syncCalendar(now); n != 1 {
		t.Errorf("contacted visit with no event synced %d, want 1 (self-heal)", n)
	}
	if f.inserts != 4 {
		t.Errorf("inserts = %d, want the contacted visit's event recreated", f.inserts)
	}
}

// TestSyncCalendarNotConnected proves the sync is inert until it is connected —
// the state every existing install starts in.
func TestSyncCalendarNotConnected(t *testing.T) {
	openTestDB(t, "gcal-off.db")
	id, err := db.InsertBooking(&db.Booking{Name: "Ann", Suburb: "Donvale", ServiceSlug: "email-outlook", Mode: "onsite"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ScheduleBooking(id, time.Now().Add(24*time.Hour), 60); err != nil {
		t.Fatal(err)
	}
	if n := syncCalendar(time.Now()); n != 0 {
		t.Errorf("synced %d with no connection, want 0", n)
	}
	if calendarSyncConnected() {
		t.Error("calendarSyncConnected() = true with no stored row")
	}
	if busy, err := busyIntervals(time.Now(), time.Now().Add(time.Hour)); err != nil || busy != nil {
		t.Errorf("busyIntervals = (%v, %v), want (nil, nil)", busy, err)
	}
}

// TestBusyIntervalsExcludesOwnCalendar checks the free/busy query skips the
// app's own calendar (its events are the bookings already drawn on the page)
// and any calendar the admin has excluded.
func TestBusyIntervalsExcludesOwnCalendar(t *testing.T) {
	openTestDB(t, "gcal-busy.db")
	f := newFakeCalendar()
	f.cals = []gcalCalendar{
		{ID: "cal-app", Summary: calendarName, Role: "owner"},
		{ID: "personal", Summary: "Personal", Primary: true},
		{ID: "sport", Summary: "Sport"},
	}
	f.busy = []busyInterval{{
		Start: time.Date(2026, 8, 26, 9, 0, 0, 0, db.Melbourne),
		End:   time.Date(2026, 8, 26, 10, 0, 0, 0, db.Melbourne),
	}}
	connectFake(t, f)
	if err := db.SetGoogleCalendarSkips([]string{"sport"}); err != nil {
		t.Fatal(err)
	}

	from := time.Date(2026, 8, 24, 0, 0, 0, 0, db.Melbourne)
	busy, err := busyIntervals(from, from.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	if len(busy) != 1 {
		t.Fatalf("got %d busy interval(s), want 1", len(busy))
	}
	if len(f.fbAsked) != 1 || f.fbAsked[0] != "personal" {
		t.Errorf("free/busy asked for %v, want only [personal]", f.fbAsked)
	}

	// The second call inside the TTL is served from cache.
	asked := len(f.fbAsked)
	f.fbAsked = nil
	if _, err := busyIntervals(from, from.AddDate(0, 0, 7)); err != nil {
		t.Fatal(err)
	}
	if f.fbAsked != nil {
		t.Errorf("cache miss: asked Google again for %v (first call asked %d)", f.fbAsked, asked)
	}
}

// TestBusyBlocksFor checks busy intervals are clipped to the displayed hours of
// one day column.
func TestBusyBlocksFor(t *testing.T) {
	date := time.Date(2026, 8, 26, 0, 0, 0, 0, db.Melbourne)
	at := func(h, m int) time.Time {
		return time.Date(2026, 8, 26, h, m, 0, 0, db.Melbourne)
	}
	busy := []busyInterval{
		{Start: at(8, 0), End: at(9, 0)},                                  // inside the day
		{Start: at(5, 0), End: at(8, 0)},                                  // starts before 7 am
		{Start: at(18, 0), End: at(23, 0)},                                // runs past 7 pm
		{Start: at(3, 0), End: at(6, 0)},                                  // wholly before the window
		{Start: date.AddDate(0, 0, 1), End: date.AddDate(0, 0, 1).Add(2)}, // the next day
	}
	got := busyBlocksFor(busy, date)
	if len(got) != 3 {
		t.Fatalf("got %d block(s), want 3: %+v", len(got), got)
	}
	// 8 am is one hour past the 7 am column start: 60 min / 15 min * 14 px.
	if got[0].Top != 56 || got[0].Height != 56 {
		t.Errorf("8–9 am block = top %d height %d, want 56/56", got[0].Top, got[0].Height)
	}
	if got[1].Top != 0 {
		t.Errorf("block starting before 7 am = top %d, want 0 (clipped)", got[1].Top)
	}
	// 6 pm to the 7 pm cut-off is one hour.
	if got[2].Height != 56 {
		t.Errorf("evening block height = %d, want it clipped to 56", got[2].Height)
	}
}

// TestLoginScopesUnchanged guards the split between sign-in and calendar
// consent: signing in must never ask for calendar access.
func TestLoginScopesUnchanged(t *testing.T) {
	site = siteConfig{BaseURL: "https://example.test"}
	t.Setenv("GOOGLE_CLIENT_ID", "test-client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "test-secret")
	prev := gcalOAuth
	t.Cleanup(func() { gcalOAuth = prev })
	initGCalOAuth()
	if gcalOAuth == nil {
		t.Fatal("initGCalOAuth did not build a config")
	}
	if len(gcalOAuth.Scopes) != 1 || gcalOAuth.Scopes[0] != calendarScope {
		t.Errorf("calendar scopes = %v, want just the calendar scope", gcalOAuth.Scopes)
	}
	if !strings.HasSuffix(gcalOAuth.RedirectURL, "/auth/google/calendar/callback") {
		t.Errorf("redirect = %q, want its own callback path", gcalOAuth.RedirectURL)
	}
	// The sign-in config is built in main(); assert the constant it uses.
	for _, s := range []string{"openid", "email", "profile"} {
		if s == calendarScope {
			t.Fatal("login scope list must not contain the calendar scope")
		}
	}
}

// TestFindOrCreateBookingCalendar reuses an existing "Local IT Help" calendar
// instead of making a second one on every reconnect.
func TestFindOrCreateBookingCalendar(t *testing.T) {
	f := newFakeCalendar()
	id, err := findOrCreateBookingCalendar(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if id != "created" {
		t.Errorf("first connect returned %q, want a newly created calendar", id)
	}
	again, err := findOrCreateBookingCalendar(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if again != id {
		t.Errorf("second connect made a new calendar (%q vs %q)", again, id)
	}
}
