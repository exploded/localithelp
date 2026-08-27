package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"

	"localithelp/db"
)

// logoPNG is a copy of static/img/icon-512.png, embedded so the PDF renders
// the logo regardless of working directory. tools/genassets regenerates both.
//
//go:embed logo.png
var logoPNG []byte

// invoicePDF renders an A4 invoice (or receipt, when paid) with fpdf's built-in
// Helvetica. Non-Latin-1 characters are folded by the cp1252 translator so
// names with accents still print; anything unmappable degrades to '?'.
func invoicePDF(v *invoiceView) ([]byte, error) {
	inv := v.Inv
	paid := inv.Status == db.InvoicePaid
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetTitle(inv.Ref(), true)
	pdf.SetAuthor("Local IT Help", true)
	pdf.SetMargins(20, 18, 20)
	pdf.SetAutoPageBreak(true, 20)
	pdf.AddPage()
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	const (
		w         = 170.0 // printable width
		footerTop = 267.0 // y of the page footer; content must stay above it
	)

	// ── Header: logo + business (left) / document title (right) ──
	y0 := pdf.GetY()
	const logoSize = 13.0
	pdf.RegisterImageOptionsReader("logo", fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(logoPNG))
	pdf.ImageOptions("logo", 20, y0, logoSize, logoSize, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	pdf.SetLeftMargin(20 + logoSize + 5)
	pdf.SetX(20 + logoSize + 5)
	pdf.SetFont("Helvetica", "B", 15)
	pdf.CellFormat(100, 7, "LOCAL IT HELP", "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(90, 90, 90)
	pdf.CellFormat(100, 4.5, "James McHugh - local computer help, Donvale VIC", "", 1, "L", false, 0, "")
	if site.ABN != "" {
		pdf.CellFormat(100, 4.5, "ABN "+site.ABN, "", 1, "L", false, 0, "")
	}
	pdf.CellFormat(100, 4.5, site.Email, "", 1, "L", false, 0, "")
	if site.Phone != "" {
		pdf.CellFormat(100, 4.5, site.Phone, "", 1, "L", false, 0, "")
	}
	pdf.CellFormat(100, 4.5, strings.TrimPrefix(strings.TrimPrefix(site.BaseURL, "https://"), "http://"), "", 1, "L", false, 0, "")
	pdf.SetLeftMargin(20)
	yLeft := pdf.GetY()

	pdf.SetXY(120, y0)
	pdf.SetTextColor(28, 28, 28)
	pdf.SetFont("Helvetica", "B", 22)
	title := "INVOICE"
	if paid {
		title = "RECEIPT"
	}
	pdf.CellFormat(70, 10, title, "", 2, "R", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetX(120)
	pdf.CellFormat(70, 5, inv.Ref(), "", 2, "R", false, 0, "")
	pdf.SetX(120)
	if !inv.IssuedAt.IsZero() {
		pdf.CellFormat(70, 5, "Issued "+fmtDate(inv.IssuedAt), "", 2, "R", false, 0, "")
		pdf.SetX(120)
	}
	if paid {
		pdf.SetTextColor(30, 120, 60)
		pdf.SetFont("Helvetica", "B", 10)
		pdf.CellFormat(70, 5, "PAID "+fmtDate(inv.PaidAt), "", 2, "R", false, 0, "")
		pdf.SetTextColor(28, 28, 28)
		pdf.SetFont("Helvetica", "", 10)
	} else if !inv.DueAt.IsZero() {
		pdf.CellFormat(70, 5, "Due "+fmtDate(inv.DueAt), "", 2, "R", false, 0, "")
	} else if inv.Status == db.InvoiceVoid {
		pdf.CellFormat(70, 5, "VOID", "", 2, "R", false, 0, "")
	}
	if pdf.GetY() < yLeft {
		pdf.SetY(yLeft)
	}
	pdf.Ln(6)
	pdf.SetDrawColor(220, 217, 210)
	pdf.Line(20, pdf.GetY(), 20+w, pdf.GetY())
	pdf.Ln(6)

	// ── Bill to / job ──
	yTop := pdf.GetY()
	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetTextColor(110, 110, 110)
	pdf.CellFormat(85, 5, "BILL TO", "", 1, "L", false, 0, "")
	pdf.SetTextColor(28, 28, 28)
	pdf.SetFont("Helvetica", "", 10)
	if v.Cust != nil {
		if v.Cust.Name != "" {
			pdf.CellFormat(85, 5, tr(v.Cust.Name), "", 1, "L", false, 0, "")
		}
		if v.Cust.Address != "" {
			pdf.MultiCell(85, 5, tr(v.Cust.Address), "", "L", false)
		}
		if v.Cust.Suburb != "" && !strings.Contains(strings.ToLower(v.Cust.Address), strings.ToLower(v.Cust.Suburb)) {
			pdf.CellFormat(85, 5, tr(v.Cust.Suburb), "", 1, "L", false, 0, "")
		}
		if v.Cust.Email != "" {
			pdf.CellFormat(85, 5, tr(v.Cust.Email), "", 1, "L", false, 0, "")
		}
		if v.Cust.Phone != "" {
			pdf.CellFormat(85, 5, tr(v.Cust.Phone), "", 1, "L", false, 0, "")
		}
	}
	yBill := pdf.GetY()
	if v.Booking != nil {
		pdf.SetXY(110, yTop)
		pdf.SetFont("Helvetica", "B", 9)
		pdf.SetTextColor(110, 110, 110)
		pdf.CellFormat(80, 5, "JOB", "", 2, "L", false, 0, "")
		pdf.SetTextColor(28, 28, 28)
		pdf.SetFont("Helvetica", "", 10)
		pdf.SetX(110)
		if v.ServiceTitle != "" {
			pdf.CellFormat(80, 5, tr(v.ServiceTitle), "", 2, "L", false, 0, "")
			pdf.SetX(110)
		}
		if !v.Booking.StartAt.IsZero() {
			pdf.CellFormat(80, 5, tr(fmtWhen(v.Booking.StartAt)), "", 2, "L", false, 0, "")
			pdf.SetX(110)
		}
		if v.Booking.Suburb != "" {
			pdf.CellFormat(80, 5, tr(v.Booking.Suburb), "", 2, "L", false, 0, "")
		}
		if pdf.GetY() > yBill {
			yBill = pdf.GetY()
		}
	}
	pdf.SetY(yBill)
	pdf.Ln(8)

	// ── Items table ──
	colDesc, colQty, colUnit, colAmt := 95.0, 20.0, 27.0, 28.0
	pdf.SetFillColor(246, 245, 242)
	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetTextColor(90, 90, 90)
	pdf.CellFormat(colDesc, 7, "Description", "B", 0, "L", true, 0, "")
	pdf.CellFormat(colQty, 7, "Qty", "B", 0, "R", true, 0, "")
	pdf.CellFormat(colUnit, 7, "Unit", "B", 0, "R", true, 0, "")
	pdf.CellFormat(colAmt, 7, "Amount", "B", 1, "R", true, 0, "")
	pdf.SetTextColor(28, 28, 28)
	pdf.SetFont("Helvetica", "", 10)
	for _, it := range v.Items {
		x, y := pdf.GetX(), pdf.GetY()
		pdf.MultiCell(colDesc, 6.5, tr(it.Description), "", "L", false)
		h := pdf.GetY() - y
		if h < 6.5 {
			h = 6.5
		}
		pdf.SetXY(x+colDesc, y)
		pdf.CellFormat(colQty, h, fmtQty(it.Qty), "", 0, "R", false, 0, "")
		pdf.CellFormat(colUnit, h, fmtCents(it.UnitCents), "", 0, "R", false, 0, "")
		pdf.CellFormat(colAmt, h, fmtCents(db.LineCents(it.Qty, it.UnitCents)), "", 1, "R", false, 0, "")
		pdf.SetDrawColor(235, 233, 228)
		pdf.Line(20, pdf.GetY(), 20+w, pdf.GetY())
	}
	pdf.Ln(2)
	pdf.SetFont("Helvetica", "B", 12)
	pdf.CellFormat(colDesc+colQty, 9, "", "", 0, "L", false, 0, "")
	label := "Total due"
	if paid {
		label = "Total paid"
	}
	pdf.CellFormat(colUnit, 9, label, "T", 0, "R", false, 0, "")
	pdf.CellFormat(colAmt, 9, fmtCents(inv.TotalCents), "T", 1, "R", false, 0, "")
	pdf.SetFont("Helvetica", "I", 8)
	pdf.SetTextColor(120, 120, 120)
	pdf.CellFormat(w, 4.5, "No GST is charged - not registered for GST.", "", 1, "R", false, 0, "")
	pdf.SetTextColor(28, 28, 28)
	pdf.Ln(8)

	// ── Payment box ──
	// Build the lines first: the height has to be known up front so the box
	// can move to a fresh page whole. Letting the auto page break split it
	// leaves the rectangle stranded — drawn on the next page with the
	// previous page's coordinates, which reads as a big empty box.
	type boxLine struct {
		label string // left column of a key/value row; "" for a full-width line
		text  string
		link  string // renders text as an underlined link
		bold  bool
		muted bool
		h     float64 // line height in mm
	}
	var lines []boxLine
	switch {
	case paid:
		received := fmtDate(inv.PaidAt) + " via " + db.PaymentMethodLabel(inv.PaymentMethod)
		if inv.PaymentRef != "" {
			received += " (ref " + inv.PaymentRef + ")"
		}
		lines = append(lines,
			boxLine{text: "Payment received - thank you", bold: true, h: 5},
			boxLine{text: tr(received), h: 5})
	case inv.Status == db.InvoiceVoid:
		lines = append(lines, boxLine{text: "This invoice has been voided - nothing is payable.", bold: true, h: 5})
	default:
		lines = append(lines, boxLine{text: "How to pay", bold: true, h: 5})
		if inv.PaymentLink != "" {
			lines = append(lines,
				boxLine{text: "Card online (Visa, Mastercard, Amex, Apple Pay, Google Pay):", h: 5},
				boxLine{text: inv.PaymentLink, link: inv.PaymentLink, h: 6})
		}
		if site.BankBSB != "" {
			lines = append(lines, boxLine{text: "Bank transfer:", h: 5})
			for _, kv := range [][2]string{
				{"Account name", site.BankName}, {"BSB", site.BankBSB}, {"Account", site.BankAcct}, {"Reference", inv.Ref()},
			} {
				lines = append(lines, boxLine{label: kv[0], text: tr(kv[1]), h: 5})
			}
		}
		lines = append(lines, boxLine{text: "Please quote " + inv.Ref() + " with any payment.", muted: true, h: 5})
	}
	boxH := 8.0 // 4 mm padding above and below
	for _, l := range lines {
		boxH += l.h
	}
	if pdf.GetY()+boxH > footerTop {
		pdf.AddPage()
	}
	pdf.SetDrawColor(220, 217, 210)
	xb, yb := 20.0, pdf.GetY()
	pdf.Rect(xb, yb, w, boxH, "D")
	pdf.SetY(yb + 4)
	for _, l := range lines {
		pdf.SetX(xb + 5)
		switch {
		case l.link != "":
			pdf.SetTextColor(20, 80, 160)
			pdf.SetFont("Helvetica", "U", 10)
			pdf.WriteLinkString(5, l.text, l.link)
			pdf.Ln(l.h)
			pdf.SetTextColor(28, 28, 28)
		case l.label != "":
			pdf.SetFont("Helvetica", "", 10)
			pdf.SetTextColor(110, 110, 110)
			pdf.CellFormat(32, l.h, l.label, "", 0, "L", false, 0, "")
			pdf.SetTextColor(28, 28, 28)
			pdf.CellFormat(w-10-32, l.h, l.text, "", 1, "L", false, 0, "")
		default:
			style := ""
			if l.bold {
				style = "B"
			}
			pdf.SetFont("Helvetica", style, 10)
			if l.muted {
				pdf.SetTextColor(110, 110, 110)
			}
			pdf.CellFormat(w-10, l.h, l.text, "", 1, "L", false, 0, "")
			pdf.SetTextColor(28, 28, 28)
		}
	}
	pdf.SetY(yb + boxH)
	pdf.Ln(6)

	if inv.Notes != "" {
		pdf.SetFont("Helvetica", "B", 9)
		pdf.SetTextColor(110, 110, 110)
		pdf.CellFormat(w, 5, "NOTES", "", 1, "L", false, 0, "")
		pdf.SetTextColor(28, 28, 28)
		pdf.SetFont("Helvetica", "", 10)
		pdf.MultiCell(w, 5, tr(inv.Notes), "", "L", false)
		pdf.Ln(4)
	}

	// ── Footer ──
	pdf.SetY(-30)
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetTextColor(140, 140, 140)
	pdf.CellFormat(w, 4, "Thank you for your business.", "", 1, "C", false, 0, "")
	if v.PublicURL != "" {
		pdf.CellFormat(w, 4, "View online: "+v.PublicURL, "", 1, "C", false, 0, "")
	}

	// ── PAID stamp ──
	if paid {
		pdf.SetFont("Helvetica", "B", 60)
		pdf.SetTextColor(210, 60, 60)
		pdf.SetAlpha(0.18, "Normal")
		pdf.TransformBegin()
		pdf.TransformRotate(-25, 105, 150)
		pdf.Text(70, 160, "PAID")
		pdf.TransformEnd()
		pdf.SetAlpha(1, "Normal")
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf output: %w", err)
	}
	return buf.Bytes(), nil
}

// fmtQty renders 1 → "1", 2.5 → "2.5", 0.25 → "0.25".
func fmtQty(q float64) string {
	s := fmt.Sprintf("%.2f", q)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}
