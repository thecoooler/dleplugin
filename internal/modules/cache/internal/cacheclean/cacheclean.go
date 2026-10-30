// Package cacheclean removes data dleplugin keeps in its cache directory:
// test run sites, installed DLE trial bases, static analysis work directories
// and the Docker tool images.
package cacheclean

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/thecoooler/dleplugin/internal/dockerimg"
	"github.com/thecoooler/dleplugin/internal/paths"
	"github.com/thecoooler/dleplugin/internal/process"
)

// Kind identifies a cache entry.
type Kind string

const (
	// RunSite is a per-plugin DLE copy of a test run.
	RunSite Kind = "run site"
	// Base is an installed DLE trial used as the source of run sites.
	Base Kind = "DLE base"
	// StaticCache is a PHPStan work directory or its shared bootstrap file.
	StaticCache Kind = "static cache"
	// TrialArchive is the downloaded DLE trial ZIP.
	TrialArchive Kind = "trial archive"
	// Image is a local Docker tool image tag.
	Image Kind = "image"
)

// Entry is a removed (or, with DryRun, removable) cache entry.
type Entry struct {
	Kind Kind
	Name string
	Size int64
}

// Options configure Clean.
type Options struct {
	// All also removes the current DLE base and the trial archive.
	All bool
	// OlderThan limits removals to entries older than the duration.
	OlderThan time.Duration
	// Docker also removes unused content-addressed tool images.
	Docker bool
	// DryRun reports the removals without performing them.
	DryRun bool
	// Root overrides the dleplugin cache root (tests).
	Root string
	// Now is the reference time for OlderThan; zero means time.Now().
	Now time.Time
}

// Result lists what Clean removed or kept.
type Result struct {
	// Removed contains removed entries; with DryRun, removable entries.
	Removed []Entry
	// Kept contains Docker images used by containers.
	Kept []Entry
	// Freed is the total size of Removed.
	Freed int64
	// Errors are per-entry failures; the cleanup continued past them.
	Errors []error
}

func (r *Result) remove(e Entry) {
	r.Removed = append(r.Removed, e)
	r.Freed += e.Size
}

// Clean removes the selected cache entries. Per-entry failures are reported
// in Result.Errors and joined into the returned error; the remaining entries
// are still cleaned.
func Clean(ctx context.Context, runner *process.Runner, o Options) (Result, error) {
	var res Result
	root := o.Root
	if root == "" {
		var err error
		root, err = paths.CacheRoot()
		if err != nil {
			return res, err
		}
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := time.Time{}
	if o.OlderThan > 0 {
		cutoff = now.Add(-o.OlderThan)
	}

	for _, dir := range []struct {
		path string
		kind Kind
	}{
		{filepath.Join(root, "test", "run"), RunSite},
		{filepath.Join(root, "static"), StaticCache},
	} {
		if err := cleanDir(dir.path, dir.kind, cutoff, o.DryRun, &res); err != nil {
			res.Errors = append(res.Errors, err)
		}
	}
	if err := cleanBases(filepath.Join(root, "test"), o.All, cutoff, o.DryRun, &res); err != nil {
		res.Errors = append(res.Errors, err)
	}
	if o.All {
		cleanFile(filepath.Join(root, "test", "dle_trial.zip"), TrialArchive, cutoff, o.DryRun, &res)
	}
	if o.Docker {
		if runner == nil {
			res.Errors = append(res.Errors, errors.New("Docker cleanup requires a command runner"))
		} else if err := dockerimg.CheckDocker(); err != nil {
			res.Errors = append(res.Errors, err)
		} else if err := cleanImages(ctx, runner, cutoff, o.DryRun, &res); err != nil {
			res.Errors = append(res.Errors, err)
		}
	}
	sort.SliceStable(res.Removed, func(i, j int) bool {
		if res.Removed[i].Kind != res.Removed[j].Kind {
			return res.Removed[i].Kind < res.Removed[j].Kind
		}
		return res.Removed[i].Name < res.Removed[j].Name
	})
	return res, errors.Join(res.Errors...)
}

// cleanDir removes the children of dir (all of them without a cutoff).
func cleanDir(dir string, kind Kind, cutoff time.Time, dryRun bool, res *Result) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("inspect %s: %w", p, err))
			continue
		}
		if !oldEnough(info.ModTime(), cutoff) {
			continue
		}
		size := pathSize(p)
		if !dryRun {
			if err := os.RemoveAll(p); err != nil {
				res.Errors = append(res.Errors, fmt.Errorf("remove %s: %w", p, err))
				continue
			}
		}
		res.remove(Entry{Kind: kind, Name: e.Name(), Size: size})
	}
	return nil
}

// baseInfo describes an installed DLE base directory.
type baseInfo struct {
	name string
	path string
	mod  time.Time
}

// cleanBases removes installed DLE bases. Without All the current base (the
// one matching the downloaded trial archive, or else the newest) is kept.
func cleanBases(dir string, all bool, cutoff time.Time, dryRun bool, res *Result) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", dir, err)
	}
	var bases []baseInfo
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "dle-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("inspect %s: %w", filepath.Join(dir, e.Name()), err))
			continue
		}
		bases = append(bases, baseInfo{name: e.Name(), path: filepath.Join(dir, e.Name()), mod: info.ModTime()})
	}
	if len(bases) == 0 {
		return nil
	}
	current := currentBase(dir, bases)
	sort.SliceStable(bases, func(i, j int) bool { return bases[i].mod.After(bases[j].mod) })
	for _, b := range bases {
		if !all && b.path == current {
			continue
		}
		if !oldEnough(b.mod, cutoff) {
			continue
		}
		size := pathSize(b.path)
		if !dryRun {
			if err := os.RemoveAll(b.path); err != nil {
				res.Errors = append(res.Errors, fmt.Errorf("remove %s: %w", b.path, err))
				continue
			}
		}
		res.remove(Entry{Kind: Base, Name: b.name, Size: size})
	}
	return nil
}

// currentBase returns the base installed from the downloaded trial archive,
// or the newest complete base when the archive is missing or has no matching
// base. Incomplete installations (no .ready) are never current.
func currentBase(dir string, bases []baseInfo) string {
	ready := make([]baseInfo, 0, len(bases))
	for _, b := range bases {
		if _, err := os.Stat(filepath.Join(b.path, ".ready")); err == nil {
			ready = append(ready, b)
		}
	}
	if len(ready) == 0 {
		return ""
	}
	if sum, err := fileHash(filepath.Join(dir, "dle_trial.zip")); err == nil {
		want := filepath.Join(dir, "dle-"+sum[:12])
		for _, b := range ready {
			if b.path == want {
				return want
			}
		}
	}
	newest := ready[0].path
	newestMod := ready[0].mod
	for _, b := range ready[1:] {
		if b.mod.After(newestMod) {
			newest, newestMod = b.path, b.mod
		}
	}
	return newest
}

func cleanFile(path string, kind Kind, cutoff time.Time, dryRun bool, res *Result) {
	info, err := os.Stat(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			res.Errors = append(res.Errors, fmt.Errorf("inspect %s: %w", path, err))
		}
		return
	}
	if !oldEnough(info.ModTime(), cutoff) {
		return
	}
	if !dryRun {
		if err := os.Remove(path); err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("remove %s: %w", path, err))
			return
		}
	}
	res.remove(Entry{Kind: kind, Name: filepath.Base(path), Size: info.Size()})
}

// cleanImages removes local tool image tags not used by a container. Tags of
// one image (a local tag plus its registry alias) are grouped: their layers
// are freed only when no tag of the image remains, and counted once.
func cleanImages(ctx context.Context, runner *process.Runner, cutoff time.Time, dryRun bool, res *Result) error {
	tags, err := dockerimg.LocalTags(ctx, runner)
	if err != nil {
		return err
	}
	byID := map[string][]dockerimg.TagInfo{}
	for _, tag := range tags {
		byID[tag.ID] = append(byID[tag.ID], tag)
	}
	sizes := map[string]int64{}
	sizeOf := func(tag dockerimg.TagInfo) int64 {
		if size, ok := sizes[tag.ID]; ok {
			return size
		}
		size, _ := dockerimg.ImageSize(ctx, runner, tag.Reference)
		sizes[tag.ID] = size
		return size
	}

	var removed []dockerimg.TagInfo
	removedRefs := map[string]bool{}
	for _, tag := range tags {
		if !isToolImage(tag) {
			continue
		}
		// An unknown creation date cannot be compared with a cutoff.
		if !cutoff.IsZero() && tag.Created.IsZero() {
			continue
		}
		if !oldEnough(tag.Created, cutoff) {
			continue
		}
		used, err := dockerimg.UsedByContainer(ctx, runner, tag.Reference)
		if err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		size := sizeOf(tag)
		if used {
			res.Kept = append(res.Kept, Entry{Kind: Image, Name: tag.Reference, Size: size})
			continue
		}
		if !dryRun {
			if err := dockerimg.RemoveTag(ctx, runner, tag.Reference); err != nil {
				res.Errors = append(res.Errors, err)
				continue
			}
		}
		removed = append(removed, tag)
		removedRefs[tag.Reference] = true
	}

	groups := map[string][]dockerimg.TagInfo{}
	for _, tag := range removed {
		groups[tag.ID] = append(groups[tag.ID], tag)
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		group := groups[id]
		refs := make([]string, len(group))
		for i, tag := range group {
			refs[i] = tag.Reference
		}
		sort.Strings(refs)
		complete := true
		for _, tag := range byID[id] {
			if !removedRefs[tag.Reference] {
				complete = false
				break
			}
		}
		size := int64(0)
		if complete {
			size = sizes[id]
		}
		res.Removed = append(res.Removed, Entry{Kind: Image, Name: strings.Join(refs, ", "), Size: size})
		res.Freed += size
	}
	return nil
}

// isToolImage reports whether the tag belongs to a tool image built by
// dockerimg: a dleplugin-* repository (also behind a registry prefix) with a
// content-derived 12-hex tag. Stable tags such as the hosts stack images are
// left alone.
func isToolImage(tag dockerimg.TagInfo) bool {
	repo := tag.Repository
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		repo = repo[i+1:]
	}
	if !strings.HasPrefix(repo, dockerimg.TagPrefix) || len(tag.Tag) != 12 {
		return false
	}
	for _, c := range tag.Tag {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// oldEnough reports whether mod is older than cutoff; a zero cutoff accepts
// every time.
func oldEnough(mod, cutoff time.Time) bool {
	return cutoff.IsZero() || mod.Before(cutoff)
}

// fileHash returns the hex SHA-256 of a file.
func fileHash(path string) (string, error) {
	f, err := os.Open(path)
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

// pathSize returns the size of a file or, recursively, of a directory.
func pathSize(path string) int64 {
	info, err := os.Lstat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return info.Size()
	}
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}
