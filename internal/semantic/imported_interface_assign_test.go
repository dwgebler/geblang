package semantic_test

import (
	"strings"
	"testing"
)

// A local class declaring an imported interface can be used where that
// interface is expected, though the interface is not in this module's
// tables (1.37.7); a class that does not declare it is still refused.
func TestLocalClassImplementingImportedInterfaceIsAssignable(t *testing.T) {
	src := `import shapes as sh;
class Tri implements sh.Shape {
    func area(): int { return 3; }
}
class Plain {
    func area(): int { return 1; }
}
list<sh.Shape> l = [];
l.push(Tri());
l.push(Plain());
`
	var got []string
	for _, d := range analyzeWith(t, src, nil) {
		if strings.Contains(d.Message, "push element") {
			got = append(got, d.Message)
		}
	}
	if len(got) != 1 || !strings.Contains(got[0], "Plain") {
		t.Fatalf("diagnostics: %v", got)
	}
}
