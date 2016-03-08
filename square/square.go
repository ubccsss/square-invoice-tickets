package square

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"sync"
)

const (
	originURL         = "https://squareup.com"
	loginURL          = "https://squareup.com/login"
	loginPostURL      = "https://api.squareup.com/mp/login"
	navigationURL     = "https://squareup.com/dashboard/navigation"
	subunitsURL       = "https://squareup.com/api/v1/multiunit/subunits"
	invoiceServiceURL = "https://squareup.com/services/squareup.invoice.service.InvoiceService/List"
)

var setupOnce sync.Once

func (c *Client) makeRequest(url string, body interface{}) ([]byte, int, error) {
	log.Printf("Hitting %s", url)

	loginResp, err := http.Get(loginURL)
	if err != nil {
		return nil, 0, err
	}
	loginResp.Body.Close()

	jsonStr, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))
	req.Header.Set("Content-Type", "application/json")
	cookies := http.DefaultClient.Jar.Cookies(req.URL)
	csrf := ""
	for _, cookie := range cookies {
		if cookie.Name == "_js_csrf" {
			csrf = cookie.Value
		}
	}
	req.Header.Set("X-Csrf-Token", csrf)
	req.Header.Set("Host", "api.squareup.com")
	req.Header.Set("Origin", originURL)
	req.Header.Set("Referer", loginURL)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/47.0.2526.111 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	buf, _ := ioutil.ReadAll(resp.Body)
	return buf, resp.StatusCode, nil
}

type Client struct {
	http *http.Client
}

func New(user, pass string) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	c := &Client{
		http: &http.Client{
			Jar: jar,
		},
	}
	if err := c.login(user, pass); err != nil {
		return nil, err
	}
	return c, err
}

type NavigationResponse struct {
	Merchant string `json:"merchant"`
	Token    string `json:"token"`
}

func (c *Client) GetNavigation() (*NavigationResponse, error) {
	body, code, err := c.makeRequest(navigationURL, nil)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("error getting navigation %d, %s", code, body)
	}
	var resp NavigationResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type Entity struct {
	Email      string `json:"email"`
	Nickname   string `json:"token"`
	Token      string `json:"token"`
	UnitActive bool   `json:"unit_active"`
}
type SubUnitResponse struct {
	Entities []*Entity
}

func (c *Client) GetSubUnits() (*SubUnitResponse, error) {
	body, code, err := c.makeRequest(subunitsURL, nil)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("error getting navigation %d, %s", code, body)
	}
	var resp SubUnitResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (c *Client) login(email, pass string) error {
	req := LoginRequest{email, pass}
	body, code, err := c.makeRequest(loginPostURL, &req)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("error logging into square %d, %s", code, body)
	}
	return nil
}

type InvoiceListRequest struct {
	Count int `json:"count"`
}

type Money struct {
	Amount       int    `json:"amount"`
	CurrencyCode string `json:"currency_code"`
}

type Payer struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Token       string `json:"token"`
}

type DueDate struct {
	DayOfMonth  int `json:"day_of_month"`
	MonthOfYear int `json:"month_of_year"`
	Year        int `json:"year"`
}

type Time struct {
	InstantUsec       uint64   `json:"instant_usec"`
	TimezoneOffsetMin int      `json:"timezone_offset_min"`
	TZName            []string `json:"tz_name"`
}

type Invoice struct {
	BuyerEnteredInstrumentEnabled bool   `json:"buyer_entered_instrument_enabled"`
	CanBeScheduled                bool   `json:"can_be_scheduled"`
	DeliveryStatus                string `json:"delivery_status"`
	Description                   string `json:"description"`
	InvoiceName                   string `json:"invoice_name"`
	LockVersion                   int    `json:"lock_version"`
	MerchantInvoiceNumber         string `json:"merchant_invoice_number"`
	MerchantToken                 string `json:"merchant_token"`
	PayerEmail                    string `json:"payer_email"`
	PayerName                     string `json:"payer_name"`
	State                         string `json:"state"`
	TippingEnabled                bool   `json:"tipping_enabled"`
	Token                         string `json:"token"`
	UnitToken                     string `json:"unit_token"`

	RequestedMoney *Money   `json:"requested_money"`
	Payer          *Payer   `json:"payer"`
	DueOn          *DueDate `json:"due_on"`
	CreatedAt      *Time    `json:"created_at"`
	SentAt         *Time    `json:"sent_at"`
	UpdatedAt      *Time    `json:"updated_at"`

	/*
		cart: {line_items: {itemization: [{quantity: "1", custom_note: "Year End Tickets",…}], discount: [,…]},…}
	*/
}
type InvoiceListResponse struct {
	NextCursor string     `json:"next_cursor"`
	Invoice    []*Invoice `json:"invoice"`
}

func (c *Client) Invoices() ([]*Invoice, error) {
	url := invoiceServiceURL
	req := InvoiceListRequest{10000000}
	body, code, err := c.makeRequest(url, &req)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("error logging into square %d, %s", code, body)
	}
	var resp InvoiceListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Invoice, nil
}
