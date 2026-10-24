package host

import (
	"fmt"
	"regexp"
	"strings"
)

// Credentials are the DLE administrator login and password.
type Credentials struct {
	Login    string
	Password string
}

var adminLoginRe = regexp.MustCompile(`^[\p{L}\p{N}_.-]+$`)

func (c Credentials) Validate() error {
	if len(c.Login) == 0 || len(c.Login) > 40 || !adminLoginRe.MatchString(c.Login) {
		return fmt.Errorf("administrator login: 1–40 bytes, letters, digits, _, . and - only")
	}
	if len(c.Password) == 0 || len(c.Password) > 72 || strings.ContainsRune(c.Password, 0) {
		return fmt.Errorf("administrator password: 1–72 bytes without NUL")
	}
	return nil
}
