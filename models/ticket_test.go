package models

import "testing"

func TestURL(t *testing.T) {
	ticket := Ticket{ID: "test"}
	out := ticket.URL()
	want := "http://tickets.ubccsss.org/ticket/test"
	if out != want {
		t.Errorf("%+v.URL() = %s; not %s", t, out, want)
	}
}
func TestHTML(t *testing.T) {
	ticket := Ticket{ID: "test", FirstName: "first", LastName: "last"}
	out := ticket.HTML()
	want := `first last <a href="http://tickets.ubccsss.org/ticket/test">http://tickets.ubccsss.org/ticket/test</a><br>`
	if out != want {
		t.Errorf("%+v.URL() = %s; not %s", t, out, want)
	}
}
