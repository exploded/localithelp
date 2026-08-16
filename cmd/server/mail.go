package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"os"

	"mchugh.com.au/db"
	"mchugh.com.au/mailer"
)

// ── Email notifications (Amazon SES) ──
//
// Configured by AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_REGION and
// MAIL_FROM. When the keys are absent the mailer is nil and every notify*
// call is a silent no-op, so local dev works without any AWS setup.

var (
	mail        *mailer.Mailer
	notifyEmail string // where booking / quote notifications go
)

func initMail() {
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "ap-southeast-2"
	}
	from := os.Getenv("MAIL_FROM")
	if from == "" {
		from = "James McHugh <james@mchugh.com.au>"
	}
	notifyEmail = os.Getenv("NOTIFY_EMAIL")
	if notifyEmail == "" {
		notifyEmail = site.Email
	}
	mail = mailer.New(region, os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"), from)
	if mail.Enabled() {
		log.Printf("email: SES enabled, from=%q notify=%q region=%s", from, notifyEmail, region)
	} else {
		log.Printf("email: not configured (set AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY) — notifications disabled")
	}
}

// send delivers an email in the background and logs failures. Callers must
// not depend on delivery; the request has already been persisted.
func send(to, subject, html, text, replyTo string) {
	if !mail.Enabled() || to == "" {
		return
	}
	go func() {
		if err := mail.Send(to, subject, html, text, replyTo); err != nil {
			log.Printf("email: %v", err)
		}
	}()
}

const mailTmplSrc = `
{{define "wrap"}}<!doctype html><html><body style="font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif;font-size:15px;line-height:1.5;color:#1c1c1c;margin:0;padding:24px;background:#f6f5f2">
<div style="max-width:560px;margin:0 auto;background:#fff;border:1px solid #e6e3dc;border-radius:8px;padding:28px">
{{.}}
<p style="margin-top:32px;font-size:12px;color:#777">James McHugh — local computer help, Donvale VIC · <a href="{{site.BaseURL}}" style="color:#777">{{site.BaseURL}}</a></p>
</div></body></html>{{end}}

{{define "row"}}<tr><td style="padding:6px 12px 6px 0;color:#666;white-space:nowrap;vertical-align:top">{{.K}}</td><td style="padding:6px 0;vertical-align:top">{{.V}}</td></tr>{{end}}

{{define "booking-admin"}}
<h2 style="margin:0 0 4px">New booking request #{{.ID}}</h2>
{{if .Suspicious}}<p style="background:#fff3cd;border:1px solid #ffe69c;padding:8px 12px;border-radius:6px"><strong>Flagged as suspicious</strong> — form timing looked automated. Marked as spam in admin; treat with care.</p>{{end}}
<table style="border-collapse:collapse;margin:12px 0 20px">
{{template "row" (kv "Name" .B.Name)}}
{{template "row" (kv "Phone" .B.Phone)}}
{{template "row" (kv "Email" .B.Email)}}
{{template "row" (kv "Suburb" .B.Suburb)}}
{{template "row" (kv "Service" .ServiceTitle)}}
{{template "row" (kv "Preferred time" .B.PreferredTime)}}
{{template "row" (kv "IP" .B.IP)}}
</table>
<p style="margin:0 0 6px;color:#666">The problem:</p>
<blockquote style="margin:0 0 20px;padding:12px 16px;border-left:3px solid #d9d5cc;background:#faf9f6;white-space:pre-wrap">{{.B.Issue}}</blockquote>
<p><a href="{{site.BaseURL}}/admin" style="display:inline-block;background:#1c1c1c;color:#fff;text-decoration:none;padding:10px 16px;border-radius:6px">Open admin</a></p>
{{end}}

{{define "booking-customer"}}
<h2 style="margin:0 0 12px">Thanks {{.B.Name}} — I've got your request.</h2>
<p>I usually reply within a couple of hours during the day to confirm a time and the expected cost. If it's urgent, just reply to this email{{if site.Phone}} or call {{site.Phone}}{{end}}.</p>
<p style="margin:20px 0 6px;color:#666">What you sent:</p>
<table style="border-collapse:collapse;margin:0 0 20px">
{{if .ServiceTitle}}{{template "row" (kv "Service" .ServiceTitle)}}{{end}}
{{if .B.Suburb}}{{template "row" (kv "Suburb" .B.Suburb)}}{{end}}
{{if .B.PreferredTime}}{{template "row" (kv "Preferred time" .B.PreferredTime)}}{{end}}
</table>
<blockquote style="margin:0 0 20px;padding:12px 16px;border-left:3px solid #d9d5cc;background:#faf9f6;white-space:pre-wrap">{{.B.Issue}}</blockquote>
<p style="color:#666">In the meantime: if the computer is on and something looks suspicious, disconnect it from the internet and leave it. Have any passwords you might need handy — I'll never ask you to read them out over the phone.</p>
<p>— James</p>
{{end}}

{{define "quote-paid"}}
<h2 style="margin:0 0 4px">Quote #{{.Q.ID}} paid — proposal due</h2>
<table style="border-collapse:collapse;margin:12px 0 20px">
{{template "row" (kv "Name" .Q.Name)}}
{{template "row" (kv "Email" .Q.Email)}}
{{template "row" (kv "Mobile" .Q.Mobile)}}
{{template "row" (kv "Address" .Q.Address)}}
{{template "row" (kv "Total" (printf "$%.2f" .Q.TotalCost))}}
</table>
<p style="margin:0 0 6px;color:#666">Description:</p>
<blockquote style="margin:0 0 20px;padding:12px 16px;border-left:3px solid #d9d5cc;background:#faf9f6;white-space:pre-wrap">{{.Q.Description}}</blockquote>
<p><a href="{{site.BaseURL}}/admin" style="display:inline-block;background:#1c1c1c;color:#fff;text-decoration:none;padding:10px 16px;border-radius:6px">Open admin</a></p>
{{end}}
`

var mailTmpl = template.Must(template.New("mail").Funcs(template.FuncMap{
	"site": func() *siteConfig { return &site },
	"kv":   func(k, v string) map[string]string { return map[string]string{"K": k, "V": v} },
}).Parse(mailTmplSrc))

// renderMail executes a named body template inside the "wrap" chrome.
func renderMail(name string, data any) (string, error) {
	var body bytes.Buffer
	if err := mailTmpl.ExecuteTemplate(&body, name, data); err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := mailTmpl.ExecuteTemplate(&out, "wrap", template.HTML(body.String())); err != nil {
		return "", err
	}
	return out.String(), nil
}

// notifyBooking emails the admin (always) and the customer (when they gave an
// address and the submission wasn't flagged) after a booking is stored.
func notifyBooking(id int64, b *db.Booking, suspicious bool) {
	if !mail.Enabled() {
		return
	}
	svcTitle := ""
	if s, ok := findService(b.ServiceSlug); ok {
		svcTitle = s.Title
	}
	data := map[string]any{"ID": id, "B": b, "ServiceTitle": svcTitle, "Suspicious": suspicious}

	// Admin notice — reply goes straight to the customer.
	subj := fmt.Sprintf("Booking #%d: %s", id, b.Name)
	if svcTitle != "" {
		subj += " — " + svcTitle
	}
	if suspicious {
		subj = "[suspicious] " + subj
	}
	html, err := renderMail("booking-admin", data)
	if err != nil {
		log.Printf("email: render booking-admin: %v", err)
		return
	}
	text := fmt.Sprintf("New booking request #%d\n\nName: %s\nPhone: %s\nEmail: %s\nSuburb: %s\nService: %s\nPreferred time: %s\n\n%s\n\n%s/admin\n",
		id, b.Name, b.Phone, b.Email, b.Suburb, svcTitle, b.PreferredTime, b.Issue, site.BaseURL)
	send(notifyEmail, subj, html, text, b.Email)

	// Customer confirmation
	if b.Email == "" || suspicious {
		return
	}
	html, err = renderMail("booking-customer", data)
	if err != nil {
		log.Printf("email: render booking-customer: %v", err)
		return
	}
	text = fmt.Sprintf("Thanks %s — I've got your request and will be in touch shortly (usually within a couple of hours during the day).\n\nWhat you sent:\n%s\n\n— James\n%s\n",
		b.Name, b.Issue, site.BaseURL)
	send(b.Email, "Got your booking request — James McHugh", html, text, site.Email)
}

// notifyQuotePaid emails the admin when a software-quote proposal fee is paid.
func notifyQuotePaid(q *db.Quote) {
	if !mail.Enabled() || q == nil {
		return
	}
	html, err := renderMail("quote-paid", map[string]any{"Q": q})
	if err != nil {
		log.Printf("email: render quote-paid: %v", err)
		return
	}
	text := fmt.Sprintf("Quote #%d paid by %s <%s> (%s) — $%.2f\n\n%s\n\n%s/admin\n",
		q.ID, q.Name, q.Email, q.Mobile, q.TotalCost, q.Description, site.BaseURL)
	send(notifyEmail, fmt.Sprintf("Quote #%d paid: %s", q.ID, q.Name), html, text, q.Email)
}
