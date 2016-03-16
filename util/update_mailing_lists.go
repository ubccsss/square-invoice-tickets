package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"strings"

	"github.com/mailgun/mailgun-go"

	"../email"
	"../models"
)

var (
	user  = flag.String("user", "admin", "the user")
	pass  = flag.String("pass", "", "the pass")
	prAPI = flag.String("prAPI", "http://tickets.ubccsss.org/api/purchaseRequests", "the prAPI")
)

func main() {
	flag.Parse()

	mg := email.NewMG()

	req, err := http.NewRequest("GET", *prAPI, nil)
	if err != nil {
		log.Fatal(err)
	}
	req.SetBasicAuth(*user, *pass)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}

	if resp.StatusCode != 200 {
		log.Fatal("Code not 200 ", resp.StatusCode)
	}

	var prs []*models.PurchaseRequest
	if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
		log.Fatal(err)
	}

	lists := make(map[string][]interface{})
	for _, record := range prs {
		m := mailgun.Member{
			Name:    record.FirstName,
			Address: record.Email,
			Vars:    map[string]interface{}{},
		}
		mLists := []string{"everyone"}
		if record.Invoice != nil && record.Invoice.State == "UNPAID" {
			mLists = append(mLists, "unpaid")
		}
		for _, list := range mLists {
			lists[list] = append(lists[list], m)
		}
	}

	// Delete old automated mailing lists
	_, mlists, err := mg.GetLists(mailgun.DefaultLimit, mailgun.DefaultSkip, "")
	if err != nil {
		log.Fatal(err)
	}
	alwaysReplace := []string{"unpaid@ubc", "everyone@ubc"}
	for _, list := range mlists {
		shouldDelete := false
		for _, prefix := range alwaysReplace {
			if strings.HasPrefix(list.Address, prefix) {
				shouldDelete = true
				break
			}
		}
		if shouldDelete {
			log.Printf("%s: Deleting old list", list.Address)
			if err := mg.DeleteList(list.Address); err != nil {
				log.Fatal(err)
			}
		}
	}

	tr := true
	for addr, members := range lists {
		addr = addr + "@" + *email.Domain
		if list, err := mg.GetListByAddress(addr); err != nil || len(list.Address) == 0 {
			log.Printf("%s: Email list doesn't exist. %s", addr, err)
			list := mailgun.List{
				Address:     addr,
				Name:        "List: " + addr,
				AccessLevel: mailgun.ReadOnly,
				Description: "Automatically created, do not edit manually",
			}
			log.Printf("%s: Creating mailing list", addr)
			if _, err := mg.CreateList(list); err != nil {
				log.Printf("%s: Failed to create list %s", addr, err)
				continue
			}
		}
		log.Printf("%s: len = %d", addr, len(members))
		if err := email.CreateMemberList(mg, &tr, addr, members); err != nil {
			log.Fatal(err)
		}
	}
}
