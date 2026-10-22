// Package dleinstall drives unmodified DLE web installers (11.0 and newer) over HTTP.
package dleinstall

import (
	"context"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
)

type Options struct {
	URL, DBHost, DBName, DBUser, DBPassword, Prefix string
	Login, Password, Email, Language                string
	// PublicURL supplies the final Host and scheme when URL is a private endpoint.
	PublicURL string
	// RemoveInstaller is required for 11.0–14.2, which do not delete install.php.
	// It is called only after the success page has been verified.
	RemoveInstaller func() error
}

var (
	scripts           = regexp.MustCompile(`(?is)<(?:script|style)\b[^>]*>.*?</(?:script|style)>`)
	tags              = regexp.MustCompile(`<[^>]*>`)
	submitControl     = regexp.MustCompile(`(?i)\btype\s*=\s*(?:"submit"|'submit'|submit(?:\s|>))`)
	failedRequirement = regexp.MustCompile(`(?i)<font\s+color\s*=\s*["']?red\b|<span\s+class\s*=\s*["']text-danger["']`)
)

var (
	// phpHTML escapes like PHP's htmlspecialchars.
	phpHTML   = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#039;")
	jsonShort = map[rune]string{'\b': `\b`, '\f': `\f`, '\n': `\n`, '\r': `\r`, '\t': `\t`}
)

// redact replaces each secret in body, including the forms PHP's
// json_encode and htmlspecialchars give it in installer responses.
func redact(body string, secrets ...string) string {
	var forms []string
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		forms = append(forms, secret, stdhtml.EscapeString(secret),
			phpHTML.Replace(secret))
		for _, slash := range []bool{false, true} {
			for _, unicode := range []bool{false, true} {
				forms = append(forms, jsonEscape(secret, slash, unicode))
			}
		}
	}
	// Longer forms first, so a secret is not replaced inside its escaped form.
	slices.SortFunc(forms, func(a, b string) int { return len(b) - len(a) })
	for _, form := range forms {
		body = strings.ReplaceAll(body, form, "[redacted]")
	}
	return body
}

// jsonEscape returns s as inside a JSON string, optionally escaping "/" and
// non-ASCII characters like PHP's json_encode does by default.
func jsonEscape(s string, slash, unicode bool) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteString(`\` + string(r))
		case jsonShort[r] != "":
			b.WriteString(jsonShort[r])
		case r == '/' && slash:
			b.WriteString(`\/`)
		case r < 0x20 || r > 0x7f && unicode:
			for _, u := range utf16.Encode([]rune{r}) {
				fmt.Fprintf(&b, `\u%04x`, u)
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Run needs an empty database and a writable, freshly unpacked distribution.
// It never retries a mutating request: a lost response may already have changed DB/files.
func Run(ctx context.Context, opts Options, log func(string)) error {
	u, err := url.Parse(opts.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("installer URL must be an HTTP(S) site root without credentials, query or fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/install.php"
	siteURL := strings.TrimSuffix(u.String(), "install.php")
	var public *url.URL
	if opts.PublicURL != "" {
		public, err = url.Parse(opts.PublicURL)
		if err != nil || public.Host == "" || public.User != nil || (public.Scheme != "http" && public.Scheme != "https") || public.RawQuery != "" || public.Fragment != "" {
			return fmt.Errorf("invalid public URL")
		}
		siteURL = strings.TrimRight(opts.PublicURL, "/") + "/"
	}
	if opts.DBHost == "" || opts.DBName == "" || opts.DBUser == "" || opts.Login == "" || opts.Password == "" || opts.Email == "" {
		return fmt.Errorf("database host/name/user and administrator login/password/email are required")
	}
	if opts.Prefix == "" {
		opts.Prefix = "dle"
	}
	for _, r := range opts.Prefix {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return fmt.Errorf("invalid database prefix")
		}
	}
	if opts.Language == "" {
		opts.Language = "Russian"
	}
	if log == nil {
		log = func(string) {}
	}
	jar, _ := cookiejar.New(nil)
	// No environment proxies or redirects: neither DB nor administrator credentials
	// should be forwarded to a proxy/login page or to another origin.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Jar: jar, Transport: transport, Timeout: 5 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(action string, form url.Values) (string, *http.Response, error) {
		target := *u
		if action != "" {
			target.RawQuery = url.Values{"action": {action}}.Encode()
		}
		method := http.MethodGet
		var body io.Reader
		if form != nil {
			method, body = http.MethodPost, strings.NewReader(form.Encode())
		}
		req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
		if err != nil {
			return "", nil, err
		}
		req.Header.Set("Accept-Language", "ru")
		if public != nil {
			req.Host = public.Host
			req.Header.Set("X-Forwarded-Proto", public.Scheme)
		}
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", nil, fmt.Errorf("%s: HTTP request failed (not retried): %w", action, err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if err != nil {
			return "", nil, fmt.Errorf("%s: incomplete HTTP response (not retried): %w", action, err)
		}
		return string(data), resp, nil
	}
	// Error responses can echo passwords (e.g. an SQL error). Redact before logging.
	failure := func(action, body string) error {
		if strings.Contains(strings.ToLower(body), "<html") {
			body = stdhtml.UnescapeString(tags.ReplaceAllString(scripts.ReplaceAllString(body, " "), " "))
			body = strings.Join(strings.Fields(body), " ")
		}
		body = redact(body, opts.DBPassword, opts.Password)
		if r := []rune(body); len(r) > 1200 {
			body = string(r[:1200]) + "…"
		}
		return fmt.Errorf("installer %s: unexpected response: %s", action, body)
	}
	html := func(action string, form url.Values, markers ...string) error {
		body, resp, err := request(action, form)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
			if trimmed := strings.TrimSpace(body); trimmed != "" {
				msg += ": " + trimmed
			}
			return failure(action, msg)
		}
		for _, marker := range markers {
			if !strings.Contains(body, marker) {
				return failure(action, body)
			}
		}
		return nil
	}
	jsonRequest := func(action string, form url.Values) (map[string]json.RawMessage, error) {
		body, resp, err := request(action, form)
		if err != nil {
			return nil, err
		}
		var result map[string]json.RawMessage
		if resp.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &result) != nil || string(result["status"]) != `"ok"` {
			return nil, failure(action, body)
		}
		return result, nil
	}
	log("HTTP: installer session and license")
	if err := html("", nil, `value="eula"`); err != nil {
		return err
	}
	if err := html("eula", url.Values{"selected_language": {opts.Language}}, `id="check-eula"`); err != nil {
		return err
	}
	log("HTTP: PHP and write permission checks")
	body, resp, err := request("function_check", url.Values{"eula": {"on"}})
	if err != nil {
		return err
	}
	location, locationErr := resp.Location()
	if resp.StatusCode == http.StatusOK && strings.Contains(body, `value="chmod_check"`) {
		if opts.RemoveInstaller == nil {
			return fmt.Errorf("DLE 11.0–14.2 requires local installer cleanup (Options.RemoveInstaller)")
		}
		// 11.0–11.1 also show non-blocking recommendations below required PHP
		// checks. A red recommendation (e.g. output_buffering) is not a failure.
		// The Russian strings below are the texts of the installer pages.
		requirements := strings.Split(strings.Split(body, "Рекомендуемые настройки")[0], "</table>")[0]
		if !submitControl.MatchString(body) || failedRequirement.MatchString(requirements) {
			return failure("function_check", body)
		}
		log("HTTP: write permission check")
		if err := html("chmod_check", nil, `value="doconfig"`, "Проверка успешно завершена"); err != nil {
			return err
		}
		if err := html("doconfig", nil, `name="dbhost"`, `name="reg_username"`); err != nil {
			return err
		}
		log("HTTP: database, configuration and administrator")
		if err := html("doinstall", url.Values{
			"url": {siteURL}, "alt_url": {"1"}, "allow_mb4": {"1"},
			"dbhost": {opts.DBHost}, "dbname": {opts.DBName}, "dbuser": {opts.DBUser},
			"dbpasswd": {opts.DBPassword}, "dbprefix": {opts.Prefix},
			"reg_username": {opts.Login}, "reg_password1": {opts.Password},
			"reg_password2": {opts.Password}, "reg_email": {opts.Email},
		}, "Установка завершена", `href="admin.php"`); err != nil {
			return err
		}
		return opts.RemoveInstaller()
	}
	if resp.StatusCode != http.StatusFound || locationErr != nil || location.Scheme != u.Scheme || location.Host != u.Host || location.Path != u.Path || location.Query().Get("action") != "dbconfig" {
		return failure("function_check", fmt.Sprintf("HTTP %d Location %q: %s", resp.StatusCode, resp.Header.Get("Location"), body))
	}
	if err := html("dbconfig", nil, `name="dbhost"`, `name="dbprefix"`); err != nil {
		return err
	}
	log("HTTP: database connection")
	if _, err := jsonRequest("checkdb", url.Values{
		"dbhost": {opts.DBHost}, "dbname": {opts.DBName}, "dbuser": {opts.DBUser},
		"dbpasswd": {opts.DBPassword}, "dbprefix": {opts.Prefix},
	}); err != nil {
		return err
	}
	if err := html("userconfig", nil, `name="reg_username"`); err != nil {
		return err
	}
	log("HTTP: administrator")
	if err := html("doinstall", url.Values{
		"reg_username": {opts.Login}, "reg_password1": {opts.Password},
		"reg_password2": {opts.Password}, "reg_email": {opts.Email},
	}, "?action=installtemplate", "?action=docreatedb", "?action=dosaveconfig"); err != nil {
		return err
	}
	for _, action := range []string{"installtemplate", "docreatedb"} {
		offset, total := 0, -1
		for batch := 0; ; batch++ {
			if batch >= 10000 {
				return fmt.Errorf("installer %s: too many batches", action)
			}
			result, err := jsonRequest(action, url.Values{"offset": {fmt.Sprint(offset)}})
			if err != nil {
				return err
			}
			var next, count int
			if json.Unmarshal(result["offset"], &next) != nil || json.Unmarshal(result["total"], &count) != nil || count <= 0 || next <= offset || next > count || total != -1 && count != total {
				return fmt.Errorf("installer %s: invalid or stalled batch progress", action)
			}
			log(fmt.Sprintf("HTTP: %s %d/%d", action, next, count))
			if next == count {
				break
			}
			offset, total = next, count
		}
	}
	log("HTTP: saving the configuration and finishing")
	_, err = jsonRequest("dosaveconfig", url.Values{"action": {"dosaveconfig"}})
	return err
}
