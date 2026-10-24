package gui

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/config"
	"github.com/thecoooler/dleplugin/internal/deploysites"
)

// Main runs the desktop app with the CLI modules it drives; it returns when
// the app quits.
func Main(modules ...app.Module) error {
	extendPath()
	configPath, err := config.DefaultPath()
	if err != nil {
		return err
	}
	runner := &Runner{ConfigPath: configPath, Modules: modules}
	desk := &mygoDesktop{}
	a := newApp(runner.Run, desk, configPath, app.Version)

	mygo.App.SetName("dleplugin")
	mygo.App.SetVersion(app.Version)
	mygo.App.WhenReady(func() {
		desk.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "DLE Plugin",
			Width:     1120,
			Height:    760,
			MinWidth:  820,
			MinHeight: 560,
			Content:   ui.View(a.view),
		})
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.cancelJob() })
	err = mygo.App.Run()
	if !a.shutdown(time.Minute) {
		return errors.Join(err, errors.New("timed out waiting for command cleanup"))
	}
	return err
}

// mygoDesktop is the desktop through MyGo.
type mygoDesktop struct{ win *mygo.Window }

func (d *mygoDesktop) chooseDirectory(title string) (string, error) {
	return d.open(mygo.OpenDialogOptions{Parent: d.win, Title: title, Directory: true, CreateDirectories: true})
}

func (d *mygoDesktop) chooseFile(title string) (string, error) {
	return d.open(mygo.OpenDialogOptions{Parent: d.win, Title: title, Filters: []mygo.FileFilter{{Name: "DLE plugin", Extensions: []string{"xml", "zip"}}}})
}

func (d *mygoDesktop) open(o mygo.OpenDialogOptions) (string, error) {
	paths, err := mygo.Dialog.Open(o)
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

func (d *mygoDesktop) openPath(path string) error { return mygo.Shell.OpenPath(path) }

func (d *mygoDesktop) wake() {
	if d.win != nil {
		d.win.Invalidate()
	}
}

// extendPath adds the usual directories of docker, git and php to PATH: an
// app started from the Dock or a desktop menu does not get the shell's PATH.
func extendPath() {
	dirs := filepath.SplitList(os.Getenv("PATH"))
	var extra []string
	switch runtime.GOOS {
	case "darwin":
		extra = []string{"/usr/local/bin", "/opt/homebrew/bin", "/Applications/Docker.app/Contents/Resources/bin"}
	case "linux":
		extra = []string{"/usr/local/bin", "/usr/bin", "/snap/bin"}
	}
	for _, dir := range extra {
		if !slices.Contains(dirs, dir) {
			if st, err := os.Stat(dir); err == nil && st.IsDir() {
				dirs = append(dirs, dir)
			}
		}
	}
	os.Setenv("PATH", strings.Join(dirs, string(os.PathListSeparator)))
}

func defaultPasswordEnv() string { return deploysites.DefaultPasswordEnv }

// savePassword keeps a password in the credential store of the CLI. A
// non-nil fallback means the OS store failed and the file was used.
func savePassword(configPath, host, login, password string) (fallback, err error) {
	c, err := credentials(configPath)
	if err != nil {
		return nil, err
	}
	_, fallback, err = c.SavePassword(host, login, password)
	return fallback, err
}

func itoa(n int) string { return strconv.Itoa(n) }
