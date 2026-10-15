// Package dlexml reads DataLife Engine plugin XML files into a minimal
// ordered element tree and extracts file operations from them.
package dlexml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Element is a node of the parsed XML tree.
type Element struct {
	Tag   string
	Attrs map[string]string
	// Text is the character data (including CDATA) located before the first
	// child element, like lxml's .text.
	Text string
	// HasText reports whether Text is non-empty (lxml's .text is not None).
	HasText  bool
	Children []*Element
}

// Find returns the first direct child with the given tag, or nil.
func (e *Element) Find(tag string) *Element {
	for _, c := range e.Children {
		if c.Tag == tag {
			return c
		}
	}
	return nil
}

// Parse reads an XML document and returns its root element.
func Parse(r io.Reader) (*Element, error) {
	dec := xml.NewDecoder(r)
	var stack []*Element
	var root *Element
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			el := &Element{Tag: t.Name.Local, Attrs: make(map[string]string, len(t.Attr))}
			for _, a := range t.Attr {
				el.Attrs[a.Name.Local] = a.Value
			}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.Children = append(p.Children, el)
			} else if root == nil {
				root = el
			}
			stack = append(stack, el)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) == 0 {
				continue
			}
			cur := stack[len(stack)-1]
			if len(cur.Children) == 0 {
				cur.Text += string(t)
				cur.HasText = cur.Text != ""
			}
		}
	}
	if root == nil {
		return nil, errors.New("parse xml: no root element")
	}
	return root, nil
}

// ParseFile parses the XML file at path.
func ParseFile(path string) (*Element, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	root, err := Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return root, nil
}

// ParsePluginFile parses a plugin file that is either a plain XML file or a
// zip archive containing "<archive name without extension>.xml".
func ParsePluginFile(path string) (*Element, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return ParseFile(path)
	}
	defer zr.Close()

	base := filepath.Base(path)
	want := strings.TrimSuffix(base, filepath.Ext(base)) + ".xml"
	for _, f := range zr.File {
		if filepath.Base(f.Name) != want {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s in %s: %w", f.Name, path, err)
		}
		defer rc.Close()
		root, err := Parse(rc)
		if err != nil {
			return nil, fmt.Errorf("%s!%s: %w", path, f.Name, err)
		}
		return root, nil
	}
	return nil, fmt.Errorf("%s: archive has no %s entry", path, want)
}

// FileOperation is one search/replace operation on a plugin file.
type FileOperation struct {
	File, Action, Search, Replace string
}

// LoadFileOperations collects the operations of all <file> children of root,
// sorted (stably) by "file-action-search".
func LoadFileOperations(root *Element) []FileOperation {
	var ops []FileOperation
	for _, f := range root.Children {
		if f.Tag != "file" {
			continue
		}
		name := f.Attrs["name"]
		for _, op := range f.Children {
			o := FileOperation{File: name, Action: op.Attrs["action"]}
			if s := op.Find("searchcode"); s != nil {
				o.Search = s.Text
			}
			if r := op.Find("replacecode"); r != nil {
				o.Replace = r.Text
			}
			ops = append(ops, o)
		}
	}
	sort.SliceStable(ops, func(i, j int) bool {
		return ops[i].File+"-"+ops[i].Action+"-"+ops[i].Search <
			ops[j].File+"-"+ops[j].Action+"-"+ops[j].Search
	})
	return ops
}
