package dleadmin_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/thecoooler/dleplugin/internal/dleadmin"
	"github.com/thecoooler/dleplugin/internal/dleadmin/dleadmintest"
)

func TestSiteURL(t *testing.T) {
	for _, tc := range []struct {
		site, adminPath, want, err string
	}{
		{site: "example.com", want: "https://example.com/admin.php"},
		{site: " example.com ", adminPath: "/panel.php", want: "https://example.com/panel.php"},
		{site: "example.com:8443", want: "https://example.com:8443/admin.php"},
		{site: "http://example.com", want: "http://example.com/admin.php"},
		{site: "dle.localhost", want: "http://dle.localhost/admin.php"},
		{site: "localhost:8080", want: "http://localhost:8080/admin.php"},
		{site: "https://dle.localhost", want: "https://dle.localhost/admin.php"},
		{site: "https://example.com/", want: "https://example.com/admin.php"},
		{site: "https://example.com/dle/panel.php?x=1", want: "https://example.com/dle/panel.php"},
		{site: "https://example.com/panel.php", adminPath: "admin.php", err: "both"},
		{site: "example.com", adminPath: "dle/", err: "not a directory"},
		{site: "", err: "empty"},
		{site: "ftp://example.com", err: "scheme"},
		{site: "https://", err: "host is empty"},
	} {
		t.Run(tc.site+"|"+tc.adminPath, func(t *testing.T) {
			u, err := dleadmin.SiteURL(tc.site, tc.adminPath)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if u.String() != tc.want {
				t.Errorf("got %s, want %s", u, tc.want)
			}
		})
	}
}

func newClient(t *testing.T, s *dleadmintest.Server) *dleadmin.Client {
	t.Helper()
	u, err := url.Parse(s.AdminURL())
	if err != nil {
		t.Fatal(err)
	}
	return dleadmin.New(u, false)
}

func TestLoginPluginsUpload(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret",
		dleadmin.Plugin{ID: 3, Name: "Other"},
		dleadmin.Plugin{ID: 12, Name: "Demo <Plugin>"})
	c := newClient(t, s)
	ctx := context.Background()

	if _, err := c.Plugins(ctx); err == nil {
		t.Fatal("plugins without login")
	}
	if err := c.Login(ctx, "admin", "wrong"); !errors.Is(err, dleadmin.ErrLogin) {
		t.Fatalf("err = %v", err)
	}
	if err := c.Login(ctx, "admin", "secret"); err != nil {
		t.Fatal(err)
	}
	plugins, err := c.Plugins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 2 || plugins[1] != (dleadmin.Plugin{ID: 12, Name: "Demo <Plugin>"}) {
		t.Fatalf("plugins = %+v", plugins)
	}
	p, ok, err := dleadmin.FindPlugin(plugins, "Demo <Plugin>")
	if err != nil || !ok || p.ID != 12 {
		t.Fatalf("find = %+v %v %v", p, ok, err)
	}
	if _, ok, err := dleadmin.FindPlugin(plugins, "Missing"); ok || err != nil {
		t.Fatal("found missing")
	}

	file := filepath.Join(t.TempDir(), "demo.xml")
	if err := os.WriteFile(file, []byte("<dleplugin/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Upload(ctx, 12, file); err != nil {
		t.Fatal(err)
	}
	if err := c.Upload(ctx, 0, file); err != nil {
		t.Fatal(err)
	}
	want := []dleadmintest.Upload{
		{ID: "12", Filename: "demo.xml", Content: "<dleplugin/>"},
		{ID: "", Filename: "demo.xml", Content: "<dleplugin/>"},
	}
	got := s.Uploads()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("uploads = %+v", got)
	}

	s.Status = "Plugin file is broken"
	if err := c.Upload(ctx, 12, file); err == nil || err.Error() != "plugin upload failed: Plugin file is broken" {
		t.Fatalf("err = %v", err)
	}
}

func TestPluginVersionFormats(t *testing.T) {
	for _, version := range []string{"abc123", "0.0.1", "1.2.0-beta+1", ""} {
		t.Run(version, func(t *testing.T) {
			s := dleadmintest.New(t, "admin", "secret", dleadmin.Plugin{ID: 7, Name: "Demo"})
			s.Version = version
			c := newClient(t, s)
			ctx := context.Background()
			if err := c.Login(ctx, "admin", "secret"); err != nil {
				t.Fatal(err)
			}
			plugins, err := c.Plugins(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if p, ok, err := dleadmin.FindPlugin(plugins, "Demo"); err != nil || !ok || p.ID != 7 {
				t.Fatalf("plugins = %+v", plugins)
			}
		})
	}
}

func TestSubdirectoryInstall(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret")
	s.AdminPath = "dle/panel.php"
	c := newClient(t, s)
	ctx := context.Background()
	if err := c.Login(ctx, "admin", "secret"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "demo.zip")
	if err := os.WriteFile(file, []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	// the fake controller only accepts /dle/engine/ajax/controller.php
	if err := c.Upload(ctx, 0, file); err != nil {
		t.Fatal(err)
	}
	if len(s.Uploads()) != 1 {
		t.Fatal("no upload")
	}
}

func TestDLE19AjaxEntry(t *testing.T) {
	for _, adminPath := range []string{"admin.php", "dle/panel.php"} {
		t.Run(adminPath, func(t *testing.T) {
			s := dleadmintest.New(t, "admin", "secret", dleadmin.Plugin{ID: 5, Name: "Demo"})
			s.AdminPath = adminPath
			s.DLE19 = true
			c := newClient(t, s)
			ctx := context.Background()
			if err := c.Login(ctx, "admin", "secret"); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "demo.xml")
			if err := os.WriteFile(file, []byte("<dleplugin/>"), 0o644); err != nil {
				t.Fatal(err)
			}
			// controller.php answers 403; the upload goes to index.php?controller=ajax
			for range 2 {
				if err := c.Upload(ctx, 5, file); err != nil {
					t.Fatal(err)
				}
			}
			if got := s.Uploads(); len(got) != 2 || got[0].ID != "5" {
				t.Fatalf("uploads = %+v", got)
			}
		})
	}
}

func TestLocalhostSubdomain(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret", dleadmin.Plugin{ID: 5, Name: "Demo"})
	u, err := url.Parse(s.AdminURL())
	if err != nil {
		t.Fatal(err)
	}
	// the fake server listens on loopback; demo.localhost is not in DNS
	u.Host = "demo.localhost:" + u.Port()
	c := dleadmin.New(u, false)
	ctx := context.Background()
	if err := c.Login(ctx, "admin", "secret"); err != nil {
		t.Fatal(err)
	}
	plugins, err := c.Plugins(ctx)
	if err != nil || len(plugins) != 1 {
		t.Fatalf("plugins = %+v, err = %v", plugins, err)
	}
}

func TestUnexpectedResponses(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret")
	s.AdminPath = "missing.php"
	c := newClient(t, s)
	s.AdminPath = "admin.php"
	err := c.Login(context.Background(), "admin", "secret")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v", err)
	}
}

func TestPluginsSectionUnavailable(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret")
	s.NoPlugins = true
	c := newClient(t, s)
	ctx := context.Background()
	if err := c.Login(ctx, "admin", "secret"); err != nil {
		t.Fatal(err)
	}
	if plugins, err := c.Plugins(ctx); err == nil || !strings.Contains(err.Error(), "DLE 13.0 or newer") {
		t.Fatalf("plugins = %+v, err = %v", plugins, err)
	}
}

func TestLoginRejectsRedirectToOtherOrigin(t *testing.T) {
	for _, tc := range []struct {
		name           string
		tls, otherHost bool
	}{
		{name: "other port"}, {name: "other host", otherHost: true}, {name: "TLS downgrade", tls: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var received atomic.Bool
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received.Store(true)
				fmt.Fprint(w, `var dle_login_hash = 'test';`)
			}))
			defer target.Close()
			location := target.URL + "/admin.php"
			if tc.otherHost {
				location = strings.Replace(location, "127.0.0.1", "localhost", 1)
			}
			for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, location, status) })
				var origin *httptest.Server
				if tc.tls {
					origin = httptest.NewTLSServer(handler)
				} else {
					origin = httptest.NewServer(handler)
				}
				admin, err := dleadmin.SiteURL(origin.URL, "")
				if err != nil {
					t.Fatal(err)
				}
				err = dleadmin.New(admin, tc.tls).Login(context.Background(), "admin", "fake-secret")
				origin.Close()
				if err == nil || !strings.Contains(err.Error(), "different origin") {
					t.Fatalf("status %d: %v", status, err)
				}
				if received.Load() {
					t.Fatal("redirect target received a request")
				}
			}
		})
	}
}

func TestLoginFollowsSameOriginRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin.php" {
			http.Redirect(w, r, "/panel.php", http.StatusTemporaryRedirect)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Method != http.MethodPost || r.Form.Get("password") != "fake-secret" {
			t.Error("same-origin login was not preserved")
		}
		fmt.Fprint(w, `var dle_login_hash = 'test';`)
	}))
	defer server.Close()
	admin, err := dleadmin.SiteURL(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := dleadmin.New(admin, false).Login(context.Background(), "admin", "fake-secret"); err != nil {
		t.Fatal(err)
	}
}

func TestPluginName(t *testing.T) {
	for _, tc := range []struct {
		name string
		page string
		want string
	}{
		{"escaped name", `<form><input type="text" name="name" value="Demo &amp; &lt;B&gt; &quot;Q&quot;"></form>`, `Demo & <B> "Q"`},
		{"missing form", `<input type="text" name="name" value="Demo">`, ""},
		{"empty name", `<form><input type="text" name="name" value=" "></form>`, ""},
		{"duplicate fields", `<form><input type="text" name="name" value="Demo"><input type="text" name="name" value="Other"></form>`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := dleadmintest.New(t, "admin", "secret")
			s.AdminPath = "dle/panel.php"
			s.EditPage = tc.page
			c := newClient(t, s)
			ctx := context.Background()
			if err := c.Login(ctx, "admin", "secret"); err != nil {
				t.Fatal(err)
			}
			got, err := c.PluginName(ctx, 7)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("accepted invalid form: %q", got)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("PluginName = %q, %v", got, err)
			}
		})
	}
}
