package main

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	netmail "net/mail"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"localithelp/db"
)

// ── SES bounces and complaints ──
//
// Every send names the SES_CONFIGURATION_SET, whose event destination
// publishes bounces and complaints to an SNS topic, which POSTs them to
// /webhooks/ses. The handler checks the message came from SES_EVENTS_TOPIC_ARN
// and carries a valid SNS signature (the signature alone only proves AWS sent
// it, not that the topic is ours), records each recipient in email_events, and
// emails the admin about permanent bounces and complaints. Blocked addresses
// are held in memory so send/sendNow and the admin templates can check them
// cheaply. scripts/ses-setup.sh creates the configuration set, topic and
// subscription.

var (
	sesTopicARN string // SES_EVENTS_TOPIC_ARN; "" = webhook disabled (404)

	emailBlocksMu sync.RWMutex
	emailBlocks   = map[string]db.EmailEvent{} // address -> latest blocking event
)

// errEmailBlocked marks a send refused because the address bounced or complained.
var errEmailBlocked = errors.New("address blocked")

// initSESEvents reads the topic ARN and loads blocked addresses. Call after db.Open.
func initSESEvents() {
	sesTopicARN = os.Getenv("SES_EVENTS_TOPIC_ARN")
	blocks, err := db.ListEmailBlocks()
	if err != nil {
		slog.Error("email blocks: load", "err", err)
		return
	}
	emailBlocksMu.Lock()
	emailBlocks = blocks
	emailBlocksMu.Unlock()
	if sesTopicARN == "" {
		slog.Info("SES_EVENTS_TOPIC_ARN not set — bounce webhook disabled", "blocked", len(blocks))
	} else {
		slog.Info("SES bounce webhook enabled", "topic", sesTopicARN, "blocked", len(blocks))
	}
}

// emailBlock returns the event that blocks mail to addr, or nil. The admin's
// own notification address is never blocked, so bounce alerts keep arriving.
func emailBlock(addr string) *db.EmailEvent {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if addr == "" || addr == strings.ToLower(notifyEmail) {
		return nil
	}
	emailBlocksMu.RLock()
	defer emailBlocksMu.RUnlock()
	if e, ok := emailBlocks[addr]; ok {
		return &e
	}
	return nil
}

// ── SNS ──

// snsMessage is the JSON body SNS POSTs to an HTTPS subscriber.
type snsMessage struct {
	Type             string
	MessageID        string `json:"MessageId"`
	Token            string
	TopicArn         string
	Subject          string
	Message          string
	SubscribeURL     string
	Timestamp        string
	SignatureVersion string
	Signature        string
	SigningCertURL   string
}

// snsHost matches the regional SNS endpoints that serve signing certificates
// and subscription confirmations, e.g. sns.ap-southeast-2.amazonaws.com.
var snsHost = regexp.MustCompile(`^sns\.[a-z0-9-]+\.amazonaws\.com$`)

func validSNSURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && snsHost.MatchString(u.Host)
}

var snsClient = &http.Client{Timeout: 10 * time.Second}

// snsCertFetch downloads a signing certificate. A var so tests can stub it.
var snsCertFetch = func(certURL string) (*x509.Certificate, error) {
	resp, err := snsClient.Get(certURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch cert: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(body)
	if block == nil {
		return nil, errors.New("cert: no PEM block")
	}
	return x509.ParseCertificate(block.Bytes)
}

// snsConfirm visits a subscription's SubscribeURL. A var so tests can stub it.
var snsConfirm = func(subscribeURL string) error {
	resp, err := snsClient.Get(subscribeURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("confirm subscription: %s", resp.Status)
	}
	return nil
}

var snsCerts sync.Map // cert URL -> *x509.Certificate

// stringToSign rebuilds the canonical text SNS signs: selected fields as
// "Name\nvalue\n" pairs in a fixed order that depends on the message type.
func (m *snsMessage) stringToSign() string {
	var b strings.Builder
	add := func(k, v string) { b.WriteString(k + "\n" + v + "\n") }
	add("Message", m.Message)
	add("MessageId", m.MessageID)
	if m.Type == "Notification" {
		if m.Subject != "" {
			add("Subject", m.Subject)
		}
		add("Timestamp", m.Timestamp)
		add("TopicArn", m.TopicArn)
		add("Type", m.Type)
		return b.String()
	}
	add("SubscribeURL", m.SubscribeURL)
	add("Timestamp", m.Timestamp)
	add("Token", m.Token)
	add("TopicArn", m.TopicArn)
	add("Type", m.Type)
	return b.String()
}

// verify checks the message's SHA256-RSA signature against the AWS
// certificate it names. Only SignatureVersion 2 is accepted; the setup script
// sets it on the topic.
func (m *snsMessage) verify() error {
	if m.SignatureVersion != "2" {
		return fmt.Errorf("signature version %q, want 2", m.SignatureVersion)
	}
	if !validSNSURL(m.SigningCertURL) || !strings.HasSuffix(m.SigningCertURL, ".pem") {
		return fmt.Errorf("signing cert URL %q is not an SNS certificate", m.SigningCertURL)
	}
	var cert *x509.Certificate
	if c, ok := snsCerts.Load(m.SigningCertURL); ok {
		cert = c.(*x509.Certificate)
	} else {
		c, err := snsCertFetch(m.SigningCertURL)
		if err != nil {
			return err
		}
		snsCerts.Store(m.SigningCertURL, c)
		cert = c
	}
	if now := time.Now(); now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return errors.New("signing cert is outside its validity period")
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return errors.New("signing cert key is not RSA")
	}
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil {
		return fmt.Errorf("signature: %w", err)
	}
	sum := sha256.Sum256([]byte(m.stringToSign()))
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig)
}

// handleSESWebhook receives SNS deliveries for the SES events topic. Non-2xx
// replies make SNS retry, so only failures a retry could fix return 5xx.
func handleSESWebhook(w http.ResponseWriter, r *http.Request) {
	if sesTopicARN == "" {
		http.NotFound(w, r)
		return
	}
	var m snsMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&m); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Topic first: it's free, and it stops strangers making us fetch certs.
	if m.TopicArn != sesTopicARN {
		slog.Warn("ses webhook: unexpected topic", "topic", m.TopicArn, "ip", clientIP(r))
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := m.verify(); err != nil {
		slog.Warn("ses webhook: bad signature", "err", err, "ip", clientIP(r))
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch m.Type {
	case "SubscriptionConfirmation":
		if !validSNSURL(m.SubscribeURL) {
			slog.Warn("ses webhook: bad subscribe URL", "url", m.SubscribeURL)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := snsConfirm(m.SubscribeURL); err != nil {
			slog.Error("ses webhook: confirm subscription", "err", err)
			http.Error(w, "confirm failed", http.StatusBadGateway)
			return
		}
		slog.Info("ses webhook: subscription confirmed", "topic", m.TopicArn)
	case "UnsubscribeConfirmation":
		slog.Warn("ses webhook: unsubscribed from topic", "topic", m.TopicArn)
	case "Notification":
		if err := handleSESEvent(m.Message); err != nil {
			slog.Error("ses webhook: record event", "err", err)
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── SES events ──

// sesEvent is the part of an SES event we use. Configuration-set event
// publishing names the type in eventType; identity notifications use
// notificationType with the same payload.
type sesEvent struct {
	EventType        string `json:"eventType"`
	NotificationType string `json:"notificationType"`
	Bounce           *struct {
		BounceType        string `json:"bounceType"`
		BounceSubType     string `json:"bounceSubType"`
		FeedbackID        string `json:"feedbackId"`
		BouncedRecipients []struct {
			EmailAddress   string `json:"emailAddress"`
			DiagnosticCode string `json:"diagnosticCode"`
		} `json:"bouncedRecipients"`
	} `json:"bounce"`
	Complaint *struct {
		ComplaintFeedbackType string `json:"complaintFeedbackType"`
		FeedbackID            string `json:"feedbackId"`
		ComplainedRecipients  []struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"complainedRecipients"`
	} `json:"complaint"`
	Mail struct {
		MessageID     string `json:"messageId"`
		CommonHeaders struct {
			Subject string `json:"subject"`
		} `json:"commonHeaders"`
	} `json:"mail"`
}

// handleSESEvent records the recipients of one bounce or complaint, blocks the
// addresses that need it and tells the admin. It returns an error only when
// the database write failed, so SNS retries; duplicates are ignored.
func handleSESEvent(raw string) error {
	var ev sesEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		slog.Warn("ses event: unreadable", "err", err)
		return nil // a retry would be just as unreadable
	}
	var events []db.EmailEvent
	switch {
	case ev.Bounce != nil:
		for _, rc := range ev.Bounce.BouncedRecipients {
			events = append(events, db.EmailEvent{
				Email: bareAddress(rc.EmailAddress), Kind: db.EmailBounce,
				BounceType: ev.Bounce.BounceType, SubType: ev.Bounce.BounceSubType,
				Diagnostic: truncate(rc.DiagnosticCode, 500), FeedbackID: ev.Bounce.FeedbackID,
			})
		}
	case ev.Complaint != nil:
		for _, rc := range ev.Complaint.ComplainedRecipients {
			events = append(events, db.EmailEvent{
				Email: bareAddress(rc.EmailAddress), Kind: db.EmailComplaint,
				SubType: ev.Complaint.ComplaintFeedbackType, FeedbackID: ev.Complaint.FeedbackID,
			})
		}
	default:
		slog.Info("ses event: ignored", "type", ev.EventType+ev.NotificationType)
		return nil
	}
	for _, e := range events {
		if e.Email == "" {
			continue
		}
		e.Subject, e.MessageID = ev.Mail.CommonHeaders.Subject, ev.Mail.MessageID
		isNew, err := db.RecordEmailEvent(e)
		if err != nil {
			return fmt.Errorf("record %s for %s: %w", e.Kind, e.Email, err)
		}
		if !isNew {
			continue
		}
		e.CreatedAt = time.Now().UTC()
		if !e.Blocking() {
			slog.Info("email: transient bounce", "to", e.Email, "type", e.SubType, "diagnostic", e.Diagnostic)
			continue
		}
		emailBlocksMu.Lock()
		emailBlocks[e.Email] = e
		emailBlocksMu.Unlock()
		slog.Warn("email: address blocked", "to", e.Email, "kind", e.Kind, "type", e.SubType,
			"diagnostic", e.Diagnostic, "subject", e.Subject)
		notifyEmailBlocked(e)
	}
	return nil
}

// bareAddress turns "Name <a@b>" or " A@B " into "a@b".
func bareAddress(s string) string {
	if a, err := netmail.ParseAddress(s); err == nil {
		s = a.Address
	}
	return strings.ToLower(strings.TrimSpace(s))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// notifyEmailBlocked tells the admin an address bounced or complained, with
// the customer it belongs to so they can phone for a working address.
func notifyEmailBlocked(e db.EmailEvent) {
	if !mail.Enabled() {
		return
	}
	cust, err := db.FindCustomer(e.Email, "")
	if err != nil {
		slog.Error("email blocked: find customer", "err", err)
	}
	html, err := renderMail("email-blocked-admin", map[string]any{"E": e, "Cust": cust})
	if err != nil {
		slog.Error("email: render email-blocked-admin", "err", err)
		return
	}
	subj := "Email to " + e.Email + " bounced"
	if e.Kind == db.EmailComplaint {
		subj = e.Email + " marked your email as spam"
	}
	text := fmt.Sprintf("%s\n\nMessage: %s\n%s\n\nThe site won't email this address again.\n", subj, e.Subject, e.Summary())
	if cust != nil {
		text += fmt.Sprintf("\nCustomer: %s %s\n%s/admin/customers/%d\n", cust.Name, cust.Phone, site.BaseURL, cust.ID)
	}
	send(notifyEmail, subj, html, text, "")
}

const mailSESTmplSrc = `
{{define "email-blocked-admin"}}
{{if eq .E.Kind "complaint"}}<h2 style="margin:0 0 12px">{{.E.Email}} marked your email as spam</h2>
<p>The site won't email this address again.</p>
{{else}}<h2 style="margin:0 0 12px">Email to {{.E.Email}} bounced</h2>
<p>The address can't receive mail, so this message didn't arrive and the site won't email the address again. Phone them for a working address and update it in admin.</p>{{end}}
<table style="border-collapse:collapse;margin:12px 0 20px">
{{if .E.Subject}}{{template "row" (kv "Message" .E.Subject)}}{{end}}
{{if .E.SubType}}{{template "row" (kv "Reason" .E.SubType)}}{{end}}
{{if .E.Diagnostic}}{{template "row" (kv "Server said" .E.Diagnostic)}}{{end}}
{{if .Cust}}{{template "row" (kv "Customer" .Cust.Name)}}
{{if .Cust.Phone}}{{template "row" (kv "Phone" .Cust.Phone)}}{{end}}{{end}}
</table>
{{if .Cust}}<p>{{template "btn" (btn (printf "%s/admin/customers/%d" site.BaseURL .Cust.ID) "Open customer")}}</p>
{{else}}<p style="color:#666">No customer has this address, so it's probably from a quote request.</p>
<p><a href="{{site.BaseURL}}/admin" style="color:#666">Open admin</a></p>{{end}}
{{end}}
`
