package lower

import (
	"fmt"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/token"
	"geblang/internal/transpiler/emit"
	"geblang/internal/transpiler/types"
)

type Lowerer struct {
	Module     *Module
	Bridge     *NativeBridge
	SourceFile string
	NamePrefix string
	IsEntry    bool
	// Canonical is this module's own canonical name, so a self-import
	// (`import profiler as native` inside profiler) routes to the native bridge.
	Canonical string

	scope       *types.Scope
	errors      []Error
	w           *emit.Writer
	parentClass string
	// moduleTopLevel holds the Geblang names of this module's top-level functions
	// and module-level let/const; a same-module reference to one prefixes it with
	// NamePrefix so it binds to the prefixed Go symbol (non-entry modules only).
	moduleTopLevel map[string]bool
	// entryHoist names entry-module top-level let/const referenced by a sibling
	// function; these lower to package-level vars instead of main() locals so the
	// functions can see them (entry modules only).
	entryHoist    map[string]bool
	inConstructor bool
	inGenerator   bool
	typeParams    map[string]struct{}
	exprTypes     map[ast.Expression]*types.Type
	expectedType  *types.Type
	// refinedDecls types an untyped empty list/dict declaration from later
	// push/index assignments in the same block.
	refinedDecls map[*ast.DeclarationStatement]*types.Type

	// currentReturnGo is the enclosing function's Go return type ("" when void);
	// a try with `return` inside routes the value through a signal of this type.
	currentReturnGo string
	// currentReturnType wraps a concrete return value into a nullable slot.
	currentReturnType *types.Type
	tryDepth          int
	loopDepth         int
	tmpSeq            int

	// currentClassIface/Gb name the hierarchy class whose virtual method is being
	// lowered; this.m() routes through this.self for late binding.
	currentClassIface string
	currentClassGb    string

	// tryCtl, when non-nil, redirects return/break/continue inside a try-region
	// closure into signal returns so they keep their enclosing-scope meaning.
	tryCtl *tryControl
}

// tryControl tracks redirection inside a try-region closure; the suspended
// flags turn it off for a nested loop (break/continue) or func literal (return).
type tryControl struct {
	retGo         string
	retSuspended  bool
	loopSuspended bool
}

// errorBindingType marks a catch variable bound to a *transpilert.Error so
// `.message` lowers to the exported `.Message` field.
var errorBindingType = &types.Type{Kind: types.KindClass, Name: "__gbError"}

// withExpectedType supplies a declared-target type so empty composite literals
// adopt the annotation's element type instead of defaulting to any.
func (l *Lowerer) withExpectedType(t *types.Type, fn func()) {
	saved := l.expectedType
	l.expectedType = t
	fn()
	l.expectedType = saved
}

// SetExprTypes supplies semantic-resolved expression types as the primary
// type source for inference; nil entries mean unknown.
func (l *Lowerer) SetExprTypes(m map[ast.Expression]*types.Type) { l.exprTypes = m }

// SetCanonical records this module's own canonical name.
func (l *Lowerer) SetCanonical(canonical string) { l.Canonical = canonical }

func NewLowerer(mod *Module, bridge *NativeBridge, sourceFile string) *Lowerer {
	return &Lowerer{
		Module:     mod,
		Bridge:     bridge,
		SourceFile: sourceFile,
		IsEntry:    true,
		scope:      types.NewScope(),
		w:          mod.MainBody(),
	}
}

func NewModuleLowerer(mod *Module, bridge *NativeBridge, sourceFile, namePrefix string) *Lowerer {
	return &Lowerer{
		Module:     mod,
		Bridge:     bridge,
		SourceFile: sourceFile,
		NamePrefix: namePrefix,
		IsEntry:    false,
		scope:      types.NewScope(),
		w:          mod.TopDecls(),
	}
}

func (l *Lowerer) Errors() []Error { return l.errors }

// EntryMainGoName is the renamed Go symbol for the entry module's exported
// main; the generated package-level func main() trampolines to it.
const EntryMainGoName = "gbEntryMain"

// emittedFuncName is the Go symbol for a top-level function. The entry module's
// main is renamed so it does not collide with the generated package main().
func (l *Lowerer) emittedFuncName(gbName string) string {
	if l.IsEntry && l.NamePrefix == "" && gbName == "main" {
		l.Module.SetHasEntryMain(true)
		return EntryMainGoName
	}
	return l.NamePrefix + emit.MangleIdent(gbName)
}

// qualifiedTopLevelName returns the Go symbol for a bare reference to a name. A
// non-entry module prefixes its own top-level functions and module-level vars
// with NamePrefix (matching their definitions); a local/param binding of the
// same name, the entry module, and non-top-level names are left unprefixed.
func (l *Lowerer) qualifiedTopLevelName(name string) string {
	mangled := emit.MangleIdent(name)
	if l.IsEntry || l.NamePrefix == "" || !l.moduleTopLevel[name] {
		return mangled
	}
	if _, bound := l.scope.Lookup(name); bound {
		return mangled
	}
	return l.NamePrefix + mangled
}

func (l *Lowerer) LowerProgram(prog *ast.Program) {
	l.preregisterTopLevel(prog.Statements)
	l.computeEntryHoist(prog.Statements)
	l.recordEmptyCollectionRefinements(prog.Statements)
	for _, stmt := range prog.Statements {
		l.lowerTopLevelStatement(stmt)
	}
}

// computeEntryHoist marks entry-module top-level let/const that a sibling function
// reads, plus the transitive closure of module-level bindings their initializers
// reference. Those bindings become package vars so the package-level functions can
// see them. A hoisted binding whose initializer is not a constant expression would
// reorder relative to top-level code, so it diagnoses rather than miscompile.
func (l *Lowerer) computeEntryHoist(stmts []ast.Statement) {
	l.entryHoist = map[string]bool{}
	if !l.IsEntry {
		return
	}
	lets := map[string]*ast.DeclarationStatement{}
	var funcs []*ast.FunctionStatement
	for _, stmt := range stmts {
		s := stmt
		if exp, ok := s.(*ast.ExportStatement); ok {
			s = exp.Statement
		}
		switch n := s.(type) {
		case *ast.DeclarationStatement:
			lets[n.Name.Value] = n
		case *ast.FunctionStatement:
			funcs = append(funcs, n)
		}
	}

	var queue []string
	for name := range lets {
		for _, fn := range funcs {
			if functionReadsFree(fn, name) {
				l.entryHoist[name] = true
				queue = append(queue, name)
				break
			}
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		decl := lets[cur]
		if decl.Value == nil {
			continue
		}
		for other := range lets {
			if l.entryHoist[other] || other == cur {
				continue
			}
			if referencesFree(decl.Value, other) {
				l.entryHoist[other] = true
				queue = append(queue, other)
			}
		}
	}

	for name := range l.entryHoist {
		decl := lets[name]
		if decl.Value != nil && !isHoistableInitializer(decl.Value) {
			l.errAt(decl.Token.Line, decl.Token.Column,
				fmt.Sprintf("module-level %q is read by a function but its initializer is not a constant expression", name),
				"native compilation hoists function-referenced module-level bindings to package scope; give it a side-effect-free initializer")
		}
	}
}

// isHoistableInitializer reports whether a module-level binding's initializer is a
// constant expression safe to evaluate at package-init time: literals, collection
// literals of such, references to other module-level bindings, and operators over
// them. Calls, selectors, and indexing may have side effects or read local state.
func isHoistableInitializer(expr ast.Expression) bool {
	switch e := expr.(type) {
	case nil:
		return true
	case *ast.Literal, *ast.IntegerLiteral, *ast.FloatLiteral, *ast.DecimalLiteral, *ast.StringLiteral, *ast.EmbeddedLiteral, *ast.Identifier:
		return true
	case *ast.ListLiteral:
		for _, el := range e.Elements {
			if !isHoistableInitializer(el) {
				return false
			}
		}
		return true
	case *ast.SetLiteral:
		for _, el := range e.Elements {
			if !isHoistableInitializer(el) {
				return false
			}
		}
		return true
	case *ast.DictLiteral:
		for _, entry := range e.Entries {
			if !isHoistableInitializer(entry.Key) || !isHoistableInitializer(entry.Value) {
				return false
			}
		}
		return true
	case *ast.InfixExpression:
		return isHoistableInitializer(e.Left) && isHoistableInitializer(e.Right)
	case *ast.PrefixExpression:
		return isHoistableInitializer(e.Right)
	case *ast.TernaryExpression:
		return isHoistableInitializer(e.Condition) && isHoistableInitializer(e.ThenExpr) && isHoistableInitializer(e.ElseExpr)
	case *ast.InterpolatedString:
		for _, p := range e.Parts {
			if !isHoistableInitializer(p) {
				return false
			}
		}
		return true
	case *ast.CastExpression:
		return isHoistableInitializer(e.Value)
	}
	return false
}

// PreregisterClasses records class names, parents, and decls so cross-module
// inheritance resolves regardless of per-module lowering order.
func (l *Lowerer) PreregisterClasses(prog *ast.Program) {
	if prog == nil {
		return
	}
	for _, stmt := range prog.Statements {
		s := stmt
		if exp, ok := s.(*ast.ExportStatement); ok {
			s = exp.Statement
		}
		if cls, ok := s.(*ast.ClassStatement); ok {
			l.Module.RegisterClass(cls.Name.Value)
			l.Module.RegisterClassDecl(cls.Name.Value, cls)
			if cls.Extends != nil {
				l.Module.RegisterClassParent(cls.Name.Value, cls.Extends.Name)
			}
		}
		if alias, ok := s.(*ast.TypeAliasStatement); ok {
			l.Module.RegisterTypeAlias(alias.Name.Value, alias.Type)
		}
	}
}

// PreregisterModuleReturns records each exported function's return type keyed by
// the module's Go symbol prefix, so an entry lowered before this module can
// infer the result type of a cross-module `module.fn()` call.
func (l *Lowerer) PreregisterModuleReturns(prog *ast.Program, prefix string) {
	if prog == nil {
		return
	}
	for _, stmt := range prog.Statements {
		s := stmt
		if exp, ok := s.(*ast.ExportStatement); ok {
			s = exp.Statement
		}
		fn, ok := s.(*ast.FunctionStatement)
		if !ok || fn.Name == nil {
			continue
		}
		ret := l.resolveTypeRef(fn.ReturnType)
		if fn.Async && ret != nil {
			ret = &types.Type{Kind: types.KindTask, Elem: ret}
		}
		l.Module.RegisterUserModuleReturn(prefix+emit.MangleIdent(fn.Name.Value), ret)
	}
}

func (l *Lowerer) preregisterTopLevel(stmts []ast.Statement) {
	if l.moduleTopLevel == nil {
		l.moduleTopLevel = map[string]bool{}
	}
	for _, stmt := range stmts {
		s := stmt
		if exp, ok := s.(*ast.ExportStatement); ok {
			s = exp.Statement
		}
		if fn, ok := s.(*ast.FunctionStatement); ok {
			ret := l.resolveTypeRef(fn.ReturnType)
			if fn.Async && ret != nil {
				ret = &types.Type{Kind: types.KindTask, Elem: ret}
			}
			l.Module.RegisterFunctionReturnType(emit.MangleIdent(fn.Name.Value), ret)
			key := l.NamePrefix + emit.MangleIdent(fn.Name.Value)
			l.Module.RegisterCalleeParams(key, paramNames(fn.Parameters))
			l.Module.RegisterCalleeSignature(key, paramDefaults(fn.Parameters), lastVariadic(fn.Parameters))
			l.moduleTopLevel[fn.Name.Value] = true
		}
		if decl, ok := s.(*ast.DeclarationStatement); ok {
			l.moduleTopLevel[decl.Name.Value] = true
		}
		if cls, ok := s.(*ast.ClassStatement); ok {
			l.Module.RegisterClass(cls.Name.Value)
			l.Module.RegisterClassDecl(cls.Name.Value, cls)
			if cls.Extends != nil {
				l.Module.RegisterClassParent(cls.Name.Value, cls.Extends.Name)
			}
		}
		if alias, ok := s.(*ast.TypeAliasStatement); ok {
			l.Module.RegisterTypeAlias(alias.Name.Value, alias.Type)
		}
		if iface, ok := s.(*ast.InterfaceStatement); ok {
			l.Module.RegisterInterface(iface.Name.Value)
			l.Module.RegisterInterfaceDecl(iface.Name.Value, iface)
		}
	}
}

func (l *Lowerer) lowerTopLevelStatement(stmt ast.Statement) {
	switch s := stmt.(type) {
	case *ast.ModuleStatement:
		return
	case *ast.ImportStatement:
		l.lowerImport(s)
	case *ast.FromImportStatement:
		l.lowerFromImport(s)
	case *ast.ExportStatement:
		l.lowerTopLevelStatement(s.Statement)
	case *ast.FunctionStatement:
		l.lowerTopLevelFunction(s)
	case *ast.ClassStatement:
		l.lowerClass(s)
	case *ast.EnumStatement:
		if s.BackingType != nil {
			l.errAt(s.Token.Line, s.Token.Column,
				fmt.Sprintf("geblang build --native does not support backed enum %s yet", s.Name.Value),
				"use the default bytecode build path for backed enums")
			return
		}
		l.lowerEnum(s)
	case *ast.InterfaceStatement:
		l.lowerInterface(s)
	case *ast.TypeAliasStatement:
		l.Module.RegisterTypeAlias(s.Name.Value, s.Type) // resolve-at-use; no Go decl
	case *ast.DeclarationStatement:
		if !l.IsEntry {
			l.lowerModuleVar(s)
			return
		}
		if l.entryHoist[s.Name.Value] {
			saved := l.w
			l.w = l.Module.TopDecls()
			l.lowerModuleVar(s)
			l.w = saved
			return
		}
		l.lowerStatement(stmt)
	default:
		if !l.IsEntry {
			l.errAt(0, 0, fmt.Sprintf("non-function top-level statement %T in non-entry module", stmt),
				"non-entry modules may only declare functions (and other top-level decls) in Phase 1")
			return
		}
		l.lowerStatement(stmt)
	}
}

func (l *Lowerer) lowerStatement(stmt ast.Statement) {
	switch s := stmt.(type) {
	case *ast.ExpressionStatement:
		l.lowerExpressionStmt(s)
	case *ast.DeclarationStatement:
		l.lowerDeclaration(s)
	case *ast.DestructuringStatement:
		l.lowerDestructuring(s)
	case *ast.IfStatement:
		l.lowerIf(s)
	case *ast.BlockStatement:
		l.lowerBlock(s.Statements)
	case *ast.ForStatement:
		l.lowerFor(s)
	case *ast.WhileStatement:
		l.lowerWhile(s)
	case *ast.ReturnStatement:
		l.lowerReturn(s)
	case *ast.SimpleStatement:
		l.lowerSimple(s)
	case *ast.MatchStatement:
		l.lowerMatchStatement(s)
	case *ast.TryStatement:
		l.lowerTry(s)
	case *ast.YieldStatement:
		l.lowerYield(s)
	case *ast.WithStatement:
		l.lowerWith(s)
	case *ast.InitStatement:
		l.withChildScope(func() { l.lowerBlock(s.Body.Statements) })
	case *ast.FromImportStatement:
		l.lowerFromImport(s)
	case *ast.DelStatement:
		l.errAt(s.Token.Line, s.Token.Column,
			"the transpiler does not yet support del",
			"del fires a destructor and removes the binding; destructors and scope removal need runtime support deferred to a later phase")
	case *ast.SelectStatement:
		l.errAt(s.Token.Line, s.Token.Column,
			"the transpiler does not yet support select",
			"channels are runtime objects not yet transpiled")
	default:
		l.errAt(0, 0, fmt.Sprintf("unsupported statement: %T", stmt),
			"this statement form is not yet implemented in the transpiler")
	}
}

func (l *Lowerer) withReturnGo(goType string, fn func()) {
	saved := l.currentReturnGo
	l.currentReturnGo = goType
	fn()
	l.currentReturnGo = saved
}

func (l *Lowerer) withReturnType(t *types.Type, fn func()) {
	saved := l.currentReturnType
	l.currentReturnType = t
	fn()
	l.currentReturnType = saved
}

// withLoopBody marks a nested loop body so break/continue bind to that loop
// rather than signalling out of an enclosing try.
func (l *Lowerer) withLoopBody(fn func()) {
	l.loopDepth++
	if l.tryCtl == nil {
		fn()
		l.loopDepth--
		return
	}
	saved := l.tryCtl.loopSuspended
	l.tryCtl.loopSuspended = true
	fn()
	l.tryCtl.loopSuspended = saved
	l.loopDepth--
}

// withNestedFunc suspends all control-flow redirection while lowering a nested
// function literal, whose return/break/continue are local to it.
func (l *Lowerer) withNestedFunc(fn func()) {
	savedLoop := l.loopDepth
	l.loopDepth = 0
	defer func() { l.loopDepth = savedLoop }()
	if l.tryCtl == nil {
		fn()
		return
	}
	savedR, savedL := l.tryCtl.retSuspended, l.tryCtl.loopSuspended
	l.tryCtl.retSuspended, l.tryCtl.loopSuspended = true, true
	fn()
	l.tryCtl.retSuspended, l.tryCtl.loopSuspended = savedR, savedL
}

// zeroValue is the Go zero literal for goType, used to fill the unused return
// slot when a try-region closure signals break/continue rather than returning.
func zeroValue(goType string) string {
	switch goType {
	case "", "any", "error":
		return "nil"
	case "int64", "int", "rune", "byte":
		return "0"
	case "float64", "float32":
		return "0"
	case "bool":
		return "false"
	case "string":
		return `""`
	}
	if strings.HasPrefix(goType, "*") || strings.HasPrefix(goType, "[]") ||
		strings.HasPrefix(goType, "map[") || strings.HasPrefix(goType, "func") ||
		strings.HasPrefix(goType, "chan") || strings.HasPrefix(goType, "interface") {
		return "nil"
	}
	return "*new(" + goType + ")"
}

func tryZeroExpr(goType string) string { return zeroValue(goType) }

// nextTmp returns a fresh Go temp name so repeated lowerings in one block do
// not redeclare the same `:=` variable.
func (l *Lowerer) nextTmp() string {
	l.tmpSeq++
	return fmt.Sprintf("__dt%d", l.tmpSeq)
}

func (l *Lowerer) withChildScope(fn func()) {
	saved := l.scope
	l.scope = l.scope.Child()
	fn()
	l.scope = saved
}

func (l *Lowerer) lowerExpression(expr ast.Expression) {
	switch e := expr.(type) {
	case *ast.StringLiteral:
		l.emitStringLiteral(e)
	case *ast.EmbeddedLiteral:
		l.lowerEmbeddedLiteral(e)
	case *ast.IntegerLiteral:
		l.emitIntegerLiteral(e)
	case *ast.FloatLiteral:
		l.emitFloatLiteral(e)
	case *ast.DecimalLiteral:
		l.emitDecimalLiteral(e)
	case *ast.Literal:
		l.lowerKeywordLiteral(e)
	case *ast.InterpolatedString:
		l.lowerInterpolated(e)
	case *ast.Identifier:
		if e.Value == "parent" && l.parentClass != "" {
			l.w.WriteString("this.")
			if l.currentClassIface != "" {
				l.w.WriteString(implName(l.parentClass))
			} else {
				l.w.WriteString(l.parentClass)
			}
			return
		}
		l.w.WriteString(l.qualifiedTopLevelName(e.Value))
	case *ast.CallExpression:
		l.lowerCall(e)
	case *ast.SelectorExpression:
		l.lowerSelector(e)
	case *ast.InfixExpression:
		l.lowerInfix(e)
	case *ast.PostfixExpression:
		l.lowerPostfix(e)
	case *ast.PrefixExpression:
		l.lowerPrefix(e)
	case *ast.AssignmentExpression:
		l.lowerAssignment(e)
	case *ast.IndexExpression:
		l.lowerIndex(e)
	case *ast.CastExpression:
		l.lowerCast(e)
	case *ast.MatchExpression:
		l.lowerMatchExpression(e)
	case *ast.TernaryExpression:
		l.lowerTernary(e)
	case *ast.FunctionLiteral:
		l.lowerFunctionLiteral(e)
	case *ast.ListLiteral:
		l.lowerList(e)
	case *ast.DictLiteral:
		l.lowerDict(e)
	case *ast.AwaitExpression:
		l.lowerAwait(e)
	case *ast.PipeExpression:
		l.lowerPipe(e)
	case *ast.PartialExpression:
		l.lowerPartial(e)
	case *ast.RangeExpression:
		l.lowerRangeValue(e)
	case *ast.ListComprehension:
		l.lowerListComprehension(e)
	case *ast.DictComprehension:
		l.lowerDictComprehension(e)
	case *ast.SetLiteral:
		l.errAt(e.Token.Line, e.Token.Column,
			"the transpiler does not yet support set literals",
			"sets are sorted and need a dedicated runtime type deferred to a later phase")
		l.w.WriteString("nil")
	case *ast.SetComprehension:
		l.errAt(e.Token.Line, e.Token.Column,
			"the transpiler does not yet support set comprehensions",
			"sets need a sorted-set runtime type deferred to a later phase")
		l.w.WriteString("nil")
	default:
		tok := exprToken(expr)
		l.errAt(tok.Line, tok.Column, fmt.Sprintf("unsupported expression: %T", expr),
			"this expression form is not yet implemented in the transpiler")
		l.w.WriteString("nil")
	}
}

func (l *Lowerer) resolveTypeRef(t *ast.TypeRef) *types.Type {
	t = l.expandTypeAlias(t)
	ty := types.FromAST(t)
	l.fixupTypeKinds(ty)
	return ty
}

// expandTypeAlias substitutes a `type X = T` name with its target so the alias
// has no runtime presence, matching the evaluator's resolve-at-use behaviour.
func (l *Lowerer) expandTypeAlias(t *ast.TypeRef) *ast.TypeRef {
	for i := 0; i < 100 && t != nil && t.Operator == "" && len(t.Arguments) == 0 && !t.ListAlias; i++ {
		target, ok := l.Module.TypeAlias(t.Name)
		if !ok {
			break
		}
		clone := *target
		clone.Nullable = clone.Nullable || t.Nullable
		t = &clone
	}
	return t
}

func (l *Lowerer) fixupTypeKinds(ty *types.Type) {
	if ty == nil {
		return
	}
	if ty.Kind == types.KindClass {
		if _, ok := l.typeParams[ty.Name]; ok {
			ty.Kind = types.KindTypeParam
		} else if l.Module.IsEnum(ty.Name) {
			ty.Kind = types.KindEnum
			ty.EnumScalar = l.Module.IsScalarEnum(ty.Name)
		} else if l.Module.IsInterface(ty.Name) {
			ty.Kind = types.KindInterface
		} else if l.Module.InClassHierarchy(ty.Name) {
			// Hierarchy classes are represented by their Go interface so a
			// subclass instance assigns into a base-typed slot and dispatches.
			ty.Kind = types.KindInterface
		}
	}
	l.fixupTypeKinds(ty.Elem)
	l.fixupTypeKinds(ty.Key)
	l.fixupTypeKinds(ty.Value)
	l.fixupTypeKinds(ty.Result)
	for _, p := range ty.Params {
		l.fixupTypeKinds(p)
	}
}

func (l *Lowerer) withTypeParams(generics []*ast.TypeParam, fn func()) {
	if len(generics) == 0 {
		fn()
		return
	}
	if l.typeParams == nil {
		l.typeParams = map[string]struct{}{}
	}
	added := make([]string, 0, len(generics))
	for _, g := range generics {
		if _, exists := l.typeParams[g.Name.Value]; exists {
			continue
		}
		l.typeParams[g.Name.Value] = struct{}{}
		added = append(added, g.Name.Value)
	}
	fn()
	for _, n := range added {
		delete(l.typeParams, n)
	}
}

func (l *Lowerer) inferExpressionType(expr ast.Expression) *types.Type {
	if l.exprTypes != nil {
		if t, ok := l.exprTypes[expr]; ok && t != nil && t.Kind != types.KindUnknown {
			// Semantic types arrive as raw FromAST (every named type is KindClass);
			// refine to KindEnum/KindInterface and set EnumScalar so nullable-enum
			// decisions match the scope-binding types.
			l.fixupTypeKinds(t)
			// A scope binding with a resolved element type wins over a semantic
			// collection type left element-unresolved (refined empty literals).
			if id, ok := expr.(*ast.Identifier); ok && collectionElemUnresolved(t) {
				if b, ok := l.scope.Lookup(id.Value); ok && b.Type != nil && !collectionElemUnresolved(b.Type) {
					return l.hierarchyAsInterface(b.Type)
				}
			}
			return l.hierarchyAsInterface(t)
		}
	}
	switch e := expr.(type) {
	case *ast.Literal:
		if _, ok := e.Value.(bool); ok {
			return &types.Type{Kind: types.KindBool}
		}
	case *ast.InfixExpression:
		switch e.Operator {
		case "==", "!=", "<", ">", "<=", ">=", "&&", "||", "instanceof":
			return &types.Type{Kind: types.KindBool}
		case "+":
			left := l.inferExpressionType(e.Left)
			right := l.inferExpressionType(e.Right)
			if (left != nil && left.Kind == types.KindString) ||
				(right != nil && right.Kind == types.KindString) {
				return &types.Type{Kind: types.KindString}
			}
			if left != nil && right != nil && left.Kind == right.Kind && left.Kind != types.KindUnknown {
				return left
			}
		case "-", "*", "/", "%":
			left := l.inferExpressionType(e.Left)
			right := l.inferExpressionType(e.Right)
			if left != nil && right != nil && left.Kind == right.Kind && left.Kind != types.KindUnknown {
				return left
			}
			if left != nil && left.Kind != types.KindUnknown {
				return left
			}
			if right != nil && right.Kind != types.KindUnknown {
				return right
			}
		}
	case *ast.TernaryExpression:
		thenTy := l.inferExpressionType(e.ThenExpr)
		elseTy := l.inferExpressionType(e.ElseExpr)
		if thenTy != nil && elseTy != nil && thenTy.Kind != types.KindUnknown &&
			thenTy.Kind == elseTy.Kind && thenTy.Name == elseTy.Name {
			return thenTy
		}
	case *ast.CastExpression:
		return l.resolveTypeRef(e.Type)
	case *ast.SelectorExpression:
		if base, ok := e.Object.(*ast.Identifier); ok && l.Module.IsEnum(base.Value) {
			return &types.Type{Kind: types.KindEnum, Name: emit.MangleIdent(base.Value)}
		}
	case *ast.MatchExpression:
		return nil
	case *ast.IndexExpression:
		leftTy := l.inferExpressionType(e.Left)
		if leftTy != nil {
			switch leftTy.Kind {
			case types.KindList, types.KindBytes:
				return leftTy.Elem
			case types.KindDict:
				return leftTy.Value
			case types.KindString:
				return &types.Type{Kind: types.KindString}
			case types.KindAny:
				// Indexing an any-typed receiver yields any so chained
				// navigation and a trailing cast compose.
				return types.Any()
			}
		}
	case *ast.AwaitExpression:
		t := l.inferExpressionType(e.Value)
		if t != nil && t.Kind == types.KindTask {
			return t.Elem
		}
	case *ast.PipeExpression:
		if call, ok := ast.LowerPipe(e); ok {
			return l.inferExpressionType(call)
		}
	case *ast.PartialExpression:
		return l.inferExpressionType(ast.LowerPartial(e))
	case *ast.ListComprehension:
		var bodyTy *types.Type
		l.withComprehensionScope(e.Clauses, func() { bodyTy = l.inferExpressionType(e.Body) })
		if bodyTy == nil {
			bodyTy = types.Any()
		}
		return &types.Type{Kind: types.KindList, Elem: bodyTy}
	case *ast.DictComprehension:
		var kTy, vTy *types.Type
		l.withComprehensionScope(e.Clauses, func() {
			kTy = l.inferExpressionType(e.KeyBody)
			vTy = l.inferExpressionType(e.ValueBody)
		})
		if kTy == nil {
			kTy = &types.Type{Kind: types.KindString}
		}
		if vTy == nil {
			vTy = types.Any()
		}
		return &types.Type{Kind: types.KindDict, Key: kTy, Value: vTy}
	}
	if call, ok := expr.(*ast.CallExpression); ok {
		if sel, ok := call.Callee.(*ast.SelectorExpression); ok {
			if base, ok := sel.Object.(*ast.Identifier); ok {
				if base.Value == "collections" && collectionsFreeFns[sel.Name.Value] && len(call.Arguments) >= 1 {
					recvTy := l.inferExpressionType(call.Arguments[0].Value)
					if t := l.hofMethodReturnType(sel.Name.Value, recvTy, call.Arguments[1:]); t != nil {
						return t
					}
					if t := builtinMethodReturnType(sel.Name.Value, recvTy); t != nil {
						return t
					}
				}
				if l.Module.IsStdlibModule(base.Value) {
					canonical := l.Module.StdlibCanonical(base.Value)
					if entry, ok := l.Bridge.Lookup(canonical, sel.Name.Value); ok && entry.ReturnType != nil {
						return entry.ReturnType
					}
				}
				if prefix, ok := l.Module.UserModulePrefix(base.Value); ok {
					if ret, ok := l.Module.UserModuleReturn(prefix + emit.MangleIdent(sel.Name.Value)); ok {
						return ret
					}
				}
				if l.Module.IsTaggedVariant(base.Value, sel.Name.Value) {
					return &types.Type{Kind: types.KindInterface, Name: emit.MangleIdent(base.Value)}
				}
				if l.Module.IsEnum(base.Value) {
					goName := emit.MangleIdent(base.Value)
					scalar := l.Module.IsScalarEnum(base.Value)
					switch sel.Name.Value {
					case "values":
						return &types.Type{Kind: types.KindList, Elem: &types.Type{Kind: types.KindEnum, Name: goName, EnumScalar: scalar}}
					case "fromName":
						return &types.Type{Kind: types.KindEnum, Name: goName, Nullable: true, EnumScalar: scalar}
					}
				}
			}
			receiverTy := l.inferExpressionType(sel.Object)
			if receiverTy != nil && receiverTy.Kind == types.KindClass && receiverTy.Name == types.RePatternName {
				if t := rePatternMethodReturnType(sel.Name.Value); t != nil {
					return t
				}
			}
			if receiverTy != nil && receiverTy.Kind == types.KindClass && receiverTy.Name == types.URLValueName {
				if t := urlValueMethodReturnType(sel.Name.Value); t != nil {
					return t
				}
			}
			if receiverTy != nil && receiverTy.Kind == types.KindClass && receiverTy.Name == types.TemplateValueName {
				if t := templateValueMethodReturnType(sel.Name.Value); t != nil {
					return t
				}
			}
			if receiverTy != nil && receiverTy.Kind == types.KindClass && receiverTy.Name == types.TemplateEngineName {
				if t := templateEngineMethodReturnType(sel.Name.Value); t != nil {
					return t
				}
			}
			if receiverTy != nil && receiverTy.Kind == types.KindClass && receiverTy.Name == types.DateTimeInstantName {
				if t := dateTimeInstantMethodReturnType(sel.Name.Value); t != nil {
					return t
				}
			}
			if receiverTy != nil && receiverTy.Kind == types.KindClass && receiverTy.Name == types.DateTimeDurationName {
				if t := dateTimeDurationMethodReturnType(sel.Name.Value); t != nil {
					return t
				}
			}
			if receiverTy != nil && receiverTy.Kind == types.KindClass && receiverTy.Name == types.DateTimeZoneName {
				if t := dateTimeZoneMethodReturnType(sel.Name.Value); t != nil {
					return t
				}
			}
			if t := l.hofMethodReturnType(sel.Name.Value, receiverTy, call.Arguments); t != nil {
				return t
			}
			if t := builtinMethodReturnType(sel.Name.Value, receiverTy); t != nil {
				return t
			}
			// A non-HOF method on an any receiver routes through CallMethod, whose
			// result is any so chained any-method navigation composes.
			if receiverTy != nil && receiverTy.Kind == types.KindAny && !anyHofMethods[sel.Name.Value] {
				return types.Any()
			}
			if receiverTy != nil && (receiverTy.Kind == types.KindClass || receiverTy.Kind == types.KindInterface) {
				if ref, ok := l.Module.ClassMethodReturnTypeRef(receiverTy.Name, sel.Name.Value); ok {
					return l.resolveTypeRef(ref)
				}
			}
		}
		if id, ok := call.Callee.(*ast.Identifier); ok {
			if id.Value == "range" {
				return &types.Type{Kind: types.KindList, Elem: &types.Type{Kind: types.KindInt}}
			}
			if l.Module.IsClass(id.Value) {
				if l.Module.InClassHierarchy(id.Value) {
					return &types.Type{Kind: types.KindInterface, Name: emit.MangleIdent(id.Value)}
				}
				return &types.Type{Kind: types.KindClass, Name: emit.MangleIdent(id.Value)}
			}
			if ret, ok := l.Module.FunctionReturnType(emit.MangleIdent(id.Value)); ok {
				return ret
			}
		}
	}
	return l.inferLocalFallback(expr)
}

// hierarchyAsInterface reclassifies a hierarchy class type as its interface so
// field access and dispatch on it route through getters/the interface.
func (l *Lowerer) hierarchyAsInterface(t *types.Type) *types.Type {
	if t != nil && t.Kind == types.KindClass && l.Module.InClassHierarchy(t.Name) {
		clone := *t
		clone.Kind = types.KindInterface
		return &clone
	}
	return t
}

func (l *Lowerer) inferLocalFallback(expr ast.Expression) *types.Type {
	if t := types.InferLiteral(expr); t.Kind != types.KindUnknown {
		return t
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		if b, ok := l.scope.Lookup(e.Value); ok {
			return b.Type
		}
	case *ast.PrefixExpression:
		if e.Operator == "-" || e.Operator == "+" {
			return l.inferLocalFallback(e.Right)
		}
	case *ast.ListLiteral:
		return &types.Type{Kind: types.KindList, Elem: l.elemFallback(e.Elements)}
	case *ast.SetLiteral:
		return &types.Type{Kind: types.KindSet, Elem: l.elemFallback(e.Elements)}
	case *ast.DictLiteral:
		k, v := l.entryFallback(e.Entries)
		return &types.Type{Kind: types.KindDict, Key: k, Value: v}
	}
	return types.Unknown()
}

func (l *Lowerer) elemFallback(elems []ast.Expression) *types.Type {
	var elem *types.Type
	for _, el := range elems {
		et := l.inferExpressionType(el)
		if elem == nil {
			elem = et
			continue
		}
		if !sameTypeKind(elem, et) {
			return types.Any()
		}
	}
	if elem == nil {
		return types.Any()
	}
	return elem
}

func (l *Lowerer) entryFallback(entries []ast.DictEntry) (*types.Type, *types.Type) {
	var k, v *types.Type
	for _, entry := range entries {
		kt := l.inferLocalFallback(entry.Key)
		vt := l.inferLocalFallback(entry.Value)
		if k == nil {
			k, v = kt, vt
			continue
		}
		if !sameTypeKind(k, kt) {
			k = types.Any()
		}
		if !sameTypeKind(v, vt) {
			v = types.Any()
		}
	}
	if k == nil {
		k = &types.Type{Kind: types.KindString}
	}
	if v == nil {
		v = types.Any()
	}
	return k, v
}

func sameTypeKind(a, b *types.Type) bool {
	if a == nil || b == nil || a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case types.KindClass, types.KindInterface, types.KindEnum, types.KindTypeParam:
		return a.Name == b.Name
	}
	return true
}

func exprToken(expr ast.Expression) token.Token {
	switch e := expr.(type) {
	case *ast.Identifier:
		return e.Token
	case *ast.StringLiteral:
		return e.Token
	case *ast.EmbeddedLiteral:
		return e.Token
	case *ast.IntegerLiteral:
		return e.Token
	case *ast.FloatLiteral:
		return e.Token
	case *ast.DecimalLiteral:
		return e.Token
	case *ast.CallExpression:
		return e.Token
	case *ast.SelectorExpression:
		return e.Token
	case *ast.IndexExpression:
		return e.Token
	case *ast.InfixExpression:
		return e.Token
	}
	return token.Token{}
}

func (l *Lowerer) errAt(line, col int, msg, hint string) {
	l.errors = append(l.errors, Error{
		File: l.SourceFile, Line: line, Column: col, Message: msg, Hint: hint,
	})
}
