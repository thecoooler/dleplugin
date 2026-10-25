package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// view builds the window: a tab for each open project with the pages of
// sites and settings beside them, the selected page, and the console of
// command output below it.
func (a *App) view(c *ui.Context) {
	a.drain()
	for _, n := range a.notices {
		if n.error {
			c.AddToast(ui.Toast{Title: n.text, Type: "error", Timeout: 8 * time.Second})
		} else {
			c.Toast(n.text)
		}
	}
	a.notices = nil

	ui.Column(c).Fill().Children(func() {
		a.tabBar(c)
		ui.SplitVerticalPercent(c, &a.consolePercent, func() {
			ui.Column(c).Fill().Children(func() { a.page(c) })
		}, func() {
			a.consoleView(c)
		}).Grow(1)
	})
	a.dialogs(c)
}

// tabBar shows the open projects as tabs, a menu opening more, and the
// buttons of the sites and settings pages.
func (a *App) tabBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Background(t.Surface).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Padding(6, 8, 0).Gap(8).AlignItems(ui.End).Children(func() {
		ui.ScrollHorizontal(c).Grow(1).Shrink(1).Children(func() {
			ui.Row(c).Gap(2).AlignItems(ui.End).Children(func() {
				for _, root := range a.settings.Open() {
					if pv := a.projects[root]; pv != nil {
						a.projectTab(c, pv)
					}
				}
				ui.MenuButton(c.Key("open-menu"), "Open", func(m *ui.Menu) {
					if m.Item("Open plugin folder…").Chosen() {
						a.chooseProject()
					}
					if m.Item("New plugin…").Chosen() {
						a.openNewProject("")
					}
					if recent := a.settings.Recent(); len(recent) > 0 {
						m.Separator()
						for _, root := range recent {
							if m.Item(root).Chosen() {
								a.addProjectDir(root)
							}
						}
					}
				}).Tooltip("Open a plugin folder in a new tab").Margin(0, 0, 4, 4)
			})
		})
		ui.Row(c).Gap(4).Padding(0, 0, 4).Children(func() {
			a.pageTab(c, pageSites, "Sites", "Deployment sites of every project")
			a.pageTab(c, pageSettings, "Settings", "")
		})
	})
}

// projectTab is the tab of an open project, with a button closing it.
func (a *App) projectTab(c *ui.Context, pv *projectView) {
	t := c.Theme()
	on := a.selected == pv.root
	tab := ui.ButtonBase(c.Key(pv.root)).Padding(6, 6, 6, 12).Gap(6).Radius(t.Radius, t.Radius, 0, 0).MaxWidth(240).
		BorderWidth(1, 1, 0, 1).Label(pv.title()).Tooltip(pv.root)
	if on {
		tab.Background(t.Background).BorderColor(t.Border)
	} else {
		tab.BorderColor(ui.Color{}).TextColor(t.TextMuted)
		if tab.Hovered() {
			tab.Background(t.SurfaceHover)
		}
	}
	if tab.Clicked() {
		a.show(pv.root)
	}
	tab.Children(func() {
		title := ui.Text(c, pv.title()).SingleLine().Shrink(1)
		if on {
			title.FontWeight(600)
		}
		if pv.err != nil {
			ui.Text(c, "!").TextColor(t.Danger).Bold()
		}
		closeTab := ui.ButtonBase(c.Key("close")).Size(18, 18).Radius(t.Radius).Label("Close " + pv.title()).Tooltip("Close the tab; the files stay")
		if closeTab.Hovered() {
			closeTab.Background(t.SurfacePressed)
		}
		closeTab.Children(func() { ui.Text(c, "×").FontSize(14).TextColor(t.TextMuted) })
		if closeTab.Clicked() {
			a.closeProject(pv.root)
		}
	})
}

// pageTab is a button showing a page that is not a project.
func (a *App) pageTab(c *ui.Context, page, label, tooltip string) {
	var b ui.Element
	if a.selected == page {
		b = ui.PrimaryButton(c.Key(page), label)
	} else {
		b = ui.Button(c.Key(page), label)
	}
	if tooltip != "" {
		b.Tooltip(tooltip)
	}
	if b.Clicked() {
		if a.selected == page {
			// Back to the last project tab.
			if open := a.settings.Open(); len(open) > 0 {
				a.show(open[len(open)-1])
				return
			}
		}
		a.show(page)
	}
}

func (a *App) page(c *ui.Context) {
	switch a.selected {
	case pageSites:
		a.globalSitesPage(c)
	case pageSettings:
		a.settingsPage(c)
	default:
		if pv := a.current(); pv != nil {
			a.projectPage(c, pv)
		} else {
			a.emptyPage(c)
		}
	}
}

func (a *App) emptyPage(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Center().Gap(12).Children(func() {
		ui.Text(c, "No plugin open").FontSize(18).Bold()
		ui.Text(c, "Open a folder with dleplugin.yml, or create a new plugin project.").TextColor(t.TextMuted)
		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c.Key("empty-add"), "Open plugin folder…").Clicked() {
				a.chooseProject()
			}
			if ui.Button(c.Key("empty-new"), "New plugin…").Clicked() {
				a.openNewProject("")
			}
		})
		if recent := a.settings.Recent(); len(recent) > 0 {
			ui.Column(c).Gap(4).Width(520).Padding(12, 0, 0).Children(func() {
				ui.Text(c, "Recent").Bold()
				home, _ := os.UserHomeDir()
				for _, root := range recent {
					ui.Row(c.Key(root)).Gap(8).Children(func() {
						ui.Text(c, shortPath(root, home)).Grow(1).Shrink(1).SingleLine().Tooltip(root)
						if ui.Button(c.Key("reopen"), "Open").Clicked() {
							a.addProjectDir(root)
						}
						if ui.Button(c.Key("forget"), "Forget").Tooltip("Remove from the list; the files stay").Clicked() {
							a.forgetProject(root)
						}
					})
				}
			})
		}
	})
}

// pageHeader is the title row of a page, with actions on the right.
func pageHeader(c *ui.Context, title, subtitle string, actions func()) {
	t := c.Theme()
	ui.Row(c).Padding(16, 20, 12).Gap(12).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(20).Bold().SingleLine()
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(12).TextColor(t.TextMuted).SingleLine().Selectable()
			}
		})
		if actions != nil {
			ui.Row(c).Gap(8).Children(actions)
		}
	})
}

// section is a titled group of controls on a page.
func section(c *ui.Context, title, description string, fn func()) {
	t := c.Theme()
	ui.Column(c).Gap(10).Padding(16).Radius(t.Radius*2).Border(1, t.Border).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(15).Bold()
			if description != "" {
				ui.Text(c, description).FontSize(12).TextColor(t.TextMuted)
			}
		})
		fn()
	})
}

// errorText shows an error message, selectable to copy.
func errorText(c *ui.Context, err string) {
	if err == "" {
		return
	}
	t := c.Theme()
	ui.Text(c, err).TextColor(t.Danger).Selectable()
}

func (a *App) settingsPage(c *ui.Context) {
	t := c.Theme()
	pageHeader(c, "Settings", "", nil)
	ui.Scroll(c).Grow(1).Padding(20).Gap(16).Children(func() {
		section(c, "Commands", "", func() {
			ui.Row(c).Gap(12).Children(func() {
				if ui.Switch(c, &a.settings.Verbose).Label("Verbose output").Changed() {
					a.saveSettings()
				}
				ui.Column(c).Gap(2).Shrink(1).Children(func() {
					ui.Text(c, "Verbose output")
					ui.Text(c, "Run commands with --verbose: external tools' commands, timings and error chains.").FontSize(12).TextColor(t.TextMuted)
				})
			})
		})
		section(c, "Files", "", func() {
			pathRow(c, a, "Global config", a.configPath)
			global, _ := registryPaths(a.configPath, "")
			pathRow(c, a, "Global sites", global)
			pathRow(c, a, "GUI settings", a.settingsPath)
		})
		section(c, "About", "", func() {
			ui.Textf(c, "dleplugin %s", a.version)
			ui.Text(c, "The desktop app runs the same commands as the dleplugin CLI; see the console for their output.").FontSize(12).TextColor(t.TextMuted)
		})
	})
}

// pathRow shows a path with a button opening it.
func pathRow(c *ui.Context, a *App, label, path string) {
	t := c.Theme()
	ui.Row(c.Key(label)).Gap(12).Children(func() {
		ui.Text(c, label).Width(110).TextColor(t.TextMuted)
		ui.Text(c, path).Grow(1).Shrink(1).Selectable()
		if ui.Button(c, "Open").Disabled(!exists(path)).Clicked() {
			a.openPath(path)
		}
	})
}

func (a *App) openPath(path string) {
	a.background(func() {
		if err := a.desk.openPath(path); err != nil {
			a.post(func() { a.notify(err.Error(), true) })
		}
	})
}

func (a *App) consoleView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Surface).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Row(c).Padding(6, 12).Gap(8).Children(func() {
			ui.Text(c, "Console").Bold()
			switch j := a.job; {
			case j != nil:
				ui.Spinner(c).Size(14, 14)
				ui.Text(c, j.title+"…").TextColor(t.TextMuted).SingleLine()
			case a.lastJob != nil:
				status, color := "succeeded", t.Success
				if a.lastJob.code != 0 {
					status, color = "failed", t.Danger
				}
				ui.Textf(c, "%s %s in %s", a.lastJob.title, status, a.lastJob.duration.Round(100*time.Millisecond)).TextColor(color).SingleLine()
			}
			ui.Spacer(c)
			if a.job != nil && ui.Button(c.Key("cancel"), "Cancel").Clicked() {
				a.cancelJob()
			}
			if ui.Button(c.Key("copy-console"), "Copy").Disabled(len(a.console.lines) == 0).Clicked() {
				c.WriteClipboard(strings.Join(a.console.lines, "\n"))
				c.Toast("Console copied")
			}
			if ui.Button(c.Key("clear-console"), "Clear").Disabled(len(a.console.lines) == 0 || a.busy()).Clicked() {
				a.console.lines = nil
			}
		})
		ui.List(c, &a.console.list, len(a.console.lines), func(i int) {
			line := a.console.lines[i]
			text := ui.Text(c, line).Font("monospace").FontSize(12)
			switch {
			case strings.HasPrefix(line, "$ "):
				text.Bold().TextColor(t.Accent)
			case strings.HasPrefix(line, "✔"):
				text.TextColor(t.Success)
			case strings.HasPrefix(line, "✖"):
				text.TextColor(t.Danger)
			case strings.HasPrefix(line, "⚠"):
				text.TextColor(t.Warning)
			}
		}).Grow(1).Padding(4, 12, 8).Selectable().Children(func() {
			if len(a.console.lines) == 0 {
				ui.Text(c, "Output of the commands you run shows here.").TextColor(t.TextMuted).FontSize(12)
			}
		})
	})
}

// formatSize is a file size for people.
func formatSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// shortPath replaces the home directory with ~.
func shortPath(path, home string) string {
	if home != "" {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return path
}
