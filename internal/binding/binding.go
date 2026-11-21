// Package binding is the single argument-binding implementation shared
// by the bytecode compiler, the bytecode VM's runtime call paths, and
// the evaluator. It maps call-site arguments (positional and named)
// onto parameter slots, honouring defaults and a trailing variadic
// parameter, and is the one source of binding error messages - the
// three formerly separate implementations drifted (the 1.17.0
// default+variadic crash cluster).
package binding

import (
	"fmt"
	"strings"
)

// Signature describes the callable being bound against. ParamNames and
// HasDefault are parallel; when Variadic is set the final parameter
// collects surplus positional arguments and cannot be named.
type Signature struct {
	FuncName string
	// Owner qualifies FuncName as Owner.FuncName in errors; built only on failure.
	Owner      string
	ParamNames []string
	HasDefault []bool
	Variadic   bool
}

// Arg is one call-site argument. Name is empty for positional
// arguments. Named matching is case-insensitive, matching the
// language's identifier rules.
type Arg struct {
	Name string
}

// DefaultSlot in a slot mapping means the parameter takes its declared
// default (or, for the variadic slot, packs from VariadicArgs).
const DefaultSlot = -1

// Result is the slot mapping Order produces. Slots has one entry per
// parameter: the argument index that fills it, or DefaultSlot. For a
// variadic signature the consumer packs the variadic value as: the
// TailArgs indices when present (positional overflow), else the
// variadic parameter's own slot as a single packed element when set
// (it is nameable, matching language behaviour), else empty.
type Result struct {
	Slots    []int
	TailArgs []int
}

func (sig Signature) displayName() string {
	if sig.Owner != "" && sig.FuncName != "" {
		return sig.Owner + "." + sig.FuncName
	}
	return DisplayName(sig.FuncName)
}

// DisplayName is the function name used in binding errors, with the
// VM's established fallback for anonymous callables.
func DisplayName(name string) string {
	if name == "" {
		return "<closure>"
	}
	return name
}

// MissingArgumentError is the canonical error for parameter i left unfilled.
func MissingArgumentError(sig Signature, i int) error {
	return fmt.Errorf("%s missing argument %s", sig.displayName(), sig.ParamNames[i])
}

func (sig Signature) hasDefaultAt(i int) bool {
	return i < len(sig.HasDefault) && sig.HasDefault[i]
}

func (sig Signature) variadicIndex() int {
	if sig.Variadic && len(sig.ParamNames) > 0 {
		return len(sig.ParamNames) - 1
	}
	return -1
}

// Order computes the slot mapping for a call. Errors use the canonical
// wording every consumer reports:
//
//	<fn> missing argument <name>
//	<fn> expects at most <n> arguments, got <m>
//	<fn> has no parameter <name>
//	<fn> parameter <name> passed more than once
func Order(sig Signature, args []Arg) (Result, error) {
	paramCount := len(sig.ParamNames)
	variadicIndex := sig.variadicIndex()
	result := Result{Slots: make([]int, paramCount)}
	for i := range result.Slots {
		result.Slots[i] = DefaultSlot
	}

	hasNamed := false
	for _, arg := range args {
		if arg.Name != "" {
			hasNamed = true
			break
		}
	}

	if !hasNamed {
		fixedCount := paramCount
		if variadicIndex >= 0 {
			fixedCount--
		}
		if len(args) > fixedCount && variadicIndex < 0 {
			return Result{}, fmt.Errorf("%s expects at most %d arguments, got %d", sig.displayName(), fixedCount, len(args))
		}
		for i := 0; i < fixedCount; i++ {
			if i < len(args) {
				result.Slots[i] = i
			} else if !sig.hasDefaultAt(i) {
				return Result{}, fmt.Errorf("%s missing argument %s", sig.displayName(), sig.ParamNames[i])
			}
		}
		for i := fixedCount; i < len(args); i++ {
			result.TailArgs = append(result.TailArgs, i)
		}
		return result, nil
	}

	// Named path: every parameter is nameable and positionally
	// fillable, including the variadic slot (which then packs the one
	// argument it received) - matching established language behaviour.
	positions := make(map[string]int, paramCount)
	for i, name := range sig.ParamNames {
		positions[strings.ToLower(name)] = i
	}
	nextPositional := 0
	for argIndex, arg := range args {
		if arg.Name == "" {
			for nextPositional < paramCount && result.Slots[nextPositional] != DefaultSlot {
				nextPositional++
			}
			if nextPositional >= paramCount {
				return Result{}, fmt.Errorf("%s expects at most %d arguments, got %d", sig.displayName(), paramCount, len(args))
			}
			result.Slots[nextPositional] = argIndex
			nextPositional++
			continue
		}
		position, ok := positions[strings.ToLower(arg.Name)]
		if !ok {
			return Result{}, fmt.Errorf("%s has no parameter %s", sig.displayName(), arg.Name)
		}
		if result.Slots[position] != DefaultSlot {
			return Result{}, fmt.Errorf("%s parameter %s passed more than once", sig.displayName(), arg.Name)
		}
		result.Slots[position] = argIndex
	}
	for i := 0; i < paramCount; i++ {
		if i == variadicIndex || result.Slots[i] != DefaultSlot {
			continue
		}
		if !sig.hasDefaultAt(i) {
			return Result{}, fmt.Errorf("%s missing argument %s", sig.displayName(), sig.ParamNames[i])
		}
	}
	return result, nil
}
