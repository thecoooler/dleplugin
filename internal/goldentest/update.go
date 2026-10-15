// Package goldentest supports explicit fixture updates from the existing tests.
package goldentest

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Update writes a generated expectation only during make golden.
func Update(t testing.TB, path string, got []byte) {
	t.Helper()
	if os.Getenv("DLEPLUGIN_UPDATE_GOLDEN") != "1" {
		return
	}
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, got) {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, got, 0o644); err != nil {
		t.Fatal(err)
	}
}

// UpdateJSON keeps the file unchanged when its values are equal.
func UpdateJSON(t testing.TB, path string, got []byte) {
	t.Helper()
	if os.Getenv("DLEPLUGIN_UPDATE_GOLDEN") != "1" {
		return
	}
	var currentValue, gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(path)
	if err == nil && json.Unmarshal(current, &currentValue) == nil && reflect.DeepEqual(currentValue, gotValue) {
		return
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, got, "", "  "); err != nil {
		t.Fatal(err)
	}
	Update(t, path, append(formatted.Bytes(), '\n'))
}

// UpdateTree replaces only the expected extracted sources, never fixture inputs.
func UpdateTree(t testing.TB, dir string, got map[string]string) {
	t.Helper()
	if os.Getenv("DLEPLUGIN_UPDATE_GOLDEN") != "1" {
		return
	}
	current := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		current[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(current, got) {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	for name, data := range got {
		Update(t, filepath.Join(dir, name), []byte(data))
	}
}
