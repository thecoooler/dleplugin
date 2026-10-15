package plugin

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/goldentest"
	"github.com/thecoooler/dleplugin/internal/manifest"
)

const golden = "../../testdata/golden"

func readJSON(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestGoldenPlugins(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join(golden, "plugin", "*"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no cases: %v", err)
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			input, err := filepath.Abs(filepath.Join(dir, "input"))
			if err != nil {
				t.Fatal(err)
			}
			p, err := Load(input)
			if err != nil {
				t.Fatal(err)
			}
			expectedJSON, err := json.Marshal(p.Info())
			if err != nil {
				t.Fatal(err)
			}
			goldentest.UpdateJSON(t, filepath.Join(dir, "info.json"), expectedJSON)
			for i := 0; i < 2; i++ { // idempotence
				raw, err := json.Marshal(p.Info())
				if err != nil {
					t.Fatal(err)
				}
				var got any
				if err := json.Unmarshal(raw, &got); err != nil {
					t.Fatal(err)
				}
				want := readJSON(t, filepath.Join(dir, "info.json"))
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("info mismatch (call %d)\n got: %s\nwant: %v", i, raw, want)
				}
			}

			files, err := p.Files()
			if err != nil {
				t.Fatal(err)
			}
			out := []map[string]string{}
			for _, f := range files {
				rel, err := filepath.Rel(input, f.Src)
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, map[string]string{"src": filepath.ToSlash(rel), "mount": f.Mount})
			}
			raw, _ := json.Marshal(out)
			goldentest.UpdateJSON(t, filepath.Join(dir, "files.json"), raw)
			var got any
			_ = json.Unmarshal(raw, &got)
			want := readJSON(t, filepath.Join(dir, "files.json"))
			if got == nil {
				got = []any{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("files mismatch\n got: %s\nwant: %v", raw, want)
			}
		})
	}
}

// TestGoldenErrors compares plugin loading errors; the plugin directory is
// written as <dir> in the fixture.
func TestGoldenErrors(t *testing.T) {
	path := filepath.Join(golden, "plugin_errors.json")
	type errCase struct {
		Manifest string `json:"manifest"`
		Error    string `json:"error"`
	}
	var cases map[string]errCase
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	got := map[string]errCase{}
	for name, c := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, Marker), []byte(c.Manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Load(dir)
		if err == nil {
			t.Fatalf("%s: expected error", name)
		}
		got[name] = errCase{c.Manifest, strings.ReplaceAll(err.Error(), dir, "<dir>")}
	}
	var raw bytes.Buffer
	enc := json.NewEncoder(&raw)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(got); err != nil {
		t.Fatal(err)
	}
	goldentest.UpdateJSON(t, path, bytes.TrimSpace(raw.Bytes()))
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for name, c := range cases {
		if got[name].Error != c.Error {
			t.Errorf("%s: error\n got: %q\nwant: %q", name, got[name].Error, c.Error)
		}
	}
}

func TestInitFixtures(t *testing.T) {
	for _, c := range []struct {
		file, slug, name, desc, ns, mount string
	}{
		{"dleplugin.yml", "Demo", "Demo Plugin", "Демо описание: 'x'", `Acme\Demo`, "engine/Acme/Demo"},
		{"plain.yml", "Plain", "Plain", "", "", "engine/Plain"},
	} {
		dir := t.TempDir()
		p, err := Init(dir, c.slug, c.name, c.desc, c.ns)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(filepath.Join(dir, Marker))
		goldentest.Update(t, filepath.Join(golden, "init", c.file), got)
		want, err := os.ReadFile(filepath.Join(golden, "init", c.file))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s mismatch:\n%s\nwant:\n%s", c.file, got, want)
		}
		wantNS := c.ns
		if wantNS == "" {
			wantNS = c.slug
		}
		if p.Description() != c.desc || p.Mount() != c.mount || p.Namespace() != wantNS {
			t.Errorf("loaded plugin: %q %q %q", p.Description(), p.Mount(), p.Namespace())
		}
		if _, err := Init(dir, c.slug, c.name, c.desc, c.ns); err == nil || err.Error() != "dleplugin.yml already exists" {
			t.Errorf("second Init error = %v", err)
		}
	}
}

func TestInitCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "Foo")
	p, err := Init(dir, "Foo", "Foo", "d", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Root() != dir {
		t.Errorf("root %q", p.Root())
	}
	for _, ns := range []string{`\Acme\Foo`, `Acme\`, `Acme\1x`, "my-plugin"} {
		if _, err := Init(t.TempDir(), "Foo", "Foo", "", ns); err == nil || !strings.Contains(err.Error(), "invalid PHP namespace") {
			t.Errorf("%q: error = %v", ns, err)
		}
	}
	if _, err := Init(t.TempDir(), "my-plugin", "X", "", ""); err == nil {
		t.Error("slug that is not a namespace accepted without --namespace")
	}
}

func TestYAMLScalar(t *testing.T) {
	cases := map[string]string{
		"":           "''",
		"abc":        "abc",
		"Юни":        "Юни",
		"a: b":       "'a: b'",
		"it's":       "it's",
		"'x":         "'''x'",
		" lead":      "' lead'",
		"trail ":     "'trail '",
		"a #b":       "'a #b'",
		"Yes":        "'Yes'",
		"null":       "'null'",
		"123":        "'123'",
		"1.5":        "'1.5'",
		"-x":         "'-x'",
		"a\nb":       `"a\nb"`,
		"x:":         "'x:'",
		"2020-01-02": "'2020-01-02'",
		"plain text": "plain text",
	}
	for in, want := range cases {
		if got := manifest.YAMLScalar(in); got != want {
			t.Errorf("YAMLScalar(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestFindRootDoesNotParse(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, Marker), []byte("::: not [valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindRoot(nested)
	if err != nil || got != root {
		t.Fatalf("FindRoot = %q, %v", got, err)
	}
	if _, err := FromDir(nested); err == nil {
		t.Error("FromDir should fail on invalid manifest")
	}
	if _, err := FindRoot(t.TempDir()); err == nil || err.Error() != "Configuration file dleplugin.yml was not found" {
		t.Errorf("error = %v", err)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil || !strings.HasPrefix(err.Error(), "dleplugin.yml was not found: ") {
		t.Errorf("error = %v", err)
	}
}

func TestLoadCRLF(t *testing.T) {
	dir := t.TempDir()
	body := "slug: X\r\nname: X\r\nmysql:\r\n  install: |\r\n    a\r\n    b\r\n"
	if err := os.WriteFile(filepath.Join(dir, Marker), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Info().MySQL.Install; got != "a\nb" {
		t.Errorf("install %q", got)
	}
}

func TestNormalizeFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(root), "elsewhere.php")
	got := NormalizeFiles(
		[]string{"FeatureTest.php", filepath.Join(root, "src/a.php"), ".", "../x.php", outside},
		root, filepath.Join(root, "tests"))
	want := []string{"tests/FeatureTest.php", "src/a.php", "tests", "x.php", outside}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	got = NormalizeFiles([]string{root, "../.."}, root, filepath.Join(root, "tests"))
	if got[0] != "." || got[1] != "../.." {
		t.Errorf("got %v", got)
	}
}

func TestFilesSymlinkAndMissingSrc(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, Marker), []byte("slug: S\nname: S\nmount: m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	files, err := p.Files()
	if err != nil || len(files) != 0 {
		t.Fatalf("%v %v", files, err)
	}
	src := filepath.Join(dir, "src")
	_ = os.MkdirAll(filepath.Join(src, "d"), 0o755)
	_ = os.WriteFile(filepath.Join(src, "d", "f.txt"), []byte("x"), 0o644)
	_ = os.Symlink(filepath.Join(src, "d", "f.txt"), filepath.Join(src, "link.txt"))
	_ = os.Symlink(filepath.Join(src, "d"), filepath.Join(src, "dirlink"))
	files, err = p.Files()
	if err != nil {
		t.Fatal(err)
	}
	var mounts []string
	for _, f := range files {
		mounts = append(mounts, f.Mount)
	}
	if !reflect.DeepEqual(mounts, []string{"m/d/f.txt", "m/link.txt"}) {
		t.Errorf("mounts %v", mounts)
	}
	var _ manifest.SourceFile
}

func TestAdminSectionEscapesSQLMetadata(t *testing.T) {
	dir := t.TempDir()
	content := "slug: Demo\nname: Bob's tools\ndescription: 'C:\\tools {prefix}'\nextras:\n  admin_section: true\n"
	if err := os.WriteFile(filepath.Join(dir, Marker), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	sql := p.Info().MySQL.Enable
	if !strings.Contains(sql, "CONVERT(X'426f62277320746f6f6c73' USING utf8mb4)") {
		t.Fatalf("name is not safely encoded: %s", sql)
	}
	if strings.Contains(sql, `C:\tools`) || strings.Contains(sql, "'Bob's tools'") {
		t.Fatalf("raw metadata leaked into SQL: %s", sql)
	}
	if p.Info().MySQL.Disable != "DELETE FROM `{prefix}_admin_sections` WHERE `name` = 'demo';" {
		t.Fatal("ordinary delete changed")
	}
}
