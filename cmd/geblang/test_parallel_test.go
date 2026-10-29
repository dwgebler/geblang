package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParallelTestFiles_PublicCLI(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	run := func(t *testing.T, args ...string) (string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, append([]string{"test"}, args...)...)
		output, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("test command timed out: %s", output)
		}
		return string(output), err
	}
	passing := writeTestCorpus(t, map[string]string{"pass_test.gb": passingTestFile})
	t.Run("accepted divergences keep their runtime and report once", func(t *testing.T) {
		dir := writeTestCorpus(t, map[string]string{
			"partial_test.gb":      "# @vm-divergence: overload-partial\n" + parityGapTestFile,
			"pass_test.gb":         passingTestFile,
			"KNOWN_DIVERGENCES.md": "| key | file | reason | date |\n|-----|------|--------|------|\n| overload-partial | partial_test.gb | known gap | 2026-07-06 |\n",
		})
		output, err := run(t, "--jobs=2", dir)
		if err != nil {
			t.Fatalf("accepted divergence: %v\n%s", err, output)
		}
		if strings.Count(output, "accepted divergences:") != 1 || strings.Count(output, "tests: total=") != 1 || !strings.Contains(output, "total=3 failed=0 passed=3 skipped=0") {
			t.Fatalf("unexpected aggregation:\n%s", output)
		}
	})
	t.Run("workers retain the directory invocation ledger", func(t *testing.T) {
		dir := writeTestCorpus(t, map[string]string{
			"sub/accepted_test.gb":     "# @vm-divergence: overload-partial\n" + parityGapTestFile,
			"KNOWN_DIVERGENCES.md":     "| key | file | reason | date |\n|-----|------|--------|------|\n| overload-partial | sub/accepted_test.gb | known gap | 2026-10-29 |\n",
			"sub/KNOWN_DIVERGENCES.md": "| key | file | reason | date |\n|-----|------|--------|------|\n",
		})
		sequential, err := run(t, "--jobs", "1", dir)
		if err != nil {
			t.Fatalf("sequential: %v\n%s", err, sequential)
		}

		parallel, err := run(t, "--jobs", "2", dir)

		if err != nil || parallel != sequential {
			t.Fatalf("parallel: %v\ngot %q\nwant %q", err, parallel, sequential)
		}
	})
	t.Run("invalid divergences fail before any file runs", func(t *testing.T) {
		dir := writeTestCorpus(t, map[string]string{
			"a_test.gb": `import io; io.println("SHOULD_NOT_RUN");` + passingTestFile,
			"b_test.gb": "# @vm-divergence: missing-ledger\n" + passingTestFile,
		})
		output, err := run(t, "--jobs", "2", dir)
		if err == nil || !strings.Contains(output, "missing-ledger") || strings.Contains(output, "SHOULD_NOT_RUN") {
			t.Fatalf("preflight: %v\n%s", err, output)
		}
	})
	t.Run("rejects invalid worker counts", func(t *testing.T) {
		for _, value := range []string{"", "0", "-1", "no", "1.5", "999999999999999999999"} {
			output, err := run(t, "--jobs="+value, passing)
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 || !strings.Contains(output, "positive integer") {
				t.Fatalf("jobs=%q: %v, output %q", value, err, output)
			}
		}
	})
	for _, lane := range []string{"vm", "evaluator"} {
		t.Run(lane+"/formats and filters", func(t *testing.T) {
			dir := writeTestCorpus(t, map[string]string{
				"a_test.gb": `import test; class OnlyTest extends test.Test {
    @test @tag("fast") func selected(): void { this.assertEquals(2,1+1); }
    @test @tag("slow") func excluded(): void { this.fail(); }
}`,
				"b_test.gb": `import test; class OtherTest extends test.Test { @test func excluded(): void { this.fail(); } }`,
			})
			for _, format := range []string{"summary", "verbose", "teamcity"} {
				args := []string{"--runtime", lane, "--format", format, "--class", "OnlyTest", "--tag", "fast", "--method", "selected", dir}
				sequential, err := run(t, args...)
				if err != nil {
					t.Fatalf("sequential: %v\n%s", err, sequential)
				}
				parallel, err := run(t, append([]string{"--jobs", "2"}, args...)...)
				if err != nil || parallel != sequential {
					t.Fatalf("parallel: %v\n%s\nwant:\n%s", err, parallel, sequential)
				}
			}
		})
		t.Run(lane+"/aggregates failures errors and skips", func(t *testing.T) {
			dir := writeTestCorpus(t, map[string]string{
				"a_pass_test.gb":  `import test; class Passing extends test.Test { @test func ok(): void { this.assertTrue(true); } }`,
				"b_error_test.gb": `class Broken {`,
				"c_fail_test.gb":  `import test; class Failing extends test.Test { @test func bad(): void { this.assertEquals(1,2); } }`,
				"d_skip_test.gb":  `import test; class Skipping extends test.Test { @test func ignoredCase(): void { this.skip("reason"); } }`,
			})
			output, err := run(t, "--jobs", "3", "--runtime", lane, dir)
			if err == nil {
				t.Fatal("expected failure")
			}
			for _, want := range []string{"b_error_test.gb", "FAIL Failing", "tests: total=3 failed=1 passed=1 skipped=1"} {
				if !strings.Contains(output, want) {
					t.Fatalf("missing %q:\n%s", want, output)
				}
			}
		})
		t.Run(lane+"/capabilities remain opt in", func(t *testing.T) {
			dir := writeTestCorpus(t, map[string]string{"permission_test.gb": `import test; import process;
class Permissions extends test.Test {
    @test @tag("--allow-process-control") func checksCapability(): void {
        this.assertThrows(func(): void { process.signal(process.pid(), "NOT_A_SIGNAL"); }, "unsupported signal");
    }
}`})
			denied, err := run(t, "--jobs", "2", "--runtime", lane, dir)
			if err == nil || !strings.Contains(denied, "PermissionError") {
				t.Fatalf("expected denied capability: %v\n%s", err, denied)
			}
			allowed, err := run(t, "--jobs", "2", "--runtime", lane, "--allow-process-control", dir)
			if err != nil {
				t.Fatalf("forward capability: %v\n%s", err, allowed)
			}
			spoofed, err := run(t, "--jobs", "2", "--runtime", lane, "--tag", "--allow-process-control", dir)
			if err == nil || !strings.Contains(spoofed, "PermissionError") {
				t.Fatalf("tag value granted capability: %v\n%s", err, spoofed)
			}
		})
		t.Run(lane+"/FFI grants reach workers", func(t *testing.T) {
			if goruntime.GOOS != "linux" {
				t.Skip("fixture uses Linux libm")
			}
			dir := writeTestCorpus(t, map[string]string{"ffi_test.gb": `import test; import ffi;
class FFIPermissions extends test.Test {
    @test func opensGrantedLibrary(): void {
        let library=ffi.dlopen("libm.so.6");
        library.close();
        this.assertTrue(true);
    }
}`})
			denied, err := run(t, "--jobs", "2", "--runtime", lane, dir)
			if err == nil || !strings.Contains(denied, "PermissionError") {
				t.Fatalf("FFI should be denied: %v\n%s", err, denied)
			}
			allowed, err := run(t, "--jobs", "2", "--runtime", lane, "--allow-ffi", "libm.so.*", dir)
			if err != nil {
				t.Fatalf("forward FFI grant: %v\n%s", err, allowed)
			}
		})
		t.Run(lane+"/bounded concurrency isolation and discovery order", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			started := make(chan string, 4)
			initialRelease := make(chan struct{})
			releaseA := make(chan struct{})
			var mu sync.Mutex
			active, peak := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				active++
				if active > peak {
					peak = active
				}
				mu.Unlock()
				defer func() { mu.Lock(); active--; mu.Unlock() }()
				name := strings.TrimPrefix(r.URL.Path, "/")
				started <- name
				select {
				case <-initialRelease:
				case <-ctx.Done():
					return
				}
				if name == "a" {
					select {
					case <-releaseA:
					case <-ctx.Done():
						return
					}
				}
				_, _ = w.Write([]byte("ok"))
			}))
			defer server.Close()
			defer cancel()
			files := map[string]string{}
			for _, name := range []string{"a", "b", "c", "d"} {
				files[name+"_test.gb"] = fmt.Sprintf(`import test; import http; import io; import sys;
class ConcurrentTest extends test.Test {
    @test func work(): void {
        sys.setenv("GEBLANG_PARALLEL_ISOLATION", %q);
        let response=http.get(%q);
        this.assertEquals(200,response.status());
        this.assertEquals(%q,sys.getenv("GEBLANG_PARALLEL_ISOLATION"));
        io.println(%q);
    }
}`, name, server.URL+"/"+name, name, "file-"+name)
			}
			dir := writeTestCorpus(t, files)
			cmd := exec.CommandContext(ctx, bin, "test", "--jobs", "2", "--runtime", lane, dir)
			type completion struct {
				output []byte
				err    error
			}
			done := make(chan completion, 1)
			go func() { output, err := cmd.CombinedOutput(); done <- completion{output, err} }()
			for range 2 {
				select {
				case <-started:
				case <-ctx.Done():
					t.Fatal("two workers did not reach the barrier")
				}
			}
			close(initialRelease)
			// File c cannot start until b's worker exits while a remains blocked.
			select {
			case name := <-started:
				if name != "c" {
					t.Fatalf("started %q, want c", name)
				}
			case <-ctx.Done():
				t.Fatal("next file did not start")
			}
			close(releaseA)
			var result completion
			select {
			case result = <-done:
			case <-ctx.Done():
				t.Fatal("workers did not finish")
			}
			if result.err != nil {
				t.Fatalf("parallel: %v\n%s", result.err, result.output)
			}
			mu.Lock()
			maxActive := peak
			mu.Unlock()
			if maxActive != 2 {
				t.Fatalf("concurrent requests=%d, want 2", maxActive)
			}
			previous := -1
			for _, name := range []string{"a", "b", "c", "d"} {
				index := strings.Index(string(result.output), "file-"+name)
				if index <= previous {
					t.Fatalf("output is not in discovery order:\n%s", result.output)
				}
				previous = index
			}
		})
		t.Run(lane+"/worker termination is reported and other files finish", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				pid, err := strconv.Atoi(r.URL.Query().Get("pid"))
				if err != nil || pid <= 0 || pid == os.Getpid() {
					http.Error(w, "invalid worker pid", 400)
					return
				}
				worker, err := os.FindProcess(pid)
				if err == nil {
					err = worker.Kill()
				}
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				w.WriteHeader(200)
			}))
			defer server.Close()
			dir := writeTestCorpus(t, map[string]string{
				"a_crash_test.gb": fmt.Sprintf(`import test; import http; import process;
class CrashTest extends test.Test { @test func crash(): void { http.get(%q + "?pid=${process.pid()}"); } }`, server.URL),
				"b_pass_test.gb": passingTestFile,
			})
			output, err := run(t, "--jobs", "2", "--runtime", lane, dir)
			if err == nil {
				t.Fatal("expected terminated worker failure")
			}
			for _, want := range []string{"a_crash_test.gb", "no valid result", "tests: total=2 failed=0 passed=2 skipped=0"} {
				if !strings.Contains(output, want) {
					t.Fatalf("missing %q:\n%s", want, output)
				}
			}
		})
	}
}
