package dletest

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Database settings of the test installation. The database container is
// reachable as "db" inside the test network.
const (
	dbHost   = "db"
	dbName   = "dle"
	dbUser   = "dle"
	dbPass   = "dle"
	dbPrefix = "dle"
)

// Administrator created by the installer.
const (
	adminName  = "admin"
	adminPass  = "admin12345"
	adminEmail = "admin@example.com"
)

// Install walks the DLE web installer served at base (".../install.php"):
// language, license, environment check, database, administrator, then the
// template, table and settings steps the installer page runs with AJAX.
func Install(ctx context.Context, base string) error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	in := &installer{base: base, client: &http.Client{Jar: jar, Timeout: 5 * time.Minute}}

	// The first page starts the installer session.
	if _, err := in.get(ctx, ""); err != nil {
		return err
	}
	if _, err := in.post(ctx, "", url.Values{"action": {"eula"}, "selected_language": {"Russian"}}); err != nil {
		return err
	}
	// Redirects to the database form when the environment check passes.
	body, err := in.get(ctx, "?action=function_check&eula=on")
	if err != nil {
		return err
	}
	if !strings.Contains(body, `name="dbhost"`) {
		return fmt.Errorf("DLE installer: environment check failed: %s", pageText(body))
	}
	if err := in.postJSON(ctx, "?action=checkdb", url.Values{
		"action": {"checkdb"}, "dbhost": {dbHost}, "dbname": {dbName},
		"dbuser": {dbUser}, "dbpasswd": {dbPass}, "dbprefix": {dbPrefix},
	}, nil); err != nil {
		return err
	}
	if _, err := in.get(ctx, "?action=userconfig"); err != nil {
		return err
	}
	body, err = in.post(ctx, "?action=doinstall", url.Values{
		"reg_username": {adminName}, "reg_password1": {adminPass},
		"reg_password2": {adminPass}, "reg_email": {adminEmail},
	})
	if err != nil {
		return err
	}
	if !strings.Contains(body, "installtemplate") {
		return fmt.Errorf("DLE installer: unexpected page: %s", pageText(body))
	}
	for _, step := range []string{"installtemplate", "docreatedb"} {
		if err := in.batches(ctx, step); err != nil {
			return err
		}
	}
	return in.postJSON(ctx, "?action=dosaveconfig", url.Values{"action": {"dosaveconfig"}}, nil)
}

type installer struct {
	base   string
	client *http.Client
}

type stepResult struct {
	Status string `json:"status"`
	Offset int    `json:"offset"`
	Total  int    `json:"total"`
	Error  string `json:"error"`
}

// batches repeats an offset-based AJAX step until it reports completion.
func (in *installer) batches(ctx context.Context, action string) error {
	offset := 0
	for {
		var r stepResult
		if err := in.postJSON(ctx, "?action="+action, url.Values{"offset": {fmt.Sprint(offset)}}, &r); err != nil {
			return err
		}
		if r.Offset >= r.Total {
			return nil
		}
		if r.Offset <= offset {
			return fmt.Errorf("DLE installer: %s does not advance (offset %d of %d)", action, r.Offset, r.Total)
		}
		offset = r.Offset
	}
}

func (in *installer) get(ctx context.Context, query string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.base+query, nil)
	if err != nil {
		return "", err
	}
	return in.do(req)
}

func (in *installer) post(ctx context.Context, query string, form url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, in.base+query, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return in.do(req)
}

// postJSON posts form and requires {"status":"ok"}; out (may be nil)
// receives the decoded response.
func (in *installer) postJSON(ctx context.Context, query string, form url.Values, out *stepResult) error {
	body, err := in.post(ctx, query, form)
	if err != nil {
		return err
	}
	var r stepResult
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		return fmt.Errorf("DLE installer %s: invalid response: %s", query, pageText(body))
	}
	if r.Status != "ok" {
		msg := r.Error
		if msg == "" {
			msg = body
		}
		return fmt.Errorf("DLE installer %s: %s", query, pageText(msg))
	}
	if out != nil {
		*out = r
	}
	return nil
}

func (in *installer) do(req *http.Request) (string, error) {
	resp, err := in.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("DLE installer: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("DLE installer: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("DLE installer %s: HTTP %d: %s", req.URL.RequestURI(), resp.StatusCode, pageText(string(b)))
	}
	return string(b), nil
}

var (
	scriptRe = regexp.MustCompile(`(?is)<(script|style)\b.*?</(script|style)>`)
	tagRe    = regexp.MustCompile(`<[^>]*>`)
	spaceRe  = regexp.MustCompile(`\s+`)
)

// pageText returns the visible text of an HTML page, shortened for errors.
func pageText(s string) string {
	s = scriptRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(tagRe.ReplaceAllString(s, " "))
	s = strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
	if r := []rune(s); len(r) > 500 {
		s = string(r[:500]) + "..."
	}
	return s
}
