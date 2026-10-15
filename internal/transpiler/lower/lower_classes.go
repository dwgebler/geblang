package lower

import (
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/transpiler/emit"
	"geblang/internal/transpiler/types"
	"sort"
	"strconv"
	"strings"
)

func (l *Lowerer) lowerTaggedEnum(s *ast.EnumStatement) {
	enumName := emit.MangleIdent(s.Name.Value)
	variantNames := make([]string, 0, len(s.Variants))
	for _, v := range s.Variants {
		variantNames = append(variantNames, v.Name.Value)
		l.Module.RegisterTaggedVariant(s.Name.Value, v.Name.Value, len(v.FieldTypes))
	}
	l.Module.RegisterEnum(s.Name.Value, variantNames)
	l.Module.RegisterInterface(s.Name.Value)

	markerMethod := "__gb" + enumName

	// User methods (plus folded interface defaults) become part of the enum
	// interface's method set so an enum value flows into any interface it
	// implements; each variant struct delegates to a single shared impl.
	methods := l.taggedEnumMethods(s)
	for _, m := range methods {
		if !l.checkEnumMethodEmittable(s, m.Name.Value) {
			return
		}
	}

	decls := l.Module.TopDecls()
	decls.WriteString("type ")
	decls.WriteString(enumName)
	decls.WriteLine(" interface {")
	decls.Indent()
	decls.WriteString(markerMethod)
	decls.WriteLine("()")
	for _, m := range methods {
		l.registerEnumMethodSignature(enumName, m)
		l.writeEnumMethodSignature(decls, m)
	}
	decls.Dedent()
	decls.WriteLine("}")
	decls.Newline()

	l.Module.AddImport("fmt")
	for _, v := range s.Variants {
		variantStruct := enumName + emit.MangleIdent(v.Name.Value)
		decls.WriteString("type ")
		decls.WriteString(variantStruct)
		decls.WriteLine(" struct {")
		decls.Indent()
		for i, ft := range v.FieldTypes {
			goTy := types.ToGo(l.resolveTypeRef(ft), l.Module.IntMode)
			l.Module.AddTypeImports(goTy)
			decls.WriteString(fmt.Sprintf("V%d %s", i, goTy.Source))
			decls.WriteLine("")
		}
		decls.Dedent()
		decls.WriteLine("}")
		decls.Newline()

		decls.WriteString("func (")
		decls.WriteString(variantStruct)
		decls.WriteString(") ")
		decls.WriteString(markerMethod)
		decls.WriteLine("() {}")
		decls.Newline()

		decls.WriteString("func New")
		decls.WriteString(variantStruct)
		decls.WriteString("(")
		for i, ft := range v.FieldTypes {
			if i > 0 {
				decls.WriteString(", ")
			}
			decls.WriteString(fmt.Sprintf("v%d ", i))
			goTy := types.ToGo(l.resolveTypeRef(ft), l.Module.IntMode)
			decls.WriteString(goTy.Source)
		}
		decls.WriteString(") ")
		decls.WriteString(variantStruct)
		decls.WriteLine(" {")
		decls.Indent()
		decls.WriteString("return ")
		decls.WriteString(variantStruct)
		decls.WriteString("{")
		for i := range v.FieldTypes {
			if i > 0 {
				decls.WriteString(", ")
			}
			decls.WriteString(fmt.Sprintf("V%d: v%d", i, i))
		}
		decls.WriteLine("}")
		decls.Dedent()
		decls.WriteLine("}")
		decls.Newline()

		decls.WriteString("func (__v ")
		decls.WriteString(variantStruct)
		decls.WriteLine(") String() string {")
		decls.Indent()
		decls.WriteString("return ")
		if len(v.FieldTypes) == 0 {
			decls.WriteLine(strconv.Quote(s.Name.Value + "." + v.Name.Value))
		} else {
			decls.WriteString(strconv.Quote(s.Name.Value + "." + v.Name.Value + "("))
			for i := range v.FieldTypes {
				decls.WriteString(" + fmt.Sprintf(\"%v\", __v.V")
				decls.WriteString(fmt.Sprintf("%d)", i))
				if i+1 < len(v.FieldTypes) {
					decls.WriteString(` + ", "`)
				}
			}
			decls.WriteLine(` + ")"`)
		}
		decls.Dedent()
		decls.WriteLine("}")
		decls.Newline()

		for _, m := range methods {
			l.emitTaggedVariantDelegate(decls, enumName, variantStruct, m)
		}
	}

	for _, m := range methods {
		l.emitTaggedEnumImpl(s.Name.Value, enumName, m)
	}
	l.emitTaggedEnumStatics(s, enumName)
}

// emitTaggedEnumStatics emits values()/fromName() for a tagged enum, restricted
// to the nullary variants: a bare name cannot construct a variant with fields.
// The helpers return the enum interface (nil for an unknown name).
func (l *Lowerer) emitTaggedEnumStatics(s *ast.EnumStatement, enumName string) {
	decls := l.Module.TopDecls()
	decls.WriteString("func " + enumName + "Values() []" + enumName + " { return []" + enumName + "{")
	first := true
	for _, v := range s.Variants {
		if len(v.FieldTypes) != 0 {
			continue
		}
		if !first {
			decls.WriteString(", ")
		}
		first = false
		decls.WriteString("New" + enumName + emit.MangleIdent(v.Name.Value) + "()")
	}
	decls.WriteLine("} }")
	decls.Newline()

	decls.WriteLine("func " + enumName + "FromName(__s string) " + enumName + " {")
	decls.Indent()
	decls.WriteLine("switch __s {")
	for _, v := range s.Variants {
		if len(v.FieldTypes) != 0 {
			continue
		}
		decls.WriteLine("case " + strconv.Quote(v.Name.Value) + ": return New" + enumName + emit.MangleIdent(v.Name.Value) + "()")
	}
	decls.WriteLine("}")
	decls.WriteLine("return nil")
	decls.Dedent()
	decls.WriteLine("}")
	decls.Newline()
}

// taggedEnumMethods returns the enum's declared methods plus any default method
// from an implemented interface it leaves unimplemented (mirroring the fold).
func (l *Lowerer) taggedEnumMethods(s *ast.EnumStatement) []*ast.FunctionStatement {
	defined := map[string]struct{}{}
	var out []*ast.FunctionStatement
	for _, m := range s.Methods {
		if m.Name == nil {
			continue
		}
		defined[strings.ToLower(m.Name.Value)] = struct{}{}
		out = append(out, m)
	}
	out = append(out, l.enumInterfaceDefaults(s, defined)...)
	return out
}

// writeEnumMethodSignature emits one method signature line for the enum
// interface declaration so its method set covers the enum's methods.
func (l *Lowerer) writeEnumMethodSignature(w *emit.Writer, m *ast.FunctionStatement) {
	w.WriteString(emit.MangleIdent(m.Name.Value))
	w.WriteString("(")
	l.emitParamList(w, m.Parameters, l.resolveParamTypes(m.Parameters))
	w.WriteString(")")
	if m.ReturnType != nil {
		w.WriteString(" ")
		goRet := types.ToGo(l.resolveTypeRef(m.ReturnType), l.Module.IntMode)
		l.Module.AddTypeImports(goRet)
		w.WriteString(goRet.Source)
	}
	w.WriteLine("")
}

// emitTaggedVariantDelegate makes a variant struct satisfy the enum interface
// by forwarding the call to the shared impl with the variant as `this`.
func (l *Lowerer) emitTaggedVariantDelegate(w *emit.Writer, enumName, variantStruct string, m *ast.FunctionStatement) {
	method := emit.MangleIdent(m.Name.Value)
	w.WriteString("func (__v ")
	w.WriteString(variantStruct)
	w.WriteString(") ")
	w.WriteString(method)
	w.WriteString("(")
	l.emitParamList(w, m.Parameters, l.resolveParamTypes(m.Parameters))
	w.WriteString(") ")
	if m.ReturnType != nil {
		goRet := types.ToGo(l.resolveTypeRef(m.ReturnType), l.Module.IntMode)
		l.Module.AddTypeImports(goRet)
		w.WriteString(goRet.Source)
		w.WriteString(" ")
	}
	w.WriteLine("{")
	w.Indent()
	if m.ReturnType != nil {
		w.WriteString("return ")
	}
	w.WriteString(enumName)
	w.WriteString("_")
	w.WriteString(method)
	w.WriteString("(__v")
	for _, p := range m.Parameters {
		w.WriteString(", ")
		if p.Variadic {
			w.WriteString(emit.MangleIdent(p.Name.Value) + "...")
		} else {
			w.WriteString(emit.MangleIdent(p.Name.Value))
		}
	}
	w.WriteLine(")")
	w.Dedent()
	w.WriteLine("}")
	w.Newline()
}

// emitTaggedEnumImpl lowers a tagged enum method's body once into a shared
// function with `this` bound to the enum interface value, so `match (this)`
// destructures the variant and a sibling call dispatches via the method set.
func (l *Lowerer) emitTaggedEnumImpl(enumGbName, enumGoName string, m *ast.FunctionStatement) {
	decls := l.Module.TopDecls()
	bodyWriter := emit.NewWriter()
	savedW := l.w
	l.w = bodyWriter
	methodRetGo := ""
	if m.ReturnType != nil {
		methodRetGo = types.ToGo(l.resolveTypeRef(m.ReturnType), l.Module.IntMode).Source
	}
	l.withReturnGo(methodRetGo, func() {
		l.withChildScope(func() {
			l.scope.Define(&types.Binding{
				Name:    "this",
				Type:    &types.Type{Kind: types.KindEnum, Name: enumGoName},
				Mutable: false,
			})
			for _, p := range m.Parameters {
				l.scope.Define(&types.Binding{
					Name:    p.Name.Value,
					Type:    paramBindingType(p, l.resolveTypeRef(p.Type)),
					Mutable: true,
					IsParam: true,
				})
			}
			if m.Body != nil {
				l.lowerBlock(m.Body.Statements)
			}
		})
		if methodRetGo != "" && bodyEndsWithTry(m.Body) {
			l.w.WriteLine("return " + zeroValue(methodRetGo))
		}
	})
	l.w = savedW

	decls.WriteString("func ")
	decls.WriteString(enumGoName)
	decls.WriteString("_")
	decls.WriteString(emit.MangleIdent(m.Name.Value))
	decls.WriteString("(this ")
	decls.WriteString(enumGoName)
	for _, p := range m.Parameters {
		decls.WriteString(", ")
		decls.WriteString(emit.MangleIdent(p.Name.Value))
		decls.WriteString(" ")
		goTy := types.ToGo(l.resolveTypeRef(p.Type), l.Module.IntMode)
		l.Module.AddTypeImports(goTy)
		if p.Variadic {
			decls.WriteString("...")
		}
		decls.WriteString(goTy.Source)
	}
	decls.WriteString(") ")
	if m.ReturnType != nil {
		goRet := types.ToGo(l.resolveTypeRef(m.ReturnType), l.Module.IntMode)
		l.Module.AddTypeImports(goRet)
		decls.WriteString(goRet.Source)
		decls.WriteString(" ")
	}
	decls.WriteLine("{")
	decls.Indent()
	decls.WriteString(bodyWriter.String())
	decls.Dedent()
	decls.WriteLine("}")
	decls.Newline()
	_ = enumGbName
}

func (l *Lowerer) lowerInterface(s *ast.InterfaceStatement) {
	if len(s.Generics) > 0 {
		l.errAt(s.Token.Line, s.Token.Column, "generic interfaces are not supported in Phase 2", "")
		return
	}
	name := emit.MangleIdent(s.Name.Value)
	l.Module.RegisterInterface(s.Name.Value)

	decls := l.Module.TopDecls()
	decls.WriteString("type ")
	decls.WriteString(name)
	decls.WriteLine(" interface {")
	decls.Indent()
	for _, parent := range s.Parents {
		decls.WriteLine(emit.MangleIdent(parent.Name))
	}
	for _, m := range s.Methods {
		decls.WriteString(emit.MangleIdent(m.Name.Value))
		decls.WriteString("(")
		for i, p := range m.Parameters {
			if i > 0 {
				decls.WriteString(", ")
			}
			decls.WriteString(emit.MangleIdent(p.Name.Value))
			decls.WriteString(" ")
			goTy := types.ToGo(l.resolveTypeRef(p.Type), l.Module.IntMode)
			l.Module.AddTypeImports(goTy)
			decls.WriteString(goTy.Source)
		}
		decls.WriteString(")")
		if m.ReturnType != nil {
			decls.WriteString(" ")
			goRet := types.ToGo(l.resolveTypeRef(m.ReturnType), l.Module.IntMode)
			l.Module.AddTypeImports(goRet)
			decls.WriteString(goRet.Source)
		}
		decls.WriteLine("")
	}
	decls.Dedent()
	decls.WriteLine("}")
	decls.Newline()
}

func (l *Lowerer) lowerEnum(s *ast.EnumStatement) {
	tagged := false
	for _, v := range s.Variants {
		if len(v.FieldTypes) > 0 {
			tagged = true
			break
		}
	}
	if tagged {
		l.lowerTaggedEnum(s)
		return
	}

	name := emit.MangleIdent(s.Name.Value)
	variantNames := make([]string, 0, len(s.Variants))
	for _, v := range s.Variants {
		variantNames = append(variantNames, v.Name.Value)
	}
	l.Module.RegisterEnum(s.Name.Value, variantNames)

	decls := l.Module.TopDecls()
	decls.WriteString("type ")
	decls.WriteString(name)
	decls.WriteLine(" int")
	decls.Newline()

	decls.WriteLine("const (")
	decls.Indent()
	for i, v := range variantNames {
		decls.WriteString(name)
		decls.WriteString(emit.MangleIdent(v))
		if i == 0 {
			decls.WriteString(" ")
			decls.WriteString(name)
			decls.WriteString(" = iota")
		}
		decls.WriteLine("")
	}
	decls.Dedent()
	decls.WriteLine(")")
	decls.Newline()

	decls.WriteString("func (__v ")
	decls.WriteString(name)
	decls.WriteLine(") String() string {")
	decls.Indent()
	decls.WriteLine("switch __v {")
	for _, v := range variantNames {
		decls.WriteString("case ")
		decls.WriteString(name)
		decls.WriteString(emit.MangleIdent(v))
		decls.WriteString(`: return "`)
		decls.WriteString(s.Name.Value)
		decls.WriteString(".")
		decls.WriteString(v)
		decls.WriteLine(`"`)
	}
	decls.WriteLine("}")
	decls.WriteString(`return "`)
	decls.WriteString(s.Name.Value)
	decls.WriteLine(`.?"`)
	decls.Dedent()
	decls.WriteLine("}")
	decls.Newline()

	l.emitEnumMethods(s, name)
	l.emitUntaggedEnumStatics(name, variantNames)
}

// emitUntaggedEnumStatics emits the per-enum values()/fromName() helpers. Values
// lists every variant; fromName returns *Enum (nil for an unknown name) so the
// nullable value-type contract holds. Match on the exact variant name.
func (l *Lowerer) emitUntaggedEnumStatics(name string, variantNames []string) {
	decls := l.Module.TopDecls()
	decls.WriteString("func " + name + "Values() []" + name + " { return []" + name + "{")
	for i, v := range variantNames {
		if i > 0 {
			decls.WriteString(", ")
		}
		decls.WriteString(name + emit.MangleIdent(v))
	}
	decls.WriteLine("} }")
	decls.Newline()

	decls.WriteLine("func " + name + "FromName(__s string) *" + name + " {")
	decls.Indent()
	decls.WriteLine("switch __s {")
	for _, v := range variantNames {
		decls.WriteLine("case " + strconv.Quote(v) + ": __v := " + name + emit.MangleIdent(v) + "; return &__v")
	}
	decls.WriteLine("}")
	decls.WriteLine("return nil")
	decls.Dedent()
	decls.WriteLine("}")
	decls.Newline()
}

// emitEnumMethods lowers an untagged enum's instance methods (plus any interface
// default it does not override) as Go methods on the enum's int type, so a value
// receiver binds `this` and interface conformance is structural.
func (l *Lowerer) emitEnumMethods(s *ast.EnumStatement, name string) {
	defined := map[string]struct{}{}
	for _, m := range s.Methods {
		if m.Name == nil {
			continue
		}
		if !l.checkEnumMethodEmittable(s, m.Name.Value) {
			return
		}
		defined[strings.ToLower(m.Name.Value)] = struct{}{}
		l.registerEnumMethodSignature(name, m)
		l.emitEnumMethod(s.Name.Value, name, m)
	}
	for _, m := range l.enumInterfaceDefaults(s, defined) {
		if !l.checkEnumMethodEmittable(s, m.Name.Value) {
			return
		}
		defined[strings.ToLower(m.Name.Value)] = struct{}{}
		l.registerEnumMethodSignature(name, m)
		l.emitEnumMethod(s.Name.Value, name, m)
	}
}

// enumInterfaceDefaults collects default methods from implemented interfaces
// that the enum leaves unimplemented, mirroring the interpreter's fold.
func (l *Lowerer) enumInterfaceDefaults(s *ast.EnumStatement, defined map[string]struct{}) []*ast.FunctionStatement {
	var out []*ast.FunctionStatement
	for _, ref := range s.Implements {
		iface, ok := l.Module.InterfaceDecl(ref.Name)
		if !ok {
			continue
		}
		for _, def := range iface.Defaults {
			if def.Name == nil {
				continue
			}
			key := strings.ToLower(def.Name.Value)
			if _, done := defined[key]; done {
				continue
			}
			defined[key] = struct{}{}
			out = append(out, def)
		}
	}
	return out
}

// checkEnumMethodEmittable rejects names the interpreter forbids (variant data
// accessors) and the generated String() the int type already carries.
func (l *Lowerer) checkEnumMethodEmittable(s *ast.EnumStatement, methodName string) bool {
	switch strings.ToLower(methodName) {
	case "variant", "fields":
		l.errAt(s.Token.Line, s.Token.Column,
			fmt.Sprintf("enum %s method %q collides with a built-in variant accessor", s.Name.Value, methodName),
			"")
		return false
	}
	if emit.MangleIdent(methodName) == "String" {
		l.errAt(s.Token.Line, s.Token.Column,
			fmt.Sprintf("the transpiler does not yet support an enum method named %q", methodName),
			"it collides with the generated String() method; build with 'geblang build' for the VM binary")
		return false
	}
	return true
}

func (l *Lowerer) registerEnumMethodSignature(enumGoName string, m *ast.FunctionStatement) {
	key := methodCalleeKey(enumGoName, m.Name.Value)
	l.Module.RegisterCalleeParams(key, paramNames(m.Parameters))
	l.Module.RegisterCalleeSignature(key, m.Parameters)
}

func (l *Lowerer) emitEnumMethod(enumGbName, enumGoName string, m *ast.FunctionStatement) {
	decls := l.Module.TopDecls()
	bodyWriter := emit.NewWriter()
	savedW := l.w
	l.w = bodyWriter
	methodRetGo := ""
	if m.ReturnType != nil {
		methodRetGo = types.ToGo(l.resolveTypeRef(m.ReturnType), l.Module.IntMode).Source
	}
	l.withReturnGo(methodRetGo, func() {
		l.withChildScope(func() {
			l.scope.Define(&types.Binding{
				Name:    "this",
				Type:    &types.Type{Kind: types.KindEnum, Name: enumGoName},
				Mutable: false,
			})
			for _, p := range m.Parameters {
				l.scope.Define(&types.Binding{
					Name:    p.Name.Value,
					Type:    paramBindingType(p, l.resolveTypeRef(p.Type)),
					Mutable: true,
					IsParam: true,
				})
			}
			if m.Body != nil {
				l.lowerBlock(m.Body.Statements)
			}
		})
		if methodRetGo != "" && bodyEndsWithTry(m.Body) {
			l.w.WriteLine("return " + zeroValue(methodRetGo))
		}
	})
	l.w = savedW

	decls.WriteString("func (this ")
	decls.WriteString(enumGoName)
	decls.WriteString(") ")
	decls.WriteString(emit.MangleIdent(m.Name.Value))
	decls.WriteString("(")
	l.emitParamList(decls, m.Parameters, l.resolveParamTypes(m.Parameters))
	decls.WriteString(") ")
	if m.ReturnType != nil {
		goRet := types.ToGo(l.resolveTypeRef(m.ReturnType), l.Module.IntMode)
		l.Module.AddTypeImports(goRet)
		decls.WriteString(goRet.Source)
		decls.WriteString(" ")
	}
	decls.WriteLine("{")
	decls.Indent()
	decls.WriteString(bodyWriter.String())
	decls.Dedent()
	decls.WriteLine("}")
	decls.Newline()
	_ = enumGbName
}

func (l *Lowerer) lowerClass(s *ast.ClassStatement) {
	if s.Destructor != nil {
		l.errAt(s.Token.Line, s.Token.Column, "destructors are not supported yet",
			"defer to a later phase")
		return
	}

	className := emit.MangleIdent(s.Name.Value)
	l.Module.RegisterClass(s.Name.Value)
	if s.Extends != nil {
		l.Module.RegisterClassParent(s.Name.Value, s.Extends.Name)
	}

	if l.Module.IsErrorClass(s.Name.Value) {
		l.emitErrorClass(s)
		return
	}

	if l.Module.InClassHierarchy(s.Name.Value) {
		if len(s.Generics) > 0 {
			l.errAt(s.Token.Line, s.Token.Column,
				"virtual dispatch for generic classes in an inheritance hierarchy is not yet supported",
				"defer to a later phase")
			return
		}
		l.emitHierarchyClass(s)
		return
	}

	parentName := ""
	if s.Extends != nil {
		parentName = emit.MangleIdent(s.Extends.Name)
		l.Module.RegisterClass(s.Extends.Name)
	}

	var fields []*ast.DeclarationStatement
	var methods []*ast.FunctionStatement
	var staticMethods []*ast.FunctionStatement
	var constructor *ast.FunctionStatement
	for _, m := range s.Members {
		switch n := m.(type) {
		case *ast.DeclarationStatement:
			fields = append(fields, n)
		case *ast.FunctionStatement:
			if n.Name.Value == s.Name.Value {
				constructor = n
				continue
			}
			if n.Static {
				staticMethods = append(staticMethods, n)
				continue
			}
			methods = append(methods, n)
		}
	}

	l.withTypeParams(s.Generics, func() {
		decls := l.Module.TopDecls()
		decls.WriteString("type ")
		decls.WriteString(className)
		l.emitGenericParams(decls, s.Generics)
		decls.WriteLine(" struct {")
		decls.Indent()
		if parentName != "" {
			decls.WriteString("*")
			decls.WriteLine(parentName)
		}
		for _, f := range fields {
			goTy := types.ToGo(l.resolveTypeRef(f.Type), l.Module.IntMode)
			l.Module.AddTypeImports(goTy)
			decls.WriteString(emit.MangleIdent(f.Name.Value))
			decls.WriteString(" ")
			decls.WriteString(goTy.Source)
			decls.WriteLine("")
		}
		decls.Dedent()
		decls.WriteLine("}")
		decls.Newline()

		if constructor != nil {
			l.Module.RegisterCalleeParams("New"+className, paramNames(constructor.Parameters))
			l.Module.RegisterCalleeSignature("New"+className, constructor.Parameters)
		}
		for _, m := range staticMethods {
			key := className + "_" + emit.MangleIdent(m.Name.Value)
			l.Module.RegisterCalleeParams(key, paramNames(m.Parameters))
			l.Module.RegisterCalleeSignature(key, m.Parameters)
		}
		l.emitConstructor(className, s.Name.Value, parentName, s.Generics, constructor, fields)
		for _, f := range fields {
			l.Module.RegisterClassField(s.Name.Value, f.Name.Value, f.Name.Value)
		}
		for _, m := range methods {
			l.Module.RegisterClassMethod(s.Name.Value, m.Name.Value)
			l.registerMethodSignature(className, m)
			l.emitMethod(className, s.Name.Value, parentName, s.Generics, m)
		}
		for _, m := range staticMethods {
			l.emitStaticMethod(className, m)
		}
		l.emitGbStringWrapper(className, "this", methods)
		l.emitClassDecoratorsTable(className, s.Decorators)
		l.emitClassMethodsTable(className, methods)
		l.emitClassFieldsTable(className, fields)
		l.emitMethodDecoratorsTable(className, methods)
		l.emitFieldDecoratorsTable(className, s.FieldDecorators)
	})
}

func (l *Lowerer) emitClassDecoratorsTable(className string, decorators []ast.Decorator) {
	decls := l.Module.TopDecls()
	decls.WriteString("var __decorators_")
	decls.WriteString(className)
	decls.WriteString(" = ")
	l.emitDecoratorList(decls, decorators)
	decls.WriteLine("")
	decls.Newline()
}

func (l *Lowerer) emitFunctionDecoratorsTable(funcName string, decorators []ast.Decorator) {
	if len(decorators) == 0 {
		return
	}
	decls := l.Module.TopDecls()
	decls.WriteString("var __decorators_")
	decls.WriteString(funcName)
	decls.WriteString(" = ")
	l.emitDecoratorList(decls, decorators)
	decls.WriteLine("")
	decls.Newline()
	l.Module.RegisterDecoratedFunction(funcName)
}

func (l *Lowerer) emitMethodDecoratorsTable(className string, methods []*ast.FunctionStatement) {
	hasAny := false
	for _, m := range methods {
		if len(m.Decorators) > 0 {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return
	}
	decls := l.Module.TopDecls()
	decls.WriteString("var __methoddecorators_")
	decls.WriteString(className)
	decls.WriteString(" = map[string][]*transpilert.OrderedDict[string, any]{")
	first := true
	for _, m := range methods {
		if len(m.Decorators) == 0 {
			continue
		}
		if !first {
			decls.WriteString(", ")
		}
		first = false
		decls.WriteString(strconv.Quote(m.Name.Value))
		decls.WriteString(": ")
		l.emitDecoratorList(decls, m.Decorators)
	}
	decls.WriteLine("}")
	decls.Newline()
}

func (l *Lowerer) emitFieldDecoratorsTable(className string, fieldDecorators map[string][]ast.Decorator) {
	if len(fieldDecorators) == 0 {
		return
	}
	keys := make([]string, 0, len(fieldDecorators))
	for k, v := range fieldDecorators {
		if len(v) == 0 {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return
	}
	sort.Strings(keys)
	decls := l.Module.TopDecls()
	decls.WriteString("var __fielddecorators_")
	decls.WriteString(className)
	decls.WriteString(" = map[string][]*transpilert.OrderedDict[string, any]{")
	for i, k := range keys {
		if i > 0 {
			decls.WriteString(", ")
		}
		decls.WriteString(strconv.Quote(k))
		decls.WriteString(": ")
		l.emitDecoratorList(decls, fieldDecorators[k])
	}
	decls.WriteLine("}")
	decls.Newline()
}

// decoratorEntryType is the Go type of one decorator's metadata; an ordered
// dict so user reflection code indexes it with the same Get/Keys API as a
// Geblang dict.
const decoratorEntryType = "*transpilert.OrderedDict[string, any]"

func (l *Lowerer) emitDecoratorList(w *emit.Writer, decorators []ast.Decorator) {
	l.Module.AddImport(types.OrderedDictImport)
	w.WriteString("[]")
	w.WriteString(decoratorEntryType)
	w.WriteString("{")
	for i, d := range decorators {
		if i > 0 {
			w.WriteString(", ")
		}
		w.WriteString("func() ")
		w.WriteString(decoratorEntryType)
		w.WriteString(" { __d := transpilert.NewOrderedDict[string, any](); __d.Set(\"name\", ")
		w.WriteString(strconv.Quote(d.Name.Value))
		w.WriteString(`); __d.Set("args", []any{`)
		for j, a := range d.Arguments {
			if j > 0 {
				w.WriteString(", ")
			}
			l.lowerExpressionInto(w, a.Value)
		}
		w.WriteString("}); return __d }()")
	}
	w.WriteString("}")
}

// emitGbStringWrapper emits an exported GbString() forwarding to a declared
// __string dunder so transpilert.Show prints the instance via the dunder
// (reflection cannot reach the unexported __string method). recvCall is the
// receiver expression that virtual-dispatches __string (this.__string for a
// flat class, this.self.__string for a hierarchy class).
func (l *Lowerer) emitGbStringWrapper(recvType, recvCall string, methods []*ast.FunctionStatement) {
	if !declaresMethod(methods, "__string") {
		return
	}
	decls := l.Module.TopDecls()
	decls.WriteLine("func (this *" + recvType + ") GbString() string {")
	decls.Indent()
	decls.WriteLine("return " + recvCall + ".__string()")
	decls.Dedent()
	decls.WriteLine("}")
	decls.Newline()
}

func declaresMethod(methods []*ast.FunctionStatement, name string) bool {
	for _, m := range methods {
		if m.Name.Value == name {
			return true
		}
	}
	return false
}

func (l *Lowerer) emitClassMethodsTable(className string, methods []*ast.FunctionStatement) {
	decls := l.Module.TopDecls()
	decls.WriteString("var __methods_")
	decls.WriteString(className)
	decls.WriteString(" = []string{")
	for i, m := range methods {
		if i > 0 {
			decls.WriteString(", ")
		}
		decls.WriteString(strconv.Quote(m.Name.Value))
	}
	decls.WriteLine("}")
	decls.Newline()
}

func (l *Lowerer) emitClassFieldsTable(className string, fields []*ast.DeclarationStatement) {
	decls := l.Module.TopDecls()
	decls.WriteString("var __fields_")
	decls.WriteString(className)
	decls.WriteString(" = []string{")
	for i, f := range fields {
		if i > 0 {
			decls.WriteString(", ")
		}
		decls.WriteString(strconv.Quote(f.Name.Value))
	}
	decls.WriteLine("}")
	decls.Newline()
}

func (l *Lowerer) emitStaticMethod(className string, m *ast.FunctionStatement) {
	bodyWriter := emit.NewWriter()
	savedW := l.w
	l.w = bodyWriter
	staticRetGo := ""
	if m.ReturnType != nil {
		staticRetGo = types.ToGo(l.resolveTypeRef(m.ReturnType), l.Module.IntMode).Source
	}
	l.withReturnGo(staticRetGo, func() {
		l.withChildScope(func() {
			for _, p := range m.Parameters {
				l.scope.Define(&types.Binding{
					Name:    p.Name.Value,
					Type:    paramBindingType(p, l.resolveTypeRef(p.Type)),
					Mutable: true,
					IsParam: true,
				})
			}
			if m.Body != nil {
				l.lowerBlock(m.Body.Statements)
			}
		})
		if staticRetGo != "" && bodyEndsWithTry(m.Body) {
			l.w.WriteLine("return " + zeroValue(staticRetGo))
		}
	})
	l.w = savedW

	decls := l.Module.TopDecls()
	decls.WriteString("func ")
	decls.WriteString(className)
	decls.WriteString("_")
	decls.WriteString(emit.MangleIdent(m.Name.Value))
	decls.WriteString("(")
	l.emitParamList(decls, m.Parameters, l.resolveParamTypes(m.Parameters))
	decls.WriteString(") ")
	if m.ReturnType != nil {
		goRet := types.ToGo(l.resolveTypeRef(m.ReturnType), l.Module.IntMode)
		l.Module.AddTypeImports(goRet)
		decls.WriteString(goRet.Source)
		decls.WriteString(" ")
	}
	decls.WriteLine("{")
	decls.Indent()
	decls.WriteString(bodyWriter.String())
	decls.Dedent()
	decls.WriteLine("}")
	decls.Newline()
}

func (l *Lowerer) emitGenericParams(w *emit.Writer, generics []*ast.TypeParam) {
	if len(generics) == 0 {
		return
	}
	w.WriteString("[")
	for i, g := range generics {
		if i > 0 {
			w.WriteString(", ")
		}
		w.WriteString(emit.MangleIdent(g.Name.Value))
		w.WriteString(" ")
		if g.Constraint != nil && l.Module.IsInterface(g.Constraint.Name) {
			w.WriteString(emit.MangleIdent(g.Constraint.Name))
		} else {
			w.WriteString("any")
		}
	}
	w.WriteString("]")
}

func (l *Lowerer) emitConstructor(className, classGbName, parentClass string, generics []*ast.TypeParam, ctor *ast.FunctionStatement, fields []*ast.DeclarationStatement) {
	decls := l.Module.TopDecls()
	bodyWriter := emit.NewWriter()
	savedW := l.w
	savedParent := l.parentClass
	savedInCtor := l.inConstructor
	l.w = bodyWriter
	l.parentClass = parentClass
	l.inConstructor = true
	l.withChildScope(func() {
		l.scope.Define(&types.Binding{
			Name:    "this",
			Type:    &types.Type{Kind: types.KindClass, Name: className},
			Mutable: true,
		})
		structRef := className
		if len(generics) > 0 {
			structRef += "["
			for i, g := range generics {
				if i > 0 {
					structRef += ", "
				}
				structRef += emit.MangleIdent(g.Name.Value)
			}
			structRef += "]"
		}
		bodyWriter.WriteLine("this := &" + structRef + "{}")
		for _, f := range fields {
			if f.Value == nil {
				continue
			}
			bodyWriter.WriteString("this.")
			bodyWriter.WriteString(emit.MangleIdent(f.Name.Value))
			bodyWriter.WriteString(" = ")
			l.withExpectedType(l.resolveTypeRef(f.Type), func() { l.lowerExpression(f.Value) })
			bodyWriter.WriteLine("")
		}
		if ctor != nil {
			for _, p := range ctor.Parameters {
				l.scope.Define(&types.Binding{
					Name:    p.Name.Value,
					Type:    paramBindingType(p, l.resolveTypeRef(p.Type)),
					Mutable: true,
					IsParam: true,
				})
			}
			if ctor.Body != nil {
				l.lowerBlock(ctor.Body.Statements)
			}
		}
		bodyWriter.WriteLine("return this")
	})
	l.w = savedW
	l.parentClass = savedParent
	l.inConstructor = savedInCtor

	decls.WriteString("func New" + className)
	l.emitGenericParams(decls, generics)
	decls.WriteString("(")
	if ctor != nil {
		l.emitParamList(decls, ctor.Parameters, l.resolveParamTypes(ctor.Parameters))
	}
	decls.WriteString(") *")
	decls.WriteString(className)
	if len(generics) > 0 {
		decls.WriteString("[")
		for i, g := range generics {
			if i > 0 {
				decls.WriteString(", ")
			}
			decls.WriteString(emit.MangleIdent(g.Name.Value))
		}
		decls.WriteString("]")
	}
	decls.WriteLine(" {")
	decls.Indent()
	decls.WriteString(bodyWriter.String())
	decls.Dedent()
	decls.WriteLine("}")
	decls.Newline()
	_ = classGbName
}

func (l *Lowerer) emitMethod(className, classGbName, parentClass string, generics []*ast.TypeParam, m *ast.FunctionStatement) {
	decls := l.Module.TopDecls()
	bodyWriter := emit.NewWriter()
	savedW := l.w
	savedParent := l.parentClass
	l.w = bodyWriter
	l.parentClass = parentClass
	methodRetGo := ""
	if m.ReturnType != nil {
		methodRetGo = types.ToGo(l.resolveTypeRef(m.ReturnType), l.Module.IntMode).Source
	}
	l.withReturnGo(methodRetGo, func() {
		l.withChildScope(func() {
			l.scope.Define(&types.Binding{
				Name:    "this",
				Type:    &types.Type{Kind: types.KindClass, Name: className},
				Mutable: true,
			})
			for _, p := range m.Parameters {
				l.scope.Define(&types.Binding{
					Name:    p.Name.Value,
					Type:    paramBindingType(p, l.resolveTypeRef(p.Type)),
					Mutable: true,
					IsParam: true,
				})
			}
			if m.Body != nil {
				l.lowerBlock(m.Body.Statements)
			}
		})
		if methodRetGo != "" && bodyEndsWithTry(m.Body) {
			l.w.WriteLine("return " + zeroValue(methodRetGo))
		}
	})
	l.w = savedW
	l.parentClass = savedParent

	decls.WriteString("func (this *")
	decls.WriteString(className)
	if len(generics) > 0 {
		decls.WriteString("[")
		for i, g := range generics {
			if i > 0 {
				decls.WriteString(", ")
			}
			decls.WriteString(emit.MangleIdent(g.Name.Value))
		}
		decls.WriteString("]")
	}
	decls.WriteString(") ")
	decls.WriteString(emit.MangleIdent(m.Name.Value))
	decls.WriteString("(")
	l.emitParamList(decls, m.Parameters, l.resolveParamTypes(m.Parameters))
	decls.WriteString(") ")
	if m.ReturnType != nil {
		goRet := types.ToGo(l.resolveTypeRef(m.ReturnType), l.Module.IntMode)
		l.Module.AddTypeImports(goRet)
		decls.WriteString(goRet.Source)
		decls.WriteString(" ")
	}
	decls.WriteLine("{")
	decls.Indent()
	decls.WriteString(bodyWriter.String())
	decls.Dedent()
	decls.WriteLine("}")
	decls.Newline()
	_ = classGbName
}

// emitParamList writes a Go parameter list; a trailing variadic param becomes
// `name ...ElemT` (Geblang types it as the element type, e.g. `int... xs`).
func (l *Lowerer) emitParamList(w *emit.Writer, params []ast.Parameter, paramTypes []*types.Type) {
	for i, p := range params {
		if i > 0 {
			w.WriteString(", ")
		}
		w.WriteString(emit.MangleIdent(p.Name.Value))
		w.WriteString(" ")
		goTy := types.ToGo(paramTypes[i], l.Module.IntMode)
		l.Module.AddTypeImports(goTy)
		if p.Variadic {
			w.WriteString("...")
		}
		w.WriteString(goTy.Source)
	}
}

// registerMethodSignature records an instance method's params/defaults/variadic
// under a method-specific key so call sites can fill defaults and spread.
func (l *Lowerer) registerMethodSignature(className string, m *ast.FunctionStatement) {
	key := methodCalleeKey(className, m.Name.Value)
	l.Module.RegisterCalleeParams(key, paramNames(m.Parameters))
	l.Module.RegisterCalleeSignature(key, m.Parameters)
}

func methodCalleeKey(className, method string) string {
	return emit.MangleIdent(className) + "#" + emit.MangleIdent(method)
}

func (l *Lowerer) resolveParamTypes(params []ast.Parameter) []*types.Type {
	out := make([]*types.Type, len(params))
	for i, p := range params {
		out[i] = l.resolveTypeRef(p.Type)
	}
	return out
}

// paramBindingType is the in-body binding type for a parameter; a variadic
// param is a list of its declared element type.
func paramBindingType(p ast.Parameter, declared *types.Type) *types.Type {
	if p.Variadic {
		return &types.Type{Kind: types.KindList, Elem: declared}
	}
	return declared
}

func (l *Lowerer) lowerTopLevelFunction(s *ast.FunctionStatement) {
	if s.Async && s.ReturnType == nil {
		l.errAt(s.Token.Line, s.Token.Column,
			"async function must declare an explicit return type",
			"add a return type like `: int` or `: string`")
		return
	}
	if s.Static {
		l.errAt(s.Token.Line, s.Token.Column,
			"static functions are only meaningful inside a class",
			"")
		return
	}

	bodyWriter := emit.NewWriter()
	savedW := l.w
	savedInGen := l.inGenerator
	l.w = bodyWriter
	var paramTypes []*types.Type
	var returnType *types.Type
	isGenerator := false
	l.withTypeParams(s.Generics, func() {
		paramTypes = make([]*types.Type, len(s.Parameters))
		for i, p := range s.Parameters {
			paramTypes[i] = l.resolveTypeRef(p.Type)
		}
		returnType = l.resolveTypeRef(s.ReturnType)
		if returnType != nil && returnType.Kind == types.KindGenerator {
			isGenerator = true
		}
		l.inGenerator = isGenerator
		retGoCtx := ""
		if !isGenerator && returnType != nil {
			if s.Async {
				retGoCtx = types.ToGo(returnType, l.Module.IntMode).Source
			} else if s.ReturnType != nil {
				retGoCtx = types.ToGo(returnType, l.Module.IntMode).Source
			}
		}
		l.withReturnGo(retGoCtx, func() {
			l.withReturnType(returnType, func() {
				l.withChildScope(func() {
					for i, p := range s.Parameters {
						l.scope.Define(&types.Binding{
							Name:    p.Name.Value,
							Type:    paramBindingType(p, paramTypes[i]),
							Mutable: true,
							IsParam: true,
						})
					}
					if s.Body != nil {
						l.lowerBlock(s.Body.Statements)
					}
				})
			})
			if retGoCtx != "" && bodyEndsWithTry(s.Body) {
				l.w.WriteLine("return " + zeroValue(retGoCtx))
			}
		})
	})
	l.w = savedW
	l.inGenerator = savedInGen

	decls := l.Module.TopDecls()
	decls.WriteString("func ")
	decls.WriteString(l.emittedFuncName(s.Name.Value))
	l.emitGenericParams(decls, s.Generics)
	decls.WriteString("(")
	l.emitParamList(decls, s.Parameters, paramTypes)
	decls.WriteString(") ")
	if s.Async {
		l.Module.RequireHelper("gbTask")
		retGo := types.ToGo(returnType, l.Module.IntMode)
		l.Module.AddTypeImports(retGo)
		decls.WriteString("*gbTask[")
		decls.WriteString(retGo.Source)
		decls.WriteString("] ")
	} else if s.ReturnType != nil {
		goRet := types.ToGo(returnType, l.Module.IntMode)
		l.Module.AddTypeImports(goRet)
		decls.WriteString(goRet.Source)
		decls.WriteString(" ")
	}
	decls.WriteLine("{")
	decls.Indent()
	if s.Async {
		retGo := types.ToGo(returnType, l.Module.IntMode)
		decls.WriteString("return gbRunTask(func() ")
		decls.WriteString(retGo.Source)
		decls.WriteLine(" {")
		decls.Indent()
		decls.WriteString(bodyWriter.String())
		decls.Dedent()
		decls.WriteLine("})")
	} else if isGenerator {
		elemGo := types.ToGo(returnType.Elem, l.Module.IntMode)
		l.Module.AddTypeImports(elemGo)
		decls.WriteString("return func(yield func(")
		decls.WriteString(elemGo.Source)
		decls.WriteLine(") bool) {")
		decls.Indent()
		decls.WriteString(bodyWriter.String())
		decls.Dedent()
		decls.WriteLine("}")
	} else {
		decls.WriteString(bodyWriter.String())
	}
	decls.Dedent()
	decls.WriteLine("}")
	decls.Newline()
	l.emitFunctionDecoratorsTable(emit.MangleIdent(s.Name.Value), s.Decorators)
}

func (l *Lowerer) lowerFunctionLiteral(e *ast.FunctionLiteral) {
	if e.Async {
		l.errAt(e.Token.Line, e.Token.Column, "async function literals not supported in Phase 2",
			"defer to Phase 4")
		l.w.WriteString("nil")
		return
	}

	bodyWriter := emit.NewWriter()
	savedW := l.w
	l.w = bodyWriter
	litRetGo := ""
	if e.ReturnType != nil {
		litRetGo = types.ToGo(l.resolveTypeRef(e.ReturnType), l.Module.IntMode).Source
	}
	l.withNestedFunc(func() {
		l.withReturnGo(litRetGo, func() {
			l.withChildScope(func() {
				for _, p := range e.Parameters {
					l.scope.Define(&types.Binding{
						Name:    p.Name.Value,
						Type:    paramBindingType(p, l.resolveTypeRef(p.Type)),
						Mutable: true,
						IsParam: true,
					})
				}
				if e.Body != nil {
					l.lowerBlock(e.Body.Statements)
				}
			})
			if litRetGo != "" && bodyEndsWithTry(e.Body) {
				l.w.WriteLine("return " + zeroValue(litRetGo))
			}
		})
	})
	l.w = savedW

	l.w.WriteString("func(")
	l.emitParamList(l.w, e.Parameters, l.resolveParamTypes(e.Parameters))
	l.w.WriteString(") ")
	if e.ReturnType != nil {
		goRet := types.ToGo(l.resolveTypeRef(e.ReturnType), l.Module.IntMode)
		l.Module.AddTypeImports(goRet)
		l.w.WriteString(goRet.Source)
		l.w.WriteString(" ")
	}
	l.w.WriteString("{")
	l.w.Newline()
	l.w.Indent()
	l.w.WriteString(bodyWriter.String())
	l.w.Dedent()
	l.w.WriteString("}")
}
