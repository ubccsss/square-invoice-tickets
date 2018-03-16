package square

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"
)

const (
	originURL               = "https://squareup.com"
	loginURL                = "https://squareup.com/login"
	loginPostURL            = "https://api.squareup.com/mp/login"
	navigationURL           = "https://squareup.com/dashboard/navigation"
	subunitsURL             = "https://squareup.com/api/v1/multiunit/subunits"
	invoiceServiceURL       = "https://squareup.com/services/squareup.invoice.service.InvoiceService/List"
	invoiceServiceCreateURL = "https://squareup.com/services/squareup.invoice.service.InvoiceService/Create"
	invoiceServiceCancelURL = "https://squareup.com/services/squareup.invoice.service.InvoiceService/Cancel"
)

var setupOnce sync.Once

type Error struct {
	Success      *bool  `json:"success"`
	ErrorTitle   string `json:"error_title"`
	ErrorMessage string `json:"error_message"`
}

func (c *Client) makeRequest(url string, body interface{}, get bool) ([]byte, int, error) {
	log.Printf("Hitting %s", url)

	var postBody bytes.Buffer
	if !get {
		if err := json.NewEncoder(&postBody).Encode(body); err != nil {
			return nil, 0, err
		}
		log.Println("req", postBody.String())
	}
	method := "POST"
	if get {
		method = "GET"
	}
	req, err := http.NewRequest(method, url, &postBody)
	if !get {
		req.Header.Set("Content-Type", "application/json")
	}
	csrf := ""
	for _, cookie := range c.http.Jar.Cookies(req.URL) {
		if cookie.Name == "_js_csrf" {
			csrf = cookie.Value
		}
	}
	req.Header.Set("X-CSRF-Token", csrf)
	if len(c.merchantToken) > 0 {
		req.Header.Set("X-Merchant-Token", c.merchantToken)
	}
	req.Header.Set("Host", "api.squareup.com")
	req.Header.Set("Origin", originURL)
	req.Header.Set("Referer", loginURL)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/47.0.2526.111 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	buf, _ := ioutil.ReadAll(resp.Body)
	var errResp Error
	if err := json.Unmarshal(buf, &errResp); err != nil {
		return nil, 0, err
	}
	if errResp.Success != nil && *errResp.Success == false {
		return nil, 0, fmt.Errorf("error %s", buf)
	}
	return buf, resp.StatusCode, nil
}

type Client struct {
	http                     *http.Client
	merchantToken, unitToken string
}

func NewCookies(rawCookies string) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	header := http.Header{}
	header.Add("Cookie", rawCookies)
	request := http.Request{
		Header: header,
	}
	cookies := request.Cookies()
	for _, c := range cookies {
		c.Domain = ".squareup.com"
	}

	url, err := url.Parse("http://squareup.com/")
	if err != nil {
		return nil, err
	}
	jar.SetCookies(url, cookies)

	c := &Client{
		http: &http.Client{
			Jar: jar,
		},
	}
	if err := c.init(); err != nil {
		return nil, err
	}
	return c, nil
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
	if err := c.init(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) init() error {
	nav, err := c.GetNavigation()
	if err != nil {
		return err
	}
	c.merchantToken = nav.Token

	subunits, err := c.GetSubUnits()
	if err != nil {
		return err
	}
	if len(subunits.Entities) > 0 {
		entity := subunits.Entities[0]
		c.unitToken = entity.Token
	}
	return nil
}

type NavigationResponse struct {
	Merchant string `json:"merchant"`
	Token    string `json:"token"`
}

func (c *Client) GetNavigation() (*NavigationResponse, error) {
	body, code, err := c.makeRequest(navigationURL, nil, false)
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
	Nickname   string `json:"nickname"`
	Token      string `json:"token"`
	UnitActive bool   `json:"unit_active"`
}
type SubUnitResponse struct {
	Entities []*Entity
}

func (c *Client) GetSubUnits() (*SubUnitResponse, error) {
	body, code, err := c.makeRequest(subunitsURL, nil, true)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("error getting subunits %d, %s", code, body)
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

func (c *Client) getCSRF() error {
	loginResp, err := c.http.Get(loginURL)
	if err != nil {
		return err
	}
	loginResp.Body.Close()
	return nil
}

func (c *Client) login(email, pass string) error {
	if err := c.getCSRF(); err != nil {
		return err
	}

	req := LoginRequest{email, pass}
	body, code, err := c.makeRequest(loginPostURL, &req, false)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("error logging into square %d, %s", code, body)
	}
	return nil
}

type InvoiceListRequest struct {
	Count     int    `json:"count"`
	UnitToken string `json:"unit_token"`
}

type Money struct {
	Amount       int    `json:"amount"`
	CurrencyCode string `json:"currency_code"`
}

type Payer struct {
	DisplayName string  `json:"display_name"`
	Email       string  `json:"email"`
	Token       *string `json:"token"`
}

type DueDate struct {
	DayOfMonth  int `json:"day_of_month"`
	MonthOfYear int `json:"month_of_year"`
	Year        int `json:"year"`
}

func (d DueDate) FromTime(t time.Time) *DueDate {
	d.Year = t.Year()
	d.MonthOfYear = int(t.Month())
	d.DayOfMonth = t.Day()

	return &d
}

type Time struct {
	InstantUsec       uint64   `json:"instant_usec"`
	TimezoneOffsetMin int      `json:"timezone_offset_min"`
	TZName            []string `json:"tz_name"`
}

func (t Time) Time() time.Time {
	return time.Unix(int64(t.InstantUsec), 0)
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

	Cart *Cart `json:"cart"`
}

type InvoiceListResponse struct {
	NextCursor string     `json:"next_cursor"`
	Invoice    []*Invoice `json:"invoice"`
}

func (c *Client) Invoices() ([]*Invoice, error) {
	url := invoiceServiceURL
	req := InvoiceListRequest{10000000, c.unitToken}
	body, code, err := c.makeRequest(url, &req, false)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("error fetching square invoices %d, %s", code, body)
	}
	var resp InvoiceListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Invoice, nil
}

type Amounts struct {
	AppliedMoney                         *Money `json:"applied_money,omitempty"`
	DiscountMoney                        *Money `json:"discount_money,omitempty"`
	GrossSalesMoney                      *Money `json:"gross_sales_money,omitempty"`
	ItemVariationPriceMoney              *Money `json:"item_variation_price_money,omitempty"`
	ItemVariationPriceTimesQuantityMoney *Money `json:"item_variation_price_times_quantity_money,omitempty"`
	TaxMoney                             *Money `json:"tax_money,omitempty"`
	TipMoney                             *Money `json:"tip_money,omitempty"`
	TotalMoney                           *Money `json:"total_money,omitempty"`
	VariableAmountMoney                  *Money `json:"variable_amount_money,omitempty"`
}
type Cart struct {
	Amounts   *Amounts   `json:"amounts"`
	LineItems *LineItems `json:"line_items"`
}
type DiscountDetails struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Amount       *Money `json:"amount"`
	DiscountType string `json:"discount_type"` // VARIABLE_AMOUNT

}
type WriteOnlyBackingDetails struct {
	Discount    *DiscountDetails `json:"discount"`
	BackingType string           `json:"backing_type"` // CUSTOM_DISCOUNT
}
type Discount struct {
	WriteOnlyBackingDetails *WriteOnlyBackingDetails `json:"write_only_backing_details"`
	Amounts                 *Amounts                 `json:"amounts"`
	Configuration           *Amounts                 `json:"configuration"`
	ApplicationScope        string                   `json:"application_scope"` // CART_LEVEL
}
type Options struct {
	Discount []*Discount `json:"discount"`
	Fee      []struct{}  `json:"fee"`
}
type Configuration struct {
	SelectedOptions         *Options `json:"selected_options"`
	BackingType             string   `json:"backing_type"`
	ItemVariationPriceMoney *Money   `json:"item_variation_price_money"`
}

type Item struct {
	Quantity      string         `json:"quantity"`
	CustomNote    string         `json:"custom_note"`
	Configuration *Configuration `json:"configuration"`
	Amounts       *Amounts       `json:"amounts"`
}

type LineItems struct {
	Itemization []*Item     `json:"itemization"`
	Fee         []struct{}  `json:"fee"`
	Discount    []*Discount `json:"discount"`
}

type InvoiceCreateRequest struct {
	AdditionalRecipientEmail []struct{} `json:"additional_recipient_email"`
	Cart                     *Cart      `json:"cart"`
	Description              string     `json:"description"`
	DueOn                    *DueDate   `json:"due_on"`
	InvoiceName              string     `json:"invoice_name"`
	MerchantInvoiceNumber    string     `json:"merchant_invoice_number"`
	Payer                    *Payer     `json:"payer"`
	RequestedMoney           *Money     `json:"requested_money"`
	IsDraft                  bool       `json:"is_draft"`
	UnitToken                string     `json:"unit_token"`
}

type InvoiceResponse struct {
	Success bool     `json:"success"`
	Invoice *Invoice `json:"invoice"`
}

func (c *Client) CreateInvoice(req *InvoiceCreateRequest) (*Invoice, error) {
	req.UnitToken = c.unitToken

	body, code, err := c.makeRequest(invoiceServiceCreateURL, req, false)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("error creating square invoice %d, %s", code, body)
	}
	log.Printf("resp %s", body)
	var resp InvoiceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Invoice, nil
}

type InvoiceCancelRequest struct {
	SendEmailToRecipients bool   `json:"send_email_to_recipients"`
	Token                 string `json:"token"`
}

func (c *Client) CancelInvoice(req *InvoiceCancelRequest) (*Invoice, error) {
	body, code, err := c.makeRequest(invoiceServiceCancelURL, req, false)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("error cancelling square invoice %d, %s", code, body)
	}
	log.Printf("resp %s", body)
	var resp InvoiceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Invoice, nil
}
