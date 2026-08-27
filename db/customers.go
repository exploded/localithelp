package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"localithelp/db/sqlc"
)

// Customer is a person we do work for. No login — customers only ever receive
// tokenised links by email. Bookings and invoices reference customers by id.
type Customer struct {
	ID        int64
	Name      string
	Email     string // lower-cased
	Phone     string
	Address   string
	Suburb    string
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func sqlcCustomer(r sqlc.Customer) *Customer {
	return &Customer{
		ID: r.ID, Name: r.Name, Email: r.Email, Phone: r.Phone, Address: r.Address,
		Suburb: r.Suburb, Notes: r.Notes,
		CreatedAt: parseUTC(r.CreatedAt), UpdatedAt: parseUTC(r.UpdatedAt),
	}
}

func GetCustomer(id int64) (*Customer, error) {
	r, err := q.GetCustomer(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return sqlcCustomer(r), nil
}

// FindOrCreateCustomer matches an enquiry to an existing customer by email
// (then by phone digits) or creates one. Existing customers get blank contact
// fields filled in from the enquiry, but never overwritten.
func FindOrCreateCustomer(name, email, phone, suburb string) (int64, error) {
	return findOrCreateCustomer(q, name, email, phone, suburb)
}

func findOrCreateCustomer(qq *sqlc.Queries, name, email, phone, suburb string) (int64, error) {
	ctx := context.Background()
	email = strings.ToLower(strings.TrimSpace(email))
	phone = strings.TrimSpace(phone)
	norm := NormalizePhone(phone)

	var found *sqlc.Customer
	if email != "" {
		if r, err := qq.GetCustomerByEmail(ctx, email); err == nil {
			found = &r
		} else if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("find customer by email: %w", err)
		}
	}
	if found == nil && norm != "" {
		if r, err := qq.GetCustomerByPhoneNorm(ctx, norm); err == nil {
			found = &r
		} else if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("find customer by phone: %w", err)
		}
	}
	if found != nil {
		// Only fill in the email if no other customer already owns it (unique index).
		fillEmail := email
		if found.Email == "" && email != "" {
			if _, err := qq.GetCustomerByEmail(ctx, email); err == nil {
				fillEmail = ""
			}
		}
		if err := qq.TouchCustomerContact(ctx, sqlc.TouchCustomerContactParams{
			Name: name, Email: fillEmail, Phone: phone, PhoneNorm: norm, Suburb: suburb, ID: found.ID,
		}); err != nil {
			return 0, fmt.Errorf("touch customer: %w", err)
		}
		return found.ID, nil
	}
	id, err := qq.InsertCustomer(ctx, sqlc.InsertCustomerParams{
		Name: name, Email: email, Phone: phone, PhoneNorm: norm, Suburb: suburb,
	})
	if err != nil {
		return 0, fmt.Errorf("insert customer: %w", err)
	}
	return id, nil
}

// FindCustomer returns the customer matching email (then phone digits), or nil
// when nobody matches. Same matching rules as FindOrCreateCustomer without the
// create, so a caller can tell "this is a new person" from "you already have
// them" before adding a duplicate.
func FindCustomer(email, phone string) (*Customer, error) {
	ctx := context.Background()
	if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
		r, err := q.GetCustomerByEmail(ctx, email)
		if err == nil {
			return sqlcCustomer(r), nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("find customer by email: %w", err)
		}
	}
	if norm := NormalizePhone(phone); norm != "" {
		r, err := q.GetCustomerByPhoneNorm(ctx, norm)
		if err == nil {
			return sqlcCustomer(r), nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("find customer by phone: %w", err)
		}
	}
	return nil, nil
}

func UpdateCustomer(c *Customer) error {
	return q.UpdateCustomer(context.Background(), sqlc.UpdateCustomerParams{
		Name: c.Name, Email: strings.ToLower(strings.TrimSpace(c.Email)), Phone: c.Phone,
		PhoneNorm: NormalizePhone(c.Phone), Address: c.Address, Suburb: c.Suburb, Notes: c.Notes, ID: c.ID,
	})
}

// ListCustomers returns all customers, or those matching query (name, email,
// phone digits or suburb) when it is non-empty.
func ListCustomers(query string) ([]Customer, error) {
	ctx := context.Background()
	var rows []sqlc.Customer
	var err error
	if query = strings.TrimSpace(query); query == "" {
		rows, err = q.ListCustomers(ctx)
	} else {
		like := "%" + query + "%"
		phoneLike := "%" + NormalizePhone(query) + "%"
		if NormalizePhone(query) == "" {
			phoneLike = "\x00" // never matches
		}
		rows, err = q.SearchCustomers(ctx, sqlc.SearchCustomersParams{
			Name: like, Email: like, PhoneNorm: phoneLike, Suburb: like,
		})
	}
	if err != nil {
		return nil, err
	}
	out := make([]Customer, len(rows))
	for i, r := range rows {
		out[i] = *sqlcCustomer(r)
	}
	return out, nil
}

// BackfillCustomers links legacy bookings (customer_id = 0) to customers,
// creating them as needed. Idempotent: it only touches unlinked, non-spam rows,
// so it is safe to run on every boot.
func BackfillCustomers() error {
	ctx := context.Background()
	rows, err := q.ListUnlinkedBookings(ctx)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	qtx := q.WithTx(tx)
	for _, b := range rows {
		id, err := findOrCreateCustomer(qtx, b.Name, b.Email, b.Phone, b.Suburb)
		if err != nil {
			return fmt.Errorf("booking #%d: %w", b.ID, err)
		}
		if err := qtx.SetBookingCustomer(ctx, sqlc.SetBookingCustomerParams{CustomerID: id, ID: b.ID}); err != nil {
			return fmt.Errorf("link booking #%d: %w", b.ID, err)
		}
	}
	return tx.Commit()
}
