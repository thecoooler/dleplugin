package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

func FuzzYAMLScalar(f *testing.F) {
	for _, s := range []string{"", "plain", " leading", "trailing ", "\t", "\n", "text😀\n", "\r", "\x00\x01\x1f\x7f", "\"\\\n", "#", ":", "a: b", "a # b", "'quoted'", "\u2028", "\u2029", "\U000e0001", "😀", "yes", "no", "null", "~", "123", "0x0", "0X0", "0B1", "0O7", "1e1_0", "0_x1", "+_1", ".inf", "-foo", "?foo", "!foo", "&foo", "*foo", "%foo", "@foo", "`foo"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			t.Skip()
		}
		var got map[string]any
		if err := yaml.Unmarshal([]byte("k: "+YAMLScalar(s)+"\n"), &got); err != nil {
			t.Fatalf("scalar %q: %v", s, err)
		}
		if got["k"] != s {
			t.Fatalf("round trip %q: got %#v", s, got["k"])
		}
	})
}

func FuzzNormalizePath(f *testing.F) {
	for _, s := range []string{"", ".", "a/./b//", "//a", "///a", "..", "a/../b", "/a", `a\b`, "C:a", "a\x00b", "a/b"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		normalized := NormalizePath(s)
		if got := NormalizePath(normalized); got != normalized {
			t.Fatalf("not idempotent: %q => %q => %q", s, normalized, got)
		}
		for _, candidate := range []string{s, normalized} {
			if ValidateFilePath(candidate) != nil {
				continue
			}
			if candidate == "" || strings.HasPrefix(candidate, "/") || strings.ContainsAny(candidate, "\\:\x00") {
				t.Fatalf("accepted unsafe path %q", candidate)
			}
			for _, segment := range strings.Split(candidate, "/") {
				if segment == ".." {
					t.Fatalf("accepted traversal %q", candidate)
				}
			}
		}
	})
}

func FuzzParse(f *testing.F) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "golden", "plugin", "*", "input", "dleplugin.yml"))
	if err != nil {
		f.Fatal(err)
	}
	if len(paths) == 0 {
		f.Fatal("no manifest seeds")
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	for _, s := range []string{"", "[", "null", "slug: demo\nname: Demo", "a: &a [*a]", "slug: &s demo\nname: *s"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = Parse(data) })
}

func TestAutoloaderTruthy(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  bool
	}{
		{"true", true}, {"false", false}, {"0", false}, {"0x0", false}, {"1", true}, {"-2", true}, {"9223372036854775808", true}, {"-9223372036854775808", true},
		{"0.0", false}, {"-0.0", false}, {"1.25", true}, {".inf", true}, {".nan", true},
		{`"no"`, true}, {`"false"`, true}, {`""`, false}, {"no", false}, {"off", false}, {"n", false}, {"yes", true}, {"on", true}, {"y", true}, {"word", true},
		{"null", false}, {"~", false}, {"[]", false}, {"{}", false}, {"[false]", true}, {"{a: false}", true},
	} {
		t.Run(tt.value, func(t *testing.T) {
			// Wrap the value in autoloader's positional list so collections themselves
			// reach truthy, rather than being interpreted as the positional list.
			info, err := Parse([]byte("slug: demo\nname: Demo\nextras:\n  autoloader: [" + tt.value + ", " + tt.value + "]\n"))
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Extras.Autoloader; got != [2]bool{tt.want, tt.want} {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
