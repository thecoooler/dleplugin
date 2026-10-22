package installer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLegacyInstallation(t *testing.T) {
	for _, mode := range []string{"success", "old layout", "critical old requirement", "requirements", "permissions", "database", "cleanup", "no cleanup"} {
		t.Run(mode, func(t *testing.T) {
			var actions []string
			removed := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				action := r.URL.Query().Get("action")
				actions = append(actions, action)
				if action == "" {
					http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: "legacy"})
				} else if cookie, err := r.Cookie("PHPSESSID"); err != nil || cookie.Value != "legacy" {
					t.Error("missing PHP session")
				}
				switch action {
				case "":
					fmt.Fprint(w, `<input name="action" value="eula">`)
				case "eula":
					fmt.Fprint(w, `<form id="check-eula">`)
				case "function_check":
					fmt.Fprint(w, `<input name="action" value="chmod_check">`)
					if mode == "old layout" || mode == "critical old requirement" {
						color := "green"
						if mode == "critical old requirement" {
							color = "red"
						}
						fmt.Fprintf(w, `<table><font color=%s>Required PHP extension</font><tr><td>Рекомендуемые настройки</td></tr><font color=red>output_buffering</font></table><input type=submit value="Continue">`, color)
						return
					}
					if mode != "requirements" {
						fmt.Fprint(w, `<button type="submit">Continue</button>`)
					}
				case "chmod_check":
					fmt.Fprint(w, `<input name="action" value="doconfig">`)
					if mode != "permissions" {
						fmt.Fprint(w, "Проверка успешно завершена")
					}
				case "doconfig":
					fmt.Fprint(w, `<input name="dbhost"><input name="reg_username">`)
				case "doinstall":
					if r.Method != "POST" || r.PostFormValue("url") != "https://legacy.example.test/" || r.PostFormValue("alt_url") != "1" || r.PostFormValue("allow_mb4") != "1" || r.PostFormValue("dbpasswd") != "db-secret" || r.PostFormValue("reg_password1") != "admin-secret" || r.PostFormValue("reg_password2") != "admin-secret" || r.PostFormValue("dbprefix") != "dle" {
						t.Error("wrong installation form")
					}
					if mode == "database" {
						fmt.Fprint(w, "SQL error db-secret admin-secret")
						return
					}
					fmt.Fprint(w, `Установка завершена <a href="admin.php">Admin</a>`)
				default:
					t.Errorf("unexpected action %s", action)
				}
			}))
			defer server.Close()
			opts := Options{URL: server.URL, PublicURL: "https://legacy.example.test/", DBHost: "db", DBName: "fresh", DBUser: "fresh", DBPassword: "db-secret",
				Login: "owner", Password: "admin-secret", Email: "owner@example.com"}
			if mode != "no cleanup" {
				opts.RemoveInstaller = func() error {
					removed = true
					if mode == "cleanup" {
						return fmt.Errorf("cannot remove install.php")
					}
					return nil
				}
			}
			err := Run(context.Background(), opts, nil)
			if mode == "success" || mode == "old layout" {
				if err != nil || !removed {
					t.Fatalf("%v, removed=%v", err, removed)
				}
				if strings.Join(actions, ",") != ",eula,function_check,chmod_check,doconfig,doinstall" {
					t.Fatalf("wrong sequence: %v", actions)
				}
			} else {
				if err == nil {
					t.Fatal("failure ignored")
				}
				if mode != "cleanup" && removed {
					t.Fatal("removed installer before verified success")
				}
				if mode == "no cleanup" && strings.Contains(strings.Join(actions, ","), "doinstall") {
					t.Fatal("installed without cleanup capability")
				}
				if strings.Contains(err.Error(), "db-secret") || strings.Contains(err.Error(), "admin-secret") {
					t.Fatal("secret in diagnostics")
				}
			}
		})
	}
}
