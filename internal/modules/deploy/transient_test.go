package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/dleadmin/dleadmintest"
	"github.com/thecoooler/dleplugin/internal/modules/deploy/internal/deploysites"
)

func TestTransientDeployFlagsNotSaved(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret")
	e := newEnv(t, "secret\n", false)
	fixture(t, "admin_section_only", e.work)
	writeXML(t, e.work, "New Plugin")
	if code := e.run("deploy", "sites", "add", s.URL, "-u", "admin", "--password-stdin", "--insecure"); code != 0 {
		t.Fatal(e.errB.String())
	}
	path := filepath.Join(e.work, deploysites.Filename)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before), "insecure") || strings.Contains(string(before), "new:") {
		t.Fatalf("transient flags persisted: %s", before)
	}
	host, err := deploysites.Host(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	if code := e.run("deploy", "upload", host, "-f", "ready.xml", "--insecure", "--new"); code != 0 {
		t.Fatal(e.errB.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("upload changed registry:\n%s", after)
	}
	if uploads := s.Uploads(); len(uploads) != 1 || uploads[0].ID != "" {
		t.Fatalf("%+v", uploads)
	}
}
