package host

import (
	"testing"
)

func TestAdminValidation(t *testing.T) {
	for _, c := range []Credentials{{"", "admin"}, {"admin", ""}, {"bad'name", "admin"}, {"admin", "bad\x00password"}} {
		if c.Validate() == nil {
			t.Errorf("accepted invalid credentials for login %q", c.Login)
		}
	}
	if err := (Credentials{"владелец", "quote' slash\\ dollar$"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
