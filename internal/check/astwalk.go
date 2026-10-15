package check

import (
	"sort"

	"geblang/internal/ast"
)

// astVisitor is the one traversal every check pass shares; TestASTWalkerCoversEveryNode guards it.
type astVisitor struct {
	expr       func(ast.Expression)
	typeRef    func(*ast.TypeRef)
	typeParams func([]*ast.TypeParam)
	binding    func(string)
	block      func(*ast.BlockStatement)
}

func (v *astVisitor) program(program *ast.Program) {
	for _, stmt := range program.Statements {
		v.stmt(stmt)
	}
}

func (v *astVisitor) bind(id *ast.Identifier) {
	if id != nil && id.Value != "" && v.binding != nil {
		v.binding(id.Value)
	}
}

func (v *astVisitor) typ(ref *ast.TypeRef) {
	if ref != nil && v.typeRef != nil {
		v.typeRef(ref)
	}
}

func (v *astVisitor) generics(params []*ast.TypeParam) {
	if len(params) == 0 {
		return
	}
	if v.typeParams != nil {
		v.typeParams(params)
	}
	for _, param := range params {
		if param != nil {
			v.typ(param.Constraint)
		}
	}
}

func (v *astVisitor) ident(id *ast.Identifier) {
	if id != nil {
		v.expression(id)
	}
}

func (v *astVisitor) body(block *ast.BlockStatement) {
	if block == nil {
		return
	}
	if v.block != nil {
		v.block(block)
	}
	for _, stmt := range block.Statements {
		v.stmt(stmt)
	}
}

func (v *astVisitor) decorators(decorators []ast.Decorator) {
	for _, decorator := range decorators {
		v.ident(decorator.Name)
		v.arguments(decorator.Arguments)
	}
}

func (v *astVisitor) arguments(args []ast.CallArgument) {
	for _, arg := range args {
		if !arg.Hole {
			v.expression(arg.Value)
		}
	}
}

func (v *astVisitor) parameters(params []ast.Parameter) {
	for _, param := range params {
		v.decorators(param.Decorators)
		v.typ(param.Type)
		v.bind(param.Name)
		v.expression(param.Default)
	}
}

func (v *astVisitor) function(fn *ast.FunctionStatement) {
	if fn == nil {
		return
	}
	v.decorators(fn.Decorators)
	v.generics(fn.Generics)
	v.parameters(fn.Parameters)
	v.typ(fn.ReturnType)
	v.body(fn.Body)
}

func (v *astVisitor) matchCases(cases []ast.MatchCase) {
	for _, clause := range cases {
		v.expression(clause.Pattern)
		for _, alt := range clause.Alternates {
			v.expression(alt)
		}
		v.typ(clause.Type)
		v.bind(clause.Name)
		if clause.EnumVariant != nil {
			v.ident(clause.EnumVariant.Enum)
			for _, param := range clause.EnumVariant.Params {
				v.typ(param.Type)
				v.bind(param.Name)
			}
		}
		if clause.ListPattern != nil {
			for _, binding := range clause.ListPattern.Bindings {
				v.typ(binding.Type)
				v.bind(binding.Name)
				v.expression(binding.Literal)
			}
		}
		v.expression(clause.Guard)
		v.expression(clause.Value)
		v.body(clause.Body)
	}
}

func (v *astVisitor) comprehension(clauses []ast.ComprehensionClause) {
	for _, clause := range clauses {
		switch c := clause.(type) {
		case *ast.ComprehensionFor:
			v.typ(c.VarType)
			v.bind(c.VarName)
			for _, name := range c.VarNames {
				v.bind(name)
			}
			v.expression(c.Iterable)
		case *ast.ComprehensionIf:
			v.expression(c.Filter)
		}
	}
}

func (v *astVisitor) stmt(stmt ast.Statement) {
	switch s := stmt.(type) {
	case nil:
	case *ast.ExportStatement:
		v.stmt(s.Statement)
	case *ast.ModuleStatement, *ast.ImportStatement, *ast.FromImportStatement, *ast.DelStatement:
	case *ast.DeclarationStatement:
		v.decorators(s.Decorators)
		v.typ(s.Type)
		v.bind(s.Name)
		v.expression(s.Value)
	case *ast.DestructuringStatement:
		for _, name := range s.Names {
			v.bind(name)
		}
		v.expression(s.Value)
	case *ast.ExpressionStatement:
		v.expression(s.Expression)
	case *ast.ReturnStatement:
		v.expression(s.Value)
	case *ast.YieldStatement:
		v.expression(s.Value)
	case *ast.SimpleStatement:
		v.expression(s.Value)
	case *ast.BlockStatement:
		v.body(s)
	case *ast.IfStatement:
		v.expression(s.Condition)
		v.body(s.Consequence)
		for _, clause := range s.ElseIfs {
			v.expression(clause.Condition)
			v.body(clause.Body)
		}
		v.body(s.Alternative)
	case *ast.WhileStatement:
		v.expression(s.Condition)
		v.body(s.Body)
	case *ast.ForStatement:
		v.stmt(s.Init)
		v.expression(s.Condition)
		v.stmt(s.Update)
		v.typ(s.VarType)
		v.bind(s.VarName)
		for _, name := range s.VarNames {
			v.bind(name)
		}
		v.expression(s.Iterable)
		v.expression(s.Step)
		v.body(s.Body)
	case *ast.WithStatement:
		v.expression(s.Value)
		v.bind(s.Name)
		v.body(s.Body)
	case *ast.SelectStatement:
		for _, clause := range s.Cases {
			v.expression(clause.Channel)
			v.expression(clause.Value)
			if clause.Binding != "" && v.binding != nil {
				v.binding(clause.Binding)
			}
			v.body(clause.Body)
		}
		v.body(s.Default)
	case *ast.TryStatement:
		v.body(s.Body)
		for _, clause := range s.Catches {
			v.typ(clause.Type)
			v.bind(clause.Name)
			v.body(clause.Body)
		}
		v.body(s.Finally)
	case *ast.MatchStatement:
		v.expression(s.Expr)
		v.matchCases(s.Cases)
	case *ast.FunctionStatement:
		v.function(s)
	case *ast.InitStatement:
		v.body(s.Body)
	case *ast.TypeAliasStatement:
		v.typ(s.Type)
	case *ast.ClassStatement:
		v.decorators(s.Decorators)
		v.generics(s.Generics)
		v.typ(s.Extends)
		for _, typ := range s.Implements {
			v.typ(typ)
		}
		fields := make([]string, 0, len(s.FieldDecorators))
		for field := range s.FieldDecorators {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		for _, field := range fields {
			v.decorators(s.FieldDecorators[field])
		}
		for _, member := range s.Members {
			v.stmt(member)
		}
		v.function(s.Destructor)
	case *ast.InterfaceStatement:
		v.generics(s.Generics)
		for _, parent := range s.Parents {
			v.typ(parent)
		}
		for _, sig := range s.Methods {
			if sig != nil {
				v.generics(sig.Generics)
				v.parameters(sig.Parameters)
				v.typ(sig.ReturnType)
			}
		}
		for _, method := range s.Defaults {
			v.function(method)
		}
		for _, field := range s.Fields {
			if field != nil {
				v.stmt(field)
			}
		}
	case *ast.EnumStatement:
		v.typ(s.BackingType)
		for _, typ := range s.Implements {
			v.typ(typ)
		}
		for _, variant := range s.Variants {
			for _, typ := range variant.FieldTypes {
				v.typ(typ)
			}
			v.expression(variant.BackingValue)
		}
		for _, method := range s.Methods {
			v.function(method)
		}
	}
}

func (v *astVisitor) expression(expr ast.Expression) {
	if expr == nil {
		return
	}
	if v.expr != nil {
		v.expr(expr)
	}
	switch e := expr.(type) {
	case *ast.Identifier, *ast.IntegerLiteral, *ast.FloatLiteral, *ast.DecimalLiteral,
		*ast.StringLiteral, *ast.Literal, *ast.EmbeddedLiteral, *ast.PrecomputedValue:
	case *ast.SpreadExpression:
		v.expression(e.Value)
	case *ast.AwaitExpression:
		v.expression(e.Value)
	case *ast.FormattedInterpolation:
		v.expression(e.Value)
	case *ast.InterpolatedString:
		for _, part := range e.Parts {
			v.expression(part)
		}
	case *ast.PrefixExpression:
		v.expression(e.Right)
	case *ast.PostfixExpression:
		v.expression(e.Left)
	case *ast.InfixExpression:
		v.expression(e.Left)
		v.expression(e.Right)
		v.typ(e.RightType)
	case *ast.AssignmentExpression:
		v.expression(e.Left)
		v.expression(e.Value)
	case *ast.SelectorExpression:
		v.expression(e.Object)
	case *ast.CallExpression:
		for _, typ := range e.TypeArguments {
			v.typ(typ)
		}
		v.expression(e.Callee)
		v.arguments(e.Arguments)
	case *ast.PartialExpression:
		for _, typ := range e.TypeArguments {
			v.typ(typ)
		}
		v.expression(e.Callee)
		v.arguments(e.Arguments)
	case *ast.IndexExpression:
		v.expression(e.Left)
		v.expression(e.Index)
	case *ast.ListLiteral:
		for _, element := range e.Elements {
			v.expression(element)
		}
	case *ast.SetLiteral:
		for _, element := range e.Elements {
			v.expression(element)
		}
	case *ast.DictLiteral:
		for _, entry := range e.Entries {
			v.expression(entry.Key)
			v.expression(entry.Value)
		}
	case *ast.RangeExpression:
		v.expression(e.Start)
		v.expression(e.End)
		v.expression(e.Step)
	case *ast.FunctionLiteral:
		v.parameters(e.Parameters)
		v.typ(e.ReturnType)
		v.body(e.Body)
	case *ast.MatchExpression:
		v.expression(e.Expr)
		v.matchCases(e.Cases)
	case *ast.CastExpression:
		v.expression(e.Value)
		v.typ(e.Type)
	case *ast.TernaryExpression:
		v.expression(e.Condition)
		v.expression(e.ThenExpr)
		v.expression(e.ElseExpr)
	case *ast.PipeExpression:
		v.expression(e.Left)
		v.expression(e.Right)
	case *ast.ListComprehension:
		v.comprehension(e.Clauses)
		v.expression(e.Body)
	case *ast.SetComprehension:
		v.comprehension(e.Clauses)
		v.expression(e.Body)
	case *ast.DictComprehension:
		v.comprehension(e.Clauses)
		v.expression(e.KeyBody)
		v.expression(e.ValueBody)
	}
}
