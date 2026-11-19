package check

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckArityOfMethodsInheritedFromTestBase(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := `import test;
import test as tt;
from test import Test as Base;

class A extends test.Test {
    func ok(?int maybe): void {
        this.assertTrue(true);
        this.assertNull(maybe);
        this.assertEquals(null, maybe);
        this.assertThrows(() => 1);
        this.assertThrows(() => 1, "x");
        this.isTrue(true);
        this.fail();
        this.skip("why");
        this.helper();
    }

    func helper(): void {}

    func bad(): void {
        this.assertTrue(true, "message");
    }
}

class B extends tt.Test {
    func bad(): void {
        this.assertEquals(1);
    }
}

class C extends Base {
    func bad(): void {
        this.assertThrows();
    }
}

class D extends A {
    func bad(): void {
        this.helper(1);
    }
}
`
	_, diags := Source(file, src, classCheckOpts(dir))
	want := map[string]bool{"A.assertTrue": false, "B.assertEquals": false, "C.assertThrows": false, "D.helper": false}
	for _, d := range diags {
		matched := false
		for key := range want {
			if strings.Contains(d.Message, "no matching overload for "+key+":") {
				want[key] = true
				matched = true
			}
		}
		if !matched {
			t.Errorf("unexpected diagnostic: %s", d.Message)
		}
	}
	for key, seen := range want {
		if !seen {
			t.Errorf("missing argument-count error for %s; diagnostics: %v", key, diags)
		}
	}
}
