package db

import (
	"strconv"
	"testing"
	"time"
)

func TestAdClickRecordAndClaim(t *testing.T) {
	openTestDB(t)

	if err := RecordAdClick(AdClick{
		Token: "tok-1", Source: SourceGoogleAds, GCLID: "abc123",
		Keyword: "computer technician near me", Campaign: "22334455", Landing: "/computer-help/donvale",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := RecordAdClick(AdClick{Token: "tok-2", Source: SourceGoogleAds, GCLID: "def456"}); err != nil {
		t.Fatalf("record second: %v", err)
	}

	got, err := GetAdClickByToken("tok-1")
	if err != nil {
		t.Fatalf("by token: %v", err)
	}
	if got == nil || got.Keyword != "computer technician near me" || got.Campaign != "22334455" ||
		got.Landing != "/computer-help/donvale" || got.BookingID != 0 {
		t.Fatalf("by token = %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Error("created_at did not parse")
	}

	// An unknown token is nothing to worry about, not an error.
	if c, err := GetAdClickByToken("nope"); err != nil || c != nil {
		t.Errorf("unknown token = %+v, %v; want nil, nil", c, err)
	}

	// Claim once, and once only.
	if ok, err := LinkAdClick("tok-1", 42); err != nil || !ok {
		t.Fatalf("link = %v, %v; want true, nil", ok, err)
	}
	if ok, err := LinkAdClick("tok-1", 43); err != nil || ok {
		t.Errorf("second link = %v, %v; want false, nil", ok, err)
	}
	if ok, err := LinkAdClick("nope", 44); err != nil || ok {
		t.Errorf("link of unknown token = %v, %v; want false, nil", ok, err)
	}

	byBooking, err := GetAdClickByBooking(42)
	if err != nil {
		t.Fatalf("by booking: %v", err)
	}
	if byBooking == nil || byBooking.Token != "tok-1" {
		t.Fatalf("by booking = %+v, want tok-1", byBooking)
	}

	// A booking that never came from an ad, and the zero id, are both blanks.
	for _, id := range []int64{0, 99} {
		if c, err := GetAdClickByBooking(id); err != nil || c != nil {
			t.Errorf("by booking %d = %+v, %v; want nil, nil", id, c, err)
		}
	}

	n, err := CountAdClicksSince(time.Now().UTC().AddDate(0, 0, -1))
	if err != nil || n != 2 {
		t.Errorf("CountAdClicksSince = %d, %v; want 2, nil", n, err)
	}
	if n, err := CountAdClicksSince(time.Now().UTC().AddDate(0, 0, 1)); err != nil || n != 0 {
		t.Errorf("CountAdClicksSince (future) = %d, %v; want 0, nil", n, err)
	}
}

// TestSourceReporting pins the two numbers the dashboard shows, and the gap
// between them: revenue that no booking can explain.
func TestSourceReporting(t *testing.T) {
	openTestDB(t)

	newBooking := func(name, source, status string) int64 {
		t.Helper()
		id, err := InsertBooking(&Booking{Name: name, Suburb: "Donvale", ServiceSlug: "computer-help", Source: source})
		if err != nil {
			t.Fatalf("insert booking: %v", err)
		}
		if status != "" {
			if err := UpdateBookingStatus(id, status); err != nil {
				t.Fatalf("status: %v", err)
			}
		}
		return id
	}
	paidInvoice := func(bookingID int64, cents int64, status string) {
		t.Helper()
		items := []InvoiceItem{{Description: "Visit", Qty: 1, UnitCents: cents, LineCents: cents}}
		id, err := CreateInvoice(bookingID, 0, Today(), "", "tok-"+status+strconv.FormatInt(bookingID, 10)+strconv.Itoa(len(items)), items)
		if err != nil {
			t.Fatalf("invoice: %v", err)
		}
		if status == InvoicePaid {
			if ok, err := MarkInvoicePaid(id, Today(), PayCash, ""); err != nil || !ok {
				t.Fatalf("mark paid: %v, %v", ok, err)
			}
		}
	}

	ads := newBooking("Ann", SourceGoogleAds, "")
	newBooking("Bob", SourceGoogleAds, "")
	search := newBooking("Cal", SourceGoogleSearch, "")
	newBooking("Spammer", SourceGoogleAds, BookingSpam)
	legacy := newBooking("Old", "", "") // pre-migration row

	paidInvoice(ads, 22000, InvoicePaid)
	paidInvoice(ads, 8000, InvoicePaid) // same job, second invoice
	paidInvoice(search, 15000, "")      // draft: not revenue yet
	paidInvoice(legacy, 5000, InvoicePaid)
	paidInvoice(0, 9000, InvoicePaid) // no booking behind it at all

	counts, err := CountBookingsBySource()
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	// Spam came from "somewhere", but it isn't work and isn't counted.
	if counts[SourceGoogleAds] != 2 || counts[SourceGoogleSearch] != 1 || counts[""] != 1 {
		t.Errorf("CountBookingsBySource = %v", counts)
	}

	rows, err := SumPaidBySource()
	if err != nil {
		t.Fatalf("paid by source: %v", err)
	}
	if len(rows) != 2 || rows[0].Source != SourceGoogleAds {
		t.Fatalf("SumPaidBySource = %+v, want Google Ads first", rows)
	}
	// Two invoices against one booking is still one job.
	if rows[0].Jobs != 1 || rows[0].Cents != 30000 {
		t.Errorf("ads row = %+v, want 1 job and 30000 cents", rows[0])
	}
	if rows[1].Source != "" || rows[1].Cents != 5000 {
		t.Errorf("legacy row = %+v, want the pre-migration row at 5000 cents", rows[1])
	}

	total, err := SumPaidCents()
	if err != nil {
		t.Fatalf("paid total: %v", err)
	}
	var attributed int64
	for _, r := range rows {
		attributed += r.Cents
	}
	// The invoice with no booking is exactly the gap the dashboard labels
	// "Unattributed". If the CAST ever falls out of these queries, the money
	// stops matching here first.
	if total != 44000 || total-attributed != 9000 {
		t.Errorf("total = %d, unattributed = %d; want 44000 and 9000", total, total-attributed)
	}
}
