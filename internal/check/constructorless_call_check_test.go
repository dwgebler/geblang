package check

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckArgumentsToConstructorlessClass(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := `import test;

class E extends ValueError {}
class F extends E {}
class Base {
    func Base(int a) {}
}
class Child extends Base {}
class GrandChild extends Child {}
class Plain {
    int n = 0;
}
@dataclass
class DC {
    int x;
}
class T extends test.Test {}
class WithCtor {
    func WithCtor(int a) {}
}

let fine = [E("m"), F("m"), DC(1), Plain(), WithCtor(1), T(), Plain(...[])];
let a = Child(1);
let b = GrandChild(1, 2);
let c = Plain(5);
func shadowed(): any {
    let Plain = (int n) => n;
    return Plain(3);
}
`
	_, diags := Source(file, src, classCheckOpts(dir))
	want := map[string]bool{"Child": false, "GrandChild": false, "Plain": false}
	for _, d := range diags {
		if d.Severity != SeverityError {
			continue
		}
		matched := false
		for name := range want {
			if d.Message == name+" constructor expects no arguments" {
				want[name] = true
				matched = true
			}
		}
		if !matched {
			t.Errorf("unexpected error: %s", d.Message)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("missing error for %s; diagnostics: %v", name, diags)
		}
	}
	if n := strings.Count(strings.Join(diagMessages(diags), "\n"), "Plain constructor expects no arguments"); n != 1 {
		t.Errorf("want exactly one Plain error (the shadowed call is not a construction), got %d", n)
	}
}

func diagMessages(diags []Diagnostic) []string {
	out := make([]string, len(diags))
	for i, d := range diags {
		out[i] = d.Message
	}
	return out
}
