package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"localithelp/db"
)

// Outbound sync: customers → Google Contacts. The app is the source of truth
// for the fields it writes (name, the customer's phone and email, a blank
// address, and a link back to the customer page). Everything else on a contact,
// such as extra numbers added on the phone, is left alone. Nothing is ever
// pulled back, and no contact is ever deleted.

// contactsGroupName is the Google Contacts label every synced contact gets.
const contactsGroupName = "LOCAL IT HELP"

const (
	contactSyncLimit = 100              // most customers pushed in one scheduler pass
	contactsTimeout  = 60 * time.Second // one customer, including a full contact list
)

// contactSyncMu serialises every contact push. Google asks for one user's
// writes to go one at a time, and two pushes racing on a new customer could
// otherwise both create a contact.
var contactSyncMu sync.Mutex

// gcontactsClientFor builds a client for the stored connection. Overridable in
// tests.
var gcontactsClientFor = func(g *db.GoogleCalendar) (contactsAPI, error) { return newContactsClient(g) }

// ── Matching ──

// contactPhoneKey reduces a phone number to a key that matches however it was
// typed: "+61 412 345 678", "0412345678", "0061 412 345 678", "(03) 9876 5432",
// "+61 (0)3 9876 5432" and a bare "9876 5432" (taken as a 03 number) all fold
// onto the 10-digit local form. db.NormalizePhone does the common cases; the
// extra folds live here so stored phone_norm values don't shift. Anything under
// eight digits is no key at all.
func contactPhoneKey(p string) string {
	d := db.NormalizePhone(p)
	if strings.HasPrefix(d, "0061") {
		d = db.NormalizePhone(d[2:])
	}
	if len(d) == 12 && strings.HasPrefix(d, "610") {
		d = d[2:]
	}
	if len(d) == 8 && d[0] != '0' {
		d = "03" + d
	}
	if len(d) < 8 {
		return ""
	}
	return d
}

// contactIndex finds existing Google contacts by phone and email.
type contactIndex struct {
	byPhone map[string][]string // phone key -> contact resources
	byEmail map[string][]string // lower-cased email -> contact resources
	names   map[string]string   // contact resource -> display name
	linked  map[string]int64    // contact resource -> customer already linked to it
}

func newContactIndex(people []gPerson, links map[int64]db.CustomerGContact) *contactIndex {
	ix := &contactIndex{
		byPhone: map[string][]string{}, byEmail: map[string][]string{},
		names: map[string]string{}, linked: map[string]int64{},
	}
	for i := range people {
		ix.add(&people[i])
	}
	for id, l := range links {
		if l.Resource != "" {
			ix.linked[l.Resource] = id
		}
	}
	return ix
}

// add indexes one contact. It is also called for contacts created during a
// run, so a second customer with the same number links instead of duplicating.
func (ix *contactIndex) add(p *gPerson) {
	if p.ResourceName == "" {
		return
	}
	ix.names[p.ResourceName] = p.displayName()
	for _, ph := range p.PhoneNumbers {
		if k := contactPhoneKey(ph.Value); k != "" {
			ix.byPhone[k] = appendUnique(ix.byPhone[k], p.ResourceName)
		}
	}
	for _, e := range p.EmailAddresses {
		if k := strings.ToLower(strings.TrimSpace(e.Value)); k != "" {
			ix.byEmail[k] = appendUnique(ix.byEmail[k], p.ResourceName)
		}
	}
}

func appendUnique(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}

// What the sync will do with one customer.
const (
	planCreate    = "create"    // no matching contact: make one
	planLink      = "link"      // an existing contact matches: link it, then update it
	planUpdate    = "update"    // already linked: update it
	planAmbiguous = "ambiguous" // matches more than one contact: leave it for the admin
	planSkip      = "skip"      // no phone or email to sync
)

type contactPlan struct {
	Action     string
	Resource   string // the contact to link or update
	GoogleName string // its current name in Google
	Problem    string // why it's ambiguous
}

// planContact decides what to do with a customer. resource is the contact
// already linked, if any. Matching is by phone first, then email; anything
// that could be two different people is left for the admin rather than
// guessed.
func planContact(ix *contactIndex, c *db.Customer, resource string) contactPlan {
	if resource != "" {
		return contactPlan{Action: planUpdate, Resource: resource, GoogleName: ix.names[resource]}
	}
	key := contactPhoneKey(c.Phone)
	email := strings.ToLower(strings.TrimSpace(c.Email))
	if key == "" && email == "" {
		return contactPlan{Action: planSkip}
	}
	var byPhone, byEmail []string
	if key != "" {
		byPhone = ix.byPhone[key]
	}
	if email != "" {
		byEmail = ix.byEmail[email]
	}
	const fix = " Merge or fix them in Google Contacts, then press Sync to Google now."
	var match string
	switch {
	case len(byPhone) > 1:
		return contactPlan{Action: planAmbiguous, Problem: fmt.Sprintf(
			"%d Google contacts have the number %s: %s.", len(byPhone), c.Phone, ix.nameList(byPhone)) + fix}
	case len(byPhone) == 1 && len(byEmail) == 1 && byEmail[0] != byPhone[0]:
		return contactPlan{Action: planAmbiguous, Problem: fmt.Sprintf(
			"The phone number matches the Google contact %s, but the email matches %s.",
			ix.nameList(byPhone), ix.nameList(byEmail)) + fix}
	case len(byPhone) == 1:
		match = byPhone[0]
	case len(byEmail) > 1:
		return contactPlan{Action: planAmbiguous, Problem: fmt.Sprintf(
			"%d Google contacts have the email %s: %s.", len(byEmail), email, ix.nameList(byEmail)) + fix}
	case len(byEmail) == 1:
		match = byEmail[0]
	default:
		return contactPlan{Action: planCreate}
	}
	if other, ok := ix.linked[match]; ok && other != c.ID {
		return contactPlan{Action: planAmbiguous, Problem: fmt.Sprintf(
			"The matching Google contact %s is already linked to customer #%d. Is this the same person?",
			ix.nameList([]string{match}), other)}
	}
	return contactPlan{Action: planLink, Resource: match, GoogleName: ix.names[match]}
}

// nameList renders contacts as `"Ron", "Ron Smith"` for messages.
func (ix *contactIndex) nameList(resources []string) string {
	parts := make([]string, len(resources))
	for i, r := range resources {
		n := ix.names[r]
		if n == "" {
			n = "(no name)"
		}
		parts[i] = strconv.Quote(n)
	}
	return strings.Join(parts, ", ")
}

// ── Writing ──

// contactPhoneValue is the number as written to a new contact: international
// form for Australian numbers, so it dials correctly from anywhere.
func contactPhoneValue(phone string) string {
	k := contactPhoneKey(phone)
	if len(k) == 10 && k[0] == '0' {
		return "+61" + k[1:]
	}
	return strings.TrimSpace(phone)
}

// contactPhoneType labels mobiles as mobile and everything else as main.
func contactPhoneType(phone string) string {
	k := contactPhoneKey(phone)
	if strings.HasPrefix(k, "04") || strings.HasPrefix(k, "05") {
		return "mobile"
	}
	return "main"
}

// splitName puts the last word in the family name, so iOS sorts "Mary Jane
// Smith" under S. A single word is a given name only.
func splitName(name string) gName {
	f := strings.Fields(name)
	switch len(f) {
	case 0:
		return gName{}
	case 1:
		return gName{GivenName: f[0]}
	}
	return gName{GivenName: strings.Join(f[:len(f)-1], " "), FamilyName: f[len(f)-1]}
}

func customerAdminURL(id int64) string {
	return site.BaseURL + "/admin/customers/" + strconv.FormatInt(id, 10)
}

func customerAddress(c *db.Customer) (gAddress, bool) {
	if a := strings.TrimSpace(c.Address); a != "" {
		return gAddress{StreetAddress: a, Type: "home"}, true
	}
	if s := strings.TrimSpace(c.Suburb); s != "" {
		return gAddress{City: s, Type: "home"}, true
	}
	return gAddress{}, false
}

// personForCustomer builds a new contact for a customer.
func personForCustomer(c *db.Customer, group string) *gPerson {
	p := &gPerson{URLs: []gURL{{Value: customerAdminURL(c.ID), Type: contactsGroupName}}}
	if n := splitName(c.Name); n != (gName{}) {
		p.Names = []gName{n}
	}
	if c.Phone != "" {
		p.PhoneNumbers = []gPhone{{Value: contactPhoneValue(c.Phone), Type: contactPhoneType(c.Phone)}}
	}
	if c.Email != "" {
		p.EmailAddresses = []gEmail{{Value: c.Email}}
	}
	if a, ok := customerAddress(c); ok {
		p.Addresses = []gAddress{a}
	}
	if group != "" {
		p.Memberships = []gMembership{{ContactGroupMembership: &struct {
			ContactGroupResourceName string `json:"contactGroupResourceName,omitempty"`
		}{group}}}
	}
	return p
}

// mergeCustomer applies the customer's details to an existing contact and
// returns the People API fields that changed. The name is replaced; the phone,
// email and admin link are added when missing, never removing what's there; the
// address is filled only when the contact has none.
func mergeCustomer(p *gPerson, c *db.Customer) []string {
	var fields []string
	if name := strings.Join(strings.Fields(c.Name), " "); name != "" &&
		!strings.EqualFold(name, strings.Join(strings.Fields(p.displayName()), " ")) {
		p.Names = []gName{splitName(name)}
		fields = append(fields, "names")
	}
	if key := contactPhoneKey(c.Phone); key != "" {
		has := false
		for _, ph := range p.PhoneNumbers {
			if contactPhoneKey(ph.Value) == key {
				has = true
				break
			}
		}
		if !has {
			p.PhoneNumbers = append(p.PhoneNumbers, gPhone{Value: contactPhoneValue(c.Phone), Type: contactPhoneType(c.Phone)})
			fields = append(fields, "phoneNumbers")
		}
	}
	if email := strings.ToLower(strings.TrimSpace(c.Email)); email != "" {
		has := false
		for _, e := range p.EmailAddresses {
			if strings.EqualFold(strings.TrimSpace(e.Value), email) {
				has = true
				break
			}
		}
		if !has {
			p.EmailAddresses = append(p.EmailAddresses, gEmail{Value: c.Email})
			fields = append(fields, "emailAddresses")
		}
	}
	if len(p.Addresses) == 0 {
		if a, ok := customerAddress(c); ok {
			p.Addresses = []gAddress{a}
			fields = append(fields, "addresses")
		}
	}
	link := customerAdminURL(c.ID)
	has := false
	for _, u := range p.URLs {
		if u.Value == link {
			has = true
			break
		}
	}
	if !has {
		p.URLs = append(p.URLs, gURL{Value: link, Type: contactsGroupName})
		fields = append(fields, "urls")
	}
	return fields
}

// ── Running ──

// contactRun pushes customers over one connection. The contact list is
// fetched once, and only when a customer without a link needs matching.
type contactRun struct {
	api   contactsAPI
	group string
	ix    *contactIndex
}

func (r *contactRun) index(ctx context.Context) (*contactIndex, error) {
	if r.ix != nil {
		return r.ix, nil
	}
	people, err := r.api.ListContacts(ctx)
	if err != nil {
		return nil, err
	}
	links, err := db.ListCustomerGContacts()
	if err != nil {
		return nil, err
	}
	r.ix = newContactIndex(people, links)
	return r.ix, nil
}

// ensureGroup returns the LOCAL IT HELP label, finding or creating it on first
// use and remembering it on the connection row.
func (r *contactRun) ensureGroup(ctx context.Context) (string, error) {
	if r.group != "" {
		return r.group, nil
	}
	groups, err := r.api.ListGroups(ctx)
	if err != nil {
		return "", err
	}
	for _, g := range groups {
		if g.GroupType == "USER_CONTACT_GROUP" && strings.EqualFold(strings.TrimSpace(g.Name), contactsGroupName) {
			r.group = g.ResourceName
		}
	}
	if r.group == "" {
		if r.group, err = r.api.CreateGroup(ctx, contactsGroupName); err != nil {
			return "", err
		}
	}
	if err := db.SetContactsGroup(r.group); err != nil {
		slog.Error("gcontacts: save group", "err", err)
	}
	return r.group, nil
}

// errContactGone reports that a linked contact was deleted in Google.
var errContactGone = errors.New("contact deleted in Google")

// sync pushes one customer and records the outcome. resource is the linked
// contact, or empty to match or create. A returned error is worth retrying; settled
// outcomes, including ambiguous matches, are recorded and return nil.
func (r *contactRun) sync(ctx context.Context, c *db.Customer, resource string) error {
	err := r.push(ctx, c, resource)
	switch {
	case errors.Is(err, errContactGone):
		return db.SetCustomerGContactGone(c.ID,
			"The Google contact was deleted, so it wasn't recreated. Press Sync to Google now to create it again.")
	case err != nil:
		if e := db.SetCustomerGContactError(c.ID, err.Error()); e != nil {
			slog.Error("gcontacts: record error", "customer", c.ID, "err", e)
		}
		return err
	}
	return nil
}

func (r *contactRun) push(ctx context.Context, c *db.Customer, resource string) error {
	var plan contactPlan
	if resource != "" {
		plan = contactPlan{Action: planUpdate, Resource: resource}
	} else {
		ix, err := r.index(ctx)
		if err != nil {
			return err
		}
		plan = planContact(ix, c, "")
	}

	switch plan.Action {
	case planSkip:
		return db.SetCustomerGContact(c.ID, "", "")
	case planAmbiguous:
		return db.SetCustomerGContact(c.ID, "", plan.Problem)
	}

	group, err := r.ensureGroup(ctx)
	if err != nil {
		return err
	}

	if plan.Action == planCreate {
		p, err := r.api.CreateContact(ctx, personForCustomer(c, group))
		if err != nil {
			return err
		}
		if r.ix != nil {
			r.ix.add(p)
			r.ix.linked[p.ResourceName] = c.ID
		}
		slog.Info("gcontacts: created", "customer", c.ID, "contact", p.ResourceName)
		return db.SetCustomerGContact(c.ID, p.ResourceName, "")
	}

	// Link or update: always read the contact fresh, for its etag and so
	// nothing added on the phone is lost.
	p, err := r.api.GetContact(ctx, plan.Resource)
	if errors.Is(err, errNotFound) {
		if plan.Action == planUpdate {
			return errContactGone
		}
		return fmt.Errorf("the matching Google contact has just been deleted; will try again")
	}
	if err != nil {
		return err
	}
	if fields := mergeCustomer(p, c); len(fields) > 0 {
		if p, err = r.api.UpdateContact(ctx, p, fields); err != nil {
			return err
		}
	}
	if !p.inGroup(group) {
		if err := r.api.AddToGroup(ctx, group, plan.Resource); err != nil {
			if errors.Is(err, errNotFound) {
				// The label was deleted in Google: forget it, and the next
				// push makes a new one.
				r.group = ""
				_ = db.SetContactsGroup("")
			}
			return err
		}
	}
	if r.ix != nil {
		r.ix.linked[plan.Resource] = c.ID
	}
	if plan.Action == planLink {
		slog.Info("gcontacts: linked", "customer", c.ID, "contact", plan.Resource)
	}
	return db.SetCustomerGContact(c.ID, plan.Resource, "")
}

// contactsTarget returns the connection and a client when contacts can be
// pushed at all (connected, with the contacts scope granted).
func contactsTarget() (*db.GoogleCalendar, contactsAPI, bool) {
	g, err := db.GetGoogleCalendar()
	if err != nil {
		slog.Error("gcontacts: load connection", "err", err)
		return nil, nil, false
	}
	if !g.ContactsReady() {
		return nil, nil, false
	}
	api, err := gcontactsClientFor(g)
	if err != nil {
		slog.Error("gcontacts: client", "err", err)
		return nil, nil, false
	}
	return g, api, true
}

// syncContacts is the scheduler job: it pushes every customer changed since
// their last push. It does nothing until contact sync is switched on.
func syncContacts(time.Time) int {
	g, api, ok := contactsTarget()
	if !ok || !g.ContactsEnabled {
		return 0
	}
	contactSyncMu.Lock()
	defer contactSyncMu.Unlock()
	cs, err := db.ListCustomersForContactSync(contactSyncLimit)
	if err != nil {
		slog.Error("gcontacts: list dirty customers", "err", err)
		return 0
	}
	run := &contactRun{api: api, group: g.ContactsGroup}
	done := 0
	for i := range cs {
		ctx, cancel := context.WithTimeout(context.Background(), contactsTimeout)
		err := run.sync(ctx, &cs[i].Customer, cs[i].Resource)
		cancel()
		if err != nil {
			slog.Error("gcontacts: sync customer", "customer", cs[i].ID, "err", err)
			continue
		}
		done++
	}
	return done
}

// syncCustomerContact pushes one customer now, for the customer page's button.
// It works while bulk sync is off, and recreates a contact deleted in Google.
func syncCustomerContact(ctx context.Context, id int64) error {
	g, api, ok := contactsTarget()
	if !ok {
		return fmt.Errorf("contact sync isn't connected")
	}
	contactSyncMu.Lock()
	defer contactSyncMu.Unlock()
	c, err := db.GetCustomer(id)
	if err != nil {
		return err
	}
	resource := ""
	if l, err := db.GetCustomerGContact(id); err != nil {
		return err
	} else if l != nil && !l.Gone {
		resource = l.Resource
	}
	run := &contactRun{api: api, group: g.ContactsGroup}
	return run.sync(ctx, c, resource)
}

// syncContactSoon pushes one customer in the background after a save, when
// contact sync is switched on. Failures are left to the next scheduler tick.
func syncContactSoon(id int64) {
	if gcalOAuth == nil || id == 0 {
		return
	}
	go func() {
		g, err := db.GetGoogleCalendar()
		if err != nil || !g.ContactsReady() || !g.ContactsEnabled {
			return
		}
		l, err := db.GetCustomerGContact(id)
		if err != nil || (l != nil && l.Gone) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), contactsTimeout)
		defer cancel()
		if err := syncCustomerContact(ctx, id); err != nil {
			slog.Error("gcontacts: sync customer", "customer", id, "err", err)
		}
	}()
}
