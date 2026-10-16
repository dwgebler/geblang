package bytecode_test

import (
	"os"
	"path/filepath"
	"testing"
)

const aspectsDonorGb = `module aspects;
export class Transient extends RuntimeError {
    func Transient(string m) { parent(m); }
}
export func retry(callable next, int attempts): callable {
    return func(any ...args): any {
        int attempt = 1;
        while (true) {
            try {
                return next(...args);
            } catch (Transient e) {
                if (attempt >= attempts) { throw e; }
                attempt += 1;
            }
        }
    };
}
export func tagged(callable next, string prefix = "", string suffix = ""): callable {
    return func(any ...args): any { return prefix + next(...args) + suffix; };
}
export func trimmed(string v): string { return v.trim(); }
export func atLeast(int min, int v): int {
    if (v < min) { throw ValueError("below ${min}"); }
    return v;
}
export func registered(any cls): any { return cls; }
`

const shopDonorGb = `module shop;
from aspects import retry, trimmed, atLeast, Transient;
from aspects import tagged as label;
export class Flaky {
    @trimmed
    string name;
    @atLeast(1)
    int failures;
    int calls = 0;
    func Flaky(string name, int failures) { this.name = name; this.failures = failures; }
    @retry(3)
    func fetch(string what): string {
        this.calls += 1;
        if (this.calls <= this.failures) { throw Transient("busy"); }
        return "${this.name}:${what}:${this.calls}";
    }
    @label(prefix: "<", suffix: ">")
    func shout(): string { return this.name.upper(); }
}
`

const importedDecoratorsMain = `import io;
import shop;
import aspects;
from aspects import tagged, registered;
@tagged("[", "]")
func greet(string who): string { return "hi " + who; }
@registered
class Local {}
let f = shop.Flaky("  box ", 2);
io.println("[${f.name}]");
io.println(f.fetch("rates"));
io.println(f.shout());
io.println(greet("ada"));
try { shop.Flaky("x", 0); } catch (ValueError e) { io.println("rejected: ${e.message}"); }
let g = shop.Flaky("y", 5);
try { g.fetch("rates"); } catch (aspects.Transient e) { io.println("gave up after ${g.calls}"); }
io.println(Local() instanceof Local);
`

func TestParityImportedDecoratorsApplyAcrossModules(t *testing.T) {
	dir := t.TempDir()
	for name, src := range map[string]string{"aspects.gb": aspectsDonorGb, "shop.gb": shopDonorGb} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	runParityModulesDir(t, dir, importedDecoratorsMain,
		"[box]\nbox:rates:3\n<BOX>\n[hi ada]\nrejected: below 1\ngave up after 3\ntrue\n")
}
