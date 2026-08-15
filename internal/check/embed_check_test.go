package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckEmbedDiagnostics(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	_, diags := Source(file, `let x = embed("missing.txt");`+"\n", Options{})
	found := false
	for _, d := range diags {
		if d.Rule == "embed" && strings.Contains(d.Message, "embed cannot read") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing-file embed produced no embed diagnostic: %+v", diags)
	}
}

// TestCheckEmbedCascadeSuppressed: the embed diagnostic survives, its derivative semantic/type diagnostics don't.
func TestCheckEmbedCascadeSuppressed(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	_, diags := Source(file, "let x = embed(\"no/such/file.txt\");\n", Options{})
	haveEmbed := false
	for _, d := range diags {
		if d.Rule == "embed" {
			haveEmbed = true
		}
		if d.Rule == "semantic" && d.Message == `unknown function "embed"` {
			t.Errorf("cascade not suppressed: got semantic unknown-function diagnostic: %+v", d)
		}
		if d.Rule == "type" && strings.Contains(d.Message, "unknown bytecode function embed") {
			t.Errorf("cascade not suppressed: got type unknown-bytecode-function diagnostic: %+v", d)
		}
	}
	if !haveEmbed {
		t.Errorf("missing-file embed produced no embed diagnostic: %+v", diags)
	}
}

// TestCheckUnknownFunctionStillReported: control - a genuinely unknown function must still be flagged.
func TestCheckUnknownFunctionStillReported(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	_, diags := Source(file, "let x = embedx(\"a.txt\");\n", Options{})
	found := false
	for _, d := range diags {
		if d.Rule == "semantic" && d.Message == `unknown function "embedx"` {
			found = true
		}
	}
	if !found {
		t.Errorf("unknown function embedx was not flagged: %+v", diags)
	}
}

func TestCheckEmbedValidTypesAsString(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.gb")
	_, diags := Source(file, `let string s = embed("a.txt");`+"\n", Options{})
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Errorf("valid embed produced error: %+v", d)
		}
	}
}

func TestCheckEmbedCascadeSuppressesExactFunctionOnly(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	_, diags := Source(file, `let y = embedHelper(); let x = embed("missing.txt");`+"\n", Options{})
	haveEmbed := false
	haveEmbedHelper := false
	for _, d := range diags {
		if d.Rule == "embed" {
			haveEmbed = true
		}
		if strings.Contains(d.Message, "embedHelper") {
			haveEmbedHelper = true
		}
	}
	if !haveEmbed {
		t.Errorf("missing embed diagnostic: %+v", diags)
	}
	if !haveEmbedHelper {
		t.Errorf("embedHelper unknown function not reported (incorrectly suppressed by embed cascade): %+v", diags)
	}
}
