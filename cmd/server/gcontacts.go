package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/oauth2"

	"localithelp/db"
)

// ── Google Contacts (People API) ──
//
// Customers are pushed one way, app → Google, so they show by name when they
// ring. The contacts scope is granted on the same connection as the calendar
// (see gcal.go), so this file holds only the People API surface; the sync rules
// are in gcontacts_sync.go.
//
// Which SIM an iPhone uses to call a contact is stored on the phone, not in
// Google, so nothing here can set or clear it. To keep it intact the sync only
// ever creates contacts or patches them in place; it never deletes or
// recreates one.

const peopleBase = "https://people.googleapis.com/v1"

// gPerson is the subset of a People API person the app reads and writes.
// Metadata is passed back untouched on update: Google requires its source
// etags, and each field entry's own metadata tells Google which entries come
// from the contact (written) and which from a Google profile (ignored).
type gPerson struct {
	ResourceName   string          `json:"resourceName,omitempty"`
	Etag           string          `json:"etag,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	Names          []gName         `json:"names,omitempty"`
	PhoneNumbers   []gPhone        `json:"phoneNumbers,omitempty"`
	EmailAddresses []gEmail        `json:"emailAddresses,omitempty"`
	Addresses      []gAddress      `json:"addresses,omitempty"`
	URLs           []gURL          `json:"urls,omitempty"`
	Memberships    []gMembership   `json:"memberships,omitempty"`
}

type gFieldMeta struct {
	Primary bool `json:"primary,omitempty"`
	Source  *struct {
		Type string `json:"type,omitempty"`
		ID   string `json:"id,omitempty"`
	} `json:"source,omitempty"`
}

type gName struct {
	Metadata    *gFieldMeta `json:"metadata,omitempty"`
	DisplayName string      `json:"displayName,omitempty"` // read-only
	GivenName   string      `json:"givenName,omitempty"`
	FamilyName  string      `json:"familyName,omitempty"`
}

type gPhone struct {
	Metadata *gFieldMeta `json:"metadata,omitempty"`
	Value    string      `json:"value,omitempty"`
	Type     string      `json:"type,omitempty"`
}

type gEmail struct {
	Metadata *gFieldMeta `json:"metadata,omitempty"`
	Value    string      `json:"value,omitempty"`
	Type     string      `json:"type,omitempty"`
}

type gAddress struct {
	Metadata        *gFieldMeta `json:"metadata,omitempty"`
	FormattedValue  string      `json:"formattedValue,omitempty"`
	Type            string      `json:"type,omitempty"`
	PoBox           string      `json:"poBox,omitempty"`
	StreetAddress   string      `json:"streetAddress,omitempty"`
	ExtendedAddress string      `json:"extendedAddress,omitempty"`
	City            string      `json:"city,omitempty"`
	Region          string      `json:"region,omitempty"`
	PostalCode      string      `json:"postalCode,omitempty"`
	Country         string      `json:"country,omitempty"`
	CountryCode     string      `json:"countryCode,omitempty"`
}

type gURL struct {
	Metadata *gFieldMeta `json:"metadata,omitempty"`
	Value    string      `json:"value,omitempty"`
	Type     string      `json:"type,omitempty"`
}

type gMembership struct {
	Metadata               *gFieldMeta `json:"metadata,omitempty"`
	ContactGroupMembership *struct {
		ContactGroupResourceName string `json:"contactGroupResourceName,omitempty"`
	} `json:"contactGroupMembership,omitempty"`
}

// displayName is the contact's name as Google shows it.
func (p *gPerson) displayName() string {
	for _, n := range p.Names {
		if n.DisplayName != "" {
			return n.DisplayName
		}
		if s := strings.TrimSpace(n.GivenName + " " + n.FamilyName); s != "" {
			return s
		}
	}
	return ""
}

// inGroup reports whether the contact already carries the given label.
func (p *gPerson) inGroup(group string) bool {
	for _, m := range p.Memberships {
		if m.ContactGroupMembership != nil && m.ContactGroupMembership.ContactGroupResourceName == group {
			return true
		}
	}
	return false
}

// gGroup is a Google contact group, which Google Contacts calls a label.
type gGroup struct {
	ResourceName string `json:"resourceName"`
	Name         string `json:"name"`
	GroupType    string `json:"groupType"`
}

// contactsAPI is the slice of the People API the sync uses. Tests substitute a
// fake.
type contactsAPI interface {
	// ListContacts returns every contact (names, phones and emails only).
	ListContacts(ctx context.Context) ([]gPerson, error)
	// GetContact returns errNotFound when the contact was deleted.
	GetContact(ctx context.Context, resource string) (*gPerson, error)
	CreateContact(ctx context.Context, p *gPerson) (*gPerson, error)
	// UpdateContact replaces the named fields; p must carry the etag and
	// metadata from a fresh GetContact.
	UpdateContact(ctx context.Context, p *gPerson, fields []string) (*gPerson, error)
	ListGroups(ctx context.Context) ([]gGroup, error)
	CreateGroup(ctx context.Context, name string) (string, error)
	AddToGroup(ctx context.Context, group, resource string) error
}

// readFields are the person fields fetched before an update.
const readFields = "names,phoneNumbers,emailAddresses,addresses,urls,memberships,metadata"

type peopleClient struct{ hc *http.Client }

// newContactsClient builds an authorised client from the stored refresh token,
// the same token the calendar uses.
func newContactsClient(g *db.GoogleCalendar) (contactsAPI, error) {
	if gcalOAuth == nil {
		return nil, fmt.Errorf("contacts: GOOGLE_CLIENT_ID is not set")
	}
	if g == nil || g.RefreshToken == "" {
		return nil, fmt.Errorf("contacts: not connected")
	}
	src := gcalOAuth.TokenSource(context.Background(), &oauth2.Token{RefreshToken: g.RefreshToken})
	return &peopleClient{hc: oauth2.NewClient(context.Background(), src)}, nil
}

func (c *peopleClient) do(ctx context.Context, method, path string, body, out any) error {
	return googleJSON(ctx, c.hc, "contacts", method, peopleBase+path, body, out)
}

// maxContactPages caps ListContacts at 50,000 contacts, so a runaway page
// token can't loop forever.
const maxContactPages = 50

func (c *peopleClient) ListContacts(ctx context.Context) ([]gPerson, error) {
	var all []gPerson
	token := ""
	for range maxContactPages {
		q := url.Values{
			"personFields": {"names,phoneNumbers,emailAddresses"},
			"pageSize":     {"1000"},
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		var out struct {
			Connections   []gPerson `json:"connections"`
			NextPageToken string    `json:"nextPageToken"`
		}
		if err := c.do(ctx, http.MethodGet, "/people/me/connections?"+q.Encode(), nil, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Connections...)
		if out.NextPageToken == "" {
			return all, nil
		}
		token = out.NextPageToken
	}
	return nil, fmt.Errorf("contacts: more than %d pages of contacts", maxContactPages)
}

func (c *peopleClient) GetContact(ctx context.Context, resource string) (*gPerson, error) {
	var out gPerson
	if err := c.do(ctx, http.MethodGet, "/"+resource+"?personFields="+readFields, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *peopleClient) CreateContact(ctx context.Context, p *gPerson) (*gPerson, error) {
	var out gPerson
	if err := c.do(ctx, http.MethodPost, "/people:createContact?personFields="+readFields, p, &out); err != nil {
		return nil, err
	}
	if out.ResourceName == "" {
		return nil, fmt.Errorf("contacts: create returned no resource name")
	}
	return &out, nil
}

func (c *peopleClient) UpdateContact(ctx context.Context, p *gPerson, fields []string) (*gPerson, error) {
	q := url.Values{
		"updatePersonFields": {strings.Join(fields, ",")},
		"personFields":       {readFields},
	}
	var out gPerson
	if err := c.do(ctx, http.MethodPatch, "/"+p.ResourceName+":updateContact?"+q.Encode(), p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *peopleClient) ListGroups(ctx context.Context) ([]gGroup, error) {
	var out struct {
		ContactGroups []gGroup `json:"contactGroups"`
	}
	if err := c.do(ctx, http.MethodGet, "/contactGroups?pageSize=1000&groupFields=name,groupType", nil, &out); err != nil {
		return nil, err
	}
	return out.ContactGroups, nil
}

func (c *peopleClient) CreateGroup(ctx context.Context, name string) (string, error) {
	body := map[string]any{"contactGroup": map[string]string{"name": name}}
	var out gGroup
	if err := c.do(ctx, http.MethodPost, "/contactGroups", body, &out); err != nil {
		return "", err
	}
	if out.ResourceName == "" {
		return "", fmt.Errorf("contacts: create group returned no resource name")
	}
	return out.ResourceName, nil
}

func (c *peopleClient) AddToGroup(ctx context.Context, group, resource string) error {
	body := map[string][]string{"resourceNamesToAdd": {resource}}
	return c.do(ctx, http.MethodPost, "/"+group+"/members:modify", body, nil)
}
