// Package cache implements the cache module: removing data dleplugin keeps in
// its cache directory (test run sites and bases, static work directories and
// Docker tool images).
package cache

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/modules/cache/internal/cacheclean"
)

type module struct{}

// Module returns the cache module.
func Module() app.Module { return module{} }

func (module) Name() string { return "cache" }

func (module) Commands(rt *app.Runtime) []*cobra.Command {
	return []*cobra.Command{cacheCommand(rt)}
}

func cacheCommand(rt *app.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "cache",
		GroupID: app.GroupMaintenance,
		Short:   "Manage the dleplugin cache",
		Long: `Manage data dleplugin keeps in its cache directory.

The cache holds the downloaded DLE trial, installed DLE bases, per-plugin
test run sites, static analysis work directories and the Docker tool images
used by test and static.`,
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(cleanCommand(rt))
	return cmd
}

func cleanCommand(rt *app.Runtime) *cobra.Command {
	var (
		o         cacheclean.Options
		olderThan string
	)
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove cached test, static and Docker data",
		Long: `Remove data dleplugin cached on disk.

Without flags, removes test run sites, static analysis work directories and
every installed DLE trial base except the current one. The current base is the
one installed from the downloaded trial archive, or else the newest.

--all also removes the current DLE base and the downloaded trial archive.
--docker removes unused content-addressed dleplugin-* tool images (test and
static); images used by containers and stable-tag images such as the hosts
stack are kept. --older-than limits every removal to entries older than the
duration, for example 24h or 30m.

Do not run this while a test is running: active run sites are removed too.`,
		Example: "  dleplugin cache clean\n" +
			"  dleplugin cache clean --older-than 24h\n" +
			"  dleplugin cache clean --all --docker --dry-run",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if olderThan != "" {
				d, err := time.ParseDuration(olderThan)
				if err != nil || d <= 0 {
					return fmt.Errorf("Invalid --older-than duration %q (use a positive duration such as 24h or 30m)", olderThan)
				}
				o.OlderThan = d
			}
			res, err := cacheclean.Clean(cmd.Context(), rt.Exec, o)
			if err != nil && len(res.Errors) == 0 {
				return err
			}
			printResult(rt, o, res)
			for _, e := range res.Errors {
				rt.UI.Warning(e.Error())
			}
			if err != nil {
				if len(res.Errors) == 0 {
					return err
				}
				return &app.SilentExit{Code: 1}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&o.All, "all", false, "Also remove the current DLE base and the downloaded trial archive")
	f.StringVar(&olderThan, "older-than", "", "Only remove entries older than this duration, e.g. 24h")
	f.BoolVar(&o.Docker, "docker", false, "Remove unused dleplugin-* tool images")
	f.BoolVar(&o.DryRun, "dry-run", false, "Show what would be removed without removing it")
	return cmd
}

func printResult(rt *app.Runtime, o cacheclean.Options, res cacheclean.Result) {
	verb := "Removed"
	if o.DryRun {
		verb = "Would remove"
	}
	for _, e := range res.Removed {
		rt.UI.Infof("%s %s %s (%s)", verb, e.Kind, e.Name, formatSize(e.Size))
	}
	for _, e := range res.Kept {
		rt.UI.Infof("Kept %s %s (used by a container)", e.Kind, e.Name)
	}
	if len(res.Removed) == 0 {
		switch {
		case len(res.Errors) > 0:
			rt.UI.Info("Nothing was removed.")
		case len(res.Kept) > 0:
			rt.UI.Info("Nothing to remove.")
		default:
			rt.UI.Info("Cache is clean.")
		}
		return
	}
	total := "Freed"
	if o.DryRun {
		total = "Would free"
	}
	rt.UI.Infof("%s %s (%d %s).", total, formatSize(res.Freed), len(res.Removed), plural(len(res.Removed)))
}

func plural(n int) string {
	if n == 1 {
		return "entry"
	}
	return "entries"
}

// formatSize renders a byte count with one decimal in the largest unit.
func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
