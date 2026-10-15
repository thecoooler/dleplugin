package deploysites

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// keyringService names dleplugin entries in the OS credential store.
const keyringService = "dleplugin"

func keyringUser(host, login string) string { return login + "@" + host }

// Password returns the admin password saved in the OS credential store.
// A missing entry is reported as ok == false without an error.
func Password(host, login string) (password string, ok bool, err error) {
	p, err := keyring.Get(keyringService, keyringUser(host, login))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read password from credential store: %w", err)
	}
	return p, true, nil
}

// SavePassword stores the admin password in the OS credential store.
func SavePassword(host, login, password string) error {
	if err := keyring.Set(keyringService, keyringUser(host, login), password); err != nil {
		return fmt.Errorf("save password to credential store: %w", err)
	}
	return nil
}

// DeletePassword removes a saved admin password; a missing entry is not an error.
func DeletePassword(host, login string) error {
	err := keyring.Delete(keyringService, keyringUser(host, login))
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete password from credential store: %w", err)
	}
	return nil
}
