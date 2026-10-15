package dlexml

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `<?xml version="1.0"?><p><name>N</name><empty/><file name="b"><op action="x"><searchcode>s</searchcode></op></file><file name="a"><op action="y"><replacecode><![CDATA[r]]></replacecode></op><op action="a"/></file></p>`

func TestParseAndOps(t *testing.T) {
	root, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if n := root.Find("name"); n == nil || n.Text != "N" || !n.HasText {
		t.Fatalf("name: %+v", n)
	}
	if e := root.Find("empty"); e == nil || e.HasText {
		t.Fatalf("empty: %+v", e)
	}
	ops := LoadFileOperations(root)
	want := []FileOperation{{"a", "a", "", ""}, {"a", "y", "", "r"}, {"b", "x", "s", ""}}
	if len(ops) != len(want) {
		t.Fatalf("ops %v", ops)
	}
	for i := range want {
		if ops[i] != want[i] {
			t.Errorf("op %d: %v != %v", i, ops[i], want[i])
		}
	}
}

func TestParsePluginFileZip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plug.zip")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("dir/plug.xml")
	_, _ = w.Write([]byte(sample))
	_ = zw.Close()
	_ = f.Close()
	root, err := ParsePluginFile(p)
	if err != nil || root.Tag != "p" {
		t.Fatalf("%v %v", root, err)
	}
	x := filepath.Join(dir, "plain.xml")
	_ = os.WriteFile(x, []byte(sample), 0o644)
	if root, err = ParsePluginFile(x); err != nil || root.Tag != "p" {
		t.Fatalf("%v %v", root, err)
	}
}
