package deploysites

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/thecoooler/dleplugin/internal/config"

	"github.com/zalando/go-keyring"
	"gopkg.in/yaml.v3"
)

// keyringService names dleplugin entries in the OS credential store.
const keyringService = "dleplugin"

func keyringUser(host, login string) string { return login + "@" + host }

// CredentialsFilename is the plain-text password file kept next to the global config.
const CredentialsFilename = "credentials.yaml"

// Credential store modes. Auto uses the OS credential store and falls back to
// the credentials file when the store is unavailable.
const (
	StoreAuto    = "auto"
	StoreKeyring = "keyring"
	StoreFile    = "file"
)

// Location tells where a saved password is kept.
type Location string

// Password locations.
const (
	InKeyring Location = "keyring"
	InFile    Location = "file"
)

// Credentials reads and writes saved admin passwords.
type Credentials struct {
	// Mode is StoreAuto, StoreKeyring or StoreFile.
	Mode string
	// Path is the credentials file.
	Path string
}

// NewCredentials returns the password store of mode with its file in dir.
// An empty mode is StoreAuto.
func NewCredentials(dir, mode string) (Credentials, error) {
	switch mode {
	case "":
		mode = StoreAuto
	case StoreAuto, StoreKeyring, StoreFile:
	default:
		return Credentials{}, fmt.Errorf("unknown credential store %q: use %s, %s or %s", mode, StoreAuto, StoreKeyring, StoreFile)
	}
	return Credentials{Mode: mode, Path: filepath.Join(dir, CredentialsFilename)}, nil
}

func (c Credentials) useKeyring() bool { return c.Mode != StoreFile }
func (c Credentials) useFile() bool    { return c.Mode != StoreKeyring }

// Password returns a saved admin password and its location. A missing entry
// is reported as an empty location without an error. In auto mode an
// unavailable OS credential store only means the file is checked.
func (c Credentials) Password(host, login string) (string, Location, error) {
	if c.useKeyring() {
		p, err := keyring.Get(keyringService, keyringUser(host, login))
		switch {
		case err == nil:
			return p, InKeyring, nil
		case errors.Is(err, keyring.ErrNotFound):
		case !c.useFile():
			return "", "", fmt.Errorf("read password from credential store: %w", err)
		}
	}
	if c.useFile() {
		f, err := c.load()
		if err != nil {
			return "", "", err
		}
		if p, ok := f.Sites[host][login]; ok {
			return p, InFile, nil
		}
	}
	return "", "", nil
}

// SavePassword stores an admin password and returns its location. In auto
// mode a failing OS credential store is returned as fallback and the password
// goes to the credentials file. After a successful OS store write, a stale
// copy in the file is removed when possible; the OS store is read first.
func (c Credentials) SavePassword(host, login, password string) (loc Location, fallback, err error) {
	if c.useKeyring() {
		err := keyring.Set(keyringService, keyringUser(host, login), password)
		if err == nil {
			if c.useFile() {
				_ = c.update(func(f *credentialsFile) bool { return f.remove(host, login) })
			}
			return InKeyring, nil, nil
		}
		err = fmt.Errorf("save password to credential store: %w", err)
		if !c.useFile() {
			return "", nil, err
		}
		fallback = err
	}
	err = c.update(func(f *credentialsFile) bool {
		if f.Sites[host] == nil {
			f.Sites[host] = make(map[string]string)
		}
		f.Sites[host][login] = password
		return true
	})
	if err != nil {
		return "", fallback, err
	}
	return InFile, fallback, nil
}

// DeletePassword removes a saved admin password; a missing entry is not an
// error. In auto mode an unavailable OS credential store is ignored.
func (c Credentials) DeletePassword(host, login string) error {
	if c.useKeyring() {
		err := keyring.Delete(keyringService, keyringUser(host, login))
		if err != nil && !errors.Is(err, keyring.ErrNotFound) && !c.useFile() {
			return fmt.Errorf("delete password from credential store: %w", err)
		}
	}
	if c.useFile() {
		return c.update(func(f *credentialsFile) bool { return f.remove(host, login) })
	}
	return nil
}

// credentialsFile maps host to login to password.
type credentialsFile struct {
	Sites map[string]map[string]string `yaml:"sites"`
}

func (f *credentialsFile) remove(host, login string) bool {
	if _, ok := f.Sites[host][login]; !ok {
		return false
	}
	delete(f.Sites[host], login)
	if len(f.Sites[host]) == 0 {
		delete(f.Sites, host)
	}
	return true
}

// load reads the credentials file; a missing file has no passwords. Anything
// but a regular file only its owner may access is rejected before reading.
func (c Credentials) load() (*credentialsFile, error) {
	f := &credentialsFile{Sites: make(map[string]map[string]string)}
	info, err := os.Lstat(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read credentials %s: %w", c.Path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("credentials %s must be a regular file, not a symlink or device", c.Path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("credentials %s must be private: run chmod 600 %s", c.Path, c.Path)
	}
	b, err := os.ReadFile(c.Path)
	if err != nil {
		return nil, fmt.Errorf("read credentials %s: %w", c.Path, err)
	}
	// Decoder errors quote values, which here are passwords.
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(f); err != nil && err != io.EOF {
		return nil, fmt.Errorf("credentials %s must map hosts to logins to passwords under sites", c.Path)
	}
	if f.Sites == nil {
		f.Sites = make(map[string]map[string]string)
	}
	return f, nil
}

// update applies change to the credentials file and atomically saves it when
// change reports a modification.
func (c Credentials) update(change func(*credentialsFile) bool) error {
	f, err := c.load()
	if err != nil {
		return err
	}
	if !change(f) {
		return nil
	}
	dir := filepath.Dir(c.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create credentials directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".credentials-*.yaml")
	if err != nil {
		return fmt.Errorf("create credentials: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary credentials: %w", err)
	}
	if err := config.SaveYAMLFile(tmp.Name(), f, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), c.Path); err != nil {
		return fmt.Errorf("save credentials %s: %w", c.Path, err)
	}
	return nil
}
