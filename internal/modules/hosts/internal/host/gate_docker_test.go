package host

import (
	"archive/zip"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
)

// TestGateDocker starts a separate hosts Docker stack (own compose project,
// temporary directory, ports on 127.0.0.1 only) and checks the login form
// through real apache, php-fpm and MariaDB. The working stack is not touched.
// Run: DLEPLUGIN_DOCKER_TEST=1 go test ./internal/modules/hosts/internal/host -run TestGateDocker -v
func TestGateDocker(t *testing.T) {
	if os.Getenv("DLEPLUGIN_DOCKER_TEST") != "1" {
		t.Skip("set DLEPLUGIN_DOCKER_TEST=1 to run the isolated Docker gate test")
	}
	dir, err := os.MkdirTemp("", "dlehost-gate-")
	if err != nil {
		t.Fatal(err)
	}
	project := "dlehostgate" + randomTestHex(t, 4)
	t.Setenv("DLEHOST_DIR", dir)
	t.Setenv("DLEHOST_COMPOSE_PROJECT_NAME", project)
	t.Setenv("DLEHOST_BIND_ADDRESS", "127.0.0.1")
	httpPort := freePort(t)
	t.Setenv("DLEHOST_HTTP_PORT", strconv.Itoa(httpPort))
	t.Setenv("DLEHOST_DB_PORT", strconv.Itoa(freePort(t)))
	s, err := stack.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if os.Getenv("DLEHOST_TEST_KEEP") != "" {
			t.Logf("stack kept: %s (project %s)", dir, project)
			return
		}
		if t.Failed() {
			out, _ := exec.Command("docker", "compose", "--project-directory", dir, "logs", "--tail", "50", "apache", "php85", "php74").CombinedOutput()
			t.Logf("compose logs:\n%s", out)
		}
		_ = s.Compose(nil, "down", "--volumes", "--rmi", "local")
		// data/mariadb belongs to the container user.
		_ = exec.Command("docker", "run", "--rm", "--network", "none", "-v", dir+":/stack", "--entrypoint", "rm", "mariadb:11.4", "-rf", "/stack/data/mariadb").Run()
		_ = os.RemoveAll(dir)
	})
	if err := s.Compose(nil, "up", "-d", "--build", "--wait"); err != nil {
		t.Fatalf("compose up: %v", err)
	}

	dists := t.TempDir()
	writeTestDist(t, dists, "21.0")
	writeTestDist(t, dists, "11.0")
	logf := func(m string) { t.Log(m) }
	if err := SetPassword(s, "", []byte("global-secret"), false, ProtectSettings{}, logf); err != nil {
		t.Fatal(err)
	}
	add := func(name, dle, php, protect string, global bool, external bool) {
		t.Helper()
		s.BaseDomain, s.ExternalOrigin = "", ""
		if external {
			s.BaseDomain, s.ExternalOrigin = "gate.test", "https://gate.test"
		}
		if _, err := Add(s, AddOptions{Name: name, DLE: dle, PHP: php, PHPVersions: []string{"8.5", "7.4"}, DistsDir: dists,
			Admin: Credentials{"owner", "dle-admin-secret"}, Protect: protect, Settings: ProtectSettings{Global: global}}, logf); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}
	// Different DLE and PHP versions use one DLE-independent mechanism.
	add("shop", "21.0", "8.5", "on", false, true)
	add("legacy", "11.0", "7.4", "default", false, false)
	add("open", "21.0", "8.5", "off", false, false)
	if err := SetPassword(s, "shop", []byte("shop-secret"), false, ProtectSettings{}, logf); err != nil {
		t.Fatal(err)
	}

	c := &gateClient{t: t, base: fmt.Sprintf("http://127.0.0.1:%d", httpPort), http: &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	const shop, legacy, open = "shop.gate.test", "legacy.localhost", "open.localhost"

	t.Run("precedence global off", func(t *testing.T) {
		c.t = t
		c.expectGate(shop, "/")
		c.expectOK(legacy, "/", "php:index.php")
		c.expectOK(open, "/", "php:index.php")
	})

	t.Run("no session redirects every path", func(t *testing.T) {
		c.t = t
		for _, path := range []string{"/", "/index.php", "/admin.php", "/admin.php?mod=options", "/style.css", "/uploads/x.txt",
			"/free", "/engine/secret.txt", "/__dlehost/x.txt", "/__dlehost/login/x", "/__dlehost/../index.php",
			"/__dlehost/%2e%2e/admin.php", "/nonexistent", "/__DLEHOST/login"} {
			c.expectGate(shop, path)
		}
		// Auto host addresses of the same directory are closed the same way.
		for _, alias := range []string{"shop.localhost", "shop.php74.localhost", "shop.php85.localhost", "SHOP.Localhost"} {
			c.expectGate(alias, "/style.css")
		}
		// POST and forged headers do not open the site.
		r := c.do(shop, http.MethodPost, "/admin.php", url.Values{"x": {"1"}}, map[string]string{
			"X-Forwarded-For": "127.0.0.1", "X-Forwarded-Proto": "https", "Authorization": "Basic b3duZXI6c2hvcC1zZWNyZXQ=",
			"X-Original-URL": "/__dlehost/login", "X-Rewrite-URL": "/__dlehost/login",
		})
		if r.code != http.StatusSeeOther || !strings.HasPrefix(r.location, "/__dlehost/login?return=") {
			t.Fatalf("POST without session: %d %q", r.code, r.location)
		}
		if strings.Contains(r.header.Get("WWW-Authenticate"), "Basic") {
			t.Fatal("Basic auth challenge")
		}
	})

	t.Run("forged cookies", func(t *testing.T) {
		c.t = t
		token := base64.RawURLEncoding.EncodeToString([]byte(randomTestHex(t, 16)))[:43]
		for _, value := range []string{"20991231000000-" + token, "x", "", "../../../etc/passwd"} {
			c.expectGateCookie(shop, "/style.css", "__Host-dlehost_gate="+value)
			c.expectGateCookie(shop, "/style.css", "dlehost_gate="+value)
		}
	})

	var session string
	t.Run("login", func(t *testing.T) {
		c.t = t
		form := c.do(shop, http.MethodGet, "/__dlehost/login?return=%2Fadmin.php%3Fmod%3Doptions", nil, nil)
		if form.code != http.StatusOK || !strings.Contains(form.body, `type="password"`) || strings.Contains(form.body, `name="login"`) {
			t.Fatalf("form: %d %s", form.code, form.body)
		}
		csrf := form.cookie("__Host-dlehost_gate_csrf")
		if csrf == nil || !csrf.Secure || !csrf.HttpOnly || csrf.SameSite != http.SameSiteStrictMode {
			t.Fatalf("csrf cookie: %+v", form.cookies)
		}
		post := func(password, token string, headers map[string]string) gateResponse {
			if headers == nil {
				headers = map[string]string{}
			}
			headers["Cookie"] = "__Host-dlehost_gate_csrf=" + csrf.Value
			headers["X-Forwarded-Proto"] = "https"
			return c.do(shop, http.MethodPost, "/__dlehost/login", url.Values{
				"password": {password}, "csrf": {token}, "return": {"/admin.php?mod=options"},
			}, headers)
		}
		for _, bad := range []string{"wrong", "SHOP-SECRET", "dle-admin-secret", ""} {
			if r := post(bad, csrf.Value, nil); r.code != http.StatusUnauthorized || r.cookie("__Host-dlehost_gate") != nil {
				t.Fatalf("bad password accepted: %d", r.code)
			}
		}
		if r := post("shop-secret", "forged", nil); r.code != http.StatusForbidden {
			t.Fatalf("missing CSRF: %d", r.code)
		}
		if r := post("shop-secret", csrf.Value, map[string]string{"Origin": "https://evil.test"}); r.code != http.StatusForbidden {
			t.Fatalf("cross origin: %d", r.code)
		}
		r := post("shop-secret", csrf.Value, map[string]string{"Origin": "https://shop.gate.test"})
		cookie := r.cookie("__Host-dlehost_gate")
		if r.code != http.StatusSeeOther || r.location != "/admin.php?mod=options" || cookie == nil {
			t.Fatalf("login: %d %q %+v", r.code, r.location, r.cookies)
		}
		if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
			t.Fatalf("session cookie attributes: %+v", cookie)
		}
		session = "__Host-dlehost_gate=" + cookie.Value
		passwords, err := loadPasswords(s)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha1.Sum([]byte(cookie.Value))
		contents := readTestFile(t, filepath.Join(s.GateDir("shop"), hex.EncodeToString(sum[:])+"-v2-"+passwordKey(passwords.effective("shop"))))
		if contents != cookie.Value[:14]+"\n" {
			t.Fatal("session contains more than expiry")
		}

		for path, want := range map[string]string{"/": "php:index.php", "/admin.php": "admin:ok", "/style.css": "css:ok", "/uploads/x.txt": "upload:ok", "/free": "css:ok"} {
			c.expectOKCookie(shop, path, session, want)
		}
		// The site .htaccess restrictions still apply after login.
		if r := c.do(shop, http.MethodGet, "/engine/secret.txt", nil, map[string]string{"Cookie": session}); r.code != http.StatusForbidden {
			t.Fatalf(".htaccess deny weakened: %d", r.code)
		}
		// Own PHP/DLE sessions work next to the form cookie.
		first := c.do(shop, http.MethodGet, "/", nil, map[string]string{"Cookie": session})
		php := first.cookie("PHPSESSID")
		if php == nil {
			t.Fatal("no PHP session")
		}
		second := c.do(shop, http.MethodGet, "/", nil, map[string]string{"Cookie": session + "; PHPSESSID=" + php.Value})
		if second.body != "php:index.php:2" {
			t.Fatalf("PHP session: %q", second.body)
		}
		// A session does not match after the expiry in the cookie is forged.
		c.expectGateCookie(shop, "/", "__Host-dlehost_gate=20991231000000"+cookie.Value[14:])
		// An already logged-in user is sent from the form page back to the site.
		if r := c.do(shop, http.MethodGet, "/__dlehost/login?return=/style.css", nil, map[string]string{"Cookie": session}); r.code != http.StatusSeeOther || r.location != "/style.css" {
			t.Fatalf("logged-in form: %d %q", r.code, r.location)
		}
	})

	t.Run("return URL stays local", func(t *testing.T) {
		c.t = t
		for _, ret := range []string{"//evil.test/", "https://evil.test/", "/\\evil.test", "/%0d%0aSet-Cookie:x=1", "javascript:alert(1)", "/__dlehost/logout"} {
			r := c.do(shop, http.MethodGet, "/__dlehost/login?return="+url.QueryEscape(ret), nil, map[string]string{"Cookie": session})
			if r.code != http.StatusSeeOther || (r.location != "/" && r.location != ret) || !strings.HasPrefix(r.location, "/") || strings.HasPrefix(r.location, "//") || strings.ContainsAny(r.location, "\\\r\n") {
				t.Fatalf("return %q -> %d %q", ret, r.code, r.location)
			}
		}
	})

	t.Run("logout", func(t *testing.T) {
		c.t = t
		page := c.do(shop, http.MethodGet, "/__dlehost/logout", nil, map[string]string{"Cookie": session, "X-Forwarded-Proto": "https"})
		start := strings.Index(page.body, `name="token" value="`)
		if page.code != http.StatusOK || start < 0 {
			t.Fatalf("logout page: %d %s", page.code, page.body)
		}
		token := page.body[start+len(`name="token" value="`):]
		token = token[:strings.Index(token, `"`)]
		if r := c.do(shop, http.MethodPost, "/__dlehost/logout", url.Values{"token": {"forged"}}, map[string]string{"Cookie": session, "X-Forwarded-Proto": "https"}); r.code != http.StatusForbidden {
			t.Fatalf("logout CSRF: %d", r.code)
		}
		c.expectOKCookie(shop, "/style.css", session, "css:ok")
		r := c.do(shop, http.MethodPost, "/__dlehost/logout", url.Values{"token": {token}}, map[string]string{"Cookie": session, "X-Forwarded-Proto": "https"})
		if r.code != http.StatusSeeOther || r.location != "/__dlehost/login" {
			t.Fatalf("logout: %d %q", r.code, r.location)
		}
		if cookie := r.cookie("__Host-dlehost_gate"); cookie == nil || cookie.MaxAge >= 0 && !cookie.Expires.Before(time.Now()) {
			t.Fatalf("logout cookie not cleared: %+v", r.cookies)
		}
		c.expectGateCookie(shop, "/style.css", session)
	})

	// login logs into the host and returns the session cookie; https goes through the proxy.
	login := func(t *testing.T, host, password string, https bool) *http.Cookie {
		t.Helper()
		proto, csrfName, name := "", "dlehost_gate_csrf", "dlehost_gate"
		if https {
			proto, csrfName, name = "https", "__Host-dlehost_gate_csrf", "__Host-dlehost_gate"
		}
		form := c.do(host, http.MethodGet, "/__dlehost/login", nil, map[string]string{"X-Forwarded-Proto": proto})
		csrf := form.cookie(csrfName)
		if csrf == nil {
			t.Fatalf("no %s: %d %+v", csrfName, form.code, form.cookies)
		}
		r := c.do(host, http.MethodPost, "/__dlehost/login", url.Values{"password": {password}, "csrf": {csrf.Value}},
			map[string]string{"Cookie": csrfName + "=" + csrf.Value, "X-Forwarded-Proto": proto})
		cookie := r.cookie(name)
		if r.code != http.StatusSeeOther || cookie == nil || cookie.Secure != https || !cookie.HttpOnly {
			t.Fatalf("login %s: %d %+v", host, r.code, r.cookies)
		}
		return cookie
	}
	get := func(host, cookie, proto string) gateResponse {
		return c.do(host, http.MethodGet, "/style.css", nil, map[string]string{"Cookie": cookie, "X-Forwarded-Proto": proto})
	}
	sessionValue := func(expiry string) string {
		return expiry + "-" + base64.RawURLEncoding.EncodeToString([]byte(randomTestHex(t, 16)))[:43]
	}
	writeSession := func(t *testing.T, name, value string) {
		t.Helper()
		sum := sha1.Sum([]byte(value))
		p, err := loadPasswords(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(s.GateDir(name), hex.EncodeToString(sum[:])+"-v2-"+passwordKey(p.effective(name))), []byte(value[:14]+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("cookie bound to scheme, logout ends both", func(t *testing.T) {
		c.t = t
		// HTTP login through a local alias yields a plain cookie.
		plain := login(t, "shop.localhost", "shop-secret", false)
		if r := get("shop.localhost", "dlehost_gate="+plain.Value, ""); r.code != http.StatusOK {
			t.Fatalf("http alias: %d", r.code)
		}
		// HTTPS does not accept a non-Secure cookie instead of __Host-.
		if r := get(shop, "dlehost_gate="+plain.Value, "https"); r.code != http.StatusFound {
			t.Fatalf("plain cookie over https: %d", r.code)
		}
		if r := get(shop, "__Host-dlehost_gate="+plain.Value, ""); r.code != http.StatusFound {
			t.Fatalf("__Host- cookie over http: %d", r.code)
		}
		secure := login(t, shop, "shop-secret", true)
		stale := sessionValue("20991231000000")
		for cookie, want := range map[string]int{
			"dlehost_gate=" + stale + "; __Host-dlehost_gate=" + secure.Value: http.StatusOK,
			"__Host-dlehost_gate=" + secure.Value + "; dlehost_gate=" + stale: http.StatusOK,
			"__Host-dlehost_gate=" + stale + "; dlehost_gate=" + plain.Value:  http.StatusFound,
		} {
			if r := get(shop, cookie, "https"); r.code != want {
				t.Fatalf("https %q: %d, want %d", cookie, r.code, want)
			}
		}
		// HTTPS logout with both cookies ends both sessions and clears both cookies.
		both := "__Host-dlehost_gate=" + secure.Value + "; dlehost_gate=" + plain.Value
		page := c.do(shop, http.MethodGet, "/__dlehost/logout", nil, map[string]string{"Cookie": both})
		start := strings.Index(page.body, `name="token" value="`)
		if start < 0 {
			t.Fatalf("logout page: %s", page.body)
		}
		token := page.body[start+len(`name="token" value="`):]
		token = token[:strings.Index(token, `"`)]
		r := c.do(shop, http.MethodPost, "/__dlehost/logout", url.Values{"token": {token}}, map[string]string{"Cookie": both})
		if r.code != http.StatusSeeOther || r.cookie("__Host-dlehost_gate") == nil || r.cookie("dlehost_gate") == nil {
			t.Fatalf("logout: %d %+v", r.code, r.cookies)
		}
		if got := get("shop.localhost", "dlehost_gate="+plain.Value, ""); got.code != http.StatusFound {
			t.Fatalf("plain session survived https logout: %d", got.code)
		}
		if got := get(shop, "__Host-dlehost_gate="+secure.Value, "https"); got.code != http.StatusFound {
			t.Fatalf("secure session survived logout: %d", got.code)
		}
		// HTTP logout without a session just clears the cookie, without an error.
		if r := c.do("shop.localhost", http.MethodPost, "/__dlehost/logout", url.Values{}, map[string]string{"Cookie": "dlehost_gate=" + plain.Value}); r.code != http.StatusSeeOther {
			t.Fatalf("logout without session: %d", r.code)
		}
	})

	t.Run(".htaccess cannot bypass the vhost rule", func(t *testing.T) {
		c.t = t
		for _, path := range []string{"/bypass/x.txt", "/bypass/", "/uploads/x.txt"} {
			c.expectGate(shop, path)
			c.expectGate("shop.localhost", path)
		}
		cookie := login(t, shop, "shop-secret", true)
		c.expectOKCookie(shop, "/bypass/x.txt", "__Host-dlehost_gate="+cookie.Value, "bypass:ok")
		if r := c.do(shop, http.MethodGet, "/engine/secret.txt", nil, map[string]string{"Cookie": "__Host-dlehost_gate=" + cookie.Value}); r.code != http.StatusForbidden {
			t.Fatalf(".htaccess deny after login: %d", r.code)
		}
	})

	t.Run("session TTL", func(t *testing.T) {
		c.t = t
		// No server-side expiry by default; the cookie lasts 400 days (the browser limit).
		forever := login(t, shop, "shop-secret", true)
		if !strings.HasPrefix(forever.Value, "99991231235959-") || forever.MaxAge < 399*24*3600 || forever.MaxAge > 400*24*3600 {
			t.Fatalf("unlimited session: %q max-age %d", forever.Value, forever.MaxAge)
		}
		// Time is modelled by the expiry in the session file: a past expiry is rejected,
		// a non-expiring one is accepted.
		expired, unlimited := sessionValue("20000101000000"), sessionValue("99991231235959")
		writeSession(t, "shop", expired)
		writeSession(t, "shop", unlimited)
		if r := get(shop, "__Host-dlehost_gate="+expired, "https"); r.code != http.StatusFound {
			t.Fatalf("expired session: %d", r.code)
		}
		if r := get(shop, "__Host-dlehost_gate="+unlimited, "https"); r.code != http.StatusOK {
			t.Fatalf("unlimited session: %d", r.code)
		}
		// Short lifetime: old sessions end, a new one lives an hour in the cookie and on the server.
		hour := ProtectSettings{SessionTTL: time.Hour}
		if err := ApplyProtect(s, hour, ProtectSettings{}, func() error { return nil }, logf); err != nil {
			t.Fatal(err)
		}
		for _, v := range []string{forever.Value, unlimited} {
			if r := get(shop, "__Host-dlehost_gate="+v, "https"); r.code != http.StatusFound {
				t.Fatalf("session kept after shortening TTL: %d", r.code)
			}
		}
		before := time.Now().UTC()
		cookie := login(t, shop, "shop-secret", true)
		expiry, err := time.Parse("20060102150405", cookie.Value[:14])
		if err != nil || expiry.Before(before.Add(time.Hour-time.Minute)) || expiry.After(before.Add(time.Hour+time.Minute)) {
			t.Fatalf("finite expiry %q: %v", cookie.Value[:14], err)
		}
		if cookie.MaxAge < 3500 || cookie.MaxAge > 3600 {
			t.Fatalf("cookie max-age %d", cookie.MaxAge)
		}
		if r := get(shop, "__Host-dlehost_gate="+cookie.Value, "https"); r.code != http.StatusOK {
			t.Fatalf("finite session: %d", r.code)
		}
		// The same identifier with a forged expiry is rejected.
		if r := get(shop, "__Host-dlehost_gate=99991231235959"+cookie.Value[14:], "https"); r.code != http.StatusFound {
			t.Fatalf("extended expiry accepted: %d", r.code)
		}
		// reset also ends valid sessions.
		if err := ResetSessions(s, "shop", logf); err != nil {
			t.Fatal(err)
		}
		if r := get(shop, "__Host-dlehost_gate="+cookie.Value, "https"); r.code != http.StatusFound {
			t.Fatalf("session survived reset: %d", r.code)
		}
		login(t, shop, "shop-secret", true) // the session directory works after reset
		if err := ApplyProtect(s, ProtectSettings{}, hour, func() error { return nil }, logf); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("global on applies to default only", func(t *testing.T) {
		c.t = t
		if err := ApplyProtect(s, ProtectSettings{Global: true}, ProtectSettings{}, func() error { return nil }, logf); err != nil {
			t.Fatal(err)
		}
		c.expectGate(legacy, "/style.css")
		c.expectOK(open, "/style.css", "css:ok")
		// DLE 11.0 with PHP 7.4: the same global bcrypt; HTTP cookie without __Host- and Secure.
		form := c.do(legacy, http.MethodGet, "/__dlehost/login", nil, nil)
		csrf := form.cookie("dlehost_gate_csrf")
		if csrf == nil || csrf.Secure {
			t.Fatalf("http csrf cookie: %+v", form.cookies)
		}
		r := c.do(legacy, http.MethodPost, "/__dlehost/login", url.Values{"password": {"global-secret"}, "csrf": {csrf.Value}, "return": {"/style.css"}},
			map[string]string{"Cookie": "dlehost_gate_csrf=" + csrf.Value})
		cookie := r.cookie("dlehost_gate")
		if r.code != http.StatusSeeOther || cookie == nil || cookie.Secure || !cookie.HttpOnly {
			t.Fatalf("legacy login: %d %+v", r.code, r.cookies)
		}
		c.expectOKCookie(legacy, "/style.css", "dlehost_gate="+cookie.Value, "css:ok")
		// A session of one host does not open another.
		c.expectGateCookie(shop, "/style.css", "__Host-dlehost_gate="+cookie.Value)

		// DLE account changes do not affect protect.
		if err := s.SQL("UPDATE dle_legacy.dle_users SET password = MD5(MD5('changed-in-dle')) WHERE name = 'owner';"); err != nil {
			t.Fatal(err)
		}
		postPassword := func(password string) int {
			return c.do(legacy, http.MethodPost, "/__dlehost/login", url.Values{"password": {password}, "csrf": {csrf.Value}},
				map[string]string{"Cookie": "dlehost_gate_csrf=" + csrf.Value}).code
		}
		if code := postPassword("changed-in-dle"); code != http.StatusUnauthorized {
			t.Fatalf("DLE password accepted: %d", code)
		}
		if code := postPassword("global-secret"); code != http.StatusSeeOther {
			t.Fatalf("global password: %d", code)
		}
		// Changing the global password revokes the sessions of inheriting hosts.
		if err := SetPassword(s, "", []byte("new-global"), false, ProtectSettings{Global: true}, logf); err != nil {
			t.Fatal(err)
		}
		c.expectGateCookie(legacy, "/style.css", "dlehost_gate="+cookie.Value)
		if code := postPassword("global-secret"); code != http.StatusUnauthorized {
			t.Fatalf("old global password: %d", code)
		}
		if code := postPassword("new-global"); code != http.StatusSeeOther {
			t.Fatalf("new global password: %d", code)
		}
		// Brute force is limited: after 20 failures in 15 minutes, 429 even for the right password.
		for i := 0; i < 20; i++ {
			postPassword("wrong")
		}
		if code := postPassword("new-global"); code != http.StatusTooManyRequests {
			t.Fatalf("rate limit: %d", code)
		}
	})

	t.Run("site mode overrides global", func(t *testing.T) {
		c.t = t
		if _, err := SetProtect(s, "legacy", "off", ProtectSettings{Global: true}, logf); err != nil {
			t.Fatal(err)
		}
		c.expectOK(legacy, "/style.css", "css:ok")
		if _, err := os.Stat(s.GateDir("legacy")); !os.IsNotExist(err) {
			t.Fatal("sessions kept after disabling")
		}
		if _, err := SetProtect(s, "open", "on", ProtectSettings{Global: true}, logf); err != nil {
			t.Fatal(err)
		}
		c.expectGate(open, "/style.css")
		if err := ApplyProtect(s, ProtectSettings{}, ProtectSettings{Global: true}, func() error { return nil }, logf); err != nil {
			t.Fatal(err)
		}
		c.expectOK(legacy, "/style.css", "css:ok")
		c.expectGate(open, "/style.css")
		c.expectGate(shop, "/style.css")
	})

	t.Run("missing hash fails closed", func(t *testing.T) {
		c.t = t
		data, err := os.ReadFile(passwordPath(s))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(passwordPath(s)); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := os.WriteFile(passwordPath(s), data, 0644); err != nil {
				t.Fatal(err)
			}
		}()
		form := c.do(open, http.MethodGet, "/__dlehost/login", nil, nil)
		csrf := form.cookie("dlehost_gate_csrf")
		if csrf == nil {
			t.Fatal("no CSRF cookie")
		}
		r := c.do(open, http.MethodPost, "/__dlehost/login", url.Values{"password": {"new-global"}, "csrf": {csrf.Value}}, map[string]string{"Cookie": "dlehost_gate_csrf=" + csrf.Value})
		if r.code != http.StatusUnauthorized || r.cookie("dlehost_gate") != nil {
			t.Fatalf("missing hash: %d", r.code)
		}
	})

	t.Run("database down does not affect login", func(t *testing.T) {
		c.t = t
		if err := s.Compose(nil, "stop", "db"); err != nil {
			t.Fatal(err)
		}
		form := c.do(open, http.MethodGet, "/__dlehost/login", nil, nil)
		csrf := form.cookie("dlehost_gate_csrf")
		r := c.do(open, http.MethodPost, "/__dlehost/login", url.Values{"password": {"new-global"}, "csrf": {csrf.Value}},
			map[string]string{"Cookie": "dlehost_gate_csrf=" + csrf.Value})
		if r.code != http.StatusSeeOther || r.cookie("dlehost_gate") == nil {
			t.Fatalf("db down: %d %+v", r.code, r.cookies)
		}
		c.expectOKCookie(open, "/style.css", "dlehost_gate="+r.cookie("dlehost_gate").Value, "css:ok")
		// remove with an unavailable database fails, and the host stays closed.
		if err := Remove(s, "open", logf); err == nil {
			t.Fatal("remove with database down succeeded")
		}
		c.expectGate(open, "/style.css")
		c.expectGate("open.php74.localhost", "/style.css")
		if _, err := os.Stat(s.HostDir("open")); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("unknown host still 403", func(t *testing.T) {
		c.t = t
		r := c.do("unused.gate.test", http.MethodGet, "/", nil, nil)
		if r.code != http.StatusForbidden || strings.TrimSpace(r.body) != "Unknown host" {
			t.Fatalf("fallback: %d %q", r.code, r.body)
		}
	})
}

type gateClient struct {
	t    *testing.T
	base string
	http *http.Client
}

type gateResponse struct {
	code     int
	location string
	body     string
	header   http.Header
	cookies  []*http.Cookie
}

func (r gateResponse) cookie(name string) *http.Cookie {
	for _, c := range r.cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (c *gateClient) do(host, method, path string, form url.Values, headers map[string]string) gateResponse {
	c.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	request, err := http.NewRequest(method, c.base+"/", body)
	if err != nil {
		c.t.Fatal(err)
	}
	request.URL.Opaque = path // the path is sent without client normalization
	request.Host = host
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if strings.HasSuffix(host, ".gate.test") {
		request.Header.Set("X-Forwarded-Proto", "https")
	}
	for k, v := range headers {
		if v == "" {
			request.Header.Del(k) // an empty value means no header
			continue
		}
		request.Header.Set(k, v)
	}
	response, err := c.http.Do(request)
	if err != nil {
		c.t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return gateResponse{response.StatusCode, response.Header.Get("Location"), string(data), response.Header, response.Cookies()}
}

func (c *gateClient) expectGateCookie(host, path, cookie string) {
	c.t.Helper()
	headers := map[string]string{}
	if cookie != "" {
		headers["Cookie"] = cookie
	}
	r := c.do(host, http.MethodGet, path, nil, headers)
	if r.code != http.StatusFound || !strings.HasPrefix(r.location, "/__dlehost/login?return=") || strings.Contains(r.body, ":ok") || strings.Contains(r.body, "php:") {
		c.t.Fatalf("%s%s with %q: want redirect to form, got %d %q %q", host, path, cookie, r.code, r.location, r.body)
	}
}

func (c *gateClient) expectGate(host, path string) { c.t.Helper(); c.expectGateCookie(host, path, "") }

func (c *gateClient) expectOKCookie(host, path, cookie, want string) {
	c.t.Helper()
	headers := map[string]string{}
	if cookie != "" {
		headers["Cookie"] = cookie
	}
	r := c.do(host, http.MethodGet, path, nil, headers)
	if r.code != http.StatusOK || !strings.HasPrefix(r.body, want) {
		c.t.Fatalf("%s%s: want 200 %q, got %d %q", host, path, want, r.code, r.body)
	}
}

func (c *gateClient) expectOK(host, path, want string) {
	c.t.Helper()
	c.expectOKCookie(host, path, "", want)
}

func writeTestDist(t *testing.T, dir, version string) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, version+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, data := range map[string]string{
		"install.php":                fmt.Sprintf("// $config = array('version_id' => %q);\n", version) + testInstallerPHP,
		"fixture-version.txt":        version,
		"engine/data/.keep":          "",
		"templates/Default/main.tpl": "test template",
		"index.php":                  "<?php session_start(); $_SESSION['n'] = ($_SESSION['n'] ?? 0) + 1; echo 'php:index.php:' . $_SESSION['n'];",
		"admin.php":                  "<?php echo 'admin:ok';",
		"style.css":                  "css:ok",
		".htaccess":                  "RewriteEngine On\nRewriteRule ^free$ style.css [L]\n",
		"engine/.htaccess":           "Require all denied\n",
		"engine/secret.txt":          "secret",
		"uploads/.htaccess":          "<If \"true\">\n    Require all granted\n</If>\nSatisfy Any\n",
		"uploads/x.txt":              "upload:ok",
		"__dlehost/x.txt":            "shadow:ok",
		// Everything .htaccess could use to open access; the vhost rule runs first.
		"bypass/.htaccess": "RewriteEngine Off\nOrder allow,deny\nAllow from all\nSatisfy Any\n<If \"true\">\n    Require all granted\n</If>\n",
		"bypass/x.txt":     "bypass:ok",
	} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func randomTestHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}
