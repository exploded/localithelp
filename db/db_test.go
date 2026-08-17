package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openTestDB(t *testing.T) {
	t.Helper()
	if err := Open(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { Close() })
}

func TestCustomerLinkingAndBackfill(t *testing.T) {
	openTestDB(t)
	ctx := context.Background()

	// A legacy booking with no customer (simulates rows from before the migration).
	if _, err := conn.ExecContext(ctx, `INSERT INTO bookings (name, phone, email, suburb) VALUES ('Old Row', '0400 000 001', 'OLD@Example.com', 'Donvale')`); err != nil {
		t.Fatal(err)
	}
	if err := BackfillCustomers(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if err := BackfillCustomers(); err != nil { // idempotent
		t.Fatalf("backfill again: %v", err)
	}
	custs, err := ListCustomers("")
	if err != nil {
		t.Fatal(err)
	}
	if len(custs) != 1 || custs[0].Email != "old@example.com" {
		t.Fatalf("expected one lower-cased customer, got %+v", custs)
	}
	bs, _ := ListBookings()
	if len(bs) != 1 || bs[0].CustomerID != custs[0].ID {
		t.Fatalf("legacy booking not linked: %+v", bs)
	}

	// New enquiry with same email but different case → same customer.
	id, err := FindOrCreateCustomer("Old Row", "Old@example.COM", "", "")
	if err != nil || id != custs[0].ID {
		t.Fatalf("email match: id=%d err=%v", id, err)
	}
	// Match by phone digits when email is missing (+61 form).
	id, err = FindOrCreateCustomer("Old Row", "", "+61 400 000 001", "")
	if err != nil || id != custs[0].ID {
		t.Fatalf("phone match: id=%d err=%v", id, err)
	}
	// Different person → new customer.
	id2, err := FindOrCreateCustomer("New Person", "new@example.com", "0400 000 002", "Ringwood")
	if err != nil || id2 == custs[0].ID {
		t.Fatalf("new customer: id=%d err=%v", id2, err)
	}

	// Insert + follow-up.
	bid, err := InsertBooking(&Booking{Name: "New Person", Email: "new@example.com", CustomerID: id2, Mode: "onsite"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := GetBooking(bid)
	start := time.Date(2026, 8, 20, 9, 30, 0, 0, Melbourne)
	if err := ScheduleBooking(bid, start, 45); err != nil {
		t.Fatal(err)
	}
	b, _ = GetBooking(bid)
	if b.Status != BookingBooked || !b.StartAt.Equal(start) || b.DurationMin != 45 {
		t.Fatalf("schedule not applied: %+v", b)
	}
	got, _ := ListBookingsBetween(start.Add(-time.Hour), start.Add(time.Hour))
	if len(got) != 1 {
		t.Fatalf("between: got %d", len(got))
	}
	fid, err := CreateFollowup(b, "Return laptop")
	if err != nil {
		t.Fatal(err)
	}
	kids, _ := ListChildBookings(bid)
	if len(kids) != 1 || kids[0].ID != fid || kids[0].CustomerID != id2 || kids[0].Status != BookingNew {
		t.Fatalf("followup: %+v", kids)
	}
}

func TestInvoiceNumberingAndTransitions(t *testing.T) {
	openTestDB(t)
	items := []InvoiceItem{
		{Description: "Onsite service fee", Qty: 1, UnitCents: 8000},
		{Description: "Labour", Qty: 4, UnitCents: 3000},
		{Description: "SSD", Qty: 1, UnitCents: 12950},
	}
	due := time.Date(2026, 8, 27, 0, 0, 0, 0, Melbourne)
	id1, err := CreateInvoice(0, 0, due, "", "tok1", items)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := CreateInvoice(0, 0, due, "", "tok2", items[:1])
	if err != nil {
		t.Fatal(err)
	}
	a, _ := GetInvoice(id1)
	b, _ := GetInvoice(id2)
	if a.Number != FirstInvoiceNumber || b.Number != FirstInvoiceNumber+1 {
		t.Fatalf("numbers: %d %d", a.Number, b.Number)
	}
	if a.TotalCents != 8000+12000+12950 || a.Ref() != "INV-1000" || !a.DueAt.Equal(due) {
		t.Fatalf("invoice a: %+v", a)
	}
	got, _ := ListInvoiceItems(id1)
	if len(got) != 3 || got[1].LineCents != 12000 {
		t.Fatalf("items: %+v", got)
	}
	if v, err := GetInvoiceByToken("tok2"); err != nil || v.ID != id2 {
		t.Fatalf("by token: %v", err)
	}

	// Edit draft, then send; second send is a no-op.
	if err := SaveInvoiceDraft(id1, due, "note", "https://pay.example/x", items[:2]); err != nil {
		t.Fatal(err)
	}
	a, _ = GetInvoice(id1)
	if a.TotalCents != 20000 || a.PaymentLink != "https://pay.example/x" {
		t.Fatalf("draft save: %+v", a)
	}
	today := Today()
	if ok, err := MarkInvoiceSent(id1, today, due); err != nil || !ok {
		t.Fatalf("send: ok=%v err=%v", ok, err)
	}
	if ok, _ := MarkInvoiceSent(id1, today, due); ok {
		t.Fatal("second send should be a no-op")
	}
	if err := SaveInvoiceDraft(id1, due, "changed", "", items); err != nil {
		t.Fatal(err)
	}
	a, _ = GetInvoice(id1)
	if a.Notes != "note" || a.Status != InvoiceSent {
		t.Fatalf("sent invoice must not be editable: %+v", a)
	}
	out, _ := SumOutstandingCents()
	if out != 20000 {
		t.Fatalf("outstanding: %d", out)
	}
	if ok, _ := MarkInvoicePaid(id1, today, PayZellerLink, "Z123"); !ok {
		t.Fatal("paid")
	}
	if ok, _ := VoidInvoice(id1); ok {
		t.Fatal("void of paid invoice must fail")
	}
	if ok, _ := VoidInvoice(id2); !ok {
		t.Fatal("void draft")
	}
	a, _ = GetInvoice(id1)
	if a.Status != InvoicePaid || a.PaymentMethod != PayZellerLink || a.PaidAt.IsZero() {
		t.Fatalf("paid: %+v", a)
	}
}

func TestPhoneAndTime(t *testing.T) {
	if NormalizePhone("+61 400 000 001") != "0400000001" || NormalizePhone("(03) 9876 5432") != "0398765432" {
		t.Fatal("normalize")
	}
	if ParseStartAt("2026-08-20 09:30").Format("15:04 MST") == "" || !ParseStartAt("").IsZero() || !ParseStartAt("junk").IsZero() {
		t.Fatal("parse start")
	}
	if FormatStartAt(time.Date(2026, 8, 20, 9, 30, 0, 0, Melbourne)) != "2026-08-20 09:30" {
		t.Fatal("format start")
	}
}
