package models

import "time"

type PurchaseRequest struct {
	ID                                       int
	FirstName                                string `valid:"required"`
	LastName                                 string `valid:"required"`
	Email                                    string `valid:"required,email"`
	PhoneNumber                              string `valid:"required"`
	RawType                                  string `valid:"required"`
	Type                                     PurchaseType
	GroupMember2, GroupMember3, GroupMember4 string
	RawAfterPartyCount                       string
	AfterPartyCount                          int
	PromoCode                                string
	Charged                                  float32

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

type PromoCode struct {
	ID      string
	Percent float32
	Amount  float32

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
	ID          string
	FirstName   string
	LastName    string
	PhoneNumber string
	Email       string

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}
