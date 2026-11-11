package semantic_test

import (
	"testing"

	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/semantic"
)

func TestInferredGenericCallUsesInferredSignatureInTypedContext(t *testing.T) {
	const id = `func id<T>(T value): T { return value; } `
	for _, tc := range []struct {
		name   string
		source string
		valid  bool
	}{
		{"declaration", id + `int n = id(17);`, true},
		{"return", id + `func f(): int { return id(17); }`, true},
		{"any return", id + `func f(): any { return id(17); }`, true},
		{"callback", id + `let f = func(): int { return id(17); };`, true},
		{"nullable", id + `func f(): ?int { return id(17); }`, true},
		{"collection", `func wrap<T>(T value): list<T> { return [value]; } list<int> xs = wrap(3);`, true},
		{"two params", `func pair<K, V>(K k, V v): dict<K, V> { return {k: v}; } dict<string, int> d = pair("k", 1);`, true},
		{"nested arg", `func first<T>(list<T> xs): T { return xs[0]; } int n = first([1, 2]);`, true},
		{"uninferable stays open", `func make<T>(): ?T { return null; } ?int n = make();`, true},
		{"wrong declaration", id + `string s = id(17);`, false},
		{"wrong collection", `func wrap<T>(T value): list<T> { return [value]; } list<string> xs = wrap(3);`, false},
		{"wrong nested arg", `func first<T>(list<T> xs): T { return xs[0]; } string s = first([1, 2]);`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := parser.New(lexer.New(tc.source))
			program := p.ParseProgram()
			if len(p.Errors()) != 0 {
				t.Fatal(p.Errors())
			}

			diagnostics := semantic.New().Analyze(program)

			if (len(diagnostics) == 0) != tc.valid {
				t.Fatalf("valid=%v, diagnostics=%v", tc.valid, diagnostics)
			}
		})
	}
}

func TestExplicitGenericCallSeedsInferenceBindings(t *testing.T) {
	const classes = `interface Priced { func price(): int; } class Animal { int legs = 4; } class Dog extends Animal {} class Cat extends Animal {} class Rock {} `
	const both = `func both<T>(T a, T b): list<T> { return [a, b]; } `
	for _, tc := range []struct {
		name   string
		source string
		valid  bool
	}{
		{"explicit supertype", classes + both + `list<Animal> xs = both<Animal>(Dog(), Cat());`, true},
		{"explicit subtype mismatch", classes + both + `let xs = both<Dog>(Dog(), Cat());`, false},
		{"inferred siblings", classes + both + `let xs = both(Dog(), Cat());`, false},
		{"explicit constraint", classes + `func cost<T implements Priced>(T x): int { return 1; } int n = cost<Rock>(Rock());`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := parser.New(lexer.New(tc.source))
			program := p.ParseProgram()
			if len(p.Errors()) != 0 {
				t.Fatal(p.Errors())
			}

			diagnostics := semantic.New().Analyze(program)

			if (len(diagnostics) == 0) != tc.valid {
				t.Fatalf("valid=%v, diagnostics=%v", tc.valid, diagnostics)
			}
		})
	}
}

func TestAnyArgumentsDeferTypedContextCallChecks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		valid  bool
	}{
		{"collection param", `any bad = ["x"]; func q(list<int> xs): int { return 1; } int n = q(bad);`, true},
		{"generic param", `any one = 1; any bad = ["x"]; func p<T>(T s, list<T> xs): int { return 1; } let f = func(): int { return p(one, bad); };`, true},
		{"known mismatch", `func q(list<int> xs): int { return 1; } int n = q(["x"]);`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := parser.New(lexer.New(tc.source))
			program := p.ParseProgram()
			if len(p.Errors()) != 0 {
				t.Fatal(p.Errors())
			}

			diagnostics := semantic.New().Analyze(program)

			if (len(diagnostics) == 0) != tc.valid {
				t.Fatalf("valid=%v, diagnostics=%v", tc.valid, diagnostics)
			}
		})
	}
}

func TestAnyArgumentNeitherBindsNorConflicts(t *testing.T) {
	for _, source := range []string{
		`func id<T>(T value): T { return value; } any x = 1; let r = id<string>(x);`,
		`func both<T>(T a, T b): int { return 1; } any x = 1; int n = both(x, "s");`,
	} {
		p := parser.New(lexer.New(source))
		program := p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Fatal(p.Errors())
		}
		if diagnostics := semantic.New().Analyze(program); len(diagnostics) != 0 {
			t.Fatalf("%s: %v", source, diagnostics)
		}
	}
}
