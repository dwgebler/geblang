package arityscan

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// MaxArity bounds the argument counts compared.
const MaxArity = 8

// IsRegisterCall reports whether call is a Registry.Register(module, name, fn) call.
func IsRegisterCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Register"
}

// StringLiteral unquotes a string literal expression.
func StringLiteral(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// LeadingGuard reads `if <len(args) comparisons joined by ||> { return ..., err }` as body's first statement and returns the accepted argument counts.
func LeadingGuard(body *ast.BlockStmt) ([]int, bool) {
	if body == nil || len(body.List) == 0 {
		return nil, false
	}
	ifStmt, ok := body.List[0].(*ast.IfStmt)
	if !ok || ifStmt.Init != nil || ifStmt.Else != nil || !returnsError(ifStmt.Body) {
		return nil, false
	}
	var rejects []func(int) bool
	if !collectLenComparisons(ifStmt.Cond, &rejects) {
		return nil, false
	}
	var accepts []int
	for argc := 0; argc <= MaxArity; argc++ {
		rejected := false
		for _, r := range rejects {
			if r(argc) {
				rejected = true
				break
			}
		}
		if !rejected {
			accepts = append(accepts, argc)
		}
	}
	return accepts, true
}

func collectLenComparisons(e ast.Expr, out *[]func(int) bool) bool {
	switch v := e.(type) {
	case *ast.ParenExpr:
		return collectLenComparisons(v.X, out)
	case *ast.BinaryExpr:
		if v.Op == token.LOR {
			return collectLenComparisons(v.X, out) && collectLenComparisons(v.Y, out)
		}
		if !isLenArgs(v.X) {
			return false
		}
		lit, ok := v.Y.(*ast.BasicLit)
		if !ok || lit.Kind != token.INT {
			return false
		}
		n, err := strconv.Atoi(lit.Value)
		if err != nil {
			return false
		}
		switch v.Op {
		case token.NEQ:
			*out = append(*out, func(a int) bool { return a != n })
		case token.EQL:
			*out = append(*out, func(a int) bool { return a == n })
		case token.LSS:
			*out = append(*out, func(a int) bool { return a < n })
		case token.GTR:
			*out = append(*out, func(a int) bool { return a > n })
		case token.LEQ:
			*out = append(*out, func(a int) bool { return a <= n })
		case token.GEQ:
			*out = append(*out, func(a int) bool { return a >= n })
		default:
			return false
		}
		return true
	}
	return false
}

func isLenArgs(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	fn, ok := call.Fun.(*ast.Ident)
	arg, okA := call.Args[0].(*ast.Ident)
	return ok && okA && fn.Name == "len" && arg.Name == "args"
}

func returnsError(body *ast.BlockStmt) bool {
	for _, stmt := range body.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 2 {
			continue
		}
		if ident, ok := ret.Results[1].(*ast.Ident); ok && ident.Name == "nil" {
			continue
		}
		return true
	}
	return false
}

// SameInts reports whether two sorted count lists are equal.
func SameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Ints renders a count list.
func Ints(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Itoa(x)
	}
	return "[" + strings.Join(parts, " ") + "]"
}
