package semantic_test

import (
	"strings"
	"testing"
)

func TestNativeCallArityIsCheckedByTheAnalyzer(t *testing.T) {
	cases := map[string]string{
		"import math;\nlet x = math.sqrt(4.0, 1);\n":           "math.sqrt(value) does not accept 2 argument(s)",
		"import random;\nlet x = random.choice();\n":           "random.choice(items) does not accept 0 argument(s)",
		"import math as m;\nlet x = m.pow(2.0);\n":             "math.pow(",
		"import watch;\nlet x = watch.wait(\"a\", 1, 2, 3);\n": "watch.wait(path, timeoutMs, intervalMs) does not accept 4 argument(s)",
	}
	for src, want := range cases {
		diags := analyzeWith(t, src, nil)
		found := false
		for _, d := range diags {
			if strings.Contains(d.Message, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: want a diagnostic containing %q, got %v", src, want, diags)
		}
	}
}

func TestNativeCallArityAcceptsEveryCatalogForm(t *testing.T) {
	src := `import random;
import watch;
import collections;
import datetime;
import errors;
let g = random.Generator(1);
let a = random.choice(g, [1]);
let b = random.choice([1]);
let c = random.next(g);
let d = random.float(g);
let e = random.bool(g);
let f = random.shuffle(g, [1]);
let w = watch.wait("x", 10);
let r = collections.range(4);
let s = datetime.toLocal(0, "UTC");
let n = errors.new("ValueError");
let shadowed = func(any random): any { return random.choice(); };
`
	for _, d := range analyzeWith(t, src, nil) {
		if strings.Contains(d.Message, "does not accept") {
			t.Errorf("unexpected arity diagnostic: %v", d)
		}
	}
}
