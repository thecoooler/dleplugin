package host

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
)

const fakeInstallerPort = `if [ "$1" = port ]; then printf '%s\n' "$DLEHOST_TEST_INSTALL_ADDRESS"; exit 0; fi
`

// Unit tests use a fake Docker executable and this HTTP peer. It writes only
// fixture configuration; actual DLE and database installation have Docker tests.
func serveTestInstaller(t *testing.T, s *stack.Stack) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			return
		}
		entries, err := os.ReadDir(s.StagingDir())
		if err != nil || len(entries) != 1 {
			http.Error(w, "missing staging", 500)
			return
		}
		root := filepath.Join(s.StagingDir(), entries[0].Name())
		write := func(name, data string) {
			if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o644); err != nil {
				t.Error(err)
			}
		}
		switch r.URL.Query().Get("action") {
		case "":
			fmt.Fprint(w, `<input value="eula">`)
		case "eula":
			fmt.Fprint(w, `<form id="check-eula">`)
		case "function_check":
			w.Header().Set("Location", "?action=dbconfig")
			w.WriteHeader(302)
		case "dbconfig":
			fmt.Fprint(w, `<input name="dbhost"><input name="dbprefix">`)
		case "checkdb":
			write("engine/data/dbconfig.php", fmt.Sprintf("<?php\ndefine (\"DBNAME\", %q);\n", r.PostFormValue("dbname")))
			fmt.Fprint(w, `{"status":"ok"}`)
		case "userconfig":
			fmt.Fprint(w, `<input name="reg_username">`)
		case "doinstall":
			fmt.Fprint(w, "?action=installtemplate ?action=docreatedb ?action=dosaveconfig")
		case "installtemplate", "docreatedb":
			fmt.Fprint(w, `{"status":"ok","offset":1,"total":1}`)
		case "dosaveconfig":
			write("engine/data/config.php", "<?php\n$config = array('skin' => 'Default', 'allow_alt_url' => '0', 'http_home_url' => '"+r.Header.Get("X-Forwarded-Proto")+"://"+r.Host+"/');")
			if err := os.Remove(filepath.Join(root, "install.php")); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, `{"status":"ok"}`)
		default:
			http.Error(w, "unexpected action", 400)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("DLEHOST_TEST_INSTALL_ADDRESS", strings.TrimPrefix(server.URL, "http://"))
}

// Minimal web installer for gate integration fixtures. The production installer
// client drives it via HTTP; it creates its schema/user instead of importing SQL.
const testInstallerPHP = `<?php
session_start();
$action = $_GET['action'] ?? '';
switch ($action) {
case '': echo '<input value="eula">'; break;
case 'eula': echo '<form id="check-eula">'; break;
case 'function_check': header('Location: ?action=dbconfig'); break;
case 'dbconfig': echo '<input name="dbhost"><input name="dbprefix">'; break;
case 'checkdb':
 $_SESSION['db'] = $_POST;
 $config = "<?php\n";
 foreach (['DBHOST'=>'dbhost', 'DBNAME'=>'dbname', 'DBUSER'=>'dbuser', 'DBPASS'=>'dbpasswd', 'PREFIX'=>'dbprefix', 'USERPREFIX'=>'dbprefix'] as $key=>$field) {
  $config .= 'define ("'.$key.'", '.var_export($_POST[$field], true).");\n";
 }
 file_put_contents(__DIR__.'/engine/data/dbconfig.php', $config);
 echo '{"status":"ok"}'; break;
case 'userconfig': echo '<input name="reg_username">'; break;
case 'doinstall':
 $_SESSION['admin'] = $_POST;
 echo '?action=installtemplate ?action=docreatedb ?action=dosaveconfig'; break;
case 'installtemplate': echo '{"status":"ok","offset":1,"total":1}'; break;
case 'docreatedb':
 $d = $_SESSION['db']; $a = $_SESSION['admin'];
 $db = new mysqli($d['dbhost'], $d['dbuser'], $d['dbpasswd'], $d['dbname']);
 $db->query('CREATE TABLE dle_users (user_id INT PRIMARY KEY, name VARCHAR(40) NOT NULL, password VARCHAR(255) NOT NULL, email VARCHAR(50), user_group SMALLINT)');
 $password = version_compare(trim(file_get_contents(__DIR__.'/fixture-version.txt')), '11.2', '<') ? md5(md5($a['reg_password1'])) : password_hash($a['reg_password1'], PASSWORD_DEFAULT);
 $stmt = $db->prepare('INSERT INTO dle_users VALUES (1, ?, ?, ?, 1)');
 $stmt->bind_param('sss', $a['reg_username'], $password, $a['reg_email']); $stmt->execute();
 echo '{"status":"ok","offset":1,"total":1}'; break;
case 'dosaveconfig':
 $url = ($_SERVER['HTTP_X_FORWARDED_PROTO'] ?? 'http').'://'.$_SERVER['HTTP_HOST'].'/';
 file_put_contents(__DIR__.'/engine/data/config.php', "<?php\n\$config = array('skin' => 'Default', 'allow_alt_url' => '1', 'http_home_url' => ".var_export($url, true).");");
 unlink(__FILE__); echo '{"status":"ok"}'; break;
default: http_response_code(400);
}
`
