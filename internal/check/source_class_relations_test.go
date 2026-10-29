package check_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/check"
)

func TestSourceClassAssignment_FollowsImportedAncestorsAndRejectsUnrelatedTypes(t *testing.T) {
	dir := t.TempDir()
	for name, source := range map[string]string{
		"base.gb":  `module base; export class Root {} export class Middle extends Root {}`,
		"other.gb": `module other; export class Root {}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(dir, "main.gb")
	for _, tc := range []struct {
		name, statement string
		rejected        bool
	}{
		{"imported ancestor", "b.Root value = Leaf();", false},
		{"unrelated module", "o.Root value = Leaf();", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `import base as b; import other as o; class Leaf extends b.Middle {} ` + tc.statement

			_, diags := check.Source(file, source, check.Options{Resolver: check.NewDefaultResolver(file), CrossModule: true})

			rejected := false
			for _, diag := range diags {
				if diag.Severity == check.SeverityError {
					if strings.Contains(diag.Message, "cannot assign") {
						rejected = true
					} else {
						t.Fatalf("unexpected diagnostic: %v", diag)
					}
				}
			}
			if rejected != tc.rejected {
				t.Fatalf("rejected=%v, want %v; diagnostics %v", rejected, tc.rejected, diags)
			}
		})
	}
}
