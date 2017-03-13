package main

import (
	"flag"
	"log"

	"github.com/ubccsss/square-invoice-tickets/email"
)

var to = flag.String("to", "test@ubccsss.org", "who to send the poke to")

func main() {
	flag.Parse()
	subj := `CSSS Year End Gala - Outstanding Invoice`
	body := `<p>Hey %recipient_name%,</p>
					<p>We notice that you still haven't paid the ticket invoice that was sent to you. If you don't take action now, the invoice will be canceled on <b>Monday</b> to allow other students to go to the Gala! If it is canceled and you decide to get tickets later, you may pay a higher cost due to tiered pricing.</p>
					<p>Thanks!<br>The CSSS</p>`
	if err := email.SendEmail(*to, subj, body); err != nil {
		log.Println("send email err", err)
	}
}
