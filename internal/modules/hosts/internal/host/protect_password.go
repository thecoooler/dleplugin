package host

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
	"golang.org/x/crypto/bcrypt"
)

// legacyGateMarker marks vhosts written by early dlehost versions, whose form
// logged in with DLE administrator accounts; it is kept verbatim to detect them.
const legacyGateMarker = "Вход по логину и паролю администратора DLE"

// The store is unrelated to the DLE config and is mounted into the containers read-only.
type protectPasswords struct {
	Global string            `json:"global,omitempty"`
	Hosts  map[string]string `json:"hosts"`
}

func passwordPath(s *stack.Stack) string {
	return filepath.Join(s.Dir, "data", "protect", "passwords.json")
}

func loadPasswords(s *stack.Stack) (protectPasswords, error) {
	p := protectPasswords{Hosts: map[string]string{}}
	data, err := os.ReadFile(passwordPath(s))
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("could not read the protect password store")
	}
	if p.Hosts == nil {
		p.Hosts = map[string]string{}
	}
	for name, hash := range p.Hosts {
		if !hostNameRe.MatchString(name) || !validProtectHash(hash) {
			return p, fmt.Errorf("invalid protect hash of host %s", name)
		}
	}
	if p.Global != "" && !validProtectHash(p.Global) {
		return p, fmt.Errorf("invalid global protect hash")
	}
	return p, nil
}

var protectHashRE = regexp.MustCompile(`^\$2[aby]\$10\$[./A-Za-z0-9]{53}$`)

func validProtectHash(hash string) bool {
	if !protectHashRE.MatchString(hash) {
		return false
	}
	cost, err := bcrypt.Cost([]byte(hash))
	return err == nil && cost == bcrypt.DefaultCost
}

func (p protectPasswords) effective(name string) string {
	if hash := p.Hosts[name]; hash != "" {
		return hash
	}
	return p.Global
}

func passwordKey(hash string) string {
	sum := sha256.Sum256([]byte(hash))
	return hex.EncodeToString(sum[:])
}

func savePasswords(s *stack.Stack, p protectPasswords) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := passwordPath(s)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// A rename inside the mounted directory is visible to the running PHP.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".passwords-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// PasswordStatus returns only whether passwords are set, without the hashes.
func PasswordStatus(s *stack.Stack) (bool, map[string]bool, error) {
	p, err := loadPasswords(s)
	own := map[string]bool{}
	for name := range p.Hosts {
		own[name] = true
	}
	return p.Global != "", own, err
}

// SetPassword sets a password (empty name: the global one) or returns the host
// to the global password (inherit). The plain password exists only in memory.
func SetPassword(s *stack.Stack, name string, password []byte, inherit bool, settings ProtectSettings, log func(string)) error {
	hosts, err := loadRegistry(s)
	if err != nil {
		return err
	}
	if name != "" {
		if _, ok := hosts[name]; !ok {
			return fmt.Errorf("host %s is not among the hosts created by hosts add", name)
		}
	} else if inherit {
		return fmt.Errorf("--default is only valid for a host password")
	}
	p, err := loadPasswords(s)
	if err != nil {
		return err
	}
	old, err := loadPasswords(s)
	if err != nil {
		return err
	}
	if inherit {
		delete(p.Hosts, name)
	} else {
		if len(password) == 0 || len(password) > 72 {
			return fmt.Errorf("the password must be 1 to 72 bytes")
		}
		hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("could not hash the password: %w", err)
		}
		if name == "" {
			p.Global = string(hash)
		} else {
			p.Hosts[name] = string(hash)
		}
	}
	// Check all affected hosts before writing: a reset must not leave a closed host without a password.
	for _, h := range hosts {
		if (name == "" || h.Name == name) && h.Protected(settings.Global) && p.effective(h.Name) == "" {
			return missingPassword(h.Name)
		}
	}
	// The first update has to mount the new directory read-only.
	// The password can be set before up; old sessions are revoked right away.
	legacy := false
	for _, h := range hosts {
		data, err := os.ReadFile(vhostPath(s, h.Name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if strings.Contains(string(data), legacyGateMarker) {
			legacy = true
		}
	}
	if err := savePasswords(s, p); err != nil {
		return err
	}
	if legacy {
		if err := ResetSessions(s, "", log); err != nil {
			return err
		}
		log("password saved; run dleplugin hosts up to move from the old form")
		return nil
	}
	err = ApplyProtect(s, settings, settings, func() error { return nil }, log)
	if err != nil {
		if rerr := savePasswords(s, old); rerr != nil {
			return fmt.Errorf("%w; restoring the previous hashes failed: %v", err, rerr)
		}
		return err
	}
	// The session file namespace already changed in the vhost; remove the old files.
	for _, h := range hosts {
		if old.effective(h.Name) != p.effective(h.Name) {
			if err := clearSessions(s, h.Name, h.Protected(settings.Global)); err != nil {
				return fmt.Errorf("password changed, but removing the sessions of %s failed: %w", h.Name, err)
			}
		}
	}
	log("protect password saved")
	return nil
}

func missingPassword(name string) error {
	return fmt.Errorf("closed host %s has no password: run dleplugin hosts protect password or dleplugin hosts protect %s password", name, name)
}

// CheckProtectPasswords checks the settings before the stack starts. Sessions of
// the old DLE form are revoked even when the migration is unfinished; sites stay closed.
func CheckProtectPasswords(s *stack.Stack, settings ProtectSettings) error {
	hosts, err := loadRegistry(s)
	if err != nil {
		return err
	}
	p, err := loadPasswords(s)
	if err != nil {
		return err
	}
	for _, h := range hosts {
		data, err := os.ReadFile(vhostPath(s, h.Name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if strings.Contains(string(data), legacyGateMarker) {
			if err := clearSessions(s, h.Name, true); err != nil {
				return err
			}
		}
	}
	for _, h := range hosts {
		if h.Protected(settings.Global) && p.effective(h.Name) == "" {
			return missingPassword(h.Name)
		}
	}
	return nil
}
