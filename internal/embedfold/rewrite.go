package embedfold

import "geblang/internal/ast"

func rewriteProgram(program *ast.Program, f *folder) {
	for _, stmt := range program.Statements {
		rewriteStmt(stmt, f)
	}
}

func rewriteBlock(block *ast.BlockStatement, f *folder) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		rewriteStmt(stmt, f)
	}
}

func rewriteStmt(s ast.Statement, f *folder) {
	if s == nil {
		return
	}
	switch n := s.(type) {
	case *ast.BlockStatement:
		rewriteBlock(n, f)
	case *ast.ExportStatement:
		rewriteStmt(n.Statement, f)
	case *ast.InitStatement:
		rewriteBlock(n.Body, f)
	case *ast.DeclarationStatement:
		n.Value = rewriteExpr(n.Value, f)
		rewriteDecorators(n.Decorators, f)
	case *ast.DestructuringStatement:
		n.Value = rewriteExpr(n.Value, f)
	case *ast.ExpressionStatement:
		n.Expression = rewriteExpr(n.Expression, f)
	case *ast.ReturnStatement:
		n.Value = rewriteExpr(n.Value, f)
	case *ast.YieldStatement:
		n.Value = rewriteExpr(n.Value, f)
	case *ast.SimpleStatement:
		n.Value = rewriteExpr(n.Value, f)
	case *ast.IfStatement:
		n.Condition = rewriteExpr(n.Condition, f)
		rewriteBlock(n.Consequence, f)
		for i := range n.ElseIfs {
			n.ElseIfs[i].Condition = rewriteExpr(n.ElseIfs[i].Condition, f)
			rewriteBlock(n.ElseIfs[i].Body, f)
		}
		rewriteBlock(n.Alternative, f)
	case *ast.WhileStatement:
		n.Condition = rewriteExpr(n.Condition, f)
		rewriteBlock(n.Body, f)
	case *ast.ForStatement:
		rewriteStmt(n.Init, f)
		n.Condition = rewriteExpr(n.Condition, f)
		rewriteStmt(n.Update, f)
		n.Iterable = rewriteExpr(n.Iterable, f)
		n.Step = rewriteExpr(n.Step, f)
		rewriteBlock(n.Body, f)
	case *ast.WithStatement:
		n.Value = rewriteExpr(n.Value, f)
		rewriteBlock(n.Body, f)
	case *ast.TryStatement:
		rewriteBlock(n.Body, f)
		for i := range n.Catches {
			rewriteBlock(n.Catches[i].Body, f)
		}
		rewriteBlock(n.Finally, f)
	case *ast.MatchStatement:
		n.Expr = rewriteExpr(n.Expr, f)
		rewriteCases(n.Cases, f)
	case *ast.SelectStatement:
		for i := range n.Cases {
			n.Cases[i].Channel = rewriteExpr(n.Cases[i].Channel, f)
			n.Cases[i].Value = rewriteExpr(n.Cases[i].Value, f)
			rewriteBlock(n.Cases[i].Body, f)
		}
		rewriteBlock(n.Default, f)
	case *ast.FunctionStatement:
		rewriteFunction(n, f)
	case *ast.ClassStatement:
		rewriteDecorators(n.Decorators, f)
		for _, member := range n.Members {
			rewriteStmt(member, f)
		}
		rewriteFunction(n.Destructor, f)
	case *ast.InterfaceStatement:
		for _, sig := range n.Methods {
			if sig != nil {
				rewriteParams(sig.Parameters, f)
			}
		}
		for _, def := range n.Defaults {
			rewriteFunction(def, f)
		}
		for _, field := range n.Fields {
			rewriteStmt(field, f)
		}
	case *ast.EnumStatement:
		for i := range n.Variants {
			n.Variants[i].BackingValue = rewriteExpr(n.Variants[i].BackingValue, f)
		}
		for _, method := range n.Methods {
			rewriteFunction(method, f)
		}
	}
}

func rewriteFunction(fn *ast.FunctionStatement, f *folder) {
	if fn == nil {
		return
	}
	rewriteDecorators(fn.Decorators, f)
	rewriteParams(fn.Parameters, f)
	rewriteBlock(fn.Body, f)
}

func rewriteParams(params []ast.Parameter, f *folder) {
	for i := range params {
		params[i].Default = rewriteExpr(params[i].Default, f)
		rewriteDecorators(params[i].Decorators, f)
	}
}

// ClassStatement.FieldDecorators aliases each field declaration's own slice, so rewriting it again would double-report.
func rewriteDecorators(decorators []ast.Decorator, f *folder) {
	for i := range decorators {
		rewriteArgs(decorators[i].Arguments, f)
	}
}

func rewriteArgs(args []ast.CallArgument, f *folder) {
	for i := range args {
		args[i].Value = rewriteExpr(args[i].Value, f)
	}
}

func rewriteCases(cases []ast.MatchCase, f *folder) {
	for i := range cases {
		cases[i].Pattern = rewriteExpr(cases[i].Pattern, f)
		cases[i].Guard = rewriteExpr(cases[i].Guard, f)
		cases[i].Value = rewriteExpr(cases[i].Value, f)
		for j := range cases[i].Alternates {
			cases[i].Alternates[j] = rewriteExpr(cases[i].Alternates[j], f)
		}
		if pattern := cases[i].ListPattern; pattern != nil {
			for j := range pattern.Bindings {
				pattern.Bindings[j].Literal = rewriteExpr(pattern.Bindings[j].Literal, f)
			}
		}
		rewriteBlock(cases[i].Body, f)
	}
}

func rewriteClauses(clauses []ast.ComprehensionClause, f *folder) {
	for _, clause := range clauses {
		switch c := clause.(type) {
		case *ast.ComprehensionFor:
			c.Iterable = rewriteExpr(c.Iterable, f)
		case *ast.ComprehensionIf:
			c.Filter = rewriteExpr(c.Filter, f)
		}
	}
}

func rewriteExpr(e ast.Expression, f *folder) ast.Expression {
	if e == nil {
		return nil
	}
	switch n := e.(type) {
	case *ast.InterpolatedString:
		for i := range n.Parts {
			n.Parts[i] = rewriteExpr(n.Parts[i], f)
		}
	case *ast.FormattedInterpolation:
		n.Value = rewriteExpr(n.Value, f)
	case *ast.ListLiteral:
		for i := range n.Elements {
			n.Elements[i] = rewriteExpr(n.Elements[i], f)
		}
	case *ast.SetLiteral:
		for i := range n.Elements {
			n.Elements[i] = rewriteExpr(n.Elements[i], f)
		}
	case *ast.DictLiteral:
		for i := range n.Entries {
			n.Entries[i].Key = rewriteExpr(n.Entries[i].Key, f)
			n.Entries[i].Value = rewriteExpr(n.Entries[i].Value, f)
		}
	case *ast.ListComprehension:
		n.Body = rewriteExpr(n.Body, f)
		rewriteClauses(n.Clauses, f)
	case *ast.SetComprehension:
		n.Body = rewriteExpr(n.Body, f)
		rewriteClauses(n.Clauses, f)
	case *ast.DictComprehension:
		n.KeyBody = rewriteExpr(n.KeyBody, f)
		n.ValueBody = rewriteExpr(n.ValueBody, f)
		rewriteClauses(n.Clauses, f)
	case *ast.PrefixExpression:
		n.Right = rewriteExpr(n.Right, f)
	case *ast.PostfixExpression:
		n.Left = rewriteExpr(n.Left, f)
	case *ast.InfixExpression:
		n.Left = rewriteExpr(n.Left, f)
		n.Right = rewriteExpr(n.Right, f)
	case *ast.AssignmentExpression:
		n.Left = rewriteExpr(n.Left, f)
		n.Value = rewriteExpr(n.Value, f)
	case *ast.SelectorExpression:
		n.Object = rewriteExpr(n.Object, f)
	case *ast.IndexExpression:
		n.Left = rewriteExpr(n.Left, f)
		n.Index = rewriteExpr(n.Index, f)
	case *ast.SpreadExpression:
		n.Value = rewriteExpr(n.Value, f)
	case *ast.RangeExpression:
		n.Start = rewriteExpr(n.Start, f)
		n.End = rewriteExpr(n.End, f)
		n.Step = rewriteExpr(n.Step, f)
	case *ast.PipeExpression:
		n.Left = rewriteExpr(n.Left, f)
		n.Right = rewriteExpr(n.Right, f)
	case *ast.CallExpression:
		if _, isEmbed := embedCallee(n.Callee); !isEmbed {
			n.Callee = rewriteExpr(n.Callee, f)
		}
		rewriteArgs(n.Arguments, f)
	case *ast.PartialExpression:
		n.Callee = rewriteExpr(n.Callee, f)
		rewriteArgs(n.Arguments, f)
	case *ast.FunctionLiteral:
		rewriteParams(n.Parameters, f)
		rewriteBlock(n.Body, f)
	case *ast.AwaitExpression:
		n.Value = rewriteExpr(n.Value, f)
	case *ast.CastExpression:
		n.Value = rewriteExpr(n.Value, f)
	case *ast.TernaryExpression:
		n.Condition = rewriteExpr(n.Condition, f)
		n.ThenExpr = rewriteExpr(n.ThenExpr, f)
		n.ElseExpr = rewriteExpr(n.ElseExpr, f)
	case *ast.MatchExpression:
		n.Expr = rewriteExpr(n.Expr, f)
		rewriteCases(n.Cases, f)
	}
	return f.visit(e)
}
