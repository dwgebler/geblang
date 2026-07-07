package bytecode_test

import "testing"

// F1: a select inside a closure must capture the channel as an upvalue (the free-var scan skipped SelectStatement).
func TestParitySelectInClosureCapturesChannel(t *testing.T) {
	runParityWithStdlib(t, `import io;
import async.channel as ch;
let c = ch.Channel<int>(1);
c.send(5);
let f = func(): int {
    select {
        case let v = c.recv(): { return v; }
        default: { return -1; }
    }
};
io.println(f());
`, "5\n")
}

// F2a: an async function used as a value inside a @test method returns a Task, as at top level.
func TestParityTestMethodAsyncValueReturnsTask(t *testing.T) {
	runParityWithStdlib(t, `import io;
import async;
import test;
async func triple(int x): int { return x * 3; }
class AsyncValT extends test.Test {
    @test
    func m(): void {
        let f = triple;
        let t = f(4);
        this.assertEquals("Task", "${typeof(t)}");
        this.assertEquals(12, async.await(t));
    }
}
let r = test.run(AsyncValT);
io.println((r["passed"] as string) + "/" + (r["failed"] as string));
`, "1/0\n")
}

// F2b: test.mock patches are visible to a @test method body and roll back before the next method.
func TestParityTestMethodMockAppliedAndRestored(t *testing.T) {
	runParityWithStdlib(t, `import io;
import datetime;
import test;
class MockT extends test.Test {
    @test
    func mocked(): void {
        test.mock("datetime", {"nowUnix": func(): int { return 42; }});
        this.assertEquals(42, datetime.nowUnix());
    }
    @test
    func realAfterRestore(): void {
        this.assertTrue(datetime.nowUnix() > 1_000_000_000);
    }
}
let r = test.run(MockT);
io.println((r["passed"] as string) + "/" + (r["failed"] as string));
`, "2/0\n")
}

// F2c: a nested test.run inside a @test method sees a module-global mutation the outer method made.
func TestParityTestMethodNestedRunSeesGlobalMutation(t *testing.T) {
	runParityWithStdlib(t, `import io;
import test;
int counter = 0;
class InnerT extends test.Test {
    @test
    func checks(): void { this.assertEquals(7, counter); }
}
class OuterT extends test.Test {
    @test
    func m(): void {
        counter = 7;
        let inner = test.run(InnerT);
        this.assertEquals(1, inner["passed"]);
    }
}
let r = test.run(OuterT);
io.println((r["passed"] as string) + "/" + (r["failed"] as string));
`, "1/0\n")
}

// A reflect method reference casts to a callable type on both backends.
func TestParityReflectMethodAsCallableCast(t *testing.T) {
	runParityWithStdlib(t, `import io;
import reflect;
class Greeter { func hello(): string { return "hi"; } }
let g = Greeter();
let m = reflect.method(g, "hello");
let f = m as callable;
io.println("cast ok");
`, "cast ok\n")
}

// Cross-module: reflect.staticMethod on an entry-module class discovered from an imported module resolves and is invocable.
func TestParityCrossModuleReflectStaticMethod(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "scan", `module scan;
import reflect;
export func scanAll(): list<string> {
    list<string> out = [];
    for (cls in reflect.classes()) {
        if (reflect.hasDecorator(cls, "Marker")) {
            let sm = reflect.staticMethod(cls, "repositoryClass");
            let nm = reflect.className(cls) as string;
            if (sm == null) {
                out = out.push(nm + ":NULL");
            } else {
                out = out.push(nm + ":" + (reflect.className(sm()) as string));
            }
        }
    }
    return out;
}
`)
	runParityModulesDir(t, dir, `import io;
import scan;
class Repo {}
@Marker
class Widget {
    static func repositoryClass(): any { return Repo; }
}
io.println("${scan.scanAll()}");
`, "[\"Widget:Repo\"]\n")
}
