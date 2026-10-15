package dle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/thecoooler/dleplugin/internal/manifest"
)

// Registry keeps the plugin record in the DLE database.
type Registry interface {
	// Add inserts or updates the plugin and its file patches.
	Add(ctx context.Context, info *manifest.Info) error
}

// OpenMySQL opens a connection pool for cfg. The connection is established
// lazily; use db.PingContext to verify it.
func OpenMySQL(cfg DBConfig) (*sql.DB, error) {
	db, err := sql.Open("mysql", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	return db, nil
}

// SQLRegistry is the MySQL implementation of Registry.
type SQLRegistry struct {
	db  *sql.DB
	cfg DBConfig
}

// NewSQLRegistry returns a Registry backed by db.
func NewSQLRegistry(db *sql.DB, cfg DBConfig) *SQLRegistry {
	return &SQLRegistry{db: db, cfg: cfg}
}

var _ Registry = (*SQLRegistry)(nil)

// Add registers the plugin, or updates it when a plugin with the same name exists.
func (r *SQLRegistry) Add(ctx context.Context, info *manifest.Info) error {
	id, found, err := r.find(ctx, info.Name)
	if err != nil {
		return err
	}
	if found {
		if _, err := r.db.ExecContext(ctx,
			"UPDATE "+r.table("plugins")+" SET description=?, mysqlinstall=?, mysqlupgrade=?, mysqlenable=?, mysqldisable=?, mysqldelete=? WHERE id=?",
			info.Description, info.MySQL.Install, info.MySQL.Upgrade, info.MySQL.Enable, info.MySQL.Disable, info.MySQL.Delete, id,
		); err != nil {
			return fmt.Errorf("update plugin: %w", err)
		}
		if err := r.removeFiles(ctx, id); err != nil {
			return err
		}
		if err := r.execHook(ctx, info.MySQL.Upgrade); err != nil {
			return err
		}
	} else {
		res, err := r.db.ExecContext(ctx,
			"INSERT INTO "+r.table("plugins")+" (active, name, description, mysqlinstall, mysqlupgrade, mysqlenable, mysqldisable, mysqldelete) VALUES (1, ?, ?, ?, ?, ?, ?, ?)",
			info.Name, info.Description, info.MySQL.Install, info.MySQL.Upgrade, info.MySQL.Enable, info.MySQL.Disable, info.MySQL.Delete,
		)
		if err != nil {
			return fmt.Errorf("insert plugin: %w", err)
		}
		id, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("plugin id: %w", err)
		}
		if err := r.execHook(ctx, info.MySQL.Install); err != nil {
			return err
		}
		if err := r.execHook(ctx, info.MySQL.Enable); err != nil {
			return err
		}
	}

	if info.Files == nil {
		return nil
	}
	for _, name := range info.Files.Keys() {
		patch, ok := info.Files.Get(name)
		if !ok || patch == nil {
			continue
		}
		for _, a := range patch.Actions {
			if _, err := r.db.ExecContext(ctx,
				"INSERT INTO "+r.table("plugins_files")+" (active, plugin_id, file, action, searchcode, replacecode) VALUES (1, ?, ?, ?, ?, ?)",
				id, name, a.Name, a.Search, a.Replace,
			); err != nil {
				return fmt.Errorf("insert plugin file %q: %w", name, err)
			}
		}
	}
	return nil
}

// table returns the prefixed name of a DLE table.
func (r *SQLRegistry) table(name string) string { return r.cfg.Prefix + "_" + name }

func (r *SQLRegistry) find(ctx context.Context, name string) (int64, bool, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, "SELECT id FROM "+r.table("plugins")+" WHERE name = ? LIMIT 1", name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("find plugin: %w", err)
	}
	return id, true, nil
}

func (r *SQLRegistry) removeFiles(ctx context.Context, id int64) error {
	if _, err := r.db.ExecContext(ctx, "DELETE FROM "+r.table("plugins_files")+" WHERE plugin_id = ?", id); err != nil {
		return fmt.Errorf("delete plugin files: %w", err)
	}
	return nil
}

func (r *SQLRegistry) execHook(ctx context.Context, query string) error {
	if query == "" {
		return nil
	}
	query = strings.NewReplacer(
		"{prefix}", r.cfg.Prefix,
		"{userprefix}", r.cfg.UserPrefix,
		"{engine}", "InnoDB",
	).Replace(query)
	if _, err := r.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("MySQL query execution failed\n---\nQuery: %s\nMessage: %w", query, err)
	}
	return nil
}
