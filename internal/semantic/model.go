package semantic

import (
	"strings"

	"geblang/internal/ast"
)

// ClassDecl is the resolved shape of a class used by cross-module
// method-existence checks. Method/field names are stored lowercased
// (dispatch is case-insensitive at storage level).
type ClassDecl struct {
	Name       string
	Parent     string   // declared parent class name, or ""
	Implements []string // implemented interface names
	Methods    map[string]bool
	Fields     map[string]bool
	// HasCall marks a `__call` method (dynamic dispatch): callers must
	// not flag unknown methods on such a class.
	HasCall bool
	// Decorated marks class-level decorators, which may inject members:
	// callers must bail rather than risk a false positive.
	Decorated bool
	// MethodSigs holds the overload signatures per (lowercased) method name,
	// for static argument validation. A name in Methods may be absent here.
	MethodSigs map[string][]MethodSignature
	// TypeParams are the class's declared generic parameter names.
	TypeParams []string
	// FieldTypes maps lowercased field name -> declared type, nil where untyped.
	FieldTypes map[string]*ast.TypeRef
}

// methodSignatureFromFunc captures a method's parameter types + arity for
// static call validation. Geblang methods take `this` implicitly, so the
// AST parameters are exactly the caller-supplied arguments.
func methodSignatureFromFunc(m *ast.FunctionStatement) MethodSignature {
	sig := MethodSignature{}
	for _, g := range m.Generics {
		if g != nil && g.Name != nil {
			sig.TypeParams = append(sig.TypeParams, g.Name.Value)
		}
	}
	for _, p := range m.Parameters {
		sig.Params = append(sig.Params, p.Type)
		if p.Variadic {
			sig.Variadic = true
		}
		if p.Default == nil && !p.Variadic {
			sig.Required++
		}
	}
	return sig
}

// MethodSignature is one overload of a class method: the declared
// parameter types plus arity, so callers can validate call arguments
// statically. A nil Params entry marks an untyped parameter.
type MethodSignature struct {
	Params     []*ast.TypeRef
	Required   int // positional args that must be supplied (no default, not variadic)
	Variadic   bool
	TypeParams []string // the method's own generic parameter names
}

// InterfaceDecl is the resolved shape of an interface. Methods include
// both abstract signatures and default-implementation names; an
// instance responds to all of them.
type InterfaceDecl struct {
	Name    string
	Parents []string
	Methods map[string]bool
	Fields  map[string]bool
}

// ModuleModel is the class/interface surface of one parsed module.
type ModuleModel struct {
	Classes    map[string]ClassDecl
	Interfaces map[string]InterfaceDecl
}

// ModuleCallSignatures returns the signatures a call to `name` in another module must match: the overloads of an undecorated top-level function, or an undecorated class's constructors (one empty signature when a class with no parent declares none). ok is false when the name is not such a declaration, so callers stay silent.
func ModuleCallSignatures(program *ast.Program, name string) ([]MethodSignature, bool) {
	if program == nil {
		return nil, false
	}
	var sigs []MethodSignature
	for _, stmt := range program.Statements {
		if exp, ok := stmt.(*ast.ExportStatement); ok {
			stmt = exp.Statement
		}
		switch s := stmt.(type) {
		case *ast.FunctionStatement:
			if s.Name == nil || s.Name.Value != name {
				continue
			}
			if len(s.Decorators) > 0 {
				return nil, false
			}
			sigs = append(sigs, methodSignatureFromFunc(s))
		case *ast.ClassStatement:
			if s.Name == nil || s.Name.Value != name {
				continue
			}
			if len(s.Decorators) > 0 {
				return nil, false
			}
			var classGenerics []string
			for _, g := range s.Generics {
				if g != nil && g.Name != nil {
					classGenerics = append(classGenerics, g.Name.Value)
				}
			}
			var ctors []MethodSignature
			for _, member := range s.Members {
				if m, ok := member.(*ast.FunctionStatement); ok && m.Name != nil && m.Name.Value == s.Name.Value {
					if len(m.Decorators) > 0 {
						return nil, false
					}
					sig := methodSignatureFromFunc(m)
					sig.TypeParams = append(sig.TypeParams, classGenerics...)
					ctors = append(ctors, sig)
				}
			}
			if len(ctors) == 0 {
				if s.Extends != nil {
					// The constructor may be inherited from a parent elsewhere.
					return nil, false
				}
				ctors = []MethodSignature{{}}
			}
			return ctors, true
		}
	}
	return sigs, len(sigs) > 0
}

// ExtractModel walks a parsed program and returns its class/interface
// declarations. Pure (no IO); the check package calls it per module to
// build a cross-module class graph. Keys are the declared names (as
// written); member sets are lowercased.
func ExtractModel(program *ast.Program) ModuleModel {
	model := ModuleModel{
		Classes:    map[string]ClassDecl{},
		Interfaces: map[string]InterfaceDecl{},
	}
	if program == nil {
		return model
	}
	var walk func(stmt ast.Statement)
	walk = func(stmt ast.Statement) {
		switch s := stmt.(type) {
		case *ast.ExportStatement:
			walk(s.Statement)
		case *ast.ClassStatement:
			if s.Name == nil {
				return
			}
			decl := ClassDecl{
				Name:       s.Name.Value,
				Methods:    map[string]bool{},
				Fields:     map[string]bool{},
				FieldTypes: map[string]*ast.TypeRef{},
				Decorated:  len(s.Decorators) > 0,
				MethodSigs: map[string][]MethodSignature{},
			}
			for _, g := range s.Generics {
				if g != nil && g.Name != nil {
					decl.TypeParams = append(decl.TypeParams, g.Name.Value)
				}
			}
			if s.Extends != nil {
				decl.Parent = s.Extends.Name
			}
			for _, iface := range s.Implements {
				decl.Implements = append(decl.Implements, iface.Name)
			}
			for _, member := range s.Members {
				switch m := member.(type) {
				case *ast.FunctionStatement:
					if m.Name == nil {
						continue
					}
					name := strings.ToLower(m.Name.Value)
					decl.Methods[name] = true
					decl.MethodSigs[name] = append(decl.MethodSigs[name], methodSignatureFromFunc(m))
					if name == "__call" {
						decl.HasCall = true
					}
				case *ast.DeclarationStatement:
					if m.Name != nil {
						lower := strings.ToLower(m.Name.Value)
						decl.Fields[lower] = true
						decl.FieldTypes[lower] = m.Type
					}
				}
			}
			model.Classes[s.Name.Value] = decl
		case *ast.InterfaceStatement:
			if s.Name == nil {
				return
			}
			decl := InterfaceDecl{Name: s.Name.Value, Methods: map[string]bool{}, Fields: map[string]bool{}}
			for _, parent := range s.Parents {
				decl.Parents = append(decl.Parents, parent.Name)
			}
			for _, method := range s.Methods {
				if method.Name != nil {
					decl.Methods[strings.ToLower(method.Name.Value)] = true
				}
			}
			for _, def := range s.Defaults {
				if def.Name != nil {
					decl.Methods[strings.ToLower(def.Name.Value)] = true
				}
			}
			for _, field := range s.Fields {
				if field.Name != nil {
					decl.Fields[strings.ToLower(field.Name.Value)] = true
				}
			}
			model.Interfaces[s.Name.Value] = decl
		}
	}
	for _, stmt := range program.Statements {
		walk(stmt)
	}
	return model
}
