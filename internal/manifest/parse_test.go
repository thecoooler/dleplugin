package manifest

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	cases := map[string]string{
		"":                        "",
		".":                       "",
		"./engine/./Acme//Quirks": "engine/Acme/Quirks",
		"engine/Acme/X/":          "engine/Acme/X",
		"a/../b":                  "a/../b",
		"/a//b":                   "/a/b",
		"//a":                     "//a",
		"///a":                    "/a",
	}
	for in, want := range cases {
		if got := NormalizePath(in); got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
	if JoinMount("", "a/b") != "a/b" || JoinMount("m", "a") != "m/a" {
		t.Error("JoinMount")
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "must contain a mapping at the root"},
		{"- a\n", "must contain a mapping at the root"},
		{"slug: [\n", "dleplugin.yml syntax error"},
		{"name: X\n", "dleplugin.yml validation error: slug: "},
		{"slug: X\nname: X\nmysql:\n  install:\n", "mysql.install"},
		{"slug: X\nname: X\nfiles:\n  a.php:\n    - before:\n        replace: y\n", "search"},
		{"slug: X\nname: X\ndependencies: ExampleDependency\n", "dependencies: Input should be a valid list"},
		{"slug: X\nname: X\ndependencies: [A, ' ']\n", "dependencies.1: Plugin name must not be empty"},
		{"slug: X\nname: X\ndependencies: ['A,B']\n", "dependencies.0: Plugin name must not contain a comma"},
		{"slug: X\nname: X\ndependencies: [~]\n", "dependencies.0"},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.in))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Parse(%q) error = %v, want containing %q", c.in, err, c.want)
		}
	}
	_, err := Parse([]byte("x: 1"))
	if err == nil || !strings.Contains(err.Error(), "slug") || !strings.Contains(err.Error(), "name") || !strings.Contains(err.Error(), "; ") {
		t.Errorf("expected joined issues, got %v", err)
	}
}

func TestParseDependencies(t *testing.T) {
	info, err := Parse([]byte("slug: X\nname: X\ndependencies:\n  - ExampleDependency\n  - ' Other '\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ExampleDependency", "Other"}; !reflect.DeepEqual(info.Dependencies, want) {
		t.Errorf("dependencies = %q", info.Dependencies)
	}
	c := info.Clone()
	c.Dependencies[0] = "changed"
	if info.Dependencies[0] != "ExampleDependency" {
		t.Error("Clone shares dependencies")
	}
	for _, in := range []string{"slug: X\nname: X\n", "slug: X\nname: X\ndependencies:\n"} {
		if info, err := Parse([]byte(in)); err != nil || info.Dependencies != nil {
			t.Errorf("Parse(%q) = %q, %v", in, info.Dependencies, err)
		}
	}
}

func TestParseSemantics(t *testing.T) {
	in := "slug: X\nname: X\nextras:\n  autoloader: [\"false\", 0, true]\n" +
		"files:\n  b.php:\n    - create: \"\\n\\nbody\\n\\n\"\n  a.php:\n    - before: {search: \"  s\", replace: \"r\\r\\n\"}\n"
	info, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if info.Extras.Autoloader != [2]bool{true, false} {
		t.Errorf("autoloader %v", info.Extras.Autoloader)
	}
	if k := info.Files.Keys(); k[0] != "b.php" || k[1] != "a.php" {
		t.Errorf("order %v", k)
	}
	p, _ := info.Files.Get("a.php")
	if p.Actions[0].Search != "  s" || p.Actions[0].Replace != "r" {
		t.Errorf("%+v", p.Actions[0])
	}
	c, _ := info.Files.Get("b.php")
	if c.Actions[0] != (Action{Name: "create", Replace: "body"}) {
		t.Errorf("%+v", c.Actions[0])
	}
	info, err = Parse([]byte("slug: X\nname: X\nextras:\n  autoloader: 1\n"))
	if err != nil || info.Extras.Autoloader != [2]bool{true, true} {
		t.Errorf("autoloader scalar: %v %v", info, err)
	}
}

func TestParseFiles(t *testing.T) {
	files, err := ParseFiles([]byte("slug: ignored\nfiles:\n  a.php:\n    - after: {search: a, replace: b}\n"))
	if err != nil || files.Len() != 1 {
		t.Fatalf("%v %v", files, err)
	}
}

func TestRejectUnsafeManifestPaths(t *testing.T) {
	for _, field := range []string{
		"slug: ../escape\nname: Demo\n", "slug: ''\nname: Demo\n", "slug: .\nname: Demo\n",
		"slug: Demo\nname: Demo\nmount: ../escape\n", "slug: Demo\nname: Demo\nmount: /escape\n",
		"slug: Demo\nname: Demo\nalias: {a.php: ../escape}\n",
		"slug: Demo\nname: Demo\nfiles: {'../escape': [{create: content}]}\n",
	} {
		if _, err := Parse([]byte(field)); err == nil {
			t.Errorf("accepted %q", field)
		}
	}
	if _, err := ParseFiles([]byte("files: {'../escape': [{create: content}]}\n")); err == nil {
		t.Fatal("unsafe extra config accepted")
	}
}

func TestNamespace(t *testing.T) {
	cases := []struct{ name, in, ns, mount, err string }{
		{"from mount", "mount: engine/Acme/Demo/\n", `Acme\Demo`, "engine/Acme/Demo", ""},
		{"from slug", "", "X", "", ""},
		{"mount outside engine", "mount: templates/x\n", "X", "templates/x", ""},
		{"explicit sets mount", "namespace: Acme\\Demo\n", `Acme\Demo`, "engine/Acme/Demo", ""},
		{"explicit keeps mount", "namespace: Acme\\Demo\nmount: modules/demo\n", `Acme\Demo`, "modules/demo", ""},
		{"invalid", "namespace: '\\Acme'\n", "", "", "namespace: invalid PHP namespace"},
		{"autoloader mismatch", "namespace: Acme\\Demo\nmount: engine/Other\nextras: {autoloader: true}\n", "", "", "mount must be engine/Acme/Demo"},
		{"autoloader without mount", "extras: {autoloader: true}\n", "X", "", ""},
		{"autoloader without namespace", "slug: my-x\nmount: x/y\nextras: {autoloader: [false, true]}\n", "", "", "namespace is required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := c.in
			if !strings.Contains(in, "slug:") {
				in = "slug: X\n" + in
			}
			info, err := Parse([]byte(in + "name: N\n"))
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if info.Namespace != c.ns || info.Mount != c.mount {
				t.Errorf("namespace %q mount %q", info.Namespace, info.Mount)
			}
		})
	}
}
