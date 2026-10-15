package build

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	gmhtml "github.com/yuin/goldmark/renderer/html"

	"github.com/thecoooler/dleplugin/internal/manifest"
)

// Result lists the artifacts produced by WriteDist; unused fields are empty.
type Result struct {
	Dir         string
	XMLPath     string
	ArchivePath string
	ReadmePath  string
}

// WriteDist rebuilds root/dist: the plugin XML, README.html (when
// root/README.md exists) and, in split mode, the zip archive.
func WriteDist(root string, info *manifest.Info, files []manifest.SourceFile, version string) (*Result, error) {
	if err := manifest.ValidateSlug(info.Slug); err != nil {
		return nil, err
	}
	for _, f := range files {
		if err := manifest.ValidateFilePath(f.Mount); err != nil {
			return nil, err
		}
	}
	if info.Files != nil {
		for _, name := range info.Files.Keys() {
			if err := manifest.ValidateFilePath(name); err != nil {
				return nil, err
			}
		}
	}
	if info.Build.Split {
		if _, err := os.Stat(filepath.Join(root, "src", ".htaccess")); err != nil {
			return nil, errors.New("Not found .htaccess, but it is required when build split mode is true")
		}
	}

	project, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open project: %w", err)
	}
	defer project.Close()
	dist := filepath.Join(root, "dist")
	if err := project.RemoveAll("dist"); err != nil {
		return nil, fmt.Errorf("clean %s: %w", dist, err)
	}
	if err := project.MkdirAll("dist", 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", dist, err)
	}

	output, err := project.OpenRoot("dist")
	if err != nil {
		return nil, err
	}
	defer output.Close()
	res := &Result{Dir: dist}
	xmlPath := filepath.Join(dist, info.Slug+".xml")
	xml, err := XML(info, files, version)
	if err != nil {
		return nil, err
	}
	if err := output.WriteFile(info.Slug+".xml", []byte(xml), 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", xmlPath, err)
	}
	res.XMLPath = xmlPath

	md, err := os.ReadFile(filepath.Join(root, "README.md"))
	switch {
	case err == nil:
		readme, err := renderReadme(info.Name, md)
		if err != nil {
			return nil, err
		}
		res.ReadmePath = filepath.Join(dist, "README.html")
		if err := output.WriteFile("README.html", []byte(readme), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", res.ReadmePath, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("read README.md: %w", err)
	}

	if info.Build.Split {
		archive := filepath.Join(dist, info.Slug+".zip")
		if err := writeSplitArchive(output, info.Slug+".zip", xmlPath, files); err != nil {
			return nil, err
		}
		if err := output.Remove(info.Slug + ".xml"); err != nil {
			return nil, fmt.Errorf("remove %s: %w", xmlPath, err)
		}
		res.XMLPath = ""
		res.ArchivePath = archive
	}
	return res, nil
}

func renderReadme(title string, md []byte) (string, error) {
	var body bytes.Buffer
	conv := goldmark.New(goldmark.WithRendererOptions(gmhtml.WithUnsafe()))
	if err := conv.Convert(md, &body); err != nil {
		return "", fmt.Errorf("render README.md: %w", err)
	}
	return fmt.Sprintf(readmeTemplate, title, body.String()), nil
}

func writeSplitArchive(root *os.Root, archive, xmlPath string, files []manifest.SourceFile) (err error) {
	out, err := root.Create(archive)
	if err != nil {
		return fmt.Errorf("create %s: %w", archive, err)
	}
	defer func() {
		if cerr := out.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("close %s: %w", archive, cerr)
		}
	}()

	zw := zip.NewWriter(out)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.BestCompression)
	})
	if err := addZipFile(zw, xmlPath, filepath.Base(xmlPath)); err != nil {
		return err
	}
	for _, f := range files {
		if err := addZipFile(zw, f.Src, f.Mount); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finish %s: %w", archive, err)
	}
	return nil
}

func addZipFile(zw *zip.Writer, src, name string) error {
	st, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("archive %s: %w", src, err)
	}
	hdr, err := zip.FileInfoHeader(st)
	if err != nil {
		return fmt.Errorf("archive %s: %w", src, err)
	}
	hdr.Name = filepath.ToSlash(name)
	hdr.Method = zip.Deflate
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return fmt.Errorf("archive %s: %w", src, err)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("archive %s: %w", src, err)
	}
	defer in.Close()
	if _, err := io.Copy(w, in); err != nil {
		return fmt.Errorf("archive %s: %w", src, err)
	}
	return nil
}

// CommitID returns the first 7 characters of HEAD in dir, or "" when git is
// unavailable, dir is not inside a repository or there are no commits.
func CommitID(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(out))
	if len(id) < 7 {
		return ""
	}
	return id[:7]
}

// readmeTemplate is the README.html page wrapper (title, body).
const readmeTemplate = `
        <!doctype>
        <html>
        <head>
            <meta charset="utf-8">
            <meta name="viewport" content="width=device-width, initial-scale=1">
            <title>%s</title>
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-QWTKZyjpPEjISv5WaRU9OFeRpok6YctnYmDr5pNlyT2bRjXh0JMhjY6hW+ALEwIH" crossorigin="anonymous">
        </head>
        <body>
            <div class="container mt-4">%s</div>

            <script src="https://cdn.jsdelivr.net/npm/bootstrap@5.3.3/dist/js/bootstrap.bundle.min.js" integrity="sha384-YvpcrYf0tY3lHB60NNkmXc5s9fDVZLESaAA55NDzOxhy9GkcIdslK1eN7N6jIeHz" crossorigin="anonymous"></script>

            <link rel="stylesheet" href="https://unpkg.com/@highlightjs/cdn-assets@11.9.0/styles/default.min.css">
            <script src="https://unpkg.com/@highlightjs/cdn-assets@11.9.0/highlight.min.js"></script>

            <!-- and it's easy to individually load additional languages -->
            <script src="https://unpkg.com/@highlightjs/cdn-assets@11.9.0/languages/go.min.js"></script>

            <script>hljs.highlightAll();</script>
        </body>
        </html>
        `
