package db

import (
	"context"
	"database/sql"
	"errors"

	"localithelp/db/sqlc"
)

// ── Customer ↔ Google Contacts ──
//
// One row per customer the contact sync has looked at. The sync itself lives in
// cmd/server/gcontacts_sync.go; this is just its bookkeeping.

// CustomerGContact is a customer's link to a Google contact.
type CustomerGContact struct {
	CustomerID int64
	Resource   string // 'people/c…'; '' when not linked
	Error      string // a problem that needs the admin; '' when fine
	Gone       bool   // deleted in Google; not recreated automatically
	SyncedAt   string // UTC, as SQLite stores it; '' before the first push
}

// ContactID is the id Google Contacts uses in its web URLs
// (contacts.google.com/person/<id>).
func (g *CustomerGContact) ContactID() string {
	if g == nil || len(g.Resource) <= len("people/") {
		return ""
	}
	return g.Resource[len("people/"):]
}

func sqlcGContact(r sqlc.CustomerGcontact) CustomerGContact {
	return CustomerGContact{CustomerID: r.CustomerID, Resource: r.Resource, Error: r.Error, Gone: r.Gone != 0, SyncedAt: r.SyncedAt}
}

// ContactSyncCustomer is a customer due for a push, with any linked contact.
type ContactSyncCustomer struct {
	Customer
	Resource string
}

// ListCustomersForContactSync returns customers changed since their last push
// (or never pushed) that have a phone or an email, oldest first.
func ListCustomersForContactSync(limit int) ([]ContactSyncCustomer, error) {
	rows, err := q.ListCustomersForContactSync(context.Background(), int64(limit))
	if err != nil {
		return nil, err
	}
	out := make([]ContactSyncCustomer, len(rows))
	for i, r := range rows {
		out[i] = ContactSyncCustomer{
			Customer: Customer{
				ID: r.ID, Name: r.Name, Email: r.Email, Phone: r.Phone, Address: r.Address,
				Suburb: r.Suburb, Notes: r.Notes,
				CreatedAt: parseUTC(r.CreatedAt), UpdatedAt: parseUTC(r.UpdatedAt),
			},
			Resource: r.GcontactResource,
		}
	}
	return out, nil
}

// CountCustomersForContactSync is the pending count for the settings page.
func CountCustomersForContactSync() (int, error) {
	n, err := q.CountCustomersForContactSync(context.Background())
	return int(n), err
}

// GetCustomerGContact returns the customer's link, or nil when the sync has
// never looked at them.
func GetCustomerGContact(customerID int64) (*CustomerGContact, error) {
	r, err := q.GetCustomerGContact(context.Background(), customerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	g := sqlcGContact(r)
	return &g, nil
}

// ListCustomerGContacts returns every link, keyed by customer id.
func ListCustomerGContacts() (map[int64]CustomerGContact, error) {
	rows, err := q.ListCustomerGContacts(context.Background())
	if err != nil {
		return nil, err
	}
	out := make(map[int64]CustomerGContact, len(rows))
	for _, r := range rows {
		out[r.CustomerID] = sqlcGContact(r)
	}
	return out, nil
}

// CountLinkedGContacts and CountGContactErrors feed the settings page.
func CountLinkedGContacts() (int, error) {
	n, err := q.CountLinkedGContacts(context.Background())
	return int(n), err
}

func CountGContactErrors() (int, error) {
	n, err := q.CountGContactErrors(context.Background())
	return int(n), err
}

// SetCustomerGContact records a settled push: the linked contact (empty for
// none) and any problem for the admin. The row stops looking dirty until the
// customer is edited again.
func SetCustomerGContact(customerID int64, resource, problem string) error {
	return q.SetCustomerGContact(context.Background(), sqlc.SetCustomerGContactParams{
		CustomerID: customerID, Resource: resource, Error: problem,
	})
}

// SetCustomerGContactError records a failure worth retrying. The link is kept
// and the row stays dirty, so the next scheduler tick tries again.
func SetCustomerGContactError(customerID int64, problem string) error {
	return q.SetCustomerGContactError(context.Background(), sqlc.SetCustomerGContactErrorParams{
		CustomerID: customerID, Error: problem,
	})
}

// SetCustomerGContactGone records that the linked contact was deleted in
// Google. The sync then leaves the customer alone until the admin asks.
func SetCustomerGContactGone(customerID int64, problem string) error {
	return q.SetCustomerGContactGone(context.Background(), sqlc.SetCustomerGContactGoneParams{
		CustomerID: customerID, Error: problem,
	})
}
