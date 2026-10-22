package host

import (
	"fmt"
	"regexp"
	"strings"
)

// Credentials — логин и пароль администратора DLE.
type Credentials struct {
	Login    string
	Password string
}

var adminLoginRe = regexp.MustCompile(`^[\p{L}\p{N}_.-]+$`)

func (c Credentials) Validate() error {
	if len(c.Login) == 0 || len(c.Login) > 40 || !adminLoginRe.MatchString(c.Login) {
		return fmt.Errorf("логин администратора: 1–40 байт, только буквы, цифры, _, . и -")
	}
	if len(c.Password) == 0 || len(c.Password) > 72 || strings.ContainsRune(c.Password, 0) {
		return fmt.Errorf("пароль администратора: 1–72 байта без нулевого символа")
	}
	return nil
}
