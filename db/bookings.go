package db

import (
	"context"
	"time"

	"localithelp/db/sqlc"
)

// Booking statuses.
const (
	BookingNew       = "new"       // enquiry received
	BookingContacted = "contacted" // spoken to the customer
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
	Address         string // full street address, e.g. "12 Smith St, Donvale VIC 3111"
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
	ReminderSentAt  time.Time // UTC; zero until the day-before reminder went to the customer
	AdminAlertAt    time.Time // UTC; zero until the 1-hour heads-up went to the admin
	GCalEventID     string    // Google Calendar event id; empty until pushed
	GCalSyncedAt    time.Time // UTC; zero until the first successful push
	Source          string    // where it came from: google-ads, phone, referral… ("" on pre-migration rows)
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
		Address: r.Address, ServiceSlug: r.ServiceSlug, Mode: r.Mode, Issue: r.Issue, PreferredTime: r.PreferredTime,
		Status: r.Status, IP: r.Ip, StartAt: ParseStartAt(r.StartAt), DurationMin: int(r.DurationMin),
		AdminNotes: r.AdminNotes, ParentBookingID: r.ParentBookingID,
		CreatedAt: parseUTC(r.CreatedAt), UpdatedAt: parseUTC(r.UpdatedAt),
		ReminderSentAt: parseUTC(r.ReminderSentAt), AdminAlertAt: parseUTC(r.AdminAlertSentAt),
		GCalEventID: r.GcalEventID, GCalSyncedAt: parseUTC(r.GcalSyncedAt), Source: r.Source,
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
		Name: b.Name, Phone: b.Phone, Email: b.Email, Suburb: b.Suburb, Address: b.Address,
		ServiceSlug: b.ServiceSlug, Mode: b.Mode, Issue: b.Issue, PreferredTime: b.PreferredTime,
		Ip: b.IP, CustomerID: b.CustomerID, Source: b.Source, AdminNotes: b.AdminNotes,
	})
}

// CreateFollowup creates a new booking for the same customer as parent (e.g.
// returning a repaired machine) and returns its id. It starts unscheduled and
// inherits the parent's source — the work still traces back to that first click.
func CreateFollowup(parent *Booking, issue string) (int64, error) {
	return q.InsertFollowupBooking(context.Background(), sqlc.InsertFollowupBookingParams{
		Name: parent.Name, Phone: parent.Phone, Email: parent.Email, Suburb: parent.Suburb,
		Address: parent.Address, ServiceSlug: parent.ServiceSlug, Mode: parent.Mode, Issue: issue,
		CustomerID: parent.CustomerID, ParentBookingID: parent.ID, Source: parent.Source,
	})
}

// CreateForCustomer creates a new unscheduled booking for an existing
// customer (admin-initiated, e.g. a phone enquiry) and returns its id. An
// existing customer coming back is repeat business, not a fresh lead.
func CreateForCustomer(c *Customer, serviceSlug, issue string) (int64, error) {
	return InsertBooking(&Booking{
		CustomerID: c.ID, Name: c.Name, Phone: c.Phone, Email: c.Email, Suburb: c.Suburb,
		Address: c.Address, ServiceSlug: serviceSlug, Mode: "onsite", Issue: issue,
		Source: SourceRepeat,
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

// UpdateBookingIssue rewrites the customer's problem description. The admin
// edits it after the phone call, once the real fault is clear.
func UpdateBookingIssue(id int64, issue string) error {
	return q.UpdateBookingIssue(context.Background(), sqlc.UpdateBookingIssueParams{Issue: issue, ID: id})
}

// SetBookingCustomer links a booking to a customer record.
func SetBookingCustomer(id, customerID int64) error {
	return q.SetBookingCustomer(context.Background(), sqlc.SetBookingCustomerParams{CustomerID: customerID, ID: id})
}

// UpdateBookingAddress sets the booking's street address and suburb.
func UpdateBookingAddress(id int64, address, suburb string) error {
	return q.UpdateBookingAddress(context.Background(), sqlc.UpdateBookingAddressParams{Address: address, Suburb: suburb, ID: id})
}

// ── Scheduler support ──

// ListBookingsForReminder returns booked visits with from <= start < to whose
// customer has an email and has not yet received the day-before reminder.
func ListBookingsForReminder(from, to time.Time) ([]Booking, error) {
	return sqlcBookings(q.ListBookingsForReminder(context.Background(), sqlc.ListBookingsForReminderParams{
		StartAt: FormatStartAt(from), StartAt_2: FormatStartAt(to),
	}))
}

// ListBookingsForAdminAlert returns booked visits with from <= start < to that
// the admin has not yet been alerted about.
func ListBookingsForAdminAlert(from, to time.Time) ([]Booking, error) {
	return sqlcBookings(q.ListBookingsForAdminAlert(context.Background(), sqlc.ListBookingsForAdminAlertParams{
		StartAt: FormatStartAt(from), StartAt_2: FormatStartAt(to),
	}))
}

func MarkBookingReminderSent(id int64) error {
	return q.MarkBookingReminderSent(context.Background(), id)
}

func MarkBookingAdminAlertSent(id int64) error {
	return q.MarkBookingAdminAlertSent(context.Background(), id)
}

// ClaimSchedulerRun records that job ran on the given local date and reports
// whether this call was the first to do so. Once-a-day jobs call it before
// sending, so a restart or overlapping tick can never send twice.
func ClaimSchedulerRun(job string, on time.Time) (bool, error) {
	n, err := q.InsertSchedulerRun(context.Background(), sqlc.InsertSchedulerRunParams{Job: job, RanOn: FormatDate(on)})
	return n == 1, err
}

// ReleaseSchedulerRun drops a claim so the job can run again today. Jobs whose
// work happens after the claim (the backup upload) call it on failure so the
// next tick retries instead of waiting for tomorrow.
func ReleaseSchedulerRun(job string, on time.Time) error {
	return q.DeleteSchedulerRun(context.Background(), sqlc.DeleteSchedulerRunParams{Job: job, RanOn: FormatDate(on)})
}
