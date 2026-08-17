package main

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mchugh.com.au/db"
)

// ── Admin: bookings ──

// bookingRow is a booking decorated for list/table display.
type bookingRow struct {
	db.Booking
	When         string // "Thu 20 Aug, 9:30 am" or ""
	ServiceTitle string
	Preview      string // first ~90 chars of the issue
}

func bookingRows(bs []db.Booking) []bookingRow {
	out := make([]bookingRow, len(bs))
	for i, b := range bs {
		out[i] = newBookingRow(b)
	}
	return out
}

func newBookingRow(b db.Booking) bookingRow {
	r := bookingRow{Booking: b, When: fmtWhen(b.StartAt), Preview: b.Issue}
	if s, ok := findService(b.ServiceSlug); ok {
		r.ServiceTitle = s.Title
	}
	if rs := []rune(r.Preview); len(rs) > 90 {
		r.Preview = string(rs[:90]) + "…"
	}
	return r
}

// durationChoices are the schedule form's duration options (minutes).
var durationChoices = []int{15, 30, 45, 60, 75, 90, 105, 120, 150, 180, 240}

// redirectMsg redirects to path with a flash message in the query string.
func redirectMsg(w http.ResponseWriter, r *http.Request, path, key, msg string) {
	if msg != "" {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		path += sep + key + "=" + url.QueryEscape(msg)
	}
	http.Redirect(w, r, path, http.StatusSeeOther)
}

// flash reads ok/err messages from the query string.
type flash struct{ OK, Err string }

func readFlash(r *http.Request) flash {
	return flash{OK: r.URL.Query().Get("ok"), Err: r.URL.Query().Get("err")}
}

type adminBookingsData struct {
	Flash    flash
	Status   string // active filter ("" = all except spam)
	Statuses []string
	Counts   map[string]int
	Rows     []bookingRow
}

func handleAdminBookings(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	var (
		bs  []db.Booking
		err error
	)
	if status == "" {
		bs, err = db.ListBookings()
		if err == nil {
			kept := bs[:0]
			for _, b := range bs {
				if b.Status != db.BookingSpam {
					kept = append(kept, b)
				}
			}
			bs = kept
		}
	} else {
		bs, err = db.ListBookingsByStatus(status)
	}
	if err != nil {
		log.Printf("admin bookings: %v", err)
		http.Error(w, "failed to load bookings", http.StatusInternalServerError)
		return
	}
	counts, _ := db.CountBookingsByStatus()
	render(w, r, "admin-bookings", adminBookingsData{
		Flash: readFlash(r), Status: status, Statuses: db.BookingStatuses, Counts: counts, Rows: bookingRows(bs),
	})
}

type adminBookingData struct {
	Flash        flash
	B            *db.Booking
	Row          bookingRow
	Cust         *db.Customer
	StartValue   string // datetime-local value for the schedule form
	Durations    []int
	Invoices     []db.Invoice
	Parent       *db.Booking
	Children     []bookingRow
	Statuses     []string
	CanInvoice   bool
	FollowupText string
}

func loadBooking(w http.ResponseWriter, r *http.Request) *db.Booking {
	b, err := db.GetBooking(pathID(r))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			log.Printf("admin booking: %v", err)
			http.Error(w, "failed to load booking", http.StatusInternalServerError)
		}
		return nil
	}
	return b
}

func handleAdminBooking(w http.ResponseWriter, r *http.Request) {
	b := loadBooking(w, r)
	if b == nil {
		return
	}
	d := adminBookingData{
		Flash: readFlash(r), B: b, Row: newBookingRow(*b), Durations: durationChoices, Statuses: db.BookingStatuses,
		FollowupText: "Follow-up visit — return repaired equipment and check everything is working.",
	}
	if b.CustomerID != 0 {
		d.Cust, _ = db.GetCustomer(b.CustomerID)
	}
	// Prefill from the calendar (?start=2006-01-02T15:04) or the saved time.
	start := b.StartAt
	if s := r.URL.Query().Get("start"); s != "" {
		if t, err := time.ParseInLocation("2006-01-02T15:04", s, db.Melbourne); err == nil {
			start = t
		}
	}
	if start.IsZero() {
		// Default: next whole hour tomorrow at 9:00.
		start = db.Today().AddDate(0, 0, 1).Add(9 * time.Hour)
	}
	d.StartValue = start.In(db.Melbourne).Format("2006-01-02T15:04")
	d.Invoices, _ = db.ListInvoicesByBooking(b.ID)
	if b.ParentBookingID != 0 {
		d.Parent, _ = db.GetBooking(b.ParentBookingID)
	}
	kids, _ := db.ListChildBookings(b.ID)
	d.Children = bookingRows(kids)
	d.CanInvoice = b.CustomerID != 0 && b.Status != db.BookingSpam && b.Status != db.BookingCancelled
	render(w, r, "admin-booking", d)
}

func handleAdminBookingSchedule(w http.ResponseWriter, r *http.Request) {
	b := loadBooking(w, r)
	if b == nil {
		return
	}
	back := "/admin/bookings/" + strconv.FormatInt(b.ID, 10)
	start, err := time.ParseInLocation("2006-01-02T15:04", r.FormValue("start"), db.Melbourne)
	if err != nil {
		redirectMsg(w, r, back, "err", "Please choose a valid date and time.")
		return
	}
	dur, _ := strconv.Atoi(r.FormValue("duration"))
	if dur < 15 || dur > 480 {
		dur = 60
	}
	rescheduled := b.Status == db.BookingBooked && !b.StartAt.IsZero() && !b.StartAt.Equal(start)
	if err := db.ScheduleBooking(b.ID, start, dur); err != nil {
		log.Printf("schedule booking #%d: %v", b.ID, err)
		redirectMsg(w, r, back, "err", "Could not save the schedule.")
		return
	}
	b.StartAt, b.DurationMin, b.Status = start, dur, db.BookingBooked
	msg := "Booked for " + fmtWhen(start) + "."
	if r.FormValue("notify") != "" {
		if err := sendBookingConfirmation(b, rescheduled); err != nil {
			logMailErr("booking confirmation", err)
			redirectMsg(w, r, back, "err", msg+" But the confirmation email failed: "+err.Error())
			return
		}
		msg += " Confirmation emailed."
	}
	redirectMsg(w, r, back, "ok", msg)
}

// allowedManualStatuses are the statuses the admin can set directly; invoiced/paid
// come from invoice actions.
var allowedManualStatuses = map[string]bool{
	db.BookingNew: true, db.BookingContacted: true, db.BookingDone: true, db.BookingCancelled: true, db.BookingSpam: true,
}

func handleAdminBookingStatus(w http.ResponseWriter, r *http.Request) {
	b := loadBooking(w, r)
	if b == nil {
		return
	}
	back := "/admin/bookings/" + strconv.FormatInt(b.ID, 10)
	status := r.FormValue("status")
	if !allowedManualStatuses[status] {
		redirectMsg(w, r, back, "err", "That status can't be set by hand.")
		return
	}
	if err := db.UpdateBookingStatus(b.ID, status); err != nil {
		log.Printf("booking #%d status: %v", b.ID, err)
		redirectMsg(w, r, back, "err", "Could not update the status.")
		return
	}
	msg := "Status set to " + status + "."
	if status == db.BookingCancelled && r.FormValue("notify") != "" {
		if err := sendBookingCancellation(b, strings.TrimSpace(r.FormValue("reason"))); err != nil {
			logMailErr("booking cancellation", err)
			redirectMsg(w, r, back, "err", msg+" But the cancellation email failed: "+err.Error())
			return
		}
		msg += " Customer emailed."
	}
	redirectMsg(w, r, back, "ok", msg)
}

func handleAdminBookingNotes(w http.ResponseWriter, r *http.Request) {
	b := loadBooking(w, r)
	if b == nil {
		return
	}
	back := "/admin/bookings/" + strconv.FormatInt(b.ID, 10)
	notes := strings.TrimSpace(r.FormValue("notes"))
	if len(notes) > 5000 {
		notes = notes[:5000]
	}
	if err := db.UpdateBookingNotes(b.ID, notes); err != nil {
		redirectMsg(w, r, back, "err", "Could not save notes.")
		return
	}
	redirectMsg(w, r, back, "ok", "Notes saved.")
}

func handleAdminBookingFollowup(w http.ResponseWriter, r *http.Request) {
	b := loadBooking(w, r)
	if b == nil {
		return
	}
	issue := strings.TrimSpace(r.FormValue("issue"))
	if issue == "" {
		issue = "Follow-up visit"
	}
	id, err := db.CreateFollowup(b, issue)
	if err != nil {
		log.Printf("followup for #%d: %v", b.ID, err)
		redirectMsg(w, r, "/admin/bookings/"+strconv.FormatInt(b.ID, 10), "err", "Could not create the follow-up booking.")
		return
	}
	redirectMsg(w, r, "/admin/bookings/"+strconv.FormatInt(id, 10), "ok", "Follow-up booking created — pick a time below.")
}

// ── Admin: calendar ──

const (
	calStartHour = 7
	calEndHour   = 19 // exclusive
	calSlotMin   = 15
	calSlotPx    = 14 // height of one 15-minute slot
)

type calSlot struct {
	Time   string // "09:15"
	Href   string // set when picking a time for a booking
	OnHour bool
}

type calEvent struct {
	Row    bookingRow
	Top    int // px from column top
	Height int
	Href   string
	Label  string // "9:30 am – 10:30 am"
}

type calDay struct {
	Date    time.Time
	Label   string // "Mon 17"
	IsToday bool
	Slots   []calSlot
	Events  []calEvent
}

type calendarData struct {
	WeekStart time.Time
	WeekLabel string
	Prev      string // week param values
	Next      string
	ThisWeek  string
	Days      []calDay
	Hours     []int
	SlotPx    int
	ColPx     int
	ForID     int64
	ForName   string
}

func handleAdminCalendar(w http.ResponseWriter, r *http.Request) {
	weekStart := startOfWeek(db.Today())
	if s := r.URL.Query().Get("week"); s != "" {
		if t, err := time.ParseInLocation("2006-01-02", s, db.Melbourne); err == nil {
			weekStart = startOfWeek(t)
		}
	}
	weekEnd := weekStart.AddDate(0, 0, 7)
	bs, err := db.ListBookingsBetween(weekStart, weekEnd)
	if err != nil {
		log.Printf("calendar: %v", err)
		http.Error(w, "failed to load calendar", http.StatusInternalServerError)
		return
	}
	forID, _ := strconv.ParseInt(r.URL.Query().Get("for"), 10, 64)
	d := calendarData{
		WeekStart: weekStart,
		WeekLabel: weekStart.Format("2 Jan") + " – " + weekEnd.AddDate(0, 0, -1).Format("2 Jan 2006"),
		Prev:      weekStart.AddDate(0, 0, -7).Format("2006-01-02"),
		Next:      weekEnd.Format("2006-01-02"),
		ThisWeek:  startOfWeek(db.Today()).Format("2006-01-02"),
		SlotPx:    calSlotPx,
		ColPx:     (calEndHour - calStartHour) * 60 / calSlotMin * calSlotPx,
		ForID:     forID,
	}
	if forID != 0 {
		if fb, err := db.GetBooking(forID); err == nil {
			d.ForName = fb.Name
		}
	}
	for h := calStartHour; h < calEndHour; h++ {
		d.Hours = append(d.Hours, h)
	}
	today := db.Today()
	for i := 0; i < 7; i++ {
		date := weekStart.AddDate(0, 0, i)
		day := calDay{Date: date, Label: date.Format("Mon 2"), IsToday: date.Equal(today)}
		for h := calStartHour; h < calEndHour; h++ {
			for m := 0; m < 60; m += calSlotMin {
				slot := calSlot{Time: time.Date(2000, 1, 1, h, m, 0, 0, time.UTC).Format("15:04"), OnHour: m == 0}
				if forID != 0 {
					slot.Href = "/admin/bookings/" + strconv.FormatInt(forID, 10) + "?start=" + date.Format("2006-01-02") + "T" + slot.Time
				}
				day.Slots = append(day.Slots, slot)
			}
		}
		for _, b := range bs {
			st := b.StartAt.In(db.Melbourne)
			if st.Year() != date.Year() || st.YearDay() != date.YearDay() {
				continue
			}
			startMin := (st.Hour()-calStartHour)*60 + st.Minute()
			top := startMin * calSlotPx / calSlotMin
			height := b.DurationMin * calSlotPx / calSlotMin
			if top < 0 {
				height += top
				top = 0
			}
			if height < calSlotPx {
				height = calSlotPx
			}
			day.Events = append(day.Events, calEvent{
				Row: newBookingRow(b), Top: top, Height: height,
				Href:  "/admin/bookings/" + strconv.FormatInt(b.ID, 10),
				Label: fmtClock(b.StartAt) + " – " + fmtClock(b.EndAt()),
			})
		}
		d.Days = append(d.Days, day)
	}
	render(w, r, "admin-calendar", d)
}
