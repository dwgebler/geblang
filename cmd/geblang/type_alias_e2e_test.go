package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExportedTypeAliasesAcrossCachedAndBuiltPaths(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	files := map[string]string{
		"geblang.yaml": "name: aliases\nversion: 0.0.1\n",
		"contract.gb": `module contract;
export interface Client { func chat(): string; }
export class Base { func Base() {} func hello(): string { return "base"; } }
`,
		"facade.gb": `module facade;
import contract;
export type Client = contract.Client;
export type Root = contract.Base;
export type Money = decimal;
`,
		"app.gb": `module app;
import facade;
import io;
from facade import Money;
class Mine implements facade.Client { func Mine() {} func chat(): string { return "mine"; } }
class Kid extends facade.Root { func Kid() { parent(); } }
func use(facade.Client c): string { return c.chat(); }
export func main(list<string> args): int {
    Money m = 2;
    io.println("${use(Mine())} ${Kid().hello()} ${typeof(m)}");
    io.println(Mine() instanceof facade.Client);
    return 0;
}
`,
	}
	write := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, source := range files {
		write(name, source)
	}
	run := func(path string, args ...string) string {
		t.Helper()
		cmd := exec.Command(path, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	want := "mine base decimal\ntrue\n"
	for _, check := range []struct {
		name string
		args []string
	}{
		{name: "cold VM", args: []string{"app.gb"}},
		{name: "cached VM", args: []string{"app.gb"}},
		{name: "evaluator", args: []string{"--disable-vm", "app.gb"}},
	} {
		if got := run(bin, check.args...); got != want {
			t.Fatalf("%s: got %q, want %q", check.name, got, want)
		}
	}
	out := filepath.Join(dir, "aliases")
	build := exec.Command(bin, "build", "--entry", "app", "--out", out, dir)
	if built, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, built)
	}
	if got := run(out); got != want {
		t.Fatalf("built binary: got %q, want %q", got, want)
	}
	write("facade.gb", `module facade;
import contract;
export type Client = contract.Client;
export type Root = contract.Base;
export type Money = float;
`)
	if got, want := run(bin, "app.gb"), "mine base float\ntrue\n"; got != want {
		t.Fatalf("cached VM after alias change: got %q, want %q", got, want)
	}
}
