// Package dle connects plugin projects to a local DataLife Engine
// installation: it places the plugin files, registers the plugin in the DLE
// database and clears the DLE cache.
package dle

import (
	"net"
	"strings"

	"github.com/go-sql-driver/mysql"
)

// DBConfig is the database configuration of a DLE installation.
type DBConfig struct {
	Host       string
	Name       string
	User       string
	Pass       string
	Prefix     string
	UserPrefix string
}

// DSN returns a go-sql-driver/mysql data source name. Host may be "host" or
// "host:port" (default port 3306).
func (c DBConfig) DSN() string {
	cfg := mysql.NewConfig()
	cfg.User = c.User
	cfg.Passwd = c.Pass
	cfg.DBName = c.Name
	cfg.MultiStatements = true
	cfg.ParseTime = false
	cfg.Params = map[string]string{"charset": "utf8mb4"}
	cfg.Net = "tcp"
	if h, p, err := net.SplitHostPort(c.Host); err == nil && p != "" {
		cfg.Addr = net.JoinHostPort(h, p)
	} else {
		cfg.Addr = net.JoinHostPort(strings.Trim(c.Host, "[]"), "3306")
	}
	return cfg.FormatDSN()
}
