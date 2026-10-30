package semantic_test

import (
	"strings"
	"testing"
)

func TestParameterDefaultSeesOnlyEarlierParameters(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"function own name", "int x = 5;\nfunc f(string x = x): string { return x; }\n", "cannot use int default for string parameter x"},
		{"closure own name", "int x = 5;\nlet f = func(string x = x): string { return x; };\n", "cannot use int default for string parameter x"},
		{"method own name", "int x = 5;\nclass C { func m(string x = x): string { return x; } }\n", "cannot use int default for string parameter x"},
		{"earlier parameter", "func g(int a = 1, int b = a + 1): int { return b; }\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diagnostics := analyzeInput(t, tc.src)
			if tc.want == "" {
				if len(diagnostics) != 0 {
					t.Fatalf("expected no diagnostics, got %v", diagnostics)
				}
				return
			}
			for _, d := range diagnostics {
				if strings.Contains(d.Message, tc.want) {
					return
				}
			}
			t.Fatalf("missing %q in %v", tc.want, diagnostics)
		})
	}
}
