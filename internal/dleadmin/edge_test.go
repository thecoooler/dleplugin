package dleadmin

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
	"testing"
	"unicode/utf8"
)

func TestOriginPort(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"http://example.com", "80"}, {"https://example.com", "443"}, {"https://example.com:8443", "8443"},
		{"http://[::1]", "80"}, {"https://[::1]:9443", "9443"},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := originPort(u); got != tc.want {
			t.Errorf("%s: %s", tc.raw, got)
		}
	}
}

func TestRedirectOriginPolicy(t *testing.T) {
	initial, _ := url.Parse("https://EXAMPLE.com/admin.php")
	c := New(initial, false)
	for _, tc := range []struct {
		raw     string
		allowed bool
	}{
		{"https://example.com/next", true}, {"https://example.com:443/next", true},
		{"https://example.com:444/next", false}, {"http://example.com/next", false},
		{"https://other.example/next", false},
	} {
		req, _ := http.NewRequest(http.MethodGet, tc.raw, nil)
		err := c.http.CheckRedirect(req, []*http.Request{{URL: initial}})
		if (err == nil) != tc.allowed {
			t.Errorf("%s: %v", tc.raw, err)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, initial.String(), nil)
	if err := c.http.CheckRedirect(req, make([]*http.Request, 10)); err == nil {
		t.Fatal("redirect loop accepted")
	}
}

func TestLoginRefusesCrossOriginRedirects(t *testing.T) {
	for _, tls := range []bool{false, true} {
		t.Run(fmt.Sprint(tls), func(t *testing.T) {
			reached := false
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true
				fmt.Fprint(w, "var dle_login_hash = 'bad';")
			}))
			defer target.Close()
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
			})
			var source *httptest.Server
			if tls {
				source = httptest.NewTLSServer(handler)
			} else {
				source = httptest.NewServer(handler)
			}
			defer source.Close()
			u, _ := url.Parse(source.URL + "/admin.php")
			c := New(u, true)
			err := c.Login(context.Background(), "admin", "secret")
			if err == nil || !strings.Contains(err.Error(), "different origin") || reached {
				t.Fatalf("err=%v target reached=%v", err, reached)
			}
		})
	}
}

func TestSnippet(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{" <html>\n  denied\t </html> ", "<html> denied </html>"},
		{strings.Repeat("a", 201), strings.Repeat("a", 200) + "..."},
		{strings.Repeat("a", 199) + "界more", strings.Repeat("a", 199) + "界..."},
		{"", ""},
	} {
		got := snippet([]byte(tc.body))
		if got != tc.want || !utf8.ValidString(got) {
			t.Fatalf("got %q want %q", got, tc.want)
		}
	}
}

func TestUploadFallbackAndFailureSnippet(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plugin.xml")
	if err := os.WriteFile(file, []byte("<plugin/>"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, code := range []int{403, 404, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			old, newCalls := 0, 0
			response := " <html>\n" + strings.Repeat("界", 201) + " </html> "
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/sub/engine/ajax/controller.php":
					old++
					http.Error(w, "old", code)
				case "/sub/index.php":
					newCalls++
					if r.URL.Query().Get("controller") != "ajax" {
						t.Error("missing controller")
					}
					fmt.Fprint(w, response)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			}))
			defer s.Close()
			u, _ := url.Parse(s.URL + "/sub/admin.php")
			c := New(u, false)
			c.hash = "hash"
			err := c.Upload(context.Background(), 0, file)
			if err == nil {
				t.Fatal("expected failure")
			}
			if code == 500 {
				if newCalls != 0 {
					t.Fatal("retried server error")
				}
				return
			}
			if old != 1 || newCalls != 1 || !strings.Contains(err.Error(), "unexpected response: "+snippet([]byte(response))) || !utf8.ValidString(err.Error()) {
				t.Fatalf("old=%d new=%d err=%v", old, newCalls, err)
			}
			// A successful HTTP response caches the selected endpoint, including malformed JSON responses.
			_ = c.Upload(context.Background(), 0, file)
			if old != 1 || newCalls != 2 {
				t.Fatalf("fallback not cached: %d %d", old, newCalls)
			}
		})
	}
}

func TestLoginFailureBodies(t *testing.T) {
	for _, body := range []string{"<form>login failed</form>", "var dle_login_hash = '';"} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		u, _ := url.Parse(s.URL + "/admin.php")
		err := New(u, false).Login(context.Background(), "admin", "secret")
		s.Close()
		if !errors.Is(err, ErrLogin) || !strings.Contains(err.Error(), "login") {
			t.Fatalf("%v", err)
		}
	}
}
