package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A cached imported module loads without parsing; editing a module it took aliases from must still invalidate it.
func TestCachedModuleRevalidatesImportedAliases(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	write := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("geblang.yaml", "name: cachehit\nversion: 0.0.1\n")
	write("funds.gb", "module funds;\nexport type Cash = int;\n")
	write("wallet.gb", `module wallet;
import funds;
export type Coin = funds.Cash;
export func accept(funds.Cash c): string { return "${typeof(c)}"; }
`)
	write("app.gb", `module app;
import io;
import wallet;
func attempt(any v): string {
    try {
        return wallet.accept(v);
    } catch (TypeError e) {
        return "rejected";
    }
}
export func main(list<string> args): int {
    wallet.Coin c = 3;
    io.println("${attempt(5)} ${attempt(2.5)} ${typeof(c)}");
    return 0;
}
`)
	run := func() string {
		t.Helper()
		cmd := exec.Command(bin, "app.gb")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		return string(out)
	}
	want := "int rejected int\n"
	for _, label := range []string{"cold", "cached"} {
		if got := run(); got != want {
			t.Fatalf("%s: got %q, want %q", label, got, want)
		}
	}
	write("funds.gb", "module funds;\nexport type Cash = decimal;\n")
	if got, want := run(), "rejected decimal decimal\n"; got != want {
		t.Fatalf("after alias change: got %q, want %q", got, want)
	}
	if got, want := run(), "rejected decimal decimal\n"; got != want {
		t.Fatalf("cached after alias change: got %q, want %q", got, want)
	}
}
