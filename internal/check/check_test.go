package check

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/ast"
	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/modules"
	"geblang/internal/parser"
)

func TestSourceFlagsUnresolvedImport(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	source := "import nope.does.not.exist;\n"
	opts := Options{Resolver: modules.NewResolver([]string{dir})}
	_, diags := Source(file, source, opts)
	if !hasDiag(diags, "import", "cannot resolve import nope.does.not.exist") {
		t.Fatalf("expected unresolved-import diagnostic, got %+v", diags)
	}
}

func TestSourceAllowsPackageLocalModuleWithBuiltinShortName(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "src")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "geblang.yaml"), []byte("name: app\nsource: src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sourceDir, "i18n.gb")
	source := "module i18n;\nexport func greeting(): string { return \"hello\"; }\n"
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	_, diags := Source(file, source, Options{Resolver: modules.NewResolver([]string{root})})
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Fatalf("package-local i18n rejected: %+v", diags)
		}
	}
	resolver := modules.NewResolver([]string{root})
	qualified, err := resolver.Resolve("app.i18n")
	if err != nil || qualified != file {
		t.Fatalf("qualified package import resolved to %q: %v", qualified, err)
	}
	bare, err := resolver.Resolve("i18n")
	if err != nil || bare == file {
		t.Fatalf("bare stdlib import resolved to %q: %v", bare, err)
	}
}

func TestSourceRejectsStandaloneModuleWithBuiltinName(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "i18n.gb")
	source := "module i18n;\nexport func greeting(): string { return \"hello\"; }\n"

	_, diags := Source(file, source, Options{Resolver: modules.NewResolver([]string{root})})
	if !hasDiag(diags, "module", "shadows a reserved built-in module name") {
		t.Fatalf("standalone i18n was not rejected: %+v", diags)
	}
}

func TestSourceRejectsReservedNamespaceInPackage(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "src")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "geblang.yaml"), []byte("name: app\nsource: src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sourceDir, "i18n.gb")
	source := "module geblang.i18n;\nexport func greeting(): string { return \"hello\"; }\n"

	_, diags := Source(file, source, Options{Resolver: modules.NewResolver([]string{root})})
	if !hasDiag(diags, "module", "shadows a reserved built-in module name") {
		t.Fatalf("reserved namespace was not rejected: %+v", diags)
	}
}

func TestSourceTreatsNativeImportsAsResolved(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	source := "import binary;\nbinary.size(\">I\");\n"
	opts := Options{Resolver: modules.NewResolver([]string{dir})}
	_, diags := Source(file, source, opts)
	for _, d := range diags {
		if d.Rule == "import" {
			t.Fatalf("native import flagged: %+v", d)
		}
	}
}

func TestSourceUnusedImportWarning(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	source := "import io;\nimport bytes;\nbytes.fromString(\"hi\");\n"
	opts := Options{Lint: true, Resolver: modules.NewResolver([]string{dir})}
	_, diags := Source(file, source, opts)
	if !hasDiag(diags, "unused-import", "import io is not used") {
		t.Fatalf("expected unused-import warning for io, got %+v", diags)
	}
	for _, d := range diags {
		if d.Rule == "unused-import" && strings.Contains(d.Message, "bytes") {
			t.Fatalf("bytes import should not be flagged as unused: %+v", d)
		}
	}
}

func TestSourceUnusedImportCountsCastAndInstanceof(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	// http is referenced only via `instanceof http.Response` and `as
	// http.Response` - both must count as usage.
	source := "import http;\n" +
		"func handle(any r): int {\n" +
		"    if (r instanceof http.Response) { return (r as http.Response).status(); }\n" +
		"    return 0;\n" +
		"}\n"
	opts := Options{Lint: true, Resolver: modules.NewResolver([]string{dir})}
	_, diags := Source(file, source, opts)
	for _, d := range diags {
		if d.Rule == "unused-import" && strings.Contains(d.Message, "http") {
			t.Fatalf("http used via cast/instanceof must not be flagged unused: %+v", d)
		}
	}
}

func TestSourceCrossModuleFlagsMissingNativeSymbol(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	source := "import binary;\nbinary.absolutelyNotARealFunction();\n"
	opts := Options{
		Resolver:    modules.NewResolver([]string{dir}),
		CrossModule: true,
		NativeSymbols: map[string]map[string]struct{}{
			"binary": {"pack": {}, "unpack": {}, "size": {}, "unpackNamed": {}},
		},
	}
	_, diags := Source(file, source, opts)
	if !hasDiag(diags, "import", "binary has no exported member absolutelyNotARealFunction") {
		t.Fatalf("expected cross-module symbol diagnostic, got %+v", diags)
	}
}

func TestSourceCrossModuleAllowsKnownNativeSymbol(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	source := "import binary;\nbinary.pack(\">I\", 1);\n"
	opts := Options{
		Resolver:    modules.NewResolver([]string{dir}),
		CrossModule: true,
		NativeSymbols: map[string]map[string]struct{}{
			"binary": {"pack": {}},
		},
	}
	_, diags := Source(file, source, opts)
	for _, d := range diags {
		if d.Rule == "import" && strings.Contains(d.Message, "binary.pack") {
			t.Fatalf("known native symbol should not be flagged: %+v", d)
		}
	}
}

func TestSourceCrossModuleFlagsMissingProjectSymbol(t *testing.T) {
	dir := t.TempDir()
	depPath := filepath.Join(dir, "util.gb")
	depBody := "func helper(): int { return 1; }\n"
	if err := os.WriteFile(depPath, []byte(depBody), 0o644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(dir, "main.gb")
	mainBody := "import util;\nutil.missing();\n"
	opts := Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true}
	_, diags := Source(mainPath, mainBody, opts)
	if !hasDiag(diags, "import", "util has no exported member missing") {
		t.Fatalf("expected missing-export diagnostic, got %+v", diags)
	}
}

func TestSourceCrossModuleAllowsProjectSymbol(t *testing.T) {
	dir := t.TempDir()
	depPath := filepath.Join(dir, "util.gb")
	depBody := "func helper(): int { return 1; }\n"
	if err := os.WriteFile(depPath, []byte(depBody), 0o644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(dir, "main.gb")
	mainBody := "import util;\nutil.helper();\n"
	opts := Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true}
	_, diags := Source(mainPath, mainBody, opts)
	for _, d := range diags {
		if d.Rule == "import" && strings.Contains(d.Message, "util.helper") {
			t.Fatalf("known project symbol flagged: %+v", d)
		}
	}
}

func TestSourceCrossModuleIgnoresShadowedModule(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	// `errors` is imported but shadowed by a local list; `errors.push`
	// is the list method, not a module member.
	source := "import errors;\nlet errors = [1, 2];\nerrors = errors.push(3);\n"
	opts := Options{
		Resolver:      modules.NewResolver([]string{dir}),
		CrossModule:   true,
		NativeSymbols: map[string]map[string]struct{}{"errors": {"raise": {}}},
	}
	_, diags := Source(file, source, opts)
	for _, d := range diags {
		if d.Rule == "import" && strings.Contains(d.Message, "errors") {
			t.Fatalf("shadowed module member should not be flagged: %+v", d)
		}
	}
}

func TestSourceCrossModuleNativeFallsThroughToSource(t *testing.T) {
	dir := t.TempDir()
	// A bundled-source module named like a native (empty native symbols)
	// must resolve its real exports from the .gb file.
	if err := os.WriteFile(filepath.Join(dir, "streams.gb"),
		[]byte("module streams;\nexport func of(any x): int { return 1; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.gb")
	source := "import streams;\nstreams.of(5);\n"
	opts := Options{
		Resolver:      modules.NewResolver([]string{dir}),
		CrossModule:   true,
		NativeSymbols: map[string]map[string]struct{}{"streams": {}},
	}
	_, diags := Source(file, source, opts)
	for _, d := range diags {
		if d.Rule == "import" && strings.Contains(d.Message, "streams.of") {
			t.Fatalf("bundled-source member should resolve, got %+v", d)
		}
	}
}

func TestSourceImportEngineNativeNotFlaggedUnresolved(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	// A module present in NativeSymbols but absent from NativeModuleNames
	// is still recognised as native (the engine-aware fallback).
	source := "import zzengineonly;\n"
	opts := Options{
		Resolver:      modules.NewResolver([]string{dir}),
		CrossModule:   true,
		NativeSymbols: map[string]map[string]struct{}{"zzengineonly": {"go": {}}},
	}
	_, diags := Source(file, source, opts)
	for _, d := range diags {
		if d.Rule == "import" && strings.Contains(d.Message, "cannot resolve") {
			t.Fatalf("engine-native import should not be flagged unresolved: %+v", d)
		}
	}
}

func classCheckOpts(dir string) Options {
	return Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true}
}

func TestSourceFlagsUnknownMethodSameFileClass(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "class Box {\n    func open(): void {}\n}\nBox b = Box();\nb.open();\nb.smash();\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	if !hasDiag(diags, "semantic", "Box has no method smash") {
		t.Fatalf("expected unknown-method diagnostic, got %+v", diags)
	}
	for _, d := range diags {
		if strings.Contains(d.Message, "no method open") {
			t.Fatalf("real method flagged: %+v", d)
		}
	}
}

func TestSourceFlagsUnknownMethodCrossFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shapes.gb"),
		[]byte("module shapes;\nexport class Circle {\n    func area(): float { return 3.14; }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.gb")
	src := "from shapes import Circle;\nCircle c = Circle();\nc.area();\nc.bogus();\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	if !hasDiag(diags, "semantic", "Circle has no method bogus") {
		t.Fatalf("expected cross-file unknown-method diagnostic, got %+v", diags)
	}
	for _, d := range diags {
		if strings.Contains(d.Message, "no method area") {
			t.Fatalf("real cross-file method flagged: %+v", d)
		}
	}
}

func TestSourceAllowsInheritedMethod(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "class Base {\n    func ping(): void {}\n}\nclass Derived extends Base {\n    func pong(): void {}\n}\nDerived d = Derived();\nd.ping();\nd.pong();\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	for _, d := range diags {
		if d.Rule == "semantic" && strings.Contains(d.Message, "no method") {
			t.Fatalf("inherited/own method flagged: %+v", d)
		}
	}
}

func TestSourceBailsOnCallClass(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	// A __call class dispatches dynamically; unknown methods must not flag.
	src := "class Proxy {\n    func __call(string m, list<any> a): any { return null; }\n}\nProxy p = Proxy();\np.anything();\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	for _, d := range diags {
		if d.Rule == "semantic" && strings.Contains(d.Message, "no method") {
			t.Fatalf("__call class should bail, got %+v", d)
		}
	}
}

func TestSourceBailsOnDecoratedClass(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "deco.gb"),
		[]byte("module deco;\nexport func Tag(any c): any { return c; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.gb")
	// Decorators may inject members; the class must bail.
	src := "from deco import Tag;\n@Tag\nclass Svc {\n    func run(): void {}\n}\nSvc s = Svc();\ns.injectedMaybe();\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	for _, d := range diags {
		if d.Rule == "semantic" && strings.Contains(d.Message, "no method") {
			t.Fatalf("decorated class should bail, got %+v", d)
		}
	}
}

func TestSourcePrimitiveMethodTypoErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "\"hello\".upper();\n\"hello\".fooBar();\n[1, 2].push(3);\n[1, 2].smoosh();\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	if !hasDiag(diags, "semantic", "string has no method fooBar") {
		t.Fatalf("expected string method diagnostic, got %+v", diags)
	}
	if !hasDiag(diags, "semantic", "list has no method smoosh") {
		t.Fatalf("expected list method diagnostic, got %+v", diags)
	}
	for _, d := range diags {
		if strings.Contains(d.Message, "has no method") && d.Severity != SeverityError {
			t.Fatalf("primitive method typo should be an error: %+v", d)
		}
		if strings.Contains(d.Message, "no method upper") || strings.Contains(d.Message, "no method push") {
			t.Fatalf("real method flagged: %+v", d)
		}
	}
}

func TestSourceFlagsUndefinedFunction(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	_, diags := Source(file, "notAThing();\n", classCheckOpts(dir))
	found := false
	for _, d := range diags {
		if d.Severity == SeverityError && strings.Contains(d.Message, "notAThing") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected undefined-function error, got %+v", diags)
	}
}

func TestSourceAllowsDefinedAndImportedCalls(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "helper.gb"),
		[]byte("module helper;\nexport func go(): int { return 1; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.gb")
	// forward reference, local function-variable, from-imported name, and
	// a bare builtin must all resolve cleanly.
	src := "from helper import go;\nfunc a(): void { b(); }\nfunc b(): void {}\nlet f = a;\nf();\ngo();\nlet t = typeof(1);\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Fatalf("valid calls should not error: %+v", d)
		}
	}
}

// TestSourceFlagsCollectionElementMismatch verifies check reports a list<int>
// argument passed where list<string> is expected, in a bare statement call -
// the gap the bytecode compiler cannot see because it strips element args.
func TestSourceFlagsCollectionElementMismatch(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "func count(list<string> xs): int { return xs.length(); }\n" +
		"let list<int> ints = [1, 2, 3];\n" +
		"count(ints);\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	found := false
	for _, d := range diags {
		if d.Severity == SeverityError && strings.Contains(d.Message, "no matching overload for count") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected collection element-mismatch error, got %+v", diags)
	}
}

// A scalar overload mismatch surfaces the evaluator-matching detailed message
// rather than the bare "no matching overload".
func TestSourceOverloadMismatchDetailedMessage(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "func g(int x): int { return x; }\ng(\"s\");\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	if !hasDiag(diags, "type", "g expects int for parameter 'x', got string") {
		t.Fatalf("expected detailed overload message, got %+v", diags)
	}
}

// When an unknown parameter type already explains why no overload matches, the
// redundant "no matching overload" error is suppressed.
func TestSourceSuppressesRedundantOverloadAfterUnknownType(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "func f(Nope x): int { return 0; }\nf(1);\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	if !hasDiag(diags, "semantic", "unknown type \"Nope\" in parameter x of function f") {
		t.Fatalf("expected unknown-type diagnostic, got %+v", diags)
	}
	for _, d := range diags {
		if _, ok := overloadErrorFunctionName(d.Message); ok {
			t.Fatalf("overload error should be suppressed, got %+v", d)
		}
	}
}

// TestSourceAllowsCovariantCollectionArgument verifies check does NOT
// false-positive on a covariant collection argument (list<Dog> -> list<Animal>).
func TestSourceAllowsCovariantCollectionArgument(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "class Animal {}\nclass Dog extends Animal {}\n" +
		"func count(list<Animal> xs): int { return xs.length(); }\n" +
		"let list<Dog> dogs = [Dog(), Dog()];\n" +
		"count(dogs);\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Fatalf("covariant collection arg should not error: %+v", d)
		}
	}
}

func TestSourceReturnsParseDiagnostics(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	_, diags := Source(file, "func {", Options{})
	if len(diags) == 0 || diags[0].Rule != "parse" {
		t.Fatalf("expected parse diagnostic, got %+v", diags)
	}
}

func TestSourceFromImportFlagsMissingNativeExport(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	source := "from crypt import passwordHash, notARealFunction;\n"
	opts := Options{
		Resolver:    modules.NewResolver([]string{dir}),
		CrossModule: true,
		NativeSymbols: map[string]map[string]struct{}{
			"crypt": {"passwordHash": {}, "passwordVerify": {}},
		},
	}
	_, diags := Source(file, source, opts)
	if !hasDiag(diags, "import", "crypt has no exported member notARealFunction") {
		t.Fatalf("expected missing-export diagnostic, got %+v", diags)
	}
	for _, d := range diags {
		if d.Rule == "import" && strings.Contains(d.Message, "passwordHash") {
			t.Fatalf("known export passwordHash should not be flagged: %+v", d)
		}
	}
}

func TestSourceFromImportFlagsMissingProjectSymbol(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "util.gb"), []byte("func helper(): int { return 1; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.gb")
	body := "from util import helper, missing;\n"
	opts := Options{Resolver: modules.NewResolver([]string{dir}), CrossModule: true}
	_, diags := Source(main, body, opts)
	if !hasDiag(diags, "import", "util has no exported member missing") {
		t.Fatalf("expected missing-export diagnostic, got %+v", diags)
	}
}

func TestSourceFromImportWarnsOnUnusedAlias(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	source := "from crypt import passwordHash as unused;\n"
	opts := Options{Lint: true, Resolver: modules.NewResolver([]string{dir})}
	_, diags := Source(file, source, opts)
	if !hasDiag(diags, "unused-import", "from crypt import passwordHash: unused is not used") {
		t.Fatalf("expected unused-from-import warning, got %+v", diags)
	}
}

func TestSourceFromImportMarksAliasAsUsed(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	source := "from crypt import passwordHash;\nlet x = passwordHash(\"hi\");\n"
	opts := Options{Lint: true, Resolver: modules.NewResolver([]string{dir})}
	_, diags := Source(file, source, opts)
	for _, d := range diags {
		if d.Rule == "unused-import" {
			t.Fatalf("referenced from-import flagged as unused: %+v", d)
		}
	}
}

func TestSourceFieldReceiverMethodTypoErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "class Box {\n    list<string> items;\n}\nBox b = Box();\nb.items.smoosh();\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	if !hasDiag(diags, "semantic", "list has no method smoosh") {
		t.Fatalf("expected field-receiver method typo, got %+v", diags)
	}
}

func TestSourceFieldReceiverMethodTypoCrossFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "models.gb"),
		[]byte("module models;\nexport class Bag {\n    list<string> tags;\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.gb")
	src := "from models import Bag;\nBag g = Bag();\ng.tags.smoosh();\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	if !hasDiag(diags, "semantic", "list has no method smoosh") {
		t.Fatalf("expected cross-file field-receiver method typo, got %+v", diags)
	}
}

func TestSourceAnalyzesMethodBodyArgErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "class Greeter {\n" +
		"    func greet(string name): string { return \"hi \" + name; }\n" +
		"    func bad(): string { return this.greet(42); }\n" +
		"    func ok(): string { return this.greet(\"x\"); }\n" +
		"}\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	if !hasDiag(diags, "semantic", "no matching overload for Greeter.greet") {
		t.Fatalf("expected method-body arg mismatch, got %+v", diags)
	}
	got := 0
	for _, d := range diags {
		if strings.Contains(d.Message, "no matching overload for Greeter.greet") {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("expected exactly one overload error (ok() must not flag), got %d: %+v", got, diags)
	}
}

func TestSourceSkipsExistenceOnThisReceiver(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	// existence skipped for missing this.step(), args still checked for this.greet()
	src := "class Tmpl {\n" +
		"    func run(): void { this.step(); }\n" +
		"    func greet(string name): string { return name; }\n" +
		"    func bad(): string { return this.greet(42); }\n" +
		"}\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	for _, d := range diags {
		if strings.Contains(d.Message, "has no method step") {
			t.Fatalf("template-method this.step() must not flag: %+v", d)
		}
	}
	if !hasDiag(diags, "semantic", "no matching overload for Tmpl.greet") {
		t.Fatalf("arg validation on this must still fire, got %+v", diags)
	}
}

func TestSourceFlagsMethodArgCrossModuleQualifiedParent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "base.gb"),
		[]byte("module base;\nexport class Base {\n    func tag(string s): string { return s; }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.gb")
	src := "import base;\nclass Sub extends base.Base {\n    func go(): string { return this.tag(99); }\n}\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	if !hasDiag(diags, "semantic", "no matching overload") {
		t.Fatalf("expected cross-module qualified-parent method arg error, got %+v", diags)
	}
}

// TestSourceFlagsCollectionMismatchInMethodBody verifies check catches a
// list<int> passed to a list<string> param inside a class method body.
func TestSourceFlagsCollectionMismatchInMethodBody(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "func countStrings(list<string> xs): int { return xs.length(); }\n" +
		"class Tester {\n" +
		"    func run(): void {\n" +
		"        let list<int> ints = [1, 2, 3];\n" +
		"        countStrings(ints);\n" +
		"    }\n" +
		"}\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	if !hasDiag(diags, "semantic", "no matching overload for countStrings") {
		t.Fatalf("expected collection element-mismatch error in method body, got %+v", diags)
	}
}

// TestSourceFlagsInvariantGenericArgMismatch verifies check rejects
// Container<Sub> passed where Container<Base> is expected (invariant generics).
func TestSourceFlagsInvariantGenericArgMismatch(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "class Base { int n; func Base(int n) { this.n = n; } }\n" +
		"class Sub extends Base { func Sub(int n) { parent(n); } }\n" +
		"class Container<T> {\n" +
		"    list<T> items;\n" +
		"    func Container() { this.items = []; }\n" +
		"    func add(T item): void { this.items = this.items.push(item); }\n" +
		"}\n" +
		"func consume(Container<Base> c): void { c.add(Base(0)); }\n" +
		"let subs = Container<Sub>();\n" +
		"consume(subs);\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	if !hasDiag(diags, "semantic", "no matching overload for consume") {
		t.Fatalf("expected invariant-generic arg mismatch error, got %+v", diags)
	}
}

func hasDiag(diags []Diagnostic, rule, contains string) bool {
	for _, d := range diags {
		if d.Rule == rule && strings.Contains(d.Message, contains) {
			return true
		}
	}
	return false
}

func TestCompileDiagnosticClassifiesVMGapsAsWarnings(t *testing.T) {
	gap := bytecode.NewParityError(errors.New("bytecode compiler does not support some.thing yet"))
	d, ok := compileDiagnostic("f.gb", gap)
	if !ok {
		t.Fatal("expected a diagnostic for a non-nil error")
	}
	if d.Severity != SeverityWarning || d.Rule != "vm-unsupported" {
		t.Fatalf("VM capability gap should be a vm-unsupported warning, got %s[%s]", d.Severity, d.Rule)
	}
}

func TestCompileDiagnosticClassifiesGenuineErrorsAsErrors(t *testing.T) {
	bad := errors.New("unknown bytecode name notAThing")
	d, ok := compileDiagnostic("f.gb", bad)
	if !ok {
		t.Fatal("expected a diagnostic for a non-nil error")
	}
	if d.Severity != SeverityError || d.Rule != "type" {
		t.Fatalf("genuine static error should be error[type], got %s[%s]", d.Severity, d.Rule)
	}
}

func TestCompileDiagnosticIgnoresNil(t *testing.T) {
	if _, ok := compileDiagnostic("f.gb", nil); ok {
		t.Fatal("nil error must not produce a diagnostic")
	}
}

func TestSourceFlagsMethodArgFromImportParent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "base.gb"),
		[]byte("module base;\nexport class Base {\n    func tag(string s): string { return s; }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.gb")
	src := "from base import Base;\nclass Sub extends Base {\n    func go(): string { return this.tag(99); }\n}\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	if !hasDiag(diags, "semantic", "no matching overload") {
		t.Fatalf("expected from-import-parent method arg error, got %+v", diags)
	}
}

func TestSourceAllowsNullForAnyMethodParam(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "class C {\n    func send(any body): int { return 1; }\n    func go(): int { return this.send(null); }\n}\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	for _, d := range diags {
		if strings.Contains(d.Message, "no matching overload") {
			t.Fatalf("null must be accepted for an any param: %+v", d)
		}
	}
}

func TestSourceAllowsNullAssignToNullableAfterNarrowing(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "class C {\n    func loop(): void {\n        ?int n = 5;\n        while (n != null) {\n            if (n == 1) { n = null; } else { n = n - 1; }\n        }\n    }\n}\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	for _, d := range diags {
		if strings.Contains(d.Message, "cannot assign null") {
			t.Fatalf("null assign to a declared-nullable var must be allowed: %+v", d)
		}
	}
}

func TestSourceAllowsNullAssignAfterGuardNarrowing(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "class C {\n    func f(): void {\n        ?int x = 5;\n        if (x == null) { return; }\n        x = null;\n    }\n}\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	for _, d := range diags {
		if strings.Contains(d.Message, "cannot assign null") {
			t.Fatalf("null assign to a declared-nullable var after guard narrowing must be allowed: %+v", d)
		}
	}
}

func TestSourceAllowsAnyReturnToGenericParam(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.gb")
	src := "class Box<T> {\n    any v;\n    func get(): T { return this.v; }\n}\n"
	_, diags := Source(file, src, classCheckOpts(dir))
	for _, d := range diags {
		if strings.Contains(d.Message, "cannot return any") {
			t.Fatalf("any->generic-param return must be allowed: %+v", d)
		}
	}
}

// A *_test.gb declaring the same module name as a sibling module file
// merges with it so private references analyze; plain scripts, non-test
// files, and mismatched declarations do not merge.
func TestSameModuleTestProgram(t *testing.T) {
	dir := t.TempDir()
	moduleSource := `module helpers;
func hidden(int x): int { return x * 2; }
export func visible(int x): int { return hidden(x); }
`
	if err := os.WriteFile(filepath.Join(dir, "helpers.gb"), []byte(moduleSource), 0o644); err != nil {
		t.Fatal(err)
	}
	testSource := `module helpers;
import test;
class HiddenTest extends test.Test {
    @test
    func doubles(): void { this.assertEquals(4, hidden(2)); }
}
`
	testPath := filepath.Join(dir, "helpers_test.gb")
	if err := os.WriteFile(testPath, []byte(testSource), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := modules.NewResolver([]string{dir})

	parse := func(src string) *ast.Program {
		p := parser.New(lexer.New(src))
		program := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse: %v", p.Errors())
		}
		return program
	}

	merged, ok := SameModuleTestProgram(testPath, parse(testSource), resolver)
	if !ok {
		t.Fatal("expected same-module merge to apply")
	}
	if DeclaredModuleName(merged) != "" {
		t.Fatal("merged program must not retain a module declaration")
	}
	sawHidden := false
	for _, stmt := range merged.Statements {
		if fn, ok := stmt.(*ast.FunctionStatement); ok && fn.Name.Value == "hidden" {
			sawHidden = true
		}
	}
	if !sawHidden {
		t.Fatal("merged program must include the module's private function")
	}

	// The merged program checks clean; the unmerged test file would not.
	diags := CrossModuleAnalysis(testPath, merged, Options{Resolver: resolver, CrossModule: true})
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Fatalf("unexpected diagnostic on merged program: %s", d.Message)
		}
	}

	if _, ok := SameModuleTestProgram(filepath.Join(dir, "helpers_other.gb"), parse(testSource), resolver); ok {
		t.Fatal("non *_test.gb file must not merge")
	}
	if _, ok := SameModuleTestProgram(testPath, parse("import test;\n"), resolver); ok {
		t.Fatal("test file without a module declaration must not merge")
	}
	if _, ok := SameModuleTestProgram(testPath, parse("module nothere;\n"), resolver); ok {
		t.Fatal("unresolvable module declaration must not merge")
	}
}
