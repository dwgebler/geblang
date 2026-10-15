package bytecode_test

import "testing"

const decoratorFailurePrelude = `func wrap(any next, int count): any { return next; }
func bad(any next): any { return "nope"; }
func two(any next, int a): any { return next; }
func two(any next, string a, int b): any { return next; }
func boom(any next): any { throw ValueError("boom"); }
func tag(any cls, int n): any { return cls; }
`

func TestUncaughtDecoratorFailuresMatchAcrossBackends(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{"@wrap(\"bad\")\nfunc greet(): string { return \"Ada\"; }\n", "uncaught TypeError: decorator @wrap cannot be called for greet: wrap expects int for parameter 'count', got string\n  at <top level> (line 9)"},
		{"@wrap\nfunc greet(): string { return \"Ada\"; }\n", "uncaught RuntimeError: decorator @wrap cannot be called for greet: no matching overload for wrap\n  at <top level> (line 9)"},
		{"@two(true)\nfunc greet(): string { return \"Ada\"; }\n", "uncaught RuntimeError: decorator @two cannot be called for greet: no matching overload for two\n  at <top level> (line 9)"},
		{"@bad\nfunc greet(): string { return \"Ada\"; }\n", "uncaught RuntimeError: decorator @bad must return function, got string\n  at <top level> (line 9)"},
		{"@boom\nfunc greet(): string { return \"Ada\"; }\n", "uncaught ValueError: decorator @boom: boom\n  at <top level> (line 9)"},
		{"class S {\n    @wrap(\"bad\")\n    func m(): string { return \"m\"; }\n}\n", "uncaught TypeError: decorator @wrap cannot be called for m: wrap expects int for parameter 'count', got string\n  at <top level> (line 10)"},
		{"@tag(\"x\")\nclass A {}\n", "uncaught TypeError: decorator @tag cannot be called for class A: tag expects int for parameter 'n', got string\n  at <top level> (line 9)"},
	}
	for _, tc := range cases {
		ev, vm := uncaughtOnBothBackends(t, decoratorFailurePrelude+"\n"+tc.body)
		if ev != tc.want || vm != tc.want {
			t.Fatalf("decorator failure for %q:\nevaluator: %q\nvm: %q\nwant: %q", tc.body, ev, vm, tc.want)
		}
	}
}
