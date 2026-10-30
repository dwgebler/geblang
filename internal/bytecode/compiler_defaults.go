package bytecode

import (
	"geblang/internal/ast"
	"geblang/internal/runtime"
)

type computedDefault struct {
	paramIndex int
	slot       int64
	expr       ast.Expression
}

// parameterDefaultConstant stores a literal default, or a pending marker the callee prologue resolves.
func (c *Compiler) parameterDefaultConstant(def ast.Expression) (int64, bool) {
	value, err := c.constantValueFromExpression(def)
	computed := err != nil
	if computed {
		value = runtime.PendingDefault{}
	}
	c.chunk.Constants = append(c.chunk.Constants, value)
	return int64(len(c.chunk.Constants) - 1), computed
}

// compileComputedDefaults evaluates pending defaults in order; later parameters stay out of scope, as in the evaluator.
func (c *Compiler) compileComputedDefaults(funcIndex int64, params []ast.Parameter, computed []computedDefault, preParams map[string]binding) error {
	scope := c.scopes[len(c.scopes)-1]
	for _, d := range computed {
		position := 0
		for position < len(params) && params[position].Default != d.expr {
			position++
		}
		line, column := params[position].Name.Token.Line, params[position].Name.Token.Column
		saved := map[string]binding{}
		for _, later := range params[position:] {
			name := later.Name.Value
			saved[name] = scope[name]
			if outer, ok := preParams[name]; ok {
				scope[name] = outer
			} else {
				delete(scope, name)
			}
		}
		c.emitAt(OpJumpIfArgPresent, line, column, -1, d.slot)
		skip := len(c.chunk.Instructions) - 1
		err := c.compileExpression(d.expr)
		for name, b := range saved {
			scope[name] = b
		}
		if err != nil {
			return err
		}
		c.emitAt(OpStoreDefaultArg, line, column, d.slot, funcIndex, int64(d.paramIndex))
		c.patchJump(skip)
	}
	return nil
}

// compileFieldDefaultFunction compiles a non-literal field default as a hidden module function of the new instance, named after its class for traces.
func (c *Compiler) compileFieldDefaultFunction(classIndex int64, className string, fieldIndex int, member *ast.DeclarationStatement, expected string) (int64, error) {
	line, column := member.Token.Line, member.Token.Column
	index := c.declareFunction("<default:" + className + "." + member.Name.Value + ">")
	c.chunk.Functions[index].Name = className
	skip := c.emitJump(OpJump, line, column)
	c.chunk.Functions[index].Entry = int64(len(c.chunk.Instructions))
	c.chunk.Functions[index].DefLine = int64(line)
	c.chunk.Functions[index].DefColumn = int64(column)
	c.pushScope()
	c.pushFunctionLocals()
	instanceSlot := c.defineLocalWithType("<instance>", "")
	c.chunk.Functions[index].ParamNames = []string{"<instance>"}
	c.chunk.Functions[index].ParamDisplayNames = []string{"<instance>"}
	c.chunk.Functions[index].ParamSlots = []int64{instanceSlot}
	c.chunk.Functions[index].ParamTypes = []string{""}
	c.chunk.Functions[index].ParamDisplayTypes = []string{"any"}
	c.chunk.Functions[index].DefaultConstants = []int64{-1}
	c.inFunc++
	c.returnTypes = append(c.returnTypes, "")
	savedLoops, savedFinalizers, savedFuncIndex := c.loops, c.finalizers, c.currentFuncIndex
	c.loops, c.finalizers, c.currentFuncIndex = nil, nil, index
	err := c.compileExpressionWithExpected(member.Value, expected)
	c.loops, c.finalizers, c.currentFuncIndex = savedLoops, savedFinalizers, savedFuncIndex
	c.emitAt(OpCheckFieldDefault, line, column, classIndex, int64(fieldIndex))
	c.emitAt(OpReturn, line, column)
	c.chunk.Functions[index].LocalCount = c.popFunctionLocals()
	c.inFunc--
	c.returnTypes = c.returnTypes[:len(c.returnTypes)-1]
	c.popScope()
	c.patchJump(skip)
	return index, err
}
