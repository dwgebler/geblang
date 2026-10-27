package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAuditRepairsAcrossImportedCachedAndBuiltPaths(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	files := map[string]string{
		"geblang.yaml": "name: audit\nversion: 0.0.1\n",
		"donor.gb": `module donor;
import io;
from aspects import wrap;
export class Base {
    func value(): int { try { return 1; } finally { return 2; } }
    static func stable(): int { try { return 3; } finally { return 4; } }
    func reader(): callable { func read(): int { return this.value(); } return read; }
}
export class Child extends Base {
    func own(): int { return this.value() + Base.stable(); }
    func ownReader(): callable { func read(): int { return this.own(); } return read; }
}
func step(): int { io.println("step"); return 1; }
export func factory(int value): callable {
    @wrap func inner(int delta = 1): int { value += delta; return value; }
    return func(int delta = 1): int { return inner(delta: delta); };
}
export func overloads(): callable {
    int base = 4;
    @wrap func choose(int x = 2): int { return x + base; }
    func choose(string x): string { return x + "!"; }
    return choose;
}
export func probe(): void {
    io.println(Base().value());
    io.println(Child().value());
    io.println(Child().own());
    io.println(Base().reader()());
    io.println(Child().reader()());
    io.println(Child().ownReader()());
    let xs = [1, 2, 3];
    list<int> seen = [];
    for (x in xs) { seen.push(x); if (x == 1) { xs.push(4); xs[1] = 9; } }
    io.println(seen);
    any missing = null;
    try { for (i in missing..<2) { io.println(i); } }
    catch (Error e) { io.println(e.message); }
    try { let r = 0..<2 by missing; io.println(r); }
    catch (Error e) { io.println(e.message); }
    for (ch in 'a'..'b' by step()) { io.println(ch); }
}
`,
		"aspects.gb": `module aspects;
export func wrap(callable next): callable { return func(any... args): any { return next(...args); }; }
`,
		"app.gb": `module app;
import donor;
import io;
export func main(list<string> args): int {
    donor.probe();
    let next = donor.factory(9);
    io.println(next()); io.println(next(delta: 2));
    let choose = donor.overloads();
    io.println(choose(3)); io.println(choose(x: "ok"));
    return 0;
}
`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(path string, args ...string) string {
		t.Helper()
		command := exec.Command(path, args...)
		command.Dir = dir
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
		return string(output)
	}
	want := "2\n2\n6\n2\n2\n6\n[1, 9, 3]\nrange start must be int\nrange step must be int\nstep\na\nb\n10\n12\n7\nok!\n"

	for _, args := range [][]string{{"--vm-strict", "app.gb"}, {"--vm-strict", "app.gb"}, {"--disable-vm", "app.gb"}} {
		if got := run(bin, args...); got != want {
			t.Fatalf("%v: got %q, want %q", args, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".geblang-cache")); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "audit")
	run(bin, "build", "--entry", "app", "--out", out, dir)

	if got := run(out); got != want {
		t.Fatalf("built output = %q, want %q", got, want)
	}
}
