package evaluator

import (
	"os"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/runtime"
	"geblang/internal/typealias"
)

func isTypeValue(value runtime.Value) bool {
	switch value.(type) {
	case *runtime.Class, *runtime.Interface, *runtime.EnumDef:
		return true
	}
	return false
}

func cutLastDot(name string) (string, string, bool) {
	dot := strings.LastIndexByte(name, '.')
	if dot <= 0 || dot == len(name)-1 {
		return "", "", false
	}
	return name[:dot], name[dot+1:], true
}

// Child evaluators start with an empty module table, so lookups fall back to the parent chain.
func (e *Evaluator) moduleForPrefix(prefix string) *runtime.Module {
	canonical := prefix
	if target, ok := e.importNames[prefix]; ok {
		canonical = target
	}
	for ev := e; ev != nil; ev = ev.parent {
		if module, ok := ev.modules[canonical]; ok {
			return module
		}
	}
	return nil
}

func (e *Evaluator) exportedTypeAliasTarget(name string) (string, bool) {
	prefix, member, ok := cutLastDot(name)
	if !ok {
		return "", false
	}
	module := e.moduleForPrefix(prefix)
	if module == nil {
		return "", false
	}
	target, ok := module.TypeAliases[member]
	return target, ok
}

func (e *Evaluator) exportedTypeAlias(name string) (*ast.TypeRef, bool) {
	target, ok := e.exportedTypeAliasTarget(name)
	if !ok {
		return nil, false
	}
	ref, err := parser.ParseTypeRef(target)
	if err != nil {
		return nil, false
	}
	return ref, true
}

func (e *Evaluator) resolveAliasedTypeValue(ref *ast.TypeRef, env *runtime.Environment) (runtime.Value, bool) {
	target := e.resolveTypeRef(ref, env)
	if target == nil || target.Operator != "" {
		return nil, false
	}
	prefix, member, qualified := cutLastDot(target.Name)
	if !qualified {
		return nil, false
	}
	module := e.moduleForPrefix(prefix)
	if module == nil {
		return nil, false
	}
	value, exists := module.Exports[member]
	if !exists || !isTypeValue(value) {
		return nil, false
	}
	return value, true
}

func (e *Evaluator) sourceTypeAliasTargets() *typealias.SourceLookup {
	return typealias.NewSourceLookup(func(canonical string) (*ast.Program, bool) {
		path, err := e.resolveModulePath(canonical)
		if err != nil {
			return nil, false
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return nil, false
		}
		p := parser.New(lexer.New(string(source)))
		program := p.ParseProgram()
		return program, len(p.Errors()) == 0
	})
}

func (e *Evaluator) typeAliasLookup(env *runtime.Environment) typealias.Lookup {
	return func(name string) (string, bool) {
		if target, ok := env.GetTypeAlias(name); ok {
			return target.String(), true
		}
		return e.exportedTypeAliasTarget(name)
	}
}
