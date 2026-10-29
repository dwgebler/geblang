package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareTestProgram_ValidatesFilesWithoutSelectedTests(t *testing.T) {
	for _, filter := range []string{"", "OtherTest"} {
		for _, source := range []string{
			`func bad(): int { return "wrong"; }`,
			`import test; class ExampleTest extends test.Test { @test func bad(): int { return "wrong"; } }`,
		} {
			dir := t.TempDir()
			path := filepath.Join(dir, "bad_test.gb")
			if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := prepareTestProgram(path, nil, filter, nil, "summary")

			if err == nil || !strings.Contains(err.Error(), "return") {
				t.Fatalf("filter %q: expected return type diagnostic, got %v", filter, err)
			}
		}
	}
}
