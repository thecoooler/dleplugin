// Package static implements the static module: PHP static analysis for the
// plugin.
package static

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/modules/static/internal/static"
	"github.com/thecoooler/dleplugin/internal/plugin"
)

type module struct{}

// Module returns the static module.
func Module() app.Module { return module{} }

func (module) Name() string { return "static" }

func (module) Commands(rt *app.Runtime) []*cobra.Command {
	return []*cobra.Command{staticCommand(rt)}
}

// pluginRoot finds the plugin root from the working directory with symlinks
// resolved.
func pluginRoot(rt *app.Runtime) (root, wd string, err error) {
	if wd, err = rt.WorkDir(); err != nil {
		return "", "", err
	}
	if root, err = plugin.FindRoot(wd); err != nil {
		return "", "", err
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	return root, wd, nil
}

func staticCommand(rt *app.Runtime) *cobra.Command {
	var (
		toolVerbose, jsonOutput, countOnly bool
		phpVersion                         string
		dependencyRoots                    []string
	)
	cmd := &cobra.Command{
		Use:     "static [FILES...]",
		GroupID: app.GroupCheck,
		Short:   "Run static analysis for the plugin",
		Long: `Run PHPStan for the plugin inside Docker (no local PHP needed).

FILES are relative to the current directory and default to src. Dependencies
listed in dlestatic.yml are resolved from the "static" section of the global
config or --dependency-root.

The code is analysed for the container's PHP (8.4) unless --php-version sets
another target version, e.g. 8.1.

Exits with code 1 when there are findings or the analyser failed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if toolVerbose {
				rt.UI.SetVerbose(true)
			}
			var php int
			if phpVersion != "" {
				var err error
				if php, err = static.ParsePHPVersion(phpVersion); err != nil {
					return err
				}
			}
			root, wd, err := pluginRoot(rt)
			if err != nil {
				return err
			}
			cfg, err := static.LoadConfig(rt.Config)
			if err != nil {
				return err
			}
			files := []string{"src"}
			if len(args) > 0 {
				files = plugin.NormalizeFiles(args, root, wd)
			}
			if len(dependencyRoots) > 0 {
				cfg.DependencyRoots = dependencyRoots
			}
			res, err := static.Run(cmd.Context(), rt.Exec, static.Options{
				PluginDir:       root,
				Files:           files,
				InvocationDir:   wd,
				Dependencies:    cfg.Dependencies,
				DependencyRoots: cfg.DependencyRoots,
				PHPVersion:      php,
				StreamBuild:     rt.UI.Verbose(),
				// keep stdout clean for --json/--count
				Notify: func(msg string) { fmt.Fprintln(rt.UI.Err, msg) },
			})
			if err != nil {
				return err
			}
			switch {
			case countOnly:
				printCount(rt, res)
			case jsonOutput:
				if err := printJSON(rt, res); err != nil {
					return err
				}
			default:
				printLines(rt, res)
			}
			if len(res.Messages) > 0 || len(res.Errors) > 0 {
				return &app.SilentExit{Code: 1}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&toolVerbose, "tool-verbose", "t", false, "Verbose output from static analysis tools")
	f.BoolVar(&jsonOutput, "json", false, "Print result as JSON")
	f.BoolVar(&countOnly, "count", false, "Print errors count only")
	f.StringVar(&phpVersion, "php-version", "", "PHP version to analyse for, e.g. 8.1 (default: the container's PHP)")
	f.StringArrayVar(&dependencyRoots, "dependency-root", nil, "Directory used to resolve dependencies from dlestatic.yml (repeatable)")
	return cmd
}

func countGroups(files [][]static.Group) int {
	n := 0
	for _, groups := range files {
		n += len(groups)
	}
	return n
}

// printCount prints the number of distinct file:line locations.
func printCount(rt *app.Runtime, res *static.Result) {
	rt.UI.Info(strconv.Itoa(countGroups(static.GroupMessages(res.Messages))))
}

// printJSON prints the first message of every file:line location.
func printJSON(rt *app.Runtime, res *static.Result) error {
	files := static.GroupMessages(res.Messages)
	out := struct {
		Messages []static.Message  `json:"messages"`
		Errors   map[string]string `json:"errors"`
		Warnings []string          `json:"warnings"`
		Count    int               `json:"count"`
	}{Messages: []static.Message{}, Errors: res.Errors, Warnings: res.Warnings, Count: countGroups(files)}
	for _, groups := range files {
		for _, g := range groups {
			out.Messages = append(out.Messages, g.Messages[0])
		}
	}
	if out.Errors == nil {
		out.Errors = map[string]string{}
	}
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return err
	}
	_, err := rt.UI.Out.Write(buf.Bytes())
	return err
}

// printLines prints "file:line: message" (lines descending per file), the
// total and the warnings and provider errors.
func printLines(rt *app.Runtime, res *static.Result) {
	n := 0
	for _, groups := range static.GroupMessages(res.Messages) {
		for _, g := range static.SortLinesDesc(groups) {
			line := "-"
			if g.Line != nil {
				line = strconv.Itoa(*g.Line)
			}
			for _, m := range g.Messages {
				rt.UI.Infof("%s:%s: %s", m.File, line, m.Message)
				n++
			}
		}
	}
	rt.UI.Info("")
	rt.UI.Infof("[INFO] %d error(s)", n)
	for _, w := range res.Warnings {
		rt.UI.Warning("[WARNING] " + w)
	}
	for provider, msg := range res.Errors {
		rt.UI.Errorf("[ERROR] %s: %s", provider, msg)
	}
}
