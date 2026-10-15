package diff

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/build"
	"github.com/thecoooler/dleplugin/internal/convert"
	"github.com/thecoooler/dleplugin/internal/plugin"
)

func write(t *testing.T, dir, name, desc string) string {
	p := filepath.Join(dir, name)
	x := `<p><name>N</name><description>` + desc + `</description><mysqlinstall>q</mysqlinstall></p>`
	if err := os.WriteFile(p, []byte(x), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestForDiff(t *testing.T) {
	got, err := ForDiff(write(t, t.TempDir(), "a.xml", "d"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "slug:") || strings.Contains(got, "mount:") {
		t.Fatalf("not stripped: %q", got)
	}
	if !strings.HasPrefix(got, "name: 'N'\ndescription: d\nfiles:") {
		t.Fatalf("%q", got)
	}
}

func projectFixture(t *testing.T, fixture string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "..", "testdata", "golden", "plugin", fixture, "input"))); err != nil {
		t.Fatal(err)
	}
	return dir
}

func projectXML(t *testing.T, dir string) string {
	t.Helper()
	p, err := plugin.FromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files, err := p.Files()
	if err != nil {
		t.Fatal(err)
	}
	x, err := build.XML(p.Info(), files, "test-version")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), p.Slug()+".xml")
	if err := os.WriteFile(path, []byte(x), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func archiveXML(t *testing.T, xmlPath string) string {
	t.Helper()
	data, err := os.ReadFile(xmlPath)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(filepath.Base(xmlPath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := strings.TrimSuffix(xmlPath, ".xml") + ".zip"
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestForDiffRunYAML(t *testing.T) {
	if _, err := exec.LookPath("diff"); err != nil {
		t.Skip("no diff binary")
	}
	d := t.TempDir()
	a, b, c := write(t, d, "a.xml", "d"), write(t, d, "b.xml", "d"), write(t, d, "c.xml", "e")
	project := projectFixture(t, "complete")
	other := projectFixture(t, "complete")
	xml := projectXML(t, project)
	changed := projectFixture(t, "complete")
	manifest := filepath.Join(changed, plugin.Marker)
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, bytes.Replace(data, []byte("description: Example DLE plugin"), []byte("description: Changed"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := projectFixture(t, "complete")
	if err := os.WriteFile(filepath.Join(patch, "patches", "extra.yml"), []byte("engine/test.php:\n  - create: changed patch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sourceChanged := projectFixture(t, "complete")
	if err := os.WriteFile(filepath.Join(sourceChanged, "src", "admin.php"), []byte("<?php // changed source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	split := projectFixture(t, "complete")
	splitManifest := filepath.Join(split, plugin.Marker)
	splitData, err := os.ReadFile(splitManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(splitManifest, bytes.Replace(splitData, []byte("split: false"), []byte("split: true"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	splitXML := projectXML(t, split)
	tabPatch := projectFixture(t, "complete")
	if err := os.WriteFile(filepath.Join(tabPatch, "patches", "extra.yml"), []byte("engine/test.php:\n  - before:\n      search: \"\\tfoo\"\n      replace: \"\\tbar\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tabXML := projectXML(t, tabPatch)
	if err := os.WriteFile(filepath.Join(split, "src", "admin.php"), []byte("<?php // excluded in split XML\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, left, right string
		code              int
	}{
		{"XML identical", a, b, 0},
		{"XML description", a, c, 1},
		{"same source", project, project, 0},
		{"projects identical", project, other, 0},
		{"projects description", project, changed, 1},
		{"projects patch", project, patch, 1},
		{"projects source file", project, sourceChanged, 1},
		{"project built XML", project, xml, 0},
		{"tab-indented patch built XML", tabPatch, tabXML, 0},
		{"built XML project", xml, project, 0},
		{"XML project changed", xml, changed, 1},
		{"project XML changed", changed, xml, 1},
		{"project subdirectory", filepath.Join(project, "src", "Admin"), xml, 0},
		{"project zip", project, archiveXML(t, xml), 0},
		{"zip XML", archiveXML(t, xml), xml, 0},
		{"split project XML", split, splitXML, 0},
		{"split zip project", archiveXML(t, splitXML), split, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			left, err := ForDiff(tt.left)
			if err != nil {
				t.Fatal(err)
			}
			right, err := ForDiff(tt.right)
			if err != nil {
				t.Fatal(err)
			}
			code, err := RunYAML(context.Background(), left, right, &out, &errb)
			if err != nil || code != tt.code {
				t.Fatalf("exit %d, error %v, stderr %s", code, err, &errb)
			}
			if (out.Len() > 0) != (tt.code == 1) || errb.Len() != 0 {
				t.Fatalf("stdout %q, stderr %q", &out, &errb)
			}
		})
	}
}

func TestForDiffErrors(t *testing.T) {
	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid.xml")
	if err := os.WriteFile(invalid, []byte("<broken>"), 0o644); err != nil {
		t.Fatal(err)
	}
	missingEntry := filepath.Join(dir, "other.zip")
	archive := archiveXML(t, write(t, dir, "valid.xml", "d"))
	if err := os.Rename(archive, missingEntry); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, path, want string }{
		{"missing", filepath.Join(dir, "missing"), "source not found"},
		{"non-project", dir, "dleplugin.yml was not found"},
		{"invalid XML", invalid, "parse xml"},
		{"archive missing XML", missingEntry, "archive has no other.xml entry"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ForDiff(tt.path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestConvertGoldenRoundTrip(t *testing.T) {
	inputs, err := filepath.Glob(filepath.Join("..", "..", "testdata", "golden", "convert", "*", "*.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) == 0 {
		t.Fatal("no convert XML fixtures found")
	}
	for _, input := range inputs {
		t.Run(filepath.Base(input), func(t *testing.T) {
			dir := t.TempDir()
			data, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			original := filepath.Join(dir, filepath.Base(input))
			if err := os.WriteFile(original, data, 0o644); err != nil {
				t.Fatal(err)
			}
			project := filepath.Join(dir, "project")
			if err := os.Mkdir(project, 0o755); err != nil {
				t.Fatal(err)
			}
			yml, err := convert.Convert(original, filepath.Join(project, "src"), "")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, "dleplugin.yml"), []byte(yml), 0o644); err != nil {
				t.Fatal(err)
			}
			p, err := plugin.Load(project)
			if err != nil {
				t.Fatal(err)
			}
			files, err := p.Files()
			if err != nil {
				t.Fatal(err)
			}
			x, err := build.XML(p.Info(), files, "")
			if err != nil {
				t.Fatal(err)
			}
			rebuilt := filepath.Join(dir, "rebuilt.xml")
			if err := os.WriteFile(rebuilt, []byte(x), 0o644); err != nil {
				t.Fatal(err)
			}
			want, err := ForDiff(original)
			if err != nil {
				t.Fatal(err)
			}
			for _, source := range []string{project, rebuilt} {
				got, err := ForDiff(source)
				if err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Errorf("round trip through %s differs:\ngot:\n%s\nwant:\n%s", source, got, want)
				}
			}
		})
	}
}
