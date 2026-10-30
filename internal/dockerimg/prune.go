package dockerimg

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thecoooler/dleplugin/internal/process"
)

// KeepTags is the number of most recent tags Prune keeps per image.
const KeepTags = 2

// TagPrefix is the repository prefix of the tool images.
const TagPrefix = "dleplugin-"

// TagInfo describes a local image tag.
type TagInfo struct {
	// Reference is "repository:tag".
	Reference  string
	Repository string
	Tag        string
	Created    time.Time
}

const tagsFormat = "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}"

// LocalTags lists the local image tags, newest first.
func LocalTags(ctx context.Context, runner *process.Runner) ([]TagInfo, error) {
	out, err := runner.Output(ctx, process.Command{Name: "docker", Args: []string{"image", "ls", "--format", tagsFormat}})
	if err != nil {
		return nil, err
	}
	var tags []TagInfo
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(strings.TrimSpace(line), "|")
		if len(parts) != 2 {
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
		tags = append(tags, TagInfo{Reference: ref, Repository: repo, Tag: tag, Created: parseCreated(parts[1])})
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

var createdLayouts = []string{"2006-01-02 15:04:05 -0700 MST", "2006-01-02 15:04:05 -0700", time.RFC3339Nano}

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
// tags and the current tag. Images used by a container are left in place.
// Failures are joined; callers should treat them as best effort.
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
		if t.Repository == img.Name {
			mine = append(mine, t)
		}
	}
	kept := map[string]bool{img.Tag(): true}
	for i := 0; i < len(mine) && i < keep; i++ {
		kept[mine[i].Reference] = true
	}
	var removed []string
	var errs []error
	for _, t := range mine {
		if kept[t.Reference] {
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
