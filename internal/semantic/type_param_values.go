package semantic

import (
	"strings"

	"geblang/internal/ast"
	"geblang/internal/token"
)

type typeParamValueState struct {
	fnStack  []*ast.FunctionStatement
	uses     map[*ast.FunctionStatement]map[string]bool
	topLevel map[string][]*ast.FunctionStatement
	calls    []untypedGenericCall
}

type untypedGenericCall struct {
	name string
	tok  token.Token
}

func (a *Analyzer) recordTopLevelFunction(stmt *ast.FunctionStatement) {
	if stmt == nil || stmt.Name == nil {
		return
	}
	if a.typeParamValues.topLevel == nil {
		a.typeParamValues.topLevel = map[string][]*ast.FunctionStatement{}
	}
	key := strings.ToLower(stmt.Name.Value)
	a.typeParamValues.topLevel[key] = append(a.typeParamValues.topLevel[key], stmt)
}

func (a *Analyzer) pushTypeParamFunction(stmt *ast.FunctionStatement) func() {
	a.typeParamValues.fnStack = append(a.typeParamValues.fnStack, stmt)
	return func() { a.typeParamValues.fnStack = a.typeParamValues.fnStack[:len(a.typeParamValues.fnStack)-1] }
}

// noteTypeParamValueUse records that the innermost function declaring name uses it as a value.
func (a *Analyzer) noteTypeParamValueUse(name string) {
	if a.typeParamScope[name] == 0 {
		return
	}
	if _, shadowed := a.lookup(name); shadowed {
		return
	}
	stack := a.typeParamValues.fnStack
	for i := len(stack) - 1; i >= 0; i-- {
		for _, generic := range stack[i].Generics {
			if generic.Name != nil && generic.Name.Value == name {
				if a.typeParamValues.uses == nil {
					a.typeParamValues.uses = map[*ast.FunctionStatement]map[string]bool{}
				}
				if a.typeParamValues.uses[stack[i]] == nil {
					a.typeParamValues.uses[stack[i]] = map[string]bool{}
				}
				a.typeParamValues.uses[stack[i]][name] = true
				return
			}
		}
	}
}

func (a *Analyzer) noteUntypedGenericCall(call *ast.CallExpression, ident *ast.Identifier) {
	if len(call.TypeArguments) > 0 || a.boundInInnerScope(ident.Value) {
		return
	}
	a.typeParamValues.calls = append(a.typeParamValues.calls, untypedGenericCall{name: strings.ToLower(ident.Value), tok: ident.Token})
}

// reportUninferableTypeParamCalls errors on calls that can never bind a type parameter the callee uses as a value.
func (a *Analyzer) reportUninferableTypeParamCalls() {
	for _, call := range a.typeParamValues.calls {
		decls := a.typeParamValues.topLevel[call.name]
		if len(decls) != 1 {
			continue
		}
		decl := decls[0]
		for _, generic := range decl.Generics {
			if generic.Name == nil || !a.typeParamValues.uses[decl][generic.Name.Value] {
				continue
			}
			if parametersMentionType(decl.Parameters, generic.Name.Value) {
				continue
			}
			a.errorAt(call.tok, "type parameter %s of %s cannot be inferred; pass a type argument", generic.Name.Value, decl.Name.Value)
		}
	}
}

func parametersMentionType(params []ast.Parameter, name string) bool {
	for _, param := range params {
		if typeRefMentions(param.Type, name) {
			return true
		}
	}
	return false
}

func typeRefMentions(ref *ast.TypeRef, name string) bool {
	if ref == nil {
		return false
	}
	if ref.Name == name || typeRefMentions(ref.Left, name) || typeRefMentions(ref.Right, name) {
		return true
	}
	for _, arg := range ref.Arguments {
		if typeRefMentions(arg, name) {
			return true
		}
	}
	return false
}

// boundInInnerScope reports a local binding that shadows a top-level declaration of name.
func (a *Analyzer) boundInInnerScope(name string) bool {
	for i := len(a.scopes) - 1; i >= 1; i-- {
		if _, ok := a.scopes[i][name]; ok {
			return true
		}
	}
	return false
}
