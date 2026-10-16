package semantic

import (
	"strings"

	"geblang/internal/ast"
)

// validateOverrides errors when an @override method overrides no ancestor or
// interface method. Unresolvable supertypes are skipped to avoid false positives.
func (a *Analyzer) validateOverrides(stmts []ast.Statement) {
	for _, stmt := range stmts {
		class := classStatement(stmt)
		if class == nil {
			continue
		}
		for _, member := range class.Members {
			fn, ok := member.(*ast.FunctionStatement)
			if !ok || fn.Name == nil || fn.Static {
				continue
			}
			if !hasDecorator(fn.Decorators, "override") {
				continue
			}
			if class.Extends == nil && len(class.Implements) == 0 {
				a.errorAt(fn.Name.Token, "method %s is marked @override but %s has no parent class or interface", fn.Name.Value, class.Name.Value)
				continue
			}
			resolved, found := a.overrideTarget(class, strings.ToLower(fn.Name.Value))
			if resolved && !found {
				a.errorAt(fn.Name.Token, "method %s is marked @override but no ancestor or interface of %s declares it", fn.Name.Value, class.Name.Value)
			}
		}
	}
}

// overrideTarget reports whether methodLower exists on an ancestor class or
// implemented interface; resolved is false when a supertype is not visible.
func (a *Analyzer) overrideTarget(class *ast.ClassStatement, methodLower string) (resolved bool, found bool) {
	if class.Extends != nil {
		r, f := a.ancestorHasMethod(class.Extends.Name, methodLower)
		if f {
			return true, true
		}
		if !r {
			return false, false
		}
	}
	for _, iface := range class.Implements {
		r, f := a.interfaceHasMethod(iface.Name, methodLower, map[string]bool{})
		if f {
			return true, true
		}
		if !r {
			return false, false
		}
	}
	return true, false
}

// interfaceHasMethod walks an interface and its parents; resolved is false if one isn't visible.
func (a *Analyzer) interfaceHasMethod(name, methodLower string, seen map[string]bool) (resolved bool, found bool) {
	if seen[name] {
		return true, false
	}
	seen[name] = true
	iface, ok := a.interfaces[name]
	if !ok {
		return false, false
	}
	if _, ok := iface.methods[methodLower]; ok {
		return true, true
	}
	if iface.defaults[methodLower] {
		return true, true
	}
	for _, parent := range iface.parents {
		r, f := a.interfaceHasMethod(parent, methodLower, seen)
		if f {
			return true, true
		}
		if !r {
			return false, false
		}
	}
	return true, false
}

// ancestorHasMethod walks the parent chain; resolved is false if a class isn't visible.
func (a *Analyzer) ancestorHasMethod(className, methodLower string) (resolved bool, found bool) {
	seen := map[string]bool{}
	for className != "" {
		if seen[className] {
			return true, false
		}
		seen[className] = true
		class, ok := a.classes[className]
		if !ok {
			return false, false
		}
		if _, ok := class.methods[methodLower]; ok {
			return true, true
		}
		className = class.parent
	}
	return true, false
}

// collectDeprecations records @deprecated functions, classes, and methods (with optional message).
func (a *Analyzer) collectDeprecations(stmts []ast.Statement) {
	for _, stmt := range stmts {
		switch s := unwrapExport(stmt).(type) {
		case *ast.FunctionStatement:
			if s.Name != nil && hasDecorator(s.Decorators, "deprecated") {
				a.deprecatedFuncs[strings.ToLower(s.Name.Value)] = decoratorMessage(s.Decorators, "deprecated")
			}
		case *ast.ClassStatement:
			if s.Name == nil {
				continue
			}
			if hasDecorator(s.Decorators, "deprecated") {
				a.deprecatedClasses[s.Name.Value] = decoratorMessage(s.Decorators, "deprecated")
			}
			for _, member := range s.Members {
				fn, ok := member.(*ast.FunctionStatement)
				if !ok || fn.Name == nil || !hasDecorator(fn.Decorators, "deprecated") {
					continue
				}
				methods := a.deprecatedMethods[s.Name.Value]
				if methods == nil {
					methods = map[string]string{}
					a.deprecatedMethods[s.Name.Value] = methods
				}
				methods[strings.ToLower(fn.Name.Value)] = decoratorMessage(fn.Decorators, "deprecated")
			}
		}
	}
}

// checkDeprecatedCall warns at calls to a @deprecated function, class, or method.
func (a *Analyzer) checkDeprecatedCall(call *ast.CallExpression) {
	switch callee := call.Callee.(type) {
	case *ast.Identifier:
		name := callee.Value
		if msg, ok := a.deprecatedFuncs[strings.ToLower(name)]; ok {
			a.ruleWarningAt(callee.Token, "deprecated", "%s", deprecationMessage(name, msg))
		} else if msg, ok := a.deprecatedClasses[name]; ok {
			a.ruleWarningAt(callee.Token, "deprecated", "%s", deprecationMessage(name, msg))
		}
	case *ast.SelectorExpression:
		if callee.Name == nil {
			return
		}
		receiver := a.expressionTypeName(callee.Object)
		if !receiver.known {
			return
		}
		if msg, ok := a.deprecatedMethodMessage(receiver.name, strings.ToLower(callee.Name.Value)); ok {
			a.ruleWarningAt(callee.Name.Token, "deprecated", "%s", deprecationMessage(callee.Name.Value, msg))
		}
	}
}

// deprecatedMethodMessage looks up a deprecated method on a class or its ancestors.
func (a *Analyzer) deprecatedMethodMessage(className, methodLower string) (string, bool) {
	seen := map[string]bool{}
	for className != "" && !seen[className] {
		seen[className] = true
		if methods, ok := a.deprecatedMethods[className]; ok {
			if msg, ok := methods[methodLower]; ok {
				return msg, true
			}
		}
		class, ok := a.classes[className]
		if !ok {
			break
		}
		className = class.parent
	}
	return "", false
}

func deprecationMessage(name, msg string) string {
	if msg == "" {
		return "use of deprecated " + name
	}
	return "use of deprecated " + name + ": " + msg
}

func classStatement(stmt ast.Statement) *ast.ClassStatement {
	if c, ok := unwrapExport(stmt).(*ast.ClassStatement); ok {
		return c
	}
	return nil
}

func unwrapExport(stmt ast.Statement) ast.Statement {
	if export, ok := stmt.(*ast.ExportStatement); ok {
		return export.Statement
	}
	return stmt
}

func hasDecorator(decorators []ast.Decorator, name string) bool {
	for _, d := range decorators {
		if d.Name != nil && d.Name.Value == name {
			return true
		}
	}
	return false
}

func decoratorMessage(decorators []ast.Decorator, name string) string {
	for _, d := range decorators {
		if d.Name == nil || d.Name.Value != name || len(d.Arguments) == 0 {
			continue
		}
		if lit, ok := d.Arguments[0].Value.(*ast.StringLiteral); ok {
			return lit.Value
		}
	}
	return ""
}
