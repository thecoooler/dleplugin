package convert

import (
	"bytes"
	"encoding/xml"
	"github.com/thecoooler/dleplugin/internal/goldentest"
	"github.com/thecoooler/dleplugin/internal/manifest"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIndent(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"a\nb", 2, "  a\n  b"},
		{"a\n\n  \nb\n", 2, "  a\n\n  \n  b\n"},
		{"a\r\nb\rc\x0bd e", 1, " a\r\n b\r c\x0b d  e"},
		{"", 3, ""},
		{" \nx", 2, " \n  x"},
	}
	for _, c := range cases {
		if got := indent(c.in, c.n); got != c.want {
			t.Errorf("indent(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestSplitLines(t *testing.T) {
	got := splitLinesKeepEnds("a\r\nb\n\nc")
	want := []string{"a\r\n", "b\n", "\n", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
}

func readTree(t *testing.T, dir string) map[string]string {
	m := map[string]string{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		m[rel] = string(b)
		return nil
	})
	return m
}

func TestGolden(t *testing.T) {
	for _, c := range []string{"Basic", "NoFiles", "Escapes"} {
		t.Run(c, func(t *testing.T) {
			gd := filepath.Join("..", "..", "testdata", "golden", "convert", c)
			data, err := os.ReadFile(filepath.Join(gd, c+".xml"))
			if err != nil {
				t.Fatal(err)
			}
			tmp := t.TempDir()
			xmlPath := filepath.Join(tmp, c+".xml")
			if err := os.WriteFile(xmlPath, data, 0o644); err != nil {
				t.Fatal(err)
			}
			src := filepath.Join(tmp, "src")
			got, err := Convert(xmlPath, src, "")
			if err != nil {
				t.Fatal(err)
			}
			goldentest.Update(t, filepath.Join(gd, "expected.yml"), []byte(got))
			goldentest.UpdateTree(t, filepath.Join(gd, "expected_src"), readTree(t, src))
			want, _ := os.ReadFile(filepath.Join(gd, "expected.yml"))
			if got != string(want) {
				t.Errorf("yml mismatch:\n%q\n%q", got, want)
			}
			if !reflect.DeepEqual(readTree(t, src), readTree(t, filepath.Join(gd, "expected_src"))) {
				t.Errorf("src tree mismatch: %v", readTree(t, src))
			}
			got, err = Convert(xmlPath, "", "")
			if err != nil {
				t.Fatal(err)
			}
			goldentest.Update(t, filepath.Join(gd, "expected_nosrc.yml"), []byte(got))
			want, _ = os.ReadFile(filepath.Join(gd, "expected_nosrc.yml"))
			if got != string(want) {
				t.Errorf("nosrc mismatch:\n%q\n%q", got, want)
			}
		})
	}
}

func TestDependencies(t *testing.T) {
	dir := t.TempDir()
	xml := filepath.Join(dir, "Demo.xml")
	if err := os.WriteFile(xml, []byte(`<?xml version="1.0" encoding="utf-8"?><dleplugin><name>Demo</name><description>d</description>`+
		`<needplugin>ExampleDependency, Other ,</needplugin><mysqlinstall></mysqlinstall></dleplugin>`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Convert(xml, filepath.Join(dir, "src"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "mount: engine/Demo\ndependencies:\n  - ExampleDependency\n  - Other\nfiles:") {
		t.Errorf("out = %s", out)
	}
}

func TestMissing(t *testing.T) {
	if _, err := Convert("/nonexistent/x.xml", "", ""); err == nil {
		t.Fatal("want error")
	}
}

func TestCodeRoundTrip(t *testing.T) {
	for _, text := range []string{
		"plain\nsecond line", "\tfoo\n\tbar", "  leading spaces\n    more spaces",
		"\n\n\tfoo", "\n\n  foo", "\nplain\n", "\tfoo\n\n", "  foo\n\n",
	} {
		t.Run(text, func(t *testing.T) {
			var escaped bytes.Buffer
			if err := xml.EscapeText(&escaped, []byte(text)); err != nil {
				t.Fatal(err)
			}
			code := escaped.String()
			content := `<plugin><name>Demo</name><description>Demo</description>` +
				`<file name="engine/test.php"><operation action="before"><searchcode>` + code +
				`</searchcode><replacecode>` + code + `</replacecode></operation></file>`
			for _, hook := range manifest.LifecycleHooks {
				content += "<mysql" + hook + ">" + code + "</mysql" + hook + ">"
			}
			content += `</plugin>`
			input := filepath.Join(t.TempDir(), "Demo.xml")
			if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			output, err := Convert(input, "", "")
			if err != nil {
				t.Fatal(err)
			}
			info, err := manifest.Parse([]byte(output))
			if err != nil {
				t.Fatalf("%v\n%s", err, output)
			}
			want := strings.Trim(text, "\r\n")
			patch, ok := info.Files.Get("engine/test.php")
			if !ok || len(patch.Actions) != 1 {
				t.Fatalf("missing patch: %+v", patch)
			}
			if action := patch.Actions[0]; action.Search != want || action.Replace != want {
				t.Errorf("patch = %+v, want search and replace %q", action, want)
			}
			for _, hook := range manifest.LifecycleHooks {
				if got := info.MySQL.Get(hook); got != want {
					t.Errorf("mysql.%s = %q, want %q", hook, got, want)
				}
			}
		})
	}
}

func TestRejectUnsafePathsBeforeWriting(t *testing.T) {
	for _, name := range []string{"../victim", "../../victim", "/tmp/victim", `..\victim`, `C:\victim`, "engine/../victim"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			victim := filepath.Join(dir, "victim")
			if err := os.WriteFile(victim, []byte("unchanged"), 0o644); err != nil {
				t.Fatal(err)
			}
			xml := filepath.Join(dir, "Demo.xml")
			content := `<plugin><name>Demo</name><description>Demo</description><file name="a.php"><operation action="create"><replacecode>first</replacecode></operation></file><file name="` + name + `"><operation action="create"><replacecode>bad</replacecode></operation></file></plugin>`
			if err := os.WriteFile(xml, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			src := filepath.Join(dir, "project", "src")
			if _, err := Convert(xml, src, ""); err == nil {
				t.Fatal("unsafe path accepted")
			}
			if _, err := os.Lstat(src); !os.IsNotExist(err) {
				t.Fatalf("partial source directory: %v", err)
			}
			got, err := os.ReadFile(victim)
			if err != nil || string(got) != "unchanged" {
				t.Fatalf("victim = %q, %v", got, err)
			}
		})
	}
}

func TestInvalidImportLeavesNoSources(t *testing.T) {
	for _, content := range []string{
		`<plugin><name>Demo</name><file name="a.php"><operation action="create"><replacecode>first</replacecode></operation></file></plugin>`,
		`<plugin><name>Demo</name><description>Demo</description><file name="a.php"><operation action="create"><replacecode>first</replacecode></operation></file><file name="./a.php"><operation action="create"><replacecode>second</replacecode></operation></file></plugin>`,
	} {
		dir := t.TempDir()
		xml := filepath.Join(dir, "Demo.xml")
		if err := os.WriteFile(xml, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		src := filepath.Join(dir, "src")
		if _, err := Convert(xml, src, ""); err == nil {
			t.Fatal("invalid import accepted")
		}
		if _, err := os.Lstat(src); !os.IsNotExist(err) {
			t.Fatalf("partial source directory: %v", err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("staging files left behind: %v, %v", entries, err)
		}
	}
}

func TestImportRejectsExistingSourceSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.Symlink(outside, src); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	xml := filepath.Join(dir, "Demo.xml")
	if err := os.WriteFile(xml, []byte(`<plugin><name>Demo</name><description>Demo</description><file name="victim"><operation action="create"><replacecode>bad</replacecode></operation></file></plugin>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Convert(xml, src, ""); err == nil {
		t.Fatal("source symlink accepted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside files = %v, %v", entries, err)
	}
}

func TestQuotedMetadataRoundTrip(t *testing.T) {
	for _, value := range []string{"Demo: Tools", "Demo # note", "true", "123", "2026-10-12", "line one\nline two", `O'Brien "tools"`} {
		t.Run(value, func(t *testing.T) {
			var escaped bytes.Buffer
			if err := xml.EscapeText(&escaped, []byte(value)); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			input := filepath.Join(dir, "Demo.xml")
			content := `<plugin><name>` + escaped.String() + `</name><description>` + escaped.String() + `</description><needplugin>` + escaped.String() + `</needplugin><file name="@special.php"><operation action="before"><searchcode>old</searchcode><replacecode>new</replacecode></operation></file></plugin>`
			if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			output, err := Convert(input, filepath.Join(dir, "src"), "")
			if err != nil {
				t.Fatal(err)
			}
			info, err := manifest.Parse([]byte(output))
			if err != nil {
				t.Fatalf("%v\n%s", err, output)
			}
			if info.Name != value || info.Description != value || len(info.Dependencies) != 1 || info.Dependencies[0] != value {
				t.Fatalf("round trip: %+v", info)
			}
			if _, ok := info.Files.Get("@special.php"); !ok {
				t.Fatal("file key changed")
			}
		})
	}
}

func TestMount(t *testing.T) {
	dir := t.TempDir()
	xml := filepath.Join(dir, "Demo.xml")
	if err := os.WriteFile(xml, []byte(`<?xml version="1.0" encoding="utf-8"?><dleplugin><name>Demo</name><description>d</description>`+
		`<file name="engine/Acme/Demo/Main.php"><operation action="create"><replacecode>x</replacecode></operation></file></dleplugin>`), 0o644); err != nil {
		t.Fatal(err)
	}
	for ns, want := range map[string]string{"": "engine/Acme/Demo", `Other\Demo`: "engine/Other/Demo"} {
		out, err := Convert(xml, "", ns)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "\nmount: "+want+"\n") {
			t.Errorf("%q: out = %s", ns, out)
		}
	}
	if _, err := Convert(xml, "", "bad-ns"); err == nil || !strings.Contains(err.Error(), "invalid PHP namespace") {
		t.Errorf("err = %v", err)
	}
}

func TestExtractSourcePaths(t *testing.T) {
	for _, tc := range []struct {
		name      string
		namespace string
		targets   []string
		paths     []string
		aliases   map[string]string
	}{
		{"in mount", "", []string{"engine/Acme/Demo/Main.php"}, []string{"Main.php"}, nil},
		{"outside mount", "", []string{"engine/inc/demo.php"}, []string{"engine/inc/demo.php"}, map[string]string{"engine/inc/demo.php": "engine/inc/demo.php"}},
		{"mixed", "", []string{"engine/Acme/Demo/Main.php", "engine/inc/demo.php"}, []string{"Main.php", "engine/inc/demo.php"}, map[string]string{"engine/inc/demo.php": "engine/inc/demo.php"}},
		{"explicit namespace", `Other\Demo`, []string{"engine/Acme/Demo/Main.php", "engine/Other/Demo/Other.php"}, []string{"engine/Acme/Demo/Main.php", "Other.php"}, map[string]string{"engine/Acme/Demo/Main.php": "engine/Acme/Demo/Main.php"}},
		{"mount prefix boundary", "", []string{"engine/Acme/Demo/Main.php", "engine/Acme/DemoExtra/Other.php"}, []string{"Main.php", "engine/Acme/DemoExtra/Other.php"}, map[string]string{"engine/Acme/DemoExtra/Other.php": "engine/Acme/DemoExtra/Other.php"}},
		{"quoted alias", "", []string{"@special.php", "true"}, []string{"@special.php", "true"}, map[string]string{"@special.php": "@special.php", "true": "true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "Demo.xml")
			content := `<plugin><name>Demo</name><description>Demo</description>`
			wantTree := make(map[string]string)
			for i, target := range tc.targets {
				content += `<file name="` + target + `"><operation action="create"><replacecode>` + target + `</replacecode></operation></file>`
				wantTree[filepath.FromSlash(tc.paths[i])] = target
			}
			content += `</plugin>`
			if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			src := filepath.Join(dir, "src")
			output, err := Convert(input, src, tc.namespace)
			if err != nil {
				t.Fatal(err)
			}
			info, err := manifest.Parse([]byte(output))
			if err != nil {
				t.Fatalf("parse output: %v\n%s", err, output)
			}
			if got := readTree(t, src); !reflect.DeepEqual(got, wantTree) {
				t.Errorf("sources = %v, want %v", got, wantTree)
			}
			if info.Alias.Len() != len(tc.aliases) {
				t.Fatalf("alias count = %d, want %d", info.Alias.Len(), len(tc.aliases))
			}
			for key, want := range tc.aliases {
				if got, ok := info.Alias.Get(key); !ok || got != want {
					t.Errorf("alias[%q] = %q, %v; want %q", key, got, ok, want)
				}
			}
			if len(tc.aliases) == 0 && strings.Contains(output, "\nalias:") {
				t.Fatal("empty alias section emitted")
			}
		})
	}
}

func TestSourcePathCollision(t *testing.T) {
	for _, targets := range [][]string{
		{"engine/Acme/Demo/Main.php", "Main.php"},
		{"Main.php", "engine/Acme/Demo/Main.php"},
	} {
		dir := t.TempDir()
		input := filepath.Join(dir, "Demo.xml")
		content := `<plugin><name>Demo</name><description>Demo</description>`
		for _, target := range targets {
			content += `<file name="` + target + `"><operation action="create"><replacecode>code</replacecode></operation></file>`
		}
		content += `</plugin>`
		if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		src := filepath.Join(dir, "src")
		_, err := Convert(input, src, "")
		if err == nil || !strings.Contains(err.Error(), "source path collision") || !strings.Contains(err.Error(), "engine/Acme/Demo/Main.php") || !strings.Contains(err.Error(), "src/Main.php") {
			t.Fatalf("collision error = %v", err)
		}
		if _, err := os.Lstat(src); !os.IsNotExist(err) {
			t.Fatalf("source directory published after collision: %v", err)
		}
		if _, err := Convert(input, "", ""); err != nil {
			t.Fatalf("YAML-only conversion: %v", err)
		}
	}
}
