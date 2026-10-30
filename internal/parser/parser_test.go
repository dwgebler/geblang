package parser_test

import (
	"strings"
	"testing"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

func TestParserBuildsASTForHelloProgram(t *testing.T) {
	input := `import io;
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

	if len(program.Statements) != 6 {
		t.Fatalf("statement count: got %d, want 6", len(program.Statements))
	}

	firstImport, ok := program.Statements[0].(*ast.ImportStatement)
	if !ok {
		t.Fatalf("statement 0: got %T, want *ast.ImportStatement", program.Statements[0])
	}
	if firstImport.ModuleName() != "io" {
		t.Fatalf("import module: got %q, want io", firstImport.ModuleName())
	}

	callStmt, ok := program.Statements[2].(*ast.ExpressionStatement)
	if !ok {
		t.Fatalf("statement 2: got %T, want *ast.ExpressionStatement", program.Statements[2])
	}
	call, ok := callStmt.Expression.(*ast.CallExpression)
	if !ok {
		t.Fatalf("statement expression: got %T, want *ast.CallExpression", callStmt.Expression)
	}
	selector, ok := call.Callee.(*ast.SelectorExpression)
	if !ok {
		t.Fatalf("callee: got %T, want *ast.SelectorExpression", call.Callee)
	}
	if selector.String() != "io.print" {
		t.Fatalf("selector: got %q, want io.print", selector.String())
	}
	arg, ok := call.Arguments[0].Value.(*ast.StringLiteral)
	if !ok {
		t.Fatalf("arg: got %T, want *ast.StringLiteral", call.Arguments[0])
	}
	if arg.Value != "Hello world\n" {
		t.Fatalf("arg value: got %q", arg.Value)
	}
}

func TestParserDistinguishesSetAndDictLiterals(t *testing.T) {
	input := `let s = {1, 2, 3};
let d = {"a": 1, "b": 2};
let empty = {};
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	if len(program.Statements) != 3 {
		t.Fatalf("statement count: got %d, want 3", len(program.Statements))
	}

	setDecl, ok := program.Statements[0].(*ast.DeclarationStatement)
	if !ok {
		t.Fatalf("statement 0: got %T, want *ast.DeclarationStatement", program.Statements[0])
	}
	setLiteral, ok := setDecl.Value.(*ast.SetLiteral)
	if !ok {
		t.Fatalf("set value: got %T, want *ast.SetLiteral", setDecl.Value)
	}
	if len(setLiteral.Elements) != 3 {
		t.Fatalf("set element count: got %d, want 3", len(setLiteral.Elements))
	}

	dictDecl, ok := program.Statements[1].(*ast.DeclarationStatement)
	if !ok {
		t.Fatalf("statement 1: got %T, want *ast.DeclarationStatement", program.Statements[1])
	}
	dictLiteral, ok := dictDecl.Value.(*ast.DictLiteral)
	if !ok {
		t.Fatalf("dict value: got %T, want *ast.DictLiteral", dictDecl.Value)
	}
	if len(dictLiteral.Entries) != 2 {
		t.Fatalf("dict entry count: got %d, want 2", len(dictLiteral.Entries))
	}

	emptyDecl, ok := program.Statements[2].(*ast.DeclarationStatement)
	if !ok {
		t.Fatalf("statement 2: got %T, want *ast.DeclarationStatement", program.Statements[2])
	}
	emptyDict, ok := emptyDecl.Value.(*ast.DictLiteral)
	if !ok {
		t.Fatalf("empty value: got %T, want *ast.DictLiteral", emptyDecl.Value)
	}
	if len(emptyDict.Entries) != 0 {
		t.Fatalf("empty dict entry count: got %d, want 0", len(emptyDict.Entries))
	}
}

func TestParserBuildsASTForCoreSyntaxMilestone(t *testing.T) {
	input := `module app.main;
import crypto.hash as hash;

const VERSION = "1.0";
let nums = [1, 2, 3];
dict<string, any> data = {"name": "Dave", "age": 42};

func wrap<T>(T value): list<T> {
    defer io.println("done");
    return [value];
}

async func fetch(string url): Response {
    return await http.get(url);
}

interface Printable {
    func print(): void;
}

class User extends Person implements Printable {
    string name;
    static const KIND = "user";

    func User(string name) {
        parent(name);
        this.name = name;
    }

    func print(): void {
        io.println(this.name);
    }
}

if (nums[0] < 10 && true) {
    nums[0] = nums[0] + 1;
} elseif (nums[0] == 10) {
    nums[0]++;
} else {
    --nums[0];
}

while (nums[0] < 20) {
    break;
}

for (int i = 0; i < 10; i++) {
    continue;
}

for (i in 1..10 by 2) {
    io.println(i);
}

try {
    throw Error("bad");
} catch (Error e) {
    io.println(e.message);
} catch {
    io.println("unknown");
} finally {
    io.println("cleanup");
}

let label = match (data["age"]) {
    case int n if (n > 0) => "positive";
    case null => "missing";
    default => "other";
};
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	if len(program.Statements) == 0 {
		t.Fatal("expected statements")
	}
	if _, ok := program.Statements[0].(*ast.ModuleStatement); !ok {
		t.Fatalf("statement 0: got %T, want *ast.ModuleStatement", program.Statements[0])
	}

	var sawClass, sawInterface, sawTry, sawFor, sawIf bool
	for _, stmt := range program.Statements {
		switch stmt.(type) {
		case *ast.ClassStatement:
			sawClass = true
		case *ast.InterfaceStatement:
			sawInterface = true
		case *ast.TryStatement:
			sawTry = true
		case *ast.ForStatement:
			sawFor = true
		case *ast.IfStatement:
			sawIf = true
		}
	}
	if !sawClass || !sawInterface || !sawTry || !sawFor || !sawIf {
		t.Fatalf("missing expected top-level nodes: class=%v interface=%v try=%v for=%v if=%v", sawClass, sawInterface, sawTry, sawFor, sawIf)
	}
}

func TestParserParsesCStyleForLoop(t *testing.T) {
	input := `for (int i = 0; i < 10; i++) { continue; }`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	if len(program.Statements) != 1 {
		t.Fatalf("statement count: got %d, want 1", len(program.Statements))
	}
	if _, ok := program.Statements[0].(*ast.ForStatement); !ok {
		t.Fatalf("statement: got %T, want *ast.ForStatement", program.Statements[0])
	}
}

func TestParserParsesDecorators(t *testing.T) {
	input := `@route("GET", "/users")
@transactional
func listUsers(): void {
}

@service(name: "users")
class UserService {
}
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	if len(program.Statements) != 2 {
		t.Fatalf("statements: got %d", len(program.Statements))
	}
	fn, ok := program.Statements[0].(*ast.FunctionStatement)
	if !ok {
		t.Fatalf("first statement: got %T", program.Statements[0])
	}
	if len(fn.Decorators) != 2 {
		t.Fatalf("function decorators: got %d", len(fn.Decorators))
	}
	if fn.Decorators[0].Name.Value != "route" || len(fn.Decorators[0].Arguments) != 2 {
		t.Fatalf("route decorator mismatch: %#v", fn.Decorators[0])
	}
	class, ok := program.Statements[1].(*ast.ClassStatement)
	if !ok {
		t.Fatalf("second statement: got %T", program.Statements[1])
	}
	if len(class.Decorators) != 1 || class.Decorators[0].Name.Value != "service" {
		t.Fatalf("class decorator mismatch: %#v", class.Decorators)
	}
	if class.Decorators[0].Arguments[0].Name.Value != "name" {
		t.Fatalf("decorator named argument not parsed")
	}
}

func TestParserAllowsAsyncAsModuleIdentifier(t *testing.T) {
	input := `import async;
async.sleep(1);
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	if len(program.Statements) != 2 {
		t.Fatalf("statement count: got %d, want 2", len(program.Statements))
	}
	imp, ok := program.Statements[0].(*ast.ImportStatement)
	if !ok {
		t.Fatalf("statement 0: got %T, want *ast.ImportStatement", program.Statements[0])
	}
	if imp.ModuleName() != "async" {
		t.Fatalf("import module: got %q, want async", imp.ModuleName())
	}
	callStmt, ok := program.Statements[1].(*ast.ExpressionStatement)
	if !ok {
		t.Fatalf("statement 1: got %T, want *ast.ExpressionStatement", program.Statements[1])
	}
	call, ok := callStmt.Expression.(*ast.CallExpression)
	if !ok {
		t.Fatalf("statement expression: got %T, want *ast.CallExpression", callStmt.Expression)
	}
	selector, ok := call.Callee.(*ast.SelectorExpression)
	if !ok {
		t.Fatalf("callee: got %T, want *ast.SelectorExpression", call.Callee)
	}
	if selector.String() != "async.sleep" {
		t.Fatalf("selector: got %q, want async.sleep", selector.String())
	}
}

func TestParserNestedGenericsAdjacentClosingBrackets(t *testing.T) {
	// >> must parse as two closing generic brackets, not as the right-shift operator.
	inputs := []string{
		`func f(list<dict<string, int>> rows): void {}`,
		`func f(list<list<int>> rows): void {}`,
		`func f(dict<string, list<int>> m): void {}`,
	}
	for _, input := range inputs {
		p := parser.New(lexer.New(input))
		program := p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Errorf("input %q: parser errors: %v", input, p.Errors())
		}
		if len(program.Statements) != 1 {
			t.Errorf("input %q: got %d statements, want 1", input, len(program.Statements))
		}
	}
}

func TestParserParsesDestructor(t *testing.T) {
	input := `class Foo {
    int x;
    func Foo(int x) { this.x = x; }
    func ~Foo() { this.x = -1; }
}
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	class, ok := program.Statements[0].(*ast.ClassStatement)
	if !ok {
		t.Fatalf("statement 0: got %T, want *ast.ClassStatement", program.Statements[0])
	}
	if class.Destructor == nil {
		t.Fatalf("destructor was not parsed onto ClassStatement.Destructor")
	}
	if class.Destructor.Name == nil || class.Destructor.Name.Value != "Foo" {
		t.Fatalf("destructor name: got %v, want Foo", class.Destructor.Name)
	}
	if len(class.Destructor.Parameters) != 0 {
		t.Fatalf("destructor parameters: got %d, want 0", len(class.Destructor.Parameters))
	}
}

func TestParserDestructorNameMustMatch(t *testing.T) {
	input := `class Foo {
    func Foo() {}
    func ~Bar() {}
}
`
	p := parser.New(lexer.New(input))
	p.ParseProgram()
	errs := p.Errors()
	if len(errs) == 0 {
		t.Fatal("expected a parser error for mismatched destructor name")
	}
}

func TestParserDestructorRejectsParameters(t *testing.T) {
	input := `class Foo {
    func Foo() {}
    func ~Foo(int x) {}
}
`
	p := parser.New(lexer.New(input))
	p.ParseProgram()
	errs := p.Errors()
	if len(errs) == 0 {
		t.Fatal("expected a parser error for destructor with parameters")
	}
}

func TestParserParsesWithStatement(t *testing.T) {
	cases := []string{
		`with (resource()) { use(); }`,
		`with (r = open("x")) { use(r); }`,
	}
	for _, input := range cases {
		p := parser.New(lexer.New(input))
		program := p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Errorf("input %q: parser errors: %v", input, p.Errors())
		}
		if len(program.Statements) != 1 {
			t.Errorf("input %q: got %d statements, want 1", input, len(program.Statements))
		}
		if _, ok := program.Statements[0].(*ast.WithStatement); !ok {
			t.Errorf("input %q: got %T, want *ast.WithStatement", input, program.Statements[0])
		}
	}
}

func TestParserGenericCallExpression(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		typeArgs    []string
		argCount    int
		expectIdent string
	}{
		{
			name:        "function with explicit type arg",
			src:         `assertIs<string>("hello");`,
			typeArgs:    []string{"string"},
			argCount:    1,
			expectIdent: "assertIs",
		},
		{
			name:        "class instantiation with explicit type arg, no args",
			src:         `Box<int>();`,
			typeArgs:    []string{"int"},
			argCount:    0,
			expectIdent: "Box",
		},
		{
			name:        "class instantiation with nested generic arg",
			src:         `Box<list<int>>();`,
			typeArgs:    []string{"list"},
			argCount:    0,
			expectIdent: "Box",
		},
		{
			name:        "two type args",
			src:         `pair<string, int>(1, 2);`,
			typeArgs:    []string{"string", "int"},
			argCount:    2,
			expectIdent: "pair",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := parser.New(lexer.New(c.src))
			prog := p.ParseProgram()
			if errs := p.Errors(); len(errs) != 0 {
				t.Fatalf("parser errors: %v", errs)
			}
			if len(prog.Statements) != 1 {
				t.Fatalf("statement count: got %d, want 1", len(prog.Statements))
			}
			es, ok := prog.Statements[0].(*ast.ExpressionStatement)
			if !ok {
				t.Fatalf("got %T, want *ast.ExpressionStatement", prog.Statements[0])
			}
			call, ok := es.Expression.(*ast.CallExpression)
			if !ok {
				t.Fatalf("got %T, want *ast.CallExpression", es.Expression)
			}
			if len(call.TypeArguments) != len(c.typeArgs) {
				t.Fatalf("type arg count: got %d, want %d", len(call.TypeArguments), len(c.typeArgs))
			}
			for i, want := range c.typeArgs {
				if call.TypeArguments[i].Name != want {
					t.Errorf("type arg %d: got %q, want %q", i, call.TypeArguments[i].Name, want)
				}
			}
			if len(call.Arguments) != c.argCount {
				t.Fatalf("call arg count: got %d, want %d", len(call.Arguments), c.argCount)
			}
			ident, ok := call.Callee.(*ast.Identifier)
			if !ok {
				t.Fatalf("callee: got %T, want *ast.Identifier", call.Callee)
			}
			if ident.Value != c.expectIdent {
				t.Errorf("callee name: got %q, want %q", ident.Value, c.expectIdent)
			}
		})
	}
}

func TestParserFromImportSingle(t *testing.T) {
	p := parser.New(lexer.New("from crypt import passwordHash;\n"))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}
	stmt, ok := prog.Statements[0].(*ast.FromImportStatement)
	if !ok {
		t.Fatalf("expected FromImportStatement, got %T", prog.Statements[0])
	}
	if len(stmt.Path) != 1 || stmt.Path[0] != "crypt" {
		t.Fatalf("path: got %v want [crypt]", stmt.Path)
	}
	if len(stmt.Names) != 1 || stmt.Names[0].Name.Value != "passwordHash" || stmt.Names[0].Alias != nil {
		t.Fatalf("names: got %+v", stmt.Names)
	}
	if stmt.Names[0].Local() != "passwordHash" {
		t.Fatalf("local: got %q want passwordHash", stmt.Names[0].Local())
	}
}

func TestParserFromImportMultipleWithAlias(t *testing.T) {
	p := parser.New(lexer.New("from crypt import passwordHash, passwordVerify as verify;\n"))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}
	stmt := prog.Statements[0].(*ast.FromImportStatement)
	if len(stmt.Names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(stmt.Names))
	}
	if stmt.Names[0].Name.Value != "passwordHash" || stmt.Names[0].Alias != nil {
		t.Fatalf("first name: %+v", stmt.Names[0])
	}
	if stmt.Names[1].Name.Value != "passwordVerify" || stmt.Names[1].Alias == nil || stmt.Names[1].Alias.Value != "verify" {
		t.Fatalf("second name: %+v", stmt.Names[1])
	}
}

func TestParserFromImportDottedPath(t *testing.T) {
	p := parser.New(lexer.New("from a.b.c import x;\n"))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}
	stmt := prog.Statements[0].(*ast.FromImportStatement)
	if len(stmt.Path) != 3 {
		t.Fatalf("path length: got %d want 3", len(stmt.Path))
	}
}

func TestParserPreservesFromAsIdentifier(t *testing.T) {
	src := `func nextMarker(string source, int from): int { return from + 1; }
`
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parser errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(prog.Statements))
	}
	if _, ok := prog.Statements[0].(*ast.FunctionStatement); !ok {
		t.Fatalf("expected FunctionStatement, got %T", prog.Statements[0])
	}
}

func TestParserLessThanComparisonStillParses(t *testing.T) {
	cases := []string{
		`let a = x < y;`,
		`if (count < 10) { }`,
		`let r = a < b > c;`,
	}
	for _, src := range cases {
		p := parser.New(lexer.New(src))
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) != 0 {
			t.Fatalf("input %q: parser errors: %v", src, errs)
		}
		if len(prog.Statements) == 0 {
			t.Fatalf("input %q: no statements parsed", src)
		}
	}
}

func TestParserParsesStaticAsyncMethodAndRejectsReversedOrder(t *testing.T) {
	p := parser.New(lexer.New("class A {\n    static async func f(): int { return 1; }\n}\n"))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	class := program.Statements[0].(*ast.ClassStatement)
	fn := class.Members[0].(*ast.FunctionStatement)
	if !fn.Static || !fn.Async {
		t.Fatalf("expected a static async method, got static=%v async=%v", fn.Static, fn.Async)
	}
	reversed := parser.New(lexer.New("class A {\n    async static func f(): int { return 1; }\n}\n"))
	reversed.ParseProgram()
	if len(reversed.Errors()) == 0 || !strings.Contains(reversed.Errors()[0], "static async func") {
		t.Fatalf("expected an error suggesting `static async func`, got %v", reversed.Errors())
	}
}

func TestParserParsesAsyncEnumMethods(t *testing.T) {
	p := parser.New(lexer.New("enum Light {\n    Red, Green;\n    async func next(): string { return \"x\"; }\n    func label(): string { return \"y\"; }\n}\n"))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	enum := program.Statements[0].(*ast.EnumStatement)
	if len(enum.Methods) != 2 || !enum.Methods[0].Async || enum.Methods[1].Async {
		t.Fatalf("expected one async and one sync method, got %+v", enum.Methods)
	}
}

func TestParseTypeRefAcceptsQualifiedKeywordModuleNames(t *testing.T) {
	for _, source := range []string{"async.atomic.AtomicInt", "list<async.atomic.AtomicInt>", "module.type.Item"} {
		ref, err := parser.ParseTypeRef(source)
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		if ref.String() != source {
			t.Fatalf("got %q, want %q", ref.String(), source)
		}
	}
}
