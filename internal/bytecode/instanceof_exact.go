package bytecode

import (
	"geblang/internal/ast"
	"geblang/internal/runtime"
	"strings"
)

// Module-exact instanceof target is "\x00<module>\x00<name>"; empty module resolves to vm.moduleName.

func encodeExactInstanceofTarget(module, name string) string {
	return "\x00" + module + "\x00" + name
}

func parseExactInstanceofTarget(target string) (module, name string, ok bool) {
	if len(target) == 0 || target[0] != 0 {
		return "", "", false
	}
	rest := target[1:]
	sep := strings.IndexByte(rest, 0)
	if sep < 0 {
		return "", "", false
	}
	return rest[:sep], rest[sep+1:], true
}

// instanceofExactTarget resolves an instanceof RHS naming a user class/interface to a module-exact identity; other forms keep the legacy name-based path.
func (c *Compiler) instanceofExactTarget(rt *ast.TypeRef) (string, bool) {
	if rt == nil || rt.Operator != "" || len(rt.Arguments) > 0 {
		return "", false
	}
	name := rt.Name
	if runtime.IsBuiltinTypeName(name) {
		return "", false
	}
	if alias, member, ok := strings.Cut(name, "."); ok {
		if strings.Contains(member, ".") {
			return "", false
		}
		if canonical, isModule := c.moduleAliases[alias]; isModule {
			return encodeExactInstanceofTarget(canonical, member), true
		}
		return "", false
	}
	if _, ok := c.classes[strings.ToLower(name)]; ok {
		return encodeExactInstanceofTarget("", name), true
	}
	if _, ok := c.interfaces[strings.ToLower(name)]; ok {
		return encodeExactInstanceofTarget("", name), true
	}
	if qual, ok := c.fromImports[name]; ok {
		if module, member, ok := splitQualifiedClassName(qual); ok {
			return encodeExactInstanceofTarget(module, member), true
		}
	}
	return "", false
}
