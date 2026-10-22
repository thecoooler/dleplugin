// Package dist handles clean DLE distributions.
package dist

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ErrNoDir reports that the distributions directory is not set.
var ErrNoDir = errors.New("the distributions directory is not set: dleplugin hosts config set dists_dir PATH")

var (
	versionIDRe     = regexp.MustCompile(`['"]version_id['"]\s*=>\s*['"]([0-9]+\.[0-9]+)['"]`)
	versionDefineRe = regexp.MustCompile(`define\s*\(\s*['"]VERSIONID['"]\s*,\s*['"]([0-9]+\.[0-9]+)['"]\s*\)`)
)

// Info holds the checked parameters of a clean DLE distribution.
type Info struct {
	Path    string
	Version string
	Prefix  string
}

// Inspect checks that a ZIP archive is a clean DLE distribution and detects the
// version and the site directory prefix in the archive from where install.php is.
func Inspect(archive *zip.Reader) (*Info, error) {
	prefix, installFile, err := findInstallScript(archive)
	if err != nil {
		return nil, err
	}
	for _, f := range archive.File {
		if f.Name == prefix+"engine/data/config.php" || f.Name == prefix+"engine/data/dbconfig.php" {
			return nil, fmt.Errorf("the archive is not a clean distribution: it contains %s (the site is already installed)", f.Name)
		}
	}
	if !hasFile(archive, prefix+"index.php") {
		return nil, fmt.Errorf("the archive is not a complete DLE distribution: %s is missing", prefix+"index.php")
	}
	if !hasEngine(archive, prefix) {
		return nil, fmt.Errorf("the archive is not a complete DLE distribution: directory %s is missing", prefix+"engine/")
	}
	version, err := readVersion(installFile)
	if err != nil {
		return nil, err
	}
	if !CleanSupported(version) {
		return nil, fmt.Errorf("unsupported DLE version %q: installation is available from 11.0", version)
	}
	return &Info{
		Version: version,
		Prefix:  prefix,
	}, nil
}

// InspectFile opens the ZIP archive at path and checks its contents.
func InspectFile(path string) (*Info, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	info, err := Inspect(&r.Reader)
	if err != nil {
		return nil, err
	}
	info.Path = path
	return info, nil
}

// DetectVersion detects the DLE version from the archive contents (from install.php).
func DetectVersion(archive *zip.Reader) (string, error) {
	info, err := Inspect(archive)
	if err != nil {
		return "", err
	}
	return info.Version, nil
}

func findInstallScript(archive *zip.Reader) (string, *zip.File, error) {
	var candidates []*zip.File
	for _, f := range archive.File {
		if f.Name == "install.php" || strings.HasSuffix(f.Name, "/install.php") {
			candidates = append(candidates, f)
		}
	}
	if len(candidates) == 0 {
		return "", nil, errors.New("the archive has no install.php")
	}
	// With several candidates, pick the one next to index.php
	matches := candidates
	if len(candidates) > 1 {
		var valid []*zip.File
		for _, c := range candidates {
			prefix := strings.TrimSuffix(c.Name, "install.php")
			if hasFile(archive, prefix+"index.php") {
				valid = append(valid, c)
			}
		}
		if len(valid) > 0 {
			matches = valid
		}
	}
	if len(matches) > 1 {
		return "", nil, fmt.Errorf("the archive has several possible directories with install.php (%d): unpack the nested ZIP", len(matches))
	}
	installFile := matches[0]
	prefix := strings.TrimSuffix(installFile.Name, "install.php")
	return prefix, installFile, nil
}

func hasFile(archive *zip.Reader, path string) bool {
	for _, f := range archive.File {
		if f.Name == path {
			return true
		}
	}
	return false
}

func hasEngine(archive *zip.Reader, prefix string) bool {
	enginePrefix := prefix + "engine/"
	for _, f := range archive.File {
		if strings.HasPrefix(f.Name, enginePrefix) {
			return true
		}
	}
	return false
}

func readVersion(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, 1024*1024))
	if err != nil {
		return "", err
	}
	if m := versionIDRe.FindSubmatch(data); m != nil {
		return string(m[1]), nil
	}
	if m := versionDefineRe.FindSubmatch(data); m != nil {
		return string(m[1]), nil
	}
	return "", fmt.Errorf("no DLE version found in %s (version_id field)", f.Name)
}

// CleanSupported accepts numeric DLE release versions starting with 11.0.
func CleanSupported(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return !VersionLess(version, "11.0")
}

// Resolve finds the archive <version>.zip in dir and checks that it is a
// clean distribution of the given DLE version.
func Resolve(dir, version string) (string, error) {
	if dir == "" {
		return "", ErrNoDir
	}
	if !CleanSupported(version) {
		return "", fmt.Errorf("unsupported DLE version %q: installation is available from 11.0", version)
	}
	path := filepath.Join(dir, version+".zip")
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("no DLE %s distribution: %s.zip not found in %s", version, version, dir)
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("no DLE %s distribution: %s is a directory, expected a ZIP archive", version, path)
	}
	distInfo, err := InspectFile(path)
	if err != nil {
		return "", fmt.Errorf("archive %s: %w", filepath.Base(path), err)
	}
	if distInfo.Version != version {
		return "", fmt.Errorf("archive %s contains DLE %s, expected %s", filepath.Base(path), distInfo.Version, version)
	}
	return path, nil
}

// List returns the DLE versions of the clean <version>.zip distributions in dir, ascending.
func List(dir string) ([]string, error) {
	if dir == "" {
		return nil, ErrNoDir
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var versions []string
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		version := strings.TrimSuffix(e.Name(), ".zip")
		if !CleanSupported(version) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := InspectFile(path)
		if err != nil || info.Version != version {
			continue
		}
		if !seen[info.Version] {
			versions = append(versions, info.Version)
			seen[info.Version] = true
		}
	}
	sort.Slice(versions, func(i, j int) bool { return VersionLess(versions[i], versions[j]) })
	return versions, nil
}

// VersionLess compares versions such as 19.0 and 11.2 by their numeric parts.
func VersionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, ea := strconv.Atoi(pa[i])
		nb, eb := strconv.Atoi(pb[i])
		if ea != nil || eb != nil {
			if pa[i] != pb[i] {
				return pa[i] < pb[i]
			}
			continue
		}
		if na != nb {
			return na < nb
		}
	}
	return len(pa) < len(pb)
}

// Extract unpacks a clean distribution into dest.
func Extract(archive *zip.ReadCloser, dest string) error {
	info, err := Inspect(&archive.Reader)
	if err != nil {
		return err
	}
	return ExtractPrefix(archive, dest, info.Prefix)
}

// ExtractPrefix unpacks the archive files starting with prefix into dest, stripping the prefix.
func ExtractPrefix(archive *zip.ReadCloser, dest, prefix string) error {
	return extract(archive, dest, prefix)
}

func extract(archive *zip.ReadCloser, dest, prefix string) error {
	root := filepath.Clean(dest) + string(os.PathSeparator)
	for _, f := range archive.File {
		if !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		name := strings.TrimPrefix(f.Name, prefix)
		if name == "" {
			continue
		}
		target := filepath.Join(dest, name)
		if !strings.HasPrefix(target, root) {
			return fmt.Errorf("invalid path in the archive: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := extractFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}
