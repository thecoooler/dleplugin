package host

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testHTTPClient struct {
	t    *testing.T
	base string
	http *http.Client
}

type testHTTPResponse struct {
	code int
	body string
}

func (c *testHTTPClient) do(host, method, path string, form url.Values, headers map[string]string) testHTTPResponse {
	c.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	request, err := http.NewRequest(method, c.base+"/", body)
	if err != nil {
		c.t.Fatal(err)
	}
	request.URL.Opaque = path // the path is sent without client normalization
	request.Host = host
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range headers {
		if v == "" {
			request.Header.Del(k) // an empty value means no header
			continue
		}
		request.Header.Set(k, v)
	}
	response, err := c.http.Do(request)
	if err != nil {
		c.t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return testHTTPResponse{response.StatusCode, string(data)}
}

func writeTestDist(t *testing.T, dir, version string) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, version+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, data := range map[string]string{
		// The version comment must follow "<?php": text before it would precede every response.
		"install.php":                strings.Replace(testInstallerPHP, "<?php\n", fmt.Sprintf("<?php\n// $config = array('version_id' => %q);\n", version), 1),
		"fixture-version.txt":        version,
		"engine/data/.keep":          "",
		"templates/Default/main.tpl": "test template",
		"index.php":                  "<?php session_start(); $_SESSION['n'] = ($_SESSION['n'] ?? 0) + 1; echo 'php:index.php:' . $_SESSION['n'];",
		"admin.php":                  "<?php echo 'admin:ok';",
		"style.css":                  "css:ok",
		".htaccess":                  "RewriteEngine On\nRewriteRule ^free$ style.css [L]\n",
		"engine/.htaccess":           "Require all denied\n",
		"engine/secret.txt":          "secret",
		"uploads/.htaccess":          "<If \"true\">\n    Require all granted\n</If>\nSatisfy Any\n",
		"uploads/x.txt":              "upload:ok",
	} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func randomTestHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func fakeDocker(t *testing.T) (calls, fail string) {
	t.Helper()
	bin := t.TempDir()
	calls, fail = filepath.Join(bin, "calls"), filepath.Join(bin, "fail")
	script := "#!/bin/sh\n" + fakeInstallerPort + "in=$(cat)\necho \"$* $in\" >> " + calls + "\n" +
		"case \"$*\" in *graceful*) if [ -n \"$DLEHOST_TEST_WATCH\" ] && [ -e \"$DLEHOST_TEST_WATCH\" ]; then echo published-before-reload >> " + calls + "; fi;; esac\n" +
		"if [ -s " + fail + " ]; then pat=$(cat " + fail + "); case \"$* $in\" in *\"$pat\"*) exit 1;; esac; fi\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return calls, fail
}
