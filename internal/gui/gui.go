// Package gui is the desktop app of dleplugin: a native window, drawn by
// MyGo without a webview, that manages plugin projects and runs the CLI's
// commands on them (build, deploy, sites, checks).
//
// The commands run in process through app.Execute with the arguments the
// CLI documents, so the GUI behaves exactly as the CLI does; their output
// goes to the window's console. The CLI binary does not link this package.
package gui

import (
	"context"
	"io"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// runFunc runs a dleplugin command; Runner.Run is the real one.
type runFunc func(ctx context.Context, dir string, args []string, stdin string, out io.Writer) int

// desktop is what the app asks of the system, behind an interface for tests.
type desktop interface {
	// chooseDirectory and chooseFile return "" when the user cancels.
	chooseDirectory(title string) (string, error)
	chooseFile(title string) (string, error)
	openPath(path string) error
	// wake asks the window for a frame, to apply posted changes.
	wake()
}

// maxConsoleLines bounds the console; older lines are dropped.
const maxConsoleLines = 5000

// App is the GUI's state; view builds the window from it on the main thread.
type App struct {
	run          runFunc
	desk         desktop
	configPath   string
	settingsPath string
	version      string

	// pending are changes posted from other goroutines, applied by the
	// next frame; bg counts those goroutines, for tests to wait on.
	mu      sync.Mutex
	pending []func()
	bg      sync.WaitGroup

	settings *Settings
	projects map[string]*projectView
	selected string // a project root, pageSites, pageSettings or "" for none
	open     struct{ projects, general bool }
	notices  []notice

	globalSites sitesView

	console struct {
		lines []string
		list  ui.ListState
	}
	consolePercent float32
	job            *job
	lastJob        *job

	// Dialogs.
	initOpen    bool
	initForm    initForm
	initErr     string
	initAsk     bool
	initAskDir  string
	removeAsk   bool
	siteOpen    bool
	siteForm    siteForm
	siteErr     string
	siteRemove  bool
	siteRemoved siteRow
}

// Sidebar pages that are not projects.
const (
	pageSites    = "@sites"
	pageSettings = "@settings"
)

// notice is a toast to show, posted from outside the view.
type notice struct {
	text  string
	error bool
}

// projectView is a project in the sidebar with the state of its pages.
type projectView struct {
	*project
	tab    int
	build  buildForm
	deploy deployForm
	static staticForm
	test   testForm
	sites  sitesView
	checks struct {
		list     ui.ListState
		selected int
	}
}

// sitesView is a table of saved sites.
type sitesView struct {
	rows     []siteRow
	err      error
	loading  bool
	loaded   bool
	table    ui.ListState
	selected int
}

// job is a command running, or the last one run.
type job struct {
	title    string
	line     string
	lines    []string
	started  time.Time
	duration time.Duration
	code     int
	running  bool
	cancel   context.CancelFunc
}

// newApp makes the app's state from its settings file.
func newApp(run runFunc, desk desktop, configPath, version string) *App {
	a := &App{
		run:            run,
		desk:           desk,
		configPath:     configPath,
		settingsPath:   filepath.Join(filepath.Dir(configPath), SettingsFilename),
		version:        version,
		projects:       map[string]*projectView{},
		consolePercent: 70,
	}
	a.open.projects, a.open.general = true, true
	a.console.list.FollowEnd = true
	s, err := LoadSettings(a.settingsPath)
	if err != nil {
		a.notify(err.Error(), true)
	}
	a.settings = s
	for _, p := range s.Projects {
		a.projects[p.Path] = a.newProjectView(p.Path)
	}
	if len(s.Projects) > 0 {
		a.selected = s.Projects[0].Path
	}
	return a
}

func (a *App) newProjectView(root string) *projectView {
	pv := &projectView{project: loadProject(root)}
	pv.checks.selected = -1
	pv.sites.selected = -1
	if ps := a.settings.Project(root); ps != nil {
		pv.deploy.site = ps.DeploySite
	}
	return pv
}

// post runs fn on the main thread before the next frame.
func (a *App) post(fn func()) {
	a.mu.Lock()
	a.pending = append(a.pending, fn)
	a.mu.Unlock()
	a.desk.wake()
}

// drain applies posted changes; the view calls it first.
func (a *App) drain() {
	for {
		a.mu.Lock()
		fns := a.pending
		a.pending = nil
		a.mu.Unlock()
		if len(fns) == 0 {
			return
		}
		for _, fn := range fns {
			fn()
		}
	}
}

// background runs fn on a goroutine of its own.
func (a *App) background(fn func()) {
	a.bg.Add(1)
	go func() {
		defer a.bg.Done()
		fn()
	}()
}

// wait waits for the goroutines started by background and applies what
// they posted (tests).
func (a *App) wait() {
	a.bg.Wait()
	a.drain()
}

func (a *App) notify(text string, isError bool) {
	a.notices = append(a.notices, notice{text: text, error: isError})
}

func (a *App) saveSettings() {
	if err := a.settings.Save(a.settingsPath); err != nil {
		a.notify(err.Error(), true)
	}
}

// current returns the selected project, or nil on another page.
func (a *App) current() *projectView { return a.projects[a.selected] }

// busy reports whether a command is running; one runs at a time.
func (a *App) busy() bool { return a.job != nil }

// start runs a dleplugin command in dir, showing its output in the console;
// done runs on the main thread with its exit code and output.
func (a *App) start(title, dir string, args []string, stdin string, done func(code int, lines []string)) {
	if a.busy() {
		a.notify("Another command is running.", true)
		return
	}
	if a.settings.Verbose {
		args = append(args, "--verbose")
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{title: title, line: commandLine(args), started: time.Now(), running: true, cancel: cancel}
	a.job = j
	if len(a.console.lines) > 0 {
		a.appendConsole("")
	}
	a.appendConsole("$ " + j.line)
	out := &lineWriter{emit: func(lines []string) {
		a.post(func() {
			j.lines = append(j.lines, lines...)
			a.appendConsole(lines...)
		})
	}}
	a.background(func() {
		code := a.run(ctx, dir, args, stdin, out)
		out.Flush()
		cancel()
		a.post(func() {
			j.running, j.code, j.duration = false, code, time.Since(j.started)
			a.job, a.lastJob = nil, j
			if ctx.Err() != nil && code != 0 {
				a.appendConsole("⚠ " + title + " canceled")
			} else if code != 0 {
				a.appendConsole("✖ " + title + " failed (exit code " + strconv.Itoa(code) + ")")
			}
			if done != nil {
				done(code, j.lines)
			}
		})
	})
}

// cancelJob cancels the running command.
func (a *App) cancelJob() {
	if a.job != nil {
		a.job.cancel()
	}
}

func (a *App) appendConsole(lines ...string) {
	a.console.lines = append(a.console.lines, lines...)
	if n := len(a.console.lines) - maxConsoleLines; n > 0 {
		a.console.lines = append([]string(nil), a.console.lines[n:]...)
	}
}

// selectProject adds the project at root to the sidebar, or selects it when
// it is there.
func (a *App) selectProject(root string) {
	if a.settings.AddProject(root) {
		a.saveSettings()
	}
	if _, ok := a.projects[root]; !ok {
		a.projects[root] = a.newProjectView(root)
	}
	a.selected = root
}

// removeProject removes the selected project from the sidebar; its files
// stay.
func (a *App) removeProject(root string) {
	a.settings.RemoveProject(root)
	delete(a.projects, root)
	a.saveSettings()
	a.selected = ""
	if len(a.settings.Projects) > 0 {
		a.selected = a.settings.Projects[0].Path
	}
}

// reload reads the project's manifest and sites again.
func (a *App) reload(pv *projectView) {
	pv.project = loadProject(pv.root)
	a.loadSites(&pv.sites, pv.root)
}

// loadSites reads the sites of a project (root) or the global ones (root
// "") off the main thread.
func (a *App) loadSites(sv *sitesView, root string) {
	if sv.loading {
		return
	}
	sv.loading = true
	a.background(func() {
		rows, err := loadSites(a.configPath, root)
		a.post(func() {
			sv.rows, sv.err, sv.loading, sv.loaded = rows, err, false, true
			if sv.selected >= len(sv.rows) {
				sv.selected = len(sv.rows) - 1
			}
		})
	})
}

// addProjectDir adds the project containing dir, or offers to create one
// there.
func (a *App) addProjectDir(dir string) {
	root, err := projectRoot(dir)
	if err != nil {
		a.initAsk, a.initAskDir = true, dir
		return
	}
	a.selectProject(root)
}

// chooseProject asks for a project directory.
func (a *App) chooseProject() {
	a.background(func() {
		dir, err := a.desk.chooseDirectory("Add a plugin project")
		a.post(func() {
			if err != nil {
				a.notify(err.Error(), true)
			} else if dir != "" {
				a.addProjectDir(dir)
			}
		})
	})
}

// openNewProject opens the dialog creating a project in dir.
func (a *App) openNewProject(dir string) {
	a.initForm = initForm{dir: dir}
	if dir != "" {
		a.initForm.slug = filepath.Base(dir)
	}
	a.initErr = ""
	a.initOpen = true
}
