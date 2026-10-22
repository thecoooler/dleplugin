package installer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPInstallation(t *testing.T) {
	for _, mode := range []string{"success", "db error", "bad JSON", "stalled", "session lost", "redirect", "requirements"} {
		t.Run(mode, func(t *testing.T) {
			var actions []string
			saved := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/sub/install.php" {
					t.Errorf("path: %s", r.URL.Path)
				}
				if r.Host != "shop.example.com" || r.Header.Get("X-Forwarded-Proto") != "https" {
					t.Error("missing final origin")
				}
				action := r.URL.Query().Get("action")
				actions = append(actions, action)
				if action != "" {
					cookie, err := r.Cookie("PHPSESSID")
					if err != nil || cookie.Value != "session" {
						t.Error("PHP session lost")
					}
				}
				if action == "" {
					http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: "session", Path: "/sub/"})
					fmt.Fprint(w, `<input value="eula"><select name="selected_language">`)
					return
				}
				if mode == "session lost" && action == "eula" {
					fmt.Fprint(w, "Session expired")
					return
				}
				switch action {
				case "eula":
					if r.Method != "POST" || r.PostFormValue("selected_language") != "Russian" {
						t.Error("language form")
					}
					fmt.Fprint(w, `<form id="check-eula">`)
				case "function_check":
					if mode == "requirements" {
						fmt.Fprint(w, "mysqli missing")
						return
					}
					w.Header().Set("Location", "?action=dbconfig")
					w.WriteHeader(http.StatusFound)
				case "dbconfig":
					fmt.Fprint(w, `<input name="dbhost"><input name="dbprefix">`)
				case "checkdb":
					if r.PostFormValue("dbpasswd") != "db-secret" || r.PostFormValue("dbname") != "fresh" {
						t.Error("DB form")
					}
					if mode == "db error" {
						fmt.Fprint(w, `{"status":"error","text":"db-secret admin-secret"}`)
						return
					}
					if mode == "redirect" {
						http.Redirect(w, r, "https://other.example/", http.StatusTemporaryRedirect)
						return
					}
					fmt.Fprint(w, `{"status":"ok"}`)
				case "userconfig":
					fmt.Fprint(w, `<input name="reg_username">`)
				case "doinstall":
					if r.PostFormValue("reg_password1") != "admin-secret" || r.PostFormValue("reg_password2") != "admin-secret" {
						t.Error("admin form")
					}
					fmt.Fprint(w, "?action=installtemplate ?action=docreatedb ?action=dosaveconfig")
				case "installtemplate", "docreatedb":
					if r.Method != "POST" {
						t.Error("batch must be POST")
					}
					if mode == "bad JSON" {
						fmt.Fprint(w, "PHP Fatal error")
						return
					}
					if mode == "stalled" {
						fmt.Fprint(w, `{"status":"ok","offset":0,"total":2}`)
						return
					}
					switch r.PostFormValue("offset") {
					case "0":
						fmt.Fprint(w, `{"status":"ok","offset":1,"total":2}`)
					case "1":
						fmt.Fprint(w, `{"status":"ok","offset":2,"total":2}`)
					default:
						t.Error("invalid offset")
					}
				case "dosaveconfig":
					saved = true
					fmt.Fprint(w, `{"status":"ok"}`)
				default:
					t.Errorf("unexpected action %q", action)
				}
			}))
			defer server.Close()
			err := Run(context.Background(), Options{URL: server.URL + "/sub/", PublicURL: "https://shop.example.com/sub/",
				DBHost: "db", DBName: "fresh", DBUser: "fresh", DBPassword: "db-secret",
				Login: "owner", Password: "admin-secret", Email: "owner@example.com"}, nil)
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				want := ",eula,function_check,dbconfig,checkdb,userconfig,doinstall,installtemplate,installtemplate,docreatedb,docreatedb,dosaveconfig"
				if strings.Join(actions, ",") != want || !saved {
					t.Fatalf("sequence: %v", actions)
				}
			} else {
				if err == nil || saved {
					t.Fatalf("failure was ignored: %v, saved=%v", err, saved)
				}
				if strings.Contains(err.Error(), "db-secret") || strings.Contains(err.Error(), "admin-secret") {
					t.Fatal("secret in diagnostics")
				}
			}
		})
	}
}
