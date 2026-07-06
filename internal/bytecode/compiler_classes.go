package bytecode

import (
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"sort"
	"strings"
)

func reflectFunctionMetadataFromStatement(c *Compiler, stmt *ast.FunctionStatement, target string, overload int64) (runtime.FunctionMetadata, error) {
	parameters := make([]runtime.ParameterMetadata, 0, len(stmt.Parameters))
	for _, param := range stmt.Parameters {
		name := ""
		if param.Name != nil {
			name = param.Name.Value
		}
		paramDecs, err := decoratorsMetadata(param.Decorators, "parameter", 0)
		if err != nil {
			return runtime.FunctionMetadata{}, err
		}
		parameters = append(parameters, runtime.ParameterMetadata{
			Name:       name,
			Type:       c.bytecodeTypeName(param.Type),
			Variadic:   param.Variadic,
			HasDefault: param.Default != nil,
			Decorators: paramDecs,
		})
	}
	dec, err := decoratorsMetadata(stmt.Decorators, target, overload)
	if err != nil {
		return runtime.FunctionMetadata{}, err
	}
	return runtime.FunctionMetadata{
		Name:       stmt.Name.Value,
		Target:     target,
		Doc:        stmt.Doc,
		Parameters: parameters,
		ReturnType: c.bytecodeReturnType(stmt.ReturnType),
		Async:      stmt.Async,
		Variadic:   len(stmt.Parameters) > 0 && stmt.Parameters[len(stmt.Parameters)-1].Variadic,
		Decorators: dec,
		DefLine:    int64(stmt.Token.Line),
		DefColumn:  int64(stmt.Token.Column),
	}, nil
}

func reflectFunctionMetadataFromSignature(c *Compiler, sig *ast.FunctionSignature) runtime.FunctionMetadata {
	parameters := make([]runtime.ParameterMetadata, 0, len(sig.Parameters))
	for _, param := range sig.Parameters {
		name := ""
		if param.Name != nil {
			name = param.Name.Value
		}
		paramDecs, _ := decoratorsMetadata(param.Decorators, "parameter", 0)
		parameters = append(parameters, runtime.ParameterMetadata{
			Name:       name,
			Type:       c.bytecodeTypeName(param.Type),
			Variadic:   param.Variadic,
			HasDefault: param.Default != nil,
			Decorators: paramDecs,
		})
	}
	name := ""
	if sig.Name != nil {
		name = sig.Name.Value
	}
	return runtime.FunctionMetadata{
		Name:       name,
		Target:     "interfaceMethod",
		Doc:        sig.Doc,
		Parameters: parameters,
		ReturnType: c.bytecodeReturnType(sig.ReturnType),
		Variadic:   len(sig.Parameters) > 0 && sig.Parameters[len(sig.Parameters)-1].Variadic,
	}
}

func reflectClassMetadataFromStatement(stmt *ast.ClassStatement) *runtime.ClassMetadata {
	metadata := &runtime.ClassMetadata{
		Name:      stmt.Name.Value,
		Doc:       stmt.Doc,
		DefLine:   int64(stmt.Token.Line),
		DefColumn: int64(stmt.Token.Column),
	}
	if stmt.Extends != nil {
		metadata.Parent = stmt.Extends.Name
	}
	for _, iface := range stmt.Implements {
		metadata.Interfaces = append(metadata.Interfaces, iface.Name)
	}
	methods := map[string]string{}
	staticMethods := map[string]string{}
	for _, member := range stmt.Members {
		switch member := member.(type) {
		case *ast.DeclarationStatement:
			if member.Name != nil && !strings.HasPrefix(member.Kind, "static ") {
				metadata.Fields = append(metadata.Fields, member.Name.Value)
			}
		case *ast.FunctionStatement:
			if member.Name == nil {
				continue
			}
			// A function whose name matches the class name is the
			// constructor - exposed via reflect.constructors rather
			// than reflect.methods. Matches the evaluator and avoids
			// the previous divergence where the VM included the
			// constructor in the methods list.
			if strings.EqualFold(member.Name.Value, stmt.Name.Value) {
				continue
			}
			key := strings.ToLower(member.Name.Value)
			if member.Static {
				staticMethods[key] = member.Name.Value
			} else {
				methods[key] = member.Name.Value
			}
		}
	}
	metadata.Methods = sortedMapValues(methods)
	metadata.StaticMethods = sortedMapValues(staticMethods)
	sort.Strings(metadata.Fields)
	sort.Strings(metadata.Interfaces)
	return metadata
}

func sortedMapValues(values map[string]string) []string {
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

func appendReflectFunctionTarget(target runtime.DecoratorTarget, fn runtime.FunctionMetadata) runtime.DecoratorTarget {
	if target.Target == "" {
		target.Target = fn.Target
	}
	if target.Function == nil {
		copied := fn
		target.Function = &copied
	} else {
		target.Function.Decorators = append(target.Function.Decorators, fn.Decorators...)
	}
	target.Decorators = append(target.Decorators, fn.Decorators...)
	return target
}

func reflectFunctionTargetOverloadCount(target runtime.DecoratorTarget) int {
	maxOverload := int64(-1)
	for _, decorator := range target.Decorators {
		if decorator.Overload > maxOverload {
			maxOverload = decorator.Overload
		}
	}
	return int(maxOverload + 1)
}

func (c *Compiler) declareNativeInterfacesForImport(path []string) {
	if len(path) != 1 {
		return
	}
	switch path[0] {
	case "log":
		c.declareNativeInterface("log.LogInterface", []runtime.FunctionMetadata{
			{
				Name:   "handle",
				Target: "interfaceMethod",
				Parameters: []runtime.ParameterMetadata{
					{Name: "level"},
					{Name: "message"},
					{Name: "fields"},
				},
			},
		})
	}
}

func (c *Compiler) declareNativeInterface(name string, methods []runtime.FunctionMetadata) {
	index := c.declareInterface(name)
	if index < 0 || int(index) >= len(c.chunk.Interfaces) {
		return
	}
	iface := c.chunk.Interfaces[index]
	iface.Methods = append([]runtime.FunctionMetadata(nil), methods...)
	c.chunk.Interfaces[index] = iface
}

func (c *Compiler) compileClassStatement(stmt *ast.ClassStatement) error {
	index := c.declareClass(stmt.Name.Value)
	class := c.chunk.Classes[index]
	class.Doc = stmt.Doc
	class.ParentIndex = -1
	class.DefLine = int64(stmt.Token.Line)
	class.DefColumn = int64(stmt.Token.Column)
	class.TypeParameters = typeParameterNames(stmt.Generics)
	class.TypeParamConstraintExprs = typeParamConstraintExprs(stmt.Generics)
	class.Methods = map[string][]int64{}
	class.StaticValues = map[string]int64{}
	class.StaticMethods = map[string][]int64{}
	callableDecorators := make([]ast.Decorator, 0, len(stmt.Decorators))
	for _, dec := range stmt.Decorators {
		if dec.Name.Value == "immutable" && len(dec.Arguments) == 0 {
			class.Immutable = true
		} else {
			callableDecorators = append(callableDecorators, dec)
		}
	}
	classDec, err := decoratorsMetadata(callableDecorators, "class", 0)
	if err != nil {
		return err
	}
	class.Decorators = classDec
	class.MethodDecorators = map[string][]runtime.DecoratorMetadata{}
	class.StaticDecorators = map[string][]runtime.DecoratorMetadata{}
	if stmt.Extends != nil {
		parentIndex, ok := c.classes[strings.ToLower(stmt.Extends.Name)]
		if ok {
			class.ParentIndex = parentIndex
			class.ParentName = c.chunk.Classes[parentIndex].Name
		} else if isBuiltinErrorClass(stmt.Extends.Name) || strings.Contains(stmt.Extends.Name, ".") {
			// Cross-module parent (e.g. `extends pluginmod.Plugin`). Resolve
			// the alias prefix to its canonical module so the VM can look
			// up the parent class at runtime via the module loader.
			class.ParentName = c.resolveQualifiedClassName(stmt.Extends.Name)
		} else if qual, ok := c.fromImports[stmt.Extends.Name]; ok {
			class.ParentName = qual
		} else {
			return fmt.Errorf("bytecode compiler parent class %s is not declared", stmt.Extends.Name)
		}
		if len(stmt.Extends.Arguments) > 0 {
			args := make([]string, 0, len(stmt.Extends.Arguments))
			for _, arg := range stmt.Extends.Arguments {
				args = append(args, arg.String())
			}
			class.ParentArguments = args
		}
	}
	for _, ifaceRef := range stmt.Implements {
		if _, ok := c.interfaces[strings.ToLower(ifaceRef.Name)]; !ok {
			if strings.Contains(ifaceRef.Name, ".") {
				// Canonicalize the alias so module-exact instanceof agrees with the RHS's declaring module.
				class.Implements = append(class.Implements, c.resolveQualifiedClassName(ifaceRef.Name))
				continue
			} else if qual, ok := c.fromImports[ifaceRef.Name]; ok {
				class.Implements = append(class.Implements, qual)
				continue
			} else {
				return fmt.Errorf("bytecode compiler interface %s is not declared", ifaceRef.Name)
			}
		}
		class.Implements = append(class.Implements, ifaceRef.Name)
	}
	if err := c.injectInterfaceMembers(stmt); err != nil {
		return err
	}
	c.chunk.Classes[index] = class
	c.classStack = append(c.classStack, index)
	defer func() {
		c.classStack = c.classStack[:len(c.classStack)-1]
	}()
	for _, member := range stmt.Members {
		switch member := member.(type) {
		case *ast.DeclarationStatement:
			if member.Kind == "static const" || member.Kind == "static let" {
				value := runtime.Value(runtime.Null{})
				if member.Value != nil {
					parsed, err := constantValueFromExpression(member.Value)
					if err != nil {
						return c.withStatementLocation(member, fmt.Errorf("bytecode compiler only supports literal static %s values", strings.TrimPrefix(member.Kind, "static ")))
					}
					value = parsed
				}
				class.StaticValues[member.Name.Value] = int64(len(c.chunk.Constants))
				c.chunk.Constants = append(c.chunk.Constants, value)
				continue
			}
			if strings.HasPrefix(member.Kind, "static ") {
				return c.withStatementLocation(member, fmt.Errorf("bytecode compiler only supports static const and static let class declarations"))
			}
			class.FieldNames = append(class.FieldNames, member.Name.Value)
			fieldType := ""
			if member.Type != nil {
				fieldType = member.Type.String()
			}
			class.FieldTypes = append(class.FieldTypes, fieldType)
			class.FieldDocs = append(class.FieldDocs, member.Doc)
			/* Field decorators ride alongside the field at compile time
			 * so reflect.fields can surface them at runtime without
			 * re-reading the AST. Frameworks consume them as
			 * metadata - they are never auto-executed on field
			 * access or assignment. */
			fieldDecs := member.Decorators
			if hasImmutableFieldDecorator(member.Decorators) {
				if member.Value != nil {
					return c.withStatementLocation(member, fmt.Errorf("@immutable field %q may not declare a default value", member.Name.Value))
				}
				class.ImmutableFields = append(class.ImmutableFields, member.Name.Value)
				fieldDecs = withoutImmutableDecorator(member.Decorators)
			}
			class.FieldDecorators = appendFieldDecorators(class.FieldDecorators, len(class.FieldNames)-1, fieldDecs)
			if member.Value == nil {
				class.FieldDefaults = append(class.FieldDefaults, -1)
				continue
			}
			value, err := constantValueFromExpression(member.Value)
			if err != nil {
				return c.withStatementLocation(member, fmt.Errorf("bytecode compiler only supports literal class field defaults"))
			}
			class.FieldDefaults = append(class.FieldDefaults, int64(len(c.chunk.Constants)))
			c.chunk.Constants = append(c.chunk.Constants, value)
		case *ast.FunctionStatement:
			// Flush field metadata into chunk.Classes before compiling
			// methods so staticIntExpr / staticStringExpr can resolve
			// `this.field` against the up-to-date class info.
			c.chunk.Classes[index] = class
			functionName := stmt.Name.Value + "." + member.Name.Value
			compiledMember := *member
			compiledMember.Static = false
			var receiverName string
			var prologue func() error
			if member.Static {
				receiverName = ""
			} else {
				receiverName = "this"
			}
			if !member.Static && strings.EqualFold(member.Name.Value, stmt.Name.Value) && class.ParentIndex >= 0 && !containsParentConstructorCall(member.Body) {
				prologue = func() error {
					parent := c.chunk.Classes[class.ParentIndex]
					if len(parent.ConstructorIndices) > 0 {
						if _, _, err := c.selectFunctionIndicesCallIgnoringReturn(parent.Name, parent.ConstructorIndices, nil, 1); err != nil {
							return err
						}
					}
					resolved, ok := c.resolveName("this")
					if !ok {
						return fmt.Errorf("auto parent constructor call requires this")
					}
					c.emitAt(OpGetLocal, member.Token.Line, member.Token.Column, resolved.slot)
					c.emitAt(OpCallParentConstructor, member.Token.Line, member.Token.Column, index, 0)
					c.emitAt(OpPop, member.Token.Line, member.Token.Column)
					return nil
				}
			}
			if err := c.compileFunctionWithPrologue(&compiledMember, functionName, receiverName, prologue); err != nil {
				return c.withStatementLocation(member, err)
			}
			functionIndex, err := c.lastFunctionIndex(functionName)
			if err != nil {
				return err
			}
			if member.Static {
				key := strings.ToLower(member.Name.Value)
				class.StaticMethods[key] = append(class.StaticMethods[key], functionIndex)
				staticDec, err := decoratorsMetadata(member.Decorators, "staticMethod", nextOverloadIndex(class.StaticDecorators[key]))
				if err != nil {
					return c.withStatementLocation(member, err)
				}
				class.StaticDecorators[key] = append(class.StaticDecorators[key], staticDec...)
			} else if strings.EqualFold(member.Name.Value, stmt.Name.Value) {
				class.ConstructorIndices = append(class.ConstructorIndices, functionIndex)
			} else {
				key := strings.ToLower(member.Name.Value)
				class.Methods[key] = append(class.Methods[key], functionIndex)
				methodDec, err := decoratorsMetadata(member.Decorators, "method", nextOverloadIndex(class.MethodDecorators[key]))
				if err != nil {
					return c.withStatementLocation(member, err)
				}
				class.MethodDecorators[key] = append(class.MethodDecorators[key], methodDec...)
			}
		default:
			return c.withStatementLocation(member, parityErrorf("bytecode compiler does not support class member %T yet", member))
		}
	}
	if stmt.Destructor != nil {
		dtor := stmt.Destructor
		functionName := stmt.Name.Value + ".~" + dtor.Name.Value
		compiled := *dtor
		compiled.Static = false
		if err := c.compileFunctionWithPrologue(&compiled, functionName, "this", nil); err != nil {
			return c.withStatementLocation(dtor, err)
		}
		functionIndex, err := c.lastFunctionIndex(functionName)
		if err != nil {
			return err
		}
		class.DestructorIndex = functionIndex
	}
	c.chunk.Classes[index] = class
	c.emitAt(OpDefineClass, stmt.Token.Line, stmt.Token.Column, index)
	return nil
}

func (c *Compiler) injectInterfaceMembers(stmt *ast.ClassStatement) error {
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
	for _, ifaceRef := range stmt.Implements {
		iface, ok := c.interfaceAST[strings.ToLower(ifaceRef.Name)]
		if !ok {
			continue
		}
		for _, def := range iface.Defaults {
			key := strings.ToLower(def.Name.Value)
			if declaredMethods[key] {
				continue
			}
			if prev, seen := defaultSource[key]; seen && prev != iface.Name.Value {
				return c.withStatementLocation(stmt, fmt.Errorf("class %s inherits multiple defaults for %s from %s and %s; class must override", stmt.Name.Value, def.Name.Value, prev, iface.Name.Value))
			}
			defaultSource[key] = iface.Name.Value
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
					return c.withStatementLocation(stmt, fmt.Errorf("class %s inherits field %s from %s (%s) and %s (%s) with conflicting types", stmt.Name.Value, field.Name.Value, prev, prevField.Type.String(), iface.Name.Value, field.Type.String()))
				}
				continue
			}
			fieldSource[key] = iface.Name.Value
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

func (c *Compiler) compileInterfaceStatement(stmt *ast.InterfaceStatement) error {
	c.interfaceAST[strings.ToLower(stmt.Name.Value)] = stmt
	index := c.declareInterface(stmt.Name.Value)
	iface := c.chunk.Interfaces[index]
	iface.Doc = stmt.Doc
	iface.TypeParameters = typeParameterNames(stmt.Generics)
	iface.Parents = iface.Parents[:0]
	iface.Methods = iface.Methods[:0]
	iface.Fields = iface.Fields[:0]
	iface.FieldTypes = iface.FieldTypes[:0]
	iface.Defaults = nil
	for _, parent := range stmt.Parents {
		if _, ok := c.interfaces[strings.ToLower(parent.Name)]; !ok {
			if !strings.Contains(parent.Name, ".") {
				return fmt.Errorf("bytecode compiler parent interface %s is not declared", parent.Name)
			}
		}
		iface.Parents = append(iface.Parents, parent.Name)
	}
	for _, method := range stmt.Methods {
		iface.Methods = append(iface.Methods, reflectFunctionMetadataFromSignature(c, method))
	}
	for _, field := range stmt.Fields {
		iface.Fields = append(iface.Fields, field.Name.Value)
		typeStr := ""
		if field.Type != nil {
			typeStr = field.Type.String()
		}
		iface.FieldTypes = append(iface.FieldTypes, typeStr)
	}
	if len(stmt.Defaults) > 0 {
		iface.Defaults = make(map[string]int64, len(stmt.Defaults))
		for _, def := range stmt.Defaults {
			fnIndex, err := c.compileInterfaceDefault(stmt.Name.Value, def)
			if err != nil {
				return err
			}
			iface.Defaults[strings.ToLower(def.Name.Value)] = fnIndex
		}
	}
	c.chunk.Interfaces[index] = iface
	return nil
}

func (c *Compiler) compileInterfaceDefault(ifaceName string, def *ast.FunctionStatement) (int64, error) {
	wrapped := *def
	wrapped.Static = false
	functionName := ifaceName + "." + def.Name.Value
	if err := c.compileFunction(&wrapped, functionName, "this"); err != nil {
		return -1, err
	}
	return c.lastFunctionIndex(functionName)
}

func containsParentConstructorCall(block *ast.BlockStatement) bool {
	if block == nil {
		return false
	}
	for _, stmt := range block.Statements {
		if statementContainsParentConstructorCall(stmt) {
			return true
		}
	}
	return false
}

func statementContainsParentConstructorCall(stmt ast.Statement) bool {
	switch stmt := stmt.(type) {
	case *ast.BlockStatement:
		return containsParentConstructorCall(stmt)
	case *ast.ExportStatement:
		return statementContainsParentConstructorCall(stmt.Statement)
	case *ast.DeclarationStatement:
		return expressionContainsParentConstructorCall(stmt.Value)
	case *ast.ExpressionStatement:
		return expressionContainsParentConstructorCall(stmt.Expression)
	case *ast.ReturnStatement:
		return expressionContainsParentConstructorCall(stmt.Value)
	case *ast.YieldStatement:
		return expressionContainsParentConstructorCall(stmt.Value)
	case *ast.SimpleStatement:
		return expressionContainsParentConstructorCall(stmt.Value)
	case *ast.IfStatement:
		if expressionContainsParentConstructorCall(stmt.Condition) || containsParentConstructorCall(stmt.Consequence) || containsParentConstructorCall(stmt.Alternative) {
			return true
		}
		for _, elseif := range stmt.ElseIfs {
			if expressionContainsParentConstructorCall(elseif.Condition) || containsParentConstructorCall(elseif.Body) {
				return true
			}
		}
	case *ast.WhileStatement:
		return expressionContainsParentConstructorCall(stmt.Condition) || containsParentConstructorCall(stmt.Body)
	case *ast.ForStatement:
		return statementContainsParentConstructorCall(stmt.Init) ||
			expressionContainsParentConstructorCall(stmt.Condition) ||
			statementContainsParentConstructorCall(stmt.Update) ||
			expressionContainsParentConstructorCall(stmt.Iterable) ||
			expressionContainsParentConstructorCall(stmt.Step) ||
			containsParentConstructorCall(stmt.Body)
	case *ast.MatchStatement:
		if expressionContainsParentConstructorCall(stmt.Expr) {
			return true
		}
		for _, matchCase := range stmt.Cases {
			if expressionContainsParentConstructorCall(matchCase.Pattern) ||
				expressionContainsParentConstructorCall(matchCase.Guard) ||
				expressionContainsParentConstructorCall(matchCase.Value) ||
				containsParentConstructorCall(matchCase.Body) {
				return true
			}
		}
	case *ast.TryStatement:
		if containsParentConstructorCall(stmt.Body) || containsParentConstructorCall(stmt.Finally) {
			return true
		}
		for _, catch := range stmt.Catches {
			if containsParentConstructorCall(catch.Body) {
				return true
			}
		}
	case *ast.WithStatement:
		if expressionContainsParentConstructorCall(stmt.Value) || containsParentConstructorCall(stmt.Body) {
			return true
		}
	}
	return false
}

func expressionContainsParentConstructorCall(expr ast.Expression) bool {
	switch expr := expr.(type) {
	case nil:
		return false
	case *ast.CallExpression:
		if ident, ok := expr.Callee.(*ast.Identifier); ok && strings.EqualFold(ident.Value, "parent") {
			return true
		}
		if expressionContainsParentConstructorCall(expr.Callee) {
			return true
		}
		for _, arg := range expr.Arguments {
			if expressionContainsParentConstructorCall(arg.Value) {
				return true
			}
		}
	case *ast.PrefixExpression:
		return expressionContainsParentConstructorCall(expr.Right)
	case *ast.PostfixExpression:
		return expressionContainsParentConstructorCall(expr.Left)
	case *ast.InfixExpression:
		return expressionContainsParentConstructorCall(expr.Left) || expressionContainsParentConstructorCall(expr.Right)
	case *ast.AssignmentExpression:
		return expressionContainsParentConstructorCall(expr.Left) || expressionContainsParentConstructorCall(expr.Value)
	case *ast.SelectorExpression:
		return expressionContainsParentConstructorCall(expr.Object)
	case *ast.IndexExpression:
		return expressionContainsParentConstructorCall(expr.Left) || expressionContainsParentConstructorCall(expr.Index)
	case *ast.ListLiteral:
		for _, element := range expr.Elements {
			if expressionContainsParentConstructorCall(element) {
				return true
			}
		}
	case *ast.DictLiteral:
		for _, entry := range expr.Entries {
			if expressionContainsParentConstructorCall(entry.Key) || expressionContainsParentConstructorCall(entry.Value) {
				return true
			}
		}
	case *ast.SetLiteral:
		for _, element := range expr.Elements {
			if expressionContainsParentConstructorCall(element) {
				return true
			}
		}
	case *ast.RangeExpression:
		return expressionContainsParentConstructorCall(expr.Start) ||
			expressionContainsParentConstructorCall(expr.End) ||
			expressionContainsParentConstructorCall(expr.Step)
	case *ast.FunctionLiteral:
		return false
	case *ast.MatchExpression:
		if expressionContainsParentConstructorCall(expr.Expr) {
			return true
		}
		for _, matchCase := range expr.Cases {
			if expressionContainsParentConstructorCall(matchCase.Pattern) ||
				expressionContainsParentConstructorCall(matchCase.Guard) ||
				expressionContainsParentConstructorCall(matchCase.Value) ||
				containsParentConstructorCall(matchCase.Body) {
				return true
			}
		}
	case *ast.SpreadExpression:
		return expressionContainsParentConstructorCall(expr.Value)
	case *ast.AwaitExpression:
		return expressionContainsParentConstructorCall(expr.Value)
	case *ast.CastExpression:
		return expressionContainsParentConstructorCall(expr.Value)
	case *ast.TernaryExpression:
		return expressionContainsParentConstructorCall(expr.Condition) ||
			expressionContainsParentConstructorCall(expr.ThenExpr) ||
			expressionContainsParentConstructorCall(expr.ElseExpr)
	}
	return false
}

func (c *Compiler) classHasSubclassOverriding(classIndex int64, methodName string) bool {
	lowered := strings.ToLower(methodName)
	for i := range c.chunk.Classes {
		if int64(i) == classIndex {
			continue
		}
		sub := c.chunk.Classes[i]
		for ancestor := sub.ParentIndex; ancestor >= 0 && int(ancestor) < len(c.chunk.Classes); {
			if ancestor == classIndex {
				if _, hasMethod := sub.Methods[lowered]; hasMethod {
					return true
				}
				break
			}
			ancestor = c.chunk.Classes[ancestor].ParentIndex
		}
	}
	return false
}

func (c *Compiler) declareClass(name string) int64 {
	key := strings.ToLower(name)
	if index, ok := c.classes[key]; ok {
		return index
	}
	index := int64(len(c.chunk.Classes))
	c.classes[key] = index
	c.chunk.Classes = append(c.chunk.Classes, ClassInfo{
		Name:            name,
		ParentIndex:     -1,
		DestructorIndex: -1,
		Methods:         map[string][]int64{},
		StaticValues:    map[string]int64{},
		StaticMethods:   map[string][]int64{},
	})
	return index
}

func (c *Compiler) declareInterface(name string) int64 {
	key := strings.ToLower(name)
	if index, ok := c.interfaces[key]; ok {
		return index
	}
	index := int64(len(c.chunk.Interfaces))
	c.interfaces[key] = index
	c.chunk.Interfaces = append(c.chunk.Interfaces, InterfaceInfo{Name: name})
	return index
}

func (c *Compiler) compileEnumStatement(stmt *ast.EnumStatement) error {
	index := c.declareEnum(stmt)
	enumDef, _ := c.chunk.Constants[index].(*runtime.EnumDef)
	if err := c.injectEnumInterfaceMethods(stmt); err != nil {
		return err
	}
	if len(stmt.Methods) > 0 && enumDef != nil {
		enumDef.MethodIndices = map[string][]int64{}
		for _, member := range stmt.Methods {
			if err := c.checkEnumMethodCollision(stmt, member.Name.Value); err != nil {
				return err
			}
			functionName := stmt.Name.Value + "." + member.Name.Value
			compiledMember := *member
			compiledMember.Static = false
			if err := c.compileFunctionWithPrologue(&compiledMember, functionName, "this", nil); err != nil {
				return c.withStatementLocation(member, err)
			}
			functionIndex, err := c.lastFunctionIndex(functionName)
			if err != nil {
				return err
			}
			key := strings.ToLower(member.Name.Value)
			enumDef.MethodIndices[key] = append(enumDef.MethodIndices[key], functionIndex)
		}
	}
	if enumDef != nil {
		for _, ifaceRef := range stmt.Implements {
			enumDef.Implements = append(enumDef.Implements, ifaceRef.Name)
		}
		if err := c.checkEnumConformance(stmt, enumDef); err != nil {
			return err
		}
	}
	c.emitAt(OpConstant, stmt.Token.Line, stmt.Token.Column, index)
	slot := c.globalSlot(stmt.Name.Value)
	c.emitAt(OpDefineGlobal, stmt.Token.Line, stmt.Token.Column, slot)
	return nil
}

// injectEnumInterfaceMethods appends an interface's default method bodies to
// the enum's method list for any method the enum leaves unimplemented.
func (c *Compiler) injectEnumInterfaceMethods(stmt *ast.EnumStatement) error {
	declared := map[string]bool{}
	for _, m := range stmt.Methods {
		declared[strings.ToLower(m.Name.Value)] = true
	}
	defaultSource := map[string]string{}
	var defaults []*ast.FunctionStatement
	for _, ifaceRef := range stmt.Implements {
		iface, ok := c.interfaceAST[strings.ToLower(ifaceRef.Name)]
		if !ok {
			continue
		}
		for _, def := range iface.Defaults {
			key := strings.ToLower(def.Name.Value)
			if declared[key] {
				continue
			}
			if prev, seen := defaultSource[key]; seen && prev != iface.Name.Value {
				return c.withStatementLocation(stmt, fmt.Errorf("enum %s inherits multiple defaults for %s from %s and %s; enum must override", stmt.Name.Value, def.Name.Value, prev, iface.Name.Value))
			}
			defaultSource[key] = iface.Name.Value
			defaults = append(defaults, def)
			declared[key] = true
		}
	}
	stmt.Methods = append(stmt.Methods, defaults...)
	return nil
}

func (c *Compiler) checkEnumMethodCollision(stmt *ast.EnumStatement, name string) error {
	switch strings.ToLower(name) {
	case "variant", "fields":
		return c.withStatementLocation(stmt, fmt.Errorf("enum %s method %q collides with a built-in variant accessor", stmt.Name.Value, name))
	case "value":
		if stmt.BackingType != nil {
			return c.withStatementLocation(stmt, fmt.Errorf("enum %s method %q collides with a built-in variant accessor", stmt.Name.Value, name))
		}
	}
	return nil
}

func (c *Compiler) checkEnumConformance(stmt *ast.EnumStatement, enumDef *runtime.EnumDef) error {
	for _, ifaceRef := range stmt.Implements {
		iface, ok := c.interfaceAST[strings.ToLower(ifaceRef.Name)]
		if !ok {
			continue
		}
		if err := c.checkEnumImplementsInterface(stmt, enumDef, iface); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) checkEnumImplementsInterface(stmt *ast.EnumStatement, enumDef *runtime.EnumDef, iface *ast.InterfaceStatement) error {
	for _, parentRef := range iface.Parents {
		if parent, ok := c.interfaceAST[strings.ToLower(parentRef.Name)]; ok {
			if err := c.checkEnumImplementsInterface(stmt, enumDef, parent); err != nil {
				return err
			}
		}
	}
	for _, sig := range iface.Methods {
		if len(enumDef.MethodIndices[strings.ToLower(sig.Name.Value)]) == 0 {
			return c.withStatementLocation(stmt, fmt.Errorf("enum %s implements %s but is missing compatible method %s", stmt.Name.Value, iface.Name.Value, sig.Name.Value))
		}
	}
	return nil
}

func (c *Compiler) declareEnum(stmt *ast.EnumStatement) int64 {
	key := strings.ToLower(stmt.Name.Value)
	if index, ok := c.enums[key]; ok {
		return index
	}
	enumDef := &runtime.EnumDef{Name: stmt.Name.Value}
	if stmt.BackingType != nil {
		enumDef.BackingType = stmt.BackingType.Name
	}
	for _, v := range stmt.Variants {
		backingValue, err := enumBackingValue(v.BackingValue)
		if err != nil {
			backingValue = nil
		}
		enumDef.Variants = append(enumDef.Variants, runtime.EnumVariantDefRuntime{
			Name:         v.Name.Value,
			FieldCount:   len(v.FieldTypes),
			BackingValue: backingValue,
		})
	}
	index := int64(len(c.chunk.Constants))
	c.chunk.Constants = append(c.chunk.Constants, enumDef)
	c.enums[key] = index
	return index
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

func (c *Compiler) declareTypeAlias(stmt *ast.TypeAliasStatement) {
	if stmt == nil || stmt.Name == nil || stmt.Type == nil {
		return
	}
	c.typeAliases[strings.ToLower(stmt.Name.Value)] = c.resolveTypeRef(stmt.Type)
}

// appendFieldDecorators grows the parallel FieldDecorators slice up
// to the given field index (filling intermediate slots with nil) and
// stores the metadata derived from the AST decorators. Keeps the
// slice length aligned with FieldNames so the encoder writes a
// matching number of entries. Argument-decoding errors are silently
// ignored to keep field declarations parseable even when the user
// hasn't yet introduced the constants their decorator args
// reference; the runtime simply sees an empty decorator list for
// that field.
// hasImmutableFieldDecorator reports whether a field is annotated `@immutable`
// (no arguments) - the set-once field marker.
func hasImmutableFieldDecorator(decorators []ast.Decorator) bool {
	for _, dec := range decorators {
		if dec.Name != nil && dec.Name.Value == "immutable" && len(dec.Arguments) == 0 {
			return true
		}
	}
	return false
}

// withoutImmutableDecorator returns decorators with the bare `@immutable`
// removed, so it is not also stored as a reflectable/callable field decorator.
func withoutImmutableDecorator(decorators []ast.Decorator) []ast.Decorator {
	out := make([]ast.Decorator, 0, len(decorators))
	for _, dec := range decorators {
		if dec.Name != nil && dec.Name.Value == "immutable" && len(dec.Arguments) == 0 {
			continue
		}
		out = append(out, dec)
	}
	return out
}

func appendFieldDecorators(existing [][]runtime.DecoratorMetadata, fieldIndex int, decorators []ast.Decorator) [][]runtime.DecoratorMetadata {
	for len(existing) <= fieldIndex {
		existing = append(existing, nil)
	}
	if len(decorators) == 0 {
		return existing
	}
	metas, err := decoratorsMetadata(decorators, "field", 0)
	if err != nil {
		return existing
	}
	existing[fieldIndex] = metas
	return existing
}

func decoratorsMetadata(decorators []ast.Decorator, target string, overload int64) ([]runtime.DecoratorMetadata, error) {
	metadata := make([]runtime.DecoratorMetadata, 0, len(decorators))
	for position, decorator := range decorators {
		item := runtime.DecoratorMetadata{
			Target:    target,
			Position:  int64(position),
			Overload:  overload,
			Line:      int64(decorator.Token.Line),
			Column:    int64(decorator.Token.Column),
			NamedArgs: map[string]runtime.Value{},
		}
		if decorator.Name != nil {
			item.Name = decorator.Name.Value
		}
		for _, arg := range decorator.Arguments {
			value, err := decoratorConstantValue(arg.Value)
			if err != nil {
				return nil, fmt.Errorf("decorator @%s: %w", item.Name, err)
			}
			if arg.Name != nil {
				item.NamedArgs[arg.Name.Value] = value
			} else {
				item.Args = append(item.Args, value)
			}
		}
		metadata = append(metadata, item)
	}
	return metadata, nil
}

func decoratorConstantValue(expr ast.Expression) (runtime.Value, error) {
	if value, err := constantValueFromExpression(expr); err == nil {
		return value, nil
	}
	switch expr := expr.(type) {
	case *ast.ListLiteral:
		values := make([]runtime.Value, 0, len(expr.Elements))
		for _, element := range expr.Elements {
			value, err := decoratorConstantValue(element)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return &runtime.List{Elements: values}, nil
	case *ast.DictLiteral:
		entries := map[string]runtime.DictEntry{}
		for _, entry := range expr.Entries {
			key, err := decoratorConstantValue(entry.Key)
			if err != nil {
				return nil, err
			}
			value, err := decoratorConstantValue(entry.Value)
			if err != nil {
				return nil, err
			}
			entries[native.DictKey(key)] = runtime.DictEntry{Key: key, Value: value}
		}
		return runtime.Dict{Entries: entries}, nil
	case *ast.SetLiteral:
		entries := map[string]runtime.SetEntry{}
		for _, element := range expr.Elements {
			value, err := decoratorConstantValue(element)
			if err != nil {
				return nil, err
			}
			entries[native.DictKey(value)] = runtime.SetEntry{Value: value}
		}
		return runtime.Set{Elements: entries}, nil
	default:
		return nil, fmt.Errorf("unsupported decorator argument expression %s", expr.String())
	}
}

func (c *Compiler) classExtendsBuiltinError(classInfo ClassInfo) bool {
	for {
		if isBuiltinErrorClass(classInfo.ParentName) {
			return true
		}
		if classInfo.ParentIndex < 0 || int(classInfo.ParentIndex) >= len(c.chunk.Classes) {
			return false
		}
		classInfo = c.chunk.Classes[classInfo.ParentIndex]
	}
}
