package host

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
)

func TestProtectPrecedence(t *testing.T) {
	for _, tc := range []struct {
		mode   string
		global bool
		want   bool
	}{
		{"default", false, false}, {"default", true, true},
		{"on", false, true}, {"on", true, true},
		{"off", false, false}, {"off", true, false},
	} {
		value, err := ParseProtect(tc.mode)
		if err != nil {
			t.Fatal(err)
		}
		h := Host{Protect: value}
		if got := h.Protected(tc.global); got != tc.want {
			t.Errorf("%s with global %v: got %v", tc.mode, tc.global, got)
		}
		if h.ProtectMode() != tc.mode {
			t.Errorf("mode %q shown as %q", tc.mode, h.ProtectMode())
		}
	}
	for _, bad := range []string{"yes", "ON", "true", "inherit"} {
		if _, err := ParseProtect(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestProtectRegistryCompatibility(t *testing.T) {
	s := isolatedStack(t)
	seedProtectPassword(t, s)
	// The old format without the protect field means default; writing default adds no field.
	legacy := `[{"name":"dle","domain":"dle.dle.icooler.opik.net","external_origin":"https://dle.dle.icooler.opik.net","dle":"21.0","php":"8.5","database":"dle_dle","created_at":"2026-10-01T00:00:00Z"}]`
	if err := os.WriteFile(s.RegistryPath(), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	hosts, err := loadRegistry(s)
	if err != nil {
		t.Fatal(err)
	}
	if hosts["dle"].ProtectMode() != ProtectDefault {
		t.Fatal("old record is not default")
	}
	if err := saveRegistry(s, hosts); err != nil {
		t.Fatal(err)
	}
	saved := readTestFile(t, s.RegistryPath())
	if strings.Contains(saved, "protect") || !strings.Contains(saved, `"domain": "dle.dle.icooler.opik.net"`) || !strings.Contains(saved, `"external_origin": "https://dle.dle.icooler.opik.net"`) {
		t.Fatalf("registry fields changed: %s", saved)
	}
	// An unknown mode (manual edit) must not silently turn protection off.
	for _, bad := range []string{"yes", "default", "ON"} {
		if err := os.WriteFile(s.RegistryPath(), []byte(`[{"name":"dle","dle":"21.0","php":"8.5","database":"dle_dle","protect":"`+bad+`"}]`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadRegistry(s); err == nil {
			t.Errorf("accepted protect %q", bad)
		}
	}
}

func TestVhostGate(t *testing.T) {
	h := Host{Name: "shop", DomainName: "shop.dle.icooler.opik.net", ExternalOrigin: "https://shop.dle.icooler.opik.net", DLE: "21.0", PHP: "8.5"}
	open := vhostConfig(h, false, 0)
	for _, unexpected := range []string{gateMarker, "ServerAlias", "RewriteEngine", "__dlehost"} {
		if strings.Contains(open, unexpected) {
			t.Fatalf("unprotected vhost contains %q", unexpected)
		}
	}
	gated := vhostConfig(h, true, 30*24*time.Hour)
	for _, expected := range []string{
		"ServerName shop.dle.icooler.opik.net",
		"ServerAlias shop.localhost shop.*.localhost",
		gateMarker + "shop",
		`AliasMatch "^/__dlehost/(login|logout)$" "/opt/dlehost/gate/gate.php"`,
		"RewriteCond %{REQUEST_URI} !^/__dlehost/(login|logout)$",
		"RewriteRule ^ /__dlehost/login [PT,L]",
		"SetEnv DLEHOST_GATE_TTL 2592000",
		`-f '/var/lib/dlehost/gate/shop/' . sha1($1)`,
		// HTTPS accepts only the Secure cookie, HTTP aliases only the plain one.
		`req('X-Forwarded-Proto') == 'https' && %{HTTP_COOKIE} =~ /(?:^|;)\s*__Host-dlehost_gate=`,
		`req('X-Forwarded-Proto') != 'https' && %{HTTP_COOKIE} =~ /(?:^|;)\s*dlehost_gate=`,
		`ProxyFCGISetEnvIf "req('X-Forwarded-Proto') == 'https'" HTTPS "on"`,
	} {
		if !strings.Contains(gated, expected) {
			t.Fatalf("protected vhost missing %q:\n%s", expected, gated)
		}
	}
	if strings.Contains(gated, "WWW-Authenticate") || strings.Contains(gated, "AuthType") || strings.Contains(gated, "<Location />") {
		t.Fatal("Basic auth or claimed Require backstop in vhost")
	}
	// A local host without an HTTPS origin knows only the plain cookie.
	local := vhostConfig(Host{Name: "dev", DLE: "21.0", PHP: "7.4"}, true, 0)
	if strings.Contains(local, "__Host-") || strings.Contains(local, "X-Forwarded-Proto") || !strings.Contains(local, "SetEnv DLEHOST_GATE_TTL 0") {
		t.Fatalf("local gate:\n%s", local)
	}
}

// fakeDocker replaces docker: it logs calls (and stdin) and fails when the
// arguments or stdin contain the string from the fail file. On graceful it
// records whether the directory from DLEHOST_TEST_WATCH already exists.
func fakeDocker(t *testing.T) (calls, fail string) {
	t.Helper()
	bin := t.TempDir()
	calls, fail = filepath.Join(bin, "calls"), filepath.Join(bin, "fail")
	script := "#!/bin/sh\n" + fakeInstallerPort + "in=$(cat)\necho \"$* $in\" >> " + calls + "\n" +
		"case \"$*\" in *graceful*) if [ -n \"$DLEHOST_TEST_WATCH\" ] && [ -e \"$DLEHOST_TEST_WATCH\" ]; then echo published-before-reload >> " + calls + "; fi;; esac\n" +
		"if [ -s " + fail + " ]; then pat=$(cat " + fail + "); case \"$* $in\" in *\"$pat\"*) exit 1;; esac; fi\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return calls, fail
}

func registerTestHosts(t *testing.T, s *stack.Stack, hosts ...Host) {
	t.Helper()
	m := map[string]Host{}
	for _, h := range hosts {
		h.DLE, h.PHP, h.Database, h.CreatedAt = "21.0", "8.5", "dle_"+h.Name, time.Unix(0, 0).UTC()
		m[h.Name] = h
		if err := os.WriteFile(vhostPath(s, h.Name), []byte(vhostConfig(h, false, 0)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveRegistry(s, m); err != nil {
		t.Fatal(err)
	}
}

func TestApplyProtectGlobal(t *testing.T) {
	s := isolatedStack(t)
	seedProtectPassword(t, s)
	calls, _ := fakeDocker(t)
	registerTestHosts(t, s, Host{Name: "inherit"}, Host{Name: "on", Protect: "on"}, Host{Name: "off", Protect: "off"})
	gated := func(name string) bool { return strings.Contains(readTestFile(t, vhostPath(s, name)), gateMarker) }
	committed := 0
	commit := func() error { committed++; return nil }

	// An on host is closed even with global off: the host mode wins.
	if _, err := SetProtect(s, "on", "on", ProtectSettings{}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !gated("on") || gated("inherit") || gated("off") {
		t.Fatal("site mode with global off")
	}
	if err := ApplyProtect(s, ProtectSettings{Global: true}, ProtectSettings{}, commit, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !gated("inherit") || !gated("on") || gated("off") || committed != 1 {
		t.Fatal("global on")
	}
	if info, err := os.Stat(s.GateDir("inherit")); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("session dir: %v %v", info, err)
	}
	log := readTestFile(t, calls)
	if !strings.Contains(log, "exec -T -u www-data apache sh -c test -r /opt/dlehost/gate/gate.php") || !strings.Contains(log, "php85") || !strings.Contains(log, "graceful") {
		t.Fatalf("gate check or reload missing:\n%s", log)
	}
	// A repeat without changes does not touch apache.
	before := readTestFile(t, calls)
	if err := ApplyProtect(s, ProtectSettings{Global: true}, ProtectSettings{}, commit, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if readTestFile(t, calls) != before {
		t.Fatal("apache reloaded without changes")
	}
	if err := os.WriteFile(filepath.Join(s.GateDir("inherit"), "session"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyProtect(s, ProtectSettings{}, ProtectSettings{Global: true}, commit, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if gated("inherit") || !gated("on") || gated("off") {
		t.Fatal("global off")
	}
	if _, err := os.Stat(s.GateDir("inherit")); !os.IsNotExist(err) {
		t.Fatal("sessions of unprotected host kept")
	}
	if _, err := os.Stat(s.GateDir("on")); err != nil {
		t.Fatal("sessions of protected host removed")
	}
	// default follows the global setting again.
	if _, err := SetProtect(s, "off", "default", ProtectSettings{}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	hosts, _ := loadRegistry(s)
	if hosts["off"].Protect != "" || gated("off") {
		t.Fatal("default mode")
	}
	if _, err := SetProtect(s, "missing", "on", ProtectSettings{}, func(string) {}); err == nil {
		t.Fatal("unknown host")
	}
	if _, err := SetProtect(s, "on", "maybe", ProtectSettings{}, func(string) {}); err == nil {
		t.Fatal("unknown mode")
	}
}

func TestApplyProtectRollback(t *testing.T) {
	s := isolatedStack(t)
	seedProtectPassword(t, s)
	_, fail := fakeDocker(t)
	registerTestHosts(t, s, Host{Name: "shop"})
	vhost := readTestFile(t, vhostPath(s, "shop"))
	registry := readTestFile(t, s.RegistryPath())

	// apache rejected the config: vhost and registry unchanged.
	if err := os.WriteFile(fail, []byte("configtest"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SetProtect(s, "shop", "on", ProtectSettings{}, func(string) {}); err == nil {
		t.Fatal("reload failure ignored")
	}
	if readTestFile(t, vhostPath(s, "shop")) != vhost || readTestFile(t, s.RegistryPath()) != registry {
		t.Fatal("not rolled back after apache failure")
	}
	if err := os.Remove(fail); err != nil {
		t.Fatal(err)
	}
	// The config was not saved: the vhost is restored and apache reloaded again.
	err := ApplyProtect(s, ProtectSettings{Global: true}, ProtectSettings{}, func() error { return errors.New("disk full") }, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("commit failure: %v", err)
	}
	if readTestFile(t, vhostPath(s, "shop")) != vhost {
		t.Fatal("not rolled back after commit failure")
	}
}

func TestAddProtect(t *testing.T) {
	for _, tc := range []struct {
		mode   string
		global bool
		want   bool
	}{{"default", false, false}, {"default", true, true}, {"off", true, false}, {"on", false, true}} {
		t.Run(tc.mode, func(t *testing.T) {
			s := isolatedStack(t)
			seedProtectPassword(t, s)
			dists := t.TempDir()
			writeTestDist(t, dists, "21.0")
			res, err := Add(s, AddOptions{Name: "shop", DLE: "21.0", PHP: "8.5", PHPVersions: []string{"8.5"}, DistsDir: dists,
				Admin: Credentials{"admin", "secret"}, Protect: tc.mode, Settings: ProtectSettings{Global: tc.global}}, func(string) {})
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(readTestFile(t, vhostPath(s, "shop")), gateMarker); got != tc.want {
				t.Fatalf("gate=%v", got)
			}
			if res.Host.ProtectMode() != tc.mode {
				t.Fatalf("mode=%s", res.Host.ProtectMode())
			}
			if err := Remove(s, "shop", func(string) {}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(s.GateDir("shop")); !os.IsNotExist(err) {
				t.Fatal("sessions kept after remove")
			}
		})
	}
	s := isolatedStack(t)
	seedProtectPassword(t, s)
	if _, err := Add(s, AddOptions{Name: "shop", DLE: "21.0", PHP: "8.5", PHPVersions: []string{"8.5"}, DistsDir: t.TempDir(),
		Admin: Credentials{"admin", "secret"}, Protect: "yes"}, func(string) {}); err == nil {
		t.Fatal("accepted invalid mode")
	}
}

func TestAddProtectRollback(t *testing.T) {
	s := isolatedStack(t)
	seedProtectPassword(t, s)
	_, fail := fakeDocker(t)
	dists := t.TempDir()
	writeTestDist(t, dists, "21.0")
	if err := os.WriteFile(fail, []byte("configtest"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(s, AddOptions{Name: "shop", DLE: "21.0", PHP: "8.5", PHPVersions: []string{"8.5"}, DistsDir: dists,
		Admin: Credentials{"admin", "secret"}, Protect: "on"}, func(string) {}); err == nil {
		t.Fatal("apache failure ignored")
	}
	for _, path := range []string{vhostPath(s, "shop"), s.GateDir("shop"), s.HostDir("shop"), s.RegistryPath()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s left after rollback", path)
		}
	}
	if entries, _ := os.ReadDir(s.StagingDir()); len(entries) != 0 {
		t.Error("staging left after rollback")
	}
}

func TestProtectSessionTTL(t *testing.T) {
	s := isolatedStack(t)
	seedProtectPassword(t, s)
	calls, _ := fakeDocker(t)
	registerTestHosts(t, s, Host{Name: "on", Protect: "on"}, Host{Name: "off", Protect: "off"})
	unlimited := ProtectSettings{}
	if _, err := SetProtect(s, "on", "on", unlimited, func(string) {}); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(s.GateDir("on"), "0123456789abcdef0123456789abcdef01234567")
	writeSession := func() {
		t.Helper()
		if err := os.WriteFile(session, []byte("99991231235959\nowner\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeSession()
	offVhost := readTestFile(t, vhostPath(s, "off"))
	apply := func(settings, prev ProtectSettings) {
		t.Helper()
		if err := ApplyProtect(s, settings, prev, func() error { return nil }, func(string) {}); err != nil {
			t.Fatal(err)
		}
	}
	// never → 30d: the lifetime got shorter, so non-expiring sessions are removed and the directory stays.
	month := ProtectSettings{SessionTTL: 30 * 24 * time.Hour}
	apply(month, unlimited)
	if !strings.Contains(readTestFile(t, vhostPath(s, "on")), "SetEnv DLEHOST_GATE_TTL 2592000") {
		t.Fatal("TTL not applied to protected vhost")
	}
	if readTestFile(t, vhostPath(s, "off")) != offVhost {
		t.Fatal("unprotected vhost rewritten for TTL")
	}
	if _, err := os.Stat(session); !os.IsNotExist(err) {
		t.Fatal("sessions kept after shortening TTL")
	}
	if info, err := os.Stat(s.GateDir("on")); err != nil || !info.IsDir() {
		t.Fatal("session dir of protected host removed")
	}
	// 30d → never and 30d → 60d: the lifetime got longer, so sessions are kept.
	writeSession()
	apply(ProtectSettings{SessionTTL: 60 * 24 * time.Hour}, month)
	apply(unlimited, ProtectSettings{SessionTTL: 60 * 24 * time.Hour})
	if _, err := os.Stat(session); err != nil {
		t.Fatal("sessions removed after extending TTL")
	}
	// Failed config write: vhost and sessions unchanged.
	before := readTestFile(t, vhostPath(s, "on"))
	if err := ApplyProtect(s, month, unlimited, func() error { return errors.New("disk full") }, func(string) {}); err == nil {
		t.Fatal("commit failure ignored")
	}
	if readTestFile(t, vhostPath(s, "on")) != before {
		t.Fatal("TTL vhost not rolled back")
	}
	if _, err := os.Stat(session); err != nil {
		t.Fatal("sessions removed although TTL change failed")
	}
	if !strings.Contains(readTestFile(t, calls), "graceful") {
		t.Fatal("apache not reloaded")
	}
	// reset ends the host sessions; the directory of a closed host stays.
	if err := ResetSessions(s, "on", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(session); !os.IsNotExist(err) {
		t.Fatal("reset kept session")
	}
	if _, err := os.Stat(s.GateDir("on")); err != nil {
		t.Fatal("reset removed session dir")
	}
	if ResetSessions(s, "missing", func(string) {}) == nil {
		t.Fatal("reset of unknown host")
	}
}

func TestApplyProtectRestoresMissingVhost(t *testing.T) {
	s := isolatedStack(t)
	seedProtectPassword(t, s)
	_, fail := fakeDocker(t)
	registerTestHosts(t, s, Host{Name: "shop"}, Host{Name: "lost"})
	if err := os.Remove(vhostPath(s, "lost")); err != nil {
		t.Fatal(err)
	}
	// Apache refused: the restored file is removed again, the others are restored.
	if err := os.WriteFile(fail, []byte("configtest"), 0o644); err != nil {
		t.Fatal(err)
	}
	shop := readTestFile(t, vhostPath(s, "shop"))
	if err := ApplyProtect(s, ProtectSettings{Global: true}, ProtectSettings{}, func() error { return nil }, func(string) {}); err == nil {
		t.Fatal("apache failure ignored")
	}
	if _, err := os.Stat(vhostPath(s, "lost")); !os.IsNotExist(err) {
		t.Fatal("restored vhost kept after rollback")
	}
	if readTestFile(t, vhostPath(s, "shop")) != shop {
		t.Fatal("shop not rolled back")
	}
	if err := os.Remove(fail); err != nil {
		t.Fatal(err)
	}
	// Success: the missing vhost is restored from the registry and closed by the form.
	if err := ApplyProtect(s, ProtectSettings{Global: true}, ProtectSettings{}, func() error { return nil }, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readTestFile(t, vhostPath(s, "lost")), gateMarker+"lost") {
		t.Fatal("missing vhost not restored protected")
	}
}

func TestRemoveKeepsHostClosedOnFailure(t *testing.T) {
	s := isolatedStack(t)
	seedProtectPassword(t, s)
	calls, fail := fakeDocker(t)
	registerTestHosts(t, s, Host{Name: "shop", Protect: "on"})
	if _, err := SetProtect(s, "shop", "on", ProtectSettings{}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.HostDir("shop"), "engine"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Database removal error: files, closed vhost and registry stay.
	if err := os.WriteFile(fail, []byte("DROP DATABASE"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(s, "shop", func(string) {}); err == nil {
		t.Fatal("database failure ignored")
	}
	if !strings.Contains(readTestFile(t, vhostPath(s, "shop")), gateMarker) {
		t.Fatal("vhost removed before database: files exposed via shop.localhost")
	}
	if _, err := os.Stat(s.HostDir("shop")); err != nil {
		t.Fatal(err)
	}
	if hosts, _ := loadRegistry(s); hosts["shop"].Name == "" {
		t.Fatal("registry entry removed")
	}
	// A repeat after the fix removes everything, the vhost after the files.
	if err := os.Remove(fail); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(calls, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DLEHOST_TEST_WATCH", s.HostDir("shop"))
	if err := Remove(s, "shop", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readTestFile(t, calls), "published-before-reload") {
		t.Fatal("vhost removed while files still existed")
	}
	for _, path := range []string{vhostPath(s, "shop"), s.HostDir("shop"), s.GateDir("shop")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s left", path)
		}
	}
}

func TestAddPublishesFilesAfterVhost(t *testing.T) {
	s := isolatedStack(t)
	seedProtectPassword(t, s)
	calls, _ := fakeDocker(t)
	dists := t.TempDir()
	writeTestDist(t, dists, "21.0")
	t.Setenv("DLEHOST_TEST_WATCH", s.HostDir("shop"))
	if _, err := Add(s, AddOptions{Name: "shop", DLE: "21.0", PHP: "8.5", PHPVersions: []string{"8.5"}, DistsDir: dists,
		Admin: Credentials{"admin", "secret"}, Protect: "on"}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readTestFile(t, calls), "published-before-reload") {
		t.Fatal("site files were in www/ before the protected vhost was loaded")
	}
	if !strings.Contains(readTestFile(t, filepath.Join(s.HostDir("shop"), "engine/data/dbconfig.php")), `define ("DBNAME", "dle_shop")`) {
		t.Fatal("DLE config not prepared before publication")
	}
	if entries, _ := os.ReadDir(s.StagingDir()); len(entries) != 0 {
		t.Fatal("staging left")
	}
	// Control of the check itself: a reload with published files is recorded.
	if _, err := SetProtect(s, "shop", "off", ProtectSettings{}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readTestFile(t, calls), "published-before-reload") {
		t.Fatal("publication watch does not work")
	}
}

// Hash of a known test password, with the same cost as the CLI and PHP.
const testProtectHash = "$2a$10$pMVK93Fjc2/Dp9KllbU9Ju1StdQUNCkzO1nXCik70k9DYSVYoby72"

func seedProtectPassword(t *testing.T, s *stack.Stack) {
	t.Helper()
	if err := savePasswords(s, protectPasswords{Global: testProtectHash, Hosts: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
}
