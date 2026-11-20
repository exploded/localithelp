package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"localithelp/db"
)

// fakeContacts is an in-memory People API, so the tests can assert on the
// create/link/update decisions without touching Google.
type fakeContacts struct {
	people    map[string]*gPerson
	order     []string
	groups    []gGroup
	next      int
	creates   int
	updates   int
	groupAdds int
	lists     int
	fields    [][]string // updatePersonFields of each update
}

func newFakeContacts() *fakeContacts { return &fakeContacts{people: map[string]*gPerson{}} }

func clonePerson(p *gPerson) *gPerson {
	b, _ := json.Marshal(p)
	var out gPerson
	_ = json.Unmarshal(b, &out)
	return &out
}

// seed adds an existing contact, as if typed on the phone.
func (f *fakeContacts) seed(name string, phones ...string) string {
	p := &gPerson{Names: []gName{{DisplayName: name, GivenName: name}}}
	for _, ph := range phones {
		p.PhoneNumbers = append(p.PhoneNumbers, gPhone{Value: ph, Type: "mobile"})
	}
	return f.store(p)
}

func (f *fakeContacts) store(p *gPerson) string {
	f.next++
	p.ResourceName = fmt.Sprintf("people/c%d", f.next)
	p.Etag = "etag"
	f.people[p.ResourceName] = clonePerson(p)
	f.order = append(f.order, p.ResourceName)
	return p.ResourceName
}

func (f *fakeContacts) ListContacts(context.Context) ([]gPerson, error) {
	f.lists++
	var out []gPerson
	for _, r := range f.order {
		if p, ok := f.people[r]; ok {
			out = append(out, *clonePerson(p))
		}
	}
	return out, nil
}

func (f *fakeContacts) GetContact(_ context.Context, resource string) (*gPerson, error) {
	p, ok := f.people[resource]
	if !ok {
		return nil, errNotFound
	}
	return clonePerson(p), nil
}

func (f *fakeContacts) CreateContact(_ context.Context, p *gPerson) (*gPerson, error) {
	f.creates++
	c := clonePerson(p)
	if len(c.Names) > 0 {
		c.Names[0].DisplayName = strings.TrimSpace(c.Names[0].GivenName + " " + c.Names[0].FamilyName)
	}
	f.store(c)
	return clonePerson(c), nil
}

func (f *fakeContacts) UpdateContact(_ context.Context, p *gPerson, fields []string) (*gPerson, error) {
	cur, ok := f.people[p.ResourceName]
	if !ok {
		return nil, errNotFound
	}
	f.updates++
	f.fields = append(f.fields, fields)
	for _, fl := range fields {
		switch fl {
		case "names":
			cur.Names = p.Names
			for i := range cur.Names {
				cur.Names[i].DisplayName = strings.TrimSpace(cur.Names[i].GivenName + " " + cur.Names[i].FamilyName)
			}
		case "phoneNumbers":
			cur.PhoneNumbers = p.PhoneNumbers
		case "emailAddresses":
			cur.EmailAddresses = p.EmailAddresses
		case "addresses":
			cur.Addresses = p.Addresses
		case "urls":
			cur.URLs = p.URLs
		}
	}
	return clonePerson(cur), nil
}

func (f *fakeContacts) ListGroups(context.Context) ([]gGroup, error) { return f.groups, nil }

func (f *fakeContacts) CreateGroup(_ context.Context, name string) (string, error) {
	g := gGroup{ResourceName: fmt.Sprintf("contactGroups/g%d", len(f.groups)+1), Name: name, GroupType: "USER_CONTACT_GROUP"}
	f.groups = append(f.groups, g)
	return g.ResourceName, nil
}

func (f *fakeContacts) AddToGroup(_ context.Context, group, resource string) error {
	p, ok := f.people[resource]
	if !ok {
		return errNotFound
	}
	f.groupAdds++
	p.Memberships = append(p.Memberships, personForCustomer(&db.Customer{}, group).Memberships...)
	return nil
}

// connectContactsFake stores a connection with the contacts scope granted and
// points the sync at the fake.
func connectContactsFake(t *testing.T, f *fakeContacts, enabled bool) {
	t.Helper()
	if err := db.SaveGoogleCalendar("me@example.test", "refresh-token", "cal-app", calendarName, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetContactsEnabled(enabled); err != nil {
		t.Fatal(err)
	}
	prev := gcontactsClientFor
	gcontactsClientFor = func(*db.GoogleCalendar) (contactsAPI, error) { return f, nil }
	t.Cleanup(func() { gcontactsClientFor = prev })
}

func addCustomer(t *testing.T, name, email, phone string) int64 {
	t.Helper()
	id, err := db.FindOrCreateCustomer(name, email, phone, "Donvale")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func gcontactOf(t *testing.T, id int64) *db.CustomerGContact {
	t.Helper()
	l, err := db.GetCustomerGContact(id)
	if err != nil {
		t.Fatal(err)
	}
	if l == nil {
		t.Fatalf("customer #%d has no contact row", id)
	}
	return l
}

func TestContactPhoneKey(t *testing.T) {
	for in, want := range map[string]string{
		"+61412345678":       "0412345678",
		"+61 412 345 678":    "0412345678",
		"0412 345 678":       "0412345678",
		"0412-345-678":       "0412345678",
		"0061 412 345 678":   "0412345678",
		"+61 3 9876 5432":    "0398765432",
		"(03) 9876 5432":     "0398765432",
		"+61 (0)3 9876 5432": "0398765432",
		"9876 5432":          "0398765432",
		"1300 123 456":       "1300123456",
		"000":                "",
		"":                   "",
	} {
		if got := contactPhoneKey(in); got != want {
			t.Errorf("contactPhoneKey(%q) = %q, want %q", in, got, want)
		}
	}
	if got := contactPhoneValue("0412 345 678"); got != "+61412345678" {
		t.Errorf("contactPhoneValue = %q, want international form", got)
	}
	if contactPhoneType("(03) 9876 5432") != "main" || contactPhoneType("0412345678") != "mobile" {
		t.Error("phone types: want main for landlines, mobile for 04")
	}
}

func TestPlanContact(t *testing.T) {
	people := []gPerson{
		{ResourceName: "people/ron", Names: []gName{{DisplayName: "Ron"}}, PhoneNumbers: []gPhone{{Value: "+61 412 345 678"}}},
		{ResourceName: "people/ann", Names: []gName{{DisplayName: "Ann"}}, EmailAddresses: []gEmail{{Value: "Ann@Example.test"}}},
		{ResourceName: "people/twin1", Names: []gName{{DisplayName: "Twin"}}, PhoneNumbers: []gPhone{{Value: "0400 111 222"}}},
		{ResourceName: "people/twin2", Names: []gName{{DisplayName: "Twin B"}}, PhoneNumbers: []gPhone{{Value: "+61400111222"}}},
		{ResourceName: "people/taken", Names: []gName{{DisplayName: "Taken"}}, PhoneNumbers: []gPhone{{Value: "0400 999 999"}}},
	}
	ix := newContactIndex(people, map[int64]db.CustomerGContact{50: {CustomerID: 50, Resource: "people/taken"}})
	cases := []struct {
		name string
		c    db.Customer
		res  string
		want string
		to   string
	}{
		{"phone in another format", db.Customer{ID: 1, Name: "Ron Smith", Phone: "0412345678"}, "", planLink, "people/ron"},
		{"email, any case", db.Customer{ID: 2, Name: "Ann", Email: "ann@example.test"}, "", planLink, "people/ann"},
		{"two contacts share the number", db.Customer{ID: 3, Phone: "0400111222"}, "", planAmbiguous, ""},
		{"phone and email disagree", db.Customer{ID: 4, Phone: "0412 345 678", Email: "ann@example.test"}, "", planAmbiguous, ""},
		{"already linked to someone else", db.Customer{ID: 5, Phone: "0400999999"}, "", planAmbiguous, ""},
		{"no match", db.Customer{ID: 6, Name: "New", Phone: "0400 555 666"}, "", planCreate, ""},
		{"nothing to match on", db.Customer{ID: 7, Name: "Nobody"}, "", planSkip, ""},
		{"already linked", db.Customer{ID: 8, Phone: "0400 555 666"}, "people/x", planUpdate, "people/x"},
	}
	for _, tc := range cases {
		p := planContact(ix, &tc.c, tc.res)
		if p.Action != tc.want || p.Resource != tc.to {
			t.Errorf("%s: got %s %q, want %s %q (%s)", tc.name, p.Action, p.Resource, tc.want, tc.to, p.Problem)
		}
		if p.Action == planAmbiguous && p.Problem == "" {
			t.Errorf("%s: ambiguous with no explanation", tc.name)
		}
	}
	// The linked customer itself still matches its own contact.
	if p := planContact(ix, &db.Customer{ID: 50, Phone: "0400999999"}, ""); p.Action != planLink {
		t.Errorf("own contact: got %s, want link", p.Action)
	}
}

// TestMergeCustomerKeepsPhoneData checks a linked contact keeps whatever was
// added on the phone: extra numbers, emails and its address.
func TestMergeCustomerKeepsPhoneData(t *testing.T) {
	site = siteConfig{BaseURL: "https://example.test"}
	p := &gPerson{
		Names:          []gName{{DisplayName: "Ron"}},
		PhoneNumbers:   []gPhone{{Value: "+61 412 345 678"}, {Value: "(03) 9000 0000", Type: "home"}},
		EmailAddresses: []gEmail{{Value: "ron@home.test"}},
		Addresses:      []gAddress{{FormattedValue: "1 Phone St"}},
	}
	c := &db.Customer{ID: 9, Name: "Ron Smith", Phone: "0412345678", Email: "ron@work.test", Address: "2 App Rd"}
	fields := mergeCustomer(p, c)
	if want := []string{"names", "emailAddresses", "urls"}; !slices.Equal(fields, want) {
		t.Errorf("fields = %v, want %v", fields, want)
	}
	if p.Names[0].GivenName != "Ron" || p.Names[0].FamilyName != "Smith" {
		t.Errorf("name = %+v, want Ron / Smith", p.Names[0])
	}
	if len(p.PhoneNumbers) != 2 {
		t.Errorf("phones = %+v, want both kept and no duplicate", p.PhoneNumbers)
	}
	if len(p.EmailAddresses) != 2 || p.EmailAddresses[0].Value != "ron@home.test" {
		t.Errorf("emails = %+v, want the phone's email kept and the customer's added", p.EmailAddresses)
	}
	if len(p.Addresses) != 1 || p.Addresses[0].FormattedValue != "1 Phone St" {
		t.Errorf("address = %+v, want the existing one untouched", p.Addresses)
	}
	// Nothing left to change on a second pass.
	if again := mergeCustomer(p, c); len(again) != 0 {
		t.Errorf("second merge changed %v, want nothing", again)
	}
	// Same name in a different case is not a rename.
	q := &gPerson{Names: []gName{{DisplayName: "ron smith"}}}
	for _, f := range mergeCustomer(q, c) {
		if f == "names" {
			t.Error("case-only difference renamed the contact")
		}
	}
}

func TestSyncContacts(t *testing.T) {
	openTestDB(t, "contacts.db")
	f := newFakeContacts()
	ronContact := f.seed("Ron", "+61 412 345 678", "(03) 9000 0000")
	f.seed("Twin", "0400 111 222")
	f.seed("Twin B", "+61400111222")

	ron := addCustomer(t, "Ron Smith", "", "0412345678")
	ann := addCustomer(t, "Ann Lee", "ann@example.test", "0400 555 666")
	bo := addCustomer(t, "Bo", "", "0400111222")

	// Off by default: the scheduler does nothing.
	connectContactsFake(t, f, false)
	if n := syncContacts(time.Now()); n != 0 || f.lists != 0 {
		t.Fatalf("sync while off pushed %d customers (%d lists)", n, f.lists)
	}

	if err := db.SetContactsEnabled(true); err != nil {
		t.Fatal(err)
	}
	if n := syncContacts(time.Now()); n != 3 {
		t.Fatalf("first pass pushed %d customers, want 3", n)
	}
	if f.lists != 1 {
		t.Errorf("listed contacts %d times, want once per pass", f.lists)
	}
	if f.creates != 1 {
		t.Errorf("created %d contacts, want 1 (Ann only)", f.creates)
	}

	// Ron is linked to the contact typed on the phone, renamed, keeps both
	// numbers, and gains the label and the admin link.
	if l := gcontactOf(t, ron); l.Resource != ronContact || l.Error != "" {
		t.Errorf("Ron link = %+v, want %s", l, ronContact)
	}
	rc := f.people[ronContact]
	if rc.displayName() != "Ron Smith" || len(rc.PhoneNumbers) != 2 {
		t.Errorf("Ron's contact = %q with %d numbers, want renamed with both numbers", rc.displayName(), len(rc.PhoneNumbers))
	}
	if len(f.groups) != 1 || !rc.inGroup(f.groups[0].ResourceName) {
		t.Errorf("groups = %+v, want one LOCAL IT HELP label holding Ron", f.groups)
	}
	if len(rc.URLs) != 1 || !strings.HasSuffix(rc.URLs[0].Value, fmt.Sprintf("/admin/customers/%d", ron)) {
		t.Errorf("Ron's URLs = %+v, want the customer page", rc.URLs)
	}

	// Ann is a new contact, created already in the label.
	al := gcontactOf(t, ann)
	ac := f.people[al.Resource]
	if ac == nil || ac.displayName() != "Ann Lee" || !ac.inGroup(f.groups[0].ResourceName) {
		t.Fatalf("Ann's contact = %+v, want created in the label", ac)
	}
	if ac.PhoneNumbers[0].Value != "+61400555666" {
		t.Errorf("Ann's number = %q, want international form", ac.PhoneNumbers[0].Value)
	}

	// Bo matches two contacts: left for the admin, nothing created.
	if bl := gcontactOf(t, bo); bl.Resource != "" || !strings.Contains(bl.Error, "2 Google contacts") {
		t.Errorf("Bo = %+v, want an ambiguity message and no link", bl)
	}

	// Everyone settled: the next pass has nothing to do and makes no calls.
	lists := f.lists
	if n := syncContacts(time.Now()); n != 0 || f.lists != lists {
		t.Errorf("second pass pushed %d (lists %d → %d), want nothing", n, lists, f.lists)
	}
	if pending, _ := db.CountCustomersForContactSync(); pending != 0 {
		t.Errorf("pending = %d, want 0", pending)
	}

	// A contact deleted in Google is not recreated by the sync...
	delete(f.people, al.Resource)
	run := &contactRun{api: f, group: f.groups[0].ResourceName}
	c, _ := db.GetCustomer(ann)
	if err := run.sync(context.Background(), c, al.Resource); err != nil {
		t.Fatal(err)
	}
	if l := gcontactOf(t, ann); !l.Gone || l.Resource != "" {
		t.Errorf("after delete: %+v, want gone", l)
	}
	creates := f.creates
	// ...but the customer page's button does recreate it.
	if err := syncCustomerContact(context.Background(), ann); err != nil {
		t.Fatal(err)
	}
	if l := gcontactOf(t, ann); l.Gone || l.Resource == "" || f.creates != creates+1 {
		t.Errorf("after Sync now: %+v (creates %d), want a new contact", l, f.creates)
	}
}

// TestSyncContactsNeedsScope keeps the sync off when the connection lacks the
// contacts scope, even if the switch is on.
func TestSyncContactsNeedsScope(t *testing.T) {
	openTestDB(t, "noscope.db")
	f := newFakeContacts()
	connectContactsFake(t, f, true)
	if err := db.SaveGoogleCalendar("me@example.test", "refresh-token", "cal-app", calendarName, false); err != nil {
		t.Fatal(err)
	}
	addCustomer(t, "Ann", "ann@example.test", "")
	if n := syncContacts(time.Now()); n != 0 || f.lists+f.creates != 0 {
		t.Errorf("synced %d without the contacts scope", n)
	}
	if err := syncCustomerContact(context.Background(), 1); err == nil {
		t.Error("Sync now worked without the contacts scope")
	}
}

func TestScopeGranted(t *testing.T) {
	tok := (&oauth2.Token{}).WithExtra(map[string]any{"scope": calendarScope + " " + contactsScope + " openid"})
	if !scopeGranted(tok, contactsScope) {
		t.Error("contacts scope not found in the granted list")
	}
	tok = (&oauth2.Token{}).WithExtra(map[string]any{"scope": calendarScope})
	if scopeGranted(tok, contactsScope) {
		t.Error("contacts reported granted when only the calendar was")
	}
	if scopeGranted(&oauth2.Token{}, contactsScope) {
		t.Error("contacts reported granted with no scope field")
	}
}

// TestContactAdminPages drives the preview, the customer page's button and the
// on/off switch through the real handlers.
func TestContactAdminPages(t *testing.T) {
	openTestDB(t, "contacts-admin.db")
	var err error
	if pages, err = loadTemplates("../../templates"); err != nil {
		t.Fatal(err)
	}
	f := newFakeContacts()
	f.seed("Ron", "+61 412 345 678")
	ron := addCustomer(t, "Ron Smith", "", "0412345678")
	ann := addCustomer(t, "Ann Lee", "ann@example.test", "")
	connectContactsFake(t, f, false)

	sessTok := "sess-" + generateSessionToken()
	userSessions.Store(sessTok, &userSession{User: &db.User{Email: adminEmail()}, Expiry: time.Now().Add(time.Hour), CSRF: "csrf1"})
	t.Cleanup(func() { userSessions.Delete(sessTok) })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/customers/{id}", requireAdmin(handleAdminCustomer))
	mux.HandleFunc("POST /admin/customers/{id}/gcontact", requireAdmin(handleAdminCustomerGContact))
	mux.HandleFunc("GET /admin/contacts/preview", requireAdmin(handleAdminContactsPreview))
	mux.HandleFunc("POST /admin/contacts/enabled", requireAdmin(handleAdminContactsEnabled))
	do := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var req *http.Request
		if method == http.MethodPost {
			if form == nil {
				form = url.Values{}
			}
			form.Set("csrf", "csrf1")
			req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		req.AddCookie(&http.Cookie{Name: "user_session", Value: sessTok})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr
	}

	// The preview writes nothing.
	rr := do(http.MethodGet, "/admin/contacts/preview", nil)
	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, "Link to existing") || !strings.Contains(body, "New contact") ||
		!strings.Contains(body, "will become “Ron Smith”") {
		t.Fatalf("preview: %d\n%s", rr.Code, body)
	}
	if f.creates+f.updates != 0 {
		t.Fatalf("preview wrote to Google: %d creates, %d updates", f.creates, f.updates)
	}
	if l, _ := db.GetCustomerGContact(ron); l != nil {
		t.Fatalf("preview wrote a link: %+v", l)
	}

	// Sync to Google now links Ron while bulk sync is off.
	rr = do(http.MethodPost, fmt.Sprintf("/admin/customers/%d/gcontact", ron), nil)
	if loc := rr.Header().Get("Location"); rr.Code != http.StatusSeeOther || !strings.Contains(loc, "ok=") {
		t.Fatalf("sync now: %d %s", rr.Code, loc)
	}
	if l := gcontactOf(t, ron); l.Resource == "" || f.creates != 0 {
		t.Fatalf("Ron after sync now: %+v (creates %d)", l, f.creates)
	}
	if body := do(http.MethodGet, fmt.Sprintf("/admin/customers/%d", ron), nil).Body.String(); !strings.Contains(body, "contacts.google.com/person/c1") {
		t.Errorf("customer page has no Google Contacts link:\n%s", body)
	}
	if l, _ := db.GetCustomerGContact(ann); l != nil {
		t.Errorf("Ann was pushed while sync was off: %+v", l)
	}

	// The switch turns the scheduled push on and off.
	if rr := do(http.MethodPost, "/admin/contacts/enabled", url.Values{"enabled": {"off"}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("switch off: %d", rr.Code)
	}
	if g, _ := db.GetGoogleCalendar(); g.ContactsEnabled {
		t.Error("contact sync still on after turning it off")
	}
}
