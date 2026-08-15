package bundle

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"geblang/internal/ast"
	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func mustParse(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	return prog
}

func writeBundleFile(t *testing.T, path string, manifest Manifest, files map[string][]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := Write(f, manifest, files); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func openBundleFile(t *testing.T, path string) *Bundle {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	zipSize := int64(binary.LittleEndian.Uint64(raw[len(raw)-TrailerSize : len(raw)-TrailerSize+8]))
	zipData := raw[int64(len(raw))-int64(TrailerSize)-zipSize : int64(len(raw))-int64(TrailerSize)]
	b, err := parseZip(zipData)
	if err != nil {
		t.Fatalf("parseZip: %v", err)
	}
	return b
}

func bundleFrom(t *testing.T, files map[string][]byte) *Bundle {
	t.Helper()
	return bundleFromManifest(t, Manifest{Version: "test", Entry: "main"}, files)
}

func bundleFromManifest(t *testing.T, manifest Manifest, files map[string][]byte) *Bundle {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(&buf, manifest, files); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw := buf.Bytes()
	zipSize := int64(binary.LittleEndian.Uint64(raw[len(raw)-TrailerSize : len(raw)-TrailerSize+8]))
	zipData := raw[int64(len(raw))-int64(TrailerSize)-zipSize : int64(len(raw))-int64(TrailerSize)]
	b, err := parseZip(zipData)
	if err != nil {
		t.Fatalf("parseZip: %v", err)
	}
	return b
}

func TestPermissionsRoundTrip(t *testing.T) {
	files := map[string][]byte{"src/main.gb": []byte("func main() {}")}
	want := &Permissions{FFI: []string{"/usr/lib/a.so", "/opt/*.so"}, Onnx: true, ProcessControl: true}

	b := bundleFromManifest(t, Manifest{Version: "test", Entry: "main", Permissions: want}, files)
	if !reflect.DeepEqual(b.Manifest.Permissions, want) {
		t.Fatalf("round-trip: got %+v, want %+v", b.Manifest.Permissions, want)
	}

	plain := bundleFromManifest(t, Manifest{Version: "test", Entry: "main"}, files)
	if plain.Manifest.Permissions != nil {
		t.Fatalf("a manifest with no permissions must decode as nil, got %+v", plain.Manifest.Permissions)
	}
}

// TestExtractAtomicPublish: ExtractTo publishes a complete dir, leaves no temp dirs, and skips on a second call.
func TestExtractAtomicPublish(t *testing.T) {
	b := bundleFrom(t, map[string][]byte{
		"src/main.gb": []byte("func main() {}"),
		"stdlib/a.gb": []byte("module a;"),
	})
	parent := t.TempDir()
	dir := filepath.Join(parent, "extract")
	if err := b.ExtractTo(dir, nil); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stdlib", "a.gb")); err != nil {
		t.Fatalf("extracted file missing: %v", err)
	}
	entries, _ := os.ReadDir(parent)
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), ".geblang-extract-") {
			t.Errorf("leftover temp extract dir: %s", e.Name())
		}
	}
	if err := b.ExtractTo(dir, nil); err != nil {
		t.Fatalf("second extract (idempotent skip): %v", err)
	}
}

// TestResourceRoundTrip embeds a non-.gb resource at a project-relative path and
// confirms ExtractTo writes it back at the same relative path under the extract
// dir, so a bundled program reads it via sys.bundleDir()+"/templates/page.html".
func TestResourceRoundTrip(t *testing.T) {
	want := []byte("<h1>{{ title }}</h1>")
	files := map[string][]byte{
		"src/main.gb":         []byte("func main() {}"),
		"templates/page.html": want,
	}
	manifest := Manifest{Version: "test", Entry: "main"}

	var buf bytes.Buffer
	if err := Write(&buf, manifest, files); err != nil {
		t.Fatalf("write: %v", err)
	}

	raw := buf.Bytes()
	zipSize := int64(binary.LittleEndian.Uint64(raw[len(raw)-TrailerSize : len(raw)-TrailerSize+8]))
	zipData := raw[int64(len(raw))-int64(TrailerSize)-zipSize : int64(len(raw))-int64(TrailerSize)]

	b, err := parseZip(zipData)
	if err != nil {
		t.Fatalf("parseZip: %v", err)
	}

	dir := filepath.Join(t.TempDir(), "extract")
	if err := b.ExtractTo(dir, nil); err != nil {
		t.Fatalf("extract: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "templates", "page.html"))
	if err != nil {
		t.Fatalf("read embedded resource: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("resource content mismatch: got %q, want %q", got, want)
	}
}

// TestExtractToSeedsRealCachePaths: the seeded .gbc must land at bytecode.CachePath's path, not a drifted lookalike.
func TestExtractToSeedsRealCachePaths(t *testing.T) {
	src := []byte("import io;\nio.println(\"hi\");\n")
	chunk, err := bytecode.Compile(mustParse(t, string(src)), src, "testc")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	files := map[string][]byte{"src/app.gb": src, "src/app.gbc": encoded}
	manifest := Manifest{Version: "testc", Entry: "app"}
	out := filepath.Join(t.TempDir(), "bin")
	writeBundleFile(t, out, manifest, files)
	b := openBundleFile(t, out)

	cacheDir := t.TempDir()
	extractDir := filepath.Join(t.TempDir(), "x")
	err = b.ExtractTo(extractDir, func(sp string, s []byte) string {
		return bytecode.CachePath(cacheDir, sp, s, "testc")
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	finalSrc := filepath.Join(extractDir, "src", "app.gb")
	want := bytecode.CachePath(cacheDir, finalSrc, src, "testc")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("seeded cache file not at the runtime's lookup path: %v", err)
	}
}
