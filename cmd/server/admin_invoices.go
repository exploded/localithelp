package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"localithelp/db"
)

// ── Invoices ──

// invoiceView bundles everything the invoice page, email and PDF need.
type invoiceView struct {
	Inv          *db.Invoice
	Items        []db.InvoiceItem
	Cust         *db.Customer
	Booking      *db.Booking
	ServiceTitle string
	PublicURL    string
}

func loadInvoiceView(inv *db.Invoice) (*invoiceView, error) {
	v := &invoiceView{Inv: inv, PublicURL: site.BaseURL + "/invoice/" + inv.ViewToken}
	var err error
	if v.Items, err = db.ListInvoiceItems(inv.ID); err != nil {
		return nil, err
	}
	if inv.CustomerID != 0 {
		if v.Cust, err = db.GetCustomer(inv.CustomerID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	if inv.BookingID != 0 {
		if v.Booking, err = db.GetBooking(inv.BookingID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if v.Booking != nil {
			if s, ok := findService(v.Booking.ServiceSlug); ok {
				v.ServiceTitle = s.Title
			}
		}
	}
	return v, nil
}

func loadInvoice(w http.ResponseWriter, r *http.Request) *db.Invoice {
	inv, err := db.GetInvoice(pathID(r))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			log.Printf("admin invoice: %v", err)
			http.Error(w, "failed to load invoice", http.StatusInternalServerError)
		}
		return nil
	}
	return inv
}

// invoiceRow decorates an invoice for lists.
type invoiceRow struct {
	db.Invoice
	Ref         string
	CustName    string
	Issued      string
	Due         string
	Paid        string
	ReviewAsked string
}

func invoiceRows(invs []db.Invoice) []invoiceRow {
	cache := map[int64]string{}
	out := make([]invoiceRow, len(invs))
	for i, inv := range invs {
		name, ok := cache[inv.CustomerID]
		if !ok && inv.CustomerID != 0 {
			if c, err := db.GetCustomer(inv.CustomerID); err == nil {
				name = c.Name
			}
			cache[inv.CustomerID] = name
		}
		out[i] = invoiceRow{Invoice: inv, Ref: inv.Ref(), CustName: name, Issued: fmtDate(inv.IssuedAt), Due: fmtDate(inv.DueAt),
			Paid: fmtDate(inv.PaidAt), ReviewAsked: fmtDate(inv.ReviewAskedAt)}
	}
	return out
}

type adminInvoicesData struct {
	Flash    flash
	Status   string
	Statuses []string
	Rows     []invoiceRow
	TotalOut int64
}

func handleAdminInvoices(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	invs, err := db.ListInvoices(status)
	if err != nil {
		log.Printf("admin invoices: %v", err)
		http.Error(w, "failed to load invoices", http.StatusInternalServerError)
		return
	}
	out, _ := db.SumOutstandingCents()
	render(w, r, "admin-invoices", adminInvoicesData{
		Flash: readFlash(r), Status: status, Statuses: []string{db.InvoiceDraft, db.InvoiceSent, db.InvoicePaid, db.InvoiceVoid},
		Rows: invoiceRows(invs), TotalOut: out,
	})
}

// handleAdminInvoiceNew creates a draft and opens it for editing. It works from
// a booking (prefilled from the booking's service and duration) or from a
// customer alone, for work that never had a booking — a software job, say.
func handleAdminInvoiceNew(w http.ResponseWriter, r *http.Request) {
	bookingID, _ := strconv.ParseInt(r.FormValue("booking"), 10, 64)
	customerID, _ := strconv.ParseInt(r.FormValue("customer"), 10, 64)
	back := "/admin/bookings"
	if bookingID == 0 {
		back = "/admin/customers"
		if customerID != 0 {
			back += "/" + strconv.FormatInt(customerID, 10)
		}
	}
	var b *db.Booking
	if bookingID != 0 {
		var err error
		if b, err = db.GetBooking(bookingID); err != nil {
			http.NotFound(w, r)
			return
		}
		customerID = b.CustomerID
	}
	if customerID == 0 {
		redirectMsg(w, r, back, "err", "The booking has no customer linked, so an invoice can't be created.")
		return
	}
	// Reuse an existing draft for the same booking rather than minting numbers.
	if bookingID != 0 {
		if existing, err := db.ListInvoicesByBooking(bookingID); err == nil {
			for _, e := range existing {
				if e.Status == db.InvoiceDraft {
					http.Redirect(w, r, "/admin/invoices/"+strconv.FormatInt(e.ID, 10), http.StatusSeeOther)
					return
				}
			}
		}
	}
	items := seedInvoiceItems(b, r.FormValue("kind"))
	id, err := db.CreateInvoice(bookingID, customerID, db.Today().AddDate(0, 0, 7), "", generateSessionToken(), items)
	if err != nil {
		log.Printf("create invoice: %v", err)
		redirectMsg(w, r, back, "err", "Could not create the invoice.")
		return
	}
	redirectMsg(w, r, "/admin/invoices/"+strconv.FormatInt(id, 10), "ok", "Draft created — check the lines, add any hardware, then send.")
}

// seedInvoiceItems prefills a new draft. kind comes from the New invoice button
// and wins when set; otherwise it follows the booking's service, so a software
// job is billed at the hourly rate rather than as a visit plus 15-minute blocks.
// A booking-less draft starts empty — handleAdminInvoice always offers blank
// rows, so there is nothing to delete before typing the real lines.
func seedInvoiceItems(b *db.Booking, kind string) []db.InvoiceItem {
	if kind == "" {
		switch {
		case b == nil:
			kind = "blank"
		case b.ServiceSlug == "software-development":
			kind = "software"
		default:
			kind = "onsite"
		}
	}
	switch kind {
	case "blank":
		return nil
	case "software":
		return []db.InvoiceItem{
			{Description: "Software development", Qty: 1, UnitCents: softwareHourlyDollars * 100},
		}
	}
	units := 4
	if b != nil && b.DurationMin > 0 {
		units = (b.DurationMin + calSlotMin - 1) / calSlotMin
	}
	return []db.InvoiceItem{
		{Description: "Visit fee — includes travel", Qty: 1, UnitCents: int64(site.OnsiteFee) * 100},
		{Description: fmt.Sprintf("Labour — %d × 15 min", units), Qty: float64(units), UnitCents: int64(site.BlockRate) * 100},
	}
}

type adminInvoiceData struct {
	Flash          flash
	V              *invoiceView
	Ref            string
	Total          string
	TotalDollars   string // "129.50" for the Zeller amount copy button
	Editable       bool
	CanSend        bool
	CanPay         bool
	CanVoid        bool
	CanResend      bool
	CanResendRcpt  bool // paid: re-email the receipt (PAID-stamped PDF)
	Lines          []invoiceLine
	Blank          int // extra blank rows for the editor
	DueValue       string
	PaidValue      string
	Methods        []struct{ Value, Label string }
	When           string
	BlockRate      string
	OnsiteFee      string
	SeniorsPct     string
	PaymentMethod  string
	HasCustEmail   bool
	SentEmailNote  string
	PublicURL      string
	Statuses       []string
	BankConfigured bool
	ReviewOn       bool   // REVIEW_URL configured: show the review tick-box
	ReviewAsked    string // date a review was requested, empty if never
}

// invoiceLine is an editable row (dollars as strings for the form).
type invoiceLine struct {
	Description string
	Qty         string
	Unit        string // bare dollars for the editor input, e.g. "-58.00"
	UnitFmt     string // display form for read-only views, e.g. "-$58.00"
	Line        string
}

func handleAdminInvoice(w http.ResponseWriter, r *http.Request) {
	inv := loadInvoice(w, r)
	if inv == nil {
		return
	}
	v, err := loadInvoiceView(inv)
	if err != nil {
		log.Printf("invoice view: %v", err)
		http.Error(w, "failed to load invoice", http.StatusInternalServerError)
		return
	}
	d := adminInvoiceData{
		Flash: readFlash(r), V: v, Ref: inv.Ref(), Total: fmtCents(inv.TotalCents),
		TotalDollars:   strings.ReplaceAll(strings.ReplaceAll(fmtCents(inv.TotalCents), ",", ""), "$", ""),
		Editable:       inv.Status == db.InvoiceDraft,
		CanSend:        inv.Status == db.InvoiceDraft,
		CanResend:      inv.Status == db.InvoiceSent,
		CanResendRcpt:  inv.Status == db.InvoicePaid,
		CanPay:         inv.Status == db.InvoiceDraft || inv.Status == db.InvoiceSent,
		CanVoid:        inv.Status == db.InvoiceDraft || inv.Status == db.InvoiceSent,
		DueValue:       db.FormatDate(inv.DueAt),
		PaidValue:      db.FormatDate(db.Today()),
		Methods:        db.PaymentMethods,
		BlockRate:      strconv.Itoa(site.BlockRate),
		OnsiteFee:      strconv.Itoa(site.OnsiteFee),
		SeniorsPct:     strconv.Itoa(site.SeniorsPct),
		PaymentMethod:  db.PaymentMethodLabel(inv.PaymentMethod),
		HasCustEmail:   v.Cust != nil && v.Cust.Email != "",
		PublicURL:      v.PublicURL,
		BankConfigured: site.BankBSB != "",
		ReviewOn:       site.ReviewURL != "",
		ReviewAsked:    fmtDate(inv.ReviewAskedAt),
	}
	if v.Booking != nil {
		d.When = fmtWhen(v.Booking.StartAt)
	}
	for _, it := range v.Items {
		d.Lines = append(d.Lines, invoiceLine{
			Description: it.Description, Qty: fmtQty(it.Qty),
			Unit:    strings.ReplaceAll(strings.ReplaceAll(fmtCents(it.UnitCents), ",", ""), "$", ""),
			UnitFmt: fmtCents(it.UnitCents),
			Line:    fmtCents(db.LineCents(it.Qty, it.UnitCents)),
		})
	}
	if d.Editable {
		d.Blank = 3
	}
	render(w, r, "admin-invoice", d)
}

// parseInvoiceItems reads the parallel desc[]/qty[]/unit[] form arrays.
func parseInvoiceItems(r *http.Request) ([]db.InvoiceItem, error) {
	descs, qtys, units := r.Form["desc"], r.Form["qty"], r.Form["unit"]
	var items []db.InvoiceItem
	for i, desc := range descs {
		desc = strings.TrimSpace(desc)
		var qtyS, unitS string
		if i < len(qtys) {
			qtyS = strings.TrimSpace(qtys[i])
		}
		if i < len(units) {
			unitS = strings.TrimSpace(units[i])
		}
		if desc == "" && unitS == "" { // untouched blank row (qty defaults to 1)
			continue
		}
		if desc == "" {
			return nil, fmt.Errorf("line %d needs a description", i+1)
		}
		qty := 1.0
		if qtyS != "" {
			q, err := strconv.ParseFloat(qtyS, 64)
			if err != nil || q < 0 || q > 10000 {
				return nil, fmt.Errorf("line %d has an invalid quantity", i+1)
			}
			qty = q
		}
		unit, err := dollarsToCents(unitS)
		if err != nil {
			return nil, fmt.Errorf("line %d has an invalid unit price", i+1)
		}
		items = append(items, db.InvoiceItem{Description: desc, Qty: qty, UnitCents: unit})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("add at least one line")
	}
	return items, nil
}

// validPaymentLink accepts only https URLs (html/template would otherwise
// neuter the href, and we never want javascript: here).
func validPaymentLink(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", true
	}
	if !strings.HasPrefix(s, "https://") || strings.ContainsAny(s, " \"'<>") || len(s) > 500 {
		return "", false
	}
	return s, true
}

func handleAdminInvoiceItems(w http.ResponseWriter, r *http.Request) {
	inv := loadInvoice(w, r)
	if inv == nil {
		return
	}
	back := "/admin/invoices/" + strconv.FormatInt(inv.ID, 10)
	if inv.Status != db.InvoiceDraft {
		redirectMsg(w, r, back, "err", "Only draft invoices can be edited.")
		return
	}
	items, err := parseInvoiceItems(r)
	if err != nil {
		redirectMsg(w, r, back, "err", err.Error())
		return
	}
	link, ok := validPaymentLink(r.FormValue("payment_link"))
	if !ok {
		redirectMsg(w, r, back, "err", "The payment link must be an https:// URL.")
		return
	}
	due := db.ParseDate(r.FormValue("due"))
	notes := strings.TrimSpace(r.FormValue("notes"))
	if len(notes) > 2000 {
		notes = notes[:2000]
	}
	if err := db.SaveInvoiceDraft(inv.ID, due, notes, link, items); err != nil {
		log.Printf("save invoice #%d: %v", inv.ID, err)
		redirectMsg(w, r, back, "err", "Could not save the invoice.")
		return
	}
	redirectMsg(w, r, back, "ok", "Saved.")
}

func handleAdminInvoiceLink(w http.ResponseWriter, r *http.Request) {
	inv := loadInvoice(w, r)
	if inv == nil {
		return
	}
	back := "/admin/invoices/" + strconv.FormatInt(inv.ID, 10)
	link, ok := validPaymentLink(r.FormValue("payment_link"))
	if !ok {
		redirectMsg(w, r, back, "err", "The payment link must be an https:// URL.")
		return
	}
	if err := db.SetInvoicePaymentLink(inv.ID, link); err != nil {
		redirectMsg(w, r, back, "err", "Could not save the link.")
		return
	}
	redirectMsg(w, r, back, "ok", "Payment link saved.")
}

// handleAdminInvoiceSend emails the invoice PDF. Draft → sent (state flips
// first, guarded, so a double-click can't send twice); sent → resend;
// paid → resend the receipt (the PAID-stamped PDF).
func handleAdminInvoiceSend(w http.ResponseWriter, r *http.Request) {
	inv := loadInvoice(w, r)
	if inv == nil {
		return
	}
	back := "/admin/invoices/" + strconv.FormatInt(inv.ID, 10)
	if inv.Status == db.InvoicePaid {
		resendReceipt(w, r, inv, back)
		return
	}
	switch inv.Status {
	case db.InvoiceDraft:
		if inv.PaymentLink == "" && r.FormValue("no_link_ok") == "" && site.BankBSB == "" {
			redirectMsg(w, r, back, "err", "Paste the Zeller payment link (or tick “send without a payment link”) first.")
			return
		}
		if inv.TotalCents <= 0 {
			redirectMsg(w, r, back, "err", "The invoice total is zero — add lines first.")
			return
		}
		due := inv.DueAt
		if due.IsZero() {
			due = db.Today().AddDate(0, 0, 7)
		}
		ok, err := db.MarkInvoiceSent(inv.ID, db.Today(), due)
		if err != nil {
			log.Printf("mark sent #%d: %v", inv.ID, err)
			redirectMsg(w, r, back, "err", "Could not update the invoice.")
			return
		}
		if !ok {
			redirectMsg(w, r, back, "err", "This invoice was already sent.")
			return
		}
		if inv.BookingID != 0 {
			if err := db.UpdateBookingStatus(inv.BookingID, db.BookingInvoiced); err != nil {
				log.Printf("booking #%d → invoiced: %v", inv.BookingID, err)
			}
			syncBookingSoon(inv.BookingID)
		}
	case db.InvoiceSent:
		// resend
	default:
		redirectMsg(w, r, back, "err", "Only draft or sent invoices can be emailed.")
		return
	}
	inv, _ = db.GetInvoice(inv.ID)
	v, err := loadInvoiceView(inv)
	if err != nil {
		redirectMsg(w, r, back, "err", "Marked as sent, but the invoice could not be loaded for emailing.")
		return
	}
	pdf, err := invoicePDF(v)
	if err != nil {
		log.Printf("invoice pdf #%d: %v", inv.ID, err)
		redirectMsg(w, r, back, "err", "Marked as sent, but the PDF failed to render: "+err.Error())
		return
	}
	if err := sendInvoiceEmail(v, pdf); err != nil {
		logMailErr("invoice send", err)
		redirectMsg(w, r, back, "err", "Marked as sent, but the email failed: "+err.Error()+" — fix and use Resend.")
		return
	}
	redirectMsg(w, r, back, "ok", inv.Ref()+" emailed to "+v.Cust.Email+".")
}

// resendReceipt re-emails the receipt for a paid invoice (same email and
// PAID-stamped PDF as the one sent when it was marked paid), optionally adding
// the Google review ask if it wasn't included the first time.
func resendReceipt(w http.ResponseWriter, r *http.Request, inv *db.Invoice, back string) {
	v, err := loadInvoiceView(inv)
	if err != nil {
		redirectMsg(w, r, back, "err", "The invoice could not be loaded for emailing.")
		return
	}
	if v.Cust == nil || v.Cust.Email == "" {
		redirectMsg(w, r, back, "err", "The customer has no email address.")
		return
	}
	pdf, err := invoicePDF(v)
	if err != nil {
		log.Printf("receipt pdf #%d: %v", inv.ID, err)
		redirectMsg(w, r, back, "err", "The PDF failed to render: "+err.Error())
		return
	}
	askReview := wantsReviewAsk(r)
	if err := sendReceiptEmail(v, pdf, askReview); err != nil {
		logMailErr("receipt resend", err)
		redirectMsg(w, r, back, "err", "The receipt email failed: "+err.Error())
		return
	}
	msg := "Receipt for " + inv.Ref() + " emailed to " + v.Cust.Email + "."
	if askReview {
		recordReviewAsk(inv.ID)
		msg += " Review request included."
	}
	redirectMsg(w, r, back, "ok", msg)
}

func handleAdminInvoicePaid(w http.ResponseWriter, r *http.Request) {
	inv := loadInvoice(w, r)
	if inv == nil {
		return
	}
	back := "/admin/invoices/" + strconv.FormatInt(inv.ID, 10)
	method := r.FormValue("method")
	if db.PaymentMethodLabel(method) == method && method != db.PayOther { // unknown value
		redirectMsg(w, r, back, "err", "Choose how it was paid.")
		return
	}
	paidAt := db.ParseDate(r.FormValue("paid_at"))
	if paidAt.IsZero() {
		paidAt = db.Today()
	}
	ref := strings.TrimSpace(r.FormValue("ref"))
	if len(ref) > 100 {
		ref = ref[:100]
	}
	ok, err := db.MarkInvoicePaid(inv.ID, paidAt, method, ref)
	if err != nil {
		log.Printf("mark paid #%d: %v", inv.ID, err)
		redirectMsg(w, r, back, "err", "Could not update the invoice.")
		return
	}
	if !ok {
		redirectMsg(w, r, back, "err", "This invoice can't be marked paid (already paid or void).")
		return
	}
	if inv.BookingID != 0 {
		if err := db.UpdateBookingStatus(inv.BookingID, db.BookingPaid); err != nil {
			log.Printf("booking #%d → paid: %v", inv.BookingID, err)
		}
		syncBookingSoon(inv.BookingID)
	}
	msg := inv.Ref() + " marked paid."
	if r.FormValue("notify") != "" {
		askReview := wantsReviewAsk(r)
		inv, _ = db.GetInvoice(inv.ID)
		v, err := loadInvoiceView(inv)
		if err == nil {
			var pdf []byte
			if pdf, err = invoicePDF(v); err == nil {
				err = sendReceiptEmail(v, pdf, askReview)
			}
		}
		if err != nil {
			logMailErr("receipt", err)
			redirectMsg(w, r, back, "err", msg+" But the receipt email failed: "+err.Error())
			return
		}
		msg += " Receipt emailed."
		if askReview {
			recordReviewAsk(inv.ID)
			msg += " Review request included."
		}
	}
	redirectMsg(w, r, back, "ok", msg)
}

// wantsReviewAsk reports whether the admin ticked "ask for a Google review" and
// there is somewhere to send them.
func wantsReviewAsk(r *http.Request) bool {
	return r.FormValue("review") != "" && site.ReviewURL != ""
}

// recordReviewAsk stamps the invoice so the admin can see who has already been
// asked. A failure here must not turn a successful send into an error.
func recordReviewAsk(id int64) {
	if err := db.MarkInvoiceReviewAsked(id, db.Today()); err != nil {
		log.Printf("mark review asked #%d: %v", id, err)
	}
}

func handleAdminInvoiceVoid(w http.ResponseWriter, r *http.Request) {
	inv := loadInvoice(w, r)
	if inv == nil {
		return
	}
	back := "/admin/invoices/" + strconv.FormatInt(inv.ID, 10)
	ok, err := db.VoidInvoice(inv.ID)
	if err != nil || !ok {
		redirectMsg(w, r, back, "err", "This invoice can't be voided.")
		return
	}
	// A voided invoice leaves the booking as "done" so it can be re-invoiced.
	if inv.BookingID != 0 && inv.Status == db.InvoiceSent {
		_ = db.UpdateBookingStatus(inv.BookingID, db.BookingDone)
		syncBookingSoon(inv.BookingID)
	}
	redirectMsg(w, r, back, "ok", inv.Ref()+" voided.")
}

func servePDF(w http.ResponseWriter, inv *db.Invoice, v *invoiceView) {
	pdf, err := invoicePDF(v)
	if err != nil {
		log.Printf("pdf %s: %v", inv.Ref(), err)
		http.Error(w, "failed to render PDF", http.StatusInternalServerError)
		return
	}
	name := inv.Ref() + ".pdf"
	if inv.Status == db.InvoicePaid {
		name = inv.Ref() + "-receipt.pdf"
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Write(pdf)
}

func handleAdminInvoicePDF(w http.ResponseWriter, r *http.Request) {
	inv := loadInvoice(w, r)
	if inv == nil {
		return
	}
	v, err := loadInvoiceView(inv)
	if err != nil {
		http.Error(w, "failed to load invoice", http.StatusInternalServerError)
		return
	}
	servePDF(w, inv, v)
}

// ── Public tokenised views ──

func loadInvoiceByToken(w http.ResponseWriter, r *http.Request) *db.Invoice {
	tok := r.PathValue("token")
	if len(tok) != 64 {
		http.NotFound(w, r)
		return nil
	}
	inv, err := db.GetInvoiceByToken(tok)
	if err != nil {
		http.NotFound(w, r)
		return nil
	}
	return inv
}

type invoicePublicData struct {
	V       *invoiceView
	Ref     string
	Total   string
	Lines   []invoiceLine
	Issued  string
	Due     string
	Paid    string
	Method  string
	Paidish bool
	Void    bool
	When    string
}

func handleInvoicePublic(w http.ResponseWriter, r *http.Request) {
	inv := loadInvoiceByToken(w, r)
	if inv == nil {
		return
	}
	v, err := loadInvoiceView(inv)
	if err != nil {
		http.Error(w, "failed to load invoice", http.StatusInternalServerError)
		return
	}
	d := invoicePublicData{
		V: v, Ref: inv.Ref(), Total: fmtCents(inv.TotalCents), Issued: fmtDate(inv.IssuedAt), Due: fmtDate(inv.DueAt),
		Paid: fmtDate(inv.PaidAt), Method: db.PaymentMethodLabel(inv.PaymentMethod),
		Paidish: inv.Status == db.InvoicePaid, Void: inv.Status == db.InvoiceVoid,
	}
	if v.Booking != nil {
		d.When = fmtWhen(v.Booking.StartAt)
	}
	for _, it := range v.Items {
		d.Lines = append(d.Lines, invoiceLine{
			Description: it.Description, Qty: fmtQty(it.Qty), Unit: fmtCents(it.UnitCents), Line: fmtCents(db.LineCents(it.Qty, it.UnitCents)),
		})
	}
	w.Header().Set("X-Robots-Tag", "noindex")
	render(w, r, "invoice-public", d)
}

func handleInvoicePublicPDF(w http.ResponseWriter, r *http.Request) {
	inv := loadInvoiceByToken(w, r)
	if inv == nil {
		return
	}
	v, err := loadInvoiceView(inv)
	if err != nil {
		http.Error(w, "failed to load invoice", http.StatusInternalServerError)
		return
	}
	servePDF(w, inv, v)
}

// ── Customers ──

type adminCustomersData struct {
	Flash flash
	Query string
	Rows  []db.Customer
}

func handleAdminCustomers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	rows, err := db.ListCustomers(q)
	if err != nil {
		log.Printf("admin customers: %v", err)
		http.Error(w, "failed to load customers", http.StatusInternalServerError)
		return
	}
	render(w, r, "admin-customers", adminCustomersData{Flash: readFlash(r), Query: q, Rows: rows})
}

type adminCustomerData struct {
	Flash          flash
	C              *db.Customer
	Bookings       []bookingRow
	Invoices       []invoiceRow
	Services       []Service
	SoftwareHourly string // label for the New invoice "software" option
}

func handleAdminCustomer(w http.ResponseWriter, r *http.Request) {
	c, err := db.GetCustomer(pathID(r))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	bs, _ := db.ListBookingsByCustomer(c.ID)
	invs, _ := db.ListInvoicesByCustomer(c.ID)
	render(w, r, "admin-customer", adminCustomerData{Flash: readFlash(r), C: c, Bookings: bookingRows(bs), Invoices: invoiceRows(invs),
		Services: services, SoftwareHourly: softwareHourly})
}

// handleAdminCustomerBooking creates a new unscheduled booking for the
// customer (e.g. a phone enquiry) and sends the admin to it to pick a time.
func handleAdminCustomerBooking(w http.ResponseWriter, r *http.Request) {
	c, err := db.GetCustomer(pathID(r))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := "/admin/customers/" + strconv.FormatInt(c.ID, 10)
	slug := strings.TrimSpace(r.FormValue("service"))
	if _, ok := findService(slug); !ok {
		slug = ""
	}
	issue := strings.TrimSpace(r.FormValue("issue"))
	if rs := []rune(issue); len(rs) > 5000 {
		issue = string(rs[:5000])
	}
	if issue == "" {
		issue = "Booking created by admin"
	}
	id, err := db.CreateForCustomer(c, slug, issue)
	if err != nil {
		log.Printf("new booking for customer #%d: %v", c.ID, err)
		redirectMsg(w, r, back, "err", "Could not create the booking.")
		return
	}
	redirectMsg(w, r, "/admin/bookings/"+strconv.FormatInt(id, 10), "ok", "Booking created — pick a time below.")
}

func handleAdminCustomerSave(w http.ResponseWriter, r *http.Request) {
	c, err := db.GetCustomer(pathID(r))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := "/admin/customers/" + strconv.FormatInt(c.ID, 10)
	get := func(k string, max int) string {
		v := strings.TrimSpace(r.FormValue(k))
		if len(v) > max {
			v = v[:max]
		}
		return v
	}
	c.Name, c.Email, c.Phone = get("name", 120), get("email", 120), get("phone", 40)
	c.Address, c.Suburb, c.Notes = get("address", 200), get("suburb", 80), get("notes", 5000)
	if c.Email != "" && !validEmail(c.Email) {
		redirectMsg(w, r, back, "err", "That email address doesn't look valid.")
		return
	}
	if err := db.UpdateCustomer(c); err != nil {
		log.Printf("update customer #%d: %v", c.ID, err)
		redirectMsg(w, r, back, "err", "Could not save — is that email already used by another customer?")
		return
	}
	redirectMsg(w, r, back, "ok", "Customer saved.")
}
