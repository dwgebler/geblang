package evaluator

import (
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"io"
	"sort"
	"strconv"
	"strings"
)

func (e *Evaluator) evalReflectClassesCall(call *ast.CallExpression, env *runtime.Environment) (runtime.Value, error) {
	args, err := e.evalCallArguments(call, env)
	if err != nil {
		return nil, err
	}
	if len(args) != 0 {
		return nil, fmt.Errorf("reflect.classes takes no arguments")
	}
	names := make([]string, 0, len(e.globalClasses))
	for n := range e.globalClasses {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]runtime.Value, 0, len(names))
	for _, n := range names {
		out = append(out, e.globalClasses[n])
	}
	return &runtime.List{Elements: out}, nil
}

func (e *Evaluator) evalReflectLookupCall(call *ast.CallExpression, env *runtime.Environment, name string) (runtime.Value, error) {
	args, err := e.evalCallArguments(call, env)
	if err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("reflect.%s expects exactly one argument", name)
	}
	// `reflect.class` / `reflect.function` / `reflect.module` accept either
	// a name string (legacy) or a value of the appropriate kind. Passing
	// the value itself lets framework code work uniformly with the VM
	// backend, which has always accepted values directly.
	if name == "class" {
		switch v := args[0].(type) {
		case *runtime.Class:
			return v, nil
		case *runtime.Instance:
			if v != nil && v.Class != nil {
				return v.Class, nil
			}
			return runtime.Null{}, nil
		}
	}
	if name == "function" {
		switch v := args[0].(type) {
		case runtime.Function, runtime.OverloadedFunction:
			return v, nil
		}
	}
	if name == "module" {
		if mod, ok := args[0].(*runtime.Module); ok {
			return mod, nil
		}
	}
	targetName, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("reflect.%s argument must be a %s value or name string", name, name)
	}
	value, ok, err := e.reflectLookupValue(targetName.Value, env)
	if err != nil {
		return nil, err
	}
	if !ok && name == "class" {
		// Fall back to the cross-module class registry so framework
		// code can resolve a class by name even when the local env
		// doesn't have it (the user's classes live in their own
		// module's env, but a framework helper imported into the
		// user's program still needs to reflect on them).
		if class, found := e.globalClasses[targetName.Value]; found {
			return class, nil
		}
	}
	if !ok && name == "function" {
		// Mirror the class fallback for an imported module's export (matches the VM).
		if function, found := e.globalFunctions[targetName.Value]; found {
			return function, nil
		}
		if value, found := e.nativeQualifiedFunctionValue(targetName.Value, env); found {
			return value, nil
		}
	}
	if !ok {
		return runtime.Null{}, nil
	}
	switch name {
	case "function":
		switch value := value.(type) {
		case runtime.Function, runtime.OverloadedFunction:
			return value, nil
		default:
			return nil, fmt.Errorf("reflect.function %q is %s, not function", targetName.Value, value.TypeName())
		}
	case "class":
		class, ok := value.(*runtime.Class)
		if !ok {
			return nil, fmt.Errorf("reflect.class %q is %s, not class", targetName.Value, value.TypeName())
		}
		return class, nil
	case "module":
		module, ok := value.(*runtime.Module)
		if !ok {
			return nil, fmt.Errorf("reflect.module %q is %s, not module", targetName.Value, value.TypeName())
		}
		return module, nil
	default:
		return nil, fmt.Errorf("unsupported reflect lookup %s", name)
	}
}

// nativeQualifiedFunctionValue resolves "module.fn" to a pure native
// builtin as a first-class callable, for reflect.function name lookups.
func (e *Evaluator) nativeQualifiedFunctionValue(name string, env *runtime.Environment) (runtime.Value, bool) {
	moduleName, exportName, ok := strings.Cut(name, ".")
	if !ok {
		return nil, false
	}
	moduleValue, found := env.Get(moduleName)
	if !found {
		return nil, false
	}
	module, ok := moduleValue.(*runtime.Module)
	if !ok {
		return nil, false
	}
	canonical := module.Canonical
	if canonical == "" {
		canonical = module.Name
	}
	return e.nativeBuiltinValue(canonical, exportName)
}

func (e *Evaluator) reflectLookupValue(name string, env *runtime.Environment) (runtime.Value, bool, error) {
	if moduleName, exportName, ok := strings.Cut(name, "."); ok {
		moduleValue, valueOK := env.Get(moduleName)
		if !valueOK {
			return nil, false, nil
		}
		module, ok := moduleValue.(*runtime.Module)
		if !ok {
			return nil, false, fmt.Errorf("reflect lookup %q: %s is not a module", name, moduleName)
		}
		value, ok := module.Exports[exportName]
		return value, ok, nil
	}
	value, ok := env.Get(name)
	return value, ok, nil
}

func (e *Evaluator) evalDirCall(call *ast.CallExpression, env *runtime.Environment) (runtime.Value, error) {
	if len(call.Arguments) > 1 {
		return nil, fmt.Errorf("dir expects zero or one argument")
	}
	if len(call.Arguments) == 0 {
		return stringList(env.Names()), nil
	}
	if call.Arguments[0].Name != nil {
		return nil, fmt.Errorf("dir does not accept named arguments")
	}
	if ident, ok := call.Arguments[0].Value.(*ast.Identifier); ok {
		if names, ok := e.dirImportedModule(ident.Value); ok {
			return stringList(names), nil
		}
	}
	value, err := e.evalExpression(call.Arguments[0].Value, env)
	if err != nil {
		return nil, err
	}
	return stringList(dirValue(value)), nil
}

func (e *Evaluator) evalDumpCall(call *ast.CallExpression, env *runtime.Environment) (runtime.Value, error) {
	if len(call.Arguments) != 1 {
		return nil, fmt.Errorf("dump expects exactly one argument")
	}
	if call.Arguments[0].Name != nil {
		return nil, fmt.Errorf("dump does not accept named arguments")
	}
	value, err := e.evalExpression(call.Arguments[0].Value, env)
	if err != nil {
		return nil, err
	}
	return runtime.String{Value: native.DumpValue(value)}, nil
}

// Union of native functions and source exports, since dual-name modules make
// both reachable via `module.<member>`.
func (e *Evaluator) dirImportedModule(alias string) ([]string, bool) {
	canonical, ok := e.importNames[alias]
	if !ok {
		return nil, false
	}
	members := map[string]struct{}{}
	for _, name := range native.ModuleDirNames(canonical, CachedNativeModuleSymbols()) {
		members[name] = struct{}{}
	}
	if path, perr := e.resolveModulePath(canonical); perr == nil && !e.loading[path] {
		if module, err := e.loadUserModule(canonical, alias); err == nil {
			for name := range module.Exports {
				members[name] = struct{}{}
			}
		}
	}
	if len(members) == 0 {
		return nil, false
	}
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, true
}

func (e *Evaluator) builtinModuleTypeExportNames(canonical string) []string {
	switch canonical {
	case "http":
		return []string{"Cookie", "Headers", "Request", "Response"}
	case "test":
		return []string{"Test"}
	case "json":
		return []string{"JsonStreamInterface"}
	case "xml":
		return []string{"XmlStreamInterface"}
	case "yaml":
		return []string{"YamlStreamInterface"}
	case "csv":
		return []string{"CsvStreamInterface"}
	case "log":
		return []string{"LogInterface"}
	default:
		return nil
	}
}

func dirValue(value runtime.Value) []string {
	names := []string{}
	switch value := value.(type) {
	case *runtime.Module:
		for name := range value.Exports {
			names = append(names, name)
		}
	case *runtime.Class:
		seen := map[string]bool{}
		for class := value; class != nil; class = class.Parent {
			for _, field := range class.Fields {
				seen[field.Name] = true
			}
			for name := range class.Methods {
				seen[name] = true
			}
			for name := range class.StaticMethods {
				seen[name] = true
			}
			for name := range class.StaticValues {
				seen[name] = true
			}
		}
		for name := range seen {
			names = append(names, name)
		}
	case *runtime.Instance:
		seen := map[string]bool{}
		for name := range value.Fields {
			seen[name] = true
		}
		for class := value.Class; class != nil; class = class.Parent {
			for _, field := range class.Fields {
				seen[field.Name] = true
			}
			for name := range class.Methods {
				seen[name] = true
			}
		}
		for name := range seen {
			names = append(names, name)
		}
	case runtime.Dict:
		names = primitiveMethodNamesFor("dict")
	case runtime.Set:
		names = primitiveMethodNamesFor("set")
	case *runtime.List:
		names = primitiveMethodNamesFor("list")
	case runtime.String:
		names = primitiveMethodNamesFor("string")
	case runtime.Bytes:
		names = primitiveMethodNamesFor("bytes")
	case runtime.Range:
		names = primitiveMethodNamesFor("range")
	case runtime.SmallInt, runtime.Int:
		names = primitiveMethodNamesFor("int")
	case runtime.Decimal:
		names = primitiveMethodNamesFor("decimal")
	case runtime.Float:
		names = primitiveMethodNamesFor("float")
	case runtime.Bool:
		names = primitiveMethodNamesFor("bool")
	case runtime.NativeObject:
		names = nativeObjectMethods(value.Kind)
	case *runtime.Generator:
		names = append([]string(nil), native.GeneratorMethods...)
	case *runtime.NDArray:
		names = append([]string(nil), native.NDArrayMethods...)
	case *runtime.HtmlNode:
		names = append([]string(nil), native.HtmlNodeMethods...)
	case *runtime.Distribution:
		names = append([]string(nil), native.DistributionMethods...)
	case *runtime.Complex:
		names = append([]string(nil), native.ComplexMethods...)
	case *runtime.DataFrame:
		names = append([]string(nil), native.DataFrameMethods...)
	case *runtime.DFSeries:
		names = append([]string(nil), native.DFSeriesMethods...)
	case *runtime.DFExpr:
		names = append([]string(nil), native.DFExprMethods...)
	case *runtime.DFGroupBy:
		names = append([]string(nil), native.DFGroupByMethods...)
	case runtime.DateTimeInstant:
		names = append([]string(nil), native.DateTimeInstantMethods...)
	case runtime.DateTimeDuration:
		names = append([]string(nil), native.DateTimeDurationMethods...)
	case runtime.DateTimeZone:
		names = append([]string(nil), native.DateTimeZoneMethods...)
	case runtime.Function, runtime.OverloadedFunction:
		names = []string{"call"}
	default:
		names = []string{}
	}
	sort.Strings(names)
	return names
}

func nativeObjectMethods(kind string) []string {
	switch kind {
	case "IOBuffer":
		return []string{"close", "length", "reset", "toString", "write", "writeln"}
	case "IOStream":
		return []string{"close", "read", "readAll", "readBytes", "toString", "write", "writeBytes", "writeln"}
	case "IOCapture":
		return []string{"bytes", "close", "read", "readAll", "readBytes", "reset", "toString", "write", "writeBytes", "writeln"}
	case "JsonReader", "XmlReader", "CsvReader", "YamlReader":
		return []string{"close", "next"}
	default:
		return nil
	}
}

func stringList(names []string) *runtime.List {
	elements := make([]runtime.Value, 0, len(names))
	for _, name := range names {
		elements = append(elements, runtime.String{Value: name})
	}
	return &runtime.List{Elements: elements}
}

func reflectDecorators(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, fmt.Errorf("%s expects value and optional decorator name", call.Callee.String())
	}
	filter, err := optionalDecoratorName(call, args)
	if err != nil {
		return nil, err
	}
	if overloaded, ok := args[0].(runtime.OverloadedFunction); ok {
		return overloadedDecoratorListValue(overloaded, filter)
	}
	decorators, target, err := reflectTargetDecorators(call, args[0])
	if err != nil {
		return nil, err
	}
	return decoratorListValue(decorators, target, filter)
}

func reflectHasDecorator(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects value and decorator name", call.Callee.String())
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s decorator name must be string", call.Callee.String())
	}
	if overloaded, ok := args[0].(runtime.OverloadedFunction); ok {
		for _, overload := range overloaded.Overloads {
			if hasDecorator(overload.Decorators, name.Value) {
				return runtime.Bool{Value: true}, nil
			}
		}
		return runtime.Bool{Value: false}, nil
	}
	decorators, _, err := reflectTargetDecorators(call, args[0])
	if err != nil {
		return nil, err
	}
	return runtime.Bool{Value: hasDecorator(decorators, name.Value)}, nil
}

func reflectDecorator(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects value and decorator name", call.Callee.String())
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s decorator name must be string", call.Callee.String())
	}
	if overloaded, ok := args[0].(runtime.OverloadedFunction); ok {
		for overloadIndex, overload := range overloaded.Overloads {
			target := reflectFunctionTarget(overload)
			for position, decorator := range overload.Decorators {
				if strings.EqualFold(decorator.Name.Value, name.Value) {
					return decoratorValue(decorator, target, position, overloadIndex)
				}
			}
		}
		return runtime.Null{}, nil
	}
	decorators, target, err := reflectTargetDecorators(call, args[0])
	if err != nil {
		return nil, err
	}
	for position, decorator := range decorators {
		if strings.EqualFold(decorator.Name.Value, name.Value) {
			return decoratorValue(decorator, target, position, 0)
		}
	}
	return runtime.Null{}, nil
}

func reflectParameters(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects value", call.Callee.String())
	}
	fn, ok := reflectFunctionMetadataValue(args[0])
	if !ok {
		return nil, fmt.Errorf("%s expects function or method, got %s", call.Callee.String(), args[0].TypeName())
	}
	values := make([]runtime.Value, 0, len(fn.Parameters))
	for _, parameter := range fn.Parameters {
		values = append(values, parameterMetadataValue(parameter))
	}
	return &runtime.List{Elements: values}, nil
}

func reflectReturnType(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects value", call.Callee.String())
	}
	fn, ok := reflectFunctionMetadataValue(args[0])
	if !ok {
		return nil, fmt.Errorf("%s expects function or method, got %s", call.Callee.String(), args[0].TypeName())
	}
	return runtime.String{Value: fn.ReturnType}, nil
}

func reflectDoc(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	doc, ok, err := reflectDocText(call, args)
	if err != nil || !ok {
		return nil, err
	}
	if doc == "" {
		return runtime.Null{}, nil
	}
	return runtime.String{Value: doc}, nil
}

func reflectDocs(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	doc, ok, err := reflectDocText(call, args)
	if err != nil || !ok {
		return nil, err
	}
	if doc == "" {
		return runtime.Null{}, nil
	}
	return docMetadataValue(doc), nil
}

func reflectDocText(call *ast.CallExpression, args []runtime.Value) (string, bool, error) {
	if len(args) != 1 {
		return "", false, fmt.Errorf("%s expects value", call.Callee.String())
	}
	if fn, ok := reflectFunctionMetadataValue(args[0]); ok {
		return fn.Doc, true, nil
	}
	if metadata, err := reflectClassMetadataValue(call, args); err == nil {
		return metadata.Doc, true, nil
	}
	if iface, ok := args[0].(*runtime.Interface); ok {
		return iface.Doc, true, nil
	}
	return "", false, fmt.Errorf("%s expects function, method, class, or interface, got %s", call.Callee.String(), args[0].TypeName())
}

func docMetadataValue(doc string) runtime.Dict {
	lines := strings.Split(strings.ReplaceAll(doc, "\r\n", "\n"), "\n")
	lineValues := make([]runtime.Value, 0, len(lines))
	for _, line := range lines {
		lineValues = append(lineValues, runtime.String{Value: line})
	}
	summary := ""
	summaryIndex := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			summary = strings.TrimSpace(line)
			summaryIndex = i
			break
		}
	}
	body := ""
	if summaryIndex >= 0 && summaryIndex+1 < len(lines) {
		body = strings.TrimSpace(strings.Join(lines[summaryIndex+1:], "\n"))
	}
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "text", runtime.String{Value: doc})
	putDict(entries, "summary", runtime.String{Value: summary})
	putDict(entries, "body", runtime.String{Value: body})
	putDict(entries, "lines", &runtime.List{Elements: lineValues})
	return runtime.Dict{Entries: entries}
}

func reflectExports(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects module", call.Callee.String())
	}
	module, ok := args[0].(*runtime.Module)
	if !ok {
		return nil, fmt.Errorf("%s expects module, got %s", call.Callee.String(), args[0].TypeName())
	}
	return stringList(dirValue(module)), nil
}

func reflectTypeOf(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects value", call.Callee.String())
	}
	return runtime.Type{Name: args[0].TypeName()}, nil
}

// reflectLocation returns the source position of a function or class
// declaration as `{module, line, column}`. Returns null when the
// value carries no recorded location.
func reflectLocation(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects value", call.Callee.String())
	}
	makeDict := func(module string, line, column int64) runtime.Dict {
		entries := map[string]runtime.DictEntry{}
		putDict(entries, "module", runtime.String{Value: module})
		putDict(entries, "line", runtime.NewInt64(line))
		putDict(entries, "column", runtime.NewInt64(column))
		return runtime.Dict{Entries: entries}
	}
	switch v := args[0].(type) {
	case runtime.DecoratorTarget:
		if v.Function != nil && (v.Function.DefLine != 0 || v.Function.DefColumn != 0) {
			return makeDict(v.Function.Module, v.Function.DefLine, v.Function.DefColumn), nil
		}
		if v.Class != nil && (v.Class.DefLine != 0 || v.Class.DefColumn != 0) {
			return makeDict(v.Class.Module, v.Class.DefLine, v.Class.DefColumn), nil
		}
	case runtime.Function:
		if v.DefinitionLine != 0 || v.DefinitionColumn != 0 {
			return makeDict(v.DefinitionModule, int64(v.DefinitionLine), int64(v.DefinitionColumn)), nil
		}
	case *runtime.Class:
		if v != nil && (v.DefinitionLine != 0 || v.DefinitionColumn != 0) {
			return makeDict(v.DefinitionModule, int64(v.DefinitionLine), int64(v.DefinitionColumn)), nil
		}
	case *runtime.Instance:
		if v != nil && v.Class != nil && (v.Class.DefinitionLine != 0 || v.Class.DefinitionColumn != 0) {
			return makeDict(v.Class.DefinitionModule, int64(v.Class.DefinitionLine), int64(v.Class.DefinitionColumn)), nil
		}
	}
	return runtime.Null{}, nil
}

func reflectFields(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects class", call.Callee.String())
	}
	// Prefer structured field metadata - matches the
	// `reflect.parameters` shape (each entry {name, type, nullable,
	// hasDefault}). When the value is a *runtime.Class or an
	// instance we can read the actual Field structs; for
	// bytecode-class or compile-time metadata we fall back to the
	// name-only list wrapped in dicts.
	if cls, ok := classForReflectFields(args[0]); ok && cls != nil {
		entries := make([]runtime.Value, 0, len(cls.Fields))
		for _, field := range cls.Fields {
			fd := map[string]runtime.DictEntry{}
			putDict(fd, "name", runtime.String{Value: field.Name})
			putDict(fd, "type", runtime.String{Value: typeRefToString(field.Type)})
			nullable := false
			if field.Type != nil {
				nullable = field.Type.Nullable
			}
			putDict(fd, "nullable", runtime.Bool{Value: nullable})
			putDict(fd, "hasDefault", runtime.Bool{Value: field.Default != nil})
			if field.Doc == "" {
				putDict(fd, "doc", runtime.Null{})
			} else {
				putDict(fd, "doc", runtime.String{Value: field.Doc})
			}
			decs, derr := decoratorListValue(field.Decorators, "field", "")
			if derr == nil {
				putDict(fd, "decorators", decs)
			}
			entries = append(entries, runtime.Dict{Entries: fd})
		}
		sort.SliceStable(entries, func(i, j int) bool {
			a := entries[i].(runtime.Dict).EntryValue(native.DictKey(runtime.String{Value: "name"})).(runtime.String).Value
			b := entries[j].(runtime.Dict).EntryValue(native.DictKey(runtime.String{Value: "name"})).(runtime.String).Value
			return a < b
		})
		return &runtime.List{Elements: entries}, nil
	}
	metadata, err := reflectClassMetadataValue(call, args)
	if err != nil {
		return nil, err
	}
	entries := make([]runtime.Value, 0, len(metadata.Fields))
	for _, name := range metadata.Fields {
		fd := map[string]runtime.DictEntry{}
		putDict(fd, "name", runtime.String{Value: name})
		putDict(fd, "type", runtime.String{Value: "any"})
		putDict(fd, "nullable", runtime.Bool{Value: false})
		putDict(fd, "hasDefault", runtime.Bool{Value: false})
		entries = append(entries, runtime.Dict{Entries: fd})
	}
	return &runtime.List{Elements: entries}, nil
}

func classForReflectFields(v runtime.Value) (*runtime.Class, bool) {
	switch x := v.(type) {
	case *runtime.Class:
		return x, true
	case *runtime.Instance:
		if x != nil {
			return x.Class, true
		}
	}
	return nil, false
}

func typeRefToString(t *ast.TypeRef) string {
	if t == nil {
		return "any"
	}
	return t.String()
}

func reflectMethods(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	metadata, err := reflectClassMetadataValue(call, args)
	if err != nil {
		return nil, err
	}
	methods := append([]string(nil), metadata.Methods...)
	sort.Strings(methods)
	return stringList(methods), nil
}

func reflectStaticMethods(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	metadata, err := reflectClassMetadataValue(call, args)
	if err != nil {
		return nil, err
	}
	return stringList(metadata.StaticMethods), nil
}

func reflectParent(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	metadata, err := reflectClassMetadataValue(call, args)
	if err != nil {
		return nil, err
	}
	if metadata.Parent == "" {
		return runtime.Null{}, nil
	}
	return runtime.String{Value: metadata.Parent}, nil
}

// reflectGetField reads a single field off an instance by name.
// Returns null when the field doesn't exist on the instance's
// class (rather than erroring) so callers driving framework-style
// reflection don't need a separate `hasField` probe.
func reflectGetField(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects (instance, fieldName)", call.Callee.String())
	}
	instance, ok := args[0].(*runtime.Instance)
	if !ok {
		return nil, fmt.Errorf("%s expects instance, got %s", call.Callee.String(), args[0].TypeName())
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s field name must be string", call.Callee.String())
	}
	if v, hit := instance.Fields[name.Value]; hit {
		return v, nil
	}
	return runtime.Null{}, nil
}

// reflectSetField assigns a value to a named field on an instance.
// Returns the same instance (allowing fluent chaining). Field
// existence is not validated up-front: the assign succeeds and the
// field becomes part of the instance's field map. This matches the
// permissive shape that framework code (Gebweb's @Assert /
// @ApiResource PATCH) needs.
func reflectSetField(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("%s expects (instance, fieldName, value)", call.Callee.String())
	}
	instance, ok := args[0].(*runtime.Instance)
	if !ok {
		return nil, fmt.Errorf("%s expects instance, got %s", call.Callee.String(), args[0].TypeName())
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s field name must be string", call.Callee.String())
	}
	if instance.Fields == nil {
		instance.Fields = map[string]runtime.Value{}
	}
	instance.Fields[name.Value] = args[2]
	return instance, nil
}

// reflectClassName returns the class's own name regardless of whether
// the argument is a class value, an instance, or a primitive. For a
// class value `reflect.typeOf` returns the meta-string "class" - this
// builtin returns the class's actual identifier (e.g. "UserRepo").
// Returns null when the argument carries no class identity (closures,
// modules, ...). Symmetric with `reflect.class(name)` which goes the
// other way.
func reflectClassName(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	metadata, err := reflectClassMetadataValue(call, args)
	if err == nil && metadata.Name != "" {
		return runtime.String{Value: metadata.Name}, nil
	}
	/* className is total: for primitives and other values without
	 * class metadata, return the runtime type name (symmetric with
	 * how reflect.typeOf handles instances). */
	return runtime.String{Value: args[0].TypeName()}, nil
}

func reflectInterfaces(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	metadata, err := reflectClassMetadataValue(call, args)
	if err != nil {
		return nil, err
	}
	return stringList(metadata.Interfaces), nil
}

func reflectConstructors(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects class", call.Callee.String())
	}
	switch value := args[0].(type) {
	case *runtime.Class:
		overloads := make([]runtime.Value, 0, len(value.Constructors))
		for _, ctor := range value.Constructors {
			md := functionMetadataFromRuntimeFunction(ctor)
			paramValues := make([]runtime.Value, 0, len(md.Parameters))
			for _, p := range md.Parameters {
				paramValues = append(paramValues, parameterMetadataValue(p))
			}
			overloads = append(overloads, &runtime.List{Elements: paramValues})
		}
		return &runtime.List{Elements: overloads}, nil
	case runtime.BytecodeClass:
		overloads := make([]runtime.Value, 0, len(value.ConstructorMetadata))
		for _, md := range value.ConstructorMetadata {
			paramValues := make([]runtime.Value, 0, len(md.Parameters))
			for _, p := range md.Parameters {
				paramValues = append(paramValues, parameterMetadataValue(p))
			}
			overloads = append(overloads, &runtime.List{Elements: paramValues})
		}
		return &runtime.List{Elements: overloads}, nil
	case runtime.DecoratorTarget:
		if value.Class != nil {
			return &runtime.List{Elements: []runtime.Value{}}, nil
		}
	}
	return nil, fmt.Errorf("%s expects class, got %s", call.Callee.String(), args[0].TypeName())
}

func reflectTypeBindings(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects instance", call.Callee.String())
	}
	entries := map[string]runtime.DictEntry{}
	switch v := args[0].(type) {
	case *runtime.Instance:
		for name, typeName := range v.TypeBindings {
			putDict(entries, name, runtime.String{Value: typeName})
		}
	case *runtime.List:
		if len(v.ElementTypes) >= 1 {
			putDict(entries, "T", runtime.String{Value: elementTagBase(v.ElementTypes[0])})
		}
	case runtime.Set:
		if len(v.ElementTypes) >= 1 {
			putDict(entries, "T", runtime.String{Value: elementTagBase(v.ElementTypes[0])})
		}
	case runtime.Dict:
		if len(v.ElementTypes) >= 2 {
			putDict(entries, "K", runtime.String{Value: elementTagBase(v.ElementTypes[0])})
			putDict(entries, "V", runtime.String{Value: elementTagBase(v.ElementTypes[1])})
		}
	default:
		return nil, fmt.Errorf("%s expects instance or generic collection, got %s", call.Callee.String(), args[0].TypeName())
	}
	return runtime.Dict{Entries: entries}, nil
}

func reflectInterfaceMethods(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects interface", call.Callee.String())
	}
	iface, ok := args[0].(*runtime.Interface)
	if !ok {
		return nil, fmt.Errorf("%s expects interface, got %s", call.Callee.String(), args[0].TypeName())
	}
	type methodEntry struct {
		name string
		val  runtime.Value
	}
	methods := make([]methodEntry, 0, len(iface.Methods))
	for _, sig := range iface.Methods {
		name := ""
		if sig.Name != nil {
			name = sig.Name.Value
		}
		params := make([]runtime.Value, 0, len(sig.Parameters))
		for _, param := range sig.Parameters {
			pname := ""
			if param.Name != nil {
				pname = param.Name.Value
			}
			ptype := "any"
			if param.Type != nil {
				ptype = param.Type.String()
			}
			pm := runtime.ParameterMetadata{
				Name:       pname,
				Type:       ptype,
				Variadic:   param.Variadic,
				HasDefault: param.Default != nil,
				Decorators: decoratorsMetadataFromAST(param.Decorators, "parameter"),
			}
			params = append(params, parameterMetadataValue(pm))
		}
		rt := "void"
		if sig.ReturnType != nil {
			rt = sig.ReturnType.String()
		}
		entries := map[string]runtime.DictEntry{}
		putDict(entries, "name", runtime.String{Value: name})
		if sig.Doc == "" {
			putDict(entries, "doc", runtime.Null{})
		} else {
			putDict(entries, "doc", runtime.String{Value: sig.Doc})
		}
		putDict(entries, "parameters", &runtime.List{Elements: params})
		putDict(entries, "returnType", runtime.String{Value: rt})
		methods = append(methods, methodEntry{name: strings.ToLower(name), val: runtime.Dict{Entries: entries}})
	}
	sort.Slice(methods, func(i, j int) bool { return methods[i].name < methods[j].name })
	values := make([]runtime.Value, len(methods))
	for i, m := range methods {
		values[i] = m.val
	}
	return &runtime.List{Elements: values}, nil
}

func reflectInterfaceParents(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects interface", call.Callee.String())
	}
	iface, ok := args[0].(*runtime.Interface)
	if !ok {
		return nil, fmt.Errorf("%s expects interface, got %s", call.Callee.String(), args[0].TypeName())
	}
	names := make([]string, 0, len(iface.Parents))
	for _, parent := range iface.Parents {
		names = append(names, parent.Name)
	}
	sort.Strings(names)
	return stringList(names), nil
}

func classMetadataFromEnum(enum *runtime.EnumDef) runtime.ClassMetadata {
	md := runtime.ClassMetadata{Name: enum.Name, Module: enum.Module}
	for name := range enum.Methods {
		md.Methods = append(md.Methods, name)
	}
	md.Interfaces = append(md.Interfaces, enum.Implements...)
	sort.Strings(md.Methods)
	sort.Strings(md.Interfaces)
	return md
}

func reflectClassMetadataValue(call *ast.CallExpression, args []runtime.Value) (runtime.ClassMetadata, error) {
	if len(args) != 1 {
		return runtime.ClassMetadata{}, fmt.Errorf("%s expects class", call.Callee.String())
	}
	switch value := args[0].(type) {
	case *runtime.Class:
		return classMetadataFromRuntimeClass(value), nil
	case *runtime.Instance:
		// Accept an instance for symmetry with the VM: framework
		// code that holds an instance shouldn't have to recover the
		// class from its name.
		if value != nil && value.Class != nil {
			return classMetadataFromRuntimeClass(value.Class), nil
		}
	case runtime.Error:
		// Error-derived class instances are wrapped as runtime.Error.
		// The evaluator-aware variant (e.errorClassMetadataValue) can
		// follow the cross-module registry; this free function only
		// returns the class name when nothing else is available.
		return runtime.ClassMetadata{Name: value.Class}, nil
	case runtime.DecoratorTarget:
		if value.Class != nil {
			return *value.Class, nil
		}
	case runtime.BytecodeClass:
		return classMetadataFromBytecodeClass(value), nil
	case *runtime.EnumDef:
		return classMetadataFromEnum(value), nil
	}
	// Built-in primitive reflection: list / dict / set / string /
	// bytes / range expose their method table via the curated table
	// in primitiveTypeMetadata. Last-resort lookup so interface
	// names (passed in as strings from the compile-time path) get a
	// chance at their proper handler upstream.
	if md, ok := primitiveTypeMetadata(args[0]); ok {
		return md, nil
	}
	return runtime.ClassMetadata{}, fmt.Errorf("%s expects class, got %s", call.Callee.String(), args[0].TypeName())
}

// primitiveTypeMetadata returns a synthetic ClassMetadata describing the
// method surface of a built-in primitive value (list, dict, set, string,
// bytes, range). The reflect.* API uses this so framework code can
// introspect primitives the same way it introspects user-defined classes.
func primitiveTypeMetadata(value runtime.Value) (runtime.ClassMetadata, bool) {
	switch value.(type) {
	case *runtime.List:
		return runtime.ClassMetadata{
			Name:    "list",
			Methods: primitiveMethodNamesFor("list"),
		}, true
	case runtime.Dict:
		return runtime.ClassMetadata{
			Name:    "dict",
			Methods: primitiveMethodNamesFor("dict"),
		}, true
	case runtime.Set:
		return runtime.ClassMetadata{
			Name:    "set",
			Methods: primitiveMethodNamesFor("set"),
		}, true
	case runtime.String:
		return runtime.ClassMetadata{
			Name:    "string",
			Methods: primitiveMethodNamesFor("string"),
		}, true
	case runtime.Bytes:
		return runtime.ClassMetadata{
			Name:    "bytes",
			Methods: primitiveMethodNamesFor("bytes"),
		}, true
	case runtime.Range:
		return runtime.ClassMetadata{
			Name:    "range",
			Methods: primitiveMethodNamesFor("range"),
		}, true
	}
	return runtime.ClassMetadata{}, false
}

// primitiveMethodNamesFor returns a sorted list of method names for a
// primitive type. The list is curated rather than introspected from the
// dispatch tables because some method names share an implementation and
// some have different effective surfaces per type.
func primitiveMethodNamesFor(typeName string) []string {
	return append([]string(nil), native.PrimitiveMethods[typeName]...)
}

func classMetadataFromRuntimeClass(class *runtime.Class) runtime.ClassMetadata {
	metadata := runtime.ClassMetadata{Name: class.Name, Doc: class.Doc}
	if class.Parent != nil {
		metadata.Parent = class.Parent.Name
	}
	for _, field := range class.Fields {
		metadata.Fields = append(metadata.Fields, field.Name)
	}
	methods := map[string]string{}
	for name, overloads := range class.Methods {
		methods[name] = reflectedFunctionName(name, overloads)
	}
	staticMethods := map[string]string{}
	for name, overloads := range class.StaticMethods {
		staticMethods[name] = reflectedFunctionName(name, overloads)
	}
	for _, iface := range class.Implements {
		metadata.Interfaces = append(metadata.Interfaces, iface.Name)
	}
	sort.Strings(metadata.Fields)
	metadata.Methods = sortedStringMapValues(methods)
	metadata.StaticMethods = sortedStringMapValues(staticMethods)
	sort.Strings(metadata.Interfaces)
	return metadata
}

func classMetadataFromBytecodeClass(class runtime.BytecodeClass) runtime.ClassMetadata {
	methods := map[string]string{}
	for name, overloads := range class.MethodMetadata {
		methods[name] = reflectedFunctionMetadataName(name, overloads)
	}
	staticMethods := map[string]string{}
	for name, overloads := range class.StaticMetadata {
		staticMethods[name] = reflectedFunctionMetadataName(name, overloads)
	}
	metadata := runtime.ClassMetadata{
		Name:          class.Name,
		Doc:           class.Doc,
		Parent:        class.Parent,
		Fields:        append([]string(nil), class.Fields...),
		Methods:       sortedStringMapValues(methods),
		StaticMethods: sortedStringMapValues(staticMethods),
		Interfaces:    append([]string(nil), class.Interfaces...),
	}
	sort.Strings(metadata.Fields)
	sort.Strings(metadata.Interfaces)
	return metadata
}

func reflectedFunctionName(fallback string, overloads []runtime.Function) string {
	if len(overloads) > 0 && overloads[0].Name != "" {
		return overloads[0].Name
	}
	return fallback
}

func reflectedFunctionMetadataName(fallback string, overloads []runtime.FunctionMetadata) string {
	if len(overloads) > 0 && overloads[0].Name != "" {
		return overloads[0].Name
	}
	return fallback
}

func sortedStringMapValues(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, values[key])
	}
	return out
}

func reflectLookupRequiresEvaluator(call *ast.CallExpression, _ []runtime.Value) (runtime.Value, error) {
	return nil, fmt.Errorf("%s requires evaluator context", call.Callee.String())
}

func reflectMethod(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects class or instance and method name", call.Callee.String())
	}
	instance, bound := args[0].(*runtime.Instance)
	class, err := reflectClassArg(call, args[0])
	if err != nil {
		return nil, err
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s method name must be string", call.Callee.String())
	}
	overloads, ok := class.Methods[strings.ToLower(name.Value)]
	if !ok {
		return runtime.Null{}, nil
	}
	if bound {
		return boundReflectMethodFunction(class.Name+"."+name.Value, overloads, instance, nil), nil
	}
	return reflectFunctionValue(name.Value, overloads), nil
}

// reflectMethodBound is the evaluator-bound variant. It captures the live
// Evaluator so the returned bound method runs against the same module
// loader / state instead of a fresh stub Evaluator that can't resolve
// imported modules (gebweb.notFound, etc.).
func (e *Evaluator) reflectMethodBound(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects class or instance and method name", call.Callee.String())
	}
	instance, bound := args[0].(*runtime.Instance)
	class, err := reflectClassArg(call, args[0])
	if err != nil {
		return nil, err
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s method name must be string", call.Callee.String())
	}
	overloads, ok := class.Methods[strings.ToLower(name.Value)]
	if !ok {
		return runtime.Null{}, nil
	}
	if bound {
		return boundReflectMethodFunction(class.Name+"."+name.Value, overloads, instance, e), nil
	}
	return reflectFunctionValue(name.Value, overloads), nil
}

func boundReflectMethodFunction(label string, overloads []runtime.Function, instance *runtime.Instance, host *Evaluator) runtime.Function {
	metadataSource := runtime.Function{}
	if len(overloads) > 0 {
		metadataSource = overloads[0]
	}
	return runtime.Function{
		Name:       metadataSource.Name,
		Parameters: append([]ast.Parameter(nil), metadataSource.Parameters...),
		ReturnType: metadataSource.ReturnType,
		Decorators: append([]ast.Decorator(nil), metadataSource.Decorators...),
		Target:     reflectFunctionTarget(metadataSource),
		Async:      metadataSource.Async,
		Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
			method, err := selectOverload(label, overloads, args)
			if err != nil {
				return nil, err
			}
			ev := host
			if ev == nil {
				ev = evaluatorForNativeMethod(method)
			}
			return ev.applyFunctionWithThis(method, args, instance)
		},
	}
}

func evaluatorForNativeMethod(fn runtime.Function) *Evaluator {
	return &Evaluator{stdout: io.Discard, maxCallDepth: DefaultMaxCallDepth}
}

func reflectStaticMethod(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects class and static method name", call.Callee.String())
	}
	class, ok := args[0].(*runtime.Class)
	if !ok {
		return nil, fmt.Errorf("%s expects class, got %s", call.Callee.String(), args[0].TypeName())
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s static method name must be string", call.Callee.String())
	}
	overloads, ok := class.StaticMethods[strings.ToLower(name.Value)]
	if !ok {
		return runtime.Null{}, nil
	}
	return reflectFunctionValue(name.Value, overloads), nil
}

func reflectClassArg(call *ast.CallExpression, value runtime.Value) (*runtime.Class, error) {
	switch value := value.(type) {
	case *runtime.Class:
		return value, nil
	case *runtime.Instance:
		return value.Class, nil
	default:
		return nil, fmt.Errorf("%s expects class or instance, got %s", call.Callee.String(), value.TypeName())
	}
}

func reflectFunctionValue(name string, overloads []runtime.Function) runtime.Value {
	if len(overloads) == 1 {
		return overloads[0]
	}
	return runtime.OverloadedFunction{Name: name, Overloads: append([]runtime.Function(nil), overloads...)}
}

func reflectTargetDecorators(call *ast.CallExpression, value runtime.Value) ([]ast.Decorator, string, error) {
	switch value := value.(type) {
	case runtime.Function:
		return value.Decorators, reflectFunctionTarget(value), nil
	case *runtime.Class:
		return value.Decorators, "class", nil
	case runtime.DecoratorTarget:
		return nil, value.Target, nil
	default:
		return nil, "", fmt.Errorf("%s expects function or class, got %s", call.Callee.String(), value.TypeName())
	}
}

func reflectFunctionTarget(fn runtime.Function) string {
	if fn.Target != "" {
		return fn.Target
	}
	return "function"
}

func reflectFunctionMetadataValue(value runtime.Value) (runtime.FunctionMetadata, bool) {
	switch value := value.(type) {
	case runtime.Function:
		return functionMetadataFromRuntimeFunction(value), true
	case runtime.OverloadedFunction:
		if len(value.Overloads) == 0 {
			return runtime.FunctionMetadata{}, false
		}
		return functionMetadataFromRuntimeFunction(value.Overloads[0]), true
	case runtime.DecoratorTarget:
		if value.Function != nil {
			return *value.Function, true
		}
	case runtime.BytecodeFunction:
		return runtime.FunctionMetadata{
			Name:       value.Name,
			Target:     "function",
			Doc:        value.Doc,
			Parameters: append([]runtime.ParameterMetadata(nil), value.Parameters...),
			ReturnType: value.ReturnType,
			Async:      value.Async,
			Variadic:   value.Variadic,
			Decorators: append([]runtime.DecoratorMetadata(nil), value.Decorators...),
		}, true
	}
	return runtime.FunctionMetadata{}, false
}

func functionMetadataFromRuntimeFunction(fn runtime.Function) runtime.FunctionMetadata {
	parameters := make([]runtime.ParameterMetadata, 0, len(fn.Parameters))
	for _, param := range fn.Parameters {
		name := ""
		if param.Name != nil {
			name = param.Name.Value
		}
		typ := "any"
		if param.Type != nil {
			typ = param.Type.String()
		}
		parameters = append(parameters, runtime.ParameterMetadata{
			Name:       name,
			Type:       typ,
			Variadic:   param.Variadic,
			HasDefault: param.Default != nil,
			Decorators: decoratorsMetadataFromAST(param.Decorators, "parameter"),
		})
	}
	returnType := "void"
	if fn.ReturnType != nil {
		returnType = fn.ReturnType.String()
	}
	return runtime.FunctionMetadata{
		Name:       fn.Name,
		Target:     reflectFunctionTarget(fn),
		Doc:        fn.Doc,
		Parameters: parameters,
		ReturnType: returnType,
		Async:      fn.Async,
		Variadic:   len(fn.Parameters) > 0 && fn.Parameters[len(fn.Parameters)-1].Variadic,
	}
}

func optionalDecoratorName(call *ast.CallExpression, args []runtime.Value) (string, error) {
	if len(args) == 1 {
		return "", nil
	}
	name, ok := args[1].(runtime.String)
	if !ok {
		return "", fmt.Errorf("%s decorator name must be string", call.Callee.String())
	}
	return name.Value, nil
}

func decoratorListValue(decorators []ast.Decorator, target string, filter string) (runtime.Value, error) {
	values := make([]runtime.Value, 0, len(decorators))
	for position, decorator := range decorators {
		if filter != "" && !strings.EqualFold(decorator.Name.Value, filter) {
			continue
		}
		value, err := decoratorValue(decorator, target, position, 0)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return &runtime.List{Elements: values}, nil
}

func overloadedDecoratorListValue(overloaded runtime.OverloadedFunction, filter string) (runtime.Value, error) {
	values := []runtime.Value{}
	for overloadIndex, overload := range overloaded.Overloads {
		target := reflectFunctionTarget(overload)
		for position, decorator := range overload.Decorators {
			if filter != "" && !strings.EqualFold(decorator.Name.Value, filter) {
				continue
			}
			value, err := decoratorValue(decorator, target, position, overloadIndex)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
	}
	return &runtime.List{Elements: values}, nil
}

func decoratorValue(decorator ast.Decorator, target string, position int, overload int) (runtime.Value, error) {
	entries := map[string]runtime.DictEntry{}
	name := ""
	if decorator.Name != nil {
		name = decorator.Name.Value
	}
	putDict(entries, "name", runtime.String{Value: name})
	putDict(entries, "target", runtime.String{Value: target})
	putDict(entries, "position", runtime.NewInt64(int64(position)))
	putDict(entries, "overload", runtime.NewInt64(int64(overload)))
	args := []runtime.Value{}
	namedArgs := map[string]runtime.DictEntry{}
	for _, arg := range decorator.Arguments {
		if arg.Spread {
			return nil, fmt.Errorf("decorator %s metadata does not support spread arguments", name)
		}
		value, err := decoratorMetadataValue(arg.Value)
		if err != nil {
			return nil, fmt.Errorf("decorator %s metadata: %w", name, err)
		}
		if arg.Name != nil {
			putDict(namedArgs, arg.Name.Value, value)
		} else {
			args = append(args, value)
		}
	}
	putDict(entries, "args", &runtime.List{Elements: args})
	putDict(entries, "namedArgs", runtime.Dict{Entries: namedArgs})
	putDict(entries, "line", runtime.NewInt64(int64(decorator.Token.Line)))
	putDict(entries, "column", runtime.NewInt64(int64(decorator.Token.Column)))
	return runtime.Dict{Entries: entries}, nil
}

func parameterMetadataValue(parameter runtime.ParameterMetadata) runtime.Value {
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "name", runtime.String{Value: parameter.Name})
	putDict(entries, "type", runtime.String{Value: parameter.Type})
	putDict(entries, "variadic", runtime.Bool{Value: parameter.Variadic})
	putDict(entries, "hasDefault", runtime.Bool{Value: parameter.HasDefault})
	if len(parameter.Decorators) > 0 {
		decValues := make([]runtime.Value, 0, len(parameter.Decorators))
		for _, dec := range parameter.Decorators {
			decValues = append(decValues, decoratorMetadataDictValue(dec))
		}
		putDict(entries, "decorators", &runtime.List{Elements: decValues})
	}
	return runtime.Dict{Entries: entries}
}

func decoratorMetadataDictValue(metadata runtime.DecoratorMetadata) runtime.Value {
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "name", runtime.String{Value: metadata.Name})
	putDict(entries, "target", runtime.String{Value: metadata.Target})
	putDict(entries, "position", runtime.NewInt64(metadata.Position))
	putDict(entries, "overload", runtime.NewInt64(metadata.Overload))
	args := make([]runtime.Value, 0, len(metadata.Args))
	args = append(args, metadata.Args...)
	putDict(entries, "args", &runtime.List{Elements: args})
	namedArgs := map[string]runtime.DictEntry{}
	for k, v := range metadata.NamedArgs {
		putDict(namedArgs, k, v)
	}
	putDict(entries, "namedArgs", runtime.Dict{Entries: namedArgs})
	putDict(entries, "line", runtime.NewInt64(metadata.Line))
	putDict(entries, "column", runtime.NewInt64(metadata.Column))
	return runtime.Dict{Entries: entries}
}

func decoratorsMetadataFromAST(decorators []ast.Decorator, target string) []runtime.DecoratorMetadata {
	if len(decorators) == 0 {
		return nil
	}
	out := make([]runtime.DecoratorMetadata, 0, len(decorators))
	for position, dec := range decorators {
		item := runtime.DecoratorMetadata{
			Target:    target,
			Position:  int64(position),
			Line:      int64(dec.Token.Line),
			Column:    int64(dec.Token.Column),
			NamedArgs: map[string]runtime.Value{},
		}
		if dec.Name != nil {
			item.Name = dec.Name.Value
		}
		for _, arg := range dec.Arguments {
			value, err := decoratorMetadataValue(arg.Value)
			if err != nil {
				continue
			}
			if arg.Name != nil {
				item.NamedArgs[arg.Name.Value] = value
			} else {
				item.Args = append(item.Args, value)
			}
		}
		out = append(out, item)
	}
	return out
}

func decoratorMetadataValue(expr ast.Expression) (runtime.Value, error) {
	switch expr := expr.(type) {
	case *ast.StringLiteral:
		return runtime.String{Value: expr.Value}, nil
	case *ast.EmbeddedLiteral:
		if expr.Binary {
			return runtime.Bytes{Value: expr.Content}, nil
		}
		return runtime.String{Value: string(expr.Content)}, nil
	case *ast.IntegerLiteral:
		return runtime.NewIntLiteral(expr.Value)
	case *ast.DecimalLiteral:
		return runtime.NewDecimalLiteral(expr.Value)
	case *ast.FloatLiteral:
		stripped := strings.ReplaceAll(expr.Value[:len(expr.Value)-1], "_", "")
		value, err := strconv.ParseFloat(stripped, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid float literal %q", expr.Value)
		}
		return runtime.Float{Value: value}, nil
	case *ast.Literal:
		switch value := expr.Value.(type) {
		case bool:
			return runtime.Bool{Value: value}, nil
		case nil:
			return runtime.Null{}, nil
		default:
			return nil, fmt.Errorf("unsupported literal %v", expr.Value)
		}
	case *ast.ListLiteral:
		values := make([]runtime.Value, 0, len(expr.Elements))
		for _, element := range expr.Elements {
			value, err := decoratorMetadataValue(element)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return &runtime.List{Elements: values}, nil
	case *ast.DictLiteral:
		entries := map[string]runtime.DictEntry{}
		for _, entry := range expr.Entries {
			key, err := decoratorMetadataValue(entry.Key)
			if err != nil {
				return nil, err
			}
			value, err := decoratorMetadataValue(entry.Value)
			if err != nil {
				return nil, err
			}
			entries[dictKey(key)] = runtime.DictEntry{Key: key, Value: value}
		}
		return runtime.Dict{Entries: entries}, nil
	case *ast.SetLiteral:
		entries := map[string]runtime.SetEntry{}
		for _, element := range expr.Elements {
			value, err := decoratorMetadataValue(element)
			if err != nil {
				return nil, err
			}
			entries[dictKey(value)] = runtime.SetEntry{Value: value}
		}
		return runtime.Set{Elements: entries}, nil
	default:
		return nil, fmt.Errorf("unsupported decorator argument expression %s", expr.String())
	}
}
