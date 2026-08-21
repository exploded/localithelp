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
// Configured by AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_REGION; the
// sender and notification address are CONTACT_EMAIL. When the keys are absent the mailer is nil and every notify*
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
	// Sender and notification target both derive from CONTACT_EMAIL; the
	// address must be a verified SES identity.
	from := "Local IT Help <" + site.Email + ">"
	notifyEmail = site.Email
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

// sendNow delivers synchronously (with optional attachments) and returns the
// error, for admin actions where the outcome must be shown and a state change
// should only happen on success. When SES is not configured it logs and
// returns nil so local flows still complete.
func sendNow(to, subject, html, text, replyTo string, atts ...mailer.Attachment) error {
	if to == "" {
		return fmt.Errorf("no recipient address")
	}
	if !mail.Enabled() {
		log.Printf("email: (disabled) would send %q to %s with %d attachment(s)", subject, to, len(atts))
		return nil
	}
	return mail.SendWithAttachments(to, subject, html, text, replyTo, atts...)
}

const mailTmplSrc = `
{{define "wrap"}}<!doctype html><html><body style="font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif;font-size:15px;line-height:1.5;color:#1c1c1c;margin:0;padding:24px;background:#f6f5f2">
<div style="max-width:560px;margin:0 auto;background:#fff;border:1px solid #e6e3dc;border-radius:8px;padding:28px">
{{.}}
<p style="margin-top:32px;font-size:12px;color:#777">Local IT Help — James McHugh, Donvale VIC · <a href="{{site.BaseURL}}" style="color:#777">{{site.BaseURL}}</a></p>
</div></body></html>{{end}}

{{define "row"}}<tr><td style="padding:6px 12px 6px 0;color:#666;white-space:nowrap;vertical-align:top">{{.K}}</td><td style="padding:6px 0;vertical-align:top">{{.V}}</td></tr>{{end}}

{{define "booking-admin"}}
<h2 style="margin:0 0 4px">New booking request #{{.ID}}</h2>
{{if .Suspicious}}<p style="background:#fff3cd;border:1px solid #ffe69c;padding:8px 12px;border-radius:6px"><strong>Flagged as suspicious</strong> — form timing looked automated. Marked as spam in admin; treat with care.</p>{{end}}
<table style="border-collapse:collapse;margin:12px 0 20px">
{{template "row" (kv "Name" .B.Name)}}
{{template "row" (kv "Phone" .B.Phone)}}
{{template "row" (kv "Email" .B.Email)}}
{{template "row" (kv "Address" .B.Address)}}
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
{{if .B.Address}}{{template "row" (kv "Address" .B.Address)}}{{else if .B.Suburb}}{{template "row" (kv "Suburb" .B.Suburb)}}{{end}}
{{if .B.PreferredTime}}{{template "row" (kv "Preferred time" .B.PreferredTime)}}{{end}}
</table>
<blockquote style="margin:0 0 20px;padding:12px 16px;border-left:3px solid #d9d5cc;background:#faf9f6;white-space:pre-wrap">{{.B.Issue}}</blockquote>
<p style="color:#666">In the meantime: if the computer is on and something looks suspicious, disconnect it from the internet and leave it. Have any passwords you might need handy — I'll never ask you to read them out over the phone.</p>
<p>— James</p>
{{end}}

{{define "quote-verify"}}
<h2 style="margin:0 0 12px">Confirm your email to get your quote</h2>
<p>Hi {{.Q.Name}} — thanks for describing your project. Click the button below to confirm this address and I'll generate your instant estimate straight away.</p>
<p style="margin:24px 0"><a href="{{.Link}}" style="display:inline-block;background:#1c1c1c;color:#fff;text-decoration:none;padding:12px 20px;border-radius:6px;font-weight:600">Confirm email &amp; get my quote</a></p>
<p style="color:#666;font-size:13px">Or paste this link into your browser:<br><a href="{{.Link}}" style="color:#666;word-break:break-all">{{.Link}}</a></p>
<p style="color:#666;font-size:13px">The link is valid for 48 hours. If you didn't request a quote from {{site.BaseURL}}, you can ignore this email — nothing will be generated.</p>
<p>— James</p>
{{end}}

{{define "quote-ready-admin"}}
<h2 style="margin:0 0 4px">Quote #{{.Q.ID}} — email verified, estimate generated</h2>
<table style="border-collapse:collapse;margin:12px 0 20px">
{{template "row" (kv "Name" .Q.Name)}}
{{template "row" (kv "Email" .Q.Email)}}
{{template "row" (kv "Mobile" .Q.Mobile)}}
{{template "row" (kv "Address" .Q.Address)}}
{{template "row" (kv "Configured total" (printf "$%.0f" .Q.TotalCost))}}
</table>
<p style="margin:0 0 6px;color:#666">Description:</p>
<blockquote style="margin:0 0 20px;padding:12px 16px;border-left:3px solid #d9d5cc;background:#faf9f6;white-space:pre-wrap">{{.Q.Description}}</blockquote>
<p style="margin:0 0 6px;color:#666">AI estimate sent to the customer:</p>
<div style="margin:0 0 20px;padding:12px 16px;border:1px solid #e6e3dc;border-radius:6px;background:#fff">{{.Estimate}}</div>
<p><a href="{{.Link}}" style="display:inline-block;background:#1c1c1c;color:#fff;text-decoration:none;padding:10px 16px;border-radius:6px">View quote</a>
&nbsp; <a href="{{site.BaseURL}}/admin" style="color:#666">Open admin</a></p>
{{end}}

{{define "quote-ready-customer"}}
<h2 style="margin:0 0 12px">Your quote{{if .Q.Name}}, {{.Q.Name}}{{end}}</h2>
<p>Thanks for confirming your email. Here's the instant estimate based on what you told me. I'll also review your requirements personally and follow up with a detailed proposal within 2 business days.</p>
<div style="margin:20px 0;padding:16px 20px;border:1px solid #e6e3dc;border-radius:6px;background:#faf9f6">{{.Estimate}}</div>
<p style="margin:0 0 6px;color:#666">What you described:</p>
<blockquote style="margin:0 0 20px;padding:12px 16px;border-left:3px solid #d9d5cc;background:#fff;white-space:pre-wrap">{{.Q.Description}}</blockquote>
<p>You can view this quote any time at <a href="{{.Link}}" style="color:#1c1c1c">{{.Link}}</a>. Questions? Just reply to this email{{if site.Phone}} or call {{site.Phone}}{{end}}.</p>
<p>— James</p>
{{end}}
`

var mailTmpl = template.Must(template.Must(template.New("mail").Funcs(template.FuncMap{
	"site":  func() *siteConfig { return &site },
	"kv":    func(k, v string) map[string]string { return map[string]string{"K": k, "V": v} },
	"money": fmtCents,
	"btn":   func(href, label string) map[string]string { return map[string]string{"Href": href, "Label": label} },
}).Parse(mailTmplSrc)).Parse(mailBillingTmplSrc))

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
	text := fmt.Sprintf("New booking request #%d\n\nName: %s\nPhone: %s\nEmail: %s\nAddress: %s\nService: %s\nPreferred time: %s\n\n%s\n\n%s/admin\n",
		id, b.Name, b.Phone, b.Email, b.Address, svcTitle, b.PreferredTime, b.Issue, site.BaseURL)
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
	send(b.Email, "Got your booking request — Local IT Help", html, text, site.Email)
}

// sendQuoteVerification emails the customer the link that confirms their
// address and triggers estimate generation.
func sendQuoteVerification(q *db.Quote, link string) {
	if !mail.Enabled() || q == nil {
		return
	}
	html, err := renderMail("quote-verify", map[string]any{"Q": q, "Link": link})
	if err != nil {
		log.Printf("email: render quote-verify: %v", err)
		return
	}
	text := fmt.Sprintf("Hi %s — confirm your email to get your instant quote:\n\n%s\n\nThe link is valid for 48 hours. If you didn't request a quote from %s you can ignore this email.\n\n— James\n",
		q.Name, link, site.BaseURL)
	send(q.Email, "Confirm your email to get your quote — Local IT Help", html, text, site.Email)
}

// notifyQuoteVerified runs once a quote's email is verified and the estimate
// exists: it emails James (reply-to the customer) and sends the customer a copy.
func notifyQuoteVerified(q *db.Quote, link string) {
	if !mail.Enabled() || q == nil {
		return
	}
	data := map[string]any{"Q": q, "Link": link, "Estimate": template.HTML(q.AIEstimate)}

	html, err := renderMail("quote-ready-admin", data)
	if err != nil {
		log.Printf("email: render quote-ready-admin: %v", err)
	} else {
		text := fmt.Sprintf("Quote #%d verified — %s <%s> (%s) — configured total $%.0f\n\n%s\n\n%s\n%s/admin\n",
			q.ID, q.Name, q.Email, q.Mobile, q.TotalCost, q.Description, link, site.BaseURL)
		send(notifyEmail, fmt.Sprintf("Quote #%d: %s", q.ID, q.Name), html, text, q.Email)
	}

	html, err = renderMail("quote-ready-customer", data)
	if err != nil {
		log.Printf("email: render quote-ready-customer: %v", err)
		return
	}
	text := fmt.Sprintf("Thanks for confirming your email, %s. Your instant quote is ready — view it here:\n\n%s\n\nI'll review your requirements personally and follow up with a detailed proposal within 2 business days.\n\n— James\n",
		q.Name, link)
	send(q.Email, "Your quote is ready — Local IT Help", html, text, site.Email)
}
