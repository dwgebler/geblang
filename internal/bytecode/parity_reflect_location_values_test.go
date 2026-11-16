package bytecode_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParityReflectLocationFunctionValues(t *testing.T) {
	dir := t.TempDir()
	fixture, err := os.ReadFile("../../tests/reflect/location_values_donor.gb")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "location_values_donor.gb"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}
	source := `import io;
import reflect;
import math;
import location_values_donor as donor;
class Base {
    func own(): int { return 1; }
    static func staticOwn(): int { return 2; }
    func literal(): callable { return func(): int { return 3; }; }
}
class Sub extends Base {}
func probe(any target): void {
    let location = reflect.location(target);
    io.println(location);
    io.println(donor.inspect(target) == location);
}
probe(func(): int { return 1; });
probe(donor.literal(2));
probe(Base().literal());
probe(Sub().literal());
probe(donor.Sub().literal());
for (obj in [Base(), Sub()]) {
    probe(reflect.method(obj, "own"));
    probe(obj.own);
}
for (obj in [donor.Base(), donor.Sub()]) {
    probe(reflect.method(obj, "own"));
    probe(obj.own);
}
probe(reflect.staticMethod(Base, "staticOwn"));
probe(reflect.staticMethod(donor.Base, "staticOwn"));
probe(donor.Sub().extra);
io.println(reflect.location(math.abs) == null);
`

	ev, vm := parityOutputsModulesDir(t, dir, source)

	if ev != vm {
		t.Fatalf("evaluator=%s\nVM=%s", ev, vm)
	}
	if strings.Contains(ev, "null") || strings.Contains(ev, "false") || !strings.Contains(ev, `"module": "location_values_donor"`) {
		t.Fatalf("missing function location: %s", ev)
	}
}
