package gui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// openDeploy opens the deploy dialog of a project, with its sites read
// again.
func (a *App) openDeploy(pv *projectView) {
	pv.deploy.clearPasswords()
	a.loadSites(&pv.sites, pv.root)
	a.deployRoot, a.deployOpen = pv.root, true
}

// closeDeploy closes the deploy dialog, forgetting the typed passwords.
func (a *App) closeDeploy() {
	if pv := a.projects[a.deployRoot]; pv != nil {
		pv.deploy.clearPasswords()
	}
	a.deployOpen = false
}

func (a *App) deployDialog(c *ui.Context) {
	pv := a.projects[a.deployRoot]
	if pv == nil {
		a.deployOpen = false
		return
	}
	t := c.Theme()
	f := &pv.deploy
	rows := pv.sites.rows
	if len(f.sites) == 0 && len(rows) == 1 && pv.sites.loaded {
		f.choose(rows[0], true)
	}
	ui.Column(c).Gap(14).Children(func() {
		dialogTitle(c, "Deploy "+pv.title(), "Logs in to the admin panel of each chosen site and installs or updates the plugin there, one site after another.")
		ui.Column(c).Gap(8).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, "Sites").Bold()
				if pv.sites.loading {
					ui.Spinner(c).Size(14, 14)
				}
				ui.Spacer(c)
				if len(rows) > 1 {
					all := len(f.targets(rows)) == len(rows)
					label := "Choose all"
					if all {
						label = "Choose none"
					}
					if ui.Button(c.Key("deploy-all"), label).Clicked() {
						for _, r := range rows {
							f.choose(r, !all)
						}
					}
				}
				if ui.Button(c.Key("deploy-add-site"), "Add site…").Disabled(a.busy()).Tooltip("Save a new site; this dialog opens again after it").Clicked() {
					a.deployOpen, a.siteReturn = false, true
					a.openSite(siteForm{})
				}
			})
			if pv.sites.err != nil {
				errorText(c, pv.sites.err.Error())
			}
			if len(rows) == 0 && !pv.sites.loading {
				ui.Text(c, "No saved sites yet: add one to deploy to it.").TextColor(t.TextMuted)
			}
			ui.Scroll(c.Key("deploy-sites")).MaxHeight(260).Gap(6).Children(func() {
				for _, r := range rows {
					a.deploySiteRow(c, f, r)
				}
			})
		})
		ui.Form(c, func() {
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
			ui.Fieldset(c, "Options", func() {
				ui.Field(c, "", func() {
					ui.Column(c).Gap(6).Children(func() {
						if typedPasswords(f, rows) {
							ui.Checkbox(c, &f.remember, "Remember the typed passwords in the credential store")
						}
						ui.Checkbox(c, &f.allowNew, "Install the plugin where it is not installed")
						ui.Checkbox(c, &f.dryRun, "Dry run: log in and look the plugin up without uploading")
						ui.Checkbox(c, &f.insecure, "Skip TLS certificate verification")
					})
				})
			})
		})
		err := f.validate(rows)
		dialogButtons(c, func() {
			if err != nil {
				ui.Text(c, err.Error()).FontSize(12).TextColor(t.TextMuted).Shrink(1)
				ui.Spacer(c)
			}
			if ui.Button(c.Key("deploy-cancel"), "Cancel").Clicked() {
				a.closeDeploy()
			}
			if ui.PrimaryButton(c.Key("deploy-start"), deployLabel(f, len(f.targets(rows)))).Disabled(a.busy() || err != nil).Clicked() {
				a.deploy(pv)
			}
		})
	})
}

// deploySiteRow is a saved site to choose, with the password to use when
// none is saved.
func (a *App) deploySiteRow(c *ui.Context, f *deployForm, r siteRow) {
	t := c.Theme()
	on := f.chosen(r.host)
	ui.Column(c.Key(r.host)).Gap(6).Padding(8, 10).Radius(t.Radius).Border(1, t.Border).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			if ui.Checkbox(c, &on, r.host).Changed() {
				f.choose(r, on)
			}
			ui.Spacer(c)
			ui.Text(c, r.password.String()).FontSize(12).TextColor(t.TextMuted)
		})
		ui.Text(c, r.admin+" · "+r.site.Login+" · "+r.scope+" site").FontSize(12).TextColor(t.TextMuted).SingleLine()
		if on {
			p := f.password(r)
			if ui.TextInput(c, &p).Password().Placeholder(deployPasswordPlaceholder(r)).Label("Password for " + passwordKey(r)).Changed() {
				f.setPassword(r, p)
			}
		}
	})
}

func deployPasswordPlaceholder(r siteRow) string {
	switch r.password {
	case passwordStored:
		return "Saved; type to use another"
	case passwordEnv:
		return "From $" + r.env + "; type to use another"
	}
	return "Admin password of " + passwordKey(r)
}

// typedPasswords reports whether a password is typed for a chosen site.
func typedPasswords(f *deployForm, rows []siteRow) bool {
	for _, r := range f.targets(rows) {
		if f.password(r) != "" {
			return true
		}
	}
	return false
}

func deployLabel(f *deployForm, n int) string {
	verb := "Upload"
	if f.dryRun {
		verb = "Dry run"
	}
	switch n {
	case 0:
		return verb
	case 1:
		return verb + " to 1 site"
	}
	return fmt.Sprintf("%s to %d sites", verb, n)
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

// deploy uploads the plugin to the chosen sites, one deploy upload after
// another. A failed site does not stop the others; canceling stops them all.
func (a *App) deploy(pv *projectView) {
	rows := pv.sites.rows
	if a.busy() || pv.deploy.validate(rows) != nil {
		return
	}
	f := pv.deploy
	targets := f.targets(rows)
	a.rememberDeploySites(pv, targets)
	pv.deploy.clearPasswords()
	a.deployOpen = false
	title := "Deploy"
	if f.dryRun {
		title = "Dry run"
	}
	b := &batch{}
	a.batch = b
	var failed []string
	var next func(i int)
	next = func(i int) {
		if b.canceled || i == len(targets) {
			a.batch = nil
			pv.project = loadProject(pv.root)
			switch {
			case b.canceled:
			case len(failed) > 0:
				a.notify(fmt.Sprintf("%s failed for %s; see the console", title, strings.Join(failed, ", ")), true)
			default:
				a.notify(fmt.Sprintf("%s finished: %s", title, deployCount(len(targets))), false)
			}
			return
		}
		r := targets[i]
		args, stdin := f.args(r)
		label := title + " to " + r.host
		if len(targets) > 1 {
			label += fmt.Sprintf(" (%d/%d)", i+1, len(targets))
		}
		a.start(label, pv.root, args, stdin, func(code int, _ []string) {
			switch {
			case code == 0 && f.remember && stdin != "":
				a.savePassword(r.host, r.site.Login, stdin)
			case code != 0 && !b.canceled:
				failed = append(failed, r.host)
			}
			next(i + 1)
		})
	}
	next(0)
}

func deployCount(n int) string {
	if n == 1 {
		return "1 site"
	}
	return fmt.Sprintf("%d sites", n)
}

// rememberDeploySites keeps the chosen sites for the next deploy.
func (a *App) rememberDeploySites(pv *projectView, targets []siteRow) {
	ps := a.settings.Project(pv.root)
	if ps == nil {
		return
	}
	var sites []string
	for _, r := range targets {
		sites = append(sites, r.host)
	}
	if strings.Join(ps.DeploySites, "\n") == strings.Join(sites, "\n") {
		return
	}
	ps.DeploySites = sites
	a.saveSettings()
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
