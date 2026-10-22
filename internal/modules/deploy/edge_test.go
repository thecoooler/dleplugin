package deploy

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/modules/deploy/internal/deploysites"
	"github.com/zalando/go-keyring"
)

// testRuntime returns a runtime whose global config lives in a temporary directory.
func testRuntime(t *testing.T) (*app.Runtime, *bytes.Buffer) {
	t.Helper()
	t.Setenv(CredentialStoreEnv, "")
	var out, diag bytes.Buffer
	rt := app.NewRuntime(strings.NewReader(""), &out, &diag, filepath.Join(t.TempDir(), "config.yml"))
	return rt, &diag
}

func TestRememberPassword(t *testing.T) {
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
	rt, diag := testRuntime(t)
	rt.UI.SetVerbose(true)
	c, err := credentials(rt)
	if err != nil {
		t.Fatal(err)
	}
	rememberPassword(rt, "example.com", "admin", "secret")
	if p, loc, err := c.Password("example.com", "admin"); err != nil || loc != deploysites.InKeyring || p != "secret" {
		t.Fatalf("%q %v %v", p, loc, err)
	}
	if !strings.Contains(diag.String(), "password for admin@example.com saved") || strings.Contains(diag.String(), "secret") {
		t.Fatalf("%s", diag)
	}
	// An unavailable credential store falls back to the file with a warning.
	diag.Reset()
	keyring.MockInitWithError(errors.New("unavailable"))
	rememberPassword(rt, "example.com", "admin", "secret")
	if !strings.Contains(diag.String(), "⚠") || !strings.Contains(diag.String(), c.Path) || !strings.Contains(diag.String(), CredentialStoreEnv+"=file") || strings.Contains(diag.String(), "secret") {
		t.Fatalf("%s", diag)
	}
	if p, loc, err := c.Password("example.com", "admin"); err != nil || loc != deploysites.InFile || p != "secret" {
		t.Fatalf("%q %v %v", p, loc, err)
	}
	// A failing file is reported together with the credential store failure.
	diag.Reset()
	if err := os.Chmod(c.Path, 0o644); err != nil {
		t.Fatal(err)
	}
	rememberPassword(rt, "example.com", "admin", "secret")
	if !strings.Contains(diag.String(), "unavailable") || !strings.Contains(diag.String(), "chmod 600") || !strings.Contains(diag.String(), "--password-env") {
		t.Fatalf("%s", diag)
	}
	if err := os.Chmod(c.Path, 0o600); err != nil {
		t.Fatal(err)
	}
	// Keyring-only mode keeps the old warning and does not touch the file.
	diag.Reset()
	t.Setenv(CredentialStoreEnv, deploysites.StoreKeyring)
	rememberPassword(rt, "other.com", "admin", "secret")
	if !strings.Contains(diag.String(), "--password-env") || strings.Contains(diag.String(), "secret") {
		t.Fatalf("%s", diag)
	}
	if _, loc, _ := c.Password("other.com", "admin"); loc != "" {
		t.Fatalf("keyring mode wrote the file: %v", loc)
	}
	// An unknown mode is reported, not silently ignored.
	diag.Reset()
	t.Setenv(CredentialStoreEnv, "vault")
	rememberPassword(rt, "example.com", "admin", "secret")
	if !strings.Contains(diag.String(), CredentialStoreEnv) || !strings.Contains(diag.String(), "vault") {
		t.Fatalf("%s", diag)
	}
	if _, _, err := readPassword(rt, false, "", "example.com", "admin"); err == nil || !strings.Contains(err.Error(), "vault") {
		t.Fatalf("%v", err)
	}
}

func TestEmptyPasswordEnvironment(t *testing.T) {
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
	t.Setenv("DEPLOY_EMPTY_TEST", "")
	t.Setenv(PasswordEnv, "default-secret")
	rt, _ := testRuntime(t)
	if _, _, err := readPassword(rt, false, "DEPLOY_EMPTY_TEST", "example.com", "admin"); err == nil || !strings.Contains(err.Error(), "DEPLOY_EMPTY_TEST") {
		t.Fatalf("empty explicit env: %v", err)
	}
	// An empty explicit variable falls through to the saved credential, never to the default variable.
	c, err := credentials(rt)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.SavePassword("example.com", "admin", "saved"); err != nil {
		t.Fatal(err)
	}
	if p, source, err := readPassword(rt, false, "DEPLOY_EMPTY_TEST", "example.com", "admin"); err != nil || p != "saved" || source != fromStore {
		t.Fatalf("%q %v %v", p, source, err)
	}
	// An empty --password-env selects the default environment variable.
	if p, source, err := readPassword(rt, false, "", "example.com", "admin"); err != nil || p != "default-secret" || source != fromEnv {
		t.Fatalf("%q %v %v", p, source, err)
	}
}
