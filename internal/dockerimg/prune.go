package dockerimg

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thecoooler/dleplugin/internal/process"
)

// KeepTags is the number of most recent image versions Prune keeps per
// image, including the current version. Registry aliases count as one version.
const KeepTags = 2

// TagPrefix is the repository prefix of the tool images.
const TagPrefix = "dleplugin-"

// TagInfo describes a local image tag.
type TagInfo struct {
	// Reference is "repository:tag"; ID identifies the underlying image.
	Reference  string
	Repository string
	Tag        string
	ID         string
	Created    time.Time
}

const tagsFormat = "{{.Repository}}:{{.Tag}}|{{.ID}}|{{.CreatedAt}}"

// LocalTags lists the local image tags, newest first.
func LocalTags(ctx context.Context, runner *process.Runner) ([]TagInfo, error) {
	out, err := runner.Output(ctx, process.Command{Name: "docker", Args: []string{"image", "ls", "--format", tagsFormat}})
	if err != nil {
		return nil, err
	}
	var tags []TagInfo
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(strings.TrimSpace(line), "|")
		if len(parts) != 3 {
			continue
		}
		ref := parts[0]
		sep := strings.LastIndex(ref, ":")
		if sep < 0 {
			continue
		}
		repo, tag := ref[:sep], ref[sep+1:]
		if tag == "<none>" {
			continue
		}
		tags = append(tags, TagInfo{Reference: ref, Repository: repo, Tag: tag, ID: parts[1], Created: parseCreated(parts[2])})
	}
	sort.SliceStable(tags, func(i, j int) bool {
		ti, tj := tags[i].Created, tags[j].Created
		if ti.Equal(tj) {
			return tags[i].Reference < tags[j].Reference
		}
		if ti.IsZero() {
			return false
		}
		if tj.IsZero() {
			return true
		}
		return ti.After(tj)
	})
	return tags, nil
}

var createdLayouts = []string{
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999 -0700",
	"2006-01-02 15:04:05 -0700 MST",
	"2006-01-02 15:04:05 -0700",
	time.RFC3339Nano,
}

func parseCreated(s string) time.Time {
	for _, layout := range createdLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ImageSize returns the size of a local image tag in bytes.
func ImageSize(ctx context.Context, runner *process.Runner, ref string) (int64, error) {
	out, err := runner.Output(ctx, process.Command{Name: "docker", Args: []string{"image", "inspect", "--format", "{{.Size}}", ref}})
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse image size %q: %w", out, err)
	}
	return n, nil
}

// UsedByContainer reports whether a container (running or stopped) uses ref.
func UsedByContainer(ctx context.Context, runner *process.Runner, ref string) (bool, error) {
	out, err := runner.Output(ctx, process.Command{Name: "docker", Args: []string{"ps", "--all", "--quiet", "--filter", "ancestor=" + ref}})
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// RemoveTag removes a local tag. Without force Docker refuses to remove an
// image used by a container.
func RemoveTag(ctx context.Context, runner *process.Runner, ref string) error {
	_, err := runner.Run(ctx, process.Command{Name: "docker", Args: []string{"image", "rm", ref}})
	return err
}

// Prune removes the older local tags of img, keeping the keep most recent
// image versions (always including the current one) and every local tag or
// registry alias of a kept version. Images used by a container are left in
// place. Failures are joined; callers should treat them as best effort.
func Prune(ctx context.Context, runner *process.Runner, img Image, keep int, notify func(string)) ([]string, error) {
	if keep < 1 {
		keep = 1
	}
	tags, err := LocalTags(ctx, runner)
	if err != nil {
		return nil, err
	}
	var mine []TagInfo
	for _, t := range tags {
		// Include registry aliases: a pulled image keeps a second tag such as
		// ghcr.io/thecoooler/dleplugin-test:<tag>.
		if path.Base(t.Repository) == img.Name {
			mine = append(mine, t)
		}
	}
	// Group local tags and their registry aliases by image ID: a version is
	// kept or removed as a whole, and aliases do not consume retention slots.
	type version struct {
		refs    []string
		current bool
	}
	var versions []*version
	byID := map[string]*version{}
	current := img.Tag()
	for _, t := range mine {
		v, ok := byID[t.ID]
		if !ok {
			v = &version{}
			byID[t.ID] = v
			versions = append(versions, v)
		}
		v.refs = append(v.refs, t.Reference)
		if t.Reference == current {
			v.current = true
		}
	}
	kept := map[string]bool{current: true}
	keptVersions := 0
	for _, v := range versions {
		if !v.current {
			continue
		}
		for _, ref := range v.refs {
			kept[ref] = true
		}
		keptVersions++
	}
	for _, v := range versions {
		if v.current || keptVersions >= keep {
			continue
		}
		for _, ref := range v.refs {
			kept[ref] = true
		}
		keptVersions++
	}
	var removed []string
	var errs []error
	for _, t := range mine {
		if kept[t.Reference] {
			continue
		}
		// An unknown creation date cannot establish recency; never prune it.
		if t.Created.IsZero() {
			continue
		}
		used, err := UsedByContainer(ctx, runner, t.Reference)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if used {
			if notify != nil {
				notify("Keeping image " + t.Reference + " (used by a container).")
			}
			continue
		}
		if err := RemoveTag(ctx, runner, t.Reference); err != nil {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, t.Reference)
		if notify != nil {
			notify("Removed old image " + t.Reference + ".")
		}
	}
	return removed, errors.Join(errs...)
}
