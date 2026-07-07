package bytecode

import (
	"fmt"
	"geblang/internal/ast"
	argbinding "geblang/internal/binding"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"strconv"
	"strings"
)

func (c *Compiler) compileExpression(expr ast.Expression) error {
	return c.compileExpressionWithExpected(expr, "")
}

func (c *Compiler) compileExpressionWithExpected(expr ast.Expression, expected string) error {
	c.expectedTypes = append(c.expectedTypes, expected)
	defer func() {
		c.expectedTypes = c.expectedTypes[:len(c.expectedTypes)-1]
	}()
	return c.compileExpressionInner(expr)
}

func (c *Compiler) compileExpressionInner(expr ast.Expression) error {
	switch expr := expr.(type) {
	case *ast.IntegerLiteral:
		value, err := runtime.NewIntLiteral(expr.Value)
		if err != nil {
			return err
		}
		if value.Value.IsInt64() {
			c.emitConstant(runtime.SmallInt{Value: value.Value.Int64()}, expr.Token.Line, expr.Token.Column)
		} else {
			c.emitConstant(value, expr.Token.Line, expr.Token.Column)
		}
		return nil
	case *ast.DecimalLiteral:
		value, err := runtime.NewDecimalLiteral(expr.Value)
		if err != nil {
			return err
		}
		c.emitConstant(value, expr.Token.Line, expr.Token.Column)
		return nil
	case *ast.FloatLiteral:
		stripped := strings.ReplaceAll(expr.Value[:len(expr.Value)-1], "_", "")
		value, err := strconv.ParseFloat(stripped, 64)
		if err != nil {
			return fmt.Errorf("invalid float literal %q", expr.Value)
		}
		c.emitConstant(runtime.Float{Value: value}, expr.Token.Line, expr.Token.Column)
		return nil
	case *ast.StringLiteral:
		c.emitConstant(runtime.String{Value: expr.Value}, expr.Token.Line, expr.Token.Column)
		return nil
	case *ast.InterpolatedString:
		if len(expr.Parts) == 0 {
			c.emitConstant(runtime.String{Value: ""}, expr.Token.Line, expr.Token.Column)
			return nil
		}
		for i, part := range expr.Parts {
			if err := c.compileExpression(part); err != nil {
				return err
			}
			_, isStr := part.(*ast.StringLiteral)
			_, isFmt := part.(*ast.FormattedInterpolation)
			if !isStr && !isFmt {
				// cast expression result to string using the same path as `as string`
				c.emitConstant(runtime.String{Value: "string"}, expr.Token.Line, expr.Token.Column)
				c.emitAt(OpCast, expr.Token.Line, expr.Token.Column)
			}
			if i > 0 {
				c.emitAt(OpAdd, expr.Token.Line, expr.Token.Column)
			}
		}
		return nil
	case *ast.FormattedInterpolation:
		if err := c.compileExpression(expr.Value); err != nil {
			return err
		}
		c.emitConstant(runtime.String{Value: expr.Spec}, expr.Token.Line, expr.Token.Column)
		c.emitAt(OpFormatSpec, expr.Token.Line, expr.Token.Column)
		return nil
	case *ast.Literal:
		switch value := expr.Value.(type) {
		case bool:
			c.emitConstant(runtime.Bool{Value: value}, expr.Token.Line, expr.Token.Column)
			return nil
		case nil:
			c.emitConstant(runtime.Null{}, expr.Token.Line, expr.Token.Column)
			return nil
		default:
			return parityErrorf("bytecode compiler does not support literal %T", value)
		}
	case *ast.ListLiteral:
		if !listHasSpread(expr.Elements) {
			for _, element := range expr.Elements {
				if err := c.compileExpression(element); err != nil {
					return err
				}
			}
			c.emitAt(OpBuildList, expr.Token.Line, expr.Token.Column, int64(len(expr.Elements)))
			return nil
		}
		segCount := int64(0)
		segStart := 0
		for i, element := range expr.Elements {
			spread, ok := element.(*ast.SpreadExpression)
			if !ok {
				continue
			}
			if i > segStart {
				for _, e := range expr.Elements[segStart:i] {
					if err := c.compileExpression(e); err != nil {
						return err
					}
				}
				c.emitAt(OpBuildList, expr.Token.Line, expr.Token.Column, int64(i-segStart))
				segCount++
			}
			if err := c.compileExpression(spread.Value); err != nil {
				return err
			}
			segCount++
			segStart = i + 1
		}
		if segStart < len(expr.Elements) {
			for _, e := range expr.Elements[segStart:] {
				if err := c.compileExpression(e); err != nil {
					return err
				}
			}
			c.emitAt(OpBuildList, expr.Token.Line, expr.Token.Column, int64(len(expr.Elements)-segStart))
			segCount++
		}
		c.emitAt(OpListConcat, expr.Token.Line, expr.Token.Column, segCount)
		return nil
	case *ast.SpreadExpression:
		return fmt.Errorf("spread expression is only valid inside a list literal or function call")

	case *ast.DictLiteral:
		if dictHasSpread(expr.Entries) {
			return c.compileExpression(desugarDictLiteralWithSpread(expr))
		}
		for _, entry := range expr.Entries {
			if err := c.compileExpression(entry.Key); err != nil {
				return err
			}
			if err := c.compileExpression(entry.Value); err != nil {
				return err
			}
		}
		c.emitAt(OpBuildDict, expr.Token.Line, expr.Token.Column, int64(len(expr.Entries)))
		return nil
	case *ast.SetLiteral:
		if setHasSpread(expr.Elements) {
			return c.compileExpression(desugarSetLiteralWithSpread(expr))
		}
		for _, element := range expr.Elements {
			if err := c.compileExpression(element); err != nil {
				return err
			}
		}
		c.emitAt(OpBuildSet, expr.Token.Line, expr.Token.Column, int64(len(expr.Elements)))
		return nil
	case *ast.PipeExpression:
		call, ok := ast.LowerPipe(expr)
		if !ok {
			return fmt.Errorf("`|>` right side must be a call, identifier, or selector")
		}
		return c.compileExpression(call)
	case *ast.PartialExpression:
		return c.compileExpression(ast.LowerPartial(expr))
	case *ast.ListComprehension:
		return c.compileExpression(desugarListComprehension(expr))
	case *ast.SetComprehension:
		return c.compileExpression(desugarSetComprehension(expr))
	case *ast.DictComprehension:
		return c.compileExpression(desugarDictComprehension(expr))
	case *ast.RangeExpression:
		if expr.Start == nil {
			c.emitConstant(runtime.Null{}, expr.Token.Line, expr.Token.Column)
		} else if err := c.compileExpression(expr.Start); err != nil {
			return err
		}
		if expr.End == nil {
			c.emitConstant(runtime.Null{}, expr.Token.Line, expr.Token.Column)
		} else if err := c.compileExpression(expr.End); err != nil {
			return err
		}
		if expr.Step == nil {
			c.emitConstant(runtime.Null{}, expr.Token.Line, expr.Token.Column)
		} else if err := c.compileExpression(expr.Step); err != nil {
			return err
		}
		if expr.Exclusive {
			c.emitAt(OpBuildRange, expr.Token.Line, expr.Token.Column, 1)
		} else {
			c.emitAt(OpBuildRange, expr.Token.Line, expr.Token.Column, 0)
		}
		return nil
	case *ast.IndexExpression:
		if err := c.compileExpression(expr.Left); err != nil {
			return err
		}
		if rng, ok := expr.Index.(*ast.RangeExpression); ok {
			if rng.Start == nil {
				c.emitConstant(runtime.Null{}, expr.Token.Line, expr.Token.Column)
			} else if err := c.compileExpression(rng.Start); err != nil {
				return err
			}
			if rng.End == nil {
				c.emitConstant(runtime.Null{}, expr.Token.Line, expr.Token.Column)
			} else if err := c.compileExpression(rng.End); err != nil {
				return err
			}
			if rng.Step == nil {
				c.emitConstant(runtime.Null{}, expr.Token.Line, expr.Token.Column)
			} else if err := c.compileExpression(rng.Step); err != nil {
				return err
			}
			if rng.Exclusive {
				c.emitAt(OpSlice, expr.Token.Line, expr.Token.Column, 1)
			} else {
				c.emitAt(OpSlice, expr.Token.Line, expr.Token.Column, 0)
			}
			return nil
		}
		if err := c.compileExpression(expr.Index); err != nil {
			return err
		}
		c.emitAt(OpIndex, expr.Token.Line, expr.Token.Column)
		return nil
	case *ast.Identifier:
		resolved, ok := c.resolveName(expr.Value)
		if !ok {
			if classIndex, found := c.classes[strings.ToLower(expr.Value)]; found && int(classIndex) < len(c.chunk.Classes) && c.chunk.Classes[classIndex].Name == expr.Value {
				classInfo := c.chunk.Classes[classIndex]
				c.emitConstant(runtime.BytecodeClass{
					Name:             classInfo.Name,
					Doc:              classInfo.Doc,
					Index:            classIndex,
					Decorators:       classInfo.Decorators,
					MethodDecorators: classInfo.MethodDecorators,
					StaticDecorators: classInfo.StaticDecorators,
				}, expr.Token.Line, expr.Token.Column)
				return nil
			}
			// A named top-level function used as a value: one overload becomes a zero-upvalue closure; several become an OverloadedFunction so call-time selection sees every overload.
			if indices, found := c.funcs[strings.ToLower(expr.Value)]; found && len(indices) > 0 {
				if len(indices) > 1 {
					c.emitAt(OpMakeOverloaded, expr.Token.Line, expr.Token.Column, indices...)
				} else {
					c.emitAt(OpMakeClosure, expr.Token.Line, expr.Token.Column, indices[0], 0)
				}
				return nil
			}
			if runtime.IsBuiltinTypeName(expr.Value) {
				c.emitConstant(runtime.Type{Name: strings.ToLower(expr.Value)}, expr.Token.Line, expr.Token.Column)
				return nil
			}
			return fmt.Errorf("unknown bytecode name %s", expr.Value)
		}
		if resolved.kind == "local" {
			c.emitAt(OpGetLocal, expr.Token.Line, expr.Token.Column, resolved.slot)
		} else {
			c.emitAt(OpGetGlobal, expr.Token.Line, expr.Token.Column, resolved.slot)
		}
		return nil
	case *ast.InfixExpression:
		if expr.Operator == "instanceof" {
			if err := c.compileExpression(expr.Left); err != nil {
				return err
			}
			typeName, err := typeNameFromExpression(expr.Right)
			if err != nil {
				return err
			}
			if target, ok := c.instanceofExactTarget(expr.RightType); ok {
				typeName = target
			}
			c.emitConstant(runtime.String{Value: typeName}, expr.Token.Line, expr.Token.Column)
			c.emitAt(OpInstanceOf, expr.Token.Line, expr.Token.Column)
			return nil
		}
		if expr.Operator == "is" || expr.Operator == "is not" {
			if err := c.compileExpression(expr.Left); err != nil {
				return err
			}
			if err := c.compileExpression(expr.Right); err != nil {
				return err
			}
			c.emitAt(OpIdentical, expr.Token.Line, expr.Token.Column)
			if expr.Operator == "is not" {
				c.emitAt(OpNot, expr.Token.Line, expr.Token.Column)
			}
			return nil
		}
		if expr.Operator == "&&" || expr.Operator == "||" {
			return c.compileShortCircuitExpression(expr)
		}
		if expr.Operator == "??" {
			return c.compileNullCoalesceExpression(expr)
		}
		if folded, ok, err := foldConstantBinary(expr.Operator, expr.Left, expr.Right); ok {
			if err != nil {
				return fmt.Errorf("%d:%d: %s", expr.Token.Line, expr.Token.Column, err.Error())
			}
			c.emitConstant(folded, expr.Token.Line, expr.Token.Column)
			return nil
		}
		bothInt := c.staticIntExpr(expr.Left) && c.staticIntExpr(expr.Right)
		bothString := !bothInt && expr.Operator == "+" && c.staticStringExpr(expr.Left) && c.staticStringExpr(expr.Right)
		// `acc + "literal"` (the common string-builder pattern): bake
		// the literal's constant index into a specialised opcode so
		// the runtime only pops one operand instead of two.
		if bothString {
			if rightLit, ok := expr.Right.(*ast.StringLiteral); ok {
				if !isStringLiteral(expr.Left) {
					if err := c.compileExpression(expr.Left); err != nil {
						return err
					}
					constIdx := int64(len(c.chunk.Constants))
					c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: rightLit.Value})
					c.emitAt(OpAddStringConst, expr.Token.Line, expr.Token.Column, constIdx)
					return nil
				}
			}
		}
		if err := c.compileExpression(expr.Left); err != nil {
			return err
		}
		if err := c.compileExpression(expr.Right); err != nil {
			return err
		}
		switch expr.Operator {
		case "in":
			c.emitAt(OpContains, expr.Token.Line, expr.Token.Column)
		case "+":
			if bothInt {
				c.emitAt(OpAddInt, expr.Token.Line, expr.Token.Column)
			} else if bothString {
				c.emitAt(OpAddString, expr.Token.Line, expr.Token.Column)
			} else {
				c.emitAt(OpAdd, expr.Token.Line, expr.Token.Column)
			}
		case "-":
			if bothInt {
				c.emitAt(OpSubInt, expr.Token.Line, expr.Token.Column)
			} else {
				c.emitAt(OpSub, expr.Token.Line, expr.Token.Column)
			}
		case "*":
			if bothInt {
				c.emitAt(OpMulInt, expr.Token.Line, expr.Token.Column)
			} else {
				c.emitAt(OpMul, expr.Token.Line, expr.Token.Column)
			}
		case "/":
			c.emitAt(OpDiv, expr.Token.Line, expr.Token.Column)
		case "//":
			c.emitAt(OpIntDiv, expr.Token.Line, expr.Token.Column)
		case "%":
			if bothInt {
				c.emitAt(OpModInt, expr.Token.Line, expr.Token.Column)
			} else {
				c.emitAt(OpMod, expr.Token.Line, expr.Token.Column)
			}
		case "**":
			c.emitAt(OpPow, expr.Token.Line, expr.Token.Column)
		case "==":
			if bothInt {
				c.emitAt(OpEqualInt, expr.Token.Line, expr.Token.Column)
			} else {
				c.emitAt(OpEqual, expr.Token.Line, expr.Token.Column)
			}
		case "!=":
			if bothInt {
				c.emitAt(OpEqualInt, expr.Token.Line, expr.Token.Column)
			} else {
				c.emitAt(OpEqual, expr.Token.Line, expr.Token.Column)
			}
			c.emitAt(OpNot, expr.Token.Line, expr.Token.Column)
		case "<":
			if bothInt {
				c.emitAt(OpLessInt, expr.Token.Line, expr.Token.Column)
			} else {
				c.emitAt(OpLess, expr.Token.Line, expr.Token.Column)
			}
		case "<=":
			if bothInt {
				c.emitAt(OpLessEqualInt, expr.Token.Line, expr.Token.Column)
			} else {
				c.emitAt(OpLessEqual, expr.Token.Line, expr.Token.Column)
			}
		case ">":
			if bothInt {
				c.emitAt(OpGreaterInt, expr.Token.Line, expr.Token.Column)
			} else {
				c.emitAt(OpGreater, expr.Token.Line, expr.Token.Column)
			}
		case ">=":
			if bothInt {
				c.emitAt(OpGreaterEqualInt, expr.Token.Line, expr.Token.Column)
			} else {
				c.emitAt(OpGreaterEqual, expr.Token.Line, expr.Token.Column)
			}
		case "xor":
			c.emitAt(OpBoolXor, expr.Token.Line, expr.Token.Column)
		case "&":
			c.emitAt(OpBitAnd, expr.Token.Line, expr.Token.Column)
		case "|":
			c.emitAt(OpBitOr, expr.Token.Line, expr.Token.Column)
		case "^":
			c.emitAt(OpBitXor, expr.Token.Line, expr.Token.Column)
		case "<<":
			c.emitAt(OpLShift, expr.Token.Line, expr.Token.Column)
		case ">>":
			c.emitAt(OpRShift, expr.Token.Line, expr.Token.Column)
		default:
			return parityErrorf("bytecode compiler does not support operator %q", expr.Operator)
		}
		return nil
	case *ast.PrefixExpression:
		if err := c.compileExpression(expr.Right); err != nil {
			return err
		}
		switch expr.Operator {
		case "!":
			c.emitAt(OpNot, expr.Token.Line, expr.Token.Column)
		case "-":
			c.emitAt(OpNegate, expr.Token.Line, expr.Token.Column)
		case "~":
			c.emitAt(OpBitNot, expr.Token.Line, expr.Token.Column)
		default:
			return parityErrorf("bytecode compiler does not support prefix operator %q", expr.Operator)
		}
		return nil
	case *ast.PostfixExpression:
		ident, ok := expr.Left.(*ast.Identifier)
		if !ok {
			return fmt.Errorf("bytecode compiler only supports identifier postfix increments")
		}
		resolved, ok := c.resolveName(ident.Value)
		if !ok {
			return fmt.Errorf("unknown bytecode name %s", ident.Value)
		}
		if resolved.typ == "int" {
			switch expr.Operator {
			case "++":
				if resolved.kind == "local" {
					c.emitAt(OpIncLocalInt, expr.Token.Line, expr.Token.Column, resolved.slot)
				} else {
					c.emitAt(OpIncGlobalInt, expr.Token.Line, expr.Token.Column, resolved.slot)
				}
				return nil
			case "--":
				if resolved.kind == "local" {
					c.emitAt(OpDecLocalInt, expr.Token.Line, expr.Token.Column, resolved.slot)
				} else {
					c.emitAt(OpDecGlobalInt, expr.Token.Line, expr.Token.Column, resolved.slot)
				}
				return nil
			}
		}
		if resolved.kind == "local" {
			c.emitAt(OpGetLocal, expr.Token.Line, expr.Token.Column, resolved.slot)
		} else {
			c.emitAt(OpGetGlobal, expr.Token.Line, expr.Token.Column, resolved.slot)
		}
		c.emitAt(OpDup, expr.Token.Line, expr.Token.Column)
		c.emitConstant(runtime.SmallInt{Value: 1}, expr.Token.Line, expr.Token.Column)
		switch expr.Operator {
		case "++":
			c.emitAt(OpAdd, expr.Token.Line, expr.Token.Column)
		case "--":
			c.emitAt(OpSub, expr.Token.Line, expr.Token.Column)
		default:
			return parityErrorf("bytecode compiler does not support postfix operator %q", expr.Operator)
		}
		if resolved.kind == "local" {
			c.emitAt(OpSetLocal, expr.Token.Line, expr.Token.Column, resolved.slot)
		} else {
			c.emitAt(OpSetGlobal, expr.Token.Line, expr.Token.Column, resolved.slot)
		}
		c.emitAt(OpPop, expr.Token.Line, expr.Token.Column)
		return nil
	case *ast.AssignmentExpression:
		return c.compileAssignmentExpression(expr)
	case *ast.CallExpression:
		if ident, ok := expr.Callee.(*ast.Identifier); ok {
			if strings.EqualFold(ident.Value, "parent") {
				if len(c.classStack) == 0 {
					return fmt.Errorf("parent is only available inside class methods")
				}
				currentClass := c.chunk.Classes[c.classStack[len(c.classStack)-1]]
				isBuiltinErrorParent := isBuiltinErrorClass(currentClass.ParentName) && (currentClass.ParentIndex < 0 || int(currentClass.ParentIndex) >= len(c.chunk.Classes))
				isCrossModuleParent := !isBuiltinErrorParent && (currentClass.ParentIndex < 0 || int(currentClass.ParentIndex) >= len(c.chunk.Classes)) && strings.Contains(currentClass.ParentName, ".")
				if !isBuiltinErrorParent && !isCrossModuleParent && (currentClass.ParentIndex < 0 || int(currentClass.ParentIndex) >= len(c.chunk.Classes)) {
					return fmt.Errorf("%s has no parent class", currentClass.Name)
				}
				var orderedArgs []ast.Expression
				if !isBuiltinErrorParent && !isCrossModuleParent {
					parent := c.chunk.Classes[currentClass.ParentIndex]
					if len(parent.ConstructorIndices) > 0 {
						var err error
						_, orderedArgs, err = c.selectFunctionIndicesCallIgnoringReturn(parent.Name, parent.ConstructorIndices, expr.Arguments, 1)
						if err != nil {
							return err
						}
					} else {
						orderedArgs = positionalArguments(expr.Arguments)
						if orderedArgs == nil && len(expr.Arguments) > 0 {
							return fmt.Errorf("%s constructor does not accept named arguments", parent.Name)
						}
					}
				} else {
					// Cross-module or builtin-error parent: positional only at
					// compile time. Overload selection (cross-module) happens
					// at runtime via the module loader.
					orderedArgs = positionalArguments(expr.Arguments)
					if orderedArgs == nil && len(expr.Arguments) > 0 {
						return fmt.Errorf("%s constructor does not accept named arguments", currentClass.ParentName)
					}
				}
				resolved, ok := c.resolveName("this")
				if !ok {
					return fmt.Errorf("parent constructor is only available inside instance methods")
				}
				if resolved.kind == "local" {
					c.emitAt(OpGetLocal, expr.Token.Line, expr.Token.Column, resolved.slot)
				} else {
					c.emitAt(OpGetGlobal, expr.Token.Line, expr.Token.Column, resolved.slot)
				}
				for _, arg := range orderedArgs {
					if err := c.compileExpression(arg); err != nil {
						return err
					}
				}
				c.emitAt(OpCallParentConstructor, expr.Token.Line, expr.Token.Column, c.classStack[len(c.classStack)-1], int64(len(orderedArgs)))
				return nil
			}
			if strings.EqualFold(ident.Value, "dir") {
				if len(expr.Arguments) != 1 || expr.Arguments[0].Name != nil {
					return fmt.Errorf("dir(value) expects one positional argument; dir() scope introspection is evaluator/REPL-only")
				}
				// A pure-native module alias is not a VM value, so emit its
				// member list as a compile-time string list. Source-backed
				// aliases ARE global module values: fall through to OpDir so
				// their runtime Exports (the external surface) are listed.
				if arg, ok := expr.Arguments[0].Value.(*ast.Identifier); ok && c.nativeSymbols != nil && !c.sourceModuleAliases[arg.Value] {
					if canonical, isModule := c.moduleAliases[arg.Value]; isModule {
						if names := native.ModuleDirNames(canonical, c.nativeSymbols); names != nil {
							for _, name := range names {
								c.emitConstant(runtime.String{Value: name}, expr.Token.Line, expr.Token.Column)
							}
							c.emitAt(OpBuildList, expr.Token.Line, expr.Token.Column, int64(len(names)))
							return nil
						}
					}
				}
				if err := c.compileExpression(expr.Arguments[0].Value); err != nil {
					return err
				}
				c.emitAt(OpDir, expr.Token.Line, expr.Token.Column)
				return nil
			}
			if strings.EqualFold(ident.Value, "dump") {
				if len(expr.Arguments) != 1 || expr.Arguments[0].Name != nil {
					return fmt.Errorf("dump expects exactly one positional argument")
				}
				if err := c.compileExpression(expr.Arguments[0].Value); err != nil {
					return err
				}
				c.emitAt(OpDump, expr.Token.Line, expr.Token.Column)
				return nil
			}
			if strings.EqualFold(ident.Value, "typeof") {
				if len(expr.Arguments) != 1 || expr.Arguments[0].Name != nil {
					return fmt.Errorf("typeof expects exactly one positional argument")
				}
				if err := c.compileExpression(expr.Arguments[0].Value); err != nil {
					return err
				}
				c.emitAt(OpTypeOf, expr.Token.Line, expr.Token.Column)
				return nil
			}
			if ident.Value == "assert" {
				return c.compileAssertCall(expr)
			}
			if ident.Value == "range" {
				if len(expr.Arguments) < 2 || len(expr.Arguments) > 3 {
					return fmt.Errorf("range expects (start, end) or (start, end, step)")
				}
				for _, arg := range expr.Arguments {
					if arg.Name != nil {
						return fmt.Errorf("range does not accept named arguments")
					}
					if err := c.compileExpression(arg.Value); err != nil {
						return err
					}
				}
				c.emitAt(OpRange, expr.Token.Line, expr.Token.Column, int64(len(expr.Arguments)))
				return nil
			}
			if ident.Value == "zrange" {
				if len(expr.Arguments) < 1 || len(expr.Arguments) > 3 {
					return fmt.Errorf("zrange expects (n), (start, end), or (start, end, step)")
				}
				for _, arg := range expr.Arguments {
					if arg.Name != nil {
						return fmt.Errorf("zrange does not accept named arguments")
					}
					if err := c.compileExpression(arg.Value); err != nil {
						return err
					}
				}
				c.emitAt(OpZRange, expr.Token.Line, expr.Token.Column, int64(len(expr.Arguments)))
				return nil
			}
			if isBuiltinErrorClass(ident.Value) {
				if len(expr.Arguments) > 1 {
					return fmt.Errorf("%s expects zero or one argument", ident.Value)
				}
				for _, arg := range expr.Arguments {
					if arg.Name != nil && !strings.EqualFold(arg.Name.Value, "message") {
						return fmt.Errorf("%s has no parameter %s", ident.Value, arg.Name.Value)
					}
					if err := c.compileExpression(arg.Value); err != nil {
						return err
					}
				}
				classIndex := int64(len(c.chunk.Constants))
				c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: ident.Value})
				c.emitAt(OpMakeError, expr.Token.Line, expr.Token.Column, classIndex, int64(len(expr.Arguments)))
				return nil
			}
			/* Class dispatch is case-sensitive at the call site: when
			 * `view(...)` is called we must not bind to a `View`
			 * class. The compiler's internal storage keys are
			 * lowercased but the language distinguishes case. */
			if classIndex, ok := c.classes[strings.ToLower(ident.Value)]; ok && c.chunk.Classes[classIndex].Name == ident.Value {
				classInfo := c.chunk.Classes[classIndex]
				if c.classExtendsBuiltinError(classInfo) && len(classInfo.FieldNames) == 0 && len(classInfo.ConstructorIndices) == 0 {
					if len(expr.Arguments) > 1 {
						return fmt.Errorf("%s expects zero or one argument", ident.Value)
					}
					for _, arg := range expr.Arguments {
						if arg.Name != nil && !strings.EqualFold(arg.Name.Value, "message") {
							return fmt.Errorf("%s has no parameter %s", ident.Value, arg.Name.Value)
						}
						if err := c.compileExpression(arg.Value); err != nil {
							return err
						}
					}
					classNameIndex := int64(len(c.chunk.Constants))
					c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: classInfo.Name})
					c.emitAt(OpMakeError, expr.Token.Line, expr.Token.Column, classNameIndex, int64(len(expr.Arguments)))
					return nil
				}
				if _, hasSpread := callSpreadIndex(expr.Arguments); hasSpread {
					if len(classInfo.ConstructorIndices) > 1 {
						return fmt.Errorf("cannot use spread with overloaded constructor %s", classInfo.Name)
					}
					meta, err := c.compileSpreadCallArguments(expr.Arguments)
					if err != nil {
						return err
					}
					operands := append([]int64{classIndex, int64(len(expr.Arguments))}, meta...)
					c.emitAt(OpConstructClassSpread, expr.Token.Line, expr.Token.Column, operands...)
					return nil
				}
				var orderedArgs []ast.Expression
				var functionIndex int64
				if len(classInfo.ConstructorIndices) > 0 {
					var bindings map[string]string
					if len(expr.TypeArguments) > 0 && len(classInfo.TypeParameters) > 0 {
						bindings = map[string]string{}
						for i, arg := range expr.TypeArguments {
							if i >= len(classInfo.TypeParameters) {
								break
							}
							if arg != nil && arg.Operator == "" && arg.Name != "" {
								bindings[classInfo.TypeParameters[i]] = arg.Name
							}
						}
					}
					var err error
					functionIndex, orderedArgs, err = c.selectFunctionIndicesCallBound(classInfo.Name, classInfo.ConstructorIndices, expr.Arguments, 1, false, bindings)
					if err != nil {
						return err
					}
				} else {
					orderedArgs = positionalArguments(expr.Arguments)
					if orderedArgs == nil && len(expr.Arguments) > 0 {
						return fmt.Errorf("%s constructor does not accept named arguments", classInfo.Name)
					}
				}
				if len(classInfo.ConstructorIndices) > 0 {
					if err := c.compileOrderedArguments(c.chunk.Functions[functionIndex], orderedArgs, 1, expr.Token.Line, expr.Token.Column); err != nil {
						return err
					}
				} else {
					for _, arg := range orderedArgs {
						if err := c.compileExpression(arg); err != nil {
							return err
						}
					}
				}
				// Plant explicit `<TypeArgs>` before construction so the
				// constructor's argument validation sees the caller's bindings.
				c.emitPlantCallTypeBindings(expr, classInfo.TypeParameters)
				c.emitAt(OpConstructClass, expr.Token.Line, expr.Token.Column, classIndex, int64(len(orderedArgs)))
				// Explicit type arguments on the call site (e.g. `Container<T>(...)`)
				// pin the reified bindings on the freshly-constructed instance, so
				// runtime invariance checks against a typed parameter see the
				// caller's intent rather than only those bindings inferred from
				// constructor argument types.
				if len(expr.TypeArguments) > 0 && len(classInfo.TypeParameters) > 0 {
					operands := []int64{0}
					count := int64(0)
					for i, arg := range expr.TypeArguments {
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
						c.emitAt(OpSetTypeBindings, expr.Token.Line, expr.Token.Column, operands...)
					}
				}
				return nil
			}
			if resolved, ok := c.resolveName(ident.Value); ok {
				if resolved.kind == "local" {
					c.emitAt(OpGetLocal, expr.Token.Line, expr.Token.Column, resolved.slot)
				} else {
					c.emitAt(OpGetGlobal, expr.Token.Line, expr.Token.Column, resolved.slot)
				}
				if _, hasSpread := callSpreadIndex(expr.Arguments); hasSpread {
					meta, err := c.compileSpreadCallArguments(expr.Arguments)
					if err != nil {
						return err
					}
					nameIndex := int64(len(c.chunk.Constants))
					c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "__invoke"})
					operands := append([]int64{nameIndex, int64(len(expr.Arguments))}, meta...)
					c.emitAt(OpMethodCallSpread, expr.Token.Line, expr.Token.Column, operands...)
					return nil
				}
				hasNamedArgs := false
				for _, arg := range expr.Arguments {
					hasNamedArgs = hasNamedArgs || arg.Name != nil
					if err := c.compileExpression(arg.Value); err != nil {
						return err
					}
				}
				nameIndex := int64(len(c.chunk.Constants))
				c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "__invoke"})
				if hasNamedArgs {
					operands := []int64{nameIndex, int64(len(expr.Arguments))}
					for _, arg := range expr.Arguments {
						argNameIndex := int64(-1)
						if arg.Name != nil {
							argNameIndex = int64(len(c.chunk.Constants))
							c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: arg.Name.Value})
						}
						operands = append(operands, argNameIndex)
					}
					c.emitAt(OpMethodCallNamed, expr.Token.Line, expr.Token.Column, operands...)
					return nil
				}
				c.emitAt(OpMethodCall, expr.Token.Line, expr.Token.Column, nameIndex, int64(len(expr.Arguments)))
				return nil
			}
			if _, hasSpread := callSpreadIndex(expr.Arguments); hasSpread {
				index, err := c.selectFunctionCallSpread(ident.Value)
				if err != nil {
					return err
				}
				meta, err := c.compileSpreadCallArguments(expr.Arguments)
				if err != nil {
					return err
				}
				operands := append([]int64{index, int64(len(expr.Arguments))}, meta...)
				c.emitAt(OpCallSpread, expr.Token.Line, expr.Token.Column, operands...)
				return nil
			}
			index, orderedArgs, err := c.selectFunctionCall(ident.Value, expr.Arguments, 0)
			if err != nil {
				return err
			}
			if err := c.compileOrderedArguments(c.chunk.Functions[index], orderedArgs, 0, expr.Token.Line, expr.Token.Column); err != nil {
				return err
			}
			c.emitPlantCallTypeBindings(expr, c.chunk.Functions[index].TypeParameters)
			c.emitAt(OpCall, expr.Token.Line, expr.Token.Column, index, int64(len(orderedArgs)))
			return nil
		}
		if selector, ok := expr.Callee.(*ast.SelectorExpression); ok && selector.Parenthesized {
			/* `(obj.fn)(args)` invokes the value of obj.fn rather
			 * than dispatching as a method call on obj. Compile
			 * the selector as a value, push args, OpMethodCall
			 * __invoke. */
			if _, hasSpread := callSpreadIndex(expr.Arguments); hasSpread {
				if err := c.compileExpression(expr.Callee); err != nil {
					return err
				}
				meta, err := c.compileSpreadCallArguments(expr.Arguments)
				if err != nil {
					return err
				}
				nameIndex := int64(len(c.chunk.Constants))
				c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "__invoke"})
				operands := append([]int64{nameIndex, int64(len(expr.Arguments))}, meta...)
				c.emitAt(OpMethodCallSpread, expr.Token.Line, expr.Token.Column, operands...)
				return nil
			}
			if err := c.compileExpression(expr.Callee); err != nil {
				return err
			}
			hasNamedArgs := false
			for _, arg := range expr.Arguments {
				hasNamedArgs = hasNamedArgs || arg.Name != nil
				if err := c.compileExpression(arg.Value); err != nil {
					return err
				}
			}
			nameIndex := int64(len(c.chunk.Constants))
			c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "__invoke"})
			if hasNamedArgs {
				operands := []int64{nameIndex, int64(len(expr.Arguments))}
				for _, arg := range expr.Arguments {
					argNameIndex := int64(-1)
					if arg.Name != nil {
						argNameIndex = int64(len(c.chunk.Constants))
						c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: arg.Name.Value})
					}
					operands = append(operands, argNameIndex)
				}
				c.emitAt(OpMethodCallNamed, expr.Token.Line, expr.Token.Column, operands...)
				return nil
			}
			c.emitAt(OpMethodCall, expr.Token.Line, expr.Token.Column, nameIndex, int64(len(expr.Arguments)))
			return nil
		}
		if selector, ok := expr.Callee.(*ast.SelectorExpression); ok {
			if object, ok := selector.Object.(*ast.Identifier); ok && strings.EqualFold(object.Value, "parent") {
				if len(c.classStack) == 0 {
					return fmt.Errorf("parent is only available inside class methods")
				}
				currentClass := c.chunk.Classes[c.classStack[len(c.classStack)-1]]
				isCrossModuleParent := (currentClass.ParentIndex < 0 || int(currentClass.ParentIndex) >= len(c.chunk.Classes)) && strings.Contains(currentClass.ParentName, ".")
				if !isCrossModuleParent && (currentClass.ParentIndex < 0 || int(currentClass.ParentIndex) >= len(c.chunk.Classes)) {
					return fmt.Errorf("%s has no parent class", currentClass.Name)
				}
				var orderedArgs []ast.Expression
				crossModuleAncestor := ""
				if !isCrossModuleParent {
					parent := c.chunk.Classes[currentClass.ParentIndex]
					indices, ok := c.lookupMethod(parent, selector.Name.Value)
					if !ok {
						// Method may live in a cross-module ancestor above this
						// same-chunk parent; defer overload selection to runtime.
						if qualified, boundary := c.crossModuleBoundary(parent); boundary {
							crossModuleAncestor = qualified
						} else {
							return fmt.Errorf("unknown parent method %s.%s", parent.Name, selector.Name.Value)
						}
					} else {
						var err error
						_, orderedArgs, err = c.selectFunctionIndicesCall(selector.Name.Value, indices, expr.Arguments, 1)
						if err != nil {
							return err
						}
					}
				}
				if isCrossModuleParent || crossModuleAncestor != "" {
					// Cross-module parent method: positional args; overload
					// selection runs in the parent module's VM at runtime.
					orderedArgs = positionalArguments(expr.Arguments)
					if orderedArgs == nil && len(expr.Arguments) > 0 {
						ancestor := currentClass.ParentName
						if crossModuleAncestor != "" {
							ancestor = crossModuleAncestor
						}
						return fmt.Errorf("parent method %s.%s does not accept named arguments", ancestor, selector.Name.Value)
					}
				}
				resolved, ok := c.resolveName("this")
				if !ok {
					return fmt.Errorf("parent method is only available inside instance methods")
				}
				if resolved.kind == "local" {
					c.emitAt(OpGetLocal, expr.Token.Line, expr.Token.Column, resolved.slot)
				} else {
					c.emitAt(OpGetGlobal, expr.Token.Line, expr.Token.Column, resolved.slot)
				}
				for _, arg := range orderedArgs {
					if err := c.compileExpression(arg); err != nil {
						return err
					}
				}
				nameIndex := int64(len(c.chunk.Constants))
				c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: selector.Name.Value})
				c.emitAt(OpCallParentMethod, expr.Token.Line, expr.Token.Column, c.classStack[len(c.classStack)-1], nameIndex, int64(len(orderedArgs)))
				return nil
			}
			if object, ok := selector.Object.(*ast.Identifier); ok {
				if _, resolvedName := c.resolveName(object.Value); !resolvedName {
					if classIndex, ok := c.classes[strings.ToLower(object.Value)]; ok && c.chunk.Classes[classIndex].Name == object.Value {
						indices, ok := c.lookupStaticMethod(c.chunk.Classes[classIndex], selector.Name.Value)
						if _, hasSpread := callSpreadIndex(expr.Arguments); hasSpread {
							if ok && len(indices) > 1 {
								return fmt.Errorf("cannot use spread with overloaded static method %s", selector.Name.Value)
							}
							meta, err := c.compileSpreadCallArguments(expr.Arguments)
							if err != nil {
								return err
							}
							nameIndex := int64(len(c.chunk.Constants))
							c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: selector.Name.Value})
							operands := append([]int64{classIndex, nameIndex, int64(len(expr.Arguments))}, meta...)
							c.emitAt(OpCallStaticMethodSpread, expr.Token.Line, expr.Token.Column, operands...)
							return nil
						}
						var orderedArgs []ast.Expression
						var functionIndex int64
						var err error
						if ok {
							functionIndex, orderedArgs, err = c.selectFunctionIndicesCall(selector.Name.Value, indices, expr.Arguments, 0)
							if err != nil {
								return err
							}
							if err := c.compileOrderedArguments(c.chunk.Functions[functionIndex], orderedArgs, 0, expr.Token.Line, expr.Token.Column); err != nil {
								return err
							}
						} else {
							orderedArgs = make([]ast.Expression, 0, len(expr.Arguments))
							for _, arg := range expr.Arguments {
								orderedArgs = append(orderedArgs, arg.Value)
							}
							for _, arg := range orderedArgs {
								if err := c.compileExpression(arg); err != nil {
									return err
								}
							}
						}
						nameIndex := int64(len(c.chunk.Constants))
						c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: selector.Name.Value})
						c.emitAt(OpCallStaticMethod, expr.Token.Line, expr.Token.Column, classIndex, nameIndex, int64(len(orderedArgs)))
						return nil
					}
					if enumConstIndex, ok := c.enums[strings.ToLower(object.Value)]; ok {
						c.emitAt(OpConstant, expr.Token.Line, expr.Token.Column, enumConstIndex)
						for _, arg := range expr.Arguments {
							if err := c.compileExpression(arg.Value); err != nil {
								return err
							}
						}
						nameIndex := int64(len(c.chunk.Constants))
						c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: selector.Name.Value})
						c.emitAt(OpMethodCall, expr.Token.Line, expr.Token.Column, nameIndex, int64(len(expr.Arguments)))
						return nil
					}
				}
			}
			if module, name, ok := selectorName(expr.Callee); ok {
				canonical := c.canonicalModule(module)
				if isBytecodeCallableModule(canonical) && !isDualNameSourceModule(canonical) {
					// resolveName uses the *original* identifier so a local
					// variable shadowing the alias still wins over module
					// dispatch (the established precedence).
					if _, resolved := c.resolveName(module); !resolved {
						return c.compileBuiltinCall(expr, canonical, name)
					}
				}
			}
			hasNamedMethodArgs := false
			for _, arg := range expr.Arguments {
				if arg.Name != nil {
					hasNamedMethodArgs = true
					break
				}
			}
			_, hasSpreadMethodArg := callSpreadIndex(expr.Arguments)
			if !selector.Optional && !hasNamedMethodArgs && !hasSpreadMethodArg {
				if fnIndex, ok := c.staticallyResolveMethodCall(selector, expr.Arguments); ok {
					if err := c.compileExpression(selector.Object); err != nil {
						return err
					}
					for _, arg := range expr.Arguments {
						if err := c.compileExpression(arg.Value); err != nil {
							return err
						}
					}
					c.emitPlantCallTypeBindings(expr, c.chunk.Functions[fnIndex].TypeParameters)
					c.emitAt(OpCallResolvedMethod, expr.Token.Line, expr.Token.Column, fnIndex, int64(len(expr.Arguments)))
					return nil
				}
			}
			if err := c.compileExpression(selector.Object); err != nil {
				return err
			}
			var optionalJump int
			if selector.Optional {
				optionalJump = c.emitJump(OpOptionalChain, expr.Token.Line, expr.Token.Column)
			}
			if _, hasSpread := callSpreadIndex(expr.Arguments); hasSpread {
				meta, err := c.compileSpreadCallArguments(expr.Arguments)
				if err != nil {
					return err
				}
				nameIndex := int64(len(c.chunk.Constants))
				c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: selector.Name.Value})
				operands := append([]int64{nameIndex, int64(len(expr.Arguments))}, meta...)
				c.emitAt(OpMethodCallSpread, expr.Token.Line, expr.Token.Column, operands...)
				if selector.Optional {
					c.patchJump(optionalJump)
				}
				return nil
			}
			for _, arg := range expr.Arguments {
				if err := c.compileExpression(arg.Value); err != nil {
					return err
				}
			}
			nameIndex := int64(len(c.chunk.Constants))
			c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: selector.Name.Value})
			if hasNamedMethodArgs {
				operands := []int64{nameIndex, int64(len(expr.Arguments))}
				for _, arg := range expr.Arguments {
					argNameIndex := int64(-1)
					if arg.Name != nil {
						argNameIndex = int64(len(c.chunk.Constants))
						c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: arg.Name.Value})
					}
					operands = append(operands, argNameIndex)
				}
				c.emitAt(OpMethodCallNamed, expr.Token.Line, expr.Token.Column, operands...)
				if selector.Optional {
					c.patchJump(optionalJump)
				}
				return nil
			}
			c.emitPlantCallTypeArgs(expr)
			c.emitAt(OpMethodCall, expr.Token.Line, expr.Token.Column, nameIndex, int64(len(expr.Arguments)))
			if selector.Optional {
				c.patchJump(optionalJump)
			}
			return nil
		}
		if _, hasSpread := callSpreadIndex(expr.Arguments); hasSpread {
			if err := c.compileExpression(expr.Callee); err != nil {
				return err
			}
			meta, err := c.compileSpreadCallArguments(expr.Arguments)
			if err != nil {
				return err
			}
			nameIndex := int64(len(c.chunk.Constants))
			c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "__invoke"})
			operands := append([]int64{nameIndex, int64(len(expr.Arguments))}, meta...)
			c.emitAt(OpMethodCallSpread, expr.Token.Line, expr.Token.Column, operands...)
			return nil
		}
		if err := c.compileExpression(expr.Callee); err != nil {
			return err
		}
		hasNamedArgs := false
		for _, arg := range expr.Arguments {
			hasNamedArgs = hasNamedArgs || arg.Name != nil
			if err := c.compileExpression(arg.Value); err != nil {
				return err
			}
		}
		nameIndex := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "__invoke"})
		if hasNamedArgs {
			operands := []int64{nameIndex, int64(len(expr.Arguments))}
			for _, arg := range expr.Arguments {
				argNameIndex := int64(-1)
				if arg.Name != nil {
					argNameIndex = int64(len(c.chunk.Constants))
					c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: arg.Name.Value})
				}
				operands = append(operands, argNameIndex)
			}
			c.emitAt(OpMethodCallNamed, expr.Token.Line, expr.Token.Column, operands...)
			return nil
		}
		c.emitAt(OpMethodCall, expr.Token.Line, expr.Token.Column, nameIndex, int64(len(expr.Arguments)))
		return nil
	case *ast.SelectorExpression:
		if expr.Name.Value == "type" && !expr.Optional {
			if ident, ok := expr.Object.(*ast.Identifier); ok {
				if _, resolvedName := c.resolveName(ident.Value); !resolvedName {
					c.emitConstant(runtime.Type{Name: ident.Value}, expr.Token.Line, expr.Token.Column)
					return nil
				}
			}
			// Resolved variable or non-identifier: evaluate and get type at runtime.
			if err := c.compileExpression(expr.Object); err != nil {
				return err
			}
			c.emitAt(OpTypeOf, expr.Token.Line, expr.Token.Column)
			return nil
		}
		if !expr.Optional {
			if object, ok := expr.Object.(*ast.Identifier); ok {
				if _, resolvedName := c.resolveName(object.Value); !resolvedName {
					if classIndex, ok := c.classes[strings.ToLower(object.Value)]; ok && c.chunk.Classes[classIndex].Name == object.Value {
						nameIndex := int64(len(c.chunk.Constants))
						c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: expr.Name.Value})
						c.emitAt(OpGetStaticValue, expr.Token.Line, expr.Token.Column, classIndex, nameIndex)
						return nil
					}
					if enumConstIndex, ok := c.enums[strings.ToLower(object.Value)]; ok {
						c.emitAt(OpConstant, expr.Token.Line, expr.Token.Column, enumConstIndex)
						nameIndex := int64(len(c.chunk.Constants))
						c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: expr.Name.Value})
						c.emitAt(OpGetField, expr.Token.Line, expr.Token.Column, nameIndex)
						return nil
					}
					// An imported native module's function as a first-class value
					// (math.abs without calling it). Native modules are not runtime
					// values, so emit a dedicated push.
					if _, imported := c.moduleAliases[object.Value]; imported {
						canonical := c.canonicalModule(object.Value)
						if native.IsPureBuiltin(canonical, expr.Name.Value) {
							canonicalIdx := int64(len(c.chunk.Constants))
							c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: canonical})
							nameIdx := int64(len(c.chunk.Constants))
							c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: expr.Name.Value})
							c.emitAt(OpNativeValue, expr.Token.Line, expr.Token.Column, canonicalIdx, nameIdx)
							return nil
						}
					}
				}
			}
		}
		if err := c.compileExpression(expr.Object); err != nil {
			return err
		}
		nameIndex := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: expr.Name.Value})
		if expr.Optional {
			endJump := c.emitJump(OpOptionalChain, expr.Token.Line, expr.Token.Column)
			c.emitAt(OpGetField, expr.Token.Line, expr.Token.Column, nameIndex)
			c.patchJump(endJump)
		} else {
			c.emitAt(OpGetField, expr.Token.Line, expr.Token.Column, nameIndex)
		}
		return nil
	case *ast.CastExpression:
		if err := c.compileExpression(expr.Value); err != nil {
			return err
		}
		c.emitConstant(runtime.String{Value: c.bytecodeTypeName(expr.Type)}, expr.Token.Line, expr.Token.Column)
		c.emitAt(OpCast, expr.Token.Line, expr.Token.Column)
		return nil
	case *ast.MatchExpression:
		return c.compileMatchExpression(expr)
	case *ast.FunctionLiteral:
		return c.compileFunctionLiteral(expr)
	case *ast.AwaitExpression:
		if err := c.compileExpression(expr.Value); err != nil {
			return err
		}
		c.emitAt(OpAwait, expr.Token.Line, expr.Token.Column)
		return nil
	case *ast.TernaryExpression:
		return c.compileTernaryExpression(expr)
	default:
		return parityErrorf("bytecode compiler does not support %T yet", expr)
	}
}

func (c *Compiler) compileTernaryExpression(node *ast.TernaryExpression) error {
	jumpFalsePos, err := c.compileConditionAndJumpIfFalse(node.Condition, node.Token.Line, node.Token.Column)
	if err != nil {
		return err
	}
	if err := c.compileExpression(node.ThenExpr); err != nil {
		return err
	}
	jumpPos := c.emitJump(OpJump, node.Token.Line, node.Token.Column)
	c.patchJump(jumpFalsePos)
	if err := c.compileExpression(node.ElseExpr); err != nil {
		return err
	}
	c.patchJump(jumpPos)
	return nil
}

// freeVarSet walks an AST node collecting identifiers that are used but not
// declared within the subtree. Nested function literals are handled: their own
// free variables are propagated up unless they are locally shadowed.
type freeVarSet struct {
	defined map[string]bool
	free    map[string]bool
}

func newFreeVarSet(params []ast.Parameter) *freeVarSet {
	s := &freeVarSet{defined: map[string]bool{}, free: map[string]bool{}}
	for _, p := range params {
		if p.Name != nil {
			// Identifiers in Geblang are case-sensitive (locals are
			// looked up by exact name in the enclosing scope). The
			// scanner used to lowercase here, which silently
			// dropped captured upvalues whose names contained
			// uppercase letters - the closure body then emitted
			// `OpGetLocal <outer-slot>` that, at runtime, indexed
			// into the closure's own locals frame at the wrong slot.
			s.defined[p.Name.Value] = true
		}
	}
	return s
}

func (s *freeVarSet) scanBlock(block *ast.BlockStatement) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		s.scanStatement(stmt)
	}
}

func (s *freeVarSet) scanStatement(stmt ast.Statement) {
	if stmt == nil {
		return
	}
	switch stmt := stmt.(type) {
	case *ast.DeclarationStatement:
		if stmt.Value != nil {
			s.scanExpr(stmt.Value)
		}
		if stmt.Name != nil {
			s.defined[stmt.Name.Value] = true
		}
	case *ast.DestructuringStatement:
		s.scanExpr(stmt.Value)
		for _, name := range stmt.Names {
			if name == nil {
				continue
			}
			if stmt.Define {
				s.defined[name.Value] = true
			} else {
				s.scanExpr(name)
			}
		}
	case *ast.ReturnStatement:
		if stmt.Value != nil {
			s.scanExpr(stmt.Value)
		}
	case *ast.YieldStatement:
		if stmt.Value != nil {
			s.scanExpr(stmt.Value)
		}
	case *ast.ExpressionStatement:
		s.scanExpr(stmt.Expression)
	case *ast.IfStatement:
		s.scanExpr(stmt.Condition)
		s.scanBlock(stmt.Consequence)
		for _, branch := range stmt.ElseIfs {
			s.scanExpr(branch.Condition)
			s.scanBlock(branch.Body)
		}
		s.scanBlock(stmt.Alternative)
	case *ast.WhileStatement:
		s.scanExpr(stmt.Condition)
		s.scanBlock(stmt.Body)
	case *ast.ForStatement:
		s.scanStatement(stmt.Init)
		s.scanExpr(stmt.Condition)
		s.scanStatement(stmt.Update)
		if stmt.VarName != nil {
			s.defined[stmt.VarName.Value] = true
		}
		for _, v := range stmt.VarNames {
			if v != nil {
				s.defined[v.Value] = true
			}
		}
		s.scanExpr(stmt.Iterable)
		s.scanExpr(stmt.Step)
		s.scanBlock(stmt.Body)
	case *ast.TryStatement:
		s.scanBlock(stmt.Body)
		for _, catch := range stmt.Catches {
			if catch.Name != nil {
				inner := &freeVarSet{defined: map[string]bool{catch.Name.Value: true}, free: map[string]bool{}}
				for k := range s.defined {
					inner.defined[k] = true
				}
				inner.scanBlock(catch.Body)
				for name := range inner.free {
					if !s.defined[name] {
						s.free[name] = true
					}
				}
			} else {
				s.scanBlock(catch.Body)
			}
		}
		s.scanBlock(stmt.Finally)
	case *ast.FunctionStatement:
		if stmt.Name != nil {
			s.defined[stmt.Name.Value] = true
		}
		inner := newFreeVarSet(stmt.Parameters)
		inner.scanBlock(stmt.Body)
		for name := range inner.free {
			if !s.defined[name] {
				s.free[name] = true
			}
		}
	case *ast.SimpleStatement:
		s.scanExpr(stmt.Value)
	case *ast.MatchStatement:
		s.scanExpr(stmt.Expr)
		for _, mc := range stmt.Cases {
			s.scanMatchCase(mc)
		}
	case *ast.SelectStatement:
		for _, sc := range stmt.Cases {
			s.scanExpr(sc.Channel)
			if sc.Kind == "send" {
				s.scanExpr(sc.Value)
			}
			// A recv binding is a fresh local scoped to its case body.
			if sc.Kind == "recv" && sc.Binding != "" {
				inner := &freeVarSet{defined: map[string]bool{sc.Binding: true}, free: map[string]bool{}}
				for k := range s.defined {
					inner.defined[k] = true
				}
				inner.scanBlock(sc.Body)
				for name := range inner.free {
					if !s.defined[name] {
						s.free[name] = true
					}
				}
			} else {
				s.scanBlock(sc.Body)
			}
		}
		s.scanBlock(stmt.Default)
	case *ast.ClassStatement:
		if stmt.Name != nil {
			s.defined[stmt.Name.Value] = true
		}
	case *ast.WithStatement:
		s.scanExpr(stmt.Value)
		if stmt.Name != nil {
			inner := &freeVarSet{defined: map[string]bool{stmt.Name.Value: true}, free: map[string]bool{}}
			for k := range s.defined {
				inner.defined[k] = true
			}
			inner.scanBlock(stmt.Body)
			for name := range inner.free {
				if !s.defined[name] {
					s.free[name] = true
				}
			}
		} else {
			s.scanBlock(stmt.Body)
		}
	}
}

func (s *freeVarSet) scanExpr(expr ast.Expression) {
	if expr == nil {
		return
	}
	switch expr := expr.(type) {
	case *ast.Identifier:
		name := expr.Value
		if !s.defined[name] {
			s.free[name] = true
		}
	case *ast.FunctionLiteral:
		inner := newFreeVarSet(expr.Parameters)
		inner.scanBlock(expr.Body)
		for name := range inner.free {
			if !s.defined[name] {
				s.free[name] = true
			}
		}
	case *ast.InfixExpression:
		s.scanExpr(expr.Left)
		s.scanExpr(expr.Right)
	case *ast.PrefixExpression:
		s.scanExpr(expr.Right)
	case *ast.PostfixExpression:
		s.scanExpr(expr.Left)
	case *ast.AssignmentExpression:
		s.scanExpr(expr.Left)
		s.scanExpr(expr.Value)
	case *ast.CallExpression:
		s.scanExpr(expr.Callee)
		for _, arg := range expr.Arguments {
			s.scanExpr(arg.Value)
		}
	case *ast.SelectorExpression:
		s.scanExpr(expr.Object)
	case *ast.IndexExpression:
		s.scanExpr(expr.Left)
		s.scanExpr(expr.Index)
	case *ast.ListLiteral:
		for _, el := range expr.Elements {
			s.scanExpr(el)
		}
	case *ast.DictLiteral:
		for _, entry := range expr.Entries {
			s.scanExpr(entry.Key)
			s.scanExpr(entry.Value)
		}
	case *ast.SetLiteral:
		for _, el := range expr.Elements {
			s.scanExpr(el)
		}
	case *ast.RangeExpression:
		s.scanExpr(expr.Start)
		s.scanExpr(expr.End)
		s.scanExpr(expr.Step)
	case *ast.MatchExpression:
		s.scanExpr(expr.Expr)
		for _, mc := range expr.Cases {
			s.scanMatchCase(mc)
		}
	case *ast.SpreadExpression:
		s.scanExpr(expr.Value)
	case *ast.CastExpression:
		s.scanExpr(expr.Value)
	case *ast.AwaitExpression:
		s.scanExpr(expr.Value)
	case *ast.TernaryExpression:
		s.scanExpr(expr.Condition)
		s.scanExpr(expr.ThenExpr)
		s.scanExpr(expr.ElseExpr)
	case *ast.InterpolatedString:
		for _, part := range expr.Parts {
			s.scanExpr(part)
		}
	case *ast.FormattedInterpolation:
		s.scanExpr(expr.Value)
	case *ast.PipeExpression:
		s.scanExpr(expr.Left)
		s.scanExpr(expr.Right)
	case *ast.PartialExpression:
		s.scanExpr(expr.Callee)
		for _, arg := range expr.Arguments {
			if !arg.Hole {
				s.scanExpr(arg.Value)
			}
		}
	case *ast.ListComprehension:
		s.scanComprehensionClauses(expr.Clauses)
		s.scanExpr(expr.Body)
	case *ast.SetComprehension:
		s.scanComprehensionClauses(expr.Clauses)
		s.scanExpr(expr.Body)
	case *ast.DictComprehension:
		s.scanComprehensionClauses(expr.Clauses)
		s.scanExpr(expr.KeyBody)
		s.scanExpr(expr.ValueBody)
	}
}

// scanComprehensionClauses mirrors the ForStatement case: loop vars are defined before later clauses/body are scanned.
func (s *freeVarSet) scanComprehensionClauses(clauses []ast.ComprehensionClause) {
	for _, clause := range clauses {
		switch c := clause.(type) {
		case *ast.ComprehensionFor:
			if c.VarName != nil {
				s.defined[c.VarName.Value] = true
			}
			for _, v := range c.VarNames {
				if v != nil {
					s.defined[v.Value] = true
				}
			}
			s.scanExpr(c.Iterable)
		case *ast.ComprehensionIf:
			s.scanExpr(c.Filter)
		}
	}
}

// scanMatchCase covers Alternates and list-pattern literal bindings that the Pattern/Guard/Value/Body scan missed.
func (s *freeVarSet) scanMatchCase(mc ast.MatchCase) {
	s.scanExpr(mc.Pattern)
	s.scanExpr(mc.Guard)
	s.scanExpr(mc.Value)
	s.scanBlock(mc.Body)
	for _, alt := range mc.Alternates {
		s.scanExpr(alt)
	}
	if mc.ListPattern != nil {
		for _, binding := range mc.ListPattern.Bindings {
			s.scanExpr(binding.Literal)
		}
	}
}

func (c *Compiler) compileFunctionLiteral(expr *ast.FunctionLiteral) error {

	// Collect free variables: identifiers used in body not declared as params.
	scanner := newFreeVarSet(expr.Parameters)
	scanner.scanBlock(expr.Body)

	// Resolve which free variables are local upvalues vs. globals.
	type upvalueCapture struct {
		name      string
		outerSlot int64
	}
	var captures []upvalueCapture
	for name := range scanner.free {
		b, ok := c.resolveName(name)
		if !ok || b.kind != "local" {
			continue
		}
		captures = append(captures, upvalueCapture{name: name, outerSlot: b.slot})
	}
	// Sort for deterministic slot assignment.
	for i := 1; i < len(captures); i++ {
		for j := i; j > 0 && captures[j].name < captures[j-1].name; j-- {
			captures[j], captures[j-1] = captures[j-1], captures[j]
		}
	}

	index := c.declareFunction("<closure>")
	skipJump := c.emitJump(OpJump, expr.Token.Line, expr.Token.Column)
	entry := int64(len(c.chunk.Instructions))
	c.pushScope()
	c.pushFunctionLocals()

	// Allocate upvalue slots first (indices 0..N-1 in ParamSlots).
	upvalueCount := int64(len(captures))
	paramSlots := make([]int64, 0, upvalueCount+int64(len(expr.Parameters)))
	paramNames := make([]string, 0, cap(paramSlots))
	paramDisplayNames := make([]string, 0, cap(paramSlots))
	paramTypes := make([]string, 0, cap(paramSlots))
	defaultConstants := make([]int64, 0, cap(paramSlots))

	for _, cap := range captures {
		slot := c.defineLocalWithType(cap.name, "")
		paramSlots = append(paramSlots, slot)
		paramNames = append(paramNames, cap.name)
		paramDisplayNames = append(paramDisplayNames, cap.name)
		paramTypes = append(paramTypes, "")
		defaultConstants = append(defaultConstants, -1)
	}

	// Allocate parameter slots.
	for _, param := range expr.Parameters {
		if param.Name == nil {
			c.popFunctionLocals()
			c.popScope()
			return fmt.Errorf("function literal parameter has no name")
		}
		paramType := c.bytecodeTypeName(param.Type)
		slot := c.defineLocalWithType(param.Name.Value, paramType)
		paramSlots = append(paramSlots, slot)
		paramNames = append(paramNames, strings.ToLower(param.Name.Value))
		paramDisplayNames = append(paramDisplayNames, param.Name.Value)
		paramTypes = append(paramTypes, paramType)
		if param.Default == nil {
			defaultConstants = append(defaultConstants, -1)
			continue
		}
		value, err := constantValueFromExpression(param.Default)
		if err != nil {
			c.popFunctionLocals()
			c.popScope()
			return err
		}
		defaultConstants = append(defaultConstants, int64(len(c.chunk.Constants)))
		c.chunk.Constants = append(c.chunk.Constants, value)
	}

	fn := &c.chunk.Functions[index]
	fn.Entry = entry
	fn.ParamNames = paramNames
	fn.ParamDisplayNames = paramDisplayNames
	fn.ParamSlots = paramSlots
	fn.ParamTypes = paramTypes
	fn.ReturnType = c.bytecodeReturnType(expr.ReturnType)
	fn.DefaultConstants = defaultConstants
	fn.UpvalueCount = upvalueCount
	fn.Variadic = len(expr.Parameters) > 0 && expr.Parameters[len(expr.Parameters)-1].Variadic
	fn.Async = expr.Async
	fn.IsGenerator = blockContainsYield(expr.Body)

	c.inFunc++
	c.returnTypes = append(c.returnTypes, fn.ReturnType)
	// Isolate finalizers (see compileFunctionWithPrologue).
	savedLoops, savedFinalizers := c.loops, c.finalizers
	c.loops, c.finalizers = nil, nil
	defer func() { c.loops, c.finalizers = savedLoops, savedFinalizers }()
	if err := c.compileBlock(expr.Body); err != nil {
		c.inFunc--
		c.returnTypes = c.returnTypes[:len(c.returnTypes)-1]
		c.popFunctionLocals()
		c.popScope()
		return err
	}
	c.emitConstant(runtime.Null{}, expr.Token.Line, expr.Token.Column)
	c.emitAt(OpReturn, expr.Token.Line, expr.Token.Column)
	fn.LocalCount = c.popFunctionLocals()
	c.inFunc--
	c.returnTypes = c.returnTypes[:len(c.returnTypes)-1]
	c.popScope()
	c.patchJump(skipJump)

	// Emit OpMakeClosure: [funcIndex, N, outerSlot0, ..., outerSlotN-1].
	operands := make([]int64, 0, 2+len(captures))
	operands = append(operands, index, upvalueCount)
	for _, cap := range captures {
		operands = append(operands, cap.outerSlot)
	}
	c.emitAt(OpMakeClosure, expr.Token.Line, expr.Token.Column, operands...)
	return nil
}

func (c *Compiler) compileShortCircuitExpression(expr *ast.InfixExpression) error {
	if err := c.compileExpression(expr.Left); err != nil {
		return err
	}
	if expr.Operator == "&&" {
		falseJump := c.emitJump(OpJumpIfFalse, expr.Token.Line, expr.Token.Column)
		if err := c.compileExpression(expr.Right); err != nil {
			return err
		}
		endJump := c.emitJump(OpJump, expr.Token.Line, expr.Token.Column)
		c.patchJump(falseJump)
		c.emitConstant(runtime.Bool{Value: false}, expr.Token.Line, expr.Token.Column)
		c.patchJump(endJump)
		return nil
	}
	rightJump := c.emitJump(OpJumpIfFalse, expr.Token.Line, expr.Token.Column)
	c.emitConstant(runtime.Bool{Value: true}, expr.Token.Line, expr.Token.Column)
	endJump := c.emitJump(OpJump, expr.Token.Line, expr.Token.Column)
	c.patchJump(rightJump)
	if err := c.compileExpression(expr.Right); err != nil {
		return err
	}
	c.patchJump(endJump)
	return nil
}

func (c *Compiler) compileNullCoalesceExpression(expr *ast.InfixExpression) error {
	if err := c.compileExpression(expr.Left); err != nil {
		return err
	}
	// If left is non-null, jump past right side (leaving left on stack).
	endJump := c.emitJump(OpNullCoalesce, expr.Token.Line, expr.Token.Column)
	// Left was null: pop it, then evaluate right side.
	if err := c.compileExpression(expr.Right); err != nil {
		return err
	}
	c.patchJump(endJump)
	return nil
}

func (c *Compiler) compileBuiltinCall(expr *ast.CallExpression, module, name string) error {
	if module == "reflect" {
		return c.compileReflectCall(expr, name)
	}
	switch {
	case module == "io" && name == "println":
		args, err := c.singleBuiltinArgument(expr, module, name)
		if err != nil {
			return err
		}
		if err := c.compileExpression(args[0]); err != nil {
			return err
		}
		c.emitAt(OpPrintln, expr.Token.Line, expr.Token.Column)
		return nil
	case module == "io" && (name == "print" || name == "stdoutWrite"):
		args, err := c.singleBuiltinArgument(expr, module, name)
		if err != nil {
			return err
		}
		if err := c.compileExpression(args[0]); err != nil {
			return err
		}
		c.emitAt(OpPrint, expr.Token.Line, expr.Token.Column)
		return nil
	case module == "sys" && name == "exit":
		args, err := c.singleBuiltinArgument(expr, module, name)
		if err != nil {
			return err
		}
		if err := c.compileExpression(args[0]); err != nil {
			return err
		}
		c.emitAt(OpExit, expr.Token.Line, expr.Token.Column)
		return nil
	case native.IsPureBuiltin(module, name), isStatefulBytecodeBuiltin(module, name):
		if _, hasSpread := callSpreadIndex(expr.Arguments); hasSpread {
			meta, err := c.compileSpreadCallArguments(expr.Arguments)
			if err != nil {
				return err
			}
			nameIndex := int64(len(c.chunk.Constants))
			c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: module + "." + name})
			operands := append([]int64{nameIndex, int64(len(expr.Arguments))}, meta...)
			c.emitAt(OpNativeCallSpread, expr.Token.Line, expr.Token.Column, operands...)
			return nil
		}
		hasNamedArgs := false
		for _, arg := range expr.Arguments {
			if arg.Name != nil {
				hasNamedArgs = true
			}
			if err := c.compileExpression(arg.Value); err != nil {
				return err
			}
		}
		nameIndex := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: module + "." + name})
		if !hasNamedArgs {
			c.emitAt(OpNativeCall, expr.Token.Line, expr.Token.Column, nameIndex, int64(len(expr.Arguments)))
			return nil
		}
		operands := []int64{nameIndex, int64(len(expr.Arguments))}
		for _, arg := range expr.Arguments {
			argName := ""
			if arg.Name != nil {
				argName = arg.Name.Value
			}
			argNameIndex := int64(len(c.chunk.Constants))
			c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: argName})
			operands = append(operands, argNameIndex)
		}
		c.emitAt(OpNativeCallNamed, expr.Token.Line, expr.Token.Column, operands...)
		return nil
	default:
		return parityErrorf("bytecode compiler does not support %s.%s; use --disable-vm to run with the tree-walking evaluator", module, name)
	}
}

func (c *Compiler) compileReflectCall(expr *ast.CallExpression, name string) error {
	switch name {
	case "function", "class":
		if len(expr.Arguments) != 1 {
			return fmt.Errorf("reflect.%s expects exactly one argument", name)
		}
		if literal, ok := expr.Arguments[0].Value.(*ast.StringLiteral); ok {
			value, handled, err := c.reflectNamedTarget(expr, name)
			if err != nil {
				return err
			}
			if handled {
				c.emitConstant(value, expr.Token.Line, expr.Token.Column)
				return nil
			}
			// Qualified `module.export` form goes through the
			// compile-time lookup helper; bare names fall through
			// to the runtime native call so the VM's FindClassByName
			// can resolve them across loaded modules.
			if strings.Contains(literal.Value, ".") {
				argc, err := c.compileReflectQualifiedLookup(expr, name)
				if err != nil {
					return err
				}
				// argc==0 means the helper already pushed the result (a Null
				// for an unknown module, matching the evaluator's env-miss).
				if argc == 0 {
					return nil
				}
				nameIndex := int64(len(c.chunk.Constants))
				c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "reflect." + name})
				c.emitAt(OpNativeCall, expr.Token.Line, expr.Token.Column, nameIndex, int64(argc))
				return nil
			}
			c.emitConstant(runtime.String{Value: literal.Value}, expr.Token.Line, expr.Token.Column)
			nameIndex := int64(len(c.chunk.Constants))
			c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "reflect." + name})
			c.emitAt(OpNativeCall, expr.Token.Line, expr.Token.Column, nameIndex, 1)
			return nil
		}
		if err := c.compileExpression(expr.Arguments[0].Value); err != nil {
			return err
		}
		nameIndex := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "reflect." + name})
		c.emitAt(OpNativeCall, expr.Token.Line, expr.Token.Column, nameIndex, 1)
		return nil
	case "module":
		if err := c.compileReflectModuleLookup(expr); err != nil {
			return err
		}
		nameIndex := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "reflect.module"})
		c.emitAt(OpNativeCall, expr.Token.Line, expr.Token.Column, nameIndex, 1)
		return nil
	case "method", "staticMethod", "getField", "setField":
		for _, arg := range expr.Arguments {
			if err := c.compileExpression(arg.Value); err != nil {
				return err
			}
		}
		nameIndex := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "reflect." + name})
		c.emitAt(OpNativeCall, expr.Token.Line, expr.Token.Column, nameIndex, int64(len(expr.Arguments)))
		return nil
	case "decorators", "hasDecorator", "decorator", "parameters", "returnType", "doc", "docs", "typeOf", "location", "fields", "methods", "staticMethods", "parent", "interfaces", "className", "constructors", "typeBindings", "interfaceMethods", "interfaceParents":
		if reflectSingleValueCall(name) && len(expr.Arguments) != 1 {
			return fmt.Errorf("reflect.%s expects value", name)
		}
		if name == "decorators" && len(expr.Arguments) != 1 && len(expr.Arguments) != 2 {
			return fmt.Errorf("reflect.decorators expects value and optional decorator name")
		}
		if len(expr.Arguments) != 2 && name != "decorators" && !reflectSingleValueCall(name) {
			return fmt.Errorf("reflect.%s expects value and decorator name", name)
		}
		if name == "typeOf" {
			if err := c.compileExpression(expr.Arguments[0].Value); err != nil {
				return err
			}
		} else if name == "interfaceMethods" || name == "interfaceParents" {
			if err := c.compileReflectInterfaceArgument(expr.Arguments[0].Value); err != nil {
				return err
			}
		} else if name == "doc" || name == "docs" {
			if ident, ok := expr.Arguments[0].Value.(*ast.Identifier); ok {
				if _, found := c.interfaces[strings.ToLower(ident.Value)]; found {
					c.emitConstant(runtime.String{Value: ident.Value}, ident.Token.Line, ident.Token.Column)
				} else if err := c.compileReflectTargetArgument(expr.Arguments[0].Value); err != nil {
					return err
				}
			} else if err := c.compileReflectTargetArgument(expr.Arguments[0].Value); err != nil {
				return err
			}
		} else {
			if err := c.compileReflectTargetArgument(expr.Arguments[0].Value); err != nil {
				return err
			}
		}
		for _, arg := range expr.Arguments[1:] {
			if err := c.compileExpression(arg.Value); err != nil {
				return err
			}
		}
		nameIndex := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "reflect." + name})
		c.emitAt(OpNativeCall, expr.Token.Line, expr.Token.Column, nameIndex, int64(len(expr.Arguments)))
		return nil
	case "exports":
		if len(expr.Arguments) != 1 {
			return fmt.Errorf("reflect.exports expects module")
		}
		if err := c.compileExpression(expr.Arguments[0].Value); err != nil {
			return err
		}
		nameIndex := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "reflect.exports"})
		c.emitAt(OpNativeCall, expr.Token.Line, expr.Token.Column, nameIndex, 1)
		return nil
	case "classes":
		if len(expr.Arguments) != 0 {
			return fmt.Errorf("reflect.classes takes no arguments")
		}
		nameIndex := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "reflect.classes"})
		c.emitAt(OpNativeCall, expr.Token.Line, expr.Token.Column, nameIndex, 0)
		return nil
	default:
		return parityErrorf("bytecode compiler does not support reflect.%s", name)
	}
}

func reflectSingleValueCall(name string) bool {
	switch name {
	case "parameters", "returnType", "doc", "docs", "typeOf", "location", "fields", "methods", "staticMethods", "parent", "interfaces", "className", "constructors", "typeBindings", "interfaceMethods", "interfaceParents":
		return true
	default:
		return false
	}
}

func (c *Compiler) compileReflectInterfaceArgument(expr ast.Expression) error {
	if ident, ok := expr.(*ast.Identifier); ok {
		if _, found := c.interfaces[strings.ToLower(ident.Value)]; found {
			c.emitConstant(runtime.String{Value: ident.Value}, ident.Token.Line, ident.Token.Column)
			return nil
		}
	}
	return c.compileExpression(expr)
}

func (c *Compiler) compileReflectModuleLookup(expr *ast.CallExpression) error {
	if len(expr.Arguments) != 1 {
		return fmt.Errorf("reflect.module expects exactly one name")
	}
	nameLiteral, ok := expr.Arguments[0].Value.(*ast.StringLiteral)
	if !ok {
		return fmt.Errorf("reflect.module name must be a string literal in bytecode")
	}
	if strings.Contains(nameLiteral.Value, ".") {
		return fmt.Errorf("reflect.module bytecode name must be an imported module alias")
	}
	resolved, ok := c.resolveName(nameLiteral.Value)
	if !ok {
		// Native modules are imported via the fast path (no global slot) but
		// recorded in moduleAliases; load the module value so reflect.module
		// resolves them like the evaluator does.
		if canonical, isModule := c.moduleAliases[nameLiteral.Value]; isModule {
			c.emitModuleValue(canonical, expr.Token.Line, expr.Token.Column)
			return nil
		}
		c.emitConstant(runtime.Null{}, expr.Token.Line, expr.Token.Column)
		return nil
	}
	if resolved.kind == "local" {
		c.emitAt(OpGetLocal, expr.Token.Line, expr.Token.Column, resolved.slot)
	} else {
		c.emitAt(OpGetGlobal, expr.Token.Line, expr.Token.Column, resolved.slot)
	}
	return nil
}

// emitModuleValue emits an OpLoadModuleValue for the canonical module name.
func (c *Compiler) emitModuleValue(canonical string, line int, column int) {
	index := int64(len(c.chunk.Constants))
	c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: canonical})
	c.emitAt(OpLoadModuleValue, line, column, index)
}

func (c *Compiler) compileReflectTargetArgument(expr ast.Expression) error {
	if target, ok, err := c.reflectTargetFromExpression(expr); ok || err != nil {
		if err != nil {
			return err
		}
		line, column := expressionLocation(expr)
		c.emitConstant(target, line, column)
		return nil
	}
	return c.compileExpression(expr)
}

func (c *Compiler) reflectTargetFromExpression(expr ast.Expression) (runtime.DecoratorTarget, bool, error) {
	if ident, ok := expr.(*ast.Identifier); ok {
		key := strings.ToLower(ident.Value)
		if target, ok := c.reflectFuncs[key]; ok {
			if err := validateReflectDecorators(target.Decorators); err != nil {
				return runtime.DecoratorTarget{}, false, err
			}
			return target, true, nil
		}
		if target, ok := c.reflectClasses[key]; ok {
			if err := validateReflectDecorators(target.Decorators); err != nil {
				return runtime.DecoratorTarget{}, false, err
			}
			return target, true, nil
		}
	}
	if call, ok := expr.(*ast.CallExpression); ok {
		if module, name, ok := selectorName(call.Callee); ok && module == "reflect" {
			switch name {
			case "function", "class":
				// A value argument (e.g. an instance) resolves only at runtime.
				if len(call.Arguments) != 1 {
					return runtime.DecoratorTarget{}, false, nil
				}
				if _, ok := call.Arguments[0].Value.(*ast.StringLiteral); !ok {
					return runtime.DecoratorTarget{}, false, nil
				}
				target, handled, err := c.reflectNamedTarget(call, name)
				if err != nil {
					return runtime.DecoratorTarget{}, true, err
				}
				if !handled {
					return runtime.DecoratorTarget{}, false, nil
				}
				if targetValue, ok := target.(runtime.DecoratorTarget); ok {
					return targetValue, true, nil
				}
				return runtime.DecoratorTarget{}, false, nil
			case "method", "staticMethod":
				return runtime.DecoratorTarget{}, false, nil
			}
		}
	}
	return runtime.DecoratorTarget{}, false, nil
}

func (c *Compiler) reflectNamedTarget(expr *ast.CallExpression, name string) (runtime.Value, bool, error) {
	if len(expr.Arguments) != 1 {
		return nil, false, fmt.Errorf("reflect.%s expects exactly one name", name)
	}
	nameLiteral, ok := expr.Arguments[0].Value.(*ast.StringLiteral)
	if !ok {
		return nil, false, fmt.Errorf("reflect.%s name must be a string literal in bytecode", name)
	}
	if strings.Contains(nameLiteral.Value, ".") {
		return nil, false, nil
	}
	key := strings.ToLower(nameLiteral.Value)
	switch name {
	case "function":
		target, ok := c.reflectFuncs[key]
		if !ok {
			// Fall through to the runtime path so the VM can look
			// up the function in another loaded module via the
			// module loader.
			return nil, false, nil
		}
		if err := validateReflectDecorators(target.Decorators); err != nil {
			return nil, false, err
		}
		return target, true, nil
	case "class":
		target, ok := c.reflectClasses[key]
		if !ok {
			// Fall through to the runtime path so the VM's
			// FindClassByName can resolve classes declared in
			// other loaded modules.
			return nil, false, nil
		}
		if err := validateReflectDecorators(target.Decorators); err != nil {
			return nil, false, err
		}
		return target, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported reflect lookup %s", name)
	}
}

func (c *Compiler) compileReflectQualifiedLookup(expr *ast.CallExpression, name string) (int, error) {
	if len(expr.Arguments) != 1 {
		return 0, fmt.Errorf("reflect.%s expects exactly one name", name)
	}
	nameLiteral, ok := expr.Arguments[0].Value.(*ast.StringLiteral)
	if !ok {
		return 0, fmt.Errorf("reflect.%s name must be a string literal in bytecode", name)
	}
	moduleName, exportName, ok := strings.Cut(nameLiteral.Value, ".")
	if !ok || moduleName == "" || exportName == "" || strings.Contains(exportName, ".") {
		return 0, fmt.Errorf("reflect.%s bytecode qualified name must be module.export", name)
	}
	resolved, ok := c.resolveName(moduleName)
	if !ok {
		// Native modules carry no global slot; load the module value so the
		// runtime reads module.Exports[export], as the evaluator does.
		if canonical, isModule := c.moduleAliases[moduleName]; isModule {
			c.emitModuleValue(canonical, expr.Token.Line, expr.Token.Column)
			c.emitConstant(runtime.String{Value: exportName}, expr.Token.Line, expr.Token.Column)
			return 2, nil
		}
		// Unknown module: the evaluator's env-miss yields null, so match it.
		c.emitConstant(runtime.Null{}, expr.Token.Line, expr.Token.Column)
		return 0, nil
	}
	if resolved.kind == "local" {
		c.emitAt(OpGetLocal, expr.Token.Line, expr.Token.Column, resolved.slot)
	} else {
		c.emitAt(OpGetGlobal, expr.Token.Line, expr.Token.Column, resolved.slot)
	}
	c.emitConstant(runtime.String{Value: exportName}, expr.Token.Line, expr.Token.Column)
	return 2, nil
}

func (c *Compiler) reflectMethodTarget(expr *ast.CallExpression, static bool) (runtime.DecoratorTarget, bool, error) {
	label := "reflect.method"
	if static {
		label = "reflect.staticMethod"
	}
	if len(expr.Arguments) != 2 {
		return runtime.DecoratorTarget{}, false, fmt.Errorf("%s expects class and method name", label)
	}
	classIdent, ok := expr.Arguments[0].Value.(*ast.Identifier)
	if !ok {
		return runtime.DecoratorTarget{}, false, nil
	}
	if _, ok := c.resolveName(classIdent.Value); ok {
		return runtime.DecoratorTarget{}, false, nil
	}
	methodName, ok := expr.Arguments[1].Value.(*ast.StringLiteral)
	if !ok {
		return runtime.DecoratorTarget{}, false, fmt.Errorf("%s method name must be a string literal in bytecode", label)
	}
	classKey := strings.ToLower(classIdent.Value)
	if _, ok := c.reflectClasses[classKey]; !ok {
		return runtime.DecoratorTarget{}, false, nil
	}
	methodKey := strings.ToLower(methodName.Value)
	target := "method"
	reflectTarget := runtime.DecoratorTarget{}
	if static {
		target = "staticMethod"
		if methods, ok := c.reflectStatics[classKey]; ok {
			reflectTarget = methods[methodKey]
		}
		if reflectTarget.Target == "" {
			if methods, ok := c.reflectMethods[classKey]; ok {
				reflectTarget = methods[methodKey]
			}
		}
	} else if methods, ok := c.reflectMethods[classKey]; ok {
		reflectTarget = methods[methodKey]
	}
	if reflectTarget.Target == "" {
		reflectTarget = runtime.DecoratorTarget{Target: target, Decorators: []runtime.DecoratorMetadata{}}
	}
	if err := validateReflectDecorators(reflectTarget.Decorators); err != nil {
		return runtime.DecoratorTarget{}, false, err
	}
	return reflectTarget, true, nil
}

func validateReflectDecorators(decorators []runtime.DecoratorMetadata) error {
	for _, decorator := range decorators {
		for _, value := range decorator.Args {
			if errValue, ok := value.(runtime.Error); ok {
				return fmt.Errorf("decorator %s metadata: %s", decorator.Name, errValue.Message)
			}
		}
		for _, value := range decorator.NamedArgs {
			if errValue, ok := value.(runtime.Error); ok {
				return fmt.Errorf("decorator %s metadata: %s", decorator.Name, errValue.Message)
			}
		}
	}
	return nil
}

// emitPlantCallTypeBindings emits an OpPlantCallTypeBindings instruction
// for a CallExpression whose explicit `<TypeArgs>` clause should pin the
// callee's type parameters before the matching OpCall consumes them.
// No instruction is emitted when there are no explicit type args, the
// callee declares no type parameters, or none of the args produce a
// usable binding. The order of operands mirrors OpSetTypeBindings so
// the runtime handler can be shared in spirit with the class-instance
// path.
func (c *Compiler) emitPlantCallTypeBindings(expr *ast.CallExpression, typeParameters []string) {
	if len(expr.TypeArguments) == 0 || len(typeParameters) == 0 {
		return
	}
	operands := []int64{0}
	count := int64(0)
	for i, arg := range expr.TypeArguments {
		if i >= len(typeParameters) {
			break
		}
		if arg == nil || arg.Operator != "" || arg.Name == "" {
			continue
		}
		paramNameIdx := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: typeParameters[i]})
		typeNameIdx := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: arg.Name})
		operands = append(operands, paramNameIdx, typeNameIdx)
		count++
	}
	if count == 0 {
		return
	}
	operands[0] = count
	c.emitAt(OpPlantCallTypeBindings, expr.Token.Line, expr.Token.Column, operands...)
}

// annotationConstructorCall copies `Box<int> x = Box(...)`'s initializer
// with the annotation's type args adopted; the AST stays untouched
// because the formatter and LSP reprint it.
func annotationConstructorCall(stmt *ast.DeclarationStatement) (*ast.CallExpression, bool) {
	if stmt.Type == nil || stmt.Type.Operator != "" || len(stmt.Type.Arguments) == 0 || stmt.Value == nil {
		return nil, false
	}
	call, ok := stmt.Value.(*ast.CallExpression)
	if !ok || len(call.TypeArguments) > 0 {
		return nil, false
	}
	name := ""
	switch callee := call.Callee.(type) {
	case *ast.Identifier:
		name = callee.Value
	case *ast.SelectorExpression:
		if obj, ok := callee.Object.(*ast.Identifier); ok && callee.Name != nil {
			name = obj.Value + "." + callee.Name.Value
		}
	}
	if name == "" || name != stmt.Type.Name {
		return nil, false
	}
	copied := *call
	copied.TypeArguments = stmt.Type.Arguments
	return &copied, true
}

// emitPlantCallTypeArgs stages a selector call's explicit `<TypeArgs>`
// positionally; the dispatch site zips them against the callee's params.
func (c *Compiler) emitPlantCallTypeArgs(expr *ast.CallExpression) {
	if len(expr.TypeArguments) == 0 {
		return
	}
	operands := []int64{0}
	count := int64(0)
	for _, arg := range expr.TypeArguments {
		if arg == nil || arg.Operator != "" || arg.Name == "" {
			continue
		}
		typeNameIdx := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: arg.Name})
		operands = append(operands, typeNameIdx)
		count++
	}
	if count == 0 {
		return
	}
	operands[0] = count
	c.emitAt(OpPlantCallTypeArgs, expr.Token.Line, expr.Token.Column, operands...)
}

func expressionLocation(expr ast.Expression) (int, int) {
	switch expr := expr.(type) {
	case *ast.Identifier:
		return expr.Token.Line, expr.Token.Column
	case *ast.CallExpression:
		return expr.Token.Line, expr.Token.Column
	case *ast.SelectorExpression:
		return expr.Token.Line, expr.Token.Column
	default:
		return 0, 0
	}
}

func (c *Compiler) singleBuiltinArgument(expr *ast.CallExpression, module, name string) ([]ast.Expression, error) {
	if len(expr.Arguments) != 1 {
		switch {
		case module == "io" && name == "println":
			return nil, fmt.Errorf("io.println expects exactly one argument")
		case module == "io" && (name == "print" || name == "stdoutWrite"):
			return nil, fmt.Errorf("%s.%s expects exactly one argument", module, name)
		case module == "sys" && name == "exit":
			return nil, fmt.Errorf("sys.exit expects exactly one argument")
		default:
			return nil, fmt.Errorf("%s.%s expects exactly one argument", module, name)
		}
	}
	return []ast.Expression{expr.Arguments[0].Value}, nil
}

func (c *Compiler) compileAssignmentExpression(expr *ast.AssignmentExpression) error {
	switch left := expr.Left.(type) {
	case *ast.Identifier:
		resolved, ok := c.resolveName(left.Value)
		if !ok {
			return fmt.Errorf("unknown bytecode name %s", left.Value)
		}
		if arg, ok := selfListPushAssignment(left.Value, expr.Value); ok {
			if err := c.compileExpression(arg); err != nil {
				return err
			}
			if resolved.kind == "local" {
				c.emitAt(OpAppendLocalList, expr.Token.Line, expr.Token.Column, resolved.slot)
			} else {
				c.emitAt(OpAppendGlobalList, expr.Token.Line, expr.Token.Column, resolved.slot)
			}
			return nil
		}
		if resolved.typ == "int" {
			if op, rhsKind, rhsSlot, rhsConst, ok := c.selfIntArithAssignment(left.Value, expr.Value); ok {
				c.emitTypedIntSelfArith(op, resolved.kind, resolved.slot, rhsKind, rhsSlot, rhsConst, expr.Token.Line, expr.Token.Column)
				return nil
			}
		}
		if resolved.typ == "string" {
			if lit, ok := selfStringConstAppendAssignment(left.Value, expr.Value); ok {
				constIdx := int64(len(c.chunk.Constants))
				c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: lit})
				op := OpAppendStringConst
				if resolved.kind == "global" {
					op = OpAppendGlobalStringConst
				}
				c.emitAt(op, expr.Token.Line, expr.Token.Column, resolved.slot, constIdx)
				return nil
			}
		}
		if err := c.compileExpression(expr.Value); err != nil {
			return err
		}
		if resolved.kind == "local" {
			c.emitAt(OpSetLocal, expr.Token.Line, expr.Token.Column, resolved.slot)
		} else {
			c.emitAt(OpSetGlobal, expr.Token.Line, expr.Token.Column, resolved.slot)
		}
		return nil
	case *ast.IndexExpression:
		if err := c.compileExpression(left.Left); err != nil {
			return err
		}
		if err := c.compileExpression(left.Index); err != nil {
			return err
		}
		if err := c.compileExpression(expr.Value); err != nil {
			return err
		}
		c.emitAt(OpSetIndex, expr.Token.Line, expr.Token.Column)
		return nil
	case *ast.SelectorExpression:
		if object, ok := left.Object.(*ast.Identifier); ok {
			if _, resolvedName := c.resolveName(object.Value); !resolvedName {
				if classIndex, ok := c.classes[strings.ToLower(object.Value)]; ok && c.chunk.Classes[classIndex].Name == object.Value {
					if err := c.compileExpression(expr.Value); err != nil {
						return err
					}
					nameIndex := int64(len(c.chunk.Constants))
					c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: left.Name.Value})
					c.emitAt(OpSetStaticValue, expr.Token.Line, expr.Token.Column, classIndex, nameIndex)
					return nil
				}
			}
		}
		if err := c.compileExpression(left.Object); err != nil {
			return err
		}
		if err := c.compileExpression(expr.Value); err != nil {
			return err
		}
		nameIndex := int64(len(c.chunk.Constants))
		c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: left.Name.Value})
		c.emitAt(OpSetField, expr.Token.Line, expr.Token.Column, nameIndex)
		return nil
	case *ast.ListLiteral:
		if err := c.compileExpression(expr.Value); err != nil {
			return err
		}
		tempSlot := c.allocateLocal()
		c.emitAt(OpDefineLocal, expr.Token.Line, expr.Token.Column, tempSlot)
		for i, element := range left.Elements {
			ident, ok := element.(*ast.Identifier)
			if !ok {
				return fmt.Errorf("list destructuring target must be identifier")
			}
			resolved, ok := c.resolveName(ident.Value)
			if !ok {
				return fmt.Errorf("unknown bytecode name %s", ident.Value)
			}
			c.emitAt(OpGetLocal, expr.Token.Line, expr.Token.Column, tempSlot)
			idxConst := int64(len(c.chunk.Constants))
			c.chunk.Constants = append(c.chunk.Constants, runtime.SmallInt{Value: int64(i)})
			c.emitAt(OpConstant, expr.Token.Line, expr.Token.Column, idxConst)
			c.emitAt(OpIndex, expr.Token.Line, expr.Token.Column)
			if resolved.kind == "local" {
				c.emitAt(OpSetLocal, expr.Token.Line, expr.Token.Column, resolved.slot)
			} else {
				c.emitAt(OpSetGlobal, expr.Token.Line, expr.Token.Column, resolved.slot)
			}
		}
		return nil
	default:
		return fmt.Errorf("bytecode compiler only supports identifier, selector, index, and list destructuring assignment")
	}
}

// selfIntArithAssignment recognizes `target = target <op> rhs` where op is
// `+` or `-` and rhs is a statically int-typed term. Returns the operator, the
// rhs kind ("local" / "global" / "const"), the rhs slot or const value, and
// ok=true on match. Used to emit fused OpAdd/Sub<Scope>Int<Scope|Const> opcodes
// in compileAssignmentExpression.
func (c *Compiler) selfIntArithAssignment(targetName string, expr ast.Expression) (op string, rhsKind string, rhsSlot int64, rhsConst int64, ok bool) {
	infix, isInfix := expr.(*ast.InfixExpression)
	if !isInfix {
		return "", "", 0, 0, false
	}
	if infix.Operator != "+" && infix.Operator != "-" {
		return "", "", 0, 0, false
	}
	leftIdent, isLeftIdent := infix.Left.(*ast.Identifier)
	if !isLeftIdent || leftIdent.Value != targetName {
		return "", "", 0, 0, false
	}
	if rhsIdent, ok := infix.Right.(*ast.Identifier); ok {
		b, found := c.resolveName(rhsIdent.Value)
		if !found || b.typ != "int" {
			return "", "", 0, 0, false
		}
		return infix.Operator, b.kind, b.slot, 0, true
	}
	if intLit, ok := infix.Right.(*ast.IntegerLiteral); ok {
		value, err := runtime.NewIntLiteral(intLit.Value)
		if err != nil || !value.Value.IsInt64() {
			return "", "", 0, 0, false
		}
		return infix.Operator, "const", 0, value.Value.Int64(), true
	}
	return "", "", 0, 0, false
}

// emitTypedIntSelfArith emits the correct fused self-update arithmetic opcode
// for the destination kind, the rhs kind, and the operator. The result is
// stored back to dst and pushed onto the stack.
func (c *Compiler) emitTypedIntSelfArith(op, dstKind string, dstSlot int64, rhsKind string, rhsSlot, rhsConst int64, line, column int) {
	var opcode Op
	var rhsOperand int64
	switch dstKind {
	case "local":
		switch rhsKind {
		case "local":
			if op == "+" {
				opcode = OpAddLocalIntLocal
			} else {
				opcode = OpSubLocalIntLocal
			}
			rhsOperand = rhsSlot
		case "global":
			if op == "+" {
				opcode = OpAddLocalIntGlobal
			} else {
				opcode = OpSubLocalIntGlobal
			}
			rhsOperand = rhsSlot
		case "const":
			if op == "+" {
				opcode = OpAddLocalIntConst
			} else {
				opcode = OpSubLocalIntConst
			}
			rhsOperand = rhsConst
		}
	case "global":
		switch rhsKind {
		case "global":
			if op == "+" {
				opcode = OpAddGlobalIntGlobal
			} else {
				opcode = OpSubGlobalIntGlobal
			}
			rhsOperand = rhsSlot
		case "local":
			if op == "+" {
				opcode = OpAddGlobalIntLocal
			} else {
				opcode = OpSubGlobalIntLocal
			}
			rhsOperand = rhsSlot
		case "const":
			if op == "+" {
				opcode = OpAddGlobalIntConst
			} else {
				opcode = OpSubGlobalIntConst
			}
			rhsOperand = rhsConst
		}
	}
	c.emitAt(opcode, line, column, dstSlot, rhsOperand)
}

func selfListPushAssignment(name string, expr ast.Expression) (ast.Expression, bool) {
	call, ok := expr.(*ast.CallExpression)
	if !ok || len(call.Arguments) != 1 || call.Arguments[0].Name != nil || call.Arguments[0].Spread {
		return nil, false
	}
	selector, ok := call.Callee.(*ast.SelectorExpression)
	if !ok || selector.Optional || !strings.EqualFold(selector.Name.Value, "push") {
		return nil, false
	}
	object, ok := selector.Object.(*ast.Identifier)
	if !ok || !strings.EqualFold(object.Value, name) {
		return nil, false
	}
	return call.Arguments[0].Value, true
}

func (c *Compiler) orderFunctionArguments(function FunctionInfo, args []ast.CallArgument, paramOffset int) ([]ast.Expression, error) {
	paramNames := function.ParamNames
	if paramOffset < len(paramNames) {
		paramNames = paramNames[paramOffset:]
	} else {
		paramNames = nil
	}
	hasDefault := make([]bool, len(paramNames))
	for i := range paramNames {
		paramIndex := i + paramOffset
		hasDefault[i] = paramIndex < len(function.DefaultConstants) && function.DefaultConstants[paramIndex] >= 0
	}
	sig := argbinding.Signature{
		FuncName:   function.Name,
		ParamNames: paramNames,
		HasDefault: hasDefault,
		Variadic:   function.Variadic,
	}
	bargs := make([]argbinding.Arg, len(args))
	hasNamed := false
	for i, arg := range args {
		if arg.Name != nil {
			bargs[i].Name = arg.Name.Value
			hasNamed = true
		}
	}
	result, err := argbinding.Order(sig, bargs)
	if err != nil {
		return nil, err
	}
	if !hasNamed {
		// Positional calls emit in source order; the runtime packs any
		// variadic tail and fills trailing defaults by arity.
		if len(args) == 0 {
			return nil, nil
		}
		ordered := make([]ast.Expression, 0, len(args))
		for _, arg := range args {
			ordered = append(ordered, arg.Value)
		}
		return ordered, nil
	}
	// Named calls emit one slot per parameter with nil holes where a
	// default applies, trimmed of trailing holes.
	ordered := make([]ast.Expression, len(result.Slots))
	for i, slot := range result.Slots {
		if slot != argbinding.DefaultSlot {
			ordered[i] = args[slot].Value
		}
	}
	for len(ordered) > 0 && ordered[len(ordered)-1] == nil {
		ordered = ordered[:len(ordered)-1]
	}
	return ordered, nil
}

func (c *Compiler) selectFunctionCall(name string, args []ast.CallArgument, paramOffset int) (int64, []ast.Expression, error) {
	indices := c.funcs[strings.ToLower(name)]
	if len(indices) == 0 {
		return 0, nil, fmt.Errorf("unknown bytecode function %s", name)
	}
	return c.selectFunctionIndicesCall(name, indices, args, paramOffset)
}

func (c *Compiler) selectFunctionIndicesCall(name string, indices []int64, args []ast.CallArgument, paramOffset int) (int64, []ast.Expression, error) {
	return c.selectFunctionIndicesCallWithReturnFilter(name, indices, args, paramOffset, true)
}

func (c *Compiler) selectFunctionIndicesCallIgnoringReturn(name string, indices []int64, args []ast.CallArgument, paramOffset int) (int64, []ast.Expression, error) {
	return c.selectFunctionIndicesCallBound(name, indices, args, paramOffset, false, nil)
}

func (c *Compiler) selectFunctionIndicesCallWithReturnFilter(name string, indices []int64, args []ast.CallArgument, paramOffset int, filterReturn bool) (int64, []ast.Expression, error) {
	return c.selectFunctionIndicesCallBound(name, indices, args, paramOffset, filterReturn, nil)
}

// selectFunctionIndicesCallBound breaks overload-selection ties with
// explicit bindings; single-candidate mismatches stay with runtime
// construct-site validation.
func (c *Compiler) selectFunctionIndicesCallBound(name string, indices []int64, args []ast.CallArgument, paramOffset int, filterReturn bool, bindings map[string]string) (int64, []ast.Expression, error) {
	matches := []int64{}
	orderedMatches := [][]ast.Expression{}
	for _, index := range indices {
		function := c.chunk.Functions[index]
		if len(function.ParamNames) == 0 && len(args) > 0 {
			continue
		}
		orderedArgs, err := c.orderFunctionArguments(function, args, paramOffset)
		if err != nil {
			continue
		}
		if !c.staticArgumentsMayMatch(function, orderedArgs, paramOffset) {
			continue
		}
		if expected := c.currentExpectedType(); filterReturn && expected != "" && !c.staticFunctionReturnAssignable(function, expected) {
			continue
		}
		matches = append(matches, index)
		orderedMatches = append(orderedMatches, orderedArgs)
	}
	if len(matches) > 1 && len(bindings) > 0 {
		kept := matches[:0]
		keptOrdered := orderedMatches[:0]
		for i, index := range matches {
			if c.staticArgumentsMayMatchBound(c.chunk.Functions[index], orderedMatches[i], paramOffset, bindings) {
				kept = append(kept, index)
				keptOrdered = append(keptOrdered, orderedMatches[i])
			}
		}
		if len(kept) > 0 {
			matches = kept
			orderedMatches = keptOrdered
		}
	}
	if len(matches) == 0 {
		if msg := c.overloadMismatchDetail(name, indices, args, paramOffset); msg != "" {
			return 0, nil, fmt.Errorf("%s", msg)
		}
		return 0, nil, fmt.Errorf("no matching overload for %s", name)
	}
	if len(matches) > 1 {
		return 0, nil, parityErrorf("ambiguous overload for %s; the evaluator resolves it at runtime, the bytecode VM resolves overloads at compile time", name)
	}
	return matches[0], orderedMatches[0], nil
}

// overloadMismatchDetail mirrors the evaluator's per-parameter error for the
// lone arity-matching candidate; "" when it cannot pin one mismatch (caller
// falls back to the generic overload message).
func (c *Compiler) overloadMismatchDetail(name string, indices []int64, args []ast.CallArgument, paramOffset int) string {
	var candidate *FunctionInfo
	var ordered []ast.Expression
	for _, index := range indices {
		function := c.chunk.Functions[index]
		if len(function.ParamNames) == 0 && len(args) > 0 {
			continue
		}
		orderedArgs, err := c.orderFunctionArguments(function, args, paramOffset)
		if err != nil {
			continue
		}
		if candidate != nil {
			return ""
		}
		fn := function
		candidate = &fn
		ordered = orderedArgs
	}
	if candidate == nil || len(candidate.ParamTypes) == 0 {
		return ""
	}
	variadicIndex := -1
	if candidate.Variadic {
		variadicIndex = len(candidate.ParamTypes) - 1
	}
	for i, arg := range ordered {
		if arg == nil {
			continue
		}
		argType := c.expressionStaticType(arg)
		if argType == "" {
			continue
		}
		paramIndex := i + paramOffset
		if paramIndex >= len(candidate.ParamTypes) {
			if variadicIndex < 0 {
				return ""
			}
			paramIndex = variadicIndex
		}
		if !c.staticFunctionParamAssignable(*candidate, candidate.ParamTypes[paramIndex], argType) {
			paramName := ""
			if paramIndex < len(candidate.ParamNames) {
				paramName = candidate.ParamNames[paramIndex]
			}
			return fmt.Sprintf("%s expects %s for parameter '%s', got %s", name, candidate.ParamTypes[paramIndex], paramName, argType)
		}
	}
	return ""
}

func (c *Compiler) staticArgumentsMayMatch(function FunctionInfo, args []ast.Expression, paramOffset int) bool {
	return c.staticArgumentsMayMatchBound(function, args, paramOffset, nil)
}

func (c *Compiler) staticArgumentsMayMatchBound(function FunctionInfo, args []ast.Expression, paramOffset int, bindings map[string]string) bool {
	if len(function.ParamTypes) == 0 {
		return true
	}
	variadicIndex := -1
	if function.Variadic && len(function.ParamTypes) > 0 {
		variadicIndex = len(function.ParamTypes) - 1
	}
	for i, arg := range args {
		if arg == nil {
			continue
		}
		argType := c.expressionStaticType(arg)
		if argType == "" {
			continue
		}
		paramIndex := i + paramOffset
		if paramIndex >= len(function.ParamTypes) {
			if variadicIndex < 0 {
				return false
			}
			paramIndex = variadicIndex
		}
		paramType := function.ParamTypes[paramIndex]
		if bound, ok := bindings[paramType]; ok && bound != "" {
			paramType = bound
		}
		if !c.staticFunctionParamAssignable(function, paramType, argType) {
			return false
		}
	}
	return true
}

func (c *Compiler) expressionStaticType(expr ast.Expression) string {
	switch expr := expr.(type) {
	case *ast.IntegerLiteral:
		return "int"
	case *ast.DecimalLiteral:
		return "decimal"
	case *ast.FloatLiteral:
		return "float"
	case *ast.StringLiteral:
		return "string"
	case *ast.InterpolatedString:
		return "string"
	case *ast.Literal:
		switch expr.Value.(type) {
		case bool:
			return "bool"
		case nil:
			return "null"
		}
	case *ast.ListLiteral:
		return "list"
	case *ast.DictLiteral:
		return "dict"
	case *ast.SetLiteral:
		return "set"
	case *ast.Identifier:
		if strings.EqualFold(expr.Value, "null") {
			return "null"
		}
		if resolved, ok := c.resolveName(expr.Value); ok {
			return resolved.typ
		}
	case *ast.CallExpression:
		if ident, ok := expr.Callee.(*ast.Identifier); ok {
			if classIndex, ok := c.classes[strings.ToLower(ident.Value)]; ok && c.chunk.Classes[classIndex].Name == ident.Value {
				return ident.Value
			}
		}
	case *ast.FunctionLiteral:
		return "func"
	case *ast.CastExpression:
		return c.bytecodeTypeName(expr.Type)
	}
	return ""
}

func (c *Compiler) staticTypeAssignable(target string, actual string) bool {
	if target == "" || strings.EqualFold(target, "any") || actual == "" {
		return true
	}
	// An `any` actual is statically opaque; runtime validation owns it.
	if strings.EqualFold(strings.TrimPrefix(actual, "?"), "any") {
		return true
	}
	// Union target: value is assignable when it matches any branch.
	if branches, ok := splitTopLevelTypeOp(target, '|'); ok {
		for _, b := range branches {
			if c.staticTypeAssignable(b, actual) {
				return true
			}
		}
		return false
	}
	// Intersection target: value is assignable only when it matches
	// every branch. (Rare at the parameter boundary; mainly here for
	// symmetry with the runtime check.)
	if branches, ok := splitTopLevelTypeOp(target, '&'); ok {
		for _, b := range branches {
			if !c.staticTypeAssignable(b, actual) {
				return false
			}
		}
		return true
	}
	// Union actual: every branch must be assignable to the target.
	if branches, ok := splitTopLevelTypeOp(actual, '|'); ok {
		for _, b := range branches {
			if !c.staticTypeAssignable(target, b) {
				return false
			}
		}
		return true
	}
	target = normalizeCallableTypeName(target)
	actual = normalizeCallableTypeName(actual)
	nullable := strings.HasPrefix(target, "?")
	if nullable {
		target = strings.TrimPrefix(target, "?")
	}
	if strings.EqualFold(actual, "null") {
		return nullable
	}
	actual = strings.TrimPrefix(actual, "?")
	if strings.EqualFold(target, actual) {
		return true
	}
	// Compare base type names by stripping generic type arguments. The
	// reified-generics element check is enforced at runtime; static checking
	// here only validates the base type compatibility.
	baseTarget := target
	if ltIdx := strings.Index(target, "<"); ltIdx >= 0 {
		baseTarget = target[:ltIdx]
	}
	baseActual := actual
	if ltIdx := strings.Index(actual, "<"); ltIdx >= 0 {
		baseActual = actual[:ltIdx]
	}
	if strings.EqualFold(baseTarget, baseActual) {
		return true
	}
	// generator and iterable are interchangeable type names: a function
	// declared to return generator<T> may be passed where iterable<T> is
	// expected, and vice versa. Mirrors isGeneratorTypeName at runtime.
	if isGeneratorTypeName(baseTarget) && isGeneratorTypeName(baseActual) {
		return true
	}
	return c.staticClassAssignable(baseTarget, baseActual)
}

func (c *Compiler) staticFunctionParamAssignable(function FunctionInfo, target string, actual string) bool {
	// Strip generic type arg ("list<T>" → "list") from both sides before
	// the type-name comparison. The element type comparison is handled by
	// the semantic analyzer for declarations and by reified-generics at
	// runtime.
	baseTarget := target
	if ltIdx := strings.Index(target, "<"); ltIdx >= 0 {
		baseTarget = target[:ltIdx]
	}
	baseActual := actual
	if ltIdx := strings.Index(actual, "<"); ltIdx >= 0 {
		baseActual = actual[:ltIdx]
	}
	if functionTypeParameterSetOrNil(function)[strings.ToLower(strings.TrimPrefix(baseTarget, "?"))] {
		return true
	}
	return c.staticTypeAssignable(baseTarget, baseActual)
}

func (c *Compiler) staticFunctionReturnAssignable(function FunctionInfo, expected string) bool {
	if functionTypeParameterSetOrNil(function)[strings.ToLower(strings.TrimPrefix(function.ReturnType, "?"))] {
		return true
	}
	return c.staticTypeAssignable(expected, function.ReturnType)
}

// staticallyResolveMethodCall returns the chunk-function index when
// the receiver's class is known statically, the named method
// resolves to a single non-decorated/non-async/non-generator
// overload, and no subclass overrides it.
func (c *Compiler) staticallyResolveMethodCall(selector *ast.SelectorExpression, args []ast.CallArgument) (int64, bool) {
	typeName := c.expressionStaticType(selector.Object)
	if typeName == "" {
		return 0, false
	}
	classIndex, ok := c.classes[strings.ToLower(typeName)]
	if !ok {
		return 0, false
	}
	// An exported class may be subclassed and its method overridden by an unseen importing module, so devirtualizing its call is unsound.
	if c.exportedClasses[strings.ToLower(typeName)] {
		return 0, false
	}
	classInfo := c.chunk.Classes[classIndex]
	if len(classInfo.TypeParameters) > 0 {
		return 0, false
	}
	if len(classInfo.Decorators) > 0 {
		return 0, false
	}
	methodName := selector.Name.Value
	indices, ok := classInfo.Methods[strings.ToLower(methodName)]
	if !ok || len(indices) != 1 {
		return 0, false
	}
	if c.classHasSubclassOverriding(classIndex, methodName) {
		return 0, false
	}
	fnIndex := indices[0]
	if fnIndex < 0 || int(fnIndex) >= len(c.chunk.Functions) {
		return 0, false
	}
	fn := c.chunk.Functions[fnIndex]
	if fn.Async || fn.IsGenerator || len(fn.Decorators) > 0 || fn.Variadic {
		return 0, false
	}
	if len(args) != len(fn.ParamSlots)-1 {
		return 0, false
	}
	if decorators, hasDecorators := classInfo.MethodDecorators[strings.ToLower(methodName)]; hasDecorators && len(decorators) > 0 {
		return 0, false
	}
	return fnIndex, true
}

func isStringLiteral(expr ast.Expression) bool {
	_, ok := expr.(*ast.StringLiteral)
	return ok
}

// selfStringConstAppendAssignment detects `name = name + "literal"`
// where both reads of `name` refer to the same identifier and the
// right operand of `+` is a string literal. Returns the literal's
// raw value on a match.
func selfStringConstAppendAssignment(name string, value ast.Expression) (string, bool) {
	infix, ok := value.(*ast.InfixExpression)
	if !ok || infix.Operator != "+" {
		return "", false
	}
	leftIdent, ok := infix.Left.(*ast.Identifier)
	if !ok || !strings.EqualFold(leftIdent.Value, name) {
		return "", false
	}
	lit, ok := infix.Right.(*ast.StringLiteral)
	if !ok {
		return "", false
	}
	return lit.Value, true
}

func isPrimitiveTypeForTCE(paramType string) bool {
	switch strings.ToLower(paramType) {
	case "", "any", "int", "bool", "float", "string", "decimal":
		return true
	}
	// Trim a leading "?" nullable marker; the underlying type still
	// needs primitive scalar validation.
	if strings.HasPrefix(paramType, "?") {
		return isPrimitiveTypeForTCE(paramType[1:])
	}
	return false
}

func functionRequiresParamValidation(function FunctionInfo) bool {
	for _, typ := range function.ParamTypes {
		if typ == "" {
			continue
		}
		if strings.EqualFold(typ, "any") {
			continue
		}
		return true
	}
	return false
}

func (c *Compiler) staticClassAssignable(target string, actual string) bool {
	classIndex, ok := c.classes[strings.ToLower(actual)]
	if !ok {
		return false
	}
	for {
		if classIndex < 0 || int(classIndex) >= len(c.chunk.Classes) {
			return false
		}
		classInfo := c.chunk.Classes[classIndex]
		if strings.EqualFold(classInfo.Name, target) {
			return true
		}
		for _, iface := range classInfo.Implements {
			if c.staticInterfaceAssignable(target, iface) {
				return true
			}
		}
		if classInfo.ParentIndex < 0 {
			return false
		}
		classIndex = classInfo.ParentIndex
	}
}

func (c *Compiler) staticInterfaceAssignable(target string, actual string) bool {
	if strings.EqualFold(target, actual) {
		return true
	}
	ifaceIndex, ok := c.interfaces[strings.ToLower(actual)]
	if !ok || ifaceIndex < 0 || int(ifaceIndex) >= len(c.chunk.Interfaces) {
		return false
	}
	for _, parent := range c.chunk.Interfaces[ifaceIndex].Parents {
		if c.staticInterfaceAssignable(target, parent) {
			return true
		}
	}
	return false
}

func (c *Compiler) compileOrderedArguments(function FunctionInfo, args []ast.Expression, paramOffset int, line int, column int) error {
	for i, arg := range args {
		if arg == nil {
			defaultIndex := function.DefaultConstants[i+paramOffset]
			c.emitAt(OpConstant, line, column, defaultIndex)
			continue
		}
		expectedParamType := ""
		paramIndex := i + paramOffset
		if paramIndex < len(function.ParamTypes) {
			t := function.ParamTypes[paramIndex]
			if t != "" && t != "any" {
				expectedParamType = t
			}
		} else if function.Variadic && len(function.ParamTypes) > 0 {
			t := function.ParamTypes[len(function.ParamTypes)-1]
			if t != "" && t != "any" {
				expectedParamType = t
			}
		}
		if err := c.compileExpressionWithExpected(arg, expectedParamType); err != nil {
			return err
		}
	}
	return nil
}

func positionalArguments(args []ast.CallArgument) []ast.Expression {
	ordered := make([]ast.Expression, 0, len(args))
	for _, arg := range args {
		if arg.Name != nil {
			return nil
		}
		ordered = append(ordered, arg.Value)
	}
	return ordered
}

func foldConstantBinary(op string, left, right ast.Expression) (runtime.Value, bool, error) {
	if lInt, ok := intLiteralValue(left); ok {
		if rInt, ok := intLiteralValue(right); ok {
			return foldIntInt(op, lInt, rInt)
		}
		if rFloat, ok := floatLiteralValue(right); ok {
			return foldFloatFloat(op, float64(lInt), rFloat)
		}
	}
	if lFloat, ok := floatLiteralValue(left); ok {
		if rFloat, ok := floatLiteralValue(right); ok {
			return foldFloatFloat(op, lFloat, rFloat)
		}
		if rInt, ok := intLiteralValue(right); ok {
			return foldFloatFloat(op, lFloat, float64(rInt))
		}
	}
	if lStr, ok := stringLiteralValue(left); ok {
		if rStr, ok := stringLiteralValue(right); ok {
			return foldStringString(op, lStr, rStr)
		}
	}
	if lBool, ok := boolLiteralValue(left); ok {
		if rBool, ok := boolLiteralValue(right); ok {
			return foldBoolBool(op, lBool, rBool)
		}
	}
	return nil, false, nil
}

func intLiteralValue(expr ast.Expression) (int64, bool) {
	lit, ok := expr.(*ast.IntegerLiteral)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(lit.Value, 0, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func floatLiteralValue(expr ast.Expression) (float64, bool) {
	lit, ok := expr.(*ast.FloatLiteral)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(lit.Value, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func stringLiteralValue(expr ast.Expression) (string, bool) {
	lit, ok := expr.(*ast.StringLiteral)
	if !ok || lit.Triple {
		return "", false
	}
	return lit.Value, true
}

func boolLiteralValue(expr ast.Expression) (bool, bool) {
	lit, ok := expr.(*ast.Literal)
	if !ok {
		return false, false
	}
	b, ok := lit.Value.(bool)
	if !ok {
		return false, false
	}
	return b, true
}

func foldIntInt(op string, l, r int64) (runtime.Value, bool, error) {
	switch op {
	case "+":
		return runtime.NewInt64(l + r), true, nil
	case "-":
		return runtime.NewInt64(l - r), true, nil
	case "*":
		return runtime.NewInt64(l * r), true, nil
	case "//":
		if r == 0 {
			// Defer to runtime so the throw stays catchable (eval parity).
			return nil, false, nil
		}
		// Floor semantics: Go's `/` truncates toward zero.
		q := l / r
		if (l^r) < 0 && q*r != l {
			q--
		}
		return runtime.NewInt64(q), true, nil
	case "%":
		if r == 0 {
			return nil, false, nil
		}
		// Floor semantics: sign of result follows divisor.
		m := l % r
		if m != 0 && (m^r) < 0 {
			m += r
		}
		return runtime.NewInt64(m), true, nil
	case "==":
		return runtime.Bool{Value: l == r}, true, nil
	case "!=":
		return runtime.Bool{Value: l != r}, true, nil
	case "<":
		return runtime.Bool{Value: l < r}, true, nil
	case "<=":
		return runtime.Bool{Value: l <= r}, true, nil
	case ">":
		return runtime.Bool{Value: l > r}, true, nil
	case ">=":
		return runtime.Bool{Value: l >= r}, true, nil
	}
	return nil, false, nil
}

func foldFloatFloat(op string, l, r float64) (runtime.Value, bool, error) {
	switch op {
	case "+":
		return runtime.Float{Value: l + r}, true, nil
	case "-":
		return runtime.Float{Value: l - r}, true, nil
	case "*":
		return runtime.Float{Value: l * r}, true, nil
	case "==":
		return runtime.Bool{Value: l == r}, true, nil
	case "!=":
		return runtime.Bool{Value: l != r}, true, nil
	case "<":
		return runtime.Bool{Value: l < r}, true, nil
	case "<=":
		return runtime.Bool{Value: l <= r}, true, nil
	case ">":
		return runtime.Bool{Value: l > r}, true, nil
	case ">=":
		return runtime.Bool{Value: l >= r}, true, nil
	}
	return nil, false, nil
}

func foldStringString(op string, l, r string) (runtime.Value, bool, error) {
	switch op {
	case "+":
		return runtime.String{Value: l + r}, true, nil
	case "==":
		return runtime.Bool{Value: l == r}, true, nil
	case "!=":
		return runtime.Bool{Value: l != r}, true, nil
	case "<":
		return runtime.Bool{Value: l < r}, true, nil
	case "<=":
		return runtime.Bool{Value: l <= r}, true, nil
	case ">":
		return runtime.Bool{Value: l > r}, true, nil
	case ">=":
		return runtime.Bool{Value: l >= r}, true, nil
	}
	return nil, false, nil
}

func foldBoolBool(op string, l, r bool) (runtime.Value, bool, error) {
	switch op {
	case "==":
		return runtime.Bool{Value: l == r}, true, nil
	case "!=":
		return runtime.Bool{Value: l != r}, true, nil
	}
	return nil, false, nil
}

func (c *Compiler) compileAssertCall(expr *ast.CallExpression) error {
	if len(expr.Arguments) < 1 || len(expr.Arguments) > 2 {
		return fmt.Errorf("assert expects (condition) or (condition, message)")
	}
	for _, arg := range expr.Arguments {
		if arg.Name != nil {
			return fmt.Errorf("assert does not accept named arguments")
		}
	}
	line, col := expr.Token.Line, expr.Token.Column
	if c.AssertionsDisabled {
		c.emitConstant(runtime.Null{}, line, col)
		return nil
	}
	cond := expr.Arguments[0].Value
	if err := c.compileExpression(cond); err != nil {
		return err
	}
	c.emitAt(OpNot, line, col)
	jumpEnd := c.emitJump(OpJumpIfFalse, line, col)
	if len(expr.Arguments) == 2 {
		if err := c.compileExpression(expr.Arguments[1].Value); err != nil {
			return err
		}
	} else {
		c.emitConstant(runtime.String{Value: "assertion failed: " + cond.String()}, line, col)
	}
	classIndex := int64(len(c.chunk.Constants))
	c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: "AssertionError"})
	c.emitAt(OpMakeError, line, col, classIndex, 1)
	c.emitAt(OpThrow, line, col)
	c.patchJump(jumpEnd)
	c.emitConstant(runtime.Null{}, line, col)
	return nil
}

// compileConditionAndJumpIfFalse compiles cond and emits a jump that fires
// when cond is logically false. When cond is a typed-int infix comparison,
// emits a single fused OpJumpIfNot<Cmp>Int opcode (Phase 11) and returns its
// patch position. Otherwise emits the unfused [cond; OpJumpIfFalse] pair.
func (c *Compiler) compileConditionAndJumpIfFalse(cond ast.Expression, line, column int) (int, error) {
	if infix, ok := cond.(*ast.InfixExpression); ok {
		if slot, divisor, op, ok := c.tryFuseModZeroBranch(infix); ok {
			c.emitAt(op, line, column, -1, slot, divisor)
			return len(c.chunk.Instructions) - 1, nil
		}
		fusedOp, fused := fusedCompareJumpOpFor(infix.Operator)
		if fused && c.staticIntExpr(infix.Left) && c.staticIntExpr(infix.Right) {
			if err := c.compileExpression(infix.Left); err != nil {
				return 0, err
			}
			if err := c.compileExpression(infix.Right); err != nil {
				return 0, err
			}
			return c.emitJump(fusedOp, line, column), nil
		}
	}
	if err := c.compileExpression(cond); err != nil {
		return 0, err
	}
	return c.emitJump(OpJumpIfFalse, line, column), nil
}

// tryFuseModZeroBranch matches `<local> % <const_int> == 0` (and !=, and the
// reversed `0 == <local>%<const>` form). On a match returns the local slot,
// divisor, and the matching fused opcode (NotZero opcode for `==`, Zero
// opcode for `!=`, because both jump when the body should be skipped).
func (c *Compiler) tryFuseModZeroBranch(infix *ast.InfixExpression) (int64, int64, Op, bool) {
	if infix.Operator != "==" && infix.Operator != "!=" {
		return 0, 0, 0, false
	}
	modExpr, zero := infix.Left, infix.Right
	if !isIntZeroLiteral(zero) {
		modExpr, zero = infix.Right, infix.Left
		if !isIntZeroLiteral(zero) {
			return 0, 0, 0, false
		}
	}
	mod, ok := modExpr.(*ast.InfixExpression)
	if !ok || mod.Operator != "%" {
		return 0, 0, 0, false
	}
	ident, ok := mod.Left.(*ast.Identifier)
	if !ok {
		return 0, 0, 0, false
	}
	b, found := c.resolveName(ident.Value)
	if !found || b.kind != "local" || b.typ != "int" {
		return 0, 0, 0, false
	}
	divLit, ok := mod.Right.(*ast.IntegerLiteral)
	if !ok {
		return 0, 0, 0, false
	}
	divisor, err := strconv.ParseInt(divLit.Value, 10, 64)
	if err != nil || divisor == 0 {
		return 0, 0, 0, false
	}
	op := OpJumpIfModNotZero
	if infix.Operator == "!=" {
		op = OpJumpIfModZero
	}
	return b.slot, divisor, op, true
}

func isIntZeroLiteral(e ast.Expression) bool {
	lit, ok := e.(*ast.IntegerLiteral)
	if !ok {
		return false
	}
	v, err := strconv.ParseInt(lit.Value, 10, 64)
	return err == nil && v == 0
}

// fusedCompareJumpOpFor maps an infix comparison operator to the corresponding
// fused compare-and-skip-body opcode. Returns (op, true) on match.
func fusedCompareJumpOpFor(operator string) (Op, bool) {
	switch operator {
	case "<":
		return OpJumpIfNotLessInt, true
	case "<=":
		return OpJumpIfNotLessEqualInt, true
	case ">":
		return OpJumpIfNotGreaterInt, true
	case ">=":
		return OpJumpIfNotGreaterEqualInt, true
	case "==":
		return OpJumpIfNotEqualInt, true
	case "!=":
		return OpJumpIfEqualInt, true
	}
	return 0, false
}

// staticIntExpr reports whether expr is provably of static type "int" at
// compile time. Used to decide whether to emit type-specialized integer
// opcodes (OpAddInt, OpSubInt, etc.).
func (c *Compiler) staticIntExpr(expr ast.Expression) bool {
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return true
	case *ast.Identifier:
		b, ok := c.resolveName(e.Value)
		return ok && b.typ == "int"
	case *ast.InfixExpression:
		switch e.Operator {
		case "+", "-", "*", "//", "%":
			return c.staticIntExpr(e.Left) && c.staticIntExpr(e.Right)
		}
	case *ast.PrefixExpression:
		if e.Operator == "-" {
			return c.staticIntExpr(e.Right)
		}
	case *ast.CallExpression:
		return c.callExprReturnsType(e, "int")
	case *ast.CastExpression:
		return strings.EqualFold(c.bytecodeTypeName(e.Type), "int")
	case *ast.SelectorExpression:
		return c.selectorReturnsType(e, "int")
	}
	return false
}

// selectorReturnsType reports whether `obj.field` resolves to a class
// field whose declared type matches `want`. Lets staticIntExpr (and
// friends) propagate int/string/bool through `this.fieldname` access
// inside methods of typed classes.
func (c *Compiler) selectorReturnsType(sel *ast.SelectorExpression, want string) bool {
	typeName := c.staticReceiverType(sel.Object)
	if typeName == "" {
		return false
	}
	classIndex, ok := c.classes[strings.ToLower(typeName)]
	if !ok {
		return false
	}
	classInfo := c.chunk.Classes[classIndex]
	for i, name := range classInfo.FieldNames {
		if strings.EqualFold(name, sel.Name.Value) {
			if i < len(classInfo.FieldTypes) {
				return strings.EqualFold(classInfo.FieldTypes[i], want)
			}
			return false
		}
	}
	return false
}

// staticReceiverType returns the class name of `expr` when it can be
// determined at compile time. Handles plain identifiers (via
// expressionStaticType) and the `this` keyword (via the enclosing
// class stack).
func (c *Compiler) staticReceiverType(expr ast.Expression) string {
	if ident, ok := expr.(*ast.Identifier); ok && strings.EqualFold(ident.Value, "this") {
		if len(c.classStack) == 0 {
			return ""
		}
		idx := c.classStack[len(c.classStack)-1]
		if idx < 0 || int(idx) >= len(c.chunk.Classes) {
			return ""
		}
		return c.chunk.Classes[idx].Name
	}
	return c.expressionStaticType(expr)
}

// callExprReturnsType reports whether a call to `e` is provably of the
// given return type. Handles direct-name function calls and
// `this.method()` / `typedReceiver.method()` style method calls.
func (c *Compiler) callExprReturnsType(e *ast.CallExpression, want string) bool {
	if ident, ok := e.Callee.(*ast.Identifier); ok {
		indices, found := c.funcs[strings.ToLower(ident.Value)]
		if !found || len(indices) == 0 {
			return false
		}
		for _, idx := range indices {
			if idx < 0 || int(idx) >= len(c.chunk.Functions) {
				return false
			}
			if !strings.EqualFold(c.chunk.Functions[idx].ReturnType, want) {
				return false
			}
		}
		return true
	}
	if sel, ok := e.Callee.(*ast.SelectorExpression); ok {
		typeName := c.expressionStaticType(sel.Object)
		if typeName == "" {
			return false
		}
		classIndex, ok := c.classes[strings.ToLower(typeName)]
		if !ok {
			return false
		}
		classInfo := c.chunk.Classes[classIndex]
		indices, ok := classInfo.Methods[strings.ToLower(sel.Name.Value)]
		if !ok || len(indices) == 0 {
			return false
		}
		for _, idx := range indices {
			if idx < 0 || int(idx) >= len(c.chunk.Functions) {
				return false
			}
			if !strings.EqualFold(c.chunk.Functions[idx].ReturnType, want) {
				return false
			}
		}
		return true
	}
	return false
}

// staticStringExpr reports whether expr is provably of static type
// `string` at compile time. Used to decide whether to emit OpAddString
// for `string + string`, mirroring staticIntExpr for the int arith
// opcodes. Nested concatenations like `"a" + b + "c"` produce a static
// string when every leaf is statically typed.
func (c *Compiler) staticStringExpr(expr ast.Expression) bool {
	switch e := expr.(type) {
	case *ast.StringLiteral:
		return true
	case *ast.Identifier:
		b, ok := c.resolveName(e.Value)
		return ok && b.typ == "string"
	case *ast.InfixExpression:
		if e.Operator == "+" {
			return c.staticStringExpr(e.Left) && c.staticStringExpr(e.Right)
		}
	case *ast.CallExpression:
		return c.callExprReturnsType(e, "string")
	case *ast.SelectorExpression:
		return c.selectorReturnsType(e, "string")
	case *ast.CastExpression:
		return strings.EqualFold(c.bytecodeTypeName(e.Type), "string")
	}
	return false
}

func callSpreadIndex(args []ast.CallArgument) (int, bool) {
	for i, arg := range args {
		if arg.Spread {
			return i, true
		}
	}
	return -1, false
}

// compileSpreadCallArguments compiles every argument and returns one meta operand each (spread / positional / name index) so trailing and named args survive.
func (c *Compiler) compileSpreadCallArguments(args []ast.CallArgument) ([]int64, error) {
	meta := make([]int64, 0, len(args))
	for _, arg := range args {
		if err := c.compileExpression(arg.Value); err != nil {
			return nil, err
		}
		switch {
		case arg.Spread:
			meta = append(meta, spreadArgMeta)
		case arg.Name != nil:
			idx := int64(len(c.chunk.Constants))
			c.chunk.Constants = append(c.chunk.Constants, runtime.String{Value: arg.Name.Value})
			meta = append(meta, idx)
		default:
			meta = append(meta, positionalArgMeta)
		}
	}
	return meta, nil
}

func listHasSpread(elements []ast.Expression) bool {
	for _, e := range elements {
		if _, ok := e.(*ast.SpreadExpression); ok {
			return true
		}
	}
	return false
}

func (c *Compiler) selectFunctionCallSpread(name string) (int64, error) {
	indices := c.funcs[strings.ToLower(name)]
	if len(indices) == 0 {
		return 0, fmt.Errorf("unknown function %s", name)
	}
	if len(indices) > 1 {
		return 0, fmt.Errorf("cannot use spread with overloaded function %s", name)
	}
	return indices[0], nil
}
