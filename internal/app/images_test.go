package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/dockerimg"
)

type imgMod struct {
	fakeMod
	images []dockerimg.Image
}

func (m imgMod) Images() []dockerimg.Image { return m.images }

func TestToolImages(t *testing.T) {
	a := dockerimg.Image{Name: "tool-a", Dockerfile: []byte("FROM a\n"), Files: map[string][]byte{"composer.lock": []byte("a")}}
	b := dockerimg.Image{Name: "tool-b", Dockerfile: []byte("FROM b\n")}
	rejected := dockerimg.Image{Name: "tool-c", Dockerfile: []byte("FROM c\n")}
	mods := []Module{
		imgMod{fakeMod{name: "a", cmds: []string{"a"}}, []dockerimg.Image{a}},
		fakeMod{name: "plain", cmds: []string{"p"}},
		imgMod{fakeMod{name: "b", cmds: []string{"b"}}, []dockerimg.Image{b}},
		imgMod{fakeMod{name: "c", cmds: []string{"a"}}, []dockerimg.Image{rejected}},
	}

	rt, o, e, _ := setup(t)
	dir := t.TempDir()
	if code := Execute(context.Background(), rt, []string{"tool-images", "--context", dir}, mods...); code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
	if want := a.Tag() + "\n" + b.Tag() + "\n"; o.String() != want {
		t.Errorf("out = %q, want %q", o, want)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "tool-a", "composer.lock")); err != nil || string(got) != "a" {
		t.Errorf("tool-a context = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "tool-b", "Dockerfile")); err != nil || string(got) != "FROM b\n" {
		t.Errorf("tool-b context = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tool-c")); !os.IsNotExist(err) {
		t.Errorf("rejected module image written: %v", err)
	}

	rt, o, _, _ = setup(t)
	if code := Execute(context.Background(), rt, []string{"--help"}, mods...); code != 0 || strings.Contains(o.String(), "tool-images") {
		t.Errorf("exit %d, help lists the hidden command:\n%s", code, o)
	}
}
