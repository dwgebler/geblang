package bytecode_test

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	"geblang/internal/bytecode"
	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
)

// Generative parity fuzzing (D2). Emits random *valid, type-directed*
// Geblang programs whose every result (and every caught error class) is
// printed, runs each on both the evaluator and the bytecode VM, and
// fails on any stdout or success/failure divergence. This is the
// semantic-drift guard the surface guards (R-series) cannot provide: it
// would have auto-caught this session's catchability / instanceof /
// errors.is divergences.
//
// Determinism (no-flaky rule): a fixed seed runs the same program set
// every time. Override GEBLANG_FUZZ_SEED / GEBLANG_FUZZ_ITERS to hunt
// for new divergences; a failure prints the seed + program to reproduce.

type fuzzType int

const (
	fzInt fuzzType = iota
	fzDecimal
	fzString
	fzBool
)

var fuzzScalarTypes = []fuzzType{fzInt, fzDecimal, fzString, fzBool}

type fuzzGen struct {
	rng *rand.Rand
}

func (g *fuzzGen) intLit() string {
	return strconv.Itoa(g.rng.Intn(199) - 99)
}

func (g *fuzzGen) decimalLit() string {
	return fmt.Sprintf("%d.%02d", g.rng.Intn(40)-20, g.rng.Intn(100))
}

func (g *fuzzGen) stringLit() string {
	words := []string{"", "a", "Ada", "  pad  ", "Hello", "x9", "MiXeD", "z"}
	return strconv.Quote(words[g.rng.Intn(len(words))])
}

// expr returns a valid expression of type t. depth bounds recursion.
func (g *fuzzGen) expr(t fuzzType, depth int) string {
	if depth <= 0 {
		return g.leaf(t)
	}
	switch t {
	case fzInt:
		switch g.rng.Intn(9) {
		case 0:
			return g.leaf(t)
		case 1:
			return "(" + g.expr(fzInt, depth-1) + " + " + g.expr(fzInt, depth-1) + ")"
		case 2:
			return "(" + g.expr(fzInt, depth-1) + " - " + g.expr(fzInt, depth-1) + ")"
		case 3:
			return "(" + g.expr(fzInt, depth-1) + " * " + g.expr(fzInt, depth-1) + ")"
		case 4:
			return "(" + g.expr(fzInt, depth-1) + ").abs()"
		case 5:
			return "(" + g.expr(fzString, depth-1) + ").length()"
		case 6:
			return "(" + g.expr(fzInt, depth-1) + ").sign()"
		case 7: // decimal -> int cast (truncation)
			return "((" + g.expr(fzDecimal, depth-1) + ") as int)"
		default: // ternary
			return "(" + g.expr(fzBool, depth-1) + " ? " + g.expr(fzInt, depth-1) + " : " + g.expr(fzInt, depth-1) + ")"
		}
	case fzDecimal:
		switch g.rng.Intn(6) {
		case 0:
			return g.leaf(t)
		case 1:
			return "(" + g.expr(fzDecimal, depth-1) + " + " + g.expr(fzDecimal, depth-1) + ")"
		case 2:
			return "(" + g.expr(fzDecimal, depth-1) + " * " + g.expr(fzDecimal, depth-1) + ")"
		case 3:
			return "(" + g.expr(fzDecimal, depth-1) + ").round(" + strconv.Itoa(g.rng.Intn(4)) + ")"
		case 4:
			return "(" + g.expr(fzDecimal, depth-1) + ").abs()"
		default: // int -> decimal cast
			return "((" + g.expr(fzInt, depth-1) + ") as decimal)"
		}
	case fzString:
		switch g.rng.Intn(7) {
		case 0:
			return g.leaf(t)
		case 1:
			return "(" + g.expr(fzString, depth-1) + " + " + g.expr(fzString, depth-1) + ")"
		case 2:
			return "(" + g.expr(fzString, depth-1) + ").upper()"
		case 3:
			return "(" + g.expr(fzString, depth-1) + ").trim()"
		case 4:
			return "(" + g.expr(fzInt, depth-1) + ").toString()"
		case 5: // plain f-string interpolation
			return "(\"${" + g.expr(fzInt, depth-1) + "}\")"
		default: // f-string with fixed-point format spec on a decimal
			return "(\"${(" + g.expr(fzDecimal, depth-1) + "):.2f}\")"
		}
	default: // fzBool
		switch g.rng.Intn(7) {
		case 0:
			return g.leaf(t)
		case 1:
			return "(" + g.expr(fzInt, depth-1) + " < " + g.expr(fzInt, depth-1) + ")"
		case 2:
			return "(" + g.expr(fzInt, depth-1) + " == " + g.expr(fzInt, depth-1) + ")"
		case 3:
			return "(" + g.expr(fzBool, depth-1) + " && " + g.expr(fzBool, depth-1) + ")"
		case 4:
			return "(" + g.expr(fzBool, depth-1) + " || " + g.expr(fzBool, depth-1) + ")"
		case 5:
			return "(!" + g.expr(fzBool, depth-1) + ")"
		default:
			return "(" + g.expr(fzInt, depth-1) + ").isEven()"
		}
	}
}

func (g *fuzzGen) leaf(t fuzzType) string {
	switch t {
	case fzInt:
		return g.intLit()
	case fzDecimal:
		return "(" + g.decimalLit() + ")"
	case fzString:
		return g.stringLit()
	default:
		if g.rng.Intn(2) == 0 {
			return "true"
		}
		return "false"
	}
}

// faultStmt emits a try/catch around a fault-prone operation, printing
// "ok" on success or the caught class AND message on failure - error
// class dispatch and message text are both guarded parity surfaces.
func (g *fuzzGen) faultStmt() string {
	// Operands come from `.toInt()` of a string literal so the bytecode
	// compiler cannot constant-fold (and thus reject at compile time) a
	// literal `x / 0`; the fault must occur at runtime on both backends
	// for the comparison to be valid. The VM's compile-time strictness
	// for constant faults is a known, accepted difference.
	rt := func(n int) string { return "(\"" + strconv.Itoa(n) + "\".toInt())" }
	var body string
	switch g.rng.Intn(6) {
	case 0:
		body = "let q = " + g.intLit() + " / " + rt(g.rng.Intn(3)) + ";"
	case 1:
		body = "let r = " + g.intLit() + " % " + rt(g.rng.Intn(3)) + ";"
	case 2:
		body = "let xs = [1, 2, 3]; let v = xs[" + rt(g.rng.Intn(6)) + "];"
	case 3:
		receivers := []string{"[1, 2]", "{1, 2}", "{\"k\": 1}", g.stringLit(), g.intLit()}
		body = "let m = (" + receivers[g.rng.Intn(len(receivers))] + ").bogusMethod();"
	case 4:
		lefts := []string{g.stringLit(), "[1]", "true"}
		ops := []string{"-", "*", "%"}
		body = "let o = " + lefts[g.rng.Intn(len(lefts))] + " " + ops[g.rng.Intn(len(ops))] + " " + rt(1+g.rng.Intn(3)) + ";"
	default:
		body = "let n = " + g.stringLit() + ".toInt();"
	}
	return "try { " + body + " io.println(\"ok\"); } catch (Error e) { io.println(e.class); io.println(e.message); }"
}

// classBlock emits a valid random class hierarchy: a base class with a
// typed int field + two methods (one calling this.other() so override
// dispatch is exercised), one or two subclasses overriding a method and
// calling parent.method()/parent(...), plus a generic Box<int>. Method
// bodies print nothing and return ints/bools/strings the caller prints,
// so output is value-only with no type-formatting ambiguity. Dispatch is
// the recurring eval/VM bug source, so this is the high-value path.
func (g *fuzzGen) classBlock() (decls string, calls []string) {
	base := g.intLit()
	bump := g.intLit()
	var d strings.Builder
	// Base: field, value(), labeled() that calls this.value() (override-dispatched).
	d.WriteString("class Base {\n")
	d.WriteString("  int n;\n")
	d.WriteString("  string tag;\n")
	d.WriteString("  func Base(int n) { this.n = n; this.tag = \"base\"; }\n")
	d.WriteString("  func value(): int { return (this.n + " + base + "); }\n")
	d.WriteString("  func labeled(): int { return (this.value() * 2); }\n")
	d.WriteString("  func name(): string { return this.tag; }\n")
	d.WriteString("}\n")
	// Sub: overrides value() via parent.value(), overrides name().
	d.WriteString("class Sub extends Base {\n")
	d.WriteString("  func Sub(int n) { parent(n); this.tag = \"sub\"; }\n")
	d.WriteString("  func value(): int { return (parent.value() + " + bump + "); }\n")
	d.WriteString("  func name(): string { return (parent.name() + \"!\"); }\n")
	d.WriteString("}\n")
	// Leaf: two levels deep; overrides value() chaining parent.value().
	d.WriteString("class Leaf extends Sub {\n")
	d.WriteString("  func Leaf(int n) { parent(n); }\n")
	d.WriteString("  func value(): int { return (parent.value() - 3); }\n")
	d.WriteString("}\n")
	// Generic box.
	d.WriteString("class Box<T> {\n")
	d.WriteString("  T item;\n")
	d.WriteString("  func Box(T v) { this.item = v; }\n")
	d.WriteString("  func get(): T { return this.item; }\n")
	d.WriteString("  func put(T v): void { this.item = v; }\n")
	d.WriteString("}\n")
	ctors := []string{"Base", "Sub", "Leaf"}
	c := ctors[g.rng.Intn(len(ctors))]
	inst := "let inst = " + c + "(" + g.intLit() + ");"
	calls = append(calls,
		inst,
		"io.println(inst.value());",   // override dispatch
		"io.println(inst.labeled());", // base method calling overridden this.value()
		"io.println(inst.name());",
		"io.println(inst.n);", // field read (inherited)
		"io.println(inst instanceof Base);",
		"let bx = Box("+g.intLit()+"); io.println(bx.get());",
	)
	// Explicit type-arg enforcement + binding-aware instanceof (1.20.0):
	// matching constructions succeed, mismatching constructor and method
	// args throw - class AND message must agree across backends.
	wrap := func(body string) string {
		return "try { " + body + " io.println(\"ok\"); } catch (Error e) { io.println(e.class); io.println(e.message); }"
	}
	if g.rng.Intn(2) == 0 {
		calls = append(calls,
			"let gb = Box<int>("+g.intLit()+"); io.println(gb.get()); io.println(gb instanceof Box<int>); io.println(gb instanceof Box<string>);",
			wrap("let gbad = Box<string>("+g.intLit()+");"),
			wrap("let gm = Box<int>("+g.intLit()+"); gm.put("+g.stringLit()+");"),
		)
	}
	return d.String(), calls
}

// signatureBlock emits a function (and method/static/constructor
// carriers of the same signature) mixing required, defaulted, and
// variadic parameters, plus calls in every binding shape: positional
// subsets, named args for defaulted params, and list spread. Argument
// binding across dispatch contexts is a proven divergence source
// (the default+variadic cluster fixed in 1.17.0).
func (g *fuzzGen) signatureBlock() (decls string, calls []string) {
	// Non-negative: a negative default is a prefix expression, which the
	// bytecode compiler rejects (literal defaults only).
	defB := strconv.Itoa(g.rng.Intn(99))
	hasVariadic := g.rng.Intn(2) == 0
	sig := "int a, int b = " + defB
	body := "\"${a}|${b}\""
	if hasVariadic {
		sig += ", int ...rest"
		body = "\"${a}|${b}|${rest}\""
	}
	var d strings.Builder
	d.WriteString("func sigf(" + sig + "): string { return " + body + "; }\n")
	d.WriteString("class Sig {\n")
	d.WriteString("  string s;\n")
	d.WriteString("  func Sig(" + sig + ") { this.s = " + body + "; }\n")
	d.WriteString("  func m(" + sig + "): string { return " + body + "; }\n")
	d.WriteString("  static func sm(" + sig + "): string { return " + body + "; }\n")
	d.WriteString("}\n")
	d.WriteString("let sigl = func(" + sig + "): string { return " + body + "; };\n")

	argShapes := []string{
		"(" + g.intLit() + ")",
		"(" + g.intLit() + ", " + g.intLit() + ")",
		"(" + g.intLit() + ", b: " + g.intLit() + ")",
		"(a: " + g.intLit() + ", b: " + g.intLit() + ")",
		"(b: " + g.intLit() + ", a: " + g.intLit() + ")",
		"(...{\"a\": " + g.intLit() + ", \"b\": " + g.intLit() + "})",
		"(...{\"a\": " + g.intLit() + "})",
	}
	if hasVariadic {
		argShapes = append(argShapes,
			"("+g.intLit()+", "+g.intLit()+", "+g.intLit()+", "+g.intLit()+")",
			"(...["+g.intLit()+", "+g.intLit()+", "+g.intLit()+"])",
		)
	} else {
		argShapes = append(argShapes, "(...["+g.intLit()+", "+g.intLit()+"])")
	}
	shape := func() string { return argShapes[g.rng.Intn(len(argShapes))] }
	calls = append(calls,
		"io.println(sigf"+shape()+");",
		"io.println(Sig"+shape()+".s);",
		"io.println(Sig(0).m"+shape()+");",
		"io.println(Sig.sm"+shape()+");",
		"io.println(sigl"+shape()+");",
	)
	return d.String(), calls
}

// generatorBlock emits a generator that yields a few values and then
// either finishes or throws a typed error, consumed by a for-in inside
// try/catch that prints every value and any caught class+message.
// Generator fault routing is a proven divergence source (class loss
// fixed in 1.17.0).
func (g *fuzzGen) generatorBlock() (decls string, calls []string) {
	yields := 1 + g.rng.Intn(3)
	var d strings.Builder
	d.WriteString("class GenErr extends ValueError { func GenErr(string m) { parent(m); } }\n")
	d.WriteString("func gen(): generator<int> {\n")
	if g.rng.Intn(2) == 0 {
		for i := 0; i < yields; i++ {
			d.WriteString("  yield " + g.intLit() + ";\n")
		}
	} else {
		// Loop-driven yields: resumption interleaves with loop state.
		d.WriteString("  for (i in 0..<" + strconv.Itoa(yields+1) + ") {\n")
		d.WriteString("    yield (i * " + strconv.Itoa(1+g.rng.Intn(9)) + ");\n")
		d.WriteString("  }\n")
	}
	switch g.rng.Intn(3) {
	case 0:
		// clean finish
	case 1:
		d.WriteString("  throw ValueError(" + g.stringLit() + ");\n")
	default:
		d.WriteString("  throw GenErr(" + g.stringLit() + ");\n")
	}
	d.WriteString("}\n")
	calls = append(calls,
		`try { for (n in gen()) { io.println(n); } io.println("done"); } catch (ValueError e) { io.println(e.class); io.println(e.message); }`,
	)
	return d.String(), calls
}

// loopBlock emits instruction-dense control flow: a for loop with
// break/continue and compound assignment, a while-driven string
// accumulator, and container index mutation. Broad straight-line
// instruction coverage guards representation changes (e.g. operand
// packing) where sparse expression trees would not.
func (g *fuzzGen) loopBlock() (decls string, calls []string) {
	limit := strconv.Itoa(2 + g.rng.Intn(9))
	step := strconv.Itoa(1 + g.rng.Intn(4))
	var d strings.Builder
	d.WriteString("func acc(int n): int {\n")
	d.WriteString("  int total = 0;\n")
	d.WriteString("  for (i in 0..<n) {\n")
	d.WriteString("    if (i % 3 == 1) { continue; }\n")
	d.WriteString("    if (i > " + limit + ") { break; }\n")
	d.WriteString("    total += (i * " + step + ");\n")
	d.WriteString("  }\n")
	d.WriteString("  return total;\n")
	d.WriteString("}\n")
	d.WriteString("func wloop(int n): string {\n")
	d.WriteString("  string out = \"\";\n")
	d.WriteString("  int i = n;\n")
	d.WriteString("  while (i > 0) {\n")
	d.WriteString("    out += (i as string);\n")
	d.WriteString("    i -= 1;\n")
	d.WriteString("  }\n")
	d.WriteString("  return out;\n")
	d.WriteString("}\n")
	calls = append(calls,
		"io.println(acc("+strconv.Itoa(g.rng.Intn(14))+"));",
		"io.println(wloop("+strconv.Itoa(1+g.rng.Intn(5))+"));",
		"let lxs = [3, 1, 2]; lxs[1] = lxs[1] + "+g.intLit()+"; io.println(\"${lxs.sorted()}\");",
		"let ldd = {\"k\": "+g.intLit()+"}; ldd[\"k\"] = (ldd[\"k\"] as int) * 2; io.println(ldd[\"k\"]);",
	)
	return d.String(), calls
}

// collectionsBlock exercises list and dict higher-order and query methods whose
// results print identically on both backends. List HOF dispatch and dict
// ordering/keying are recurring divergence sources (the collections sweep and
// dict-spread fixes landed this way).
func (g *fuzzGen) collectionsBlock() (decls string, calls []string) {
	a, b, c := g.intLit(), g.intLit(), g.intLit()
	calls = append(calls,
		"let xs = ["+a+", "+b+", "+c+"];",
		"io.println(\"${xs.sorted()}\");",
		"io.println(\"${xs.reversed()}\");",
		"io.println(\"${xs.map(func(int n): int { return n + 1; })}\");",
		"io.println(\"${xs.filter(func(int n): bool { return n >= 0; })}\");",
		"io.println(xs.reduce(func(int acc, int n): int { return acc + n; }, 0));",
		"io.println(xs.length());",
		"io.println(xs.contains("+b+"));",
		"io.println(xs.get(0));",
		"let dd = {\"x\": "+a+", \"y\": "+b+"};",
		"io.println(\"${dd.keys().sorted()}\");",
		"io.println(dd.get(\"x\"));",
		"io.println(dd.length());",
		"io.println(dd.contains(\"x\"));",
		"io.println(\"${dd}\");",
	)
	return "", calls
}

// matchBlock exercises match expressions: literal value cases with a default,
// and list-destructuring patterns with guards (a proven divergence source -
// guard ordering and binding must agree across backends).
func (g *fuzzGen) matchBlock() (decls string, calls []string) {
	var d strings.Builder
	d.WriteString("func classify(int n): string {\n")
	d.WriteString("  return match (n) {\n")
	d.WriteString("    case 0 => \"zero\";\n")
	d.WriteString("    case " + strconv.Itoa(1+g.rng.Intn(5)) + " => \"hit\";\n")
	d.WriteString("    default => (n < 0) ? \"neg\" : \"many\";\n")
	d.WriteString("  };\n")
	d.WriteString("}\n")
	d.WriteString("func pairLabel(list<int> p): string {\n")
	d.WriteString("  return match (p) {\n")
	d.WriteString("    case [int x, int y] if (x > y) => \"first\";\n")
	d.WriteString("    case [int x, int y] if (x == y) => \"tie\";\n")
	d.WriteString("    case [int x, int y] => \"second\";\n")
	d.WriteString("    default => \"other\";\n")
	d.WriteString("  };\n")
	d.WriteString("}\n")
	calls = append(calls,
		"io.println(classify("+g.intLit()+"));",
		"io.println(classify(0));",
		"io.println(pairLabel(["+g.intLit()+", "+g.intLit()+"]));",
		"io.println(pairLabel([5, 5]));",
	)
	return d.String(), calls
}

// closureCaptureBlock: a closure whose ONLY reference to an outer local is via interpolation, comprehension, pipe, or partial application (upvalue-capture scan gap).
func (g *fuzzGen) closureCaptureBlock() (decls string, calls []string) {
	base := g.intLit()
	add := g.intLit()
	var d strings.Builder
	d.WriteString("func capDbl(int x): int { return x * 2; }\n")
	d.WriteString("func capAdd(int x, int y): int { return x + y; }\n")
	calls = append(calls,
		"let capBase = "+base+";",
		"let capInterp = func(): string { return \"v=${capBase}\"; };",
		"io.println((capInterp as callable)());",
		"let capList = func(): list<int> { return [capBase + i for i in [0, 1, 2]]; };",
		"io.println(\"${(capList as callable)()}\");",
		"let capDict = func(): dict<int, int> { return {i: capBase + i for i in [0, 1, 2]}; };",
		"io.println(\"${(capDict as callable)()}\");",
		"let capPipe = func(): int { return capBase |> capDbl; };",
		"io.println((capPipe as callable)());",
		"let capPartial = func(): callable { return capAdd(capBase, _); };",
		"io.println(((capPartial as callable)() as callable)("+add+"));",
	)
	return d.String(), calls
}

// spreadArgBlock: a spread mixed with non-spread args in non-trailing position and named-before-spread order (per-argument binding was silently mis-bound).
func (g *fuzzGen) spreadArgBlock() (decls string, calls []string) {
	a, b, c := g.intLit(), g.intLit(), g.intLit()
	x, y := g.intLit(), g.intLit()
	var d strings.Builder
	d.WriteString("func spread3(int a, int b, int c): string { return \"${a}|${b}|${c}\"; }\n")
	d.WriteString("func spreadVar(int a, int ...rest): string { return \"${a}:${rest}\"; }\n")
	calls = append(calls,
		"io.println(spread3(...["+a+"], "+b+", "+c+"));",
		"io.println(spread3("+a+", ...["+b+"], "+c+"));",
		"io.println(spread3(c: "+c+", ...["+a+", "+b+"]));",
		"io.println(spreadVar("+a+", ...["+b+", "+c+"], "+x+"));",
		"io.println(spreadVar(...["+a+", "+b+", "+y+"]));",
	)
	return d.String(), calls
}

// decoratedBlock: a callable decorator over func/method/static, each called succeeding and throwing (both CAUGHT). The uncaught decorated forms live in uncaughtProgram (9.37).
func (g *fuzzGen) decoratedBlock() (decls string, calls []string) {
	ok := strconv.Itoa(g.rng.Intn(50))
	bad := "-" + strconv.Itoa(1+g.rng.Intn(50))
	var d strings.Builder
	d.WriteString("func decoWrap(any next): any { return func(any arg): any { return next(arg); }; }\n")
	d.WriteString("@decoWrap\nfunc decoFn(int x): int { if (x < 0) { throw ValueError(\"fn ${x}\"); } return x + 1; }\n")
	d.WriteString("class DecoSvc {\n")
	d.WriteString("  @decoWrap\n  func handle(int x): int { if (x < 0) { throw ValueError(\"m ${x}\"); } return x + 2; }\n")
	d.WriteString("  @decoWrap\n  static func shandle(int x): int { if (x < 0) { throw ValueError(\"s ${x}\"); } return x + 3; }\n")
	d.WriteString("}\n")
	d.WriteString("func dtry(callable f): void { try { io.println((f as callable)()); io.println(\"ok\"); } catch (Error e) { io.println(e.class); io.println(e.message); } }\n")
	calls = append(calls,
		"dtry(func(): any { return decoFn("+ok+"); });",
		"dtry(func(): any { return decoFn("+bad+"); });",
		"dtry(func(): any { return DecoSvc().handle("+ok+"); });",
		"dtry(func(): any { return DecoSvc().handle("+bad+"); });",
		"dtry(func(): any { return DecoSvc.shandle("+ok+"); });",
		"dtry(func(): any { return DecoSvc.shandle("+bad+"); });",
	)
	return d.String(), calls
}

// methodValueBlock: a static method value and a bound instance method value used as first-class callables (called directly, passed to sortBy), including DECORATED method/static values whose wrapper must run with the receiver forwarded (9.39).
func (g *fuzzGen) methodValueBlock() (decls string, calls []string) {
	base := g.intLit()
	m1, m2 := g.intLit(), g.intLit()
	pos := strconv.Itoa(g.rng.Intn(50))
	bad := "-" + strconv.Itoa(1+g.rng.Intn(50))
	var d strings.Builder
	d.WriteString("func mvWrap(any next): any { return func(int x): int { return (next(x) * 2); }; }\n")
	d.WriteString("class MvCalc {\n")
	d.WriteString("  int base;\n")
	d.WriteString("  func MvCalc(int b) { this.base = b; }\n")
	d.WriteString("  func addTo(int x): int { return (this.base + x); }\n")
	d.WriteString("  static func mul(int a, int b): int { return (a * b); }\n")
	d.WriteString("  @mvWrap\n  func decAdd(int x): int { if (x < 0) { throw ValueError(\"dv ${x}\"); } return (this.base + x); }\n")
	d.WriteString("  @mvWrap\n  static func decMul(int x): int { return (x * 3); }\n")
	d.WriteString("}\n")
	calls = append(calls,
		"let mvS = MvCalc.mul;",
		"io.println((mvS as callable)("+m1+", "+m2+"));",
		"let mvi = MvCalc("+base+");",
		"let mvB = mvi.addTo;",
		"io.println((mvB as callable)("+m1+"));",
		"io.println(\"${[3, 1, 2].sortBy(mvi.addTo)}\");",
		"let mvDb = mvi.decAdd;",
		"io.println((mvDb as callable)("+pos+"));",
		"let mvDs = MvCalc.decMul;",
		"io.println((mvDs as callable)("+pos+"));",
		"io.println(\"${[3, 1, 2].sortBy(mvi.decAdd)}\");",
		"try { io.println((mvDb as callable)("+bad+")); } catch (ValueError e) { io.println(e.message); }",
	)
	return d.String(), calls
}

// deferFormsBlock: defer with a module-qualified callee, a nested-selector callee, an argument spread, and a loop-registered defer (all LIFO).
func (g *fuzzGen) deferFormsBlock() (decls string, calls []string) {
	a, b := g.intLit(), g.intLit()
	var d strings.Builder
	d.WriteString("class DfInner { func ping(): void { io.println(\"ping\"); } }\n")
	d.WriteString("class DfOuter { DfInner inner; func DfOuter() { this.inner = DfInner(); } }\n")
	d.WriteString("func dfShow(int a, int b): void { io.println(\"${a}|${b}\"); }\n")
	d.WriteString("func dfRun(): void {\n")
	d.WriteString("  let o = DfOuter();\n")
	d.WriteString("  let xs = [" + a + ", " + b + "];\n")
	d.WriteString("  defer o.inner.ping();\n")
	d.WriteString("  defer dfShow(...xs);\n")
	d.WriteString("  defer io.println(\"modq\");\n")
	d.WriteString("  for (i in 0..<2) { defer io.println(\"loop${i}\"); }\n")
	d.WriteString("  io.println(\"body\");\n")
	d.WriteString("}\n")
	calls = append(calls, "dfRun();")
	return d.String(), calls
}

func (g *fuzzGen) program() string {
	var b strings.Builder
	b.WriteString("import io;\n")
	// Pick at most one structural section per program so the generated
	// declarations cannot collide; the rest of the budget is scalar
	// expressions and fault statements.
	var sectionCalls []string
	switch g.rng.Intn(12) {
	case 0:
		decls, calls := g.classBlock()
		b.WriteString(decls)
		sectionCalls = calls
	case 1:
		decls, calls := g.signatureBlock()
		b.WriteString(decls)
		sectionCalls = calls
	case 2:
		decls, calls := g.generatorBlock()
		b.WriteString(decls)
		sectionCalls = calls
	case 3:
		decls, calls := g.loopBlock()
		b.WriteString(decls)
		sectionCalls = calls
	case 4:
		decls, calls := g.collectionsBlock()
		b.WriteString(decls)
		sectionCalls = calls
	case 5:
		decls, calls := g.matchBlock()
		b.WriteString(decls)
		sectionCalls = calls
	case 6:
		decls, calls := g.closureCaptureBlock()
		b.WriteString(decls)
		sectionCalls = calls
	case 7:
		decls, calls := g.spreadArgBlock()
		b.WriteString(decls)
		sectionCalls = calls
	case 8:
		decls, calls := g.decoratedBlock()
		b.WriteString(decls)
		sectionCalls = calls
	case 9:
		decls, calls := g.methodValueBlock()
		b.WriteString(decls)
		sectionCalls = calls
	case 10:
		decls, calls := g.deferFormsBlock()
		b.WriteString(decls)
		sectionCalls = calls
	}
	stmts := 3 + g.rng.Intn(5)
	for i := 0; i < stmts; i++ {
		if i < len(sectionCalls) {
			b.WriteString(sectionCalls[i])
		} else if g.rng.Intn(3) == 0 {
			b.WriteString(g.faultStmt())
		} else {
			t := fuzzScalarTypes[g.rng.Intn(len(fuzzScalarTypes))]
			b.WriteString("io.println(" + g.expr(t, 3) + ");")
		}
		b.WriteByte('\n')
	}
	// Ensure all section calls run even if the statement budget was small.
	for i := stmts; i < len(sectionCalls); i++ {
		b.WriteString(sectionCalls[i])
		b.WriteByte('\n')
	}
	return b.String()
}

// uncaughtProgram emits a program whose final statement throws or faults
// with nothing catching it; the rendered uncaught output (header + frame
// lines) is a guarded parity surface since 1.19.0.
func (g *fuzzGen) uncaughtProgram() string {
	var b strings.Builder
	b.WriteString("import io;\n")
	rt := func(n int) string { return "(\"" + strconv.Itoa(n) + "\".toInt())" }
	msg := g.stringLit()
	switch g.rng.Intn(11) {
	case 0: // plain call chain
		b.WriteString("func inner(int x): int { throw ValueError(" + msg + "); }\n")
		b.WriteString("func middle(int x): int { let r = inner(x + 1); return r; }\n")
		b.WriteString("io.println(\"pre\");\n")
		b.WriteString("io.println(middle(" + g.intLit() + "));\n")
	case 1: // method throw
		b.WriteString("class Boomer {\n  func work(int x): int { throw ValueError(" + msg + "); }\n}\n")
		b.WriteString("io.println(Boomer().work(" + g.intLit() + "));\n")
	case 2: // self tail recursion (collapse path)
		depth := 2 + g.rng.Intn(40)
		b.WriteString("func down(int n): int {\n  if (n == 0) { throw ValueError(" + msg + "); }\n  return down(n - 1);\n}\n")
		b.WriteString("io.println(down(" + strconv.Itoa(depth) + "));\n")
	case 3: // deferred throw
		b.WriteString("func explode() { throw ValueError(" + msg + "); }\n")
		b.WriteString("func run() {\n  defer explode();\n  io.println(\"body\");\n}\n")
		b.WriteString("run();\n")
	case 4: // runtime fault, uncaught (runtime operands defeat constant folding)
		if g.rng.Intn(2) == 0 {
			b.WriteString("let xs = [" + g.intLit() + ", " + g.intLit() + "];\n")
			b.WriteString("io.println(xs[" + rt(7+g.rng.Intn(90)) + "]);\n")
		} else {
			b.WriteString("io.println(" + rt(1+g.rng.Intn(99)) + " // " + rt(0) + ");\n")
		}
	case 5: // caught then rethrown, escaping at top level
		b.WriteString("func origin() { throw ValueError(" + msg + "); }\n")
		b.WriteString("func relay() {\n  try { origin(); } catch (ValueError e) { throw e; }\n}\n")
		b.WriteString("relay();\n")
	case 6: // decorated callable throws, uncaught (9.37 wrapper-frame + top-level-line parity)
		b.WriteString("func decoWrap(any next): any { return func(any arg): any { return next(arg); }; }\n")
		switch g.rng.Intn(3) {
		case 0:
			b.WriteString("@decoWrap\nfunc dboom(int x): int { throw ValueError(" + msg + "); }\n")
			b.WriteString("io.println(\"pre\");\n")
			b.WriteString("io.println(dboom(" + g.intLit() + "));\n")
		case 1:
			b.WriteString("class DBoom {\n  @decoWrap\n  func work(int x): int { throw ValueError(" + msg + "); }\n}\n")
			b.WriteString("io.println(\"pre\");\n")
			b.WriteString("io.println(DBoom().work(" + g.intLit() + "));\n")
		default:
			b.WriteString("class DBoom {\n  @decoWrap\n  static func swork(int x): int { throw ValueError(" + msg + "); }\n}\n")
			b.WriteString("io.println(\"pre\");\n")
			b.WriteString("io.println(DBoom.swork(" + g.intLit() + "));\n")
		}
	case 7: // bound instance method value throws, uncaught
		b.WriteString("class MvBoom {\n  func key(int n): int { throw ValueError(" + msg + "); }\n}\n")
		b.WriteString("let mv = MvBoom();\n")
		b.WriteString("let kf = mv.key;\n")
		b.WriteString("io.println(\"pre\");\n")
		b.WriteString("io.println((kf as callable)(" + g.intLit() + "));\n")
	case 8: // decorated bound instance method value throws, uncaught (9.39)
		b.WriteString("func decoWrap(any next): any { return func(int x): int { return next(x); }; }\n")
		b.WriteString("class MvDBoom {\n  @decoWrap\n  func key(int n): int { throw ValueError(" + msg + "); }\n}\n")
		b.WriteString("let mvd = MvDBoom();\n")
		b.WriteString("let dkf = mvd.key;\n")
		b.WriteString("io.println(\"pre\");\n")
		b.WriteString("io.println((dkf as callable)(" + g.intLit() + "));\n")
	case 9: // class-constructor decorator throws, uncaught (9.40)
		b.WriteString("func guard(any cls): any { return func(any... a): any { throw ValueError(" + msg + "); }; }\n")
		b.WriteString("@guard\nclass GBox {\n  int v;\n  func GBox(int v) { this.v = v; }\n}\n")
		b.WriteString("io.println(\"pre\");\n")
		b.WriteString("io.println(GBox(" + g.intLit() + "));\n")
	default: // closure throw
		b.WriteString("let f = func(int x): int { throw ValueError(" + msg + "); };\n")
		b.WriteString("io.println(f(" + g.intLit() + "));\n")
	}
	return b.String()
}

func fuzzRunBoth(source string) (evOut string, evErr error, vmOut string, vmErr error, compileErr error) {
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		return "", nil, "", nil, fmt.Errorf("parse: %s", strings.Join(p.Errors(), "; "))
	}
	var evBuf bytes.Buffer
	_, evErr = evaluator.New(&evBuf).Eval(program)
	chunk, cErr := bytecode.Compile(program, []byte(source), "fuzz")
	if cErr != nil {
		return "", nil, "", nil, fmt.Errorf("compile: %w", cErr)
	}
	var vmBuf bytes.Buffer
	vmErr = bytecode.NewVM(chunk, &vmBuf).Run()
	return evBuf.String(), evErr, vmBuf.String(), vmErr, nil
}

func TestParityFuzz(t *testing.T) {
	seed := int64(1)
	if s := os.Getenv("GEBLANG_FUZZ_SEED"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			seed = v
		}
	}
	iters := 800
	if s := os.Getenv("GEBLANG_FUZZ_ITERS"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			iters = v
		}
	}
	g := &fuzzGen{rng: rand.New(rand.NewSource(seed))}
	for i := 0; i < iters; i++ {
		wantUncaught := i%4 == 3
		var src string
		if wantUncaught {
			src = g.uncaughtProgram()
		} else {
			src = g.program()
		}
		evOut, evErr, vmOut, vmErr, compileErr := fuzzRunBoth(src)
		if compileErr != nil {
			t.Fatalf("generated program did not compile (seed %d, iter %d): %v\n%s", seed, i, compileErr, src)
		}
		if wantUncaught && evErr == nil {
			t.Fatalf("uncaught program did not fail (seed %d, iter %d)\n%s", seed, i, src)
		}
		if (evErr == nil) != (vmErr == nil) {
			t.Fatalf("PARITY: success/failure divergence (seed %d, iter %d)\nprogram:\n%s\neval err: %v\nvm err: %v\neval out: %q\nvm out: %q",
				seed, i, src, evErr, vmErr, evOut, vmOut)
		}
		if evErr != nil && vmErr != nil && evErr.Error() != vmErr.Error() {
			t.Fatalf("PARITY: uncaught-render divergence (seed %d, iter %d)\nprogram:\n%s\n--- eval ---\n%s\n--- vm ---\n%s",
				seed, i, src, evErr.Error(), vmErr.Error())
		}
		if evOut != vmOut {
			t.Fatalf("PARITY: stdout divergence (seed %d, iter %d)\nprogram:\n%s\neval: %q\nvm:   %q",
				seed, i, src, evOut, vmOut)
		}
	}
}
