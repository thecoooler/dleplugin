package static

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/thecoooler/dleplugin/internal/config"
	"github.com/thecoooler/dleplugin/internal/process"
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

func intp(i int) *int { return &i }

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name, yaml string
		want       Config
		err        string
	}{
		{name: "missing file", yaml: "-", want: Config{}},
		{name: "section", yaml: "static:\n  dle_composer_dir: /c\n  dependencies:\n    ExampleDependency: /d\n  dependency_roots: [/r]\ndependency_roots: [/top]\n",
			want: Config{DLEComposerDir: "/c", Dependencies: map[string]string{"ExampleDependency": "/d"}, DependencyRoots: []string{"/r"}}},
		{name: "top-level roots", yaml: "static:\n  dle_composer_dir: /c\ndependency_roots: [/top]\n",
			want: Config{DLEComposerDir: "/c", DependencyRoots: []string{"/top"}}},
		{name: "no static section", yaml: "host_dir: /h\n", want: Config{}},
		{name: "not mapping", yaml: "static: invalid\n", err: "static must be a mapping"},
		{name: "bad roots", yaml: "dependency_roots: x\n", err: "dependency_roots must be a list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, tc.name, "config.yml")
			if tc.yaml != "-" {
				write(t, p, tc.yaml)
			}
			c, err := LoadConfig(config.NewStore(p))
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(*c, tc.want) {
				t.Errorf("got %+v, want %+v", *c, tc.want)
			}
		})
	}
}

func TestLoadPluginConf(t *testing.T) {
	dir := t.TempDir()
	c, err := LoadPluginConf(dir)
	if err != nil || !reflect.DeepEqual(*c, PluginConf{}) {
		t.Fatalf("missing: %+v %v", c, err)
	}
	write(t, filepath.Join(dir, ConfFile), "dependencies: [A]\nexcludePaths:\n  analyse: [src/x]\nstrict: true\nignoreErrors: [foo]\nextra: 1\n")
	c, err = LoadPluginConf(dir)
	want := PluginConf{Dependencies: []string{"A"}, ExcludePaths: &ExcludePaths{Analyse: []string{"src/x"}}, Strict: true, IgnoreErrors: []string{"foo"}}
	if err != nil || !reflect.DeepEqual(*c, want) {
		t.Fatalf("got %+v %v", c, err)
	}
	write(t, filepath.Join(dir, ConfFile), "")
	if c, err = LoadPluginConf(dir); err != nil || !reflect.DeepEqual(*c, PluginConf{}) {
		t.Fatalf("empty: %+v %v", c, err)
	}
	for _, bad := range []string{"excludePaths: {}\n", "strict: [1]\n"} {
		write(t, filepath.Join(dir, ConfFile), bad)
		if _, err := LoadPluginConf(dir); err == nil || !strings.Contains(err.Error(), "dlestatic.yml validation error") {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}

func TestReadSlug(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "dleplugin.yml"), "slug: TestPlugin\nname: Test Plugin\n")
	if s, err := ReadSlug(dir); err != nil || s != "TestPlugin" {
		t.Fatalf("%q %v", s, err)
	}
	write(t, filepath.Join(dir, "dleplugin.yml"), "name: x\n")
	if _, err := ReadSlug(dir); err == nil {
		t.Fatal("no error")
	}
}

func TestIncludePaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "roots", "B"), 0o755); err != nil {
		t.Fatal(err)
	}
	conf := &PluginConf{Dependencies: []string{"A", "B", "C"}}
	paths, warnings := IncludePaths(conf, "/composer/", map[string]string{"A": "/a"}, []string{filepath.Join(dir, "missing"), filepath.Join(dir, "roots")})
	want := []string{"/composer", "/a/src", filepath.Join(dir, "roots", "B", "src")}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %v", paths)
	}
	if !reflect.DeepEqual(warnings, []string{"Dependency not found: C"}) {
		t.Errorf("warnings = %v", warnings)
	}
	if paths, _ := IncludePaths(&PluginConf{}, "", nil, nil); paths != nil {
		t.Errorf("empty = %v", paths)
	}
}

func TestFilterIgnored(t *testing.T) {
	msgs := []Message{
		{Message: "Visible error"},
		{Message: "Ignored by config"},
		{Message: "Call to method foo() of internal class Bar from outside its root namespace Pest."},
	}
	got, err := FilterIgnored(msgs, &PluginConf{IgnoreErrors: []string{"by conf"}})
	if err != nil || len(got) != 1 || got[0].Message != "Visible error" {
		t.Fatalf("got %+v %v", got, err)
	}
	if _, err := FilterIgnored(msgs, &PluginConf{IgnoreErrors: []string{"(?<=x)"}}); err == nil {
		t.Fatal("invalid pattern accepted")
	}
}

func TestRuntime(t *testing.T) {
	rt := &Runtime{
		Image:   "dleplugin-static:abc",
		WorkDir: "/cache/work",
		Mounts: []Mount{
			{Host: "/plugin/src", Container: "/work/src", ReadOnly: true},
			{Host: "/dle", Container: "/include/0", ReadOnly: true},
		},
	}
	cmd := rt.Command([]string{"phpstan", "analyze"})
	joined := strings.Join(cmd, " ")
	if !strings.HasPrefix(joined, "docker run --rm --pull never --network none --env HOME=/tmp ") {
		t.Errorf("cmd = %s", joined)
	}
	for _, s := range []string{"--volume /cache/work:/work ", "--volume /plugin/src:/work/src:ro ", "--volume /dle:/include/0:ro "} {
		if !strings.Contains(joined, s) {
			t.Errorf("cmd lacks %q: %s", s, joined)
		}
	}
	if tail := cmd[len(cmd)-5:]; !reflect.DeepEqual(tail, []string{"--workdir", "/work", "dleplugin-static:abc", "phpstan", "analyze"}) {
		t.Errorf("tail = %v", tail)
	}

	for in, want := range map[string]string{
		"/work/src/a.php":    "/plugin/src/a.php",
		"/work/phpstan.neon": "/cache/work/phpstan.neon",
		"/include/0/b.php":   "/dle/b.php",
		"/other/c.php":       "/other/c.php",
		"/work/srcx/d.php":   "/cache/work/srcx/d.php",
	} {
		if got := rt.ToHost(in); got != want {
			t.Errorf("ToHost(%s) = %s, want %s", in, got, want)
		}
	}

	msgs := []Message{{File: "/work/src/index.php"}, {File: "/work/phpstan.neon"}}
	rt.NormalizePaths(msgs, "/plugin")
	if msgs[0].File != "src/index.php" || msgs[1].File != "phpstan.neon" {
		t.Errorf("normalized = %+v", msgs)
	}
}

func TestImageTagIncludesLocks(t *testing.T) {
	changed := image
	changed.Files = map[string][]byte{"composer.json": composerJSON, "composer.lock": append(append([]byte{}, composerLock...), '\n')}
	if changed.Tag() == ImageTag() {
		t.Fatal("lock changes must invalidate image cache")
	}
}

func TestNewWorkspace(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "xdg"))
	plugin := filepath.Join(root, "plugin")
	write(t, filepath.Join(plugin, "src", "index.php"), "<?php\n")
	dle := filepath.Join(root, "dle")
	vendor := filepath.Join(root, "xdg", "dleplugin", "composer", "vendor")
	for _, d := range []string{dle, vendor} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	ws, err := NewWorkspace(plugin, "TestPlugin", "image", []string{dle, "missing"}, root)
	if err != nil {
		t.Fatal(err)
	}
	mounts := map[string]string{}
	for _, m := range ws.Runtime.Mounts {
		if !m.ReadOnly {
			t.Errorf("rw mount %+v", m)
		}
		mounts[m.Container] = m.Host
	}
	work := ws.Runtime.WorkDir
	if !strings.HasPrefix(work, filepath.Join(root, "xdg", "dleplugin", "static", "TestPlugin-")) {
		t.Errorf("work = %s", work)
	}
	want := map[string]string{
		"/opt/tools/bootstrap.php": filepath.Join(root, "xdg", "dleplugin", "static", "bootstrap.php"),
		"/work/src":                filepath.Join(plugin, "src"),
		"/include/0":               dle,
		"/test-vendor":             vendor,
	}
	if !reflect.DeepEqual(mounts, want) {
		t.Errorf("mounts = %v", mounts)
	}
	if !reflect.DeepEqual(ws.IncludePaths, []string{"/include/0"}) {
		t.Errorf("includes = %v", ws.IncludePaths)
	}
	if !reflect.DeepEqual(ws.Warnings, []string{"Include path not found: missing"}) {
		t.Errorf("warnings = %v", ws.Warnings)
	}
	if b, err := os.ReadFile(want["/opt/tools/bootstrap.php"]); err != nil || string(b) != string(bootstrapPHP) {
		t.Errorf("bootstrap not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "src")); err != nil {
		t.Errorf("work src: %v", err)
	}
}

func TestWritePHPStanConf(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("/nonexistent", filepath.Join(dir, "phpstan.neon")); err != nil {
		t.Fatal(err)
	}
	conf := &PluginConf{Strict: true, ExcludePaths: &ExcludePaths{Analyse: []string{"src/vendor"}}}
	if err := writePHPStanConf(dir, conf, []string{"/include/0"}, 80100); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "phpstan.neon"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"parameters": map[string]any{
			"level":           "max",
			"paths":           []any{"src"},
			"tmpDir":          "./.phpstan",
			"bootstrapFiles":  []any{"/opt/tools/bootstrap.php"},
			"scanDirectories": []any{"/include/0"},
			"excludePaths":    map[string]any{"analyse": []any{"src/vendor"}},
			"phpVersion":      80100,
		},
		"includes": []any{
			"phar://phpstan.phar/conf/bleedingEdge.neon",
			"/opt/tools/vendor/phpstan/phpstan-strict-rules/rules.neon",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("conf = %s", b)
	}

	if err := writePHPStanConf(dir, &PluginConf{}, nil, 0); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "phpstan.neon"))
	if !strings.Contains(string(b), "scanDirectories: []") || !strings.Contains(string(b), "excludePaths: []") ||
		strings.Contains(string(b), "phpVersion") {
		t.Errorf("conf = %s", b)
	}
}

func TestParsePHPVersion(t *testing.T) {
	for in, want := range map[string]int{"8.1": 80100, "7.4": 70400, "8.4.12": 80412, "8.0.0": 80000} {
		if got, err := ParsePHPVersion(in); err != nil || got != want {
			t.Errorf("ParsePHPVersion(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "8", "8.", "8.1.2.3", "8.x", "80100", "8.100", "08.1", "8.-1", "5.6", "10.0"} {
		if _, err := ParsePHPVersion(in); err == nil {
			t.Errorf("ParsePHPVersion(%q) succeeded", in)
		}
	}
}

func TestParsePHPStan(t *testing.T) {
	out := `{"totals":{"errors":0,"file_errors":3},"files":{` +
		`"/work/src/z.php":{"errors":1,"messages":[{"message":"Z","line":3,"ignorable":true}]},` +
		`"/work/src/a.php":{"errors":2,"messages":[{"message":"A1","line":5,"tip":"try"},{"message":"A2","line":null}]}},"errors":[]}`
	msgs, err := parsePHPStan(&process.Result{Stdout: []byte(out)}, []string{"phpstan"})
	if err != nil {
		t.Fatal(err)
	}
	tip := "try"
	want := []Message{
		{Provider: "phpstan", File: "/work/src/z.php", Line: intp(3), Message: "Z"},
		{Provider: "phpstan", File: "/work/src/a.php", Line: intp(5), Message: "A1", Tip: &tip},
		{Provider: "phpstan", File: "/work/src/a.php", Message: "A2"},
	}
	if !reflect.DeepEqual(msgs, want) {
		t.Errorf("msgs = %+v", msgs)
	}
	for _, empty := range []string{`{"files":[]}`, `{}`} {
		if msgs, err := parsePHPStan(&process.Result{Stdout: []byte(empty)}, nil); err != nil || msgs != nil {
			t.Errorf("%s: %v %v", empty, msgs, err)
		}
	}
	if _, err := parsePHPStan(&process.Result{Stdout: []byte("PHP Fatal error")}, []string{"phpstan"}); err == nil || !strings.Contains(err.Error(), "Command returned invalid JSON: phpstan") {
		t.Errorf("err = %v", err)
	}
}

func TestGroupMessages(t *testing.T) {
	msgs := []Message{
		{File: "b.php", Line: intp(1), Message: "b1"},
		{File: "a.php", Line: intp(2), Message: "a2"},
		{File: "b.php", Line: intp(9), Message: "b9"},
		{File: "b.php", Line: intp(1), Message: "b1 again"},
		{File: "b.php", Message: "no line"},
	}
	files := GroupMessages(msgs)
	if len(files) != 2 || files[0][0].File != "b.php" || len(files[0]) != 3 || len(files[0][0].Messages) != 2 {
		t.Fatalf("groups = %+v", files)
	}
	var order []string
	for _, g := range SortLinesDesc(files[0]) {
		order = append(order, g.Messages[0].Message)
	}
	if !reflect.DeepEqual(order, []string{"b9", "b1", "no line"}) {
		t.Errorf("order = %v", order)
	}
}
