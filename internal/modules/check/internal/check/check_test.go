package check

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/dlexml"
	"github.com/thecoooler/dleplugin/internal/manifest"
)

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeDist(t *testing.T, dir, version string) {
	t.Helper()
	write(t, filepath.Join(dir, "engine/inc/include/init.php"), "<?php define('VERSIONID', '"+version+"');")
}

func TestSearchPattern(t *testing.T) {
	for _, tt := range []struct {
		code, text string
		want       bool
	}{
		{"if ($x) {\n    echo 'ok';\n}", "if   ($x) {\n\techo 'ok';\n}", true},
		{"a  b", "ab", true},
		{"Echo $X;", "echo $x;", true},
		{"a.b", "axb", false},
		{"$a[1]", "$a[1]", true},
	} {
		if got := SearchPattern(tt.code).MatchString(tt.text); got != tt.want {
			t.Errorf("SearchPattern(%q).Match(%q) = %v", tt.code, tt.text, got)
		}
	}
}

func TestOpenDist(t *testing.T) {
	dir := t.TempDir()
	writeDist(t, dir, "18.1")
	d, err := OpenDist(dir)
	if err != nil || d != (Dist{Dir: dir, Version: "18.1"}) {
		t.Errorf("OpenDist = %+v, %v", d, err)
	}

	write(t, filepath.Join(dir, "engine/inc/include/init.php"), "<?php\n")
	if d, err := OpenDist(dir); err != nil || d.Version != "" {
		t.Errorf("OpenDist without VERSIONID = %+v, %v", d, err)
	}

	empty := t.TempDir()
	if _, err := OpenDist(empty); err == nil || !strings.Contains(err.Error(), "is not a DLE directory") {
		t.Errorf("OpenDist(empty) err = %v", err)
	}
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	writeDist(t, dir, "13.0")
	write(t, filepath.Join(dir, "engine/modules/main.php"), "<?php\nalpha beta\nalpha beta\nneedle\n")
	ops := []Operation{
		{File: "engine/modules/main.php", Action: "before", Search: "alpha beta"},
		{File: "engine/modules/main.php", Action: "after", Search: "missing text"},
		{File: "engine/modules/main.php", Action: "replace", Search: "NEEDLE"},
		{File: "engine/modules/main.php", Action: "before", Search: " \n"},
		{File: "engine/modules/missing.php", Action: "replace", Search: "anything"},
		{File: "engine/modules/missing.php", Action: "after", Search: "again"},
		{File: "engine/modules/new.php", Action: "create"},
		{File: "engine/modules/new.php", Action: "replace", Search: "in the new file"},
		{File: "engine/modules/main.php", Action: "unknown", Search: "skipped"},
	}
	r, err := Run(Dist{Dir: dir, Version: "13.0"}, ops)
	if err != nil {
		t.Fatal(err)
	}
	want := []Problem{
		{Kind: Duplicate, File: "engine/modules/main.php", Search: "alpha beta", Count: 2},
		{Kind: TextNotFound, File: "engine/modules/main.php", Search: "missing text"},
		{Kind: EmptySearch, File: "engine/modules/main.php"},
		{Kind: FileNotFound, File: "engine/modules/missing.php"},
	}
	if !reflect.DeepEqual(r.Problems, want) {
		t.Errorf("problems = %+v\nwant %+v", r.Problems, want)
	}
	if !r.Failed() {
		t.Error("Failed() = false")
	}
	if (Report{Problems: want[:1]}).Failed() {
		t.Error("a duplicate alone must not fail")
	}
}

func TestFromInfoAndXML(t *testing.T) {
	info, err := manifest.Parse([]byte(`slug: example
name: Example
files:
  engine/modules/main.php:
    - before: {search: needle, replace: code}
    - after: {search: other, replace: code}
  engine/modules/new.php:
    - create: new file
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Operation{
		{File: "engine/modules/main.php", Action: "before", Search: "needle"},
		{File: "engine/modules/main.php", Action: "after", Search: "other"},
		{File: "engine/modules/new.php", Action: "create"},
	}
	if got := FromInfo(info); !reflect.DeepEqual(got, want) {
		t.Errorf("FromInfo = %+v", got)
	}

	root, err := dlexml.Parse(strings.NewReader(`<dleplugin><name>x</name>
<file name="engine/modules/main.php">
  <operation action="before"><searchcode><![CDATA[needle]]></searchcode><replacecode>code</replacecode></operation>
  <operation action="after"><searchcode>other</searchcode></operation>
</file>
<file name="engine/modules/new.php"><operation action="create"><replacecode>new file</replacecode></operation></file>
</dleplugin>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := FromXML(root); !reflect.DeepEqual(got, want) {
		t.Errorf("FromXML = %+v", got)
	}
}
