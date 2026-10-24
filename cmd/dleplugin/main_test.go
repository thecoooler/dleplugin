package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/app"
)

func TestHelpOrder(t *testing.T) {
	var out bytes.Buffer
	rt := app.NewRuntime(strings.NewReader(""), &out, &out, "")
	registered := modules()
	var names []string
	for _, module := range registered {
		names = append(names, module.Name())
	}
	if got, want := strings.Join(names, ","), "init,build,convert,diff,deploy,static,test,check,hosts,skills"; got != want {
		t.Fatalf("module order = %s, want %s", got, want)
	}
	root := app.NewRootCommand(rt, registered...)
	if errs := rt.ModuleErrors(); len(errs) != 0 {
		t.Fatalf("module errors: %v", errs)
	}
	root.SetOut(&out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, line := range strings.Split(out.String(), "\n") {
		if m := regexp.MustCompile(`^(\S.*:)$|^  (\w+) `).FindStringSubmatch(line); m != nil {
			got = append(got, m[1]+m[2])
		}
	}
	want := []string{
		"Usage:", "dleplugin",
		"Plugin project:", "init", "convert",
		"Checks:", "static", "test", "check",
		"Build and deploy:", "build", "diff", "deploy",
		"DLE hosts:", "hosts",
		"AI agents:", "skills",
		"Additional Commands:", "help",
		"Flags:",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("help order\n got: %v\nwant: %v\n%s", got, want, out.String())
	}
}
