// Package dist — чистые дистрибутивы DLE.
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

// ErrNoDir — не задан каталог дистрибутивов.
var ErrNoDir = errors.New("не задан каталог дистрибутивов: dlehost config set dists_dir <путь>")

var (
	versionIDRe    = regexp.MustCompile(`['"]version_id['"]\s*=>\s*['"]([0-9]+\.[0-9]+)['"]`)
	versionDefineRe = regexp.MustCompile(`define\s*\(\s*['"]VERSIONID['"]\s*,\s*['"]([0-9]+\.[0-9]+)['"]\s*\)`)
)

// Info содержит проверенные параметры чистого дистрибутива DLE.
type Info struct {
	Path    string
	Version string
	Prefix  string
}

// Inspect проверяет ZIP-архив на соответствие чистому дистрибутиву DLE
// и определяет версию и префикс каталога сайта в архиве по расположению install.php.
func Inspect(archive *zip.Reader) (*Info, error) {
	prefix, installFile, err := findInstallScript(archive)
	if err != nil {
		return nil, err
	}
	for _, f := range archive.File {
		if f.Name == prefix+"engine/data/config.php" || f.Name == prefix+"engine/data/dbconfig.php" {
			return nil, fmt.Errorf("архив не является чистым дистрибутивом: содержит %s (сайт уже установлен)", f.Name)
		}
	}
	if !hasFile(archive, prefix+"index.php") {
		return nil, fmt.Errorf("архив не является полным дистрибутивом DLE: отсутствует %s", prefix+"index.php")
	}
	if !hasEngine(archive, prefix) {
		return nil, fmt.Errorf("архив не является полным дистрибутивом DLE: отсутствует каталог %s", prefix+"engine/")
	}
	version, err := readVersion(installFile)
	if err != nil {
		return nil, err
	}
	if !CleanSupported(version) {
		return nil, fmt.Errorf("неподдерживаемая версия DLE %q: установка доступна начиная с 11.0", version)
	}
	return &Info{
		Version: version,
		Prefix:  prefix,
	}, nil
}

// InspectFile открывает ZIP-архив по пути и проверяет его содержимое.
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

// DetectVersion определяет версию DLE по содержимому архива (из install.php).
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
		return "", nil, errors.New("в архиве нет install.php")
	}
	// Если кандидатов несколько, выбираем тот, рядом с которым расположен index.php
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
		return "", nil, fmt.Errorf("в архиве несколько возможных каталогов с install.php (%d): распакуйте вложенный ZIP", len(matches))
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
	return "", fmt.Errorf("в %s не найдена версия DLE (поле version_id)", f.Name)
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

// Resolve находит архив <версия>.zip в каталоге dir
// и проверяет, что он является чистым дистрибутивом и содержит указанную версию DLE.
func Resolve(dir, version string) (string, error) {
	if dir == "" {
		return "", ErrNoDir
	}
	if !CleanSupported(version) {
		return "", fmt.Errorf("неподдерживаемая версия DLE %q: установка доступна начиная с 11.0", version)
	}
	path := filepath.Join(dir, version+".zip")
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("нет дистрибутива DLE %s: файл %s.zip не найден в %s", version, version, dir)
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("нет дистрибутива DLE %s: %s является каталогом, ожидался ZIP-архив", version, path)
	}
	distInfo, err := InspectFile(path)
	if err != nil {
		return "", fmt.Errorf("архив %s: %w", filepath.Base(path), err)
	}
	if distInfo.Version != version {
		return "", fmt.Errorf("в архиве %s обнаружена версия DLE %s, ожидалась %s", filepath.Base(path), distInfo.Version, version)
	}
	return path, nil
}

// List — версии DLE из чистых дистрибутивов <версия>.zip в каталоге dir по возрастанию.
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

// VersionLess сравнивает версии вида 19.0 и 11.2 по числовым частям.
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

// Extract распаковывает чистый дистрибутив в dest.
func Extract(archive *zip.ReadCloser, dest string) error {
	info, err := Inspect(&archive.Reader)
	if err != nil {
		return err
	}
	return ExtractPrefix(archive, dest, info.Prefix)
}

// ExtractPrefix распаковывает файлы архива, начинающиеся с prefix, в dest со снятием префикса.
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
			return fmt.Errorf("недопустимый путь в архиве: %s", f.Name)
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
