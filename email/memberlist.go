package email

import "github.com/mailgun/mailgun-go"

func CreateMemberList(mg mailgun.Mailgun, tr *bool, addr string, members []interface{}) error {
	for len(members) > 0 {
		batch := members
		if len(members) > 1000 {
			batch = members[:1000]
			members = members[1000:]
		} else {
			members = nil
		}
		if err := mg.CreateMemberList(tr, addr, batch); err != nil {
			return err
		}
	}
	return nil
}
