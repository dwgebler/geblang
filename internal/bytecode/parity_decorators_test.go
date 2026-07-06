package bytecode_test

// Parity tests run each snippet through both the evaluator and the VM and
// assert that the printed output matches.  They pin the intersection of
// features that both execution paths are expected to support identically.

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

// stdlibModuleLoader is a minimal bytecode-side module loader used by
// parity tests for source-distributed stdlib modules (time.scheduler,
// async.rate, etc.). It resolves canonical module paths via the standard
// resolver (which walks ancestors to find stdlib/), compiles each
// imported source module to bytecode on demand, and caches the result
// inside the loader struct.

func TestParityReflectDecoratorMetadata(t *testing.T) {
	runParity(t, `import io;
import reflect;

@route("GET", "/users", name: "users")
func index(): string {
    return "ok";
}

@service(name: "users")
class Controller {
    @route("POST", "/users")
    static func create(): string {
        return "created";
    }

    @route("GET", "/users")
    func list(): string {
        return "list";
    }
}

let fn = reflect.decorators(index);
io.println(fn[0]["name"]);
io.println(fn[0]["target"]);
io.println(fn[0]["args"][0]);
io.println(fn[0]["namedArgs"]["name"]);
io.println(reflect.hasDecorator(index, "ROUTE"));

let cls = reflect.decorators(Controller);
io.println(cls[0]["name"]);
io.println(cls[0]["target"]);

let method = reflect.method(Controller, "list");
io.println(reflect.decorator(method, "route")["target"]);
io.println(reflect.decorator(method, "route")["args"][0]);

let staticMethod = reflect.staticMethod(Controller, "create");
io.println(reflect.decorators(staticMethod)[0]["target"]);
io.println(reflect.decorators(staticMethod)[0]["args"][0]);
`, "route\nfunction\nGET\nusers\ntrue\nservice\nclass\nmethod\nGET\nstaticMethod\nPOST\n")
}

func TestParityCallableFunctionDecorators(t *testing.T) {
	runParity(t, `import io;
import reflect;

func suffix(any next, string mark): any {
    return func(string name): string { return next(name) + mark; };
}

func prefix(any next, string label): any {
    return func(string name): string { return label + next(name); };
}

@prefix(label: "Hello, ")
@suffix("!")
func greet(string name): string {
    return name;
}

io.println(greet("Ada"));
io.println(reflect.decorators(greet)[0]["name"]);
io.println(reflect.decorators(greet)[1]["name"]);
`, "Hello, Ada!\nprefix\nsuffix\n")
}

func TestParityCallableFunctionDecoratorOverloads(t *testing.T) {
	runParity(t, `import io;

func label(any next, string prefix): any {
    return func(string name): string { return prefix + next(name); };
}

func label(any next, int count): any {
    return func(string name): string {
        let out = next(name);
        for (let int i = 0; i < count; i++) {
            out = out + "!";
        }
        return out;
    };
}

@label("Hello, ")
func greet(string name): string {
    return name;
}

@label(3)
func cheer(string name): string {
    return name;
}

io.println(greet("Ada"));
io.println(cheer("Bob"));
`, "Hello, Ada\nBob!!!\n")
}

func TestParityCallableDecoratorsOnOverloadedTargets(t *testing.T) {
	runParity(t, `import io;

func prefixInt(any next, string label): any {
    return func(int value): string { return label + next(value); };
}

func prefixString(any next, string label): any {
    return func(string value): string { return label + next(value); };
}

@prefixInt("int:")
func describe(int value): string {
    return value as string;
}

@prefixString("string:")
func describe(string value): string {
    return value;
}

io.println(describe(7));
io.println(describe("Ada"));
`, "int:7\nstring:Ada\n")
}

func TestParityCallableMethodDecorators(t *testing.T) {
	runParity(t, `import io;
import reflect;

func suffix(any next, string mark): any {
    return func(): string {
        return next() + mark;
    };
}

func prefix(any next, string label): any {
    return func(string name): string {
        return label + next(name);
    };
}

class Greeter {
    string name;

    func Greeter(string name) {
        this.name = name;
    }

    @suffix("!")
    func greet(): string {
        return this.name;
    }

    @prefix("kind:")
    static func label(string name): string {
        return name;
    }
}

let greeter = Greeter("Ada");
io.println(greeter.greet());
io.println(Greeter.label("user"));
let method = reflect.method(Greeter, "greet");
io.println(reflect.decorators(method)[0]["name"]);
io.println(reflect.decorators(method)[0]["target"]);
let staticMethod = reflect.staticMethod(Greeter, "label");
io.println(reflect.decorators(staticMethod)[0]["name"]);
io.println(reflect.decorators(staticMethod)[0]["target"]);
`, "Ada!\nkind:user\nsuffix\nmethod\nprefix\nstaticMethod\n")
}

func TestParityCallableMethodDecoratorOverloadsHaveDistinctDecorators(t *testing.T) {
	runParity(t, `import io;

func tag(any next, string label): any {
    return func(string s): string { return label + ":" + next(s); };
}
func tagInt(any next, string label): any {
    return func(int n): string { return label + ":" + next(n); };
}

class Formatter {
    func Formatter() {}

    @tag("str")
    func format(string s): string {
        return s;
    }

    @tagInt("int")
    func format(int n): string {
        return n.toString();
    }
}

let f = Formatter();
io.println(f.format("hello"));
io.println(f.format(42));
`, "str:hello\nint:42\n")
}

func TestParityCallableMethodDecoratorMultipleDecoratorsCallNext(t *testing.T) {
	runParity(t, `import io;

func wrap(any next, string before, string after): any {
    return func(): string {
        return before + next() + after;
    };
}

class Service {
    string name;

    func Service(string name) {
        this.name = name;
    }

    @wrap("[", "]")
    @wrap("{", "}")
    func describe(): string {
        return this.name;
    }
}

let s = Service("core");
io.println(s.describe());
`, "[{core}]\n")
}

func TestParityCallableClassDecorators(t *testing.T) {
	runParity(t, `import io;
import reflect;

dict<string, any> registry = {};

func service(any cls, string name): any {
    registry[name] = cls;
    return cls;
}

@service("users")
class UserService {
    func greeting(): string {
        return "hello from users";
    }
}

let svc = UserService();
io.println(svc.greeting());
io.println(reflect.decorators(UserService)[0]["name"]);
io.println(reflect.decorators(UserService)[0]["args"][0]);
io.println(registry.hasKey("users"));
`, "hello from users\nservice\nusers\ntrue\n")
}

func TestParityCallableClassDecoratorsApplyBottomUp(t *testing.T) {
	runParity(t, `import io;

func tagA(any cls): any {
    io.println("A");
    return cls;
}

func tagB(any cls): any {
    io.println("B");
    return cls;
}

@tagA
@tagB
class Widget {}
`, "B\nA\n")
}

func TestParityUnknownClassDecoratorsAreMetadataOnly(t *testing.T) {
	runParity(t, `import io;
import reflect;

@controller("/api")
class ApiController {
    func ping(): string {
        return "pong";
    }
}

let c = ApiController();
io.println(c.ping());
io.println(reflect.decorators(ApiController)[0]["name"]);
io.println(reflect.decorators(ApiController)[0]["args"][0]);
`, "pong\ncontroller\n/api\n")
}

func TestParityReflectOverloadedDecoratorMetadata(t *testing.T) {
	runParity(t, `import io;
	import reflect;

@tag("int")
func describe(int value): string {
    return "int";
}

@tag("string")
func describe(string value): string {
    return "string";
}

let decorators = reflect.decorators(describe);
let tags = reflect.decorators(describe, "TAG");
io.println(decorators.length());
io.println(decorators[0]["args"][0]);
io.println(decorators[0]["overload"]);
io.println(decorators[1]["args"][0]);
io.println(decorators[1]["overload"]);
io.println(tags.length());
io.println(tags[0]["args"][0]);
io.println(tags[1]["args"][0]);
`, "2\nint\n0\nstring\n1\n2\nint\nstring\n")
}

func TestParityCallableDecoratorOnAsyncFunction(t *testing.T) {
	runParity(t, `import io;
import reflect;

func passthrough(any next): any {
    return func(string name): any {
        return next(name);
    };
}

@passthrough
async func greet(string name): string {
    return "hello " + name;
}

let task = greet("Ada");
io.println(typeof(task));
io.println(await task);
io.println(reflect.decorators(greet)[0]["name"]);
`, "Task\nhello Ada\npassthrough\n")
}

// TestParityClassDecoratorPatterns covers the three class-decorator
// shapes: register-in-place (return cls), swap-to-another-class
// (return DifferentClass), and wrap-as-callable (return a closure
// that becomes the new constructor). Prior to the fix the VM
// silently ignored swaps and both engines rejected callable returns.
func TestParityClassDecoratorPatterns(t *testing.T) {
	runParity(t, `import io;
import reflect;

func register(any cls): any {
    io.println("[register] " + reflect.className(cls));
    return cls;
}

@register
class Service {
    string name;
    func Service(string n) { this.name = n; }
}

io.println(Service("alpha").name);

class Replacement {
    string label;
    func Replacement(string n) { this.label = "replaced:" + n; }
    func describe(): string { return this.label; }
}

func swap(any cls): any { return Replacement; }

@swap
class Original {
    func Original(string n) {}
    func describe(): string { return "original"; }
}

io.println(Original("ada").describe());

func wrap(any cls): any {
    return func(string n): any {
        io.println("[wrap] " + n);
        return cls(n + "!");
    };
}

@wrap
class Greeter {
    string greeting;
    func Greeter(string n) { this.greeting = "Hello, " + n; }
    func say(): string { return this.greeting; }
}

io.println(Greeter("Ada").say());
`, `[register] Service
alpha
replaced:ada
[wrap] Ada
Hello, Ada!
`)
}

func TestParityParameterDecoratorsAreSurfacedByReflect(t *testing.T) {
	runParity(t, `import io;
import reflect;

@Get("/x")
func handler(
    @PathParam("id") string id,
    @QueryParam("limit") int limit = 10,
    @Header("X-Api-Key") @Inject string apiKey
): void { }

for (p in reflect.parameters(handler)) {
    let pd = p as dict<string, any>;
    string name = pd["name"] as string;
    if (!pd.contains("decorators")) {
        io.println(name + " no-decorators");
        continue;
    }
    let decs = pd["decorators"] as list<any>;
    let names = [];
    for (d in decs) {
        let dd = d as dict<string, any>;
        names = names.push(dd["name"] as string);
    }
    io.println(name + " " + names.join(","));
}
`, "id PathParam\nlimit QueryParam\napiKey Header,Inject\n")
}

// TestParityFieldCallableDecorators covers callable field decorators:
// every assignment (including constructor) flows through the decorator
// chain bottom-up; transforms can reshape the value, or throw to reject.
func TestParityFieldCallableDecorators(t *testing.T) {
	runParity(t, `import io;

func upper(string v): string { return v.upper(); }
func prefix(string p, string v): string { return p + v; }
func minLen(int min, string v): string {
    if (v.length() < min) { throw RuntimeError("too short"); }
    return v;
}

class User {
    @prefix("hello-")
    @minLen(2)
    @upper
    string name;
    func User(string n) { this.name = n; }
}

let u = User("ada");
io.println(u.name);

u.name = "bo";
io.println(u.name);

try { u.name = "x"; io.println("FAIL"); }
catch (RuntimeError e) { io.println(e.message); }
`, "hello-ADA\nhello-BO\ntoo short\n")
}

// TestParityClassDecoratorTypedDelegation covers the typed-delegation
// pattern: when a wrap closure on @storage returns an instance of
// a different class, the returned instance still satisfies
// `instanceof OriginalClass` via an auto-extended type list.
func TestParityClassDecoratorTypedDelegation(t *testing.T) {
	runParity(t, `import io;

class JsonRepository {
    string a;
    func JsonRepository(string a) { this.a = a; }
    func describe(): string { return "json:" + this.a; }
}

func storage(any cls): any {
    return func(string a): any { return JsonRepository(a); };
}

@storage
class UserRepository {
    func UserRepository(string a) {}
}

let ur = UserRepository("ada");
io.println(ur instanceof UserRepository);
io.println(ur instanceof JsonRepository);
io.println(ur.describe());
`, "true\ntrue\njson:ada\n")
}

// TestParityAbstractDecorator covers the @abstract class/method
// decorator. A class is abstract when either:
//   - it carries @abstract on the class itself, or
//   - it (or an ancestor) has a method decorated @abstract and no
//     more-derived class provides a concrete override.
//
// Direct instantiation of an abstract class throws RuntimeError;
// concrete subclasses instantiate normally.
func TestParityAbstractDecorator(t *testing.T) {
	runParity(t, `import io;

@abstract
class AbstractBase {
    func name(): string { return "base"; }
}
class Derived extends AbstractBase {}

class Shape {
    @abstract
    func area(): int { return 0; }
}
class Circle extends Shape {
    int r;
    func Circle(int r) { this.r = r; }
    func area(): int { return 3 * this.r * this.r; }
}
class Square extends Shape {}

try { AbstractBase(); io.println("FAIL: abstract class instantiated"); }
catch (RuntimeError e) { io.println(e.message); }

io.println(Derived().name());

try { Shape(); io.println("FAIL: shape instantiated"); }
catch (RuntimeError e) { io.println(e.message); }

io.println(Circle(2).area());

try { Square(); io.println("FAIL: square instantiated"); }
catch (RuntimeError e) { io.println(e.message); }
`, `cannot instantiate abstract class AbstractBase
base
cannot instantiate Shape: abstract method Shape.area is not implemented
12
cannot instantiate Square: abstract method Shape.area is not implemented
`)
}

// TestParityFieldDecoratorsAndDottedNames covers two language
// additions that together let frameworks attach annotations to
// class fields: dotted decorator names (`@Assert.email`,
// `@Foo.bar.baz`) parse as a single composite identifier, and
// `@`-prefixed decorators on field declarations inside a class
// body persist into `reflect.fields(class)` as a per-field
// `decorators` list. Field decorators are pure metadata; the
// runtime never executes them automatically.
func TestParityFieldDecoratorsAndDottedNames(t *testing.T) {
	runParity(t, `import io;
import reflect;

class User {
    @Assert.email
    string email;

    @Assert.minLength(2)
    @Assert.maxLength(64)
    string name;

    string id;
}

for (entry in reflect.fields(User)) {
    let field = entry as dict<string, any>;
    io.println(field["name"]);
    if (field.contains("decorators")) {
        for (d in field["decorators"] as list<any>) {
            let dec = d as dict<string, any>;
            io.print("  @" + (dec["name"] as string));
            let args = dec["args"] as list<any>;
            if (args.length() > 0) {
                io.print("(" + (args[0] as string) + ")");
            }
            io.println("");
        }
    }
}
`, "email\n  @Assert.email\nid\nname\n  @Assert.minLength(2)\n  @Assert.maxLength(64)\n")
}

// @memoize runs the body once per distinct argument tuple; recursion is memoized too.
func TestParityMemoize(t *testing.T) {
	runParity(t, `import io;
let calls = 0;
@memoize
func square(int n): int { calls = calls + 1; return n * n; }
io.println(square(4));
io.println(square(4));
io.println(square(5));
io.println(calls);
`, "16\n16\n25\n2\n")

	// Recursive memoization: each fib(n) body runs once.
	runParity(t, `import io;
let bodies = 0;
@memoize
func fib(int n): int { bodies = bodies + 1; if (n < 2) { return n; } return fib(n - 1) + fib(n - 2); }
io.println(fib(10));
io.println(bodies);
`, "55\n11\n")

	// Distinct argument tuples are cached independently.
	runParity(t, `import io;
let calls = 0;
@memoize
func add(int a, int b): int { calls = calls + 1; return a + b; }
io.println(add(1, 2));
io.println(add(1, 2));
io.println(add(2, 1));
io.println(calls);
`, "3\n3\n3\n2\n")
}

// TestParityCrossModuleClassDecorators guards the fix for reflect.class(instance)
// dropping class-level decorators on the VM when the class is declared in another
// module (gebweb reads controller decorators this way). Both backends must read
// the same class decorators AND methods for a cross-module instance.
func TestParityCrossModuleClassDecorators(t *testing.T) {
	dir := t.TempDir()
	lib := "module reflectlib;\n" +
		"import reflect;\n" +
		"export func info(any instance): dict<string, any> {\n" +
		"    let cls = reflect.class(instance);\n" +
		"    list<string> decs = [];\n" +
		"    for (dd in reflect.decorators(cls)) { decs = decs.push((dd as dict<string, any>)[\"name\"] as string); }\n" +
		"    return {\"decorators\": decs, \"nmethods\": reflect.methods(cls).length()};\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(dir, "reflectlib.gb"), []byte(lib), 0o644); err != nil {
		t.Fatalf("write lib: %v", err)
	}
	source := "import io;\nimport reflectlib;\n" +
		"@Service(\"u\")\nclass C { func m(): int { return 1; } func n(): int { return 2; } }\n" +
		"io.println(reflectlib.info(C()));\n"

	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	var evOut bytes.Buffer
	ev := evaluator.NewWithArgsAndModulePaths(&evOut, nil, []string{dir})
	if _, err := ev.Eval(program); err != nil {
		t.Fatalf("evaluator error: %v", err)
	}
	chunk, err := bytecode.Compile(program, []byte(source), "parity")
	if err != nil {
		t.Fatalf("compile error: %v", err)
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
	stateful.SetMethodDispatcher(vm)
	if err := vm.Run(); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if evOut.String() != vmOut.String() {
		t.Errorf("cross-module reflection mismatch:\n  evaluator: %q\n  vm:        %q", evOut.String(), vmOut.String())
	}
	want := "{\"decorators\": [\"Service\"], \"nmethods\": 2}\n"
	if evOut.String() != want {
		t.Errorf("wrong output: got %q, want %q", evOut.String(), want)
	}
}

// TestParityCallableMethodDecoratorNamedArgs guards 9.21: named args bind to the ORIGINAL method's parameter names, not the wrapper's.
func TestParityCallableMethodDecoratorNamedArgs(t *testing.T) {
	runParity(t, `import io;

func transparent(any next): any {
    return func(any arg): any {
        return next(arg);
    };
}

class Greeter {
    @transparent
    func handle(int x): int {
        return x + 1;
    }
}

let g = Greeter();
io.println(g.handle(x: 5));
io.println(g.handle(5));
`, "6\n6\n")
}

func TestParityCallableStaticMethodDecoratorNamedArgs(t *testing.T) {
	runParity(t, `import io;

func transparent(any next): any {
    return func(any arg): any {
        return next(arg);
    };
}

class Greeter {
    @transparent
    static func handle(int x): int {
        return x + 100;
    }
}

io.println(Greeter.handle(x: 5));
io.println(Greeter.handle(5));
`, "105\n105\n")
}

func TestParityCallableFunctionDecoratorNamedArgs(t *testing.T) {
	runParity(t, `import io;

func transparent(any next): any {
    return func(any arg): any {
        return next(arg);
    };
}

@transparent
func handle(int x): int {
    return x * 2;
}

io.println(handle(x: 5));
io.println(handle(5));
`, "10\n10\n")
}

// TestParityCallableMethodDecoratorNamedArgsOutOfOrder: wrapper renames every parameter; named args come in reverse declared order.
func TestParityCallableMethodDecoratorNamedArgsOutOfOrder(t *testing.T) {
	runParity(t, `import io;

func transparent(any next): any {
    return func(any a, any b): any {
        return next(a, b);
    };
}

class Calc {
    @transparent
    func subtract(int x, int y): int {
        return x - y;
    }
}

let c = Calc();
io.println(c.subtract(x: 10, y: 3));
io.println(c.subtract(y: 3, x: 10));
io.println(c.subtract(10, 3));
`, "7\n7\n7\n")
}

// TestParityCallableMethodDecoratorVariadicWrapperNamedArgs: the `func(any ...args)` forwarding idiom must keep working for named calls too.
func TestParityCallableMethodDecoratorVariadicWrapperNamedArgs(t *testing.T) {
	runParity(t, `import io;

func transparent(any next): any {
    return func(any ...args): any {
        return next(...args);
    };
}

class Calc {
    @transparent
    func subtract(int x, int y): int {
        return x - y;
    }
}

let c = Calc();
io.println(c.subtract(x: 10, y: 3));
io.println(c.subtract(y: 3, x: 10));
io.println(c.subtract(10, 3));
`, "7\n7\n7\n")
}

// TestParityMethodNamedArgumentsUndecorated: plain (undecorated) method named-arg binding must be unaffected by the 9.21 fix.
func TestParityMethodNamedArgumentsUndecorated(t *testing.T) {
	runParity(t, `import io;

class Connector {
    func connect(string host, int port = 80, bool tls = false): string {
        string result = host + ":" + (port as string);
        if (tls) { result = result + "s"; }
        return result;
    }

    static func label(string host, int port = 80): string {
        return host + "/" + (port as string);
    }
}

let c = Connector();
io.println(c.connect("example.com"));
io.println(c.connect("example.com", port: 443, tls: true));
io.println(c.connect("api.com", tls: true, port: 8443));
io.println(Connector.label("api.com"));
io.println(Connector.label(port: 22, host: "ssh.com"));
`, "example.com:80\nexample.com:443s\napi.com:8443s\napi.com/80\nssh.com/22\n")
}
