package db

import (
	"context"
	"fmt"
	"math"
	"time"

	"localithelp/db/sqlc"
)

// Invoice statuses.
const (
	InvoiceDraft = "draft"
	InvoiceSent  = "sent"
	InvoicePaid  = "paid"
	InvoiceVoid  = "void"
)

// Payment methods.
const (
	PayZellerLink   = "zeller_link"
	PayBankTransfer = "bank_transfer"
	PayCardOnDay    = "card_on_day"
	PayCash         = "cash"
	PayOther        = "other"
)

// PaymentMethods maps method → label for forms and documents.
var PaymentMethods = []struct{ Value, Label string }{
	{PayZellerLink, "Zeller payment link"},
	{PayBankTransfer, "Bank transfer"},
	{PayCardOnDay, "Card on the day"},
	{PayCash, "Cash"},
	{PayOther, "Other"},
}

// PaymentMethodLabel returns the display label for a payment method value.
func PaymentMethodLabel(v string) string {
	for _, m := range PaymentMethods {
		if m.Value == v {
			return m.Label
		}
	}
	return v
}

// FirstInvoiceNumber is the number assigned to the very first invoice.
const FirstInvoiceNumber = 1000

// Invoice is a bill for a booking. Money is integer cents; no GST (not
// registered). Number is assigned at creation and never reused (voided
// invoices keep theirs).
type Invoice struct {
	ID            int64
	Number        int64
	BookingID     int64
	CustomerID    int64
	Status        string
	IssuedAt      time.Time // local date; zero until sent
	DueAt         time.Time
	PaidAt        time.Time
	PaymentMethod string
	PaymentRef    string
	PaymentLink   string // Zeller payment link pasted by admin
	TotalCents    int64
	Notes         string
	ViewToken     string
	ReviewAskedAt time.Time // local date a Google review was requested; zero if never
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Ref is the human-facing invoice reference, e.g. "INV-1001".
func (v Invoice) Ref() string { return fmt.Sprintf("INV-%d", v.Number) }

// InvoiceItem is one line on an invoice. Qty is fractional-capable so labour
// can be expressed in 15-minute units.
type InvoiceItem struct {
	ID          int64
	InvoiceID   int64
	Description string
	Qty         float64
	UnitCents   int64
	LineCents   int64
	SortOrder   int
}

func sqlcInvoice(r sqlc.Invoice) *Invoice {
	return &Invoice{
		ID: r.ID, Number: r.Number, BookingID: r.BookingID, CustomerID: r.CustomerID, Status: r.Status,
		IssuedAt: ParseDate(r.IssuedAt), DueAt: ParseDate(r.DueAt), PaidAt: ParseDate(r.PaidAt),
		PaymentMethod: r.PaymentMethod, PaymentRef: r.PaymentRef, PaymentLink: r.PaymentLink,
		TotalCents: r.TotalCents, Notes: r.Notes, ViewToken: r.ViewToken, ReviewAskedAt: ParseDate(r.ReviewAskedAt),
		CreatedAt: parseUTC(r.CreatedAt), UpdatedAt: parseUTC(r.UpdatedAt),
	}
}

func sqlcInvoices(rows []sqlc.Invoice, err error) ([]Invoice, error) {
	if err != nil {
		return nil, err
	}
	out := make([]Invoice, len(rows))
	for i, r := range rows {
		out[i] = *sqlcInvoice(r)
	}
	return out, nil
}

// LineCents computes a line total with round-half-away-from-zero on cents.
func LineCents(qty float64, unitCents int64) int64 {
	return int64(math.Round(qty * float64(unitCents)))
}

// SumItems returns the invoice total for items (recomputing each line).
func SumItems(items []InvoiceItem) int64 {
	var total int64
	for _, it := range items {
		total += LineCents(it.Qty, it.UnitCents)
	}
	return total
}

// CreateInvoice inserts a draft invoice with the next number and its items in
// one transaction and returns the new id.
func CreateInvoice(bookingID, customerID int64, dueAt time.Time, notes, token string, items []InvoiceItem) (int64, error) {
	ctx := context.Background()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	qtx := q.WithTx(tx)
	id, err := qtx.InsertInvoice(ctx, sqlc.InsertInvoiceParams{
		BookingID: bookingID, CustomerID: customerID, IssuedAt: "", DueAt: FormatDate(dueAt), Notes: notes, ViewToken: token,
	})
	if err != nil {
		return 0, fmt.Errorf("insert invoice: %w", err)
	}
	if err := writeItems(qtx, id, items); err != nil {
		return 0, err
	}
	if err := qtx.UpdateInvoiceDraft(ctx, sqlc.UpdateInvoiceDraftParams{
		DueAt: FormatDate(dueAt), Notes: notes, PaymentLink: "", TotalCents: SumItems(items), ID: id,
	}); err != nil {
		return 0, fmt.Errorf("set total: %w", err)
	}
	return id, tx.Commit()
}

func writeItems(qtx *sqlc.Queries, invoiceID int64, items []InvoiceItem) error {
	ctx := context.Background()
	if err := qtx.DeleteInvoiceItems(ctx, invoiceID); err != nil {
		return fmt.Errorf("clear items: %w", err)
	}
	for i, it := range items {
		if err := qtx.InsertInvoiceItem(ctx, sqlc.InsertInvoiceItemParams{
			InvoiceID: invoiceID, Description: it.Description, Qty: it.Qty, UnitCents: it.UnitCents,
			LineCents: LineCents(it.Qty, it.UnitCents), SortOrder: int64(i),
		}); err != nil {
			return fmt.Errorf("insert item: %w", err)
		}
	}
	return nil
}

// SaveInvoiceDraft replaces the items and editable fields of a draft invoice
// (no-op on non-drafts because the UPDATE is status-guarded).
func SaveInvoiceDraft(id int64, dueAt time.Time, notes, paymentLink string, items []InvoiceItem) error {
	ctx := context.Background()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	qtx := q.WithTx(tx)
	if err := writeItems(qtx, id, items); err != nil {
		return err
	}
	if err := qtx.UpdateInvoiceDraft(ctx, sqlc.UpdateInvoiceDraftParams{
		DueAt: FormatDate(dueAt), Notes: notes, PaymentLink: paymentLink, TotalCents: SumItems(items), ID: id,
	}); err != nil {
		return fmt.Errorf("update draft: %w", err)
	}
	return tx.Commit()
}

// SetInvoicePaymentLink updates the pasted payment link on a draft or sent invoice.
func SetInvoicePaymentLink(id int64, link string) error {
	return q.UpdateInvoicePaymentLink(context.Background(), sqlc.UpdateInvoicePaymentLinkParams{PaymentLink: link, ID: id})
}

func GetInvoice(id int64) (*Invoice, error) {
	r, err := q.GetInvoice(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return sqlcInvoice(r), nil
}

func GetInvoiceByToken(token string) (*Invoice, error) {
	r, err := q.GetInvoiceByToken(context.Background(), token)
	if err != nil {
		return nil, err
	}
	return sqlcInvoice(r), nil
}

// ListInvoices returns all invoices, or only those with status when non-empty.
func ListInvoices(status string) ([]Invoice, error) {
	if status == "" {
		return sqlcInvoices(q.ListInvoices(context.Background()))
	}
	return sqlcInvoices(q.ListInvoicesByStatus(context.Background(), status))
}

func ListInvoicesByCustomer(customerID int64) ([]Invoice, error) {
	return sqlcInvoices(q.ListInvoicesByCustomer(context.Background(), customerID))
}

func ListInvoicesByBooking(bookingID int64) ([]Invoice, error) {
	return sqlcInvoices(q.ListInvoicesByBooking(context.Background(), bookingID))
}

func ListInvoiceItems(invoiceID int64) ([]InvoiceItem, error) {
	rows, err := q.ListInvoiceItems(context.Background(), invoiceID)
	if err != nil {
		return nil, err
	}
	out := make([]InvoiceItem, len(rows))
	for i, r := range rows {
		out[i] = InvoiceItem{
			ID: r.ID, InvoiceID: r.InvoiceID, Description: r.Description, Qty: r.Qty,
			UnitCents: r.UnitCents, LineCents: r.LineCents, SortOrder: int(r.SortOrder),
		}
	}
	return out, nil
}

// SumOutstandingCents totals invoices that have been sent but not paid.
// ListOverdueInvoices returns sent, unpaid invoices due before the given local
// date, oldest first.
func ListOverdueInvoices(before time.Time) ([]Invoice, error) {
	return sqlcInvoices(q.ListOverdueInvoices(context.Background(), FormatDate(before)))
}

func SumOutstandingCents() (int64, error) {
	v, err := q.SumOutstandingCents(context.Background())
	if err != nil {
		return 0, err
	}
	switch n := v.(type) {
	case int64:
		return n, nil
	case float64:
		return int64(n), nil
	}
	return 0, nil
}

// MarkInvoiceSent flips draft → sent, stamping issued/due dates. Returns false
// if the invoice was not a draft (already sent), so a retry never re-sends.
func MarkInvoiceSent(id int64, issued, due time.Time) (bool, error) {
	n, err := q.MarkInvoiceSent(context.Background(), sqlc.MarkInvoiceSentParams{
		IssuedAt: FormatDate(issued), DueAt: FormatDate(due), ID: id,
	})
	return n == 1, err
}

// MarkInvoicePaid flips draft|sent → paid. Returns false if it was not payable.
func MarkInvoicePaid(id int64, paidAt time.Time, method, ref string) (bool, error) {
	n, err := q.MarkInvoicePaid(context.Background(), sqlc.MarkInvoicePaidParams{
		PaidAt: FormatDate(paidAt), PaymentMethod: method, PaymentRef: ref, IssuedAt: FormatDate(paidAt), ID: id,
	})
	return n == 1, err
}

// MarkInvoiceReviewAsked records the date a Google review was requested for
// this invoice, so the same customer isn't asked twice.
func MarkInvoiceReviewAsked(id int64, on time.Time) error {
	return q.MarkInvoiceReviewAsked(context.Background(), sqlc.MarkInvoiceReviewAskedParams{
		ReviewAskedAt: FormatDate(on), ID: id,
	})
}

// VoidInvoice flips draft|sent → void. Returns false if it was already paid/void.
func VoidInvoice(id int64) (bool, error) {
	n, err := q.VoidInvoice(context.Background(), id)
	return n == 1, err
}
