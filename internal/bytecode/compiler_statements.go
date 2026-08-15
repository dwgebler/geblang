package bytecode

import (
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/runtime"
	"strconv"
	"strings"
)

func (c *Compiler) compileStatement(stmt ast.Statement) error {
	switch stmt := stmt.(type) {
	case *ast.ModuleStatement:
		return nil
	case *ast.ImportStatement:
		if isEvaluatorOnlyBuiltinImport(stmt.Path) {
			return c.withStatementLocation(stmt, parityErrorf("bytecode compiler does not support builtin module %s yet", strings.Join(stmt.Path, ".")))
		}
		c.declareNativeInterfacesForImport(stmt.Path)
		if !isBytecodeImportModule(stmt.Path) {
			alias := stmt.ModuleName()
			if alias == "" {
				return fmt.Errorf("empty import path")
			}
			canonical := strings.Join(stmt.Path, ".")
			// Record the alias so resolveQualifiedClassName can rewrite
			// `alias.Class` references (e.g. `extends mymod.Foo`) into
			// their canonical `module.Class` form for cross-module
			// parent-class dispatch.
			c.moduleAliases[alias] = canonical
			c.sourceModuleAliases[alias] = true
			canonicalIndex := int64(len(c.chunk.Constants))
			c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: canonical})
			aliasIndex := int64(len(c.chunk.Constants))
			c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: alias})
			c.emitAt(OpImportModule, stmt.Token.Line, stmt.Token.Column, canonicalIndex, aliasIndex, c.globalSlot(alias))
			return nil
		}
		// Native (bytecode-callable) imports: register the alias-to-canonical
		// mapping so later `alias.fn(...)` calls can be dispatched as
		// `canonical.fn(...)` by the same module-recognition path that
		// handles unaliased calls. The unaliased form maps a name to
		// itself, which is a harmless no-op in canonicalModule().
		c.moduleAliases[stmt.ModuleName()] = strings.Join(stmt.Path, ".")
		return nil
	case *ast.FromImportStatement:
		if isEvaluatorOnlyBuiltinImport(stmt.Path) {
			return c.withStatementLocation(stmt, parityErrorf("bytecode compiler does not support builtin module %s yet", strings.Join(stmt.Path, ".")))
		}
		canonical := strings.Join(stmt.Path, ".")
		native := isBytecodeImportModule(stmt.Path)
		canonicalIdx := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: canonical})
		nativeFlag := int64(0)
		if native {
			nativeFlag = 1
		}
		operands := []int64{canonicalIdx, nativeFlag, int64(len(stmt.Names))}
		for _, item := range stmt.Names {
			if item.Name == nil {
				return c.withStatementLocation(stmt, fmt.Errorf("from %s import: empty name", canonical))
			}
			nameIdx := int64(len(c.chunk.Constants))
			c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: item.Name.Value})
			operands = append(operands, nameIdx, int64(c.globalSlot(item.Local())))
		}
		c.emitAt(OpImportFrom, stmt.Token.Line, stmt.Token.Column, operands...)
		return nil
	case *ast.ExportStatement:
		if err := c.compileStatement(stmt.Statement); err != nil {
			return err
		}
		if decl, ok := stmt.Statement.(*ast.DeclarationStatement); ok && decl.Name != nil {
			c.chunk.Exports = append(c.chunk.Exports, ExportInfo{Name: decl.Name.Value, Slot: c.globalSlot(decl.Name.Value), FunctionIndex: -1, ClassIndex: -1, InterfaceIndex: -1})
		}
		if fn, ok := stmt.Statement.(*ast.FunctionStatement); ok && fn.Name != nil {
			indices := c.funcs[strings.ToLower(fn.Name.Value)]
			if len(indices) == 0 {
				return c.withStatementLocation(stmt, fmt.Errorf("unknown bytecode function %s", fn.Name.Value))
			}
			if len(indices) == 1 {
				c.chunk.Exports = append(c.chunk.Exports, ExportInfo{Name: fn.Name.Value, Slot: -1, FunctionIndex: indices[0], ClassIndex: -1, InterfaceIndex: -1})
			} else {
				c.chunk.Exports = append(c.chunk.Exports, ExportInfo{Name: fn.Name.Value, Slot: -1, FunctionIndex: -1, ClassIndex: -1, InterfaceIndex: -1, OverloadIndices: append([]int64(nil), indices...)})
			}
		}
		if class, ok := stmt.Statement.(*ast.ClassStatement); ok && class.Name != nil {
			c.chunk.Exports = append(c.chunk.Exports, ExportInfo{Name: class.Name.Value, Slot: -1, FunctionIndex: -1, ClassIndex: c.classes[strings.ToLower(class.Name.Value)], InterfaceIndex: -1})
		}
		if iface, ok := stmt.Statement.(*ast.InterfaceStatement); ok && iface.Name != nil {
			c.chunk.Exports = append(c.chunk.Exports, ExportInfo{Name: iface.Name.Value, Slot: -1, FunctionIndex: -1, ClassIndex: -1, InterfaceIndex: c.interfaces[strings.ToLower(iface.Name.Value)]})
		}
		if enum, ok := stmt.Statement.(*ast.EnumStatement); ok && enum.Name != nil {
			c.chunk.Exports = append(c.chunk.Exports, ExportInfo{Name: enum.Name.Value, Slot: c.globalSlot(enum.Name.Value), FunctionIndex: -1, ClassIndex: -1, InterfaceIndex: -1})
		}
		return nil
	case *ast.InitStatement:
		return c.compileBlock(stmt.Body)
	case *ast.TypeAliasStatement:
		c.declareTypeAlias(stmt)
		return nil
	case *ast.DeclarationStatement:
		declType := c.bytecodeTypeName(stmt.Type)
		if declType == "any" && stmt.Value != nil {
			declType = c.expressionStaticType(stmt.Value)
		}
		if stmt.Value == nil {
			c.emitConstant(runtime.Null{}, stmt.Token.Line, stmt.Token.Column)
		} else {
			expected := declType
			if expected == "any" {
				expected = ""
			}
			value := stmt.Value
			if injected, ok := annotationConstructorCall(stmt); ok {
				value = injected
			}
			if err := c.compileExpressionWithExpected(value, expected); err != nil {
				return err
			}
		}
		// For generic built-in collection declarations (e.g. list<int>, int[]), emit OpTypeAssert
		// to enforce element types at runtime.
		if stmt.Type != nil && stmt.Type.Operator == "" && stmt.Value != nil {
			baseName := strings.ToLower(stmt.Type.Name)
			isListAlias := stmt.Type.ListAlias && len(stmt.Type.Arguments) == 0
			isGenericCollection := len(stmt.Type.Arguments) > 0 && (baseName == "list" || baseName == "set" || baseName == "dict")
			if isListAlias || isGenericCollection {
				fullTypeName := c.bytecodeTypeNameForParam(stmt.Type, nil)
				typeStrIdx := int64(len(c.chunk.Constants))
				c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: fullTypeName})
				c.emitAt(OpTypeAssert, stmt.Token.Line, stmt.Token.Column, typeStrIdx)
			}
		}
		// When the declared type has explicit type arguments (e.g. Box<int>), emit
		// OpSetTypeBindings so the constructed instance carries the declared bindings
		// rather than ones inferred from the constructor argument types.
		if stmt.Type != nil && stmt.Type.Operator == "" && len(stmt.Type.Arguments) > 0 {
			if classIdx, ok := c.classes[strings.ToLower(stmt.Type.Name)]; ok && int(classIdx) < len(c.chunk.Classes) {
				classInfo := c.chunk.Classes[classIdx]
				if len(classInfo.TypeParameters) > 0 {
					operands := []int64{0}
					count := int64(0)
					for i, arg := range stmt.Type.Arguments {
						if i >= len(classInfo.TypeParameters) {
							break
						}
						if arg == nil || arg.Operator != "" || arg.Name == "" {
							continue
						}
						paramNameIdx := int64(len(c.chunk.Constants))
						c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: classInfo.TypeParameters[i]})
						typeNameIdx := int64(len(c.chunk.Constants))
						c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: arg.Name})
						operands = append(operands, paramNameIdx, typeNameIdx)
						count++
					}
					if count > 0 {
						operands[0] = count
						c.emitAt(OpSetTypeBindings, stmt.Token.Line, stmt.Token.Column, operands...)
					}
				}
			}
		}
		if stmt.Kind == "const" {
			c.emitAt(OpShallowFreeze, stmt.Token.Line, stmt.Token.Column)
		}
		if c.inGlobalScope() {
			slot := c.globalSlot(stmt.Name.Value)
			c.globalTypes[stmt.Name.Value] = declType
			c.emitAt(OpDefineGlobal, stmt.Token.Line, stmt.Token.Column, slot)
			return nil
		}
		slot := c.defineLocalWithType(stmt.Name.Value, declType)
		c.emitAt(OpDefineLocal, stmt.Token.Line, stmt.Token.Column, slot)
		return nil
	case *ast.DestructuringStatement:
		return c.compileDestructuringStatement(stmt)
	case *ast.IfStatement:
		return c.compileIfStatement(stmt)
	case *ast.WhileStatement:
		return c.compileWhileStatement(stmt)
	case *ast.ForStatement:
		return c.compileForStatement(stmt)
	case *ast.MatchStatement:
		return c.compileMatchStatement(stmt)
	case *ast.SelectStatement:
		return c.compileSelectStatement(stmt)
	case *ast.FunctionStatement:
		return c.compileFunctionStatement(stmt)
	case *ast.ClassStatement:
		return c.compileClassStatement(stmt)
	case *ast.InterfaceStatement:
		return c.compileInterfaceStatement(stmt)
	case *ast.EnumStatement:
		return c.compileEnumStatement(stmt)
	case *ast.TryStatement:
		return c.compileTryStatement(stmt)
	case *ast.WithStatement:
		return c.compileWithStatement(stmt)
	case *ast.DelStatement:
		return c.compileDelStatement(stmt)
	case *ast.SimpleStatement:
		return c.compileSimpleStatement(stmt)
	case *ast.ReturnStatement:
		if c.inFunc > 0 && stmt.Value != nil && len(c.finalizers) == 0 {
			if emitted, err := c.tryEmitTailCall(stmt); err != nil {
				return err
			} else if emitted {
				return nil
			}
		}
		if c.inFunc > 0 {
			if stmt.Value != nil {
				expected := c.currentReturnType()
				if expected == "void" || expected == "any" {
					expected = ""
				}
				if err := c.compileExpressionWithExpected(stmt.Value, expected); err != nil {
					return err
				}
			} else {
				c.emitConstant(runtime.Null{}, stmt.Token.Line, stmt.Token.Column)
			}
		} else if stmt.Value != nil {
			if err := c.compileExpression(stmt.Value); err != nil {
				return err
			}
			c.emitAt(OpPop, stmt.Token.Line, stmt.Token.Column)
		}
		if err := c.compileActiveFinalizers(stmt.Token.Line, stmt.Token.Column, true); err != nil {
			return err
		}
		c.emitAt(OpReturn, stmt.Token.Line, stmt.Token.Column)
		return nil
	case *ast.YieldStatement:
		if stmt.Value != nil {
			if err := c.compileExpression(stmt.Value); err != nil {
				return err
			}
		} else {
			c.emitConstant(runtime.Null{}, stmt.Token.Line, stmt.Token.Column)
		}
		c.emitAt(OpYield, stmt.Token.Line, stmt.Token.Column)
		return nil
	case *ast.ExpressionStatement:
		if c.tryEmitFusedStringAppendStmt(stmt.Expression) {
			return nil
		}
		if err := c.compileExpression(stmt.Expression); err != nil {
			return err
		}
		if !c.expressionLeavesNoValue(stmt.Expression) {
			c.emitAt(OpPop, stmt.Token.Line, stmt.Token.Column)
		}
		return nil
	default:
		return parityErrorf("bytecode compiler does not support %T yet", stmt)
	}
}

// tryEmitTailCall checks whether `return f(args)` qualifies for tail
// call elimination and, if so, emits an OpTailCall plus the args. Skips
// the OpReturn emission; the tail-called function's eventual OpReturn
// pops the reused frame to the original caller.
//
// Conditions:
//   - The return value is a direct CallExpression with a plain
//     identifier callee resolving to a statically-known function.
//   - The function has no spread / no type args / no decorators.
//   - Args use positional-only (no named args).
//
// Returns true if the tail call was emitted; the caller skips its
// normal compile-then-OpReturn path. Returns false to fall through.
func (c *Compiler) tryEmitTailCall(stmt *ast.ReturnStatement) (bool, error) {
	call, ok := stmt.Value.(*ast.CallExpression)
	if !ok {
		return false, nil
	}
	ident, ok := call.Callee.(*ast.Identifier)
	if !ok {
		return false, nil
	}
	// Skip when the identifier resolves to a local/global (the call
	// dispatches through a closure value, not a direct function).
	if _, isVar := c.resolveName(ident.Value); isVar {
		return false, nil
	}
	// Skip class instantiation: `Stream(args)` is OpNew + constructor,
	// not a direct function call. The constructor function lives in
	// c.funcs under the class name, but tail-calling it would bypass
	// OpNew and produce an empty value. Case-sensitive: `view(args)`
	// must NOT be treated as construction just because `View` exists.
	if classIndex, isClass := c.classes[strings.ToLower(ident.Value)]; isClass && c.chunk.Classes[classIndex].Name == ident.Value {
		return false, nil
	}
	if _, hasSpread := callSpreadIndex(call.Arguments); hasSpread {
		return false, nil
	}
	if len(call.TypeArguments) > 0 {
		return false, nil
	}
	for _, arg := range call.Arguments {
		if arg.Name != nil {
			return false, nil
		}
	}
	index, orderedArgs, err := c.selectFunctionCall(ident.Value, call.Arguments, 0)
	if err != nil {
		return false, nil
	}
	// TCE only for self-recursion: eliding a different caller's frame destroys its trace entry.
	if index != c.currentFuncIndex {
		return false, nil
	}
	fn := c.chunk.Functions[index]
	if fn.Async || fn.IsGenerator || fn.Variadic {
		return false, nil
	}
	if fn.SharesParentFrame {
		// Nested functions borrow the enclosing frame's locals; tail
		// calling would release them out from under any in-flight
		// reference.
		return false, nil
	}
	if len(fn.Decorators) > 0 {
		return false, nil
	}
	if len(fn.TypeParameters) > 0 {
		// Generic functions need type-binding inference; skip TCE.
		return false, nil
	}
	if len(orderedArgs) != len(fn.ParamSlots) {
		// Default args or short calls; let the normal path expand them.
		return false, nil
	}
	for _, paramType := range fn.ParamTypes {
		// Restrict TCE to functions whose params validate in O(1).
		// Container types (dict/list/set/Class<T>) require deep walks
		// that the OpCall path amortises but TCE would repeat per
		// iteration of a tail-recursive loop.
		if paramType == "" {
			continue
		}
		if !isPrimitiveTypeForTCE(paramType) {
			return false, nil
		}
	}
	if err := c.compileOrderedArguments(fn, orderedArgs, 0, call.Token.Line, call.Token.Column); err != nil {
		return false, err
	}
	c.emitAt(OpTailCall, stmt.Token.Line, stmt.Token.Column, index, int64(len(orderedArgs)))
	return true, nil
}

func (c *Compiler) tryEmitFusedStringAppendStmt(expr ast.Expression) bool {
	assign, ok := expr.(*ast.AssignmentExpression)
	if !ok {
		return false
	}
	leftIdent, ok := assign.Left.(*ast.Identifier)
	if !ok {
		return false
	}
	resolved, ok := c.resolveName(leftIdent.Value)
	if !ok || resolved.typ != "string" {
		return false
	}
	if resolved.kind != "local" && resolved.kind != "global" {
		return false
	}
	lit, ok := selfStringConstAppendAssignment(leftIdent.Value, assign.Value)
	if !ok {
		return false
	}
	constIdx := int64(len(c.chunk.Constants))
	c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: lit})
	op := OpAppendStringConstStmt
	if resolved.kind == "global" {
		op = OpAppendGlobalStringConstStmt
	}
	c.emitAt(op, assign.Token.Line, assign.Token.Column, resolved.slot, constIdx)
	return true
}

func (c *Compiler) compileWhileStatement(stmt *ast.WhileStatement) error {
	loopStart := len(c.chunk.Instructions)
	exitJump, err := c.compileConditionAndJumpIfFalse(stmt.Condition, stmt.Token.Line, stmt.Token.Column)
	if err != nil {
		return err
	}
	c.loops = append(c.loops, loopContext{continueTarget: loopStart})
	if err := c.compileBlock(stmt.Body); err != nil {
		c.loops = c.loops[:len(c.loops)-1]
		return err
	}
	loop := c.loops[len(c.loops)-1]
	c.loops = c.loops[:len(c.loops)-1]
	c.emitAt(OpJump, stmt.Token.Line, stmt.Token.Column, int64(loopStart))
	c.patchJump(exitJump)
	for _, jump := range loop.breakJumps {
		c.patchJump(jump)
	}
	return nil
}

func (c *Compiler) compileDestructuringStatement(stmt *ast.DestructuringStatement) error {
	if err := c.compileExpression(stmt.Value); err != nil {
		return err
	}
	tempSlot := c.allocateLocal()
	c.emitAt(OpDefineLocal, stmt.Token.Line, stmt.Token.Column, tempSlot)
	if stmt.IsList {
		c.emitAt(OpCheckUnpackLen, stmt.Token.Line, stmt.Token.Column, tempSlot, int64(len(stmt.Names)))
	}
	for i, name := range stmt.Names {
		c.emitAt(OpGetLocal, stmt.Token.Line, stmt.Token.Column, tempSlot)
		if stmt.IsList {
			idxConst := int64(len(c.chunk.Constants))
			c.chunk.Constants = append(c.chunk.Constants, runtime.SmallInt{Value: int64(i)})
			c.emitAt(OpConstant, stmt.Token.Line, stmt.Token.Column, idxConst)
		} else {
			key := name.Value
			if i < len(stmt.Keys) && stmt.Keys[i] != "" {
				key = stmt.Keys[i]
			}
			keyConst := int64(len(c.chunk.Constants))
			c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: key})
			c.emitAt(OpConstant, stmt.Token.Line, stmt.Token.Column, keyConst)
		}
		c.emitAt(OpIndex, stmt.Token.Line, stmt.Token.Column)
		if stmt.Define {
			if c.inGlobalScope() {
				slot := c.globalSlot(name.Value)
				c.globalTypes[name.Value] = "any"
				c.emitAt(OpDefineGlobal, stmt.Token.Line, stmt.Token.Column, slot)
			} else {
				slot := c.defineLocalWithType(name.Value, "any")
				c.emitAt(OpDefineLocal, stmt.Token.Line, stmt.Token.Column, slot)
			}
		} else {
			resolved, ok := c.resolveName(name.Value)
			if !ok {
				return fmt.Errorf("unknown bytecode name %s", name.Value)
			}
			if resolved.kind == "local" {
				c.emitAt(OpSetLocal, stmt.Token.Line, stmt.Token.Column, resolved.slot)
			} else {
				c.emitAt(OpSetGlobal, stmt.Token.Line, stmt.Token.Column, resolved.slot)
			}
		}
	}
	return nil
}

func (c *Compiler) compileForStatement(stmt *ast.ForStatement) error {
	if stmt.Iterable != nil {
		return c.compileForInStatement(stmt)
	}
	c.pushScope()
	defer c.popScope()
	if stmt.Init != nil {
		if err := c.compileStatement(stmt.Init); err != nil {
			return err
		}
	}
	loopStart := len(c.chunk.Instructions)
	exitJump := -1
	if stmt.Condition != nil {
		jp, err := c.compileConditionAndJumpIfFalse(stmt.Condition, stmt.Token.Line, stmt.Token.Column)
		if err != nil {
			return err
		}
		exitJump = jp
	}
	c.loops = append(c.loops, loopContext{continueTarget: -1})
	if err := c.compileBlock(stmt.Body); err != nil {
		c.loops = c.loops[:len(c.loops)-1]
		return err
	}
	loop := c.loops[len(c.loops)-1]
	c.loops = c.loops[:len(c.loops)-1]
	updateStart := len(c.chunk.Instructions)
	c.patchLoopContinues(loop, updateStart)
	if stmt.Update != nil {
		if err := c.compileStatement(stmt.Update); err != nil {
			return err
		}
	}
	c.emitAt(OpJump, stmt.Token.Line, stmt.Token.Column, int64(loopStart))
	if exitJump >= 0 {
		c.patchJump(exitJump)
	}
	for _, jump := range loop.breakJumps {
		c.patchJump(jump)
	}
	return nil
}

func (c *Compiler) compileForInStatement(stmt *ast.ForStatement) error {
	names := stmt.VarNames
	if len(names) == 0 && stmt.VarName != nil {
		names = []*ast.Identifier{stmt.VarName}
	}
	if len(names) == 0 {
		return fmt.Errorf("for-in loop has no loop variable")
	}
	c.pushScope()
	defer c.popScope()

	c.emitConstant(runtime.Null{}, stmt.Token.Line, stmt.Token.Column)
	valueSlot := c.allocateLocal()
	c.emitAt(OpDefineLocal, stmt.Token.Line, stmt.Token.Column, valueSlot)
	targetSlots := make([]int64, 0, len(names))
	for _, name := range names {
		c.emitConstant(runtime.Null{}, stmt.Token.Line, stmt.Token.Column)
		slot := c.defineLocal(name.Value)
		c.emitAt(OpDefineLocal, stmt.Token.Line, stmt.Token.Column, slot)
		targetSlots = append(targetSlots, slot)
	}
	if err := c.compileExpression(stmt.Iterable); err != nil {
		return err
	}
	c.emitAt(OpIterInit, stmt.Token.Line, stmt.Token.Column)
	iterSlot := c.allocateLocal()
	c.emitAt(OpDefineLocal, stmt.Token.Line, stmt.Token.Column, iterSlot)

	loopStart := len(c.chunk.Instructions)
	nextJump := c.emitJump(OpIterNext, stmt.Token.Line, stmt.Token.Column)
	c.chunk.Instructions[nextJump].Operands = append(c.chunk.Instructions[nextJump].Operands, iterSlot, valueSlot)
	if len(targetSlots) == 1 {
		c.emitAt(OpGetLocal, stmt.Token.Line, stmt.Token.Column, valueSlot)
		c.emitAt(OpSetLocal, stmt.Token.Line, stmt.Token.Column, targetSlots[0])
		c.emitAt(OpPop, stmt.Token.Line, stmt.Token.Column)
	} else {
		operands := append([]int64{valueSlot}, targetSlots...)
		c.emitAt(OpUnpackList, stmt.Token.Line, stmt.Token.Column, operands...)
	}

	c.loops = append(c.loops, loopContext{continueTarget: loopStart, iterSlot: iterSlot, hasIterSlot: true})
	c.pushFinalizer(finalizerContext{iterSlot: iterSlot, hasIterSlot: true})
	if err := c.compileBlock(stmt.Body); err != nil {
		c.popFinalizer()
		c.loops = c.loops[:len(c.loops)-1]
		return err
	}
	c.popFinalizer()
	loop := c.loops[len(c.loops)-1]
	c.loops = c.loops[:len(c.loops)-1]
	c.emitAt(OpJump, stmt.Token.Line, stmt.Token.Column, int64(loopStart))
	c.patchJump(nextJump)
	c.emitAt(OpIterClose, stmt.Token.Line, stmt.Token.Column, iterSlot)
	for _, jump := range loop.breakJumps {
		c.patchJump(jump)
	}
	return nil
}

func (c *Compiler) compileFunctionStatement(stmt *ast.FunctionStatement) error {
	return c.compileFunction(stmt, stmt.Name.Value, "")
}

func (c *Compiler) compileFunction(stmt *ast.FunctionStatement, name string, receiverName string) error {
	return c.compileFunctionWithPrologue(stmt, name, receiverName, nil)
}

func (c *Compiler) currentClassTypeParams() []string {
	if len(c.classStack) == 0 {
		return nil
	}
	classIndex := c.classStack[len(c.classStack)-1]
	if classIndex < 0 || int(classIndex) >= len(c.chunk.Classes) {
		return nil
	}
	return c.chunk.Classes[classIndex].TypeParameters
}

func (c *Compiler) currentClassTypeParamConstraintExprs() []string {
	if len(c.classStack) == 0 {
		return nil
	}
	classIndex := c.classStack[len(c.classStack)-1]
	if classIndex < 0 || int(classIndex) >= len(c.chunk.Classes) {
		return nil
	}
	return c.chunk.Classes[classIndex].TypeParamConstraintExprs
}

func (c *Compiler) compileFunctionWithPrologue(stmt *ast.FunctionStatement, name string, receiverName string, prologue func() error) error {
	/* Static methods compile through the same body pipeline as regular
	 * methods - they just skip the implicit `this` receiver. The
	 * caller is responsible for passing `receiverName = ""` for
	 * statics, and for registering the resulting function index
	 * under `class.StaticMethods` rather than `class.Methods` (see
	 * compileClassStatement). */
	index := c.nextFunctionIndex(name)
	skipJump := c.emitJump(OpJump, stmt.Token.Line, stmt.Token.Column)
	entry := int64(len(c.chunk.Instructions))
	c.pushScope()
	// Nested function statements (declared inside another function's body)
	// share the enclosing function's slot space because their bodies may
	// reference outer locals directly via resolveName. Only outermost
	// function and method declarations reset the slot counter.
	nestedStatement := c.inFunc > 0
	if !nestedStatement {
		c.pushFunctionLocals()
	}
	paramSlots := make([]int64, 0, len(stmt.Parameters))
	var constSlots []int64
	paramNames := make([]string, 0, len(stmt.Parameters))
	paramDisplayNames := make([]string, 0, len(stmt.Parameters))
	paramTypes := make([]string, 0, len(stmt.Parameters))
	paramDecorators := make([][]runtime.DecoratorMetadata, 0, len(stmt.Parameters))
	defaultConstants := make([]int64, 0, len(stmt.Parameters))
	if receiverName != "" {
		paramNames = append(paramNames, strings.ToLower(receiverName))
		paramDisplayNames = append(paramDisplayNames, receiverName)
		paramSlots = append(paramSlots, c.defineLocalWithType(receiverName, ""))
		paramTypes = append(paramTypes, "")
		paramDecorators = append(paramDecorators, nil)
		defaultConstants = append(defaultConstants, -1)
	}
	allTypeParams := append(typeParameterNames(stmt.Generics), c.currentClassTypeParams()...)
	allTypeParamConstraintExprs := append(typeParamConstraintExprs(stmt.Generics), c.currentClassTypeParamConstraintExprs()...)
	for _, param := range stmt.Parameters {
		if param.Name == nil {
			if !nestedStatement {
				c.popFunctionLocals()
			}
			c.popScope()
			return fmt.Errorf("function parameter has no name")
		}
		paramNames = append(paramNames, strings.ToLower(param.Name.Value))
		paramDisplayNames = append(paramDisplayNames, param.Name.Value)
		paramType := c.bytecodeTypeNameForParam(param.Type, allTypeParams)
		paramSlot := c.defineLocalWithType(param.Name.Value, c.bytecodeTypeName(param.Type))
		paramSlots = append(paramSlots, paramSlot)
		if param.Const {
			constSlots = append(constSlots, paramSlot)
		}
		paramTypes = append(paramTypes, paramType)
		paramDecs, err := c.decoratorsMetadata(param.Decorators, "parameter", 0)
		if err != nil {
			if !nestedStatement {
				c.popFunctionLocals()
			}
			c.popScope()
			return err
		}
		paramDecorators = append(paramDecorators, paramDecs)
		if param.Default == nil {
			defaultConstants = append(defaultConstants, -1)
			continue
		}
		value, err := c.constantValueFromExpression(param.Default)
		if err != nil {
			if !nestedStatement {
				c.popFunctionLocals()
			}
			c.popScope()
			return err
		}
		defaultConstants = append(defaultConstants, int64(len(c.chunk.Constants)))
		c.chunk.Constants = append(c.chunk.Constants, value)
	}
	c.chunk.Functions[index].TypeParameters = allTypeParams
	c.chunk.Functions[index].TypeParamConstraintExprs = allTypeParamConstraintExprs
	c.chunk.Functions[index].Doc = stmt.Doc
	c.chunk.Functions[index].Entry = entry
	c.chunk.Functions[index].DefLine = int64(stmt.Token.Line)
	c.chunk.Functions[index].DefColumn = int64(stmt.Token.Column)
	c.chunk.Functions[index].ParamNames = paramNames
	c.chunk.Functions[index].ParamDisplayNames = paramDisplayNames
	c.chunk.Functions[index].ParamSlots = paramSlots
	c.chunk.Functions[index].ParamTypes = paramTypes
	c.chunk.Functions[index].ParamDecorators = paramDecorators
	c.chunk.Functions[index].ReturnType = c.bytecodeReturnType(stmt.ReturnType)
	c.chunk.Functions[index].DefaultConstants = defaultConstants
	c.chunk.Functions[index].IsGenerator = blockContainsYield(stmt.Body)
	target := "function"
	if receiverName != "" {
		target = "method"
	} else if strings.Contains(name, ".") {
		target = "staticMethod"
	}
	dec, err := c.decoratorsMetadata(stmt.Decorators, target, int64(len(c.chunk.Functions[index].Decorators)))
	if err != nil {
		return err
	}
	c.chunk.Functions[index].Decorators = dec
	if n := len(stmt.Parameters); n > 0 && stmt.Parameters[n-1].Variadic {
		c.chunk.Functions[index].Variadic = true
	}
	c.chunk.Functions[index].Async = stmt.Async
	c.inFunc++
	c.returnTypes = append(c.returnTypes, c.chunk.Functions[index].ReturnType)
	// Isolate finalizers so a `return` here emits only this function's, not an
	// enclosing for-loop's iterator close (whose slot lives in another frame).
	savedLoops, savedFinalizers := c.loops, c.finalizers
	c.loops, c.finalizers = nil, nil
	savedFuncIndex := c.currentFuncIndex
	c.currentFuncIndex = index
	defer func() {
		c.loops, c.finalizers = savedLoops, savedFinalizers
		c.currentFuncIndex = savedFuncIndex
	}()
	if prologue != nil {
		if err := prologue(); err != nil {
			c.inFunc--
			c.returnTypes = c.returnTypes[:len(c.returnTypes)-1]
			if !nestedStatement {
				c.popFunctionLocals()
			}
			c.popScope()
			return err
		}
	}
	for _, slot := range constSlots {
		c.emitAt(OpFreezeLocal, stmt.Token.Line, stmt.Token.Column, slot)
	}
	if err := c.compileBlock(stmt.Body); err != nil {
		c.inFunc--
		c.returnTypes = c.returnTypes[:len(c.returnTypes)-1]
		if !nestedStatement {
			c.popFunctionLocals()
		}
		c.popScope()
		return err
	}
	c.emitConstant(runtime.Null{}, stmt.Token.Line, stmt.Token.Column)
	c.emitAt(OpReturn, stmt.Token.Line, stmt.Token.Column)
	if nestedStatement {
		// Inner function shares the outer function's slot space; its
		// LocalCount tracks the current high-water so the outer can
		// surface a correct cumulative count when it finishes. The VM
		// uses the SharesParentFrame flag to skip allocating a fresh
		// locals slice on call entry.
		c.chunk.Functions[index].LocalCount = c.locals
		c.chunk.Functions[index].SharesParentFrame = true
	} else {
		c.chunk.Functions[index].LocalCount = c.popFunctionLocals()
	}
	c.inFunc--
	c.returnTypes = c.returnTypes[:len(c.returnTypes)-1]
	c.popScope()
	c.patchJump(skipJump)
	return nil
}

func (c *Compiler) compileSimpleStatement(stmt *ast.SimpleStatement) error {
	switch stmt.Kind {
	case "break":
		if len(c.loops) == 0 {
			return fmt.Errorf("break is only supported inside bytecode loops")
		}
		if err := c.compileActiveFinalizers(stmt.Token.Line, stmt.Token.Column, true); err != nil {
			return err
		}
		jump := c.emitJump(OpJump, stmt.Token.Line, stmt.Token.Column)
		current := len(c.loops) - 1
		c.loops[current].breakJumps = append(c.loops[current].breakJumps, jump)
		return nil
	case "continue":
		if len(c.loops) == 0 {
			return fmt.Errorf("continue is only supported inside bytecode loops")
		}
		if err := c.compileActiveFinalizers(stmt.Token.Line, stmt.Token.Column, false); err != nil {
			return err
		}
		current := len(c.loops) - 1
		if c.loops[current].continueTarget >= 0 {
			c.emitAt(OpJump, stmt.Token.Line, stmt.Token.Column, int64(c.loops[current].continueTarget))
			return nil
		}
		jump := c.emitJump(OpJump, stmt.Token.Line, stmt.Token.Column)
		c.loops[current].continueJumps = append(c.loops[current].continueJumps, jump)
		return nil
	case "defer":
		return c.compileDeferStatement(stmt)
	case "throw":
		if stmt.Value == nil {
			return fmt.Errorf("throw expects a value")
		}
		if err := c.compileExpression(stmt.Value); err != nil {
			return err
		}
		c.emitAt(OpThrow, stmt.Token.Line, stmt.Token.Column)
		return nil
	default:
		return parityErrorf("bytecode compiler does not support %s statements yet", stmt.Kind)
	}
}

func (c *Compiler) compileTryStatement(stmt *ast.TryStatement) error {
	handlerJump := c.emitJump(OpPushExceptionHandler, stmt.Token.Line, stmt.Token.Column)
	c.pushFinalizer(finalizerContext{body: stmt.Finally, popHandler: true})
	if err := c.compileBlock(stmt.Body); err != nil {
		c.popFinalizer()
		return err
	}
	c.popFinalizer()
	c.emitAt(OpPopExceptionHandler, stmt.Token.Line, stmt.Token.Column)
	normalJump := c.emitJump(OpJump, stmt.Token.Line, stmt.Token.Column)
	c.patchJump(handlerJump)
	catchEndJumps := []int{}
	for _, catch := range stmt.Catches {
		nextCatchJump := c.emitJump(OpCatch, stmt.Token.Line, stmt.Token.Column)
		typeIndex := int64(-1)
		if catch.Type != nil {
			typeIndex = int64(len(c.chunk.Constants))
			c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: catch.Type.Name})
		}
		slot := int64(-1)
		c.pushScope()
		if catch.Name != nil {
			slot = c.defineLocal(catch.Name.Value)
		}
		c.chunk.Instructions[nextCatchJump].Operands = append(c.chunk.Instructions[nextCatchJump].Operands, typeIndex, slot)
		c.pushFinalizer(finalizerContext{body: stmt.Finally})
		for _, bodyStmt := range catch.Body.Statements {
			if err := c.compileStatement(bodyStmt); err != nil {
				c.popFinalizer()
				c.popScope()
				return err
			}
		}
		c.popFinalizer()
		c.popScope()
		catchEndJumps = append(catchEndJumps, c.emitJump(OpJump, stmt.Token.Line, stmt.Token.Column))
		c.patchJump(nextCatchJump)
	}
	if stmt.Finally != nil {
		if err := c.compileBlock(stmt.Finally); err != nil {
			return err
		}
	}
	c.emitAt(OpRethrow, stmt.Token.Line, stmt.Token.Column)
	handledOrNormalTarget := len(c.chunk.Instructions)
	c.chunk.Instructions[normalJump].Operands[0] = int64(handledOrNormalTarget)
	for _, jump := range catchEndJumps {
		c.chunk.Instructions[jump].Operands[0] = int64(handledOrNormalTarget)
	}
	if stmt.Finally != nil {
		if err := c.compileBlock(stmt.Finally); err != nil {
			return err
		}
	}
	return nil
}

// compileWithStatement emits bytecode for `with (expr) { ... }`
// and `with (name = expr) { ... }`. The resource value is stored
// in a hidden local slot so the cleanup epilogue can locate it
// regardless of how the block exits (normal completion, return,
// break, continue, or exception). Cleanup invokes __exit__ if the
// class defines it, else the class destructor.
func (c *Compiler) compileWithStatement(stmt *ast.WithStatement) error {
	c.pushScope()
	defer c.popScope()
	if err := c.compileExpression(stmt.Value); err != nil {
		return err
	}
	hiddenSlot := c.allocateLocal()
	c.emitAt(OpDefineLocal, stmt.Token.Line, stmt.Token.Column, hiddenSlot)
	c.emitAt(OpGetLocal, stmt.Token.Line, stmt.Token.Column, hiddenSlot)
	c.emitAt(OpWithEnter, stmt.Token.Line, stmt.Token.Column)
	if stmt.Name != nil {
		nameSlot := c.defineLocal(stmt.Name.Value)
		c.emitAt(OpDefineLocal, stmt.Token.Line, stmt.Token.Column, nameSlot)
	} else {
		c.emitAt(OpPop, stmt.Token.Line, stmt.Token.Column)
	}
	handlerJump := c.emitJump(OpPushExceptionHandler, stmt.Token.Line, stmt.Token.Column)
	c.pushFinalizer(finalizerContext{popHandler: true, hasWithCleanup: true, withSlot: hiddenSlot})
	if err := c.compileBlock(stmt.Body); err != nil {
		c.popFinalizer()
		return err
	}
	c.popFinalizer()
	c.emitAt(OpPopExceptionHandler, stmt.Token.Line, stmt.Token.Column)
	c.emitAt(OpGetLocal, stmt.Token.Line, stmt.Token.Column, hiddenSlot)
	c.emitAt(OpWithExit, stmt.Token.Line, stmt.Token.Column)
	normalJump := c.emitJump(OpJump, stmt.Token.Line, stmt.Token.Column)
	c.patchJump(handlerJump)
	c.emitAt(OpGetLocal, stmt.Token.Line, stmt.Token.Column, hiddenSlot)
	c.emitAt(OpWithExit, stmt.Token.Line, stmt.Token.Column)
	c.emitAt(OpRethrow, stmt.Token.Line, stmt.Token.Column)
	c.patchJump(normalJump)
	return nil
}

// compileDelStatement emits OpDel for `del x`. The opcode takes
// two operands: the slot index and a kind flag (0 = local,
// 1 = global). Other kinds are rejected at compile time.
func (c *Compiler) compileDelStatement(stmt *ast.DelStatement) error {
	if stmt.Target == nil {
		return fmt.Errorf("del requires an identifier")
	}
	// del operates on variables; declarations are rejected so both backends agree.
	if c.isDeclaredNonVariable(stmt.Target.Value) {
		return fmt.Errorf("cannot del %q: del operates on variables, not declarations", stmt.Target.Value)
	}
	resolved, ok := c.resolveName(stmt.Target.Value)
	if !ok {
		return fmt.Errorf("del: unknown identifier %q", stmt.Target.Value)
	}
	switch resolved.kind {
	case "local":
		c.emitAt(OpDel, stmt.Token.Line, stmt.Token.Column, resolved.slot, 0)
	case "global":
		c.emitAt(OpDel, stmt.Token.Line, stmt.Token.Column, resolved.slot, 1)
	default:
		return fmt.Errorf("del: unsupported binding kind for %q", stmt.Target.Value)
	}
	return nil
}

func (c *Compiler) isDeclaredNonVariable(name string) bool {
	return c.declaredDecls[name]
}

func (c *Compiler) compileDeferStatement(stmt *ast.SimpleStatement) error {
	call, ok := stmt.Value.(*ast.CallExpression)
	if !ok {
		return fmt.Errorf("bytecode compiler only supports deferring calls, not %T; use --disable-vm to run with the tree-walking evaluator", stmt.Value)
	}
	// Plain identifier call: defer someFunc(args)
	if ident, ok := call.Callee.(*ast.Identifier); ok {
		if _, hasSpread := callSpreadIndex(call.Arguments); hasSpread {
			return c.compileDeferIdentifierSpread(stmt, call, ident)
		}
		index, orderedArgs, err := c.selectFunctionCall(ident.Value, call.Arguments, 0)
		if err == nil {
			if err := c.compileOrderedArguments(c.chunk.Functions[index], orderedArgs, 0, stmt.Token.Line, stmt.Token.Column); err != nil {
				return err
			}
			c.emitAt(OpDeferFuncCall, stmt.Token.Line, stmt.Token.Column, index, int64(len(orderedArgs)))
			return nil
		}
		// Fall back to a variable holding a callable (closure/lambda).
		if resolved, isVar := c.resolveName(ident.Value); isVar {
			if resolved.kind == "local" {
				c.emitAt(OpGetLocal, stmt.Token.Line, stmt.Token.Column, resolved.slot)
			} else {
				c.emitAt(OpGetGlobal, stmt.Token.Line, stmt.Token.Column, resolved.slot)
			}
			hasNamedArgs := false
			for _, arg := range call.Arguments {
				if arg.Name != nil {
					hasNamedArgs = true
				}
				if err := c.compileExpression(arg.Value); err != nil {
					return err
				}
			}
			if !hasNamedArgs {
				c.emitAt(OpDeferCallableCall, stmt.Token.Line, stmt.Token.Column, int64(len(call.Arguments)))
				return nil
			}
			operands := []int64{int64(len(call.Arguments))}
			for _, arg := range call.Arguments {
				operands = append(operands, c.argNameIndex(arg))
			}
			c.emitAt(OpDeferCallableCallNamed, stmt.Token.Line, stmt.Token.Column, operands...)
			return nil
		}
		return fmt.Errorf("defer: %w", err)
	}
	module, name, ok := selectorName(call.Callee)
	if !ok {
		// Nested selector (e.g. m.Class.staticMethod): defer via a thunk that runs the call verbatim.
		return c.compileDeferThunk(stmt, call)
	}
	// A user-module-qualified call (m.func / cross-module static) is not an instance method; defer it through the regular cross-module call path via a thunk.
	if c.sourceModuleAliases[module] {
		return c.compileDeferThunk(stmt, call)
	}
	// If the selector object resolves to a variable it's a method call, not a module call.
	if resolved, isVar := c.resolveName(module); isVar {
		if resolved.kind == "local" {
			c.emitAt(OpGetLocal, stmt.Token.Line, stmt.Token.Column, resolved.slot)
		} else {
			c.emitAt(OpGetGlobal, stmt.Token.Line, stmt.Token.Column, resolved.slot)
		}
		if _, hasSpread := callSpreadIndex(call.Arguments); hasSpread {
			meta, err := c.compileSpreadCallArguments(call.Arguments)
			if err != nil {
				return err
			}
			nameIndex := int64(len(c.chunk.Constants))
			c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: name})
			operands := append([]int64{nameIndex, int64(len(call.Arguments))}, meta...)
			c.emitAt(OpDeferMethodCallSpread, stmt.Token.Line, stmt.Token.Column, operands...)
			return nil
		}
		hasNamedArgs := false
		for _, arg := range call.Arguments {
			if arg.Name != nil {
				hasNamedArgs = true
			}
			if err := c.compileExpression(arg.Value); err != nil {
				return err
			}
		}
		nameIndex := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: name})
		if !hasNamedArgs {
			c.emitAt(OpDeferMethodCall, stmt.Token.Line, stmt.Token.Column, nameIndex, int64(len(call.Arguments)))
			return nil
		}
		operands := []int64{nameIndex, int64(len(call.Arguments))}
		for _, arg := range call.Arguments {
			operands = append(operands, c.argNameIndex(arg))
		}
		c.emitAt(OpDeferMethodCallNamed, stmt.Token.Line, stmt.Token.Column, operands...)
		return nil
	}
	// Resolve any `import path as natpath` alias to its canonical name before dispatching (unaliased calls map to themselves).
	canonical := c.canonicalModule(module)
	// Keep dedicated opcodes for io.print/println (they write to stdout directly).
	if canonical == "io" && len(call.Arguments) == 1 && call.Arguments[0].Name == nil && !call.Arguments[0].Spread {
		if err := c.compileExpression(call.Arguments[0].Value); err != nil {
			return err
		}
		switch name {
		case "println":
			c.emitAt(OpDeferPrintln, stmt.Token.Line, stmt.Token.Column)
			return nil
		case "print", "stdoutWrite":
			c.emitAt(OpDeferPrint, stmt.Token.Line, stmt.Token.Column)
			return nil
		}
	}
	if !isBytecodeCallableModule(canonical) {
		// Local static (Class.method) or any other module-qualified call the native path can't record: defer via a thunk.
		return c.compileDeferThunk(stmt, call)
	}
	if _, hasSpread := callSpreadIndex(call.Arguments); hasSpread {
		meta, err := c.compileSpreadCallArguments(call.Arguments)
		if err != nil {
			return err
		}
		nameIndex := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: canonical + "." + name})
		operands := append([]int64{nameIndex, int64(len(call.Arguments))}, meta...)
		c.emitAt(OpDeferNativeCallSpread, stmt.Token.Line, stmt.Token.Column, operands...)
		return nil
	}
	hasNamedArgs := false
	for _, arg := range call.Arguments {
		if arg.Name != nil {
			hasNamedArgs = true
		}
		if err := c.compileExpression(arg.Value); err != nil {
			return err
		}
	}
	nameIndex := int64(len(c.chunk.Constants))
	c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: canonical + "." + name})
	if !hasNamedArgs {
		c.emitAt(OpDeferNativeCall, stmt.Token.Line, stmt.Token.Column, nameIndex, int64(len(call.Arguments)))
		return nil
	}
	operands := []int64{nameIndex, int64(len(call.Arguments))}
	for _, arg := range call.Arguments {
		operands = append(operands, c.argNameIndex(arg))
	}
	c.emitAt(OpDeferNativeCallNamed, stmt.Token.Line, stmt.Token.Column, operands...)
	return nil
}

// compileDeferThunk defers a module-qualified / nested-selector / local-static call by freezing the callee value and its arguments at registration time (a closure thunk would instead capture them by execution-time reference).
func (c *Compiler) compileDeferThunk(stmt *ast.SimpleStatement, call *ast.CallExpression) error {
	if err := c.compileExpression(call.Callee); err != nil {
		return err
	}
	if _, hasSpread := callSpreadIndex(call.Arguments); hasSpread {
		meta, err := c.compileSpreadCallArguments(call.Arguments)
		if err != nil {
			return err
		}
		operands := append([]int64{int64(len(call.Arguments))}, meta...)
		c.emitAt(OpDeferCallableCallSpread, stmt.Token.Line, stmt.Token.Column, operands...)
		return nil
	}
	hasNamedArgs := false
	for _, arg := range call.Arguments {
		if arg.Name != nil {
			hasNamedArgs = true
		}
		if err := c.compileExpression(arg.Value); err != nil {
			return err
		}
	}
	if !hasNamedArgs {
		c.emitAt(OpDeferCallableCall, stmt.Token.Line, stmt.Token.Column, int64(len(call.Arguments)))
		return nil
	}
	operands := []int64{int64(len(call.Arguments))}
	for _, arg := range call.Arguments {
		operands = append(operands, c.argNameIndex(arg))
	}
	c.emitAt(OpDeferCallableCallNamed, stmt.Token.Line, stmt.Token.Column, operands...)
	return nil
}

// compileDeferIdentifierSpread handles `defer someFunc(...xs)`: a compile-time-resolved function uses OpDeferFuncCallSpread; a variable holding a callable falls back to OpDeferCallableCallSpread, matching the non-spread branch's precedence.
func (c *Compiler) compileDeferIdentifierSpread(stmt *ast.SimpleStatement, call *ast.CallExpression, ident *ast.Identifier) error {
	funcIdx, err := c.selectFunctionCallSpread(ident.Value)
	if err == nil {
		meta, compileErr := c.compileSpreadCallArguments(call.Arguments)
		if compileErr != nil {
			return compileErr
		}
		operands := append([]int64{funcIdx, int64(len(call.Arguments))}, meta...)
		c.emitAt(OpDeferFuncCallSpread, stmt.Token.Line, stmt.Token.Column, operands...)
		return nil
	}
	if resolved, isVar := c.resolveName(ident.Value); isVar {
		if resolved.kind == "local" {
			c.emitAt(OpGetLocal, stmt.Token.Line, stmt.Token.Column, resolved.slot)
		} else {
			c.emitAt(OpGetGlobal, stmt.Token.Line, stmt.Token.Column, resolved.slot)
		}
		meta, compileErr := c.compileSpreadCallArguments(call.Arguments)
		if compileErr != nil {
			return compileErr
		}
		operands := append([]int64{int64(len(call.Arguments))}, meta...)
		c.emitAt(OpDeferCallableCallSpread, stmt.Token.Line, stmt.Token.Column, operands...)
		return nil
	}
	return fmt.Errorf("defer: %w", err)
}

// argNameIndex returns the constant-pool index of a String holding the
// argument's keyword name, or -1 when the argument is positional. Used
// by the OpDefer*CallNamed encoders.
func (c *Compiler) argNameIndex(arg ast.CallArgument) int64 {
	if arg.Name == nil {
		return -1
	}
	idx := int64(len(c.chunk.Constants))
	c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: arg.Name.Value})
	return idx
}

func (c *Compiler) pushFinalizer(ctx finalizerContext) {
	c.finalizers = append(c.finalizers, ctx)
}

func (c *Compiler) popFinalizer() {
	c.finalizers = c.finalizers[:len(c.finalizers)-1]
}

func (c *Compiler) compileActiveFinalizers(line int, column int, includeIteratorFinalizers bool) error {
	for i := len(c.finalizers) - 1; i >= 0; i-- {
		ctx := c.finalizers[i]
		if ctx.hasIterSlot && includeIteratorFinalizers {
			c.emitAt(OpIterClose, line, column, ctx.iterSlot)
		}
		if ctx.popHandler {
			c.emitAt(OpPopExceptionHandler, line, column)
		}
		if ctx.hasWithCleanup {
			c.emitAt(OpGetLocal, line, column, ctx.withSlot)
			c.emitAt(OpWithExit, line, column)
		}
		if ctx.body != nil {
			if err := c.compileBlock(ctx.body); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Compiler) compileIfStatement(stmt *ast.IfStatement) error {
	jumpIfFalse, err := c.compileConditionAndJumpIfFalse(stmt.Condition, stmt.Token.Line, stmt.Token.Column)
	if err != nil {
		return err
	}
	if err := c.compileBlock(stmt.Consequence); err != nil {
		return err
	}
	endJumps := []int{c.emitJump(OpJump, stmt.Token.Line, stmt.Token.Column)}
	c.patchJump(jumpIfFalse)
	for _, elseif := range stmt.ElseIfs {
		branchJump, err := c.compileConditionAndJumpIfFalse(elseif.Condition, stmt.Token.Line, stmt.Token.Column)
		if err != nil {
			return err
		}
		if err := c.compileBlock(elseif.Body); err != nil {
			return err
		}
		endJumps = append(endJumps, c.emitJump(OpJump, stmt.Token.Line, stmt.Token.Column))
		c.patchJump(branchJump)
	}
	if stmt.Alternative != nil {
		if err := c.compileBlock(stmt.Alternative); err != nil {
			return err
		}
	}
	for _, jump := range endJumps {
		c.patchJump(jump)
	}
	return nil
}

func (c *Compiler) compileSelectStatement(stmt *ast.SelectStatement) error {
	c.pushScope()
	defer c.popScope()
	line, col := stmt.Token.Line, stmt.Token.Column
	for _, sc := range stmt.Cases {
		if err := c.compileExpression(sc.Channel); err != nil {
			return err
		}
		hIdx := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "_h"})
		c.emitAt(OpGetField, sc.Token.Line, sc.Token.Column, hIdx)
		if sc.Kind == "send" {
			if err := c.compileExpression(sc.Value); err != nil {
				return err
			}
		}
	}
	hasDefault := int64(0)
	if stmt.Default != nil {
		hasDefault = 1
	}
	operands := []int64{int64(len(stmt.Cases)), hasDefault}
	bodyOffsetPositions := make([]int, 0, len(stmt.Cases)+1)
	bindingSlots := make([]int64, 0, len(stmt.Cases))
	for _, sc := range stmt.Cases {
		kindCode := int64(0)
		if sc.Kind == "send" {
			kindCode = 1
		}
		operands = append(operands, kindCode)
		// Placeholder for body offset; patched after body emitted.
		bodyOffsetPositions = append(bodyOffsetPositions, len(operands))
		operands = append(operands, -1)
		bindingSlot := int64(-1)
		if sc.Kind == "recv" && sc.Binding != "" {
			bindingSlot = c.allocateLocal()
		}
		bindingSlots = append(bindingSlots, bindingSlot)
		operands = append(operands, bindingSlot)
	}
	if hasDefault == 1 {
		bodyOffsetPositions = append(bodyOffsetPositions, len(operands))
		operands = append(operands, -1)
	}
	c.emitAt(OpSelect, line, col, operands...)
	selectIPos := len(c.chunk.Instructions) - 1
	endJumps := []int{}
	for i, sc := range stmt.Cases {
		c.chunk.Instructions[selectIPos].Operands[bodyOffsetPositions[i]] = int64(len(c.chunk.Instructions))
		c.pushScope()
		if sc.Kind == "recv" && sc.Binding != "" {
			c.scopes[len(c.scopes)-1][sc.Binding] = binding{kind: "local", slot: bindingSlots[i]}
		}
		if sc.Body != nil {
			if err := c.compileBlock(sc.Body); err != nil {
				c.popScope()
				return err
			}
		}
		c.popScope()
		endJumps = append(endJumps, c.emitJump(OpJump, sc.Token.Line, sc.Token.Column))
	}
	if hasDefault == 1 {
		c.chunk.Instructions[selectIPos].Operands[bodyOffsetPositions[len(stmt.Cases)]] = int64(len(c.chunk.Instructions))
		if err := c.compileBlock(stmt.Default); err != nil {
			return err
		}
		endJumps = append(endJumps, c.emitJump(OpJump, line, col))
	}
	for _, j := range endJumps {
		c.patchJump(j)
	}
	return nil
}

func (c *Compiler) compileMatchStatement(stmt *ast.MatchStatement) error {
	c.pushScope()
	defer c.popScope()
	if err := c.compileExpression(stmt.Expr); err != nil {
		return err
	}
	valueSlot := c.allocateLocal()
	c.emitAt(OpDefineLocal, stmt.Token.Line, stmt.Token.Column, valueSlot)
	endJumps := []int{}
	for _, matchCase := range stmt.Cases {
		c.pushScope()
		nextJumps, err := c.compileMatchCaseCondition(valueSlot, matchCase)
		if err != nil {
			c.popScope()
			return err
		}
		if matchCase.Body != nil {
			if err := c.compileBlock(matchCase.Body); err != nil {
				c.popScope()
				return err
			}
		} else if matchCase.Value != nil {
			// Arrow-bodied arm in statement position: run the action,
			// discard the value.
			if err := c.compileExpression(matchCase.Value); err != nil {
				c.popScope()
				return err
			}
			if !c.expressionLeavesNoValue(matchCase.Value) {
				c.emitAt(OpPop, matchCase.Token.Line, matchCase.Token.Column)
			}
		}
		c.popScope()
		endJumps = append(endJumps, c.emitJump(OpJump, matchCase.Token.Line, matchCase.Token.Column))
		for _, nextJump := range nextJumps {
			c.patchJump(nextJump)
		}
	}
	hint := matchExhaustivenessHint(stmt.Cases)
	hintIndex := int64(len(c.chunk.Constants))
	c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: hint})
	c.emitAt(OpGetLocal, stmt.Token.Line, stmt.Token.Column, valueSlot)
	c.emitAt(OpMatchError, stmt.Token.Line, stmt.Token.Column, hintIndex)
	for _, jump := range endJumps {
		c.patchJump(jump)
	}
	return nil
}

func (c *Compiler) compileBlock(block *ast.BlockStatement) error {
	c.pushScope()
	defer c.popScope()
	for _, stmt := range block.Statements {
		if err := c.compileStatement(stmt); err != nil {
			return c.withStatementLocation(stmt, err)
		}
	}
	return nil
}

func (c *Compiler) compileMatchExpression(expr *ast.MatchExpression) error {
	c.pushScope()
	defer c.popScope()
	if err := c.compileExpression(expr.Expr); err != nil {
		return err
	}
	valueSlot := c.allocateLocal()
	c.emitAt(OpDefineLocal, expr.Token.Line, expr.Token.Column, valueSlot)
	endJumps := []int{}
	for _, matchCase := range expr.Cases {
		c.pushScope()
		nextJumps, err := c.compileMatchCaseCondition(valueSlot, matchCase)
		if err != nil {
			c.popScope()
			return err
		}
		if matchCase.Value == nil {
			c.emitConstant(runtime.Null{}, matchCase.Token.Line, matchCase.Token.Column)
		} else if err := c.compileExpression(matchCase.Value); err != nil {
			c.popScope()
			return err
		}
		c.popScope()
		endJumps = append(endJumps, c.emitJump(OpJump, matchCase.Token.Line, matchCase.Token.Column))
		for _, nextJump := range nextJumps {
			c.patchJump(nextJump)
		}
	}
	hint := matchExhaustivenessHint(expr.Cases)
	hintIndex := int64(len(c.chunk.Constants))
	c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: hint})
	c.emitAt(OpGetLocal, expr.Token.Line, expr.Token.Column, valueSlot)
	c.emitAt(OpMatchError, expr.Token.Line, expr.Token.Column, hintIndex)
	for _, jump := range endJumps {
		c.patchJump(jump)
	}
	return nil
}

func matchExhaustivenessHint(cases []ast.MatchCase) string {
	for _, mc := range cases {
		if mc.Default && mc.Guard == nil {
			return "no match case matched"
		}
	}
	return "no match case matched (add a 'default:' case to handle all values)"
}

func (c *Compiler) compileMatchCaseCondition(valueSlot int64, matchCase ast.MatchCase) ([]int, error) {
	if matchCase.Default {
		if matchCase.Guard == nil {
			return nil, nil
		}
		jp, err := c.compileConditionAndJumpIfFalse(matchCase.Guard, matchCase.Token.Line, matchCase.Token.Column)
		if err != nil {
			return nil, err
		}
		return []int{jp}, nil
	}
	line, col := matchCase.Token.Line, matchCase.Token.Column
	c.emitAt(OpGetLocal, line, col, valueSlot)
	switch {
	case matchCase.EnumVariant != nil:
		ev := matchCase.EnumVariant
		typeName := ev.Enum.Value + "." + ev.Variant.Value
		c.emitConstant(runtime.String{Value: typeName}, line, col)
		c.emitAt(OpInstanceOf, line, col)
	case matchCase.Type != nil:
		c.emitConstant(runtime.String{Value: matchCase.Type.String()}, line, col)
		c.emitAt(OpInstanceOf, line, col)
	case matchCase.ListPattern != nil:
		c.emitAt(OpMatchListShape, line, col, int64(len(matchCase.ListPattern.Bindings)))
	case matchCase.Pattern != nil:
		if err := c.compileExpression(matchCase.Pattern); err != nil {
			return nil, err
		}
		c.emitAt(OpEqual, line, col)
	default:
		return nil, fmt.Errorf("match case has no pattern")
	}
	var nextJumps []int
	bodyJumps := []int{}
	if len(matchCase.Alternates) > 0 {
		primaryFail := c.emitJump(OpJumpIfFalse, line, col)
		bodyJumps = append(bodyJumps, c.emitJump(OpJump, line, col))
		c.patchJump(primaryFail)
		for i, alt := range matchCase.Alternates {
			c.emitAt(OpGetLocal, line, col, valueSlot)
			if err := c.emitOrAlternateCheck(alt, line, col); err != nil {
				return nil, err
			}
			if i == len(matchCase.Alternates)-1 {
				nextJumps = append(nextJumps, c.emitJump(OpJumpIfFalse, line, col))
			} else {
				altFail := c.emitJump(OpJumpIfFalse, line, col)
				bodyJumps = append(bodyJumps, c.emitJump(OpJump, line, col))
				c.patchJump(altFail)
			}
		}
		for _, jp := range bodyJumps {
			c.patchJump(jp)
		}
	} else {
		nextJumps = []int{c.emitJump(OpJumpIfFalse, line, col)}
	}
	if matchCase.EnumVariant != nil {
		for i, param := range matchCase.EnumVariant.Params {
			if param.Name != nil {
				c.emitAt(OpGetLocal, line, col, valueSlot)
				idxName := strconv.Itoa(i)
				nameIndex := int64(len(c.chunk.Constants))
				c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: idxName})
				c.emitAt(OpGetField, line, col, nameIndex)
				c.emitAt(OpDefineLocal, line, col, c.defineLocal(param.Name.Value))
			}
		}
	}
	if matchCase.ListPattern != nil {
		for i, binding := range matchCase.ListPattern.Bindings {
			if binding.Literal != nil {
				c.emitAt(OpGetLocal, line, col, valueSlot)
				c.emitConstant(runtime.SmallInt{Value: int64(i)}, line, col)
				c.emitAt(OpIndex, line, col)
				if err := c.compileExpression(binding.Literal); err != nil {
					return nil, err
				}
				c.emitAt(OpEqual, line, col)
				nextJumps = append(nextJumps, c.emitJump(OpJumpIfFalse, line, col))
				continue
			}
			isWildcard := binding.Name == nil || binding.Name.Value == "_"
			if binding.Type == nil && isWildcard {
				continue
			}
			c.emitAt(OpGetLocal, line, col, valueSlot)
			c.emitConstant(runtime.SmallInt{Value: int64(i)}, line, col)
			c.emitAt(OpIndex, line, col)
			if binding.Type != nil {
				c.emitConstant(runtime.String{Value: binding.Type.Name}, line, col)
				c.emitAt(OpInstanceOf, line, col)
				nextJumps = append(nextJumps, c.emitJump(OpJumpIfFalse, line, col))
				if !isWildcard {
					c.emitAt(OpGetLocal, line, col, valueSlot)
					c.emitConstant(runtime.SmallInt{Value: int64(i)}, line, col)
					c.emitAt(OpIndex, line, col)
				}
			}
			if isWildcard {
				continue
			}
			c.emitAt(OpDefineLocal, line, col, c.defineLocal(binding.Name.Value))
		}
	}
	if matchCase.Name != nil {
		c.emitAt(OpGetLocal, matchCase.Token.Line, matchCase.Token.Column, valueSlot)
		c.emitAt(OpDefineLocal, matchCase.Token.Line, matchCase.Token.Column, c.defineLocal(matchCase.Name.Value))
	}
	if matchCase.Guard != nil {
		jp, err := c.compileConditionAndJumpIfFalse(matchCase.Guard, matchCase.Token.Line, matchCase.Token.Column)
		if err != nil {
			return nil, err
		}
		nextJumps = append(nextJumps, jp)
	}
	return nextJumps, nil
}

// emitOrAlternateCheck emits the type-or-equality check for a single
// or-pattern alternate. The value to test is already on the stack.
// Bare-type alternates (e.g. `int`, stored as an Identifier) emit
// OpInstanceOf; everything else evaluates the expression and emits OpEqual.
func (c *Compiler) emitOrAlternateCheck(alt ast.Expression, line, col int) error {
	if ident, ok := alt.(*ast.Identifier); ok && isOrAlternateBuiltinType(ident.Value) {
		c.emitConstant(runtime.String{Value: ident.Value}, line, col)
		c.emitAt(OpInstanceOf, line, col)
		return nil
	}
	if err := c.compileExpression(alt); err != nil {
		return err
	}
	c.emitAt(OpEqual, line, col)
	return nil
}

func isOrAlternateBuiltinType(name string) bool {
	switch name {
	case "string", "int", "float", "decimal", "bool", "bytes", "list", "dict", "set", "range", "null":
		return true
	}
	return false
}

func (c *Compiler) patchLoopContinues(loop loopContext, target int) {
	for _, jump := range loop.continueJumps {
		c.chunk.Instructions[jump].Operands[0] = int64(target)
	}
}

// Canonicalize + resolveName-gate exactly like compileBuiltinCall so an aliased
// io.print/println (value-less opcode) skips the trailing OpPop too.
func (c *Compiler) expressionLeavesNoValue(expr ast.Expression) bool {
	call, ok := expr.(*ast.CallExpression)
	if !ok {
		return false
	}
	module, name, ok := selectorName(call.Callee)
	if !ok {
		return false
	}
	if _, resolved := c.resolveName(module); resolved {
		return false
	}
	return c.canonicalModule(module) == "io" && (name == "println" || name == "print" || name == "stdoutWrite")
}
