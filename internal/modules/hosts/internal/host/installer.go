package host

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/dist"
	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
)

// Explicit --php overrides automatic selection. A configured preference is used
// only within the compatible runtime family, so existing default_php: 8.5 does
// not force old releases onto PHP 8, nor default_php: 7.4 onto DLE 16.1+.
func selectPHP(version, requested, preferred string) string {
	if requested != "" && requested != "auto" {
		return requested
	}
	if requested == "auto" {
		preferred = ""
	}
	modern := !dist.VersionLess(version, "16.1")
	if phpVersionRE.MatchString(preferred) && !dist.VersionLess(preferred, minimumInstallerPHP(version)) &&
		(modern && !dist.VersionLess(preferred, "8.0") || !modern && dist.VersionLess(preferred, "8.0")) {
		return preferred
	}
	if modern {
		return "8.5"
	}
	return "7.4"
}

var phpVersionRE = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

func minimumInstallerPHP(version string) string {
	switch {
	case !dist.VersionLess(version, "16.1"):
		return "8.0"
	case !dist.VersionLess(version, "16.0"):
		return "7.4"
	case !dist.VersionLess(version, "15.0"):
		return "7.2"
	case !dist.VersionLess(version, "14.3"):
		return "7.0"
	case !dist.VersionLess(version, "12.1"):
		return "5.4"
	case !dist.VersionLess(version, "11.2"):
		return "5.3.7"
	default:
		return "5.3"
	}
}

var skinConfigRE = regexp.MustCompile(`['"]skin['"]\s*=>\s*['"]([a-zA-Z0-9_-]+)['"]`)
var altURLConfigRE = regexp.MustCompile(`(['"]allow_alt_url['"]\s*=>\s*)['"]0['"]`)

func finishInstallation(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "engine/data/config.php"))
	if err != nil {
		return err
	}
	skin := skinConfigRE.FindSubmatch(data)
	if skin == nil {
		return fmt.Errorf("the installer did not save the template name")
	}
	for _, file := range []string{"engine/data/dbconfig.php", "templates/" + string(skin[1]) + "/main.tpl"} {
		if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "install.php")); !os.IsNotExist(err) {
		return fmt.Errorf("the installer did not remove install.php")
	}
	// Older installers leave the template zip; it is not needed by the installed site.
	if err := os.Remove(filepath.Join(dir, "templates/default_templates.zip")); err != nil && !os.IsNotExist(err) {
		return err
	}
	// PHP's built-in server cannot detect Apache rewrite support of the final host.
	return replaceInFile(filepath.Join(dir, "engine/data/config.php"), func(config string) string {
		return altURLConfigRE.ReplaceAllString(config, "${1}'1'")
	})
}

func prepareInstallerFiles(s *stack.Stack, dir string) error {
	if err := os.Chmod(dir, 0o755); err != nil {
		return err
	}
	if os.Getuid() != 0 {
		return nil
	}
	uid, e1 := strconv.Atoi(s.Env["UID"])
	gid, e2 := strconv.Atoi(s.Env["GID"])
	if e1 != nil || e2 != nil {
		return fmt.Errorf("install: UID/GID are not set in .env")
	}
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(path, uid, gid)
	})
}
