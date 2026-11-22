package runtime

import (
	"fmt"
	"strings"

	"geblang/internal/ast"
)

// BoundClass is a generic class reference carrying type arguments, produced when a type parameter bound to e.g. Box<int> is used as a value.
type BoundClass struct {
	Class    Value
	TypeArgs []string
}

func (v BoundClass) TypeName() string { return "class" }
func (v BoundClass) Inspect() string {
	return "<class " + ClassValueName(v.Class) + "<" + strings.Join(v.TypeArgs, ", ") + ">>"
}

func ClassValueName(v Value) string {
	switch c := v.(type) {
	case *Class:
		return c.Name
	case BytecodeClass:
		return c.Name
	}
	return v.Inspect()
}

// UnwrapClass returns the plain class behind a BoundClass, or the value unchanged.
func UnwrapClass(v Value) Value {
	if b, ok := v.(BoundClass); ok {
		return b.Class
	}
	return v
}

// UnwrapClassArgs replaces BoundClass arguments with their plain class, copying only when one is present.
func UnwrapClassArgs(args []Value) []Value {
	for i, arg := range args {
		if b, ok := arg.(BoundClass); ok {
			out := append([]Value(nil), args...)
			out[i] = b.Class
			for j := i + 1; j < len(out); j++ {
				out[j] = UnwrapClass(out[j])
			}
			return out
		}
	}
	return args
}

func boundClassesEqual(l, r BoundClass) bool {
	if len(l.TypeArgs) != len(r.TypeArgs) {
		return false
	}
	for i := range l.TypeArgs {
		if l.TypeArgs[i] != r.TypeArgs[i] {
			return false
		}
	}
	eq, _ := LeafValuesEqual(l.Class, r.Class)
	return eq
}

type TypeParamBindingKind int

const (
	TypeParamBindingUnsupported TypeParamBindingKind = iota
	TypeParamBindingBuiltin
	TypeParamBindingClass
)

// ClassifyTypeParamBinding decides what a type parameter's binding evaluates to; for a class it returns the class name and its type arguments.
func ClassifyTypeParamBinding(ref *ast.TypeRef) (TypeParamBindingKind, string, []string) {
	if ref == nil || ref.Operator != "" || ref.Nullable || ref.ListAlias {
		return TypeParamBindingUnsupported, "", nil
	}
	if IsBuiltinTypeName(ref.Name) {
		if len(ref.Arguments) == 0 {
			return TypeParamBindingBuiltin, strings.ToLower(ref.Name), nil
		}
		return TypeParamBindingUnsupported, "", nil
	}
	args := make([]string, len(ref.Arguments))
	for i, arg := range ref.Arguments {
		args[i] = arg.String()
	}
	return TypeParamBindingClass, ref.Name, args
}

func TypeParamUnboundError(param, owner string) error {
	if owner == "" {
		return ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("type parameter %s is not bound; pass a type argument", param)}
	}
	return ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("type parameter %s of %s is not bound; pass a type argument", param, owner)}
}

func TypeParamNotConstructibleError(param, bound string) error {
	return ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("type parameter %s is bound to %s, which cannot be constructed", param, bound)}
}

func TypeParamNotClassError(param, bound string) error {
	return ClassifiedError{Class: "TypeError", Message: fmt.Sprintf("type parameter %s is bound to %s, which is not a class", param, bound)}
}
