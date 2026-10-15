package evaluator

import (
	"strings"

	"geblang/internal/ast"
	"geblang/internal/runtime"
)

// intLiteralMask marks positional integer-literal arguments; nil when there are none or args are named/spread.
func intLiteralMask(call *ast.CallExpression) []bool {
	if call == nil {
		return nil
	}
	mask := make([]bool, len(call.Arguments))
	found := false
	for i, arg := range call.Arguments {
		if arg.Name != nil || arg.Spread {
			return nil
		}
		if ast.IsIntegerLiteral(arg.Value) {
			mask[i] = true
			found = true
		}
	}
	if !found {
		return nil
	}
	return mask
}

// overloadWithTypedLiterals retries with float/decimal-typed int literals, fewest conversions winning; false on none or a tie.
func overloadWithTypedLiterals(overloads []runtime.Function, args []runtime.Value, mask []bool, this *runtime.Instance) (runtime.Function, []runtime.Value, bool) {
	var best runtime.Function
	var bestArgs []runtime.Value
	bestCount, tied := -1, false
	for _, fn := range overloads {
		if !functionAcceptsPositionalArgs(fn, len(args)) {
			continue
		}
		converted := append([]runtime.Value(nil), args...)
		count := typeLiteralArgs(fn, converted, mask, this)
		bound := make([]runtime.Value, len(fn.Parameters))
		copy(bound, converted)
		if count == 0 || !functionArgumentsMatch(fn, bound) {
			continue
		}
		switch {
		case bestCount < 0 || count < bestCount:
			best, bestArgs, bestCount, tied = fn, converted, count, false
		case count == bestCount:
			tied = true
		}
	}
	return best, bestArgs, bestCount >= 0 && !tied
}

// selectOverloadWithLiterals is selectOverload with integer literals typed by the chosen or fewest-conversion overload.
func selectOverloadWithLiterals(label string, overloads []runtime.Function, args []runtime.Value, mask []bool, this *runtime.Instance) (runtime.Function, []runtime.Value, error) {
	fn, err := selectOverload(label, overloads, args)
	if mask == nil {
		if err != nil && strings.HasPrefix(err.Error(), "no matching overload") {
			err = singleOverloadMismatch(overloads, args, this, err)
		}
		return fn, args, err
	}
	if err == nil {
		typeLiteralArgs(fn, args, mask, this)
		return fn, args, nil
	}
	if !strings.HasPrefix(err.Error(), "no matching overload") {
		return fn, args, err
	}
	if typed, converted, ok := overloadWithTypedLiterals(overloads, args, mask, this); ok {
		return typed, converted, nil
	}
	return fn, args, singleOverloadMismatch(overloads, args, this, err)
}

// singleOverloadMismatch replaces a no-match error with the lone candidate's precise parameter TypeError.
func singleOverloadMismatch(overloads []runtime.Function, args []runtime.Value, this *runtime.Instance, err error) error {
	if len(overloads) != 1 || !functionAcceptsPositionalArgs(overloads[0], len(args)) {
		return err
	}
	bound := make([]runtime.Value, len(overloads[0].Parameters))
	copy(bound, args)
	if precise := functionArgumentsMatchError(overloads[0], bound, this); precise != nil {
		return precise
	}
	return err
}

// typeLiteralArgs converts integer-literal args whose parameter is float or decimal, directly or via a bound type parameter.
func typeLiteralArgs(fn runtime.Function, args []runtime.Value, mask []bool, this *runtime.Instance) int {
	var bindings map[string]string
	if this != nil {
		bindings = this.TypeBindings
		if fn.OwnerClass != nil {
			bindings = runtime.DeclaringClassBindings(this, fn.OwnerClass)
		}
	}
	count := 0
	for i := range args {
		if i >= len(mask) || !mask[i] || i >= len(fn.Parameters) || fn.Parameters[i].Type == nil || fn.Parameters[i].Type.Operator != "" {
			continue
		}
		target := fn.Parameters[i].Type.Name
		if bound, ok := bindings[target]; ok && bound != "" && !shadowsClassTypeParameter(fn.TypeParameters, target) {
			target = strings.TrimPrefix(bound, "?")
		}
		target = strings.ToLower(target)
		if target != "float" && target != "decimal" {
			continue
		}
		switch args[i].(type) {
		case runtime.Int, runtime.SmallInt:
			if value, err := castValue(args[i], target); err == nil {
				args[i] = value
				count++
			}
		}
	}
	return count
}

// shadowsClassTypeParameter reports a method-level generic that reuses a class type parameter's name.
func shadowsClassTypeParameter(params []string, name string) bool {
	count := 0
	for _, param := range params {
		if strings.EqualFold(param, name) {
			count++
		}
	}
	return count > 1
}

// withoutShadowedTypeParams drops class bindings that a method-level generic of the same name hides.
func withoutShadowedTypeParams(bindings map[string]string, params []string) map[string]string {
	if len(bindings) == 0 || len(params) < 2 {
		return bindings
	}
	var out map[string]string
	for _, param := range params {
		if !shadowsClassTypeParameter(params, param) {
			continue
		}
		for key := range bindings {
			if !strings.EqualFold(key, param) {
				continue
			}
			if out == nil {
				out = make(map[string]string, len(bindings))
				for k, v := range bindings {
					out[k] = v
				}
			}
			delete(out, key)
		}
	}
	if out == nil {
		return bindings
	}
	return out
}
