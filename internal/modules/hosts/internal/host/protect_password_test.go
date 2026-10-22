package host

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestProtectRequiresPassword(t *testing.T) {
	s := isolatedStack(t)
	calls, _ := fakeDocker(t)
	registerTestHosts(t, s, Host{Name: "shop"})
	before := readTestFile(t, vhostPath(s, "shop"))
	commit := false
	if err := ApplyProtect(s, ProtectSettings{Global: true}, ProtectSettings{}, func() error { commit = true; return nil }, func(string) {}); err == nil || !strings.Contains(err.Error(), "не задан пароль") {
		t.Fatalf("global enable: %v", err)
	}
	if _, err := SetProtect(s, "shop", "on", ProtectSettings{}, func(string) {}); err == nil {
		t.Fatal("host enabled without password")
	}
	for _, mode := range []string{"on", "default"} {
		_, err := Add(s, AddOptions{Name: "new", Protect: mode, Settings: ProtectSettings{Global: true}, Admin: Credentials{"admin", "secret"}}, func(string) {})
		if err == nil || !strings.Contains(err.Error(), "не задан пароль") {
			t.Fatalf("add %s: %v", mode, err)
		}
	}
	if commit || readTestFile(t, vhostPath(s, "shop")) != before {
		t.Fatal("missing password changed state")
	}
	if _, err := os.Stat(calls); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("docker called before password validation")
	}
}

func TestProtectPasswordLifecycle(t *testing.T) {
	s := isolatedStack(t)
	_, fail := fakeDocker(t)
	registerTestHosts(t, s, Host{Name: "inherit"}, Host{Name: "own"}, Host{Name: "off", Protect: "off"})
	log := func(string) {}
	if err := SetPassword(s, "", []byte("global-secret"), false, ProtectSettings{}, log); err != nil {
		t.Fatal(err)
	}
	if err := SetPassword(s, "own", []byte("own-secret"), false, ProtectSettings{}, log); err != nil {
		t.Fatal(err)
	}
	settings := ProtectSettings{Global: true}
	if err := ApplyProtect(s, settings, ProtectSettings{}, func() error { return nil }, log); err != nil {
		t.Fatal(err)
	}
	p, err := loadPasswords(s)
	if err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(p.Global), []byte("global-secret")) != nil || bcrypt.CompareHashAndPassword([]byte(p.effective("own")), []byte("own-secret")) != nil {
		t.Fatal("incorrect password precedence/hash")
	}
	global, own, err := PasswordStatus(s)
	if err != nil || !global || !own["own"] || own["inherit"] {
		t.Fatal("password status")
	}
	if strings.Contains(readTestFile(t, passwordPath(s)), "secret") {
		t.Fatal("plaintext persisted")
	}
	session := func(name string) string { return filepath.Join(s.GateDir(name), "session") }
	for _, name := range []string{"inherit", "own"} {
		if err := os.WriteFile(session(name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ownVhost := readTestFile(t, vhostPath(s, "own"))
	inheritVhost := readTestFile(t, vhostPath(s, "inherit"))
	if err := SetPassword(s, "", []byte("new-global"), false, settings, log); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(session("inherit")); !os.IsNotExist(err) {
		t.Fatal("global password kept inherited session")
	}
	if _, err := os.Stat(session("own")); err != nil {
		t.Fatal("global password revoked own session")
	}
	if ownVhost != readTestFile(t, vhostPath(s, "own")) || inheritVhost == readTestFile(t, vhostPath(s, "inherit")) {
		t.Fatal("wrong credential namespaces updated")
	}
	if err := SetPassword(s, "own", nil, true, settings, log); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(session("own")); !os.IsNotExist(err) {
		t.Fatal("inherit kept session")
	}
	p, _ = loadPasswords(s)
	if p.Hosts["own"] != "" || p.effective("own") != p.Global {
		t.Fatal("inherit did not remove override")
	}
	// Ошибка Apache возвращает хранилище и vhost, не отзывая действующие сессии.
	before := readTestFile(t, passwordPath(s))
	vhost := readTestFile(t, vhostPath(s, "inherit"))
	if err := os.WriteFile(session("inherit"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fail, []byte("configtest"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SetPassword(s, "", []byte("failed-change"), false, settings, log); err == nil {
		t.Fatal("reload failure ignored")
	}
	if before != readTestFile(t, passwordPath(s)) || vhost != readTestFile(t, vhostPath(s, "inherit")) {
		t.Fatal("password change not rolled back")
	}
	if _, err := os.Stat(session("inherit")); err != nil {
		t.Fatal("session revoked on failure")
	}
}

func TestProtectHostOnlyPasswordAndMigration(t *testing.T) {
	s := isolatedStack(t)
	registerTestHosts(t, s, Host{Name: "shop"})
	log := func(string) {}
	if err := SetPassword(s, "shop", []byte("host-only"), false, ProtectSettings{}, log); err != nil {
		t.Fatal(err)
	}
	if _, err := SetProtect(s, "shop", "on", ProtectSettings{}, log); err != nil {
		t.Fatal(err)
	}
	before := readTestFile(t, passwordPath(s))
	if err := SetPassword(s, "shop", nil, true, ProtectSettings{}, log); err == nil {
		t.Fatal("removed only effective password")
	}
	if before != readTestFile(t, passwordPath(s)) {
		t.Fatal("failed inherit changed storage")
	}
	// Старый vhost и сессии DLE: пароль можно задать до подключения нового mount.
	data := strings.Replace(readTestFile(t, vhostPath(s, "shop")), "Вход по отдельному паролю сайта", "Вход по логину и паролю администратора DLE", 1)
	if err := os.WriteFile(vhostPath(s, "shop"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	oldSession := filepath.Join(s.GateDir("shop"), "old-session")
	if err := os.WriteFile(oldSession, []byte("99991231235959\nadmin\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(passwordPath(s)); err != nil {
		t.Fatal(err)
	}
	if err := CheckProtectPasswords(s, ProtectSettings{}); err == nil {
		t.Fatal("legacy protection started without password")
	}
	if _, err := os.Stat(oldSession); !os.IsNotExist(err) {
		t.Fatal("legacy session survived migration preflight")
	}
	if err := SetPassword(s, "", []byte("new-secret"), false, ProtectSettings{}, log); err != nil {
		t.Fatal(err)
	}
	if err := CheckProtectPasswords(s, ProtectSettings{}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyProtect(s, ProtectSettings{}, ProtectSettings{}, func() error { return nil }, log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readTestFile(t, vhostPath(s, "shop")), "-v2-") {
		t.Fatal("migration did not namespace sessions")
	}
}

func TestProtectPasswordStoreValidation(t *testing.T) {
	s := isolatedStack(t)
	for _, data := range []string{`{`, `{"global":"plaintext","hosts":{}}`, `{"global":"0123456789abcdef0123456789abcdef","hosts":{}}`, `{"hosts":{"../escape":"` + testProtectHash + `"}}`} {
		if err := os.WriteFile(passwordPath(s), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := PasswordStatus(s); err == nil {
			t.Fatal("invalid password storage accepted")
		}
	}
}
