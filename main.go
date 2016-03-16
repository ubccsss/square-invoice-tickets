package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/abbot/go-http-auth"
	"github.com/asaskevich/govalidator"
	"github.com/d4l3k/square-invoice-tickets/email"
	"github.com/d4l3k/square-invoice-tickets/models"
	"github.com/d4l3k/square-invoice-tickets/square"
	"github.com/dustinkirkland/golang-petname"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/jinzhu/gorm"

	_ "github.com/mattn/go-sqlite3"
)

var (
	addr          = flag.String("addr", ":8383", "the address to listen on")
	debug         = flag.Bool("debug", false, "whether to run in debug mode")
	adminPassword = flag.String("pass", "", "the md5 hash of the admin password")

	squareEmail = flag.String("squareEmail", "", "the square email address")
	squarePass  = flag.String("squarePass", "", "the square password")
	currency    = flag.String("currency", "CAD", "the currency to use")

	priceGroup      = flag.Float64("priceGroup", 80, "the price for group tickets")
	priceIndividual = flag.Float64("priceIndividual", 25, "the price for individual tickets")
	maxTickets      = flag.Int("maxTickets", 18, "the number of tickets that can be sold")

	poll = flag.Bool("poll", true, "whether to poll square")
)

func main() {
	flag.Parse()
	rand.Seed(time.Now().UTC().UnixNano())

	s, err := newServer()
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(s.listen())
}

type server struct {
	db gorm.DB
}

func newServer() (*server, error) {
	s := &server{}
	db, err := gorm.Open("sqlite3", "tickets.db")
	if err != nil {
		return nil, err
	}
	s.db = db

	if err := db.AutoMigrate(&models.PurchaseRequest{}).Error; err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&models.PromoCode{}).Error; err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&models.Ticket{}).Error; err != nil {
		return nil, err
	}

	log.Printf("Password hash %s", *adminPassword)
	auth := auth.NewBasicAuthenticator("localhost:8383", s.secret)

	r := mux.NewRouter()

	api := r.PathPrefix("/api").Subrouter()
	api.HandleFunc("/purchaseRequests", auth.Wrap(s.purchaseRequests))
	api.HandleFunc("/promoCodes", auth.Wrap(s.promoCodes))
	api.HandleFunc("/tickets", auth.Wrap(s.tickets))
	api.HandleFunc("/square", auth.Wrap(s.square))
	api.HandleFunc("/stats", auth.Wrap(s.stats))
	api.HandleFunc("/ticket/{id}", s.ticket)
	api.HandleFunc("/details", s.details)

	apiPost := api.Methods("POST").Subrouter()
	apiPost.HandleFunc("/buy", s.buy)

	r.HandleFunc("/", index)
	r.PathPrefix("/").Handler(NotFoundHook{http.FileServer(http.Dir("./static/"))})
	http.Handle("/", r)

	return s, nil
}

type hookedResponseWriter struct {
	http.ResponseWriter
	r      *http.Request
	ignore bool
}

func (hrw *hookedResponseWriter) WriteHeader(status int) {
	if status == 404 {
		url := hrw.r.URL.String()
		if !(strings.HasPrefix(url, "/api") ||
			strings.HasPrefix(url, "/elements") ||
			strings.HasPrefix(url, "/bower_components")) {

			hrw.ignore = true
			index(hrw.ResponseWriter, hrw.r)
			return
		}
	}
	hrw.ResponseWriter.WriteHeader(status)
}

func (hrw *hookedResponseWriter) Write(p []byte) (int, error) {
	if hrw.ignore {
		return 0, nil
	}
	return hrw.ResponseWriter.Write(p)
}

type NotFoundHook struct {
	h http.Handler
}

func (nfh NotFoundHook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	nfh.h.ServeHTTP(&hookedResponseWriter{ResponseWriter: w, r: r}, r)
}

func (s *server) listen() error {
	if *poll {
		go s.pollSquare()
	}

	log.Printf("Listening on %s", *addr)
	return http.ListenAndServe(*addr, handlers.LoggingHandler(os.Stdout, http.DefaultServeMux))
}

func index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	if *debug {
		http.ServeFile(w, r, "static/app.html")
	} else {
		http.ServeFile(w, r, "static/index.html")
	}
}

func (s *server) purchaseRequests(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	w.Header().Set("Content-Type", "application/json")
	var records []*models.PurchaseRequest
	if err := s.db.Find(&records).Error; err != nil {
		s.err(w, err, 500)
		return
	}
	m := make(map[int]*square.Invoice)
	sq, err := square.New(*squareEmail, *squarePass)
	if err != nil {
		s.err(w, err, 500)
		return
	}
	invoices, err := sq.Invoices()
	if err != nil {
		s.err(w, err, 500)
		return
	}
	for _, invoice := range invoices {
		if !strings.HasPrefix(invoice.MerchantInvoiceNumber, "PurchaseRequest ") {
			continue
		}
		bits := strings.Split(invoice.MerchantInvoiceNumber, " ")
		if len(bits) != 2 {
			continue
		}
		id, err := strconv.Atoi(bits[1])
		if err != nil {
			log.Println("invoice parse err", err)
			continue
		}
		m[id] = invoice
	}
	for _, pr := range records {
		invoice, ok := m[pr.ID]
		if !ok {
			pr.Status = "NO_INVOICE"
			continue
		}
		pr.Status = invoice.State + " - " + invoice.DeliveryStatus
		pr.Invoice = invoice
	}

	if err := json.NewEncoder(w).Encode(records); err != nil {
		s.err(w, err, 500)
		return
	}
}

func (s *server) ticket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	w.Header().Set("Content-Type", "application/json")
	var records models.Ticket
	if err := s.db.Where("id = ?", vars["id"]).Find(&records).Error; err != nil {
		s.err(w, err, 400)
		return
	}
	if records.ID == "" {
		s.err(w, fmt.Errorf("ticket %s not found", records.ID), 404)
		return
	}
	ticket := models.Ticket{
		ID:          records.ID,
		FirstName:   records.FirstName,
		LastName:    records.LastName,
		PhoneNumber: records.PhoneNumber,
		Email:       records.Email,
	}
	if err := json.NewEncoder(w).Encode(ticket); err != nil {
		s.err(w, err, 500)
		return
	}
}

func (s *server) square(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	w.Header().Set("Content-Type", "application/json")
	sq, err := square.New(*squareEmail, *squarePass)
	if err != nil {
		s.err(w, err, 500)
		return
	}
	invoices, err := sq.Invoices()
	if err != nil {
		s.err(w, err, 500)
		return
	}
	if err := json.NewEncoder(w).Encode(invoices); err != nil {
		s.err(w, err, 500)
		return
	}
}

type Stats struct {
	Tickets, PurchaseRequests, PeopleCount, AfterPartyCount int
}

func (s *server) stats(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	w.Header().Set("Content-Type", "application/json")

	stats := &Stats{}

	if err := s.db.Model(&models.Ticket{}).Count(&stats.Tickets).Error; err != nil {
		s.err(w, err, 500)
		return
	}

	var reqs []*models.PurchaseRequest
	if err := s.db.Model(&models.PurchaseRequest{}).Find(&reqs).Error; err != nil {
		s.err(w, err, 500)
		return
	}
	stats.PurchaseRequests = len(reqs)
	for _, req := range reqs {
		stats.AfterPartyCount += req.AfterPartyCount
		switch req.Type {
		case models.Individual:
			stats.PeopleCount += 1
		case models.Group:
			stats.PeopleCount += 4
		}
	}
	json.NewEncoder(w).Encode(stats)
}

func (s *server) tickets(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "POST":
		var req models.Ticket
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.err(w, err, 400)
			return
		}
		req.ID = petname.Generate(3, "-")
		if err := s.db.Create(&req).Error; err != nil {
			s.err(w, err, 500)
			return
		}
	case "PATCH":
		var req models.Ticket
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.err(w, err, 400)
			return
		}
		if err := s.db.Where("id = ?", req.ID).Save(&req).Error; err != nil {
			s.err(w, err, 500)
			return
		}
	case "DELETE":
		var req []*models.Ticket
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.err(w, err, 400)
			return
		}
		for _, ticket := range req {
			if err := s.db.Delete(ticket).Error; err != nil {
				s.err(w, err, 400)
				return
			}
		}
	case "GET":
	default:
		s.err(w, fmt.Errorf("unknown method %s", r.Method), 400)
		return
	}
	var records []*models.Ticket
	if err := s.db.Find(&records).Error; err != nil {
		s.err(w, err, 500)
		return
	}
	if err := json.NewEncoder(w).Encode(records); err != nil {
		s.err(w, err, 500)
		return
	}
}

func (s *server) promoCodes(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == "POST" {
		var req models.PromoCode
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.err(w, err, 400)
			return
		}
		if err := s.db.Create(&req).Error; err != nil {
			s.err(w, err, 500)
			return
		}
	} else if r.Method == "PATCH" {
		var req models.PromoCode
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.err(w, err, 400)
			return
		}
		if err := s.db.Where("id = ?", req.ID).Save(&req).Error; err != nil {
			s.err(w, err, 500)
			return
		}
	}

	var records []*models.PromoCode
	if err := s.db.Find(&records).Error; err != nil {
		s.err(w, err, 500)
		return
	}
	if err := json.NewEncoder(w).Encode(records); err != nil {
		s.err(w, err, 500)
		return
	}
}

func (s *server) getPromoCode(code string) (*models.PromoCode, error) {
	var promoCode *models.PromoCode
	if code != "" {
		var pc models.PromoCode
		if err := s.db.Where("id = ?", code).First(&pc).Error; err != nil {
			return nil, err
		}
		if pc.Count == 0 {
			return nil, nil
		}
		promoCode = &pc
	}
	return promoCode, nil
}

func (s *server) priceEstimate(req *models.PurchaseRequest) (float64, error) {
	// No promo codes for groups
	if req.Type == models.Group {
		return *priceGroup, nil
	}

	// Price code
	basePrice := *priceIndividual

	promoCode, err := s.getPromoCode(req.PromoCode)
	if err != nil {
		return 0, err
	}

	if promoCode != nil {
		basePrice = basePrice*(1-promoCode.Percent) - promoCode.Amount
	}

	return basePrice, nil
}

type DetailsResponse struct {
	PromoCode *models.PromoCode
	Price     string
}

func (s *server) details(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	req := &models.PurchaseRequest{
		PromoCode: r.FormValue("code"),
		RawType:   r.FormValue("type"),
	}
	if err := processReq(req); err != nil {
		s.err(w, err, 400)
		return
	}
	var promoCode *models.PromoCode
	if req.Type == models.Individual {
		pc, err := s.getPromoCode(req.PromoCode)
		if err != nil {
			s.err(w, err, 400)
			return
		}
		promoCode = pc
	}
	price, err := s.priceEstimate(req)
	if err != nil {
		s.err(w, err, 500)
		return
	}
	json.NewEncoder(w).Encode(DetailsResponse{PromoCode: promoCode, Price: fmt.Sprintf("%.2f", price)})
}

func processReq(req *models.PurchaseRequest) error {
	if strings.Contains(req.RawType, "Group") {
		req.Type = models.Group
	}
	if len(req.RawAfterPartyCount) > 0 {
		count, err := strconv.Atoi(req.RawAfterPartyCount)
		if err != nil {
			return err
		}
		req.AfterPartyCount = count
	}
	return nil
}

func (s *server) buy(w http.ResponseWriter, r *http.Request) {
	var req models.PurchaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.err(w, err, 400)
		return
	}
	if err := processReq(&req); err != nil {
		s.err(w, err, 400)
		return
	}
	if err := s.ValidatePurchaseRequest(&req); err != nil {
		s.err(w, err, 400)
		return
	}

	price, err := s.priceEstimate(&req)
	if err != nil {
		s.err(w, err, 500)
		return
	}
	req.Charged = price

	if err := s.db.Create(&req).Error; err != nil {
		s.err(w, err, 500)
		return
	}

	if req.PromoCode != "" {
		promoCode, err := s.getPromoCode(req.PromoCode)
		if err != nil {
			s.err(w, err, 500)
			return
		}
		if promoCode.Count > 0 {
			promoCode.Count -= 1
			if err := s.db.Save(promoCode).Error; err != nil {
				s.err(w, err, 500)
				return
			}
		}
	}

	if err := SendInvoice(&req); err != nil {
		s.err(w, err, 500)
		return
	}
}

type err struct {
	Error string
}

func (s *server) err(w http.ResponseWriter, sendErr error, status int) {
	body, err := json.Marshal(err{sendErr.Error()})
	if err != nil {
		http.Error(w, sendErr.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}

func (s *server) ValidatePurchaseRequest(pr *models.PurchaseRequest) error {
	needed := 1
	if pr.Type == models.Group {
		needed = 4
	}
	count := 0
	if err := s.db.Model(&models.Ticket{}).Count(&count).Error; err != nil {
		return err
	}
	if count+needed > *maxTickets {
		return fmt.Errorf("Sorry, there are %d tickets available. This event may be sold out, or you need to check back later.", *maxTickets-count)
	}

	promoCode, err := s.getPromoCode(pr.PromoCode)
	if err != nil {
		return err
	}
	if pr.PromoCode != "" && promoCode == nil {
		return fmt.Errorf("Invalid promo code: %s", pr.PromoCode)
	}
	if _, err := govalidator.ValidateStruct(pr); err != nil {
		return err
	}
	return nil
}

func (s server) secret(user, realm string) string {
	if user == "admin" {
		return *adminPassword
	}
	return ""
}

func newTicket(first, last, phone, email string) models.Ticket {
	id := petname.Generate(3, "-")
	return models.Ticket{
		ID:          id,
		FirstName:   first,
		LastName:    last,
		PhoneNumber: phone,
		Email:       email,
	}
}

func SendInvoice(pr *models.PurchaseRequest) error {
	amt := &square.Money{
		Amount:       int(pr.Charged * 100),
		CurrencyCode: *currency,
	}
	none := &square.Money{
		Amount:       0,
		CurrencyCode: *currency,
	}
	req := &square.InvoiceCreateRequest{
		AdditionalRecipientEmail: make([]struct{}, 0),
		Cart: &square.Cart{
			Amounts: &square.Amounts{
				DiscountMoney: none,
				TaxMoney:      none,
				TipMoney:      none,
				TotalMoney:    amt,
			},
			LineItems: &square.LineItems{
				Itemization: []*square.Item{
					{
						Amounts: &square.Amounts{
							DiscountMoney:                        none,
							GrossSalesMoney:                      amt,
							ItemVariationPriceMoney:              amt,
							ItemVariationPriceTimesQuantityMoney: amt,
							TaxMoney:   none,
							TotalMoney: amt,
						},
						Configuration: &square.Configuration{
							BackingType:             "CUSTOM_AMOUNT",
							ItemVariationPriceMoney: amt,
							SelectedOptions: &square.Options{
								Discount: make([]*square.Discount, 0),
								Fee:      make([]struct{}, 0),
							},
						},
						CustomNote: "CSSS Year End Gala Ticket - " + pr.RawType,
						Quantity:   "1",
					},
				},
				Discount: make([]*square.Discount, 0),
				Fee:      make([]struct{}, 0),
			},
		},
		DueOn:                 square.DueDate{}.FromTime(time.Now().Add(24 * time.Hour)),
		InvoiceName:           "Happily Ever After - CSSS Year End Gala Tickets",
		IsDraft:               false,
		MerchantInvoiceNumber: fmt.Sprintf("PurchaseRequest %d", pr.ID),
		Payer: &square.Payer{
			DisplayName: pr.FirstName + " " + pr.LastName,
			Email:       pr.Email,
		},
		RequestedMoney: amt,
	}
	sq, err := square.New(*squareEmail, *squarePass)
	if err != nil {
		return err
	}
	invoice, err := sq.CreateInvoice(req)
	if err != nil {
		return err
	}
	log.Printf("invoice %+v", invoice)
	return nil
}

func (s *server) pollSquare() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for _ = range ticker.C {
		sq, err := square.New(*squareEmail, *squarePass)
		if err != nil {
			log.Println("square err", err)
			continue
		}
		invoices, err := sq.Invoices()
		if err != nil {
			log.Println("square err", err)
			continue
		}
		log.Printf("invoices %+v", invoices)
		for _, invoice := range invoices {
			if !strings.HasPrefix(invoice.MerchantInvoiceNumber, "PurchaseRequest ") {
				continue
			}
			bits := strings.Split(invoice.MerchantInvoiceNumber, " ")
			if len(bits) != 2 {
				continue
			}
			id, err := strconv.Atoi(bits[1])
			if err != nil {
				log.Println("invoice parse err", err)
				continue
			}
			var pr models.PurchaseRequest
			query := s.db.Find(&pr, id)
			if err := query.Error; err != nil {
				log.Println("db err", err)
				continue
			}
			if err := query.Association("Tickets").Find(&pr.Tickets).Error; err != nil {
				log.Println("db tickets err", err)
				continue
			}
			if len(pr.Tickets) != 0 {
				continue
			}
			if invoice.State == "PAID" {
				log.Printf("Found paid invoice %+v %+v", invoice, pr)
				var tickets []models.Ticket
				tickets = append(tickets, newTicket(pr.FirstName, pr.LastName, pr.PhoneNumber, pr.Email))

				if pr.Type == models.Group {
					tickets = append(tickets, newTicket(pr.GroupMember2FirstName,
						pr.GroupMember2LastName, pr.GroupMember2PhoneNumber, pr.GroupMember2Email))
					tickets = append(tickets, newTicket(pr.GroupMember3FirstName,
						pr.GroupMember3LastName, pr.GroupMember3PhoneNumber, pr.GroupMember3Email))
					tickets = append(tickets, newTicket(pr.GroupMember4FirstName,
						pr.GroupMember4LastName, pr.GroupMember4PhoneNumber, pr.GroupMember4Email))
				}
				for _, ticket := range tickets {
					if err := s.db.Create(&ticket).Error; err != nil {
						log.Println("db err", err)
						return
					}
				}
				if err := s.db.First(&pr, id).Update("Tickets", tickets).Error; err != nil {
					log.Println("db err", err)
					continue
				}
				for i, ticket := range tickets {
					body := `<p>Hey ` + ticket.FirstName + `,</p>
					<p>Here's your tickets for Happily Ever After - CSSS Year End Gala:</p>
					<p>`
					body += ticket.HTML()

					if i == 0 {
						for _, ticket := range tickets[1:] {
							body += ticket.HTML()
						}
					}
					body += `</p><p>See you at the gala!<br>The CSSS</p>`
					if err := email.SendEmail(ticket.Email, "Happily Ever After - CSSS Year End Gala Tickets", body); err != nil {
						log.Println("send email err", err)
					}
				}
			} else if invoice.State == "UNPAID" {
				if time.Now().Add(-24 * time.Hour).Before(pr.CreatedAt) {
					continue
				}
				log.Printf("old and needs to be removed %+v", invoice)
				_, err := sq.CancelInvoice(&square.InvoiceCancelRequest{
					Token: invoice.Token,
					SendEmailToRecipients: false,
				})
				if err != nil {
					log.Println("square invoice cancel err", err)
				}
			}
		}
	}
}
