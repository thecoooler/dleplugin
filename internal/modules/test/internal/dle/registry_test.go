package dle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/thecoooler/dleplugin/internal/manifest"
)

func newMock(t *testing.T) (*SQLRegistry, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewSQLRegistry(db, DBConfig{Prefix: "pf", UserPrefix: "up"}), mock
}

func testInfo() *manifest.Info {
	info := manifest.NewInfo()
	info.Name = "demo"
	info.Description = "Demo"
	info.AppendActions("a.php", manifest.Action{Name: "after", Search: "s1", Replace: "r1"}, manifest.Action{Name: "before", Search: "s2", Replace: "r2"})
	info.AppendActions("b.php", manifest.Action{Name: "replace", Search: "s3", Replace: "r3"})
	return info
}

const (
	qSelect  = "SELECT id FROM pf_plugins WHERE name = ? LIMIT 1"
	qUpdate  = "UPDATE pf_plugins SET description=?, mysqlinstall=?, mysqlupgrade=?, mysqlenable=?, mysqldisable=?, mysqldelete=? WHERE id=?"
	qInsert  = "INSERT INTO pf_plugins (active, name, description, mysqlinstall, mysqlupgrade, mysqlenable, mysqldisable, mysqldelete) VALUES (1, ?, ?, ?, ?, ?, ?, ?)"
	qDelFile = "DELETE FROM pf_plugins_files WHERE plugin_id = ?"
	qFile    = "INSERT INTO pf_plugins_files (active, plugin_id, file, action, searchcode, replacecode) VALUES (1, ?, ?, ?, ?, ?)"
)

func expectNotFound(m sqlmock.Sqlmock) {
	m.ExpectQuery(qSelect).WithArgs("demo").WillReturnRows(sqlmock.NewRows([]string{"id"}))
}

func expectFound(m sqlmock.Sqlmock, id int64) {
	m.ExpectQuery(qSelect).WithArgs("demo").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(id))
}

func TestAddNew(t *testing.T) {
	r, m := newMock(t)
	info := testInfo()
	info.MySQL.Install = "INSTALL"
	info.MySQL.Enable = "ENABLE"
	expectNotFound(m)
	m.ExpectExec(qInsert).WithArgs("demo", "Demo", "INSTALL", "", "ENABLE", "", "").WillReturnResult(sqlmock.NewResult(7, 1))
	m.ExpectExec("INSTALL").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectExec("ENABLE").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectExec(qFile).WithArgs(int64(7), "a.php", "after", "s1", "r1").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec(qFile).WithArgs(int64(7), "a.php", "before", "s2", "r2").WillReturnResult(sqlmock.NewResult(2, 1))
	m.ExpectExec(qFile).WithArgs(int64(7), "b.php", "replace", "s3", "r3").WillReturnResult(sqlmock.NewResult(3, 1))
	if err := r.Add(context.Background(), info); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAddExisting(t *testing.T) {
	r, m := newMock(t)
	info := testInfo()
	info.MySQL.Upgrade = "UPGRADE"
	info.MySQL.Install = "INSTALL"
	expectFound(m, 3)
	m.ExpectExec(qUpdate).WithArgs("Demo", "INSTALL", "UPGRADE", "", "", "", int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec(qDelFile).WithArgs(int64(3)).WillReturnResult(sqlmock.NewResult(0, 3))
	m.ExpectExec("UPGRADE").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectExec(qFile).WithArgs(int64(3), "a.php", "after", "s1", "r1").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec(qFile).WithArgs(int64(3), "a.php", "before", "s2", "r2").WillReturnResult(sqlmock.NewResult(2, 1))
	m.ExpectExec(qFile).WithArgs(int64(3), "b.php", "replace", "s3", "r3").WillReturnResult(sqlmock.NewResult(3, 1))
	if err := r.Add(context.Background(), info); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyHooksSkipped(t *testing.T) {
	r, m := newMock(t)
	info := manifest.NewInfo()
	info.Name = "demo"
	expectNotFound(m)
	m.ExpectExec(qInsert).WithArgs("demo", "", "", "", "", "", "").WillReturnResult(sqlmock.NewResult(1, 1))
	if err := r.Add(context.Background(), info); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPlaceholderSubstitution(t *testing.T) {
	r, m := newMock(t)
	info := manifest.NewInfo()
	info.Name = "demo"
	info.MySQL.Install = "CREATE TABLE {prefix}_a; CREATE TABLE {userprefix}_b ENGINE={engine}"
	expectNotFound(m)
	m.ExpectExec(qInsert).WithArgs("demo", "", info.MySQL.Install, "", "", "", "").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("CREATE TABLE pf_a; CREATE TABLE up_b ENGINE=InnoDB").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := r.Add(context.Background(), info); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestHookFailureMessage(t *testing.T) {
	r, m := newMock(t)
	info := manifest.NewInfo()
	info.Name = "demo"
	info.MySQL.Install = "BAD {prefix}"
	expectNotFound(m)
	m.ExpectExec(qInsert).WithArgs("demo", "", info.MySQL.Install, "", "", "", "").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("BAD pf").WillReturnError(errors.New("boom"))
	err := r.Add(context.Background(), info)
	if err == nil {
		t.Fatal("expected error")
	}
	want := "MySQL query execution failed\n---\nQuery: BAD pf\nMessage: boom"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("got %q", err.Error())
	}
}
