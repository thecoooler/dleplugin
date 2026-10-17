package build

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/goldentest"
	"github.com/thecoooler/dleplugin/internal/manifest"
)

// orderedKeys returns the keys of a JSON object in document order.
func orderedKeys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

func loadInfo(t *testing.T, path string) *manifest.Info {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Files       json.RawMessage `json:"files"`
		Slug        string          `json:"slug"`
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Build       struct{ Split bool }
		MySQL       manifest.Lifecycle `json:"mysql"`
		PHP         manifest.Lifecycle `json:"php"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	info := manifest.NewInfo()
	info.Slug, info.Name, info.Description = raw.Slug, raw.Name, raw.Description
	info.Build.Split = raw.Build.Split
	info.MySQL, info.PHP = raw.MySQL, raw.PHP
	var byName map[string]*manifest.FilePatch
	if err := json.Unmarshal(raw.Files, &byName); err != nil {
		t.Fatal(err)
	}
	for _, k := range orderedKeys(t, raw.Files) {
		info.Files.Set(k, byName[k])
	}
	return info
}

func TestXMLGolden(t *testing.T) {
	for _, c := range []string{"minimal", "only_required", "complete", "quirks", "quirks_files", "admin_section_only", "unicode"} {
		t.Run(c, func(t *testing.T) {
			dir := filepath.Join("..", "..", "testdata", "golden", "plugin", c)
			info := loadInfo(t, filepath.Join(dir, "info.json"))
			data, err := os.ReadFile(filepath.Join(dir, "files.json"))
			if err != nil {
				t.Fatal(err)
			}
			var list []struct{ Src, Mount string }
			if err := json.Unmarshal(data, &list); err != nil {
				t.Fatal(err)
			}
			var files []manifest.SourceFile
			for _, f := range list {
				files = append(files, manifest.SourceFile{Src: filepath.Join(dir, "input", f.Src), Mount: f.Mount})
			}
			for file, commit := range map[string]string{"build.xml": "abc1234", "build_nocommit.xml": ""} {
				got, err := XML(info, files, commit, "")
				if err != nil {
					t.Fatal(err)
				}
				goldentest.Update(t, filepath.Join(dir, file), []byte(got))
				want, err := os.ReadFile(filepath.Join(dir, file))
				if err != nil {
					t.Fatal(err)
				}
				if got != string(want) {
					t.Errorf("%s mismatch\n got: %.300q\nwant: %.300q", file, got, want)
				}
			}
		})
	}
}

func TestXMLEscapingAndErrors(t *testing.T) {
	info := manifest.NewInfo()
	info.Name = "a&b<c>d\r"
	info.Files.Set("f\"\n&.php", &manifest.FilePatch{Actions: []manifest.Action{{Name: "after", Search: "s", Replace: "r"}}})
	got, err := XML(info, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<name>a&amp;b&lt;c&gt;d&#13;</name>",
		`<file name="f&quot;&#10;&amp;.php">`,
		"<icon></icon>",
		"<mysqlinstall><![CDATA[]]></mysqlinstall>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}

	info = manifest.NewInfo()
	info.Dependencies = []string{"ExampleDependency", "Other"}
	if got, err := XML(info, nil, "", ""); err != nil || !strings.Contains(got, "<needplugin>ExampleDependency,Other</needplugin>") {
		t.Errorf("needplugin: %v %s", err, got)
	}

	bad := manifest.NewInfo()
	bad.MySQL.Install = "x]]>y"
	if _, err := XML(bad, nil, "", ""); err == nil {
		t.Error("expected CDATA error")
	}
	bad = manifest.NewInfo()
	bad.Description = "nul\x00"
	if _, err := XML(bad, nil, "", ""); err == nil || !strings.Contains(err.Error(), "XML compatible") {
		t.Errorf("expected XML compatible error, got %v", err)
	}
	bad = manifest.NewInfo()
	bad.PHP.Delete = "\xff"
	if _, err := XML(bad, nil, "", ""); err == nil {
		t.Error("expected invalid UTF-8 error")
	}
	if _, err := XML(manifest.NewInfo(), []manifest.SourceFile{{Src: "/nonexistent", Mount: "a"}}, "", ""); err == nil || !strings.Contains(err.Error(), "error in plugin file /nonexistent") {
		t.Errorf("unexpected err %v", err)
	}
}

func newProject(t *testing.T, split bool) (string, *manifest.Info, []manifest.SourceFile) {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(src, "a.php"), "<?php\r\necho 1;\r\n")
	write(filepath.Join(src, "sub", "b.php"), "b")
	write(filepath.Join(src, ".htaccess"), "deny")
	info := manifest.NewInfo()
	info.Slug, info.Name = "demo", "Demo"
	info.Build.Split = split
	files := []manifest.SourceFile{
		{Src: filepath.Join(src, ".htaccess"), Mount: "engine/x/.htaccess"},
		{Src: filepath.Join(src, "a.php"), Mount: "engine/x/a.php"},
		{Src: filepath.Join(src, "sub", "b.php"), Mount: "engine/x/sub/b.php"},
	}
	return root, info, files
}

func TestWriteDistNonSplit(t *testing.T) {
	root, info, files := newProject(t, false)
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(root, "dist", "stale.txt")
	os.WriteFile(stale, []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("# Example\n\ntext\n"), 0o644)

	res, err := WriteDist(root, info, files, "abc1234", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("stale file not removed")
	}
	if res.ArchivePath != "" || res.XMLPath != filepath.Join(root, "dist", "demo.xml") || res.ReadmePath == "" {
		t.Errorf("result %+v", res)
	}
	x, _ := os.ReadFile(res.XMLPath)
	if !strings.Contains(string(x), "<version>abc1234</version>") || !strings.Contains(string(x), "echo 1;\n]]>") {
		t.Errorf("xml: %s", x)
	}
	got, _ := os.ReadFile(res.ReadmePath)
	if !strings.Contains(string(got), "<h1>Example</h1>") || !strings.Contains(string(got), "<title>Demo</title>") {
		t.Errorf("readme: %s", got)
	}
}

func TestReadmeWrapper(t *testing.T) {
	gold, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "readme", "README.html"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := renderReadme("Example", []byte("# Example\n"))
	if err != nil {
		t.Fatal(err)
	}
	const open, closeTag = `<div class="container mt-4">`, "</div>\n\n"
	split := func(s string) (string, string) {
		i := strings.Index(s, open)
		j := strings.LastIndex(s, closeTag)
		if i < 0 || j < 0 {
			t.Fatalf("wrapper markers missing in %q", s)
		}
		return s[:i+len(open)] + s[j:], s[i+len(open) : j]
	}
	gw, _ := split(string(gold))
	pw, body := split(got)
	if !strings.Contains(gw, "<title>") {
		t.Fatal("unexpected golden")
	}
	// Golden title comes from its own fixture; normalise it.
	norm := func(s string) string {
		i, j := strings.Index(s, "<title>"), strings.Index(s, "</title>")
		return s[:i] + s[j:]
	}
	if norm(gw) != norm(pw) {
		t.Errorf("wrapper mismatch\n got: %q\nwant: %q", pw, gw)
	}
	if !strings.Contains(body, "<h1>Example</h1>") {
		t.Errorf("body: %q", body)
	}
}

func TestXMLUpgradeURL(t *testing.T) {
	info := manifest.NewInfo()
	info.Name = "Demo"
	got, err := XML(info, nil, "1.0", "https://example.com/plugin.xml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<upgradeurl>https://example.com/plugin.xml</upgradeurl>") {
		t.Errorf("missing upgradeurl in %s", got)
	}
	// An escaped URL stays valid XML.
	got, err = XML(info, nil, "1.0", "https://example.com/u.xml?code=a&b")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<upgradeurl>https://example.com/u.xml?code=a&amp;b</upgradeurl>") {
		t.Errorf("upgradeurl not escaped: %s", got)
	}
	for _, bad := range []string{"http://example.com/plugin.xml", "ftp://example.com/plugin.xml", "https://"} {
		if _, err := XML(info, nil, "1.0", bad); err == nil {
			t.Errorf("accepted upgrade URL %q", bad)
		}
	}
}

func TestWriteDistUpgradeURL(t *testing.T) {
	root, info, files := newProject(t, false)
	res, err := WriteDist(root, info, files, "1.0", "https://example.com/plugin.xml")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(res.XMLPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<upgradeurl>https://example.com/plugin.xml</upgradeurl>") {
		t.Errorf("xml: %s", data)
	}
}

func TestWriteDistSplit(t *testing.T) {
	root, info, files := newProject(t, true)
	res, err := WriteDist(root, info, files, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.XMLPath != "" || res.ReadmePath != "" || res.ArchivePath != filepath.Join(root, "dist", "demo.zip") {
		t.Errorf("result %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "dist", "demo.xml")); err == nil {
		t.Error("xml not removed")
	}
	zr, err := zip.OpenReader(res.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	want := "demo.xml,engine/x/.htaccess,engine/x/a.php,engine/x/sub/b.php"
	if strings.Join(names, ",") != want {
		t.Errorf("entries %v", names)
	}
}

func TestWriteDistRequiresHtaccess(t *testing.T) {
	root, info, files := newProject(t, true)
	os.Remove(filepath.Join(root, "src", ".htaccess"))
	keep := filepath.Join(root, "dist", "keep.txt")
	os.MkdirAll(filepath.Dir(keep), 0o755)
	os.WriteFile(keep, []byte("x"), 0o644)
	_, err := WriteDist(root, info, files, "", "")
	if err == nil || err.Error() != "Not found .htaccess, but it is required when build split mode is true" {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("dist was touched")
	}
}

func TestUnsafeBuildLeavesExistingDist(t *testing.T) {
	for _, tc := range []struct{ slug, mount string }{
		{"../../escape", "safe.php"}, {"demo", "../escape.php"}, {"demo", `C:\escape.php`},
	} {
		t.Run(tc.slug+"|"+tc.mount, func(t *testing.T) {
			root, info, files := newProject(t, true)
			stale := filepath.Join(root, "dist", "keep.txt")
			if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(stale, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			info.Slug = tc.slug
			files[0].Mount = tc.mount
			if _, err := WriteDist(root, info, files, "", ""); err == nil {
				t.Fatal("unsafe build accepted")
			}
			data, err := os.ReadFile(stale)
			if err != nil || string(data) != "keep" {
				t.Fatalf("existing dist changed: %q, %v", data, err)
			}
		})
	}
}
