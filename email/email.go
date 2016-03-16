package email

import (
	"flag"
	"log"

	"github.com/jaytaylor/html2text"
	"github.com/mailgun/mailgun-go"
	"github.com/vanng822/go-premailer/premailer"
)

var (
	Key    = flag.String("mg", "", "the mailgun api key")
	PubKey = flag.String("mgPub", "", "the mailgun api pubkey")
	Domain = flag.String("mgDomain", "ubccsss.org", "the email domain")
)

func NewMG() mailgun.Mailgun {
	return mailgun.NewMailgun(*Domain, *Key, *PubKey)
}

func SendEmail(to, subj, body string) error {
	pm := premailer.NewPremailerFromString(body, premailer.NewOptions())
	message, err := pm.Transform()
	if err != nil {
		return err
	}

	mg := NewMG()

	text, err := html2text.FromString(message)
	if err != nil {
		return err
	}
	m := mg.NewMessage(
		"UBC CSSS <noreply@ubccsss.org>",
		subj,
		text,
		to,
	)
	m.SetHtml(message)
	log.Printf("To: %s\nSubj: %s\nText:\n%sHTML:\n%s", to, subj, text, message)
	_, id, err := mg.Send(m)
	if err != nil {
		return err
	}
	log.Println(id)
	return nil
}
