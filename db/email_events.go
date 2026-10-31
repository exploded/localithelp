package db

import (
	"context"
	"strings"
	"time"

	"localithelp/db/sqlc"
)

// Email events are the bounces and complaints SES reports back after a send.
// A permanent bounce or a complaint means mail to that address is pointless
// (SES drops it via its account suppression list anyway), so the app flags the
// address in the admin and refuses to send to it. Status is per address, not
// per customer: bookings and quotes carry their own copy of the address, and a
// corrected address starts clean.

const (
	EmailBounce    = "bounce"
	EmailComplaint = "complaint"
)

// EmailEvent is one recipient's entry in an SES bounce or complaint report.
type EmailEvent struct {
	ID         int64
	Email      string // lower-cased
	Kind       string // EmailBounce | EmailComplaint
	BounceType string // Permanent | Transient | Undetermined; "" for complaints
	SubType    string // e.g. NoEmail, MailboxFull; complaint feedback type such as abuse
	Diagnostic string // the remote server's SMTP response, e.g. "smtp; 550 5.1.1 user unknown"
	Subject    string // subject of the message that bounced
	MessageID  string // SES message id of that message
	FeedbackID string
	CreatedAt  time.Time
}

// Blocking reports whether the event should stop further sends to the address.
func (e EmailEvent) Blocking() bool {
	return e.Kind == EmailComplaint || e.BounceType == "Permanent"
}

// Label is the short badge text shown next to the address in the admin.
func (e EmailEvent) Label() string {
	if e.Kind == EmailComplaint {
		return "spam complaint"
	}
	return "bounced"
}

// Summary describes the event in one line, for tooltips and error messages.
func (e EmailEvent) Summary() string {
	var s string
	if e.Kind == EmailComplaint {
		s = "Marked as spam"
	} else {
		s = strings.TrimSpace(e.BounceType + " bounce")
	}
	s += " on " + e.CreatedAt.In(Melbourne).Format("2 Jan 2006")
	if e.Diagnostic != "" {
		s += ": " + e.Diagnostic
	} else if e.SubType != "" {
		s += " (" + e.SubType + ")"
	}
	return s
}

// RecordEmailEvent stores an event and reports whether it was new. A repeat of
// the same feedback report for the same address (an SNS retry) is ignored.
func RecordEmailEvent(e EmailEvent) (bool, error) {
	n, err := q.InsertEmailEvent(context.Background(), sqlc.InsertEmailEventParams{
		Email: strings.ToLower(strings.TrimSpace(e.Email)), Kind: e.Kind, BounceType: e.BounceType,
		SubType: e.SubType, Diagnostic: e.Diagnostic, Subject: e.Subject,
		MessageID: e.MessageID, FeedbackID: e.FeedbackID,
	})
	return n == 1, err
}

// ListEmailBlocks returns the latest blocking event for each address that has one.
func ListEmailBlocks() (map[string]EmailEvent, error) {
	rows, err := q.ListBlockingEmailEvents(context.Background())
	if err != nil {
		return nil, err
	}
	out := make(map[string]EmailEvent, len(rows))
	for _, r := range rows { // ordered by id, so a later event overwrites an earlier one
		out[r.Email] = EmailEvent{
			ID: r.ID, Email: r.Email, Kind: r.Kind, BounceType: r.BounceType, SubType: r.SubType,
			Diagnostic: r.Diagnostic, Subject: r.Subject, MessageID: r.MessageID,
			FeedbackID: r.FeedbackID, CreatedAt: parseUTC(r.CreatedAt),
		}
	}
	return out, nil
}
