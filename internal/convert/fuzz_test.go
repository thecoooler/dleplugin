package convert

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/thecoooler/dleplugin/internal/dlexml"
	"github.com/thecoooler/dleplugin/internal/manifest"
)

func FuzzBlockScalars(f *testing.F) {
	for _, s := range []string{"", "plain\nsecond", "\tfoo\n\tbar", "  foo\n    bar", "\n\nfoo\n", "foo  \n\n  bar  ", " \n", "\t\n", "a\r\nb", "a\rb", "a\u0085b", "a\u2028b", "a\u2029b", "\n  \nfoo", "\tfoo\n \n\tbar", "😀"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if !utf8.ValidString(text) {
			t.Skip()
		}
		// XML 1.0 cannot carry other control characters or U+FFFE/U+FFFF.
		// EscapeText preserves CR with character references, avoiding XML's
		// mandatory normalization of literal CR/CRLF line endings.
		for _, r := range text {
			if !(r == 9 || r == 10 || r == 13 || r >= 0x20 && r <= 0xd7ff || r >= 0xe000 && r <= 0xfffd || r >= 0x10000 && r <= 0x10ffff) {
				t.Skip()
			}
		}
		var escaped bytes.Buffer
		if err := xml.EscapeText(&escaped, []byte(text)); err != nil {
			t.Fatal(err)
		}
		code := escaped.String()
		content := `<plugin><name>Demo</name><description>Demo</description><file name="engine/test.php">`
		for _, action := range []string{"before", "after", "replace"} {
			content += `<operation action="` + action + `"><searchcode>` + code + `</searchcode><replacecode>` + code + `</replacecode></operation>`
		}
		content += `</file><mysqlinstall>` + code + `</mysqlinstall></plugin>`
		root, err := dlexml.Parse(strings.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		output, err := FromElement(root, "Demo", "", "")
		if err != nil {
			t.Fatalf("convert %q: %v", text, err)
		}
		info, err := manifest.Parse([]byte(output))
		if err != nil {
			t.Fatalf("parse %q: %v\n%s", text, err, output)
		}
		want := strings.Trim(text, "\r\n")
		patch, ok := info.Files.Get("engine/test.php")
		if !ok || len(patch.Actions) != 3 {
			t.Fatalf("missing actions: %+v", patch)
		}
		for _, action := range patch.Actions {
			if action.Search != want || action.Replace != want {
				t.Fatalf("%s round trip %q: search %q replace %q", action.Name, text, action.Search, action.Replace)
			}
		}
		if info.MySQL.Install != want {
			t.Fatalf("hook round trip %q: got %q", text, info.MySQL.Install)
		}
	})
}
