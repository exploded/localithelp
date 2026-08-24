package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"localithelp/db"
)

// Outbound sync: bookings → Google Calendar. The app is the source of truth,
// so edits made in Google are never pulled back — a reschedule has to go
// through /admin/bookings/{id} so the customer gets the confirmation email.

// syncWindow bounds which bookings the reconcile pass considers.
const (
	syncPast   = 30 * 24 * time.Hour  // ignore visits older than this
	syncFuture = 365 * 24 * time.Hour // ...or further out than this
	syncLimit  = 200                  // most rows pushed in one pass
)

// bookingIDProp is the private extended property carrying the booking id, so an
// event can still be matched if gcal_event_id is ever lost.
const bookingIDProp = "localithelp_booking_id"

// wantsEvent reports whether a booking should exist in Google Calendar: a real,
// scheduled visit that hasn't been cancelled or marked spam.
func wantsEvent(b *db.Booking) bool {
	if b.StartAt.IsZero() {
		return false
	}
	switch b.Status {
	case db.BookingBooked, db.BookingDone, db.BookingInvoiced, db.BookingPaid:
		return true
	}
	return false
}

// eventForBooking maps a booking onto the Google Calendar event fields the app
// owns. Times carry an explicit Melbourne zone so Google shows the visit at the
// right local time wherever it is read.
func eventForBooking(b *db.Booking) *gcalEvent {
	title := "Visit"
	if strings.EqualFold(b.Mode, "remote") {
		title = "Remote"
	}
	if s, ok := findService(b.ServiceSlug); ok {
		title += ": " + b.Name + " — " + s.Title
	} else {
		title += ": " + b.Name
	}

	loc := strings.TrimSpace(b.Address)
	if loc == "" {
		loc = strings.TrimSpace(b.Suburb)
	}

	var desc strings.Builder
	if b.Phone != "" {
		fmt.Fprintf(&desc, "Phone: %s\n", b.Phone)
	}
	if b.Email != "" {
		fmt.Fprintf(&desc, "Email: %s\n", b.Email)
	}
	if b.Issue != "" {
		fmt.Fprintf(&desc, "\n%s\n", strings.TrimSpace(b.Issue))
	}
	if b.AdminNotes != "" {
		fmt.Fprintf(&desc, "\nNotes: %s\n", strings.TrimSpace(b.AdminNotes))
	}
	link := site.BaseURL + "/admin/bookings/" + strconv.FormatInt(b.ID, 10)
	fmt.Fprintf(&desc, "\n%s", link)

	const layout = "2006-01-02T15:04:05"
	start := b.StartAt.In(db.Melbourne)
	end := b.EndAt().In(db.Melbourne)
	return &gcalEvent{
		Summary:     title,
		Description: desc.String(),
		Location:    loc,
		Start:       &gcalEventTime{DateTime: start.Format(layout), TimeZone: db.Melbourne.String()},
		End:         &gcalEventTime{DateTime: end.Format(layout), TimeZone: db.Melbourne.String()},
		// The app already sends its own 1-hour heads-up email; a calendar
		// notification on top would double up.
		Reminders:          &gcalReminders{UseDefault: false},
		ExtendedProperties: &gcalExtProps{Private: map[string]string{bookingIDProp: strconv.FormatInt(b.ID, 10)}},
		Source:             &gcalEventSource{Title: "Local IT Help", URL: link},
	}
}

// syncBooking brings one booking's Google Calendar event into line and stamps
// gcal_synced_at. It is safe to call for any booking: rows that shouldn't have
// an event and don't are stamped and skipped, so they stop looking dirty.
func syncBooking(ctx context.Context, api calendarAPI, calendarID string, b *db.Booking) error {
	switch {
	case wantsEvent(b) && b.GCalEventID == "":
		id, err := api.InsertEvent(ctx, calendarID, eventForBooking(b))
		if err != nil {
			return err
		}
		if id == "" {
			return fmt.Errorf("insert returned no event id")
		}
		return db.SetBookingCalendarEvent(b.ID, id)

	case wantsEvent(b):
		err := api.PatchEvent(ctx, calendarID, b.GCalEventID, eventForBooking(b))
		if errors.Is(err, errNotFound) {
			// Deleted in Google: recreate it rather than losing the visit.
			id, err := api.InsertEvent(ctx, calendarID, eventForBooking(b))
			if err != nil {
				return err
			}
			return db.SetBookingCalendarEvent(b.ID, id)
		}
		if err != nil {
			return err
		}
		return db.SetBookingCalendarEvent(b.ID, b.GCalEventID)

	case b.GCalEventID != "":
		// Cancelled, marked spam, or the start time was cleared.
		if err := api.DeleteEvent(ctx, calendarID, b.GCalEventID); err != nil && !errors.Is(err, errNotFound) {
			return err
		}
		return db.SetBookingCalendarEvent(b.ID, "")

	default:
		// Nothing to do (e.g. a new, unscheduled enquiry). Stamp it so the
		// reconcile pass doesn't keep picking it up.
		return db.SetBookingCalendarEvent(b.ID, "")
	}
}

// syncBookingSoon pushes one booking in the background, right after the admin
// saves it. Failures are logged and left to the next reconcile pass — a booking
// must never fail to save because Google is unreachable.
func syncBookingSoon(id int64) {
	// Without an OAuth client the sync can't be connected, so skip the
	// goroutine entirely (this is also what keeps the handler tests from
	// touching the database in the background).
	if gcalOAuth == nil {
		return
	}
	go func() {
		g, api, ok := calendarTarget()
		if !ok {
			return
		}
		b, err := db.GetBooking(id)
		if err != nil {
			log.Printf("gcal: load booking #%d: %v", id, err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), gcalTimeout)
		defer cancel()
		if err := syncBooking(ctx, api, g.CalendarID, b); err != nil {
			logGCal(fmt.Sprintf("sync booking #%d", id), err)
			return
		}
		if err := db.MarkGoogleCalendarSynced(); err != nil {
			log.Printf("gcal: stamp sync: %v", err)
		}
	}()
}

// calendarTarget returns the stored connection and an API client, or ok=false
// when sync isn't connected (the normal state before setup, and in tests).
func calendarTarget() (*db.GoogleCalendar, calendarAPI, bool) {
	g, err := db.GetGoogleCalendar()
	if err != nil {
		log.Printf("gcal: load connection: %v", err)
		return nil, nil, false
	}
	if !g.Connected() {
		return nil, nil, false
	}
	api, err := gcalClientFor(g)
	if err != nil {
		logGCal("client", err)
		return nil, nil, false
	}
	return g, api, true
}

// calendarSyncConnected reports whether a usable connection is stored, for the
// views that show "connected / not connected".
func calendarSyncConnected() bool {
	g, err := db.GetGoogleCalendar()
	if err != nil {
		log.Printf("gcal: load connection: %v", err)
		return false
	}
	return g.Connected()
}

// syncCalendar is the scheduler job: it pushes every booking whose row changed
// since its last successful push. This is what makes the sync reliable — an
// immediate push lost to a network blip or a restart mid-request is picked up
// within one tick (15 minutes).
func syncCalendar(now time.Time) int {
	g, api, ok := calendarTarget()
	if !ok {
		return 0
	}
	bs, err := db.ListBookingsForCalendarSync(now.Add(-syncPast), now.Add(syncFuture), syncLimit)
	if err != nil {
		log.Printf("gcal: list dirty bookings: %v", err)
		return 0
	}
	if len(bs) == 0 {
		return 0
	}
	n, failed := pushBookings(api, g.CalendarID, bs)
	if failed == 0 {
		if err := db.MarkGoogleCalendarSynced(); err != nil {
			log.Printf("gcal: stamp sync: %v", err)
		}
	}
	return n
}

// resyncAllBookings re-pushes every current booking, ignoring the dirty check.
// Used on first connect and by the "Resync all" button.
func resyncAllBookings() int {
	g, api, ok := calendarTarget()
	if !ok {
		return 0
	}
	now := time.Now().In(db.Melbourne)
	bs, err := db.ListBookingsForCalendarBackfill(now.Add(-syncPast), now.Add(syncFuture))
	if err != nil {
		log.Printf("gcal: list bookings for resync: %v", err)
		return 0
	}
	n, failed := pushBookings(api, g.CalendarID, bs)
	if failed == 0 {
		if err := db.MarkGoogleCalendarSynced(); err != nil {
			log.Printf("gcal: stamp sync: %v", err)
		}
	}
	log.Printf("gcal: resync pushed %d booking(s), %d failed", n, failed)
	return n
}

// pushBookings syncs each booking in turn, returning how many succeeded and how
// many failed. One bad row never stops the rest.
func pushBookings(api calendarAPI, calendarID string, bs []db.Booking) (done, failed int) {
	for i := range bs {
		b := &bs[i]
		ctx, cancel := context.WithTimeout(context.Background(), gcalTimeout)
		err := syncBooking(ctx, api, calendarID, b)
		cancel()
		if err != nil {
			logGCal(fmt.Sprintf("sync booking #%d", b.ID), err)
			failed++
			continue
		}
		done++
	}
	return done, failed
}
