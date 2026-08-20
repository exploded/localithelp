package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"

	"mchugh.com.au/db"
)

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
	const w = 170.0 // printable width

	// ── Header: business (left) / document title (right) ──
	y0 := pdf.GetY()
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
	pdf.SetFillColor(250, 249, 246)
	pdf.SetDrawColor(220, 217, 210)
	xb, yb := pdf.GetX(), pdf.GetY()
	pdf.SetXY(xb+5, yb+4)
	if paid {
		pdf.SetFont("Helvetica", "B", 10)
		pdf.CellFormat(w-10, 5, "Payment received - thank you", "", 2, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 10)
		pdf.SetX(xb + 5)
		line := fmtDate(inv.PaidAt) + " via " + db.PaymentMethodLabel(inv.PaymentMethod)
		if inv.PaymentRef != "" {
			line += " (ref " + inv.PaymentRef + ")"
		}
		pdf.CellFormat(w-10, 5, tr(line), "", 2, "L", false, 0, "")
	} else if inv.Status == db.InvoiceVoid {
		pdf.SetFont("Helvetica", "B", 10)
		pdf.CellFormat(w-10, 5, "This invoice has been voided - nothing is payable.", "", 2, "L", false, 0, "")
	} else {
		pdf.SetFont("Helvetica", "B", 10)
		pdf.CellFormat(w-10, 5, "How to pay", "", 2, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 10)
		if inv.PaymentLink != "" {
			pdf.SetX(xb + 5)
			pdf.CellFormat(w-10, 5, "Card online (Visa, Mastercard, Amex, Apple Pay, Google Pay):", "", 2, "L", false, 0, "")
			pdf.SetX(xb + 5)
			pdf.SetTextColor(20, 80, 160)
			pdf.SetFont("Helvetica", "U", 10)
			pdf.WriteLinkString(5, inv.PaymentLink, inv.PaymentLink)
			pdf.Ln(6)
			pdf.SetTextColor(28, 28, 28)
			pdf.SetFont("Helvetica", "", 10)
		}
		if site.BankBSB != "" {
			pdf.SetX(xb + 5)
			pdf.CellFormat(w-10, 5, "Bank transfer:", "", 2, "L", false, 0, "")
			for _, kv := range [][2]string{
				{"Account name", site.BankName}, {"BSB", site.BankBSB}, {"Account", site.BankAcct}, {"Reference", inv.Ref()},
			} {
				pdf.SetX(xb + 5)
				pdf.SetTextColor(110, 110, 110)
				pdf.CellFormat(32, 5, kv[0], "", 0, "L", false, 0, "")
				pdf.SetTextColor(28, 28, 28)
				pdf.CellFormat(w-10-32, 5, tr(kv[1]), "", 1, "L", false, 0, "")
			}
		}
		pdf.SetX(xb + 5)
		pdf.SetTextColor(110, 110, 110)
		pdf.CellFormat(w-10, 5, "Or tap-to-pay by card or cash on the day. Please quote "+inv.Ref()+" with any payment.", "", 1, "L", false, 0, "")
		pdf.SetTextColor(28, 28, 28)
	}
	yEnd := pdf.GetY() + 4
	pdf.Rect(xb, yb, w, yEnd-yb, "D")
	pdf.SetY(yEnd)
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
