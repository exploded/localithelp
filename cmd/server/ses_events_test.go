package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"localithelp/db"
)

const (
	testTopic   = "arn:aws:sns:ap-southeast-2:123456789012:localithelp-ses-events"
	testCertURL = "https://sns.ap-southeast-2.amazonaws.com/SimpleNotificationService-test.pem"
)

// sesWebhookFixture opens a fresh DB, enables the webhook and swaps the AWS
// certificate fetch for a self-signed one, returning a signer for messages.
func sesWebhookFixture(t *testing.T) (sign func(*snsMessage), confirmed *[]string) {
	t.Helper()
	if err := db.Open(filepath.Join(t.TempDir(), "ses.db")); err != nil {
		t.Fatalf("open db: %v", err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "sns.amazonaws.com"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	var urls []string
	oldFetch, oldConfirm, oldNotify := snsCertFetch, snsConfirm, notifyEmail
	snsCertFetch = func(u string) (*x509.Certificate, error) {
		if u != testCertURL {
			return nil, errors.New("unexpected cert URL " + u)
		}
		return cert, nil
	}
	snsConfirm = func(u string) error { urls = append(urls, u); return nil }
	sesTopicARN, notifyEmail = testTopic, "admin@example.test"
	emailBlocks = map[string]db.EmailEvent{}
	t.Cleanup(func() {
		db.Close()
		snsCertFetch, snsConfirm, notifyEmail = oldFetch, oldConfirm, oldNotify
		sesTopicARN = ""
		emailBlocks = map[string]db.EmailEvent{}
		snsCerts.Clear()
	})

	sign = func(m *snsMessage) {
		m.SignatureVersion, m.SigningCertURL = "2", testCertURL
		if m.TopicArn == "" {
			m.TopicArn = testTopic
		}
		sum := sha256.Sum256([]byte(m.stringToSign()))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
		if err != nil {
			t.Fatal(err)
		}
		m.Signature = base64.StdEncoding.EncodeToString(sig)
	}
	return sign, &urls
}

func postSNS(t *testing.T, m *snsMessage) int {
	t.Helper()
	body, _ := json.Marshal(m)
	rec := httptest.NewRecorder()
	handleSESWebhook(rec, httptest.NewRequest(http.MethodPost, "/webhooks/ses", strings.NewReader(string(body))))
	return rec.Code
}

// sesNotification wraps an SES event in a signed SNS Notification.
func sesNotification(sign func(*snsMessage), id, event string) *snsMessage {
	m := &snsMessage{Type: "Notification", MessageID: id, Message: event, Timestamp: "2026-10-31T01:02:03.000Z"}
	sign(m)
	return m
}

const permanentBounce = `{"eventType":"Bounce","bounce":{"bounceType":"Permanent","bounceSubType":"General",
	"feedbackId":"fb-1","bouncedRecipients":[{"emailAddress":"Jo <Jo@Example.com>","diagnosticCode":"smtp; 550 5.1.1 user unknown"}]},
	"mail":{"messageId":"ses-1","commonHeaders":{"subject":"Invoice INV-1003 from Local IT Help"}}}`

func TestSESWebhookBlocksPermanentBounce(t *testing.T) {
	sign, _ := sesWebhookFixture(t)

	if code := postSNS(t, sesNotification(sign, "n1", permanentBounce)); code != http.StatusNoContent {
		t.Fatalf("bounce: status %d, want 204", code)
	}
	e := emailBlock("jo@example.com")
	if e == nil {
		t.Fatal("jo@example.com not blocked after a permanent bounce")
	}
	if e.Subject != "Invoice INV-1003 from Local IT Help" || e.MessageID != "ses-1" || !strings.Contains(e.Diagnostic, "5.1.1") {
		t.Errorf("event not recorded in full: %+v", e)
	}
	if err := sendNow("JO@example.com", "s", "h", "t", ""); !errors.Is(err, errEmailBlocked) {
		t.Errorf("sendNow to a bounced address: err = %v, want errEmailBlocked", err)
	}

	// An SNS retry of the same report is accepted but not stored twice.
	if code := postSNS(t, sesNotification(sign, "n1", permanentBounce)); code != http.StatusNoContent {
		t.Fatalf("retry: status %d, want 204", code)
	}
	if fresh, err := db.RecordEmailEvent(db.EmailEvent{Email: "jo@example.com", Kind: db.EmailBounce, FeedbackID: "fb-1"}); err != nil || fresh {
		t.Errorf("same feedback id stored again: fresh=%v err=%v", fresh, err)
	}

	// The block survives a restart.
	emailBlocks = map[string]db.EmailEvent{}
	initSESEvents()
	if emailBlock("jo@example.com") == nil {
		t.Error("block not reloaded from the database")
	}
}

func TestSESWebhookTransientAndComplaint(t *testing.T) {
	sign, _ := sesWebhookFixture(t)

	transient := `{"eventType":"Bounce","bounce":{"bounceType":"Transient","bounceSubType":"MailboxFull","feedbackId":"fb-2",
		"bouncedRecipients":[{"emailAddress":"full@example.com"}]},"mail":{"messageId":"ses-2"}}`
	if code := postSNS(t, sesNotification(sign, "n2", transient)); code != http.StatusNoContent {
		t.Fatalf("transient: status %d", code)
	}
	if emailBlock("full@example.com") != nil {
		t.Error("a transient bounce blocked the address")
	}

	complaint := `{"eventType":"Complaint","complaint":{"complaintFeedbackType":"abuse","feedbackId":"fb-3",
		"complainedRecipients":[{"emailAddress":"cross@example.com"}]},"mail":{"messageId":"ses-3"}}`
	if code := postSNS(t, sesNotification(sign, "n3", complaint)); code != http.StatusNoContent {
		t.Fatalf("complaint: status %d", code)
	}
	if e := emailBlock("cross@example.com"); e == nil || e.Label() != "spam complaint" {
		t.Errorf("complaint: block = %+v", e)
	}
}

func TestSESWebhookRejects(t *testing.T) {
	sign, confirmed := sesWebhookFixture(t)

	tampered := sesNotification(sign, "n4", permanentBounce)
	tampered.Message = strings.Replace(tampered.Message, "Jo@Example.com", "victim@example.com", 1)

	otherTopic := &snsMessage{Type: "Notification", MessageID: "n5", Message: permanentBounce,
		TopicArn: "arn:aws:sns:ap-southeast-2:999999999999:someone-else"}
	sign(otherTopic) // validly signed by "AWS", but not our topic

	foreignCert := sesNotification(sign, "n6", permanentBounce)
	foreignCert.SigningCertURL = "https://evil.example/cert.pem"

	badSubscribe := &snsMessage{Type: "SubscriptionConfirmation", MessageID: "n7", Token: "t",
		SubscribeURL: "https://evil.example/confirm"}
	sign(badSubscribe)

	for name, m := range map[string]*snsMessage{
		"tampered message": tampered, "other topic": otherTopic,
		"foreign cert URL": foreignCert, "foreign subscribe URL": badSubscribe,
	} {
		if code := postSNS(t, m); code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, code)
		}
	}
	if emailBlock("victim@example.com") != nil || emailBlock("jo@example.com") != nil {
		t.Error("a rejected message blocked an address")
	}
	if len(*confirmed) != 0 {
		t.Errorf("visited subscribe URLs %v", *confirmed)
	}

	sesTopicARN = ""
	if code := postSNS(t, sesNotification(sign, "n8", permanentBounce)); code != http.StatusNotFound {
		t.Errorf("disabled webhook: status %d, want 404", code)
	}
}

func TestSESWebhookConfirmsSubscription(t *testing.T) {
	sign, confirmed := sesWebhookFixture(t)
	const sub = "https://sns.ap-southeast-2.amazonaws.com/?Action=ConfirmSubscription&TopicArn=x&Token=t"
	m := &snsMessage{Type: "SubscriptionConfirmation", MessageID: "n9", Token: "t", SubscribeURL: sub,
		Message: "You have chosen to subscribe…", Timestamp: "2026-10-31T01:02:03.000Z"}
	sign(m)
	if code := postSNS(t, m); code != http.StatusNoContent {
		t.Fatalf("status %d, want 204", code)
	}
	if len(*confirmed) != 1 || (*confirmed)[0] != sub {
		t.Errorf("confirmed %v, want [%s]", *confirmed, sub)
	}
}

// TestSESWebhookRoute checks the real router serves the webhook: 404 until the
// topic is configured, then 400 for an empty body. scripts/ses-setup.sh relies
// on that difference to know when it can subscribe.
func TestSESWebhookRoute(t *testing.T) {
	post := func() int {
		rec := httptest.NewRecorder()
		newMux("../..").ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/ses", nil))
		return rec.Code
	}
	sesTopicARN = ""
	if code := post(); code != http.StatusNotFound {
		t.Errorf("unconfigured: status %d, want 404", code)
	}
	sesTopicARN = testTopic
	t.Cleanup(func() { sesTopicARN = "" })
	if code := post(); code != http.StatusBadRequest {
		t.Errorf("configured, empty body: status %d, want 400", code)
	}
}

func TestEmailBlockedAlertRenders(t *testing.T) {
	site = siteConfig{BaseURL: "https://example.test", Email: "me@example.test"}
	e := db.EmailEvent{Email: "jo@example.com", Kind: db.EmailBounce, BounceType: "Permanent", SubType: "General",
		Diagnostic: "smtp; 550 <b>no</b>", Subject: "Invoice INV-1003", CreatedAt: time.Now()}
	cust := &db.Customer{ID: 4, Name: "Jo", Phone: "0400 111 222"}
	for name, c := range map[string]*db.Customer{"customer": cust, "no customer": nil} {
		out, err := renderMail("email-blocked-admin", map[string]any{"E": e, "Cust": c})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(out, "<b>no</b>") {
			t.Errorf("%s: diagnostic not escaped", name)
		}
		if !strings.Contains(out, "Invoice INV-1003") {
			t.Errorf("%s: missing the bounced subject", name)
		}
		if want := "https://example.test/admin/customers/4"; (c != nil) != strings.Contains(out, want) {
			t.Errorf("%s: customer link presence wrong:\n%s", name, out)
		}
	}
}

func TestEmailBlockNeverBlocksAdmin(t *testing.T) {
	old := notifyEmail
	notifyEmail = "James@LocalITHelp.com.au"
	emailBlocks = map[string]db.EmailEvent{"james@localithelp.com.au": {Kind: db.EmailComplaint}}
	t.Cleanup(func() { notifyEmail, emailBlocks = old, map[string]db.EmailEvent{} })
	if emailBlock("james@localithelp.com.au") != nil {
		t.Error("the admin's own address was blocked")
	}
}
