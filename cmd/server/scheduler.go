package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"localithelp/db"
)

// ── Background scheduler ──
//
// One goroutine ticks every 15 minutes and runs the jobs below. Every job is
// idempotent: it stamps a column (or a scheduler_runs row) once the send has
// been accepted, so a restart or an overlapping tick can never send twice, and
// a failed send is simply retried on the next tick. All times are Melbourne
// local; the whole thing is off unless PROD or SCHEDULER=1 is set.

const schedulerInterval = 15 * time.Minute

// schedulerEnabled reports whether the ticker should run for this process.
func schedulerEnabled() bool {
	switch strings.ToLower(os.Getenv("SCHEDULER")) {
	case "0", "false", "off":
		return false
	case "1", "true", "on":
		return true
	}
	return os.Getenv("PROD") != ""
}

func startScheduler() {
	if !schedulerEnabled() {
		log.Println("scheduler: disabled (set PROD or SCHEDULER=1 to enable)")
		return
	}
	log.Printf("scheduler: enabled, every %s", schedulerInterval)
	go func() {
		runScheduledJobs(time.Now())
		for range time.Tick(schedulerInterval) {
			runScheduledJobs(time.Now())
		}
	}()
}

// schedulerSummary counts what one tick sent, for logs and tests.
type schedulerSummary struct {
	Reminders int // day-before customer reminders
	Alerts    int // 1-hour admin heads-ups
	Digest    bool
	Backup    bool // nightly DB snapshot uploaded
}

// runScheduledJobs runs every job once for the given instant. Errors are
// logged, never fatal — the next tick retries anything that did not stamp.
func runScheduledJobs(now time.Time) schedulerSummary {
	now = now.In(db.Melbourne)
	var s schedulerSummary
	s.Reminders = sendDayBeforeReminders(now)
	s.Alerts = sendAdminAlerts(now)
	s.Digest = sendMorningDigest(now)
	s.Backup = backupDatabase(now)
	if s.Reminders+s.Alerts > 0 || s.Digest {
		log.Printf("scheduler: sent %d reminder(s), %d heads-up(s), digest=%v", s.Reminders, s.Alerts, s.Digest)
	}
	return s
}

// sendDayBeforeReminders emails customers between 4 pm and 8 pm about
// tomorrow's visits. Visits booked for today (or after the window has passed)
// get no reminder — the confirmation email already covered a same-day booking.
func sendDayBeforeReminders(now time.Time) int {
	if h := now.Hour(); h < 16 || h >= 20 {
		return 0
	}
	from := midnight(now).AddDate(0, 0, 1)
	bs, err := db.ListBookingsForReminder(from, from.AddDate(0, 0, 1))
	if err != nil {
		log.Printf("scheduler: reminders: %v", err)
		return 0
	}
	n := 0
	for i := range bs {
		b := &bs[i]
		if err := sendBookingReminder(b); err != nil {
			log.Printf("scheduler: reminder for booking #%d: %v", b.ID, err)
			continue
		}
		if err := db.MarkBookingReminderSent(b.ID); err != nil {
			log.Printf("scheduler: stamp reminder #%d: %v", b.ID, err)
			continue
		}
		n++
	}
	return n
}

// sendAdminAlerts emails the admin about visits starting within the next hour.
// With a 15-minute tick the heads-up lands 45–60 minutes before the visit.
func sendAdminAlerts(now time.Time) int {
	bs, err := db.ListBookingsForAdminAlert(now, now.Add(time.Hour))
	if err != nil {
		log.Printf("scheduler: alerts: %v", err)
		return 0
	}
	n := 0
	for i := range bs {
		b := &bs[i]
		if err := sendBookingSoon(b, now); err != nil {
			log.Printf("scheduler: heads-up for booking #%d: %v", b.ID, err)
			continue
		}
		if err := db.MarkBookingAdminAlertSent(b.ID); err != nil {
			log.Printf("scheduler: stamp heads-up #%d: %v", b.ID, err)
			continue
		}
		n++
	}
	return n
}

// digestHour:digestMinute is the earliest local time the morning digest goes.
const (
	digestHour   = 7
	digestMinute = 30
)

// sendMorningDigest emails the admin once a day, on the first tick at or after
// 07:30, and only when there is something to report. The scheduler_runs row is
// claimed before sending so a restart mid-send cannot repeat it.
func sendMorningDigest(now time.Time) bool {
	if now.Hour()*60+now.Minute() < digestHour*60+digestMinute {
		return false
	}
	d, err := buildDigest(now)
	if err != nil {
		log.Printf("scheduler: digest: %v", err)
		return false
	}
	first, err := db.ClaimSchedulerRun("digest", now)
	if err != nil {
		log.Printf("scheduler: claim digest: %v", err)
		return false
	}
	if !first || d.Empty() {
		return false
	}
	if err := sendDigest(d); err != nil {
		log.Printf("scheduler: digest send: %v", err)
		return false
	}
	return true
}

// midnight returns 00:00 on the same Melbourne date as t.
func midnight(t time.Time) time.Time {
	t = t.In(db.Melbourne)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, db.Melbourne)
}

// ── Digest data ──

// digestInvoice is an overdue invoice decorated for the digest.
type digestInvoice struct {
	db.Invoice
	Ref      string
	Customer string
	Due      string
	DaysLate int
}

// digestData is the view for the morning digest email.
type digestData struct {
	Date    string
	Today   []bookingRow
	New     []bookingRow
	Overdue []digestInvoice
	Quotes  []db.Quote // verified in the last 7 days
}

func (d digestData) Empty() bool {
	return len(d.Today) == 0 && len(d.New) == 0 && len(d.Overdue) == 0 && len(d.Quotes) == 0
}

func buildDigest(now time.Time) (digestData, error) {
	day := midnight(now)
	d := digestData{Date: day.Format("Monday 2 January")}

	today, err := db.ListBookingsBetween(day, day.AddDate(0, 0, 1))
	if err != nil {
		return d, fmt.Errorf("today's bookings: %w", err)
	}
	d.Today = bookingRows(today)

	fresh, err := db.ListBookingsByStatus(db.BookingNew)
	if err != nil {
		return d, fmt.Errorf("new bookings: %w", err)
	}
	d.New = bookingRows(fresh)

	overdue, err := db.ListOverdueInvoices(day)
	if err != nil {
		return d, fmt.Errorf("overdue invoices: %w", err)
	}
	for _, inv := range overdue {
		di := digestInvoice{Invoice: inv, Ref: inv.Ref(), Due: fmtDate(inv.DueAt),
			DaysLate: int(day.Sub(inv.DueAt).Hours() / 24)}
		if c, err := db.GetCustomer(inv.CustomerID); err == nil {
			di.Customer = c.Name
		}
		d.Overdue = append(d.Overdue, di)
	}

	quotes, err := db.ListQuotes()
	if err != nil {
		return d, fmt.Errorf("quotes: %w", err)
	}
	cutoff := now.AddDate(0, 0, -7)
	for _, q := range quotes {
		if q.Status == "verified" && q.VerifiedAt.After(cutoff) {
			d.Quotes = append(d.Quotes, q)
		}
	}
	return d, nil
}

// ── Mail ──

const mailSchedulerTmplSrc = `
{{define "booking-reminder"}}
<h2 style="margin:0 0 12px">See you tomorrow{{if .B.Name}}, {{.B.Name}}{{end}}</h2>
<p>Just a reminder that I'm booked in to see you tomorrow.</p>
<table style="border-collapse:collapse;margin:12px 0 20px">
{{template "row" (kv "When" .When)}}
{{template "row" (kv "Duration" .Duration)}}
{{if .ServiceTitle}}{{template "row" (kv "Service" .ServiceTitle)}}{{end}}
{{if .Where}}{{template "row" (kv "Where" .Where)}}{{end}}
</table>
<p style="color:#666">Before I arrive: leave the computer plugged in and switched on if you can, and have any passwords you might need handy (I'll never ask you to read them out over the phone).</p>
<p>If the time no longer suits, reply to this email{{if site.Phone}} or call {{site.Phone}}{{end}} and we'll sort out another one.</p>
<p>— James</p>
{{end}}

{{define "booking-soon"}}
<h2 style="margin:0 0 4px">In {{.In}}: {{.B.Name}}{{if .ServiceTitle}} — {{.ServiceTitle}}{{end}}</h2>
<table style="border-collapse:collapse;margin:12px 0 20px">
{{template "row" (kv "When" .When)}}
{{template "row" (kv "Duration" .Duration)}}
{{template "row" (kv "Phone" .B.Phone)}}
{{if .B.Address}}{{template "row" (kv "Address" .B.Address)}}{{else if .B.Suburb}}{{template "row" (kv "Suburb" .B.Suburb)}}{{end}}
</table>
{{if .Issue}}<p style="margin:0 0 6px;color:#666">The problem:</p>
<blockquote style="margin:0 0 20px;padding:12px 16px;border-left:3px solid #d9d5cc;background:#faf9f6;white-space:pre-wrap">{{.Issue}}</blockquote>{{end}}
{{if .Notes}}<p style="margin:0 0 6px;color:#666">Your notes:</p>
<blockquote style="margin:0 0 20px;padding:12px 16px;border-left:3px solid #d9d5cc;background:#faf9f6;white-space:pre-wrap">{{.Notes}}</blockquote>{{end}}
<p>{{if .MapURL}}<a href="{{.MapURL}}" style="display:inline-block;background:#1c1c1c;color:#fff;text-decoration:none;padding:10px 16px;border-radius:6px">Directions</a> &nbsp; {{end}}<a href="{{site.BaseURL}}/admin/bookings/{{.B.ID}}" style="color:#666">Open booking</a></p>
{{end}}

{{define "digest"}}
<h2 style="margin:0 0 16px">{{.Date}}</h2>
{{if .Today}}<h3 style="margin:0 0 8px;font-size:15px">Today's visits</h3>
<table style="border-collapse:collapse;margin:0 0 20px">
{{range .Today}}<tr><td style="padding:6px 12px 6px 0;color:#666;white-space:nowrap;vertical-align:top">{{.When}}</td><td style="padding:6px 0;vertical-align:top"><a href="{{site.BaseURL}}/admin/bookings/{{.ID}}" style="color:#1c1c1c">{{.Name}}</a>{{if .ServiceTitle}} — {{.ServiceTitle}}{{end}}<br><span style="color:#666">{{if .Address}}{{.Address}}{{else}}{{.Suburb}}{{end}}{{if .Phone}} · {{.Phone}}{{end}}</span></td></tr>
{{end}}</table>{{end}}
{{if .New}}<h3 style="margin:0 0 8px;font-size:15px">New enquiries to reply to ({{len .New}})</h3>
<table style="border-collapse:collapse;margin:0 0 20px">
{{range .New}}<tr><td style="padding:6px 0;vertical-align:top"><a href="{{site.BaseURL}}/admin/bookings/{{.ID}}" style="color:#1c1c1c">#{{.ID}} {{.Name}}</a>{{if .ServiceTitle}} — {{.ServiceTitle}}{{end}}{{if .Suburb}} ({{.Suburb}}){{end}}<br><span style="color:#666">{{.Preview}}</span></td></tr>
{{end}}</table>{{end}}
{{if .Overdue}}<h3 style="margin:0 0 8px;font-size:15px">Overdue invoices ({{len .Overdue}})</h3>
<table style="border-collapse:collapse;margin:0 0 20px">
{{range .Overdue}}<tr><td style="padding:6px 12px 6px 0;vertical-align:top"><a href="{{site.BaseURL}}/admin/invoices/{{.ID}}" style="color:#1c1c1c">{{.Ref}}</a></td><td style="padding:6px 12px 6px 0;vertical-align:top">{{.Customer}}</td><td style="padding:6px 12px 6px 0;vertical-align:top;white-space:nowrap">{{money .TotalCents}}</td><td style="padding:6px 0;color:#666;vertical-align:top;white-space:nowrap">due {{.Due}} ({{.DaysLate}}d)</td></tr>
{{end}}</table>{{end}}
{{if .Quotes}}<h3 style="margin:0 0 8px;font-size:15px">Software quotes verified this week ({{len .Quotes}})</h3>
<table style="border-collapse:collapse;margin:0 0 20px">
{{range .Quotes}}<tr><td style="padding:6px 0;vertical-align:top">#{{.ID}} {{.Name}} &lt;{{.Email}}&gt; — {{printf "$%.0f" .TotalCost}}</td></tr>
{{end}}</table>{{end}}
<p><a href="{{site.BaseURL}}/admin" style="display:inline-block;background:#1c1c1c;color:#fff;text-decoration:none;padding:10px 16px;border-radius:6px">Open admin</a></p>
{{end}}

{{define "backup-failed"}}
<h2 style="margin:0 0 12px">Nightly database backup failed</h2>
<table style="border-collapse:collapse;margin:12px 0 20px">
{{template "row" (kv "Object" (printf "s3://%s/%s" .Bucket .Key))}}
{{template "row" (kv "Error" .Error)}}
</table>
<p style="color:#666">The scheduler retries every 15 minutes and will email again if the next attempt fails too. Check the service log with <code>journalctl -u localithelp</code>.</p>
{{end}}
`

// sendBookingReminder emails the customer the day-before reminder.
func sendBookingReminder(b *db.Booking) error {
	if b.Email == "" {
		return fmt.Errorf("booking has no email address")
	}
	d := newBookingMailData(b)
	html, err := renderMail("booking-reminder", d)
	if err != nil {
		return fmt.Errorf("render booking-reminder: %w", err)
	}
	text := fmt.Sprintf("Hi %s — a reminder that I'm booked in to see you tomorrow.\n\nWhen: %s\nDuration: %s\n%s\n\nIf the time no longer suits, reply to this email and we'll sort out another one.\n\n— James\n%s\n",
		b.Name, d.When, d.Duration, d.Where, site.BaseURL)
	return sendNow(b.Email, "Reminder: your visit tomorrow — "+fmtWhen(b.StartAt), html, text, site.Email)
}

// bookingSoonData is the view for the admin heads-up.
type bookingSoonData struct {
	bookingMailData
	In     string // "45 min"
	Notes  string
	MapURL string
}

// sendBookingSoon emails the admin the 1-hour heads-up for a visit.
func sendBookingSoon(b *db.Booking, now time.Time) error {
	d := bookingSoonData{bookingMailData: newBookingMailData(b), Notes: b.AdminNotes}
	d.In = fmtDuration(int(b.StartAt.Sub(now).Round(time.Minute).Minutes()))
	if addr := strings.TrimSpace(b.Address); addr != "" {
		d.MapURL = "https://www.google.com/maps/dir/?api=1&destination=" + strings.ReplaceAll(addr, " ", "+")
	}
	html, err := renderMail("booking-soon", d)
	if err != nil {
		return fmt.Errorf("render booking-soon: %w", err)
	}
	where := b.Address
	if where == "" {
		where = b.Suburb
	}
	svc := ""
	if d.ServiceTitle != "" {
		svc = " — " + d.ServiceTitle
	}
	text := fmt.Sprintf("In %s: %s%s\n\nWhen: %s (%s)\nPhone: %s\nWhere: %s\n\n%s\n\n%s\n%s/admin/bookings/%d\n",
		d.In, b.Name, svc, d.When, d.Duration, b.Phone, where, b.Issue, b.AdminNotes, site.BaseURL, b.ID)
	subj := fmt.Sprintf("In %s: %s, %s%s", d.In, b.Name, fmtClock(b.StartAt), svc)
	return sendNow(notifyEmail, subj, html, text, b.Email)
}

// sendDigest emails the admin the morning digest.
func sendDigest(d digestData) error {
	html, err := renderMail("digest", d)
	if err != nil {
		return fmt.Errorf("render digest: %w", err)
	}
	var t strings.Builder
	fmt.Fprintf(&t, "%s\n\n", d.Date)
	if len(d.Today) > 0 {
		t.WriteString("Today's visits:\n")
		for _, b := range d.Today {
			fmt.Fprintf(&t, "  %s  %s — %s (%s)\n", b.When, b.Name, b.ServiceTitle, b.Suburb)
		}
		t.WriteString("\n")
	}
	if len(d.New) > 0 {
		fmt.Fprintf(&t, "New enquiries: %d\n", len(d.New))
		for _, b := range d.New {
			fmt.Fprintf(&t, "  #%d %s — %s\n", b.ID, b.Name, b.Preview)
		}
		t.WriteString("\n")
	}
	if len(d.Overdue) > 0 {
		fmt.Fprintf(&t, "Overdue invoices: %d\n", len(d.Overdue))
		for _, v := range d.Overdue {
			fmt.Fprintf(&t, "  %s %s %s due %s (%dd)\n", v.Ref, v.Customer, fmtCents(v.TotalCents), v.Due, v.DaysLate)
		}
		t.WriteString("\n")
	}
	if len(d.Quotes) > 0 {
		fmt.Fprintf(&t, "Quotes verified this week: %d\n", len(d.Quotes))
		for _, q := range d.Quotes {
			fmt.Fprintf(&t, "  #%d %s <%s> $%.0f\n", q.ID, q.Name, q.Email, q.TotalCost)
		}
		t.WriteString("\n")
	}
	fmt.Fprintf(&t, "%s/admin\n", site.BaseURL)
	subj := fmt.Sprintf("Today: %d visit(s)", len(d.Today))
	if n := len(d.New); n > 0 {
		subj += fmt.Sprintf(", %d new", n)
	}
	if n := len(d.Overdue); n > 0 {
		subj += fmt.Sprintf(", %d overdue", n)
	}
	return sendNow(notifyEmail, subj, html, t.String(), "")
}
