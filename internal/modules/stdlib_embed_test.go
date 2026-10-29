package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"

	rootembed "geblang"
	"geblang/internal/version"
)

func TestEmbeddedStdlibContainsSourceModules(t *testing.T) {
	for _, p := range []string{"stdlib/money.gb", "stdlib/llm.gb", "stdlib/llm/openai.gb", "stdlib/rag.gb", "stdlib/geblang.yaml"} {
		if _, err := rootembed.StdlibFS.ReadFile(p); err != nil {
			t.Errorf("embedded stdlib missing %s: %v", p, err)
		}
	}
}

func TestEnsureEmbeddedStdlibExtracts(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	dir := ensureEmbeddedStdlib()
	if dir == "" {
		t.Fatal("ensureEmbeddedStdlib returned empty")
	}
	hash, err := embeddedStdlibHash(rootembed.StdlibFS)
	if err != nil {
		t.Fatal(err)
	}
	if base := filepath.Base(dir); base != "stdlib-"+version.Geblang+"-"+hash[:16] {
		t.Errorf("cache dir not version-scoped: %s", base)
	}
	for _, rel := range []string{"llm.gb", filepath.Join("llm", "openai.gb"), "geblang.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("extracted stdlib missing %s: %v", rel, err)
		}
	}
	if again := ensureEmbeddedStdlib(); again != dir {
		t.Errorf("second call should reuse the cache: %s != %s", again, dir)
	}
}

func TestEmbeddedStdlibHashChangesWithContentAndPaths(t *testing.T) {
	files := fstest.MapFS{"stdlib/geblang.yaml": {Data: []byte("name: stdlib\n")}}
	original, err := embeddedStdlibHash(files)
	if err != nil {
		t.Fatal(err)
	}
	files["stdlib/geblang.yaml"] = &fstest.MapFile{Data: []byte("name: newer\n")}
	changed, err := embeddedStdlibHash(files)
	if err != nil {
		t.Fatal(err)
	}
	if original == changed {
		t.Fatal("content change reused the same cache key")
	}
	files["stdlib/new.gb"] = &fstest.MapFile{Data: []byte("module new;")}
	added, err := embeddedStdlibHash(files)
	if err != nil {
		t.Fatal(err)
	}
	if changed == added {
		t.Fatal("new module reused the same cache key")
	}
}

func TestEnsureEmbeddedStdlibConcurrentReadersSeeCompleteSources(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var workers sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		workers.Go(func() {
			dir := ensureEmbeddedStdlib()
			if dir == "" {
				failures <- fmt.Errorf("could not resolve stdlib")
				return
			}
			for _, name := range []string{"money.gb", "resources.gb", "geblang.yaml"} {
				want, err := rootembed.StdlibFS.ReadFile("stdlib/" + name)
				if err != nil {
					failures <- err
					return
				}
				got, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					failures <- err
					return
				}
				if string(got) != string(want) {
					failures <- fmt.Errorf("incomplete source %s", name)
					return
				}
			}
		})
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}
