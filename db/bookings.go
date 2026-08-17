package db

import (
	"context"
	"time"

	"mchugh.com.au/db/sqlc"
)

// Booking statuses.
const (
	BookingNew       = "new"       // enquiry received
	BookingContacted = "contacted" // spoken to the customer, not yet scheduled
	BookingBooked    = "booked"    // start_at set, confirmation sent
	BookingDone      = "done"      // visit complete, not yet invoiced
	BookingInvoiced  = "invoiced"  // invoice sent
	BookingPaid      = "paid"      // invoice paid
	BookingCancelled = "cancelled"
	BookingSpam      = "spam"
)

// BookingStatuses lists the statuses in lifecycle order (for filters/badges).
var BookingStatuses = []string{
	BookingNew, BookingContacted, BookingBooked, BookingDone, BookingInvoiced, BookingPaid, BookingCancelled, BookingSpam,
}

// Booking is an enquiry that becomes a scheduled visit. StartAt is zero until
// the admin schedules it; times are Melbourne local.
type Booking struct {
	ID              int64
	CustomerID      int64
	Name            string
	Phone           string
	Email           string
	Suburb          string
	ServiceSlug     string
	Mode            string // onsite | remote | either
	Issue           string
	PreferredTime   string
	Status          string
	IP              string
	StartAt         time.Time // zero = unscheduled
	DurationMin     int
	AdminNotes      string
	ParentBookingID int64 // set on follow-up visits
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// EndAt is StartAt + DurationMin (zero when unscheduled).
func (b Booking) EndAt() time.Time {
	if b.StartAt.IsZero() {
		return time.Time{}
	}
	return b.StartAt.Add(time.Duration(b.DurationMin) * time.Minute)
}

func sqlcBooking(r sqlc.Booking) Booking {
	return Booking{
		ID: r.ID, CustomerID: r.CustomerID, Name: r.Name, Phone: r.Phone, Email: r.Email, Suburb: r.Suburb,
		ServiceSlug: r.ServiceSlug, Mode: r.Mode, Issue: r.Issue, PreferredTime: r.PreferredTime,
		Status: r.Status, IP: r.Ip, StartAt: ParseStartAt(r.StartAt), DurationMin: int(r.DurationMin),
		AdminNotes: r.AdminNotes, ParentBookingID: r.ParentBookingID,
		CreatedAt: parseUTC(r.CreatedAt), UpdatedAt: parseUTC(r.UpdatedAt),
	}
}

func sqlcBookings(rows []sqlc.Booking, err error) ([]Booking, error) {
	if err != nil {
		return nil, err
	}
	out := make([]Booking, len(rows))
	for i, r := range rows {
		out[i] = sqlcBooking(r)
	}
	return out, nil
}

// InsertBooking stores a new enquiry and returns its id. b.CustomerID should
// already be set (see FindOrCreateCustomer).
func InsertBooking(b *Booking) (int64, error) {
	return q.InsertBooking(context.Background(), sqlc.InsertBookingParams{
		Name: b.Name, Phone: b.Phone, Email: b.Email, Suburb: b.Suburb, ServiceSlug: b.ServiceSlug,
		Mode: b.Mode, Issue: b.Issue, PreferredTime: b.PreferredTime, Ip: b.IP, CustomerID: b.CustomerID,
	})
}

// CreateFollowup creates a new booking for the same customer as parent (e.g.
// returning a repaired machine) and returns its id. It starts unscheduled.
func CreateFollowup(parent *Booking, issue string) (int64, error) {
	return q.InsertFollowupBooking(context.Background(), sqlc.InsertFollowupBookingParams{
		Name: parent.Name, Phone: parent.Phone, Email: parent.Email, Suburb: parent.Suburb,
		ServiceSlug: parent.ServiceSlug, Mode: parent.Mode, Issue: issue,
		CustomerID: parent.CustomerID, ParentBookingID: parent.ID,
	})
}

func GetBooking(id int64) (*Booking, error) {
	r, err := q.GetBooking(context.Background(), id)
	if err != nil {
		return nil, err
	}
	b := sqlcBooking(r)
	return &b, nil
}

func ListBookings() ([]Booking, error) {
	return sqlcBookings(q.ListBookings(context.Background()))
}

func ListBookingsByStatus(status string) ([]Booking, error) {
	return sqlcBookings(q.ListBookingsByStatus(context.Background(), status))
}

// ListBookingsBetween returns scheduled, non-cancelled bookings with
// from <= start < to (Melbourne local).
func ListBookingsBetween(from, to time.Time) ([]Booking, error) {
	return sqlcBookings(q.ListBookingsBetween(context.Background(), sqlc.ListBookingsBetweenParams{
		StartAt: FormatStartAt(from), StartAt_2: FormatStartAt(to),
	}))
}

func ListBookingsByCustomer(customerID int64) ([]Booking, error) {
	return sqlcBookings(q.ListBookingsByCustomer(context.Background(), customerID))
}

func ListChildBookings(parentID int64) ([]Booking, error) {
	return sqlcBookings(q.ListChildBookings(context.Background(), parentID))
}

// CountBookingsByStatus returns status → count.
func CountBookingsByStatus() (map[string]int, error) {
	rows, err := q.CountBookingsByStatus(context.Background())
	if err != nil {
		return nil, err
	}
	m := make(map[string]int, len(rows))
	for _, r := range rows {
		m[r.Status] = int(r.N)
	}
	return m, nil
}

func UpdateBookingStatus(id int64, status string) error {
	return q.UpdateBookingStatus(context.Background(), sqlc.UpdateBookingStatusParams{Status: status, ID: id})
}

// ScheduleBooking sets the visit time and marks the booking booked.
func ScheduleBooking(id int64, start time.Time, durationMin int) error {
	return q.UpdateBookingSchedule(context.Background(), sqlc.UpdateBookingScheduleParams{
		StartAt: FormatStartAt(start), DurationMin: int64(durationMin), ID: id,
	})
}

func UpdateBookingNotes(id int64, notes string) error {
	return q.UpdateBookingNotes(context.Background(), sqlc.UpdateBookingNotesParams{AdminNotes: notes, ID: id})
}
