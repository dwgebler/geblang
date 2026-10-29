package bytecode_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// runMultiModuleParity runs mainSrc against the named temp modules on both backends through the real loader and asserts byte-identical output equal to want.
func runMultiModuleParity(t *testing.T, modules map[string]string, mainSrc, want string) {
	t.Helper()
	dir := t.TempDir()
	for name, src := range modules {
		if err := os.WriteFile(filepath.Join(dir, name+".gb"), []byte(src), 0o644); err != nil {
			t.Fatalf("write module %s: %v", name, err)
		}
	}

	p := parser.New(lexer.New(mainSrc))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator: unexpected error: %v", err)
	}
	if evOut.String() != want {
		t.Fatalf("evaluator output: got %q, want %q", evOut.String(), want)
	}

	chunk, err := bytecode.Compile(program, []byte(mainSrc), "parity")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var vmOut bytes.Buffer
	stateful := evaluator.NewWithArgsAndModulePaths(&vmOut, nil, []string{dir})
	loader := newHarnessLoader(&vmOut, stateful)
	loader.SetModulePaths([]string{dir})
	loader.SetMainChunk(chunk)
	vm := bytecode.NewVMWithModuleLoader(chunk, &vmOut, loader)
	loader.SetMainVM(vm)
	vm.SetModulePaths([]string{dir})
	vm.SetStatefulNativeCaller(stateful)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm: unexpected error: %v", err)
	}
	if vmOut.String() != want {
		t.Fatalf("vm output: got %q, want %q", vmOut.String(), want)
	}
}

// Direct-entry (redesign Phase 3): a cross-module free function with args returns its value.
func TestParityDirectEntryFunctionArgs(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"mathx": "module mathx;\nexport func addmul(int a, int b): int { return (a + b) * 2; }\n",
	}, "import io;\nimport mathx;\nio.println(mathx.addmul(3, 4));\n", "14\n")
}

// Direct entry with default + variadic parameters resolves args identically.
func TestParityDirectEntryDefaultsVariadic(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"v": "module v;\nexport func greet(string name, string sep = \": \"): string { return name + sep; }\nexport func total(int... ns): int { let s = 0; for (n in ns) { s = s + n; } return s; }\n",
	}, "import io;\nimport v;\nio.println(v.greet(\"a\"));\nio.println(v.greet(\"b\", \"! \"));\nio.println(v.total(1, 2, 3, 4));\n", "a: \nb! \n10\n")
}

// A cross-module direct-entry function that throws is caught by the host's try/catch.
func TestParityDirectEntryThrowCaught(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"boom": "module boom;\nexport func go(): void { throw RuntimeError(\"kaboom\"); }\n",
	}, "import io;\nimport boom;\ntry {\n    boom.go();\n} catch (Error e) {\n    io.println(\"caught: \" + e.message);\n}\nio.println(\"after\");\n", "caught: kaboom\nafter\n")
}

// A defer inside a direct-entry function fires on its return.
func TestParityDirectEntryDefer(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"d": "module d;\nimport io;\nexport func work(): int {\n    defer io.println(\"cleanup\");\n    io.println(\"working\");\n    return 7;\n}\n",
	}, "import io;\nimport d;\nio.println(d.work());\n", "working\ncleanup\n7\n")
}

// Recursion inside a direct-entry callee (same-module self-calls entered cross-module).
func TestParityDirectEntryRecursion(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"rec": "module rec;\nexport func fib(int n): int { if (n < 2) { return n; } return fib(n - 1) + fib(n - 2); }\n",
	}, "import io;\nimport rec;\nio.println(rec.fib(10));\n", "55\n")
}

// A direct-entry function in module a calls into module b (cross-module nested in cross-module).
func TestParityDirectEntryReentry(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"alib": "module alib;\nimport blib;\nexport func start(int n): int { return blib.twice(n) + 1; }\n",
		"blib": "module blib;\nexport func twice(int n): int { return n * 2; }\n",
	}, "import io;\nimport alib;\nio.println(alib.start(5));\n", "11\n")
}

// A decorated cross-module function still applies its decorator (direct entry excludes decorated functions, so they take the wrapper fallback).
func TestParityDirectEntryDecoratedFallback(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"deco": "module deco;\nfunc prefix(any next, string label): any {\n    return func(string name): string { return label + next(name); };\n}\nexport @prefix(\"Hello, \")\nfunc greet(string name): string { return name; }\n",
	}, "import io;\nimport deco;\nio.println(deco.greet(\"Ada\"));\nio.println(deco.greet(\"Bob\"));\n", "Hello, Ada\nHello, Bob\n")
}

// Direct entry (Phase 4.2): a cross-module function returns a closure capturing an upvalue; the host invokes it.
func TestParityDirectEntryClosureFromModule(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"clo": "module clo;\nexport func adder(int base): func {\n    return func(int x): int { return base + x; };\n}\n",
	}, "import io;\nimport clo;\nlet add5 = clo.adder(5);\nio.println(add5(3));\nio.println(add5(10));\n", "8\n15\n")
}

// A host closure passed into a cross-module function is invoked back in the host's context.
func TestParityDirectEntryClosureCallback(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"runner": "module runner;\nexport func apply(int x, any cb): int { return cb(x); }\n",
	}, "import io;\nimport runner;\nlet double = func(int n): int { return n * 2; };\nio.println(runner.apply(21, double));\n", "42\n")
}

// Direct entry (Phase 4.4): direct construction of an imported class, then a method on the instance.
func TestParityDirectEntryConstruct(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"shape": "module shape;\nexport class Point {\n    int x;\n    int y;\n    func Point(int x, int y) { this.x = x; this.y = y; }\n    func sum(): int { return this.x + this.y; }\n}\n",
	}, "import io;\nimport shape;\nlet p = shape.Point(3, 4);\nio.println(p.sum());\n", "7\n")
}

// Direct construction of an imported generic class with explicit type args.
func TestParityDirectEntryConstructTypeArgs(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"boxmod": "module boxmod;\nexport class Box<T> {\n    T v;\n    func Box(T v) { this.v = v; }\n    func get(): T { return this.v; }\n}\n",
	}, "import io;\nimport boxmod;\nlet b = boxmod.Box<int>(42);\nio.println(b.get());\n", "42\n")
}

// A cross-module constructor that throws is caught by the host's try/catch.
func TestParityDirectEntryConstructorThrows(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"guard": "module guard;\nexport class Positive {\n    int v;\n    func Positive(int v) {\n        if (v <= 0) { throw RuntimeError(\"must be positive\"); }\n        this.v = v;\n    }\n}\n",
	}, "import io;\nimport guard;\ntry {\n    let p = guard.Positive(-1);\n    io.println(p.v);\n} catch (Error e) {\n    io.println(\"caught: \" + e.message);\n}\nio.println(\"after\");\n", "caught: must be positive\nafter\n")
}

// Direct entry (Phase 4.5): a cross-module static method runs on the exclusive worker via CallStaticMethodFast.
func TestParityDirectEntryStaticMethod(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"mk": "module mk;\nexport class Factory {\n    static func create(int n): int { return n * 100; }\n}\n",
	}, "import io;\nimport mk;\nio.println(mk.Factory.create(5));\n", "500\n")
}

// Direct entry (Phase 4.3): a cross-module instance dunder (__string) invoked via the native instance-invoker takes the loader's exclusive-worker direct path (CallMethodFast).
func TestParityDirectEntryCrossModuleDunder(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"dunder_fixture": "module dunder_fixture;\nexport class Money {\n    int cents;\n    func Money(int c) { this.cents = c; }\n    func __string(): string { return \"$\" + (this.cents as string); }\n}\nexport func make(int c): Money { return Money(c); }\n",
	}, "import io;\nimport dunder_fixture;\nlet m = dunder_fixture.make(150);\nio.println(\"price=${m}\");\n", "price=$150\n")
}

// Phase 4.6: a cross-module generator function is excluded from direct entry and runs on its own worker (lazyGenerator); the host iterates it across the module boundary.
func TestParityDirectEntryCrossModuleGenerator(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"gen": "module gen;\nexport func count(int n): generator<int> {\n    let i = 1;\n    while (i <= n) { yield i; i = i + 1; }\n}\n",
	}, "import io;\nimport gen;\nfor (x in gen.count(3)) { io.println(x); }\n", "1\n2\n3\n")
}

// Phase 4.6: a cross-module function invoked inside an async task (the async worker direct-enters it).
func TestParityDirectEntryCrossModuleAsync(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"calc": "module calc;\nexport func square(int n): int { return n * n; }\n",
	}, "import io;\nimport async;\nimport calc;\nlet task = async.run(func(): int { return calc.square(9); });\nio.println(async.await(task));\n", "81\n")
}

// Review #4: a returned instance's method dispatches via a data-only wrapper, so it still works after the constructing worker has been recycled by intervening cross-module calls.
func TestParityCrossModuleReturnedInstanceMethodAfterRecycle(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"widget": "module widget;\nexport class Widget {\n    int v;\n    func Widget(int v) { this.v = v; }\n    func value(): int { return this.v; }\n}\nexport func make(int v): Widget { return Widget(v); }\nexport func churn(): int { return 0; }\n",
	}, "import io;\nimport widget;\nlet w = widget.make(42);\nwidget.churn();\nwidget.churn();\nio.println(w.value());\n", "42\n")
}

// Review #4: an inherited method on a returned cross-module instance dispatches via the data-only wrapper.
func TestParityCrossModuleReturnedInstanceInheritedMethod(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"shapes": "module shapes;\nclass Base {\n    int v;\n    func Base(int v) { this.v = v; }\n    func describe(): string { return \"v=\" + (this.v as string); }\n}\nexport class Sub extends Base {\n    func Sub(int v) { parent(v); }\n}\nexport func make(int v): Sub { return Sub(v); }\n",
	}, "import io;\nimport shapes;\nlet s = shapes.make(7);\nio.println(s.describe());\n", "v=7\n")
}

// Phase 5: concurrent method calls on a SHARED cross-module instance must each run on their own exclusive worker (no shared mutable execution state). Run under -race.
func TestParityCrossModuleSharedInstanceConcurrent(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"shared": "module shared;\nexport class Acc {\n    func Acc() {}\n    func compute(int n): int { return n * 3; }\n}\nexport func make(): Acc { return Acc(); }\n",
	}, "import io;\nimport async;\nimport shared;\nlet obj = shared.make();\nlet tasks = [];\nfor (int i = 0; i < 8; i++) {\n    let n = i;\n    tasks = tasks.push(async.run(func(): int { return obj.compute(n); }));\n}\nio.println(async.await(async.all(tasks)));\n", "[0, 3, 6, 9, 12, 15, 18, 21]\n")
}

// Phase 6.2: a cross-module function reads a module-level global initialized at load; the cached VMValue snapshot must restore it correctly on every call.
func TestParityCrossModuleGlobalReadFromSnapshot(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"cfg": "module cfg;\nlet base = 100;\nexport func scaled(int n): int { return base + n; }\n",
	}, "import io;\nimport cfg;\nio.println(cfg.scaled(5));\nio.println(cfg.scaled(7));\n", "105\n107\n")
}

// A direct-entry function returns an instance whose method the host then calls.
func TestParityDirectEntryReturnsInstance(t *testing.T) {
	runMultiModuleParity(t, map[string]string{
		"box": "module box;\nexport class Boxed {\n    int v;\n    func Boxed(int v) { this.v = v; }\n    func get(): int { return this.v; }\n}\nexport func make(int v): Boxed { return Boxed(v); }\n",
	}, "import io;\nimport box;\nlet b = box.make(5);\nio.println(b.get());\n", "5\n")
}
