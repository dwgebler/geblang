package semantic_test

import (
	"strings"
	"testing"
)

const typeParamValueDecls = `class User { int id = 0; }
func make<T>(): T { return T(); }
func viaArg<T>(T sample): T { return T(); }
func viaList<T>(list<T> samples): T { return T(); }
func testsOnly<T>(any v): bool { return v instanceof T; }
`

func TestUninferableTypeParamValueCallIsAnError(t *testing.T) {
	diags := analyzeWith(t, typeParamValueDecls+"make();\n", nil)
	want := "type parameter T of make cannot be inferred; pass a type argument"
	for _, d := range diags {
		if strings.Contains(d.Message, want) {
			return
		}
	}
	t.Fatalf("want a diagnostic containing %q, got %v", want, diags)
}

func TestInferableOrUnusedTypeParamCallsAreAccepted(t *testing.T) {
	src := typeParamValueDecls + `make<User>();
viaArg(User());
viaList([User()]);
testsOnly(1);
func shadowed(): void {
    let make = func(): int { return 1; };
    make();
}
`
	for _, d := range analyzeWith(t, src, nil) {
		if strings.Contains(d.Message, "cannot be inferred") {
			t.Fatalf("unexpected diagnostic: %v", d)
		}
	}
}
