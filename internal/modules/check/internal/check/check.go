// Package check verifies that the file patches of a plugin apply to DLE
// installations: every searched code must be found in its target file.
package check

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/thecoooler/dleplugin/internal/dlexml"
	"github.com/thecoooler/dleplugin/internal/manifest"
)

// Operation is one patch of a DLE file.
type Operation struct {
	File, Action, Search string
}

// FromInfo returns the file operations of a manifest (with generated extras
// applied, see plugin.Plugin.Info) in manifest order.
func FromInfo(info *manifest.Info) []Operation {
	var ops []Operation
	for _, name := range info.Files.Keys() {
		patch, _ := info.Files.Get(name)
		if patch == nil {
			continue
		}
		for _, a := range patch.Actions {
			ops = append(ops, Operation{File: name, Action: a.Name, Search: a.Search})
		}
	}
	return ops
}

// FromXML returns the file operations of a plugin XML in document order.
func FromXML(root *dlexml.Element) []Operation {
	var ops []Operation
	for _, f := range root.Children {
		if f.Tag != "file" {
			continue
		}
		for _, op := range f.Children {
			if op.Tag != "operation" {
				continue
			}
			o := Operation{File: f.Attrs["name"], Action: op.Attrs["action"]}
			if s := op.Find("searchcode"); s != nil {
				o.Search = s.Text
			}
			ops = append(ops, o)
		}
	}
	return ops
}

// initFile is the DLE file defining VERSIONID, relative to the DLE root.
const initFile = "engine/inc/include/init.php"

var versionRe = regexp.MustCompile(`define\s*\(\s*['"]VERSIONID['"]\s*,\s*['"](.*?)['"]\s*\)`)

// Dist is a DLE installation or unpacked distribution.
type Dist struct {
	Dir string
	// Version is VERSIONID from init.php ("" when it is not defined).
	Version string
}

// OpenDist checks that dir is a DLE root (it has engine/inc/include/init.php)
// and reads its version.
func OpenDist(dir string) (Dist, error) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(initFile)))
	if errors.Is(err, fs.ErrNotExist) {
		return Dist{}, fmt.Errorf("%s is not a DLE directory: %s not found", dir, initFile)
	}
	if err != nil {
		return Dist{}, fmt.Errorf("read DLE version: %w", err)
	}
	d := Dist{Dir: dir}
	if m := versionRe.FindSubmatch(data); m != nil {
		d.Version = string(m[1])
	}
	return d, nil
}

// Kind classifies a Problem.
type Kind string

const (
	// FileNotFound: the patched file does not exist in the DLE directory.
	FileNotFound Kind = "FILE_NOT_FOUND"
	// TextNotFound: the searched code is not in the file.
	TextNotFound Kind = "TEXT_NOT_FOUND"
	// EmptySearch: the operation has no searched code.
	EmptySearch Kind = "EMPTY_SEARCH"
	// Duplicate: the searched code occurs more than once (a warning).
	Duplicate Kind = "DUPLICATE"
)

// Problem is one finding of Run.
type Problem struct {
	Kind         Kind
	File, Search string
	// Count is the number of matches (Duplicate only).
	Count int
}

// Warning reports whether the problem does not fail the check.
func (p Problem) Warning() bool { return p.Kind == Duplicate }

// Report is the result of checking one Dist.
type Report struct {
	Dist     Dist
	Problems []Problem
}

// Failed reports whether some problem is an error.
func (r Report) Failed() bool {
	for _, p := range r.Problems {
		if !p.Warning() {
			return true
		}
	}
	return false
}

// Run checks ops against dist. Only before/after/replace operations search
// code; a file created by the plugin (a create operation) is not checked
// after it is created. A missing file is reported once.
func Run(dist Dist, ops []Operation) (Report, error) {
	r := Report{Dist: dist}
	created := map[string]bool{}
	missing := map[string]bool{}
	contents := map[string][]byte{}
	for _, op := range ops {
		if op.File == "" || created[op.File] || missing[op.File] {
			continue
		}
		switch op.Action {
		case "create":
			created[op.File] = true
			continue
		case "before", "after", "replace":
		default:
			continue
		}
		data, ok := contents[op.File]
		if !ok {
			var err error
			data, err = os.ReadFile(filepath.Join(dist.Dir, filepath.FromSlash(op.File)))
			if errors.Is(err, fs.ErrNotExist) {
				missing[op.File] = true
				r.Problems = append(r.Problems, Problem{Kind: FileNotFound, File: op.File})
				continue
			}
			if err != nil {
				return Report{}, fmt.Errorf("read %s: %w", op.File, err)
			}
			contents[op.File] = data
		}
		if strings.TrimSpace(op.Search) == "" {
			r.Problems = append(r.Problems, Problem{Kind: EmptySearch, File: op.File})
			continue
		}
		switch n := len(SearchPattern(op.Search).FindAllIndex(data, -1)); {
		case n == 0:
			r.Problems = append(r.Problems, Problem{Kind: TextNotFound, File: op.File, Search: op.Search})
		case n > 1:
			r.Problems = append(r.Problems, Problem{Kind: Duplicate, File: op.File, Search: op.Search, Count: n})
		}
	}
	return r, nil
}

var spaceRe = regexp.MustCompile(`\s+`)

// SearchPattern compiles code into a case-insensitive pattern in which any
// whitespace (including none) may separate the words of code.
func SearchPattern(code string) *regexp.Regexp {
	words := spaceRe.Split(strings.TrimSpace(code), -1)
	for i, w := range words {
		words[i] = regexp.QuoteMeta(w)
	}
	return regexp.MustCompile(`(?is)` + strings.Join(words, `\s*`))
}
