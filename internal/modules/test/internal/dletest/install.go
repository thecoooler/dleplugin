package dletest

import (
	"context"
	"os"
	"path/filepath"

	"github.com/thecoooler/dleplugin/internal/dleinstall"
)

// Database settings of the test installation. The database container is
// reachable as "db" inside the test network.
const (
	dbHost   = "db"
	dbName   = "dle"
	dbUser   = "dle"
	dbPass   = "dle"
	dbPrefix = "dle"
)

// Administrator created by the installer.
const (
	adminName  = "admin"
	adminPass  = "admin12345"
	adminEmail = "admin@example.com"
)

// Install runs the DLE web installer served at siteURL (the site root) for
// the distribution unpacked in site.
func Install(ctx context.Context, siteURL, site string) error {
	return dleinstall.Run(ctx, dleinstall.Options{
		URL: siteURL, DBHost: dbHost, DBName: dbName, DBUser: dbUser, DBPassword: dbPass, Prefix: dbPrefix,
		Login: adminName, Password: adminPass, Email: adminEmail,
		// DLE 11.0–14.2 do not delete the installer themselves.
		RemoveInstaller: func() error { return os.Remove(filepath.Join(site, "install.php")) },
	}, nil)
}
