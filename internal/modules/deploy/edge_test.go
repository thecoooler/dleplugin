package deploy

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/modules/deploy/internal/deploysites"
	"github.com/zalando/go-keyring"
)

func TestRememberPassword(t *testing.T) {
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
	var out, diag bytes.Buffer
	rt := app.NewRuntime(strings.NewReader(""), &out, &diag, "")
	rt.UI.SetVerbose(true)
	rememberPassword(rt, "example.com", "admin", "secret")
	if p, ok, err := deploysites.Password("example.com", "admin"); err != nil || !ok || p != "secret" {
		t.Fatalf("%q %v %v", p, ok, err)
	}
	if !strings.Contains(diag.String(), "password for admin@example.com saved") || strings.Contains(diag.String(), "secret") {
		t.Fatalf("%s", &diag)
	}
	diag.Reset()
	keyring.MockInitWithError(errors.New("unavailable"))
	rememberPassword(rt, "example.com", "admin", "secret")
	if !strings.Contains(diag.String(), "⚠") || !strings.Contains(diag.String(), "--password-env") || strings.Contains(diag.String(), "secret") {
		t.Fatalf("%s", &diag)
	}
}

func TestEmptyPasswordEnvironment(t *testing.T) {
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
	t.Setenv("DEPLOY_EMPTY_TEST", "")
	t.Setenv(PasswordEnv, "default-secret")
	var out, diag bytes.Buffer
	rt := app.NewRuntime(strings.NewReader(""), &out, &diag, "")
	if _, _, err := readPassword(rt, false, "DEPLOY_EMPTY_TEST", "example.com", "admin"); err == nil || !strings.Contains(err.Error(), "DEPLOY_EMPTY_TEST") {
		t.Fatalf("empty explicit env: %v", err)
	}
	// An empty explicit variable falls through to the saved credential, never to the default variable.
	if err := deploysites.SavePassword("example.com", "admin", "saved"); err != nil {
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
