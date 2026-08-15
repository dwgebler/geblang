package bytecode

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/desugar"
	"geblang/internal/native"
	"geblang/internal/runtime"
)

type Compiler struct {
	chunk           Chunk
	loops           []loopContext
	globals         map[string]int64
	globalTypes     map[string]string
	globalDeclKinds map[string]globalDecl
	deletedGlobals  map[string]bool
	declaredDecls   map[string]bool // case-sensitive class/func/enum/interface names
	scopes          []map[string]binding
	// locals is the next slot index for the function currently being
	// compiled. It resets to zero on every function boundary so that
	// slot numbers are relative to the function's own frame rather than
	// accumulating across the whole chunk. The previous value is pushed
	// onto localsStack at function entry and restored at exit. At any
	// point in compilation it doubles as the high-water mark (slots are
	// never reclaimed on scope exit).
	locals          int64
	localsStack     []int64
	funcs           map[string][]int64
	functionCursors map[string]int
	classes         map[string]int64
	// exportedClasses holds lowercased names of classes exported from this chunk; such a class may be subclassed and its methods overridden by an unseen importing module, so the compiler must not devirtualize its method calls.
	exportedClasses map[string]bool
	interfaces      map[string]int64
	interfaceAST    map[string]*ast.InterfaceStatement
	enums           map[string]int64
	typeAliases     map[string]*ast.TypeRef
	inFunc          int
	// Registry index of the function body being compiled (-1 at top level); gates TCE to self-calls.
	currentFuncIndex int64
	classStack       []int64
	finalizers       []finalizerContext
	expectedTypes    []string
	returnTypes      []string
	reflectFuncs     map[string]runtime.DecoratorTarget
	reflectClasses   map[string]runtime.DecoratorTarget
	reflectMethods   map[string]map[string]runtime.DecoratorTarget
	reflectStatics   map[string]map[string]runtime.DecoratorTarget
	// moduleAliases maps an `import X as Y` alias name to its canonical
	// dotted module path. Populated for every native (bytecode-callable)
	// import so module-recognition sites can translate `Y.fn(...)` calls
	// back to the canonical `X.fn` dispatch.
	moduleAliases map[string]string
	// sourceModuleAliases marks aliases imported via OpImportModule, whose
	// dir() lists the runtime source Exports rather than the native symbols.
	sourceModuleAliases map[string]bool
	// fromImports: from-import local/alias name -> fully-qualified class name.
	fromImports map[string]string
	// nativeSymbols sources the compile-time dir(<moduleAlias>) member list.
	nativeSymbols map[string]map[string]struct{}
	// AssertionsDisabled elides assert(...) call sites at compile time
	// (no code emitted; arguments are not evaluated). Set via the
	// --no-assert CLI flag on `geblang` and `geblang build`.
	AssertionsDisabled bool
	// embeds dedups embed(...) file records by path across repeated compilation of the same node.
	embeds map[string][32]byte
}

type binding struct {
	kind string
	slot int64
	typ  string
}

type loopContext struct {
	continueTarget int
	continueJumps  []int
	breakJumps     []int
	iterSlot       int64
	hasIterSlot    bool
}

type finalizerContext struct {
	body           *ast.BlockStatement
	popHandler     bool
	iterSlot       int64
	hasIterSlot    bool
	withSlot       int64
	hasWithCleanup bool
}

// AssertionsDisabled, when set true before Compile (typically by the
// --no-assert CLI flag), elides every assert(...) call at compile time:
// neither the condition nor the message is evaluated. Off by default.
var AssertionsDisabled bool

// CompileOptions carries optional compile-time inputs.
type CompileOptions struct {
	// NativeSymbols sources the compile-time dir(<moduleAlias>) member list;
	// nil falls back to the runtime OpDir path.
	NativeSymbols map[string]map[string]struct{}
}

func Compile(program *ast.Program, source []byte, compilerVersion string) (Chunk, error) {
	return CompileWithOptions(program, source, compilerVersion, CompileOptions{})
}

func CompileWithOptions(program *ast.Program, source []byte, compilerVersion string, opts CompileOptions) (Chunk, error) {
	if err := desugar.Dataclasses(program); err != nil {
		return Chunk{}, err
	}
	if err := desugar.Memoize(program); err != nil {
		return Chunk{}, err
	}
	c := &Compiler{
		nativeSymbols: opts.NativeSymbols,
		chunk: Chunk{
			SourceHash: SourceHash(source),
			Compiler:   compilerVersion,
		},
		AssertionsDisabled:  AssertionsDisabled,
		currentFuncIndex:    -1,
		globals:             map[string]int64{},
		globalTypes:         map[string]string{},
		globalDeclKinds:     map[string]globalDecl{},
		deletedGlobals:      map[string]bool{},
		declaredDecls:       map[string]bool{},
		scopes:              []map[string]binding{{}},
		funcs:               map[string][]int64{},
		functionCursors:     map[string]int{},
		classes:             map[string]int64{},
		exportedClasses:     map[string]bool{},
		interfaces:          map[string]int64{},
		interfaceAST:        map[string]*ast.InterfaceStatement{},
		enums:               map[string]int64{},
		typeAliases:         map[string]*ast.TypeRef{},
		reflectFuncs:        map[string]runtime.DecoratorTarget{},
		reflectClasses:      map[string]runtime.DecoratorTarget{},
		reflectMethods:      map[string]map[string]runtime.DecoratorTarget{},
		reflectStatics:      map[string]map[string]runtime.DecoratorTarget{},
		moduleAliases:       map[string]string{},
		sourceModuleAliases: map[string]bool{},
		fromImports:         map[string]string{},
	}
	// A top-level `del name` removes the binding, so a later same-name
	// declaration is a legal re-bind (the evaluator allows it at runtime).
	// Exempt del'd names from the redeclaration check rather than model
	// control flow; erring toward accept never rejects valid code.
	for _, stmt := range program.Statements {
		if del, ok := stmt.(*ast.DelStatement); ok && del.Target != nil {
			c.deletedGlobals[del.Target.Value] = true
		}
	}
	for _, stmt := range program.Statements {
		exported := false
		if export, ok := stmt.(*ast.ExportStatement); ok {
			stmt = export.Statement
			exported = true
		}
		if exported {
			if class, ok := stmt.(*ast.ClassStatement); ok && class.Name != nil {
				c.exportedClasses[strings.ToLower(class.Name.Value)] = true
			}
		}
		if alias, ok := stmt.(*ast.TypeAliasStatement); ok {
			c.declareTypeAlias(alias)
		}
		if from, ok := stmt.(*ast.FromImportStatement); ok {
			canonical := strings.Join(from.Path, ".")
			for _, n := range from.Names {
				// A from-imported name is immutable: it cannot be locally
				// redeclared or overloaded. Re-importing the same symbol is idempotent.
				if local := n.Local(); local != "" && n.Name != nil {
					if msg := c.claimGlobalKind(local, "import", canonical+"."+n.Name.Value); msg != "" {
						return Chunk{}, fmt.Errorf("line %d:%d: %s", from.Token.Line, from.Token.Column, msg)
					}
					c.fromImports[local] = canonical + "." + n.Name.Value
				}
			}
		}
		if fn, ok := stmt.(*ast.FunctionStatement); ok {
			c.declaredDecls[fn.Name.Value] = true
			if msg := c.claimGlobalKind(fn.Name.Value, "func", ""); msg != "" {
				return Chunk{}, fmt.Errorf("line %d:%d: %s", fn.Token.Line, fn.Token.Column, msg)
			}
			index := c.declareFunction(fn.Name.Value)
			/* Forward-call resolution: populate the function's
			 * signature (param names/types, return type, defaults,
			 * variadic flag) up front so a body compiled before
			 * this declaration can still match the call site
			 * against a real signature instead of skipping the
			 * "uninitialised" candidate. The body itself is filled
			 * in by compileFunctionStatement on the second pass. */
			c.populateFunctionSignature(index, fn)
			key := strings.ToLower(fn.Name.Value)
			meta, err := reflectFunctionMetadataFromStatement(c, fn, "function", int64(len(c.funcs[key])-1))
			if err != nil {
				return Chunk{}, err
			}
			c.reflectFuncs[key] = appendReflectFunctionTarget(c.reflectFuncs[key], meta)
		}
		if class, ok := stmt.(*ast.ClassStatement); ok {
			c.declaredDecls[class.Name.Value] = true
			if msg := c.claimGlobalKind(class.Name.Value, "class", ""); msg != "" {
				return Chunk{}, fmt.Errorf("line %d:%d: %s", class.Token.Line, class.Token.Column, msg)
			}
			c.declareClass(class.Name.Value)
			classKey := strings.ToLower(class.Name.Value)
			classDec, err := c.decoratorsMetadata(class.Decorators, "class", 0)
			if err != nil {
				return Chunk{}, err
			}
			c.reflectClasses[classKey] = runtime.DecoratorTarget{Target: "class", Decorators: classDec, Class: reflectClassMetadataFromStatement(class)}
			for _, member := range class.Members {
				fn, ok := member.(*ast.FunctionStatement)
				if !ok {
					continue
				}
				methodKey := strings.ToLower(fn.Name.Value)
				if fn.Static {
					if c.reflectStatics[classKey] == nil {
						c.reflectStatics[classKey] = map[string]runtime.DecoratorTarget{}
					}
					target := c.reflectStatics[classKey][methodKey]
					meta, err := reflectFunctionMetadataFromStatement(c, fn, "staticMethod", int64(reflectFunctionTargetOverloadCount(target)))
					if err != nil {
						return Chunk{}, err
					}
					c.reflectStatics[classKey][methodKey] = appendReflectFunctionTarget(target, meta)
				} else {
					if c.reflectMethods[classKey] == nil {
						c.reflectMethods[classKey] = map[string]runtime.DecoratorTarget{}
					}
					target := c.reflectMethods[classKey][methodKey]
					meta, err := reflectFunctionMetadataFromStatement(c, fn, "method", int64(reflectFunctionTargetOverloadCount(target)))
					if err != nil {
						return Chunk{}, err
					}
					c.reflectMethods[classKey][methodKey] = appendReflectFunctionTarget(target, meta)
				}
			}
		}
		if iface, ok := stmt.(*ast.InterfaceStatement); ok {
			c.declaredDecls[iface.Name.Value] = true
			if msg := c.claimGlobalKind(iface.Name.Value, "interface", ""); msg != "" {
				return Chunk{}, fmt.Errorf("line %d:%d: %s", iface.Token.Line, iface.Token.Column, msg)
			}
			c.declareInterface(iface.Name.Value)
		}
		if enum, ok := stmt.(*ast.EnumStatement); ok {
			c.declaredDecls[enum.Name.Value] = true
			if msg := c.claimGlobalKind(enum.Name.Value, "enum", ""); msg != "" {
				return Chunk{}, fmt.Errorf("line %d:%d: %s", enum.Token.Line, enum.Token.Column, msg)
			}
			c.declareEnum(enum)
		}
		if decl, ok := stmt.(*ast.DeclarationStatement); ok && decl.Name != nil {
			if msg := c.claimGlobalKind(decl.Name.Value, "var", ""); msg != "" {
				return Chunk{}, fmt.Errorf("line %d:%d: %s", decl.Token.Line, decl.Token.Column, msg)
			}
			// Pre-allocate the global slot so a function body compiled before this declaration can resolve a forward reference to it, matching the evaluator.
			c.globalSlot(decl.Name.Value)
		}
		if imp, ok := stmt.(*ast.ImportStatement); ok {
			if alias := imp.ModuleName(); alias != "" {
				if msg := c.claimGlobalKind(alias, "import", strings.Join(imp.Path, ".")); msg != "" {
					return Chunk{}, fmt.Errorf("line %d:%d: %s", imp.Token.Line, imp.Token.Column, msg)
				}
			}
		}
	}
	for _, stmt := range program.Statements {
		if err := c.compileStatement(stmt); err != nil {
			return Chunk{}, c.withStatementLocation(stmt, err)
		}
	}
	c.emit(OpReturn)
	c.chunk.TopLevelLocalCount = c.locals
	c.chunk.GlobalCount = int64(len(c.globals))
	embedPaths := make([]string, 0, len(c.embeds))
	for p := range c.embeds {
		embedPaths = append(embedPaths, p)
	}
	sort.Strings(embedPaths)
	for _, p := range embedPaths {
		c.chunk.Embeds = append(c.chunk.Embeds, EmbedRecord{Path: p, Hash: c.embeds[p]})
	}
	c.chunk.consolidateOperands()
	c.chunk.sharedMeta = newChunkSharedMeta()
	return c.chunk, nil
}

// parityError marks a compiler failure that is a VM capability gap rather
// than a genuine static error in the user's code. IsParityError detects it.
type parityError struct{ err error }

func (e parityError) Error() string { return e.err.Error() }
func (e parityError) Unwrap() error { return e.err }

func parityErrorf(format string, args ...any) error {
	return parityError{err: fmt.Errorf(format, args...)}
}

// NewParityError wraps err as a VM capability gap so that IsParityError
// returns true. Intended for tests that need to synthesize a parity error.
func NewParityError(err error) error { return parityError{err: err} }

type locatedError struct {
	line   int
	column int
	err    error
}

func (e locatedError) Error() string {
	return fmt.Sprintf("line %d:%d: %v", e.line, e.column, e.err)
}

func (e locatedError) Unwrap() error {
	return e.err
}

func (c *Compiler) withStatementLocation(stmt ast.Statement, err error) error {
	if err == nil {
		return nil
	}
	var located locatedError
	if errors.As(err, &located) {
		return err
	}
	line, column := statementLocation(stmt)
	if line <= 0 {
		return err
	}
	return locatedError{line: line, column: column, err: err}
}

func statementLocation(stmt ast.Statement) (int, int) {
	switch stmt := stmt.(type) {
	case *ast.BlockStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.ModuleStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.ImportStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.ExportStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.InitStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.DeclarationStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.ExpressionStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.ReturnStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.YieldStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.SimpleStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.IfStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.WhileStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.ForStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.FunctionStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.ClassStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.InterfaceStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.EnumStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.TryStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.WithStatement:
		return stmt.Token.Line, stmt.Token.Column
	case *ast.MatchStatement:
		return stmt.Token.Line, stmt.Token.Column
	default:
		return 0, 0
	}
}

func functionTypeParameterSet(function FunctionInfo) map[string]bool {
	params := map[string]bool{}
	for _, name := range function.TypeParameters {
		params[strings.ToLower(name)] = true
	}
	return params
}

func functionTypeParameterSetOrNil(function FunctionInfo) map[string]bool {
	if len(function.TypeParameters) == 0 {
		return nil
	}
	return functionTypeParameterSet(function)
}

func (c *Compiler) lookupStaticMethod(classInfo ClassInfo, name string) ([]int64, bool) {
	if indices, ok := classInfo.StaticMethods[strings.ToLower(name)]; ok {
		return indices, true
	}
	if classInfo.ParentIndex >= 0 && int(classInfo.ParentIndex) < len(c.chunk.Classes) {
		return c.lookupStaticMethod(c.chunk.Classes[classInfo.ParentIndex], name)
	}
	return nil, false
}

func (c *Compiler) lookupMethod(classInfo ClassInfo, name string) ([]int64, bool) {
	if indices, ok := classInfo.Methods[strings.ToLower(name)]; ok {
		return indices, true
	}
	if classInfo.ParentIndex >= 0 && int(classInfo.ParentIndex) < len(c.chunk.Classes) {
		return c.lookupMethod(c.chunk.Classes[classInfo.ParentIndex], name)
	}
	return nil, false
}

// crossModuleBoundary reports whether classInfo's same-chunk ancestor chain
// terminates in a cross-module parent, returning that qualified name.
func (c *Compiler) crossModuleBoundary(classInfo ClassInfo) (string, bool) {
	top := classInfo
	for top.ParentIndex >= 0 && int(top.ParentIndex) < len(c.chunk.Classes) {
		top = c.chunk.Classes[top.ParentIndex]
	}
	if strings.Contains(top.ParentName, ".") {
		return top.ParentName, true
	}
	return "", false
}

func (c *Compiler) emitConstant(value runtime.Value, line int, column int) {
	index := int64(len(c.chunk.Constants))
	c.chunk.Constants = append(c.chunk.Constants, value)
	c.emitAt(OpConstant, line, column, index)
}

func (c *Compiler) emit(op Op, operands ...int64) {
	c.emitAt(op, 0, 0, operands...)
}

func (c *Compiler) emitAt(op Op, line int, column int, operands ...int64) {
	c.chunk.Instructions = append(c.chunk.Instructions, Instruction{Op: op, Operands: operands, Line: int32(line), Column: clampColumn(column)})
}

func (c *Compiler) emitJump(op Op, line int, column int) int {
	c.emitAt(op, line, column, -1)
	return len(c.chunk.Instructions) - 1
}

func (c *Compiler) patchJump(index int) {
	c.chunk.Instructions[index].Operands[0] = int64(len(c.chunk.Instructions))
}

func (c *Compiler) pushScope() {
	c.scopes = append(c.scopes, map[string]binding{})
}

func (c *Compiler) popScope() {
	c.scopes = c.scopes[:len(c.scopes)-1]
}

func (c *Compiler) inGlobalScope() bool {
	return len(c.scopes) == 1
}

func (c *Compiler) defineLocal(name string) int64 {
	return c.defineLocalWithType(name, "")
}

func (c *Compiler) defineLocalWithType(name string, typ string) int64 {
	slot := c.allocateLocal()
	c.scopes[len(c.scopes)-1][name] = binding{kind: "local", slot: slot, typ: typ}
	return slot
}

func (c *Compiler) allocateLocal() int64 {
	slot := c.locals
	c.locals++
	return slot
}

// pushFunctionLocals saves the current locals counter and resets it to
// zero so the function being entered gets its own relative slot range.
func (c *Compiler) pushFunctionLocals() {
	c.localsStack = append(c.localsStack, c.locals)
	c.locals = 0
}

// popFunctionLocals returns the LocalCount of the just-compiled function
// (the high-water mark) and restores the enclosing scope's counter.
func (c *Compiler) popFunctionLocals() int64 {
	count := c.locals
	last := len(c.localsStack) - 1
	c.locals = c.localsStack[last]
	c.localsStack = c.localsStack[:last]
	return count
}

func (c *Compiler) declareFunction(name string) int64 {
	key := strings.ToLower(name)
	index := int64(len(c.chunk.Functions))
	c.funcs[key] = append(c.funcs[key], index)
	c.chunk.Functions = append(c.chunk.Functions, FunctionInfo{Name: name})
	return index
}

// populateFunctionSignature fills in the parameter / return type
// metadata for a function declared but not yet compiled. Callers
// that depend on signature-based overload resolution (notably the
// forward-call lookup in selectFunctionIndicesCall) read this
// metadata; the function body is compiled later by
// compileFunctionStatement which overwrites everything except the
// reserved index.
func (c *Compiler) populateFunctionSignature(index int64, fn *ast.FunctionStatement) {
	info := &c.chunk.Functions[index]
	info.ParamNames = make([]string, 0, len(fn.Parameters))
	info.ParamTypes = make([]string, 0, len(fn.Parameters))
	info.DefaultConstants = make([]int64, 0, len(fn.Parameters))
	for _, param := range fn.Parameters {
		info.ParamNames = append(info.ParamNames, param.Name.Value)
		if param.Type != nil {
			info.ParamTypes = append(info.ParamTypes, param.Type.String())
		} else {
			info.ParamTypes = append(info.ParamTypes, "")
		}
		info.DefaultConstants = append(info.DefaultConstants, -1)
		if param.Variadic {
			info.Variadic = true
		}
	}
	info.ReturnType = c.bytecodeReturnType(fn.ReturnType)
}

func (c *Compiler) nextFunctionIndex(name string) int64 {
	key := strings.ToLower(name)
	indices := c.funcs[key]
	cursor := c.functionCursors[key]
	if cursor >= len(indices) {
		index := c.declareFunction(name)
		c.functionCursors[key] = cursor + 1
		return index
	}
	c.functionCursors[key] = cursor + 1
	return indices[cursor]
}

func (c *Compiler) lastFunctionIndex(name string) (int64, error) {
	indices := c.funcs[strings.ToLower(name)]
	if len(indices) == 0 {
		return 0, fmt.Errorf("unknown bytecode function %s", name)
	}
	return indices[len(indices)-1], nil
}

func (c *Compiler) resolveTypeRef(ref *ast.TypeRef) *ast.TypeRef {
	if ref == nil {
		return nil
	}
	out := cloneTypeRef(ref)
	if out.Operator != "" {
		out.Left = c.resolveTypeRef(out.Left)
		out.Right = c.resolveTypeRef(out.Right)
		return out
	}
	if alias, ok := c.typeAliases[strings.ToLower(out.Name)]; ok {
		resolved := cloneTypeRef(alias)
		resolved.Nullable = resolved.Nullable || out.Nullable
		resolved.ListAlias = resolved.ListAlias || out.ListAlias
		return resolved
	}
	for i, arg := range out.Arguments {
		out.Arguments[i] = c.resolveTypeRef(arg)
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

func typeParamConstraintExprs(params []*ast.TypeParam) []string {
	exprs := make([]string, 0, len(params))
	for _, param := range params {
		if param == nil || param.Constraint == nil {
			exprs = append(exprs, "")
		} else {
			exprs = append(exprs, typeRefConstraintString(param.Constraint))
		}
	}
	return exprs
}

func typeRefConstraintString(ref *ast.TypeRef) string {
	if ref == nil {
		return ""
	}
	if ref.Operator != "" {
		return "(" + typeRefConstraintString(ref.Left) + ref.Operator + typeRefConstraintString(ref.Right) + ")"
	}
	return ref.Name
}

func blockContainsYield(block *ast.BlockStatement) bool {
	if block == nil {
		return false
	}
	for _, stmt := range block.Statements {
		if statementContainsYield(stmt) {
			return true
		}
	}
	return false
}

func statementContainsYield(stmt ast.Statement) bool {
	switch stmt := stmt.(type) {
	case *ast.YieldStatement:
		return true
	case *ast.IfStatement:
		if blockContainsYield(stmt.Consequence) || blockContainsYield(stmt.Alternative) {
			return true
		}
		for _, elseif := range stmt.ElseIfs {
			if blockContainsYield(elseif.Body) {
				return true
			}
		}
	case *ast.WhileStatement:
		return blockContainsYield(stmt.Body)
	case *ast.ForStatement:
		return blockContainsYield(stmt.Body)
	case *ast.TryStatement:
		if blockContainsYield(stmt.Body) || blockContainsYield(stmt.Finally) {
			return true
		}
		for _, catch := range stmt.Catches {
			if blockContainsYield(catch.Body) {
				return true
			}
		}
	case *ast.MatchStatement:
		for _, matchCase := range stmt.Cases {
			if blockContainsYield(matchCase.Body) {
				return true
			}
		}
	case *ast.WithStatement:
		return blockContainsYield(stmt.Body)
	}
	return false
}

func (c *Compiler) bytecodeTypeName(typ *ast.TypeRef) string {
	if typ == nil || strings.EqualFold(typ.Name, "any") {
		return "any"
	}
	if typ.Operator == "|" || typ.Operator == "&" {
		// Preserve union / intersection shape so the VM type-spec
		// parser can split on the top-level operator. Other binary
		// operators (legacy / unknown) keep the historical
		// "any" fallback.
		return c.bytecodeTypeName(typ.Left) + " " + typ.Operator + " " + c.bytecodeTypeName(typ.Right)
	}
	if typ.Operator != "" {
		return "any"
	}
	typ = c.resolveTypeRef(typ)
	name := typ.Name
	if isCallableTypeName(name) {
		name = "func"
	}
	if isGeneratorTypeName(name) {
		name = "generator"
	}
	if typ.ListAlias || strings.EqualFold(name, "list") {
		name = "list"
	}
	if typ.Nullable {
		return "?" + name
	}
	return name
}

// bytecodeTypeNameForParam is like bytecodeTypeName but preserves generic type arguments.
// Concrete collection types (e.g. list<int>, int[]) retain their element type for runtime enforcement.
// Generic type parameters (e.g. list<T>) are also preserved for type-binding inference.
func (c *Compiler) bytecodeTypeNameForParam(typ *ast.TypeRef, typeParams []string) string {
	base := c.bytecodeTypeName(typ)
	if typ == nil {
		return base
	}
	// T[] syntax: ListAlias=true, Arguments is empty, Name holds the element type.
	if typ.ListAlias && len(typ.Arguments) == 0 && typ.Name != "" && !strings.EqualFold(typ.Name, "list") {
		return "list<" + typ.Name + ">"
	}
	if len(typ.Arguments) == 0 {
		return base
	}
	parts := make([]string, 0, len(typ.Arguments))
	for _, arg := range typ.Arguments {
		if arg == nil || arg.Operator != "" || arg.Name == "" {
			parts = append(parts, c.bytecodeTypeName(arg))
			continue
		}
		parts = append(parts, c.bytecodeTypeNameForParam(arg, typeParams))
	}
	return base + "<" + strings.Join(parts, ",") + ">"
}

func normalizeCallableTypeName(name string) string {
	nullable := strings.HasPrefix(name, "?")
	trimmed := strings.TrimPrefix(name, "?")
	if isCallableTypeName(trimmed) {
		if nullable {
			return "?func"
		}
		return "func"
	}
	return name
}

func isCallableTypeName(name string) bool {
	return strings.EqualFold(name, "func") || strings.EqualFold(name, "callable") || strings.EqualFold(name, "function")
}

func isGeneratorTypeName(name string) bool {
	return strings.EqualFold(name, "generator") || strings.EqualFold(name, "iterable")
}

func (c *Compiler) bytecodeReturnType(typ *ast.TypeRef) string {
	if typ == nil {
		return "void"
	}
	return c.bytecodeTypeName(typ)
}

func (c *Compiler) currentExpectedType() string {
	if len(c.expectedTypes) == 0 {
		return ""
	}
	return c.expectedTypes[len(c.expectedTypes)-1]
}

func (c *Compiler) currentReturnType() string {
	if len(c.returnTypes) == 0 {
		return ""
	}
	return c.returnTypes[len(c.returnTypes)-1]
}

// resolveQualifiedClassName takes a dotted reference like `pluginmod.Plugin`
// and replaces the alias prefix with its canonical module path so the
// runtime can locate the class through the module loader.
func (c *Compiler) resolveQualifiedClassName(name string) string {
	dot := strings.LastIndex(name, ".")
	if dot < 0 {
		return name
	}
	prefix := name[:dot]
	className := name[dot+1:]
	if canonical, ok := c.moduleAliases[prefix]; ok {
		return canonical + "." + className
	}
	return name
}

// canonicalModule returns the canonical native-module path that `name`
// resolves to. For an alias registered at `import` time (e.g.
// `import path as natpath`), returns the canonical path ("path"). For
// an unaliased identifier or a name that isn't a known module alias,
// returns `name` unchanged - safe for the caller to feed straight into
// `isBytecodeCallableModule` / `isStatefulBytecodeBuiltin`.
func (c *Compiler) canonicalModule(name string) string {
	if canonical, ok := c.moduleAliases[name]; ok {
		return canonical
	}
	return name
}

func (c *Compiler) resolveName(name string) (binding, bool) {
	for i := len(c.scopes) - 1; i >= 1; i-- {
		if resolved, ok := c.scopes[i][name]; ok {
			return resolved, true
		}
	}
	if slot, ok := c.globals[name]; ok {
		return binding{kind: "global", slot: slot, typ: c.globalTypes[name]}, true
	}
	return binding{}, false
}

func (c *Compiler) globalSlot(name string) int64 {
	if slot, ok := c.globals[name]; ok {
		return slot
	}
	slot := int64(len(c.globals))
	c.globals[name] = slot
	return slot
}

type globalDecl struct {
	kind      string
	canonical string // module path; lets idempotent re-import of one module pass
}

// Mirrors the evaluator's Environment.Define/DefineFunction: only func+func
// (overloads) and re-import of the same module pass; every other same-name
// top-level declaration is a redeclaration. Type aliases use a separate
// namespace and never call this.
func (c *Compiler) claimGlobalKind(name, kind, canonical string) string {
	if c.deletedGlobals[name] {
		return ""
	}
	if existing, ok := c.globalDeclKinds[name]; ok {
		if existing.kind == "func" && kind == "func" {
			return ""
		}
		if existing.kind == "import" && kind == "import" && existing.canonical == canonical {
			return ""
		}
		return fmt.Sprintf("%q is already declared in this scope", name)
	}
	c.globalDeclKinds[name] = globalDecl{kind: kind, canonical: canonical}
	return ""
}

func selectorName(expr ast.Expression) (string, string, bool) {
	selector, ok := expr.(*ast.SelectorExpression)
	if !ok {
		return "", "", false
	}
	ident, ok := selector.Object.(*ast.Identifier)
	if !ok {
		return "", "", false
	}
	return ident.Value, selector.Name.Value, true
}

func typeNameFromExpression(expr ast.Expression) (string, error) {
	switch expr := expr.(type) {
	case *ast.Identifier:
		return expr.Value, nil
	case *ast.SelectorExpression:
		if expr.Name.Value == "type" {
			return expr.Object.String(), nil
		}
		if ident, ok := expr.Object.(*ast.Identifier); ok {
			return ident.Value + "." + expr.Name.Value, nil
		}
	}
	return "", fmt.Errorf("expected type name, got %s", expr.String())
}

func (c *Compiler) constantValueFromExpression(expr ast.Expression) (runtime.Value, error) {
	switch expr := expr.(type) {
	case *ast.IntegerLiteral:
		value, err := runtime.NewIntLiteral(expr.Value)
		if err != nil {
			return nil, err
		}
		if value.Value.IsInt64() {
			return runtime.SmallInt{Value: value.Value.Int64()}, nil
		}
		return value, nil
	case *ast.DecimalLiteral:
		return runtime.NewDecimalLiteral(expr.Value)
	case *ast.FloatLiteral:
		stripped := strings.ReplaceAll(expr.Value[:len(expr.Value)-1], "_", "")
		value, err := strconv.ParseFloat(stripped, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid float literal %q", expr.Value)
		}
		return runtime.Float{Value: value}, nil
	case *ast.StringLiteral:
		return runtime.String{Value: expr.Value}, nil
	case *ast.EmbeddedLiteral:
		c.recordEmbed(expr)
		if expr.Binary {
			return runtime.Bytes{Value: expr.Content}, nil
		}
		return runtime.String{Value: string(expr.Content)}, nil
	case *ast.Literal:
		switch value := expr.Value.(type) {
		case bool:
			return runtime.Bool{Value: value}, nil
		case nil:
			return runtime.Null{}, nil
		}
	case *ast.ListLiteral:
		/* Accept the empty list literal as a default. Non-empty
		 * lists need runtime expression evaluation (each element
		 * could be an arbitrary expression). Empty lists are safe
		 * because the VM clones on default-fill to avoid the
		 * Python-style mutable-default trap. */
		if len(expr.Elements) == 0 {
			return &runtime.List{Elements: nil}, nil
		}
	case *ast.SetLiteral:
		if len(expr.Elements) == 0 {
			return runtime.Set{Elements: map[string]runtime.SetEntry{}}, nil
		}
	case *ast.DictLiteral:
		if len(expr.Entries) == 0 {
			return runtime.Dict{Entries: map[string]runtime.DictEntry{}}, nil
		}
	}
	return nil, fmt.Errorf("bytecode compiler only supports literal default function parameters")
}

// recordEmbed dedups by path so a node visited twice on an edge compile path is recorded once.
func (c *Compiler) recordEmbed(e *ast.EmbeddedLiteral) {
	if c.embeds == nil {
		c.embeds = map[string][32]byte{}
	}
	c.embeds[e.Path] = SourceHash(e.Content)
}

func nextOverloadIndex(decorators []runtime.DecoratorMetadata) int64 {
	next := int64(0)
	for _, decorator := range decorators {
		if decorator.Overload >= next {
			next = decorator.Overload + 1
		}
	}
	return next
}

func isBytecodeBuiltinModule(name string) bool {
	return name == "io" || name == "sys" || name == "reflect" || native.IsPureBuiltinModule(name)
}

func isBytecodeCallableModule(name string) bool {
	return isBytecodeBuiltinModule(name) || isStatefulBytecodeBuiltinModule(name)
}

func isBytecodeImportModule(path []string) bool {
	return len(path) == 1 && isBytecodeCallableModule(path[0]) && !isDualNameSourceModule(path[0])
}

// isDualNameSourceModule: root native modules that also have a stdlib source,
// so the VM must load them via OpImportModule to reach the source exports.
func isDualNameSourceModule(name string) bool {
	return name == "profiler"
}

func isEvaluatorOnlyBuiltinImport(path []string) bool {
	return len(path) == 1 && isEvaluatorOnlyBuiltinModule(path[0])
}

func isStatefulBytecodeBuiltinModule(name string) bool {
	switch name {
	case "io", "sys", "secrets", "process", "procnative", "sshnative",
		"http", "websocket", "smtp", "web", "db", "ext", "ffinative", "net", "test", "log", "watch",
		"csv", "schema", "serde", "metrics", "trace", "profile", "path", "async", "dotenv", "cli",
		"amqp", "kafka", "dataframe", "onnx", "browser":
		return true
	default:
		return false
	}
}

func isStatefulBytecodeBuiltin(module, name string) bool {
	if isStatefulBytecodeBuiltinModule(module) {
		return true
	}
	switch module {
	case "json", "xml", "yaml":
		return name == "reader" || name == "stream"
	default:
		return false
	}
}

func isEvaluatorOnlyBuiltinModule(name string) bool {
	if isBytecodeCallableModule(name) {
		return false
	}
	switch name {
	case "reflect":
		return true
	default:
		return false
	}
}

func isBuiltinErrorClass(name string) bool {
	switch name {
	case "Error", "RuntimeError", "TypeError", "ValueError", "IOError", "ParseError", "MatchError", "ImmutableError", "PermissionError", "AssertionError", "FatalError", "TimeoutError", "TlsError":
		return true
	default:
		return false
	}
}

// IsParityError reports whether err is a VM capability gap (a construct the
// bytecode compiler doesn't support yet) rather than a genuine static error.
func IsParityError(err error) bool {
	if err == nil {
		return false
	}
	var pe parityError
	return errors.As(err, &pe)
}
