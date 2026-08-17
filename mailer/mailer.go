// Package mailer sends transactional email through the Amazon SES v2 API.
package mailer

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sestypes "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

// Mailer sends HTML email from a fixed sender address.
type Mailer struct {
	client *sesv2.Client
	from   string
}

// New creates a Mailer using static AWS credentials. It returns nil when the
// credentials are empty so callers can treat "email not configured" as a
// graceful no-op (a nil *Mailer's methods are safe to call).
func New(region, accessKey, secretKey, from string) *Mailer {
	if accessKey == "" || secretKey == "" || from == "" {
		return nil
	}
	cfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
	}
	return &Mailer{client: sesv2.NewFromConfig(cfg), from: from}
}

// Enabled reports whether the mailer is configured.
func (m *Mailer) Enabled() bool { return m != nil }

// Attachment is a file to attach to an email.
type Attachment struct {
	Filename    string
	ContentType string // e.g. "application/pdf"
	Data        []byte
}

// Send sends an HTML email (with a plain-text fallback derived by the caller).
// replyTo may be empty.
func (m *Mailer) Send(to, subject, htmlBody, textBody, replyTo string) error {
	return m.SendWithAttachments(to, subject, htmlBody, textBody, replyTo)
}

// SendWithAttachments is Send plus zero or more file attachments (SES v2
// simple-message attachments; the SDK handles the MIME encoding).
func (m *Mailer) SendWithAttachments(to, subject, htmlBody, textBody, replyTo string, atts ...Attachment) error {
	if m == nil {
		return nil
	}
	body := &sestypes.Body{
		Html: &sestypes.Content{Data: aws.String(htmlBody), Charset: aws.String("UTF-8")},
	}
	if textBody != "" {
		body.Text = &sestypes.Content{Data: aws.String(textBody), Charset: aws.String("UTF-8")}
	}
	msg := &sestypes.Message{
		Subject: &sestypes.Content{Data: aws.String(subject), Charset: aws.String("UTF-8")},
		Body:    body,
	}
	for _, a := range atts {
		// SES defaults ContentTransferEncoding to SEVEN_BIT, which corrupts
		// binary payloads (a PDF arrives the right size but with its compressed
		// streams mangled → blank pages). Always ask for base64.
		att := sestypes.Attachment{
			FileName:                aws.String(a.Filename),
			RawContent:              a.Data,
			ContentDisposition:      sestypes.AttachmentContentDispositionAttachment,
			ContentTransferEncoding: sestypes.AttachmentContentTransferEncodingBase64,
		}
		if a.ContentType != "" {
			att.ContentType = aws.String(a.ContentType)
		}
		msg.Attachments = append(msg.Attachments, att)
	}
	in := &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(m.from),
		Destination:      &sestypes.Destination{ToAddresses: []string{to}},
		Content:          &sestypes.EmailContent{Simple: msg},
	}
	if replyTo != "" {
		in.ReplyToAddresses = []string{replyTo}
	}
	// Bounded so a stuck SES call can never hang a request goroutine.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := m.client.SendEmail(ctx, in); err != nil {
		return fmt.Errorf("ses send to %s: %w", to, err)
	}
	return nil
}
