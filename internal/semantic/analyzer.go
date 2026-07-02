package semantic

import (
	"fmt"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/token"
)

// Severity is the importance of a semantic diagnostic. SeverityError
// is the default (zero value) so all existing call sites that haven't
// explicitly opted into a warning continue to be treated as errors.
type Severity int

const (
	SeverityError   Severity = 0
	SeverityWarning Severity = 1
)

// Diagnostic is a single semantic finding. Line and Column are 1-based,
// matching the parser's error positions. Zero values mean "position
// unknown" and downstream consumers (the LSP server) will surface
// them at (1, 1) as a fallback. Severity controls whether the
// finding blocks execution in `geblang run` (Error) or prints to
// stderr but allows the program to start (Warning).
type Diagnostic struct {
	Message  string
	Line     int
	Column   int
	Severity Severity
	// Rule categorizes the finding for tooling (e.g. "deprecated"). Empty
	// defaults to the generic "semantic" rule downstream.
	Rule string
}

type Analyzer struct {
	diagnostics []Diagnostic
	scopes      []map[string]typeInfo
	functions   map[string][]methodInfo
	classes     map[string]classInfo
	interfaces  map[string]interfaceInfo
	enums       map[string]enumInfo
	aliases     map[string]typeInfo
	// typeParamScope counts in-scope generic type-param names so the
	// unknown-type check accepts `func f<T>(T x)`.
	typeParamScope map[string]int
	// classSurface, when set, returns the full member set (methods +
	// fields, walked across parents/interfaces and modules) for a class
	// name, plus whether the class was cleanly resolvable. Injected by
	// the check command to enable cross-module unknown-method detection;
	// nil in the execution (test/run) path, which disables the check.
	classSurface func(className string) (map[string]bool, bool)
	// classMethodSigs, when set, returns a method's overload signatures (plus
	// the declaring class's generic parameter names) for static argument
	// validation. Injected by the check command alongside classSurface.
	classMethodSigs func(className, methodLower string) ([]MethodSignature, []string, bool)
	// classFieldType resolves a field's declared type across parents/modules; nil disables field-receiver typing.
	classFieldType func(className, fieldLower string) (*ast.TypeRef, bool)
	// methodChecks enables the unknown-method checks (primitive + class).
	// Off in the execution path; turned on by the check command.
	methodChecks bool
	// deprecated registries (from @deprecated): lowercased function name and
	// class name -> message, and class -> lowercased method -> message. Empty
	// message means no explanatory text was supplied.
	deprecatedFuncs   map[string]string
	deprecatedClasses map[string]string
	deprecatedMethods map[string]map[string]string
	// from-import local names, treated as known types.
	importedNames map[string]bool
	// import aliases (module names): legal only in qualified access, dir, reflect.module.
	moduleAliases map[string]bool
	// every name bound by any declaration in the file; backs the
	// unknown-bare-callee check without lexical-scope false positives.
	declaredNames map[string]struct{}
	// exprTypes, when non-nil, turns on best-effort expression-type
	// recording (the transpiler's type source). Nil in every normal
	// path, so analysis behaviour and cost are unchanged.
	exprTypes map[ast.Expression]ExprType
}

// SetClassSurfaceResolver installs the cross-module class member
// resolver used by the unknown-method check. Call before Analyze.
func (a *Analyzer) SetClassSurfaceResolver(fn func(string) (map[string]bool, bool)) {
	a.classSurface = fn
}

// SetClassMethodSignatureResolver installs the resolver used to validate
// method-call arguments. Call before Analyze.
func (a *Analyzer) SetClassMethodSignatureResolver(fn func(string, string) ([]MethodSignature, []string, bool)) {
	a.classMethodSigs = fn
}

// SetClassFieldTypeResolver installs the field-type resolver used to type
// field receivers (e.g. `obj.field`) for the member/method checks. Call before Analyze.
func (a *Analyzer) SetClassFieldTypeResolver(fn func(string, string) (*ast.TypeRef, bool)) {
	a.classFieldType = fn
}

// EnableMethodChecks turns on the unknown-method diagnostics (primitive
// method typos and, when a class resolver is set, class methods). Call
// before Analyze.
func (a *Analyzer) EnableMethodChecks() {
	a.methodChecks = true
}

type typeInfo struct {
	name     string
	nullable bool
	known    bool
	// declaredNullable tracks that the declared type was nullable, preserved through non-null narrowing.
	declaredNullable bool
	// args carries generic type arguments. For `list<int>`, args = [int].
	// For `dict<string, User>`, args = [string, User]. For non-generic types,
	// args is nil. Element- and call-argument validation walks these.
	args []typeInfo
	// destroyed is set on a binding after a `del x` statement
	// retires it. Identifier references that resolve to a
	// destroyed binding emit a 'use of destroyed binding' error.
	// Re-binding (let / const / typed declaration) of the same
	// name creates a fresh entry with destroyed=false.
	destroyed bool
}

type classInfo struct {
	name       string
	parent     string
	implements []string
	typeParams []string
	methods    map[string][]methodInfo
}

type interfaceInfo struct {
	name    string
	parents []string
	methods map[string][]methodInfo
}

type enumInfo struct {
	name        string
	backingType string
	implements  []string
	methods     map[string][]methodInfo
	variants    []string
}

type methodInfo struct {
	name       string
	parameters []typeInfo
	// minArgs is the smallest positional arg count a caller may pass.
	// Defaults to len(parameters); lowered when trailing parameters
	// declare default values.
	minArgs    int
	returnType typeInfo
	typeParams []typeParam
}

// typeParam captures the name and optional constraint of a generic type
// parameter declared on a function/method/class signature. The name is the
// literal identifier (`T`); the constraint, if present, is the upper-bound
// type the inferred argument must be assignable to.
type typeParam struct {
	name       string
	constraint typeInfo
}

// builtinModuleNames is the set of names that resolve to a built-in
// stdlib module. Declaring a local variable with any of these names
// is legal (lexical scope: the local wins at the resolution site),
// but the analyzer still surfaces a warning so a reader skimming
// the code isn't misled by `json.parse(x)` on a shadowed local.
//
// Common parameter names like `args`, `path`, and `name` are
// intentionally NOT in the list - they collide too often with
// idiomatic Geblang variable naming to be useful warnings, and
// the runtime resolution rule eliminates the actual bug surface.
var builtinModuleNames = map[string]bool{
	"io": true, "sys": true, "process": true, "async": true,
	"json": true, "xml": true, "toml": true, "yaml": true, "csv": true,
	"collections": true, "secrets": true, "random": true,
	"strbuilder": true,
	"schema":     true, "serde": true, "metrics": true,
	"trace": true, "profile": true, "crypt": true,
	"encoding": true, "compress": true, "template": true,
	"re": true, "pcre": true, "markdown": true, "datetime": true, "uuid": true,
	"dotenv": true, "cli": true, "http": true,
	"websocket": true, "smtp": true, "db": true,
	"reflect": true, "log": true,
	"watch": true, "errors": true, "freeze": true,
}

// checkBuiltinModuleShadow emits a warning when `name` would shadow
// a built-in stdlib module. We skip primitive-typed declarations
// (`string path`, `int errors` etc.) where the user is plainly not
// using the name as a module reference - the warning is reserved
// for collection / class / any-typed shadows where a `.method()`
// dispatch on the shadowed name would surface as the confusing
// "X is not a module" runtime error.
func (a *Analyzer) checkBuiltinModuleShadow(name *ast.Identifier, declaredType *ast.TypeRef) {
	if name == nil || name.Value == "" {
		return
	}
	if !builtinModuleNames[name.Value] {
		return
	}
	if declaredType != nil {
		switch strings.ToLower(declaredType.Name) {
		case "string", "int", "float", "decimal", "bool", "bytes":
			return
		}
	}
	a.warningAt(name.Token,
		"%q shadows the built-in stdlib module %q; identifier resolution may pick the module unexpectedly - rename the local",
		name.Value, name.Value)
}

func New() *Analyzer {
	return &Analyzer{scopes: []map[string]typeInfo{{}}, functions: map[string][]methodInfo{}, classes: map[string]classInfo{}, interfaces: map[string]interfaceInfo{}, enums: map[string]enumInfo{}, aliases: map[string]typeInfo{}, typeParamScope: map[string]int{}, deprecatedFuncs: map[string]string{}, deprecatedClasses: map[string]string{}, deprecatedMethods: map[string]map[string]string{}, importedNames: map[string]bool{}, moduleAliases: map[string]bool{}, declaredNames: map[string]struct{}{}}
}

func (a *Analyzer) Analyze(program *ast.Program) []Diagnostic {
	a.collectTypeDeclarations(program.Statements)
	collectDeclaredNames(program.Statements, a.declaredNames)
	a.validateTypeAnnotations(program.Statements)
	a.validateTopLevelOverloads()
	a.validateClassOverloads()
	a.validateInterfaceOverloads()
	a.validateInterfaceImplementations()
	a.validateCastDunderReturns(program.Statements)
	a.collectDeprecations(program.Statements)
	a.validateOverrides(program.Statements)
	isModule := fileIsModule(program.Statements)
	seenInit := false
	for _, stmt := range program.Statements {
		a.analyzeStatement(stmt, nil)
		switch s := stmt.(type) {
		case *ast.InitStatement:
			if seenInit {
				a.errorAt(s.Token, "only one init block is allowed per file")
			}
			seenInit = true
		}
		if isModule && !isAllowedAtModuleTopLevel(stmt) {
			a.errorAt(statementToken(stmt),
				"free-standing top-level %s is not allowed in a module file; wrap imperative setup in an init { ... } block",
				moduleTopLevelKind(stmt))
		}
	}
	return a.diagnostics
}

// fileIsModule reports whether the program is a module file - i.e. its
// first non-comment top-level statement is `module name;`. Script
// files (no module declaration) keep their full top-level freedom.
func fileIsModule(stmts []ast.Statement) bool {
	for _, stmt := range stmts {
		if _, ok := stmt.(*ast.ModuleStatement); ok {
			return true
		}
		// First non-module statement decides; even if a later one is a
		// ModuleStatement (which would be a parse error in practice),
		// we treat the file as a script.
		return false
	}
	return false
}

// isAllowedAtModuleTopLevel returns true for the declarative
// statements that are permitted at the top level of a module file
// (declarations, type aliases, imports/exports, classes, interfaces,
// enums, functions, and the at-most-one init block). Anything else
// must live inside `init { ... }`.
func isAllowedAtModuleTopLevel(stmt ast.Statement) bool {
	switch s := stmt.(type) {
	case *ast.ModuleStatement,
		*ast.ImportStatement,
		*ast.FromImportStatement,
		*ast.InitStatement,
		*ast.TypeAliasStatement,
		*ast.DeclarationStatement,
		*ast.FunctionStatement,
		*ast.ClassStatement,
		*ast.InterfaceStatement,
		*ast.EnumStatement:
		return true
	case *ast.ExportStatement:
		// export wraps another statement - the wrapped form is what
		// needs to be a declarative kind.
		return isAllowedAtModuleTopLevel(s.Statement)
	case *ast.DestructuringStatement:
		// `let [a, b] = ...` is a declaration; bare `[a, b] = ...` is
		// not.
		return s.Define
	}
	return false
}

// statementToken returns the leading token of any statement, used to
// anchor diagnostics. Falls back to a zero token (position-less) when
// the type isn't recognised.
func statementToken(stmt ast.Statement) token.Token {
	switch s := stmt.(type) {
	case *ast.ModuleStatement:
		return s.Token
	case *ast.ImportStatement:
		return s.Token
	case *ast.FromImportStatement:
		return s.Token
	case *ast.ExportStatement:
		return s.Token
	case *ast.InitStatement:
		return s.Token
	case *ast.TypeAliasStatement:
		return s.Token
	case *ast.DeclarationStatement:
		return s.Token
	case *ast.DestructuringStatement:
		return s.Token
	case *ast.FunctionStatement:
		return s.Token
	case *ast.ClassStatement:
		return s.Token
	case *ast.InterfaceStatement:
		return s.Token
	case *ast.EnumStatement:
		return s.Token
	case *ast.ExpressionStatement:
		return tokenOfExpression(s.Expression)
	case *ast.ReturnStatement:
		return s.Token
	case *ast.YieldStatement:
		return s.Token
	case *ast.SimpleStatement:
		return s.Token
	case *ast.IfStatement:
		return s.Token
	case *ast.WhileStatement:
		return s.Token
	case *ast.ForStatement:
		return s.Token
	case *ast.TryStatement:
		return s.Token
	case *ast.MatchStatement:
		return s.Token
	case *ast.BlockStatement:
		return s.Token
	}
	return token.Token{}
}

// moduleTopLevelKind names the offending statement type for the
// diagnostic message.
func moduleTopLevelKind(stmt ast.Statement) string {
	switch stmt.(type) {
	case *ast.ExpressionStatement:
		return "expression"
	case *ast.ReturnStatement:
		return "return"
	case *ast.YieldStatement:
		return "yield"
	case *ast.SimpleStatement:
		return "statement"
	case *ast.IfStatement:
		return "if"
	case *ast.WhileStatement:
		return "while"
	case *ast.ForStatement:
		return "for"
	case *ast.TryStatement:
		return "try"
	case *ast.MatchStatement:
		return "match"
	case *ast.BlockStatement:
		return "block"
	case *ast.DestructuringStatement:
		return "destructuring assignment"
	}
	return "statement"
}

func (a *Analyzer) collectTypeDeclarations(stmts []ast.Statement) {
	for _, stmt := range stmts {
		switch stmt := stmt.(type) {
		case *ast.ExportStatement:
			a.collectTypeDeclarations([]ast.Statement{stmt.Statement})
		case *ast.TypeAliasStatement:
			a.aliases[strings.ToLower(stmt.Name.Value)] = a.typeInfoFromRef(stmt.Type)
		case *ast.FunctionStatement:
			info := a.methodInfoFromFunction(stmt)
			key := strings.ToLower(info.name)
			a.functions[key] = append(a.functions[key], info)
		case *ast.ClassStatement:
			info := classInfo{name: stmt.Name.Value, methods: map[string][]methodInfo{}}
			for _, generic := range stmt.Generics {
				info.typeParams = append(info.typeParams, generic.Name.Value)
			}
			if stmt.Extends != nil {
				info.parent = stmt.Extends.Name
			}
			for _, iface := range stmt.Implements {
				info.implements = append(info.implements, iface.Name)
			}
			for _, member := range stmt.Members {
				if fn, ok := member.(*ast.FunctionStatement); ok && !fn.Static {
					method := a.methodInfoFromFunction(fn)
					key := strings.ToLower(method.name)
					info.methods[key] = append(info.methods[key], method)
				}
			}
			a.classes[info.name] = info
		case *ast.InterfaceStatement:
			info := interfaceInfo{name: stmt.Name.Value, methods: map[string][]methodInfo{}}
			for _, parent := range stmt.Parents {
				info.parents = append(info.parents, parent.Name)
			}
			for _, method := range stmt.Methods {
				methodInfo := a.methodInfoFromSignature(method)
				key := strings.ToLower(methodInfo.name)
				info.methods[key] = append(info.methods[key], methodInfo)
			}
			a.interfaces[info.name] = info
		case *ast.EnumStatement:
			info := enumInfo{name: stmt.Name.Value, methods: map[string][]methodInfo{}}
			if stmt.BackingType != nil {
				info.backingType = stmt.BackingType.Name
			}
			for _, v := range stmt.Variants {
				if v.Name != nil {
					info.variants = append(info.variants, v.Name.Value)
				}
			}
			for _, iface := range stmt.Implements {
				info.implements = append(info.implements, iface.Name)
			}
			for _, method := range stmt.Methods {
				m := a.methodInfoFromFunction(method)
				key := strings.ToLower(m.name)
				info.methods[key] = append(info.methods[key], m)
			}
			a.enums[stmt.Name.Value] = info
		case *ast.ImportStatement:
			a.moduleAliases[stmt.ModuleName()] = true
		case *ast.FromImportStatement:
			for _, item := range stmt.Names {
				if local := item.Local(); local != "" {
					a.importedNames[local] = true
				}
			}
		}
	}
}

// validateTypeAnnotations walks every statement and expression and
// flags any bare type name in an annotation position that resolves to
// no known type. Generic type-param names are scoped per signature.
func (a *Analyzer) validateTypeAnnotations(stmts []ast.Statement) {
	for _, stmt := range stmts {
		a.validateStmtAnnotations(stmt)
	}
}

func (a *Analyzer) validateStmtAnnotations(stmt ast.Statement) {
	switch s := stmt.(type) {
	case *ast.ExportStatement:
		a.validateStmtAnnotations(s.Statement)
	case *ast.TypeAliasStatement:
		a.checkTypeRefExists(s.Type, "type alias "+s.Name.Value)
	case *ast.DeclarationStatement:
		a.checkTypeRefExists(s.Type, "declaration of "+s.Name.Value)
		a.validateExprAnnotations(s.Value)
	case *ast.FunctionStatement:
		pop := a.pushTypeParams(s.Generics)
		a.validateGenericConstraints(s.Generics)
		a.validateSignatureAnnotations(s.Parameters, s.ReturnType, "function "+nameOf(s.Name))
		a.validateBlockAnnotations(s.Body)
		pop()
	case *ast.ClassStatement:
		pop := a.pushTypeParams(s.Generics)
		a.validateGenericConstraints(s.Generics)
		a.checkTypeRefExists(s.Extends, "extends clause of "+nameOf(s.Name))
		for _, iface := range s.Implements {
			a.checkTypeRefExists(iface, "implements clause of "+nameOf(s.Name))
		}
		for _, member := range s.Members {
			a.validateStmtAnnotations(member)
		}
		if s.Destructor != nil {
			a.validateBlockAnnotations(s.Destructor.Body)
		}
		pop()
	case *ast.InterfaceStatement:
		pop := a.pushTypeParams(s.Generics)
		a.validateGenericConstraints(s.Generics)
		for _, parent := range s.Parents {
			a.checkTypeRefExists(parent, "extends clause of "+nameOf(s.Name))
		}
		for _, sig := range s.Methods {
			sp := a.pushTypeParams(sig.Generics)
			a.validateGenericConstraints(sig.Generics)
			a.validateSignatureAnnotations(sig.Parameters, sig.ReturnType, "interface method "+nameOf(sig.Name))
			sp()
		}
		for _, def := range s.Defaults {
			a.validateStmtAnnotations(def)
		}
		for _, field := range s.Fields {
			a.validateStmtAnnotations(field)
		}
		pop()
	case *ast.EnumStatement:
		a.checkTypeRefExists(s.BackingType, "backing type of "+nameOf(s.Name))
		for _, variant := range s.Variants {
			a.validateExprAnnotations(variant.BackingValue)
		}
		for _, iface := range s.Implements {
			a.checkTypeRefExists(iface, "implements clause of "+nameOf(s.Name))
		}
		for _, method := range s.Methods {
			a.validateStmtAnnotations(method)
		}
	case *ast.InitStatement:
		a.validateBlockAnnotations(s.Body)
	case *ast.ExpressionStatement:
		a.validateExprAnnotations(s.Expression)
	case *ast.ReturnStatement:
		a.validateExprAnnotations(s.Value)
	case *ast.YieldStatement:
		a.validateExprAnnotations(s.Value)
	case *ast.SimpleStatement:
		a.validateExprAnnotations(s.Value)
	case *ast.IfStatement:
		a.validateExprAnnotations(s.Condition)
		a.validateBlockAnnotations(s.Consequence)
		for _, ei := range s.ElseIfs {
			a.validateExprAnnotations(ei.Condition)
			a.validateBlockAnnotations(ei.Body)
		}
		a.validateBlockAnnotations(s.Alternative)
	case *ast.WhileStatement:
		a.validateExprAnnotations(s.Condition)
		a.validateBlockAnnotations(s.Body)
	case *ast.ForStatement:
		a.validateStmtAnnotations(s.Init)
		a.validateExprAnnotations(s.Condition)
		a.validateStmtAnnotations(s.Update)
		a.validateBlockAnnotations(s.Body)
	case *ast.WithStatement:
		a.validateExprAnnotations(s.Value)
		a.validateBlockAnnotations(s.Body)
	case *ast.TryStatement:
		a.validateBlockAnnotations(s.Body)
		for _, catch := range s.Catches {
			a.checkTypeRefExists(catch.Type, "catch clause")
			a.validateBlockAnnotations(catch.Body)
		}
		a.validateBlockAnnotations(s.Finally)
	case *ast.MatchStatement:
		a.validateExprAnnotations(s.Expr)
		for _, c := range s.Cases {
			a.validateBlockAnnotations(c.Body)
		}
	case *ast.SelectStatement:
		for _, c := range s.Cases {
			a.validateBlockAnnotations(c.Body)
		}
		a.validateBlockAnnotations(s.Default)
	case *ast.DestructuringStatement:
		a.validateExprAnnotations(s.Value)
	}
}

func (a *Analyzer) validateSignatureAnnotations(params []ast.Parameter, returnType *ast.TypeRef, context string) {
	for _, p := range params {
		a.checkTypeRefExists(p.Type, "parameter "+nameOf(p.Name)+" of "+context)
		a.validateExprAnnotations(p.Default)
	}
	a.checkTypeRefExists(returnType, "return type of "+context)
}

func (a *Analyzer) validateBlockAnnotations(block *ast.BlockStatement) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		a.validateStmtAnnotations(stmt)
	}
}

func (a *Analyzer) validateExprAnnotations(expr ast.Expression) {
	switch e := expr.(type) {
	case nil:
		return
	case *ast.CastExpression:
		a.checkTypeRefExists(e.Type, "cast")
		a.validateExprAnnotations(e.Value)
	case *ast.PrefixExpression:
		a.validateExprAnnotations(e.Right)
	case *ast.PostfixExpression:
		a.validateExprAnnotations(e.Left)
	case *ast.InfixExpression:
		a.validateExprAnnotations(e.Left)
		a.validateExprAnnotations(e.Right)
		// instanceof targets match cross-module types by trailing name, so
		// bare-name validation needs export resolution (the check layer).
	case *ast.AssignmentExpression:
		a.validateExprAnnotations(e.Left)
		a.validateExprAnnotations(e.Value)
	case *ast.SelectorExpression:
		a.validateExprAnnotations(e.Object)
	case *ast.IndexExpression:
		a.validateExprAnnotations(e.Left)
		a.validateExprAnnotations(e.Index)
	case *ast.CallExpression:
		a.validateExprAnnotations(e.Callee)
		for _, arg := range e.Arguments {
			a.validateExprAnnotations(arg.Value)
		}
	case *ast.ListLiteral:
		for _, el := range e.Elements {
			a.validateExprAnnotations(el)
		}
	case *ast.SetLiteral:
		for _, el := range e.Elements {
			a.validateExprAnnotations(el)
		}
	case *ast.DictLiteral:
		for _, entry := range e.Entries {
			a.validateExprAnnotations(entry.Key)
			a.validateExprAnnotations(entry.Value)
		}
	case *ast.TernaryExpression:
		a.validateExprAnnotations(e.Condition)
		a.validateExprAnnotations(e.ThenExpr)
		a.validateExprAnnotations(e.ElseExpr)
	case *ast.RangeExpression:
		a.validateExprAnnotations(e.Start)
		a.validateExprAnnotations(e.End)
		a.validateExprAnnotations(e.Step)
	case *ast.PipeExpression:
		a.validateExprAnnotations(e.Left)
		a.validateExprAnnotations(e.Right)
	case *ast.PartialExpression:
		a.validateExprAnnotations(e.Callee)
		for _, arg := range e.Arguments {
			if !arg.Hole {
				a.validateExprAnnotations(arg.Value)
			}
		}
	case *ast.AwaitExpression:
		a.validateExprAnnotations(e.Value)
	case *ast.SpreadExpression:
		a.validateExprAnnotations(e.Value)
	case *ast.FunctionLiteral:
		a.validateSignatureAnnotations(e.Parameters, e.ReturnType, "function literal")
		a.validateBlockAnnotations(e.Body)
	}
}

func nameOf(id *ast.Identifier) string {
	if id == nil {
		return ""
	}
	return id.Value
}

// castDunderExpectedReturn returns the declared-return-type a cast
// dunder must use. Empty when the method name is not a cast dunder.
func castDunderExpectedReturn(name string) string {
	switch name {
	case "__string":
		return "string"
	case "__int":
		return "int"
	case "__float":
		return "float"
	case "__bool":
		return "bool"
	case "__decimal":
		return "decimal"
	case "__bytes":
		return "bytes"
	}
	return ""
}

// validateCastDunderReturns rejects classes that declare a cast
// dunder (__string, __int, ...) with the wrong return type.
func (a *Analyzer) validateCastDunderReturns(stmts []ast.Statement) {
	for _, stmt := range stmts {
		class, ok := stmt.(*ast.ClassStatement)
		if !ok {
			continue
		}
		for _, member := range class.Members {
			fn, ok := member.(*ast.FunctionStatement)
			if !ok || fn.Static {
				continue
			}
			expected := castDunderExpectedReturn(fn.Name.Value)
			if expected == "" {
				continue
			}
			actual := ""
			if fn.ReturnType != nil {
				actual = fn.ReturnType.Name
			}
			if !strings.EqualFold(actual, expected) {
				a.errorAt(fn.Token, "%s.%s must declare return type %s, got %q", class.Name.Value, fn.Name.Value, expected, actual)
			}
		}
	}
}

func (a *Analyzer) validateTopLevelOverloads() {
	for name, methods := range a.functions {
		a.validateOverloadSet("function "+name, methods)
	}
}

func (a *Analyzer) validateClassOverloads() {
	for _, class := range a.classes {
		for name, methods := range class.methods {
			a.validateOverloadSet("method "+class.name+"."+name, methods)
		}
	}
}

func (a *Analyzer) validateInterfaceOverloads() {
	for _, iface := range a.interfaces {
		for name, methods := range iface.methods {
			a.validateOverloadSet("interface method "+iface.name+"."+name, methods)
		}
	}
}

func (a *Analyzer) validateOverloadSet(label string, methods []methodInfo) {
	seen := map[string]bool{}
	for _, method := range methods {
		key := method.signatureKey()
		if seen[key] {
			a.errorf("duplicate overload for %s with signature %s", label, key)
			continue
		}
		seen[key] = true
	}
}

func (a *Analyzer) validateInterfaceImplementations() {
	for _, class := range a.classes {
		for _, ifaceName := range class.implements {
			required := a.interfaceMethods(ifaceName, map[string]bool{})
			for name, expectedMethods := range required {
				for _, expected := range expectedMethods {
					if !a.classHasCompatibleMethod(class.name, name, expected) {
						a.errorf("class %s implements %s but is missing compatible method %s%s", class.name, ifaceName, name, expected.signatureKey())
					}
				}
			}
		}
	}
	for _, enum := range a.enums {
		for _, ifaceName := range enum.implements {
			required := a.interfaceMethods(ifaceName, map[string]bool{})
			for name, expectedMethods := range required {
				for _, expected := range expectedMethods {
					if !a.enumHasCompatibleMethod(enum, name, expected) {
						a.errorf("enum %s implements %s but is missing compatible method %s%s", enum.name, ifaceName, name, expected.signatureKey())
					}
				}
			}
		}
	}
}

func (a *Analyzer) enumHasCompatibleMethod(enum enumInfo, name string, expected methodInfo) bool {
	for _, actual := range enum.methods[strings.ToLower(name)] {
		if a.methodCompatible(expected, actual) {
			return true
		}
	}
	return false
}

func (a *Analyzer) analyzeStatement(stmt ast.Statement, fn *ast.FunctionStatement) {
	switch stmt := stmt.(type) {
	case *ast.TypeAliasStatement:
		return
	case *ast.DeclarationStatement:
		a.analyzeDeclaration(stmt)
	case *ast.ExpressionStatement:
		a.analyzeExpression(stmt.Expression)
		a.validateCallStatementArgs(stmt.Expression)
	case *ast.ExportStatement:
		a.analyzeStatement(stmt.Statement, fn)
	case *ast.FunctionStatement:
		a.declare(stmt.Name.Value, typeInfo{name: "func", known: true})
		a.pushScope()
		for _, param := range stmt.Parameters {
			/* Parameter names are scoped to one function body and
			 * tend to use common labels like `args`, `path`, `io`
			 * intentionally. The shadowing diagnostic for those
			 * would create more noise than signal - reserved for
			 * top-level / class-level declarations where the
			 * confusion is more likely (`let errors = [...]`
			 * followed by `errors.push(...)`). */
			a.declare(param.Name.Value, a.parameterBindingType(param))
			if param.Default != nil {
				a.checkAssignable(param.Type, param.Default, fmt.Sprintf("cannot use %s default for %s parameter %s", a.expressionTypeName(param.Default).display(), param.Type.String(), param.Name.Value))
			}
		}
		a.analyzeBlock(stmt.Body, stmt)
		a.popScope()
	case *ast.ReturnStatement:
		a.analyzeReturn(stmt, fn)
	case *ast.YieldStatement:
		if stmt.Value != nil {
			a.analyzeExpression(stmt.Value)
		}
	case *ast.SimpleStatement:
		if stmt.Value != nil {
			a.analyzeExpression(stmt.Value)
		}
	case *ast.InitStatement:
		a.analyzeBlock(stmt.Body, fn)
	case *ast.IfStatement:
		consequenceNarrowing, alternativeNarrowing := a.narrowingsForCondition(stmt.Condition)
		a.analyzeBlockWithNarrowing(stmt.Consequence, fn, consequenceNarrowing)
		for _, elseif := range stmt.ElseIfs {
			elseifCons, _ := a.narrowingsForCondition(elseif.Condition)
			a.analyzeBlockWithNarrowing(elseif.Body, fn, elseifCons)
		}
		a.analyzeBlockWithNarrowing(stmt.Alternative, fn, alternativeNarrowing)
	case *ast.WhileStatement:
		consequenceNarrowing, _ := a.narrowingsForCondition(stmt.Condition)
		a.analyzeBlockWithNarrowing(stmt.Body, fn, consequenceNarrowing)
	case *ast.ForStatement:
		if a.exprTypes != nil {
			a.recordForBody(stmt, fn)
			break
		}
		if stmt.Init != nil {
			a.analyzeStatement(stmt.Init, fn)
		}
		a.checkForInBinder(stmt)
		if stmt.Update != nil {
			a.analyzeStatement(stmt.Update, fn)
		}
		a.analyzeBlock(stmt.Body, fn)
	case *ast.TryStatement:
		a.analyzeBlock(stmt.Body, fn)
		for _, catch := range stmt.Catches {
			a.analyzeBlock(catch.Body, fn)
		}
		a.analyzeBlock(stmt.Finally, fn)
	case *ast.MatchStatement:
		a.checkMatchExhaustiveness(stmt.Expr, stmt.Cases, stmt.Token)
		for _, matchCase := range stmt.Cases {
			a.analyzeBlock(matchCase.Body, fn)
		}
	case *ast.ClassStatement:
		a.declare(stmt.Name.Value, typeInfo{name: stmt.Name.Value, known: true})
		a.analyzeClassMembers(stmt)
	case *ast.InterfaceStatement:
		a.declare(stmt.Name.Value, typeInfo{name: stmt.Name.Value, known: true})
	case *ast.EnumStatement:
		a.declare(stmt.Name.Value, typeInfo{name: stmt.Name.Value, known: true})
		a.analyzeEnumMembers(stmt)
	case *ast.DelStatement:
		if stmt.Target == nil {
			return
		}
		info, ok := a.lookup(stmt.Target.Value)
		if !ok {
			a.errorAt(stmt.Target.Token, "del: unknown identifier %q", stmt.Target.Value)
			return
		}
		if a.isDeclarationName(stmt.Target.Value, info) {
			a.errorAt(stmt.Target.Token, "cannot del %q: del operates on variables, not declarations", stmt.Target.Value)
			return
		}
		a.markBindingDestroyed(stmt.Target.Value)
	}
}

func (a *Analyzer) analyzeBlock(block *ast.BlockStatement, fn *ast.FunctionStatement) {
	a.analyzeBlockWithNarrowing(block, fn, nil)
}

func (a *Analyzer) analyzeBlockWithNarrowing(block *ast.BlockStatement, fn *ast.FunctionStatement, narrowing map[string]typeInfo) {
	if block == nil {
		return
	}
	a.pushScope()
	defer a.popScope()
	for name, typ := range narrowing {
		if prev, ok := a.lookup(name); ok {
			typ.declaredNullable = prev.nullable || prev.declaredNullable
		}
		a.declare(name, typ)
	}
	for _, stmt := range block.Statements {
		a.analyzeStatement(stmt, fn)
		// Guard-pattern narrowing: if (x == null) { return/throw/... } narrows x to
		// non-null for all subsequent statements in this block.
		if ifStmt, ok := stmt.(*ast.IfStatement); ok && len(ifStmt.ElseIfs) == 0 && ifStmt.Alternative == nil {
			if a.blockAlwaysExits(ifStmt.Consequence) {
				_, altNarrowing := a.narrowingsForCondition(ifStmt.Condition)
				for name, typ := range altNarrowing {
					if prev, ok := a.lookup(name); ok {
						typ.declaredNullable = prev.nullable || prev.declaredNullable
					}
					a.declare(name, typ)
				}
			}
		}
	}
}

func (a *Analyzer) analyzeClassMembers(stmt *ast.ClassStatement) {
	popTypeParams := a.pushTypeParams(stmt.Generics)
	defer popTypeParams()
	members := stmt.Members
	if stmt.Destructor != nil {
		members = append(append([]ast.Statement{}, members...), stmt.Destructor)
	}
	for _, member := range members {
		method, ok := member.(*ast.FunctionStatement)
		if !ok {
			continue
		}
		a.pushScope()
		if !method.Static {
			a.declare("this", typeInfo{name: stmt.Name.Value, known: true})
		}
		for _, param := range method.Parameters {
			a.declare(param.Name.Value, a.parameterBindingType(param))
			if param.Default != nil {
				a.checkAssignable(param.Type, param.Default, fmt.Sprintf("cannot use %s default for %s parameter %s", a.expressionTypeName(param.Default).display(), param.Type.String(), param.Name.Value))
			}
		}
		a.analyzeBlock(method.Body, method)
		a.popScope()
	}
}

func (a *Analyzer) analyzeEnumMembers(stmt *ast.EnumStatement) {
	a.validateBackedEnum(stmt)
	for _, method := range stmt.Methods {
		switch strings.ToLower(method.Name.Value) {
		case "variant", "fields", "value":
			if strings.EqualFold(method.Name.Value, "value") && stmt.BackingType == nil {
				break
			}
			a.errorAt(method.Name.Token, "enum %s method %q collides with a built-in variant accessor", stmt.Name.Value, method.Name.Value)
		}
		a.pushScope()
		a.declare("this", typeInfo{name: stmt.Name.Value, known: true})
		for _, param := range method.Parameters {
			a.declare(param.Name.Value, a.parameterBindingType(param))
			if param.Default != nil {
				a.checkAssignable(param.Type, param.Default, fmt.Sprintf("cannot use %s default for %s parameter %s", a.expressionTypeName(param.Default).display(), param.Type.String(), param.Name.Value))
			}
		}
		a.analyzeBlock(method.Body, method)
		a.popScope()
	}
}

func (a *Analyzer) validateBackedEnum(stmt *ast.EnumStatement) {
	backed := stmt.BackingType != nil
	if backed {
		switch stmt.BackingType.Name {
		case "string", "int":
		default:
			a.errorAt(stmt.BackingType.Token, "enum %s backing type must be string or int, got %s", stmt.Name.Value, stmt.BackingType.Name)
		}
	}
	seen := map[string]token.Token{}
	for _, variant := range stmt.Variants {
		if variant.Name == nil {
			continue
		}
		if len(variant.FieldTypes) > 0 && variant.BackingValue != nil {
			a.errorAt(variant.Name.Token, "enum variant %s.%s cannot have both associated data and a backing value", stmt.Name.Value, variant.Name.Value)
		}
		if backed && variant.BackingValue == nil {
			a.errorAt(variant.Name.Token, "backed enum %s requires a backing value for %s", stmt.Name.Value, variant.Name.Value)
			continue
		}
		if !backed && variant.BackingValue != nil {
			a.errorAt(variant.Name.Token, "enum %s has a backing value for %s but no backing type", stmt.Name.Value, variant.Name.Value)
			continue
		}
		if !backed || variant.BackingValue == nil {
			continue
		}
		typ, key, ok := enumBackingLiteralInfo(variant.BackingValue)
		if !ok {
			a.errorAt(tokenOfExpression(variant.BackingValue), "enum backing value must be a string or int literal")
			continue
		}
		if typ != stmt.BackingType.Name {
			a.errorAt(tokenOfExpression(variant.BackingValue), "enum %s backing value for %s must be %s, got %s", stmt.Name.Value, variant.Name.Value, stmt.BackingType.Name, typ)
			continue
		}
		if first, exists := seen[key]; exists {
			a.errorAt(tokenOfExpression(variant.BackingValue), "enum %s backing value %s is already used at line %d:%d", stmt.Name.Value, key, first.Line, first.Column)
			continue
		}
		seen[key] = tokenOfExpression(variant.BackingValue)
	}
}

func enumBackingLiteralInfo(expr ast.Expression) (string, string, bool) {
	switch lit := expr.(type) {
	case *ast.StringLiteral:
		return "string", "string:" + lit.Value, true
	case *ast.IntegerLiteral:
		value, err := ast.ParseIntLiteral(lit.Value)
		if err != nil {
			return "", "", false
		}
		return "int", "int:" + value.String(), true
	case *ast.PrefixExpression:
		if lit.Operator == "-" {
			if intLit, ok := lit.Right.(*ast.IntegerLiteral); ok {
				value, err := ast.ParseIntLiteral("-" + intLit.Value)
				if err != nil {
					return "", "", false
				}
				return "int", "int:" + value.String(), true
			}
		}
	}
	return "", "", false
}

func (a *Analyzer) analyzeDeclaration(stmt *ast.DeclarationStatement) {
	a.checkBuiltinModuleShadow(stmt.Name, stmt.Type)
	a.checkAnnotationConstructorCall(stmt)
	var declared typeInfo
	if stmt.Type != nil {
		declared = a.typeInfoFromRef(stmt.Type)
	} else if stmt.Value != nil {
		declared = a.expressionTypeName(stmt.Value)
	}
	if declared.known {
		a.declare(stmt.Name.Value, declared)
	}
	if stmt.Value != nil {
		a.analyzeExpression(stmt.Value)
	}
	if stmt.Type == nil || stmt.Value == nil {
		return
	}
	a.validateCallExpression(stmt.Value, declared)
	a.validateContainerLiteral(declared, stmt.Value, stmt.Name.Value)
	a.checkAssignable(stmt.Type, stmt.Value, fmt.Sprintf("cannot assign %s to %s %s", a.expressionTypeName(stmt.Value).display(), stmt.Type.String(), stmt.Name.Value))
}

// builtinTypeNames are the lower-case type names the language treats
// as primitives. Includes pseudo-types like `iterable` and `callable`
// so legitimate stdlib signatures resolve.
var builtinTypeNames = map[string]struct{}{
	"string": {}, "int": {}, "float": {}, "decimal": {}, "bool": {},
	"bytes": {}, "list": {}, "dict": {}, "set": {}, "range": {},
	"void": {}, "any": {}, "null": {},
	"callable": {}, "func": {}, "function": {},
	"iterable": {}, "generator": {},
}

// ambientErrorClasses are the built-in error types every program may
// reference without declaring them (mirrors the engine's
// isBuiltinErrorClass).
var ambientErrorClasses = map[string]struct{}{
	"Error": {}, "RuntimeError": {}, "TypeError": {}, "ValueError": {},
	"IOError": {}, "ParseError": {}, "MatchError": {}, "ImmutableError": {},
	"PermissionError": {}, "AssertionError": {}, "FatalError": {},
	"TimeoutError": {}, "TlsError": {},
}

// IsAmbientTypeName reports whether name is a primitive, pseudo-type,
// ambient error class, or ambient native type - known without any
// declaration or import.
func IsAmbientTypeName(name string) bool {
	if _, ok := builtinTypeNames[strings.ToLower(name)]; ok {
		return true
	}
	if _, ok := ambientErrorClasses[name]; ok {
		return true
	}
	_, ok := ambientNativeTypeNames[name]
	return ok
}

func (a *Analyzer) isKnownTypeName(name string) bool {
	if name == "" {
		return true
	}
	if _, ok := builtinTypeNames[strings.ToLower(name)]; ok {
		return true
	}
	if _, ok := ambientErrorClasses[name]; ok {
		return true
	}
	if _, ok := ambientNativeTypeNames[name]; ok {
		return true
	}
	if a.typeParamScope[name] > 0 {
		return true
	}
	if _, ok := a.aliases[strings.ToLower(name)]; ok {
		return true
	}
	if _, ok := a.classes[name]; ok {
		return true
	}
	if _, ok := a.interfaces[name]; ok {
		return true
	}
	if _, ok := a.enums[name]; ok {
		return true
	}
	if a.importedNames[name] {
		return true
	}
	if _, ok := a.lookup(name); ok {
		return true
	}
	return false
}

// checkTypeRefExists recursively flags any bare type name in an
// annotation that resolves to no known type. Module-qualified names
// (containing a dot) are validated by the cross-module check layer.
// checkForInBinder enforces a present for-in loop-variable type like a typed declaration.
func (a *Analyzer) checkForInBinder(stmt *ast.ForStatement) {
	if stmt.VarType == nil {
		return
	}
	a.checkTypeRefExists(stmt.VarType, "for-in loop variable")
	if stmt.Iterable == nil || stmt.VarType.Name == "" || !a.isKnownTypeName(stmt.VarType.Name) {
		return
	}
	declared := a.typeInfoFromRef(stmt.VarType)
	names := stmt.VarNames
	if stmt.VarName != nil {
		names = []*ast.Identifier{stmt.VarName}
	}
	binds := a.forInBindingTypes(stmt.Iterable, len(names))
	for i, name := range names {
		if name == nil || i >= len(binds) {
			continue
		}
		if binds[i].known && binds[i].name != "" && !a.isAssignable(declared, binds[i]) {
			a.errorAt(stmt.VarType.Token, "cannot assign %s to %s %s", binds[i].display(), stmt.VarType.String(), name.Value)
		}
	}
}

func (a *Analyzer) checkTypeRefExists(ref *ast.TypeRef, context string) {
	if ref == nil {
		return
	}
	if ref.Operator != "" {
		a.checkTypeRefExists(ref.Left, context)
		a.checkTypeRefExists(ref.Right, context)
		return
	}
	for _, arg := range ref.Arguments {
		a.checkTypeRefExists(arg, context)
	}
	name := ref.Name
	if name == "" || strings.Contains(name, ".") {
		return
	}
	if a.isKnownTypeName(name) {
		return
	}
	a.errorAt(ref.Token, "unknown type %q in %s", name, context)
}

// validateGenericConstraints flags unknown type names in constraint
// clauses (<T implements Nope>), called with the params already pushed.
func (a *Analyzer) validateGenericConstraints(generics []*ast.TypeParam) {
	for _, g := range generics {
		if g == nil || g.Constraint == nil {
			continue
		}
		name := "type parameter"
		if g.Name != nil {
			name = "constraint of type parameter " + g.Name.Value
		}
		a.checkTypeRefExists(g.Constraint, name)
	}
}

// pushTypeParams adds generic type-param names to scope and returns a
// function that removes them.
func (a *Analyzer) pushTypeParams(generics []*ast.TypeParam) func() {
	for _, g := range generics {
		if g.Name != nil {
			a.typeParamScope[g.Name.Value]++
		}
	}
	return func() {
		for _, g := range generics {
			if g.Name != nil {
				a.typeParamScope[g.Name.Value]--
			}
		}
	}
}

// Declare exposes the analyzer's binding-registration so REPL sessions
// can re-seed bindings from prior prompts before analyzing a new one.
// typeName is wrapped in a minimal typeInfo; nullable and generic
// arguments are not preserved across prompt boundaries.
func (a *Analyzer) Declare(name, typeName string) {
	if name == "" {
		return
	}
	a.declare(name, typeInfo{name: typeName, known: true})
}

// validateContainerLiteral validates that the elements of a list/dict/set
// literal match the declared generic element type. For nested literals, the
// check recurses (e.g. `list<list<int>>` validates each inner list as
// `list<int>`). The function is a no-op when the declared type is not a
// generic collection or when no value is provided.
func (a *Analyzer) validateContainerLiteral(declared typeInfo, value ast.Expression, name string) {
	if !declared.known || len(declared.args) == 0 {
		return
	}
	switch declared.name {
	case "list", "set":
		var elements []ast.Expression
		switch lit := value.(type) {
		case *ast.ListLiteral:
			elements = lit.Elements
		case *ast.SetLiteral:
			elements = lit.Elements
		default:
			return
		}
		elemType := declared.args[0]
		for _, element := range elements {
			a.checkValueAgainstType(elemType, element, fmt.Sprintf("cannot use %s as %s element of %s %s", a.expressionTypeName(element).display(), elemType.display(), declared.name, name))
			a.validateContainerLiteral(elemType, element, name)
		}
	case "dict":
		lit, ok := value.(*ast.DictLiteral)
		if !ok {
			return
		}
		if len(declared.args) < 2 {
			return
		}
		keyType := declared.args[0]
		valueType := declared.args[1]
		for _, entry := range lit.Entries {
			a.checkValueAgainstType(keyType, entry.Key, fmt.Sprintf("cannot use %s as %s key of dict %s", a.expressionTypeName(entry.Key).display(), keyType.display(), name))
			a.checkValueAgainstType(valueType, entry.Value, fmt.Sprintf("cannot use %s as %s value of dict %s", a.expressionTypeName(entry.Value).display(), valueType.display(), name))
			a.validateContainerLiteral(valueType, entry.Value, name)
		}
	}
}

// checkTypedCollectionMethodCall validates that mutation methods on typed
// collections (list<T>.push, set<T>.add, dict<K,V>.set/insert) receive
// arguments compatible with the declared element/key/value types.
// checkPrimitiveMethodCall flags `value.method(...)` when value has a
// statically known built-in type (from a literal or typed binding) that
// has no such method. Conversion helpers and unknown/any types are
// never flagged.
func (a *Analyzer) checkPrimitiveMethodCall(call *ast.CallExpression) {
	if !a.methodChecks {
		return
	}
	selector, ok := call.Callee.(*ast.SelectorExpression)
	if !ok || selector.Name == nil || selector.Optional {
		return
	}
	recv := a.expressionTypeName(selector.Object)
	if !recv.known || recv.nullable {
		return
	}
	isPrimitive, exists := primitiveMethodLookup(recv.name, selector.Name.Value)
	if !isPrimitive || exists {
		return
	}
	// native.PrimitiveMethods is the authoritative built-in method set,
	// guarded against phantoms (every entry callable) and completeness
	// drift (the tripwire test), so an unlisted method is a real typo.
	a.errorAt(selector.Name.Token, "%s has no method %s", recv.name, selector.Name.Value)
}

// checkClassMethodCall flags `obj.method(...)` when obj is a typed
// instance of a resolvable class whose full hierarchy (parents +
// interfaces, across modules) has no such method or callable field.
// Only runs when a class-surface resolver is installed (the check
// command); stays silent on any uncertainty to avoid false positives.
func (a *Analyzer) checkClassMethodCall(call *ast.CallExpression) {
	if a.classSurface == nil {
		return
	}
	selector, ok := call.Callee.(*ast.SelectorExpression)
	if !ok || selector.Name == nil || selector.Optional {
		return
	}
	receiverType := a.expressionTypeName(selector.Object)
	if !receiverType.known || receiverType.nullable {
		return
	}
	members, resolvable := a.classSurface(receiverType.name)
	if !resolvable {
		return
	}
	if !members[strings.ToLower(selector.Name.Value)] {
		if isSelfReceiver(selector.Object) {
			return
		}
		a.errorAt(selector.Name.Token, "%s has no method %s", receiverType.name, selector.Name.Value)
		return
	}
	a.checkMethodCallArgs(call, selector, receiverType)
}

// a subclass may define the method, so existence on this/parent isn't sound
func isSelfReceiver(obj ast.Expression) bool {
	ident, ok := obj.(*ast.Identifier)
	return ok && (ident.Value == "this" || ident.Value == "parent")
}

// checkMethodCallArgs flags `obj.method(args)` when the method is resolvable
// but no overload can accept the given arguments (wrong arity or argument
// types), matching what both backends reject at runtime. Conservative: it
// stays silent on any uncertainty (named/spread args, unknown argument types,
// variadic overloads, or generic/untyped parameter positions) to avoid false
// positives.
func (a *Analyzer) checkMethodCallArgs(call *ast.CallExpression, selector *ast.SelectorExpression, receiverType typeInfo) {
	if a.classMethodSigs == nil {
		return
	}
	sigs, classTypeParams, ok := a.classMethodSigs(receiverType.name, strings.ToLower(selector.Name.Value))
	if !ok || len(sigs) == 0 {
		return
	}
	args := make([]typeInfo, 0, len(call.Arguments))
	for _, arg := range call.Arguments {
		if arg.Name != nil || arg.Spread {
			return
		}
		argType := a.expressionTypeName(arg.Value)
		if !argType.known {
			return
		}
		args = append(args, argType)
	}
	for _, sig := range sigs {
		if a.methodOverloadPossible(args, sig, classTypeParams) {
			return
		}
	}
	a.errorAt(selector.Name.Token, "no matching overload for %s.%s: got (%s)", receiverType.name, selector.Name.Value, displayTypeInfos(args))
}

// displayTypeInfos renders an argument/parameter type list for diagnostics.
func displayTypeInfos(infos []typeInfo) string {
	parts := make([]string, len(infos))
	for i, t := range infos {
		parts[i] = t.display()
	}
	return strings.Join(parts, ", ")
}

// methodOverloadPossible reports whether one overload could accept args.
// Generic, untyped, or unresolvable parameter positions are treated as
// wildcards, and variadic overloads are accepted outright, so the result is
// only ever false when an overload is certainly incompatible.
func (a *Analyzer) methodOverloadPossible(args []typeInfo, sig MethodSignature, classTypeParams []string) bool {
	if sig.Variadic {
		return true
	}
	generic := map[string]bool{}
	for _, t := range classTypeParams {
		generic[t] = true
	}
	for _, t := range sig.TypeParams {
		generic[t] = true
	}
	params := make([]typeInfo, len(sig.Params))
	for i, pref := range sig.Params {
		if pref == nil || refMentionsTypeParam(pref, generic) {
			params[i] = typeInfo{name: "any", known: true}
			continue
		}
		params[i] = a.typeInfoFromRef(pref)
		if !params[i].known {
			params[i] = typeInfo{name: "any", known: true}
		}
	}
	return a.callArgumentsCompatible(args, params, sig.Required)
}

// refMentionsTypeParam reports whether a type reference (anywhere in its
// argument / union tree) names one of the given generic parameters.
func refMentionsTypeParam(ref *ast.TypeRef, generic map[string]bool) bool {
	if ref == nil {
		return false
	}
	if generic[ref.Name] {
		return true
	}
	for _, arg := range ref.Arguments {
		if refMentionsTypeParam(arg, generic) {
			return true
		}
	}
	return refMentionsTypeParam(ref.Left, generic) || refMentionsTypeParam(ref.Right, generic)
}

func (a *Analyzer) checkTypedCollectionMethodCall(call *ast.CallExpression) {
	selector, ok := call.Callee.(*ast.SelectorExpression)
	if !ok {
		return
	}
	receiver, ok := selector.Object.(*ast.Identifier)
	if !ok {
		return
	}
	receiverType, ok := a.lookup(receiver.Value)
	if !ok || !receiverType.known || len(receiverType.args) == 0 {
		return
	}
	method := strings.ToLower(selector.Name.Value)
	switch receiverType.name {
	case "list":
		switch method {
		case "push", "prepend", "append", "unshift":
			a.validateArgs(call.Arguments, []typeInfo{receiverType.args[0]},
				fmt.Sprintf("list<%s>.%s element", receiverType.args[0].display(), selector.Name.Value), receiver.Value)
		case "fill":
			// fill(value, count) - first arg is the element.
			if len(call.Arguments) >= 1 {
				a.validateArgAt(call.Arguments[0], receiverType.args[0],
					fmt.Sprintf("list<%s>.fill element", receiverType.args[0].display()), receiver.Value)
			}
		case "insert":
			// insert(index, value) - second arg is the element.
			if len(call.Arguments) >= 2 {
				a.validateArgAt(call.Arguments[1], receiverType.args[0],
					fmt.Sprintf("list<%s>.insert element", receiverType.args[0].display()), receiver.Value)
			}
		case "set":
			// set(index, value)
			if len(call.Arguments) >= 2 {
				a.validateArgAt(call.Arguments[1], receiverType.args[0],
					fmt.Sprintf("list<%s>.set element", receiverType.args[0].display()), receiver.Value)
			}
		}
	case "set":
		if method == "add" || method == "insert" {
			a.validateArgs(call.Arguments, []typeInfo{receiverType.args[0]},
				fmt.Sprintf("set<%s>.%s element", receiverType.args[0].display(), selector.Name.Value), receiver.Value)
		}
	case "dict":
		if len(receiverType.args) < 2 {
			return
		}
		key := receiverType.args[0]
		value := receiverType.args[1]
		switch method {
		case "set", "insert", "put":
			// (key, value)
			if len(call.Arguments) >= 1 {
				a.validateArgAt(call.Arguments[0], key,
					fmt.Sprintf("dict<%s,%s>.%s key", key.display(), value.display(), selector.Name.Value), receiver.Value)
			}
			if len(call.Arguments) >= 2 {
				a.validateArgAt(call.Arguments[1], value,
					fmt.Sprintf("dict<%s,%s>.%s value", key.display(), value.display(), selector.Name.Value), receiver.Value)
			}
		}
	}
}

func (a *Analyzer) validateArgs(args []ast.CallArgument, expected []typeInfo, role, receiver string) {
	for i, arg := range args {
		if i >= len(expected) {
			break
		}
		a.validateArgAt(arg, expected[i], role, receiver)
	}
}

func (a *Analyzer) validateArgAt(arg ast.CallArgument, target typeInfo, role, receiver string) {
	if arg.Spread {
		return
	}
	a.checkValueAgainstType(target, arg.Value, fmt.Sprintf("cannot use %s as %s of %s", a.expressionTypeName(arg.Value).display(), role, receiver))
}

// checkValueAgainstType reports an error if expr cannot be assigned to the
// type described by target. Used by validateContainerLiteral and the
// .push/.set/.add element-type check.
func (a *Analyzer) checkValueAgainstType(target typeInfo, expr ast.Expression, message string) {
	if !target.known {
		return
	}
	if target.name == "any" {
		return
	}
	actual := a.expressionTypeName(expr)
	if !actual.known {
		return
	}
	// An `any` actual is statically opaque; runtime validation owns it.
	if strings.EqualFold(actual.name, "any") {
		return
	}
	tok := tokenOfExpression(expr)
	if actual.name == "null" {
		if !target.nullable {
			a.errorAt(tok, "%s", message)
		}
		return
	}
	if isNumericLiteralWidening(target.name, expr) {
		return
	}
	if !target.nullable && actual.nullable {
		a.errorAt(tok, "%s", message)
		return
	}
	if !a.isAssignable(target, actual) {
		a.errorAt(tok, "%s", message)
	}
}

func (a *Analyzer) analyzeReturn(stmt *ast.ReturnStatement, fn *ast.FunctionStatement) {
	if fn == nil || fn.ReturnType == nil {
		return
	}
	if stmt.Value == nil {
		/* Bare `return;` is always legal in a void function -
		 * there's nothing to assign, the early-exit just terminates
		 * the function body. For non-void returns, bare return is
		 * legal when the return type is nullable or `any`.
		 * Otherwise it's an error: the caller would observe a null
		 * where it expected a concrete value. */
		if !fn.ReturnType.Nullable &&
			!strings.EqualFold(fn.ReturnType.Name, "any") &&
			!strings.EqualFold(fn.ReturnType.Name, "void") {
			a.errorAt(stmt.Token, "cannot return null from %s returning %s", fn.Name.Value, fn.ReturnType.String())
		}
		return
	}
	a.analyzeExpression(stmt.Value)
	a.validateCallExpression(stmt.Value, a.typeInfoFromRef(fn.ReturnType))
	a.checkAssignable(fn.ReturnType, stmt.Value, fmt.Sprintf("cannot return %s from %s returning %s", a.expressionTypeName(stmt.Value).display(), fn.Name.Value, fn.ReturnType.String()))
}

func (a *Analyzer) checkAssignable(target *ast.TypeRef, expr ast.Expression, message string) {
	if target == nil || target.Operator != "" {
		return
	}
	// generic type params are unconstrained at the definition site
	if target.Name != "" && a.typeParamScope[target.Name] > 0 {
		return
	}
	targetInfo := a.typeInfoFromRef(target)
	actual := a.expressionTypeName(expr)
	if !actual.known {
		return
	}
	if actual.name == "null" {
		if !target.Nullable && targetInfo.name != "any" {
			a.errorf("%s", message)
		}
		return
	}
	if targetInfo.name == "any" {
		return
	}
	if isNumericLiteralWidening(targetInfo.name, expr) {
		return
	}
	if target.ListAlias || targetInfo.name == "list" {
		if actual.name != "list" {
			a.errorf("%s", message)
			return
		}
		if len(targetInfo.args) > 0 && len(actual.args) > 0 && !a.isAssignable(targetInfo, actual) {
			a.errorf("%s", message)
		}
		return
	}
	if !targetInfo.nullable && actual.nullable {
		a.errorf("%s", message)
		return
	}
	if !a.isAssignable(targetInfo, actual) {
		a.errorf("%s", message)
	}
}

func (a *Analyzer) validateCallExpression(expr ast.Expression, expected typeInfo) {
	call, ok := expr.(*ast.CallExpression)
	if !ok {
		return
	}
	ident, ok := call.Callee.(*ast.Identifier)
	if !ok {
		return
	}
	if _, ok := a.classes[ident.Value]; ok {
		return
	}
	overloads := a.functions[strings.ToLower(ident.Value)]
	if len(overloads) == 0 {
		return
	}
	// Generic call-site inference and constraint check fires regardless of
	// whether the surrounding context expects a particular return type.
	a.checkGenericCallInference(call, ident.Value, overloads)
	if !expected.known {
		return
	}
	args := make([]typeInfo, 0, len(call.Arguments))
	for _, arg := range call.Arguments {
		if arg.Name != nil {
			return
		}
		argType := a.expressionTypeName(arg.Value)
		if !argType.known {
			return
		}
		args = append(args, argType)
	}
	matches := []methodInfo{}
	for _, overload := range overloads {
		if !a.callArgumentsCompatible(args, overload.parameters, overload.minArgs) {
			continue
		}
		if !a.returnTypeCompatible(expected, overload.returnType) {
			continue
		}
		matches = append(matches, overload)
	}
	if len(matches) == 0 {
		a.errorf("no matching overload for %s returning %s", ident.Value, expected.display())
		return
	}
	if len(matches) > 1 {
		a.errorf("ambiguous overload for %s returning %s", ident.Value, expected.display())
	}
}

// checkConstructorTypeArgs flags Box<string>(42): explicit bindings
// substitute into bare type-param params, then the normal compatibility
// walk runs (covariant passing stays accepted; opaque args defer to runtime).
func (a *Analyzer) checkConstructorTypeArgs(call *ast.CallExpression) {
	if len(call.TypeArguments) == 0 {
		return
	}
	ident, ok := call.Callee.(*ast.Identifier)
	if !ok {
		return
	}
	a.checkConstructorCallBindings(call, ident, call.TypeArguments)
}

// checkAnnotationConstructorCall applies the same validation to
// `Box<int> x = Box("text")`: the annotation's args are the bindings.
func (a *Analyzer) checkAnnotationConstructorCall(stmt *ast.DeclarationStatement) {
	if stmt.Type == nil || stmt.Type.Operator != "" || len(stmt.Type.Arguments) == 0 || stmt.Value == nil {
		return
	}
	call, ok := stmt.Value.(*ast.CallExpression)
	if !ok || len(call.TypeArguments) > 0 {
		return
	}
	ident, ok := call.Callee.(*ast.Identifier)
	if !ok || ident.Value != stmt.Type.Name {
		return
	}
	a.checkConstructorCallBindings(call, ident, stmt.Type.Arguments)
}

func (a *Analyzer) checkConstructorCallBindings(call *ast.CallExpression, ident *ast.Identifier, typeRefs []*ast.TypeRef) {
	cls, ok := a.classes[ident.Value]
	if !ok || len(cls.typeParams) == 0 {
		return
	}
	bindings := map[string]typeInfo{}
	for i, arg := range typeRefs {
		if i >= len(cls.typeParams) {
			break
		}
		if arg == nil || arg.Operator != "" || len(arg.Arguments) > 0 {
			continue
		}
		ti := a.typeInfoFromRef(arg)
		if ti.known {
			bindings[strings.ToLower(cls.typeParams[i])] = ti
		}
	}
	if len(bindings) == 0 {
		return
	}
	ctors := cls.methods[strings.ToLower(ident.Value)]
	if len(ctors) == 0 {
		return
	}
	args := make([]typeInfo, 0, len(call.Arguments))
	for _, arg := range call.Arguments {
		if arg.Name != nil || arg.Spread {
			return
		}
		argType := a.expressionTypeName(arg.Value)
		// An `any`-typed argument is statically opaque; runtime
		// enforcement owns that case.
		if !argType.known || strings.EqualFold(argType.name, "any") {
			return
		}
		args = append(args, argType)
	}
	classParams := map[string]bool{}
	for _, p := range cls.typeParams {
		classParams[strings.ToLower(p)] = true
	}
	expected := ""
	for _, ctor := range ctors {
		substituted := make([]typeInfo, len(ctor.parameters))
		copy(substituted, ctor.parameters)
		fullyBound := true
		for i, p := range substituted {
			if len(p.args) > 0 || !classParams[strings.ToLower(p.name)] {
				continue
			}
			bound, isBound := bindings[strings.ToLower(p.name)]
			if !isBound {
				fullyBound = false
				break
			}
			bound.nullable = bound.nullable || p.nullable
			substituted[i] = bound
		}
		if !fullyBound {
			return
		}
		if a.callArgumentsCompatible(args, substituted, ctor.minArgs) {
			return
		}
		if expected == "" && len(args) >= ctor.minArgs && len(args) <= len(ctor.parameters) {
			expected = displayTypeInfos(substituted)
		}
	}
	if expected == "" {
		// Arity mismatches are reported elsewhere; only the
		// binding-contradiction case is this check's to flag.
		return
	}
	a.errorAt(ident.Token, "no matching overload for %s: got (%s), expected (%s)", ident.Value, displayTypeInfos(args), expected)
}

// validateCallStatementArgs flags collection element-type mismatches on bare
// statement calls (e.g. `wantStrings(ints);`) that the bytecode compiler cannot
// see, since it strips collection element args. Scalar / arity / base-type
// mismatches are left to the bytecode compiler to avoid duplicate diagnostics:
// an error is raised only when some overload matches on base types but none
// matches once element types are checked.
func (a *Analyzer) validateCallStatementArgs(expr ast.Expression) {
	call, ok := expr.(*ast.CallExpression)
	if !ok {
		return
	}
	ident, ok := call.Callee.(*ast.Identifier)
	if !ok {
		return
	}
	if _, ok := a.classes[ident.Value]; ok {
		return
	}
	overloads := a.functions[strings.ToLower(ident.Value)]
	if len(overloads) == 0 {
		return
	}
	args := make([]typeInfo, 0, len(call.Arguments))
	for _, arg := range call.Arguments {
		if arg.Name != nil || arg.Spread {
			return
		}
		argType := a.expressionTypeName(arg.Value)
		if !argType.known {
			return
		}
		args = append(args, argType)
	}
	baseMatch := false
	for _, overload := range overloads {
		if a.callArgumentsCompatible(args, overload.parameters, overload.minArgs) {
			return
		}
		if a.callArgumentsBaseCompatible(args, overload.parameters, overload.minArgs) {
			baseMatch = true
		}
	}
	if baseMatch {
		expected := ""
		for _, overload := range overloads {
			if a.callArgumentsBaseCompatible(args, overload.parameters, overload.minArgs) {
				expected = displayTypeInfos(overload.parameters)
				break
			}
		}
		a.errorf("no matching overload for %s: got (%s), expected (%s)", ident.Value, displayTypeInfos(args), expected)
	}
}

// callArgumentsBaseCompatible mirrors callArgumentsCompatible but compares only
// the base type names (ignoring collection element args), matching what the
// bytecode compiler resolves at compile time.
func (a *Analyzer) callArgumentsBaseCompatible(args, parameters []typeInfo, minArgs int) bool {
	if len(args) < minArgs || len(args) > len(parameters) {
		return false
	}
	for i, arg := range args {
		if !parameters[i].known || !arg.known {
			return false
		}
		if arg.name == "null" {
			if !parameters[i].nullable {
				return false
			}
			continue
		}
		if !parameters[i].nullable && arg.nullable {
			return false
		}
		if parameters[i].name != "any" && !a.isAssignableType(parameters[i].name, arg.name) {
			return false
		}
	}
	return true
}

// checkGenericCallInference walks each overload, infers the binding of every
// type parameter T from the argument types, and verifies (a) T is bound to a
// single type across multiple argument positions and (b) the inferred binding
// satisfies the parameter's declared constraint. Only fires when at least one
// overload has type parameters; non-generic overloads are unaffected.
func (a *Analyzer) checkGenericCallInference(call *ast.CallExpression, name string, overloads []methodInfo) {
	// Collect argument types up front.
	args := make([]typeInfo, 0, len(call.Arguments))
	for _, arg := range call.Arguments {
		if arg.Name != nil || arg.Spread {
			return
		}
		args = append(args, a.expressionTypeName(arg.Value))
	}
	// If a unique overload matches by arity AND has type params, run the
	// inference check on it.
	var generic *methodInfo
	for i := range overloads {
		if len(overloads[i].typeParams) == 0 {
			continue
		}
		if len(overloads[i].parameters) != len(args) {
			continue
		}
		if generic != nil {
			return // ambiguous; let the overload-resolution path report it
		}
		copy := overloads[i]
		generic = &copy
	}
	if generic == nil {
		return
	}
	paramSet := map[string]bool{}
	for _, tp := range generic.typeParams {
		paramSet[strings.ToLower(tp.name)] = true
	}
	bindings := map[string]typeInfo{}
	for i, paramType := range generic.parameters {
		if i >= len(args) {
			break
		}
		actual := args[i]
		if !actual.known {
			continue
		}
		a.inferTypeBinding(paramType, actual, paramSet, bindings, name)
	}
	for _, tp := range generic.typeParams {
		key := strings.ToLower(tp.name)
		bound, ok := bindings[key]
		if !ok || !bound.known {
			continue
		}
		if !tp.constraint.known || tp.constraint.name == "any" {
			continue
		}
		if !a.isAssignableType(tp.constraint.name, bound.name) {
			a.errorf("type parameter %s of %s is inferred as %s which does not satisfy constraint %s",
				tp.name, name, bound.display(), tp.constraint.display())
		}
	}
}

// inferTypeBinding walks paramType (a parameter's declared type) alongside
// actual (an argument's inferred type). When paramType is a bare type-parameter
// reference (its name matches a key in paramSet), bind it to actual. If a
// previous binding exists and differs, raise a consistency error. Generic
// argument lists are walked structurally so e.g. `list<T>` paired with
// `list<int>` binds T -> int.
func (a *Analyzer) inferTypeBinding(paramType, actual typeInfo, paramSet map[string]bool, bindings map[string]typeInfo, name string) {
	if !paramType.known || !actual.known {
		return
	}
	key := strings.ToLower(paramType.name)
	if paramSet[key] {
		if existing, ok := bindings[key]; ok && existing.known {
			if !sameTypeInfo(existing, actual) && !a.isAssignableType(existing.name, actual.name) && !a.isAssignableType(actual.name, existing.name) {
				a.errorf("type parameter %s of %s is bound to both %s and %s",
					paramType.name, name, existing.display(), actual.display())
			}
			return
		}
		bindings[key] = actual
		return
	}
	// Structural walk for generic containers: list<T>, dict<K,V>, Box<T>, etc.
	if len(paramType.args) > 0 && len(actual.args) == len(paramType.args) {
		for i := range paramType.args {
			a.inferTypeBinding(paramType.args[i], actual.args[i], paramSet, bindings, name)
		}
	}
}

// checkFunctionCallTypeArgs flags explicit call-site type arguments (firstMatch<float>(...)) that contradict the actual arguments.
func (a *Analyzer) checkFunctionCallTypeArgs(call *ast.CallExpression, name string, overloads []methodInfo) {
	if len(call.TypeArguments) == 0 {
		return
	}
	args, ok := a.positionalArgTypes(call)
	if !ok {
		return
	}
	var generic *methodInfo
	for i := range overloads {
		if len(overloads[i].typeParams) == 0 || len(overloads[i].parameters) != len(args) {
			continue
		}
		if generic != nil {
			return // ambiguous; overload resolution reports it
		}
		dup := overloads[i]
		generic = &dup
	}
	if generic == nil {
		return
	}
	names := make([]string, len(generic.typeParams))
	for i, tp := range generic.typeParams {
		names[i] = tp.name
	}
	a.validateExplicitTypeArgs(name, call.Token, names, generic.parameters, args, call.TypeArguments)
}

// checkMethodCallTypeArgs applies the same explicit-type-argument validation to obj.method<float>(...) calls.
func (a *Analyzer) checkMethodCallTypeArgs(call *ast.CallExpression) {
	if len(call.TypeArguments) == 0 || a.classMethodSigs == nil {
		return
	}
	selector, ok := call.Callee.(*ast.SelectorExpression)
	if !ok || selector.Name == nil || selector.Optional {
		return
	}
	receiverType := a.expressionTypeName(selector.Object)
	if !receiverType.known || receiverType.nullable {
		return
	}
	sigs, _, ok := a.classMethodSigs(receiverType.name, strings.ToLower(selector.Name.Value))
	if !ok {
		return
	}
	args, ok := a.positionalArgTypes(call)
	if !ok {
		return
	}
	var sig *MethodSignature
	for i := range sigs {
		if len(sigs[i].TypeParams) == 0 || sigs[i].Variadic || len(sigs[i].Params) != len(args) {
			continue
		}
		if sig != nil {
			return // ambiguous
		}
		dup := sigs[i]
		sig = &dup
	}
	if sig == nil {
		return
	}
	params := make([]typeInfo, len(sig.Params))
	for i, pref := range sig.Params {
		params[i] = a.typeInfoFromRef(pref)
	}
	a.validateExplicitTypeArgs(receiverType.name+"."+selector.Name.Value, selector.Name.Token, sig.TypeParams, params, args, call.TypeArguments)
}

// positionalArgTypes returns the inferred type of each call argument; ok=false when any argument is named or spread.
func (a *Analyzer) positionalArgTypes(call *ast.CallExpression) ([]typeInfo, bool) {
	for _, arg := range call.Arguments {
		if arg.Name != nil || arg.Spread {
			return nil, false
		}
	}
	args := make([]typeInfo, len(call.Arguments))
	for i, arg := range call.Arguments {
		args[i] = a.expressionTypeName(arg.Value)
	}
	return args, true
}

// validateExplicitTypeArgs binds typeArgs to the named type params, substitutes them through params, and flags a mismatched actual argument.
func (a *Analyzer) validateExplicitTypeArgs(name string, tok token.Token, typeParamNames []string, params, args []typeInfo, typeArgs []*ast.TypeRef) {
	if len(typeArgs) > len(typeParamNames) {
		a.errorAt(tok, "%s has %d type parameter(s) but %d type argument(s) were given", name, len(typeParamNames), len(typeArgs))
		return
	}
	paramSet := map[string]bool{}
	for _, n := range typeParamNames {
		paramSet[strings.ToLower(n)] = true
	}
	bindings := map[string]typeInfo{}
	for i, ref := range typeArgs {
		if ref == nil || i >= len(typeParamNames) {
			continue
		}
		if ti := a.typeInfoFromRef(ref); ti.known {
			bindings[strings.ToLower(typeParamNames[i])] = ti
		}
	}
	if len(bindings) == 0 {
		return
	}
	for i, p := range params {
		if i >= len(args) {
			break
		}
		sub, ok := substituteTypeParam(p, bindings, paramSet)
		if !ok || !sub.known || strings.EqualFold(sub.name, "any") {
			continue
		}
		actual := args[i]
		if !actual.known || actual.name == "null" || strings.EqualFold(actual.name, "any") {
			continue
		}
		if !a.isAssignable(sub, actual) {
			a.errorAt(tok, "%s: cannot pass %s as %s required by the explicit type argument", name, actual.display(), sub.display())
			return
		}
	}
}

// substituteTypeParam replaces type-parameter references in ti (recursing through args, so list<T> becomes list<float>); ok=false if an unbound parameter is referenced.
func substituteTypeParam(ti typeInfo, bindings map[string]typeInfo, paramSet map[string]bool) (typeInfo, bool) {
	key := strings.ToLower(ti.name)
	if paramSet[key] {
		bound, ok := bindings[key]
		if !ok {
			return ti, false
		}
		bound.nullable = bound.nullable || ti.nullable
		return bound, true
	}
	if len(ti.args) == 0 {
		return ti, true
	}
	out := ti
	out.args = make([]typeInfo, len(ti.args))
	for i, arg := range ti.args {
		sub, ok := substituteTypeParam(arg, bindings, paramSet)
		if !ok {
			return ti, false
		}
		out.args[i] = sub
	}
	return out, true
}

func (a *Analyzer) callArgumentsCompatible(args, parameters []typeInfo, minArgs int) bool {
	if len(args) < minArgs || len(args) > len(parameters) {
		return false
	}
	for i, arg := range args {
		if !parameters[i].known || !arg.known {
			return false
		}
		if arg.name == "null" {
			if !parameters[i].nullable && parameters[i].name != "any" {
				return false
			}
			continue
		}
		if !parameters[i].nullable && arg.nullable {
			return false
		}
		if parameters[i].name != "any" && !a.isAssignable(parameters[i], arg) {
			return false
		}
	}
	return true
}

func (a *Analyzer) returnTypeCompatible(expected, actual typeInfo) bool {
	if expected.name == "any" {
		return true
	}
	// An `any` actual is statically opaque; runtime validation owns it.
	if actual.known && strings.EqualFold(actual.name, "any") {
		return true
	}
	if !actual.known {
		return expected.nullable
	}
	if !expected.nullable && actual.nullable {
		return false
	}
	return a.isAssignable(expected, actual)
}

// checkDecimalFloatMix statically flags decimal+float arithmetic (the runtime precision wall) when both operand types are known.
func (a *Analyzer) checkDecimalFloatMix(expr *ast.InfixExpression) {
	switch expr.Operator {
	case "+", "-", "*", "/", "//", "%", "**":
	default:
		return
	}
	lt := a.expressionTypeName(expr.Left)
	rt := a.expressionTypeName(expr.Right)
	if !lt.known || !rt.known {
		return
	}
	if (lt.name == "decimal" && rt.name == "float") || (lt.name == "float" && rt.name == "decimal") {
		a.errorAt(expr.Token, "cannot mix decimal and float in %s (got %s and %s): cast one side - 'as float' drops decimal exactness, 'as decimal' adopts the float's imprecision", expr.Operator, lt.name, rt.name)
	}
}

func (a *Analyzer) analyzeExpression(expr ast.Expression) {
	if a.exprTypes != nil && expr != nil {
		a.recordExprType(expr)
		a.recordSkippedExpression(expr)
	}
	switch expr := expr.(type) {
	case *ast.Identifier:
		a.checkBindingNotDestroyed(expr)
		if _, shadowed := a.lookup(expr.Value); a.moduleAliases[expr.Value] && !shadowed {
			a.errorAt(expr.Token, "'%s' is a module, not a value; reference its members as '%s.<name>' or alias the import with 'import %s as <name>'", expr.Value, expr.Value, expr.Value)
		}
	case *ast.AssignmentExpression:
		a.analyzeAssignment(expr)
	case *ast.InfixExpression:
		a.analyzeExpression(expr.Left)
		// instanceof's right operand is a type reference, not a value.
		if expr.Operator != "instanceof" {
			a.analyzeExpression(expr.Right)
		}
		a.checkDecimalFloatMix(expr)
		if expr.Operator == "//" || expr.Operator == "%" || expr.Operator == "/" {
			if lit, ok := expr.Right.(*ast.IntegerLiteral); ok && strings.Trim(lit.Value, "0_") == "" {
				a.ruleWarningAt(expr.Token, "div-by-zero", "%q by literal zero always throws at runtime", expr.Operator)
			}
		}
	case *ast.PrefixExpression:
		a.analyzeExpression(expr.Right)
	case *ast.PostfixExpression:
		a.analyzeExpression(expr.Left)
	case *ast.MatchExpression:
		a.checkMatchExhaustiveness(expr.Expr, expr.Cases, expr.Token)
	case *ast.SelectorExpression:
		a.checkUnimportedModuleSelector(expr)
		if !a.isModuleValueRef(expr.Object) {
			a.analyzeExpression(expr.Object)
		}
	case *ast.IndexExpression:
		a.analyzeExpression(expr.Left)
		a.analyzeExpression(expr.Index)
	case *ast.CallExpression:
		a.analyzeExpression(expr.Callee)
		if ident, ok := expr.Callee.(*ast.Identifier); ok {
			a.checkBareCalleeKnown(ident)
		}
		moduleArgAllowed := callAcceptsModuleArg(expr.Callee)
		for _, arg := range expr.Arguments {
			if moduleArgAllowed && a.isModuleValueRef(arg.Value) {
				continue
			}
			a.analyzeExpression(arg.Value)
		}
		a.checkTypedCollectionMethodCall(expr)
		a.checkPrimitiveMethodCall(expr)
		a.checkClassMethodCall(expr)
		a.checkMethodCallTypeArgs(expr)
		a.checkConstructorTypeArgs(expr)
		a.checkDeprecatedCall(expr)
		if ident, ok := expr.Callee.(*ast.Identifier); ok {
			if overloads := a.functions[strings.ToLower(ident.Value)]; len(overloads) > 0 {
				a.checkGenericCallInference(expr, ident.Value, overloads)
				a.checkFunctionCallTypeArgs(expr, ident.Value, overloads)
			}
		}
	case *ast.ListLiteral:
		for _, element := range expr.Elements {
			a.analyzeExpression(element)
		}
	case *ast.DictLiteral:
		for _, entry := range expr.Entries {
			a.analyzeExpression(entry.Key)
			a.analyzeExpression(entry.Value)
		}
	case *ast.SetLiteral:
		for _, element := range expr.Elements {
			a.analyzeExpression(element)
		}
	case *ast.PartialExpression:
		a.analyzeExpression(expr.Callee)
		for _, arg := range expr.Arguments {
			if !arg.Hole {
				a.analyzeExpression(arg.Value)
			}
		}
	}
}

func (a *Analyzer) isModuleValueRef(expr ast.Expression) bool {
	ident, ok := expr.(*ast.Identifier)
	return ok && a.moduleAliases[ident.Value]
}

// checkUnimportedModuleSelector errors when a known engine module name is the base of a selector without an import; the VM resolved pure natives bare while the evaluator threw at runtime (import required as of 1.19.0).
func (a *Analyzer) checkUnimportedModuleSelector(expr *ast.SelectorExpression) {
	ident, ok := expr.Object.(*ast.Identifier)
	if !ok {
		return
	}
	name := ident.Value
	switch name {
	case "string", "bytes", "int", "float", "decimal", "bool", "list", "dict", "set", "any", "null", "void", "this", "parent":
		return
	}
	if a.moduleAliases[name] || a.importedNames[name] {
		return
	}
	if _, declared := a.declaredNames[name]; declared {
		return
	}
	if _, bound := a.lookup(name); bound {
		return
	}
	if _, isClass := a.classes[name]; isClass {
		return
	}
	if _, isEnum := a.enums[name]; isEnum {
		return
	}
	for alias := range a.moduleAliases {
		if strings.HasPrefix(alias, name+".") {
			return
		}
	}
	if !native.IsNativeModule(name) {
		return
	}
	a.errorAt(ident.Token, "module %q is used without an import; add 'import %s;'", name, name)
}

// dir(M) accepts a module name as its argument.
func callAcceptsModuleArg(callee ast.Expression) bool {
	ident, ok := callee.(*ast.Identifier)
	return ok && ident.Value == "dir"
}

// checkBindingNotDestroyed emits a diagnostic when the identifier
// resolves to a binding that has been retired by `del`.
func (a *Analyzer) checkBindingNotDestroyed(ident *ast.Identifier) {
	if ident == nil {
		return
	}
	info, ok := a.lookup(ident.Value)
	if !ok || !info.destroyed {
		return
	}
	a.errorAt(ident.Token, "use of destroyed binding %q", ident.Value)
}

// markBindingDestroyed walks scopes inner-to-outer to find the
// binding for `name` and flips its destroyed flag. Called by the
// `del x` statement's analyzer hook.
// checkBareCalleeKnown errors on a call to a bare name declared nowhere in
// the file, so the evaluator path rejects what the bytecode compiler
// already rejects. The declared-anywhere set (not lexical scope) keeps it
// free of false positives from the analyzer's partial scope tracking.
func (a *Analyzer) checkBareCalleeKnown(ident *ast.Identifier) {
	name := ident.Value
	if name == "" || name == "this" {
		return
	}
	if _, ok := a.declaredNames[name]; ok {
		return
	}
	if _, ok := a.lookup(name); ok {
		return
	}
	if _, ok := a.functions[strings.ToLower(name)]; ok {
		return
	}
	if _, ok := a.classes[name]; ok {
		return
	}
	if _, ok := a.interfaces[name]; ok {
		return
	}
	if _, ok := a.enums[name]; ok {
		return
	}
	if _, ok := a.aliases[strings.ToLower(name)]; ok {
		return
	}
	if _, ok := ambientErrorClasses[name]; ok {
		return
	}
	if a.importedNames[name] || a.moduleAliases[name] {
		return
	}
	for _, builtin := range native.BareBuiltins {
		if builtin == name {
			return
		}
	}
	a.errorAt(ident.Token, "unknown function %q", name)
}

// collectDeclaredNames gathers every name bound by a declaration anywhere in
// the statement tree, feeding checkBareCalleeKnown's declared-anywhere set.
func collectDeclaredNames(stmts []ast.Statement, into map[string]struct{}) {
	addIdent := func(ident *ast.Identifier) {
		if ident != nil && ident.Value != "" {
			into[ident.Value] = struct{}{}
		}
	}
	var walk func(stmt ast.Statement)
	walkBlock := func(block *ast.BlockStatement) {
		if block != nil {
			collectDeclaredNames(block.Statements, into)
		}
	}
	walk = func(stmt ast.Statement) {
		switch s := stmt.(type) {
		case *ast.DeclarationStatement:
			addIdent(s.Name)
		case *ast.DestructuringStatement:
			for _, name := range s.Names {
				addIdent(name)
			}
		case *ast.FunctionStatement:
			addIdent(s.Name)
			for _, param := range s.Parameters {
				addIdent(param.Name)
			}
			walkBlock(s.Body)
		case *ast.ClassStatement:
			addIdent(s.Name)
			collectDeclaredNames(s.Members, into)
			if s.Destructor != nil {
				walk(s.Destructor)
			}
		case *ast.InterfaceStatement:
			addIdent(s.Name)
		case *ast.EnumStatement:
			addIdent(s.Name)
		case *ast.ExportStatement:
			walk(s.Statement)
		case *ast.InitStatement:
			walkBlock(s.Body)
		case *ast.WithStatement:
			addIdent(s.Name)
			walkBlock(s.Body)
		case *ast.IfStatement:
			walkBlock(s.Consequence)
			for _, elseIf := range s.ElseIfs {
				walkBlock(elseIf.Body)
			}
			walkBlock(s.Alternative)
		case *ast.WhileStatement:
			walkBlock(s.Body)
		case *ast.ForStatement:
			addIdent(s.VarName)
			for _, name := range s.VarNames {
				addIdent(name)
			}
			if s.Init != nil {
				walk(s.Init)
			}
			walkBlock(s.Body)
		case *ast.TryStatement:
			walkBlock(s.Body)
			for _, catch := range s.Catches {
				addIdent(catch.Name)
				walkBlock(catch.Body)
			}
			walkBlock(s.Finally)
		case *ast.MatchStatement:
			for _, matchCase := range s.Cases {
				addIdent(matchCase.Name)
				walkBlock(matchCase.Body)
			}
		case *ast.SelectStatement:
			for _, selectCase := range s.Cases {
				if selectCase.Binding != "" {
					into[selectCase.Binding] = struct{}{}
				}
				walkBlock(selectCase.Body)
			}
			walkBlock(s.Default)
		case *ast.BlockStatement:
			walkBlock(s)
		}
	}
	for _, stmt := range stmts {
		walk(stmt)
	}
}

// isDeclarationName reports whether name binds a class, function, enum, or
// interface declaration (vs a variable). Class/enum/interface declarations
// bind a typeInfo whose name equals the declared name.
func (a *Analyzer) isDeclarationName(name string, info typeInfo) bool {
	if _, ok := a.functions[name]; ok {
		return true
	}
	if _, ok := a.classes[name]; ok {
		return true
	}
	if _, ok := a.interfaces[name]; ok {
		return true
	}
	return info.name == name
}

func (a *Analyzer) markBindingDestroyed(name string) bool {
	for i := len(a.scopes) - 1; i >= 0; i-- {
		if entry, ok := a.scopes[i][name]; ok {
			entry.destroyed = true
			a.scopes[i][name] = entry
			return true
		}
	}
	return false
}

func (a *Analyzer) analyzeAssignment(expr *ast.AssignmentExpression) {
	a.analyzeExpression(expr.Value)
	ident, ok := expr.Left.(*ast.Identifier)
	if !ok {
		return
	}
	a.checkBindingNotDestroyed(ident)
	target, ok := a.lookup(ident.Value)
	if !ok || !target.known {
		return
	}
	actual := a.expressionTypeName(expr.Value)
	if !actual.known {
		return
	}
	if actual.name == "null" {
		if !target.nullable && !target.declaredNullable {
			a.errorf("cannot assign null to %s %s", target.name, ident.Value)
		}
		return
	}
	if target.name != "any" && !isNumericLiteralWidening(target.name, expr.Value) && !a.isAssignable(target, actual) {
		a.errorf("cannot assign %s to %s %s", actual.display(), target.display(), ident.Value)
	}
}

func isNumericLiteralWidening(target string, expr ast.Expression) bool {
	_, ok := expr.(*ast.IntegerLiteral)
	return ok && (target == "decimal" || target == "float")
}

// narrowingsForCondition returns (consequenceNarrowing, alternativeNarrowing).
// consequenceNarrowing applies when the condition is true; alternativeNarrowing when false.
func (a *Analyzer) narrowingsForCondition(expr ast.Expression) (map[string]typeInfo, map[string]typeInfo) {
	if expr == nil {
		return nil, nil
	}
	// Conditions reach recording mode only through here (if/elseif/while);
	// record them so binder uses inside conditions get typed.
	if a.exprTypes != nil {
		a.analyzeExpression(expr)
	}
	infix, ok := expr.(*ast.InfixExpression)
	if !ok {
		return nil, nil
	}
	switch infix.Operator {
	case "&&":
		// Both sides must be true → merge both consequence narrowings.
		consL, _ := a.narrowingsForCondition(infix.Left)
		consR, _ := a.narrowingsForCondition(infix.Right)
		return mergeNarrowings(consL, consR), nil
	case "||":
		// Either side may be false → merge both alternative narrowings.
		_, altL := a.narrowingsForCondition(infix.Left)
		_, altR := a.narrowingsForCondition(infix.Right)
		return nil, mergeNarrowings(altL, altR)
	case "instanceof":
		// if (x instanceof SomeClass) narrows x to SomeClass in the consequence.
		if ident, ok := infix.Left.(*ast.Identifier); ok {
			if typeIdent, ok := infix.Right.(*ast.Identifier); ok {
				narrowed := typeInfo{name: typeIdent.Value, known: true}
				return map[string]typeInfo{ident.Value: narrowed}, nil
			}
		}
		return nil, nil
	case "!=", "==":
		name, ok := nullComparedIdentifier(infix.Left, infix.Right)
		if !ok {
			return nil, nil
		}
		typ, ok := a.lookup(name)
		if !ok || !typ.known || !typ.nullable || typ.name == "null" {
			return nil, nil
		}
		nonNull := typ
		nonNull.nullable = false
		if infix.Operator == "!=" {
			return map[string]typeInfo{name: nonNull}, nil
		}
		return nil, map[string]typeInfo{name: nonNull}
	}
	return nil, nil
}

func mergeNarrowings(a, b map[string]typeInfo) map[string]typeInfo {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make(map[string]typeInfo, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// blockAlwaysExits reports whether every execution path through block ends with
// an unconditional exit (return, throw, break, continue).
func (a *Analyzer) blockAlwaysExits(block *ast.BlockStatement) bool {
	if block == nil || len(block.Statements) == 0 {
		return false
	}
	for _, stmt := range block.Statements {
		if stmtAlwaysExits(stmt) {
			return true
		}
	}
	return false
}

func stmtAlwaysExits(stmt ast.Statement) bool {
	switch s := stmt.(type) {
	case *ast.ReturnStatement:
		return true
	case *ast.SimpleStatement:
		return s.Kind == "throw" || s.Kind == "break" || s.Kind == "continue"
	case *ast.IfStatement:
		// An if-else where both branches always exit also always exits.
		if s.Alternative != nil && len(s.ElseIfs) == 0 {
			return blockAlwaysExitsStatic(s.Consequence) && blockAlwaysExitsStatic(s.Alternative)
		}
		return false
	}
	return false
}

func blockAlwaysExitsStatic(block *ast.BlockStatement) bool {
	if block == nil || len(block.Statements) == 0 {
		return false
	}
	for _, stmt := range block.Statements {
		if stmtAlwaysExits(stmt) {
			return true
		}
	}
	return false
}

func nullComparedIdentifier(left, right ast.Expression) (string, bool) {
	if ident, ok := left.(*ast.Identifier); ok && isNullLiteral(right) {
		return ident.Value, true
	}
	if ident, ok := right.(*ast.Identifier); ok && isNullLiteral(left) {
		return ident.Value, true
	}
	return "", false
}

func isNullLiteral(expr ast.Expression) bool {
	literal, ok := expr.(*ast.Literal)
	return ok && literal.Value == nil
}

func (a *Analyzer) expressionTypeName(expr ast.Expression) typeInfo {
	switch expr := expr.(type) {
	case *ast.IntegerLiteral:
		return typeInfo{name: "int", known: true}
	case *ast.DecimalLiteral:
		return typeInfo{name: "decimal", known: true}
	case *ast.FloatLiteral:
		return typeInfo{name: "float", known: true}
	case *ast.StringLiteral:
		return typeInfo{name: "string", known: true}
	case *ast.ListLiteral:
		return a.collectionLiteralType("list", expr.Elements)
	case *ast.SetLiteral:
		return a.collectionLiteralType("set", expr.Elements)
	case *ast.DictLiteral:
		return a.dictLiteralType(expr)
	case *ast.Identifier:
		if value, ok := a.lookup(expr.Value); ok {
			return value
		}
		return typeInfo{}
	case *ast.CallExpression:
		if ident, ok := expr.Callee.(*ast.Identifier); ok {
			if _, ok := a.classes[ident.Value]; ok {
				info := typeInfo{name: ident.Value, known: true}
				if len(expr.TypeArguments) > 0 {
					info.args = make([]typeInfo, 0, len(expr.TypeArguments))
					for _, arg := range expr.TypeArguments {
						info.args = append(info.args, a.typeInfoFromRef(arg))
					}
				}
				return info
			}
		}
		return typeInfo{}
	case *ast.Literal:
		switch expr.Value.(type) {
		case bool:
			return typeInfo{name: "bool", known: true}
		case nil:
			return typeInfo{name: "null", nullable: true, known: true}
		default:
			return typeInfo{}
		}
	case *ast.SelectorExpression:
		if expr.Name == nil || a.classFieldType == nil {
			return typeInfo{}
		}
		recv := a.expressionTypeName(expr.Object)
		if !recv.known || recv.nullable {
			return typeInfo{}
		}
		ref, ok := a.classFieldType(recv.name, strings.ToLower(expr.Name.Value))
		if !ok {
			return typeInfo{}
		}
		return a.typeInfoFromRef(ref)
	case *ast.InfixExpression:
		return a.infixNumericResult(expr)
	default:
		return typeInfo{}
	}
}

const (
	rankInt     = 1
	rankDecimal = 2
	rankFloat   = 3
)

func numericRank(t typeInfo) int {
	if !t.known || t.nullable {
		return 0
	}
	switch t.name {
	case "int":
		return rankInt
	case "decimal":
		return rankDecimal
	case "float":
		return rankFloat
	}
	return 0
}

// infixNumericResult infers an arithmetic result type so assignability catches e.g. int x = a / b (decimal).
func (a *Analyzer) infixNumericResult(expr *ast.InfixExpression) typeInfo {
	switch expr.Operator {
	case "+", "-", "*", "/", "//", "%":
	default:
		return typeInfo{}
	}
	lk := numericRank(a.expressionTypeName(expr.Left))
	rk := numericRank(a.expressionTypeName(expr.Right))
	if lk == 0 || rk == 0 {
		return typeInfo{}
	}
	// decimal and float never mix at runtime; leave that to the runtime error.
	if (lk == rankDecimal && rk == rankFloat) || (lk == rankFloat && rk == rankDecimal) {
		return typeInfo{}
	}
	name := "int"
	if lk == rankFloat || rk == rankFloat {
		name = "float"
	} else if lk == rankDecimal || rk == rankDecimal || expr.Operator == "/" {
		name = "decimal"
	}
	return typeInfo{name: name, known: true}
}

func (a *Analyzer) isAssignableType(target, actual string) bool {
	if target == actual {
		return true
	}
	if isCallableTypeName(target) {
		return a.typeHasInvoke(actual)
	}
	if isGeneratorTypeName(target) && isGeneratorTypeName(actual) {
		return true
	}
	if _, ok := a.interfaces[target]; ok {
		return a.classImplements(actual, target) || a.interfaceExtends(actual, target) || a.enumImplements(actual, target)
	}
	if _, ok := a.classes[target]; ok {
		return a.classExtends(actual, target)
	}
	return false
}

func isCallableTypeName(name string) bool {
	return strings.EqualFold(name, "callable") || strings.EqualFold(name, "func") || strings.EqualFold(name, "function")
}

func isGeneratorTypeName(name string) bool {
	return strings.EqualFold(name, "generator") || strings.EqualFold(name, "iterable")
}

func (a *Analyzer) typeHasInvoke(name string) bool {
	if isCallableTypeName(name) {
		return true
	}
	return len(a.classMethods(name, "__invoke")) > 0
}

func (a *Analyzer) classExtends(actual, target string) bool {
	for current, ok := a.classes[actual]; ok && current.parent != ""; current, ok = a.classes[current.parent] {
		if current.parent == target {
			return true
		}
	}
	return false
}

func (a *Analyzer) enumImplements(enumName, ifaceName string) bool {
	enum, ok := a.enums[enumName]
	if !ok {
		return false
	}
	for _, implemented := range enum.implements {
		if implemented == ifaceName || a.interfaceExtends(implemented, ifaceName) {
			return true
		}
	}
	return false
}

func (a *Analyzer) classImplements(className, ifaceName string) bool {
	for current, ok := a.classes[className]; ok; current, ok = a.classes[current.parent] {
		for _, implemented := range current.implements {
			if implemented == ifaceName || a.interfaceExtends(implemented, ifaceName) {
				return true
			}
		}
		if current.parent == "" {
			break
		}
	}
	return false
}

func (a *Analyzer) interfaceExtends(actual, target string) bool {
	if actual == target {
		return true
	}
	info, ok := a.interfaces[actual]
	if !ok {
		return false
	}
	for _, parent := range info.parents {
		if parent == target || a.interfaceExtends(parent, target) {
			return true
		}
	}
	return false
}

func (a *Analyzer) interfaceMethods(name string, seen map[string]bool) map[string][]methodInfo {
	if seen[name] {
		return map[string][]methodInfo{}
	}
	seen[name] = true
	info, ok := a.interfaces[name]
	if !ok {
		return map[string][]methodInfo{}
	}
	methods := map[string][]methodInfo{}
	for _, parent := range info.parents {
		for name, overloads := range a.interfaceMethods(parent, seen) {
			methods[name] = append(methods[name], overloads...)
		}
	}
	for name, overloads := range info.methods {
		methods[name] = append(methods[name], overloads...)
	}
	return methods
}

func (a *Analyzer) classMethods(className, methodName string) []methodInfo {
	key := strings.ToLower(methodName)
	for current, ok := a.classes[className]; ok; current, ok = a.classes[current.parent] {
		if methods, ok := current.methods[key]; ok {
			return methods
		}
		if current.parent == "" {
			break
		}
	}
	return nil
}

func (a *Analyzer) classHasCompatibleMethod(className, methodName string, expected methodInfo) bool {
	for _, actual := range a.classMethods(className, methodName) {
		if a.methodCompatible(expected, actual) {
			return true
		}
	}
	return false
}

func (a *Analyzer) methodCompatible(expected, actual methodInfo) bool {
	if len(expected.parameters) != len(actual.parameters) {
		return false
	}
	for i := range expected.parameters {
		if !sameTypeInfo(expected.parameters[i], actual.parameters[i]) {
			return false
		}
	}
	if !expected.returnType.known {
		return true
	}
	if !actual.returnType.known {
		return false
	}
	if expected.returnType.nullable != actual.returnType.nullable {
		return false
	}
	return a.isAssignableType(expected.returnType.name, actual.returnType.name)
}

func (a *Analyzer) methodInfoFromFunction(fn *ast.FunctionStatement) methodInfo {
	info := methodInfo{name: fn.Name.Value, returnType: a.typeInfoFromRef(fn.ReturnType)}
	info.minArgs = len(fn.Parameters)
	for i, param := range fn.Parameters {
		info.parameters = append(info.parameters, a.typeInfoFromRef(param.Type))
		if param.Default != nil && i < info.minArgs {
			info.minArgs = i
		}
	}
	for _, generic := range fn.Generics {
		info.typeParams = append(info.typeParams, typeParam{
			name:       generic.Name.Value,
			constraint: a.typeInfoFromRef(generic.Constraint),
		})
	}
	return info
}

func (a *Analyzer) methodInfoFromSignature(sig *ast.FunctionSignature) methodInfo {
	info := methodInfo{name: sig.Name.Value, returnType: a.typeInfoFromRef(sig.ReturnType)}
	info.minArgs = len(sig.Parameters)
	for i, param := range sig.Parameters {
		info.parameters = append(info.parameters, a.typeInfoFromRef(param.Type))
		if param.Default != nil && i < info.minArgs {
			info.minArgs = i
		}
	}
	for _, generic := range sig.Generics {
		info.typeParams = append(info.typeParams, typeParam{
			name:       generic.Name.Value,
			constraint: a.typeInfoFromRef(generic.Constraint),
		})
	}
	return info
}

func (m methodInfo) signatureKey() string {
	params := make([]string, 0, len(m.parameters))
	for _, param := range m.parameters {
		params = append(params, param.display())
	}
	return fmt.Sprintf("(%s):%s", strings.Join(params, ","), m.returnType.display())
}

func sameTypeInfo(left, right typeInfo) bool {
	if !left.known || !right.known {
		return !left.known && !right.known
	}
	if left.name != right.name || left.nullable != right.nullable {
		return false
	}
	if len(left.args) != len(right.args) {
		return false
	}
	for i := range left.args {
		if !sameTypeInfo(left.args[i], right.args[i]) {
			return false
		}
	}
	return true
}

// isAssignable extends isAssignableType with generic-argument checking. The
// element rule matches the runtime: built-in collections (list/set/dict) are
// COVARIANT in their element types (`list<Dog>` is assignable to `list<Animal>`,
// `list<int>` to `list<any>`), while user generic classes stay INVARIANT (each
// argument must be the exact same type). Covariant collections are technically
// unsound with mutation, but the runtime allows it, so the analyzer matches to
// avoid false positives.
//
// When either side carries no type arguments (a raw instance whose reified
// bindings are still polymorphic), the check passes - argument checking only
// applies when both sides explicitly carry args.
func (a *Analyzer) isAssignable(target, actual typeInfo) bool {
	if !a.isAssignableType(target.name, actual.name) {
		return false
	}
	if len(target.args) == 0 || len(actual.args) == 0 {
		return true
	}
	if len(target.args) != len(actual.args) {
		return false
	}
	covariant := isCollectionTypeName(target.name)
	for i := range target.args {
		if covariant {
			if !a.elementAssignable(target.args[i], actual.args[i]) {
				return false
			}
		} else if !sameTypeInfo(target.args[i], actual.args[i]) {
			return false
		}
	}
	return true
}

func isCollectionTypeName(name string) bool {
	switch name {
	case "list", "set", "dict":
		return true
	}
	return false
}

// elementAssignable is the covariant rule for collection element/value types:
// `any` on either side is permissive, a subtype is assignable to its supertype,
// nested collections recurse covariantly, but two unrelated concrete types
// (including numeric widening like int->float) are rejected. An unresolved type
// parameter stays permissive so generic code does not false-positive.
func (a *Analyzer) elementAssignable(target, actual typeInfo) bool {
	if !target.known || !actual.known {
		return true
	}
	if target.name == "any" || actual.name == "any" {
		return true
	}
	if target.name == actual.name {
		if len(target.args) == 0 || len(actual.args) == 0 {
			return true
		}
		if len(target.args) != len(actual.args) {
			return false
		}
		covariant := isCollectionTypeName(target.name)
		for i := range target.args {
			if covariant {
				if !a.elementAssignable(target.args[i], actual.args[i]) {
					return false
				}
			} else if !sameTypeInfo(target.args[i], actual.args[i]) {
				return false
			}
		}
		return true
	}
	if a.isAssignableType(target.name, actual.name) {
		return true
	}
	if a.isConcreteTypeName(target.name) && a.isConcreteTypeName(actual.name) {
		return false
	}
	return true
}

func (a *Analyzer) isConcreteTypeName(name string) bool {
	switch name {
	case "int", "float", "decimal", "string", "bool", "bytes", "list", "set", "dict":
		return true
	}
	if _, ok := a.classes[name]; ok {
		return true
	}
	if _, ok := a.interfaces[name]; ok {
		return true
	}
	return false
}

// collectionLiteralType infers list<T>/set<T> when every element shares one
// known type; otherwise the bare collection type so element checks stay lenient.
func (a *Analyzer) collectionLiteralType(name string, elements []ast.Expression) typeInfo {
	elem, ok := a.homogeneousElementType(elements)
	if !ok {
		return typeInfo{name: name, known: true}
	}
	return typeInfo{name: name, known: true, args: []typeInfo{elem}}
}

func (a *Analyzer) dictLiteralType(lit *ast.DictLiteral) typeInfo {
	if len(lit.Entries) == 0 {
		return typeInfo{name: "dict", known: true}
	}
	keys := make([]ast.Expression, 0, len(lit.Entries))
	vals := make([]ast.Expression, 0, len(lit.Entries))
	for _, entry := range lit.Entries {
		if entry.Spread {
			return typeInfo{name: "dict", known: true}
		}
		keys = append(keys, entry.Key)
		vals = append(vals, entry.Value)
	}
	k, kok := a.homogeneousElementType(keys)
	v, vok := a.homogeneousElementType(vals)
	if !kok || !vok {
		return typeInfo{name: "dict", known: true}
	}
	return typeInfo{name: "dict", known: true, args: []typeInfo{k, v}}
}

// homogeneousElementType returns the common element type when every element is
// known and shares the exact same type; otherwise (empty, mixed, or any
// unknown) it reports not-ok so downstream element checks stay lenient.
func (a *Analyzer) homogeneousElementType(elements []ast.Expression) (typeInfo, bool) {
	if len(elements) == 0 {
		return typeInfo{}, false
	}
	var common typeInfo
	for i, el := range elements {
		t := a.expressionTypeName(el)
		if !t.known {
			return typeInfo{}, false
		}
		if i == 0 {
			common = t
			continue
		}
		if !sameTypeInfo(common, t) {
			return typeInfo{}, false
		}
	}
	return common, true
}

// parameterBindingType is the type a parameter NAME binds to inside the
// function body. A variadic `T ...rest` collects into a list<T>.
func (a *Analyzer) parameterBindingType(param ast.Parameter) typeInfo {
	t := a.typeInfoFromRef(param.Type)
	if param.Variadic {
		return typeInfo{name: "list", known: true, args: []typeInfo{t}}
	}
	return t
}

func (a *Analyzer) typeInfoFromRef(ref *ast.TypeRef) typeInfo {
	if ref == nil || ref.Operator != "" {
		return typeInfo{}
	}
	var args []typeInfo
	if len(ref.Arguments) > 0 {
		args = make([]typeInfo, 0, len(ref.Arguments))
		for _, arg := range ref.Arguments {
			args = append(args, a.typeInfoFromRef(arg))
		}
	}
	if ref.ListAlias {
		// `T[]` is shorthand for `list<T>` - record the element type so element
		// checks downstream behave identically to `list<T>`.
		var elemArgs []typeInfo
		if args == nil {
			elemArgs = []typeInfo{{name: ref.Name, nullable: ref.Nullable, known: true}}
		} else {
			elemArgs = args
		}
		return typeInfo{name: "list", nullable: ref.Nullable, known: true, args: elemArgs}
	}
	if alias, ok := a.aliases[strings.ToLower(ref.Name)]; ok {
		alias.nullable = alias.nullable || ref.Nullable
		return alias
	}
	return typeInfo{name: ref.Name, nullable: ref.Nullable, known: true, args: args}
}

func (t typeInfo) display() string {
	if !t.known {
		return "unknown"
	}
	name := t.name
	if len(t.args) > 0 {
		parts := make([]string, 0, len(t.args))
		for _, arg := range t.args {
			parts = append(parts, arg.display())
		}
		name = name + "<" + strings.Join(parts, ", ") + ">"
	}
	if t.nullable && t.name != "null" {
		return "?" + name
	}
	return name
}

func (a *Analyzer) pushScope() {
	a.scopes = append(a.scopes, map[string]typeInfo{})
}

func (a *Analyzer) popScope() {
	if len(a.scopes) > 1 {
		a.scopes = a.scopes[:len(a.scopes)-1]
	}
}

func (a *Analyzer) declare(name string, typ typeInfo) {
	if len(a.scopes) == 0 {
		a.scopes = []map[string]typeInfo{{}}
	}
	a.scopes[len(a.scopes)-1][name] = typ
}

func (a *Analyzer) lookup(name string) (typeInfo, bool) {
	for i := len(a.scopes) - 1; i >= 0; i-- {
		if value, ok := a.scopes[i][name]; ok {
			return value, true
		}
	}
	return typeInfo{}, false
}

// errorf records a semantic diagnostic without a known source position.
// Prefer errorAt when a token is in scope - the LSP layer reports
// position-less diagnostics at (1, 1) as a fallback, which is rarely
// where the actual problem is.
func (a *Analyzer) errorf(format string, args ...any) {
	a.diagnostics = append(a.diagnostics, Diagnostic{Message: fmt.Sprintf(format, args...)})
}

// errorAt records a semantic diagnostic anchored at the position of
// the given token. A zero token (Line == 0) falls back to the
// position-less behaviour of errorf. Defaults to SeverityError.
func (a *Analyzer) errorAt(tok token.Token, format string, args ...any) {
	a.diagnostics = append(a.diagnostics, Diagnostic{
		Message:  fmt.Sprintf(format, args...),
		Line:     tok.Line,
		Column:   tok.Column,
		Severity: SeverityError,
	})
}

// warningAt is the Warning counterpart of errorAt. Warnings print to
// stderr but do not block execution in `geblang run`; they show up
// as severity=2 in the LSP and `geblang check` output. Use this for
// checks where the runtime can sometimes prove the analyzer wrong,
// or for stylistic findings that shouldn't fail a build.
func (a *Analyzer) warningAt(tok token.Token, format string, args ...any) {
	a.diagnostics = append(a.diagnostics, Diagnostic{
		Message:  fmt.Sprintf(format, args...),
		Line:     tok.Line,
		Column:   tok.Column,
		Severity: SeverityWarning,
	})
}

// ruleWarningAt is warningAt with an explicit rule category (e.g. "deprecated").
func (a *Analyzer) ruleWarningAt(tok token.Token, rule, format string, args ...any) {
	a.diagnostics = append(a.diagnostics, Diagnostic{
		Message:  fmt.Sprintf(format, args...),
		Line:     tok.Line,
		Column:   tok.Column,
		Severity: SeverityWarning,
		Rule:     rule,
	})
}

// checkMatchExhaustiveness warns on an enum match missing a variant (guarded cases never cover; unknowns suppress).
func (a *Analyzer) checkMatchExhaustiveness(subject ast.Expression, cases []ast.MatchCase, tok token.Token) {
	if subject == nil {
		return
	}
	st := a.expressionTypeName(subject)
	if !st.known {
		return
	}
	info, ok := a.enums[st.name]
	if !ok || len(info.variants) == 0 {
		return
	}
	covered := map[string]bool{}
	variantSet := map[string]bool{}
	for _, v := range info.variants {
		variantSet[v] = true
	}
	for _, mc := range cases {
		if mc.Guard != nil {
			continue
		}
		if mc.Default {
			return
		}
		// No-payload variants parse as type patterns (Enum.Variant, or a union TypeRef for `A | B`).
		if mc.Type != nil {
			if collectTypeVariants(mc.Type, st.name, variantSet, covered) {
				return
			}
			continue
		}
		if mc.EnumVariant != nil && mc.EnumVariant.Variant != nil {
			covered[mc.EnumVariant.Variant.Value] = true
			continue
		}
		markCoveredVariant(mc.Pattern, st.name, variantSet, covered)
		for _, alt := range mc.Alternates {
			markCoveredVariant(alt, st.name, variantSet, covered)
		}
	}
	var missing []string
	for _, v := range info.variants {
		if !covered[v] {
			missing = append(missing, v)
		}
	}
	if len(missing) == 0 {
		return
	}
	a.ruleWarningAt(tok, "match-nonexhaustive",
		"match on enum '%s' is not exhaustive: missing %s (add the missing case(s) or a 'default:' case)",
		st.name, strings.Join(missing, ", "))
}

// markCoveredVariant records the variant a selector or (qualified) identifier names.
func markCoveredVariant(expr ast.Expression, enumName string, variantSet, covered map[string]bool) {
	switch e := expr.(type) {
	case *ast.SelectorExpression:
		if e.Name != nil && variantSet[e.Name.Value] {
			covered[e.Name.Value] = true
		}
	case *ast.Identifier:
		if v, ok := variantFromQualified(e.Value, enumName, variantSet); ok {
			covered[v] = true
		}
	}
}

// collectTypeVariants records variants from a (possibly union) type pattern,
// returning true when it is a catch-all over the whole enum (bare enum or `any`).
func collectTypeVariants(tr *ast.TypeRef, enumName string, variantSet, covered map[string]bool) bool {
	if tr == nil {
		return false
	}
	if tr.Operator == "|" || tr.Operator == "&" {
		left := collectTypeVariants(tr.Left, enumName, variantSet, covered)
		right := collectTypeVariants(tr.Right, enumName, variantSet, covered)
		return left || right
	}
	if !strings.Contains(tr.Name, ".") {
		return tr.Name == enumName || strings.EqualFold(tr.Name, "any")
	}
	if v, ok := variantFromQualified(tr.Name, enumName, variantSet); ok {
		covered[v] = true
	}
	return false
}

// variantFromQualified resolves Enum.Variant or a bare variant, requiring the prefix to match.
func variantFromQualified(name, enumName string, variantSet map[string]bool) (string, bool) {
	if enum, variant, ok := strings.Cut(name, "."); ok {
		if enum == enumName && variantSet[variant] {
			return variant, true
		}
		return "", false
	}
	if variantSet[name] {
		return name, true
	}
	return "", false
}

// tokenOfExpression returns the leading token of an AST expression,
// or a zero Token if the type isn't recognised. The Expression
// interface intentionally doesn't expose a Token() method; this
// helper centralises the type switch used for diagnostic anchoring.
func tokenOfExpression(expr ast.Expression) token.Token {
	switch e := expr.(type) {
	case *ast.Identifier:
		return e.Token
	case *ast.IntegerLiteral:
		return e.Token
	case *ast.FloatLiteral:
		return e.Token
	case *ast.DecimalLiteral:
		return e.Token
	case *ast.StringLiteral:
		return e.Token
	case *ast.PrefixExpression:
		return e.Token
	case *ast.InfixExpression:
		return e.Token
	case *ast.PostfixExpression:
		return e.Token
	case *ast.AssignmentExpression:
		return e.Token
	case *ast.CallExpression:
		return tokenOfExpression(e.Callee)
	case *ast.SelectorExpression:
		return tokenOfExpression(e.Object)
	case *ast.IndexExpression:
		return tokenOfExpression(e.Left)
	case *ast.ListLiteral:
		return e.Token
	case *ast.DictLiteral:
		return e.Token
	case *ast.SetLiteral:
		return e.Token
	case *ast.FunctionLiteral:
		return e.Token
	case *ast.CastExpression:
		return e.Token
	case *ast.TernaryExpression:
		return e.Token
	}
	return token.Token{}
}
