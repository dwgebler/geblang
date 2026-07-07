package bytecode

import (
	"errors"
	"fmt"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"strings"
)

// nativePatchController is implemented by the bridge evaluator so the runner can roll back test.mock patches on the stateful registry (stateful natives dispatch through it) alongside the VM's own.
type nativePatchController interface {
	NativeSnapshot() map[string]native.Function
	RestoreNatives(snapshot map[string]native.Function)
}

// cleanTestError mirrors the evaluator's thrownError: Error() is the clean Inspect() message, and the carried runtime.Error supplies the full frame trace, so VM-runner failures read the same as the evaluator instead of the rendered "uncaught ..." blob.
type cleanTestError struct{ err runtime.Error }

func (e cleanTestError) Error() string { return e.err.Inspect() }

// normalizeTestError converts a VM boundary fault (typed throw or plain runtime fault) into a cleanTestError; other errors pass through unchanged. A method entered directly from the runner has no in-VM call instruction, so topLevelLine (the test.run call site) stands in for the otherwise-absent top-level frame line, matching the evaluator.
func normalizeTestError(err error, topLevelLine int) error {
	if err == nil {
		return nil
	}
	if fault, ok := boundaryFaultToError(err); ok {
		if fault.TopLevelLine == 0 {
			fault.TopLevelLine = topLevelLine
		}
		return cleanTestError{err: fault}
	}
	return err
}

// testFailureWithTrace mirrors the evaluator's failureWithTrace: clean message plus the thrown error's full frame trace.
func testFailureWithTrace(err error) string {
	msg := err.Error()
	var clean cleanTestError
	if errors.As(err, &clean) {
		if t := clean.err.FramesTrace(); t != "" {
			return msg + "\n" + t
		}
	}
	return msg
}

func (vm *VM) hasTestAncestor(classInfo ClassInfo) bool {
	if classInfo.ParentIndex >= 0 && int(classInfo.ParentIndex) < len(vm.chunk.Classes) {
		return vm.hasTestAncestor(vm.chunk.Classes[classInfo.ParentIndex])
	}
	return strings.EqualFold(classInfo.ParentName, "test.Test")
}

func (vm *VM) assertThrowsOfImpl(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("Test.assertThrowsOf expects (callable, classOrName[, expectedSubstring])")
	}
	expectedClass, err := classNameFromArgValue(args[1])
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
	_, err = vm.callCallable(args[0], nil)
	if err == nil {
		return nil, fmt.Errorf("expected callable to throw %s, but it returned normally", expectedClass)
	}
	actualClass := extractThrownErrorClass(err)
	if !vm.errorTypeMatchesClass(actualClass, expectedClass) {
		return nil, fmt.Errorf("expected %s, got %s: %s", expectedClass, actualClass, err.Error())
	}
	if expectedSub != "" && !strings.Contains(err.Error(), expectedSub) {
		return nil, fmt.Errorf("expected error containing %q, got %q", expectedSub, err.Error())
	}
	return runtime.Null{}, nil
}

func classNameFromArgValue(v runtime.Value) (string, error) {
	switch x := v.(type) {
	case runtime.String:
		return x.Value, nil
	case runtime.BytecodeClass:
		return x.Name, nil
	}
	return "", fmt.Errorf("expected class value or class name string, got %s", v.TypeName())
}

func extractThrownErrorClass(err error) string {
	var typed runtime.TypedError
	if errors.As(err, &typed) {
		return typed.ErrorClass()
	}
	return runtime.RecoverableErrorClass(err)
}

// errorTypeMatchesClass mirrors the evaluator's errorTypeMatches
// but walks the chunk's class table for user-defined error
// hierarchies plus the built-in error chain for system classes.
func (vm *VM) errorTypeMatchesClass(actual, target string) bool {
	if target == "" || target == "Error" {
		// FatalError is its own tier, not an Error.
		return actual != "FatalError"
	}
	if actual == target {
		return true
	}
	for current := actual; current != ""; {
		if current == target {
			return true
		}
		next, ok := vm.lookupErrorParent(current)
		if !ok {
			break
		}
		current = next
	}
	return false
}

func (vm *VM) lookupErrorParent(class string) (string, bool) {
	for _, c := range vm.chunk.Classes {
		if c.Name == class {
			if c.ParentName != "" {
				return c.ParentName, true
			}
			break
		}
	}
	switch class {
	case "RuntimeError", "TypeError", "ValueError", "IOError", "ParseError", "MatchError", "ImmutableError", "PermissionError", "AssertionError":
		return "Error", true
	}
	return "", false
}

// assertThrowsImpl mirrors the evaluator's assertThrows so VM-mode
// tests can use the same helper. Signature:
// assertThrows(callable) or assertThrows(callable, expectedSubstring).
func (vm *VM) assertThrowsImpl(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("Test.assertThrows expects (callable[, expectedSubstring])")
	}
	var expectedSub string
	if len(args) == 2 {
		s, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("Test.assertThrows: second argument must be a string substring")
		}
		expectedSub = s.Value
	}
	_, err := vm.callCallable(args[0], nil)
	if err == nil {
		return nil, fmt.Errorf("expected callable to throw, but it returned normally")
	}
	if expectedSub != "" && !strings.Contains(err.Error(), expectedSub) {
		return nil, fmt.Errorf("expected error containing %q, got %q", expectedSub, err.Error())
	}
	return runtime.Null{}, nil
}

// HasInstanceMethod reports whether instance has a method with the given name,
// including methods inherited from a builtin parent class like test.Test.
func (vm *VM) HasInstanceMethod(instance *runtime.Instance, name string) bool {
	classInfo, ok := vm.classInfo(instance.Class.Name)
	if !ok {
		return false
	}
	if _, ok := vm.lookupMethod(classInfo, name); ok {
		return true
	}
	_, handled, _ := vm.callBuiltinParentMethod(classInfo, instance, name, nil)
	return handled
}

// CallInstanceMethod calls a named method on instance via the VM.
func (vm *VM) CallInstanceMethod(instance *runtime.Instance, name string, args []runtime.Value) (runtime.Value, error) {
	return vm.CallMethod(instance, name, args)
}

// RunTestClass runs all @test-decorated methods on a bytecode class and returns a result dict.
// PatchNative installs a registry override so subsequent calls
// to `module.name` dispatch through `fn` instead of the originally
// registered native. Used by test.mock; the evaluator pairs this
// with NativeSnapshot / RestoreNatives so patches roll back at
// @test method boundaries.
func (vm *VM) PatchNative(module, name string, fn native.Function) {
	vm.natives.Patch(module, name, fn)
	vm.invalidateNativeCache()
}

// UnpatchNative removes a single patch.
func (vm *VM) UnpatchNative(module, name string) {
	vm.natives.Unpatch(module, name)
	vm.invalidateNativeCache()
}

// NativeSnapshot returns the active patch map.
func (vm *VM) NativeSnapshot() map[string]native.Function {
	return vm.natives.Snapshot()
}

// RestoreNatives replaces the active patch map; nil clears every patch.
func (vm *VM) RestoreNatives(snapshot map[string]native.Function) {
	vm.natives.Restore(snapshot)
	vm.invalidateNativeCache()
}

// invalidateNativeCache drops the per-name fast path so a patch change is not bypassed by a stale entry.
func (vm *VM) invalidateNativeCache() {
	for i := range vm.nativeCache {
		vm.nativeCache[i] = nil
	}
}

func (vm *VM) RunTestClass(classIndex int64, tagFilter []string) (runtime.Value, error) {
	if classIndex < 0 || int(classIndex) >= len(vm.chunk.Classes) {
		return nil, fmt.Errorf("class index out of range")
	}
	classInfo := vm.chunk.Classes[classIndex]

	// The runner is entered from the test.run native, whose call-site line is the top-level frame's line for every failure trace it produces.
	topLevelLine := vm.nativeCallLine

	tagSet := map[string]bool{}
	for _, t := range tagFilter {
		tagSet[strings.ToLower(t)] = true
	}

	// callKey is the lookup key; displayName is used in failure messages.
	type testMethod struct {
		callKey     string
		displayName string
		skip        bool
		skipReason  string
	}
	var testMethods []testMethod
	seenMethods := map[string]bool{}

	var collectMethods func(info ClassInfo)
	collectMethods = func(info ClassInfo) {
		for methodKey, indices := range info.Methods {
			if seenMethods[methodKey] {
				continue
			}
			decs := info.MethodDecorators[methodKey]
			hasTest := false
			for _, dec := range decs {
				if strings.EqualFold(dec.Name, "test") {
					hasTest = true
					break
				}
			}
			if !hasTest {
				continue
			}
			if len(tagSet) > 0 {
				hasTag := false
				for _, dec := range decs {
					if strings.EqualFold(dec.Name, "tag") {
						for _, arg := range dec.Args {
							if s, ok := arg.(runtime.String); ok && tagSet[strings.ToLower(s.Value)] {
								hasTag = true
							}
						}
					}
				}
				if !hasTag {
					continue
				}
			}
			seenMethods[methodKey] = true
			displayName := methodKey
			if len(indices) > 0 && indices[0] >= 0 && int(indices[0]) < len(vm.chunk.Functions) {
				if n := vm.chunk.Functions[indices[0]].Name; n != "" {
					// Function names are stored as "ClassName.methodname" - strip class prefix.
					if dotIdx := strings.LastIndex(n, "."); dotIdx >= 0 {
						displayName = n[dotIdx+1:]
					} else {
						displayName = n
					}
				}
			}
			skip := false
			skipReason := ""
			for _, dec := range decs {
				if strings.EqualFold(dec.Name, "skip") {
					skip = true
					if len(dec.Args) > 0 {
						if s, ok := dec.Args[0].(runtime.String); ok {
							skipReason = s.Value
						}
					}
				}
			}
			testMethods = append(testMethods, testMethod{callKey: methodKey, displayName: displayName, skip: skip, skipReason: skipReason})
		}
		if info.ParentIndex >= 0 && int(info.ParentIndex) < len(vm.chunk.Classes) {
			collectMethods(vm.chunk.Classes[info.ParentIndex])
		}
	}
	collectMethods(classInfo)

	instanceValue, err := vm.ConstructClass(classIndex, nil)
	if err != nil {
		return nil, err
	}
	instance, ok := instanceValue.(*runtime.Instance)
	if !ok {
		return nil, fmt.Errorf("ConstructClass did not return an instance")
	}

	hasMethod := func(name string) bool {
		return vm.HasInstanceMethod(instance, name)
	}
	// Enter test methods directly on this VM (not a runWrapper callVM) so they observe normal top-level state: async functions used as values wrap in a Task (the wrapper VM forces syncMode), and test.mock patches on this VM's registry are visible to the body.
	callHook := func(name string) error {
		_, err := vm.CallMethodFast(instance, name, nil)
		return err
	}
	bridge, _ := vm.statefulNative.(nativePatchController)
	// snapshotPatches captures the test.mock overlay on both the VM registry and the bridge evaluator so a method's mocks roll back before the next, matching the evaluator runner.
	snapshotPatches := func() func() {
		vmSnap := vm.NativeSnapshot()
		var bridgeSnap map[string]native.Function
		if bridge != nil {
			bridgeSnap = bridge.NativeSnapshot()
		}
		return func() {
			vm.RestoreNatives(vmSnap)
			if bridge != nil {
				bridge.RestoreNatives(bridgeSnap)
			}
		}
	}

	total := int64(0)
	passed := int64(0)
	failed := int64(0)
	skipped := int64(0)
	failures := []runtime.Value{}
	tests := []runtime.Value{}

	buildTestEntry := func(name string, ok bool, message string) runtime.Value {
		entries := map[string]runtime.DictEntry{}
		k := runtime.String{Value: "name"}
		entries[native.DictKey(k)] = runtime.DictEntry{Key: k, Value: runtime.String{Value: name}}
		k = runtime.String{Value: "passed"}
		entries[native.DictKey(k)] = runtime.DictEntry{Key: k, Value: runtime.Bool{Value: ok}}
		if !ok {
			k = runtime.String{Value: "message"}
			entries[native.DictKey(k)] = runtime.DictEntry{Key: k, Value: runtime.String{Value: message}}
		}
		return runtime.Dict{Entries: entries}
	}

	buildSkippedEntry := func(name string, reason string) runtime.Value {
		entries := map[string]runtime.DictEntry{}
		k := runtime.String{Value: "name"}
		entries[native.DictKey(k)] = runtime.DictEntry{Key: k, Value: runtime.String{Value: name}}
		k = runtime.String{Value: "passed"}
		entries[native.DictKey(k)] = runtime.DictEntry{Key: k, Value: runtime.Bool{Value: false}}
		k = runtime.String{Value: "skipped"}
		entries[native.DictKey(k)] = runtime.DictEntry{Key: k, Value: runtime.Bool{Value: true}}
		k = runtime.String{Value: "message"}
		entries[native.DictKey(k)] = runtime.DictEntry{Key: k, Value: runtime.String{Value: reason}}
		return runtime.Dict{Entries: entries}
	}

	setupFailed := false
	if hasMethod("setupClass") {
		if err := callHook("setupClass"); err != nil {
			err = normalizeTestError(err, topLevelLine)
			setupFailed = true
			failed = int64(len(testMethods))
			for _, m := range testMethods {
				total++
				failures = append(failures, runtime.String{Value: m.displayName + ": setupClass: " + err.Error()})
				tests = append(tests, buildTestEntry(m.displayName, false, "setupClass: "+err.Error()))
			}
		}
	}

	if !setupFailed {
		for _, m := range testMethods {
			total++
			if m.skip {
				skipped++
				tests = append(tests, buildSkippedEntry(m.displayName, m.skipReason))
				continue
			}
			restorePatches := snapshotPatches()
			var bodyErr error
			if hasMethod("setup") {
				bodyErr = callHook("setup")
			}
			if bodyErr == nil {
				_, bodyErr = vm.CallMethodFast(instance, m.callKey, nil)
			}
			var teardownErr error
			if hasMethod("teardown") {
				teardownErr = callHook("teardown")
			}
			restorePatches()
			if bodyErr != nil && teardownErr == nil {
				var thrown vmThrownError
				if errors.As(bodyErr, &thrown) && thrown.err.Class == "TestSkip" {
					skipped++
					tests = append(tests, buildSkippedEntry(m.displayName, thrown.err.Message))
					continue
				}
			}
			bodyErr = normalizeTestError(bodyErr, topLevelLine)
			teardownErr = normalizeTestError(teardownErr, topLevelLine)
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
				failures = append(failures, runtime.String{Value: m.displayName + ": " + testFailureWithTrace(testErr)})
				tests = append(tests, buildTestEntry(m.displayName, false, testErr.Error()))
			} else {
				passed++
				tests = append(tests, buildTestEntry(m.displayName, true, ""))
			}
		}
	}

	if hasMethod("teardownClass") {
		if err := callHook("teardownClass"); err != nil {
			err = normalizeTestError(err, topLevelLine)
			failed++
			failures = append(failures, runtime.String{Value: "teardownClass: " + err.Error()})
			if passed > 0 {
				passed--
			}
			if total == 0 {
				total = 1
			}
		}
	}

	setEntry := func(entries map[string]runtime.DictEntry, key string, val runtime.Value) {
		k := runtime.String{Value: key}
		entries[native.DictKey(k)] = runtime.DictEntry{Key: k, Value: val}
	}
	entries := map[string]runtime.DictEntry{}
	setEntry(entries, "total", runtime.NewInt64(total))
	setEntry(entries, "passed", runtime.NewInt64(passed))
	setEntry(entries, "failed", runtime.NewInt64(failed))
	setEntry(entries, "skipped", runtime.NewInt64(skipped))
	setEntry(entries, "failures", &runtime.List{Elements: failures})
	setEntry(entries, "tests", &runtime.List{Elements: tests})
	return runtime.Dict{Entries: entries}, nil
}
