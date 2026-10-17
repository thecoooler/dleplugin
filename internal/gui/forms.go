package gui

import (
	"errors"
	"strings"
)

// Forms hold what the user typed; args turns them into the CLI arguments of
// the command they run, so the GUI does what the documented commands do.

// buildForm is the Build tab.
type buildForm struct {
	version, upgradeURL string
}

func (f *buildForm) args() []string {
	args := []string{"build"}
	if v := strings.TrimSpace(f.version); v != "" {
		args = append(args, "--version", v)
	}
	if u := strings.TrimSpace(f.upgradeURL); u != "" {
		args = append(args, "--upgrade-url", u)
	}
	return args
}

// customSite is the deploy target option for a site that is not saved.
const customSite = "Other site…"

// deployForm is the Deploy tab.
type deployForm struct {
	// site is a saved host, or customSite for target and login.
	site          string
	target, login string
	// source is 0 to build the project, 1 to upload file.
	source              int
	file                string
	version, upgradeURL string
	password            string
	remember            bool
	allowNew, dryRun    bool
	insecure            bool
}

func (f *deployForm) custom() bool { return f.site == customSite }

// validate reports what is missing before an upload; sites are the saved
// ones.
func (f *deployForm) validate(sites []siteRow) error {
	switch {
	case f.site == "":
		return errors.New("Choose a site.")
	case f.custom() && strings.TrimSpace(f.target) == "":
		return errors.New("Enter the site host or admin URL.")
	case f.custom() && strings.TrimSpace(f.login) == "":
		return errors.New("Enter the admin login.")
	case f.source == 1 && strings.TrimSpace(f.file) == "":
		return errors.New("Choose a plugin file.")
	}
	if !f.custom() {
		if _, ok := findSite(sites, f.site); !ok {
			return errors.New("The chosen site is no longer saved.")
		}
	}
	return nil
}

// args returns the arguments of deploy upload and its standard input.
func (f *deployForm) args() ([]string, string) {
	args := []string{"deploy", "upload"}
	if f.custom() {
		args = append(args, strings.TrimSpace(f.target), "--login", strings.TrimSpace(f.login))
	} else {
		args = append(args, f.site)
	}
	if f.source == 1 {
		args = append(args, "--file", strings.TrimSpace(f.file))
	} else {
		if v := strings.TrimSpace(f.version); v != "" {
			args = append(args, "--version", v)
		}
		if u := strings.TrimSpace(f.upgradeURL); u != "" {
			args = append(args, "--upgrade-url", u)
		}
	}
	stdin := ""
	if f.password != "" {
		args = append(args, "--password-stdin")
		stdin = f.password
	}
	if f.allowNew {
		args = append(args, "--new")
	}
	if f.dryRun {
		args = append(args, "--dry-run")
	}
	if f.insecure {
		args = append(args, "--insecure")
	}
	return args, stdin
}

// siteForm is the dialog adding or editing a saved site.
type siteForm struct {
	edit, global bool
	// target is the host or admin URL; on edit, the saved host.
	target, login, adminPath string
	password, passwordEnv    string
	insecure                 bool
}

func (f *siteForm) validate() error {
	switch {
	case strings.TrimSpace(f.target) == "":
		return errors.New("Enter the site host or admin URL.")
	case strings.TrimSpace(f.login) == "":
		return errors.New("Enter the admin login.")
	case !f.edit && f.password == "" && strings.TrimSpace(f.passwordEnv) == "":
		return errors.New("Enter the password, or the environment variable holding it.")
	}
	return nil
}

// args returns the arguments of deploy sites add or edit and its standard
// input. The login is checked against the site before it is saved.
func (f *siteForm) args() ([]string, string) {
	verb := "add"
	if f.edit {
		verb = "edit"
	}
	args := []string{"deploy", "sites", verb, strings.TrimSpace(f.target), "--login", strings.TrimSpace(f.login)}
	// Edit always passes them, so that clearing a field clears the setting.
	if p := strings.TrimSpace(f.adminPath); p != "" || f.edit {
		args = append(args, "--admin-path", p)
	}
	if env := strings.TrimSpace(f.passwordEnv); env != "" || f.edit {
		args = append(args, "--password-env", env)
	}
	stdin := ""
	if f.password != "" {
		args = append(args, "--password-stdin")
		stdin = f.password
	}
	if f.insecure {
		args = append(args, "--insecure")
	}
	if f.global {
		args = append(args, "--global")
	}
	return args, stdin
}

// removeSiteArgs returns the arguments removing a saved site.
func removeSiteArgs(host string, global bool) []string {
	args := []string{"deploy", "sites", "remove", host}
	if global {
		args = append(args, "--global")
	}
	return args
}

// staticForm runs PHPStan.
type staticForm struct{ phpVersion string }

func (f *staticForm) args() []string {
	args := []string{"static"}
	if v := strings.TrimSpace(f.phpVersion); v != "" {
		args = append(args, "--php-version", v)
	}
	return args
}

// testForm runs the Pest tests.
type testForm struct {
	filter                    string
	updateSnapshots, coverage bool
	refresh                   bool
}

func (f *testForm) args() []string {
	args := []string{"test"}
	if v := strings.TrimSpace(f.filter); v != "" {
		args = append(args, "--filter", v)
	}
	if f.updateSnapshots {
		args = append(args, "--update-snapshots")
	}
	if f.coverage {
		args = append(args, "--coverage")
	}
	if f.refresh {
		args = append(args, "--refresh-dle")
	}
	return args
}

// checkArgs returns the arguments checking the project's patches against
// DLE directories.
func checkArgs(dirs []string) []string { return append([]string{"check"}, dirs...) }

// initForm is the dialog creating a plugin project.
type initForm struct {
	dir, slug, name, description, namespace string
}

// commandLine shows args as the command a user would type.
func commandLine(args []string) string {
	parts := []string{"dleplugin"}
	for _, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\"'\\$`") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}
