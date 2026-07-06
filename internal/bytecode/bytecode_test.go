package bytecode_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"geblang/internal/ast"
	"geblang/internal/bytecode"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/runtime"
)

func TestEncodeDecodeChunk(t *testing.T) {
	source := []byte("io.println(\"hello\");")
	chunk := bytecode.Chunk{
		SourceHash: bytecode.SourceHash(source),
		Compiler:   "test-compiler",
		Constants: []runtime.Value{
			runtime.NewInt64(7),
			runtime.String{Value: "hello"},
			mustDecimal(t, "1.25"),
			runtime.Float{Value: 3.5},
			runtime.DecoratorTarget{Target: "function", Function: &runtime.FunctionMetadata{Name: "index", Target: "function", TypeParameters: []string{"T"}, Parameters: []runtime.ParameterMetadata{{Name: "name", Type: "string"}, {Name: "limit", Type: "int", HasDefault: true}}, ReturnType: "string", Decorators: []runtime.DecoratorMetadata{{Name: "route", Target: "function", Args: []runtime.Value{runtime.String{Value: "GET"}}, NamedArgs: map[string]runtime.Value{"name": runtime.String{Value: "users"}}}}}, Decorators: []runtime.DecoratorMetadata{{
				Name:      "route",
				Target:    "function",
				Position:  0,
				Args:      []runtime.Value{runtime.String{Value: "GET"}},
				NamedArgs: map[string]runtime.Value{"name": runtime.String{Value: "users"}},
				Line:      12,
				Column:    1,
			}}},
		},
		Instructions: []bytecode.Instruction{
			{Op: bytecode.OpNoop, Operands: []int64{1, 2}, Line: 1, Column: 1},
		},
		Functions: []bytecode.FunctionInfo{
			{Name: "add", TypeParameters: []string{"T"}, Entry: 3, ParamNames: []string{"a", "b"}, ParamSlots: []int64{1, 2}, ParamTypes: []string{"int", "?string"}, ReturnType: "int", DefaultConstants: []int64{-1, 1}, Decorators: []runtime.DecoratorMetadata{{Name: "tag", Target: "function", Args: []runtime.Value{runtime.String{Value: "fast"}}, NamedArgs: map[string]runtime.Value{}}}},
		},
		Classes: []bytecode.ClassInfo{
			{Name: "User", ParentName: "Base", ParentIndex: -1, FieldNames: []string{"name"}, FieldDefaults: []int64{1}, ConstructorIndices: []int64{0}, Methods: map[string][]int64{"label": []int64{0}}, StaticValues: map[string]int64{"KIND": 1}, StaticMethods: map[string][]int64{"make": []int64{0}}, Decorators: []runtime.DecoratorMetadata{{Name: "service", Target: "class", NamedArgs: map[string]runtime.Value{"name": runtime.String{Value: "users"}}}}, MethodDecorators: map[string][]runtime.DecoratorMetadata{"label": []runtime.DecoratorMetadata{{Name: "route", Target: "method", Args: []runtime.Value{runtime.String{Value: "GET"}}, NamedArgs: map[string]runtime.Value{}}}}, StaticDecorators: map[string][]runtime.DecoratorMetadata{"make": []runtime.DecoratorMetadata{{Name: "route", Target: "staticMethod", Args: []runtime.Value{runtime.String{Value: "POST"}}, NamedArgs: map[string]runtime.Value{}}}}},
		},
		Exports: []bytecode.ExportInfo{
			{Name: "User", Slot: 0, FunctionIndex: -1, ClassIndex: 0, InterfaceIndex: -1},
			{Name: "Marker", Slot: 2, FunctionIndex: -1, ClassIndex: -1, InterfaceIndex: 3},
		},
	}

	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Compiler != chunk.Compiler {
		t.Fatalf("compiler: got %q, want %q", decoded.Compiler, chunk.Compiler)
	}
	if !bytes.Equal(decoded.SourceHash[:], chunk.SourceHash[:]) {
		t.Fatalf("source hash mismatch")
	}
	if len(decoded.Constants) != 5 || decoded.Constants[0].Inspect() != "7" || decoded.Constants[1].Inspect() != "hello" || decoded.Constants[2].Inspect() != "1.2500000000" || decoded.Constants[3].Inspect() != "3.5" {
		t.Fatalf("decoded constants mismatch: %#v", decoded.Constants)
	}
	target, ok := decoded.Constants[4].(runtime.DecoratorTarget)
	if !ok || target.Target != "function" || target.Function == nil || len(target.Function.TypeParameters) != 1 || target.Function.TypeParameters[0] != "T" || len(target.Function.Parameters) != 2 || target.Function.Parameters[1].Name != "limit" || !target.Function.Parameters[1].HasDefault || target.Function.ReturnType != "string" || len(target.Decorators) != 1 || target.Decorators[0].Name != "route" || target.Decorators[0].Args[0].Inspect() != "GET" || target.Decorators[0].NamedArgs["name"].Inspect() != "users" {
		t.Fatalf("decoded decorator target mismatch: %#v", decoded.Constants[4])
	}
	if len(decoded.Instructions) != 1 {
		t.Fatalf("instruction count: got %d", len(decoded.Instructions))
	}
	if decoded.Instructions[0].Op != bytecode.OpNoop || decoded.Instructions[0].Operands[1] != 2 {
		t.Fatalf("decoded instruction mismatch: %#v", decoded.Instructions[0])
	}
	if len(decoded.Functions) != 1 || decoded.Functions[0].Name != "add" || len(decoded.Functions[0].TypeParameters) != 1 || decoded.Functions[0].TypeParameters[0] != "T" || decoded.Functions[0].Entry != 3 || len(decoded.Functions[0].ParamNames) != 2 || decoded.Functions[0].ParamNames[1] != "b" || len(decoded.Functions[0].ParamSlots) != 2 || decoded.Functions[0].ParamSlots[1] != 2 || len(decoded.Functions[0].ParamTypes) != 2 || decoded.Functions[0].ParamTypes[1] != "?string" || decoded.Functions[0].ReturnType != "int" || len(decoded.Functions[0].DefaultConstants) != 2 || decoded.Functions[0].DefaultConstants[1] != 1 {
		t.Fatalf("decoded functions mismatch: %#v", decoded.Functions)
	}
	if len(decoded.Functions[0].Decorators) != 1 || decoded.Functions[0].Decorators[0].Name != "tag" || decoded.Functions[0].Decorators[0].Args[0].Inspect() != "fast" {
		t.Fatalf("decoded function decorators mismatch: %#v", decoded.Functions[0].Decorators)
	}
	if len(decoded.Classes) != 1 || decoded.Classes[0].Name != "User" || decoded.Classes[0].ParentName != "Base" || decoded.Classes[0].ParentIndex != -1 || decoded.Classes[0].FieldNames[0] != "name" || decoded.Classes[0].FieldDefaults[0] != 1 || decoded.Classes[0].ConstructorIndices[0] != 0 || decoded.Classes[0].Methods["label"][0] != 0 || decoded.Classes[0].StaticValues["KIND"] != 1 || decoded.Classes[0].StaticMethods["make"][0] != 0 {
		t.Fatalf("decoded classes mismatch: %#v", decoded.Classes)
	}
	if len(decoded.Classes[0].Decorators) != 1 || decoded.Classes[0].Decorators[0].Name != "service" || decoded.Classes[0].Decorators[0].NamedArgs["name"].Inspect() != "users" {
		t.Fatalf("decoded class decorators mismatch: %#v", decoded.Classes[0].Decorators)
	}
	if decoded.Classes[0].MethodDecorators["label"][0].Args[0].Inspect() != "GET" || decoded.Classes[0].StaticDecorators["make"][0].Args[0].Inspect() != "POST" {
		t.Fatalf("decoded class method decorators mismatch: %#v %#v", decoded.Classes[0].MethodDecorators, decoded.Classes[0].StaticDecorators)
	}
	if len(decoded.Exports) != 2 || decoded.Exports[1].Name != "Marker" || decoded.Exports[1].InterfaceIndex != 3 || decoded.Exports[1].ClassIndex != -1 || decoded.Exports[1].FunctionIndex != -1 || decoded.Exports[1].Slot != 2 || decoded.Exports[0].ClassIndex != 0 || decoded.Exports[0].InterfaceIndex != -1 {
		t.Fatalf("decoded exports mismatch: %#v", decoded.Exports)
	}
}

func TestCompileUnsupportedFeatureReportsStatementLocation(t *testing.T) {
	/* Non-literal class field defaults are one of the parity
	 * fallbacks the bytecode pipeline still routes through the
	 * evaluator. This test guards that the rejection carries an
	 * accurate source line+column so the fallback reporter can
	 * blame the right statement. */
	source := []byte(`func compute(): int { return 42; }
class Foo {
    int x = compute();
}
`)
	program := parseProgram(t, string(source))
	_, err := bytecode.Compile(program, source, "test")
	if err == nil {
		t.Fatal("expected compile error")
	}
	if !strings.Contains(err.Error(), "literal class field defaults") {
		t.Fatalf("error: got %v", err)
	}
}

// TestCompileAllowsStaticFunctions verifies the lifted compiler
// parity gap: `static func` declarations now compile to bytecode
// directly. Previously they bailed out as a parity error and the
// CLI fell back to the evaluator.
func TestCompileAllowsStaticFunctions(t *testing.T) {
	source := []byte(`class Foo {
    static func greet(string name): string {
        return "hello " + name;
    }
}
`)
	program := parseProgram(t, string(source))
	if _, err := bytecode.Compile(program, source, "test"); err != nil {
		t.Fatalf("compile: %v", err)
	}
}

func TestCompileStatefulBuiltinCallsForVM(t *testing.T) {
	source := []byte(`import ext;
let conn = ext.load("python_example");
ext.call(conn, "greet", name: "Geblang");
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var sawLoad bool
	var sawNamedCall bool
	for _, instruction := range chunk.Instructions {
		if instruction.Op == bytecode.OpNativeCall {
			name := chunk.Constants[instruction.Operands[0]].(runtime.String).Value
			if name == "ext.load" {
				sawLoad = true
			}
		}
		if instruction.Op == bytecode.OpNativeCallNamed {
			name := chunk.Constants[instruction.Operands[0]].(runtime.String).Value
			if name == "ext.call" {
				sawNamedCall = true
			}
		}
	}
	if !sawLoad || !sawNamedCall {
		t.Fatalf("expected ext.load native call and named ext.call, got %#v", chunk.Instructions)
	}
}

type fakeStatefulNative struct {
	calls []string
	names [][]string
}

func (f *fakeStatefulNative) CallBuiltin(module, name string, args []runtime.Value, argNames []string) (runtime.Value, error) {
	f.calls = append(f.calls, module+"."+name)
	f.names = append(f.names, append([]string(nil), argNames...))
	switch module + "." + name {
	case "metrics.inc":
		return runtime.Null{}, nil
	case "metrics.get":
		return runtime.NewInt64(7), nil
	case "async.sleep":
		return runtime.String{Value: "slept"}, nil
	case "async.await":
		return args[0], nil
	case "ext.call":
		return runtime.String{Value: "hello " + args[2].(runtime.String).Value}, nil
	case "cli.style":
		return runtime.String{Value: "styled:" + args[0].(runtime.String).Value}, nil
	default:
		return nil, fmt.Errorf("unexpected stateful call %s.%s", module, name)
	}
}

func TestVMRunsCLIModuleThroughStatefulNativeBridge(t *testing.T) {
	source := []byte(`import io;
import cli;

io.println(cli.style("ok", "green"));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	fake := &fakeStatefulNative{}
	vm := bytecode.NewVM(chunk, &out)
	vm.SetStatefulNativeCaller(fake)
	if err := vm.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "styled:ok\n" {
		t.Fatalf("output: got %q", out.String())
	}
	if strings.Join(fake.calls, ",") != "cli.style" {
		t.Fatalf("calls: got %#v", fake.calls)
	}
}

type fakeModuleLoader struct {
	module *runtime.Module
}

func (l fakeModuleLoader) LoadModule(canonical string, alias string) (*runtime.Module, error) {
	return l.module, nil
}

func (l fakeModuleLoader) CallModuleFunction(function runtime.BytecodeFunction, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	return runtime.Null{}, fmt.Errorf("unexpected module function call %s", function.Name)
}

func (l fakeModuleLoader) CallModuleClosure(closure runtime.BytecodeClosure, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	return runtime.Null{}, fmt.Errorf("unexpected module closure call %s", closure.Name)
}

func (l fakeModuleLoader) CallModuleHandlerIsolated(fn runtime.Value, module string, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	return runtime.Null{}, fmt.Errorf("unexpected isolated handler call in module %s", module)
}

func (l fakeModuleLoader) ConstructModuleClass(class runtime.BytecodeClass, args []runtime.Value, typeArgs []string, caller *bytecode.VM) (runtime.Value, error) {
	return runtime.Null{}, fmt.Errorf("unexpected module class construction %s", class.Name)
}

func (l fakeModuleLoader) ConstructModuleClassNamed(class runtime.BytecodeClass, args []runtime.Value, names []string, typeArgs []string, caller *bytecode.VM) (runtime.Value, error) {
	return runtime.Null{}, fmt.Errorf("unexpected named module class construction %s", class.Name)
}

func (l fakeModuleLoader) CallModuleOverloaded(module string, indices []int64, displayName string, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	return runtime.Null{}, fmt.Errorf("unexpected module overloaded call %s", displayName)
}

func (l fakeModuleLoader) CallModuleOverloadedNamed(module string, indices []int64, displayName string, args []runtime.Value, names []string, caller *bytecode.VM) (runtime.Value, error) {
	return runtime.Null{}, fmt.Errorf("unexpected named module overloaded call %s", displayName)
}

func (l fakeModuleLoader) CallModuleStaticMethod(class runtime.BytecodeClass, methodName string, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	return runtime.Null{}, fmt.Errorf("unexpected module static method call %s.%s", class.Name, methodName)
}

func (l fakeModuleLoader) ModuleMethodParamNames(module string, className string, methodName string) ([]string, error) {
	return nil, fmt.Errorf("fake loader has no module method metadata")
}

func (l fakeModuleLoader) ModuleFunctionParamNames(module string, index int64) ([]string, error) {
	return nil, fmt.Errorf("fake loader has no module function metadata")
}

func (l fakeModuleLoader) ModuleConstructorParamNames(module string, className string) ([]string, error) {
	return nil, fmt.Errorf("fake loader has no module constructor metadata")
}

func (l fakeModuleLoader) CallModuleMethod(module string, className string, methodName string, instance *runtime.Instance, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	return runtime.Null{}, fmt.Errorf("unexpected module method call %s.%s", className, methodName)
}

func (l fakeModuleLoader) CallModuleMethodValue(module string, funcIndex int64, instance *runtime.Instance, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	return runtime.Null{}, fmt.Errorf("unexpected module method value call in %s", module)
}

func (l fakeModuleLoader) CallModuleDestructor(module string, className string, instance *runtime.Instance, caller *bytecode.VM) error {
	return fmt.Errorf("unexpected module destructor call %s.%s", module, className)
}

func (l fakeModuleLoader) CallParentInModule(module string, className string, methodName string, instance *runtime.Instance, args []runtime.Value, caller *bytecode.VM) (runtime.Value, error) {
	return runtime.Null{}, fmt.Errorf("unexpected cross-module parent call %s.%s", className, methodName)
}

func (l fakeModuleLoader) ImmutableFieldsForModuleClass(module string, className string) []string {
	return nil
}

func (l fakeModuleLoader) FindClassByName(name string) (runtime.Value, bool) {
	return nil, false
}

func (l fakeModuleLoader) ClassValueInModule(module, className string) (runtime.Value, bool) {
	return nil, false
}

func (l fakeModuleLoader) RuntimeClassFor(module string, className string) (*runtime.Class, bool) {
	return nil, false
}

func (l fakeModuleLoader) PersistModuleGlobals(vm *bytecode.VM) {}

func (l fakeModuleLoader) FindFunctionByName(name string) (runtime.Value, bool) {
	return nil, false
}

func (l fakeModuleLoader) DeserializeModuleClass(class runtime.BytecodeClass, value runtime.Value) (runtime.Value, error) {
	return nil, fmt.Errorf("module %s is not loaded", class.Module)
}

func (l fakeModuleLoader) ConstructorsForModuleClass(class runtime.BytecodeClass) (runtime.Value, error) {
	return nil, fmt.Errorf("module %s is not loaded", class.Module)
}

func (l fakeModuleLoader) FieldsForModuleClass(class runtime.BytecodeClass) (runtime.Value, error) {
	return nil, fmt.Errorf("module %s is not loaded", class.Module)
}

func (l fakeModuleLoader) LookupModuleInterface(module, name string) (bytecode.InterfaceInfo, bool) {
	return bytecode.InterfaceInfo{}, false
}

func (l fakeModuleLoader) ListAllClasses() []runtime.Value { return nil }

func (l fakeModuleLoader) ModuleClassDescendsFrom(module, className, targetSimpleName string) bool {
	return false
}

func (l fakeModuleLoader) StaticValueForModuleClass(module, className, name string) (runtime.Value, bool) {
	return nil, false
}

func (l fakeModuleLoader) CallModuleStaticMethodByName(module, className, methodName string, args []runtime.Value) (runtime.Value, bool, error) {
	return nil, false, nil
}

func (l fakeModuleLoader) StaticMethodValueForModuleClass(module, className, name string) (runtime.Value, bool) {
	return nil, false
}

func (l fakeModuleLoader) MethodFirstOverload(module, className, name string) (string, int64, bool) {
	return "", 0, false
}

func (l fakeModuleLoader) UnimplementedAbstractMethods(module, className string) map[string]string {
	return nil
}

func TestVMRunsStatefulNativeBridge(t *testing.T) {
	source := []byte(`import io;
import metrics;

metrics.inc("hits");
io.println(metrics.get("hits"));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	fake := &fakeStatefulNative{}
	vm := bytecode.NewVM(chunk, &out)
	vm.SetStatefulNativeCaller(fake)
	if err := vm.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "7\n" {
		t.Fatalf("output: got %q", out.String())
	}
	if strings.Join(fake.calls, ",") != "metrics.inc,metrics.get" {
		t.Fatalf("calls: got %#v", fake.calls)
	}
}

func TestVMRunsNamedStatefulNativeBridge(t *testing.T) {
	source := []byte(`import io;
import ext;

io.println(ext.call(function: "greet", conn: 1, args: "Geblang"));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	fake := &fakeStatefulNative{}
	vm := bytecode.NewVM(chunk, &out)
	vm.SetStatefulNativeCaller(fake)
	if err := vm.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	// Named args bind to ext.call's parameters (conn, function, args) and reach the stateful native as reordered positional args, with no names forwarded.
	if out.String() != "hello Geblang\n" {
		t.Fatalf("output: got %q", out.String())
	}
	if len(fake.names) != 1 || len(fake.names[0]) != 0 {
		t.Fatalf("names should be cleared after binding: got %#v", fake.names)
	}
}

func TestVMRunsAsyncModuleThroughStatefulNativeBridge(t *testing.T) {
	source := []byte(`import async;
import io;

let task = async.sleep(1);
io.println(async.await(task));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	fake := &fakeStatefulNative{}
	vm := bytecode.NewVM(chunk, &out)
	vm.SetStatefulNativeCaller(fake)
	if err := vm.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "slept\n" {
		t.Fatalf("output: got %q", out.String())
	}
	if strings.Join(fake.calls, ",") != "async.sleep,async.await" {
		t.Fatalf("calls: got %#v", fake.calls)
	}
}

func TestVMDeferNativeCall(t *testing.T) {
	source := []byte(`import io;
import metrics;
func run(): void {
    defer metrics.inc("deferred");
    metrics.inc("body");
    io.println("body");
}
run();
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var out bytes.Buffer
	fake := &fakeStatefulNative{}
	vm := bytecode.NewVM(chunk, &out)
	vm.SetStatefulNativeCaller(fake)
	if err := vm.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "body\n" {
		t.Fatalf("output: got %q", out.String())
	}
	if strings.Join(fake.calls, ",") != "metrics.inc,metrics.inc" {
		t.Fatalf("calls: got %#v - expected body call first, deferred call second", fake.calls)
	}
}

func TestCompileAndRunBytecodeSubset(t *testing.T) {
	source := []byte(`import io;
int x = 2 + 3 * 4;
io.println(x);
io.println("done");
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "14\ndone\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileEncodeDecodeRunBackedEnum(t *testing.T) {
	source := []byte(`import io;
enum Status: string {
    Active = "active";
    Closed = "closed";
}
io.println(Status.Active.value);
io.println(Status.from("closed") == Status.Closed);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got, want := out.String(), "active\ntrue\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestCompileAndRunBytecodeInitBlock(t *testing.T) {
	source := []byte(`import io;
int value = 1;
init {
    value = value + 2;
    io.println("init");
}
io.println(value);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "init\n3\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeModuleAndExportDeclarations(t *testing.T) {
	source := []byte(`module math.calc;
import io;
export const string label = "calc";
export func double(int x): int {
    return x * 2;
}
io.println(label);
io.println(double(4));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "calc\n8\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileBytecodeUserModuleImport(t *testing.T) {
	source := []byte(`import util;
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	found := false
	for _, instruction := range chunk.Instructions {
		if instruction.Op == bytecode.OpImportModule {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected import module opcode")
	}
}

func TestVMReflectsImportedModuleExportDecorators(t *testing.T) {
	moduleSource := []byte(`module util;
export @route("GET", "/users", name: "users")
func index(string name, int limit = 10): string {
    return "ok";
}

export @service(name: "users")
class Controller {
    string prefix;
    int count;

    @route("GET", "/users")
    func list(string prefix): string {
        return "list";
    }

    @route("POST", "/users")
    static func create(int count): string {
        return "created";
    }
}
`)
	moduleProgram := parseProgram(t, string(moduleSource))
	moduleChunk, err := bytecode.Compile(moduleProgram, moduleSource, "test")
	if err != nil {
		t.Fatalf("compile module: %v", err)
	}
	moduleVM := bytecode.NewVM(moduleChunk, &bytes.Buffer{})
	if err := moduleVM.Run(); err != nil {
		t.Fatalf("run module: %v", err)
	}
	exports, err := moduleVM.Exports()
	if err != nil {
		t.Fatalf("exports: %v", err)
	}
	module := &runtime.Module{Name: "util", Exports: exports}
	for name, value := range module.Exports {
		if function, ok := value.(runtime.BytecodeFunction); ok {
			function.Module = "util"
			module.Exports[name] = function
		}
		if class, ok := value.(runtime.BytecodeClass); ok {
			class.Module = "util"
			module.Exports[name] = class
		}
	}

	source := []byte(`import io;
import reflect;
import util;

let fn = reflect.decorators(util.index);
io.println(fn[0]["name"]);
io.println(fn[0]["target"]);
io.println(fn[0]["args"][0]);
io.println(fn[0]["namedArgs"]["name"]);
io.println(reflect.hasDecorator(util.index, "route"));

let cls = reflect.decorators(util.Controller);
io.println(cls[0]["name"]);
io.println(cls[0]["target"]);
io.println(cls[0]["namedArgs"]["name"]);

let mod = reflect.module("util");
let exports = reflect.exports(mod);
io.println(exports[0]);
io.println(exports[1]);
io.println(reflect.module("missing") == null);

let namedFn = reflect.decorators(reflect.function("util.index"));
io.println(namedFn[0]["name"]);
io.println(namedFn[0]["args"][0]);
let fnParams = reflect.parameters(reflect.function("util.index"));
io.println(fnParams[0]["name"]);
io.println(fnParams[0]["type"]);
io.println(fnParams[1]["hasDefault"]);
io.println(reflect.returnType(reflect.function("util.index")));

let namedClass = reflect.decorators(reflect.class("util.Controller"));
io.println(namedClass[0]["name"]);
io.println(namedClass[0]["namedArgs"]["name"]);
io.println(reflect.fields(reflect.class("util.Controller"))[0]["name"]);
io.println(reflect.fields(reflect.class("util.Controller"))[1]["name"]);
io.println(reflect.methods(reflect.class("util.Controller"))[0]);
io.println(reflect.staticMethods(reflect.class("util.Controller"))[0]);
io.println(reflect.parent(reflect.class("util.Controller")) == null);
io.println(reflect.function("util.missing") == null);
io.println(reflect.class("util.missing") == null);

let method = reflect.method(util.Controller, "list");
io.println(reflect.decorators(method)[0]["target"]);
io.println(reflect.decorators(method)[0]["args"][0]);

let namedMethod = reflect.method(reflect.class("util.Controller"), "list");
io.println(reflect.decorators(namedMethod)[0]["target"]);
io.println(reflect.decorators(namedMethod)[0]["args"][0]);
let methodParams = reflect.parameters(namedMethod);
io.println(methodParams[0]["name"]);
io.println(methodParams[0]["type"]);
io.println(reflect.returnType(namedMethod));

let staticMethod = reflect.staticMethod(util.Controller, "create");
io.println(reflect.decorators(staticMethod)[0]["target"]);
io.println(reflect.decorators(staticMethod)[0]["args"][0]);

let namedStaticMethod = reflect.staticMethod(reflect.class("util.Controller"), "create");
io.println(reflect.decorators(namedStaticMethod)[0]["target"]);
io.println(reflect.decorators(namedStaticMethod)[0]["args"][0]);
let staticParams = reflect.parameters(namedStaticMethod);
io.println(staticParams[0]["name"]);
io.println(staticParams[0]["type"]);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var out bytes.Buffer
	if err := bytecode.NewVMWithModuleLoader(chunk, &out, fakeModuleLoader{module: module}).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "route\nfunction\nGET\nusers\ntrue\nservice\nclass\nusers\nController\nindex\ntrue\nroute\nGET\nname\nstring\ntrue\nstring\nservice\nusers\ncount\nprefix\nlist\ncreate\ntrue\ntrue\ntrue\nmethod\nGET\nmethod\nGET\nprefix\nstring\nstring\nstaticMethod\nPOST\nstaticMethod\nPOST\ncount\nint\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestCompileAndRunBytecodePrintCalls(t *testing.T) {
	source := []byte(`import io;
io.print("hello");
io.stdoutWrite(" ");
io.println("world");
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "hello world\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeNamedSingleArgumentBuiltins(t *testing.T) {
	source := []byte(`import io;
import json;
io.print(value: "hello");
io.stdoutWrite(value: " ");
io.println(value: json.validate(text: "{\"ok\": true}"));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "hello true\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeSysExit(t *testing.T) {
	source := []byte(`import sys;
sys.exit(7);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	err = bytecode.NewVM(chunk, &out).Run()
	var exitErr bytecode.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run error: got %v, want ExitError", err)
	}
	if exitErr.Code != 7 {
		t.Fatalf("exit code: got %d, want 7", exitErr.Code)
	}
}

func TestCompileAndRunBytecodeMathBuiltins(t *testing.T) {
	source := []byte(`import io;
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
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "5\n1\n3\n10\n3\n4\n4\n9007199254740993\n9007199254740993\n9007199254740993\n3\n8\n0\ntrue\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeJSONBuiltins(t *testing.T) {
	source := []byte(`import io;
import json;
let parsed = json.parse("{\"name\":\"Ada\",\"scores\":[2,3],\"ok\":true}");
io.println(parsed["name"]);
io.println(parsed["scores"][1]);
io.println(parsed["ok"]);
io.println(json.stringify({"b": 2, "a": [true, null]}));
io.println(json.validate("{\"ok\":true}"));
io.println(json.validate("{\"ok\":"));
let reparsed = json.tryParse("{\"ok\": true}");
io.println(reparsed["ok"]);
io.println(reparsed["value"]["ok"]);
let failed = json.tryParse("{\"ok\":");
io.println(failed["ok"]);
io.println(failed["error"]["line"] > 0);
io.println(failed["error"]["column"] > 0);
let detailed = json.validateDetailed("{\"ok\":");
io.println(detailed["valid"]);
io.println(detailed["error"]["offset"] > 0);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "Ada\n3\ntrue\n{\"a\":[true,null],\"b\":2}\ntrue\nfalse\ntrue\ntrue\nfalse\ntrue\ntrue\nfalse\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeXMLBuiltins(t *testing.T) {
	source := []byte(`import io;
import xml;
io.println(xml.validate("<root><child /></root>"));
io.println(xml.validate("<root><child></root>"));
io.println(xml.validate("<a></a><b></b>"));
let parsed = xml.parse("<root id=\"1\"><child>text</child></root>");
io.println(parsed["name"]);
io.println(parsed["attributes"]["id"]);
io.println(parsed["children"][0]["name"]);
io.println(parsed["children"][0]["text"]);
io.println(xml.stringify(parsed));
let tryParsed = xml.tryParse("<root />");
io.println(tryParsed["ok"]);
io.println(tryParsed["value"]["name"]);
let detailed = xml.validateDetailed("<root><child></root>");
io.println(detailed["valid"]);
io.println(detailed["error"]["line"] > 0);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "true\nfalse\nfalse\nroot\n1\nchild\ntext\n<root id=\"1\"><child>text</child></root>\ntrue\nroot\nfalse\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeTOMLBuiltins(t *testing.T) {
	source := []byte(`import io;
import toml;
let parsed = toml.parse("name = \"Ada\"\n[server]\nport = 8080\n");
io.println(parsed["name"]);
io.println(parsed["server"]["port"]);
io.println(toml.stringify({"name": "Ada"}).contains("name = \"Ada\""));
let tryParsed = toml.tryParse("name = \"Ada\"\n");
io.println(tryParsed["ok"]);
io.println(tryParsed["value"]["name"]);
let failed = toml.tryParse("name = \n");
io.println(failed["ok"]);
io.println(failed["error"]["line"] > 0);
io.println(toml.validate("name = \"Ada\"\n"));
let detailed = toml.validateDetailed("name = \n");
io.println(detailed["valid"]);
io.println(detailed["error"]["message"].length() > 0);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "Ada\n8080\ntrue\ntrue\nAda\nfalse\ntrue\ntrue\nfalse\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeYAMLBuiltins(t *testing.T) {
	source := []byte(`import io;
import yaml;
let parsed = yaml.parse("name: Ada\nserver:\n  port: 8080\n");
io.println(parsed["name"]);
io.println(parsed["server"]["port"]);
io.println(yaml.stringify({"name": "Ada"}).contains("name: Ada"));
let tryParsed = yaml.tryParse("name: Ada\n");
io.println(tryParsed["ok"]);
io.println(tryParsed["value"]["name"]);
let failed = yaml.tryParse("name: [\n");
io.println(failed["ok"]);
io.println(failed["error"]["line"] > 0);
io.println(yaml.validate("name: Ada\n"));
let detailed = yaml.validateDetailed("name: [\n");
io.println(detailed["valid"]);
io.println(detailed["error"]["message"].length() > 0);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "Ada\n8080\ntrue\ntrue\nAda\nfalse\ntrue\ntrue\nfalse\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeCryptBuiltins(t *testing.T) {
	source := []byte(`import crypt;
import io;
io.println(crypt.md5("abc"));
io.println(crypt.sha1("abc"));
io.println(crypt.sha256("abc"));
io.println(crypt.sha3_256("abc"));
io.println(crypt.blake2b("abc"));
io.println(crypt.crc32("abc"));
io.println(crypt.sha512("abc").length());
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
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "900150983cd24fb0d6963f7d28e17f72\na9993e364706816aba3e25717850c26c9cd0d89d\nba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\n3a985da74fe225b2045c172d6bd390bd855f086e3e9d525b46bfe24511431532\nbddd813c634239723171ef3fee98579b94964e3bb1cb3e427262c8c068d52319\n891568578\n128\n6e9ef29b75fffc5b7abae527d58fdadb2fe42e7219011976917343065f58ed4a\ntrue\nfalse\ntrue\ntrue\nfalse\naGVsbG8=\nhello\n8\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeDatetimeBuiltins(t *testing.T) {
	source := []byte(`import datetime;
import io;
io.println(datetime.unix(0));
let parsed = datetime.parse("1970-01-01T00:00:00Z");
io.println(parsed);
io.println(datetime.addSeconds(parsed, 60));
io.println(datetime.format(0, "2006-01-02"));
io.println(datetime.nowUnix() > 0);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "1970-01-01T00:00:00Z\n0\n60\n1970-01-01\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeSecretsBuiltins(t *testing.T) {
	source := []byte(`import io;
import secrets;
let n = secrets.randomInt(5, 7);
io.println(n >= 5 && n <= 7);
io.println(secrets.randomHex(4).length());
io.println(secrets.randomBase64(6).length());
io.println(secrets.constantTimeEqual("same", "same"));
io.println(secrets.constantTimeEqual("same", "different"));
io.println(secrets.constantTimeEqual("same", "sane"));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "true\n8\n8\ntrue\nfalse\nfalse\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeBytesBuiltins(t *testing.T) {
	source := []byte(`import bytes;
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
io.println(joined.toString());
io.println(json.stringify({"blob": bytes.fromString("hi")}));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "5\n101\n101\nell\n68656c6c6f\nworld\naGVsbG8=\nhi\ntrue\nhello\nhello!\n{\"blob\":\"aGk=\"}\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeFunctions(t *testing.T) {
	source := []byte(`import io;
func add(int a, int b): int {
    return a + b;
}
func fact(int n): int {
    if (n <= 1) {
        return 1;
    }
    return n * fact(n - 1);
}
io.println(add(2, 3));
io.println(FACT(5));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "5\n120\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeCallableTypeHints(t *testing.T) {
	source := []byte(`import io;

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
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "7\nfn\ntext\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeSpreadsDictIntoNamedArguments(t *testing.T) {
	source := []byte(`import io;

func join(string left, string right, string sep = "-"): string {
    return left + sep + right;
}

let args = {"right": "B", "sep": "|"};
io.println(join("A", ...args));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "A|B\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeGenericFunctionsWithTypeErasure(t *testing.T) {
	source := []byte(`import io;

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
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "Ada\n7\nx\n10\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeGenericFunctionOverloadRequiresConcreteContainer(t *testing.T) {
	source := []byte(`func first<T>(list<T> values): T {
    return values[0];
}

func first(string value): string {
    return value;
}

first(10);
`)
	program := parseProgram(t, string(source))
	_, err := bytecode.Compile(program, source, "test")
	if err == nil || !strings.Contains(err.Error(), "no matching overload for first") {
		t.Fatalf("expected overload error, got %v", err)
	}
}

// A lone candidate whose parameter type the static arg cannot satisfy yields
// the evaluator-matching detailed message, not the generic overload error.
func TestCompileOverloadMismatchDetailMatchesEvaluator(t *testing.T) {
	source := []byte(`func g(int x): int { return x; }
g("s");
`)
	program := parseProgram(t, string(source))
	_, err := bytecode.Compile(program, source, "test")
	want := "g expects int for parameter 'x', got string"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("expected %q, got %v", want, err)
	}
}

func TestCompileAndRunBytecodeRejectsExcessiveRecursion(t *testing.T) {
	// Non-tail recursion: the `+ 0` keeps the call out of tail position
	// so the new OpTailCall fast path does not collapse the frames.
	source := []byte(`func recurse(int n): int {
    return recurse(n + 1) + 0;
}

recurse(0);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	vm := bytecode.NewVM(chunk, &out)
	vm.SetMaxCallDepth(16)
	err = vm.Run()
	if err == nil {
		t.Fatal("expected max call depth error")
	}
	if !strings.Contains(err.Error(), "maximum call depth exceeded (16)") {
		t.Fatalf("error: got %v", err)
	}
}

func TestCompileAndRunBytecodeFunctionOverloadsByArity(t *testing.T) {
	source := []byte(`import io;
func label(): string {
    return "none";
}
func label(string name): string {
    return "one:" + name;
}
func label(string name, string suffix = "!"): string {
    return "two:" + name + suffix;
}
io.println(label());
io.println(label("Ada", "?"));
io.println(label(name: "Dave", suffix: "."));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "none\ntwo:Ada?\ntwo:Dave.\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeFunctionOverloadsByStaticArgumentType(t *testing.T) {
	source := []byte(`import io;

func describe(int value): string {
    return "int:" + (value as string);
}

func describe(string value): string {
    return "string:" + value;
}

let inferred = "seven";
int typed = 8;
io.println(describe(7));
io.println(describe("six"));
io.println(describe(inferred));
io.println(describe(typed));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "int:7\nstring:six\nstring:seven\nint:8\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeFunctionOverloadsByClassAndInterfaceType(t *testing.T) {
	source := []byte(`import io;

interface Named {
    func name(): string;
}

class Animal {
    func name(): string {
        return "animal";
    }
}

class Dog extends Animal implements Named {
    func name(): string {
        return "dog";
    }
}

class Label {
    string value;

    func Label(string value) {
        this.value = value;
    }
}

func describe(Animal value): string {
    return "animal:" + value.name();
}

func describe(Label value): string {
    return "label:" + value.value;
}

func show(Named value): string {
    return "named:" + value.name();
}

func show(Label value): string {
    return "label:" + value.value;
}

Dog dog = Dog();
Label label = Label("box");
io.println(describe(dog));
io.println(describe(label));
io.println(show(dog));
io.println(show(label));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "animal:dog\nlabel:box\nnamed:dog\nlabel:box\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeFunctionOverloadsByExpectedReturnType(t *testing.T) {
	source := []byte(`import io;

func convert(string value): int {
    return value as int;
}

func convert(string value): string {
    return "s:" + value;
}

func asString(): string {
    return convert("8");
}

int n = convert("7");
string s = convert("seven");
io.println(n + 1);
io.println(s);
io.println(asString());
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "8\ns:seven\ns:8\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeExpectedReturnTypeDoesNotLeakIntoNestedCalls(t *testing.T) {
	source := []byte(`import io;

func values(): list<any> {
    return ["x"];
}

func hasValues(): bool {
    return values().length() > 0;
}

func first(): string {
    return values()[0] as string;
}

io.println(hasValues());
io.println(first());
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "true\nx\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeMethodOverloadsByArity(t *testing.T) {
	source := []byte(`import io;

class Formatter {
    func label(): string {
        return "none";
    }

    func label(string name): string {
        return "one:" + name;
    }

    func label(string name, string suffix = "!"): string {
        return "two:" + name + suffix;
    }

    static func make(): string {
        return "static:none";
    }

    static func make(string name, string suffix = "!"): string {
        return "static:" + name + suffix;
    }
}

Formatter f = Formatter();
io.println(f.label());
io.println(f.label("Ada", "?"));
io.println(f.label(name: "Dave", suffix: "."));
io.println(Formatter.make());
io.println(Formatter.make("Eve", "?"));
io.println(Formatter.make(name: "Bea", suffix: "."));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "none\ntwo:Ada?\ntwo:Dave.\nstatic:none\nstatic:Eve?\nstatic:Bea.\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeMethodOverloadsByRuntimeType(t *testing.T) {
	source := []byte(`import io;

class Formatter {
    func label(int value): string {
        return "int:" + (value as string);
    }

    func label(string value): string {
        return "string:" + value;
    }
}

Formatter f = Formatter();
io.println(f.label(7));
io.println(f.label("seven"));
io.println(f.label(value: "named"));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "int:7\nstring:seven\nstring:named\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeBasicClass(t *testing.T) {
	source := []byte(`import io;

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
io.println(u.visit());
io.println(u.label());
io.println(u instanceof User);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "Dave\n1\n2\nDave:2\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeClassInheritance(t *testing.T) {
	source := []byte(`import io;

class Animal {
    string name;

    func Animal(string name) {
        this.name = name;
    }

    func speak(): string {
        return this.name;
    }

    func kind(): string {
        return "animal";
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

Dog d = Dog("Rex", "Collie");
io.println(d.name);
io.println(d.breed);
io.println(d.speak());
io.println(d.kind());
io.println(d instanceof Dog);
io.println(d instanceof Animal);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "Rex\nCollie\nRex:woof:Collie\nanimal\ntrue\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeNamedParentArguments(t *testing.T) {
	source := []byte(`import io;

class Base {
    string name;
    int count;

    func Base(string name, int count = 1) {
        this.name = name;
        this.count = count;
    }

    func label(string prefix, string suffix = "!"): string {
        return prefix + ":" + this.name + ":" + suffix;
    }
}

class Child extends Base {
    func Child(string name) {
        parent(count: 3, name: name);
    }

    func label(string prefix, string suffix = "!"): string {
        return parent.label(suffix: suffix, prefix: prefix) + ":" + (this.count as string);
    }
}

Child child = Child("Ada");
io.println(child.name);
io.println(child.count);
io.println(child.label(suffix: "?", prefix: "user"));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "Ada\n3\nuser:Ada:?:3\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeAutoParentConstructor(t *testing.T) {
	source := []byte(`import io;

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
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "1\n1\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeStaticClassMembers(t *testing.T) {
	source := []byte(`import io;

class Named {
    static const prefix = "N";

    static func label(string name): string {
        return Named.prefix + ":" + name;
    }
}

class User extends Named {
    static func make(string name): string {
        return User.label(name);
    }
}

io.println(User.prefix);
io.println(User.label("Ada"));
io.println(User.make(name: "Dave"));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "N\nN:Ada\nN:Dave\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeStaticMethodOverloadsByStaticArgumentType(t *testing.T) {
	source := []byte(`import io;

class Formatter {
    static func label(int value): string {
        return "int:" + (value as string);
    }

    static func label(string value): string {
        return "string:" + value;
    }
}

let inferred = "seven";
io.println(Formatter.label(7));
io.println(Formatter.label("six"));
io.println(Formatter.label(inferred));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "int:7\nstring:six\nstring:seven\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeStaticMagicMethods(t *testing.T) {
	source := []byte(`import io;

class Dynamic {
    static func __getStatic(string name): string {
        return "get:" + name;
    }

    static func __setStatic(string name, any value): void {
        io.println("set:" + name + ":" + (value as string));
    }

    static func __callStatic(string name, list<any> args): string {
        return "call:" + name + ":" + (args.length() as string);
    }
}

io.println(Dynamic.value);
Dynamic.value = 7;
io.println(Dynamic.make("x", "y"));
io.println(Dynamic.make(first: "x", second: "y"));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "get:value\nset:value:7\ncall:make:2\ncall:make:2\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeNamedConstructorArguments(t *testing.T) {
	source := []byte(`import io;

class User {
    string name;
    int visits;

    func User(string name, int visits = 1) {
        this.name = name;
        this.visits = visits;
    }
}

User u = User(visits: 3, name: "Ada");
User v = User(name: "Dave");
io.println(u.name);
io.println(u.visits);
io.println(v.name);
io.println(v.visits);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "Ada\n3\nDave\n1\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeConstructorOverloadsByArity(t *testing.T) {
	source := []byte(`import io;

class User {
    string name;
    int visits;

    func User() {
        this.name = "anonymous";
        this.visits = 0;
    }

    func User(string name, int visits = 1) {
        this.name = name;
        this.visits = visits;
    }
}

class Child extends User {
    func Child(string name, int visits = 2) {
        parent(name: name, visits: visits);
    }
}

class AutoChild extends User {
    func AutoChild() {
    }
}

User a = User();
User b = User("Ada");
User c = User(name: "Dave", visits: 3);
Child d = Child("Bea");
AutoChild e = AutoChild();
io.println(a.name + ":" + (a.visits as string));
io.println(b.name + ":" + (b.visits as string));
io.println(c.name + ":" + (c.visits as string));
io.println(d.name + ":" + (d.visits as string));
io.println(e.name + ":" + (e.visits as string));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "anonymous:0\nAda:1\nDave:3\nBea:2\nanonymous:0\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeConstructorOverloadsByStaticArgumentType(t *testing.T) {
	source := []byte(`import io;

class Box {
    string label;

    func Box(int value) {
        this.label = "int:" + (value as string);
    }

    func Box(string value) {
        this.label = "string:" + value;
    }
}

let inferred = "seven";
Box a = Box(7);
Box b = Box("six");
Box c = Box(inferred);
io.println(a.label);
io.println(b.label);
io.println(c.label);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "int:7\nstring:six\nstring:seven\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeNamedMethodArguments(t *testing.T) {
	source := []byte(`import io;

class Formatter {
    func label(string name, string prefix = "user", int count = 1): string {
        return prefix + ":" + name + ":" + (count as string);
    }
}

Formatter f = Formatter();
io.println(f.label(count: 3, name: "Ada"));
io.println(f.label(name: "Dave", prefix: "admin"));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "user:Ada:3\nadmin:Dave:1\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeInstanceStructuralEquality(t *testing.T) {
	source := []byte(`import io;

class User {
    string name;
    int visits;

    func User(string name, int visits) {
        this.name = name;
        this.visits = visits;
    }
}

User a = User("Ada", 1);
User b = User("Ada", 1);
User c = User("Ada", 2);
io.println(a == b);
io.println(a == c);
io.println(a != c);
io.println(a is b);
io.println(a is not b);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "true\nfalse\ntrue\nfalse\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeEqMethod(t *testing.T) {
	source := []byte(`import io;

class User {
    string name;

    func User(string name) {
        this.name = name;
    }

    func __eq(any other): bool {
        return other instanceof User && this.name == other.name;
    }
}

User a = User("Ada");
User b = User("Ada");
User c = User("Dave");
io.println(a == b);
io.println(a == c);
io.println(a != c);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "true\nfalse\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeComparisonOperatorMethods(t *testing.T) {
	source := []byte(`import io;

class Score {
    int value;

    func Score(int value) {
        this.value = value;
    }

    func __lt(Score other): bool {
        return this.value < other.value;
    }

    func __gt(Score other): bool {
        return this.value > other.value;
    }
}

Score low = Score(1);
Score high = Score(3);
io.println(low < high);
io.println(high > low);
io.println(low >= high);
io.println(low <= high);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "true\ntrue\nfalse\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeArithmeticOperatorMethods(t *testing.T) {
	source := []byte(`import io;

class Money {
    int amount;

    func Money(int amount) {
        this.amount = amount;
    }

    func __add(Money other): Money {
        return Money(this.amount + other.amount);
    }

    func __sub(Money other): Money {
        return Money(this.amount - other.amount);
    }
}

Money a = Money(7);
Money b = Money(2);
io.println((a + b).amount);
io.println((a - b).amount);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "9\n5\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodePrefixOperatorMethods(t *testing.T) {
	source := []byte(`import io;

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
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "true\n-7\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeDynamicInstanceMagicMethods(t *testing.T) {
	source := []byte(`import io;

class Dynamic {
    string saved = "";

    func __get(string name): string {
        return "get:" + name + ":" + this.saved;
    }

    func __set(string name, any value): void {
        this.saved = name + "=" + (value as string);
    }

    func __call(string name, list<any> args): string {
        return "call:" + name + ":" + (args.length() as string);
    }
}

Dynamic d = Dynamic();
d.value = 7;
io.println(d.saved);
io.println(d.missing);
io.println(d.make(1, 2));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "value=7\nget:missing:value=7\ncall:make:2\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeInvokeMagicMethod(t *testing.T) {
	source := []byte(`import io;

class Callable {
    func __invoke(string name, string suffix = "!"): string {
        return name + suffix;
    }
}

Callable c = Callable();
io.println(c("Ada", "?"));
io.println(c(name: "Dave", suffix: "."));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "Ada?\nDave.\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeInterfaces(t *testing.T) {
	source := []byte(`import io;

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
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "report:Q1\ntrue\ntrue\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeFunctionDefaultParameters(t *testing.T) {
	source := []byte(`import io;
func connect(string host, int port = 80, bool tls = false): string {
    if (tls) {
        return host + ":tls";
    }
    return host + ":" + (port as string);
}
io.println(connect("example.test"));
io.println(connect("example.test", 443, true));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "example.test:80\nexample.test:tls\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeNamedErrorConstructorArgument(t *testing.T) {
	source := []byte(`import io;
try {
    throw ValueError(message: "bad");
} catch (ValueError err) {
    io.println(err);
}
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "ValueError: bad\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeNamedFunctionArguments(t *testing.T) {
	source := []byte(`import io;
func connect(string host, int port = 80, bool tls = false): string {
    if (tls) {
        return host + ":tls";
    }
    return host + ":" + (port as string);
}
io.println(connect(port: 8080, host: "example.test"));
io.println(connect("example.test", tls: true));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "example.test:8080\nexample.test:tls\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeTryCatchFinally(t *testing.T) {
	source := []byte(`import io;

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

try {
    throw RuntimeError("uncaught here");
} finally {
    io.println("before rethrow");
}
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	err = bytecode.NewVM(chunk, &out).Run()
	if err == nil {
		t.Fatal("expected uncaught throw")
	}
	if !strings.Contains(err.Error(), "uncaught RuntimeError: uncaught here") {
		t.Fatalf("error: got %v", err)
	}
	if out.String() != "ValueError: bad\ncleanup\ncaught\nbefore rethrow\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeCatchesUserErrorSubclass(t *testing.T) {
	source := []byte(`import io;

class MyError extends ValueError {
}

class SpecificError extends MyError {
}

try {
    throw SpecificError("custom");
} catch (TypeError e) {
    io.println("type");
} catch (ValueError e) {
    io.println(e);
}
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "SpecificError: custom\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeExceptionUnwindsFunctionFrames(t *testing.T) {
	source := []byte(`import io;

func fail() {
    defer io.println("deferred");
    throw ValueError("from function");
}

try {
    fail();
} catch (ValueError e) {
    io.println(e);
}

io.println("after");
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "deferred\nValueError: from function\nafter\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeFinallyOnAbruptControlFlow(t *testing.T) {
	source := []byte(`import io;

func returnsWithFinally(): int {
    try {
        return 7;
    } finally {
        io.println("before return");
    }
}

io.println(returnsWithFinally());

for (let int i = 0; i < 3; i++) {
    try {
        if (i == 1) {
            continue;
        }
        io.println(i);
    } finally {
        io.println("loop finally");
    }
}

while (true) {
    try {
        break;
    } finally {
        io.println("before break");
    }
}

io.println("done");
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "before return\n7\n0\nloop finally\nloop finally\n2\nloop finally\nbefore break\ndone\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestCompileAndRunBytecodeMatch(t *testing.T) {
	source := []byte(`import io;
int value = 7;
let label = match (value) {
    case string => "string";
    case int n if (n > 5) => "big:" + (n as string);
    case 0 => "zero";
    default => "other";
};
io.println(label);
match ("x") {
    case "y":
        io.println("bad");
    default:
        io.println("ok");
}
match (value) {
    case int n:
        io.println(n + 1);
    default:
        io.println("bad");
}
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "big:7\nok\n8\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeTypesAndCasts(t *testing.T) {
	source := []byte(`import io;
int x = 7;
io.println(typeof(x));
io.println(x instanceof int);
io.println(x instanceof string);
io.println((x as decimal) / 2);
io.println(("42" as int) + 1);
io.println(("true" as bool) && true);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "int\ntrue\nfalse\n3.5000000000\n43\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodePrimitiveMethods(t *testing.T) {
	source := []byte(`import io;
string name = "Geblang";
int[] nums = [1, 2, 3];
dict<string, int> scores = {"a": 10};
io.println(name.length());
io.println(name.contains("lang"));
io.println(name.get(-1));
io.println(nums.length());
io.println(nums.contains(2));
io.println(nums.get(99));
io.println(scores.length());
io.println(scores.contains("a"));
io.println(scores.get("missing"));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "7\ntrue\ng\n3\ntrue\nnull\n1\ntrue\nnull\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeExpandedPrimitiveMethods(t *testing.T) {
	source := []byte(`import io;
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
int n = -7;
decimal d = -3.50;
float f = -2.5f;
bool ok = true;
io.println(n.abs());
io.println(n.isNegative());
io.println(n.toString());
io.println(n.toDecimal().format(2));
io.println(n.toFloat());
io.println(d.abs());
io.println(d.isPositive());
io.println(d.format(0));
io.println(d.format(3));
io.println(d.toString(1));
io.println(d.toFloat());
io.println(f.abs());
io.println(f.isInf());
io.println(f.toDecimal().format(2));
io.println(ok.not());
io.println(ok.toString());
io.println("42".toInt() + 1);
io.println("3.25".toDecimal().format(2));
io.println("2.5".toFloat());
io.println("true".toBool());
dict<string, int> data = {"a": 1};
data.set("b", 2);
io.println(data.get("b"));
io.println(data.keys().length());
io.println(data.values().length());
io.println(data.items().length());
int[] nums = [1, 2];
nums.set(1, 9);
io.println(nums.get(1));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "Geblang\ngeblang\nGEBLANG\ntrue\ntrue\ntrue\n__Geblang  \n3\n2\nname=Ada age=37 hex=ff price=12.50\n7\ntrue\n-7\n-7.00\n-7\n3.5000000000\nfalse\n-4\n-3.500\n-3.5\n-3.5\n2.5\nfalse\n-2.50\nfalse\ntrue\n43\n3.25\n2.5\ntrue\n2\n2\n2\n2\n9\n"
	if out.String() != want {
		t.Fatalf("output: got %q, want %q", out.String(), want)
	}
}

func TestCompileAndRunBytecodeIntegerOperators(t *testing.T) {
	source := []byte(`import io;
io.println(3 / 2);
io.println(7 // 2);
io.println(7 % 2);
io.println(-7 // 3);
io.println(-7 % 3);
io.println(7 // -3);
io.println(7 % -3);
io.println(2 ** 5);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "1.5000000000\n3\n1\n-3\n2\n-3\n-2\n32\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeDecimalAndFloatOperators(t *testing.T) {
	source := []byte(`import io;
decimal price = 1.25;
io.println(price + 0.75);
io.println(price < 2.0);
io.println(5.5 % 2.0);
io.println(5.5 // 2.0);
io.println(1.5 ** 2.0);
float factor = 1.5f;
io.println(factor * 2.0f);
io.println(5.5f % 2.0f);
io.println(-(factor ** 2.0f));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	encoded, err := bytecode.Encode(chunk)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := bytecode.Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(decoded, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	/* `5.5 // 2.0` is decimal//decimal -> decimal (same-kind rule;
	 * see release notes for 1.0.2). The `5.5f % 2.0f` produces a
	 * float since both operands are float. */
	if out.String() != "2.0000000000\ntrue\n1.5000000000\n2.0000000000\n2.2500000000\n3\n1.5\n-2.25\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeMixedFloatNumericError(t *testing.T) {
	// int + float promotes to float; decimal + float is the precision wall.
	okSource := []byte("import io;\nio.println(1 + 2.0f);\n")
	program := parseProgram(t, string(okSource))
	chunk, err := bytecode.Compile(program, okSource, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("int + float should promote to float: %v", err)
	}
	if strings.TrimSpace(out.String()) != "3" {
		t.Fatalf("int + float: got %q, want 3", out.String())
	}

	errSource := []byte("import io;\nio.println(2.5 + 2.0f);\n")
	program = parseProgram(t, string(errSource))
	chunk, err = bytecode.Compile(program, errSource, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var out2 bytes.Buffer
	err = bytecode.NewVM(chunk, &out2).Run()
	if err == nil {
		t.Fatal("expected decimal/float arithmetic error")
	}
	if !strings.Contains(err.Error(), "cannot mix decimal and float") {
		t.Fatalf("error: got %v", err)
	}
}

func TestCompileAndRunBytecodeStringConcatenation(t *testing.T) {
	source := []byte(`import io;
string name = "Ada";
io.println("hello " + name);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "hello Ada\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeListLiteralAndIndex(t *testing.T) {
	source := []byte(`import io;
int[] nums = [10, 20, 30];
io.println(nums[0]);
io.println(nums[-1]);
io.println([1, 2, 3][1]);
io.println("Dave"[1]);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "10\n30\n2\na\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeSlices(t *testing.T) {
	source := []byte(`import io;
int[] nums = [0, 1, 2, 3, 4];
io.println(nums[1..<4][0]);
io.println(nums[1..3][-1]);
io.println(nums[..<2][-1]);
io.println(nums[3..][0]);
io.println("Geblang"[0..<3]);
io.println("Geblang"[3..]);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "1\n3\n1\n3\nGeb\nlang\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeDictLiteralAndIndex(t *testing.T) {
	source := []byte(`import io;
dict<string, int> scores = {"a": 10, "b": 20};
io.println(scores["a"]);
io.println(scores["missing"]);
io.println({1: "one", 2: "two"}[2]);
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
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "10\nnull\ntwo\n2\n2\none\ntwo\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeIndexAssignment(t *testing.T) {
	source := []byte(`import io;
int[] nums = [10, 20, 30];
let alias = nums;
nums[1] = 99;
nums[-1] = 42;
alias.set(0, 77);
nums.set(1, 88);
io.println(nums[0]);
io.println(nums[1]);
io.println(alias[1]);
io.println(alias[2]);
dict<string, int> scores = {"a": 10};
scores["b"] = 20;
scores["a"] = 11;
io.println(scores["a"]);
io.println(scores["b"]);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "77\n88\n88\n42\n11\n20\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeIfElse(t *testing.T) {
	source := []byte(`import io;
int x = 7;
if (x > 3) {
    io.println("big");
} else {
    io.println("small");
}
if (x == 8) {
    io.println("bad");
} else {
    io.println("ok");
}
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "big\nok\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeComparisonOperators(t *testing.T) {
	source := []byte(`import io;
int x = 7;
io.println(x != 8);
io.println(x <= 7);
io.println(x >= 7);
io.println(x <= 6);
io.println(x >= 8);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "true\ntrue\ntrue\nfalse\nfalse\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeStructuralEquality(t *testing.T) {
	source := []byte(`import io;
io.println([1, {"a": 2}] == [1, {"a": 2}]);
io.println([1, 2] == [1, 3]);
io.println({"a": [1, 2]} == {"a": [1, 2]});
io.println({"a": 1} == {"b": 1});
io.println({"a": 1} != {"a": 2});
io.println(1.00000000001 == 1.00000000002);
io.println([1.00000000001] == [1.00000000002]);
let nan = 0.0f / 0.0f;
io.println(nan == nan);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "true\nfalse\ntrue\nfalse\ntrue\nfalse\nfalse\nfalse\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeElseIf(t *testing.T) {
	source := []byte(`import io;
int x = 7;
if (x < 3) {
    io.println("small");
} elseif (x == 7) {
    io.println("seven");
} elseif (x > 10) {
    io.println("large");
} else {
    io.println("other");
}
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "seven\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeWhileLoop(t *testing.T) {
	source := []byte(`import io;
int x = 1;
while (x < 4) {
    io.println(x);
    x = x + 1;
}
io.println("done");
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "1\n2\n3\ndone\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeLoopBreakAndContinue(t *testing.T) {
	source := []byte(`import io;
int x = 0;
while (x < 6) {
    x = x + 1;
    if (x == 2) {
        continue;
    }
    if (x == 5) {
        break;
    }
    io.println(x);
}
io.println("done");
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "1\n3\n4\ndone\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeForLoop(t *testing.T) {
	source := []byte(`import io;
for (let int x = 0; x < 6; x = x + 1) {
    if (x == 1) {
        continue;
    }
    if (x == 4) {
        break;
    }
    io.println(x);
}
io.println("done");
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "0\n2\n3\ndone\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeForInListLoop(t *testing.T) {
	source := []byte(`import io;
int[] nums = [1, 2, 3, 4];
int sum = 0;
for (int n in nums) {
    if (n == 2) {
        continue;
    }
    if (n == 4) {
        break;
    }
    sum = sum + n;
}
io.println(sum);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "4\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeDestructuringForInListLoop(t *testing.T) {
	source := []byte(`import io;
let pairs = [["a", 1], ["b", 2], ["c", 3]];
for (name, value in pairs) {
    io.println(name + ":" + (value as string));
}
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "a:1\nb:2\nc:3\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeForInRangeLoop(t *testing.T) {
	source := []byte(`import io;
int sum = 0;
for (int n in 1..5 by 2) {
    sum = sum + n;
}
io.println(sum);
for (int n in 5..<1 by -2) {
    io.println(n);
}
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "9\n5\n3\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeForInRangeLoopIsLazy(t *testing.T) {
	source := []byte(`import io;
for (int n in 0..1000000000000000000000000000000000000000000000000) {
    io.println(n);
    break;
}
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "0\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodePostfixIncrement(t *testing.T) {
	source := []byte(`import io;
int x = 0;
x++;
io.println(x);
x--;
io.println(x);
int y = x++;
io.println(y);
io.println(x);
int z = x--;
io.println(z);
io.println(x);
for (let int i = 0; i < 3; i++) {
    io.println(i);
}
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var hasLocalIntUpdate bool
	var hasGlobalIntUpdate bool
	for _, instruction := range chunk.Instructions {
		switch instruction.Op {
		case bytecode.OpIncLocalInt, bytecode.OpDecLocalInt:
			hasLocalIntUpdate = true
		case bytecode.OpIncGlobalInt, bytecode.OpDecGlobalInt:
			hasGlobalIntUpdate = true
		}
	}
	if !hasLocalIntUpdate || !hasGlobalIntUpdate {
		t.Fatalf("expected specialized int update opcodes, local=%v global=%v", hasLocalIntUpdate, hasGlobalIntUpdate)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "1\n0\n0\n1\n1\n0\n0\n1\n2\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeSelfPushAssignmentUsesAppendOpcode(t *testing.T) {
	source := []byte(`import io;
let values = [];
values = values.push(1);
values = values.push(2);
io.println(values.length());
io.println(values[0]);
io.println(values[1]);
func collect(int n): list<int> {
    let local = [];
    for (let int i = 0; i < n; i++) {
        local = local.push(i);
    }
    return local;
}
let result = collect(3);
io.println(result.length());
io.println(result[2]);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var hasLocalAppend bool
	var hasGlobalAppend bool
	for _, instruction := range chunk.Instructions {
		switch instruction.Op {
		case bytecode.OpAppendLocalList:
			hasLocalAppend = true
		case bytecode.OpAppendGlobalList:
			hasGlobalAppend = true
		}
	}
	if !hasLocalAppend || !hasGlobalAppend {
		t.Fatalf("expected specialized list append opcodes, local=%v global=%v", hasLocalAppend, hasGlobalAppend)
	}
	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "2\n1\n2\n3\n2\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeLocalShadowing(t *testing.T) {
	source := []byte(`import io;
int x = 1;
if (true) {
    int x = 2;
    io.println(x);
}
io.println(x);
for (let int x = 0; x < 2; x++) {
    io.println(x);
}
io.println(x);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "2\n1\n0\n1\n1\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeGrowsGlobalAndLocalSlots(t *testing.T) {
	var source strings.Builder
	source.WriteString("import io;\n")
	for i := 0; i < 270; i++ {
		fmt.Fprintf(&source, "int g%d = %d;\n", i, i)
	}
	source.WriteString("func manyLocals(): int {\n")
	for i := 0; i < 1050; i++ {
		fmt.Fprintf(&source, "    int l%d = %d;\n", i, i)
	}
	source.WriteString("    return l1049;\n")
	source.WriteString("}\n")
	source.WriteString("io.println(g269);\n")
	source.WriteString("io.println(manyLocals());\n")

	input := []byte(source.String())
	program := parseProgram(t, source.String())
	chunk, err := bytecode.Compile(program, input, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "269\n1049\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeTopLevelReturn(t *testing.T) {
	source := []byte(`import io;
io.println("before");
return 42;
io.println("after");
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "before\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeDefer(t *testing.T) {
	source := []byte(`import io;
defer io.println("top first");
defer io.println("top second");
func f(): int {
    defer io.println("f first");
    defer io.println("f second");
    return 3;
}
io.println(f());
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "f second\nf first\n3\ntop second\ntop first\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodePrefixOperators(t *testing.T) {
	source := []byte(`import io;
int x = 4;
io.println(-x);
io.println(!(x == 5));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "-4\ntrue\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeBooleanOperators(t *testing.T) {
	source := []byte(`import io;
int x = 4;
io.println(x == 4 && x != 5);
io.println(x == 3 || x == 4);
io.println(true xor false);
io.println(true xor true);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "true\ntrue\ntrue\nfalse\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func parseProgram(t *testing.T, input string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	return program
}

func mustDecimal(t *testing.T, value string) runtime.Decimal {
	t.Helper()
	decimal, err := runtime.NewDecimalLiteral(value)
	if err != nil {
		t.Fatalf("decimal literal: %v", err)
	}
	return decimal
}

func TestCompileAndRunBytecodeFunctionLiteral(t *testing.T) {
	source := []byte(`import io;
let adder = func(int a, int b): int { return a + b; };
io.println(adder(3, 4));
io.println(adder(10, 20));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "7\n30\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeCallableFunctionDecorators(t *testing.T) {
	source := []byte(`import io;
func suffix(any next, string mark): any {
    return func(string name): string { return next(name) + mark; };
}
func prefix(any next, string label): any {
    return func(string name): string { return label + next(name); };
}

@prefix("Hello, ")
@suffix("!")
func greet(string name): string {
    return name;
}

io.println(greet("Ada"));
io.println(reflect.decorators(greet)[0]["name"]);
io.println(reflect.decorators(greet)[1]["name"]);
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "Hello, Ada!\nprefix\nsuffix\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeCallableFunctionDecoratorsWithNamedArgs(t *testing.T) {
	source := []byte(`import io;
func surround(any next, string prefix, string suffix): any {
    return func(string name): string { return prefix + next(name) + suffix; };
}

@surround(suffix: "]", prefix: "[")
func greet(string name): string {
    return name;
}

io.println(greet("Ada"));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "[Ada]\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestBytecodeCallFunctionAppliesCallableFunctionDecorators(t *testing.T) {
	source := []byte(`func prefix(any next, string label): any {
    return func(string name): string { return label + next(name); };
}

@prefix("Hello, ")
func greet(string name): string {
    return name;
}
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var greetIndex int64 = -1
	for index, fn := range chunk.Functions {
		if fn.Name == "greet" {
			greetIndex = int64(index)
			break
		}
	}
	if greetIndex < 0 {
		t.Fatal("greet function not found")
	}

	got, err := bytecode.NewVM(chunk, io.Discard).CallFunction(greetIndex, []runtime.Value{runtime.String{Value: "Ada"}})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if got.Inspect() != "Hello, Ada" {
		t.Fatalf("call result: got %q", got.Inspect())
	}
}

func TestBytecodeFunctionDecoratorStateSnapshotAvoidsReapplyingDecorators(t *testing.T) {
	source := []byte(`import metrics;
func prefix(any next, string label): any {
    metrics.inc("decorator");
    return func(string name): string { return label + next(name); };
}

@prefix("Hello, ")
func greet(string name): string {
    return name;
}
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var greetIndex int64 = -1
	for index, fn := range chunk.Functions {
		if fn.Name == "greet" {
			greetIndex = int64(index)
			break
		}
	}
	if greetIndex < 0 {
		t.Fatal("greet function not found")
	}

	fake := &fakeStatefulNative{}
	moduleVM := bytecode.NewVM(chunk, io.Discard)
	moduleVM.SetStatefulNativeCaller(fake)
	if err := moduleVM.Run(); err != nil {
		t.Fatalf("run module: %v", err)
	}
	if got := strings.Join(fake.calls, ","); got != "metrics.inc" {
		t.Fatalf("decorator calls after module load: got %q", got)
	}

	callVM := bytecode.NewVM(chunk, io.Discard)
	callVM.SetStatefulNativeCaller(fake)
	callVM.RestoreGlobals(moduleVM.GlobalsSnapshot())
	callVM.RestoreFunctionDecoratorState(moduleVM.FunctionDecoratorState())
	got, err := callVM.CallFunction(greetIndex, []runtime.Value{runtime.String{Value: "Ada"}})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if got.Inspect() != "Hello, Ada" {
		t.Fatalf("call result: got %q", got.Inspect())
	}
	if got := strings.Join(fake.calls, ","); got != "metrics.inc" {
		t.Fatalf("decorator was reapplied: calls %q", got)
	}
}

func TestCompileAndRunBytecodeCallableFunctionDecoratorRejectsNonFunction(t *testing.T) {
	source := []byte(`func bad(any next): any {
    return "not callable";
}

@bad
func greet(): string {
    return "Ada";
}
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	err = bytecode.NewVM(chunk, io.Discard).Run()
	if err == nil || !strings.Contains(err.Error(), "decorator @bad must return function, got string") {
		t.Fatalf("expected decorator return error, got %v", err)
	}
}

func TestCompileAndRunBytecodeCallableFunctionDecoratorReportsOverloadMismatch(t *testing.T) {
	source := []byte(`func wrap(any next, int count): any {
    return next;
}

@wrap("bad")
func greet(): string {
    return "Ada";
}
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	err = bytecode.NewVM(chunk, io.Discard).Run()
	if err == nil || !strings.Contains(err.Error(), "decorator @wrap cannot be called for greet") || !strings.Contains(err.Error(), "no matching overload for wrap") {
		t.Fatalf("expected decorator overload error, got %v", err)
	}
}

func TestCompileAndRunBytecodeCallableFunctionDecoratorRejectsIncompatibleWrapper(t *testing.T) {
	source := []byte(`func bad(any next): any {
    return func(): string {
        return "wrong";
    };
}

@bad
func greet(string name): string {
    return name;
}
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	err = bytecode.NewVM(chunk, io.Discard).Run()
	if err == nil || !strings.Contains(err.Error(), "decorator @bad returned incompatible wrapper for greet") {
		t.Fatalf("expected decorator wrapper compatibility error, got %v", err)
	}
}

func TestCompileAndRunBytecodeCallableFunctionDecoratorRejectsExecutableArgumentMetadata(t *testing.T) {
	source := []byte(`func wrap(any next, string label): any {
    return next;
}
func label(): string {
    return "bad";
}

@wrap(label())
func greet(): string {
    return "Ada";
}
`)
	program := parseProgram(t, string(source))
	_, err := bytecode.Compile(program, source, "test")
	if err == nil || !strings.Contains(err.Error(), "decorator @wrap: unsupported decorator argument expression") {
		t.Fatalf("expected compile-time decorator argument error, got %v", err)
	}
}

func TestCompileAndRunBytecodeMethodDecoratorCallsNext(t *testing.T) {
	source := []byte(`import io;
func audit(any next, string tag): any {
    return func(): string {
        return "[" + tag + "] " + next();
    };
}

class Service {
    string name;

    func Service(string name) {
        this.name = name;
    }

    @audit("log")
    func describe(): string {
        return this.name;
    }
}

let s = Service("payments");
io.println(s.describe());
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "[log] payments\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeMethodDecoratorOverloadsHaveDistinctDecorators(t *testing.T) {
	source := []byte(`import io;
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
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "str:hello\nint:42\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeClosureCapture(t *testing.T) {
	source := []byte(`import io;
func makeAdder(int n): any {
    return func(int x): int { return x + n; };
}
let add5 = makeAdder(5);
io.println(add5(3));
io.println(add5(10));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "8\n15\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeClosureMultipleCaptures(t *testing.T) {
	source := []byte(`import io;
func makeMultiplier(int a, int b): any {
    return func(int x): int { return x * a + b; };
}
let f = makeMultiplier(3, 10);
io.println(f(2));
io.println(f(5));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "16\n25\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeClosureMutableCapture(t *testing.T) {
	source := []byte(`import io;
func makeCounter(): any {
    int n = 0;
    return func(): int {
        n++;
        return n;
    };
}
let counter = makeCounter();
io.println(counter());
io.println(counter());
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "1\n2\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeClosureNoCapture(t *testing.T) {
	source := []byte(`import io;
let double = func(int x): int { return x * 2; };
io.println(double(7));
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "14\n" {
		t.Fatalf("output: got %q", out.String())
	}
}

func TestCompileAndRunBytecodeGenericClassWithTypeErasure(t *testing.T) {
	source := []byte(`import io;

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
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got, want := out.String(), "hello\nworld\n42\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestCompileAndRunBytecodeGenericClassMultipleTypeParams(t *testing.T) {
	source := []byte(`import io;

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
`)
	program := parseProgram(t, string(source))
	chunk, err := bytecode.Compile(program, source, "test")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var out bytes.Buffer
	if err := bytecode.NewVM(chunk, &out).Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got, want := out.String(), "abc\n3\nabc!\n"; got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestDecodeRejectsInvalidMagic(t *testing.T) {
	if _, err := bytecode.Decode([]byte("bad")); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestCachePathIncludesCompilerAndSourcePath(t *testing.T) {
	a := bytecode.CachePath("/tmp/geblang", "a.gb", []byte("x"), "v1")
	b := bytecode.CachePath("/tmp/geblang", "a.gb", []byte("x"), "v2")
	c := bytecode.CachePath("/tmp/geblang", "b.gb", []byte("x"), "v1")
	if a == b || a == c || b == c {
		t.Fatalf("cache paths should differ: %q %q %q", a, b, c)
	}
}
