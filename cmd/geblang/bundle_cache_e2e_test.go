package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/bcloader"
	"geblang/internal/bytecode"
)

// A built binary seeds its cache with module chunks whose alias fingerprints point into the extraction directory.
func TestBundledModulesSeedParseFreeCache(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	write := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("geblang.yaml", "name: bundlecache\nversion: 0.0.1\n")
	write("funds.gb", "module funds;\nexport type Cash = int;\n")
	write("wallet.gb", `module wallet;
import funds;
export type Coin = funds.Cash;
export func accept(funds.Cash c): string { return "${typeof(c)}"; }
`)
	write("app.gb", `module app;
import io;
import wallet;
export func main(list<string> args): int {
    wallet.Coin c = 3;
    io.println("${wallet.accept(5)} ${typeof(c)}");
    return 0;
}
`)
	out := filepath.Join(dir, "app-bin")
	if built, err := exec.Command(bin, "build", "--entry", "app", "--out", out, dir).CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, built)
	}
	runDir, tmp := t.TempDir(), t.TempDir()
	for i := 0; i < 2; i++ {
		cmd := exec.Command(out)
		cmd.Dir = runDir
		cmd.Env = append(os.Environ(), "TMPDIR="+tmp)
		got, err := cmd.CombinedOutput()
		if err != nil || string(got) != "int int\n" {
			t.Fatalf("run %d: %v %q", i, err, got)
		}
	}
	extracted, err := filepath.Glob(filepath.Join(tmp, "geblang-*"))
	if err != nil || len(extracted) != 1 {
		t.Fatalf("extraction dir: %v %v", extracted, err)
	}
	root := extracted[0]
	raw, err := os.ReadFile(filepath.Join(root, "src", "wallet.gbc"))
	if err != nil {
		t.Fatal(err)
	}
	if bundled, err := bytecode.Decode(raw); err != nil || !bundled.ClassAliasesResolved || len(bundled.AliasSources) == 0 || !strings.HasPrefix(bundled.AliasSources[0].Path, bytecode.BundleSourcePrefix) {
		t.Fatalf("bundled wallet chunk lacks bundle-relative fingerprints: %+v %v", bundled.AliasSources, err)
	}
	source, err := os.ReadFile(filepath.Join(root, "src", "wallet.gb"))
	if err != nil {
		t.Fatal(err)
	}
	seeded, err := bytecode.Decode(rebasedBundleChunk(raw, root))
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range seeded.AliasSources {
		if src.Path != "" && !strings.HasPrefix(src.Path, root) {
			t.Fatalf("alias source %q not rebased under %s", src.Path, root)
		}
	}
	if !bcloader.AliasSourcesFresh(seeded, []string{filepath.Join(root, "src")}) || seeded.SourceHash != bytecode.SourceHash(source) {
		t.Fatal("seeded wallet chunk would not load without parsing")
	}
}

func TestBundleRelativeSourcesRejectsFilesOutsideTheBundle(t *testing.T) {
	chunk := bytecode.Chunk{AliasSources: []bytecode.AliasSource{{Module: "funds", Path: "/src/funds.gb"}, {Module: "gone", Path: ""}}}
	if !bundleRelativeSources(&chunk, map[string]string{"/src/funds.gb": "src/funds.gb"}) {
		t.Fatal("bundled source rejected")
	}
	if got := chunk.AliasSources[0].Path; got != bytecode.BundleSourcePrefix+"src/funds.gb" {
		t.Fatalf("rewritten path %q", got)
	}
	bytecode.RebaseBundleSources(&chunk, "/x/geblang-hash")
	if got := chunk.AliasSources[0].Path; got != filepath.Join("/x/geblang-hash", "src", "funds.gb") {
		t.Fatalf("rebased path %q", got)
	}
	outside := bytecode.Chunk{AliasSources: []bytecode.AliasSource{{Module: "ext", Path: "/elsewhere/ext.gb"}}}
	if bundleRelativeSources(&outside, map[string]string{}) {
		t.Fatal("a source outside the bundle was accepted")
	}
}
