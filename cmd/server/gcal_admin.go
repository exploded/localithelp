package main

import (
	"context"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"localithelp/db"
)

// /admin/calendar/settings — connect, disconnect, choose which calendars
// contribute busy times, and force a resync.

type calSettingsData struct {
	Flash          flash
	Configured     bool // GOOGLE_CLIENT_ID present, so connecting is possible
	Conn           *db.GoogleCalendar
	Calendars      []calSettingsCal
	ListErr        string
	AdminEmail     string
	SyncedBookings int
}

type calSettingsCal struct {
	ID      string
	Name    string
	Primary bool
	Skipped bool
	IsApp   bool // the calendar bookings are written to
}

func handleAdminCalendarSettings(w http.ResponseWriter, r *http.Request) {
	d := calSettingsData{
		Flash:      readFlash(r),
		Configured: gcalOAuth != nil,
		AdminEmail: adminEmail(),
	}
	conn, err := db.GetGoogleCalendar()
	if err != nil {
		log.Printf("gcal settings: %v", err)
		http.Error(w, "failed to load calendar settings", http.StatusInternalServerError)
		return
	}
	d.Conn = conn

	// The calendar list drives the busy-time tick boxes. A failure here is
	// shown inline; the rest of the page still works (including Disconnect).
	if conn.Connected() {
		if n, err := db.CountBookingsWithCalendarEvent(); err == nil {
			d.SyncedBookings = n
		}
		if api, err := gcalClientFor(conn); err != nil {
			d.ListErr = err.Error()
		} else {
			ctx, cancel := context.WithTimeout(r.Context(), gcalTimeout)
			defer cancel()
			cals, err := api.ListCalendars(ctx)
			if err != nil {
				d.ListErr = err.Error()
			} else {
				for _, c := range cals {
					d.Calendars = append(d.Calendars, calSettingsCal{
						ID: c.ID, Name: c.Summary, Primary: c.Primary,
						Skipped: conn.Skipped(c.ID), IsApp: c.ID == conn.CalendarID,
					})
				}
				sort.SliceStable(d.Calendars, func(i, j int) bool {
					a, b := d.Calendars[i], d.Calendars[j]
					if a.IsApp != b.IsApp {
						return a.IsApp
					}
					if a.Primary != b.Primary {
						return a.Primary
					}
					return strings.ToLower(a.Name) < strings.ToLower(b.Name)
				})
			}
		}
	}
	render(w, r, "admin-calendar-settings", d)
}

// handleAdminCalendarBusy saves which calendars are excluded from busy times.
// The form posts one "use" checkbox per calendar, so anything unticked (and not
// the app's own calendar) becomes a skip.
func handleAdminCalendarBusy(w http.ResponseWriter, r *http.Request) {
	const back = "/admin/calendar/settings"
	conn, err := db.GetGoogleCalendar()
	if err != nil || !conn.Connected() {
		redirectMsg(w, r, back, "err", "Calendar sync is not connected.")
		return
	}
	// requireAdmin only parses the form when the CSRF token came in the body,
	// so parse defensively before reading the repeated fields.
	if err := r.ParseForm(); err != nil {
		redirectMsg(w, r, back, "err", "Could not read the form.")
		return
	}
	used := map[string]bool{}
	for _, id := range r.Form["use"] {
		used[id] = true
	}
	var skips []string
	for _, id := range r.Form["calendar"] {
		if !used[id] && id != conn.CalendarID {
			skips = append(skips, id)
		}
	}
	if err := db.SetGoogleCalendarSkips(skips); err != nil {
		log.Printf("gcal skips: %v", err)
		redirectMsg(w, r, back, "err", "Could not save the busy-time calendars.")
		return
	}
	busyCache.clear()
	redirectMsg(w, r, back, "ok", "Busy-time calendars saved.")
}

// handleAdminCalendarResync re-pushes every current booking. It runs inline so
// the flash message can report the result — the admin asked for it and is
// waiting on it.
func handleAdminCalendarResync(w http.ResponseWriter, r *http.Request) {
	const back = "/admin/calendar/settings"
	conn, err := db.GetGoogleCalendar()
	if err != nil || !conn.Connected() {
		redirectMsg(w, r, back, "err", "Calendar sync is not connected.")
		return
	}
	n := resyncAllBookings()
	redirectMsg(w, r, back, "ok", "Resynced "+plural(n, "booking", "bookings")+".")
}

// plural renders "1 booking" / "3 bookings".
func plural(n int, one, many string) string {
	word := many
	if n == 1 {
		word = one
	}
	return strconv.Itoa(n) + " " + word
}
