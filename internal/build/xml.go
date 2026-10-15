// Package build generates DLE plugin XML and writes the dist/ directory.
package build

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/thecoooler/dleplugin/internal/manifest"
)

const xmlHeader = `<?xml version="1.0" encoding="utf-8"?>`

var errNotXMLCompatible = errors.New("All strings must be XML compatible: Unicode or ASCII, no NULL bytes or control characters")

// XML renders the plugin XML for an already expanded info. When
// info.Build.Split is false every source file is embedded as a create
// operation.
func XML(info *manifest.Info, files []manifest.SourceFile, version string) (string, error) {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString("<dleplugin>")

	for _, e := range []struct{ name, text string }{
		{"name", info.Name},
		{"description", info.Description},
		{"icon", ""},
		{"version", version},
		{"upgradeurl", ""},
		{"filedelete", "0"},
		{"needplugin", strings.Join(info.Dependencies, ",")},
		{"mnotice", "0"},
	} {
		if err := writeText(&b, e.name, e.text); err != nil {
			return "", err
		}
	}
	for _, prefix := range []string{"mysql", "php"} {
		for _, hook := range manifest.LifecycleHooks {
			lc := info.MySQL
			if prefix == "php" {
				lc = info.PHP
			}
			if err := writeCDATA(&b, prefix+hook, lc.Get(hook)); err != nil {
				return "", err
			}
		}
	}
	if err := writeCDATA(&b, "notice", ""); err != nil {
		return "", err
	}

	if info.Files != nil {
		for _, name := range info.Files.Keys() {
			patch, _ := info.Files.Get(name)
			if patch == nil {
				continue
			}
			for _, a := range patch.Actions {
				if err := writeFile(&b, name, a.Name, a.Search, a.Replace); err != nil {
					return "", err
				}
			}
		}
	}

	if !info.Build.Split {
		for _, f := range files {
			content, err := ReadSource(f.Src)
			if err == nil {
				err = writeFile(&b, f.Mount, "create", "", content)
			}
			if err != nil {
				return "", fmt.Errorf("error in plugin file %s: %w", f.Src, err)
			}
		}
	}

	b.WriteString("</dleplugin>")
	return b.String(), nil
}

// ReadSource reads a UTF-8 text file with universal newlines: "\r\n" and
// lone "\r" become "\n".
func ReadSource(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("read %s: file is not valid UTF-8", path)
	}
	s := string(data)
	if strings.Contains(s, "\r") {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
	}
	return s, nil
}

func writeFile(b *strings.Builder, name, action, search, replace string) error {
	if err := checkXML(name); err != nil {
		return err
	}
	if err := checkXML(action); err != nil {
		return err
	}
	b.WriteString(`<file name="`)
	b.WriteString(escapeAttr(name))
	b.WriteString(`"><operation action="`)
	b.WriteString(escapeAttr(action))
	b.WriteString(`">`)
	if action != "create" {
		if err := writeCDATA(b, "searchcode", search); err != nil {
			return err
		}
	}
	if err := writeCDATA(b, "replacecode", replace); err != nil {
		return err
	}
	b.WriteString("<enabled>1</enabled></operation></file>")
	return nil
}

func writeText(b *strings.Builder, name, text string) error {
	if err := checkXML(text); err != nil {
		return err
	}
	b.WriteString("<" + name + ">")
	b.WriteString(escapeText(text))
	b.WriteString("</" + name + ">")
	return nil
}

func writeCDATA(b *strings.Builder, name, text string) error {
	if strings.Contains(text, "]]>") {
		return errors.New("CDATA string cannot contain ']]>'")
	}
	if err := checkXML(text); err != nil {
		return err
	}
	b.WriteString("<" + name + "><![CDATA[")
	b.WriteString(text)
	b.WriteString("]]></" + name + ">")
	return nil
}

// checkXML reports an error when s has invalid UTF-8 or characters outside
// the XML 1.0 Char production.
func checkXML(s string) error {
	if !utf8.ValidString(s) {
		return errNotXMLCompatible
	}
	for _, r := range s {
		switch {
		case r == '\t', r == '\n', r == '\r':
		case r < 0x20, r == 0xFFFE, r == 0xFFFF, r >= 0xD800 && r <= 0xDFFF:
			return errNotXMLCompatible
		}
	}
	return nil
}

var (
	textReplacer = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#13;")
	attrReplacer = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;",
		"\n", "&#10;", "\r", "&#13;", "\t", "&#9;")
)

func escapeText(s string) string { return textReplacer.Replace(s) }
func escapeAttr(s string) string { return attrReplacer.Replace(s) }
