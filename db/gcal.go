package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"localithelp/db/sqlc"
)

// GoogleCalendar holds the admin's Google Calendar connection. There is at most
// one row; the zero value means "not connected".
type GoogleCalendar struct {
	AccountEmail  string
	RefreshToken  string
	CalendarID    string    // the calendar bookings are written to
	CalendarName  string    // display name, for the settings page
	SkipCalendars []string  // calendar ids excluded from busy times
	ConnectedAt   time.Time // UTC
	LastSyncAt    time.Time // UTC; zero until the first successful call
	LastError     string
}

// Connected reports whether a usable connection is stored.
func (g *GoogleCalendar) Connected() bool {
	return g != nil && g.RefreshToken != "" && g.CalendarID != ""
}

// Skipped reports whether busy times from calendarID should be ignored.
func (g *GoogleCalendar) Skipped(calendarID string) bool {
	if g == nil {
		return false
	}
	for _, id := range g.SkipCalendars {
		if id == calendarID {
			return true
		}
	}
	return false
}

// GetGoogleCalendar returns the stored connection, or nil when sync has never
// been connected (or was disconnected). A missing row is not an error.
func GetGoogleCalendar() (*GoogleCalendar, error) {
	r, err := q.GetGoogleCalendar(context.Background())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &GoogleCalendar{
		AccountEmail: r.AccountEmail, RefreshToken: r.RefreshToken,
		CalendarID: r.CalendarID, CalendarName: r.CalendarName,
		SkipCalendars: splitLines(r.SkipCalendars),
		ConnectedAt:   parseUTC(r.ConnectedAt), LastSyncAt: parseUTC(r.LastSyncAt),
		LastError: r.LastError,
	}, nil
}

// SaveGoogleCalendar stores (or replaces) the connection and clears any error.
func SaveGoogleCalendar(accountEmail, refreshToken, calendarID, calendarName string) error {
	return q.SaveGoogleCalendar(context.Background(), sqlc.SaveGoogleCalendarParams{
		AccountEmail: accountEmail, RefreshToken: refreshToken,
		CalendarID: calendarID, CalendarName: calendarName,
	})
}

// SetGoogleCalendarSkips replaces the list of calendars excluded from busy times.
func SetGoogleCalendarSkips(ids []string) error {
	return q.SetGoogleCalendarSkips(context.Background(), strings.Join(ids, "\n"))
}

// MarkGoogleCalendarSynced stamps a successful call and clears the last error.
func MarkGoogleCalendarSynced() error {
	return q.MarkGoogleCalendarSynced(context.Background())
}

// SetGoogleCalendarError records the most recent failure for the settings page.
func SetGoogleCalendarError(msg string) error {
	return q.SetGoogleCalendarError(context.Background(), msg)
}

// DisconnectGoogleCalendar drops the stored token and forgets every pushed
// event id. Events already in Google are left in place.
func DisconnectGoogleCalendar() error {
	if err := q.DeleteGoogleCalendar(context.Background()); err != nil {
		return err
	}
	return q.ClearBookingCalendarEventIDs(context.Background())
}

// ── Booking ↔ calendar ──

// ListBookingsForCalendarSync returns bookings whose row changed since the last
// successful push. Rows starting inside [from, to) are included, as is any row
// that still holds an event id (so cancellations and cleared times are pushed
// even once the visit falls outside the window). limit caps a single pass.
func ListBookingsForCalendarSync(from, to time.Time, limit int) ([]Booking, error) {
	return sqlcBookings(q.ListBookingsForCalendarSync(context.Background(), sqlc.ListBookingsForCalendarSyncParams{
		StartAt: FormatStartAt(from), StartAt_2: FormatStartAt(to), Limit: int64(limit),
	}))
}

// ListBookingsForCalendarBackfill returns every booking in [from, to) plus any
// row holding an event id, ignoring the dirty check — used by "resync all".
func ListBookingsForCalendarBackfill(from, to time.Time) ([]Booking, error) {
	return sqlcBookings(q.ListBookingsForCalendarBackfill(context.Background(), sqlc.ListBookingsForCalendarBackfillParams{
		StartAt: FormatStartAt(from), StartAt_2: FormatStartAt(to),
	}))
}

// SetBookingCalendarEvent records the Google event id for a booking and stamps
// the sync time. Pass an empty id after deleting the event. updated_at is left
// untouched so the row does not look dirty again straight away.
func SetBookingCalendarEvent(id int64, eventID string) error {
	return q.SetBookingCalendarEvent(context.Background(), sqlc.SetBookingCalendarEventParams{
		GcalEventID: eventID, ID: id,
	})
}

// CountBookingsWithCalendarEvent reports how many bookings currently hold a
// Google Calendar event, for the settings page.
func CountBookingsWithCalendarEvent() (int, error) {
	n, err := q.CountBookingsWithCalendarEvent(context.Background())
	return int(n), err
}

func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
