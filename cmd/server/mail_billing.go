package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"localithelp/db"
	"localithelp/mailer"
)

// ── Booking lifecycle + invoice emails ──
//
// Templates live in mailBillingTmplSrc (parsed into mailTmpl alongside
// mailTmplSrc). All customer-facing sends here are synchronous (sendNow) so the
// admin sees failures and state only advances after delivery is accepted.

const mailBillingTmplSrc = `
{{define "btn"}}<a href="{{.Href}}" style="display:inline-block;background:#1c1c1c;color:#fff;text-decoration:none;padding:12px 20px;border-radius:6px;font-weight:600">{{.Label}}</a>{{end}}

{{define "booking-confirm"}}
<h2 style="margin:0 0 12px">{{if .Rescheduled}}Your visit has been rescheduled{{else}}Your visit is booked{{end}}{{if .B.Name}}, {{.B.Name}}{{end}}</h2>
<p>{{if .Rescheduled}}Here are the new details.{{else}}Thanks — I've booked you in.{{end}} Please reply to this email{{if site.Phone}} or call {{site.Phone}}{{end}} if anything changes.</p>
<table style="border-collapse:collapse;margin:12px 0 20px">
{{template "row" (kv "When" .When)}}
{{template "row" (kv "Duration" .Duration)}}
{{if .ServiceTitle}}{{template "row" (kv "Service" .ServiceTitle)}}{{end}}
{{if .Where}}{{template "row" (kv "Where" .Where)}}{{end}}
</table>
{{if .Followup}}<p>This is a follow-up visit{{if .Issue}}: {{.Issue}}{{end}}.</p>{{else if .Issue}}<p style="margin:0 0 6px;color:#666">What you asked about:</p>
<blockquote style="margin:0 0 20px;padding:12px 16px;border-left:3px solid #d9d5cc;background:#faf9f6;white-space:pre-wrap">{{.Issue}}</blockquote>{{end}}
<p style="color:#666">Before I arrive: leave the computer plugged in and switched on if you can, and have any passwords you might need handy (I'll never ask you to read them out over the phone). A calendar invite is attached.</p>
<p>— James</p>
{{end}}

{{define "booking-cancel"}}
<h2 style="margin:0 0 12px">Your visit has been cancelled{{if .B.Name}}, {{.B.Name}}{{end}}</h2>
<p>The visit{{if .When}} on {{.When}}{{end}} has been cancelled{{if .Reason}}: {{.Reason}}{{end}}.</p>
<p>If you'd like to rebook, just reply to this email{{if site.Phone}} or call {{site.Phone}}{{end}}, or book online at <a href="{{site.BaseURL}}/book" style="color:#1c1c1c">{{site.BaseURL}}/book</a>.</p>
<p>— James</p>
{{end}}

{{define "invoice-send"}}
<h2 style="margin:0 0 12px">Invoice {{.Ref}}{{if .Cust.Name}} for {{.Cust.Name}}{{end}}</h2>
<p>Thanks for having me{{if .When}} on {{.When}}{{end}}. Here's the invoice — the PDF is attached and you can also view it online.</p>
<table style="border-collapse:collapse;margin:12px 0 20px">
{{template "row" (kv "Amount due" (money .Inv.TotalCents))}}
{{template "row" (kv "Due" .Due)}}
{{template "row" (kv "Reference" .Ref)}}
</table>
{{if .Inv.PaymentLink}}<p style="margin:0 0 6px"><strong>Pay by card online</strong> (Visa, Mastercard, Amex, Apple Pay, Google Pay):</p>
<p style="margin:0 0 20px">{{template "btn" (btn .Inv.PaymentLink (printf "Pay %s now" (money .Inv.TotalCents)))}}</p>{{end}}
{{if site.BankBSB}}<p style="margin:0 0 6px"><strong>Or pay by bank transfer</strong> using {{.Ref}} as the reference:</p>
<table style="border-collapse:collapse;margin:0 0 20px">
{{template "row" (kv "Account name" site.BankName)}}
{{template "row" (kv "BSB" site.BankBSB)}}
{{template "row" (kv "Account" site.BankAcct)}}
{{template "row" (kv "Reference" .Ref)}}
</table>{{end}}
<p style="color:#666;font-size:13px">View or download the invoice any time: <a href="{{.Link}}" style="color:#666;word-break:break-all">{{.Link}}</a></p>
<p>Questions? Just reply to this email{{if site.Phone}} or call {{site.Phone}}{{end}}. Thanks again.</p>
<p>— James</p>
{{end}}

{{define "invoice-receipt"}}
<h2 style="margin:0 0 12px">Receipt for {{.Ref}} — thank you{{if .Cust.Name}}, {{.Cust.Name}}{{end}}</h2>
<p>Payment received — this invoice is now paid in full. The receipt is attached as a PDF.</p>
<table style="border-collapse:collapse;margin:12px 0 20px">
{{template "row" (kv "Amount paid" (money .Inv.TotalCents))}}
{{template "row" (kv "Paid on" .Paid)}}
{{template "row" (kv "Method" .Method)}}
{{if .Inv.PaymentRef}}{{template "row" (kv "Payment ref" .Inv.PaymentRef)}}{{end}}
</table>
<p style="color:#666;font-size:13px">View or download it any time: <a href="{{.Link}}" style="color:#666;word-break:break-all">{{.Link}}</a></p>
<p>If you ever need a hand again, book at <a href="{{site.BaseURL}}/book" style="color:#1c1c1c">{{site.BaseURL}}/book</a> or just reply to this email.</p>
<p>— James</p>
{{end}}
`

// bookingMailData is the view for booking-confirm / booking-cancel.
type bookingMailData struct {
	B            *db.Booking
	When         string
	Duration     string
	ServiceTitle string
	Where        string
	Issue        string
	Rescheduled  bool
	Followup     bool
	Reason       string
}

func newBookingMailData(b *db.Booking) bookingMailData {
	d := bookingMailData{
		B: b, When: fmtWhenLong(b.StartAt), Duration: fmtDuration(b.DurationMin), Issue: b.Issue,
		Followup: b.ParentBookingID != 0,
	}
	if s, ok := findService(b.ServiceSlug); ok {
		d.ServiceTitle = s.Title
	}
	if b.Suburb != "" {
		d.Where = "Your place in " + b.Suburb
	}
	return d
}

// sendBookingConfirmation emails the customer their (re)scheduled visit with
// an .ics invite attached. Returns an error if there is no email or SES fails.
func sendBookingConfirmation(b *db.Booking, rescheduled bool) error {
	if b.Email == "" {
		return fmt.Errorf("booking has no email address")
	}
	d := newBookingMailData(b)
	d.Rescheduled = rescheduled
	html, err := renderMail("booking-confirm", d)
	if err != nil {
		return fmt.Errorf("render booking-confirm: %w", err)
	}
	subj := "Your visit is booked — " + fmtWhen(b.StartAt)
	if rescheduled {
		subj = "Your visit has been rescheduled — " + fmtWhen(b.StartAt)
	}
	text := fmt.Sprintf("Hi %s — %s\n\nWhen: %s\nDuration: %s\n%s\nReply to this email if anything changes.\n\n— James\n%s\n",
		b.Name, strings.TrimSuffix(subj, " — "+fmtWhen(b.StartAt)), d.When, d.Duration, d.Where, site.BaseURL)
	ics := mailer.Attachment{Filename: "visit.ics", ContentType: "text/calendar", Data: []byte(bookingICS(b, d.ServiceTitle))}
	return sendNow(b.Email, subj, html, text, site.Email, ics)
}

// sendBookingCancellation emails the customer that the visit is off.
func sendBookingCancellation(b *db.Booking, reason string) error {
	if b.Email == "" {
		return fmt.Errorf("booking has no email address")
	}
	d := newBookingMailData(b)
	d.Reason = reason
	html, err := renderMail("booking-cancel", d)
	if err != nil {
		return fmt.Errorf("render booking-cancel: %w", err)
	}
	text := fmt.Sprintf("Hi %s — your visit%s has been cancelled%s.\n\nTo rebook, reply to this email or visit %s/book.\n\n— James\n",
		b.Name, map[bool]string{true: " on " + d.When, false: ""}[d.When != ""], map[bool]string{true: ": " + reason, false: ""}[reason != ""], site.BaseURL)
	return sendNow(b.Email, "Your visit has been cancelled — Local IT Help", html, text, site.Email)
}

// bookingICS builds a minimal VCALENDAR invite for the visit (UTC times).
func bookingICS(b *db.Booking, serviceTitle string) string {
	const layout = "20060102T150405Z"
	start := b.StartAt.UTC()
	end := b.EndAt().UTC()
	summary := "Local IT Help — computer help"
	if serviceTitle != "" {
		summary = "Local IT Help — " + serviceTitle
	}
	desc := strings.ReplaceAll(strings.ReplaceAll(b.Issue, "\r", ""), "\n", "\\n")
	loc := ""
	if b.Suburb != "" {
		loc = "LOCATION:" + icsEscape(b.Suburb) + "\r\n"
	}
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//localithelp.com.au//bookings//EN\r\nMETHOD:PUBLISH\r\n" +
		"BEGIN:VEVENT\r\n" +
		fmt.Sprintf("UID:booking-%d@localithelp.com.au\r\n", b.ID) +
		"DTSTAMP:" + time.Now().UTC().Format(layout) + "\r\n" +
		"DTSTART:" + start.Format(layout) + "\r\n" +
		"DTEND:" + end.Format(layout) + "\r\n" +
		"SUMMARY:" + icsEscape(summary) + "\r\n" +
		loc +
		"DESCRIPTION:" + icsEscape(desc) + "\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
}

func icsEscape(s string) string {
	r := strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,")
	return r.Replace(s)
}

// invoiceMailData is the view for invoice-send / invoice-receipt.
type invoiceMailData struct {
	Inv    *db.Invoice
	Cust   *db.Customer
	Ref    string
	When   string // visit date, if linked to a booking
	Due    string
	Paid   string
	Method string
	Link   string
}

func newInvoiceMailData(v *invoiceView) invoiceMailData {
	d := invoiceMailData{
		Inv: v.Inv, Cust: v.Cust, Ref: v.Inv.Ref(), Due: fmtDate(v.Inv.DueAt), Paid: fmtDate(v.Inv.PaidAt),
		Method: db.PaymentMethodLabel(v.Inv.PaymentMethod), Link: v.PublicURL,
	}
	if v.Booking != nil && !v.Booking.StartAt.IsZero() {
		d.When = fmtDate(v.Booking.StartAt)
	}
	if d.Due == "" {
		d.Due = "on receipt"
	}
	return d
}

// sendInvoiceEmail emails the invoice with the PDF attached.
func sendInvoiceEmail(v *invoiceView, pdf []byte) error {
	if v.Cust == nil || v.Cust.Email == "" {
		return fmt.Errorf("customer has no email address")
	}
	d := newInvoiceMailData(v)
	html, err := renderMail("invoice-send", d)
	if err != nil {
		return fmt.Errorf("render invoice-send: %w", err)
	}
	text := fmt.Sprintf("Invoice %s from Local IT Help\n\nAmount due: %s\nDue: %s\nReference: %s\n", d.Ref, fmtCents(v.Inv.TotalCents), d.Due, d.Ref)
	if v.Inv.PaymentLink != "" {
		text += "\nPay by card online: " + v.Inv.PaymentLink + "\n"
	}
	if site.BankBSB != "" {
		text += fmt.Sprintf("\nBank transfer: %s, BSB %s, Account %s, Reference %s\n", site.BankName, site.BankBSB, site.BankAcct, d.Ref)
	}
	text += "\nView online: " + d.Link + "\n\n— James\n"
	att := mailer.Attachment{Filename: d.Ref + ".pdf", ContentType: "application/pdf", Data: pdf}
	return sendNow(v.Cust.Email, fmt.Sprintf("Invoice %s from Local IT Help — %s", d.Ref, fmtCents(v.Inv.TotalCents)), html, text, site.Email, att)
}

// sendReceiptEmail emails the paid invoice (receipt) with the PDF attached.
func sendReceiptEmail(v *invoiceView, pdf []byte) error {
	if v.Cust == nil || v.Cust.Email == "" {
		return fmt.Errorf("customer has no email address")
	}
	d := newInvoiceMailData(v)
	html, err := renderMail("invoice-receipt", d)
	if err != nil {
		return fmt.Errorf("render invoice-receipt: %w", err)
	}
	text := fmt.Sprintf("Receipt for %s — thank you.\n\nAmount paid: %s\nPaid on: %s\nMethod: %s\n\nView online: %s\n\n— James\n",
		d.Ref, fmtCents(v.Inv.TotalCents), d.Paid, d.Method, d.Link)
	att := mailer.Attachment{Filename: d.Ref + "-receipt.pdf", ContentType: "application/pdf", Data: pdf}
	return sendNow(v.Cust.Email, fmt.Sprintf("Receipt for %s — Local IT Help", d.Ref), html, text, site.Email, att)
}

// logMailErr is a tiny helper for handlers that surface mail failures.
func logMailErr(what string, err error) {
	if err != nil {
		log.Printf("email: %s: %v", what, err)
	}
}
