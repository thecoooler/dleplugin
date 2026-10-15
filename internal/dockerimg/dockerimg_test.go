package dockerimg

import (
	"archive/tar"
	"bytes"
	"io"
	"reflect"
	"testing"
)

func TestBuildContextContainsEveryInput(t *testing.T) {
	image := Image{Name: "tools", Dockerfile: []byte("FROM scratch\n"), Files: map[string][]byte{"composer.json": []byte("{}"), "composer.lock": []byte("locked")}}
	context, err := image.buildContext()
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	reader := tar.NewReader(bytes.NewReader(context))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		entries[header.Name] = string(data)
	}
	want := map[string]string{"Dockerfile": "FROM scratch\n", "composer.json": "{}", "composer.lock": "locked"}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("build context = %v", entries)
	}
	reordered := image
	reordered.Files = map[string][]byte{"composer.lock": []byte("locked"), "composer.json": []byte("{}")}
	other, err := reordered.buildContext()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(context, other) || image.Tag() != reordered.Tag() {
		t.Fatal("map iteration changed image identity")
	}
	reordered.Files["composer.lock"] = []byte("updated")
	if image.Tag() == reordered.Tag() {
		t.Fatal("dependency update did not invalidate cached image")
	}
}
