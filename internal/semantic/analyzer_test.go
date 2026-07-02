package semantic_test

import (
	"strings"
	"testing"

	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/semantic"
)

func TestAnalyzerRejectsMultipleInitBlocks(t *testing.T) {
	input := `init {}
init {}
`

	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	diagnostics := semantic.New().Analyze(program)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics count: got %d, want 1", len(diagnostics))
	}
	// The duplicate-init diagnostic should be anchored at the second
	// init keyword, not at line 1 col 1.
	if got := diagnostics[0].Line; got != 2 {
		t.Fatalf("diagnostic line: got %d, want 2 (the second init token)", got)
	}
}

// TestAnalyzerAcceptsSingleInitBlock verifies the analyzer emits no
// diagnostic for a well-formed file with one init block, including
// surrounding declarations and top-level code.
func TestAnalyzerAcceptsSingleInitBlock(t *testing.T) {
	input := `int count = 0;

init {
    count = 1;
}
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	diagnostics := semantic.New().Analyze(program)
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %d: %#v", len(diagnostics), diagnostics)
	}
}

// TestAnalyzerRejectsFreeStandingExpressionInModule verifies that a
// module file - one beginning with `module name;` - rejects
// free-standing top-level expression statements, which would
// otherwise execute as a side-effect at import time.
func TestAnalyzerRejectsFreeStandingExpressionInModule(t *testing.T) {
	input := `module example;
import io;
io.println("ran at import time");
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	diagnostics := semantic.New().Analyze(program)
	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %#v", len(diagnostics), diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, "free-standing top-level") {
		t.Fatalf("diagnostic should describe the rule: %q", diagnostics[0].Message)
	}
	if diagnostics[0].Line != 3 {
		t.Fatalf("diagnostic should anchor at the offending statement (line 3), got %d", diagnostics[0].Line)
	}
}

// TestAnalyzerAcceptsTopLevelCodeInScripts verifies the new rule does
// not apply to script files - those without a `module` declaration -
// because top-level imperative code is the whole point of a script.
func TestAnalyzerAcceptsTopLevelCodeInScripts(t *testing.T) {
	input := `import io;
io.println("script start");
let x = 1;
io.println(x);
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	diagnostics := semantic.New().Analyze(program)
	if len(diagnostics) != 0 {
		t.Fatalf("script should have no diagnostics, got %d: %#v", len(diagnostics), diagnostics)
	}
}

// TestAnalyzerAcceptsModuleWithDeclarationsAndInit verifies the
// declarative-only contract: a module with declarations, exports,
// classes, functions, an init block, and side-effecting initialisers
// is accepted.
func TestAnalyzerAcceptsModuleWithDeclarationsAndInit(t *testing.T) {
	input := `module app.helpers;
import io;
const PREFIX = "x";
let counter = 0;
export func nextId(): string { return PREFIX; }
export class Tag { string name; func Tag(string n) { this.name = n; } }
init {
    counter = counter + 1;
}
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	diagnostics := semantic.New().Analyze(program)
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %d: %#v", len(diagnostics), diagnostics)
	}
}

// TestAnalyzerDiagnosticsDefaultToErrorSeverity verifies the
// Severity zero-value is SeverityError so existing callers that
// don't explicitly opt into warnings continue to produce errors.
func TestAnalyzerDiagnosticsDefaultToErrorSeverity(t *testing.T) {
	input := `init {}
init {}
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	diagnostics := semantic.New().Analyze(program)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics count: got %d, want 1", len(diagnostics))
	}
	if diagnostics[0].Severity != semantic.SeverityError {
		t.Fatalf("severity: got %d, want SeverityError (%d)", diagnostics[0].Severity, semantic.SeverityError)
	}
}

// TestAnalyzerRejectsTopLevelControlFlowInModule verifies the rule
// catches if/while/for/match at the top level of a module file.
func TestAnalyzerRejectsTopLevelControlFlowInModule(t *testing.T) {
	input := `module example;
import sys;
if (sys.platform() == "linux") {
    sys.exit(0);
}
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	diagnostics := semantic.New().Analyze(program)
	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %#v", len(diagnostics), diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, "if") {
		t.Fatalf("diagnostic should name the offending kind: %q", diagnostics[0].Message)
	}
}

func TestAnalyzerRejectsDeclarationLiteralTypeMismatch(t *testing.T) {
	input := `let int x = "no";
string y = null;
?string z = null;
bool ok = true;
`

	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics count: got %d, want 2: %#v", len(diagnostics), diagnostics)
	}
}

func TestAnalyzerRejectsReturnLiteralTypeMismatch(t *testing.T) {
	input := `func count(): int {
    return "no";
}

func maybe(): ?string {
    return null;
}
`

	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics count: got %d, want 1: %#v", len(diagnostics), diagnostics)
	}
}

func TestAnalyzerTracksDeclaredAndInferredSymbolTypes(t *testing.T) {
	input := `int count = 1;
count = "bad";

let name = "Ada";
name = 2;

func echo(string value): string {
    return value;
}

func badReturn(): string {
    string text = "ok";
    text = null;
    return text;
}
`

	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 3 {
		t.Fatalf("diagnostics count: got %d, want 3: %#v", len(diagnostics), diagnostics)
	}
}

func TestAnalyzerNarrowsNullableTypesInIfBranches(t *testing.T) {
	input := `func valueOrDefault(?string value): string {
    if (value != null) {
        return value;
    }
    return "default";
}

func valueFromElse(?string value): string {
    if (value == null) {
        return "default";
    } else {
        return value;
    }
}

func bad(?string value): string {
    return value;
}
`

	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics count: got %d, want 1: %#v", len(diagnostics), diagnostics)
	}
}

func TestAnalyzerChecksClassAndInterfaceCompatibility(t *testing.T) {
	input := `interface Named {
    func name(): string;
}

interface Entity extends Named {
    func id(): int;
}

class Person implements Entity {
    func name(): string { return "Ada"; }
    func id(): int { return 1; }
}

class Employee extends Person {}
class Other {}

Person p = Employee();
Named n = Person();
Entity e = Employee();
Person bad = Other();
Named badInterface = Other();
`

	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics count: got %d, want 2: %#v", len(diagnostics), diagnostics)
	}
}

func TestAnalyzerTreatsInvokeClassAsCallable(t *testing.T) {
	input := `class Guard {
    func __invoke(string value): bool {
        return true;
    }
}

class NotCallable {}

func makeGuard(): callable {
    return Guard();
}

callable guard = Guard();
callable bad = NotCallable();
`

	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics count: got %d, want 1: %#v", len(diagnostics), diagnostics)
	}
}

func TestAnalyzerValidatesInterfaceMethodImplementations(t *testing.T) {
	input := `interface Named {
    func name(): string;
}

interface Entity extends Named {
    func id(): int;
}

interface Formatter {
    func format(int value): string;
}

class BaseNamed {
    func name(): string { return "base"; }
}

class Good extends BaseNamed implements Entity {
    func id(): int { return 1; }
}

class Missing implements Entity {
    func name(): string { return "missing"; }
}

class WrongReturn implements Named {
    func name(): int { return 1; }
}

class WrongParams implements Named {
    func name(string prefix): string { return prefix; }
}

class WrongParamType implements Formatter {
    func format(bool value): string { return "bad"; }
}
`

	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 4 {
		t.Fatalf("diagnostics count: got %d, want 4: %#v", len(diagnostics), diagnostics)
	}
}

func TestAnalyzerRejectsDuplicateOverloadSignatures(t *testing.T) {
	input := `func parse(string value): int {
    return 1;
}

func Parse(string value): int {
    return 2;
}

func parse(int value): int {
    return value;
}

func parse(string value): string {
    return value;
}

class Runner {
    func run(int value): int { return value; }
    func RUN(int value): int { return value; }
    func run(string value): int { return 1; }
}

interface Worker {
    func work(int value): int;
    func Work(int value): int;
    func work(string value): int;
}
`

	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 3 {
		t.Fatalf("diagnostics count: got %d, want 3: %#v", len(diagnostics), diagnostics)
	}
}

func TestAnalyzerValidatesInterfaceOverloadImplementations(t *testing.T) {
	input := `interface Formatter {
    func format(int value): string;
    func format(string value): string;
}

class Good implements Formatter {
    func format(int value): string { return "int"; }
    func format(string value): string { return value; }
}

class Missing implements Formatter {
    func format(int value): string { return "int"; }
}
`

	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics count: got %d, want 1: %#v", len(diagnostics), diagnostics)
	}
}

func TestAnalyzerChecksKnownTopLevelCallOverloads(t *testing.T) {
	input := `func parse(string value): int {
    return 1;
}

func parse(string value): string {
    return value;
}

func parseReturn(): string {
    return parse("ok");
}

int number = parse("ok");
string text = parse("ok");
bool bad = parse("ok");
any ambiguous = parse("ok");
`

	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics count: got %d, want 2: %#v", len(diagnostics), diagnostics)
	}
}

func TestFlowSensitiveGuardPattern(t *testing.T) {
	// if (x == null) { return; } — after the guard, x is non-null
	input := `func greet(?string name): string {
    if (name == null) { return "anon"; }
    string result = name;
    return result;
}
`
	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics after guard pattern, got: %v", diagnostics)
	}
}

func TestFlowSensitiveGuardPatternThrow(t *testing.T) {
	input := `func greet(?string name): string {
    if (name == null) { throw "missing"; }
    string result = name;
    return result;
}
`
	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics after throw guard, got: %v", diagnostics)
	}
}

func TestFlowSensitiveCompoundAnd(t *testing.T) {
	// if (a != null && b != null) narrows both inside the body
	input := `func process(?string a, ?string b): string {
    if (a != null && b != null) {
        string x = a;
        string y = b;
        return x;
    }
    return "";
}
`
	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics for && narrowing, got: %v", diagnostics)
	}
}

func TestFlowSensitiveWhileCondition(t *testing.T) {
	// while (x != null) narrows x inside the loop body
	input := `func loop(?string x): void {
    while (x != null) {
        string s = x;
    }
}
`
	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics for while narrowing, got: %v", diagnostics)
	}
}

func TestFlowSensitiveInstanceofNarrowing(t *testing.T) {
	input := `class Foo {}
func handle(any val): void {
    if (val instanceof Foo) {
        Foo f = val;
    }
}
`
	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics for instanceof narrowing, got: %v", diagnostics)
	}
}

func TestFlowSensitiveNullableStillErrorsWithoutGuard(t *testing.T) {
	// Without a guard, assigning ?string to string must still error
	input := `func bad(?string name): string {
    string result = name;
    return result;
}
`
	diagnostics := analyzeInput(t, input)
	if len(diagnostics) == 0 {
		t.Fatal("expected diagnostic for nullable assigned to non-nullable without guard")
	}
}

func hasDiagnostic(diags []semantic.Diagnostic, substr string) bool {
	for _, d := range diags {
		if strings.Contains(d.Message, substr) {
			return true
		}
	}
	return false
}

// TestOverrideDecoratorValid accepts a method that genuinely overrides an
// ancestor method (including through an intermediate class).
func TestOverrideDecoratorValid(t *testing.T) {
	input := `class A { func greet(): string { return "a"; } }
class B extends A {}
class C extends B { @override func greet(): string { return "c"; } }
`
	if diags := analyzeInput(t, input); len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got: %v", diags)
	}
}

// TestOverrideDecoratorRejectsNonOverride errors when no ancestor declares the
// method, and when the class has no parent at all.
func TestOverrideDecoratorRejectsNonOverride(t *testing.T) {
	noAncestor := `class A { func greet(): string { return "a"; } }
class B extends A { @override func farewell(): string { return "b"; } }
`
	if diags := analyzeInput(t, noAncestor); !hasDiagnostic(diags, "no ancestor or interface of B declares it") {
		t.Fatalf("expected override diagnostic, got: %v", diags)
	}

	noParent := `class A { @override func greet(): string { return "a"; } }
`
	if diags := analyzeInput(t, noParent); !hasDiagnostic(diags, "A has no parent class or interface") {
		t.Fatalf("expected no-parent override diagnostic, got: %v", diags)
	}
}

// TestOverrideDecoratorAcceptsInterfaceMethod accepts @override on a method that
// implements an interface method, directly or through interface inheritance.
func TestOverrideDecoratorAcceptsInterfaceMethod(t *testing.T) {
	direct := `interface Greeter { func greet(): string; }
class Person implements Greeter { @override func greet(): string { return "hi"; } }
`
	if diags := analyzeInput(t, direct); len(diags) != 0 {
		t.Fatalf("expected no diagnostics for interface override, got: %v", diags)
	}

	inherited := `interface Base { func id(): int; }
interface Both extends Base {}
class Thing implements Both { @override func id(): int { return 1; } }
`
	if diags := analyzeInput(t, inherited); len(diags) != 0 {
		t.Fatalf("expected no diagnostics for inherited-interface override, got: %v", diags)
	}

	// Still errors when the method matches neither a parent nor any interface.
	noMatch := `interface I { func a(): int; }
class C implements I { func a(): int { return 1; } @override func b(): int { return 2; } }
`
	if diags := analyzeInput(t, noMatch); !hasDiagnostic(diags, "no ancestor or interface of C declares it") {
		t.Fatalf("expected override diagnostic, got: %v", diags)
	}
}

// TestOverrideDecoratorSkipsUnresolvableParent does not error when the parent
// class is not visible in the same program (e.g. imported from another module).
func TestOverrideDecoratorSkipsUnresolvableParent(t *testing.T) {
	input := `class B extends Outside { @override func greet(): string { return "b"; } }
`
	if diags := analyzeInput(t, input); hasDiagnostic(diags, "@override") {
		t.Fatalf("expected no override diagnostic for unresolvable parent, got: %v", diags)
	}
}

// TestDeprecatedDecoratorWarnsAtCallSites flags use of a deprecated function,
// class, and method, each as a deprecated-rule warning carrying the message.
func TestDeprecatedDecoratorWarnsAtCallSites(t *testing.T) {
	input := `@deprecated("use newApi")
func oldApi(): int { return 1; }
func newApi(): int { return 2; }
@deprecated
class OldThing { int x; func OldThing(int x) { this.x = x; } }
class Service {
    @deprecated("use fetch")
    func load(): int { return 1; }
}
init {
    let a = oldApi();
    let t = OldThing(1);
    let s = Service();
    let r = s.load();
}
`
	diags := analyzeInput(t, input)
	want := []string{
		"use of deprecated oldApi: use newApi",
		"use of deprecated OldThing",
		"use of deprecated load: use fetch",
	}
	for _, w := range want {
		if !hasDiagnostic(diags, w) {
			t.Fatalf("missing deprecation warning %q in: %v", w, diags)
		}
	}
	for _, d := range diags {
		if strings.HasPrefix(d.Message, "use of deprecated") {
			if d.Severity != semantic.SeverityWarning {
				t.Errorf("deprecation should be a warning, got severity %d", d.Severity)
			}
			if d.Rule != "deprecated" {
				t.Errorf("deprecation rule = %q, want \"deprecated\"", d.Rule)
			}
		}
	}
}

func analyzeInput(t *testing.T, input string) []semantic.Diagnostic {
	t.Helper()
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	return semantic.New().Analyze(program)
}

// TestAnalyzerRejectsUseAfterDel verifies the analyzer flags a
// reference to a binding that has been retired with `del x`.
func TestAnalyzerRejectsUseAfterDel(t *testing.T) {
	input := `class R { func R() {} func ~R() {} }
let r = R();
del r;
let _ = r;
`
	diagnostics := analyzeInput(t, input)
	found := false
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "use of destroyed binding") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected use-of-destroyed-binding diagnostic, got: %v", diagnostics)
	}
}

// TestAnalyzerRejectsUseAfterConditionalDel verifies that
// `del x` inside an if-block still marks x destroyed downstream
// (conservative branch handling).
func TestAnalyzerRejectsUseAfterConditionalDel(t *testing.T) {
	input := `class R { func R() {} func ~R() {} }
let r = R();
if (true) {
    del r;
}
let _ = r;
`
	diagnostics := analyzeInput(t, input)
	found := false
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "use of destroyed binding") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected use-of-destroyed-binding diagnostic for conditional del, got: %v", diagnostics)
	}
}

// TestAnalyzerAllowsRebindingAfterDel verifies that re-declaring
// the same name after `del` clears the destroyed state and the
// new binding is usable.
func TestAnalyzerAllowsRebindingAfterDel(t *testing.T) {
	input := `class R { func R() {} func ~R() {} }
let r = R();
del r;
let r = R();
let _ = r;
`
	diagnostics := analyzeInput(t, input)
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "use of destroyed binding") {
			t.Fatalf("re-bound name should not be flagged: %v", diagnostics)
		}
	}
}

// TestAnalyzerRejectsAssignmentToDestroyedBinding verifies that
// assigning to a name after `del` is also flagged.
func TestAnalyzerRejectsAssignmentToDestroyedBinding(t *testing.T) {
	input := `class R { func R() {} func ~R() {} }
let r = R();
del r;
r = R();
`
	diagnostics := analyzeInput(t, input)
	found := false
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "use of destroyed binding") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected use-of-destroyed-binding diagnostic for assignment, got: %v", diagnostics)
	}
}

// TestAnalyzerRejectsDelOfUnknownIdentifier verifies that
// `del foo` when `foo` was never declared raises a diagnostic.
func TestAnalyzerRejectsDelOfUnknownIdentifier(t *testing.T) {
	input := `del foo;`
	diagnostics := analyzeInput(t, input)
	found := false
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "del: unknown identifier") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected del unknown-identifier diagnostic, got: %v", diagnostics)
	}
}

// TestAnalyzerRejectsInvariantGenericAssignment verifies that the analyzer
// treats user-defined generic classes as INVARIANT in their type parameters.
// Even when Sub extends Base, Container<Sub> is not assignable to
// Container<Base>: that's the standard invariance rule for generics, and it
// prevents the classic unsoundness where a Container<Sub> is widened to
// Container<Base> and then mutated with a sibling Base subtype.
func TestAnalyzerRejectsInvariantGenericAssignment(t *testing.T) {
	input := `class Base {}
class Sub extends Base {}
class Container<T> { func Container() {} }
Container<Base> c = Container<Sub>();
`
	diagnostics := analyzeInput(t, input)
	found := false
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "cannot assign Container<Sub> to Container<Base>") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected invariance diagnostic, got: %v", diagnostics)
	}
}

// TestAnalyzerAcceptsExactGenericMatch verifies invariance does not
// over-reject: Container<Sub> assigned to Container<Sub> is fine.
func TestAnalyzerAcceptsExactGenericMatch(t *testing.T) {
	input := `class Sub {}
class Container<T> { func Container() {} }
Container<Sub> c = Container<Sub>();
`
	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got: %v", diagnostics)
	}
}

func TestAnalyzerRejectsMismatchedFunctionCallTypeArg(t *testing.T) {
	firstMatch := `func firstMatch<T>(list<T> items, callable pred): ?T {
    for (item in items) { if (pred(item)) { return item; } }
    return null;
}
list<int> xs = [1, 2, 3];
`
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"float over list<int>", firstMatch + `let r = firstMatch<float>(xs, func(int n): bool { return n > 1; });`, "cannot pass list<int> as list<float>"},
		{"string over list<int>", firstMatch + `let r = firstMatch<string>(xs, func(int n): bool { return n > 1; });`, "cannot pass list<int> as list<string>"},
		{"bare-T parameter", `func identity<T>(T value): T { return value; }
let r = identity<string>(42);`, "cannot pass int as string"},
		{"too many type arguments", `func identity<T>(T value): T { return value; }
let r = identity<int, string>(42);`, "type parameter"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			diagnostics := analyzeInput(t, c.input)
			found := false
			for _, d := range diagnostics {
				if strings.Contains(d.Message, c.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected diagnostic containing %q, got: %v", c.want, diagnostics)
			}
		})
	}
}

func TestAnalyzerAcceptsValidFunctionCallTypeArg(t *testing.T) {
	firstMatch := `func firstMatch<T>(list<T> items, callable pred): ?T {
    for (item in items) { if (pred(item)) { return item; } }
    return null;
}
list<int> xs = [1, 2, 3];
`
	cases := []struct {
		name  string
		input string
	}{
		{"explicit arg matches element type", firstMatch + `let r = firstMatch<int>(xs, func(int n): bool { return n > 1; });`},
		{"no explicit args, inference unaffected", firstMatch + `let r = firstMatch(xs, func(int n): bool { return n > 1; });`},
		{"bare-T matching explicit arg", `func identity<T>(T value): T { return value; }
let r = identity<string>("ok");`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			diagnostics := analyzeInput(t, c.input)
			if len(diagnostics) != 0 {
				t.Fatalf("expected no diagnostics, got: %v", diagnostics)
			}
		})
	}
}

// TestAnalyzerAcceptsRawGenericAssignment verifies that when the actual value
// carries no explicit type arguments (raw construction with inference), the
// assignment to a parameterised target is allowed at compile time - the
// runtime check enforces invariance once the bindings are reified.
func TestAnalyzerAcceptsRawGenericAssignment(t *testing.T) {
	input := `class Sub {}
class Container<T> { func Container() {} }
Container<Sub> c = Container();
`
	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got: %v", diagnostics)
	}
}

// TestAnalyzerInfersDivisionAsDecimal pins that `/` is true division, so int x = a / b is rejected.
func TestAnalyzerInfersDivisionAsDecimal(t *testing.T) {
	prelude := "let a = 7;\nlet b = 2;\nlet fv = 1.5f;\n"
	rejects := []string{
		"int n = a / b;",
		"int n = (a / b) * a;",
		"int n = a / fv;",
	}
	for _, line := range rejects {
		diags := analyzeInput(t, prelude+line+"\n")
		found := false
		for _, d := range diags {
			if strings.Contains(d.Message, "cannot assign decimal to int") ||
				strings.Contains(d.Message, "cannot assign float to int") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected numeric-narrowing diagnostic for %q, got: %v", line, diags)
		}
	}
	accepts := []string{
		"int n = a + b;",
		"int n = a * b;",
		"int n = a // b;",
		"int n = a % b;",
		"decimal d = a / b;",
		"float f = a / fv;",
	}
	for _, line := range accepts {
		diags := analyzeInput(t, prelude+line+"\n")
		if len(diags) != 0 {
			t.Errorf("expected no diagnostics for %q, got: %v", line, diags)
		}
	}
}

// TestAnalyzerRejectsUnknownLowercaseTypeName guards the "aaa bbb;"
// typo case. Two bare identifiers parse as a typed declaration with
// the first as the type. Lower-case unknown type names error out.
func TestAnalyzerRejectsUnknownLowercaseTypeName(t *testing.T) {
	diagnostics := analyzeInput(t, "aaa bbb;\n")
	if len(diagnostics) == 0 {
		t.Fatalf("expected unknown-type diagnostic, got none")
	}
	found := false
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "unknown type \"aaa\"") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unknown-type diagnostic, got: %v", diagnostics)
	}
}

func TestAnalyzerAcceptsBuiltinTypeNames(t *testing.T) {
	input := `string a = "x";
int b = 1;
decimal c = 1.0;
bool d = true;
bytes e = "x".bytes();
list<int> f = [];
dict<string, int> g = {};
set<int> h = [] as set;
?int i = null;
any j = "x";
`
	diagnostics := analyzeInput(t, input)
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "unknown type") {
			t.Fatalf("unexpected unknown-type diagnostic: %v", d)
		}
	}
}

func TestAnalyzerAcceptsDeclaredClassAsType(t *testing.T) {
	input := `class Foo { func Foo() {} }
Foo f = Foo();
`
	diagnostics := analyzeInput(t, input)
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "unknown type") {
			t.Fatalf("unexpected unknown-type diagnostic: %v", d)
		}
	}
}

// TestAnalyzerAcceptsGenericParamAsType guards against false positives
// on type parameter references inside generic class/function bodies.
// `T` is a single uppercase identifier and the lower-case check skips
// it.
func TestAnalyzerAcceptsGenericParamAsType(t *testing.T) {
	input := `func first<T>(list<T> xs): T {
    return xs.get(0);
}
`
	diagnostics := analyzeInput(t, input)
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "unknown type") {
			t.Fatalf("unexpected unknown-type diagnostic on generic param: %v", d)
		}
	}
}

// TestAnalyzerDeclareInjectsSessionBinding verifies that
// `analyzer.Declare(name, typeName)` makes a binding visible to
// subsequent analysis - this is the REPL session-rebind hook.
func TestAnalyzerDeclareInjectsSessionBinding(t *testing.T) {
	p := parser.New(lexer.New(`del t;`))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	a := semantic.New()
	a.Declare("t", "list")
	diagnostics := a.Analyze(program)
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "unknown identifier") {
			t.Fatalf("expected pre-declared binding to be visible, got: %v", d)
		}
	}
}

// TestAnalyzerAcceptsCovariantCollectionArgument verifies the analyzer treats
// built-in collections as COVARIANT in their element types, matching the
// runtime: list<Dog> is accepted where list<Animal> is expected.
func TestAnalyzerAcceptsCovariantCollectionArgument(t *testing.T) {
	input := `class Animal {}
class Dog extends Animal {}
func count(list<Animal> xs): int { return xs.length(); }
let list<Dog> dogs = [Dog(), Dog()];
let int n = count(dogs);
`
	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics for covariant collection arg, got: %v", diagnostics)
	}
}

// TestAnalyzerAcceptsCollectionToAnyElement verifies list<int> is assignable to
// list<any> (element widening to any is allowed, matching the runtime).
func TestAnalyzerAcceptsCollectionToAnyElement(t *testing.T) {
	input := `func count(list<any> xs): int { return xs.length(); }
let list<int> ints = [1, 2, 3];
let int n = count(ints);
`
	diagnostics := analyzeInput(t, input)
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics for list<int> -> list<any>, got: %v", diagnostics)
	}
}

// TestAnalyzerRejectsUnrelatedCollectionElement verifies the analyzer flags an
// element-type mismatch the runtime also rejects (list<int> -> list<string>),
// in a value (declaration) context.
func TestAnalyzerRejectsUnrelatedCollectionElement(t *testing.T) {
	input := `func count(list<string> xs): int { return xs.length(); }
let list<int> ints = [1, 2, 3];
let int n = count(ints);
`
	diagnostics := analyzeInput(t, input)
	found := false
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "no matching overload for count") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected element-mismatch diagnostic, got: %v", diagnostics)
	}
}

// TestAnalyzerRejectsCollectionElementInStatement verifies the element-mismatch
// check also fires for bare statement calls, which the bytecode compiler cannot
// see (it strips collection element args).
func TestAnalyzerRejectsCollectionElementInStatement(t *testing.T) {
	input := `func count(list<string> xs): int { return xs.length(); }
let list<int> ints = [1, 2, 3];
count(ints);
`
	diagnostics := analyzeInput(t, input)
	found := false
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "no matching overload for count: got (list<int>), expected (list<string>)") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected statement-context element-mismatch diagnostic, got: %v", diagnostics)
	}
}

// TestAnalyzerNoFalsePositiveOnScalarStatement verifies the statement-context
// check does NOT duplicate the bytecode compiler's scalar diagnostic: a plain
// scalar mismatch produces no analyzer (semantic) diagnostic here.
func TestAnalyzerNoFalsePositiveOnScalarStatement(t *testing.T) {
	input := `func want(int n): int { return n; }
want("hello");
`
	diagnostics := analyzeInput(t, input)
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "no matching overload for want") {
			t.Fatalf("scalar mismatch should be left to the bytecode compiler, got: %v", diagnostics)
		}
	}
}

// TestAnalyzerKeepsUserGenericInvariantInCalls verifies covariance is limited to
// built-in collections: a user generic Box<Dog> is NOT accepted where Box<Animal>
// is expected (invariance preserved).
func TestAnalyzerKeepsUserGenericInvariantInCalls(t *testing.T) {
	input := `class Animal {}
class Dog extends Animal {}
class Box<T> { func Box() {} }
func take(Box<Animal> b): int { return 0; }
let Box<Dog> b = Box<Dog>();
let int n = take(b);
`
	diagnostics := analyzeInput(t, input)
	found := false
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "no matching overload for take") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected user-generic invariance to reject Box<Dog> -> Box<Animal>, got: %v", diagnostics)
	}
}

func hasUnknownTypeDiag(diags []semantic.Diagnostic) bool {
	for _, d := range diags {
		if strings.Contains(d.Message, "unknown type") {
			return true
		}
	}
	return false
}

// TestUndefinedTypeErrorsByPosition checks that a non-existent bare
// type name is flagged in each annotation position.
func TestUndefinedTypeErrorsByPosition(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"param", `func f(Nope x): void {}`},
		{"return", `func f(): Nope {}`},
		{"field", `class C { Nope x; }`},
		{"method param", `class C { func m(Nope x): void {} }`},
		{"method return", `class C { func m(): Nope {} }`},
		{"var decl", `Nope x = 0;`},
		{"generic arg", `list<Nope> xs = [];`},
		{"nested generic arg", `dict<string, Nope> d = {};`},
		{"nullable", `?Nope x = null;`},
		{"union", `func f(): int | Nope { return 0; }`},
		{"catch", `try {} catch (Nope e) {}`},
		{"cast", `let y = 1 as Nope;`},
		{"interface method", `interface I { func m(Nope x): void; }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := analyzeInput(t, tc.input)
			if !hasUnknownTypeDiag(diags) {
				t.Fatalf("expected unknown-type error for %s, got: %v", tc.name, diags)
			}
		})
	}
}

// TestUndefinedTypeAcceptsValidTypes checks that legitimate type
// references in annotation positions are not flagged.
func TestUndefinedTypeAcceptsValidTypes(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"primitive param", `func f(int x): void {}`},
		{"forward class", `func f(Later x): void {}
class Later {}`},
		{"interface", `interface Shape {}
func f(Shape x): void {}`},
		{"enum forward", `func f(Color c): void {}
enum Color { Red, Green }`},
		{"alias", `type Id = int;
func f(Id x): void {}`},
		{"generic real", `class Real {}
list<Real> xs = [];`},
		{"generic primitive", `list<int> xs = [];`},
		{"nullable primitive", `?int x = null;`},
		{"union real", `class Real {}
func f(): int | Real { return 0; }`},
		{"catch error class", `try {} catch (Error e) {}`},
		{"cast real", `class Real {}
let r = Real();
let y = r as Real;`},
		{"cast primitive", `let y = 1 as int;`},
		{"func type param", `func f<T>(T x): T { return x; }`},
		{"class type param", `class Box<T> { T value; func get(): T { return this.value; } }`},
		{"method type param", `class C { func m<U>(U x): U { return x; } }`},
		{"qualified skipped", `import foo;
func f(foo.Bar x): void {}`},
		{"runtime error class param", `func f(RuntimeError e): void {}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := analyzeInput(t, tc.input)
			if hasUnknownTypeDiag(diags) {
				t.Fatalf("unexpected unknown-type error for %s: %v", tc.name, diags)
			}
		})
	}
}

func TestBackedEnumValidation(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "unsupported backing type",
			src:  `enum Bad: bool { Yes = true; }`,
			want: "backing type must be string or int",
		},
		{
			name: "missing backing value",
			src:  `enum Bad: string { Yes = "yes"; No }`,
			want: "requires a backing value",
		},
		{
			name: "backing value without type",
			src:  `enum Bad { Yes = "yes" }`,
			want: "no backing type",
		},
		{
			name: "wrong literal type",
			src:  `enum Bad: int { Yes = "yes"; }`,
			want: "must be int",
		},
		{
			name: "duplicate value",
			src:  `enum Bad: string { A = "x"; B = "x"; }`,
			want: "already used",
		},
		{
			name: "non literal",
			src:  `let x = "x"; enum Bad: string { A = x; }`,
			want: "must be a string or int literal",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := analyzeInput(t, tc.src)
			for _, diag := range diags {
				if strings.Contains(diag.Message, tc.want) {
					return
				}
			}
			t.Fatalf("expected diagnostic containing %q, got: %v", tc.want, diags)
		})
	}
}

func TestAnalyzerRejectsModuleAsValue(t *testing.T) {
	cases := []string{
		"import math;\nlet x = math;\n",
		"import math;\nfunc f(any v): void {}\nf(math);\n",
		"import math;\nfunc g(): any { return math; }\n",
		"import math;\nlet xs = [math];\n",
		"import math;\nlet d = {\"k\": math};\n",
		"import math as m;\nlet x = m;\n",
		"import reflect;\nimport math;\nlet m = reflect.module(math);\n",
	}
	for _, input := range cases {
		p := parser.New(lexer.New(input))
		program := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors for %q: %v", input, p.Errors())
		}
		diags := semantic.New().Analyze(program)
		found := false
		for _, d := range diags {
			if strings.Contains(d.Message, "is a module, not a value") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected module-as-value diagnostic for %q, got %v", input, diags)
		}
	}
}

func TestAnalyzerAllowsLegalModuleUses(t *testing.T) {
	cases := []string{
		"import math;\nlet x = math.sqrt(16.0);\n",
		"import math as m;\nlet x = m.sqrt(16.0);\n",
		"import sys;\nlet names = dir(sys);\n",
		"import reflect;\nlet m = reflect.module(\"math\");\n",
	}
	for _, input := range cases {
		p := parser.New(lexer.New(input))
		program := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors for %q: %v", input, p.Errors())
		}
		diags := semantic.New().Analyze(program)
		for _, d := range diags {
			if strings.Contains(d.Message, "is a module, not a value") {
				t.Errorf("unexpected module-as-value diagnostic for %q: %q", input, d.Message)
			}
		}
	}
}

func TestAnalyzerAllowsShadowedModuleName(t *testing.T) {
	cases := []string{
		"import math;\nfunc f(): int { let math = 5; return math + 1; }\n",
		"import math;\nfunc g(int math): int { return math + 1; }\n",
	}
	for _, input := range cases {
		p := parser.New(lexer.New(input))
		program := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors for %q: %v", input, p.Errors())
		}
		diags := semantic.New().Analyze(program)
		for _, d := range diags {
			if strings.Contains(d.Message, "is a module, not a value") {
				t.Errorf("unexpected module-as-value diagnostic for %q: %q", input, d.Message)
			}
		}
	}
}

// A builtin type name that also names an imported module is a valid instanceof target in expression position.
func TestInstanceofBuiltinTypeCollidingWithModule(t *testing.T) {
	accepted := []string{
		"import bytes;\nfunc f(any v): bool { return v instanceof bytes; }\n",
		"import string;\nfunc f(any v): bool { return v instanceof string; }\n",
		"import bytes;\nfunc f(any v): bool { let ok = v instanceof bytes; return ok; }\n",
		"import bytes;\nimport io;\nfunc g(bool b): void {}\nfunc f(any v): void { g(v instanceof bytes); }\n",
		"import bytes;\nfunc f(any v): void { if (v instanceof bytes) {} }\n",
	}
	for _, input := range accepted {
		diags := analyzeInput(t, input)
		if hasDiagnostic(diags, "is a module, not a value") {
			t.Errorf("unexpected module-as-value diagnostic for %q: %v", input, diags)
		}
	}

	rejected := []string{
		"import bytes;\nlet x = bytes;\n",
		"import bytes;\nfunc f(any v): void {}\nf(bytes);\n",
		"import bytes;\nlet y = bytes;\nlet z = y;\n",
		"import bytes;\nfunc g(): any { return bytes; }\n",
	}
	for _, input := range rejected {
		diags := analyzeInput(t, input)
		if !hasDiagnostic(diags, "is a module, not a value") {
			t.Errorf("expected module-as-value diagnostic for %q, got %v", input, diags)
		}
	}
}

// TestAnalyzerFlagsUnknownBareCallee: a call to an undeclared bare name must
// be a static error so the evaluator path rejects what the VM compiler
// already rejects (static-analysis contract).
func TestAnalyzerFlagsUnknownBareCallee(t *testing.T) {
	input := `import io;
RangeError("x");
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	diagnostics := semantic.New().Analyze(program)
	found := false
	for _, d := range diagnostics {
		if d.Severity == semantic.SeverityError && strings.Contains(d.Message, "RangeError") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unknown-callee error for RangeError, got %+v", diagnostics)
	}
}

// TestAnalyzerAcceptsKnownBareCallees covers the resolvable callee kinds that
// must NOT be flagged: declared funcs (incl. forward refs), lambdas in vars,
// classes, ambient errors, bare builtins, from-imports, and nested funcs.
func TestAnalyzerAcceptsKnownBareCallees(t *testing.T) {
	input := `import io;
from math import sqrt;
forward();
func forward(): void {}
let lam = func(): int { return 1; };
lam();
class Widget { func Widget() {} }
Widget();
ValueError("boom");
typeof(lam);
sqrt(4.0);
func outer(): void {
    func inner(): void {}
    inner();
}
outer();
`
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	for _, d := range semantic.New().Analyze(program) {
		if d.Severity == semantic.SeverityError {
			t.Fatalf("unexpected analyzer error: %s", d.Message)
		}
	}
}

// A variadic `T ...rest` binds rest as list<T> inside the body: list
// methods are valid, element-type methods on the list are not.
func TestAnalyzerVariadicParamBindsAsList(t *testing.T) {
	good := `func f(int ...rest): int { return rest.length(); }
class C {
    func tag(string base, string ...parts): string { return base + parts.join("-"); }
}
`
	p := parser.New(lexer.New(good))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	for _, d := range semantic.New().Analyze(program) {
		if d.Severity == semantic.SeverityError {
			t.Fatalf("unexpected diagnostic: %s", d.Message)
		}
	}

	bad := `func h(int ...rest): string { return rest.upper(); }
`
	p = parser.New(lexer.New(bad))
	program = p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	analyzer := semantic.New()
	analyzer.EnableMethodChecks()
	diagnostics := analyzer.Analyze(program)
	found := false
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "list has no method upper") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'list has no method upper' diagnostic, got %v", diagnostics)
	}
}

func TestAnalyzerEnforcesForInBinderType(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"unknown type var", "for (var x in [1, 2]) {}\n", "unknown type"},
		{"unknown type name", "for (Bogus x in [1, 2]) {}\n", "unknown type"},
		{"mismatch string over ints", "for (string x in [1, 2]) {}\n", "cannot assign int to string"},
		{"typed match", "for (int x in [1, 2]) {}\n", ""},
		{"bare binder", "for (x in [1, 2]) {}\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := parser.New(lexer.New(tc.src))
			program := p.ParseProgram()
			if len(p.Errors()) != 0 {
				t.Fatalf("parser errors: %v", p.Errors())
			}
			diagnostics := semantic.New().Analyze(program)
			if tc.want == "" {
				if len(diagnostics) != 0 {
					t.Fatalf("expected no diagnostics, got %v", diagnostics)
				}
				return
			}
			found := false
			for _, d := range diagnostics {
				if strings.Contains(d.Message, tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected a diagnostic containing %q, got %v", tc.want, diagnostics)
			}
		})
	}
}
