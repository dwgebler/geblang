package evaluator_test

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"

	gorillawebsocket "github.com/gorilla/websocket"
)

func newLocalHTTPTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local sockets unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	// Close the auto-bound listener being replaced so it cannot leak.
	server.Listener.Close()
	server.Listener = listener
	server.Start()
	waitForListenerReady(t, listener.Addr().String())
	return server
}

// waitForListenerReady blocks until the server accepts a connection: a fresh loopback listener can be transiently unreachable under package load.
func waitForListenerReady(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp4", addr, time.Second)
		if err == nil {
			conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("test server never became reachable at %s: %v", addr, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestEvaluatorRunsHelloProgram(t *testing.T) {
	input := `import bytes;
import io;
import sys;
io.print("Hello world\n");
io.println("Hello world");
io.print('Hello world\n');
sys.exit(0);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	result, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if !result.Exited || result.ExitCode != 0 {
		t.Fatalf("exit result: got exited=%v code=%d", result.Exited, result.ExitCode)
	}

	want := "Hello world\nHello world\nHello world\\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorBuildsSMTPMessage(t *testing.T) {
	input := `import io;
import smtp;

let raw = smtp.message({
    "from": "App <noreply@example.com>",
    "to": ["Ada <ada@example.com>"],
    "bcc": ["Audit <audit@example.com>"],
    "subject": "Welcome",
    "text": "Hello Ada",
    "html": "<p>Hello Ada</p>",
    "headers": {"X-App": "Geblang"},
    "attachments": [
        {"filename": "welcome.txt", "contentType": "text/plain", "content": "Thanks"}
    ]
});

io.println(raw.contains("multipart/mixed"));
io.println(raw.contains("multipart/alternative"));
io.println(raw.contains("Subject: Welcome"));
io.println(raw.contains("X-App: Geblang"));
io.println(raw.contains("welcome.txt"));
io.println(raw.contains("Audit <audit@example.com>"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(program); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "true\ntrue\ntrue\ntrue\ntrue\nfalse\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsCoreScriptSubset(t *testing.T) {
	input := `import io;

int x = 1;
const label = "count";

while (x < 4) {
    io.println(label + ": " + "tick");
    x = x + 1;
}

if (x == 4 && true xor false) {
    io.println(x);
}

io.println(5 // 2);
io.println(5 / 2);
io.println(-7 // 3);
io.println(-7 % 3);
let nan = 0.0f / 0.0f;
io.println(nan == nan);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	result, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if result.Exited {
		t.Fatalf("unexpected exit")
	}

	want := "count: tick\ncount: tick\ncount: tick\n4\n2\n2.5000000000\n-3\n2\nfalse\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorReflectsFunctionAndClassDecorators(t *testing.T) {
	input := `import io;
import reflect;

@route("GET", "/users", name: "users")
@tag("public")
func index(): string {
    return "ok";
}

@service(name: "users")
class UserService {}

let fnDecorators = reflect.decorators(index);
io.println(fnDecorators.length());
io.println(fnDecorators[0]["name"]);
io.println(fnDecorators[0]["target"]);
io.println(fnDecorators[0]["args"][0]);
io.println(fnDecorators[0]["args"][1]);
io.println(fnDecorators[0]["namedArgs"]["name"]);
io.println(reflect.hasDecorator(index, "ROUTE"));
io.println(reflect.decorator(index, "tag")["args"][0]);

let classDecorators = reflect.decorators(UserService);
io.println(classDecorators.length());
io.println(classDecorators[0]["name"]);
io.println(classDecorators[0]["target"]);
io.println(classDecorators[0]["namedArgs"]["name"]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "2\nroute\nfunction\nGET\n/users\nusers\ntrue\npublic\n1\nservice\nclass\nusers\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorReflectsNamedFunctionAndClassHandles(t *testing.T) {
	input := `import io;
import reflect;

@route("GET", "/users")
func index(): string {
    return "ok";
}

@service(name: "users")
class UserService {}

let fn = reflect.function("index");
io.println(reflect.decorator(fn, "route")["target"]);
io.println(reflect.decorators(reflect.function("index"))[0]["args"][0]);

let cls = reflect.class("UserService");
io.println(reflect.decorator(cls, "service")["target"]);
io.println(reflect.decorators(reflect.class("UserService"))[0]["namedArgs"]["name"]);
io.println(reflect.function("missing") == null);
io.println(reflect.class("missing") == null);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "function\nGET\nclass\nusers\ntrue\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorReflectsFunctionSignatureMetadata(t *testing.T) {
	input := `import io;
import reflect;

func index(string name, int limit = 10): string {
    return name;
}

func collect(string ...tags): string {
    return "ok";
}

class Controller {
    func list(string prefix): string {
        return prefix;
    }
}

let params = reflect.parameters(index);
io.println(params.length());
io.println(params[0]["name"]);
io.println(params[0]["type"]);
io.println(params[1]["hasDefault"]);
io.println(reflect.returnType(index));
io.println(reflect.parameters(collect)[0]["variadic"]);

let method = reflect.method(Controller, "list");
let methodParams = reflect.parameters(method);
io.println(methodParams[0]["name"]);
io.println(methodParams[0]["type"]);
io.println(reflect.returnType(method));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "2\nname\nstring\ntrue\nstring\ntrue\nprefix\nstring\nstring\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorReflectsClassShapeMetadata(t *testing.T) {
	input := `import io;
import reflect;

interface Named {
    func name(): string;
}

class Base {}

class Controller extends Base implements Named {
    string prefix;
    int count;

    func name(): string {
        return this.prefix;
    }

    func list(): string {
        return this.prefix;
    }

    static func create(): string {
        return "created";
    }
}

io.println(reflect.parent(Controller));
io.println(reflect.interfaces(Controller)[0]);
io.println(reflect.fields(Controller)[0]["name"]);
io.println(reflect.fields(Controller)[1]["name"]);
io.println(reflect.methods(Controller)[0]);
io.println(reflect.methods(Controller)[1]);
io.println(reflect.staticMethods(Controller)[0]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "Base\nNamed\ncount\nprefix\nlist\nname\ncreate\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorReflectsConstructors(t *testing.T) {
	input := `import io;
import reflect;

class Point {
    int x;
    int y;
    func Point(int x, int y) {
        this.x = x;
        this.y = y;
    }
}

let ctors = reflect.constructors(Point);
io.println(ctors.length());
io.println(ctors[0][0]["name"]);
io.println(ctors[0][0]["type"]);
io.println(ctors[0][1]["name"]);
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "1\nx\nint\ny\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorReflectsTypeBindings(t *testing.T) {
	input := `import io;
import reflect;

class Box<T> {
    T value;
    func Box(T v) { this.value = v; }
}

Box<string> b = Box("hello");
Box<int> n = Box(42);
io.println(reflect.typeBindings(b)["T"]);
io.println(reflect.typeBindings(n)["T"]);
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "string\nint\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorReflectsInterfaceMethods(t *testing.T) {
	input := `import io;
import reflect;

interface Animal {
    func name(): string;
    func sound(): string;
}

let methods = reflect.interfaceMethods(Animal);
io.println(methods.length());
io.println(methods[0]["name"]);
io.println(methods[0]["returnType"]);
io.println(methods[1]["name"]);
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "2\nname\nstring\nsound\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorReflectsInterfaceParents(t *testing.T) {
	input := `import io;
import reflect;

interface Base {}
interface Extended extends Base {}

let parents = reflect.interfaceParents(Extended);
io.println(parents.length());
io.println(parents[0]);
let noParents = reflect.interfaceParents(Base);
io.println(noParents.length());
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "1\nBase\n0\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorReflectsModuleQualifiedFunctionAndClassHandles(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "util")
	if err := os.Mkdir(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "meta.gb"), []byte(`module util.meta;

export @route("GET", "/users")
func index(): string {
    return "ok";
}

export @service(name: "users")
class Controller {}
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}

	input := `import io;
import reflect;
import util.meta;

let fn = reflect.function("meta.index");
io.println(reflect.decorators(fn)[0]["name"]);
io.println(reflect.decorators(fn)[0]["args"][0]);

let cls = reflect.class("meta.Controller");
io.println(reflect.decorators(cls)[0]["name"]);
io.println(reflect.decorators(cls)[0]["namedArgs"]["name"]);
io.println(reflect.class("meta.Missing") == null);

let mod = reflect.module("meta");
let exports = reflect.exports(mod);
io.println(exports[0]);
io.println(exports[1]);
io.println(reflect.module("missing") == null);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.NewWithArgsAndModulePaths(&out, nil, []string{dir}).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "route\nGET\nservice\nusers\ntrue\nController\nindex\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorReflectsMethodDecorators(t *testing.T) {
	input := `import io;
import reflect;

class Controller {
    @route("GET", "/users")
    func list(): string {
        return "ok";
    }

    @route("POST", "/users")
    static func create(): string {
        return "created";
    }
}

let method = reflect.method(Controller, "list");
let methodDecorators = reflect.decorators(method);
io.println(methodDecorators.length());
io.println(methodDecorators[0]["name"]);
io.println(methodDecorators[0]["target"]);
io.println(methodDecorators[0]["args"][0]);
io.println(methodDecorators[0]["args"][1]);
io.println(reflect.hasDecorator(method, "ROUTE"));

let controller = Controller();
let instanceMethod = reflect.method(controller, "list");
io.println(reflect.decorator(instanceMethod, "route")["target"]);

let staticMethod = reflect.staticMethod(Controller, "create");
let staticDecorators = reflect.decorators(staticMethod);
io.println(staticDecorators[0]["target"]);
io.println(staticDecorators[0]["args"][0]);
io.println(staticDecorators[0]["args"][1]);
io.println(reflect.method(Controller, "missing") == null);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "1\nroute\nmethod\nGET\n/users\ntrue\nmethod\nstaticMethod\nPOST\n/users\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorReflectsCallableBoundMethods(t *testing.T) {
	input := `import io;
import reflect;

class Greeter {
    string name;

    func Greeter(string name) {
        this.name = name;
    }

    @route("GET", "/hello")
    func greet(string prefix): string {
        return prefix + this.name;
    }

    static func label(string name): string {
        return "kind:" + name;
    }
}

let greeter = Greeter("Ada");
let method = reflect.method(greeter, "greet");
io.println(method("hi "));
io.println(reflect.decorators(method)[0]["name"]);

let staticMethod = reflect.staticMethod(Greeter, "label");
io.println(staticMethod("user"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "hi Ada\nroute\nkind:user\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorReflectsOverloadedDecoratorMetadata(t *testing.T) {
	input := `import io;
import reflect;

@tag("int")
@route("GET", "/numbers")
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
io.println(decorators[0]["name"]);
io.println(decorators[0]["overload"]);
io.println(decorators[0]["position"]);
io.println(decorators[1]["name"]);
io.println(decorators[1]["overload"]);
io.println(decorators[1]["position"]);
io.println(decorators[2]["name"]);
io.println(decorators[2]["overload"]);
io.println(decorators[2]["position"]);
io.println(tags.length());
io.println(tags[0]["args"][0]);
io.println(tags[1]["args"][0]);
io.println(reflect.decorator(describe, "tag")["overload"]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "3\ntag\n0\n0\nroute\n0\n1\ntag\n1\n0\n2\nint\nstring\n0\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRejectsExecutableDecoratorMetadata(t *testing.T) {
	input := `import reflect;

func label(): string {
    return "dynamic";
}

@route(label())
func index(): string {
    return "ok";
}

reflect.decorators(index);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	_, err := evaluator.New(&bytes.Buffer{}).Eval(program)
	if err == nil || !strings.Contains(err.Error(), "unsupported decorator argument expression") {
		t.Fatalf("expected unsupported decorator metadata error, got %v", err)
	}
}

func TestEvaluatorAppliesCallableFunctionDecorators(t *testing.T) {
	input := `import io;
import reflect;

func prefix(any fn, string label): any {
    return func(string name): string {
        return label + fn(name);
    };
}

func suffix(any fn, string label): any {
    return func(string name): string {
        return fn(name) + label;
    };
}

@prefix("hello ")
@suffix("!")
func greet(string name): string {
    return name;
}

io.println(greet("Ada"));
io.println(reflect.decorators(greet)[0]["name"]);
io.println(reflect.decorators(greet)[1]["name"]);
io.println(reflect.parameters(greet)[0]["name"]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "hello Ada!\nprefix\nsuffix\nname\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRejectsCallableDecoratorReturningNonFunction(t *testing.T) {
	input := `func bad(any fn): string {
    return "nope";
}

@bad
func greet(): string {
    return "Ada";
}
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	_, err := evaluator.New(&bytes.Buffer{}).Eval(program)
	if err == nil || !strings.Contains(err.Error(), "decorator bad must return function") {
		t.Fatalf("expected decorator return error, got %v", err)
	}
}

func TestEvaluatorRejectsCallableDecoratorWithIncompatibleWrapper(t *testing.T) {
	input := `func bad(any fn): any {
    return func(): string {
        return "wrong";
    };
}

@bad
func greet(string name): string {
    return name;
}
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	_, err := evaluator.New(&bytes.Buffer{}).Eval(program)
	if err == nil || !strings.Contains(err.Error(), "decorator bad returned incompatible wrapper for greet") {
		t.Fatalf("expected decorator wrapper compatibility error, got %v", err)
	}
}

func TestEvaluatorAppliesCallableMethodDecorators(t *testing.T) {
	input := `import io;
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
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "Ada!\nkind:user\nsuffix\nmethod\nprefix\nstaticMethod\n"
	if out.String() != want {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestEvaluatorLeavesUnknownMethodDecoratorsMetadataOnly(t *testing.T) {
	input := `import io;
import reflect;

class Greeter {
    @route("GET", "/hello")
    func greet(): string {
        return "hello";
    }
}

let greeter = Greeter();
io.println(greeter.greet());
let method = reflect.method(Greeter, "greet");
io.println(reflect.decorators(method)[0]["name"]);
io.println(reflect.decorators(method)[0]["args"][0]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if out.String() != "hello\nroute\nGET\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestEvaluatorRejectsCallableMethodDecoratorReturningNonFunction(t *testing.T) {
	input := `func bad(any next): string {
    return "nope";
}

class Greeter {
    @bad
    func greet(): string {
        return "hello";
    }
}
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	_, err := evaluator.New(io.Discard).Eval(program)
	if err == nil || !strings.Contains(err.Error(), "decorator bad must return function") {
		t.Fatalf("expected decorator return error, got %v", err)
	}
}

func TestEvaluatorAppliesCallableClassDecorators(t *testing.T) {
	input := `import io;
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
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "hello from users\nservice\nusers\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorAppliesMultipleCallableClassDecorators(t *testing.T) {
	input := `import io;

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
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	// Decorators apply bottom-up: tagB first, then tagA
	want := "B\nA\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorLeavesUnknownClassDecoratorsMetadataOnly(t *testing.T) {
	input := `import io;
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
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "pong\ncontroller\n/api\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRejectsCallableClassDecoratorReturningNonClass(t *testing.T) {
	input := `func bad(any cls): string {
    return "nope";
}

@bad
class Widget {}
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	_, err := evaluator.New(io.Discard).Eval(program)
	if err == nil || !strings.Contains(err.Error(), "decorator bad must return class") {
		t.Fatalf("expected decorator class return error, got %v", err)
	}
}

func TestEvaluatorRejectsExcessiveRecursion(t *testing.T) {
	input := `func recurse(int n): int {
    return recurse(n + 1);
}

recurse(0);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	ev := evaluator.New(&out)
	ev.SetMaxCallDepth(16)
	_, err := ev.Eval(program)
	if err == nil {
		t.Fatal("expected max call depth error")
	}
	if !strings.Contains(err.Error(), "maximum call depth exceeded (16)") {
		t.Fatalf("error: got %v", err)
	}
}

func TestEvaluatorRunsGenericFunctionsWithTypeErasure(t *testing.T) {
	input := `import io;

func identity<T>(T value): T {
    return value;
}

func single<T>(T value): list<T> {
    return [value];
}

func first<T>(list<T> values): T {
    return values[0];
}

io.println(identity("Ada"));
io.println(identity(7));
io.println(single("x")[0]);
io.println(first([10, 20]));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if got, want := out.String(), "Ada\n7\nx\n10\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorGenericFunctionOverloadRequiresConcreteContainer(t *testing.T) {
	input := `func first<T>(list<T> values): T {
    return values[0];
}

func first(string value): string {
    return value;
}

first(10);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	_, err := evaluator.New(io.Discard).Eval(program)
	if err == nil || !strings.Contains(err.Error(), "no matching overload for first") {
		t.Fatalf("expected overload error, got %v", err)
	}
}

func TestEvaluatorRejectsConstantAssignment(t *testing.T) {
	input := `const x = 1;
x = 2;
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	_, err := evaluator.New(&bytes.Buffer{}).Eval(program)
	if err == nil {
		t.Fatal("expected constant assignment error")
	}
}

func TestEvaluatorRunsFunctionsAndClosures(t *testing.T) {
	input := `import io;

func fib(int n): int {
    if (n < 2) {
        return n;
    }
    return fib(n - 1) + fib(n - 2);
}

func add(int a, int b = 10): int {
    return a + b;
}

func makeCounter(): func {
    int n = 0;
    return func(): int {
        n++;
        return n;
    };
}

let counter = makeCounter();
io.println(fib(8));
io.println(add(5));
io.println(counter());
io.println(counter());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "21\n15\n1\n2\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsCallableTypeHints(t *testing.T) {
	input := `import io;

func apply(callable fn): int {
    return fn(4);
}

func pick(callable fn): string {
    return fn();
}

func pick(string value): string {
    return value;
}

io.println(apply(func(int x): int { return x + 3; }));
io.println(pick(func(): string { return "fn"; }));
io.println(pick("text"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if got, want := out.String(), "7\nfn\ntext\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorRunsNamedArguments(t *testing.T) {
	input := `import io;

func connect(string host, int port = 80, bool tls = true): string {
    return host + ":" + (port as string) + ":" + (tls as string);
}

class Client {
    string host;
    int port;

    func Client(string host, int port = 80) {
        this.host = host;
        this.port = port;
    }

    func url(string scheme = "https"): string {
        return scheme + "://" + this.host + ":" + (this.port as string);
    }
}

io.println(connect("example.test", tls: false));
io.println(connect(port: 443, host: "example.test"));
Client c = Client(port: 8080, host: "localhost");
io.println(c.url(scheme: "http"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "example.test:80:false\nexample.test:443:true\nhttp://localhost:8080\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorSpreadsDictIntoNamedArguments(t *testing.T) {
	input := `import io;

func join(string left, string right, string sep = "-"): string {
    return left + sep + right;
}

func describe(string name, int count): string {
    return name + ":" + (count as string);
}

func describe(int count): string {
    return "count:" + (count as string);
}

let joinArgs = {"right": "B", "sep": "|"};
let describeArgs = {"name": "Ada", "count": 3};
io.println(join("A", ...joinArgs));
io.println(describe(...describeArgs));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if got, want := out.String(), "A|B\nAda:3\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorRunsForLoopsAndIteration(t *testing.T) {
	input := `import io;

int total = 0;
for (int i = 0; i < 4; i++) {
    total = total + i;
}
io.println(total);

total = 0;
for (i in 1..5 by 2) {
    total = total + i;
}
io.println(total);

total = 0;
for (i in 5..<1 by -2) {
    total = total + i;
}
io.println(total);

let nums = [2, 4, 6];
total = 0;
for (n in nums) {
    total = total + n;
}
io.println(total);

let data = {"a": 2, "b": 3};
total = 0;
for (value in data.values()) {
    total = total + value;
}
io.println(total);

total = 0;
for (key, value in data.items()) {
    total = total + value;
}
io.println(total);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "6\n9\n8\n12\n5\n5\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsCollectionIndexAssignmentAndMethods(t *testing.T) {
	input := `import io;

let nums = [1, 2, 3];
nums[0] = 9;
nums.set(-1, 7);
io.println(nums[0]);
io.println(nums.get(-1));
io.println(nums.length());
io.println(nums.isEmpty());

let data = {"name": "Dave"};
data["age"] = 42;
data.set("name", "Sarah");
io.println(data.get("name"));
io.println(data["age"]);
io.println(data.length());
io.println(data.isEmpty());

string name = "Geb";
io.println(name[0]);
io.println(name.get(-1));
io.println(name.length());
io.println(name.isEmpty());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "9\n7\n3\nfalse\nSarah\n42\n2\nfalse\nG\nb\n3\nfalse\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsSlices(t *testing.T) {
	input := `import io;

let nums = [1, 2, 3, 4, 5];
let mid = nums[1..<4];
mid[0] = 99;

io.println(mid[0]);
io.println(nums[1]);
io.println(nums[1..3].length());
io.println(nums[..<2].length());
io.println(nums[3..].length());
io.println(nums[..].length());

string name = "Geblang";
io.println(name[0..<3]);
io.println(name[3..]);
io.println(name[..]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "99\n2\n3\n2\n2\n5\nGeb\nlang\nGeblang\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsTryCatchFinally(t *testing.T) {
	input := `import io;

try {
    throw ValueError("bad");
} catch (TypeError e) {
    io.println("type");
} catch (ValueError e) {
    io.println(e);
} finally {
    io.println("cleanup");
}

try {
    throw Error("fallback");
} catch {
    io.println("caught");
}

func returnsWithFinally(): int {
    try {
        return 7;
    } finally {
        io.println("before return");
    }
}

io.println(returnsWithFinally());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "ValueError: bad\ncleanup\ncaught\nbefore return\n7\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsDeferAtFunctionAndTopLevelExit(t *testing.T) {
	input := `import io;

defer io.println("top first");
defer io.println("top second");

func f(): int {
    defer io.println("f first");
    defer io.println("f second");
    return 3;
}

io.println(f());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "f second\nf first\n3\ntop second\ntop first\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsMatchStatementsAndExpressions(t *testing.T) {
	input := `import io;

let value = 7;
let label = match (value) {
    case string s => s;
    case int n if (n > 5) => "big";
    case 0 => "zero";
    default => "other";
};
io.println(label);

match (Error("bad")) {
    case ValueError e:
        io.println("value");
    case Error e:
        io.println(e);
}

`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "big\nError: bad\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorCatchesUnmatchedMatchExpression(t *testing.T) {
	input := `import io;

try {
    let value = match (7) {
        case string s => s;
    };
    io.println(value);
} catch (MatchError e) {
    io.println(e);
}
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "MatchError: no match case matched (add a 'default:' case to handle all values); got 7 (type: int)\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsTypeReflectionAndCasts(t *testing.T) {
	input := `import io;
import reflect;

let x = "42";
type UserId = string;
type Money = decimal;
UserId id = "u-1";
Money price = 12.5;
io.println(typeof(x));
io.println(reflect.typeOf([1, 2]));
io.println(dump(x));
io.println(string.type);
io.println(x instanceof string);
io.println((x as int) + 1);
io.println((7 as decimal) / 2);
io.println(("true" as bool) && true);
io.println(id);
io.println(price.format(2));
io.println(("43" as UserId).toInt());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "string\nlist\nstring(\"42\")\nstring\ntrue\n43\n3.5000000000\ntrue\nu-1\n12.50\n43\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsBasePrefixedNumericLiterals(t *testing.T) {
	input := `import io;

int b = 0b11;
int h = 0x1F;
int mode = 0o644;
decimal d = 0x1F;
float f = 0b11;
io.println(b);
io.println(h);
io.println(mode);
io.println(d);
io.println(f);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "3\n31\n420\n31.0000000000\n3\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRuntimeErrorsAreCatchable(t *testing.T) {
	input := `import io;

try {
    let int x = 5;
    io.println(x / 0);
} catch (RuntimeError e) {
    io.println(e.name);
    io.println(e.message);
}

func failInFunction(): int {
    return 1 // 0;
}

try {
    failInFunction();
} catch (RuntimeError e) {
    io.println(e.message);
}
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "RuntimeError\ndecimal division by zero\ninteger division by zero\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsBasicClasses(t *testing.T) {
	input := `import io;

class User {
    string name;
    int visits = 0;

    func User(string name) {
        this.name = name;
    }

    func visit(): int {
        this.visits = this.visits + 1;
        return this.visits;
    }

    func label(): string {
        return this.name + ":" + (this.visits as string);
    }
}

User u = User("Dave");
io.println(u.name);
io.println(u.visit());
io.println(u.VISIT());
io.println(u.label());
io.println(u instanceof User);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "Dave\n1\n2\nDave:2\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsClassInheritance(t *testing.T) {
	input := `import io;

class Animal {
    string name;

    func Animal(string name) {
        this.name = name;
    }

    func speak(): string {
        return this.name + ":?";
    }
}

class Dog extends Animal {
    string breed;

    func Dog(string name, string breed) {
        parent(name);
        this.breed = breed;
    }

    func speak(): string {
        return parent.speak() + ":woof:" + this.breed;
    }
}

Dog d = Dog("Ada", "Collie");
io.println(d.name);
io.println(d.breed);
io.println(d.speak());
io.println(d instanceof Dog);
io.println(d instanceof Animal);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "Ada\nCollie\nAda:?:woof:Collie\ntrue\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsInterfaces(t *testing.T) {
	input := `import io;

interface Printable {
    func print(): string;
}

interface Labelled extends Printable {
    func label(): string;
}

class Report implements Labelled {
    string name;

    func Report(string name) {
        this.name = name;
    }

    func print(): string {
        return "report:" + this.name;
    }

    func label(): string {
        return this.name;
    }
}

Report r = Report("Q1");
io.println(r.print());
io.println(r instanceof Report);
io.println(r instanceof Labelled);
io.println(r instanceof Printable);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "report:Q1\ntrue\ntrue\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRejectsMissingInterfaceMethod(t *testing.T) {
	input := `interface Printable {
    func print(): string;
}

class Report implements Printable {
}
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	_, err := evaluator.New(&bytes.Buffer{}).Eval(program)
	if err == nil {
		t.Fatal("expected missing interface method error")
	}
}

func TestEvaluatorRunsStaticClassMembers(t *testing.T) {
	input := `import io;

class Named {
    static const prefix = "N";

    static func label(string name): string {
        return Named.prefix + ":" + name;
    }
}

class User extends Named {
    static func labelUser(string name): string {
        return User.label(name);
    }
}

io.println(Named.prefix);
io.println(Named.label("Ada"));
io.println(User.prefix);
io.println(User.label("Bea"));
io.println(User.labelUser("Cal"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "N\nN:Ada\nN\nN:Bea\nN:Cal\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsObjectEquality(t *testing.T) {
	input := `import io;

class Point {
    int x;
    int y;

    func Point(int x, int y) {
        this.x = x;
        this.y = y;
    }
}

class Tagged {
    string tag;

    func Tagged(string tag) {
        this.tag = tag;
    }

    func __eq(any other): bool {
        return other instanceof Tagged && this.tag == other.tag;
    }
}

Point a = Point(1, 2);
Point b = Point(1, 2);
Point c = a;
Tagged x = Tagged("a");
Tagged y = Tagged("a");
Tagged z = Tagged("b");

io.println(a == b);
io.println(a is b);
io.println(a is c);
io.println(x == y);
io.println(x != z);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "true\nfalse\ntrue\ntrue\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorShortCircuitsBooleanOperators(t *testing.T) {
	input := `import io;

func fail(): bool {
    throw RuntimeError("should not run");
}

io.println(false && fail());
io.println(true || fail());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "false\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorDispatchesFunctionAndMethodOverloads(t *testing.T) {
	input := `import io;

func describe(int value): string {
    return "int:" + (value as string);
}

func describe(string value): string {
    return "string:" + value;
}

class Formatter {
    func format(int value): string {
        return "method-int:" + (value as string);
    }

    func format(string value): string {
        return "method-string:" + value;
    }
}

io.println(describe(7));
io.println(describe("seven"));
Formatter formatter = Formatter();
io.println(formatter.format(8));
io.println(formatter.format("eight"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "int:7\nstring:seven\nmethod-int:8\nmethod-string:eight\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRejectsAmbiguousReturnTypeOnlyOverload(t *testing.T) {
	input := `func value(string input): int {
    return 1;
}

func value(string input): string {
    return input;
}

value("x");
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err == nil {
		t.Fatalf("expected ambiguous overload error")
	}
	if !strings.Contains(err.Error(), "ambiguous overload") {
		t.Fatalf("error: got %q, want ambiguous overload", err.Error())
	}
}

func TestEvaluatorUsesExpectedTypeForReturnTypeOnlyOverloads(t *testing.T) {
	input := `import io;

func parse(string input): int {
    return 42;
}

func parse(string input): string {
    return "s:" + input;
}

func parseReturn(): string {
    return parse("return");
}

int number = parse("x");
string text = parse("x");
io.println(number);
io.println(text);
io.println(parseReturn());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "42\ns:x\ns:return\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorDispatchesOverloadedConstructors(t *testing.T) {
	input := `import io;

class Box {
    string label;

    func Box(int value) {
        this.label = "int:" + (value as string);
    }

    func Box(string value) {
        this.label = "string:" + value;
    }
}

class ChildBox extends Box {
    func ChildBox(int value) {
        parent(value);
    }

    func ChildBox(string value) {
        parent(value);
    }
}

io.println(Box(3).label);
io.println(Box("three").label);
io.println(ChildBox(4).label);
io.println(ChildBox("four").label);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "int:3\nstring:three\nint:4\nstring:four\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsClassOperatorMethods(t *testing.T) {
	input := `import io;

class Money {
    int cents;

    func Money(int cents) {
        this.cents = cents;
    }

    func __add(Money other): Money {
        return Money(this.cents + other.cents);
    }

    func __lt(Money other): bool {
        return this.cents < other.cents;
    }
}

Money a = Money(125);
Money b = Money(75);
Money c = a + b;

io.println(c.cents);
io.println(b < a);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "200\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorDispatchesInheritedMethodOverloads(t *testing.T) {
	input := `import io;

class Base {
    func format(int value): string {
        return "base-int:" + (value as string);
    }

    func format(bool value): string {
        return "base-bool:" + (value as string);
    }
}

class Child extends Base {
    func format(string value): string {
        return "child-string:" + value;
    }

    func format(bool value): string {
        return "child-bool:" + (value as string);
    }
}

Child child = Child();
io.println(child.format(7));
io.println(child.format("seven"));
io.println(child.format(true));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "base-int:7\nchild-string:seven\nchild-bool:true\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorAutoCallsNoArgParentConstructor(t *testing.T) {
	input := `import io;

class Base {
    int count = 0;

    func Base() {
        this.count = this.count + 1;
    }
}

class AutoChild extends Base {
    func AutoChild() {
    }
}

class ExplicitChild extends Base {
    func ExplicitChild() {
        parent();
    }
}

AutoChild a = AutoChild();
ExplicitChild e = ExplicitChild();
io.println(a.count);
io.println(e.count);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "1\n1\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsPrefixOperatorMethods(t *testing.T) {
	input := `import io;

class Flag {
    bool value;

    func Flag(bool value) {
        this.value = value;
    }

    func __not(): bool {
        return !this.value;
    }
}

class Vector {
    int x;

    func Vector(int x) {
        this.x = x;
    }

    func __neg(): Vector {
        return Vector(-this.x);
    }
}

Flag flag = Flag(false);
Vector v = Vector(7);
io.println(!flag);
io.println((-v).x);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "true\n-7\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsMagicGetSetCallAndInvoke(t *testing.T) {
	input := `import io;

class Dynamic {
    dict<string, any> values;

    func Dynamic() {
        this.values = {};
    }

    func __get(string name): any {
        return this.values.get(name);
    }

    func __set(string name, any value): void {
        this.values[name] = value;
    }

    func __call(string name, list<any> args): string {
        return name + ":" + (args.length() as string);
    }

    func __invoke(int x): int {
        return x + 1;
    }

    static func __getStatic(string name): string {
        return "static:" + name;
    }

    static func __setStatic(string name, any value): void {
        io.println(name + "=" + (value as string));
    }

    static func __callStatic(string name, list<any> args): string {
        return "staticcall:" + name + ":" + (args.length() as string);
    }
}

Dynamic d = Dynamic();
d.name = "Ada";
io.println(d.name);
io.println(d.missing(1, 2));
io.println(d(4));
io.println(Dynamic.value);
Dynamic.value = 7;
io.println(Dynamic.make("x"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "Ada\nmissing:2\n5\nstatic:value\nvalue=7\nstaticcall:make:1\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsJSONBuiltins(t *testing.T) {
	input := `import io;
import json;

let data = json.parse('{"name":"Ada","count":3,"items":[true,null]}');
io.println(data["name"]);
io.println(data["count"] + 4);
io.println(data["items"][0]);
io.println(json.stringify({"ok": true, "name": data["name"]}));
io.println(json.validate('{"ok":true}'));
io.println(json.validate('{"ok":'));
let parsed = json.tryParse('{"ok": true}');
io.println(parsed["ok"]);
io.println(parsed["value"]["ok"]);
let failed = json.tryParse('{"ok":');
io.println(failed["ok"]);
io.println(failed["error"]["line"] > 0);
io.println(failed["error"]["column"] > 0);
let detailed = json.validateDetailed('{"ok":');
io.println(detailed["valid"]);
io.println(detailed["error"]["offset"] > 0);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "Ada\n7\ntrue\n{\"name\":\"Ada\",\"ok\":true}\ntrue\nfalse\ntrue\ntrue\nfalse\ntrue\ntrue\nfalse\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsXMLBuiltins(t *testing.T) {
	input := `import io;
import xml;

io.println(xml.validate("<root><child /></root>"));
io.println(xml.validate("<root><child></root>"));
io.println(xml.validate("<a></a><b></b>"));
let parsed = xml.parse('<root id="1"><child>text</child></root>');
io.println(parsed["name"]);
io.println(parsed["attributes"]["id"]);
io.println(parsed["children"][0]["name"]);
io.println(parsed["children"][0]["text"]);
io.println(xml.stringify(parsed));
let tryParsed = xml.tryParse('<root />');
io.println(tryParsed["ok"]);
io.println(tryParsed["value"]["name"]);
let detailed = xml.validateDetailed("<root><child></root>");
io.println(detailed["valid"]);
io.println(detailed["error"]["line"] > 0);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "true\nfalse\nfalse\nroot\n1\nchild\ntext\n<root id=\"1\"><child>text</child></root>\ntrue\nroot\nfalse\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestEvaluatorInstallsStreamInterfaces(t *testing.T) {
	input := `import io;
import json;

class Handler implements json.JsonStreamInterface {
    func onStartObject() {}
    func onEndObject() {}
    func onStartArray() {}
    func onEndArray() {}
    func onKey(string key) {}
    func onValue(any value) {}
    func onError(any error) {}
}

let handler = Handler();
io.println(handler instanceof json.JsonStreamInterface);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if out.String() != "true\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestEvaluatorRunsJSONReader(t *testing.T) {
	input := `import io;
import json;

let reader = json.reader('{"name":"Ada","items":[1,true]}');
while (reader.hasNext()) {
    let event = reader.next();
    io.println(event["type"] + ":" + (event["value"] as string));
}
io.println(reader.next() == null);
reader.close();
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "startObject:null\nkey:name\nvalue:Ada\nkey:items\nstartArray:null\nvalue:1\nvalue:true\nendArray:null\nendObject:null\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsJSONReaderFromFileHandle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.json")
	if err := os.WriteFile(path, []byte(`{"items":[1,2]}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	input := `import io;
import json;

let file = io.open(` + strconv.Quote(path) + `, "r");
let reader = json.reader(file);
while (reader.hasNext()) {
    let event = reader.next();
    io.println(event["type"] + ":" + (event["value"] as string));
}
reader.close();
io.close(file);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "startObject:null\nkey:items\nstartArray:null\nvalue:1\nvalue:2\nendArray:null\nendObject:null\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsJSONStreamHandler(t *testing.T) {
	input := `import io;
import json;

class Handler implements json.JsonStreamInterface {
    func onStartObject() { io.println("startObject"); }
    func onEndObject() { io.println("endObject"); }
    func onStartArray() { io.println("startArray"); }
    func onEndArray() { io.println("endArray"); }
    func onKey(string key) { io.println("key:" + key); }
    func onValue(any value) { io.println("value:" + (value as string)); }
    func onError(any error) { io.println("error:" + error["message"]); }
}

let count = json.stream('{"name":"Ada","items":[1]}', Handler());
io.println("count:" + (count as string));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "startObject\nkey:name\nvalue:Ada\nkey:items\nstartArray\nvalue:1\nendArray\nendObject\ncount:8\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsXMLReader(t *testing.T) {
	input := `import io;
import xml;

let reader = xml.reader('<root id="1"><child>text</child><!--note--></root>');
while (reader.hasNext()) {
    let event = reader.next();
    if (event["type"] == "startElement") {
        if (event["value"]["name"] == "root") {
            io.println(event["type"] + ":" + event["value"]["name"] + ":" + event["value"]["attributes"]["id"]);
        } else {
            io.println(event["type"] + ":" + event["value"]["name"]);
        }
    } else {
        io.println(event["type"] + ":" + (event["value"] as string));
    }
}
io.println(reader.next() == null);
reader.close();
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "startElement:root:1\nstartElement:child\ntext:text\nendElement:child\ncomment:note\nendElement:root\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsXMLReaderFromFileHandle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.xml")
	if err := os.WriteFile(path, []byte(`<root><item>A</item></root>`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	input := `import io;
import xml;

let file = io.open(` + strconv.Quote(path) + `, "r");
let reader = xml.reader(file);
while (reader.hasNext()) {
    let event = reader.next();
    if (event["type"] == "startElement") {
        io.println(event["type"] + ":" + event["value"]["name"]);
    } else {
        io.println(event["type"] + ":" + (event["value"] as string));
    }
}
reader.close();
io.close(file);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "startElement:root\nstartElement:item\ntext:A\nendElement:item\nendElement:root\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorXMLReaderReportsParseErrors(t *testing.T) {
	input := `import io;
import xml;

let reader = xml.reader('<root><child></root>');
let last = null;
while (reader.hasNext()) {
    last = reader.next();
}
io.println(last["type"]);
io.println(last["value"]["line"] > 0);
reader.close();
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "error\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsXMLStreamHandler(t *testing.T) {
	input := `import io;
import xml;

class Handler implements xml.XmlStreamInterface {
    func onStartElement(string name, dict attributes) {
        if (name == "root") {
            io.println("start:" + name + ":" + attributes["id"]);
        } else {
            io.println("start:" + name);
        }
    }
    func onEndElement(string name) { io.println("end:" + name); }
    func onText(string text) { io.println("text:" + text); }
    func onComment(string text) { io.println("comment:" + text); }
    func onError(any error) { io.println("error:" + error["message"]); }
}

let count = xml.stream('<root id="1"><child>text</child><!--note--></root>', Handler());
io.println("count:" + (count as string));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "start:root:1\nstart:child\ntext:text\nend:child\ncomment:note\nend:root\ncount:6\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsCSVReader(t *testing.T) {
	input := `import io;
import csv;

let reader = csv.reader("name,age\nAda,37\nLinus,55\n");
while (reader.hasNext()) {
    let event = reader.next();
    io.println(event["type"] + ":" + event["value"][0] + "|" + event["value"][1]);
}
io.println(reader.next() == null);
reader.close();
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "header:name|age\nrow:Ada|37\nrow:Linus|55\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsCSVReaderFromFileHandle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.csv")
	if err := os.WriteFile(path, []byte("name,age\nAda,37\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	input := `import io;
import csv;

let file = io.open(` + strconv.Quote(path) + `, "r");
let reader = csv.reader(file);
while (reader.hasNext()) {
    let event = reader.next();
    io.println(event["type"] + ":" + event["value"][0] + "|" + event["value"][1]);
}
reader.close();
io.close(file);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "header:name|age\nrow:Ada|37\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsCSVStreamHandler(t *testing.T) {
	input := `import io;
import csv;

class Handler implements csv.CsvStreamInterface {
    func onHeader(list<string> columns) { io.println("header:" + columns[0] + "|" + columns[1]); }
    func onRow(list<string> row) { io.println("row:" + row[0] + "|" + row[1]); }
    func onError(any error) { io.println("error:" + error["message"]); }
}

let count = csv.stream("name,age\nAda,37\n", Handler());
io.println("count:" + (count as string));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "header:name|age\nrow:Ada|37\ncount:2\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorCSVStreamHandlerReceivesErrors(t *testing.T) {
	input := `import io;
import csv;

class Handler implements csv.CsvStreamInterface {
    func onHeader(list<string> columns) { io.println("header"); }
    func onRow(list<string> row) { io.println("row"); }
    func onError(any error) {
        io.println("error");
        io.println(error["line"] > 0);
        io.println(error["column"] > 0);
    }
}

let count = csv.stream("name,age\nAda\n", Handler());
io.println("count:" + (count as string));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "header\nerror\ntrue\ntrue\ncount:2\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsYAMLReader(t *testing.T) {
	input := `import io;
import yaml;

let reader = yaml.reader("name: Ada\nroles:\n  - admin\nactive: true\n");
while (reader.hasNext()) {
    let event = reader.next();
    io.println(event["type"] + ":" + (event["value"] as string));
}
io.println(reader.next() == null);
reader.close();
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "startMap:null\nkey:name\nvalue:Ada\nkey:roles\nstartList:null\nvalue:admin\nendList:null\nkey:active\nvalue:true\nendMap:null\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsYAMLReaderFromFileHandle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.yaml")
	if err := os.WriteFile(path, []byte("name: Ada\nroles:\n  - admin\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	input := `import io;
import yaml;

let file = io.open(` + strconv.Quote(path) + `, "r");
let reader = yaml.reader(file);
while (reader.hasNext()) {
    let event = reader.next();
    io.println(event["type"] + ":" + (event["value"] as string));
}
reader.close();
io.close(file);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "startMap:null\nkey:name\nvalue:Ada\nkey:roles\nstartList:null\nvalue:admin\nendList:null\nendMap:null\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorFormatReadersUseGenericStreamSources(t *testing.T) {
	input := `import io;
import bytes;
import json;
import xml;
import csv;
import yaml;

let jsonReader = json.reader(bytes.fromString('{"ok":true}'));
while (jsonReader.hasNext()) {
    let event = jsonReader.next();
    if (event["type"] == "value") {
        io.println("json:" + (event["value"] as string));
    }
}
jsonReader.close();

let xmlBuffer = io.buffer();
io.write(xmlBuffer, "<root>ok</root>");
let xmlReader = xml.reader(xmlBuffer);
while (xmlReader.hasNext()) {
    let event = xmlReader.next();
    if (event["type"] == "text") {
        io.println("xml:" + event["value"]);
    }
}
xmlReader.close();
io.close(xmlBuffer);

let csvReader = csv.reader(bytes.fromString("name\nAda\n"));
while (csvReader.hasNext()) {
    let event = csvReader.next();
    if (event["type"] == "row") {
        io.println("csv:" + event["value"][0]);
    }
}
csvReader.close();

let yamlBuffer = io.buffer();
io.write(yamlBuffer, "name: Ada\n");
let yamlReader = yaml.reader(yamlBuffer);
while (yamlReader.hasNext()) {
    let event = yamlReader.next();
    if (event["type"] == "key" || event["type"] == "value") {
        io.println("yaml:" + (event["value"] as string));
    }
}
yamlReader.close();
io.close(yamlBuffer);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "json:true\nxml:ok\ncsv:Ada\nyaml:name\nyaml:Ada\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsYAMLStreamHandler(t *testing.T) {
	input := `import io;
import yaml;

class Handler implements yaml.YamlStreamInterface {
    func onStartMap() { io.println("startMap"); }
    func onEndMap() { io.println("endMap"); }
    func onStartList() { io.println("startList"); }
    func onEndList() { io.println("endList"); }
    func onKey(string key) { io.println("key:" + key); }
    func onValue(any value) { io.println("value:" + (value as string)); }
    func onError(any error) { io.println("error:" + error["message"]); }
}

let count = yaml.stream("name: Ada\nroles:\n  - admin\n", Handler());
io.println("count:" + (count as string));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "startMap\nkey:name\nvalue:Ada\nkey:roles\nstartList\nvalue:admin\nendList\nendMap\ncount:8\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsTOMLDetailedParseHelpers(t *testing.T) {
	input := `import io;
import toml;

let parsed = toml.tryParse("name = \"Ada\"\n");
io.println(parsed["ok"]);
io.println(parsed["value"]["name"]);
let failed = toml.tryParse("name = \n");
io.println(failed["ok"]);
io.println(failed["error"]["line"] > 0);
io.println(toml.validate("name = \"Ada\"\n"));
let detailed = toml.validateDetailed("name = \n");
io.println(detailed["valid"]);
io.println(detailed["error"]["message"].length() > 0);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "true\nAda\nfalse\ntrue\ntrue\nfalse\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsYAMLDetailedParseHelpers(t *testing.T) {
	input := `import io;
import yaml;

let parsed = yaml.tryParse("name: Ada\n");
io.println(parsed["ok"]);
io.println(parsed["value"]["name"]);
let failed = yaml.tryParse("name: [\n");
io.println(failed["ok"]);
io.println(failed["error"]["line"] > 0);
io.println(yaml.validate("name: Ada\n"));
let detailed = yaml.validateDetailed("name: [\n");
io.println(detailed["valid"]);
io.println(detailed["error"]["message"].length() > 0);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "true\nAda\nfalse\ntrue\ntrue\nfalse\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsCryptAndDatetimeBuiltins(t *testing.T) {
	input := `import io;
import crypt;
import datetime;

io.println(crypt.md5("abc"));
io.println(crypt.sha1("abc"));
io.println(crypt.sha256("abc"));
io.println(crypt.sha3_256("abc"));
io.println(crypt.blake2b("abc"));
io.println(crypt.crc32("abc"));
io.println(crypt.hmacSha256("key", "message"));
let passwordHash = crypt.bcryptHash("secret", 4);
io.println(crypt.bcryptVerify("secret", passwordHash));
io.println(crypt.bcryptVerify("wrong", passwordHash));
let argonHash = crypt.argon2idHash("secret", {"memory": 64, "time": 1, "parallelism": 1, "keyLength": 16, "saltLength": 8});
io.println(argonHash.startsWith("$argon2id$"));
io.println(crypt.argon2idVerify("secret", argonHash));
io.println(crypt.argon2idVerify("wrong", argonHash));
io.println(crypt.base64Encode("hello"));
io.println(crypt.base64Decode("aGVsbG8="));
io.println(crypt.randomHex(4).length());
io.println(datetime.unix(0));
let parsed = datetime.parse("1970-01-01T00:00:00Z");
io.println(parsed);
io.println(datetime.addSeconds(parsed, 60));
io.println(datetime.addDays(parsed, 1));
io.println(datetime.addMonths(parsed, 1));
io.println(datetime.addYears(parsed, 1));
let delta = datetime.diff(parsed, datetime.addSeconds(parsed, 90061));
io.println(delta["days"]);
io.println(delta["hours"]);
io.println(delta["minutes"]);
io.println(delta["seconds"]);
io.println(datetime.toLocal(parsed, "UTC"));
io.println(datetime.toUtc(parsed));
let now = datetime.now();
io.println(now.hasKey("year"));
io.println(datetime.format(0, "2006-01-02"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "900150983cd24fb0d6963f7d28e17f72\na9993e364706816aba3e25717850c26c9cd0d89d\nba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\n" +
		"3a985da74fe225b2045c172d6bd390bd855f086e3e9d525b46bfe24511431532\nbddd813c634239723171ef3fee98579b94964e3bb1cb3e427262c8c068d52319\n891568578\n" +
		"6e9ef29b75fffc5b7abae527d58fdadb2fe42e7219011976917343065f58ed4a\n" +
		"true\nfalse\ntrue\ntrue\nfalse\naGVsbG8=\nhello\n8\n1970-01-01T00:00:00Z\n0\n60\n86400\n2678400\n31536000\n1\n1\n1\n1\n1970-01-01T00:00:00Z\n1970-01-01T00:00:00Z\ntrue\n1970-01-01\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsIOAndSysBuiltins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	input := `import io;
import sys;

io.writeText(` + strconv.Quote(path) + `, "hello");
io.println(io.exists(` + strconv.Quote(path) + `));
io.println(io.readText(` + strconv.Quote(path) + `));
sys.setenv("GEBLANG_TEST_VALUE", "ok");
io.println(sys.getenv("GEBLANG_TEST_VALUE"));
io.println(sys.getenv("GEBLANG_TEST_MISSING") == null);
let args = sys.args();
io.println(args.length());
io.println(args[0]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.NewWithArgs(&out, []string{"first"}).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "true\nhello\nok\ntrue\n1\nfirst\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsExpandedIOBuiltins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")
	csvPath := filepath.Join(dir, "data.csv")
	renamedPath := filepath.Join(dir, "renamed.txt")
	linkPath := filepath.Join(dir, "data-link.txt")
	input := `import io;

let handle = io.open(` + strconv.Quote(path) + `, "w");
io.write(handle, "hello");
io.flush(handle);
io.sync(handle);
io.dataSync(handle);
io.lock(handle);
io.unlock(handle);
io.println(io.tryLock(handle, "shared"));
io.unlock(handle);
io.close(handle);

io.appendText(` + strconv.Quote(path) + `, " world");
io.symlink(` + strconv.Quote(path) + `, ` + strconv.Quote(linkPath) + `);
io.println(io.readLink(` + strconv.Quote(linkPath) + `) == ` + strconv.Quote(path) + `);
io.remove(` + strconv.Quote(linkPath) + `);
let reader = io.open(` + strconv.Quote(path) + `, "r");
io.println(io.read(reader, 5));
io.println(io.readAll(reader));
io.close(reader);

let stat = io.stat(` + strconv.Quote(path) + `);
io.println(stat["size"]);
io.chmod(` + strconv.Quote(path) + `, 420);
io.println(io.stat(` + strconv.Quote(path) + `)["mode"]);

io.writeCSV(` + strconv.Quote(csvPath) + `, [["name", "age"], ["Ada", 37]]);
let rows = io.readCSV(` + strconv.Quote(csvPath) + `);
io.println(rows[1][0]);

let tempFile = io.tempFile(` + strconv.Quote(dir) + `, "geb-test-*.txt");
io.writeText(tempFile, "tmp");
io.println(io.readText(tempFile));
io.remove(tempFile);

let tempDir = io.tempDir(` + strconv.Quote(dir) + `, "geb-dir-*");
io.println(io.exists(tempDir));
io.remove(tempDir);

io.rename(` + strconv.Quote(path) + `, ` + strconv.Quote(renamedPath) + `);
io.println(io.exists(` + strconv.Quote(renamedPath) + `));
io.println(io.listDir(` + strconv.Quote(dir) + `).length() >= 2);
io.remove(` + strconv.Quote(renamedPath) + `);
io.println(io.exists(` + strconv.Quote(renamedPath) + `));
let buf = io.buffer();
io.write(buf, "hello");
io.writeln(buf, " world");
io.println(io.bufferToString(buf));
io.println(buf.length());
buf.reset();
buf.write("method");
buf.writeln(" call");
io.println(buf.toString());
io.flush(buf);
io.close(buf);
io.stdoutWrite("stdout-ok\n");
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "true\ntrue\nhello\n world\n11\n420\nAda\ntmp\ntrue\ntrue\ntrue\nfalse\nhello world\n\n12\nmethod call\n\nstdout-ok\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsIOStreamsAndCapture(t *testing.T) {
	input := `import bytes;
import io;

let mem = io.memory("seed");
io.write(mem, "-text");
io.writeBytes(mem, bytes.fromHex("2d6279746573"));
io.println(io.toString(mem));
io.println(mem.bytes().toString());

let out = io.stdout();
io.writeln(out, "direct stdout");

let capturedOut = io.captureStdout();
io.println("hidden stdout");
io.stdoutWrite("raw stdout\n");
let capturedOutText = capturedOut.read();
capturedOut.close();
io.print(capturedOutText);

let capturedErr = io.captureStderr();
io.stderrPrintln("hidden stderr");
io.stderrWrite("raw stderr\n");
let capturedErrText = capturedErr.read();
capturedErr.close();
io.print(capturedErrText);

let redirected = io.memory();
let restoreOut = io.redirectStdout(redirected);
io.println("redirected stdout");
restoreOut();
io.print(io.toString(redirected));

let inputStream = io.memory("line one\nline two\n");
let restoreIn = io.redirectStdin(inputStream);
io.println(io.stdinReadLine());
io.println(io.stdinReadLine());
restoreIn();
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "seed-text-bytes\nseed-text-bytes\ndirect stdout\nhidden stdout\nraw stdout\nhidden stderr\nraw stderr\nredirected stdout\nline one\nline two\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsDestructuringAssignment(t *testing.T) {
	input := `import io;
let left = 0;
let right = 0;
[left, right] = [7, 8];
io.println(left);
io.println(right);
let name = "";
let age = 0;
{name, age} = {"name": "Ada", "age": 37};
io.println(name);
io.println(age);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "7\n8\nAda\n37\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsSysRunBuiltin(t *testing.T) {
	input := `import io;
import sys;

let result = sys.run("printf", "hello");
io.println(result["code"]);
io.println(result["stdout"]);
io.println(result["stderr"] == "");
let shell = sys.shell("printf shell");
io.println(shell["stdout"]);
let proc = sys.start("printf", "started");
io.println(sys.processReadStdout(proc));
io.println(sys.processWait(proc));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "0\nhello\ntrue\nshell\nstarted\n0\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorCleanupTerminatesUnwaitedProcesses(t *testing.T) {
	input := `import io;
import sys;

let proc = sys.start("sleep", "30");
io.println(sys.processPid(proc));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(out.String()))
	if err != nil {
		t.Fatalf("pid output: %q", out.String())
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("find process: %v", err)
	}
	if err := process.Signal(syscall.Signal(0)); err == nil {
		_ = process.Kill()
		t.Fatalf("process %d still exists after evaluator cleanup", pid)
	}
}

func TestEvaluatorRunsProcessOptions(t *testing.T) {
	dir := t.TempDir()
	input := `import io;
import sys;

let result = sys.runWithOptions({
    "command": "sh",
    "args": ["-c", "printf $GEB_TEST:$PWD"],
    "cwd": ` + strconv.Quote(dir) + `,
    "env": {"GEB_TEST": "ok"}
});
io.println(result["code"]);
io.println(result["stdout"].contains("ok:"));
io.println(result["stdout"].contains(` + strconv.Quote(dir) + `));
io.println(result["timedOut"]);

let timed = sys.runWithOptions({
    "command": "sh",
    "args": ["-c", "sleep 1"],
    "timeoutMs": 10
});
io.println(timed["timedOut"]);

let proc = sys.startWithOptions({
    "command": "sh",
    "args": ["-c", "printf $GEB_TEST"],
    "env": {"GEB_TEST": "started"}
});
io.println(sys.processReadStdout(proc));
io.println(sys.processWait(proc));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "0\ntrue\ntrue\nfalse\ntrue\nstarted\n0\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsProcessControlHelpers(t *testing.T) {
	input := `import io;
import sys;

let proc = sys.start("sh", "-c", "printf abcdef; sleep 1");
io.println(sys.processPid(proc) > 0);
io.println(sys.processReadStdoutN(proc, 3));
io.println(sys.processReadStdout(proc));
io.println(sys.processWait(proc));

let proc2 = sys.start("sh", "-c", "sleep 5");
sys.processSignal(proc2, "KILL");
io.println(sys.processWait(proc2) != 0);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "true\nabc\ndef\n0\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsHTTPClientBuiltin(t *testing.T) {
	server := newLocalHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/plain" && r.Header.Get("X-Test") != "yes" {
			t.Errorf("missing X-Test header")
		}
		w.Header().Set("Content-Type", "text/plain")
		if r.URL.Path == "/json" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"method":%q,"path":%q}`, r.Method, r.URL.Path)
			return
		}
		if r.URL.Path == "/echo-json" {
			w.Header().Set("Content-Type", "application/json")
			data, _ := io.ReadAll(r.Body)
			fmt.Fprintf(w, `{"contentType":%q,"body":%s}`, r.Header.Get("Content-Type"), data)
			return
		}
		fmt.Fprintf(w, "%s:%s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	input := `import io;
import bytes;
import http;

let response = http.request("GET", "` + server.URL + `/hello", "", {"X-Test": "yes"});
io.println(response["status"]);
io.println(response["body"]);
io.println(response["headers"].get("Content-Type"));
let get = http.get("` + server.URL + `/plain");
io.println(get["body"]);
let post = http.post("` + server.URL + `/submit2", "payload", {"X-Test": "yes"});
io.println(post["body"]);
let jsonResponse = http.postJson("` + server.URL + `/echo-json", {"name": "Geblang"}, {"X-Test": "yes"});
let parsed = http.parseJson(jsonResponse);
io.println(parsed["contentType"]);
io.println(parsed["body"]["name"]);
let response2 = http.requestWithOptions({
    "method": "POST",
    "url": "` + server.URL + `/submit",
    "body": "payload",
    "headers": {"X-Test": "yes"},
    "timeoutMs": 1000
});
io.println(response2["status"]);
io.println(response2["body"]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "200\nGET:/hello\ntext/plain\nGET:/plain\nPOST:/submit2\napplication/json\nGeblang\n200\nPOST:/submit\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsHTTPRequestResponseObjects(t *testing.T) {
	input := `import io;
import bytes;
import http;

let req = http.Request({
    "method": "POST",
    "path": "/items",
    "query": "limit=1",
    "remoteAddr": "127.0.0.1:1",
    "body": "{\"name\":\"Ada\"}",
    "headers": {"Content-Type": "application/json"}
});
io.println(req.method);
io.println(req.header("content-type"));
io.println(req.bodyText());
io.println(req.bodyBytes().length() as string);
let parsed = req.json();
io.println(parsed["name"]);

let res = http.Response("created", 201, {"X-Test": "yes"}).withHeader("Content-Type", "text/plain");
io.println(res.status as string);
io.println(res.headers["X-Test"]);
io.println(res.headers["Content-Type"]);
let dict = res.toDict();
io.println(dict["body"]);

let res2 = http.response({"status": 202, "body": "accepted"});
io.println(res2.status as string);

let binaryRes = http.Response(bytes.fromString("binary"), 204);
io.println(binaryRes.body.length() as string);

let buffer = io.buffer();
io.write(buffer, "buffered");
let streamRes = http.Response(buffer, 205);
io.println(typeof(streamRes.body));
io.close(buffer);

let jsonRes = http.jsonResponse({"ok": true}, 203);
io.println(jsonRes.status as string);
io.println(jsonRes.headers["Content-Type"]);
io.println(jsonRes.body);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "POST\napplication/json\n{\"name\":\"Ada\"}\n14\nAda\n201\nyes\ntext/plain\ncreated\n202\n6\nIOBuffer\n203\napplication/json\n{\"ok\":true}\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsHTTPHeadersObject(t *testing.T) {
	input := `import io;
import http;

let headers = http.Headers({"content-type": "text/plain", "Set-Cookie": ["a=1", "b=2"]});
io.println(headers.get("Content-Type"));
io.println(headers.getAll("set-cookie").length() as string);
io.println(headers.has("CONTENT-TYPE") as string);
let updated = headers.set("content-type", "application/json").add("set-cookie", "c=3");
io.println(updated.get("Content-Type"));
io.println(updated.getAll("Set-Cookie").length() as string);
let deleted = updated.delete("content-type");
io.println(deleted.has("Content-Type") as string);
let dict = updated.toDict();
io.println(dict["Content-Type"]);
io.println(dict["Set-Cookie"].length() as string);

let req = http.Request({"headers": updated});
io.println(req.header("content-type"));
let reqDict = req.toDict();
io.println(reqDict["headers"]["Set-Cookie"].length() as string);

let res = http.Response("ok", 200, updated);
let resDict = res.toDict();
io.println(resDict["headers"]["Content-Type"]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	want := "text/plain\n2\ntrue\napplication/json\n3\nfalse\napplication/json\n3\napplication/json\n3\napplication/json\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsHTTPCookieObject(t *testing.T) {
	input := `import io;
import http;

let cookie = http.Cookie({
    "name": "sid",
    "value": "abc",
    "path": "/",
    "httpOnly": true,
    "secure": true,
    "sameSite": "lax",
    "maxAge": 3600
});
io.println(cookie.name());
io.println(cookie.value());
io.println(cookie.httpOnly() as string);
io.println(cookie.secure() as string);
io.println(cookie.sameSite());
io.println(cookie.maxAge() as string);
io.println(cookie.toHeader());

let updated = cookie.withValue("def").withSameSite("strict").withMaxAge(60);
io.println(updated.value());
io.println(updated.sameSite());
io.println(updated.maxAge() as string);
let dict = updated.toDict();
io.println(dict["name"]);
io.println(dict["httpOnly"] as string);

let parsed = http.Cookie("sid=xyz; Path=/app; HttpOnly; Secure; SameSite=None");
io.println(parsed.name());
io.println(parsed.value());
io.println(parsed.path());
io.println(parsed.httpOnly() as string);
io.println(parsed.secure() as string);
io.println(parsed.sameSite());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	output := out.String()
	for _, want := range []string{
		"sid\nabc\ntrue\ntrue\nlax\n3600\n",
		"sid=abc; Path=/; Max-Age=3600; HttpOnly; Secure; SameSite=Lax\n",
		"def\nstrict\n60\nsid\ntrue\nsid\nxyz\n/app\ntrue\ntrue\nnone\n",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q in %q", want, output)
		}
	}
}

func TestEvaluatorExportsStdlibTypesThroughModules(t *testing.T) {
	input := `import io;
import http;
import test;
import json;

io.println(dir().contains("Request"));
io.println(dir(http).contains("Request"));
io.println(dir(http).contains("Response"));
io.println(dir(test).contains("Test"));
io.println(dir(json).contains("JsonStreamInterface"));

let response = http.Response("ok", 200);
io.println(response instanceof http.Response);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "false\ntrue\ntrue\ntrue\ntrue\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsHTTPServerLifecycle(t *testing.T) {
	probe, listenErr := net.Listen("tcp", "127.0.0.1:0")
	if listenErr != nil {
		t.Skipf("local sockets unavailable: %v", listenErr)
	}
	_ = probe.Close()

	input := `import io;
import http;

func handle(http.Request request): http.Response {
    return http.Response("ok:" + request.path, 200, {"Content-Type": "text/plain"});
}

let server = http.listen("127.0.0.1:0", handle);
let addr = http.serverAddr(server);
let response = http.get("http://" + addr + "/hello");
io.println(response["status"] as string);
io.println(response["body"]);
io.println(response["headers"].get("Content-Type"));
http.shutdown(server, 1000);
http.close(server);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "200\nok:/hello\ntext/plain\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsHTTPStreamingResponse(t *testing.T) {
	probe, listenErr := net.Listen("tcp", "127.0.0.1:0")
	if listenErr != nil {
		t.Skipf("local sockets unavailable: %v", listenErr)
	}
	_ = probe.Close()

	input := `import http;
import io;
import web.sse as sse;

func handle(dict<string, any> request): dict<string, any> {
    return sse.streaming(func(any stream): void {
        sse.write(stream, sse.comment("connected"));
        sse.flush(stream);
        sse.write(stream, sse.named("tick", "one"));
        sse.flush(stream);
        sse.close(stream);
    });
}

let server = http.listen("127.0.0.1:0", handle);
let addr = http.serverAddr(server);
let response = http.get("http://" + addr + "/events");
io.println(response["status"] as string);
io.println(response["headers"].get("Content-Type"));
io.println(response["body"].contains("event: tick"));
io.println(response["body"].contains("data: one"));
http.shutdown(server, 1000);
http.close(server);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "200\ntext/event-stream; charset=utf-8\ntrue\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsSQLiteDatabaseBuiltins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.sqlite")
	input := `import io;
import db;

let conn = db.open("sqlite", ` + strconv.Quote(path) + `);
db.configure(conn, {
    "maxOpenConns": 4,
    "maxIdleConns": 2,
    "connMaxLifetimeMs": 60000,
    "connMaxIdleTimeMs": 30000
});
let stats = db.stats(conn);
io.println(stats["maxOpenConnections"]);

let migrations = db.migrate(conn, [
    {
        "id": "001_create_users",
        "sql": "create table users (id integer primary key, name text)"
    }
]);
io.println(migrations["applied"][0]);
let rerun = db.migrate(conn, [
    {
        "id": "001_create_users",
        "sql": "create table users (id integer primary key, name text)"
    }
]);
io.println(rerun["skipped"][0]);
let inserted = db.exec(conn, "insert into users (name) values (?)", "Ada");
let rows = db.query(conn, "select id, name from users where name = ?", "Ada");
io.println(inserted["rowsAffected"]);
io.println(rows.length());
io.println(rows[0]["name"]);

let tx = db.begin(conn);
db.txExec(tx, "insert into users (name) values (?)", "Grace");
let txRows = db.txQuery(tx, "select name from users where name = ?", "Grace");
io.println(txRows[0]["name"]);
db.commit(tx);

let rolledBack = db.begin(conn);
db.txExec(rolledBack, "insert into users (name) values (?)", "Rollback");
db.rollback(rolledBack);
let afterRollback = db.query(conn, "select name from users where name = ?", "Rollback");
io.println(afterRollback.length());

let insertUser = db.prepare(conn, "insert into users (name) values (?)");
db.stmtExec(insertUser, "Linus");
db.stmtClose(insertUser);

let findUser = db.prepare(conn, "select name from users where name = ?");
let preparedRows = db.stmtQuery(findUser, "Linus");
io.println(preparedRows[0]["name"]);
db.stmtClose(findUser);
db.close(conn);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "4\n001_create_users\n001_create_users\n1\n1\nAda\nGrace\n0\nLinus\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsStreamingDatabaseRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "streaming.sqlite")
	input := `import io;
import db;

let conn = db.Connection("sqlite", ` + strconv.Quote(path) + `);
defer conn.close();

conn.exec("create table users (id integer primary key, name text)");
conn.exec("insert into users (name) values (?)", "Ada");
conn.exec("insert into users (name) values (?)", "Grace");

let rows = conn.query("select name from users order by id");
defer rows.close();
io.println(rows.columns()[0]);
while (rows.next()) {
    let row = rows.row();
    io.println(row["name"]);
}
io.println(rows.row() == null);

let cached = conn.query("select name from users order by id");
defer cached.close();
io.println(cached.first()["name"]);
io.println(cached.length());
io.println(cached.get(1)["name"]);

let tx = conn.begin();
tx.exec("insert into users (name) values (?)", "Linus");
let txRows = tx.query("select name from users where name = ?", "Linus");
let txList = txRows.all();
txRows.close();
io.println(txList[0]["name"]);
tx.commit();
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "name\nAda\nGrace\ntrue\nAda\n2\nGrace\nLinus\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsStandardDatabaseBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "standard-binding.sqlite")
	input := `import io;
import db;

let conn = db.Connection({
    "driver": "sqlite",
    "path": ` + strconv.Quote(path) + `
});
defer conn.close();

conn.exec("create table users (id integer primary key, name text, email text)");
conn.exec(
    "insert into users (name, email) values (:name, :email)",
    {"name": "Ada", "email": "ada@example.com"}
);
conn.exec(
    "insert into users (name, email) values (?, ?)",
    ["Grace", "grace@example.com"]
);

let rows = conn.query(
    "select email from users where name = :name",
    {"name": "Ada"}
);
defer rows.close();
io.println(rows.first()["email"]);

let positional = conn.query(
    "select email from users where name = ?",
    ["Grace"]
);
defer positional.close();
io.println(positional.first()["email"]);

let stmt = conn.prepare("select name from users where email = :email");
let prepared = stmt.query({"email": "ada@example.com"});
defer prepared.close();
io.println(prepared.first()["name"]);
stmt.close();
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "ada@example.com\ngrace@example.com\nAda\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorLoadsUserModules(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "util")
	if err := os.Mkdir(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "math.gb"), []byte(`module util.math;

func hidden(int x): int {
    return x + 1;
}

export const string label = "calc";

export func double(int x): int {
    return hidden(x) * 2;
}
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}

	input := `import io;
import util.math as math;

io.println(math.label);
io.println(math.double(4));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.NewWithArgsAndModulePaths(&out, nil, []string{dir}).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "calc\n10\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorLoadsBundledStdlibModule(t *testing.T) {
	input := `import io;
import testing.assertions as assertions;

io.println(assertions.contains("Geblang", "lang") as string);
io.println(assertions.startsWith("Geblang", "Geb") as string);
io.println(assertions.endsWith("Geblang", "lang") as string);
io.println(assertions.isBlank("   ") as string);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.NewWithArgsAndModulePaths(&out, nil, []string{t.TempDir()}).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if out.String() != "true\ntrue\ntrue\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestEvaluatorLoadsBundledConfigModule(t *testing.T) {
	input := `import config;
import io;

let base = {"server": {"host": "127.0.0.1", "port": 8080}, "debug": false};
let overrides = {"server": {"port": 9000}};
let loaded = config.Config(config.merge(base, overrides));

io.println(loaded.get("server.host"));
io.println(loaded.get("server.port"));
io.println(loaded.getOr("missing", "fallback"));
io.println(config.parse("json", "{\"name\":\"Geblang\"}").get("name"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.NewWithArgsAndModulePaths(&out, nil, []string{t.TempDir()}).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if out.String() != "127.0.0.1\n9000\nfallback\nGeblang\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestEvaluatorLoadsBundledCLICommandModule(t *testing.T) {
	input := `import cli.command as command;
import io;

let deploy = command.newCommand("deploy", "Deploy an app");
deploy.option(command.newOption("env", "string").short("e").default("dev"));
deploy.option(command.newOption("dry-run", "bool"));

let parsed = deploy.requireParsed(["--env", "prod", "--dry-run"]);
io.println(parsed["env"]);
io.println(parsed["dry-run"]);
io.println(deploy.help().contains("--env"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.NewWithArgsAndModulePaths(&out, nil, []string{t.TempDir()}).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if out.String() != "prod\ntrue\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestEvaluatorLoadsDirectoryModulesFromGeblangPath(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "pkg", "greeter")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "init.gb"), []byte(`module pkg.greeter;

export func hello(string name): string {
    return "hello " + name;
}
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}
	t.Setenv("GEBLANG_PATH", dir)

	input := `import io;
import pkg.greeter;

io.println(greeter.hello("Ada"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "hello Ada\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorLoadsModulesFromPackageManifest(t *testing.T) {
	dir := t.TempDir()
	scriptDir := filepath.Join(dir, "scripts")
	moduleDir := filepath.Join(dir, "src", "util")
	libDir := filepath.Join(dir, "lib")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module dir: %v", err)
	}
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatalf("mkdir lib dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geblang.yaml"), []byte(`name: acme.tools
version: 0.1.0
source: src
paths:
  - lib
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "math.gb"), []byte(`module acme.tools.util.math;

export func double(int x): int {
    return x * 2;
}
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "extra.gb"), []byte(`module extra;

export const string label = "extra";
`), 0o644); err != nil {
		t.Fatalf("write extra module: %v", err)
	}

	input := `import io;
import acme.tools.util.math as math;
import extra;

io.println(math.double(6));
io.println(extra.label);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.NewWithArgsAndModulePaths(&out, nil, []string{scriptDir}).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "12\nextra\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

// A user file named like a built-in module must not shadow it: built-in names
// are reserved and always resolve to the built-in, identically on both backends.
// (The evaluator previously preferred the user file, diverging from the VM.)
func TestEvaluatorReservedNameIgnoresUserShadow(t *testing.T) {
	dir := t.TempDir()
	// A user `math.gb` that, if used, would return a string instead of a number.
	if err := os.WriteFile(filepath.Join(dir, "math.gb"), []byte("module math;\nexport func sqrt(any x): string { return \"USER\"; }\n"), 0o644); err != nil {
		t.Fatalf("write shadow: %v", err)
	}
	run := func(src string) string {
		p := parser.New(lexer.New(src))
		program := p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		var out bytes.Buffer
		if _, err := evaluator.NewWithArgsAndModulePaths(&out, nil, []string{dir}).Eval(program); err != nil {
			t.Fatalf("eval error: %v", err)
		}
		return out.String()
	}
	// Bare import: the native math wins, not the user shadow.
	if got := run("import math;\nimport io;\nio.println(math.sqrt(16.0));\n"); got != "4\n" {
		t.Fatalf("shadowed native math: got %q, want %q", got, "4\n")
	}
	// geblang. prefix resolves the built-in explicitly.
	if got := run("import geblang.math as m;\nimport io;\nio.println(m.sqrt(16.0));\n"); got != "4\n" {
		t.Fatalf("geblang.math: got %q, want %q", got, "4\n")
	}
}

func TestEvaluatorLoadsJSONPackageManifest(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geblang.json"), []byte(`{"package":{"name":"jsonpkg","version":"1.0.0"},"source":"src"}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "hello.gb"), []byte(`module jsonpkg.hello;

export func message(): string {
    return "json manifest";
}
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}

	input := `import io;
import jsonpkg.hello as hello;

io.println(hello.message());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.NewWithArgsAndModulePaths(&out, nil, []string{dir}).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "json manifest\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorLoadsLocalManifestDependencies(t *testing.T) {
	dir := t.TempDir()
	appDir := filepath.Join(dir, "app")
	depDir := filepath.Join(dir, "dep")
	if err := os.MkdirAll(filepath.Join(appDir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir app src: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(depDir, "src", "util"), 0o755); err != nil {
		t.Fatalf("mkdir dep src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "geblang.yaml"), []byte(`name: app.main
source: src
dependencies:
  dep.lib:
    path: ../dep
`), 0o644); err != nil {
		t.Fatalf("write app manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "geblang.yaml"), []byte(`name: dep.lib
source: src
`), 0o644); err != nil {
		t.Fatalf("write dep manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "src", "util", "format.gb"), []byte(`module dep.lib.util.format;

export func label(string value): string {
    return "dep:" + value;
}
`), 0o644); err != nil {
		t.Fatalf("write dependency module: %v", err)
	}

	input := `import io;
import dep.lib.util.format as format;

io.println(format.label("ok"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.NewWithArgsAndModulePaths(&out, nil, []string{filepath.Join(appDir, "src")}).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "dep:ok\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsNetworkingBuiltins(t *testing.T) {
	input := `import io;
import net;

let addr = net.joinHostPort("127.0.0.1", "8080");
let parts = net.splitHostPort(addr);
let hosts = net.lookupHost("localhost");

io.println(addr);
io.println(parts["host"]);
io.println(parts["port"]);
io.println(hosts.length() > 0);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "127.0.0.1:8080\n127.0.0.1\n8080\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsNetworkSocketBuiltins(t *testing.T) {
	tcpListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local TCP sockets unavailable: %v", err)
	}
	defer tcpListener.Close()
	tcpDone := make(chan struct{})
	go func() {
		defer close(tcpDone)
		conn, err := tcpListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 32)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte("tcp:" + string(buf[:n])))
	}()

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local UDP sockets unavailable: %v", err)
	}
	defer udpConn.Close()
	udpDone := make(chan struct{})
	go func() {
		defer close(udpDone)
		buf := make([]byte, 32)
		n, addr, err := udpConn.ReadFrom(buf)
		if err != nil {
			return
		}
		_, _ = udpConn.WriteTo([]byte("udp:"+string(buf[:n])), addr)
	}()

	input := `import bytes;
import io;
import net;

let client = net.connectTcp("` + tcpListener.Addr().String() + `");
net.setDeadline(client, 5000);
net.clearDeadline(client);
net.write(client, "hello");
io.println(net.read(client, 32).toString());
io.println(net.remoteAddr(client).length() > 0);
net.close(client);

let listener = net.listenTcp("127.0.0.1:0");
net.setDeadline(listener, 5000);
net.clearDeadline(listener);
io.println(net.localAddr(listener).contains(":"));
net.close(listener);

let udp = net.listenUdp("127.0.0.1:0");
net.setDeadline(udp, 5000);
net.clearDeadline(udp);
net.writeTo(udp, "` + udpConn.LocalAddr().String() + `", bytes.fromString("ping"));
let packet = net.readFrom(udp, 32);
io.println(packet["data"].toString());
io.println(packet["addr"].contains(":"));
net.close(udp);

let udpClient = net.dialUdp("` + udpConn.LocalAddr().String() + `");
net.setDeadline(udpClient, 5000);
net.clearDeadline(udpClient);
io.println(net.localAddr(udpClient).contains(":"));
net.close(udpClient);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err = evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	<-tcpDone
	<-udpDone

	want := "tcp:hello\ntrue\ntrue\nudp:ping\ntrue\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorLoadsBundledRedisModule(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local TCP sockets unavailable: %v", err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			req := string(buf[:n])
			switch {
			case strings.Contains(req, "\r\nPING\r\n"):
				_, _ = conn.Write([]byte("+PONG\r\n"))
			case strings.Contains(req, "\r\nSET\r\n"):
				_, _ = conn.Write([]byte("+OK\r\n"))
			case strings.Contains(req, "\r\nGET\r\n"):
				_, _ = conn.Write([]byte("$3\r\nAda\r\n"))
			case strings.Contains(req, "\r\nEXISTS\r\n"):
				_, _ = conn.Write([]byte(":1\r\n"))
			case strings.Contains(req, "\r\nINCR\r\n"):
				_, _ = conn.Write([]byte(":2\r\n"))
			case strings.Contains(req, "\r\nEXPIRE\r\n"):
				_, _ = conn.Write([]byte(":1\r\n"))
			case strings.Contains(req, "\r\nTTL\r\n"):
				_, _ = conn.Write([]byte(":60\r\n"))
			case strings.Contains(req, "\r\nLPUSH\r\n"):
				_, _ = conn.Write([]byte(":1\r\n"))
			case strings.Contains(req, "\r\nLRANGE\r\n"):
				_, _ = conn.Write([]byte("*2\r\n$3\r\none\r\n$3\r\ntwo\r\n"))
			case strings.Contains(req, "\r\nSADD\r\n"):
				_, _ = conn.Write([]byte(":1\r\n"))
			case strings.Contains(req, "\r\nSISMEMBER\r\n"):
				_, _ = conn.Write([]byte(":1\r\n"))
			case strings.Contains(req, "\r\nSMEMBERS\r\n"):
				_, _ = conn.Write([]byte("*2\r\n$3\r\nred\r\n$4\r\nblue\r\n"))
			case strings.Contains(req, "\r\nHSET\r\n"):
				_, _ = conn.Write([]byte(":1\r\n"))
			case strings.Contains(req, "\r\nHGETALL\r\n"):
				_, _ = conn.Write([]byte("*4\r\n$4\r\nname\r\n$3\r\nAda\r\n$4\r\nrole\r\n$5\r\nadmin\r\n"))
			case strings.Contains(req, "\r\nDEL\r\n"):
				_, _ = conn.Write([]byte(":1\r\n"))
			default:
				_, _ = conn.Write([]byte("-ERR unsupported\r\n"))
			}
		}
	}()

	input := `import io;
import redis;

let client = redis.connect("` + listener.Addr().String() + `");
io.println(client.ping());
io.println(client.set("user", "Ada"));
io.println(client.get("user"));
io.println(client.exists("user"));
io.println(client.incr("visits"));
io.println(client.expire("user", 60));
io.println(client.ttl("user"));
io.println(client.lpush("jobs", "one"));
io.println(client.lrange("jobs", 0, -1)[1]);
io.println(client.sadd("colors", "red"));
io.println(client.sismember("colors", "red"));
io.println(client.smembers("colors")[0]);
io.println(client.hset("profile", "name", "Ada"));
let profile = client.hgetAll("profile");
io.println(profile["role"]);
io.println(client.del("user"));
client.close();
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err = evaluator.NewWithArgsAndModulePaths(&out, nil, []string{t.TempDir()}).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	<-done

	want := "true\ntrue\nAda\ntrue\n2\ntrue\n60\n1\ntwo\n1\ntrue\nred\n1\nadmin\n1\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsTestingModule(t *testing.T) {
	input := `import io;
import test;

class MathTest extends test.Test {
    int value = 0;
    int classSetup = 0;
    int classTeardown = 0;

    func setupClass(): void {
        this.classSetup = this.classSetup + 1;
    }

    func teardownClass(): void {
        this.classTeardown = this.classTeardown + 1;
    }

    func setup(): void {
        this.value = 10;
    }

    func teardown(): void {
        this.value = 0;
    }

    @tag("fast")
    @test
    func addition(): void {
        this.equal(2 + 2, 4);
        this.assertEquals(4, 2 + 2);
        this.assertNotEquals(5, 2 + 2);
        this.isTrue(4 > 3);
        this.assertTrue(4 > 3);
        this.isFalse(3 > 4);
        this.assertFalse(3 > 4);
        this.notNull("ok");
        this.assertNotNull("ok");
        this.assertNull(null);
        this.assertContains("hello Geblang", "Geb");
        this.assertNotContains("hello Geblang", "PHP");
        this.assertContains([1, 2, 3], 2);
        this.assertContains({"name": "Ada"}, "name");
        this.assertEmpty([]);
        this.assertNotEmpty(["ok"]);
        this.assertGreaterThan(3, 4);
        this.assertGreaterThanOrEqual(4, 4);
        this.assertLessThan(5, 4);
        this.assertLessThanOrEqual(4, 4);
        this.equal(this.value, 10);
        this.equal(this.classSetup, 1);
    }

    @tag("slow")
    @test
    func failure(): void {
        this.assertEquals(2, 1);
    }
}

let result = test.run(MathTest);
io.println(result["total"]);
io.println(result["passed"]);
io.println(result["failed"]);
io.println(result["failures"].length());
let fast = test.run(MathTest, {"tags": ["fast"]});
io.println(fast["total"]);
io.println(fast["passed"]);
io.println(fast["failed"]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "2\n1\n1\n1\n1\n1\n0\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsLogModule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	input := `import io;
import log;

class Capture implements log.LogInterface {
    string last = "";

    func handle(string level, string message, dict<string, any> fields): void {
        this.last = level + ":" + message + ":" + fields["id"];
    }
}

let stdout = log.stdout();
log.info(stdout, "hello", {"id": "1"});

let file = log.file(` + strconv.Quote(path) + `);
log.warn(file, "file", {"id": "2"});
log.close(file);
io.println(io.readText(` + strconv.Quote(path) + `).length() > 0);

Capture capture = Capture();
let custom = log.custom(capture);
log.error(custom, "custom", {"id": "3"});
io.println(capture.last);
io.println(capture instanceof log.LogInterface);
io.println(dir(log).contains("LogInterface"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, `"level":"info"`) || !strings.Contains(got, `"message":"hello"`) {
		t.Fatalf("stdout log missing fields: %q", got)
	}
	if !strings.Contains(got, "true\nerror:custom:3\ntrue\ntrue\n") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestEvaluatorRunsPathModule(t *testing.T) {
	input := `import io;
import path;

let joined = path.join("a", "b", "file.txt");
io.println(joined);
io.println(path.base(joined));
io.println(path.dir(joined));
io.println(path.ext(joined));
io.println(path.clean("a/../b"));
io.println(path.rel("/tmp", "/tmp/app/file.txt"));
io.println(path.abs(".").length() > 0);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "a/b/file.txt\nfile.txt\na/b\n.txt\nb\napp/file.txt\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsMathModule(t *testing.T) {
	input := `import io;
import math;

io.println(math.abs(-5));
io.println(math.min(3, 1, 2));
io.println(math.max(3, 1, 2));
io.println(math.clamp(12, 0, 10));
io.println(math.floor(3.7));
io.println(math.ceil(3.1));
io.println(math.round(3.5));
io.println(math.floor(9007199254740993));
io.println(math.ceil(9007199254740993));
io.println(math.round(9007199254740993));
io.println(math.sqrt(9));
io.println(math.pow(2, 3));
io.println(math.sin(0));
io.println(math.pi() > 3f);
io.println(math.e() > 2f);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "5\n1\n3\n10\n3\n4\n4\n9007199254740993\n9007199254740993\n9007199254740993\n3\n8\n0\ntrue\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsPrimitiveObjectMethods(t *testing.T) {
	input := `import io;

string text = "  Geblang  ";
io.println(text.trim());
io.println(text.trim().lower());
io.println(text.trim().upper());
io.println(text.contains("lang"));
io.println(text.startsWith("  G"));
io.println(text.endsWith("  "));
io.println(text.replace(" ", "_", 2));
io.println("a,b,c".split(",").length());
io.println("héllo".indexOf("l"));
io.println("name=%s age=%d hex=%x price=%.2f".format("Ada", 37, 255, 12.5));
io.println(text.toString());

int n = -7;
io.println(n.abs());
io.println(n.isNegative());
io.println(n.toString());
io.println(n.toDecimal().format(2));
io.println(n.toFloat());

decimal d = -2.50;
io.println(d.abs());
io.println(d.isPositive());
io.println(d.format(0));
io.println(d.format(3));
io.println(d.toString(1));
io.println(d.toFloat());

float f = -3.5f;
io.println(f.abs());
io.println(f.isInf());
io.println(f.toDecimal().format(2));

bool ok = true;
io.println(ok.not());
io.println(ok.toString());
io.println("42".toInt() + 1);
io.println("3.25".toDecimal().format(2));
io.println("2.5".toFloat());
io.println("true".toBool());

class KeywordMethods {
    func bool(): string {
        return "bool method";
    }

    func not(): string {
        return "not method";
    }
}

KeywordMethods keywords = KeywordMethods();
io.println(keywords.bool());
io.println(keywords.not());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "Geblang\ngeblang\nGEBLANG\ntrue\ntrue\ntrue\n__Geblang  \n3\n2\nname=Ada age=37 hex=ff price=12.50\n  Geblang  \n7\ntrue\n-7\n-7.00\n-7\n2.5000000000\nfalse\n-3\n-2.500\n-2.5\n-2.5\n3.5\nfalse\n-3.50\nfalse\ntrue\n43\n3.25\n2.5\ntrue\nbool method\nnot method\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorStringFormatRejectsExtraArguments(t *testing.T) {
	input := `let decimal y = 5;
(y as string).format("%.2f");
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	_, err := evaluator.New(&bytes.Buffer{}).Eval(program)
	if err == nil || !strings.Contains(err.Error(), "invalid string.format arguments") {
		t.Fatalf("eval error: got %v", err)
	}
}

func TestEvaluatorRunsBytesModuleAndBinaryIO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	input := `import bytes;
import compress;
import io;
import json;

let data = bytes.fromString("hello", "utf-8");
io.println(data.length());
io.println(data.get(1));
io.println(data[1]);
io.println(data[1..<4].toString());
io.println(data.toHex());
io.println(bytes.toString(bytes.fromHex("776f726c64")));
io.println(bytes.toBase64(data));
io.println(bytes.fromBase64("aGk=").toString());
let gz = compress.gzip(data);
io.println(gz.length() > data.length());
io.println(compress.gunzip(gz).toString());

let joined = bytes.concat(data, bytes.fromString("!"));
io.writeBytes(` + strconv.Quote(path) + `, joined);
io.appendBytes(` + strconv.Quote(path) + `, bytes.fromHex("0a"));
let read = io.readBytes(` + strconv.Quote(path) + `);
io.println(read.length());
io.println(read.toString());

let handle = io.open(` + strconv.Quote(path) + `, "r");
io.println(io.readBytes(handle, 5).toString());
io.close(handle);
io.println(json.stringify({"blob": bytes.fromString("hi")}));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "5\n101\n101\nell\n68656c6c6f\nworld\naGVsbG8=\nhi\ntrue\nhello\n7\nhello!\n\nhello\n{\"blob\":\"aGk=\"}\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsCollectionsModule(t *testing.T) {
	input := `import bytes;
import collections;
import io;

let nums = [3, 1, 2];
let sorted = collections.sort(nums);
io.println(collections.join(sorted, ","));
io.println(collections.join(collections.reverse(sorted), ":"));
io.println(collections.contains(nums, 1));
io.println(collections.length(nums));
io.println(collections.isEmpty([]));

let dict = {"name": "Geblang"};
io.println(collections.contains(dict, "name"));
io.println(collections.length(dict));

io.println(collections.contains("Geblang", "lang"));
io.println(collections.reverse("abc"));

let raw = bytes.fromHex("010203");
io.println(collections.contains(raw, bytes.fromHex("02")));
io.println(collections.reverse(raw).toHex());

io.println(collections.contains([1.00000000001], 1.00000000002));
io.println(collections.contains([[1.00000000001]], [1.00000000002]));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "1,2,3\n3:2:1\ntrue\n3\ntrue\ntrue\n1\ntrue\ncba\ntrue\n030201\nfalse\nfalse\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsCollectionsHigherOrderHelpers(t *testing.T) {
	input := `import collections;
import io;

let nums = [1, 2, 3, 4, 5];
let doubled = collections.map(nums, func(int x): int { return x * 2; });
io.println(doubled.get(0));
let evens = collections.filter(nums, func(int x): bool { return x % 2 == 0; });
io.println(evens.length());
let total = collections.reduce(nums, func(int acc, int x): int { return acc + x; }, 0);
io.println(total);
let found = collections.find(nums, func(int x): bool { return x > 3; });
io.println(found);
io.println(collections.any(nums, func(int x): bool { return x > 4; }));
io.println(collections.all(nums, func(int x): bool { return x > 0; }));
io.println(collections.flatten([[1, 2], [3, 4]]).length());
io.println(collections.unique([1, 2, 2, 3, 1]).length());
io.println(collections.zip([1, 2], ["a", "b"]).length());
io.println(collections.sorted([3, 1, 2]).get(0));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "2\n2\n15\n4\ntrue\ntrue\n4\n3\n2\n1\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsCollectionsLazyHelpers(t *testing.T) {
	input := `import collections;
import io;

let stream = collections.lazyMap(collections.range(1, 10), func(int x): int {
    return x * 2;
});
let evens = collections.lazyFilter(stream, func(int x): bool {
    return x % 4 == 0;
});
for (n in collections.take(evens, 3)) {
    io.println(n);
}
for (n in collections.range(5, 0, -2)) {
    io.println(n);
}
for (n in collections.take([7, 8, 9], 2)) {
    io.println(n);
}
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "4\n8\n12\n5\n3\n1\n7\n8\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsURLModule(t *testing.T) {
	input := `import io;
import url;

let parts = url.parse("https://example.test:8443/api/v1/items?tag=a&tag=b&q=hello+world#top");
io.println(parts["scheme"]);
io.println(parts["host"]);
io.println(parts["port"]);
io.println(parts["path"]);
io.println(parts["query"]["q"]);
io.println(parts["query"]["tag"].length());
io.println(parts["fragment"]);
io.println(url.encode("a b&c=d"));
io.println(url.decode("a+b%26c%3Dd"));
io.println(url.joinPath("https://example.test/api/", "/v1/", "items"));
io.println(url.stringify({
    "scheme": "https",
    "host": "example.test",
    "path": "/search",
    "query": {"q": "hello world", "tag": ["a", "b"]},
    "fragment": "top"
}));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "https\nexample.test\n8443\n/api/v1/items\nhello world\n2\ntop\na+b%26c%3Dd\na b&c=d\nhttps://example.test/api/v1/items\nhttps://example.test/search?q=hello+world&tag=a&tag=b#top\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsUUIDModule(t *testing.T) {
	input := `import io;
import uuid;

let a = uuid.v4();
let b = uuid.v7();
io.println(a.length());
io.println(a.get(8));
io.println(a.get(13));
io.println(a.get(14));
io.println(a.get(18));
io.println(a.get(23));
io.println(b.length());
io.println(b.get(14));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "36\n-\n-\n4\n-\n-\n36\n7\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorCollectionsSortReturnsComparisonErrors(t *testing.T) {
	input := `import collections;

collections.sort([1, "two", 3]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	_, err := evaluator.New(&bytes.Buffer{}).Eval(program)
	if err == nil {
		t.Fatal("expected comparison error")
	}
	if !strings.Contains(err.Error(), "cannot compare") {
		t.Fatalf("error: got %v", err)
	}
}

func TestEvaluatorUsesCanonicalDictKeys(t *testing.T) {
	input := `import io;

io.println({1.00000000001: "a", 1.00000000002: "b"}.length());

class User {
    string name;

    func User(string name) {
        this.name = name;
    }
}

User first = User("first");
User second = User("second");
let users = {first: "one", second: "two"};
io.println(users.length());
io.println(users[first]);
io.println(users[second]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "2\n2\none\ntwo\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorIteratesDictPairs(t *testing.T) {
	input := `import io;

let data = {"a": 1, "b": 2};
int total = 0;
bool sawA = false;
bool sawB = false;
for (key, value in data) {
    total = total + value;
    if (key == "a") {
        sawA = true;
    }
    if (key == "b") {
        sawB = true;
    }
}
io.println(total);
io.println(sawA);
io.println(sawB);

for (pair in {"x": 7}) {
    io.println(pair[0] + ":" + (pair[1] as string));
}
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "3\ntrue\ntrue\nx:7\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsSecretsModule(t *testing.T) {
	t.Setenv("GEBLANG_TEST_SECRET", "env-secret")
	path := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(path, []byte("file-secret\n"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	input := `import io;
import secrets;

io.println(secrets.getEnv("GEBLANG_TEST_SECRET"));
io.println(secrets.requireEnv("GEBLANG_TEST_SECRET"));
io.println(secrets.getEnv("GEBLANG_MISSING_SECRET") == null);
io.println(secrets.readFile(` + strconv.Quote(path) + `));
io.println(secrets.randomBytes(4).length());
let n = secrets.randomInt(5, 7);
io.println(n >= 5 && n <= 7);
io.println(secrets.randomHex(4).length());
io.println(secrets.randomBase64(6).length() > 0);
io.println(secrets.constantTimeEqual("abc", "abc"));
io.println(secrets.constantTimeEqual("abc", "abd"));
io.println(secrets.constantTimeEqual("abc", "abcd"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "env-secret\nenv-secret\ntrue\nfile-secret\n4\ntrue\n8\ntrue\ntrue\nfalse\nfalse\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsSchemaModule(t *testing.T) {
	input := `import io;
import schema;

let userSchema = {
    "type": "object",
    "required": ["name", "roles"],
    "properties": {
        "name": {"type": "string"},
        "age": {"type": "number"},
        "roles": {"type": "array", "items": {"type": "string"}},
        "status": {"type": "string", "enum": ["active", "disabled"]}
    }
};

let ok = schema.validate({
    "name": "Ada",
    "age": 37,
    "roles": ["admin"],
    "status": "active"
}, userSchema);
io.println(ok["valid"]);
io.println(ok["errors"].length());

let bad = schema.validate({
    "name": 42,
    "roles": ["admin", false],
    "status": "pending"
}, userSchema);
io.println(bad["valid"]);
io.println(bad["errors"].length());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "true\n0\nfalse\n3\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsSerdeModule(t *testing.T) {
	input := `import io;
import serde;

let jsonData = serde.parse("json", "{\"name\":\"Ada\"}");
io.println(jsonData["name"]);

let tomlData = serde.parse("toml", "name = \"Grace\"\n");
io.println(tomlData["name"]);

let yamlData = serde.parse("yaml", "name: Linus\n");
io.println(yamlData["name"]);

io.println(serde.stringify("json", {"ok": true}));
io.println(serde.stringify("yaml", {"ok": true}).contains("ok: true"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "Ada\nGrace\nLinus\n{\"ok\":true}\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsMetricsModule(t *testing.T) {
	input := `import io;
import metrics;

metrics.inc("requests");
metrics.inc("requests", 2);
metrics.set("queue.depth", 4);
io.println(metrics.get("requests"));
io.println(metrics.snapshot()["queue.depth"]);
let start = metrics.now();
io.println(metrics.duration(start) >= 0);
metrics.reset("requests");
io.println(metrics.get("requests"));
metrics.reset();
io.println(metrics.snapshot().length());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "3\n4\ntrue\n0\n0\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsTraceModule(t *testing.T) {
	input := `import io;
import trace;

let span = trace.start("request", {"path": "/health"});
trace.event(span, "db", {"query": "select 1"});
let ended = trace.end(span);
io.println(ended["name"]);
io.println(ended["ended"]);
io.println(ended["attrs"]["path"]);
io.println(ended["events"][0]["name"]);
io.println(ended["durationNanos"] >= 0);
io.println(trace.snapshot().length());
trace.reset();
io.println(trace.snapshot().length());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "request\ntrue\n/health\ndb\ntrue\n1\n0\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsProfileModule(t *testing.T) {
	input := `import io;
import profile;

let before = profile.memStats();
io.println(before["alloc"] >= 0);
io.println(before["goroutines"] >= 1);
profile.gc();
let start = profile.now();
io.println(profile.elapsed(start) >= 0);
let after = profile.memStats();
io.println(after["numGC"] >= before["numGC"]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "true\ntrue\ntrue\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsCLIModuleFormattingAndArgs(t *testing.T) {
	input := `import cli;
import io;

let styled = cli.style("ok", {"fg": "green", "bold": true});
io.println(cli.stripAnsi(styled));
io.println(styled != "ok");

let table = cli.table([
    {"name": "Ada", "role": "admin"},
    {"name": "Linus", "role": "user"}
]);
io.println(table.contains("name"));
io.println(table.contains("Ada"));

/* Options-dict form (matches docs/user/stdlib/13-cli.md): columns
 * picks the dict fields, headers controls labels, separator
 * customises the column gap. */
let opts = cli.table([
    {"name": "Ada", "role": "admin", "active": "yes"},
    {"name": "Bob", "role": "user", "active": "no"}
], {
    "columns": ["name", "role", "active"],
    "headers": ["Name", "Role", "Active"],
    "separator": " | "
});
io.println(opts.contains("Name | Role"));
io.println(opts.contains("Ada"));
io.println(!opts.contains("name | role"));

let parsed = cli.parseArgs(["--verbose", "--count", "3", "file.txt"], {
    "verbose": {"type": "bool", "short": "v"},
    "count": {"type": "int", "default": 1}
});
io.println(parsed["verbose"]);
io.println(parsed["count"]);
io.println(parsed["_"][0]);
io.println(cli.help("tool", {"verbose": {"type": "bool", "short": "v"}}).contains("--verbose"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "ok\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\n3\nfile.txt\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsCLIModuleInteractiveFallback(t *testing.T) {
	previousStdin := os.Stdin
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = read
	defer func() {
		os.Stdin = previousStdin
		_ = read.Close()
	}()
	if _, err := write.WriteString("Alice\n\ny\nsecret-value\n"); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	_ = write.Close()

	input := `import cli;
import io;

io.println(cli.prompt("Name: "));
io.println(cli.prompt("Env: ", "dev"));
io.println(cli.confirm("Continue? ", false));
io.println(cli.password("Password: ").length());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err = evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	want := "Name: Alice\nEnv: dev\nContinue? true\nPassword: \n12\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsWebModule(t *testing.T) {
	input := `import io;
import web;

let app = web.new();

web.use(app, func(dict<string, any> request, dict<string, any> response): dict<string, any> {
    return web.withHeader(response, "X-App", "Geblang");
});

web.before(app, func(dict<string, any> request): any {
    if (request["path"] == "/blocked") {
        return {"status": 401, "body": "blocked"};
    }
    return null;
});

web.get(app, "/users/:id", func(dict<string, any> request): dict<string, any> {
    return {
        "status": 200,
        "body": "user " + request["params"]["id"]
    };
});

web.post(app, "/users", func(dict<string, any> request): string {
    return "created " + request["body"];
});

let found = web.handle(app, {"method": "GET", "path": "/users/42", "body": ""});
io.println(found["status"]);
io.println(found["body"]);
io.println(found["headers"]["X-App"]);

let created = web.handle(app, {"method": "POST", "path": "/users", "body": "Ada"});
io.println(created["body"]);
io.println(created["headers"]["X-App"]);

web.get(app, "/blocked", func(dict<string, any> request): string {
    return "should not run";
});
let blocked = web.handle(app, {"method": "GET", "path": "/blocked", "body": ""});
io.println(blocked["status"]);
io.println(blocked["body"]);
io.println(blocked["headers"]["X-App"]);

let missing = web.handle(app, {"method": "GET", "path": "/missing", "body": ""});
io.println(missing["status"]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "200\nuser 42\nGeblang\ncreated Ada\nGeblang\n401\nblocked\nGeblang\n404\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsWebRouterSessionStores(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local TCP sockets unavailable: %v", err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			req := string(buf[:n])
			switch {
			case strings.Contains(req, "\r\nSET\r\n"):
				_, _ = conn.Write([]byte("+OK\r\n"))
			case strings.Contains(req, "\r\nEXPIRE\r\n"):
				_, _ = conn.Write([]byte(":1\r\n"))
			case strings.Contains(req, "\r\ncache:answer\r\n") && strings.Contains(req, "\r\nGET\r\n"):
				_, _ = conn.Write([]byte("$45\r\n{\"value\":\"RedisCache\",\"expiresAt\":4102444800}\r\n"))
			case strings.Contains(req, "\r\nGET\r\n"):
				_, _ = conn.Write([]byte("$16\r\n{\"user\":\"Redis\"}\r\n"))
			case strings.Contains(req, "\r\nDEL\r\n"):
				_, _ = conn.Write([]byte(":1\r\n"))
			default:
				_, _ = conn.Write([]byte("-ERR unsupported\r\n"))
			}
		}
	}()

	sessionDir := filepath.Join(t.TempDir(), "sessions")
	dbPath := filepath.Join(t.TempDir(), "sessions.sqlite")
	input := `import db;
import io;
import redis;
import web.auth as auth;
import web.cache as cache;
import web.http as http;
import web.router as router;
import web.session as session;

let redisClient = redis.connect(` + strconv.Quote(listener.Addr().String()) + `);
let redisStore = session.redisSessionStore(redisClient, "sess:", 60);
let redisResponse = redisStore.save(http.text("redis"), {"user": "Redis"}, {"httpOnly": true});
let redisRequest = http.request("GET", "/");
redisRequest["headers"] = {"Cookie": redisResponse["headers"]["Set-Cookie"]};
io.println(redisStore.load(redisRequest)["user"]);
redisStore.clear(http.text("clear"), redisRequest);
let redisCache = cache.redisCacheStore(redisClient, "cache:", 60);
redisCache.set("answer", "RedisCache");
io.println(redisCache.get("answer"));
io.println(redisCache.has("answer"));
io.println(redisCache.delete("answer"));
redisClient.close();

let fileStore = session.fileSessionStore(` + strconv.Quote(sessionDir) + `, 60);
let fileResponse = fileStore.save(http.text("file"), {"user": "File"}, {"httpOnly": true});
let fileRequest = http.request("GET", "/");
fileRequest["headers"] = {"Cookie": fileResponse["headers"]["Set-Cookie"]};
io.println(fileStore.load(fileRequest)["user"]);
let fileClear = fileStore.clear(http.text("clear"), fileRequest);
io.println(fileClear["headers"]["Set-Cookie"].contains("Max-Age=0"));
let fileCache = cache.fileCacheStore(` + strconv.Quote(filepath.Join(t.TempDir(), "cache")) + `, 60);
fileCache.set("answer", {"value": 42});
let fileCached = fileCache.get("answer") as dict<string, any>;
io.println(fileCached["value"]);
io.println(fileCache.has("answer"));
io.println(fileCache.delete("answer"));
io.println(!fileCache.has("answer"));
let loginResponse = auth.login(fileStore, http.text("login"), {"name": "Ada", "roles": ["admin"], "permissions": ["users.edit"]}, {"httpOnly": true});
let loginRequest = http.request("GET", "/me");
loginRequest["headers"] = {"Cookie": loginResponse["headers"]["Set-Cookie"]};
let current = auth.currentUser(fileStore, loginRequest);
io.println(current["name"]);
io.println(auth.isAuthenticated(fileStore, loginRequest));
io.println(auth.userHasRole(current, "admin"));
io.println(auth.userHasPermission(current, "users.edit"));
let flashResponse = session.withFlash(fileStore, http.text("flash"), loginRequest, "success", "Profile saved", {"httpOnly": true});
let flashRequest = http.request("GET", "/me");
flashRequest["headers"] = {"Cookie": flashResponse["headers"]["Set-Cookie"]};
let messages = session.flashes(fileStore, flashRequest);
io.println(messages[0]["category"]);
io.println(messages[0]["message"]);
let clearFlashResponse = session.clearFlashes(fileStore, http.text("clear"), flashRequest, {"httpOnly": true});
let clearFlashRequest = http.request("GET", "/me");
clearFlashRequest["headers"] = {"Cookie": clearFlashResponse["headers"]["Set-Cookie"]};
io.println(session.flashes(fileStore, clearFlashRequest).length());
let app = router.newRouter();
router.before(app, auth.requireRole(fileStore, "admin"));
router.get(app, "/admin", func(dict<string, any> request): string {
	return "admin";
});
router.put(app, "/admin", func(dict<string, any> request): dict<string, any> {
	return http.jsonStatus({"updated": true}, 202);
});
router.delete(app, "/admin", func(dict<string, any> request): dict<string, any> {
	return http.noContent();
});
let missingAuth = router.handle(app, http.request("GET", "/admin"));
io.println(missingAuth["status"]);
let allowedRequest = http.request("GET", "/admin");
allowedRequest["headers"] = {"Cookie": loginResponse["headers"]["Set-Cookie"]};
let allowed = router.handle(app, allowedRequest);
io.println(allowed["body"]);
let updateRequest = http.requestWithBody("PUT", "/admin", {"name": "Ada"});
updateRequest["headers"] = {"Cookie": loginResponse["headers"]["Set-Cookie"]};
let updated = router.handle(app, updateRequest);
io.println(updated["status"]);
io.println(updated["headers"]["Content-Type"]);
let deleteRequest = http.request("DELETE", "/admin");
deleteRequest["headers"] = {"Cookie": loginResponse["headers"]["Set-Cookie"]};
let deleted = router.handle(app, deleteRequest);
io.println(deleted["status"]);

let conn = db.open("sqlite", ` + strconv.Quote(dbPath) + `);
let dbStore = session.databaseSessionStore(conn, "geb_sessions", 60).install();
let dbCache = cache.databaseCacheStore(conn, "geb_cache", 60).install();
let dbResponse = dbStore.save(http.text("db"), {"user": "Database"}, {"httpOnly": true});
let dbRequest = http.request("GET", "/");
dbRequest["headers"] = {"Cookie": dbResponse["headers"]["Set-Cookie"]};
io.println(dbStore.load(dbRequest)["user"]);
dbCache.set("answer", {"value": 84});
let dbCached = dbCache.get("answer") as dict<string, any>;
io.println(dbCached["value"]);
io.println(dbCache.has("answer"));
io.println(dbCache.delete("answer"));
io.println(!dbCache.has("answer"));
dbStore.clear(http.text("clear"), dbRequest);
db.close(conn);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err = evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}
	<-done

	want := "Redis\nRedisCache\ntrue\ntrue\nFile\ntrue\n42\ntrue\ntrue\ntrue\nAda\ntrue\ntrue\ntrue\nsuccess\nProfile saved\n0\n401\nadmin\n202\napplication/json\n204\nDatabase\n84\ntrue\ntrue\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsTOMLBuiltins(t *testing.T) {
	input := `import io;
import toml;

let config = toml.parse("title = \"Geblang\"\n[server]\nport = 8080\nenabled = true\nnames = [\"api\", \"admin\"]\n");
io.println(config["title"]);
io.println(config["server"]["port"]);
io.println(config["server"]["enabled"]);
io.println(config["server"]["names"][1]);

let text = toml.stringify({
    "title": "Geblang",
    "server": {
        "port": 8080,
        "enabled": true,
        "names": ["api", "admin"]
    }
});
io.println(text.contains("title = \"Geblang\""));
io.println(text.contains("[server]"));
io.println(text.contains("port = 8080"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "Geblang\n8080\ntrue\nadmin\ntrue\ntrue\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsYAMLBuiltins(t *testing.T) {
	input := `import io;
import yaml;

let config = yaml.parse("title: Geblang\nserver:\n  port: 8080\n  enabled: true\n  names:\n    - api\n    - admin\n");
io.println(config["title"]);
io.println(config["server"]["port"]);
io.println(config["server"]["enabled"]);
io.println(config["server"]["names"][1]);

let text = yaml.stringify({
    "title": "Geblang",
    "server": {
        "port": 8080,
        "enabled": true,
        "names": ["api", "admin"]
    }
});
io.println(text.contains("title: Geblang"));
io.println(text.contains("server:"));
io.println(text.contains("port: 8080"));
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "Geblang\n8080\ntrue\nadmin\ntrue\ntrue\ntrue\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsWebSocketClientBuiltin(t *testing.T) {
	upgrader := gorillawebsocket.Upgrader{}
	server := newLocalHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "yes" {
			t.Errorf("missing X-Test header")
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		for {
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(messageType, data); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	input := `import bytes;
import io;
import websocket;

let conn = websocket.connect("` + wsURL + `", {"X-Test": "yes"});
websocket.sendText(conn, "hello");
io.println(websocket.readText(conn));
websocket.sendBytes(conn, bytes.fromHex("0102ff"));
io.println(websocket.readBytes(conn).toHex());
websocket.close(conn);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "hello\n0102ff\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorBuildsWebSocketUpgradeResponse(t *testing.T) {
	input := `import io;
import websocket;

let response = websocket.upgrade(func(int conn) {
    websocket.sendText(conn, "hello");
});
io.println(response["websocket"]);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "<func>\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRejectsHTTPServeNonFunctionHandler(t *testing.T) {
	input := `import http;
http.serve("127.0.0.1:0", "bad");
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	_, err := evaluator.New(&bytes.Buffer{}).Eval(program)
	if err == nil {
		t.Fatal("expected http.serve handler type error")
	}
}

func TestEvaluatorReportsUncaughtThrow(t *testing.T) {
	input := `throw Error("boom");`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	_, err := evaluator.New(&bytes.Buffer{}).Eval(program)
	if err == nil {
		t.Fatal("expected uncaught throw error")
	}
}

func TestEvaluatorRunsAsyncFunctionAndAwait(t *testing.T) {
	input := `import async;
import io;

async func double(int x): int {
    await async.sleep(1);
    return x * 2;
}

let task = double(21);
io.println(typeof(task));
io.println(task.done as string);
io.println(await task);
io.println(task.done() as string);
io.println(task.await());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "Task\nfalse\n42\ntrue\n42\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorAsyncRunAndAwaitRethrows(t *testing.T) {
	input := `import async;
import io;

let ok = async.run(func(): int {
    return 7;
});
io.println(await ok);

async func fail(): int {
    throw ValueError("bad async");
}

try {
    await fail();
} catch (ValueError err) {
    io.println(err.message);
}
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "7\nbad async\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorAsyncClassMethodReturnsTask(t *testing.T) {
	input := `import io;

class Worker {
    async func value(): int {
        return 9;
    }
}

let worker = Worker();
let task = worker.value();
io.println(typeof(task));
io.println(await task);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	want := "Task\n9\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestEvaluatorRunsGenericClassWithTypeErasure(t *testing.T) {
	input := `import io;

class Box<T> {
    T value;

    func Box(T v) {
        this.value = v;
    }

    func get(): T {
        return this.value;
    }

    func set(T v) {
        this.value = v;
    }
}

Box<string> b = Box("hello");
io.println(b.get());
b.set("world");
io.println(b.get());

Box<int> n = Box(42);
io.println(n.get());
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if got, want := out.String(), "hello\nworld\n42\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorRunsGenericClassWithMultipleTypeParams(t *testing.T) {
	input := `import io;

class Pair<A, B> {
    A first;
    B second;

    func Pair(A a, B b) {
        this.first = a;
        this.second = b;
    }

    func getFirst(): A {
        return this.first;
    }

    func getSecond(): B {
        return this.second;
    }

    func mapFirst<C>(func f): C {
        return f(this.first);
    }

}

Pair<string, int> p = Pair("abc", 3);
io.println(p.getFirst());
io.println(p.getSecond());
io.println(p.mapFirst(func(string s): string { return s + "!"; }));
`

	p2 := parser.New(lexer.New(input))
	program := p2.ParseProgram()
	if len(p2.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p2.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if got, want := out.String(), "abc\n3\nabc!\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorGenericFunctionInstanceofTypeParam(t *testing.T) {
	input := `import io;

func check<T>(T value): bool {
    return value instanceof T;
}

io.println(check("hello"));
io.println(check(42));
io.println(check(true));
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if got, want := out.String(), "true\ntrue\ntrue\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorGenericClassMethodInstanceofTypeParam(t *testing.T) {
	input := `import io;

class Container<T> {
    T item;

    func Container(T v) {
        this.item = v;
    }

    func isT(): bool {
        return this.item instanceof T;
    }
}

Container<string> cs = Container("hello");
io.println(cs.isT());

Container<int> ci = Container(42);
io.println(ci.isT());
`
	p2 := parser.New(lexer.New(input))
	program := p2.ParseProgram()
	if len(p2.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p2.Errors())
	}

	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if got, want := out.String(), "true\ntrue\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorGenericMultiParamClassInstanceofTypeParams(t *testing.T) {
	input := `import io;

class Pair<A, B> {
    A first;
    B second;

    func Pair(A a, B b) {
        this.first = a;
        this.second = b;
    }

    func firstIsA(): bool {
        return this.first instanceof A;
    }

    func secondIsB(): bool {
        return this.second instanceof B;
    }
}

Pair<string, int> p = Pair("abc", 3);
io.println(p.firstIsA());
io.println(p.secondIsB());
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "true\ntrue\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorGenericDeclarationAnnotationWinsOverInference(t *testing.T) {
	input := `import io;

class Box<T> {
    T value;

    func Box(T v) {
        this.value = v;
    }

    func isT(): bool {
        return this.value instanceof T;
    }
}

Box<int> n = Box(42);
io.println(n.isT());

Box<string> s = Box("hello");
io.println(s.isT());

any raw = "not an int";
try {
    Box<int> wrong = Box(raw);
    io.println(wrong.isT());
} catch (RuntimeError e) {
    io.println("caught: " + e.message);
}
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "true\ntrue\ncaught: Box expects T for parameter 'v', got string\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorGenericListParamInfersElementType(t *testing.T) {
	input := `import io;

func firstIs<T>(list<T> xs): bool {
    return xs[0] instanceof T;
}

io.println(firstIs(["hello", "world"]));
io.println(firstIs([1, 2, 3]));
io.println(firstIs([true, false]));
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "true\ntrue\ntrue\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorEnumSimpleVariants(t *testing.T) {
	input := `import io;

enum Color { Red, Green, Blue }

Color c = Color.Red;
io.println(c);
io.println(Color.Green);
io.println(c == Color.Red);
io.println(c == Color.Blue);
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "Color.Red\nColor.Green\ntrue\nfalse\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorEnumTaggedVariants(t *testing.T) {
	input := `import io;

enum Result { Ok(string), Err(string) }

Result r = Result.Ok("hello");
io.println(r);
Result e = Result.Err("oops");
io.println(e);
io.println(r == Result.Ok("hello"));
io.println(r == Result.Err("hello"));
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "Result.Ok(hello)\nResult.Err(oops)\ntrue\nfalse\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorEnumInstanceof(t *testing.T) {
	input := `import io;

enum Color { Red, Green, Blue }
enum Result { Ok(string), Err(string) }

Color c = Color.Green;
Result r = Result.Ok("hi");
Result e = Result.Err("bad");

io.println(c instanceof Color);
io.println(r instanceof Result);
io.println(r instanceof Result.Ok);
io.println(e instanceof Result.Ok);
io.println(e instanceof Result.Err);
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "true\ntrue\ntrue\nfalse\ntrue\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorEnumMatchSimpleVariants(t *testing.T) {
	input := `import io;

enum Direction { North, South, East, West }

func describe(Direction d): string {
    return match (d) {
        case Direction.North => "up";
        case Direction.South => "down";
        case Direction.East => "right";
        default => "left";
    };
}

io.println(describe(Direction.North));
io.println(describe(Direction.East));
io.println(describe(Direction.West));
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "up\nright\nleft\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorEnumMatchTaggedVariants(t *testing.T) {
	input := `import io;

enum Result { Ok(string), Err(string) }

func handle(Result r): string {
    return match (r) {
        case Result.Ok(string msg) => "ok: " + msg;
        case Result.Err(string msg) => "err: " + msg;
    };
}

io.println(handle(Result.Ok("hello")));
io.println(handle(Result.Err("oops")));
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "ok: hello\nerr: oops\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorGenericConstraintAcceptsImplementor(t *testing.T) {
	input := `import io;

interface Printable {
    func print(): string;
}

class Dog implements Printable {
    func print(): string {
        return "woof";
    }
}

func show<T implements Printable>(T item): string {
    return item.print();
}

Dog d = Dog();
io.println(show(d));
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "woof\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorGenericConstraintRejectsNonImplementor(t *testing.T) {
	input := `import io;

interface Printable {
    func print(): string;
}

class Cat {
    func meow(): string {
        return "meow";
    }
}

func show<T implements Printable>(T item): string {
    return item.print();
}

Cat c = Cat();
show(c);
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(prog)
	if err == nil {
		t.Fatal("expected error for type not satisfying constraint, got nil")
	}
	if !strings.Contains(err.Error(), "Cat") || !strings.Contains(err.Error(), "Printable") {
		t.Fatalf("error should mention Cat and Printable, got: %v", err)
	}
}

func TestEvaluatorGenericConstraintRejectsPrimitive(t *testing.T) {
	input := `interface Printable {
    func print(): string;
}

func show<T implements Printable>(T item): void {
}

show("not printable");
`
	p := parser.New(lexer.New(input))
	prog := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(prog)
	if err == nil {
		t.Fatal("expected error for primitive not satisfying constraint, got nil")
	}
	if !strings.Contains(err.Error(), "string") || !strings.Contains(err.Error(), "Printable") {
		t.Fatalf("error should mention string and Printable, got: %v", err)
	}
}

func TestEvaluatorClassGenericConstraintRejectsNonImplementor(t *testing.T) {
	input := `interface Printable {
    func print(): string;
}

class Dog implements Printable {
    func print(): string { return "woof"; }
}

class Cat {
}

class Box<T implements Printable> {
    T value;
    func Box(T value) {
        this.value = value;
    }
}

Dog d = Dog();
Box<Dog> ok = Box(d);
Cat c = Cat();
Box<Cat> bad = Box(c);
`
	p := parser.New(lexer.New(input))
	prog := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(prog)
	if err == nil {
		t.Fatal("expected error for class type parameter constraint, got nil")
	}
	if !strings.Contains(err.Error(), "Cat") || !strings.Contains(err.Error(), "Printable") {
		t.Fatalf("error should mention Cat and Printable, got: %v", err)
	}
}

func TestEvaluatorGenericUnionConstraintAcceptsEither(t *testing.T) {
	input := `import io;

interface Swimmer {
    func swim(): string;
}

interface Runner {
    func run(): string;
}

class Fish implements Swimmer {
    func swim(): string { return "splash"; }
}

class Horse implements Runner {
    func run(): string { return "gallop"; }
}

func move<T implements Swimmer | Runner>(T animal): void {
    io.println("ok");
}

Fish f = Fish();
Horse h = Horse();
move(f);
move(h);
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "ok\nok\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorGenericSetParamInfersElementType(t *testing.T) {
	input := `import io;

func isSetOf<T>(set<T> s, any val): bool {
    return val instanceof T;
}

io.println(isSetOf({"hello", "world"}, "x"));
io.println(isSetOf({1, 2, 3}, 42));
io.println(isSetOf({"hello"}, 99));
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "true\ntrue\nfalse\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorGenericDictParamInfersKeyValueTypes(t *testing.T) {
	input := `import io;

func keyIs<K, V>(dict<K, V> d, any val): bool {
    return val instanceof K;
}

func valIs<K, V>(dict<K, V> d, any val): bool {
    return val instanceof V;
}

io.println(keyIs({"a": 1, "b": 2}, "x"));
io.println(keyIs({"a": 1}, 42));
io.println(valIs({"a": 1, "b": 2}, 99));
io.println(valIs({"a": 1}, "hello"));
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "true\nfalse\ntrue\nfalse\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorGenericCommaConstraints(t *testing.T) {
	input := `import io;

interface Printable {
    func print(): string;
}

interface Saveable {
    func save(): string;
}

class Document implements Printable, Saveable {
    func print(): string { return "printing"; }
    func save(): string { return "saving"; }
}

func process<T implements Printable, Saveable>(T item): void {
    io.println(item.print());
    io.println(item.save());
}

Document d = Document();
process(d);
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "printing\nsaving\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorGenericCommaConstraintsRejectsNonImplementor(t *testing.T) {
	input := `import io;

interface Printable {
    func print(): string;
}

interface Saveable {
    func save(): string;
}

class PrintOnly implements Printable {
    func print(): string { return "printing"; }
}

func process<T implements Printable, Saveable>(T item): void {}

PrintOnly p = PrintOnly();
process(p);
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(prog)
	if err == nil {
		t.Fatal("expected constraint error, got none")
	}
}

func TestEvaluatorGenericInterfaceWithTypeParams(t *testing.T) {
	input := `import io;

interface Container<T> {
    func get(): T;
}

class Box<T> implements Container<T> {
    T value;

    func Box(T v) {
        this.value = v;
    }

    func get(): T {
        return this.value;
    }
}

Box<string> b = Box("hello");
io.println(b.get());
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("eval error: %v", err)
	}
	if got, want := out.String(), "hello\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorStackTraceUncaughtError(t *testing.T) {
	input := `
func inner() {
    throw RuntimeError("boom");
}
func outer() {
    inner();
}
outer();
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	_, err := evaluator.New(&out).Eval(prog)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "uncaught RuntimeError: boom") {
		t.Errorf("missing error class/message in: %q", msg)
	}
	if !strings.Contains(msg, "at inner") {
		t.Errorf("missing inner frame in: %q", msg)
	}
	if !strings.Contains(msg, "at outer") {
		t.Errorf("missing outer frame in: %q", msg)
	}
	if !strings.Contains(msg, "at <top level>") {
		t.Errorf("missing top-level in: %q", msg)
	}
}

func TestEvaluatorStackTraceCaughtErrorHasNoTrace(t *testing.T) {
	input := `import io;
func inner() {
    throw RuntimeError("oops");
}
try {
    inner();
} catch (RuntimeError e) {
    io.println(e.message);
}
`
	prog := parser.New(lexer.New(input)).ParseProgram()
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(prog); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := out.String(), "oops\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorCollectionsBFS(t *testing.T) {
	input := `import io;
import collections;
let g = {"a": ["b", "c"], "b": ["d"], "c": ["d"], "d": []};
io.println(collections.bfs(g, "a"));
io.println(collections.bfs(g, "d"));
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(program); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := out.String(), "[\"a\", \"b\", \"c\", \"d\"]\n[\"d\"]\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorCollectionsDFS(t *testing.T) {
	input := `import io;
import collections;
let g = {"a": ["b", "c"], "b": ["d"], "c": ["d"], "d": []};
io.println(collections.dfs(g, "a"));
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(program); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := out.String(), "[\"a\", \"b\", \"d\", \"c\"]\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorCollectionsTopologicalSort(t *testing.T) {
	input := `import io;
import collections;
let g = {"a": ["b", "c"], "b": ["d"], "c": ["d"], "d": []};
io.println(collections.topologicalSort(g));
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(program); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := out.String(), "[\"a\", \"b\", \"c\", \"d\"]\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestEvaluatorCollectionsTopologicalSortCycleError(t *testing.T) {
	input := `import collections;
let g = {"a": ["b"], "b": ["c"], "c": ["a"]};
collections.topologicalSort(g);
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	_, err := evaluator.New(&bytes.Buffer{}).Eval(program)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("error: got %v", err)
	}
}

func TestEvaluatorCollectionsShortestPath(t *testing.T) {
	input := `import io;
import collections;
let g = {"a": ["b", "c"], "b": ["d"], "c": ["d"], "d": []};
io.println(collections.shortestPath(g, "a", "d"));
io.println(collections.shortestPath(g, "a", "a"));
io.println(collections.shortestPath(g, "d", "a"));
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	var out bytes.Buffer
	if _, err := evaluator.New(&out).Eval(program); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := out.String(), "[\"a\", \"b\", \"d\"]\n[\"a\"]\nnull\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}
