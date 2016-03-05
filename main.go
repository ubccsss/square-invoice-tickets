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
)

const (
	priceIndividual = 25
	priceGroup      = 80
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
	if err := square.Login(*squareEmail, *squarePass); err != nil {
		s.err(w, err, 500)
	}
}

func (s *server) tickets(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "POST" {
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
		promoCode = &pc
	}
	return promoCode, nil
}

func (s *server) priceEstimate(req *models.PurchaseRequest) (float32, error) {
	// Price code
	basePrice := float32(priceIndividual)
	switch req.Type {
	case models.Group:
		basePrice = priceGroup
	}

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
	promoCode, err := s.getPromoCode(req.PromoCode)
	if err != nil {
		s.err(w, err, 400)
		return
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
	var promoCodes []*models.PromoCode
	if err := s.db.Find(&promoCodes).Error; err != nil {
		return err
	}
	var valid = false
	for _, code := range promoCodes {
		if pr.PromoCode == code.ID {
			valid = true
		}
	}

	if pr.PromoCode != "" && !valid {
		return fmt.Errorf("Invalid promocode: %s", pr.PromoCode)
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
