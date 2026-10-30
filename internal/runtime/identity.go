package runtime

import (
	"reflect"
	"sync"
	"unsafe"
)

// FunctionIdentity marks one created function value; copies share it, separate creations do not.
type FunctionIdentity struct{ _ byte }

func NewFunctionIdentity() *FunctionIdentity { return &FunctionIdentity{} }

var stableFunctionIdentities sync.Map

// StableFunctionIdentity returns the identity shared by every value built for the same declaration key.
func StableFunctionIdentity(key string) *FunctionIdentity {
	if existing, ok := stableFunctionIdentities.Load(key); ok {
		return existing.(*FunctionIdentity)
	}
	identity, _ := stableFunctionIdentities.LoadOrStore(key, NewFunctionIdentity())
	return identity.(*FunctionIdentity)
}

// ValuesIdentical implements `is`: reference kinds compare by object, value kinds by type and value.
func ValuesIdentical(left, right Value) bool {
	switch l := left.(type) {
	case *Instance:
		r, ok := right.(*Instance)
		return ok && l == r
	case *Class:
		r, ok := right.(*Class)
		return ok && l == r
	case *List:
		r, ok := right.(*List)
		return ok && l == r
	case Dict:
		r, ok := right.(Dict)
		return ok && sameDictStore(l, r)
	case Set:
		r, ok := right.(Set)
		return ok && sameMap(l.Elements, r.Elements)
	case *Task:
		r, ok := right.(*Task)
		return ok && l == r
	case *Generator:
		r, ok := right.(*Generator)
		return ok && l == r
	case *Module:
		r, ok := right.(*Module)
		return ok && (l == r || (l.Canonical != "" && l.Canonical == r.Canonical))
	case Function:
		r, ok := right.(Function)
		return ok && functionsIdentical(l, r)
	case BytecodeClosure:
		r, ok := right.(BytecodeClosure)
		return ok && l.Module == r.Module && l.FunctionIndex == r.FunctionIndex && l.Identity == r.Identity && sameUpvalues(l.Upvalues, r.Upvalues)
	case BytecodeFunction:
		r, ok := right.(BytecodeFunction)
		return ok && l.Module == r.Module && l.Index == r.Index
	case OverloadedFunction:
		r, ok := right.(OverloadedFunction)
		return ok && overloadsIdentical(l, r)
	case DecoratorTarget:
		r, ok := right.(DecoratorTarget)
		return ok && l.Target == r.Target && l.Callable != nil && r.Callable != nil && ValuesIdentical(l.Callable, r.Callable)
	case NativeObject:
		r, ok := right.(NativeObject)
		return ok && nativeObjectsEqual(l, r)
	case EnumVariant:
		r, ok := right.(EnumVariant)
		return ok && enumVariantsIdentical(l, r)
	case Error:
		r, ok := right.(Error)
		return ok && l.Class == r.Class && l.Message == r.Message && sameMap(l.Fields, r.Fields)
	}
	if equal, handled := LeafValuesEqual(left, right); handled {
		return equal && left.TypeName() == right.TypeName()
	}
	return false
}

func functionsIdentical(l, r Function) bool {
	switch {
	case l.Identity != nil || r.Identity != nil:
		return l.Identity == r.Identity
	case l.Bound != nil || r.Bound != nil:
		return l.Bound == r.Bound
	case l.Body != nil || r.Body != nil:
		return l.Body == r.Body
	case l.Native != nil && r.Native != nil:
		return l.Name == r.Name && reflect.ValueOf(l.Native).Pointer() == reflect.ValueOf(r.Native).Pointer()
	}
	return false
}

func overloadsIdentical(l, r OverloadedFunction) bool {
	if len(l.Callables) != len(r.Callables) {
		return false
	}
	for i := range l.Callables {
		if !ValuesIdentical(l.Callables[i], r.Callables[i]) {
			return false
		}
	}
	if l.Name != r.Name || l.Module != r.Module || len(l.Indices) != len(r.Indices) || len(l.Overloads) != len(r.Overloads) {
		return false
	}
	for i := range l.Indices {
		if l.Indices[i] != r.Indices[i] {
			return false
		}
	}
	if len(l.Callables) > 0 {
		return true
	}
	for i := range l.Overloads {
		if !functionsIdentical(l.Overloads[i], r.Overloads[i]) {
			return false
		}
	}
	return true
}

func enumVariantsIdentical(l, r EnumVariant) bool {
	if l.Variant != r.Variant || len(l.Fields) != len(r.Fields) || l.Enum == nil || r.Enum == nil {
		return false
	}
	if l.Enum != r.Enum && (l.Enum.Name != r.Enum.Name || l.Enum.Module != r.Enum.Module) {
		return false
	}
	for i := range l.Fields {
		if !ValuesIdentical(l.Fields[i], r.Fields[i]) {
			return false
		}
	}
	return true
}

// ContainerIdentity returns the object a list, dict or instance refers to, the identity ValuesIdentical compares.
func ContainerIdentity(value Value) (unsafe.Pointer, bool) {
	switch v := value.(type) {
	case *List:
		return unsafe.Pointer(v), true
	case *Instance:
		return unsafe.Pointer(v), true
	case Dict:
		if v.data != nil {
			return unsafe.Pointer(v.data), true
		}
		if v.Order != nil {
			return unsafe.Pointer(v.Order), true
		}
		return reflect.ValueOf(v.Entries).UnsafePointer(), true
	}
	return nil, false
}

func sameDictStore(l, r Dict) bool {
	if l.data != nil || r.data != nil {
		return l.data == r.data
	}
	if l.Order != nil || r.Order != nil {
		return l.Order == r.Order
	}
	return sameMap(l.Entries, r.Entries)
}

func sameMap[K comparable, V any](l, r map[K]V) bool {
	return reflect.ValueOf(l).UnsafePointer() == reflect.ValueOf(r).UnsafePointer()
}

func sameUpvalues(l, r []Value) bool {
	if len(l) != len(r) {
		return false
	}
	return len(l) == 0 || &l[0] == &r[0]
}
