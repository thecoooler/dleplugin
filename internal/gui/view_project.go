package gui

import (
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

var projectTabs = []string{"Overview", "Sites", "Checks"}

const (
	tabOverview = iota
	tabSites
	tabChecks
)

func (a *App) projectPage(c *ui.Context, pv *projectView) {
	if !pv.sites.loaded && !pv.sites.loading {
		a.loadSites(&pv.sites, pv.root)
	}
	home, _ := os.UserHomeDir()
	pageHeader(c, pv.title(), shortPath(pv.root, home), func() {
		if iconButton(c, ui.Button(c.Key("open-folder"), ""), folderIcon, "Open folder").Clicked() {
			a.openPath(pv.root)
		}
		if iconButton(c, ui.Button(c.Key("reload"), ""), reloadIcon, "Reload").Tooltip("Read dleplugin.yml and the sites again").Clicked() {
			a.reload(pv)
		}
		if iconButton(c, ui.Button(c.Key("build"), ""), buildIcon, "Build").Disabled(a.busy()).Tooltip("Write the plugin into dist/").Clicked() {
			a.build(pv)
		}
		if iconButton(c, ui.PrimaryButton(c.Key("deploy"), ""), deployIcon, "Deploy…").Disabled(a.busy()).Tooltip("Upload the plugin to saved sites").Clicked() {
			a.openDeploy(pv)
		}
	})
	ui.Row(c).Padding(10, 20, 0).Children(func() {
		ui.Tabs(c, &pv.tab, projectTabs...)
	})
	ui.Scroll(c.Key(pv.root)).Grow(1).Padding(16, 20, 20).Gap(16).Children(func() {
		switch pv.tab {
		case tabOverview:
			a.overviewTab(c, pv)
		case tabSites:
			a.sitesTab(c, pv)
		case tabChecks:
			a.checksTab(c, pv)
		}
	})
}

func (a *App) overviewTab(c *ui.Context, pv *projectView) {
	t := c.Theme()
	if pv.err != nil {
		section(c, "The project has errors", "Fix dleplugin.yml and press Reload.", func() {
			errorText(c, pv.err.Error())
		})
	}
	if pv.info == nil {
		return
	}
	info := pv.info
	section(c, "Plugin", "From dleplugin.yml.", func() {
		ui.Form(c, func() {
			value := func(label, v string) {
				ui.Field(c, label, func() {
					if v == "" {
						ui.Text(c, "—").TextColor(t.TextMuted)
					} else {
						ui.Text(c, v).Selectable()
					}
				})
			}
			value("Name", info.Name)
			value("Slug", info.Slug)
			value("Version", info.Version)
			value("Description", info.Description)
			value("Upgrade URL", info.UpgradeURL)
			value("Dependencies", strings.Join(info.Dependencies, ", "))
			value("Source files", itoa(pv.files))
		})
	})
	a.buildOptions(c, pv)
	section(c, "Build artifacts", "Files of dist/, written by Build.", func() {
		a.distList(c, pv)
	})
}

func (a *App) distList(c *ui.Context, pv *projectView) {
	t := c.Theme()
	if len(pv.dist) == 0 {
		ui.Text(c, "Not built yet.").TextColor(t.TextMuted)
		return
	}
	ui.Column(c).Gap(4).Children(func() {
		for _, f := range pv.dist {
			ui.Row(c.Key(f.name)).Gap(12).Children(func() {
				ui.Text(c, f.name).Grow(1).Shrink(1).SingleLine()
				ui.Text(c, formatSize(f.size)).TextColor(t.TextMuted)
			})
		}
	})
	if ui.Button(c.Key("open-dist"), "Open dist folder").AlignSelf(ui.Start).Clicked() {
		a.openPath(filepath.Join(pv.root, "dist"))
	}
}

// buildOptions are the values Build uses instead of dleplugin.yml's.
func (a *App) buildOptions(c *ui.Context, pv *projectView) {
	section(c, "Build options", "Build writes the plugin XML (and the zip of split files, README.html) into dist/; these override dleplugin.yml.", func() {
		ui.Form(c, func() {
			ui.Field(c, "Version", func() {
				ui.TextInput(c, &pv.build.version).Placeholder(placeholder(pv, func() string { return pv.info.Version }, "from dleplugin.yml"))
			})
			ui.Field(c, "Upgrade URL", func() {
				ui.TextInput(c, &pv.build.upgradeURL).Placeholder(placeholder(pv, func() string { return pv.info.UpgradeURL }, "from dleplugin.yml"))
			}).Description("An https link.")
		})
	})
}

// placeholder shows a manifest value in an empty field that overrides it.
func placeholder(pv *projectView, value func() string, fallback string) string {
	if pv.info != nil {
		if v := value(); v != "" {
			return v
		}
	}
	return fallback
}

func (a *App) build(pv *projectView) {
	a.start("Build", pv.root, pv.build.args(), "", func(code int, _ []string) {
		pv.project = loadProject(pv.root)
		if code == 0 {
			a.notify("Plugin built", false)
		}
	})
}

func (a *App) sitesTab(c *ui.Context, pv *projectView) {
	global, project := registryPaths(a.configPath, pv.root)
	section(c, "Deployment sites", "Sites of "+project+" replace global sites ("+global+") of the same host.", func() {
		a.sitesTable(c, &pv.sites, pv.root)
	})
}

func (a *App) globalSitesPage(c *ui.Context) {
	if !a.globalSites.loaded && !a.globalSites.loading {
		a.loadSites(&a.globalSites, "")
	}
	global, _ := registryPaths(a.configPath, "")
	pageHeader(c, "Sites", global, func() {
		if ui.Button(c.Key("reload-sites"), "Reload").Clicked() {
			a.loadSites(&a.globalSites, "")
		}
	})
	ui.Scroll(c).Grow(1).Padding(16, 20, 20).Gap(16).Children(func() {
		section(c, "Deployment sites", "Available to every project; a project's own sites replace them.", func() {
			a.sitesTable(c, &a.globalSites, "")
		})
	})
}

var siteColumns = []ui.TableColumn{
	{Title: "Host", MinWidth: 140},
	{Title: "Login", Width: 120},
	{Title: "Scope", Width: 80},
	{Title: "Password", Width: 130},
}

// sitesTable lists sites with actions; root is the project, "" for the
// global sites.
func (a *App) sitesTable(c *ui.Context, sv *sitesView, root string) {
	t := c.Theme()
	if sv.err != nil {
		errorText(c, sv.err.Error())
	}
	rows := sv.rows
	sv.table.Selected = &sv.selected
	sv.table.Key = func(i int) any { return rows[i].host }
	sv.table.Label = func(i int) string { return rows[i].host }
	edit := ui.Table(c, &sv.table, siteColumns, len(rows), func(row, col int) {
		r := rows[row]
		switch col {
		case 0:
			ui.Text(c, r.host).SingleLine()
		case 1:
			ui.Text(c, r.site.Login).SingleLine()
		case 2:
			ui.Text(c, r.scope).SingleLine()
		case 3:
			ui.Text(c, r.password.String()).SingleLine()
		}
	}).Height(min(float32(len(rows)), 8)*30 + 34).Submitted()
	if len(rows) == 0 && !sv.loading {
		ui.Text(c, "No saved sites.").TextColor(t.TextMuted)
	}
	var chosen *siteRow
	if sv.selected >= 0 && sv.selected < len(rows) {
		chosen = &rows[sv.selected]
	}
	// Global sites shown in a project are edited on the Sites page.
	editable := chosen != nil && (root == "" || chosen.scope == "project")
	ui.Row(c).Gap(8).Children(func() {
		if ui.Button(c.Key("add-site"), "Add site…").Disabled(a.busy()).Clicked() {
			a.openSite(siteForm{global: root == ""})
		}
		if (ui.Button(c.Key("edit-site"), "Edit…").Disabled(!editable || a.busy()).Clicked() || (edit && editable)) && chosen != nil {
			adminPath := chosen.site.AdminPath
			if admin, err := url.Parse(chosen.admin); err == nil {
				adminPath = strings.TrimPrefix(admin.Path, "/")
			}
			a.openSite(siteForm{
				edit: true, global: chosen.scope == "global",
				target: chosen.host, login: chosen.site.Login,
				adminPath: adminPath, passwordEnv: chosen.site.PasswordEnv,
			})
		}
		if ui.Button(c.Key("remove-site"), "Remove…").Disabled(!editable || a.busy()).Clicked() && chosen != nil {
			a.siteRemove, a.siteRemoved = true, *chosen
		}
		if chosen != nil && !editable {
			ui.Text(c, "A global site: edit it on the Sites page.").FontSize(12).TextColor(t.TextMuted)
		}
	})
	if chosen != nil && chosen.admin != "" {
		ui.Text(c, chosen.admin+" · password from "+passwordSource(*chosen)).FontSize(12).TextColor(t.TextMuted).Selectable()
	}
}

func passwordSource(r siteRow) string {
	switch r.password {
	case passwordEnv:
		return "$" + r.env
	case passwordStored:
		return "the credential store"
	}
	return "$" + r.env + " or the credential store (none saved)"
}

func (a *App) openSite(f siteForm) {
	a.siteForm, a.siteErr, a.siteOpen = f, "", true
}

// siteRoot is the project whose sites the dialogs change, "" for the
// global page.
func (a *App) siteRoot() (string, *sitesView) {
	if pv := a.current(); pv != nil {
		return pv.root, &pv.sites
	}
	return "", &a.globalSites
}

// commandDir is where commands about global settings run: a project, or the
// config directory.
func (a *App) commandDir(root string) string {
	if root != "" {
		return root
	}
	return filepath.Dir(a.configPath)
}

func (a *App) saveSite() {
	f := a.siteForm
	if err := f.validate(); err != nil {
		a.siteErr = err.Error()
		return
	}
	root, _ := a.siteRoot()
	args, stdin := f.args()
	a.siteErr = ""
	a.start("Save site", a.commandDir(root), args, stdin, func(code int, lines []string) {
		a.refreshSites()
		if code != 0 {
			a.siteErr = errorLine(lines)
			return
		}
		a.siteOpen = false
		a.notify("Site saved", false)
	})
}

func (a *App) removeSite(r siteRow) {
	root, _ := a.siteRoot()
	a.start("Remove site", a.commandDir(root), removeSiteArgs(r.host, r.scope == "global"), "", func(code int, _ []string) {
		a.refreshSites()
		if code == 0 {
			a.notify("Site removed", false)
		}
	})
}

func (a *App) checksTab(c *ui.Context, pv *projectView) {
	t := c.Theme()
	section(c, "Static analysis", "PHPStan in Docker, with the plugin's dlestatic.yml.", func() {
		ui.Form(c, func() {
			ui.Field(c, "PHP version", func() {
				ui.TextInput(c, &pv.static.phpVersion).Placeholder("the container's PHP, e.g. 8.1").Width(240)
			})
		})
		if ui.PrimaryButton(c.Key("static"), "Run analysis").AlignSelf(ui.Start).Disabled(a.busy()).Clicked() {
			a.start("Static analysis", pv.root, pv.static.args(), "", nil)
		}
	})
	section(c, "Tests", "Pest in Docker against the DLE trial, with a fresh database for every run.", func() {
		ui.Form(c, func() {
			ui.Field(c, "Filter", func() {
				ui.TextInput(c, &pv.test.filter).Placeholder("only the tests matching a pattern").Width(320)
			})
			ui.Fieldset(c, "Options", func() {
				ui.Field(c, "", func() {
					ui.Column(c).Gap(6).Children(func() {
						ui.Checkbox(c, &pv.test.updateSnapshots, "Update snapshots")
						ui.Checkbox(c, &pv.test.coverage, "Code coverage report")
						ui.Checkbox(c, &pv.test.refresh, "Download and install the DLE trial again")
					})
				})
			})
		})
		if ui.PrimaryButton(c.Key("test"), "Run tests").AlignSelf(ui.Start).Disabled(a.busy()).Clicked() {
			a.start("Tests", pv.root, pv.test.args(), "", nil)
		}
	})
	ps := a.settings.Project(pv.root)
	var dirs []string
	if ps != nil {
		dirs = ps.CheckDirs
	}
	section(c, "Patch check", "Checks that the plugin's file patches apply to DLE installations (directories with engine/inc/include/init.php).", func() {
		if len(dirs) == 0 {
			ui.Text(c, "Add the DLE directories to check against.").TextColor(t.TextMuted)
		}
		ui.Column(c).Gap(4).Children(func() {
			for i, dir := range dirs {
				ui.Row(c.Key(dir)).Gap(8).Children(func() {
					ui.Text(c, dir).Grow(1).Shrink(1).SingleLine().Selectable()
					if ui.Button(c.Key("remove-dir"), "Remove").Clicked() {
						ps.CheckDirs = slices.Delete(slices.Clone(ps.CheckDirs), i, i+1)
						a.saveSettings()
					}
				})
			}
		})
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c.Key("add-dir"), "Add DLE directory…").Disabled(ps == nil).Clicked() {
				a.chooseCheckDir(pv)
			}
			if ui.PrimaryButton(c.Key("check"), "Run check").Disabled(a.busy() || len(dirs) == 0).Clicked() {
				a.start("Patch check", pv.root, checkArgs(dirs), "", nil)
			}
		})
	})
}

func (a *App) chooseCheckDir(pv *projectView) {
	a.background(func() {
		dir, err := a.desk.chooseDirectory("DLE directory")
		a.post(func() {
			ps := a.settings.Project(pv.root)
			switch {
			case err != nil:
				a.notify(err.Error(), true)
			case dir == "" || ps == nil:
			case slices.Contains(ps.CheckDirs, dir):
			default:
				ps.CheckDirs = append(ps.CheckDirs, dir)
				a.saveSettings()
			}
		})
	})
}
