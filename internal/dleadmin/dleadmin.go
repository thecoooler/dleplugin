// Package dleadmin is an HTTP client for the DataLife Engine admin panel:
// login, listing installed plugins and uploading plugin files.
package dleadmin

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// DefaultAdminPath is the admin script used when none is given.
const DefaultAdminPath = "admin.php"

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// ErrLogin is returned when the admin panel does not accept the credentials.
var ErrLogin = errors.New("login failed: check login, password and admin path")

// Plugin is a plugin installed on a site.
type Plugin struct {
	ID   int
	Name string
}

// Client talks to one site's admin panel. It is not safe for concurrent use.
type Client struct {
	admin *url.URL
	http  *http.Client
	hash  string
	// ajax is the AJAX entry point that accepted the last upload; empty
	// until then.
	ajax string
}

// SiteURL builds the admin script URL from a host or URL and an optional
// admin path. site is either a host ("example.com", https is assumed; http
// for local "*.localhost" hosts) or a URL ("http://example.com/dle/panel.php");
// a URL path and adminPath are mutually exclusive.
func SiteURL(site, adminPath string) (*url.URL, error) {
	site = strings.TrimSpace(site)
	if site == "" {
		return nil, errors.New("site host is empty")
	}
	raw := site
	if !strings.Contains(site, "://") {
		raw = "https://" + site
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid site %q: %w", site, err)
	}
	if !strings.Contains(site, "://") && isLocalhost(u.Hostname()) {
		u.Scheme = "http"
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("invalid site %q: scheme must be http or https", site)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("invalid site %q: host is empty", site)
	}
	urlPath := strings.TrimPrefix(u.Path, "/")
	adminPath = strings.TrimPrefix(strings.TrimSpace(adminPath), "/")
	switch {
	case urlPath != "" && adminPath != "":
		return nil, fmt.Errorf("admin path is set both in %q and --admin-path", site)
	case urlPath == "" && adminPath == "":
		adminPath = DefaultAdminPath
	case urlPath != "":
		adminPath = urlPath
	}
	if strings.HasSuffix(adminPath, "/") {
		return nil, fmt.Errorf("admin path %q must point to a script, not a directory", adminPath)
	}
	return &url.URL{Scheme: u.Scheme, User: u.User, Host: u.Host, Path: "/" + adminPath}, nil
}

// New creates a client for the admin script at admin. insecure disables TLS
// certificate verification.
func New(admin *url.URL, insecure bool) *Client {
	jar, _ := cookiejar.New(nil) // never fails with nil options
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit --insecure
	}
	tr.DialContext = dialLocalhost(tr.DialContext)
	return &Client{
		admin: admin,
		http: &http.Client{
			Jar: jar, Transport: tr, Timeout: 5 * time.Minute,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("stopped after 10 redirects")
				}
				from := via[0].URL
				if req.URL.Scheme != from.Scheme || !strings.EqualFold(req.URL.Hostname(), from.Hostname()) || originPort(req.URL) != originPort(from) {
					return errors.New("refusing redirect to a different origin; use the site's final admin URL")
				}
				return nil
			},
		},
	}
}

func originPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

// dialLocalhost sends "*.localhost" to loopback (RFC 6761), as browsers and
// curl do: neither the Go nor the macOS resolver knows these names, which
// local DLE hosts use.
func dialLocalhost(dial func(ctx context.Context, network, addr string) (net.Conn, error)) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if host, port, err := net.SplitHostPort(addr); err == nil && isLocalhost(host) {
			addr = net.JoinHostPort("localhost", port)
		}
		return dial(ctx, network, addr)
	}
}

func isLocalhost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

// AdminURL returns the admin script URL.
func (c *Client) AdminURL() string { return c.admin.String() }

func (c *Client) pluginsURL() string { return c.admin.String() + "?mod=plugins" }

// ajaxURLs are the AJAX entry points next to the admin script (the DLE root
// may be a subdirectory): engine/ajax/controller.php before DLE 19 and
// index.php?controller=ajax since, where the old script answers 403.
func (c *Client) ajaxURLs() []string {
	return []string{
		c.admin.ResolveReference(&url.URL{Path: "engine/ajax/controller.php"}).String(),
		c.admin.ResolveReference(&url.URL{Path: "index.php", RawQuery: "controller=ajax&mod=plugins"}).String(),
	}
}

// statusError is a non-200 response.
type statusError struct {
	method, url, status string
	code                int
}

func (e *statusError) Error() string {
	return fmt.Sprintf("%s %s: unexpected status %s", e.method, e.url, e.status)
}

func (c *Client) do(req *http.Request, referer string) ([]byte, error) {
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en;q=0.8")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", req.Method, req.URL.Redacted(), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response from %s: %w", req.URL.Redacted(), err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &statusError{method: req.Method, url: req.URL.Redacted(), status: resp.Status, code: resp.StatusCode}
	}
	return body, nil
}

var loginHashRe = regexp.MustCompile(`var dle_login_hash = '(.*?)';`)

// Login signs in to the admin panel.
func (c *Client) Login(ctx context.Context, login, password string) error {
	form := url.Values{
		"subaction":         {"dologin"},
		"username":          {login},
		"password":          {password},
		"selected_language": {"Russian"},
		"login_not_save":    {"1"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.AdminURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, err := c.do(req, c.AdminURL())
	if err != nil {
		return err
	}
	m := loginHashRe.FindSubmatch(body)
	if m == nil || len(m[1]) == 0 {
		return ErrLogin
	}
	c.hash = string(m[1])
	return nil
}

// versionTagRe matches the version suffix rendered after a plugin name. The
// version is free-form: a commit hash, a semver such as 0.0.1, and so on.
var versionTagRe = regexp.MustCompile(` v\.<span id="version\d+">[^<]*</span>`)

// Plugins lists the plugins installed on the site. Login must succeed first.
func (c *Client) Plugins(ctx context.Context) ([]Plugin, error) {
	if c.hash == "" {
		return nil, errors.New("not logged in")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.pluginsURL(), nil)
	if err != nil {
		return nil, err
	}
	body, err := c.do(req, c.AdminURL())
	if err != nil {
		return nil, err
	}
	return parsePlugins(versionTagRe.ReplaceAll(body, nil))
}

// parsePlugins reads table rows of the plugins page: the name is the text of
// <h6> without nested <span>s, the id comes from the first dropdown link.
func parsePlugins(page []byte) ([]Plugin, error) {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return nil, fmt.Errorf("parse plugins page: %w", err)
	}
	if findFirst(doc, func(n *html.Node) bool {
		return isTag(n, "input") && attr(n, "name") == "pluginfile" && attr(n, "type") == "file"
	}) == nil {
		return nil, errors.New("plugins page is unavailable: deploy requires DLE 13.0 or newer and access to the plugins section; check the site URL and admin permissions")
	}
	var out []Plugin
	for _, tr := range findAll(doc, func(n *html.Node) bool { return isTag(n, "tr") }) {
		h6 := findFirst(tr, func(n *html.Node) bool { return isTag(n, "h6") })
		if h6 == nil {
			continue
		}
		name := strings.TrimSpace(textWithoutSpans(h6))
		if name == "" {
			return nil, errors.New("parse plugins page: empty plugin name")
		}
		var link *html.Node
		if menu := findFirst(tr, func(n *html.Node) bool { return hasClass(n, "dropdown-menu") }); menu != nil {
			link = findFirst(menu, func(n *html.Node) bool { return isTag(n, "a") })
		}
		if link == nil {
			return nil, fmt.Errorf("parse plugins page: no actions link for plugin %q", name)
		}
		id, err := idFromHref(attr(link, "href"))
		if err != nil {
			return nil, fmt.Errorf("parse plugins page: plugin %q: %w", name, err)
		}
		out = append(out, Plugin{ID: id, Name: name})
	}
	return out, nil
}

func idFromHref(href string) (int, error) {
	u, err := url.Parse(href)
	if err != nil {
		return 0, fmt.Errorf("invalid link %q: %w", href, err)
	}
	id, err := strconv.Atoi(u.Query().Get("id"))
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("no plugin id in link %q", href)
	}
	return id, nil
}

// FindPlugin prefers an exact list name, then tries the plain version suffix
// used by DLE 13.0 and the unwrapped "0" version. The returned candidate's
// name must be verified using PluginName before updating it.
// Ambiguous names are rejected rather than selecting an arbitrary ID.
func FindPlugin(plugins []Plugin, name string) (Plugin, bool, error) {
	var found Plugin
	var matches int
	for _, p := range plugins {
		if p.Name == name {
			found = p
			matches++
		}
	}
	if matches == 0 {
		for _, p := range plugins {
			if strings.HasPrefix(p.Name, name+" v.") || p.Name == name+"0" {
				found = p
				matches++
			}
		}
	}
	if matches > 1 {
		return Plugin{}, false, fmt.Errorf("multiple installed plugins match name %q (%d matches); refusing to update", name, matches)
	}
	return found, matches == 1, nil
}

// PluginName reads the exact name from the edit form for one installed plugin.
func (c *Client) PluginName(ctx context.Context, id int) (string, error) {
	if c.hash == "" {
		return "", errors.New("not logged in")
	}
	if id <= 0 {
		return "", errors.New("invalid plugin id")
	}
	u := *c.admin
	q := u.Query()
	q.Set("mod", "plugins")
	q.Set("action", "edit")
	q.Set("id", strconv.Itoa(id))
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	body, err := c.do(req, c.pluginsURL())
	if err != nil {
		return "", fmt.Errorf("read plugin %d edit form: %w", id, err)
	}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("parse plugin %d edit form: %w", id, err)
	}
	var names []string
	for _, form := range findAll(doc, func(n *html.Node) bool { return isTag(n, "form") }) {
		for _, input := range findAll(form, func(n *html.Node) bool {
			return isTag(n, "input") && attr(n, "name") == "name" && attr(n, "type") == "text"
		}) {
			names = append(names, attr(input, "value"))
		}
	}
	if len(names) != 1 || strings.TrimSpace(names[0]) == "" {
		return "", fmt.Errorf("plugin %d edit form has no unique non-empty name; refusing to update", id)
	}
	return names[0], nil
}

// Upload sends a plugin file (.xml or .zip). id is the installed plugin to
// update, or 0 to install the file as a new plugin. Login must succeed first.
func (c *Client) Upload(ctx context.Context, id int, path string) error {
	if c.hash == "" {
		return errors.New("not logged in")
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open plugin file: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("mod", "plugins")
	_ = mw.WriteField("user_hash", c.hash)
	if id > 0 {
		_ = mw.WriteField("id", strconv.Itoa(id))
	}
	fw, err := mw.CreateFormFile("pluginfile", filepath.Base(path))
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return fmt.Errorf("read plugin file: %w", err)
	}
	if err := mw.Close(); err != nil {
		return err
	}

	body, err := c.postAjax(ctx, buf.Bytes(), mw.FormDataContentType())
	if err != nil {
		return err
	}
	var res struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("plugin upload failed: unexpected response: %s", snippet(body))
	}
	// DLE answers "succes" (sic) on success.
	if res.Status != "succes" {
		return fmt.Errorf("plugin upload failed: %s", res.Status)
	}
	return nil
}

// postAjax posts to the AJAX entry point, trying the next one when a site
// rejects the old script with 403 or 404.
func (c *Client) postAjax(ctx context.Context, payload []byte, contentType string) ([]byte, error) {
	urls := c.ajaxURLs()
	if c.ajax != "" {
		urls = []string{c.ajax}
	}
	var err error
	for _, u := range urls {
		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", contentType)
		var body []byte
		body, err = c.do(req, c.pluginsURL())
		var se *statusError
		if errors.As(err, &se) && (se.code == http.StatusForbidden || se.code == http.StatusNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		c.ajax = u
		return body, nil
	}
	return nil, err
}

func snippet(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if runes := []rune(s); len(runes) > 200 {
		s = string(runes[:200]) + "..."
	}
	return s
}

func isTag(n *html.Node, tag string) bool { return n.Type == html.ElementNode && n.Data == tag }

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	return n != nil && n.Type == html.ElementNode && strings.Contains(" "+attr(n, "class")+" ", " "+class+" ")
}

func findAll(n *html.Node, match func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if match(c) {
			out = append(out, c)
		}
		out = append(out, findAll(c, match)...)
	}
	return out
}

func findFirst(n *html.Node, match func(*html.Node) bool) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if match(c) {
			return c
		}
		if f := findFirst(c, match); f != nil {
			return f
		}
	}
	return nil
}

func textWithoutSpans(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch {
			case c.Type == html.TextNode:
				sb.WriteString(c.Data)
			case isTag(c, "span"):
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return sb.String()
}
