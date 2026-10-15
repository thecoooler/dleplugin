package dletest

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/thecoooler/dleplugin/internal/paths"
)

// TrialURL is the official download of the free DLE trial. The trial cannot
// be redistributed, so every user downloads it.
const TrialURL = "https://dle-news.ru/files/dle_trial.zip"

// CacheRoot is the dleplugin "test" cache directory: the trial archive,
// installed bases and per-plugin run directories.
func CacheRoot() (string, error) { return paths.CacheDir("test") }

// Download fetches url into dest through a temporary file.
func Download(ctx context.Context, client *http.Client, url, dest string) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".*")
	if err != nil {
		return err
	}
	defer func() {
		tmp.Close()
		if err != nil {
			os.Remove(tmp.Name())
		}
	}()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

// FileHash returns the hex sha256 of the file at p.
func FileHash(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// trialRoot is the directory of the DLE files inside the trial archive.
const trialRoot = "upload/"

// Unpack extracts the DLE files (the upload/ directory) of the trial archive
// into dir.
func Unpack(archive, dir string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("open %s: %w", archive, err)
	}
	defer zr.Close()
	found := false
	for _, f := range zr.File {
		// Clean resolves "..", so entries escaping upload/ are skipped.
		name := path.Clean(f.Name)
		if !strings.HasPrefix(name+"/", trialRoot) || name+"/" == trialRoot {
			continue
		}
		rel := strings.TrimPrefix(name, trialRoot)
		found = true
		target := filepath.Join(dir, filepath.FromSlash(rel))
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
	if !found {
		return fmt.Errorf("%s has no %s directory: not a DLE archive", archive, strings.TrimSuffix(trialRoot, "/"))
	}
	return nil
}

func extractFile(f *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	src, err := f.Open()
	if err != nil {
		return fmt.Errorf("extract %s: %w", f.Name, err)
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return fmt.Errorf("extract %s: %w", f.Name, err)
	}
	return dst.Close()
}

// SiteURL is the site address (http_home_url) of test installations.
const SiteURL = "http://localhost/"

var homeURLRe = regexp.MustCompile(`('http_home_url'\s*=>\s*)'[^']*'`)

// SetSiteURL replaces http_home_url in engine/data/config.php: the installer
// stores the temporary address it was served from.
func SetSiteURL(site, siteURL string) error {
	p := filepath.Join(site, "engine", "data", "config.php")
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if !homeURLRe.Match(b) {
		return fmt.Errorf("%s: http_home_url not found", p)
	}
	b = homeURLRe.ReplaceAll(b, []byte("${1}'"+siteURL+"'"))
	return os.WriteFile(p, b, 0o644)
}

var versionRe = regexp.MustCompile(`'version_id'\s*=>\s*'([^']+)'`)

// Version reads version_id from the installed engine/data/config.php ("" when
// unknown).
func Version(site string) string {
	b, err := os.ReadFile(filepath.Join(site, "engine", "data", "config.php"))
	if err != nil {
		return ""
	}
	if m := versionRe.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}
