package main

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"localithelp/db"
)

// Admin pages for the Google Contacts sync: the preview, the on/off switch
// (both reached from /admin/calendar/settings) and the customer page's
// "Sync to Google now" button.

// contactPreviewRow is one customer on the preview page.
type contactPreviewRow struct {
	ID         int64
	Name       string
	Phone      string
	Email      string
	Action     string // label for the badge
	Kind       string // planCreate, planLink, ... or "gone"
	GoogleName string // the matching contact's current name
	NameChange bool   // linking will rename the contact to the customer's name
	ContactID  string // for the contacts.google.com link
	Note       string
}

type contactPreviewData struct {
	Flash   flash
	Rows    []contactPreviewRow
	Counts  map[string]int
	Enabled bool
	Err     string
}

// handleAdminContactsPreview shows what the sync would do with every customer,
// without writing anything to Google or the database.
func handleAdminContactsPreview(w http.ResponseWriter, r *http.Request) {
	d := contactPreviewData{Flash: readFlash(r), Counts: map[string]int{}}
	g, api, ok := contactsTarget()
	if !ok {
		redirectMsg(w, r, "/admin/calendar/settings", "err", "Connect Google with contacts access first.")
		return
	}
	d.Enabled = g.ContactsEnabled

	ctx, cancel := context.WithTimeout(r.Context(), contactsTimeout)
	defer cancel()
	people, err := api.ListContacts(ctx)
	if err != nil {
		slog.Error("gcontacts preview: list contacts", "err", err)
		d.Err = err.Error()
		render(w, r, "admin-contacts-preview", d)
		return
	}
	links, err := db.ListCustomerGContacts()
	if err != nil {
		slog.Error("gcontacts preview: links", "err", err)
		http.Error(w, "failed to load contact links", http.StatusInternalServerError)
		return
	}
	customers, err := db.ListCustomers("")
	if err != nil {
		slog.Error("gcontacts preview: customers", "err", err)
		http.Error(w, "failed to load customers", http.StatusInternalServerError)
		return
	}

	ix := newContactIndex(people, links)
	for i := range customers {
		c := &customers[i]
		row := contactPreviewRow{ID: c.ID, Name: c.Name, Phone: c.Phone, Email: c.Email}
		l, hasLink := links[c.ID]
		switch {
		case hasLink && l.Gone:
			row.Kind, row.Action = "gone", "Left alone"
			row.Note = "Its Google contact was deleted. Use Sync to Google now on the customer page to create it again."
		default:
			plan := planContact(ix, c, l.Resource)
			row.Kind, row.GoogleName = plan.Action, plan.GoogleName
			row.ContactID = (&db.CustomerGContact{Resource: plan.Resource}).ContactID()
			switch plan.Action {
			case planCreate:
				row.Action = "New contact"
			case planLink:
				row.Action = "Link to existing"
			case planUpdate:
				row.Action = "Linked"
				if _, found := ix.names[plan.Resource]; !found {
					row.Note = "The linked contact wasn't found in Google. It will be marked as deleted, not recreated."
				}
			case planAmbiguous:
				row.Action, row.Note = "Unclear", plan.Problem
			case planSkip:
				row.Action, row.Note = "Skipped", "No phone or email."
			}
			if plan.GoogleName != "" && c.Name != "" &&
				!strings.EqualFold(strings.Join(strings.Fields(c.Name), " "), strings.Join(strings.Fields(plan.GoogleName), " ")) {
				row.NameChange = true
			}
		}
		d.Counts[row.Kind]++
		d.Rows = append(d.Rows, row)
	}
	render(w, r, "admin-contacts-preview", d)
}

// handleAdminContactsEnabled turns the scheduled push on or off. Turning it on
// starts a first pass straight away rather than waiting for the next tick.
func handleAdminContactsEnabled(w http.ResponseWriter, r *http.Request) {
	const back = "/admin/calendar/settings"
	g, err := db.GetGoogleCalendar()
	if err != nil || !g.ContactsReady() {
		redirectMsg(w, r, back, "err", "Connect Google with contacts access first.")
		return
	}
	on := r.FormValue("enabled") == "on"
	if err := db.SetContactsEnabled(on); err != nil {
		slog.Error("gcontacts: set enabled", "err", err)
		redirectMsg(w, r, back, "err", "Could not save the setting.")
		return
	}
	if !on {
		redirectMsg(w, r, back, "ok", "Contact sync is off. Contacts already in Google are left there.")
		return
	}
	go func() {
		n := syncContacts(time.Now())
		slog.Info("gcontacts: first pass after enabling", "customers", n)
	}()
	redirectMsg(w, r, back, "ok", "Contact sync is on. Customers are being pushed now; refresh to see the counts.")
}

// handleAdminCustomerGContact pushes one customer now. It runs inline so the
// message can report the outcome, and works while bulk sync is off; that is
// how a single contact is tested before switching sync on.
func handleAdminCustomerGContact(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	back := "/admin/customers/" + strconv.FormatInt(id, 10)
	if _, err := db.GetCustomer(id); err != nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), contactsTimeout)
	defer cancel()
	if err := syncCustomerContact(ctx, id); err != nil {
		slog.Error("gcontacts: sync now", "customer", id, "err", err)
		redirectMsg(w, r, back, "err", "Could not sync to Google: "+err.Error())
		return
	}
	l, err := db.GetCustomerGContact(id)
	switch {
	case err != nil || l == nil:
		redirectMsg(w, r, back, "ok", "Synced to Google Contacts.")
	case l.Error != "":
		redirectMsg(w, r, back, "err", l.Error)
	case l.Resource == "":
		redirectMsg(w, r, back, "ok", "Nothing to sync: this customer has no phone or email.")
	default:
		redirectMsg(w, r, back, "ok", "Synced to Google Contacts.")
	}
}
