package evaluator

import (
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/runtime"
	"strings"
)

func (e *Evaluator) applyCallableFunctionDecorators(fn runtime.Function, decorators []ast.Decorator, env *runtime.Environment) (runtime.Function, error) {
	current := fn
	wrapped := false
	for i := len(decorators) - 1; i >= 0; i-- {
		decorator := decorators[i]
		if decorator.Name == nil {
			continue
		}
		value, ok := env.Get(decorator.Name.Value)
		if !ok {
			continue
		}
		args, err := e.decoratorCallArguments(current, decorator, env)
		if err != nil {
			return runtime.Function{}, err
		}
		var result runtime.Value
		switch callable := value.(type) {
		case runtime.Function:
			bound, ok := bindEvaluatedFunctionCallArguments(callable, args)
			if !ok || !functionArgumentsMatch(callable, bound) {
				return runtime.Function{}, fmt.Errorf("decorator %s cannot be called with decorated function arguments", decorator.Name.Value)
			}
			result, err = e.applyFunction(callable, bound)
		case runtime.OverloadedFunction:
			var matches []runtime.Function
			var matchedArgs [][]runtime.Value
			for _, overload := range callable.Overloads {
				bound, ok := bindEvaluatedFunctionCallArguments(overload, args)
				if !ok || !functionArgumentsMatch(overload, bound) {
					continue
				}
				matches = append(matches, overload)
				matchedArgs = append(matchedArgs, bound)
			}
			if len(matches) == 0 {
				return runtime.Function{}, fmt.Errorf("no matching overload for decorator %s", decorator.Name.Value)
			}
			if len(matches) > 1 {
				return runtime.Function{}, fmt.Errorf("ambiguous overload for decorator %s", decorator.Name.Value)
			}
			result, err = e.applyFunction(matches[0], matchedArgs[0])
		default:
			continue
		}
		if err != nil {
			return runtime.Function{}, err
		}
		next, ok := result.(runtime.Function)
		if !ok {
			return runtime.Function{}, fmt.Errorf("decorator %s must return function, got %s", decorator.Name.Value, result.TypeName())
		}
		if !decoratorWrapperCompatible(fn, next) {
			return runtime.Function{}, fmt.Errorf("decorator %s returned incompatible wrapper for %s", decorator.Name.Value, fn.Name)
		}
		current = mergeDecoratedFunctionMetadata(fn, next)
		wrapped = true
	}
	if wrapped {
		current.OriginalParameters = fn.Parameters
	}
	return current, nil
}

func decoratorWrapperCompatible(original runtime.Function, wrapper runtime.Function) bool {
	origMin, origMax, origVariadic := functionArityRange(original.Parameters)
	wrapMin, wrapMax, wrapVariadic := functionArityRange(wrapper.Parameters)
	if wrapMin > origMin {
		return false
	}
	if origVariadic {
		return wrapVariadic
	}
	return wrapVariadic || wrapMax >= origMax
}

func functionArityRange(params []ast.Parameter) (int, int, bool) {
	variadic := len(params) > 0 && params[len(params)-1].Variadic
	min := len(params)
	if variadic {
		min--
	}
	for min > 0 && params[min-1].Default != nil {
		min--
	}
	return min, len(params), variadic
}

func (e *Evaluator) decoratorCallArguments(fn runtime.Function, decorator ast.Decorator, env *runtime.Environment) ([]evaluatedCallArg, error) {
	args := []evaluatedCallArg{{value: fn}}
	for _, arg := range decorator.Arguments {
		value, err := e.evalExpression(arg.Value, env)
		if err != nil {
			return nil, fmt.Errorf("decorator %s argument: %w", decorator.Name.Value, err)
		}
		if arg.Spread {
			list, ok := value.(*runtime.List)
			if !ok {
				return nil, fmt.Errorf("decorator %s spread argument must be a list", decorator.Name.Value)
			}
			for _, element := range list.Elements {
				args = append(args, evaluatedCallArg{value: element})
			}
			continue
		}
		name := ""
		if arg.Name != nil {
			name = arg.Name.Value
		}
		args = append(args, evaluatedCallArg{name: name, value: value})
	}
	return args, nil
}

func mergeDecoratedFunctionMetadata(original runtime.Function, decorated runtime.Function) runtime.Function {
	if decorated.Name == "" {
		decorated.Name = original.Name
	}
	if decorated.Doc == "" {
		decorated.Doc = original.Doc
	}
	if decorated.Target == "" {
		decorated.Target = original.Target
	}
	if len(decorated.Decorators) == 0 {
		decorated.Decorators = append([]ast.Decorator(nil), original.Decorators...)
	}
	if len(decorated.TypeParameters) == 0 {
		decorated.TypeParameters = append([]string(nil), original.TypeParameters...)
	}
	if !decorated.ForwardThis {
		decorated.ForwardThis = original.ForwardThis
	}
	return decorated
}

func (e *Evaluator) applyCallableClassDecorators(class *runtime.Class, decorators []ast.Decorator, env *runtime.Environment) (runtime.Value, error) {
	var current runtime.Value = class
	for i := len(decorators) - 1; i >= 0; i-- {
		decorator := decorators[i]
		if decorator.Name == nil {
			continue
		}
		value, ok := env.Get(decorator.Name.Value)
		if !ok {
			continue
		}
		args, err := e.decoratorClassCallArguments(current, decorator, env)
		if err != nil {
			return nil, err
		}
		var result runtime.Value
		switch callable := value.(type) {
		case runtime.Function:
			bound, ok := bindEvaluatedFunctionCallArguments(callable, args)
			if !ok || !functionArgumentsMatch(callable, bound) {
				return nil, fmt.Errorf("decorator %s cannot be called with decorated class arguments", decorator.Name.Value)
			}
			result, err = e.applyFunction(callable, bound)
		case runtime.OverloadedFunction:
			var matches []runtime.Function
			var matchedArgs [][]runtime.Value
			for _, overload := range callable.Overloads {
				bound, ok := bindEvaluatedFunctionCallArguments(overload, args)
				if !ok || !functionArgumentsMatch(overload, bound) {
					continue
				}
				matches = append(matches, overload)
				matchedArgs = append(matchedArgs, bound)
			}
			if len(matches) == 0 {
				return nil, fmt.Errorf("no matching overload for decorator %s", decorator.Name.Value)
			}
			if len(matches) > 1 {
				return nil, fmt.Errorf("ambiguous overload for decorator %s", decorator.Name.Value)
			}
			result, err = e.applyFunction(matches[0], matchedArgs[0])
		default:
			continue
		}
		if err != nil {
			return nil, err
		}
		switch next := result.(type) {
		case *runtime.Class:
			current = mergeDecoratedClassMetadata(class, next)
		case runtime.Function, runtime.OverloadedFunction:
			current = result
		default:
			return nil, fmt.Errorf("decorator %s must return class or callable, got %s", decorator.Name.Value, result.TypeName())
		}
	}
	return current, nil
}

func (e *Evaluator) decoratorClassCallArguments(class runtime.Value, decorator ast.Decorator, env *runtime.Environment) ([]evaluatedCallArg, error) {
	args := []evaluatedCallArg{{value: class}}
	for _, arg := range decorator.Arguments {
		value, err := e.evalExpression(arg.Value, env)
		if err != nil {
			return nil, fmt.Errorf("decorator %s argument: %w", decorator.Name.Value, err)
		}
		if arg.Spread {
			list, ok := value.(*runtime.List)
			if !ok {
				return nil, fmt.Errorf("decorator %s spread argument must be a list", decorator.Name.Value)
			}
			for _, element := range list.Elements {
				args = append(args, evaluatedCallArg{value: element})
			}
			continue
		}
		name := ""
		if arg.Name != nil {
			name = arg.Name.Value
		}
		args = append(args, evaluatedCallArg{name: name, value: value})
	}
	return args, nil
}

func mergeDecoratedClassMetadata(original *runtime.Class, decorated *runtime.Class) *runtime.Class {
	if decorated.Name == "" {
		decorated.Name = original.Name
	}
	if decorated.Doc == "" {
		decorated.Doc = original.Doc
	}
	if len(decorated.Decorators) == 0 {
		decorated.Decorators = original.Decorators
	}
	if len(decorated.TypeParameters) == 0 {
		decorated.TypeParameters = append([]string(nil), original.TypeParameters...)
	}
	if decorated.Module == "" {
		decorated.Module = original.Module
	}
	if decorated.Env == nil {
		decorated.Env = original.Env
	}
	return decorated
}

func mergeInterfaceMembers(stmt *ast.ClassStatement, ifaces []*runtime.Interface) error {
	declaredMethods := map[string]bool{}
	declaredFields := map[string]bool{}
	for _, member := range stmt.Members {
		switch m := member.(type) {
		case *ast.FunctionStatement:
			declaredMethods[strings.ToLower(m.Name.Value)] = true
		case *ast.DeclarationStatement:
			if !strings.HasPrefix(m.Kind, "static") {
				declaredFields[strings.ToLower(m.Name.Value)] = true
			}
		}
	}
	defaultSource := map[string]string{}
	defaultMethod := map[string]*ast.FunctionStatement{}
	fieldSource := map[string]string{}
	fieldDecl := map[string]*ast.DeclarationStatement{}
	for _, iface := range ifaces {
		for _, def := range iface.Defaults {
			key := strings.ToLower(def.Name.Value)
			if declaredMethods[key] {
				continue
			}
			if prev, seen := defaultSource[key]; seen && prev != iface.Name {
				return fmt.Errorf("class %s inherits multiple defaults for %s from %s and %s; class must override", stmt.Name.Value, def.Name.Value, prev, iface.Name)
			}
			defaultSource[key] = iface.Name
			defaultMethod[key] = def
		}
		for _, field := range iface.Fields {
			key := strings.ToLower(field.Name.Value)
			if declaredFields[key] {
				continue
			}
			if prev, seen := fieldSource[key]; seen {
				prevField := fieldDecl[key]
				if prevField.Type.String() != field.Type.String() {
					return fmt.Errorf("class %s inherits field %s from %s (%s) and %s (%s) with conflicting types", stmt.Name.Value, field.Name.Value, prev, prevField.Type.String(), iface.Name, field.Type.String())
				}
				continue
			}
			fieldSource[key] = iface.Name
			fieldDecl[key] = field
		}
	}
	for _, field := range fieldDecl {
		stmt.Members = append(stmt.Members, field)
	}
	for _, method := range defaultMethod {
		stmt.Members = append(stmt.Members, method)
	}
	return nil
}

func (e *Evaluator) buildInterface(stmt *ast.InterfaceStatement, env *runtime.Environment) (*runtime.Interface, error) {
	iface := &runtime.Interface{Name: stmt.Name.Value, Doc: stmt.Doc, Module: e.currentModule, TypeParameters: typeParameterNames(stmt.Generics), Methods: e.resolveFunctionSignatures(stmt.Methods), Defaults: stmt.Defaults, Fields: stmt.Fields}
	for _, parentRef := range stmt.Parents {
		parentValue, ok, err := e.resolveTypeValue(parentRef, env)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("parent interface %q is not declared", parentRef.Name)
		}
		parent, ok := parentValue.(*runtime.Interface)
		if !ok {
			return nil, fmt.Errorf("%q is not an interface", parentRef.Name)
		}
		iface.Parents = append(iface.Parents, parent)
	}
	return iface, nil
}

func (e *Evaluator) getErrorSentinel(name string) *runtime.Class {
	if cls, ok := e.errorSentinels[name]; ok {
		return cls
	}
	var parent *runtime.Class
	if pname := errorParent(name); pname != "" {
		parent = e.getErrorSentinel(pname)
	}
	cls := &runtime.Class{
		Name:          name,
		Fields:        []runtime.Field{},
		Methods:       map[string][]runtime.Function{},
		StaticMethods: map[string][]runtime.Function{},
		StaticValues:  map[string]runtime.Value{},
		Parent:        parent,
	}
	e.errorSentinels[name] = cls
	return cls
}

func (e *Evaluator) installBuiltinTypes(env *runtime.Environment) error {
	testClass := &runtime.Class{
		Name:    "Test",
		Fields:  []runtime.Field{},
		Methods: map[string][]runtime.Function{},
		Env:     env,
	}
	for _, methodName := range []string{
		"equal",
		"assertEqual",
		"assertEquals",
		"assertNotEqual",
		"assertNotEquals",
		"isTrue",
		"assertTrue",
		"isFalse",
		"assertFalse",
		"assertNull",
		"notNull",
		"assertNotNull",
		"assertContains",
		"assertNotContains",
		"assertEmpty",
		"assertNotEmpty",
		"assertGreaterThan",
		"assertGreaterThanOrEqual",
		"assertLessThan",
		"assertLessThanOrEqual",
		"assertThrows",
		"assertThrowsOf",
		"fail",
		"skip",
	} {
		testClass.Methods[strings.ToLower(methodName)] = []runtime.Function{{Name: methodName, Native: e.nativeTestAssertion(methodName)}}
	}
	e.testClass = testClass
	classes := httpObjectClasses(env)
	for _, class := range classes {
		if strings.EqualFold(class.Name, "Request") {
			class.Module = "http"
			e.httpRequestClass = class
		}
		if strings.EqualFold(class.Name, "Response") {
			class.Module = "http"
			e.httpResponseClass = class
			httpResponseResultClassOnce.Do(func() { httpResponseResultClass = class })
		}
	}
	for _, class := range e.processObjectClasses() {
		if strings.EqualFold(class.Name, "Process") {
			e.processClass = class
		}
		if strings.EqualFold(class.Name, "Result") {
			e.processResultClass = class
		}
	}
	for _, class := range e.httpClientObjectClasses() {
		class.Module = "http"
		switch strings.ToLower(class.Name) {
		case "client":
			e.httpClientClass = class
		case "builder":
			e.httpBuilderClass = class
		case "cookiejar":
			e.httpCookieJarClass = class
		case "fetchstream":
			e.httpFetchStreamClass = class
		}
	}
	for _, class := range e.dbObjectClasses(env) {
		switch strings.ToLower(class.Name) {
		case "connection":
			e.dbConnectionClass = class
		case "transaction":
			e.dbTransactionClass = class
		case "statement":
			e.dbStatementClass = class
		case "rows":
			e.dbRowsClass = class
		}
	}
	e.streamIfaces = map[string]*runtime.Interface{}
	for _, iface := range streamInterfaces() {
		e.streamIfaces[strings.ToLower(iface.Name)] = iface
	}
	return nil
}

func streamInterfaces() []*runtime.Interface {
	return []*runtime.Interface{
		streamInterface("JsonStreamInterface", []methodSpec{
			{"onStartObject", nil},
			{"onEndObject", nil},
			{"onStartArray", nil},
			{"onEndArray", nil},
			{"onKey", []string{"key"}},
			{"onValue", []string{"value"}},
			{"onError", []string{"error"}},
		}),
		streamInterface("XmlStreamInterface", []methodSpec{
			{"onStartElement", []string{"name", "attributes"}},
			{"onEndElement", []string{"name"}},
			{"onText", []string{"text"}},
			{"onComment", []string{"text"}},
			{"onError", []string{"error"}},
		}),
		streamInterface("YamlStreamInterface", []methodSpec{
			{"onStartMap", nil},
			{"onEndMap", nil},
			{"onStartList", nil},
			{"onEndList", nil},
			{"onKey", []string{"key"}},
			{"onValue", []string{"value"}},
			{"onError", []string{"error"}},
		}),
		streamInterface("CsvStreamInterface", []methodSpec{
			{"onHeader", []string{"columns"}},
			{"onRow", []string{"row"}},
			{"onError", []string{"error"}},
		}),
		streamInterface("LogInterface", []methodSpec{
			{"handle", []string{"level", "message", "fields"}},
		}),
	}
}

type methodSpec struct {
	name       string
	parameters []string
}

func streamInterface(name string, methods []methodSpec) *runtime.Interface {
	iface := &runtime.Interface{Name: name}
	for _, method := range methods {
		sig := &ast.FunctionSignature{Name: &ast.Identifier{Value: method.name}}
		for _, param := range method.parameters {
			sig.Parameters = append(sig.Parameters, ast.Parameter{Name: &ast.Identifier{Value: param}})
		}
		iface.Methods = append(iface.Methods, sig)
	}
	return iface
}

func (e *Evaluator) buildEnum(stmt *ast.EnumStatement, env *runtime.Environment) (*runtime.EnumDef, error) {
	enum := &runtime.EnumDef{Name: stmt.Name.Value}
	if stmt.BackingType != nil {
		enum.BackingType = stmt.BackingType.Name
	}
	for _, v := range stmt.Variants {
		backingValue, err := enumBackingValue(v.BackingValue)
		if err != nil {
			return nil, err
		}
		enum.Variants = append(enum.Variants, runtime.EnumVariantDefRuntime{
			Name:         v.Name.Value,
			FieldCount:   len(v.FieldTypes),
			BackingValue: backingValue,
		})
	}
	if len(stmt.Methods) > 0 {
		enum.Methods = map[string][]runtime.Function{}
		for _, member := range stmt.Methods {
			if err := checkEnumMethodCollision(enum, member.Name.Value); err != nil {
				return nil, err
			}
			fn := runtime.Function{Name: member.Name.Value, Doc: member.Doc, TypeParameters: typeParameterNames(member.Generics), TypeParamConstraints: typeParamConstraints(member.Generics), Parameters: e.resolveParameters(member.Parameters), ReturnType: e.resolveTypeRef(member.ReturnType), Body: member.Body, Env: env, Decorators: member.Decorators, Target: "method", Async: member.Async, IsGenerator: blockContainsYield(member.Body), ForwardThis: true}
			decorated, err := e.applyCallableFunctionDecorators(fn, member.Decorators, env)
			if err != nil {
				return nil, err
			}
			key := strings.ToLower(member.Name.Value)
			enum.Methods[key] = append(enum.Methods[key], decorated)
		}
	}
	for _, ifaceRef := range stmt.Implements {
		ifaceValue, ok, err := e.resolveTypeValue(ifaceRef, env)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("interface %q is not declared", ifaceRef.Name)
		}
		iface, ok := ifaceValue.(*runtime.Interface)
		if !ok {
			return nil, fmt.Errorf("%q is not an interface", ifaceRef.Name)
		}
		enum.Implements = append(enum.Implements, iface.Name)
		if enum.Methods == nil {
			enum.Methods = map[string][]runtime.Function{}
		}
		if err := e.foldInterfaceDefaultsIntoEnum(enum, iface, env); err != nil {
			return nil, err
		}
		if err := validateEnumInterfaceImplementation(enum, iface); err != nil {
			return nil, err
		}
	}
	return enum, nil
}

func enumBackingValue(expr ast.Expression) (runtime.Value, error) {
	switch lit := expr.(type) {
	case nil:
		return nil, nil
	case *ast.StringLiteral:
		return runtime.String{Value: lit.Value}, nil
	case *ast.IntegerLiteral:
		value, err := runtime.NewIntLiteral(lit.Value)
		if err != nil {
			return nil, err
		}
		if value.Value.IsInt64() {
			return runtime.SmallInt{Value: value.Value.Int64()}, nil
		}
		return value, nil
	case *ast.PrefixExpression:
		if lit.Operator == "-" {
			if intLit, ok := lit.Right.(*ast.IntegerLiteral); ok {
				value, err := runtime.NewIntLiteral("-" + intLit.Value)
				if err != nil {
					return nil, err
				}
				if value.Value.IsInt64() {
					return runtime.SmallInt{Value: value.Value.Int64()}, nil
				}
				return value, nil
			}
		}
	}
	return nil, fmt.Errorf("enum backing value must be a string or int literal")
}

// foldInterfaceDefaultsIntoEnum compiles an interface's default method bodies
// into the enum's method table for any method the enum leaves unimplemented.
func (e *Evaluator) foldInterfaceDefaultsIntoEnum(enum *runtime.EnumDef, iface *runtime.Interface, env *runtime.Environment) error {
	for _, parent := range iface.Parents {
		if err := e.foldInterfaceDefaultsIntoEnum(enum, parent, env); err != nil {
			return err
		}
	}
	for _, def := range iface.Defaults {
		key := strings.ToLower(def.Name.Value)
		if len(enum.Methods[key]) > 0 {
			continue
		}
		if err := checkEnumMethodCollision(enum, def.Name.Value); err != nil {
			return err
		}
		fn := runtime.Function{Name: def.Name.Value, Doc: def.Doc, TypeParameters: typeParameterNames(def.Generics), TypeParamConstraints: typeParamConstraints(def.Generics), Parameters: e.resolveParameters(def.Parameters), ReturnType: e.resolveTypeRef(def.ReturnType), Body: def.Body, Env: env, Decorators: def.Decorators, Target: "method", Async: def.Async, IsGenerator: blockContainsYield(def.Body), ForwardThis: true}
		enum.Methods[key] = append(enum.Methods[key], fn)
	}
	return nil
}

func (e *Evaluator) lookupEnumMethod(enum *runtime.EnumDef, name string) (runtime.Function, bool) {
	methods := enum.Methods[strings.ToLower(name)]
	if len(methods) == 0 {
		return runtime.Function{}, false
	}
	return methods[0], true
}

// checkEnumMethodCollision rejects a method whose name shadows a variant's
// built-in data accessor (`variant`, `fields`, or a numeric field index).
func checkEnumMethodCollision(enum *runtime.EnumDef, name string) error {
	switch strings.ToLower(name) {
	case "variant", "fields":
		return fmt.Errorf("enum %s method %q collides with a built-in variant accessor", enum.Name, name)
	case "value":
		if enum.BackingType != "" {
			return fmt.Errorf("enum %s method %q collides with a built-in variant accessor", enum.Name, name)
		}
	}
	return nil
}

func interfaceHasDefault(iface *runtime.Interface, name string) bool {
	for _, def := range iface.Defaults {
		if def.Name != nil && strings.EqualFold(def.Name.Value, name) {
			return true
		}
	}
	for _, parent := range iface.Parents {
		if interfaceHasDefault(parent, name) {
			return true
		}
	}
	return false
}

func validateEnumInterfaceImplementation(enum *runtime.EnumDef, iface *runtime.Interface) error {
	for _, parent := range iface.Parents {
		if err := validateEnumInterfaceImplementation(enum, parent); err != nil {
			return err
		}
	}
	for _, sig := range iface.Methods {
		found := false
		for _, method := range enum.Methods[strings.ToLower(sig.Name.Value)] {
			if len(method.Parameters) == len(sig.Parameters) {
				found = true
				break
			}
		}
		if !found {
			// An interface default supplies the body when the enum omits it.
			if interfaceHasDefault(iface, sig.Name.Value) {
				continue
			}
			return fmt.Errorf("enum %s implements %s but is missing compatible method %s", enum.Name, iface.Name, sig.Name.Value)
		}
	}
	return nil
}

func enumVariantValue(enum *runtime.EnumDef, name string) (runtime.Value, error) {
	for _, variant := range enum.Variants {
		if !strings.EqualFold(variant.Name, name) {
			continue
		}
		if variant.FieldCount == 0 {
			return runtime.EnumVariant{Enum: enum, Variant: variant.Name}, nil
		}
		capturedEnum := enum
		capturedName := variant.Name
		return runtime.Function{
			Name: enum.Name + "." + variant.Name,
			Native: func(_ *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
				return runtime.EnumVariant{Enum: capturedEnum, Variant: capturedName, Fields: args}, nil
			},
		}, nil
	}
	return nil, fmt.Errorf("enum %s has no variant %s", enum.Name, name)
}

func enumVariantField(ev runtime.EnumVariant, name string) (runtime.Value, error) {
	switch name {
	case "variant":
		return runtime.String{Value: ev.Variant}, nil
	case "fields":
		return &runtime.List{Elements: ev.Fields}, nil
	case "value":
		if value, ok := runtime.EnumVariantBackingValue(ev); ok {
			return value, nil
		}
	}
	return nil, fmt.Errorf("enum variant %s.%s has no field %s", ev.Enum.Name, ev.Variant, name)
}

func (e *Evaluator) buildClass(stmt *ast.ClassStatement, env *runtime.Environment) (*runtime.Class, error) {
	classTypeParams := typeParameterNames(stmt.Generics)
	classTypeParamConstraints := typeParamConstraints(stmt.Generics)
	class := &runtime.Class{
		Name:                 stmt.Name.Value,
		Doc:                  stmt.Doc,
		TypeParameters:       classTypeParams,
		TypeParamConstraints: classTypeParamConstraints,
		Decorators:           stmt.Decorators,
		Fields:               []runtime.Field{},
		Methods:              map[string][]runtime.Function{},
		StaticMethods:        map[string][]runtime.Function{},
		StaticValues:         map[string]runtime.Value{},
		Env:                  env,
		DefinitionModule:     e.currentModule,
		DefinitionLine:       stmt.Token.Line,
		DefinitionColumn:     stmt.Token.Column,
	}
	if stmt.Extends != nil {
		if isBuiltinErrorClass(stmt.Extends.Name) {
			class.Parent = e.getErrorSentinel(stmt.Extends.Name)
			e.errorClassParents[class.Name] = stmt.Extends.Name
		} else {
			parentValue, ok, err := e.resolveTypeValue(stmt.Extends, env)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("parent class %q is not declared", stmt.Extends.Name)
			}
			parent, ok := parentValue.(*runtime.Class)
			if !ok {
				return nil, fmt.Errorf("%q is not a class", stmt.Extends.Name)
			}
			class.Parent = parent
			if len(stmt.Extends.Arguments) > 0 {
				args := make([]string, 0, len(stmt.Extends.Arguments))
				for _, arg := range stmt.Extends.Arguments {
					args = append(args, arg.String())
				}
				class.ParentArguments = args
			}
			if isErrorDerived(class) {
				e.errorClassParents[class.Name] = parent.Name
			}
		}
	}
	implementedIfaces := make([]*runtime.Interface, 0, len(stmt.Implements))
	for _, ifaceRef := range stmt.Implements {
		ifaceValue, ok, err := e.resolveTypeValue(ifaceRef, env)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("interface %q is not declared", ifaceRef.Name)
		}
		iface, ok := ifaceValue.(*runtime.Interface)
		if !ok {
			return nil, fmt.Errorf("%q is not an interface", ifaceRef.Name)
		}
		class.Implements = append(class.Implements, iface)
		implementedIfaces = append(implementedIfaces, iface)
	}
	if err := mergeInterfaceMembers(stmt, implementedIfaces); err != nil {
		return nil, err
	}
	for _, member := range stmt.Members {
		switch member := member.(type) {
		case *ast.DeclarationStatement:
			if member.Name == nil {
				continue
			}
			if member.Kind == "static const" || member.Kind == "static let" {
				value := runtime.Value(runtime.Null{})
				if member.Value != nil {
					evaluated, err := e.evalExpression(member.Value, env)
					if err != nil {
						return nil, err
					}
					value = evaluated
				}
				class.StaticValues[member.Name.Value] = value
				continue
			}
			if strings.HasPrefix(member.Kind, "static ") {
				return nil, fmt.Errorf("only static const and static let class members are supported")
			}
			fieldDecs := member.Decorators
			if hasImmutableFieldDecoratorEval(member.Decorators) {
				if member.Value != nil {
					return nil, fmt.Errorf("@immutable field %q may not declare a default value", member.Name.Value)
				}
				class.ImmutableFields = append(class.ImmutableFields, member.Name.Value)
				fieldDecs = withoutImmutableDecoratorEval(member.Decorators)
			}
			class.Fields = append(class.Fields, runtime.Field{Name: member.Name.Value, Type: member.Type, Default: member.Value, Doc: member.Doc, Decorators: fieldDecs})
		case *ast.FunctionStatement:
			target := "method"
			if member.Static {
				target = "staticMethod"
			}
			methodTypeParams := append(typeParameterNames(member.Generics), classTypeParams...)
			fn := runtime.Function{Name: member.Name.Value, Doc: member.Doc, TypeParameters: methodTypeParams, TypeParamConstraints: mergeTypeParamConstraints(typeParamConstraints(member.Generics), classTypeParamConstraints), Parameters: e.resolveParameters(member.Parameters), ReturnType: e.resolveTypeRef(member.ReturnType), Body: member.Body, Env: env, Decorators: member.Decorators, Target: target, Async: member.Async, IsGenerator: blockContainsYield(member.Body), ForwardThis: !member.Static}
			decorated, err := e.applyCallableFunctionDecorators(fn, member.Decorators, env)
			if err != nil {
				return nil, err
			}
			fn = decorated
			fn.OwnerClass = class
			if member.Static {
				key := strings.ToLower(member.Name.Value)
				class.StaticMethods[key] = append(class.StaticMethods[key], fn)
				continue
			}
			if strings.EqualFold(member.Name.Value, stmt.Name.Value) {
				class.Constructors = append(class.Constructors, fn)
			} else {
				key := strings.ToLower(member.Name.Value)
				class.Methods[key] = append(class.Methods[key], fn)
			}
		default:
			return nil, fmt.Errorf("unsupported class member %T", member)
		}
	}
	if stmt.Destructor != nil {
		dtor := stmt.Destructor
		methodTypeParams := append(typeParameterNames(dtor.Generics), classTypeParams...)
		fn := runtime.Function{Name: "~" + dtor.Name.Value, Doc: dtor.Doc, TypeParameters: methodTypeParams, TypeParamConstraints: mergeTypeParamConstraints(typeParamConstraints(dtor.Generics), classTypeParamConstraints), Parameters: e.resolveParameters(dtor.Parameters), ReturnType: e.resolveTypeRef(dtor.ReturnType), Body: dtor.Body, Env: env, Decorators: dtor.Decorators, Target: "method", Async: dtor.Async, IsGenerator: false, ForwardThis: true}
		decorated, err := e.applyCallableFunctionDecorators(fn, dtor.Decorators, env)
		if err != nil {
			return nil, err
		}
		fn = decorated
		fn.OwnerClass = class
		class.Destructor = &fn
	}
	for _, iface := range class.Implements {
		if err := validateInterfaceImplementation(class, iface); err != nil {
			return nil, err
		}
	}
	return class, nil
}

func (e *Evaluator) resolveTypeValue(ref *ast.TypeRef, env *runtime.Environment) (runtime.Value, bool, error) {
	if ref == nil || ref.Operator != "" {
		return nil, false, nil
	}
	if moduleName, exportName, ok := strings.Cut(ref.Name, "."); ok {
		moduleValue, exists := env.Get(moduleName)
		if !exists {
			return nil, false, nil
		}
		module, ok := moduleValue.(*runtime.Module)
		if !ok {
			return nil, false, fmt.Errorf("%s is not a module", moduleName)
		}
		value, exists := module.Exports[exportName]
		return value, exists, nil
	}
	value, ok := env.Get(ref.Name)
	return value, ok, nil
}

func isErrorDerived(class *runtime.Class) bool {
	for c := class; c != nil; c = c.Parent {
		if isBuiltinErrorClass(c.Name) {
			return true
		}
	}
	return false
}

func (e *Evaluator) instantiateClass(class *runtime.Class, args []runtime.Value) (runtime.Value, error) {
	if reason, abstract := classAbstractnessReason(class); abstract {
		return nil, thrownError{value: e.withTrace(runtime.Error{Class: "RuntimeError", Message: reason, Parents: []string{"RuntimeError", "Error"}})}
	}
	if isErrorDerived(class) {
		if len(class.Fields) > 0 || len(class.Constructors) > 0 {
			return e.instantiateUserErrorClass(class, args)
		}
		msg := ""
		for _, arg := range args {
			if s, ok := arg.(runtime.String); ok {
				msg = s.Value
				break
			}
		}
		return runtime.Error{Class: class.Name, Message: msg, Parents: e.errorParentChain(class.Name)}, nil
	}
	instance := &runtime.Instance{Class: class, Fields: map[string]runtime.Value{}}
	if err := e.initializeFields(instance, class); err != nil {
		return nil, err
	}
	if len(class.Constructors) > 0 {
		constructor, err := selectOverload(class.Name, class.Constructors, args)
		if err != nil {
			return nil, err
		}
		if err := e.applyAutoParentConstructor(instance, constructor); err != nil {
			return nil, err
		}
		if _, err := e.applyFunctionWithThis(constructor, args, instance); err != nil {
			return nil, err
		}
	} else if len(args) != 0 {
		return nil, fmt.Errorf("%s constructor expects no arguments", class.Name)
	}
	if err := e.checkClassTypeParamConstraints(class, instance.TypeBindings); err != nil {
		return nil, err
	}
	if class.Immutable {
		instance.Frozen = true
	}
	for _, f := range class.ImmutableFields {
		instance.LockField(f)
	}
	if class.Destructor != nil {
		e.destructibleInstances = append(e.destructibleInstances, instance)
	}
	return instance, nil
}

// instantiateUserErrorClass runs the full constructor path for user-defined error
// subclasses that declare custom fields or a constructor. After construction, the
// instance fields (minus the __parentMsg sentinel) are preserved in Error.Fields.
func (e *Evaluator) instantiateUserErrorClass(class *runtime.Class, args []runtime.Value) (runtime.Value, error) {
	instance := &runtime.Instance{Class: class, Fields: map[string]runtime.Value{}}
	if err := e.initializeFields(instance, class); err != nil {
		return nil, err
	}
	if len(class.Constructors) > 0 {
		constructor, err := selectOverload(class.Name, class.Constructors, args)
		if err != nil {
			return nil, err
		}
		if _, err := e.applyFunctionWithThis(constructor, args, instance); err != nil {
			return nil, err
		}
	} else if len(args) != 0 {
		return nil, fmt.Errorf("%s constructor expects no arguments", class.Name)
	}
	msg := ""
	if m, ok := instance.Fields["__parentMsg"]; ok {
		if s, ok := m.(runtime.String); ok {
			msg = s.Value
		}
		delete(instance.Fields, "__parentMsg")
	} else {
		for _, arg := range args {
			if s, ok := arg.(runtime.String); ok {
				msg = s.Value
				break
			}
		}
	}
	var fields map[string]runtime.Value
	if len(instance.Fields) > 0 {
		fields = instance.Fields
	}
	return runtime.Error{Class: class.Name, Message: msg, Fields: fields, Parents: e.errorParentChain(class.Name)}, nil
}

// registerGlobalClass adds a class to the cross-module registry so
// reflect.class(name) can find it from any module. Idempotent: classes
// re-imported across modules just overwrite the entry with the same
// pointer (Geblang class identity is global by name).
func (e *Evaluator) registerGlobalClass(class *runtime.Class) {
	if class == nil || class.Name == "" {
		return
	}
	e.globalClasses[class.Name] = class
}

func (e *Evaluator) registerTopLevelFunction(stmt ast.Statement, env *runtime.Environment) {
	if exp, ok := stmt.(*ast.ExportStatement); ok {
		stmt = exp.Statement
	}
	fnStmt, ok := stmt.(*ast.FunctionStatement)
	if !ok || fnStmt.Name == nil {
		return
	}
	value, found := env.Get(fnStmt.Name.Value)
	if !found {
		return
	}
	switch v := value.(type) {
	case runtime.Function:
		e.globalFunctions[fnStmt.Name.Value] = v
	case runtime.OverloadedFunction:
		// Last-write-wins by bare name, mirroring globalClasses.
		if n := len(v.Overloads); n > 0 {
			e.globalFunctions[fnStmt.Name.Value] = v.Overloads[n-1]
		}
	}
}

// errorParentChain returns the parent class name list (immediate parent
// first) for an error-derived class, used when constructing a
// runtime.Error value so cross-module `instanceof` / typed-parameter
// matching can walk the chain without re-reading the evaluator state.
func (e *Evaluator) errorParentChain(className string) []string {
	var parents []string
	visited := map[string]bool{className: true}
	for parent := e.errorParent(className); parent != ""; parent = e.errorParent(parent) {
		if visited[parent] {
			break
		}
		visited[parent] = true
		parents = append(parents, parent)
	}
	return parents
}

func (e *Evaluator) instantiateClassFromCall(class *runtime.Class, call *ast.CallExpression, env *runtime.Environment, declared ...*ast.TypeRef) (runtime.Value, error) {
	// Declaration-annotation args validate like explicit call-site type args.
	if len(call.TypeArguments) == 0 && len(declared) > 0 && declared[0] != nil && declared[0] == e.declAnnotation {
		exp := declared[0]
		if exp.Operator == "" && len(exp.Arguments) > 0 && len(class.TypeParameters) > 0 && typeNamesEqual(exp.Name, class.Name) {
			resolved := make([]*ast.TypeRef, len(exp.Arguments))
			for i, a := range exp.Arguments {
				resolved[i] = a
				if a != nil && a.Operator == "" && a.Name != "" {
					if bound, ok := env.GetTypeBinding(a.Name); ok && bound != "" {
						resolved[i] = &ast.TypeRef{Token: a.Token, Name: bound, Nullable: a.Nullable}
					}
				}
			}
			copied := *call
			copied.TypeArguments = resolved
			call = &copied
		}
	}
	if reason, abstract := classAbstractnessReason(class); abstract {
		return nil, thrownError{value: e.withTrace(runtime.Error{Class: "RuntimeError", Message: reason, Parents: []string{"RuntimeError", "Error"}})}
	}
	if isErrorDerived(class) {
		if len(class.Fields) > 0 || len(class.Constructors) > 0 {
			args, err := e.evalCallArguments(call, env)
			if err != nil {
				return nil, err
			}
			return e.instantiateUserErrorClass(class, args)
		}
		msg := ""
		for _, callArg := range call.Arguments {
			argVal, err := e.evalExpression(callArg.Value, env)
			if err != nil {
				return nil, err
			}
			if s, ok := argVal.(runtime.String); ok {
				msg = s.Value
				break
			}
		}
		return runtime.Error{Class: class.Name, Message: msg, Parents: e.errorParentChain(class.Name)}, nil
	}
	instance := &runtime.Instance{Class: class, Fields: map[string]runtime.Value{}}
	// Inherit type bindings from the parent class's declaration (e.g.
	// `class Sub extends Base<string>` propagates {T: "string"} through
	// the parent chain). Done first so explicit declaration annotations
	// and constructor-argument inference below can override.
	for c := class; c != nil; c = c.Parent {
		if c.Parent == nil || len(c.ParentArguments) == 0 || len(c.Parent.TypeParameters) == 0 {
			continue
		}
		if instance.TypeBindings == nil {
			instance.TypeBindings = map[string]string{}
		}
		for i, name := range c.Parent.TypeParameters {
			if i >= len(c.ParentArguments) {
				break
			}
			if _, exists := instance.TypeBindings[name]; !exists {
				instance.TypeBindings[name] = c.ParentArguments[i]
			}
		}
	}
	// Pre-populate type bindings from the call site's explicit type arguments
	// (e.g. `Repository<Dog>(...)`); failing that, fall back to the declared
	// LHS annotation (e.g. `Box<int> b = Box(...)`). Either path takes
	// priority over inference from constructor args, which fills in any
	// remaining bindings further down.
	// A type-arg name that is itself a bound generic param (an enclosing
	// generic function's T) resolves to the call site's concrete binding.
	resolveArgName := func(name string) string {
		if bound, ok := env.GetTypeBinding(name); ok && bound != "" {
			return bound
		}
		return name
	}
	if len(call.TypeArguments) > 0 && len(class.TypeParameters) > 0 {
		if instance.TypeBindings == nil {
			instance.TypeBindings = map[string]string{}
		}
		for i, arg := range call.TypeArguments {
			if i >= len(class.TypeParameters) {
				break
			}
			if arg != nil && arg.Operator == "" && arg.Name != "" {
				instance.TypeBindings[class.TypeParameters[i]] = resolveArgName(arg.Name)
			}
		}
	} else if len(declared) > 0 && declared[0] != nil {
		exp := declared[0]
		if exp.Operator == "" && len(exp.Arguments) > 0 && len(class.TypeParameters) > 0 {
			if instance.TypeBindings == nil {
				instance.TypeBindings = map[string]string{}
			}
			for i, arg := range exp.Arguments {
				if i >= len(class.TypeParameters) {
					break
				}
				if arg != nil && arg.Operator == "" && arg.Name != "" {
					instance.TypeBindings[class.TypeParameters[i]] = resolveArgName(arg.Name)
				}
			}
		}
	}
	if err := e.initializeFields(instance, class); err != nil {
		return nil, err
	}
	if len(class.Constructors) == 0 {
		if len(call.Arguments) != 0 {
			return nil, fmt.Errorf("%s constructor expects no arguments", class.Name)
		}
		if err := e.checkClassTypeParamConstraints(class, instance.TypeBindings); err != nil {
			return nil, err
		}
		if class.Immutable {
			instance.Frozen = true
		}
		for _, f := range class.ImmutableFields {
			instance.LockField(f)
		}
		if class.Destructor != nil {
			e.destructibleInstances = append(e.destructibleInstances, instance)
		}
		return instance, nil
	}
	if _, err := e.applyOverloadedFunction(class.Name, class.Constructors, call, env, instance, nil); err != nil {
		return nil, err
	}
	if err := e.checkClassTypeParamConstraints(class, instance.TypeBindings); err != nil {
		return nil, err
	}
	if class.Immutable {
		instance.Frozen = true
	}
	for _, f := range class.ImmutableFields {
		instance.LockField(f)
	}
	if class.Destructor != nil {
		e.destructibleInstances = append(e.destructibleInstances, instance)
	}
	return instance, nil
}

func (e *Evaluator) applyAutoParentConstructor(instance *runtime.Instance, constructor runtime.Function) error {
	if instance == nil || instance.Class == nil || instance.Class.Parent == nil || len(instance.Class.Parent.Constructors) == 0 {
		return nil
	}
	if evaluatorContainsParentConstructorCall(constructor.Body) {
		return nil
	}
	parent := instance.Class.Parent
	parentConstructor, err := selectOverload(parent.Name, parent.Constructors, nil)
	if err != nil {
		return err
	}
	if instance.LockedFields == nil {
		instance.LockedFields = map[string]bool{}
	}
	// Share LockedFields with the throwaway so grandparent locks land on the real instance.
	throwaway := &runtime.Instance{Class: parent, Fields: instance.Fields, LockedFields: instance.LockedFields, TypeBindings: instance.TypeBindings}
	if err := e.applyAutoParentConstructor(throwaway, parentConstructor); err != nil {
		return err
	}
	if _, err := e.applyFunctionWithThis(parentConstructor, nil, instance); err != nil {
		return err
	}
	for _, f := range parent.ImmutableFields {
		instance.LockField(f)
	}
	return nil
}

func evaluatorContainsParentConstructorCall(block *ast.BlockStatement) bool {
	if block == nil {
		return false
	}
	for _, stmt := range block.Statements {
		if evaluatorStatementContainsParentConstructorCall(stmt) {
			return true
		}
	}
	return false
}

func evaluatorStatementContainsParentConstructorCall(stmt ast.Statement) bool {
	switch stmt := stmt.(type) {
	case *ast.BlockStatement:
		return evaluatorContainsParentConstructorCall(stmt)
	case *ast.ExportStatement:
		return evaluatorStatementContainsParentConstructorCall(stmt.Statement)
	case *ast.DeclarationStatement:
		return evaluatorExpressionContainsParentConstructorCall(stmt.Value)
	case *ast.ExpressionStatement:
		return evaluatorExpressionContainsParentConstructorCall(stmt.Expression)
	case *ast.ReturnStatement:
		return evaluatorExpressionContainsParentConstructorCall(stmt.Value)
	case *ast.YieldStatement:
		return evaluatorExpressionContainsParentConstructorCall(stmt.Value)
	case *ast.SimpleStatement:
		return evaluatorExpressionContainsParentConstructorCall(stmt.Value)
	case *ast.IfStatement:
		if evaluatorExpressionContainsParentConstructorCall(stmt.Condition) || evaluatorContainsParentConstructorCall(stmt.Consequence) || evaluatorContainsParentConstructorCall(stmt.Alternative) {
			return true
		}
		for _, elseif := range stmt.ElseIfs {
			if evaluatorExpressionContainsParentConstructorCall(elseif.Condition) || evaluatorContainsParentConstructorCall(elseif.Body) {
				return true
			}
		}
	case *ast.WhileStatement:
		return evaluatorExpressionContainsParentConstructorCall(stmt.Condition) || evaluatorContainsParentConstructorCall(stmt.Body)
	case *ast.ForStatement:
		return evaluatorStatementContainsParentConstructorCall(stmt.Init) ||
			evaluatorExpressionContainsParentConstructorCall(stmt.Condition) ||
			evaluatorStatementContainsParentConstructorCall(stmt.Update) ||
			evaluatorExpressionContainsParentConstructorCall(stmt.Iterable) ||
			evaluatorExpressionContainsParentConstructorCall(stmt.Step) ||
			evaluatorContainsParentConstructorCall(stmt.Body)
	case *ast.MatchStatement:
		if evaluatorExpressionContainsParentConstructorCall(stmt.Expr) {
			return true
		}
		for _, matchCase := range stmt.Cases {
			if evaluatorExpressionContainsParentConstructorCall(matchCase.Pattern) ||
				evaluatorExpressionContainsParentConstructorCall(matchCase.Guard) ||
				evaluatorExpressionContainsParentConstructorCall(matchCase.Value) ||
				evaluatorContainsParentConstructorCall(matchCase.Body) {
				return true
			}
		}
	case *ast.TryStatement:
		if evaluatorContainsParentConstructorCall(stmt.Body) || evaluatorContainsParentConstructorCall(stmt.Finally) {
			return true
		}
		for _, catch := range stmt.Catches {
			if evaluatorContainsParentConstructorCall(catch.Body) {
				return true
			}
		}
	}
	return false
}

func evaluatorExpressionContainsParentConstructorCall(expr ast.Expression) bool {
	switch expr := expr.(type) {
	case nil:
		return false
	case *ast.CallExpression:
		if ident, ok := expr.Callee.(*ast.Identifier); ok && ident.Value == "parent" {
			return true
		}
		if evaluatorExpressionContainsParentConstructorCall(expr.Callee) {
			return true
		}
		for _, arg := range expr.Arguments {
			if evaluatorExpressionContainsParentConstructorCall(arg.Value) {
				return true
			}
		}
	case *ast.PrefixExpression:
		return evaluatorExpressionContainsParentConstructorCall(expr.Right)
	case *ast.PostfixExpression:
		return evaluatorExpressionContainsParentConstructorCall(expr.Left)
	case *ast.InfixExpression:
		return evaluatorExpressionContainsParentConstructorCall(expr.Left) || evaluatorExpressionContainsParentConstructorCall(expr.Right)
	case *ast.AssignmentExpression:
		return evaluatorExpressionContainsParentConstructorCall(expr.Left) || evaluatorExpressionContainsParentConstructorCall(expr.Value)
	case *ast.SelectorExpression:
		return evaluatorExpressionContainsParentConstructorCall(expr.Object)
	case *ast.IndexExpression:
		return evaluatorExpressionContainsParentConstructorCall(expr.Left) || evaluatorExpressionContainsParentConstructorCall(expr.Index)
	case *ast.ListLiteral:
		for _, element := range expr.Elements {
			if evaluatorExpressionContainsParentConstructorCall(element) {
				return true
			}
		}
	case *ast.DictLiteral:
		for _, entry := range expr.Entries {
			if evaluatorExpressionContainsParentConstructorCall(entry.Key) || evaluatorExpressionContainsParentConstructorCall(entry.Value) {
				return true
			}
		}
	case *ast.SetLiteral:
		for _, element := range expr.Elements {
			if evaluatorExpressionContainsParentConstructorCall(element) {
				return true
			}
		}
	case *ast.RangeExpression:
		return evaluatorExpressionContainsParentConstructorCall(expr.Start) ||
			evaluatorExpressionContainsParentConstructorCall(expr.End) ||
			evaluatorExpressionContainsParentConstructorCall(expr.Step)
	case *ast.FunctionLiteral:
		return false
	case *ast.MatchExpression:
		if evaluatorExpressionContainsParentConstructorCall(expr.Expr) {
			return true
		}
		for _, matchCase := range expr.Cases {
			if evaluatorExpressionContainsParentConstructorCall(matchCase.Pattern) ||
				evaluatorExpressionContainsParentConstructorCall(matchCase.Guard) ||
				evaluatorExpressionContainsParentConstructorCall(matchCase.Value) ||
				evaluatorContainsParentConstructorCall(matchCase.Body) {
				return true
			}
		}
	case *ast.SpreadExpression:
		return evaluatorExpressionContainsParentConstructorCall(expr.Value)
	case *ast.AwaitExpression:
		return evaluatorExpressionContainsParentConstructorCall(expr.Value)
	case *ast.CastExpression:
		return evaluatorExpressionContainsParentConstructorCall(expr.Value)
	case *ast.TernaryExpression:
		return evaluatorExpressionContainsParentConstructorCall(expr.Condition) ||
			evaluatorExpressionContainsParentConstructorCall(expr.ThenExpr) ||
			evaluatorExpressionContainsParentConstructorCall(expr.ElseExpr)
	}
	return false
}

func hasImmutableFieldDecoratorEval(decorators []ast.Decorator) bool {
	for _, dec := range decorators {
		if dec.Name != nil && dec.Name.Value == "immutable" && len(dec.Arguments) == 0 {
			return true
		}
	}
	return false
}

func withoutImmutableDecoratorEval(decorators []ast.Decorator) []ast.Decorator {
	out := make([]ast.Decorator, 0, len(decorators))
	for _, dec := range decorators {
		if dec.Name != nil && dec.Name.Value == "immutable" && len(dec.Arguments) == 0 {
			continue
		}
		out = append(out, dec)
	}
	return out
}

func (e *Evaluator) applyFieldDecorators(class *runtime.Class, name string, value runtime.Value, env *runtime.Environment) (runtime.Value, error) {
	for c := class; c != nil; c = c.Parent {
		for _, field := range c.Fields {
			if !strings.EqualFold(field.Name, name) {
				continue
			}
			for i := len(field.Decorators) - 1; i >= 0; i-- {
				dec := field.Decorators[i]
				if dec.Name == nil {
					continue
				}
				decoratorValue, found := env.Get(dec.Name.Value)
				if !found && e.globalClasses != nil {
					if v, ok := envOrGlobalFunc(env, dec.Name.Value); ok {
						decoratorValue = v
						found = true
					}
				}
				if !found {
					continue
				}
				callArgs := []runtime.Value{}
				for _, arg := range dec.Arguments {
					v, err := e.evalExpression(arg.Value, env)
					if err != nil {
						return nil, fmt.Errorf("field decorator @%s: %w", dec.Name.Value, err)
					}
					callArgs = append(callArgs, v)
				}
				callArgs = append(callArgs, value)
				result, err := e.applyCallableNoCall(decoratorValue, callArgs)
				if err != nil {
					return nil, err
				}
				value = result
			}
			return value, nil
		}
	}
	return value, nil
}

func envOrGlobalFunc(env *runtime.Environment, name string) (runtime.Value, bool) {
	if v, ok := env.Get(name); ok {
		return v, true
	}
	return nil, false
}

// Apply a callable runtime.Value to args without going through an
// AST CallExpression; used by field decorators which need to invoke
// the transform from inside an assignment path.
func (e *Evaluator) applyCallableNoCall(callee runtime.Value, args []runtime.Value) (runtime.Value, error) {
	switch fn := callee.(type) {
	case runtime.Function:
		return e.applyFunction(fn, args)
	case runtime.OverloadedFunction:
		for _, overload := range fn.Overloads {
			bound, ok := bindEvaluatedFunctionCallArguments(overload, evaluatedCallArgsFromValues(args))
			if !ok || !functionArgumentsMatch(overload, bound) {
				continue
			}
			return e.applyFunction(overload, bound)
		}
		return nil, fmt.Errorf("no matching overload for %s", fn.Name)
	}
	return nil, fmt.Errorf("decorator is not callable: %s", callee.TypeName())
}

func evaluatedCallArgsFromValues(args []runtime.Value) []evaluatedCallArg {
	result := make([]evaluatedCallArg, len(args))
	for i, v := range args {
		result[i] = evaluatedCallArg{value: v}
	}
	return result
}

func (e *Evaluator) initializeFields(instance *runtime.Instance, class *runtime.Class) error {
	if class.Parent != nil {
		if err := e.initializeFields(instance, class.Parent); err != nil {
			return err
		}
	}
	fieldEnv := runtime.NewEnclosedEnvironment(class.Env)
	for _, field := range class.Fields {
		value := runtime.Value(runtime.Null{})
		if field.Default != nil {
			evaluated, err := e.evalExpression(field.Default, fieldEnv)
			if err != nil {
				return err
			}
			value = evaluated
		}
		instance.Fields[field.Name] = value
	}
	return nil
}

func lookupMethod(class *runtime.Class, name string) (runtime.Function, bool) {
	methods := lookupMethodOverloads(class, name)
	if len(methods) == 0 {
		return runtime.Function{}, false
	}
	return methods[0], true
}

func lookupMethodOverloads(class *runtime.Class, name string) []runtime.Function {
	var methods []runtime.Function
	seen := map[string]bool{}
	for current := class; current != nil; current = current.Parent {
		overloads, ok := current.Methods[strings.ToLower(name)]
		if !ok {
			continue
		}
		for _, method := range overloads {
			key := functionParameterSignatureKey(method)
			if seen[key] {
				continue
			}
			seen[key] = true
			methods = append(methods, method)
		}
	}
	return methods
}

func functionParameterSignatureKey(fn runtime.Function) string {
	parts := make([]string, 0, len(fn.Parameters))
	for _, param := range fn.Parameters {
		parts = append(parts, strings.ToLower(typeRefSignature(param.Type)))
	}
	return strings.Join(parts, ",")
}

func typeRefSignature(typ *ast.TypeRef) string {
	if typ == nil {
		return "any"
	}
	return typ.String()
}

func lookupStaticMethod(class *runtime.Class, name string) (runtime.Function, bool) {
	methods := lookupStaticMethodOverloads(class, name)
	if len(methods) == 0 {
		return runtime.Function{}, false
	}
	return methods[0], true
}

func lookupStaticMethodOverloads(class *runtime.Class, name string) []runtime.Function {
	for current := class; current != nil; current = current.Parent {
		methods, ok := current.StaticMethods[strings.ToLower(name)]
		if ok {
			return methods
		}
	}
	return nil
}

func lookupStaticValue(class *runtime.Class, name string) (runtime.Value, bool) {
	for current := class; current != nil; current = current.Parent {
		value, ok := current.StaticValues[name]
		if ok {
			return value, true
		}
	}
	return nil, false
}

func classImplementsInterface(class *runtime.Class, name string) bool {
	return runtime.ClassImplementsInterface(class, name)
}

func interfaceMatches(iface *runtime.Interface, name string) bool {
	return runtime.InterfaceMatches(iface, name)
}

func validateInterfaceImplementation(class *runtime.Class, iface *runtime.Interface) error {
	for _, parent := range iface.Parents {
		if err := validateInterfaceImplementation(class, parent); err != nil {
			return err
		}
	}
	for _, sig := range iface.Methods {
		methods := lookupMethodOverloads(class, sig.Name.Value)
		found := false
		for _, method := range methods {
			if len(method.Parameters) == len(sig.Parameters) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("class %s implements %s but is missing compatible method %s", class.Name, iface.Name, sig.Name.Value)
		}
	}
	return nil
}

func (e *Evaluator) resolveTypeRef(ref *ast.TypeRef) *ast.TypeRef {
	if ref == nil {
		return nil
	}
	out := cloneTypeRef(ref)
	if out.Operator != "" {
		out.Left = e.resolveTypeRef(out.Left)
		out.Right = e.resolveTypeRef(out.Right)
		return out
	}
	resolved, ok := e.typeAliases[strings.ToLower(out.Name)]
	if ok {
		alias := cloneTypeRef(resolved)
		alias.Nullable = alias.Nullable || out.Nullable
		alias.ListAlias = alias.ListAlias || out.ListAlias
		return alias
	}
	for i, arg := range out.Arguments {
		out.Arguments[i] = e.resolveTypeRef(arg)
	}
	return out
}

func (e *Evaluator) resolveParameters(params []ast.Parameter) []ast.Parameter {
	out := make([]ast.Parameter, len(params))
	for i, param := range params {
		out[i] = param
		out[i].Type = e.resolveTypeRef(param.Type)
	}
	return out
}

func (e *Evaluator) resolveFunctionSignatures(sigs []*ast.FunctionSignature) []*ast.FunctionSignature {
	out := make([]*ast.FunctionSignature, len(sigs))
	for i, sig := range sigs {
		copied := *sig
		copied.Parameters = e.resolveParameters(sig.Parameters)
		copied.ReturnType = e.resolveTypeRef(sig.ReturnType)
		out[i] = &copied
	}
	return out
}

func cloneTypeRef(ref *ast.TypeRef) *ast.TypeRef {
	if ref == nil {
		return nil
	}
	out := *ref
	if len(ref.Arguments) > 0 {
		out.Arguments = make([]*ast.TypeRef, len(ref.Arguments))
		for i, arg := range ref.Arguments {
			out.Arguments[i] = cloneTypeRef(arg)
		}
	}
	out.Left = cloneTypeRef(ref.Left)
	out.Right = cloneTypeRef(ref.Right)
	return &out
}

func typeParameterNames(params []*ast.TypeParam) []string {
	names := make([]string, 0, len(params))
	for _, param := range params {
		if param != nil && param.Name != nil {
			names = append(names, param.Name.Value)
		}
	}
	return names
}

func typeParamConstraints(params []*ast.TypeParam) map[string]*ast.TypeRef {
	if len(params) == 0 {
		return nil
	}
	var m map[string]*ast.TypeRef
	for _, param := range params {
		if param != nil && param.Name != nil && param.Constraint != nil {
			if m == nil {
				m = map[string]*ast.TypeRef{}
			}
			m[param.Name.Value] = param.Constraint
		}
	}
	return m
}

func mergeTypeParamConstraints(maps ...map[string]*ast.TypeRef) map[string]*ast.TypeRef {
	var out map[string]*ast.TypeRef
	for _, constraints := range maps {
		for name, constraint := range constraints {
			if out == nil {
				out = map[string]*ast.TypeRef{}
			}
			out[name] = constraint
		}
	}
	return out
}

// classAbstractnessReason reports whether `class` cannot be
// instantiated directly. A class is abstract when it carries the
// @abstract class-level decorator, or any method declared on it or
// an ancestor carries @abstract and no more-derived class provides
// a concrete override.
func classAbstractnessReason(class *runtime.Class) (string, bool) {
	if class == nil {
		return "", false
	}
	if hasDecorator(class.Decorators, "abstract") {
		return "cannot instantiate abstract class " + class.Name, true
	}
	overridden := map[string]bool{}
	abstractDecl := map[string]string{}
	walk := func(c *runtime.Class) {
		for methodName, overloads := range c.Methods {
			isAbstract := false
			for _, fn := range overloads {
				if hasDecorator(fn.Decorators, "abstract") {
					isAbstract = true
					break
				}
			}
			if isAbstract {
				if !overridden[methodName] {
					if _, seen := abstractDecl[methodName]; !seen {
						abstractDecl[methodName] = c.Name
					}
				}
			} else {
				overridden[methodName] = true
				delete(abstractDecl, methodName)
			}
		}
	}
	walk(class)
	for c := class.Parent; c != nil; c = c.Parent {
		walk(c)
	}
	if len(abstractDecl) == 0 {
		return "", false
	}
	var sample, sampleClass string
	for name, owner := range abstractDecl {
		if sample == "" || name < sample {
			sample = name
			sampleClass = owner
		}
	}
	return "cannot instantiate " + class.Name + ": abstract method " + sampleClass + "." + sample + " is not implemented", true
}
