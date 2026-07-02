package runtime

import (
	"bytes"
	"fmt"
	"math"
	"strings"
)

// RunTestAssertion implements the native assertion methods exposed by test.Test.
// It returns handled=false when name is not a built-in assertion.
func RunTestAssertion(name string, args []Value) (Value, bool, error) {
	switch strings.ToLower(name) {
	case "equal":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("Test.equal expects two arguments")
		}
		return assertEqual(args[1], args[0], "expected %s, got %s")
	case "assertequal", "assertequals":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("Test.%s expects two arguments", name)
		}
		return assertEqual(args[0], args[1], "expected %s, got %s")
	case "assertnotequal", "assertnotequals":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("Test.%s expects two arguments", name)
		}
		if ValuesEqual(args[0], args[1]) {
			return nil, true, fmt.Errorf("did not expect %s", args[1].Inspect())
		}
		return Null{}, true, nil
	case "istrue", "asserttrue":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("Test.%s expects one argument", name)
		}
		value, ok := args[0].(Bool)
		if !ok || !value.Value {
			return nil, true, fmt.Errorf("expected true")
		}
		return Null{}, true, nil
	case "isfalse", "assertfalse":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("Test.%s expects one argument", name)
		}
		value, ok := args[0].(Bool)
		if !ok || value.Value {
			return nil, true, fmt.Errorf("expected false")
		}
		return Null{}, true, nil
	case "assertnull":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("Test.assertNull expects one argument")
		}
		if _, ok := args[0].(Null); !ok {
			return nil, true, fmt.Errorf("expected null, got %s", args[0].Inspect())
		}
		return Null{}, true, nil
	case "notnull", "assertnotnull":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("Test.%s expects one argument", name)
		}
		if _, ok := args[0].(Null); ok {
			return nil, true, fmt.Errorf("expected non-null")
		}
		return Null{}, true, nil
	case "assertcontains":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("Test.assertContains expects two arguments")
		}
		if !containsValue(args[0], args[1]) {
			return nil, true, fmt.Errorf("expected %s to contain %s", args[0].Inspect(), args[1].Inspect())
		}
		return Null{}, true, nil
	case "assertnotcontains":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("Test.assertNotContains expects two arguments")
		}
		if containsValue(args[0], args[1]) {
			return nil, true, fmt.Errorf("did not expect %s to contain %s", args[0].Inspect(), args[1].Inspect())
		}
		return Null{}, true, nil
	case "assertempty":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("Test.assertEmpty expects one argument")
		}
		if !isEmptyValue(args[0]) {
			return nil, true, fmt.Errorf("expected empty value, got %s", args[0].Inspect())
		}
		return Null{}, true, nil
	case "assertnotempty":
		if len(args) != 1 {
			return nil, true, fmt.Errorf("Test.assertNotEmpty expects one argument")
		}
		if isEmptyValue(args[0]) {
			return nil, true, fmt.Errorf("expected non-empty value")
		}
		return Null{}, true, nil
	case "assertgreaterthan", "assertgreaterthanorequal", "assertlessthan", "assertlessthanorequal":
		if len(args) != 2 {
			return nil, true, fmt.Errorf("Test.%s expects two arguments", name)
		}
		cmp, ok := compareOrdered(args[1], args[0])
		if !ok {
			return nil, true, fmt.Errorf("Test.%s expects ordered numeric or string values", name)
		}
		if !comparisonPasses(strings.ToLower(name), cmp) {
			return nil, true, fmt.Errorf("expected %s to satisfy %s %s", args[1].Inspect(), comparisonSymbol(strings.ToLower(name)), args[0].Inspect())
		}
		return Null{}, true, nil
	case "fail":
		if len(args) > 1 {
			return nil, true, fmt.Errorf("Test.fail expects zero or one argument")
		}
		if len(args) == 1 {
			return nil, true, fmt.Errorf("%s", args[0].Inspect())
		}
		return nil, true, fmt.Errorf("failed")
	case "skip":
		if len(args) > 1 {
			return nil, true, fmt.Errorf("Test.skip expects zero or one argument")
		}
		reason := ""
		if len(args) == 1 {
			if s, ok := args[0].(String); ok {
				reason = s.Value
			} else {
				reason = args[0].Inspect()
			}
		}
		return nil, true, &TestSkip{Reason: reason}
	default:
		return nil, false, nil
	}
}

// TestSkip is the signal raised by this.skip(reason) inside a @test method.
// It surfaces as the "TestSkip" error class so the runners recognise it across
// the native-to-script boundary (via TypedError) and record a skip, not a fail.
type TestSkip struct {
	Reason string
}

func (e *TestSkip) Error() string {
	if e.Reason == "" {
		return "test skipped"
	}
	return e.Reason
}

func (e *TestSkip) ErrorClass() string { return "TestSkip" }

func assertEqual(expected Value, actual Value, format string) (Value, bool, error) {
	if !ValuesEqual(actual, expected) {
		return nil, true, fmt.Errorf(format, expected.Inspect(), actual.Inspect())
	}
	return Null{}, true, nil
}

// ValuesEqual compares runtime values using the same deep equality rules used
// by the evaluator and VM for plain data.
func ValuesEqual(left Value, right Value) bool {
	switch leftValue := left.(type) {
	case *List:
		rightValue, ok := right.(*List)
		if !ok || len(leftValue.Elements) != len(rightValue.Elements) {
			return false
		}
		for i, element := range leftValue.Elements {
			if !ValuesEqual(element, rightValue.Elements[i]) {
				return false
			}
		}
		return true
	case Dict:
		rightValue, ok := right.(Dict)
		if !ok || leftValue.Len() != rightValue.Len() {
			return false
		}
		for _, key := range leftValue.EntryKeys() {
			entry, _ := leftValue.GetEntry(key)
			other, ok := rightValue.GetEntry(key)
			if !ok || !ValuesEqual(entry.Key, other.Key) || !ValuesEqual(entry.Value, other.Value) {
				return false
			}
		}
		return true
	case Set:
		rightValue, ok := right.(Set)
		if !ok || len(leftValue.Elements) != len(rightValue.Elements) {
			return false
		}
		for key, entry := range leftValue.Elements {
			other, ok := rightValue.Elements[key]
			if !ok || !ValuesEqual(entry.Value, other.Value) {
				return false
			}
		}
		return true
	case EnumVariant:
		rightValue, ok := right.(EnumVariant)
		if !ok || leftValue.Enum != rightValue.Enum || leftValue.Variant != rightValue.Variant || len(leftValue.Fields) != len(rightValue.Fields) {
			return false
		}
		for i, field := range leftValue.Fields {
			if !ValuesEqual(field, rightValue.Fields[i]) {
				return false
			}
		}
		return true
	case *Instance:
		rightValue, ok := right.(*Instance)
		if !ok || !strings.EqualFold(leftValue.Class.Name, rightValue.Class.Name) || len(leftValue.Fields) != len(rightValue.Fields) {
			return false
		}
		for name, value := range leftValue.Fields {
			other, ok := rightValue.Fields[name]
			if !ok || !ValuesEqual(value, other) {
				return false
			}
		}
		return true
	default:
		return primitiveValuesEqual(left, right)
	}
}

func primitiveValuesEqual(left Value, right Value) bool {
	// Numbers compare by exact value across int/decimal/float.
	if eq, both := NumericValuesEqual(left, right); both {
		return eq
	}
	switch leftValue := left.(type) {
	case Null:
		_, ok := right.(Null)
		return ok
	case Bool:
		rightValue, ok := right.(Bool)
		return ok && leftValue.Value == rightValue.Value
	case SmallInt:
		switch rightValue := right.(type) {
		case SmallInt:
			return leftValue.Value == rightValue.Value
		case Int:
			return rightValue.Value.IsInt64() && leftValue.Value == rightValue.Value.Int64()
		}
		return false
	case Int:
		switch rightValue := right.(type) {
		case Int:
			return leftValue.Value.Cmp(rightValue.Value) == 0
		case SmallInt:
			return leftValue.Value.IsInt64() && leftValue.Value.Int64() == rightValue.Value
		}
		return false
	case Decimal:
		rightValue, ok := right.(Decimal)
		return ok && leftValue.Value.Cmp(rightValue.Value) == 0
	case Float:
		rightValue, ok := right.(Float)
		return ok && leftValue.Value == rightValue.Value
	case String:
		if rightValue, ok := right.(String); ok {
			return leftValue.Value == rightValue.Value
		}
		// typeof(x) yields a Type; assertEquals against its name string mirrors the == operator.
		if rightType, ok := right.(Type); ok {
			return leftValue.Value == rightType.Name
		}
		return false
	case Bytes:
		rightValue, ok := right.(Bytes)
		return ok && bytes.Equal(leftValue.Value, rightValue.Value)
	case DateTimeInstant:
		rightValue, ok := right.(DateTimeInstant)
		return ok && leftValue == rightValue
	case DateTimeDuration:
		rightValue, ok := right.(DateTimeDuration)
		return ok && leftValue == rightValue
	case DateTimeZone:
		rightValue, ok := right.(DateTimeZone)
		return ok && leftValue == rightValue
	case URLValue:
		rightValue, ok := right.(URLValue)
		return ok && leftValue == rightValue
	case HTTPHeaders:
		rightValue, ok := right.(HTTPHeaders)
		if !ok || len(leftValue.Values) != len(rightValue.Values) {
			return false
		}
		for key, values := range leftValue.Values {
			other := rightValue.Values[key]
			if len(values) != len(other) {
				return false
			}
			for i, value := range values {
				if value != other[i] {
					return false
				}
			}
		}
		return true
	case HTTPCookie:
		rightValue, ok := right.(HTTPCookie)
		return ok && leftValue == rightValue
	case TemplateValue:
		rightValue, ok := right.(TemplateValue)
		return ok && leftValue == rightValue
	case TemplateEngine:
		rightValue, ok := right.(TemplateEngine)
		return ok && leftValue == rightValue
	case Range:
		rightValue, ok := right.(Range)
		return ok &&
			leftValue.Exclusive == rightValue.Exclusive &&
			leftValue.Start.Cmp(rightValue.Start) == 0 &&
			leftValue.End.Cmp(rightValue.End) == 0 &&
			leftValue.Step.Cmp(rightValue.Step) == 0
	case BytecodeFunction:
		rightValue, ok := right.(BytecodeFunction)
		return ok && leftValue.Module == rightValue.Module && leftValue.Name == rightValue.Name && leftValue.Index == rightValue.Index
	case BytecodeClass:
		if rightValue, ok := right.(BytecodeClass); ok {
			return leftValue.Module == rightValue.Module && leftValue.Name == rightValue.Name && leftValue.Index == rightValue.Index
		}
		// A class value compares equal to typeof(instance) of the same name, mirroring ==.
		if rightType, ok := right.(Type); ok {
			return leftValue.Name == rightType.Name
		}
		return false
	case NativeObject:
		rightValue, ok := right.(NativeObject)
		return ok && leftValue == rightValue
	case Error:
		rightValue, ok := right.(Error)
		return ok && leftValue.Class == rightValue.Class && leftValue.Message == rightValue.Message
	case Type:
		if rightValue, ok := right.(Type); ok {
			return leftValue == rightValue
		}
		if rightStr, ok := right.(String); ok {
			return leftValue.Name == rightStr.Value
		}
		// typeof(x) compares equal to a class value of the same name, mirroring ==.
		if rightClass, ok := right.(*Class); ok {
			return leftValue.Name == rightClass.Name
		}
		if rightClass, ok := right.(BytecodeClass); ok {
			return leftValue.Name == rightClass.Name
		}
		return false
	case *Module:
		rightValue, ok := right.(*Module)
		return ok && leftValue == rightValue
	case *Class:
		if rightValue, ok := right.(*Class); ok {
			return leftValue == rightValue
		}
		if rightType, ok := right.(Type); ok {
			return leftValue.Name == rightType.Name
		}
		return false
	case *Interface:
		rightValue, ok := right.(*Interface)
		return ok && leftValue == rightValue
	default:
		return false
	}
}

func containsValue(haystack Value, needle Value) bool {
	switch value := haystack.(type) {
	case String:
		n, ok := needle.(String)
		return ok && strings.Contains(value.Value, n.Value)
	case Bytes:
		if n, ok := needle.(Bytes); ok {
			return bytes.Contains(value.Value, n.Value)
		}
		switch n := needle.(type) {
		case SmallInt:
			b := n.Value
			return b >= 0 && b <= 255 && bytes.Contains(value.Value, []byte{byte(b)})
		case Int:
			if !n.Value.IsInt64() {
				return false
			}
			b := n.Value.Int64()
			return b >= 0 && b <= 255 && bytes.Contains(value.Value, []byte{byte(b)})
		}
		return false
	case *List:
		for _, element := range value.Elements {
			if ValuesEqual(element, needle) {
				return true
			}
		}
		return false
	case Dict:
		for _, key := range value.EntryKeys() {
			entry, _ := value.GetEntry(key)
			if ValuesEqual(entry.Key, needle) {
				return true
			}
		}
		return false
	case Set:
		for _, entry := range value.Elements {
			if ValuesEqual(entry.Value, needle) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func isEmptyValue(value Value) bool {
	switch v := value.(type) {
	case Null:
		return true
	case String:
		return v.Value == ""
	case Bytes:
		return len(v.Value) == 0
	case *List:
		return len(v.Elements) == 0
	case Dict:
		return len(v.Entries) == 0
	case Set:
		return len(v.Elements) == 0
	case Range:
		return v.Length().Sign() == 0
	default:
		return false
	}
}

func compareOrdered(actual Value, expected Value) (int, bool) {
	if left, ok := numericFloat(actual); ok {
		right, ok := numericFloat(expected)
		if !ok {
			return 0, false
		}
		if left < right {
			return -1, true
		}
		if left > right {
			return 1, true
		}
		return 0, true
	}
	left, ok := actual.(String)
	if !ok {
		return 0, false
	}
	right, ok := expected.(String)
	if !ok {
		return 0, false
	}
	return strings.Compare(left.Value, right.Value), true
}

func numericFloat(value Value) (float64, bool) {
	switch v := value.(type) {
	case SmallInt:
		return float64(v.Value), true
	case Int:
		f, _ := v.Value.Float64()
		return f, true
	case Decimal:
		f, _ := v.Value.Float64()
		return f, true
	case Float:
		if math.IsNaN(v.Value) {
			return 0, false
		}
		return v.Value, true
	default:
		return 0, false
	}
}

func comparisonPasses(name string, cmp int) bool {
	switch name {
	case "assertgreaterthan":
		return cmp > 0
	case "assertgreaterthanorequal":
		return cmp >= 0
	case "assertlessthan":
		return cmp < 0
	case "assertlessthanorequal":
		return cmp <= 0
	default:
		return false
	}
}

func comparisonSymbol(name string) string {
	switch name {
	case "assertgreaterthan":
		return ">"
	case "assertgreaterthanorequal":
		return ">="
	case "assertlessthan":
		return "<"
	case "assertlessthanorequal":
		return "<="
	default:
		return "?"
	}
}
