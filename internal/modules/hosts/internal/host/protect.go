package host

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
)

// Host login form modes (hosts protect). The registry stores default as an
// empty value, so old entries without the field follow the global setting.
const (
	ProtectDefault = "default"
	ProtectOn      = "on"
	ProtectOff     = "off"
)

// ProtectSettings are the global login form settings from the config.
type ProtectSettings struct {
	Global     bool          // form for hosts in the default mode
	SessionTTL time.Duration // session lifetime; 0 means no server-side expiry
}

// shorter reports that sessions issued under old may outlive what s allows.
func (s ProtectSettings) shorter(old ProtectSettings) bool {
	return s.SessionTTL > 0 && (old.SessionTTL == 0 || s.SessionTTL < old.SessionTTL)
}

// ParseProtect converts a CLI mode to the registry field value.
func ParseProtect(mode string) (string, error) {
	switch mode {
	case ProtectDefault, "":
		return "", nil
	case ProtectOn, ProtectOff:
		return mode, nil
	}
	return "", fmt.Errorf("protect mode %q: use default, on or off", mode)
}

// ProtectMode is the host mode for output: default, on or off.
func (h Host) ProtectMode() string {
	if h.Protect == "" {
		return ProtectDefault
	}
	return h.Protect
}

// Protected reports whether the login form closes the host under the global setting.
func (h Host) Protected(global bool) bool {
	switch h.Protect {
	case ProtectOn:
		return true
	case ProtectOff:
		return false
	}
	return global
}

// gateMarker is present only in vhosts closed by the login form.
const gateMarker = "SetEnv DLEHOST_GATE_SITE "

// Cookie value: <UTC expiry, like %{TIME} in apache>-<256-bit base64url>.
const cookieValueRe = `(([0-9]{14})-[A-Za-z0-9_-]{43})\s*(?:;|$)`

// gateExpr is the valid session condition. The cookie name depends on the
// scheme as in gate.php: over HTTPS (X-Forwarded-Proto: https for public
// HTTPS hosts, like FastCGI HTTPS=on) only the Secure cookie
// __Host-dlehost_gate is accepted, otherwise only dlehost_gate. The expiry in
// the value has not passed, and the host session directory has a file for the
// value with the fingerprint of the effective bcrypt hash. An old password gives no valid session.
func gateExpr(h Host) string {
	plain := `%{HTTP_COOKIE} =~ /(?:^|;)\s*dlehost_gate=` + cookieValueRe + `/`
	cookie := plain
	if strings.HasPrefix(h.ExternalOrigin, "https://") {
		secure := `%{HTTP_COOKIE} =~ /(?:^|;)\s*__Host-dlehost_gate=` + cookieValueRe + `/`
		cookie = fmt.Sprintf(`(req('X-Forwarded-Proto') == 'https' && %s || req('X-Forwarded-Proto') != 'https' && %s)`, secure, plain)
	}
	return fmt.Sprintf(`%s && $2 > %%{TIME} && -f '%s/%s/' . sha1($1) . '-v2-%s'`, cookie, stack.ContainerGateDir, h.Name, h.gateKey)
}

// gateConfig closes the whole vhost with the login form through a vhost-level
// mod_rewrite rule. It runs during URL mapping before .htaccess is read, so
// site rules (RewriteEngine Off, Satisfy Any, Require in <If> and so on) do
// not cancel it; a request without a session goes to the form. This is the only
// check: there is no separate fallback Require. The site .htaccess restrictions
// apply as usual after login. Without login only /__dlehost/login and logout are open.
func gateConfig(h Host, ttl time.Duration) string {
	return fmt.Sprintf(`    # Separate site password login (hosts protect)
    %[1]s%[2]s
    SetEnv DLEHOST_GATE_TTL %[3]d
    AliasMatch "^/__dlehost/(login|logout)$" "%[4]s/gate.php"
    RewriteEngine On
    RewriteCond %%{REQUEST_URI} !^/__dlehost/(login|logout)$
    RewriteCond expr "!(%[5]s)"
    RewriteRule ^ /__dlehost/login [PT,L]
    <LocationMatch "^/__dlehost/(login|logout)$">
        Require all granted
    </LocationMatch>
`, gateMarker, h.Name, int64(ttl/time.Second), stack.ContainerGateApp, gateExpr(h))
}

// gateAliases takes <name>.localhost and <name>.*.localhost from the 90-auto.conf
// auto hosts, which would otherwise serve the host directory without the form.
func gateAliases(h Host) string {
	return fmt.Sprintf("    ServerAlias %[1]s.localhost %[1]s.*.localhost\n", h.Name)
}

func ensureGateDir(s *stack.Stack, name string) error {
	dir := s.GateDir(name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// As root, the directory is handed to the container user (UID/GID from .env).
	if os.Getuid() == 0 {
		uid, err1 := strconv.Atoi(s.Env["UID"])
		gid, err2 := strconv.Atoi(s.Env["GID"])
		if err1 != nil || err2 != nil {
			return fmt.Errorf("UID/GID are not set in .env: directory %s must be handed to the container user", dir)
		}
		return os.Chown(dir, uid, gid)
	}
	return nil
}

// clearSessions removes all login form sessions of a host: without a session
// file apache does not accept the cookie. keep leaves an empty directory for a
// closed host (as root, php could not create it itself).
func clearSessions(s *stack.Stack, name string, keep bool) error {
	dir := s.GateDir(name)
	if !keep {
		return os.RemoveAll(dir)
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// applyVhosts brings the vhosts of hosts to the login form settings, checks and
// reloads apache, then runs commit (writing the registry or the config). On
// error at any step the previous vhosts are restored (newly created ones are
// removed) and apache is reloaded again, so protection is never recorded as
// enabled without being applied. Rewritten are vhosts whose form state
// changes, closed vhosts with other form parameters, and missing vhosts of
// registry hosts (restored from the registry, otherwise a closed host would stay
// open through the <name>.localhost auto host).
// clear selects the hosts whose sessions are removed after a successful commit.
func applyVhosts(s *stack.Stack, hosts []Host, settings ProtectSettings, commit func() error, clear func(h Host, wasProtected bool) bool, log func(string)) error {
	type change struct {
		h            Host
		path         string
		old          []byte
		existed      bool
		wasProtected bool
		protected    bool
	}
	passwords, err := loadPasswords(s)
	if err != nil {
		return err
	}
	// Check the whole set first so that an error leaves no partial changes.
	for _, h := range hosts {
		if h.Protected(settings.Global) && passwords.effective(h.Name) == "" {
			return missingPassword(h.Name)
		}
	}
	var changes []change
	type clearing struct {
		name string
		keep bool
	}
	var cleared []clearing
	for _, h := range hosts {
		h.gateKey = passwordKey(passwords.effective(h.Name))
		path := vhostPath(s, h.Name)
		protected := h.Protected(settings.Global)
		want := vhostConfig(h, protected, settings.SessionTTL)
		old, err := os.ReadFile(path)
		existed := err == nil
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("vhost of host %s: %w", h.Name, err)
		}
		wasProtected := strings.Contains(string(old), gateMarker)
		if existed && wasProtected == protected && (!protected || string(old) == want) {
			if clear(h, wasProtected) {
				cleared = append(cleared, clearing{h.Name, protected})
			}
			continue
		}
		changes = append(changes, change{h, path, old, existed, wasProtected, protected})
		if clear(h, wasProtected) {
			cleared = append(cleared, clearing{h.Name, protected})
		}
	}
	for _, c := range changes {
		if !c.protected {
			continue
		}
		if err := ensureGateDir(s, c.h.Name); err != nil {
			return err
		}
		if err := s.CheckGate(c.h.PHP, c.h.Name); err != nil {
			return err
		}
	}

	restore := func(cause error) error {
		var errs []error
		for _, c := range changes {
			var err error
			if c.existed {
				err = os.WriteFile(c.path, c.old, 0o644)
			} else if err = os.Remove(c.path); errors.Is(err, os.ErrNotExist) {
				err = nil
			}
			if err != nil {
				errs = append(errs, err)
			}
		}
		if len(changes) > 0 {
			if err := s.ReloadApache(); err != nil {
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("%w; restoring the previous vhosts failed: %v", cause, errors.Join(errs...))
		}
		return fmt.Errorf("%w (previous vhosts restored)", cause)
	}
	for _, c := range changes {
		state := "off"
		if c.protected {
			state = "on"
		}
		switch {
		case !c.existed:
			log("vhost " + c.h.Name + " was missing, restored from the registry, login form " + state)
		case c.wasProtected != c.protected:
			log("login form " + c.h.Name + ": " + state)
		default:
			log("login form " + c.h.Name + ": new parameters")
		}
		if err := os.WriteFile(c.path, []byte(vhostConfig(c.h, c.protected, settings.SessionTTL)), 0o644); err != nil {
			return restore(err)
		}
	}
	if len(changes) > 0 {
		log("reloading apache")
		if err := s.ReloadApache(); err != nil {
			return restore(err)
		}
	}
	if err := commit(); err != nil {
		return restore(err)
	}
	var errs []error
	for _, c := range cleared {
		log("login form sessions of " + c.name + " removed")
		if err := clearSessions(s, c.name, c.keep); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("settings applied, but removing sessions failed: %w", errors.Join(errs...))
	}
	return nil
}

// SetProtect changes the login form mode of host name: default, on or off.
// Turning it off removes the host sessions.
func SetProtect(s *stack.Stack, name, mode string, settings ProtectSettings, log func(string)) (Host, error) {
	value, err := ParseProtect(mode)
	if err != nil {
		return Host{}, err
	}
	hosts, err := loadRegistry(s)
	if err != nil {
		return Host{}, err
	}
	h, ok := hosts[name]
	if !ok {
		return Host{}, fmt.Errorf("host %s is not among the hosts created by hosts add", name)
	}
	h.Protect = value
	hosts[name] = h
	clear := func(h Host, was bool) bool { return was && !h.Protected(settings.Global) }
	return h, applyVhosts(s, []Host{h}, settings, func() error { return saveRegistry(s, hosts) }, clear, log)
}

// ApplyProtect applies the new global settings (previously prev) to all
// registry hosts; commit saves them to the config after apache reloads.
// Sessions are removed for hosts where the form turns off, and for all closed
// hosts when the session lifetime gets shorter: sessions issued earlier would
// otherwise outlive the new lifetime.
func ApplyProtect(s *stack.Stack, settings, prev ProtectSettings, commit func() error, log func(string)) error {
	hosts, err := loadRegistry(s)
	if err != nil {
		return err
	}
	list := make([]Host, 0, len(hosts))
	for _, h := range hosts {
		list = append(list, h)
	}
	sortHosts(list)
	clear := func(h Host, was bool) bool {
		return was && (!h.Protected(settings.Global) || settings.shorter(prev))
	}
	return applyVhosts(s, list, settings, commit, clear, log)
}

// ResetSessions ends all login form sessions of host name (or of all registry
// hosts when name is empty). Users have to log in again.
func ResetSessions(s *stack.Stack, name string, log func(string)) error {
	hosts, err := loadRegistry(s)
	if err != nil {
		return err
	}
	if name != "" {
		if _, ok := hosts[name]; !ok {
			return fmt.Errorf("host %s is not among the hosts created by hosts add", name)
		}
		hosts = map[string]Host{name: hosts[name]}
	}
	var errs []error
	for n := range hosts {
		if _, err := os.Stat(s.GateDir(n)); err == nil {
			log("login form sessions of " + n + " removed")
		}
		if err := clearSessions(s, n, true); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
