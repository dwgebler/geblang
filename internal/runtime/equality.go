package runtime

import (
	"bytes"
	"reflect"
)

// LeafValuesEqual is the one scalar/Type/Class equality both backends' == and assertEquals share; handled=false delegates containers/instances/enums (and numeric cross-type) back to the caller.
func LeafValuesEqual(left Value, right Value) (equal bool, handled bool) {
	switch leftValue := left.(type) {
	case Null:
		_, ok := right.(Null)
		return ok, true
	case Bool:
		rightValue, ok := right.(Bool)
		return ok && leftValue.Value == rightValue.Value, true
	case SmallInt:
		switch rv := right.(type) {
		case SmallInt:
			return leftValue.Value == rv.Value, true
		case Int:
			return rv.Value.IsInt64() && rv.Value.Int64() == leftValue.Value, true
		}
		return false, true
	case Int:
		switch rv := right.(type) {
		case SmallInt:
			return leftValue.Value.IsInt64() && leftValue.Value.Int64() == rv.Value, true
		case Int:
			return leftValue.Value.Cmp(rv.Value) == 0, true
		}
		return false, true
	case Decimal:
		rightValue, ok := right.(Decimal)
		return ok && leftValue.Value.Cmp(rightValue.Value) == 0, true
	case Float:
		rightValue, ok := right.(Float)
		return ok && leftValue.Value == rightValue.Value, true
	case String:
		if rightValue, ok := right.(String); ok {
			return leftValue.Value == rightValue.Value, true
		}
		// Symmetry with `typeof(x) == "name"`: a Type equals the string of its name.
		if rightType, ok := right.(Type); ok {
			return leftValue.Value == rightType.Name, true
		}
		return false, true
	case Bytes:
		rightValue, ok := right.(Bytes)
		return ok && bytes.Equal(leftValue.Value, rightValue.Value), true
	case DateTimeInstant:
		rightValue, ok := right.(DateTimeInstant)
		return ok && leftValue == rightValue, true
	case DateTimeDuration:
		rightValue, ok := right.(DateTimeDuration)
		return ok && leftValue == rightValue, true
	case DateTimeZone:
		rightValue, ok := right.(DateTimeZone)
		return ok && leftValue == rightValue, true
	case URLValue:
		rightValue, ok := right.(URLValue)
		return ok && leftValue == rightValue, true
	case HTTPHeaders:
		rightValue, ok := right.(HTTPHeaders)
		if !ok || len(leftValue.Values) != len(rightValue.Values) {
			return false, true
		}
		for key, values := range leftValue.Values {
			other := rightValue.Values[key]
			if len(values) != len(other) {
				return false, true
			}
			for i, value := range values {
				if value != other[i] {
					return false, true
				}
			}
		}
		return true, true
	case HTTPCookie:
		rightValue, ok := right.(HTTPCookie)
		return ok && leftValue == rightValue, true
	case TemplateValue:
		rightValue, ok := right.(TemplateValue)
		return ok && leftValue == rightValue, true
	case TemplateEngine:
		rightValue, ok := right.(TemplateEngine)
		return ok && leftValue == rightValue, true
	case Range:
		rightValue, ok := right.(Range)
		return ok &&
			leftValue.Exclusive == rightValue.Exclusive &&
			leftValue.Start.Cmp(rightValue.Start) == 0 &&
			leftValue.End.Cmp(rightValue.End) == 0 &&
			leftValue.Step.Cmp(rightValue.Step) == 0, true
	case BytecodeFunction:
		rightValue, ok := right.(BytecodeFunction)
		return ok && leftValue.Module == rightValue.Module && leftValue.Name == rightValue.Name && leftValue.Index == rightValue.Index, true
	case BytecodeClass:
		switch rv := right.(type) {
		case BytecodeClass:
			return leftValue.Module == rv.Module && leftValue.Name == rv.Name && leftValue.Index == rv.Index, true
		case Type:
			return leftValue.Name == rv.Name, true
		}
		return false, true
	case NativeObject:
		rightValue, ok := right.(NativeObject)
		return ok && nativeObjectsEqual(leftValue, rightValue), true
	case Error:
		rightValue, ok := right.(Error)
		return ok && leftValue.Class == rightValue.Class && leftValue.Message == rightValue.Message, true
	case Type:
		switch rv := right.(type) {
		case Type:
			return leftValue.Name == rv.Name, true
		case BytecodeClass:
			return leftValue.Name == rv.Name, true
		case *Class:
			return leftValue.Name == rv.Name, true
		case String:
			return leftValue.Name == rv.Value, true
		}
		return false, true
	case *Module:
		rightValue, ok := right.(*Module)
		return ok && leftValue == rightValue, true
	case *Class:
		switch rv := right.(type) {
		case *Class:
			return leftValue == rv, true
		case Type:
			return leftValue.Name == rv.Name, true
		}
		return false, true
	case *Interface:
		rightValue, ok := right.(*Interface)
		return ok && leftValue == rightValue, true
	case *Complex:
		rightValue, ok := right.(*Complex)
		return ok && leftValue.C == rightValue.C, true
	}
	return false, false
}

// nativeObjectsEqual compares native handles without panicking on an uncomparable payload (e.g. the serveFile marker's Dict holds a map).
func nativeObjectsEqual(left NativeObject, right NativeObject) bool {
	if left.Kind != right.Kind || left.ID != right.ID {
		return false
	}
	if left.Payload == nil || right.Payload == nil {
		return left.Payload == nil && right.Payload == nil
	}
	if reflect.TypeOf(left.Payload).Comparable() && reflect.TypeOf(right.Payload).Comparable() {
		return left.Payload == right.Payload
	}
	return reflect.DeepEqual(left.Payload, right.Payload)
}
