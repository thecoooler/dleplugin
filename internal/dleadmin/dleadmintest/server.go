// Package dleadmintest provides a fake DLE admin panel for tests.
package dleadmintest

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/thecoooler/dleplugin/internal/dleadmin"
)

const (
	hash   = "h4sh"
	cookie = "dle_admin"
)

// Upload is a plugin file received by the server.
type Upload struct {
	ID       string // "" when installed as new
	Filename string
	Content  string
}

// Server is a fake DLE site with its admin script at /<AdminPath>.
type Server struct {
	*httptest.Server
	Login, Password string
	AdminPath       string
	Plugins         []dleadmin.Plugin
	// Status is returned by the upload controller ("succes" by default).
	Status string
	// DLE19 serves AJAX at index.php?controller=ajax and answers 403 at
	// engine/ajax/controller.php, as DLE 19 and newer do.
	DLE19 bool
	// NoPlugins mimics older DLE versions without a plugins section.
	NoPlugins bool
	// Version is rendered after each plugin name ("abc123" by default).
	Version string
	// Versions overrides the displayed version for individual plugin IDs.
	Versions map[int]string
	// PlainVersion mimics DLE 13.0, which does not wrap versions in a span.
	PlainVersion bool
	// EditPage overrides the edit form response when non-empty.
	EditPage string

	mu      sync.Mutex
	uploads []Upload
	edits   []int
}

// New starts a server serving admin.php that is closed with the test.
func New(t *testing.T, login, password string, plugins ...dleadmin.Plugin) *Server {
	t.Helper()
	s := &Server{Login: login, Password: password, AdminPath: "admin.php", Plugins: plugins, Status: "succes", Version: "abc123"}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

// AdminURL returns the admin script URL.
func (s *Server) AdminURL() string { return s.URL + "/" + s.AdminPath }

// Uploads returns the received plugin files.
func (s *Server) Uploads() []Upload {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Upload(nil), s.uploads...)
}

// Edits returns the plugin IDs requested through their edit forms.
func (s *Server) Edits() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.edits...)
}

func (s *Server) authorized(r *http.Request) bool {
	c, err := r.Cookie(cookie)
	return err == nil && c.Value == "1"
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/"+s.AdminPath && r.Method == http.MethodPost:
		_ = r.ParseForm()
		if r.PostForm.Get("subaction") == "dologin" &&
			r.PostForm.Get("username") == s.Login && r.PostForm.Get("password") == s.Password {
			http.SetCookie(w, &http.Cookie{Name: cookie, Value: "1", Path: "/"})
			fmt.Fprintf(w, "<script>var dle_login_hash = '%s';</script>", hash)
			return
		}
		fmt.Fprint(w, "<form>login</form>")
	case r.URL.Path == "/"+s.AdminPath && r.URL.Query().Get("mod") == "plugins":
		if !s.authorized(r) {
			fmt.Fprint(w, "<form>login</form>")
			return
		}
		if s.NoPlugins {
			fmt.Fprint(w, `<html><body>Admin dashboard</body></html>`)
			return
		}
		if r.URL.Query().Get("action") == "edit" {
			id, _ := strconv.Atoi(r.URL.Query().Get("id"))
			s.mu.Lock()
			s.edits = append(s.edits, id)
			s.mu.Unlock()
			if s.EditPage != "" {
				fmt.Fprint(w, s.EditPage)
				return
			}
			for _, p := range s.Plugins {
				if p.ID == id {
					fmt.Fprintf(w, `<form method="post"><input type="text" name="name" value="%s"></form>`, html.EscapeString(p.Name))
					return
				}
			}
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, s.pluginsPage())
	case r.URL.Path == path.Join("/", path.Dir(s.AdminPath), "engine/ajax/controller.php") && s.DLE19:
		http.Error(w, "Forbidden", http.StatusForbidden)
	case r.URL.Path == path.Join("/", path.Dir(s.AdminPath), "engine/ajax/controller.php") && r.Method == http.MethodPost,
		r.URL.Path == path.Join("/", path.Dir(s.AdminPath), "index.php") && s.DLE19 &&
			r.URL.Query().Get("controller") == "ajax" && r.Method == http.MethodPost:
		if err := r.ParseMultipartForm(1 << 20); err != nil || !s.authorized(r) ||
			r.FormValue("user_hash") != hash || r.FormValue("mod") != "plugins" {
			fmt.Fprint(w, `{"status":"error"}`)
			return
		}
		f, fh, err := r.FormFile("pluginfile")
		if err != nil {
			fmt.Fprint(w, `{"status":"no file"}`)
			return
		}
		b, _ := io.ReadAll(f)
		s.mu.Lock()
		s.uploads = append(s.uploads, Upload{ID: r.FormValue("id"), Filename: fh.Filename, Content: string(b)})
		s.mu.Unlock()
		fmt.Fprintf(w, `{"status":%q}`, s.Status)
	default:
		http.NotFound(w, r)
	}
}

// pluginsPage mimics the DLE plugins list markup.
func (s *Server) pluginsPage() string {
	var sb strings.Builder
	sb.WriteString(`<html><body><form><input type="file" name="pluginfile"></form><table class="table">`)
	for _, p := range s.Plugins {
		version := s.Version
		if override, ok := s.Versions[p.ID]; ok {
			version = override
		}
		suffix := fmt.Sprintf(` v.<span id="version%d">%s</span>`, p.ID, html.EscapeString(version))
		if version == "" {
			suffix = ""
		} else if version == "0" {
			suffix = "0"
		} else if s.PlainVersion {
			suffix = " v." + html.EscapeString(version)
		}
		fmt.Fprintf(&sb, `<tr><td><h6 class="media-heading">%s%s <span class="label">on</span></h6></td>`+
			`<td><ul class="dropdown-menu"><li><a href="?mod=plugins&amp;action=edit&amp;id=%d">Edit</a></li></ul></td></tr>`,
			html.EscapeString(p.Name), suffix, p.ID)
	}
	sb.WriteString(`</table></body></html>`)
	return sb.String()
}
