package evaluator

import (
	"errors"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"strings"
)

func (e *Evaluator) testRun(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, fmt.Errorf("%s expects test class and optional options", call.Callee.String())
	}
	if bytecodeClass, ok := args[0].(runtime.BytecodeClass); ok {
		if e.vmDispatcher == nil {
			return nil, fmt.Errorf("%s requires VM dispatcher for bytecode class", call.Callee.String())
		}
		var tagFilter []string
		if len(args) == 2 {
			options, err := testRunOptionsFromArgs(call, args)
			if err != nil {
				return nil, err
			}
			for tag := range options.tags {
				tagFilter = append(tagFilter, tag)
			}
		}
		return e.vmDispatcher.RunTestClass(bytecodeClass.Index, tagFilter)
	}
	class, ok := args[0].(*runtime.Class)
	if !ok {
		return nil, fmt.Errorf("%s expects a test class", call.Callee.String())
	}
	options, err := testRunOptionsFromArgs(call, args)
	if err != nil {
		return nil, err
	}
	instanceValue, err := e.instantiateClass(class, nil)
	if err != nil {
		return nil, err
	}
	instance := instanceValue.(*runtime.Instance)
	total := int64(0)
	passed := int64(0)
	failed := int64(0)
	skipped := int64(0)
	failures := []runtime.Value{}
	tests := []runtime.Value{}
	methods := filterTestMethods(decoratedMethods(class, "test"), options.tags, options.methods)
	if err := e.applyOptionalTestHook(instance, "setupClass"); err != nil {
		failed = int64(len(methods))
		for _, method := range methods {
			total++
			failures = append(failures, runtime.String{Value: method.Name + ": setupClass: " + err.Error()})
			tests = append(tests, testCaseDict(method.Name, false, "setupClass: "+err.Error()))
		}
	} else {
		for _, method := range methods {
			total++
			if hasDecorator(method.Decorators, "skip") {
				skipped++
				tests = append(tests, testCaseSkipped(method.Name, skipDecoratorReason(method.Decorators)))
				continue
			}
			/* Snapshot test.mock patches before each method so
			 * mocks from one test don't leak into the next. */
			patchSnapshot := e.natives.Snapshot()
			var vmSnapshot map[string]native.Function
			if e.vmDispatcher != nil {
				vmSnapshot = e.vmDispatcher.NativeSnapshot()
			}
			bodyErr := e.applyOptionalTestHook(instance, "setup")
			if bodyErr == nil {
				_, bodyErr = e.applyFunctionWithThis(method, nil, instance)
			}
			teardownErr := e.applyOptionalTestHook(instance, "teardown")
			e.natives.Restore(patchSnapshot)
			if e.vmDispatcher != nil {
				e.vmDispatcher.RestoreNatives(vmSnapshot)
			}
			if bodyErr != nil && teardownErr == nil {
				if rerr := runtime.NewRecoverableError(bodyErr); rerr.Class == "TestSkip" {
					skipped++
					tests = append(tests, testCaseSkipped(method.Name, rerr.Message))
					continue
				}
			}
			testErr := bodyErr
			if teardownErr != nil {
				if testErr != nil {
					testErr = fmt.Errorf("%v; teardown: %w", testErr, teardownErr)
				} else {
					testErr = fmt.Errorf("teardown: %w", teardownErr)
				}
			}
			if testErr != nil {
				failed++
				failures = append(failures, runtime.String{Value: method.Name + ": " + failureWithTrace(testErr)})
				tests = append(tests, testCaseDict(method.Name, false, testErr.Error()))
				continue
			}
			passed++
			tests = append(tests, testCaseDict(method.Name, true, ""))
		}
	}
	if err := e.applyOptionalTestHook(instance, "teardownClass"); err != nil {
		failed++
		failures = append(failures, runtime.String{Value: "teardownClass: " + err.Error()})
		if passed > 0 {
			passed--
		}
		if total == 0 {
			total = 1
		}
	}
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "total", runtime.NewInt64(total))
	putDict(entries, "passed", runtime.NewInt64(passed))
	putDict(entries, "failed", runtime.NewInt64(failed))
	putDict(entries, "skipped", runtime.NewInt64(skipped))
	putDict(entries, "failures", &runtime.List{Elements: failures})
	putDict(entries, "tests", &runtime.List{Elements: tests})
	return runtime.Dict{Entries: entries}, nil
}

// failureWithTrace appends the thrown error's trace to a test-failure line so the default runner output shows where the failure happened.
func failureWithTrace(err error) string {
	msg := err.Error()
	var thrown thrownError
	if errors.As(err, &thrown) {
		if t := thrown.value.FramesTrace(); t != "" {
			return msg + "\n" + t
		}
	}
	return msg
}

// skipDecoratorReason returns the optional string argument of a @Skip decorator.
func skipDecoratorReason(decorators []ast.Decorator) string {
	for _, d := range decorators {
		if !strings.EqualFold(d.Name.Value, "skip") || len(d.Arguments) == 0 {
			continue
		}
		if lit, ok := d.Arguments[0].Value.(*ast.StringLiteral); ok {
			return lit.Value
		}
	}
	return ""
}

// testMock(moduleName, {"fname": callable, ...}) installs patches
// on the registry shared by all native calls so subsequent
// invocations of those module functions dispatch to the user's
// callable instead. Patches roll back automatically at the end
// of each @test method via the snapshot/restore in testRun.
func (e *Evaluator) testMock(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects (moduleName, dict<string, callable>)", call.Callee.String())
	}
	moduleName, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s moduleName must be a string", call.Callee.String())
	}
	replacements, ok := args[1].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s second argument must be a dict<string, callable>", call.Callee.String())
	}
	for _, __dk := range replacements.EntryKeys() {
		entry, _ := replacements.GetEntry(__dk)
		fnameValue, ok := entry.Key.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s dict keys must be strings", call.Callee.String())
		}
		fname := fnameValue.Value
		if _, builtin := e.builtins[moduleName.Value][fname]; !builtin && !e.natives.Has(moduleName.Value, fname) {
			return nil, runtime.ClassifiedError{Class: "ValueError", Message: fmt.Sprintf("%s: %s.%s is not a native function that can be mocked", call.Callee.String(), moduleName.Value, fname)}
		}
		callable := entry.Value
		patch := e.makeMockPatch(call, callable)
		e.natives.Patch(moduleName.Value, fname, patch)
		/* When running with the bytecode VM as the primary engine,
		 * the VM has its own registry; mirror the patch there so
		 * test.mock works on either dispatch path. */
		if e.vmDispatcher != nil {
			e.vmDispatcher.PatchNative(moduleName.Value, fname, patch)
		}
	}
	return runtime.Null{}, nil
}

// testRestore(module, fname) removes a single patch.
func (e *Evaluator) testRestore(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects (moduleName, fname)", call.Callee.String())
	}
	moduleName, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s moduleName must be a string", call.Callee.String())
	}
	fname, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s fname must be a string", call.Callee.String())
	}
	e.natives.Unpatch(moduleName.Value, fname.Value)
	if e.vmDispatcher != nil {
		e.vmDispatcher.UnpatchNative(moduleName.Value, fname.Value)
	}
	return runtime.Null{}, nil
}

// testRestoreAll() clears every active patch. The test runner
// also calls this implicitly between methods.
func (e *Evaluator) testRestoreAll(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s expects no arguments", call.Callee.String())
	}
	e.natives.Restore(nil)
	if e.vmDispatcher != nil {
		e.vmDispatcher.RestoreNatives(nil)
	}
	return runtime.Null{}, nil
}

// makeMockPatch wraps a Geblang callable as a native.Function so
// the registry can dispatch native-call sites through it. The
// callable runs back through the evaluator via applyCallableValue.
func (e *Evaluator) makeMockPatch(call *ast.CallExpression, callable runtime.Value) native.Function {
	return func(args []runtime.Value) (runtime.Value, error) {
		return e.invokeMockCallable(call, callable, args)
	}
}

func (e *Evaluator) invokeMockCallable(call *ast.CallExpression, callable runtime.Value, args []runtime.Value) (runtime.Value, error) {
	switch fn := callable.(type) {
	case runtime.Function:
		return e.applyFunction(fn, args)
	case runtime.OverloadedFunction:
		for _, overload := range fn.Overloads {
			if len(overload.Parameters) == len(args) {
				return e.applyFunction(overload, args)
			}
		}
		return nil, fmt.Errorf("test.mock: no matching overload for %d arguments", len(args))
	}
	return nil, fmt.Errorf("test.mock: replacement is not callable")
}

// testCaseDict builds a per-test result entry with name, passed, and
// (for failures) a message. Used by the runner's verbose output path.
func testCaseDict(name string, passed bool, message string) runtime.Value {
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "name", runtime.String{Value: name})
	putDict(entries, "passed", runtime.Bool{Value: passed})
	if !passed {
		putDict(entries, "message", runtime.String{Value: message})
	}
	return runtime.Dict{Entries: entries}
}

func testCaseSkipped(name string, reason string) runtime.Value {
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "name", runtime.String{Value: name})
	putDict(entries, "passed", runtime.Bool{Value: false})
	putDict(entries, "skipped", runtime.Bool{Value: true})
	putDict(entries, "message", runtime.String{Value: reason})
	return runtime.Dict{Entries: entries}
}

type testRunOptions struct {
	tags    map[string]bool
	methods map[string]bool
}

func testRunOptionsFromArgs(call *ast.CallExpression, args []runtime.Value) (testRunOptions, error) {
	options := testRunOptions{}
	if len(args) == 1 {
		return options, nil
	}
	dict, ok := args[1].(runtime.Dict)
	if !ok {
		return options, fmt.Errorf("%s options must be dict", call.Callee.String())
	}
	if value, ok := dictField(dict, "tags"); ok {
		list, ok := value.(*runtime.List)
		if !ok {
			return options, fmt.Errorf("%s options.tags must be list<string>", call.Callee.String())
		}
		options.tags = map[string]bool{}
		for _, element := range list.Elements {
			tag, ok := element.(runtime.String)
			if !ok {
				return options, fmt.Errorf("%s options.tags must be list<string>", call.Callee.String())
			}
			options.tags[strings.ToLower(tag.Value)] = true
		}
	}
	if value, ok := dictField(dict, "methods"); ok {
		list, ok := value.(*runtime.List)
		if !ok {
			return options, fmt.Errorf("%s options.methods must be list<string>", call.Callee.String())
		}
		options.methods = map[string]bool{}
		for _, element := range list.Elements {
			name, ok := element.(runtime.String)
			if !ok {
				return options, fmt.Errorf("%s options.methods must be list<string>", call.Callee.String())
			}
			options.methods[name.Value] = true
		}
	}
	return options, nil
}

func filterTestMethods(methods []runtime.Function, tags map[string]bool, names map[string]bool) []runtime.Function {
	if len(tags) == 0 && len(names) == 0 {
		return methods
	}
	filtered := []runtime.Function{}
	for _, method := range methods {
		if len(names) > 0 && !names[method.Name] {
			continue
		}
		if len(tags) == 0 {
			filtered = append(filtered, method)
			continue
		}
		for _, tag := range testMethodTags(method) {
			if tags[strings.ToLower(tag)] {
				filtered = append(filtered, method)
				break
			}
		}
	}
	return filtered
}

func testMethodTags(method runtime.Function) []string {
	tags := []string{}
	for _, decorator := range method.Decorators {
		if !strings.EqualFold(decorator.Name.Value, "tag") {
			continue
		}
		for _, arg := range decorator.Arguments {
			if literal, ok := arg.Value.(*ast.StringLiteral); ok {
				tags = append(tags, literal.Value)
			}
		}
	}
	return tags
}

func (e *Evaluator) applyOptionalTestHook(instance *runtime.Instance, name string) error {
	method, ok := lookupMethod(instance.Class, name)
	if !ok {
		return nil
	}
	_, err := e.applyFunctionWithThis(method, nil, instance)
	return err
}

func decoratedMethods(class *runtime.Class, decorator string) []runtime.Function {
	methods := []runtime.Function{}
	seen := map[string]bool{}
	for current := class; current != nil; current = current.Parent {
		for key, overloads := range current.Methods {
			if seen[key] {
				continue
			}
			for _, method := range overloads {
				if !hasDecorator(method.Decorators, decorator) {
					continue
				}
				seen[key] = true
				methods = append(methods, method)
			}
		}
	}
	return methods
}

func hasDecorator(decorators []ast.Decorator, name string) bool {
	for _, decorator := range decorators {
		if strings.EqualFold(decorator.Name.Value, name) {
			return true
		}
	}
	return false
}

func (e *Evaluator) nativeTestAssertion(name string) func(*runtime.Instance, []runtime.Value) (runtime.Value, error) {
	return func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if strings.EqualFold(name, "assertThrows") {
			return e.assertThrowsImpl(args)
		}
		if strings.EqualFold(name, "assertThrowsOf") {
			return e.assertThrowsOfImpl(args)
		}
		value, handled, err := runtime.RunTestAssertion(name, args)
		if !handled {
			return nil, fmt.Errorf("unknown test assertion %s", name)
		}
		return value, err
	}
}

// assertThrowsOfImpl asserts the callable raises an error whose class
// matches `expectedClass` (walking the parent chain like a catch
// clause). The class argument is either a class value or a string
// class name; the latter lets built-in error classes
// (RuntimeError, PermissionError, ...) be referenced without
// requiring them to be reified as runtime values. Optional third
// argument is a substring that must appear in the error message.
func (e *Evaluator) assertThrowsOfImpl(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("Test.assertThrowsOf expects (callable, classOrName[, expectedSubstring])")
	}
	fn, ok := args[0].(runtime.Function)
	if !ok {
		return nil, fmt.Errorf("Test.assertThrowsOf expects a callable as the first argument")
	}
	expectedClass, err := classNameFromValue(args[1])
	if err != nil {
		return nil, fmt.Errorf("Test.assertThrowsOf: %w", err)
	}
	var expectedSub string
	if len(args) == 3 {
		s, ok := args[2].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Test.assertThrowsOf: third argument must be a string substring")
		}
		expectedSub = s.Value
	}
	_, err = e.applyFunction(fn, nil)
	if err == nil {
		return nil, fmt.Errorf("expected callable to throw %s, but it returned normally", expectedClass)
	}
	actualClass := extractErrorClass(err)
	if !e.errorTypeMatches(actualClass, expectedClass) {
		return nil, fmt.Errorf("expected %s, got %s: %s", expectedClass, actualClass, err.Error())
	}
	if expectedSub != "" && !strings.Contains(err.Error(), expectedSub) {
		return nil, fmt.Errorf("expected error containing %q, got %q", expectedSub, err.Error())
	}
	return runtime.Null{}, nil
}

// classNameFromValue projects a class reference into its name.
// Accepts a string (for built-in error classes whose identifiers
// aren't reified as values) or a runtime class value (user-
// defined classes referenced by name in source).
func classNameFromValue(v runtime.Value) (string, error) {
	switch x := v.(type) {
	case runtime.String:
		return x.Value, nil
	case *runtime.Class:
		return x.Name, nil
	case runtime.BytecodeClass:
		return x.Name, nil
	}
	return "", fmt.Errorf("expected class value or class name string, got %s", v.TypeName())
}

// extractErrorClass pulls the carried Geblang class name out of a
// thrown error, falling back to "RuntimeError" when the wrapped
// value doesn't carry an explicit class.
func extractErrorClass(err error) string {
	var typed runtime.TypedError
	if errors.As(err, &typed) {
		return typed.ErrorClass()
	}
	return runtime.RecoverableErrorClass(err)
}

// assertThrowsImpl invokes the callable arg and asserts it raises.
// Signature: assertThrows(callable) or assertThrows(callable, expectedSubstring).
func (e *Evaluator) assertThrowsImpl(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("Test.assertThrows expects (callable[, expectedSubstring])")
	}
	fn, ok := args[0].(runtime.Function)
	if !ok {
		return nil, fmt.Errorf("Test.assertThrows expects a callable as the first argument")
	}
	var expectedSub string
	if len(args) == 2 {
		s, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Test.assertThrows: second argument must be a string substring")
		}
		expectedSub = s.Value
	}
	_, err := e.applyFunction(fn, nil)
	if err == nil {
		return nil, fmt.Errorf("expected callable to throw, but it returned normally")
	}
	if expectedSub != "" && !strings.Contains(err.Error(), expectedSub) {
		return nil, fmt.Errorf("expected error containing %q, got %q", expectedSub, err.Error())
	}
	return runtime.Null{}, nil
}

// withTestPatches routes every builtin module function through the test.mock patch table first.
func withTestPatches(modules map[string]map[string]builtinFunc) map[string]map[string]builtinFunc {
	for module, functions := range modules {
		for name, fn := range functions {
			key, inner := native.Key(module, name), fn
			functions[name] = func(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
				if patch := native.LookupPatch(key); patch != nil {
					return patch(args)
				}
				return inner(call, args)
			}
		}
	}
	return modules
}
