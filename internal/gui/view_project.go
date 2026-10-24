package gui

import (
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

var projectTabs = []string{"Overview", "Build", "Deploy", "Sites", "Checks"}

const (
	tabOverview = iota
	tabBuild
	tabDeploy
	tabSites
	tabChecks
)

func (a *App) projectPage(c *ui.Context, pv *projectView) {
	if !pv.sites.loaded && !pv.sites.loading {
		a.loadSites(&pv.sites, pv.root)
	}
	home, _ := os.UserHomeDir()
	pageHeader(c, pv.title(), shortPath(pv.root, home), func() {
		if ui.Button(c.Key("open-folder"), "Open folder").Clicked() {
			a.openPath(pv.root)
		}
		if ui.Button(c.Key("reload"), "Reload").Tooltip("Read dleplugin.yml and the sites again").Clicked() {
			a.reload(pv)
		}
		if ui.Button(c.Key("remove-project"), "Remove from list…").Tooltip("Remove from the list; the files stay").Clicked() {
			a.removeAsk = true
		}
	})
	ui.Row(c).Padding(10, 20, 0).Children(func() {
		ui.Tabs(c, &pv.tab, projectTabs...)
	})
	ui.Scroll(c.Key(pv.root)).Grow(1).Padding(16, 20, 20).Gap(16).Children(func() {
		switch pv.tab {
		case tabOverview:
			a.overviewTab(c, pv)
		case tabBuild:
			a.buildTab(c, pv)
		case tabDeploy:
			a.deployTab(c, pv)
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

func (a *App) buildTab(c *ui.Context, pv *projectView) {
	section(c, "Build", "Writes the plugin XML (and the zip of split files, README.html) into dist/.", func() {
		ui.Form(c, func() {
			ui.Field(c, "Version", func() {
				ui.TextInput(c, &pv.build.version).Placeholder(placeholder(pv, func() string { return pv.info.Version }, "from dleplugin.yml"))
			}).Description("Overrides version of dleplugin.yml.")
			ui.Field(c, "Upgrade URL", func() {
				ui.TextInput(c, &pv.build.upgradeURL).Placeholder(placeholder(pv, func() string { return pv.info.UpgradeURL }, "from dleplugin.yml"))
			}).Description("An https link; overrides upgrade_url of dleplugin.yml.")
		})
		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c.Key("build"), "Build plugin").Disabled(a.busy()).Clicked() {
				a.build(pv)
			}
		})
	})
	section(c, "Artifacts", "", func() { a.distList(c, pv) })
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

func (a *App) deployTab(c *ui.Context, pv *projectView) {
	t := c.Theme()
	f := &pv.deploy
	options := append(hosts(pv.sites.rows), customSite)
	if f.site != "" && !slices.Contains(options, f.site) && pv.sites.loaded {
		f.site = ""
		f.password, f.remember = "", false
	}
	if f.site == "" && len(options) > 1 {
		f.site = options[0]
		f.password, f.remember = "", false
	}
	site, saved := findSite(pv.sites.rows, f.site)
	section(c, "Deploy to a site", "Logs in to the DLE admin panel and installs or updates the plugin.", func() {
		ui.Form(c, func() {
			ui.Field(c, "Site", func() {
				ui.Row(c).Gap(8).Children(func() {
					if ui.Select(c, &f.site, options).Label("Site").Changed() {
						f.password, f.remember = "", false
						site, saved = findSite(pv.sites.rows, f.site)
						a.rememberDeploySite(pv)
					}
					if pv.sites.loading {
						ui.Spinner(c).Size(14, 14)
					}
				})
			}).Description(siteDescription(site, saved))
			if f.custom() {
				ui.Field(c, "Host or admin URL", func() {
					if ui.TextInput(c, &f.target).Placeholder("example.com or https://example.com/admin.php").Changed() {
						f.password, f.remember = "", false
					}
				})
				ui.Field(c, "Login", func() {
					if ui.TextInput(c, &f.login).Changed() {
						f.password, f.remember = "", false
					}
				})
			}
			ui.Field(c, "Plugin", func() {
				ui.Segmented(c, &f.source, "Build the project", "Ready file")
			})
			if f.source == 1 {
				ui.Field(c, "File", func() {
					ui.Row(c).Gap(8).Children(func() {
						ui.TextInput(c, &f.file).Placeholder(".xml or .zip").Grow(1)
						if ui.Button(c.Key("choose-file"), "Choose…").Clicked() {
							a.chooseDeployFile(pv)
						}
					})
				})
			} else {
				ui.Field(c, "Version", func() {
					ui.TextInput(c, &f.version).Placeholder(placeholder(pv, func() string { return pv.info.Version }, "from dleplugin.yml"))
				})
				ui.Field(c, "Upgrade URL", func() {
					ui.TextInput(c, &f.upgradeURL).Placeholder(placeholder(pv, func() string { return pv.info.UpgradeURL }, "from dleplugin.yml"))
				})
			}
			ui.Field(c, "Password", func() {
				ui.TextInput(c, &f.password).Password().Placeholder(passwordPlaceholder(site, saved))
			})
			ui.Fieldset(c, "Options", func() {
				ui.Field(c, "", func() {
					ui.Column(c).Gap(6).Children(func() {
						if saved {
							ui.Checkbox(c, &f.remember, "Remember the password in the credential store")
						}
						ui.Checkbox(c, &f.allowNew, "Install the plugin when it is not installed")
						ui.Checkbox(c, &f.dryRun, "Dry run: log in and look the plugin up without uploading")
						ui.Checkbox(c, &f.insecure, "Skip TLS certificate verification")
					})
				})
			})
		})
		err := f.validate(pv.sites.rows)
		if err == nil && !f.custom() && saved && f.password == "" && site.password != passwordEnv && site.password != passwordStored {
			err = errString("Enter the password: none is saved for " + site.site.Login + "@" + site.host + ".")
		}
		if err == nil && f.custom() && f.password == "" && os.Getenv(defaultPasswordEnv()) == "" {
			err = errString("Enter the password.")
		}
		ui.Row(c).Gap(12).Children(func() {
			label := "Upload"
			if f.dryRun {
				label = "Dry run"
			}
			if ui.PrimaryButton(c.Key("deploy"), label).Disabled(a.busy() || err != nil).Clicked() {
				a.deployUpload(pv)
			}
			if err != nil && f.site != "" {
				ui.Text(c, err.Error()).TextColor(t.TextMuted).FontSize(12).Shrink(1)
			}
		})
	})
}

type errString string

func (e errString) Error() string { return string(e) }

func siteDescription(site siteRow, saved bool) string {
	if !saved {
		return "A site that is not saved; add it on the Sites tab to keep its settings."
	}
	return site.admin + " · " + site.site.Login + " · " + site.scope
}

func passwordPlaceholder(site siteRow, saved bool) string {
	switch {
	case !saved:
		return "Admin password, or $" + defaultPasswordEnv()
	case site.password == passwordStored:
		return "Saved in the credential store"
	case site.password == passwordEnv:
		return "From $" + site.env
	}
	return "Admin password"
}

func (a *App) rememberDeploySite(pv *projectView) {
	ps := a.settings.Project(pv.root)
	if ps == nil || pv.deploy.custom() || ps.DeploySite == pv.deploy.site {
		return
	}
	ps.DeploySite = pv.deploy.site
	a.saveSettings()
}

func (a *App) chooseDeployFile(pv *projectView) {
	a.background(func() {
		path, err := a.desk.chooseFile("Plugin file")
		a.post(func() {
			if err != nil {
				a.notify(err.Error(), true)
			} else if path != "" {
				pv.deploy.file = path
			}
		})
	})
}

func (a *App) deployUpload(pv *projectView) {
	f := pv.deploy
	args, stdin := f.args()
	site, saved := findSite(pv.sites.rows, f.site)
	title := "Deploy"
	if f.dryRun {
		title = "Dry run"
	}
	a.start(title, pv.root, args, stdin, func(code int, _ []string) {
		pv.project = loadProject(pv.root)
		if code != 0 {
			return
		}
		pv.deploy.password = ""
		if f.remember && saved && f.password != "" {
			a.savePassword(site.host, site.site.Login, f.password)
		}
		a.notify(title+" finished", false)
	})
}

// savePassword keeps a password that worked in the credential store.
func (a *App) savePassword(host, login, password string) {
	a.background(func() {
		fallback, err := savePassword(a.configPath, host, login, password)
		a.post(func() {
			if err != nil {
				a.notify(err.Error(), true)
				return
			}
			if fallback != nil {
				a.notify(fallback.Error()+"; password saved to the credentials file instead", false)
			}
			a.refreshSites()
		})
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
	pageHeader(c, "Global sites", global, func() {
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
	// Global sites shown in a project are edited on the Global sites page.
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
			ui.Text(c, "A global site: edit it on the Global sites page.").FontSize(12).TextColor(t.TextMuted)
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
