package models

import (
	"fmt"
	"time"

	"github.com/d4l3k/square-invoice-tickets/square"
)

type PurchaseRequest struct {
	ID          int
	FirstName   string `valid:"required"`
	LastName    string `valid:"required"`
	Email       string `valid:"required,email"`
	PhoneNumber string `valid:"required"`
	RawType     string `valid:"required"`
	Type        PurchaseType

	Status  string
	Invoice *square.Invoice

	GroupMember2FirstName   string
	GroupMember2LastName    string
	GroupMember2Email       string
	GroupMember2PhoneNumber string

	GroupMember3FirstName   string
	GroupMember3LastName    string
	GroupMember3Email       string
	GroupMember3PhoneNumber string

	GroupMember4FirstName   string
	GroupMember4LastName    string
	GroupMember4Email       string
	GroupMember4PhoneNumber string

	RawAfterPartyCount string
	AfterPartyCount    int
	PromoCode          string
	Charged            float64

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time

	Tickets []Ticket
}

type PromoCode struct {
	ID      string
	Percent float64
	Amount  float64
	Count   int

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

type PurchaseType int

const (
	Individual PurchaseType = iota
	Group
)

type Ticket struct {
	ID                string
	PurchaseRequestID int
	FirstName         string
	LastName          string
	PhoneNumber       string
	Email             string

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

func (t Ticket) URL() string {
	return "http://tickets.ubccsss.org/ticket/" + t.ID
}
func (t Ticket) HTML() string {
	return fmt.Sprintf(`%s %s <a href="%s">%s</a><br>`, t.FirstName, t.LastName, t.URL(), t.URL())
}
