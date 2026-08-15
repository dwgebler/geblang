package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCollectResources covers the three resource forms: a directory (embedded
// recursively), a glob, and the reserved-path / no-match guards.
func TestCollectResources(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("templates/page.html", "<h1>hi</h1>")
	write("templates/partials/nav.html", "<nav></nav>")
	write("static/app.css", "body{}")
	write("static/app.js", "console.log(1)")

	got, err := collectResources(root, []resourceSpec{{src: "templates"}, {src: "static/*.css"}})
	if err != nil {
		t.Fatalf("collectResources: %v", err)
	}

	wantKeys := []string{"templates/page.html", "templates/partials/nav.html", "static/app.css"}
	for _, k := range wantKeys {
		if _, ok := got[k]; !ok {
			t.Errorf("missing resource %q", k)
		}
	}
	if _, ok := got["static/app.js"]; ok {
		t.Errorf("glob should not have matched static/app.js")
	}
	if len(got) != len(wantKeys) {
		t.Errorf("got %d resources, want %d: %v", len(got), len(wantKeys), keys(got))
	}

	if _, err := collectResources(root, []resourceSpec{{src: "nope/*.png"}}); err == nil {
		t.Error("expected error for pattern matching no files")
	}

	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := collectResources(root, []resourceSpec{{src: "src"}}); err == nil {
		t.Error("expected error for resource colliding with reserved 'src' directory")
	}
}

// TestCollectResourcesMappedDest covers the "src=dest" remap form a build step
// uses to embed staged/minified copies at the runtime path without touching the
// source tree.
func TestCollectResourcesMappedDest(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("build/min/page.html", "<h1>min</h1>")
	write("build/min/partials/nav.html", "<nav>min</nav>")

	got, err := collectResources(root, []resourceSpec{
		parseResourceSpec("build/min=templates"),
	})
	if err != nil {
		t.Fatalf("collectResources: %v", err)
	}

	want := map[string]string{
		"templates/page.html":         "<h1>min</h1>",
		"templates/partials/nav.html": "<nav>min</nav>",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d resources, want %d: %v", len(got), len(want), keys(got))
	}
	for k, v := range want {
		if string(got[k]) != v {
			t.Errorf("mapped resource %q = %q, want %q", k, got[k], v)
		}
	}
}

// TestCollectResourcesRejectsEmbedShapedPath: a resource remapped onto a nested src/ path (embed()'s packing shape) must be rejected.
func TestCollectResourcesRejectsEmbedShapedPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "msg.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := collectResources(root, []resourceSpec{parseResourceSpec("msg.txt=src/data/msg.txt")})
	if err == nil {
		t.Error("expected error for resource remapped onto reserved src/ path")
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
