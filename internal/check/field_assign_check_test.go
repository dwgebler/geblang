package check

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"geblang/internal/modules"
)

func fieldAssignDiagnostics(t *testing.T, source string) []Diagnostic {
	t.Helper()
	dir := t.TempDir()
	donor := "module accounts;\n\nexport class Base {\n    int count;\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "accounts.gb"), []byte(donor), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true}
	_, diags := Source(filepath.Join(dir, "main.gb"), source, opts)
	return diags
}

func TestStaticFieldAssignmentMismatchIsError(t *testing.T) {
	source := "class A {\n    int n;\n    ?string note;\n    float rate;\n}\nlet a = A();\na.n = \"x\";\na.n = null;\na.rate = 0.5;\n"
	diags := fieldAssignDiagnostics(t, source)
	for _, want := range []string{
		"7:3:cannot assign string to int field A.n",
		"8:3:cannot assign null to int field A.n",
		"9:3:cannot assign decimal to float field A.rate",
	} {
		found := false
		for _, d := range diags {
			got := strings.Join([]string{strconv.Itoa(d.Line), strconv.Itoa(d.Column), d.Message}, ":")
			if d.Severity == SeverityError && got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %q in %+v", want, diags)
		}
	}
}

func TestStaticFieldAssignmentAllowsUnknownsAndLiterals(t *testing.T) {
	source := "class A {\n    int n;\n    ?string note;\n    float rate;\n    decimal total;\n}\n" +
		"class Box<T> {\n    T value;\n}\n" +
		"let a = A();\nany dyn = \"d\";\na.n = dyn;\na.note = null;\na.rate = 2;\na.total = 0;\n" +
		"let b = Box<int>();\nb.value = \"s\";\n"
	for _, d := range fieldAssignDiagnostics(t, source) {
		if d.Severity == SeverityError && strings.Contains(d.Message, "field") {
			t.Fatalf("unexpected field error: %+v", d)
		}
	}
}

func TestStaticFieldAssignmentSeesCrossModuleParent(t *testing.T) {
	source := "import accounts;\n\nclass Child extends accounts.Base {\n}\nlet c = Child();\nc.count = \"many\";\n"
	diags := fieldAssignDiagnostics(t, source)
	if !hasDiag(diags, "semantic", "cannot assign string to int field Child.count") {
		t.Fatalf("expected cross-module field error, got %+v", diags)
	}
}

func TestStaticFieldAssignmentSkipsUnionAliases(t *testing.T) {
	source := "type Label = int | string;\nclass H {\n    Label l;\n    func H() { this.l = 1; }\n}\nlet h = H();\nh.l = \"s\";\n"
	for _, d := range fieldAssignDiagnostics(t, source) {
		if d.Severity == SeverityError {
			t.Fatalf("unexpected error: %+v", d)
		}
	}
}
